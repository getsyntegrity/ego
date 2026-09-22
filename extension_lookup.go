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

package ego

import (
	"fmt"

	goakt "github.com/tochemey/goakt/v4/actor"
)

// requireExtension looks up the extension registered under extensionID on the
// actor system reachable through ctx and asserts it to type T.
//
// ctx.Extension returns the plain extension.Extension interface, and every
// actor PreStart in this package used to assert it directly, e.g.
// ctx.Extension(id).(*extensions.EventsStore). That panics with an
// unrecoverable "interface conversion" error whenever the extension is
// missing or was registered under a different type. Because PreStart runs on
// the goroutine created for the Spawn/SpawnChild call — which goakt drives
// through a golang.org/x/sync/singleflight.Group so concurrent spawns of the
// same identity coalesce onto one execution — a panic there is deliberately
// re-panicked by singleflight on a fresh, unrecoverable goroutine (see
// golang.org/x/sync/singleflight.(*Group).doCall), which crashes the whole
// process instead of just failing the one Spawn call. This can happen for a
// child actor (eventsWriterActor, eventsJanitorActor, ...) spawned while its
// required extension was never registered, or a mismatched actor-system setup.
//
// requireExtension turns that panic into a descriptive PreStart error
// instead, which goakt reports as an ordinary spawn/init failure.
func requireExtension[T any](ctx *goakt.Context, extensionID string) (T, error) {
	var zero T

	ext := ctx.Extension(extensionID)
	if ext == nil {
		return zero, fmt.Errorf("%w: %s is not registered on the actor system (actor=%q)",
			ErrMissingRequiredExtensions, extensionID, ctx.ActorName())
	}

	typed, ok := ext.(T)
	if !ok {
		return zero, fmt.Errorf("%w: %s was registered with unexpected type %T (actor=%q)",
			ErrMissingRequiredExtensions, extensionID, ext, ctx.ActorName())
	}

	return typed, nil
}
