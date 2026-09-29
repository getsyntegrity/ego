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

package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

// errText returns err's message, or "<nil>" when err is nil, so a nil error fails
// a message comparison instead of panicking.
func errText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

func TestChain(t *testing.T) {
	specs.Describe(t, "Chain runs its runners according to the configured error policy", func(s *specs.Spec) {
		s.It("With AddRunner FailFast", func(ctx *specs.Context) {
			var (
				calledFn1 = false
				calledFn2 = false
				calledFn3 = false
			)

			fn1 := func() error { calledFn1 = true; return errors.New("err1") }
			fn2 := func() error { calledFn2 = true; return errors.New("err2") }
			fn3 := func() error { calledFn3 = true; return errors.New("err3") }

			chain := New(WithFailFast()).
				AddRunner(fn1).
				AddRunner(fn2).
				AddRunner(fn3)
			actual := chain.Run()

			ctx.Expect(errText(actual)).ToEqual("err1")
			ctx.Expect(calledFn1).To(specs.BeTrue())
			ctx.Expect(calledFn2).To(specs.BeFalse())
			ctx.Expect(calledFn3).To(specs.BeFalse())
		})

		s.It("With AddRunners FailFast", func(ctx *specs.Context) {
			var (
				calledFn1 = false
				calledFn2 = false
				calledFn3 = false
			)

			fn1 := func() error { calledFn1 = true; return errors.New("err1") }
			fn2 := func() error { calledFn2 = true; return errors.New("err2") }
			fn3 := func() error { calledFn3 = true; return errors.New("err3") }

			chain := New(WithFailFast()).AddRunners(fn1, fn2, fn3)
			actual := chain.Run()

			ctx.Expect(errText(actual)).ToEqual("err1")
			ctx.Expect(calledFn1).To(specs.BeTrue())
			ctx.Expect(calledFn2).To(specs.BeFalse())
			ctx.Expect(calledFn3).To(specs.BeFalse())
		})

		s.It("With AddRunner ReturnAll", func(ctx *specs.Context) {
			var (
				calledFn1 = false
				calledFn2 = false
				calledFn3 = false
			)

			fn1 := func() error { calledFn1 = true; return errors.New("err1") }
			fn2 := func() error { calledFn2 = true; return errors.New("err2") }
			fn3 := func() error { calledFn3 = true; return nil }

			chain := New(WithRunAll()).
				AddRunner(fn1).
				AddRunner(fn2).
				AddRunner(fn3)
			actual := chain.Run()

			ctx.Expect(errText(actual)).ToEqual("err1; err2")
			ctx.Expect(calledFn1).To(specs.BeTrue())
			ctx.Expect(calledFn2).To(specs.BeTrue())
			ctx.Expect(calledFn3).To(specs.BeTrue())
		})
	})
}

func TestAddContextRunnerIf(t *testing.T) {
	specs.Describe(t, "AddContextRunnerIf adds a context runner only when its condition holds", func(s *specs.Spec) {
		bg := context.Background()

		s.It("FailFast - condition true, error returned", func(ctx *specs.Context) {
			called := false
			fn := func(_ context.Context) error {
				called = true
				return errors.New("err1")
			}
			chain := New(WithFailFast(), WithContext(bg)).AddContextRunnerIf(true, fn)
			ctx.Expect(errText(chain.Run())).ToEqual("err1")
			ctx.Expect(called).To(specs.BeTrue())
		})

		s.It("FailFast - condition false, fn not called", func(ctx *specs.Context) {
			called := false
			fn := func(_ context.Context) error {
				called = true
				return errors.New("err1")
			}
			chain := New(WithFailFast()).AddContextRunnerIf(false, fn)
			ctx.Expect(chain.Run()).To(specs.BeNil())
			ctx.Expect(called).To(specs.BeFalse())
		})

		s.It("ReturnAll - condition true, error returned", func(ctx *specs.Context) {
			called := false
			fn := func(_ context.Context) error {
				called = true
				return errors.New("err2")
			}
			chain := New(WithRunAll()).AddContextRunnerIf(true, fn)
			ctx.Expect(errText(chain.Run())).ToEqual("err2")
			ctx.Expect(called).To(specs.BeTrue())
		})

		s.It("ReturnAll - condition false, fn not called", func(ctx *specs.Context) {
			called := false
			fn := func(_ context.Context) error {
				called = true
				return errors.New("err2")
			}
			chain := New(WithRunAll()).AddContextRunnerIf(false, fn)
			ctx.Expect(chain.Run()).To(specs.BeNil())
			ctx.Expect(called).To(specs.BeFalse())
		})

		s.It("ReturnAll - condition true, fn returns nil", func(ctx *specs.Context) {
			called := false
			fn := func(_ context.Context) error {
				called = true
				return nil
			}
			chain := New(WithRunAll()).AddContextRunnerIf(true, fn)
			ctx.Expect(chain.Run()).To(specs.BeNil())
			ctx.Expect(called).To(specs.BeTrue())
		})
	})
}

func TestAddContextRunner(t *testing.T) {
	specs.Describe(t, "AddContextRunner adds a context runner that always runs", func(s *specs.Spec) {
		s.It("FailFast - fn not called, error returned", func(ctx *specs.Context) {
			called := false
			fn := func(_ context.Context) error {
				called = true
				return errors.New("err1")
			}
			chain := New(WithFailFast()).AddContextRunner(fn)
			ctx.Expect(chain.Run()).To(specs.Not(specs.BeNil()))
			ctx.Expect(called).To(specs.BeTrue())
		})

		s.It("ReturnAll - fn not called", func(ctx *specs.Context) {
			called := false
			fn := func(_ context.Context) error {
				called = true
				return errors.New("err2")
			}
			chain := New(WithRunAll()).AddContextRunner(fn)
			ctx.Expect(chain.Run()).To(specs.Not(specs.BeNil()))
			ctx.Expect(called).To(specs.BeTrue())
		})

		s.It("ReturnAll - fn returns nil", func(ctx *specs.Context) {
			called := false
			fn := func(_ context.Context) error {
				called = true
				return nil
			}
			chain := New(WithRunAll()).AddContextRunner(fn)
			ctx.Expect(chain.Run()).To(specs.BeNil())
			ctx.Expect(called).To(specs.BeTrue())
		})
	})
}
