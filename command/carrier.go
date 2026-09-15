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

	"github.com/pablogore/ego/v4/tenancy"
)

// Canonical ego.cmd.* keys Carrier uses to carry Metadata's fields (D9).
// The tenant slot round-trips through tenancy's own ego.tenant.* keys
// (tenancy.MarshalMetadata/UnmarshalMetadata) rather than a duplicate
// serialization (W3). Custom metadata keys are carried unprefixed — D6
// already guarantees they can never collide with these reserved names or
// with the "ego." prefix.
const (
	carrierKeyOperationID   = "ego.cmd.operation_id"
	carrierKeyCorrelationID = "ego.cmd.correlation_id"
	carrierKeyCausationID   = "ego.cmd.causation_id"
	carrierKeyTimestamp     = "ego.cmd.timestamp"
	carrierKeyDeadline      = "ego.cmd.deadline"
	carrierKeyPrincipalID   = "ego.cmd.principal_id"
	carrierKeyPrincipalKind = "ego.cmd.principal_kind"
)

// carrierEgoPrefix namespaces every key this package or tenancy recognizes.
// Any "ego."-prefixed key Unmarshal does not recognize is ignored rather
// than rejected (forward compatibility); it is never folded into custom
// metadata.
const carrierEgoPrefix = "ego."

// Carrier is a flat string-map carry format for Metadata (D9): a Go-side
// representation that crosses boundaries a context.Context does not
// survive (e.g. goakt.SendSync, which accepts only proto.Message). It
// carries canonical metadata only — never the command payload; see
// Envelope.Payload, which has no Carrier path.
type Carrier map[string]string

// MarshalMetadata serializes m into a Carrier using ego.cmd.* keys for its
// own fields, tenancy.MarshalMetadata's ego.tenant.* keys for the tenant
// slot (unchanged, no duplicate serialization), and custom metadata keys
// carried unprefixed as-is.
func MarshalMetadata(m Metadata) Carrier {
	c := Carrier{
		carrierKeyOperationID:   string(m.OperationID()),
		carrierKeyCorrelationID: string(m.CorrelationID()),
		carrierKeyTimestamp:     m.Timestamp().Format(time.RFC3339Nano),
	}
	if causationID, ok := m.CausationID(); ok {
		c[carrierKeyCausationID] = string(causationID)
	}
	if deadline, ok := m.Deadline(); ok {
		c[carrierKeyDeadline] = deadline.Format(time.RFC3339Nano)
	}
	if principal, ok := m.Principal(); ok {
		c[carrierKeyPrincipalID] = principal.ID()
		if kind, ok := principal.Kind(); ok {
			c[carrierKeyPrincipalKind] = kind
		}
	}
	if tenant, ok := m.Tenant(); ok {
		for k, v := range tenancy.MarshalMetadata(tenant) {
			c[k] = v
		}
	}
	for k, v := range m.Custom() {
		c[k] = v
	}
	return c
}

// UnmarshalMetadata reconstructs a Metadata from c, re-running the same
// construction and validation rules NewMetadata/WithCustom apply directly
// — this is the trust-boundary constructor for metadata arriving over a
// carried path. operation_id, correlation_id and timestamp are required
// (ErrInvalidMetadata otherwise); causation_id, deadline and the principal
// slot are recognized only when present. A key shadowing a canonical field
// name, or a custom value that fails D6's rules, fails the same way
// WithCustom would. Any other "ego."-prefixed key Unmarshal does not
// recognize is ignored, not rejected (forward compatibility); every
// remaining key is treated as custom metadata.
func UnmarshalMetadata(c Carrier) (Metadata, error) {
	opID, ok := c[carrierKeyOperationID]
	if !ok {
		return Metadata{}, NewError(ErrInvalidMetadata, "command: carrier missing operation id", nil)
	}
	op, err := NewOperationID(opID)
	if err != nil {
		return Metadata{}, err
	}

	correlationID, ok := c[carrierKeyCorrelationID]
	if !ok {
		return Metadata{}, NewError(ErrInvalidMetadata, "command: carrier missing correlation id", nil)
	}

	rawTimestamp, ok := c[carrierKeyTimestamp]
	if !ok {
		return Metadata{}, NewError(ErrInvalidMetadata, "command: carrier missing timestamp", nil)
	}
	timestamp, err := time.Parse(time.RFC3339Nano, rawTimestamp)
	if err != nil {
		return Metadata{}, NewError(ErrInvalidMetadata, "command: carrier timestamp is not a valid RFC3339 timestamp", err)
	}

	opts := []MetadataOption{
		WithCorrelationID(CorrelationID(correlationID)),
		WithTimestamp(timestamp),
	}

	if rawDeadline, ok := c[carrierKeyDeadline]; ok {
		deadline, err := time.Parse(time.RFC3339Nano, rawDeadline)
		if err != nil {
			return Metadata{}, NewError(ErrInvalidMetadata, "command: carrier deadline is not a valid RFC3339 timestamp", err)
		}
		opts = append(opts, WithDeadline(deadline))
	}

	if principalID, ok := c[carrierKeyPrincipalID]; ok {
		principalOpts := []PrincipalOption(nil)
		if kind, ok := c[carrierKeyPrincipalKind]; ok {
			principalOpts = append(principalOpts, WithPrincipalKind(kind))
		}
		principal, err := NewPrincipal(principalID, principalOpts...)
		if err != nil {
			return Metadata{}, err
		}
		opts = append(opts, WithPrincipal(principal))
	}

	if tenantMetadata := tenantSubset(c); len(tenantMetadata) > 0 {
		tenant, err := tenancy.UnmarshalMetadata(tenantMetadata)
		if err != nil {
			return Metadata{}, err
		}
		opts = append(opts, WithTenant(tenant))
	}

	for k, v := range c {
		if strings.HasPrefix(k, carrierEgoPrefix) {
			continue
		}
		opts = append(opts, WithCustom(k, v))
	}

	m, err := NewMetadata(op, opts...)
	if err != nil {
		return Metadata{}, err
	}

	if causationID, ok := c[carrierKeyCausationID]; ok {
		m.causationID = CausationID(causationID)
		m.hasCausation = true
	}

	return m, nil
}

// tenantSubset extracts c's ego.tenant.* keys into a tenancy.Metadata,
// leaving everything else behind.
func tenantSubset(c Carrier) tenancy.Metadata {
	out := tenancy.Metadata{}
	for k, v := range c {
		if strings.HasPrefix(k, "ego.tenant.") {
			out[k] = v
		}
	}
	return out
}
