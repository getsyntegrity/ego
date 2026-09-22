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
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/pablogore/ego/v4/command"
	"github.com/pablogore/ego/v4/persistence"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
)

// envelopeCapturingEventSourcedBehavior implements both EventSourcedBehavior
// and EventSourcedEnvelopeBehavior (#60). It records, for every invocation,
// which method the runtime actually called and, when it was HandleEnvelope,
// the command.Envelope it received — so tests can assert against the real
// dispatch path instead of inferring it.
type envelopeCapturingEventSourcedBehavior struct {
	id string
	// delay, when non-zero, is slept at the top of HandleCommand and
	// HandleEnvelope before applying the command — used to prove Dispatch
	// bounds SendSync's wait to the Metadata deadline rather than the
	// caller-supplied timeout when the deadline is tighter.
	delay time.Duration

	mu               sync.Mutex
	handleCommandHit int
	handleEnvelope   int
	lastEnvelope     command.Envelope
}

var (
	_ EventSourcedBehavior         = (*envelopeCapturingEventSourcedBehavior)(nil)
	_ EventSourcedEnvelopeBehavior = (*envelopeCapturingEventSourcedBehavior)(nil)
)

func newEnvelopeCapturingEventSourcedBehavior(id string) *envelopeCapturingEventSourcedBehavior {
	return &envelopeCapturingEventSourcedBehavior{id: id}
}

func (x *envelopeCapturingEventSourcedBehavior) ID() string { return x.id }

func (x *envelopeCapturingEventSourcedBehavior) InitialState() State {
	return new(testpb.Account)
}

func (x *envelopeCapturingEventSourcedBehavior) HandleCommand(_ context.Context, cmd Command, _ State) (events []Event, err error) {
	if x.delay > 0 {
		time.Sleep(x.delay)
	}
	x.mu.Lock()
	x.handleCommandHit++
	x.mu.Unlock()
	return x.apply(cmd)
}

func (x *envelopeCapturingEventSourcedBehavior) HandleEnvelope(_ context.Context, env command.Envelope, _ State) (events []Event, err error) {
	if x.delay > 0 {
		time.Sleep(x.delay)
	}
	x.mu.Lock()
	x.handleEnvelope++
	x.lastEnvelope = env
	x.mu.Unlock()
	return x.apply(env.Payload())
}

