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

package selector

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// Class is how a single changed file was classified.
type Class int

const (
	// ClassPackage is a file that belongs to a specific module package
	// (directly, or via a testdata directory).
	ClassPackage Class = iota
	// ClassNoTest is documentation or governance content that needs no
	// test run (e.g. *.md, openspec/, assets/).
	ClassNoTest
	// ClassSatellite is a file inside a nested Go module (its own go.mod)
	// that this selector does not cover.
	ClassSatellite
	// ClassFullFallback is a high-impact path (build/CI config, the
	// selector's own source, or a shared-core package) that forces the
	// full suite.
	ClassFullFallback
	// ClassUnknown is a path the selector could not classify at all; it
	// also forces the full suite, to fail safe.
	ClassUnknown
)

// String renders the Class the way summary.md groups changed files.
func (c Class) String() string {
	switch c {
	case ClassPackage:
		return "package"
	case ClassNoTest:
		return "no-test"
	case ClassSatellite:
		return "satellite"
	case ClassFullFallback:
		return "full-fallback"
	case ClassUnknown:
		return "unknown"
	default:
		return "unclassified"
	}
}

// ChangedFile is one input path together with its classification.
type ChangedFile struct {
	// Path is exactly the path as given to Select (repo-relative, as read
	// from the -changed input).
	Path string
	// Class is the file's classification.
	Class Class
	// Package is set when Class == ClassPackage: the import path of the
	// package that owns the file.
	Package string
	// Reason explains why the file was classified this way; always set
	// for ClassFullFallback, ClassSatellite and ClassUnknown.
	Reason string
}

// fullFallbackExactFiles are module-relative file paths whose change
// forces the full suite: build/toolchain/lint/proto configuration that
// affects every package's build or test outcome.
var fullFallbackExactFiles = map[string]bool{
	"go.mod":        true,
	"go.sum":        true,
	"Makefile":      true,
	"Dockerfile.ci": true,
	".golangci.yml": true,
	"buf.yaml":      true,
	"buf.gen.yaml":  true,
}

// fullFallbackPrefixDirs are module-relative directory prefixes whose
// change forces the full suite:
//   - .github/: workflow and repo automation configuration.
//   - protos/: proto sources that egopb (and everything downstream) is
//     generated from.
//   - internal/cmd/ciselect/: the selector's own source; a bug here must
//     not be trusted to select its own fix.
//   - scripts/ci/: the CI scripts the workflows invoke.
//   - egopb/: generated protobuf types that nearly every package imports
//     directly or transitively; a shared-core package.
var fullFallbackPrefixDirs = []string{
	".github",
	"protos",
	"internal/cmd/ciselect",
	"scripts/ci",
	"egopb",
}

// noTestPrefixDirs are module-relative directory prefixes that hold
// documentation or governance content with no associated behavior to
// test.
var noTestPrefixDirs = []string{
	"openspec",
	".spec-governance",
	"assets",
}

// noTestExactFiles are module-relative file paths that hold documentation
// or governance content with no associated behavior to test.
var noTestExactFiles = map[string]bool{
	"LICENSE":       true,
	"renovate.json": true,
}

// classify decides the Class of a single changed path.
//
// path is as given to Select (normally repo-relative, but by construction
// of the caller it is expected to already be expressed relative to the
// module root using forward slashes before it reaches here — see
// normalizeChangedPath). satelliteDirs are module-relative, forward-slash
// directories known to hold a nested go.mod. dirIndex maps a module-
// relative package directory to its import path (see buildDirIndex).
func classify(p string, satelliteDirs []string, dirIndex map[string]string) ChangedFile {
	cf := ChangedFile{Path: p}
	sp := normalizeChangedPath(p)

	if fullFallbackExactFiles[sp] {
		cf.Class = ClassFullFallback
		cf.Reason = fmt.Sprintf("%s changed (full-fallback path)", sp)
		return cf
	}
	for _, d := range fullFallbackPrefixDirs {
		if hasPathPrefix(sp, d) {
			cf.Class = ClassFullFallback
			cf.Reason = fmt.Sprintf("%s changed (under full-fallback path %s/)", sp, d)
			return cf
		}
	}
	if isRootPackageGoFile(sp) {
		cf.Class = ClassFullFallback
		cf.Reason = fmt.Sprintf("%s changed (shared root package)", sp)
		return cf
	}
	if d, ok := matchingPrefix(sp, satelliteDirs); ok {
		cf.Class = ClassSatellite
		cf.Reason = fmt.Sprintf("%s is inside satellite module %s (not covered by this lane, see #104)", sp, d)
		return cf
	}
	if isNoTestPath(sp) {
		cf.Class = ClassNoTest
		cf.Reason = fmt.Sprintf("%s is documentation/governance content", sp)
		return cf
	}

	dir := packageDirFor(sp)
	if imp, ok := dirIndex[dir]; ok {
		cf.Class = ClassPackage
		cf.Package = imp
		return cf
	}

	cf.Class = ClassUnknown
	cf.Reason = fmt.Sprintf("unrecognized path %s: no matching package (full-suite fallback)", sp)
	return cf
}

// normalizeChangedPath converts an input path to a clean, forward-slash,
// "./"-stripped form for comparison against the classification tables.
func normalizeChangedPath(p string) string {
	sp := filepath.ToSlash(p)
	sp = strings.TrimPrefix(sp, "./")
	return sp
}

// hasPathPrefix reports whether sp is prefix, or is inside the directory
// named prefix.
func hasPathPrefix(sp, prefix string) bool {
	return sp == prefix || strings.HasPrefix(sp, prefix+"/")
}

func matchingPrefix(sp string, prefixes []string) (string, bool) {
	for _, prefix := range prefixes {
		if hasPathPrefix(sp, prefix) {
			return prefix, true
		}
	}
	return "", false
}

// isRootPackageGoFile reports whether sp is a .go file directly inside the
// module root directory (not a subdirectory).
func isRootPackageGoFile(sp string) bool {
	return path.Dir(sp) == "." && strings.HasSuffix(sp, ".go")
}

// isNoTestPath reports whether sp is documentation or governance content.
func isNoTestPath(sp string) bool {
	if strings.EqualFold(path.Ext(sp), ".md") {
		return true
	}
	if noTestExactFiles[sp] {
		return true
	}
	for _, d := range noTestPrefixDirs {
		if hasPathPrefix(sp, d) {
			return true
		}
	}
	return false
}

// packageDirFor returns the module-relative package directory sp belongs
// to: the file's own directory, except that any "testdata" path segment
// (and everything the file is nested under it) is stripped, so a file
// under a testdata directory maps to the nearest ancestor package
// directory instead of to "testdata" itself.
func packageDirFor(sp string) string {
	dir := path.Dir(sp)
	if dir == "." {
		return dir
	}
	segs := strings.Split(dir, "/")
	for i, s := range segs {
		if s == "testdata" {
			dir = strings.Join(segs[:i], "/")
			if dir == "" {
				dir = "."
			}
			return dir
		}
	}
	return dir
}
