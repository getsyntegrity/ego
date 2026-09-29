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
	"go/ast"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"time"
)

// bodyScan accumulates the signals of one Test function and the local helpers it
// calls. Helpers are scanned one level deep: a helper's own helpers are not followed.
type bodyScan struct {
	pkg        *pkgFuncs
	self       string
	signals    map[Signal]bool
	waitNS     int64
	unresolved int
	envs       []Signal
	skips      bool
	called     map[string]bool
}

func newBodyScan(pkg *pkgFuncs) *bodyScan {
	return &bodyScan{pkg: pkg, signals: map[Signal]bool{}, called: map[string]bool{}}
}

// scan walks fn. via is empty for the Test function and the helper name otherwise.
func (b *bodyScan) scan(fn *ast.FuncDecl, pf *parsedFile, via string) {
	if via == "" {
		b.self = fn.Name.Name
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch f := call.Fun.(type) {
		case *ast.SelectorExpr:
			if id, ok := f.X.(*ast.Ident); ok {
				if p, isImport := pf.imports[id.Name]; isImport {
					b.importCall(p, f.Sel.Name, call, pf, via)
					return true
				}
			}
			b.methodCall(f.Sel.Name, via)
		case *ast.Ident:
			if via == "" && f.Name != b.self && !strings.HasPrefix(f.Name, "Test") {
				if _, ok := b.pkg.funcs[f.Name]; ok {
					b.called[f.Name] = true
				}
			}
		}
		return true
	})
}

