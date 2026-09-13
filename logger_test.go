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
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	kitlog "github.com/pablogore/kit-logger/pkg/logger"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/log"

	"github.com/tochemey/ego/v4/testkit"
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

func (c *capture) last(t *testing.T) capturedRecord {
	t.Helper()
	records := c.all()
	require.NotEmpty(t, records, "no record reached the backend")
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

// stringerSpy records whether fmt ever asked for its string form. It proves
// that a printf-style call skipped formatting when the level was disabled.
type stringerSpy struct{ called bool }

func (s *stringerSpy) String() string {
	s.called = true
	return "formatted"
}

func TestLoggerAdapterRoutesEveryLevel(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKey{}, "carried")

	tests := []struct {
		name  string
		level slog.Level
		call  func(a *loggerAdapter)
		msg   string
		wants map[string]any
		ctx   bool
	}{
		{"Debug", slog.LevelDebug, func(a *loggerAdapter) { a.Debug("msg", "k", "v") }, "msg", map[string]any{"k": "v"}, false},
		{"Debugf", slog.LevelDebug, func(a *loggerAdapter) { a.Debugf("n=%d", 1) }, "n=1", nil, false},
		{"DebugContext", slog.LevelDebug, func(a *loggerAdapter) { a.DebugContext(ctx, "msg", "k", "v") }, "msg", map[string]any{"k": "v"}, true},
		{"DebugfContext", slog.LevelDebug, func(a *loggerAdapter) { a.DebugfContext(ctx, "n=%d", 1) }, "n=1", nil, true},
		{"Info", slog.LevelInfo, func(a *loggerAdapter) { a.Info("msg", "k", "v") }, "msg", map[string]any{"k": "v"}, false},
		{"Infof", slog.LevelInfo, func(a *loggerAdapter) { a.Infof("n=%d", 1) }, "n=1", nil, false},
		{"InfoContext", slog.LevelInfo, func(a *loggerAdapter) { a.InfoContext(ctx, "msg", "k", "v") }, "msg", map[string]any{"k": "v"}, true},
		{"InfofContext", slog.LevelInfo, func(a *loggerAdapter) { a.InfofContext(ctx, "n=%d", 1) }, "n=1", nil, true},
		{"Warn", slog.LevelWarn, func(a *loggerAdapter) { a.Warn("msg", "k", "v") }, "msg", map[string]any{"k": "v"}, false},
		{"Warnf", slog.LevelWarn, func(a *loggerAdapter) { a.Warnf("n=%d", 1) }, "n=1", nil, false},
		{"WarnContext", slog.LevelWarn, func(a *loggerAdapter) { a.WarnContext(ctx, "msg", "k", "v") }, "msg", map[string]any{"k": "v"}, true},
		{"WarnfContext", slog.LevelWarn, func(a *loggerAdapter) { a.WarnfContext(ctx, "n=%d", 1) }, "n=1", nil, true},
		{"Error", slog.LevelError, func(a *loggerAdapter) { a.Error("msg", "k", "v") }, "msg", map[string]any{"k": "v"}, false},
		{"Errorf", slog.LevelError, func(a *loggerAdapter) { a.Errorf("n=%d", 1) }, "n=1", nil, false},
		{"ErrorContext", slog.LevelError, func(a *loggerAdapter) { a.ErrorContext(ctx, "msg", "k", "v") }, "msg", map[string]any{"k": "v"}, true},
		{"ErrorfContext", slog.LevelError, func(a *loggerAdapter) { a.ErrorfContext(ctx, "n=%d", 1) }, "n=1", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, sink := newCaptureLogger(kitlog.LevelDebug)
			tt.call(newLoggerAdapter(logger))

			got := sink.last(t)
			assert.Equal(t, tt.level, got.level)
			assert.Equal(t, tt.msg, got.msg)
			for k, v := range tt.wants {
				assert.Equal(t, v, got.attrs[k], "field %q", k)
			}
			if tt.ctx {
				assert.Equal(t, "carried", got.ctx.Value(ctxKey{}), "the caller's context must reach the backend verbatim")
			}
		})
	}
}

