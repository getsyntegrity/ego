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

package testkit

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/encryption"
	"github.com/getsyntegrity/ego/persistence"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
)

// mustSetUpStore fails the test when a fixture step that is not itself under
// test returned an error, naming the step so the failure says what broke.
func mustSetUpStore(t testing.TB, step string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", step, err)
	}
}

// disconnectAtEnd disconnects store once every case of the calling test has
// run, and fails the test if that disconnect returns an error.
func disconnectAtEnd(bg context.Context, t testing.TB, store interface{ Disconnect(context.Context) error }) {
	t.Helper()
	t.Cleanup(func() {
		if err := store.Disconnect(bg); err != nil {
			t.Errorf("disconnect store: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// EventStore tests
// ---------------------------------------------------------------------------

func TestEventStore_NewEventsStore(t *testing.T) {
	specs.Describe(t, "NewEventsStore", func(s *specs.Spec) {
		s.It("returns a store", func(ctx *specs.Context) {
			store := NewEventsStore()
			ctx.Expect(store).To(specs.Not(specs.BeNil()))
		})
	})
}

func TestEventStore_Connect(t *testing.T) {
	specs.Describe(t, "EventStore.Connect", func(s *specs.Spec) {
		bg := context.TODO()
		store := NewEventsStore()
		s.It("fresh connect", func(ctx *specs.Context) {
			err := store.Connect(bg)
			ctx.Expect(err).To(specs.BeNil())
		})
		s.It("already connected", func(ctx *specs.Context) {
			err := store.Connect(bg)
			ctx.Expect(err).To(specs.BeNil())
		})
	})
}

func TestEventStore_Disconnect(t *testing.T) {
	specs.Describe(t, "EventStore.Disconnect", func(s *specs.Spec) {
		bg := context.TODO()
		s.It("connected store", func(ctx *specs.Context) {
			store := NewEventsStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			err := store.Disconnect(bg)
			ctx.Expect(err).To(specs.BeNil())
		})
		s.It("already disconnected", func(ctx *specs.Context) {
			store := NewEventsStore()
			err := store.Disconnect(bg)
			ctx.Expect(err).To(specs.BeNil())
		})
	})
}

func TestEventStore_Ping(t *testing.T) {
	specs.Describe(t, "EventStore.Ping", func(s *specs.Spec) {
		bg := context.TODO()
		s.It("when connected", func(ctx *specs.Context) {
			store := NewEventsStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			err := store.Ping(bg)
			ctx.Expect(err).To(specs.BeNil())
		})
		s.It("when not connected auto-connects", func(ctx *specs.Context) {
			store := NewEventsStore()
			err := store.Ping(bg)
			ctx.Expect(err).To(specs.BeNil())
		})
	})
}

func TestEventStore_WriteAndReplayEvents(t *testing.T) {
	specs.Describe(t, "EventStore replays the events written for an entity", func(s *specs.Spec) {
		bg := context.TODO()
		store := NewEventsStore()
		mustSetUpStore(t, "connect store", store.Connect(bg))

		anyEvent, err := anypb.New(&testpb.AccountCreated{AccountId: "acc-1", AccountBalance: 100})
		mustSetUpStore(t, "build event payload", err)

		events := []*egopb.Event{
			{PersistenceId: "entity-1", SequenceNumber: 1, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
			{PersistenceId: "entity-1", SequenceNumber: 2, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
			{PersistenceId: "entity-1", SequenceNumber: 3, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
		}

		mustSetUpStore(t, "write events", store.WriteEvents(bg, persistence.Unscoped(), events, persistence.Unconditional()))
		disconnectAtEnd(bg, t, store)

		s.It("replay all events", func(ctx *specs.Context) {
			replayed, err := store.ReplayEvents(bg, persistence.Unscoped(), "entity-1", 1, 3, 10)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(len(replayed)).ToEqual(3)
		})

		s.It("replay with limit", func(ctx *specs.Context) {
			replayed, err := store.ReplayEvents(bg, persistence.Unscoped(), "entity-1", 1, 3, 2)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(len(replayed) <= 2).To(specs.BeTrue())
		})

		s.It("replay non-existent entity", func(ctx *specs.Context) {
			replayed, err := store.ReplayEvents(bg, persistence.Unscoped(), "non-existent", 1, 10, 100)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(len(replayed)).ToEqual(0)
		})
	})
}

func TestEventStore_GetLatestEvent(t *testing.T) {
	specs.Describe(t, "EventStore.GetLatestEvent", func(s *specs.Spec) {
		bg := context.TODO()
		store := NewEventsStore()
		mustSetUpStore(t, "connect store", store.Connect(bg))
		disconnectAtEnd(bg, t, store)

		s.It("no events returns nil", func(ctx *specs.Context) {
			event, err := store.GetLatestEvent(bg, persistence.Unscoped(), "non-existent")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(event).To(specs.BeNil())
		})

		s.It("returns latest by sequence number", func(ctx *specs.Context) {
			anyEvent, _ := anypb.New(&testpb.AccountCreated{AccountId: "acc-1", AccountBalance: 100})
			events := []*egopb.Event{
				{PersistenceId: "latest-test", SequenceNumber: 1, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
				{PersistenceId: "latest-test", SequenceNumber: 5, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
				{PersistenceId: "latest-test", SequenceNumber: 3, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
			}
			ctx.Expect(store.WriteEvents(bg, persistence.Unscoped(), events, persistence.Unconditional())).To(specs.BeNil())

			latest, err := store.GetLatestEvent(bg, persistence.Unscoped(), "latest-test")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(latest).To(specs.Not(specs.BeNil()))
			ctx.Expect(latest.GetSequenceNumber()).ToEqual(uint64(5))
		})
	})
}

func TestEventStore_DeleteEvents(t *testing.T) {
	specs.Describe(t, "EventStore.DeleteEvents", func(s *specs.Spec) {
		s.It("removes the events up to the given sequence number", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewEventsStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())

			anyEvent, _ := anypb.New(&testpb.AccountCreated{AccountId: "acc-1", AccountBalance: 100})
			events := []*egopb.Event{
				{PersistenceId: "del-test", SequenceNumber: 1, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
				{PersistenceId: "del-test", SequenceNumber: 2, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
				{PersistenceId: "del-test", SequenceNumber: 3, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
			}
			ctx.Expect(store.WriteEvents(bg, persistence.Unscoped(), events, persistence.Unconditional())).To(specs.BeNil())

			err := store.DeleteEvents(bg, persistence.Unscoped(), "del-test", 2)
			ctx.Expect(err).To(specs.BeNil())

			replayed, err := store.ReplayEvents(bg, persistence.Unscoped(), "del-test", 1, 3, 10)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(len(replayed)).ToEqual(1)
			ctx.Expect(replayed[0].GetSequenceNumber()).ToEqual(uint64(3))

			ctx.Expect(store.Disconnect(bg)).To(specs.BeNil())
		})
	})
}

func TestEventStore_PersistenceIDs(t *testing.T) {
	specs.Describe(t, "EventStore.PersistenceIDs", func(s *specs.Spec) {
		bg := context.TODO()
		store := NewEventsStore()
		mustSetUpStore(t, "connect store", store.Connect(bg))
		disconnectAtEnd(bg, t, store)

		s.It("empty store", func(ctx *specs.Context) {
			ids, nextToken, err := store.PersistenceIDs(bg, persistence.Unscoped(), 10, "")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(len(ids)).ToEqual(0)
			ctx.Expect(nextToken).ToEqual("")
		})

		s.It("with events and pagination", func(ctx *specs.Context) {
			anyEvent, _ := anypb.New(&testpb.AccountCreated{AccountId: "acc-1", AccountBalance: 100})
			events := []*egopb.Event{
				{PersistenceId: "pid-a", SequenceNumber: 1, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
				{PersistenceId: "pid-b", SequenceNumber: 1, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
				{PersistenceId: "pid-c", SequenceNumber: 1, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
			}
			ctx.Expect(store.WriteEvents(bg, persistence.Unscoped(), events, persistence.Unconditional())).To(specs.BeNil())

			ids, nextToken, err := store.PersistenceIDs(bg, persistence.Unscoped(), 2, "")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(len(ids)).ToEqual(2)
			ctx.Expect(nextToken).To(specs.Not(specs.Equal("")))

			ids2, nextToken2, err := store.PersistenceIDs(bg, persistence.Unscoped(), 10, nextToken)
			ctx.Expect(err).To(specs.BeNil())
			// the third id must still be returned by the second page, not skipped at the page boundary
			ctx.Expect(ids2).ToEqual([]string{"pid-c"})
			// no ids remain, so the token must signal iteration is complete
			ctx.Expect(nextToken2).ToEqual("")
		})
	})
}

// TestEventStore_PersistenceIDsPaginationExhaustive is a direct regression
// for the off-by-one at every page boundary: PersistenceIDs used to hand
// back keys[endIndex] (the first key NOT yet returned) as nextPageToken,
// while the following call resumed strictly AFTER that same token. That key
// was therefore never returned by any page. This writes enough persistence
// ids to force several pages at a small page size and asserts that
// iterating to exhaustion returns every id exactly once, matching the
// contract now stated on persistence.EventsStore.PersistenceIDs's doc
// comment. See also persistence/conformance/events.go's
// eventsPersistenceIDsPaginationCoversEveryIDExactlyOnce, which pins the
// same contract for every conforming store implementation.
func TestEventStore_PersistenceIDsPaginationExhaustive(t *testing.T) {
	specs.Describe(t, "EventStore.PersistenceIDs pagination", func(s *specs.Spec) {
		s.It("iterating to exhaustion returns every id exactly once", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewEventsStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			ctx.T.Cleanup(func() { _ = store.Disconnect(bg) })

			const pageSize = 3
			const total = 10 // forces at least four pages at pageSize
			anyEvent, err := anypb.New(&testpb.AccountCreated{AccountId: "acc-1", AccountBalance: 100})
			ctx.Expect(err).To(specs.BeNil())

			want := make([]string, 0, total)
			for i := 0; i < total; i++ {
				id := fmt.Sprintf("pagination-exhaustive-%02d", i)
				want = append(want, id)
				event := &egopb.Event{PersistenceId: id, SequenceNumber: 1, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1}
				ctx.Expect(store.WriteEvents(bg, persistence.Unscoped(), []*egopb.Event{event}, persistence.Unconditional())).To(specs.BeNil())
			}

			var got []string
			var pageToken string
			pages := 0
			for {
				ids, nextToken, err := store.PersistenceIDs(bg, persistence.Unscoped(), pageSize, pageToken)
				ctx.Expect(err).To(specs.BeNil())
				pages++
				// pagination must terminate
				ctx.Expect(pages <= total+1).To(specs.BeTrue())
				got = append(got, ids...)
				if nextToken == "" {
					break
				}
				pageToken = nextToken
			}

			// the setup must actually force multiple pages
			ctx.Expect(pages >= 4).To(specs.BeTrue())
			// every written id must be returned exactly once across pages, none skipped at a page boundary
			sort.Strings(want)
			sort.Strings(got)
			ctx.Expect(got).ToEqual(want)
		})
	})
}

func TestEventStore_GetShardEvents(t *testing.T) {
	specs.Describe(t, "EventStore.GetShardEvents", func(s *specs.Spec) {
		bg := context.TODO()
		store := NewEventsStore()
		mustSetUpStore(t, "connect store", store.Connect(bg))
		disconnectAtEnd(bg, t, store)

		s.It("no events for shard", func(ctx *specs.Context) {
			events, nextOffset, err := store.GetShardEvents(bg, 99, 0, 10)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(len(events)).ToEqual(0)
			ctx.Expect(nextOffset).ToEqual(int64(0))
		})

		s.It("with shard events", func(ctx *specs.Context) {
			anyEvent, _ := anypb.New(&testpb.AccountCreated{AccountId: "acc-1", AccountBalance: 100})
			ts := time.Now().UnixMilli()
			events := []*egopb.Event{
				{PersistenceId: "shard-test-1", SequenceNumber: 1, Event: anyEvent, Timestamp: ts, Shard: 5},
				{PersistenceId: "shard-test-2", SequenceNumber: 1, Event: anyEvent, Timestamp: ts + 1, Shard: 5},
				{PersistenceId: "shard-test-3", SequenceNumber: 1, Event: anyEvent, Timestamp: ts + 2, Shard: 6},
			}
			ctx.Expect(store.WriteEvents(bg, persistence.Unscoped(), events, persistence.Unconditional())).To(specs.BeNil())

			result, nextOffset, err := store.GetShardEvents(bg, 5, 0, 10)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(len(result)).To(specs.Not(specs.Equal(0)))
			ctx.Expect(nextOffset > int64(0)).To(specs.BeTrue())
		})
	})
}

func TestEventStore_ShardOffsets(t *testing.T) {
	specs.Describe(t, "EventStore.ShardOffsets", func(s *specs.Spec) {
		s.It("reports the timestamp of each shard's most recent event", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewEventsStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())

			anyEvent, _ := anypb.New(&testpb.AccountCreated{AccountId: "acc-1", AccountBalance: 100})
			events := []*egopb.Event{
				{PersistenceId: "sn-1", SequenceNumber: 1, Event: anyEvent, Timestamp: 100, Shard: 1},
				{PersistenceId: "sn-2", SequenceNumber: 1, Event: anyEvent, Timestamp: 300, Shard: 2},
				{PersistenceId: "sn-3", SequenceNumber: 1, Event: anyEvent, Timestamp: 200, Shard: 1},
			}
			ctx.Expect(store.WriteEvents(bg, persistence.Unscoped(), events, persistence.Unconditional())).To(specs.BeNil())

			offsets, err := store.ShardOffsets(bg)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(len(offsets)).ToEqual(2)
			// each shard reports the timestamp of its most recent event
			ctx.Expect(offsets[1]).ToEqual(int64(200))
			ctx.Expect(offsets[2]).ToEqual(int64(300))

			ctx.Expect(store.Disconnect(bg)).To(specs.BeNil())
		})
	})
}

// ---------------------------------------------------------------------------
// DurableStore tests
// ---------------------------------------------------------------------------

func TestDurableStore_NewDurableStore(t *testing.T) {
	specs.Describe(t, "NewDurableStore", func(s *specs.Spec) {
		s.It("returns a store", func(ctx *specs.Context) {
			store := NewDurableStore()
			ctx.Expect(store).To(specs.Not(specs.BeNil()))
		})
	})
}

func TestDurableStore_Connect(t *testing.T) {
	specs.Describe(t, "DurableStore.Connect", func(s *specs.Spec) {
		bg := context.TODO()
		store := NewDurableStore()
		s.It("fresh connect", func(ctx *specs.Context) {
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
		})
		s.It("already connected", func(ctx *specs.Context) {
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
		})
	})
}

func TestDurableStore_Disconnect(t *testing.T) {
	specs.Describe(t, "DurableStore.Disconnect", func(s *specs.Spec) {
		bg := context.TODO()
		s.It("connected store", func(ctx *specs.Context) {
			store := NewDurableStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			ctx.Expect(store.Disconnect(bg)).To(specs.BeNil())
		})
		s.It("already disconnected", func(ctx *specs.Context) {
			store := NewDurableStore()
			ctx.Expect(store.Disconnect(bg)).To(specs.BeNil())
		})
	})
}

func TestDurableStore_Ping(t *testing.T) {
	specs.Describe(t, "DurableStore.Ping", func(s *specs.Spec) {
		bg := context.TODO()
		s.It("when connected", func(ctx *specs.Context) {
			store := NewDurableStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			ctx.Expect(store.Ping(bg)).To(specs.BeNil())
		})
		s.It("when not connected auto-connects", func(ctx *specs.Context) {
			store := NewDurableStore()
			ctx.Expect(store.Ping(bg)).To(specs.BeNil())
		})
	})
}

func TestDurableStore_WriteAndGetState(t *testing.T) {
	specs.Describe(t, "DurableStore writes and reads durable state", func(s *specs.Spec) {
		bg := context.TODO()
		store := NewDurableStore()
		mustSetUpStore(t, "connect store", store.Connect(bg))
		disconnectAtEnd(bg, t, store)

		anyState, err := anypb.New(&testpb.Account{AccountId: "acc-1", AccountBalance: 500})
		mustSetUpStore(t, "build state payload", err)

		state := &egopb.DurableState{
			PersistenceId:  "ds-entity-1",
			ResultingState: anyState,
			VersionNumber:  1,
		}

		s.It("write and read state", func(ctx *specs.Context) {
			ctx.Expect(store.WriteState(bg, persistence.Unscoped(), state, persistence.Unconditional())).To(specs.BeNil())
			got, err := store.GetLatestState(bg, persistence.Unscoped(), "ds-entity-1")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.Not(specs.BeNil()))
			ctx.Expect(got.GetPersistenceId()).ToEqual(state.GetPersistenceId())
			ctx.Expect(got.GetVersionNumber()).ToEqual(uint64(1))
		})

		s.It("get non-existent state", func(ctx *specs.Context) {
			got, err := store.GetLatestState(bg, persistence.Unscoped(), "non-existent")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.BeNil())
		})
	})
}

func TestDurableStore_WriteState_NotConnected(t *testing.T) {
	specs.Describe(t, "DurableStore.WriteState on a store that is not connected", func(s *specs.Spec) {
		s.It("fails with a not-connected error", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewDurableStore()
			anyState, _ := anypb.New(&testpb.Account{AccountId: "acc-1", AccountBalance: 100})
			state := &egopb.DurableState{PersistenceId: "entity-1", ResultingState: anyState, VersionNumber: 1}
			err := store.WriteState(bg, persistence.Unscoped(), state, persistence.Unconditional())
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err.Error()).To(specs.Contain("not connected"))
		})
	})
}

func TestDurableStore_GetLatestState_NotConnected(t *testing.T) {
	specs.Describe(t, "DurableStore.GetLatestState on a store that is not connected", func(s *specs.Spec) {
		s.It("fails and returns no state", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewDurableStore()
			got, err := store.GetLatestState(bg, persistence.Unscoped(), "entity-1")
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(got).To(specs.BeNil())
		})
	})
}

// ---------------------------------------------------------------------------
// OffsetStore tests
// ---------------------------------------------------------------------------

func TestOffsetStore_NewOffsetStore(t *testing.T) {
	specs.Describe(t, "NewOffsetStore", func(s *specs.Spec) {
		s.It("returns a store", func(ctx *specs.Context) {
			store := NewOffsetStore()
			ctx.Expect(store).To(specs.Not(specs.BeNil()))
		})
	})
}

func TestOffsetStore_Connect(t *testing.T) {
	specs.Describe(t, "OffsetStore.Connect", func(s *specs.Spec) {
		bg := context.TODO()
		store := NewOffsetStore()
		s.It("fresh connect", func(ctx *specs.Context) {
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
		})
		s.It("already connected", func(ctx *specs.Context) {
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
		})
	})
}

func TestOffsetStore_Disconnect(t *testing.T) {
	specs.Describe(t, "OffsetStore.Disconnect", func(s *specs.Spec) {
		bg := context.TODO()
		s.It("connected store", func(ctx *specs.Context) {
			store := NewOffsetStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			ctx.Expect(store.Disconnect(bg)).To(specs.BeNil())
		})
		s.It("already disconnected", func(ctx *specs.Context) {
			store := NewOffsetStore()
			ctx.Expect(store.Disconnect(bg)).To(specs.BeNil())
		})
	})
}

func TestOffsetStore_Ping(t *testing.T) {
	specs.Describe(t, "OffsetStore.Ping", func(s *specs.Spec) {
		bg := context.TODO()
		s.It("when connected", func(ctx *specs.Context) {
			store := NewOffsetStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			ctx.Expect(store.Ping(bg)).To(specs.BeNil())
		})
		s.It("when not connected auto-connects", func(ctx *specs.Context) {
			store := NewOffsetStore()
			ctx.Expect(store.Ping(bg)).To(specs.BeNil())
		})
	})
}

func TestOffsetStore_WriteAndGetOffset(t *testing.T) {
	specs.Describe(t, "OffsetStore writes and reads projection offsets", func(s *specs.Spec) {
		bg := context.TODO()
		store := NewOffsetStore()
		mustSetUpStore(t, "connect store", store.Connect(bg))
		disconnectAtEnd(bg, t, store)

		offset := &egopb.Offset{
			ProjectionName: "proj-1",
			ShardNumber:    1,
			Value:          100,
			Timestamp:      time.Now().UnixMilli(),
		}

		s.It("write and read offset", func(ctx *specs.Context) {
			ctx.Expect(store.WriteOffset(bg, offset)).To(specs.BeNil())

			projID := &egopb.ProjectionId{
				ProjectionName: "proj-1",
				ShardNumber:    1,
			}
			got, err := store.GetCurrentOffset(bg, projID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.Not(specs.BeNil()))
			ctx.Expect(got.GetValue()).ToEqual(int64(100))
		})

		s.It("get non-existent offset", func(ctx *specs.Context) {
			projID := &egopb.ProjectionId{
				ProjectionName: "non-existent",
				ShardNumber:    99,
			}
			got, err := store.GetCurrentOffset(bg, projID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.BeNil())
		})
	})
}

// ---------------------------------------------------------------------------
// SnapshotStore tests
// ---------------------------------------------------------------------------

func TestSnapshotStore_NewSnapshotStore(t *testing.T) {
	specs.Describe(t, "NewSnapshotStore", func(s *specs.Spec) {
		s.It("returns a store", func(ctx *specs.Context) {
			store := NewSnapshotStore()
			ctx.Expect(store).To(specs.Not(specs.BeNil()))
		})
	})
}

func TestSnapshotStore_Connect(t *testing.T) {
	specs.Describe(t, "SnapshotStore.Connect", func(s *specs.Spec) {
		bg := context.TODO()
		store := NewSnapshotStore()
		s.It("fresh connect", func(ctx *specs.Context) {
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
		})
		s.It("already connected", func(ctx *specs.Context) {
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
		})
	})
}

func TestSnapshotStore_Disconnect(t *testing.T) {
	specs.Describe(t, "SnapshotStore.Disconnect", func(s *specs.Spec) {
		bg := context.TODO()
		s.It("connected store", func(ctx *specs.Context) {
			store := NewSnapshotStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			ctx.Expect(store.Disconnect(bg)).To(specs.BeNil())
		})
		s.It("already disconnected", func(ctx *specs.Context) {
			store := NewSnapshotStore()
			ctx.Expect(store.Disconnect(bg)).To(specs.BeNil())
		})
	})
}

func TestSnapshotStore_Ping(t *testing.T) {
	specs.Describe(t, "SnapshotStore.Ping", func(s *specs.Spec) {
		bg := context.TODO()
		s.It("when connected", func(ctx *specs.Context) {
			store := NewSnapshotStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			ctx.Expect(store.Ping(bg)).To(specs.BeNil())
		})
		s.It("when not connected auto-connects", func(ctx *specs.Context) {
			store := NewSnapshotStore()
			ctx.Expect(store.Ping(bg)).To(specs.BeNil())
		})
	})
}

func TestSnapshotStore_WriteAndGetSnapshot(t *testing.T) {
	specs.Describe(t, "SnapshotStore writes and reads snapshots", func(s *specs.Spec) {
		bg := context.TODO()
		store := NewSnapshotStore()
		mustSetUpStore(t, "connect store", store.Connect(bg))
		disconnectAtEnd(bg, t, store)

		anyState, err := anypb.New(&testpb.Account{AccountId: "acc-1", AccountBalance: 500})
		mustSetUpStore(t, "build snapshot payload", err)

		snapshots := []*egopb.Snapshot{
			{PersistenceId: "snap-entity-1", SequenceNumber: 1, State: anyState},
			{PersistenceId: "snap-entity-1", SequenceNumber: 5, State: anyState},
			{PersistenceId: "snap-entity-1", SequenceNumber: 3, State: anyState},
		}

		for i, snap := range snapshots {
			mustSetUpStore(t, fmt.Sprintf("write snapshot %d (sequence %d)", i, snap.GetSequenceNumber()), store.WriteSnapshot(bg, persistence.Unscoped(), snap))
		}

		s.It("returns latest snapshot", func(ctx *specs.Context) {
			got, err := store.GetLatestSnapshot(bg, persistence.Unscoped(), "snap-entity-1")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.Not(specs.BeNil()))
			ctx.Expect(got.GetSequenceNumber()).ToEqual(uint64(5))
		})

		s.It("no snapshot returns nil", func(ctx *specs.Context) {
			got, err := store.GetLatestSnapshot(bg, persistence.Unscoped(), "non-existent")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.BeNil())
		})
	})
}

func TestSnapshotStore_DeleteSnapshots(t *testing.T) {
	specs.Describe(t, "SnapshotStore.DeleteSnapshots", func(s *specs.Spec) {
		s.It("removes the snapshots up to the given sequence number", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewSnapshotStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())

			anyState, _ := anypb.New(&testpb.Account{AccountId: "acc-1", AccountBalance: 100})
			snapshots := []*egopb.Snapshot{
				{PersistenceId: "del-snap", SequenceNumber: 1, State: anyState},
				{PersistenceId: "del-snap", SequenceNumber: 2, State: anyState},
				{PersistenceId: "del-snap", SequenceNumber: 3, State: anyState},
			}
			for _, snap := range snapshots {
				ctx.Expect(store.WriteSnapshot(bg, persistence.Unscoped(), snap)).To(specs.BeNil())
			}

			ctx.Expect(store.DeleteSnapshots(bg, persistence.Unscoped(), "del-snap", 2)).To(specs.BeNil())

			got, err := store.GetLatestSnapshot(bg, persistence.Unscoped(), "del-snap")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.Not(specs.BeNil()))
			ctx.Expect(got.GetSequenceNumber()).ToEqual(uint64(3))

			ctx.Expect(store.Disconnect(bg)).To(specs.BeNil())
		})
	})
}

// ---------------------------------------------------------------------------
// KeyStore tests
// ---------------------------------------------------------------------------

func TestKeyStore_NewKeyStore(t *testing.T) {
	specs.Describe(t, "NewKeyStore", func(s *specs.Spec) {
		s.It("returns a store", func(ctx *specs.Context) {
			store := NewKeyStore()
			ctx.Expect(store).To(specs.Not(specs.BeNil()))
		})
	})
}

func TestKeyStore_GetOrCreateKey(t *testing.T) {
	specs.Describe(t, "KeyStore.GetOrCreateKey", func(s *specs.Spec) {
		bg := context.TODO()
		store := NewKeyStore()

		s.It("creates new key", func(ctx *specs.Context) {
			keyID, key, err := store.GetOrCreateKey(bg, "entity-1")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(keyID).To(specs.Not(specs.Equal("")))
			ctx.Expect(len(key)).ToEqual(32)
		})

		s.It("returns existing key", func(ctx *specs.Context) {
			keyID1, key1, err := store.GetOrCreateKey(bg, "entity-2")
			ctx.Expect(err).To(specs.BeNil())

			keyID2, key2, err := store.GetOrCreateKey(bg, "entity-2")
			ctx.Expect(err).To(specs.BeNil())

			ctx.Expect(keyID2).ToEqual(keyID1)
			ctx.Expect(key2).ToEqual(key1)
		})
	})
}

func TestKeyStore_GetKey(t *testing.T) {
	specs.Describe(t, "KeyStore.GetKey", func(s *specs.Spec) {
		bg := context.TODO()
		store := NewKeyStore()

		s.It("existing key", func(ctx *specs.Context) {
			keyID, expectedKey, err := store.GetOrCreateKey(bg, "entity-1")
			ctx.Expect(err).To(specs.BeNil())

			key, err := store.GetKey(bg, keyID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(key).ToEqual(expectedKey)
		})

		s.It("non-existent key", func(ctx *specs.Context) {
			_, err := store.GetKey(bg, "non-existent-key-id")
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.MatchError(encryption.ErrKeyNotFound))
		})
	})
}

func TestKeyStore_DeleteKey(t *testing.T) {
	specs.Describe(t, "KeyStore.DeleteKey", func(s *specs.Spec) {
		bg := context.TODO()
		store := NewKeyStore()

		s.It("delete existing key", func(ctx *specs.Context) {
			keyID, _, err := store.GetOrCreateKey(bg, "entity-to-delete")
			ctx.Expect(err).To(specs.BeNil())

			err = store.DeleteKey(bg, "entity-to-delete")
			ctx.Expect(err).To(specs.BeNil())

			_, err = store.GetKey(bg, keyID)
			ctx.Expect(err).To(specs.MatchError(encryption.ErrKeyNotFound))
		})

		s.It("delete non-existent key is no-op", func(ctx *specs.Context) {
			err := store.DeleteKey(bg, "non-existent")
			ctx.Expect(err).To(specs.BeNil())
		})
	})
}
