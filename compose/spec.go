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

// Package compose is the runtime-neutral half of Ego's composition root
// (openspec/changes/ego-arch-003/design.md). It holds Spec, the plain
// description of what a deployment wires together, its static validation
// (Spec.Validate), and the StartError type a composition root returns when
// startup fails.
//
// A Spec is only the input to a composition root; it builds nothing and
// starts nothing. The consumer constructs every dependency and places it
// in a named field. There is no registry, no lookup by type and no
// reflection-based wiring anywhere in this package (design §D2): the one
// use of reflect is the typed-nil check in Validate, which inspects a value
// the consumer already put in a named field.
//
// This package imports contract packages only. It must not import package
// ego, internal/extensions or the GoAkt runtime; internal/cmd/archcheck's
// composition-no-runtime rule enforces that, and its composition-leaf rule
// keeps every other production package in the root module, except main
// packages and examples, from importing compose (design §D8).
package compose

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/pablogore/ego/v4/encryption"
	"github.com/pablogore/ego/v4/eventadapter"
	"github.com/pablogore/ego/v4/offsetstore"
	"github.com/pablogore/ego/v4/persistence"
	"github.com/pablogore/ego/v4/port/publishing"
	"github.com/pablogore/ego/v4/projection"
	"github.com/pablogore/ego/v4/tenancy"
)

// Family is a bit set of the entity families a deployment runs. Which
// families are declared decides which stores Spec.Validate requires.
type Family uint8

const (
	// EventSourced declares event-sourced entities; it requires
	// Spec.EventsStore.
	EventSourced Family = 1 << iota
	// DurableState declares durable-state entities; it requires
	// Spec.StateStore.
	DurableState
	// Saga declares sagas; it requires Spec.EventsStore.
	Saga
)

// Spec lists the already-constructed dependencies and the declarative
// settings a composition root needs. It means the same thing for every
// runtime; runtime-specific settings (telemetry, logger, cluster
// configuration) belong to the composition root's own options.
//
// Ownership (design §D5): stores stay owned by the consumer, who connects
// them before handing them over and closes them after the composition root
// stops. Publishers become owned by the composition root once its
// constructor succeeds.
type Spec struct {
	// Name identifies the deployment. Each runtime validates its own
	// naming rules; Validate does not.
	Name string
	// Families declares the entity families the deployment runs. At least
	// one is required (V1); nothing is implied by leaving it zero.
	Families Family

	// EventsStore is required when EventSourced or Saga is declared, or
	// when Projections is non-empty (V2).
	EventsStore persistence.EventsStore
	// StateStore is required when DurableState is declared (V3).
	StateStore persistence.StateStore
	// SnapshotStore is optional.
	SnapshotStore persistence.SnapshotStore
	// OffsetStore is required when Projections is non-empty (V4).
	OffsetStore offsetstore.OffsetStore

	// Projections maps a projection name to its options; every entry is
	// started by the composition root. Keying by name keeps names unique
	// by construction. Every entry needs non-nil options with a non-nil
	// Handler (V4).
	Projections map[string]*projection.Options

	// EventAdapters is optional; each element must be non-nil (V5).
	EventAdapters []eventadapter.EventAdapter
	// Encryptor is optional.
	Encryptor encryption.Encryptor
	// TenantResolver is optional. It is a single field, so "more than one
	// resolver" cannot be expressed.
	TenantResolver tenancy.TenantResolver

	// EventPublishers must each be non-nil, with IDs unique among events
	// publishers (V6).
	EventPublishers []publishing.EventPublisher
	// StatePublishers must each be non-nil, with IDs unique among state
	// publishers (V6).
	StatePublishers []publishing.StatePublisher

	// ShutdownTimeout bounds rollback and Stop; zero means the composition
	// root's default. It must not be negative (V7).
	ShutdownTimeout time.Duration
}

