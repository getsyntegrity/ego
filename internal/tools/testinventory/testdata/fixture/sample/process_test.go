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

package sample

import (
	"go/parser"
	"os/exec"
	"testing"
)

func TestGoList(t *testing.T) {
	_, _ = exec.Command("go", "list", "./...").CombinedOutput()
}

func TestOtherBinary(t *testing.T) {
	_, _ = exec.Command("psql", "--version").CombinedOutput()
}

func TestGoListThroughVariable(t *testing.T) {
	goBin := "go"
	_, _ = exec.Command(goBin, "build", "./...").CombinedOutput()
}

func TestParsesSource(t *testing.T) {
	_, _ = parser.ParseDir(nil, ".", nil, 0)
}

func TestGoViaLookPath(t *testing.T) {
	args := []string{"list", "./..."}
	goBin, _ := exec.LookPath("go")
	_, _ = exec.Command(goBin, args...).CombinedOutput()
}
