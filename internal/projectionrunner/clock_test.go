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
	"sync"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/atomic"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/internal/instrumentation"
	"github.com/getsyntegrity/ego/persistence"
	"github.com/getsyntegrity/ego/projection"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
	testkit2 "github.com/getsyntegrity/ego/testkit"
)

// manualClock adapts a go-specs ManualClock to the runner's clock and records
// the duration of every timer the runner arms, so a test can assert the delay
// the runner chose, not only that some timer fired. A test moves time with
// Advance and waits for the runner to arm a timer with awaitTimer, so an
// advance is never lost to a timer that does not exist yet.
type manualClock struct {
	*specs.ManualClock

	mu     sync.Mutex
	delays []time.Duration
}

var _ clock = (*manualClock)(nil)

func newManualClock() *manualClock { return &manualClock{ManualClock: specs.NewManualClock()} }

// NewTimer records d and arms a timer on the manual clock. The go-specs timer
// already has the method set of the runner's timer.
func (m *manualClock) NewTimer(d time.Duration) timer {
	m.mu.Lock()
	m.delays = append(m.delays, d)
	m.mu.Unlock()
	return m.ManualClock.NewTimer(d)
}

// timers returns the duration of every timer armed so far, in order.
func (m *manualClock) timers() []time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]time.Duration(nil), m.delays...)
}

// awaitTimer waits until the runner has exactly one timer pending.
func awaitTimer(ctx *specs.Context, clk *manualClock) {
	ctx.Eventually(func() any { return clk.Pending() }, specs.Equal(1), poll...)
}

// clockedStores stubs the two stores for a runner that never reaches a real
// shard: both answer Ping, and ShardOffsets answers shardOffsets and counts
// its calls in pulls.
func clockedStores(ctx *specs.Context, pulls *atomic.Int32, shardOffsets func() []any) (eventsStoreMock, offsetStoreMock) {
	offsetCtrl := mock.NewController(ctx)
	offsetCtrl.Method("Ping").Expect(mock.Any()).Return(nil).AtLeast(1)

	eventsCtrl := mock.NewController(ctx)
	eventsCtrl.Method("Ping").Expect(mock.Any()).Return(nil).AtLeast(1)
	eventsCtrl.Method("ShardOffsets").Expect(mock.Any()).AtLeast(1).
		Do(func([]any) []any { pulls.Inc(); return shardOffsets() })

	return eventsStoreMock{eventsCtrl}, offsetStoreMock{offsetCtrl}
}

// seededShard is a shard holding one event, written to testkit stores. The
// committed offset is written only when it is not zero.
func seededShard(ctx *specs.Context, name string, shard uint64, committed, eventTimestamp int64) (*testkit2.EventStore, *testkit2.OffsetStore) {
	bg := context.TODO()
	events := testkit2.NewEventsStore()
	ctx.Expect(events.Connect(bg)).To(specs.BeNil())
	offsets := testkit2.NewOffsetStore()
	ctx.Expect(offsets.Connect(bg)).To(specs.BeNil())

	if committed != 0 {
		ctx.Expect(offsets.WriteOffset(bg, &egopb.Offset{ProjectionName: name, ShardNumber: shard, Value: committed})).To(specs.BeNil())
	}

	event, err := anypb.New(&testpb.AccountCredited{})
	ctx.Expect(err).To(specs.BeNil())
	journal := []*egopb.Event{{
		PersistenceId:  uuid.NewString(),
		SequenceNumber: 1,
		Event:          event,
		Timestamp:      eventTimestamp,
		Shard:          shard,
	}}
	ctx.Expect(events.WriteEvents(bg, persistence.Unscoped(), journal, persistence.Unconditional())).To(specs.BeNil())

	return events, offsets
}

// lagOf observes the lag gauge the runner recorded.
func lagOf(reader *sdkmetric.ManualReader) func() any {
	return func() any {
		var collected metricdata.ResourceMetrics
		if err := reader.Collect(context.TODO(), &collected); err != nil {
			return err
		}
		for _, scope := range collected.ScopeMetrics {
			for _, metric := range scope.Metrics {
				gauge, ok := metric.Data.(metricdata.Gauge[int64])
				if metric.Name == "ego.projection.lag_ms" && ok && len(gauge.DataPoints) > 0 {
					return gauge.DataPoints[0].Value
				}
			}
		}
		return nil
	}
}

// failingHandler fails every event and counts the attempts.
type failingHandler struct{ calls *atomic.Int32 }

func (h failingHandler) Handle(context.Context, string, *anypb.Any, uint64) error {
	h.calls.Inc()
	return errFailed
}

