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

package goakt

import (
	"context"
	"errors"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/urd/compose"
	runtimeport "github.com/getsyntegrity/urd/port/runtime"
)

// TestRuntime_NilBeforeStart pins design ego-runtime-001 §D6: before Start
// the accessor returns an untyped nil, so a consumer's `rt == nil` check
// holds. Returning the atomic pointer directly would wrap a nil *engine.Engine
// in a non-nil interface and fail this test.
func TestRuntime_NilBeforeStart(t *testing.T) {
	specs.Describe(t, "Runtime returns an untyped nil interface before Start", func(s *specs.Spec) {
		s.It("compares equal to nil, not a non-nil interface wrapping a nil engine", func(ctx *specs.Context) {
			app := mustNew(ctx, newFixture(ctx, "runtime-nil-before-start").spec)

			rt := app.Runtime()
			// Compared as an interface on purpose: a typed-nil *engine.Engine inside
			// the interface must fail here.
			ctx.Expect(rt == nil).To(specs.BeTrue())
		})
	})
}

// TestRuntime_IsTheEngineAfterStartAndAfterStop: after Start the accessor
// hands out the same engine as Engine(); after Stop it keeps returning that
// stopped engine, which refuses work with ErrEngineNotStarted (§D6, §7 #24).
func TestRuntime_IsTheEngineAfterStartAndAfterStop(t *testing.T) {
	specs.Describe(t, "Runtime after Start and Stop", func(s *specs.Spec) {
		s.It("is the started engine after Start and the stopped engine after Stop", func(ctx *specs.Context) {
			bg := context.Background()
			app := mustNew(ctx, newFixture(ctx, "runtime-after-start").spec)
			ctx.Expect(app.Start(bg)).To(specs.BeNil())

			rt := app.Runtime()
			ctx.Expect(rt == nil).To(specs.BeFalse())
			engine := app.Engine()
			ctx.Expect(engine != nil && engine.Started() && rt == runtimeport.Runtime(engine)).To(specs.BeTrue())

			ctx.Expect(app.Stop(bg)).To(specs.BeNil())
			after := app.Runtime()
			ctx.Expect(after != nil && after == runtimeport.Runtime(engine)).To(specs.BeTrue())
			_, err := after.EntityExists(bg, "any")
			ctx.Expect(err).To(specs.MatchError(runtimeport.ErrEngineNotStarted))
		})
	})
}

// TestRuntime_NilAfterFailedStart: a failed Start leaves the accessor nil
// for good, like Engine() (§D6).
func TestRuntime_NilAfterFailedStart(t *testing.T) {
	specs.Describe(t, "Runtime after a failed Start", func(s *specs.Spec) {
		s.It("stays nil for good, like Engine()", func(ctx *specs.Context) {
			bg := context.Background()
			app := mustNew(ctx, newFixture(ctx, "runtime-failed-start").spec)
			injected := errors.New("injected failure")
			app.hooks.afterStep = func(name string) error {
				if name == StepStartEngine {
					return injected
				}
				return nil
			}

			var se *compose.StartError
			ctx.Expect(app.Start(bg)).To(specs.MatchErrorAs(&se))
			ctx.Expect(se.Err).To(specs.MatchError(injected))
			ctx.Expect(app.Runtime() == nil).To(specs.BeTrue())
			ctx.Expect(app.Start(bg)).To(specs.MatchError(ErrNotStartable))
			ctx.Expect(app.Runtime() == nil).To(specs.BeTrue())
		})
	})
}
