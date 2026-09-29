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

package protocol

import (
	"fmt"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/command"
	"github.com/getsyntegrity/ego/persistence"
)

func TestClassifierRegistrySentinelsDoNotPrefixEachOther(t *testing.T) {
	specs.Describe(t, "the classifier registry sentinels are unambiguous prefixes", func(s *specs.Spec) {
		s.It("no sentinel has another sentinel as a prefix", func(ctx *specs.Context) {
			var offenders []string
			for i, a := range classifierRegistry {
				for j, b := range classifierRegistry {
					if i == j {
						continue
					}
					if strings.HasPrefix(a.sentinel, b.sentinel) {
						offenders = append(offenders, fmt.Sprintf("classifierRegistry[%d].sentinel %q has classifierRegistry[%d].sentinel %q as a prefix; match order would be ambiguous", i, a.sentinel, j, b.sentinel))
					}
				}
			}
			ctx.Expect(offenders).To(specs.BeNil())
		})
	})
}

func newTestMetadata(t *testing.T) command.Metadata {
	t.Helper()
	md, err := command.NewMetadata("op-1")
	if err != nil {
		t.Fatalf("command.NewMetadata: %v", err)
	}
	return md
}

func TestClassifyErrorReplyContextCanceled(t *testing.T) {
	specs.Describe(t, "ClassifyErrorReply classifies an actor context cancellation as canceled", func(s *specs.Spec) {
		s.It("returns OutcomeCanceled wrapping command.ErrCanceled", func(ctx *specs.Context) {
			md := newTestMetadata(ctx.T)
			message := errActorContextCanceled.Error() + ": dispatchToBehavior"

			result, err := ClassifyErrorReply(md, message)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(result.Outcome()).ToEqual(command.OutcomeCanceled)
			ctx.Expect(result.Err()).To(specs.MatchError(command.ErrCanceled))
		})
	})
}

func TestClassifyErrorReplyDeadlineExceeded(t *testing.T) {
	specs.Describe(t, "ClassifyErrorReply classifies an actor deadline as timed out", func(s *specs.Spec) {
		s.It("returns OutcomeTimedOut wrapping command.ErrTimedOut", func(ctx *specs.Context) {
			md := newTestMetadata(ctx.T)
			message := errActorDeadlineExceeded.Error() + ": dispatchToBehavior"

			result, err := ClassifyErrorReply(md, message)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(result.Outcome()).ToEqual(command.OutcomeTimedOut)
			ctx.Expect(result.Err()).To(specs.MatchError(command.ErrTimedOut))
		})
	})
}

func TestClassifyErrorReplyConcurrencyConflict(t *testing.T) {
	specs.Describe(t, "ClassifyErrorReply classifies a persistence conflict as a rejected concurrency conflict", func(s *specs.Spec) {
		s.It("returns OutcomeRejected with the conflict code and a recoverable ConflictError", func(ctx *specs.Context) {
			md := newTestMetadata(ctx.T)
			conflictErr := persistence.NewConflictError(persistence.Unscoped(), "entity-1", persistence.ExpectRevision(3), persistence.WithActualRevision(5))
			message := conflictErr.Error()

			result, err := ClassifyErrorReply(md, message)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(result.Outcome()).ToEqual(command.OutcomeRejected)
			failure, ok := result.Failure()
			ctx.Expect(ok).To(specs.BeTrue())
			code, hasCode := failure.Code()
			ctx.Expect(hasCode).To(specs.BeTrue())
			ctx.Expect(code).ToEqual(command.CodeConcurrencyConflict)
			ctx.Expect(result.Err()).To(specs.MatchError(command.ErrRejected))

			var recovered *persistence.ConflictError
			ctx.Expect(result.Err()).To(specs.MatchErrorAs(&recovered))
			ctx.Expect(recovered.PersistenceID()).ToEqual("entity-1")
			expectedRevision, ok := recovered.Expected().Revision()
			ctx.Expect(ok).To(specs.BeTrue())
			specs.ExpectT(ctx, expectedRevision).ToEqual(3)
			actualRevision, ok := recovered.ActualRevision()
			ctx.Expect(ok).To(specs.BeTrue())
			specs.ExpectT(ctx, actualRevision).ToEqual(5)
		})
	})
}

// TestClassifyErrorReplyWrappedConflictDegradesToFailed is task 5.3's test
// for design.md D7's unwrapped-error invariant: "a *ConflictError MUST
// reach sendErrorReply unwrapped -- no fmt.Errorf("...: %w", err) prefix --
// or classification degrades to OutcomeFailed. [...] A store whose error
// does not conform simply does not classify: the caller sees today's
// OutcomeFailed, never a wrong conflict." This documents the invariant
// rather than weakening it: ClassifyErrorReply matches by
// strings.HasPrefix against the message, so a non-empty prefix in front of
// the *ConflictError's own Error() text makes the sentinel match fail and
// the message fall through to the registry's default (OutcomeFailed), not
// to OutcomeRejected/concurrency_conflict.
func TestClassifyErrorReplyWrappedConflictDegradesToFailed(t *testing.T) {
	specs.Describe(t, "ClassifyErrorReply only classifies an unwrapped conflict as a concurrency conflict", func(s *specs.Spec) {
		conflictErr := persistence.NewConflictError(persistence.Unscoped(), "entity-1", persistence.ExpectRevision(3), persistence.WithActualRevision(5))

		s.It("unwrapped conflict classifies as concurrency_conflict", func(ctx *specs.Context) {
			md := newTestMetadata(ctx.T)

			result, err := ClassifyErrorReply(md, conflictErr.Error())
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(result.Outcome()).ToEqual(command.OutcomeRejected)
			failure, ok := result.Failure()
			ctx.Expect(ok).To(specs.BeTrue())
			code, hasCode := failure.Code()
			ctx.Expect(hasCode).To(specs.BeTrue())
			ctx.Expect(code).ToEqual(command.CodeConcurrencyConflict)
		})

		s.It("prefix-wrapped conflict degrades to OutcomeFailed, not OutcomeRejected", func(ctx *specs.Context) {
			md := newTestMetadata(ctx.T)
			wrapped := fmt.Errorf("actor: %w", conflictErr)
			message := wrapped.Error()

			// D7: a wrapped ConflictError must not classify as concurrency_conflict.
			result, err := ClassifyErrorReply(md, message)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(result.Outcome()).ToEqual(command.OutcomeFailed)
			ctx.Expect(result.Err()).To(specs.MatchError(command.ErrFailed))

			failure, ok := result.Failure()
			ctx.Expect(ok).To(specs.BeTrue())
			// A prefix-wrapped ConflictError must never classify as concurrency_conflict.
			code, _ := failure.Code()
			ctx.Expect(code).To(specs.NotEqual(command.CodeConcurrencyConflict))
		})
	})
}

func TestClassifyErrorReplyDefaultsToFailed(t *testing.T) {
	specs.Describe(t, "ClassifyErrorReply classifies an unrecognized message as failed", func(s *specs.Spec) {
		s.It("returns OutcomeFailed wrapping command.ErrFailed", func(ctx *specs.Context) {
			md := newTestMetadata(ctx.T)
			message := "some unrelated application error"

			result, err := ClassifyErrorReply(md, message)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(result.Outcome()).ToEqual(command.OutcomeFailed)
			ctx.Expect(result.Err()).To(specs.MatchError(command.ErrFailed))
		})
	})
}
