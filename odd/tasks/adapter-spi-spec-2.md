# Feature: adapter conformance suites and two adopters (#106, ego-arch-004 spec 2)

Branch: `feat/106-spec2-conformance` · Base: `origin/main` `fd1ae63` (spec 1, #157, merged) · Epic: #10 ·
Issue: #106

Implements [`openspec/changes/ego-arch-004/specs/adapter-conformance/spec.md`](../../openspec/changes/ego-arch-004/specs/adapter-conformance/spec.md)
(spec 2 of 3: slices SPI-3 and SPI-4). Previous: spec 1 (`adapter-spi-boundary`, #157). Next: spec 3
(`adapter-composition`).

## Problem

Spec 1 gave adapters a way to describe themselves (`port/adapter`), but nothing checks that a
descriptor tells the truth or that an adapter follows the lifecycle rules L1–L5 of design §D4. #106
asks for a small reusable conformance suite and for at least two adapter types to use it. The chosen
publisher, `publisher/websocket`, also broke L2: a second `Close` returned the error of closing an
already-closed connection.

## What changes

1. **SPI-3.** Two standard-library-only test packages. `port/adapter/adaptertest` runs AT-1…AT-5 from a
   `Target{Port, Ownership, New, FailStart, Stall, Capabilities}`; `Run` makes each exercised check a
   subtest and returns `[]Result`, and `Capture` runs the same checks without failing the caller so the
   package can prove each check fails against a broken fake. `port/publishing/publishingtest` runs
   PT-1…PT-3 through `RunEvents` and `RunState`. A check is skipped only when a factory returns an error
   matching `adaptertest.ErrUnreachable`; a check the suite cannot run is logged as "not exercised",
   gets no subtest, and is listed in the closing summary.
2. **SPI-4.** `publisher/websocket` makes `Close` idempotent, adds `Describe` to both publishers and
   runs both suites against an `httptest` server. The `testkit` `EventStore`, `DurableStore` and
   `OffsetStore` add `Describe` (`Name: "testkit-memory"`, no capabilities) and run `adaptertest` as
   `Borrowed` next to `persistence/conformance`.

## Scope and constraints

- File ownership: the spec's list (`port/adapter/adaptertest/**`, `port/publishing/publishingtest/**`,
  `publisher/websocket/**` except `closure_test.go`, the three `testkit` stores and their tests), plus
  `CHANGELOG.md`, this document and one doc-comment sentence in `port/adapter/adapter.go` (the #157
  review carry-over, authorized by the coordinator). Not touched: `port/runtime`, `engine.go`,
  `spawn_config.go`, `saga.go`, `supervisor.go`, `compose/`, other publishers, `.github/`, `docs/`.
- Additive in v4; apidiff additions only.
- TDD: strict (user global configuration); runner `go test` (plus `scripts/ci/verify-module.sh` for
  nested modules). Never `-race`, never the workbench.
- Route: delegated direct (one writer; 2+ non-trivial files).
- Delivery strategy: `single-pr` (the coordinator asked for one pull request for the spec; about 1,700
  added lines, most of them the two suites' self-checks and license headers). Five work-unit commits,
  one per task, each reviewable on its own.

## Tasks

- [x] T1 `adaptertest` (SPI-3). Commit `0e929b9`.
- [x] T2 `publishingtest` (SPI-3). Commit `fae6614`.
- [x] T3 Idempotent websocket `Close` (SPI-4). Commit `37a00f6`.
- [x] T4 Websocket adopts the model (SPI-4). Commit `8e6e9d4`.
- [x] T5 `testkit` stores adopt the model (SPI-4). Commit `9d761bf`.
- [x] CHANGELOG and this document. Commit: the docs commit on top.

## Judgement calls

- **How `publishingtest` recognizes `ErrUnreachable`.** `publishingtest` may not import `port/adapter`
  or anything under it, so it cannot call `errors.Is(err, adaptertest.ErrUnreachable)`. The
  `adaptertest.ErrUnreachable` value has an `Unreachable() bool` method, and `publishingtest` skips
  when `errors.As` finds that method reporting true. An adapter wraps the one sentinel and both suites
  skip. Rejected: a second sentinel `publishingtest.ErrUnreachable`, which every adopter would have to
  wrap as well.
- **Which ports imply `CapReady`.** Design §D3 says `CapReady` is implied by every store port and
  never declared there. `adaptertest` cannot import `persistence` or `offsetstore`, so it keeps the four
  store port names as string literals; an internal test pins them to the contract constants. For those
  ports AT-1 skips the `CapReady` direction checks, as V8b does. Rejected: inferring it from
  `Ownership: Borrowed`, which ties a port property to a lifecycle choice.
- **What AT-2 can observe.** A generic suite cannot see whether a failed `Start` freed its resources.
  AT-2 checks the observable half of L1: the `FailStart` value's acquire returns an error within the
  timeout (a hook whose acquire succeeds fails AT-2 as a broken hook); AT-3's failed-acquire case then
  checks that the value is still safe to release twice.
- **"Not exercised" is not a subtest.** A not-exercised check has no `t.Run`, so `go test -v` never
  prints `--- PASS` for it; it is logged and listed in the summary, and `Run`/`Capture` return it as
  `NotExercised`. The adopters assert their exact outcome sets.
- **AT-4 runs last**, because `Stall` has no undo. The probe value built for AT-1 is released before
  any other check runs.
- **testkit also reports AT-2 and the failed-acquire case of AT-3 as not exercised** (no `FailStart`:
  an in-memory `Connect` cannot fail). The spec names only AT-4 for testkit; this is the same rule
  applied honestly.
- **websocket AT-5 is not exercised**: the publishers have no `Ping`, and adding one is outside O5 and
  this spec.
- **`publisher_contract_test.go` folds into PT-1**: its `Publish`-after-stop test is removed and PT-1
  runs it against a publisher that really connected and closed. The file keeps the compile-time
  interface assertions and a comment pointing at PT-1 and at test/compat's sentinel identity check.

## Progress and evidence

**RED, then GREEN.**

| Requirement / scenario | Test | RED observed |
|---|---|---|
| Double close (websocket, L2) | `publisher/websocket/close_test.go` `TestCloseIsIdempotent` (httptest server in `server_test.go`) | on `fd1ae63` code: both publishers' second `Close` = `use of closed network connection` |
| Suites stdlib-only | `adaptertest/architecture_test.go`, `publishingtest/architecture_test.go` (`go list -deps`, allowlists) | temporary `egopb` import in adaptertest -> fails; temporary `port/adapter` import in publishingtest -> fails |
| A lying descriptor fails AT-1 naming `CapStart` | `TestCapture_LyingDescriptorFailsAT1NamingCapStart` | package absent; mutation dropping the `CapStart` check -> fails |
| A capability without a check fails ("no check supplied") | `TestCapture_CapabilityWithoutCheckFailsAT1` | mutation removing the rule -> fails |
| `Target.Capabilities` both directions | `TestCapture_TargetCapabilitiesAreCheckedBothWays` (4 cases) | mutation checking one direction -> fails |
| Only `ErrUnreachable` skips | `TestCapture_OnlyErrUnreachableSkips`; publishingtest `TestCapture_OnlyUnreachableSkips` | mutation skipping on any error -> both fail |
| Not exercised, never passed (no hook; constructor dialing) | `TestCapture_ConstructorAcquireIsNotExercised`, `TestRun_CorrectBorrowedStorePasses` | mutation running the constructor case -> fails |
| Non-idempotent `Close` fails AT-3 | `TestCapture_NonIdempotentCloseFailsAT3` | mutation with a single release -> fails |
| `Close` ignoring the deadline fails AT-4 | `TestCapture_CloseIgnoringTheDeadlineFailsAT4` | mutation that never calls `Stall` -> fails |
| Publisher publishing after `Close` fails PT-1 | `TestCapture_PublishingAfterCloseFailsPT1` | mutation accepting any error -> fails |
| PT-2, PT-3 | `TestCapture_UnstableIDFailsPT2`, `TestCapture_LostEventFailsPT3` | mutations -> fail |
| Websocket adopts, unskipped | `publisher/websocket/conformance_test.go` (exact outcome sets; `TestDescriptors`) | before `Describe`: AT-1 not exercised (undeclared) -> fails |
| testkit adopts as Borrowed, unskipped | `testkit/conformance_test.go` `TestStoresAdapterConformance`, `TestStoreDescriptors` | before `Describe`: AT-1 not exercised -> fails |

Each mutation was applied to a copy of the suite source, the self-checks were run, and the original was
restored (diff-checked).

**No SKIP.** `go test -v -run Conformance` in `publisher/websocket` and in `testkit`: 0 `--- SKIP` lines.
Websocket: AT-1, AT-3 (twice, without acquire), AT-4, PT-1…PT-3 pass for both publishers; AT-2 and the
failed-acquire case of AT-3 "not exercised: acquire happens in the constructor"; AT-5 "not exercised:
the adapter does not implement adapter.Pinger". testkit: AT-1, AT-3 (two cases), AT-5 pass for the
three stores; AT-2, AT-3 failed-acquire and AT-4 "not exercised: no hook".

**Closure.** `publisher/websocket`: `go list -deps -test ./...` (GOWORK=off) lists 263 packages, no
`github.com/tochemey/goakt/v4` package, not the root package, nothing under `compose`; the root-module
packages are `egopb`, `port/adapter`, `port/adapter/adaptertest`, `port/publishing`,
`port/publishing/publishingtest`. `TestUnitTestClosureExcludesRuntimeAndRoot` passes.

**apidiff** (base `fd1ae63` -> head): `port/adapter` no change (doc comment only); `testkit`: compatible,
`(*EventStore).Describe`, `(*DurableStore).Describe`, `(*OffsetStore).Describe` added;
`publisher/websocket`: compatible, `(*EventsPublisher).Describe`, `(*DurableStatePublisher).Describe`
added. `port/adapter/adaptertest` and `port/publishing/publishingtest` are new.

**Checks.** `go test ./port/...` passes. `go run ./internal/cmd/archcheck`: `8 modules checked, 49
packages checked, 200 edges checked, 0 baselined, 0 violation(s), 0 stale entries`.
`ciselect -changed <diff> -base origin/main`: mode `affected`, 8 of 31 root packages, nested modules
`benchmark`, `example/cluster`, `publisher/websocket`, `test/compat`; `scripts/ci/verify-module.sh`
passes for all four (Go 1.26.6 SDK, `GOTOOLCHAIN=local`). `golangci-lint --new-from-rev=origin/main`:
0 issues (root `./port/... ./testkit/...` and `publisher/websocket`). Full root suite: see below.

Full root suite `go test -count=1 ./...` (no `-race`, Go 1.26.6 SDK) on the T5 commit `9d761bf`: exit 0, 27 packages `ok`, no failure.

## Carry-over for spec 3

- The #157 review asked that "a nil or typed-nil value is undeclared" be stated in the accessor and
  suite contracts. Done in the `port/adapter` package doc and the `adaptertest` package doc. Spec 2 owns
  no design text, so the matching sentence in the ego-arch-004 design sketch (§D2, "An adapter that does
  not implement `Describer` is undeclared") is left for spec 3.
- `docs/ci.md` (lines ~616, ~647, ~684) still describes a per-publisher `publisher_contract_test.go`
  that checks `Publish` before `Start`; for websocket that check is now PT-1. Spec 3's extension guide
  should update that text (and F-A will do the same for kafka, nats and pulsar).

## Next step

Open the pull request; then spec 3 (`adapter-composition`).
