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
	"encoding/json"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
)

// Entry is one test in the inventory: the static scan, its lane and, when a run was
// recorded, what happened at run time.
type Entry struct {
	Test
	Classification
	LeavesPR bool `json:"leaves_pr"`
	// Override is the written reason when a person reclassified the test.
	Override string     `json:"override,omitempty"`
	Run      *RunResult `json:"run,omitempty"`
}

// ModuleScan pairs a module with its static scan.
type ModuleScan struct {
	Module Module
	Scan   Scan
}

// Override reclassifies one test that static analysis cannot see through.
type Override struct {
	// Dir is the repository-relative directory of the package.
	Dir string `json:"dir"`
	// Test or Tests (not both) name the tests of Dir the entry applies to. Tests lets
	// several tests share one written reason.
	Test        string   `json:"test,omitempty"`
	Tests       []string `json:"tests,omitempty"`
	Lane        Lane     `json:"lane"`
	Destination string   `json:"destination,omitempty"`
	Reason      string   `json:"reason"`
}

func (o Override) names() []string {
	if o.Test != "" {
		return append([]string{o.Test}, o.Tests...)
	}
	return o.Tests
}

// Overrides is the content of inventory-overrides.json.
type Overrides struct {
	ExampleModules []string   `json:"example_modules"`
	Overrides      []Override `json:"overrides"`
}

// LoadOverrides reads the overrides file, rejecting unknown fields.
func LoadOverrides(file string) (Overrides, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Overrides{}, err
	}
	var ov Overrides
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ov); err != nil {
		return Overrides{}, fmt.Errorf("%s: %w", file, err)
	}
	return ov, nil
}

// DefaultDestination is where tests of a lane run when an override does not say.
func DefaultDestination(l Lane) string {
	switch l {
	case LaneIntegration:
		return "#210"
	case LaneArchitecture:
		return "#208"
	case LaneExample:
		return "#214"
	}
	return DestPR
}

// BuildEntries classifies every scanned test from its own signals.
func BuildEntries(scans []ModuleScan, ov Overrides) []Entry {
	var entries []Entry
	for _, ms := range scans {
		example := slices.Contains(ov.ExampleModules, ms.Module.Rel)
		for _, t := range ms.Scan.Tests {
			c := Classify(t.Signals, example)
			entries = append(entries, Entry{Test: t, Classification: c, LeavesPR: c.Lane.LeavesPR()})
		}
	}
	return entries
}

// ApplyOverrides returns a copy of entries with the overrides applied. It fails when an
// override has no reason, an invalid lane, no matching test, or is listed twice.
func ApplyOverrides(entries []Entry, ov Overrides) ([]Entry, error) {
	out := slices.Clone(entries)
	seen := map[string]bool{}
	for _, o := range ov.Overrides {
		if (o.Test == "") == (len(o.Tests) == 0) {
			return nil, fmt.Errorf("override in %s must set exactly one of test and tests", o.Dir)
		}
		if strings.TrimSpace(o.Reason) == "" {
			return nil, fmt.Errorf("override in %s has no reason", o.Dir)
		}
		if !o.Lane.Valid() {
			return nil, fmt.Errorf("override in %s has invalid lane %q", o.Dir, o.Lane)
		}
		dest := o.Destination
		if dest == "" {
			dest = DefaultDestination(o.Lane)
		}
		for _, name := range o.names() {
			key := o.Dir + "::" + name
			if seen[key] {
				return nil, fmt.Errorf("override %s is listed twice", key)
			}
			seen[key] = true
			idx := slices.IndexFunc(out, func(e Entry) bool { return path.Dir(e.File) == o.Dir && e.Name == name })
			if idx < 0 {
				return nil, fmt.Errorf("override %s points at a test that does not exist", key)
			}
			e := out[idx]
			e.Classification = Classification{Lane: o.Lane, Destination: dest, Reason: "override"}
			e.LeavesPR = o.Lane.LeavesPR()
			e.Override = o.Reason
			out[idx] = e
		}
	}
	return out, nil
}

// MergeRun attaches run results to the entries and returns the tests the run saw that
// the static scan did not (sorted), which would indicate a scanner blind spot.
func MergeRun(entries []Entry, run RunData) []RunKey {
	matched := map[RunKey]bool{}
	for i := range entries {
		key := RunKey{Package: entries[i].Package, Name: entries[i].Name}
		if res, ok := run.Tests[key]; ok {
			entries[i].Run = res
			matched[key] = true
		}
	}
	var unmatched []RunKey
	for key := range run.Tests {
		if !matched[key] {
			unmatched = append(unmatched, key)
		}
	}
	slices.SortFunc(unmatched, func(a, b RunKey) int {
		if c := strings.Compare(a.Package, b.Package); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	return unmatched
}
