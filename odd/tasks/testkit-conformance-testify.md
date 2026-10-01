# Drop testify from testkit and persistence/conformance (STOPPED)

Origin: #205. Status: **option 2 approved and implemented; `persistence/conformance` split out as `conformance-testify-major`.**

## Problem

Three non-test library packages still import `github.com/stretchr/testify`:
`testkit/scenario.go`, `persistence/conformance/*.go` (check, events, state,
snapshot, helpers) and `internal/engine/enginetest/failing_behavior.go`.
The first two are public helpers that users call from their own tests, so
testify ends up in every user's dependency graph (`go list -deps` of both
packages lists `testify/assert` and `testify/require`; go-specs is NOT a
dependency of either, so it is not an option here).

## Analysis

### 1. Exported API that touches testify

| Package | Exported symbol | Touches testify how |
|---|---|---|
| `testkit` | `EventSourcedScenarioResult.ThenEvents/ThenState/ThenError/ThenNoEvents` (and the `When` arrangement check) | internal calls only; parameter is `testing.TB` |
| `testkit` | `DurableStateScenarioResult.ThenState/ThenVersion/ThenError` | internal calls only; parameter is `testing.TB` |
| `persistence/conformance` | `Check[S].Run` (exported field) | **type in the signature: `func(ctx, require.TestingT, S)`** |
| `persistence/conformance` | `EventsStoreChecks`, `StateStoreChecks`, `SnapshotStoreChecks` (exported vars of `[]Check[...]`) | inherit the leak through `Check` |
| `persistence/conformance` | `RunEventsStoreConformance`, `RunStateStoreConformance`, `RunSnapshotStoreConformance` (take `*testing.T`), `Capture*StoreChecks`, `CheckResult`, `Lifecycle` | internal calls only |
| `internal/engine/enginetest` | `FailingHandleEventBehavior.HandleEvent` returns `assert.AnError` | internal package, not public API |

### 2. Do testify types leak into the exported API?

Yes, in exactly one place: `conformance.Check.Run` is typed with
`github.com/stretchr/testify/require.TestingT`. Everything else is stdlib
(`testing.TB`, `*testing.T`) or internal.

I verified the consequence rather than guessing: I changed `Run` to a local
`type T = interface{ Errorf(string, ...interface{}); FailNow() }`, built, and ran
`apidiff` against a baseline. Result:

```
Incompatible changes:
- ./persistence/conformance.Check.Run: changed from func(context.Context,
  github.com/stretchr/testify/require.TestingT, ...StateStore) to func(context.Context, T, ...StateStore)
```

Even an alias with an identical method set is flagged, because `require.TestingT`
is a named type and the alias target is an unnamed interface literal. It would
also break user code that assigns a `func(ctx, require.TestingT, S)` to `Run`.
(The experiment was reverted; the working tree is clean.)

### 3. What callers observe on failure today

`testkit` (testify semantics, via `testing.TB`):

- `require.*` = fatal (`t.FailNow`): `NoError` on arrangement/processing error,
  `Len` on event count, `Error` when an error was expected. Testify formats the
  message as `Error Trace / Error / Messages` blocks.
- `assert.*` = non-fatal (`t.Errorf`): `True(proto.Equal)` on event and state
  mismatch, `Equal` on version, `Contains` on error substring, `Empty` on
  "no events".
- Testify's `Equal`/`Contains`/`Empty` messages include `expected:`/`actual:`
  dumps and a diff. A stdlib rewrite can keep fatal-vs-nonfatal exactly and
  carry the same custom messages, but the diff/dump layout would change.

`persistence/conformance`:

- Checks call `require.*` (fatal) through `Check.Run`. Normal path: `*testing.T`
  per subtest, so a failure ends that subtest. Captured path:
  `captureTestingT` implements `Errorf`+`FailNow` (`FailNow` = `runtime.Goexit`)
  and stores formatted messages in `CheckResult.Errors`. Those strings are
  observable by callers and carry testify's block format.

## Decision

Per the STOP rule: removing testify from `persistence/conformance` requires
changing the exported `Check.Run` signature (confirmed incompatible by apidiff),
so I did **not** implement it and opened no PR.

