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
	"strings"
	"testing"
	"time"
)

const sha = "1111111111111111111111111111111111aaaa"

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parsing time %q: %v", s, err)
	}
	return tm
}

func TestDecide_OffMainFailsRegardlessOfRuns(t *testing.T) {
	runs := []Run{
		{ID: 1, HeadSHA: sha, Status: "completed", Conclusion: "success", CreatedAt: mustTime(t, "2026-09-28T10:00:00Z")},
	}
	res := Decide(sha, false, runs)
	if res.Verdict != Fail {
		t.Fatalf("Verdict = %v, want Fail", res.Verdict)
	}
	if !strings.Contains(res.Reason, "not reachable from origin/main") {
		t.Fatalf("Reason = %q, want mention of main reachability", res.Reason)
	}
}

func TestDecide_SuccessOnExactSHAPasses(t *testing.T) {
	runs := []Run{
		{ID: 1, HeadSHA: sha, Status: "completed", Conclusion: "success", CreatedAt: mustTime(t, "2026-09-28T10:00:00Z"), HTMLURL: "https://github.com/o/r/actions/runs/1"},
	}
	res := Decide(sha, true, runs)
	if res.Verdict != Pass {
		t.Fatalf("Verdict = %v, want Pass; reason=%q", res.Verdict, res.Reason)
	}
}

func TestDecide_DifferentSHAIsIgnored(t *testing.T) {
	runs := []Run{
		{ID: 1, HeadSHA: "0000000000000000000000000000000000dead", Status: "completed", Conclusion: "success", CreatedAt: mustTime(t, "2026-09-28T10:00:00Z")},
	}
	res := Decide(sha, true, runs)
	if res.Verdict != Wait {
		t.Fatalf("Verdict = %v, want Wait (no run yet for the target SHA)", res.Verdict)
	}
}

func TestDecide_FailureConclusionFails(t *testing.T) {
	runs := []Run{
		{ID: 1, HeadSHA: sha, Status: "completed", Conclusion: "failure", CreatedAt: mustTime(t, "2026-09-28T10:00:00Z"), HTMLURL: "https://github.com/o/r/actions/runs/1"},
	}
	res := Decide(sha, true, runs)
	if res.Verdict != Fail {
		t.Fatalf("Verdict = %v, want Fail", res.Verdict)
	}
	if !strings.Contains(res.Reason, "failure") || !strings.Contains(res.Reason, "https://github.com/o/r/actions/runs/1") {
		t.Fatalf("Reason = %q, want it to name the conclusion and the run URL", res.Reason)
	}
}

func TestDecide_CancelledConclusionFails(t *testing.T) {
	runs := []Run{
		{ID: 1, HeadSHA: sha, Status: "completed", Conclusion: "cancelled", CreatedAt: mustTime(t, "2026-09-28T10:00:00Z")},
	}
	res := Decide(sha, true, runs)
	if res.Verdict != Fail {
		t.Fatalf("Verdict = %v, want Fail", res.Verdict)
	}
	if !strings.Contains(res.Reason, "cancelled") {
		t.Fatalf("Reason = %q, want it to name conclusion cancelled", res.Reason)
	}
}

func TestDecide_PendingRunWaits(t *testing.T) {
	runs := []Run{
		{ID: 1, HeadSHA: sha, Status: "in_progress", Conclusion: "", CreatedAt: mustTime(t, "2026-09-28T10:00:00Z")},
	}
	res := Decide(sha, true, runs)
	if res.Verdict != Wait {
		t.Fatalf("Verdict = %v, want Wait", res.Verdict)
	}
}

func TestDecide_QueuedRunWaits(t *testing.T) {
	runs := []Run{
		{ID: 1, HeadSHA: sha, Status: "queued", CreatedAt: mustTime(t, "2026-09-28T10:00:00Z")},
	}
	res := Decide(sha, true, runs)
	if res.Verdict != Wait {
		t.Fatalf("Verdict = %v, want Wait", res.Verdict)
	}
}

func TestDecide_NoRunAtAllWaits(t *testing.T) {
	res := Decide(sha, true, nil)
	if res.Verdict != Wait {
		t.Fatalf("Verdict = %v, want Wait", res.Verdict)
	}
	if !strings.Contains(res.Reason, "no build.yml run") {
		t.Fatalf("Reason = %q, want it to say no run was found", res.Reason)
	}
}

// TestDecide_MostRecentCompletedRunGoverns verifies the recommended rule:
// the LATEST completed run for the SHA decides the outcome, not "any run
// ever succeeded" and not "any run ever failed". An older failure followed
// by a newer success passes; an older success followed by a newer failure
// fails (a re-run that regressed must not be masked by a stale green run).
func TestDecide_MostRecentCompletedRunGoverns(t *testing.T) {
	t.Run("older failure then newer success passes", func(t *testing.T) {
		runs := []Run{
			{ID: 1, HeadSHA: sha, Status: "completed", Conclusion: "failure", CreatedAt: mustTime(t, "2026-09-28T09:00:00Z")},
			{ID: 2, HeadSHA: sha, Status: "completed", Conclusion: "success", CreatedAt: mustTime(t, "2026-09-28T10:00:00Z")},
		}
		res := Decide(sha, true, runs)
		if res.Verdict != Pass {
			t.Fatalf("Verdict = %v, want Pass; reason=%q", res.Verdict, res.Reason)
		}
	})

	t.Run("older success then newer failure fails", func(t *testing.T) {
		runs := []Run{
			{ID: 1, HeadSHA: sha, Status: "completed", Conclusion: "success", CreatedAt: mustTime(t, "2026-09-28T09:00:00Z")},
			{ID: 2, HeadSHA: sha, Status: "completed", Conclusion: "failure", CreatedAt: mustTime(t, "2026-09-28T10:00:00Z"), HTMLURL: "https://github.com/o/r/actions/runs/2"},
		}
		res := Decide(sha, true, runs)
		if res.Verdict != Fail {
			t.Fatalf("Verdict = %v, want Fail; reason=%q", res.Verdict, res.Reason)
		}
	})

	t.Run("completed success then newer still-running waits", func(t *testing.T) {
		runs := []Run{
			{ID: 1, HeadSHA: sha, Status: "completed", Conclusion: "success", CreatedAt: mustTime(t, "2026-09-28T09:00:00Z")},
			{ID: 2, HeadSHA: sha, Status: "in_progress", CreatedAt: mustTime(t, "2026-09-28T10:00:00Z")},
		}
		res := Decide(sha, true, runs)
		if res.Verdict != Wait {
			t.Fatalf("Verdict = %v, want Wait; reason=%q", res.Verdict, res.Reason)
		}
	})
}
