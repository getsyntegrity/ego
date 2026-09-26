// MIT License
//
// Copyright (c) 2022-2026 Arsene Tochemey Gandote
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pablogore/ego/v4/internal/cmd/archcheck/rules"
)

// skipDirNames are directory names the loaders never descend into, at any
// level except the module/repo root itself: module/build caches, VCS
// metadata and generated test fixtures that cannot hold a package this
// check needs to see.
func skipDirName(name string) bool {
	if name == "vendor" || name == "testdata" || name == ".git" {
		return true
	}
	return strings.HasPrefix(name, ".") && name != "."
}

// rawListPackage mirrors the subset of `go list -e -json` fields the root
// loader needs; it deliberately omits TestImports and XTestImports because
// only production imports are ever checked (see loadRootModule).
type rawListPackage struct {
	ImportPath string
	Imports    []string
	Error      *struct {
		Err string
	}
}

// loadRootModule runs `go list -e -json ./...` in repoRoot and returns one
// rules.Package per root-module package, carrying only its production
// imports. It honors the inherited environment (e.g. GOFLAGS=-mod=vendor in
// CI) and decodes the concatenated JSON object stream `go list` prints, one
// object per package, exactly like internal/cmd/ciselect does.
//
// A package that failed to load is a hard failure, not a skip: a package
// archcheck cannot see the imports of might be hiding a real violation, so
// this fails closed rather than silently checking less than it claims to.
func loadRootModule(repoRoot string) ([]rules.Package, error) {
	cmd := exec.Command("go", "list", "-e", "-json", "./...")
	cmd.Dir = repoRoot
	cmd.Env = os.Environ()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting go list: %w", err)
	}

	dec := json.NewDecoder(stdout)
	var pkgs []rules.Package
	var loadErrs []string
	for {
		var raw rawListPackage
		if err := dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			_ = cmd.Wait()
			return nil, fmt.Errorf("decoding go list output: %w", err)
		}
		if raw.Error != nil {
			loadErrs = append(loadErrs, fmt.Sprintf("%s: %s", raw.ImportPath, raw.Error.Err))
			continue
		}
		if isVendoredImportPath(raw.ImportPath) {
			continue
		}
		pkgs = append(pkgs, rules.Package{
			ImportPath: raw.ImportPath,
			Kind:       rules.RootModule,
			Imports:    raw.Imports,
		})
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("go list -e -json ./...: %w: %s", err, stderr.String())
	}
	if len(loadErrs) > 0 {
		return nil, fmt.Errorf("root module package graph has load errors:\n%s", strings.Join(loadErrs, "\n"))
	}
	return pkgs, nil
}

func isVendoredImportPath(importPath string) bool {
	return importPath == "vendor" || strings.Contains(importPath, "/vendor/") || strings.HasPrefix(importPath, "vendor/")
}

// discoverNestedModuleDirs finds every directory under repoRoot that holds
// its own go.mod, other than repoRoot's own. It never descends into
// vendor/, testdata/, a hidden directory, or a satellite module once found
// (a module inside another module is not a case this repository has, but
// the loader must not silently merge one into its parent if it ever does).
func discoverNestedModuleDirs(repoRoot string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != repoRoot && skipDirName(d.Name()) {
			return filepath.SkipDir
		}
		if path == repoRoot {
			return nil
		}
		if _, statErr := os.Stat(filepath.Join(path, "go.mod")); statErr == nil {
			dirs = append(dirs, path)
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(dirs)
	return dirs, nil
}

// parseGoModModulePath extracts the module path from the "module ..." line
// of the go.mod file at path, without depending on golang.org/x/mod: only
// the module's own declared identity is needed here, not full go.mod
// semantics.
func parseGoModModulePath(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s: no module line found", path)
}

// loadNestedModule parses every non-_test .go file under moduleDir with
// go/parser in imports-only mode (no module download, no network, no
// building), groups them by directory into packages, and returns one
// rules.Package per non-empty directory. A directory's import path is the
// module path, plus "/" and the directory's path relative to moduleDir
// when that is not the module root itself.
func loadNestedModule(moduleDir string) ([]rules.Package, error) {
	modulePath, err := parseGoModModulePath(filepath.Join(moduleDir, "go.mod"))
	if err != nil {
		return nil, err
	}

	importsByDir := make(map[string]map[string]bool)
	fset := token.NewFileSet()

	walkErr := filepath.WalkDir(moduleDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != moduleDir && skipDirName(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return fmt.Errorf("parsing %s: %w", path, parseErr)
		}

		dir := filepath.Dir(path)
		rel, relErr := filepath.Rel(moduleDir, dir)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if importsByDir[rel] == nil {
			importsByDir[rel] = make(map[string]bool)
		}
		for _, imp := range file.Imports {
			impPath, unquoteErr := strconv.Unquote(imp.Path.Value)
			if unquoteErr != nil {
				return fmt.Errorf("parsing import in %s: %w", path, unquoteErr)
			}
			importsByDir[rel][impPath] = true
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	pkgs := make([]rules.Package, 0, len(importsByDir))
	for rel, imports := range importsByDir {
		importPath := modulePath
		if rel != "." {
			importPath = modulePath + "/" + rel
		}
		list := make([]string, 0, len(imports))
		for imp := range imports {
			list = append(list, imp)
		}
		sort.Strings(list)
		pkgs = append(pkgs, rules.Package{
			ImportPath: importPath,
			Kind:       rules.NestedModule,
			Imports:    list,
		})
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].ImportPath < pkgs[j].ImportPath })
	return pkgs, nil
}

// loadNestedModules discovers and loads every nested module under
// repoRoot.
func loadNestedModules(repoRoot string) ([]rules.Package, error) {
	dirs, err := discoverNestedModuleDirs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("discovering nested modules: %w", err)
	}

	var pkgs []rules.Package
	for _, dir := range dirs {
		modPkgs, err := loadNestedModule(dir)
		if err != nil {
			return nil, fmt.Errorf("loading nested module %s: %w", dir, err)
		}
		pkgs = append(pkgs, modPkgs...)
	}
	return pkgs, nil
}
