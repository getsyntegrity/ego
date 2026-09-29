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

package runtime_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/port/runtime"
)

func TestErrUnsupportedWrapsStandardError(t *testing.T) {
	specs.Describe(t, "ErrUnsupported wraps the standard library's errors.ErrUnsupported", func(s *specs.Spec) {
		s.It("matches errors.ErrUnsupported", func(ctx *specs.Context) {
			ctx.Expect(runtime.ErrUnsupported).To(specs.MatchError(errors.ErrUnsupported))
		})
	})
}

func TestUnsupportedError(t *testing.T) {
	specs.Describe(t, "UnsupportedError names the runtime and the operation and matches the unsupported sentinels", func(s *specs.Spec) {
		s.It("matches both sentinels, words its message and survives wrapping", func(ctx *specs.Context) {
			var err error = &runtime.UnsupportedError{Runtime: "inmem", Operation: "StartProjection"}
			ctx.Expect(err).To(specs.MatchError(runtime.ErrUnsupported))
			ctx.Expect(err).To(specs.MatchError(errors.ErrUnsupported))
			ctx.Expect(err.Error()).ToEqual(`eGo: runtime "inmem" does not support StartProjection`)

			wrapped := fmt.Errorf("spawn: %w", err)
			var target *runtime.UnsupportedError
			ctx.Expect(wrapped).To(specs.MatchErrorAs(&target))
			ctx.Expect(target.Runtime).ToEqual("inmem")
			ctx.Expect(target.Operation).ToEqual("StartProjection")
			ctx.Expect(wrapped).To(specs.MatchError(runtime.ErrUnsupported))
		})
	})
}

func TestSentinelMessagesAreKept(t *testing.T) {
	specs.Describe(t, "The runtime sentinel errors keep their messages", func(s *specs.Spec) {
		cases := []struct {
			name string
			err  error
			msg  string
		}{
			{"ErrEngineNotStarted", runtime.ErrEngineNotStarted, "eGo engine has not started"},
			{"ErrUndefinedEntityID", runtime.ErrUndefinedEntityID, "eGo entity id is not defined"},
			{"ErrDurableStateStoreRequired", runtime.ErrDurableStateStoreRequired, "durable state store is required"},
			{"ErrEventsStoreRequired", runtime.ErrEventsStoreRequired, "events store is required"},
			{"ErrProjectionNotRegistered", runtime.ErrProjectionNotRegistered, "projection is not registered; register it with ego.WithProjection"},
			{"ErrSpawnTenantUndetermined", runtime.ErrSpawnTenantUndetermined, "eGo: tenant-aware spawn requires ego.WithTenant (the registered resolver exposes no fixed tenant); see tenancy.FixedTenantResolver"},
			{"ErrSpawnTenantMismatch", runtime.ErrSpawnTenantMismatch, "eGo: entity id is already bound to a different tenant"},
			{"ErrSpawnTenantUnverified", runtime.ErrSpawnTenantUnverified, "eGo: the spawned actor's tenant binding could not be verified"},
			{"ErrNotACommand", runtime.ErrNotACommand, "eGo: payload is an engine-internal control message, not a command"},
			{"ErrEntityFamilyNotDeclared", runtime.ErrEntityFamilyNotDeclared, "eGo: entity family is not declared; declare it with ego.WithEntityFamilies"},
		}

		s.It("covers ten distinct sentinels", func(ctx *specs.Context) {
			ctx.Expect(len(cases)).ToEqual(10)
			var duplicates []string
			for i, tc := range cases {
				for _, other := range cases[:i] {
					if errors.Is(tc.err, other.err) {
						duplicates = append(duplicates, tc.name+" is the same error as "+other.name)
					}
				}
			}
			ctx.Expect(duplicates).To(specs.BeNil())
		})

		for _, tc := range cases {
			s.It(tc.name, func(ctx *specs.Context) {
				ctx.Expect(tc.err.Error()).ToEqual(tc.msg)
			})
		}
	})
}
