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
	goakt "github.com/tochemey/goakt/v4/actor"
	"google.golang.org/protobuf/proto"
)

// SimpleReplyActor is a test actor that responds to any message with a fixed reply.
type SimpleReplyActor struct {
	Reply proto.Message
}

var _ goakt.Actor = (*SimpleReplyActor)(nil)

func (a *SimpleReplyActor) PreStart(_ *goakt.Context) error   { return nil }
func (a *SimpleReplyActor) PostStop(_ *goakt.Context) error   { return nil }
func (a *SimpleReplyActor) Receive(ctx *goakt.ReceiveContext) { ctx.Response(a.Reply) }
