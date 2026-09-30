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

package runtime_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/port/runtime"
)

func TestSagaStatusString(t *testing.T) {
	specs.Describe(t, "SagaStatus.String names each status and reports unknown for any other value", func(s *specs.Spec) {
		type reading struct {
			name   string
			status runtime.SagaStatus
			want   string
		}
		specs.Table(s, []reading{
			{"SagaRunning", runtime.SagaRunning, "running"},
			{"SagaCompleted", runtime.SagaCompleted, "completed"},
			{"SagaCompensating", runtime.SagaCompensating, "compensating"},
			{"SagaFailed", runtime.SagaFailed, "failed"},
			{"an undefined value", runtime.SagaStatus(99), "unknown"},
		}, func(tc reading) string { return tc.name + " reads " + tc.want }, func(ctx *specs.Context, tc reading) {
			ctx.Expect(tc.status.String()).ToEqual(tc.want)
		})
	})
}

func TestEnumValuesAreUnchanged(t *testing.T) {
	specs.Describe(t, "The exported enum values keep their numeric values", func(s *specs.Spec) {
		s.It("keeps placement, supervisor directive and saga status values", func(ctx *specs.Context) {
			ctx.Expect(int(runtime.RoundRobin)).ToEqual(0)
			ctx.Expect(int(runtime.Random)).ToEqual(1)
			ctx.Expect(int(runtime.Local)).ToEqual(2)
			ctx.Expect(int(runtime.LeastLoad)).ToEqual(3)
			ctx.Expect(int(runtime.StopDirective)).ToEqual(0)
			ctx.Expect(int(runtime.RestartDirective)).ToEqual(1)
			ctx.Expect(int(runtime.SagaRunning)).ToEqual(0)
			ctx.Expect(int(runtime.SagaCompleted)).ToEqual(1)
			ctx.Expect(int(runtime.SagaCompensating)).ToEqual(2)
			ctx.Expect(int(runtime.SagaFailed)).ToEqual(3)
		})
	})
}
