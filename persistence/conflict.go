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
	"fmt"
	"strconv"
	"strings"
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
// canonical wire message. ParseConflictError is its exact inverse.
const conflictGrammarPrefix = "ego: concurrency conflict: persistence_id="

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
// errors.As (to recover persistence identity and the declared precondition)
// and via errors.Is(err, ErrConcurrencyConflict).
type ConflictError struct {
	persistenceID  string
	expected       WritePrecondition
	actualRevision uint64
	hasActual      bool
}

// NewConflictError builds a ConflictError for persistenceID, recording the
// precondition that failed to hold. Apply WithActualRevision only when the
// actual StorageRevision was cheaply observed at the failed compare.
func NewConflictError(persistenceID string, expected WritePrecondition, opts ...ConflictOption) *ConflictError {
	e := &ConflictError{persistenceID: persistenceID, expected: expected}
	for _, opt := range opts {
		opt(e)
	}
	return e
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

// Error renders e using the canonical conflict grammar:
//
//	ego: concurrency conflict: persistence_id=<id>, expected=<unconditional|genesis|N>, actual=<M|unknown>
//
// ParseConflictError is its exact inverse.
func (e *ConflictError) Error() string {
	actual := "unknown"
	if e.hasActual {
		actual = strconv.FormatUint(e.actualRevision, 10)
	}
	return fmt.Sprintf("%s%s, expected=%s, actual=%s", conflictGrammarPrefix, e.persistenceID, e.expected.String(), actual)
}

// ParseConflictError parses message produced by (*ConflictError).Error(),
// returning the reconstructed error and true on success. It is the exact
// inverse of Error(): for any *ConflictError e, ParseConflictError(e.Error())
// reconstructs an equivalent error. It returns false for any message that
// does not match the canonical grammar.
func ParseConflictError(message string) (*ConflictError, bool) {
	rest, ok := strings.CutPrefix(message, conflictGrammarPrefix)
	if !ok {
		return nil, false
	}

	idPart, rest, ok := strings.Cut(rest, ", expected=")
	if !ok {
		return nil, false
	}

	expectedPart, actualPart, ok := strings.Cut(rest, ", actual=")
	if !ok {
		return nil, false
	}

	expected, ok := parsePreconditionToken(expectedPart)
	if !ok {
		return nil, false
	}

	var opts []ConflictOption
	if actualPart != "unknown" {
		actual, err := strconv.ParseUint(actualPart, 10, 64)
		if err != nil {
			return nil, false
		}
		opts = append(opts, WithActualRevision(actual))
	}

	return NewConflictError(idPart, expected, opts...), true
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
		revision, err := strconv.ParseUint(token, 10, 64)
		if err != nil {
			return WritePrecondition{}, false
		}
		return ExpectRevision(revision), true
	}
}
