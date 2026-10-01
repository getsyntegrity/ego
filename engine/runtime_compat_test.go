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
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"

	runtimeport "github.com/getsyntegrity/ego/port/runtime"
	"github.com/getsyntegrity/ego/tenancy"
)

// TestRuntimeSentinelsAreTheSameValues checks, for each of the ten sentinels
// that moved to port/runtime (ego-runtime-001 design §D2), that the ego name
// and the port/runtime name are the same error value, and that errors.Is
// matches a wrapped error in both directions.
func TestRuntimeSentinelsAreTheSameValues(t *testing.T) {
	type sentinel struct {
		name    string
		ego     error
		runtime error
	}
	specs.Describe(t, "a sentinel that moved to port/runtime", func(s *specs.Spec) {
		specs.Table(s, []sentinel{
			{"ErrEngineNotStarted", ErrEngineNotStarted, runtimeport.ErrEngineNotStarted},
			{"ErrUndefinedEntityID", ErrUndefinedEntityID, runtimeport.ErrUndefinedEntityID},
			{"ErrDurableStateStoreRequired", ErrDurableStateStoreRequired, runtimeport.ErrDurableStateStoreRequired},
			{"ErrEventsStoreRequired", ErrEventsStoreRequired, runtimeport.ErrEventsStoreRequired},
			{"ErrProjectionNotRegistered", ErrProjectionNotRegistered, runtimeport.ErrProjectionNotRegistered},
			{"ErrSpawnTenantUndetermined", ErrSpawnTenantUndetermined, runtimeport.ErrSpawnTenantUndetermined},
			{"ErrSpawnTenantMismatch", ErrSpawnTenantMismatch, runtimeport.ErrSpawnTenantMismatch},
			{"ErrSpawnTenantUnverified", ErrSpawnTenantUnverified, runtimeport.ErrSpawnTenantUnverified},
			{"ErrNotACommand", ErrNotACommand, runtimeport.ErrNotACommand},
			{"ErrEntityFamilyNotDeclared", ErrEntityFamilyNotDeclared, runtimeport.ErrEntityFamilyNotDeclared},
		}, func(c sentinel) string { return c.name }, func(ctx *specs.Context, c sentinel) {
			ctx.Expect(c.ego).To(specs.Not(specs.BeNil()))
			// identity is the point: errors.Is would also accept a wrapper
			ctx.Expect(c.ego).To(specs.Satisfy("the identical error value as "+c.name+" in port/runtime",
				func(got any) bool { return got == any(c.runtime) }))

			ctx.Expect(fmt.Errorf("op: %w", c.ego)).To(specs.MatchError(c.runtime))
			ctx.Expect(fmt.Errorf("op: %w", c.runtime)).To(specs.MatchError(c.ego))
			ctx.Expect(fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", c.runtime))).To(specs.MatchError(c.ego))
		})
	})
}

// TestRuntimeMovedTypesAreAliases checks that each type that moved to
// port/runtime is the same type under both names: a value of the ego name is
// a value of the port/runtime name.
func TestRuntimeMovedTypesAreAliases(t *testing.T) {
	type pair struct {
		ego     any
		runtime any
	}
	type constant struct {
		name    string
		ego     any
		runtime any
	}
	specs.Describe(t, "a type or constant that moved to port/runtime", func(s *specs.Spec) {
		specs.Table(s, []pair{
			{EntitiesPlacement(0), runtimeport.EntitiesPlacement(0)},
			{SupervisorDirective(0), runtimeport.SupervisorDirective(0)},
			{SagaStatus(0), runtimeport.SagaStatus(0)},
			{SagaInfo{}, runtimeport.SagaInfo{}},
			{(*SpawnOption)(nil), (*runtimeport.SpawnOption)(nil)},
		}, func(p pair) string { return fmt.Sprintf("%T is one type under both names", p.runtime) },
			func(ctx *specs.Context, p pair) {
				ctx.Expect(reflect.TypeOf(p.ego)).To(specs.Equal(reflect.TypeOf(p.runtime)))
			})

		s.It("accepts a value of the ego name where the port/runtime name is expected", func(ctx *specs.Context) {
			var _ *runtimeport.SagaInfo = &SagaInfo{ID: "s", Status: SagaCompleted} //nolint:staticcheck // compile-time alias assertion: the explicit type is the point
			info := &SagaInfo{ID: "s", Status: SagaCompleted}
			ctx.Expect(info.Status).To(specs.Equal(runtimeport.SagaCompleted))

			opt := WithPlacement(Local)
			var _ runtimeport.SpawnOption = opt //nolint:staticcheck // compile-time alias assertion: the explicit type is the point
			var egoOpt SpawnOption = opt        //nolint:staticcheck // compile-time alias assertion: the explicit type is the point
			ctx.Expect(runtimeport.ResolveSpawnOptions(egoOpt).Placement()).To(specs.Equal(runtimeport.Local))
		})

		specs.Table(s, []constant{
			{"RoundRobin", RoundRobin, runtimeport.RoundRobin},
			{"Random", Random, runtimeport.Random},
			{"Local", Local, runtimeport.Local},
			{"LeastLoad", LeastLoad, runtimeport.LeastLoad},
			{"StopDirective", StopDirective, runtimeport.StopDirective},
			{"RestartDirective", RestartDirective, runtimeport.RestartDirective},
			{"SagaRunning", SagaRunning, runtimeport.SagaRunning},
			{"SagaCompleted", SagaCompleted, runtimeport.SagaCompleted},
			{"SagaCompensating", SagaCompensating, runtimeport.SagaCompensating},
			{"SagaFailed", SagaFailed, runtimeport.SagaFailed},
		}, func(c constant) string { return c.name + " has the same value under both names" },
			func(ctx *specs.Context, c constant) {
				ctx.Expect(c.ego).To(specs.Equal(c.runtime))
			})

		s.It("keeps the String form of a moved status", func(ctx *specs.Context) {
			ctx.Expect(SagaCompensating.String()).To(specs.Equal("compensating"))
		})
	})
}

// TestEgoSpawnOptionsResolveThroughRuntime checks that every engine.With* spawn
// option is readable by another runtime through ResolveSpawnOptions: the five
// neutral ones through their getters, the four write-side ones as adapter
// settings under ego's own keys.
func TestEgoSpawnOptionsResolveThroughRuntime(t *testing.T) {
	specs.Describe(t, "ResolveSpawnOptions over the engine spawn options", func(s *specs.Spec) {
		policy := RetentionPolicy{DeleteEventsOnSnapshot: true, EventsRetentionCount: 7}
		resolve := func() runtimeport.SpawnSettings {
			return runtimeport.ResolveSpawnOptions(
				WithPassivateAfter(2*time.Second),
				WithRelocation(true),
				WithSupervisorDirective(StopDirective),
				WithPlacement(LeastLoad),
				WithTenant(tenancy.TenantID("acme")),
				WithSnapshotInterval(5),
				WithRetentionPolicy(policy),
				WithBatchThreshold(9),
				WithBatchFlushWindow(3*time.Millisecond),
			)
		}

		s.It("exposes the five neutral options through their getters", func(ctx *specs.Context) {
			settings := resolve()
			ctx.Expect(settings.PassivateAfter()).To(specs.Equal(2 * time.Second))
			ctx.Expect(settings.Relocation()).To(specs.BeTrue())
			ctx.Expect(settings.SupervisorDirective()).To(specs.Equal(StopDirective))
			ctx.Expect(settings.Placement()).To(specs.Equal(LeastLoad))
			ctx.Expect(settings.Tenant()).To(specs.Equal(tenancy.TenantID("acme")))
		})

		type adapterSetting struct {
			name string
			key  any
			want any
		}
		specs.Table(s, []adapterSetting{
			{"snapshot interval", snapshotIntervalKey{}, uint64(5)},
			{"retention policy", retentionPolicyKey{}, policy},
			{"batch threshold", batchThresholdKey{}, 9},
			{"batch flush window", batchFlushWindowKey{}, 3 * time.Millisecond},
		}, func(c adapterSetting) string { return "carries the " + c.name + " as an adapter setting" },
			func(ctx *specs.Context, c adapterSetting) {
				v, ok := resolve().AdapterSetting(c.key)
				ctx.Expect(ok).To(specs.BeTrue())
				ctx.Expect(v).To(specs.Equal(c.want))
			})
	})
}

// TestNewSpawnConfigRoundTrip checks that the GoAkt adapter's private config
// receives every option, including the write-side ones carried as adapter
// settings.
func TestNewSpawnConfigRoundTrip(t *testing.T) {
	specs.Describe(t, "newSpawnConfig over every spawn option", func(s *specs.Spec) {
		s.It("hands each option to the private config", func(ctx *specs.Context) {
			policy := RetentionPolicy{DeleteSnapshotsOnSnapshot: true, EventsRetentionCount: 3}
			config := newSpawnConfig(
				WithPassivateAfter(time.Minute),
				WithRelocation(true),
				WithSupervisorDirective(StopDirective),
				WithPlacement(Random),
				WithTenant(tenancy.TenantID("t1")),
				WithSnapshotInterval(4),
				WithRetentionPolicy(policy),
				WithBatchThreshold(8),
				WithBatchFlushWindow(time.Millisecond),
			)
			ctx.Expect(config).To(specs.Equal(&spawnConfig{
				passivateAfter:      time.Minute,
				toRelocate:          true,
				supervisorDirective: StopDirective,
				entitiesPlacement:   Random,
				snapshotInterval:    4,
				retentionPolicy:     &policy,
				batchThreshold:      8,
				batchFlushWindow:    time.Millisecond,
				tenantID:            tenancy.TenantID("t1"),
			}))
		})
	})
}

// TestNewSpawnConfigSkipsNilOption records the one behavior change of S4-2:
// a nil SpawnOption used to panic in newSpawnConfig and is now skipped.
func TestNewSpawnConfigSkipsNilOption(t *testing.T) {
	specs.Describe(t, "newSpawnConfig given a nil SpawnOption", func(s *specs.Spec) {
		s.It("skips it and applies the others", func(ctx *specs.Context) {
			var config *spawnConfig
			ctx.Expect(panicValue(func() { config = newSpawnConfig(nil, WithBatchThreshold(2)) })).To(specs.BeNil())
			ctx.Expect(config.batchThreshold).To(specs.Equal(2))
		})
	})
}
