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

	"github.com/pablogore/ego/v4/compose"
	runtimeport "github.com/pablogore/ego/v4/port/runtime"
)

// TestRuntime_NilBeforeStart pins design ego-runtime-001 §D6: before Start
// the accessor returns an untyped nil, so a consumer's `rt == nil` check
// holds. Returning the atomic pointer directly would wrap a nil *ego.Engine
// in a non-nil interface and fail this test.
func TestRuntime_NilBeforeStart(t *testing.T) {
	app := mustNew(t, newFixture(t, "runtime-nil-before-start").spec)

	var rt runtimeport.Runtime = app.Runtime()
	if rt != nil {
		t.Fatalf("Runtime() before Start = %#v, want an untyped nil interface", rt)
	}
}

// TestRuntime_IsTheEngineAfterStartAndAfterStop: after Start the accessor
// hands out the same engine as Engine(); after Stop it keeps returning that
// stopped engine, which refuses work with ErrEngineNotStarted (§D6, §7 #24).
func TestRuntime_IsTheEngineAfterStartAndAfterStop(t *testing.T) {
	ctx := context.Background()
	app := mustNew(t, newFixture(t, "runtime-after-start").spec)
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	rt := app.Runtime()
	if rt == nil {
		t.Fatal("Runtime() after Start must not be nil")
	}
	engine := app.Engine()
	if engine == nil || !engine.Started() || rt != runtimeport.Runtime(engine) {
		t.Fatalf("Runtime() after Start = %#v, want the started engine Engine() returns (%p)", rt, engine)
	}

	if err := app.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	after := app.Runtime()
	if after == nil || after != runtimeport.Runtime(engine) {
		t.Fatalf("Runtime() after Stop = %#v, want the stopped engine", after)
	}
	if _, err := after.EntityExists(ctx, "any"); !errors.Is(err, runtimeport.ErrEngineNotStarted) {
		t.Fatalf("EntityExists after Stop = %v, want ErrEngineNotStarted", err)
	}
}

// TestRuntime_NilAfterFailedStart: a failed Start leaves the accessor nil
// for good, like Engine() (§D6).
func TestRuntime_NilAfterFailedStart(t *testing.T) {
	ctx := context.Background()
	app := mustNew(t, newFixture(t, "runtime-failed-start").spec)
	injected := errors.New("injected failure")
	app.hooks.afterStep = func(name string) error {
		if name == StepStartEngine {
			return injected
		}
		return nil
	}

	var se *compose.StartError
	if err := app.Start(ctx); !errors.As(err, &se) || !errors.Is(se.Err, injected) {
		t.Fatalf("Start = %v, want a StartError carrying the injected failure", err)
	}
	if rt := app.Runtime(); rt != nil {
		t.Fatalf("Runtime() after a failed Start = %#v, want nil", rt)
	}
	if err := app.Start(ctx); !errors.Is(err, ErrNotStartable) {
		t.Fatalf("second Start = %v, want ErrNotStartable", err)
	}
	if rt := app.Runtime(); rt != nil {
		t.Fatalf("Runtime() after a refused Start = %#v, want nil for good", rt)
	}
}
