# Feature: wait for sum.golang.org, not just the proxy, before the publisher bump (#189)

Branch: `ci/189-sumdb-wait` · Base: `origin/main` `cac848a7d4068600d2443eca7cb4ea7a621f450c` · Issue: #189 · Related: #159, #134, #39 · Out of scope: #190 (post-publish public consumer test)

## Problem

`.github/workflows/release.yml`, job `prepare-publisher-bump`, step "Wait for ego module to be available on proxy" loops `go list -m github.com/getsyntegrity/ego/v4@$EGO_VERSION` (30 tries, 30 s apart). That command only proves that `proxy.golang.org` serves the version. The next step, `go get`, also verifies the module against `sum.golang.org`, and the checksum database can lag behind the proxy.

This happened in the first release: `release.yml` run 36474920456, attempt 1 (job 109106620419). The wait passed at 19:51:23Z and `go get` failed 15 seconds later with `reading https://sum.golang.org/lookup/github.com/getsyntegrity/ego/v4@v4.0.0: 404 Not Found` and `invalid version: unknown revision v4.0.0`. Attempt 2 (20:07Z) passed with no code change.

## What changes

A new standard-library-only command, `internal/cmd/modwait`, replaces the shell loop. Each attempt runs the consumer's public path: `go mod download -json <module>@<version>` in a fresh temporary directory with a fresh temporary `GOMODCACHE`, with `GOPROXY` and `GOSUMDB` active. A fresh module cache matters: a cached copy would skip the checksum lookup, and the checksum lookup is exactly what `go get` does next and what failed. So a pass here means "proxy and sum.golang.org both answer for this version", which is what the next step needs.

Output is classified by a pure function (`Classify`) into transient (retry until the deadline), permanent (fail at once) or unclassified (retry, but say so). The wait loop takes an injected prober, clock and sleeper, in the style of `internal/cmd/releasegate`, so every case is tested without network or real sleeping. `release.yml` calls `go run ./internal/cmd/modwait ...` in place of the loop. `docs/ci.md` documents it.

## Why

- HTTP status must win over text. The real failure contains `invalid version: unknown revision v4.0.0` inside a sumdb `404`. Reading only the text would call it permanent and fail the release for a propagation delay.
- An `unknown revision` with no HTTP status is also transient: the proxy or VCS may not have the tag yet. The cost of being wrong is one wasted wait up to the timeout, never a wrong success.
- Unclassified output is retried, not failed: an unknown message is more likely a new phrasing of a transient condition than a proof of permanence, and the deadline bounds the cost. It is reported as `unclassified` so a human sees it.
- Rejected alternative, a shell script (`curl` on `sum.golang.org/lookup/...`): equally possible, but classification over multi-line `go` output and a fake clock are awkward to test in shell, and `curl` would test a lookalike of `go get`'s path instead of the path itself. Rejected alternative, a bare `GET .../lookup/` with `curl`: same reason.
- Rejected alternative, `go get` in a scratch module: it also works, but `go mod download -json` reports errors as structured JSON and does not need a `go.mod` edit.

## Scope and constraints

- In scope: `internal/cmd/modwait/**`, the one wait step in `.github/workflows/release.yml`, a section in `docs/ci.md`, this document.
- Out of scope: #190; every other step of `release.yml` (byte-identical); `verify-published.sh`; any tag, push, PR, release or workflow dispatch.
- Never set `GOSUMDB=off`, `GONOSUMDB`, `GOPRIVATE`, `GOINSECURE`, `GOFLAGS=-insecure` or a `replace` for the checked module. `${{ }}` only through `env:`.
- TDD: strict (user global configuration); runner `go test -count=1 ./internal/cmd/modwait/...`. Never `-race`, never the workbench. RDD: off (global).
- Route: delegated direct (one writer). Delivery: one work-unit commit per task; push and PR belong to the coordinator. Forecast well under 400 authored lines of code plus tests plus docs; if the tests push it over, that is natural (tests dominate) and not split.

## Tasks

- [x] T1 Pure classifier `Classify` plus tests, including the verbatim run 36474920456 fixture. Check: RED (does not compile), then GREEN. Commit `0b49e80`.
- [x] T2 Bounded wait loop (injected prober, clock, sleeper; flags, `-timeout 0`, sleep clamp, `::error::` messages) and the real prober (`go mod download -json` with a scrubbed environment and temp dirs cleaned; tests inspect env and args). Check: table tests without network, fake `go` binary for the exec plumbing. Commit `de5b947`. Tasks T2 and the former "real prober" task share one commit because `package main` needs `main()` and the real prober to build; splitting would leave a non-building commit.
- [x] T3 Wire `release.yml`; actionlint, YAML parse, `bash -n`, no `${{` in run bodies, diff limited to the wait step. Commit `16e650e`.
- [x] T4 `docs/ci.md` section, live read-only probes, final verification, SHAs in this document. Commit: the one that adds this text (docs only).

