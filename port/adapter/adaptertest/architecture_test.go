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

package adaptertest_test

import (
	"bytes"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const (
	adapterPath     = "github.com/getsyntegrity/ego/port/adapter"
	adaptertestPath = adapterPath + "/adaptertest"
)

// Spec scenario "the architecture tests hold the line": the resolved
// import graph of adaptertest (a real `go list -deps`, so transitive
// dependencies count) holds only the standard library and port/adapter,
// so a nested adapter module can run the suite without the runtime
// (ego-arch-004 design §D8).
func TestAdaptertestDependsOnlyOnStdlibAndAdapter(t *testing.T) {
	deps := goListDeps(t, ".")
	if !slices.Contains(deps, adaptertestPath) || !slices.Contains(deps, adapterPath) || !slices.Contains(deps, "testing") {
		t.Fatalf("go list -deps . = %v, want it to list %s, %s and testing; the test would prove nothing", deps, adaptertestPath, adapterPath)
	}
	for _, dep := range deps {
		if dep == adaptertestPath || dep == adapterPath || isStdlib(dep) {
			continue
		}
		t.Errorf("port/adapter/adaptertest must not depend on %q: it may import only the standard library and port/adapter (openspec/changes/ego-arch-004/design.md §D8)", dep)
	}
}

// isStdlib reports whether an import path belongs to the standard library:
// its first path element has no dot.
func isStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func goListDeps(t *testing.T, pkg string) []string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go toolchain not found on PATH: %v", err)
	}
	cmd := exec.Command(goBin, "list", "-deps", pkg)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go list -deps %s failed: %v\n%s", pkg, err, stderr.String())
	}
	return strings.Fields(stdout.String())
}
