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
	"math"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/command"
)

// carrierHas reports whether the carrier holds key.
func carrierHas(c command.Carrier, key string) bool {
	_, ok := c[key]
	return ok
}

func TestMarshalMetadataUsesCanonicalKeys(t *testing.T) {
	specs.Describe(t, "MarshalMetadata writes root metadata under the canonical ego.cmd keys", func(s *specs.Spec) {
		s.It("writes the required keys and omits the optional ones", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			carrier := command.MarshalMetadata(md)
			ctx.Expect(carrier["ego.cmd.operation_id"]).ToEqual(string(op))
			ctx.Expect(carrier["ego.cmd.correlation_id"]).ToEqual(string(command.CorrelationID(op)))
			ctx.Expect(carrier["ego.cmd.timestamp"] == "").To(specs.BeFalse())
			ctx.Expect(carrierHas(carrier, "ego.cmd.causation_id")).To(specs.BeFalse())
			ctx.Expect(carrierHas(carrier, "ego.cmd.deadline")).To(specs.BeFalse())
			ctx.Expect(carrierHas(carrier, "ego.cmd.principal_id")).To(specs.BeFalse())
			ctx.Expect(carrierHas(carrier, "ego.cmd.principal_kind")).To(specs.BeFalse())
		})
	})
}

func TestCarrierRoundTripPreservesIdentity(t *testing.T) {
	specs.Describe(t, "a carrier round trip preserves operation, correlation and causation identity", func(s *specs.Spec) {
		s.It("restores the identity of a derived child", func(ctx *specs.Context) {
			root := mustOperationID(ctx, "op-root")
			rootMD, err := command.NewMetadata(root)
			ctx.Expect(err).To(specs.BeNil())

			child := mustOperationID(ctx, "op-child")
			childMD, err := rootMD.Derive(child)
			ctx.Expect(err).To(specs.BeNil())

			carrier := command.MarshalMetadata(childMD)
			got, err := command.UnmarshalMetadata(carrier)
			ctx.Expect(err).To(specs.BeNil())

			ctx.Expect(got.OperationID()).ToEqual(childMD.OperationID())
			ctx.Expect(got.CorrelationID()).ToEqual(childMD.CorrelationID())

			wantCausation, ok := childMD.CausationID()
			ctx.Expect(ok).To(specs.BeTrue())
			gotCausation, ok := got.CausationID()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(gotCausation).ToEqual(wantCausation)
		})
	})
}

func TestCarrierRoundTripOptionalFieldsAbsent(t *testing.T) {
	specs.Describe(t, "a carrier round trip keeps absent optional fields absent", func(s *specs.Spec) {
		s.It("reports causation, tenant, principal, deadline and custom values as absent", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			carrier := command.MarshalMetadata(md)
			got, err := command.UnmarshalMetadata(carrier)
			ctx.Expect(err).To(specs.BeNil())

			_, ok := got.CausationID()
			ctx.Expect(ok).To(specs.BeFalse())
			_, ok = got.Tenant()
			ctx.Expect(ok).To(specs.BeFalse())
			_, ok = got.Principal()
			ctx.Expect(ok).To(specs.BeFalse())
			_, ok = got.Deadline()
			ctx.Expect(ok).To(specs.BeFalse())
			ctx.Expect(len(got.Custom())).ToEqual(0)
		})
	})
}

func TestCarrierRoundTripOptionalFieldsPresent(t *testing.T) {
	specs.Describe(t, "a carrier round trip preserves present optional fields", func(s *specs.Spec) {
		s.It("restores tenant, principal, deadline and custom value", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			tc := mustTenantContext(ctx, "tenant-1")
			principal, err := command.NewPrincipal("user-1", command.WithPrincipalKind("service-account"))
			ctx.Expect(err).To(specs.BeNil())
			deadline := time.Now().UTC().Add(time.Hour).Truncate(time.Nanosecond)

			md, err := command.NewMetadata(op,
				command.WithTenant(tc),
				command.WithPrincipal(principal),
				command.WithDeadline(deadline),
				command.WithCustom("region", "us-east-1"),
			)
			ctx.Expect(err).To(specs.BeNil())

			carrier := command.MarshalMetadata(md)
			got, err := command.UnmarshalMetadata(carrier)
			ctx.Expect(err).To(specs.BeNil())

			gotTenant, ok := got.Tenant()
			ctx.Expect(ok).To(specs.BeTrue())
			gotTenantID, _ := gotTenant.Tenant()
			wantTenantID, _ := tc.Tenant()
			ctx.Expect(gotTenantID).ToEqual(wantTenantID)

			gotPrincipal, ok := got.Principal()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(gotPrincipal).ToEqual(principal)

			gotDeadline, ok := got.Deadline()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(deadline.Equal(gotDeadline)).To(specs.BeTrue())

			gotCustom, ok := got.CustomValue("region")
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(gotCustom).ToEqual("us-east-1")
		})
	})
}

