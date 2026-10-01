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
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/getsyntegrity/ego/command"
	"github.com/getsyntegrity/ego/testkit"
)

// This file holds go-specs versions of the helpers the engine tests of group
// g4 need from helper_test.go and from files owned by other groups. Every name
// carries the g4 suffix so the groups never clash on a declaration.

// connectedEventsStoreG4 returns an in-memory events store that is connected
// now and disconnected when the case ends.
func connectedEventsStoreG4(ctx *specs.Context) *testkit.EventStore {
	bg := context.Background()
	store := testkit.NewEventsStore()
	ctx.Expect(store.Connect(bg)).To(specs.BeNil())
	ctx.Cleanup(func() { _ = store.Disconnect(bg) })
	return store
}

// connectedStateStoreG4 is connectedEventsStoreG4 for the durable-state store.
func connectedStateStoreG4(ctx *specs.Context) *testkit.DurableStore {
	bg := context.Background()
	store := testkit.NewDurableStore()
	ctx.Expect(store.Connect(bg)).To(specs.BeNil())
	ctx.Cleanup(func() { _ = store.Disconnect(bg) })
	return store
}

// connectedOffsetStoreG4 is connectedEventsStoreG4 for the offset store.
func connectedOffsetStoreG4(ctx *specs.Context) *testkit.OffsetStore {
	bg := context.Background()
	store := testkit.NewOffsetStore()
	ctx.Expect(store.Connect(bg)).To(specs.BeNil())
	ctx.Cleanup(func() { _ = store.Disconnect(bg) })
	return store
}

// dispatchWithMetadataG4 sends payload to the entity as a command envelope
// carrying the given metadata options and returns the command result.
func dispatchWithMetadataG4(ctx *specs.Context, engine *Engine, entityID string, payload proto.Message,
	opts ...command.MetadataOption) command.Result {
	md, err := command.NewMetadata(command.OperationID(uuid.NewString()), opts...)
	ctx.Expect(err).To(specs.BeNil())
	env, err := command.NewEnvelope(payload, md)
	ctx.Expect(err).To(specs.BeNil())
	result, err := engine.Dispatch(context.Background(), entityID, env, time.Minute)
	ctx.Expect(err).To(specs.BeNil())
	return result
}
