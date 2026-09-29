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

package projectionrunner

import (
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	kitlog "github.com/pablogore/kit-logger/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.uber.org/atomic"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/encryption"
	"github.com/getsyntegrity/ego/eventadapter"
	"github.com/getsyntegrity/ego/eventstream"
	"github.com/getsyntegrity/ego/internal/instrumentation"
	"github.com/getsyntegrity/ego/internal/pause"
	mockencryption "github.com/getsyntegrity/ego/mocks/encryption"
	mockadapter "github.com/getsyntegrity/ego/mocks/eventadapter"
	mocksoffsetstore "github.com/getsyntegrity/ego/mocks/offsetstore"
	mockseventstore "github.com/getsyntegrity/ego/mocks/persistence"
	"github.com/getsyntegrity/ego/persistence"
	"github.com/getsyntegrity/ego/projection"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
	testkit2 "github.com/getsyntegrity/ego/testkit"
)

// errFailed is the failure the stubbed collaborators return.
var errFailed = errors.New("failed")

const (
	// waitTimeout bounds every wait on an asynchronous effect of the runner.
	waitTimeout = 10 * time.Second
	// waitInterval is how often a waited-on condition is polled.
	waitInterval = 2 * time.Millisecond
)

// waitUntil polls cond every interval until it holds and fails the test with
// what when timeout passes first. It never waits without a bound.
func waitUntil(t testing.TB, timeout, interval time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(interval)
	}
}

// awaitStopped waits until the runner has stopped processing.
func awaitStopped(t testing.TB, runner *Runner) {
	t.Helper()
	waitUntil(t, waitTimeout, waitInterval, func() bool { return !runner.running.Load() }, "the runner to stop")
}

// awaitCalls waits until calls holds at least want.
func awaitCalls(t testing.TB, calls *atomic.Int32, want int32, what string) {
	t.Helper()
	waitUntil(t, waitTimeout, waitInterval, func() bool { return calls.Load() >= want }, what)
}

// awaitFailure returns the next error the runner reported to its host.
func awaitFailure(t testing.TB, failures <-chan error) error {
	t.Helper()
	select {
	case err := <-failures:
		return err
	case <-time.After(waitTimeout):
		t.Fatalf("timed out after %s waiting for the failure callback", waitTimeout)
	}
	return nil
}

// errText is err's message, or "" for nil, so a text expectation on a missing
// error fails on the expectation instead of panicking.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestProjectionRunnerErrorPaths(t *testing.T) {
	specs.Describe(t, "the runner stops when an event cannot be processed in processEnvelope", func(s *specs.Spec) {
		s.It("with decrypt failure in processEnvelope stops the runner", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()
			shardNumber := uint64(9)
			timestamp := timestamppb.Now()

			projectionID := &egopb.ProjectionId{
				ProjectionName: projectionName,
				ShardNumber:    shardNumber,
			}

			offset := &egopb.Offset{
				ShardNumber:    shardNumber,
				ProjectionName: projectionName,
				Value:          timestamp.AsTime().Unix(),
				Timestamp:      0,
			}

			// Build a valid anypb payload
			eventProto := &testpb.AccountCredited{}
			eventAny, err := anypb.New(eventProto)
			ctx.Expect(err).To(specs.BeNil())

			// Mark event as encrypted so the decrypt path is triggered
			encryptedBytes := []byte("cipher")
			events := []*egopb.Event{
				{
					PersistenceId:  persistenceID,
					SequenceNumber: 1,
					IsDeleted:      false,
					Event: &anypb.Any{
						TypeUrl: eventAny.GetTypeUrl(),
						Value:   encryptedBytes,
					},
					Timestamp:       timestamp.AsTime().Unix(),
					Shard:           shardNumber,
					IsEncrypted:     true,
					EncryptionKeyId: "key-1",
				},
			}

			nextOffset := timestamppb.New(time.Now().Add(time.Minute))
			maxBufferSize := 10
			resetOffsetTo := time.Now().UTC()

			encryptor := new(mockencryption.Encryptor)
			encryptor.EXPECT().Decrypt(mock.Anything, persistenceID, encryptedBytes, "key-1").Return(nil, errFailed)

			offsetStore := new(mocksoffsetstore.OffsetStore)
			offsetStore.EXPECT().Ping(mock.Anything).Return(nil)
			offsetStore.EXPECT().ResetOffset(mock.Anything, projectionName, resetOffsetTo.UnixMilli()).Return(nil)
			offsetStore.EXPECT().GetCurrentOffset(mock.Anything, projectionID).Return(offset, nil)

			eventsStore := new(mockseventstore.EventsStore)
			eventsStore.EXPECT().Ping(mock.Anything).Return(nil)
			eventsStore.EXPECT().ShardOffsets(mock.Anything).Return(map[uint64]int64{shardNumber: nextOffset.AsTime().UnixMilli()}, nil)
			eventsStore.EXPECT().GetShardEvents(mock.Anything, shardNumber, offset.GetValue(), uint64(maxBufferSize)).Return(events, nextOffset.AsTime().UnixMilli(), nil)

			handler := projection.NewDiscardHandler()
			runner := New(projectionName, handler, eventsStore, offsetStore,
				WithPullInterval(time.Millisecond),
				WithEncryptor(encryptor),
			)
			runner.resetOffsetTo = resetOffsetTo
			runner.maxBufferSize = maxBufferSize

			err = runner.Start(bg)
			ctx.Expect(err).To(specs.BeNil())

			runner.Run(bg, nil)

			awaitStopped(ctx.T, runner)

			encryptor.AssertExpectations(ctx.T)
			eventsStore.AssertExpectations(ctx.T)
			offsetStore.AssertExpectations(ctx.T)

			ctx.Expect(runner.running.Load()).To(specs.BeFalse())

			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with unmarshal failure after decrypt in processEnvelope stops the runner", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()
			shardNumber := uint64(9)
			timestamp := timestamppb.Now()

			projectionID := &egopb.ProjectionId{
				ProjectionName: projectionName,
				ShardNumber:    shardNumber,
			}

			offset := &egopb.Offset{
				ShardNumber:    shardNumber,
				ProjectionName: projectionName,
				Value:          timestamp.AsTime().Unix(),
				Timestamp:      0,
			}

			eventProto := &testpb.AccountCredited{}
			eventAny, err := anypb.New(eventProto)
			ctx.Expect(err).To(specs.BeNil())

			encryptedBytes := []byte("cipher")
			events := []*egopb.Event{
				{
					PersistenceId:  persistenceID,
					SequenceNumber: 1,
					IsDeleted:      false,
					Event: &anypb.Any{
						TypeUrl: eventAny.GetTypeUrl(),
						Value:   encryptedBytes,
					},
					Timestamp:       timestamp.AsTime().Unix(),
					Shard:           shardNumber,
					IsEncrypted:     true,
					EncryptionKeyId: "key-1",
				},
			}

			nextOffset := timestamppb.New(time.Now().Add(time.Minute))
			maxBufferSize := 10
			resetOffsetTo := time.Now().UTC()

			// Return invalid bytes that cannot be unmarshalled as a proto message
			invalidBytes := []byte("not-valid-proto")

			encryptor := new(mockencryption.Encryptor)
			encryptor.EXPECT().Decrypt(mock.Anything, persistenceID, encryptedBytes, "key-1").Return(invalidBytes, nil)

			offsetStore := new(mocksoffsetstore.OffsetStore)
			offsetStore.EXPECT().Ping(mock.Anything).Return(nil)
			offsetStore.EXPECT().ResetOffset(mock.Anything, projectionName, resetOffsetTo.UnixMilli()).Return(nil)
			offsetStore.EXPECT().GetCurrentOffset(mock.Anything, projectionID).Return(offset, nil)

			eventsStore := new(mockseventstore.EventsStore)
			eventsStore.EXPECT().Ping(mock.Anything).Return(nil)
			eventsStore.EXPECT().ShardOffsets(mock.Anything).Return(map[uint64]int64{shardNumber: nextOffset.AsTime().UnixMilli()}, nil)
			eventsStore.EXPECT().GetShardEvents(mock.Anything, shardNumber, offset.GetValue(), uint64(maxBufferSize)).Return(events, nextOffset.AsTime().UnixMilli(), nil)

			handler := projection.NewDiscardHandler()
			runner := New(projectionName, handler, eventsStore, offsetStore,
				WithPullInterval(time.Millisecond),
				WithEncryptor(encryptor),
			)
			runner.resetOffsetTo = resetOffsetTo
			runner.maxBufferSize = maxBufferSize

			err = runner.Start(bg)
			ctx.Expect(err).To(specs.BeNil())

			runner.Run(bg, nil)

			awaitStopped(ctx.T, runner)

			encryptor.AssertExpectations(ctx.T)
			eventsStore.AssertExpectations(ctx.T)
			offsetStore.AssertExpectations(ctx.T)

			ctx.Expect(runner.running.Load()).To(specs.BeFalse())

			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with event adapter chain failure in processEnvelope stops the runner", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()
			shardNumber := uint64(9)
			timestamp := timestamppb.Now()

			projectionID := &egopb.ProjectionId{
				ProjectionName: projectionName,
				ShardNumber:    shardNumber,
			}

			offset := &egopb.Offset{
				ShardNumber:    shardNumber,
				ProjectionName: projectionName,
				Value:          timestamp.AsTime().Unix(),
				Timestamp:      0,
			}

			eventProto := &testpb.AccountCredited{}
			eventAny, err := anypb.New(eventProto)
			ctx.Expect(err).To(specs.BeNil())

			events := []*egopb.Event{
				{
					PersistenceId:  persistenceID,
					SequenceNumber: 1,
					IsDeleted:      false,
					Event:          eventAny,
					Timestamp:      timestamp.AsTime().Unix(),
					Shard:          shardNumber,
				},
			}

			nextOffset := timestamppb.New(time.Now().Add(time.Minute))
			maxBufferSize := 10
			resetOffsetTo := time.Now().UTC()

			adapter := new(mockadapter.EventAdapter)
			adapter.EXPECT().Adapt(eventAny, uint64(1)).Return(nil, errFailed)

			offsetStore := new(mocksoffsetstore.OffsetStore)
			offsetStore.EXPECT().Ping(mock.Anything).Return(nil)
			offsetStore.EXPECT().ResetOffset(mock.Anything, projectionName, resetOffsetTo.UnixMilli()).Return(nil)
			offsetStore.EXPECT().GetCurrentOffset(mock.Anything, projectionID).Return(offset, nil)

			eventsStore := new(mockseventstore.EventsStore)
			eventsStore.EXPECT().Ping(mock.Anything).Return(nil)
			eventsStore.EXPECT().ShardOffsets(mock.Anything).Return(map[uint64]int64{shardNumber: nextOffset.AsTime().UnixMilli()}, nil)
			eventsStore.EXPECT().GetShardEvents(mock.Anything, shardNumber, offset.GetValue(), uint64(maxBufferSize)).Return(events, nextOffset.AsTime().UnixMilli(), nil)

			handler := projection.NewDiscardHandler()
			runner := New(projectionName, handler, eventsStore, offsetStore,
				WithPullInterval(time.Millisecond),
				WithEventAdapters([]eventadapter.EventAdapter{adapter}),
			)
			runner.resetOffsetTo = resetOffsetTo
			runner.maxBufferSize = maxBufferSize

			err = runner.Start(bg)
			ctx.Expect(err).To(specs.BeNil())

			runner.Run(bg, nil)

			awaitStopped(ctx.T, runner)

			adapter.AssertExpectations(ctx.T)
			eventsStore.AssertExpectations(ctx.T)
			offsetStore.AssertExpectations(ctx.T)

			ctx.Expect(runner.running.Load()).To(specs.BeFalse())

			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
	})
}

