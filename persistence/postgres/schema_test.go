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
	specs.Describe(t, "loadSchemaFiles reads the numbered SQL files in order", func(s *specs.Spec) {
		specs.Table(s, []loadCase{
			{
				name: "versions 1..n in order",
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
				name: "files that are not .sql are ignored",
				files: fstest.MapFS{
					"schema/001_a.sql": sqlFile("SELECT 1;"),
					"schema/README.md": sqlFile("notes"),
				},
				want: []schemaFile{{version: 1, name: "001_a.sql", sql: "SELECT 1;"}},
			},
			{
				name:    "a sequence that does not start at 1 is rejected",
				files:   fstest.MapFS{"schema/002_b.sql": sqlFile("SELECT 2;")},
				wantErr: "gap",
			},
			{
				name: "a gap in the sequence is rejected",
				files: fstest.MapFS{
					"schema/001_a.sql": sqlFile("SELECT 1;"),
					"schema/003_c.sql": sqlFile("SELECT 3;"),
				},
				wantErr: "gap",
			},
			{
				name: "two files with the same version are rejected",
				files: fstest.MapFS{
					"schema/001_a.sql": sqlFile("SELECT 1;"),
					"schema/001_b.sql": sqlFile("SELECT 1;"),
				},
				wantErr: "duplicate",
			},
			{
				name:    "a .sql file without a numeric prefix is rejected",
				files:   fstest.MapFS{"schema/events.sql": sqlFile("SELECT 1;")},
				wantErr: "not named",
			},
			{
				name:    "version 0 is rejected because 0 means never migrated",
				files:   fstest.MapFS{"schema/000_a.sql": sqlFile("SELECT 1;")},
				wantErr: "version 0",
			},
			{
				name:    "an empty file is rejected",
				files:   fstest.MapFS{"schema/001_a.sql": sqlFile("  \n")},
				wantErr: "empty",
			},
			{
				name:    "a directory with no SQL file is rejected",
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
	specs.Describe(t, "the SQL files shipped with the module", func(s *specs.Spec) {
		s.It("load as the sequence 1..n", func(ctx *specs.Context) {
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
	specs.Describe(t, "pendingSchemaFiles selects the files above the current version", func(s *specs.Spec) {
		specs.Table(s, []pendingCase{
			{name: "an empty database applies everything", current: 0, want: []uint{1, 2, 3}},
			{name: "a partly migrated database applies the rest", current: 1, want: []uint{2, 3}},
			{name: "an up-to-date database applies nothing", current: 3, want: nil},
			{name: "a database ahead of this build applies nothing", current: 9, want: nil},
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
	specs.Describe(t, "inferSchemaVersion reads the version of a database that was never versioned", func(s *specs.Spec) {
		specs.Table(s, []baselineCase{
			{name: "an empty database is version 0", objects: nil, want: 0},
			{name: "only the offsets table is still version 0, the earlier tables are missing",
				objects: []string{"offsets_store"}, want: 0},
			{name: "events_store with tenant_id is version 1",
				objects: []string{"events_store", "events_store.tenant_id"}, want: 1},
			{name: "the events indexes make it version 2",
				objects: []string{"events_store", "events_store.tenant_id", "index.idx_events_store_shard"}, want: 2},
			{name: "the revisions table makes it version 3",
				objects: []string{"events_store", "events_store.tenant_id", "index.idx_events_store_shard", "events_store_revisions"}, want: 3},
			{name: "the shape of a legacy database without indexes stops before them",
				objects: []string{"events_store", "events_store.tenant_id", "events_store_revisions"}, want: 1},
			{name: "tenant_metadata makes it version 4",
				objects: []string{"events_store", "events_store.tenant_id", "index.idx_events_store_shard", "events_store_revisions", "events_store.tenant_metadata"}, want: 4},
			{name: "the full k8s init.sql shape is version 5",
				objects: []string{"events_store", "events_store.tenant_id", "index.idx_events_store_shard", "events_store_revisions", "events_store.tenant_metadata", "offsets_store"}, want: 5},
			{name: "a later object without the earlier ones does not skip a version",
				objects: []string{"events_store", "events_store.tenant_id", "events_store.tenant_metadata", "offsets_store"}, want: 1},
			{name: "events_store without tenant_id predates the scoped stores and is refused",
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

func TestStoresNeedAConnectionToMigrate(t *testing.T) {
	specs.Describe(t, "Migrate and SchemaVersion on a store that is not connected", func(s *specs.Spec) {
		s.It("EventStore returns ErrNotConnected", func(ctx *specs.Context) {
			store := NewEventStore("postgres://unused")
			ctx.Expect(store.Migrate(context.Background())).To(specs.MatchError(ErrNotConnected))
			_, err := store.SchemaVersion(context.Background())
			ctx.Expect(err).To(specs.MatchError(ErrNotConnected))
		})

		s.It("OffsetStore returns ErrNotConnected", func(ctx *specs.Context) {
			store := NewOffsetStore("postgres://unused")
			ctx.Expect(store.Migrate(context.Background())).To(specs.MatchError(ErrNotConnected))
			_, err := store.SchemaVersion(context.Background())
			ctx.Expect(err).To(specs.MatchError(ErrNotConnected))
		})
	})
}
