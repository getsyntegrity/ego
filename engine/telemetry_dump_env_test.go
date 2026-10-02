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

package engine

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

// TestTelemetryDumpPathPrefersUrdVariable checks that URD_TELEMETRY_CONTRACT_DUMP
// wins over the legacy EGO_TELEMETRY_CONTRACT_DUMP when both are set.
func TestTelemetryDumpPathPrefersUrdVariable(t *testing.T) {
	t.Setenv(telemetryDumpEnv, "/new")
	t.Setenv(legacyTelemetryDumpEnv, "/old")
	specs.Describe(t, "telemetryDumpPath with both variables set", func(s *specs.Spec) {
		s.It("returns the URD path and reports no legacy use", func(ctx *specs.Context) {
			path, legacy := telemetryDumpPath()
			ctx.Expect(path).To(specs.Equal("/new"))
			ctx.Expect(legacy).To(specs.BeFalse())
		})
	})
}

// TestTelemetryDumpPathFallsBackToEgoVariable checks that the legacy
// EGO_TELEMETRY_CONTRACT_DUMP is still honored when the URD name is unset.
func TestTelemetryDumpPathFallsBackToEgoVariable(t *testing.T) {
	t.Setenv(telemetryDumpEnv, "")
	t.Setenv(legacyTelemetryDumpEnv, "/old")
	specs.Describe(t, "telemetryDumpPath with only the legacy variable set", func(s *specs.Spec) {
		s.It("returns the EGO path and reports legacy use", func(ctx *specs.Context) {
			path, legacy := telemetryDumpPath()
			ctx.Expect(path).To(specs.Equal("/old"))
			ctx.Expect(legacy).To(specs.BeTrue())
		})
	})
}
