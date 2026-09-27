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
	"sort"
	"strings"
)

// Module selection (ego-arch-006 design §5.2). Modules are nodes, and a
// module's in-repository go.mod requirements resolved from the working
// tree (ModuleInfo.Deps) are its edges. A change selects the modules that
// contain it plus every module that transitively requires one of them.

// rootDir is the root module's directory in ModuleInfo.Dir.
const rootDir = "."

// chainSep joins a ModulePlan.Chain for display.
const chainSep = " ← "

// globalExactFiles and globalPrefixDirs are the global paths of the design
// (§5.3): a change to any of them selects every module and forces the root
// lane to full. The root go.mod and go.sum are deliberately not global:
// they fully change the root module, and the closure then selects every
// module that requires the root.
var globalExactFiles = map[string]bool{
	"go.work":       true,
	"go.work.sum":   true,
	".golangci.yml": true,
	"Makefile":      true,
	"Dockerfile.ci": true,
	"buf.yaml":      true,
	"buf.gen.yaml":  true,
}

var globalPrefixDirs = []string{
	".github",
	"scripts/ci",
	"internal/cmd/ciselect",
	"protos",
}

// globalReason returns why every module must be selected, or "" when no
// global condition applies.
func globalReason(opts Options, changed []string) string {
	if opts.All {
		return "global: -all requested"
	}
	if len(changed) == 0 {
		return "global: no changed files detected"
	}
	for _, c := range changed {
		sp := normalizeChangedPath(c)
		if globalExactFiles[sp] {
			return fmt.Sprintf("global: %s changed", sp)
		}
		if _, ok := matchingPrefix(sp, globalPrefixDirs); ok {
			return fmt.Sprintf("global: %s changed", sp)
		}
	}
	return ""
}

// moduleGraph indexes Options.Modules.
type moduleGraph struct {
	byDir      map[string]ModuleInfo
	dirOfPath  map[string]string
	dependents map[string][]string // dir -> sorted dirs of modules requiring it
	dirs       []string            // root first, then sorted
}

func newModuleGraph(modules []ModuleInfo) moduleGraph {
	mg := moduleGraph{
		byDir:      make(map[string]ModuleInfo, len(modules)),
		dirOfPath:  make(map[string]string, len(modules)),
		dependents: make(map[string][]string),
	}
	for _, m := range modules {
		mg.byDir[m.Dir] = m
		if m.Path != "" {
			mg.dirOfPath[m.Path] = m.Dir
		}
		mg.dirs = append(mg.dirs, m.Dir)
	}
	sort.Slice(mg.dirs, func(i, j int) bool { return lessDir(mg.dirs[i], mg.dirs[j]) })
	for _, m := range modules {
		for _, dep := range m.Deps {
			if d, ok := mg.dirOfPath[dep]; ok && d != m.Dir {
				mg.dependents[d] = append(mg.dependents[d], m.Dir)
			}
		}
	}
	for d := range mg.dependents {
		sort.Strings(mg.dependents[d])
	}
	return mg
}

// lessDir orders module directories root first, then lexically.
func lessDir(a, b string) bool {
	if a == rootDir || b == rootDir {
		return a == rootDir && b != rootDir
	}
	return a < b
}

// nestedDirs returns every module directory except the root.
func (mg moduleGraph) nestedDirs() []string {
	var out []string
	for _, d := range mg.dirs {
		if d != rootDir {
			out = append(out, d)
		}
	}
	return out
}

// ownerOf returns the module directory owning the repo-relative path sp:
// the nested module with the longest directory prefix, or the root. Every
// path belongs to some module, because the root module's directory is the
// repository root.
func (mg moduleGraph) ownerOf(sp string) string {
	return mg.ownerExcluding(sp, "")
}

// ownerExcluding is ownerOf ignoring the module at directory skip, so
// ownerExcluding(dir, dir) is the module that owns dir when dir's own
// go.mod is absent: its parent module.
func (mg moduleGraph) ownerExcluding(sp, skip string) string {
	best := rootDir
	for _, d := range mg.nestedDirs() {
		if d == skip || !hasPathPrefix(sp, d) {
			continue
		}
		if best == rootDir || len(d) > len(best) {
			best = d
		}
	}
	return best
}

