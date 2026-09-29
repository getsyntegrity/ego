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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forbiddenKeys are the settings that would weaken or bypass the public
// path a consumer takes; none may appear in the probe's environment unless
// the test expects it.
var forbiddenKeys = []string{"GOSUMDB", "GONOSUMDB", "GONOSUMCHECK", "GOPRIVATE", "GOINSECURE", "GONOPROXY", "GOFLAGS"}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}

func TestProbeEnv_StripsOverridesAndKeepsPublicSettings(t *testing.T) {
	base := []string{
		"PATH=/usr/bin", "HOME=/home/ci",
		"GOPROXY=https://proxy.golang.org,direct", "GOSUMDB=sum.golang.org",
		"GONOSUMDB=github.com/getsyntegrity/*", "GONOSUMCHECK=1", "GOPRIVATE=github.com/getsyntegrity",
		"GOINSECURE=github.com/getsyntegrity", "GONOPROXY=github.com/getsyntegrity",
		"GOFLAGS=-insecure -mod=mod", "GOWORK=/some/go.work", "GOMODCACHE=/stale/cache", "GOENV=/x/env",
	}
	got := envMap(probeEnv(base, "/tmp/fresh-modcache"))

	for _, k := range forbiddenKeys {
		v, present := got[k]
		if k == "GOSUMDB" {
			if v != "sum.golang.org" {
				t.Errorf("GOSUMDB = %q, want the inherited default kept", v)
			}
			continue
		}
		if present {
			t.Errorf("%s is set to %q in the probe environment, it must be unset", k, v)
		}
	}
	if got["GOPROXY"] != "https://proxy.golang.org,direct" {
		t.Errorf("GOPROXY = %q, want the inherited value", got["GOPROXY"])
	}
	if got["GOMODCACHE"] != "/tmp/fresh-modcache" {
		t.Errorf("GOMODCACHE = %q, want the fresh temporary cache", got["GOMODCACHE"])
	}
	if got["GOWORK"] != "off" {
		t.Errorf("GOWORK = %q, want off", got["GOWORK"])
	}
	if got["GOENV"] != "off" {
		t.Errorf("GOENV = %q, want off (a `go env -w` file must not weaken the path)", got["GOENV"])
	}
	if got["PATH"] != "/usr/bin" || got["HOME"] != "/home/ci" {
		t.Errorf("PATH/HOME were not preserved: %v", got)
	}
}

func TestProbeEnv_DoesNotInventGOSUMDBWhenUnset(t *testing.T) {
	got := envMap(probeEnv([]string{"PATH=/usr/bin"}, "/m"))
	for _, k := range forbiddenKeys {
		if _, present := got[k]; present {
			t.Errorf("%s must not be introduced by the probe", k)
		}
	}
}

func TestProbeCommand_ArgsAndDir(t *testing.T) {
	cmd := probeCommand(context.Background(), "go", "github.com/getsyntegrity/ego/v4", "v4.0.0", "/tmp/work", "/tmp/mod", []string{"PATH=/usr/bin"})
	want := []string{"go", "mod", "download", "-json", "github.com/getsyntegrity/ego/v4@v4.0.0"}
	if strings.Join(cmd.Args, " ") != strings.Join(want, " ") {
		t.Errorf("Args = %v, want %v", cmd.Args, want)
	}
	if cmd.Dir != "/tmp/work" {
		t.Errorf("Dir = %q, want the fresh temporary directory", cmd.Dir)
	}
	for _, a := range cmd.Args {
		for _, bad := range []string{"replace", "-modfile", "-insecure", "-x"} {
			if a == bad {
				t.Errorf("unexpected argument %q", a)
			}
		}
	}
	env := envMap(cmd.Env)
	for _, k := range []string{"GONOSUMDB", "GOPRIVATE", "GOINSECURE", "GOFLAGS"} {
		if _, ok := env[k]; ok {
			t.Errorf("%s present in the command environment", k)
		}
	}
	if _, ok := env["GOSUMDB"]; ok {
		t.Errorf("GOSUMDB must not be forced by the probe; it is inherited or left at Go's default")
	}
}

