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
	"context"
	"errors"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

// nopStore satisfies Lifecycle so a Check can run without a real store.
type nopStore struct{}

func (nopStore) Connect(context.Context) error    { return nil }
func (nopStore) Disconnect(context.Context) error { return nil }

// run executes one check through the captured path and returns its result
// together with whether the statement after the failing assertion ran.
func runCaptured(body func(t TestingT, reached *bool)) (CheckResult, bool) {
	reached := false
	checks := []Check[nopStore]{{
		Name: "probe",
		Run:  func(_ context.Context, t TestingT, _ nopStore) { body(t, &reached) },
	}}
	return captureChecks(checks, func() nopStore { return nopStore{} })[0], reached
}

var errBoom = errors.New("boom")

func TestReportingHelpers(t *testing.T) {
	type row struct {
		name      string
		body      func(t TestingT, reached *bool)
		wantFail  bool
		wantInMsg []string
	}
	rows := []row{
		{
			name:     "a passing assertion records nothing",
			body:     func(t TestingT, reached *bool) { requireNoError(t, nil); *reached = true },
			wantFail: false,
		},
		{
			name:      "NoError is fatal and shows the error and the message",
			body:      func(t TestingT, reached *bool) { requireNoError(t, errBoom, "writing %s", "x"); *reached = true },
			wantFail:  true,
			wantInMsg: []string{"writing x", "boom"},
		},
		{
			name: "Equal is fatal and shows expected and actual",
			body: func(t TestingT, reached *bool) {
				requireEqual(t, float64(111), float64(222), "own record")
				*reached = true
			},
			wantFail:  true,
			wantInMsg: []string{"own record", "expected", "111", "actual", "222"},
		},
		{
			name:      "Nil shows the unexpected value",
			body:      func(t TestingT, reached *bool) { requireNil(t, &nopStore{}, "must be empty"); *reached = true },
			wantFail:  true,
			wantInMsg: []string{"must be empty", "expected nil"},
		},
		{
			name:      "NotNil fails on a typed nil pointer",
			body:      func(t TestingT, reached *bool) { var p *nopStore; requireNotNil(t, p, "must exist"); *reached = true },
			wantFail:  true,
			wantInMsg: []string{"must exist", "expected a non-nil"},
		},
		{
			name:      "True shows the message",
			body:      func(t TestingT, reached *bool) { requireTrue(t, false, "same record"); *reached = true },
			wantFail:  true,
			wantInMsg: []string{"same record", "expected true"},
		},
		{
			name:      "Error fails when the error is nil",
			body:      func(t TestingT, reached *bool) { requireError(t, nil); *reached = true },
			wantFail:  true,
			wantInMsg: []string{"expected an error"},
		},
		{
			name:      "ErrorIs names the target",
			body:      func(t TestingT, reached *bool) { requireErrorIs(t, errBoom, errors.New("other")); *reached = true },
			wantFail:  true,
			wantInMsg: []string{"boom", "other"},
		},
		{
			name:      "Empty shows the offending value",
			body:      func(t TestingT, reached *bool) { requireEmpty(t, []string{"a"}, "no replay"); *reached = true },
			wantFail:  true,
			wantInMsg: []string{"no replay", "expected empty", "a"},
		},
		{
			name: "ElementsMatch reports both sides",
			body: func(t TestingT, reached *bool) {
				requireElementsMatch(t, []string{"a", "b"}, []string{"a", "c"})
				*reached = true
			},
			wantFail:  true,
			wantInMsg: []string{"expected", "a", "b", "actual", "c"},
		},
		{
			name:      "NotContains names the element",
			body:      func(t TestingT, reached *bool) { requireNotContains(t, []string{"a", "b"}, "b"); *reached = true },
			wantFail:  true,
			wantInMsg: []string{"b"},
		},
		{
			name:      "LessOrEqual shows both numbers",
			body:      func(t TestingT, reached *bool) { requireLessOrEqual(t, 5, 3, "pages"); *reached = true },
			wantFail:  true,
			wantInMsg: []string{"pages", "5", "3"},
		},
		{
			name:      "GreaterOrEqual shows both numbers",
			body:      func(t TestingT, reached *bool) { requireGreaterOrEqual(t, 1, 4, "pages"); *reached = true },
			wantFail:  true,
			wantInMsg: []string{"pages", "1", "4"},
		},
		{
			name:     "a message with a non-string argument is printed as is",
			body:     func(t TestingT, reached *bool) { requireTrue(t, false, 42); *reached = true },
			wantFail: true, wantInMsg: []string{"42: expected true"},
		},
		{
			name:     "a message whose first argument is not a format string is joined",
			body:     func(t TestingT, reached *bool) { requireTrue(t, false, 1, 2); *reached = true },
			wantFail: true, wantInMsg: []string{"1 2: expected true"},
		},
		{
			name: "Nil accepts an untyped nil and a typed nil map",
			body: func(t TestingT, reached *bool) {
				var m map[string]int
				requireNil(t, nil)
				requireNil(t, m)
				*reached = true
			},
			wantFail: false,
		},
		{
			name:     "Nil rejects a non-nilable value",
			body:     func(t TestingT, reached *bool) { requireNil(t, 7); *reached = true },
			wantFail: true, wantInMsg: []string{"expected nil", "7"},
		},
		{
			name: "Empty accepts nil, an empty slice and a zero value",
			body: func(t TestingT, reached *bool) {
				requireEmpty(t, nil)
				requireEmpty(t, []int{})
				requireEmpty(t, 0)
				*reached = true
			},
			wantFail: false,
		},
		{
			name:     "Empty rejects a non-zero value",
			body:     func(t TestingT, reached *bool) { requireEmpty(t, 3); *reached = true },
			wantFail: true, wantInMsg: []string{"expected empty", "3"},
		},
		{
			name: "ElementsMatch ignores order and accepts two nils",
			body: func(t TestingT, reached *bool) {
				requireElementsMatch(t, []int{1, 2}, []int{2, 1})
				requireElementsMatch(t, nil, nil)
				*reached = true
			},
			wantFail: false,
		},
		{
			name:     "ElementsMatch fails on different lengths",
			body:     func(t TestingT, reached *bool) { requireElementsMatch(t, []int{1}, []int{1, 1}); *reached = true },
			wantFail: true, wantInMsg: []string{"elements differ"},
		},
		{
			name:     "ElementsMatch rejects non-slices",
			body:     func(t TestingT, reached *bool) { requireElementsMatch(t, 1, 2); *reached = true },
			wantFail: true, wantInMsg: []string{"expected two slices", "int"},
		},
		{
			name:     "NotContains passes when the element is absent",
			body:     func(t TestingT, reached *bool) { requireNotContains(t, []string{"a"}, "z"); *reached = true },
			wantFail: false,
		},
		{
			name:     "NotContains rejects a non-slice container",
			body:     func(t TestingT, reached *bool) { requireNotContains(t, 5, 1); *reached = true },
			wantFail: true, wantInMsg: []string{"expected a slice or array"},
		},
	}

	specs.Describe(t, "the conformance reporting helpers", func(s *specs.Spec) {
		specs.Table(s, rows, func(r row) string { return r.name }, func(ctx *specs.Context, r row) {
			res, reached := runCaptured(r.body)

			ctx.Expect(res.Failed).To(specs.Equal(r.wantFail))
			ctx.Expect(reached).To(specs.Equal(!r.wantFail)) // a failure is fatal: nothing after it runs
			if r.wantFail {
				ctx.Expect(res.Errors).To(specs.HaveLen(1))
				want := make([]any, len(r.wantInMsg))
				for i, w := range r.wantInMsg {
					want[i] = w
				}
				ctx.Expect(res.Errors[0]).To(specs.ContainAllOf(want...))
			}
		})
	})
}

// A *testing.T must satisfy TestingT: that is what lets the same Check run as
// a normal subtest.
var _ TestingT = (*testing.T)(nil)
var _ TestingT = (*captureTestingT)(nil)

// failingConnectStore reports Connect as failed so the captured path has a
// connection error to report.
type failingConnectStore struct{ nopStore }

func (failingConnectStore) Connect(context.Context) error { return errBoom }

func TestCaptureReportsAConnectFailure(t *testing.T) {
	specs.Describe(t, "captureChecks", func(s *specs.Spec) {
		s.It("reports a store that cannot connect as a failed check", func(ctx *specs.Context) {
			checks := []Check[failingConnectStore]{{Name: "never runs", Run: func(context.Context, TestingT, failingConnectStore) {
				ctx.T.Fatal("the check must not run against a store that failed to connect")
			}}}

			res := captureChecks(checks, func() failingConnectStore { return failingConnectStore{} })

			ctx.Expect(res).To(specs.Equal([]CheckResult{{Name: "never runs", Failed: true, Errors: []string{"connect: boom"}}}))
		})
	})
}
