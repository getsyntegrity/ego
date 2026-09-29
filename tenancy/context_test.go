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
	"context"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/tenancy"
)

// --- 2.3: Attach / From / Require / VerifyUnchanged ------------------------

func TestAttach_BindsTenantContextRetrievableViaFrom(t *testing.T) {
	specs.Describe(t, "Attach binds a TenantContext that From returns", func(s *specs.Spec) {
		s.It("returns the attached context", func(ctx *specs.Context) {
			tc, err := tenancy.NewTenantContext(mustTenantID(ctx.T, "acme-corp"))
			ctx.Expect(err).To(specs.BeNil())

			bound, err := tenancy.Attach(context.Background(), tc)
			ctx.Expect(err).To(specs.BeNil())

			got, ok := tenancy.From(bound)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(got).ToEqual(tc)
		})
	})
}

func TestAttach_IsIdempotentForTheSameTenantContext(t *testing.T) {
	specs.Describe(t, "Attach is idempotent for an equal TenantContext", func(s *specs.Spec) {
		s.It("accepts re-attaching the same context and keeps it bound", func(ctx *specs.Context) {
			tc, err := tenancy.NewTenantContext(mustTenantID(ctx.T, "acme-corp"))
			ctx.Expect(err).To(specs.BeNil())

			bound, err := tenancy.Attach(context.Background(), tc)
			ctx.Expect(err).To(specs.BeNil())

			// Re-attaching the same (equal) TenantContext must succeed, not be
			// treated as a change.
			bound2, err := tenancy.Attach(bound, tc)
			ctx.Expect(err).To(specs.BeNil())

			got, ok := tenancy.From(bound2)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(got).ToEqual(tc)
		})
	})
}

func TestAttach_RejectsChangingAlreadyBoundTenantContext(t *testing.T) {
	specs.Describe(t, "Attach rejects changing an already-bound TenantContext", func(s *specs.Spec) {
		s.It("fails with ErrDenied and keeps the original binding", func(ctx *specs.Context) {
			tcA, err := tenancy.NewTenantContext(mustTenantID(ctx.T, "acme-corp"))
			ctx.Expect(err).To(specs.BeNil())
			tcB, err := tenancy.NewTenantContext(mustTenantID(ctx.T, "globex-corp"))
			ctx.Expect(err).To(specs.BeNil())

			bound, err := tenancy.Attach(context.Background(), tcA)
			ctx.Expect(err).To(specs.BeNil())

			_, err = tenancy.Attach(bound, tcB)
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrDenied))

			// The original binding must survive the rejected attempt unchanged.
			got, ok := tenancy.From(bound)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(got).ToEqual(tcA)
		})
	})
}

