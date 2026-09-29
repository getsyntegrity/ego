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

package testpb_test

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/getsyntegrity/ego/test/data/testpb"
)

// See egopb/descriptor_test.go: go_package sits inside the serialized
// descriptor, so the regenerated code must load and round-trip.
func TestDescriptor_IsSoundAndCarriesTheModulePath(t *testing.T) {
	fd := testpb.File_test_test_proto
	if fd == nil {
		t.Fatal("File_test_test_proto is nil")
	}
	opts, ok := fd.Options().(*descriptorpb.FileOptions)
	if !ok {
		t.Fatalf("file options = %T", fd.Options())
	}
	const want = "github.com/getsyntegrity/ego/test/data/testpb;testpb"
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
}
