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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// probeTimeout bounds one `go mod download` so a hung connection cannot
// outlive the wait's own deadline by more than this.
const probeTimeout = 3 * time.Minute

// strippedEnv are the variables the probe removes from the inherited
// environment, because each one would make the probe take a different path
// than a consumer's plain `go get`:
//   - GONOSUMDB/GONOSUMCHECK/GOPRIVATE/GOINSECURE/GONOPROXY skip or redirect
//     checksum verification or the proxy for matching modules.
//   - GOFLAGS can carry -insecure or -mod=... .
//   - GOWORK is forced to "off" and GOMODCACHE to a fresh directory below.
//   - GOENV is forced to "off" so a `go env -w` file on the machine cannot
//     reintroduce any of the above.
//
// GOPROXY and GOSUMDB are deliberately NOT here: they are inherited, so the
// job's own settings (Go's defaults on a GitHub runner) are what is tested.
var strippedEnv = map[string]bool{
	"GONOSUMDB": true, "GONOSUMCHECK": true, "GOPRIVATE": true, "GOINSECURE": true,
	"GONOPROXY": true, "GOFLAGS": true, "GOWORK": true, "GOMODCACHE": true, "GOENV": true,
}

// probeEnv builds the probe's environment from base: overrides stripped,
// GOWORK/GOENV off, GOMODCACHE pointing at a fresh empty directory (a cached
// copy would skip the very checksum lookup this wait exists to observe).
// It never sets GOSUMDB, GOFLAGS or anything else that weakens verification.
func probeEnv(base []string, modCache string) []string {
	env := make([]string, 0, len(base)+3)
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if strippedEnv[k] {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GOWORK=off", "GOENV=off", "GOMODCACHE="+modCache)
}

var (
	modulePathRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~/-]*$`)
	versionRe    = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
)

// validateTarget rejects a module path or version that could be read as a
// flag or carry anything but the characters Go allows, before either reaches
// a subprocess. Versions must be full semantic versions (tags, pseudo
// versions): a branch name or query such as "latest" is not a version.
func validateTarget(module, version string) error {
	if !modulePathRe.MatchString(module) || strings.Contains(module, "..") {
		return fmt.Errorf("malformed module path %q", module)
	}
	if !versionRe.MatchString(version) {
		return fmt.Errorf("malformed version %q: want a full semantic version such as v1.2.3", version)
	}
	return nil
}

// probeCommand builds the exact command of one attempt:
// `go mod download -json <module>@<version>`, run in dir with probeEnv.
func probeCommand(ctx context.Context, goBin, module, version, dir, modCache string, baseEnv []string) *exec.Cmd {
	// module and version are validated by validateTarget (parseConfig and
	// Probe) before this point; there is no shell involved.
	cmd := exec.CommandContext(ctx, goBin, "mod", "download", "-json", module+"@"+version) //nolint:gosec // inputs validated by validateTarget, no shell
	cmd.Dir = dir
	cmd.Env = probeEnv(baseEnv, modCache)
	return cmd
}

// checkPublicPath refuses to run when the inherited environment already
// disables the public path being tested: waiting on a path that skips
// verification would prove nothing about what `go get` needs.
func checkPublicPath(env []string) error {
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		switch {
		case k == "GOSUMDB" && v == "off":
			return errors.New("refusing to run with GOSUMDB=off: the wait must exercise checksum-database verification")
		case k == "GOPROXY" && v == "off":
			return errors.New("refusing to run with GOPROXY=off: the wait must exercise the public proxy")
		case k == "GOFLAGS" && strings.Contains(v, "-insecure"):
			return errors.New("refusing to run with -insecure in GOFLAGS")
		}
	}
	return nil
}

// execProber runs the real `go` binary.
type execProber struct {
	goBin   string
	baseEnv []string
}

// Probe makes one attempt in a fresh temporary directory with a fresh
// temporary module cache, both removed afterwards.
func (p *execProber) Probe(ctx context.Context, module, version string) ProbeResult {
	if err := validateTarget(module, version); err != nil {
		return ProbeResult{Output: "modwait: " + err.Error()}
	}
	root, err := os.MkdirTemp("", "modwait-")
	if err != nil {
		return ProbeResult{Output: "modwait: cannot create a temporary directory: " + err.Error()}
	}
	defer p.cleanup(root)

	work := filepath.Join(root, "work")
	modCache := filepath.Join(root, "modcache")
	for _, d := range []string{work, modCache} {
		if err := os.Mkdir(d, 0o700); err != nil {
			return ProbeResult{Output: "modwait: cannot create a temporary directory: " + err.Error()}
		}
	}
	// A throwaway main module keeps `go mod download` off any surrounding
	// go.mod or go.work. Its name is not the checked module, so there is
	// no replace or self-reference.
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte("module modwait.invalid/probe\n\ngo 1.21\n"), 0o600); err != nil {
		return ProbeResult{Output: "modwait: cannot write the probe go.mod: " + err.Error()}
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := probeCommand(ctx, p.goBin, module, version, work, modCache, p.baseEnv)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	if ctx.Err() != nil && runErr != nil {
		return ProbeResult{Output: fmt.Sprintf("modwait: probe timed out or was cancelled after at most %s (%v)", probeTimeout, ctx.Err())}
	}
	return parseDownload(stdout.String(), stderr.String(), runErr)
}

// parseDownload turns `go mod download -json` results into a ProbeResult.
// The attempt passes only when go exited 0 and reported no JSON "Error".
// Otherwise Output carries the JSON Error and stderr for Classify.
func parseDownload(stdout, stderr string, runErr error) ProbeResult {
	var parsed struct{ Error string }
	jsonErr := ""
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &parsed); err == nil {
		jsonErr = parsed.Error
	}
	if runErr == nil && jsonErr == "" {
		return ProbeResult{OK: true}
	}

	var parts []string
	for _, s := range []string{jsonErr, strings.TrimSpace(stderr)} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 && runErr != nil {
		parts = append(parts, runErr.Error())
	}
	return ProbeResult{Output: strings.Join(parts, "\n")}
}

// cleanup removes the attempt's temporary tree. Go writes the module cache
// read-only, so a plain RemoveAll fails on it; `go clean -modcache` is Go's
// own supported way to delete it, run against the temporary GOMODCACHE only.
// No permission walk is needed (and none races with the filesystem).
func (p *execProber) cleanup(root string) {
	modCache := filepath.Join(root, "modcache")
	clean := exec.Command(p.goBin, "clean", "-modcache") //nolint:gosec // fixed arguments, goBin comes from exec.LookPath
	clean.Dir = root
	clean.Env = probeEnv(p.baseEnv, modCache)
	_ = clean.Run()
	_ = os.RemoveAll(root)
}