func TestWithClock(t *testing.T) {
	specs.Describe(t, "the runner reads time through an injectable clock", func(s *specs.Spec) {
		s.It("uses the clock given to WithClock", func(ctx *specs.Context) {
			manual := newManualClock()
			var r Runner
			WithClock(manual).Apply(&r)
			ctx.Expect(r.clock).To(specs.Equal(manual))
		})
		s.It("defaults New to the real clock", func(ctx *specs.Context) {
			runner := New("clock-default", nil, nil, nil)
			ctx.Expect(runner.clock).To(specs.Equal(realClock{}))
		})
		s.It("keeps the real clock when WithClock is given nil", func(ctx *specs.Context) {
			runner := New("clock-nil", nil, nil, nil, WithClock(nil))
			ctx.Expect(runner.clock).To(specs.Equal(realClock{}))
		})
		s.It("has a real clock that tells the wall time and fires its timers", func(ctx *specs.Context) {
			before := time.Now()
			now := realClock{}.Now()
			ctx.Expect(now.Before(before)).To(specs.BeFalse())

			fired := realClock{}.NewTimer(time.Millisecond)
			defer fired.Stop()
			ctx.Eventually(func() any {
				select {
				case <-fired.C():
					return true
				default:
					return false
				}
			}, specs.BeTrue(), poll...)

			stopped := realClock{}.NewTimer(time.Hour)
			ctx.Expect(stopped.Stop()).To(specs.BeTrue())
		})
	})
}

