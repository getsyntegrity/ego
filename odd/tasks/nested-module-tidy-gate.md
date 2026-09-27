# Feature: make `go mod tidy -diff` a real CI gate for every nested module (#122 follow-up)

Branch: `ci/122-nested-tidy-gate` · Base: `origin/main` `27848da` · Epic: #10 · Issue: #122

## Problem

#122's acceptance criteria named `go mod tidy -diff` alongside `go.mod`/`go.sum`/`go mod graph`
recording as something the change would check for the four publisher modules. #122's own PR (#130,
merged) recorded `go mod tidy -diff` results by hand in its task document and the CHANGELOG, but
`scripts/ci/verify-module.sh` — the script CI actually runs for every nested module — never ran the
command itself (confirmed by reading the script: it downloads, builds, vets, lints and tests, with
no tidy step anywhere). An untidy `go.mod`/`go.sum` in `benchmark`, `example/cluster`, or any of the
four publishers would go green today. The maintainer approved adding it as a separate, focused PR
(2026-09-27) rather than folding it back into #130.

## What changes

`scripts/ci/verify-module.sh` gains one new step, `go mod tidy -diff`, placed right after
`go mod download` and before `go build` (a tidy check is cheaper than a build, and a build against an
untidy `go.mod` is not the failure worth reporting first). `-diff` never writes `go.mod`/`go.sum`; it
prints the pending change as a unified diff and exits non-zero when one exists. The script now prints
that diff, a one-line explanation, and exits 1 — the same failure shape as every other step (an
`::group::`/`::endgroup::` pair, a step recorded for the job summary). The script's existing
`export GOWORK=off` gains a sibling `export GOFLAGS=`, so an inherited `-mod=vendor` (nested modules
keep no `vendor/` directory) can no longer break the new step, or any other step, for a reason that
has nothing to do with the module's own tidiness.

## Why this shape

