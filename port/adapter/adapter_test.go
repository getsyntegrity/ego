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

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/port/adapter"
)

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
	specs.Describe(t, "the accessors report an undeclared value as absent", func(s *specs.Spec) {
		for _, tc := range []struct {
			name string
			v    any
		}{
			{"struct", undeclared{}},
			{"pointer", &undeclared{}},
			{"untyped nil", nil},
			{"string", "not an adapter"},
		} {
			s.It(tc.name, func(ctx *specs.Context) {
				d, ok := adapter.Describe(tc.v)
				ctx.Expect(ok).To(specs.BeFalse())
				ctx.Expect(isZeroDescriptor(d)).To(specs.BeTrue())

				st, ok := adapter.StarterOf(tc.v)
				ctx.Expect(ok).To(specs.BeFalse())
				ctx.Expect(st == nil).To(specs.BeTrue())

				p, ok := adapter.PingerOf(tc.v)
				ctx.Expect(ok).To(specs.BeFalse())
				ctx.Expect(p == nil).To(specs.BeTrue())
			})
		}
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
			f := &full{desc: want, startErr: errors.New("dial failed")}

			d, ok := adapter.Describe(f)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(d.Name).ToEqual(want.Name)
			ctx.Expect(len(d.Ports)).ToEqual(1)
			ctx.Expect(d.Ports[0]).ToEqual(want.Ports[0])
			ctx.Expect(len(d.Capabilities)).ToEqual(2)

			st, ok := adapter.StarterOf(f)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(st == nil).To(specs.BeFalse())
			ctx.Expect(st.Start(context.Background())).To(specs.MatchError(f.startErr))
			ctx.Expect(f.started).ToEqual(1)

			p, ok := adapter.PingerOf(f)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(p == nil).To(specs.BeFalse())
			ctx.Expect(p.Ping(context.Background())).To(specs.BeNil())
			ctx.Expect(f.pinged).ToEqual(1)
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
// as package ego's ResolveLogger treats a typed-nil logger, so a caller
// never receives a value it cannot safely call, and Describe never calls
// the method.
func TestAccessors_TypedNilIsTreatedAsAbsent(t *testing.T) {
	specs.Describe(t, "the accessors treat a typed-nil pointer as absent", func(s *specs.Spec) {
		s.It("returns the zero value and false from every accessor", func(ctx *specs.Context) {
			var f *full
			d, ok := adapter.Describe(f)
			ctx.Expect(ok).To(specs.BeFalse())
			ctx.Expect(isZeroDescriptor(d)).To(specs.BeTrue())

			st, ok := adapter.StarterOf(f)
			ctx.Expect(ok).To(specs.BeFalse())
			ctx.Expect(st == nil).To(specs.BeTrue())

			p, ok := adapter.PingerOf(f)
			ctx.Expect(ok).To(specs.BeFalse())
			ctx.Expect(p == nil).To(specs.BeTrue())
		})
	})
}

func TestDescriptor_DeclaresAndServes(t *testing.T) {
	specs.Describe(t, "a Descriptor declares its capabilities and serves its ports", func(s *specs.Spec) {
		d := adapter.Descriptor{
			Ports:        []adapter.Port{"persistence.EventsStore", "persistence.SnapshotStore"},
			Name:         "postgres",
			Capabilities: []adapter.Capability{adapter.CapReady, "tenancy.fixed-tenant"},
		}
		for _, c := range []adapter.Capability{adapter.CapReady, "tenancy.fixed-tenant"} {
			s.It(fmt.Sprintf("declares %q", c), func(ctx *specs.Context) {
				ctx.Expect(d.Declares(c)).To(specs.BeTrue())
			})
		}
		for _, c := range []adapter.Capability{adapter.CapStart, "", "tenancy"} {
			s.It(fmt.Sprintf("does not declare %q", c), func(ctx *specs.Context) {
				ctx.Expect(d.Declares(c)).To(specs.BeFalse())
			})
		}
		for _, p := range []adapter.Port{"persistence.EventsStore", "persistence.SnapshotStore"} {
			s.It(fmt.Sprintf("serves %q", p), func(ctx *specs.Context) {
				ctx.Expect(d.Serves(p)).To(specs.BeTrue())
			})
		}
		for _, p := range []adapter.Port{"persistence.StateStore", "", "persistence"} {
			s.It(fmt.Sprintf("does not serve %q", p), func(ctx *specs.Context) {
				ctx.Expect(d.Serves(p)).To(specs.BeFalse())
			})
		}

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

func isZeroDescriptor(d adapter.Descriptor) bool {
	return d.Name == "" && d.Ports == nil && d.Capabilities == nil
}
