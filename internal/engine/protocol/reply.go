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
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/getsyntegrity/ego/command"
	"github.com/getsyntegrity/ego/egopb"
)

// ParseCommandReply unpacks a wire-level egopb.CommandReply into the state it
// carries and its sequence number, or the error an ErrorReply reports.
func ParseCommandReply(reply *egopb.CommandReply) (proto.Message, uint64, error) {
	var (
		state proto.Message
		err   error
	)

	switch r := reply.GetReply().(type) {
	case *egopb.CommandReply_StateReply:
		msg, err := r.StateReply.GetState().UnmarshalNew()
		if err != nil {
			return state, 0, err
		}

		switch v := msg.(type) {
		case proto.Message:
			return v, r.StateReply.GetSequenceNumber(), nil
		default:
			return state, 0, fmt.Errorf("got %s", r.StateReply.GetState().GetTypeUrl())
		}
	case *egopb.CommandReply_ErrorReply:
		err = errors.New(r.ErrorReply.GetMessage())
		return state, 0, err
	}
	return state, 0, errors.New("no state received")
}

// ResultFromReply maps a wire-level egopb.CommandReply onto the canonical
// command.Result taxonomy, carrying md (the dispatched Envelope's own
// Metadata, since the wire reply itself carries none back) as the Result's
// Metadata.
//
// egopb.ErrorReply -> command.OutcomeFailed is a deliberately lossy mapping
// (#60's Alcance calls this out explicitly): the wire protocol has no way
// to distinguish a domain rejection from an application failure, a timeout
// or a cancellation, so every CommandReply_ErrorReply becomes OutcomeFailed
// regardless of its true cause — with two recognized exceptions, applied by
// ClassifyErrorReply's ordered registry (reply_classification.go, design.md
// D8): a message produced by an actor's CheckDeadline gate
// (deadline_gate.go), identified by its
// errActorDeadlineExceeded/errActorContextCanceled prefix, maps to
// OutcomeTimedOut/OutcomeCanceled instead, so a mid-handler deadline
// rejection is classifiable the same way a pre-dispatch one is; and a
// message produced by a conditional write's *persistence.ConflictError
// (D3/D7), identified by its persistence.ErrConcurrencyConflict prefix,
// maps to OutcomeRejected with Failure.Code() ==
// command.CodeConcurrencyConflict (D6). An empty ErrorReply.Message (never
// produced by this repo's own sendErrorReply call sites, but not ruled out
// for an external egopb.CommandReply) is substituted with a placeholder,
// since command.NewFailure rejects an empty message.
func ResultFromReply(reply *egopb.CommandReply, md command.Metadata) (command.Result, error) {
	switch r := reply.GetReply().(type) {
	case *egopb.CommandReply_StateReply:
		state, err := r.StateReply.GetState().UnmarshalNew()
		if err != nil {
			return command.Result{}, err
		}
		return command.NewSuccess(md, state, r.StateReply.GetSequenceNumber())
	case *egopb.CommandReply_ErrorReply:
		message := r.ErrorReply.GetMessage()
		if message == "" {
			message = "command: empty error reply message"
		}
		return ClassifyErrorReply(md, message)
	}
	return command.Result{}, errors.New("no state received")
}
