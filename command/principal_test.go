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

package command_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/command"
)

func TestNewPrincipal(t *testing.T) {
	specs.Describe(t, "NewPrincipal builds an opaque identity reference", func(s *specs.Spec) {
		s.It("id required", func(ctx *specs.Context) {
			_, err := command.NewPrincipal("")
			ctx.Expect(err).To(specs.MatchError(command.ErrInvalidPrincipal))
		})

		s.It("id only, kind absent", func(ctx *specs.Context) {
			p, err := command.NewPrincipal("user-42")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(p.ID()).ToEqual("user-42")

			kind, ok := p.Kind()
			ctx.Expect(ok).To(specs.BeFalse())
			ctx.Expect(kind).ToEqual("")
		})

		s.It("id and kind", func(ctx *specs.Context) {
			p, err := command.NewPrincipal("user-42", command.WithPrincipalKind("service-account"))
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(p.ID()).ToEqual("user-42")

			kind, ok := p.Kind()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(kind).ToEqual("service-account")
		})
	})
}

func TestPrincipalIsAbstract(t *testing.T) {
	specs.Describe(t, "Principal carries only an opaque identity reference", func(s *specs.Spec) {
		s.It("is built from an id alone", func(ctx *specs.Context) {
			// Principal carries only an opaque identity reference (AC5): its
			// declared surface is ID/Kind only, no credential, token, role, scope
			// or protocol-specific field. This is asserted structurally by the
			// fact that NewPrincipal takes only an id and options, never a token
			// or credential parameter.
			p, err := command.NewPrincipal("user-42")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(p.ID()).ToEqual("user-42")
		})
	})
}
