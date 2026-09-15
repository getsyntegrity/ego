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

// Principal is an abstract security-identity slot: who is issuing a
// command. It carries no concrete auth mechanism, credential, token, role,
// scope or wire-protocol detail (AC14) — only an opaque identity reference
// and an optional, caller-defined kind label.
type Principal struct {
	id      string
	kind    string
	hasKind bool
}

// PrincipalOption configures optional Principal fields.
type PrincipalOption func(*Principal)

// WithPrincipalKind sets an optional, caller-defined classification for
// the principal (e.g. "user", "service-account"). The value is opaque to
// this package.
func WithPrincipalKind(kind string) PrincipalOption {
	return func(p *Principal) {
		p.kind = kind
		p.hasKind = true
	}
}

// NewPrincipal builds a Principal identified by id. id is required; kind
// is optional.
func NewPrincipal(id string, opts ...PrincipalOption) (Principal, error) {
	if id == "" {
		return Principal{}, NewError(ErrInvalidPrincipal, "command: principal id must not be empty", nil)
	}

	p := Principal{id: id}
	for _, opt := range opts {
		opt(&p)
	}
	return p, nil
}

// ID returns the principal's opaque identity reference.
func (p Principal) ID() string {
	return p.id
}

// Kind returns the principal's caller-defined classification, if set.
func (p Principal) Kind() (string, bool) {
	return p.kind, p.hasKind
}
