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

// Package runtime is the runtime-neutral contract of the application side of
// Urd's runtime SPI: the types, spawn options and errors that code running
// entities, sagas and projections needs, whichever runtime hosts them.
//
// It is a contract package: it depends only on the standard library and other
// contracts (tenancy, port/behavior), never on the GoAkt runtime. The GoAkt
// adapter, *engine.Engine, is one implementation; package engine keeps its older
// names as aliases of the types and errors declared here (engine.SpawnOption,
// engine.SagaInfo, engine.ErrEngineNotStarted, ...), so both names denote the same
// type or the same error value.
//
// # Spawn options
//
// A SpawnOption is built only by the With* functions of this package, or of a
// runtime adapter through WithAdapterSetting. A runtime reads the options a
// caller passed with ResolveSpawnOptions, which returns a read-only
// SpawnSettings.
//
// # Unsupported operations
//
// A runtime that lacks an operation returns an error that matches
// ErrUnsupported (and errors.ErrUnsupported), usually an *UnsupportedError
// naming the runtime and the operation. Such an error is returned before any
// side effect, never as a panic, and never for a transient failure: a runtime
// that supports an operation but cannot reach its store returns the store's
// error. Spawn settings are not operations; what a runtime does with a
// setting it cannot honor is decided by RUNTIME-003.
//
// Import this package under another name (Urd uses runtimeport) in a file
// that also imports the standard library's runtime package.
package runtime
