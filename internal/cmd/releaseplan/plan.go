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
	"encoding/json"
	"fmt"
	"strings"
)

// PlanModule is one released module's place in the plan: its directory
// and module path, its current tag (empty if none exists yet), the next
// tag releaseplan computed for it, and the released module paths it
// requires (already ordered before it).
type PlanModule struct {
	Dir        string   `json:"dir"`
	Path       string   `json:"path"`
	CurrentTag string   `json:"currentTag,omitempty"`
	NextTag    string   `json:"nextTag"`
	Requires   []string `json:"requires"`
}

// Plan is the whole release plan: every released module, in release
// order.
type Plan struct {
	Bump    string       `json:"bump"`
	Modules []PlanModule `json:"modules"`
}

// buildPlan validates released (module directories, D5's explicit,
// reviewable released-module list) against g, orders them (D3), and
// computes each one's next tag (D2 (a)) against tags for the requested
// bumpKind. It returns the first error encountered: a cycle anywhere in
// the discovered graph, an invalid released list (releasedSet), or a
// major-version refusal (nextTag) — releaseplan never proceeds past a
// refusal to compute the rest of the plan, so a build failure is always
// unambiguous about what to fix first.
func buildPlan(g *Graph, releaseDirs []string, tags []string, bumpKind string) (Plan, error) {
	if err := detectCycle(g); err != nil {
		return Plan{}, err
	}

	released, err := releasedSet(g, releaseDirs)
	if err != nil {
		return Plan{}, err
	}

	order := releaseOrder(g, released)
	modules := make([]PlanModule, 0, len(order))
	for _, dir := range order {
		mod, _ := g.ByDir(dir)

		var requires []string
		for _, reqPath := range mod.Requires {
			if depDir, ok := g.DirOfPath(reqPath); ok && released[depDir] {
				requires = append(requires, reqPath)
			}
		}

		currentTag, next, err := nextTag(dir, mod.Path, tags, bumpKind)
		if err != nil {
			return Plan{}, err
		}

		modules = append(modules, PlanModule{
			Dir:        dir,
			Path:       mod.Path,
			CurrentTag: currentTag,
			NextTag:    tagPrefix(dir) + next.String(),
			Requires:   requires,
		})
	}

	return Plan{Bump: bumpKind, Modules: modules}, nil
}

// planDoc is plan.json's shape: Modules is always a JSON array, never
// null, and neither is any module's Requires — an empty plan or an empty
// dependency list must still decode cleanly wherever build.yml or a
// human reads it.
type planDoc struct {
	Bump    string       `json:"bump"`
	Modules []PlanModule `json:"modules"`
}

func renderPlanJSON(plan Plan) (string, error) {
	doc := planDoc{Bump: plan.Bump, Modules: make([]PlanModule, len(plan.Modules))}
	for i, m := range plan.Modules {
		m.Requires = nonNilStrings(m.Requires)
		doc.Modules[i] = m
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// renderSummary renders plan as a markdown table for the CI job summary
// and stdout: release order, current tag, next tag and in-repository
// requires for every released module.
func renderSummary(plan Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Release plan (dry run)\n\n")
	fmt.Fprintf(&b, "Bump: `%s`\n\n", plan.Bump)

	if len(plan.Modules) == 0 {
		b.WriteString("No released modules.\n")
		return b.String()
	}

	b.WriteString("| Order | Directory | Module path | Current tag | Next tag | Requires |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for i, m := range plan.Modules {
		current := m.CurrentTag
		if current == "" {
			current = "(none)"
		}
		requires := "(none)"
		if len(m.Requires) > 0 {
			requires = strings.Join(m.Requires, ", ")
		}
		fmt.Fprintf(&b, "| %d | `%s` | `%s` | `%s` | `%s` | %s |\n", i+1, m.Dir, m.Path, current, m.NextTag, requires)
	}
	return b.String()
}
