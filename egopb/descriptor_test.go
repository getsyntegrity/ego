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

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/getsyntegrity/ego/egopb"
)

// The go_package option lives inside the length-prefixed serialized file
// descriptor, so a text rename of ego.pb.go compiles but panics at init.
// This test proves the regenerated descriptor is sound: it loads, carries
// the module's current package path, and every message resolves through the
// global registry and survives a marshal/unmarshal round trip.
func TestDescriptor_IsSoundAndCarriesTheModulePath(t *testing.T) {
	fd := egopb.File_ego_ego_proto
	if fd == nil {
		t.Fatal("File_ego_ego_proto is nil")
	}

	opts, ok := fd.Options().(*descriptorpb.FileOptions)
	if !ok {
		t.Fatalf("file options = %T", fd.Options())
	}
	const want = "github.com/getsyntegrity/ego/egopb;egopb"
	if got := opts.GetGoPackage(); got != want {
		t.Fatalf("go_package = %q, want %q", got, want)
	}

	msgs := fd.Messages()
	if msgs.Len() == 0 {
		t.Fatal("descriptor has no messages")
	}
	for i := 0; i < msgs.Len(); i++ {
		name := msgs.Get(i).FullName()
		mt, err := protoregistry.GlobalTypes.FindMessageByName(name)
		if err != nil {
			t.Fatalf("registry lookup %s: %v", name, err)
		}
		m := mt.New().Interface()
		b, err := proto.Marshal(m)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		back := mt.New().Interface()
		if err := proto.Unmarshal(b, back); err != nil {
			t.Fatalf("unmarshal %s: %v", name, err)
		}
		if !proto.Equal(m, back) {
			t.Errorf("%s did not round-trip", name)
		}
	}

	found, err := protoregistry.GlobalFiles.FindFileByPath("ego/ego.proto")
	if err != nil || protoreflect.FileDescriptor(found) != fd {
		t.Fatalf("file not registered under ego/ego.proto: %v", err)
	}
}
