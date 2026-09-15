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
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxOperationIDBytes bounds OperationID length, reusing
// tenancy.NewTenantID's own validation cap and rules verbatim (D8: this is
// a validation cap, not a format mandate).
const maxOperationIDBytes = 128

// OperationID identifies one dispatched command instance. It is a defined
// type, not an alias of string, so it cannot be confused at compile time
// with CorrelationID, CausationID or a future idempotency key (W4):
// OperationID carries no retry semantics — it names *this* operation, not
// a stable logical intent.
type OperationID string

// CorrelationID identifies the logical flow an operation belongs to; it
// persists across every operation in that flow. It is a distinct concept
// from tenancy.Administrative.CorrelationID(), an unrelated
// administrative-attribution accessor reachable only in
// tenancy.ScopeAdministrative: a command CorrelationID is a defined type
// present on every command envelope, never a bare string reachable only
// through an administrative attribution.
type CorrelationID string

// CausationID identifies the operation that directly caused another
// operation. It is absent on a root operation with no parent.
type CausationID string

// NewOperationID validates s and returns an OperationID.
//
// Validation reuses tenancy.NewTenantID's rules verbatim: s is rejected
// when it is empty, not valid UTF-8, longer than 128 bytes, has leading
// or trailing whitespace, or contains a control rune anywhere. Interior
// whitespace is accepted.
func NewOperationID(s string) (OperationID, error) {
	if s == "" {
		return "", NewError(ErrInvalidMetadata, "command: operation id must not be empty", nil)
	}
	if !utf8.ValidString(s) {
		return "", NewError(ErrInvalidMetadata, "command: operation id must be valid UTF-8", nil)
	}
	if len(s) > maxOperationIDBytes {
		return "", NewError(ErrInvalidMetadata, fmt.Sprintf("command: operation id exceeds %d bytes", maxOperationIDBytes), nil)
	}
	if strings.TrimSpace(s) != s {
		return "", NewError(ErrInvalidMetadata, "command: operation id must not have leading or trailing whitespace", nil)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", NewError(ErrInvalidMetadata, "command: operation id must not contain control characters", nil)
		}
	}
	return OperationID(s), nil
}

// GenerateOperationID returns a fresh, randomly generated OperationID: 128
// bits of crypto/rand entropy, hex-encoded. It never collides with a
// caller-supplied identifier in practice and always satisfies
// NewOperationID's own validation rules.
func GenerateOperationID() (OperationID, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", NewError(ErrInvalidMetadata, "command: failed to generate operation id", err)
	}
	return OperationID(hex.EncodeToString(buf)), nil
}
