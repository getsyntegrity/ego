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

package tenancy

// Metadata keys used to carry a TenantContext across a boundary that does
// not preserve context.Context — e.g. a saga event/command (spec.md:
// "Resolve-Once, Propagate-After Discipline" requires the saga boundary to
// reconstruct identity from carried metadata, never implicit propagation).
// The "ego.tenant." prefix namespaces these keys against unrelated
// metadata a transport or event envelope may already carry.
const (
	metaKeyScope            = "ego.tenant.scope"
	metaKeyID               = "ego.tenant.id"
	metaKeyAdminActor       = "ego.tenant.admin_actor"
	metaKeyAdminReason      = "ego.tenant.admin_reason"
	metaKeyAdminCorrelation = "ego.tenant.admin_correlation_id"
	metaScopeValueTenant    = "tenant"
	metaScopeValueAdmin     = "administrative"
)

// Metadata is a flat string map carrying a serialized TenantContext. It is
// the wire/carry format for the saga-boundary reconstruction path (design.md
// Interfaces/Contracts); it deliberately mirrors the shape of event/command
// metadata maps already used at transport boundaries, rather than
// introducing a new envelope type here (durable envelope format itself is
// TENANT-002's scope, per proposal.md Scope).
type Metadata map[string]string

// MarshalMetadata serializes tc into Metadata using ego.tenant.* keys. A
// tenant-scoped context yields scope+id; an administrative context yields
// scope+admin_actor+admin_reason, plus admin_correlation_id only when tc's
// Administrative carries one. The zero-value TenantContext (invalid,
// unreachable through the constructors) marshals to an empty Metadata with
// no recognizable scope key, which UnmarshalMetadata then rejects.
func MarshalMetadata(tc TenantContext) Metadata {
	md := Metadata{}
	switch tc.Scope() {
	case ScopeTenant:
		id, _ := tc.Tenant()
		md[metaKeyScope] = metaScopeValueTenant
		md[metaKeyID] = string(id)
	case ScopeAdministrative:
		admin, _ := tc.Administrative()
		md[metaKeyScope] = metaScopeValueAdmin
		md[metaKeyAdminActor] = admin.Actor()
		md[metaKeyAdminReason] = admin.Reason()
		if correlationID, ok := admin.CorrelationID(); ok {
			md[metaKeyAdminCorrelation] = correlationID
		}
	}
	return md
}

// UnmarshalMetadata reconstructs a TenantContext from md, re-running the
// same construction and validation rules NewTenantContext/
// NewAdministrativeContext apply directly (R1: validate, don't normalize;
// this is the trust-boundary constructor for identity arriving over a
// carried-metadata path, so it is held to the exact same rules as any other
// entry point). It fails with ErrInvalid when the scope key is missing or
// unrecognized, or the fields required for that scope are absent or
// malformed.
func UnmarshalMetadata(md Metadata) (TenantContext, error) {
	switch md[metaKeyScope] {
	case metaScopeValueTenant:
		id, err := NewTenantID(md[metaKeyID])
		if err != nil {
			return TenantContext{}, err
		}
		return NewTenantContext(id)
	case metaScopeValueAdmin:
		admin, err := NewAdministrative(md[metaKeyAdminActor], md[metaKeyAdminReason])
		if err != nil {
			return TenantContext{}, err
		}
		if correlationID, ok := md[metaKeyAdminCorrelation]; ok && correlationID != "" {
			admin = admin.WithCorrelationID(correlationID)
		}
		return NewAdministrativeContext(admin)
	default:
		return TenantContext{}, newError(ReasonInvalid, "tenancy: metadata missing or unrecognized tenant scope", nil)
	}
}
