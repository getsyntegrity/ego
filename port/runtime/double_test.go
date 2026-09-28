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

package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/getsyntegrity/ego/v4/command"
	"github.com/getsyntegrity/ego/v4/eventstream"
	"github.com/getsyntegrity/ego/v4/port/behavior"
	"github.com/getsyntegrity/ego/v4/port/runtime"
)

// The contract is implementable without GoAkt and without package ego:
// TestRuntimeTestClosureExcludesGoAktAndRoot checks that this file's package
// depends on neither.
var _ runtime.Runtime = (*double)(nil)

// doubleRuntime is the runtime name the double reports in *UnsupportedError.
const doubleRuntime = "double"

// double is a map-backed runtime with no actor system, no mailbox, no
// persistence and no projections (openspec/changes/ego-runtime-001/design.md
// §D7). It proves the contract's shape, not a runtime's behavior: it hosts
// event-sourced entities and runs their commands synchronously, the way
// testkit/scenario.go does, and answers every other operation with an
// *UnsupportedError, before any side effect.
type double struct {
	entities map[string]*doubleEntity
}

type doubleEntity struct {
	behavior behavior.EventSourced
	state    behavior.State
	revision uint64
	settings runtime.SpawnSettings
}

func newDouble() *double {
	return &double{entities: make(map[string]*doubleEntity)}
}

func unsupported(operation string) error {
	return &runtime.UnsupportedError{Runtime: doubleRuntime, Operation: operation}
}

func (d *double) SpawnEventSourced(_ context.Context, b behavior.EventSourced, opts ...runtime.SpawnOption) error {
	if b == nil {
		return errors.New("double: nil behavior")
	}
	id := b.ID()
	if id == "" {
		return runtime.ErrUndefinedEntityID
	}
	if _, ok := d.entities[id]; ok {
		return nil // re-spawning a live id is an idempotent success
	}
	d.entities[id] = &doubleEntity{
		behavior: b,
		state:    b.InitialState(),
		settings: runtime.ResolveSpawnOptions(opts...),
	}
	return nil
}

func (d *double) SpawnDurableState(context.Context, behavior.DurableState, ...runtime.SpawnOption) error {
	return unsupported("SpawnDurableState")
}

func (d *double) EntityExists(context.Context, string) (bool, error) {
	return false, unsupported("EntityExists")
}

func (d *double) SendCommand(ctx context.Context, entityID string, cmd behavior.Command, _ time.Duration) (behavior.State, uint64, error) {
	if entityID == "" {
		return nil, 0, runtime.ErrUndefinedEntityID
	}
	e, ok := d.entities[entityID]
	if !ok {
		return nil, 0, fmt.Errorf("double: entity %q not found", entityID)
	}
	events, err := e.behavior.HandleCommand(ctx, cmd, e.state)
	if err != nil {
		return nil, 0, err
	}
	if len(events) == 0 {
		return nil, e.revision, nil
	}
	state := e.state
	for _, event := range events {
		if state, err = e.behavior.HandleEvent(ctx, event, state); err != nil {
			return nil, 0, err
		}
	}
	e.state = state
	e.revision += uint64(len(events))
	return e.state, e.revision, nil
}

func (d *double) Dispatch(context.Context, string, command.Envelope, time.Duration) (command.Result, error) {
	return command.Result{}, unsupported("Dispatch")
}

func (d *double) EraseEntity(context.Context, string, bool) error {
	return unsupported("EraseEntity")
}

func (d *double) SpawnSaga(context.Context, behavior.Saga, time.Duration, ...runtime.SpawnOption) error {
	return unsupported("SpawnSaga")
}

func (d *double) SagaStatus(context.Context, string, time.Duration) (*runtime.SagaInfo, error) {
	return nil, unsupported("SagaStatus")
}

func (d *double) StartProjection(context.Context, string) error {
	return unsupported("StartProjection")
}

func (d *double) StopProjection(context.Context, string) error {
	return unsupported("StopProjection")
}

func (d *double) IsProjectionRunning(context.Context, string) (bool, error) {
	return false, unsupported("IsProjectionRunning")
}

func (d *double) RebuildProjection(context.Context, string, time.Time) error {
	return unsupported("RebuildProjection")
}

func (d *double) ProjectionLag(context.Context, string) (map[uint64]time.Duration, error) {
	return nil, unsupported("ProjectionLag")
}

func (d *double) Subscribe() (eventstream.Subscriber, error) {
	return nil, unsupported("Subscribe")
}

// counter is an event-sourced behavior over wrapperspb values: a command is
// an Int64Value to add, its event is that same value, and the state is the
// running total. A zero command produces no event.
type counter struct{ id string }

