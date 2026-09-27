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
	"reflect"
	"time"

	"github.com/pablogore/ego/v4/tenancy"
)

// EntitiesPlacement defines the algorithm used by the entity system to determine
// where an entity should be spawned in a clustered environment.
//
// This strategy is only relevant when cluster mode is enabled.
// It affects how entities are distributed across the nodes in the cluster.
type EntitiesPlacement int

const (
	// RoundRobin distributes entities evenly across nodes
	// by cycling through the available nodes in a round-robin manner.
	// This strategy provides balanced load distribution over time.
	// ⚠️ Note: This strategy is subject to the cluster topology at the time of creation. For a stable cluster topology,
	// it ensures an even distribution of entities across all nodes.
	RoundRobin EntitiesPlacement = iota

	// Random selects a node at random from the available pool of nodes.
	// This strategy is stateless and can help quickly spread entities across the cluster,
	// but may result in uneven load distribution.
	Random

	// Local forces the entity to be spawned on the local node,
	// regardless of the cluster configuration.
	// Useful when locality is important (e.g., accessing local resources).
	Local

	// LeastLoad selects the node with the least current load to spawn the entity.
	// This strategy aims to optimize resource utilization by placing entities
	// on nodes that are less busy, potentially improving performance and responsiveness.
	// Note: This strategy may require additional overhead when placing entities,
	// as it needs to get nodes load metrics depending on the cluster size.
	LeastLoad
)

// SupervisorDirective defines the supervisor directive
//
// It represents the action that a supervisor can take when an entity fails or panics
// during message processing. Each directive corresponds to a specific recovery behavior:
//
//   - StopDirective: Instructs the supervisor to stop the failing entity
//     (typically used when the failure is irrecoverable).
//   - RestartDirective: Instructs the supervisor to restart the failing entity, reinitializing its state.
type SupervisorDirective int

const (
	// StopDirective indicates that when an entity fails, the supervisor should immediately stop
	// the entity. This directive is typically used when a failure is deemed irrecoverable
	// or when the entity's state cannot be safely resumed.
	StopDirective SupervisorDirective = iota

	// RestartDirective indicates that when an entity fails, the supervisor should restart the entity.
	// Restarting involves stopping the current instance and creating a new one, effectively resetting
	// the entity's internal state.
	RestartDirective
)

// spawnConfig is unexported so that SpawnOption stays sealed to this
// package: options are built only by the constructors below.
type spawnConfig struct {
	// passivateAfter is the idle time after which an entity is passivated;
	// 0 disables passivation.
	passivateAfter time.Duration
	// relocation reports whether the entity may be relocated to another node
	// when its node leaves the cluster.
	relocation bool
	// supervisorDirective is applied when the entity fails.
	supervisorDirective SupervisorDirective
	// placement is the cluster placement strategy.
	placement EntitiesPlacement
	// tenantID is the tenant the spawned entity belongs to; empty when not
	// declared.
	tenantID tenancy.TenantID
	// adapter holds adapter settings by key (WithAdapterSetting). It is nil
	// until the first adapter setting is applied.
	adapter map[any]any
}

// SpawnOption configures one spawn. Build it with the With* functions of this
// package, or of a runtime adapter for adapter settings (WithAdapterSetting).
// Its method takes an unexported type, so no other package can implement it;
// a runtime reads the options it receives with ResolveSpawnOptions.
type SpawnOption interface {
	// Apply sets the Option value of a config.
	Apply(config *spawnConfig)
}

// ensures that the interface is fully implemented
var _ SpawnOption = spawnOption(nil)

// spawnOption implements the SpawnOption interface.
type spawnOption func(config *spawnConfig)

// Apply sets the Option value of a config.
func (f spawnOption) Apply(c *spawnConfig) {
	f(c)
}

// SpawnSettings is the resolved, read-only result of a list of spawn options.
// Runtimes read it; nothing can change it after ResolveSpawnOptions. It holds
// a map of adapter settings, so it is not comparable with ==; compare the
// getters instead.
type SpawnSettings struct {
	c spawnConfig
}

// ResolveSpawnOptions applies opts in order over the defaults
// (RestartDirective, RoundRobin, no passivation, relocation disabled, no
// tenant) and returns the result; a later option overrides an earlier one. A
// nil option is skipped. A non-nil value that embeds a nil SpawnOption (for
// example struct{ SpawnOption }{}) is not nil, so its Apply panics.
func ResolveSpawnOptions(opts ...SpawnOption) SpawnSettings {
	c := spawnConfig{
		supervisorDirective: RestartDirective,
		placement:           RoundRobin,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt.Apply(&c)
	}
	return SpawnSettings{c: c}
}

// PassivateAfter returns the idle time after which the entity is passivated.
// 0 means no passivation.
func (s SpawnSettings) PassivateAfter() time.Duration { return s.c.passivateAfter }

// Relocation reports whether the entity may be relocated to another node when
// its node leaves the cluster. It is false unless WithRelocation(true) was
// passed.
func (s SpawnSettings) Relocation() bool { return s.c.relocation }

