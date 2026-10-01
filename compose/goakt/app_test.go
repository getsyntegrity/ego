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

package goakt

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"
	actor "github.com/tochemey/goakt/v4/actor"

	"github.com/getsyntegrity/ego/compose"
	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/engine"
	"github.com/getsyntegrity/ego/eventstream"
	"github.com/getsyntegrity/ego/port/adapter"
	"github.com/getsyntegrity/ego/port/publishing"
	"github.com/getsyntegrity/ego/projection"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
	"github.com/getsyntegrity/ego/testkit"
)

const projectionName = "balances"

// validationError matches an error that wraps a *compose.ValidationError of
// the given rule and, when field is not empty, of the given field. A failure
// names the part that differs ("Rule: expected V7 to equal V2").
func validationError(rule, field string) specs.Matcher {
	part := func(name string, get func(*compose.ValidationError) string, want string) specs.Matcher {
		return specs.Project(name, func(err error) string {
			var ve *compose.ValidationError
			if !errors.As(err, &ve) {
				return ""
			}
			return get(ve)
		}, specs.Equal(want))
	}
	ms := []specs.Matcher{
		specs.MatchErrorAs(new(*compose.ValidationError)),
		part("Rule", func(ve *compose.ValidationError) string { return ve.Rule }, rule),
	}
	if field != "" {
		ms = append(ms, part("Field", func(ve *compose.ValidationError) string { return ve.Field }, field))
	}
	return specs.All(ms...)
}

// publisherMock is the publishing.EventPublisher and publishing.StatePublisher
// port forwarded to a mock.Controller. Method names carry the publisher id, so
// one controller serves several publishers.
type publisherMock struct {
	c  *mock.Controller
	id string
}

func (m publisherMock) ID() string { return m.id }

func (m publisherMock) Close(ctx context.Context) error {
	return m.c.Method(m.id + ".Close").Call(ctx).Err(0)
}

// expectClose declares how many times the publisher must be closed.
func (m publisherMock) expectClose(n int) {
	m.c.Method(m.id + ".Close").Expect(mock.Any()).Times(n).Return(nil)
}

type eventPublisherMock struct{ publisherMock }

func (m eventPublisherMock) Publish(ctx context.Context, event *egopb.Event) error {
	return m.c.Method(m.id+".Publish").Call(ctx, event).Err(0)
}

type statePublisherMock struct{ publisherMock }

func (m statePublisherMock) Publish(ctx context.Context, state *egopb.DurableState) error {
	return m.c.Method(m.id+".Publish").Call(ctx, state).Err(0)
}

// pingMockEventsStore is a connected testkit events store whose Ping is
// forwarded to a mock.Controller. Every other method is the real in-memory
// store.
type pingMockEventsStore struct {
	*testkit.EventStore
	c *mock.Controller
}

func (s pingMockEventsStore) Ping(ctx context.Context) error {
	return s.c.Method("EventsStore.Ping").Call(ctx).Err(0)
}

// expectPings declares how many times the events store is probed and what
// each probe returns.
func (s pingMockEventsStore) expectPings(n int, err error) {
	s.c.Method("EventsStore.Ping").Expect(mock.Any()).Times(n).Return(err)
}

// mockedFixture is a valid Spec like fixture's, whose two publishers and
// events store Ping are mocks, so a case states the calls it expects with
// Times(n) instead of reading counters. The other stores are connected
// testkit stores. It is for cases that never start the actor system.
type mockedFixture struct {
	spec   compose.Spec
	events pingMockEventsStore
	evPub  eventPublisherMock
	stPub  statePublisherMock
}

func newMockedFixture(ctx *specs.Context, name string) *mockedFixture {
	ctrl := mock.NewController(ctx) // registered first, so it verifies after the stores disconnect
	bg := context.Background()
	events := pingMockEventsStore{EventStore: testkit.NewEventsStore(), c: ctrl}
	states := testkit.NewDurableStore()
	offsets := testkit.NewOffsetStore()
	for _, connect := range []func(context.Context) error{events.Connect, states.Connect, offsets.Connect} {
		ctx.Expect(connect(bg)).To(specs.BeNil())
	}
	ctx.Cleanup(func() {
		_ = events.Disconnect(bg)
		_ = states.Disconnect(bg)
		_ = offsets.Disconnect(bg)
	})
	f := &mockedFixture{
		events: events,
		evPub:  eventPublisherMock{publisherMock{ctrl, "events-pub"}},
		stPub:  statePublisherMock{publisherMock{ctrl, "states-pub"}},
	}
	f.spec = compose.Spec{
		Name:        name,
		Families:    compose.EventSourced | compose.DurableState,
		EventsStore: events,
		StateStore:  states,
		OffsetStore: offsets,
		Projections: map[string]*projection.Options{
			projectionName: {Handler: &recordingHandler{}, BufferSize: 10, PullInterval: 10 * time.Millisecond},
		},
		EventPublishers: []publishing.EventPublisher{f.evPub},
		StatePublishers: []publishing.StatePublisher{f.stPub},
		ShutdownTimeout: 20 * time.Second,
	}
	return f
}

