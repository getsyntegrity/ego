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

package projection

import (
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
)

func TestNewRecovery_Defaults(t *testing.T) {
	specs.Describe(t, "NewRecovery without options uses the defaults", func(s *specs.Spec) {
		s.It("has 5 retries, a 1s delay and the Fail policy", func(ctx *specs.Context) {
			r := NewRecovery()
			ctx.Expect(r.Retries()).ToEqual(uint64(5))
			ctx.Expect(r.RetryDelay()).ToEqual(time.Second)
			ctx.Expect(r.RecoveryPolicy()).ToEqual(Fail)
		})
	})
}

func TestNewRecovery_WithAllOptions(t *testing.T) {
	specs.Describe(t, "NewRecovery applies every option it is given", func(s *specs.Spec) {
		s.It("overrides retries, delay and policy", func(ctx *specs.Context) {
			r := NewRecovery(
				WithRetries(10),
				WithRetryDelay(2*time.Second),
				WithRecoveryPolicy(RetryAndSkip),
			)
			ctx.Expect(r.Retries()).ToEqual(uint64(10))
			ctx.Expect(r.RetryDelay()).ToEqual(2 * time.Second)
			ctx.Expect(r.RecoveryPolicy()).ToEqual(RetryAndSkip)
		})
	})
}

// recoveryOptionCase is one option together with the Recovery it must produce when applied to a zero value.
type recoveryOptionCase struct {
	name     string
	option   RecoveryOption
	expected Recovery
}

func TestRecoveryOption(t *testing.T) {
	specs.Describe(t, "A RecoveryOption sets only its own field on a Recovery", func(s *specs.Spec) {
		ts := time.Second
		specs.Table(s, []recoveryOptionCase{
			{
				name:     "WithRetries",
				option:   WithRetries(5),
				expected: Recovery{retries: 5},
			},
			{
				name:     "WithRetryDelay",
				option:   WithRetryDelay(ts),
				expected: Recovery{retryDelay: ts},
			},
			{
				name:     "WithRecoveryPolicy",
				option:   WithRecoveryPolicy(Fail),
				expected: Recovery{policy: Fail},
			},
		}, func(c recoveryOptionCase) string { return c.name }, func(ctx *specs.Context, c recoveryOptionCase) {
			var e Recovery
			c.option.Apply(&e)
			ctx.Expect(e).ToEqual(c.expected)
		})
	})
}
