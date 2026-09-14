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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablogore/ego/v4/tenancy"
)

// --- 2.3: Attach / From / Require / VerifyUnchanged ------------------------

func TestAttach_BindsTenantContextRetrievableViaFrom(t *testing.T) {
	tc, err := tenancy.NewTenantContext(mustTenantID(t, "acme-corp"))
	require.NoError(t, err)

	ctx, err := tenancy.Attach(context.Background(), tc)
	require.NoError(t, err)

	got, ok := tenancy.From(ctx)
	require.True(t, ok)
	assert.Equal(t, tc, got)
}

func TestAttach_IsIdempotentForTheSameTenantContext(t *testing.T) {
	tc, err := tenancy.NewTenantContext(mustTenantID(t, "acme-corp"))
	require.NoError(t, err)

	ctx, err := tenancy.Attach(context.Background(), tc)
	require.NoError(t, err)

	// Re-attaching the same (equal) TenantContext must succeed, not be
	// treated as a change.
	ctx2, err := tenancy.Attach(ctx, tc)
	require.NoError(t, err)

	got, ok := tenancy.From(ctx2)
	require.True(t, ok)
	assert.Equal(t, tc, got)
}

func TestAttach_RejectsChangingAlreadyBoundTenantContext(t *testing.T) {
	tcA, err := tenancy.NewTenantContext(mustTenantID(t, "acme-corp"))
	require.NoError(t, err)
	tcB, err := tenancy.NewTenantContext(mustTenantID(t, "globex-corp"))
	require.NoError(t, err)

	ctx, err := tenancy.Attach(context.Background(), tcA)
	require.NoError(t, err)

	_, err = tenancy.Attach(ctx, tcB)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrDenied))

	// The original binding must survive the rejected attempt unchanged.
	got, ok := tenancy.From(ctx)
	require.True(t, ok)
	assert.Equal(t, tcA, got)
}

func TestFrom_ReturnsFalseWhenNothingAttached(t *testing.T) {
	_, ok := tenancy.From(context.Background())
	assert.False(t, ok)
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
	var zero tenancy.TenantContext

	ctx, err := tenancy.Attach(context.Background(), zero)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrInvalid))

	// ctx must be left unchanged: no TenantContext attached at all, not
	// even the invalid one.
	_, ok := tenancy.From(ctx)
	assert.False(t, ok, "Attach must not bind an invalid TenantContext even when rejecting it")
}

func TestAttach_ValidTenantScopedContextStillFlowsThroughUnchanged(t *testing.T) {
	tc, err := tenancy.NewTenantContext(mustTenantID(t, "acme-corp"))
	require.NoError(t, err)

	ctx, err := tenancy.Attach(context.Background(), tc)
	require.NoError(t, err)

	got, err := tenancy.Require(ctx)
	require.NoError(t, err)
	assert.Equal(t, tc, got)
}

func TestAttach_ValidAdministrativeContextStillFlowsThroughUnchanged(t *testing.T) {
	admin, err := tenancy.NewAdministrative("ops-tool", "crypto-shredding")
	require.NoError(t, err)
	tc, err := tenancy.NewAdministrativeContext(admin)
	require.NoError(t, err)

	ctx, err := tenancy.Attach(context.Background(), tc)
	require.NoError(t, err)

	got, err := tenancy.Require(ctx)
	require.NoError(t, err)
	assert.Equal(t, tc, got)
}

func TestRequire_ReturnsErrMissingWhenNothingAttached(t *testing.T) {
	_, err := tenancy.Require(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrMissing))
}

func TestRequire_ReturnsBoundTenantContext(t *testing.T) {
	tc, err := tenancy.NewTenantContext(mustTenantID(t, "acme-corp"))
	require.NoError(t, err)

	ctx, err := tenancy.Attach(context.Background(), tc)
	require.NoError(t, err)

	got, err := tenancy.Require(ctx)
	require.NoError(t, err)
	assert.Equal(t, tc, got)
}

func TestVerifyUnchanged_ReturnsNilWhenEqual(t *testing.T) {
	tc, err := tenancy.NewTenantContext(mustTenantID(t, "acme-corp"))
	require.NoError(t, err)

	assert.NoError(t, tenancy.VerifyUnchanged(tc, tc))
}