func TestLoggerAdapterNonStringFirstArgumentBecomesTheMessage(t *testing.T) {
	logger, sink := newCaptureLogger(kitlog.LevelDebug)
	newLoggerAdapter(logger).Error(errors.New("boom"))
	assert.Equal(t, "boom", sink.last(t).msg)
}

func TestLoggerAdapterFormattedMethodsSkipFormattingWhenDisabled(t *testing.T) {
	logger, sink := newCaptureLogger(kitlog.LevelError)
	adapter := newLoggerAdapter(logger)
	ctx := context.Background()

	calls := []struct {
		name string
		call func(spy *stringerSpy)
	}{
		{"Debugf", func(spy *stringerSpy) { adapter.Debugf("%s", spy) }},
		{"DebugfContext", func(spy *stringerSpy) { adapter.DebugfContext(ctx, "%s", spy) }},
		{"Infof", func(spy *stringerSpy) { adapter.Infof("%s", spy) }},
		{"InfofContext", func(spy *stringerSpy) { adapter.InfofContext(ctx, "%s", spy) }},
		{"Warnf", func(spy *stringerSpy) { adapter.Warnf("%s", spy) }},
		{"WarnfContext", func(spy *stringerSpy) { adapter.WarnfContext(ctx, "%s", spy) }},
	}
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			spy := &stringerSpy{}
			c.call(spy)
			assert.False(t, spy.called, "%s must not format a record the backend will drop", c.name)
		})
	}
	assert.Empty(t, sink.all())

	spy := &stringerSpy{}
	adapter.Errorf("%s", spy)
	assert.True(t, spy.called, "an enabled level is formatted and emitted")
	assert.Equal(t, "formatted", sink.last(t).msg)
}

func TestLoggerAdapterWithBuildsTheChildInTheBackend(t *testing.T) {
	logger, sink := newCaptureLogger(kitlog.LevelDebug)
	parent := newLoggerAdapter(logger)

	child := parent.With("subsystem", "engine")
	require.IsType(t, &loggerAdapter{}, child)
	assert.NotSame(t, parent, child)

	child.Info("child record", "k", "v")
	got := sink.last(t)
	assert.Equal(t, "engine", got.attrs["subsystem"])
	assert.Equal(t, "v", got.attrs["k"])

	parent.Info("parent record")
	_, hasSubsystem := sink.last(t).attrs["subsystem"]
	assert.False(t, hasSubsystem, "the parent must not inherit the child's fields")

	grandchild := child.With("entity_id", "42")
	grandchild.Info("grandchild record")
	got = sink.last(t)
	assert.Equal(t, "engine", got.attrs["subsystem"], "chained With calls accumulate")
	assert.Equal(t, "42", got.attrs["entity_id"])

	assert.Same(t, parent, parent.With(), "With without fields returns the same adapter")
}

func TestLoggerAdapterLevelTracksTheBackendAtRuntime(t *testing.T) {
	logger, _ := newCaptureLogger(kitlog.LevelInfo)
	adapter := newLoggerAdapter(logger)

	assert.Equal(t, log.InfoLevel, adapter.LogLevel())
	assert.False(t, adapter.Enabled(log.DebugLevel))
	assert.True(t, adapter.Enabled(log.InfoLevel))
	assert.True(t, adapter.Enabled(log.ErrorLevel))

	logger.SetLevel(slog.LevelDebug)
	assert.Equal(t, log.DebugLevel, adapter.LogLevel(), "a runtime SetLevel is honored on the next check")
	assert.True(t, adapter.Enabled(log.DebugLevel))

	logger.SetLevel(slog.LevelError)
	assert.Equal(t, log.ErrorLevel, adapter.LogLevel())
	assert.False(t, adapter.Enabled(log.WarningLevel))
	assert.True(t, adapter.Enabled(log.FatalLevel))
	assert.True(t, adapter.Enabled(log.PanicLevel))
}

