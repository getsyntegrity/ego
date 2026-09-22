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
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/pablogore/ego/v4/command"
	"github.com/pablogore/ego/v4/persistence"
)

func TestClassifierRegistrySentinelsDoNotPrefixEachOther(t *testing.T) {
	for i, a := range classifierRegistry {
		for j, b := range classifierRegistry {
			if i == j {
				continue
			}
			if strings.HasPrefix(a.sentinel, b.sentinel) {
				t.Fatalf("classifierRegistry[%d].sentinel %q has classifierRegistry[%d].sentinel %q as a prefix; match order would be ambiguous", i, a.sentinel, j, b.sentinel)
			}
		}
	}
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
	md := newTestMetadata(t)
	message := errActorContextCanceled.Error() + ": dispatchToBehavior"

	result, err := classifyErrorReply(md, message)
	if err != nil {
		t.Fatalf("classifyErrorReply: %v", err)
	}
	if result.Outcome() != command.OutcomeCanceled {
		t.Fatalf("got outcome %s, want OutcomeCanceled", result.Outcome())
	}
	if !errors.Is(result.Err(), command.ErrCanceled) {
		t.Fatalf("result.Err() does not classify as command.ErrCanceled")
	}
}

func TestClassifyErrorReplyDeadlineExceeded(t *testing.T) {
	md := newTestMetadata(t)
	message := errActorDeadlineExceeded.Error() + ": dispatchToBehavior"

	result, err := classifyErrorReply(md, message)
	if err != nil {
		t.Fatalf("classifyErrorReply: %v", err)
	}
	if result.Outcome() != command.OutcomeTimedOut {
		t.Fatalf("got outcome %s, want OutcomeTimedOut", result.Outcome())
	}
	if !errors.Is(result.Err(), command.ErrTimedOut) {
		t.Fatalf("result.Err() does not classify as command.ErrTimedOut")
	}
}

func TestClassifyErrorReplyConcurrencyConflict(t *testing.T) {
	md := newTestMetadata(t)
	conflictErr := persistence.NewConflictError(persistence.Unscoped(), "entity-1", persistence.ExpectRevision(3), persistence.WithActualRevision(5))
	message := conflictErr.Error()

	result, err := classifyErrorReply(md, message)
	if err != nil {
		t.Fatalf("classifyErrorReply: %v", err)
	}
	if result.Outcome() != command.OutcomeRejected {
		t.Fatalf("got outcome %s, want OutcomeRejected", result.Outcome())
	}
	failure, ok := result.Failure()
	if !ok {
		t.Fatalf("result.Failure() ok = false, want true")
	}
	code, hasCode := failure.Code()
	if !hasCode || code != command.CodeConcurrencyConflict {
		t.Fatalf("got code %q (hasCode=%v), want %q", code, hasCode, command.CodeConcurrencyConflict)
	}
	if !errors.Is(result.Err(), command.ErrRejected) {
		t.Fatalf("result.Err() does not classify as command.ErrRejected")
	}

	var recovered *persistence.ConflictError
	if !errors.As(result.Err(), &recovered) {
		t.Fatalf("errors.As(result.Err(), &recovered) = false, want true")
	}
	if recovered.PersistenceID() != "entity-1" {
		t.Fatalf("got recovered persistence id %q, want %q", recovered.PersistenceID(), "entity-1")
	}
	expectedRevision, ok := recovered.Expected().Revision()
	if !ok || expectedRevision != 3 {
		t.Fatalf("got recovered expected revision %d (ok=%v), want 3", expectedRevision, ok)
	}
	actualRevision, ok := recovered.ActualRevision()
	if !ok || actualRevision != 5 {
		t.Fatalf("got recovered actual revision %d (ok=%v), want 5", actualRevision, ok)
	}
}

// TestClassifyErrorReplyWrappedConflictDegradesToFailed is task 5.3's test
// for design.md D7's unwrapped-error invariant: "a *ConflictError MUST
// reach sendErrorReply unwrapped -- no fmt.Errorf("...: %w", err) prefix --
// or classification degrades to OutcomeFailed. [...] A store whose error
// does not conform simply does not classify: the caller sees today's
// OutcomeFailed, never a wrong conflict." This documents the invariant
// rather than weakening it: classifyErrorReply matches by
// strings.HasPrefix against the message, so a non-empty prefix in front of
// the *ConflictError's own Error() text makes the sentinel match fail and
// the message fall through to the registry's default (OutcomeFailed), not
// to OutcomeRejected/concurrency_conflict.
func TestClassifyErrorReplyWrappedConflictDegradesToFailed(t *testing.T) {
	md := newTestMetadata(t)
	conflictErr := persistence.NewConflictError(persistence.Unscoped(), "entity-1", persistence.ExpectRevision(3), persistence.WithActualRevision(5))

	t.Run("unwrapped conflict classifies as concurrency_conflict", func(t *testing.T) {
		result, err := classifyErrorReply(md, conflictErr.Error())
		if err != nil {
			t.Fatalf("classifyErrorReply: %v", err)
		}
		if result.Outcome() != command.OutcomeRejected {
			t.Fatalf("got outcome %s, want OutcomeRejected", result.Outcome())
		}
		failure, ok := result.Failure()
		if !ok {
			t.Fatalf("result.Failure() ok = false, want true")
		}
		code, hasCode := failure.Code()
		if !hasCode || code != command.CodeConcurrencyConflict {
			t.Fatalf("got code %q (hasCode=%v), want %q", code, hasCode, command.CodeConcurrencyConflict)
		}
	})

	t.Run("prefix-wrapped conflict degrades to OutcomeFailed, not OutcomeRejected", func(t *testing.T) {
		wrapped := fmt.Errorf("actor: %w", conflictErr)
		message := wrapped.Error()

		result, err := classifyErrorReply(md, message)
		if err != nil {
			t.Fatalf("classifyErrorReply: %v", err)
		}
		if result.Outcome() != command.OutcomeFailed {
			t.Fatalf("got outcome %s, want OutcomeFailed (D7: a wrapped ConflictError must not classify as concurrency_conflict)", result.Outcome())
		}
		if !errors.Is(result.Err(), command.ErrFailed) {
			t.Fatalf("result.Err() does not classify as command.ErrFailed")
		}

		failure, ok := result.Failure()
		if !ok {
			t.Fatalf("result.Failure() ok = false, want true")
		}
		if code, hasCode := failure.Code(); hasCode {
			if code == command.CodeConcurrencyConflict {
				t.Fatalf("a prefix-wrapped ConflictError must never classify as %q", command.CodeConcurrencyConflict)
			}
		}
	})
}

func TestClassifyErrorReplyDefaultsToFailed(t *testing.T) {
	md := newTestMetadata(t)
	message := "some unrelated application error"

	result, err := classifyErrorReply(md, message)
	if err != nil {
		t.Fatalf("classifyErrorReply: %v", err)
	}
	if result.Outcome() != command.OutcomeFailed {
		t.Fatalf("got outcome %s, want OutcomeFailed", result.Outcome())
	}
	if !errors.Is(result.Err(), command.ErrFailed) {
		t.Fatalf("result.Err() does not classify as command.ErrFailed")
	}
}