func TestFrom_ReturnsFalseWhenNothingAttached(t *testing.T) {
	specs.Describe(t, "From on a context with nothing attached", func(s *specs.Spec) {
		s.It("reports no tenant context", func(ctx *specs.Context) {
			_, ok := tenancy.From(context.Background())
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

// --- Blocker 1 fix (EGO-TENANT-006 review): Attach/Require must reject an
// invalid (zero-value) TenantContext, not just an absent one. A caller's
// own tenancy.TenantResolver implementation lives outside this package and
// can only ever construct tenancy.TenantContext{} via a bare struct
// literal (every field is unexported), so `TenantContext{}, nil` — no
// error — is the one malformed value external code can produce. Before
// design.md Decision D8, Attach bound it unchecked and Require returned it
// unchecked; these tests are the RED/GREEN pair for that fix. ---

func TestAttach_RejectsZeroValueTenantContext(t *testing.T) {
	specs.Describe(t, "Attach rejects a zero-value TenantContext", func(s *specs.Spec) {
		s.It("fails with ErrInvalid and binds nothing", func(ctx *specs.Context) {
			var zero tenancy.TenantContext

			bound, err := tenancy.Attach(context.Background(), zero)
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))

			// ctx must be left unchanged: no TenantContext attached at all, not
			// even the invalid one. Attach must not bind an invalid TenantContext
			// even when rejecting it.
			_, ok := tenancy.From(bound)
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestAttach_ValidTenantScopedContextStillFlowsThroughUnchanged(t *testing.T) {
	specs.Describe(t, "a valid tenant-scoped context flows through Attach and Require unchanged", func(s *specs.Spec) {
		s.It("Require returns what Attach bound", func(ctx *specs.Context) {
			tc, err := tenancy.NewTenantContext(mustTenantID(ctx.T, "acme-corp"))
			ctx.Expect(err).To(specs.BeNil())

			bound, err := tenancy.Attach(context.Background(), tc)
			ctx.Expect(err).To(specs.BeNil())

			got, err := tenancy.Require(bound)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).ToEqual(tc)
		})
	})
}

func TestAttach_ValidAdministrativeContextStillFlowsThroughUnchanged(t *testing.T) {
	specs.Describe(t, "a valid administrative context flows through Attach and Require unchanged", func(s *specs.Spec) {
		s.It("Require returns what Attach bound", func(ctx *specs.Context) {
			admin, err := tenancy.NewAdministrative("ops-tool", "crypto-shredding")
			ctx.Expect(err).To(specs.BeNil())
			tc, err := tenancy.NewAdministrativeContext(admin)
			ctx.Expect(err).To(specs.BeNil())

			bound, err := tenancy.Attach(context.Background(), tc)
			ctx.Expect(err).To(specs.BeNil())

			got, err := tenancy.Require(bound)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).ToEqual(tc)
		})
	})
}

func TestRequire_ReturnsErrMissingWhenNothingAttached(t *testing.T) {
	specs.Describe(t, "Require on a context with nothing attached", func(s *specs.Spec) {
		s.It("fails with ErrMissing", func(ctx *specs.Context) {
			_, err := tenancy.Require(context.Background())
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrMissing))
		})
	})
}

func TestRequire_ReturnsBoundTenantContext(t *testing.T) {
	specs.Describe(t, "Require returns the bound TenantContext", func(s *specs.Spec) {
		s.It("returns the context that was attached", func(ctx *specs.Context) {
			tc, err := tenancy.NewTenantContext(mustTenantID(ctx.T, "acme-corp"))
			ctx.Expect(err).To(specs.BeNil())

			bound, err := tenancy.Attach(context.Background(), tc)
			ctx.Expect(err).To(specs.BeNil())

			got, err := tenancy.Require(bound)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).ToEqual(tc)
		})
	})
}

func TestVerifyUnchanged_ReturnsNilWhenEqual(t *testing.T) {
	specs.Describe(t, "VerifyUnchanged accepts equal contexts", func(s *specs.Spec) {
		s.It("returns nil for the same context twice", func(ctx *specs.Context) {
			tc, err := tenancy.NewTenantContext(mustTenantID(ctx.T, "acme-corp"))
			ctx.Expect(err).To(specs.BeNil())

			ctx.Expect(tenancy.VerifyUnchanged(tc, tc)).To(specs.BeNil())
		})
	})
}

func TestVerifyUnchanged_ReturnsErrDeniedWhenDifferent(t *testing.T) {
	specs.Describe(t, "VerifyUnchanged rejects different contexts", func(s *specs.Spec) {
		s.It("fails with ErrDenied", func(ctx *specs.Context) {
			tcA, err := tenancy.NewTenantContext(mustTenantID(ctx.T, "acme-corp"))
			ctx.Expect(err).To(specs.BeNil())
			tcB, err := tenancy.NewTenantContext(mustTenantID(ctx.T, "globex-corp"))
			ctx.Expect(err).To(specs.BeNil())

			err = tenancy.VerifyUnchanged(tcA, tcB)
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrDenied))
		})
	})
}

// --- 2.4: saga boundary — reconstruct via metadata after context.Background() reset ---
//
// saga_actor.go resets to context.Background() at several points in its
// pipeline (design.md Technical Approach; exploration.md §10). This is a
// faithful, in-process simulation of that reset: it produces a
// TenantContext at an entrypoint, discards it exactly the way
// saga_actor.go's context.Background() calls do, and proves the ONLY way
// identity survives is explicit reconstruction from carried metadata,
// never implicit context propagation.

// simulateSagaCommand models what an entrypoint hands to a saga: metadata
// carried alongside the command/event, produced once at the trust boundary.
type simulateSagaCommand struct {
	metadata tenancy.Metadata
}

// simulateSagaStep models one step of saga_actor.go's pipeline: it resets
// to context.Background() (as saga_actor.go genuinely does before
// HandleEvent/persistAndApplyEvents/etc.), then — if reconstruct is true —
// explicitly reconstructs the TenantContext from the command's carried
// metadata and attaches it, exactly as design.md's Resolve-Once,
// Propagate-After Discipline requires for a saga boundary. It returns the
// resulting context (background reset, optionally reconstructed).
func simulateSagaStep(t testing.TB, cmd simulateSagaCommand, reconstruct bool) context.Context {
	t.Helper()

	// This is the real saga_actor.go behavior being simulated: every
	// pipeline step starts from a fresh context.Background(), not from
	// whatever context the entrypoint used.
	sagaCtx := context.Background()

	if !reconstruct {
		return sagaCtx
	}

	tc, err := tenancy.UnmarshalMetadata(cmd.metadata)
	if err != nil {
		t.Fatalf("UnmarshalMetadata: %v", err)
	}

	sagaCtx, err = tenancy.Attach(sagaCtx, tc)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	return sagaCtx
}

func TestSagaBoundary_ReconstructsTenantIdentityFromCarriedMetadata(t *testing.T) {
	specs.Describe(t, "a saga boundary reconstructs tenant identity from carried metadata", func(s *specs.Spec) {
		s.It("survives the context.Background() reset only through metadata reconstruction", func(ctx *specs.Context) {
			// Entrypoint: resolve once, attach (this is the trust boundary).
			resolver, err := tenancy.WithSingleTenant(mustTenantID(ctx.T, "acme-corp"))
			ctx.Expect(err).To(specs.BeNil())

			entrypointCtx := context.Background()
			entrypointTC, err := resolver.Resolve(entrypointCtx)
			ctx.Expect(err).To(specs.BeNil())
			entrypointCtx, err = tenancy.Attach(entrypointCtx, entrypointTC)
			ctx.Expect(err).To(specs.BeNil())

			// The entrypoint hands the saga a command carrying tenant-aware
			// metadata alongside it — never relying on context.Context surviving
			// the saga's own context.Background() resets.
			bound, err := tenancy.Require(entrypointCtx)
			ctx.Expect(err).To(specs.BeNil())
			cmd := simulateSagaCommand{metadata: tenancy.MarshalMetadata(bound)}

			// Sanity: the simulated reset genuinely drops any implicit identity —
			// this proves the harness models the reset faithfully, not just in
			// name. A context.Background() reset must not carry any tenant
			// identity implicitly.
			resetOnly := simulateSagaStep(ctx.T, cmd, false)
			_, ok := tenancy.From(resetOnly)
			ctx.Expect(ok).To(specs.BeFalse())

			// The saga step reconstructs identity explicitly from carried metadata.
			sagaCtx := simulateSagaStep(ctx.T, cmd, true)

			sagaTC, err := tenancy.Require(sagaCtx)
			ctx.Expect(err).To(specs.BeNil())

			originalID, ok := entrypointTC.Tenant()
			ctx.Expect(ok).To(specs.BeTrue())
			sagaID, ok := sagaTC.Tenant()
			ctx.Expect(ok).To(specs.BeTrue())
			// tenant identity must survive the saga's context.Background() reset
			// via metadata reconstruction
			ctx.Expect(sagaID).ToEqual(originalID)
		})
	})
}

func TestSagaBoundary_SkippingMetadataReconstructionFailsClosed(t *testing.T) {
	specs.Describe(t, "a saga step that skips metadata reconstruction fails closed", func(s *specs.Spec) {
		s.It("Require fails with ErrMissing after a bare context.Background() reset", func(ctx *specs.Context) {
			resolver, err := tenancy.WithSingleTenant(mustTenantID(ctx.T, "acme-corp"))
			ctx.Expect(err).To(specs.BeNil())
			entrypointCtx, err := tenancy.Attach(context.Background(), mustResolve(ctx.T, resolver, context.Background()))
			ctx.Expect(err).To(specs.BeNil())
			bound, err := tenancy.Require(entrypointCtx)
			ctx.Expect(err).To(specs.BeNil())
			cmd := simulateSagaCommand{metadata: tenancy.MarshalMetadata(bound)}

			// A saga step that resets to context.Background() and never
			// reconstructs from the carried metadata (a real, reachable bug) must
			// fail closed on the read side, not silently proceed unattributed.
			sagaCtx := simulateSagaStep(ctx.T, cmd, false)

			_, err = tenancy.Require(sagaCtx)
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrMissing))
		})
	})
}

