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

	behaviorport "github.com/getsyntegrity/ego/port/behavior"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
)

// AccountDurableStateBehavior is a small bank-account durable state behavior
// shared by the actor packages and the engine tests.
type AccountDurableStateBehavior struct {
	id string
}

// enforces compilation error
var (
	_ behaviorport.DurableState = (*AccountDurableStateBehavior)(nil)
	_ extension.Dependency      = (*AccountDurableStateBehavior)(nil)
)

// NewAccountDurableStateBehavior returns a behavior for the account id.
func NewAccountDurableStateBehavior(id string) *AccountDurableStateBehavior {
	return &AccountDurableStateBehavior{id: id}
}

func (x *AccountDurableStateBehavior) ID() string {
	return x.id
}

func (x *AccountDurableStateBehavior) InitialState() proto.Message {
	return new(testpb.Account)
}

// nolint
func (x *AccountDurableStateBehavior) HandleCommand(ctx context.Context, command proto.Message, priorVersion uint64, priorState proto.Message) (newState proto.Message, newVersion uint64, err error) {
	switch cmd := command.(type) {
	case *testpb.CreateAccount:
		return &testpb.Account{
			AccountId:      x.id,
			AccountBalance: cmd.GetAccountBalance(),
		}, priorVersion + 1, nil

	case *testpb.CreditAccount:
		if cmd.GetAccountId() == x.id {
			account := priorState.(*testpb.Account)
			bal := account.GetAccountBalance() + cmd.GetBalance()

			return &testpb.Account{
				AccountId:      cmd.GetAccountId(),
				AccountBalance: bal,
			}, priorVersion + 1, nil
		}

		return nil, 0, errors.New("command sent to the wrong entity")

	default:
		return nil, 0, errors.New("unhandled command")
	}
}

func (x *AccountDurableStateBehavior) MarshalBinary() (data []byte, err error) {
	serializable := struct {
		ID string `json:"id"`
	}{
		ID: x.id,
	}
	return json.Marshal(serializable)
}

func (x *AccountDurableStateBehavior) UnmarshalBinary(data []byte) error {
	serializable := struct {
		ID string `json:"id"`
	}{}

	if err := json.Unmarshal(data, &serializable); err != nil {
		return err
	}

	x.id = serializable.ID
	return nil
}
