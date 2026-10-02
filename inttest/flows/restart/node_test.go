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

package restart_test

import (
	"context"
	"sync"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	goakt "github.com/tochemey/goakt/v4/actor"

	"github.com/getsyntegrity/urd/engine"
	"github.com/getsyntegrity/urd/persistence/postgres"
)

// node is one running eGo process: an actor system, the engine plugged into it and the Postgres events store
// the engine persists to. Stopping it and starting another on the same database is the restart under test.
type node struct {
	store  *postgres.EventStore
	system goakt.ActorSystem
	engine *engine.Engine

	stopOnce sync.Once
}

// startNode builds a node on the database at dsn. WithSchemaMigration makes engine.Start create the schema, so
// the flow proves the opt-in option end to end, and the second node finds it already current. The actor system
// has a unique name and no remoting, so nodes of parallel tests share no name and no port.
func startNode(sc *specs.Context, dsn string) *node {
	sc.Helper()
	ctx := context.Background()

	n := &node{store: postgres.NewEventStore(dsn)}
	// Registered before anything can fail, so a node that only half started is still torn down with the spec.
	// stop is idempotent, so the explicit stop of the restart flow makes this a no-op.
	sc.Cleanup(func() { n.stop(sc) })
	sc.Expect(n.store.Connect(ctx)).To(specs.BeNil())

	cfg := engine.NewConfig(n.store, engine.WithSchemaMigration(), engine.WithLogger(engine.DiscardLogger))
	system, err := goakt.NewActorSystem("inttest-"+uuid.NewString(), cfg.GoaktOptions()...)
	sc.Expect(err).To(specs.BeNil())
	n.system = system
	sc.Expect(system.Start(ctx)).To(specs.BeNil())

	eng, err := engine.NewEngine(system, cfg)
	sc.Expect(err).To(specs.BeNil())
	n.engine = eng
	sc.Expect(eng.Start(ctx)).To(specs.BeNil())
	return n
}

// stop shuts the engine, the actor system and the store down, in that order, skipping what never started. It
// runs once and later calls do nothing. After it returns nothing of the node is alive, so a following node can
// only learn the entity state from the database.
func (n *node) stop(sc *specs.Context) {
	sc.Helper()
	n.stopOnce.Do(func() {
		ctx := context.Background()
		if n.engine != nil {
			sc.Expect(n.engine.Stop(ctx)).To(specs.BeNil())
		}
		if n.system != nil {
			sc.Expect(n.system.Stop(ctx)).To(specs.BeNil())
		}
		sc.Expect(n.store.Disconnect(ctx)).To(specs.BeNil())
	})
}
