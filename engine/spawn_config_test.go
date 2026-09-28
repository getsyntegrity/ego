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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSpawnOption(t *testing.T) {
	t.Run("WithPassivateAfter with options", func(t *testing.T) {
		config := newSpawnConfig(WithPassivateAfter(time.Second))
		require.EqualValues(t, time.Second, config.passivateAfter)
	})
	t.Run("WithPassivateAfter changes only passivateAfter", func(t *testing.T) {
		config := newSpawnConfig(WithPassivateAfter(time.Second))
		require.Equal(t, withDefaults(spawnConfig{passivateAfter: time.Second}), config)
	})
	t.Run("WithRelocation", func(t *testing.T) {
		config := newSpawnConfig(WithRelocation(true))
		require.Equal(t, withDefaults(spawnConfig{toRelocate: true}), config)
	})
	t.Run("WithSupervisorDirective", func(t *testing.T) {
		config := newSpawnConfig(WithSupervisorDirective(StopDirective))
		require.Equal(t, &spawnConfig{supervisorDirective: StopDirective, entitiesPlacement: RoundRobin}, config)
	})
	t.Run("WithEntitiesPlacement", func(t *testing.T) {
		config := newSpawnConfig(WithPlacement(LeastLoad))
		require.Equal(t, &spawnConfig{supervisorDirective: RestartDirective, entitiesPlacement: LeastLoad}, config)
	})
	t.Run("WithSnapshotInterval", func(t *testing.T) {
		config := newSpawnConfig(WithSnapshotInterval(10))
		require.Equal(t, withDefaults(spawnConfig{snapshotInterval: 10}), config)
	})
	t.Run("WithSnapshotInterval zero is default", func(t *testing.T) {
		config := newSpawnConfig()
		require.EqualValues(t, 0, config.snapshotInterval)
	})
	t.Run("WithRetentionPolicy", func(t *testing.T) {
		policy := RetentionPolicy{
			DeleteEventsOnSnapshot:    true,
			DeleteSnapshotsOnSnapshot: true,
			EventsRetentionCount:      100,
		}
		config := newSpawnConfig(WithRetentionPolicy(policy))
		require.NotNil(t, config.retentionPolicy)
		require.True(t, config.retentionPolicy.DeleteEventsOnSnapshot)
		require.True(t, config.retentionPolicy.DeleteSnapshotsOnSnapshot)
		require.EqualValues(t, 100, config.retentionPolicy.EventsRetentionCount)
	})
	t.Run("default config has no retention policy", func(t *testing.T) {
		config := newSpawnConfig()
		require.Nil(t, config.retentionPolicy)
	})
	t.Run("default config has RestartDirective", func(t *testing.T) {
		config := newSpawnConfig()
		require.Equal(t, RestartDirective, config.supervisorDirective)
	})
	t.Run("default config has RoundRobin placement", func(t *testing.T) {
		config := newSpawnConfig()
		require.Equal(t, RoundRobin, config.entitiesPlacement)
	})
}

// withDefaults returns c with the defaults newSpawnConfig applies
// (RestartDirective, RoundRobin) where c leaves them zero; c must not set
// either field.
func withDefaults(c spawnConfig) *spawnConfig {
	c.supervisorDirective = RestartDirective
	c.entitiesPlacement = RoundRobin
	return &c
}
