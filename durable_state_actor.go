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
	"fmt"
	"math"
	"time"

	goakt "github.com/tochemey/goakt/v4/actor"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/eventstream"
	"github.com/pablogore/ego/v4/internal/extensions"
	"github.com/pablogore/ego/v4/internal/runner"
	"github.com/pablogore/ego/v4/persistence"
	"github.com/pablogore/ego/v4/tenancy"
)

// statesTopic is the single in-process pub/sub topic eGo's durable-state
// entities publish to and the engine's state publishers/subscribers consume
// from. The shard each state version belongs to is carried in
// egopb.DurableState.Shard, so downstream consumers can filter by shard
// without the topic name having to encode it.
const statesTopic = "topic.states"

// DurableStateActor is a durable state based actor
type DurableStateActor struct {
	behavior        DurableStateBehavior
	stateStore      persistence.StateStore
	currentState    State
	cachedStateAny  *anypb.Any // cached marshal of currentState, invalidated on state change
	currentVersion  uint64
	lastCommandTime time.Time
	eventsStream    eventstream.Stream
	actorSystem     goakt.ActorSystem
	persistenceID   string
	tracer          trace.Tracer
	metrics         *metrics

	// Cached values computed once at startup to avoid per-command allocations.
	shardNumber uint64

	// tenantAware reports whether the actor system was built from a Config
	// with a tenancy.TenantResolver registered (extensions.TenancyMarker
	// present). It is presence-only: the actor never holds a resolver and
	// never calls Resolve — see PreStart. When true, the pre-handler gate in
	// processCommand requires a TenantContext to already be attached to the
	// incoming context (set by Engine.SendCommand at the trust boundary)
	// before HandleCommand runs.
	tenantAware bool

	// actorTenant records this actor's tenant identity for its full
	// lifetime (DS1/DS2, EGO-TENANT-002 PR2), mirroring
	// EventSourcedActor.actorTenant position-for-position. Seeded once in
	// recoverFromStore (DS2) when recovering a committed (version > 0)
	// record, or committed together with the first successful command's
	// state and version in commitState (PR2 review round 2 P1 fix) when
	// starting at genesis — never before HandleCommand and checkPreconditions
	// both succeed, and never before the durable write itself succeeds. Every
	// subsequent command is checked against it via tenancy.VerifyUnchanged.
	// Holds noTenantContext (the zero value, declared
	// event_sourced_actor.go:96, same package) until seeded or committed.
	// No-op in legacy mode.
	actorTenant tenancy.TenantContext
}

// implements the goakt.Actor interface
var _ goakt.Actor = (*DurableStateActor)(nil)

// newDurableStateActor creates an instance of an actor provided the DurableStateBehavior
func newDurableStateActor() *DurableStateActor {
	return &DurableStateActor{}
}

// PreStart pre-starts the actor
func (entity *DurableStateActor) PreStart(ctx *goakt.Context) error {
	entity.stateStore = ctx.Extension(extensions.DurableStateStoreExtensionID).(*extensions.DurableStateStore).Underlying()
	entity.eventsStream = ctx.Extension(extensions.EventsStreamExtensionID).(*extensions.EventsStream).Underlying()
	entity.persistenceID = ctx.ActorName()
	// Presence-only signal: tenant-aware mode is active when the engine
	// registered the tenancy marker extension. The marker carries no
	// resolver (internal/extensions.TenancyMarker), so the actor can never
	// reach a TenantResolver through it.
	entity.tenantAware = ctx.Extension(extensions.TenancyExtensionID) != nil
	// Computed here rather than in PostStart's Receive: PreStart runs on the
	// actor's spawning goroutine before it is registered as running, so it
	// happens-before any concurrent PostStop triggered by an early Shutdown/
	// Kill. Reading shardNumber in PostStop (via persistStateAndPublish) while
	// it was still being written from the dispatcher's PostStart handling was
	// a genuine data race caught by -race.
	entity.shardNumber = ctx.ActorSystem().Partition(entity.persistenceID)

	for _, dependency := range ctx.Dependencies() {
		if dependency != nil {
			if behavior, ok := dependency.(DurableStateBehavior); ok {
				entity.behavior = behavior
				break
			}
		}
	}

	if ext := ctx.Extension(extensions.TelemetryExtensionID); ext != nil {
		telExt := ext.(*extensions.TelemetryExtension)
		entity.tracer = telExt.Tracer()
		entity.metrics = newMetrics(telExt.Meter())
	}

	if err := runner.
		New(runner.WithFailFast()).
		AddRunner(entity.durableStateRequired).
		AddRunner(func() error {
			if entity.behavior == nil {
				return fmt.Errorf("behavior is required")
			}
			return nil
		}).
		AddRunner(func() error { return entity.stateStore.Ping(ctx.Context()) }).
		AddRunner(func() error { return entity.recoverFromStore(ctx.Context()) }).
		Run(); err != nil {
		return err
	}

	if entity.metrics != nil {
		entity.metrics.entitiesActive.Add(ctx.Context(), 1)
	}

	return nil
}

