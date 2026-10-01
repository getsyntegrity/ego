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

package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/jackc/pgx/v5"

	pginfra "github.com/getsyntegrity/ego/inttest/infra/postgres"
)

var shared *pginfra.Postgres

func TestMain(m *testing.M) {
	ctx := context.Background()
	pg, err := pginfra.StartPostgres(ctx)
	if err != nil {
		_, _ = os.Stderr.WriteString("inttest/infra/postgres: cannot start the Postgres container: " + err.Error() + "\n")
		os.Exit(1)
	}
	shared = pg
	code := m.Run()
	if err := pg.Terminate(ctx); err != nil {
		_, _ = os.Stderr.WriteString("inttest/infra/postgres: cannot terminate the Postgres container: " + err.Error() + "\n")
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func TestPostgresNewDatabase(t *testing.T) {
	t.Parallel()
	specs.Describe(t, "postgres.Postgres.NewDatabase on the shared container", func(s *specs.Spec) {
		s.It("returns a reachable database that has no tables", func(sc *specs.Context) {
			ctx := context.Background()
			dsn := shared.NewDatabase(t)

			conn, err := pgx.Connect(ctx, dsn)
			sc.Expect(err).To(specs.BeNil())
			defer func() { _ = conn.Close(ctx) }()

			var tables int
			sc.Expect(conn.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public'`).Scan(&tables)).To(specs.BeNil())
			sc.Expect(tables).To(specs.Equal(0))
		})

		s.It("gives each call a database whose tables the other databases do not see", func(sc *specs.Context) {
			ctx := context.Background()
			first, second := shared.NewDatabase(t), shared.NewDatabase(t)
			sc.Expect(first).To(specs.Not(specs.Equal(second)))

			conn, err := pgx.Connect(ctx, first)
			sc.Expect(err).To(specs.BeNil())
			defer func() { _ = conn.Close(ctx) }()
			_, err = conn.Exec(ctx, `CREATE TABLE only_in_first (id INT)`)
			sc.Expect(err).To(specs.BeNil())

			other, err := pgx.Connect(ctx, second)
			sc.Expect(err).To(specs.BeNil())
			defer func() { _ = other.Close(ctx) }()
			var found bool
			sc.Expect(other.QueryRow(ctx, `SELECT to_regclass('only_in_first') IS NOT NULL`).Scan(&found)).To(specs.BeNil())
			sc.Expect(found).To(specs.BeFalse())
		})
	})
}
