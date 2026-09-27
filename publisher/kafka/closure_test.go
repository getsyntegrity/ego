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

package kafka

import (
	"os/exec"
	"strings"
	"testing"
)

// TestUnitTestClosureExcludesRuntimeAndRoot guards the regression tracked by
// #122: this module's default (untagged) unit-test closure must never again
// pull in the GoAkt runtime or the root package `ego`. The historical
// alias/sentinel compatibility checks against package `ego` still exist —
// see compat_test.go — but they run behind the `compat` build tag, in a
// separate CI lane (docs/ci.md, "Compatibility lane"), specifically so this
// command stays clean. GOWORK is inherited from the caller (verify-module.sh
// and the local test-execution rule both set GOWORK=off) so a stray root
// go.work file can never fold this module back into the root module's graph.
func TestUnitTestClosureExcludesRuntimeAndRoot(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-test", "./...").CombinedOutput()
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
