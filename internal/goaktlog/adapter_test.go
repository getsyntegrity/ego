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

package goaktlog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"
	kitlog "github.com/pablogore/kit-logger/pkg/logger"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
	"github.com/tochemey/goakt/v4/log"

	"github.com/getsyntegrity/urd/internal/logging"
)

// capturedRecord is one record that reached the kit-logger sink.
type capturedRecord struct {
	ctx   context.Context
	level slog.Level
	msg   string
	attrs map[string]any
}

// capture collects every record a kit-logger built with newCaptureLogger
// emits, so a test can assert on what actually reached the backend.
type capture struct {
	mu      sync.Mutex
	records []capturedRecord
}

func (c *capture) add(ctx context.Context, r slog.Record) {
	attrs := make(map[string]any, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, capturedRecord{ctx: ctx, level: r.Level, msg: r.Message, attrs: attrs})
}

func (c *capture) all() []capturedRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]capturedRecord(nil), c.records...)
}

// last returns the most recent record. The case fails through the spec, not
// through t.Fatalf, when no record reached the backend.
func (c *capture) last(ctx *specs.Context) capturedRecord {
	records := c.all()
	ctx.Expect(records).To(specs.Not(specs.BeEmpty()))
	return records[len(records)-1]
}

// newCaptureLogger builds a real kit-logger whose sink records into a capture,
// so the adapter is exercised against the genuine backend pipeline — level
// gate, With, context propagation — rather than a hand-rolled fake.
func newCaptureLogger(level kitlog.Level) (kitlog.Logger, *capture) {
	c := &capture{}
	logger := kitlog.New(kitlog.Config{
		Level: level,
		Sink:  kitlogtest.NewTestHandler(c.add),
	})
	return logger, c
}

type ctxKey struct{}

// stringerMock is a fmt.Stringer backed by a mock controller. A case declares
// how often fmt may ask for the string form, so it can prove that a
// printf-style call skipped formatting when the level was disabled.
type stringerMock struct{ c *mock.Controller }

func (m stringerMock) String() string {
	return mock.Value[string](m.c.Method("String").Call(), 0)
}

// newStringer returns a stringer whose String call is expected exactly times
// times; the controller verifies the count when the case ends.
func newStringer(ctx *specs.Context, times int) stringerMock {
	ctrl := mock.NewController(ctx)
	ctrl.Method("String").Expect().Times(times).Return("formatted")
	return stringerMock{ctrl}
}

// managedBackend is a kit-logger with an explicit lifecycle whose Flush and
// Shutdown are forwarded to a mock controller. The embedded logger is a real
// discarding one, so the adapter's constructor works as it does in production.
type managedBackend struct {
	kitlog.Logger
	c *mock.Controller
}

func (m managedBackend) Flush(ctx context.Context) error {
	return m.c.Method("Flush").Call(ctx).Err(0)
}

func (m managedBackend) Shutdown(ctx context.Context) error {
	return m.c.Method("Shutdown").Call(ctx).Err(0)
}

func newManagedBackend(ctx *specs.Context) (managedBackend, *mock.Controller) {
	ctrl := mock.NewController(ctx)
	discard := kitlog.New(kitlog.Config{Sink: slog.DiscardHandler})
	return managedBackend{Logger: discard, c: ctrl}, ctrl
}

// beTheSameInstance matches a value that is the very instance want. ToEqual
// compares deeply, so it would also accept a distinct but equal value.
func beTheSameInstance(want any) specs.Matcher {
	return specs.Satisfy("be the same instance", func(got any) bool { return got == want })
}

// beAnAdapter matches a logger that is eGo's *adapter.
func beAnAdapter() specs.Matcher {
	return specs.Satisfy("be an *adapter", func(got any) bool {
		_, ok := got.(*adapter)
		return ok
	})
}

// panicked reports whether fn panics.
func panicked(fn func()) (didPanic bool) {
	defer func() { didPanic = recover() != nil }()
	fn()
	return false
}

// field is one key/value pair a record must carry.
type field struct {
	key string
	val any
}