// expectPublisherCloses declares how many times each publisher is closed.
func (f *mockedFixture) expectPublisherCloses(n int) {
	f.evPub.expectClose(n)
	f.stPub.expectClose(n)
}

// newApp builds the App under test. It registers no Stop: these cases never
// start anything, and a Stop at the end would be a call the case did not ask for.
func newApp(ctx *specs.Context, spec compose.Spec, opts ...Option) *App {
	app, err := New(spec, append([]Option{WithLogger(engine.DiscardLogger)}, opts...)...)
	ctx.Expect(err).To(specs.BeNil())
	return app
}

// fixture is one fully wired Spec plus the fakes a test inspects.
type fixture struct {
	spec    compose.Spec
	events  *pingCountingEventsStore
	states  *writeCountingStateStore
	evPub   *eventPublisher
	stPub   *statePublisher
	handler *recordingHandler
}

// newFixture builds a valid Spec that exercises every start step: two
// families, all four kinds of store, one projection and one publisher of
// each kind.
func newFixture(t *testing.T, name string) *fixture {
	t.Helper()
	events, states, offsets := connected(t)
	f := &fixture{
		events:  events,
		states:  states,
		evPub:   newEventPublisher("events-pub"),
		stPub:   newStatePublisher("states-pub"),
		handler: &recordingHandler{},
	}
	f.spec = compose.Spec{
		Name:        name,
		Families:    compose.EventSourced | compose.DurableState,
		EventsStore: events,
		StateStore:  states,
		OffsetStore: offsets,
		Projections: map[string]*projection.Options{
			projectionName: {Handler: f.handler, BufferSize: 10, PullInterval: 10 * time.Millisecond},
		},
		EventPublishers: []publishing.EventPublisher{f.evPub},
		StatePublishers: []publishing.StatePublisher{f.stPub},
		ShutdownTimeout: 20 * time.Second,
	}
	return f
}

