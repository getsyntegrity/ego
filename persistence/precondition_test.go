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

package persistence_test

import (
	"testing"

	"github.com/pablogore/ego/v4/persistence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWritePreconditionZeroValueIsInvalid(t *testing.T) {
	var zero persistence.WritePrecondition

	assert.False(t, zero.Valid())
	assert.False(t, zero.IsUnconditional())
	assert.False(t, zero.IsGenesis())

	_, ok := zero.Revision()
	assert.False(t, ok)

	assert.Equal(t, "unspecified", zero.String())
}

func TestWritePreconditionUnconditional(t *testing.T) {
	p := persistence.Unconditional()

	assert.True(t, p.Valid())
	assert.True(t, p.IsUnconditional())
	assert.False(t, p.IsGenesis())

	_, ok := p.Revision()
	assert.False(t, ok)

	assert.Equal(t, "unconditional", p.String())
}

func TestWritePreconditionExpectGenesis(t *testing.T) {
	p := persistence.ExpectGenesis()

	assert.True(t, p.Valid())
	assert.False(t, p.IsUnconditional())
	assert.True(t, p.IsGenesis())

	_, ok := p.Revision()
	assert.False(t, ok)

	assert.Equal(t, "genesis", p.String())
}

func TestWritePreconditionExpectRevision(t *testing.T) {
	p := persistence.ExpectRevision(42)

	assert.True(t, p.Valid())
	assert.False(t, p.IsUnconditional())
	assert.False(t, p.IsGenesis())

	revision, ok := p.Revision()
	require.True(t, ok)
	assert.Equal(t, uint64(42), revision)

	assert.Equal(t, "42", p.String())
}

// ExpectRevision(0) is a distinct, meaningful precondition — an exact-revision
// check against revision zero — and must never collapse into Unconditional()
// or ExpectGenesis(). No magic-number sentinel is permitted for "unconditional".
func TestWritePreconditionExpectRevisionZeroIsNotGenesisOrUnconditional(t *testing.T) {
	p := persistence.ExpectRevision(0)

	assert.False(t, p.IsUnconditional())
	assert.False(t, p.IsGenesis())

	revision, ok := p.Revision()
	require.True(t, ok)
	assert.Equal(t, uint64(0), revision)

	assert.Equal(t, "0", p.String())
	assert.NotEqual(t, persistence.Unconditional(), p)
	assert.NotEqual(t, persistence.ExpectGenesis(), p)
}

func TestWritePreconditionComparable(t *testing.T) {
	assert.Equal(t, persistence.Unconditional(), persistence.Unconditional())
	assert.Equal(t, persistence.ExpectGenesis(), persistence.ExpectGenesis())
	assert.Equal(t, persistence.ExpectRevision(7), persistence.ExpectRevision(7))
	assert.NotEqual(t, persistence.ExpectRevision(7), persistence.ExpectRevision(8))
	assert.NotEqual(t, persistence.WritePrecondition{}, persistence.Unconditional())
}
