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

package adaptertest

import (
	"fmt"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/offsetstore"
	"github.com/getsyntegrity/ego/persistence"
	"github.com/getsyntegrity/ego/port/adapter"
)

// The table of ports that imply CapReady is written as string literals,
// because this package may import only the standard library and
// port/adapter. This test, which may import more, pins it to the contract
// packages' own constants so a renamed port cannot drift silently.
func TestImpliesReadyMatchesTheStorePortConstants(t *testing.T) {
	type portCase struct {
		name string
		port adapter.Port
		want bool
	}
	implies := func(p adapter.Port) portCase {
		return portCase{fmt.Sprintf("%s implies CapReady, since every store port has Ping", p), p, true}
	}
	doesNotImply := func(p adapter.Port) portCase {
		return portCase{fmt.Sprintf("%s does not imply CapReady", p), p, false}
	}
	specs.Describe(t, "the ports that imply CapReady match the contract packages' store port constants", func(s *specs.Spec) {
		specs.Table(s, []portCase{
			implies(persistence.PortEventsStore),
			implies(persistence.PortStateStore),
			implies(persistence.PortSnapshotStore),
			implies(offsetstore.PortOffsetStore),
			doesNotImply("publishing.EventPublisher"),
			doesNotImply("publishing.StatePublisher"),
			doesNotImply("tenancy.TenantResolver"),
			doesNotImply("encryption.Encryptor"),
		}, func(c portCase) string { return c.name }, func(ctx *specs.Context, c portCase) {
			ctx.Expect(impliesReady(c.port)).To(specs.Equal(c.want))
		})
		s.It("lists exactly the 4 store ports", func(ctx *specs.Context) {
			ctx.Expect(readyPorts).To(specs.HaveLen(4))
		})
	})
}
