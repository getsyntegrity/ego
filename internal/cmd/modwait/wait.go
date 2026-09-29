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

// Command modwait waits until a Go module version is resolvable the way a
// consumer's `go get` resolves it: through the public module proxy AND
// verified against the public checksum database.
//
// Why it exists (#189): release.yml used to wait with `go list -m`, which
// only proves proxy.golang.org serves the version. The next step, `go get`,
// also verifies against sum.golang.org, which can lag behind the proxy. In
// the first release (run 36474920456, attempt 1) the wait passed and
// `go get` failed 15 seconds later with a sumdb 404. modwait runs the same
// public path (see probe.go) until it passes, fails permanently, or the
// -timeout deadline expires.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"
)

// clock and sleeper are the seams that make the wait loop deterministic
// under test (same idea as internal/cmd/releasegate).
type clock interface{ Now() time.Time }
type sleeper interface{ Sleep(time.Duration) }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type systemSleeper struct{}

func (systemSleeper) Sleep(d time.Duration) { time.Sleep(d) }

// ProbeResult is what one attempt observed. Output is the text the
// classifier reads (the JSON "Error" field plus stderr); it is empty when OK.
type ProbeResult struct {
	OK     bool
	Output string
}

// prober performs one attempt against the public proxy and checksum
// database. Tests script it; production uses execProber (probe.go).
type prober interface {
	Probe(ctx context.Context, module, version string) ProbeResult
}

// resumeHint tells an operator how to recover from a timeout without
// creating a duplicate tag. The root tag already exists when this runs, and
// the publisher bump branch is pushed only by a later step of the same job,
// so re-running the failed job is safe and repeats nothing irreversible.
const resumeHint = "The root tag already exists: do NOT delete or re-push it. " +
	"Wait a few minutes, then use \"Re-run failed jobs\" on this same release.yml run " +
	"(the release/publishers-* branch has not been pushed at this point, so re-running prepare-publisher-bump is safe)."

// config is the parsed command line.
type config struct {
	module   string
	version  string
	timeout  time.Duration
	interval time.Duration
}

// reportedError marks a failure whose ::error:: annotation was already
// written to stdout, so main does not print it a second time.
type reportedError struct{ msg string }

func (e *reportedError) Error() string { return e.msg }

func parseConfig(args []string, stderr io.Writer) (config, error) {
	fs := flag.NewFlagSet("modwait", flag.ContinueOnError)
	fs.SetOutput(stderr)
	module := fs.String("module", "", "module path, e.g. github.com/getsyntegrity/ego")
	version := fs.String("version", "", "module version, e.g. v4.0.0")
	timeout := fs.Duration("timeout", 20*time.Minute, "maximum total time to wait; 0 makes a single attempt and never waits")
	interval := fs.Duration("interval", 30*time.Second, "time between attempts (each sleep is clamped to the time left)")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() > 0 {
		return config{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if *module == "" {
		return config{}, errors.New("-module is required")
	}
	if *version == "" {
		return config{}, errors.New("-version is required")
	}
	if strings.Contains(*module, "@") || strings.Contains(*version, "@") {
		return config{}, errors.New("-module and -version must not contain \"@\"; pass them separately")
	}
	if err := validateTarget(*module, *version); err != nil {
		return config{}, err
	}
	if *timeout < 0 {
		return config{}, fmt.Errorf("-timeout must not be negative, got %s", *timeout)
	}
	if *interval <= 0 {
		return config{}, fmt.Errorf("-interval must be positive, got %s", *interval)
	}
	return config{module: *module, version: *version, timeout: *timeout, interval: *interval}, nil
}

// run is modwait's whole testable behavior; main only wires the real
// process, clock, sleeper and prober into it.
func run(args []string, stdout, stderr io.Writer, clk clock, slp sleeper, p prober) error {
	cfg, err := parseConfig(args, stderr)
	if err != nil {
		return err
	}
	return waitForModule(context.Background(), p, cfg, clk, slp, stdout)
}

// waitForModule probes until the module resolves, a permanent failure is
// seen, or the deadline passes.
//
//   - Success: the probe passes (proxy and checksum database both answered).
//   - Permanent classification: one ::error:: line and return at once; no
//     more probes, no sleep.
//   - Transient or unclassified: sleep min(interval, time left) and probe
//     again. The last attempt happens exactly at the deadline. timeout 0
//     means one attempt and no sleep.
//
// Every failure path writes exactly one single-line ::error:: annotation to
// stdout and returns a *reportedError.
func waitForModule(ctx context.Context, p prober, cfg config, clk clock, slp sleeper, stdout io.Writer) error {
	target := cfg.module + "@" + cfg.version
	start := clk.Now()
	deadline := start.Add(cfg.timeout)

	fmt.Fprintf(stdout, "modwait: waiting for %s to resolve through the public Go proxy and checksum database (proxy.golang.org / sum.golang.org, verification on); timeout %s, interval %s\n",
		target, cfg.timeout, cfg.interval)

	var last Result
	attempts := 0
	for {
		if err := ctx.Err(); err != nil {
			return fail(stdout, "%s: wait cancelled after %d attempt(s): %v", target, attempts, err)
		}
		attempts++
		res := p.Probe(ctx, cfg.module, cfg.version)
		elapsed := clk.Now().Sub(start).Round(time.Second)
		if res.OK {
			fmt.Fprintf(stdout, "modwait: attempt %d (%s elapsed): %s resolved through the proxy and the checksum database\n", attempts, elapsed, target)
			return nil
		}

		last = Classify(res.Output)
		fmt.Fprintf(stdout, "modwait: attempt %d (%s elapsed): %s (%s): %s\n", attempts, elapsed, last.Class, last.Reason, last.LastLine)

		if last.Class == Permanent {
			return fail(stdout, "%s failed with a permanent error (%s), retrying cannot fix it: %s", target, last.Reason, last.LastLine)
		}

		remaining := deadline.Sub(clk.Now())
		if remaining <= 0 {
			break
		}
		sleep := cfg.interval
		if sleep > remaining {
			sleep = remaining
		}
		fmt.Fprintf(stdout, "modwait: retrying in %s\n", sleep)
		slp.Sleep(sleep)
	}

	return fail(stdout, "Timed out after %s (%d attempt(s)) waiting for %s to resolve through proxy.golang.org and sum.golang.org; last result: %s (%s): %s. %s",
		cfg.timeout, attempts, target, last.Class, last.Reason, last.LastLine, resumeHint)
}

// fail writes one ::error:: workflow annotation (escaped so it stays one
// line) and returns the matching *reportedError.
func fail(stdout io.Writer, format string, a ...any) error {
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintf(stdout, "::error::%s\n", escapeAnnotation(msg))
	return &reportedError{msg: msg}
}

// escapeAnnotation applies GitHub's workflow-command data escaping so an
// output line can never break out of, or split, the annotation.
func escapeAnnotation(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}
