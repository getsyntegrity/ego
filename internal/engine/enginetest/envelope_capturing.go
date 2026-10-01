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

package enginetest

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/tochemey/goakt/v4/extension"
	"google.golang.org/protobuf/proto"

	"github.com/getsyntegrity/ego/command"
	testpb "github.com/getsyntegrity/ego/internal/testpb"
	behaviorport "github.com/getsyntegrity/ego/port/behavior"
)

// EnvelopeCapturingEventSourcedBehavior implements both EventSourcedBehavior
// and EventSourcedEnvelopeBehavior (#60). It records, for every invocation,
// which method the runtime actually called and, when it was HandleEnvelope,
// the command.Envelope it received — so tests can assert against the real
// dispatch path instead of inferring it.
type EnvelopeCapturingEventSourcedBehavior struct {
	id string
	// delay, when non-zero, is slept at the top of HandleCommand and
	// HandleEnvelope before applying the command — used to prove Dispatch
	// bounds SendSync's wait to the Metadata deadline rather than the
	// caller-supplied timeout when the deadline is tighter.
	delay time.Duration

	mu               sync.Mutex
	handleCommandHit int
	handleEnvelope   int
	lastEnvelope     command.Envelope
}

var (
	_ extension.Dependency              = (*EnvelopeCapturingEventSourcedBehavior)(nil)
	_ behaviorport.EventSourced         = (*EnvelopeCapturingEventSourcedBehavior)(nil)
	_ behaviorport.EventSourcedEnvelope = (*EnvelopeCapturingEventSourcedBehavior)(nil)
)

// SetDelay makes every handler sleep for d before applying the command.
func (x *EnvelopeCapturingEventSourcedBehavior) SetDelay(d time.Duration) { x.delay = d }

// NewEnvelopeCapturingEventSourcedBehavior returns a behavior for the id.
func NewEnvelopeCapturingEventSourcedBehavior(id string) *EnvelopeCapturingEventSourcedBehavior {
	return &EnvelopeCapturingEventSourcedBehavior{id: id}
}

func (x *EnvelopeCapturingEventSourcedBehavior) ID() string { return x.id }

func (x *EnvelopeCapturingEventSourcedBehavior) InitialState() proto.Message {
	return new(testpb.Account)
}

func (x *EnvelopeCapturingEventSourcedBehavior) HandleCommand(_ context.Context, cmd proto.Message, _ proto.Message) (events []proto.Message, err error) {
	if x.delay > 0 {
		time.Sleep(x.delay)
	}
	x.mu.Lock()
	x.handleCommandHit++
	x.mu.Unlock()
	return x.apply(cmd)
}

func (x *EnvelopeCapturingEventSourcedBehavior) HandleEnvelope(_ context.Context, env command.Envelope, _ proto.Message) (events []proto.Message, err error) {
	if x.delay > 0 {
		time.Sleep(x.delay)
	}
	x.mu.Lock()
	x.handleEnvelope++
	x.lastEnvelope = env
	x.mu.Unlock()
	return x.apply(env.Payload())
}

func (x *EnvelopeCapturingEventSourcedBehavior) apply(cmd proto.Message) (events []proto.Message, err error) {
	switch c := cmd.(type) {
	case *testpb.CreateAccount:
		return []proto.Message{
			&testpb.AccountCreated{
				AccountId:      x.id,
				AccountBalance: c.GetAccountBalance(),
			},
		}, nil
	default:
		return nil, errors.New("unhandled command")
	}
}

func (x *EnvelopeCapturingEventSourcedBehavior) HandleEvent(_ context.Context, event proto.Message, _ proto.Message) (state proto.Message, err error) {
	switch evt := event.(type) {
	case *testpb.AccountCreated:
		return &testpb.Account{
			AccountId:      evt.GetAccountId(),
			AccountBalance: evt.GetAccountBalance(),
		}, nil
	default:
		return nil, errors.New("unhandled event")
	}
}

func (x *EnvelopeCapturingEventSourcedBehavior) MarshalBinary() (data []byte, err error) {
	return json.Marshal(struct {
		ID string `json:"id"`
	}{ID: x.id})
}

func (x *EnvelopeCapturingEventSourcedBehavior) UnmarshalBinary(data []byte) error {
	aux := struct {
		ID string `json:"id"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	x.id = aux.ID
	return nil
}

// Snapshot reports how often each handler ran and the last envelope received.
func (x *EnvelopeCapturingEventSourcedBehavior) Snapshot() (handleCommandHit, handleEnvelopeHit int, lastEnvelope command.Envelope) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.handleCommandHit, x.handleEnvelope, x.lastEnvelope
}
