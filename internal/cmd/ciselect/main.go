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

// Command ciselect decides which packages of the github.com/pablogore/ego/v4
// module a change must test and cover, and writes that decision out for the
// CI workflows to consume. See internal/cmd/ciselect/selector for the
// selection rules.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
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

	"github.com/pablogore/ego/v4/internal/cmd/ciselect/selector"
)

// skipDirs are directories the satellite-module scan never descends into:
// they are either huge (module/build caches) or cannot contain a Go module
// relevant to this repository.
var skipDirs = map[string]bool{
	".git":         true,
	"vendor":       true,
	"node_modules": true,
	".codegraph":   true,
	".atl":         true,
	"odd":          true,
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "ciselect: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("ciselect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	changedFlag := fs.String("changed", "", "path to a newline-separated list of changed, repo-relative files (\"-\" reads stdin)")
	allFlag := fs.Bool("all", false, "select the full suite without reading -changed")
	reasonFlag := fs.String("reason", "", "optional extra context appended to the summary")
	moduleDirFlag := fs.String("module-dir", ".", "directory of the Go module to select packages from")
	repoRootFlag := fs.String("repo-root", ".", "repository root that -changed paths are relative to")
	outDirFlag := fs.String("out-dir", "", "directory to write mode, packages.txt, coverpkg and summary.md into (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *outDirFlag == "" {
		return errors.New("-out-dir is required")
	}
	if !*allFlag && *changedFlag == "" {
		return errors.New("either -changed or -all is required")
	}

	moduleDir, err := filepath.Abs(*moduleDirFlag)
	if err != nil {
		return fmt.Errorf("resolving -module-dir: %w", err)
	}
	repoRoot, err := filepath.Abs(*repoRootFlag)
	if err != nil {
		return fmt.Errorf("resolving -repo-root: %w", err)
	}

	modulePath, err := goListModulePath(moduleDir)
	if err != nil {
		return fmt.Errorf("determining module path: %w", err)
	}

	pkgs, err := goListPackages(moduleDir)
	if err != nil {
		return fmt.Errorf("loading package graph: %w", err)
	}

	graph := selector.Graph{
		ModulePath: modulePath,
		ModuleDir:  moduleDir,
		Packages:   pkgs,
	}

	var loadErrs []string
	for _, p := range graph.Packages {
		if p.Error != "" {
			loadErrs = append(loadErrs, fmt.Sprintf("%s: %s", p.ImportPath, p.Error))
		}
	}
	if len(loadErrs) > 0 {
		return fmt.Errorf("package graph has load errors:\n%s", strings.Join(loadErrs, "\n"))
	}

	var changed []string
	if !*allFlag {
		changed, err = readChangedFiles(*changedFlag)
		if err != nil {
			return fmt.Errorf("reading -changed: %w", err)
		}
	}
	moduleRelChanged := make([]string, len(changed))
	for i, c := range changed {
		moduleRelChanged[i] = toModuleRelPath(repoRoot, moduleDir, c)
	}

	satelliteDirs, err := findSatelliteDirs(moduleDir)
	if err != nil {
		return fmt.Errorf("scanning for satellite modules: %w", err)
	}

	modules := make([]selector.Module, 0, len(satelliteDirs))
	for _, dir := range satelliteDirs {
		imports, err := discoverModuleImports(moduleDir, dir, modulePath)
		if err != nil {
			return fmt.Errorf("discovering imports for module %s: %w", dir, err)
		}
		modules = append(modules, selector.Module{Dir: dir, Imports: imports})
	}

	result := selector.Select(graph, moduleRelChanged, selector.Options{
		All:           *allFlag,
		Reason:        *reasonFlag,
		SatelliteDirs: satelliteDirs,
		Modules:       modules,
	})

	summary := selector.BuildSummary(result)
	if err := writeOutputs(*outDirFlag, result, summary); err != nil {
		return fmt.Errorf("writing outputs: %w", err)
	}

	fmt.Fprint(stdout, summary)
	return nil
}

// goListModulePath returns the import path of the module rooted at dir.
func goListModulePath(dir string) (string, error) {
	cmd := exec.Command("go", "list", "-m")
	cmd.Dir = dir
	cmd.Env = os.Environ()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go list -m: %w: %s", err, stderr.String())
	}
	return strings.TrimSpace(string(out)), nil
}

// rawPackage mirrors the subset of `go list -e -json` fields the selector
// needs.
type rawPackage struct {
	ImportPath   string
	Dir          string
	Imports      []string
	TestImports  []string
	XTestImports []string
	Error        *struct {
		Err string
	}
}

