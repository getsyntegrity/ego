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
	"time"

	runtimeport "github.com/getsyntegrity/ego/port/runtime"
	"github.com/getsyntegrity/ego/tenancy"
)

// EntitiesPlacement defines the algorithm used by the entity system to determine
// where an entity should be spawned in a clustered environment. It is an alias
// of [runtimeport.EntitiesPlacement], so engine.EntitiesPlacement and
// runtime.EntitiesPlacement are the same type.
type EntitiesPlacement = runtimeport.EntitiesPlacement

// The placement strategies, as constants of the same type and value as their
// port/runtime counterparts.
const (
	// RoundRobin is [runtimeport.RoundRobin].
	RoundRobin = runtimeport.RoundRobin
	// Random is [runtimeport.Random].
	Random = runtimeport.Random
	// Local is [runtimeport.Local].
	Local = runtimeport.Local
	// LeastLoad is [runtimeport.LeastLoad].
	LeastLoad = runtimeport.LeastLoad
)

// spawnConfig defines the spawn config
type spawnConfig struct {
	// passivateAfter sets the passivation time
	// when this value is not defined then passivation is disabled
	passivateAfter time.Duration
	// specifies if the entity should be relocated
	toRelocate bool
	// supervisorDirective sets the supervisor directive to use
	// when the given entity fails
	supervisorDirective SupervisorDirective
	// specifies the placement strategy to use
	entitiesPlacement EntitiesPlacement
	// snapshotInterval defines how often resulting state is stored alongside events.
	// A value of 0 or 1 means every event carries a snapshot (default behavior).
	// A value of N > 1 means only every Nth event carries a resulting state snapshot.
	snapshotInterval uint64
	// retentionPolicy controls cleanup of old events and snapshots after a snapshot write.
	retentionPolicy *RetentionPolicy
	// batchThreshold sets the number of events to accumulate before flushing
	// them to the store in a single write. A value of 0 disables batching.
	batchThreshold int
	// batchFlushWindow sets the maximum time to wait before flushing a partial
	// batch. When the window expires, pending events are flushed regardless of
	// how many events have been accumulated.
	batchFlushWindow time.Duration
	// tenantID is the tenant this entity/durable-state entity/saga belongs
	// to, set via WithTenant. It is only consulted in tenant-aware mode
	// (engine.tenantResolver != nil); the empty value means "not declared",
	// in which case the engine falls back to the registered resolver's
	// tenancy.FixedTenantResolver capability, and fails closed if neither
	// source yields a tenant (TENANT-003 T4).
	tenantID tenancy.TenantID
}

// Keys of the write-side spawn settings. They are unexported types of package
// ego, so only the GoAkt adapter can name them: these settings travel through
// runtimeport.WithAdapterSetting and every other runtime ignores them. Whether
// they belong in the runtime contract is #12's decision
// (openspec/changes/ego-runtime-001/design.md §D3).
type (
	snapshotIntervalKey struct{}
	retentionPolicyKey  struct{}
	batchThresholdKey   struct{}
	batchFlushWindowKey struct{}
)

// newSpawnConfig resolves opts with runtimeport.ResolveSpawnOptions and builds
// the GoAkt adapter's private spawnConfig from the result: the five neutral
// settings from its getters, the four write-side settings from ego's adapter
// settings. A nil option is skipped.
func newSpawnConfig(opts ...SpawnOption) *spawnConfig {
	settings := runtimeport.ResolveSpawnOptions(opts...)
	config := &spawnConfig{
		passivateAfter:      settings.PassivateAfter(),
		toRelocate:          settings.Relocation(),
		supervisorDirective: settings.SupervisorDirective(),
		entitiesPlacement:   settings.Placement(),
		tenantID:            settings.Tenant(),
	}
	if v, ok := settings.AdapterSetting(snapshotIntervalKey{}); ok {
		config.snapshotInterval = v.(uint64)
	}
	if v, ok := settings.AdapterSetting(retentionPolicyKey{}); ok {
		policy := v.(RetentionPolicy)
		config.retentionPolicy = &policy
	}
	if v, ok := settings.AdapterSetting(batchThresholdKey{}); ok {
		config.batchThreshold = v.(int)
	}
	if v, ok := settings.AdapterSetting(batchFlushWindowKey{}); ok {
		config.batchFlushWindow = v.(time.Duration)
	}
	return config
}

// SpawnOption configures one spawn. It is an alias of
// [runtimeport.SpawnOption], so engine.SpawnOption and runtime.SpawnOption are
// the same type: an option built by either package's With* functions is
// accepted by every spawn method, and another runtime reads it with
// [runtimeport.ResolveSpawnOptions].
type SpawnOption = runtimeport.SpawnOption

// WithPassivateAfter sets a custom duration after which an idle entity
// will be passivated. Passivation allows the entity system to free up
// resources by stopping entities that have been inactive for the specified
// duration. If the entity receives a message before this timeout,
// the passivation timer is reset. It returns [runtimeport.WithPassivateAfter].
func WithPassivateAfter(after time.Duration) SpawnOption {
	return runtimeport.WithPassivateAfter(after)
}