func mustResolve(t testing.TB, r tenancy.TenantResolver, ctx context.Context) tenancy.TenantContext {
	t.Helper()
	tc, err := r.Resolve(ctx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return tc
}

// --- 2.5: invocation — entrypoint resolves+attaches, behavior only requires ---
//
// This is the second spec-mandated acceptance scenario (design.md Decision
// R2 / Testing Strategy "Acceptance (b) invocation"): it demonstrates,
// concretely, who invokes the resolver and who only reads the result.

// simulateBehavior models a Behavior/Saga implementation: it never knows
// about a TenantResolver, JWT, HTTP, or any other identity mechanism — it
// only reads the TenantContext already attached to the context it is
// handed (design.md Decision R2: "domain code reads").
func simulateBehavior(ctx context.Context) (tenancy.TenantContext, error) {
	return tenancy.Require(ctx)
}

// simulateEntrypoint models the transport/runtime entrypoint at a trust
// boundary: it is the ONLY code that invokes the configured TenantResolver
// and attaches the result, before handing the context to domain code.
func simulateEntrypoint(ctx context.Context, resolver tenancy.TenantResolver) (context.Context, error) {
	tc, err := resolver.Resolve(ctx)
	if err != nil {
		return ctx, err
	}
	return tenancy.Attach(ctx, tc)
}

func TestInvocation_EntrypointResolvesAndAttaches_BehaviorOnlyRequires(t *testing.T) {
	specs.Describe(t, "the entrypoint resolves and attaches; behavior only requires", func(s *specs.Spec) {
		s.It("behavior reads the tenant the entrypoint attached", func(ctx *specs.Context) {
			resolver, err := tenancy.WithSingleTenant(mustTenantID(ctx.T, "acme-corp"))
			ctx.Expect(err).To(specs.BeNil())

			// Entrypoint: invokes the resolver and attaches — this is the ONLY
			// place Resolve is called.
			attached, err := simulateEntrypoint(context.Background(), resolver)
			ctx.Expect(err).To(specs.BeNil())

			// Domain code: only ever reads via Require, never resolves anything
			// itself.
			tc, err := simulateBehavior(attached)
			ctx.Expect(err).To(specs.BeNil())

			gotID, ok := tc.Tenant()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(gotID).ToEqual(tenancy.TenantID("acme-corp"))
		})
	})
}

func TestInvocation_SkippingEntrypointAttachMeansBehaviorFails(t *testing.T) {
	specs.Describe(t, "behavior fails when the entrypoint never attached", func(s *specs.Spec) {
		s.It("fails closed with ErrMissing", func(ctx *specs.Context) {
			// Negative path: if the entrypoint never invokes the resolver/Attach
			// (e.g. a transport adapter forgets to wire it — R2's manual
			// responsibility until TENANT-006 automates it), domain code must fail
			// closed, not silently execute as some default tenant.
			_, err := simulateBehavior(context.Background())
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrMissing))
		})
	})
}