## Acceptance criteria

The list under "Requirements" of #189 and the maintainer's brief: immediate success, 404 then success, 404 until timeout, 410 and checksum mismatch fail without retry, 5xx/429/network then success, explicit limit, resume instructions, actionlint clean.

## Evidence

- Base: `cac848a7d4068600d2443eca7cb4ea7a621f450c`. Tested code head: `16e650e` (the docs commit after it changes only `docs/ci.md` and this file).
- RED: `go test ./internal/cmd/modwait/...` before any implementation failed to compile (`undefined: Class`, `Transient`, then `ProbeResult`), for T1 and for the wait tests. GREEN: all pass after implementation. The probe tests were written before `probe.go` but shared the RED compile failure of the package rather than a run of their own.
- Verification: `go test -count=1` on modwait, releasegate, releaseplan, ciselect, archcheck all ok; `go vet`, `gofmt -l`, staticcheck and revive report nothing; actionlint on `release.yml` has 0 findings; YAML parses; `bash -n` passes on all 9 run bodies; no `${{` inside any run body.
- Live read-only probes (2026-09-28, under the old `/v4` path; the nonexistent-version probe must not be repeated because the checksum database caches negative answers, public proxy and sumdb): `-version v4.0.0 -timeout 0` passes (exit 0, one attempt, about 6 s). `-version v4.0.99 -timeout 0` exits 1: classified `transient (unknown revision without an HTTP status)` because the proxy's 404 falls through to a direct VCS lookup, which answers `invalid version: unknown revision v4.0.99` with no 404; it then emits the `::error::` line with the resume message.
- Judgement calls: `unknown revision` without a status is transient; unclassified output is retried and reported; `GOENV=off` and `GOWORK=off` are forced and `GONOPROXY` is stripped too (beyond the list in the brief), because a `go env -w` file or `GONOPROXY` would also change the public path; the tool refuses to start with `GOSUMDB=off`, `GOPROXY=off` or `-insecure`; one attempt is capped at 3 minutes; the resume hint text is a constant in `wait.go`, not a flag.
- Limitations: not exercised against a real propagation delay (none available on demand); the step's `go run` needs the repository checkout at the tagged commit to contain `internal/cmd/modwait`, which holds for any tag cut after this change merges.

## Follow-up A: gosec fix (PR #192 CI)

golangci-lint (gosec) flagged `modwait/probe.go`: G204 (subprocess with variable arguments), G306 (file mode above 0600) and G122 (permission walk inside `WalkDir`). Fixed in `d34c64e`: `validateTarget` checks the module path and a full semantic version before either reaches `exec` (and `parseConfig` rejects them earlier, so a flag-like `-x` module cannot be smuggled in); the two `exec` calls carry `//nolint:gosec // <reason>`, the convention of `internal/cmd/ciselect/main.go`; directories are 0700 and `go.mod` is 0600; the read-only module cache is deleted with `go clean -modcache` against the temporary `GOMODCACHE`, then `os.RemoveAll`, instead of a `chmod` walk. Standalone `gosec` ignores `//nolint`, so its two remaining G204 reports are the suppressed ones; `golangci-lint` itself could not be run locally (it fails loading packages with a vendoring error in this environment).

## Follow-up B: the root path becomes `github.com/getsyntegrity/ego` (v1.0.0)

Maintainer decision: the root module moves from `.../ego/v4` to the suffix-less `github.com/getsyntegrity/ego`; the first version is `v1.0.0`, the publishers go `v0.1.0` to `v0.2.0`. `v4.0.0` stays published under the old path (never deleted or moved).

Tasks, one commit each:

- [x] B1 Path rewrite in Go code, `go.mod` files (nested modules require `v1.0.0` and keep their local `replace`), tooling literals, `.proto` `go_package`, regenerated `egopb/ego.pb.go` and `test/data/testpb/test.pb.go` (protoc-gen-go v1.36.12, `buf` v1.50.0, never text-edited), and `internal/cmd/releaseplan` ignoring tags whose major is illegal for the module path (TDD: RED was a compile failure on `latestLegalTag`, `nextTagDetailed`, `PlanModule.IgnoredTags`). Commit `f13a0a6`. The generated diff is exactly the path bytes plus their two length prefixes (`Bx` to `Bu`, `Z+` to `Z(`; `B\x89\x01` to `B\x87\x01`).
- [x] B2 Release tooling: `release.yml` (`modwait -module github.com/getsyntegrity/ego`, `go get` of the new path), `release-publishers.yml` text, `verify-published.sh`, `verify-consumer.sh` (default publisher version `v0.2.0`, still drops every tag of the scratch clone, so the real `v4.0.0` and `v0.1.0` cannot collide, and still rejects a publisher `v2.0.0`). Commit `4cc2206`.
- [x] B3 Descriptor proof tests (`egopb/descriptor_test.go`, `test/data/testpb/descriptor_test.go`: registry lookup of every message, marshal/unmarshal round trip, `go_package` value), `docs/ci.md` (new "Root module path and versions" section and path updates), `CHANGELOG.md`, `readme.md`, this document. Commit: the one that adds this text.

