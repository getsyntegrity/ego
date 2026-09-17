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

package ego

import (
	"context"

	"github.com/pablogore/ego/v4/command"
)

// commandCarrierContextKey is this package's own unexported context.Context
// key type for a command.Carrier, mirroring tenancy's contextKey pattern
// (tenancy/context.go) so it can never collide with a key used by another
// package.
type commandCarrierContextKey struct{}

// commandCarrierKey is the single key under which a command.Carrier is
// stored in a context.Context by attachCarrier.
var commandCarrierKey = commandCarrierContextKey{}

// attachCarrier binds c into ctx under this package's own key and returns
// the resulting context. This is how Metadata crosses the goakt actor
// boundary (M-3, design.md option (c), #60): on a local hop goakt's
// SendSync passes ctx through unchanged into the receiving actor's
// ReceiveContext, so metadataFromContext can rematerialize the same
// Metadata on the other side via command.UnmarshalMetadata without a wire
// change. It does not yet cover a genuinely remote or cluster hop — see
// the #60 final report for that explicitly deferred gap.
func attachCarrier(ctx context.Context, c command.Carrier) context.Context {
	return context.WithValue(ctx, commandCarrierKey, c)
}

// carrierFromContext returns the command.Carrier bound to ctx by
// attachCarrier, if any. The second return value is false when no Carrier
// is attached — e.g. a caller that reaches an entity directly via
// actorSystem.NoSender() instead of Engine.Dispatch/SendCommand or a
// migrated SagaActor.
func carrierFromContext(ctx context.Context) (command.Carrier, bool) {
	c, ok := ctx.Value(commandCarrierKey).(command.Carrier)
	return c, ok
}

// metadataFromContext extracts the command.Carrier bound to ctx and
// rematerializes it into a command.Metadata via command.UnmarshalMetadata.
// The second return value is false when no Carrier is attached, or the
// attached Carrier fails to unmarshal (e.g. it is missing a required
// field) — both are treated as "no envelope metadata available" by
// callers, which fall back to the legacy Command-only dispatch path rather
// than failing the command outright.
func metadataFromContext(ctx context.Context) (command.Metadata, bool) {
	c, ok := carrierFromContext(ctx)
	if !ok {
		return command.Metadata{}, false
	}
	md, err := command.UnmarshalMetadata(c)
	if err != nil {
		return command.Metadata{}, false
	}
	return md, true
}
