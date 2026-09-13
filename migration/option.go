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

package migration

import kitlog "github.com/pablogore/kit-logger/pkg/logger"

// Option configures the Migrator.
type Option interface {
	apply(m *Migrator)
}

type optionFunc func(m *Migrator)

func (f optionFunc) apply(m *Migrator) { f(m) }

// WithPageSize sets the number of persistence IDs fetched per page
// and the batch size for replaying events per entity.
// Default is 500.
func WithPageSize(size uint64) Option {
	return optionFunc(func(m *Migrator) {
		m.pageSize = size
	})
}

// WithLogger sets the kit-logger Logger used during migration, the same
// logging seam the engine uses. When the option is not used, or when the
// given logger is nil or a typed-nil pointer, the migrator logs through
// ego.DefaultLogger().
func WithLogger(logger kitlog.Logger) Option {
	return optionFunc(func(m *Migrator) {
		m.logger = logger
	})
}
