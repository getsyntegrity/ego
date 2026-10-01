package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/getsyntegrity/go-specs/specs"
)

const (
	rootModule    = "example.com/root"
	clusterModule = "example.com/root/example/cluster"
	clusterPkg    = clusterModule
)

const goodManifest = "# suites\nexample/cluster | . | TestA\nexample/cluster | . | TestB\n"

const clusterTests = `package cluster

import "testing"

func TestA(t *testing.T) {}
func TestB(t *testing.T) {}
func helper(t *testing.T) {}
`

// repo is an in-memory repository: a root module and a nested module holding the listed tests.
func repo(extra map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{
		"go.mod":                             {Data: []byte("module " + rootModule + "\n\ngo 1.26\n")},
		"example/cluster/go.mod":             {Data: []byte("module " + clusterModule + "\n\ngo 1.26\n")},
		"example/cluster/stores_test.go":     {Data: []byte(clusterTests)},
		".github/integration-suites.txt":     {Data: []byte(goodManifest)},
		"example/cluster/testdata/x_test.go": {Data: []byte(taggedFile("TestIgnored"))},
	}
	for name, src := range extra {
		fsys[name] = &fstest.MapFile{Data: []byte(src)}
	}
	return fsys
}

func taggedFile(tests ...string) string {
	var b strings.Builder
	b.WriteString("//go:build integration\n\npackage cluster\n\nimport \"testing\"\n\n")
	for _, name := range tests {
		fmt.Fprintf(&b, "func %s(t *testing.T) {}\n", name)
	}
	return b.String()
}

// ev renders one line of `go test -json` output.
func ev(action, pkg, test string) string {
	if test == "" {
		return fmt.Sprintf(`{"Action":%q,"Package":%q}`, action, pkg)
	}
	return fmt.Sprintf(`{"Action":%q,"Package":%q,"Test":%q}`, action, pkg, test)
}

func stream(lines ...string) io.Reader { return strings.NewReader(strings.Join(lines, "\n") + "\n") }

// green is the output of a run in which both listed tests passed.
func green() io.Reader {
	return stream(
		ev("run", clusterPkg, "TestA"), ev("pass", clusterPkg, "TestA"),
		ev("run", clusterPkg, "TestB"), ev("pass", clusterPkg, "TestB"),
		ev("pass", clusterPkg, ""),
	)
}

type gateRun struct {
	code            int
	out, errs, summ string
}

func runGate(opts options, fsys fstest.MapFS, jsons ...io.Reader) gateRun {
	var out, errs, summ bytes.Buffer
	if opts.Manifest == "" {
		opts.Manifest = ".github/integration-suites.txt"
	}
	code := run(fsys, opts, jsons, &out, &errs, &summ)
	return gateRun{code, out.String(), errs.String(), summ.String()}
}