Judgement calls:

- `releaseplan` keeps a `v0.0.0` baseline for a suffix-less root, so the dry run shows `v0.1.0` for `-bump minor` and `v1.0.0` for `-bump major` (root has no legal tag). The first real root tag is `v1.0.0`, pushed by a person; the dry run cannot show `v1.0.0` for the root and `v0.2.0` for the publishers at once because it uses one `-bump`. Rejected: making a first suffix-less release always `v1.0.0` (the code comment already rejected it, and it would diverge from `release.yml` for publishers).
- The root path is now a prefix of every nested module path. Checked: `go list ./...` at the root lists no nested module package; `ciselect` and `archcheck` resolve owners by longest prefix; `verify-consumer.sh` (a consumer importing the root plus all four publishers, no `replace`) resolves with no "ambiguous import".
- Docs keep historical mentions of the `/v4` path where they quote a past measurement or the real failing log of run 36474920456.

Verification (root, on `4cc2206` plus the descriptor tests): `go build ./...`, `go vet ./...`, `go test -count=1 ./...` (all packages ok; `engine` 340 s), `go run ./internal/cmd/archcheck` (0 violations). Nested modules (`benchmark`, `example/cluster`, `test/compat`, `publisher/kafka|nats|pulsar|websocket`): `go mod tidy -diff` clean, `go build`, `go vet`, `go test -count=1` ok. `scripts/ci/verify-consumer.sh`: OK (root `v1.0.0`, four publishers `v0.2.0`, publisher `v2.0.0` rejected). `actionlint`: 0. Not run locally: `golangci-lint`, `govulncheck` (not installed / cannot load packages here), `scripts/ci/verify-module.sh` as a whole.

## Follow-up C: pre-release state, no Ego lookups outside GitHub, retired tag names

Maintainer direction after Follow-up B. All five earlier releases and tags were deleted on GitHub; their names are retired for ever (the Go proxy and checksum database keep the old content). Until further notice nothing here may query `proxy.golang.org`, `sum.golang.org` or `pkg.go.dev` for an Ego path, so every local `go` command ran with `GOPRIVATE` and `GONOSUMDB` set to `github.com/getsyntegrity/*`, and `modwait` was exercised through its unit tests only.

- [x] C1 Retired-names guard, test first (RED: `undefined: readRetiredTags`, `checkNotRetired`). `scripts/ci/retired-tags.txt` holds the five names; `internal/cmd/releaseplan` gains `-retired` (refuses to plan a retired tag and refuses when one exists in `-tags`) and `-continuation-check-not-retired <tag>`; wired into the `build.yml` dry run, `release-publishers.yml` (the `ego_version` check and both publisher plan computations) and a new first step of `release.yml`'s `gate` job.
- [x] C2 Development version: nested modules require `github.com/getsyntegrity/ego v0.0.0` (local `replace` kept). `verify-consumer.sh` keeps its synthetic root tag `v0.0.0` (it must equal what the publishers require) and publisher tag `v0.2.0`, refuses a retired name, and stays fully local (bare clone plus `GOPRIVATE`). `release-publishers.yml` text no longer promises versions.
- [x] C3 Pre-release docs: `CHANGELOG.md` restarted with only an Unreleased section; `readme.md` has no pkg.go.dev badge or link and no installable version; `docs/ci.md` describes the scheme, the retired names and an audit table of CI steps; `renovate.json` ignores Ego modules.

Judgement calls and open decisions for the maintainers:

- The guard turns the release-publishers default (`bump: minor`, plans `publisher/<name>/v0.1.0`) into a refusal, because `v0.1.0` of every publisher is retired. The first real publisher release therefore needs another bump or version; I did not choose one. Rejected: silently skipping retired versions (it would hide the collision).
- The guard also refuses when a retired name shows up in `-tags` (someone pushed it again), not only when it is planned.
- The synthetic root tag in `verify-consumer.sh` is `v0.0.0`, not `v0.1.0`, because Go resolves the root at exactly the version the publishers require.
- `govulncheck` reads `vuln.go.dev`, which is not one of the restricted services; noted in the audit table rather than changed.

Not run, because each would query an Ego path on the proxy, sumdb or pkg.go.dev: the real `modwait` probe, `verify-published.sh`, `go get` of any Ego version, any public install test, pkg.go.dev or Go Report Card renders. Also not run locally: `golangci-lint` (fails to load packages here) and `govulncheck`.
