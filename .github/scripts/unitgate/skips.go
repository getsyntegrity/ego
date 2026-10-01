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

package main

import (
	"go/ast"
	"sort"
)

// skipFindings reports every call that can make a test skip or shrink itself, in a file of the inttest module:
// a call to a method named Skip, Skipf or SkipNow on any expression (t.Skip, tb.Skipf, s.T().SkipNow and so
// on, because the receiver can have any name), a call to the go-specs equivalents SkipIt, PendingIt and FIt
// (on a Spec or a Builder; FIt focuses one case, so every other case of the run is skipped), and a call to
// testing.Short.
//
// The inttest module exists so that a missing dependency fails the run instead of skipping it. A skip there
// brings back the green-but-empty result the module replaced, so the rule has no allowlist. It checks
// non-test files too, because a helper in inttest/infra can call tb.Skip on behalf of a test. It is syntactic:
// a method called Skip on a type that has nothing to do with testing is flagged as well, and is better renamed.
func skipFindings(p string, f *ast.File, imports map[string]string) []Finding {
	if !insideInttest(p) {
		return nil
	}
	details := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			switch sel.Sel.Name {
			case "Skip", "Skipf", "SkipNow", "SkipIt", "PendingIt", "FIt":
				details["calls "+sel.Sel.Name] = true
			}
		}
		if qualifiedCall(call, imports) == "testing.Short" {
			details["calls testing.Short"] = true
		}
		return true
	})

	var out []Finding
	for d := range details {
		out = append(out, Finding{Path: p, Rule: RuleSkip, Detail: d})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Detail < out[j].Detail })
	return out
}
