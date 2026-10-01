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

package engine

import (
	"context"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/getsyntegrity/ego/egopb"
	testpb "github.com/getsyntegrity/ego/internal/testpb"
	"github.com/getsyntegrity/ego/persistence"
	"github.com/getsyntegrity/ego/tenancy"
)

// administrativeScopeKey marks a ctx that administrativeScopeResolver
// resolves to an administrative (non-tenant) tenancy.TenantContext.
type administrativeScopeKey struct{}

// administrativeScopeResolver resolves an administrative TenantContext when
// ctx carries administrativeScopeKey and otherwise behaves like
// perCallerTenantResolver. It exposes no fixed tenant.
type administrativeScopeResolver struct{}

var _ tenancy.TenantResolver = administrativeScopeResolver{}

func (administrativeScopeResolver) Resolve(ctx context.Context) (tenancy.TenantContext, error) {
	if ctx.Value(administrativeScopeKey{}) != nil {
		admin, err := tenancy.NewAdministrative("ops-team", "audit")
		if err != nil {
			return tenancy.TenantContext{}, err
		}
		return tenancy.NewAdministrativeContext(admin)
	}
	return perCallerTenantResolver{}.Resolve(ctx)
}

// TestAdministrativeScopeIsNeverAnAggregateTenantScope pins that an
// administrative TenantContext can never stand in for an aggregate's tenant
// scope (administrative bypass is TENANT-008's scope, not TENANT-003's): it
// cannot declare a spawn, cannot command a tenant-bound entity, and cannot
// scope an erasure.
func TestAdministrativeScopeIsNeverAnAggregateTenantScope(t *testing.T) {
	specs.Describe(t, "an administrative TenantContext is never an aggregate's tenant scope", func(s *specs.Spec) {
		bg := context.Background()
		adminCtx := context.WithValue(bg, administrativeScopeKey{}, true)

		s.It("an administrative-only resolver cannot bind a spawn", func(ctx *specs.Context) {
			store := connectedEventsStore(ctx)

			engine := newSpecsEngine(ctx, "Sample", store, WithTenantResolver(administrativeScopeResolver{}))
			ctx.Expect(engine.Start(bg)).To(specs.BeNil())

			err := engine.Entity(adminCtx, newTenancyProbeEventSourcedBehavior(uuid.NewString()))
			ctx.Expect(err).To(specs.MatchError(ErrSpawnTenantUndetermined))
		})

		s.It("an administrative command is rejected by a tenant-bound entity", func(ctx *specs.Context) {
			store := connectedEventsStore(ctx)

			engine := newSpecsEngine(ctx, "Sample", store, WithTenantResolver(administrativeScopeResolver{}))
			ctx.Expect(engine.Start(bg)).To(specs.BeNil())

			entityID := uuid.NewString()
			probe := newTenancyProbeEventSourcedBehavior(entityID)
			ctx.Expect(engine.Entity(bg, probe, WithTenant(tenancy.TenantID("acme")))).To(specs.BeNil())

			_, _, err := engine.SendCommand(adminCtx, entityID, &testpb.CreateAccount{AccountBalance: 500}, time.Minute)
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			// HandleCommand must never run under an administrative scope
			ctx.Expect(probe.InvocationCount()).To(specs.BeZero())

			acme, err := persistence.NewTenantScope("acme")
			ctx.Expect(err).To(specs.BeNil())
			latest, err := store.GetLatestEvent(bg, acme, entityID)
			ctx.Expect(err).To(specs.BeNil())
			// nothing may be persisted under the entity's tenant
			ctx.Expect(latest).To(specs.BeNil())
		})

		s.It("an administrative erasure is denied and erases nothing", func(ctx *specs.Context) {
			store := connectedEventsStore(ctx)

			persistenceID := uuid.NewString()
			eventAny, err := anypb.New(&testpb.AccountCreated{AccountId: persistenceID, AccountBalance: 100})
			ctx.Expect(err).To(specs.BeNil())
			acme, err := persistence.NewTenantScope("acme")
			ctx.Expect(err).To(specs.BeNil())
			scopes := []persistence.Scope{persistence.Unscoped(), acme}
			for _, scope := range scopes {
				ctx.Expect(store.WriteEvents(bg, scope, []*egopb.Event{{
					PersistenceId:  persistenceID,
					SequenceNumber: 1,
					Event:          eventAny,
					Timestamp:      time.Now().UnixNano(),
				}}, persistence.Unconditional())).To(specs.BeNil())
			}

			engine := newSpecsEngine(ctx, "Sample", store, WithTenantResolver(administrativeScopeResolver{}))
			ctx.Expect(engine.Start(bg)).To(specs.BeNil())

			ctx.Expect(engine.EraseEntity(adminCtx, persistenceID, true)).To(specs.MatchError(tenancy.ErrDenied))

			// an administrative erasure must not touch any scope
			var erased []string
			for _, scope := range scopes {
				latest, err := store.GetLatestEvent(bg, scope, persistenceID)
				ctx.Expect(err).To(specs.BeNil())
				if latest == nil {
					erased = append(erased, scope.String())
				}
			}
			ctx.Expect(erased).To(specs.BeEmpty())
		})
	})
}
