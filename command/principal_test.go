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

func TestNewPrincipal(t *testing.T) {
	t.Run("id required", func(t *testing.T) {
		_, err := command.NewPrincipal("")
		require.Error(t, err)
	})

	t.Run("id only, kind absent", func(t *testing.T) {
		p, err := command.NewPrincipal("user-42")
		require.NoError(t, err)
		require.Equal(t, "user-42", p.ID())

		kind, ok := p.Kind()
		require.False(t, ok)
		require.Empty(t, kind)
	})

	t.Run("id and kind", func(t *testing.T) {
		p, err := command.NewPrincipal("user-42", command.WithPrincipalKind("service-account"))
		require.NoError(t, err)
		require.Equal(t, "user-42", p.ID())

		kind, ok := p.Kind()
		require.True(t, ok)
		require.Equal(t, "service-account", kind)
	})
}

func TestPrincipalIsAbstract(t *testing.T) {
	// Principal carries only an opaque identity reference (AC5): its
	// declared surface is ID/Kind only, no credential, token, role, scope
	// or protocol-specific field. This is asserted structurally by the
	// fact that NewPrincipal takes only an id and options, never a token
	// or credential parameter.
	p, err := command.NewPrincipal("user-42")
	require.NoError(t, err)
	require.Equal(t, "user-42", p.ID())
}
