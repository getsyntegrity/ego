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

package engine

import (
	goakt "github.com/tochemey/goakt/v4/actor"

	"github.com/getsyntegrity/ego/internal/extensions"
)

// requireExtension looks up the extension registered under extensionID on the
// actor system reachable through ctx and asserts it to type T. It returns an
// error wrapping ErrMissingRequiredExtensions instead of panicking when the
// extension is missing or has an unexpected type; see extensions.Require,
// which the actors outside this package share, for why that matters.
func requireExtension[T any](ctx *goakt.Context, extensionID string) (T, error) {
	return extensions.Require[T](ctx, extensionID)
}

// optionalExtension looks up the extension registered under extensionID like
// requireExtension, but treats a missing registration as valid: it returns
// the zero value and a nil error. A mismatched type is still an error
// wrapping ErrMissingRequiredExtensions; see extensions.Optional, which the
// actors outside this package share.
func optionalExtension[T any](ctx *goakt.Context, extensionID string) (T, error) {
	return extensions.Optional[T](ctx, extensionID)
}
