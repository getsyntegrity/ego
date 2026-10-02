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

// Package protocol is the command protocol between the engine and the actors
// it spawns: the command metadata carried in the context of a dispatch, the
// deadline gate an actor applies before running a handler, the parsing of a
// command reply and its classification into a command.Result, the tenant
// binding answer, and the names of the event and state streams.
//
// It depends only on Urd's contract packages, so every actor package and the
// engine facade can share it without importing each other.
package protocol

const (
	// EventsTopic is the single in-process pub/sub topic Urd's event-sourced
	// entities publish to and the engine's publishers/subscribers consume
	// from. The shard each event belongs to is carried in egopb.Event.Shard,
	// so downstream consumers can filter by shard without the topic name
	// having to encode it.
	EventsTopic = "topic.events"

	// StatesTopic is the single in-process pub/sub topic Urd's durable-state
	// entities publish to and the engine's state publishers/subscribers
	// consume from. The shard each state version belongs to is carried in
	// egopb.DurableState.Shard, so downstream consumers can filter by shard
	// without the topic name having to encode it.
	StatesTopic = "topic.states"
)
