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
	"errors"
	"testing"

	"go.uber.org/atomic"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/port/publishing"
)

// The publishers implement the contracts from port/publishing directly, with
// no dependency on package `ego` or the GoAkt runtime it pulls in. The
// historical compatibility check against the `ego` aliases (ADR
// ego-arch-001, S1 criterion 3) still exists, but it runs in a separate lane
// behind the `compat` build tag — see compat_test.go and docs/ci.md,
// "Compatibility lane" — precisely so that this file, part of the default
// unit-test closure, never needs to import `ego` (#122).
var (
	_ publishing.EventPublisher = (*EventsPublisher)(nil)
	_ publishing.StatePublisher = (*DurableStatePublisher)(nil)
)

// TestPublishBeforeStartMatchesPublishingSentinel checks that the error a
// stopped publisher returns matches publishing.ErrPublisherNotStarted. The
// publishers are built without a broker connection: Publish rejects the call
// before touching the client.
func TestPublishBeforeStartMatchesPublishingSentinel(t *testing.T) {
	ctx := context.Background()
	errs := map[string]error{
		"events": (&EventsPublisher{started: atomic.NewBool(false)}).Publish(ctx, &egopb.Event{}),
		"state":  (&DurableStatePublisher{started: atomic.NewBool(false)}).Publish(ctx, &egopb.DurableState{}),
	}

	for name, err := range errs {
		if !errors.Is(err, publishing.ErrPublisherNotStarted) {
			t.Errorf("%s: errors.Is(%v, publishing.ErrPublisherNotStarted) = false", name, err)
		}
	}
}
