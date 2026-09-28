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
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_ListBuildRuns_SinglePage(t *testing.T) {
	var gotPath, gotAuth, gotAPIVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		gotAPIVersion = r.Header.Get("X-GitHub-Api-Version")
		if r.URL.Query().Get("head_sha") != sha {
			t.Errorf("head_sha query param = %q, want %q", r.URL.Query().Get("head_sha"), sha)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"total_count":1,"workflow_runs":[{"id":36420765355,"head_sha":"`+sha+`","event":"push","status":"completed","conclusion":"success","created_at":"2026-09-28T10:00:00Z","run_started_at":"2026-09-28T10:05:00Z","run_attempt":2,"html_url":"https://github.com/o/r/actions/runs/36420765355"}]}`)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "test-token", HTTPClient: srv.Client()}
	runs, err := c.ListBuildRuns(context.Background(), "getsyntegrity/ego", sha)
	if err != nil {
		t.Fatalf("ListBuildRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1", len(runs))
	}
	if runs[0].ID != 36420765355 || runs[0].HeadSHA != sha || runs[0].Conclusion != "success" {
		t.Fatalf("runs[0] = %+v, unexpected", runs[0])
	}
	if runs[0].RunAttempt != 2 {
		t.Fatalf("runs[0].RunAttempt = %d, want 2", runs[0].RunAttempt)
	}
	wantRunStartedAt := mustTime(t, "2026-09-28T10:05:00Z")
	if !runs[0].RunStartedAt.Equal(wantRunStartedAt) {
		t.Fatalf("runs[0].RunStartedAt = %v, want %v", runs[0].RunStartedAt, wantRunStartedAt)
	}
	if !strings.HasPrefix(gotPath, "/repos/getsyntegrity/ego/actions/workflows/build.yml/runs?") {
		t.Fatalf("request path = %q, want the build.yml runs endpoint", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization header = %q, want Bearer test-token", gotAuth)
	}
	if gotAPIVersion != "2022-11-28" {
		t.Fatalf("X-GitHub-Api-Version header = %q, want 2022-11-28", gotAPIVersion)
	}
}

func TestClient_ListBuildRuns_Pagination(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		page := r.URL.Query().Get("page")
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case "1", "":
			runs := make([]string, 0, 100)
			for i := 0; i < 100; i++ {
				runs = append(runs, fmt.Sprintf(`{"id":%d,"head_sha":%q,"status":"completed","conclusion":"failure","created_at":"2026-09-28T09:00:00Z"}`, i, sha))
			}
			fmt.Fprintf(w, `{"total_count":101,"workflow_runs":[%s]}`, strings.Join(runs, ","))
		case "2":
			fmt.Fprintf(w, `{"total_count":101,"workflow_runs":[{"id":101,"head_sha":%q,"status":"completed","conclusion":"success","created_at":"2026-09-28T10:00:00Z"}]}`, sha)
		default:
			t.Fatalf("unexpected page %q", page)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "test-token", HTTPClient: srv.Client()}
	runs, err := c.ListBuildRuns(context.Background(), "getsyntegrity/ego", sha)
	if err != nil {
		t.Fatalf("ListBuildRuns: %v", err)
	}
	if len(runs) != 101 {
		t.Fatalf("len(runs) = %d, want 101 (both pages)", len(runs))
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (paginated)", calls)
	}
}

// TestClient_ListBuildRuns_ErrorClassification replaces the old
// TestClient_ListBuildRuns_NonOKStatus (which only checked a 401 produced
// *an* error): it now proves ListBuildRuns classifies every non-2xx
// response as either permanent (fail fast: retrying can never succeed —
// 401 unauthorized, 404 not found, or a 403 that is NOT a rate limit) or
// retryable (5xx, 429, or a 403 that IS a rate limit, detected via the
// `X-RateLimit-Remaining: 0` / `Retry-After` headers or a secondary-rate-
// limit message in the response body). main.go's waitForGate uses
// errors.Is(err, ErrPermanentGitHubError) to tell the two apart.
func TestClient_ListBuildRuns_ErrorClassification(t *testing.T) {
	cases := []struct {
		name          string
		status        int
		headers       map[string]string
		body          string
		wantPermanent bool
	}{
		{
			name:          "401 unauthorized is permanent",
			status:        http.StatusUnauthorized,
			body:          `{"message":"Bad credentials"}`,
			wantPermanent: true,
		},
		{
			name:          "404 not found is permanent",
			status:        http.StatusNotFound,
			body:          `{"message":"Not Found"}`,
			wantPermanent: true,
		},
		{
			name:          "403 with no rate-limit signal is permanent",
			status:        http.StatusForbidden,
			body:          `{"message":"Forbidden"}`,
			wantPermanent: true,
		},
		{
			name:          "403 with X-RateLimit-Remaining: 0 is retryable",
			status:        http.StatusForbidden,
			headers:       map[string]string{"X-RateLimit-Remaining": "0"},
			body:          `{"message":"API rate limit exceeded for user"}`,
			wantPermanent: false,
		},
		{
			name:          "403 with Retry-After is retryable",
			status:        http.StatusForbidden,
			headers:       map[string]string{"Retry-After": "60"},
			body:          `{"message":"Forbidden"}`,
			wantPermanent: false,
		},
		{
			name:          "403 secondary rate limit message body is retryable",
			status:        http.StatusForbidden,
			body:          `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes."}`,
			wantPermanent: false,
		},
		{
			name:          "429 too many requests is retryable",
			status:        http.StatusTooManyRequests,
			body:          `{"message":"rate limited"}`,
			wantPermanent: false,
		},
		{
			name:          "500 internal server error is retryable",
			status:        http.StatusInternalServerError,
			body:          `{"message":"boom"}`,
			wantPermanent: false,
		},
		{
			name:          "503 service unavailable is retryable",
			status:        http.StatusServiceUnavailable,
			body:          `{"message":"temporarily unavailable"}`,
			wantPermanent: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()

			c := &Client{BaseURL: srv.URL, Token: "bad-token", HTTPClient: srv.Client()}
			_, err := c.ListBuildRuns(context.Background(), "getsyntegrity/ego", sha)
			if err == nil {
				t.Fatalf("ListBuildRuns: want error on %d, got nil", tc.status)
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("%d", tc.status)) {
				t.Fatalf("error = %v, want it to mention the %d status", err, tc.status)
			}
			if got := errors.Is(err, ErrPermanentGitHubError); got != tc.wantPermanent {
				t.Fatalf("errors.Is(err, ErrPermanentGitHubError) = %v, want %v (err=%v)", got, tc.wantPermanent, err)
			}
		})
	}
}

