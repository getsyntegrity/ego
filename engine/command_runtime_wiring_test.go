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
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"

	"github.com/getsyntegrity/ego/command"
	"github.com/getsyntegrity/ego/persistence"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
	"github.com/getsyntegrity/ego/testkit"
)

// TestEngineSendCommandDispatchesHandleEnvelope proves the M-3 wiring
// end-to-end: a behavior implementing the additive, optional
// EventSourcedEnvelopeBehavior interface (#60) receives HandleEnvelope, not
// HandleCommand, when reached through Engine.SendCommand — with a
// well-formed command.Envelope whose Metadata carries a generated
// OperationID and a Timestamp, exactly as command_context.go's Carrier
// rematerialization is meant to produce.
func TestEngineSendCommandDispatchesHandleEnvelope(t *testing.T) {
	specs.Describe(t, "Engine Send Command Dispatches Handle Envelope", func(s *specs.Spec) {
		s.It("holds", func(sc *specs.Context) {
			t := sc.T
			ctx := context.Background()
			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = store.Disconnect(ctx) })

			engine := newTestEngine(t, "EnvelopeWiring", store, WithLogger(DiscardLogger))
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			entityID := uuid.NewString()
			behavior := newEnvelopeCapturingEventSourcedBehavior(entityID)
			sc.Expect(engine.Entity(ctx, behavior)).To(specs.BeNil())

			state, revision, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{
				AccountBalance: 100,
			}, time.Minute)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(revision).To(specs.Equal(uint64(1)))
			acct, ok := state.(*testpb.Account)
			sc.Expect(ok).To(specs.BeTrue())
			sc.Expect(acct.GetAccountBalance()).To(specs.Equal(float64(100)))

			handleCommandHit, handleEnvelopeHit, env := behavior.Snapshot()
			sc.Expect(handleCommandHit).To(specs.BeZero())
			sc.Expect(handleEnvelopeHit).To(specs.Equal(1))

			sc.Expect(env.Metadata().OperationID()).To(specs.Not(specs.BeEmpty()))
			sc.Expect(env.Metadata().Timestamp().IsZero()).To(specs.BeFalse())
			_, ok = command.PayloadAs[*testpb.CreateAccount](env)
			sc.Expect(ok).To(specs.BeTrue())

			sc.Expect(engine.Stop(ctx)).To(specs.BeNil())
		})
	})
}

// TestEngineDispatchDispatchesHandleEnvelope is the same proof against the
// new Dispatch entry point directly (rather than through the SendCommand
// adapter), confirming both entry points feed the same rematerialization
// path (command_context.go's attachCarrier/metadataFromContext).
func TestEngineDispatchDispatchesHandleEnvelope(t *testing.T) {
	specs.Describe(t, "Engine Dispatch Dispatches Handle Envelope", func(s *specs.Spec) {
		s.It("holds", func(sc *specs.Context) {
			t := sc.T
			ctx := context.Background()
			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = store.Disconnect(ctx) })

			engine := newTestEngine(t, "EnvelopeWiringDispatch", store, WithLogger(DiscardLogger))
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			entityID := uuid.NewString()
			behavior := newEnvelopeCapturingEventSourcedBehavior(entityID)
			sc.Expect(engine.Entity(ctx, behavior)).To(specs.BeNil())

			op, err := command.NewOperationID("test-op-" + entityID)
			sc.Expect(err).To(specs.BeNil())
			md, err := command.NewMetadata(op)
			sc.Expect(err).To(specs.BeNil())
			env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 42}, md)
			sc.Expect(err).To(specs.BeNil())

			result, err := engine.Dispatch(ctx, entityID, env, time.Minute)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(result.Outcome()).To(specs.Equal(command.OutcomeSuccess))

			handleCommandHit, handleEnvelopeHit, gotEnv := behavior.Snapshot()
			sc.Expect(handleCommandHit).To(specs.BeZero())
			sc.Expect(handleEnvelopeHit).To(specs.Equal(1))
			sc.Expect(gotEnv.Metadata().OperationID()).To(specs.Equal(op))

			sc.Expect(engine.Stop(ctx)).To(specs.BeNil())
		})
	})
}

