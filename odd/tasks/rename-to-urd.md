# Rename the project `ego` to `urd`

## Problem and goal

The project ships as `github.com/getsyntegrity/ego`. It is being rebranded as
**Urd — event sourcing for Go**, with the module path
`github.com/getsyntegrity/urd`. This branch prepares the code, docs and CI;
the GitHub repository rename, tags and releases are done manually by the
maintainer (see T5).

The rename must not break data already persisted by `ego` users, nor rolling
upgrades between old and new nodes. So names that are stored or sent on the
wire keep their `ego` spelling; only Go paths, labels, docs and free-text
messages change.

## Decisions (approved 2026-10-02)

| ID | Item | Decision |
|----|------|----------|
| E1 | proto `package egopb` | keep (type names are persisted in `Any` payloads) |
| E2 | Go package/dir `egopb` | keep (buf lint `PACKAGE_SAME_GO_PACKAGE` ties it to E1) |
| E3 | metadata keys `ego.cmd.*`, `ego.tenant.*`, `ego.adoption.receipt`, reserved prefix `ego.` | keep (persisted; strict unmarshal) |
| E4 | GoAkt extension ID strings | keep the strings; Go constant names may change |
| E5 | OpenTelemetry metric/span/attribute names | keep for now (assumed; no answer given) |
| E6 | NATS stream names `ego-events`, `ego-durable-states` | keep (server-side state) |
| E7 | publisher IDs / Kafka client ID (`ego-kafka`, ...) | rename to `urd-*` |
| E8 | Postgres table `ego_schema_migrations`, advisory lock key | keep |
| E9 | env vars `EGO_*` | rename to `URD_*`, still read `EGO_*` as a fallback |
| E10 | parsed error grammar `ego: concurrency conflict: grammar=v1` | keep; free-text `ego:` / `eGo:` messages become `urd:` |
| E11 | `openspec/`, `odd/tasks/` history, `.spec-governance/`, past CHANGELOG entries | keep as history |
| E12 | first version | `v0.1.0` for every module; old tags reset by the maintainer |

There is no root Go package (the repo root has no `.go` files, enforced by
archcheck), so no `package urd` is introduced.

## Tasks

TDD: strict mode on (user global config). Runner: `go test ./...` per module.
Race detector only where the user asked: `go test -race -count=1 ./...` on
the root module in T4.

- [x] T1 — Module paths: rewrite every `go.mod`, import, `replace`, hardcoded
  module-path string (closure/architecture tests, `unitgate/scan.go`,
  `benchmark/Makefile`), proto `go_package` options and
  `buf.gen.example.yaml`; regenerate with `make docker-protogen`;
  `go mod tidy` in every module. Check: `go build ./... && go vet ./...` in
  every module. Route: delegated (writer trigger: hundreds of files).
- [ ] T2 — Approved group-E changes: E7 publisher IDs, E9 env vars with
  fallback, E10 free-text error messages, local `ego*` identifiers and
  `Package ego` doc comments. Check: unit tests per module. Route: delegated.
- [ ] T3 — Docs, CI and branding: README (title, tagline, install, "Formerly
  ego" note with sed migration), badges, docs/, `.github/` templates and
  SECURITY, Makefiles, Dockerfiles, `example/cluster` k8s/Makefile, workflows,
  scripts; add `MIGRATION.md`. Check: structural readback, the grep in T4.
  Route: delegated.
- [ ] T4 — Verification: build/vet/test per module, race on root, leftover
  `ego` grep with every hit explained, `go list -m all` has no old path,
  `git diff develop... --stat` has no unrelated changes. Route: delegated.
- [ ] T5 — Hand-off checklist for the maintainer (deprecation commit on the
  old path, repo rename, remote update, tag reset and `v0.1.0` tags,
  pkg.go.dev indexing, external references). Route: inline.

## Progress

Base: `origin/develop` at `afaafb3`. Branch: `refactor/rename-to-urd`.

- T1 done (dfe3462 module path, ce18478 proto go_package + regen). `make docker-protogen` ran; pb.go changed only the go_package string (same length, no version-header changes). Per module `go build ./... && go vet ./...` clean (10 modules); root `go test ./...` 39 packages ok, none failing. Also fixed `.github/scripts/count-tests.sh`. Leftover `getsyntegrity/ego` only in `.github/ISSUE_TEMPLATE/*.yml` (URLs, T3).
