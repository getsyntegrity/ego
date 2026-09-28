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

package pulsar

import (
	"os"
	"os/exec"
	"strconv"
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

// rootModule and compositionRoot are the root module's path and the
// composition root under it.
const (
	rootModule      = "github.com/getsyntegrity/ego/v4"
	compositionRoot = rootModule + "/compose"
)

// closureViolation returns why dep must not appear in this module's
// unit-test closure, or "" when it may. It rejects the GoAkt runtime, the
// root package `ego` (#122) and the composition root, compose and anything
// under it (ego-arch-004 design §D7): archcheck's
// external-adapter-no-composition rule never reads _test.go files, so this
// guard covers the test side.
func closureViolation(dep string) string {
	switch {
	case dep == "github.com/tochemey/goakt/v4" || strings.HasPrefix(dep, "github.com/tochemey/goakt/v4/"):
		return "unit-test closure regressed: GoAkt package " + strconv.Quote(dep) + " reappeared in `go list -deps -test ./...`; the historical ego-alias checks belong in the test/compat module, not in this module's test closure"
	case dep == rootModule:
		return "unit-test closure regressed: root package " + strconv.Quote(dep) + " reappeared in `go list -deps -test ./...`; the historical ego-alias checks belong in the test/compat module, not in this module's test closure"
	case dep == compositionRoot || strings.HasPrefix(dep, compositionRoot+"/"):
		return "adapter depends on the composition root: " + strconv.Quote(dep) + " appeared in `go list -deps -test ./...`; an adapter module must not import compose or anything under it, in production code or tests (ego-arch-004 design §D7); end-to-end tests that need a running App belong in the test/compat module"
	default:
		return ""
	}
}

// TestUnitTestClosureExcludesRuntimeAndRoot guards the regression tracked by
// #122: this module's unit-test closure must never again pull in the GoAkt
// runtime or the root package `ego`, and, since ego-arch-004 (design §D7),
// the composition root. The historical alias/sentinel
// compatibility checks against package `ego` still exist, but they live in
// the separate, unreleased test/compat module (ADR ego-arch-006, slice S1;
// docs/ci.md, "Compatibility checks: the test/compat module"), specifically
// so this command stays clean. The child `go list` runs under
// hermeticGoEnv() so a stray root go.work file or an inherited GOFLAGS can
// never change the result, independently of verify-module.sh's own GOWORK=off.
func TestUnitTestClosureExcludesRuntimeAndRoot(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-test", "./...")
	cmd.Env = hermeticGoEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps -test ./...: %v\n%s", err, out)
	}

	for _, dep := range strings.Fields(string(out)) {
		if msg := closureViolation(dep); msg != "" {
			t.Error(msg)
		}
	}
}

// TestClosureGuardRejectsCompositionRoot pins what the closure guard
// rejects, without a subprocess: the composition root (compose and
// anything under it) as well as GoAkt and the root package, matched by
// whole path segment.
func TestClosureGuardRejectsCompositionRoot(t *testing.T) {
	for _, dep := range []string{
		"github.com/getsyntegrity/ego/v4/compose",
		"github.com/getsyntegrity/ego/v4/compose/goakt",
		"github.com/getsyntegrity/ego/v4/compose/internal/lifecycle",
		"github.com/getsyntegrity/ego/v4",
		"github.com/tochemey/goakt/v4/actor",
	} {
		if closureViolation(dep) == "" {
			t.Errorf("closureViolation(%q) = \"\", want a rejection", dep)
		}
	}
	for _, dep := range []string{
		"github.com/getsyntegrity/ego/v4/composer",
		"github.com/getsyntegrity/ego/v4/port/publishing",
		"github.com/getsyntegrity/ego/v4/egopb",
	} {
		if msg := closureViolation(dep); msg != "" {
			t.Errorf("closureViolation(%q) = %q, want it allowed", dep, msg)
		}
	}
}
