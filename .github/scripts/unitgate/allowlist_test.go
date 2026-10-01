package main

import (
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

func TestParseAllowlist(t *testing.T) {
	specs.Describe(t, "ParseAllowlist reads `path | note` lines", func(s *specs.Spec) {
		s.It("keeps path and note, skipping comments and blank lines", func(ctx *specs.Context) {
			got, err := ParseAllowlist("# header\n\nengine/a_test.go | PR #245\nmocks/ | retired after #205\n")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).ToEqual([]Entry{
				{Path: "engine/a_test.go", Note: "PR #245"},
				{Path: "mocks/", Note: "retired after #205"},
			})
		})

		specs.Table(s, []struct{ name, text, wantErr string }{
			{"a missing note", "engine/a_test.go\n", "line 1: want `path | note`"},
			{"an empty note", "engine/a_test.go |  \n", "line 1: the note is required"},
			{"an empty path", " | PR #1\n", "line 1: the path is required"},
			{"a duplicate path", "a_test.go | x\na_test.go | y\n", "line 2: duplicate entry a_test.go"},
		}, func(c struct{ name, text, wantErr string }) string { return c.name },
			func(ctx *specs.Context, c struct{ name, text, wantErr string }) {
				_, err := ParseAllowlist(c.text)
				ctx.Expect(err).To(specs.Not(specs.BeNil()))
				ctx.Expect(err.Error()).To(specs.Equal(c.wantErr))
			})
	})
}

type evalCase struct {
	name         string
	findings     []Finding
	pending      string
	resources    string
	strict       bool
	wantProblems []string
	wantWarnings []string
}

const (
	stalePending   = "engine/a_test.go: stale pending entry (PR #245): the file no longer violates the unit-test rules, remove the line"
	staleResources = "engine/b_test.go: stale resources entry (loopback server): the file no longer uses a real resource, remove the line"
)

func TestEvaluate(t *testing.T) {
	testify := Finding{Path: "engine/a_test.go", Rule: RuleTestify, Detail: "imports github.com/stretchr/testify/require"}
	mocks := Finding{Path: "mocks/ego/m.go", Rule: RuleTestify, Detail: "imports github.com/stretchr/testify/mock"}
	net := Finding{Path: "engine/b_test.go", Rule: RuleResource, Detail: "calls net.Listen"}
	testifyLine := "engine/a_test.go: testify: imports github.com/stretchr/testify/require"

	specs.Describe(t, "Evaluate turns findings and allowlists into problems and warnings", func(s *specs.Spec) {
		specs.Table(s, []evalCase{
			{name: "no findings and no entries is clean"},
			{name: "a violation that is not allowlisted fails", findings: []Finding{testify},
				wantProblems: []string{testifyLine}},
			{name: "a pending entry covers its file", findings: []Finding{testify}, pending: "engine/a_test.go | PR #245"},
			{name: "a pending directory entry covers every file below it", findings: []Finding{mocks}, pending: "mocks/ | retired after #205"},
			{name: "a stale pending entry only warns", pending: "engine/a_test.go | PR #245",
				wantWarnings: []string{stalePending}},
			{name: "a stale pending entry fails in strict mode", pending: "engine/a_test.go | PR #245", strict: true,
				wantProblems: []string{stalePending}},
			{name: "a stale pending directory entry warns", pending: "mocks/ | retired after #205",
				wantWarnings: []string{"mocks/: stale pending entry (retired after #205): the file no longer violates the unit-test rules, remove the line"}},
			{name: "a pending entry does not cover a resource violation", findings: []Finding{net}, pending: "engine/b_test.go | PR #1",
				wantProblems: []string{"engine/b_test.go: resource: calls net.Listen"},
				wantWarnings: []string{"engine/b_test.go: stale pending entry (PR #1): the file no longer violates the unit-test rules, remove the line"}},
			{name: "a resources entry covers a resource violation", findings: []Finding{net}, resources: "engine/b_test.go | loopback server"},
			{name: "a stale resources entry only warns", resources: "engine/b_test.go | loopback server",
				wantWarnings: []string{staleResources}},
			{name: "a stale resources entry fails in strict mode", resources: "engine/b_test.go | loopback server", strict: true,
				wantProblems: []string{staleResources}},
			{name: "a resources entry does not cover an import violation", findings: []Finding{testify}, resources: "engine/a_test.go | why",
				wantProblems: []string{testifyLine},
				wantWarnings: []string{"engine/a_test.go: stale resources entry (why): the file no longer uses a real resource, remove the line"}},
			{name: "strict mode keeps new violations as problems before stale entries", findings: []Finding{testify}, pending: "engine/z_test.go | PR #2", strict: true,
				wantProblems: []string{testifyLine, "engine/z_test.go: stale pending entry (PR #2): the file no longer violates the unit-test rules, remove the line"}},
		}, func(c evalCase) string { return c.name }, func(ctx *specs.Context, c evalCase) {
			pending, err := ParseAllowlist(c.pending)
			ctx.Expect(err).To(specs.BeNil())
			resources, err := ParseAllowlist(c.resources)
			ctx.Expect(err).To(specs.BeNil())
			problems, warnings := Evaluate(c.findings, pending, resources, c.strict)
			ctx.Expect(problems).ToEqual(c.wantProblems)
			ctx.Expect(warnings).ToEqual(c.wantWarnings)
		})
	})
}

