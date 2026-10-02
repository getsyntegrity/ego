# Feature: move the testpb fixture from test/data to internal (#283, part 1)

Branch: `refactor/internal-testpb` · Base: `ci/examples-job` · Issue: #283 (part 1 only; part 2 is out of scope here)

## Problem

`test/data/testpb` is a generated Go package of protobuf messages (`Account`, `AccountCreated`, ...) that only the tests of this repository use. It lives under `test/data`, a public import path of the root module, so any consumer can import `github.com/getsyntegrity/ego/test/data/testpb` and would then depend on a test fixture we never promised to keep. Its location also hides that it is a fixture: nothing in the path says "internal".

## What changes

The package moves to `internal/testpb`, next to `internal/samplepb`, the other generated fixture. Go's `internal` rule then makes the package importable only from inside the `github.com/getsyntegrity/ego` module tree. Nested modules (`inttest`, `benchmark`, `example`, the publishers, `persistence/postgres`) have module paths under that prefix, so they can still import it; the compiler confirms this.

Concretely: the `go_package` option in `protos/test/test.proto` points to the new path, `make proto` and `make docker-protogen` copy the generated file to the new directory, `test.pb.go` is regenerated with the same tools, and every importer switches to the new import path. The Go package name stays `testpb`, and the proto package and message full names do not change, so no wire format or registry name moves.

## Why

It narrows the public surface to what we intend to support, and it makes the fixture's purpose visible from its path. The rejected alternative is leaving the package where it is and documenting it as unsupported: that keeps the accidental API alive and relies on readers noticing the note.

## Scope and constraints

- In scope: `protos/test/test.proto`, `Makefile`, the regenerated `internal/testpb/test.pb.go`, `internal/testpb/descriptor_test.go`, every importer, two hard-coded path strings in `internal/runtimeconsumer` (its dependency allowlist and a doc comment), and `docs/testing/unit-migration.md`.
- Out of scope: `test/compat` (analyzed by someone else) and part 2 of #283.
- No behavior change. go-specs v0.3.3 in any touched test, no testify. Never `-race`, never the workbench. No push, no PR.
- TDD: strict by user configuration; runner `go test`. This is a pure relocation, so the existing `descriptor_test.go` (it asserts the embedded `go_package`) is updated to the new path and acts as the guard.
- Route: delegated writer (2+ files; mechanical, 60+ importers). RDD: off (global).

## Tasks

- [x] T1 Move and regenerate: `git mv test/data/testpb internal/testpb`, update `go_package`, `Makefile` and the descriptor test, regenerate with buf v1.69.0 and protoc-gen-go v1.36.12.
- [x] T2 Importers: rewrite the import path in every Go file, restore import ordering with `gofmt`, update the hard-coded allowlist in `internal/runtimeconsumer/closure_test.go`. T1 and T2 land in one commit because the tree does not build between them.
- [x] T3 Docs: `docs/testing/unit-migration.md` heading moved to `internal/testpb` in sorted position; a repo-wide search for the old path is empty outside historical records.

## Progress and evidence

### T1 and T2

- Generator: `buf` 1.69.0 and `protoc-gen-go` v1.36.12 (same version as the old file header), installed under `/tmp/gentools`. Command sequence is the `proto` Makefile target.
- Generated-file diff against the old file: only the embedded raw descriptor changes, namely the `go_package` string (`.../test/data/testpb;testpb` to `.../internal/testpb;testpb`) and the two length-prefix bytes that depend on it (`\x87` to `\x86`, `Z4` to `Z3`). The header, proto package `testpb` and message full names are identical.
- Hidden coupling found: `internal/runtimeconsumer/closure_test.go` lists allowed first-party dependencies as a string and still named the old path; updated.

### T3 and persistence/conformance

- `persistence/conformance` uses `testpb` only inside the unexported helpers in `helpers.go`. Its exported functions and types take and return `persistence` types, `*testing.T`, `CheckResult` and similar; none exposes a `testpb` type. External importers of the conformance package are unaffected by the move.

### Verification

Recorded in the final report of the writer; see the commit messages for the commit identities.