// moduleOfImport returns the directory of the in-repository module that
// provides import path imp (the longest module path prefix), if any.
func (mg moduleGraph) moduleOfImport(imp string) (string, bool) {
	bestPath := ""
	for p := range mg.dirOfPath {
		if (imp == p || strings.HasPrefix(imp, p+"/")) && len(p) > len(bestPath) {
			bestPath = p
		}
	}
	if bestPath == "" {
		return "", false
	}
	return mg.dirOfPath[bestPath], true
}

// selectWithModules runs the whole algorithm: global check, ownership,
// boundary changes, the root lane, the reverse-transitive closure and the
// root-lane seeding through dependency modules.
func selectWithModules(g Graph, changed []string, opts Options) Result {
	mg := newModuleGraph(opts.Modules)
	in := rootInputs{satelliteDirs: append(append([]string{}, opts.SatelliteDirs...), mg.nestedDirs()...)}

	// 1. Global check.
	if reason := globalReason(opts, changed); reason != "" {
		in.forceFull = []string{reason}
		res := selectRoot(g, changed, opts, in)
		res.Global = true
		for _, d := range mg.dirs {
			res.Plan = append(res.Plan, ModulePlan{Dir: d, Path: mg.byDir[d].Path, Selected: true, Reason: reason, Chain: []string{d}})
		}
		res.Modules = nestedSelections(res.Plan)
		return res
	}

	// 2-3. Ownership and module boundary changes.
	st := walkState{
		mg:              mg,
		manifestChanged: map[string]bool{},
		fullyChanged:    map[string]bool{},
	}
	changedReasons := map[string][]string{} // nested dir -> reasons
	for _, c := range changed {
		sp := normalizeChangedPath(c)
		owner := mg.ownerOf(sp)
		if owner != rootDir {
			addReason(changedReasons, owner, fmt.Sprintf("changed files in %s", owner))
		}
		base, dir := path.Base(sp), path.Dir(sp)
		if (base == "go.mod" || base == "go.sum") && dir == owner {
			st.manifestChanged[owner] = true
		}
		if base == "go.mod" && dir != rootDir && isBoundaryChange(opts.GoMods, sp) {
			reason := fmt.Sprintf("module boundary changed: %s", sp)
			if parent := mg.ownerExcluding(dir, dir); parent == rootDir {
				in.forceFull = append(in.forceFull, reason)
			} else {
				addReason(changedReasons, parent, reason)
				st.fullyChanged[parent] = true
			}
		}
	}

	st.root = selectRoot(g, changed, opts, in)
	_, hasRoot := mg.byDir[rootDir]

	// 4. Changed set.
	var start []string
	for d := range changedReasons {
		start = append(start, d)
	}
	if hasRoot && st.root.Mode != ModeNone {
		start = append(start, rootDir)
	}
	sort.Slice(start, func(i, j int) bool { return lessDir(start[i], start[j]) })

	// 5-6. Closure with the import filter, and root-lane seeding through a
	// module the root requires. Seeding can widen the root lane, which can
	// widen the closure from the root, so repeat until the root lane is
	// stable. Both only grow, so this ends; the bound is defensive.
	var chains map[string][]string
	for i := 0; i <= len(mg.dirs); i++ {
		chains = st.closure(start)
		if _, reached := chains[rootDir]; !reached || !hasRoot {
			break
		}
		seeded, ok := st.seedRoot(g, chains, in)
		if !ok {
			break
		}
		next := selectRoot(g, changed, opts, seeded)
		if next.Mode == st.root.Mode && len(next.Selected) == len(st.root.Selected) {
			st.root = next
			break
		}
		st.root = next
	}
	// Recompute the closure against the final root lane, so chains can
	// never lag one iteration behind it, even if the defensive bound were
	// hit. On every normal exit this reproduces the same chains.
	chains = st.closure(start)
	root := st.root

	rootReason := strings.Join(root.Reasons, "; ")
	for _, d := range mg.dirs {
		m := mg.byDir[d]
		p := ModulePlan{Dir: d, Path: m.Path}
		if chain, ok := chains[d]; ok {
			p.Selected = true
			p.Chain = chain
			switch {
			case len(chain) > 1:
				p.Reason = strings.Join(chain, chainSep)
			case d == rootDir:
				p.Reason = rootReason
			default:
				p.Reason = strings.Join(changedReasons[d], "; ")
			}
		} else {
			p.Reason = unselectedReason(m, mg, chains)
			if p.Reason == notAffected {
				if d := st.filteredDep(m, chains); d != "" {
					p.Reason = fmt.Sprintf("not affected: requires %s but imports none of its affected packages", d)
				}
			}
		}
		root.Plan = append(root.Plan, p)
	}
	root.Modules = nestedSelections(root.Plan)
	return root
}

