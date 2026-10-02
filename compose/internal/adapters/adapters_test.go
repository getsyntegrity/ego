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

package adapters_test

import (
	"context"
	"errors"
	"testing"

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/urd/compose/internal/adapters"
	"github.com/getsyntegrity/urd/port/adapter"
)

// The adapters below are go-specs mocks: each method forwards to the case's
// controller, passing the adapter's own name so a case can expect "Start of a"
// apart from "Start of b" and require their order.

// plain implements neither Starter nor Pinger.
type plain struct{}

// starter implements Starter only.
type starter struct {
	c    *mock.Controller
	name string
}

func (s *starter) Start(ctx context.Context) error {
	return s.c.Method("Start").Call(ctx, s.name).Err(0)
}

// pinger implements Pinger only.
type pinger struct {
	c    *mock.Controller
	name string
}

func (p *pinger) Ping(ctx context.Context) error {
	return p.c.Method("Ping").Call(ctx, p.name).Err(0)
}

// both implements Starter and Pinger, and declares a descriptor. The
// descriptor is a fixed fixture, not a call the cases verify.
type both struct {
	c    *mock.Controller
	name string
}

func (b *both) Start(ctx context.Context) error {
	return b.c.Method("Start").Call(ctx, b.name).Err(0)
}

func (b *both) Ping(ctx context.Context) error {
	return b.c.Method("Ping").Call(ctx, b.name).Err(0)
}

func (b *both) Describe() adapter.Descriptor {
	return adapter.Descriptor{Name: "fake-broker", Capabilities: []adapter.Capability{adapter.CapStart, adapter.CapReady}}
}

// call names one expected call: the method and the adapter it reaches.
type call struct{ method, name string }

// expectInOrder declares each call once and requires them in this order. The
// last one answers with failure when it is not nil. A call the case did not
// declare is reported by the controller as unexpected, which is how the cases
// prove that nothing runs after a failure.
func expectInOrder(ctrl *mock.Controller, failure error, calls ...call) {
	exps := make([]*mock.Expectation, len(calls))
	for i, c := range calls {
		exps[i] = ctrl.Method(c.method).Expect(mock.Any(), c.name).Times(1)
	}
	if failure != nil {
		exps[len(exps)-1].Return(failure)
	}
	ctrl.InOrder(exps...)
}

// Each value is started, then probed, before the next one, in order;
// values without Start or Ping are skipped for that call.
func TestStartAndProbe_StartsThenPingsEachInOrder(t *testing.T) {
	specs.Describe(t, "StartAndProbe starts and then probes each value in order, skipping absent methods", func(s *specs.Spec) {
		s.It("calls Start then Ping on each value that has them", func(ctx *specs.Context) {
			ctrl := mock.NewController(ctx)
			expectInOrder(ctrl, nil,
				call{"Start", "a"}, call{"Ping", "a"}, call{"Start", "c"}, call{"Ping", "d"})
			owned := []adapters.Owned{
				{Kind: "events publisher", ID: "a", Value: &both{c: ctrl, name: "a"}},
				{Kind: "events publisher", ID: "b", Value: plain{}},
				{Kind: "events publisher", ID: "c", Value: &starter{c: ctrl, name: "c"}},
				{Kind: "state publisher", ID: "d", Value: &pinger{c: ctrl, name: "d"}},
			}
			ctx.Expect(adapters.StartAndProbe(context.Background(), owned)).To(specs.BeNil())
		})
	})
}

// The first failure stops the loop: later values are neither started nor
// probed, and the error names the failing value by kind, ID and, when it
// declares one, its descriptor name. Nothing is closed: releasing is the
// caller's job.
func TestStartAndProbe_StopsAtFirstFailure(t *testing.T) {
	boom := errors.New("dial refused")
	type failureCase struct {
		name string
		// second builds the failing value around the case's own controller,
		// so a case shares no state with another.
		second  func(c *mock.Controller) any
		wantErr []any
		// calls are the only calls that may happen; the last one fails.
		calls []call
	}
	specs.Describe(t, "StartAndProbe stops at the first failure and names the failing value", func(s *specs.Spec) {
		specs.Table(s, []failureCase{
			{
				name:    "start fails, undeclared",
				second:  func(c *mock.Controller) any { return &starter{c: c, name: "b"} },
				wantErr: []any{"start", `events publisher "b"`},
				calls:   []call{{"Start", "a"}, {"Ping", "a"}, {"Start", "b"}},
			},
			{
				name:    "ping fails, undeclared",
				second:  func(c *mock.Controller) any { return &pinger{c: c, name: "b"} },
				wantErr: []any{"ping", `events publisher "b"`},
				calls:   []call{{"Start", "a"}, {"Ping", "a"}, {"Ping", "b"}},
			},
			{
				name:    "start fails, declared",
				second:  func(c *mock.Controller) any { return &both{c: c, name: "b"} },
				wantErr: []any{"start", `events publisher "b"`, `"fake-broker"`},
				calls:   []call{{"Start", "a"}, {"Ping", "a"}, {"Start", "b"}},
			},
			{
				name:    "ping fails after start, declared",
				second:  func(c *mock.Controller) any { return &both{c: c, name: "b"} },
				wantErr: []any{"ping", `events publisher "b"`, `"fake-broker"`},
				calls:   []call{{"Start", "a"}, {"Ping", "a"}, {"Start", "b"}, {"Ping", "b"}},
			},
		}, func(c failureCase) string { return c.name }, func(ctx *specs.Context, c failureCase) {
			ctrl := mock.NewController(ctx)
			expectInOrder(ctrl, boom, c.calls...)
			owned := []adapters.Owned{
				{Kind: "events publisher", ID: "a", Value: &both{c: ctrl, name: "a"}},
				{Kind: "events publisher", ID: "b", Value: c.second(ctrl)},
				{Kind: "events publisher", ID: "c", Value: &both{c: ctrl, name: "c"}},
			}

			err := adapters.StartAndProbe(context.Background(), owned)

			ctx.Expect(err).To(specs.MatchError(boom))
			ctx.Expect(err.Error()).To(specs.ContainAllOf(c.wantErr...))
			ctx.Expect(err.Error()).To(specs.Not(specs.Contain(`"c"`)))
		})
	})
}

// A typed-nil value implements nothing (adapter.StarterOf and PingerOf
// report it absent), so it is skipped instead of panicking.
func TestStartAndProbe_SkipsTypedNil(t *testing.T) {
	specs.Describe(t, "StartAndProbe skips a typed-nil value instead of panicking", func(s *specs.Spec) {
		s.It("reports no error", func(ctx *specs.Context) {
			owned := []adapters.Owned{{Kind: "events publisher", ID: "nil", Value: (*both)(nil)}}
			ctx.Expect(adapters.StartAndProbe(context.Background(), owned)).To(specs.BeNil())
		})
	})
}

func TestStartAndProbe_Empty(t *testing.T) {
	specs.Describe(t, "StartAndProbe accepts an empty list", func(s *specs.Spec) {
		s.It("reports no error for nil", func(ctx *specs.Context) {
			ctx.Expect(adapters.StartAndProbe(context.Background(), nil)).To(specs.BeNil())
		})
	})
}