// TestEngineDispatchRejectsInvalidMetadataWithoutInvokingHandler proves
// Dispatch validates env's Metadata against the same round-trip the
// receiving actor depends on (command.MarshalMetadata/UnmarshalMetadata)
// before ever calling SendSync. Without this check, a zero-value
// command.Metadata{} (legal to embed in an Envelope since NewEnvelope does
// not validate md) would reach the actor, fail UnmarshalMetadata there, and
// silently fall back to HandleCommand — running the payload as a legacy
// command instead of surfacing the caller's error.
func TestEngineDispatchRejectsInvalidMetadataWithoutInvokingHandler(t *testing.T) {
	specs.Describe(t, "Engine Dispatch Rejects Invalid Metadata Without Invoking Handler", func(s *specs.Spec) {
		s.It("holds", func(sc *specs.Context) {
			t := sc.T
			ctx := context.Background()
			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = store.Disconnect(ctx) })

			engine := newTestEngine(t, "EnvelopeWiringInvalidMetadata", store, WithLogger(DiscardLogger))
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			entityID := uuid.NewString()
			behavior := newEnvelopeCapturingEventSourcedBehavior(entityID)
			sc.Expect(engine.Entity(ctx, behavior)).To(specs.BeNil())

			env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 100}, command.Metadata{})
			sc.Expect(err).To(specs.BeNil())

			result, err := engine.Dispatch(ctx, entityID, env, time.Minute)
			sc.Expect(err).To(specs.Not(specs.BeNil()))
			sc.Expect(err).To(specs.MatchError(command.ErrInvalidMetadata))
			sc.Expect(result).To(specs.Equal(command.Result{}))

			handleCommandHit, handleEnvelopeHit, _ := behavior.Snapshot()
			sc.Expect(handleCommandHit).To(specs.BeZero())
			sc.Expect(handleEnvelopeHit).To(specs.BeZero())

			event, err := store.GetLatestEvent(ctx, persistence.Unscoped(), entityID)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(event).To(specs.BeNil())

			sc.Expect(engine.Stop(ctx)).To(specs.BeNil())
		})
	})
}

// TestEngineDispatchRejectsZeroValueEnvelopeWithoutPanicking proves Dispatch
// rejects a caller-constructed zero-value command.Envelope{} outright rather
// than panicking. Envelope's fields are unexported but Go still allows an
// empty struct literal from any package, so NewEnvelope's nil-payload guard
// can be bypassed entirely; a nil Payload() previously reached
// env.Payload().ProtoReflect() in Dispatch's telemetry span setup before any
// validation ran, panicking on the nil proto.Message interface whenever
// telemetry was configured.
func TestEngineDispatchRejectsZeroValueEnvelopeWithoutPanicking(t *testing.T) {
	specs.Describe(t, "Engine Dispatch Rejects Zero Value Envelope Without Panicking", func(s *specs.Spec) {
		s.It("holds", func(sc *specs.Context) {
			t := sc.T
			ctx := context.Background()
			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = store.Disconnect(ctx) })

			engine := newTestEngine(t, "EnvelopeWiringZeroValueEnvelope", store, WithLogger(DiscardLogger),
				WithTelemetry(&Telemetry{Tracer: otel.Tracer("test")}))
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			entityID := uuid.NewString()
			behavior := newEnvelopeCapturingEventSourcedBehavior(entityID)
			sc.Expect(engine.Entity(ctx, behavior)).To(specs.BeNil())

			var zeroEnv command.Envelope

			sc.Expect(panicValueG1(func() {
				result, err := engine.Dispatch(ctx, entityID, zeroEnv, time.Minute)
				sc.Expect(err).To(specs.Not(specs.BeNil()))
				sc.Expect(err).To(specs.MatchError(command.ErrInvalidEnvelope))
				sc.Expect(result).To(specs.Equal(command.Result{}))
			})).To(specs.BeNil())

			handleCommandHit, handleEnvelopeHit, _ := behavior.Snapshot()
			sc.Expect(handleCommandHit).To(specs.BeZero())
			sc.Expect(handleEnvelopeHit).To(specs.BeZero())

			event, err := store.GetLatestEvent(ctx, persistence.Unscoped(), entityID)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(event).To(specs.BeNil())

			sc.Expect(engine.Stop(ctx)).To(specs.BeNil())
		})
	})
}

