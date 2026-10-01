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

package command_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/getsyntegrity/ego/command"
)

func mustMetadata(ctx *specs.Context) command.Metadata {
	op := mustOperationID(ctx, "op-1")
	md, err := command.NewMetadata(op)
	ctx.Expect(err).To(specs.BeNil())
	return md
}

// equalProto matches a proto.Message that proto.Equal reports equal to want. ToEqual would compare the
// internal state of the generated struct, and its failure would print that state instead of the fields.
func equalProto(want proto.Message) specs.Matcher {
	return specs.Satisfy("proto-equal to "+prototext.Format(want), func(a any) bool {
		got, ok := a.(proto.Message)
		return ok && proto.Equal(want, got)
	})
}

func TestOutcomeZeroValueInvalid(t *testing.T) {
	specs.Describe(t, "the zero Outcome is not a valid outcome", func(s *specs.Spec) {
		s.It("renders as unknown", func(ctx *specs.Context) {
			var zero command.Outcome
			ctx.Expect(zero.String()).ToEqual("unknown")
		})
	})
}

func TestOutcomeStringPerKind(t *testing.T) {
	specs.Describe(t, "Outcome.String names each outcome kind", func(s *specs.Spec) {
		s.It("renders every kind with its canonical name", func(ctx *specs.Context) {
			cases := map[command.Outcome]string{
				command.OutcomeSuccess:        "success",
				command.OutcomeSuccessNoState: "success_no_state",
				command.OutcomeRejected:       "rejected",
				command.OutcomeFailed:         "failed",
				command.OutcomeTimedOut:       "timed_out",
				command.OutcomeCanceled:       "canceled",
			}
			for outcome, want := range cases {
				ctx.Expect(outcome.String()).ToEqual(want)
			}
		})
	})
}

func TestNewRejectedConcurrencyConflictCodeCheckableWithoutStringInspection(t *testing.T) {
	specs.Describe(t, "a rejected result carries a concurrency conflict code checkable without string inspection", func(s *specs.Spec) {
		s.It("exposes CodeConcurrencyConflict on the failure", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			f, err := command.NewFailure("expected revision mismatch", command.WithFailureCode(command.CodeConcurrencyConflict))
			ctx.Expect(err).To(specs.BeNil())

			r, err := command.NewRejected(md, f)
			ctx.Expect(err).To(specs.BeNil())

			failure, ok := r.Failure()
			ctx.Expect(ok).To(specs.BeTrue())
			code, ok := failure.Code()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(code).ToEqual(command.CodeConcurrencyConflict)
		})
	})
}

func TestOutcomeKindsMutuallyExclusive(t *testing.T) {
	specs.Describe(t, "the outcome kinds are mutually exclusive", func(s *specs.Spec) {
		kinds := []command.Outcome{
			command.OutcomeSuccess, command.OutcomeSuccessNoState, command.OutcomeRejected,
			command.OutcomeFailed, command.OutcomeTimedOut, command.OutcomeCanceled,
		}

		// One case per kind, named by its string, so a duplicated value says which kind collided.
		specs.Table(s, kinds, command.Outcome.String, func(ctx *specs.Context, k command.Outcome) {
			ctx.Expect(kinds).To(specs.ExactlyNElements(1, specs.Equal(k)))
		})

		s.It("all kinds together are six distinct values", func(ctx *specs.Context) {
			seen := map[command.Outcome]struct{}{}
			for _, k := range kinds {
				seen[k] = struct{}{}
			}
			ctx.Expect(seen).To(specs.HaveLen(6))
		})
	})
}

func TestNewSuccessRequiresState(t *testing.T) {
	specs.Describe(t, "NewSuccess requires a state", func(s *specs.Spec) {
		s.It("fails with ErrInvalidResult for a nil state", func(ctx *specs.Context) {
			md := mustMetadata(ctx)

			_, err := command.NewSuccess(md, nil, 1)
			ctx.Expect(err).To(specs.MatchError(command.ErrInvalidResult))
		})
	})
}

func TestNewSuccessWithState(t *testing.T) {
	specs.Describe(t, "NewSuccess builds a success result carrying state", func(s *specs.Spec) {
		s.It("exposes the outcome, revision and state, with no error", func(ctx *specs.Context) {
			md := mustMetadata(ctx)
			state := timestamppb.New(time.Unix(1, 0))

			r, err := command.NewSuccess(md, state, 7)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(r.Outcome()).ToEqual(command.OutcomeSuccess)
			ctx.Expect(r.Revision()).ToEqual(uint64(7))

			got, ok := r.State()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(got).To(equalProto(state))

			ctx.Expect(r.Err()).To(specs.BeNil())
		})
	})
}

func TestNewSuccessNoState(t *testing.T) {
	specs.Describe(t, "NewSuccessNoState builds a success result without state", func(s *specs.Spec) {
		s.It("has the success-no-state outcome, no state and no error", func(ctx *specs.Context) {
			md := mustMetadata(ctx)

			r, err := command.NewSuccessNoState(md)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(r.Outcome()).ToEqual(command.OutcomeSuccessNoState)

			_, ok := r.State()
			ctx.Expect(ok).To(specs.BeFalse())
			ctx.Expect(r.Err()).To(specs.BeNil())
		})
	})
}

