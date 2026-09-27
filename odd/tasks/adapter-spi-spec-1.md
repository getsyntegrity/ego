# Feature: adapter SPI package and composition-root boundary (#106, ego-arch-004 spec 1)

Branch: `feat/106-spec1-spi-boundary` · Base: `origin/main` `8e6fd7c` (started on `1934663`, rebased
before the first push after #152 merged) · Epic: #10 · Issue: #106

Implements [`openspec/changes/ego-arch-004/specs/adapter-spi-boundary/spec.md`](../../openspec/changes/ego-arch-004/specs/adapter-spi-boundary/spec.md)
(spec 1 of 3: slices SPI-1 and SPI-2). Next in the chain: spec 2, `adapter-conformance`.

## Problem

An adapter today has no way to say which ports it serves or which optional behaviors it has, so
the composition root can only find out by type-asserting optional interfaces at the call site.
Separately, the #106 follow-up showed that a nested publisher module can import `compose` or
`compose/goakt` and archcheck stays green, although ego-arch-001 §3 says an external adapter may
import only contracts and `egopb`. The maintainers decided on 2026-09-27 (O1) that adapters must not
import the composition root.

## What changes

1. **SPI-1.** A new standard-library-only contract package, `port/adapter`: `Port`, `Capability`,
   `Descriptor` (with `Declares` and `Serves`), the optional interfaces `Describer`, `Starter`,
   `Pinger`, the constants `CapStart` and `CapReady`, and the accessors `Describe`, `StarterOf` and
   `PingerOf`, which own the only type assertions on those three interfaces. Each slot-owning contract
   package (`port/publishing`, `persistence`, `offsetstore`, `tenancy`, `encryption`) gets a `port.go`
   with **untyped** port-name constants, so none of them imports `port/adapter` (a typed constant
   would, and would later form a module cycle in the ego-arch-006 contracts module).
2. **SPI-2.** A new archcheck denylist rule, `external-adapter-no-composition`, on
   `ExternalAdapterLayer`: a package of a nested module under `publisher/` must not import
   `<root>/compose` or anything under it. No `main`/example exemption. The four publisher closure tests
   also reject `compose` in `go list -deps -test ./...`, because archcheck never reads `_test.go` files.

Rejected: widening `external-adapter-no-runtime` (its ID says "runtime", so a report would name the
wrong constraint) and widening `composition-leaf` (it exempts `main` packages), per design §10.

## Scope and constraints

- File ownership: the spec's list, plus `CHANGELOG.md`, this document and
  `internal/cmd/archcheck/e2e_test.go` (an end-to-end fixture for the new rule; it is outside the
  spec's `internal/cmd/archcheck/rules/*_test.go` glob, within the coordinator's
  `internal/cmd/archcheck/**` scope). Not touched:
  `engine.go`, `port/runtime`, `spawn_config.go`, `saga.go`, `supervisor.go`, `compose/`, publisher
  production code, `.github/`.
- Additive in v4: no method added to any existing interface; apidiff additions only.
- No baseline entry added. The repository baseline on the base (`8e6fd7c`, after #152) is empty.
- The spec lists SPI-1 and SPI-2 as slices; as the coordinator asked for one pull request, they land
  as two work-unit commits in it (SPI-1 about 760 changed lines, SPI-2 about 475; mostly tests and
  license headers). Each commit can be reviewed on its own.
- TDD: strict (user global configuration); runner `go test` (plus `scripts/ci/verify-module.sh` for
  nested modules). Never `-race`, never the workbench.
- Route: delegated direct (one writer; this spec touches 2+ non-trivial files).

## Tasks

- [x] T1 `port/adapter`: types, accessors, package doc naming the one-assertion rule; RED tests first.
      Commit `6b20d77`.
- [x] T2 Port-name constants in the five `port.go` files; `go list -deps` stdlib-only architecture
      test for `port/adapter`; test that the five contract packages do not import `port/adapter`.
      Commit `6b20d77`.
- [x] T3 archcheck rule `external-adapter-no-composition` with RED graph tests (`compose`,
      `compose/goakt`, `compose/internal/lifecycle` with `no-cross-module-internal` also firing; root
      `main` importing `compose` stays allowed) and an end-to-end fixture. Commit `4e0d169`.
- [x] T4 Publisher closure tests reject `<root>/compose` and its subpackages. Commit `4e0d169`.
- [x] T5 Documentation: `docs/ci.md` rule table and adapter-roots note; ego-arch-001 design §3
      amendment; `CHANGELOG.md`. Commit `4e0d169`.

## Acceptance criteria

- Every scenario of the spec has a test that was observed failing first.
- `go run ./internal/cmd/archcheck`: 0 violations, no new baseline entry, new rule evaluated.
- apidiff: additions only on `port/publishing`, `persistence`, `offsetstore`, `tenancy`,
  `encryption`; `port/adapter` new.
- Closure tests and `scripts/ci/verify-module.sh` pass for all nested modules.

## Progress and evidence

**Judgement call: typed-nil input.** The design says an accessor returns `(zero, false)` when the
value "does not implement" the interface; it does not say what a typed-nil pointer is. A typed-nil
`*T` implements the interfaces by method set, but calling `Describe`, `Start` or `Ping` on it would
usually dereference nil. The accessors treat a nil or typed-nil value as absent (a `reflect` nilness
check, no method discovery), as package `ego`'s `ResolveLogger` does for a typed-nil logger. The
alternative, returning the typed nil with `true`, would hand callers a value they cannot safely call.

**RED, then GREEN.**

| Requirement / scenario | Test | RED observed |
|---|---|---|
| Undeclared value -> zero and false, no panic | `port/adapter/adapter_test.go` `TestAccessors_UndeclaredValueReturnsZeroAndFalse` (+ implementing, independence, typed-nil, `Declares`/`Serves`, `CapStart`/`CapReady`) | build failure (package absent); typed-nil guard removed -> `TestAccessors_TypedNilIsTreatedAsAbsent` panics |
| `port/adapter` stdlib-only | `adapter_architecture_test.go` `TestAdapterDependsOnlyOnStdlib` (`go list -deps`, empty allowlist) | package absent |
| Contracts never import `port/adapter` | `TestContractPackagesDoNotImportAdapter`; `TestPortNameConstantsAreUntyped` (parses the five `port.go`) | port.go absent -> 5 subtests fail; a temporary `tenancy` file importing `port/adapter` -> fails for `tenancy` and `persistence` |
| Spike: `publisher/kafka` imports `compose/goakt` -> violation | `rules/adapter_composition_test.go` `TestExternalAdapterNoComposition_ExplorationSpikeFails`; `e2e_test.go` `TestRunCheck_AdapterImportingCompositionFails` | rule not found / runCheck passed; real repo with a temporary kafka file importing `compose` and `compose/goakt`: archcheck exit 0 before, 2 violations after |
| `compose`, `compose/goakt`, `compose/internal/lifecycle`; `main` and example inside the adapter not exempt; `no-cross-module-internal` also fires on `internal` | `TestExternalAdapterNoComposition_ForbidsEveryCompositionPackage` | unknown rule id |
| Root `main` importing `compose` stays allowed | `TestExternalAdapterNoComposition_AllowsLegitimateImporters`, `..._RootMainIsAllowedByEveryRule` | unknown rule id |
| Fail closed | `TestEvaluate_ZeroMatchRulesFailClosed` now names the new rule | error did not name it |
| Closure tests reject `compose` | `publisher/*/closure_test.go` `TestClosureGuardRejectsCompositionRoot` | undefined `closureViolation`; real kafka module with a temporary file importing `compose`: closure test passed before, failed after |

**archcheck on the real repository.** Before (`8e6fd7c` plus the SPI-1 commit, without the rule): `8 modules checked,
47 packages checked, 195 edges checked, 0 baselined, 0 violation(s), 0 stale entries`. After: the same
line, with `external-adapter-no-composition` evaluated and matching 4 packages (one per publisher).
No baseline entry added; `baseline.go` untouched and empty.

**apidiff** (base `8e6fd7c` -> head): compatible additions only. `port/publishing`:
`PortEventPublisher`, `PortStatePublisher`; `persistence`: `PortEventsStore`, `PortSnapshotStore`,
`PortStateStore`; `offsetstore`: `PortOffsetStore`; `tenancy`: `PortTenantResolver`; `encryption`:
`PortEncryptor`. `port/adapter` is new.

**Checks.** `go test ./port/... ./persistence/... ./offsetstore/... ./tenancy/... ./encryption/...`
and `go test ./internal/cmd/archcheck/...` pass. `ciselect -base 8e6fd7c`: mode `affected`, 18 of 29
root packages, all 7 nested modules. `scripts/ci/verify-module.sh` passes for `publisher/kafka`,
`publisher/nats`, `publisher/pulsar`, `publisher/websocket`, `benchmark`, `example/cluster` and
`test/compat` (Go 1.26.6 SDK, `GOTOOLCHAIN=local`; `example/cluster` needed one retry because another
session's golangci-lint held the lock). `golangci-lint --new-from-rev=origin/main`: 0 issues (root and
`publisher/kafka`; `--modules-download-mode=readonly` locally because `.golangci.yml` uses vendor mode
and the repository has no `vendor/`). Full root suite `go test -count=1 ./...` (no `-race`) on `4e0d169`: exit 0, 25 packages `ok`, no
failure.

## Next step

Open the pull request; then spec 2 (`adapter-conformance`), which needs `port/adapter`.
