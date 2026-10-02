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

	"github.com/getsyntegrity/go-specs/specs"
)

func TestSpawnOption(t *testing.T) {
	specs.Describe(t, "newSpawnConfig applies each spawn option and the defaults", func(s *specs.Spec) {
		s.It("WithPassivateAfter with options", func(ctx *specs.Context) {
			config := newSpawnConfig(WithPassivateAfter(time.Second))
			specs.ExpectT(ctx, config.passivateAfter).ToEqual(time.Second)
		})
		s.It("WithPassivateAfter changes only passivateAfter", func(ctx *specs.Context) {
			config := newSpawnConfig(WithPassivateAfter(time.Second))
			ctx.Expect(config).ToEqual(withDefaults(spawnConfig{passivateAfter: time.Second}))
		})
		s.It("WithRelocation", func(ctx *specs.Context) {
			config := newSpawnConfig(WithRelocation(true))
			ctx.Expect(config).ToEqual(withDefaults(spawnConfig{toRelocate: true}))
		})
		s.It("WithSupervisorDirective", func(ctx *specs.Context) {
			config := newSpawnConfig(WithSupervisorDirective(StopDirective))
			ctx.Expect(config).ToEqual(&spawnConfig{supervisorDirective: StopDirective, entitiesPlacement: RoundRobin})
		})
		s.It("WithEntitiesPlacement", func(ctx *specs.Context) {
			config := newSpawnConfig(WithPlacement(LeastLoad))
			ctx.Expect(config).ToEqual(&spawnConfig{supervisorDirective: RestartDirective, entitiesPlacement: LeastLoad})
		})
		s.It("WithSnapshotInterval", func(ctx *specs.Context) {
			config := newSpawnConfig(WithSnapshotInterval(10))
			ctx.Expect(config).ToEqual(withDefaults(spawnConfig{snapshotInterval: 10}))
		})
		s.It("WithSnapshotInterval zero is default", func(ctx *specs.Context) {
			config := newSpawnConfig()
			specs.ExpectT(ctx, config.snapshotInterval).ToEqual(0)
		})
		s.It("WithRetentionPolicy", func(ctx *specs.Context) {
			policy := RetentionPolicy{
				DeleteEventsOnSnapshot:    true,
				DeleteSnapshotsOnSnapshot: true,
				EventsRetentionCount:      100,
			}
			config := newSpawnConfig(WithRetentionPolicy(policy))
			ctx.Expect(config.retentionPolicy != nil).To(specs.BeTrue())
			ctx.Expect(config.retentionPolicy.DeleteEventsOnSnapshot).To(specs.BeTrue())
			ctx.Expect(config.retentionPolicy.DeleteSnapshotsOnSnapshot).To(specs.BeTrue())
			specs.ExpectT(ctx, config.retentionPolicy.EventsRetentionCount).ToEqual(100)
		})
		s.It("default config has no retention policy", func(ctx *specs.Context) {
			config := newSpawnConfig()
			ctx.Expect(config.retentionPolicy == nil).To(specs.BeTrue())
		})
		s.It("default config has RestartDirective", func(ctx *specs.Context) {
			config := newSpawnConfig()
			ctx.Expect(config.supervisorDirective).ToEqual(RestartDirective)
		})
		s.It("default config has RoundRobin placement", func(ctx *specs.Context) {
			config := newSpawnConfig()
			ctx.Expect(config.entitiesPlacement).ToEqual(RoundRobin)
		})
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
