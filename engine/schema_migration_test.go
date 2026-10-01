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
	"errors"
	"testing"

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/testkit"
)

var errSchemaDown = errors.New("schema backend down")

// schemaMigratorMock is a go-specs mock of persistence.SchemaMigrator. Embedded
// in a store it makes that store a SchemaMigrator whose calls the case's
// controller verifies.
type schemaMigratorMock struct{ c *mock.Controller }

func (m schemaMigratorMock) Migrate(ctx context.Context) error {
	return m.c.Method("Migrate").Call(ctx).Err(0)
}

func (m schemaMigratorMock) SchemaVersion(ctx context.Context) (uint, error) {
	r := m.c.Method("SchemaVersion").Call(ctx)
	return mock.Value[uint](r, 0), r.Err(1)
}

// migratingEventsStore, migratingDurableStore and migratingOffsetStore are the
// in-memory testkit stores with SchemaMigrator added by the mock.
type migratingEventsStore struct {
	*testkit.EventStore
	schemaMigratorMock
}

type migratingDurableStore struct {
	*testkit.DurableStore
	schemaMigratorMock
}

type migratingOffsetStore struct {
	*testkit.OffsetStore
	schemaMigratorMock
}

// migratingSnapshotStore is a snapshot store that migrates too.
type migratingSnapshotStore struct {
	*testkit.SnapshotStore
	schemaMigratorMock
}

// expectMigrate declares how many times a store's Migrate runs in the case, and
// the error it answers.
func expectMigrate(ctrl *mock.Controller, times int, err error) {
	if times == 0 {
		ctrl.Method("Migrate").Expect(mock.Any()).Never()
		return
	}
	ctrl.Method("Migrate").Expect(mock.Any()).Return(err).Times(times)
}

// schemaEngine builds an Engine that has the stores given and has not started.
// It needs no actor system: Start only checks that one is attached, so an empty
// reference stands in for it.
func schemaEngine(migrate bool, build func(e *Engine)) *Engine {
	e := &Engine{schemaMigration: migrate}
	build(e)
	e.actorSystem.Store(&actorSystemRef{})
	return e
}

func TestWithSchemaMigrationSetsTheConfig(t *testing.T) {
	specs.Describe(t, "WithSchemaMigration", func(s *specs.Spec) {
		s.It("is off unless the option is given", func(ctx *specs.Context) {
			ctx.Expect(NewConfig(nil).schemaMigration).To(specs.BeFalse())
		})

		s.It("turns schema migration on", func(ctx *specs.Context) {
			ctx.Expect(NewConfig(nil, WithSchemaMigration()).schemaMigration).To(specs.BeTrue())
		})
	})
}

func TestEngineStartMigratesTheSchema(t *testing.T) {
	bg := context.Background()
	specs.Describe(t, "Engine.Start with and without WithSchemaMigration", func(s *specs.Spec) {
		s.It("does not call Migrate when the option is not given", func(ctx *specs.Context) {
			ctrl := mock.NewController(ctx)
			expectMigrate(ctrl, 0, nil)
			engine := schemaEngine(false, func(e *Engine) {
				e.eventsStore = migratingEventsStore{testkit.NewEventsStore(), schemaMigratorMock{ctrl}}
			})

			ctx.Expect(engine.Start(bg)).To(specs.BeNil())
			ctx.Expect(engine.Started()).To(specs.BeTrue())
		})

		s.It("calls Migrate once on every store that has it before Start returns", func(ctx *specs.Context) {
			events, durable, offsets, snapshots := mock.NewController(ctx), mock.NewController(ctx), mock.NewController(ctx), mock.NewController(ctx)
			var order []string
			for name, ctrl := range map[string]*mock.Controller{"events": events, "durable": durable, "offsets": offsets, "snapshots": snapshots} {
				ctrl.Method("Migrate").Expect(mock.Any()).Do(func([]any) []any {
					order = append(order, name)
					return []any{nil}
				})
			}
			engine := schemaEngine(true, func(e *Engine) {
				e.eventsStore = migratingEventsStore{testkit.NewEventsStore(), schemaMigratorMock{events}}
				e.stateStore = migratingDurableStore{testkit.NewDurableStore(), schemaMigratorMock{durable}}
				e.offsetStore = migratingOffsetStore{testkit.NewOffsetStore(), schemaMigratorMock{offsets}}
				e.snapshotStore = migratingSnapshotStore{testkit.NewSnapshotStore(), schemaMigratorMock{snapshots}}
			})

			ctx.Expect(engine.Start(bg)).To(specs.BeNil())
			ctx.Expect(engine.Started()).To(specs.BeTrue())
			ctx.Expect(order).To(specs.ContainTheSameElementsAs([]string{"events", "durable", "offsets", "snapshots"}))
		})

		s.It("ignores a store that cannot migrate", func(ctx *specs.Context) {
			ctrl := mock.NewController(ctx)
			expectMigrate(ctrl, 1, nil)
			engine := schemaEngine(true, func(e *Engine) {
				e.eventsStore = testkit.NewEventsStore() // no Migrate
				e.offsetStore = migratingOffsetStore{testkit.NewOffsetStore(), schemaMigratorMock{ctrl}}
			})

			ctx.Expect(engine.Start(bg)).To(specs.BeNil())
			ctx.Expect(engine.Started()).To(specs.BeTrue())
		})

		s.It("starts when no configured store can migrate", func(ctx *specs.Context) {
			engine := schemaEngine(true, func(e *Engine) { e.eventsStore = testkit.NewEventsStore() })

			ctx.Expect(engine.Start(bg)).To(specs.BeNil())
			ctx.Expect(engine.Started()).To(specs.BeTrue())
		})

		s.It("returns the Migrate error, names the store, and does not start the engine", func(ctx *specs.Context) {
			events, offsets := mock.NewController(ctx), mock.NewController(ctx)
			expectMigrate(events, 1, errSchemaDown)
			expectMigrate(offsets, 0, nil) // the first failure stops the rest
			engine := schemaEngine(true, func(e *Engine) {
				e.eventsStore = migratingEventsStore{testkit.NewEventsStore(), schemaMigratorMock{events}}
				e.offsetStore = migratingOffsetStore{testkit.NewOffsetStore(), schemaMigratorMock{offsets}}
			})

			err := engine.Start(bg)

			ctx.Expect(err).To(specs.MatchError(errSchemaDown))
			ctx.Expect(err.Error()).To(specs.MatchRegex("events store"))
			ctx.Expect(engine.Started()).To(specs.BeFalse())
		})
	})
}

func TestEngineStartMigratesThroughNewEngine(t *testing.T) {
	specs.Describe(t, "WithSchemaMigration through NewConfig and NewEngine", func(s *specs.Spec) {
		s.It("migrates the configured stores when the engine starts", func(ctx *specs.Context) {
			ctrl := mock.NewController(ctx)
			expectMigrate(ctrl, 1, nil)
			store := migratingEventsStore{testkit.NewEventsStore(), schemaMigratorMock{ctrl}}

			engine := newSpecsEngine(ctx, "schema-migration-wiring", store, WithSchemaMigration())
			ctx.Expect(engine.Start(context.Background())).To(specs.BeNil())
			ctx.Expect(engine.Started()).To(specs.BeTrue())
		})
	})
}
