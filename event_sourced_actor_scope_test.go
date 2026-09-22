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
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/eventstream"
	"github.com/pablogore/ego/v4/internal/extensions"
	"github.com/pablogore/ego/v4/internal/pause"
	mocks "github.com/pablogore/ego/v4/mocks/persistence"
	"github.com/pablogore/ego/v4/persistence"
	"github.com/pablogore/ego/v4/tenancy"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
)

// TestEventSourcedActorSpawnBindsExactTenantScope covers TENANT-003 T4's
// core guarantee: once PreStart binds entity.scope via resolveScope, every
// store call this actor makes — both the read during recovery and the
// write from a live command — carries the exact tenant scope resolved for
// this spawn, never persistence.Unscoped(). A *mocks.EventsStore is used
// instead of the real testkit.EventStore precisely so that a call made
// with the wrong scope is observable directly: mockery's generated mock
// only matches an expectation whose argument values compare equal, so an
// unexpected-scope call panics rather than silently succeeding against
// some other (scope, persistenceID) bucket.
func TestEventSourcedActorSpawnBindsExactTenantScope(t *testing.T) {
	ctx := context.Background()
	persistenceID := uuid.NewString()
	behavior := NewAccountEventSourcedBehavior(persistenceID)

	scopeA, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)

	store := new(mocks.EventsStore)
	store.EXPECT().Ping(mock.Anything).Return(nil)
	store.EXPECT().GetLatestEvent(mock.Anything, scopeA, persistenceID).Return(nil, nil)
	store.EXPECT().WriteEvents(mock.Anything, scopeA, mock.Anything, mock.Anything).Return(nil).Once()

	eventStream := eventstream.New()

	actorSystem, err := goakt.NewActorSystem("TestActorSystem",
		goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
		goakt.WithExtensions(
			extensions.NewEventsStore(store),
			extensions.NewEventsStream(eventStream),
			extensions.NewTenancyMarker(),
		),
		goakt.WithActorInitMaxRetries(3))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	pause.For(time.Second)

	actor := newEventSourcedActor()
	pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
		goakt.WithDependencies(behavior, extensions.NewEntityTenantScope("acme")),
		goakt.WithLongLived(), goakt.WithStashing())
	require.NoError(t, err, "PreStart must succeed and recover using the spawn-bound scope, not Unscoped()")
	require.NotNil(t, pid)
	pause.For(time.Second)

	tenantA, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)
	ctxA, err := tenancy.Attach(ctx, tenantA)
	require.NoError(t, err)

	reply, err := goakt.Ask(ctxA, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
	require.NoError(t, err)
	commandReply, ok := reply.(*egopb.CommandReply)
	require.True(t, ok)
	require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply(),
		"the command must succeed against the mock store bound to the exact tenant scope")

	pause.For(time.Second)

	// Every registered expectation above named scopeA explicitly (never
	// mock.Anything for the scope argument): if either GetLatestEvent
	// (recovery) or WriteEvents (the live command's persist) had instead
	// been called with persistence.Unscoped(), that call would not have
	// matched any expectation and the mock would have panicked well before
	// this point. AssertExpectations proves the expected calls did happen.
	store.AssertExpectations(t)

	eventStream.Close()
	pause.For(time.Second)
	require.NoError(t, actorSystem.Stop(ctx))
}

