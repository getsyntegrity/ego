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
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
)

// readRetiredTags reads the committed list of retired tag names
// (scripts/ci/retired-tags.txt): one tag per line, blank lines and "#"
// comments skipped. A missing file is an error, never an empty list: a
// guard that silently turns itself off protects nothing.
func readRetiredTags(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	retired := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		retired[line] = true
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return retired, nil
}

// checkNotRetired refuses, naming every offender, when any of tags is a
// retired name. A retired tag was published and then withdrawn: the Go
// module proxy and checksum database keep the old content forever, so
// tagging different content under the same name would be a checksum
// mismatch for every consumer that already saw it. A nil or empty retired
// set restricts nothing.
func checkNotRetired(retired map[string]bool, tags ...string) error {
	var hits []string
	for _, t := range tags {
		if retired[t] {
			hits = append(hits, t)
		}
	}
	if len(hits) == 0 {
		return nil
	}
	sort.Strings(hits)
	return fmt.Errorf("refusing retired tag(s) %s: these names were published and then withdrawn, and the Go module proxy and checksum database keep the old content, so reusing them would be a checksum mismatch for consumers; pick a different version (see scripts/ci/retired-tags.txt)", strings.Join(hits, ", "))
}

// guardRetired applies the retired-names guard to a computed plan: it
// refuses when any existing tag is a retired name (a withdrawn name was
// pushed again) or when the plan would create one. retiredPath "" disables
// the guard.
func guardRetired(retiredPath string, existing []string, modules []PlanModule) error {
	if retiredPath == "" {
		return nil
	}
	retired, err := readRetiredTags(retiredPath)
	if err != nil {
		return fmt.Errorf("reading -retired: %w", err)
	}
	if err := checkNotRetired(retired, existing...); err != nil {
		return fmt.Errorf("an existing tag is retired: %w", err)
	}
	planned := make([]string, 0, len(modules))
	for _, m := range modules {
		planned = append(planned, m.NextTag)
	}
	return checkNotRetired(retired, planned...)
}
