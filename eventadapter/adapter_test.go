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

package eventadapter

import (
	"errors"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// noopAdapter returns the event unchanged.
type noopAdapter struct{}

func (a *noopAdapter) Adapt(event *anypb.Any, _ uint64) (*anypb.Any, error) {
	return event, nil
}

// timestampToDurationAdapter transforms a Timestamp event into a Duration
// using the seconds field from the timestamp.
type timestampToDurationAdapter struct{}

func (a *timestampToDurationAdapter) Adapt(event *anypb.Any, _ uint64) (*anypb.Any, error) {
	var ts timestamppb.Timestamp
	if err := event.UnmarshalTo(&ts); err != nil {
		// not a Timestamp; pass through
		return event, nil
	}
	return anypb.New(durationpb.New(ts.AsTime().Sub(ts.AsTime()) + ts.AsTime().Sub(ts.AsTime())))
}

// errorAdapter always returns an error.
type errorAdapter struct {
	err error
}

func (a *errorAdapter) Adapt(_ *anypb.Any, _ uint64) (*anypb.Any, error) {
	return nil, a.err
}

// revisionGatedAdapter only transforms events at or above a given revision.
// It replaces a Timestamp event with a Duration of 42 seconds.
type revisionGatedAdapter struct {
	minRevision uint64
}

func (a *revisionGatedAdapter) Adapt(event *anypb.Any, revision uint64) (*anypb.Any, error) {
	if revision < a.minRevision {
		return event, nil
	}
	return anypb.New(durationpb.New(42_000_000_000)) // 42 seconds
}

// addSecondsAdapter adds extra seconds to a Duration event.
type addSecondsAdapter struct {
	extra int64
}

func (a *addSecondsAdapter) Adapt(event *anypb.Any, _ uint64) (*anypb.Any, error) {
	var d durationpb.Duration
	if err := event.UnmarshalTo(&d); err != nil {
		return event, nil
	}
	d.Seconds += a.extra
	return anypb.New(&d)
}

func TestChainNoAdapters(t *testing.T) {
	specs.Describe(t, "Chain with no adapters returns the event unchanged", func(s *specs.Spec) {
		s.It("returns the same event for a nil slice and an empty slice", func(ctx *specs.Context) {
			event, err := anypb.New(timestamppb.Now())
			ctx.Expect(err).To(specs.BeNil())

			// nil slice
			result, err := Chain(nil, event, 1)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(result == event).To(specs.BeTrue())

			// empty slice
			result, err = Chain([]EventAdapter{}, event, 1)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(result == event).To(specs.BeTrue())
		})
	})
}

func TestChainSingleAdapterTransforms(t *testing.T) {
	specs.Describe(t, "Chain applies a single adapter to the event", func(s *specs.Spec) {
		s.It("transforms a Timestamp into a Duration", func(ctx *specs.Context) {
			ts := &timestamppb.Timestamp{Seconds: 1000, Nanos: 0}
			event, err := anypb.New(ts)
			ctx.Expect(err).To(specs.BeNil())

			adapters := []EventAdapter{&timestampToDurationAdapter{}}
			result, err := Chain(adapters, event, 1)
			ctx.Expect(err).To(specs.BeNil())

			// The result should be a Duration (different type URL from the input Timestamp)
			var d durationpb.Duration
			ctx.Expect(result.UnmarshalTo(&d)).To(specs.BeNil())
			// timestampToDurationAdapter produces a zero duration
			ctx.Expect(d.GetSeconds()).ToEqual(int64(0))
		})
	})
}

func TestChainMultipleAdaptersAppliedInOrder(t *testing.T) {
	specs.Describe(t, "Chain applies several adapters in order", func(s *specs.Spec) {
		s.It("accumulates the seconds added by each adapter", func(ctx *specs.Context) {
			// Start with a Duration of 10 seconds
			event, err := anypb.New(durationpb.New(10_000_000_000)) // 10s
			ctx.Expect(err).To(specs.BeNil())

			adapters := []EventAdapter{
				&addSecondsAdapter{extra: 5},
				&addSecondsAdapter{extra: 20},
			}

			result, err := Chain(adapters, event, 1)
			ctx.Expect(err).To(specs.BeNil())

			var d durationpb.Duration
			ctx.Expect(result.UnmarshalTo(&d)).To(specs.BeNil())
			// 10 + 5 + 20 = 35 seconds
			ctx.Expect(d.GetSeconds()).ToEqual(int64(35))
		})
	})
}

func TestChainAdapterReturnsError(t *testing.T) {
	specs.Describe(t, "Chain propagates an adapter error", func(s *specs.Spec) {
		s.It("returns the adapter error and a nil result", func(ctx *specs.Context) {
			event, err := anypb.New(timestamppb.Now())
			ctx.Expect(err).To(specs.BeNil())

			expectedErr := errors.New("adapter failure")
			adapters := []EventAdapter{
				&noopAdapter{},
				&errorAdapter{err: expectedErr},
				&addSecondsAdapter{extra: 100}, // should never run
			}

			result, err := Chain(adapters, event, 1)
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.MatchError(expectedErr))
			ctx.Expect(result).To(specs.BeNil())
		})
	})
}

