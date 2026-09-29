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

	"github.com/stretchr/testify/require"

	"github.com/getsyntegrity/ego/port/runtime"
)

func TestSagaStatusString(t *testing.T) {
	cases := []struct {
		status runtime.SagaStatus
		want   string
	}{
		{runtime.SagaRunning, "running"},
		{runtime.SagaCompleted, "completed"},
		{runtime.SagaCompensating, "compensating"},
		{runtime.SagaFailed, "failed"},
		{runtime.SagaStatus(99), "unknown"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, tc.status.String())
	}
}

func TestEnumValuesAreUnchanged(t *testing.T) {
	require.Equal(t, 0, int(runtime.RoundRobin))
	require.Equal(t, 1, int(runtime.Random))
	require.Equal(t, 2, int(runtime.Local))
	require.Equal(t, 3, int(runtime.LeastLoad))
	require.Equal(t, 0, int(runtime.StopDirective))
	require.Equal(t, 1, int(runtime.RestartDirective))
	require.Equal(t, 0, int(runtime.SagaRunning))
	require.Equal(t, 1, int(runtime.SagaCompleted))
	require.Equal(t, 2, int(runtime.SagaCompensating))
	require.Equal(t, 3, int(runtime.SagaFailed))
}
