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

	behaviorport "github.com/getsyntegrity/ego/port/behavior"
	"github.com/getsyntegrity/ego/tenancy"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
)

// TenancyProbeEventSourcedBehavior is a minimal event sourced behavior that
// records, from inside a real HandleCommand invocation, how many times it
// was called and the ctx it was called with. Tests use it to prove (rather
// than infer) that a TenantContext resolved and attached at the trust
// boundary reaches domain code unchanged via tenancy.From, and that the
// pre-handler gate prevents HandleCommand from ever running when no
// TenantContext is attached.
type TenancyProbeEventSourcedBehavior struct {
	id string

	mu          sync.Mutex
	invocations int
	lastCtx     context.Context
}

var (
	_ behaviorport.EventSourced = (*TenancyProbeEventSourcedBehavior)(nil)
	_ extension.Dependency      = (*TenancyProbeEventSourcedBehavior)(nil)
)

// NewTenancyProbeEventSourcedBehavior returns a probe for the entity id.
func NewTenancyProbeEventSourcedBehavior(id string) *TenancyProbeEventSourcedBehavior {
	return &TenancyProbeEventSourcedBehavior{id: id}
}

func (x *TenancyProbeEventSourcedBehavior) ID() string {
	return x.id
}

func (x *TenancyProbeEventSourcedBehavior) InitialState() proto.Message {
	return new(testpb.Account)
}

func (x *TenancyProbeEventSourcedBehavior) HandleCommand(ctx context.Context, command proto.Message, _ proto.Message) (events []proto.Message, err error) {
	x.mu.Lock()
	x.invocations++
	x.lastCtx = ctx
	x.mu.Unlock()

	switch cmd := command.(type) {
	case *testpb.CreateAccount:
		return []proto.Message{
			&testpb.AccountCreated{
				AccountId:      x.id,
				AccountBalance: cmd.GetAccountBalance(),
			},
		}, nil
	case *testpb.TestNoEvent:
		// A genuinely idempotent no-op: no error, zero events. Used by the
		// Blocker 3 cross-tenant batch-leak regression test, which needs a
		// command that would otherwise reach processAndBatch's
		// len(events)==0 reply path without ever erroring out first.
		return nil, nil
	default:
		return nil, errors.New("unhandled command")
	}
}

func (x *TenancyProbeEventSourcedBehavior) HandleEvent(_ context.Context, event proto.Message, _ proto.Message) (state proto.Message, err error) {
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

func (x *TenancyProbeEventSourcedBehavior) MarshalBinary() (data []byte, err error) {
	return json.Marshal(struct {
		ID string `json:"id"`
	}{ID: x.id})
}

func (x *TenancyProbeEventSourcedBehavior) UnmarshalBinary(data []byte) error {
	aux := struct {
		ID string `json:"id"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	x.id = aux.ID
	return nil
}

// InvocationCount reports how many times HandleCommand has run so far.
func (x *TenancyProbeEventSourcedBehavior) InvocationCount() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.invocations
}

// ObservedTenant returns the tenancy.TenantContext bound to the ctx of the
// most recent HandleCommand invocation, if any.
func (x *TenancyProbeEventSourcedBehavior) ObservedTenant() (tenancy.TenantContext, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.lastCtx == nil {
		return tenancy.TenantContext{}, false
	}
	return tenancy.From(x.lastCtx)
}