func TestCheckPublicPath(t *testing.T) {
	for name, tc := range map[string]struct {
		env     []string
		wantErr string
	}{
		"clean":          {[]string{"PATH=/x"}, ""},
		"defaults":       {[]string{"GOSUMDB=sum.golang.org", "GOPROXY=https://proxy.golang.org,direct"}, ""},
		"sumdb off":      {[]string{"GOSUMDB=off"}, "GOSUMDB=off"},
		"proxy off":      {[]string{"GOPROXY=off"}, "GOPROXY=off"},
		"insecure flags": {[]string{"GOFLAGS=-insecure"}, "GOFLAGS"},
	} {
		t.Run(name, func(t *testing.T) {
			err := checkPublicPath(tc.env)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestParseDownload(t *testing.T) {
	tests := []struct {
		name    string
		stdout  string
		stderr  string
		runErr  error
		wantOK  bool
		wantOut []string
	}{
		{"success", `{"Path":"m","Version":"v1.0.0","Sum":"h1:x"}`, "", nil, true, nil},
		{"json error and stderr", `{"Path":"m","Error":"reading https://sum.golang.org/lookup/m@v1: 404 Not Found"}`, "go: extra stderr line", errors.New("exit status 1"), false,
			[]string{"404 Not Found", "extra stderr line"}},
		{"json error but exit 0", `{"Error":"boom"}`, "", nil, false, []string{"boom"}},
		{"failure with stderr only", "", "dial tcp: no such host", errors.New("exit status 1"), false, []string{"no such host"}},
		{"failure with nothing", "", "", errors.New("signal: killed"), false, []string{"signal: killed"}},
		{"unparseable stdout on failure", "not json", "oops", errors.New("exit status 1"), false, []string{"oops"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDownload(tt.stdout, tt.stderr, tt.runErr)
			if got.OK != tt.wantOK {
				t.Fatalf("OK = %v, want %v (output %q)", got.OK, tt.wantOK, got.Output)
			}
			for _, w := range tt.wantOut {
				if !strings.Contains(got.Output, w) {
					t.Errorf("Output %q lacks %q", got.Output, w)
				}
			}
		})
	}
}

// writeFakeGo installs a stand-in `go` that records its environment and
// working directory, and prints canned output, so the real exec plumbing and
// the temp-dir cleanup are tested with no network.
func writeFakeGo(t *testing.T, body string) (goBin, record string) {
	t.Helper()
	dir := t.TempDir()
	record = filepath.Join(dir, "record")
	goBin = filepath.Join(dir, "go")
	script := "#!/bin/sh\n" +
		"{ echo \"ARGS=$*\"; echo \"PWD=$(pwd)\"; env; } > " + record + "\n" +
		"mkdir -p \"$GOMODCACHE/cache/download\" && touch \"$GOMODCACHE/cache/download/f\" && chmod -R a-w \"$GOMODCACHE/cache\"\n" +
		body + "\n"
	if err := os.WriteFile(goBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return goBin, record
}

func TestExecProber_RunsInFreshDirsAndCleansUp(t *testing.T) {
	goBin, record := writeFakeGo(t, `echo '{"Path":"m","Version":"v1.0.0"}'`)
	p := &execProber{goBin: goBin, baseEnv: []string{"PATH=" + os.Getenv("PATH"), "GOSUMDB=sum.golang.org", "GONOSUMDB=evil", "GOFLAGS=-insecure"}}
	res := p.Probe(context.Background(), "github.com/getsyntegrity/ego/v4", "v4.0.0")
	if !res.OK {
		t.Fatalf("Probe not OK: %q", res.Output)
	}

	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	rec := string(data)
	for _, want := range []string{"ARGS=mod download -json github.com/getsyntegrity/ego/v4@v4.0.0", "GOSUMDB=sum.golang.org", "GOWORK=off"} {
		if !strings.Contains(rec, want) {
			t.Errorf("record lacks %q:\n%s", want, rec)
		}
	}
	for _, bad := range []string{"GONOSUMDB=", "GOFLAGS=", "GOPRIVATE=", "GOINSECURE="} {
		if strings.Contains(rec, bad) {
			t.Errorf("record contains %q, the probe must strip it:\n%s", bad, rec)
		}
	}

	// The temporary working dir and module cache must be gone (including the
	// read-only cache tree the fake go created).
	var workDir, modCache string
	for _, l := range strings.Split(rec, "\n") {
		if v, ok := strings.CutPrefix(l, "PWD="); ok {
			workDir = v
		}
		if v, ok := strings.CutPrefix(l, "GOMODCACHE="); ok {
			modCache = v
		}
	}
	if workDir == "" || modCache == "" {
		t.Fatalf("could not read PWD/GOMODCACHE from record:\n%s", rec)
	}
	for _, d := range []string{workDir, modCache} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("%s still exists after the attempt (err=%v)", d, err)
		}
	}
	if !strings.Contains(workDir, "modwait-") {
		t.Errorf("workDir %q is not a modwait temp dir", workDir)
	}
}

func TestExecProber_FailureCarriesOutput(t *testing.T) {
	goBin, _ := writeFakeGo(t, `echo '{"Error":"reading https://sum.golang.org/lookup/m@v1: 404 Not Found"}'; echo "stderr line" >&2; exit 1`)
	p := &execProber{goBin: goBin, baseEnv: []string{"PATH=" + os.Getenv("PATH")}}
	res := p.Probe(context.Background(), "m", "v1")
	if res.OK {
		t.Fatal("want a failed probe")
	}
	if c := Classify(res.Output); c.Class != Transient {
		t.Errorf("class = %v (%q), want transient", c.Class, res.Output)
	}
}