func mustNew(t *testing.T, spec compose.Spec, opts ...Option) *App {
	t.Helper()
	app, err := New(spec, append([]Option{WithLogger(engine.DiscardLogger)}, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	return app
}

// TestApp_ValidSpecRunsAnEngine is the end-to-end wiring test: a valid Spec
// with testkit stores reaches a running engine that spawns, answers a
// command, publishes, runs its projection, and stops cleanly (IMPL-4 row,
// first item; design §5.1 walkthrough).
func TestApp_ValidSpecRunsAnEngine(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "compose-e2e")
	app := mustNew(t, f.spec)

	if app.Engine() != nil {
		t.Fatal("Engine() before Start must be nil")
	}
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	engine := app.Engine()
	if engine == nil || !engine.Started() {
		t.Fatal("Engine() after Start must be a started engine")
	}
	sys := engine.ActorSystem()
	if sys == nil || !sys.Running() {
		t.Fatal("the actor system must be running after Start")
	}

	if err := engine.SpawnEventSourced(ctx, &account{id: "acc-1"}); err != nil {
		t.Fatalf("SpawnEventSourced: %v", err)
	}
	state, revision, err := engine.SendCommand(ctx, "acc-1", &testpb.CreateAccount{AccountBalance: 42}, waitTimeout)
	if err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	if revision != 1 || state.(*testpb.Account).GetAccountBalance() != 42 {
		t.Fatalf("SendCommand = (%v, %d), want balance 42 at revision 1", state, revision)
	}
	select {
	case evt := <-f.evPub.events:
		if evt.GetPersistenceId() != "acc-1" {
			t.Fatalf("published event for %q, want acc-1", evt.GetPersistenceId())
		}
	case <-time.After(waitTimeout):
		t.Fatal("the attached events publisher received nothing")
	}
	running, err := engine.IsProjectionRunning(ctx, projectionName)
	if err != nil || !running {
		t.Fatalf("IsProjectionRunning = (%v, %v), want the Spec's projection running", running, err)
	}

	if err := app.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if engine.Started() || sys.Running() {
		t.Fatal("Stop must stop the engine and the actor system")
	}
	if f.evPub.closed.Load() != 1 || f.stPub.closed.Load() != 1 {
		t.Fatalf("publisher closes = (%d, %d), want each closed exactly once", f.evPub.closed.Load(), f.stPub.closed.Load())
	}
}

// TestNew_MissingRequiredDependencyFailsWithNothingStarted: a Spec missing a
// required store fails at New, before any I/O, and the consumer keeps
// ownership of its publishers (design §D4a, §D5).
func TestNew_MissingRequiredDependencyFailsWithNothingStarted(t *testing.T) {
	specs.Describe(t, "New fails on a Spec missing a required store, before any I/O, and leaves publishers to the consumer", func(s *specs.Spec) {
		s.It("returns no App and a V2 ValidationError on EventsStore, and closes no publisher", func(ctx *specs.Context) {
			pub := eventPublisherMock{publisherMock{mock.NewController(ctx), "p"}}
			pub.expectClose(0)
			app, err := New(compose.Spec{
				Name:            "missing-store",
				Families:        compose.EventSourced,
				EventPublishers: []publishing.EventPublisher{pub},
			})
			ctx.Expect(app).To(specs.BeNil())
			ctx.Expect(err).To(validationError("V2", "EventsStore"))
		})
	})
}

// TestNew_StartsNothing: New on a valid Spec does no I/O; the store probe
// happens only at Start (design §D4).
func TestNew_StartsNothing(t *testing.T) {
	specs.Describe(t, "New on a valid Spec does no I/O", func(s *specs.Spec) {
		s.It("pings no store and builds no engine or actor system", func(ctx *specs.Context) {
			f := newMockedFixture(ctx, "new-starts-nothing")
			f.events.expectPings(0, nil)
			f.expectPublisherCloses(0)
			app := newApp(ctx, f.spec)
			ctx.Expect(app.Engine()).To(specs.BeNil())
			ctx.Expect(app.sys).To(specs.BeNil())
		})
	})
}

// TestNew_NegativeShutdownTimeoutFailsAtNew is maintainer decision 3: V7
// rejects a negative ShutdownTimeout at New instead of at lifecycle.New.
func TestNew_NegativeShutdownTimeoutFailsAtNew(t *testing.T) {
	specs.Describe(t, "New rejects a negative ShutdownTimeout", func(s *specs.Spec) {
		s.It("fails with a V7 ValidationError", func(ctx *specs.Context) {
			f := newFixture(ctx.T, "negative-timeout")
			f.spec.ShutdownTimeout = -time.Second
			_, err := New(f.spec)
			ctx.Expect(err).To(validationError("V7", ""))
		})
	})
}

// TestNew_G1_ClusterRequiresEntityKinds: WithCluster without entity kinds
// fails at New, not at the first remote spawn (design §D4a G1).
func TestNew_G1_ClusterRequiresEntityKinds(t *testing.T) {
	specs.Describe(t, "New rejects WithCluster without entity kinds or without a cluster config", func(s *specs.Spec) {
		s.It("reports the missing kinds and the nil config, and accepts a config with kinds", func(ctx *specs.Context) {
			f := newFixture(ctx.T, "cluster-kinds")

			_, err := New(f.spec, WithCluster(actor.NewClusterConfig()))
			ctx.Expect(err).To(specs.MatchError(ErrClusterKindsRequired))
			_, err = New(f.spec, WithCluster(nil, &wallet{}))
			ctx.Expect(err).To(specs.MatchError(ErrClusterConfigRequired))
			app, err := New(f.spec, WithCluster(actor.NewClusterConfig(), &wallet{}))
			ctx.Expect(err).To(specs.BeNil())
			ctx.Cleanup(func() { _ = app.Stop(context.Background()) })
		})
	})
}

// g2Case is one actor system name. The row is named after the name; the empty
// name has no usable subtest name, so it is called "empty name".
type g2Case struct{ row, name string }

// TestNew_G2_ActorSystemName agrees with GoAkt's own name check for every
// case, so a drift in GoAkt's rule fails here.
func TestNew_G2_ActorSystemName(t *testing.T) {
	specs.Describe(t, "New rejects an actor system name with G2 exactly when GoAkt's NewActorSystem rejects it", func(s *specs.Spec) {
		specs.Table(s, []g2Case{
			{"empty name", ""}, {"Sample", "Sample"}, {"ego-cluster", "ego-cluster"}, {"a_b-9", "a_b-9"},
			{"9lives", "9lives"}, {"-lead", "-lead"}, {"_lead", "_lead"}, {"has space", "has space"},
			{"dot.ted", "dot.ted"}, {"é", "é"},
		}, func(c g2Case) string { return c.row }, func(ctx *specs.Context, c g2Case) {
			f := newFixture(ctx.T, c.name)
			_, goaktErr := actor.NewActorSystem(c.name)

			app, err := New(f.spec)
			if app != nil {
				ctx.Cleanup(func() { _ = app.Stop(context.Background()) })
			}

			var ve *compose.ValidationError
			gotG2 := errors.As(err, &ve) && ve.Rule == "G2" && ve.Field == "Name"
			ctx.Expect(gotG2).ToEqual(goaktErr != nil)
		})
	})
}

// TestNew_ReportsEveryProblem joins Spec and GoAkt problems in one error.
func TestNew_ReportsEveryProblem(t *testing.T) {
	specs.Describe(t, "New joins Spec and GoAkt problems in one error", func(s *specs.Spec) {
		_, err := New(compose.Spec{Name: "bad name", Families: compose.Saga}, WithCluster(actor.NewClusterConfig()))
		specs.Table(s, []string{"(V2)", "(G2)", "(G1)"}, func(want string) string { return "reports " + want },
			func(ctx *specs.Context, want string) {
				ctx.Expect(err).To(specs.Not(specs.BeNil()))
				ctx.Expect(err.Error()).To(specs.Contain(want))
			})
	})
}

// TestStart_FailureAtEachStepReleasesEverything injects a failure at the
// end of each of the five start steps, after the step did its work, so each
// step must also undo its own half-start (maintainer decision 2). Every
// case must leave no running actor system, a closed event stream and every
// publisher closed exactly once (IMPL-4 row; design §D5, §D6).
func TestStart_FailureAtEachStepReleasesEverything(t *testing.T) {
	steps := []string{StepProbeStores, StepStartActorSystem, StepStartEngine, StepAttachPublishers, StepStartProjections}
	for i, step := range steps {
		t.Run(step, func(t *testing.T) {
			ctx := context.Background()
			f := newFixture(t, "fail-"+strings.ReplaceAll(step, " ", "-"))
			app := mustNew(t, f.spec)
			var stream *countingStream
			app.hooks.newEventStream = func() eventstream.Stream {
				stream = &countingStream{Stream: eventstream.New()}
				return stream
			}
			injected := errors.New("injected failure")
			app.hooks.afterStep = func(name string) error {
				if name == step {
					return injected
				}
				return nil
			}

			err := app.Start(ctx)

			var se *compose.StartError
			if !errors.As(err, &se) || se.Step != step || !errors.Is(se.Err, injected) || se.Rollback != nil {
				t.Fatalf("Start = %v, want a StartError for %q carrying the injected error and a clean rollback", err, step)
			}
			if i >= 1 { // the actor system and the stream exist from step 2 on
				if app.sys == nil || app.sys.Running() {
					t.Fatal("a failed Start must leave no running actor system")
				}
				if stream == nil || stream.closed.Load() == 0 {
					t.Fatal("a failed Start must close the event stream")
				}
			} else if stream != nil {
				t.Fatal("a failed probe must not allocate the event stream")
			}
			if f.evPub.closed.Load() != 1 || f.stPub.closed.Load() != 1 {
				t.Fatalf("publisher closes = (%d, %d), want every publisher closed exactly once", f.evPub.closed.Load(), f.stPub.closed.Load())
			}
			if app.Engine() != nil {
				t.Fatal("Engine() after a failed Start must be nil")
			}
			if err := app.Stop(ctx); err != nil {
				t.Fatalf("Stop after a failed Start = %v, want nil (no-op)", err)
			}
			if f.evPub.closed.Load() != 1 || f.stPub.closed.Load() != 1 {
				t.Fatal("Stop after a failed Start must not close anything again")
			}
		})
	}
}

// TestStart_ProbeFailureNamesTheStore uses a real Ping failure, not a hook.
func TestStart_ProbeFailureNamesTheStore(t *testing.T) {
	specs.Describe(t, "Start reports a real store Ping failure as a probe StartError naming the store", func(s *specs.Spec) {
		s.It("names EventsStore and closes every publisher", func(ctx *specs.Context) {
			f := newMockedFixture(ctx, "probe-fails")
			f.events.expectPings(1, errors.New("connection refused"))
			f.expectPublisherCloses(1)
			app := newApp(ctx, f.spec)

			err := app.Start(context.Background())

			var se *compose.StartError
			ctx.Expect(err).To(specs.MatchErrorAs(&se))
			ctx.Expect(se.Step).ToEqual(StepProbeStores)
			ctx.Expect(se.Err).To(specs.Not(specs.BeNil()))
			ctx.Expect(se.Err.Error()).To(specs.Contain("EventsStore"))
		})
	})
}

// TestStart_ActorSystemStepFailsForReal uses a real step-2 failure, no hook:
// a cluster configuration with kinds but no discovery provider and no
// remoting passes New (G1 holds) and is rejected by GoAkt inside step 2. The
// step closes the event stream it allocated, and every publisher is closed
// exactly once. No real cluster is started.
func TestStart_ActorSystemStepFailsForReal(t *testing.T) {
	f := newFixture(t, "cluster-misconfigured")
	app := mustNew(t, f.spec, WithCluster(actor.NewClusterConfig(), &wallet{}))
	var stream *countingStream
	app.hooks.newEventStream = func() eventstream.Stream {
		stream = &countingStream{Stream: eventstream.New()}
		return stream
	}

	err := app.Start(context.Background())

	var se *compose.StartError
	if !errors.As(err, &se) || se.Step != StepStartActorSystem || se.Rollback != nil {
		t.Fatalf("Start = %v, want a StartError for %q with a clean rollback", err, StepStartActorSystem)
	}
	t.Logf("step 2 failed with: %v", se.Err)
	if stream == nil || stream.closed.Load() == 0 {
		t.Fatal("step 2 must close the event stream it allocated")
	}
	if app.sys != nil && app.sys.Running() {
		t.Fatal("no actor system may be left running")
	}
	if f.evPub.closed.Load() != 1 || f.stPub.closed.Load() != 1 {
		t.Fatalf("publisher closes = (%d, %d), want each closed exactly once", f.evPub.closed.Load(), f.stPub.closed.Load())
	}
}

// TestStart_CancelledContextStartsNothing: with a context already done,
// the lifecycle's ctx.Err() check fails the first step before it runs
// (maintainer decision 1), and the publishers are still released.
func TestStart_CancelledContextStartsNothing(t *testing.T) {
	specs.Describe(t, "Start on a done context fails the first step before it runs and still releases publishers", func(s *specs.Spec) {
		s.It("fails the probe step with context.Canceled, runs nothing and closes every publisher", func(ctx *specs.Context) {
			f := newMockedFixture(ctx, "cancelled")
			f.events.expectPings(0, nil)
			f.expectPublisherCloses(1)
			app := newApp(ctx, f.spec)
			cctx, cancel := context.WithCancel(context.Background())
			cancel()

			err := app.Start(cctx)

			var se *compose.StartError
			ctx.Expect(err).To(specs.MatchErrorAs(&se))
			ctx.Expect(se.Step).ToEqual(StepProbeStores)
			ctx.Expect(err).To(specs.MatchError(context.Canceled))
			ctx.Expect(app.sys).To(specs.BeNil())
		})
	})
}

// TestStop_NeverStartedClosesPublishers: ownership moved to the App at
// New, so Stop without Start closes them (design §D5).
func TestStop_NeverStartedClosesPublishers(t *testing.T) {
	specs.Describe(t, "Stop on a never-started App closes the publishers it took ownership of at New", func(s *specs.Spec) {
		s.It("closes every publisher and touches no store", func(ctx *specs.Context) {
			f := newMockedFixture(ctx, "never-started")
			f.events.expectPings(0, nil)
			f.expectPublisherCloses(1)
			app := newApp(ctx, f.spec)
			ctx.Expect(app.Stop(context.Background())).To(specs.BeNil())
		})
	})
}

// TestStop_AfterStopIsNoOp and Start after Stop is refused.
func TestStop_AfterStopIsNoOp(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "stop-twice")
	app := mustNew(t, f.spec)
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := app.Stop(ctx); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := app.Stop(ctx); err != nil {
		t.Fatalf("second Stop = %v, want nil", err)
	}
	if f.evPub.closed.Load() != 1 || f.stPub.closed.Load() != 1 {
		t.Fatal("a second Stop must not close anything again")
	}
	if err := app.Start(ctx); !errors.Is(err, ErrNotStartable) {
		t.Fatalf("Start after Stop = %v, want ErrNotStartable", err)
	}
}

