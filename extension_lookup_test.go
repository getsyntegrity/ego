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

package ego

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"

	"github.com/pablogore/ego/v4/eventstream"
	"github.com/pablogore/ego/v4/internal/extensions"
	"github.com/pablogore/ego/v4/internal/pause"
	mocks "github.com/pablogore/ego/v4/mocks/persistence"
	"github.com/pablogore/ego/v4/persistence"
)

// requireExtensionProbeActor exercises requireExtension directly from
// PreStart, so the helper's branches can be pinned down without spinning up
// a full entity, saga, or projection actor for every case.
type requireExtensionProbeActor struct {
	lookup func(ctx *goakt.Context) error
}

var _ goakt.Actor = (*requireExtensionProbeActor)(nil)

func (a *requireExtensionProbeActor) PreStart(ctx *goakt.Context) error {
	return a.lookup(ctx)
}

func (a *requireExtensionProbeActor) PostStop(_ *goakt.Context) error { return nil }

func (a *requireExtensionProbeActor) Receive(ctx *goakt.ReceiveContext) { ctx.Unhandled() }

// TestRequireExtension pins down requireExtension's behavior: it must return
// a descriptive error — never panic — whenever the requested extension is
// absent or was registered under an unexpected type, and it must return the
// typed extension when the lookup succeeds. See issue #99: an unchecked
// ctx.Extension(id).(*T) type assertion in PreStart panics, and that panic
// crashes the whole process because goakt drives Spawn/SpawnChild through a
// golang.org/x/sync/singleflight.Group that deliberately re-panics on a
// fresh, unrecoverable goroutine (see extension_lookup.go).
func TestRequireExtension(t *testing.T) {
	t.Run("returns an error instead of panicking when the extension is absent", func(t *testing.T) {
		ctx := context.TODO()

		actorSystem, err := goakt.NewActorSystem("TestRequireExtensionMissingSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		probe := &requireExtensionProbeActor{
			lookup: func(ctx *goakt.Context) error {
				_, err := requireExtension[*extensions.EventsStore](ctx, extensions.EventsStoreExtensionID)
				return err
			},
		}
		pid, err := actorSystem.Spawn(ctx, "require-ext-missing", probe)
		require.Error(t, err)
		require.Nil(t, pid)
		assert.ErrorIs(t, err, ErrMissingRequiredExtensions)

		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("returns an error instead of panicking when the extension is registered under a mismatched type", func(t *testing.T) {
		ctx := context.TODO()

		eventStream := eventstream.New()

		actorSystem, err := goakt.NewActorSystem("TestRequireExtensionMismatchSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		probe := &requireExtensionProbeActor{
			lookup: func(ctx *goakt.Context) error {
				// The events-stream extension is registered, but under the
				// events-store ID it is asked for here: simulates an
				// extension registered with an unexpected concrete type.
				_, err := requireExtension[*extensions.EventsStore](ctx, extensions.EventsStreamExtensionID)
				return err
			},
		}
		pid, err := actorSystem.Spawn(ctx, "require-ext-mismatch", probe)
		require.Error(t, err)
		require.Nil(t, pid)
		assert.ErrorIs(t, err, ErrMissingRequiredExtensions)

		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("returns the typed extension when registered correctly", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := new(mocks.EventsStore)
		eventStore.EXPECT().Ping(mock.Anything).Return(nil).Maybe()

		actorSystem, err := goakt.NewActorSystem("TestRequireExtensionOKSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		var got persistence.EventsStore
		probe := &requireExtensionProbeActor{
			lookup: func(ctx *goakt.Context) error {
				ext, err := requireExtension[*extensions.EventsStore](ctx, extensions.EventsStoreExtensionID)
				if err != nil {
					return err
				}
				got = ext.Underlying()
				return nil
			},
		}
		pid, err := actorSystem.Spawn(ctx, "require-ext-ok", probe)
		require.NoError(t, err)
		require.NotNil(t, pid)
		assert.Equal(t, persistence.EventsStore(eventStore), got)

		require.NoError(t, actorSystem.Stop(ctx))
	})
}
