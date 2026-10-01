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

package websocket

import (
	"context"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

// Spec scenario "double close" (ego-arch-004 spec 2, lifecycle rule L2):
// a publisher connected to a real server returns nil from both calls to
// Close.
func TestCloseIsIdempotent(t *testing.T) {
	type closeCase struct {
		name string
		open func(url string) (interface{ Close(context.Context) error }, error)
	}
	specs.Describe(t, "Close on a publisher connected to a real server", func(s *specs.Spec) {
		specs.Table(s, []closeCase{
			{"events", func(url string) (interface{ Close(context.Context) error }, error) {
				return NewEventsPublisher(&Config{URL: url})
			}},
			{"state", func(url string) (interface{ Close(context.Context) error }, error) {
				return NewDurableStatePublisher(&Config{URL: url})
			}},
		}, func(c closeCase) string { return c.name + " publisher returns nil from both calls" }, func(ctx *specs.Context, c closeCase) {
			bg := context.Background()
			srv := newTestServer(ctx.T)
			pub, err := c.open(srv.URL())
			ctx.Expect(err).To(specs.BeNil())

			ctx.Expect(pub.Close(bg)).To(specs.BeNil())
			ctx.Expect(pub.Close(bg)).To(specs.BeNil())
		})
	})
}