// TestStop_OrderMatchesD7 records, at the moment each publisher and the
// event stream close, whether the projection and the actor system are
// still alive: projections stop first, then the engine (publishers and the
// stream), then the actor system (design §D7).
func TestStop_OrderMatchesD7(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "stop-order")
	app := mustNew(t, f.spec)
	var stream *countingStream
	app.hooks.newEventStream = func() eventstream.Stream {
		stream = &countingStream{Stream: eventstream.New()}
		return stream
	}
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	engine := app.Engine()
	sys := engine.ActorSystem()

	var mu sync.Mutex
	var log []string
	observe := func(what string) func() {
		return func() {
			projection := "projection running"
			if pid, err := sys.ActorOf(ctx, projectionName); err != nil || pid == nil || !pid.IsRunning() {
				projection = "projection stopped"
			}
			system := "actor system stopped"
			if sys.Running() {
				system = "actor system running"
			}
			engineState := "engine stopped"
			if engine.Started() {
				engineState = "engine started"
			}
			mu.Lock()
			log = append(log, strings.Join([]string{what, projection, engineState, system}, ", "))
			mu.Unlock()
		}
	}
	f.evPub.onClose = observe("close events publisher")
	f.stPub.onClose = observe("close state publisher")
	stream.onClose = observe("close event stream")

	if err := app.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	observe("after Stop")()

	want := []string{
		"close events publisher, projection stopped, engine stopped, actor system running",
		"close state publisher, projection stopped, engine stopped, actor system running",
		"close event stream, projection stopped, engine stopped, actor system running",
	}
	// The stream may also be closed a second time by the actor-system step's
	// idempotent Close; only the first three observations are ordered by D7.
	mu.Lock()
	got := slices.Clone(log)
	mu.Unlock()
	if len(got) < len(want)+1 || !slices.Equal(got[:len(want)], want) {
		t.Fatalf("stop observations =\n%s\nwant prefix\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if last := got[len(got)-1]; last != "after Stop, projection stopped, engine stopped, actor system stopped" {
		t.Fatalf("after Stop = %q, want everything stopped", last)
	}
}

