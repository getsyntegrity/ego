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

package flows_test

import (
	"context"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"

	"github.com/getsyntegrity/ego/internal/engine/enginetest"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
)

const commandTimeout = 30 * time.Second

// recoveredAccount is what the restart flow compares: the identity, balance and revision of an account.
type recoveredAccount struct {
	id       string
	balance  float64
	revision uint64
}

// TestEngineRestart_RecoversEntityFromPostgres drives one event-sourced account through an engine whose events
// store is persistence/postgres, throws the whole actor system away, and starts a new one on the same database.
// The account must come back with the balance and revision it had, which only the stored events can supply.
func TestEngineRestart_RecoversEntityFromPostgres(t *testing.T) {
	t.Parallel()
	dsn := shared.NewDatabase(t)

	specs.Describe(t, "an engine on a Postgres events store", func(s *specs.Spec) {
		s.It("recovers an entity after the actor system restarts", func(sc *specs.Context) {
			ctx := context.Background()
			accountID := uuid.NewString()

			first := startNode(sc, dsn)
			sc.Expect(first.engine.SpawnEventSourced(ctx, enginetest.NewAccountEventSourcedBehavior(accountID))).To(specs.BeNil())

			state, revision, err := first.engine.SendCommand(ctx, accountID, &testpb.CreateAccount{AccountBalance: 500}, commandTimeout)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(state.(*testpb.Account).GetAccountBalance()).To(specs.Equal(500.0))
			sc.Expect(revision).To(specs.Equal(uint64(1)))

			state, revision, err = first.engine.SendCommand(ctx, accountID, &testpb.CreditAccount{AccountId: accountID, Balance: 250}, commandTimeout)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(state.(*testpb.Account).GetAccountBalance()).To(specs.Equal(750.0))
			sc.Expect(revision).To(specs.Equal(uint64(2)))

			first.stop(sc)

			second := startNode(sc, dsn)
			defer second.stop(sc)
			sc.Expect(second.engine.SpawnEventSourced(ctx, enginetest.NewAccountEventSourcedBehavior(accountID))).To(specs.BeNil())

			// TestNoEvent produces no event, so the reply is the recovered state and nothing more.
			sc.Eventually(func() any {
				recovered, rev, err := second.engine.SendCommand(ctx, accountID, &testpb.TestNoEvent{}, commandTimeout)
				if err != nil {
					return err
				}
				account, ok := recovered.(*testpb.Account)
				if !ok {
					return recovered
				}
				return recoveredAccount{id: account.GetAccountId(), balance: account.GetAccountBalance(), revision: rev}
			}, specs.Equal(recoveredAccount{id: accountID, balance: 750, revision: 2}), specs.WithTimeout(commandTimeout))

			// The restarted entity keeps going from the recovered revision.
			state, revision, err = second.engine.SendCommand(ctx, accountID, &testpb.CreditAccount{AccountId: accountID, Balance: 50}, commandTimeout)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(state.(*testpb.Account).GetAccountBalance()).To(specs.Equal(800.0))
			sc.Expect(revision).To(specs.Equal(uint64(3)))
		})
	})
}