// notAffected is the reason of a module nothing reached.
const notAffected = "not affected"

// isBoundaryChange reports whether the changed nested go.mod at sp is a
// module boundary change: added or deleted (present at exactly one of base
// and head). Without base information (goMods nil, or sp not in it) every
// changed nested go.mod is a boundary change, the conservative fallback.
func isBoundaryChange(goMods map[string]GoModPresence, sp string) bool {
	if goMods == nil {
		return true
	}
	p, ok := goMods[sp]
	if !ok {
		return true
	}
	return !p.AtBase || !p.AtHead
}

// walkState is what the closure needs to decide each requirement edge.
type walkState struct {
	mg moduleGraph
	// root is the current root lane decision.
	root Result
	// manifestChanged marks modules whose go.mod or go.sum changed.
	manifestChanged map[string]bool
	// fullyChanged marks nested modules that are the parent of a module
	// boundary change.
	fullyChanged map[string]bool
}

// unfiltered reports whether every module requiring d follows it, without
// the import filter: d's manifest changed, d is fully changed, or d is the
// root with lane full (§5.1 "When the import filter is sound").
func (st walkState) unfiltered(d string) bool {
	return st.manifestChanged[d] || st.fullyChanged[d] || (d == rootDir && st.root.Mode == ModeFull)
}

// edge reports whether consumer m is reached from the reached module d it
// requires: unfiltered, or m imports one of d's affected packages. Every
// package of a nested module counts as affected; the root's affected
// packages are its lane's Selected set.
func (st walkState) edge(m ModuleInfo, d string) bool {
	if st.unfiltered(d) {
		return true
	}
	if d == rootDir {
		selected := make(map[string]bool, len(st.root.Selected))
		for _, p := range st.root.Selected {
			selected[p] = true
		}
		for _, imp := range m.Imports {
			if selected[imp] {
				return true
			}
		}
		return false
	}
	for _, imp := range m.Imports {
		if owner, ok := st.mg.moduleOfImport(imp); ok && owner == d {
			return true
		}
	}
	return false
}

// closure walks the reversed Deps edges breadth-first from start (already
// sorted), visiting dependents in sorted order and following an edge only
// when st.edge allows it, and returns the first chain that reached each
// module: the module itself, then back to a start one.
func (st walkState) closure(start []string) map[string][]string {
	chains := make(map[string][]string, len(start))
	queue := make([]string, 0, len(start))
	for _, d := range start {
		if _, seen := chains[d]; seen {
			continue
		}
		chains[d] = []string{d}
		queue = append(queue, d)
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, dep := range st.mg.dependents[cur] {
			if _, seen := chains[dep]; seen {
				continue
			}
			if !st.edge(st.mg.byDir[dep], cur) {
				continue
			}
			chains[dep] = append([]string{dep}, chains[cur]...)
			queue = append(queue, dep)
		}
	}
	return chains
}

