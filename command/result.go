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

package command

import (
	"context"

	"google.golang.org/protobuf/proto"
)

// Outcome discriminates a Result between its six mutually exclusive kinds
// (D5, AC8). The zero value is intentionally invalid — mirroring
// tenancy.Reason — so a Result can only report a kind it was actually
// constructed with.
type Outcome uint8

const (
	_ Outcome = iota
	// OutcomeSuccess is a successful command carrying a resulting state.
	OutcomeSuccess
	// OutcomeSuccessNoState is a successful command with no resulting
	// state to report.
	OutcomeSuccessNoState
	// OutcomeRejected is a domain rejection — a business rule failure,
	// not an infrastructure fault.
	OutcomeRejected
	// OutcomeFailed is an application or runtime failure while
	// processing the command.
	OutcomeFailed
	// OutcomeTimedOut is a command that did not complete before its
	// deadline.
	OutcomeTimedOut
	// OutcomeCanceled is a command whose context was canceled before
	// completion.
	OutcomeCanceled
)

// String returns a lower-case, human-readable name for the outcome. The
// zero value and any other unrecognized value report "unknown".
func (o Outcome) String() string {
	switch o {
	case OutcomeSuccess:
		return "success"
	case OutcomeSuccessNoState:
		return "success_no_state"
	case OutcomeRejected:
		return "rejected"
	case OutcomeFailed:
		return "failed"
	case OutcomeTimedOut:
		return "timed_out"
	case OutcomeCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

// CodeConcurrencyConflict is the canonical, framework-emitted Failure code
// (WRITE-004, AC5) for a write precondition that was not satisfied at
// commit, carried via WithFailureCode + NewRejected. It introduces no new
// Outcome kind — the six-kind taxonomy is unchanged.
const CodeConcurrencyConflict = "concurrency_conflict"

// Failure carries the detail of a non-success Result: a required
// human-readable message, an optional caller-defined code, and an
// optional underlying cause.
type Failure struct {
	message string
	code    string
	hasCode bool
	cause   error
}

// FailureOption configures optional Failure fields.
type FailureOption func(*Failure)

// WithFailureCode sets an optional, caller-defined classification code
// (e.g. "INVALID_ARGUMENT").
func WithFailureCode(code string) FailureOption {
	return func(f *Failure) {
		f.code = code
		f.hasCode = true
	}
}

// WithFailureCause attaches an optional underlying error, traversable via
// errors.Unwrap once the Failure backs a Result's Err().
func WithFailureCause(err error) FailureOption {
	return func(f *Failure) {
		f.cause = err
	}
}

// NewFailure builds a Failure. message is required (ErrInvalidResult
// otherwise).
func NewFailure(message string, opts ...FailureOption) (Failure, error) {
	if message == "" {
		return Failure{}, NewError(ErrInvalidResult, "command: failure message must not be empty", nil)
	}
	f := Failure{message: message}
	for _, opt := range opts {
		opt(&f)
	}
	return f, nil
}

// Message returns f's human-readable message.
func (f Failure) Message() string {
	return f.message
}

// Code returns f's optional caller-defined classification code, if set.
func (f Failure) Code() (string, bool) {
	return f.code, f.hasCode
}

// Result is the canonical outcome of a dispatched command: exactly one of
// six mutually exclusive kinds (D5, AC8), each identifiable at the type
// level via Outcome() rather than by inspecting an error string.
type Result struct {
	metadata   Metadata
	outcome    Outcome
	state      proto.Message
	revision   uint64
	failure    Failure
	hasFailure bool
}

// NewSuccess builds a successful Result carrying state and revision.
// state must be non-nil (ErrInvalidResult otherwise) — use
// NewSuccessNoState when there is no state to report.
func NewSuccess(md Metadata, state proto.Message, revision uint64) (Result, error) {
	if state == nil {
		return Result{}, NewError(ErrInvalidResult, "command: success result state must not be nil", nil)
	}
	return Result{metadata: md, outcome: OutcomeSuccess, state: state, revision: revision}, nil
}

// NewSuccessNoState builds a successful Result with no resulting state.
func NewSuccessNoState(md Metadata) (Result, error) {
	return Result{metadata: md, outcome: OutcomeSuccessNoState}, nil
}

// NewRejected builds a Result reporting a domain rejection.
func NewRejected(md Metadata, f Failure) (Result, error) {
	return Result{metadata: md, outcome: OutcomeRejected, failure: f, hasFailure: true}, nil
}

// NewFailed builds a Result reporting an application or runtime failure.
func NewFailed(md Metadata, f Failure) (Result, error) {
	return Result{metadata: md, outcome: OutcomeFailed, failure: f, hasFailure: true}, nil
}

// NewTimedOut builds a Result reporting a deadline/timeout. A nil f.cause
// defaults to context.DeadlineExceeded, so stdlib classification against
// the returned Result's Err() works unchanged.
func NewTimedOut(md Metadata, f Failure) (Result, error) {
	if f.cause == nil {
		f.cause = context.DeadlineExceeded
	}
	return Result{metadata: md, outcome: OutcomeTimedOut, failure: f, hasFailure: true}, nil
}

// NewCanceled builds a Result reporting a cancellation. A nil f.cause
// defaults to context.Canceled, so stdlib classification against the
// returned Result's Err() works unchanged.
func NewCanceled(md Metadata, f Failure) (Result, error) {
	if f.cause == nil {
		f.cause = context.Canceled
	}
	return Result{metadata: md, outcome: OutcomeCanceled, failure: f, hasFailure: true}, nil
}

// Outcome reports which of the six mutually exclusive kinds r is.
func (r Result) Outcome() Outcome {
	return r.outcome
}

// Metadata returns r's canonical metadata.
func (r Result) Metadata() Metadata {
	return r.metadata
}

// State returns r's resulting state, if any. The second return value is
// false for every kind other than OutcomeSuccess.
func (r Result) State() (proto.Message, bool) {
	if r.outcome != OutcomeSuccess {
		return nil, false
	}
	return r.state, true
}

// Revision returns r's resulting revision, meaningful only for
// OutcomeSuccess.
func (r Result) Revision() uint64 {
	return r.revision
}

// Failure returns r's failure detail, if any. The second return value is
// false for either success kind.
func (r Result) Failure() (Failure, bool) {
	return r.failure, r.hasFailure
}

// Err returns nil for either success kind, and a *Error classifiable via
// errors.Is against ErrRejected/ErrFailed/ErrTimedOut/ErrCanceled
// otherwise.
func (r Result) Err() error {
	var sentinel error
	switch r.outcome {
	case OutcomeSuccess, OutcomeSuccessNoState:
		return nil
	case OutcomeRejected:
		sentinel = ErrRejected
	case OutcomeFailed:
		sentinel = ErrFailed
	case OutcomeTimedOut:
		sentinel = ErrTimedOut
	case OutcomeCanceled:
		sentinel = ErrCanceled
	default:
		return nil
	}
	return newOutcomeError(sentinel, r.outcome, r.failure)
}

// StateAs recovers r's resulting state as concrete type T. The second
// return value is false when r carries no state, or the state is not of
// type T.
func StateAs[T proto.Message](r Result) (T, bool) {
	var zero T
	state, ok := r.State()
	if !ok {
		return zero, false
	}
	v, ok := state.(T)
	return v, ok
}
