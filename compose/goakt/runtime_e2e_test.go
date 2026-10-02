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

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/urd/compose"
	egoakt "github.com/getsyntegrity/urd/compose/goakt"
	"github.com/getsyntegrity/urd/engine"
	"github.com/getsyntegrity/urd/internal/runtimeconsumer"
	"github.com/getsyntegrity/urd/testkit"
)

// TestRuntime_ConsumerDrivesTheAppEndToEnd is #147's end-to-end criterion
// (ego-runtime-001 §D8): internal/runtimeconsumer, whose build closure holds
// neither package engine nor GoAkt, spawns a behavior, sends it two commands
// and reads its state through App.Runtime() on a real GoAkt application.
func TestRuntime_ConsumerDrivesTheAppEndToEnd(t *testing.T) {
	specs.Describe(t, "internal/runtimeconsumer driving a real GoAkt App through App.Runtime()", func(s *specs.Spec) {
		s.It("spawns, commands and reads an entity end to end", func(ctx *specs.Context) {
			bg := context.Background()
			events := testkit.NewEventsStore()
			ctx.Expect(events.Connect(bg)).To(specs.BeNil())
			ctx.Cleanup(func() { _ = events.Disconnect(bg) })

			app, err := egoakt.New(compose.Spec{
				Name:            "runtime-consumer-e2e",
				Families:        compose.EventSourced,
				EventsStore:     events,
				ShutdownTimeout: 20 * time.Second,
			}, egoakt.WithLogger(engine.DiscardLogger))
			ctx.Expect(err).To(specs.BeNil())
			// Stop is idempotent (a no-op after Stop or a failed Start), so this
			// cleanup only matters when the case fails before its explicit Stop;
			// its error is deliberately ignored, the explicit Stop below checks it.
			ctx.Cleanup(func() { _ = app.Stop(context.Background()) })
			ctx.Expect(app.Start(bg)).To(specs.BeNil())

			account, err := runtimeconsumer.Run(bg, app.Runtime())
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(account.GetAccountId()).To(specs.Equal(runtimeconsumer.AccountID))
			ctx.Expect(account.GetAccountBalance()).To(specs.Equal(runtimeconsumer.FinalBalance))

			exists, err := app.Runtime().EntityExists(bg, runtimeconsumer.AccountID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(exists).To(specs.BeTrue())

			ctx.Expect(app.Stop(bg)).To(specs.BeNil())
		})
	})
}