func (x *envelopeCapturingEventSourcedBehavior) apply(cmd Command) (events []Event, err error) {
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

func (x *envelopeCapturingEventSourcedBehavior) HandleEvent(_ context.Context, event Event, _ State) (state State, err error) {
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

func (x *envelopeCapturingEventSourcedBehavior) MarshalBinary() (data []byte, err error) {
	return json.Marshal(struct {
		ID string `json:"id"`
	}{ID: x.id})
}

func (x *envelopeCapturingEventSourcedBehavior) UnmarshalBinary(data []byte) error {
	aux := struct {
		ID string `json:"id"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	x.id = aux.ID
	return nil
}

func (x *envelopeCapturingEventSourcedBehavior) snapshot() (handleCommandHit, handleEnvelopeHit int, lastEnvelope command.Envelope) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.handleCommandHit, x.handleEnvelope, x.lastEnvelope
}

// TestEngineSendCommandDispatchesHandleEnvelope proves the M-3 wiring
// end-to-end: a behavior implementing the additive, optional
// EventSourcedEnvelopeBehavior interface (#60) receives HandleEnvelope, not
// HandleCommand, when reached through Engine.SendCommand — with a
// well-formed command.Envelope whose Metadata carries a generated
// OperationID and a Timestamp, exactly as command_context.go's Carrier
// rematerialization is meant to produce.
func TestEngineSendCommandDispatchesHandleEnvelope(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "EnvelopeWiring", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	behavior := newEnvelopeCapturingEventSourcedBehavior(entityID)
	require.NoError(t, engine.Entity(ctx, behavior))

	state, revision, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{
		AccountBalance: 100,
	}, time.Minute)
	require.NoError(t, err)
	assert.EqualValues(t, 1, revision)
	acct, ok := state.(*testpb.Account)
	require.True(t, ok)
	assert.EqualValues(t, 100, acct.GetAccountBalance())

	handleCommandHit, handleEnvelopeHit, env := behavior.snapshot()
	assert.Zero(t, handleCommandHit, "HandleCommand must not be called when the behavior implements HandleEnvelope and Metadata is available")
	assert.Equal(t, 1, handleEnvelopeHit)

	require.NotEmpty(t, env.Metadata().OperationID())
	assert.False(t, env.Metadata().Timestamp().IsZero())
	_, ok = command.PayloadAs[*testpb.CreateAccount](env)
	require.True(t, ok)

	require.NoError(t, engine.Stop(ctx))
}

// TestEngineDispatchDispatchesHandleEnvelope is the same proof against the
// new Dispatch entry point directly (rather than through the SendCommand
// adapter), confirming both entry points feed the same rematerialization
// path (command_context.go's attachCarrier/metadataFromContext).
func TestEngineDispatchDispatchesHandleEnvelope(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "EnvelopeWiringDispatch", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	behavior := newEnvelopeCapturingEventSourcedBehavior(entityID)
	require.NoError(t, engine.Entity(ctx, behavior))

	op, err := command.NewOperationID("test-op-" + entityID)
	require.NoError(t, err)
	md, err := command.NewMetadata(op)
	require.NoError(t, err)
	env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 42}, md)
	require.NoError(t, err)

	result, err := engine.Dispatch(ctx, entityID, env, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, command.OutcomeSuccess, result.Outcome())

	handleCommandHit, handleEnvelopeHit, gotEnv := behavior.snapshot()
	assert.Zero(t, handleCommandHit)
	assert.Equal(t, 1, handleEnvelopeHit)
	assert.Equal(t, op, gotEnv.Metadata().OperationID())

	require.NoError(t, engine.Stop(ctx))
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
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "EnvelopeWiringInvalidMetadata", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	behavior := newEnvelopeCapturingEventSourcedBehavior(entityID)
	require.NoError(t, engine.Entity(ctx, behavior))

	env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 100}, command.Metadata{})
	require.NoError(t, err)

	result, err := engine.Dispatch(ctx, entityID, env, time.Minute)
	require.Error(t, err)
	assert.ErrorIs(t, err, command.ErrInvalidMetadata)
	assert.Equal(t, command.Result{}, result)

	handleCommandHit, handleEnvelopeHit, _ := behavior.snapshot()
	assert.Zero(t, handleCommandHit, "HandleCommand must not run for a command rejected on invalid metadata")
	assert.Zero(t, handleEnvelopeHit, "HandleEnvelope must not run for a command rejected on invalid metadata")

	event, err := store.GetLatestEvent(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	assert.Nil(t, event, "the store must never be invoked for a command rejected on invalid metadata")

	require.NoError(t, engine.Stop(ctx))
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
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "EnvelopeWiringZeroValueEnvelope", store, WithLogger(DiscardLogger),
		WithTelemetry(&Telemetry{Tracer: otel.Tracer("test")}))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	behavior := newEnvelopeCapturingEventSourcedBehavior(entityID)
	require.NoError(t, engine.Entity(ctx, behavior))

	var zeroEnv command.Envelope

	require.NotPanics(t, func() {
		result, err := engine.Dispatch(ctx, entityID, zeroEnv, time.Minute)
		require.Error(t, err)
		assert.ErrorIs(t, err, command.ErrInvalidEnvelope)
		assert.Equal(t, command.Result{}, result)
	})

	handleCommandHit, handleEnvelopeHit, _ := behavior.snapshot()
	assert.Zero(t, handleCommandHit, "HandleCommand must not run for a zero-value envelope")
	assert.Zero(t, handleEnvelopeHit, "HandleEnvelope must not run for a zero-value envelope")

	event, err := store.GetLatestEvent(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	assert.Nil(t, event, "the store must never be invoked for a zero-value envelope")

	require.NoError(t, engine.Stop(ctx))
}

// TestEngineDispatchRejectsExpiredDeadlineWithoutInvokingHandler proves
// Dispatch rejects an Envelope whose Metadata deadline has already passed
// outright — no SendSync call, no actor, no handler, no persistence —
// rather than letting the command run and possibly persist after its
// declared deadline.
func TestEngineDispatchRejectsExpiredDeadlineWithoutInvokingHandler(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "EnvelopeWiringExpiredDeadline", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	behavior := newEnvelopeCapturingEventSourcedBehavior(entityID)
	require.NoError(t, engine.Entity(ctx, behavior))

	op, err := command.NewOperationID("expired-deadline-" + entityID)
	require.NoError(t, err)
	md, err := command.NewMetadata(op, command.WithDeadline(time.Now().Add(-time.Minute)))
	require.NoError(t, err)
	env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 100}, md)
	require.NoError(t, err)

	result, err := engine.Dispatch(ctx, entityID, env, time.Minute)
	require.NoError(t, err, "an expired deadline is an outcome (OutcomeTimedOut), not a Go error")
	assert.Equal(t, command.OutcomeTimedOut, result.Outcome())
	assert.ErrorIs(t, result.Err(), command.ErrTimedOut)

	handleCommandHit, handleEnvelopeHit, _ := behavior.snapshot()
	assert.Zero(t, handleCommandHit, "HandleCommand must not run for a command rejected on an already-expired deadline")
	assert.Zero(t, handleEnvelopeHit, "HandleEnvelope must not run for a command rejected on an already-expired deadline")

	event, err := store.GetLatestEvent(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	assert.Nil(t, event, "the store must never be invoked for a command rejected on an already-expired deadline")

	require.NoError(t, engine.Stop(ctx))
}

// TestEngineDispatchClampsTimeoutToDeadline proves Dispatch bounds
// SendSync's wait to the Metadata deadline, not the (looser)
// caller-supplied timeout, when the deadline is tighter: a handler slow
// enough to outlive the deadline but well within timeout must not be
// allowed to complete and persist past the deadline.
func TestEngineDispatchClampsTimeoutToDeadline(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "EnvelopeWiringDeadlineClamp", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	behavior := &envelopeCapturingEventSourcedBehavior{id: entityID, delay: 500 * time.Millisecond}
	require.NoError(t, engine.Entity(ctx, behavior))

	op, err := command.NewOperationID("deadline-clamp-" + entityID)
	require.NoError(t, err)
	md, err := command.NewMetadata(op, command.WithDeadline(time.Now().Add(100*time.Millisecond)))
	require.NoError(t, err)
	env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 100}, md)
	require.NoError(t, err)

	start := time.Now()
	result, err := engine.Dispatch(ctx, entityID, env, 10*time.Second)
	elapsed := time.Since(start)

	require.NoError(t, err, "an expired deadline is an outcome (OutcomeTimedOut), not a Go error")
	assert.Equal(t, command.OutcomeTimedOut, result.Outcome(), "SendSync must time out at the deadline rather than succeed at the handler's own 500ms")
	assert.ErrorIs(t, result.Err(), command.ErrTimedOut)
	assert.Less(t, elapsed, 400*time.Millisecond, "Dispatch must not wait anywhere near the 10s caller timeout when the deadline is 100ms out")

	event, err := store.GetLatestEvent(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	assert.Nil(t, event, "the actor's post-handler gate must discard the handler's output once the deadline has passed, even though the handler ignored ctx and ran to completion")

	require.NoError(t, engine.Stop(ctx))
}

// TestEngineSendCommandFallsBackToHandleCommandWithoutMetadata proves the
// additive-interface design's safety net: a behavior that DOES implement
// EventSourcedEnvelopeBehavior still falls back to HandleCommand when no
// Metadata is available for the incoming command (e.g. the entity is
// reached directly through the actor system rather than through
// Engine.Dispatch/SendCommand's Carrier attachment).
func TestEventSourcedActorFallsBackToHandleCommandWithoutMetadata(t *testing.T) {
	entity := &EventSourcedActor{
		behavior: newEnvelopeCapturingEventSourcedBehavior("no-metadata"),
	}

	events, err := entity.dispatchToBehavior(context.Background(), &testpb.CreateAccount{AccountBalance: 7}, new(testpb.Account))
	require.NoError(t, err)
	require.Len(t, events, 1)

	behavior := entity.behavior.(*envelopeCapturingEventSourcedBehavior)
	handleCommandHit, handleEnvelopeHit, _ := behavior.snapshot()
	assert.Equal(t, 1, handleCommandHit)
	assert.Zero(t, handleEnvelopeHit)
}

// envelopeCapturingDurableStateBehavior is the DurableStateBehavior
// counterpart of envelopeCapturingEventSourcedBehavior, proving the same
// M-3 wiring for DurableStateActor.dispatchToBehavior.
type envelopeCapturingDurableStateBehavior struct {
	id string

	mu               sync.Mutex
	handleCommandHit int
	handleEnvelope   int
	lastEnvelope     command.Envelope
}

var (
	_ DurableStateBehavior         = (*envelopeCapturingDurableStateBehavior)(nil)
	_ DurableStateEnvelopeBehavior = (*envelopeCapturingDurableStateBehavior)(nil)
)

func newEnvelopeCapturingDurableStateBehavior(id string) *envelopeCapturingDurableStateBehavior {
	return &envelopeCapturingDurableStateBehavior{id: id}
}

func (x *envelopeCapturingDurableStateBehavior) ID() string { return x.id }

func (x *envelopeCapturingDurableStateBehavior) InitialState() State {
	return new(testpb.Account)
}

// nolint
func (x *envelopeCapturingDurableStateBehavior) HandleCommand(_ context.Context, cmd Command, priorVersion uint64, _ State) (newState State, newVersion uint64, err error) {
	x.mu.Lock()
	x.handleCommandHit++
	x.mu.Unlock()
	return x.apply(cmd, priorVersion)
}

func (x *envelopeCapturingDurableStateBehavior) HandleEnvelope(_ context.Context, env command.Envelope, priorVersion uint64, _ State) (newState State, newVersion uint64, err error) {
	x.mu.Lock()
	x.handleEnvelope++
	x.lastEnvelope = env
	x.mu.Unlock()
	return x.apply(env.Payload(), priorVersion)
}

func (x *envelopeCapturingDurableStateBehavior) apply(cmd Command, priorVersion uint64) (State, uint64, error) {
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

func (x *envelopeCapturingDurableStateBehavior) MarshalBinary() (data []byte, err error) {
	return json.Marshal(struct {
		ID string `json:"id"`
	}{ID: x.id})
}

func (x *envelopeCapturingDurableStateBehavior) UnmarshalBinary(data []byte) error {
	aux := struct {
		ID string `json:"id"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	x.id = aux.ID
	return nil
}

func (x *envelopeCapturingDurableStateBehavior) snapshot() (handleCommandHit, handleEnvelopeHit int, lastEnvelope command.Envelope) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.handleCommandHit, x.handleEnvelope, x.lastEnvelope
}

// TestEngineSendCommandDispatchesDurableStateHandleEnvelope is the
// DurableStateActor counterpart of
// TestEngineSendCommandDispatchesHandleEnvelope.
func TestEngineSendCommandDispatchesDurableStateHandleEnvelope(t *testing.T) {
	ctx := context.Background()
	stateStore := testkit.NewDurableStore()
	require.NoError(t, stateStore.Connect(ctx))
	t.Cleanup(func() { _ = stateStore.Disconnect(ctx) })

	engine := newTestEngine(t, "EnvelopeWiringDurableState", nil,
		WithLogger(DiscardLogger),
		WithStateStore(stateStore),
	)
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	behavior := newEnvelopeCapturingDurableStateBehavior(entityID)
	require.NoError(t, engine.DurableStateEntity(ctx, behavior))

	state, revision, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{
		AccountBalance: 55,
	}, time.Minute)
	require.NoError(t, err)
	assert.EqualValues(t, 1, revision)
	acct, ok := state.(*testpb.Account)
	require.True(t, ok)
	assert.EqualValues(t, 55, acct.GetAccountBalance())

	handleCommandHit, handleEnvelopeHit, env := behavior.snapshot()
	assert.Zero(t, handleCommandHit)
	assert.Equal(t, 1, handleEnvelopeHit)
	require.NotEmpty(t, env.Metadata().OperationID())

	require.NoError(t, engine.Stop(ctx))
}

// TestDurableStateActorFallsBackToHandleCommandWithoutMetadata mirrors
// TestEventSourcedActorFallsBackToHandleCommandWithoutMetadata for
// DurableStateActor.dispatchToBehavior.
func TestDurableStateActorFallsBackToHandleCommandWithoutMetadata(t *testing.T) {
	entity := &DurableStateActor{
		behavior: newEnvelopeCapturingDurableStateBehavior("no-metadata"),
	}

	newState, newVersion, err := entity.dispatchToBehavior(context.Background(), &testpb.CreateAccount{AccountBalance: 9}, 0, new(testpb.Account))
	require.NoError(t, err)
	require.EqualValues(t, 1, newVersion)
	require.NotNil(t, newState)

	behavior := entity.behavior.(*envelopeCapturingDurableStateBehavior)
	handleCommandHit, handleEnvelopeHit, _ := behavior.snapshot()
	assert.Equal(t, 1, handleCommandHit)
	assert.Zero(t, handleEnvelopeHit)
}

// TestSagaActorAttachCommandMetadata exercises SagaCommand's dual metadata
// behavior (#60, issue #60's "SagaCommand gana metadata" scope item):
// when a SagaCommand carries an explicit Metadata, attachCommandMetadata
// must use it verbatim; when left as the zero value, it must derive a
// fresh child from the saga's own rootMetadata (correlation inherited,
// causation set to the saga's root operation).
func TestSagaActorAttachCommandMetadata(t *testing.T) {
	rootOp, err := command.NewOperationID("saga-root-1")
	require.NoError(t, err)
	rootMetadata, err := command.NewMetadata(rootOp)
	require.NoError(t, err)

	s := &SagaActor{
		sagaID:       "saga-root-1",
		rootMetadata: rootMetadata,
		logger:       DiscardLogger,
	}

	t.Run("zero-value metadata is auto-derived from the saga's root", func(t *testing.T) {
		ctx := s.attachCommandMetadata(context.Background(), command.Metadata{})

		md, ok := metadataFromContext(ctx)
		require.True(t, ok)
		assert.NotEqual(t, rootMetadata.OperationID(), md.OperationID(), "derived metadata must carry a fresh operation id, not the root's")
		assert.Equal(t, rootMetadata.CorrelationID(), md.CorrelationID(), "correlation id must be inherited from the root (D7)")
		causation, ok := md.CausationID()
		require.True(t, ok)
		assert.Equal(t, command.CausationID(rootMetadata.OperationID()), causation, "causation must be the saga's root operation")
	})

	t.Run("explicit metadata is used verbatim", func(t *testing.T) {
		explicitOp, err := command.NewOperationID("explicit-op-1")
		require.NoError(t, err)
		explicit, err := command.NewMetadata(explicitOp, command.WithCorrelationID("explicit-correlation"))
		require.NoError(t, err)

		ctx := s.attachCommandMetadata(context.Background(), explicit)

		md, ok := metadataFromContext(ctx)
		require.True(t, ok)
		assert.Equal(t, explicit.OperationID(), md.OperationID())
		assert.Equal(t, explicit.CorrelationID(), md.CorrelationID())
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
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "DeadlineDirectPath", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	behavior := newBlockingEventSourcedBehavior(entityID)
	require.NoError(t, engine.Entity(ctx, behavior))

	op, err := command.NewOperationID("deadline-direct-" + entityID)
	require.NoError(t, err)
	md, err := command.NewMetadata(op, command.WithDeadline(time.Now().Add(150*time.Millisecond)))
	require.NoError(t, err)
	env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 100}, md)
	require.NoError(t, err)

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
	require.NoError(t, outcome.err, "an expired deadline is an outcome (OutcomeTimedOut), not a Go error")
	assert.Equal(t, command.OutcomeTimedOut, outcome.result.Outcome())
	assert.ErrorIs(t, outcome.result.Err(), command.ErrTimedOut)

	// The handler is still blocked and nothing has been written either way.
	// Release it now and prove the actor's own post-handler gate — not the
	// caller giving up — is what keeps its output out of the store.
	close(behavior.release)

	state, revision, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{AccountBalance: 50}, time.Minute)
	require.NoError(t, err)
	assert.EqualValues(t, 1, revision, "the expired command must not have advanced entity.eventsCounter")
	acct, ok := state.(*testpb.Account)
	require.True(t, ok)
	assert.EqualValues(t, 50, acct.GetAccountBalance(), "state must come only from the follow-up command, not the discarded one")

	event, err := store.GetLatestEvent(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	require.NotNil(t, event)
	assert.EqualValues(t, 1, event.GetSequenceNumber(), "only the follow-up command's event may ever have been written")

	require.NoError(t, engine.Stop(ctx))
}

// TestDurableStateActorDiscardsHandlerOutputAfterDeadlineExpiry is the
// DurableStateActor counterpart of
// TestEventSourcedActorDirectPathDiscardsHandlerOutputAfterDeadlineExpiry.
func TestDurableStateActorDiscardsHandlerOutputAfterDeadlineExpiry(t *testing.T) {
	ctx := context.Background()
	stateStore := testkit.NewDurableStore()
	require.NoError(t, stateStore.Connect(ctx))
	t.Cleanup(func() { _ = stateStore.Disconnect(ctx) })

	engine := newTestEngine(t, "DeadlineDurableState", nil,
		WithLogger(DiscardLogger),
		WithStateStore(stateStore),
	)
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	behavior := newBlockingDurableStateBehavior(entityID)
	require.NoError(t, engine.DurableStateEntity(ctx, behavior))

	op, err := command.NewOperationID("deadline-durable-" + entityID)
	require.NoError(t, err)
	md, err := command.NewMetadata(op, command.WithDeadline(time.Now().Add(150*time.Millisecond)))
	require.NoError(t, err)
	env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 100}, md)
	require.NoError(t, err)

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
	require.NoError(t, outcome.err, "an expired deadline is an outcome (OutcomeTimedOut), not a Go error")
	assert.Equal(t, command.OutcomeTimedOut, outcome.result.Outcome())
	assert.ErrorIs(t, outcome.result.Err(), command.ErrTimedOut)

	close(behavior.release)

	state, revision, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{AccountBalance: 50}, time.Minute)
	require.NoError(t, err)
	assert.EqualValues(t, 1, revision, "the expired command must not have advanced entity.currentVersion")
	acct, ok := state.(*testpb.Account)
	require.True(t, ok)
	assert.EqualValues(t, 50, acct.GetAccountBalance())

	stored, err := stateStore.GetLatestState(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.EqualValues(t, 1, stored.GetVersionNumber(), "only the follow-up command's write may ever have reached the store")

	require.NoError(t, engine.Stop(ctx))
}