type levelCase struct {
	name       string
	level      slog.Level
	call       func(a *adapter)
	msg        string
	wants      []field
	carriesCtx bool
}

type stringerCase struct {
	name string
	call func(a *adapter, s fmt.Stringer)
}

func TestLoggerAdapterRoutesEveryLevel(t *testing.T) {
	specs.Describe(t, "the adapter routes every GoAkt logging method to the matching backend level", func(s *specs.Spec) {
		carried := context.WithValue(context.Background(), ctxKey{}, "carried")

		tests := []levelCase{
			{"Debug", slog.LevelDebug, func(a *adapter) { a.Debug("msg", "k", "v") }, "msg", []field{{"k", "v"}}, false},
			{"Debugf", slog.LevelDebug, func(a *adapter) { a.Debugf("n=%d", 1) }, "n=1", nil, false},
			{"DebugContext", slog.LevelDebug, func(a *adapter) { a.DebugContext(carried, "msg", "k", "v") }, "msg", []field{{"k", "v"}}, true},
			{"DebugfContext", slog.LevelDebug, func(a *adapter) { a.DebugfContext(carried, "n=%d", 1) }, "n=1", nil, true},
			{"Info", slog.LevelInfo, func(a *adapter) { a.Info("msg", "k", "v") }, "msg", []field{{"k", "v"}}, false},
			{"Infof", slog.LevelInfo, func(a *adapter) { a.Infof("n=%d", 1) }, "n=1", nil, false},
			{"InfoContext", slog.LevelInfo, func(a *adapter) { a.InfoContext(carried, "msg", "k", "v") }, "msg", []field{{"k", "v"}}, true},
			{"InfofContext", slog.LevelInfo, func(a *adapter) { a.InfofContext(carried, "n=%d", 1) }, "n=1", nil, true},
			{"Warn", slog.LevelWarn, func(a *adapter) { a.Warn("msg", "k", "v") }, "msg", []field{{"k", "v"}}, false},
			{"Warnf", slog.LevelWarn, func(a *adapter) { a.Warnf("n=%d", 1) }, "n=1", nil, false},
			{"WarnContext", slog.LevelWarn, func(a *adapter) { a.WarnContext(carried, "msg", "k", "v") }, "msg", []field{{"k", "v"}}, true},
			{"WarnfContext", slog.LevelWarn, func(a *adapter) { a.WarnfContext(carried, "n=%d", 1) }, "n=1", nil, true},
			{"Error", slog.LevelError, func(a *adapter) { a.Error("msg", "k", "v") }, "msg", []field{{"k", "v"}}, false},
			{"Errorf", slog.LevelError, func(a *adapter) { a.Errorf("n=%d", 1) }, "n=1", nil, false},
			{"ErrorContext", slog.LevelError, func(a *adapter) { a.ErrorContext(carried, "msg", "k", "v") }, "msg", []field{{"k", "v"}}, true},
			{"ErrorfContext", slog.LevelError, func(a *adapter) { a.ErrorfContext(carried, "n=%d", 1) }, "n=1", nil, true},
		}

		specs.Table(s, tests, func(tt levelCase) string { return tt.name }, func(ctx *specs.Context, tt levelCase) {
			logger, sink := newCaptureLogger(kitlog.LevelDebug)
			tt.call(newAdapter(logger))

			got := sink.last(ctx)
			ctx.Expect(got.level).ToEqual(tt.level)
			ctx.Expect(got.msg).ToEqual(tt.msg)
			for _, f := range tt.wants {
				ctx.Expect(got.attrs).To(specs.HavePair(f.key, f.val))
			}
			if tt.carriesCtx {
				// The caller's context must reach the backend verbatim.
				ctx.Expect(got.ctx.Value(ctxKey{})).ToEqual("carried")
			}
		})
	})
}

func TestLoggerAdapterNonStringFirstArgumentBecomesTheMessage(t *testing.T) {
	specs.Describe(t, "a non-string first argument becomes the record message", func(s *specs.Spec) {
		s.It("uses the error text as the message", func(ctx *specs.Context) {
			logger, sink := newCaptureLogger(kitlog.LevelDebug)
			newAdapter(logger).Error(errors.New("boom"))
			ctx.Expect(sink.last(ctx).msg).ToEqual("boom")
		})
	})
}

