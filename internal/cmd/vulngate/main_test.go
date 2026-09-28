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

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- fixtures -------------------------------------------------------------

// reportStream builds a govulncheck -format json stream: one "config"
// message (every real report starts with one) followed by the given
// pre-built finding messages, mirroring the real, undocumented-but-observed
// shape of golang.org/x/vuln/cmd/govulncheck@v1.8.0's JSON output (verified
// against a real `govulncheck -format json` run against publisher/pulsar and
// publisher/nats in this repository; see odd/tasks/impact-ci-159.md).
func reportStream(findings ...string) string {
	var b strings.Builder
	b.WriteString(`{"config":{"protocol_version":"v1.0.0","scanner_name":"govulncheck","scanner_version":"v1.8.0","db":"https://vuln.go.dev","go_version":"go1.27.1","scan_level":"symbol","scan_mode":"source"}}` + "\n")
	b.WriteString(`{"progress":{"message":"Fetching vulnerabilities from the database..."}}` + "\n")
	for _, f := range findings {
		b.WriteString(f + "\n")
	}
	return b.String()
}

// calledFinding is a symbol-level finding: govulncheck's own JSON puts the
// vulnerable symbol as trace[0], and the calling code as the last frame, so
// trace[0].function is set exactly when the code actually calls in.
func calledFinding(osv, module string) string {
	return fmt.Sprintf(`{"finding":{"osv":%q,"trace":[{"module":%q,"version":"v1.0.0","package":%q,"function":"Do"},{"module":"example.com/caller","package":"example.com/caller","function":"main"}]}}`,
		osv, module, module)
}

// moduleOnlyFinding is the coarsest finding govulncheck emits for a
// vulnerability that is only required, never imported: trace has a single
// frame naming just the module and version, no package, no function.
func moduleOnlyFinding(osv, module string) string {
	return fmt.Sprintf(`{"finding":{"osv":%q,"trace":[{"module":%q,"version":"v1.0.0"}]}}`, osv, module)
}

// packageOnlyFinding is the middle finding govulncheck emits for a
// vulnerability whose package is imported but whose vulnerable symbols are
// never called: trace names the package but no function.
func packageOnlyFinding(osv, module string) string {
	return fmt.Sprintf(`{"finding":{"osv":%q,"trace":[{"module":%q,"version":"v1.0.0","package":%q}]}}`, osv, module, module)
}

func allowFileJSON(entries ...string) string {
	return "[" + strings.Join(entries, ",") + "]"
}

