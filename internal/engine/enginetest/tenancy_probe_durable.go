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

	testpb "github.com/getsyntegrity/ego/internal/testpb"
	behaviorport "github.com/getsyntegrity/ego/port/behavior"
	"github.com/getsyntegrity/ego/tenancy"
)

// TenancyProbeDurableStateBehavior is the durable state counterpart of
// TenancyProbeEventSourcedBehavior, used for the same purpose against
// the durable state actor's processCommand gate.
type TenancyProbeDurableStateBehavior struct {
	id string

	mu          sync.Mutex
	invocations int
	lastCtx     context.Context
}

var (
	_ behaviorport.DurableState = (*TenancyProbeDurableStateBehavior)(nil)
	_ extension.Dependency      = (*TenancyProbeDurableStateBehavior)(nil)
)

// NewTenancyProbeDurableStateBehavior returns a probe for the entity id.
func NewTenancyProbeDurableStateBehavior(id string) *TenancyProbeDurableStateBehavior {
	return &TenancyProbeDurableStateBehavior{id: id}
}

func (x *TenancyProbeDurableStateBehavior) ID() string {
	return x.id
}

func (x *TenancyProbeDurableStateBehavior) InitialState() proto.Message {
	return new(testpb.Account)
}

// nolint
func (x *TenancyProbeDurableStateBehavior) HandleCommand(ctx context.Context, command proto.Message, priorVersion uint64, _ proto.Message) (newState proto.Message, newVersion uint64, err error) {
	x.mu.Lock()
	x.invocations++
	x.lastCtx = ctx
	x.mu.Unlock()

	switch cmd := command.(type) {
	case *testpb.CreateAccount:
		return &testpb.Account{
			AccountId:      x.id,
			AccountBalance: cmd.GetAccountBalance(),
		}, priorVersion + 1, nil
	default:
		return nil, 0, errors.New("unhandled command")
	}
}

func (x *TenancyProbeDurableStateBehavior) MarshalBinary() (data []byte, err error) {
	return json.Marshal(struct {
		ID string `json:"id"`
	}{ID: x.id})
}

func (x *TenancyProbeDurableStateBehavior) UnmarshalBinary(data []byte) error {
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
func (x *TenancyProbeDurableStateBehavior) InvocationCount() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.invocations
}

// ObservedTenant returns the tenancy.TenantContext bound to the ctx of the
// most recent HandleCommand invocation, if any.
func (x *TenancyProbeDurableStateBehavior) ObservedTenant() (tenancy.TenantContext, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.lastCtx == nil {
		return tenancy.TenantContext{}, false
	}
	return tenancy.From(x.lastCtx)
}

// testSagaBehavior implements SagaBehavior for testing
