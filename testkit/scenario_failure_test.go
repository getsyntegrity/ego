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

package testkit

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	testpb "github.com/getsyntegrity/ego/test/data/testpb"
)

// messageTB records what a scenario assertion reports: every Errorf message and
// whether the assertion aborted (FailNow). It lets the failure contract of the
// scenarios be asserted: which failures stop the test (fatal) and which let it
// continue (non-fatal), and the text a user reads.
type messageTB struct {
	testing.TB
	fatal    bool
	messages []string
}

func (m *messageTB) Errorf(format string, args ...any) {
	m.messages = append(m.messages, fmt.Sprintf(format, args...))
}
func (m *messageTB) Helper() {}
func (m *messageTB) FailNow() {
	m.fatal = true
	runtime.Goexit()
}

// observe runs assert against a messageTB on its own goroutine (a fatal failure
// ends it through runtime.Goexit) and returns what was reported.
func observe(t testing.TB, assert func(t testing.TB)) *messageTB {
	recorder := &messageTB{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		assert(recorder)
	}()
	<-done
	return recorder
}

type failureRow struct {
	name    string
	run     func(t testing.TB)
	fatal   bool
	message string
}

func TestScenarioFailureReporting(t *testing.T) {
	specs.Describe(t, "scenario assertions report failures with a stable fatal/non-fatal contract", func(s *specs.Spec) {
		es := func() *EventSourcedScenario {
			return ForEventSourcedBehavior(&accountEventSourcedBehavior{id: "acc-1"})
		}
		created := func() *EventSourcedScenarioResult {
			return es().When(&testpb.CreateAccount{AccountBalance: 10})
		}
		wrongEntity := func() *EventSourcedScenarioResult {
			return es().When(&testpb.CreditAccount{AccountId: "other", Balance: 1})
		}
		brokenArrangement := func() *EventSourcedScenarioResult {
			return es().GivenEvents(&testpb.TestNoEvent{}).When(&testpb.CreateAccount{AccountBalance: 10})
		}
		ds := func() *DurableStateScenario {
			return ForDurableStateBehavior(&accountDurableStateBehavior{id: "ds-1"})
		}
		dsCreated := func() *DurableStateScenarioResult {
			return ds().When(&testpb.CreateAccount{AccountBalance: 10})
		}
		dsWrongEntity := func() *DurableStateScenarioResult {
			return ds().When(&testpb.CreditAccount{AccountId: "other", Balance: 1})
		}

		rows := []failureRow{
			{"ES ThenEvents wrong count", func(t testing.TB) { created().ThenEvents(t) }, true, "unexpected number of events: expected 0, got 1"},
			{"ES ThenEvents wrong event", func(t testing.TB) { created().ThenEvents(t, &testpb.AccountCreated{AccountId: "x"}) }, false, "event at index 0: expected"},
			{"ES ThenEvents command error", func(t testing.TB) { wrongEntity().ThenEvents(t) }, true, "command processing returned an error: "},
			{"ES ThenEvents broken arrangement", func(t testing.TB) { brokenArrangement().ThenEvents(t) }, true, "scenario arrangement failed: given events could not be applied"},
			{"ES ThenState wrong state", func(t testing.TB) { created().ThenState(t, &testpb.Account{AccountId: "x"}) }, false, "state mismatch: expected"},
			{"ES ThenState command error", func(t testing.TB) { wrongEntity().ThenState(t, &testpb.Account{}) }, true, "command processing returned an error: "},
			{"ES ThenError none", func(t testing.TB) { created().ThenError(t, "boom") }, true, "expected an error but got none"},
			{"ES ThenError wrong substring", func(t testing.TB) { wrongEntity().ThenError(t, "zzz") }, false, `error "`},
			{"ES ThenNoEvents with events", func(t testing.TB) { created().ThenNoEvents(t) }, false, "expected no events but got 1"},
			{"ES ThenNoEvents command error", func(t testing.TB) { wrongEntity().ThenNoEvents(t) }, true, "command processing returned an error: "},
			{"DS ThenState wrong state", func(t testing.TB) { dsCreated().ThenState(t, &testpb.Account{AccountId: "x"}) }, false, "state mismatch: expected"},
			{"DS ThenState command error", func(t testing.TB) { dsWrongEntity().ThenState(t, &testpb.Account{}) }, true, "command processing returned an error: "},
			{"DS ThenVersion wrong version", func(t testing.TB) { dsCreated().ThenVersion(t, 9) }, false, "version mismatch: expected 9, got 1"},
			{"DS ThenVersion command error", func(t testing.TB) { dsWrongEntity().ThenVersion(t, 1) }, true, "command processing returned an error: "},
			{"DS ThenError none", func(t testing.TB) { dsCreated().ThenError(t, "boom") }, true, "expected an error but got none"},
			{"DS ThenError wrong substring", func(t testing.TB) { dsWrongEntity().ThenError(t, "zzz") }, false, `error "`},
		}

		specs.Table(s, rows, func(r failureRow) string { return r.name }, func(ctx *specs.Context, r failureRow) {
			got := observe(ctx.T, r.run)
			ctx.Expect(got.fatal).To(specs.Equal(r.fatal))
			ctx.Expect(got.messages).To(specs.Not(specs.BeEmpty()))
			ctx.Expect(got.messages[0]).To(specs.Contain(r.message))
		})
	})
}