func TestNewRejected(t *testing.T) {
	specs.Describe(t, "NewRejected builds a rejected result", func(s *specs.Spec) {
		s.It("exposes the outcome and failure and classifies as ErrRejected", func(ctx *specs.Context) {
			md := mustMetadata(ctx)
			f, err := command.NewFailure("domain rejected")
			ctx.Expect(err).To(specs.BeNil())

			r, err := command.NewRejected(md, f)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(r.Outcome()).ToEqual(command.OutcomeRejected)

			got, ok := r.Failure()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(got).ToEqual(f)

			ctx.Expect(r.Err()).To(specs.MatchError(command.ErrRejected))
		})
	})
}

func TestNewFailed(t *testing.T) {
	specs.Describe(t, "NewFailed builds a failed result", func(s *specs.Spec) {
		s.It("classifies as ErrFailed and wraps the failure cause", func(ctx *specs.Context) {
			md := mustMetadata(ctx)
			cause := errors.New("boom")
			f, err := command.NewFailure("runtime failure", command.WithFailureCause(cause))
			ctx.Expect(err).To(specs.BeNil())

			r, err := command.NewFailed(md, f)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(r.Outcome()).ToEqual(command.OutcomeFailed)
			ctx.Expect(r.Err()).To(specs.MatchError(command.ErrFailed))
			ctx.Expect(r.Err()).To(specs.MatchError(cause))
		})
	})
}

func TestNewTimedOutDefaultCause(t *testing.T) {
	specs.Describe(t, "NewTimedOut defaults its cause to context.DeadlineExceeded", func(s *specs.Spec) {
		s.It("classifies as ErrTimedOut and wraps the deadline error", func(ctx *specs.Context) {
			md := mustMetadata(ctx)
			f, err := command.NewFailure("deadline exceeded")
			ctx.Expect(err).To(specs.BeNil())

			r, err := command.NewTimedOut(md, f)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(r.Outcome()).ToEqual(command.OutcomeTimedOut)
			ctx.Expect(r.Err()).To(specs.MatchError(command.ErrTimedOut))
			ctx.Expect(r.Err()).To(specs.MatchError(context.DeadlineExceeded))
		})
	})
}

func TestNewCanceledDefaultCause(t *testing.T) {
	specs.Describe(t, "NewCanceled defaults its cause to context.Canceled", func(s *specs.Spec) {
		s.It("classifies as ErrCanceled and wraps the cancellation error", func(ctx *specs.Context) {
			md := mustMetadata(ctx)
			f, err := command.NewFailure("canceled")
			ctx.Expect(err).To(specs.BeNil())

			r, err := command.NewCanceled(md, f)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(r.Outcome()).ToEqual(command.OutcomeCanceled)
			ctx.Expect(r.Err()).To(specs.MatchError(command.ErrCanceled))
			ctx.Expect(r.Err()).To(specs.MatchError(context.Canceled))
		})
	})
}

func TestFailureWithCode(t *testing.T) {
	specs.Describe(t, "NewFailure honors WithFailureCode", func(s *specs.Spec) {
		s.It("exposes the code and the message", func(ctx *specs.Context) {
			f, err := command.NewFailure("bad input", command.WithFailureCode("INVALID_ARGUMENT"))
			ctx.Expect(err).To(specs.BeNil())

			code, ok := f.Code()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(code).ToEqual("INVALID_ARGUMENT")
			ctx.Expect(f.Message()).ToEqual("bad input")
		})
	})
}

func TestNewFailureRequiresMessage(t *testing.T) {
	specs.Describe(t, "NewFailure requires a message", func(s *specs.Spec) {
		s.It("fails with ErrInvalidResult for an empty message", func(ctx *specs.Context) {
			_, err := command.NewFailure("")
			ctx.Expect(err).To(specs.MatchError(command.ErrInvalidResult))
		})
	})
}

func TestResultErrAsCommandError(t *testing.T) {
	specs.Describe(t, "Result.Err is recoverable as a command Error", func(s *specs.Spec) {
		s.It("yields the command Error that still matches ErrRejected", func(ctx *specs.Context) {
			md := mustMetadata(ctx)
			f, err := command.NewFailure("domain rejected")
			ctx.Expect(err).To(specs.BeNil())

			r, err := command.NewRejected(md, f)
			ctx.Expect(err).To(specs.BeNil())

			var cmdErr *command.Error
			ctx.Expect(r.Err()).To(specs.MatchErrorAs(&cmdErr))
			ctx.Expect(cmdErr).To(specs.MatchError(command.ErrRejected))
		})
	})
}

func TestStateAsTypedExtraction(t *testing.T) {
	specs.Describe(t, "StateAs extracts the state as a concrete type", func(s *specs.Spec) {
		s.It("returns the state for its own type", func(ctx *specs.Context) {
			md := mustMetadata(ctx)
			state := timestamppb.New(time.Unix(42, 0))

			r, err := command.NewSuccess(md, state, 1)
			ctx.Expect(err).To(specs.BeNil())

			got, ok := command.StateAs[*timestamppb.Timestamp](r)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(got.GetSeconds()).ToEqual(int64(42))
		})
	})
}

func TestStateAsFalseWhenNoState(t *testing.T) {
	specs.Describe(t, "StateAs reports absence when the result has no state", func(s *specs.Spec) {
		s.It("returns false for a success without state", func(ctx *specs.Context) {
			md := mustMetadata(ctx)

			r, err := command.NewSuccessNoState(md)
			ctx.Expect(err).To(specs.BeNil())

			_, ok := command.StateAs[*timestamppb.Timestamp](r)
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}