func TestRunnerOnAManualClock(t *testing.T) {
	const interval = 10 * time.Second

	specs.Describe(t, "the runner waits only on the clock it was given", func(s *specs.Spec) {
		s.It("pulls once per interval of the clock", func(ctx *specs.Context) {
			bg := context.TODO()
			clk := newManualClock()
			pulls := atomic.NewInt32(0)
			eventsStore, offsetStore := clockedStores(ctx, pulls, func() []any { return []any{nil, nil} })
			runner := New("clock-pull", projection.NewDiscardHandler(), eventsStore, offsetStore,
				WithPullInterval(interval), WithClock(clk))

			ctx.Expect(runner.Start(bg)).To(specs.BeNil())
			runner.Run(bg, nil)
			awaitTimer(ctx, clk)

			// nothing is pulled until a whole interval has elapsed
			clk.Advance(interval - time.Millisecond)
			ctx.Expect(pulls.Load()).To(specs.Equal(int32(0)))

			clk.Advance(time.Millisecond)
			ctx.Eventually(counter(pulls), specs.Equal(int32(1)), poll...)
			awaitTimer(ctx, clk)
			ctx.Expect(pulls.Load()).To(specs.Equal(int32(1)))

			clk.Advance(interval)
			ctx.Eventually(counter(pulls), specs.Equal(int32(2)), poll...)
			awaitTimer(ctx, clk)
			ctx.Expect(clk.timers()).To(specs.Equal([]time.Duration{interval, interval, interval}))

			// stopping the runner leaves no timer behind
			ctx.Expect(runner.Stop()).To(specs.BeNil())
			ctx.Eventually(func() any { return clk.Pending() }, specs.Equal(0), poll...)
		})

		s.It("backs off on the clock after a failed store round trip", func(ctx *specs.Context) {
			bg := context.TODO()
			clk := newManualClock()
			pulls := atomic.NewInt32(0)
			eventsStore, offsetStore := clockedStores(ctx, pulls, func() []any { return []any{nil, errFailed} })
			runner := New("clock-backoff", projection.NewDiscardHandler(), eventsStore, offsetStore,
				WithPullInterval(interval), WithClock(clk))

			ctx.Expect(runner.Start(bg)).To(specs.BeNil())
			runner.Run(bg, nil)
			awaitTimer(ctx, clk)

			// the first failure waits storeRetryDelay(1), then the next pull waits for the interval
			clk.Advance(interval)
			ctx.Eventually(counter(pulls), specs.Equal(int32(1)), poll...)
			awaitTimer(ctx, clk)
			clk.Advance(storeRetryDelay(1))
			awaitTimer(ctx, clk)

			// the second consecutive failure waits twice as long
			clk.Advance(interval)
			ctx.Eventually(counter(pulls), specs.Equal(int32(2)), poll...)
			awaitTimer(ctx, clk)

			ctx.Expect(clk.timers()).To(specs.Equal([]time.Duration{
				interval, storeRetryDelay(1), interval, storeRetryDelay(2),
			}))
			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})

		s.It("pings the stores five times, one second apart, before Start fails", func(ctx *specs.Context) {
			clk := newManualClock()
			pings := atomic.NewInt32(0)

			eventsCtrl := mock.NewController(ctx)
			eventsCtrl.Method("Ping").Expect(mock.Any()).AtLeast(1).
				Do(func([]any) []any { pings.Inc(); return []any{errors.New("fail ping")} })
			offsetCtrl := mock.NewController(ctx)
			offsetCtrl.Method("Ping").Expect(mock.Any()).Return(nil).AtLeast(1)

			runner := New("clock-ping", projection.NewDiscardHandler(), eventsStoreMock{eventsCtrl}, offsetStoreMock{offsetCtrl},
				WithClock(clk))

			started := make(chan error, 1)
			ctx.Go(func(*specs.Context) { started <- runner.Start(context.Background()) })

			for range 4 {
				awaitTimer(ctx, clk)
				clk.Advance(time.Second)
			}

			ctx.Expect(awaitFailure(ctx, started)).To(haveMessage("failed to start the projection: fail ping"))
			ctx.Expect(pings.Load()).To(specs.Equal(int32(5)))
			ctx.Expect(clk.timers()).To(specs.Equal([]time.Duration{time.Second, time.Second, time.Second, time.Second}))
		})

		s.It("retries a failing handler on the clock before it skips the event", func(ctx *specs.Context) {
			bg := context.TODO()
			const name, shard = "clock-recovery", uint64(4)
			clk := newManualClock()
			eventTimestamp := clk.Now().Add(time.Second).UnixNano()
			eventsStore, offsetStore := seededShard(ctx, name, shard, 0, eventTimestamp)

			attempts := atomic.NewInt32(0)
			runner := New(name, failingHandler{attempts}, eventsStore, offsetStore,
				WithPullInterval(interval), WithClock(clk),
				WithRecoveryStrategy(projection.NewRecovery(
					projection.WithRecoveryPolicy(projection.RetryAndSkip),
					projection.WithRetries(3),
					projection.WithRetryDelay(500*time.Millisecond))))

			ctx.Expect(runner.Start(bg)).To(specs.BeNil())
			runner.Run(bg, nil)
			awaitTimer(ctx, clk)

			clk.Advance(interval)
			ctx.Eventually(counter(attempts), specs.Equal(int32(1)), poll...)
			awaitTimer(ctx, clk)
			clk.Advance(500 * time.Millisecond)
			ctx.Eventually(counter(attempts), specs.Equal(int32(2)), poll...)
			awaitTimer(ctx, clk)
			clk.Advance(500 * time.Millisecond)

			// the third attempt is the last: the event is skipped and its offset committed
			projectionID := &egopb.ProjectionId{ProjectionName: name, ShardNumber: shard}
			ctx.Eventually(offsetOf(offsetStore, projectionID), committedAt(eventTimestamp), poll...)
			ctx.Expect(attempts.Load()).To(specs.Equal(int32(3)))
			ctx.Expect(clk.timers()[:3]).To(specs.Equal([]time.Duration{interval, 500 * time.Millisecond, 500 * time.Millisecond}))

			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})

		s.It("stamps the committed offset with the time of the clock", func(ctx *specs.Context) {
			bg := context.TODO()
			const name, shard = "clock-stamp", uint64(2)
			clk := newManualClock()
			eventTimestamp := clk.Now().Add(time.Second).UnixNano()
			eventsStore, offsetStore := seededShard(ctx, name, shard, 0, eventTimestamp)

			runner := New(name, projection.NewDiscardHandler(), eventsStore, offsetStore,
				WithPullInterval(interval), WithClock(clk))
			ctx.Expect(runner.Start(bg)).To(specs.BeNil())
			runner.Run(bg, nil)
			awaitTimer(ctx, clk)

			clk.Advance(interval)
			projectionID := &egopb.ProjectionId{ProjectionName: name, ShardNumber: shard}
			ctx.Eventually(offsetOf(offsetStore, projectionID), committedAt(eventTimestamp), poll...)

			offset, err := offsetStore.GetCurrentOffset(bg, projectionID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(offset.GetTimestamp()).To(specs.Equal(clk.Now().UnixMilli()))

			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})

		s.It("measures the lag against the time of the clock", func(ctx *specs.Context) {
			bg := context.TODO()
			const name, shard = "clock-lag", uint64(6)
			clk := newManualClock()
			epoch := clk.Now()
			// the shard is committed up to 8s past the epoch and the clock reads 10s
			eventsStore, offsetStore := seededShard(ctx, name, shard, epoch.Add(8*time.Second).UnixNano(), epoch.Add(9*time.Second).UnixNano())

			reader := sdkmetric.NewManualReader()
			metrics := instrumentation.New(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))

			// a one-event buffer is full, so the lag stays recorded after the pass
			runner := New(name, projection.NewDiscardHandler(), eventsStore, offsetStore,
				WithPullInterval(interval), WithClock(clk), WithMetrics(metrics), WithMaxBufferSize(1))
			ctx.Expect(runner.Start(bg)).To(specs.BeNil())
			runner.Run(bg, nil)
			awaitTimer(ctx, clk)

			clk.Advance(interval)
			ctx.Eventually(lagOf(reader), specs.Equal(int64(2000)), poll...)

			ctx.Expect(runner.Stop()).To(specs.BeNil())
		})
	})
}
