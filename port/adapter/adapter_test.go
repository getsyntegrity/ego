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
	"fmt"
	"testing"

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/urd/port/adapter"
)

// undeclared implements none of the optional interfaces.
type undeclared struct{}

// full is a go-specs mock adapter for Describer, Starter and Pinger: each
// method forwards to the controller, and the case declares what it expects.
// It uses pointer receivers, so a typed-nil *full implements all three by
// method set.
type full struct{ c *mock.Controller }

func (f *full) Describe() adapter.Descriptor {
	return mock.Value[adapter.Descriptor](f.c.Method("Describe").Call(), 0)
}

func (f *full) Start(ctx context.Context) error { return f.c.Method("Start").Call(ctx).Err(0) }

func (f *full) Ping(ctx context.Context) error { return f.c.Method("Ping").Call(ctx).Err(0) }

// pingOnly implements Pinger with a value receiver, like a store that has
// only the mandatory readiness probe.
type pingOnly struct{}

func (pingOnly) Ping(context.Context) error { return nil }

// Spec scenario "an undeclared value": each accessor returns its zero value
// and false, and nothing panics.
func TestAccessors_UndeclaredValueReturnsZeroAndFalse(t *testing.T) {
	type undeclaredCase struct {
		name string
		v    any
	}
	specs.Describe(t, "the accessors report an undeclared value as absent", func(s *specs.Spec) {
		specs.Table(s, []undeclaredCase{
			{"struct", undeclared{}},
			{"pointer", &undeclared{}},
			{"untyped nil", nil},
			{"string", "not an adapter"},
		}, func(c undeclaredCase) string { return c.name }, func(ctx *specs.Context, c undeclaredCase) {
			d, ok := adapter.Describe(c.v)
			ctx.Expect(ok).To(specs.BeFalse())
			ctx.Expect(d).To(specs.BeZero())

			st, ok := adapter.StarterOf(c.v)
			ctx.Expect(ok).To(specs.BeFalse())
			ctx.Expect(st).To(specs.BeNil())

			p, ok := adapter.PingerOf(c.v)
			ctx.Expect(ok).To(specs.BeFalse())
			ctx.Expect(p).To(specs.BeNil())
		})
	})
}

func TestAccessors_ImplementingValueIsReturned(t *testing.T) {
	specs.Describe(t, "the accessors return a value that implements the optional interfaces", func(s *specs.Spec) {
		s.It("returns the descriptor, the starter and the pinger of the same value", func(ctx *specs.Context) {
			want := adapter.Descriptor{
				Ports:        []adapter.Port{"publishing.EventPublisher"},
				Name:         "fake",
				Capabilities: []adapter.Capability{adapter.CapStart, adapter.CapReady},
			}
			startErr := errors.New("dial failed")
			ctrl := mock.NewController(ctx) // verifies every expectation when the case ends
			ctrl.Method("Describe").Expect().Times(1).Return(want)
			ctrl.Method("Start").Expect(mock.Any()).Times(1).Return(startErr)
			ctrl.Method("Ping").Expect(mock.Any()).Times(1).Return(nil)
			f := &full{ctrl}

			d, ok := adapter.Describe(f)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(d.Name).ToEqual(want.Name)
			ctx.Expect(d.Ports).To(specs.HaveLen(1))
			ctx.Expect(d.Ports[0]).ToEqual(want.Ports[0])
			ctx.Expect(d.Capabilities).To(specs.HaveLen(2))

			st, ok := adapter.StarterOf(f)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(st).To(specs.Not(specs.BeNil()))
			ctx.Expect(st.Start(context.Background())).To(specs.MatchError(startErr))

			p, ok := adapter.PingerOf(f)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(p).To(specs.Not(specs.BeNil()))
			ctx.Expect(p.Ping(context.Background())).To(specs.BeNil())
		})
	})
}