func TestRunReportsProblemsAndExitCode(t *testing.T) {
	specs.Describe(t, "run scans, evaluates and prints one line per problem", func(s *specs.Spec) {
		s.It("returns 0 and prints a summary on a clean tree", func(ctx *specs.Context) {
			var out strings.Builder
			code := run(memFS(map[string]string{"a/a_test.go": cleanTest}), "", "", false, &out)
			ctx.Expect(code).ToEqual(0)
			ctx.Expect(out.String()).To(specs.StartWith("unit-test gate: ok"))
		})

		s.It("returns 1 and names the file on a testify import", func(ctx *specs.Context) {
			var out strings.Builder
			bad := "package x\nimport _ \"github.com/stretchr/testify/require\"\n"
			code := run(memFS(map[string]string{"a/a.go": bad}), "", "", false, &out)
			ctx.Expect(code).ToEqual(1)
			ctx.Expect(out.String()).To(specs.Contain("a/a.go: testify: imports github.com/stretchr/testify/require"))
		})

		s.It("returns 0 and prints a warning on a stale pending entry", func(ctx *specs.Context) {
			var out strings.Builder
			code := run(memFS(map[string]string{"a/a_test.go": cleanTest}), "a/a_test.go | PR #9\n", "", false, &out)
			ctx.Expect(code).ToEqual(0)
			ctx.Expect(out.String()).To(specs.StartWith("unit-test gate: ok"))
			ctx.Expect(out.String()).To(specs.Contain("warning: a/a_test.go: stale pending entry (PR #9)"))
		})

		s.It("returns 1 on a stale pending entry in strict mode", func(ctx *specs.Context) {
			var out strings.Builder
			code := run(memFS(map[string]string{"a/a_test.go": cleanTest}), "a/a_test.go | PR #9\n", "", true, &out)
			ctx.Expect(code).ToEqual(1)
			ctx.Expect(out.String()).To(specs.Contain("stale pending entry (PR #9)"))
		})

		s.It("still fails on a new violation and prints the warnings after it", func(ctx *specs.Context) {
			var out strings.Builder
			bad := "package x\nimport _ \"github.com/stretchr/testify/require\"\n"
			code := run(memFS(map[string]string{"a/a.go": bad}), "gone_test.go | PR #9\n", "", false, &out)
			ctx.Expect(code).ToEqual(1)
			ctx.Expect(out.String()).To(specs.Contain("a/a.go: testify"))
			ctx.Expect(out.String()).To(specs.Contain("warning: gone_test.go: stale pending entry"))
		})

		s.It("returns 2 on a malformed allowlist", func(ctx *specs.Context) {
			var out strings.Builder
			code := run(memFS(nil), "no-note\n", "", false, &out)
			ctx.Expect(code).ToEqual(2)
			ctx.Expect(out.String()).To(specs.Contain("pending list: line 1"))
		})
	})
}
