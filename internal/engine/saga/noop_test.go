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

package saga

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

// TestSagaActionIsNoop covers the helper that replaced the unexported method
// (*sagaAction).isNoop, which cannot stay a method once sagaAction is declared
// in port/behavior (design.md §5.2).
func TestSagaActionIsNoop(t *testing.T) {
	specs.Describe(t, "actionIsNoop reports whether a saga action asks for nothing", func(s *specs.Spec) {
		type noopCase struct {
			name   string
			action *sagaAction
			noop   bool
		}
		specs.Table(s, []noopCase{
			{name: "nil action", action: nil, noop: true},
			{name: "empty action", action: &sagaAction{}, noop: true},
			{name: "empty slices", action: &sagaAction{Commands: []sagaCommand{}, Events: []Event{}}, noop: true},
			{name: "command", action: &sagaAction{Commands: []sagaCommand{{EntityID: "a"}}}, noop: false},
			{name: "event", action: &sagaAction{Events: []Event{nil}}, noop: false},
			{name: "complete", action: &sagaAction{Complete: true}, noop: false},
			{name: "compensate", action: &sagaAction{Compensate: true}, noop: false},
		}, func(c noopCase) string { return c.name }, func(ctx *specs.Context, c noopCase) {
			ctx.Expect(actionIsNoop(c.action)).ToEqual(c.noop)
		})
	})
}
