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

// Package adapter is the part of the adapter SPI that is common to every
// port: how an adapter says which ports it serves and which optional
// capabilities it has, and the optional lifecycle interfaces Starter and
// Pinger (ego-arch-004 design §D1–§D3).
//
// An adapter that does not implement Describer is undeclared. It keeps
// working exactly as before; declaring a Descriptor is how an adapter
// becomes inspectable, never a requirement in v4. A nil or typed-nil value
// is undeclared too, whatever its type's method set: Describe, StarterOf
// and PingerOf report it as absent and never call its methods.
//
// # One assertion per optional interface
//
// Describe, StarterOf and PingerOf are the only places in the code base
// that type-assert Describer, Starter and Pinger. Every other caller — the
// composition root, its validation, conformance suites — goes through these
// three functions and never asserts the interfaces itself, so the rule for
// what counts as "implements" lives in one place. Capabilities specific to
// one port are declared, with their own single accessor, in the contract
// package that owns the port (for example tenancy), never here.
//
// # Port and capability names
//
// Contract packages declare their port names and capabilities as untyped
// string constants (for example publishing.PortEventPublisher), which
// convert to Port and Capability where they are used. That keeps every
// contract package independent of this one, so moving a contract into
// another module cannot create a module cycle through port/adapter.
//
// This package imports only the standard library.
package adapter

import (
	"context"
	"reflect"
	"slices"
)

// Port names the contract an adapter implements, for example
// "persistence.EventsStore". The contract package that owns the port
// declares its name as an untyped constant.
type Port string

// Capability names one optional behavior of a port, for example
// "tenancy.fixed-tenant". The contract package that owns the optional
// interface declares its name as an untyped constant.
type Capability string

// CapStart is the capability of an adapter that implements Starter: it has
// work to do between construction and first use.
const CapStart Capability = "adapter.start"

// CapReady is the capability of an adapter that implements Pinger. It is
// implied by every store port, whose interface already has Ping, and
// optional for publishers.
const CapReady Capability = "adapter.ready"

// Descriptor is what an adapter says about itself. It is a value, so it
// can be logged, compared and extended with new fields without breaking
// anyone.
type Descriptor struct {
	// Ports lists every contract this value implements; one Go type often
	// serves several, for example an events store that is also a snapshot
	// store.
	Ports []Port
	// Name is the implementation name, for example "kafka" or
	// "testkit-memory". Together with Ports it identifies the adapter type;
	// instance identity (a publisher's ID) is separate.
	Name string
	// Capabilities lists the optional behaviors the adapter declares, in
	// any order. A capability implied by the port's own interface is never
	// declared.
	Capabilities []Capability
}

// Declares reports whether d lists the capability c.
func (d Descriptor) Declares(c Capability) bool {
	return slices.Contains(d.Capabilities, c)
}

// Serves reports whether d lists the port p.
func (d Descriptor) Serves(p Port) bool {
	return slices.Contains(d.Ports, p)
}

// Describer is implemented by an adapter that declares a Descriptor.
// Optional; inspect it only through Describe.
type Describer interface {
	Describe() Descriptor
}

// Starter is implemented by an owned adapter that has work to do between
// construction and first use, such as dialing a broker. Optional; inspect
// it only through StarterOf.
type Starter interface {
	Start(ctx context.Context) error
}

// Pinger is the readiness probe. Every store contract already has it;
// for a publisher it is optional. Inspect it only through PingerOf.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Describe returns v's Descriptor and true, or the zero Descriptor and
// false when v does not implement Describer. A nil value, including a
// typed-nil pointer, counts as not implementing it, and its Describe
// method is not called.
func Describe(v any) (Descriptor, bool) {
	d, ok := v.(Describer)
	if !ok || isNil(v) {
		return Descriptor{}, false
	}
	return d.Describe(), true
}

// StarterOf returns v as a Starter and true, or nil and false when v does
// not implement Starter. A nil value, including a typed-nil pointer,
// counts as not implementing it.
func StarterOf(v any) (Starter, bool) {
	s, ok := v.(Starter)
	if !ok || isNil(v) {
		return nil, false
	}
	return s, true
}

// PingerOf returns v as a Pinger and true, or nil and false when v does
// not implement Pinger. A nil value, including a typed-nil pointer, counts
// as not implementing it.
func PingerOf(v any) (Pinger, bool) {
	p, ok := v.(Pinger)
	if !ok || isNil(v) {
		return nil, false
	}
	return p, true
}

// isNil reports whether v is nil or holds a nil value of a kind that can
// be nil (a typed-nil pointer, map, slice, func, channel or unsafe
// pointer), the same kind set as compose's isTypedNil. reflect.ValueOf
// never reports an Interface kind for a value stored in an any, so that
// kind is not listed. It inspects nilness only; it never discovers methods.
func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return rv.IsNil()
	default:
		return false
	}
}
