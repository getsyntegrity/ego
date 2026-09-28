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

	actor "github.com/tochemey/goakt/v4/actor"

	"github.com/pablogore/ego/v4"
	"github.com/pablogore/ego/v4/compose"
	"github.com/pablogore/ego/v4/eventstream"
	"github.com/pablogore/ego/v4/port/adapter"
	"github.com/pablogore/ego/v4/port/publishing"
	"github.com/pablogore/ego/v4/projection"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
)

const projectionName = "balances"

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
	app, err := New(spec, append([]Option{WithLogger(ego.DiscardLogger)}, opts...)...)
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
	pub := newEventPublisher("p")
	app, err := New(compose.Spec{
		Name:            "missing-store",
		Families:        compose.EventSourced,
		EventPublishers: []publishing.EventPublisher{pub},
	})
	if app != nil {
		t.Fatal("New must not return an App when validation fails")
	}
	var ve *compose.ValidationError
	if !errors.As(err, &ve) || ve.Rule != "V2" || ve.Field != "EventsStore" {
		t.Fatalf("New = %v, want a V2 ValidationError on EventsStore", err)
	}
	if pub.closed.Load() != 0 {
		t.Fatal("a failed New must not close publishers: the consumer still owns them")
	}
}

// TestNew_StartsNothing: New on a valid Spec does no I/O; the store probe
// happens only at Start (design §D4).
func TestNew_StartsNothing(t *testing.T) {
	f := newFixture(t, "new-starts-nothing")
	app := mustNew(t, f.spec)
	if f.events.pings.Load() != 0 {
		t.Fatal("New must not ping stores")
	}
	if app.Engine() != nil || app.sys != nil {
		t.Fatal("New must not build an engine or an actor system")
	}
}

// TestNew_NegativeShutdownTimeoutFailsAtNew is maintainer decision 3: V7
// rejects a negative ShutdownTimeout at New instead of at lifecycle.New.
func TestNew_NegativeShutdownTimeoutFailsAtNew(t *testing.T) {
	f := newFixture(t, "negative-timeout")
	f.spec.ShutdownTimeout = -time.Second
	_, err := New(f.spec)
	var ve *compose.ValidationError
	if !errors.As(err, &ve) || ve.Rule != "V7" {
		t.Fatalf("New = %v, want a V7 ValidationError", err)
	}
}

// TestNew_G1_ClusterRequiresEntityKinds: WithCluster without entity kinds
// fails at New, not at the first remote spawn (design §D4a G1).
func TestNew_G1_ClusterRequiresEntityKinds(t *testing.T) {
	f := newFixture(t, "cluster-kinds")

	_, err := New(f.spec, WithCluster(actor.NewClusterConfig()))
	if !errors.Is(err, ErrClusterKindsRequired) {
		t.Fatalf("New without kinds = %v, want ErrClusterKindsRequired", err)
	}
	_, err = New(f.spec, WithCluster(nil, &wallet{}))
	if !errors.Is(err, ErrClusterConfigRequired) {
		t.Fatalf("New with a nil cluster config = %v, want ErrClusterConfigRequired", err)
	}
	app, err := New(f.spec, WithCluster(actor.NewClusterConfig(), &wallet{}))
	if err != nil {
		t.Fatalf("New with kinds = %v, want nil", err)
	}
	_ = app.Stop(context.Background())
}

// TestNew_G2_ActorSystemName agrees with GoAkt's own name check for every
// case, so a drift in GoAkt's rule fails here.
func TestNew_G2_ActorSystemName(t *testing.T) {
	for _, name := range []string{"", "Sample", "ego-cluster", "a_b-9", "9lives", "-lead", "_lead", "has space", "dot.ted", "é"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, name)
			_, goaktErr := actor.NewActorSystem(name)
			app, err := New(f.spec)
			var ve *compose.ValidationError
			gotG2 := errors.As(err, &ve) && ve.Rule == "G2" && ve.Field == "Name"
			if gotG2 != (goaktErr != nil) {
				t.Fatalf("New G2 = %v (err %v), GoAkt NewActorSystem err = %v: the two must agree", gotG2, err, goaktErr)
			}
			if app != nil {
				_ = app.Stop(context.Background())
			}
		})
	}
}

