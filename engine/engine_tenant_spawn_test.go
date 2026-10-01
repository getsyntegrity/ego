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

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"

	"github.com/getsyntegrity/ego/persistence"
	"github.com/getsyntegrity/ego/tenancy"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
)

// This file covers TENANT-003 T4's corrected spawn-time tenant design: CI
// caught the earlier design's Resolve-Once, Propagate-After violation
// (TestSendCommandResolverSwapIdenticalSequence, engine_test.go, observed a
// resolver invoked twice for one spawn-plus-command sequence). The engine
// now never calls tenancy.TenantResolver.Resolve at spawn; instead the
// application declares an entity/durable-state entity/saga's tenant via
// engine.WithTenant, and the built-in tenancy.WithSingleTenant resolver
// exposes its fixed tenant via tenancy.FixedTenantResolver so single-tenant
// mode needs no such declaration (acceptance criterion 6). See
// engine.go's spawnTenantScope and spawn_config.go's WithTenant.

// TestEngineEntitySpawnRequiresExplicitTenantWhenResolverHasNoFixedTenant
// covers the fail-closed half of the corrected design: tenancy is active (a
// resolver is registered), that resolver exposes no fixed tenant (it is not
// a tenancy.FixedTenantResolver, or reports it has none), and the caller
// did not pass engine.WithTenant. The spawn must be refused outright with the
// typed ErrSpawnTenantUndetermined, and — because the engine never calls
// Resolve to find this out — no store method may be invoked at all.
func TestEngineEntitySpawnRequiresExplicitTenantWhenResolverHasNoFixedTenant(t *testing.T) {
	specs.Describe(t, "a spawn without WithTenant is refused when the resolver has no fixed tenant", func(s *specs.Spec) {
		bg := context.Background()

		s.It("Entity refuses to spawn and touches no store", func(ctx *specs.Context) {
			// A controller with no expectation: any store call fails the case. ANY
			// call to it (Ping, GetLatestEvent, WriteEvents, ...) is reported as an
			// unexpected call, and spawnTenantScope must reject before the actor is
			// ever created, let alone reaches a store.
			store := eventsStoreMock{mock.NewController(ctx)}

			engine := newSpecsEngineG3(ctx, "Sample", store, WithTenantResolver(&stubTenantResolver{id: "acme"}))
			ctx.Expect(engine.Start(bg)).To(specs.BeNil())

			entityID := uuid.NewString()
			probe := newTenancyProbeEventSourcedBehavior(entityID)

			// The rejection must be the typed ErrSpawnTenantUndetermined, not an invented error.
			ctx.Expect(engine.Entity(bg, probe)).To(specs.MatchError(ErrSpawnTenantUndetermined))

			// No actor may be spawned when the tenant cannot be determined.
			exists, existsErr := engine.EntityExists(bg, entityID)
			ctx.Expect(existsErr).To(specs.BeNil())
			ctx.Expect(exists).To(specs.BeFalse())
			// HandleCommand must never run: the entity was never spawned.
			ctx.Expect(probe.InvocationCount()).To(specs.BeZero())

			ctx.Expect(engine.Stop(bg)).To(specs.BeNil())
		})

		s.It("DurableStateEntity refuses to spawn and touches no store", func(ctx *specs.Context) {
			store := newConnectedEventsStoreG3(ctx)
			durableStore := stateStoreMock{mock.NewController(ctx)}

			engine := newSpecsEngineG3(ctx, "Sample", store,
				WithTenantResolver(&stubTenantResolver{id: "acme"}),
				WithStateStore(durableStore))
			ctx.Expect(engine.Start(bg)).To(specs.BeNil())

			entityID := uuid.NewString()
			behavior := NewAccountDurableStateBehavior(entityID)

			ctx.Expect(engine.DurableStateEntity(bg, behavior)).To(specs.MatchError(ErrSpawnTenantUndetermined))

			exists, existsErr := engine.EntityExists(bg, entityID)
			ctx.Expect(existsErr).To(specs.BeNil())
			ctx.Expect(exists).To(specs.BeFalse())

			ctx.Expect(engine.Stop(bg)).To(specs.BeNil())
		})
	})
}

