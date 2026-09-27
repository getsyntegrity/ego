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

package compose

import "fmt"

// ValidationError is one problem Spec.Validate found. Validate joins every
// problem it finds with errors.Join, so a caller sees all of them at once;
// each one names the rule it broke and the offending Spec field.
type ValidationError struct {
	// Rule is the validation rule that failed: "V1" through "V7" for
	// Spec.Validate, as defined in openspec/changes/ego-arch-003/design.md
	// §D4a (V7, a non-negative ShutdownTimeout, was added in IMPL-4), or a
	// runtime-specific rule a composition root checks on top of them, such
	// as compose/goakt's "G1" and "G2".
	Rule string
	// Field names the offending Spec field, indexed or keyed when the
	// problem is one element of a slice or map, for example "EventsStore",
	// "EventPublishers[2]" or `Projections["orders"].Handler`.
	Field string
	// Problem says what is wrong with Field, in a short phrase.
	Problem string
}

// Error implements error.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("compose: Spec.%s: %s (%s)", e.Field, e.Problem, e.Rule)
}

// StartError is returned by a composition root's Start when one of its
// ordered start steps fails (design §D6). It names the step that failed,
// the error that step returned, and — separately — anything that went
// wrong while undoing the steps that had already run, so a caller can tell
// a clean rollback from a partial one.
type StartError struct {
	// Step names the start step that failed, for example "probe stores".
	Step string
	// Err is the error the failed step returned.
	Err error
	// Rollback joins every error returned while undoing the earlier steps
	// and releasing resources not yet attached; nil when rollback was
	// clean.
	Rollback error
}

// Error implements error.
func (e *StartError) Error() string {
	if e.Rollback == nil {
		return fmt.Sprintf("compose: start step %q failed: %v", e.Step, e.Err)
	}
	return fmt.Sprintf("compose: start step %q failed: %v; rollback: %v", e.Step, e.Err, e.Rollback)
}

// Unwrap returns the step error and, when present, the rollback error, so
// errors.Is and errors.As see both.
func (e *StartError) Unwrap() []error {
	errs := make([]error, 0, 2)
	if e.Err != nil {
		errs = append(errs, e.Err)
	}
	if e.Rollback != nil {
		errs = append(errs, e.Rollback)
	}
	return errs
}