// Receive processes any message dropped into the actor mailbox.
func (entity *DurableStateActor) Receive(ctx *goakt.ReceiveContext) {
	switch message := ctx.Message().(type) {
	case *goakt.PostStart:
		entity.actorSystem = ctx.ActorSystem()
	case *egopb.GetStateCommand:
		entity.getStateAndReply(ctx)
	default:
		msg := message.(Command)
		entity.processCommand(ctx, msg)
	}
}

// PostStop prepares the actor to gracefully shutdown.
//
// T4-B exclusion (design.md D4): this lifecycle flush deliberately carries
// no verifyTenantForPersist gate. It persists whatever state the actor
// already holds in memory — state that only ever landed there via
// commitState's single commit point. There is no new tenant identity to
// re-confirm here, and ctx.Context() at shutdown is not the per-command
// context T4-B reasons about; adding a gate here would just fail closed on a
// shutdown path for no defensive benefit.
//
// Version-gated skip (DS3, EGO-TENANT-002 PR2, PR2 review round 2 P1 fix):
// currentVersion is the durable discriminant for "has this actor ever
// committed a command" — commitState only ever advances it together with
// actorTenant, in the same in-memory assignment, so checking
// currentVersion > 0 here is equivalent to checking actorTenant was
// committed, without relying on actorTenant's zero value as a stand-in for
// that fact. An actor that starts at genesis and receives no command is
// exactly the actor still at version 0. Flushing that state would persist a
// DurableState whose tenant_metadata is an empty map, which recoverFromStore
// then refuses to recover from — bricking the persistence ID permanently.
// Skipping the flush here loses nothing (in-memory state is still exactly
// InitialState() at version 0) and keeps fail-closed in the strong
// direction: no tenant-less record is ever written. Legacy mode is
// unaffected — it keeps today's unconditional flush.
func (entity *DurableStateActor) PostStop(ctx *goakt.Context) error {
	if entity.metrics != nil {
		entity.metrics.entitiesActive.Add(ctx.Context(), -1)
	}
	chain := runner.
		New(runner.WithFailFast()).
		AddRunner(func() error { return entity.stateStore.Ping(ctx.Context()) })
	if !entity.tenantAware || entity.currentVersion > 0 {
		chain = chain.AddRunner(func() error { return entity.persistStateAndPublish(ctx.Context()) })
	}
	return chain.Run()
}

