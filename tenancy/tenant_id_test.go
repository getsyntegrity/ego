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

package tenancy_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablogore/ego/v4/tenancy"
)

func TestNewTenantID_RejectsEmpty(t *testing.T) {
	_, err := tenancy.NewTenantID("")
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrInvalid))
}

func TestNewTenantID_RejectsWhitespaceOnly(t *testing.T) {
	_, err := tenancy.NewTenantID("   ")
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrInvalid))
}

func TestNewTenantID_RejectsLeadingOrTrailingWhitespace(t *testing.T) {
	_, err := tenancy.NewTenantID("  acme-corp  ")
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrInvalid))
}

func TestNewTenantID_RejectsInteriorWhitespace(t *testing.T) {
	_, err := tenancy.NewTenantID("acme corp")
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrInvalid))
}

func TestNewTenantID_RejectsControlRune(t *testing.T) {
	_, err := tenancy.NewTenantID("acme\x00corp")
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrInvalid))
}

func TestNewTenantID_RejectsTabAndNewline(t *testing.T) {
	_, err := tenancy.NewTenantID("acme\tcorp\n")
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrInvalid))
}

func TestNewTenantID_RejectsInvalidUTF8(t *testing.T) {
	invalid := string([]byte{0xff, 0xfe, 0xfd})
	_, err := tenancy.NewTenantID(invalid)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrInvalid))
}

func TestNewTenantID_RejectsTooLong(t *testing.T) {
	tooLong := strings.Repeat("a", 129)
	_, err := tenancy.NewTenantID(tooLong)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrInvalid))
}

func TestNewTenantID_AcceptsMaxLength(t *testing.T) {
	maxLen := strings.Repeat("a", 128)
	id, err := tenancy.NewTenantID(maxLen)
	require.NoError(t, err)
	assert.Equal(t, tenancy.TenantID(maxLen), id)
}

func TestNewTenantID_AcceptsArbitraryNonUUIDIdentifiers(t *testing.T) {
	tests := []string{
		"acme-corp",
		"customer_42",
		"12345",
		"Tenant.With.Dots",
		"a",
	}

	for _, s := range tests {
		t.Run(s, func(t *testing.T) {
			id, err := tenancy.NewTenantID(s)
			require.NoError(t, err)
			assert.Equal(t, tenancy.TenantID(s), id)
		})
	}
}

func TestNewTenantID_DoesNotNormalizeCase(t *testing.T) {
	id, err := tenancy.NewTenantID("Acme-Corp")
	require.NoError(t, err)
	assert.Equal(t, tenancy.TenantID("Acme-Corp"), id, "constructor must not case-fold per R1")
}
