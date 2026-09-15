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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/pablogore/ego/v4/command"
)

func TestNewEnvelope(t *testing.T) {
	op := mustOperationID(t, "op-1")
	md, err := command.NewMetadata(op)
	require.NoError(t, err)

	payload := timestamppb.New(time.Unix(100, 0))
	env, err := command.NewEnvelope(payload, md)
	require.NoError(t, err)

	require.True(t, proto.Equal(payload, env.Payload()))
	require.Equal(t, md, env.Metadata())
}

func TestNewEnvelopeRejectsNilPayload(t *testing.T) {
	op := mustOperationID(t, "op-1")
	md, err := command.NewMetadata(op)
	require.NoError(t, err)

	_, err = command.NewEnvelope(nil, md)
	require.ErrorIs(t, err, command.ErrInvalidEnvelope)
}

func TestPayloadAsTypedExtraction(t *testing.T) {
	op := mustOperationID(t, "op-1")
	md, err := command.NewMetadata(op)
	require.NoError(t, err)

	payload := timestamppb.New(time.Unix(100, 0))
	env, err := command.NewEnvelope(payload, md)
	require.NoError(t, err)

	got, ok := command.PayloadAs[*timestamppb.Timestamp](env)
	require.True(t, ok)
	require.Equal(t, int64(100), got.GetSeconds())

	_, ok = command.PayloadAs[*emptypb.Empty](env)
	require.False(t, ok)
}

func TestEnvelopeDeriveDelegatesToMetadataDerive(t *testing.T) {
	parentOp := mustOperationID(t, "op-1")
	parentMD, err := command.NewMetadata(parentOp)
	require.NoError(t, err)

	parentPayload := timestamppb.New(time.Unix(200, 0))
	parentEnv, err := command.NewEnvelope(parentPayload, parentMD)
	require.NoError(t, err)

	childOp := mustOperationID(t, "op-2")
	childPayload := timestamppb.New(time.Unix(300, 0))
	childEnv, err := parentEnv.Derive(childPayload, childOp)
	require.NoError(t, err)

	require.True(t, proto.Equal(childPayload, childEnv.Payload()))
	require.Equal(t, parentMD.CorrelationID(), childEnv.Metadata().CorrelationID())
	require.Equal(t, childOp, childEnv.Metadata().OperationID())

	causation, ok := childEnv.Metadata().CausationID()
	require.True(t, ok)
	require.Equal(t, command.CausationID(parentOp), causation)
}

func TestEnvelopeDeriveRejectsNilPayload(t *testing.T) {
	op := mustOperationID(t, "op-1")
	md, err := command.NewMetadata(op)
	require.NoError(t, err)

	payload := timestamppb.New(time.Unix(200, 0))
	env, err := command.NewEnvelope(payload, md)
	require.NoError(t, err)

	childOp := mustOperationID(t, "op-2")
	_, err = env.Derive(nil, childOp)
	require.ErrorIs(t, err, command.ErrInvalidEnvelope)
}
