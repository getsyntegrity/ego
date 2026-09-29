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
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/compose/internal/adapters"
	"github.com/getsyntegrity/ego/port/adapter"
)

// recorder collects the calls made on the fakes, in order.
type recorder struct{ calls []string }

// plain implements neither Starter nor Pinger.
type plain struct{}

// starter implements Starter and records its calls.
type starter struct {
	name string
	rec  *recorder
	err  error
}

func (s *starter) Start(context.Context) error {
	s.rec.calls = append(s.rec.calls, "start "+s.name)
	return s.err
}

// pinger implements Pinger and records its calls.
type pinger struct {
	name string
	rec  *recorder
	err  error
}

func (p *pinger) Ping(context.Context) error {
	p.rec.calls = append(p.rec.calls, "ping "+p.name)
	return p.err
}

// both implements Starter and Pinger, and declares a descriptor.
type both struct {
	name     string
	rec      *recorder
	startErr error
	pingErr  error
}

func (b *both) Start(context.Context) error {
	b.rec.calls = append(b.rec.calls, "start "+b.name)
	return b.startErr
}

func (b *both) Ping(context.Context) error {
	b.rec.calls = append(b.rec.calls, "ping "+b.name)
	return b.pingErr
}

func (b *both) Describe() adapter.Descriptor {
	return adapter.Descriptor{Name: "fake-broker", Capabilities: []adapter.Capability{adapter.CapStart, adapter.CapReady}}
}

// Each value is started, then probed, before the next one, in order;
// values without Start or Ping are skipped for that call.
func TestStartAndProbe_StartsThenPingsEachInOrder(t *testing.T) {
	specs.Describe(t, "StartAndProbe starts and then probes each value in order, skipping absent methods", func(s *specs.Spec) {
		s.It("calls Start then Ping on each value that has them", func(ctx *specs.Context) {
			rec := &recorder{}
			owned := []adapters.Owned{
				{Kind: "events publisher", ID: "a", Value: &both{name: "a", rec: rec}},
				{Kind: "events publisher", ID: "b", Value: plain{}},
				{Kind: "events publisher", ID: "c", Value: &starter{name: "c", rec: rec}},
				{Kind: "state publisher", ID: "d", Value: &pinger{name: "d", rec: rec}},
			}
			ctx.Expect(adapters.StartAndProbe(context.Background(), owned)).To(specs.BeNil())
			ctx.Expect(rec.calls).ToEqual([]string{"start a", "ping a", "start c", "ping d"})
		})
	})
}

// The first failure stops the loop: later values are neither started nor
// probed, and the error names the failing value by kind, ID and, when it
// declares one, its descriptor name. Nothing is closed: releasing is the
// caller's job.
func TestStartAndProbe_StopsAtFirstFailure(t *testing.T) {
	boom := errors.New("dial refused")
	// second builds the failing value around the case's own recorder, so a
	// case shares no state with another.
	cases := []struct {
		name     string
		second   func(rec *recorder) any
		wantErr  []string
		wantCall []string
	}{
		{
			name:     "start fails, undeclared",
			second:   func(rec *recorder) any { return &starter{name: "b", rec: rec, err: boom} },
			wantErr:  []string{"start", `events publisher "b"`},
			wantCall: []string{"start a", "ping a", "start b"},
		},
		{
			name:     "ping fails, undeclared",
			second:   func(rec *recorder) any { return &pinger{name: "b", rec: rec, err: boom} },
			wantErr:  []string{"ping", `events publisher "b"`},
			wantCall: []string{"start a", "ping a", "ping b"},
		},
		{
			name:     "start fails, declared",
			second:   func(rec *recorder) any { return &both{name: "b", rec: rec, startErr: boom} },
			wantErr:  []string{"start", `events publisher "b"`, `"fake-broker"`},
			wantCall: []string{"start a", "ping a", "start b"},
		},
		{
			name:     "ping fails after start, declared",
			second:   func(rec *recorder) any { return &both{name: "b", rec: rec, pingErr: boom} },
			wantErr:  []string{"ping", `events publisher "b"`, `"fake-broker"`},
			wantCall: []string{"start a", "ping a", "start b", "ping b"},
		},
	}
	specs.Describe(t, "StartAndProbe stops at the first failure and names the failing value", func(s *specs.Spec) {
		for _, c := range cases {
			s.It(c.name, func(ctx *specs.Context) {
				rec := &recorder{}
				owned := []adapters.Owned{
					{Kind: "events publisher", ID: "a", Value: &both{name: "a", rec: rec}},
					{Kind: "events publisher", ID: "b", Value: c.second(rec)},
					{Kind: "events publisher", ID: "c", Value: &both{name: "c", rec: rec}},
				}
				err := adapters.StartAndProbe(context.Background(), owned)
				ctx.Expect(err).To(specs.MatchError(boom))

				// The old loop named the missing string in its message; an
				// expectation carries none, so collect the offenders instead.
				var missing []string
				for _, want := range c.wantErr {
					if !strings.Contains(err.Error(), want) {
						missing = append(missing, want)
					}
				}
				ctx.Expect(missing).To(specs.BeNil())
				ctx.Expect(err.Error()).To(specs.Not(specs.Contain(`"c"`)))
				ctx.Expect(rec.calls).ToEqual(c.wantCall)
			})
		}
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