// TestEventSourcedActorPreStartFailsClosedWithoutTenantScope covers
// TENANT-003 T4's fail-closed guard: when tenancy is active (the actor
// system carries extensions.TenancyExtensionID) but the per-spawn
// extensions.EntityTenantScope dependency was not injected, PreStart must
// refuse to start the actor — via resolveScope, before validateAndRecover
// ever runs — rather than falling back to persistence.Unscoped(). No store
// method may be invoked at all: not Ping, not GetLatestEvent, not
// WriteEvents. No expectation is registered on the mock below, so any call
// to it at all would panic; the explicit AssertNotCalled checks make that
// guarantee an assertion rather than an accident of test ordering.
func TestEventSourcedActorPreStartFailsClosedWithoutTenantScope(t *testing.T) {
	ctx := context.Background()
	persistenceID := uuid.NewString()
	behavior := NewAccountEventSourcedBehavior(persistenceID)

	store := new(mocks.EventsStore)
	eventStream := eventstream.New()

	actorSystem, err := goakt.NewActorSystem("TestActorSystem",
		goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
		goakt.WithExtensions(
			extensions.NewEventsStore(store),
			extensions.NewEventsStream(eventStream),
			extensions.NewTenancyMarker(),
		),
		goakt.WithActorInitMaxRetries(1))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	pause.For(time.Second)

	actor := newEventSourcedActor()
	// Deliberately no extensions.NewEntityTenantScope dependency: tenancy is
	// active (extensions.NewTenancyMarker() above), but no scope was bound
	// for this spawn.
	pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
		goakt.WithDependencies(behavior),
		goakt.WithLongLived(), goakt.WithStashing())
	require.Error(t, err, "PreStart must fail closed when tenancy is active and no tenant scope was injected")
	require.True(t, errors.Is(err, ErrEntityTenantScopeMissing),
		"the rejection must be the typed ErrEntityTenantScopeMissing, not an invented error")
	require.Nil(t, pid)
	pause.For(time.Second)

	store.AssertNotCalled(t, "Ping", mock.Anything)
	store.AssertNotCalled(t, "GetLatestEvent", mock.Anything, mock.Anything, mock.Anything)
	store.AssertNotCalled(t, "WriteEvents", mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	eventStream.Close()
	pause.For(time.Second)
	require.NoError(t, actorSystem.Stop(ctx))
}

// TestEventSourcedActorLegacyModeAlwaysUsesUnscopedStore is the regression
// guard for existing non-tenant users of TENANT-003 T4: when no
// tenancy.TenantResolver is configured (no extensions.TenancyMarker on the
// actor system, entity.tenantAware ends up false), every store call this
// actor makes must still carry persistence.Unscoped() exactly as it did
// before TENANT-003 T4 introduced entity.scope. The mock store below only
// has expectations registered for persistence.Unscoped(): a call with any
// other scope value would not match and would panic the mock.
func TestEventSourcedActorLegacyModeAlwaysUsesUnscopedStore(t *testing.T) {
	ctx := context.Background()
	persistenceID := uuid.NewString()
	behavior := NewAccountEventSourcedBehavior(persistenceID)

	store := new(mocks.EventsStore)
	store.EXPECT().Ping(mock.Anything).Return(nil)
	store.EXPECT().GetLatestEvent(mock.Anything, persistence.Unscoped(), persistenceID).Return(nil, nil)
	store.EXPECT().WriteEvents(mock.Anything, persistence.Unscoped(), mock.Anything, mock.Anything).Return(nil).Once()

	eventStream := eventstream.New()

	// No extensions.NewTenancyMarker() here: legacy mode, exactly like the
	// pre-TENANT-003 actor system configuration.
	actorSystem, err := goakt.NewActorSystem("TestActorSystem",
		goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
		goakt.WithExtensions(
			extensions.NewEventsStore(store),
			extensions.NewEventsStream(eventStream),
		),
		goakt.WithActorInitMaxRetries(3))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	pause.For(time.Second)

	actor := newEventSourcedActor()
	pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
		goakt.WithDependencies(behavior),
		goakt.WithLongLived(), goakt.WithStashing())
	require.NoError(t, err)
	require.NotNil(t, pid)
	pause.For(time.Second)

	reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
	require.NoError(t, err)
	commandReply, ok := reply.(*egopb.CommandReply)
	require.True(t, ok)
	require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

	pause.For(time.Second)
	store.AssertExpectations(t)

	eventStream.Close()
	pause.For(time.Second)
	require.NoError(t, actorSystem.Stop(ctx))
}
