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

package engine

import (
	"context"
	"reflect"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
)

// beTheSamePointer matches a pointer that is the very same object as want,
// the go-specs counterpart of testify's assert.Same. specs.Equal compares
// deeply, so it cannot tell two equal but distinct values apart.
func beTheSamePointer(want any) specs.Matcher {
	return specs.Satisfy("the same pointer as the expected one", func(got any) bool {
		w, g := reflect.ValueOf(want), reflect.ValueOf(got)
		return w.Kind() == reflect.Pointer && g.Kind() == reflect.Pointer &&
			w.Type() == g.Type() && w.Pointer() == g.Pointer()
	})
}

// panicValue runs fn and returns what it panicked with, or nil when it did not
// panic. Expect(panicValue(fn)).To(specs.BeNil()) is the go-specs counterpart
// of testify's NotPanics.
func panicValue(fn func()) (recovered any) {
	defer func() { recovered = recover() }()
	fn()
	return nil
}

// waitTimeout bounds every ctx.Eventually poll on a real actor system. It is a
// ceiling, not a delay: a poll returns as soon as its condition holds.
const waitTimeout = 10 * time.Second

// projectionRunning returns a poll function reporting whether the named
// projection is running. A lookup error counts as not running yet.
func projectionRunning(ctx context.Context, engine *Engine, name string) func() any {
	return func() any {
		running, err := engine.IsProjectionRunning(ctx, name)
		return err == nil && running
	}
}
