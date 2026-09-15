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

package command

import "google.golang.org/protobuf/proto"

// Envelope pairs a caller-supplied command payload with its canonical
// Metadata (AC1). It is always constructed by the calling application —
// this package never synthesizes an Envelope internally from metadata it
// did not receive. Envelope is not generic (D2): a non-generic payload
// lets heterogeneous collections (e.g. a saga's compensation list) share
// one slice type, while PayloadAs recovers compile-checked typed access
// where a caller knows the concrete payload type.
type Envelope struct {
	payload  proto.Message
	metadata Metadata
}

// NewEnvelope builds an Envelope pairing payload with md. payload must be
// non-nil (ErrInvalidEnvelope otherwise).
func NewEnvelope(payload proto.Message, md Metadata) (Envelope, error) {
	if payload == nil {
		return Envelope{}, NewError(ErrInvalidEnvelope, "command: envelope payload must not be nil", nil)
	}
	return Envelope{payload: payload, metadata: md}, nil
}

// Payload returns e's command payload.
func (e Envelope) Payload() proto.Message {
	return e.payload
}

// Metadata returns e's canonical metadata.
func (e Envelope) Metadata() Metadata {
	return e.metadata
}

// Derive builds a child Envelope carrying payload under metadata derived
// from e's own via Metadata.Derive(op, opts...) (D7). payload must be
// non-nil (ErrInvalidEnvelope otherwise).
func (e Envelope) Derive(payload proto.Message, op OperationID, opts ...MetadataOption) (Envelope, error) {
	if payload == nil {
		return Envelope{}, NewError(ErrInvalidEnvelope, "command: envelope payload must not be nil", nil)
	}
	md, err := e.metadata.Derive(op, opts...)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{payload: payload, metadata: md}, nil
}

// PayloadAs recovers e's payload as concrete type T. The second return
// value is false when the payload is not of type T.
func PayloadAs[T proto.Message](e Envelope) (T, bool) {
	v, ok := e.payload.(T)
	return v, ok
}
