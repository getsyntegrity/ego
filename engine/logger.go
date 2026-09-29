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
	"log/slog"

	kitlog "github.com/pablogore/kit-logger/pkg/logger"

	"github.com/getsyntegrity/ego/internal/logging"
)

// eGo logs through kit-logger (github.com/pablogore/kit-logger). Every
// logging surface of the framework — the engine, the migrator, the
// publishers, and the GoAkt actor system eGo sits on — takes a kit-logger
// Logger, so one logger instance configured by the application covers the
// whole runtime.
//
// This file is the public logging facade: DiscardLogger, DefaultLogger and
// ResolveLogger, the loggers and the fallback eGo itself supplies. The glue
// behind it lives in two internal packages:
//
//   - internal/logging resolves the default logger and the nil and typed-nil
//     fallback. It is runtime-neutral, so migration can use it too.
//   - internal/goaktlog presents a kit-logger Logger to GoAkt through GoAkt's
//     own log.Logger interface. GoAkt is the actor runtime and cannot take a
//     kit-logger Logger directly, so that package is the single seam where
//     GoAkt's printf-style API is translated into structured records.

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
//
// The typed-nil detection and the actual resolution live in the runtime-free
// internal/logging package (#147, ego-arch-001 §3, S4-1), so that migration —
// which needs the same fallback but must not import package engine — can use it
// without pulling in the GoAkt runtime. DefaultLogger and ResolveLogger here
// only delegate, preserving their exact signature and behavior, including
// the DefaultLogger() identity every WithLogger(nil) caller compares against.
func DefaultLogger() kitlog.Logger {
	return logging.DefaultLogger()
}

// ResolveLogger returns logger when it is usable, and DefaultLogger() when it
// is nil or a typed-nil pointer. It lets packages outside the root apply the
// same nil-logger semantics the engine uses, without each of them
// re-implementing the typed-nil detection. See DefaultLogger's doc comment
// for where the underlying logic now lives.
func ResolveLogger(logger kitlog.Logger) kitlog.Logger {
	return logging.ResolveLogger(logger)
}
