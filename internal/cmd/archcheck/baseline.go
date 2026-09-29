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

package main

import "github.com/getsyntegrity/ego/internal/cmd/archcheck/rules"

// repoBaseline is the repository's current, explicit set of known
// violations (odd/tasks/arch-boundary-check.md, "Baseline at the start").
// Every entry names an owner, a justification for why the violation exists
// today and the criterion that removes it; ValidateBaseline enforces that
// shape, and Evaluate reports an entry that stops matching a real
// violation as stale, so this list can only shrink.
//
// It is empty as of S4-1 (#147, ego-arch-001 §3): the last entry,
// migration -> ego (application-no-runtime), was removed once migration
// stopped importing package ego for ego.ResolveLogger and switched to the
// runtime-free internal/logging package instead. Evaluate and ValidateBaseline
// both accept an empty (or nil) baseline; see
// TestValidateBaseline_EmptyBaselineIsValid and
// TestEvaluate_EmptyBaselineOnCleanGraphReportsZero in rules/evaluate_test.go.
var repoBaseline = []rules.BaselineEntry{}