// TestNew_ReportsEveryProblem joins Spec and GoAkt problems in one error.
func TestNew_ReportsEveryProblem(t *testing.T) {
	_, err := New(compose.Spec{Name: "bad name", Families: compose.Saga}, WithCluster(actor.NewClusterConfig()))
	for _, want := range []string{"(V2)", "(G2)", "(G1)"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("New error %v does not report %s", err, want)
		}
	}
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
	f := newFixture(t, "probe-fails")
	f.events.pingErr = errors.New("connection refused")
	app := mustNew(t, f.spec)

	err := app.Start(context.Background())

	var se *compose.StartError
	if !errors.As(err, &se) || se.Step != StepProbeStores || !strings.Contains(se.Err.Error(), "EventsStore") {
		t.Fatalf("Start = %v, want a probe StartError naming EventsStore", err)
	}
	if f.evPub.closed.Load() != 1 || f.stPub.closed.Load() != 1 {
		t.Fatal("a failed probe must close every publisher")
	}
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
	f := newFixture(t, "cancelled")
	app := mustNew(t, f.spec)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := app.Start(ctx)

	var se *compose.StartError
	if !errors.As(err, &se) || se.Step != StepProbeStores || !errors.Is(err, context.Canceled) {
		t.Fatalf("Start = %v, want a StartError for the probe step carrying context.Canceled", err)
	}
	if f.events.pings.Load() != 0 || app.sys != nil {
		t.Fatal("nothing may run on a done context")
	}
	if f.evPub.closed.Load() != 1 || f.stPub.closed.Load() != 1 {
		t.Fatal("publishers must be released")
	}
}

// TestStop_NeverStartedClosesPublishers: ownership moved to the App at
// New, so Stop without Start closes them (design §D5).
func TestStop_NeverStartedClosesPublishers(t *testing.T) {
	f := newFixture(t, "never-started")
	app := mustNew(t, f.spec)
	if err := app.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if f.evPub.closed.Load() != 1 || f.stPub.closed.Load() != 1 {
		t.Fatal("Stop on a never-started App must close every publisher")
	}
	if f.events.pings.Load() != 0 {
		t.Fatal("Stop on a never-started App must not touch the stores")
	}
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
// returns ego.ErrEntityFamilyNotDeclared (design §D3).
func TestEngine_UndeclaredFamilyReturnsTypedError(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "undeclared-family")
	f.spec.Families = compose.EventSourced
	app := mustNew(t, f.spec)
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := app.Engine().SpawnDurableState(ctx, &wallet{id: "w-new"}); !errors.Is(err, ego.ErrEntityFamilyNotDeclared) {
		t.Fatalf("SpawnDurableState = %v, want ErrEntityFamilyNotDeclared", err)
	}
	//nolint:staticcheck // the deprecated entry point must enforce the same guard
	if err := app.Engine().DurableStateEntity(ctx, &wallet{id: "w-old"}); !errors.Is(err, ego.ErrEntityFamilyNotDeclared) {
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
type lyingStatePublisher struct{ *statePublisher }

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
	f := newFixture(t, "lying-publisher")
	liar := &lyingStatePublisher{statePublisher: newStatePublisher("states-1")}
	f.spec.StatePublishers = []publishing.StatePublisher{liar}
	app, err := New(f.spec)
	if app != nil {
		t.Fatal("New must not return an App when V8 fails")
	}
	var ve *compose.ValidationError
	if !errors.As(err, &ve) || ve.Rule != "V8" || ve.Field != "StatePublishers[0]" {
		t.Fatalf("New = %v, want a V8 ValidationError on StatePublishers[0]", err)
	}
	if f.events.pings.Load() != 0 || liar.closed.Load() != 0 {
		t.Fatal("a failed New must not ping stores or close publishers")
	}
}
