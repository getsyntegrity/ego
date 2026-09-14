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

// This file is a white-box (package tenancy) test: it uses the unexported
// tenantContextKey directly to bind a TenantContext into a context.Context
// WITHOUT going through Attach. That is otherwise unreachable from outside
// this package (Attach is the only exported way to bind a TenantContext,
// and — as of design.md Decision D8 — it now refuses an invalid one on the
// way in). This file proves Require's OWN independent rejection: defense
// in depth for the hypothetical case where a value is bound some other
// way, not a substitute for Attach's check.
package tenancy

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequire_RejectsInvalidTenantContextEvenIfSomehowBound(t *testing.T) {
	// Bypasses Attach entirely: simulates a bound-but-invalid TenantContext
	// reaching Require despite Attach's own rejection, e.g. a future
	// refactor that binds via context.WithValue directly.
	var zero TenantContext
	ctx := context.WithValue(context.Background(), tenantContextKey, zero)

	_, err := Require(ctx)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalid))
}

func TestRequire_AcceptsValidTenantContextBoundDirectly(t *testing.T) {
	tc, err := NewTenantContext("acme-corp")
	require.NoError(t, err)
	ctx := context.WithValue(context.Background(), tenantContextKey, tc)

	got, err := Require(ctx)
	require.NoError(t, err)
	assert.Equal(t, tc, got)
}
