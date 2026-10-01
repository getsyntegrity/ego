# Unit-test gate (#204, parent epic #201)

## Problem

Epic #201 says a unit test never uses testify, never uses the generated `mocks/*` packages, always uses
go-specs, and never reaches a real resource. Today nothing enforces it, so a migrated package can regress the
moment someone adds a `require` import. Issue #204 asks for an automated gate with justified exceptions and
positive and negative cases.

## What changes

A small Go program, `.github/scripts/unitgate`, scans every `.go` file of every module in the repository (it
walks the whole tree, so nested modules such as `publisher/kafka` are included) and fails on:

1. any import of `github.com/stretchr/testify/...`, in test and non-test files;
2. any import of `github.com/getsyntegrity/ego/mocks/...` from a file outside `mocks/`;
3. a `*_test.go` file that declares a `func TestXxx(t *testing.T)` and never calls `specs.Describe`
   (fuzz targets, benchmarks and `TestMain` are not tests in this sense);
4. a test file that reaches a real resource: `sql.Open`, `net.Dial*`/`net.Listen*`, `exec.Command*`,
   `httptest.NewServer*`, a real goakt `NewActorSystem`, `os.Create` in a file with no `TempDir`, or an
   `os.Getenv("...DSN...")`.

Two plain-text lists hold the exceptions, one entry per line as `path | note`:

- `.github/unit-test-gate-pending.txt`: the temporary list. Files that still violate 1-3 because an open PR is
  migrating them. Each line names the owning PR. An entry whose file no longer violates (migrated, or deleted)
  is itself an error, so the list can only shrink.
- `.github/unit-test-gate-resources.txt`: the permanent list for rule 4. Tests that are legitimately outside the
  unit lane, each with a one-line reason. A stale entry is an error here too.

The gate runs as a new `unit-gate` job in the existing `.github/workflows/ci.yml` and is listed in `ci-ok`.
No new workflow file.

## What does not change, and why

- No golangci-lint `depguard`: it only sees one package at a time per config and cannot express rule 3 or the
  stale-entry check, and the repo would need a second tool to cover nested modules. One program covers all four
  rules with one allowlist format. Rejected alternative: depguard for rule 1 plus the program for the rest.
- The generated `mocks/` directory itself imports testify (mockery output). It is listed once, as a directory
  entry, in the pending list; it goes away when the last consumer is migrated.
- No test is moved, tagged or skipped, and no existing job changes (epic #201 rules).

## Constraints

Strict TDD (go-specs v0.3.3, runner `go test`), no `-race`, no workbench, no testify in the new tests. The
program's own tests use an in-memory `fstest.MapFS`, so they touch no disk.

## Tasks

Route for every task: direct inline (one small package, no delegation trigger fired). TDD: go-specs v0.3.3,
runner `go test`, source: the standing rule of epic #201.

- [x] T1 Scanner for rules 1-3 with positive and negative fixtures. RED: stub `Scan` returned nothing, 6
  fixtures failed; GREEN after the implementation. Commit 8cf3f00.
- [x] T2 Resource rule (4). RED: stub returned nothing, 9 cases failed; GREEN after. Commit 5502d26.
- [x] T3 Allowlist parsing, stale-entry detection and the command line. RED: stubs, 15 cases failed; GREEN
  after; REFACTOR: tests share one in-memory tree helper. Commit 2e18227.
- [x] T4 Real allowlists computed from `origin/develop` (41 pending entries including the `mocks/` directory,
  47 resource entries), `unit-gate` job in `ci.yml` and listed in `ci-ok`, docs section. RED on the real
  repository: a testify import added to `internal/queue/queue_test.go` made the gate exit 1 with
  `internal/queue/queue_test.go: testify: imports github.com/stretchr/testify/require`; a pending line for a
  clean file exited 1 with `stale pending entry`; both reverted. Commit c044c21.

## Follow-up

When the PRs listed in the pending file merge, each removes its own lines. The last one deletes the file and
the `mocks/` directory. Not in this spec.

## Progress

All four tasks done. `go test -count=5` passes, coverage of the new package is 86.1%, `golangci-lint` reports 0
issues on it, `actionlint` is clean on `ci.yml`. No existing package changed, so no existing coverage moved.
The pending list names the owners found on `origin/develop` when it was computed: #245, #246, #264, and, for
files with no open PR yet (engine remainder after #252, eventsource snapshots), the work that is still pending.
Delivery strategy: single PR, about 700 authored lines, most of them tests and the two data files.
