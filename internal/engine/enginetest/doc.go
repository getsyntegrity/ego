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

// Package enginetest holds test fixtures shared by the engine actor packages
// and engine's own tests: sample behaviors and a discarding logger.
//
// It also holds typed go-specs mock adapters for the ports the engine actors
// call: EventsStoreMock, SnapshotStoreMock, StateStoreMock, EncryptorMock and
// EventAdapterMock. Each wraps a *mock.Controller, forwards every method to
// Method("<Name>").Call(args...) and turns the answer back into typed values.
// A case builds the controller with mock.NewController(ctx), so its
// expectations are verified when the case ends, and declares what each call
// answers with c.Method("Name").Expect(...).Return(...).
//
// It lives in a regular (non-test) package so that several test binaries can
// import it, and it must not import package engine, whose tests import it.
package enginetest
