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

package eventsource

import (
	"context"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/persistence"
	behaviorport "github.com/getsyntegrity/ego/port/behavior"
)

// newRecoveringActor builds the Actor that recover needs and nothing else: the
// behavior, the unscoped persistence scope and the id the stores are keyed by.
// A unit case sets the stores, the encryptor and the event adapters it wants on
// the returned value. No actor system is involved.
func newRecoveringActor(persistenceID string, behavior behaviorport.EventSourced) *Actor {
	return &Actor{
		persistenceID: persistenceID,
		behavior:      behavior,
		scope:         persistence.Unscoped(),
	}
}

// expectRecoveryFailure runs recover on entity and requires it to fail with a
// message that starts with prefix and to leave the actor with no recovered
// state. When cause is not nil the error must also wrap it.
func expectRecoveryFailure(ctx *specs.Context, entity *Actor, prefix string, cause error) {
	err := entity.recover(context.Background())
	ctx.Expect(err).To(specs.Not(specs.BeNil()))
	ctx.Expect(err.Error()).To(specs.StartWith(prefix))
	if cause != nil {
		ctx.Expect(err).To(specs.MatchError(cause))
	}
	ctx.Expect(entity.currentState).To(specs.BeNil())
	ctx.Expect(entity.eventsCounter).ToEqual(uint64(0))
}
