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

	"github.com/getsyntegrity/ego/internal/extensions"
)

// engRestErrText is err's message, or "" for nil, so a text expectation on a
// missing error fails on the expectation instead of panicking.
func engRestErrText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestMissingRequiredExtensionsSentinel(t *testing.T) {
	specs.Describe(t, "ErrMissingRequiredExtensions is the sentinel owned by the extensions package", func(s *specs.Spec) {
		s.It("is the same error value under the public name and states the missing extensions", func(ctx *specs.Context) {
			// Actors outside this package (internal/engine/...) wrap the sentinel
			// owned by internal/extensions; callers match it through the public name.
			ctx.Expect(extensions.ErrMissingRequiredExtensions == ErrMissingRequiredExtensions).To(specs.BeTrue()) //nolint:errorlint // identity is the point
			ctx.Expect(engRestErrText(ErrMissingRequiredExtensions)).ToEqual("actor system is missing required ego extensions")
		})
	})
}
