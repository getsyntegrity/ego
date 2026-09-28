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
	"fmt"
	"sort"
	"strings"
)

// detectCycle reports an error naming the modules involved if the
// in-repository require edges of g (every discovered module, not only a
// released subset — D3 orders released modules by *all* declared
// requires, so a cycle anywhere in the graph makes that order
// impossible) contain a cycle. Traversal order is deterministic
// (directories visited lexicographically) so the same cycle is always
// reported the same way.
func detectCycle(g *Graph) error {
	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := make(map[string]int, len(g.Modules))
	var stack []string

	var visit func(dir string) error
	visit = func(dir string) error {
		switch state[dir] {
		case done:
			return nil
		case visiting:
			start := 0
			for i, d := range stack {
				if d == dir {
					start = i
					break
				}
			}
			cycle := append(append([]string{}, stack[start:]...), dir)
			return fmt.Errorf("cycle among discovered modules: %s", strings.Join(cycle, " -> "))
		}

		state[dir] = visiting
		stack = append(stack, dir)

		mod, _ := g.ByDir(dir)
		deps := make([]string, 0, len(mod.Requires))
		for _, reqPath := range mod.Requires {
			if depDir, ok := g.DirOfPath(reqPath); ok {
				deps = append(deps, depDir)
			}
		}
		sort.Strings(deps)
		for _, dep := range deps {
			if err := visit(dep); err != nil {
				return err
			}
		}

		stack = stack[:len(stack)-1]
		state[dir] = done
		return nil
	}

	dirs := make([]string, len(g.Modules))
	for i, m := range g.Modules {
		dirs[i] = m.Dir
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		if err := visit(dir); err != nil {
			return err
		}
	}
	return nil
}

// releaseOrder topologically sorts released (a set of module directories,
// already validated to exist and to have every in-repository require of
// each member also in the set) by their in-repository require edges: a
// module is ordered after every released module it requires (D3). Ties
// (several modules whose released requirements are already ordered)
// break on directory, lexicographically, so the order is deterministic.
// g must already be free of cycles (detectCycle).
func releaseOrder(g *Graph, released map[string]bool) []string {
	inDegree := make(map[string]int, len(released))
	dependents := make(map[string][]string, len(released))
	for dir := range released {
		inDegree[dir] = 0
	}
	for dir := range released {
		mod, _ := g.ByDir(dir)
		for _, reqPath := range mod.Requires {
			depDir, ok := g.DirOfPath(reqPath)
			if !ok || !released[depDir] {
				continue
			}
			inDegree[dir]++
			dependents[depDir] = append(dependents[depDir], dir)
		}
	}

	var ready []string
	for dir, n := range inDegree {
		if n == 0 {
			ready = append(ready, dir)
		}
	}
	sort.Strings(ready)

	order := make([]string, 0, len(released))
	for len(ready) > 0 {
		next := ready[0]
		ready = ready[1:]
		order = append(order, next)

		var newlyReady []string
		for _, dependent := range dependents[next] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				newlyReady = append(newlyReady, dependent)
			}
		}
		sort.Strings(newlyReady)
		ready = append(ready, newlyReady...)
		sort.Strings(ready)
	}

	return order
}
