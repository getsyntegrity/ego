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

package command

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pablogore/ego/v4/tenancy"
)

// maxCustomValueBytes bounds a custom metadata value's length (D6).
const maxCustomValueBytes = 1024

// reservedCustomPrefix is the blanket framework reservation covering every
// canonical namespace (D6): ego.tenant.*, ego.cmd.* and WRITE-005's future
// ego.idem.*.
const reservedCustomPrefix = "ego."

// reservedCustomKeys names every canonical Metadata field so a custom key
// can never shadow one, even unprefixed (D6, AC6).
var reservedCustomKeys = map[string]struct{}{
	"operation_id":   {},
	"correlation_id": {},
	"causation_id":   {},
	"timestamp":      {},
	"deadline":       {},
	"principal_id":   {},
	"principal_kind": {},
	"tenant":         {},
}

// Metadata is the canonical, runtime-independent metadata carried by every
// command envelope: operation identity, correlation, causation, an
// optional tenant slot composing tenancy.TenantContext, an optional
// principal slot, governed custom metadata, and temporal fields. Every
// field is unexported; Metadata can only be built through NewMetadata or
// Derive, never a bare struct literal.
type Metadata struct {
	operationID   OperationID
	correlationID CorrelationID
	causationID   CausationID
	hasCausation  bool
	tenant        tenancy.TenantContext
	hasTenant     bool
	principal     Principal
	hasPrincipal  bool
	custom        map[string]string
	timestamp     time.Time
	deadline      time.Time
	hasDeadline   bool
}

// MetadataOption configures optional Metadata fields at construction or
// derivation time.
type MetadataOption func(*Metadata) error

// WithCorrelationID sets the logical flow identity. At the root, when this
// option is omitted, correlation defaults to CorrelationID(op).
func WithCorrelationID(id CorrelationID) MetadataOption {
	return func(m *Metadata) error {
		m.correlationID = id
		return nil
	}
}

// WithTenant attaches tc, tenancy/'s own type, as this metadata's tenant
// slot (W3): command never defines or duplicates a tenant-identity type.
func WithTenant(tc tenancy.TenantContext) MetadataOption {
	return func(m *Metadata) error {
		m.tenant = tc
		m.hasTenant = true
		return nil
	}
}

// WithPrincipal attaches the abstract security identity issuing the
// command (AC5).
func WithPrincipal(p Principal) MetadataOption {
	return func(m *Metadata) error {
		m.principal = p
		m.hasPrincipal = true
		return nil
	}
}

// WithCustom sets a governed custom metadata key/value pair (D6, AC6). key
// is rejected with ErrReservedKey when it is prefixed "ego.", is an exact
// canonical field name, or fails NewOperationID's validation rules reused
// verbatim (non-empty, valid UTF-8, <=128 bytes, no surrounding
// whitespace, no control runes). value must be valid UTF-8, <=1024 bytes
// and contain no control runes.
func WithCustom(key, value string) MetadataOption {
	return func(m *Metadata) error {
		if err := validateCustomKey(key); err != nil {
			return err
		}
		if err := validateCustomValue(value); err != nil {
			return err
		}
		if m.custom == nil {
			m.custom = make(map[string]string)
		}
		m.custom[key] = value
		return nil
	}
}

// WithTimestamp overrides the default creation timestamp
// (time.Now().UTC()) with t.
func WithTimestamp(t time.Time) MetadataOption {
	return func(m *Metadata) error {
		m.timestamp = t
		return nil
	}
}

// WithDeadline sets an optional deadline for the operation. Deadline
// enforcement is out of scope of this package (AC7); only recognition is
// defined here, via Deadline's presence and value.
func WithDeadline(t time.Time) MetadataOption {
	return func(m *Metadata) error {
		m.deadline = t
		m.hasDeadline = true
		return nil
	}
}

// NewMetadata builds a root Metadata for op. Correlation defaults to
// CorrelationID(op) unless overridden by WithCorrelationID; the root
// carries no causation. Timestamp defaults to time.Now().UTC() unless
// overridden by WithTimestamp.
func NewMetadata(op OperationID, opts ...MetadataOption) (Metadata, error) {
	m := Metadata{
		operationID:   op,
		correlationID: CorrelationID(op),
		timestamp:     time.Now().UTC(),
	}
	for _, opt := range opts {
		if err := opt(&m); err != nil {
			return Metadata{}, err
		}
	}
	return m, nil
}

// OperationID returns the identity of this operation instance.
func (m Metadata) OperationID() OperationID {
	return m.operationID
}

// CorrelationID returns the logical flow this operation belongs to.
func (m Metadata) CorrelationID() CorrelationID {
	return m.correlationID
}

// CausationID returns the operation that directly caused this one. The
// second return value is false for a root operation with no parent.
func (m Metadata) CausationID() (CausationID, bool) {
	return m.causationID, m.hasCausation
}

