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
	"slices"
	"strings"
	"testing"

	"github.com/pablogore/ego/v4/compose/internal/adapters"
	"github.com/pablogore/ego/v4/port/adapter"
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
	rec := &recorder{}
	owned := []adapters.Owned{
		{Kind: "events publisher", ID: "a", Value: &both{name: "a", rec: rec}},
		{Kind: "events publisher", ID: "b", Value: plain{}},
		{Kind: "events publisher", ID: "c", Value: &starter{name: "c", rec: rec}},
		{Kind: "state publisher", ID: "d", Value: &pinger{name: "d", rec: rec}},
	}
	if err := adapters.StartAndProbe(context.Background(), owned); err != nil {
		t.Fatalf("StartAndProbe = %v, want nil", err)
	}
	want := []string{"start a", "ping a", "start c", "ping d"}
	if !slices.Equal(rec.calls, want) {
		t.Fatalf("calls = %v, want %v", rec.calls, want)
	}
}

// The first failure stops the loop: later values are neither started nor
// probed, and the error names the failing value by kind, ID and, when it
// declares one, its descriptor name. Nothing is closed: releasing is the
// caller's job.
func TestStartAndProbe_StopsAtFirstFailure(t *testing.T) {
	boom := errors.New("dial refused")
	cases := []struct {
		name     string
		second   any
		wantErr  []string
		wantCall []string
	}{
		{
			name:     "start fails, undeclared",
			second:   &starter{name: "b", err: boom},
			wantErr:  []string{"start", `events publisher "b"`},
			wantCall: []string{"start a", "ping a", "start b"},
		},
		{
			name:     "ping fails, undeclared",
			second:   &pinger{name: "b", err: boom},
			wantErr:  []string{"ping", `events publisher "b"`},
			wantCall: []string{"start a", "ping a", "ping b"},
		},
		{
			name:     "start fails, declared",
			second:   &both{name: "b", startErr: boom},
			wantErr:  []string{"start", `events publisher "b"`, `"fake-broker"`},
			wantCall: []string{"start a", "ping a", "start b"},
		},
		{
			name:     "ping fails after start, declared",
			second:   &both{name: "b", pingErr: boom},
			wantErr:  []string{"ping", `events publisher "b"`, `"fake-broker"`},
			wantCall: []string{"start a", "ping a", "start b", "ping b"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &recorder{}
			switch v := c.second.(type) {
			case *starter:
				v.rec = rec
			case *pinger:
				v.rec = rec
			case *both:
				v.rec = rec
			}
			owned := []adapters.Owned{
				{Kind: "events publisher", ID: "a", Value: &both{name: "a", rec: rec}},
				{Kind: "events publisher", ID: "b", Value: c.second},
				{Kind: "events publisher", ID: "c", Value: &both{name: "c", rec: rec}},
			}
			err := adapters.StartAndProbe(context.Background(), owned)
			if !errors.Is(err, boom) {
				t.Fatalf("StartAndProbe = %v, want it to wrap %v", err, boom)
			}
			for _, s := range c.wantErr {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("error %q does not mention %q", err, s)
				}
			}
			if strings.Contains(err.Error(), `"c"`) {
				t.Errorf("error %q names a value after the failing one", err)
			}
			if !slices.Equal(rec.calls, c.wantCall) {
				t.Fatalf("calls = %v, want %v", rec.calls, c.wantCall)
			}
		})
	}
}

// A typed-nil value implements nothing (adapter.StarterOf and PingerOf
// report it absent), so it is skipped instead of panicking.
func TestStartAndProbe_SkipsTypedNil(t *testing.T) {
	owned := []adapters.Owned{{Kind: "events publisher", ID: "nil", Value: (*both)(nil)}}
	if err := adapters.StartAndProbe(context.Background(), owned); err != nil {
		t.Fatalf("StartAndProbe = %v, want nil", err)
	}
}

func TestStartAndProbe_Empty(t *testing.T) {
	if err := adapters.StartAndProbe(context.Background(), nil); err != nil {
		t.Fatalf("StartAndProbe(nil) = %v, want nil", err)
	}
}