func TestEvaluate(t *testing.T) {
	suites := []Suite{
		{Module: "example/cluster", Dir: ".", Test: "TestA", Line: 2},
		{Module: "example/cluster", Dir: ".", Test: "TestB", Line: 3},
	}
	paths := map[string]string{"example/cluster": clusterModule}

	eval := func(ctx *specs.Context, lines ...string) ([]Row, []string) {
		res := NewResults()
		ctx.Expect(res.Read(stream(lines...))).To(specs.BeNil())
		return Evaluate(suites, paths, res)
	}

	specs.Describe(t, "Evaluate compares the manifest with the go test -json results", func(s *specs.Spec) {
		s.It("reports every executed and passing suite as passed", func(ctx *specs.Context) {
			rows, problems := eval(ctx,
				ev("pass", clusterPkg, "TestA"), ev("pass", clusterPkg, "TestB"), ev("pass", clusterPkg, ""))
			ctx.Expect(problems).To(specs.BeEmpty())
			ctx.Expect(rows).ToEqual([]Row{{suites[0], "passed"}, {suites[1], "passed"}})
		})

		s.It("flags a listed test that never produced a result", func(ctx *specs.Context) {
			rows, problems := eval(ctx, ev("pass", clusterPkg, "TestA"), ev("pass", clusterPkg, ""))
			ctx.Expect(problems).ToEqual([]string{
				"example/cluster | . | TestB: never executed: no result in the go test output",
			})
			ctx.Expect(rows[1].Status).To(specs.Equal("not executed"))
		})

		s.It("flags a skipped test, which is how an unset resource looks", func(ctx *specs.Context) {
			rows, problems := eval(ctx,
				ev("pass", clusterPkg, "TestA"), ev("skip", clusterPkg, "TestB"), ev("pass", clusterPkg, ""))
			ctx.Expect(problems).ToEqual([]string{
				"example/cluster | . | TestB: skipped: a listed test must run, so check that the resource it needs is configured",
			})
			ctx.Expect(rows[1].Status).To(specs.Equal("skipped"))
		})

		s.It("flags a failed test", func(ctx *specs.Context) {
			rows, problems := eval(ctx,
				ev("fail", clusterPkg, "TestA"), ev("pass", clusterPkg, "TestB"), ev("fail", clusterPkg, ""))
			ctx.Expect(problems).ToEqual([]string{"example/cluster | . | TestA: failed"})
			ctx.Expect(rows[0].Status).To(specs.Equal("failed"))
		})

		s.It("reports a package that failed to build, with the tests it never ran", func(ctx *specs.Context) {
			rows, problems := eval(ctx, ev("fail", clusterPkg, ""))
			ctx.Expect(problems).ToEqual([]string{
				"package " + clusterPkg + ": failed (build failure, panic or TestMain exit)",
				"example/cluster | . | TestA: never executed: no result in the go test output",
				"example/cluster | . | TestB: never executed: no result in the go test output",
			})
			ctx.Expect(rows[0].Status).To(specs.Equal("not executed"))
		})

		s.It("ignores subtests, other packages and tests that are not listed", func(ctx *specs.Context) {
			rows, problems := eval(ctx,
				ev("pass", clusterPkg, "TestA"), ev("skip", clusterPkg, "TestA/sub"),
				ev("pass", clusterPkg, "TestB"), ev("fail", clusterPkg, "TestUnlisted"),
				ev("fail", "example.com/other", "TestA"), ev("pass", clusterPkg, ""))
			ctx.Expect(problems).To(specs.BeEmpty())
			ctx.Expect(rows[0].Status).To(specs.Equal("passed"))
		})

		s.It("keeps a failure when the same test ran several times", func(ctx *specs.Context) {
			_, problems := eval(ctx,
				ev("fail", clusterPkg, "TestA"), ev("pass", clusterPkg, "TestA"), ev("pass", clusterPkg, "TestB"),
				ev("pass", clusterPkg, ""))
			ctx.Expect(problems).ToEqual([]string{"example/cluster | . | TestA: failed"})
		})

		s.It("reports a module whose go.mod could not be read instead of guessing its import path", func(ctx *specs.Context) {
			res := NewResults()
			_, problems := Evaluate(suites[:1], map[string]string{}, res)
			ctx.Expect(problems).ToEqual([]string{
				"example/cluster | . | TestA: never executed: the module path of example/cluster is unknown",
			})
		})

		s.It("uses the module path alone for a package in the module root", func(ctx *specs.Context) {
			rootSuite := []Suite{{Module: ".", Dir: ".", Test: "TestR", Line: 1}}
			res := NewResults()
			ctx.Expect(res.Read(stream(ev("pass", rootModule, "TestR")))).To(specs.BeNil())
			_, problems := Evaluate(rootSuite, map[string]string{".": rootModule}, res)
			ctx.Expect(problems).To(specs.BeEmpty())
		})

		s.It("rejects output that is not go test -json", func(ctx *specs.Context) {
			err := NewResults().Read(strings.NewReader("not json\n"))
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
		})
	})
}

