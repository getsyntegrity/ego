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
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"

	"github.com/getsyntegrity/urd/eventstream"
	"github.com/getsyntegrity/urd/internal/engine/protocol"
	"github.com/getsyntegrity/urd/testkit"
)

// familyTestEngineG4 builds a started engine with an events store and a state
// store, so a rejected spawn can only come from the declared-family guard,
// never from a missing store.
func familyTestEngineG4(ctx *specs.Context, name string, opts ...Option) *Engine {
	opts = append([]Option{WithLogger(DiscardLogger), WithStateStore(connectedDurableStore(ctx))}, opts...)
	engine := newTestEngine(ctx.T, name, connectedEventsStore(ctx), opts...)
	ctx.Expect(engine.Start(context.Background())).To(specs.BeNil())
	return engine
}

// spawnCase is one spawn entry point for one family: the deprecated public
// method or its runtime-neutral successor.
type spawnCase struct {
	name   string
	family EntityFamily
	spawn  func(ctx context.Context, engine *Engine, id string) error
}

// everySpawnEntryPoint lists both entry points of every family, so the
// guard is proven to live in the one shared unexported spawn function.
func everySpawnEntryPoint() []spawnCase {
	return []spawnCase{
		{"Entity (deprecated)", EventSourcedFamily, func(ctx context.Context, e *Engine, id string) error {
			return e.Entity(ctx, NewAccountEventSourcedBehavior(id))
		}},
		{"SpawnEventSourced", EventSourcedFamily, func(ctx context.Context, e *Engine, id string) error {
			return e.SpawnEventSourced(ctx, &domainOnlyEventSourced{id: id})
		}},
		{"DurableStateEntity (deprecated)", DurableStateFamily, func(ctx context.Context, e *Engine, id string) error {
			return e.DurableStateEntity(ctx, NewAccountDurableStateBehavior(id))
		}},
		{"SpawnDurableState", DurableStateFamily, func(ctx context.Context, e *Engine, id string) error {
			return e.SpawnDurableState(ctx, &domainOnlyDurableState{id: id})
		}},
		{"Saga (deprecated)", SagaFamily, func(ctx context.Context, e *Engine, id string) error {
			return e.Saga(ctx, &testSagaBehavior{sagaID: id}, time.Second)
		}},
		{"SpawnSaga", SagaFamily, func(ctx context.Context, e *Engine, id string) error {
			return e.SpawnSaga(ctx, &domainOnlySaga{id: id}, time.Second)
		}},
	}
}

// familyRowG4 is one spawn entry point tried on an engine built with opts,
// and whether the declared-family guard must reject it.
type familyRowG4 struct {
	name     string
	opts     []Option
	entry    spawnCase
	rejected bool
}

// familyRowsG4 returns one row per entry point for an engine built with opts.
// rejects says which families the declaration leaves out.
func familyRowsG4(prefix string, opts []Option, rejects func(EntityFamily) bool) []familyRowG4 {
	var rows []familyRowG4
	for _, entry := range everySpawnEntryPoint() {
		rows = append(rows, familyRowG4{
			name:     prefix + "/" + entry.name,
			opts:     opts,
			entry:    entry,
			rejected: rejects(entry.family),
		})
	}
	return rows
}

func familyRowNameG4(r familyRowG4) string { return r.name }

// expectFamilySpawn spawns through the row's entry point on a fresh engine. A
// rejected spawn must return ErrEntityFamilyNotDeclared, name the undeclared
// family and spawn nothing; any other spawn must succeed.
func expectFamilySpawn(ctx *specs.Context, r familyRowG4) {
	bg := context.Background()
	engine := familyTestEngineG4(ctx, "family-"+uuid.NewString()[:8], r.opts...)
	id := "family-" + uuid.NewString()

	err := r.entry.spawn(bg, engine, id)

	if !r.rejected {
		ctx.Expect(err).To(specs.BeNil())
		return
	}
	ctx.Expect(err).To(specs.MatchError(ErrEntityFamilyNotDeclared))
	// the error names the undeclared family
	ctx.Expect(err.Error()).To(specs.Contain(r.entry.family.String()))
	exists, existsErr := engine.EntityExists(bg, id)
	ctx.Expect(existsErr).To(specs.BeNil())
	// a rejected spawn must not spawn anything
	ctx.Expect(exists).To(specs.BeFalse())
}

// TestWithEntityFamilies_UndeclaredFamilyIsRejected declares one family at a
// time and spawns through every entry point: the declared family spawns, the
// other two return ErrEntityFamilyNotDeclared before anything is spawned
// (ego-arch-003 design §D3, IMPL-4).
func TestWithEntityFamilies_UndeclaredFamilyIsRejected(t *testing.T) {
	specs.Describe(t, "an entity family that was not declared is rejected at spawn", func(s *specs.Spec) {
		var rows []familyRowG4
		for _, declared := range []EntityFamily{EventSourcedFamily, DurableStateFamily, SagaFamily} {
			rows = append(rows, familyRowsG4("declared "+declared.String(), []Option{WithEntityFamilies(declared)},
				func(family EntityFamily) bool { return family != declared })...)
		}
		specs.Table(s, rows, familyRowNameG4, expectFamilySpawn)
	})
}

// TestWithEntityFamilies_Combined declares several families in one or more
// calls; the declared set is their union.
func TestWithEntityFamilies_Combined(t *testing.T) {
	specs.Describe(t, "several WithEntityFamilies calls declare the union of their families", func(s *specs.Spec) {
		opts := []Option{WithEntityFamilies(EventSourcedFamily), WithEntityFamilies(SagaFamily)}
		specs.Table(s, familyRowsG4("event-sourced and saga declared", opts,
			func(family EntityFamily) bool { return family == DurableStateFamily }), familyRowNameG4, expectFamilySpawn)
	})
}

