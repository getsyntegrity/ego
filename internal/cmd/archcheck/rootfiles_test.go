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
	"path/filepath"
	"strings"
	"testing"
)

// TestRootGoFiles_ListsOnlyRootLevelGoSources checks that the guard reports
// every *.go file directly in the module root (tests included) and ignores
// Go files in subdirectories and non-Go files.
func TestRootGoFiles_ListsOnlyRootLevelGoSources(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/m\n")
	writeFile(t, filepath.Join(dir, "engine.go"), "package m\n")
	writeFile(t, filepath.Join(dir, "engine_test.go"), "package m\n")
	writeFile(t, filepath.Join(dir, "readme.md"), "# m\n")
	writeFile(t, filepath.Join(dir, "sub", "sub.go"), "package sub\n")

	got, err := rootGoFiles(dir)
	if err != nil {
		t.Fatalf("rootGoFiles: %v", err)
	}
	want := []string{"engine.go", "engine_test.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("rootGoFiles = %v, want %v", got, want)
	}
}

// TestRunCheck_RootGoFileFails: a Go file at the module root fails the
// check even when the import graph is otherwise clean.
func TestRunCheck_RootGoFileFails(t *testing.T) {
	requireGo(t)

	dir, _ := writeRunFixture(t, false)
	writeFile(t, filepath.Join(dir, "stray.go"), "package archcheckfixture\n")
	var stdout strings.Builder
	err := runCheck(dir, nil, &stdout)
	if err == nil {
		t.Fatalf("runCheck() = nil error, want the root Go file guard to fail; output:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "stray.go") || !strings.Contains(stdout.String(), "root-no-go-files") {
		t.Errorf("stdout = %q, want it to name the guard and stray.go", stdout.String())
	}
}