func TestVerifyUnchanged_ReturnsErrDeniedWhenDifferent(t *testing.T) {
	tcA, err := tenancy.NewTenantContext(mustTenantID(t, "acme-corp"))
	require.NoError(t, err)
	tcB, err := tenancy.NewTenantContext(mustTenantID(t, "globex-corp"))
	require.NoError(t, err)

	err = tenancy.VerifyUnchanged(tcA, tcB)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrDenied))
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
func simulateSagaStep(t *testing.T, cmd simulateSagaCommand, reconstruct bool) context.Context {
	t.Helper()

	// This is the real saga_actor.go behavior being simulated: every
	// pipeline step starts from a fresh context.Background(), not from
	// whatever context the entrypoint used.
	sagaCtx := context.Background()

	if !reconstruct {
		return sagaCtx
	}

	tc, err := tenancy.UnmarshalMetadata(cmd.metadata)
	require.NoError(t, err)

	sagaCtx, err = tenancy.Attach(sagaCtx, tc)
	require.NoError(t, err)
	return sagaCtx
}

func TestSagaBoundary_ReconstructsTenantIdentityFromCarriedMetadata(t *testing.T) {
	// Entrypoint: resolve once, attach (this is the trust boundary).
	resolver, err := tenancy.WithSingleTenant(mustTenantID(t, "acme-corp"))
	require.NoError(t, err)

	entrypointCtx := context.Background()
	entrypointTC, err := resolver.Resolve(entrypointCtx)
	require.NoError(t, err)
	entrypointCtx, err = tenancy.Attach(entrypointCtx, entrypointTC)
	require.NoError(t, err)

	// The entrypoint hands the saga a command carrying tenant-aware
	// metadata alongside it — never relying on context.Context surviving
	// the saga's own context.Background() resets.
	bound, err := tenancy.Require(entrypointCtx)
	require.NoError(t, err)
	cmd := simulateSagaCommand{metadata: tenancy.MarshalMetadata(bound)}

	// Sanity: the simulated reset genuinely drops any implicit identity —
	// this proves the harness models the reset faithfully, not just in name.
	resetOnly := simulateSagaStep(t, cmd, false)
	_, ok := tenancy.From(resetOnly)
	assert.False(t, ok, "a context.Background() reset must not carry any tenant identity implicitly")

	// The saga step reconstructs identity explicitly from carried metadata.
	sagaCtx := simulateSagaStep(t, cmd, true)

	sagaTC, err := tenancy.Require(sagaCtx)
	require.NoError(t, err)

	originalID, ok := entrypointTC.Tenant()
	require.True(t, ok)
	sagaID, ok := sagaTC.Tenant()
	require.True(t, ok)
	assert.Equal(t, originalID, sagaID, "tenant identity must survive the saga's context.Background() reset via metadata reconstruction")
}

func TestSagaBoundary_SkippingMetadataReconstructionFailsClosed(t *testing.T) {
	resolver, err := tenancy.WithSingleTenant(mustTenantID(t, "acme-corp"))
	require.NoError(t, err)
	entrypointCtx, err := tenancy.Attach(context.Background(), mustResolve(t, resolver, context.Background()))
	require.NoError(t, err)
	bound, err := tenancy.Require(entrypointCtx)
	require.NoError(t, err)
	cmd := simulateSagaCommand{metadata: tenancy.MarshalMetadata(bound)}

	// A saga step that resets to context.Background() and never
	// reconstructs from the carried metadata (a real, reachable bug) must
	// fail closed on the read side, not silently proceed unattributed.
	sagaCtx := simulateSagaStep(t, cmd, false)

	_, err = tenancy.Require(sagaCtx)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrMissing))
}

func mustResolve(t *testing.T, r tenancy.TenantResolver, ctx context.Context) tenancy.TenantContext {
	t.Helper()
	tc, err := r.Resolve(ctx)
	require.NoError(t, err)
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
	resolver, err := tenancy.WithSingleTenant(mustTenantID(t, "acme-corp"))
	require.NoError(t, err)

	// Entrypoint: invokes the resolver and attaches — this is the ONLY
	// place Resolve is called.
	ctx, err := simulateEntrypoint(context.Background(), resolver)
	require.NoError(t, err)

	// Domain code: only ever reads via Require, never resolves anything
	// itself.
	tc, err := simulateBehavior(ctx)
	require.NoError(t, err)

	gotID, ok := tc.Tenant()
	require.True(t, ok)
	assert.Equal(t, tenancy.TenantID("acme-corp"), gotID)
}

func TestInvocation_SkippingEntrypointAttachMeansBehaviorFails(t *testing.T) {
	// Negative path: if the entrypoint never invokes the resolver/Attach
	// (e.g. a transport adapter forgets to wire it — R2's manual
	// responsibility until TENANT-006 automates it), domain code must fail
	// closed, not silently execute as some default tenant.
	_, err := simulateBehavior(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrMissing))
}
