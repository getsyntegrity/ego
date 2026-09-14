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

package ego

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTenancyArchitecture enforces design.md's ratified "Import-graph
// tooling" decision (EGO-TENANT-001): the tenancy core package MUST NOT
// acquire a dependency on GoAkt, net/http, JWT libraries, Ory libraries,
// any transport adapter, or any application/runtime package of this repo
// (including ego itself) — only the Go standard library.
//
// The mechanism is a real `go list -deps ./tenancy/...` subprocess, not a
// source-text scan (unlike logger_architecture_test.go's substring scan,
// which was deliberately NOT extended here per design.md — a substring
// scan over tenancy/*.go can only ever see direct, literal imports in
// files it happens to read; it cannot see what a direct dependency itself
// imports). `go list -deps` walks the real, resolved import graph, so it
// also catches TRANSITIVE dependencies: a stdlib-looking direct import
// that itself pulls in GoAkt would still be caught here.
//
// Every import path `go list` returns is classified as standard library
// if and only if its first path segment (everything before the first
// '/') contains no dot: module-qualified import paths always have a
// dotted first segment (a registrable domain, e.g. "github.com",
// "go.uber.org"), while no package in the Go standard library ever does
// (e.g. "context", "net/http", "unicode/utf8").
func TestTenancyArchitecture(t *testing.T) {
	goBin, err := tenancyArchitectureGoBinary()
	require.NoError(t, err, "go toolchain not found")

	root, err := os.Getwd()
	require.NoError(t, err)

	// The tenancy package(s) themselves are always present in their own
	// `-deps` output; they are the subject under test, not a dependency
	// it acquired, so they must be excluded from the forbidden-import
	// check below rather than trivially failing it.
	tenancyPackages := tenancyArchitectureGoList(t, goBin, root, "list", "./tenancy/...")
	require.NotEmpty(t, tenancyPackages, "go list ./tenancy/... returned no packages, so it proves nothing")

	self := make(map[string]struct{}, len(tenancyPackages))
	for _, pkg := range tenancyPackages {
		self[pkg] = struct{}{}
	}

	deps := tenancyArchitectureGoList(t, goBin, root, "list", "-deps", "./tenancy/...")
	require.NotEmpty(t, deps, "go list -deps ./tenancy/... returned no dependencies, so it proves nothing")

	var checked int
	for _, dep := range deps {
		if _, isSelf := self[dep]; isSelf {
			continue
		}
		checked++

		first, _, _ := strings.Cut(dep, "/")
		require.Falsef(t, strings.Contains(first, "."),
			"tenancy/ must not depend on %q: the tenancy core package must be stdlib-only "+
				"(design.md \"Import-graph tooling\" decision) — it is a leaf package with no "+
				"GoAkt, transport, auth, or first-party runtime dependency", dep)
	}
	require.NotZero(t, checked, "no external dependency was checked against the allowlist, so this test proves nothing")
}

// tenancyArchitectureGoList runs `go <args...>` from dir and returns the
// resulting import paths, one per whitespace-separated token of stdout.
// It is a real subprocess invocation (os/exec), not a static scan of
// source text, per design.md's ratified decision for this conformance
// check.
func tenancyArchitectureGoList(t *testing.T, goBin, dir string, args ...string) []string {
	t.Helper()

	cmd := exec.Command(goBin, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	require.NoErrorf(t, err, "%s %s failed: %s", goBin, strings.Join(args, " "), stderr.String())

	return strings.Fields(stdout.String())
}

// tenancyArchitectureGoBinary locates the Go toolchain binary. It prefers
// PATH, then falls back to GOROOT/bin/go, so the test still runs in an
// environment where `go` is not on PATH but a test binary was still built
// with a known toolchain.
func tenancyArchitectureGoBinary() (string, error) {
	if p, err := exec.LookPath("go"); err == nil {
		return p, nil
	}

	if root := runtime.GOROOT(); root != "" {
		candidate := filepath.Join(root, "bin", "go")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("go toolchain not found on PATH or GOROOT (GOROOT=%q)", runtime.GOROOT())
}
