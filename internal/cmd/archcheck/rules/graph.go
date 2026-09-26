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

// Package rules is the pure rule engine behind internal/cmd/archcheck: given
// an import graph, a table of layer rules and a baseline of known
// violations, it decides which import edges break a rule and which baseline
// entries no longer match anything real. Nothing here execs a process or
// touches the filesystem, so it is unit tested with in-memory fixtures. The
// internal/cmd/archcheck command wires this package to `go list -e -json`
// (root module) and `go/parser` (nested modules); see
// odd/tasks/arch-boundary-check.md and openspec/changes/ego-arch-001/design.md
// §3 for the rules this package encodes.
package rules

import "strings"

// rootModulePath is the import path of the repository's root Go module.
// The layer predicates below are root-module-specific (design.md §3 states
// the rules for this module only), so they refer to it directly rather than
// threading it through every call.
const rootModulePath = "github.com/pablogore/ego/v4"

// ModuleKind distinguishes a package that lives in the root module from one
// that lives in a nested module (its own go.mod under the repository, e.g.
// publisher/kafka).
type ModuleKind int

const (
	// RootModule is the module rooted at the repository root
	// (github.com/pablogore/ego/v4).
	RootModule ModuleKind = iota
	// NestedModule is any module with its own go.mod under the repository
	// (publisher/*, benchmark, example/cluster).
	NestedModule
)

// String renders k for diagnostics and test failure messages.
func (k ModuleKind) String() string {
	switch k {
	case RootModule:
		return "root"
	case NestedModule:
		return "nested"
	default:
		return "unknown"
	}
}

// Package is one node in the import graph Evaluate walks: a single Go
// package's identity, the module it belongs to, and its production
// (non-test) imports. Loaders (internal/cmd/archcheck/main.go) build these
// from `go list -e -json` for the root module and from `go/parser` for
// nested modules; only production imports are ever placed in Imports, so
// this package never sees a _test.go-only edge.
type Package struct {
	// ImportPath is the package's full import path, e.g.
	// "github.com/pablogore/ego/v4/tenancy" or
	// "github.com/pablogore/ego/v4/publisher/kafka".
	ImportPath string
	// Kind is which module ImportPath belongs to.
	Kind ModuleKind
	// Imports are the import paths this package's production (non-test)
	// files import: standard library, other first-party packages (root or
	// nested module) and third-party packages alike.
	Imports []string
}

// Graph is the whole import graph Evaluate checks: every package considered
// in a single archcheck run, root module and nested modules together.
type Graph struct {
	Packages []Package
}

// IsStdlib reports whether importPath is a standard-library import: an
// import path whose first path segment contains no dot. This mirrors how
// the Go toolchain itself tells first-party/third-party module paths
// (always dotted, e.g. "github.com", "google.golang.org") apart from
// standard-library paths (never dotted, e.g. "fmt", "os/exec", "context").
// Stdlib is always allowed by every rule in this package; Evaluate never
// calls a Rule's Forbids func for a stdlib import.
func IsStdlib(importPath string) bool {
	first := importPath
	if idx := strings.IndexByte(importPath, '/'); idx >= 0 {
		first = importPath[:idx]
	}
	return !strings.Contains(first, ".")
}

// hasPathOrSubpath reports whether importPath is exactly base, or a
// subpackage of base (base followed by "/").
func hasPathOrSubpath(importPath, base string) bool {
	return importPath == base || strings.HasPrefix(importPath, base+"/")
}
