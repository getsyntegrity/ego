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
	"fmt"
	golog "log"
	"log/slog"
	"reflect"
	"strings"

	kitlog "github.com/pablogore/kit-logger/pkg/logger"
	"github.com/tochemey/goakt/v4/log"
)

// eGo logs through kit-logger (github.com/pablogore/kit-logger). Every
// logging surface of the framework — the engine, the migrator, the
// publishers, and the GoAkt actor system eGo sits on — takes a kit-logger
// Logger, so one logger instance configured by the application covers the
// whole runtime.
//
// This file owns the only two pieces of logging glue eGo needs:
//
//   - DiscardLogger and DefaultLogger, the two loggers eGo itself supplies.
//   - loggerAdapter, which presents a kit-logger Logger to GoAkt through
//     GoAkt's own log.Logger interface. GoAkt is the actor runtime and cannot
//     take a kit-logger Logger directly, so the adapter is the single seam
//     where GoAkt's printf-style API is translated into structured records.

// DiscardLogger is a kit-logger Logger that silently discards every record.
// Its level gate reports every level as disabled, so the engine and the actor
// system skip message formatting entirely. It is useful in tests and
// benchmarks, or wherever logging is not desired.
var DiscardLogger kitlog.Logger = kitlog.New(kitlog.Config{Sink: slog.DiscardHandler})

// DefaultLogger returns the Logger eGo uses when none is supplied: kit-logger's
// process-wide logger, as returned by logger.L(). An application that installs
// its own logger with logger.SetGlobal before building the engine therefore
// gets eGo's records through it without passing WithLogger at all.
//
// It is a function rather than a variable so that importing eGo never
// constructs the global logger as a side effect; the lookup happens when a
// Config or a Migrator is built.
func DefaultLogger() kitlog.Logger {
	return kitlog.L()
}

