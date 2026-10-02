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

import behaviorport "github.com/getsyntegrity/urd/port/behavior"

// actionIsNoop reports whether the action has no observable effect: nothing
// to persist, no command to dispatch, no completion, no compensation. A saga
// behavior returns such an action for stream events it recognizes as
// irrelevant (SG4: this lets the caller skip tenant binding for events the
// saga was never going to act on). It is a function rather than a method
// because SagaAction is declared in port/behavior.
func actionIsNoop(a *behaviorport.SagaAction) bool {
	return a == nil || (len(a.Commands) == 0 && len(a.Events) == 0 && !a.Complete && !a.Compensate)
}
