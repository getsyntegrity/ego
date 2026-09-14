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

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxTenantIDBytes bounds TenantID length. This is a validation cap, not a
// format mandate: any UTF-8 string within this budget, free of surrounding
// whitespace and control runes, is an acceptable tenant identifier.
const maxTenantIDBytes = 128

// TenantID is an opaque tenant identifier. It is a defined type, not an
// alias over string, buying compile-time isolation from other identifiers
// (persistence IDs, entity IDs, saga IDs) that remain bare strings. It
// carries no format mandate — no UUID requirement, no case-folding.
type TenantID string

// NewTenantID validates s and returns a TenantID.
//
// Validation is strict and never normalizes (R1: validate, don't
// normalize): no trimming, no case-folding. s is rejected when it is
// empty, not valid UTF-8, longer than 128 bytes, has leading or
// trailing whitespace, or contains a control rune anywhere. Interior
// whitespace (e.g. "Acme Europe") is a legitimate tenant identifier per
// R1's ratified scope — "surrounding space", not "any whitespace" — and
// is accepted. Any other non-empty string is accepted verbatim.
func NewTenantID(s string) (TenantID, error) {
	if s == "" {
		return "", newError(ReasonInvalid, "tenancy: tenant id must not be empty", nil)
	}
	if !utf8.ValidString(s) {
		return "", newError(ReasonInvalid, "tenancy: tenant id must be valid UTF-8", nil)
	}
	if len(s) > maxTenantIDBytes {
		return "", newError(ReasonInvalid, fmt.Sprintf("tenancy: tenant id exceeds %d bytes", maxTenantIDBytes), nil)
	}
	if strings.TrimSpace(s) != s {
		return "", newError(ReasonInvalid, "tenancy: tenant id must not have leading or trailing whitespace", nil)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", newError(ReasonInvalid, "tenancy: tenant id must not contain control characters", nil)
		}
	}
	return TenantID(s), nil
}