// recoverFromStore reset the persistent actor to the latest state in case there is one
// this is vital when the entity actor is restarting.
func (entity *DurableStateActor) recoverFromStore(ctx context.Context) error {
	durableState, err := entity.stateStore.GetLatestState(ctx, entity.persistenceID)
	if err != nil {
		return fmt.Errorf("failed to get the latest state: %w", err)
	}

	// Genesis: no record at all. Nothing to recover.
	if durableState == nil {
		entity.currentState = entity.behavior.InitialState()
		return nil
	}

	// Legacy genesis (DS2, EGO-TENANT-002 PR2 review round 2 P1 fix): in
	// tenant-aware mode, a record at version 0 carries no committed tenant to
	// recover and none to enforce. commitState only ever advances the version
	// together with a committed tenant, and checkPreconditions only ever
	// admits a version exactly 1 above the prior one, so a durable record can
	// only be at version 0 by way of a legacy installation's PostStop, which
	// used to flush InitialState() unconditionally even when the actor never
	// received a command. Treat it exactly like no record at all, discarding
	// its payload and any tenant_metadata rather than attempting to unmarshal
	// either. version > 0 is never legacy-genesis and always requires valid
	// tenant metadata below. Non-tenant-aware installations never carried
	// this ambiguity: they keep validating whatever payload is on record
	// regardless of version, unchanged from before this fix.
	if entity.tenantAware && durableState.GetVersionNumber() == 0 {
		entity.currentState = entity.behavior.InitialState()
		return nil
	}

	// Seed this actor's lifetime tenant identity from the recovered record's
	// carried metadata (DS2, EGO-TENANT-002 PR2), before its state payload is
	// touched. recoverFromStore is this actor's only recovery path — no
	// snapshot, no replay — so there is a single seed source and no
	// cross-check to perform: unlike EventSourcedActor's seedActorTenant,
	// this assigns directly. Absent or malformed metadata on a committed
	// (version > 0) tenant-aware record is not tolerated: it means this
	// record predates tenancy or was corrupted, and the actor must refuse to
	// start rather than silently run without an identity (fail-closed,
	// UnmarshalMetadata's own ErrInvalid is the rejection).
	if entity.tenantAware {
		tc, err := tenancy.UnmarshalMetadata(tenancy.Metadata(durableState.GetTenantMetadata()))
		if err != nil {
			return fmt.Errorf("failed to unmarshal durable state tenant metadata: %w", err)
		}
		entity.actorTenant = tc
	}

	currentState := entity.behavior.InitialState()
	if resultingState := durableState.GetResultingState(); resultingState != nil {
		if err := resultingState.UnmarshalTo(currentState); err != nil {
			return fmt.Errorf("failed to unmarshal the latest state: %w", err)
		}
	}

	entity.currentState = currentState
	entity.currentVersion = durableState.GetVersionNumber()
	return nil
}

// processCommand processes the incoming command
func (entity *DurableStateActor) processCommand(receiveContext *goakt.ReceiveContext, command Command) {
	ctx := receiveContext.Context()
	startTime := time.Now()

	if entity.tracer != nil {
		var span trace.Span
		ctx, span = entity.tracer.Start(ctx, "ego.command",
			trace.WithAttributes(
				attribute.String("ego.persistence_id", entity.persistenceID),
				attribute.String("ego.command_type", string(command.ProtoReflect().Descriptor().FullName())),
			))
		defer span.End()
	}

	if entity.metrics != nil {
		entity.metrics.commandsTotal.Add(ctx, 1)
		defer func() {
			duration := float64(time.Since(startTime).Milliseconds())
			entity.metrics.commandsDuration.Record(ctx, duration)
		}()
	}

	// Pre-handler gate (T4-A): in tenant-aware mode, HandleCommand must never
	// run without a TenantContext already attached by Engine.SendCommand.
	// This reuses tenancy.Require, a read-only check of the context already
	// in hand; it never calls a resolver and never re-resolves.
	//
	// candidateTenant is deliberately NOT written into entity.actorTenant
	// here (PR2 review round 2 P1 fix): a command that fails HandleCommand
	// or checkPreconditions below must never appropriate the actor for a
	// tenant that accepted no mutation. It is carried through to commitState,
	// the single point — immediately after WriteState confirms the durable
	// write, before Publish — where committed state, committed version, and
	// owning tenant land together.
	//
	// Cross-tenant extension (DS1, EGO-TENANT-002 PR2): once actorTenant is
	// seeded (DS2, recovery) or committed (a prior successful command on this
	// actor), a command resolving to a different tenant is rejected here —
	// before HandleCommand runs and before entity.currentState/currentVersion
	// are ever mutated, which is exactly why this identity check cannot live
	// in commitState instead: by that point the foreign tenant's state would
	// already be resident in memory.
	var candidateTenant tenancy.TenantContext
	if entity.tenantAware {
		tc, err := tenancy.Require(ctx)
		if err != nil {
			entity.sendErrorReply(receiveContext, err)
			return
		}
		if entity.actorTenant != noTenantContext {
			if verifyErr := tenancy.VerifyUnchanged(entity.actorTenant, tc); verifyErr != nil {
				entity.sendErrorReply(receiveContext, verifyErr)
				return
			}
		}
		candidateTenant = tc
	}

	newState, newVersion, err := entity.behavior.HandleCommand(ctx, command, entity.currentVersion, entity.currentState)
	if err != nil {
		entity.sendErrorReply(receiveContext, err)
		return
	}

	// check whether the pre-conditions have met
	if err := entity.checkPreconditions(newState, newVersion); err != nil {
		entity.sendErrorReply(receiveContext, err)
		return
	}

	// Defensive persistence invariant (T4-B, design.md D4): re-confirm a
	// valid tenant identity is still present before this state is committed.
	// This reuses tenancy.Require — a read-only check of the context already
	// validated by the pre-handler gate above — and never re-invokes
	// TenantResolver.Resolve. It is deliberately not the sole enforcement
	// point: T4-A above already blocks HandleCommand itself.
	if err := entity.verifyTenantForPersist(ctx); err != nil {
		entity.sendErrorReply(receiveContext, err)
		return
	}

	if err := entity.commitState(ctx, newState, newVersion, time.Now(), candidateTenant); err != nil {
		entity.sendErrorReply(receiveContext, err)
		return
	}

	entity.sendStateReply(receiveContext)
}

