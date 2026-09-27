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

package compat_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"unsafe"

	"github.com/pablogore/ego/v4"
	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/publisher/kafka"
	"github.com/pablogore/ego/v4/publisher/nats"
	"github.com/pablogore/ego/v4/publisher/pulsar"
	"github.com/pablogore/ego/v4/publisher/websocket"
)

// The publishers still satisfy the compatibility aliases in package ego (ADR
// ego-arch-001, S1 criterion 3), which existing consumers may still name them
// through. These are the eight compile-time assertions each publisher's
// compat_test.go used to hold, two per publisher.
var (
	_ ego.EventPublisher = (*kafka.EventsPublisher)(nil)
	_ ego.StatePublisher = (*kafka.DurableStatePublisher)(nil)

	_ ego.EventPublisher = (*nats.EventsPublisher)(nil)
	_ ego.StatePublisher = (*nats.DurableStatePublisher)(nil)

	_ ego.EventPublisher = (*pulsar.EventsPublisher)(nil)
	_ ego.StatePublisher = (*pulsar.DurableStatePublisher)(nil)

	_ ego.EventPublisher = (*websocket.EventsPublisher)(nil)
	_ ego.StatePublisher = (*websocket.DurableStatePublisher)(nil)
)

// TestPublishBeforeStartMatchesEgoSentinel checks, for every publisher, that
// the error a stopped publisher returns still matches the historical
// ego.ErrPublisherNotStarted alias. It is the runtime half of each
// publisher's former compat_test.go, one subtest per publisher and kind.
//
// Each publisher is built without a broker connection, in the same state the
// former in-package test built with a struct literal: the zero value with
// its unexported `started` flag set to a new, false go.uber.org/atomic.Bool.
// Publish rejects the call before touching the client. See stoppedPublisher
// for why the flag is set through reflection.
func TestPublishBeforeStartMatchesEgoSentinel(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		publish func(t *testing.T) error
	}{
		{"kafka/events", func(t *testing.T) error {
			return stoppedPublisher[kafka.EventsPublisher](t).Publish(ctx, &egopb.Event{})
		}},
		{"kafka/state", func(t *testing.T) error {
			return stoppedPublisher[kafka.DurableStatePublisher](t).Publish(ctx, &egopb.DurableState{})
		}},
		{"nats/events", func(t *testing.T) error {
			return stoppedPublisher[nats.EventsPublisher](t).Publish(ctx, &egopb.Event{})
		}},
		{"nats/state", func(t *testing.T) error {
			return stoppedPublisher[nats.DurableStatePublisher](t).Publish(ctx, &egopb.DurableState{})
		}},
		{"pulsar/events", func(t *testing.T) error {
			return stoppedPublisher[pulsar.EventsPublisher](t).Publish(ctx, &egopb.Event{})
		}},
		{"pulsar/state", func(t *testing.T) error {
			return stoppedPublisher[pulsar.DurableStatePublisher](t).Publish(ctx, &egopb.DurableState{})
		}},
		{"websocket/events", func(t *testing.T) error {
			return stoppedPublisher[websocket.EventsPublisher](t).Publish(ctx, &egopb.Event{})
		}},
		{"websocket/state", func(t *testing.T) error {
			return stoppedPublisher[websocket.DurableStatePublisher](t).Publish(ctx, &egopb.DurableState{})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.publish(t)
			if !errors.Is(err, ego.ErrPublisherNotStarted) {
				t.Errorf("errors.Is(%v, ego.ErrPublisherNotStarted) = false", err)
			}
		})
	}
}

// stoppedPublisher returns a zero-value *T whose unexported `started` field
// holds a new go.uber.org/atomic.Bool with value false: exactly the value the
// former in-package tests built as &T{started: atomic.NewBool(false)}.
//
// This module is outside the publishers' packages, so it cannot name the
// field in a struct literal, and every publisher constructor dials its
// broker (Pulsar has no embeddable server). Setting the field through
// reflect.NewAt is the only way to keep the runtime assertion one-to-one
// without adding a test hook to the publishers' production API. The helper
// fails the test, rather than silently checking something else, if the
// field is renamed or changes type.
func stoppedPublisher[T any](t *testing.T) *T {
	t.Helper()
	p := new(T)
	field := reflect.ValueOf(p).Elem().FieldByName("started")
	if !field.IsValid() {
		t.Fatalf("%T has no field named started; update stoppedPublisher to the publisher's new stop flag", p)
	}
	ft := field.Type()
	if ft.Kind() != reflect.Pointer || ft.Elem().PkgPath() != "go.uber.org/atomic" || ft.Elem().Name() != "Bool" {
		t.Fatalf("%T.started has type %s, want *go.uber.org/atomic.Bool; update stoppedPublisher", p, ft)
	}
	// A zero go.uber.org/atomic.Bool reads false, the same as NewBool(false).
	stopped := reflect.New(ft.Elem())
	// #nosec G103 -- test-only write to one unexported field of a value this
	// function just allocated; the field's type is checked above.
	reflect.NewAt(ft, unsafe.Pointer(field.UnsafeAddr())).Elem().Set(stopped)
	return p
}
