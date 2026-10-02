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

	runtimeport "github.com/getsyntegrity/urd/port/runtime"
	"github.com/getsyntegrity/urd/tenancy"
)

// optRecovered runs fn and returns the value it panicked with, or nil.
func optRecovered(fn func()) (value any) {
	defer func() { value = recover() }()
	fn()
	return nil
}

// TestRuntimeSentinelsAreTheSameValues checks, for each of the ten sentinels
// that moved to port/runtime (ego-runtime-001 design §D2), that the ego name
// and the port/runtime name are the same error value, and that errors.Is
// matches a wrapped error in both directions.
func TestRuntimeSentinelsAreTheSameValues(t *testing.T) {
	type sentinelCase struct {
		name    string
		ego     error
		runtime error
	}
	cases := []sentinelCase{
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
	}
	specs.Describe(t, "each moved sentinel is the same error value under the ego and port/runtime names", func(s *specs.Spec) {
		specs.Table(s, cases, func(tc sentinelCase) string { return tc.name }, func(ctx *specs.Context, tc sentinelCase) {
			ctx.Expect(tc.ego).To(specs.Not(specs.BeNil()))
			ctx.Expect(tc.ego).To(beTheSame(tc.runtime))

			ctx.Expect(fmt.Errorf("op: %w", tc.ego)).To(specs.MatchError(tc.runtime))
			ctx.Expect(fmt.Errorf("op: %w", tc.runtime)).To(specs.MatchError(tc.ego))
			ctx.Expect(fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", tc.runtime))).To(specs.MatchError(tc.ego))
		})
	})
}

// TestRuntimeMovedTypesAreAliases checks that each type that moved to
// port/runtime is the same type under both names: a value of the ego name is
// a value of the port/runtime name.
func TestRuntimeMovedTypesAreAliases(t *testing.T) {
	pairs := []struct {
		ego     any
		runtime any
	}{
		{EntitiesPlacement(0), runtimeport.EntitiesPlacement(0)},
		{SupervisorDirective(0), runtimeport.SupervisorDirective(0)},
		{SagaStatus(0), runtimeport.SagaStatus(0)},
		{SagaInfo{}, runtimeport.SagaInfo{}},
		{(*SpawnOption)(nil), (*runtimeport.SpawnOption)(nil)},
	}
	specs.Describe(t, "the types and constants that moved to port/runtime are aliases of the ego names", func(s *specs.Spec) {
		s.It("aliases the moved types, constants and values", func(ctx *specs.Context) {
			var egoTypes, runtimeTypes []reflect.Type
			for _, p := range pairs {
				egoTypes = append(egoTypes, reflect.TypeOf(p.ego))
				runtimeTypes = append(runtimeTypes, reflect.TypeOf(p.runtime))
			}
			ctx.Expect(egoTypes).To(specs.HaveLen(len(pairs)))
			ctx.Expect(egoTypes).ToEqual(runtimeTypes)

			var _ *runtimeport.SagaInfo = &SagaInfo{ID: "s", Status: SagaCompleted} //nolint:staticcheck // compile-time alias assertion: the explicit type is the point
			info := &SagaInfo{ID: "s", Status: SagaCompleted}
			ctx.Expect(info.Status).ToEqual(runtimeport.SagaCompleted)

			opt := WithPlacement(Local)
			var _ runtimeport.SpawnOption = opt //nolint:staticcheck // compile-time alias assertion: the explicit type is the point
			var egoOpt SpawnOption = opt        //nolint:staticcheck // compile-time alias assertion: the explicit type is the point
			ctx.Expect(runtimeport.ResolveSpawnOptions(egoOpt).Placement()).ToEqual(runtimeport.Local)

			ctx.Expect(RoundRobin).ToEqual(runtimeport.RoundRobin)
			ctx.Expect(Random).ToEqual(runtimeport.Random)
			ctx.Expect(Local).ToEqual(runtimeport.Local)
			ctx.Expect(LeastLoad).ToEqual(runtimeport.LeastLoad)
			ctx.Expect(StopDirective).ToEqual(runtimeport.StopDirective)
			ctx.Expect(RestartDirective).ToEqual(runtimeport.RestartDirective)
			ctx.Expect(SagaRunning).ToEqual(runtimeport.SagaRunning)
			ctx.Expect(SagaCompleted).ToEqual(runtimeport.SagaCompleted)
			ctx.Expect(SagaCompensating).ToEqual(runtimeport.SagaCompensating)
			ctx.Expect(SagaFailed).ToEqual(runtimeport.SagaFailed)
			ctx.Expect(SagaCompensating.String()).ToEqual("compensating")
		})
	})
}

// TestEgoSpawnOptionsResolveThroughRuntime checks that every engine.With* spawn
// option is readable by another runtime through ResolveSpawnOptions: the five
// neutral ones through their getters, the four write-side ones as adapter
// settings under ego's own keys.
func TestEgoSpawnOptionsResolveThroughRuntime(t *testing.T) {
	specs.Describe(t, "engine spawn options are readable through the port/runtime ResolveSpawnOptions", func(s *specs.Spec) {
		s.It("exposes the neutral options through getters and the write-side ones as adapter settings", func(ctx *specs.Context) {
			policy := RetentionPolicy{DeleteEventsOnSnapshot: true, EventsRetentionCount: 7}
			resolved := runtimeport.ResolveSpawnOptions(
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
			ctx.Expect(resolved.PassivateAfter()).ToEqual(2 * time.Second)
			ctx.Expect(resolved.Relocation()).To(specs.BeTrue())
			ctx.Expect(resolved.SupervisorDirective()).ToEqual(StopDirective)
			ctx.Expect(resolved.Placement()).ToEqual(LeastLoad)
			ctx.Expect(resolved.Tenant()).ToEqual(tenancy.TenantID("acme"))

			v, ok := resolved.AdapterSetting(snapshotIntervalKey{})
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(v).ToEqual(uint64(5))
			v, ok = resolved.AdapterSetting(retentionPolicyKey{})
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(v).ToEqual(policy)
			v, ok = resolved.AdapterSetting(batchThresholdKey{})
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(v).ToEqual(9)
			v, ok = resolved.AdapterSetting(batchFlushWindowKey{})
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(v).ToEqual(3 * time.Millisecond)
		})
	})
}

// TestNewSpawnConfigRoundTrip checks that the GoAkt adapter's private config
// receives every option, including the write-side ones carried as adapter
// settings.
func TestNewSpawnConfigRoundTrip(t *testing.T) {
	specs.Describe(t, "newSpawnConfig hands the GoAkt adapter every spawn option", func(s *specs.Spec) {
		s.It("carries the neutral and the write-side options into the private config", func(ctx *specs.Context) {
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
			ctx.Expect(config).ToEqual(&spawnConfig{
				passivateAfter:      time.Minute,
				toRelocate:          true,
				supervisorDirective: StopDirective,
				entitiesPlacement:   Random,
				snapshotInterval:    4,
				retentionPolicy:     &policy,
				batchThreshold:      8,
				batchFlushWindow:    time.Millisecond,
				tenantID:            tenancy.TenantID("t1"),
			})
		})
	})
}

// TestNewSpawnConfigSkipsNilOption records the one behavior change of S4-2:
// a nil SpawnOption used to panic in newSpawnConfig and is now skipped.
func TestNewSpawnConfigSkipsNilOption(t *testing.T) {
	specs.Describe(t, "newSpawnConfig skips a nil spawn option", func(s *specs.Spec) {
		s.It("does not panic and still applies the other options", func(ctx *specs.Context) {
			var config *spawnConfig
			panicked := optRecovered(func() {
				config = newSpawnConfig(nil, WithBatchThreshold(2))
			})
			ctx.Expect(panicked).To(specs.BeNil())
			ctx.Expect(config.batchThreshold).ToEqual(2)
		})
	})
}
