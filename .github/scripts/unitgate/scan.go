// Command unitgate enforces the unit-test rules of epic #201 / #204 on every Go file of the repository.
// This file is the scanner; allowlist.go holds the temporary and justified exceptions.
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	testifyPrefix = "github.com/stretchr/testify"
	mocksPrefix   = "github.com/getsyntegrity/ego/mocks"
	specsPath     = "github.com/getsyntegrity/go-specs/specs"
)

// Rule names one thing the gate forbids.
type Rule string

const (
	RuleTestify  Rule = "testify"         // import of github.com/stretchr/testify/...
	RuleMocks    Rule = "generated-mocks" // import of the generated mocks packages from outside mocks/
	RuleNoSpecs  Rule = "no-specs"        // *_test.go with a Test function and no specs.Describe
	RuleResource Rule = "resource"        // a test reaching a real resource (see resources.go)
	RuleSkip     Rule = "no-skip"         // a Skip, SkipIt, PendingIt or FIt call, or testing.Short, under inttest/ (see skips.go); never allowlisted
	RuleCluster  Rule = "cluster-name"    // a test that starts a cluster (goakt.WithCluster, dynaport) and is not named TestCluster* (see cluster.go); never allowlisted
	RuleUnparsed Rule = "unparsed"        // the file is not valid Go
)

// Finding is one violation in one file.
type Finding struct {
	Path   string
	Rule   Rule
	Detail string
}

// Scan walks fsys and returns every violation, sorted by path then rule. It reads every .go file of every
// Go module under the root; .git, vendor and testdata directories are skipped.
func Scan(fsys fs.FS) ([]Finding, error) {
	var out []Finding
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		src, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		out = append(out, scanFile(p, src)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Rule < out[j].Rule
	})
	return out, nil
}

func scanFile(p string, src []byte) []Finding {
	f, err := parser.ParseFile(token.NewFileSet(), p, src, parser.SkipObjectResolution)
	if err != nil {
		return []Finding{{Path: p, Rule: RuleUnparsed, Detail: firstLine(err.Error(), p)}}
	}
	imports := importNames(f)
	isTest := strings.HasSuffix(p, "_test.go")

	var out []Finding
	add := func(r Rule, detail string) { out = append(out, Finding{Path: p, Rule: r, Detail: detail}) }

	for _, ip := range sortedImportPaths(imports) {
		switch {
		case hasPathPrefix(ip, testifyPrefix):
			add(RuleTestify, "imports "+ip)
		case hasPathPrefix(ip, mocksPrefix) && !insideMocks(p):
			add(RuleMocks, "imports "+ip)
		}
	}
	out = append(out, skipFindings(p, f, imports)...)
	if isTest {
		if name := firstTestWithoutSpecs(f, imports); name != "" {
			add(RuleNoSpecs, "declares "+name+" without specs.Describe")
		}
		out = append(out, resourceFindings(p, f, imports)...)
		out = append(out, clusterFindings(p, f, imports)...)
	}
	return out
}

// importNames maps the local name each import is known by to its path.
func importNames(f *ast.File) map[string]string {
	m := map[string]string{}
	for _, spec := range f.Imports {
		ip, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := path.Base(ip)
		if spec.Name != nil {
			name = spec.Name.Name
		} else if isVersionSuffix(name) {
			name = path.Base(path.Dir(ip))
		}
		m[name] = ip
	}
	return m
}

func isVersionSuffix(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	_, err := strconv.Atoi(s[1:])
	return err == nil
}

func sortedImportPaths(m map[string]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, ip := range m {
		if !seen[ip] {
			seen[ip] = true
			out = append(out, ip)
		}
	}
	sort.Strings(out)
	return out
}

func hasPathPrefix(importPath, prefix string) bool {
	return importPath == prefix || strings.HasPrefix(importPath, prefix+"/")
}

func insideMocks(p string) bool { return strings.HasPrefix(p, "mocks/") }

// firstTestWithoutSpecs returns the first Test function of the file when the file never calls specs.Describe.
func firstTestWithoutSpecs(f *ast.File, imports map[string]string) string {
	first := ""
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && isTestFunc(fn) {
			first = fn.Name.Name
			break
		}
	}
	if first == "" || callsDescribe(f, imports) {
		return ""
	}
	return first
}

// isTestFunc reports whether fn is a Go test: func TestXxx(t *testing.T), Xxx not starting lower case.
func isTestFunc(fn *ast.FuncDecl) bool {
	name := fn.Name.Name
	if fn.Recv != nil || !strings.HasPrefix(name, "Test") {
		return false
	}
	if rest := name[len("Test"):]; rest != "" {
		if r, _ := utf8.DecodeRuneInString(rest); unicode.IsLower(r) {
			return false
		}
	}
	params := fn.Type.Params.List
	if len(params) != 1 {
		return false
	}
	star, ok := params[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "T" && identName(sel.X) == "testing"
}

func callsDescribe(f *ast.File, imports map[string]string) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && qualifiedCall(call, imports) == specsPath+".Describe" {
			found = true
		}
		return !found
	})
	return found
}

// qualifiedCall returns "<import path>.<Func>" for a call of the form pkg.Func(...), or "".
func qualifiedCall(call *ast.CallExpr, imports map[string]string) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	ip, ok := imports[identName(sel.X)]
	if !ok {
		return ""
	}
	return ip + "." + sel.Sel.Name
}

func identName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// firstLine trims the "path:line:col: " prefix go/parser puts on its errors.
func firstLine(msg, p string) string {
	msg = strings.TrimPrefix(msg, p+":")
	if i := strings.Index(msg, ": "); i >= 0 {
		msg = msg[i+2:]
	}
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return msg
}
