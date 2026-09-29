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
	"context"

	"github.com/getsyntegrity/ego/persistence"
)

// ExpectedRevisionFromContext extracts the ExpectedRevision metadata field
// (design.md D5) from goCtx, if any. It reuses MetadataFromContext — the
// same lookup dispatchToBehavior performs — so both the direct and batched
// paths agree on how a command's declared precondition intention is
// recovered. A command reached without envelope metadata (e.g. one that
// bypasses Engine.Dispatch/SendCommand) is treated identically to one that
// carries metadata but declares no ExpectedRevision: both resolve to "no
// declared revision" and, via PreconditionFromRevision, to Unconditional().
func ExpectedRevisionFromContext(goCtx context.Context) (uint64, bool) {
	md, ok := MetadataFromContext(goCtx)
	if !ok {
		return 0, false
	}
	return md.ExpectedRevision()
}

// PreconditionFromRevision resolves an ExpectedRevision metadata value to
// the persistence.WritePrecondition it names (design.md D4): no declared
// revision maps to Unconditional() (legacy compatibility, D8), 0 maps to
// ExpectGenesis(), and any N > 0 maps to ExpectRevision(N).
func PreconditionFromRevision(revision uint64, hasRevision bool) persistence.WritePrecondition {
	if !hasRevision {
		return persistence.Unconditional()
	}
	if revision == 0 {
		return persistence.ExpectGenesis()
	}
	return persistence.ExpectRevision(revision)
}
