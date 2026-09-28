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

import "fmt"

// releasedSet validates dirs — the module directories an operator listed
// as released — against g and returns them as a set. Two things make a
// released list invalid:
//
//   - a listed directory that discoverGraph never found a go.mod in
//     ("missing listed dir");
//   - a released module whose in-repository require is not itself in
//     dirs ("released module requires unreleased module", D5): the
//     released set is an explicit, reviewable input, never inferred, so
//     a gap between what is released and what a released module actually
//     needs must fail loudly here rather than surface later as a broken
//     build for whoever consumes the release.
func releasedSet(g *Graph, dirs []string) (map[string]bool, error) {
	set := make(map[string]bool, len(dirs))
	for _, dir := range dirs {
		if _, ok := g.ByDir(dir); !ok {
			return nil, fmt.Errorf("released module directory %q has no go.mod", dir)
		}
		set[dir] = true
	}

	for _, dir := range dirs {
		mod, _ := g.ByDir(dir)
		for _, reqPath := range mod.Requires {
			depDir, ok := g.DirOfPath(reqPath)
			if !ok {
				// A require on a module path this graph never discovered
				// under repo-root is not an in-repository edge (should not
				// happen: Module.Requires only ever holds discovered
				// paths), so it is not this check's concern.
				continue
			}
			if !set[depDir] {
				return nil, fmt.Errorf(
					"released module %s (%s) requires unreleased in-repository module %s (%s); add it to the release list or drop the requirement",
					dir, mod.Path, depDir, reqPath,
				)
			}
		}
	}

	return set, nil
}
