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

package extensions

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/extension"
	"github.com/tochemey/goakt/v4/log"
)

// probeExtension is a minimal goakt extension registered under a chosen ID.
type probeExtension struct{ id string }

func (p *probeExtension) ID() string { return p.id }

// otherExtension is a second concrete type, used to register an extension
// under an ID whose expected type it does not match.
type otherExtension struct{ id string }

func (o *otherExtension) ID() string { return o.id }

// lookupProbeActor runs a lookup from PreStart, where the helpers are used.
type lookupProbeActor struct {
	lookup func(ctx *goakt.Context) error
}

var _ goakt.Actor = (*lookupProbeActor)(nil)

func (a *lookupProbeActor) PreStart(ctx *goakt.Context) error { return a.lookup(ctx) }
func (a *lookupProbeActor) PostStop(_ *goakt.Context) error   { return nil }
func (a *lookupProbeActor) Receive(ctx *goakt.ReceiveContext) { ctx.Unhandled() }

func startProbeSystem(t *testing.T, name string, exts ...extension.Extension) goakt.ActorSystem {
	t.Helper()
	ctx := context.Background()

	opts := []goakt.Option{goakt.WithLogger(log.DiscardLogger), goakt.WithActorInitMaxRetries(1)}
	if len(exts) > 0 {
		opts = append(opts, goakt.WithExtensions(exts...))
	}
	actorSystem, err := goakt.NewActorSystem(name, opts...)
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	t.Cleanup(func() { _ = actorSystem.Stop(ctx) })
	return actorSystem
}

func TestOptional(t *testing.T) {
	const id = "OptionalProbeExtension"

	t.Run("a missing extension yields the zero value and no error", func(t *testing.T) {
		actorSystem := startProbeSystem(t, "OptionalMissingSystem")

		var got *probeExtension
		probe := &lookupProbeActor{lookup: func(ctx *goakt.Context) error {
			var err error
			got, err = Optional[*probeExtension](ctx, id)
			return err
		}}
		pid, err := actorSystem.Spawn(context.Background(), "optional-missing", probe)
		require.NoError(t, err)
		require.NotNil(t, pid)
		assert.Nil(t, got)
	})

	t.Run("a registered extension of the expected type is returned", func(t *testing.T) {
		registered := &probeExtension{id: id}
		actorSystem := startProbeSystem(t, "OptionalPresentSystem", registered)

		var got *probeExtension
		probe := &lookupProbeActor{lookup: func(ctx *goakt.Context) error {
			var err error
			got, err = Optional[*probeExtension](ctx, id)
			return err
		}}
		pid, err := actorSystem.Spawn(context.Background(), "optional-present", probe)
		require.NoError(t, err)
		require.NotNil(t, pid)
		assert.Same(t, registered, got)
	})

	t.Run("an extension of an unexpected type is an error, not a panic", func(t *testing.T) {
		actorSystem := startProbeSystem(t, "OptionalMismatchSystem", &otherExtension{id: id})

		probe := &lookupProbeActor{lookup: func(ctx *goakt.Context) error {
			_, err := Optional[*probeExtension](ctx, id)
			return err
		}}
		pid, err := actorSystem.Spawn(context.Background(), "optional-mismatch", probe)
		require.Error(t, err)
		require.Nil(t, pid)
		assert.ErrorIs(t, err, ErrMissingRequiredExtensions)
		assert.Contains(t, err.Error(), "was registered with unexpected type")
	})
}