func TestLoggerAdapterFormattedMethodsSkipFormattingWhenDisabled(t *testing.T) {
	specs.Describe(t, "printf-style methods skip formatting when the level is disabled", func(s *specs.Spec) {
		var (
			sink *capture
			a    *adapter
		)

		// Shared setup: each case gets its own backend that only emits errors.
		s.BeforeEach(func(ctx *specs.Context) {
			var logger kitlog.Logger
			logger, sink = newCaptureLogger(kitlog.LevelError)
			a = newAdapter(logger)
		})

		calls := []stringerCase{
			{"Debugf", func(a *adapter, s fmt.Stringer) { a.Debugf("%s", s) }},
			{"DebugfContext", func(a *adapter, s fmt.Stringer) { a.DebugfContext(context.Background(), "%s", s) }},
			{"Infof", func(a *adapter, s fmt.Stringer) { a.Infof("%s", s) }},
			{"InfofContext", func(a *adapter, s fmt.Stringer) { a.InfofContext(context.Background(), "%s", s) }},
			{"Warnf", func(a *adapter, s fmt.Stringer) { a.Warnf("%s", s) }},
			{"WarnfContext", func(a *adapter, s fmt.Stringer) { a.WarnfContext(context.Background(), "%s", s) }},
		}

		// A disabled record must not be formatted: the stringer expects no call.
		specs.Table(s, calls, func(c stringerCase) string { return c.name }, func(ctx *specs.Context, c stringerCase) {
			c.call(a, newStringer(ctx, 0))
		})

		s.It("drops every disabled record", func(ctx *specs.Context) {
			stringer := newStringer(ctx, 0)
			for _, c := range calls {
				c.call(a, stringer)
			}
			ctx.Expect(sink.all()).To(specs.BeEmpty())
		})

		s.It("formats and emits an enabled level", func(ctx *specs.Context) {
			a.Errorf("%s", newStringer(ctx, 1))
			ctx.Expect(sink.last(ctx).msg).ToEqual("formatted")
		})
	})
}

func TestLoggerAdapterWithBuildsTheChildInTheBackend(t *testing.T) {
	specs.Describe(t, "With builds the child logger in the backend", func(s *specs.Spec) {
		s.It("adds fields to the child only and accumulates them through chained calls", func(ctx *specs.Context) {
			logger, sink := newCaptureLogger(kitlog.LevelDebug)
			parent := newAdapter(logger)

			child := parent.With("subsystem", "engine")
			ctx.Expect(child).To(beAnAdapter())
			ctx.Expect(child).To(specs.Not(beTheSameInstance(log.Logger(parent))))

			child.Info("child record", "k", "v")
			got := sink.last(ctx)
			ctx.Expect(got.attrs).To(specs.HavePair("subsystem", "engine"))
			ctx.Expect(got.attrs).To(specs.HavePair("k", "v"))

			parent.Info("parent record")
			// The parent must not inherit the child's fields.
			ctx.Expect(sink.last(ctx).attrs).To(specs.Not(specs.HaveKey("subsystem")))

			grandchild := child.With("entity_id", "42")
			grandchild.Info("grandchild record")
			got = sink.last(ctx)
			// Chained With calls accumulate.
			ctx.Expect(got.attrs).To(specs.HavePair("subsystem", "engine"))
			ctx.Expect(got.attrs).To(specs.HavePair("entity_id", "42"))

			// With without fields returns the same adapter.
			ctx.Expect(parent.With()).To(beTheSameInstance(log.Logger(parent)))
		})
	})
}

