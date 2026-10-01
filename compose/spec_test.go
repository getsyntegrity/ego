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
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"

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

// problems returns every *ValidationError joined into err, in order. A nil
// error has none. A malformed error fails the case through the spec.
func problems(ctx *specs.Context, err error) []*ValidationError {
	if err == nil {
		return nil
	}
	ctx.Expect(err).To(specs.Satisfy("be a joined error", func(v any) bool {
		_, ok := v.(interface{ Unwrap() []error })
		return ok
	}))
	var out []*ValidationError
	for _, e := range err.(interface{ Unwrap() []error }).Unwrap() {
		var ve *ValidationError
		ctx.Expect(e).To(specs.MatchErrorAs(&ve))
		out = append(out, ve)
	}
	return out
}

// problem matches a *ValidationError with the given rule and field.
func problem(rule, field string) specs.Matcher {
	return specs.All(
		specs.Project("Rule", func(v *ValidationError) string { return v.Rule }, specs.Equal(rule)),
		specs.Project("Field", func(v *ValidationError) string { return v.Field }, specs.Equal(field)),
	)
}

// requireOneProblem asserts err holds exactly one *ValidationError with
// the given rule and field, and that its message names the field.
func requireOneProblem(ctx *specs.Context, err error, rule, field string) {
	ctx.Expect(problems(ctx, err)).To(specs.HaveElementsInOrder(problem(rule, field)))
	ctx.Expect(err.Error()).To(specs.Contain(field))
}

func TestSpecValidate_ValidSpecPasses(t *testing.T) {
	specs.Describe(t, "Validate accepts a Spec that uses every family, every optional field and a projection", func(s *specs.Spec) {
		s.It("reports no problem", func(ctx *specs.Context) {
			ctx.Expect(validSpec().Validate()).To(specs.BeNil())
		})
	})
}

// A Spec that declares one family needs only that family's store, and the
// optional fields may be left out entirely (a literal nil means "not
// configured").
func TestSpecValidate_MinimalSpecsPass(t *testing.T) {
	type minimalCase struct {
		name string
		spec Spec
	}
	specs.Describe(t, "Validate accepts a Spec that declares one family with only that family's store", func(s *specs.Spec) {
		specs.Table(s, []minimalCase{
			{"event sourced only", Spec{Families: EventSourced, EventsStore: &fakeEventsStore{}}},
			{"durable state only", Spec{Families: DurableState, StateStore: &fakeStateStore{}}},
			{"saga only", Spec{Families: Saga, EventsStore: &fakeEventsStore{}}},
		}, func(c minimalCase) string { return c.name }, func(ctx *specs.Context, c minimalCase) {
			ctx.Expect(c.spec.Validate()).To(specs.BeNil())
		})
	})
}

func TestSpecValidate_V1_ZeroFamilies(t *testing.T) {
	specs.Describe(t, "V1 rejects a Spec that declares no family", func(s *specs.Spec) {
		s.It("reports one V1 problem on Families", func(ctx *specs.Context) {
			spec := validSpec()
			spec.Families = 0
			requireOneProblem(ctx, spec.Validate(), "V1", "Families")
		})
	})
}

func TestSpecValidate_V2_EventsStoreRequired(t *testing.T) {
	type eventsStoreCase struct {
		name     string
		families Family
	}
	specs.Describe(t, "V2 requires an events store when an event-backed family or a projection needs one", func(s *specs.Spec) {
		specs.Table(s, []eventsStoreCase{
			{"event sourced declared", EventSourced},
			{"saga declared", Saga},
			// validSpec has a projection, which needs the events store even
			// when no event-backed family is declared.
			{"projections without an event-backed family", DurableState},
		}, func(c eventsStoreCase) string { return c.name }, func(ctx *specs.Context, c eventsStoreCase) {
			spec := validSpec()
			spec.Families = c.families
			spec.EventsStore = nil
			requireOneProblem(ctx, spec.Validate(), "V2", "EventsStore")
		})
	})
}