// TestEventSourcedActorBatchPathDoesNotContaminateBatchStateAfterDeadlineExpiry
// is the batch-path (processAndBatch) counterpart of
// TestEventSourcedActorDirectPathDiscardsHandlerOutputAfterDeadlineExpiry:
// an expired command's output must never reach entity.batchBuffer /
// entity.batchState / entity.batchEntries, so it can neither be flushed to
// the store itself nor corrupt a later, valid command sharing the same
// batch.
func TestEventSourcedActorBatchPathDoesNotContaminateBatchStateAfterDeadlineExpiry(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "DeadlineBatchPath", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	behavior := newBlockingEventSourcedBehavior(entityID)
	require.NoError(t, engine.Entity(ctx, behavior, WithBatchThreshold(1)))

	op, err := command.NewOperationID("deadline-batch-" + entityID)
	require.NoError(t, err)
	md, err := command.NewMetadata(op, command.WithDeadline(time.Now().Add(150*time.Millisecond)))
	require.NoError(t, err)
	env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 100}, md)
	require.NoError(t, err)

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
	require.NoError(t, outcome.err, "an expired deadline is an outcome (OutcomeTimedOut), not a Go error")
	assert.Equal(t, command.OutcomeTimedOut, outcome.result.Outcome())

	close(behavior.release)

	state, revision, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{AccountBalance: 50}, time.Minute)
	require.NoError(t, err)
	assert.EqualValues(t, 1, revision, "the expired command must never have entered batchBuffer/batchState")
	acct, ok := state.(*testpb.Account)
	require.True(t, ok)
	assert.EqualValues(t, 50, acct.GetAccountBalance())

	event, err := store.GetLatestEvent(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	require.NotNil(t, event)
	assert.EqualValues(t, 1, event.GetSequenceNumber(), "only the follow-up command's event may ever have reached the batch writer")

	require.NoError(t, engine.Stop(ctx))
}

