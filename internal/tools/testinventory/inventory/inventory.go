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

package inventory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Header describes the machine and revision of the recorded run. It is the only
// place where environment-dependent data lives.
type Header struct {
	Generator   string `json:"generator"`
	GoVersion   string `json:"go_version"`
	GOOS        string `json:"goos"`
	GOARCH      string `json:"goarch"`
	CPUs        int    `json:"cpus"`
	Commit      string `json:"commit"`
	RunRecorded bool   `json:"run_recorded"`
}

// ModuleInfo lists a scanned module.
type ModuleInfo struct {
	Path    string `json:"path"`
	Dir     string `json:"dir"`
	Example bool   `json:"example,omitempty"`
}

// Inventory is the content of docs/testing/inventory.json.
type Inventory struct {
	Header   Header       `json:"header"`
	Modules  []ModuleInfo `json:"modules"`
	Packages []PackageRun `json:"packages,omitempty"`
	Tests    []Entry      `json:"tests"`
	// RunUnmatched lists tests the run reported that the static scan did not find.
	RunUnmatched []string `json:"run_unmatched,omitempty"`
}

// DiscoverModules finds every go.mod under root, skipping testdata, vendor and hidden
// directories. The root module comes first, then the rest sorted by directory.
func DiscoverModules(root string) ([]Module, error) {
	var mods []Module
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if p != root && (name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "go.mod" {
			return nil
		}
		modPath, err := readModulePath(p)
		if err != nil {
			return err
		}
		dir := filepath.Dir(p)
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return err
		}
		mods = append(mods, Module{Path: modPath, Dir: dir, Rel: filepath.ToSlash(rel)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(mods, func(a, b Module) int { return strings.Compare(a.Rel, b.Rel) })
	return mods, nil
}

func readModulePath(goMod string) (string, error) {
	f, err := os.Open(goMod)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`), nil
		}
	}
	return "", fmt.Errorf("%s: no module directive", goMod)
}

// ScanAll scans every module.
func ScanAll(mods []Module) ([]ModuleScan, error) {
	out := make([]ModuleScan, 0, len(mods))
	for _, m := range mods {
		s, err := ScanModule(m)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", m.Rel, err)
		}
		out = append(out, ModuleScan{Module: m, Scan: s})
	}
	return out, nil
}

// Assemble classifies the scanned tests, applies overrides and, when prev is given,
// carries over its header, package timings and per-test run data for tests that still exist.
func Assemble(scans []ModuleScan, ov Overrides, prev *Inventory) (Inventory, error) {
	entries, err := ApplyOverrides(BuildEntries(scans, ov), ov)
	if err != nil {
		return Inventory{}, err
	}
	slices.SortStableFunc(entries, func(a, b Entry) int {
		if c := strings.Compare(a.Module, b.Module); c != 0 {
			return c
		}
		if c := strings.Compare(a.Package, b.Package); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	inv := Inventory{Tests: entries}
	for _, ms := range scans {
		inv.Modules = append(inv.Modules, ModuleInfo{Path: ms.Module.Path, Dir: ms.Module.Rel, Example: slices.Contains(ov.ExampleModules, ms.Module.Rel)})
	}
	if prev != nil {
		inv.Header = prev.Header
		inv.Packages = prev.Packages
		inv.RunUnmatched = prev.RunUnmatched
		old := map[RunKey]*RunResult{}
		for _, e := range prev.Tests {
			old[RunKey{Package: e.Package, Name: e.Name}] = e.Run
		}
		for i := range inv.Tests {
			inv.Tests[i].Run = old[RunKey{Package: inv.Tests[i].Package, Name: inv.Tests[i].Name}]
		}
	}
	return inv, nil
}

// AttachRun merges the run of one module into the inventory.
func AttachRun(inv *Inventory, run RunData) {
	for _, u := range MergeRun(inv.Tests, run) {
		inv.RunUnmatched = append(inv.RunUnmatched, u.Package+"."+u.Name)
	}
	for _, p := range run.Packages {
		inv.Packages = append(inv.Packages, p)
	}
	slices.SortFunc(inv.Packages, func(a, b PackageRun) int { return strings.Compare(a.Package, b.Package) })
	slices.Sort(inv.RunUnmatched)
}

// ReadInventory loads a committed inventory.
func ReadInventory(file string) (Inventory, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Inventory{}, err
	}
	var inv Inventory
	if err := json.Unmarshal(data, &inv); err != nil {
		return Inventory{}, fmt.Errorf("%s: %w", file, err)
	}
	return inv, nil
}

// WriteInventory writes the inventory with stable formatting.
func WriteInventory(file string, inv Inventory) error {
	data, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		return err
	}
	return os.WriteFile(file, append(data, '\n'), 0o600)
}

type checkKey struct{ pkg, name string }

// Check compares the committed inventory with a fresh static scan and returns one
// message per problem. It only looks at what the contract promises: every test is
// listed once, each has a valid lane that still matches the scan, and the summary is current.
func Check(committed, fresh Inventory, summary string) []string {
	var problems []string
	have := map[checkKey]Entry{}
	for _, e := range committed.Tests {
		k := checkKey{e.Package, e.Name}
		if _, dup := have[k]; dup {
			problems = append(problems, fmt.Sprintf("listed twice: %s.%s", e.Package, e.Name))
		}
		have[k] = e
		if !e.Lane.Valid() {
			problems = append(problems, fmt.Sprintf("unclassified: %s.%s has lane %q", e.Package, e.Name, e.Lane))
		}
	}
	seen := map[checkKey]bool{}
	for _, f := range fresh.Tests {
		k := checkKey{f.Package, f.Name}
		seen[k] = true
		c, ok := have[k]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("missing from inventory: %s.%s (%s)", f.Package, f.Name, f.File))
		case c.Lane.Valid() && (c.Lane != f.Lane || c.Destination != f.Destination):
			problems = append(problems, fmt.Sprintf("lane changed: %s.%s is %s/%s in the inventory but %s/%s in the source", f.Package, f.Name, c.Lane, c.Destination, f.Lane, f.Destination))
		}
	}
	for _, e := range committed.Tests {
		if !seen[checkKey{e.Package, e.Name}] {
			problems = append(problems, fmt.Sprintf("no longer in the source: %s.%s", e.Package, e.Name))
		}
	}
	if summary != RenderMarkdown(committed) {
		problems = append(problems, "inventory.md does not match inventory.json")
	}
	return problems
}

// packageDir returns the repository-relative directory of the test's package.
func packageDir(e Entry) string { return path.Dir(e.File) }