// TestEngineDispatchRejectsExpiredDeadlineWithoutInvokingHandler proves
// Dispatch rejects an Envelope whose Metadata deadline has already passed
// outright — no SendSync call, no actor, no handler, no persistence —
// rather than letting the command run and possibly persist after its
// declared deadline.
func TestEngineDispatchRejectsExpiredDeadlineWithoutInvokingHandler(t *testing.T) {
	specs.Describe(t, "Engine Dispatch Rejects Expired Deadline Without Invoking Handler", func(s *specs.Spec) {
		s.It("holds", func(sc *specs.Context) {
			t := sc.T
			ctx := context.Background()
			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = store.Disconnect(ctx) })

			engine := newTestEngine(t, "EnvelopeWiringExpiredDeadline", store, WithLogger(DiscardLogger))
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			entityID := uuid.NewString()
			behavior := newEnvelopeCapturingEventSourcedBehavior(entityID)
			sc.Expect(engine.Entity(ctx, behavior)).To(specs.BeNil())

			op, err := command.NewOperationID("expired-deadline-" + entityID)
			sc.Expect(err).To(specs.BeNil())
			md, err := command.NewMetadata(op, command.WithDeadline(time.Now().Add(-time.Minute)))
			sc.Expect(err).To(specs.BeNil())
			env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 100}, md)
			sc.Expect(err).To(specs.BeNil())

			result, err := engine.Dispatch(ctx, entityID, env, time.Minute)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(result.Outcome()).To(specs.Equal(command.OutcomeTimedOut))
			sc.Expect(result.Err()).To(specs.MatchError(command.ErrTimedOut))

			handleCommandHit, handleEnvelopeHit, _ := behavior.Snapshot()
			sc.Expect(handleCommandHit).To(specs.BeZero())
			sc.Expect(handleEnvelopeHit).To(specs.BeZero())

			event, err := store.GetLatestEvent(ctx, persistence.Unscoped(), entityID)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(event).To(specs.BeNil())

			sc.Expect(engine.Stop(ctx)).To(specs.BeNil())
		})
	})
}

// TestEngineDispatchClampsTimeoutToDeadline proves Dispatch bounds
// SendSync's wait to the Metadata deadline, not the (looser)
// caller-supplied timeout, when the deadline is tighter: a handler slow
// enough to outlive the deadline but well within timeout must not be
// allowed to complete and persist past the deadline.
func TestEngineDispatchClampsTimeoutToDeadline(t *testing.T) {
	specs.Describe(t, "Engine Dispatch Clamps Timeout To Deadline", func(s *specs.Spec) {
		s.It("holds", func(sc *specs.Context) {
			t := sc.T
			ctx := context.Background()
			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = store.Disconnect(ctx) })

			engine := newTestEngine(t, "EnvelopeWiringDeadlineClamp", store, WithLogger(DiscardLogger))
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			entityID := uuid.NewString()
			behavior := newEnvelopeCapturingEventSourcedBehavior(entityID)
			behavior.SetDelay(500 * time.Millisecond)
			sc.Expect(engine.Entity(ctx, behavior)).To(specs.BeNil())

			op, err := command.NewOperationID("deadline-clamp-" + entityID)
			sc.Expect(err).To(specs.BeNil())
			md, err := command.NewMetadata(op, command.WithDeadline(time.Now().Add(100*time.Millisecond)))
			sc.Expect(err).To(specs.BeNil())
			env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 100}, md)
			sc.Expect(err).To(specs.BeNil())

			start := time.Now()
			result, err := engine.Dispatch(ctx, entityID, env, 10*time.Second)
			elapsed := time.Since(start)

			sc.Expect(err).To(specs.BeNil())
			sc.Expect(result.Outcome()).To(specs.Equal(command.OutcomeTimedOut))
			sc.Expect(result.Err()).To(specs.MatchError(command.ErrTimedOut))
			sc.Expect(elapsed).To(specs.BeLessThan(400 * time.Millisecond))

			event, err := store.GetLatestEvent(ctx, persistence.Unscoped(), entityID)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(event).To(specs.BeNil())

			sc.Expect(engine.Stop(ctx)).To(specs.BeNil())
		})
	})
}

