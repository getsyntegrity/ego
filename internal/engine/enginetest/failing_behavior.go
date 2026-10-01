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
	"errors"

	"google.golang.org/protobuf/proto"

	"github.com/getsyntegrity/ego/egopb"
	testpb "github.com/getsyntegrity/ego/internal/testpb"
)

// ErrHandleEvent is the error FailingHandleEventBehavior.HandleEvent returns.
var ErrHandleEvent = errors.New("failing handle event behavior: HandleEvent always fails")

// FailingHandleEventBehavior is a test behavior whose HandleEvent always returns an error.
type FailingHandleEventBehavior struct {
	id string
}

// NewFailingHandleEventBehavior creates a FailingHandleEventBehavior.
func NewFailingHandleEventBehavior(id string) *FailingHandleEventBehavior {
	return &FailingHandleEventBehavior{id: id}
}

func (f *FailingHandleEventBehavior) ID() string { return f.id }

func (f *FailingHandleEventBehavior) InitialState() proto.Message {
	return new(testpb.Account)
}

func (f *FailingHandleEventBehavior) HandleCommand(_ context.Context, command proto.Message, _ proto.Message) ([]proto.Message, error) {
	switch command.(type) {
	case *testpb.CreateAccount:
		return []proto.Message{&testpb.AccountCreated{AccountId: f.id, AccountBalance: 100}}, nil
	}
	return nil, nil
}

func (f *FailingHandleEventBehavior) HandleEvent(_ context.Context, _ proto.Message, _ proto.Message) (proto.Message, error) {
	return nil, ErrHandleEvent
}

func (f *FailingHandleEventBehavior) MarshalBinary() ([]byte, error) {
	return proto.Marshal(&egopb.StateReply{PersistenceId: f.id})
}

func (f *FailingHandleEventBehavior) UnmarshalBinary(data []byte) error {
	msg := new(egopb.StateReply)
	if err := proto.Unmarshal(data, msg); err != nil {
		return err
	}
	f.id = msg.GetPersistenceId()
	return nil
}