// goListPackages runs `go list -e -json ./...` in dir and decodes the
// concatenated JSON stream it prints, one object per package.
func goListPackages(dir string) ([]selector.Package, error) {
	cmd := exec.Command("go", "list", "-e", "-json", "./...")
	cmd.Dir = dir
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
	var pkgs []selector.Package
	for {
		var raw rawPackage
		if err := dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			_ = cmd.Wait()
			return nil, fmt.Errorf("decoding go list output: %w", err)
		}
		p := selector.Package{
			ImportPath:   raw.ImportPath,
			Dir:          raw.Dir,
			Imports:      raw.Imports,
			TestImports:  raw.TestImports,
			XTestImports: raw.XTestImports,
		}
		if raw.Error != nil {
			p.Error = raw.Error.Err
		}
		pkgs = append(pkgs, p)
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("go list -e -json ./...: %w: %s", err, stderr.String())
	}
	return pkgs, nil
}

// readChangedFiles reads newline-separated, repo-relative paths from path
// ("-" reads stdin), skipping blank lines.
func readChangedFiles(path string) ([]string, error) {
	var r io.Reader
	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		r = f
	}

	var lines []string
	scanner := bufio.NewScanner(r)
	// Long generated diffs can exceed bufio's default 64KiB line limit if a
	// single path were pathological; 1MiB is generous headroom.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// toModuleRelPath converts a repo-relative changed-file path into a path
// relative to the module directory, using forward slashes, for the
// selector to classify.
func toModuleRelPath(repoRoot, moduleDir, repoRelPath string) string {
	abs := filepath.Join(repoRoot, repoRelPath)
	rel, err := filepath.Rel(moduleDir, abs)
	if err != nil {
		// Fall back to the original path; the selector will classify it
		// as unknown and fail safe to the full suite.
		return filepath.ToSlash(repoRelPath)
	}
	return filepath.ToSlash(rel)
}

// findSatelliteDirs scans under root for directories that hold their own
// go.mod (other than root's own go.mod), and returns their paths relative
// to root, using forward slashes. It does not descend into a satellite
// directory once found, nor into skipDirs.
func findSatelliteDirs(root string) ([]string, error) {
	var satellites []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		if path == root {
			return nil
		}
		if _, statErr := os.Stat(filepath.Join(path, "go.mod")); statErr == nil {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			satellites = append(satellites, filepath.ToSlash(rel))
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return satellites, nil
}

// writeOutputs writes mode, packages.txt, coverpkg, modules.json and
// summary.md into outDir, creating it if necessary.
func writeOutputs(outDir string, result selector.Result, summary string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	modulesJSON, err := modulesJSON(result.Modules)
	if err != nil {
		return fmt.Errorf("encoding modules.json: %w", err)
	}
	files := map[string]string{
		"mode":         string(result.Mode) + "\n",
		"packages.txt": joinLines(result.Selected),
		"coverpkg":     strings.Join(result.Included, ","),
		"modules.json": modulesJSON,
		"summary.md":   summary,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte(content), 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
	}
	return nil
}

// modulesJSON renders the selected module directories as a JSON array of
// strings, always valid JSON: "[]" when none were selected, never "null".
// This is what pull_request.yml and build.yml feed into a matrix job's
// fromJSON().
func modulesJSON(modules []selector.ModuleSelection) (string, error) {
	dirs := make([]string, 0, len(modules))
	for _, m := range modules {
		dirs = append(dirs, m.Dir)
	}
	b, err := json.Marshal(dirs)
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// moduleImportsSkipDir reports whether a directory named name must never be
// descended into while parsing a nested module's own files: skipDirs
// (module/build caches, VCS metadata) plus "vendor", which a released
// nested module never checks in but a locally `go mod vendor`-ed one
// might.
func moduleImportsSkipDir(name string) bool {
	return skipDirs[name] || name == "vendor"
}

// discoverModuleImports parses every non-vendor .go file under the nested
// module at repo-relative dir (resolved against moduleDir), including
// _test.go files — a nested module's tests can import a root package its
// production code does not, and a CI-selection decision must not miss
// that — with go/parser in imports-only mode: no module download, no
// network, no build. It returns the sorted, de-duplicated set of import
// paths that start with modulePath (the root module being selected for);
// a nested module's other dependencies can never appear in the root
// package graph, so they are dropped here rather than carried around
// unused.
func discoverModuleImports(moduleDir, dir, modulePath string) ([]string, error) {
	root := filepath.Join(moduleDir, filepath.FromSlash(dir))
	fset := token.NewFileSet()
	imports := make(map[string]bool)

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && moduleImportsSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return fmt.Errorf("parsing %s: %w", path, parseErr)
		}
		for _, imp := range file.Imports {
			impPath, unquoteErr := strconv.Unquote(imp.Path.Value)
			if unquoteErr != nil {
				return fmt.Errorf("parsing import in %s: %w", path, unquoteErr)
			}
			if impPath == modulePath || strings.HasPrefix(impPath, modulePath+"/") {
				imports[impPath] = true
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	out := make([]string, 0, len(imports))
	for imp := range imports {
		out = append(out, imp)
	}
	sort.Strings(out)
	return out, nil
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