func TestLoggerAdapterLevelTracksTheBackendAtRuntime(t *testing.T) {
	specs.Describe(t, "LogLevel and Enabled follow the backend level at runtime", func(s *specs.Spec) {
		s.It("reflects the initial level and every later SetLevel", func(ctx *specs.Context) {
			logger, _ := newCaptureLogger(kitlog.LevelInfo)
			a := newAdapter(logger)

			ctx.Expect(a.LogLevel()).ToEqual(log.InfoLevel)
			ctx.Expect(a.Enabled(log.DebugLevel)).To(specs.BeFalse())
			ctx.Expect(a.Enabled(log.InfoLevel)).To(specs.BeTrue())
			ctx.Expect(a.Enabled(log.ErrorLevel)).To(specs.BeTrue())

			// A runtime SetLevel is honored on the next check.
			logger.SetLevel(slog.LevelDebug)
			ctx.Expect(a.LogLevel()).ToEqual(log.DebugLevel)
			ctx.Expect(a.Enabled(log.DebugLevel)).To(specs.BeTrue())

			logger.SetLevel(slog.LevelError)
			ctx.Expect(a.LogLevel()).ToEqual(log.ErrorLevel)
			ctx.Expect(a.Enabled(log.WarningLevel)).To(specs.BeFalse())
			ctx.Expect(a.Enabled(log.FatalLevel)).To(specs.BeTrue())
			ctx.Expect(a.Enabled(log.PanicLevel)).To(specs.BeTrue())
		})
	})
}

func TestDiscardingBackendDisablesEveryLevel(t *testing.T) {
	specs.Describe(t, "an adapter over a discarding backend disables every level", func(s *specs.Spec) {
		s.It("reports the invalid level and no level enabled", func(ctx *specs.Context) {
			// Built the same way as engine.DiscardLogger; the engine keeps its own
			// test against the exported variable itself.
			discard := kitlog.New(kitlog.Config{Sink: slog.DiscardHandler})
			a := newAdapter(discard)

			ctx.Expect(a.LogLevel()).ToEqual(log.InvalidLevel)
			var enabled []log.Level
			for _, level := range goaktLevelsByVerbosity {
				if a.Enabled(level) {
					enabled = append(enabled, level)
				}
			}
			ctx.Expect(enabled).To(specs.BeNil())
		})

		s.It("emitting through a discarding logger never panics", func(ctx *specs.Context) {
			discard := kitlog.New(kitlog.Config{Sink: slog.DiscardHandler})
			a := newAdapter(discard)

			// Emitting through a discarding logger must be a no-op, never a panic.
			ctx.Expect(panicked(func() {
				a.Info("dropped")
				a.Errorf("dropped %d", 1)
				a.With("k", "v").Warn("dropped")
				discard.Error("dropped", "k", "v")
			})).To(specs.BeFalse())
		})
	})
}

func TestGoaktToSlogLevel(t *testing.T) {
	specs.Describe(t, "goaktToSlogLevel maps GoAkt levels to slog levels", func(s *specs.Spec) {
		type levelMapping struct {
			in   log.Level
			want slog.Level
		}
		tests := []levelMapping{
			{log.DebugLevel, slog.LevelDebug},
			{log.InfoLevel, slog.LevelInfo},
			{log.WarningLevel, slog.LevelWarn},
			{log.ErrorLevel, slog.LevelError},
			{log.FatalLevel, slog.LevelError + 4},
			{log.PanicLevel, slog.LevelError + 8},
			{log.InvalidLevel, slog.LevelInfo},
		}
		specs.Table(s, tests, func(tt levelMapping) string { return fmt.Sprintf("level %v", tt.in) },
			func(ctx *specs.Context, tt levelMapping) {
				ctx.Expect(goaktToSlogLevel(tt.in)).ToEqual(tt.want)
			})

		s.It("is strictly monotonic in severity", func(ctx *specs.Context) {
			// LogLevel probes levels in verbosity order and stops at the first
			// enabled one, so the mapping must never repeat or go backwards.
			var notGreater []log.Level
			previous := goaktToSlogLevel(goaktLevelsByVerbosity[0])
			for _, level := range goaktLevelsByVerbosity[1:] {
				current := goaktToSlogLevel(level)
				if current <= previous {
					notGreater = append(notGreater, level)
				}
				previous = current
			}
			ctx.Expect(notGreater).To(specs.BeNil())
		})
	})
}

