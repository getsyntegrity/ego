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

// Package selector decides which of a Go module's packages a change must
// test and cover, given the module's package graph and the set of changed
// files. It is a pure, dependency-free package: nothing here execs a
// process or touches the filesystem, so it can be unit tested with
// in-memory fixtures. The internal/cmd/ciselect command wires this package
// to `go list -e -json` and to the filesystem.
package selector

import (
	"path/filepath"
	"sort"
	"strings"
)

// Package is one entry from `go list -e -json`, restricted to the fields
// the selector needs.
type Package struct {
	// ImportPath is the package's full import path, e.g.
	// "github.com/pablogore/ego/v4/internal/pause".
	ImportPath string
	// Dir is the package's absolute directory on disk.
	Dir string
	// Imports are the import paths this package's non-test files import
	// (both module-internal and external; only module-internal ones are
	// used to build the reverse dependency graph).
	Imports []string
	// TestImports are the import paths this package's in-package (_test.go,
	// same package) test files import.
	TestImports []string
	// XTestImports are the import paths this package's external
	// (_test.go, package foo_test) test files import.
	XTestImports []string
	// Error is the load error `go list -e -json` reported for this
	// package, if any. A non-empty Error must make the caller fail the
	// whole selection rather than silently skip the package.
	Error string
}

// Graph is a Go module's package graph, as loaded from
// `go list -e -json ./...` run inside the module directory.
type Graph struct {
	// ModulePath is the module's import path, e.g.
	// "github.com/pablogore/ego/v4".
	ModulePath string
	// ModuleDir is the module's root directory on disk (absolute).
	ModuleDir string
	// Packages are every package `go list -e -json ./...` reported for
	// this module.
	Packages []Package
}

// excludedSegments lists the whole path SEGMENTS (relative to the module
// path, matched exactly, never by substring) that are excluded from the
// tested-and-covered "included" package set. A segment match on "test"
// must not also match "testkit": that substring bug is exactly what
// go-acc's `--ignore test` did, and it silently dropped a real public
// package (./testkit) from both execution and coverage. Segment matching
// fixes that.
var excludedSegments = []string{
	// egopb: generated protobuf code; nothing to unit test.
	"egopb",
	// example: sample programs (and, below it, the example/cluster
	// satellite module); not library behavior.
	"example",
	// mocks: generated mockery output.
	"mocks",
	// test: shared test-only fixtures and generated protobuf
	// (test/data/testpb); note this does NOT match "testkit" because
	// matching is by whole segment, not substring.
	"test",
}

// Included returns the sorted import paths of every package in g that is
// tested and covered: every module package minus the excluded segments.
// The coverage denominator (coverpkg) is always this full set, in every
// selection mode, so coverage numbers stay comparable across runs.
func Included(g Graph) []string {
	out := make([]string, 0, len(g.Packages))
	for _, p := range g.Packages {
		if isExcludedImportPath(p.ImportPath, g.ModulePath) {
			continue
		}
		out = append(out, p.ImportPath)
	}
	sort.Strings(out)
	return out
}

// isExcludedImportPath reports whether importPath falls under one of
// excludedSegments, relative to modulePath, matching whole path segments
// only.
func isExcludedImportPath(importPath, modulePath string) bool {
	rel := strings.TrimPrefix(importPath, modulePath)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		// The root package itself is never excluded.
		return false
	}
	first := rel
	if idx := strings.IndexByte(rel, '/'); idx >= 0 {
		first = rel[:idx]
	}
	for _, seg := range excludedSegments {
		if first == seg {
			return true
		}
	}
	return false
}

// buildDirIndex maps each package's directory, expressed relative to the
// module root using forward slashes (posix-style, "." for the module root
// itself), to that package's import path. It is used to resolve a changed
// file's directory to the package that owns it.
func buildDirIndex(g Graph) map[string]string {
	idx := make(map[string]string, len(g.Packages))
	for _, p := range g.Packages {
		rel, err := filepath.Rel(g.ModuleDir, p.Dir)
		if err != nil {
			continue
		}
		idx[filepath.ToSlash(rel)] = p.ImportPath
	}
	return idx
}

// buildImporters returns, for every module-internal import path, the list
// of packages whose non-test build (Imports) directly imports it. Only
// edges between packages present in g are kept; standard-library and
// third-party imports are not module-internal and are ignored.
func buildImporters(g Graph) map[string][]string {
	byPath := make(map[string]bool, len(g.Packages))
	for _, p := range g.Packages {
		byPath[p.ImportPath] = true
	}
	importers := make(map[string][]string)
	for _, p := range g.Packages {
		for _, imp := range p.Imports {
			if byPath[imp] {
				importers[imp] = append(importers[imp], p.ImportPath)
			}
		}
	}
	return importers
}

// affectedByChange computes the set of packages affected by a change to
// the packages in changed:
//
//  1. R = changed, plus every package whose non-test build transitively
//     imports something in changed (reverse closure over Imports).
//  2. affected = R, plus every package P whose TestImports or XTestImports
//     directly names a package in R (a test binary links its test
//     imports' transitive deps, and R is already closed over those).
func affectedByChange(g Graph, changed map[string]bool) map[string]bool {
	byPath := make(map[string]bool, len(g.Packages))
	for _, p := range g.Packages {
		byPath[p.ImportPath] = true
	}
	importers := buildImporters(g)

	r := make(map[string]bool)
	var queue []string
	for c := range changed {
		if !byPath[c] {
			continue
		}
		if !r[c] {
			r[c] = true
			queue = append(queue, c)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, importer := range importers[cur] {
			if !r[importer] {
				r[importer] = true
				queue = append(queue, importer)
			}
		}
	}

	affected := make(map[string]bool, len(r))
	for p := range r {
		affected[p] = true
	}
	for _, p := range g.Packages {
		if affected[p.ImportPath] {
			continue
		}
		if testImportsIntersect(p, r) {
			affected[p.ImportPath] = true
		}
	}
	return affected
}

func testImportsIntersect(p Package, r map[string]bool) bool {
	for _, ti := range p.TestImports {
		if r[ti] {
			return true
		}
	}
	for _, ti := range p.XTestImports {
		if r[ti] {
			return true
		}
	}
	return false
}