// TestEngineSendCommandDispatchesDurableStateHandleEnvelope is the
// DurableStateActor counterpart of
// TestEngineSendCommandDispatchesHandleEnvelope.
func TestEngineSendCommandDispatchesDurableStateHandleEnvelope(t *testing.T) {
	specs.Describe(t, "Engine Send Command Dispatches Durable State Handle Envelope", func(s *specs.Spec) {
		s.It("holds", func(sc *specs.Context) {
			t := sc.T
			ctx := context.Background()
			stateStore := testkit.NewDurableStore()
			sc.Expect(stateStore.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = stateStore.Disconnect(ctx) })

			engine := newTestEngine(t, "EnvelopeWiringDurableState", nil,
				WithLogger(DiscardLogger),
				WithStateStore(stateStore),
			)
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			entityID := uuid.NewString()
			behavior := newEnvelopeCapturingDurableStateBehavior(entityID)
			sc.Expect(engine.DurableStateEntity(ctx, behavior)).To(specs.BeNil())

			state, revision, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{
				AccountBalance: 55,
			}, time.Minute)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(revision).To(specs.Equal(uint64(1)))
			acct, ok := state.(*testpb.Account)
			sc.Expect(ok).To(specs.BeTrue())
			sc.Expect(acct.GetAccountBalance()).To(specs.Equal(float64(55)))

			handleCommandHit, handleEnvelopeHit, env := behavior.Snapshot()
			sc.Expect(handleCommandHit).To(specs.BeZero())
			sc.Expect(handleEnvelopeHit).To(specs.Equal(1))
			sc.Expect(env.Metadata().OperationID()).To(specs.Not(specs.BeEmpty()))

			sc.Expect(engine.Stop(ctx)).To(specs.BeNil())
		})
	})
}

// dispatchOutcome carries an engine.Dispatch call's result back across a
// goroutine boundary, for tests that must observe Dispatch returning
// (because the caller-side wait gave up at the effective deadline) while
// the actor-side handler is still deliberately blocked.
type dispatchOutcome struct {
	result command.Result
	err    error
}

// blockingEventSourcedBehavior lets a test observe exactly when a command
// handler begins executing (via started) and control exactly when it
// returns (via release), instead of inferring timing from time.Sleep and
// an elapsed-duration assertion. This is what the mandatory deadline tests
// (#60 Round 2 review) use to prove a handler that ignores its context is
// stopped by the actor's own fail-closed gates, not merely by the caller
// giving up.
type blockingEventSourcedBehavior struct {
	id string

	started chan struct{}
	release chan struct{}

	mu  sync.Mutex
	hit int
}

var _ EventSourcedBehavior = (*blockingEventSourcedBehavior)(nil)

