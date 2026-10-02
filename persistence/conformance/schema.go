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
	"sync"
	"testing"

	"github.com/getsyntegrity/urd/persistence"
)

// concurrentMigrators is how many migrators the concurrency check runs at the
// same time against one backend.
const concurrentMigrators = 8

// SchemaT is what a SchemaMigratorHarness hook needs from a test: TestingT to
// report a failure and Cleanup to release what the hook opened. *testing.T
// satisfies it; CaptureSchemaMigratorChecks passes a recorder that does too.
type SchemaT interface {
	TestingT
	Cleanup(func())
}

// SchemaMigratorHarness connects a backend to RunSchemaMigratorConformance.
//
// A hook returns an opener instead of a migrator because the suite needs
// several independent migrators on the same backend: concurrent callers must
// each have their own connection, as separate cluster nodes do. Every call of
// the opener returns a new, connected migrator on that one backend and
// registers its own cleanup on the SchemaT it receives.
type SchemaMigratorHarness struct {
	// NewBackend provisions a fresh, empty backend, one that was never
	// migrated, and returns the opener for it. It runs once per check, never once
	// per suite, so no check sees what another left behind.
	NewBackend func(t SchemaT) (open func(t SchemaT) persistence.SchemaMigrator)

	// LatestVersion is the version SchemaVersion reports once Migrate has run.
	// It must not be 0, which is reserved for a backend that was never migrated.
	LatestVersion uint

	// Legacy lists the schemas that existed before versioning. Only the backend
	// knows what its legacy shapes looked like, so it supplies them. It may be
	// empty for a backend that never had one.
	Legacy []LegacySchema
}

// LegacySchema is one pre-versioning shape of a backend.
type LegacySchema struct {
	// Name identifies the shape in the failure output: it becomes the check name
	// "Legacy/<Name>".
	Name string

	// Prepare provisions a fresh backend holding the legacy shape, with some data
	// if the shape had any, and returns the opener for it.
	Prepare func(t SchemaT) (open func(t SchemaT) persistence.SchemaMigrator)

	// Verify is optional. It runs after the suite has migrated the legacy backend
	// and checked its version, and asserts on what only the backend can see: that
	// the data written before the upgrade is still there, for example. It
	// receives the opener of the migrated backend.
	Verify func(t SchemaT, open func(t SchemaT) persistence.SchemaMigrator)
}

// SchemaMigratorCheck is one named assertion of the schema migrator suite.
type SchemaMigratorCheck struct {
	Name string
	Run  func(ctx context.Context, t SchemaT, h SchemaMigratorHarness)
}

// SchemaMigratorChecks returns the checks RunSchemaMigratorConformance runs for
// h: the fixed checks, then one "Legacy/<Name>" check per legacy schema.
func SchemaMigratorChecks(h SchemaMigratorHarness) []SchemaMigratorCheck {
	checks := []SchemaMigratorCheck{
		{Name: "EmptyBackend/VersionIsZero", Run: schemaVersionIsZeroBeforeMigrate},
		{Name: "Migrate/ReportsLatestVersion", Run: schemaMigrateReportsLatest},
		{Name: "Migrate/IsIdempotent", Run: schemaMigrateIsIdempotent},
		{Name: "Migrate/ConcurrentCallersAllSucceed", Run: schemaMigrateConcurrentCallers},
		{Name: "Migrate/NewMigratorSeesAppliedSchema", Run: schemaNewMigratorSeesAppliedSchema},
	}
	for _, legacy := range h.Legacy {
		checks = append(checks, SchemaMigratorCheck{
			Name: "Legacy/" + legacy.Name,
			Run: func(ctx context.Context, t SchemaT, h SchemaMigratorHarness) {
				schemaMigrateLegacy(ctx, t, h, legacy)
			},
		})
	}
	return checks
}

// RunSchemaMigratorConformance runs the schema migrator suite against a
// persistence.SchemaMigrator implementation, one subtest per check. It checks
// that a never-migrated backend reports version 0, that Migrate reaches the
// latest version and is idempotent, that concurrent callers all succeed, that
// the version is a property of the backend and not of one migrator, and that
// every legacy schema in the harness upgrades.
func RunSchemaMigratorConformance(t *testing.T, h SchemaMigratorHarness) {
	t.Helper()
	ctx := context.Background()
	for _, c := range SchemaMigratorChecks(h) {
		t.Run(c.Name, func(t *testing.T) {
			c.Run(ctx, t, h)
		})
	}
}

