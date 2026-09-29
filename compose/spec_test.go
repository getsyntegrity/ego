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

package compose

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsyntegrity/ego/encryption"
	"github.com/getsyntegrity/ego/eventadapter"
	"github.com/getsyntegrity/ego/offsetstore"
	"github.com/getsyntegrity/ego/persistence"
	"github.com/getsyntegrity/ego/port/adapter"
	"github.com/getsyntegrity/ego/port/publishing"
	"github.com/getsyntegrity/ego/projection"
	"github.com/getsyntegrity/ego/tenancy"
)

// The fakes below embed the contract interface they stand in for, so they
// satisfy it without implementing any method: Validate never calls a
// dependency (design D4a: no I/O), it only inspects the values. A nil
// pointer to one of them is the typed-nil case V5 targets.
type (
	fakeEventsStore    struct{ persistence.EventsStore }
	fakeStateStore     struct{ persistence.StateStore }
	fakeSnapshotStore  struct{ persistence.SnapshotStore }
	fakeOffsetStore    struct{ offsetstore.OffsetStore }
	fakeEventAdapter   struct{ eventadapter.EventAdapter }
	fakeEncryptor      struct{ encryption.Encryptor }
	fakeTenantResolver struct{ tenancy.TenantResolver }
	fakeHandler        struct{ projection.Handler }
)

type fakeEventPublisher struct {
	publishing.EventPublisher
	id string
}

func (p *fakeEventPublisher) ID() string { return p.id }

type fakeStatePublisher struct {
	publishing.StatePublisher
	id string
}

func (p *fakeStatePublisher) ID() string { return p.id }

// validSpec returns a Spec that uses every family, every optional field and
// one projection, and passes Validate. Each rule test breaks exactly one
// thing in it, so a failure can only come from the rule under test.
func validSpec() Spec {
	return Spec{
		Name:          "orders",
		Families:      EventSourced | DurableState | Saga,
		EventsStore:   &fakeEventsStore{},
		StateStore:    &fakeStateStore{},
		SnapshotStore: &fakeSnapshotStore{},
		OffsetStore:   &fakeOffsetStore{},
		Projections: map[string]*projection.Options{
			"order-view": {Handler: &fakeHandler{}},
		},
		EventAdapters:   []eventadapter.EventAdapter{&fakeEventAdapter{}},
		Encryptor:       &fakeEncryptor{},
		TenantResolver:  &fakeTenantResolver{},
		EventPublishers: []publishing.EventPublisher{&fakeEventPublisher{id: "events-a"}, &fakeEventPublisher{id: "events-b"}},
		StatePublishers: []publishing.StatePublisher{&fakeStatePublisher{id: "states-a"}},
	}
}

// problems returns every *ValidationError joined into err, in order.
func problems(t *testing.T, err error) []*ValidationError {
	t.Helper()
	if err == nil {
		return nil
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("Validate error %T is not a joined error: %v", err, err)
	}
	var out []*ValidationError
	for _, e := range joined.Unwrap() {
		var ve *ValidationError
		if !errors.As(e, &ve) {
			t.Fatalf("joined error %T is not a *ValidationError: %v", e, e)
		}
		out = append(out, ve)
	}
	return out
}

// requireOneProblem asserts err holds exactly one *ValidationError with
// the given rule and field, and that its message names the field.
func requireOneProblem(t *testing.T, err error, rule, field string) {
	t.Helper()
	got := problems(t, err)
	if len(got) != 1 {
		t.Fatalf("Validate() reported %d problem(s), want exactly 1 (%s on %s): %v", len(got), rule, field, err)
	}
	if got[0].Rule != rule || got[0].Field != field {
		t.Fatalf("Validate() problem = {Rule: %q, Field: %q}, want {Rule: %q, Field: %q}: %v", got[0].Rule, got[0].Field, rule, field, err)
	}
	if !strings.Contains(err.Error(), field) {
		t.Errorf("error %q does not name the offending field %q", err, field)
	}
}

func TestSpecValidate_ValidSpecPasses(t *testing.T) {
	if err := validSpec().Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil for a valid Spec", err)
	}
}

