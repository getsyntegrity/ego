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
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- fakes for deterministic, fast bounded-wait tests -----------------------

// fakeClock is a manually-advanced virtual clock: Now() never calls the
// real system clock, so tests that exercise the bounded wait loop's timeout
// arithmetic are deterministic and do not sleep in real time.
type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time { return f.now }

// fakeSleeper advances the paired fakeClock by the requested duration
// instead of actually blocking, and records every call so tests can assert
// on the number of polls.
type fakeSleeper struct {
	clock *fakeClock
	calls []time.Duration
}

func (f *fakeSleeper) Sleep(d time.Duration) {
	f.calls = append(f.calls, d)
	f.clock.now = f.clock.now.Add(d)
}

func fakeGetenv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

// --- fixtures ---------------------------------------------------------------

func alwaysReturn(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
}

func successRunBody() string {
	return fmt.Sprintf(`{"total_count":1,"workflow_runs":[{"id":1,"head_sha":%q,"status":"completed","conclusion":"success","created_at":"2026-09-28T10:00:00Z","html_url":"https://github.com/o/r/actions/runs/1"}]}`, sha)
}

func failureRunBody() string {
	return fmt.Sprintf(`{"total_count":1,"workflow_runs":[{"id":1,"head_sha":%q,"status":"completed","conclusion":"failure","created_at":"2026-09-28T10:00:00Z","html_url":"https://github.com/o/r/actions/runs/1"}]}`, sha)
}

func inProgressRunBody() string {
	return fmt.Sprintf(`{"total_count":1,"workflow_runs":[{"id":1,"head_sha":%q,"status":"in_progress","created_at":"2026-09-28T10:00:00Z"}]}`, sha)
}

func baseArgs() []string {
	return []string{
		"-repo", "getsyntegrity/ego",
		"-sha", sha,
		"-on-main", "true",
		"-timeout", "20m",
		"-interval", "30s",
	}
}

// --- tests --------------------------------------------------------------

func TestRun_Pass(t *testing.T) {
	srv := alwaysReturn(t, successRunBody())
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	clk := &fakeClock{now: time.Now()}
	slp := &fakeSleeper{clock: clk}
	err := run(baseArgs(), &stdout, &stderr, fakeGetenv(map[string]string{"GITHUB_TOKEN": "tok"}), clk, slp, srv.URL)
	if err != nil {
		t.Fatalf("run: %v; stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "PASS") {
		t.Fatalf("stdout = %q, want it to mention PASS", stdout.String())
	}
}

func TestRun_Fail(t *testing.T) {
	srv := alwaysReturn(t, failureRunBody())
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	clk := &fakeClock{now: time.Now()}
	slp := &fakeSleeper{clock: clk}
	err := run(baseArgs(), &stdout, &stderr, fakeGetenv(map[string]string{"GITHUB_TOKEN": "tok"}), clk, slp, srv.URL)
	if err == nil {
		t.Fatal("run: want error on a failed build.yml run, got nil")
	}
	if !strings.Contains(err.Error(), "failure") {
		t.Fatalf("error = %v, want it to name the failure conclusion", err)
	}
}

func TestRun_OffMainFailsFastWithoutCallingGitHub(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, successRunBody())
	}))
	defer srv.Close()

	args := []string{"-repo", "getsyntegrity/ego", "-sha", sha, "-on-main", "false", "-timeout", "20m", "-interval", "30s"}
	var stdout, stderr bytes.Buffer
	clk := &fakeClock{now: time.Now()}
	slp := &fakeSleeper{clock: clk}
	err := run(args, &stdout, &stderr, fakeGetenv(map[string]string{"GITHUB_TOKEN": "tok"}), clk, slp, srv.URL)
	if err == nil {
		t.Fatal("run: want error when the SHA is not on main, got nil")
	}
	if !strings.Contains(err.Error(), "main") {
		t.Fatalf("error = %v, want it to mention main", err)
	}
	if calls != 0 {
		t.Fatalf("GitHub API was called %d times, want 0 (off-main must fail fast without polling)", calls)
	}
}

func TestRun_WaitThenTimeout(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, inProgressRunBody())
	}))
	defer srv.Close()

	args := []string{"-repo", "getsyntegrity/ego", "-sha", sha, "-on-main", "true", "-timeout", "2m", "-interval", "1m"}
	var stdout, stderr bytes.Buffer
	clk := &fakeClock{now: time.Now()}
	slp := &fakeSleeper{clock: clk}
	err := run(args, &stdout, &stderr, fakeGetenv(map[string]string{"GITHUB_TOKEN": "tok"}), clk, slp, srv.URL)
	if err == nil {
		t.Fatal("run: want a timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want it to say timed out", err)
	}
	if calls != 3 {
		t.Fatalf("GitHub API was called %d times, want 3 (poll at 0, 1m, 2m before the 2m deadline expires)", calls)
	}
	if len(slp.calls) != 2 {
		t.Fatalf("Sleep was called %d times, want 2", len(slp.calls))
	}
}

func TestRun_TimeoutZeroIsSingleCheckNoSleep(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, inProgressRunBody())
	}))
	defer srv.Close()

	args := []string{"-repo", "getsyntegrity/ego", "-sha", sha, "-on-main", "true", "-timeout", "0", "-interval", "30s"}
	var stdout, stderr bytes.Buffer
	clk := &fakeClock{now: time.Now()}
	slp := &fakeSleeper{clock: clk}
	err := run(args, &stdout, &stderr, fakeGetenv(map[string]string{"GITHUB_TOKEN": "tok"}), clk, slp, srv.URL)
	if err == nil {
		t.Fatal("run: want an error (still pending, no wait budget), got nil")
	}
	if calls != 1 {
		t.Fatalf("GitHub API was called %d times, want exactly 1 (-timeout 0 checks once, does not wait)", calls)
	}
	if len(slp.calls) != 0 {
		t.Fatalf("Sleep was called %d times, want 0", len(slp.calls))
	}
}

func TestRun_MissingGitHubToken(t *testing.T) {
	var stdout, stderr bytes.Buffer
	clk := &fakeClock{now: time.Now()}
	slp := &fakeSleeper{clock: clk}
	err := run(baseArgs(), &stdout, &stderr, fakeGetenv(nil), clk, slp, "")
	if err == nil {
		t.Fatal("run: want error when GITHUB_TOKEN is unset, got nil")
	}
	if !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("error = %v, want it to mention GITHUB_TOKEN", err)
	}
}

func TestRun_MissingRequiredFlags(t *testing.T) {
	cases := [][]string{
		{"-sha", sha, "-on-main", "true"},                  // missing -repo
		{"-repo", "o/r", "-on-main", "true"},               // missing -sha
		{"-repo", "o/r", "-sha", sha},                      // missing -on-main
		{"-repo", "o/r", "-sha", sha, "-on-main", "maybe"}, // invalid -on-main
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		clk := &fakeClock{now: time.Now()}
		slp := &fakeSleeper{clock: clk}
		err := run(args, &stdout, &stderr, fakeGetenv(map[string]string{"GITHUB_TOKEN": "tok"}), clk, slp, "")
		if err == nil {
			t.Fatalf("run(%v): want error, got nil", args)
		}
	}
}
