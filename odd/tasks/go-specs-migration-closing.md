# Closing the go-specs migration: zero testify, zero generated mocks (#205, epic #201, gate #204)

## Problem

Every migration PR of epic #201 is merged (the last one was #267, engine G2), but three leftovers still held the
repository on testify and on the old mock tooling:

- `engine/helper_test.go` still imported `testify/require` for `newTestEngine`, and the engine specs carried
  four near-duplicate helper files (`specs_helpers_g1..g4_test.go`) whose function names ended in `G1`..`G4`.
- The generated `mocks/` tree (mockery output, built on `testify/mock`) was still in the repository, together with
  the `docker-mock` Makefile target and a mockery install in `Dockerfile.ci`.
- The same typed `mock.Controller` adapters were copied four times: `engine/specs_mocks_test.go`,
  `internal/engine/projection/specs_mocks_test.go`, `internal/extensions/specs_mocks_test.go` and
  `internal/projectionrunner/mocks_test.go`, although `internal/engine/enginetest` already had most of them.
- `go.mod` required testify, the unit gate still had 42 pending lines, and CI ran the gate without `-strict`.

## What changes

1. `engine/helper_test.go`: `newTestEngine` reports failures with `t.Fatalf` instead of `require`, so it no
   longer imports testify. The six helper files collapse into `engine/specs_helpers_test.go`; the `G1`..`G4`
   suffixes are gone (`connectedEventsStore`, `connectedDurableStore`, `connectedOffsetStore`,
   `newSpecsEngine`, `startEngine`, `dispatch`, ...). No top-level `Test` function is renamed. An unused
   plain-`*testing.T` `dispatchWithMetadata` that lint flagged is deleted.
2. `internal/engine/enginetest` gains `OffsetStoreMock`, `EventPublisherMock` and `StatePublisherMock` (with
   specs in `mocks_test.go`). The four local adapter files are deleted and the specs use
   `enginetest.NewXxxMock(ctrl)`. No import cycle prevents this: `enginetest` imports only leaf packages, so
   all four packages could move and nothing stays duplicated.
3. The `mocks/` tree is deleted, plus `docker-mock` in the `Makefile`, the mockery install in `Dockerfile.ci`, the
   `docker-mock` row in `contributing.md` and the "generated mocks" sentence in `readme.md`. There was no
   `.mockery.yaml`: the flags lived in the Makefile target.
4. `go mod tidy` in all eight modules. Only the root `go.mod` changed: `testify`, `objx` and `go.yaml.in/yaml/v3`
   leave it. `go.sum` files keep `stretchr/testify` lines: they are `go.mod` hashes needed to resolve the
   module graph, because go-specs, goakt, olric and otel all list testify in their own `go.mod`. Nothing in
   this repository requires it, so `go mod tidy` correctly keeps those lines.
5. The unit gate: `.github/unit-test-gate-pending.txt` is empty (header only, because the gate reads the file),
   stale lines in `.github/unit-test-gate-resources.txt` are removed (the two helper files became one, three
   actor test files no longer start a real system), and `ci.yml` runs `unitgate -strict`.
6. Docs: `docs/testing/unit-migration.md` says every unit test is `migrated` and lists the out-of-unit-lane
   tests as documented exceptions tied to the resources allowlist; `docs/testing/go-specs.md` says testify and
   generated mocks no longer exist.

## What does not change, and why

- Production code. No file outside tests, tooling and docs is touched.
- The 256 out-of-phase tests (real goakt actor systems, `go list`, `httptest`, the Postgres example). They are
  not unit tests; the resources allowlist is where the gate records them.
- `newTestEngine` keeps its `*testing.T` signature. Converting its 50 call sites to a `*specs.Context` helper
  was rejected: it is a pure rename with risk and no gain, since `t.Fatalf` is what `require` did.

## Constraints

Strict TDD (RED by deliberate mutation of a helper or mock, reverted), no `-race`, no workbench, no
network or database in unit tests, Conventional Commits without attribution.

## Public API note

`github.com/getsyntegrity/ego/mocks/...` was an importable package, so removing it is a breaking change for
anyone who imported it. The module is v0.x, so this needs a `kind/breaking` label and a release note, not a
`/vN` bump.

## Tasks

| ID | Task | Route | Evidence | Commit |
|---|---|---|---|---|
| T1 | Fold helpers, drop testify from `helper_test.go` | inline | RED: mutated `expectConcurrencyConflict` made `go test ./engine` fail (42 failure lines); GREEN after revert (`ok engine 64.7s`) | 97c7562 |
| T2 | Consolidate mock adapters onto `enginetest` | inline | RED: renamed a forwarded method in `OffsetStoreMock`, enginetest failed (7 failure lines); GREEN: engine, projection, extensions, projectionrunner, enginetest all `ok` | 5115698 |
| T3 | Delete `mocks/` and its tooling | inline | `rg -l 'ego/mocks' --type go` empty; build and vet pass in all modules | 830c46c |
| T4 | `go mod tidy`, gate strict and empty pending | inline | `go run ./.github/scripts/unitgate -strict` prints `ok (0 pending entries, 41 resource entries)` | f39dba0, a8cb3a1 |
| T5 | Docs, lint cleanup | inline | `golangci-lint run ./...` 0 issues; root `go test -count=1 ./...` all `ok` | e42c0fb, 94837b8 |

The five tasks fit the cap; there is no follow-up spec.

## Progress

All tasks done. The nested modules `benchmark`, `example/cluster` and `publisher/pulsar` show pre-existing
golangci-lint findings (staticcheck, errcheck, unused) that this change does not touch.