### Options (for the owner to choose)

1. **Accept a breaking change in conformance** (pre-1.0 / next major): type
   `Run` as `func(context.Context, *testing.T-like local interface, S)`, drop
   testify with stdlib `t.Fatalf`. Needs `release:major` on a PR to main and a
   CHANGELOG entry. Cleanest end state.
2. **Split the spec**: do `testkit/scenario.go` and
   `internal/engine/enginetest/failing_behavior.go` now (no exported signature
   changes; `assert.AnError` becomes a package-level `errors.New` sentinel in
   an internal package; testkit keeps `Fatalf` vs `Errorf` semantics, with
   message text becoming a short plain `expected/actual` form, which is a
   release note "failure message format changed"). Leave `conformance` for
   option 1. Note `testkit` itself is public, so the changed failure text is
   the only visible effect.
3. **Keep `conformance` as is** and document that it depends on testify
   (zero risk, keeps the dependency).

Recommendation: option 2 now, option 1 at the next major window. I held back
even option 2 because the instruction was to stop and report when any part
needs an exported change; say the word and I will do it as a separate PR
(about 3 tasks: enginetest sentinel, testkit require/assert rewrite with RED
mutation proof, release note describing the changed message layout).

## Approved scope

The coordinator approved option 2 (the user later added: nothing may keep
testify). This PR covers `testkit/scenario.go`, `internal/engine/enginetest/failing_behavior.go`
and the two `example/cluster` test files. `persistence/conformance` is NOT touched here:
it was the breaking change above and landed separately in #262
(named follow-up `conformance-testify-major`, done).

Rules kept: fatal stays fatal (testify's `require` was `Errorf` then `FailNow`;
the new `failNow` helper does exactly that), non-fatal stays `Errorf`. No exported
signature changed (`api-check.sh origin/develop` reports no API changes).

Message layout that users of `testkit` see now (testify printed `Error Trace`,
`Error`, `Messages` blocks with diffs; now one line):

| Assertion | New message |
|---|---|
| arrangement failed (fatal) | `scenario arrangement failed: <err>` |
| command returned error (fatal) | `command processing returned an error: <err>` |
| wrong event count (fatal) | `unexpected number of events: expected N, got M` |
| expected error, got none (fatal) | `expected an error but got none` |
| event / state mismatch | unchanged text (`event at index i: expected .., got ..`, `state mismatch: ..`) |
| error substring (non-fatal) | `error "<err>" does not contain "<sub>"` |
| unexpected events (non-fatal) | `expected no events but got N` |
| version (non-fatal) | `version mismatch: expected N, got M` |

## Tasks

- [x] T1 testkit: failure-contract tests, then stdlib rewrite. Route: inline
  (one source file plus one new test). RED: new `testkit/scenario_failure_test.go`
  failed 10 of 16 rows against testify (for example `Messages:   unexpected number
  of events` vs the expected `unexpected number of events: expected 0, got 1`);
  GREEN after the rewrite; `go test -count=5 ./testkit` ok; coverage 94.5% before and after.
  Commit: `a579cd8`.
- [x] T2 enginetest: `assert.AnError` becomes the package sentinel `ErrHandleEvent`
  (internal package; no caller compared the error). Commit: `febf8bc`.
- [x] T3 example/cluster tests: superseded. #260 moved them to go-specs on develop,
  so this PR takes develop's versions and only runs `go mod tidy` there (testify
  disappears from `example/cluster/go.mod`). My earlier stdlib-helper migration was dropped.
- [x] T4 `go mod tidy` in `benchmark` and `example/cluster` (CI `tidy` job).

## Progress

All three tasks done. Checks: `go build ./...`, `go vet`, `gofmt -l` clean,
`golangci-lint` 0 issues on the touched root packages; `example/cluster` lint shows
8 errcheck issues that already existed (main.go and `store.Disconnect` defers).

## Follow-up (named)

`conformance-testify-major`: change `conformance.Check.Run` off `require.TestingT`
(breaking, needs `release:major`); branch `feat/conformance-without-testify`.
