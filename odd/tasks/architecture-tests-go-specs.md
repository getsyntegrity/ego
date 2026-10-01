# Architecture and closure tests on go-specs (#205)

## Problem

Epic #205 moves every unit test to go-specs. Eight files on `develop` still do not use `specs.Describe`: the
architecture and closure guards that run `go list -deps` and check the import graph of a package. Three of them
(`port/behavior`, `port/publishing`, `port/runtime`) also use `testify/require`, which the conventions in
`docs/testing/go-specs.md` rule out. The rest use `t.Fatalf` and `t.Errorf` inside hand-written loops.

## What changes

Only `*_test.go` files change. Each guard keeps its top-level `Test` name and its behavior (a real `go list`
subprocess, or a parse of `port.go`), and now sits inside one `specs.Describe`.

- Loops that called `t.Errorf` per offending import became one matcher over the whole list:
  `EveryElement(Satisfy(...))` for "only these are allowed" and `NoElement(Satisfy/Equal)` for "none of these".
  When one fails, go-specs lists the offending packages by index, and the `Satisfy` description carries the same
  reason the old message did (the design section that owns the rule).
- The "this test would prove nothing" guards stay and run first: `ContainAllOf(...)`, `HaveLen(1)`,
  `Not(BeEmpty())`, `Contain(...)`.
- `go list` and parse failures are turned into an `error` value and checked with `BeNil()`, so the `go list`
  output is still in the message.
- `TestContractPackagesDoNotImportAdapter` and `TestPortNameConstantsAreUntyped` use `specs.Table`, one row per
  contract package (the second one replaces the old `t.Run(dir, ...)`). In the second, `portNameConstants`
  returns what `port.go` declares and the case checks typed constants, non-literal constants, the exact
  name-to-value map (`Equal`, which prints a diff) and the existing interfaces (`ContainAllOf`).
- Where one old loop checked two rules (`publishingtest`, `runtimeconsumer`, the test closure of `port/runtime`,
  `migration`), each rule is now its own `It`, so a failure names the rule that broke.
- Subtest names move one level down, as the conventions say. Top-level names do not change, so CI sharding is
  unaffected. `go test -v` lists 20 more cases than before, because `migration` and others now split by rule.

## The `adaptertest` exception

`port/adapter/adaptertest` must stay standard-library plus `port/adapter` in its non-test code. Its guard
(`TestAdaptertestDependsOnlyOnStdlibAndAdapter`) runs `go list -deps .`, which has no `-test`, so it reads only
the production files. The package already imports go-specs in `clock_internal_test.go` and
`implied_internal_test.go`, and `publishingtest` does the same, so the rule never covered test files. That is why
`architecture_test.go` migrated like the other seven. A comment on the test says so.

## What does not change, and why

- **Production code and `go.mod`.** Nothing outside `*_test.go` changes.
- **The `go list` subprocess and the `port.go` parser.** They are the point of these guards and are not an
  external resource beyond the toolchain that already runs the tests. They stay.
- **Allowlists.** Same entries, same meaning. `allowedFirstParty` in `runtimeconsumer` became `[]any` only to
  pass it to `BeOneOf`.

## Constraints

- Strict TDD. The runner is `go test` on the touched packages. RED is a deliberate production mutation (a
  scratch file that adds a forbidden import, or an edit to `port.go`), then reverted.
- No `-race`, no workbench, no external resources.
- `develop` does not compile `./migration` tests right now (`connectedSnapshotStore` is declared twice after
  #227 and #228). #250 fixes it. To run the `migration` guard I applied that PR's version of
  `migration/tenant_adoption_test.go` locally and did not commit it. CI for `migration` stays red here until
  #250 merges.

## Tasks

- [x] T1 `internal/runtimeconsumer/closure_test.go`, `migration/closure_test.go`. Route: inline (two small
      files, one pattern). Commit `8669681`. RED: a scratch file importing `engine` and `persistence` failed
      all three rules (`NoElement: 1 of 541 elements matched — [538] .../engine`, the GoAkt list, and
      `EveryElement: 19 of 27 elements failed`); in `migration`, importing `engine` failed `.../never_reaches_the_engine_package`.
- [x] T2 `port/adapter/adapter_architecture_test.go` (3 tests) and `adaptertest/architecture_test.go`. Route:
      inline. Commit `092fbf5`. RED: `port/adapter` importing `github.com/google/uuid` failed with
      `EveryElement: 1 of 116 elements failed — [114] github.com/google/uuid`; `tenancy` importing
      `port/adapter` failed `.../tenancy` and `.../persistence`; in `tenancy/port.go`, a typed constant
      (`expected [PortTenantResolver] to be empty`), a wrong value and an extra constant (`Equal` diffs), a
      non-literal constant and `TenantResolver` no longer an interface (`missing [TenantResolver]`) each failed;
      `adaptertest` importing `uuid` failed.
- [x] T3 `port/behavior`, `port/publishing`, `publishingtest`, `port/runtime`. Route: inline. Commit `bb36eee`.
      RED: `uuid` in `port/behavior` and `port/publishing`, `persistence` in `port/runtime` (`egopb` rejected)
      and `port/adapter` in `publishingtest` all failed the allowlist; a scratch `_test.go` importing `engine`
      failed both `port/runtime` test-closure cases.
- [x] T4 Verify and deliver. Route: inline. `go build ./...`, `go vet`, `golangci-lint run` (0 issues) and
      `gofmt -l` are clean. `go test -count=5` passes on every touched package. Coverage before and after is
      identical: `port/adapter` 95.0%, `adaptertest` 94.1%, `publishingtest` 87.0%, `port/runtime` 100.0%,
      `migration` 82.8%.

## Follow-up spec

None. Waiting on #250 only for the `migration` package to compile on `develop`.

## Progress

- 2026-09-30: T1 to T4 done and committed. Engram mirror: pending (not available to this writer).