// TestStop_D7OpenQuestion_StateFlushedDuringActorShutdown reproduces the
// question design §D7 records without resolving: a durable-state actor
// persists its state again in PostStop, while the actor system stops (D7
// step 4), after the engine closed the publishers and the event stream (D7
// step 3). This test records what happens today; it does not decide the
// flush/drain policy, which belongs to #24 (LIFE-004). If that policy
// changes, update the expectation below together with it.
func TestStop_D7OpenQuestion_StateFlushedDuringActorShutdown(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "d7-open-question")
	app := mustNew(t, f.spec)
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := app.Engine().SpawnDurableState(ctx, &wallet{id: "w-1"}); err != nil {
		t.Fatalf("SpawnDurableState: %v", err)
	}
	if _, _, err := app.Engine().SendCommand(ctx, "w-1", &testpb.CreateAccount{AccountBalance: 5}, waitTimeout); err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	select {
	case <-f.stPub.states:
	case <-time.After(waitTimeout):
		t.Fatal("the state publisher did not receive the command's state")
	}
	writesBefore := f.states.writes.Load()

	if err := app.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	flushWrites := f.states.writes.Load() - writesBefore
	delivered := len(f.stPub.states) + int(f.stPub.late.Load())
	t.Logf("D7 observed: state writes during Stop = %d; states delivered to the publisher during Stop = %d", flushWrites, delivered)
	if flushWrites != 1 {
		t.Fatalf("observed %d state writes during Stop, want the 1 PostStop flush this test documents", flushWrites)
	}
	if delivered != 0 {
		t.Fatalf("observed %d states delivered during Stop, want 0: the publisher closes before the actor system stops", delivered)
	}
}

