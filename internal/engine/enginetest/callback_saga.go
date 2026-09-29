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

	"github.com/tochemey/goakt/v4/extension"
	"google.golang.org/protobuf/proto"

	samplepb "github.com/getsyntegrity/ego/example/examplepb"
	behaviorport "github.com/getsyntegrity/ego/port/behavior"
)

// CallbackSagaBehavior is a configurable saga behavior for testing
// that delegates each method to a user-supplied function field.
type CallbackSagaBehavior struct {
	SagaID         string
	InitialStateFn func() proto.Message
	HandleEventFn  func(ctx context.Context, event proto.Message, state proto.Message) (*behaviorport.SagaAction, error)
	HandleResultFn func(ctx context.Context, entityID string, result proto.Message, sagaState proto.Message) (*behaviorport.SagaAction, error)
	HandleErrorFn  func(ctx context.Context, entityID string, err error, sagaState proto.Message) (*behaviorport.SagaAction, error)
	ApplyEventFn   func(ctx context.Context, event proto.Message, state proto.Message) (proto.Message, error)
	CompensateFn   func(ctx context.Context, state proto.Message) ([]behaviorport.SagaCommand, error)
}

var (
	_ behaviorport.Saga    = (*CallbackSagaBehavior)(nil)
	_ extension.Dependency = (*CallbackSagaBehavior)(nil)
)

func (c *CallbackSagaBehavior) ID() string { return c.SagaID }

func (c *CallbackSagaBehavior) InitialState() proto.Message {
	if c.InitialStateFn != nil {
		return c.InitialStateFn()
	}
	return new(samplepb.Account)
}

func (c *CallbackSagaBehavior) HandleEvent(ctx context.Context, event proto.Message, state proto.Message) (*behaviorport.SagaAction, error) {
	if c.HandleEventFn != nil {
		return c.HandleEventFn(ctx, event, state)
	}
	return &behaviorport.SagaAction{}, nil
}

func (c *CallbackSagaBehavior) HandleResult(ctx context.Context, entityID string, result proto.Message, sagaState proto.Message) (*behaviorport.SagaAction, error) {
	if c.HandleResultFn != nil {
		return c.HandleResultFn(ctx, entityID, result, sagaState)
	}
	return &behaviorport.SagaAction{Complete: true}, nil
}

func (c *CallbackSagaBehavior) HandleError(ctx context.Context, entityID string, err error, sagaState proto.Message) (*behaviorport.SagaAction, error) {
	if c.HandleErrorFn != nil {
		return c.HandleErrorFn(ctx, entityID, err, sagaState)
	}
	return &behaviorport.SagaAction{Compensate: true}, nil
}

func (c *CallbackSagaBehavior) ApplyEvent(ctx context.Context, event proto.Message, state proto.Message) (proto.Message, error) {
	if c.ApplyEventFn != nil {
		return c.ApplyEventFn(ctx, event, state)
	}
	return state, nil
}

func (c *CallbackSagaBehavior) Compensate(ctx context.Context, state proto.Message) ([]behaviorport.SagaCommand, error) {
	if c.CompensateFn != nil {
		return c.CompensateFn(ctx, state)
	}
	return nil, nil
}

func (c *CallbackSagaBehavior) MarshalBinary() ([]byte, error) {
	return json.Marshal(c.SagaID)
}

func (c *CallbackSagaBehavior) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, &c.SagaID)
}
