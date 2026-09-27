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

package adapter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/pablogore/ego/v4/port/adapter"
)

// These tests use only the standard library: port/adapter joins the
// ego-arch-006 contracts module later (ego-arch-004 design §9, O2), and a
// test dependency there would become a requirement of that module.

// undeclared implements none of the optional interfaces.
type undeclared struct{}

// full implements Describer, Starter and Pinger with pointer receivers, so
// a typed-nil *full implements all three by method set.
type full struct {
	desc     adapter.Descriptor
	started  int
	pinged   int
	startErr error
}

func (f *full) Describe() adapter.Descriptor { return f.desc }

func (f *full) Start(context.Context) error {
	f.started++
	return f.startErr
}

func (f *full) Ping(context.Context) error {
	f.pinged++
	return nil
}

// pingOnly implements Pinger with a value receiver, like a store that has
// only the mandatory readiness probe.
type pingOnly struct{}

func (pingOnly) Ping(context.Context) error { return nil }

// Spec scenario "an undeclared value": each accessor returns its zero value
// and false, and nothing panics.
func TestAccessors_UndeclaredValueReturnsZeroAndFalse(t *testing.T) {
	for name, v := range map[string]any{
		"struct":      undeclared{},
		"pointer":     &undeclared{},
		"untyped nil": nil,
		"string":      "not an adapter",
	} {
		t.Run(name, func(t *testing.T) {
			d, ok := adapter.Describe(v)
			if ok {
				t.Errorf("Describe(%v) ok = true, want false", v)
			}
			if !isZeroDescriptor(d) {
				t.Errorf("Describe(%v) = %+v, want the zero Descriptor", v, d)
			}
			if s, ok := adapter.StarterOf(v); ok || s != nil {
				t.Errorf("StarterOf(%v) = (%v, %v), want (nil, false)", v, s, ok)
			}
			if p, ok := adapter.PingerOf(v); ok || p != nil {
				t.Errorf("PingerOf(%v) = (%v, %v), want (nil, false)", v, p, ok)
			}
		})
	}
}

func TestAccessors_ImplementingValueIsReturned(t *testing.T) {
	want := adapter.Descriptor{
		Ports:        []adapter.Port{"publishing.EventPublisher"},
		Name:         "fake",
		Capabilities: []adapter.Capability{adapter.CapStart, adapter.CapReady},
	}
	f := &full{desc: want, startErr: errors.New("dial failed")}

	d, ok := adapter.Describe(f)
	if !ok {
		t.Fatal("Describe ok = false, want true")
	}
	if d.Name != want.Name || len(d.Ports) != 1 || d.Ports[0] != want.Ports[0] || len(d.Capabilities) != 2 {
		t.Errorf("Describe = %+v, want %+v", d, want)
	}

	s, ok := adapter.StarterOf(f)
	if !ok || s == nil {
		t.Fatalf("StarterOf = (%v, %v), want the value and true", s, ok)
	}
	if err := s.Start(context.Background()); !errors.Is(err, f.startErr) {
		t.Errorf("Start error = %v, want %v", err, f.startErr)
	}
	if f.started != 1 {
		t.Errorf("Start called %d times on the adapter, want 1", f.started)
	}

	p, ok := adapter.PingerOf(f)
	if !ok || p == nil {
		t.Fatalf("PingerOf = (%v, %v), want the value and true", p, ok)
	}
	if err := p.Ping(context.Background()); err != nil {
		t.Errorf("Ping error = %v, want nil", err)
	}
	if f.pinged != 1 {
		t.Errorf("Ping called %d times on the adapter, want 1", f.pinged)
	}
}

// A value implementing one optional interface is reported only for that
// one: the accessors are independent of each other and of the descriptor.
func TestAccessors_AreIndependent(t *testing.T) {
	v := pingOnly{}
	if _, ok := adapter.PingerOf(v); !ok {
		t.Error("PingerOf(pingOnly) ok = false, want true")
	}
	if _, ok := adapter.StarterOf(v); ok {
		t.Error("StarterOf(pingOnly) ok = true, want false")
	}
	if _, ok := adapter.Describe(v); ok {
		t.Error("Describe(pingOnly) ok = true, want false")
	}
}

// A typed-nil pointer implements the interfaces by method set, but calling
// a method on it would dereference nil. The accessors treat it as absent,
// as package ego's ResolveLogger treats a typed-nil logger, so a caller
// never receives a value it cannot safely call, and Describe never calls
// the method.
func TestAccessors_TypedNilIsTreatedAsAbsent(t *testing.T) {
	var f *full
	if d, ok := adapter.Describe(f); ok || !isZeroDescriptor(d) {
		t.Errorf("Describe(typed nil) = (%+v, %v), want (zero, false)", d, ok)
	}
	if s, ok := adapter.StarterOf(f); ok || s != nil {
		t.Errorf("StarterOf(typed nil) = (%v, %v), want (nil, false)", s, ok)
	}
	if p, ok := adapter.PingerOf(f); ok || p != nil {
		t.Errorf("PingerOf(typed nil) = (%v, %v), want (nil, false)", p, ok)
	}
}

func TestDescriptor_DeclaresAndServes(t *testing.T) {
	d := adapter.Descriptor{
		Ports:        []adapter.Port{"persistence.EventsStore", "persistence.SnapshotStore"},
		Name:         "postgres",
		Capabilities: []adapter.Capability{adapter.CapReady, "tenancy.fixed-tenant"},
	}
	for _, c := range []adapter.Capability{adapter.CapReady, "tenancy.fixed-tenant"} {
		if !d.Declares(c) {
			t.Errorf("Declares(%q) = false, want true", c)
		}
	}
	for _, c := range []adapter.Capability{adapter.CapStart, "", "tenancy"} {
		if d.Declares(c) {
			t.Errorf("Declares(%q) = true, want false", c)
		}
	}
	for _, p := range []adapter.Port{"persistence.EventsStore", "persistence.SnapshotStore"} {
		if !d.Serves(p) {
			t.Errorf("Serves(%q) = false, want true", p)
		}
	}
	for _, p := range []adapter.Port{"persistence.StateStore", "", "persistence"} {
		if d.Serves(p) {
			t.Errorf("Serves(%q) = true, want false", p)
		}
	}

	var zero adapter.Descriptor
	if zero.Declares(adapter.CapReady) || zero.Serves("persistence.EventsStore") {
		t.Error("the zero Descriptor declares or serves something, want nothing")
	}
}

// The lifecycle capabilities are distinct names in the "adapter."
// namespace, so a descriptor can declare them like any other capability.
func TestLifecycleCapabilities(t *testing.T) {
	if adapter.CapStart != "adapter.start" || adapter.CapReady != "adapter.ready" {
		t.Errorf("CapStart = %q, CapReady = %q, want \"adapter.start\" and \"adapter.ready\"", adapter.CapStart, adapter.CapReady)
	}
}

func isZeroDescriptor(d adapter.Descriptor) bool {
	return d.Name == "" && d.Ports == nil && d.Capabilities == nil
}