func newBlockingEventSourcedBehavior(id string) *blockingEventSourcedBehavior {
	return &blockingEventSourcedBehavior{
		id:      id,
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
}

func (x *blockingEventSourcedBehavior) ID() string { return x.id }

func (x *blockingEventSourcedBehavior) InitialState() State { return new(testpb.Account) }

func (x *blockingEventSourcedBehavior) HandleCommand(_ context.Context, cmd Command, _ State) (events []Event, err error) {
	x.mu.Lock()
	x.hit++
	x.mu.Unlock()

	select {
	case x.started <- struct{}{}:
	default:
	}
	<-x.release

	switch c := cmd.(type) {
	case *testpb.CreateAccount:
		return []Event{
			&testpb.AccountCreated{
				AccountId:      x.id,
				AccountBalance: c.GetAccountBalance(),
			},
		}, nil
	default:
		return nil, errors.New("unhandled command")
	}
}

func (x *blockingEventSourcedBehavior) HandleEvent(_ context.Context, event Event, _ State) (state State, err error) {
	switch evt := event.(type) {
	case *testpb.AccountCreated:
		return &testpb.Account{
			AccountId:      evt.GetAccountId(),
			AccountBalance: evt.GetAccountBalance(),
		}, nil
	default:
		return nil, errors.New("unhandled event")
	}
}

func (x *blockingEventSourcedBehavior) MarshalBinary() (data []byte, err error) {
	return json.Marshal(struct {
		ID string `json:"id"`
	}{ID: x.id})
}

func (x *blockingEventSourcedBehavior) UnmarshalBinary(data []byte) error {
	aux := struct {
		ID string `json:"id"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	x.id = aux.ID
	return nil
}

// blockingDurableStateBehavior is the DurableStateBehavior counterpart of
// blockingEventSourcedBehavior.
type blockingDurableStateBehavior struct {
	id string

	started chan struct{}
	release chan struct{}

	mu  sync.Mutex
	hit int
}

var _ DurableStateBehavior = (*blockingDurableStateBehavior)(nil)

func newBlockingDurableStateBehavior(id string) *blockingDurableStateBehavior {
	return &blockingDurableStateBehavior{
		id:      id,
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
}

func (x *blockingDurableStateBehavior) ID() string { return x.id }

func (x *blockingDurableStateBehavior) InitialState() State { return new(testpb.Account) }

// nolint
func (x *blockingDurableStateBehavior) HandleCommand(_ context.Context, cmd Command, priorVersion uint64, _ State) (newState State, newVersion uint64, err error) {
	x.mu.Lock()
	x.hit++
	x.mu.Unlock()

	select {
	case x.started <- struct{}{}:
	default:
	}
	<-x.release

	switch c := cmd.(type) {
	case *testpb.CreateAccount:
		return &testpb.Account{
			AccountId:      x.id,
			AccountBalance: c.GetAccountBalance(),
		}, priorVersion + 1, nil
	default:
		return nil, 0, errors.New("unhandled command")
	}
}

func (x *blockingDurableStateBehavior) MarshalBinary() (data []byte, err error) {
	return json.Marshal(struct {
		ID string `json:"id"`
	}{ID: x.id})
}

func (x *blockingDurableStateBehavior) UnmarshalBinary(data []byte) error {
	aux := struct {
		ID string `json:"id"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	x.id = aux.ID
	return nil
}

// TestEventSourcedActorDirectPathDiscardsHandlerOutputAfterDeadlineExpiry
// proves the two-gate fail-closed design (deadline_gate.go) actually stops
// a handler that ignores its context from mutating state or persisting,
// for EventSourcedActor's direct (non-batched) path.
//
// The handler is blocked on a channel the test controls, never
// time.Sleep. The sequence is: the handler starts and blocks; Dispatch's
// own caller-side wait gives up once the effective deadline passes and
// reports OutcomeTimedOut while the handler is STILL blocked (proving the
// caller did not wait for the handler); only then does the test release
// the handler, letting the actor's own post-handler/pre-persist gate run.
// A follow-up command on the same entity starting at sequence 1 proves the
// discarded handler's output never reached WriteEvents or mutated
// entity.currentState/entity.eventsCounter.
func TestEventSourcedActorDirectPathDiscardsHandlerOutputAfterDeadlineExpiry(t *testing.T) {
	specs.Describe(t, "Event Sourced Actor Direct Path Discards Handler Output After Deadline Expiry", func(s *specs.Spec) {
		s.It("holds", func(sc *specs.Context) {
			t := sc.T
			ctx := context.Background()
			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = store.Disconnect(ctx) })

			engine := newTestEngine(t, "DeadlineDirectPath", store, WithLogger(DiscardLogger))
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			entityID := uuid.NewString()
			behavior := newBlockingEventSourcedBehavior(entityID)
			sc.Expect(engine.Entity(ctx, behavior)).To(specs.BeNil())

			op, err := command.NewOperationID("deadline-direct-" + entityID)
			sc.Expect(err).To(specs.BeNil())
			md, err := command.NewMetadata(op, command.WithDeadline(time.Now().Add(150*time.Millisecond)))
			sc.Expect(err).To(specs.BeNil())
			env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 100}, md)
			sc.Expect(err).To(specs.BeNil())

			outcomeCh := make(chan dispatchOutcome, 1)
			go func() {
				result, dispatchErr := engine.Dispatch(context.Background(), entityID, env, 5*time.Second)
				outcomeCh <- dispatchOutcome{result: result, err: dispatchErr}
			}()

			select {
			case <-behavior.started:
			case <-time.After(2 * time.Second):
				t.Fatal("handler never started")
			}

			var outcome dispatchOutcome
			select {
			case outcome = <-outcomeCh:
			case <-time.After(3 * time.Second):
				t.Fatal("Dispatch never returned once the deadline passed; the caller-side wait must not wait for the still-blocked handler")
			}
			sc.Expect(outcome.err).To(specs.BeNil())
			sc.Expect(outcome.result.Outcome()).To(specs.Equal(command.OutcomeTimedOut))
			sc.Expect(outcome.result.Err()).To(specs.MatchError(command.ErrTimedOut))

			// The handler is still blocked and nothing has been written either way.
			// Release it now and prove the actor's own post-handler gate — not the
			// caller giving up — is what keeps its output out of the store.
			close(behavior.release)

			state, revision, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{AccountBalance: 50}, time.Minute)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(revision).To(specs.Equal(uint64(1)))
			acct, ok := state.(*testpb.Account)
			sc.Expect(ok).To(specs.BeTrue())
			sc.Expect(acct.GetAccountBalance()).To(specs.Equal(float64(50)))

			event, err := store.GetLatestEvent(ctx, persistence.Unscoped(), entityID)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(event).To(specs.Not(specs.BeNil()))
			sc.Expect(event.GetSequenceNumber()).To(specs.Equal(uint64(1)))

			sc.Expect(engine.Stop(ctx)).To(specs.BeNil())
		})
	})
}

