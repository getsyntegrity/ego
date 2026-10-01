package main

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

type resourceCase struct {
	name string
	body string // imports and statements of the test body
	want []string
}

// resourceSource wraps a snippet in a go-specs test so only the resource rule can fire.
func resourceSource(imports, body string) string {
	return `package x

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
` + imports + `
)

func TestThing(t *testing.T) {
	specs.Describe(t, "thing", func(s *specs.Spec) {
		s.It("runs", func(ctx *specs.Context) {
` + body + `
		})
	})
}
`
}

func TestScanFlagsRealResourcesInTestFiles(t *testing.T) {
	specs.Describe(t, "Scan reports real resources used by a test file", func(s *specs.Spec) {
		specs.Table(s, []resourceCase{
			{name: "sql.Open", body: "_, _ = sql.Open(\"pgx\", \"x\")", want: []string{"calls database/sql.Open"}},
			{name: "net.Listen", body: "_, _ = net.Listen(\"tcp\", \":0\")", want: []string{"calls net.Listen"}},
			{name: "net.Dial", body: "_, _ = net.Dial(\"tcp\", \"x:1\")", want: []string{"calls net.Dial"}},
			{name: "exec.Command", body: "_ = exec.Command(\"go\", \"list\")", want: []string{"calls os/exec.Command"}},
			{name: "httptest server", body: "_ = httptest.NewServer(nil)", want: []string{"calls net/http/httptest.NewServer"}},
			{name: "goakt actor system", body: "_, _ = actor.NewActorSystem(\"a\")", want: []string{"calls goakt actor.NewActorSystem"}},
			{name: "DSN from the environment", body: "_ = os.Getenv(\"EGO_POSTGRES_DSN\")", want: []string{"reads the DSN variable EGO_POSTGRES_DSN"}},
			{name: "os.Create with no TempDir", body: "_, _ = os.Create(\"out.txt\")", want: []string{"calls os.Create outside t.TempDir"}},
			{name: "os.Create next to TempDir is allowed", body: "_ = ctx.T.TempDir()\n_, _ = os.Create(\"out.txt\")"},
			{name: "an unrelated Getenv is allowed", body: "_ = os.Getenv(\"HOME\")"},
			{name: "a local type named Command is not exec", body: "_ = Command(\"x\")"},
			{name: "each distinct call is listed once, in order", body: "_ = exec.Command(\"a\")\n_ = exec.Command(\"b\")\n_, _ = net.Dial(\"tcp\", \"x\")",
				want: []string{"calls net.Dial", "calls os/exec.Command"}},
		}, func(c resourceCase) string { return c.name }, func(ctx *specs.Context, c resourceCase) {
			got := scanFiles(ctx, map[string]string{"a/a_test.go": resourceSource(resourceImports, c.body)})
			var details []string
			for _, f := range got {
				ctx.Expect(f.Rule).ToEqual(RuleResource)
				details = append(details, f.Detail)
			}
			ctx.Expect(details).ToEqual(c.want)
		})

		s.It("does not apply the resource rule to the inttest module", func(ctx *specs.Context) {
			src := resourceSource(resourceImports, "_, _ = net.Dial(\"tcp\", \"x:1\")\n_ = os.Getenv(\"EGO_POSTGRES_DSN\")")
			ctx.Expect(scanFiles(ctx, map[string]string{"inttest/flows/eventstore/a_test.go": src})).To(specs.BeEmpty())
		})

		s.It("keeps the other rules on in the inttest module", func(ctx *specs.Context) {
			src := "package x\nimport (\n\t\"testing\"\n\t\"github.com/stretchr/testify/require\"\n)\nfunc TestThing(t *testing.T) { require.True(t, true) }\n"
			var rules []Rule
			for _, f := range scanFiles(ctx, map[string]string{"inttest/flows/eventstore/a_test.go": src}) {
				rules = append(rules, f.Rule)
			}
			ctx.Expect(rules).ToEqual([]Rule{RuleNoSpecs, RuleTestify})
		})

		s.It("does not mistake a path that only starts with the same letters for inttest", func(ctx *specs.Context) {
			src := resourceSource(resourceImports, "_, _ = net.Dial(\"tcp\", \"x:1\")")
			ctx.Expect(scanFiles(ctx, map[string]string{"inttestx/a_test.go": src})).To(specs.HaveLen(1))
		})

		s.It("ignores the same calls in a non-test file", func(ctx *specs.Context) {
			src := "package x\nimport \"net\"\nfunc f() { _, _ = net.Listen(\"tcp\", \":0\") }\n"
			ctx.Expect(scanFiles(ctx, map[string]string{"a/a.go": src})).To(specs.BeEmpty())
		})
	})
}

const resourceImports = `
	"database/sql"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"

	"github.com/tochemey/goakt/v4/actor"
`