// Tenant returns this metadata's tenant slot, if attached.
func (m Metadata) Tenant() (tenancy.TenantContext, bool) {
	return m.tenant, m.hasTenant
}

// Principal returns this metadata's principal slot, if attached.
func (m Metadata) Principal() (Principal, bool) {
	return m.principal, m.hasPrincipal
}

// Custom returns a defensive copy of this metadata's governed custom
// key/value pairs; mutating the returned map never affects m.
func (m Metadata) Custom() map[string]string {
	out := make(map[string]string, len(m.custom))
	for k, v := range m.custom {
		out[k] = v
	}
	return out
}

// CustomValue returns the custom value stored under key, if any.
func (m Metadata) CustomValue(key string) (string, bool) {
	v, ok := m.custom[key]
	return v, ok
}

// Timestamp returns this operation's creation time.
func (m Metadata) Timestamp() time.Time {
	return m.timestamp
}

// Deadline returns this operation's optional deadline, if set.
func (m Metadata) Deadline() (time.Time, bool) {
	return m.deadline, m.hasDeadline
}

// Derive builds a child Metadata for a new operation op from m (D7):
// correlation is inherited unchanged, causation is set to m's
// OperationID, and a fresh timestamp is assigned. op must differ from m's
// OperationID (ErrSameOperationID otherwise). Tenant, principal and
// deadline are inherited unless overridden by opts; a tenant override
// that switches tenants fails via tenancy.VerifyUnchanged
// (tenancy.ErrDenied, W3), and a deadline override that extends m's
// deadline fails with ErrDeadlineExtension. Custom metadata is never
// inherited (D7) — opts must re-supply it for the child.
func (m Metadata) Derive(op OperationID, opts ...MetadataOption) (Metadata, error) {
	if op == m.operationID {
		return Metadata{}, NewError(ErrSameOperationID, "command: derived operation id must differ from parent", nil)
	}

	child := Metadata{
		operationID:   op,
		correlationID: m.correlationID,
		causationID:   CausationID(m.operationID),
		hasCausation:  true,
		tenant:        m.tenant,
		hasTenant:     m.hasTenant,
		principal:     m.principal,
		hasPrincipal:  m.hasPrincipal,
		deadline:      m.deadline,
		hasDeadline:   m.hasDeadline,
		timestamp:     time.Now().UTC(),
	}

	parentDeadline, parentHasDeadline := m.deadline, m.hasDeadline

	for _, opt := range opts {
		if err := opt(&child); err != nil {
			return Metadata{}, err
		}
	}

	if child.hasTenant && m.hasTenant {
		if err := tenancy.VerifyUnchanged(m.tenant, child.tenant); err != nil {
			return Metadata{}, err
		}
	}

	if child.hasDeadline && parentHasDeadline && child.deadline.After(parentDeadline) {
		return Metadata{}, NewError(ErrDeadlineExtension, "command: derived deadline must not extend the parent's", nil)
	}

	return child, nil
}

// validateCustomKey rejects a custom metadata key reserved by D6: any key
// prefixed "ego.", any exact canonical field name, and any key failing
// NewOperationID's shape rules reused verbatim.
func validateCustomKey(key string) error {
	if strings.HasPrefix(key, reservedCustomPrefix) {
		return NewError(ErrReservedKey, "command: custom metadata key must not use the reserved \"ego.\" prefix", nil)
	}
	if _, reserved := reservedCustomKeys[key]; reserved {
		return NewError(ErrReservedKey, "command: custom metadata key must not shadow a canonical field", nil)
	}
	if key == "" {
		return NewError(ErrReservedKey, "command: custom metadata key must not be empty", nil)
	}
	if !utf8.ValidString(key) {
		return NewError(ErrReservedKey, "command: custom metadata key must be valid UTF-8", nil)
	}
	if len(key) > maxOperationIDBytes {
		return NewError(ErrReservedKey, "command: custom metadata key exceeds the maximum length", nil)
	}
	if strings.TrimSpace(key) != key {
		return NewError(ErrReservedKey, "command: custom metadata key must not have leading or trailing whitespace", nil)
	}
	for _, r := range key {
		if unicode.IsControl(r) {
			return NewError(ErrReservedKey, "command: custom metadata key must not contain control characters", nil)
		}
	}
	return nil
}

// validateCustomValue enforces D6's value rules: valid UTF-8, <=1024
// bytes, no control runes.
func validateCustomValue(value string) error {
	if !utf8.ValidString(value) {
		return NewError(ErrInvalidMetadata, "command: custom metadata value must be valid UTF-8", nil)
	}
	if len(value) > maxCustomValueBytes {
		return NewError(ErrInvalidMetadata, "command: custom metadata value exceeds the maximum length", nil)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return NewError(ErrInvalidMetadata, "command: custom metadata value must not contain control characters", nil)
		}
	}
	return nil
}
