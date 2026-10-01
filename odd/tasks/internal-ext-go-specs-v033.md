# internal/extensions and internal/goaktlog tests on go-specs v0.3.3 (#234)

## Problem

PR #234 (branch `test/205-migrate-internal-ext`) moved the unit tests of `internal/extensions` and
`internal/goaktlog` to go-specs v0.3.1. They still carry things the v0.3.3 conventions
(`docs/testing/go-specs.md`) replace:

- `extensions_test.go` checks `== nil` and pointer identity with `BeTrue`, and uses `len(...) ToEqual(0)`.
- `goaktlog/adapter_test.go` verifies the flush contract through `kitlogtest.MockLogger` counters
  (`FlushCalls`, `ShutdownCalls`), and proves "formatting was skipped" through a hand-written `stringerSpy`
  with a `called` flag.
- `capture.last` calls `t.Fatalf`, which bypasses the spec.
- Several `for _, tt := range tests { s.It(...) }` loops, `strings.HasSuffix`/`strings.Contains` with `BeTrue`,
  and a `hasSubsystem` boolean stand in for tables and matchers.

## What changes

Only `internal/extensions/extensions_test.go` and `internal/goaktlog/adapter_test.go`.

- `extensions_test.go`: `BeNil`, `BeEmpty`, and a `beTheSamePointer` matcher (identity, with a readable failure).
- `adapter_test.go`: `stringerSpy` becomes `stringerMock` over `mock.Controller` (`Times(0)` when the level is
  disabled, `Times(1)` when enabled). `kitlogtest.MockLogger` becomes `managedBackend`, a real discarding
  kit-logger whose `Flush`/`Shutdown` go through a controller (`Flush` once, `Shutdown` never). `capture.last`
  asserts through the spec. The loops become `specs.Table`. Matchers: `HavePair`, `HaveKey`, `BeEmpty`,
  `EndWith`, `Contain`.

## What does not change, and why

- **Production code.** Untouched.
- **The extension wrapper tests stay one `Test` per type.** They differ by type, so a generic table would be less
  readable than the current cases. Names are kept.
- **`capture` and `newCaptureLogger`.** A real kit-logger with a recording sink is the genuine pipeline under test,
  not a stand-in for an external dependency (the doc allows in-process real implementations).
- **`panicked`.** v0.3.3 has no panic matcher, so the helper stays.
- **Real-time waits.** There are none in either package.

## Constraints

- Every case stays, subtest names stay: 37 + 73 `--- PASS` before and after, names identical.
- No force-push (develop merged in), no `-race`, no workbench. TDD runner: `go test ./internal/extensions ./internal/goaktlog`.

## Tasks

- [x] T1 Merge `origin/develop` (v0.3.3). Route: inline. Evidence: merge `d291398`; `go mod tidy` clean, build clean.
- [x] T2 `extensions_test.go` matchers. Route: delegated writer. Evidence: `94a0f51`. RED mutations caught:
      `Get` returning a copy (`expected &{...} to satisfy "be the same pointer as the registered value"`),
      `NewEventAdapters(nil)` turned into an empty slice (`expected nil, got []`), `MarshalBinary` returning
      empty non-nil bytes (`expected nil, got []`).
- [x] T3 `goaktlog/adapter_test.go`: controllers, tables, matchers. Route: same writer. Evidence: `e7e986a`.
      RED mutations caught: `Flush` also calling `Shutdown` (`mock: unexpected call Shutdown(...)`), and `Infof`
      formatting before the level gate (the `Infof` and `drops every disabled record` cases fail).
- [x] T4 Verify and deliver: vet, golangci-lint (0 issues), gofmt, `-count=5`, coverage `extensions` 86.8% and
      `goaktlog` 96.4%, both unchanged. Then push and update the PR body.

## Follow-up spec (not in this document)

None needed for these packages.

## Progress

- 2026-09-30: T1-T4 done. Engram mirror: not written by this writer.
