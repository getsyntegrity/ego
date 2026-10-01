package main

import (
	"go/ast"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

const goaktActorPrefix = "github.com/tochemey/goakt/"

// resourceFindings reports the calls in a test file that reach something outside the process. It is a static,
// per-file check: it names the call, it does not prove the call runs. A file that is legitimately outside the
// unit lane goes in the resources allowlist with a reason.
func resourceFindings(p string, f *ast.File, imports map[string]string) []Finding {
	hasTempDir := false
	details := map[string]bool{}

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "TempDir" {
			hasTempDir = true
		}
		if d := resourceCall(call, imports); d != "" {
			details[d] = true
		}
		return true
	})

	var out []Finding
	for d := range details {
		if d == osCreateDetail && hasTempDir {
			continue
		}
		out = append(out, Finding{Path: p, Rule: RuleResource, Detail: d})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Detail < out[j].Detail })
	return out
}

const osCreateDetail = "calls os.Create outside t.TempDir"

// resourceCall returns a description when call reaches a real resource, or "".
func resourceCall(call *ast.CallExpr, imports map[string]string) string {
	q := qualifiedCall(call, imports)
	if q == "" {
		return ""
	}
	i := strings.LastIndexByte(q, '.')
	pkg, fn := q[:i], q[i+1:]

	switch {
	case pkg == "database/sql" && fn == "Open":
		return "calls " + q
	case pkg == "net" && (strings.HasPrefix(fn, "Dial") || strings.HasPrefix(fn, "Listen")):
		return "calls " + q
	case pkg == "os/exec" && strings.HasPrefix(fn, "Command"):
		return "calls " + q
	case pkg == "net/http/httptest" && strings.HasPrefix(fn, "New") && strings.HasSuffix(fn, "Server"),
		pkg == "net/http/httptest" && fn == "NewUnstartedServer":
		return "calls " + q
	case strings.HasPrefix(pkg, goaktActorPrefix) && fn == "NewActorSystem":
		return "calls goakt actor." + fn
	case pkg == "os" && fn == "Create":
		return osCreateDetail
	case pkg == "os" && (fn == "Getenv" || fn == "LookupEnv"):
		if name := firstStringArg(call); strings.Contains(strings.ToUpper(name), "DSN") {
			return "reads the DSN variable " + name
		}
	}
	return ""
}

func firstStringArg(call *ast.CallExpr) string {
	if len(call.Args) == 0 {
		return ""
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return s
}
