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

package ego

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/pablogore/ego/v4/command"
	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
)

// -----------------------------------------------------------------------
// Regression coverage for the D9 batched-path defect found in adversarial
// review: a batch founded by a command with no ExpectedRevision (U) that
// later admits a command which DOES declare one (C) must still anchor the
// flush's single physical WriteEvents call to a real CAS precondition —
// batchHasPrecondition must become true the moment ANY admitted command in
// the cycle (founder or later) declares a revision, not only the founder,
// and batchBase must never move once the batch has opened. See
// openspec/changes/ego-write-004/design.md D9 and event_sourced_actor.go's
// batchBase/batchHasPrecondition field comment and processAndBatch.
//
// The harness below drives a precise, deterministic sequence of commands
// into the SAME open batch cycle. A batched command's Dispatch call blocks
// (the actor stashes it) until the eventual flush, so each step is fired
// from its own goroutine; the test does not move on to the next step until
// it has observed the current step's HandleCommand actually begin. Since a
// goakt actor processes its mailbox strictly one message to completion at a
// time, that observation proves the previous step's entire processAndBatch
// (admission gate, batchBase/batchHasPrecondition seeding, batchEntries
// append) has already finished running before the next step is even
// considered for admission — without relying on real-time sleeps.
// -----------------------------------------------------------------------

// batchStepBehavior wraps AccountEventSourcedBehavior, announcing the start
// of every HandleCommand call on entered so a test can synchronize on it.
type batchStepBehavior struct {
	*AccountEventSourcedBehavior
	entered chan Command
}

func newBatchStepBehavior(id string) *batchStepBehavior {
	return &batchStepBehavior{
		AccountEventSourcedBehavior: NewAccountEventSourcedBehavior(id),
		entered:                     make(chan Command),
	}
}

func (x *batchStepBehavior) HandleCommand(ctx context.Context, cmd Command, state State) ([]Event, error) {
	x.entered <- cmd
	return x.AccountEventSourcedBehavior.HandleCommand(ctx, cmd, state)
}

// batchStep is one command to dispatch, with its optional metadata (e.g. an
// ExpectedRevision declaration).
type batchStep struct {
	payload proto.Message
	opts    []command.MetadataOption
}

// stepU builds a step that declares no ExpectedRevision at all.
func stepU(payload proto.Message) batchStep {
	return batchStep{payload: payload}
}

// stepC builds a step that declares ExpectedRevision(revision).
func stepC(payload proto.Message, revision uint64) batchStep {
	return batchStep{payload: payload, opts: []command.MetadataOption{command.WithExpectedRevision(revision)}}
}

type batchDispatchOutcome struct {
	result command.Result
	err    error
}

// runBatchSequence dispatches each of steps, in strict order, into the same
// actor/batch cycle and returns each step's command.Result in the same
// order. See the package doc comment above this type for the
// synchronization argument.
func runBatchSequence(t *testing.T, engine *Engine, entityID string, behavior *batchStepBehavior, steps []batchStep) []command.Result {
	t.Helper()

	chans := make([]chan batchDispatchOutcome, len(steps))
	for i, step := range steps {
		md, err := command.NewMetadata(command.OperationID(uuid.NewString()), step.opts...)
		require.NoError(t, err)
		env, err := command.NewEnvelope(step.payload, md)
		require.NoError(t, err)

		ch := make(chan batchDispatchOutcome, 1)
		chans[i] = ch
		go func() {
			result, dispatchErr := engine.Dispatch(context.Background(), entityID, env, 15*time.Second)
			ch <- batchDispatchOutcome{result: result, err: dispatchErr}
		}()

		select {
		case <-behavior.entered:
		case <-time.After(5 * time.Second):
			t.Fatalf("step %d (%T) never reached HandleCommand", i, step.payload)
		}
	}

	results := make([]command.Result, len(steps))
	for i, ch := range chans {
		select {
		case out := <-ch:
			require.NoError(t, out.err, "step %d dispatch error", i)
			results[i] = out.result
		case <-time.After(15 * time.Second):
			t.Fatalf("step %d never returned a result", i)
		}
	}
	return results
}

