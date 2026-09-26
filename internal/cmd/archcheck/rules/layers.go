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

import "strings"

// Layer groups the packages that share one set of dependency-direction
// rules (design.md §3 names four: contracts, the application package
// `migration`, external adapter modules and "every nested module").
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

// ContractLayer is the layer named "Contract packages" in design.md §3:
// tenancy, command, persistence, offsetstore, projection, eventstream,
// encryption, eventadapter, and every package under port/.
var ContractLayer = Layer{
	Name: "contract packages",
	Match: func(pkg Package) bool {
		if pkg.Kind != RootModule {
			return false
		}
		rel := strings.TrimPrefix(pkg.ImportPath, rootModulePath+"/")
		if rel == pkg.ImportPath {
			// pkg.ImportPath does not start with rootModulePath+"/": either
			// it is the root package itself (never a contract) or it is not
			// a root-module package at all.
			return false
		}
		return isContractRelPath(rel)
	},
}

// ApplicationLayer is the layer named "Application: migration" in
// design.md §3: the root-module package migration and its subpackages.
var ApplicationLayer = Layer{
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

// ExternalAdapterLayer is the layer named "Nested adapter modules:
// publisher/*" in design.md §3: every package in a nested module rooted
// under publisher/.
var ExternalAdapterLayer = Layer{
	Name: "external adapter modules (publisher/*)",
	Match: func(pkg Package) bool {
		if pkg.Kind != NestedModule {
			return false
		}
		return hasPathOrSubpath(pkg.ImportPath, rootModulePath+"/publisher")
	},
}

// AnyNestedModuleLayer is "every nested module" in design.md §3: it backs
// the no-cross-module-internal rule, which applies to every nested module,
// not only publisher/*.
var AnyNestedModuleLayer = Layer{
	Name: "every nested module",
	Match: func(pkg Package) bool {
		return pkg.Kind == NestedModule
	},
}
