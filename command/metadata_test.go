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
	"strings"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/command"
	"github.com/getsyntegrity/ego/tenancy"
)

// mustOperationID, mustTenantContext and mustMetadata build fixtures through the spec: a failed
// precondition is reported by the matcher, like any other expectation, and stops the case.
func mustOperationID(ctx *specs.Context, s string) command.OperationID {
	op, err := command.NewOperationID(s)
	ctx.Expect(err).To(specs.BeNil())
	return op
}

func mustTenantContext(ctx *specs.Context, id string) tenancy.TenantContext {
	tenantID, err := tenancy.NewTenantID(id)
	ctx.Expect(err).To(specs.BeNil())
	tc, err := tenancy.NewTenantContext(tenantID)
	ctx.Expect(err).To(specs.BeNil())
	return tc
}

// beTime matches a time.Time that is the same instant as want. Unlike ToEqual it ignores the
// location and the monotonic reading, and its failure prints both instants.
func beTime(want time.Time) specs.Matcher {
	return specs.Satisfy("the instant "+want.Format(time.RFC3339Nano), func(a any) bool {
		got, ok := a.(time.Time)
		return ok && got.Equal(want)
	})
}

// beTimeBetween matches a time.Time within [lo, hi], both inclusive.
func beTimeBetween(lo, hi time.Time) specs.Matcher {
	return specs.Satisfy("an instant between "+lo.Format(time.RFC3339Nano)+" and "+hi.Format(time.RFC3339Nano), func(a any) bool {
		got, ok := a.(time.Time)
		return ok && !got.Before(lo) && !got.After(hi)
	})
}

// beTimeBefore matches a time.Time strictly before limit.
func beTimeBefore(limit time.Time) specs.Matcher {
	return specs.Satisfy("an instant before "+limit.Format(time.RFC3339Nano), func(a any) bool {
		got, ok := a.(time.Time)
		return ok && got.Before(limit)
	})
}