// Validate checks s statically — no I/O, no goroutines — and returns every
// problem it finds joined with errors.Join, each one a *ValidationError
// naming its rule and field, in Spec field order. It returns nil when s is
// valid. The rules (design §D4a):
//
//   - V1: Families is non-zero.
//   - V2: EventsStore is set when EventSourced or Saga is declared, or
//     Projections is non-empty.
//   - V3: StateStore is set when DurableState is declared.
//   - V4: OffsetStore is set when Projections is non-empty, and every
//     projection has non-nil options with a non-nil Handler.
//   - V5: no interface-typed field or element holds a typed nil (a nil
//     pointer wrapped in a non-nil interface), whether the field is
//     required or optional. Leaving an optional field out (a literal nil)
//     is fine; a typed nil is not, because it passes a runtime's own
//     "!= nil" guard and panics on first use. A literal nil EventAdapters
//     element is reported under V5 too.
//   - V6: every publisher is non-nil, and publisher IDs are unique per
//     kind (events, states).
//   - V7: ShutdownTimeout is not negative. Zero means the composition
//     root's default; a negative value would otherwise fail only when the
//     composition root builds its lifecycle, after New had succeeded.
func (s Spec) Validate() error {
	var errs []error
	report := func(rule, field, problem string) {
		errs = append(errs, &ValidationError{Rule: rule, Field: field, Problem: problem})
	}
	// check reports a literal nil under rule when the field is required,
	// and a typed nil under V5 whether the field is required or not; an
	// optional field passes required=false, so a literal nil is accepted.
	check := func(value any, required bool, rule, field, why string) {
		switch {
		case value == nil && required:
			report(rule, field, "required "+why)
		case value != nil && isTypedNil(value):
			report("V5", field, "holds a typed nil value")
		}
	}

	if s.Families == 0 {
		report("V1", "Families", "at least one entity family must be declared")
	}

	needsEvents := s.Families&(EventSourced|Saga) != 0 || len(s.Projections) > 0
	check(s.EventsStore, needsEvents, "V2", "EventsStore", "when EventSourced or Saga is declared or projections are configured")
	check(s.StateStore, s.Families&DurableState != 0, "V3", "StateStore", "when DurableState is declared")
	check(s.SnapshotStore, false, "", "SnapshotStore", "")
	check(s.OffsetStore, len(s.Projections) > 0, "V4", "OffsetStore", "when projections are configured")

	names := make([]string, 0, len(s.Projections))
	for name := range s.Projections {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		field := fmt.Sprintf("Projections[%q]", name)
		opts := s.Projections[name]
		if opts == nil {
			report("V4", field, "projection options must not be nil")
			continue
		}
		check(opts.Handler, true, "V4", field+".Handler", "for every projection")
	}

	for i, adapter := range s.EventAdapters {
		field := fmt.Sprintf("EventAdapters[%d]", i)
		if adapter == nil {
			report("V5", field, "event adapter must not be nil")
			continue
		}
		check(adapter, false, "", field, "")
	}
	check(s.Encryptor, false, "", "Encryptor", "")
	check(s.TenantResolver, false, "", "TenantResolver", "")

	validatePublishers(s.EventPublishers, "EventPublishers", "events", report)
	validatePublishers(s.StatePublishers, "StatePublishers", "state", report)

	if s.ShutdownTimeout < 0 {
		report("V7", "ShutdownTimeout", fmt.Sprintf("must not be negative, got %s (zero means the default)", s.ShutdownTimeout))
	}

	return errors.Join(errs...)
}

// validatePublishers applies V5 and V6 to one kind of publisher. The engine
// keys each kind's publishers by ID in its own map, so a duplicate ID
// overwrites the first publisher and leaks its goroutine; uniqueness is
// therefore checked per kind, not across kinds.
func validatePublishers[P interface{ ID() string }](publishers []P, fieldName, kind string, report func(rule, field, problem string)) {
	firstByID := make(map[string]int, len(publishers))
	for i, publisher := range publishers {
		field := fmt.Sprintf("%s[%d]", fieldName, i)
		value := any(publisher)
		if value == nil {
			report("V6", field, kind+" publisher must not be nil")
			continue
		}
		if isTypedNil(value) {
			report("V5", field, "holds a typed nil value")
			continue
		}
		id := publisher.ID()
		if first, seen := firstByID[id]; seen {
			report("V6", field, fmt.Sprintf("duplicate %s publisher ID %q (first used by %s[%d])", kind, id, fieldName, first))
			continue
		}
		firstByID[id] = i
	}
}

// isTypedNil reports whether v, known to be a non-nil interface value,
// wraps a nil pointer, map, slice, func or channel. It is the only use of
// reflect in this package: it inspects a value, it does not wire anything.
func isTypedNil(v any) bool {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return rv.IsNil()
	default:
		return false
	}
}