// newBatchHarness spins up a fresh engine/entity pair wired through a
// preconditionSpyEventsStore (defined in event_sourced_actor_expected_revision_test.go)
// so the test can assert on the exact persistence.WritePrecondition each
// physical WriteEvents call actually received, and returns the pieces a test
// needs to drive it.
func newBatchHarness(t *testing.T, name string, threshold int) (engine *Engine, entityID string, behavior *batchStepBehavior, spy *preconditionSpyEventsStore, store *testkit.EventStore) {
	t.Helper()
	ctx := context.Background()

	underlying := testkit.NewEventsStore()
	require.NoError(t, underlying.Connect(ctx))
	t.Cleanup(func() { _ = underlying.Disconnect(ctx) })
	spy = &preconditionSpyEventsStore{EventStore: underlying}

	engine = newTestEngine(t, name, spy, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID = uuid.NewString()
	behavior = newBatchStepBehavior(entityID)
	require.NoError(t, engine.Entity(ctx, behavior, WithBatchThreshold(threshold)))

	return engine, entityID, behavior, spy, underlying
}

// TestBatchedPreconditionMatrix_GenesisBase exercises every U/C admission
// sequence design.md D9 names, on a brand-new aggregate (genesis base), and
// verifies the exact persistence.WritePrecondition that reaches the store's
// WriteEvents for the batch's one physical flush — not merely a boolean
// batchHasPrecondition inspection. U = no ExpectedRevision declared, C =
// ExpectedRevision declared and admitted. Each step produces exactly one
// event, so batchThreshold is set to len(steps) for each case: the batch
// stays open across every step and flushes exactly once, right after the
// last one.
func TestBatchedPreconditionMatrix_GenesisBase(t *testing.T) {
	tests := []struct {
		name  string
		build func(entityID string) []batchStep
		want  persistence.WritePrecondition
	}{
		{
			name: "U_U",
			build: func(id string) []batchStep {
				return []batchStep{
					stepU(&testpb.CreateAccount{AccountBalance: 500}),
					stepU(&testpb.CreditAccount{AccountId: id, Balance: 10}),
				}
			},
			want: persistence.Unconditional(),
		},
		{
			name: "U_C",
			build: func(id string) []batchStep {
				return []batchStep{
					stepU(&testpb.CreateAccount{AccountBalance: 500}),
					stepC(&testpb.CreditAccount{AccountId: id, Balance: 10}, 1),
				}
			},
			want: persistence.ExpectGenesis(),
		},
		{
			name: "C_U",
			build: func(id string) []batchStep {
				return []batchStep{
					stepC(&testpb.CreateAccount{AccountBalance: 500}, 0),
					stepU(&testpb.CreditAccount{AccountId: id, Balance: 10}),
				}
			},
			want: persistence.ExpectGenesis(),
		},
		{
			name: "C_C",
			build: func(id string) []batchStep {
				return []batchStep{
					stepC(&testpb.CreateAccount{AccountBalance: 500}, 0),
					stepC(&testpb.CreditAccount{AccountId: id, Balance: 10}, 1),
				}
			},
			want: persistence.ExpectGenesis(),
		},
		{
			name: "U_U_C",
			build: func(id string) []batchStep {
				return []batchStep{
					stepU(&testpb.CreateAccount{AccountBalance: 500}),
					stepU(&testpb.CreditAccount{AccountId: id, Balance: 10}),
					stepC(&testpb.CreditAccount{AccountId: id, Balance: 10}, 2),
				}
			},
			want: persistence.ExpectGenesis(),
		},
		{
			name: "U_C_U",
			build: func(id string) []batchStep {
				return []batchStep{
					stepU(&testpb.CreateAccount{AccountBalance: 500}),
					stepC(&testpb.CreditAccount{AccountId: id, Balance: 10}, 1),
					stepU(&testpb.CreditAccount{AccountId: id, Balance: 10}),
				}
			},
			want: persistence.ExpectGenesis(),
		},
		{
			name: "C_U_C",
			build: func(id string) []batchStep {
				return []batchStep{
					stepC(&testpb.CreateAccount{AccountBalance: 500}, 0),
					stepU(&testpb.CreditAccount{AccountId: id, Balance: 10}),
					stepC(&testpb.CreditAccount{AccountId: id, Balance: 10}, 2),
				}
			},
			want: persistence.ExpectGenesis(),
		},
		{
			name: "C_C_C",
			build: func(id string) []batchStep {
				return []batchStep{
					stepC(&testpb.CreateAccount{AccountBalance: 500}, 0),
					stepC(&testpb.CreditAccount{AccountId: id, Balance: 10}, 1),
					stepC(&testpb.CreditAccount{AccountId: id, Balance: 10}, 2),
				}
			},
			want: persistence.ExpectGenesis(),
		},
		{
			// Multiple consecutive C's, beyond just three in a row.
			name: "C_C_C_C",
			build: func(id string) []batchStep {
				return []batchStep{
					stepC(&testpb.CreateAccount{AccountBalance: 500}, 0),
					stepC(&testpb.CreditAccount{AccountId: id, Balance: 10}, 1),
					stepC(&testpb.CreditAccount{AccountId: id, Balance: 10}, 2),
					stepC(&testpb.CreditAccount{AccountId: id, Balance: 10}, 3),
				}
			},
			want: persistence.ExpectGenesis(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// entityID must be known before steps are built (CreditAccount's
			// AccountId must match), so build a throwaway ID first, then
			// spin the harness with it explicitly.
			ctx := context.Background()
			underlying := testkit.NewEventsStore()
			require.NoError(t, underlying.Connect(ctx))
			t.Cleanup(func() { _ = underlying.Disconnect(ctx) })
			spy := &preconditionSpyEventsStore{EventStore: underlying}

			entityID := uuid.NewString()
			steps := tc.build(entityID)

			engine := newTestEngine(t, "matrix-genesis-"+tc.name, spy, WithLogger(DiscardLogger))
			require.NoError(t, engine.Start(ctx))
			behavior := newBatchStepBehavior(entityID)
			require.NoError(t, engine.Entity(ctx, behavior, WithBatchThreshold(len(steps))))

			results := runBatchSequence(t, engine, entityID, behavior, steps)

			for i, result := range results {
				require.Equal(t, command.OutcomeSuccess, result.Outcome(), "step %d", i)
			}

			require.Len(t, spy.preconditions, 1, "exactly one physical flush for the whole batch")
			assert.Equal(t, tc.want, spy.preconditions[0])

			event, err := underlying.GetLatestEvent(ctx, entityID)
			require.NoError(t, err)
			require.NotNil(t, event)
			assert.EqualValues(t, len(steps), event.GetSequenceNumber(), "every step's event actually committed")
		})
	}
}

// TestBatchedPhysicalBaseAnchorsToPreBatchRevision_NotLogicalCounter is the
// critical Step 3 test: it proves the batch's physical CAS is anchored to
// the store revision that existed the moment the batch opened (batchBase),
// never to the running logical batchCounter a later admitted command's own
// declared revision advances to.
//
// Persistence starts at revision R (=2, established by an earlier,
// already-flushed batch). A fresh batch then opens: C1 (=U here) founds it
// unconditionally (batchBase seeded to eventsCounter==R==2); C2 (=C) is
// admitted declaring ExpectedRevision == batchCounter (3, the revision the
// batch reaches immediately before C2's own event) — matching design.md
// D9's admission rule. The flush must still check storage against R (2),
// never against 3 (C2's own declared value) nor 4 (the batch's logical end
// revision after both commands).
func TestBatchedPhysicalBaseAnchorsToPreBatchRevision_NotLogicalCounter(t *testing.T) {
	ctx := context.Background()
	engine, entityID, behavior, spy, store := newBatchHarness(t, "physical-base-vs-logical", 2)

	// Prefix phase: two unconditional commands, in their own batch cycle
	// (batchThreshold==2 auto-flushes right after them), to establish a
	// real, already-confirmed, non-zero storage revision R=2.
	prefix := []batchStep{
		stepU(&testpb.CreateAccount{AccountBalance: 500}),
		stepU(&testpb.CreditAccount{AccountId: entityID, Balance: 10}),
	}
	prefixResults := runBatchSequence(t, engine, entityID, behavior, prefix)
	for i, result := range prefixResults {
		require.Equal(t, command.OutcomeSuccess, result.Outcome(), "prefix step %d", i)
	}
	require.Len(t, spy.preconditions, 1)
	assert.Equal(t, persistence.Unconditional(), spy.preconditions[0])

	event, err := store.GetLatestEvent(ctx, entityID)
	require.NoError(t, err)
	require.EqualValues(t, 2, event.GetSequenceNumber(), "R=2 after the prefix batch's flush")

	// Batch-under-test: U founds (batchBase seeded to eventsCounter==2),
	// C is admitted declaring ExpectedRevision(3) (batchCounter after U's
	// own event). The physical CAS must use base 2, not 3, and not the
	// batch's logical end revision (4).
	underTest := []batchStep{
		stepU(&testpb.CreditAccount{AccountId: entityID, Balance: 10}),
		stepC(&testpb.CreditAccount{AccountId: entityID, Balance: 10}, 3),
	}
	results := runBatchSequence(t, engine, entityID, behavior, underTest)
	for i, result := range results {
		require.Equal(t, command.OutcomeSuccess, result.Outcome(), "batch-under-test step %d", i)
	}

	require.Len(t, spy.preconditions, 2)
	got := spy.preconditions[1]
	assert.Equal(t, persistence.ExpectRevision(2), got, "must anchor to the PRE-BATCH physical base (R=2), not the logical mid/end-of-batch revision")
	assert.NotEqual(t, persistence.ExpectRevision(3), got)
	assert.NotEqual(t, persistence.ExpectRevision(4), got)

	event, err = store.GetLatestEvent(ctx, entityID)
	require.NoError(t, err)
	assert.EqualValues(t, 4, event.GetSequenceNumber(), "both batches' events are committed: 2 (prefix) + 2 (under test)")
}

// TestBatchedZeroEventAdmittedCommandStillPreservesLaterPrecondition is
// Step 5's core case: a command admitted into an open batch with a matching
// declared ExpectedRevision, but whose handler produces zero events (an
// idempotent no-op via testpb.TestNoEvent), must still cause
// batchHasPrecondition to become true — it must not be silently lost merely
// because that particular command happened to persist nothing of its own.
func TestBatchedZeroEventAdmittedCommandStillPreservesLaterPrecondition(t *testing.T) {
	ctx := context.Background()
	// threshold=2: only the two *event-producing* commands (U founder, U
	// filler) count toward it; the zero-event C in between contributes
	// nothing to batchNumEvents, so it does not itself trigger a flush.
	engine, entityID, behavior, spy, store := newBatchHarness(t, "zero-event-preserves-precondition", 2)

	// testpb.TestNoEvent is the zero-event command AccountEventSourcedBehavior
	// recognizes (HandleCommand returns nil, nil for it).
	realSteps := []batchStep{
		stepU(&testpb.CreateAccount{AccountBalance: 500}),
		stepC(&testpb.TestNoEvent{}, 1),                                // admitted (1 == batchCounter), produces 0 events
		stepU(&testpb.CreditAccount{AccountId: entityID, Balance: 10}), // filler: reaches threshold, forces the flush
	}

	results := runBatchSequence(t, engine, entityID, behavior, realSteps)
	for i, result := range results {
		require.Equal(t, command.OutcomeSuccess, result.Outcome(), "step %d", i)
	}

	// The zero-event step's own reply must report the unchanged revision
	// (1, from the founder alone) — it never advanced batchCounter itself.
	assert.EqualValues(t, 1, results[1].Revision(), "zero-event command must not itself advance the batch counter")

	require.Len(t, spy.preconditions, 1, "exactly one physical flush")
	assert.Equal(t, persistence.ExpectGenesis(), spy.preconditions[0], "the zero-event command's declared ExpectedRevision(1) must still be honored as a real CAS precondition")

	event, err := store.GetLatestEvent(ctx, entityID)
	require.NoError(t, err)
	assert.EqualValues(t, 2, event.GetSequenceNumber(), "only the two real events (founder + filler) were ever persisted")
}

// TestBatchedZeroEventFounderNeverOpensBatch documents the companion case:
// a command that would otherwise found a brand-new batch, but itself
// produces zero events, never actually opens one — batchBase/batchHasPrecondition
// stay untouched, and the very next (event-producing) command becomes the
// real founder instead.
func TestBatchedZeroEventFounderNeverOpensBatch(t *testing.T) {
	ctx := context.Background()
	// threshold=1: the real founder (the second command here) auto-flushes
	// on its own, since it is alone in its batch cycle.
	engine, entityID, behavior, spy, store := newBatchHarness(t, "zero-event-founder-noop", 1)

	steps := []batchStep{
		stepC(&testpb.TestNoEvent{}, 0),                   // would-be founder; produces 0 events; batch never opens
		stepU(&testpb.CreateAccount{AccountBalance: 500}), // the real founder
	}
	results := runBatchSequence(t, engine, entityID, behavior, steps)
	for i, result := range results {
		require.Equal(t, command.OutcomeSuccess, result.Outcome(), "step %d", i)
	}

	require.Len(t, spy.preconditions, 1)
	assert.Equal(t, persistence.Unconditional(), spy.preconditions[0], "the zero-event command must not have anchored a genesis batchBase")

	event, err := store.GetLatestEvent(ctx, entityID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, event.GetSequenceNumber(), "only the real founder's single event was ever persisted")
}

// TestBatchAdmissionGateRejectsStaleRevision_ForcesEarlyFlushThenFoundsFreshBatch
// covers Step 6's stale/mismatched-revision edge: a command declaring an
// ExpectedRevision that does not match the currently open batch's counter is
// never admitted into it. Instead (design.md D9 step 4) it forces an early
// flush of what is already staged, then founds a brand-new batch on its own
// — where its own stale declared revision is still caught by the store's
// real CAS at that new batch's own flush, not silently ignored.
func TestBatchAdmissionGateRejectsStaleRevision_ForcesEarlyFlushThenFoundsFreshBatch(t *testing.T) {
	ctx := context.Background()
	engine, entityID, behavior, spy, store := newBatchHarness(t, "stale-revision-admission", 2)

	steps := []batchStep{
		stepU(&testpb.CreateAccount{AccountBalance: 500}),                  // founder, batch stays open (threshold=2, 1 event so far)
		stepC(&testpb.CreditAccount{AccountId: entityID, Balance: 10}, 99), // stale: batchCounter is 1, not 99
		stepU(&testpb.CreditAccount{AccountId: entityID, Balance: 10}),     // filler for the fresh batch the stale command founds
	}
	results := runBatchSequence(t, engine, entityID, behavior, steps)

	// Step 0 (U, the original founder) is confirmed by the forced early
	// flush (Unconditional, since nothing had declared a revision yet).
	require.Equal(t, command.OutcomeSuccess, results[0].Outcome())

	// Steps 1 and 2 land in a second batch founded by the stale command
	// itself (batchBase=99, its own declared value); that batch's flush
	// checks storage (real revision 1) against 99 and must conflict.
	require.Equal(t, command.OutcomeRejected, results[1].Outcome())
	require.Equal(t, command.OutcomeRejected, results[2].Outcome())
	for i, result := range results[1:] {
		failure, ok := result.Failure()
		require.True(t, ok, "step %d", i+1)
		code, hasCode := failure.Code()
		require.True(t, hasCode, "step %d", i+1)
		assert.Equal(t, command.CodeConcurrencyConflict, code, "step %d", i+1)
	}

	require.Len(t, spy.preconditions, 2, "the forced early flush plus the stale command's own fresh-batch flush")
	assert.Equal(t, persistence.Unconditional(), spy.preconditions[0], "forced flush of the original open batch: nothing in it had declared a revision")
	assert.Equal(t, persistence.ExpectRevision(99), spy.preconditions[1], "the stale command's own fresh batch anchors to its own declared (stale) revision")

	event, err := store.GetLatestEvent(ctx, entityID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, event.GetSequenceNumber(), "only the original founder's event ever committed; the stale-founded batch's conflict must not have persisted anything")
}

// TestBatchedExternalWriterWinsCAS_RejectsWholeBatchWithoutAdvancingCounter
// is Step 4's mandatory adversarial test: an independent writer advances
// persistence, behind the actor's back, after a batch has already admitted
// a command declaring an ExpectedRevision but before that batch physically
// flushes. The flush's real CAS (exercised through the actual
// persistence/testkit store, not merely batchHasPrecondition inspection)
// must fail, every command in the batch must surface OutcomeRejected with
// Failure.Code()==CodeConcurrencyConflict, no rejected event may be
// confirmed, and eventsCounter must not advance (design.md D9/D10).
func TestBatchedExternalWriterWinsCAS_RejectsWholeBatchWithoutAdvancingCounter(t *testing.T) {
	ctx := context.Background()
	// threshold=3: after the two official commands (U founder + C, 2
	// events), the batch stays open — giving the test a window to act as
	// an external writer before the third (filler) command tips it over
	// the threshold and forces the flush.
	engine, entityID, behavior, spy, store := newBatchHarness(t, "external-writer-cas-conflict", 3)

	official := []batchStep{
		stepU(&testpb.CreateAccount{AccountBalance: 500}),                 // founder: batchBase=0 (genesis)
		stepC(&testpb.CreditAccount{AccountId: entityID, Balance: 10}, 1), // admitted: batchHasPrecondition=true
	}
	officialChans := make([]chan batchDispatchOutcome, len(official))
	for i, step := range official {
		md, err := command.NewMetadata(command.OperationID(uuid.NewString()), step.opts...)
		require.NoError(t, err)
		env, err := command.NewEnvelope(step.payload, md)
		require.NoError(t, err)

		ch := make(chan batchDispatchOutcome, 1)
		officialChans[i] = ch
		go func() {
			result, dispatchErr := engine.Dispatch(context.Background(), entityID, env, 15*time.Second)
			ch <- batchDispatchOutcome{result: result, err: dispatchErr}
		}()

		select {
		case <-behavior.entered:
		case <-time.After(5 * time.Second):
			t.Fatalf("official step %d never reached HandleCommand", i)
		}
	}

	// At this point both official commands are staged (2 events, threshold
	// 3 not yet reached) and the actor is idle, waiting on its mailbox.
	// Act as an independent external writer: commit an event directly to
	// the SAME persistence-id's stream, bypassing the actor entirely, which
	// physically advances the store to revision 1 while the batch's
	// anchored batchBase (genesis, 0) and the actor's own eventsCounter
	// (still 0, nothing confirmed yet) know nothing about it.
	externalEventAny, err := anypb.New(&testpb.AccountCreated{AccountId: entityID, AccountBalance: 999})
	require.NoError(t, err)
	externalEvent := &egopb.Event{
		PersistenceId:  entityID,
		SequenceNumber: 1,
		Event:          externalEventAny,
		Timestamp:      time.Now().Unix(),
		Shard:          0,
	}
	require.NoError(t, store.WriteEvents(ctx, []*egopb.Event{externalEvent}, persistence.Unconditional()))

	// Now dispatch the filler command that tips batchNumEvents over the
	// threshold, forcing the flush.
	fillerMd, err := command.NewMetadata(command.OperationID(uuid.NewString()))
	require.NoError(t, err)
	fillerEnv, err := command.NewEnvelope(&testpb.CreditAccount{AccountId: entityID, Balance: 10}, fillerMd)
	require.NoError(t, err)
	fillerCh := make(chan batchDispatchOutcome, 1)
	go func() {
		result, dispatchErr := engine.Dispatch(context.Background(), entityID, fillerEnv, 15*time.Second)
		fillerCh <- batchDispatchOutcome{result: result, err: dispatchErr}
	}()
	select {
	case <-behavior.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("filler command never reached HandleCommand")
	}

	allChans := append(officialChans, fillerCh)
	results := make([]command.Result, len(allChans))
	for i, ch := range allChans {
		select {
		case out := <-ch:
			require.NoError(t, out.err, "step %d dispatch error", i)
			results[i] = out.result
		case <-time.After(15 * time.Second):
			t.Fatalf("step %d never returned a result", i)
		}
	}

	for i, result := range results {
		require.Equal(t, command.OutcomeRejected, result.Outcome(), "step %d", i)
		failure, ok := result.Failure()
		require.True(t, ok, "step %d", i)
		code, hasCode := failure.Code()
		require.True(t, hasCode, "step %d", i)
		assert.Equal(t, command.CodeConcurrencyConflict, code, "step %d", i)

		var conflict *persistence.ConflictError
		require.True(t, errors.As(result.Err(), &conflict), "step %d", i)
		assert.Equal(t, persistence.ExpectGenesis(), conflict.Expected(), "step %d", i)
		actual, ok := conflict.ActualRevision()
		require.True(t, ok, "step %d", i)
		assert.EqualValues(t, 1, actual, "step %d: the external writer's event", i)
	}

	require.Len(t, spy.preconditions, 1)
	assert.Equal(t, persistence.ExpectGenesis(), spy.preconditions[0])

	// D10 recheck: no rejected event was confirmed, and eventsCounter must
	// not have advanced as a result of the rejected persist — only the
	// external writer's single event is visible in the store.
	event, err := store.GetLatestEvent(ctx, entityID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, event.GetSequenceNumber(), "only the external writer's event ever committed")

	var committed testpb.AccountCreated
	require.NoError(t, event.GetEvent().UnmarshalTo(&committed))
	assert.EqualValues(t, 999, committed.GetAccountBalance(), "the visible event is the external writer's own, not one of the rejected batch's")
}
