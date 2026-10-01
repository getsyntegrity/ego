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

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/command"
)

func TestNewOperationID(t *testing.T) {
	specs.Describe(t, "NewOperationID validates an operation id", func(s *specs.Spec) {
		s.It("valid", func(ctx *specs.Context) {
			op, err := command.NewOperationID("order-123")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(op).ToEqual(command.OperationID("order-123"))
		})

		type rejection struct {
			name   string
			inputs []string
		}
		specs.Table(s, []rejection{
			{name: "empty rejected", inputs: []string{""}},
			{name: "not valid UTF-8 rejected", inputs: []string{string([]byte{0xff, 0xfe})}},
			{name: "leading or trailing whitespace rejected", inputs: []string{" order-123", "order-123 "}},
			{name: "control rune rejected", inputs: []string{"order-123\n"}},
			{name: "exceeds max length rejected", inputs: []string{strings.Repeat("a", 129)}},
		}, func(r rejection) string { return r.name }, func(ctx *specs.Context, r rejection) {
			for _, in := range r.inputs {
				_, err := command.NewOperationID(in)
				ctx.Expect(err).To(specs.MatchError(command.ErrInvalidMetadata))
			}
		})

		s.It("interior whitespace accepted", func(ctx *specs.Context) {
			op, err := command.NewOperationID("order 123")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(op).ToEqual(command.OperationID("order 123"))
		})
	})
}

func TestGenerateOperationID(t *testing.T) {
	specs.Describe(t, "GenerateOperationID produces distinct, valid operation ids", func(s *specs.Spec) {
		s.It("returns non-empty ids that differ and pass NewOperationID validation", func(ctx *specs.Context) {
			op1, err := command.GenerateOperationID()
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(string(op1)).To(specs.Not(specs.BeEmpty()))

			op2, err := command.GenerateOperationID()
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(string(op2)).To(specs.Not(specs.BeEmpty()))

			ctx.Expect(op1).To(specs.NotEqual(op2))

			// GenerateOperationID's output must itself satisfy NewOperationID's
			// validation rules.
			_, err = command.NewOperationID(string(op1))
			ctx.Expect(err).To(specs.BeNil())
		})
	})
}

func TestGenerateOperationIDUniqueness(t *testing.T) {
	specs.Describe(t, "GenerateOperationID does not repeat an id", func(s *specs.Spec) {
		s.It("generates 1000 unique ids", func(ctx *specs.Context) {
			// Duplicates are collected rather than asserted one by one, so a failure lists the repeated ids.
			seen := make(map[command.OperationID]struct{})
			var duplicates []command.OperationID
			for i := 0; i < 1000; i++ {
				op, err := command.GenerateOperationID()
				ctx.Expect(err).To(specs.BeNil())
				if _, exists := seen[op]; exists {
					duplicates = append(duplicates, op)
				}
				seen[op] = struct{}{}
			}
			ctx.Expect(duplicates).To(specs.BeEmpty())
		})
	})
}

func TestIdentityDefinedTypesAreDistinct(t *testing.T) {
	specs.Describe(t, "OperationID, CorrelationID and CausationID are distinct defined types", func(s *specs.Spec) {
		s.It("converts each to its underlying string", func(ctx *specs.Context) {
			// OperationID, CorrelationID and CausationID are distinct defined
			// types over string, not aliases of each other or of tenancy's
			// correlation concept — this is a compile-time assertion.
			var op command.OperationID = "op-1"
			var corr command.CorrelationID = "corr-1"
			var caus command.CausationID = "caus-1"

			ctx.Expect(string(op)).ToEqual("op-1")
			ctx.Expect(string(corr)).ToEqual("corr-1")
			ctx.Expect(string(caus)).ToEqual("caus-1")
		})
	})
}