func TestGoaktArgsToMsg(t *testing.T) {
	specs.Describe(t, "goaktArgsToMsg splits GoAkt arguments into a message and fields", func(s *specs.Spec) {
		type argsCase struct {
			name       string
			args       []any
			wantMsg    string
			wantFields []any
		}
		tests := []argsCase{
			{"no args", nil, "", nil},
			{"string only", []any{"hello"}, "hello", nil},
			{"string with fields", []any{"hello", "k", "v"}, "hello", []any{"k", "v"}},
			{"non-string first arg", []any{42}, "42", nil},
			{"error first arg", []any{errors.New("boom"), "k", "v"}, "boom", []any{"k", "v"}},
		}
		specs.Table(s, tests, func(tt argsCase) string { return tt.name }, func(ctx *specs.Context, tt argsCase) {
			msg, fields := goaktArgsToMsg(tt.args)
			ctx.Expect(msg).ToEqual(tt.wantMsg)
			ctx.Expect(fields).ToEqual(tt.wantFields)
		})
	})
}

func TestLoggerAdapterFlush(t *testing.T) {
	specs.Describe(t, "Flush drains the backend without shutting it down", func(s *specs.Spec) {
		s.It("forwards to a managed backend", func(ctx *specs.Context) {
			backend, ctrl := newManagedBackend(ctx)
			ctrl.Method("Flush").Expect(mock.Any()).Times(1).Return(nil)
			// GoAkt's flush must never shut the application's logger down.
			ctrl.Method("Shutdown").Expect(mock.Any()).Never()

			ctx.Expect(newAdapter(backend).Flush()).To(specs.BeNil())
		})

		s.It("reports the backend's error", func(ctx *specs.Context) {
			backend, ctrl := newManagedBackend(ctx)
			flushErr := errors.New("flush failed")
			ctrl.Method("Flush").Expect(mock.Any()).Times(1).Return(flushErr)

			ctx.Expect(newAdapter(backend).Flush()).To(specs.MatchError(flushErr))
		})

		s.It("a real logger without a buffer flushes immediately", func(ctx *specs.Context) {
			logger, _ := newCaptureLogger(kitlog.LevelInfo)
			ctx.Expect(newAdapter(logger).Flush()).To(specs.BeNil())
		})
	})
}

func TestLoggerAdapterStdLogger(t *testing.T) {
	specs.Describe(t, "StdLogger bridges the standard library logger to the backend", func(s *specs.Spec) {
		s.It("emits standard library lines at info level without the trailing newline", func(ctx *specs.Context) {
			logger, sink := newCaptureLogger(kitlog.LevelInfo)
			std := newAdapter(logger).StdLogger()
			ctx.Expect(std).To(specs.Not(specs.BeNil()))

			std.Println("from the standard library")
			got := sink.last(ctx)
			ctx.Expect(got.level).ToEqual(slog.LevelInfo)
			ctx.Expect(got.msg).ToEqual("from the standard library")
		})
	})
}

func TestLoggerWriterTrimsLineEndings(t *testing.T) {
	specs.Describe(t, "loggerWriter trims the line ending before emitting", func(s *specs.Spec) {
		type lineCase struct {
			name string
			in   string
			want string
		}
		tests := []lineCase{
			{"newline", "line\n", "line"},
			{"crlf", "line\r\n", "line"},
			{"none", "line", "line"},
		}
		specs.Table(s, tests, func(tt lineCase) string { return tt.name }, func(ctx *specs.Context, tt lineCase) {
			logger, sink := newCaptureLogger(kitlog.LevelInfo)
			n, err := (&loggerWriter{inner: logger}).Write([]byte(tt.in))
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(n).ToEqual(len(tt.in))
			ctx.Expect(sink.last(ctx).msg).ToEqual(tt.want)
		})
	})
}

func TestNewWrapsTheBackendInAnAdapter(t *testing.T) {
	specs.Describe(t, "New wraps a kit-logger backend in an adapter", func(s *specs.Spec) {
		s.It("returns an adapter that emits through the backend", func(ctx *specs.Context) {
			logger, sink := newCaptureLogger(kitlog.LevelInfo)
			wrapped := New(logger)
			ctx.Expect(wrapped).To(beAnAdapter())

			wrapped.Info("through New", "k", "v")
			got := sink.last(ctx)
			ctx.Expect(got.msg).ToEqual("through New")
			ctx.Expect(got.attrs).To(specs.HavePair("k", "v"))
		})
	})
}

