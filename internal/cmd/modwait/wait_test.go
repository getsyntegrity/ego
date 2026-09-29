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
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// fakeClock and fakeSleeper share one virtual timeline so a 20-minute
// timeout runs in microseconds.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakeSleeper struct {
	clk    *fakeClock
	sleeps []time.Duration
}

func (s *fakeSleeper) Sleep(d time.Duration) {
	s.sleeps = append(s.sleeps, d)
	s.clk.now = s.clk.now.Add(d)
}

// scriptedProber returns the scripted results in order; the last one repeats.
type scriptedProber struct {
	results []ProbeResult
	calls   int
}

func (p *scriptedProber) Probe(_ context.Context, _, _ string) ProbeResult {
	i := p.calls
	if i >= len(p.results) {
		i = len(p.results) - 1
	}
	p.calls++
	return p.results[i]
}

var (
	okResult   = ProbeResult{OK: true}
	notFound   = ProbeResult{Output: realSumdb404}
	permResult = func(out string) ProbeResult { return ProbeResult{Output: out} }
)

func newHarness() (*fakeClock, *fakeSleeper) {
	clk := &fakeClock{now: time.Date(2026, 9, 28, 20, 0, 0, 0, time.UTC)}
	return clk, &fakeSleeper{clk: clk}
}

func testConfig(timeout, interval time.Duration) config {
	return config{module: "github.com/getsyntegrity/ego/v4", version: "v4.0.0", timeout: timeout, interval: interval}
}

func TestWait_ImmediateSuccess(t *testing.T) {
	clk, slp := newHarness()
	p := &scriptedProber{results: []ProbeResult{okResult}}
	var out bytes.Buffer
	if err := waitForModule(context.Background(), p, testConfig(20*time.Minute, 30*time.Second), clk, slp, &out); err != nil {
		t.Fatalf("err = %v", err)
	}
	if p.calls != 1 || len(slp.sleeps) != 0 {
		t.Errorf("calls=%d sleeps=%v, want 1 and none", p.calls, slp.sleeps)
	}
}

func TestWait_LateSuccess_404Then404ThenOK(t *testing.T) {
	clk, slp := newHarness()
	p := &scriptedProber{results: []ProbeResult{notFound, notFound, okResult}}
	var out bytes.Buffer
	if err := waitForModule(context.Background(), p, testConfig(20*time.Minute, 30*time.Second), clk, slp, &out); err != nil {
		t.Fatalf("err = %v", err)
	}
	if p.calls != 3 {
		t.Errorf("calls = %d, want 3", p.calls)
	}
	if len(slp.sleeps) != 2 || slp.sleeps[0] != 30*time.Second {
		t.Errorf("sleeps = %v, want two 30s sleeps", slp.sleeps)
	}
	if strings.Contains(out.String(), "::error::") {
		t.Errorf("success must not emit ::error::, got %q", out.String())
	}
}

func TestWait_TransientThenPass(t *testing.T) {
	for name, first := range map[string]string{
		"5xx":     "reading https://sum.golang.org/lookup/x: 503 Service Unavailable",
		"429":     "reading https://sum.golang.org/lookup/x: 429 Too Many Requests",
		"network": "dial tcp: lookup sum.golang.org: no such host",
	} {
		t.Run(name, func(t *testing.T) {
			clk, slp := newHarness()
			p := &scriptedProber{results: []ProbeResult{permResult(first), okResult}}
			if err := waitForModule(context.Background(), p, testConfig(time.Minute, 10*time.Second), clk, slp, io.Discard); err != nil {
				t.Fatalf("err = %v", err)
			}
			if p.calls != 2 || len(slp.sleeps) != 1 {
				t.Errorf("calls=%d sleeps=%v, want 2 and one sleep", p.calls, slp.sleeps)
			}
		})
	}
}

