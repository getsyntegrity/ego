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
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pablogore/ego/v4/command"
)

func TestCarrierFromContext_NoneAttached(t *testing.T) {
	_, ok := carrierFromContext(context.Background())
	require.False(t, ok)
}

func TestAttachCarrier_RoundTrip(t *testing.T) {
	op, err := command.NewOperationID("op-1")
	require.NoError(t, err)
	md, err := command.NewMetadata(op)
	require.NoError(t, err)

	ctx := attachCarrier(context.Background(), command.MarshalMetadata(md))

	c, ok := carrierFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, command.MarshalMetadata(md), c)
}

func TestMetadataFromContext_NoneAttached(t *testing.T) {
	_, ok := metadataFromContext(context.Background())
	require.False(t, ok)
}

func TestMetadataFromContext_RematerializesMetadata(t *testing.T) {
	op, err := command.NewOperationID("op-1")
	require.NoError(t, err)
	corr, err := command.NewOperationID("corr-1")
	require.NoError(t, err)
	md, err := command.NewMetadata(op, command.WithCorrelationID(command.CorrelationID(corr)))
	require.NoError(t, err)

	ctx := attachCarrier(context.Background(), command.MarshalMetadata(md))

	got, ok := metadataFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, md.OperationID(), got.OperationID())
	require.Equal(t, md.CorrelationID(), got.CorrelationID())
	require.Equal(t, md.Timestamp(), got.Timestamp())
}

func TestMetadataFromContext_InvalidCarrierFailsClosed(t *testing.T) {
	// A Carrier missing the required operation_id key fails to unmarshal;
	// metadataFromContext reports this as "no metadata available" rather
	// than propagating the error, so callers fall back to legacy dispatch.
	ctx := attachCarrier(context.Background(), command.Carrier{})
	_, ok := metadataFromContext(ctx)
	require.False(t, ok)
}
