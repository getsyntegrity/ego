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
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Class is how one failed probe attempt is treated by the wait loop.
type Class int

const (
	// Unclassified output is retried until the deadline (fail-open on
	// purpose: an unknown message is more likely a new phrasing of a
	// transient condition than proof of permanence, and the deadline bounds
	// the cost) but reported as "unclassified" so a human notices it.
	Unclassified Class = iota
	// Transient failures are propagation delays or outages: retry.
	Transient
	// Permanent failures can never turn into success by waiting: fail now.
	Permanent
)

func (c Class) String() string {
	switch c {
	case Transient:
		return "transient"
	case Permanent:
		return "permanent"
	default:
		return "unclassified"
	}
}

// maxLineLen bounds Result.LastLine so an oversized response body cannot
// flood a workflow annotation.
const maxLineLen = 300

// Result is the classification of one attempt's captured output.
type Result struct {
	Class    Class
	Reason   string // short cause, e.g. "HTTP 404" or "network error"
	LastLine string // last non-empty output line, truncated
}

var (
	// statusRe finds an HTTP status the way Go's module fetcher prints it
	// ("reading <url>: 404 Not Found").
	statusRe = regexp.MustCompile(`\b([1-5]\d\d) [A-Z][a-z]+`)

	networkMarkers = []string{
		"dial tcp", "i/o timeout", "connection reset", "connection refused",
		"tls handshake timeout", "no such host", "unexpected eof",
		"temporary failure in name resolution", "client.timeout",
		"context deadline exceeded", "network is unreachable", "broken pipe",
	}

	// permanentMarkers are lowercase substrings that prove the request
	// itself is wrong, whatever the servers say. "invalid version" is
	// handled separately because the real transient failure embeds
	// "invalid version: unknown revision".
	permanentMarkers = []string{
		"declares its path as", "but was required as", "malformed",
		"should be v", "must match major version", "post-v0 module path",
	}
)

// Classify decides how a failed attempt is treated. It is a pure function of
// the captured output (the JSON "Error" field plus stderr of
// `go mod download -json`), so every case is testable without network.
//
// Precedence, highest first:
//  1. checksum mismatch / SECURITY ERROR: permanent. Integrity beats
//     everything, even a 404 elsewhere in the same output.
//  2. HTTP 410 Gone: permanent.
//  3. HTTP 404, 408, 429 or 5xx: transient. The HTTP status wins over any
//     text: the real failure (release.yml run 36474920456, attempt 1)
//     reads "404 Not Found ... invalid version: unknown revision v4.0.0",
//     and it was only propagation delay.
//  4. network errors: transient.
//  5. a malformed request (declared-path mismatch, major-suffix mismatch,
//     malformed version, any "invalid version" other than "unknown
//     revision"): permanent.
//  6. "unknown revision" with no HTTP status: transient, because the proxy
//     or the VCS may not have the tag yet; a tag that never appears costs
//     one full wait, never a false success.
//  7. anything else: Unclassified (retried).
func Classify(output string) Result {
	r := Result{LastLine: lastLine(output)}
	lower := strings.ToLower(output)

	if strings.Contains(lower, "checksum mismatch") || strings.Contains(lower, "security error") {
		r.Class, r.Reason = Permanent, "checksum mismatch or SECURITY ERROR"
		return r
	}

	var transientStatus int
	for _, m := range statusRe.FindAllStringSubmatch(output, -1) {
		code, _ := strconv.Atoi(m[1])
		switch {
		case code == 410:
			r.Class, r.Reason = Permanent, "HTTP 410 Gone"
			return r
		case code == 404 || code == 408 || code == 429 || code >= 500:
			if transientStatus == 0 {
				transientStatus = code
			}
		}
	}
	if transientStatus != 0 {
		r.Class, r.Reason = Transient, fmt.Sprintf("HTTP %d", transientStatus)
		return r
	}

	for _, m := range networkMarkers {
		if strings.Contains(lower, m) {
			r.Class, r.Reason = Transient, "network error ("+m+")"
			return r
		}
	}

	for _, m := range permanentMarkers {
		if strings.Contains(lower, m) {
			r.Class, r.Reason = Permanent, "invalid request or module path ("+m+")"
			return r
		}
	}
	const unknownRev = "invalid version: unknown revision"
	if strings.Contains(strings.ReplaceAll(lower, unknownRev, ""), "invalid version") {
		r.Class, r.Reason = Permanent, "invalid version"
		return r
	}
	if strings.Contains(lower, "unknown revision") {
		r.Class, r.Reason = Transient, "unknown revision without an HTTP status (tag not visible yet)"
		return r
	}

	r.Class, r.Reason = Unclassified, "unrecognized output"
	return r
}

// lastLine returns the last non-empty trimmed line, truncated.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	l := strings.TrimSpace(lines[len(lines)-1])
	if len(l) > maxLineLen {
		l = l[:maxLineLen] + "..."
	}
	return l
}
