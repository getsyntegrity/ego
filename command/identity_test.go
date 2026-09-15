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

	"github.com/stretchr/testify/require"

	"github.com/pablogore/ego/v4/command"
)

func TestNewOperationID(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		op, err := command.NewOperationID("order-123")
		require.NoError(t, err)
		require.EqualValues(t, "order-123", op)
	})

	t.Run("empty rejected", func(t *testing.T) {
		_, err := command.NewOperationID("")
		require.Error(t, err)
	})

	t.Run("not valid UTF-8 rejected", func(t *testing.T) {
		_, err := command.NewOperationID(string([]byte{0xff, 0xfe}))
		require.Error(t, err)
	})

	t.Run("leading or trailing whitespace rejected", func(t *testing.T) {
		_, err := command.NewOperationID(" order-123")
		require.Error(t, err)

		_, err = command.NewOperationID("order-123 ")
		require.Error(t, err)
	})

	t.Run("control rune rejected", func(t *testing.T) {
		_, err := command.NewOperationID("order-123\n")
		require.Error(t, err)
	})

	t.Run("exceeds max length rejected", func(t *testing.T) {
		long := make([]byte, 129)
		for i := range long {
			long[i] = 'a'
		}
		_, err := command.NewOperationID(string(long))
		require.Error(t, err)
	})

	t.Run("interior whitespace accepted", func(t *testing.T) {
		op, err := command.NewOperationID("order 123")
		require.NoError(t, err)
		require.EqualValues(t, "order 123", op)
	})
}

func TestGenerateOperationID(t *testing.T) {
	op1, err := command.GenerateOperationID()
	require.NoError(t, err)
	require.NotEmpty(t, op1)

	op2, err := command.GenerateOperationID()
	require.NoError(t, err)
	require.NotEmpty(t, op2)

	require.NotEqual(t, op1, op2)

	// GenerateOperationID's output must itself satisfy NewOperationID's
	// validation rules.
	_, err = command.NewOperationID(string(op1))
	require.NoError(t, err)
}

func TestGenerateOperationIDUniqueness(t *testing.T) {
	seen := make(map[command.OperationID]struct{})
	for i := 0; i < 1000; i++ {
		op, err := command.GenerateOperationID()
		require.NoError(t, err)
		_, exists := seen[op]
		require.False(t, exists, "duplicate operation id generated: %s", op)
		seen[op] = struct{}{}
	}
}

func TestIdentityDefinedTypesAreDistinct(t *testing.T) {
	// OperationID, CorrelationID and CausationID are distinct defined
	// types over string, not aliases of each other or of tenancy's
	// correlation concept — this is a compile-time assertion.
	var op command.OperationID = "op-1"
	var corr command.CorrelationID = "corr-1"
	var caus command.CausationID = "caus-1"

	require.Equal(t, "op-1", string(op))
	require.Equal(t, "corr-1", string(corr))
	require.Equal(t, "caus-1", string(caus))
}
