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
	"errors"
	"fmt"
	"testing"

	"github.com/getsyntegrity/ego/persistence"
	"github.com/getsyntegrity/ego/tenancy"
	"github.com/getsyntegrity/go-specs/specs"
)

func TestConflictErrorIdentifiableViaErrorsIs(t *testing.T) {
	specs.Describe(t, "ConflictError is identifiable with errors.Is", func(s *specs.Spec) {
		s.It("matches ErrConcurrencyConflict", func(ctx *specs.Context) {
			err := persistence.NewConflictError(persistence.Unscoped(), "agg-1", persistence.ExpectRevision(3))

			ctx.Expect(err).To(specs.MatchError(persistence.ErrConcurrencyConflict))
		})
	})
}

func TestConflictErrorIdentifiableViaErrorsAs(t *testing.T) {
	specs.Describe(t, "ConflictError is identifiable with errors.As", func(s *specs.Spec) {
		s.It("exposes its persistence id, expected precondition and actual revision", func(ctx *specs.Context) {
			var err error = persistence.NewConflictError(persistence.Unscoped(), "agg-1", persistence.ExpectRevision(3), persistence.WithActualRevision(5))

			var conflict *persistence.ConflictError
			ctx.Expect(errors.As(err, &conflict)).To(specs.BeTrue())
			ctx.Expect(conflict.PersistenceID()).ToEqual("agg-1")
			ctx.Expect(conflict.Expected()).ToEqual(persistence.ExpectRevision(3))

			actual, ok := conflict.ActualRevision()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(actual).ToEqual(uint64(5))
		})
	})
}

func TestConflictErrorWrappedIsStillIdentifiable(t *testing.T) {
	specs.Describe(t, "a wrapped ConflictError stays identifiable", func(s *specs.Spec) {
		s.It("matches both errors.Is and errors.As through errors.Join", func(ctx *specs.Context) {
			err := persistence.NewConflictError(persistence.Unscoped(), "agg-1", persistence.ExpectGenesis())
			wrapped := errors.Join(errors.New("write failed"), err)

			ctx.Expect(wrapped).To(specs.MatchError(persistence.ErrConcurrencyConflict))

			var conflict *persistence.ConflictError
			ctx.Expect(errors.As(wrapped, &conflict)).To(specs.BeTrue())
			ctx.Expect(conflict.PersistenceID()).ToEqual("agg-1")
		})
	})
}