func TestWait_PermanentFailsImmediately(t *testing.T) {
	for name, output := range map[string]string{
		"410":               "reading https://proxy.golang.org/x: 410 Gone",
		"checksum mismatch": "verifying module: checksum mismatch\nSECURITY ERROR",
		"declared path":     "module declares its path as: github.com/y/v4\n\tbut was required as: github.com/x/v4",
		"malformed version": "invalid version: malformed version: v4..0",
	} {
		t.Run(name, func(t *testing.T) {
			clk, slp := newHarness()
			p := &scriptedProber{results: []ProbeResult{permResult(output), okResult}}
			var out bytes.Buffer
			err := waitForModule(context.Background(), p, testConfig(20*time.Minute, 30*time.Second), clk, slp, &out)
			if err == nil {
				t.Fatal("want an error")
			}
			if p.calls != 1 || len(slp.sleeps) != 0 {
				t.Errorf("calls=%d sleeps=%v, want exactly one probe and no sleep", p.calls, slp.sleeps)
			}
			line := errorLine(t, out.String())
			for _, want := range []string{"github.com/getsyntegrity/ego/v4@v4.0.0", "permanent"} {
				if !strings.Contains(line, want) {
					t.Errorf("::error:: line %q lacks %q", line, want)
				}
			}
		})
	}
}

func TestWait_TimeoutNamesResumeSteps(t *testing.T) {
	clk, slp := newHarness()
	p := &scriptedProber{results: []ProbeResult{notFound}}
	var out bytes.Buffer
	err := waitForModule(context.Background(), p, testConfig(2*time.Minute, 30*time.Second), clk, slp, &out)
	if err == nil {
		t.Fatal("want a timeout error")
	}
	// attempts at 0s, 30s, 60s, 90s and 120s (the deadline itself)
	if p.calls != 5 {
		t.Errorf("calls = %d, want 5", p.calls)
	}
	line := errorLine(t, out.String())
	for _, want := range []string{
		"github.com/getsyntegrity/ego/v4@v4.0.0",
		"2m0s",
		"transient",
		"HTTP 404",
		"unknown revision v4.0.0", // the last output line
		"do NOT delete or re-push",
		"Re-run failed jobs",
		"release.yml",
		"prepare-publisher-bump",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("::error:: line %q lacks %q", line, want)
		}
	}
	if strings.Count(out.String(), "::error::") != 1 {
		t.Errorf("want exactly one ::error:: line, got %q", out.String())
	}
	if strings.Contains(line, "\n") {
		t.Errorf("::error:: annotation must be one line: %q", line)
	}
}

func TestWait_UnclassifiedIsRetriedAndReported(t *testing.T) {
	clk, slp := newHarness()
	p := &scriptedProber{results: []ProbeResult{permResult("strange new failure")}}
	var out bytes.Buffer
	if err := waitForModule(context.Background(), p, testConfig(time.Minute, 30*time.Second), clk, slp, &out); err == nil {
		t.Fatal("want a timeout error")
	}
	if p.calls != 3 {
		t.Errorf("calls = %d, want 3 (retried until the deadline)", p.calls)
	}
	line := errorLine(t, out.String())
	if !strings.Contains(line, "unclassified") || !strings.Contains(line, "strange new failure") {
		t.Errorf("::error:: line %q must report the unclassified last output", line)
	}
}

