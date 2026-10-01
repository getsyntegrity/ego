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

package protocol

import (
	"testing"

	"github.com/getsyntegrity/go-specs/assert"
	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/persistence"
)

type revisionCase struct {
	name        string
	revision    uint64
	hasRevision bool
	want        persistence.WritePrecondition
}

func TestPreconditionFromRevisionMapsPerD4(t *testing.T) {
	specs.Describe(t, "PreconditionFromRevision maps an expected revision to a write precondition", func(s *specs.Spec) {
		specs.Table(s, []revisionCase{
			{name: "absent is unconditional", revision: 0, hasRevision: false, want: persistence.Unconditional()},
			{name: "absent ignores a stray revision", revision: 7, hasRevision: false, want: persistence.Unconditional()},
			{name: "zero is genesis, not absence", revision: 0, hasRevision: true, want: persistence.ExpectGenesis()},
			{name: "positive revision is an exact expectation", revision: 42, hasRevision: true, want: persistence.ExpectRevision(42)},
			{name: "max revision is an exact expectation", revision: ^uint64(0), hasRevision: true, want: persistence.ExpectRevision(^uint64(0))},
		}, func(c revisionCase) string { return c.name }, func(ctx *specs.Context, c revisionCase) {
			ctx.Expect(PreconditionFromRevision(c.revision, c.hasRevision)).ToEqual(c.want)
		})
	})
}

// FuzzPreconditionFromRevision checks the D4 mapping for every input, not only the table rows:
// an absent revision is always unconditional, zero is always genesis, and any other value is an
// exact expectation on that same value.
func FuzzPreconditionFromRevision(f *testing.F) {
	f.Add(uint64(0), false)
	f.Add(uint64(0), true)
	f.Add(uint64(42), true)
	f.Add(^uint64(0), true)
	f.Fuzz(func(t *testing.T, revision uint64, hasRevision bool) {
		want := persistence.ExpectRevision(revision)
		switch {
		case !hasRevision:
			want = persistence.Unconditional()
		case revision == 0:
			want = persistence.ExpectGenesis()
		}
		got := PreconditionFromRevision(revision, hasRevision)
		if m := assert.Equal(want); !m.Match(got) {
			t.Fatal(m.FailureMessage(got))
		}
	})
}