func TestDiscardLoggerDisablesEveryLevel(t *testing.T) {
	adapter := newLoggerAdapter(DiscardLogger)

	assert.Equal(t, log.InvalidLevel, adapter.LogLevel())
	for _, level := range goaktLevelsByVerbosity {
		assert.False(t, adapter.Enabled(level), "level %v must be disabled", level)
	}

	// Emitting through a discarding logger must be a no-op, never a panic.
	require.NotPanics(t, func() {
		adapter.Info("dropped")
		adapter.Errorf("dropped %d", 1)
		adapter.With("k", "v").Warn("dropped")
		DiscardLogger.Error("dropped", "k", "v")
	})
}

func TestGoaktToSlogLevel(t *testing.T) {
	tests := []struct {
		in   log.Level
		want slog.Level
	}{
		{log.DebugLevel, slog.LevelDebug},
		{log.InfoLevel, slog.LevelInfo},
		{log.WarningLevel, slog.LevelWarn},
		{log.ErrorLevel, slog.LevelError},
		{log.FatalLevel, slog.LevelError + 4},
		{log.PanicLevel, slog.LevelError + 8},
		{log.InvalidLevel, slog.LevelInfo},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, goaktToSlogLevel(tt.in), "level %v", tt.in)
	}

	// The mapping must be strictly monotonic in severity so LogLevel can
	// probe levels in verbosity order and stop at the first enabled one.
	previous := goaktToSlogLevel(goaktLevelsByVerbosity[0])
	for _, level := range goaktLevelsByVerbosity[1:] {
		current := goaktToSlogLevel(level)
		assert.Greater(t, current, previous)
		previous = current
	}
}

func TestGoaktArgsToMsg(t *testing.T) {
	tests := []struct {
		name       string
		args       []any
		wantMsg    string
		wantFields []any
	}{
		{"no args", nil, "", nil},
		{"string only", []any{"hello"}, "hello", nil},
		{"string with fields", []any{"hello", "k", "v"}, "hello", []any{"k", "v"}},
		{"non-string first arg", []any{42}, "42", nil},
		{"error first arg", []any{errors.New("boom"), "k", "v"}, "boom", []any{"k", "v"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, fields := goaktArgsToMsg(tt.args)
			assert.Equal(t, tt.wantMsg, msg)
			assert.Equal(t, tt.wantFields, fields)
		})
	}
}

func TestLoggerAdapterFlush(t *testing.T) {
	t.Run("forwards to a managed backend", func(t *testing.T) {
		mock := kitlogtest.NewMockLogger()
		require.NoError(t, newLoggerAdapter(mock).Flush())
		assert.Equal(t, 1, mock.FlushCalls())
		assert.Equal(t, 0, mock.ShutdownCalls(), "GoAkt's flush must never shut the application's logger down")
	})

	t.Run("reports the backend's error", func(t *testing.T) {
		mock := kitlogtest.NewMockLogger()
		mock.FlushErr = errors.New("flush failed")
		assert.ErrorIs(t, newLoggerAdapter(mock).Flush(), mock.FlushErr)
	})

	t.Run("a real logger without a buffer flushes immediately", func(t *testing.T) {
		logger, _ := newCaptureLogger(kitlog.LevelInfo)
		require.NoError(t, newLoggerAdapter(logger).Flush())
	})
}

func TestLoggerAdapterStdLogger(t *testing.T) {
	logger, sink := newCaptureLogger(kitlog.LevelInfo)
	std := newLoggerAdapter(logger).StdLogger()
	require.NotNil(t, std)

	std.Println("from the standard library")
	got := sink.last(t)
	assert.Equal(t, slog.LevelInfo, got.level)
	assert.Equal(t, "from the standard library", got.msg, "the trailing newline is trimmed")
}

func TestLoggerWriterTrimsLineEndings(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"newline", "line\n", "line"},
		{"crlf", "line\r\n", "line"},
		{"none", "line", "line"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, sink := newCaptureLogger(kitlog.LevelInfo)
			n, err := (&loggerWriter{inner: logger}).Write([]byte(tt.in))
			require.NoError(t, err)
			assert.Equal(t, len(tt.in), n)
			assert.Equal(t, tt.want, sink.last(t).msg)
		})
	}
}

