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
	"log/slog"
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
	kitlog "github.com/pablogore/kit-logger/pkg/logger"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/log"

	"github.com/getsyntegrity/ego/internal/goaktlog"
	"github.com/getsyntegrity/ego/testkit"
)

// newLoggerAdapter wraps a kit-logger Logger for GoAkt the same way
// Config.GoaktOptions does. The engine tests build actor systems by hand, so
// they share this one helper instead of each importing internal/goaktlog.
func newLoggerAdapter(logger kitlog.Logger) log.Logger {
	return goaktlog.New(logger)
}

// recordSink collects the messages a kit-logger built with newSinkLogger
// emits, so a test can assert on what actually reached the backend.
type recordSink struct {
	mu       sync.Mutex
	messages []string
}

func (s *recordSink) add(_ context.Context, r slog.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, r.Message)
}

func (s *recordSink) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.messages...)
}

func newSinkLogger(level kitlog.Level) (kitlog.Logger, *recordSink) {
	sink := &recordSink{}
	logger := kitlog.New(kitlog.Config{
		Level: level,
		Sink:  kitlogtest.NewTestHandler(sink.add),
	})
	return logger, sink
}

func TestResolveLogger(t *testing.T) {
	specs.Describe(t, "ResolveLogger returns a usable logger", func(s *specs.Spec) {
		s.It("nil falls back to the default", func(ctx *specs.Context) {
			ctx.Expect(ResolveLogger(nil) == DefaultLogger()).To(specs.BeTrue())
		})

		s.It("typed nil falls back to the default", func(ctx *specs.Context) {
			var typedNil *kitlogtest.MockLogger
			ctx.Expect(ResolveLogger(typedNil) == DefaultLogger()).To(specs.BeTrue())
		})

		s.It("a usable logger is returned as-is", func(ctx *specs.Context) {
			logger := kitlogtest.NewMockLogger()
			ctx.Expect(ResolveLogger(logger) == kitlog.Logger(logger)).To(specs.BeTrue())
		})
	})
}

func TestDefaultLoggerIsKitLoggerGlobal(t *testing.T) {
	specs.Describe(t, "DefaultLogger is the kit-logger global", func(s *specs.Spec) {
		s.It("follows the global, including one installed before configuring eGo", func(ctx *specs.Context) {
			ctx.Expect(kitlog.L() == DefaultLogger()).To(specs.BeTrue())

			// An application that installs its own global before configuring eGo
			// gets the engine's records through it without passing WithLogger.
			previous := kitlog.L()
			ctx.T.Cleanup(func() { kitlog.SetGlobal(previous) })

			custom := kitlogtest.NewMockLogger()
			kitlog.SetGlobal(custom)
			ctx.Expect(DefaultLogger() == kitlog.Logger(custom)).To(specs.BeTrue())
			ctx.Expect(NewConfig(nil).logger == kitlog.Logger(custom)).To(specs.BeTrue())
		})
	})
}

func TestDiscardLoggerDisablesEveryLevel(t *testing.T) {
	specs.Describe(t, "DiscardLogger disables every level", func(s *specs.Spec) {
		s.It("reports no level and drops every record without panicking", func(ctx *specs.Context) {
			adapter := newLoggerAdapter(DiscardLogger)

			ctx.Expect(adapter.LogLevel()).ToEqual(log.InvalidLevel)
			var enabled []log.Level
			for _, level := range []log.Level{
				log.DebugLevel, log.InfoLevel, log.WarningLevel,
				log.ErrorLevel, log.FatalLevel, log.PanicLevel,
			} {
				if adapter.Enabled(level) {
					enabled = append(enabled, level)
				}
			}
			ctx.Expect(enabled).To(specs.BeNil())

			// Emitting through a discarding logger must be a no-op, never a panic.
			panicked := optRecovered(func() {
				adapter.Info("dropped")
				adapter.Errorf("dropped %d", 1)
				adapter.With("k", "v").Warn("dropped")
				DiscardLogger.Error("dropped", "k", "v")
			})
			ctx.Expect(panicked).To(specs.BeNil())
		})
	})
}

// TestGoaktOptionsCarryTheResolvedLogger pins the configuration wiring: the
// logger GoAkt receives from Config.GoaktOptions is the one WithLogger
// resolved, so eGo's actors recover exactly that backend.
func TestGoaktOptionsCarryTheResolvedLogger(t *testing.T) {
	ctx := context.Background()

	backendOf := func(t *testing.T, opts ...Option) kitlog.Logger {
		t.Helper()
		cfg := NewConfig(testkit.NewEventsStore(), opts...)
		sys, err := goakt.NewActorSystem("LoggerWiring", cfg.GoaktOptions()...)
		require.NoError(t, err)
		return goaktlog.Backend(sys.Logger())
	}

	t.Run("an explicit logger", func(t *testing.T) {
		logger, _ := newSinkLogger(kitlog.LevelInfo)
		assert.Same(t, logger, backendOf(t, WithLogger(logger)))
	})

	t.Run("no logger falls back to the default", func(t *testing.T) {
		assert.Same(t, DefaultLogger(), backendOf(t))
	})

	t.Run("a nil logger falls back to the default", func(t *testing.T) {
		assert.Same(t, DefaultLogger(), backendOf(t, WithLogger(nil)))
	})

	t.Run("a typed-nil logger falls back to the default", func(t *testing.T) {
		var typedNil *kitlogtest.MockLogger
		assert.Same(t, DefaultLogger(), backendOf(t, WithLogger(typedNil)))
	})

	t.Run("records reach the backend through the running actor system", func(t *testing.T) {
		logger, sink := newSinkLogger(kitlog.LevelDebug)
		cfg := NewConfig(testkit.NewEventsStore(), WithLogger(logger))
		sys, err := goakt.NewActorSystem("LoggerPath", cfg.GoaktOptions()...)
		require.NoError(t, err)
		require.NoError(t, sys.Start(ctx))
		require.NoError(t, sys.Stop(ctx))

		messages := sink.all()
		require.NotEmpty(t, messages, "the actor system's own records must reach the kit-logger backend")
		for _, msg := range messages {
			assert.NotEmpty(t, msg)
		}
	})
}