func TestCheckSource(t *testing.T) {
	suites := []Suite{
		{Module: "example/cluster", Dir: ".", Test: "TestA", Line: 2},
		{Module: "example/cluster", Dir: ".", Test: "TestB", Line: 3},
	}

	specs.Describe(t, "CheckSource finds stale manifest entries and orphan integration tests", func(s *specs.Spec) {
		s.It("accepts a manifest whose tests all exist and that has no orphan", func(ctx *specs.Context) {
			ctx.Expect(CheckSource(repo(nil), suites)).To(specs.BeEmpty())
		})

		s.It("flags an entry whose test function no longer exists", func(ctx *specs.Context) {
			stale := append([]Suite{}, suites...)
			stale = append(stale, Suite{Module: "example/cluster", Dir: ".", Test: "TestGone", Line: 4})
			ctx.Expect(CheckSource(repo(nil), stale)).ToEqual([]string{
				"example/cluster | . | TestGone: stale entry (manifest line 4): no func TestGone in the _test.go files of example/cluster",
			})
		})

		s.It("does not count a helper that is not a test as the listed test", func(ctx *specs.Context) {
			stale := []Suite{{Module: "example/cluster", Dir: ".", Test: "Testhelper", Line: 2}}
			ctx.Expect(CheckSource(repo(nil), stale)).To(specs.HaveLen(1))
		})

		s.It("flags an entry whose package directory has no test files", func(ctx *specs.Context) {
			missing := []Suite{{Module: "example/cluster", Dir: "nope", Test: "TestA", Line: 2}}
			ctx.Expect(CheckSource(repo(nil), missing)).ToEqual([]string{
				"example/cluster | nope | TestA: stale entry (manifest line 2): no func TestA in the _test.go files of example/cluster/nope",
			})
		})

		s.It("flags a top-level test in an integration-tagged file that the manifest does not list", func(ctx *specs.Context) {
			fsys := repo(map[string]string{"example/cluster/orphan_test.go": taggedFile("TestOrphan", "TestA")})
			ctx.Expect(CheckSource(fsys, suites)).ToEqual([]string{
				"example/cluster/orphan_test.go: TestOrphan is built with the integration tag but is not in the manifest",
			})
		})

		s.It("accepts a tagged test that the manifest lists", func(ctx *specs.Context) {
			fsys := repo(map[string]string{"example/cluster/extra_test.go": taggedFile("TestExtra")})
			listed := append([]Suite{}, suites...)
			listed = append(listed, Suite{Module: "example/cluster", Dir: ".", Test: "TestExtra", Line: 4})
			ctx.Expect(CheckSource(fsys, listed)).To(specs.BeEmpty())
		})

		type tagCase struct {
			name, header string
			orphan       bool
		}
		specs.Table(s, []tagCase{
			{"the plain integration tag", "//go:build integration", true},
			{"integration among other terms", "//go:build integration && !windows", true},
			{"integration as one alternative", "//go:build linux || integration", true},
			{"the tag negated", "//go:build !integration", false},
			{"an unrelated tag", "//go:build linux", false},
			{"no constraint", "// plain comment", false},
		}, func(c tagCase) string { return c.name }, func(ctx *specs.Context, c tagCase) {
			src := c.header + "\n\npackage cluster\n\nimport \"testing\"\n\nfunc TestMaybe(t *testing.T) {}\n"
			problems := CheckSource(repo(map[string]string{"example/cluster/maybe_test.go": src}), suites)
			if c.orphan {
				ctx.Expect(problems).To(specs.HaveLen(1))
			} else {
				ctx.Expect(problems).To(specs.BeEmpty())
			}
		})

		s.It("finds an orphan in any module and skips testdata, vendor, .git and .claude", func(ctx *specs.Context) {
			skipped := taggedFile("TestSkipped")
			fsys := repo(map[string]string{
				"other/pkg/o_test.go":           taggedFile("TestElsewhere"),
				"vendor/v/v_test.go":            skipped,
				".git/x_test.go":                skipped,
				".claude/worktrees/w/w_test.go": skipped,
			})
			ctx.Expect(CheckSource(fsys, suites)).ToEqual([]string{
				"other/pkg/o_test.go: TestElsewhere is built with the integration tag but is not in the manifest",
			})
		})

		s.It("reports a test file that does not parse", func(ctx *specs.Context) {
			fsys := repo(map[string]string{"example/cluster/broken_test.go": "package"})
			problems := CheckSource(fsys, suites)
			ctx.Expect(problems).To(specs.HaveLen(1))
			ctx.Expect(problems[0]).To(specs.StartWith("example/cluster/broken_test.go: cannot parse: "))
		})
	})
}