**No `-tags compat` variant of the tidy check.** `go mod tidy` has no `-tags` flag at all (`go help
mod tidy`), so, unlike `go build`/`go vet`/`go test`, it does not need to be told about the `compat`
build tag the four publishers use for their historical alias check (docs/ci.md, "Compatibility
lane") — it already considers every file in the module when computing required modules. #122's own
change already measured this empirically: `go mod tidy -diff` was empty in every publisher both
before and after the `compat` tag was introduced (`odd/tasks/publisher-test-closures-122.md`, T3).
One tidy step covers both lanes.

**Placement: right after `go mod download`, before `go build`.** A tidy check needs the module's
dependencies downloaded (so it can compute the graph without hitting the network for straightforward
cases) but nothing else — build, vet, lint and test all depend on a state the module is already in,
which tidiness is a precondition for. Failing here gives the clearest first message: "this go.mod is
wrong" instead of a build or vet error that a stale `go.mod` might also happen to produce.

**`GOFLAGS=` next to the existing `GOWORK=off`.** The `modules` matrix job in both `pull_request.yml`
and `build.yml` never sets `GOFLAGS` itself (confirmed by reading both workflow files: the job's own
`env:` block only sets `FORCE_COLOR`), so this is a no-op in CI. Locally, an inherited
`GOFLAGS=-mod=vendor` from a caller's shell would make `go mod tidy` refuse outright and would also
silently change the build/vet/test steps against a module that carries no `vendor/` directory — the
same class of hazard the script's own `GOWORK=off` already guards against for `go.work`. Clearing it
unconditionally, rather than only around the new step, keeps the whole script consistent about what
environment it verifies against.

**No workflow change.** Both `pull_request.yml` and `build.yml` already call
`scripts/ci/verify-module.sh "${{ matrix.module }}"` for every selected module; the new step lives
entirely inside the script, so neither workflow file needed touching. Confirmed by reading both
files' `modules` job.

## Constraints

- File ownership: `scripts/ci/verify-module.sh` (and any existing tests for it — none exist), `docs/ci.md`,
  `CHANGELOG.md`, this document. Not `.github/workflows/*` (not needed — see above), `internal/cmd/*`,
  publisher code, or the root package.
- The check must fail closed: any nested module with an untidy `go.mod`/`go.sum` must fail the job,
  with the diff printed so a contributor knows exactly what to run.
- No `-race` locally; `GOWORK=off` and (now) `GOFLAGS=` cleared for nested-module commands, per the
  script's own convention.
- TDD: strict (user's global configuration); no Go test runner applies here (a shell script, no shell
  test framework exists in this repository) — verified by RED/GREEN against a throwaway local fixture
  instead, documented below.
- Route: delegated direct — one script plus its docs, one writer, no SDD artifacts.

## Tasks

- [x] **T1** Read `scripts/ci/verify-module.sh` and both workflow files; confirm the tidy step is
  genuinely missing and that no workflow change is required. Check: `rg -n "tidy" scripts/ci/*.sh
  .github/workflows/*.yml` before the edit shows `verify-module.sh` never calls `go mod tidy`, and
  both workflow files' `modules` job only ever calls `verify-module.sh` with no tidy-related flags.
- [x] **T2** Confirm no nested module is currently untidy on `main` (27848da), so the new gate does not
  fail immediately for an unrelated reason. Check: `go mod tidy -diff` (`GOWORK=off`, `GOFLAGS=`)
  exits 0 with no output in all six modules — recorded below.
- [x] **T3** Add the `go mod tidy -diff` step to `verify-module.sh` (`GOWORK=off`/`GOFLAGS=`
  semantics, placed after `go mod download`, before `go build`); update the file's top-of-script
  comment. Check: `bash -n scripts/ci/verify-module.sh` clean; RED against a throwaway untidy fixture
  in one publisher, then GREEN on the clean tree for all six modules — both recorded below.
- [x] **T4** Update `docs/ci.md` (the "what runs for one selected module" step list and the
  paragraph after it), `CHANGELOG.md`, and this document.
  Check: structural readback.

## Acceptance criteria (from #122, this follow-up's scope)

1. `scripts/ci/verify-module.sh` runs `go mod tidy -diff` for the module being verified, in the same
   job as build/vet/lint/test, with no separate workflow job or step.
2. An untidy `go.mod`/`go.sum` fails the job with the diff printed, before build/vet/lint/test run.
3. A tidy module (all six today) is unaffected: the new step adds one `::group::` and no failure.
4. `docs/ci.md` documents the new step in the same place it documents every other `verify-module.sh`
   step.

## Progress and evidence

Go 1.27.1, linux/amd64 (`env -u GOROOT go version`); CI pins 1.27.0. `GOWORK=off`, `GOFLAGS=` cleared,
no `-race`. `golangci-lint` (v2.13.1, locally installed) panics with a `math/rand/v2` typecheck error
on every nested module — the same pre-existing local toolchain mismatch (Go 1.27 stdlib vs.
golangci-lint's own Go 1.26.6-pinned type checker) already documented in
`odd/tasks/publisher-test-closures-122.md`; unrelated to this change. Full-script GREEN runs below
use a stub `golangci-lint` (prints one line, exits 0) on `PATH` to isolate that known limitation, the
same technique `publisher-test-closures-122.md`'s T2 used; CI's own lint run remains the check of
record.

**T1 — done.** `rg -n "tidy" scripts/ci/verify-module.sh` (pre-edit): no match. `rg -n
"verify-module|GOFLAGS|tidy" .github/workflows/pull_request.yml .github/workflows/build.yml`: both
call `scripts/ci/verify-module.sh "${{ matrix.module }}"` with only `GO_TEST_RACE` in the step's own
`env:`; the job-level `env:` in both files sets only `FORCE_COLOR`. No workflow change needed.

**T2 — done.** `go mod tidy -diff` (`GOWORK=off`, `GOFLAGS=`), run individually in each module's own
directory, against `main` at `27848da`:

| Module | `go mod tidy -diff` |
|---|---|
| `benchmark` | exit 0, no diff |
| `example/cluster` | exit 0, no diff |
| `publisher/kafka` | exit 0, no diff |
| `publisher/nats` | exit 0, no diff |
| `publisher/pulsar` | exit 0, no diff |
| `publisher/websocket` | exit 0, no diff |

No STOP condition triggered — the gate does not fail immediately for an unrelated reason.

**T3 — done.**

RED: on a throwaway, uncommitted copy of `publisher/nats/go.mod` (backed up to `/tmp` first, restored
byte-for-byte afterwards — `git status --porcelain publisher/nats/go.mod` empty, `git diff` empty), the
line `github.com/nats-io/nats.go v1.53.1` was removed from the `require` block — a directly-imported
dependency missing from a tidy `go.mod`. Running the updated script:

```
$ bash scripts/ci/verify-module.sh publisher/nats
::group::go mod download (publisher/nats)
::endgroup::
::group::go mod tidy -diff (publisher/nats)
diff current/go.mod tidy/go.mod
--- current/go.mod
+++ tidy/go.mod
@@ -10,6 +10,7 @@
 require (
 	github.com/flowchartsman/retry v1.2.0
+	github.com/nats-io/nats.go v1.53.1
 	go.uber.org/atomic v1.11.0
 	google.golang.org/protobuf v1.36.12
 )
::endgroup::
verify-module.sh: publisher/nats's go.mod/go.sum are not tidy.
Run 'go mod tidy' inside publisher/nats and commit the result; see the diff above for what changes.
$ echo $?
1
```

Failed at the new step, before `go build` ever ran, with the offending diff printed. The fixture was
restored immediately afterward and never committed.

GREEN: full script, all six modules, clean tree:

| Module | Exit | Tidy step | Compat lane |
|---|---|---|---|
| `benchmark` | 0 | no diff | n/a (no compat-tagged file) |
| `example/cluster` | 0 | no diff | n/a (no compat-tagged file) |
| `publisher/kafka` | 0 | no diff | ran (vet/lint/test `-tags compat`) |
| `publisher/nats` | 0 | no diff | ran |
| `publisher/pulsar` | 0 | no diff | ran |
| `publisher/websocket` | 0 | no diff | ran |

No shell test framework exists under `scripts/ci` or elsewhere in the repository (`fd -e bats .`
found nothing); this RED/GREEN pair against a throwaway fixture is the documented manual check, per
the task brief.

`shellcheck` is not installed in this environment (`which shellcheck` → not found); not run. No
`brew`/package install was performed for it — flagging for the reviewer/maintainer rather than
installing tooling unprompted.

**T4 — done.** `docs/ci.md`'s "`scripts/ci/verify-module.sh`: what runs for one selected module" step
list now numbers the tidy check as step 2 (renumbering build/vet/lint/test/compat-lane to 3–7) with
an explanation of why no `-tags compat` variant is needed; the "Any of these steps failing…" paragraph
now names an untidy `go.mod`/`go.sum` alongside build/lint/test failures. `CHANGELOG.md` gained one
`🧹 Improvements` entry under `[Unreleased]`. `archcheck`/`ciselect` were not re-run: this change
touches no `.go` file, so neither tool's output can change; not claimed as evidence.

**Review:** RDD is off (global), so no native review ran; delivery follows ordinary repository policy.

**Next step:** open the PR against `main` with `Refs #122`, label `enhancement`.
