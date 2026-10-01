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

package conformance

import (
	"errors"
	"fmt"
	"reflect"
)

// TestingT is the part of *testing.T a Check needs to report a failure. It
// is owned by this package so that the exported Check.Run signature does not
// depend on a third-party assertion library. *testing.T satisfies it, so a
// Check runs as a normal subtest without adaptation; the package's own
// recorder (see CaptureEventsStoreChecks) satisfies it too.
//
// FailNow MUST stop the calling goroutine, as *testing.T.FailNow does: the
// checks rely on it to not touch a store value after a failed assertion.
type TestingT interface {
	Errorf(format string, args ...any)
	FailNow()
	Helper()
}

// The require* helpers below are the stdlib-only assertions the checks use.
// Each one reports through t.Errorf and then stops with t.FailNow, so a
// failed assertion is fatal exactly like testify's require package was. The
// failure text is "<message>: <what was expected, what was seen>", where
// the optional message is the check's own explanation of the rule that was
// broken.

// fail reports one failure and stops the check.
func fail(t TestingT, detail string, msgAndArgs []any) {
	t.Helper()
	if msg := formatMessage(msgAndArgs); msg != "" {
		detail = msg + ": " + detail
	}
	t.Errorf("%s", detail)
	t.FailNow()
}

// formatMessage renders the optional message arguments: a format string
// followed by its arguments, or any other single value printed as is.
func formatMessage(msgAndArgs []any) string {
	switch len(msgAndArgs) {
	case 0:
		return ""
	case 1:
		if s, ok := msgAndArgs[0].(string); ok {
			return s
		}
		return fmt.Sprint(msgAndArgs[0])
	}
	if format, ok := msgAndArgs[0].(string); ok {
		return fmt.Sprintf(format, msgAndArgs[1:]...)
	}
	return fmt.Sprint(msgAndArgs...)
}

func isNilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return rv.IsNil()
	}
	return false
}

func requireNoError(t TestingT, err error, msgAndArgs ...any) {
	t.Helper()
	if err != nil {
		fail(t, fmt.Sprintf("expected no error, got: %v", err), msgAndArgs)
	}
}

func requireError(t TestingT, err error, msgAndArgs ...any) {
	t.Helper()
	if err == nil {
		fail(t, "expected an error, got nil", msgAndArgs)
	}
}

func requireErrorIs(t TestingT, err, target error, msgAndArgs ...any) {
	t.Helper()
	if !errors.Is(err, target) {
		fail(t, fmt.Sprintf("expected the error to match %q, got: %v", target, err), msgAndArgs)
	}
}

func requireTrue(t TestingT, ok bool, msgAndArgs ...any) {
	t.Helper()
	if !ok {
		fail(t, "expected true, got false", msgAndArgs)
	}
}

func requireNil(t TestingT, v any, msgAndArgs ...any) {
	t.Helper()
	if !isNilValue(v) {
		fail(t, fmt.Sprintf("expected nil, got: %+v", v), msgAndArgs)
	}
}

func requireNotNil(t TestingT, v any, msgAndArgs ...any) {
	t.Helper()
	if isNilValue(v) {
		fail(t, "expected a non-nil value, got nil", msgAndArgs)
	}
}

func requireEqual(t TestingT, expected, actual any, msgAndArgs ...any) {
	t.Helper()
	if !reflect.DeepEqual(expected, actual) {
		fail(t, fmt.Sprintf("not equal\nexpected: %#v\nactual:   %#v", expected, actual), msgAndArgs)
	}
}

func requireEmpty(t TestingT, v any, msgAndArgs ...any) {
	t.Helper()
	if isNilValue(v) {
		return
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
		if rv.Len() == 0 {
			return
		}
	default:
		if rv.IsZero() {
			return
		}
	}
	fail(t, fmt.Sprintf("expected empty, got: %+v", v), msgAndArgs)
}

// requireElementsMatch checks that expected and actual hold the same
// elements, ignoring order. Both must be slices or arrays.
func requireElementsMatch(t TestingT, expected, actual any, msgAndArgs ...any) {
	t.Helper()
	if isNilValue(expected) && isNilValue(actual) {
		return
	}
	ev, av := reflect.ValueOf(expected), reflect.ValueOf(actual)
	if (ev.Kind() != reflect.Slice && ev.Kind() != reflect.Array) || (av.Kind() != reflect.Slice && av.Kind() != reflect.Array) {
		fail(t, fmt.Sprintf("expected two slices, got %T and %T", expected, actual), msgAndArgs)
		return
	}
	used := make([]bool, av.Len())
	matched := ev.Len() == av.Len()
	for i := 0; matched && i < ev.Len(); i++ {
		found := false
		for j := 0; j < av.Len(); j++ {
			if !used[j] && reflect.DeepEqual(ev.Index(i).Interface(), av.Index(j).Interface()) {
				used[j], found = true, true
				break
			}
		}
		matched = found
	}
	if !matched {
		fail(t, fmt.Sprintf("elements differ\nexpected: %#v\nactual:   %#v", expected, actual), msgAndArgs)
	}
}

// requireNotContains checks that the slice, array or string container does
// not hold element.
func requireNotContains(t TestingT, container, element any, msgAndArgs ...any) {
	t.Helper()
	cv := reflect.ValueOf(container)
	switch cv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < cv.Len(); i++ {
			if reflect.DeepEqual(cv.Index(i).Interface(), element) {
				fail(t, fmt.Sprintf("expected %#v not to contain %#v", container, element), msgAndArgs)
				return
			}
		}
	default:
		fail(t, fmt.Sprintf("expected a slice or array, got %T", container), msgAndArgs)
	}
}

func requireLessOrEqual(t TestingT, actual, limit int, msgAndArgs ...any) {
	t.Helper()
	if actual > limit {
		fail(t, fmt.Sprintf("expected %d to be less than or equal to %d", actual, limit), msgAndArgs)
	}
}

func requireGreaterOrEqual(t TestingT, actual, limit int, msgAndArgs ...any) {
	t.Helper()
	if actual < limit {
		fail(t, fmt.Sprintf("expected %d to be greater than or equal to %d", actual, limit), msgAndArgs)
	}
}
