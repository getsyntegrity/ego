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

// This file is a white-box (package tenancy) test: it exercises the
// unexported newError/newErrorWithTenant constructors directly, since
// *Error can only be produced by the package itself (design.md: exact
// signatures, unexported-field constructor-only pattern).
package tenancy

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestError_ReasonAccessor(t *testing.T) {
	err := newError(ReasonMissing, "tenancy: tenant context missing", nil)
	assert.Equal(t, ReasonMissing, err.Reason())
}

func TestError_ErrorMessage(t *testing.T) {
	err := newError(ReasonInvalid, "tenancy: tenant id must not be empty", nil)
	assert.Equal(t, "tenancy: tenant id must not be empty", err.Error())
}

func TestError_UnwrapReturnsCause(t *testing.T) {
	cause := errors.New("store: tenant not found")
	err := newError(ReasonInvalid, "tenancy: tenant identity invalid", cause)

	require.ErrorIs(t, err, cause)
	assert.Equal(t, cause, errors.Unwrap(err))
}

func TestError_UnwrapReturnsNilWithoutCause(t *testing.T) {
	err := newError(ReasonDenied, "tenancy: tenant identity denied", nil)
	assert.Nil(t, errors.Unwrap(err))
}

func TestError_TenantAccessorWithoutAttribution(t *testing.T) {
	err := newError(ReasonInvalid, "tenancy: tenant id must not be empty", nil)

	id, ok := err.Tenant()
	assert.False(t, ok)
	assert.Equal(t, TenantID(""), id)
}

func TestError_TenantAccessorWithAttribution(t *testing.T) {
	err := newErrorWithTenant(ReasonDenied, TenantID("acme-corp"), "tenancy: tenant identity denied", nil)

	id, ok := err.Tenant()
	require.True(t, ok)
	assert.Equal(t, TenantID("acme-corp"), id)
}

func TestError_IsMatchesSentinelByReason(t *testing.T) {
	tests := []struct {
		name     string
		reason   Reason
		sentinel error
	}{
		{"missing", ReasonMissing, ErrMissing},
		{"invalid", ReasonInvalid, ErrInvalid},
		{"denied", ReasonDenied, ErrDenied},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newError(tt.reason, "boom", nil)
			assert.True(t, errors.Is(err, tt.sentinel))
		})
	}
}

func TestError_IsDoesNotMatchOtherSentinels(t *testing.T) {
	err := newError(ReasonMissing, "tenancy: tenant context missing", nil)

	assert.False(t, errors.Is(err, ErrInvalid))
	assert.False(t, errors.Is(err, ErrDenied))
}

func TestSentinels_AreDistinctFromEachOther(t *testing.T) {
	assert.False(t, errors.Is(ErrMissing, ErrInvalid))
	assert.False(t, errors.Is(ErrMissing, ErrDenied))
	assert.False(t, errors.Is(ErrInvalid, ErrDenied))
}
