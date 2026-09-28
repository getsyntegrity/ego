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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// DefaultBaseURL is GitHub's REST API base. Client uses it unless a test
// overrides Client.BaseURL to point at an httptest.Server.
const DefaultBaseURL = "https://api.github.com"

// githubAPIVersion pins the REST API version this client was written
// against (docs.github.com/en/rest/actions/workflow-runs, "List workflow
// runs for a workflow", verified 2026-09-28): GitHub recommends every
// caller send this header explicitly so a future default-version change
// cannot silently alter this client's behavior.
const githubAPIVersion = "2022-11-28"

// buildWorkflowFile is the one workflow releasegate ever asks about. It is
// intentionally not a flag: the release gate's whole contract is "build.yml
// succeeded for this exact SHA", not "some configurable workflow did".
const buildWorkflowFile = "build.yml"

// Client is a minimal, read-only GitHub REST client for the one thing the
// release gate needs: listing build.yml's workflow runs for a given commit.
// It never writes anything — the gate only ever decides, it never mutates
// GitHub state.
type Client struct {
	// BaseURL is the API root, no trailing slash. Tests point this at an
	// httptest.Server; production code leaves it at DefaultBaseURL (see
	// NewClient).
	BaseURL string
	// Token is sent as an "Authorization: Bearer <Token>" header
	// (docs.github.com/en/rest/authentication) when non-empty. Production
	// use always sets this (see main.go: GITHUB_TOKEN is required), but an
	// empty Token is accepted rather than rejected here, since the HTTP
	// call itself, not this client, is the right place for GitHub to
	// reject an unauthenticated or under-scoped request.
	Token string
	// HTTPClient performs the actual requests. NewClient sets a sane
	// default; tests may swap in httptest's own client (for TLS trust) or
	// one with a short timeout.
	HTTPClient *http.Client
}

