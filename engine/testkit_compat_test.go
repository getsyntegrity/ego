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
	behaviorport "github.com/getsyntegrity/urd/port/behavior"
	"github.com/getsyntegrity/urd/testkit"
)

// The testkit declares the behavior subsets it needs rather than importing ego,
// because ego's own tests import the testkit. Structural compatibility only holds
// while Command, Event, and State are aliases for proto.Message: turning any of
// them back into a defined type makes the signatures differ, and no behavior the
// engine accepts would compile against the scenario API. These assertions fail
// the build the moment that happens.
//
// The runtime-neutral contracts in port/behavior must satisfy the same subsets,
// so a behavior written against them can be driven by the testkit scenarios.
var (
	_ testkit.EventSourcedBehavior = (EventSourcedBehavior)(nil)
	_ testkit.DurableStateBehavior = (DurableStateBehavior)(nil)
	_ testkit.EventSourcedBehavior = (behaviorport.EventSourced)(nil)
	_ testkit.DurableStateBehavior = (behaviorport.DurableState)(nil)
)