// TestEngineDispatchEffectiveDeadlinePrecedence proves Dispatch computes the
// effective deadline as min(ctx's own deadline, the envelope Metadata's
// deadline, the caller-supplied timeout) rather than any single source
// alone: whichever source is tightest is honored even when the other two
// are generous, and an explicit caller cancellation is reported distinctly
// as OutcomeCanceled rather than OutcomeTimedOut.
func TestEngineDispatchEffectiveDeadlinePrecedence(t *testing.T) {
	newHarness := func(t *testing.T, name string) (*Engine, *testkit.EventStore) {
		t.Helper()
		ctx := context.Background()
		store := testkit.NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		t.Cleanup(func() { _ = store.Disconnect(ctx) })

		engine := newTestEngine(t, name, store, WithLogger(DiscardLogger))
		require.NoError(t, engine.Start(ctx))
		t.Cleanup(func() { _ = engine.Stop(context.Background()) })
		return engine, store
	}

	t.Run("metadata deadline wins over a generous timeout", func(t *testing.T) {
		engine, _ := newHarness(t, "PrecedenceMetadataBeforeTimeout")
		entityID := uuid.NewString()
		behavior := newEnvelopeCapturingEventSourcedBehavior(entityID)
		require.NoError(t, engine.Entity(context.Background(), behavior))

		op, err := command.NewOperationID("precedence-md-" + entityID)
		require.NoError(t, err)
		md, err := command.NewMetadata(op, command.WithDeadline(time.Now().Add(-time.Minute)))
		require.NoError(t, err)
		env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 1}, md)
		require.NoError(t, err)

		result, err := engine.Dispatch(context.Background(), entityID, env, time.Hour)
		require.NoError(t, err)
		assert.Equal(t, command.OutcomeTimedOut, result.Outcome())

		handleCommandHit, handleEnvelopeHit, _ := behavior.snapshot()
		assert.Zero(t, handleCommandHit)
		assert.Zero(t, handleEnvelopeHit, "a 1-hour timeout must not override an already-expired Metadata deadline")
	})

	t.Run("ctx deadline wins over a generous metadata deadline and timeout", func(t *testing.T) {
		engine, _ := newHarness(t, "PrecedenceCtxBeforeMetadata")
		entityID := uuid.NewString()
		behavior := newEnvelopeCapturingEventSourcedBehavior(entityID)
		require.NoError(t, engine.Entity(context.Background(), behavior))

		op, err := command.NewOperationID("precedence-ctx-" + entityID)
		require.NoError(t, err)
		md, err := command.NewMetadata(op, command.WithDeadline(time.Now().Add(time.Hour)))
		require.NoError(t, err)
		env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 1}, md)
		require.NoError(t, err)

		expiredCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Minute))
		defer cancel()

		result, err := engine.Dispatch(expiredCtx, entityID, env, time.Hour)
		require.NoError(t, err)
		assert.Equal(t, command.OutcomeTimedOut, result.Outcome())

		handleCommandHit, handleEnvelopeHit, _ := behavior.snapshot()
		assert.Zero(t, handleCommandHit)
		assert.Zero(t, handleEnvelopeHit, "an already-expired ctx deadline must be honored even though Metadata/timeout are generous")
	})

	t.Run("caller timeout wins when it is the tightest bound", func(t *testing.T) {
		engine, store := newHarness(t, "PrecedenceTimeoutBeforeBoth")
		entityID := uuid.NewString()
		blocking := newBlockingEventSourcedBehavior(entityID)
		require.NoError(t, engine.Entity(context.Background(), blocking))

		op, err := command.NewOperationID("precedence-timeout-" + entityID)
		require.NoError(t, err)
		md, err := command.NewMetadata(op)
		require.NoError(t, err)
		env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 1}, md)
		require.NoError(t, err)

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
		require.NoError(t, outcome.err)
		assert.Equal(t, command.OutcomeTimedOut, outcome.result.Outcome())

		close(blocking.release)
		event, err := store.GetLatestEvent(context.Background(), persistence.Unscoped(), entityID)
		require.NoError(t, err)
		assert.Nil(t, event, "the handler's output must be discarded once released, since the caller timeout already expired")
	})

	t.Run("explicit caller cancellation is reported as OutcomeCanceled", func(t *testing.T) {
		engine, _ := newHarness(t, "PrecedenceExplicitCancel")
		entityID := uuid.NewString()
		behavior := newEnvelopeCapturingEventSourcedBehavior(entityID)
		require.NoError(t, engine.Entity(context.Background(), behavior))

		op, err := command.NewOperationID("precedence-cancel-" + entityID)
		require.NoError(t, err)
		md, err := command.NewMetadata(op)
		require.NoError(t, err)
		env, err := command.NewEnvelope(&testpb.CreateAccount{AccountBalance: 1}, md)
		require.NoError(t, err)

		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel()

		result, err := engine.Dispatch(canceledCtx, entityID, env, time.Minute)
		require.NoError(t, err, "a caller cancellation is an outcome (OutcomeCanceled), not a Go error")
		assert.Equal(t, command.OutcomeCanceled, result.Outcome())
		assert.ErrorIs(t, result.Err(), command.ErrCanceled)

		handleCommandHit, handleEnvelopeHit, _ := behavior.snapshot()
		assert.Zero(t, handleCommandHit)
		assert.Zero(t, handleEnvelopeHit)
	})
}
