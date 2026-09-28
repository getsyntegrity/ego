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
	"sort"
	"strings"
)

// ModuleIndex answers "which in-repository module owns this import path?"
// for the module-aware rules. A path belongs to the module with the longest
// module path that equals it or prefixes it at a path-segment boundary,
// which is how the go command itself assigns a directory to its nearest
// go.mod: github.com/getsyntegrity/ego/publisher/kafka/internal/x belongs to
// the publisher/kafka module, not to the root module whose path also
// prefixes it.
type ModuleIndex struct {
	// paths is sorted longest first, so Owner returns the first match.
	paths []string
}

// NewModuleIndex indexes modules by path.
func NewModuleIndex(modules []Module) ModuleIndex {
	paths := make([]string, 0, len(modules))
	for _, m := range modules {
		paths = append(paths, m.Path)
	}
	sort.Slice(paths, func(i, j int) bool {
		if len(paths[i]) != len(paths[j]) {
			return len(paths[i]) > len(paths[j])
		}
		return paths[i] < paths[j]
	})
	return ModuleIndex{paths: paths}
}

// Owner returns the path of the in-repository module that owns importPath,
// or "" when none does (a third-party or standard-library path).
func (idx ModuleIndex) Owner(importPath string) string {
	for _, p := range idx.paths {
		if hasPathOrSubpath(importPath, p) {
			return p
		}
	}
	return ""
}

// hasInternalElement reports whether importPath has a path element named
// "internal", the element the go command's internal-package rule keys on.
func hasInternalElement(importPath string) bool {
	for _, elem := range strings.Split(importPath, "/") {
		if elem == "internal" {
			return true
		}
	}
	return false
}

// crossModuleInternal is no-cross-module-internal's decision: importer may
// not import an internal/ package owned by a different in-repository
// module. The go command does not stop this on its own, because every
// module here shares the root module's path prefix, and its internal rule
// only looks at import paths (openspec/changes/ego-arch-006/design.md §7).
// An internal/ path that no in-repository module owns is left to the go
// command.
func crossModuleInternal(modules ModuleIndex, importer, importPath string) (bool, string) {
	if !hasInternalElement(importPath) {
		return false, ""
	}
	owner := modules.Owner(importPath)
	if owner == "" {
		return false, ""
	}
	from := modules.Owner(importer)
	if owner == from {
		return false, ""
	}
	return true, "imports " + importPath + ", an internal/ package of module " + owner +
		", from module " + from + ": internal/ packages must stay inside the module that owns them"
}

// moduleCycleViolations is no-module-cycle's check. It reports every
// in-repository requirement edge that lies on a cycle, one Violation per
// edge (Importer is the requiring module, Import the required one), so a
// baseline entry can name a single edge exactly as it names an import edge.
// An edge that only leads into a cycle is not reported. Requirements naming
// a module outside modules are ignored.
func moduleCycleViolations(modules []Module) []Violation {
	known := make(map[string]bool, len(modules))
	for _, m := range modules {
		known[m.Path] = true
	}
	edges := make(map[string][]string, len(modules))
	nodes := make([]string, 0, len(modules))
	for _, m := range modules {
		nodes = append(nodes, m.Path)
		for _, req := range m.Requires {
			if known[req] {
				edges[m.Path] = append(edges[m.Path], req)
			}
		}
		sort.Strings(edges[m.Path])
	}
	sort.Strings(nodes)

	component := stronglyConnectedComponents(nodes, edges)
	size := make(map[int]int, len(component))
	for _, c := range component {
		size[c]++
	}

	var violations []Violation
	for _, from := range nodes {
		for _, to := range edges[from] {
			onCycle := from == to || (component[from] == component[to] && size[component[from]] > 1)
			if !onCycle {
				continue
			}
			violations = append(violations, Violation{
				Importer: from,
				Import:   to,
				Rule:     "no-module-cycle",
				Reason:   "requires " + to + ", which requires it back: " + strings.Join(cyclePath(edges, from, to), " -> "),
			})
		}
	}
	return violations
}

// cyclePath returns the shortest requirement path from -> to -> ... -> from,
// found breadth first in sorted order so the text is deterministic. The
// caller guarantees the edge from -> to lies on a cycle.
func cyclePath(edges map[string][]string, from, to string) []string {
	prev := map[string]string{to: ""}
	queue := []string{to}
	for len(queue) > 0 && from != to {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range edges[cur] {
			if _, seen := prev[next]; seen {
				continue
			}
			prev[next] = cur
			if next == from {
				queue = nil
				break
			}
			queue = append(queue, next)
		}
	}
	// Walk back from `from` to `to`, then prepend the starting node.
	var back []string
	for cur := from; cur != to; cur = prev[cur] {
		back = append(back, cur)
	}
	path := []string{from, to}
	for i := len(back) - 1; i >= 0; i-- {
		path = append(path, back[i])
	}
	return path
}

// stronglyConnectedComponents labels every node with the index of its
// strongly connected component (Tarjan's algorithm). Two nodes share a
// label exactly when each can reach the other.
func stronglyConnectedComponents(nodes []string, edges map[string][]string) map[string]int {
	var (
		index    = make(map[string]int, len(nodes))
		lowlink  = make(map[string]int, len(nodes))
		onStack  = make(map[string]bool, len(nodes))
		label    = make(map[string]int, len(nodes))
		stack    []string
		next     int
		nextComp int
		visit    func(string)
	)
	visit = func(v string) {
		index[v] = next
		lowlink[v] = next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range edges[v] {
			if _, seen := index[w]; !seen {
				visit(w)
				lowlink[v] = min(lowlink[v], lowlink[w])
			} else if onStack[w] {
				lowlink[v] = min(lowlink[v], index[w])
			}
		}
		if lowlink[v] == index[v] {
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				label[w] = nextComp
				if w == v {
					break
				}
			}
			nextComp++
		}
	}
	for _, v := range nodes {
		if _, seen := index[v]; !seen {
			visit(v)
		}
	}
	return label
}
