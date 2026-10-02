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

package runtime

import (
	"context"
	"time"

	"github.com/getsyntegrity/urd/command"
	"github.com/getsyntegrity/urd/eventstream"
	"github.com/getsyntegrity/urd/port/behavior"
)

// The interfaces below are the application side of the runtime SPI. Each
// capability is its own small interface, so a consumer can depend on only the
// capability it uses; Runtime combines them.
//
// Every method of every interface follows the same two rules:
//
//   - A runtime that has not started, or has stopped, returns
//     ErrEngineNotStarted.
//   - A runtime that does not provide an operation still implements the
//     method, and returns an error matching ErrUnsupported (usually an
//     *UnsupportedError) before any side effect, never a panic. Such an error
//     never reports a transient failure: a runtime that provides the operation
//     but cannot reach its store returns the store's error.
//
// When both rules apply, ErrUnsupported comes first: whether a runtime
// provides an operation does not depend on whether it has started, so an
// unsupported operation reports ErrUnsupported in every lifecycle state.
//
// Whether a runtime provides an operation cannot be read from its method set,
// since every runtime implements every method; only the call's error says so.

// Entities spawns event-sourced and durable-state entities and invokes them by
// their string ID.
type Entities interface {
	// SpawnEventSourced spawns an event-sourced entity for b, identified by
	// b.ID(). The entity handles commands, persists the resulting events and
	// rebuilds its state from them; send it commands with SendCommand or
	// Dispatch. opts configure this spawn only; a runtime reads them with
	// ResolveSpawnOptions.
	//
	// It returns ErrEventsStoreRequired, before anything is spawned, when the
	// runtime has no events store. When tenancy is active, the spawn fails
	// with ErrSpawnTenantUndetermined, ErrSpawnTenantMismatch or
	// ErrSpawnTenantUnverified as those errors describe. When the runtime
	// declares its entity families and b's family is not among them, it
	// returns an error matching ErrEntityFamilyNotDeclared and spawns nothing.
	SpawnEventSourced(ctx context.Context, b behavior.EventSourced, opts ...SpawnOption) error

	// SpawnDurableState spawns a durable-state entity for b, identified by
	// b.ID(). The entity handles commands and persists only its latest state.
	//
	// It returns ErrDurableStateStoreRequired, before anything is spawned,
	// when the runtime has no durable state store. Tenancy and entity-family
	// errors are those of SpawnEventSourced.
	SpawnDurableState(ctx context.Context, b behavior.DurableState, opts ...SpawnOption) error

	// EntityExists reports whether an entity with the given ID is currently
	// alive. It works for event-sourced and durable-state entities and is a
	// liveness probe only: it does not spawn the entity, recover its state or
	// replay its journal, so an entity that is persisted but not currently
	// hydrated (passivated, or not yet spawned after a restart) reports false.
	//
	// It returns (true, nil) when the entity is alive, (false, nil) when no
	// live entity has that ID, and (false, err) when the lookup fails.
	EntityExists(ctx context.Context, entityID string) (bool, error)

	// SendCommand sends cmd to the entity identified by entityID and waits at
	// most timeout for its reply. The entity validates the command, applies
	// the resulting state change and persists it.
	//
	// It returns the entity's state after the command and its revision, a
	// monotonically increasing version of the persisted state. When the
	// command causes no state change, resultingState is nil. It returns
	// ErrUndefinedEntityID when entityID is empty, and ErrNotACommand when
	// cmd is a runtime-internal control message.
	//
	// SendCommand is the legacy shape of Dispatch: it carries cmd in a
	// command.Envelope with metadata derived from ctx, and maps the
	// command.Result back to (State, revision, error).
	SendCommand(ctx context.Context, entityID string, cmd behavior.Command, timeout time.Duration) (resultingState behavior.State, revision uint64, err error)

	// Dispatch sends env's payload, with env's metadata, to the entity
	// identified by entityID and returns the canonical command.Result.
	//
	// The effective deadline is the earliest of ctx's deadline, the deadline
	// in env's metadata and now+timeout. Dispatch returns a timed-out or
	// canceled Result, without delivering the command, when ctx is already
	// done or the effective deadline has already passed. It returns
	// ErrNotACommand when the payload is a runtime-internal control message.
	Dispatch(ctx context.Context, entityID string, env command.Envelope, timeout time.Duration) (command.Result, error)

	// EraseEntity performs GDPR erasure for persistenceID. When the runtime's
	// encryptor is backed by a key store, it deletes the entity's encryption
	// key (crypto-shredding), which makes its encrypted events and snapshots
	// unreadable. When full is true, it also deletes the entity's events and
	// snapshots from the stores.
	//
	// When tenancy is active, erasure is scoped to the caller's tenant and
	// fails closed when the caller has no tenant identity.
	EraseEntity(ctx context.Context, persistenceID string, full bool) error
}

