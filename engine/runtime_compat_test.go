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

	"github.com/stretchr/testify/require"

	runtimeport "github.com/getsyntegrity/ego/port/runtime"
	"github.com/getsyntegrity/ego/tenancy"
)

// TestRuntimeSentinelsAreTheSameValues checks, for each of the ten sentinels
// that moved to port/runtime (ego-runtime-001 design §D2), that the ego name
// and the port/runtime name are the same error value, and that errors.Is
// matches a wrapped error in both directions.
func TestRuntimeSentinelsAreTheSameValues(t *testing.T) {
	cases := []struct {
		name    string
		ego     error
		runtime error
	}{
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
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NotNil(t, tc.ego)
			require.True(t, tc.ego == tc.runtime, "ego.%s and runtime.%s must be the same value", tc.name, tc.name) //nolint:errorlint // identity is the point

			require.ErrorIs(t, fmt.Errorf("op: %w", tc.ego), tc.runtime)
			require.ErrorIs(t, fmt.Errorf("op: %w", tc.runtime), tc.ego)
			require.ErrorIs(t, fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", tc.runtime)), tc.ego)
		})
	}
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
	for _, p := range pairs {
		require.Equal(t, reflect.TypeOf(p.runtime), reflect.TypeOf(p.ego))
	}

	var info *runtimeport.SagaInfo = &SagaInfo{ID: "s", Status: SagaCompleted}
	require.Equal(t, runtimeport.SagaCompleted, info.Status)

	var opt runtimeport.SpawnOption = WithPlacement(Local)
	var egoOpt SpawnOption = opt
	require.Equal(t, runtimeport.Local, runtimeport.ResolveSpawnOptions(egoOpt).Placement())

	require.Equal(t, runtimeport.RoundRobin, RoundRobin)
	require.Equal(t, runtimeport.Random, Random)
	require.Equal(t, runtimeport.Local, Local)
	require.Equal(t, runtimeport.LeastLoad, LeastLoad)
	require.Equal(t, runtimeport.StopDirective, StopDirective)
	require.Equal(t, runtimeport.RestartDirective, RestartDirective)
	require.Equal(t, runtimeport.SagaRunning, SagaRunning)
	require.Equal(t, runtimeport.SagaCompleted, SagaCompleted)
	require.Equal(t, runtimeport.SagaCompensating, SagaCompensating)
	require.Equal(t, runtimeport.SagaFailed, SagaFailed)
	require.Equal(t, "compensating", SagaCompensating.String())
}

// TestEgoSpawnOptionsResolveThroughRuntime checks that every engine.With* spawn
// option is readable by another runtime through ResolveSpawnOptions: the five
// neutral ones through their getters, the four write-side ones as adapter
// settings under ego's own keys.
func TestEgoSpawnOptionsResolveThroughRuntime(t *testing.T) {
	policy := RetentionPolicy{DeleteEventsOnSnapshot: true, EventsRetentionCount: 7}
	s := runtimeport.ResolveSpawnOptions(
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
	require.Equal(t, 2*time.Second, s.PassivateAfter())
	require.True(t, s.Relocation())
	require.Equal(t, StopDirective, s.SupervisorDirective())
	require.Equal(t, LeastLoad, s.Placement())
	require.Equal(t, tenancy.TenantID("acme"), s.Tenant())

	v, ok := s.AdapterSetting(snapshotIntervalKey{})
	require.True(t, ok)
	require.Equal(t, uint64(5), v)
	v, ok = s.AdapterSetting(retentionPolicyKey{})
	require.True(t, ok)
	require.Equal(t, policy, v)
	v, ok = s.AdapterSetting(batchThresholdKey{})
	require.True(t, ok)
	require.Equal(t, 9, v)
	v, ok = s.AdapterSetting(batchFlushWindowKey{})
	require.True(t, ok)
	require.Equal(t, 3*time.Millisecond, v)
}

// TestNewSpawnConfigRoundTrip checks that the GoAkt adapter's private config
// receives every option, including the write-side ones carried as adapter
// settings.
func TestNewSpawnConfigRoundTrip(t *testing.T) {
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
	require.Equal(t, &spawnConfig{
		passivateAfter:      time.Minute,
		toRelocate:          true,
		supervisorDirective: StopDirective,
		entitiesPlacement:   Random,
		snapshotInterval:    4,
		retentionPolicy:     &policy,
		batchThreshold:      8,
		batchFlushWindow:    time.Millisecond,
		tenantID:            tenancy.TenantID("t1"),
	}, config)
}

// TestNewSpawnConfigSkipsNilOption records the one behavior change of S4-2:
// a nil SpawnOption used to panic in newSpawnConfig and is now skipped.
func TestNewSpawnConfigSkipsNilOption(t *testing.T) {
	require.NotPanics(t, func() {
		config := newSpawnConfig(nil, WithBatchThreshold(2))
		require.Equal(t, 2, config.batchThreshold)
	})
}
