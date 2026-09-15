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
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pablogore/ego/v4/command"
)

func TestErrorClassification(t *testing.T) {
	cases := []struct {
		name     string
		sentinel error
		other    []error
	}{
		{
			name:     "rejected",
			sentinel: command.ErrRejected,
			other:    []error{command.ErrFailed, command.ErrTimedOut, command.ErrCanceled},
		},
		{
			name:     "failed",
			sentinel: command.ErrFailed,
			other:    []error{command.ErrRejected, command.ErrTimedOut, command.ErrCanceled},
		},
		{
			name:     "timed out",
			sentinel: command.ErrTimedOut,
			other:    []error{command.ErrRejected, command.ErrFailed, command.ErrCanceled},
		},
		{
			name:     "canceled",
			sentinel: command.ErrCanceled,
			other:    []error{command.ErrRejected, command.ErrFailed, command.ErrTimedOut},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := command.NewError(tc.sentinel, "boom", nil)
			require.Error(t, err)
			require.ErrorIs(t, err, tc.sentinel)
			for _, o := range tc.other {
				require.NotErrorIs(t, err, o)
			}
		})
	}
}

func TestErrorUnwrap(t *testing.T) {
	cause := errors.New("underlying cause")
	err := command.NewError(command.ErrFailed, "wrapped", cause)

	require.ErrorIs(t, err, command.ErrFailed)
	require.ErrorIs(t, err, cause)
	require.Equal(t, cause, errors.Unwrap(err))
}

func TestErrorUnwrapNilCause(t *testing.T) {
	err := command.NewError(command.ErrTimedOut, "no cause", nil)

	require.ErrorIs(t, err, command.ErrTimedOut)
	require.NoError(t, errors.Unwrap(err))
}

func TestErrorMessage(t *testing.T) {
	err := command.NewError(command.ErrRejected, "domain rejected the command", nil)
	require.Equal(t, "domain rejected the command", err.Error())
}

func TestErrorAs(t *testing.T) {
	err := fmt.Errorf("context: %w", command.NewError(command.ErrCanceled, "canceled", nil))

	var cmdErr *command.Error
	require.ErrorAs(t, err, &cmdErr)
	require.ErrorIs(t, cmdErr, command.ErrCanceled)
}

func TestValidationSentinelsAreDistinct(t *testing.T) {
	sentinels := []error{
		command.ErrInvalidMetadata,
		command.ErrInvalidEnvelope,
		command.ErrInvalidResult,
		command.ErrInvalidPrincipal,
		command.ErrReservedKey,
		command.ErrSameOperationID,
		command.ErrDeadlineExtension,
	}

	for i, a := range sentinels {
		for j, b := range sentinels {
			if i == j {
				continue
			}
			require.NotErrorIs(t, a, b)
		}
	}
}