func (b *bodyScan) helperNames() []string {
	names := make([]string, 0, len(b.called))
	for n := range b.called {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (b *bodyScan) add(id, via, detail string) {
	b.signals[Signal{ID: id, Via: via, Detail: detail}] = true
}

func (b *bodyScan) methodCall(name, via string) {
	switch name {
	case "TempDir":
		b.add(SigFSTempDir, via, "")
	case "Parallel":
		b.add(SigParallel, via, "")
	case "Skip", "Skipf", "SkipNow":
		b.skips = true
	}
}

func hasPrefixAny(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func isOneOf(s string, set ...string) bool {
	for _, v := range set {
		if s == v {
			return true
		}
	}
	return false
}

func constructorLike(sel string) bool {
	return hasPrefixAny(sel, "New", "Connect", "Dial", "Open")
}

// importCall records the signals of a call to pkgPath.sel.
func (b *bodyScan) importCall(pkgPath, sel string, call *ast.CallExpr, pf *parsedFile, via string) {
	switch {
	case pkgPath == "net" && strings.HasPrefix(sel, "Listen"):
		b.add(SigNetListen, via, "")
	case pkgPath == "net" && strings.HasPrefix(sel, "Dial"):
		b.add(SigNetDial, via, "")
	case pkgPath == "net/http" && strings.HasPrefix(sel, "ListenAndServe"):
		b.add(SigNetListen, via, "")
	case pkgPath == "net/http/httptest" && isOneOf(sel, "NewServer", "NewTLSServer", "NewUnstartedServer"):
		b.add(SigHTTPTestServer, via, "")
	case pkgPath == "os/exec" && isOneOf(sel, "Command", "CommandContext"):
		b.execCall(call, via)
	case pkgPath == "database/sql" && isOneOf(sel, "Open", "OpenDB"):
		b.add(SigDBSQL, via, "")
	case hasPrefixAny(pkgPath, "github.com/jackc/pgx", "github.com/lib/pq"):
		b.add(SigDBPostgres, via, "")
	case strings.HasPrefix(pkgPath, "github.com/tochemey/goakt"):
		b.goaktCall(pkgPath, sel, via)
	case strings.HasPrefix(pkgPath, "github.com/travisjeffery/go-dynaport") && sel == "Get":
		b.add(SigPortAlloc, via, "")
	case brokerSignal(pkgPath) != "" && constructorLike(sel):
		b.add(brokerSignal(pkgPath), via, "")
	case isOneOf(pkgPath, "go/parser", "go/build", "go/types") || strings.HasPrefix(pkgPath, "golang.org/x/tools/go/"):
		b.add(SigSourceInspect, via, "")
	case pkgPath == "os":
		b.osCall(sel, call, via)
	case isOneOf(pkgPath, "path/filepath", "io/fs") && isOneOf(sel, "Walk", "WalkDir", "Glob"):
		b.add(SigFSIO, via, "")
	case pkgPath == "time" && sel == "Sleep":
		b.wait(SigWaitSleep, call, pf, via)
	case strings.HasSuffix(pkgPath, "/internal/pause") && sel == "For":
		b.wait(SigWaitPause, call, pf, via)
	}
}

func brokerSignal(pkgPath string) string {
	switch {
	case strings.Contains(pkgPath, "kafka") || strings.Contains(pkgPath, "sarama") || strings.Contains(pkgPath, "franz-go"):
		return SigBrokerKafka
	case strings.HasPrefix(pkgPath, "github.com/nats-io/"):
		return SigBrokerNATS
	case strings.Contains(pkgPath, "pulsar"):
		return SigBrokerPulsar
	}
	return ""
}

func (b *bodyScan) goaktCall(pkgPath, sel, via string) {
	switch {
	case sel == "NewActorSystem", strings.HasSuffix(pkgPath, "/testkit") && sel == "New":
		b.add(SigActorSystem, via, "")
	case isOneOf(sel, "NewClusterConfig", "WithCluster"):
		b.add(SigCluster, via, "")
	}
}

func (b *bodyScan) osCall(sel string, call *ast.CallExpr, via string) {
	switch sel {
	case "MkdirTemp", "CreateTemp":
		b.add(SigFSTempDir, via, "")
	case "ReadFile", "WriteFile", "Create", "Open", "OpenFile", "Mkdir", "MkdirAll", "ReadDir", "Remove", "RemoveAll", "Stat", "Rename":
		b.add(SigFSIO, via, "")
	case "Getenv", "LookupEnv":
		if len(call.Args) > 0 {
			if name, ok := stringLit(call.Args[0]); ok {
				b.envs = append(b.envs, Signal{ID: SigSkipEnv, Via: via, Detail: name})
			}
		}
	}
}

var goToolchainWords = []string{"go", "list", "build", "vet", "env", "test"}

func (b *bodyScan) execCall(call *ast.CallExpr, via string) {
	var lits []string
	for _, a := range call.Args {
		if s, ok := stringLit(a); ok {
			lits = append(lits, s)
		}
	}
	detail := strings.Join(lits[:min(len(lits), 2)], " ")
	for _, l := range lits {
		if isOneOf(l, goToolchainWords...) {
			b.add(SigGoToolchain, via, detail)
			return
		}
	}
	b.add(SigProcessExec, via, detail)
}

func (b *bodyScan) wait(id string, call *ast.CallExpr, pf *parsedFile, via string) {
	b.add(id, via, "")
	if len(call.Args) == 1 {
		if ns, ok := evalDuration(call.Args[0], pf, b.pkg.consts, 0); ok {
			b.waitNS += ns
			return
		}
	}
	b.unresolved++
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// finish writes the accumulated signals into t. An environment read only becomes
// a skip signal when the test or one of its helpers can also skip.
func (b *bodyScan) finish(t *Test) {
	if b.skips {
		for _, e := range b.envs {
			b.signals[e] = true
			if externalEndpointName.MatchString(e.Detail) {
				b.signals[Signal{ID: SigExternalEndpoint, Via: e.Via, Detail: e.Detail}] = true
			}
		}
	}
	for s := range b.signals {
		t.Signals = append(t.Signals, s)
	}
	sort.Slice(t.Signals, func(i, j int) bool {
		a, c := t.Signals[i], t.Signals[j]
		if a.ID != c.ID {
			return a.ID < c.ID
		}
		if a.Via != c.Via {
			return a.Via < c.Via
		}
		return a.Detail < c.Detail
	})
	t.FixedWaitMS = time.Duration(b.waitNS).Milliseconds()
	t.UnresolvedWaits = b.unresolved
}
