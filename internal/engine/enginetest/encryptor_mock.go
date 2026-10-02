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

import (
	"context"

	"github.com/getsyntegrity/go-specs/mock"

	"github.com/getsyntegrity/urd/encryption"
)

// EncryptorMock is an encryption.Encryptor backed by a go-specs
// mock.Controller. Every method forwards its call to the controller under the
// method's own name, and the case declares the answer with
// c.Method("Name").Expect(...).Return(...). Build the controller with
// mock.NewController(ctx) so its expectations are verified when the case ends.
type EncryptorMock struct{ c *mock.Controller }

var _ encryption.Encryptor = (*EncryptorMock)(nil)

// NewEncryptorMock returns an EncryptorMock that forwards to c.
func NewEncryptorMock(c *mock.Controller) *EncryptorMock { return &EncryptorMock{c: c} }

// Encrypt forwards to the controller. Its results are the ciphertext, the key
// ID and the error, in that order.
func (m *EncryptorMock) Encrypt(ctx context.Context, persistenceID string, plaintext []byte) ([]byte, string, error) {
	r := m.c.Method("Encrypt").Call(ctx, persistenceID, plaintext)
	return mock.Value[[]byte](r, 0), mock.Value[string](r, 1), r.Err(2)
}

// Decrypt forwards to the controller. Its results are the plaintext and the
// error, in that order.
func (m *EncryptorMock) Decrypt(ctx context.Context, persistenceID string, ciphertext []byte, keyID string) ([]byte, error) {
	r := m.c.Method("Decrypt").Call(ctx, persistenceID, ciphertext, keyID)
	return mock.Value[[]byte](r, 0), r.Err(1)
}
