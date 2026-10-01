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

package main

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

// Plain-stdlib check helpers for this package's tests. "must*" helpers end the
// test on failure (Errorf + FailNow); "expect*" helpers report and let it go on.

func describe(msgAndArgs []any) string {
	if len(msgAndArgs) == 0 {
		return ""
	}
	if format, ok := msgAndArgs[0].(string); ok {
		return ": " + fmt.Sprintf(format, msgAndArgs[1:]...)
	}
	return ": " + fmt.Sprint(msgAndArgs...)
}

func fail(t testing.TB, fatal bool, format string, args ...any) {
	t.Helper()
	t.Errorf(format, args...)
	if fatal {
		t.FailNow()
	}
}

func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	}
	return false
}

func isEmpty(v any) bool {
	if isNil(v) {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
		return rv.Len() == 0
	}
	return rv.IsZero()
}

// convertibleEqual compares like reflect-based EqualValues checks: equal, or equal after
// converting expected to the actual's type.
func convertibleEqual(expected, actual any) bool {
	if reflect.DeepEqual(expected, actual) {
		return true
	}
	ev, av := reflect.ValueOf(expected), reflect.ValueOf(actual)
	if ev.IsValid() && av.IsValid() && ev.Type().ConvertibleTo(av.Type()) {
		return reflect.DeepEqual(ev.Convert(av.Type()).Interface(), actual)
	}
	return false
}

func check(t testing.TB, fatal, ok bool, format string, args ...any) {
	t.Helper()
	if !ok {
		fail(t, fatal, format, args...)
	}
}

func mustNoError(t testing.TB, err error, m ...any) {
	t.Helper()
	check(t, true, err == nil, "unexpected error: %v%s", err, describe(m))
}

func mustError(t testing.TB, err error, m ...any) {
	t.Helper()
	check(t, true, err != nil, "expected an error but got none%s", describe(m))
}

func mustErrorIs(t testing.TB, err, target error, m ...any) {
	t.Helper()
	check(t, true, errors.Is(err, target), "error %v is not %v%s", err, target, describe(m))
}

func mustNil(t testing.TB, v any, m ...any) {
	t.Helper()
	check(t, true, isNil(v), "expected nil, got %v%s", v, describe(m))
}

func mustNotNil(t testing.TB, v any, m ...any) {
	t.Helper()
	check(t, true, !isNil(v), "expected a non-nil value%s", describe(m))
}

func mustTrue(t testing.TB, ok bool, m ...any) {
	t.Helper()
	check(t, true, ok, "expected true, got false%s", describe(m))
}

func mustEqual(t testing.TB, expected, actual any, m ...any) {
	t.Helper()
	check(t, true, reflect.DeepEqual(expected, actual), "not equal: expected %v, got %v%s", expected, actual, describe(m))
}

func mustEqualValues(t testing.TB, expected, actual any, m ...any) {
	t.Helper()
	check(t, true, convertibleEqual(expected, actual), "not equal: expected %v, got %v%s", expected, actual, describe(m))
}

func mustLen(t testing.TB, v any, n int, m ...any) {
	t.Helper()
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Map && rv.Kind() != reflect.String && rv.Kind() != reflect.Array && rv.Kind() != reflect.Chan) {
		fail(t, true, "cannot take len of %T%s", v, describe(m))
		return
	}
	check(t, true, rv.Len() == n, "expected length %d, got %d%s", n, rv.Len(), describe(m))
}

func mustPositive(t testing.TB, n int, m ...any) {
	t.Helper()
	check(t, true, n > 0, "expected a positive value, got %d%s", n, describe(m))
}

func mustEmpty(t testing.TB, v any, m ...any) {
	t.Helper()
	check(t, true, isEmpty(v), "expected empty, got %v%s", v, describe(m))
}

func expectEmpty(t testing.TB, v any, m ...any) {
	t.Helper()
	check(t, false, isEmpty(v), "expected empty, got %v%s", v, describe(m))
}

func expectTrue(t testing.TB, ok bool, m ...any) {
	t.Helper()
	check(t, false, ok, "expected true, got false%s", describe(m))
}

func expectEqual(t testing.TB, expected, actual any, m ...any) {
	t.Helper()
	check(t, false, reflect.DeepEqual(expected, actual), "not equal: expected %v, got %v%s", expected, actual, describe(m))
}

func expectEqualValues(t testing.TB, expected, actual any, m ...any) {
	t.Helper()
	check(t, false, convertibleEqual(expected, actual), "not equal: expected %v, got %v%s", expected, actual, describe(m))
}
