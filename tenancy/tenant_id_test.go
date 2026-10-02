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

package tenancy_test

import (
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/urd/tenancy"
)

func TestNewTenantID_RejectsEmpty(t *testing.T) {
	specs.Describe(t, "NewTenantID rejects an empty identifier", func(s *specs.Spec) {
		s.It("fails with ErrInvalid", func(ctx *specs.Context) {
			_, err := tenancy.NewTenantID("")
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
		})
	})
}

func TestNewTenantID_RejectsWhitespaceOnly(t *testing.T) {
	specs.Describe(t, "NewTenantID rejects a whitespace-only identifier", func(s *specs.Spec) {
		s.It("fails with ErrInvalid", func(ctx *specs.Context) {
			_, err := tenancy.NewTenantID("   ")
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
		})
	})
}

func TestNewTenantID_RejectsLeadingOrTrailingWhitespace(t *testing.T) {
	specs.Describe(t, "NewTenantID rejects surrounding whitespace", func(s *specs.Spec) {
		s.It("fails with ErrInvalid", func(ctx *specs.Context) {
			_, err := tenancy.NewTenantID("  acme-corp  ")
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
		})
	})
}

func TestNewTenantID_AcceptsInteriorWhitespace(t *testing.T) {
	specs.Describe(t, "NewTenantID accepts interior whitespace", func(s *specs.Spec) {
		s.It("keeps the identifier unchanged", func(ctx *specs.Context) {
			// R1 rejects "surrounding space", not "any whitespace" — an interior
			// space (e.g. a human-readable tenant name) is a legitimate identifier.
			id, err := tenancy.NewTenantID("Acme Europe")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(id).ToEqual(tenancy.TenantID("Acme Europe"))
		})
	})
}

func TestNewTenantID_RejectsControlRune(t *testing.T) {
	specs.Describe(t, "NewTenantID rejects a control rune", func(s *specs.Spec) {
		s.It("fails with ErrInvalid", func(ctx *specs.Context) {
			_, err := tenancy.NewTenantID("acme\x00corp")
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
		})
	})
}

func TestNewTenantID_RejectsTabAndNewline(t *testing.T) {
	specs.Describe(t, "NewTenantID rejects tabs and newlines", func(s *specs.Spec) {
		s.It("fails with ErrInvalid", func(ctx *specs.Context) {
			_, err := tenancy.NewTenantID("acme\tcorp\n")
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
		})
	})
}

func TestNewTenantID_RejectsInvalidUTF8(t *testing.T) {
	specs.Describe(t, "NewTenantID rejects invalid UTF-8", func(s *specs.Spec) {
		s.It("fails with ErrInvalid", func(ctx *specs.Context) {
			invalid := string([]byte{0xff, 0xfe, 0xfd})
			_, err := tenancy.NewTenantID(invalid)
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
		})
	})
}

func TestNewTenantID_RejectsTooLong(t *testing.T) {
	specs.Describe(t, "NewTenantID rejects an identifier over the length limit", func(s *specs.Spec) {
		s.It("fails with ErrInvalid at 129 bytes", func(ctx *specs.Context) {
			tooLong := strings.Repeat("a", 129)
			_, err := tenancy.NewTenantID(tooLong)
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
		})
	})
}

func TestNewTenantID_AcceptsMaxLength(t *testing.T) {
	specs.Describe(t, "NewTenantID accepts an identifier at the length limit", func(s *specs.Spec) {
		s.It("keeps a 128-byte identifier unchanged", func(ctx *specs.Context) {
			maxLen := strings.Repeat("a", 128)
			id, err := tenancy.NewTenantID(maxLen)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(id).ToEqual(tenancy.TenantID(maxLen))
		})
	})
}

func TestNewTenantID_AcceptsArbitraryNonUUIDIdentifiers(t *testing.T) {
	specs.Describe(t, "NewTenantID accepts arbitrary non-UUID identifiers", func(s *specs.Spec) {
		specs.Table(s, []string{
			"acme-corp",
			"customer_42",
			"12345",
			"Tenant.With.Dots",
			"a",
		}, func(in string) string { return in }, func(ctx *specs.Context, in string) {
			id, err := tenancy.NewTenantID(in)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(id).ToEqual(tenancy.TenantID(in))
		})
	})
}

func TestNewTenantID_DoesNotNormalizeCase(t *testing.T) {
	specs.Describe(t, "NewTenantID does not case-fold the identifier", func(s *specs.Spec) {
		s.It("keeps mixed case unchanged (R1)", func(ctx *specs.Context) {
			id, err := tenancy.NewTenantID("Acme-Corp")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(id).ToEqual(tenancy.TenantID("Acme-Corp"))
		})
	})
}