// TestEngine_UndeclaredFamilyReturnsTypedError: the Spec declares event
// sourcing only, so a durable-state spawn through either entry point
// returns engine.ErrEntityFamilyNotDeclared (design §D3).
func TestEngine_UndeclaredFamilyReturnsTypedError(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "undeclared-family")
	f.spec.Families = compose.EventSourced
	app := mustNew(t, f.spec)
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := app.Engine().SpawnDurableState(ctx, &wallet{id: "w-new"}); !errors.Is(err, engine.ErrEntityFamilyNotDeclared) {
		t.Fatalf("SpawnDurableState = %v, want ErrEntityFamilyNotDeclared", err)
	}
	//nolint:staticcheck // the deprecated entry point must enforce the same guard
	if err := app.Engine().DurableStateEntity(ctx, &wallet{id: "w-old"}); !errors.Is(err, engine.ErrEntityFamilyNotDeclared) {
		t.Fatalf("DurableStateEntity = %v, want ErrEntityFamilyNotDeclared", err)
	}
	if err := app.Engine().SpawnEventSourced(ctx, &account{id: "acc-declared"}); err != nil {
		t.Fatalf("SpawnEventSourced of a declared family = %v", err)
	}
}

// callLog records Start and Ping calls across publishers, in order.
type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(call string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, call)
}