// NewClient returns a Client wired at GitHub's real API, ready for
// production use. token is typically GITHUB_TOKEN from the workflow
// environment; the caller may pass "" to attempt unauthenticated calls,
// which GitHub itself will then rate-limit or reject.
func NewClient(token string) *Client {
	return &Client{
		BaseURL:    DefaultBaseURL,
		Token:      token,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// runsResponse mirrors the subset of GitHub's "List workflow runs for a
// workflow" response body this client reads. Every other field of the real,
// much richer response (head_branch, actor, repository, pull_requests,
// ...) is left undeclared and simply ignored by json.Decode.
type runsResponse struct {
	TotalCount   int      `json:"total_count"`
	WorkflowRuns []apiRun `json:"workflow_runs"`
}

// apiRun mirrors one workflow run object's fields Decide needs. Its field
// order and types must stay identical to Run's (only the json tags
// differ): toRun below converts between them with a plain struct
// conversion, which Go only allows when the two types have the same
// underlying structure.
type apiRun struct {
	ID           int64     `json:"id"`
	HeadSHA      string    `json:"head_sha"`
	Event        string    `json:"event"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	CreatedAt    time.Time `json:"created_at"`
	RunStartedAt time.Time `json:"run_started_at"`
	RunAttempt   int       `json:"run_attempt"`
	HTMLURL      string    `json:"html_url"`
}

// toRun converts apiRun to Run via a plain struct conversion: both types
// declare the same fields, in the same order, with the same underlying
// types (only the json tags differ), so Go's conversion rules already do
// exactly the field-by-field copy a hand-written literal would.
func (a apiRun) toRun() Run {
	return Run(a)
}

// ListBuildRuns returns every build.yml run GitHub reports for the exact
// commit sha, across every page of the result. repo is "owner/name" (e.g.
// "getsyntegrity/ego").
//
// The request filters by head_sha only — never by branch or event. head_sha
// is already the strongest possible filter (the exact tagged commit), and
// adding branch=main or event=push on top of it risks silently dropping a
// run this gate should count: a workflow_dispatch run manually re-run
// against the exact SHA is just as strong evidence that build.yml passed
// for that commit as an automatic push run, so it must not be filtered out
// at the fetch layer (see docs/ci.md, "Release gate", for the recorded
// judgement call). Decide, not this fetch, is where any such distinction
// would belong if it were ever needed.
func (c *Client) ListBuildRuns(ctx context.Context, repo, sha string) ([]Run, error) {
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	var all []Run
	for page := 1; ; page++ {
		q := url.Values{
			"head_sha": {sha},
			"per_page": {"100"},
			"page":     {fmt.Sprintf("%d", page)},
		}
		reqURL := fmt.Sprintf("%s/repos/%s/actions/workflows/%s/runs?%s", c.BaseURL, repo, buildWorkflowFile, q.Encode())

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("building request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("listing %s runs for %s: %w", buildWorkflowFile, sha, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, classifyStatusError(resp.StatusCode, resp.Header, body, sha)
		}
		if readErr != nil {
			return nil, fmt.Errorf("reading response body: %w", readErr)
		}

		var parsed runsResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		for _, r := range parsed.WorkflowRuns {
			all = append(all, r.toRun())
		}

		if len(parsed.WorkflowRuns) < 100 {
			return all, nil
		}
	}
}

// ErrPermanentGitHubError is wrapped into the error ListBuildRuns returns
// when a non-2xx GitHub API response is classified as permanent: no amount
// of retrying before the deadline can turn it into success, so main.go's
// waitForGate checks errors.Is(err, ErrPermanentGitHubError) to fail fast
// instead of spending the whole wait budget on a status that will never
// change. See classifyStatusError for exactly which codes this covers.
var ErrPermanentGitHubError = errors.New("permanent GitHub API error, retrying will not help")

// classifyStatusError builds ListBuildRuns's non-2xx error, wrapping
// ErrPermanentGitHubError when the status is classified as permanent (see
// isPermanentStatus) so callers can tell it apart from a retryable error
// with errors.Is, without string-matching the message.
func classifyStatusError(statusCode int, header http.Header, body []byte, sha string) error {
	msg := fmt.Sprintf("GitHub API returned %d listing %s runs for %s: %s", statusCode, buildWorkflowFile, sha, truncate(body, 500))
	if isPermanentStatus(statusCode, header, body) {
		return fmt.Errorf("%s: %w", msg, ErrPermanentGitHubError)
	}
	return errors.New(msg)
}

// isPermanentStatus reports whether statusCode is one retrying can never
// turn into success:
//
//   - 401 Unauthorized: the token is invalid or missing scope. It will not
//     become valid by waiting.
//   - 404 Not Found: the repo, workflow file, or SHA does not exist as far
//     as this token can see. Waiting does not make it appear.
//   - 403 Forbidden that is NOT a rate limit: GitHub also returns 403 for
//     an actual rate limit (secondary or primary), which IS transient and
//     must stay retryable — isRateLimited tells the two 403 cases apart.
//
// Every other status (5xx, 429, and any other 4xx not listed above) is
// treated as retryable, matching this client's existing conservative
// default of retrying anything not explicitly known to be permanent.
func isPermanentStatus(statusCode int, header http.Header, body []byte) bool {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusNotFound:
		return true
	case http.StatusForbidden:
		return !isRateLimited(header, body)
	default:
		return false
	}
}

// isRateLimited detects a 403 that is actually a rate limit (primary or
// secondary), which GitHub's own docs (docs.github.com/en/rest/using-the-
// rest-api/rate-limits-for-the-rest-api) say to detect via any of:
//
//   - the X-RateLimit-Remaining response header is "0" (primary rate
//     limit exhausted);
//   - a Retry-After response header is present (GitHub sets this on both
//     primary and secondary rate-limit responses to say how long to
//     wait);
//   - the response body's message mentions a rate limit (GitHub's
//     secondary rate limit has no dedicated header, only this prose
//     message — e.g. "You have exceeded a secondary rate limit").
//
// Matching is deliberately loose (a case-insensitive substring check on
// the body) rather than parsing the JSON message field: GitHub does not
// document a stable machine-readable field for this case, and a false
// positive here only means "retry a 403 instead of failing fast", which
// is the safe direction to be wrong in.
func isRateLimited(header http.Header, body []byte) bool {
	if header.Get("X-RateLimit-Remaining") == "0" {
		return true
	}
	if header.Get("Retry-After") != "" {
		return true
	}
	return bytes.Contains(bytes.ToLower(body), []byte("rate limit"))
}

func truncate(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
