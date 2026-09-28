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
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv, systemClock{}, systemSleeper{}, ""); err != nil {
		fmt.Fprintf(os.Stderr, "releasegate: %v\n", err)
		os.Exit(1)
	}
}

// clock and sleeper are the two seams that make waitForGate's bounded-wait
// loop deterministic and fast under test: production code (main, via
// systemClock/systemSleeper) uses real time.Now/time.Sleep, and
// main_test.go's fakeClock/fakeSleeper advance a virtual clock instead of
// actually blocking, so a test exercising a 20-minute timeout still runs in
// microseconds.
type clock interface{ Now() time.Time }
type sleeper interface{ Sleep(time.Duration) }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type systemSleeper struct{}

func (systemSleeper) Sleep(d time.Duration) { time.Sleep(d) }

// config is run's fully parsed and validated command line.
type config struct {
	repo     string
	sha      string
	onMain   bool
	timeout  time.Duration
	interval time.Duration
}

// run is releasegate's entire testable behavior: flag parsing and
// validation, reading GITHUB_TOKEN, wiring a Client (optionally pointed at
// baseURLOverride so tests use an httptest.Server instead of the real
// GitHub API), and executing the bounded wait loop. main wires it to the
// real process; tests call it directly with fakes.
func run(args []string, stdout, stderr io.Writer, getenv func(string) string, clk clock, slp sleeper, baseURLOverride string) error {
	cfg, err := parseConfig(args, stderr)
	if err != nil {
		return err
	}

	token := getenv("GITHUB_TOKEN")
	if token == "" {
		return errors.New("GITHUB_TOKEN environment variable is required (a read-only token with the actions:read permission is enough)")
	}

	client := NewClient(token)
	if baseURLOverride != "" {
		client.BaseURL = baseURLOverride
	}

	return waitForGate(context.Background(), client, cfg, clk, slp, stdout)
}

// parseConfig parses and validates args into a config. flag parse errors
// and usage go to stderr, matching this repository's other internal/cmd
// CLIs (see internal/cmd/vulngate/main.go).
func parseConfig(args []string, stderr io.Writer) (config, error) {
	fs := flag.NewFlagSet("releasegate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoFlag := fs.String("repo", "", "GitHub repository as \"owner/name\" (e.g. getsyntegrity/ego)")
	shaFlag := fs.String("sha", "", "the exact tagged commit SHA (full 40-character form; dereference a lightweight tag with `git rev-parse \"$TAG^{commit}\"` first)")
	onMainFlag := fs.String("on-main", "", "whether -sha is reachable from origin/main: \"true\" or \"false\" (computed by the caller, e.g. `git merge-base --is-ancestor`)")
	timeoutFlag := fs.Duration("timeout", 20*time.Minute, "maximum time to wait for a pending build.yml run to complete before failing; 0 checks once and never waits")
	intervalFlag := fs.Duration("interval", 30*time.Second, "how long to wait between polls while a build.yml run is still pending")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}

	if *repoFlag == "" {
		return config{}, errors.New("-repo is required")
	}
	if !strings.Contains(*repoFlag, "/") {
		return config{}, fmt.Errorf("-repo %q must be in \"owner/name\" form", *repoFlag)
	}
	if *shaFlag == "" {
		return config{}, errors.New("-sha is required")
	}
	var onMain bool
	switch *onMainFlag {
	case "true":
		onMain = true
	case "false":
		onMain = false
	default:
		return config{}, fmt.Errorf("-on-main must be exactly \"true\" or \"false\", got %q", *onMainFlag)
	}
	if *intervalFlag <= 0 {
		return config{}, fmt.Errorf("-interval must be positive, got %s", *intervalFlag)
	}

	return config{
		repo:     *repoFlag,
		sha:      *shaFlag,
		onMain:   onMain,
		timeout:  *timeoutFlag,
		interval: *intervalFlag,
	}, nil
}

// waitForGate is the bounded-wait loop: it polls Client.ListBuildRuns and
// Decide until Decide returns a terminal verdict (Pass or Fail) or the
// timeout budget starting now is exhausted, whichever comes first.
//
// The off-main case is checked once, up front, without ever calling
// GitHub: Decide would report the same Fail on every poll (main
// reachability cannot change while this process runs), so polling for it
// would only waste GitHub API calls and the caller's time.
//
// cfg.timeout <= 0 means "check exactly once, never wait": this is what
// docs/ci.md's dry-test instructions use, so a read-only check against the
// real repository never blocks for up to 20 minutes.
func waitForGate(ctx context.Context, client *Client, cfg config, clk clock, slp sleeper, stdout io.Writer) error {
	if !cfg.onMain {
		return fmt.Errorf("release gate: %s", Decide(cfg.sha, false, nil).Reason)
	}

	deadline := clk.Now().Add(cfg.timeout)
	for attempt := 1; ; attempt++ {
		runs, err := client.ListBuildRuns(ctx, cfg.repo, cfg.sha)
		if err != nil {
			return fmt.Errorf("listing build.yml runs: %w", err)
		}

		res := Decide(cfg.sha, true, runs)
		switch res.Verdict {
		case Pass:
			fmt.Fprintf(stdout, "release gate: %s — %s\n", res.Verdict, res.Reason)
			return nil
		case Fail:
			return fmt.Errorf("release gate: %s — %s", res.Verdict, res.Reason)
		case Wait:
			now := clk.Now()
			if cfg.timeout <= 0 || !now.Before(deadline) {
				return fmt.Errorf("release gate timed out after %s waiting for commit %s: %s", cfg.timeout, cfg.sha, res.Reason)
			}
			fmt.Fprintf(stdout, "release gate: %s (attempt %d) — %s; polling again in %s\n", res.Verdict, attempt, res.Reason, cfg.interval)
			slp.Sleep(cfg.interval)
		}
	}
}
