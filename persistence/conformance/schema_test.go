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
	"errors"
	"runtime"
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/persistence"
)

const fakeLatestVersion = 3

// fakeBackend is the in-memory "database" the fake migrators share. version 0
// is a backend that was never migrated; legacy is the pre-versioning shape.
type fakeBackend struct {
	mu       sync.Mutex
	version  uint
	legacy   bool
	inflight int
}

// fakeFlaws switches one deliberate defect on, so the suite can be shown to
// fail it. The zero value is a correct migrator.
type fakeFlaws struct {
	failsWhenAlreadyLatest bool // Migrate is not idempotent
	failsUnderContention   bool // Migrate is not safe under concurrent callers
	reportsOneVersionShort bool // SchemaVersion lags behind the real schema
	failsOnLegacy          bool // Migrate re-creates what a legacy schema already has
	reportsVersionBefore   bool // SchemaVersion of an empty backend is not 0
}

var (
	errAlreadyLatest = errors.New("fake: schema already at the latest version")
	errContended     = errors.New("fake: another Migrate is running")
	errLegacyClash   = errors.New("fake: relation already exists")
)

type fakeMigrator struct {
	backend *fakeBackend
	flaws   fakeFlaws
}

func (m *fakeMigrator) Migrate(context.Context) error {
	b := m.backend
	if m.flaws.failsUnderContention {
		return m.migrateWithoutLock()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if m.flaws.failsWhenAlreadyLatest && b.version == fakeLatestVersion {
		return errAlreadyLatest
	}
	if m.flaws.failsOnLegacy && b.legacy {
		return errLegacyClash
	}
	b.version = fakeLatestVersion
	return nil
}

// migrateWithoutLock is the contention flaw: it does not serialize callers and
// fails when it sees another one inside. It yields, without any real time,
// for a bounded number of turns so that a concurrent caller overlaps with it
// on every run; a lone caller just finishes.
func (m *fakeMigrator) migrateWithoutLock() error {
	b := m.backend
	b.mu.Lock()
	b.inflight++
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.inflight--
		b.mu.Unlock()
	}()
	for i := 0; i < 20_000; i++ {
		b.mu.Lock()
		n := b.inflight
		b.mu.Unlock()
		if n > 1 {
			return errContended
		}
		runtime.Gosched()
	}
	b.mu.Lock()
	b.version = fakeLatestVersion
	b.mu.Unlock()
	return nil
}

func (m *fakeMigrator) SchemaVersion(context.Context) (uint, error) {
	b := m.backend
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case m.flaws.reportsOneVersionShort && b.version == fakeLatestVersion:
		return b.version - 1, nil
	case m.flaws.reportsVersionBefore && b.version == 0:
		return 1, nil
	}
	return b.version, nil
}

func fakeHarness(flaws fakeFlaws) SchemaMigratorHarness {
	opener := func(b *fakeBackend) func(SchemaT) persistence.SchemaMigrator {
		return func(SchemaT) persistence.SchemaMigrator { return &fakeMigrator{backend: b, flaws: flaws} }
	}
	return SchemaMigratorHarness{
		NewBackend:    func(SchemaT) func(SchemaT) persistence.SchemaMigrator { return opener(&fakeBackend{}) },
		LatestVersion: fakeLatestVersion,
		Legacy: []LegacySchema{{
			Name: "BeforeVersioning",
			Prepare: func(SchemaT) func(SchemaT) persistence.SchemaMigrator {
				return opener(&fakeBackend{legacy: true})
			},
		}},
	}
}

func failedChecks(results []CheckResult) []string {
	var out []string
	for _, r := range results {
		if r.Failed {
			out = append(out, r.Name)
		}
	}
	return out
}

func TestRunSchemaMigratorConformance(t *testing.T) {
	specs.Describe(t, "RunSchemaMigratorConformance accepts a correct migrator", func(s *specs.Spec) {
		s.It("runs every check as a subtest and passes", func(ctx *specs.Context) {
			RunSchemaMigratorConformance(ctx.T, fakeHarness(fakeFlaws{}))
		})

		s.It("passes every captured check", func(ctx *specs.Context) {
			ctx.Expect(failedChecks(CaptureSchemaMigratorChecks(fakeHarness(fakeFlaws{})))).To(specs.BeEmpty())
		})
	})
}

type flawCase struct {
	name   string
	flaws  fakeFlaws
	failed []string
}

func TestSchemaMigratorConformanceCatchesFlawedMigrators(t *testing.T) {
	specs.Describe(t, "the schema migrator suite fails a migrator with a defect", func(s *specs.Spec) {
		specs.Table(s, []flawCase{
			{name: "Migrate that is not idempotent", flaws: fakeFlaws{failsWhenAlreadyLatest: true},
				failed: []string{"Migrate/IsIdempotent", "Migrate/ConcurrentCallersAllSucceed", "Legacy/BeforeVersioning"}},
			{name: "Migrate that is not safe under concurrent callers", flaws: fakeFlaws{failsUnderContention: true},
				failed: []string{"Migrate/ConcurrentCallersAllSucceed"}},
			{name: "SchemaVersion that lags behind", flaws: fakeFlaws{reportsOneVersionShort: true},
				failed: []string{"Migrate/ReportsLatestVersion", "Migrate/IsIdempotent", "Migrate/ConcurrentCallersAllSucceed",
					"Migrate/NewMigratorSeesAppliedSchema", "Legacy/BeforeVersioning"}},
			{name: "SchemaVersion of an empty backend that is not 0", flaws: fakeFlaws{reportsVersionBefore: true},
				failed: []string{"EmptyBackend/VersionIsZero"}},
			{name: "Migrate that cannot upgrade a legacy schema", flaws: fakeFlaws{failsOnLegacy: true},
				failed: []string{"Legacy/BeforeVersioning"}},
		}, func(c flawCase) string { return c.name }, func(ctx *specs.Context, c flawCase) {
			ctx.Expect(failedChecks(CaptureSchemaMigratorChecks(fakeHarness(c.flaws)))).To(specs.ContainTheSameElementsAs(c.failed))
		})

		s.It("fails when the harness does not say which version is the latest", func(ctx *specs.Context) {
			h := fakeHarness(fakeFlaws{})
			h.LatestVersion = 0
			ctx.Expect(failedChecks(CaptureSchemaMigratorChecks(h))).To(specs.Contain("Migrate/ReportsLatestVersion"))
		})
	})
}
