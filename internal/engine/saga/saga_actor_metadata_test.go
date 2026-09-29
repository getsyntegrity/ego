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

package saga

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getsyntegrity/ego/command"
	"github.com/getsyntegrity/ego/internal/engine/enginetest"
	"github.com/getsyntegrity/ego/internal/engine/protocol"
)

// TestActorAttachCommandMetadata exercises sagaCommand's dual metadata
// behavior (#60, issue #60's "sagaCommand gana metadata" scope item):
// when a sagaCommand carries an explicit Metadata, attachCommandMetadata
// must use it verbatim; when left as the zero value, it must derive a
// fresh child from the saga's own rootMetadata (correlation inherited,
// causation set to the saga's root operation).
func TestActorAttachCommandMetadata(t *testing.T) {
	rootOp, err := command.NewOperationID("saga-root-1")
	require.NoError(t, err)
	rootMetadata, err := command.NewMetadata(rootOp)
	require.NoError(t, err)

	s := &Actor{
		sagaID:       "saga-root-1",
		rootMetadata: rootMetadata,
		logger:       enginetest.DiscardLogger,
	}

	t.Run("zero-value metadata is auto-derived from the saga's root", func(t *testing.T) {
		ctx := s.attachCommandMetadata(context.Background(), command.Metadata{})

		md, ok := protocol.MetadataFromContext(ctx)
		require.True(t, ok)
		assert.NotEqual(t, rootMetadata.OperationID(), md.OperationID(), "derived metadata must carry a fresh operation id, not the root's")
		assert.Equal(t, rootMetadata.CorrelationID(), md.CorrelationID(), "correlation id must be inherited from the root (D7)")
		causation, ok := md.CausationID()
		require.True(t, ok)
		assert.Equal(t, command.CausationID(rootMetadata.OperationID()), causation, "causation must be the saga's root operation")
	})

	t.Run("explicit metadata is used verbatim", func(t *testing.T) {
		explicitOp, err := command.NewOperationID("explicit-op-1")
		require.NoError(t, err)
		explicit, err := command.NewMetadata(explicitOp, command.WithCorrelationID("explicit-correlation"))
		require.NoError(t, err)

		ctx := s.attachCommandMetadata(context.Background(), explicit)

		md, ok := protocol.MetadataFromContext(ctx)
		require.True(t, ok)
		assert.Equal(t, explicit.OperationID(), md.OperationID())
		assert.Equal(t, explicit.CorrelationID(), md.CorrelationID())
	})
}