// Durable state alone with no projections does not need an events store.
func TestSpecValidate_V2_NotRequiredForDurableStateOnly(t *testing.T) {
	specs.Describe(t, "V2 does not require an events store for durable state without projections", func(s *specs.Spec) {
		s.It("accepts a nil events store", func(ctx *specs.Context) {
			spec := validSpec()
			spec.Families = DurableState
			spec.Projections = nil
			spec.EventsStore = nil
			ctx.Expect(spec.Validate()).To(specs.BeNil())
		})
	})
}

func TestSpecValidate_V3_StateStoreRequired(t *testing.T) {
	specs.Describe(t, "V3 requires a state store when durable state is declared", func(s *specs.Spec) {
		s.It("reports one V3 problem on StateStore", func(ctx *specs.Context) {
			spec := validSpec()
			spec.StateStore = nil
			requireOneProblem(ctx, spec.Validate(), "V3", "StateStore")
		})
	})
}

func TestSpecValidate_V3_NotRequiredWithoutDurableState(t *testing.T) {
	specs.Describe(t, "V3 does not require a state store without durable state", func(s *specs.Spec) {
		s.It("accepts a nil state store", func(ctx *specs.Context) {
			spec := validSpec()
			spec.Families = EventSourced | Saga
			spec.StateStore = nil
			ctx.Expect(spec.Validate()).To(specs.BeNil())
		})
	})
}

func TestSpecValidate_V4_Projections(t *testing.T) {
	specs.Describe(t, "V4 checks the offset store and every projection's options and handler", func(s *specs.Spec) {
		type projectionCase struct {
			name   string
			adjust func(*Spec)
			field  string
		}
		specs.Table(s, []projectionCase{
			{"offset store required", func(s *Spec) { s.OffsetStore = nil }, "OffsetStore"},
			{"nil options", func(s *Spec) { s.Projections["order-view"] = nil }, `Projections["order-view"]`},
			{"nil handler", func(s *Spec) { s.Projections["order-view"] = &projection.Options{} }, `Projections["order-view"].Handler`},
		}, func(c projectionCase) string { return c.name }, func(ctx *specs.Context, c projectionCase) {
			spec := validSpec()
			c.adjust(&spec)
			requireOneProblem(ctx, spec.Validate(), "V4", c.field)
		})
		s.It("offset store not required without projections", func(ctx *specs.Context) {
			spec := validSpec()
			spec.Projections = nil
			spec.OffsetStore = nil
			ctx.Expect(spec.Validate()).To(specs.BeNil())
		})
	})
}

