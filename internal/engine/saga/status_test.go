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

	"github.com/stretchr/testify/assert"

	"github.com/getsyntegrity/ego/egopb"
	runtimeport "github.com/getsyntegrity/ego/port/runtime"
)

// TestSagaStatusWireRoundTrip pins the runtimeport.SagaStatus <-> SagaLifecycleStatus
// mapping the saga actor and Engine.SagaStatus share (#153).
func TestSagaStatusWireRoundTrip(t *testing.T) {
	for _, status := range []runtimeport.SagaStatus{runtimeport.SagaRunning, runtimeport.SagaCompleted, runtimeport.SagaCompensating, runtimeport.SagaFailed} {
		t.Run(status.String(), func(t *testing.T) {
			wire := StatusToProto(status)
			assert.NotEqual(t, egopb.SagaLifecycleStatus_SAGA_LIFECYCLE_STATUS_NONE, wire, "the saga actor must always report a status")
			assert.Equal(t, status, StatusFromProto(wire))
		})
	}
	assert.Equal(t, runtimeport.SagaRunning, StatusFromProto(egopb.SagaLifecycleStatus(99)), "an unknown wire value reads as runtimeport.SagaRunning")
}
