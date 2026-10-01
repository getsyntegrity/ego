# Drop testify from testkit and persistence/conformance (STOPPED)

Origin: #205. Status: **blocked by the STOP rule. No code changed, no PR opened.**

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

## Tasks

None executed (blocked before the first write).

## Progress

- [x] Mapped every testify use and exported symbol.
- [x] Confirmed the leak and the apidiff break empirically.
- [ ] Implementation: waiting for an owner decision on the options above.

## Follow-up (named)

`testkit-enginetest-drop-testify` (option 2) and
`conformance-testing-t-breaking` (option 1, major).