var _ behavior.EventSourced = (*counter)(nil)

func (c *counter) ID() string                   { return c.id }
func (c *counter) InitialState() behavior.State { return wrapperspb.Int64(0) }

func (c *counter) HandleCommand(_ context.Context, cmd behavior.Command, _ behavior.State) ([]behavior.Event, error) {
	add, ok := cmd.(*wrapperspb.Int64Value)
	if !ok {
		return nil, fmt.Errorf("counter: unexpected command %T", cmd)
	}
	if add.GetValue() == 0 {
		return nil, nil
	}
	return []behavior.Event{wrapperspb.Int64(add.GetValue())}, nil
}

func (c *counter) HandleEvent(_ context.Context, event behavior.Event, prior behavior.State) (behavior.State, error) {
	total, ok := prior.(*wrapperspb.Int64Value)
	if !ok {
		return nil, fmt.Errorf("counter: unexpected state %T", prior)
	}
	added, ok := event.(*wrapperspb.Int64Value)
	if !ok {
		return nil, fmt.Errorf("counter: unexpected event %T", event)
	}
	return wrapperspb.Int64(total.GetValue() + added.GetValue()), nil
}

// ledger is a durable-state behavior, spawned only to show that an
// unsupported spawn hosts nothing.
type ledger struct{}

var _ behavior.DurableState = ledger{}

func (ledger) ID() string                   { return "ledger-1" }
func (ledger) InitialState() behavior.State { return wrapperspb.Int64(0) }
func (ledger) HandleCommand(context.Context, behavior.Command, uint64, behavior.State) (behavior.State, uint64, error) {
	return nil, 0, errors.New("ledger: never called")
}

// idleSaga is a saga, spawned only to show that an unsupported spawn hosts
// nothing.
type idleSaga struct{}

var _ behavior.Saga = idleSaga{}

func (idleSaga) ID() string                   { return "saga-1" }
func (idleSaga) InitialState() behavior.State { return wrapperspb.Int64(0) }
func (idleSaga) HandleEvent(context.Context, behavior.Event, behavior.State) (*behavior.SagaAction, error) {
	return nil, nil
}
func (idleSaga) HandleResult(context.Context, string, behavior.State, behavior.State) (*behavior.SagaAction, error) {
	return nil, nil
}
func (idleSaga) HandleError(context.Context, string, error, behavior.State) (*behavior.SagaAction, error) {
	return nil, nil
}
func (idleSaga) ApplyEvent(context.Context, behavior.Event, behavior.State) (behavior.State, error) {
	return nil, nil
}
func (idleSaga) Compensate(context.Context, behavior.State) ([]behavior.SagaCommand, error) {
	return nil, nil
}

type doubleKeyA struct{}

type doubleKeyB struct{}

// spawn goes through the capability a consumer would hold, runtime.Entities.
func spawn(t *testing.T, entities runtime.Entities, id string, opts ...runtime.SpawnOption) {
	t.Helper()
	require.NoError(t, entities.SpawnEventSourced(context.Background(), &counter{id: id}, opts...))
}

func TestDoubleSpawnResolvesDocumentedDefaults(t *testing.T) {
	d := newDouble()
	spawn(t, d, "account-1")

	s := d.entities["account-1"].settings
	require.Zero(t, s.PassivateAfter())
	require.False(t, s.Relocation())
	require.Equal(t, runtime.RestartDirective, s.SupervisorDirective())
	require.Equal(t, runtime.RoundRobin, s.Placement())
	require.Empty(t, s.Tenant())
}

func TestDoubleSpawnAppliesOptionsInOrderAndSkipsNil(t *testing.T) {
	d := newDouble()
	spawn(t, d, "account-1",
		runtime.WithPlacement(runtime.Random),
		nil,
		runtime.WithPlacement(runtime.Local),
		runtime.WithSupervisorDirective(runtime.StopDirective),
		runtime.WithAdapterSetting(doubleKeyA{}, 42),
	)

	s := d.entities["account-1"].settings
	require.Equal(t, runtime.Local, s.Placement(), "a later option overrides an earlier one")
	require.Equal(t, runtime.StopDirective, s.SupervisorDirective())

	value, ok := s.AdapterSetting(doubleKeyA{})
	require.True(t, ok, "an adapter setting is visible under its own key")
	require.Equal(t, 42, value)
	_, ok = s.AdapterSetting(doubleKeyB{})
	require.False(t, ok, "an adapter setting is invisible under another key")
}

