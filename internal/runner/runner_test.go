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

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"
)

var (
	errOne = errors.New("err1")
	errTwo = errors.New("err2")
)

// steps adapts a mock.Controller into the two runner shapes Chain accepts. Each
// step is a method of the controller, so a case declares what every step
// returns, and how many times it must run, with Expect(...).Return(...).Times(n).
type steps struct{ c *mock.Controller }

func (s steps) fn(name string) func() error {
	return func() error { return s.c.Method(name).Call().Err(0) }
}

func (s steps) ctxFn(name string) func(context.Context) error {
	return func(ctx context.Context) error { return s.c.Method(name).Call(ctx).Err(0) }
}

func TestChain(t *testing.T) {
	specs.Describe(t, "Chain runs its runners according to the configured error policy", func(s *specs.Spec) {
		s.It("With AddRunner FailFast", func(ctx *specs.Context) {
			ctrl := mock.NewController(ctx)
			st := steps{ctrl}
			ctrl.Method("fn1").Expect().Return(errOne)
			ctrl.Method("fn2").Expect().Never()
			ctrl.Method("fn3").Expect().Never()

			actual := New(WithFailFast()).
				AddRunner(st.fn("fn1")).
				AddRunner(st.fn("fn2")).
				AddRunner(st.fn("fn3")).
				Run()

			ctx.Expect(actual).To(specs.MatchError(errOne))
			ctx.Expect(actual.Error()).ToEqual("err1")
		})

		s.It("With AddRunners FailFast", func(ctx *specs.Context) {
			ctrl := mock.NewController(ctx)
			st := steps{ctrl}
			ctrl.Method("fn1").Expect().Return(errOne)
			ctrl.Method("fn2").Expect().Never()
			ctrl.Method("fn3").Expect().Never()

			actual := New(WithFailFast()).AddRunners(st.fn("fn1"), st.fn("fn2"), st.fn("fn3")).Run()

			ctx.Expect(actual).To(specs.MatchError(errOne))
			ctx.Expect(actual.Error()).ToEqual("err1")
		})

		s.It("With AddRunner ReturnAll", func(ctx *specs.Context) {
			ctrl := mock.NewController(ctx)
			st := steps{ctrl}
			ctrl.Method("fn1").Expect().Return(errOne)
			ctrl.Method("fn2").Expect().Return(errTwo)
			ctrl.Method("fn3").Expect().Return(nil)

			actual := New(WithRunAll()).
				AddRunner(st.fn("fn1")).
				AddRunner(st.fn("fn2")).
				AddRunner(st.fn("fn3")).
				Run()

			ctx.Expect(actual).To(specs.MatchError(errOne))
			ctx.Expect(actual).To(specs.MatchError(errTwo))
			ctx.Expect(actual.Error()).ToEqual("err1; err2")
		})
	})
}

func TestAddContextRunnerIf(t *testing.T) {
	specs.Describe(t, "AddContextRunnerIf adds a context runner only when its condition holds", func(s *specs.Spec) {
		bg := context.Background()

		s.It("FailFast - condition true, error returned", func(ctx *specs.Context) {
			ctrl := mock.NewController(ctx)
			ctrl.Method("fn").Expect(bg).Return(errOne)

			chain := New(WithFailFast(), WithContext(bg)).AddContextRunnerIf(true, steps{ctrl}.ctxFn("fn"))

			ctx.Expect(chain.Run()).To(specs.MatchError(errOne))
		})

		s.It("FailFast - condition false, fn not called", func(ctx *specs.Context) {
			ctrl := mock.NewController(ctx)
			ctrl.Method("fn").Expect(mock.Any()).Never()

			chain := New(WithFailFast()).AddContextRunnerIf(false, steps{ctrl}.ctxFn("fn"))

			ctx.Expect(chain.Run()).To(specs.BeNil())
		})

		s.It("ReturnAll - condition true, error returned", func(ctx *specs.Context) {
			ctrl := mock.NewController(ctx)
			ctrl.Method("fn").Expect(mock.Any()).Return(errTwo)

			chain := New(WithRunAll()).AddContextRunnerIf(true, steps{ctrl}.ctxFn("fn"))

			ctx.Expect(chain.Run()).To(specs.MatchError(errTwo))
		})

		s.It("ReturnAll - condition false, fn not called", func(ctx *specs.Context) {
			ctrl := mock.NewController(ctx)
			ctrl.Method("fn").Expect(mock.Any()).Never()

			chain := New(WithRunAll()).AddContextRunnerIf(false, steps{ctrl}.ctxFn("fn"))

			ctx.Expect(chain.Run()).To(specs.BeNil())
		})

		s.It("ReturnAll - condition true, fn returns nil", func(ctx *specs.Context) {
			ctrl := mock.NewController(ctx)
			ctrl.Method("fn").Expect(mock.Any()).Return(nil)

			chain := New(WithRunAll()).AddContextRunnerIf(true, steps{ctrl}.ctxFn("fn"))

			ctx.Expect(chain.Run()).To(specs.BeNil())
		})
	})
}

func TestAddContextRunner(t *testing.T) {
	specs.Describe(t, "AddContextRunner adds a context runner that always runs", func(s *specs.Spec) {
		s.It("FailFast - fn not called, error returned", func(ctx *specs.Context) {
			ctrl := mock.NewController(ctx)
			ctrl.Method("fn").Expect(mock.Any()).Return(errOne)

			chain := New(WithFailFast()).AddContextRunner(steps{ctrl}.ctxFn("fn"))

			ctx.Expect(chain.Run()).To(specs.MatchError(errOne))
		})

		s.It("ReturnAll - fn not called", func(ctx *specs.Context) {
			ctrl := mock.NewController(ctx)
			ctrl.Method("fn").Expect(mock.Any()).Return(errTwo)

			chain := New(WithRunAll()).AddContextRunner(steps{ctrl}.ctxFn("fn"))

			ctx.Expect(chain.Run()).To(specs.MatchError(errTwo))
		})

		s.It("ReturnAll - fn returns nil", func(ctx *specs.Context) {
			ctrl := mock.NewController(ctx)
			ctrl.Method("fn").Expect(mock.Any()).Return(nil)

			chain := New(WithRunAll()).AddContextRunner(steps{ctrl}.ctxFn("fn"))

			ctx.Expect(chain.Run()).To(specs.BeNil())
		})
	})
}
