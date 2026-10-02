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

// Package runtimeconsumer is evidence for #147, not API: consumer code
// written only against the runtime-neutral contracts — port/runtime,
// port/behavior and the internal/testpb messages — never against package
// ego or GoAkt (openspec/changes/ego-runtime-001/design.md §D8). Its
// closure test keeps it that way; compose/goakt's end-to-end test drives a
// real application through it with App.Runtime().
package runtimeconsumer

import (
	"context"
	"fmt"
	"time"

	testpb "github.com/getsyntegrity/urd/internal/testpb"
	behaviorport "github.com/getsyntegrity/urd/port/behavior"
	runtimeport "github.com/getsyntegrity/urd/port/runtime"
)

const (
	// AccountID is the ID of the entity Run spawns.
	AccountID = "runtimeconsumer-account"
	// OpeningBalance is the balance of Run's CreateAccount command.
	OpeningBalance = 100.0
	// Credit is the amount of Run's CreditAccount command.
	Credit = 50.0
	// FinalBalance is the balance Run's returned state carries.
	FinalBalance = OpeningBalance + Credit

	// commandTimeout bounds each command's reply.
	commandTimeout = 10 * time.Second
)

// Run spawns an account entity on r, sends it CreateAccount and then
// CreditAccount, and returns the state after the second command. It
// depends only on runtimeport.Entities, so it runs unchanged on any runtime.
func Run(ctx context.Context, r runtimeport.Entities) (*testpb.Account, error) {
	if err := r.SpawnEventSourced(ctx, &account{id: AccountID}); err != nil {
		return nil, fmt.Errorf("spawn %q: %w", AccountID, err)
	}
	if _, _, err := r.SendCommand(ctx, AccountID, &testpb.CreateAccount{AccountBalance: OpeningBalance}, commandTimeout); err != nil {
		return nil, fmt.Errorf("create %q: %w", AccountID, err)
	}
	state, _, err := r.SendCommand(ctx, AccountID, &testpb.CreditAccount{AccountId: AccountID, Balance: Credit}, commandTimeout)
	if err != nil {
		return nil, fmt.Errorf("credit %q: %w", AccountID, err)
	}
	acc, ok := state.(*testpb.Account)
	if !ok {
		return nil, fmt.Errorf("credit %q: state is %T, want *testpb.Account", AccountID, state)
	}
	return acc, nil
}

// account is an event-sourced account written against port/behavior only.
type account struct{ id string }

var _ behaviorport.EventSourced = (*account)(nil)

func (a *account) ID() string { return a.id }

func (a *account) InitialState() behaviorport.State { return new(testpb.Account) }

func (a *account) HandleCommand(_ context.Context, cmd behaviorport.Command, _ behaviorport.State) ([]behaviorport.Event, error) {
	switch c := cmd.(type) {
	case *testpb.CreateAccount:
		return []behaviorport.Event{&testpb.AccountCreated{AccountId: a.id, AccountBalance: c.GetAccountBalance()}}, nil
	case *testpb.CreditAccount:
		return []behaviorport.Event{&testpb.AccountCredited{AccountId: a.id, AccountBalance: c.GetBalance()}}, nil
	default:
		return nil, fmt.Errorf("unhandled command %T", cmd)
	}
}

func (a *account) HandleEvent(_ context.Context, evt behaviorport.Event, prior behaviorport.State) (behaviorport.State, error) {
	switch e := evt.(type) {
	case *testpb.AccountCreated:
		return &testpb.Account{AccountId: e.GetAccountId(), AccountBalance: e.GetAccountBalance()}, nil
	case *testpb.AccountCredited:
		acc, ok := prior.(*testpb.Account)
		if !ok {
			return nil, fmt.Errorf("prior state is %T, want *testpb.Account", prior)
		}
		return &testpb.Account{AccountId: e.GetAccountId(), AccountBalance: acc.GetAccountBalance() + e.GetAccountBalance()}, nil
	default:
		return nil, fmt.Errorf("unhandled event %T", evt)
	}
}