func (l *callLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.calls)
}

// startingEventPublisher is an undeclared events publisher that implements
// adapter.Starter and adapter.Pinger. onStart, when set, runs inside Start.
type startingEventPublisher struct {
	*eventPublisher
	log      *callLog
	startErr error
	pingErr  error
	onStart  func()
}

func newStartingEventPublisher(id string, log *callLog) *startingEventPublisher {
	return &startingEventPublisher{eventPublisher: newEventPublisher(id), log: log}
}

func (p *startingEventPublisher) Start(context.Context) error {
	if p.onStart != nil {
		p.onStart()
	}
	p.log.add("start " + p.id)
	return p.startErr
}

func (p *startingEventPublisher) Ping(context.Context) error {
	p.log.add("ping " + p.id)
	return p.pingErr
}

// pingingStatePublisher is a declared state publisher that implements only
// adapter.Pinger, and says so.
type pingingStatePublisher struct {
	*statePublisher
	log     *callLog
	pingErr error
}

func (p *pingingStatePublisher) Ping(context.Context) error {
	p.log.add("ping " + p.id)
	return p.pingErr
}

func (p *pingingStatePublisher) Describe() adapter.Descriptor {
	return adapter.Descriptor{
		Ports:        []adapter.Port{publishing.PortStatePublisher},
		Name:         "fake-broker",
		Capabilities: []adapter.Capability{adapter.CapReady},
	}
}

// TestStart_AttachStepStartsAndProbesPublishersFirst: step 4 starts and
// probes every publisher, in Spec order (events publishers, then state
// publishers), before it attaches any (ego-arch-004 design §D4).
func TestStart_AttachStepStartsAndProbesPublishersFirst(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "start-and-probe")
	log := &callLog{}
	first := newStartingEventPublisher("events-1", log)
	second := newStartingEventPublisher("events-2", log)
	state := &pingingStatePublisher{statePublisher: newStatePublisher("states-1"), log: log}
	f.spec.EventPublishers = []publishing.EventPublisher{first, f.evPub, second}
	f.spec.StatePublishers = []publishing.StatePublisher{state}
	app := mustNew(t, f.spec)
	var attachedAtStart []bool
	first.onStart = func() { attachedAtStart = append(attachedAtStart, app.eventsAttached || app.statesAttached) }
	second.onStart = first.onStart

	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	want := []string{"start events-1", "ping events-1", "start events-2", "ping events-2", "ping states-1"}
	if got := log.snapshot(); !slices.Equal(got, want) {
		t.Fatalf("Start/Ping calls = %v, want %v", got, want)
	}
	if !slices.Equal(attachedAtStart, []bool{false, false}) {
		t.Fatalf("attached when Start ran = %v, want no publisher attached before every Start", attachedAtStart)
	}
	if !app.eventsAttached || !app.statesAttached {
		t.Fatal("step 4 must still attach both kinds of publisher")
	}
}