// TestDurableStateActorDiscardsHandlerOutputAfterDeadlineExpiry is the
// DurableStateActor counterpart of
// TestEventSourcedActorDirectPathDiscardsHandlerOutputAfterDeadlineExpiry.
func TestDurableStateActorDiscardsHandlerOutputAfterDeadlineExpiry(t *testing.T) {
	specs.Describe(t, "Durable State Actor Discards Handler Output After Deadline Expiry", func(s *specs.Spec) {
		s.It("holds", func(sc *specs.Context) {
			t := sc.T
			ctx := context.Background()
			stateStore := testkit.NewDurableStore()
			sc.Expect(stateStore.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = stateStore.Disconnect(ctx) })

			engine := newTestEngine(t, "DeadlineDurableState", nil,
				WithLogger(DiscardLogger),
				WithStateStore(stateStore),
			)
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			entityID := uuid.NewString()
			behavior := newBlockingDurableStateBehavior(entityID)
			sc.Expect(engine.DurableStateEntity(ctx, behavior)).To(specs.BeNil())

			op, err := command.NewOperationID("deadline-durable-" + entityID)
			sc.Expect(err).To(specs.BeNil())
			md, err := command.NewMetadata(op, command.WithDeadline(time.Now().Add(150*time.Millisecond)))
			sc.Expect(err).To(specs.BeNil())
			env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 100}, md)
			sc.Expect(err).To(specs.BeNil())

			outcomeCh := make(chan dispatchOutcome, 1)
			go func() {
				result, dispatchErr := engine.Dispatch(context.Background(), entityID, env, 5*time.Second)
				outcomeCh <- dispatchOutcome{result: result, err: dispatchErr}
			}()

			select {
			case <-behavior.started:
			case <-time.After(2 * time.Second):
				t.Fatal("handler never started")
			}

			var outcome dispatchOutcome
			select {
			case outcome = <-outcomeCh:
			case <-time.After(3 * time.Second):
				t.Fatal("Dispatch never returned once the deadline passed; the caller-side wait must not wait for the still-blocked handler")
			}
			sc.Expect(outcome.err).To(specs.BeNil())
			sc.Expect(outcome.result.Outcome()).To(specs.Equal(command.OutcomeTimedOut))
			sc.Expect(outcome.result.Err()).To(specs.MatchError(command.ErrTimedOut))

			close(behavior.release)

			state, revision, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{AccountBalance: 50}, time.Minute)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(revision).To(specs.Equal(uint64(1)))
			acct, ok := state.(*testpb.Account)
			sc.Expect(ok).To(specs.BeTrue())
			sc.Expect(acct.GetAccountBalance()).To(specs.Equal(float64(50)))

			stored, err := stateStore.GetLatestState(ctx, persistence.Unscoped(), entityID)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(stored).To(specs.Not(specs.BeNil()))
			sc.Expect(stored.GetVersionNumber()).To(specs.Equal(uint64(1)))

			sc.Expect(engine.Stop(ctx)).To(specs.BeNil())
		})
	})
}