func TestBackend(t *testing.T) {
	specs.Describe(t, "Backend recovers the kit-logger behind a GoAkt logger", func(s *specs.Spec) {
		s.It("recovers the backend behind eGo's adapter", func(ctx *specs.Context) {
			logger, _ := newCaptureLogger(kitlog.LevelInfo)
			ctx.Expect(Backend(New(logger))).To(beTheSameInstance(logger))
		})

		s.It("recovers the backend behind a child adapter", func(ctx *specs.Context) {
			logger, sink := newCaptureLogger(kitlog.LevelInfo)
			child := newAdapter(logger).With("subsystem", "engine")
			Backend(child).Info("through the child")
			ctx.Expect(sink.last(ctx).attrs).To(specs.HavePair("subsystem", "engine"))
		})

		s.It("falls back to the default for a foreign GoAkt logger", func(ctx *specs.Context) {
			ctx.Expect(Backend(log.DiscardLogger)).To(beTheSameInstance(logging.DefaultLogger()))
		})
	})
}

// sourceOf returns the file and function of the "source" group kit-logger
// attaches when AddSource is set, or empty strings when the record has none.
func sourceOf(r capturedRecord) (file, function string) {
	group, ok := r.attrs[slog.SourceKey].([]slog.Attr)
	if !ok {
		return "", ""
	}
	for _, a := range group {
		switch a.Key {
		case "file":
			file = a.Value.String()
		case "function":
			function = a.Value.String()
		}
	}
	return file, function
}

func newSourceCaptureLogger() (kitlog.Logger, *capture) {
	c := &capture{}
	logger := kitlog.New(kitlog.Config{
		Level:     kitlog.LevelDebug,
		AddSource: true,
		Sink:      kitlogtest.NewTestHandler(c.add),
	})
	return logger, c
}

func TestLoggerAdapterAttributesRecordsToTheGoaktCallSite(t *testing.T) {
	specs.Describe(t, "records are attributed to the GoAkt call site, never to the adapter", func(s *specs.Spec) {
		var (
			sink *capture
			a    *adapter
		)

		// Shared setup: a source-capturing backend per case.
		s.BeforeEach(func(ctx *specs.Context) {
			var logger kitlog.Logger
			logger, sink = newSourceCaptureLogger()
			a = newAdapter(logger)
		})

		type callCase struct {
			name string
			call func()
		}
		calls := []callCase{
			{"Info", func() { a.Info("msg") }},
			{"Infof", func() { a.Infof("msg %d", 1) }},
			{"ErrorContext", func() { a.ErrorContext(context.Background(), "msg") }},
			{"With", func() { a.With("k", "v").Warn("msg") }},
		}
		specs.Table(s, calls, func(c callCase) string { return c.name }, func(ctx *specs.Context, c callCase) {
			c.call()
			file, function := sourceOf(sink.last(ctx))
			// The record must name the caller of the adapter.
			ctx.Expect(file).To(specs.EndWith("adapter_test.go"))
			// The adapter must never be the attributed frame.
			ctx.Expect(function).To(specs.Not(specs.Contain("(*adapter)")))
		})
	})
}

func TestBackendAttributesRecordsToItsDirectCaller(t *testing.T) {
	specs.Describe(t, "Backend attributes records to its direct caller", func(s *specs.Spec) {
		s.It("skips no frame for the recovered backend", func(ctx *specs.Context) {
			logger, sink := newSourceCaptureLogger()

			// eGo's own actors call the recovered backend directly, so no frame must
			// be skipped for them: a skip would blame whoever called the actor.
			Backend(newAdapter(logger)).Error("msg")
			file, function := sourceOf(sink.last(ctx))
			ctx.Expect(file).To(specs.EndWith("adapter_test.go"))
			ctx.Expect(function).To(specs.Contain("TestBackendAttributesRecordsToItsDirectCaller"))
		})
	})
}
