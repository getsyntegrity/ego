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
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	kitlog "github.com/pablogore/kit-logger/pkg/logger"
	"google.golang.org/protobuf/proto"

	"github.com/getsyntegrity/ego/compose"
	egoakt "github.com/getsyntegrity/ego/compose/goakt"
	"github.com/getsyntegrity/ego/engine"
	samplepb "github.com/getsyntegrity/ego/internal/samplepb"
	behaviorport "github.com/getsyntegrity/ego/port/behavior"
	"github.com/getsyntegrity/ego/testkit"
)

func main() {
	// create the kit-logger Logger the whole runtime logs through: eGo, the
	// actor system it sits on, and this program
	logger := kitlog.New(kitlog.Config{Level: kitlog.LevelInfo, Format: kitlog.FormatText})
	if err := run(context.Background(), logger); err != nil {
		logger.Error("eventssourced example failed", "error", err)
		os.Exit(1)
	}
}

// run wires and runs the example through compose/goakt, eGo's GoAkt
// composition root (openspec/changes/ego-arch-003/design.md §5.1). Every
// cleanup step is registered with defer before the corresponding resource
// can fail later, so run never leaves anything open on its way out and main
// never calls os.Exit before those defers have executed.
func run(ctx context.Context, logger kitlog.Logger) error {
	// create the event store
	eventStore := testkit.NewEventsStore()
	if err := eventStore.Connect(ctx); err != nil {
		return fmt.Errorf("connect event store: %w", err)
	}
	// the consumer owns the store (design §D5): it connects before New and
	// disconnects after Stop. Deferred before app.Stop below, so it runs
	// after app.Stop, per Go's LIFO defer order.
	defer func() { _ = eventStore.Disconnect(ctx) }()

	// egoakt.New validates the Spec (V1-V8) with no I/O and starts nothing:
	// a configuration mistake is visible before any goroutine or connection
	// exists. Import compose/goakt as egoakt so it doesn't clash with the
	// goakt module.
	app, err := egoakt.New(compose.Spec{
		Name:        "Sample",
		Families:    compose.EventSourced,
		EventsStore: eventStore,
	}, egoakt.WithLogger(logger))
	if err != nil {
		return fmt.Errorf("build app: %w", err)
	}
	// required once New succeeded (design §D5): App owns the actor system,
	// the engine and, from here on, every publisher in the Spec (none in
	// this example). Stop is idempotent and a no-op after a failed Start.
	defer func() { _ = app.Stop(ctx) }()

	// app.Start probes the store, then starts the actor system and the
	// engine in a fixed order (design §D6); on failure it names the step
	// that failed and rolls back everything already started.
	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("start app: %w", err)
	}

	// create a persistence id
	entityID := uuid.NewString()
	// create an entity behavior with a given id
	behavior := NewAccountBehavior(entityID)
	// spawn the entity. This example runs on a single node, so the behavior
	// below (a domain-only behavior, no GoAkt serialization methods) never
	// needs to be serialized by GoAkt.
	if err := app.Engine().SpawnEventSourced(ctx, behavior); err != nil {
		return fmt.Errorf("spawn event-sourced behavior: %w", err)
	}

	// send some commands to the pid
	var command proto.Message
	// create an account
	command = &samplepb.CreateAccount{
		AccountId:      entityID,
		AccountBalance: 500.00,
	}
	reply, _, err := app.Engine().SendCommand(ctx, entityID, command, time.Minute)
	if err != nil {
		return fmt.Errorf("create account: %w", err)
	}
	account := reply.(*samplepb.Account)
	logger.Info("current balance on opening", "balance", account.GetAccountBalance())

	// send another command to credit the balance
	command = &samplepb.CreditAccount{
		AccountId: entityID,
		Balance:   250,
	}
	reply, _, err = app.Engine().SendCommand(ctx, entityID, command, time.Minute)
	if err != nil {
		return fmt.Errorf("credit account: %w", err)
	}
	account = reply.(*samplepb.Account)
	logger.Info("current balance after a credit of 250", "balance", account.GetAccountBalance())

	// capture ctrl+c
	interruptSignal := make(chan os.Signal, 1)
	signal.Notify(interruptSignal, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-interruptSignal

	return nil
	// the deferred app.Stop(ctx) and eventStore.Disconnect(ctx) above now
	// run, in that order, before main observes run's return value.
}

// AccountBehavior implements behaviorport.EventSourced (port/behavior). It
// has no GoAkt serialization methods: this example does not run in cluster
// mode, so it never needs to be serialized by GoAkt.
type AccountBehavior struct {
	id string
}

// make sure that AccountBehavior is a true persistence behavior
var _ behaviorport.EventSourced = &AccountBehavior{}

// NewAccountBehavior creates an instance of AccountBehavior
func NewAccountBehavior(id string) *AccountBehavior {
	return &AccountBehavior{id: id}
}

// ID returns the id
func (x *AccountBehavior) ID() string {
	return x.id
}

// InitialState returns the initial state
func (x *AccountBehavior) InitialState() engine.State {
	return engine.State(new(samplepb.Account))
}

// HandleCommand handles every command that is sent to the persistent behavior
func (x *AccountBehavior) HandleCommand(_ context.Context, command engine.Command, _ engine.State) (events []engine.Event, err error) {
	switch cmd := command.(type) {
	case *samplepb.CreateAccount:
		// TODO in production grid app validate the command using the prior state
		return []engine.Event{
			&samplepb.AccountCreated{
				AccountId:      cmd.GetAccountId(),
				AccountBalance: cmd.GetAccountBalance(),
			},
		}, nil

	case *samplepb.CreditAccount:
		// TODO in production grid app validate the command using the prior state
		return []engine.Event{
			&samplepb.AccountCredited{
				AccountId:      cmd.GetAccountId(),
				AccountBalance: cmd.GetBalance(),
			},
		}, nil

	default:
		return nil, errors.New("unhandled command")
	}
}

// HandleEvent handles every event emitted
func (x *AccountBehavior) HandleEvent(_ context.Context, event engine.Event, priorState engine.State) (state engine.State, err error) {
	switch evt := event.(type) {
	case *samplepb.AccountCreated:
		return &samplepb.Account{
			AccountId:      evt.GetAccountId(),
			AccountBalance: evt.GetAccountBalance(),
		}, nil

	case *samplepb.AccountCredited:
		account := priorState.(*samplepb.Account)
		bal := account.GetAccountBalance() + evt.GetAccountBalance()
		return &samplepb.Account{
			AccountId:      evt.GetAccountId(),
			AccountBalance: bal,
		}, nil

	default:
		return nil, errors.New("unhandled event")
	}
}