func TestResolveLogger(t *testing.T) {
	t.Run("nil falls back to the default", func(t *testing.T) {
		assert.Same(t, DefaultLogger(), ResolveLogger(nil))
	})

	t.Run("typed nil falls back to the default", func(t *testing.T) {
		var typedNil *kitlogtest.MockLogger
		assert.Same(t, DefaultLogger(), ResolveLogger(typedNil))
	})

	t.Run("a usable logger is returned as-is", func(t *testing.T) {
		logger := kitlogtest.NewMockLogger()
		assert.Same(t, logger, ResolveLogger(logger))
	})
}

func TestDefaultLoggerIsKitLoggerGlobal(t *testing.T) {
	assert.Same(t, kitlog.L(), DefaultLogger())

	// An application that installs its own global before configuring eGo
	// gets the engine's records through it without passing WithLogger.
	previous := kitlog.L()
	t.Cleanup(func() { kitlog.SetGlobal(previous) })

	custom := kitlogtest.NewMockLogger()
	kitlog.SetGlobal(custom)
	assert.Same(t, custom, DefaultLogger())
	assert.Same(t, custom, NewConfig(nil).logger)
}

func TestKitLoggerFrom(t *testing.T) {
	t.Run("recovers the backend behind eGo's adapter", func(t *testing.T) {
		logger, _ := newCaptureLogger(kitlog.LevelInfo)
		assert.Same(t, logger, kitLoggerFrom(newLoggerAdapter(logger)))
	})

	t.Run("recovers the backend behind a child adapter", func(t *testing.T) {
		logger, sink := newCaptureLogger(kitlog.LevelInfo)
		child := newLoggerAdapter(logger).With("subsystem", "engine")
		kitLoggerFrom(child).Info("through the child")
		assert.Equal(t, "engine", sink.last(t).attrs["subsystem"])
	})

	t.Run("falls back to the default for a foreign GoAkt logger", func(t *testing.T) {
		assert.Same(t, DefaultLogger(), kitLoggerFrom(log.DiscardLogger))
	})
}

func TestGoaktPathReachesKitLogger(t *testing.T) {
	logger, sink := newCaptureLogger(kitlog.LevelDebug)
	ctx := context.Background()

	cfg := NewConfig(testkit.NewEventsStore(), WithLogger(logger))
	sys, err := goakt.NewActorSystem("LoggerPath", cfg.GoaktOptions()...)
	require.NoError(t, err)
	require.NoError(t, sys.Start(ctx))
	require.NoError(t, sys.Stop(ctx))

	records := sink.all()
	require.NotEmpty(t, records, "the actor system's own records must reach the kit-logger backend")
	for _, r := range records {
		assert.NotEmpty(t, r.msg)
	}
}

// sourceOf returns the file and function of the "source" group kit-logger
// attaches when AddSource is set, or empty strings when the record has none.
func sourceOf(t *testing.T, r capturedRecord) (file, function string) {
	t.Helper()
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
	logger, sink := newSourceCaptureLogger()
	adapter := newLoggerAdapter(logger)

	calls := map[string]func(){
		"Info":         func() { adapter.Info("msg") },
		"Infof":        func() { adapter.Infof("msg %d", 1) },
		"ErrorContext": func() { adapter.ErrorContext(context.Background(), "msg") },
		"With":         func() { adapter.With("k", "v").Warn("msg") },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			call()
			file, function := sourceOf(t, sink.last(t))
			assert.True(t, strings.HasSuffix(file, "logger_test.go"),
				"the record must name the caller of the adapter, got file %q", file)
			assert.NotContains(t, function, "loggerAdapter",
				"the adapter must never be the attributed frame")
		})
	}
}

func TestKitLoggerFromAttributesRecordsToItsDirectCaller(t *testing.T) {
	logger, sink := newSourceCaptureLogger()

	// eGo's own actors call the recovered backend directly, so no frame must
	// be skipped for them: a skip would blame whoever called the actor.
	kitLoggerFrom(newLoggerAdapter(logger)).Error("msg")
	file, function := sourceOf(t, sink.last(t))
	assert.True(t, strings.HasSuffix(file, "logger_test.go"), "got file %q", file)
	assert.Contains(t, function, "TestKitLoggerFromAttributesRecordsToItsDirectCaller")
}
