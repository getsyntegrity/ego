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

// Package projection implements the projection host actor: the singleton that
// runs one projectionrunner.Runner for a registered projection and reports a
// runner that stopped permanently.
//
// The error the actor fails with is built by a hook that package engine sets
// (Actor.SetEscalation), because GoAkt keys supervisor directives by the name
// of the error's Go type and that type, engine.projectionRunnerError, must stay
// in package engine. Package engine also wraps Actor in the ProjectionActor
// cluster kind for the same reason. This package never imports package engine.
//
// The root package github.com/getsyntegrity/ego/projection is imported here
// as egoprojection, since it shares this package's name.
package projection