// A value implementing one optional interface is reported only for that
// one: the accessors are independent of each other and of the descriptor.
func TestAccessors_AreIndependent(t *testing.T) {
	specs.Describe(t, "the accessors are independent of each other", func(s *specs.Spec) {
		s.It("reports a Pinger-only value for Ping alone", func(ctx *specs.Context) {
			v := pingOnly{}
			_, ok := adapter.PingerOf(v)
			ctx.Expect(ok).To(specs.BeTrue())
			_, ok = adapter.StarterOf(v)
			ctx.Expect(ok).To(specs.BeFalse())
			_, ok = adapter.Describe(v)
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

// A typed-nil pointer implements the interfaces by method set, but calling
// a method on it would dereference nil. The accessors treat it as absent,
// as package engine's ResolveLogger treats a typed-nil logger, so a caller
// never receives a value it cannot safely call, and Describe never calls
// the method.
func TestAccessors_TypedNilIsTreatedAsAbsent(t *testing.T) {
	specs.Describe(t, "the accessors treat a typed-nil pointer as absent", func(s *specs.Spec) {
		s.It("returns the zero value and false from every accessor", func(ctx *specs.Context) {
			var f *full
			d, ok := adapter.Describe(f)
			ctx.Expect(ok).To(specs.BeFalse())
			ctx.Expect(d).To(specs.BeZero())

			st, ok := adapter.StarterOf(f)
			ctx.Expect(ok).To(specs.BeFalse())
			ctx.Expect(st).To(specs.BeNil())

			p, ok := adapter.PingerOf(f)
			ctx.Expect(ok).To(specs.BeFalse())
			ctx.Expect(p).To(specs.BeNil())
		})
	})
}

func TestDescriptor_DeclaresAndServes(t *testing.T) {
	// probe asks one question of the shared descriptor and states the answer.
	type probe struct {
		name string
		ask  func(adapter.Descriptor) bool
		want bool
	}
	declares := func(c adapter.Capability, want bool) probe {
		verb := "declares"
		if !want {
			verb = "does not declare"
		}
		return probe{fmt.Sprintf("%s %q", verb, c), func(d adapter.Descriptor) bool { return d.Declares(c) }, want}
	}
	serves := func(p adapter.Port, want bool) probe {
		verb := "serves"
		if !want {
			verb = "does not serve"
		}
		return probe{fmt.Sprintf("%s %q", verb, p), func(d adapter.Descriptor) bool { return d.Serves(p) }, want}
	}
	specs.Describe(t, "a Descriptor declares its capabilities and serves its ports", func(s *specs.Spec) {
		d := adapter.Descriptor{
			Ports:        []adapter.Port{"persistence.EventsStore", "persistence.SnapshotStore"},
			Name:         "postgres",
			Capabilities: []adapter.Capability{adapter.CapReady, "tenancy.fixed-tenant"},
		}
		specs.Table(s, []probe{
			declares(adapter.CapReady, true),
			declares("tenancy.fixed-tenant", true),
			declares(adapter.CapStart, false),
			declares("", false),
			declares("tenancy", false),
			serves("persistence.EventsStore", true),
			serves("persistence.SnapshotStore", true),
			serves("persistence.StateStore", false),
			serves("", false),
			serves("persistence", false),
		}, func(p probe) string { return p.name }, func(ctx *specs.Context, p probe) {
			ctx.Expect(p.ask(d)).To(specs.Equal(p.want))
		})

		s.It("the zero Descriptor declares and serves nothing", func(ctx *specs.Context) {
			var zero adapter.Descriptor
			ctx.Expect(zero.Declares(adapter.CapReady)).To(specs.BeFalse())
			ctx.Expect(zero.Serves("persistence.EventsStore")).To(specs.BeFalse())
		})
	})
}

// The lifecycle capabilities are distinct names in the "adapter."
// namespace, so a descriptor can declare them like any other capability.
func TestLifecycleCapabilities(t *testing.T) {
	specs.Describe(t, "the lifecycle capabilities are names in the adapter namespace", func(s *specs.Spec) {
		s.It("CapStart and CapReady have their contract names", func(ctx *specs.Context) {
			ctx.Expect(adapter.CapStart).ToEqual(adapter.Capability("adapter.start"))
			ctx.Expect(adapter.CapReady).ToEqual(adapter.Capability("adapter.ready"))
		})
	})
}
