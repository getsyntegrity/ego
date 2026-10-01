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

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/offsetstore"
)

// OffsetStore implements offsetstore.OffsetStore using PostgreSQL.
type OffsetStore struct {
	pool *pgxpool.Pool
	dsn  string
}

var _ offsetstore.OffsetStore = (*OffsetStore)(nil)

// NewOffsetStore creates a new PostgreSQL-backed offset store.
func NewOffsetStore(dsn string) *OffsetStore {
	return &OffsetStore{dsn: dsn}
}

func (s *OffsetStore) Connect(ctx context.Context) error {
	cfg, err := pgxpool.ParseConfig(s.dsn)
	if err != nil {
		return fmt.Errorf("offset store config: %w", err)
	}
	cfg.MaxConns = 20
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("offset store connect: %w", err)
	}
	s.pool = pool
	return nil
}

func (s *OffsetStore) Disconnect(_ context.Context) error {
	if s.pool != nil {
		s.pool.Close()
	}
	return nil
}

func (s *OffsetStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *OffsetStore) WriteOffset(ctx context.Context, offset *egopb.Offset) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO offsets_store (projection_name, shard_number, current_offset, timestamp)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (projection_name, shard_number)
		DO UPDATE SET current_offset = EXCLUDED.current_offset, timestamp = EXCLUDED.timestamp`,
		offset.GetProjectionName(),
		offset.GetShardNumber(),
		offset.GetValue(),
		offset.GetTimestamp(),
	)
	return err
}

func (s *OffsetStore) GetCurrentOffset(ctx context.Context, projectionID *egopb.ProjectionId) (*egopb.Offset, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT projection_name, shard_number, current_offset, timestamp
		FROM offsets_store
		WHERE projection_name=$1 AND shard_number=$2`,
		projectionID.GetProjectionName(),
		projectionID.GetShardNumber(),
	)

	var offset egopb.Offset
	err := row.Scan(
		&offset.ProjectionName,
		&offset.ShardNumber,
		&offset.Value,
		&offset.Timestamp,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &offset, nil
}

func (s *OffsetStore) ResetOffset(ctx context.Context, projectionName string, value int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE offsets_store SET current_offset=$1 WHERE projection_name=$2`,
		value, projectionName)
	return err
}
