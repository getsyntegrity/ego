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

package sample

import (
	"database/sql"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestSQLOpen(t *testing.T) {
	db, _ := sql.Open("pgx", "postgres://localhost")
	_ = db
}

func TestUsesPgx(t *testing.T) {
	_, _ = pgx.Connect(nil, "postgres://localhost")
}

func TestSkipsWithoutDSN(t *testing.T) {
	dsn := os.Getenv("SAMPLE_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("SAMPLE_POSTGRES_DSN is not set")
	}
}

func TestSkipsThroughHelper(t *testing.T) {
	openDB(t)
}

func TestSkipOnOptionalFlag(t *testing.T) {
	if os.Getenv("SAMPLE_VERBOSE") == "" {
		t.Skip("verbose only")
	}
}
