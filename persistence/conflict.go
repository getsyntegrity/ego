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

package persistence

import (
	"errors"
	"strconv"
	"strings"

	"github.com/pablogore/ego/v4/tenancy"
)

// ErrConcurrencyConflict is the sentinel a ConflictError matches via
// errors.Is. It never carries persistence identity by itself; use
// errors.As to recover the concrete *ConflictError.
var ErrConcurrencyConflict = errors.New("ego: concurrency conflict")

// ErrInvalidPrecondition is returned when a WritePrecondition's zero value
// (or any other non-constructed value) is passed to a conditional write.
var ErrInvalidPrecondition = errors.New("persistence: write precondition is not valid")

// ErrPreconditionScope is returned when a conditional WriteEvents batch
// (i.e. one whose precondition is not Unconditional()) spans more than one
// PersistenceId, including when the batch is empty.
var ErrPreconditionScope = errors.New("persistence: conditional write spans multiple persistence ids")

// conflictGrammarPrefix is the fixed, parseable prefix of ConflictError's
// canonical wire message, including its grammar version. ParseConflictError
// is its exact inverse. It begins with ErrConcurrencyConflict's text, which
// is what reply classification matches on (reply_classification.go), so a
// message in any grammar version still classifies as a concurrency
// conflict even when it cannot be parsed back into a *ConflictError.
const conflictGrammarPrefix = "ego: concurrency conflict: grammar=v1, scope="

// Fixed field separators of the v1 grammar. Variable fields (the tenant id
// and the persistence id) are always Go-quoted (strconv.Quote), so none of
// these can appear unescaped inside them.
const (
	conflictTenantScopePrefix  = "tenant:"
	conflictPersistenceIDField = ", persistence_id="
	conflictExpectedField      = ", expected="
	conflictActualField        = ", actual="
	conflictUnknownActualToken = "unknown"
	conflictUnscopedToken      = "unscoped"
)

// ConflictOption configures a ConflictError at construction time.
type ConflictOption func(*ConflictError)

// WithActualRevision attaches the actual StorageRevision the store observed
// at the failed compare. It MUST be supplied only when the store cheaply
// learned that revision as part of the failed conditional write — never
// invented from a stale local counter.
func WithActualRevision(revision uint64) ConflictOption {
	return func(e *ConflictError) {
		e.actualRevision = revision
		e.hasActual = true
	}
}

// ConflictError reports that a conditional write's WritePrecondition did not
// hold against the persisted revision. It is identifiable both via
// errors.As (to recover persistence identity, scope, and the declared
// precondition) and via errors.Is(err, ErrConcurrencyConflict).
type ConflictError struct {
	scope          Scope
	persistenceID  string
	expected       WritePrecondition
	actualRevision uint64
	hasActual      bool
}

