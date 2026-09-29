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

package inventory

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	externalEndpointName = regexp.MustCompile(`DSN|URL|ENDPOINT|BROKER|ADDR`)
	majorVersion         = regexp.MustCompile(`^v\d+$`)
)

// ScanModule reads every Test function of the module and records its signals.
func ScanModule(m Module) (Scan, error) {
	dirs, err := testDirs(m.Dir)
	if err != nil {
		return Scan{}, err
	}
	var tests []Test
	for _, dir := range dirs {
		found, err := scanDir(m, dir)
		if err != nil {
			return Scan{}, err
		}
		tests = append(tests, found...)
	}
	sort.SliceStable(tests, func(i, j int) bool {
		if tests[i].Package != tests[j].Package {
			return tests[i].Package < tests[j].Package
		}
		if tests[i].Name != tests[j].Name {
			return tests[i].Name < tests[j].Name
		}
		return tests[i].File < tests[j].File
	})
	return Scan{Tests: tests}, nil
}

// testDirs returns the directories under root that hold _test.go files, without
// descending into testdata, vendor, hidden directories or nested modules.
func testDirs(root string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if p != root && skipDir(p, d.Name()) {
			return filepath.SkipDir
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), "_test.go") {
				dirs = append(dirs, p)
				break
			}
		}
		return nil
	})
	return dirs, err
}

func skipDir(p, name string) bool {
	if name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}
	_, err := os.Stat(filepath.Join(p, "go.mod"))
	return err == nil
}

// parsedFile is a test file with the import names that let a selector be resolved
// to an import path.
type parsedFile struct {
	rel     string
	ast     *ast.File
	imports map[string]string
}

type pkgFuncs struct {
	funcs  map[string]*funcInfo
	consts map[string]ast.Expr
}

type funcInfo struct {
	decl *ast.FuncDecl
	file *parsedFile
}

func scanDir(m Module, dir string) ([]Test, error) {
	fset := token.NewFileSet()
	matches, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	relDir, err := filepath.Rel(m.Dir, dir)
	if err != nil {
		return nil, err
	}
	relDir = filepath.ToSlash(relDir)
	pkgPath := m.Path
	if relDir != "." {
		pkgPath = m.Path + "/" + relDir
	}

	byPkgName := map[string]*pkgFuncs{}
	var files []*parsedFile
	for _, name := range matches {
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		pf := &parsedFile{
			rel:     path.Join(m.Rel, relDir, filepath.Base(name)),
			ast:     f,
			imports: importNames(f),
		}
		files = append(files, pf)
		pkg := byPkgName[f.Name.Name]
		if pkg == nil {
			pkg = &pkgFuncs{funcs: map[string]*funcInfo{}, consts: map[string]ast.Expr{}}
			byPkgName[f.Name.Name] = pkg
		}
		collectDecls(pkg, pf)
	}

	var tests []Test
	for _, pf := range files {
		pkg := byPkgName[pf.ast.Name.Name]
		for _, decl := range pf.ast.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !isTestFunc(fn, pf) {
				continue
			}
			sc := newBodyScan(pkg)
			sc.scan(fn, pf, "")
			for _, name := range sc.helperNames() {
				h := pkg.funcs[name]
				sc.scan(h.decl, h.file, name)
			}
			t := Test{
				Module:  m.Path,
				Package: pkgPath,
				Name:    fn.Name.Name,
				File:    pf.rel,
				Line:    fset.Position(fn.Pos()).Line,
			}
			sc.finish(&t)
			tests = append(tests, t)
		}
	}
	return tests, nil
}

func importNames(f *ast.File) map[string]string {
	names := map[string]string{}
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := importDefaultName(p)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == "_" || name == "." {
			continue
		}
		names[name] = p
	}
	return names
}

// importDefaultName guesses the package name from the last path element, skipping a
// major-version suffix such as /v5.
func importDefaultName(p string) string {
	parts := strings.Split(p, "/")
	last := parts[len(parts)-1]
	if len(parts) > 1 && majorVersion.MatchString(last) {
		last = parts[len(parts)-2]
	}
	last = strings.TrimSuffix(strings.TrimSuffix(last, "-go"), ".go")
	return strings.NewReplacer("-", "", ".", "").Replace(last)
}

func collectDecls(pkg *pkgFuncs, pf *parsedFile) {
	for _, decl := range pf.ast.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Body != nil {
				pkg.funcs[d.Name.Name] = &funcInfo{decl: d, file: pf}
			}
		case *ast.GenDecl:
			if d.Tok != token.CONST {
				continue
			}
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != len(vs.Values) {
					continue
				}
				for i, n := range vs.Names {
					pkg.consts[n.Name] = vs.Values[i]
				}
			}
		}
	}
}

// isTestFunc mirrors `go test`: the name is Test followed by a non-lowercase rune and
// the only parameter is *testing.T.
func isTestFunc(fn *ast.FuncDecl, pf *parsedFile) bool {
	name := fn.Name.Name
	if fn.Recv != nil || !strings.HasPrefix(name, "Test") || name == "TestMain" {
		return false
	}
	if rest := name[len("Test"):]; rest != "" {
		r, _ := utf8.DecodeRuneInString(rest)
		if unicode.IsLower(r) {
			return false
		}
	}
	params := fn.Type.Params.List
	if len(params) != 1 || len(params[0].Names) > 1 {
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
	id, ok := sel.X.(*ast.Ident)
	return ok && pf.imports[id.Name] == "testing"
}

var units = map[string]time.Duration{
	"Nanosecond": time.Nanosecond, "Microsecond": time.Microsecond, "Millisecond": time.Millisecond,
	"Second": time.Second, "Minute": time.Minute, "Hour": time.Hour,
}

// evalDuration evaluates a constant duration expression: integer literals, the time
// units, package-level constants and arithmetic over them.
func evalDuration(e ast.Expr, pf *parsedFile, consts map[string]ast.Expr, depth int) (int64, bool) {
	if depth > 8 {
		return 0, false
	}
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.INT {
			return 0, false
		}
		n, err := strconv.ParseInt(strings.ReplaceAll(v.Value, "_", ""), 0, 64)
		return n, err == nil
	case *ast.ParenExpr:
		return evalDuration(v.X, pf, consts, depth+1)
	case *ast.SelectorExpr:
		if id, ok := v.X.(*ast.Ident); ok && pf.imports[id.Name] == "time" {
			d, ok := units[v.Sel.Name]
			return int64(d), ok
		}
	case *ast.Ident:
		if c, ok := consts[v.Name]; ok {
			return evalDuration(c, pf, consts, depth+1)
		}
	case *ast.CallExpr:
		if len(v.Args) == 1 && isDurationConversion(v.Fun, pf) {
			return evalDuration(v.Args[0], pf, consts, depth+1)
		}
	case *ast.BinaryExpr:
		x, okx := evalDuration(v.X, pf, consts, depth+1)
		y, oky := evalDuration(v.Y, pf, consts, depth+1)
		if !okx || !oky {
			return 0, false
		}
		switch v.Op {
		case token.MUL:
			return x * y, true
		case token.ADD:
			return x + y, true
		case token.SUB:
			return x - y, true
		case token.QUO:
			if y != 0 {
				return x / y, true
			}
		}
	}
	return 0, false
}

func isDurationConversion(fun ast.Expr, pf *parsedFile) bool {
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		id, ok := f.X.(*ast.Ident)
		return ok && pf.imports[id.Name] == "time" && f.Sel.Name == "Duration"
	case *ast.Ident:
		return f.Name == "int64" || f.Name == "int"
	}
	return false
}
