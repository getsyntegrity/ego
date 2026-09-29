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
	"errors"
	"fmt"
	"os"
	"os/exec"
)

func main() {
	if err := realMain(); err != nil {
		var reported *reportedError
		if !errors.As(err, &reported) { // failures already carry a ::error:: line
			fmt.Fprintf(os.Stderr, "modwait: %v\n", err)
		}
		os.Exit(1)
	}
}

// realMain wires the real environment into run: the inherited environment
// is checked for settings that would bypass verification, `go` must be on
// PATH, and every attempt runs through execProber.
func realMain() error {
	env := os.Environ()
	if err := checkPublicPath(env); err != nil {
		return err
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("the go toolchain is required on PATH: %w", err)
	}
	return run(os.Args[1:], os.Stdout, os.Stderr, systemClock{}, systemSleeper{}, &execProber{goBin: goBin, baseEnv: env})
}
