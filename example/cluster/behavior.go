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

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/getsyntegrity/ego/engine"
	samplepb "github.com/getsyntegrity/ego/example/examplepb"
	behaviorport "github.com/getsyntegrity/ego/port/behavior"
)

// AccountBehavior implements behaviorport.EventSourced (port/behavior) for a
// bank account. It handles CreateAccount, CreditAccount, and DebitAccount
// commands. This example runs in cluster mode, so it also implements
// MarshalBinary/UnmarshalBinary (engine.BehaviorKind) so GoAkt can place and
// relocate it across nodes; main.go registers it with
// engine.WithBehaviorKinds so every node can decode a spawn a peer places on it.
type AccountBehavior struct {
	id string
}

var (
	_ behaviorport.EventSourced = (*AccountBehavior)(nil)
	_ engine.BehaviorKind       = (*AccountBehavior)(nil)
)

func NewAccountBehavior(id string) *AccountBehavior {
	return &AccountBehavior{id: id}
}

func (a *AccountBehavior) ID() string {
	return a.id
}

func (a *AccountBehavior) InitialState() engine.State {
	return new(samplepb.Account)
}

func (a *AccountBehavior) HandleCommand(_ context.Context, command engine.Command, priorState engine.State) ([]engine.Event, error) {
	switch cmd := command.(type) {
	case *samplepb.CreateAccount:
		return []engine.Event{
			&samplepb.AccountCreated{
				AccountId:      cmd.GetAccountId(),
				AccountBalance: cmd.GetAccountBalance(),
			},
		}, nil

	case *samplepb.CreditAccount:
		return []engine.Event{
			&samplepb.AccountCredited{
				AccountId:      cmd.GetAccountId(),
				AccountBalance: cmd.GetBalance(),
			},
		}, nil

	case *samplepb.DebitAccount:
		account := priorState.(*samplepb.Account)
		if account.GetAccountBalance() < cmd.GetBalance() {
			return nil, fmt.Errorf("insufficient funds: have %.2f, need %.2f",
				account.GetAccountBalance(), cmd.GetBalance())
		}
		return []engine.Event{
			&samplepb.AccountDebited{
				AccountId:      cmd.GetAccountId(),
				AccountBalance: cmd.GetBalance(),
			},
		}, nil

	default:
		return nil, errors.New("unhandled command")
	}
}

func (a *AccountBehavior) HandleEvent(_ context.Context, event engine.Event, priorState engine.State) (engine.State, error) {
	switch evt := event.(type) {
	case *samplepb.AccountCreated:
		return &samplepb.Account{
			AccountId:      evt.GetAccountId(),
			AccountBalance: evt.GetAccountBalance(),
		}, nil

	case *samplepb.AccountCredited:
		account := priorState.(*samplepb.Account)
		return &samplepb.Account{
			AccountId:      evt.GetAccountId(),
			AccountBalance: account.GetAccountBalance() + evt.GetAccountBalance(),
		}, nil

	case *samplepb.AccountDebited:
		account := priorState.(*samplepb.Account)
		return &samplepb.Account{
			AccountId:      evt.GetAccountId(),
			AccountBalance: account.GetAccountBalance() - evt.GetAccountBalance(),
		}, nil

	default:
		return nil, errors.New("unhandled event")
	}
}

func (a *AccountBehavior) MarshalBinary() ([]byte, error) {
	data := struct {
		ID string `json:"id"`
	}{ID: a.id}
	return json.Marshal(data)
}

func (a *AccountBehavior) UnmarshalBinary(data []byte) error {
	aux := struct {
		ID string `json:"id"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	a.id = aux.ID
	return nil
}