func TestStoreRetryDelay(t *testing.T) {
	specs.Describe(t, "storeRetryDelay backs off exponentially from the initial delay up to the cap", func(s *specs.Spec) {
		s.It("doubles per failure, then clamps to the cap", func(ctx *specs.Context) {
			ctx.Expect(storeRetryDelay(1)).ToEqual(storeRetryInitialDelay)
			ctx.Expect(storeRetryDelay(2)).ToEqual(2 * storeRetryInitialDelay)
			ctx.Expect(storeRetryDelay(5)).ToEqual(16 * storeRetryInitialDelay)
			// 1s << 5 = 32s exceeds the cap
			ctx.Expect(storeRetryDelay(6)).ToEqual(storeRetryMaxDelay)
			// large shift counts wrap or zero out and clamp to the cap
			ctx.Expect(storeRetryDelay(40)).ToEqual(storeRetryMaxDelay)
			ctx.Expect(storeRetryDelay(100)).ToEqual(storeRetryMaxDelay)
		})
	})
}

func TestProjectionRunnerFatalPaths(t *testing.T) {
	specs.Describe(t, "the runner retries failed store round trips in place and stops on unprocessable events", func(s *specs.Spec) {
		s.It("with store error the runner retries in place and stays running", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"

			offsetStore := new(mocksoffsetstore.OffsetStore)
			offsetStore.EXPECT().Ping(mock.Anything).Return(nil)

			// the first ShardOffsets round trip fails, subsequent ones succeed
			retried := atomic.NewInt32(0)
			eventsStore := new(mockseventstore.EventsStore)
			eventsStore.EXPECT().Ping(mock.Anything).Return(nil)
			eventsStore.EXPECT().ShardOffsets(mock.Anything).Return(nil, errFailed).Once()
			eventsStore.EXPECT().ShardOffsets(mock.Anything).Return(nil, nil).Run(func(_ context.Context) { retried.Inc() })

			handler := projection.NewDiscardHandler()
			runner := New(projectionName, handler, eventsStore, offsetStore,
				WithPullInterval(time.Millisecond))

			ctx.Expect(runner.Start(bg)).To(specs.BeNil())
			runner.Run(bg, nil)

			// the failed pull is retried once its first backoff has elapsed
			awaitCalls(ctx.T, retried, 1, "the failed pull to be retried")

			eventsStore.AssertExpectations(ctx.T)
			offsetStore.AssertExpectations(ctx.T)

			ctx.Expect(runner.running.Load()).To(specs.BeTrue())
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with unprocessable event the Runner stops", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()
			shardNumber := uint64(9)
			timestamp := timestamppb.Now()

			projectionID := &egopb.ProjectionId{
				ProjectionName: projectionName,
				ShardNumber:    shardNumber,
			}

			offset := &egopb.Offset{
				ShardNumber:    shardNumber,
				ProjectionName: projectionName,
				Value:          timestamp.AsTime().Unix(),
				Timestamp:      0,
			}

			eventAny, err := anypb.New(&testpb.AccountCredited{})
			ctx.Expect(err).To(specs.BeNil())

			events := []*egopb.Event{
				{
					PersistenceId:  persistenceID,
					SequenceNumber: 1,
					IsDeleted:      false,
					Event:          eventAny,
					Timestamp:      timestamp.AsTime().Unix(),
					Shard:          shardNumber,
				},
			}

			nextOffset := timestamppb.New(time.Now().Add(time.Minute))
			maxBufferSize := 10

			offsetStore := new(mocksoffsetstore.OffsetStore)
			offsetStore.EXPECT().Ping(mock.Anything).Return(nil)
			offsetStore.EXPECT().GetCurrentOffset(mock.Anything, projectionID).Return(offset, nil)

			eventsStore := new(mockseventstore.EventsStore)
			eventsStore.EXPECT().Ping(mock.Anything).Return(nil)
			eventsStore.EXPECT().ShardOffsets(mock.Anything).Return(map[uint64]int64{shardNumber: nextOffset.AsTime().UnixMilli()}, nil)
			eventsStore.EXPECT().GetShardEvents(mock.Anything, shardNumber, offset.GetValue(), uint64(maxBufferSize)).Return(events, nextOffset.AsTime().UnixMilli(), nil)

			// testHandler1 always fails and the default recovery policy is Fail
			runner := New(projectionName, testHandler1{}, eventsStore, offsetStore,
				WithPullInterval(time.Millisecond))
			runner.maxBufferSize = maxBufferSize

			failures := make(chan error, 2)
			ctx.Expect(runner.Start(bg)).To(specs.BeNil())
			runner.Run(bg, func(err error) { failures <- err })

			// the unprocessable event stops the processing loop permanently
			awaitStopped(ctx.T, runner)

			// the host is notified once, with the handler's own error: the
			// runner's internal classification never leaks to the host
			failure := awaitFailure(ctx.T, failures)
			ctx.Expect(errText(failure)).ToEqual("damn")
			var internal *eventError
			ctx.Expect(failure).To(specs.Not(specs.MatchErrorAs(&internal)))
			ctx.Expect(len(failures)).ToEqual(0)

			eventsStore.AssertExpectations(ctx.T)
			offsetStore.AssertExpectations(ctx.T)

			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with mixed store and event errors in one batch the Runner stops", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()
			storeShard := uint64(1)
			poisonShard := uint64(2)
			timestamp := timestamppb.Now()
			offsetValue := timestamp.AsTime().Unix()

			eventAny, err := anypb.New(&testpb.AccountCredited{})
			ctx.Expect(err).To(specs.BeNil())

			events := []*egopb.Event{
				{
					PersistenceId:  persistenceID,
					SequenceNumber: 1,
					IsDeleted:      false,
					Event:          eventAny,
					Timestamp:      offsetValue,
					Shard:          poisonShard,
				},
			}

			nextOffset := timestamppb.New(time.Now().Add(time.Minute)).AsTime().UnixMilli()
			maxBufferSize := 10

			offsetStore := new(mocksoffsetstore.OffsetStore)
			offsetStore.EXPECT().Ping(mock.Anything).Return(nil)
			offsetStore.EXPECT().GetCurrentOffset(mock.Anything, mock.AnythingOfType("*egopb.ProjectionId")).Return(&egopb.Offset{Value: offsetValue}, nil)

			// one shard fails its store round trip while the other returns an
			// event the handler cannot process
			eventsStore := new(mockseventstore.EventsStore)
			eventsStore.EXPECT().Ping(mock.Anything).Return(nil)
			eventsStore.EXPECT().ShardOffsets(mock.Anything).Return(map[uint64]int64{storeShard: nextOffset, poisonShard: nextOffset}, nil)
			eventsStore.EXPECT().GetShardEvents(mock.Anything, storeShard, offsetValue, uint64(maxBufferSize)).Return(nil, 0, errFailed)
			eventsStore.EXPECT().GetShardEvents(mock.Anything, poisonShard, offsetValue, uint64(maxBufferSize)).Return(events, nextOffset, nil)

			runner := New(projectionName, testHandler1{}, eventsStore, offsetStore,
				WithPullInterval(time.Millisecond))
			runner.maxBufferSize = maxBufferSize

			failures := make(chan error, 2)
			ctx.Expect(runner.Start(bg)).To(specs.BeNil())
			runner.Run(bg, func(err error) { failures <- err })

			// the unprocessable event outranks the store failure: the loop stops
			// instead of retrying, since retrying cannot advance past the event
			awaitStopped(ctx.T, runner)

			// the host is notified with the event error, never the store error
			failure := awaitFailure(ctx.T, failures)
			ctx.Expect(errText(failure)).ToEqual("damn")
			ctx.Expect(failure).To(specs.Not(specs.MatchError(errFailed)))

			eventsStore.AssertExpectations(ctx.T)
			offsetStore.AssertExpectations(ctx.T)

			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with persistent store error Stop interrupts the retry backoff", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"

			offsetStore := new(mocksoffsetstore.OffsetStore)
			offsetStore.EXPECT().Ping(mock.Anything).Return(nil)

			pulls := atomic.NewInt32(0)
			eventsStore := new(mockseventstore.EventsStore)
			eventsStore.EXPECT().Ping(mock.Anything).Return(nil)
			eventsStore.EXPECT().ShardOffsets(mock.Anything).Return(nil, errFailed).Run(func(_ context.Context) { pulls.Inc() })

			handler := projection.NewDiscardHandler()
			runner := New(projectionName, handler, eventsStore, offsetStore,
				WithPullInterval(time.Millisecond))

			ctx.Expect(runner.Start(bg)).To(specs.BeNil())
			runner.Run(bg, nil)

			// wait for the first failed pull, then stop
			awaitCalls(ctx.T, pulls, 1, "the first failed pull")
			ctx.Expect(runner.Stop()).To(specs.BeNil())

			ctx.Expect(runner.running.Load()).To(specs.BeFalse())
		})
	})
}

