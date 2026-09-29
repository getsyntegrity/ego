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
	"strings"
	"testing"
)

// realSumdb404 is the verbatim failure from release.yml run 36474920456,
// attempt 1 (job 109106620419): sum.golang.org answered 404 for a tag the
// proxy already served. The text carries "invalid version: unknown
// revision", which must NOT make it permanent: the HTTP status wins.
const realSumdb404 = `github.com/getsyntegrity/ego@v4.0.0: verifying module: github.com/getsyntegrity/ego@v4.0.0: reading https://sum.golang.org/lookup/github.com/getsyntegrity/ego@v4.0.0: 404 Not Found
	server response: not found: github.com/getsyntegrity/ego@v4.0.0: invalid version: unknown revision v4.0.0`

func TestClassify(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   Class
		reason string // substring expected in Result.Reason
	}{
		{"real sumdb 404 with unknown revision inside", realSumdb404, Transient, "HTTP 404"},
		{"proxy 404", "github.com/x/v4@v4.0.0: reading https://proxy.golang.org/github.com/x/v4/@v/v4.0.0.info: 404 Not Found", Transient, "HTTP 404"},
		{"sumdb 404 only", "reading https://sum.golang.org/lookup/github.com/x/v4@v4.0.0: 404 Not Found", Transient, "HTTP 404"},
		{"500", "reading https://proxy.golang.org/x: 500 Internal Server Error", Transient, "HTTP 500"},
		{"502", "reading https://sum.golang.org/lookup/x: 502 Bad Gateway", Transient, "HTTP 502"},
		{"503", "reading https://proxy.golang.org/x: 503 Service Unavailable", Transient, "HTTP 503"},
		{"504", "reading https://proxy.golang.org/x: 504 Gateway Timeout", Transient, "HTTP 504"},
		{"429", "reading https://sum.golang.org/lookup/x: 429 Too Many Requests", Transient, "HTTP 429"},
		{"dial", "dial tcp 142.250.1.1:443: connect: connection refused", Transient, "network"},
		{"i/o timeout", "Get \"https://proxy.golang.org/x\": dial tcp 1.2.3.4:443: i/o timeout", Transient, "network"},
		{"connection reset", "read tcp 10.0.0.1:1234->1.2.3.4:443: read: connection reset by peer", Transient, "network"},
		{"tls handshake timeout", "net/http: TLS handshake timeout", Transient, "network"},
		{"no such host", "dial tcp: lookup proxy.golang.org: no such host", Transient, "network"},
		{"unexpected EOF", "Get \"https://proxy.golang.org/x\": unexpected EOF", Transient, "network"},
		{"unknown revision without any HTTP status", "github.com/x/v4@v4.0.0: invalid version: unknown revision v4.0.0", Transient, "unknown revision"},

		{"410 gone", "reading https://proxy.golang.org/x: 410 Gone", Permanent, "HTTP 410"},
		{"410 beats 404 text", "reading https://proxy.golang.org/x: 404 Not Found\nreading https://sum.golang.org/lookup/x: 410 Gone", Permanent, "HTTP 410"},
		{"checksum mismatch", "verifying module: checksum mismatch\n\tdownloaded: h1:aaa\n\tsum.golang.org: h1:bbb\n\nSECURITY ERROR\nThis download does NOT match the one reported by the checksum server.", Permanent, "checksum"},
		{"security error alone", "SECURITY ERROR", Permanent, "checksum"},
		{"checksum mismatch beats a 404 line", "reading https://x: 404 Not Found\nchecksum mismatch", Permanent, "checksum"},
		{"declared path mismatch", "go: github.com/x/v4@v4.0.0: parsing go.mod:\n\tmodule declares its path as: github.com/y/v4\n\t        but was required as: github.com/x/v4", Permanent, "path"},
		{"but was required as", "but was required as: github.com/x/v4", Permanent, "path"},
		{"malformed version", "go: github.com/x/v4@v4..0: invalid version: malformed version: v4..0", Permanent, "invalid"},
		{"major suffix mismatch", "github.com/x@v4.0.0: invalid version: module contains a go.mod file, so module path must match major version (\"github.com/x/v4\")", Permanent, "invalid"},
		{"should be v4", "github.com/x@v4.0.0: invalid version: should be v0 or v1, not v4", Permanent, "invalid"},
		{"malformed module path", "malformed module path \"x\": missing dot in first path element", Permanent, "malformed"},

		{"unrecognized output", "something entirely unexpected happened", Unclassified, "unrecognized"},
		{"empty output", "", Unclassified, "unrecognized"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.output)
			if got.Class != tt.want {
				t.Fatalf("Classify() class = %v, want %v (reason %q)", got.Class, tt.want, got.Reason)
			}
			if !strings.Contains(got.Reason, tt.reason) {
				t.Errorf("Reason = %q, want it to contain %q", got.Reason, tt.reason)
			}
		})
	}
}

func TestClassify_LastLine(t *testing.T) {
	got := Classify(realSumdb404)
	want := "server response: not found: github.com/getsyntegrity/ego@v4.0.0: invalid version: unknown revision v4.0.0"
	if got.LastLine != want {
		t.Errorf("LastLine = %q, want %q", got.LastLine, want)
	}

	long := "reading https://x: 503 Service Unavailable " + strings.Repeat("y", 1000)
	if l := len(Classify(long).LastLine); l > maxLineLen+3 {
		t.Errorf("LastLine length = %d, want it truncated to about %d", l, maxLineLen)
	}
}

func TestClass_String(t *testing.T) {
	for c, want := range map[Class]string{Transient: "transient", Permanent: "permanent", Unclassified: "unclassified"} {
		if c.String() != want {
			t.Errorf("Class(%d).String() = %q, want %q", c, c.String(), want)
		}
	}
}