// TestWithEntityFamilies_NotDeclaredAllowsEveryFamily keeps the manual path
// unchanged: without WithEntityFamilies, or with no family passed to it,
// every family spawns as before.
func TestWithEntityFamilies_NotDeclaredAllowsEveryFamily(t *testing.T) {
	specs.Describe(t, "an engine that declares no family spawns every family", func(s *specs.Spec) {
		never := func(EntityFamily) bool { return false }
		var rows []familyRowG4
		rows = append(rows, familyRowsG4("option absent", nil, never)...)
		rows = append(rows, familyRowsG4("no family passed", []Option{WithEntityFamilies()}, never)...)
		rows = append(rows, familyRowsG4("zero family value", []Option{WithEntityFamilies(0)}, never)...)
		specs.Table(s, rows, familyRowNameG4, expectFamilySpawn)
	})
}

// TestWithEntityFamilies_UnknownBitsAreIgnored masks a declaration to the
// three known families: an unknown bit alone declares nothing (every family
// spawns, as without the option), and an unknown bit next to a known one
// declares only the known one.
func TestWithEntityFamilies_UnknownBitsAreIgnored(t *testing.T) {
	specs.Describe(t, "a declaration is masked to the three known families", func(s *specs.Spec) {
		var rows []familyRowG4
		rows = append(rows, familyRowsG4("unknown bit only", []Option{WithEntityFamilies(EntityFamily(8))},
			func(EntityFamily) bool { return false })...)
		rows = append(rows, familyRowsG4("unknown bit next to a known one",
			[]Option{WithEntityFamilies(EventSourcedFamily | EntityFamily(8))},
			func(family EntityFamily) bool { return family != EventSourcedFamily })...)
		specs.Table(s, rows, familyRowNameG4, expectFamilySpawn)
	})
}

func TestEntityFamily_String(t *testing.T) {
	specs.Describe(t, "EntityFamily.String names the declared families", func(s *specs.Spec) {
		s.It("renders one name per family, a union with a pipe and an unknown value by number", func(ctx *specs.Context) {
			ctx.Expect(EventSourcedFamily.String()).ToEqual("EventSourced")
			ctx.Expect(DurableStateFamily.String()).ToEqual("DurableState")
			ctx.Expect(SagaFamily.String()).ToEqual("Saga")
			ctx.Expect((EventSourcedFamily | SagaFamily).String()).ToEqual("EventSourced|Saga")
			ctx.Expect(EntityFamily(0).String()).ToEqual("EntityFamily(0)")
			ctx.Expect(EntityFamily(8).String()).ToEqual("EntityFamily(8)")
		})
	})
}

// closeCountingStream records Close calls on an otherwise real stream.
type closeCountingStream struct {
	eventstream.Stream
	closed atomic.Int32
}

func (s *closeCountingStream) Close() {
	s.closed.Add(1)
	s.Stream.Close()
}

// TestWithEventStream_UsesTheGivenStream proves the stream handed in is the
// one the actor system and the engine share: the engine's publishers read
// from it and Engine.Stop closes it (ego-arch-003 design §D5).
func TestWithEventStream_UsesTheGivenStream(t *testing.T) {
	specs.Describe(t, "WithEventStream makes the engine use the stream it was given", func(s *specs.Spec) {
		s.It("subscribes on the given stream and closes it on Stop", func(ctx *specs.Context) {
			bg := context.Background()
			stream := &closeCountingStream{Stream: eventstream.New()}

			cfg := NewConfig(testkit.NewEventsStore(), WithLogger(DiscardLogger), WithEventStream(stream))
			ctx.Expect(cfg.eventStream == eventstream.Stream(stream)).To(specs.BeTrue())

			engine := newTestEngine(ctx.T, "with-event-stream", testkit.NewEventsStore(), WithLogger(DiscardLogger), WithEventStream(stream))
			ctx.Expect(engine.Start(bg)).To(specs.BeNil())
			subscriber, err := engine.Subscribe()
			ctx.Expect(err).To(specs.BeNil())
			// Subscribe must register on the given stream
			ctx.Expect(stream.SubscribersCount(protocol.EventsTopic)).ToEqual(1)
			subscriber.Shutdown()

			ctx.Expect(engine.Stop(bg)).To(specs.BeNil())
			// Engine.Stop must close the given stream
			ctx.Expect(stream.closed.Load()).To(specs.Equal(int32(1)))
		})
	})
}

// nilStreamCase is one kind of "no stream" WithEventStream must ignore.
type nilStreamCase struct {
	name   string
	stream eventstream.Stream
}

// TestWithEventStream_NilKeepsTheDefault ignores a nil or typed-nil stream,
// so NewConfig's own stream stays in place.
func TestWithEventStream_NilKeepsTheDefault(t *testing.T) {
	specs.Describe(t, "WithEventStream ignores a nil or typed-nil stream and keeps NewConfig's own", func(s *specs.Spec) {
		specs.Table(s, []nilStreamCase{
			{"nil", nil},
			{"typed nil", (*eventstream.EventsStream)(nil)},
		}, func(c nilStreamCase) string {
			return "keeps the default stream for a " + c.name + " stream"
		}, func(ctx *specs.Context, c nilStreamCase) {
			cfg := NewConfig(nil, WithEventStream(c.stream))
			ctx.Expect(cfg.eventStream != nil).To(specs.BeTrue())
			def, isDefault := cfg.eventStream.(*eventstream.EventsStream)
			ctx.Expect(isDefault).To(specs.BeTrue())
			// the default stream must be kept, not replaced by a typed nil
			ctx.Expect(def != nil).To(specs.BeTrue())
		})
	})
}
