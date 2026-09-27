# Feature: keep the publisher compat checks out of the unit-test closure (#122)

Branch: `ci/122-publisher-test-closures` · Base: `origin/main` `77beda6` · Epic: #10 · Issue: #122

## Problem

#121 (S1b) switched the four publisher modules' *production* imports from package `ego` to
`port/publishing`, so `go list -deps ./...` no longer resolves GoAkt. Their *tests* still imported
`ego` directly, through one `compat_test.go` per module, to check the historical S1 compatibility
aliases (`ego.EventPublisher`, `ego.StatePublisher`, `ego.ErrPublisherNotStarted`). That import alone
pulls the whole GoAkt runtime back into `go list -deps -test ./...`: 45 GoAkt packages plus the root
package itself, in every publisher, on every `go test ./...`. #121 flagged this as its own known limit
and opened #122 to fix it and measure the real cost, without weakening the compatibility check itself
(the aliases stay until #124's deprecation decision — not this issue's to make).

## What changes

Each publisher's `compat_test.go` moves behind a `//go:build compat` tag and keeps only the
`ego`-alias assertions. A new, untagged `publisher_contract_test.go` keeps the equivalent
`publishing`-only assertions, so the default (untagged) build always has a contract test and never
needs to import `ego`. `scripts/ci/verify-module.sh` detects a compat-tagged file in the module it is
verifying and, only then, also runs `go vet`, `golangci-lint` and `go test` with `-tags compat` — a
"compatibility lane" that keeps proving the aliases, in the same CI job, with no new workflow file. A
new `TestUnitTestClosureExcludesRuntimeAndRoot` test per module guards the regression by shelling out
to `go list -deps -test ./...` from inside the test binary.

## Why this shape

**Build tag, not a new package or module.** A subpackage inside the same module would not remove
anything from `go list -deps -test ./...` at the module root, which already walks every subpackage. A
separate nested Go module per publisher is a heavier decision gated on design.md §6 (a real benefit,
its own CI discovery, a release story) that a four-line alias check does not clear, and it would need
a change to `internal/cmd/ciselect`'s module discovery — out of my file ownership and explicitly
serialized with #102 per the task brief. The build tag needs neither.

A separate GitHub Actions job for the lane was also rejected: it would duplicate the module-selection
and setup steps the `modules` matrix job already has, for a check whose environment is otherwise
identical; the conditional steps inside `verify-module.sh` get an equally separate `-tags compat`
build with zero workflow-file changes.

**No `ciselect` change, and the lane still runs where it must.** `internal/cmd/ciselect` parses every
`.go` file of a nested module — including `_test.go` — with `go/parser` in imports-only mode, which
never evaluates build constraints. It therefore still sees `compat_test.go`'s import of `ego`
regardless of the tag, so a root-package change (including to the aliases themselves, in
`publisher.go`) still selects all four publisher modules, and their `modules` job now runs the
compatibility lane. Observed: `ciselect -changed <publisher.go>` → root mode `full`, `modules.json`
names every nested module (evidence below).

## Constraints

- File ownership: `publisher/*/`, the workflow files and `scripts/ci/*` only as needed for the
  compatibility lane, `docs/ci.md`, `CHANGELOG.md`, this document, and
  `openspec/changes/ego-arch-001/design.md` (disclosed post-hoc: T4 edits §5/§7 there — it documents
  that S1 criterion 4 is now covered by a real test, `TestUnitTestClosureExcludesRuntimeAndRoot`,
  rather than being a one-time observation; the ADR is the only place that fact belongs, and the edit
  does not conflict with #128's separate S3-design work in the same file). Not `internal/cmd/ciselect`,
  `internal/cmd/archcheck`, or root `.go` files.
- The historical `ego`-alias/sentinel check must keep running in CI against all four publishers; it
  must not silently stop being verified.
- No `-race` locally; `GOWORK=off`, `GOFLAGS` cleared for nested-module commands, per
  `scripts/ci/verify-module.sh`'s own convention.
- TDD: strict (user's global configuration), runner `go test`.
- Route: delegated direct — 2+ non-trivial files across four modules plus a shared script; single
  writer, no SDD artifacts.

## Tasks

- [x] **T1** Add a regression-guard test (`closure_test.go`) to each publisher, observe it fail (RED)
  against the pre-existing single-file `compat_test.go`, then split `compat_test.go` (tagged `compat`,
  `ego`-only) from a new `publisher_contract_test.go` (untagged, `publishing`-only) in all four
  modules. Check: RED observed on kafka; GREEN (`go build`, `go vet`, `go test`, both untagged and
  `-tags compat`) on all four; `gofmt -l .` clean.
- [x] **T2** Wire the compatibility lane into `scripts/ci/verify-module.sh` (detect a compat-tagged
  file, run vet/lint/test with `-tags compat` right after the untagged steps) with no workflow-file
  change. Check: full script run (fake `golangci-lint` stub, since local lint fails on this Go
  version — see Progress) for all four publishers and for `benchmark` (no-op, confirms the guard).
- [x] **T3** Measure `go list -deps[-test] ./...`, `go mod tidy -diff`, `go mod graph` and clean-cache
  `go test -count=1 ./...` wall time, before and after, per publisher. Check: recorded below;
  `go mod graph` diffed byte-for-byte before/after (no change).
- [x] **T4** Update `docs/ci.md` ("Compatibility lane (#122)"), `design.md` §5/§7, `CHANGELOG.md`, and
  this document; run `archcheck` and `ciselect` evidence for the PR body. Check: structural readback;
  `archcheck` unchanged (`15 packages checked, 70 edges checked, 1 baselined, 0 violation(s), 0 stale
  entries`).

## Acceptance criteria (from #122)

1. `go list -deps -test ./...` (no `-tags`) contains neither `github.com/tochemey/goakt/v4` nor the
   root package, in every publisher.
2. The historical alias/sentinel check still runs in CI against all four publishers, in a separate
   lane, and that lane runs on PRs that could break an alias.
3. `go mod tidy -diff` and `go mod graph` recorded per module, before/after, with an honest
   explanation of what stayed and why.
4. Comparable build/test time measurements, stated method, no unproven claims.
5. CI still verifies all four modules (build, vet, tidy-diff-equivalent, test).

## Progress and evidence

Go 1.27.1, linux/amd64 (CI pins 1.27.0), `GOWORK=off`, no `-race`. `golangci-lint` locally fails with
`typecheck` errors on `math/rand/v2` (Go 1.27 stdlib vs. golangci-lint v2.13.1's own Go 1.26.6
toolchain) on every nested module, unrelated to this change — this is the repository's documented
local limitation; CI's own lint run is the check of record.

**T1 — done.** RED: with the original single-file `compat_test.go` still in place, the new
`closure_test.go`'s `TestUnitTestClosureExcludesRuntimeAndRoot` failed on kafka, reporting 44 GoAkt
subpackages, `github.com/tochemey/goakt/v4` itself, and the root package, all present in
`go list -deps -test ./...`. GREEN after the split, all four modules: `gofmt -l .` clean; `go build
./...`, `go vet ./...`, `go vet -tags compat ./...` clean; `go test ./...` and `go test -tags compat
./...` both `ok` (the untagged run shows 2 tests, the compat run shows 3 — the alias test only exists
behind the tag).

**T2 — done.** `bash -n scripts/ci/verify-module.sh` clean. Full script run (with a stub
`golangci-lint` standing in for the real one, to isolate the toolchain issue above) exits 0 for
`publisher/kafka`, `nats`, `pulsar`, `websocket`, in this step order: `go mod download`, `go build`,
`go vet`, `golangci-lint run`, `go vet -tags compat`, `golangci-lint run -tags compat`, `go test`,
`go test -tags compat`. Run against `benchmark` (no compat-tagged file): the two new steps are absent
from the log — confirms the guard is a true no-op elsewhere.

**T3 — done.** Per-publisher, `go list -deps ./...` (production) is unchanged in every module. Default
(no `-tags`) `go list -deps -test ./...`:

| Module | Before (deps) | After (deps) | Before GoAkt/root | After GoAkt/root |
|---|---|---|---|---|
| kafka | 611 | 313 | 45 + 1 | 0 |
| nats | 565 | 273 | 45 + 1 | 0 |
| pulsar | 822 | 585 | 45 + 1 | 0 |
| websocket | 551 | 256 | 45 + 1 | 0 |

`-tags compat` reproduces the "before" numbers exactly in every module (confirms the historical check
still exercises the same closure it always did). `go mod tidy -diff`: empty in every module, before and
after. `go mod graph`: byte-for-byte identical before/after in every module (`diff` exit 0). Reason
recorded in docs/ci.md: `go mod tidy` has no `-tags` flag, so it conservatively keeps requirements for
every build tag a module's files use, including `compat`; the win is the test-compilation closure, not
the declared module graph.

Clean-`GOCACHE` `go test -count=1 ./...` wall time (one machine):

| Module | Before | After (default) | After (`-tags compat`) |
|---|---|---|---|
| kafka | 30.1s | 13.3s | 26.7s |
| nats | 34.0s | 15.4s | not separately measured |
| pulsar | 55.8s | 29.1s | not separately measured |
| websocket | 33.8s | 18.2s | not separately measured |

**T4 — done.** `go run ./internal/cmd/archcheck`: `15 packages checked, 70 edges checked, 1 baselined,
0 violation(s), 0 stale entries` (unchanged). `ciselect` evidence: `-changed
publisher/kafka/compat_test.go` alone → root mode `none`, `modules.json` `["publisher/kafka"]`;
`-changed publisher.go` (a root file, simulating an alias change) → root mode `full`, `modules.json`
names all six nested modules, each reasoned "imports affected root package
`github.com/pablogore/ego/v4`" — proving the compatibility lane runs on the PRs that matter without
touching `ciselect`.

**Review:** RDD is off (global), so no native review ran; delivery follows ordinary repository policy.

**Next step:** open the PR against `main` with `Refs #122` (or `Closes #122` if all five criteria are
judged fully met at PR time), label `enhancement`.
