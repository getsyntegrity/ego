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
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"

	testpb "github.com/getsyntegrity/ego/test/data/testpb"
	"github.com/getsyntegrity/ego/testkit"
)

// valueTypeEventSourcedBehavior implements the old EventSourcedBehavior
// contract, serialization methods included, with value receivers only.
// GoAkt's type registry names a type through reflect.Type.Elem, which
// panics for a non-pointer, so such a behavior must never reach
// ActorSystem.Inject (ego-arch-002-s3 design, §2.3).
type valueTypeEventSourcedBehavior struct {
	id string
}

var _ EventSourcedBehavior = valueTypeEventSourcedBehavior{}

func (v valueTypeEventSourcedBehavior) ID() string { return v.id }

func (v valueTypeEventSourcedBehavior) InitialState() State {
	return NewAccountEventSourcedBehavior(v.id).InitialState()
}

func (v valueTypeEventSourcedBehavior) HandleCommand(ctx context.Context, cmd Command, prior State) ([]Event, error) {
	return NewAccountEventSourcedBehavior(v.id).HandleCommand(ctx, cmd, prior)
}

func (v valueTypeEventSourcedBehavior) HandleEvent(ctx context.Context, evt Event, prior State) (State, error) {
	return NewAccountEventSourcedBehavior(v.id).HandleEvent(ctx, evt, prior)
}

func (v valueTypeEventSourcedBehavior) MarshalBinary() ([]byte, error) { return []byte(v.id), nil }

func (v valueTypeEventSourcedBehavior) UnmarshalBinary([]byte) error { return nil }

// TestEngineEntityValueTypeBehaviorSingleNode spawns a value-type behavior
// through the old Entity API on a single node and sends it a command.
//
// Before the spawn-site bridge (#123, S3-2) Entity handed the value to
// ActorSystem.Inject, which panicked inside GoAkt's type registry while
// holding the actor-system lock. The panic is deliberately not recovered:
// a recovered panic leaves that lock held and the cleanup hangs. Run the
// test with a short timeout (-timeout 90s) so either the panic or the
// timeout ends the binary.
func TestEngineEntityValueTypeBehaviorSingleNode(t *testing.T) {
	specs.Describe(t, "Engine Entity Value Type Behavior Single Node", func(s *specs.Spec) {
		s.It("holds", func(sc *specs.Context) {
			t := sc.T
			ctx := context.Background()
			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = store.Disconnect(ctx) })

			engine := newTestEngine(t, "ValueTypeBehavior", store, WithLogger(DiscardLogger))
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			entityID := uuid.NewString()
			sc.Expect(engine.Entity(ctx, valueTypeEventSourcedBehavior{id: entityID})).To(specs.BeNil())

			state, revision, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{AccountBalance: 42}, time.Minute)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(revision).To(specs.Equal(uint64(1)))
			acct, ok := state.(*testpb.Account)
			sc.Expect(ok).To(specs.BeTrue())
			sc.Expect(acct.GetAccountBalance()).To(specs.Equal(float64(42)))
		})
	})
}