// TestEventSourcedActorBatchPathDoesNotContaminateBatchStateAfterDeadlineExpiry
// is the batch-path (processAndBatch) counterpart of
// TestEventSourcedActorDirectPathDiscardsHandlerOutputAfterDeadlineExpiry:
// an expired command's output must never reach entity.batchBuffer /
// entity.batchState / entity.batchEntries, so it can neither be flushed to
// the store itself nor corrupt a later, valid command sharing the same
// batch.
func TestEventSourcedActorBatchPathDoesNotContaminateBatchStateAfterDeadlineExpiry(t *testing.T) {
	specs.Describe(t, "Event Sourced Actor Batch Path Does Not Contaminate Batch State After Deadline Expiry", func(s *specs.Spec) {
		s.It("holds", func(sc *specs.Context) {
			t := sc.T
			ctx := context.Background()
			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = store.Disconnect(ctx) })

			engine := newTestEngine(t, "DeadlineBatchPath", store, WithLogger(DiscardLogger))
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			entityID := uuid.NewString()
			behavior := newBlockingEventSourcedBehavior(entityID)
			sc.Expect(engine.Entity(ctx, behavior, WithBatchThreshold(1))).To(specs.BeNil())

			op, err := command.NewOperationID("deadline-batch-" + entityID)
			sc.Expect(err).To(specs.BeNil())
			md, err := command.NewMetadata(op, command.WithDeadline(time.Now().Add(150*time.Millisecond)))
			sc.Expect(err).To(specs.BeNil())
			env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 100}, md)
			sc.Expect(err).To(specs.BeNil())

			outcomeCh := make(chan dispatchOutcome, 1)
			go func() {
				result, dispatchErr := engine.Dispatch(context.Background(), entityID, env, 5*time.Second)
				outcomeCh <- dispatchOutcome{result: result, err: dispatchErr}
			}()

			select {
			case <-behavior.started:
			case <-time.After(2 * time.Second):
				t.Fatal("handler never started")
			}

			var outcome dispatchOutcome
			select {
			case outcome = <-outcomeCh:
			case <-time.After(3 * time.Second):
				t.Fatal("Dispatch never returned once the deadline passed; the caller-side wait must not wait for the still-blocked handler")
			}
			sc.Expect(outcome.err).To(specs.BeNil())
			sc.Expect(outcome.result.Outcome()).To(specs.Equal(command.OutcomeTimedOut))

			close(behavior.release)

			state, revision, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{AccountBalance: 50}, time.Minute)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(revision).To(specs.Equal(uint64(1)))
			acct, ok := state.(*testpb.Account)
			sc.Expect(ok).To(specs.BeTrue())
			sc.Expect(acct.GetAccountBalance()).To(specs.Equal(float64(50)))

			event, err := store.GetLatestEvent(ctx, persistence.Unscoped(), entityID)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(event).To(specs.Not(specs.BeNil()))
			sc.Expect(event.GetSequenceNumber()).To(specs.Equal(uint64(1)))

			sc.Expect(engine.Stop(ctx)).To(specs.BeNil())
		})
	})
}