func TestUnmarshalMetadataRejectsMissingOperationID(t *testing.T) {
	specs.Describe(t, "UnmarshalMetadata rejects a carrier without an operation id", func(s *specs.Spec) {
		s.It("fails with ErrInvalidMetadata", func(ctx *specs.Context) {
			carrier := command.Carrier{
				"ego.cmd.correlation_id": "flow-1",
				"ego.cmd.timestamp":      time.Now().UTC().Format(time.RFC3339Nano),
			}

			_, err := command.UnmarshalMetadata(carrier)
			ctx.Expect(err).To(specs.MatchError(command.ErrInvalidMetadata))
		})
	})
}

func TestUnmarshalMetadataRejectsReservedBareKey(t *testing.T) {
	specs.Describe(t, "UnmarshalMetadata rejects a bare canonical key used as custom metadata", func(s *specs.Spec) {
		s.It("fails with ErrReservedKey", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			carrier := command.MarshalMetadata(md)
			carrier["operation_id"] = "shadow-attempt"

			_, err = command.UnmarshalMetadata(carrier)
			ctx.Expect(err).To(specs.MatchError(command.ErrReservedKey))
		})
	})
}

func TestUnmarshalMetadataRejectsInvalidCustomValue(t *testing.T) {
	specs.Describe(t, "UnmarshalMetadata rejects an invalid custom value", func(s *specs.Spec) {
		s.It("fails with ErrInvalidMetadata for a value with a NUL byte", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			carrier := command.MarshalMetadata(md)
			carrier["region"] = "us-east-1\x00"

			_, err = command.UnmarshalMetadata(carrier)
			ctx.Expect(err).To(specs.MatchError(command.ErrInvalidMetadata))
		})
	})
}

func TestUnmarshalMetadataIgnoresUnknownEgoCmdKey(t *testing.T) {
	specs.Describe(t, "UnmarshalMetadata tolerates unknown ego.cmd keys from a newer writer", func(s *specs.Spec) {
		s.It("drops the unknown key without adding custom metadata", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			carrier := command.MarshalMetadata(md)
			carrier["ego.cmd.future_field"] = "from-a-newer-writer"

			got, err := command.UnmarshalMetadata(carrier)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got.OperationID()).ToEqual(md.OperationID())
			ctx.Expect(len(got.Custom())).ToEqual(0)
		})
	})
}

// TestUnmarshalMetadataRejectsUnrecognizedEgoNamespace proves a carrier
// key under D6's reserved "ego." prefix but outside the two namespaces
// design.md's Carrier type comment grants forward compatibility to
// (ego.cmd.* ∪ ego.tenant.* — D9) is rejected with ErrReservedKey, not
// silently dropped. A hypothetical stray WRITE-005 ego.idem.* value
// stands in for that case here; reconstructing from a Carrier must not
// be more permissive than constructing directly via WithCustom, which
// already rejects any "ego."-prefixed custom key.
func TestUnmarshalMetadataRejectsUnrecognizedEgoNamespace(t *testing.T) {
	specs.Describe(t, "UnmarshalMetadata rejects keys in an unrecognized ego namespace", func(s *specs.Spec) {
		s.It("fails with ErrReservedKey for an ego.idem key", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			carrier := command.MarshalMetadata(md)
			carrier["ego.idem.key"] = "future-namespace-value"

			_, err = command.UnmarshalMetadata(carrier)
			ctx.Expect(err).To(specs.MatchError(command.ErrReservedKey))
		})
	})
}