func TestChainErrorStopsEarly(t *testing.T) {
	specs.Describe(t, "Chain stops at the first failing adapter", func(s *specs.Spec) {
		s.It("returns the mid-chain error and a nil result", func(ctx *specs.Context) {
			event, err := anypb.New(durationpb.New(10_000_000_000))
			ctx.Expect(err).To(specs.BeNil())

			expectedErr := errors.New("mid-chain error")
			adapters := []EventAdapter{
				&addSecondsAdapter{extra: 5},
				&errorAdapter{err: expectedErr},
				&addSecondsAdapter{extra: 100}, // should never run
			}

			result, err := Chain(adapters, event, 1)
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.MatchError(expectedErr))
			ctx.Expect(result).To(specs.BeNil())
		})
	})
}

func TestChainNoopAdapter(t *testing.T) {
	specs.Describe(t, "Chain with a noop adapter leaves the event untouched", func(s *specs.Spec) {
		s.It("keeps the same pointer and content", func(ctx *specs.Context) {
			original := timestamppb.Now()
			event, err := anypb.New(original)
			ctx.Expect(err).To(specs.BeNil())

			adapters := []EventAdapter{&noopAdapter{}}
			result, err := Chain(adapters, event, 1)
			ctx.Expect(err).To(specs.BeNil())
			// The pointer should be unchanged since noop returns the same event
			ctx.Expect(result == event).To(specs.BeTrue())

			// Content should still unmarshal to the same timestamp
			var ts timestamppb.Timestamp
			ctx.Expect(result.UnmarshalTo(&ts)).To(specs.BeNil())
			ctx.Expect(proto.Equal(original, &ts)).To(specs.BeTrue())
		})
	})
}

func TestChainAdapterUsesRevision(t *testing.T) {
	specs.Describe(t, "Chain passes the revision to each adapter", func(s *specs.Spec) {
		s.It("transforms only at or above the adapter's minimum revision", func(ctx *specs.Context) {
			event, err := anypb.New(timestamppb.Now())
			ctx.Expect(err).To(specs.BeNil())

			adapters := []EventAdapter{&revisionGatedAdapter{minRevision: 10}}

			// revision below threshold: event passes through unchanged
			result, err := Chain(adapters, event, 5)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(result == event).To(specs.BeTrue())

			// revision at threshold: event is transformed to a Duration
			result, err = Chain(adapters, event, 10)
			ctx.Expect(err).To(specs.BeNil())
			var d durationpb.Duration
			ctx.Expect(result.UnmarshalTo(&d)).To(specs.BeNil())
			ctx.Expect(d.GetSeconds()).ToEqual(int64(42))

			// revision above threshold: event is also transformed
			result, err = Chain(adapters, event, 100)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(result.UnmarshalTo(&d)).To(specs.BeNil())
			ctx.Expect(d.GetSeconds()).ToEqual(int64(42))
		})
	})
}

func TestChainMixedNoopAndTransform(t *testing.T) {
	specs.Describe(t, "Chain mixes noop and transforming adapters", func(s *specs.Spec) {
		s.It("applies only the transforming adapter's effect", func(ctx *specs.Context) {
			event, err := anypb.New(durationpb.New(1_000_000_000)) // 1s
			ctx.Expect(err).To(specs.BeNil())

			adapters := []EventAdapter{
				&noopAdapter{},
				&addSecondsAdapter{extra: 7},
				&noopAdapter{},
			}

			result, err := Chain(adapters, event, 1)
			ctx.Expect(err).To(specs.BeNil())

			var d durationpb.Duration
			ctx.Expect(result.UnmarshalTo(&d)).To(specs.BeNil())
			ctx.Expect(d.GetSeconds()).ToEqual(int64(8))
		})
	})
}
