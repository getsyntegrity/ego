package main

import (
	"fmt"
	"strings"
)

// Entry is one allowlist line: a file (or a directory when Path ends in "/") and the reason it is excused.
type Entry struct{ Path, Note string }

// matches reports whether the entry covers the file at p.
func (e Entry) matches(p string) bool {
	if strings.HasSuffix(e.Path, "/") {
		return strings.HasPrefix(p, e.Path)
	}
	return p == e.Path
}

// ParseAllowlist reads lines of the form `path | note`. Blank lines and lines starting with # are ignored.
// The note is required: it is the pull request that owns a pending entry, or the reason a resource is allowed.
func ParseAllowlist(text string) ([]Entry, error) {
	var out []Entry
	seen := map[string]bool{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		n := i + 1
		path, note, ok := strings.Cut(line, "|")
		if !ok {
			return nil, fmt.Errorf("line %d: want `path | note`", n)
		}
		path, note = strings.TrimSpace(path), strings.TrimSpace(note)
		switch {
		case path == "":
			return nil, fmt.Errorf("line %d: the path is required", n)
		case note == "":
			return nil, fmt.Errorf("line %d: the note is required", n)
		case seen[path]:
			return nil, fmt.Errorf("line %d: duplicate entry %s", n, path)
		}
		seen[path] = true
		out = append(out, Entry{Path: path, Note: note})
	}
	return out, nil
}

// Evaluate returns one line per problem (a violation nobody excused) and one per warning (an allowlist entry
// that excuses nothing any more). The pending list covers the import and go-specs rules; the resources list
// covers rule 4. Stale entries are only warnings, because migration PRs merge in parallel and each one would
// turn develop red until the lists were edited; with strict they become problems, which is the mode to use
// once the lists are meant to be kept exact.
func Evaluate(findings []Finding, pending, resources []Entry, strict bool) (problems, warnings []string) {
	pendingUsed := make([]bool, len(pending))
	resourcesUsed := make([]bool, len(resources))

	for _, f := range findings {
		list, used := pending, pendingUsed
		if f.Rule == RuleResource {
			list, used = resources, resourcesUsed
		}
		// An unparsed file and a skip under inttest/ cannot be excused by any list.
		if f.Rule != RuleUnparsed && f.Rule != RuleSkip && markCovered(list, used, f.Path) {
			continue
		}
		line := fmt.Sprintf("%s: %s: %s", f.Path, f.Rule, f.Detail)
		if f.Rule == RuleSkip {
			line += "; a test under inttest/ must fail when its dependency is missing, never skip"
		}
		problems = append(problems, line)
	}
	stale := &warnings
	if strict {
		stale = &problems
	}
	for i, e := range pending {
		if !pendingUsed[i] {
			*stale = append(*stale, fmt.Sprintf("%s: stale pending entry (%s): the file no longer violates the unit-test rules, remove the line", e.Path, e.Note))
		}
	}
	for i, e := range resources {
		if !resourcesUsed[i] {
			*stale = append(*stale, fmt.Sprintf("%s: stale resources entry (%s): the file no longer uses a real resource, remove the line", e.Path, e.Note))
		}
	}
	return problems, warnings
}

// markCovered marks every entry that covers p and reports whether there was one.
func markCovered(list []Entry, used []bool, p string) bool {
	covered := false
	for i, e := range list {
		if e.matches(p) {
			used[i], covered = true, true
		}
	}
	return covered
}
