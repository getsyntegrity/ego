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

// Package compat is an unreleased integration module (ADR ego-arch-006, slice
// S1, decision D5). It holds the historical compatibility checks between the
// four publisher modules and the aliases package ego still exports for them
// (ADR ego-arch-001, S1 criterion 3: ego.EventPublisher, ego.StatePublisher
// and ego.ErrPublisherNotStarted, kept until #124 by #121). Those checks used
// to live in each publisher as compat_test.go behind a `compat` build tag
// (#130, #122). Here they need no build tag, and the publishers' own tests no
// longer import package ego at all.
//
// No released module may require this one; docs/ci.md lists it as
// unreleased. The package has no code of its own: the checks are its tests.
package compat
