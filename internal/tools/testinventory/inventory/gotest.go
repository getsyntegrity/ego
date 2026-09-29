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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// Result statuses as reported by `go test -json`.
const (
	StatusPass = "pass"
	StatusFail = "fail"
	StatusSkip = "skip"
)

// Subtest is one subtest observed at run time.
type Subtest struct {
	Name     string  `json:"name"`
	Status   string  `json:"status"`
	ElapsedS float64 `json:"elapsed_s"`
	Note     string  `json:"note,omitempty"`
}

// RunResult is the outcome of one top-level test in a real run. Note holds the skip
// cause or the first failure line.
type RunResult struct {
	Status   string    `json:"status"`
	ElapsedS float64   `json:"elapsed_s"`
	Note     string    `json:"note,omitempty"`
	Subtests []Subtest `json:"subtests,omitempty"`
}

// PackageRun is the outcome of one package in a real run.
type PackageRun struct {
	Package  string  `json:"package"`
	Status   string  `json:"status"`
	ElapsedS float64 `json:"elapsed_s"`
	Note     string  `json:"note,omitempty"`
}

// RunKey identifies a top-level test across the static scan and the run.
type RunKey struct {
	Package string
	Name    string
}

// RunData is everything a `go test -json` run tells us.
type RunData struct {
	Packages map[string]PackageRun
	Tests    map[RunKey]*RunResult
}

type testEvent struct {
	Action      string
	Package     string
	Test        string
	Elapsed     float64
	Output      string
	FailedBuild string
}

var outputLine = regexp.MustCompile(`^\s*\S+\.go:\d+: (.*)$`)

func round3(f float64) float64 { return math.Round(f*1000) / 1000 }

// ParseTestJSON reads a `go test -json` stream.
func ParseTestJSON(r io.Reader) (RunData, error) {
	run := RunData{Packages: map[string]PackageRun{}, Tests: map[RunKey]*RunResult{}}
	notes := map[RunKey]string{}
	pkgOut := map[string][]string{}
	subs := map[RunKey][]Subtest{}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev testEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return RunData{}, fmt.Errorf("not a go test -json line: %w", err)
		}
		key := RunKey{Package: ev.Package, Name: ev.Test}
		switch ev.Action {
		case "output":
			if ev.Test == "" {
				pkgOut[ev.Package] = append(pkgOut[ev.Package], ev.Output)
				continue
			}
			if m := outputLine.FindStringSubmatch(strings.TrimRight(ev.Output, "\n")); m != nil && notes[key] == "" {
				notes[key] = m[1]
			}
		case StatusPass, StatusFail, StatusSkip:
			if ev.Test == "" {
				pr := PackageRun{Package: ev.Package, Status: ev.Action, ElapsedS: round3(ev.Elapsed)}
				if ev.FailedBuild != "" {
					pr.Note = "build failed"
				}
				run.Packages[ev.Package] = pr
				continue
			}
			if i := strings.Index(ev.Test, "/"); i >= 0 {
				parent := RunKey{Package: ev.Package, Name: ev.Test[:i]}
				subs[parent] = append(subs[parent], Subtest{Name: ev.Test, Status: ev.Action, ElapsedS: round3(ev.Elapsed), Note: notes[key]})
				continue
			}
			run.Tests[key] = &RunResult{Status: ev.Action, ElapsedS: round3(ev.Elapsed), Note: notes[key]}
		}
	}
	if err := sc.Err(); err != nil {
		return RunData{}, err
	}
	for key, list := range subs {
		res := run.Tests[key]
		if res == nil {
			continue
		}
		sort.SliceStable(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		res.Subtests = list
	}
	return run, nil
}

// RunModule runs `go test -json -count=1 ./...` in the module. A non-zero exit is not
// an error: failures are data. The race detector is deliberately not enabled.
func RunModule(ctx context.Context, m Module) (RunData, error) {
	cmd := exec.CommandContext(ctx, "go", "test", "-json", "-count=1", "./...")
	cmd.Dir = m.Dir
	var out bytes.Buffer
	cmd.Stdout = &out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return RunData{}, fmt.Errorf("go test in %s: %w", m.Rel, err)
	}
	run, perr := ParseTestJSON(&out)
	if perr != nil {
		return RunData{}, fmt.Errorf("go test in %s: %w (stderr: %s)", m.Rel, perr, strings.TrimSpace(stderr.String()))
	}
	return run, nil
}