func entryJSON(module, id, vulnModule, reviewBy string) string {
	return fmt.Sprintf(`{"module":%q,"id":%q,"vulnerable_module":%q,"owner":"@owner","reason":"reason","exposure":"exposure","removal":"removal","review_by":%q}`,
		module, id, vulnModule, reviewBy)
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// --- parseReport ------------------------------------------------------------

func TestParseReport_Clean(t *testing.T) {
	blocking, err := parseReport(strings.NewReader(reportStream()))
	if err != nil {
		t.Fatalf("parseReport: %v", err)
	}
	if len(blocking) != 0 {
		t.Fatalf("blocking = %v, want empty", blocking)
	}
}

func TestParseReport_CalledFindingBlocks(t *testing.T) {
	stream := reportStream(calledFinding("GO-2026-0001", "example.com/vuln"))
	blocking, err := parseReport(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("parseReport: %v", err)
	}
	if got, want := blocking["GO-2026-0001"], "example.com/vuln"; got != want {
		t.Fatalf("blocking[GO-2026-0001] = %q, want %q", got, want)
	}
}

func TestParseReport_ModuleOnlyFindingDoesNotBlock(t *testing.T) {
	stream := reportStream(moduleOnlyFinding("GO-2026-0002", "example.com/vuln"))
	blocking, err := parseReport(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("parseReport: %v", err)
	}
	if len(blocking) != 0 {
		t.Fatalf("blocking = %v, want empty (module-only finding must not block)", blocking)
	}
}

func TestParseReport_PackageOnlyFindingDoesNotBlock(t *testing.T) {
	stream := reportStream(packageOnlyFinding("GO-2026-0003", "example.com/vuln"))
	blocking, err := parseReport(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("parseReport: %v", err)
	}
	if len(blocking) != 0 {
		t.Fatalf("blocking = %v, want empty (package-only finding must not block)", blocking)
	}
}

func TestParseReport_MalformedJSONFails(t *testing.T) {
	_, err := parseReport(strings.NewReader(`{"config": not json`))
	if err == nil {
		t.Fatal("parseReport: want error for malformed JSON, got nil")
	}
}

func TestParseReport_EmptyReportFails(t *testing.T) {
	_, err := parseReport(strings.NewReader(""))
	if err == nil {
		t.Fatal("parseReport: want error for an empty report, got nil")
	}
}

func TestParseReport_MissingConfigMessageFails(t *testing.T) {
	stream := calledFinding("GO-2026-0001", "example.com/vuln") + "\n"
	_, err := parseReport(strings.NewReader(stream))
	if err == nil {
		t.Fatal("parseReport: want error for a report with no config message, got nil")
	}
}

// --- loadAllowList ------------------------------------------------------------

func TestLoadAllowList_Valid(t *testing.T) {
	path := writeTempFile(t, "allow.json", allowFileJSON(
		entryJSON("publisher/pulsar", "GO-2026-5046", "github.com/hamba/avro/v2", "2026-12-28"),
	))
	entries, err := loadAllowList(path)
	if err != nil {
		t.Fatalf("loadAllowList: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].ID != "GO-2026-5046" {
		t.Fatalf("entries[0].ID = %q, want GO-2026-5046", entries[0].ID)
	}
}

func TestLoadAllowList_UnknownFieldFails(t *testing.T) {
	raw := `[{"module":"m","id":"GO-1","vulnerable_module":"v","owner":"@o","reason":"r","exposure":"e","removal":"rm","review_by":"2026-01-01","extra":"nope"}]`
	path := writeTempFile(t, "allow.json", raw)
	if _, err := loadAllowList(path); err == nil {
		t.Fatal("loadAllowList: want error for an unknown field, got nil")
	}
}

func TestLoadAllowList_MissingFieldFails(t *testing.T) {
	raw := `[{"module":"m","id":"GO-1","vulnerable_module":"v","owner":"@o","reason":"r","exposure":"e","review_by":"2026-01-01"}]`
	path := writeTempFile(t, "allow.json", raw)
	if _, err := loadAllowList(path); err == nil {
		t.Fatal("loadAllowList: want error for a missing required field (removal), got nil")
	}
}

func TestLoadAllowList_BadDateFails(t *testing.T) {
	raw := `[{"module":"m","id":"GO-1","vulnerable_module":"v","owner":"@o","reason":"r","exposure":"e","removal":"rm","review_by":"28-12-2026"}]`
	path := writeTempFile(t, "allow.json", raw)
	if _, err := loadAllowList(path); err == nil {
		t.Fatal("loadAllowList: want error for a malformed review_by date, got nil")
	}
}

// --- evaluate ------------------------------------------------------------

func TestEvaluate_ExceptedFinding(t *testing.T) {
	blocking := map[string]string{"GO-1": "example.com/vuln"}
	allow := []allowEntry{{Module: "mod", ID: "GO-1", VulnerableModule: "example.com/vuln", Owner: "@o", ReviewBy: "2026-12-28"}}
	res := evaluate("mod", blocking, allow, "2026-01-01")
	if len(res.Excepted) != 1 || len(res.Blocked) != 0 || len(res.Stale) != 0 || len(res.Expired) != 0 {
		t.Fatalf("res = %+v, want exactly one excepted finding", res)
	}
}

func TestEvaluate_NewIDBlocks(t *testing.T) {
	blocking := map[string]string{"GO-2": "example.com/vuln"}
	res := evaluate("mod", blocking, nil, "2026-01-01")
	if len(res.Blocked) != 1 {
		t.Fatalf("res.Blocked = %+v, want exactly one blocked finding for an unlisted ID", res.Blocked)
	}
}

func TestEvaluate_SameIDInAnotherModuleBlocks(t *testing.T) {
	blocking := map[string]string{"GO-3": "example.com/other-vuln"}
	allow := []allowEntry{{Module: "mod", ID: "GO-3", VulnerableModule: "example.com/vuln", Owner: "@o", ReviewBy: "2026-12-28"}}
	res := evaluate("mod", blocking, allow, "2026-01-01")
	if len(res.Blocked) != 1 {
		t.Fatalf("res.Blocked = %+v, want exactly one blocked finding when vulnerable_module does not match", res.Blocked)
	}
	if len(res.Stale) != 0 {
		t.Fatalf("res.Stale = %+v, want none: the ID is present, just under a different module", res.Stale)
	}
}

func TestEvaluate_StaleEntryFails(t *testing.T) {
	allow := []allowEntry{{Module: "mod", ID: "GO-4", VulnerableModule: "example.com/vuln", Owner: "@o", ReviewBy: "2026-12-28"}}
	res := evaluate("mod", map[string]string{}, allow, "2026-01-01")
	if len(res.Stale) != 1 {
		t.Fatalf("res.Stale = %+v, want exactly one stale entry", res.Stale)
	}
}

func TestEvaluate_EntryForAnotherModuleIsIgnored(t *testing.T) {
	allow := []allowEntry{{Module: "other-mod", ID: "GO-5", VulnerableModule: "example.com/vuln", Owner: "@o", ReviewBy: "2026-12-28"}}
	res := evaluate("mod", map[string]string{}, allow, "2026-01-01")
	if len(res.Stale) != 0 || len(res.Blocked) != 0 {
		t.Fatalf("res = %+v, want an entry scoped to another module to be entirely ignored", res)
	}
}

func TestEvaluate_ExpiredReviewDateFails(t *testing.T) {
	blocking := map[string]string{"GO-6": "example.com/vuln"}
	allow := []allowEntry{{Module: "mod", ID: "GO-6", VulnerableModule: "example.com/vuln", Owner: "@o", ReviewBy: "2026-12-28"}}
	res := evaluate("mod", blocking, allow, "2026-12-29")
	if len(res.Expired) != 1 {
		t.Fatalf("res.Expired = %+v, want exactly one expired exception", res.Expired)
	}
	if len(res.Excepted) != 0 {
		t.Fatalf("res.Excepted = %+v, want none: an expired entry must not also count as excepted", res.Excepted)
	}
}

func TestEvaluate_ReviewDateOnTheDayItselfStillPasses(t *testing.T) {
	blocking := map[string]string{"GO-7": "example.com/vuln"}
	allow := []allowEntry{{Module: "mod", ID: "GO-7", VulnerableModule: "example.com/vuln", Owner: "@o", ReviewBy: "2026-12-28"}}
	res := evaluate("mod", blocking, allow, "2026-12-28")
	if len(res.Excepted) != 1 || len(res.Expired) != 0 {
		t.Fatalf("res = %+v, want the review_by day itself to still be valid", res)
	}
}

// --- run (end-to-end wiring) ------------------------------------------------------------

func TestRun_ExceptedFindingSucceeds(t *testing.T) {
	reportPath := writeTempFile(t, "report.json", reportStream(calledFinding("GO-2026-5046", "github.com/hamba/avro/v2")))
	allowPath := writeTempFile(t, "allow.json", allowFileJSON(
		entryJSON("publisher/pulsar", "GO-2026-5046", "github.com/hamba/avro/v2", "2026-12-28"),
	))
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-module", "publisher/pulsar",
		"-report", reportPath,
		"-allow", allowPath,
		"-today", "2026-01-01",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Excepted") {
		t.Fatalf("stdout = %q, want a mention of the excepted finding", stdout.String())
	}
}

func TestRun_MissingExceptionEntryFails(t *testing.T) {
	reportPath := writeTempFile(t, "report.json", reportStream(calledFinding("GO-2026-5046", "github.com/hamba/avro/v2")))
	allowPath := writeTempFile(t, "allow.json", allowFileJSON())
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-module", "publisher/pulsar",
		"-report", reportPath,
		"-allow", allowPath,
		"-today", "2026-01-01",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run: want an error when a blocking finding has no exception entry")
	}
}

