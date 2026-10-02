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

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"
	kitlog "github.com/pablogore/kit-logger/pkg/logger"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/log"

	"github.com/getsyntegrity/urd/eventstream"
	"github.com/getsyntegrity/urd/internal/engine/enginetest"
	"github.com/getsyntegrity/urd/internal/extensions"
	"github.com/getsyntegrity/urd/internal/goaktlog"
	"github.com/getsyntegrity/urd/persistence"
)

// discardGoaktLogger returns a GoAkt logger that drops every record, so the
// hand-built actor systems below stay quiet.
func discardGoaktLogger() log.Logger {
	return goaktlog.New(kitlog.New(kitlog.Config{Sink: slog.DiscardHandler}))
}

// startActorSystem builds and starts an in-process actor system and registers
// its shutdown with the case, so it stops even when an expectation fails.
func startActorSystem(ctx *specs.Context, name string, opts ...goakt.Option) goakt.ActorSystem {
	bg := context.Background()
	opts = append([]goakt.Option{goakt.WithLogger(discardGoaktLogger()), goakt.WithActorInitMaxRetries(1)}, opts...)
	system, err := goakt.NewActorSystem(name, opts...)
	ctx.Expect(err).To(specs.BeNil())
	ctx.Expect(system.Start(bg)).To(specs.BeNil())
	ctx.Cleanup(func() { ctx.Expect(system.Stop(bg)).To(specs.BeNil()) })
	return system
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

// expectPing declares the Ping the extension wrapper may issue on the store;
// the lookup tests do not depend on whether it happens.
func expectPing(ctrl *mock.Controller) {
	ctrl.Method("Ping").Expect(mock.Any()).Return(nil).AnyTimes()
}

// TestRequireExtension pins down Require's behavior: it must return
// a descriptive error — never panic — whenever the requested extension is
// absent or was registered under an unexpected type, and it must return the
// typed extension when the lookup succeeds. See issue #99: an unchecked
// ctx.Extension(id).(*T) type assertion in PreStart panics, and that panic
// crashes the whole process because goakt drives Spawn/SpawnChild through a
// golang.org/x/sync/singleflight.Group that deliberately re-panics on a
// fresh, unrecoverable goroutine (see extension_lookup.go).
func TestRequireExtension(t *testing.T) {
	specs.Describe(t, "Require", func(s *specs.Spec) {
		s.It("returns an error instead of panicking when the extension is absent", func(ctx *specs.Context) {
			bg := context.Background()
			system := startActorSystem(ctx, "TestRequireExtensionMissingSystem")

			probe := &requireProbeActor{
				lookup: func(ctx *goakt.Context) error {
					_, err := extensions.Require[*extensions.EventsStore](ctx, extensions.EventsStoreExtensionID)
					return err
				},
			}
			pid, err := system.Spawn(bg, "require-ext-missing", probe)

			ctx.Expect(err).To(specs.MatchError(extensions.ErrMissingRequiredExtensions))
			ctx.Expect(pid).To(specs.BeNil())
		})

		s.It("returns an error instead of panicking when the extension is registered under a mismatched type", func(ctx *specs.Context) {
			bg := context.Background()
			eventStream := eventstream.New()
			ctx.Cleanup(eventStream.Close)
			system := startActorSystem(ctx, "TestRequireExtensionMismatchSystem",
				goakt.WithExtensions(extensions.NewEventsStream(eventStream)))

			probe := &requireProbeActor{
				lookup: func(ctx *goakt.Context) error {
					// The events-stream extension is registered, but under the
					// events-store ID it is asked for here: simulates an
					// extension registered with an unexpected concrete type.
					_, err := extensions.Require[*extensions.EventsStore](ctx, extensions.EventsStreamExtensionID)
					return err
				},
			}
			pid, err := system.Spawn(bg, "require-ext-mismatch", probe)

			ctx.Expect(err).To(specs.MatchError(extensions.ErrMissingRequiredExtensions))
			ctx.Expect(err.Error()).To(specs.Contain("was registered with unexpected type"))
			ctx.Expect(pid).To(specs.BeNil())
		})

		s.It("returns the typed extension when registered correctly", func(ctx *specs.Context) {
			bg := context.Background()
			ctrl := mock.NewController(ctx)
			expectPing(ctrl)
			store := enginetest.NewEventsStoreMock(ctrl)
			system := startActorSystem(ctx, "TestRequireExtensionOKSystem",
				goakt.WithExtensions(extensions.NewEventsStore(store)))

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
			pid, err := system.Spawn(bg, "require-ext-ok", probe)

			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(pid).To(specs.Not(specs.BeNil()))
			ctx.Expect(got).To(specs.Equal(persistence.EventsStore(store)))
		})
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
	specs.Describe(t, "Optional", func(s *specs.Spec) {
		s.It("returns the zero value and no error when the extension is absent", func(ctx *specs.Context) {
			bg := context.Background()
			system := startActorSystem(ctx, "TestOptionalExtensionMissingSystem")

			var got *extensions.SnapshotStoreExt
			probe := &requireProbeActor{
				lookup: func(ctx *goakt.Context) error {
					ext, err := extensions.Optional[*extensions.SnapshotStoreExt](ctx, extensions.SnapshotStoreExtensionID)
					got = ext
					return err
				},
			}
			pid, err := system.Spawn(bg, "optional-ext-missing", probe)

			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(pid).To(specs.Not(specs.BeNil()))
			ctx.Expect(got).To(specs.BeNil())
		})

		s.It("returns an error instead of panicking when the extension is registered under a mismatched type", func(ctx *specs.Context) {
			bg := context.Background()
			eventStream := eventstream.New()
			ctx.Cleanup(eventStream.Close)
			system := startActorSystem(ctx, "TestOptionalExtensionMismatchSystem",
				goakt.WithExtensions(extensions.NewEventsStream(eventStream)))

			probe := &requireProbeActor{
				lookup: func(ctx *goakt.Context) error {
					// The events-stream extension is registered, but under the
					// snapshot-store ID it is asked for here: simulates an
					// extension registered with an unexpected concrete type.
					_, err := extensions.Optional[*extensions.SnapshotStoreExt](ctx, extensions.EventsStreamExtensionID)
					return err
				},
			}
			pid, err := system.Spawn(bg, "optional-ext-mismatch", probe)

			ctx.Expect(err).To(specs.MatchError(extensions.ErrMissingRequiredExtensions))
			ctx.Expect(err.Error()).To(specs.Contain("was registered with unexpected type"))
			ctx.Expect(pid).To(specs.BeNil())
		})

		s.It("returns the typed extension when registered correctly", func(ctx *specs.Context) {
			bg := context.Background()
			ctrl := mock.NewController(ctx)
			expectPing(ctrl)
			store := enginetest.NewEventsStoreMock(ctrl)
			system := startActorSystem(ctx, "TestOptionalExtensionOKSystem",
				goakt.WithExtensions(extensions.NewEventsStore(store)))

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
			pid, err := system.Spawn(bg, "optional-ext-ok", probe)

			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(pid).To(specs.Not(specs.BeNil()))
			ctx.Expect(got).To(specs.Equal(persistence.EventsStore(store)))
		})
	})
}