func TestConflictErrorActualRevisionUnknownWhenNotSupplied(t *testing.T) {
	specs.Describe(t, "ConflictError.ActualRevision", func(s *specs.Spec) {
		s.It("is unknown when no actual revision was supplied", func(ctx *specs.Context) {
			err := persistence.NewConflictError(persistence.Unscoped(), "agg-1", persistence.ExpectRevision(3))

			_, ok := err.ActualRevision()
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestConflictErrorScopeAccessor(t *testing.T) {
	specs.Describe(t, "ConflictError.Scope", func(s *specs.Spec) {
		s.It("returns the scope the conflict was raised in", func(ctx *specs.Context) {
			tenantScope, err := persistence.NewTenantScope("tenant-a")
			ctx.Expect(err).To(specs.BeNil())

			unscopedErr := persistence.NewConflictError(persistence.Unscoped(), "agg-1", persistence.ExpectRevision(3))
			ctx.Expect(unscopedErr.Scope().IsUnscoped()).To(specs.BeTrue())

			tenantErr := persistence.NewConflictError(tenantScope, "agg-1", persistence.ExpectRevision(3))
			ctx.Expect(tenantErr.Scope().Equal(tenantScope)).To(specs.BeTrue())
		})
	})
}

func TestConflictErrorErrorMessageCanonicalGrammar(t *testing.T) {
	specs.Describe(t, "ConflictError.Error renders the canonical grammar", func(s *specs.Spec) {
		tenantScope, err := persistence.NewTenantScope("tenant-a")
		if err != nil {
			t.Fatal(err)
		}

		tests := []struct {
			name     string
			err      *persistence.ConflictError
			expected string
		}{
			{
				name:     "unscoped, unconditional with unknown actual",
				err:      persistence.NewConflictError(persistence.Unscoped(), "agg-1", persistence.Unconditional()),
				expected: "ego: concurrency conflict: grammar=v1, scope=unscoped, persistence_id=\"agg-1\", expected=unconditional, actual=unknown",
			},
			{
				name:     "unscoped, genesis with known actual",
				err:      persistence.NewConflictError(persistence.Unscoped(), "agg-2", persistence.ExpectGenesis(), persistence.WithActualRevision(1)),
				expected: "ego: concurrency conflict: grammar=v1, scope=unscoped, persistence_id=\"agg-2\", expected=genesis, actual=1",
			},
			{
				name:     "unscoped, exact revision with known actual",
				err:      persistence.NewConflictError(persistence.Unscoped(), "agg-3", persistence.ExpectRevision(4), persistence.WithActualRevision(9)),
				expected: "ego: concurrency conflict: grammar=v1, scope=unscoped, persistence_id=\"agg-3\", expected=4, actual=9",
			},
			{
				name:     "tenant scope, exact revision with known actual",
				err:      persistence.NewConflictError(tenantScope, "agg-4", persistence.ExpectRevision(4), persistence.WithActualRevision(9)),
				expected: "ego: concurrency conflict: grammar=v1, scope=tenant:\"tenant-a\", persistence_id=\"agg-4\", expected=4, actual=9",
			},
		}

		for _, tt := range tests {
			s.It(tt.name, func(ctx *specs.Context) {
				ctx.Expect(tt.err.Error()).ToEqual(tt.expected)
			})
		}
	})
}

func TestParseConflictErrorIsExactInverseOfError(t *testing.T) {
	specs.Describe(t, "ParseConflictError is the exact inverse of ConflictError.Error", func(s *specs.Spec) {
		tenantScope, err := persistence.NewTenantScope("tenant-a")
		if err != nil {
			t.Fatal(err)
		}

		tests := []*persistence.ConflictError{
			persistence.NewConflictError(persistence.Unscoped(), "agg-1", persistence.Unconditional()),
			persistence.NewConflictError(persistence.Unscoped(), "agg-2", persistence.ExpectGenesis()),
			persistence.NewConflictError(persistence.Unscoped(), "agg-3", persistence.ExpectGenesis(), persistence.WithActualRevision(0)),
			persistence.NewConflictError(persistence.Unscoped(), "agg-4", persistence.ExpectRevision(4)),
			persistence.NewConflictError(persistence.Unscoped(), "agg-5", persistence.ExpectRevision(4), persistence.WithActualRevision(9)),
			persistence.NewConflictError(tenantScope, "agg-6", persistence.ExpectRevision(4), persistence.WithActualRevision(9)),
		}

		for _, original := range tests {
			s.It(original.Error(), func(ctx *specs.Context) {
				parsed, ok := persistence.ParseConflictError(original.Error())
				ctx.Expect(ok).To(specs.BeTrue())
				ctx.Expect(original.Scope().Equal(parsed.Scope())).To(specs.BeTrue())
				ctx.Expect(parsed.PersistenceID()).ToEqual(original.PersistenceID())
				ctx.Expect(parsed.Expected()).ToEqual(original.Expected())

				wantActual, wantOK := original.ActualRevision()
				gotActual, gotOK := parsed.ActualRevision()
				ctx.Expect(gotOK).ToEqual(wantOK)
				ctx.Expect(gotActual).ToEqual(wantActual)

				ctx.Expect(parsed.Error()).ToEqual(original.Error())
			})
		}
	})
}

func TestParseConflictErrorRejectsMalformedMessages(t *testing.T) {
	specs.Describe(t, "ParseConflictError rejects malformed messages", func(s *specs.Spec) {
		tests := []string{
			"",
			"not a conflict message",
			// Compatibility policy: neither earlier rendering is reconstructed.
			// The pre-TENANT-003 grammar carried no scope, and the unversioned
			// scope= grammar was ambiguous for valid tenant and persistence ids.
			// Both still classify as a concurrency conflict by the unchanged
			// sentinel prefix (reply_classification.go), without a cause.
			"ego: concurrency conflict: persistence_id=agg-1, expected=unconditional, actual=unknown",
			"ego: concurrency conflict: scope=unscoped, persistence_id=agg-1, expected=unconditional, actual=unknown",
			"ego: concurrency conflict: scope=tenant:tenant-a, persistence_id=agg-1, expected=4, actual=9",
			// Unknown or missing grammar version.
			`ego: concurrency conflict: grammar=v2, scope=unscoped, persistence_id="agg-1", expected=unconditional, actual=unknown`,
			`ego: concurrency conflict: grammar=, scope=unscoped, persistence_id="agg-1", expected=unconditional, actual=unknown`,
			// Scope token.
			`ego: concurrency conflict: grammar=v1, scope=unspecified, persistence_id="agg-1", expected=unconditional, actual=unknown`,
			`ego: concurrency conflict: grammar=v1, scope=tenant:tenant-a, persistence_id="agg-1", expected=unconditional, actual=unknown`,
			`ego: concurrency conflict: grammar=v1, scope=tenant:"", persistence_id="agg-1", expected=unconditional, actual=unknown`,
			`ego: concurrency conflict: grammar=v1, scope=tenant:" padded", persistence_id="agg-1", expected=unconditional, actual=unknown`,
			`ego: concurrency conflict: grammar=v1, scope=tenant:"a\tb", persistence_id="agg-1", expected=unconditional, actual=unknown`,
			"ego: concurrency conflict: grammar=v1, scope=tenant:`raw`, persistence_id=\"agg-1\", expected=unconditional, actual=unknown",
			`ego: concurrency conflict: grammar=v1, scope=tenant:"\u0061cme", persistence_id="agg-1", expected=unconditional, actual=unknown`,
			// Persistence id token.
			`ego: concurrency conflict: grammar=v1, scope=unscoped, persistence_id=agg-1, expected=unconditional, actual=unknown`,
			`ego: concurrency conflict: grammar=v1, scope=unscoped, persistence_id="agg-1, expected=unconditional, actual=unknown`,
			"ego: concurrency conflict: grammar=v1, scope=unscoped, persistence_id=`agg-1`, expected=unconditional, actual=unknown",
			`ego: concurrency conflict: grammar=v1, scope=unscoped, persistence_id="\x61gg-1", expected=unconditional, actual=unknown`,
			// Precondition and actual revision tokens.
			`ego: concurrency conflict: grammar=v1, scope=unscoped, persistence_id="agg-1", expected=bogus, actual=unknown`,
			`ego: concurrency conflict: grammar=v1, scope=unscoped, persistence_id="agg-1", expected=04, actual=unknown`,
			`ego: concurrency conflict: grammar=v1, scope=unscoped, persistence_id="agg-1", expected=unconditional, actual=not-a-number`,
			`ego: concurrency conflict: grammar=v1, scope=unscoped, persistence_id="agg-1", expected=unconditional, actual=09`,
			`ego: concurrency conflict: grammar=v1, scope=unscoped, persistence_id="agg-1", expected=unconditional`,
			`ego: concurrency conflict: grammar=v1, scope=unscoped, persistence_id="agg-1", expected=unconditional, actual=unknown, extra=1`,
			`ego: concurrency conflict: grammar=v1, scope=unscoped, persistence_id="agg-1",expected=unconditional, actual=unknown`,
			"ego: concurrency conflict: persistence_id=agg-1",
			"ego: concurrency conflict: scope=unscoped",
			"ego: concurrency conflict: scope=unspecified, persistence_id=agg-1, expected=unconditional, actual=unknown",
			"ego: concurrency conflict: scope=tenant:, persistence_id=agg-1, expected=unconditional, actual=unknown",
			"ego: concurrency conflict: scope=unscoped, persistence_id=agg-1",
			"ego: concurrency conflict: scope=unscoped, persistence_id=agg-1, expected=unconditional",
			"ego: concurrency conflict: scope=unscoped, persistence_id=agg-1, expected=bogus, actual=unknown",
			"ego: concurrency conflict: scope=unscoped, persistence_id=agg-1, expected=unconditional, actual=not-a-number",
		}

		for _, msg := range tests {
			s.It(msg, func(ctx *specs.Context) {
				_, ok := persistence.ParseConflictError(msg)
				ctx.Expect(ok).To(specs.BeFalse())
			})
		}
	})
}

func TestConflictErrorDoesNotMatchUnrelatedSentinel(t *testing.T) {
	specs.Describe(t, "ConflictError is not confused with other persistence errors", func(s *specs.Spec) {
		s.It("does not match ErrInvalidPrecondition or ErrPreconditionScope", func(ctx *specs.Context) {
			err := persistence.NewConflictError(persistence.Unscoped(), "agg-1", persistence.ExpectRevision(3))

			ctx.Expect(err).To(specs.Not(specs.MatchError(persistence.ErrInvalidPrecondition)))
			ctx.Expect(err).To(specs.Not(specs.MatchError(persistence.ErrPreconditionScope)))
		})
	})
}

// adversarialConflictIDs are valid tenant ids and persistence ids that
// contain the grammar's own delimiters, quoting characters, and non-ASCII
// text: every one must survive ParseConflictError(err.Error()) exactly.
var adversarialConflictIDs = []string{
	"a, persistence_id=x",
	"x, expected=4, actual=9",
	`quote " inside`,
	`back\slash`,
	"equals=sign,comma",
	"scope=tenant:\"forged\"",
	"grammar=v1, scope=unscoped",
	"テナント-東京",
	"emoji 🚀 id",
	"tenant:unscoped",
	"unscoped",
}

func TestParseConflictErrorRoundTripsAdversarialIdentifiers(t *testing.T) {
	persistenceIDs := append([]string{"", "line\nbreak", "nul\x00byte", "bad utf8 \xff\xfe", "\t leading tab"}, adversarialConflictIDs...)

	scopes := []persistence.Scope{persistence.Unscoped()}
	for _, id := range adversarialConflictIDs {
		scope, err := persistence.NewTenantScope(tenancy.TenantID(id))
		if err != nil {
			t.Fatalf("fixture tenant id must be valid: %q: %v", id, err)
		}
		scopes = append(scopes, scope)
	}

	// One case per scope and persistence id, named by the quoted error text, so a failure says which
	// combination did not round-trip.
	specs.Describe(t, "ParseConflictError round-trips adversarial identifiers", func(s *specs.Spec) {
		for _, scope := range scopes {
			for _, persistenceID := range persistenceIDs {
				original := persistence.NewConflictError(scope, persistenceID, persistence.ExpectRevision(7), persistence.WithActualRevision(8))
				s.It(fmt.Sprintf("%q", original.Error()), func(ctx *specs.Context) {
					parsed, ok := persistence.ParseConflictError(original.Error())
					ctx.Expect(ok).To(specs.BeTrue())
					ctx.Expect(original.Scope().Equal(parsed.Scope())).To(specs.BeTrue())
					ctx.Expect(parsed.PersistenceID()).ToEqual(persistenceID)
					ctx.Expect(parsed.Error()).ToEqual(original.Error())
					ctx.Expect(parsed).To(specs.MatchError(persistence.ErrConcurrencyConflict))
				})
			}
		}
	})
}

// FuzzParseConflictErrorRoundTrip checks the exact-inverse contract over
// arbitrary identifiers; its seed corpus runs as part of the ordinary test
// suite.
func FuzzParseConflictErrorRoundTrip(f *testing.F) {
	for _, id := range adversarialConflictIDs {
		f.Add(id, id, uint64(3), true)
	}
	f.Add("acme", "", uint64(0), false)

	f.Fuzz(func(t *testing.T, tenantID, persistenceID string, revision uint64, tenantScoped bool) {
		scope := persistence.Unscoped()
		if tenantScoped {
			tenantScope, err := persistence.NewTenantScope(tenancy.TenantID(tenantID))
			if err != nil {
				t.Skip("not a valid tenant id")
			}
			scope = tenantScope
		}
		original := persistence.NewConflictError(scope, persistenceID, persistence.ExpectRevision(revision), persistence.WithActualRevision(revision+1))
		parsed, ok := persistence.ParseConflictError(original.Error())
		if !ok {
			t.Fatalf("did not parse: %q", original.Error())
		}
		if !original.Scope().Equal(parsed.Scope()) || parsed.PersistenceID() != persistenceID || parsed.Error() != original.Error() {
			t.Fatalf("round trip mismatch: %q -> %q", original.Error(), parsed.Error())
		}
	})
}
