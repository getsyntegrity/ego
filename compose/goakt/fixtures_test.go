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

package goakt

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/anypb"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/eventstream"
	"github.com/pablogore/ego/v4/persistence"
	behaviorport "github.com/pablogore/ego/v4/port/behavior"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
)

// waitTimeout bounds every wait on an asynchronous delivery. Tests wait on
// channels, never on sleeps; this is only the failure deadline.
const waitTimeout = 10 * time.Second

// account is a runtime-neutral event-sourced behavior (port/behavior only):
// CreateAccount yields AccountCreated, which sets the balance.
type account struct{ id string }

var _ behaviorport.EventSourced = (*account)(nil)

func (a *account) ID() string                       { return a.id }
func (a *account) InitialState() behaviorport.State { return new(testpb.Account) }

func (a *account) HandleCommand(_ context.Context, cmd behaviorport.Command, _ behaviorport.State) ([]behaviorport.Event, error) {
	create, ok := cmd.(*testpb.CreateAccount)
	if !ok {
		return nil, errors.New("unhandled command")
	}
	return []behaviorport.Event{&testpb.AccountCreated{AccountId: a.id, AccountBalance: create.GetAccountBalance()}}, nil
}

func (a *account) HandleEvent(_ context.Context, evt behaviorport.Event, _ behaviorport.State) (behaviorport.State, error) {
	created, ok := evt.(*testpb.AccountCreated)
	if !ok {
		return nil, errors.New("unhandled event")
	}
	return &testpb.Account{AccountId: created.GetAccountId(), AccountBalance: created.GetAccountBalance()}, nil
}

// wallet is a durable-state behavior. It also has MarshalBinary and
// UnmarshalBinary, so it satisfies the deprecated ego.DurableStateBehavior
// and can be spawned through both entry points.
type wallet struct{ id string }

var _ behaviorport.DurableState = (*wallet)(nil)

func (w *wallet) ID() string                       { return w.id }
func (w *wallet) InitialState() behaviorport.State { return new(testpb.Account) }

func (w *wallet) HandleCommand(_ context.Context, cmd behaviorport.Command, priorVersion uint64, _ behaviorport.State) (behaviorport.State, uint64, error) {
	create, ok := cmd.(*testpb.CreateAccount)
	if !ok {
		return nil, 0, errors.New("unhandled command")
	}
	return &testpb.Account{AccountId: w.id, AccountBalance: create.GetAccountBalance()}, priorVersion + 1, nil
}

func (w *wallet) MarshalBinary() ([]byte, error) { return json.Marshal(w.id) }
func (w *wallet) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, &w.id)
}

// eventPublisher records what it receives and how often it is closed. An
// optional onClose runs inside Close, so a test can observe the state of
// the rest of the system at the moment the publisher closes.
type eventPublisher struct {
	id      string
	events  chan *egopb.Event
	closed  atomic.Int32
	late    atomic.Int32 // Publish calls after Close
	onClose func()
}

func newEventPublisher(id string) *eventPublisher {
	return &eventPublisher{id: id, events: make(chan *egopb.Event, 64)}
}

func (p *eventPublisher) ID() string { return p.id }

func (p *eventPublisher) Publish(_ context.Context, event *egopb.Event) error {
	if p.closed.Load() > 0 {
		p.late.Add(1)
	}
	p.events <- event
	return nil
}

func (p *eventPublisher) Close(context.Context) error {
	if p.onClose != nil {
		p.onClose()
	}
	p.closed.Add(1)
	return nil
}

type statePublisher struct {
	id      string
	states  chan *egopb.DurableState
	closed  atomic.Int32
	late    atomic.Int32
	onClose func()
}

func newStatePublisher(id string) *statePublisher {
	return &statePublisher{id: id, states: make(chan *egopb.DurableState, 64)}
}

func (p *statePublisher) ID() string { return p.id }

func (p *statePublisher) Publish(_ context.Context, state *egopb.DurableState) error {
	if p.closed.Load() > 0 {
		p.late.Add(1)
	}
	p.states <- state
	return nil
}

func (p *statePublisher) Close(context.Context) error {
	if p.onClose != nil {
		p.onClose()
	}
	p.closed.Add(1)
	return nil
}

// countingStream is a real event stream that counts Close calls and runs an
// optional onClose first.
type countingStream struct {
	eventstream.Stream
	closed  atomic.Int32
	onClose func()
}

func (s *countingStream) Close() {
	if s.onClose != nil {
		s.onClose()
	}
	s.closed.Add(1)
	s.Stream.Close()
}

// pingCountingEventsStore counts Ping calls and fails them on demand.
type pingCountingEventsStore struct {
	*testkit.EventStore
	pings   atomic.Int32
	pingErr error
}

func (s *pingCountingEventsStore) Ping(ctx context.Context) error {
	s.pings.Add(1)
	if s.pingErr != nil {
		return s.pingErr
	}
	return s.EventStore.Ping(ctx)
}

// writeCountingStateStore counts WriteState calls.
type writeCountingStateStore struct {
	*testkit.DurableStore
	writes atomic.Int32
}

func (s *writeCountingStateStore) WriteState(ctx context.Context, scope persistence.Scope, state *egopb.DurableState, pre persistence.WritePrecondition) error {
	s.writes.Add(1)
	return s.DurableStore.WriteState(ctx, scope, state, pre)
}

// recordingHandler is a projection handler that forwards each event it
// handles.
type recordingHandler struct {
	mu   sync.Mutex
	seen []string
}

func (h *recordingHandler) Handle(_ context.Context, persistenceID string, _ *anypb.Any, _ uint64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seen = append(h.seen, persistenceID)
	return nil
}

// connected returns connected testkit stores, disconnected at cleanup: the
// consumer owns stores (design §D5), so the tests do too.
func connected(t *testing.T) (*pingCountingEventsStore, *writeCountingStateStore, *testkit.OffsetStore) {
	t.Helper()
	ctx := context.Background()
	events := &pingCountingEventsStore{EventStore: testkit.NewEventsStore()}
	states := &writeCountingStateStore{DurableStore: testkit.NewDurableStore()}
	offsets := testkit.NewOffsetStore()
	for _, connect := range []func(context.Context) error{events.Connect, states.Connect, offsets.Connect} {
		if err := connect(ctx); err != nil {
			t.Fatalf("connect store: %v", err)
		}
	}
	t.Cleanup(func() {
		_ = events.Disconnect(ctx)
		_ = states.Disconnect(ctx)
		_ = offsets.Disconnect(ctx)
	})
	return events, states, offsets
}
