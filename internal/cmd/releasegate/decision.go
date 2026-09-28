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

// Command releasegate is release.yml's pre-publish gate: given a tagged
// commit SHA, it refuses to let the release proceed unless that EXACT SHA
// (not some nearby or previously-tagged commit) is reachable from main and
// has a build.yml run that completed with conclusion "success". See
// docs/ci.md, "Release gate", for the full rationale, how to test it
// without publishing anything, and a documented pre-existing limitation
// this gate does not fix (the release-publishers job's direct push to
// protected main).
package main

import (
	"fmt"
	"sort"
	"time"
)

// Verdict is Decide's three-way outcome for one tagged commit.
type Verdict int

const (
	// Fail means the tag must be rejected outright and no further polling
	// will change that: either the commit is not on main (which cannot
	// become true by waiting), or its most recent completed build.yml run
	// did not succeed.
	Fail Verdict = iota
	// Wait means the decision is not yet final: no build.yml run exists
	// yet for this exact SHA, or the most recent one is still running.
	// Polling again before the bounded wait's deadline might still resolve
	// this to Pass (or, if it finishes red, to Fail).
	Wait
	// Pass means the tagged SHA is reachable from main and its most recent
	// completed build.yml run's conclusion is "success".
	Pass
)

// String renders v the way releasegate's own log lines and error messages
// do, so tests and operators read the same word.
func (v Verdict) String() string {
	switch v {
	case Fail:
		return "FAIL"
	case Wait:
		return "WAIT"
	case Pass:
		return "PASS"
	default:
		return fmt.Sprintf("Verdict(%d)", int(v))
	}
}

// statusCompleted is the one GitHub Actions run Status value that means a
// run is done and its Conclusion is meaningful. Every other status
// (queued, in_progress, waiting, pending, requested) means the run can
// still change outcome.
const statusCompleted = "completed"

// Run mirrors the one GitHub Actions workflow run fields Decide needs, as
// returned by GET
// /repos/{owner}/{repo}/actions/workflows/build.yml/runs. See client.go for
// how these are fetched from the real GitHub REST API.
type Run struct {
	ID         int64
	HeadSHA    string
	Event      string // "push", "workflow_dispatch", ... — kept for the caller's own reporting; Decide does not filter on it, see the package doc comment on Client.ListBuildRuns for why.
	Status     string // "queued", "in_progress", "completed", ...
	Conclusion string // meaningful only when Status == "completed": "success", "failure", "cancelled", "skipped", "timed_out", ...
	CreatedAt  time.Time
	HTMLURL    string
}

// Result is Decide's complete, explained outcome: never just a verdict on
// its own, because both a human reading a failed CI job and a bounded-wait
// loop deciding whether to keep polling need the reason in the same call.
type Result struct {
	Verdict Verdict
	Reason  string
}

// Decide is the release gate's pure decision function. It has no I/O and no
// notion of elapsed time or deadlines — the caller's bounded-wait loop
// (see main.go's waitForGate) is what turns a persistent Wait into a
// terminal failure once its own deadline passes.
//
// sha is the exact tagged commit (already dereferenced from the pushed tag
// to a commit object — see docs/ci.md's note on lightweight tags pointing
// at annotated tag objects). onMain reports whether sha is reachable from
// origin/main; the caller computes this with `git merge-base
// --is-ancestor` (see docs/ci.md for why that was chosen over the GitHub
// compare API). runs is every build.yml run the caller fetched; Decide
// filters it to sha itself, so passing an unfiltered list (as
// Client.ListBuildRuns already returns, since it queries by head_sha
// itself) is fine and is exactly what main.go does.
//
// Rule (see docs/ci.md, "Release gate", for the two rejected alternatives
// and why): among the runs matching sha, the MOST RECENT one governs,
// ordered by CreatedAt (ties broken by the higher, and therefore later,
// run ID — GitHub Actions run IDs are assigned monotonically instance-wide,
// so this is a safe, simpler tiebreaker than parsing sub-second timestamp
// precision). If that latest run has not completed, the outcome is Wait
// regardless of what any older run for the same SHA concluded — a newer
// run in flight might still turn out to be a duplicate re-run that
// resolves either way, and true certainty means the newest evidence.
func Decide(sha string, onMain bool, runs []Run) Result {
	if !onMain {
		return Result{
			Verdict: Fail,
			Reason:  fmt.Sprintf("commit %s is not reachable from origin/main: a release tag must point at a commit that is actually on main", sha),
		}
	}

	matching := make([]Run, 0, len(runs))
	for _, r := range runs {
		if r.HeadSHA == sha {
			matching = append(matching, r)
		}
	}
	if len(matching) == 0 {
		return Result{
			Verdict: Wait,
			Reason:  fmt.Sprintf("no build.yml run found yet for commit %s", sha),
		}
	}

	sort.SliceStable(matching, func(i, j int) bool {
		if !matching[i].CreatedAt.Equal(matching[j].CreatedAt) {
			return matching[i].CreatedAt.After(matching[j].CreatedAt)
		}
		return matching[i].ID > matching[j].ID
	})
	latest := matching[0]

	if latest.Status != statusCompleted {
		return Result{
			Verdict: Wait,
			Reason:  fmt.Sprintf("the most recent build.yml run for commit %s (run %d, status %s) has not completed yet", sha, latest.ID, latest.Status),
		}
	}

	if latest.Conclusion == "success" {
		return Result{
			Verdict: Pass,
			Reason:  fmt.Sprintf("build.yml run %d for commit %s completed with conclusion success (%s)", latest.ID, sha, latest.HTMLURL),
		}
	}

	return Result{
		Verdict: Fail,
		Reason:  fmt.Sprintf("the most recent build.yml run for commit %s (run %d, %s) completed with conclusion %q, not success", sha, latest.ID, latest.HTMLURL, latest.Conclusion),
	}
}