// isNilLogger returns true when l is nil or a typed-nil (e.g. (*MyLogger)(nil)).
// A typed-nil interface value is non-nil at the interface level but wraps a nil
// pointer, which would cause a nil-dereference panic on the first log call.
func isNilLogger(l kitlog.Logger) bool {
	if l == nil {
		return true
	}
	v := reflect.ValueOf(l)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// ResolveLogger returns logger when it is usable, and DefaultLogger() when it
// is nil or a typed-nil pointer. It lets packages outside the root apply the
// same nil-logger semantics the engine uses, without each of them
// re-implementing the typed-nil detection.
func ResolveLogger(logger kitlog.Logger) kitlog.Logger {
	if isNilLogger(logger) {
		return DefaultLogger()
	}
	return logger
}

// loggerAdapter presents a kit-logger Logger through GoAkt's log.Logger
// interface, which is what the actor system and every actor context expose.
//
// The adapter is deliberately thin: kit-logger already offers context-aware
// methods, native child loggers and a level gate, so every GoAkt call maps to
// exactly one kit-logger call. The only translation it performs is splitting
// GoAkt's variadic arguments into a message and its key-value fields, and
// rendering the printf-style variants — after checking the level, so a record
// nobody will emit is never formatted.
type loggerAdapter struct {
	// backend is the kit-logger Logger the application configured, exactly as
	// it was given. It is what kitLoggerFrom hands back to eGo's own actors,
	// which call it directly and must be attributed to their own call sites.
	backend kitlog.Logger

	// inner is backend adjusted for GoAkt's records. Every GoAkt call passes
	// through one adapter method before reaching kit-logger, so when the
	// backend supports it, inner skips that frame and a record names the
	// GoAkt call site instead of this file.
	inner kitlog.Logger

	// slogger is the backend's own *slog.Logger, resolved once: it answers
	// every level check without a round trip through kit-logger's variadic
	// argument scanning, and it shares the backend's level variable, so a
	// SetLevel made at runtime is honored on the very next check.
	slogger *slog.Logger
}

// compile-time check
var _ log.Logger = (*loggerAdapter)(nil)

// newLoggerAdapter wraps a kit-logger Logger for GoAkt.
func newLoggerAdapter(backend kitlog.Logger) *loggerAdapter {
	inner := backend
	if skipper, ok := backend.(kitlog.CallerSkipper); ok {
		inner = skipper.WithCallerSkip(1)
	}
	return &loggerAdapter{backend: backend, inner: inner, slogger: backend.Slog()}
}

// kitLoggerFrom recovers the kit-logger Logger behind a GoAkt logger. Every
// actor system built from Config.GoaktOptions carries a loggerAdapter, so
// eGo's own actors log through the same backend the application configured.
// A foreign GoAkt logger — an actor system assembled without GoaktOptions —
// yields DefaultLogger(), which is the same fallback the engine applies when no
// logger is configured.
func kitLoggerFrom(l log.Logger) kitlog.Logger {
	if a, ok := l.(*loggerAdapter); ok {
		return a.backend
	}
	return DefaultLogger()
}

// goaktLevelsByVerbosity lists every goakt log.Level from most to least
// verbose. LogLevel walks it to report the most verbose enabled level.
var goaktLevelsByVerbosity = []log.Level{
	log.DebugLevel,
	log.InfoLevel,
	log.WarningLevel,
	log.ErrorLevel,
	log.FatalLevel,
	log.PanicLevel,
}

// goaktToSlogLevel maps a goakt log.Level to the slog.Level the backend is
// asked about. GoAkt's level enum has non-standard ordering (DebugLevel is the
// last iota value), so the mapping is explicit rather than a numeric cast.
// Fatal and Panic sit above Error to keep the result strictly monotonic in
// severity, which lets LogLevel probe levels in verbosity order.
func goaktToSlogLevel(l log.Level) slog.Level {
	switch l {
	case log.DebugLevel:
		return slog.LevelDebug
	case log.InfoLevel:
		return slog.LevelInfo
	case log.WarningLevel:
		return slog.LevelWarn
	case log.ErrorLevel:
		return slog.LevelError
	case log.FatalLevel:
		return slog.LevelError + 4
	case log.PanicLevel:
		return slog.LevelError + 8
	default:
		return slog.LevelInfo
	}
}

// enabledAt asks the backend whether it accepts level for the given context.
// A backend whose Slog returns nil declared no level, so every level reports
// enabled: the adapter never filters on behalf of a Logger that never said it
// wanted filtering.
func (a *loggerAdapter) enabledAt(ctx context.Context, l log.Level) bool {
	if a.slogger == nil {
		return true
	}
	return a.slogger.Enabled(ctx, goaktToSlogLevel(l))
}

// goaktArgsToMsg splits GoAkt variadic log arguments into a message string
// and optional key-value fields. GoAkt follows the slog convention where the
// first argument is the log message and subsequent arguments are alternating
// key-value pairs for structured logging.
func goaktArgsToMsg(args []any) (string, []any) {
	if len(args) == 0 {
		return "", nil
	}

	msg, ok := args[0].(string) // the common case — avoids fmt.Sprint reflection
	if !ok {
		msg = fmt.Sprint(args[0])
	}

	if len(args) == 1 {
		return msg, nil
	}
	return msg, args[1:]
}

// The plain methods do not re-check the level: GoAkt already guards its call
// sites with Enabled, and kit-logger applies its own gate before formatting.
// The printf-style methods do check, because the fmt.Sprintf allocation is one
// this adapter would otherwise pay for a record nobody will emit.

func (a *loggerAdapter) Debug(args ...any) {
	msg, fields := goaktArgsToMsg(args)
	a.inner.Debug(msg, fields...)
}
func (a *loggerAdapter) Debugf(format string, args ...any) {
	if !a.enabledAt(context.Background(), log.DebugLevel) {
		return
	}
	a.inner.Debug(fmt.Sprintf(format, args...))
}
func (a *loggerAdapter) DebugContext(ctx context.Context, args ...any) {
	msg, fields := goaktArgsToMsg(args)
	a.inner.DebugContext(ctx, msg, fields...)
}
func (a *loggerAdapter) DebugfContext(ctx context.Context, format string, args ...any) {
	if !a.enabledAt(ctx, log.DebugLevel) {
		return
	}
	a.inner.DebugContext(ctx, fmt.Sprintf(format, args...))
}
func (a *loggerAdapter) Info(args ...any) {
	msg, fields := goaktArgsToMsg(args)
	a.inner.Info(msg, fields...)
}
func (a *loggerAdapter) Infof(format string, args ...any) {
	if !a.enabledAt(context.Background(), log.InfoLevel) {
		return
	}
	a.inner.Info(fmt.Sprintf(format, args...))
}
func (a *loggerAdapter) InfoContext(ctx context.Context, args ...any) {
	msg, fields := goaktArgsToMsg(args)
	a.inner.InfoContext(ctx, msg, fields...)
}
func (a *loggerAdapter) InfofContext(ctx context.Context, format string, args ...any) {
	if !a.enabledAt(ctx, log.InfoLevel) {
		return
	}
	a.inner.InfoContext(ctx, fmt.Sprintf(format, args...))
}
func (a *loggerAdapter) Warn(args ...any) {
	msg, fields := goaktArgsToMsg(args)
	a.inner.Warn(msg, fields...)
}
func (a *loggerAdapter) Warnf(format string, args ...any) {
	if !a.enabledAt(context.Background(), log.WarningLevel) {
		return
	}
	a.inner.Warn(fmt.Sprintf(format, args...))
}
func (a *loggerAdapter) WarnContext(ctx context.Context, args ...any) {
	msg, fields := goaktArgsToMsg(args)
	a.inner.WarnContext(ctx, msg, fields...)
}
func (a *loggerAdapter) WarnfContext(ctx context.Context, format string, args ...any) {
	if !a.enabledAt(ctx, log.WarningLevel) {
		return
	}
	a.inner.WarnContext(ctx, fmt.Sprintf(format, args...))
}
func (a *loggerAdapter) Error(args ...any) {
	msg, fields := goaktArgsToMsg(args)
	a.inner.Error(msg, fields...)
}
func (a *loggerAdapter) Errorf(format string, args ...any) {
	if !a.enabledAt(context.Background(), log.ErrorLevel) {
		return
	}
	a.inner.Error(fmt.Sprintf(format, args...))
}
func (a *loggerAdapter) ErrorContext(ctx context.Context, args ...any) {
	msg, fields := goaktArgsToMsg(args)
	a.inner.ErrorContext(ctx, msg, fields...)
}
func (a *loggerAdapter) ErrorfContext(ctx context.Context, format string, args ...any) {
	if !a.enabledAt(ctx, log.ErrorLevel) {
		return
	}
	a.inner.ErrorContext(ctx, fmt.Sprintf(format, args...))
}

// LogLevel reports the most verbose level the backend currently accepts. It
// is resolved per call, never cached, so a runtime SetLevel is honored. When
// the backend enables nothing — DiscardLogger, for instance — the result is
// InvalidLevel, matching what GoAkt's own adapters report for an unmappable
// level.
func (a *loggerAdapter) LogLevel() log.Level {
	for _, l := range goaktLevelsByVerbosity {
		if a.enabledAt(context.Background(), l) {
			return l
		}
	}
	return log.InvalidLevel
}

// Enabled reports whether the backend accepts the given level. GoAkt's
// Logger interface carries no context here, so Background is used; the
// *Context methods forward the caller's real context instead.
func (a *loggerAdapter) Enabled(l log.Level) bool {
	return a.enabledAt(context.Background(), l)
}

// With returns a child logger carrying the given key-value pairs. The child is
// built by kit-logger itself, so the fields live in the backend and chained
// calls accumulate there; the adapter keeps no field slice of its own.
func (a *loggerAdapter) With(keyValues ...any) log.Logger {
	if len(keyValues) == 0 {
		return a
	}
	return newLoggerAdapter(a.backend.With(keyValues...))
}

// Flush delivers every record the backend has accepted so far. A buffered
// kit-logger drains asynchronously, so GoAkt's flush at shutdown is forwarded
// to the backend's own lifecycle when it exposes one; the drain is bounded by
// kit-logger's DefaultShutdownTimeout so a stuck downstream handler cannot
// hang the actor system's stop. The logger itself is never shut down: the
// application that built it owns its lifecycle.
func (a *loggerAdapter) Flush() error {
	if managed, ok := a.backend.(kitlog.ManagedLogger); ok {
		ctx, cancel := context.WithTimeout(context.Background(), kitlog.DefaultShutdownTimeout)
		defer cancel()
		return managed.Flush(ctx)
	}
	return a.backend.Sync()
}

// StdLogger returns a standard library logger whose every line is written as
// an INFO record through the backend, so third-party code that only accepts a
// *log.Logger still lands in the same output as everything else.
func (a *loggerAdapter) StdLogger() *golog.Logger {
	return golog.New(&loggerWriter{inner: a.backend}, "", 0)
}

// loggerWriter adapts Logger.Info to io.Writer for use with *log.Logger.
type loggerWriter struct {
	inner kitlog.Logger
}

func (w *loggerWriter) Write(p []byte) (int, error) {
	w.inner.Info(strings.TrimRight(string(p), "\r\n"))
	return len(p), nil
}