func TestClient_ListBuildRuns_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"workflow_runs": [ this is not json`)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "t", HTTPClient: srv.Client()}
	_, err := c.ListBuildRuns(context.Background(), "getsyntegrity/ego", sha)
	if err == nil {
		t.Fatal("ListBuildRuns: want error on malformed JSON, got nil")
	}
}

func TestClient_ListBuildRuns_NoTokenOmitsAuthHeader(t *testing.T) {
	var gotAuth string
	sawAuth := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, sawAuth = r.Header.Get("Authorization"), r.Header.Get("Authorization") != ""
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"total_count":0,"workflow_runs":[]}`)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "", HTTPClient: srv.Client()}
	if _, err := c.ListBuildRuns(context.Background(), "getsyntegrity/ego", sha); err != nil {
		t.Fatalf("ListBuildRuns: %v", err)
	}
	if sawAuth {
		t.Fatalf("Authorization header = %q, want none when Token is empty", gotAuth)
	}
}

func TestClient_NewClient_DefaultsBaseURL(t *testing.T) {
	c := NewClient("tok")
	if c.BaseURL != DefaultBaseURL {
		t.Fatalf("BaseURL = %q, want %q", c.BaseURL, DefaultBaseURL)
	}
	if c.Token != "tok" {
		t.Fatalf("Token = %q, want tok", c.Token)
	}
}
