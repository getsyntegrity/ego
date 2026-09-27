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

//go:build compat

// This file verifies the historical S1 compatibility aliases in package
// `ego` (ADR ego-arch-001, S1 criterion 3: `EventPublisher`,
// `StatePublisher` and `ErrPublisherNotStarted`), which #121 decided to keep
// until #124 rather than deprecate now. It is gated behind the `compat`
// build tag so it never enters this module's default unit-test closure —
// `go list -deps -test ./...` with no -tags — which would otherwise pull the
// GoAkt runtime back in through package `ego` (#122). CI runs it in a
// separate lane: `go test -tags=compat ./...`, described in docs/ci.md,
// "Compatibility lane". The publishing-only equivalent of these checks lives
// in publisher_contract_test.go, in the default closure.

package pulsar

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/atomic"

	"github.com/pablogore/ego/v4"
	"github.com/pablogore/ego/v4/egopb"
)

// The publishers still satisfy the compatibility aliases in package ego
// (ADR ego-arch-001, S1 criterion 3), which existing consumers may still
// name them through.
var (
	_ ego.EventPublisher = (*EventsPublisher)(nil)
	_ ego.StatePublisher = (*DurableStatePublisher)(nil)
)

// TestPublishBeforeStartMatchesEgoSentinel checks that the error a stopped
// publisher returns still matches the historical ego.ErrPublisherNotStarted
// alias. The publishers are built without a broker connection: Publish
// rejects the call before touching the client.
func TestPublishBeforeStartMatchesEgoSentinel(t *testing.T) {
	ctx := context.Background()
	errs := map[string]error{
		"events": (&EventsPublisher{started: atomic.NewBool(false)}).Publish(ctx, &egopb.Event{}),
		"state":  (&DurableStatePublisher{started: atomic.NewBool(false)}).Publish(ctx, &egopb.DurableState{}),
	}

	for name, err := range errs {
		if !errors.Is(err, ego.ErrPublisherNotStarted) {
			t.Errorf("%s: errors.Is(%v, ego.ErrPublisherNotStarted) = false", name, err)
		}
	}
}