func TestNewMetadataRoot(t *testing.T) {
	specs.Describe(t, "NewMetadata builds root metadata", func(s *specs.Spec) {
		s.It("uses the operation id as correlation id and has no causation", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")

			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(md.OperationID()).ToEqual(op)
			ctx.Expect(md.CorrelationID()).ToEqual(command.CorrelationID(op))

			_, ok := md.CausationID()
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestNewMetadataWithCorrelationID(t *testing.T) {
	specs.Describe(t, "NewMetadata honors WithCorrelationID", func(s *specs.Spec) {
		s.It("uses the supplied correlation id", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			corr := command.CorrelationID("flow-42")

			md, err := command.NewMetadata(op, command.WithCorrelationID(corr))
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(md.CorrelationID()).ToEqual(corr)
		})
	})
}

func TestNewMetadataWithTenant(t *testing.T) {
	specs.Describe(t, "NewMetadata honors WithTenant", func(s *specs.Spec) {
		s.It("exposes the supplied tenant context", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			tc := mustTenantContext(ctx, "acme")

			md, err := command.NewMetadata(op, command.WithTenant(tc))
			ctx.Expect(err).To(specs.BeNil())

			got, ok := md.Tenant()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(got).ToEqual(tc)
		})
	})
}

func TestNewMetadataWithoutTenant(t *testing.T) {
	specs.Describe(t, "NewMetadata without WithTenant", func(s *specs.Spec) {
		s.It("reports no tenant", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")

			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			_, ok := md.Tenant()
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestNewMetadataWithPrincipal(t *testing.T) {
	specs.Describe(t, "NewMetadata honors WithPrincipal", func(s *specs.Spec) {
		s.It("exposes the supplied principal", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			p, err := command.NewPrincipal("user-1", command.WithPrincipalKind("user"))
			ctx.Expect(err).To(specs.BeNil())

			md, err := command.NewMetadata(op, command.WithPrincipal(p))
			ctx.Expect(err).To(specs.BeNil())

			got, ok := md.Principal()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(got).ToEqual(p)
		})
	})
}

func TestNewMetadataWithTimestampDefault(t *testing.T) {
	specs.Describe(t, "NewMetadata timestamps metadata with the current time by default", func(s *specs.Spec) {
		s.It("falls between the time before and after construction", func(ctx *specs.Context) {
			before := time.Now().UTC()
			op := mustOperationID(ctx, "op-1")

			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())
			after := time.Now().UTC()

			ctx.Expect(md.Timestamp()).To(beTimeBetween(before, after))
		})
	})
}

func TestNewMetadataWithTimestampOverride(t *testing.T) {
	specs.Describe(t, "NewMetadata honors WithTimestamp", func(s *specs.Spec) {
		s.It("uses the supplied timestamp", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			ts := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

			md, err := command.NewMetadata(op, command.WithTimestamp(ts))
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(md.Timestamp()).To(beTime(ts))
		})
	})
}

func TestNewMetadataWithDeadline(t *testing.T) {
	specs.Describe(t, "NewMetadata honors WithDeadline", func(s *specs.Spec) {
		s.It("exposes the supplied deadline", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			deadline := time.Now().Add(time.Hour)

			md, err := command.NewMetadata(op, command.WithDeadline(deadline))
			ctx.Expect(err).To(specs.BeNil())

			got, ok := md.Deadline()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(got).To(beTime(deadline))
		})
	})
}

func TestNewMetadataWithoutDeadline(t *testing.T) {
	specs.Describe(t, "NewMetadata without WithDeadline", func(s *specs.Spec) {
		s.It("reports no deadline", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")

			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			_, ok := md.Deadline()
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

// TestMetadataElapsedDeadlineIsRecognized proves AC7's semantics for an
// already-elapsed deadline: Deadline and Timestamp are both plain
// time.Time, directly comparable via the stdlib, so a deadline earlier
// than the timestamp is recognized as already expired without any
// dedicated enforcement mechanism (enforcement itself is out of scope).
func TestMetadataElapsedDeadlineIsRecognized(t *testing.T) {
	specs.Describe(t, "an already-elapsed deadline is recognizable by plain time comparison", func(s *specs.Spec) {
		s.It("reports a deadline before the timestamp", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			ts := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			deadline := ts.Add(-time.Hour)

			md, err := command.NewMetadata(op, command.WithTimestamp(ts), command.WithDeadline(deadline))
			ctx.Expect(err).To(specs.BeNil())

			got, ok := md.Deadline()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(got).To(beTimeBefore(md.Timestamp()))
		})
	})
}

func TestNewMetadataCustomDefensiveCopy(t *testing.T) {
	specs.Describe(t, "Metadata.Custom returns a defensive copy", func(s *specs.Spec) {
		s.It("keeps the stored values when the returned map is mutated", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")

			md, err := command.NewMetadata(op, command.WithCustom("region", "us-east"))
			ctx.Expect(err).To(specs.BeNil())

			custom := md.Custom()
			custom["region"] = "tampered"
			custom["extra"] = "tampered"

			v, ok := md.CustomValue("region")
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(v).ToEqual("us-east")
			_, ok = md.CustomValue("extra")
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestNewMetadataCustomValue(t *testing.T) {
	specs.Describe(t, "Metadata.CustomValue looks up custom metadata by key", func(s *specs.Spec) {
		s.It("returns a stored value and reports a missing key as absent", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")

			md, err := command.NewMetadata(op, command.WithCustom("region", "us-east"))
			ctx.Expect(err).To(specs.BeNil())

			v, ok := md.CustomValue("region")
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(v).ToEqual("us-east")

			_, ok = md.CustomValue("missing")
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestNewMetadataWithCustomRejectsReservedPrefix(t *testing.T) {
	specs.Describe(t, "WithCustom rejects the reserved ego. prefix", func(s *specs.Spec) {
		s.It("fails with ErrReservedKey for an ego.tenant key", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")

			_, err := command.NewMetadata(op, command.WithCustom("ego.tenant.id", "x"))
			ctx.Expect(err).To(specs.MatchError(command.ErrReservedKey))
		})
	})
}

func TestNewMetadataWithCustomRejectsCanonicalKey(t *testing.T) {
	specs.Describe(t, "WithCustom rejects every canonical bare key", func(s *specs.Spec) {
		canonical := []string{
			"operation_id", "correlation_id", "causation_id",
			"timestamp", "deadline", "principal_id", "principal_kind", "tenant",
			"expected_revision",
		}
		for _, key := range canonical {
			s.It(key, func(ctx *specs.Context) {
				op := mustOperationID(ctx, "op-1")

				_, err := command.NewMetadata(op, command.WithCustom(key, "x"))
				ctx.Expect(err).To(specs.MatchError(command.ErrReservedKey))
			})
		}
	})
}

func TestNewMetadataWithCustomRejectsInvalidValue(t *testing.T) {
	specs.Describe(t, "WithCustom rejects an invalid value", func(s *specs.Spec) {
		s.It("fails for a value longer than the limit", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")

			_, err := command.NewMetadata(op, command.WithCustom("region", strings.Repeat("a", 1025)))
			ctx.Expect(err).To(specs.MatchError(command.ErrInvalidMetadata))
		})
	})
}

func TestNewMetadataWithCustomAcceptsValidKeyValue(t *testing.T) {
	specs.Describe(t, "WithCustom accepts a valid key and value", func(s *specs.Spec) {
		s.It("stores the pair", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")

			md, err := command.NewMetadata(op, command.WithCustom("region", "us-east"))
			ctx.Expect(err).To(specs.BeNil())

			v, ok := md.CustomValue("region")
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(v).ToEqual("us-east")
		})
	})
}

func TestMetadataDeriveInheritsCorrelationAndChainsCausation(t *testing.T) {
	specs.Describe(t, "Metadata.Derive inherits correlation and chains causation", func(s *specs.Spec) {
		s.It("keeps the correlation id and sets the parent operation as causation", func(ctx *specs.Context) {
			parentOp := mustOperationID(ctx, "op-1")
			parent, err := command.NewMetadata(parentOp)
			ctx.Expect(err).To(specs.BeNil())

			childOp := mustOperationID(ctx, "op-2")
			child, err := parent.Derive(childOp)
			ctx.Expect(err).To(specs.BeNil())

			ctx.Expect(child.CorrelationID()).ToEqual(parent.CorrelationID())
			ctx.Expect(child.OperationID()).ToEqual(childOp)

			causation, ok := child.CausationID()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(causation).ToEqual(command.CausationID(parentOp))
		})
	})
}

func TestMetadataDeriveRejectsSameOperationID(t *testing.T) {
	specs.Describe(t, "Metadata.Derive rejects reusing the parent operation id", func(s *specs.Spec) {
		s.It("fails with ErrSameOperationID", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			parent, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			_, err = parent.Derive(op)
			ctx.Expect(err).To(specs.MatchError(command.ErrSameOperationID))
		})
	})
}

func TestMetadataDeriveTenantMustNotChange(t *testing.T) {
	specs.Describe(t, "Metadata.Derive does not allow changing the tenant", func(s *specs.Spec) {
		s.It("fails with tenancy.ErrDenied for a different tenant", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			tc := mustTenantContext(ctx, "acme")
			parent, err := command.NewMetadata(op, command.WithTenant(tc))
			ctx.Expect(err).To(specs.BeNil())

			childOp := mustOperationID(ctx, "op-2")
			otherTenant := mustTenantContext(ctx, "other")

			_, err = parent.Derive(childOp, command.WithTenant(otherTenant))
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrDenied))
		})
	})
}

func TestMetadataDeriveTenantInheritedWhenUnspecified(t *testing.T) {
	specs.Describe(t, "Metadata.Derive inherits the tenant when none is specified", func(s *specs.Spec) {
		s.It("gives the child the parent tenant", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			tc := mustTenantContext(ctx, "acme")
			parent, err := command.NewMetadata(op, command.WithTenant(tc))
			ctx.Expect(err).To(specs.BeNil())

			childOp := mustOperationID(ctx, "op-2")
			child, err := parent.Derive(childOp)
			ctx.Expect(err).To(specs.BeNil())

			got, ok := child.Tenant()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(got).ToEqual(tc)
		})
	})
}

func TestMetadataDeriveDeadlineMayOnlyShorten(t *testing.T) {
	specs.Describe(t, "Metadata.Derive lets a deadline only shorten", func(s *specs.Spec) {
		s.It("accepts a shorter deadline and rejects a longer one", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			deadline := time.Now().Add(time.Hour)
			parent, err := command.NewMetadata(op, command.WithDeadline(deadline))
			ctx.Expect(err).To(specs.BeNil())

			childOp := mustOperationID(ctx, "op-2")

			shorter := deadline.Add(-time.Minute)
			child, err := parent.Derive(childOp, command.WithDeadline(shorter))
			ctx.Expect(err).To(specs.BeNil())
			got, ok := child.Deadline()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(got).To(beTime(shorter))

			longer := deadline.Add(time.Minute)
			_, err = parent.Derive(childOp, command.WithDeadline(longer))
			ctx.Expect(err).To(specs.MatchError(command.ErrDeadlineExtension))
		})
	})
}

func TestMetadataDeriveCustomNotInherited(t *testing.T) {
	specs.Describe(t, "Metadata.Derive does not inherit custom metadata", func(s *specs.Spec) {
		s.It("gives the child no custom values", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			parent, err := command.NewMetadata(op, command.WithCustom("region", "us-east"))
			ctx.Expect(err).To(specs.BeNil())

			childOp := mustOperationID(ctx, "op-2")
			child, err := parent.Derive(childOp)
			ctx.Expect(err).To(specs.BeNil())

			_, ok := child.CustomValue("region")
			ctx.Expect(ok).To(specs.BeFalse())
			ctx.Expect(child.Custom()).To(specs.BeEmpty())
		})
	})
}

func TestNewMetadataWithoutExpectedRevision(t *testing.T) {
	specs.Describe(t, "NewMetadata without WithExpectedRevision", func(s *specs.Spec) {
		s.It("reports no expected revision", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")

			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			_, ok := md.ExpectedRevision()
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestNewMetadataWithExpectedRevisionZeroIsGenesisNotAbsence(t *testing.T) {
	specs.Describe(t, "WithExpectedRevision(0) means genesis, not absence", func(s *specs.Spec) {
		s.It("reports a present expected revision of zero", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")

			md, err := command.NewMetadata(op, command.WithExpectedRevision(0))
			ctx.Expect(err).To(specs.BeNil())

			got, ok := md.ExpectedRevision()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(got).ToEqual(uint64(0))
		})
	})
}

func TestNewMetadataWithExpectedRevisionPositive(t *testing.T) {
	specs.Describe(t, "NewMetadata honors a positive WithExpectedRevision", func(s *specs.Spec) {
		s.It("exposes the supplied expected revision", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")

			md, err := command.NewMetadata(op, command.WithExpectedRevision(42))
			ctx.Expect(err).To(specs.BeNil())

			got, ok := md.ExpectedRevision()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(got).ToEqual(uint64(42))
		})
	})
}

func TestMetadataDeriveDoesNotInheritExpectedRevision(t *testing.T) {
	specs.Describe(t, "Metadata.Derive does not inherit the expected revision", func(s *specs.Spec) {
		s.It("gives the child no expected revision", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			parent, err := command.NewMetadata(op, command.WithExpectedRevision(5))
			ctx.Expect(err).To(specs.BeNil())

			childOp := mustOperationID(ctx, "op-2")
			child, err := parent.Derive(childOp)
			ctx.Expect(err).To(specs.BeNil())

			_, ok := child.ExpectedRevision()
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestMetadataDerivePrincipalInheritedUnlessOverridden(t *testing.T) {
	specs.Describe(t, "Metadata.Derive inherits the principal unless overridden", func(s *specs.Spec) {
		s.It("keeps the parent principal by default and takes an override when given", func(ctx *specs.Context) {
			op := mustOperationID(ctx, "op-1")
			p, err := command.NewPrincipal("user-1")
			ctx.Expect(err).To(specs.BeNil())
			parent, err := command.NewMetadata(op, command.WithPrincipal(p))
			ctx.Expect(err).To(specs.BeNil())

			childOp := mustOperationID(ctx, "op-2")
			child, err := parent.Derive(childOp)
			ctx.Expect(err).To(specs.BeNil())

			got, ok := child.Principal()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(got).ToEqual(p)

			other, err := command.NewPrincipal("user-2")
			ctx.Expect(err).To(specs.BeNil())
			child2, err := parent.Derive(childOp, command.WithPrincipal(other))
			ctx.Expect(err).To(specs.BeNil())

			got2, ok := child2.Principal()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(got2).ToEqual(other)
		})
	})
}
