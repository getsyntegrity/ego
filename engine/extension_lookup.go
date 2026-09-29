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
	"fmt"

	goakt "github.com/tochemey/goakt/v4/actor"

	"github.com/getsyntegrity/ego/v4/internal/extensions"
)

// requireExtension looks up the extension registered under extensionID on the
// actor system reachable through ctx and asserts it to type T. It returns an
// error wrapping ErrMissingRequiredExtensions instead of panicking when the
// extension is missing or has an unexpected type; see extensions.Require,
// which the actors outside this package share, for why that matters.
func requireExtension[T any](ctx *goakt.Context, extensionID string) (T, error) {
	return extensions.Require[T](ctx, extensionID)
}

// optionalExtension looks up the extension registered under extensionID on
// the actor system reachable through ctx and asserts it to type T, but,
// unlike requireExtension, treats a missing registration as valid: it
// returns the zero value and a nil error when no extension is registered
// under extensionID at all. This fits PreStart paths for which the
// extension is a genuine optional dependency, e.g. snapshotsWriterActor's
// snapshot store and encryptor extensions (see snapshots_writer_actor.go).
//
// It still guards against the same crash-the-process failure mode described
// on requireExtension: if an extension IS registered under extensionID but
// under an unexpected concrete type — for example because of a wiring bug
// that registers the wrong extension under an existing ID — an unchecked
// ext.(T) assertion would panic, and because PreStart runs on a goroutine
// driven through golang.org/x/sync/singleflight.Group, that panic would be
// re-panicked by singleflight on a fresh, unrecoverable goroutine and crash
// the whole process (see requireExtension). optionalExtension returns a
// descriptive error instead in that case.
func optionalExtension[T any](ctx *goakt.Context, extensionID string) (T, error) {
	var zero T

	ext := ctx.Extension(extensionID)
	if ext == nil {
		return zero, nil
	}

	typed, ok := ext.(T)
	if !ok {
		return zero, fmt.Errorf("%w: %s was registered with unexpected type %T (actor=%q)",
			ErrMissingRequiredExtensions, extensionID, ext, ctx.ActorName())
	}

	return typed, nil
}
