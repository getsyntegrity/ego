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

package persistence_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/getsyntegrity/urd/persistence"
)

func TestWritePreconditionZeroValueIsInvalid(t *testing.T) {
	specs.Describe(t, "the zero WritePrecondition", func(s *specs.Spec) {
		s.It("is invalid, neither unconditional nor genesis, and renders as unspecified", func(ctx *specs.Context) {
			var zero persistence.WritePrecondition

			ctx.Expect(zero.Valid()).To(specs.BeFalse())
			ctx.Expect(zero.IsUnconditional()).To(specs.BeFalse())
			ctx.Expect(zero.IsGenesis()).To(specs.BeFalse())

			_, ok := zero.Revision()
			ctx.Expect(ok).To(specs.BeFalse())

			ctx.Expect(zero.String()).ToEqual("unspecified")
		})
	})
}

func TestWritePreconditionUnconditional(t *testing.T) {
	specs.Describe(t, "Unconditional precondition", func(s *specs.Spec) {
		s.It("is valid, unconditional, not genesis, carries no revision and renders as unconditional", func(ctx *specs.Context) {
			p := persistence.Unconditional()

			ctx.Expect(p.Valid()).To(specs.BeTrue())
			ctx.Expect(p.IsUnconditional()).To(specs.BeTrue())
			ctx.Expect(p.IsGenesis()).To(specs.BeFalse())

			_, ok := p.Revision()
			ctx.Expect(ok).To(specs.BeFalse())

			ctx.Expect(p.String()).ToEqual("unconditional")
		})
	})
}

func TestWritePreconditionExpectGenesis(t *testing.T) {
	specs.Describe(t, "ExpectGenesis precondition", func(s *specs.Spec) {
		s.It("is valid, genesis, not unconditional, carries no revision and renders as genesis", func(ctx *specs.Context) {
			p := persistence.ExpectGenesis()

			ctx.Expect(p.Valid()).To(specs.BeTrue())
			ctx.Expect(p.IsUnconditional()).To(specs.BeFalse())
			ctx.Expect(p.IsGenesis()).To(specs.BeTrue())

			_, ok := p.Revision()
			ctx.Expect(ok).To(specs.BeFalse())

			ctx.Expect(p.String()).ToEqual("genesis")
		})
	})
}

func TestWritePreconditionExpectRevision(t *testing.T) {
	specs.Describe(t, "ExpectRevision precondition", func(s *specs.Spec) {
		s.It("is valid, neither unconditional nor genesis, carries its revision and renders it", func(ctx *specs.Context) {
			p := persistence.ExpectRevision(42)

			ctx.Expect(p.Valid()).To(specs.BeTrue())
			ctx.Expect(p.IsUnconditional()).To(specs.BeFalse())
			ctx.Expect(p.IsGenesis()).To(specs.BeFalse())

			revision, ok := p.Revision()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(revision).ToEqual(uint64(42))

			ctx.Expect(p.String()).ToEqual("42")
		})
	})
}

// ExpectRevision(0) is a distinct, meaningful precondition — an exact-revision
// check against revision zero — and must never collapse into Unconditional()
// or ExpectGenesis(). No magic-number sentinel is permitted for "unconditional".
func TestWritePreconditionExpectRevisionZeroIsNotGenesisOrUnconditional(t *testing.T) {
	specs.Describe(t, "ExpectRevision(0) precondition", func(s *specs.Spec) {
		s.It("is distinct from Unconditional and ExpectGenesis", func(ctx *specs.Context) {
			p := persistence.ExpectRevision(0)

			ctx.Expect(p.IsUnconditional()).To(specs.BeFalse())
			ctx.Expect(p.IsGenesis()).To(specs.BeFalse())

			revision, ok := p.Revision()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(revision).ToEqual(uint64(0))

			ctx.Expect(p.String()).ToEqual("0")
			ctx.Expect(p).To(specs.NotEqual(persistence.Unconditional()))
			ctx.Expect(p).To(specs.NotEqual(persistence.ExpectGenesis()))
		})
	})
}

func TestWritePreconditionComparable(t *testing.T) {
	specs.Describe(t, "WritePrecondition values", func(s *specs.Spec) {
		s.It("compare by value", func(ctx *specs.Context) {
			ctx.Expect(persistence.Unconditional()).ToEqual(persistence.Unconditional())
			ctx.Expect(persistence.ExpectGenesis()).ToEqual(persistence.ExpectGenesis())
			ctx.Expect(persistence.ExpectRevision(7)).ToEqual(persistence.ExpectRevision(7))
			ctx.Expect(persistence.ExpectRevision(7)).To(specs.NotEqual(persistence.ExpectRevision(8)))
			ctx.Expect(persistence.WritePrecondition{}).To(specs.NotEqual(persistence.Unconditional()))
		})
	})
}
