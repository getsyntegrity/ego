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

package migration

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// hermeticGoEnv returns a copy of the current process environment with any
// existing GOWORK and GOFLAGS entries removed and GOWORK=off, GOFLAGS=
// (empty) appended, so the child `go list` invocation below can never
// inherit a stray root go.work file or an ambient GOFLAGS value from the
// caller's shell. Mirrors the publisher modules' closure_test.go helper
// (#122).
func hermeticGoEnv() []string {
	base := os.Environ()
	env := make([]string, 0, len(base)+2)
	for _, kv := range base {
		if strings.HasPrefix(kv, "GOWORK=") || strings.HasPrefix(kv, "GOFLAGS=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GOWORK=off", "GOFLAGS=")
}

// TestProductionClosureExcludesRootAndGoAkt guards the S4-1 removal of the
// last archcheck baseline entry (#147, ego-arch-001 §3): migration's
// production build must never again reach the root package `ego` or the
// GoAkt runtime. Only `go list -deps .` (no -test) is checked — migration's
// tests may still import `ego` (archcheck's application-no-runtime rule
// evaluates production edges only), so a test-only import of `ego` here is
// not a regression this guard cares about.
func TestProductionClosureExcludesRootAndGoAkt(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", ".")
	cmd.Env = hermeticGoEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps .: %v\n%s", err, out)
	}

	for _, dep := range strings.Fields(string(out)) {
		switch {
		case dep == "github.com/tochemey/goakt/v4" || strings.HasPrefix(dep, "github.com/tochemey/goakt/v4/"):
			t.Errorf("migration's production closure must not reach the GoAkt runtime; got %q", dep)
		case dep == "github.com/pablogore/ego/v4":
			t.Errorf("migration's production closure must not reach the root package ego; got %q", dep)
		}
	}
}
