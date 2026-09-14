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

import "errors"

// Reason classifies why a tenancy operation failed. The zero value is
// intentionally invalid; every constructed *Error carries one of the
// named reasons below.
type Reason uint8

const (
	_ Reason = iota
	// ReasonMissing indicates a required tenant identity was not present
	// where one was required (e.g. Require on a context with none attached).
	ReasonMissing
	// ReasonInvalid indicates a malformed or unusable tenant identity or
	// administrative attribution (e.g. an empty TenantID).
	ReasonInvalid
	// ReasonDenied indicates a policy rejected an otherwise well-formed
	// request, such as attempting to change an already-bound tenant
	// identity while crossing a boundary.
	ReasonDenied
)

// String returns a lower-case, human-readable name for the reason.
func (r Reason) String() string {
	switch r {
	case ReasonMissing:
		return "missing"
	case ReasonInvalid:
		return "invalid"
	case ReasonDenied:
		return "denied"
	default:
		return "unknown"
	}
}

// Error is the structured error type returned by the tenancy package.
// Callers classify a failure with errors.Is against ErrMissing, ErrInvalid,
// or ErrDenied, and inspect it further with errors.As against *Error.
//
// Error can only be constructed by this package (unexported fields,
// unexported constructors); callers never build one directly.
type Error struct {
	reason    Reason
	message   string
	tenant    TenantID
	hasTenant bool
	cause     error
}

// newError builds a tenancy error for the given reason, with no tenant
// attribution. cause may be nil.
func newError(reason Reason, message string, cause error) *Error {
	return &Error{reason: reason, message: message, cause: cause}
}

// newErrorWithTenant builds a tenancy error attributed to a specific
// tenant. cause may be nil.
func newErrorWithTenant(reason Reason, tenant TenantID, message string, cause error) *Error {
	return &Error{reason: reason, message: message, tenant: tenant, hasTenant: true, cause: cause}
}

// Error implements the error interface.
func (e *Error) Error() string {
	return e.message
}

// Reason reports the classification of this error.
func (e *Error) Reason() Reason {
	return e.reason
}

// Tenant returns the tenant identity this error is attributed to, if any.
// The second return value is false when the error carries no tenant
// attribution.
func (e *Error) Tenant() (TenantID, bool) {
	return e.tenant, e.hasTenant
}

// Unwrap returns the underlying cause, if any. This lets errors.Is/As
// traverse resolver- or store-specific errors (e.g. a store's own
// "unknown tenant" error) wrapped by this Error.
func (e *Error) Unwrap() error {
	return e.cause
}

// Is reports whether target is the tenancy sentinel error matching this
// error's Reason (ErrMissing, ErrInvalid, or ErrDenied), enabling
// errors.Is(err, tenancy.ErrInvalid) style classification.
func (e *Error) Is(target error) bool {
	switch e.reason {
	case ReasonMissing:
		return target == ErrMissing
	case ReasonInvalid:
		return target == ErrInvalid
	case ReasonDenied:
		return target == ErrDenied
	default:
		return false
	}
}

// Sentinel errors classifying tenancy failures. Use errors.Is to test a
// returned error against these; use errors.As against *Error for detail
// such as Reason() or Tenant().
var (
	// ErrMissing indicates a required tenant identity was not present.
	ErrMissing error = errors.New("tenancy: tenant identity missing")
	// ErrInvalid indicates a tenant identity or input was malformed.
	ErrInvalid error = errors.New("tenancy: tenant identity invalid")
	// ErrDenied indicates a tenant identity change or mismatch was rejected.
	ErrDenied error = errors.New("tenancy: tenant identity denied")
)