// TestEngineEntitySpawnWithExplicitTenantResolvesOnce is the dedicated
// regression guard for the defect CI caught: for one spawn-plus-command
// sequence against an ordinary multi-tenant resolver, Resolve must be
// invoked exactly once — by SendCommand, at the command trust boundary —
// never a second time at spawn. See also
// TestSendCommandResolverSwapIdenticalSequence's "multi-tenant resolver"
// subtest (engine_test.go), which proves the same invariant end to end
// against SendCommand's observed TenantContext.
func TestEngineEntitySpawnWithExplicitTenantResolvesOnce(t *testing.T) {
	specs.Describe(t, "Resolve is invoked once per spawn-plus-command sequence, by SendCommand", func(s *specs.Spec) {
		s.It("never resolves at spawn when the tenant was declared via WithTenant", func(ctx *specs.Context) {
			bg := context.Background()
			store := newConnectedEventsStoreG3(ctx)

			resolver := &countingTenantResolver{id: "acme"}
			engine := newSpecsEngineG3(ctx, "Sample", store, WithTenantResolver(resolver))
			ctx.Expect(engine.Start(bg)).To(specs.BeNil())

			entityID := uuid.NewString()
			probe := newTenancyProbeEventSourcedBehavior(entityID)
			ctx.Expect(engine.Entity(bg, probe, WithTenant(tenancy.TenantID("acme")))).To(specs.BeNil())

			// spawn must never call Resolve when the tenant was declared via WithTenant
			ctx.Expect(resolver.callCount()).To(specs.BeZero())

			_, _, err := engine.SendCommand(bg, entityID, &testpb.CreateAccount{AccountBalance: 500}, time.Minute)
			ctx.Expect(err).To(specs.BeNil())

			// Resolve must be invoked exactly once, by SendCommand, never at spawn
			ctx.Expect(resolver.callCount()).To(specs.Equal(int64(1)))

			ctx.Expect(engine.Stop(bg)).To(specs.BeNil())
		})
	})
}

