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
	runtimeport "github.com/getsyntegrity/urd/port/runtime"
)

// SupervisorDirective defines the action a supervisor takes when an entity
// fails or panics during message processing. It is an alias of
// [runtimeport.SupervisorDirective], so engine.SupervisorDirective and
// runtime.SupervisorDirective are the same type.
type SupervisorDirective = runtimeport.SupervisorDirective

// The supervisor directives, as constants of the same type and value as their
// port/runtime counterparts.
const (
	// StopDirective is [runtimeport.StopDirective].
	StopDirective = runtimeport.StopDirective
	// RestartDirective is [runtimeport.RestartDirective].
	RestartDirective = runtimeport.RestartDirective
)
