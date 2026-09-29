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

	"github.com/stretchr/testify/assert"

	"github.com/getsyntegrity/ego/persistence"
)

func TestPreconditionFromRevisionMapsPerD4(t *testing.T) {
	tests := []struct {
		name        string
		revision    uint64
		hasRevision bool
		want        persistence.WritePrecondition
	}{
		{name: "absent is unconditional", revision: 0, hasRevision: false, want: persistence.Unconditional()},
		{name: "zero is genesis, not absence", revision: 0, hasRevision: true, want: persistence.ExpectGenesis()},
		{name: "positive revision is an exact expectation", revision: 42, hasRevision: true, want: persistence.ExpectRevision(42)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := PreconditionFromRevision(tc.revision, tc.hasRevision)
			assert.Equal(t, tc.want, got)
		})
	}
}