// currentStateAny returns the cached anypb.Any of currentState, computing it
// only when the state has changed since the last call.
func (entity *DurableStateActor) currentStateAny() *anypb.Any {
	if entity.cachedStateAny == nil {
		entity.cachedStateAny, _ = anypb.New(entity.currentState)
	}
	return entity.cachedStateAny
}

// getStateAndReply returns the last committed state of the entity without
// processing any command. Mirrors EventSourcedActor.getStateAndReply's gate
// (DS4, EGO-TENANT-002 PR2): Receive dispatches *egopb.GetStateCommand here
// directly, bypassing processCommand and its T4-A gate entirely, so without
// its own check any resolved tenant could read another tenant's full
// committed durable state.
func (entity *DurableStateActor) getStateAndReply(ctx *goakt.ReceiveContext) {
	if entity.tenantAware {
		tc, err := tenancy.Require(ctx.Context())
		if err != nil {
			entity.sendErrorReply(ctx, err)
			return
		}
		if entity.actorTenant != noTenantContext {
			if verifyErr := tenancy.VerifyUnchanged(entity.actorTenant, tc); verifyErr != nil {
				entity.sendErrorReply(ctx, verifyErr)
				return
			}
		}
	}
	entity.sendStateReply(ctx)
}

// sendStateReply sends a state reply message
func (entity *DurableStateActor) sendStateReply(ctx *goakt.ReceiveContext) {
	ctx.Response(&egopb.CommandReply{
		Reply: &egopb.CommandReply_StateReply{
			StateReply: &egopb.StateReply{
				PersistenceId:  entity.persistenceID,
				State:          entity.currentStateAny(),
				SequenceNumber: entity.currentVersion,
				Timestamp:      entity.lastCommandTime.UnixNano(),
			},
		},
	})
}

// sendErrorReply sends an error as a reply message
func (entity *DurableStateActor) sendErrorReply(ctx *goakt.ReceiveContext, err error) {
	ctx.Response(&egopb.CommandReply{
		Reply: &egopb.CommandReply_ErrorReply{
			ErrorReply: &egopb.ErrorReply{
				Message: err.Error(),
			},
		},
	})
}

// checkAndSetPreconditions validates the newState and the newVersion
func (entity *DurableStateActor) checkPreconditions(newState State, newVersion uint64) error {
	currentState := entity.currentState
	currentStateType := currentState.ProtoReflect().Descriptor().FullName()
	latestStateType := newState.ProtoReflect().Descriptor().FullName()
	if currentStateType != latestStateType {
		return fmt.Errorf("mismatch state types: %s != %s", currentStateType, latestStateType)
	}

	proceed := int(math.Abs(float64(newVersion-entity.currentVersion))) == 1
	if !proceed {
		return fmt.Errorf("%s received version=(%d) while current version is (%d)",
			entity.persistenceID,
			newVersion,
			entity.currentVersion)
	}
	return nil
}

