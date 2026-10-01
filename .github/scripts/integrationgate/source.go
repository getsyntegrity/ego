package main

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// integrationTag is the build tag the lane runs with and the orphan check looks for.
const integrationTag = "integration"

// skippedDirs are never scanned for orphans: vendored code, fixtures, VCS data and other worktrees.
var skippedDirs = map[string]bool{".git": true, ".claude": true, "vendor": true, "testdata": true, "node_modules": true}

// ModulePaths reads the module path of every module the suites mention. A module whose go.mod is missing or has
// no module line is reported and left out of the map.
func ModulePaths(fsys fs.FS, suites []Suite) (map[string]string, []string) {
	paths := map[string]string{}
	var problems []string
	tried := map[string]bool{}
	for _, s := range suites {
		if tried[s.Module] {
			continue
		}
		tried[s.Module] = true
		goMod := path.Join(s.Module, "go.mod")
		data, err := fs.ReadFile(fsys, goMod)
		if err != nil {
			problems = append(problems, fmt.Sprintf("module %s: cannot read %s: %v", s.Module, goMod, err))
			continue
		}
		module := moduleLine(string(data))
		if module == "" {
			problems = append(problems, fmt.Sprintf("module %s: %s has no module line", s.Module, goMod))
			continue
		}
		paths[s.Module] = module
	}
	return paths, problems
}

func moduleLine(goMod string) string {
	for _, line := range strings.Split(goMod, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return strings.Trim(fields[1], `"`)
		}
	}
	return ""
}

// testFile is what the source scan keeps of one _test.go file.
type testFile struct {
	tests      []string // top-level test functions
	integrated bool     // built when the integration tag is set
	err        string   // why the file could not be parsed
}

// sourceScan parses each test file once and remembers whether its parse error was already reported.
type sourceScan struct {
	fsys     fs.FS
	files    map[string]*testFile
	reported map[string]bool
	problems []string
}

func (sc *sourceScan) file(p string) *testFile {
	if f, ok := sc.files[p]; ok {
		return f
	}
	f := &testFile{}
	sc.files[p] = f
	src, err := fs.ReadFile(sc.fsys, p)
	if err != nil {
		f.err = err.Error()
	} else if parsed, err := parser.ParseFile(token.NewFileSet(), p, src, parser.ParseComments|parser.SkipObjectResolution); err != nil {
		f.err = firstLine(err.Error(), p)
	} else {
		f.tests = topLevelTests(parsed)
		f.integrated = builtWithIntegration(parsed)
	}
	if f.err != "" && !sc.reported[p] {
		sc.reported[p] = true
		sc.problems = append(sc.problems, fmt.Sprintf("%s: cannot parse: %s", p, f.err))
	}
	return f
}

// CheckSource finds stale entries (a listed test that is no longer declared) and orphans (a top-level test in a
// file built with the integration tag that the manifest does not list). Both are static: nothing is compiled.
func CheckSource(fsys fs.FS, suites []Suite) []string {
	sc := &sourceScan{fsys: fsys, files: map[string]*testFile{}, reported: map[string]bool{}}
	listed := map[string]bool{}
	var stale []string

	declared := map[string]map[string]bool{} // package directory -> declared tests
	for _, s := range suites {
		listed[s.PkgDir()+"|"+s.Test] = true
		dir := s.PkgDir()
		if declared[dir] == nil {
			declared[dir] = map[string]bool{}
			entries, _ := fs.ReadDir(fsys, dir)
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), "_test.go") {
					for _, name := range sc.file(path.Join(dir, e.Name())).tests {
						declared[dir][name] = true
					}
				}
			}
		}
		if !declared[dir][s.Test] {
			stale = append(stale, fmt.Sprintf("%s: stale entry (manifest line %d): no func %s in the _test.go files of %s", s, s.Line, s.Test, dir))
		}
	}

	var orphans []string
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case d.IsDir() && p != "." && skippedDirs[d.Name()]:
			return fs.SkipDir
		case d.IsDir() || !strings.HasSuffix(p, "_test.go"):
			return nil
		}
		f := sc.file(p)
		if !f.integrated {
			return nil
		}
		for _, name := range f.tests {
			if !listed[path.Dir(p)+"|"+name] {
				orphans = append(orphans, fmt.Sprintf("%s: %s is built with the %s tag but is not in the manifest", p, name, integrationTag))
			}
		}
		return nil
	})

	out := append(stale, orphans...)
	sort.SliceStable(sc.problems, func(i, j int) bool { return sc.problems[i] < sc.problems[j] })
	return append(out, sc.problems...)
}

// topLevelTests returns the names of the func TestXxx(t *testing.T) declarations of f.
func topLevelTests(f *ast.File) []string {
	var out []string
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && isTestFunc(fn) {
			out = append(out, fn.Name.Name)
		}
	}
	return out
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
	if !ok || sel.Sel.Name != "T" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "testing"
}

// builtWithIntegration reports whether the //go:build line of f mentions the integration tag and can be
// satisfied with it. The other tags (operating system, architecture) are tried both off and on, so
// `integration && !windows` counts while `!integration` and a constraint without the tag do not.
func builtWithIntegration(f *ast.File) bool {
	for _, group := range f.Comments {
		if group.End() >= f.Package {
			break
		}
		for _, c := range group.List {
			if !constraint.IsGoBuild(c.Text) {
				continue
			}
			expr, err := constraint.Parse(c.Text)
			if err != nil || !mentionsTag(expr, integrationTag) {
				continue
			}
			othersOff := expr.Eval(func(tag string) bool { return tag == integrationTag })
			othersOn := expr.Eval(func(tag string) bool { return true })
			return othersOff || othersOn
		}
	}
	return false
}

func mentionsTag(e constraint.Expr, tag string) bool {
	switch x := e.(type) {
	case *constraint.TagExpr:
		return x.Tag == tag
	case *constraint.NotExpr:
		return mentionsTag(x.X, tag)
	case *constraint.AndExpr:
		return mentionsTag(x.X, tag) || mentionsTag(x.Y, tag)
	case *constraint.OrExpr:
		return mentionsTag(x.X, tag) || mentionsTag(x.Y, tag)
	}
	return false
}

// firstLine trims the "path:line:col: " prefix go/parser puts on its errors and keeps the first line.
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
