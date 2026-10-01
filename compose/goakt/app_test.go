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
	testpb "github.com/getsyntegrity/ego/internal/testpb"
	"github.com/getsyntegrity/ego/port/adapter"
	"github.com/getsyntegrity/ego/port/publishing"
	"github.com/getsyntegrity/ego/projection"
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
func newFixture(ctx *specs.Context, name string) *fixture {
	ctx.Helper()
	events, states, offsets := connected(ctx)
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

func mustNew(ctx *specs.Context, spec compose.Spec, opts ...Option) *App {
	ctx.Helper()
	app, err := New(spec, append([]Option{WithLogger(engine.DiscardLogger)}, opts...)...)
	ctx.Expect(err).To(specs.BeNil())
	// T.Cleanup, not ctx.Cleanup: a BeforeAll hook builds the cluster nodes, and they
	// must outlive that hook, so the Describe's T owns them.
	ctx.T.Cleanup(func() { _ = app.Stop(context.Background()) })
	return app
}

// TestApp_ValidSpecRunsAnEngine is the end-to-end wiring test: a valid Spec
// with testkit stores reaches a running engine that spawns, answers a
// command, publishes, runs its projection, and stops cleanly (IMPL-4 row,
// first item; design §5.1 walkthrough).
func TestApp_ValidSpecRunsAnEngine(t *testing.T) {
	specs.Describe(t, "A valid Spec reaches a running engine", func(s *specs.Spec) {
		s.It("spawns, answers a command, publishes, runs its projection and stops cleanly", func(ctx *specs.Context) {
			bg := context.Background()
			f := newFixture(ctx, "compose-e2e")
			app := mustNew(ctx, f.spec)

			ctx.Expect(app.Engine() == nil).To(specs.BeTrue())
			ctx.Expect(app.Start(bg)).To(specs.BeNil())
			engine := app.Engine()
			ctx.Expect(engine != nil && engine.Started()).To(specs.BeTrue())
			sys := engine.ActorSystem()
			ctx.Expect(sys != nil && sys.Running()).To(specs.BeTrue())

			ctx.Expect(engine.SpawnEventSourced(bg, &account{id: "acc-1"})).To(specs.BeNil())
			state, revision, err := engine.SendCommand(bg, "acc-1", &testpb.CreateAccount{AccountBalance: 42}, waitTimeout)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(revision).To(specs.Equal(uint64(1)))
			ctx.Expect(state.(*testpb.Account).GetAccountBalance()).To(specs.Equal(float64(42)))
			var published *egopb.Event
			select {
			case published = <-f.evPub.events:
			case <-time.After(waitTimeout):
			}
			ctx.Expect(published != nil).To(specs.BeTrue())
			ctx.Expect(published.GetPersistenceId()).To(specs.Equal("acc-1"))
			running, err := engine.IsProjectionRunning(bg, projectionName)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(running).To(specs.BeTrue())

			ctx.Expect(app.Stop(bg)).To(specs.BeNil())
			ctx.Expect(engine.Started() || sys.Running()).To(specs.BeFalse())
			ctx.Expect(f.evPub.closed.Load()).To(specs.Equal(int32(1)))
			ctx.Expect(f.stPub.closed.Load()).To(specs.Equal(int32(1)))
		})
	})
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
			f := newFixture(ctx, "negative-timeout")
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
			f := newFixture(ctx, "cluster-kinds")

			_, err := New(f.spec, WithCluster(actor.NewClusterConfig()))
			ctx.Expect(err).To(specs.MatchError(ErrClusterKindsRequired))
			_, err = New(f.spec, WithCluster(nil, &wallet{}))
			ctx.Expect(err).To(specs.MatchError(ErrClusterConfigRequired))
			app, err := New(f.spec, WithCluster(actor.NewClusterConfig(), &wallet{}))
			ctx.Expect(err).To(specs.BeNil())
			// T.Cleanup, not ctx.Cleanup: a BeforeAll hook builds the cluster nodes, and they
			// must outlive that hook, so the Describe's T owns them.
			ctx.T.Cleanup(func() { _ = app.Stop(context.Background()) })
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
			f := newFixture(ctx, c.name)
			_, goaktErr := actor.NewActorSystem(c.name)

			app, err := New(f.spec)
			if app != nil {
				// T.Cleanup, not ctx.Cleanup: a BeforeAll hook builds the cluster nodes, and they
				// must outlive that hook, so the Describe's T owns them.
				ctx.T.Cleanup(func() { _ = app.Stop(context.Background()) })
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
	specs.Describe(t, "", func(s *specs.Spec) {
		for i, step := range steps {
			s.It(step, func(ctx *specs.Context) {
				bg := context.Background()
				f := newFixture(ctx, "fail-"+strings.ReplaceAll(step, " ", "-"))
				app := mustNew(ctx, f.spec)
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

				err := app.Start(bg)

				var se *compose.StartError
				ctx.Expect(err).To(specs.MatchErrorAs(&se))
				ctx.Expect(se.Step).To(specs.Equal(step))
				ctx.Expect(se.Err).To(specs.MatchError(injected))
				ctx.Expect(se.Rollback).To(specs.BeNil())
				if i >= 1 { // the actor system and the stream exist from step 2 on
					ctx.Expect(app.sys != nil && !app.sys.Running()).To(specs.BeTrue())
					ctx.Expect(stream != nil && stream.closed.Load() > 0).To(specs.BeTrue())
				} else {
					ctx.Expect(stream == nil).To(specs.BeTrue())
				}
				ctx.Expect(f.evPub.closed.Load()).To(specs.Equal(int32(1)))
				ctx.Expect(f.stPub.closed.Load()).To(specs.Equal(int32(1)))
				ctx.Expect(app.Engine() == nil).To(specs.BeTrue())
				ctx.Expect(app.Stop(bg)).To(specs.BeNil())
				ctx.Expect(f.evPub.closed.Load()).To(specs.Equal(int32(1)))
				ctx.Expect(f.stPub.closed.Load()).To(specs.Equal(int32(1)))
			})
		}
	})
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
	specs.Describe(t, "Start fails the actor system step for real on a misconfigured cluster", func(s *specs.Spec) {
		s.It("closes the event stream it allocated, leaves no actor system running and closes every publisher once", func(ctx *specs.Context) {
			f := newFixture(ctx, "cluster-misconfigured")
			app := mustNew(ctx, f.spec, WithCluster(actor.NewClusterConfig(), &wallet{}))
			var stream *countingStream
			app.hooks.newEventStream = func() eventstream.Stream {
				stream = &countingStream{Stream: eventstream.New()}
				return stream
			}

			err := app.Start(context.Background())

			var se *compose.StartError
			ctx.Expect(err).To(specs.MatchErrorAs(&se))
			ctx.Expect(se.Step).To(specs.Equal(StepStartActorSystem))
			ctx.Expect(se.Rollback).To(specs.BeNil())
			ctx.T.Logf("step 2 failed with: %v", se.Err)
			ctx.Expect(stream != nil && stream.closed.Load() > 0).To(specs.BeTrue())
			ctx.Expect(app.sys != nil && app.sys.Running()).To(specs.BeFalse())
			ctx.Expect(f.evPub.closed.Load()).To(specs.Equal(int32(1)))
			ctx.Expect(f.stPub.closed.Load()).To(specs.Equal(int32(1)))
		})
	})
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
	specs.Describe(t, "Stop after a Stop is a no-op", func(s *specs.Spec) {
		s.It("closes nothing twice and refuses a Start after Stop", func(ctx *specs.Context) {
			bg := context.Background()
			f := newFixture(ctx, "stop-twice")
			app := mustNew(ctx, f.spec)
			ctx.Expect(app.Start(bg)).To(specs.BeNil())
			ctx.Expect(app.Stop(bg)).To(specs.BeNil())
			ctx.Expect(app.Stop(bg)).To(specs.BeNil())
			ctx.Expect(f.evPub.closed.Load()).To(specs.Equal(int32(1)))
			ctx.Expect(f.stPub.closed.Load()).To(specs.Equal(int32(1)))
			ctx.Expect(app.Start(bg)).To(specs.MatchError(ErrNotStartable))
		})
	})
}

// TestStop_OrderMatchesD7 records, at the moment each publisher and the
// event stream close, whether the projection and the actor system are
// still alive: projections stop first, then the engine (publishers and the
// stream), then the actor system (design §D7).
func TestStop_OrderMatchesD7(t *testing.T) {
	specs.Describe(t, "Stop follows the D7 order", func(s *specs.Spec) {
		s.It("closes publishers and the stream after the projection and engine, before the actor system", func(ctx *specs.Context) {
			bg := context.Background()
			f := newFixture(ctx, "stop-order")
			app := mustNew(ctx, f.spec)
			var stream *countingStream
			app.hooks.newEventStream = func() eventstream.Stream {
				stream = &countingStream{Stream: eventstream.New()}
				return stream
			}
			ctx.Expect(app.Start(bg)).To(specs.BeNil())
			engine := app.Engine()
			sys := engine.ActorSystem()

			var mu sync.Mutex
			var log []string
			observe := func(what string) func() {
				return func() {
					projection := "projection running"
					if pid, err := sys.ActorOf(bg, projectionName); err != nil || pid == nil || !pid.IsRunning() {
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

			ctx.Expect(app.Stop(bg)).To(specs.BeNil())
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
			ctx.Expect(len(got)).To(specs.BeGreaterThan(len(want)))
			ctx.Expect(got[:len(want)]).To(specs.Equal(want))
			ctx.Expect(got[len(got)-1]).To(specs.Equal("after Stop, projection stopped, engine stopped, actor system stopped"))
		})
	})
}

// TestStop_D7OpenQuestion_StateFlushedDuringActorShutdown reproduces the
// question design §D7 records without resolving: a durable-state actor
// persists its state again in PostStop, while the actor system stops (D7
// step 4), after the engine closed the publishers and the event stream (D7
// step 3). This test records what happens today; it does not decide the
// flush/drain policy, which belongs to #24 (LIFE-004). If that policy
// changes, update the expectation below together with it.
func TestStop_D7OpenQuestion_StateFlushedDuringActorShutdown(t *testing.T) {
	specs.Describe(t, "Stop flushes durable state once while the actor system shuts down (design D7 open question)", func(s *specs.Spec) {
		s.It("writes the state once in PostStop and delivers none to the already closed publisher", func(ctx *specs.Context) {
			bg := context.Background()
			f := newFixture(ctx, "d7-open-question")
			app := mustNew(ctx, f.spec)
			ctx.Expect(app.Start(bg)).To(specs.BeNil())
			ctx.Expect(app.Engine().SpawnDurableState(bg, &wallet{id: "w-1"})).To(specs.BeNil())
			_, _, err := app.Engine().SendCommand(bg, "w-1", &testpb.CreateAccount{AccountBalance: 5}, waitTimeout)
			ctx.Expect(err).To(specs.BeNil())
			var received *egopb.DurableState
			select {
			case received = <-f.stPub.states:
			case <-time.After(waitTimeout):
			}
			ctx.Expect(received != nil).To(specs.BeTrue())
			writesBefore := f.states.writes.Load()

			ctx.Expect(app.Stop(bg)).To(specs.BeNil())

			flushWrites := f.states.writes.Load() - writesBefore
			delivered := len(f.stPub.states) + int(f.stPub.late.Load())
			ctx.T.Logf("D7 observed: state writes during Stop = %d; states delivered to the publisher during Stop = %d", flushWrites, delivered)
			// The 1 PostStop flush this test documents.
			ctx.Expect(flushWrites).To(specs.Equal(int32(1)))
			// The publisher closes before the actor system stops.
			ctx.Expect(delivered).To(specs.Equal(0))
		})
	})
}

// TestEngine_UndeclaredFamilyReturnsTypedError: the Spec declares event
// sourcing only, so a durable-state spawn through either entry point
// returns engine.ErrEntityFamilyNotDeclared (design §D3).
func TestEngine_UndeclaredFamilyReturnsTypedError(t *testing.T) {
	specs.Describe(t, "A durable-state spawn on a Spec declaring only event sourcing", func(s *specs.Spec) {
		s.It("returns ErrEntityFamilyNotDeclared through both entry points and still spawns the declared family", func(ctx *specs.Context) {
			bg := context.Background()
			f := newFixture(ctx, "undeclared-family")
			f.spec.Families = compose.EventSourced
			app := mustNew(ctx, f.spec)
			ctx.Expect(app.Start(bg)).To(specs.BeNil())
			ctx.Expect(app.Engine().SpawnDurableState(bg, &wallet{id: "w-new"})).To(specs.MatchError(engine.ErrEntityFamilyNotDeclared))
			//nolint:staticcheck // the deprecated entry point must enforce the same guard
			ctx.Expect(app.Engine().DurableStateEntity(bg, &wallet{id: "w-old"})).To(specs.MatchError(engine.ErrEntityFamilyNotDeclared))
			ctx.Expect(app.Engine().SpawnEventSourced(bg, &account{id: "acc-declared"})).To(specs.BeNil())
		})
	})
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
	specs.Describe(t, "Start step 4 starts and probes every publisher before it attaches any", func(s *specs.Spec) {
		s.It("calls Start and Ping in Spec order and attaches only afterwards", func(ctx *specs.Context) {
			bg := context.Background()
			f := newFixture(ctx, "start-and-probe")
			log := &callLog{}
			first := newStartingEventPublisher("events-1", log)
			second := newStartingEventPublisher("events-2", log)
			state := &pingingStatePublisher{statePublisher: newStatePublisher("states-1"), log: log}
			f.spec.EventPublishers = []publishing.EventPublisher{first, f.evPub, second}
			f.spec.StatePublishers = []publishing.StatePublisher{state}
			app := mustNew(ctx, f.spec)
			var attachedAtStart []bool
			first.onStart = func() { attachedAtStart = append(attachedAtStart, app.eventsAttached || app.statesAttached) }
			second.onStart = first.onStart

			ctx.Expect(app.Start(bg)).To(specs.BeNil())

			want := []string{"start events-1", "ping events-1", "start events-2", "ping events-2", "ping states-1"}
			ctx.Expect(log.snapshot()).To(specs.Equal(want))
			// No publisher is attached before every Start has run.
			ctx.Expect(attachedAtStart).To(specs.Equal([]bool{false, false}))
			ctx.Expect(app.eventsAttached && app.statesAttached).To(specs.BeTrue())
		})
	})
}

// TestStart_PublisherFailureAtK is the spec 3 scenario "failure at
// publisher k": the second of three publishers fails Start, so step 4
// fails naming it, the third is never started, the earlier steps are
// undone, and every publisher is closed exactly once (ego-arch-003 §D6).
func TestStart_PublisherFailureAtK(t *testing.T) {
	specs.Describe(t, "Start fails at the k-th publisher", func(s *specs.Spec) {
		s.It("names the failed publisher, never starts the next, undoes earlier steps and closes every publisher once", func(ctx *specs.Context) {
			bg := context.Background()
			f := newFixture(ctx, "publisher-k-fails")
			log := &callLog{}
			pubs := []*startingEventPublisher{
				newStartingEventPublisher("events-1", log),
				newStartingEventPublisher("events-2", log),
				newStartingEventPublisher("events-3", log),
			}
			injected := errors.New("broker unreachable")
			pubs[1].startErr = injected
			f.spec.EventPublishers = []publishing.EventPublisher{pubs[0], pubs[1], pubs[2]}
			app := mustNew(ctx, f.spec)

			err := app.Start(bg)

			var se *compose.StartError
			ctx.Expect(err).To(specs.MatchErrorAs(&se))
			ctx.Expect(se.Step).To(specs.Equal(StepAttachPublishers))
			ctx.Expect(se.Err).To(specs.MatchError(injected))
			ctx.Expect(se.Rollback).To(specs.BeNil())
			// The step error names the second publisher's failed Start, and nothing after it.
			msg := se.Err.Error()
			ctx.Expect(msg).To(specs.Contain(`"events-2"`))
			ctx.Expect(msg).To(specs.Contain("start"))
			ctx.Expect(msg).To(specs.Not(specs.Contain(`"events-3"`)))
			ctx.Expect(log.snapshot()).To(specs.Equal([]string{"start events-1", "ping events-1", "start events-2"}))
			for _, p := range pubs {
				ctx.Expect(p.closed.Load()).To(specs.Equal(int32(1)))
			}
			ctx.Expect(f.stPub.closed.Load()).To(specs.Equal(int32(1)))
			ctx.Expect(app.sys != nil && !app.sys.Running()).To(specs.BeTrue())
			ctx.Expect(app.Engine() == nil).To(specs.BeTrue())
			ctx.Expect(app.Stop(bg)).To(specs.BeNil())
			for _, p := range pubs {
				ctx.Expect(p.closed.Load()).To(specs.Equal(int32(1)))
			}
		})
	})
}

// TestStart_PublisherPingFailureNamesTheAdapter: a failed Ping fails step
// 4 the same way, and a declared publisher is named by its descriptor too.
func TestStart_PublisherPingFailureNamesTheAdapter(t *testing.T) {
	specs.Describe(t, "Start fails step 4 on a publisher Ping failure", func(s *specs.Spec) {
		s.It("names the adapter by its descriptor and closes every publisher once", func(ctx *specs.Context) {
			f := newFixture(ctx, "publisher-ping-fails")
			log := &callLog{}
			injected := errors.New("not ready")
			state := &pingingStatePublisher{statePublisher: newStatePublisher("states-1"), log: log, pingErr: injected}
			f.spec.StatePublishers = []publishing.StatePublisher{state}
			app := mustNew(ctx, f.spec)

			err := app.Start(context.Background())

			var se *compose.StartError
			ctx.Expect(err).To(specs.MatchErrorAs(&se))
			ctx.Expect(se.Step).To(specs.Equal(StepAttachPublishers))
			ctx.Expect(se.Err).To(specs.MatchError(injected))
			ctx.Expect(se.Err.Error()).To(specs.Contain("ping"))
			ctx.Expect(se.Err.Error()).To(specs.Contain(`state publisher "states-1"`))
			ctx.Expect(se.Err.Error()).To(specs.Contain(`"fake-broker"`))
			ctx.Expect(f.evPub.closed.Load()).To(specs.Equal(int32(1)))
			ctx.Expect(state.closed.Load()).To(specs.Equal(int32(1)))
		})
	})
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