func TestModulePaths(t *testing.T) {
	specs.Describe(t, "ModulePaths reads the module line of each go.mod", func(s *specs.Spec) {
		s.It("resolves the root and nested modules", func(ctx *specs.Context) {
			suites := []Suite{{Module: "example/cluster"}, {Module: "."}, {Module: "example/cluster"}}
			paths, problems := ModulePaths(repo(nil), suites)
			ctx.Expect(problems).To(specs.BeEmpty())
			ctx.Expect(paths).ToEqual(map[string]string{".": rootModule, "example/cluster": clusterModule})
		})

		s.It("reports a missing go.mod and a go.mod without a module line", func(ctx *specs.Context) {
			fsys := repo(map[string]string{"nomod/go.mod": "go 1.26\n"})
			_, problems := ModulePaths(fsys, []Suite{{Module: "absent"}, {Module: "nomod"}})
			ctx.Expect(problems).ToEqual([]string{
				"module absent: cannot read absent/go.mod: open absent/go.mod: file does not exist",
				"module nomod: nomod/go.mod has no module line",
			})
		})
	})
}

func TestSummary(t *testing.T) {
	specs.Describe(t, "Summary renders the Markdown for the job summary", func(s *specs.Spec) {
		rows := []Row{
			{Suite{Module: "example/cluster", Dir: ".", Test: "TestA"}, "passed"},
			{Suite{Module: "example/cluster", Dir: ".", Test: "TestB"}, "skipped"},
		}

		s.It("lists every suite with its status and the problems", func(ctx *specs.Context) {
			got := Summary(rows, []string{"example/cluster | . | TestB: skipped: x"})
			ctx.Expect(got).To(specs.Equal(strings.Join([]string{
				"### Integration gate: 1 problem(s)",
				"",
				"| Module | Package | Test | Status |",
				"|---|---|---|---|",
				"| example/cluster | . | TestA | passed |",
				"| example/cluster | . | TestB | **skipped** |",
				"",
				"Problems:",
				"",
				"- example/cluster | . | TestB: skipped: x",
				"",
			}, "\n")))
		})

		s.It("says so when everything passed", func(ctx *specs.Context) {
			got := Summary(rows[:1], nil)
			ctx.Expect(got).To(specs.StartWith("### Integration gate: ok (1 suites executed and passed)\n"))
			ctx.Expect(got).To(specs.Not(specs.Contain("Problems:")))
		})
	})
}

