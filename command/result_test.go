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

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/pablogore/ego/v4/command"
)

func mustMetadata(t *testing.T) command.Metadata {
	t.Helper()
	op := mustOperationID(t, "op-1")
	md, err := command.NewMetadata(op)
	require.NoError(t, err)
	return md
}

func TestOutcomeZeroValueInvalid(t *testing.T) {
	var zero command.Outcome
	require.Equal(t, "unknown", zero.String())
}

func TestOutcomeStringPerKind(t *testing.T) {
	cases := map[command.Outcome]string{
		command.OutcomeSuccess:        "success",
		command.OutcomeSuccessNoState: "success_no_state",
		command.OutcomeRejected:       "rejected",
		command.OutcomeFailed:         "failed",
		command.OutcomeTimedOut:       "timed_out",
		command.OutcomeCanceled:       "canceled",
	}
	for outcome, want := range cases {
		require.Equal(t, want, outcome.String())
	}
}

func TestNewRejectedConcurrencyConflictCodeCheckableWithoutStringInspection(t *testing.T) {
	op := mustOperationID(t, "op-1")
	md, err := command.NewMetadata(op)
	require.NoError(t, err)

	f, err := command.NewFailure("expected revision mismatch", command.WithFailureCode(command.CodeConcurrencyConflict))
	require.NoError(t, err)

	r, err := command.NewRejected(md, f)
	require.NoError(t, err)

	failure, ok := r.Failure()
	require.True(t, ok)
	code, ok := failure.Code()
	require.True(t, ok)
	require.Equal(t, command.CodeConcurrencyConflict, code)
}

func TestOutcomeKindsMutuallyExclusive(t *testing.T) {
	kinds := []command.Outcome{
		command.OutcomeSuccess, command.OutcomeSuccessNoState, command.OutcomeRejected,
		command.OutcomeFailed, command.OutcomeTimedOut, command.OutcomeCanceled,
	}
	seen := map[command.Outcome]bool{}
	for _, k := range kinds {
		require.False(t, seen[k], "duplicate outcome value %v", k)
		seen[k] = true
	}
	require.Len(t, seen, 6)
}

func TestNewSuccessRequiresState(t *testing.T) {
	md := mustMetadata(t)

	_, err := command.NewSuccess(md, nil, 1)
	require.ErrorIs(t, err, command.ErrInvalidResult)
}

func TestNewSuccessWithState(t *testing.T) {
	md := mustMetadata(t)
	state := timestamppb.New(time.Unix(1, 0))

	r, err := command.NewSuccess(md, state, 7)
	require.NoError(t, err)
	require.Equal(t, command.OutcomeSuccess, r.Outcome())
	require.Equal(t, uint64(7), r.Revision())

	got, ok := r.State()
	require.True(t, ok)
	require.Equal(t, state, got)

	require.NoError(t, r.Err())
}

func TestNewSuccessNoState(t *testing.T) {
	md := mustMetadata(t)

	r, err := command.NewSuccessNoState(md)
	require.NoError(t, err)
	require.Equal(t, command.OutcomeSuccessNoState, r.Outcome())

	_, ok := r.State()
	require.False(t, ok)
	require.NoError(t, r.Err())
}

func TestNewRejected(t *testing.T) {
	md := mustMetadata(t)
	f, err := command.NewFailure("domain rejected")
	require.NoError(t, err)

	r, err := command.NewRejected(md, f)
	require.NoError(t, err)
	require.Equal(t, command.OutcomeRejected, r.Outcome())

	got, ok := r.Failure()
	require.True(t, ok)
	require.Equal(t, f, got)

	require.ErrorIs(t, r.Err(), command.ErrRejected)
}

func TestNewFailed(t *testing.T) {
	md := mustMetadata(t)
	cause := errors.New("boom")
	f, err := command.NewFailure("runtime failure", command.WithFailureCause(cause))
	require.NoError(t, err)

	r, err := command.NewFailed(md, f)
	require.NoError(t, err)
	require.Equal(t, command.OutcomeFailed, r.Outcome())
	require.ErrorIs(t, r.Err(), command.ErrFailed)
	require.ErrorIs(t, r.Err(), cause)
}

func TestNewTimedOutDefaultCause(t *testing.T) {
	md := mustMetadata(t)
	f, err := command.NewFailure("deadline exceeded")
	require.NoError(t, err)

	r, err := command.NewTimedOut(md, f)
	require.NoError(t, err)
	require.Equal(t, command.OutcomeTimedOut, r.Outcome())
	require.ErrorIs(t, r.Err(), command.ErrTimedOut)
	require.ErrorIs(t, r.Err(), context.DeadlineExceeded)
}

func TestNewCanceledDefaultCause(t *testing.T) {
	md := mustMetadata(t)
	f, err := command.NewFailure("canceled")
	require.NoError(t, err)

	r, err := command.NewCanceled(md, f)
	require.NoError(t, err)
	require.Equal(t, command.OutcomeCanceled, r.Outcome())
	require.ErrorIs(t, r.Err(), command.ErrCanceled)
	require.ErrorIs(t, r.Err(), context.Canceled)
}

func TestFailureWithCode(t *testing.T) {
	f, err := command.NewFailure("bad input", command.WithFailureCode("INVALID_ARGUMENT"))
	require.NoError(t, err)

	code, ok := f.Code()
	require.True(t, ok)
	require.Equal(t, "INVALID_ARGUMENT", code)
	require.Equal(t, "bad input", f.Message())
}

func TestNewFailureRequiresMessage(t *testing.T) {
	_, err := command.NewFailure("")
	require.ErrorIs(t, err, command.ErrInvalidResult)
}

func TestResultErrAsCommandError(t *testing.T) {
	md := mustMetadata(t)
	f, err := command.NewFailure("domain rejected")
	require.NoError(t, err)

	r, err := command.NewRejected(md, f)
	require.NoError(t, err)

	var cmdErr *command.Error
	require.ErrorAs(t, r.Err(), &cmdErr)
	require.ErrorIs(t, cmdErr, command.ErrRejected)
}

func TestStateAsTypedExtraction(t *testing.T) {
	md := mustMetadata(t)
	state := timestamppb.New(time.Unix(42, 0))

	r, err := command.NewSuccess(md, state, 1)
	require.NoError(t, err)

	got, ok := command.StateAs[*timestamppb.Timestamp](r)
	require.True(t, ok)
	require.Equal(t, int64(42), got.GetSeconds())
}

func TestStateAsFalseWhenNoState(t *testing.T) {
	md := mustMetadata(t)

	r, err := command.NewSuccessNoState(md)
	require.NoError(t, err)

	_, ok := command.StateAs[*timestamppb.Timestamp](r)
	require.False(t, ok)
}
