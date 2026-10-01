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

// Package infra starts the real infrastructure the integration tests of the inttest module run against.
//
// Every helper starts one container with Testcontainers and returns a handle. A package starts its container
// once, from TestMain, and ends it after m.Run, so the cost is paid once per package and not once per test. A
// helper never skips: when Docker or the container is not available it returns an error and TestMain exits
// non-zero, so a missing environment is a red run and not a green one.
//
// Only Postgres exists today (StartPostgres). The sibling helpers follow the same shape and live next to it in
// this package, one file per system, once a test needs them:
//
//   - StartKafka(ctx) (*Kafka, error), for the Kafka publisher;
//   - StartNATS(ctx) (*NATS, error), for the NATS publisher;
//   - StartPulsar(ctx) (*Pulsar, error), for the Pulsar publisher.
package infra

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// PostgresImage is the exact image the tests run against. It is pinned to a tag so a run never depends on
// whatever "latest" is that day.
const PostgresImage = "postgres:17.6-alpine"

// Postgres is a running Postgres container shared by all the tests of one package.
type Postgres struct {
	container *tcpostgres.PostgresContainer
	baseDSN   *url.URL
}

// StartPostgres starts a Postgres container and waits until it accepts connections. It takes no testing.TB
// because it runs from TestMain, where there is none; the caller owns the error and Terminate.
func StartPostgres(ctx context.Context) (*Postgres, error) {
	container, err := tcpostgres.Run(ctx, PostgresImage,
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		// Many parallel tests each open their own pools; the default of 100 connections is too low for that.
		testcontainers.WithCmdArgs("-c", "max_connections=1000", "-c", "fsync=off"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		if container != nil {
			_ = testcontainers.TerminateContainer(container)
		}
		return nil, fmt.Errorf("start the %s container (is Docker running?): %w", PostgresImage, err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = testcontainers.TerminateContainer(container)
		return nil, fmt.Errorf("read the connection string of the Postgres container: %w", err)
	}
	base, err := url.Parse(dsn)
	if err != nil {
		_ = testcontainers.TerminateContainer(container)
		return nil, fmt.Errorf("parse the connection string of the Postgres container: %w", err)
	}
	return &Postgres{container: container, baseDSN: base}, nil
}

// Terminate stops and removes the container. TestMain calls it after m.Run.
func (p *Postgres) Terminate(ctx context.Context) error {
	if p == nil || p.container == nil {
		return nil
	}
	return p.container.Terminate(ctx)
}

// NewDatabase creates an empty database with a unique name on the shared container and returns its DSN. The
// database is dropped when the test ends. Each test gets its own, so tests can run in parallel without seeing
// each other's rows. It fails the test when the database cannot be created.
func (p *Postgres) NewDatabase(tb testing.TB) string {
	tb.Helper()
	ctx := context.Background()

	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		tb.Fatalf("generate a database name: %v", err)
	}
	name := "t_" + hex.EncodeToString(suffix)

	if err := p.admin(ctx, `CREATE DATABASE `+pgx.Identifier{name}.Sanitize()); err != nil {
		tb.Fatalf("create the database %s: %v", name, err)
	}
	tb.Cleanup(func() {
		// FORCE closes connections a test left open, so the drop never waits on a leaked pool.
		if err := p.admin(context.Background(), `DROP DATABASE IF EXISTS `+pgx.Identifier{name}.Sanitize()+` WITH (FORCE)`); err != nil {
			tb.Errorf("drop the database %s: %v", name, err)
		}
	})

	dsn := *p.baseDSN
	dsn.Path = "/" + name
	return dsn.String()
}

// admin runs one statement against the maintenance database of the container.
func (p *Postgres) admin(ctx context.Context, statement string) (err error) {
	conn, err := pgx.Connect(ctx, p.baseDSN.String())
	if err != nil {
		return fmt.Errorf("connect to the maintenance database: %w", err)
	}
	defer func() { err = errors.Join(err, conn.Close(ctx)) }()
	_, err = conn.Exec(ctx, statement)
	return err
}
