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

package runtime_test

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// allowedDependencies lists every non-standard-library package that
// port/runtime may depend on (openspec/changes/ego-runtime-001/design.md §D9):
// the command, tenancy, eventstream and port/behavior contracts, the packages
// eventstream brings in (internal/queue, internal/syncmap, uuid, atomic) and
// the protobuf runtime. The list covers the interfaces that arrive in S4-3, so
// it does not change then. It is intentionally narrower than archcheck's
// contract-allowlist rule: any new dependency should be a reviewed change to
// this list. An allowlist, rather than a denylist of GoAkt, also catches a
// dependency nobody thought to forbid.
var allowedDependencies = []string{
	"github.com/pablogore/ego/v4/command",
	"github.com/pablogore/ego/v4/tenancy",
	"github.com/pablogore/ego/v4/eventstream",
	"github.com/pablogore/ego/v4/port/behavior",
	"github.com/pablogore/ego/v4/internal/queue",
	"github.com/pablogore/ego/v4/internal/syncmap",
	"github.com/google/uuid",
	"go.uber.org/atomic",
	"google.golang.org/protobuf/",
}

// TestRuntimeDependsOnlyOnContracts walks the resolved import graph of
// port/runtime's production build with a real `go list -deps` subprocess, so
// transitive dependencies are checked too, not only the direct imports.
func TestRuntimeDependsOnlyOnContracts(t *testing.T) {
	goBin, err := exec.LookPath("go")
	require.NoError(t, err, "go toolchain not found on PATH")

	self := goList(t, goBin, "list", ".")
	require.Len(t, self, 1)

	deps := goList(t, goBin, "list", "-deps", ".")
	var checked int
	for _, dep := range deps {
		first, _, _ := strings.Cut(dep, "/")
		if dep == self[0] || !strings.Contains(first, ".") {
			continue // the package itself, or the standard library
		}
		checked++
		require.Truef(t, isAllowed(dep),
			"port/runtime must not depend on %q: it is a contract and may import only the standard library "+
				"and other contracts (openspec/changes/ego-runtime-001/design.md §D9)", dep)
	}
	require.NotZero(t, checked, "no non-standard dependency was checked, so this test proves nothing")
}

func isAllowed(dep string) bool {
	for _, allowed := range allowedDependencies {
		if dep == allowed || (strings.HasSuffix(allowed, "/") && strings.HasPrefix(dep, allowed)) {
			return true
		}
	}
	return false
}

func goList(t *testing.T, goBin string, args ...string) []string {
	t.Helper()
	cmd := exec.Command(goBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoErrorf(t, cmd.Run(), "go %s failed: %s", strings.Join(args, " "), stderr.String())
	return strings.Fields(stdout.String())
}