func TestRun_ExpiredEntryFails(t *testing.T) {
	reportPath := writeTempFile(t, "report.json", reportStream(calledFinding("GO-2026-5046", "github.com/hamba/avro/v2")))
	allowPath := writeTempFile(t, "allow.json", allowFileJSON(
		entryJSON("publisher/pulsar", "GO-2026-5046", "github.com/hamba/avro/v2", "2026-12-28"),
	))
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-module", "publisher/pulsar",
		"-report", reportPath,
		"-allow", allowPath,
		"-today", "2026-12-29",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run: want an error for a review date that has passed")
	}
}

func TestRun_StaleEntryFails(t *testing.T) {
	reportPath := writeTempFile(t, "report.json", reportStream())
	allowPath := writeTempFile(t, "allow.json", allowFileJSON(
		entryJSON("publisher/pulsar", "GO-2026-5046", "github.com/hamba/avro/v2", "2026-12-28"),
	))
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-module", "publisher/pulsar",
		"-report", reportPath,
		"-allow", allowPath,
		"-today", "2026-01-01",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run: want an error for a stale exception entry")
	}
}

func TestRun_CleanModuleWithNoEntriesSucceeds(t *testing.T) {
	reportPath := writeTempFile(t, "report.json", reportStream(moduleOnlyFinding("GO-2026-9999", "golang.org/x/crypto")))
	allowPath := writeTempFile(t, "allow.json", allowFileJSON(
		entryJSON("publisher/pulsar", "GO-2026-5046", "github.com/hamba/avro/v2", "2026-12-28"),
	))
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-module", "publisher/nats",
		"-report", reportPath,
		"-allow", allowPath,
		"-today", "2026-01-01",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, stderr.String())
	}
}

func TestRun_MissingFlagsFail(t *testing.T) {
	cases := [][]string{
		{"-report", "r", "-allow", "a"},
		{"-module", "m", "-allow", "a"},
		{"-module", "m", "-report", "r"},
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		if err := run(args, &stdout, &stderr); err == nil {
			t.Fatalf("run(%v): want an error for missing required flags", args)
		}
	}
}

func TestRun_BadTodayFlagFails(t *testing.T) {
	reportPath := writeTempFile(t, "report.json", reportStream())
	allowPath := writeTempFile(t, "allow.json", allowFileJSON())
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-module", "m",
		"-report", reportPath,
		"-allow", allowPath,
		"-today", "not-a-date",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run: want an error for a malformed -today value")
	}
}

func TestRun_DefaultTodayResolves(t *testing.T) {
	reportPath := writeTempFile(t, "report.json", reportStream(calledFinding("GO-2026-1", "example.com/vuln")))
	allowPath := writeTempFile(t, "allow.json", allowFileJSON(
		entryJSON("m", "GO-2026-1", "example.com/vuln", "9999-12-31"),
	))
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-module", "m",
		"-report", reportPath,
		"-allow", allowPath,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run with no -today: %v (stderr: %s)", err, stderr.String())
	}
}
