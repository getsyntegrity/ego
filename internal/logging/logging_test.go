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

package logging

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
	kitlog "github.com/pablogore/kit-logger/pkg/logger"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultLoggerIsKitLoggerGlobal(t *testing.T) {
	specs.Describe(t, "DefaultLogger resolves the kit-logger global on every call", func(s *specs.Spec) {
		s.It("returns the current global logger, looked up per call and never cached", func(ctx *specs.Context) {
			ctx.Expect(kitlog.L() == DefaultLogger()).To(specs.BeTrue())

			previous := kitlog.L()
			ctx.T.Cleanup(func() { kitlog.SetGlobal(previous) })

			custom := kitlogtest.NewMockLogger()
			kitlog.SetGlobal(custom)
			ctx.Expect(DefaultLogger() == kitlog.Logger(custom)).To(specs.BeTrue())
		})
	})
}

func TestResolveLogger(t *testing.T) {
	specs.Describe(t, "ResolveLogger falls back to the default for an unusable logger", func(s *specs.Spec) {
		s.It("nil falls back to the default", func(ctx *specs.Context) {
			ctx.Expect(ResolveLogger(nil) == DefaultLogger()).To(specs.BeTrue())
		})

		s.It("typed nil falls back to the default", func(ctx *specs.Context) {
			var typedNil *kitlogtest.MockLogger
			ctx.Expect(ResolveLogger(typedNil) == DefaultLogger()).To(specs.BeTrue())
		})

		s.It("a usable logger is returned as-is", func(ctx *specs.Context) {
			logger := kitlogtest.NewMockLogger()
			ctx.Expect(ResolveLogger(logger) == kitlog.Logger(logger)).To(specs.BeTrue())
		})
	})
}

// TestLoggingStaysRuntimeNeutral guards the reason this package exists:
// migration resolves its logger here precisely because the dependency
// closure carries no actor runtime. The architecture-checker rules only see direct
// imports, so this asserts the transitive closure via go list -deps.
func TestLoggingStaysRuntimeNeutral(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("the go tool is not on PATH")
	}

	out, err := exec.Command(goBin, "list", "-deps", ".").CombinedOutput()
	require.NoError(t, err, "go list -deps failed: %s", out)

	deps := strings.Fields(string(out))
	require.NotEmpty(t, deps)
	for _, dep := range deps {
		assert.Falsef(t, strings.HasPrefix(dep, "github.com/tochemey/goakt"),
			"internal/logging must not depend on GoAkt, directly or transitively; found %s", dep)
		assert.Falsef(t, strings.HasSuffix(dep, "/internal/goaktlog"),
			"internal/logging must not depend on the GoAkt logging seam; found %s", dep)
	}
}