// TestEngineDispatchEffectiveDeadlinePrecedence proves Dispatch computes the
// effective deadline as min(ctx's own deadline, the envelope Metadata's
// deadline, the caller-supplied timeout) rather than any single source
// alone: whichever source is tightest is honored even when the other two
// are generous, and an explicit caller cancellation is reported distinctly
// as OutcomeCanceled rather than OutcomeTimedOut.
func TestEngineDispatchEffectiveDeadlinePrecedence(t *testing.T) {
	specs.Describe(t, "Engine Dispatch Effective Deadline Precedence", func(s *specs.Spec) {
		newHarness := func(sc *specs.Context, name string) (*Engine, *testkit.EventStore) {
			t := sc.T
			t.Helper()
			ctx := context.Background()
			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = store.Disconnect(ctx) })

			engine := newTestEngine(t, name, store, WithLogger(DiscardLogger))
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = engine.Stop(context.Background()) })
			return engine, store
		}

		s.It("metadata deadline wins over a generous timeout", func(sc *specs.Context) {
			engine, _ := newHarness(sc, "PrecedenceMetadataBeforeTimeout")
			entityID := uuid.NewString()
			behavior := newEnvelopeCapturingEventSourcedBehavior(entityID)
			sc.Expect(engine.Entity(context.Background(), behavior)).To(specs.BeNil())

			op, err := command.NewOperationID("precedence-md-" + entityID)
			sc.Expect(err).To(specs.BeNil())
			md, err := command.NewMetadata(op, command.WithDeadline(time.Now().Add(-time.Minute)))
			sc.Expect(err).To(specs.BeNil())
			env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 1}, md)
			sc.Expect(err).To(specs.BeNil())

			result, err := engine.Dispatch(context.Background(), entityID, env, time.Hour)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(result.Outcome()).To(specs.Equal(command.OutcomeTimedOut))

			handleCommandHit, handleEnvelopeHit, _ := behavior.Snapshot()
			sc.Expect(handleCommandHit).To(specs.BeZero())
			sc.Expect(handleEnvelopeHit).To(specs.BeZero())
		})

		s.It("ctx deadline wins over a generous metadata deadline and timeout", func(sc *specs.Context) {
			engine, _ := newHarness(sc, "PrecedenceCtxBeforeMetadata")
			entityID := uuid.NewString()
			behavior := newEnvelopeCapturingEventSourcedBehavior(entityID)
			sc.Expect(engine.Entity(context.Background(), behavior)).To(specs.BeNil())

			op, err := command.NewOperationID("precedence-ctx-" + entityID)
			sc.Expect(err).To(specs.BeNil())
			md, err := command.NewMetadata(op, command.WithDeadline(time.Now().Add(time.Hour)))
			sc.Expect(err).To(specs.BeNil())
			env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 1}, md)
			sc.Expect(err).To(specs.BeNil())

			expiredCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Minute))
			defer cancel()

			result, err := engine.Dispatch(expiredCtx, entityID, env, time.Hour)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(result.Outcome()).To(specs.Equal(command.OutcomeTimedOut))

			handleCommandHit, handleEnvelopeHit, _ := behavior.Snapshot()
			sc.Expect(handleCommandHit).To(specs.BeZero())
			sc.Expect(handleEnvelopeHit).To(specs.BeZero())
		})

		s.It("caller timeout wins when it is the tightest bound", func(sc *specs.Context) {
			t := sc.T
			engine, store := newHarness(sc, "PrecedenceTimeoutBeforeBoth")
			entityID := uuid.NewString()
			blocking := newBlockingEventSourcedBehavior(entityID)
			sc.Expect(engine.Entity(context.Background(), blocking)).To(specs.BeNil())

			op, err := command.NewOperationID("precedence-timeout-" + entityID)
			sc.Expect(err).To(specs.BeNil())
			md, err := command.NewMetadata(op)
			sc.Expect(err).To(specs.BeNil())
			env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 1}, md)
			sc.Expect(err).To(specs.BeNil())

			outcomeCh := make(chan dispatchOutcome, 1)
			go func() {
				result, dispatchErr := engine.Dispatch(context.Background(), entityID, env, 100*time.Millisecond)
				outcomeCh <- dispatchOutcome{result: result, err: dispatchErr}
			}()

			select {
			case <-blocking.started:
			case <-time.After(2 * time.Second):
				t.Fatal("handler never started")
			}

			var outcome dispatchOutcome
			select {
			case outcome = <-outcomeCh:
			case <-time.After(3 * time.Second):
				t.Fatal("Dispatch never returned; a 100ms timeout with no Metadata/ctx deadline must still bound the wait")
			}
			sc.Expect(outcome.err).To(specs.BeNil())
			sc.Expect(outcome.result.Outcome()).To(specs.Equal(command.OutcomeTimedOut))

			close(blocking.release)
			event, err := store.GetLatestEvent(context.Background(), persistence.Unscoped(), entityID)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(event).To(specs.BeNil())
		})

		s.It("explicit caller cancellation is reported as OutcomeCanceled", func(sc *specs.Context) {
			engine, _ := newHarness(sc, "PrecedenceExplicitCancel")
			entityID := uuid.NewString()
			behavior := newEnvelopeCapturingEventSourcedBehavior(entityID)
			sc.Expect(engine.Entity(context.Background(), behavior)).To(specs.BeNil())

			op, err := command.NewOperationID("precedence-cancel-" + entityID)
			sc.Expect(err).To(specs.BeNil())
			md, err := command.NewMetadata(op)
			sc.Expect(err).To(specs.BeNil())
			env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 1}, md)
			sc.Expect(err).To(specs.BeNil())

			canceledCtx, cancel := context.WithCancel(context.Background())
			cancel()

			result, err := engine.Dispatch(canceledCtx, entityID, env, time.Minute)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(result.Outcome()).To(specs.Equal(command.OutcomeCanceled))
			sc.Expect(result.Err()).To(specs.MatchError(command.ErrCanceled))

			handleCommandHit, handleEnvelopeHit, _ := behavior.Snapshot()
			sc.Expect(handleCommandHit).To(specs.BeZero())
			sc.Expect(handleEnvelopeHit).To(specs.BeZero())
		})
	})
}
