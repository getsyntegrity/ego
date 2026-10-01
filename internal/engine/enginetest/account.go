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

	"github.com/tochemey/goakt/v4/extension"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	testpb "github.com/getsyntegrity/ego/internal/testpb"
	behaviorport "github.com/getsyntegrity/ego/port/behavior"
)

// AccountEventSourcedBehavior is a small bank-account event sourced behavior
// shared by the actor packages and the engine tests.
type AccountEventSourcedBehavior struct {
	id string
}

// compile-time checks that it satisfies the event sourced contract and can
// travel as a GoAkt dependency
var (
	_ behaviorport.EventSourced = (*AccountEventSourcedBehavior)(nil)
	_ extension.Dependency      = (*AccountEventSourcedBehavior)(nil)
)

// NewAccountEventSourcedBehavior returns a behavior for the account id.
func NewAccountEventSourcedBehavior(id string) *AccountEventSourcedBehavior {
	return &AccountEventSourcedBehavior{id: id}
}
func (x *AccountEventSourcedBehavior) ID() string {
	return x.id
}

func (x *AccountEventSourcedBehavior) InitialState() proto.Message {
	return new(testpb.Account)
}

func (x *AccountEventSourcedBehavior) HandleCommand(_ context.Context, command proto.Message, _ proto.Message) (events []proto.Message, err error) {
	switch cmd := command.(type) {
	case *testpb.CreateAccount:
		return []proto.Message{
			&testpb.AccountCreated{
				AccountId:      x.id,
				AccountBalance: cmd.GetAccountBalance(),
			},
		}, nil

	case *testpb.CreditAccount:
		if cmd.GetAccountId() == x.id {
			return []proto.Message{
				&testpb.AccountCredited{
					AccountId:      cmd.GetAccountId(),
					AccountBalance: cmd.GetBalance(),
				},
			}, nil
		}

		return nil, errors.New("command sent to the wrong entity")

	case *testpb.TestNoEvent:
		return nil, nil

	case *emptypb.Empty:
		return []proto.Message{new(emptypb.Empty)}, nil

	default:
		return nil, errors.New("unhandled command")
	}
}

func (x *AccountEventSourcedBehavior) HandleEvent(_ context.Context, event proto.Message, priorState proto.Message) (state proto.Message, err error) {
	switch evt := event.(type) {
	case *testpb.AccountCreated:
		return &testpb.Account{
			AccountId:      evt.GetAccountId(),
			AccountBalance: evt.GetAccountBalance(),
		}, nil

	case *testpb.AccountCredited:
		account := priorState.(*testpb.Account)
		bal := account.GetAccountBalance() + evt.GetAccountBalance()
		return &testpb.Account{
			AccountId:      evt.GetAccountId(),
			AccountBalance: bal,
		}, nil

	default:
		return nil, errors.New("unhandled event")
	}
}

func (x *AccountEventSourcedBehavior) MarshalBinary() (data []byte, err error) {
	serializable := struct {
		ID string `json:"id"`
	}{
		ID: x.id,
	}
	return json.Marshal(serializable)
}

func (x *AccountEventSourcedBehavior) UnmarshalBinary(data []byte) error {
	serializable := struct {
		ID string `json:"id"`
	}{}

	if err := json.Unmarshal(data, &serializable); err != nil {
		return err
	}

	x.id = serializable.ID
	return nil
}
