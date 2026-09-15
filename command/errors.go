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

// Package command defines the canonical, runtime-independent command/result
// envelope contract (EGO-WRITE-003): operation identity, correlation,
// causation, a tenant slot composing tenancy/, a principal slot, governed
// custom metadata, temporal fields and a six-kind outcome taxonomy. This
// package imports nothing beyond the standard library,
// google.golang.org/protobuf and ego/v4/tenancy — no GoAkt, no engine, no
// transport or auth library. It is a value contract only: Engine.SendCommand,
// its dispatch path and SagaActor are untouched by this package.
package command

import "errors"

// Error is the structured error type returned by the command package.
// Callers classify a failure with errors.Is against one of the sentinel
// errors below, and inspect it further with errors.As against *Error.
//
// Error can only be constructed by this package (unexported fields);
// callers never build one directly.
type Error struct {
	sentinel error
	message  string
	cause    error
}

// NewError builds a command error classified by sentinel, carrying message
// and an optional cause. cause may be nil.
func NewError(sentinel error, message string, cause error) *Error {
	return &Error{sentinel: sentinel, message: message, cause: cause}
}

// Error implements the error interface.
func (e *Error) Error() string {
	return e.message
}

// Unwrap returns the underlying cause, if any, letting errors.Is/As
// traverse a wrapped application or store-specific error.
func (e *Error) Unwrap() error {
	return e.cause
}

// Is reports whether target is this error's classifying sentinel, enabling
// errors.Is(err, command.ErrFailed) style classification.
func (e *Error) Is(target error) bool {
	return e.sentinel == target
}

// Sentinel errors classifying a command Result's outcome. Use errors.Is to
// test a Result.Err() against these; use errors.As against *Error for the
// wrapped message and cause.
var (
	// ErrRejected indicates the domain rejected the command (a business
	// rule failure, not an infrastructure fault).
	ErrRejected error = errors.New("command: rejected")
	// ErrFailed indicates an application or runtime failure while
	// processing the command.
	ErrFailed error = errors.New("command: failed")
	// ErrTimedOut indicates the command did not complete before its
	// deadline.
	ErrTimedOut error = errors.New("command: timed out")
	// ErrCanceled indicates the command's context was canceled before
	// completion.
	ErrCanceled error = errors.New("command: canceled")
)

// Sentinel errors classifying construction/validation failures across the
// command package's constructors.
var (
	// ErrInvalidMetadata indicates Metadata construction or an option
	// received an invalid value.
	ErrInvalidMetadata error = errors.New("command: invalid metadata")
	// ErrInvalidEnvelope indicates Envelope construction received an
	// invalid value, such as a nil payload.
	ErrInvalidEnvelope error = errors.New("command: invalid envelope")
	// ErrInvalidResult indicates Result construction received a value
	// inconsistent with its outcome kind.
	ErrInvalidResult error = errors.New("command: invalid result")
	// ErrInvalidPrincipal indicates Principal construction received an
	// invalid value, such as an empty id.
	ErrInvalidPrincipal error = errors.New("command: invalid principal")
	// ErrReservedKey indicates an attempt to use a reserved key (the
	// "ego." prefix or an exact canonical field name) as a custom
	// metadata key.
	ErrReservedKey error = errors.New("command: reserved key")
	// ErrSameOperationID indicates Metadata.Derive was called with the
	// same OperationID as its parent, which would collapse the parent
	// and child operations into one identity.
	ErrSameOperationID error = errors.New("command: derived operation id must differ from parent")
	// ErrDeadlineExtension indicates Metadata.Derive attempted to extend
	// a deadline rather than only shortening it.
	ErrDeadlineExtension error = errors.New("command: derived deadline must not extend the parent's")
)
