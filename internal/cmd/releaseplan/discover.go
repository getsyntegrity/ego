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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// skipDirs are directories the module scan never descends into:
// internal/cmd/ciselect/main.go's own skipDirs list (huge module/build
// caches, fixtures the go command itself ignores, or tooling state that
// cannot hold a Go module relevant to a release — .atl among them), plus
// ".claude", where this repository's local git worktrees live
// (.claude/worktrees/<name>) and could otherwise surface an extra copy of
// every go.mod.
var skipDirs = map[string]bool{
	".git":         true,
	"vendor":       true,
	"testdata":     true,
	"node_modules": true,
	".codegraph":   true,
	".atl":         true,
	".claude":      true,
	"odd":          true,
}

// Module is one discovered Go module: its repository-relative directory
// (forward slashes, "." for the root), its module path read from go.mod,
// and the module paths it requires that belong to another discovered
// module (in-repository requires only; third-party requires are dropped).
type Module struct {
	Dir      string
	Path     string
	Requires []string
}

// Graph is every Go module discovered under a repository root.
type Graph struct {
	// Modules holds every discovered module, the root (".") first, then
	// nested modules sorted by directory.
	Modules   []Module
	dirOfPath map[string]string
}

// ByDir returns the discovered module at dir, and whether one exists.
func (g *Graph) ByDir(dir string) (Module, bool) {
	for _, m := range g.Modules {
		if m.Dir == dir {
			return m, true
		}
	}
	return Module{}, false
}

// DirOfPath returns the directory of the discovered module whose module
// path is path, and whether one exists.
func (g *Graph) DirOfPath(path string) (string, bool) {
	dir, ok := g.dirOfPath[path]
	return dir, ok
}

// discoverGraph walks root for every go.mod (skipping skipDirs and never
// descending past a directory that itself holds a go.mod), reads each
// with `go mod edit -json` — a pure text parse, no network, no build, as
// internal/cmd/ciselect/main.go's readGoMod does — and returns the
// resulting Graph. A require whose path matches another discovered
// module's path is an in-repository require, regardless of any replace
// directive: the declared require is what a release order must honor,
// independent of the local development override.
func discoverGraph(root string) (*Graph, error) {
	dirs, err := findModuleDirs(root)
	if err != nil {
		return nil, err
	}

	mods := make([]goModFile, len(dirs))
	infos := make([]Module, len(dirs))
	dirOfPath := make(map[string]string, len(dirs))
	for i, dir := range dirs {
		mod, err := readGoMod(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			return nil, err
		}
		if mod.Module.Path == "" {
			return nil, fmt.Errorf("go.mod in %s declares no module path", dir)
		}
		if other, dup := dirOfPath[mod.Module.Path]; dup {
			return nil, fmt.Errorf("module path %s is declared by both %s and %s", mod.Module.Path, other, dir)
		}
		mods[i] = mod
		infos[i] = Module{Dir: dir, Path: mod.Module.Path}
		dirOfPath[mod.Module.Path] = dir
	}

	for i := range infos {
		var requires []string
		for _, req := range mods[i].Require {
			if req.Path == infos[i].Path {
				continue
			}
			if _, inRepo := dirOfPath[req.Path]; inRepo {
				requires = append(requires, req.Path)
			}
		}
		sort.Strings(requires)
		infos[i].Requires = requires
	}

	return &Graph{Modules: infos, dirOfPath: dirOfPath}, nil
}

// findModuleDirs returns the repository-relative directory ("." for root,
// forward slashes for nested modules) of every go.mod under root, root
// first, then sorted lexicographically. It does not descend into
// skipDirs, nor into a subdirectory once a go.mod is found there (a
// nested module never contains another module the way this scan looks
// for one directly beneath it).
func findModuleDirs(root string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return nil, fmt.Errorf("no go.mod at repository root %s: %w", root, err)
	}

	var nested []string
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path == root {
			return nil
		}
		if skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		if _, statErr := os.Stat(filepath.Join(path, "go.mod")); statErr == nil {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			nested = append(nested, filepath.ToSlash(rel))
			return filepath.SkipDir
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Strings(nested)

	return append([]string{"."}, nested...), nil
}

// goModFile mirrors the subset of `go mod edit -json` output discovery
// needs (internal/cmd/ciselect/main.go's goModFile).
type goModFile struct {
	Module struct {
		Path string
	}
	Require []struct {
		Path    string
		Version string
	}
}

// goCommand returns a `go` command running in dir with the caller's
// environment, except GOWORK forced off: discovery never runs in
// workspace mode, where a go.work could change what `go mod edit -json`
// reports.
func goCommand(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GOWORK=") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "GOWORK=off")
	return cmd
}

// readGoMod runs `go mod edit -json` in the module at dir: it only parses
// the go.mod file, with no network, no module download and no build —
// the same call internal/cmd/ciselect/main.go's readGoMod makes.
func readGoMod(dir string) (goModFile, error) {
	var mod goModFile
	cmd := goCommand(dir, "mod", "edit", "-json")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return mod, fmt.Errorf("go mod edit -json in %s: %w: %s", dir, err, strings.TrimSpace(stderr.String()))
	}
	if err := json.Unmarshal(out, &mod); err != nil {
		return mod, fmt.Errorf("decoding go mod edit -json in %s: %w", dir, err)
	}
	return mod, nil
}