// Sagas spawns sagas and reports their status.
type Sagas interface {
	// SpawnSaga spawns a saga for b, identified by b.ID(). The saga reacts to
	// events, sends commands to entities and compensates on failure. timeout
	// bounds the whole saga; zero means no timeout.
	//
	// It returns ErrEventsStoreRequired, before anything is spawned, when the
	// runtime has no events store. Tenancy and entity-family errors are those
	// of Entities.SpawnEventSourced.
	SpawnSaga(ctx context.Context, b behavior.Saga, timeout time.Duration, opts ...SpawnOption) error

	// SagaStatus returns the ID, status and current state of the saga
	// identified by sagaID, waiting at most timeout for the answer. It returns
	// ErrUndefinedEntityID when sagaID is empty, and an error when the saga is
	// not found or the query fails.
	//
	// SagaInfo.Status is the saga's lifecycle status when it answers:
	// SagaRunning, SagaCompensating, SagaCompleted or SagaFailed. An adapter
	// that runs a compensation to its end before answering (the GoAkt adapter,
	// *engine.Engine, does) reports SagaCompleted or SagaFailed rather than
	// SagaCompensating.
	SagaStatus(ctx context.Context, sagaID string, timeout time.Duration) (*SagaInfo, error)
}

// Projections controls the projections registered with the runtime. A
// projection is registered by name when the runtime is built (with
// engine.WithProjection for the GoAkt adapter); these methods address it by that
// name.
type Projections interface {
	// StartProjection starts the named projection. Once started, it processes
	// events from the events store with its registered handler and keeps its
	// position in its offset store across restarts.
	//
	// It returns ErrProjectionNotRegistered when name was never registered.
	StartProjection(ctx context.Context, name string) error

	// StopProjection stops the named projection, which then receives no more
	// events. It returns an error when the projection cannot be stopped or
	// does not exist.
	StopProjection(ctx context.Context, name string) error

	// IsProjectionRunning reports whether the named projection is running.
	// Check the error: when the status cannot be determined, it is non-nil
	// and the boolean may be a false negative.
	IsProjectionRunning(ctx context.Context, name string) (bool, error)

	// RebuildProjection stops the named projection, resets its offset to
	// from, and starts it again, so it reprocesses every event from that
	// point on. The zero time.Time, time.Time{}, replays from the beginning.
	// It requires an offset store.
	RebuildProjection(ctx context.Context, name string, from time.Time) error

	// ProjectionLag reports, per shard of the events store, how far the named
	// projection is behind the newest event persisted in that shard: the time
	// between that event and the projection's committed offset, clamped at 0.
	// A 0 value means the projection is caught up for that shard. It requires
	// an offset store.
	ProjectionLag(ctx context.Context, name string) (map[uint64]time.Duration, error)
}

// Events gives access to the runtime's in-process event stream.
type Events interface {
	// Subscribe returns a new subscriber to the runtime's event stream,
	// already subscribed to the topics on which the runtime publishes the
	// events and states its entities persist.
	Subscribe() (eventstream.Subscriber, error)
}

// Runtime is every capability together. It is what a composition root hands
// out; a consumer narrows it to the capability it needs, for example
// var entities runtime.Entities = r. *engine.Engine, the GoAkt adapter,
// implements it.
type Runtime interface {
	Entities
	Sagas
	Projections
	Events
}