func TestCarrierRoundTripExpectedRevisionPresent(t *testing.T) {
	specs.Describe(t, "a carrier round trip preserves a present expected revision", func(s *specs.Spec) {
		s.It("writes the revision as a decimal string and restores it", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			md, err := command.NewMetadata(op, command.WithExpectedRevision(7))
			ctx.Expect(err).To(specs.BeNil())

			carrier := command.MarshalMetadata(md)
			ctx.Expect(carrier["ego.cmd.expected_revision"]).ToEqual("7")

			got, err := command.UnmarshalMetadata(carrier)
			ctx.Expect(err).To(specs.BeNil())

			revision, ok := got.ExpectedRevision()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(revision).ToEqual(uint64(7))
		})
	})
}

func TestCarrierRoundTripExpectedRevisionAbsentStaysAbsent(t *testing.T) {
	specs.Describe(t, "a carrier round trip keeps an absent expected revision absent", func(s *specs.Spec) {
		s.It("writes no key and restores no revision", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			carrier := command.MarshalMetadata(md)
			ctx.Expect(carrierHas(carrier, "ego.cmd.expected_revision")).To(specs.BeFalse())

			got, err := command.UnmarshalMetadata(carrier)
			ctx.Expect(err).To(specs.BeNil())

			_, ok := got.ExpectedRevision()
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestUnmarshalMetadataRejectsMalformedExpectedRevision(t *testing.T) {
	specs.Describe(t, "UnmarshalMetadata rejects a malformed expected revision", func(s *specs.Spec) {
		s.It("fails with ErrInvalidMetadata for a non-numeric value", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			carrier := command.MarshalMetadata(md)
			carrier["ego.cmd.expected_revision"] = "not-a-number"

			_, err = command.UnmarshalMetadata(carrier)
			ctx.Expect(err).To(specs.MatchError(command.ErrInvalidMetadata))
		})
	})
}

func TestUnmarshalMetadataRejectsNegativeExpectedRevision(t *testing.T) {
	specs.Describe(t, "UnmarshalMetadata rejects a negative expected revision", func(s *specs.Spec) {
		s.It("fails with ErrInvalidMetadata for -1", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			carrier := command.MarshalMetadata(md)
			carrier["ego.cmd.expected_revision"] = "-1"

			_, err = command.UnmarshalMetadata(carrier)
			ctx.Expect(err).To(specs.MatchError(command.ErrInvalidMetadata))
		})
	})
}

func TestUnmarshalMetadataRejectsExpectedRevisionOverflow(t *testing.T) {
	specs.Describe(t, "UnmarshalMetadata rejects an expected revision beyond uint64", func(s *specs.Spec) {
		s.It("fails with ErrInvalidMetadata for math.MaxUint64 + 1", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			carrier := command.MarshalMetadata(md)
			// math.MaxUint64 + 1, one past the largest value strconv.ParseUint(_, 10, 64) accepts.
			carrier["ego.cmd.expected_revision"] = "18446744073709551616"

			_, err = command.UnmarshalMetadata(carrier)
			ctx.Expect(err).To(specs.MatchError(command.ErrInvalidMetadata))
		})
	})
}

func TestCarrierRoundTripExpectedRevisionMaxUint64(t *testing.T) {
	specs.Describe(t, "a carrier round trip preserves the largest expected revision", func(s *specs.Spec) {
		s.It("writes and restores math.MaxUint64", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			md, err := command.NewMetadata(op, command.WithExpectedRevision(math.MaxUint64))
			ctx.Expect(err).To(specs.BeNil())

			carrier := command.MarshalMetadata(md)
			ctx.Expect(carrier["ego.cmd.expected_revision"]).ToEqual("18446744073709551615")

			got, err := command.UnmarshalMetadata(carrier)
			ctx.Expect(err).To(specs.BeNil())

			revision, ok := got.ExpectedRevision()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(revision).ToEqual(uint64(math.MaxUint64))
		})
	})
}

func TestCarrierDelegatesTenantSerializationToTenancyPackage(t *testing.T) {
	specs.Describe(t, "MarshalMetadata delegates tenant serialization to the tenancy package", func(s *specs.Spec) {
		s.It("writes the tenancy scope and id keys", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			tc := mustTenantContext(ctx, "tenant-1")
			md, err := command.NewMetadata(op, command.WithTenant(tc))
			ctx.Expect(err).To(specs.BeNil())

			carrier := command.MarshalMetadata(md)
			ctx.Expect(carrier["ego.tenant.scope"]).ToEqual("tenant")
			ctx.Expect(carrier["ego.tenant.id"]).ToEqual("tenant-1")
		})
	})
}
