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

package egopb_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/getsyntegrity/urd/egopb"
)

// The go_package option lives inside the length-prefixed serialized file
// descriptor, so a text rename of ego.pb.go compiles but panics at init.
// This test proves the regenerated descriptor is sound: it loads, carries
// the module's current package path, and every message resolves through the
// global registry and survives a marshal/unmarshal round trip.
func TestDescriptor_IsSoundAndCarriesTheModulePath(t *testing.T) {
	specs.Describe(t, "the generated file descriptor loads, carries the module path and round-trips every message", func(s *specs.Spec) {
		fd := egopb.File_ego_ego_proto
		s.It("is loaded", func(ctx *specs.Context) {
			ctx.Expect(fd).To(specs.Not(specs.BeNil()))
		})
		if fd == nil {
			return
		}
		s.It("carries the module go_package", func(ctx *specs.Context) {
			opts, ok := fd.Options().(*descriptorpb.FileOptions)
			ctx.Expect(ok).To(specs.BeTrue())
			const want = "github.com/getsyntegrity/urd/egopb;egopb"
			ctx.Expect(opts.GetGoPackage()).ToEqual(want)
		})
		s.It("declares messages", func(ctx *specs.Context) {
			ctx.Expect(fd.Messages().Len()).To(specs.BeGreaterThan(0))
		})

		msgs := fd.Messages()
		for i := 0; i < msgs.Len(); i++ {
			name := msgs.Get(i).FullName()
			s.It(string(name)+" resolves through the registry and survives a marshal round trip", func(ctx *specs.Context) {
				mt, err := protoregistry.GlobalTypes.FindMessageByName(name)
				ctx.Expect(err).To(specs.BeNil())
				m := mt.New().Interface()
				b, err := proto.Marshal(m)
				ctx.Expect(err).To(specs.BeNil())
				back := mt.New().Interface()
				ctx.Expect(proto.Unmarshal(b, back)).To(specs.BeNil())
				ctx.Expect(proto.Equal(m, back)).To(specs.BeTrue())
			})
		}

		s.It("is registered under ego/ego.proto", func(ctx *specs.Context) {
			found, err := protoregistry.GlobalFiles.FindFileByPath("ego/ego.proto")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(found == fd).To(specs.BeTrue())
		})
	})
}
