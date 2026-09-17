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
	"strings"

	"github.com/pablogore/ego/v4/command"
	"github.com/pablogore/ego/v4/persistence"
)

// resultBuilder builds the command.Result for an egopb.ErrorReply message
// recognized by a classifierRegistry entry.
type resultBuilder func(md command.Metadata, message string) (command.Result, error)

// classifierRegistry is the ordered, table-driven classifier (design.md D8)
// resultFromReply consults to turn an egopb.CommandReply_ErrorReply's
// message into the correct command.Result outcome. Matching is by
// strings.HasPrefix against each entry's sentinel text; declaration order
// is match priority, and the first match wins. This registry's invariant
// (enforced by TestClassifierRegistrySentinelsDoNotPrefixEachOther) is that
// no entry's sentinel text may be a prefix of another entry's, since that
// would make match order ambiguous.
var classifierRegistry = []struct {
	sentinel string
	build    resultBuilder
}{
	{sentinel: errActorContextCanceled.Error(), build: buildCanceledResult},
	{sentinel: errActorDeadlineExceeded.Error(), build: buildTimedOutResult},
	{sentinel: persistence.ErrConcurrencyConflict.Error(), build: buildConcurrencyConflictResult},
}

func buildCanceledResult(md command.Metadata, message string) (command.Result, error) {
	failure, err := command.NewFailure(message)
	if err != nil {
		return command.Result{}, err
	}
	return command.NewCanceled(md, failure)
}

func buildTimedOutResult(md command.Metadata, message string) (command.Result, error) {
	failure, err := command.NewFailure(message)
	if err != nil {
		return command.Result{}, err
	}
	return command.NewTimedOut(md, failure)
}

// buildConcurrencyConflictResult classifies an ErrorReply produced by a
// conditional write's *persistence.ConflictError (D3/D7) as a domain
// rejection (D6): OutcomeRejected with Failure.Code() ==
// command.CodeConcurrencyConflict. It reconstructs the *ConflictError from
// message via persistence.ParseConflictError and attaches it as the
// Failure's cause, so errors.As(result.Err(), &conflictErr) recovers the
// declared/actual revision on the caller side. A message that does not
// parse as a ConflictError (never produced by this repo's own
// sendErrorReply call sites, but not ruled out for an external
// egopb.CommandReply) still classifies as a concurrency conflict by
// sentinel match, just without a recoverable cause.
func buildConcurrencyConflictResult(md command.Metadata, message string) (command.Result, error) {
	opts := []command.FailureOption{command.WithFailureCode(command.CodeConcurrencyConflict)}
	if conflictErr, ok := persistence.ParseConflictError(message); ok {
		opts = append(opts, command.WithFailureCause(conflictErr))
	}
	failure, err := command.NewFailure(message, opts...)
	if err != nil {
		return command.Result{}, err
	}
	return command.NewRejected(md, failure)
}

// classifyErrorReply maps an egopb.CommandReply_ErrorReply's message onto
// the correct command.Result outcome via classifierRegistry. A message
// matching no entry's sentinel falls through to OutcomeFailed, preserving
// the deliberately lossy default mapping documented on resultFromReply.
func classifyErrorReply(md command.Metadata, message string) (command.Result, error) {
	for _, entry := range classifierRegistry {
		if strings.HasPrefix(message, entry.sentinel) {
			return entry.build(md, message)
		}
	}
	failure, err := command.NewFailure(message)
	if err != nil {
		return command.Result{}, err
	}
	return command.NewFailed(md, failure)
}
