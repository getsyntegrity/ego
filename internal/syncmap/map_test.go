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

package syncmap

import (
	"slices"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

func TestNewAndSet(t *testing.T) {
	specs.Describe(t, "Set stores entries in a new map", func(s *specs.Spec) {
		s.It("counts every distinct key set", func(ctx *specs.Context) {
			sm := New[int, string]()
			sm.Set(1, "one")
			sm.Set(2, "two")
			ctx.Expect(sm.Len()).ToEqual(2)
		})
	})
}

func TestGet(t *testing.T) {
	specs.Describe(t, "Get looks up a value by key", func(s *specs.Spec) {
		s.It("returns the value for a present key and reports a missing key", func(ctx *specs.Context) {
			sm := New[int, string]()
			sm.Set(1, "one")

			val, ok := sm.Get(1)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(val).ToEqual("one")

			_, ok = sm.Get(2)
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestDelete(t *testing.T) {
	specs.Describe(t, "Delete removes an entry by key", func(s *specs.Spec) {
		s.It("removes the entry and tolerates a missing key", func(ctx *specs.Context) {
			sm := New[int, string]()
			sm.Set(1, "one")
			sm.Delete(1)
			_, ok := sm.Get(1)
			ctx.Expect(ok).To(specs.BeFalse())
			sm.Delete(2) // just make sure this doesn't panic
		})
	})
}

func TestLen(t *testing.T) {
	specs.Describe(t, "Len counts the entries in the map", func(s *specs.Spec) {
		s.It("excludes deleted entries", func(ctx *specs.Context) {
			sm := New[int, string]()
			sm.Set(1, "one")
			sm.Set(2, "two")
			sm.Set(3, "three")
			sm.Delete(2)
			ctx.Expect(sm.Len()).ToEqual(2)
		})
	})
}

func TestForEach(t *testing.T) {
	specs.Describe(t, "Range visits every entry", func(s *specs.Spec) {
		s.It("visits each key exactly once", func(ctx *specs.Context) {
			sm := New[int, string]()
			sm.Set(1, "one")
			sm.Set(2, "two")

			keys := make([]int, 0)
			sm.Range(func(k int, v string) { // nolint
				keys = append(keys, k)
			})

			ctx.Expect(len(keys)).ToEqual(2)
			// Check if keys 1 and 2 are present
			ctx.Expect(slices.Contains(keys, 1) && slices.Contains(keys, 2)).To(specs.BeTrue())
		})
	})
}

func TestValues(t *testing.T) {
	specs.Describe(t, "Values returns every stored value", func(s *specs.Spec) {
		s.It("returns all values in any order", func(ctx *specs.Context) {
			sm := New[int, string]()
			sm.Set(1, "one")
			sm.Set(2, "two")
			sm.Set(3, "three")

			values := sm.Values()
			ctx.Expect(len(values)).ToEqual(3)
			slices.Sort(values)
			ctx.Expect(values).ToEqual([]string{"one", "three", "two"})
		})
	})
}

func TestReset(t *testing.T) {
	specs.Describe(t, "Reset empties the map", func(s *specs.Spec) {
		s.It("drops every entry", func(ctx *specs.Context) {
			sm := New[int, string]()
			sm.Set(1, "one")
			sm.Set(2, "two")
			sm.Set(3, "three")
			ctx.Expect(sm.Len()).ToEqual(3)

			sm.Reset()
			ctx.Expect(sm.Len()).ToEqual(0)

			_, ok := sm.Get(1)
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}
