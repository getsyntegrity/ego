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

	"github.com/tochemey/goakt/v4/extension"
	"google.golang.org/protobuf/proto"

	"github.com/getsyntegrity/ego/command"
	testpb "github.com/getsyntegrity/ego/internal/testpb"
	behaviorport "github.com/getsyntegrity/ego/port/behavior"
)

// EnvelopeCapturingDurableStateBehavior is the DurableStateBehavior
// counterpart of envelopeCapturingEventSourcedBehavior, proving the same
// M-3 wiring for the durable state actor's dispatchToBehavior.
type EnvelopeCapturingDurableStateBehavior struct {
	id string

	mu               sync.Mutex
	handleCommandHit int
	handleEnvelope   int
	lastEnvelope     command.Envelope
}

var (
	_ extension.Dependency              = (*EnvelopeCapturingDurableStateBehavior)(nil)
	_ behaviorport.DurableState         = (*EnvelopeCapturingDurableStateBehavior)(nil)
	_ behaviorport.DurableStateEnvelope = (*EnvelopeCapturingDurableStateBehavior)(nil)
)

// NewEnvelopeCapturingDurableStateBehavior returns a behavior for the id.
func NewEnvelopeCapturingDurableStateBehavior(id string) *EnvelopeCapturingDurableStateBehavior {
	return &EnvelopeCapturingDurableStateBehavior{id: id}
}

func (x *EnvelopeCapturingDurableStateBehavior) ID() string { return x.id }

func (x *EnvelopeCapturingDurableStateBehavior) InitialState() proto.Message {
	return new(testpb.Account)
}

// nolint
func (x *EnvelopeCapturingDurableStateBehavior) HandleCommand(_ context.Context, cmd proto.Message, priorVersion uint64, _ proto.Message) (newState proto.Message, newVersion uint64, err error) {
	x.mu.Lock()
	x.handleCommandHit++
	x.mu.Unlock()
	return x.apply(cmd, priorVersion)
}

func (x *EnvelopeCapturingDurableStateBehavior) HandleEnvelope(_ context.Context, env command.Envelope, priorVersion uint64, _ proto.Message) (newState proto.Message, newVersion uint64, err error) {
	x.mu.Lock()
	x.handleEnvelope++
	x.lastEnvelope = env
	x.mu.Unlock()
	return x.apply(env.Payload(), priorVersion)
}

func (x *EnvelopeCapturingDurableStateBehavior) apply(cmd proto.Message, priorVersion uint64) (proto.Message, uint64, error) {
	switch c := cmd.(type) {
	case *testpb.CreateAccount:
		return &testpb.Account{
			AccountId:      x.id,
			AccountBalance: c.GetAccountBalance(),
		}, priorVersion + 1, nil
	default:
		return nil, 0, errors.New("unhandled command")
	}
}

func (x *EnvelopeCapturingDurableStateBehavior) MarshalBinary() (data []byte, err error) {
	return json.Marshal(struct {
		ID string `json:"id"`
	}{ID: x.id})
}

func (x *EnvelopeCapturingDurableStateBehavior) UnmarshalBinary(data []byte) error {
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
func (x *EnvelopeCapturingDurableStateBehavior) Snapshot() (handleCommandHit, handleEnvelopeHit int, lastEnvelope command.Envelope) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.handleCommandHit, x.handleEnvelope, x.lastEnvelope
}
