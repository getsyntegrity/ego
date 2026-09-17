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
	"context"
	"errors"
	"fmt"
)

// errActorDeadlineExceeded and errActorContextCanceled classify why an
// actor's deadline gate (checkDeadline) rejected a command: the effective
// deadline Engine.Dispatch computed — min(ctx's own deadline, the dispatched
// envelope's Metadata deadline, the caller-supplied timeout) — expired, or
// the caller's context was canceled outright, before the handler ran or
// before its output was persisted.
//
// Neither crosses the wire as a typed value: egopb.ErrorReply carries only a
// string Message. resultFromReply recovers the classification by matching a
// reply's message against these sentinels' Error() text as a prefix, so the
// caller-facing command.Result still reports OutcomeTimedOut/OutcomeCanceled
// instead of falling into the otherwise-lossy OutcomeFailed mapping.
var (
	errActorDeadlineExceeded = errors.New("ego: command deadline exceeded")
	errActorContextCanceled  = errors.New("ego: command context canceled")
)

// checkDeadline reports the error an actor's fail-closed gate should reply
// with when ctx has already expired or been canceled, or nil when ctx is
// still valid. stage identifies where the gate fired (e.g. "before handler
// execution", "before persistence") for diagnostics only — it never changes
// the classification resultFromReply recovers from the message.
//
// context.WithDeadline does not preempt a handler that ignores its context:
// this check is the actual barrier. Every call site that persists or mutates
// actor state on behalf of a command must invoke checkDeadline immediately
// before invoking the command handler, and again immediately before any
// state mutation or persistence, since the deadline can expire while the
// handler itself was still running.
func checkDeadline(ctx context.Context, stage string) error {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %s", errActorContextCanceled, stage)
	}
	return fmt.Errorf("%w: %s", errActorDeadlineExceeded, stage)
}