// TestStart_PublisherFailureAtK is the spec 3 scenario "failure at
// publisher k": the second of three publishers fails Start, so step 4
// fails naming it, the third is never started, the earlier steps are
// undone, and every publisher is closed exactly once (ego-arch-003 §D6).
func TestStart_PublisherFailureAtK(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "publisher-k-fails")
	log := &callLog{}
	pubs := []*startingEventPublisher{
		newStartingEventPublisher("events-1", log),
		newStartingEventPublisher("events-2", log),
		newStartingEventPublisher("events-3", log),
	}
	injected := errors.New("broker unreachable")
	pubs[1].startErr = injected
	f.spec.EventPublishers = []publishing.EventPublisher{pubs[0], pubs[1], pubs[2]}
	app := mustNew(t, f.spec)

	err := app.Start(ctx)

	var se *compose.StartError
	if !errors.As(err, &se) || se.Step != StepAttachPublishers || !errors.Is(se.Err, injected) || se.Rollback != nil {
		t.Fatalf("Start = %v, want a StartError for %q carrying the injected error and a clean rollback", err, StepAttachPublishers)
	}
	if msg := se.Err.Error(); !strings.Contains(msg, `"events-2"`) || !strings.Contains(msg, "start") || strings.Contains(msg, `"events-3"`) {
		t.Fatalf("step error %q must name the second publisher's failed Start, and nothing after it", msg)
	}
	if got, want := log.snapshot(), []string{"start events-1", "ping events-1", "start events-2"}; !slices.Equal(got, want) {
		t.Fatalf("Start/Ping calls = %v, want %v", got, want)
	}
	for _, p := range pubs {
		if n := p.closed.Load(); n != 1 {
			t.Errorf("publisher %s closed %d time(s), want exactly 1", p.id, n)
		}
	}
	if n := f.stPub.closed.Load(); n != 1 {
		t.Errorf("state publisher closed %d time(s), want exactly 1", n)
	}
	if app.sys == nil || app.sys.Running() {
		t.Fatal("a failed step 4 must leave no running actor system")
	}
	if app.Engine() != nil {
		t.Fatal("Engine() after a failed Start must be nil")
	}
	if err := app.Stop(ctx); err != nil {
		t.Fatalf("Stop after a failed Start = %v, want nil (no-op)", err)
	}
	for _, p := range pubs {
		if n := p.closed.Load(); n != 1 {
			t.Errorf("Stop after a failed Start closed publisher %s again (%d closes)", p.id, n)
		}
	}
}

// TestStart_PublisherPingFailureNamesTheAdapter: a failed Ping fails step
// 4 the same way, and a declared publisher is named by its descriptor too.
func TestStart_PublisherPingFailureNamesTheAdapter(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "publisher-ping-fails")
	log := &callLog{}
	injected := errors.New("not ready")
	state := &pingingStatePublisher{statePublisher: newStatePublisher("states-1"), log: log, pingErr: injected}
	f.spec.StatePublishers = []publishing.StatePublisher{state}
	app := mustNew(t, f.spec)

	err := app.Start(ctx)

	var se *compose.StartError
	if !errors.As(err, &se) || se.Step != StepAttachPublishers || !errors.Is(se.Err, injected) {
		t.Fatalf("Start = %v, want a StartError for %q carrying the ping error", err, StepAttachPublishers)
	}
	for _, s := range []string{"ping", `state publisher "states-1"`, `"fake-broker"`} {
		if !strings.Contains(se.Err.Error(), s) {
			t.Errorf("step error %q does not mention %q", se.Err, s)
		}
	}
	if f.evPub.closed.Load() != 1 || state.closed.Load() != 1 {
		t.Fatalf("publisher closes = (%d, %d), want every publisher closed exactly once", f.evPub.closed.Load(), state.closed.Load())
	}
}

// lyingStatePublisher declares CapStart without implementing Start.
type lyingStatePublisher struct{ statePublisherMock }

func (p *lyingStatePublisher) Describe() adapter.Descriptor {
	return adapter.Descriptor{
		Ports:        []adapter.Port{publishing.PortStatePublisher},
		Name:         "liar",
		Capabilities: []adapter.Capability{adapter.CapStart},
	}
}

// TestNew_V8RejectsALyingPublisherWithNothingStarted: New runs rule V8, so
// a declared adapter whose declaration and methods disagree fails before
// anything starts, and the consumer keeps owning its publishers.
func TestNew_V8RejectsALyingPublisherWithNothingStarted(t *testing.T) {
	specs.Describe(t, "New runs rule V8 and rejects a publisher whose declaration and methods disagree", func(s *specs.Spec) {
		s.It("returns no App and a V8 ValidationError on StatePublishers[0], pinging and closing nothing", func(ctx *specs.Context) {
			f := newMockedFixture(ctx, "lying-publisher")
			f.events.expectPings(0, nil)
			liar := &lyingStatePublisher{statePublisherMock{publisherMock{f.stPub.c, "states-1"}}}
			liar.expectClose(0)
			f.spec.StatePublishers = []publishing.StatePublisher{liar}
			app, err := New(f.spec)
			ctx.Expect(app).To(specs.BeNil())
			ctx.Expect(err).To(validationError("V8", "StatePublishers[0]"))
		})
	})
}