func TestRun(t *testing.T) {
	specs.Describe(t, "run ties the manifest, the results and the source together", func(s *specs.Spec) {
		s.It("passes on the green path and writes the summary", func(ctx *specs.Context) {
			r := runGate(options{}, repo(nil), green())
			ctx.Expect(r.code).To(specs.Equal(0))
			ctx.Expect(r.errs).To(specs.BeEmpty())
			ctx.Expect(r.out).To(specs.Equal("integration gate: ok (2 suites)\n"))
			ctx.Expect(r.summ).To(specs.StartWith("### Integration gate: ok (2 suites executed and passed)\n"))
		})

		s.It("fails with exit 1 and names a missing test", func(ctx *specs.Context) {
			r := runGate(options{}, repo(nil), stream(ev("pass", clusterPkg, "TestA"), ev("pass", clusterPkg, "")))
			ctx.Expect(r.code).To(specs.Equal(1))
			ctx.Expect(r.errs).To(specs.Contain("example/cluster | . | TestB: never executed"))
			ctx.Expect(r.summ).To(specs.Contain("| example/cluster | . | TestB | **not executed** |"))
		})

		s.It("fails on a skipped test", func(ctx *specs.Context) {
			r := runGate(options{}, repo(nil), stream(
				ev("pass", clusterPkg, "TestA"), ev("skip", clusterPkg, "TestB"), ev("pass", clusterPkg, "")))
			ctx.Expect(r.code).To(specs.Equal(1))
			ctx.Expect(r.errs).To(specs.Contain("TestB: skipped"))
		})

		s.It("fails on a failed test and on a build failure", func(ctx *specs.Context) {
			failed := runGate(options{}, repo(nil), stream(
				ev("fail", clusterPkg, "TestA"), ev("pass", clusterPkg, "TestB"), ev("fail", clusterPkg, "")))
			ctx.Expect(failed.code).To(specs.Equal(1))
			ctx.Expect(failed.errs).To(specs.Contain("TestA: failed"))

			built := runGate(options{}, repo(nil), stream(ev("fail", clusterPkg, "")))
			ctx.Expect(built.code).To(specs.Equal(1))
			ctx.Expect(built.errs).To(specs.Contain("failed (build failure, panic or TestMain exit)"))
		})

		s.It("combines the results of several go test runs", func(ctx *specs.Context) {
			a := stream(ev("pass", clusterPkg, "TestA"), ev("pass", clusterPkg, ""))
			b := stream(ev("pass", clusterPkg, "TestB"), ev("pass", clusterPkg, ""))
			ctx.Expect(runGate(options{}, repo(nil), a, b).code).To(specs.Equal(0))
		})

		s.It("fails on a stale entry and on an orphan even when the results are green", func(ctx *specs.Context) {
			fsys := repo(map[string]string{
				".github/integration-suites.txt": goodManifest + "example/cluster | . | TestGone\n",
				"example/cluster/orphan_test.go": taggedFile("TestOrphan"),
			})
			r := runGate(options{}, fsys, green())
			ctx.Expect(r.code).To(specs.Equal(1))
			ctx.Expect(r.errs).To(specs.Contain("TestGone: stale entry"))
			ctx.Expect(r.errs).To(specs.Contain("TestOrphan is built with the integration tag"))
		})

		s.It("fails on a malformed manifest, naming the line", func(ctx *specs.Context) {
			fsys := repo(map[string]string{".github/integration-suites.txt": "a | b\n"})
			r := runGate(options{}, fsys, green())
			ctx.Expect(r.code).To(specs.Equal(1))
			ctx.Expect(r.errs).To(specs.Contain("integration gate: manifest: line 1: want"))
		})

		s.It("fails on a duplicate manifest entry", func(ctx *specs.Context) {
			fsys := repo(map[string]string{".github/integration-suites.txt": goodManifest + "example/cluster | . | TestA\n"})
			r := runGate(options{}, fsys, green())
			ctx.Expect(r.code).To(specs.Equal(1))
			ctx.Expect(r.errs).To(specs.Contain("duplicate entry"))
		})

		s.It("exits 2 when the manifest file or the results are missing", func(ctx *specs.Context) {
			noManifest := runGate(options{Manifest: "absent.txt"}, repo(nil), green())
			ctx.Expect(noManifest.code).To(specs.Equal(2))

			noResults := runGate(options{}, repo(nil))
			ctx.Expect(noResults.code).To(specs.Equal(2))
			ctx.Expect(noResults.errs).To(specs.Contain("no go test -json input"))
		})

		s.It("exits 2 when the results are not go test -json", func(ctx *specs.Context) {
			r := runGate(options{}, repo(nil), strings.NewReader("garbage\n"))
			ctx.Expect(r.code).To(specs.Equal(2))
		})

		s.It("checks only the manifest and the source with -check-manifest, needing no results", func(ctx *specs.Context) {
			ok := runGate(options{CheckManifest: true}, repo(nil))
			ctx.Expect(ok.code).To(specs.Equal(0))
			ctx.Expect(ok.out).To(specs.Equal("integration gate: manifest ok (2 suites)\n"))

			fsys := repo(map[string]string{"example/cluster/orphan_test.go": taggedFile("TestOrphan")})
			bad := runGate(options{CheckManifest: true}, fsys)
			ctx.Expect(bad.code).To(specs.Equal(1))
			ctx.Expect(bad.errs).To(specs.Contain("TestOrphan"))
			ctx.Expect(bad.summ).To(specs.BeEmpty())
		})

		s.It("prints one tab-separated -run line per package with -plan", func(ctx *specs.Context) {
			r := runGate(options{Plan: true}, repo(nil))
			ctx.Expect(r.code).To(specs.Equal(0))
			ctx.Expect(r.out).To(specs.Equal("example/cluster\t.\t^(TestA|TestB)$\n"))
		})

		s.It("refuses to print a plan for a malformed manifest", func(ctx *specs.Context) {
			fsys := repo(map[string]string{".github/integration-suites.txt": "a | b\n"})
			r := runGate(options{Plan: true}, fsys)
			ctx.Expect(r.code).To(specs.Equal(1))
			ctx.Expect(r.out).To(specs.BeEmpty())
		})
	})
}
