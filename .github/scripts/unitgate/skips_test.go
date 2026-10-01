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
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

type skipCase struct {
	name string
	path string
	src  string
	want []string
}

// skipSource wraps body in a function that has a testing.T named t and a testing.TB named tb in scope.
func skipSource(body string) string {
	return "package x\nimport \"testing\"\nfunc helper(t *testing.T, tb testing.TB) {\n" + body + "\n}\n"
}

func TestScanRejectsSkipsInsideInttest(t *testing.T) {
	specs.Describe(t, "Scan reports skips under inttest/", func(s *specs.Spec) {
		specs.Table(s, []skipCase{
			{name: "t.Skip", path: "inttest/a/a_test.go", src: skipSource(`t.Skip("no docker")`), want: []string{"calls Skip"}},
			{name: "t.Skipf", path: "inttest/a/a_test.go", src: skipSource(`t.Skipf("no %s", "docker")`), want: []string{"calls Skipf"}},
			{name: "t.SkipNow", path: "inttest/a/a_test.go", src: skipSource(`t.SkipNow()`), want: []string{"calls SkipNow"}},
			{name: "a receiver named tb", path: "inttest/a/a_test.go", src: skipSource(`tb.Skip("x")`), want: []string{"calls Skip"}},
			{name: "a receiver that is a call result", path: "inttest/a/a_test.go",
				src: "package x\nimport \"testing\"\ntype s struct{}\nfunc (s) T() *testing.T { return nil }\nfunc f(x s) { x.T().Skip(\"x\") }\n", want: []string{"calls Skip"}},
			{name: "a receiver that is a field", path: "inttest/a/a_test.go",
				src: "package x\nimport \"testing\"\ntype s struct{ T *testing.T }\nfunc f(x s) { x.T.Skipf(\"x\") }\n", want: []string{"calls Skipf"}},
			{name: "s.SkipIt on a go-specs Spec", path: "inttest/a/a_test.go",
				src: "package x\nimport \"github.com/getsyntegrity/go-specs/specs\"\nfunc f(s *specs.Spec) { s.SkipIt(\"x\", nil) }\n", want: []string{"calls SkipIt"}},
			{name: "s.PendingIt on a go-specs Spec", path: "inttest/a/a_test.go",
				src: "package x\nimport \"github.com/getsyntegrity/go-specs/specs\"\nfunc f(s *specs.Spec) { s.PendingIt(\"x\", nil) }\n", want: []string{"calls PendingIt"}},
			{name: "s.FIt on a go-specs Spec", path: "inttest/a/a_test.go",
				src: "package x\nimport \"github.com/getsyntegrity/go-specs/specs\"\nfunc f(s *specs.Spec) { s.FIt(\"x\", nil) }\n", want: []string{"calls FIt"}},
			{name: "b.SkipIt on a go-specs Builder", path: "inttest/a/a_test.go",
				src: "package x\nimport \"github.com/getsyntegrity/go-specs/specs\"\nfunc f(b *specs.Builder) { b.SkipIt(\"x\", nil) }\n", want: []string{"calls SkipIt"}},
			{name: "b.PendingIt on a go-specs Builder", path: "inttest/a/a_test.go",
				src: "package x\nimport \"github.com/getsyntegrity/go-specs/specs\"\nfunc f(b *specs.Builder) { b.PendingIt(\"x\", nil) }\n", want: []string{"calls PendingIt"}},
			{name: "b.FIt on a go-specs Builder", path: "inttest/a/a_test.go",
				src: "package x\nimport \"github.com/getsyntegrity/go-specs/specs\"\nfunc f(b *specs.Builder) { b.FIt(\"x\", nil) }\n", want: []string{"calls FIt"}},
			{name: "testing.Short", path: "inttest/a/a_test.go",
				src: "package x\nimport \"testing\"\nfunc f() bool { return testing.Short() }\n", want: []string{"calls testing.Short"}},
			{name: "testing.Short through an import alias", path: "inttest/a/a_test.go",
				src: "package x\nimport tt \"testing\"\nfunc f() bool { return tt.Short() }\n", want: []string{"calls testing.Short"}},
			{name: "a non-test file", path: "inttest/infra/helper.go", src: skipSource(`tb.Skip("x")`), want: []string{"calls Skip"}},
			{name: "each distinct call is listed once, in order", path: "inttest/a/a_test.go",
				src: skipSource("t.Skip(\"a\")\nt.Skip(\"b\")\ntb.SkipNow()\n_ = testing.Short()"), want: []string{"calls Skip", "calls SkipNow", "calls testing.Short"}},
			{name: "the same calls outside inttest are allowed", path: "engine/a_test.go",
				src: skipSource("t.Skip(\"x\")\n_ = testing.Short()")},
			{name: "a path that only starts with the same letters is outside inttest", path: "inttestx/a_test.go",
				src: skipSource(`t.Skip("x")`)},
			{name: "a method that only starts with Skip is allowed", path: "inttest/a/a_test.go",
				src: "package x\ntype p struct{}\nfunc (p) Skipper() {}\nfunc f(x p) { x.Skipper() }\n"},
			{name: "a local function named Short is not testing.Short", path: "inttest/a/a_test.go",
				src: "package x\nfunc Short() bool { return true }\nfunc f() bool { return Short() }\n"},
		}, func(c skipCase) string { return c.name }, func(ctx *specs.Context, c skipCase) {
			var details []string
			for _, f := range scanFiles(ctx, map[string]string{c.path: c.src}) {
				ctx.Expect(f.Rule).ToEqual(RuleSkip)
				ctx.Expect(f.Path).ToEqual(c.path)
				details = append(details, f.Detail)
			}
			ctx.Expect(details).ToEqual(c.want)
		})

		s.It("cannot be excused by an allowlist entry", func(ctx *specs.Context) {
			findings := []Finding{{Path: "inttest/a/a_test.go", Rule: RuleSkip, Detail: "calls Skip"}}
			pending, err := ParseAllowlist("inttest/ | not allowed")
			ctx.Expect(err).To(specs.BeNil())
			resources, err := ParseAllowlist("inttest/ | not allowed")
			ctx.Expect(err).To(specs.BeNil())

			problems, _ := Evaluate(findings, pending, resources, false)
			ctx.Expect(problems).To(specs.HaveLen(1))
			ctx.Expect(problems[0]).ToEqual("inttest/a/a_test.go: no-skip: calls Skip; a test under inttest/ must fail when its dependency is missing, never skip, pend or focus")
		})
	})
}