func TestDoubleSendCommandRunsTheBehavior(t *testing.T) {
	d := newDouble()
	spawn(t, d, "account-1")
	var entities runtime.Entities = d
	ctx := context.Background()

	state, revision, err := entities.SendCommand(ctx, "account-1", wrapperspb.Int64(5), time.Second)
	require.NoError(t, err)
	require.True(t, proto.Equal(wrapperspb.Int64(5), state))
	require.EqualValues(t, 1, revision)

	state, revision, err = entities.SendCommand(ctx, "account-1", wrapperspb.Int64(7), time.Second)
	require.NoError(t, err)
	require.True(t, proto.Equal(wrapperspb.Int64(12), state))
	require.EqualValues(t, 2, revision)

	state, revision, err = entities.SendCommand(ctx, "account-1", wrapperspb.Int64(0), time.Second)
	require.NoError(t, err)
	require.Nil(t, state, "no event, no state update")
	require.EqualValues(t, 2, revision)

	_, _, err = entities.SendCommand(ctx, "", wrapperspb.Int64(1), time.Second)
	require.ErrorIs(t, err, runtime.ErrUndefinedEntityID)

	_, _, err = entities.SendCommand(ctx, "unknown", wrapperspb.Int64(1), time.Second)
	require.Error(t, err)
	require.NotErrorIs(t, err, runtime.ErrUnsupported, "a missing entity is not an unsupported operation")
}

// TestDoubleUnsupportedOperations calls every operation the double lacks
// through the capability interface a consumer would hold, and checks the
// contract of design §D4: the error matches ErrUnsupported and
// errors.ErrUnsupported, names the runtime and the operation, and is returned
// before any side effect (the hosted entities are unchanged).
func TestDoubleUnsupportedOperations(t *testing.T) {
	d := newDouble()
	spawn(t, d, "account-1")
	_, _, err := d.SendCommand(context.Background(), "account-1", wrapperspb.Int64(3), time.Second)
	require.NoError(t, err)

	var (
		entities    runtime.Entities    = d
		sagas       runtime.Sagas       = d
		projections runtime.Projections = d
		events      runtime.Events      = d
	)
	ctx := context.Background()
	env, err := command.NewEnvelope(wrapperspb.Int64(1), command.Metadata{})
	require.NoError(t, err)

	cases := []struct {
		operation string
		call      func(t *testing.T) error
	}{
		{"SpawnDurableState", func(*testing.T) error { return entities.SpawnDurableState(ctx, ledger{}) }},
		{"EntityExists", func(t *testing.T) error {
			exists, err := entities.EntityExists(ctx, "account-1")
			require.False(t, exists)
			return err
		}},
		{"Dispatch", func(*testing.T) error {
			_, err := entities.Dispatch(ctx, "account-1", env, time.Second)
			return err
		}},
		{"EraseEntity", func(*testing.T) error { return entities.EraseEntity(ctx, "account-1", true) }},
		{"SpawnSaga", func(*testing.T) error { return sagas.SpawnSaga(ctx, idleSaga{}, time.Second) }},
		{"SagaStatus", func(t *testing.T) error {
			info, err := sagas.SagaStatus(ctx, "saga-1", time.Second)
			require.Nil(t, info)
			return err
		}},
		{"StartProjection", func(*testing.T) error { return projections.StartProjection(ctx, "balances") }},
		{"StopProjection", func(*testing.T) error { return projections.StopProjection(ctx, "balances") }},
		{"IsProjectionRunning", func(t *testing.T) error {
			running, err := projections.IsProjectionRunning(ctx, "balances")
			require.False(t, running)
			return err
		}},
		{"RebuildProjection", func(*testing.T) error {
			return projections.RebuildProjection(ctx, "balances", time.Time{})
		}},
		{"ProjectionLag", func(t *testing.T) error {
			lag, err := projections.ProjectionLag(ctx, "balances")
			require.Nil(t, lag)
			return err
		}},
		{"Subscribe", func(t *testing.T) error {
			subscriber, err := events.Subscribe()
			require.Nil(t, subscriber)
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.operation, func(t *testing.T) {
			before := maps.Clone(d.entities)
			hosted := *d.entities["account-1"]

			err := tc.call(t)

			require.ErrorIs(t, err, runtime.ErrUnsupported)
			require.ErrorIs(t, err, errors.ErrUnsupported)
			var unsupportedErr *runtime.UnsupportedError
			require.ErrorAs(t, err, &unsupportedErr)
			require.Equal(t, doubleRuntime, unsupportedErr.Runtime)
			require.Equal(t, tc.operation, unsupportedErr.Operation)

			require.Equal(t, before, d.entities, "no entity was added or removed")
			after := d.entities["account-1"]
			require.Equal(t, hosted.revision, after.revision, "the hosted entity is unchanged")
			require.True(t, proto.Equal(hosted.state, after.state), "the hosted entity is unchanged")
		})
	}
}
