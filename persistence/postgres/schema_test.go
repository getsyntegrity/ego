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

package postgres

// These tests cover the schema runner's pure logic, with no database: how the
// SQL files are read and ordered, which of them are pending, and how the
// version of a database that predates schema versioning is inferred from its
// shape. What the SQL does to a real Postgres is checked by the DSN-gated
// conformance run in example/cluster (spec B moves it to the integration
// module).

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/getsyntegrity/go-specs/specs"
)

func catalogOf(objects ...string) catalog {
	found := catalog{}
	for _, o := range objects {
		found[o] = true
	}
	return found
}

func sqlFile(content string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(content)} }

type loadCase struct {
	name    string
	files   fstest.MapFS
	want    []schemaFile
	wantErr string
}

func TestLoadSchemaFiles(t *testing.T) {
	specs.Describe(t, "postgres.loadSchemaFiles ordering and validation of the numbered SQL files", func(s *specs.Spec) {
		specs.Table(s, []loadCase{
			{
				name: "orders the files by version 1..n",
				files: fstest.MapFS{
					"schema/003_c.sql": sqlFile("SELECT 3;"),
					"schema/001_a.sql": sqlFile("SELECT 1;"),
					"schema/002_b.sql": sqlFile("SELECT 2;"),
				},
				want: []schemaFile{
					{version: 1, name: "001_a.sql", sql: "SELECT 1;"},
					{version: 2, name: "002_b.sql", sql: "SELECT 2;"},
					{version: 3, name: "003_c.sql", sql: "SELECT 3;"},
				},
			},
			{
				name: "ignores files that are not .sql",
				files: fstest.MapFS{
					"schema/001_a.sql": sqlFile("SELECT 1;"),
					"schema/README.md": sqlFile("notes"),
				},
				want: []schemaFile{{version: 1, name: "001_a.sql", sql: "SELECT 1;"}},
			},
			{
				name:    "rejects a sequence that does not start at 1",
				files:   fstest.MapFS{"schema/002_b.sql": sqlFile("SELECT 2;")},
				wantErr: "gap",
			},
			{
				name: "rejects a gap in the sequence",
				files: fstest.MapFS{
					"schema/001_a.sql": sqlFile("SELECT 1;"),
					"schema/003_c.sql": sqlFile("SELECT 3;"),
				},
				wantErr: "gap",
			},
			{
				name: "rejects two files with the same version",
				files: fstest.MapFS{
					"schema/001_a.sql": sqlFile("SELECT 1;"),
					"schema/001_b.sql": sqlFile("SELECT 1;"),
				},
				wantErr: "duplicate",
			},
			{
				name:    "rejects a .sql file without a numeric prefix",
				files:   fstest.MapFS{"schema/events.sql": sqlFile("SELECT 1;")},
				wantErr: "not named",
			},
			{
				name:    "rejects version 0 because 0 means never migrated",
				files:   fstest.MapFS{"schema/000_a.sql": sqlFile("SELECT 1;")},
				wantErr: "version 0",
			},
			{
				name:    "rejects an empty file",
				files:   fstest.MapFS{"schema/001_a.sql": sqlFile("  \n")},
				wantErr: "empty",
			},
			{
				name:    "rejects a directory with no SQL file",
				files:   fstest.MapFS{"schema/README.md": sqlFile("notes")},
				wantErr: "no schema files",
			},
		}, func(c loadCase) string { return c.name }, func(ctx *specs.Context, c loadCase) {
			got, err := loadSchemaFiles(c.files, "schema")
			if c.wantErr != "" {
				ctx.Expect(err).To(specs.Not(specs.BeNil()))
				ctx.Expect(err.Error()).To(specs.MatchRegex(c.wantErr))
				return
			}
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.Equal(c.want))
		})
	})
}

func TestEmbeddedSchema(t *testing.T) {
	specs.Describe(t, "postgres embedded schema files", func(s *specs.Spec) {
		s.It("load as the version sequence 1..n, each holding SQL", func(ctx *specs.Context) {
			files, err := loadSchemaFiles(schemaFS, schemaDir)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(files).To(specs.EveryElement(specs.Satisfy("has SQL", func(f any) bool { return f.(schemaFile).sql != "" })))
			ctx.Expect(files[len(files)-1].version).To(specs.Equal(uint(len(files))))
		})

		s.It("have one baseline marker per file, so no version can be skipped by mistake", func(ctx *specs.Context) {
			files, err := loadSchemaFiles(schemaFS, schemaDir)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(baselineMarkers).To(specs.HaveLen(len(files)))
		})
	})
}

type pendingCase struct {
	name    string
	current uint
	want    []uint
}

func TestPendingSchemaFiles(t *testing.T) {
	files := []schemaFile{{version: 1}, {version: 2}, {version: 3}}
	specs.Describe(t, "postgres.pendingSchemaFiles selection by recorded version", func(s *specs.Spec) {
		specs.Table(s, []pendingCase{
			{name: "selects every file for a database that was never migrated", current: 0, want: []uint{1, 2, 3}},
			{name: "selects only the files above the recorded version for a partly migrated database", current: 1, want: []uint{2, 3}},
			{name: "selects nothing for an up-to-date database", current: 3, want: nil},
			{name: "selects nothing for a database ahead of this build", current: 9, want: nil},
		}, func(c pendingCase) string { return c.name }, func(ctx *specs.Context, c pendingCase) {
			var got []uint
			for _, f := range pendingSchemaFiles(files, c.current) {
				got = append(got, f.version)
			}
			ctx.Expect(got).To(specs.Equal(c.want))
		})
	})
}