// NewConflictError builds a ConflictError for (scope, persistenceID),
// recording the precondition that failed to hold. scope is a required
// parameter — following persistence.Scope's own philosophy (see its doc
// comment) of forcing every caller to state it explicitly rather than
// silently falling through to a meaningful default — so that a conflict is
// always attributable to the tenant boundary the failed write targeted.
// Apply WithActualRevision only when the actual StorageRevision was cheaply
// observed at the failed compare.
func NewConflictError(scope Scope, persistenceID string, expected WritePrecondition, opts ...ConflictOption) *ConflictError {
	e := &ConflictError{scope: scope, persistenceID: persistenceID, expected: expected}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Scope returns the tenant boundary the failed conditional write targeted.
func (e *ConflictError) Scope() Scope {
	return e.scope
}

// PersistenceID returns the identity of the aggregate the conditional write
// targeted.
func (e *ConflictError) PersistenceID() string {
	return e.persistenceID
}

// Expected returns the WritePrecondition that was declared and did not hold.
func (e *ConflictError) Expected() WritePrecondition {
	return e.expected
}

// ActualRevision returns the StorageRevision the store observed at the
// failed compare, and whether that value is in fact known. It is false when
// the store could not cheaply learn the actual revision.
func (e *ConflictError) ActualRevision() (revision uint64, ok bool) {
	return e.actualRevision, e.hasActual
}

// Is reports whether target is ErrConcurrencyConflict, so that
// errors.Is(err, ErrConcurrencyConflict) matches any *ConflictError.
func (e *ConflictError) Is(target error) bool {
	return errors.Is(target, ErrConcurrencyConflict) || target == ErrConcurrencyConflict
}

// Error renders e using the canonical v1 conflict grammar:
//
//	ego: concurrency conflict: grammar=v1, scope=<unscoped|tenant:"<id>">, persistence_id="<id>", expected=<unconditional|genesis|N>, actual=<M|unknown>
//
// Both identifiers are rendered with strconv.Quote, an exact reversible
// encoding: a tenant id or persistence id may contain any text the tenancy
// and persistence packages accept — commas, equals signs, quotes, the
// grammar's own field separators, non-ASCII text — without making the
// message ambiguous. The scope is rendered from its kind and TenantID(),
// never from Scope.String(), which stays a diagnostic rendering only.
// ParseConflictError is its exact inverse.
func (e *ConflictError) Error() string {
	actual := conflictUnknownActualToken
	if e.hasActual {
		actual = strconv.FormatUint(e.actualRevision, 10)
	}
	return conflictGrammarPrefix + formatConflictScope(e.scope) +
		conflictPersistenceIDField + strconv.Quote(e.persistenceID) +
		conflictExpectedField + e.expected.String() +
		conflictActualField + actual
}

// formatConflictScope renders scope's v1 grammar token. The zero value
// renders as Scope.String()'s "unspecified", which ParseConflictError
// rejects rather than reconstructing an invalid Scope.
func formatConflictScope(scope Scope) string {
	switch {
	case scope.IsUnscoped():
		return conflictUnscopedToken
	case scope.Valid():
		return conflictTenantScopePrefix + strconv.Quote(string(scope.TenantID()))
	default:
		return scope.String()
	}
}

// ParseConflictError parses message produced by (*ConflictError).Error(),
// returning the reconstructed error and true on success. It is the exact
// inverse of Error(): for any *ConflictError e with a valid scope,
// ParseConflictError(e.Error()) reconstructs an equivalent error whose
// Error() is byte-identical. It returns false for any message that is not
// canonical v1 grammar, including a non-canonical quoting or number that
// Error() would never produce.
//
// Compatibility policy: only grammar=v1 is reconstructed. The
// pre-TENANT-003 rendering (no scope) and the earlier unversioned scope=
// rendering are rejected rather than guessed at — the first carries no
// scope to attribute, and the second is ambiguous for valid identifiers.
// Such a message still classifies as a concurrency conflict by its
// sentinel prefix, only without a recoverable cause. A future grammar
// change bumps the version, so an older parser rejects it the same way.
//
// This is error-message reconstruction confined to wire diagnostics (e.g.
// recovering a *ConflictError cause from an egopb.CommandReply's error
// message), never a storage or cache key.
func ParseConflictError(message string) (*ConflictError, bool) {
	rest, ok := strings.CutPrefix(message, conflictGrammarPrefix)
	if !ok {
		return nil, false
	}

	scope, rest, ok := cutConflictScope(rest)
	if !ok {
		return nil, false
	}

	rest, ok = strings.CutPrefix(rest, conflictPersistenceIDField)
	if !ok {
		return nil, false
	}
	persistenceID, rest, ok := cutQuoted(rest)
	if !ok {
		return nil, false
	}

	rest, ok = strings.CutPrefix(rest, conflictExpectedField)
	if !ok {
		return nil, false
	}
	expectedPart, actualPart, ok := strings.Cut(rest, conflictActualField)
	if !ok {
		return nil, false
	}

	expected, ok := parsePreconditionToken(expectedPart)
	if !ok {
		return nil, false
	}

	var opts []ConflictOption
	if actualPart != conflictUnknownActualToken {
		actual, ok := parseCanonicalUint(actualPart)
		if !ok {
			return nil, false
		}
		opts = append(opts, WithActualRevision(actual))
	}

	return NewConflictError(scope, persistenceID, expected, opts...), true
}

// cutConflictScope parses the scope token at the start of s and returns the
// remainder. It never produces the invalid zero value of Scope: an unknown
// token, or a tenant id that fails tenancy validation, is rejected.
func cutConflictScope(s string) (Scope, string, bool) {
	if rest, ok := strings.CutPrefix(s, conflictUnscopedToken); ok {
		return Unscoped(), rest, true
	}
	rest, ok := strings.CutPrefix(s, conflictTenantScopePrefix)
	if !ok {
		return Scope{}, "", false
	}
	tenantID, rest, ok := cutQuoted(rest)
	if !ok {
		return Scope{}, "", false
	}
	scope, err := NewTenantScope(tenancy.TenantID(tenantID))
	if err != nil {
		return Scope{}, "", false
	}
	return scope, rest, true
}

// cutQuoted parses the canonical strconv.Quote rendering at the start of s
// and returns the unquoted value and the remainder. A raw (backquoted)
// string or any escape strconv.Quote would not produce is rejected, so each
// value has exactly one accepted rendering.
func cutQuoted(s string) (string, string, bool) {
	if !strings.HasPrefix(s, `"`) {
		return "", "", false
	}
	quoted, err := strconv.QuotedPrefix(s)
	if err != nil {
		return "", "", false
	}
	value, err := strconv.Unquote(quoted)
	if err != nil || strconv.Quote(value) != quoted {
		return "", "", false
	}
	return value, s[len(quoted):], true
}

// parseCanonicalUint parses token as a base-10 uint64 only when it is the
// exact strconv.FormatUint rendering (no sign, no leading zeros).
func parseCanonicalUint(token string) (uint64, bool) {
	value, err := strconv.ParseUint(token, 10, 64)
	if err != nil || strconv.FormatUint(value, 10) != token {
		return 0, false
	}
	return value, true
}

// parsePreconditionToken is the exact inverse of WritePrecondition.String()
// for the tokens that grammar can produce (unconditional, genesis, N). It
// never produces the invalid zero value or a numeric sentinel for
// unconditional/genesis.
func parsePreconditionToken(token string) (WritePrecondition, bool) {
	switch token {
	case "unconditional":
		return Unconditional(), true
	case "genesis":
		return ExpectGenesis(), true
	default:
		revision, ok := parseCanonicalUint(token)
		if !ok {
			return WritePrecondition{}, false
		}
		return ExpectRevision(revision), true
	}
}