// offsetReader is the part of an offset store a test reads committed offsets from.
type offsetReader interface {
	GetCurrentOffset(ctx context.Context, projectionID *egopb.ProjectionId) (*egopb.Offset, error)
}

// awaitOffset waits until the committed offset of projectionID satisfies ok and
// returns it.
func awaitOffset(t testing.TB, store offsetReader, projectionID *egopb.ProjectionId, ok func(*egopb.Offset) bool, what string) *egopb.Offset {
	t.Helper()
	var got *egopb.Offset
	waitUntil(t, waitTimeout, waitInterval, func() bool {
		offset, err := store.GetCurrentOffset(context.TODO(), projectionID)
		if err != nil {
			t.Fatalf("reading the committed offset: %v", err)
		}
		got = offset
		return ok(offset)
	}, what)
	return got
}

// committed reports whether an offset has been committed at all.
func committed(offset *egopb.Offset) bool { return offset != nil }

// committedAt reports whether the committed offset holds value.
func committedAt(value int64) func(*egopb.Offset) bool {
	return func(offset *egopb.Offset) bool { return offset != nil && offset.GetValue() == value }
}

// pullCountingEventsStore counts the ShardOffsets round trips, one per pull pass.
type pullCountingEventsStore struct {
	*testkit2.EventStore
	pulls atomic.Int32
}

func (x *pullCountingEventsStore) ShardOffsets(ctx context.Context) (map[uint64]int64, error) {
	x.pulls.Inc()
	return x.EventStore.ShardOffsets(ctx)
}