// WithRelocation controls whether an entity should be relocated to another
// node in the cluster when its hosting node shuts down unexpectedly.
//
// In cluster mode, entities are NOT relocated by default: WithRelocation(false)
// is the default, and relocation stays disabled unless WithRelocation(true) is
// passed. WithRelocation(true) makes the entity eligible for relocation to a
// healthy node when its host node goes down. It returns
// [runtimeport.WithRelocation], whose documentation states the same contract.
func WithRelocation(toRelocate bool) SpawnOption {
	return runtimeport.WithRelocation(toRelocate)
}

// WithSupervisorDirective sets the SupervisorDirective that will be applied to the entity
// when it fails. This controls how the entities' failures are handled (e.g. restart, stop,
// escalate). If not provided, the default is RestartDirective.
// Use this to override the default supervision strategy for specific entities.
// It returns [runtimeport.WithSupervisorDirective].
func WithSupervisorDirective(directive SupervisorDirective) SpawnOption {
	return runtimeport.WithSupervisorDirective(directive)
}

// WithSnapshotInterval sets how often the resulting state is persisted alongside events.
// A value of 0 disables automatic snapshots. A value of 1 means every event
// carries a snapshot. A value of N > 1 means only every Nth event carries the
// resulting state. During recovery, the entity replays events since the last
// snapshot point.
//
// This reduces storage cost for entities with large state and frequent events.
// It is a GoAkt adapter setting ([runtimeport.WithAdapterSetting]); other
// runtimes ignore it.
func WithSnapshotInterval(every uint64) SpawnOption {
	return runtimeport.WithAdapterSetting(snapshotIntervalKey{}, every)
}

// WithRetentionPolicy sets the retention policy that controls cleanup of old events
// and snapshots after a snapshot has been successfully written. This requires a
// snapshot store and a snapshot interval to be configured.
// It is a GoAkt adapter setting ([runtimeport.WithAdapterSetting]); other
// runtimes ignore it.
func WithRetentionPolicy(policy RetentionPolicy) SpawnOption {
	return runtimeport.WithAdapterSetting(retentionPolicyKey{}, policy)
}

// WithPlacement returns a SpawnOption that sets the placement strategy to be used when spawning an entity
// in cluster mode via the SpawnOn function.
//
// This option determines how the entity system selects a target node for spawning
// the entity across the cluster. Valid strategies include RoundRobin, Random, and Local.
//
// Parameters:
//   - placement: A EntitiesPlacement value specifying how to distribute the entity.
//
// Returns:
//   - SpawnOption that sets the placement strategy in the spawn configuration
//     ([runtimeport.WithPlacement]).
func WithPlacement(placement EntitiesPlacement) SpawnOption {
	return runtimeport.WithPlacement(placement)
}

// WithBatchThreshold sets the number of events to accumulate before flushing
// them to the store in a single write. This amortizes the cost of individual
// store round-trips across multiple commands by batching their resulting events.
//
// When batching is enabled the actor processes commands optimistically against
// pending (unconfirmed) state. Incoming commands received while a batch write
// is in flight are stashed and replayed after the write completes.
//
// A threshold of 0 (the default) disables batching: each command triggers its
// own synchronous write.
//
// Batching should be combined with [WithBatchFlushWindow] to bound the maximum
// latency for partially filled batches.
//
// It is a GoAkt adapter setting ([runtimeport.WithAdapterSetting]); other
// runtimes ignore it.
func WithBatchThreshold(threshold int) SpawnOption {
	return runtimeport.WithAdapterSetting(batchThresholdKey{}, threshold)
}

// WithBatchFlushWindow sets the maximum duration the actor will wait before
// flushing accumulated events when the batch threshold has not been reached.
// This bounds the worst-case latency added by batching.
//
// When the window expires, any pending events are flushed immediately,
// regardless of how many commands have been accumulated.
//
// If not specified and batching is enabled, a default window of 5ms is used.
//
// It is a GoAkt adapter setting ([runtimeport.WithAdapterSetting]); other
// runtimes ignore it.
func WithBatchFlushWindow(window time.Duration) SpawnOption {
	return runtimeport.WithAdapterSetting(batchFlushWindowKey{}, window)
}

// WithTenant declares the tenant identity that an entity, durable-state
// entity, or saga belongs to, for the single spawn call it is passed to
// (TENANT-003 T4).
//
// This is the application's side of the Resolve-Once, Propagate-After
// discipline (openspec/specs/tenancy-core/spec.md): a tenancy.TenantResolver
// MUST be invoked exactly once, at the command trust boundary, never at
// spawn — so the engine cannot ask the resolver "which tenant is this
// entity for" the way an earlier, CI-caught design mistakenly did. The
// application is the one that knows which tenant a given entity/durable
// state/saga belongs to (e.g. it just read the tenant off an authenticated
// request that is now creating that entity), so it states it explicitly
// with WithTenant instead.
//
// WithTenant is only consulted when tenancy is active (a
// tenancy.TenantResolver is registered via engine.WithTenantResolver); it is
// silently ignored in legacy mode. In tenant-aware mode, a spawn fails
// closed with ErrSpawnTenantUndetermined when WithTenant was not passed AND
// the registered resolver does not expose a fixed tenant via
// tenancy.FixedTenantResolver — see WithSingleTenant's doc comment for the
// one built-in resolver that does, which is what lets single-tenant
// deployments omit WithTenant entirely. It returns [runtimeport.WithTenant].
func WithTenant(id tenancy.TenantID) SpawnOption {
	return runtimeport.WithTenant(id)
}