// V5: a typed-nil value (a nil pointer wrapped in a non-nil interface) is
// the bug class behind the eventsStore.Ping panic in design §2.2. It is
// rejected for every interface-typed field and element, required or
// optional: an optional field left out is a literal nil, whereas a typed
// nil passes the runtime's own `!= nil` guards and panics on first use.
func TestSpecValidate_V5_TypedNilPerInterfaceField(t *testing.T) {
	type typedNilCase struct {
		field  string
		adjust func(*Spec)
	}
	cases := []typedNilCase{
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
	specs.Describe(t, "V5 rejects a typed-nil value in every interface-typed field and element", func(s *specs.Spec) {
		specs.Table(s, cases, func(c typedNilCase) string { return c.field }, func(ctx *specs.Context, c typedNilCase) {
			spec := validSpec()
			c.adjust(&spec)
			requireOneProblem(ctx, spec.Validate(), "V5", c.field)
		})
	})
}

// A literal nil element in EventAdapters is never meaningful either.
func TestSpecValidate_V5_NilEventAdapterElement(t *testing.T) {
	specs.Describe(t, "V5 rejects a literal nil element in EventAdapters", func(s *specs.Spec) {
		s.It("reports one V5 problem on EventAdapters[0]", func(ctx *specs.Context) {
			spec := validSpec()
			spec.EventAdapters = []eventadapter.EventAdapter{nil}
			requireOneProblem(ctx, spec.Validate(), "V5", "EventAdapters[0]")
		})
	})
}

func TestSpecValidate_V6_NilPublisher(t *testing.T) {
	specs.Describe(t, "V6 rejects a nil publisher of either kind", func(s *specs.Spec) {
		type nilPublisherCase struct {
			name   string
			adjust func(*Spec)
			field  string
		}
		specs.Table(s, []nilPublisherCase{
			{"events", func(s *Spec) { s.EventPublishers[0] = nil }, "EventPublishers[0]"},
			{"states", func(s *Spec) { s.StatePublishers[0] = nil }, "StatePublishers[0]"},
		}, func(c nilPublisherCase) string { return c.name }, func(ctx *specs.Context, c nilPublisherCase) {
			spec := validSpec()
			c.adjust(&spec)
			requireOneProblem(ctx, spec.Validate(), "V6", c.field)
		})
	})
}

func TestSpecValidate_V6_DuplicatePublisherIDsPerKind(t *testing.T) {
	specs.Describe(t, "V6 rejects duplicate publisher IDs within one kind", func(s *specs.Spec) {
		s.It("events", func(ctx *specs.Context) {
			spec := validSpec()
			spec.EventPublishers[1] = &fakeEventPublisher{id: "events-a"}
			err := spec.Validate()
			requireOneProblem(ctx, err, "V6", "EventPublishers[1]")
			ctx.Expect(err.Error()).To(specs.Contain(`"events-a"`))
		})
		s.It("states", func(ctx *specs.Context) {
			spec := validSpec()
			spec.StatePublishers = append(spec.StatePublishers, &fakeStatePublisher{id: "states-a"})
			requireOneProblem(ctx, spec.Validate(), "V6", "StatePublishers[1]")
		})
		// Uniqueness is per kind: the engine keys events publishers and states
		// publishers in separate maps, so the two kinds may share an ID.
		s.It("same ID across kinds is allowed", func(ctx *specs.Context) {
			spec := validSpec()
			spec.StatePublishers[0] = &fakeStatePublisher{id: "events-a"}
			ctx.Expect(spec.Validate()).To(specs.BeNil())
		})
	})
}

// V7: a negative ShutdownTimeout is rejected at Validate, so a composition
// root fails at New instead of when it builds its lifecycle sequence. Zero
// (the default) and any positive value pass.
func TestSpecValidate_V7_NegativeShutdownTimeout(t *testing.T) {
	type acceptedCase struct {
		name string
		d    time.Duration
	}
	specs.Describe(t, "V7 rejects a negative ShutdownTimeout and accepts zero and positive ones", func(s *specs.Spec) {
		s.It("negative is rejected", func(ctx *specs.Context) {
			spec := validSpec()
			spec.ShutdownTimeout = -time.Second
			requireOneProblem(ctx, spec.Validate(), "V7", "ShutdownTimeout")
		})
		specs.Table(s, []acceptedCase{
			{"zero", 0},
			{"one nanosecond", time.Nanosecond},
			{"one minute", time.Minute},
		}, func(c acceptedCase) string { return c.name + " is accepted" }, func(ctx *specs.Context, c acceptedCase) {
			spec := validSpec()
			spec.ShutdownTimeout = c.d
			ctx.Expect(spec.Validate()).To(specs.BeNil())
		})
	})
}

// Validate reports every problem at once instead of stopping at the first,
// in a deterministic order: Spec field order, projections sorted by name.
func TestSpecValidate_ReportsEveryProblem(t *testing.T) {
	specs.Describe(t, "Validate reports every problem at once, in Spec field order", func(s *specs.Spec) {
		s.It("lists nine problems in order and names each field in the joined error", func(ctx *specs.Context) {
			spec := Spec{
				Projections:     map[string]*projection.Options{"b-view": {}, "a-view": nil},
				SnapshotStore:   (*fakeSnapshotStore)(nil),
				EventPublishers: []publishing.EventPublisher{&fakeEventPublisher{id: "x"}, &fakeEventPublisher{id: "x"}},
				StatePublishers: []publishing.StatePublisher{nil},
				ShutdownTimeout: -1,
			}
			err := spec.Validate()

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
			inOrder := make([]specs.Matcher, len(want))
			fields := make([]any, len(want))
			for i, w := range want {
				inOrder[i] = problem(w.rule, w.field)
				fields[i] = w.field
			}
			ctx.Expect(problems(ctx, err)).To(specs.HaveElementsInOrder(inOrder...))
			ctx.Expect(err.Error()).To(specs.ContainAllOf(fields...))
		})
	})
}

func TestStartError(t *testing.T) {
	cause := errors.New("store unreachable")
	undo := errors.New("actor system did not stop")

	specs.Describe(t, "StartError names the failed step and unwraps to its cause and rollback error", func(s *specs.Spec) {
		s.It("without rollback error", func(ctx *specs.Context) {
			err := error(&StartError{Step: "probe", Err: cause})
			ctx.Expect(err).To(specs.MatchError(cause))
			msg := err.Error()
			ctx.Expect(msg).To(specs.ContainAllOf("probe", cause.Error()))
			ctx.Expect(msg).To(specs.Not(specs.Contain("rollback")))
		})
		s.It("with rollback error", func(ctx *specs.Context) {
			err := error(&StartError{Step: "attach publishers", Err: cause, Rollback: undo})
			ctx.Expect(err).To(specs.MatchError(cause))
			ctx.Expect(err).To(specs.MatchError(undo))
			msg := err.Error()
			ctx.Expect(msg).To(specs.ContainAllOf("attach publishers", cause.Error(), undo.Error()))
			var se *StartError
			ctx.Expect(err).To(specs.MatchErrorAs(&se))
			ctx.Expect(se.Step).ToEqual("attach publishers")
		})
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
func requireV8(ctx *specs.Context, err error, field string, mentions ...string) {
	requireOneProblem(ctx, err, "V8", field)
	want := []any{`"fake"`}
	for _, m := range mentions {
		want = append(want, m)
	}
	ctx.Expect(err.Error()).To(specs.ContainAllOf(want...))
}

// Declared adapters that tell the truth pass, in every slot V8 inspects.
func TestSpecValidate_V8_TruthfulDeclarationsPass(t *testing.T) {
	specs.Describe(t, "V8 accepts declared adapters whose declaration matches their ports and methods", func(s *specs.Spec) {
		s.It("passes in every slot V8 inspects", func(ctx *specs.Context) {
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
			ctx.Expect(spec.Validate()).To(specs.BeNil())
		})
	})
}

// Undeclared adapters validate exactly as before V8, whatever optional
// methods they have (design §D6).
func TestSpecValidate_V8_UndeclaredAdaptersAreNotInspected(t *testing.T) {
	specs.Describe(t, "V8 does not inspect adapters that declare no descriptor", func(s *specs.Spec) {
		s.It("accepts undeclared adapters with optional methods", func(ctx *specs.Context) {
			spec := validSpec()
			spec.TenantResolver = &undeclaredFixedTenantResolver{}
			spec.EventPublishers = []publishing.EventPublisher{&undeclaredStartingEventPublisher{fakeEventPublisher{id: "starts"}}}
			ctx.Expect(spec.Validate()).To(specs.BeNil())
		})
	})
}

// V8a: the slot's port must be one of the descriptor's Ports. A value that
// serves several ports is valid in any of their slots.
func TestSpecValidate_V8a_SlotPortMustBeDeclared(t *testing.T) {
	specs.Describe(t, "V8a requires the slot's port to be one of the descriptor's Ports", func(s *specs.Spec) {
		s.It("state store declaring only the events store port", func(ctx *specs.Context) {
			spec := validSpec()
			spec.StateStore = &declaredStateStore{described: described{descriptor(persistence.PortEventsStore)}}
			requireV8(ctx, spec.Validate(), "StateStore", persistence.PortStateStore)
		})
		s.It("publisher declaring the other publisher port", func(ctx *specs.Context) {
			spec := validSpec()
			spec.EventPublishers[0] = eventPublisher("events-a", descriptor(publishing.PortStatePublisher))
			requireV8(ctx, spec.Validate(), "EventPublishers[0]", publishing.PortEventPublisher)
		})
		s.It("one value serving two ports fits either slot", func(ctx *specs.Context) {
			both := adapter.Descriptor{Ports: []adapter.Port{persistence.PortEventsStore, persistence.PortSnapshotStore}, Name: "fake"}
			spec := validSpec()
			spec.EventsStore = &declaredEventsStore{described: described{both}}
			spec.SnapshotStore = &declaredSnapshotStore{described: described{both}}
			ctx.Expect(spec.Validate()).To(specs.BeNil())
		})
	})
}

// V8b: declaration and method set agree in both directions for every
// optional capability compose knows for the slot's port.
func TestSpecValidate_V8b_DeclarationMatchesMethods(t *testing.T) {
	type declarationCase struct {
		name       string
		adjust     func(*Spec)
		field      string
		capability string
	}
	cases := []declarationCase{
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
	specs.Describe(t, "V8b requires a declaration and its method set to agree in both directions", func(s *specs.Spec) {
		specs.Table(s, cases, func(c declarationCase) string { return c.name }, func(ctx *specs.Context, c declarationCase) {
			spec := validSpec()
			c.adjust(&spec)
			requireV8(ctx, spec.Validate(), c.field, c.capability)
		})
	})
}

// Capabilities the port already implies are never checked: Ping is part of
// every store port, so a store that declares CapReady, or does not, is
// fine either way.
func TestSpecValidate_V8b_ImpliedCapabilitiesAreSkipped(t *testing.T) {
	specs.Describe(t, "V8b skips capabilities the port already implies", func(s *specs.Spec) {
		s.It("accepts a store that declares CapReady and one that does not", func(ctx *specs.Context) {
			spec := validSpec()
			spec.EventsStore = &declaredEventsStore{described: described{descriptor(persistence.PortEventsStore, adapter.CapReady)}}
			spec.StateStore = &declaredStateStore{described: described{descriptor(persistence.PortStateStore)}}
			ctx.Expect(spec.Validate()).To(specs.BeNil())
		})
	})
}

// A declared capability compose does not know for the slot's port (one a
// later issue adds) is accepted by V8; the adapter's own conformance tests
// check it (AT-1).
func TestSpecValidate_V8b_UnknownCapabilityIsAccepted(t *testing.T) {
	specs.Describe(t, "V8b accepts a declared capability compose does not know for the slot's port", func(s *specs.Spec) {
		s.It("accepts unknown capabilities", func(ctx *specs.Context) {
			const future adapter.Capability = "publishing.flush"
			spec := validSpec()
			spec.EventPublishers[0] = eventPublisher("events-a", descriptor(publishing.PortEventPublisher, future))
			spec.EventsStore = &declaredEventsStore{described: described{descriptor(persistence.PortEventsStore, adapter.CapStart)}}
			ctx.Expect(spec.Validate()).To(specs.BeNil())
		})
	})
}

// A capability marked declaration-only (the rule design §D6 fixes for the
// runtime port, F-E) is checked in one direction: declared ⇒ implemented.
func TestSpecValidate_V8b_DeclarationOnlyCapabilityIsOneDirectional(t *testing.T) {
	specs.Describe(t, "V8b checks a declaration-only capability in one direction only", func(s *specs.Spec) {
		// The case registers a capability check in the package-level table;
		// its own Cleanup restores the table, so cases stay independent.
		s.It("accepts implemented-but-undeclared and rejects declared-but-unimplemented", func(ctx *specs.Context) {
			const port adapter.Port = publishing.PortEventPublisher
			const capability adapter.Capability = "test.declaration-only"
			implemented := false
			saved := knownCapabilities[port]
			knownCapabilities[port] = append(slices.Clone(saved), capabilityCheck{
				capability:      capability,
				implemented:     func(any) bool { return implemented },
				declarationOnly: true,
			})
			ctx.Cleanup(func() { knownCapabilities[port] = saved })

			implemented = true
			spec := validSpec()
			spec.EventPublishers[0] = eventPublisher("events-a", descriptor(port))
			ctx.Expect(spec.Validate()).To(specs.BeNil())

			implemented = false
			spec.EventPublishers[0] = eventPublisher("events-a", descriptor(port, capability))
			requireV8(ctx, spec.Validate(), "EventPublishers[0]", string(capability))
		})
	})
}

// V8c: a slot's required capabilities must be declared. The table is empty
// in v4; the test adds an entry the way #11 or #24 would.
func TestSpecValidate_V8c_RequiredCapabilities(t *testing.T) {
	specs.Describe(t, "V8c requires a slot's required capabilities to be declared", func(s *specs.Spec) {
		// The case adds an entry to the package-level table; its own Cleanup
		// removes it, so cases stay independent.
		s.It("rejects a publisher that omits a required capability", func(ctx *specs.Context) {
			ctx.Expect(requiredCapabilities).To(specs.BeEmpty()) // empty in v4 (design §D6)
			const port adapter.Port = publishing.PortEventPublisher
			requiredCapabilities[port] = []adapter.Capability{adapter.CapStart}
			ctx.Cleanup(func() { delete(requiredCapabilities, port) })

			spec := validSpec()
			spec.EventPublishers = []publishing.EventPublisher{
				startingPublisher("events-a", descriptor(port, adapter.CapStart)),
				eventPublisher("events-b", descriptor(port)),
			}
			requireV8(ctx, spec.Validate(), "EventPublishers[1]", string(adapter.CapStart))
		})
	})
}

// describeCtrl is the controller of the case that is running. The typed-nil
// publisher below has no state of its own, since its Describe runs on a nil
// receiver, so the controller is reached through this variable. The case sets
// it and restores it with ctx.Cleanup.
var describeCtrl *mock.Controller

type countingDescribePublisher struct{ fakeEventPublisher }

func (*countingDescribePublisher) Describe() adapter.Descriptor {
	return mock.Value[adapter.Descriptor](describeCtrl.Method("Describe").Call(), 0)
}

// Spec 3 scenario "typed nil is reported once": V5 reports a typed-nil
// publisher and V8 does not call Describe on it. A value V6 rejected (a
// duplicate ID) is skipped by V8 too, so one problem gives one error.
func TestSpecValidate_V8_SkipsValuesV5AndV6Rejected(t *testing.T) {
	specs.Describe(t, "V8 skips values that V5 or V6 already rejected", func(s *specs.Spec) {
		s.It("typed-nil publisher", func(ctx *specs.Context) {
			describeCtrl = mock.NewController(ctx)
			ctx.Cleanup(func() { describeCtrl = nil })
			describeCtrl.Method("Describe").Expect().Never()
			spec := validSpec()
			spec.EventPublishers[1] = (*countingDescribePublisher)(nil)
			requireOneProblem(ctx, spec.Validate(), "V5", "EventPublishers[1]")
		})
		s.It("duplicate ID", func(ctx *specs.Context) {
			spec := validSpec()
			spec.EventPublishers = []publishing.EventPublisher{
				eventPublisher("dup", descriptor(publishing.PortEventPublisher)),
				eventPublisher("dup", descriptor(publishing.PortStatePublisher)), // also a V8a mismatch
			}
			requireOneProblem(ctx, spec.Validate(), "V6", "EventPublishers[1]")
		})
		s.It("typed-nil tenant resolver", func(ctx *specs.Context) {
			spec := validSpec()
			spec.TenantResolver = (*declaredFixedTenantResolver)(nil)
			requireOneProblem(ctx, spec.Validate(), "V5", "TenantResolver")
		})
	})
}

// V8 problems appear in Spec field order among the others, and one value
// can produce several (V8a and V8b both).
func TestSpecValidate_V8_ReportsInFieldOrder(t *testing.T) {
	specs.Describe(t, "V8 problems appear in Spec field order among the others", func(s *specs.Spec) {
		s.It("reports V8a and V8b for one value, then V7", func(ctx *specs.Context) {
			spec := validSpec()
			spec.EventsStore = &declaredEventsStore{described: described{descriptor(persistence.PortStateStore)}}
			spec.TenantResolver = &declaredFixedTenantResolver{declaredTenantResolver{described: described{descriptor(persistence.PortStateStore)}}}
			spec.ShutdownTimeout = -1
			ctx.Expect(problems(ctx, spec.Validate())).To(specs.HaveElementsInOrder(
				problem("V8", "EventsStore"),
				problem("V8", "TenantResolver"), // V8a: wrong port
				problem("V8", "TenantResolver"), // V8b: FixedTenantResolver undeclared
				problem("V7", "ShutdownTimeout"),
			))
		})
	})
}