func TestRunner(t *testing.T) {
	specs.Describe(t, "a Runner starts, projects persisted events under each recovery policy and stops", func(s *specs.Spec) {
		s.It("with happy path", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()
			shardNumber := uint64(9)
			logger := discardLogger

			// set up the event store
			eventsStore := testkit2.NewEventsStore()
			ctx.Expect(eventsStore).To(specs.Not(specs.BeNil()))
			ctx.Expect(eventsStore.Connect(bg)).To(specs.BeNil())

			// set up the offset store
			offsetStore := testkit2.NewOffsetStore()
			ctx.Expect(offsetStore).To(specs.Not(specs.BeNil()))
			ctx.Expect(offsetStore.Connect(bg)).To(specs.BeNil())

			// set up the projection
			// create a underlying that return successfully
			handler := projection.NewDiscardHandler()

			// create an instance of the projection
			runner := New(projectionName, handler, eventsStore, offsetStore, WithPullInterval(time.Millisecond), WithLogger(logger))
			// start the projection
			err := runner.Start(bg)
			ctx.Expect(err).To(specs.BeNil())

			ctx.Expect(runner.Name()).ToEqual(projectionName)

			// run the projection
			runner.Run(bg, nil)

			// persist some events
			event, err := anypb.New(&testpb.AccountCredited{})
			ctx.Expect(err).To(specs.BeNil())

			count := 10
			timestamp := timestamppb.Now()
			journals := make([]*egopb.Event, count)
			for i := 0; i < count; i++ {
				seqNr := i + 1
				journals[i] = &egopb.Event{
					PersistenceId:  persistenceID,
					SequenceNumber: uint64(seqNr),
					IsDeleted:      false,
					Event:          event,

					Timestamp: timestamp.AsTime().Unix(),
					Shard:     shardNumber,
				}
			}

			ctx.Expect(eventsStore.WriteEvents(bg, persistence.Unscoped(), journals, persistence.Unconditional())).To(specs.BeNil())
			ctx.Expect(runner.running.Load()).To(specs.BeTrue())

			// create the projection id
			projectionID := &egopb.ProjectionId{
				ProjectionName: projectionName,
				ShardNumber:    shardNumber,
			}

			// the projection is eventually consistent: wait for the offset of the last event
			actual := awaitOffset(ctx.T, offsetStore, projectionID, committedAt(journals[9].GetTimestamp()), "the offset of the last event to be committed")
			ctx.Expect(actual).To(specs.Not(specs.BeNil()))
			ctx.Expect(actual.GetValue()).ToEqual(journals[9].GetTimestamp())

			// free resources
			ctx.Expect(eventsStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(offsetStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with failed handler with fail strategy", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()

			// set up the event store
			journalStore := testkit2.NewEventsStore()
			ctx.Expect(journalStore).To(specs.Not(specs.BeNil()))
			ctx.Expect(journalStore.Connect(bg)).To(specs.BeNil())

			// set up the offset store
			offsetStore := testkit2.NewOffsetStore()
			ctx.Expect(offsetStore).To(specs.Not(specs.BeNil()))
			ctx.Expect(offsetStore.Disconnect(bg)).To(specs.BeNil())

			// set up the projection
			// create a underlying that return successfully
			handler := &testHandler1{}

			runner := New(projectionName, handler, journalStore, offsetStore, WithPullInterval(time.Millisecond))
			// start the projection
			err := runner.Start(bg)
			ctx.Expect(err).To(specs.BeNil())

			// run the projection
			runner.Run(bg, nil)

			// persist some events
			event, err := anypb.New(&testpb.AccountCredited{})
			ctx.Expect(err).To(specs.BeNil())

			count := 10
			timestamp := timestamppb.Now()
			journals := make([]*egopb.Event, count)
			for i := 0; i < count; i++ {
				seqNr := i + 1
				journals[i] = &egopb.Event{
					PersistenceId:  persistenceID,
					SequenceNumber: uint64(seqNr),
					IsDeleted:      false,
					Event:          event,

					Timestamp: timestamp.AsTime().Unix(),
				}
			}

			ctx.Expect(journalStore.WriteEvents(bg, persistence.Unscoped(), journals, persistence.Unconditional())).To(specs.BeNil())
			ctx.Expect(runner.running.Load()).To(specs.BeTrue())

			// here due to the default recovery strategy the projection is stopped
			awaitStopped(ctx.T, runner)
			ctx.Expect(runner.running.Load()).To(specs.BeFalse())
			// free resources
			ctx.Expect(journalStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(offsetStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with failed handler and retry_fail strategy", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()

			// set up the event store
			journalStore := testkit2.NewEventsStore()
			ctx.Expect(journalStore).To(specs.Not(specs.BeNil()))
			ctx.Expect(journalStore.Connect(bg)).To(specs.BeNil())

			// set up the offset store
			offsetStore := testkit2.NewOffsetStore()
			ctx.Expect(offsetStore).To(specs.Not(specs.BeNil()))
			ctx.Expect(offsetStore.Disconnect(bg)).To(specs.BeNil())

			// set up the projection
			// create a underlying that return successfully
			handler := &testHandler1{}

			runner := New(projectionName, handler, journalStore, offsetStore,
				WithPullInterval(time.Millisecond),
				WithRecoveryStrategy(projection.NewRecovery(
					projection.WithRecoveryPolicy(projection.RetryAndFail),
					projection.WithRetries(2),
					projection.WithRetryDelay(100*time.Millisecond))))

			// start the projection
			err := runner.Start(bg)
			ctx.Expect(err).To(specs.BeNil())
			// run the projection
			runner.Run(bg, nil)

			// persist some events
			event, err := anypb.New(&testpb.AccountCredited{})
			ctx.Expect(err).To(specs.BeNil())

			count := 10
			timestamp := timestamppb.Now()
			journals := make([]*egopb.Event, count)
			for i := range count {
				seqNr := i + 1
				journals[i] = &egopb.Event{
					PersistenceId:  persistenceID,
					SequenceNumber: uint64(seqNr),
					IsDeleted:      false,
					Event:          event,

					Timestamp: timestamp.AsTime().Unix(),
				}
			}

			ctx.Expect(journalStore.WriteEvents(bg, persistence.Unscoped(), journals, persistence.Unconditional())).To(specs.BeNil())
			ctx.Expect(runner.running.Load()).To(specs.BeTrue())

			// the projection stops once the retries are exhausted
			awaitStopped(ctx.T, runner)
			ctx.Expect(runner.running.Load()).To(specs.BeFalse())

			// free resources
			ctx.Expect(journalStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(offsetStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with failed handler and skip strategy", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()
			shard := uint64(8)

			// set up the event store
			journalStore := testkit2.NewEventsStore()
			ctx.Expect(journalStore).To(specs.Not(specs.BeNil()))
			ctx.Expect(journalStore.Connect(bg)).To(specs.BeNil())

			// set up the offset store
			offsetStore := testkit2.NewOffsetStore()
			ctx.Expect(offsetStore).To(specs.Not(specs.BeNil()))
			ctx.Expect(offsetStore.Connect(bg)).To(specs.BeNil())

			// set up the projection
			// create a underlying that return successfully
			handler := &testHandler2{counter: atomic.NewInt32(0)}

			runner := New(projectionName, handler, journalStore, offsetStore,
				WithPullInterval(time.Millisecond),
				WithRecoveryStrategy(projection.NewRecovery(
					projection.WithRecoveryPolicy(projection.Skip),
					projection.WithRetries(2),
					projection.WithRetryDelay(100*time.Millisecond))))
			// start the projection
			err := runner.Start(bg)
			ctx.Expect(err).To(specs.BeNil())
			// run the projection
			runner.Run(bg, nil)
			// persist some events
			event, err := anypb.New(&testpb.AccountCredited{})
			ctx.Expect(err).To(specs.BeNil())

			count := 10
			timestamp := timestamppb.Now()
			journals := make([]*egopb.Event, count)
			for i := range count {
				seqNr := i + 1
				journals[i] = &egopb.Event{
					PersistenceId:  persistenceID,
					SequenceNumber: uint64(seqNr),
					IsDeleted:      false,
					Event:          event,

					Timestamp: timestamp.AsTime().Unix(),
					Shard:     shard,
				}
			}

			ctx.Expect(journalStore.WriteEvents(bg, persistence.Unscoped(), journals, persistence.Unconditional())).To(specs.BeNil())
			ctx.Expect(runner.running.Load()).To(specs.BeTrue())

			projectionID := &egopb.ProjectionId{
				ProjectionName: projectionName,
				ShardNumber:    shard,
			}

			// the batch offset is committed once every event was handled or skipped
			actual := awaitOffset(ctx.T, offsetStore, projectionID, committedAt(timestamp.AsTime().Unix()), "the batch offset to be committed")
			ctx.Expect(actual).To(specs.Not(specs.BeNil()))
			ctx.Expect(handler.counter.Load()).ToEqual(int32(5))

			// free resource
			ctx.Expect(journalStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(offsetStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with failed handler and skip retry strategy", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()
			shard := uint64(7)

			// set up the event store
			journalStore := testkit2.NewEventsStore()
			ctx.Expect(journalStore).To(specs.Not(specs.BeNil()))
			ctx.Expect(journalStore.Connect(bg)).To(specs.BeNil())

			// set up the offset store
			offsetStore := testkit2.NewOffsetStore()
			ctx.Expect(offsetStore).To(specs.Not(specs.BeNil()))
			ctx.Expect(offsetStore.Connect(bg)).To(specs.BeNil())

			// set up the projection
			// create a underlying that return successfully
			handler := &testHandler2{counter: atomic.NewInt32(0)}

			runner := New(projectionName, handler, journalStore, offsetStore,
				WithPullInterval(time.Millisecond),
				WithRecoveryStrategy(projection.NewRecovery(
					projection.WithRecoveryPolicy(projection.RetryAndSkip),
					projection.WithRetries(2),
					projection.WithRetryDelay(100*time.Millisecond))))
			// start the projection
			err := runner.Start(bg)
			ctx.Expect(err).To(specs.BeNil())
			// run the projection
			runner.Run(bg, nil)
			// persist some events
			event, err := anypb.New(&testpb.AccountCredited{})
			ctx.Expect(err).To(specs.BeNil())

			count := 10
			timestamp := timestamppb.Now()
			journals := make([]*egopb.Event, count)
			for i := range count {
				seqNr := i + 1
				journals[i] = &egopb.Event{
					PersistenceId:  persistenceID,
					SequenceNumber: uint64(seqNr),
					IsDeleted:      false,
					Event:          event,

					Timestamp: timestamp.AsTime().Unix(),
					Shard:     shard,
				}
			}

			ctx.Expect(journalStore.WriteEvents(bg, persistence.Unscoped(), journals, persistence.Unconditional())).To(specs.BeNil())
			ctx.Expect(runner.running.Load()).To(specs.BeTrue())

			projectionID := &egopb.ProjectionId{
				ProjectionName: projectionName,
				ShardNumber:    shard,
			}

			// the batch offset is committed once every event was handled or skipped
			actual := awaitOffset(ctx.T, offsetStore, projectionID, committedAt(timestamp.AsTime().Unix()), "the batch offset to be committed")
			ctx.Expect(actual).To(specs.Not(specs.BeNil()))
			ctx.Expect(handler.counter.Load()).ToEqual(int32(5))

			// free resource
			ctx.Expect(journalStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(offsetStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with handler panic and fail strategy", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()
			shardNumber := uint64(9)
			timestamp := timestamppb.Now()
			handler := &testPanicHandler{}

			// create the projection id
			projectionID := &egopb.ProjectionId{
				ProjectionName: projectionName,
				ShardNumber:    shardNumber,
			}

			offset := &egopb.Offset{
				ShardNumber:    shardNumber,
				ProjectionName: projectionName,
				Value:          timestamp.AsTime().Unix(),
				Timestamp:      0,
			}

			event, err := anypb.New(&testpb.AccountCredited{})
			ctx.Expect(err).To(specs.BeNil())
			nextOffsetValue := timestamppb.New(time.Now().Add(time.Minute))
			events := []*egopb.Event{
				{
					PersistenceId:  persistenceID,
					SequenceNumber: 1,
					IsDeleted:      false,
					Event:          event,

					Timestamp: timestamp.AsTime().Unix(),
					Shard:     shardNumber,
				},
			}

			maxBufferSize := 10

			offsetStore := new(mocksoffsetstore.OffsetStore)
			offsetStore.EXPECT().Ping(mock.Anything).Return(nil)
			offsetStore.EXPECT().GetCurrentOffset(mock.Anything, projectionID).Return(offset, nil)

			eventsStore := new(mockseventstore.EventsStore)
			eventsStore.EXPECT().Ping(mock.Anything).Return(nil)
			eventsStore.EXPECT().ShardOffsets(mock.Anything).Return(map[uint64]int64{shardNumber: nextOffsetValue.AsTime().UnixMilli()}, nil)
			eventsStore.EXPECT().GetShardEvents(mock.Anything, shardNumber, offset.GetValue(), uint64(maxBufferSize)).
				Return(events, nextOffsetValue.AsTime().UnixMilli(), nil)

			// create an instance of the projection
			runner := New(projectionName, handler, eventsStore, offsetStore, WithPullInterval(time.Millisecond))
			runner.maxBufferSize = maxBufferSize

			// start the projection
			err = runner.Start(bg)
			ctx.Expect(err).To(specs.BeNil())

			// run the projection
			runner.Run(bg, nil)

			awaitStopped(ctx.T, runner)

			eventsStore.AssertExpectations(ctx.T)
			offsetStore.AssertExpectations(ctx.T)
			offsetStore.AssertNotCalled(ctx.T, "WriteOffset", mock.Anything, mock.AnythingOfType("*egopb.Offset"))

			ctx.Expect(runner.running.Load()).To(specs.BeFalse())

			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with events store is not defined", func(ctx *specs.Context) {
			bg := context.Background()
			handler := projection.NewDiscardHandler()
			projectionName := "db-writer"
			// set up the offset store
			offsetStore := testkit2.NewOffsetStore()
			ctx.Expect(offsetStore).To(specs.Not(specs.BeNil()))
			ctx.Expect(offsetStore.Connect(bg)).To(specs.BeNil())

			// create an instance of the projection
			runner := New(projectionName, handler, nil, offsetStore, WithPullInterval(time.Millisecond))
			// start the projection
			err := runner.Start(bg)
			ctx.Expect(errText(err)).ToEqual("events store is not defined")
			ctx.Expect(offsetStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with offset store is not defined", func(ctx *specs.Context) {
			bg := context.Background()
			handler := projection.NewDiscardHandler()
			projectionName := "db-writer"
			// set up the event store
			eventsStore := testkit2.NewEventsStore()
			ctx.Expect(eventsStore).To(specs.Not(specs.BeNil()))
			ctx.Expect(eventsStore.Connect(bg)).To(specs.BeNil())

			// create an instance of the projection
			runner := New(projectionName, handler, eventsStore, nil, WithPullInterval(time.Millisecond))
			// start the projection
			err := runner.Start(bg)
			ctx.Expect(errText(err)).ToEqual("offsets store is not defined")
			ctx.Expect(eventsStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with start when already started returns nil", func(ctx *specs.Context) {
			bg := context.Background()
			handler := projection.NewDiscardHandler()
			projectionName := "db-writer"
			// set up the event store
			eventsStore := testkit2.NewEventsStore()
			ctx.Expect(eventsStore).To(specs.Not(specs.BeNil()))
			ctx.Expect(eventsStore.Connect(bg)).To(specs.BeNil())

			// set up the offset store
			offsetStore := testkit2.NewOffsetStore()
			ctx.Expect(offsetStore).To(specs.Not(specs.BeNil()))
			ctx.Expect(offsetStore.Connect(bg)).To(specs.BeNil())

			// create an instance of the projection
			runner := New(projectionName, handler, eventsStore, offsetStore, WithPullInterval(time.Millisecond))
			// start the projection
			err := runner.Start(bg)
			ctx.Expect(err).To(specs.BeNil())

			ctx.Expect(runner.Start(bg)).To(specs.BeNil())

			// free resources
			ctx.Expect(eventsStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(offsetStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with start when max retry to ping events store fails", func(ctx *specs.Context) {
			bg := context.Background()
			handler := projection.NewDiscardHandler()
			projectionName := "db-writer"

			// set up the offset store
			offsetStore := testkit2.NewOffsetStore()
			ctx.Expect(offsetStore).To(specs.Not(specs.BeNil()))
			ctx.Expect(offsetStore.Connect(bg)).To(specs.BeNil())

			eventsStore := new(mockseventstore.EventsStore)
			eventsStore.EXPECT().Ping(mock.Anything).Return(errors.New("fail ping"))

			// create an instance of the projection
			runner := New(projectionName, handler, eventsStore, offsetStore, WithPullInterval(time.Millisecond))
			// start the projection
			err := runner.Start(bg)
			ctx.Expect(errText(err)).ToEqual("failed to start the projection: fail ping")
			eventsStore.AssertExpectations(ctx.T)
			ctx.Expect(offsetStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with start when max retry to ping offsets store store fails", func(ctx *specs.Context) {
			bg := context.Background()
			handler := projection.NewDiscardHandler()
			projectionName := "db-writer"

			// set up the event store
			eventsStore := testkit2.NewEventsStore()
			ctx.Expect(eventsStore).To(specs.Not(specs.BeNil()))
			ctx.Expect(eventsStore.Connect(bg)).To(specs.BeNil())

			offsetStore := new(mocksoffsetstore.OffsetStore)
			offsetStore.EXPECT().Ping(mock.Anything).Return(errors.New("fail ping"))

			// create an instance of the projection
			runner := New(projectionName, handler, eventsStore, offsetStore, WithPullInterval(time.Millisecond))
			// start the projection
			err := runner.Start(bg)
			ctx.Expect(errText(err)).ToEqual("failed to start the projection: fail ping")
			offsetStore.AssertExpectations(ctx.T)
			ctx.Expect(eventsStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with start when ResetOffset fails", func(ctx *specs.Context) {
			bg := context.Background()
			handler := projection.NewDiscardHandler()
			projectionName := "db-writer"

			eventsStore := new(mockseventstore.EventsStore)
			eventsStore.EXPECT().Ping(mock.Anything).Return(nil)

			resetOffsetTo := time.Now().UTC()
			offsetStore := new(mocksoffsetstore.OffsetStore)
			offsetStore.EXPECT().Ping(mock.Anything).Return(nil)
			offsetStore.EXPECT().ResetOffset(bg, projectionName, resetOffsetTo.UnixMilli()).Return(errors.New("fail to reset offset"))

			// create an instance of the projection
			runner := New(projectionName, handler, eventsStore, offsetStore, WithPullInterval(time.Millisecond))
			// purposefully for test
			runner.resetOffsetTo = resetOffsetTo

			// start the projection
			err := runner.Start(bg)
			ctx.Expect(errText(err)).ToEqual("failed to reset projection=db-writer: fail to reset offset")
			offsetStore.AssertExpectations(ctx.T)
			eventsStore.AssertExpectations(ctx.T)
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("when fail to write the offset the Runner retries and keeps running", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()
			shardNumber := uint64(9)
			timestamp := timestamppb.Now()
			handler := projection.NewDiscardHandler()

			// create the projection id
			projectionID := &egopb.ProjectionId{
				ProjectionName: projectionName,
				ShardNumber:    shardNumber,
			}

			offset := &egopb.Offset{
				ShardNumber:    shardNumber,
				ProjectionName: projectionName,
				Value:          timestamp.AsTime().Unix(),
				Timestamp:      0,
			}

			event, err := anypb.New(&testpb.AccountCredited{})
			ctx.Expect(err).To(specs.BeNil())
			nextOffsetValue := timestamppb.New(time.Now().Add(time.Minute))
			events := []*egopb.Event{
				{
					PersistenceId:  persistenceID,
					SequenceNumber: 1,
					IsDeleted:      false,
					Event:          event,

					Timestamp: timestamp.AsTime().Unix(),
					Shard:     shardNumber,
				},
			}

			maxBufferSize := 10
			resetOffsetTo := time.Now().UTC()

			writes := atomic.NewInt32(0)
			offsetStore := new(mocksoffsetstore.OffsetStore)
			offsetStore.EXPECT().Ping(mock.Anything).Return(nil)
			offsetStore.EXPECT().ResetOffset(mock.Anything, projectionName, resetOffsetTo.UnixMilli()).Return(nil)
			offsetStore.EXPECT().GetCurrentOffset(mock.Anything, projectionID).Return(offset, nil)
			offsetStore.EXPECT().WriteOffset(mock.Anything, mock.AnythingOfType("*egopb.Offset")).Return(errFailed).
				Run(func(_ context.Context, _ *egopb.Offset) { writes.Inc() })

			eventsStore := new(mockseventstore.EventsStore)
			eventsStore.EXPECT().Ping(mock.Anything).Return(nil)
			eventsStore.EXPECT().ShardOffsets(mock.Anything).Return(map[uint64]int64{shardNumber: nextOffsetValue.AsTime().UnixMilli()}, nil)
			eventsStore.EXPECT().GetShardEvents(mock.Anything, shardNumber, offset.GetValue(), uint64(maxBufferSize)).Return(events, nextOffsetValue.AsTime().UnixMilli(), nil)

			// create an instance of the projection
			runner := New(projectionName, handler, eventsStore, offsetStore, WithPullInterval(time.Millisecond))
			runner.resetOffsetTo = resetOffsetTo
			runner.maxBufferSize = maxBufferSize

			// start the projection
			err = runner.Start(bg)
			ctx.Expect(err).To(specs.BeNil())

			// run the projection
			runner.Run(bg, nil)

			awaitCalls(ctx.T, writes, 1, "the failing offset write")

			eventsStore.AssertExpectations(ctx.T)
			offsetStore.AssertExpectations(ctx.T)

			ctx.Expect(runner.running.Load()).To(specs.BeTrue())

			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("when fail to fetch shard numbers the Runner retries and keeps running", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			handler := projection.NewDiscardHandler()

			maxBufferSize := 10
			resetOffsetTo := time.Now().UTC()

			offsetStore := new(mocksoffsetstore.OffsetStore)
			offsetStore.EXPECT().Ping(mock.Anything).Return(nil)
			offsetStore.EXPECT().ResetOffset(mock.Anything, projectionName, resetOffsetTo.UnixMilli()).Return(nil)

			pulls := atomic.NewInt32(0)
			eventsStore := new(mockseventstore.EventsStore)
			eventsStore.EXPECT().Ping(mock.Anything).Return(nil)
			eventsStore.EXPECT().ShardOffsets(mock.Anything).Return(nil, errFailed).Run(func(_ context.Context) { pulls.Inc() })

			// create an instance of the projection
			runner := New(projectionName, handler, eventsStore, offsetStore, WithPullInterval(time.Millisecond))
			runner.resetOffsetTo = resetOffsetTo
			runner.maxBufferSize = maxBufferSize

			// start the projection
			err := runner.Start(bg)
			ctx.Expect(err).To(specs.BeNil())

			// run the projection
			runner.Run(bg, nil)

			awaitCalls(ctx.T, pulls, 1, "the failing shard offsets fetch")

			eventsStore.AssertExpectations(ctx.T)
			offsetStore.AssertExpectations(ctx.T)

			ctx.Expect(runner.running.Load()).To(specs.BeTrue())

			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("when fail to get current offset the Runner retries and keeps running", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			shardNumber := uint64(9)

			handler := projection.NewDiscardHandler()

			// create the projection id
			projectionID := &egopb.ProjectionId{
				ProjectionName: projectionName,
				ShardNumber:    shardNumber,
			}

			maxBufferSize := 10
			resetOffsetTo := time.Now().UTC()

			reads := atomic.NewInt32(0)
			offsetStore := new(mocksoffsetstore.OffsetStore)
			offsetStore.EXPECT().Ping(mock.Anything).Return(nil)
			offsetStore.EXPECT().ResetOffset(mock.Anything, projectionName, resetOffsetTo.UnixMilli()).Return(nil)
			offsetStore.EXPECT().GetCurrentOffset(mock.Anything, projectionID).Return(nil, errFailed).
				Run(func(_ context.Context, _ *egopb.ProjectionId) { reads.Inc() })

			eventsStore := new(mockseventstore.EventsStore)
			eventsStore.EXPECT().Ping(mock.Anything).Return(nil)
			eventsStore.EXPECT().ShardOffsets(mock.Anything).Return(map[uint64]int64{shardNumber: time.Now().UnixMilli()}, nil)

			// create an instance of the projection
			runner := New(projectionName, handler, eventsStore, offsetStore, WithPullInterval(time.Millisecond))
			runner.resetOffsetTo = resetOffsetTo
			runner.maxBufferSize = maxBufferSize

			// start the projection
			err := runner.Start(bg)
			ctx.Expect(err).To(specs.BeNil())

			// run the projection
			runner.Run(bg, nil)

			awaitCalls(ctx.T, reads, 1, "the failing current offset read")

			eventsStore.AssertExpectations(ctx.T)
			offsetStore.AssertExpectations(ctx.T)

			ctx.Expect(runner.running.Load()).To(specs.BeTrue())

			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("when fail to get shard events the Runner retries and keeps running", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"

			shardNumber := uint64(9)
			timestamp := timestamppb.Now()
			handler := projection.NewDiscardHandler()

			// create the projection id
			projectionID := &egopb.ProjectionId{
				ProjectionName: projectionName,
				ShardNumber:    shardNumber,
			}

			offset := &egopb.Offset{
				ShardNumber:    shardNumber,
				ProjectionName: projectionName,
				Value:          timestamp.AsTime().Unix(),
				Timestamp:      0,
			}

			maxBufferSize := 10
			resetOffsetTo := time.Now().UTC()

			offsetStore := new(mocksoffsetstore.OffsetStore)
			offsetStore.EXPECT().Ping(mock.Anything).Return(nil)
			offsetStore.EXPECT().ResetOffset(mock.Anything, projectionName, resetOffsetTo.UnixMilli()).Return(nil)
			offsetStore.EXPECT().GetCurrentOffset(mock.Anything, projectionID).Return(offset, nil)

			fetches := atomic.NewInt32(0)
			eventsStore := new(mockseventstore.EventsStore)
			eventsStore.EXPECT().Ping(mock.Anything).Return(nil)
			eventsStore.EXPECT().ShardOffsets(mock.Anything).Return(map[uint64]int64{shardNumber: time.Now().UnixMilli()}, nil)
			eventsStore.EXPECT().GetShardEvents(mock.Anything, shardNumber, offset.GetValue(), uint64(maxBufferSize)).Return(nil, 0, errFailed).
				Run(func(_ context.Context, _ uint64, _ int64, _ uint64) { fetches.Inc() })

			// create an instance of the projection
			runner := New(projectionName, handler, eventsStore, offsetStore, WithPullInterval(time.Millisecond))
			runner.resetOffsetTo = resetOffsetTo
			runner.maxBufferSize = maxBufferSize

			// start the projection
			err := runner.Start(bg)
			ctx.Expect(err).To(specs.BeNil())

			// run the projection
			runner.Run(bg, nil)

			awaitCalls(ctx.T, fetches, 1, "the failing shard events fetch")

			eventsStore.AssertExpectations(ctx.T)
			offsetStore.AssertExpectations(ctx.T)

			ctx.Expect(runner.running.Load()).To(specs.BeTrue())

			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with encrypted events decrypted during processing", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()
			shardNumber := uint64(9)

			journalStore := testkit2.NewEventsStore()
			ctx.Expect(journalStore.Connect(bg)).To(specs.BeNil())
			offsetStore := testkit2.NewOffsetStore()
			ctx.Expect(offsetStore.Connect(bg)).To(specs.BeNil())

			keyStore := testkit2.NewKeyStore()
			encryptor := encryption.NewAESEncryptor(keyStore)

			handler := projection.NewDiscardHandler()

			// write an encrypted event
			eventAny, err := anypb.New(&testpb.AccountCredited{AccountId: persistenceID, AccountBalance: 100})
			ctx.Expect(err).To(specs.BeNil())

			eventBytes, err := proto.Marshal(eventAny)
			ctx.Expect(err).To(specs.BeNil())

			ciphertext, keyID, err := encryptor.Encrypt(bg, persistenceID, eventBytes)
			ctx.Expect(err).To(specs.BeNil())

			encryptedEvent := &anypb.Any{TypeUrl: eventAny.GetTypeUrl(), Value: ciphertext}
			timestamp := time.Now().Unix()

			journals := []*egopb.Event{
				{
					PersistenceId:   persistenceID,
					SequenceNumber:  1,
					IsDeleted:       false,
					Event:           encryptedEvent,
					Timestamp:       timestamp,
					Shard:           shardNumber,
					IsEncrypted:     true,
					EncryptionKeyId: keyID,
				},
			}
			ctx.Expect(journalStore.WriteEvents(bg, persistence.Unscoped(), journals, persistence.Unconditional())).To(specs.BeNil())

			runner := New(projectionName, handler, journalStore, offsetStore,
				WithPullInterval(time.Millisecond),
				WithEncryptor(encryptor))

			ctx.Expect(runner.Start(bg)).To(specs.BeNil())
			runner.Run(bg, nil)

			projectionID := &egopb.ProjectionId{
				ProjectionName: projectionName,
				ShardNumber:    shardNumber,
			}
			actual := awaitOffset(ctx.T, offsetStore, projectionID, committed, "the offset of the decrypted event to be committed")
			ctx.Expect(actual).To(specs.Not(specs.BeNil()))

			ctx.Expect(journalStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(offsetStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with event adapters during processing", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()
			shardNumber := uint64(9)

			journalStore := testkit2.NewEventsStore()
			ctx.Expect(journalStore.Connect(bg)).To(specs.BeNil())
			offsetStore := testkit2.NewOffsetStore()
			ctx.Expect(offsetStore.Connect(bg)).To(specs.BeNil())

			handler := projection.NewDiscardHandler()
			adapter := &noopRunnerAdapter{}

			event, err := anypb.New(&testpb.AccountCredited{AccountId: persistenceID, AccountBalance: 100})
			ctx.Expect(err).To(specs.BeNil())
			timestamp := time.Now().Unix()

			journals := []*egopb.Event{
				{
					PersistenceId:  persistenceID,
					SequenceNumber: 1,
					IsDeleted:      false,
					Event:          event,
					Timestamp:      timestamp,
					Shard:          shardNumber,
				},
			}
			ctx.Expect(journalStore.WriteEvents(bg, persistence.Unscoped(), journals, persistence.Unconditional())).To(specs.BeNil())

			runner := New(projectionName, handler, journalStore, offsetStore,
				WithPullInterval(time.Millisecond),
				WithEventAdapters([]eventadapter.EventAdapter{adapter}))

			ctx.Expect(runner.Start(bg)).To(specs.BeNil())
			runner.Run(bg, nil)

			projectionID := &egopb.ProjectionId{
				ProjectionName: projectionName,
				ShardNumber:    shardNumber,
			}
			actual := awaitOffset(ctx.T, offsetStore, projectionID, committed, "the offset of the adapted event to be committed")
			ctx.Expect(actual).To(specs.Not(specs.BeNil()))

			ctx.Expect(journalStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(offsetStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with metrics during processing", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()
			shardNumber := uint64(9)

			journalStore := testkit2.NewEventsStore()
			ctx.Expect(journalStore.Connect(bg)).To(specs.BeNil())
			offsetStore := testkit2.NewOffsetStore()
			ctx.Expect(offsetStore.Connect(bg)).To(specs.BeNil())

			handler := projection.NewDiscardHandler()

			meter := noopmetric.NewMeterProvider().Meter("test")
			m := instrumentation.New(meter)

			event, err := anypb.New(&testpb.AccountCredited{AccountId: persistenceID, AccountBalance: 100})
			ctx.Expect(err).To(specs.BeNil())
			timestamp := time.Now().Unix()

			journals := []*egopb.Event{
				{
					PersistenceId:  persistenceID,
					SequenceNumber: 1,
					IsDeleted:      false,
					Event:          event,
					Timestamp:      timestamp,
					Shard:          shardNumber,
				},
			}
			ctx.Expect(journalStore.WriteEvents(bg, persistence.Unscoped(), journals, persistence.Unconditional())).To(specs.BeNil())

			runner := New(projectionName, handler, journalStore, offsetStore,
				WithPullInterval(time.Millisecond),
				WithMetrics(m))

			ctx.Expect(runner.Start(bg)).To(specs.BeNil())
			runner.Run(bg, nil)

			projectionID := &egopb.ProjectionId{
				ProjectionName: projectionName,
				ShardNumber:    shardNumber,
			}
			actual := awaitOffset(ctx.T, offsetStore, projectionID, committed, "the offset of the measured event to be committed")
			ctx.Expect(actual).To(specs.Not(specs.BeNil()))

			ctx.Expect(journalStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(offsetStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with dead letter handler on skip failure", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()
			shardNumber := uint64(9)

			journalStore := testkit2.NewEventsStore()
			ctx.Expect(journalStore.Connect(bg)).To(specs.BeNil())
			offsetStore := testkit2.NewOffsetStore()
			ctx.Expect(offsetStore.Connect(bg)).To(specs.BeNil())

			handler := &testHandler1{} // always returns error
			dlh := projection.NewDiscardDeadLetterHandler()

			event, err := anypb.New(&testpb.AccountCredited{AccountId: persistenceID, AccountBalance: 100})
			ctx.Expect(err).To(specs.BeNil())
			timestamp := time.Now().Unix()

			journals := []*egopb.Event{
				{
					PersistenceId:  persistenceID,
					SequenceNumber: 1,
					IsDeleted:      false,
					Event:          event,
					Timestamp:      timestamp,
					Shard:          shardNumber,
				},
			}
			ctx.Expect(journalStore.WriteEvents(bg, persistence.Unscoped(), journals, persistence.Unconditional())).To(specs.BeNil())

			runner := New(projectionName, handler, journalStore, offsetStore,
				WithPullInterval(time.Millisecond),
				WithRecoveryStrategy(projection.NewRecovery(
					projection.WithRecoveryPolicy(projection.Skip))),
				WithDeadLetterHandler(dlh))

			ctx.Expect(runner.Start(bg)).To(specs.BeNil())
			runner.Run(bg, nil)

			// with Skip policy, events are skipped and offset is still committed
			projectionID := &egopb.ProjectionId{
				ProjectionName: projectionName,
				ShardNumber:    shardNumber,
			}
			actual := awaitOffset(ctx.T, offsetStore, projectionID, committed, "the offset of the skipped event to be committed")
			ctx.Expect(actual).To(specs.Not(specs.BeNil()))

			ctx.Expect(journalStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(offsetStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with dead letter handler error logging", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()
			shardNumber := uint64(9)

			journalStore := testkit2.NewEventsStore()
			ctx.Expect(journalStore.Connect(bg)).To(specs.BeNil())
			offsetStore := testkit2.NewOffsetStore()
			ctx.Expect(offsetStore.Connect(bg)).To(specs.BeNil())

			handler := &testHandler1{} // always returns error
			dlh := &errorDeadLetterHandler{}

			event, err := anypb.New(&testpb.AccountCredited{AccountId: persistenceID, AccountBalance: 100})
			ctx.Expect(err).To(specs.BeNil())
			timestamp := time.Now().Unix()

			journals := []*egopb.Event{
				{
					PersistenceId:  persistenceID,
					SequenceNumber: 1,
					IsDeleted:      false,
					Event:          event,
					Timestamp:      timestamp,
					Shard:          shardNumber,
				},
			}
			ctx.Expect(journalStore.WriteEvents(bg, persistence.Unscoped(), journals, persistence.Unconditional())).To(specs.BeNil())

			runner := New(projectionName, handler, journalStore, offsetStore,
				WithPullInterval(time.Millisecond),
				WithRecoveryStrategy(projection.NewRecovery(
					projection.WithRecoveryPolicy(projection.Skip))),
				WithDeadLetterHandler(dlh))

			ctx.Expect(runner.Start(bg)).To(specs.BeNil())
			runner.Run(bg, nil)

			// the failing dead letter handler is reached and its error is only logged
			awaitCalls(ctx.T, &dlh.calls, 1, "the dead letter handler to be called")

			ctx.Expect(journalStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(offsetStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("with starting offset configured", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()
			shardNumber := uint64(9)

			journalStore := &pullCountingEventsStore{EventStore: testkit2.NewEventsStore()}
			ctx.Expect(journalStore.Connect(bg)).To(specs.BeNil())
			offsetStore := testkit2.NewOffsetStore()
			ctx.Expect(offsetStore.Connect(bg)).To(specs.BeNil())

			handler := projection.NewDiscardHandler()

			event, err := anypb.New(&testpb.AccountCredited{AccountId: persistenceID, AccountBalance: 100})
			ctx.Expect(err).To(specs.BeNil())
			timestamp := time.Now().Unix()

			journals := []*egopb.Event{
				{
					PersistenceId:  persistenceID,
					SequenceNumber: 1,
					IsDeleted:      false,
					Event:          event,
					Timestamp:      timestamp,
					Shard:          shardNumber,
				},
			}
			ctx.Expect(journalStore.WriteEvents(bg, persistence.Unscoped(), journals, persistence.Unconditional())).To(specs.BeNil())

			startOffset := time.Now().Add(-time.Hour)
			runner := New(projectionName, handler, journalStore, offsetStore,
				WithPullInterval(time.Millisecond),
				WithStartOffset(startOffset))

			ctx.Expect(runner.Start(bg)).To(specs.BeNil())
			runner.Run(bg, nil)

			// the runner keeps pulling with the starting offset configured
			awaitCalls(ctx.T, &journalStore.pulls, 3, "the runner to pull three times")
			ctx.Expect(runner.running.Load()).To(specs.BeTrue())

			ctx.Expect(journalStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(offsetStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
		s.It("when current offset is zero", func(ctx *specs.Context) {
			bg := context.TODO()
			projectionName := "db-writer"
			persistenceID := uuid.NewString()
			shardNumber := uint64(9)
			timestamp := timestamppb.Now()
			handler := projection.NewDiscardHandler()

			// create the projection id
			projectionID := &egopb.ProjectionId{
				ProjectionName: projectionName,
				ShardNumber:    shardNumber,
			}

			offset := &egopb.Offset{
				ShardNumber:    shardNumber,
				ProjectionName: projectionName,
				Value:          timestamp.AsTime().Unix(),
				Timestamp:      0,
			}

			event, err := anypb.New(&testpb.AccountCredited{})
			ctx.Expect(err).To(specs.BeNil())
			nextOffsetValue := timestamppb.New(time.Now().Add(time.Minute))
			events := []*egopb.Event{
				{
					PersistenceId:  persistenceID,
					SequenceNumber: 1,
					IsDeleted:      false,
					Event:          event,
					Timestamp:      timestamp.AsTime().Unix(),
					Shard:          shardNumber,
				},
			}

			maxBufferSize := 10
			resetOffsetTo := time.Now().UTC()

			writes := atomic.NewInt32(0)
			offsetStore := new(mocksoffsetstore.OffsetStore)
			offsetStore.EXPECT().Ping(mock.Anything).Return(nil)
			offsetStore.EXPECT().ResetOffset(mock.Anything, projectionName, resetOffsetTo.UnixMilli()).Return(nil)
			offsetStore.EXPECT().GetCurrentOffset(mock.Anything, projectionID).Return(offset, nil)
			offsetStore.EXPECT().WriteOffset(mock.Anything, mock.AnythingOfType("*egopb.Offset")).Return(nil).
				Run(func(_ context.Context, _ *egopb.Offset) { writes.Inc() })

			eventsStore := new(mockseventstore.EventsStore)
			eventsStore.EXPECT().Ping(mock.Anything).Return(nil)
			eventsStore.EXPECT().ShardOffsets(mock.Anything).Return(map[uint64]int64{shardNumber: nextOffsetValue.AsTime().UnixMilli()}, nil)
			eventsStore.EXPECT().GetShardEvents(mock.Anything, shardNumber, offset.GetValue(), uint64(maxBufferSize)).Return(events, nextOffsetValue.AsTime().UnixMilli(), nil)

			// create an instance of the projection
			runner := New(projectionName, handler, eventsStore, offsetStore, WithPullInterval(time.Millisecond))
			runner.resetOffsetTo = resetOffsetTo
			runner.maxBufferSize = maxBufferSize

			// start the projection
			err = runner.Start(bg)
			ctx.Expect(err).To(specs.BeNil())

			// run the projection
			runner.Run(bg, nil)

			awaitCalls(ctx.T, writes, 1, "the batch offset to be written")

			eventsStore.AssertExpectations(ctx.T)
			offsetStore.AssertExpectations(ctx.T)

			ctx.Expect(runner.running.Load()).To(specs.BeTrue())

			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
	})
}

func TestProjectionRunnerLagMetrics(t *testing.T) {
	t.Run("lag resets to zero when projection is caught up", func(t *testing.T) {
		ctx := context.TODO()
		projectionName := "lag-test"
		persistenceID := uuid.NewString()
		shardNumber := uint64(3)

		journalStore := testkit2.NewEventsStore()
		require.NoError(t, journalStore.Connect(ctx))
		offsetStore := testkit2.NewOffsetStore()
		require.NoError(t, offsetStore.Connect(ctx))

		handler := projection.NewDiscardHandler()
		meter := noopmetric.NewMeterProvider().Meter("test")
		m := instrumentation.New(meter)

		event, err := anypb.New(&testpb.AccountCredited{AccountId: persistenceID, AccountBalance: 50})
		require.NoError(t, err)
		timestamp := time.Now().Unix()

		journals := []*egopb.Event{
			{
				PersistenceId:  persistenceID,
				SequenceNumber: 1,
				IsDeleted:      false,
				Event:          event,
				Timestamp:      timestamp,
				Shard:          shardNumber,
			},
		}
		require.NoError(t, journalStore.WriteEvents(ctx, persistence.Unscoped(), journals, persistence.Unconditional()))

		runner := New(projectionName, handler, journalStore, offsetStore,
			WithPullInterval(time.Millisecond),
			WithMetrics(m))

		require.NoError(t, runner.Start(ctx))
		runner.Run(ctx, nil)

		// Wait for the runner to process the event and then tick again with no events.
		pause.For(time.Second)

		// Verify the offset was committed (projection processed the event).
		projectionID := &egopb.ProjectionId{
			ProjectionName: projectionName,
			ShardNumber:    shardNumber,
		}
		actual, err := offsetStore.GetCurrentOffset(ctx, projectionID)
		require.NoError(t, err)
		require.NotNil(t, actual)
		assert.Equal(t, timestamp, actual.GetValue())

		require.NoError(t, runner.Stop())
		require.NoError(t, journalStore.Disconnect(ctx))
		require.NoError(t, offsetStore.Disconnect(ctx))
	})

	t.Run("lag uses seconds-based offset and converts to milliseconds", func(t *testing.T) {
		ctx := context.TODO()
		projectionName := "lag-formula-test"
		persistenceID := uuid.NewString()
		shardNumber := uint64(5)

		journalStore := testkit2.NewEventsStore()
		require.NoError(t, journalStore.Connect(ctx))
		offsetStore := testkit2.NewOffsetStore()
		require.NoError(t, offsetStore.Connect(ctx))

		handler := projection.NewDiscardHandler()
		meter := noopmetric.NewMeterProvider().Meter("test")
		m := instrumentation.New(meter)

		// Write an event with a timestamp 2 seconds in the past.
		pastTimestamp := time.Now().Unix() - 2
		event, err := anypb.New(&testpb.AccountCredited{AccountId: persistenceID, AccountBalance: 100})
		require.NoError(t, err)

		journals := []*egopb.Event{
			{
				PersistenceId:  persistenceID,
				SequenceNumber: 1,
				IsDeleted:      false,
				Event:          event,
				Timestamp:      pastTimestamp,
				Shard:          shardNumber,
			},
		}
		require.NoError(t, journalStore.WriteEvents(ctx, persistence.Unscoped(), journals, persistence.Unconditional()))

		runner := New(projectionName, handler, journalStore, offsetStore,
			WithPullInterval(time.Millisecond),
			WithMetrics(m))

		require.NoError(t, runner.Start(ctx))
		runner.Run(ctx, nil)

		// Wait for processing and a subsequent caught-up tick.
		pause.For(time.Second)

		// Verify the offset was committed with the event's timestamp.
		projectionID := &egopb.ProjectionId{
			ProjectionName: projectionName,
			ShardNumber:    shardNumber,
		}
		actual, err := offsetStore.GetCurrentOffset(ctx, projectionID)
		require.NoError(t, err)
		require.NotNil(t, actual)
		assert.Equal(t, pastTimestamp, actual.GetValue())

		require.NoError(t, runner.Stop())
		require.NoError(t, journalStore.Disconnect(ctx))
		require.NoError(t, offsetStore.Disconnect(ctx))
	})
}

// testEventsTopic is the in-process topic the tests publish persisted events
// on; the host passes its own topic through WithEventsStream.
const testEventsTopic = "topic.events"

type testHandler1 struct{}

var _ projection.Handler = &testHandler1{}

func (x testHandler1) Handle(_ context.Context, _ string, _ *anypb.Any, _ uint64) error {
	return errors.New("damn")
}

type testHandler2 struct {
	counter *atomic.Int32
}

func (x testHandler2) Handle(_ context.Context, _ string, _ *anypb.Any, revision uint64) error {
	if (int(revision) % 2) == 0 {
		return errors.New("failed underlying")
	}
	x.counter.Inc()
	return nil
}

type testPanicHandler struct{}

var _ projection.Handler = &testPanicHandler{}

func (x testPanicHandler) Handle(_ context.Context, _ string, _ *anypb.Any, _ uint64) error {
	panic("boom")
}

type errorDeadLetterHandler struct {
	calls atomic.Int32
}

var _ projection.DeadLetterHandler = &errorDeadLetterHandler{}

func (x *errorDeadLetterHandler) Handle(_ context.Context, _ string, _ string, _ *anypb.Any, _ uint64, _ error) error {
	x.calls.Inc()
	return errors.New("dead letter handler failed")
}

type noopRunnerAdapter struct{}

var _ eventadapter.EventAdapter = &noopRunnerAdapter{}

func (a *noopRunnerAdapter) Adapt(event *anypb.Any, _ uint64) (*anypb.Any, error) {
	return event, nil
}

// TestRunnerPullEfficiency locks in the O(active shards) pull behavior: the
// committed offset of a shard is fetched from the offset store only once, a
// batch of events commits its offset exactly once, and a caught-up shard is
// not fetched again on subsequent pulls.
func TestRunnerPullEfficiency(t *testing.T) {
	t.Run("with caught-up shard skipped, offset cached and batch committed once", func(t *testing.T) {
		ctx := context.TODO()
		projectionName := "db-writer"
		persistenceID := uuid.NewString()
		shardNumber := uint64(9)

		projectionID := &egopb.ProjectionId{
			ProjectionName: projectionName,
			ShardNumber:    shardNumber,
		}

		event, err := anypb.New(&testpb.AccountCredited{})
		require.NoError(t, err)

		const (
			committedOffset = int64(100)
			latestOffset    = int64(500)
		)

		events := []*egopb.Event{
			{PersistenceId: persistenceID, SequenceNumber: 1, Event: event, Timestamp: 200, Shard: shardNumber},
			{PersistenceId: persistenceID, SequenceNumber: 2, Event: event, Timestamp: 300, Shard: shardNumber},
			{PersistenceId: persistenceID, SequenceNumber: 3, Event: event, Timestamp: latestOffset, Shard: shardNumber},
		}

		maxBufferSize := 10

		offsetStore := new(mocksoffsetstore.OffsetStore)
		offsetStore.EXPECT().Ping(mock.Anything).Return(nil)
		// the committed offset must be resolved from the store exactly once:
		// afterwards the runner serves it from its in-memory cache.
		offsetStore.EXPECT().GetCurrentOffset(mock.Anything, projectionID).Return(&egopb.Offset{
			ShardNumber:    shardNumber,
			ProjectionName: projectionName,
			Value:          committedOffset,
		}, nil).Once()
		// the whole batch of three events must commit exactly once, with the
		// batch next offset.
		offsetStore.EXPECT().WriteOffset(mock.Anything, mock.MatchedBy(func(offset *egopb.Offset) bool {
			return offset.GetShardNumber() == shardNumber && offset.GetValue() == latestOffset
		})).Return(nil).Once()

		eventsStore := new(mockseventstore.EventsStore)
		eventsStore.EXPECT().Ping(mock.Anything).Return(nil)
		eventsStore.EXPECT().ShardOffsets(mock.Anything).Return(map[uint64]int64{shardNumber: latestOffset}, nil)
		// once the shard is caught up (committed == latest), subsequent pulls
		// must skip it entirely: a second fetch would violate Once().
		eventsStore.EXPECT().GetShardEvents(mock.Anything, shardNumber, committedOffset, uint64(maxBufferSize)).
			Return(events, latestOffset, nil).Once()

		handler := projection.NewDiscardHandler()
		runner := New(projectionName, handler, eventsStore, offsetStore,
			WithPullInterval(10*time.Millisecond),
			WithLogger(discardLogger),
		)
		runner.maxBufferSize = maxBufferSize

		require.NoError(t, runner.Start(ctx))
		runner.Run(ctx, nil)

		// many pull intervals elapse here; the Once() expectations above
		// prove none of them re-fetched the caught-up shard.
		pause.For(time.Second)

		require.True(t, runner.running.Load())
		eventsStore.AssertExpectations(t)
		offsetStore.AssertExpectations(t)

		require.NoError(t, runner.Stop())
	})
	t.Run("with events stream nudge and full buffer re-poll processing ahead of the pull interval", func(t *testing.T) {
		ctx := context.TODO()
		projectionName := "db-writer"
		persistenceID := uuid.NewString()
		shardNumber := uint64(9)

		eventsStore := testkit2.NewEventsStore()
		require.NoError(t, eventsStore.Connect(ctx))
		offsetStore := testkit2.NewOffsetStore()
		require.NoError(t, offsetStore.Connect(ctx))

		stream := eventstream.New()

		handler := projection.NewDiscardHandler()
		// the pull interval is far longer than the test: any processing that
		// happens can only have been triggered by the stream nudge, and any
		// processing beyond the first buffer only by the full-buffer re-poll.
		runner := New(projectionName, handler, eventsStore, offsetStore,
			WithPullInterval(10*time.Minute),
			WithLogger(discardLogger),
			WithEventsStream(stream, testEventsTopic),
		)
		runner.maxBufferSize = 2

		require.NoError(t, runner.Start(ctx))
		runner.Run(ctx, nil)

		event, err := anypb.New(&testpb.AccountCredited{})
		require.NoError(t, err)

		count := 5
		journals := make([]*egopb.Event, count)
		for i := range count {
			journals[i] = &egopb.Event{
				PersistenceId:  persistenceID,
				SequenceNumber: uint64(i + 1),
				Event:          event,
				Timestamp:      int64(i + 1),
				Shard:          shardNumber,
			}
		}
		require.NoError(t, eventsStore.WriteEvents(ctx, persistence.Unscoped(), journals, persistence.Unconditional()))

		// mimic what entity actors do after persisting events on this node
		stream.Publish(testEventsTopic, journals[count-1])

		pause.For(time.Second)

		projectionID := &egopb.ProjectionId{
			ProjectionName: projectionName,
			ShardNumber:    shardNumber,
		}
		actual, err := offsetStore.GetCurrentOffset(ctx, projectionID)
		require.NoError(t, err)
		// all five events were processed from a single nudge even though the
		// buffer only holds two: the full-buffer re-poll drained the backlog.
		require.EqualValues(t, journals[count-1].GetTimestamp(), actual.GetValue())

		require.NoError(t, eventsStore.Disconnect(ctx))
		require.NoError(t, offsetStore.Disconnect(ctx))
		require.NoError(t, runner.Stop())
	})
}

// -----------------------------------------------------------------------------
// Default logger
// -----------------------------------------------------------------------------

func TestProjectionRunnerDefaultLogger(t *testing.T) {
	t.Run("no withLogger option yields the discard logger", func(t *testing.T) {
		runner := New("projection-name", testHandler1{}, nil, nil)
		require.NotNil(t, runner.logger)
		// A runner is always handed the actor system's logger by the
		// projection actor; the construction default must stay silent.
		assert.Same(t, discardLogger, runner.logger)
	})

	t.Run("withLogger overrides the default", func(t *testing.T) {
		custom := kitlog.New(kitlog.Config{Sink: slog.DiscardHandler})
		runner := New("projection-name", testHandler1{}, nil, nil, WithLogger(custom))
		assert.Same(t, custom, runner.logger)
	})
}

// TestProjectionRunnerStaysRuntimeNeutral checks the transitive dependency
// closure, which the former architecture checker does not see: projection execution must not reach
// the actor runtime that hosts it.
func TestProjectionRunnerStaysRuntimeNeutral(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("the go tool is not on PATH")
	}

	out, err := exec.Command(goBin, "list", "-deps", ".").CombinedOutput()
	require.NoError(t, err, "go list -deps failed: %s", out)

	deps := strings.Fields(string(out))
	require.NotEmpty(t, deps)
	for _, dep := range deps {
		assert.Falsef(t, strings.HasPrefix(dep, "github.com/tochemey/goakt"),
			"internal/projectionrunner must not depend on GoAkt; found %s", dep)
		assert.Falsef(t, strings.HasSuffix(dep, "/ego/engine"),
			"internal/projectionrunner must not depend on the engine package; found %s", dep)
		assert.Falsef(t, strings.HasSuffix(dep, "/internal/extensions"),
			"internal/projectionrunner must not depend on the GoAkt adapter's internals; found %s", dep)
	}
}
