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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/persistence"
)

// TestAnswerTenantBinding pins the actors' shared query handler: it answers
// from the bound scope only, never discloses the bound tenant, and reports
// no binding in legacy mode or for an administrative-looking query.
func TestAnswerTenantBinding(t *testing.T) {
	acme, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)

	match := AnswerTenantBinding(true, acme, &egopb.TenantBindingQuery{TenantId: "acme"})
	assert.True(t, match.GetTenantAware())
	assert.True(t, match.GetMatches())

	other := AnswerTenantBinding(true, acme, &egopb.TenantBindingQuery{TenantId: "globex"})
	assert.True(t, other.GetTenantAware())
	assert.False(t, other.GetMatches())

	invalid := AnswerTenantBinding(true, acme, &egopb.TenantBindingQuery{TenantId: ""})
	assert.False(t, invalid.GetMatches(), "an invalid queried tenant never matches")

	legacy := AnswerTenantBinding(false, persistence.Unscoped(), &egopb.TenantBindingQuery{TenantId: "acme"})
	assert.False(t, legacy.GetTenantAware())
	assert.False(t, legacy.GetMatches())
}
