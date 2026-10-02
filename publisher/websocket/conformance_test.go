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

package websocket

import (
	"context"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/urd/egopb"
	"github.com/getsyntegrity/urd/port/adapter"
	"github.com/getsyntegrity/urd/port/adapter/adaptertest"
	"github.com/getsyntegrity/urd/port/publishing"
	"github.com/getsyntegrity/urd/port/publishing/publishingtest"
)

// The websocket publishers run both conformance suites against a real
// httptest server (ego-arch-004 spec 2, SPI-4). They dial in their
// constructor, which maintainer decision O5 keeps, so AT-2 and the
// failed-acquire case of AT-3 cannot apply; they have no Ping, so AT-5 does
// not apply either. Everything else runs, and nothing is skipped.
//
// AT-4 proves little today: Close closes the TCP socket without a
// websocket close handshake, so it returns at once whether or not the
// server is stalled. It is kept as a guard: if Close ever starts a close
// handshake or flushes pending writes, a stalled server makes it block, and
// AT-4 then fails unless Close honors its context deadline (rule L3).

// wantAdapterOutcomes is the exact result the websocket publishers must
// get from adaptertest.
var wantAdapterOutcomes = map[string]adaptertest.Outcome{
	"AT-1":                              adaptertest.Passed,
	"AT-2":                              adaptertest.NotExercised,
	"AT-3/release twice":                adaptertest.Passed,
	"AT-3/release without acquire":      adaptertest.Passed,
	"AT-3/release after failed acquire": adaptertest.NotExercised,
	"AT-4":                              adaptertest.Passed,
	"AT-5":                              adaptertest.NotExercised,
}

func requireAdapterOutcomes(t *testing.T, results []adaptertest.Result) {
	specs.Describe(t, "the adaptertest results", func(s *specs.Spec) {
		s.It("match the exact outcomes the websocket publishers must get", func(ctx *specs.Context) {
			ctx.Expect(results).To(specs.HaveLen(len(wantAdapterOutcomes)))
			for _, r := range results {
				want, ok := wantAdapterOutcomes[r.Check]
				ctx.Expect(ok).To(specs.BeTrue())
				if r.Outcome != want {
					ctx.T.Logf("%s: %s (%s), want %s", r.Check, r.Outcome, r.Detail, want)
				}
				ctx.Expect(r.Outcome).To(specs.Equal(want))
			}
		})
	})
}

func TestEventsPublisherAdapterConformance(t *testing.T) {
	srv := newTestServer(t)
	results := adaptertest.Run(t, adaptertest.Target{
		Port:      publishing.PortEventPublisher,
		Ownership: adaptertest.Owned,
		New: func(*testing.T) (any, error) {
			return NewEventsPublisher(&Config{URL: srv.URL()})
		},
		Stall: srv.Stall,
	})
	requireAdapterOutcomes(t, results)
}

func TestDurableStatePublisherAdapterConformance(t *testing.T) {
	srv := newTestServer(t)
	results := adaptertest.Run(t, adaptertest.Target{
		Port:      publishing.PortStatePublisher,
		Ownership: adaptertest.Owned,
		New: func(*testing.T) (any, error) {
			return NewDurableStatePublisher(&Config{URL: srv.URL()})
		},
		Stall: srv.Stall,
	})
	requireAdapterOutcomes(t, results)
}

// PT-1 replaces the former hand-written check that Publish on a stopped
// publisher returns publishing.ErrPublisherNotStarted; it now runs against
// a publisher that really was connected and closed.
// wantPublishingOutcomes is the exact result both websocket publishers
// must get from publishingtest: every check runs and passes.
var wantPublishingOutcomes = map[string]publishingtest.Outcome{
	"PT-1": publishingtest.Passed,
	"PT-2": publishingtest.Passed,
	"PT-3": publishingtest.Passed,
}

func requirePublishingOutcomes(t *testing.T, results []publishingtest.Result) {
	specs.Describe(t, "the publishingtest results", func(s *specs.Spec) {
		s.It("match the exact outcomes the websocket publishers must get", func(ctx *specs.Context) {
			ctx.Expect(results).To(specs.HaveLen(len(wantPublishingOutcomes)))
			for _, r := range results {
				want, ok := wantPublishingOutcomes[r.Check]
				ctx.Expect(ok).To(specs.BeTrue())
				if r.Outcome != want {
					ctx.T.Logf("%s: %s (%s), want %s", r.Check, r.Outcome, r.Detail, want)
				}
				ctx.Expect(r.Outcome).To(specs.Equal(want))
			}
		})
	})
}

func TestEventsPublisherPublishingConformance(t *testing.T) {
	srv := newTestServer(t)
	results := publishingtest.RunEvents(t, publishingtest.EventsTarget{
		New: func(*testing.T) (publishing.EventPublisher, error) {
			return NewEventsPublisher(&Config{URL: srv.URL()})
		},
		Received: func(ctx context.Context, _ *testing.T, want *egopb.Event) error {
			return srv.await(ctx, want)
		},
	})
	requirePublishingOutcomes(t, results)
}

func TestDurableStatePublisherPublishingConformance(t *testing.T) {
	srv := newTestServer(t)
	results := publishingtest.RunState(t, publishingtest.StateTarget{
		New: func(*testing.T) (publishing.StatePublisher, error) {
			return NewDurableStatePublisher(&Config{URL: srv.URL()})
		},
		Received: func(ctx context.Context, _ *testing.T, want *egopb.DurableState) error {
			return srv.await(ctx, want)
		},
	})
	requirePublishingOutcomes(t, results)
}

// The descriptors are exactly what the composition root will validate
// (spec 3): one port each, the name "websocket", no declared capability
// (no Start and no Ping, per O5).
func TestDescriptors(t *testing.T) {
	specs.Describe(t, "each publisher declares one port, the name websocket and no capability", func(s *specs.Spec) {
		type descriptorCase struct {
			name  string
			value any
			port  adapter.Port
		}
		specs.Table(s, []descriptorCase{
			{"events", &EventsPublisher{}, publishing.PortEventPublisher},
			{"state", &DurableStatePublisher{}, publishing.PortStatePublisher},
		}, func(c descriptorCase) string { return c.name }, func(ctx *specs.Context, c descriptorCase) {
			d, ok := adapter.Describe(c.value)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(d.Ports).ToEqual([]adapter.Port{c.port})
			ctx.Expect(d.Name).ToEqual("websocket")
			ctx.Expect(d.Capabilities).To(specs.BeEmpty())
		})
	})
}
