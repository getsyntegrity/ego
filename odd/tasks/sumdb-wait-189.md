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
- [x] T2 Bounded wait loop (injected prober, clock, sleeper; flags, `-timeout 0`, sleep clamp, `::error::` messages) and the real prober (`go mod download -json` with a scrubbed environment and temp dirs cleaned; tests inspect env and args). Check: table tests without network, fake `go` binary for the exec plumbing. Tasks T2 and the former "real prober" task share one commit because `package main` needs `main()` and the real prober to build; splitting would leave a non-building commit.
- [ ] T3 Wire `release.yml`; actionlint, YAML parse, `bash -n`, no `${{` in run bodies, diff limited to the wait step.
- [ ] T4 `docs/ci.md` section, live read-only probes, final verification, SHAs in this document.

## Acceptance criteria

The list under "Requirements" of #189 and the maintainer's brief: immediate success, 404 then success, 404 until timeout, 410 and checksum mismatch fail without retry, 5xx/429/network then success, explicit limit, resume instructions, actionlint clean.

## Evidence

(Filled in as tasks close.)
