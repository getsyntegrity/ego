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
	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
	"github.com/pablogore/ego/v4/tenancy"
)

// answerTenantBinding is the single implementation of the engine-internal
// egopb.TenantBindingQuery, shared by EventSourcedActor, DurableStateActor,
// and SagaActor. It answers from the actor's own spawn binding — the
// persistence.Scope PreStart bound via resolveScope, which never changes
// for the actor's lifetime — and runs no business behavior, resolves no
// tenant, and touches no store. The reply says only whether the binding is
// the queried tenant, never which tenant it is.
func answerTenantBinding(tenantAware bool, scope persistence.Scope, query *egopb.TenantBindingQuery) *egopb.TenantBindingReply {
	if !tenantAware || !scope.Valid() || scope.IsUnscoped() {
		return &egopb.TenantBindingReply{}
	}
	queried, err := persistence.NewTenantScope(tenancy.TenantID(query.GetTenantId()))
	return &egopb.TenantBindingReply{
		TenantAware: true,
		Matches:     err == nil && scope.Equal(queried),
	}
}
