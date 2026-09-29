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

package enginetest

// MistypedExtension is a goakt extension.Extension whose ID() collides with
// a real extension slot (e.g. extensions.SnapshotStoreExtensionID) but whose
// concrete type does not match what an actor PreStart expects
// there. It simulates a wiring bug where the wrong extension ends up
// registered under an existing extension ID.
//
// See issue #99: an actor PreStart nil-checks
// extensions.SnapshotStoreExtensionID and extensions.EncryptorExtensionID
// before asserting their type (both are genuinely optional dependencies),
// but the assertion itself was unchecked
// (ext.(*extensions.SnapshotStoreExt), ext.(*extensions.EncryptorExtension)).
// A present-but-mismatched-type extension still panics with an
// unrecoverable "interface conversion" error, and that panic crashes the
// whole process instead of just failing the one Spawn call, because
// goakt drives Spawn/SpawnChild through a
// golang.org/x/sync/singleflight.Group that deliberately re-panics a
// recovered panic on a fresh, unrecoverable goroutine (see
// extensions.Require).
type MistypedExtension struct {
	Name string
}

func (m *MistypedExtension) ID() string { return m.Name }
