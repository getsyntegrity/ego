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

// Package adapters is the runtime-free start-and-probe loop Ego's
// composition roots run over the adapters they own
// (openspec/changes/ego-arch-004/design.md §D4). compose/goakt runs it in
// its "attach publishers" step; compose/inmem (#148) reuses it unchanged.
//
// It inspects adapters only through port/adapter's accessors (StarterOf,
// PingerOf, Describe), never with its own type assertions. It imports no
// runtime: internal/cmd/archcheck's composition-no-runtime rule covers it
// like compose/internal/lifecycle.
package adapters

import (
	"context"
	"fmt"

	"github.com/getsyntegrity/ego/port/adapter"
)

// Owned is one adapter a composition root owns, with what it takes to name
// it in an error.
type Owned struct {
	// Kind names what the adapter is in the Spec, for example
	// "events publisher".
	Kind string
	// ID is the instance identity, for example a publisher's ID().
	ID string
	// Value is the adapter itself.
	Value any
}

// StartAndProbe goes through owned in order and, for each value, calls
// Start when adapter.StarterOf finds one, then Ping when adapter.PingerOf
// finds one, before moving to the next value. A value with neither is
// skipped, and so is a nil or typed-nil value, which the accessors report
// as implementing nothing.
//
// It stops at the first failure and returns that error wrapped with the
// call that failed and the value's kind, ID and, when the value declares a
// descriptor, its adapter name; later values are neither started nor
// probed. It never closes anything: releasing every owned value, started
// or not, is the composition root's job (design §D4, "Partial failure").
// A value whose Start failed has already cleaned up after itself (rule
// L1), and Close is safe on values that were never started (rule L2).
func StartAndProbe(ctx context.Context, owned []Owned) error {
	for _, o := range owned {
		if starter, ok := adapter.StarterOf(o.Value); ok {
			if err := starter.Start(ctx); err != nil {
				return fmt.Errorf("start %s: %w", name(o), err)
			}
		}
		if pinger, ok := adapter.PingerOf(o.Value); ok {
			if err := pinger.Ping(ctx); err != nil {
				return fmt.Errorf("ping %s: %w", name(o), err)
			}
		}
	}
	return nil
}

// name renders o for an error: `events publisher "id"`, followed by
// `(adapter "kafka")` when o declares a descriptor.
func name(o Owned) string {
	out := fmt.Sprintf("%s %q", o.Kind, o.ID)
	if desc, ok := adapter.Describe(o.Value); ok {
		out += fmt.Sprintf(" (adapter %q)", desc.Name)
	}
	return out
}