func TestWait_TimeoutZeroIsSingleAttempt(t *testing.T) {
	clk, slp := newHarness()
	p := &scriptedProber{results: []ProbeResult{notFound}}
	var out bytes.Buffer
	if err := waitForModule(context.Background(), p, testConfig(0, 30*time.Second), clk, slp, &out); err == nil {
		t.Fatal("want an error")
	}
	if p.calls != 1 || len(slp.sleeps) != 0 {
		t.Errorf("calls=%d sleeps=%v, want one attempt, no sleep", p.calls, slp.sleeps)
	}
	_ = errorLine(t, out.String())

	// and a passing single attempt still passes
	p = &scriptedProber{results: []ProbeResult{okResult}}
	if err := waitForModule(context.Background(), p, testConfig(0, 30*time.Second), clk, slp, io.Discard); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestWait_SleepIsClampedToTimeLeft(t *testing.T) {
	clk, slp := newHarness()
	p := &scriptedProber{results: []ProbeResult{notFound}}
	// interval (10m) is larger than the whole timeout (45s)
	_ = waitForModule(context.Background(), p, testConfig(45*time.Second, 10*time.Minute), clk, slp, io.Discard)
	if len(slp.sleeps) != 1 || slp.sleeps[0] != 45*time.Second {
		t.Errorf("sleeps = %v, want a single sleep clamped to 45s", slp.sleeps)
	}
	if p.calls != 2 {
		t.Errorf("calls = %d, want 2 (start and deadline)", p.calls)
	}

	clk, slp = newHarness()
	p = &scriptedProber{results: []ProbeResult{notFound}}
	_ = waitForModule(context.Background(), p, testConfig(45*time.Second, 30*time.Second), clk, slp, io.Discard)
	if len(slp.sleeps) != 2 || slp.sleeps[0] != 30*time.Second || slp.sleeps[1] != 15*time.Second {
		t.Errorf("sleeps = %v, want [30s 15s]", slp.sleeps)
	}
}

func TestWait_LogsHeaderWithLimits(t *testing.T) {
	clk, slp := newHarness()
	p := &scriptedProber{results: []ProbeResult{okResult}}
	var out bytes.Buffer
	_ = waitForModule(context.Background(), p, testConfig(20*time.Minute, 30*time.Second), clk, slp, &out)
	for _, want := range []string{"github.com/getsyntegrity/ego/v4@v4.0.0", "20m0s", "30s", "sum.golang.org"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("log %q lacks %q", out.String(), want)
		}
	}
}

func TestWait_CancelledContextStops(t *testing.T) {
	clk, slp := newHarness()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &scriptedProber{results: []ProbeResult{notFound}}
	if err := waitForModule(ctx, p, testConfig(time.Hour, time.Second), clk, slp, io.Discard); err == nil {
		t.Fatal("want an error")
	}
	if p.calls > 1 {
		t.Errorf("calls = %d, want at most 1 after cancellation", p.calls)
	}
}

func TestParseConfig(t *testing.T) {
	cfg, err := parseConfig([]string{"-module", "m/v4", "-version", "v4.0.0"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.timeout != 20*time.Minute || cfg.interval != 30*time.Second {
		t.Errorf("defaults = %s / %s, want 20m / 30s", cfg.timeout, cfg.interval)
	}

	cfg, err = parseConfig([]string{"-module", "m/v4", "-version", "v4.0.0", "-timeout", "0", "-interval", "5s"}, io.Discard)
	if err != nil || cfg.timeout != 0 || cfg.interval != 5*time.Second {
		t.Errorf("cfg=%+v err=%v", cfg, err)
	}

	for name, args := range map[string][]string{
		"missing module":   {"-version", "v4.0.0"},
		"missing version":  {"-module", "m/v4"},
		"zero interval":    {"-module", "m/v4", "-version", "v4.0.0", "-interval", "0"},
		"negative timeout": {"-module", "m/v4", "-version", "v4.0.0", "-timeout", "-1s"},
		"version has @":    {"-module", "m/v4", "-version", "@v4.0.0"},
		"module has @":     {"-module", "m/v4@v4.0.0", "-version", "v4.0.0"},
		"unknown flag":     {"-module", "m/v4", "-version", "v4.0.0", "-bogus"},
		"positional args":  {"-module", "m/v4", "-version", "v4.0.0", "extra"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseConfig(args, io.Discard); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestRun_ReportedErrorIsMarked(t *testing.T) {
	clk, slp := newHarness()
	p := &scriptedProber{results: []ProbeResult{notFound}}
	var out bytes.Buffer
	err := run([]string{"-module", "m/v4", "-version", "v4.0.0", "-timeout", "0"}, &out, io.Discard, clk, slp, p)
	var re *reportedError
	if !errors.As(err, &re) {
		t.Fatalf("err = %v, want a *reportedError (already annotated)", err)
	}
	if err := run([]string{"-version", "v4.0.0"}, &out, io.Discard, clk, slp, p); err == nil || errors.As(err, &re) {
		t.Errorf("a usage error must be a plain error, got %v", err)
	}
}

func errorLine(t *testing.T, out string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "::error::") {
			return l
		}
	}
	t.Fatalf("no ::error:: line in %q", out)
	return ""
}
