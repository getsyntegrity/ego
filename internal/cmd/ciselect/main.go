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
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

	result := selector.Select(graph, moduleRelChanged, selector.Options{
		All:           *allFlag,
		Reason:        *reasonFlag,
		SatelliteDirs: satelliteDirs,
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

// writeOutputs writes mode, packages.txt, coverpkg and summary.md into
// outDir, creating it if necessary.
func writeOutputs(outDir string, result selector.Result, summary string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	files := map[string]string{
		"mode":         string(result.Mode) + "\n",
		"packages.txt": joinLines(result.Selected),
		"coverpkg":     strings.Join(result.Included, ","),
		"summary.md":   summary,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte(content), 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
	}
	return nil
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
