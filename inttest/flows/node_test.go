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

package flows_test

import (
	"context"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	goakt "github.com/tochemey/goakt/v4/actor"

	"github.com/getsyntegrity/ego/engine"
	"github.com/getsyntegrity/ego/persistence/postgres"
)

// node is one running eGo process: an actor system, the engine plugged into it and the Postgres events store
// the engine persists to. Stopping it and starting another on the same database is the restart under test.
type node struct {
	store  *postgres.EventStore
	system goakt.ActorSystem
	engine *engine.Engine
}

// startNode builds a node on the database at dsn. WithSchemaMigration makes engine.Start create the schema, so
// the flow proves the opt-in option end to end, and the second node finds it already current. The actor system
// has a unique name and no remoting, so nodes of parallel tests share no name and no port.
func startNode(sc *specs.Context, dsn string) *node {
	sc.Helper()
	ctx := context.Background()

	store := postgres.NewEventStore(dsn)
	sc.Expect(store.Connect(ctx)).To(specs.BeNil())

	cfg := engine.NewConfig(store, engine.WithSchemaMigration(), engine.WithLogger(engine.DiscardLogger))
	system, err := goakt.NewActorSystem("inttest-"+uuid.NewString(), cfg.GoaktOptions()...)
	sc.Expect(err).To(specs.BeNil())
	sc.Expect(system.Start(ctx)).To(specs.BeNil())

	eng, err := engine.NewEngine(system, cfg)
	sc.Expect(err).To(specs.BeNil())
	sc.Expect(eng.Start(ctx)).To(specs.BeNil())

	return &node{store: store, system: system, engine: eng}
}

// stop shuts the engine, the actor system and the store down, in that order. After it returns nothing of the
// node is alive, so a following node can only learn the entity state from the database.
func (n *node) stop(sc *specs.Context) {
	sc.Helper()
	ctx := context.Background()
	sc.Expect(n.engine.Stop(ctx)).To(specs.BeNil())
	sc.Expect(n.system.Stop(ctx)).To(specs.BeNil())
	sc.Expect(n.store.Disconnect(ctx)).To(specs.BeNil())
}
