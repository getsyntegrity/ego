package main

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

func TestParseManifest(t *testing.T) {
	specs.Describe(t, "ParseManifest reads `module-dir | package-dir | TestName` lines", func(s *specs.Spec) {
		s.It("keeps the three fields and the line number, skipping comments and blank lines", func(ctx *specs.Context) {
			got, err := ParseManifest("# header\n\nexample/cluster | . | TestA\n  persistence/conformance|sub/pkg|TestB  \n")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).ToEqual([]Suite{
				{Module: "example/cluster", Dir: ".", Test: "TestA", Line: 3},
				{Module: "persistence/conformance", Dir: "sub/pkg", Test: "TestB", Line: 4},
			})
		})

		type bad struct{ name, text, wantErr string }
		specs.Table(s, []bad{
			{"too few fields", "example/cluster | .\n", "line 1: want `module-dir | package-dir | TestName`"},
			{"too many fields", "a | b | TestC | d\n", "line 1: want `module-dir | package-dir | TestName`"},
			{"an empty field", "a |  | TestC\n", "line 1: the module, package and test fields are all required"},
			{"a name that is not a test", "a | . | helper\n", `line 1: "helper" is not a top-level test name (TestXxx)`},
			{"a lowercase test name", "# c\na | . | Testx\n", `line 2: "Testx" is not a top-level test name (TestXxx)`},
			{"a subtest path", "a | . | TestA/sub\n", `line 1: "TestA/sub" is not a top-level test name (TestXxx)`},
			{"an absolute directory", "/a | . | TestA\n", "line 1: directories must be relative and stay inside the repository"},
			{"a directory that escapes the repository", "a | ../x | TestA\n", "line 1: directories must be relative and stay inside the repository"},
			{"a duplicate entry", "# c\na | . | TestA\na | . | TestA\n", "line 3: duplicate entry a | . | TestA (first at line 2)"},
		}, func(c bad) string { return c.name }, func(ctx *specs.Context, c bad) {
			_, err := ParseManifest(c.text)
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err.Error()).To(specs.Equal(c.wantErr))
		})

		s.It("accepts the same test name in different packages", func(ctx *specs.Context) {
			got, err := ParseManifest("a | . | TestA\na | sub | TestA\nb | . | TestA\n")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.HaveLen(3))
		})
	})
}

func TestPlan(t *testing.T) {
	specs.Describe(t, "Plan groups the suites into one -run regex per package", func(s *specs.Spec) {
		s.It("keeps manifest order and anchors the regex", func(ctx *specs.Context) {
			suites := []Suite{
				{Module: "example/cluster", Dir: ".", Test: "TestA"},
				{Module: "persistence/conformance", Dir: ".", Test: "TestX"},
				{Module: "example/cluster", Dir: ".", Test: "TestB"},
				{Module: "example/cluster", Dir: "sub", Test: "TestC"},
			}
			got := Plan(suites)
			ctx.Expect(got).ToEqual([]PlanLine{
				{Module: "example/cluster", Dir: ".", Run: "^(TestA|TestB)$"},
				{Module: "persistence/conformance", Dir: ".", Run: "^(TestX)$"},
				{Module: "example/cluster", Dir: "sub", Run: "^(TestC)$"},
			})
		})

		s.It("prints a line as module, directory and regex separated by tabs", func(ctx *specs.Context) {
			line := PlanLine{Module: "example/cluster", Dir: ".", Run: "^(TestA)$"}
			ctx.Expect(line.String()).To(specs.Equal("example/cluster\t.\t^(TestA)$"))
		})
	})
}
