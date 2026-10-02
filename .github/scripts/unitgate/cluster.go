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
	"go/ast"
	"sort"
	"strings"
)

const (
	clusterTestPrefix = "TestCluster"
	dynaportPrefix    = "github.com/travisjeffery/go-dynaport"
	composeGoaktPath  = "github.com/getsyntegrity/urd/compose/goakt"
)

// clusterFindings reports the top-level tests of a test file that start a real cluster and are not named
// TestCluster*. CI selects the multi-node tests by that name (go test -run '^TestCluster' and
// -skip '^TestCluster'), so a cluster test with any other name would run in the normal lane.
//
// A test starts a cluster when its body (closures included) calls WithCluster from the goakt actor package
// or from compose/goakt, or any function of go-dynaport (the loopback ports a cluster node needs). The call
// can be direct, or go through a function or method declared in the SAME file, followed through any number of
// hops. A method call x.m() counts as a call to every method named m declared in the file, whatever the
// receiver, so the rule can over-report when two receivers share a method name.
//
// What it cannot see, by design, because it parses one file at a time:
//   - helpers declared in another file of the package (engine/engine_neutral_cluster_test.go reaches its
//     cluster through newTestCluster, declared in engine/engine_test.go, so that test is checked by review
//     only);
//   - calls through a function value stored in a variable, a field or an interface;
//   - a WithCluster that is not imported from the two packages above, such as a dot import or a re-export;
//   - the tests of compose/goakt itself: they are package goakt and call WithCluster unqualified, so the
//     compose/goakt branch never fires there. TestCluster_AppTwoNodePlacesAndStopsCleanly is caught through
//     dynaport.Get, but a cluster with fixed ports in that package would not be. Matching the unqualified
//     name instead would flag the app_test.go cases, which pass a cluster config GoAkt rejects before any
//     port opens and are single-node.
//
// The opposite direction is not checked: a TestCluster* test whose file shows no cluster setup is accepted.
// The setup may sit in a helper of another file (the case above), and a per-file syntactic rule cannot tell
// that from a missing setup, so reporting it would flag valid tests. The cost of a misnamed TestCluster* test
// is only that it runs in the cluster lane; it never hides a test, which is the failure this rule exists for.
func clusterFindings(p string, f *ast.File, imports map[string]string) []Finding {
	type fn struct {
		direct  string          // the first cluster call in the body, or ""
		callees map[string]bool // names called that may be declared in this file
	}
	imports = withPackageNames(imports)
	fns := map[string]*fn{}
	var tests []*ast.FuncDecl

	for _, decl := range f.Decls {
		d, ok := decl.(*ast.FuncDecl)
		if !ok || d.Body == nil {
			continue
		}
		info := fns[d.Name.Name]
		if info == nil {
			info = &fn{callees: map[string]bool{}}
			fns[d.Name.Name] = info
		}
		ast.Inspect(d.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if q := qualifiedCall(call, imports); q != "" {
				if r := clusterCall(q); r != "" && info.direct == "" {
					info.direct = r
				}
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				info.callees[fun.Name] = true
			case *ast.SelectorExpr:
				info.callees[fun.Sel.Name] = true
			}
			return true
		})
		if isTestFunc(d) {
			tests = append(tests, d)
		}
	}

	// reach finds the first cluster call below name, walking declared callees in name order.
	reach := func(name string) string {
		visited := map[string]bool{}
		var walk func(string) string
		walk = func(cur string) string {
			if visited[cur] {
				return ""
			}
			visited[cur] = true
			info := fns[cur]
			if info == nil {
				return ""
			}
			if info.direct != "" {
				return info.direct
			}
			for _, c := range sortedKeys(info.callees) {
				if r := walk(c); r != "" {
					return r
				}
			}
			return ""
		}
		return walk(name)
	}

	var out []Finding
	for _, t := range tests {
		name := t.Name.Name
		if strings.HasPrefix(name, clusterTestPrefix) {
			continue
		}
		info := fns[name]
		detail := ""
		if info.direct != "" {
			detail = name + ": calls " + info.direct
		} else {
			for _, c := range sortedKeys(info.callees) {
				if c == name {
					continue
				}
				if r := reach(c); r != "" {
					detail = name + ": calls " + r + " through " + c
					break
				}
			}
		}
		if detail != "" {
			out = append(out, Finding{Path: p, Rule: RuleCluster, Detail: detail + ", but a test that starts a cluster must be named TestCluster*"})
		}
	}
	return out
}

// clusterCall describes q ("<import path>.<Func>") when it starts a cluster, or returns "".
func clusterCall(q string) string {
	i := strings.LastIndexByte(q, '.')
	pkg, fn := q[:i], q[i+1:]
	switch {
	case strings.HasPrefix(pkg, goaktActorPrefix) && fn == "WithCluster":
		return "goakt actor.WithCluster"
	case pkg == composeGoaktPath && fn == "WithCluster":
		return "compose/goakt.WithCluster"
	case hasPathPrefix(pkg, dynaportPrefix):
		return "dynaport." + fn
	}
	return ""
}

// withPackageNames adds the name "dynaport" for the go-dynaport import. importNames derives a local name from
// the last element of the path ("go-dynaport"), but the package is declared as dynaport, which is how every
// file refers to it when the import has no alias.
func withPackageNames(imports map[string]string) map[string]string {
	out := make(map[string]string, len(imports)+1)
	for name, ip := range imports {
		out[name] = ip
		if hasPathPrefix(ip, dynaportPrefix) {
			if _, taken := imports["dynaport"]; !taken {
				out["dynaport"] = ip
			}
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