// filteredDep returns the directory of the first (sorted) reached module m
// requires whose edge the import filter rejected, or "".
func (st walkState) filteredDep(m ModuleInfo, chains map[string][]string) string {
	var deps []string
	for _, dep := range m.Deps {
		if d, ok := st.mg.dirOfPath[dep]; ok {
			if _, reached := chains[d]; reached {
				deps = append(deps, d)
			}
		}
	}
	sort.Slice(deps, func(i, j int) bool { return lessDir(deps[i], deps[j]) })
	if len(deps) == 0 {
		return ""
	}
	return deps[0]
}

// seedRoot returns the root lane inputs for a root reached through modules
// it requires: the root packages that import a reached dependency become
// seeds of the package-level lane; a dependency whose go.mod or go.sum
// changed, or that no root package in the go list graph imports (its only
// importer sits behind a build tag), forces the root lane to full. Only
// dependencies whose edge to the root passes the import filter count. ok
// is false when no such dependency exists (the root only changed itself).
func (st walkState) seedRoot(g Graph, chains map[string][]string, in rootInputs) (rootInputs, bool) {
	mg := st.mg
	manifestChanged := st.manifestChanged
	var reachedDeps []string
	for _, dep := range mg.byDir[rootDir].Deps {
		if d, ok := mg.dirOfPath[dep]; ok && d != rootDir {
			if _, reached := chains[d]; reached && st.edge(mg.byDir[rootDir], d) {
				reachedDeps = append(reachedDeps, d)
			}
		}
	}
	if len(reachedDeps) == 0 {
		return in, false
	}
	sort.Strings(reachedDeps)

	out := in
	out.forceFull = append([]string{}, in.forceFull...)
	out.seeds = map[string]bool{}
	for _, d := range reachedDeps {
		depPath := mg.byDir[d].Path
		if manifestChanged[d] {
			out.forceFull = append(out.forceFull, fmt.Sprintf("required module %s changed its go.mod or go.sum", d))
			continue
		}
		importers := mg.rootImportersOf(g, d)
		if len(importers) == 0 {
			out.forceFull = append(out.forceFull, fmt.Sprintf("root requires %s but no root package imports it: full-suite fallback", depPath))
			continue
		}
		for _, p := range importers {
			out.seeds[p] = true
		}
		out.seedReasons = append(out.seedReasons, fmt.Sprintf("seeded from %d root package(s) importing required module %s", len(importers), d))
	}
	return out, true
}

// rootImportersOf returns the sorted root packages whose Imports,
// TestImports or XTestImports name a package of the module at dir.
func (mg moduleGraph) rootImportersOf(g Graph, dir string) []string {
	var out []string
	for _, p := range g.Packages {
		if mg.importsModule(p, dir) {
			out = append(out, p.ImportPath)
		}
	}
	sort.Strings(out)
	return out
}

func (mg moduleGraph) importsModule(p Package, dir string) bool {
	for _, list := range [][]string{p.Imports, p.TestImports, p.XTestImports} {
		for _, imp := range list {
			if d, ok := mg.moduleOfImport(imp); ok && d == dir {
				return true
			}
		}
	}
	return false
}

// unselectedReason explains why module m was not selected: pinned to a
// published version of a module that was reached, or simply not affected.
func unselectedReason(m ModuleInfo, mg moduleGraph, chains map[string][]string) string {
	for _, pin := range m.Pinned {
		depPath := pin
		if i := strings.LastIndex(pin, "@"); i >= 0 {
			depPath = pin[:i]
		}
		if d, ok := mg.dirOfPath[depPath]; ok {
			if _, reached := chains[d]; reached {
				return fmt.Sprintf("pinned to %s; not affected at HEAD", pin)
			}
		}
	}
	return notAffected
}

// nestedSelections returns the selected nested (non-root) modules of plan,
// sorted by Dir.
func nestedSelections(plan []ModulePlan) []ModuleSelection {
	var out []ModuleSelection
	for _, p := range plan {
		if p.Selected && p.Dir != rootDir {
			out = append(out, ModuleSelection{Dir: p.Dir, Reason: p.Reason, Chain: p.Chain})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out
}

func addReason(m map[string][]string, dir, reason string) {
	for _, r := range m[dir] {
		if r == reason {
			return
		}
	}
	m[dir] = append(m[dir], reason)
}