// CaptureSchemaMigratorChecks runs the same checks without a *testing.T and
// reports pass or fail for each, so a test can prove that the suite rejects a
// flawed migrator. See the package doc comment's Self-checking section.
func CaptureSchemaMigratorChecks(h SchemaMigratorHarness) []CheckResult {
	ctx := context.Background()
	checks := SchemaMigratorChecks(h)
	results := make([]CheckResult, 0, len(checks))
	for _, c := range checks {
		capture := &schemaCapture{}
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer capture.runCleanups()
			c.Run(ctx, capture, h)
		}()
		wg.Wait()
		results = append(results, CheckResult{Name: c.Name, Failed: capture.failed, Errors: capture.errors})
	}
	return results
}

// schemaCapture is a captureTestingT that also keeps the cleanups, which run
// when the check ends, last registered first, like *testing.T's.
type schemaCapture struct {
	captureTestingT
	cleanups []func()
}

func (c *schemaCapture) Cleanup(f func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanups = append(c.cleanups, f)
}

func (c *schemaCapture) runCleanups() {
	for i := len(c.cleanups) - 1; i >= 0; i-- {
		c.cleanups[i]()
	}
}

func requireLatestVersion(ctx context.Context, t SchemaT, h SchemaMigratorHarness, m persistence.SchemaMigrator, when string) {
	t.Helper()
	requireTrue(t, h.LatestVersion > 0, "the harness must set LatestVersion: 0 means never migrated")
	got, err := m.SchemaVersion(ctx)
	requireNoError(t, err, "SchemaVersion %s", when)
	requireEqual(t, h.LatestVersion, got, "SchemaVersion "+when)
}

func schemaVersionIsZeroBeforeMigrate(ctx context.Context, t SchemaT, h SchemaMigratorHarness) {
	m := h.NewBackend(t)(t)
	got, err := m.SchemaVersion(ctx)
	requireNoError(t, err, "SchemaVersion of a backend that was never migrated")
	requireEqual(t, uint(0), got, "a backend that was never migrated must report version 0")
}

func schemaMigrateReportsLatest(ctx context.Context, t SchemaT, h SchemaMigratorHarness) {
	m := h.NewBackend(t)(t)
	requireNoError(t, m.Migrate(ctx), "Migrate on an empty backend")
	requireLatestVersion(ctx, t, h, m, "after Migrate")
}

func schemaMigrateIsIdempotent(ctx context.Context, t SchemaT, h SchemaMigratorHarness) {
	m := h.NewBackend(t)(t)
	requireNoError(t, m.Migrate(ctx), "first Migrate")
	requireNoError(t, m.Migrate(ctx), "second Migrate must be a no-op, not an error")
	requireLatestVersion(ctx, t, h, m, "after Migrate ran twice")
}

func schemaMigrateConcurrentCallers(ctx context.Context, t SchemaT, h SchemaMigratorHarness) {
	open := h.NewBackend(t)
	migrators := make([]persistence.SchemaMigrator, concurrentMigrators)
	for i := range migrators {
		migrators[i] = open(t)
	}

	errs := make([]error, len(migrators))
	var wg sync.WaitGroup
	for i, m := range migrators {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = m.Migrate(ctx)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		requireNoError(t, err, fmt.Sprintf("concurrent Migrate #%d", i))
	}
	requireLatestVersion(ctx, t, h, open(t), "after concurrent Migrate calls")
}

func schemaNewMigratorSeesAppliedSchema(ctx context.Context, t SchemaT, h SchemaMigratorHarness) {
	open := h.NewBackend(t)
	requireNoError(t, open(t).Migrate(ctx), "Migrate")
	requireLatestVersion(ctx, t, h, open(t), "read through a new migrator")
}

func schemaMigrateLegacy(ctx context.Context, t SchemaT, h SchemaMigratorHarness, legacy LegacySchema) {
	open := legacy.Prepare(t)
	m := open(t)
	requireNoError(t, m.Migrate(ctx), "Migrate on the legacy schema")
	requireLatestVersion(ctx, t, h, m, "after upgrading the legacy schema")
	requireNoError(t, m.Migrate(ctx), "Migrate again on the upgraded legacy schema")
	requireLatestVersion(ctx, t, h, m, "after Migrate ran again on the upgraded legacy schema")
	if legacy.Verify != nil {
		legacy.Verify(t, open)
	}
}
