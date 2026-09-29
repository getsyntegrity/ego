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

package eventsource

import "google.golang.org/protobuf/proto"

// Command, Event and State mirror the aliases the engine package exports for
// the messages an event sourced behavior handles. They are all proto.Message.
type (
	Command = proto.Message
	Event   = proto.Message
	State   = proto.Message
)

// retentionPolicy is the retention the actor applies after a snapshot. It is
// the actor's private view of extensions.EntityConfig's retention fields; the
// public engine.RetentionPolicy option is translated into that config before
// the actor spawns.
type retentionPolicy struct {
	DeleteEventsOnSnapshot    bool
	DeleteSnapshotsOnSnapshot bool
	EventsRetentionCount      uint64
}
