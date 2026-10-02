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

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/urd/command"
)

func TestErrorClassification(t *testing.T) {
	specs.Describe(t, "NewError classifies an error as exactly one outcome sentinel", func(s *specs.Spec) {
		type classification struct {
			name     string
			sentinel error
			other    []error
		}
		cases := []classification{
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

		specs.Table(s, cases, func(tc classification) string { return tc.name }, func(ctx *specs.Context, tc classification) {
			err := command.NewError(tc.sentinel, "boom", nil)
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.MatchError(tc.sentinel))
			for _, o := range tc.other {
				ctx.Expect(err).To(specs.Not(specs.MatchError(o)))
			}
		})
	})
}

func TestErrorUnwrap(t *testing.T) {
	specs.Describe(t, "Error.Unwrap exposes the cause", func(s *specs.Spec) {
		s.It("matches both the sentinel and the cause and unwraps to the cause", func(ctx *specs.Context) {
			cause := errors.New("underlying cause")
			err := command.NewError(command.ErrFailed, "wrapped", cause)

			ctx.Expect(err).To(specs.MatchError(command.ErrFailed))
			ctx.Expect(err).To(specs.MatchError(cause))
			ctx.Expect(errors.Unwrap(err)).ToEqual(cause)
		})
	})
}

func TestErrorUnwrapNilCause(t *testing.T) {
	specs.Describe(t, "Error.Unwrap without a cause", func(s *specs.Spec) {
		s.It("matches the sentinel and unwraps to nil", func(ctx *specs.Context) {
			err := command.NewError(command.ErrTimedOut, "no cause", nil)

			ctx.Expect(err).To(specs.MatchError(command.ErrTimedOut))
			ctx.Expect(errors.Unwrap(err)).To(specs.BeNil())
		})
	})
}

func TestErrorMessage(t *testing.T) {
	specs.Describe(t, "Error.Error renders the message", func(s *specs.Spec) {
		s.It("returns the message it was built with", func(ctx *specs.Context) {
			err := command.NewError(command.ErrRejected, "domain rejected the command", nil)
			ctx.Expect(err.Error()).ToEqual("domain rejected the command")
		})
	})
}

func TestErrorAs(t *testing.T) {
	specs.Describe(t, "a wrapped command Error is recoverable with errors.As", func(s *specs.Spec) {
		s.It("yields the command Error that still matches its sentinel", func(ctx *specs.Context) {
			err := fmt.Errorf("context: %w", command.NewError(command.ErrCanceled, "canceled", nil))

			var cmdErr *command.Error
			ctx.Expect(err).To(specs.MatchErrorAs(&cmdErr))
			ctx.Expect(cmdErr).To(specs.MatchError(command.ErrCanceled))
		})
	})
}

func TestValidationSentinelsAreDistinct(t *testing.T) {
	specs.Describe(t, "the validation sentinels are distinct", func(s *specs.Spec) {
		s.It("does not match any sentinel against another", func(ctx *specs.Context) {
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
					ctx.Expect(a).To(specs.Not(specs.MatchError(b)))
				}
			}
		})
	})
}