// A Spec that declares one family needs only that family's store, and the
// optional fields may be left out entirely (a literal nil means "not
// configured").
func TestSpecValidate_MinimalSpecsPass(t *testing.T) {
	cases := map[string]Spec{
		"event sourced only": {Families: EventSourced, EventsStore: &fakeEventsStore{}},
		"durable state only": {Families: DurableState, StateStore: &fakeStateStore{}},
		"saga only":          {Families: Saga, EventsStore: &fakeEventsStore{}},
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			if err := spec.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestSpecValidate_V1_ZeroFamilies(t *testing.T) {
	spec := validSpec()
	spec.Families = 0
	requireOneProblem(t, spec.Validate(), "V1", "Families")
}

func TestSpecValidate_V2_EventsStoreRequired(t *testing.T) {
	cases := map[string]Family{
		"event sourced declared": EventSourced,
		"saga declared":          Saga,
		// validSpec has a projection, which needs the events store even
		// when no event-backed family is declared.
		"projections without an event-backed family": DurableState,
	}
	for name, families := range cases {
		t.Run(name, func(t *testing.T) {
			spec := validSpec()
			spec.Families = families
			spec.EventsStore = nil
			requireOneProblem(t, spec.Validate(), "V2", "EventsStore")
		})
	}
}

// Durable state alone with no projections does not need an events store.
func TestSpecValidate_V2_NotRequiredForDurableStateOnly(t *testing.T) {
	spec := validSpec()
	spec.Families = DurableState
	spec.Projections = nil
	spec.EventsStore = nil
	if err := spec.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestSpecValidate_V3_StateStoreRequired(t *testing.T) {
	spec := validSpec()
	spec.StateStore = nil
	requireOneProblem(t, spec.Validate(), "V3", "StateStore")
}

func TestSpecValidate_V3_NotRequiredWithoutDurableState(t *testing.T) {
	spec := validSpec()
	spec.Families = EventSourced | Saga
	spec.StateStore = nil
	if err := spec.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestSpecValidate_V4_Projections(t *testing.T) {
	t.Run("offset store required", func(t *testing.T) {
		spec := validSpec()
		spec.OffsetStore = nil
		requireOneProblem(t, spec.Validate(), "V4", "OffsetStore")
	})
	t.Run("nil options", func(t *testing.T) {
		spec := validSpec()
		spec.Projections["order-view"] = nil
		requireOneProblem(t, spec.Validate(), "V4", `Projections["order-view"]`)
	})
	t.Run("nil handler", func(t *testing.T) {
		spec := validSpec()
		spec.Projections["order-view"] = &projection.Options{}
		requireOneProblem(t, spec.Validate(), "V4", `Projections["order-view"].Handler`)
	})
	t.Run("offset store not required without projections", func(t *testing.T) {
		spec := validSpec()
		spec.Projections = nil
		spec.OffsetStore = nil
		if err := spec.Validate(); err != nil {
			t.Fatalf("Validate() = %v, want nil", err)
		}
	})
}

// V5: a typed-nil value (a nil pointer wrapped in a non-nil interface) is
// the bug class behind the eventsStore.Ping panic in design §2.2. It is
// rejected for every interface-typed field and element, required or
// optional: an optional field left out is a literal nil, whereas a typed
// nil passes the runtime's own `!= nil` guards and panics on first use.
func TestSpecValidate_V5_TypedNilPerInterfaceField(t *testing.T) {
	cases := []struct {
		field  string
		adjust func(*Spec)
	}{
		{"EventsStore", func(s *Spec) { s.EventsStore = (*fakeEventsStore)(nil) }},
		{"StateStore", func(s *Spec) { s.StateStore = (*fakeStateStore)(nil) }},
		{"SnapshotStore", func(s *Spec) { s.SnapshotStore = (*fakeSnapshotStore)(nil) }},
		{"OffsetStore", func(s *Spec) { s.OffsetStore = (*fakeOffsetStore)(nil) }},
		{"Encryptor", func(s *Spec) { s.Encryptor = (*fakeEncryptor)(nil) }},
		{"TenantResolver", func(s *Spec) { s.TenantResolver = (*fakeTenantResolver)(nil) }},
		{"EventAdapters[0]", func(s *Spec) { s.EventAdapters = []eventadapter.EventAdapter{(*fakeEventAdapter)(nil)} }},
		{"EventPublishers[1]", func(s *Spec) { s.EventPublishers[1] = (*fakeEventPublisher)(nil) }},
		{"StatePublishers[0]", func(s *Spec) { s.StatePublishers[0] = (*fakeStatePublisher)(nil) }},
		{`Projections["order-view"].Handler`, func(s *Spec) {
			s.Projections["order-view"] = &projection.Options{Handler: (*fakeHandler)(nil)}
		}},
	}
	for _, c := range cases {
		t.Run(c.field, func(t *testing.T) {
			spec := validSpec()
			c.adjust(&spec)
			requireOneProblem(t, spec.Validate(), "V5", c.field)
		})
	}
}

// A literal nil element in EventAdapters is never meaningful either.
func TestSpecValidate_V5_NilEventAdapterElement(t *testing.T) {
	spec := validSpec()
	spec.EventAdapters = []eventadapter.EventAdapter{nil}
	requireOneProblem(t, spec.Validate(), "V5", "EventAdapters[0]")
}

func TestSpecValidate_V6_NilPublisher(t *testing.T) {
	t.Run("events", func(t *testing.T) {
		spec := validSpec()
		spec.EventPublishers[0] = nil
		requireOneProblem(t, spec.Validate(), "V6", "EventPublishers[0]")
	})
	t.Run("states", func(t *testing.T) {
		spec := validSpec()
		spec.StatePublishers[0] = nil
		requireOneProblem(t, spec.Validate(), "V6", "StatePublishers[0]")
	})
}

func TestSpecValidate_V6_DuplicatePublisherIDsPerKind(t *testing.T) {
	t.Run("events", func(t *testing.T) {
		spec := validSpec()
		spec.EventPublishers[1] = &fakeEventPublisher{id: "events-a"}
		err := spec.Validate()
		requireOneProblem(t, err, "V6", "EventPublishers[1]")
		if !strings.Contains(err.Error(), `"events-a"`) {
			t.Errorf("error %q does not name the duplicate ID", err)
		}
	})
	t.Run("states", func(t *testing.T) {
		spec := validSpec()
		spec.StatePublishers = append(spec.StatePublishers, &fakeStatePublisher{id: "states-a"})
		requireOneProblem(t, spec.Validate(), "V6", "StatePublishers[1]")
	})
	// Uniqueness is per kind: the engine keys events publishers and states
	// publishers in separate maps, so the two kinds may share an ID.
	t.Run("same ID across kinds is allowed", func(t *testing.T) {
		spec := validSpec()
		spec.StatePublishers[0] = &fakeStatePublisher{id: "events-a"}
		if err := spec.Validate(); err != nil {
			t.Fatalf("Validate() = %v, want nil", err)
		}
	})
}

// V7: a negative ShutdownTimeout is rejected at Validate, so a composition
// root fails at New instead of when it builds its lifecycle sequence. Zero
// (the default) and any positive value pass.
func TestSpecValidate_V7_NegativeShutdownTimeout(t *testing.T) {
	spec := validSpec()
	spec.ShutdownTimeout = -time.Second
	requireOneProblem(t, spec.Validate(), "V7", "ShutdownTimeout")

	for _, d := range []time.Duration{0, time.Nanosecond, time.Minute} {
		spec.ShutdownTimeout = d
		if err := spec.Validate(); err != nil {
			t.Errorf("Validate() with ShutdownTimeout %s = %v, want nil", d, err)
		}
	}
}

// Validate reports every problem at once instead of stopping at the first,
// in a deterministic order: Spec field order, projections sorted by name.
func TestSpecValidate_ReportsEveryProblem(t *testing.T) {
	spec := Spec{
		Projections:     map[string]*projection.Options{"b-view": {}, "a-view": nil},
		SnapshotStore:   (*fakeSnapshotStore)(nil),
		EventPublishers: []publishing.EventPublisher{&fakeEventPublisher{id: "x"}, &fakeEventPublisher{id: "x"}},
		StatePublishers: []publishing.StatePublisher{nil},
		ShutdownTimeout: -1,
	}
	err := spec.Validate()
	got := problems(t, err)

	want := []struct{ rule, field string }{
		{"V1", "Families"},
		{"V2", "EventsStore"},
		{"V5", "SnapshotStore"},
		{"V4", "OffsetStore"},
		{"V4", `Projections["a-view"]`},
		{"V4", `Projections["b-view"].Handler`},
		{"V6", "EventPublishers[1]"},
		{"V6", "StatePublishers[0]"},
		{"V7", "ShutdownTimeout"},
	}
	if len(got) != len(want) {
		t.Fatalf("Validate() reported %d problems, want %d: %v", len(got), len(want), err)
	}
	for i, w := range want {
		if got[i].Rule != w.rule || got[i].Field != w.field {
			t.Errorf("problem %d = {%q, %q}, want {%q, %q}", i, got[i].Rule, got[i].Field, w.rule, w.field)
		}
		if !strings.Contains(err.Error(), w.field) {
			t.Errorf("joined error does not name %s: %v", w.field, err)
		}
	}
}

func TestStartError(t *testing.T) {
	cause := errors.New("store unreachable")
	undo := errors.New("actor system did not stop")

	t.Run("without rollback error", func(t *testing.T) {
		err := error(&StartError{Step: "probe", Err: cause})
		if !errors.Is(err, cause) {
			t.Errorf("errors.Is(err, cause) = false, want true")
		}
		if msg := err.Error(); !strings.Contains(msg, "probe") || !strings.Contains(msg, cause.Error()) || strings.Contains(msg, "rollback") {
			t.Errorf("Error() = %q, want the step and cause and no rollback clause", msg)
		}
	})
	t.Run("with rollback error", func(t *testing.T) {
		err := error(&StartError{Step: "attach publishers", Err: cause, Rollback: undo})
		if !errors.Is(err, cause) || !errors.Is(err, undo) {
			t.Errorf("errors.Is must match both the step error and the rollback error: %v", err)
		}
		if msg := err.Error(); !strings.Contains(msg, "attach publishers") || !strings.Contains(msg, cause.Error()) || !strings.Contains(msg, undo.Error()) {
			t.Errorf("Error() = %q, want the step, the cause and the rollback error", msg)
		}
		var se *StartError
		if !errors.As(err, &se) || se.Step != "attach publishers" {
			t.Errorf("errors.As did not recover the step: %+v", se)
		}
	})
}

// The fakes below declare an adapter.Descriptor. Each embeds the matching
// undeclared fake, so it satisfies its port without implementing any
// method, and adds only Describe plus the optional methods its name says.
type described struct{ desc adapter.Descriptor }

func (d described) Describe() adapter.Descriptor { return d.desc }

type (
	declaredEventsStore struct {
		fakeEventsStore
		described
	}
	declaredStateStore struct {
		fakeStateStore
		described
	}
	declaredSnapshotStore struct {
		fakeSnapshotStore
		described
	}
	declaredEncryptor struct {
		fakeEncryptor
		described
	}
	declaredTenantResolver struct {
		fakeTenantResolver
		described
	}
	// declaredFixedTenantResolver implements tenancy.FixedTenantResolver.
	declaredFixedTenantResolver struct{ declaredTenantResolver }
	// undeclaredFixedTenantResolver implements FixedTenantResolver and no
	// Describe.
	undeclaredFixedTenantResolver struct{ fakeTenantResolver }
	declaredEventPublisher        struct {
		fakeEventPublisher
		described
	}
	declaredStatePublisher struct {
		fakeStatePublisher
		described
	}
	// startingEventPublisher implements adapter.Starter.
	startingEventPublisher struct{ declaredEventPublisher }
	// pingingStatePublisher implements adapter.Pinger.
	pingingStatePublisher struct{ declaredStatePublisher }
	// undeclaredStartingEventPublisher implements Starter and no Describe.
	undeclaredStartingEventPublisher struct{ fakeEventPublisher }
)

func (declaredFixedTenantResolver) FixedTenant() (tenancy.TenantID, bool)   { return "", false }
func (undeclaredFixedTenantResolver) FixedTenant() (tenancy.TenantID, bool) { return "", false }
func (*startingEventPublisher) Start(context.Context) error                 { return nil }
func (*pingingStatePublisher) Ping(context.Context) error                   { return nil }
func (*undeclaredStartingEventPublisher) Start(context.Context) error       { return nil }

func descriptor(port adapter.Port, capabilities ...adapter.Capability) adapter.Descriptor {
	return adapter.Descriptor{Ports: []adapter.Port{port}, Name: "fake", Capabilities: capabilities}
}

func eventPublisher(id string, desc adapter.Descriptor) *declaredEventPublisher {
	return &declaredEventPublisher{fakeEventPublisher: fakeEventPublisher{id: id}, described: described{desc}}
}

func startingPublisher(id string, desc adapter.Descriptor) *startingEventPublisher {
	return &startingEventPublisher{declaredEventPublisher: *eventPublisher(id, desc)}
}

func statePublisher(id string, desc adapter.Descriptor) *declaredStatePublisher {
	return &declaredStatePublisher{fakeStatePublisher: fakeStatePublisher{id: id}, described: described{desc}}
}

func pingingPublisher(id string, desc adapter.Descriptor) *pingingStatePublisher {
	return &pingingStatePublisher{declaredStatePublisher: *statePublisher(id, desc)}
}

// requireV8 asserts err holds exactly one problem, a V8 on field, whose
// message names the adapter type and every string in mentions.
func requireV8(t *testing.T, err error, field string, mentions ...string) {
	t.Helper()
	requireOneProblem(t, err, "V8", field)
	for _, m := range append([]string{`"fake"`}, mentions...) {
		if !strings.Contains(err.Error(), m) {
			t.Errorf("V8 error %q does not mention %q", err, m)
		}
	}
}

// Declared adapters that tell the truth pass, in every slot V8 inspects.
func TestSpecValidate_V8_TruthfulDeclarationsPass(t *testing.T) {
	spec := validSpec()
	spec.EventsStore = &declaredEventsStore{described: described{descriptor(persistence.PortEventsStore)}}
	spec.StateStore = &declaredStateStore{described: described{descriptor(persistence.PortStateStore)}}
	spec.Encryptor = &declaredEncryptor{described: described{descriptor(encryption.PortEncryptor)}}
	spec.TenantResolver = &declaredFixedTenantResolver{declaredTenantResolver{described: described{descriptor(tenancy.PortTenantResolver, tenancy.CapFixedTenant)}}}
	spec.EventPublishers = []publishing.EventPublisher{
		eventPublisher("plain", descriptor(publishing.PortEventPublisher)),
		startingPublisher("starts", descriptor(publishing.PortEventPublisher, adapter.CapStart)),
	}
	spec.StatePublishers = []publishing.StatePublisher{pingingPublisher("pings", descriptor(publishing.PortStatePublisher, adapter.CapReady))}
	if err := spec.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

// Undeclared adapters validate exactly as before V8, whatever optional
// methods they have (design §D6).
func TestSpecValidate_V8_UndeclaredAdaptersAreNotInspected(t *testing.T) {
	spec := validSpec()
	spec.TenantResolver = &undeclaredFixedTenantResolver{}
	spec.EventPublishers = []publishing.EventPublisher{&undeclaredStartingEventPublisher{fakeEventPublisher{id: "starts"}}}
	if err := spec.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

// V8a: the slot's port must be one of the descriptor's Ports. A value that
// serves several ports is valid in any of their slots.
func TestSpecValidate_V8a_SlotPortMustBeDeclared(t *testing.T) {
	t.Run("state store declaring only the events store port", func(t *testing.T) {
		spec := validSpec()
		spec.StateStore = &declaredStateStore{described: described{descriptor(persistence.PortEventsStore)}}
		requireV8(t, spec.Validate(), "StateStore", persistence.PortStateStore)
	})
	t.Run("publisher declaring the other publisher port", func(t *testing.T) {
		spec := validSpec()
		spec.EventPublishers[0] = eventPublisher("events-a", descriptor(publishing.PortStatePublisher))
		requireV8(t, spec.Validate(), "EventPublishers[0]", publishing.PortEventPublisher)
	})
	t.Run("one value serving two ports fits either slot", func(t *testing.T) {
		both := adapter.Descriptor{Ports: []adapter.Port{persistence.PortEventsStore, persistence.PortSnapshotStore}, Name: "fake"}
		spec := validSpec()
		spec.EventsStore = &declaredEventsStore{described: described{both}}
		spec.SnapshotStore = &declaredSnapshotStore{described: described{both}}
		if err := spec.Validate(); err != nil {
			t.Fatalf("Validate() = %v, want nil", err)
		}
	})
}

// V8b: declaration and method set agree in both directions for every
// optional capability compose knows for the slot's port.
func TestSpecValidate_V8b_DeclarationMatchesMethods(t *testing.T) {
	cases := []struct {
		name       string
		adjust     func(*Spec)
		field      string
		capability string
	}{
		{
			name: "declares CapStart without Start",
			adjust: func(s *Spec) {
				s.EventPublishers[0] = eventPublisher("events-a", descriptor(publishing.PortEventPublisher, adapter.CapStart))
			},
			field: "EventPublishers[0]", capability: string(adapter.CapStart),
		},
		{
			name: "implements Start without declaring CapStart",
			adjust: func(s *Spec) {
				s.EventPublishers[1] = startingPublisher("events-b", descriptor(publishing.PortEventPublisher))
			},
			field: "EventPublishers[1]", capability: string(adapter.CapStart),
		},
		{
			name: "declares CapReady without Ping",
			adjust: func(s *Spec) {
				s.StatePublishers[0] = statePublisher("states-a", descriptor(publishing.PortStatePublisher, adapter.CapReady))
			},
			field: "StatePublishers[0]", capability: string(adapter.CapReady),
		},
		{
			name: "implements Ping without declaring CapReady",
			adjust: func(s *Spec) {
				s.StatePublishers[0] = pingingPublisher("states-a", descriptor(publishing.PortStatePublisher))
			},
			field: "StatePublishers[0]", capability: string(adapter.CapReady),
		},
		{
			// Spec 3 scenario "undeclared-but-implemented fixed tenant".
			name: "implements FixedTenantResolver without declaring CapFixedTenant",
			adjust: func(s *Spec) {
				s.TenantResolver = &declaredFixedTenantResolver{declaredTenantResolver{described: described{descriptor(tenancy.PortTenantResolver)}}}
			},
			field: "TenantResolver", capability: tenancy.CapFixedTenant,
		},
		{
			name: "declares CapFixedTenant without implementing FixedTenantResolver",
			adjust: func(s *Spec) {
				s.TenantResolver = &declaredTenantResolver{described: described{descriptor(tenancy.PortTenantResolver, tenancy.CapFixedTenant)}}
			},
			field: "TenantResolver", capability: tenancy.CapFixedTenant,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec := validSpec()
			c.adjust(&spec)
			requireV8(t, spec.Validate(), c.field, c.capability)
		})
	}
}

// Capabilities the port already implies are never checked: Ping is part of
// every store port, so a store that declares CapReady, or does not, is
// fine either way.
func TestSpecValidate_V8b_ImpliedCapabilitiesAreSkipped(t *testing.T) {
	spec := validSpec()
	spec.EventsStore = &declaredEventsStore{described: described{descriptor(persistence.PortEventsStore, adapter.CapReady)}}
	spec.StateStore = &declaredStateStore{described: described{descriptor(persistence.PortStateStore)}}
	if err := spec.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

// A declared capability compose does not know for the slot's port (one a
// later issue adds) is accepted by V8; the adapter's own conformance tests
// check it (AT-1).
func TestSpecValidate_V8b_UnknownCapabilityIsAccepted(t *testing.T) {
	const future adapter.Capability = "publishing.flush"
	spec := validSpec()
	spec.EventPublishers[0] = eventPublisher("events-a", descriptor(publishing.PortEventPublisher, future))
	spec.EventsStore = &declaredEventsStore{described: described{descriptor(persistence.PortEventsStore, adapter.CapStart)}}
	if err := spec.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

// A capability marked declaration-only (the rule design §D6 fixes for the
// runtime port, F-E) is checked in one direction: declared ⇒ implemented.
func TestSpecValidate_V8b_DeclarationOnlyCapabilityIsOneDirectional(t *testing.T) {
	const port adapter.Port = publishing.PortEventPublisher
	const capability adapter.Capability = "test.declaration-only"
	implemented := false
	saved := knownCapabilities[port]
	knownCapabilities[port] = append(slices.Clone(saved), capabilityCheck{
		capability:      capability,
		implemented:     func(any) bool { return implemented },
		declarationOnly: true,
	})
	t.Cleanup(func() { knownCapabilities[port] = saved })

	implemented = true
	spec := validSpec()
	spec.EventPublishers[0] = eventPublisher("events-a", descriptor(port))
	if err := spec.Validate(); err != nil {
		t.Fatalf("implemented, undeclared: Validate() = %v, want nil", err)
	}

	implemented = false
	spec.EventPublishers[0] = eventPublisher("events-a", descriptor(port, capability))
	requireV8(t, spec.Validate(), "EventPublishers[0]", string(capability))
}

// V8c: a slot's required capabilities must be declared. The table is empty
// in v4; the test adds an entry the way #11 or #24 would.
func TestSpecValidate_V8c_RequiredCapabilities(t *testing.T) {
	if len(requiredCapabilities) != 0 {
		t.Fatalf("requiredCapabilities = %v, want empty in v4 (design §D6)", requiredCapabilities)
	}
	const port adapter.Port = publishing.PortEventPublisher
	requiredCapabilities[port] = []adapter.Capability{adapter.CapStart}
	t.Cleanup(func() { delete(requiredCapabilities, port) })

	spec := validSpec()
	spec.EventPublishers = []publishing.EventPublisher{
		startingPublisher("events-a", descriptor(port, adapter.CapStart)),
		eventPublisher("events-b", descriptor(port)),
	}
	requireV8(t, spec.Validate(), "EventPublishers[1]", string(adapter.CapStart))
}

// describeCalls counts Describe calls on countingDescribePublisher, whose
// Describe works on a nil receiver.
var describeCalls atomic.Int32

type countingDescribePublisher struct{ fakeEventPublisher }

func (*countingDescribePublisher) Describe() adapter.Descriptor {
	describeCalls.Add(1)
	return adapter.Descriptor{Name: "fake"}
}

// Spec 3 scenario "typed nil is reported once": V5 reports a typed-nil
// publisher and V8 does not call Describe on it. A value V6 rejected (a
// duplicate ID) is skipped by V8 too, so one problem gives one error.
func TestSpecValidate_V8_SkipsValuesV5AndV6Rejected(t *testing.T) {
	t.Run("typed-nil publisher", func(t *testing.T) {
		describeCalls.Store(0)
		spec := validSpec()
		spec.EventPublishers[1] = (*countingDescribePublisher)(nil)
		requireOneProblem(t, spec.Validate(), "V5", "EventPublishers[1]")
		if n := describeCalls.Load(); n != 0 {
			t.Fatalf("Describe called %d time(s) on a typed nil, want 0", n)
		}
	})
	t.Run("duplicate ID", func(t *testing.T) {
		spec := validSpec()
		spec.EventPublishers = []publishing.EventPublisher{
			eventPublisher("dup", descriptor(publishing.PortEventPublisher)),
			eventPublisher("dup", descriptor(publishing.PortStatePublisher)), // also a V8a mismatch
		}
		requireOneProblem(t, spec.Validate(), "V6", "EventPublishers[1]")
	})
	t.Run("typed-nil tenant resolver", func(t *testing.T) {
		spec := validSpec()
		spec.TenantResolver = (*declaredFixedTenantResolver)(nil)
		requireOneProblem(t, spec.Validate(), "V5", "TenantResolver")
	})
}

// V8 problems appear in Spec field order among the others, and one value
// can produce several (V8a and V8b both).
func TestSpecValidate_V8_ReportsInFieldOrder(t *testing.T) {
	spec := validSpec()
	spec.EventsStore = &declaredEventsStore{described: described{descriptor(persistence.PortStateStore)}}
	spec.TenantResolver = &declaredFixedTenantResolver{declaredTenantResolver{described: described{descriptor(persistence.PortStateStore)}}}
	spec.ShutdownTimeout = -1
	got := problems(t, spec.Validate())
	want := []struct{ rule, field string }{
		{"V8", "EventsStore"},
		{"V8", "TenantResolver"}, // V8a: wrong port
		{"V8", "TenantResolver"}, // V8b: FixedTenantResolver undeclared
		{"V7", "ShutdownTimeout"},
	}
	if len(got) != len(want) {
		t.Fatalf("Validate() reported %d problems, want %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Rule != w.rule || got[i].Field != w.field {
			t.Errorf("problem %d = {%q, %q}, want {%q, %q}", i, got[i].Rule, got[i].Field, w.rule, w.field)
		}
	}
}
