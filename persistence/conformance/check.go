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

package conformance

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Lifecycle is the subset of persistence.EventsStore, persistence.StateStore,
// and persistence.SnapshotStore this package needs to manage a store's
// connection around each check. All three store interfaces satisfy it
// structurally.
type Lifecycle interface {
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
}

// Check is one named isolation assertion against a store of type S. It is
// written against require.TestingT rather than *testing.T so the exact same
// check function can run either as a normal `go test` subtest (via
// RunEventsStoreConformance/RunStateStoreConformance/
// RunSnapshotStoreConformance) or captured, without a *testing.T, by
// CaptureEventsStoreChecks/CaptureStateStoreChecks/CaptureSnapshotStoreChecks
// (see the package doc comment's Self-checking section). *testing.T
// satisfies require.TestingT, so no adaptation is needed for the normal
// path.
type Check[S Lifecycle] struct {
	Name string
	Run  func(ctx context.Context, t require.TestingT, store S)
}

// CheckResult reports one Check's outcome when run captured (see
// captureChecks).
type CheckResult struct {
	Name   string
	Failed bool
	Errors []string
}

// runConformance runs every check in checks as its own t.Run subtest,
// against a fresh store obtained from newStore for that subtest alone (never
// shared across checks). A subtest is skipped, not failed, when Connect
// reports the store is not reachable — an external adapter's CI may lack a
// live database, and that must never be misread as a passing isolation
// proof.
func runConformance[S Lifecycle](t *testing.T, checks []Check[S], newStore func(t *testing.T) S) {
	t.Helper()
	ctx := context.Background()
	for _, c := range checks {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			store := newStore(t)
			if err := store.Connect(ctx); err != nil {
				t.Skipf("store not reachable, skipping conformance check: %v", err)
			}
			t.Cleanup(func() {
				_ = store.Disconnect(ctx)
			})
			c.Run(ctx, t, store)
		})
	}
}

// captureTestingT implements require.TestingT by recording failures instead
// of aborting the goroutine it runs in via a real *testing.T's semantics.
// FailNow mimics *testing.T.FailNow by calling runtime.Goexit: callers MUST
// invoke the checked function in a dedicated goroutine (see captureChecks)
// so that Goexit unwinds only that goroutine, never a real test's.
type captureTestingT struct {
	mu     sync.Mutex
	failed bool
	errors []string
}

func (c *captureTestingT) Errorf(format string, args ...interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failed = true
	c.errors = append(c.errors, fmt.Sprintf(format, args...))
}

func (c *captureTestingT) FailNow() {
	c.mu.Lock()
	c.failed = true
	c.mu.Unlock()
	runtime.Goexit()
}

// captureChecks runs every check in checks against a fresh store obtained
// from newStore for that check alone, mirroring runConformance's
// one-store-per-check rule. Each check's pass/fail is captured via
// captureTestingT rather than a real *testing.T, so a detected (expected)
// isolation failure can be asserted on by the caller instead of aborting the
// caller's own test. A store that fails to Connect is reported as a failed
// check rather than silently skipped: this path exists specifically to
// prove failure detection, so a connection problem must never be mistaken
// for isolation.
func captureChecks[S Lifecycle](checks []Check[S], newStore func() S) []CheckResult {
	ctx := context.Background()
	results := make([]CheckResult, 0, len(checks))
	for _, c := range checks {
		store := newStore()
		if err := store.Connect(ctx); err != nil {
			results = append(results, CheckResult{Name: c.Name, Failed: true, Errors: []string{fmt.Sprintf("connect: %v", err)}})
			continue
		}

		capture := &captureTestingT{}
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Run(ctx, capture, store)
		}()
		wg.Wait()

		_ = store.Disconnect(ctx)
		results = append(results, CheckResult{Name: c.Name, Failed: capture.failed, Errors: capture.errors})
	}
	return results
}
