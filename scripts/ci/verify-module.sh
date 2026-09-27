#!/usr/bin/env bash
set -euo pipefail

# verify-module.sh downloads, checks go.mod/go.sum tidiness, builds, vets,
# lints (against the root .golangci.yml) and, when the module has any
# *_test.go file, tests one nested Go module: a directory with its own
# go.mod, outside the root module's `go list ./...` graph and therefore
# outside internal/cmd/ciselect's own coverage (see docs/ci.md). It
# verifies the module the way it is checked out today, with its local
# `replace` directives in effect ("integrated verification" in
# openspec/changes/ego-arch-001/design.md §8) — release verification
# against a published root version is scripts/ci/verify-published.sh, a
# separate, stricter check.
#
# When the module has a file gated behind the `compat` build tag, it also
# vets, lints and tests that file with `-tags compat` (docs/ci.md,
# "Compatibility lane", #122) — a separate lane for historical checks that
# must not enter the module's default unit-test closure. `go mod tidy` has
# no `-tags` flag: it already considers every file in the module,
# including compat-tagged ones, when computing required modules (verified
# empirically for #122: `go mod tidy -diff` was empty in every publisher
# both before and after the `compat` tag was introduced), so the tidiness
# check below needs no `-tags compat` variant of its own.
#
# Usage: verify-module.sh <module-dir>
#   <module-dir>   repo-relative path to the nested module, e.g.
#                  "publisher/kafka" or "example/cluster".
#
# Environment:
#   GO_TEST_RACE         "1" adds -race to `go test` (CI sets this for the
#                        module matrix job); default "0", so a local run
#                        never uses the race detector, per this
#                        repository's own local-testing rule.
#   GITHUB_STEP_SUMMARY  when set, a short markdown block naming the
#                        module, the steps that ran, and whether tests
#                        were skipped is appended to it.
#
# GOWORK=off is forced throughout so a stray go.work at the repository
# root can never pull a nested module's build into the root module's own
# graph. GOFLAGS is cleared throughout for the same reason a stray
# GOWORK is guarded against: an inherited `GOFLAGS=-mod=vendor` would
# make `go mod tidy` refuse outright (nested modules keep no vendor/
# directory, per docs/ci.md) and would silently change the build/vet/test
# steps too; this job never sets GOFLAGS itself (see docs/ci.md, "The
# `modules` matrix job"), so clearing it only guards against a caller's
# ambient environment, local or otherwise.

usage() {
  echo "usage: $0 <module-dir>" >&2
  exit 2
}

if [ "$#" -ne 1 ]; then
  usage
fi

module_dir=$1
export GOWORK=off
export GOFLAGS=

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/../.." && pwd)
module_path="$repo_root/$module_dir"

if [ ! -f "$module_path/go.mod" ]; then
  echo "verify-module.sh: $module_dir has no go.mod" >&2
  exit 1
fi

steps=()

cd "$module_path"

echo "::group::go mod download ($module_dir)"
go mod download
echo "::endgroup::"
steps+=("go mod download")

echo "::group::go mod tidy -diff ($module_dir)"
# `-diff` never writes go.mod/go.sum; it prints the pending change as a
# unified diff and exits non-zero when one exists (#122, acceptance
# criterion: go.mod/go.sum tidiness is a CI gate, not a manual step).
if ! tidy_diff=$(go mod tidy -diff); then
  echo "$tidy_diff"
  echo "::endgroup::"
  echo "verify-module.sh: $module_dir's go.mod/go.sum are not tidy." >&2
  echo "Run 'go mod tidy' inside $module_dir and commit the result; see the diff above for what changes." >&2
  exit 1
fi
echo "::endgroup::"
steps+=("go mod tidy -diff")

echo "::group::go build ($module_dir)"
# A module with at least one `package main` (e.g. example/cluster) needs
# an explicit -o scratch directory: a bare `go build ./...` would
# otherwise drop a binary straight into the module's own working tree
# (example/cluster/cluster). A build-only, `go list` requires no module
# download beyond what "go mod download" above already fetched.
# `go build -o <dir>/ ./...` itself refuses with "no main packages to
# build" for a library-only module (every publisher today), so -o is only
# passed when a main package actually exists.
if go list -f '{{.Name}}' ./... | grep -qx main; then
  build_out=$(mktemp -d)
  trap 'rm -rf "$build_out"' EXIT
  go build -o "$build_out/" ./...
else
  go build ./...
fi
echo "::endgroup::"
steps+=("go build ./...")

echo "::group::go vet ($module_dir)"
go vet ./...
echo "::endgroup::"
steps+=("go vet ./...")

echo "::group::golangci-lint ($module_dir)"
golangci-lint run --modules-download-mode=mod --config "$repo_root/.golangci.yml" ./...
echo "::endgroup::"
steps+=("golangci-lint run")

# Compatibility lane (#122): a nested module may keep a historical
# check — today, the four publishers' ego-alias/sentinel assertions in
# compat_test.go (ADR ego-arch-001, S1 criterion 3) — behind the `compat`
# build tag, specifically so it never enters the default unit-test closure
# verified below: `go list -deps -test ./...` with no -tags must never see
# the GoAkt runtime or the root package `ego` again (docs/ci.md,
# "Compatibility lane"). Detecting the tag by content, rather than
# hard-coding module or file names, means any future compat-tagged file
# gains this lane the moment it exists, with no script edit.
compat_tagged=$(grep -rl '^//go:build compat$' --include='*.go' . 2>/dev/null || true)
if [ -n "$compat_tagged" ]; then
  echo "::group::go vet -tags compat ($module_dir)"
  go vet -tags compat ./...
  echo "::endgroup::"
  steps+=("go vet -tags compat ./... (compatibility lane, #122)")

  echo "::group::golangci-lint -tags compat ($module_dir)"
  golangci-lint run --modules-download-mode=mod --build-tags compat --config "$repo_root/.golangci.yml" ./...
  echo "::endgroup::"
  steps+=("golangci-lint run -tags compat (compatibility lane, #122)")
fi

tests_note=""
if [ -n "$(find . -name '*_test.go' -print -quit)" ]; then
  race_flags=()
  if [ "${GO_TEST_RACE:-0}" = "1" ]; then
    race_flags=(-race)
  fi
  echo "::group::go test ($module_dir)"
  # shellcheck disable=SC2068 # race_flags is intentionally an empty or
  # one-element array, never a string to word-split.
  go test ${race_flags[@]+"${race_flags[@]}"} ./...
  echo "::endgroup::"
  steps+=("go test ./...")

  if [ -n "$compat_tagged" ]; then
    echo "::group::go test -tags compat ($module_dir)"
    # shellcheck disable=SC2068
    go test -tags compat ${race_flags[@]+"${race_flags[@]}"} ./...
    echo "::endgroup::"
    steps+=("go test -tags compat ./... (compatibility lane, #122)")
  fi
else
  tests_note="no tests"
fi

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  {
    echo "### verify-module: \`$module_dir\`"
    echo
    for s in "${steps[@]}"; do
      echo "- $s"
    done
    if [ -n "$tests_note" ]; then
      echo "- go test: $tests_note"
    fi
    echo
  } >>"$GITHUB_STEP_SUMMARY"
fi
