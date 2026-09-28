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

package goakt_test

import (
	"context"
	"testing"
	"time"

	"github.com/getsyntegrity/ego/v4/compose"
	egoakt "github.com/getsyntegrity/ego/v4/compose/goakt"
	"github.com/getsyntegrity/ego/v4/engine"
	"github.com/getsyntegrity/ego/v4/internal/runtimeconsumer"
	"github.com/getsyntegrity/ego/v4/testkit"
)

// TestRuntime_ConsumerDrivesTheAppEndToEnd is #147's end-to-end criterion
// (ego-runtime-001 §D8): internal/runtimeconsumer, whose build closure holds
// neither package ego nor GoAkt, spawns a behavior, sends it two commands
// and reads its state through App.Runtime() on a real GoAkt application.
func TestRuntime_ConsumerDrivesTheAppEndToEnd(t *testing.T) {
	ctx := context.Background()
	events := testkit.NewEventsStore()
	if err := events.Connect(ctx); err != nil {
		t.Fatalf("connect events store: %v", err)
	}
	t.Cleanup(func() { _ = events.Disconnect(ctx) })

	app, err := egoakt.New(compose.Spec{
		Name:            "runtime-consumer-e2e",
		Families:        compose.EventSourced,
		EventsStore:     events,
		ShutdownTimeout: 20 * time.Second,
	}, egoakt.WithLogger(engine.DiscardLogger))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Stop is idempotent (a no-op after Stop or a failed Start), so this
	// cleanup only matters when the test fails before its explicit Stop; its
	// error is deliberately ignored, the explicit Stop below checks it.
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	if err := app.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	account, err := runtimeconsumer.Run(ctx, app.Runtime())
	if err != nil {
		t.Fatalf("runtimeconsumer.Run: %v", err)
	}
	if account.GetAccountId() != runtimeconsumer.AccountID || account.GetAccountBalance() != runtimeconsumer.FinalBalance {
		t.Fatalf("final state = %v, want account %q with balance %v", account, runtimeconsumer.AccountID, runtimeconsumer.FinalBalance)
	}
	exists, err := app.Runtime().EntityExists(ctx, runtimeconsumer.AccountID)
	if err != nil || !exists {
		t.Fatalf("EntityExists(%q) = (%v, %v), want (true, nil)", runtimeconsumer.AccountID, exists, err)
	}

	if err := app.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
