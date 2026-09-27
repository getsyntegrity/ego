# Feature: fix the residual unchecked PreStart extension assertion (#99)

Branch: `fix/99-prestart-assertion-audit` · Base: `origin/main` `77beda6` · Issue: #99

## Problem

Issue #99 was originally about `eventsWriterActor.PreStart` panicking with an unrecoverable
"interface conversion" error whenever a *required* extension was missing, crashing the whole
process because `goakt` drives `Spawn`/`SpawnChild` through a
`golang.org/x/sync/singleflight.Group` that deliberately re-panics a recovered panic on a fresh,
unrecoverable goroutine (see `extension_lookup.go`'s doc comment). PR #100 (merged as `930b097`)
fixed every *required* lookup with a new `requireExtension[T]` helper, but explicitly left the
nil-checked, *optional* lookups alone, on the stated assumption that "they were never the unsafe
pattern". Issue #99 was reopened because (a) the root cause of the original nil-during-spawn race
was never established, and (b) it asked to audit the remaining `PreStart` paths.

That audit (see below) found that the "optional" lookups are not actually safe: they nil-check
before asserting, but the assertion itself (`ext.(*extensions.SnapshotStoreExt)`,
`ext.(*extensions.EncryptorExtension)`, etc.) is still a single-value, unchecked type assertion.
A *present* extension registered under the right ID but the wrong concrete type — for example a
wiring bug that registers the wrong extension object under an existing ID — still panics and still
crashes the whole process the same way the original bug did, just on a narrower trigger condition
(type mismatch instead of "missing").

## What changes (this slice)

- `extension_lookup.go`: new `optionalExtension[T any](ctx, extensionID) (T, error)`. Unlike
  `requireExtension`, a missing registration returns the zero value and a nil error (matches the
  actor's existing "optional" semantics). A present-but-mismatched-type registration returns an
  error wrapping the existing `ErrMissingRequiredExtensions` sentinel — reusing
  `requireExtension`'s own mismatch-branch semantics rather than inventing a new exported error.
- `snapshots_writer_actor.go`: `snapshotsWriterActor.PreStart` uses `optionalExtension` for both
  `extensions.SnapshotStoreExtensionID` and `extensions.EncryptorExtensionID`, and now returns the
  error instead of letting the runtime panic.
- Tests: `extension_lookup_test.go` (`TestOptionalExtension`, three branches: missing, mismatched,
  correct) and `snapshots_writer_actor_test.go` (two new `TestSnapshotsWriterActor` subtests that
  register a fake `extension.Extension` — `mistypedExtension`, whose `ID()` collides with a real
  extension slot — to reproduce the panic before the fix and the returned error after).
- `CHANGELOG.md`: `### 🐛 Bug Fixes` entry under `[Unreleased]`.

## Why this slice only

File ownership for this task was scoped to `snapshots_writer_actor.go` plus `extension_lookup.go`
and their tests. The audit below found the identical unchecked-assertion pattern in four more
files outside that scope (`projection_actor.go`, `event_sourced_actor.go`,
`events_janitor_actor.go`, `durable_state_actor.go`). Fixing all of them in one PR would mean five
non-trivial production files plus their test files in a single change, and other agents may be
touching root actor/test files concurrently (the task briefing flags #112 activity on root
`*_test.go` files). Per "one slice per PR" and the file-ownership guardrail, those four files are
left for an explicit follow-up slice rather than fixed here silently. See **Audit results** below
for the exact list; a human/orchestrator decision is needed on whether to open that follow-up now
or fold it into #99 directly.

## Root cause of the original nil-during-spawn race — not established (unchanged from PR #100)

Re-verified, did not re-attempt reproduction beyond a source read (timeboxed per task
instructions): `go.mod` still pins `github.com/tochemey/goakt/v4 v4.5.4` (same version #100
investigated). Confirmed in the vendored source at that pinned version:

- `actorSystem.shutdown()` runs `x.reset()` (which calls `x.extensions.Reset()`, wiping every
  registered extension) in an unconditional `defer` at the top of `shutdown`
  (`actor_system.go:3205-3220`).
- `Spawn`/`SpawnChild` go through `x.spawnActivation` and `x.grainActivation`, both
  `golang.org/x/sync/singleflight.Group` (`actor_system.go:1104,1114`), confirming the
  crash-the-whole-process propagation mechanism `requireExtension`'s doc comment describes.

This confirms PR #100's evidence still holds on this codebase; it does not newly prove *why* an
extension goes missing during a live spawn. No reproduction was attempted (PR #100 already ran the
suite 3×, ~580s each, with no repro); this task's root-cause investigation stayed at source
re-verification, per the "timebox root-cause investigation; if not established, record evidence
and hypotheses, do not guess a fix" instruction.

## Audit results — every `ctx.Extension(...)` / `sys.Extension(...)` call, root package

Command: `rg -n '\.Extension\(' --type go` (non-test files) plus manual review of the `ext != nil`
branches for an unchecked assertion.

| File:line | Extension ID | Pattern | Assessment |
|---|---|---|---|
| `snapshots_writer_actor.go:93-97` | `SnapshotStoreExtensionID`, `EncryptorExtensionID` | nil-checked, then unchecked `.( *T)` | **Fixed in this PR** |
| `projection_actor.go:111-127` | `EventAdaptersExtensionID`, `EventsStreamExtensionID`, `EncryptorExtensionID`, `TelemetryExtensionID` | nil-checked, then unchecked `.( *T)` (4 sites) | Same defect class — **not fixed here, out of scope** |
| `event_sourced_actor.go:381-395` | `SnapshotStoreExtensionID`, `EventAdaptersExtensionID`, `EncryptorExtensionID`, `TelemetryExtensionID` | nil-checked, then unchecked `.( *T)` (4 sites) | Same defect class — **not fixed here, out of scope** |
| `events_janitor_actor.go:80-82` | `SnapshotStoreExtensionID` | nil-checked, then unchecked `.( *T)` | Same defect class — **not fixed here, out of scope** |
| `durable_state_actor.go:169-171` | `TelemetryExtensionID` | nil-checked, then unchecked `.( *T)` | Same defect class — **not fixed here, out of scope** |
| `event_sourced_actor.go:302`, `durable_state_actor.go:146`, `saga_actor.go:153` | `TenancyExtensionID` | `!= nil` used only as a bool, no assertion | Safe |
| `engine.go:308` | any (config validation) | `== nil` used only as a bool, no assertion | Safe |
| `engine.go:465` | `ProjectionExtensionID` | comma-ok `ext.(*T)` (`, ok :=`) | Already safe |
| `extension_lookup.go` | any | `requireExtension`/`optionalExtension` themselves | Safe by construction |

No `ctx.Dependency(...)` calls exist in the codebase (`rg -n 'ctx\.Dependency\(' --type go` — no
matches).

## Constraints

- TDD: strict, from the user's global configuration; runner `go test` (no `-race` locally, per
  repository policy).
- Route: direct — one bounded writer agent, two production files plus their tests, already
  understood after the audit above.
- No new exported error: `optionalExtension` reuses `ErrMissingRequiredExtensions`.

## Tasks

- [x] **T1** Re-verify goakt v4.5.4 shutdown/reset/singleflight evidence from PR #100 (timeboxed).
  Check: source re-read only, documented above; no reproduction attempted.
- [x] **T2** Audit every `ctx.Extension(...)` call in the root package for the unchecked-assertion
  pattern. Check: table above, cross-checked against `rg -n '\.Extension\(' --type go`.
- [x] **T3** RED: add `mistypedExtension` plus two new `TestSnapshotsWriterActor` subtests that
  register it under `SnapshotStoreExtensionID` / `EncryptorExtensionID`. Check: `go test -run
  TestSnapshotsWriterActor .` crashes the whole test binary with `panic: interface conversion:
  extension.Extension is *ego.mistypedExtension, not *extensions.SnapshotStoreExt` at
  `snapshots_writer_actor.go:94`, propagating through `singleflight.(*Group).doCall`.
- [x] **T4** GREEN: add `optionalExtension[T]` to `extension_lookup.go`; rewrite
  `snapshotsWriterActor.PreStart` to use it and return its error. Check: same command now passes,
  10/10 subtests.
- [x] **T5** Add a direct unit pin for `optionalExtension` (`TestOptionalExtension`, three
  branches) in `extension_lookup_test.go`. Check: `go test -run TestOptionalExtension .` passes.
- [x] **T6** `gofmt`, `go vet`, `go run ./internal/cmd/archcheck` before/after, `golangci-lint`
  (best effort — see evidence: pre-existing environment defect, not caused by this change).
  Check: recorded below.
- [x] **T7** `ciselect` scope + full root suite + nested-module verification (this change touches
  the root package, so `ciselect` selects full mode).
  Check: recorded below.
- [x] **T8** `CHANGELOG.md` + this document.
  Check: structural readback.

## Acceptance criteria

1. `snapshotsWriterActor.PreStart` returns an error instead of panicking when
   `SnapshotStoreExtensionID` or `EncryptorExtensionID` is registered under an unexpected
   concrete type; a missing registration keeps returning `nil` (unchanged optional behavior).
2. No new exported API; `ErrMissingRequiredExtensions` is reused, not redefined.
3. `archcheck` reports the same baseline as `main` (0 new violations, 0 stale entries).
4. Every audit finding above is recorded with file:line, even the ones left unfixed.

## Progress and evidence

**T1-T2 — done.** See sections above.

**T3 — RED, done.** `go test -run TestSnapshotsWriterActor -v -timeout 60s .` (pre-fix):

```
=== RUN   TestSnapshotsWriterActor/returns_an_error_instead_of_panicking_when_the_snapshot_store_extension_is_registered_with_an_unexpected_type
panic: interface conversion: extension.Extension is *ego.mistypedExtension, not *extensions.SnapshotStoreExt

	golang.org/x/sync/singleflight.(*Group).doCall.func2.1()
	github.com/pablogore/ego/v4.(*snapshotsWriterActor).PreStart(...)
		snapshots_writer_actor.go:94
	github.com/tochemey/goakt/v4/actor.(*PID).init.func1(...)
	...
FAIL	github.com/pablogore/ego/v4	17.085s
```

**T4-T5 — GREEN, done.** `go test -run TestSnapshotsWriterActor -v -timeout 60s .`: 10/10 subtests
pass, 23.1s. `go test -run TestOptionalExtension -v -timeout 30s .`: 3/3 subtests pass.

**T6 — done, with one honest environment caveat.**

- `gofmt -l` on the four changed/added files: clean.
- `go vet ./...`: clean.
- `go run ./internal/cmd/archcheck` before AND after this change:
  `archcheck: 15 packages checked, 70 edges checked, 1 baselined, 0 violation(s), 0 stale entries`
  — identical to `main`, no new baseline entries.
- `golangci-lint run --new-from-rev=origin/main` (local, v2.13.1 built with go1.26.6) and
  `make docker-lint` (CI image, v2.12.2 built with go1.26.2, Go 1.27.0 stdlib) both fail with the
  same error, unrelated to any file this PR touches:
  `crypto/internal/randutil/randutil.go:11: could not import math/rand/v2 (.../math/rand/v2/rand.go:213:17: method must have no type parameters)`.
  Bisected: this reproduces byte-for-byte on a clean `origin/main` checkout with no changes at all
  (verified with `git stash` + rerun, both locally and via `make docker-lint`), so it predates this
  change entirely — a Go 1.27 stdlib/`golangci-lint` typechecker incompatibility in the local and
  CI toolchains, not something this PR introduced or can fix. Flagging for a human decision (see
  report to orchestrator); no lint run passed cleanly for this change.

**T7 — done.** `ciselect -changed <this PR's 4 files>` → mode `full` (root-package file changed),
selecting all 23 root/internal packages plus nested modules `benchmark`, `example/cluster`,
`publisher/kafka`, `publisher/nats`, `publisher/pulsar`, `publisher/websocket`.

- Root: `go test -count=1 ./...` (no `-race`) — see report to orchestrator for the exact result;
  ran once as required for a root-package change.
- `benchmark`: `scripts/ci/verify-module.sh benchmark` — full pass (build, vet, lint 0 issues,
  `go test`: no tests to run).
- `example/cluster`: build + vet pass; lint hits the same pre-existing environment defect as T6;
  `go mod tidy -diff` (GOWORK=off): no diff; `go test ./...` (GOWORK=off): ok.
- `publisher/kafka`, `publisher/nats`, `publisher/pulsar`, `publisher/websocket`: build + vet +
  `go mod tidy -diff` (no diff) + `go test ./...`: ok for all four (lint not separately re-run
  beyond the root check above — same pre-existing defect would apply).

**T8 — done.** `CHANGELOG.md` `### 🐛 Bug Fixes` entry under `[Unreleased]`; this document.

**Review:** RDD status not checked from this writer agent's scope — left to the orchestrator per
the delegation contract; this document records the writer-level verification evidence above.

**Next step:** orchestrator decision on (a) the pre-existing lint-environment defect (report or
ignore), (b) whether the four out-of-scope audit findings become a follow-up PR now or wait, and
(c) whether #99 can close after this PR or must stay open for the root-cause half — recommendation
in the report is: close the defensive/audit half, keep #99 (or a narrowed replacement issue) open
for the unproven root cause, exactly as PR #100 recommended.
