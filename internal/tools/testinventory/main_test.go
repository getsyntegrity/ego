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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeRepoFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
}

func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeRepoFile(t, root, "go.mod", "module example.com/repo\n\ngo 1.22\n")
	writeRepoFile(t, root, "a/a_test.go", `package a
import ("net"; "testing")
func TestPlain(t *testing.T) {}
func TestListen(t *testing.T) { _, _ = net.Listen("tcp", "127.0.0.1:0") }
`)
	writeRepoFile(t, root, "docs/testing/inventory-overrides.json", `{"example_modules":[],"overrides":[]}`)
	return root
}

func runCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String() + errOut.String()
}

func TestCheckPassesOnAFreshInventoryAndFailsOnAnUnclassifiedTest(t *testing.T) {
	root := newRepo(t)

	code, out := runCLI(t, "-root", root, "-update")
	require.Equal(t, 0, code, out)
	assert.FileExists(t, filepath.Join(root, "docs/testing/inventory.json"))
	assert.FileExists(t, filepath.Join(root, "docs/testing/inventory.md"))

	code, out = runCLI(t, "-root", root, "-check")
	require.Equal(t, 0, code, out)

	writeRepoFile(t, root, "a/b_test.go", "package a\nimport \"testing\"\nfunc TestNewAndUnlisted(t *testing.T) {}\n")
	code, out = runCLI(t, "-root", root, "-check")
	assert.Equal(t, 1, code)
	assert.Contains(t, out, "missing from inventory")
	assert.Contains(t, out, "TestNewAndUnlisted")

	code, out = runCLI(t, "-root", root, "-update")
	require.Equal(t, 0, code, out)
	code, out = runCLI(t, "-root", root, "-check")
	assert.Equal(t, 0, code, out)
}

func TestCheckFailsWithoutAnInventory(t *testing.T) {
	code, out := runCLI(t, "-root", newRepo(t), "-check")
	assert.Equal(t, 1, code)
	assert.Contains(t, out, "inventory.json")
}

func TestCheckFailsWhenAnOverridePointsAtNothing(t *testing.T) {
	root := newRepo(t)
	code, out := runCLI(t, "-root", root, "-update")
	require.Equal(t, 0, code, out)
	writeRepoFile(t, root, "docs/testing/inventory-overrides.json", `{"example_modules":[],"overrides":[{"dir":"a","test":"TestGone","lane":"unit","reason":"x"}]}`)
	code, out = runCLI(t, "-root", root, "-check")
	assert.Equal(t, 1, code)
	assert.Contains(t, out, "does not exist")
}

func TestExactlyOneModeIsRequired(t *testing.T) {
	code, out := runCLI(t, "-root", newRepo(t))
	assert.Equal(t, 2, code)
	assert.Contains(t, out, "-check")
	code, _ = runCLI(t, "-root", newRepo(t), "-check", "-update")
	assert.Equal(t, 2, code)
}