// SupervisorDirective returns the directive applied when the entity fails.
// The default is RestartDirective.
func (s SpawnSettings) SupervisorDirective() SupervisorDirective { return s.c.supervisorDirective }

// Placement returns the cluster placement strategy. The default is RoundRobin.
func (s SpawnSettings) Placement() EntitiesPlacement { return s.c.placement }

// Tenant returns the tenant declared with WithTenant, or "" when none was
// declared.
func (s SpawnSettings) Tenant() tenancy.TenantID { return s.c.tenantID }

// AdapterSetting returns the value set with WithAdapterSetting under key, and
// whether one was set. A nil or non-comparable key finds nothing.
func (s SpawnSettings) AdapterSetting(key any) (any, bool) {
	if !isComparableKey(key) {
		return nil, false
	}
	value, ok := s.c.adapter[key]
	return value, ok
}

// WithPassivateAfter sets a custom duration after which an idle entity
// will be passivated. Passivation allows the entity system to free up
// resources by stopping entities that have been inactive for the specified
// duration. If the entity receives a message before this timeout,
// the passivation timer is reset.
func WithPassivateAfter(after time.Duration) SpawnOption {
	return spawnOption(func(config *spawnConfig) {
		config.passivateAfter = after
	})
}

// WithRelocation controls whether an entity should be relocated to another node in the cluster
// when its hosting node shuts down unexpectedly.
//
// Relocation is disabled unless WithRelocation(true) is passed. When it is
// enabled, the entity is redeployed on a healthy node if the original node
// becomes unavailable, which suits entities that can resume without
// node-specific context. RUNTIME-003 owns the contract of this setting,
// including whether its default should change (#154).
//
// Parameters:
//   - toRelocate: If true, the entity is eligible for relocation on node failure.
//     If false, the entity will not be redeployed after a node shutdown.
//
// Returns: SpawnOption: A functional option that updates the entity's relocation configuration.
func WithRelocation(toRelocate bool) SpawnOption {
	return spawnOption(func(config *spawnConfig) {
		config.relocation = toRelocate
	})
}

// WithSupervisorDirective sets the SupervisorDirective that will be applied to the entity
// when it fails. This controls how the entities' failures are handled (e.g. restart, stop,
// escalate). If not provided, the default is RestartDirective.
// Use this to override the default supervision strategy for specific entities.
func WithSupervisorDirective(directive SupervisorDirective) SpawnOption {
	return spawnOption(func(config *spawnConfig) {
		config.supervisorDirective = directive
	})
}

// WithPlacement returns a SpawnOption that sets the placement strategy to be
// used when spawning an entity in cluster mode.
//
// This option determines how the runtime selects a target node for spawning
// the entity across the cluster. Valid strategies include RoundRobin, Random,
// Local and LeastLoad. The default is RoundRobin.
func WithPlacement(placement EntitiesPlacement) SpawnOption {
	return spawnOption(func(config *spawnConfig) {
		config.placement = placement
	})
}

// WithTenant declares the tenant identity that an entity, durable-state
// entity, or saga belongs to, for the single spawn call it is passed to
// (TENANT-003 T4). The application, which knows the tenant at the trust
// boundary, states it here; a runtime never asks the tenancy.TenantResolver at
// spawn (Resolve-Once, Propagate-After, openspec/specs/tenancy-core/spec.md).
// A runtime consults it only when tenancy is active; see ego.WithTenant for
// the GoAkt adapter's rules.
func WithTenant(id tenancy.TenantID) SpawnOption {
	return spawnOption(func(config *spawnConfig) {
		config.tenantID = id
	})
}

// WithAdapterSetting carries a value that only one runtime adapter reads,
// under a key only that adapter can name (an unexported type, as with
// context.WithValue). Other runtimes ignore it. A runtime reads it back with
// SpawnSettings.AdapterSetting; a later setting under the same key replaces
// an earlier one.
//
// Like context.WithValue, it panics when it is called, not later at spawn, if
// key is nil or its type is not comparable, so a bad key fails where the
// option is built.
func WithAdapterSetting(key, value any) SpawnOption {
	if key == nil {
		panic("eGo: runtime.WithAdapterSetting: nil key")
	}
	if !isComparableKey(key) {
		panic("eGo: runtime.WithAdapterSetting: key of type " + reflect.TypeOf(key).String() + " is not comparable")
	}
	return spawnOption(func(config *spawnConfig) {
		if config.adapter == nil {
			config.adapter = make(map[any]any)
		}
		config.adapter[key] = value
	})
}

// isComparableKey reports whether key is non-nil and of a comparable type,
// the same test context.WithValue applies. A key of a comparable type can
// still panic as a map key when its dynamic value is not comparable (an
// interface field holding a slice, for example); like context.WithValue, the
// check is on the type only.
func isComparableKey(key any) bool {
	return key != nil && reflect.TypeOf(key).Comparable()
}
