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

package extensions_test

import (
	"context"
	"log/slog"
	"testing"

	kitlog "github.com/pablogore/kit-logger/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/log"

	"github.com/getsyntegrity/ego/eventstream"
	"github.com/getsyntegrity/ego/internal/extensions"
	"github.com/getsyntegrity/ego/internal/goaktlog"
	mocks "github.com/getsyntegrity/ego/mocks/persistence"
	"github.com/getsyntegrity/ego/persistence"
)

// discardGoaktLogger returns a GoAkt logger that drops every record, so the
// hand-built actor systems below stay quiet.
func discardGoaktLogger() log.Logger {
	return goaktlog.New(kitlog.New(kitlog.Config{Sink: slog.DiscardHandler}))
}

// requireProbeActor exercises Require directly from
// PreStart, so the helper's branches can be pinned down without spinning up
// a full entity, saga, or projection actor for every case.
type requireProbeActor struct {
	lookup func(ctx *goakt.Context) error
}

var _ goakt.Actor = (*requireProbeActor)(nil)

func (a *requireProbeActor) PreStart(ctx *goakt.Context) error {
	return a.lookup(ctx)
}

func (a *requireProbeActor) PostStop(_ *goakt.Context) error { return nil }

func (a *requireProbeActor) Receive(ctx *goakt.ReceiveContext) { ctx.Unhandled() }

// TestRequireExtension pins down Require's behavior: it must return
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
			goakt.WithLogger(discardGoaktLogger()),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		probe := &requireProbeActor{
			lookup: func(ctx *goakt.Context) error {
				_, err := extensions.Require[*extensions.EventsStore](ctx, extensions.EventsStoreExtensionID)
				return err
			},
		}
		pid, err := actorSystem.Spawn(ctx, "require-ext-missing", probe)
		require.Error(t, err)
		require.Nil(t, pid)
		assert.ErrorIs(t, err, extensions.ErrMissingRequiredExtensions)

		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("returns an error instead of panicking when the extension is registered under a mismatched type", func(t *testing.T) {
		ctx := context.TODO()

		eventStream := eventstream.New()

		actorSystem, err := goakt.NewActorSystem("TestRequireExtensionMismatchSystem",
			goakt.WithLogger(discardGoaktLogger()),
			goakt.WithExtensions(
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		probe := &requireProbeActor{
			lookup: func(ctx *goakt.Context) error {
				// The events-stream extension is registered, but under the
				// events-store ID it is asked for here: simulates an
				// extension registered with an unexpected concrete type.
				_, err := extensions.Require[*extensions.EventsStore](ctx, extensions.EventsStreamExtensionID)
				return err
			},
		}
		pid, err := actorSystem.Spawn(ctx, "require-ext-mismatch", probe)
		require.Error(t, err)
		require.Nil(t, pid)
		assert.ErrorIs(t, err, extensions.ErrMissingRequiredExtensions)

		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("returns the typed extension when registered correctly", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := new(mocks.EventsStore)
		eventStore.EXPECT().Ping(mock.Anything).Return(nil).Maybe()

		actorSystem, err := goakt.NewActorSystem("TestRequireExtensionOKSystem",
			goakt.WithLogger(discardGoaktLogger()),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		var got persistence.EventsStore
		probe := &requireProbeActor{
			lookup: func(ctx *goakt.Context) error {
				ext, err := extensions.Require[*extensions.EventsStore](ctx, extensions.EventsStoreExtensionID)
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

// TestOptionalExtension pins down Optional's behavior: unlike
// Require, a missing extension is not an error (the extension is
// genuinely optional), but a present-but-mismatched-type extension must
// still return a descriptive error — never panic. See issue #99: PreStart
// paths that treat an extension as optional (nil-checked) still had an
// unchecked ext.(*T) assertion after the nil check, which panics on a type
// mismatch and crashes the whole process the same way an unguarded
// required-extension assertion did (see extension_lookup.go).
func TestOptionalExtension(t *testing.T) {
	t.Run("returns the zero value and no error when the extension is absent", func(t *testing.T) {
		ctx := context.TODO()

		actorSystem, err := goakt.NewActorSystem("TestOptionalExtensionMissingSystem",
			goakt.WithLogger(discardGoaktLogger()),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		var got *extensions.SnapshotStoreExt
		probe := &requireProbeActor{
			lookup: func(ctx *goakt.Context) error {
				ext, err := extensions.Optional[*extensions.SnapshotStoreExt](ctx, extensions.SnapshotStoreExtensionID)
				got = ext
				return err
			},
		}
		pid, err := actorSystem.Spawn(ctx, "optional-ext-missing", probe)
		require.NoError(t, err)
		require.NotNil(t, pid)
		assert.Nil(t, got)

		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("returns an error instead of panicking when the extension is registered under a mismatched type", func(t *testing.T) {
		ctx := context.TODO()

		eventStream := eventstream.New()

		actorSystem, err := goakt.NewActorSystem("TestOptionalExtensionMismatchSystem",
			goakt.WithLogger(discardGoaktLogger()),
			goakt.WithExtensions(
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		probe := &requireProbeActor{
			lookup: func(ctx *goakt.Context) error {
				// The events-stream extension is registered, but under the
				// snapshot-store ID it is asked for here: simulates an
				// extension registered with an unexpected concrete type.
				_, err := extensions.Optional[*extensions.SnapshotStoreExt](ctx, extensions.EventsStreamExtensionID)
				return err
			},
		}
		pid, err := actorSystem.Spawn(ctx, "optional-ext-mismatch", probe)
		require.Error(t, err)
		require.Nil(t, pid)
		assert.ErrorIs(t, err, extensions.ErrMissingRequiredExtensions)

		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("returns the typed extension when registered correctly", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := new(mocks.EventsStore)
		eventStore.EXPECT().Ping(mock.Anything).Return(nil).Maybe()

		actorSystem, err := goakt.NewActorSystem("TestOptionalExtensionOKSystem",
			goakt.WithLogger(discardGoaktLogger()),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		var got persistence.EventsStore
		probe := &requireProbeActor{
			lookup: func(ctx *goakt.Context) error {
				ext, err := extensions.Optional[*extensions.EventsStore](ctx, extensions.EventsStoreExtensionID)
				if err != nil {
					return err
				}
				if ext != nil {
					got = ext.Underlying()
				}
				return nil
			},
		}
		pid, err := actorSystem.Spawn(ctx, "optional-ext-ok", probe)
		require.NoError(t, err)
		require.NotNil(t, pid)
		assert.Equal(t, persistence.EventsStore(eventStore), got)

		require.NoError(t, actorSystem.Stop(ctx))
	})
}