// checks whether the durable state store is set or not
func (entity *DurableStateActor) durableStateRequired() error {
	if entity.stateStore == nil {
		return ErrDurableStateStoreRequired
	}
	return nil
}

// verifyTenantForPersist re-confirms, from ctx alone, that a valid tenant
// identity is present before this command's state is committed (T4-B,
// design.md D4). It is a no-op in legacy mode (tenantAware == false) and,
// in tenant-aware mode, does nothing but read the context already attached
// by Engine.SendCommand and validated by the pre-handler gate: it never
// invokes a TenantResolver and is never the sole enforcement point for
// fail-closed behavior.
func (entity *DurableStateActor) verifyTenantForPersist(ctx context.Context) error {
	if !entity.tenantAware {
		return nil
	}
	_, err := tenancy.Require(ctx)
	return err
}

// commitState is the single logical commit point (PR2 review round 2 P1
// fix): committed state, committed version, and owning tenant land together,
// in that order, only once WriteState confirms the durable write succeeded.
//
// It never reads entity.currentState/currentVersion/actorTenant to build the
// record — newState, newVersion, and candidateTenant are the record, passed
// in explicitly by processCommand once HandleCommand and checkPreconditions
// have both already succeeded — so a WriteState failure here leaves entity
// exactly as it was before the command: no state mutated, no tenant
// appropriated.
//
// The in-memory commit (currentState/cachedStateAny/currentVersion/
// actorTenant) happens immediately after WriteState succeeds and before
// Publish, so a Publish failure can never un-appropriate what the store
// already durably committed.
func (entity *DurableStateActor) commitState(ctx context.Context, newState State, newVersion uint64, commandTime time.Time, candidateTenant tenancy.TenantContext) error {
	newStateAny, err := anypb.New(newState)
	if err != nil {
		return err
	}

	durableState := &egopb.DurableState{
		PersistenceId:  entity.persistenceID,
		VersionNumber:  newVersion,
		ResultingState: newStateAny,
		Timestamp:      commandTime.UnixNano(),
		Shard:          entity.shardNumber,
	}
	if entity.tenantAware {
		durableState.TenantMetadata = tenancy.MarshalMetadata(candidateTenant)
	}

	if err := entity.stateStore.WriteState(ctx, durableState); err != nil {
		return err
	}

	entity.currentState = newState
	entity.cachedStateAny = newStateAny
	entity.lastCommandTime = commandTime
	entity.currentVersion = newVersion
	if entity.tenantAware {
		entity.actorTenant = candidateTenant
	}

	entity.eventsStream.Publish(statesTopic, durableState)
	return nil
}

// persistStateAndPublish flushes the actor's already-committed in-memory
// state to the store. Used only by PostStop's lifecycle flush (T4-B
// exclusion, design.md D4): there is no new candidate tenant to commit here,
// only whatever state/version/tenant a prior successful command already
// committed via commitState, or, in legacy mode, whatever is in memory.
func (entity *DurableStateActor) persistStateAndPublish(ctx context.Context) error {
	durableState := &egopb.DurableState{
		PersistenceId:  entity.persistenceID,
		VersionNumber:  entity.currentVersion,
		ResultingState: entity.currentStateAny(),
		Timestamp:      entity.lastCommandTime.UnixNano(),
		Shard:          entity.shardNumber,
	}

	// DS3 (EGO-TENANT-002 PR2): write from entity.actorTenant, never from
	// ctx — PostStop's ctx.Context() is a shutdown context that never
	// carried a TenantContext, and actorTenant is already committed by the
	// time PostStop reaches here (commitState, or legacy mode's unconditional
	// flush).
	if entity.tenantAware {
		durableState.TenantMetadata = tenancy.MarshalMetadata(entity.actorTenant)
	}

	if err := entity.stateStore.WriteState(ctx, durableState); err != nil {
		return err
	}

	entity.eventsStream.Publish(statesTopic, durableState)
	return nil
}
