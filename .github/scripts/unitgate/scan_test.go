package main

import (
	"testing"
	"testing/fstest"

	"github.com/getsyntegrity/go-specs/specs"
)

// scanFiles runs the scanner over an in-memory tree, so these tests touch no disk.
func scanFiles(ctx *specs.Context, files map[string]string) []Finding {
	fsys := fstest.MapFS{}
	for name, src := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(src)}
	}
	got, err := Scan(fsys)
	ctx.Expect(err).To(specs.BeNil())
	return got
}

type scanCase struct {
	name  string
	files map[string]string
	want  []Finding
}

const cleanTest = `package x

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

func TestThing(t *testing.T) {
	specs.Describe(t, "thing", func(s *specs.Spec) {})
}
`

func TestScanAppliesTheImportAndSpecsRules(t *testing.T) {
	specs.Describe(t, "Scan reports imports and missing specs per file", func(s *specs.Spec) {
		specs.Table(s, []scanCase{
			{
				name:  "a clean go-specs test passes",
				files: map[string]string{"a/a_test.go": cleanTest},
			},
			{
				name: "testify in a test file fails",
				files: map[string]string{"a/a_test.go": `package x
import (
	"testing"
	"github.com/stretchr/testify/require"
	"github.com/getsyntegrity/go-specs/specs"
)
func TestThing(t *testing.T) { require.True(t, true); specs.Describe(t, "x", nil) }
`},
				want: []Finding{{Path: "a/a_test.go", Rule: RuleTestify, Detail: "imports github.com/stretchr/testify/require"}},
			},
			{
				name: "testify in a non-test file fails",
				files: map[string]string{"a/a.go": `package x
import _ "github.com/stretchr/testify/mock"
`},
				want: []Finding{{Path: "a/a.go", Rule: RuleTestify, Detail: "imports github.com/stretchr/testify/mock"}},
			},
			{
				name: "generated mocks outside mocks/ fail",
				files: map[string]string{"internal/b/b_test.go": `package b
import _ "github.com/getsyntegrity/ego/mocks/persistence"
`},
				want: []Finding{{Path: "internal/b/b_test.go", Rule: RuleMocks, Detail: "imports github.com/getsyntegrity/ego/mocks/persistence"}},
			},
			{
				name: "the mocks package itself may import its siblings",
				files: map[string]string{"mocks/persistence/a.go": `package persistence
import _ "github.com/getsyntegrity/ego/mocks/ego"
`},
			},
			{
				name: "a Test function without Describe fails",
				files: map[string]string{"a/a_test.go": `package x
import "testing"
func TestThing(t *testing.T) {}
`},
				want: []Finding{{Path: "a/a_test.go", Rule: RuleNoSpecs, Detail: "declares TestThing without specs.Describe"}},
			},
			{
				name: "Describe through an import alias counts",
				files: map[string]string{"a/a_test.go": `package x
import (
	"testing"
	gs "github.com/getsyntegrity/go-specs/specs"
)
func TestThing(t *testing.T) { gs.Describe(t, "x", nil) }
`},
			},
			{
				name: "a Describe from some other package does not count",
				files: map[string]string{"a/a_test.go": `package x
import "testing"
type fake struct{}
func (fake) Describe(*testing.T) {}
func TestThing(t *testing.T) { fake{}.Describe(t) }
`},
				want: []Finding{{Path: "a/a_test.go", Rule: RuleNoSpecs, Detail: "declares TestThing without specs.Describe"}},
			},
			{
				name: "fuzz, benchmark and TestMain alone are exempt",
				files: map[string]string{"a/a_test.go": `package x
import "testing"
func FuzzThing(f *testing.F) {}
func BenchmarkThing(b *testing.B) {}
func TestMain(m *testing.M) {}
`},
			},
			{
				name: "a helper named Test* with another signature is not a test",
				files: map[string]string{"a/a_test.go": `package x
import "testing"
func Testify(x int) {}
func Testdata() {}
`},
			},
			{
				name:  "files under testdata and vendor are ignored",
				files: map[string]string{"a/testdata/b_test.go": "package x\nimport _ \"github.com/stretchr/testify/require\"\n", "vendor/v/v.go": "package v\nimport _ \"github.com/stretchr/testify/require\"\n"},
			},
			{
				name:  "an invalid Go file is reported, not skipped",
				files: map[string]string{"a/a.go": "package"},
				want:  []Finding{{Path: "a/a.go", Rule: RuleUnparsed, Detail: "expected 'IDENT', found 'EOF'"}},
			},
		}, func(c scanCase) string { return c.name }, func(ctx *specs.Context, c scanCase) {
			ctx.Expect(scanFiles(ctx, c.files)).ToEqual(c.want)
		})
	})
}
