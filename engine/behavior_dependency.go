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
	"reflect"

	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/extension"

	"github.com/getsyntegrity/ego/v4/internal/extensions"
)

// spawnDependency returns the GoAkt spawn dependency that carries behavior b
// (ego-arch-002-s3 design, §5.3). It is the one place where the engine
// decides how a behavior reaches GoAkt:
//
//   - A nil behavior or a typed-nil pointer is rejected in every mode with a
//     *BehaviorPlacementError wrapping ErrBehaviorNotPointer and an empty
//     EntityID, since it has no ID the spawn could read.
//   - A non-nil pointer that implements extension.Dependency is returned
//     unchanged, after registering its type with sys.Inject, in and out of
//     cluster mode. The GoAkt type name and wire bytes stay exactly the
//     caller's, so nodes on either side of an upgrade keep decoding each
//     other's spawns and relocations.
//   - Anything else outside cluster mode is wrapped in an
//     extensions.LocalBehavior. The behavior's own type never reaches
//     sys.Inject, whose type registry panics on non-pointer types, and the
//     wrapper is never serialized: GoAkt serializes spawn dependencies only
//     in cluster mode.
//   - Anything else in cluster mode is rejected with a *BehaviorPlacementError
//     wrapping ErrBehaviorNotSerializable (no serialization methods) or
//     ErrBehaviorNotPointer (serialization methods on a non-pointer). The
//     caller returns it before any spawn, so nothing is started locally and
//     nothing is written to the cluster registry.
func spawnDependency(sys goakt.ActorSystem, b interface{ ID() string }) (extension.Dependency, error) {
	// A nil behavior, or a typed-nil pointer, has no usable ID: the spawn
	// would read it and panic. Reject it in every mode, before anything else.
	if isNilValue(b) {
		return nil, &BehaviorPlacementError{Kind: fmt.Sprintf("%T", b), Err: ErrBehaviorNotPointer}
	}

	dependency, serializable := b.(extension.Dependency)
	pointer := isNonNilPointer(b)

	if serializable && pointer {
		// Register the behavior type on the local node as a fallback for
		// kinds missing from WithEntityKinds. This only covers spawns placed
		// locally: in cluster mode the spawn may land on a peer, which can
		// only deserialize the behavior if it was registered there. Inject
		// only errors when the actor system is not started, which every
		// caller's Started check already rules out.
		_ = sys.Inject(dependency)
		return dependency, nil
	}

	if !sys.InCluster() {
		return extensions.NewLocalBehavior(b), nil
	}

	cause := ErrBehaviorNotSerializable
	if serializable {
		cause = ErrBehaviorNotPointer
	}

	return nil, &BehaviorPlacementError{Kind: fmt.Sprintf("%T", b), EntityID: b.ID(), Err: cause}
}

// behaviorFrom reads a behavior of contract T from a spawn dependency, as an
// actor's PreStart sees it in ctx.Dependencies(): either the behavior itself
// (the pass-through case of spawnDependency) or an extensions.LocalBehavior
// that wraps it.
func behaviorFrom[T any](dependency extension.Dependency) (T, bool) {
	if local, ok := dependency.(*extensions.LocalBehavior); ok {
		b, ok := local.Behavior().(T)
		return b, ok
	}
	b, ok := dependency.(T)
	return b, ok
}

// isNonNilPointer reports whether v is a non-nil pointer, the only kind of
// value GoAkt's type registry can name.
func isNonNilPointer(v any) bool {
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && !rv.IsNil()
}

// isNilValue reports whether v is nil or a typed nil pointer, whose ID
// method cannot be called safely.
func isNilValue(v any) bool {
	rv := reflect.ValueOf(v)
	return !rv.IsValid() || (rv.Kind() == reflect.Pointer && rv.IsNil())
}