// TestEngineWithSingleTenantSpawnNeedsNoWithTenant covers acceptance
// criterion 6 directly at the spawn boundary: a tenancy.WithSingleTenant
// resolver lets Entity spawn with zero application-invented tenant
// plumbing — no engine.WithTenant — because it exposes its fixed tenant via
// tenancy.FixedTenantResolver, and the spawned entity is still correctly
// bound to that tenant's scope (proven by a subsequent command observing
// it).
func TestEngineWithSingleTenantSpawnNeedsNoWithTenant(t *testing.T) {
	specs.Describe(t, "a single-tenant resolver spawns an entity without WithTenant", func(s *specs.Spec) {
		s.It("binds the entity to the resolver's fixed tenant scope", func(ctx *specs.Context) {
			bg := context.Background()
			store := newConnectedEventsStoreG3(ctx)

			resolver, err := tenancy.WithSingleTenant(tenancy.TenantID("acme"))
			ctx.Expect(err).To(specs.BeNil())

			engine := newSpecsEngineG3(ctx, "Sample", store, WithTenantResolver(resolver))
			ctx.Expect(engine.Start(bg)).To(specs.BeNil())

			entityID := uuid.NewString()
			probe := newTenancyProbeEventSourcedBehavior(entityID)

			// No engine.WithTenant option at all.
			ctx.Expect(engine.Entity(bg, probe)).To(specs.BeNil())

			// The entity must spawn using the resolver's fixed tenant.
			exists, err := engine.EntityExists(bg, entityID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(exists).To(specs.BeTrue())

			_, _, err = engine.SendCommand(bg, entityID, &testpb.CreateAccount{AccountBalance: 500}, time.Minute)
			ctx.Expect(err).To(specs.BeNil())

			scope, err := persistence.NewTenantScope(tenancy.TenantID("acme"))
			ctx.Expect(err).To(specs.BeNil())
			latest, err := store.GetLatestEvent(bg, scope, entityID)
			ctx.Expect(err).To(specs.BeNil())
			// The entity must have been bound to and written under the resolver's fixed tenant scope.
			ctx.Expect(latest).To(specs.Not(specs.BeNil()))

			ctx.Expect(engine.Stop(bg)).To(specs.BeNil())
		})
	})
}

// TestEngineEntitySpawnWithoutResolverStaysUnscoped covers the legacy-mode
// invariant that must survive this correction unchanged: with no
// tenancy.TenantResolver registered at all, Entity spawn requires no
// engine.WithTenant, injects no tenant scope, and every store call still
// carries persistence.Unscoped(), exactly as before TENANT-003.
func TestEngineEntitySpawnWithoutResolverStaysUnscoped(t *testing.T) {
	specs.Describe(t, "legacy mode without a resolver", func(s *specs.Spec) {
		s.It("spawns without WithTenant and writes under persistence.Unscoped()", func(ctx *specs.Context) {
			bg := context.Background()
			store := newConnectedEventsStoreG3(ctx)

			engine := newSpecsEngineG3(ctx, "Sample", store, WithLogger(DiscardLogger))
			ctx.Expect(engine.Start(bg)).To(specs.BeNil())

			entityID := uuid.NewString()
			probe := newTenancyProbeEventSourcedBehavior(entityID)
			ctx.Expect(engine.Entity(bg, probe)).To(specs.BeNil())

			_, _, err := engine.SendCommand(bg, entityID, &testpb.CreateAccount{AccountBalance: 500}, time.Minute)
			ctx.Expect(err).To(specs.BeNil())

			latest, err := store.GetLatestEvent(bg, persistence.Unscoped(), entityID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(latest).To(specs.Not(specs.BeNil()))

			ctx.Expect(engine.Stop(bg)).To(specs.BeNil())
		})
	})
}

// TestEngineCommandRejectsTenantMismatchWithSpawnDeclaredTenant proves the
// spawn-declared tenant (engine.WithTenant) is not merely advisory: a command
// whose resolver-attached TenantContext disagrees with the tenant the actor
// was actually spawned under must be rejected by the actor's existing
// cross-check (tenancy.VerifyUnchanged), before HandleCommand ever runs.
func TestEngineCommandRejectsTenantMismatchWithSpawnDeclaredTenant(t *testing.T) {
	specs.Describe(t, "a command resolved to another tenant than the spawn-declared one", func(s *specs.Spec) {
		s.It("is rejected before HandleCommand runs", func(ctx *specs.Context) {
			bg := context.Background()
			store := newConnectedEventsStoreG3(ctx)

			// perCallerTenantResolver (option_test.go) resolves whichever tenant id
			// the caller placed on ctx, simulating a resolver that derives identity
			// from request-scoped data rather than a fixed value.
			engine := newSpecsEngineG3(ctx, "Sample", store, WithTenantResolver(perCallerTenantResolver{}))
			ctx.Expect(engine.Start(bg)).To(specs.BeNil())

			entityID := uuid.NewString()
			probe := newTenancyProbeEventSourcedBehavior(entityID)
			ctx.Expect(engine.Entity(bg, probe, WithTenant(tenancy.TenantID("acme")))).To(specs.BeNil())

			// SendCommand's ctx resolves to "globex", a different tenant than the
			// one this entity was spawned under.
			mismatchedCtx := context.WithValue(bg, perCallerTenantKey{}, "globex")
			_, _, err := engine.SendCommand(mismatchedCtx, entityID, &testpb.CreateAccount{AccountBalance: 500}, time.Minute)
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(probe.InvocationCount()).To(specs.BeZero())

			ctx.Expect(engine.Stop(bg)).To(specs.BeNil())
		})
	})
}
