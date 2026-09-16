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

	"github.com/pablogore/ego/v4/command"
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
	x.mu.Lock()
	x.handleCommandHit++
	x.mu.Unlock()
	return x.apply(cmd)
}

func (x *envelopeCapturingEventSourcedBehavior) HandleEnvelope(_ context.Context, env command.Envelope, _ State) (events []Event, err error) {
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