type baselineCase struct {
	name    string
	objects []string
	want    uint
	wantErr bool
}

func TestInferSchemaVersion(t *testing.T) {
	specs.Describe(t, "postgres.inferSchemaVersion baseline detection for a database with no version record", func(s *specs.Spec) {
		specs.Table(s, []baselineCase{
			{name: "infers version 0 for an empty database", objects: nil, want: 0},
			{name: "infers version 0 when only the offsets table exists, because the earlier tables are missing",
				objects: []string{"offsets_store"}, want: 0},
			{name: "infers version 1 for events_store with tenant_id",
				objects: []string{"events_store", "events_store.tenant_id"}, want: 1},
			{name: "infers version 2 once the events indexes exist",
				objects: []string{"events_store", "events_store.tenant_id", "index.idx_events_store_shard"}, want: 2},
			{name: "infers version 3 once the revisions table exists",
				objects: []string{"events_store", "events_store.tenant_id", "index.idx_events_store_shard", "events_store_revisions"}, want: 3},
			{name: "stops at version 1 for a legacy database without the indexes",
				objects: []string{"events_store", "events_store.tenant_id", "events_store_revisions"}, want: 1},
			{name: "infers version 4 once tenant_metadata exists",
				objects: []string{"events_store", "events_store.tenant_id", "index.idx_events_store_shard", "events_store_revisions", "events_store.tenant_metadata"}, want: 4},
			{name: "infers version 5 for the full k8s init.sql shape",
				objects: []string{"events_store", "events_store.tenant_id", "index.idx_events_store_shard", "events_store_revisions", "events_store.tenant_metadata", "offsets_store"}, want: 5},
			{name: "does not skip a version when a later object exists without the earlier ones",
				objects: []string{"events_store", "events_store.tenant_id", "events_store.tenant_metadata", "offsets_store"}, want: 1},
			{name: "refuses an events_store without tenant_id, which predates the scoped stores",
				objects: []string{"events_store"}, wantErr: true},
		}, func(c baselineCase) string { return c.name }, func(ctx *specs.Context, c baselineCase) {
			catalog := catalogOf(c.objects...)
			got, err := inferSchemaVersion(catalog)
			if c.wantErr {
				ctx.Expect(err).To(specs.MatchError(ErrUnsupportedSchema))
				return
			}
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.Equal(c.want))
		})
	})
}

type aheadCase struct {
	name    string
	current uint
	latest  uint
	wantErr bool
	wantMsg string
}

func TestCheckSchemaNotAhead(t *testing.T) {
	specs.Describe(t, "postgres.checkSchemaNotAhead comparison of the recorded version with the newest embedded file", func(s *specs.Spec) {
		specs.Table(s, []aheadCase{
			{name: "accepts a database that was never migrated", current: 0, latest: 5},
			{name: "accepts a database one file behind", current: 4, latest: 5},
			{name: "accepts a database at exactly the newest version", current: 5, latest: 5},
			{name: "refuses a database one version ahead, naming both versions", current: 6, latest: 5, wantErr: true,
				wantMsg: "database is at version 6, this binary knows up to 5"},
			{name: "refuses a database far ahead", current: 99, latest: 5, wantErr: true,
				wantMsg: "database is at version 99, this binary knows up to 5"},
		}, func(c aheadCase) string { return c.name }, func(ctx *specs.Context, c aheadCase) {
			err := checkSchemaNotAhead(c.current, c.latest)
			if !c.wantErr {
				ctx.Expect(err).To(specs.BeNil())
				return
			}
			ctx.Expect(err).To(specs.MatchError(ErrSchemaAhead))
			ctx.Expect(err.Error()).To(specs.MatchRegex(c.wantMsg))
		})
	})
}

func TestStoresNeedAConnectionToMigrate(t *testing.T) {
	specs.Describe(t, "postgres.EventStore and postgres.OffsetStore Migrate and SchemaVersion before Connect", func(s *specs.Spec) {
		s.It("EventStore returns ErrNotConnected from Migrate and SchemaVersion", func(ctx *specs.Context) {
			store := NewEventStore("postgres://unused")
			ctx.Expect(store.Migrate(context.Background())).To(specs.MatchError(ErrNotConnected))
			_, err := store.SchemaVersion(context.Background())
			ctx.Expect(err).To(specs.MatchError(ErrNotConnected))
		})

		s.It("OffsetStore returns ErrNotConnected from Migrate and SchemaVersion", func(ctx *specs.Context) {
			store := NewOffsetStore("postgres://unused")
			ctx.Expect(store.Migrate(context.Background())).To(specs.MatchError(ErrNotConnected))
			_, err := store.SchemaVersion(context.Background())
			ctx.Expect(err).To(specs.MatchError(ErrNotConnected))
		})
	})
}
