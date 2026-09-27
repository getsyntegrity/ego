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

package nats

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// hermeticGoEnv returns a copy of the current process environment with any
// existing GOWORK and GOFLAGS entries removed and GOWORK=off, GOFLAGS=
// (empty) appended, so the child `go` invocation below can never inherit a
// stray root go.work file or an ambient GOFLAGS value from the caller's
// shell — it always evaluates the module's default build tags in isolation,
// regardless of how the test binary itself was invoked.
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

// TestUnitTestClosureExcludesRuntimeAndRoot guards the regression tracked by
// #122: this module's default (untagged) unit-test closure must never again
// pull in the GoAkt runtime or the root package `ego`. The historical
// alias/sentinel compatibility checks against package `ego` still exist —
// see compat_test.go — but they run behind the `compat` build tag, in a
// separate CI lane (docs/ci.md, "Compatibility lane"), specifically so this
// command stays clean. The child `go list` runs under hermeticGoEnv() so a
// stray root go.work file or an inherited GOFLAGS can never change the
// result, independently of verify-module.sh's own GOWORK=off.
func TestUnitTestClosureExcludesRuntimeAndRoot(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-test", "./...")
	cmd.Env = hermeticGoEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps -test ./...: %v\n%s", err, out)
	}

	for _, dep := range strings.Fields(string(out)) {
		switch {
		case dep == "github.com/tochemey/goakt/v4" || strings.HasPrefix(dep, "github.com/tochemey/goakt/v4/"):
			t.Errorf("unit-test closure regressed: GoAkt package %q reappeared in `go list -deps -test ./...`; the historical ego-alias checks belong behind the `compat` build tag, not in the default test closure", dep)
		case dep == "github.com/pablogore/ego/v4":
			t.Errorf("unit-test closure regressed: root package %q reappeared in `go list -deps -test ./...`; the historical ego-alias checks belong behind the `compat` build tag, not in the default test closure", dep)
		}
	}
}
