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

package rules

import (
	"strconv"
	"strings"
)

// Layer groups the packages that share one set of dependency-direction
// rules (design.md §3 names four: contracts, the application package
// `migration`, external adapter modules and "every nested module";
// ego-arch-003/design.md §D8 adds the composition packages and the
// packages that must not import them; ego-arch-006 slice S1 widens "every
// nested module" to every module and adds the module graph itself).
type Layer struct {
	// Name identifies the layer in reports, e.g. "contract packages".
	Name string
	// Match reports whether pkg belongs to this layer.
	Match func(pkg Package) bool
}

// contractRoots are the package-path segments (relative to the root
// module) that name a contract package, per design.md §3: the eight named
// contracts plus every package under port/. A contract package's
// subpackages are contracts too, except the carve-out in
// isContractRelPath below.
var contractRoots = []string{
	"tenancy",
	"command",
	"persistence",
	"offsetstore",
	"projection",
	"eventstream",
	"encryption",
	"eventadapter",
	"port",
}

// isContractRelPath reports whether rel — a root-module import path with
// the module prefix already stripped — is a contract package.
// persistence/conformance is test support, not a contract (design.md §3
// excludes test-support packages from every layer), so it is carved out
// even though it would otherwise match as a subpackage of persistence.
func isContractRelPath(rel string) bool {
	if hasPathOrSubpath(rel, "persistence/conformance") {
		return false
	}
	for _, root := range contractRoots {
		if hasPathOrSubpath(rel, root) {
			return true
		}
	}
	return false
}

// ContractLayer returns the layer named "Contract packages" in design.md
// §3: tenancy, command, persistence, offsetstore, projection, eventstream,
// encryption, eventadapter, and every package under port/. rootModulePath
// is the repository's root Go module's import path, read from its go.mod
// (see DefaultRules); the layer is root-module-specific, so every Rule
// built from it takes rootModulePath explicitly rather than assuming a
// hard-coded value.
func ContractLayer(rootModulePath string) Layer {
	return Layer{
		Name: "contract packages",
		Match: func(pkg Package) bool {
			if pkg.Kind != RootModule {
				return false
			}
			rel := strings.TrimPrefix(pkg.ImportPath, rootModulePath+"/")
			if rel == pkg.ImportPath {
				// pkg.ImportPath does not start with rootModulePath+"/":
				// either it is the root package itself (never a contract)
				// or it is not a root-module package at all.
				return false
			}
			return isContractRelPath(rel)
		},
	}
}

// ApplicationLayer returns the layer named "Application: migration" in
// design.md §3: the root-module package migration and its subpackages.
func ApplicationLayer(rootModulePath string) Layer {
	return Layer{
		Name: "application package migration",
		Match: func(pkg Package) bool {
			if pkg.Kind != RootModule {
				return false
			}
			rel := strings.TrimPrefix(pkg.ImportPath, rootModulePath+"/")
			if rel == pkg.ImportPath {
				return false
			}
			return hasPathOrSubpath(rel, "migration")
		},
	}
}

// repoPathFromModule returns rootModulePath with a trailing Go major-version
// path element ("/vN", N >= 2, e.g. "/v4") removed, or rootModulePath
// unchanged when it has none. Since D1 (#134), a nested module under
// publisher/ keeps the root's major-version-free repository identity in its
// own module path (e.g. "github.com/getsyntegrity/ego/publisher/kafka"): it
// never shares the root module's own "/vN" path
// ("github.com/getsyntegrity/ego"), so a rule that matches publisher
// packages by prefix must match against the repository path, not the root
// module path.
func repoPathFromModule(rootModulePath string) string {
	i := strings.LastIndexByte(rootModulePath, '/')
	if i < 0 {
		return rootModulePath
	}
	last := rootModulePath[i+1:]
	if len(last) < 2 || last[0] != 'v' {
		return rootModulePath
	}
	if n, err := strconv.Atoi(last[1:]); err != nil || n < 2 {
		return rootModulePath
	}
	return rootModulePath[:i]
}

// ExternalAdapterLayer returns the layer named "Nested adapter modules:
// publisher/*" in design.md §3: every package in a nested module rooted
// under publisher/. It matches by repository path (see repoPathFromModule),
// not by rootModulePath, because publisher modules do not carry the root
// module's own "/vN" suffix.
func ExternalAdapterLayer(rootModulePath string) Layer {
	repoPath := repoPathFromModule(rootModulePath)
	return Layer{
		Name: "external adapter modules (publisher/*)",
		Match: func(pkg Package) bool {
			if pkg.Kind != NestedModule {
				return false
			}
			return hasPathOrSubpath(pkg.ImportPath, repoPath+"/publisher")
		},
	}
}

// AnyModuleLayer is every package of every in-repository module, root and
// nested. It backs no-cross-module-internal, which design.md §3 first
// applied to "every nested module" importing the root's internal/ packages
// and ego-arch-006 (slice S1) generalized to any pair of modules.
var AnyModuleLayer = Layer{
	Name: "every module (root and nested)",
	Match: func(Package) bool {
		return true
	},
}

// ModuleGraphLayer names what no-module-cycle checks: the in-repository
// module graph itself, not packages. Evaluate never calls its Match; a
// rule with CheckModules is evaluated over Graph.Modules instead.
var ModuleGraphLayer = Layer{
	Name: "every in-repository module (go.mod requirements)",
	Match: func(Package) bool {
		return false
	},
}

// isCompositionImport reports whether importPath is the composition
// package or anything under it (compose, compose/goakt,
// compose/internal/lifecycle, ...), matched by whole path segment.
func isCompositionImport(rootModulePath, importPath string) bool {
	return hasPathOrSubpath(importPath, rootModulePath+"/compose")
}

// CompositionLayer returns the layer ego-arch-003 design.md §D8 names for
// the runtime-neutral half of the composition root: the root-module
// package compose and everything under compose/internal/ (the lifecycle
// sequencer). It deliberately excludes runtime-specific composition roots
// such as compose/goakt, which may import the runtime they wire. It is a
// layer of its own, not part of ApplicationLayer: compose is the
// composition root, not the Application layer of ego-arch-001 §3.
func CompositionLayer(rootModulePath string) Layer {
	return Layer{
		Name: "runtime-neutral composition packages (compose, compose/internal/...)",
		Match: func(pkg Package) bool {
			if pkg.Kind != RootModule {
				return false
			}
			return pkg.ImportPath == rootModulePath+"/compose" ||
				hasPathOrSubpath(pkg.ImportPath, rootModulePath+"/compose/internal")
		},
	}
}

// CompositionLeafLayer returns the layer the composition-leaf rule applies
// to (ego-arch-003 design.md §D8): every root-module production package
// that is not under compose/, is not a main package, and is not under
// example/. Those are the composition root's legitimate consumers, and so
// are test files and the benchmark module, which never reach this layer:
// loaders record production imports only, and benchmark is a nested
// module. A package whose name is unknown (empty) counts as not main, so
// the layer fails closed.
func CompositionLeafLayer(rootModulePath string) Layer {
	return Layer{
		Name: "root-module production packages outside compose/ (except main packages and examples)",
		Match: func(pkg Package) bool {
			if pkg.Kind != RootModule || pkg.Name == "main" {
				return false
			}
			if !hasPathOrSubpath(pkg.ImportPath, rootModulePath) {
				// Not a package of this root module at all.
				return false
			}
			if isCompositionImport(rootModulePath, pkg.ImportPath) {
				return false
			}
			return !hasPathOrSubpath(pkg.ImportPath, rootModulePath+"/example")
		},
	}
}
