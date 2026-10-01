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

	"github.com/getsyntegrity/go-specs/specs"
	goakt "github.com/tochemey/goakt/v4/actor"

	"github.com/getsyntegrity/ego/persistence"
	"github.com/getsyntegrity/ego/testkit"
)

// newConnectedEventsStoreG3 returns a connected in-memory events store that is
// disconnected when the case ends.
func newConnectedEventsStoreG3(ctx *specs.Context) *testkit.EventStore {
	bg := context.Background()
	store := testkit.NewEventsStore()
	ctx.Expect(store.Connect(bg)).To(specs.BeNil())
	ctx.Cleanup(func() { _ = store.Disconnect(bg) })
	return store
}

// newSpecsEngineG3 is the go-specs counterpart of newTestEngine (helper_test.go):
// it builds an actor system and an Engine on top of it, and stops both when the
// case ends, the engine first.
func newSpecsEngineG3(ctx *specs.Context, name string, eventsStore persistence.EventsStore, opts ...Option) *Engine {
	bg := context.Background()

	cfg := NewConfig(eventsStore, opts...)
	sys, err := goakt.NewActorSystem(name, cfg.GoaktOptions()...)
	ctx.Expect(err).To(specs.BeNil())
	ctx.Expect(sys.Start(bg)).To(specs.BeNil())
	ctx.Cleanup(func() { _ = sys.Stop(bg) })

	engine, err := NewEngine(sys, cfg)
	ctx.Expect(err).To(specs.BeNil())
	ctx.Cleanup(func() { _ = engine.Stop(bg) })

	return engine
}
