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
		fmt.Fprint(w, `{"total_count":1,"workflow_runs":[{"id":36420765355,"head_sha":"`+sha+`","event":"push","status":"completed","conclusion":"success","created_at":"2026-09-28T10:00:00Z","html_url":"https://github.com/o/r/actions/runs/36420765355"}]}`)
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

func TestClient_ListBuildRuns_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"Bad credentials"}`)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "bad-token", HTTPClient: srv.Client()}
	_, err := c.ListBuildRuns(context.Background(), "getsyntegrity/ego", sha)
	if err == nil {
		t.Fatal("ListBuildRuns: want error on 401, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v, want it to mention the 401 status", err)
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
