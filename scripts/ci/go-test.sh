#!/usr/bin/env bash
set -euo pipefail

# go-test.sh runs the package selection that internal/cmd/ciselect wrote to
# an out-dir, as native `go test` coverage (replacing go-acc).
#
# It reads <out-dir>/mode and, when mode is not "none", <out-dir>/packages.txt
# (the packages to test, one import path per line) and <out-dir>/coverpkg
# (the comma-joined coverage denominator, which is always every included
# package regardless of mode, so coverage numbers stay comparable across
# runs). It then runs:
#
#   go test -timeout 30m -covermode=atomic -coverpkg=<coverpkg> \
#     -coverprofile=<coverage-out> [-race] <packages...>
#
# Environment:
#   GO_TEST_RACE   "1" (default) adds -race; set to "0" to disable it, e.g.
#                  to reproduce a run locally, where -race must never be
#                  used per this repository's local testing rules.
#   GOFLAGS        passed through as-is. CI sets GOFLAGS=-mod=vendor; a
#                  local run without vendor/ can leave it unset.
#
# Usage: go-test.sh <out-dir> [coverage-out-path]
#
# Fails closed: if mode is not "none" but packages.txt is empty, or the
# out-dir is missing its expected files, this exits non-zero rather than
# silently testing nothing.

usage() {
  echo "usage: $0 <out-dir> [coverage-out-path]" >&2
  exit 2
}

if [ "$#" -lt 1 ]; then
  usage
fi

out_dir=$1
coverage_out=${2:-coverage.out}

mode_file="$out_dir/mode"
packages_file="$out_dir/packages.txt"
coverpkg_file="$out_dir/coverpkg"

if [ ! -f "$mode_file" ]; then
  echo "go-test.sh: missing $mode_file (run ciselect first)" >&2
  exit 1
fi

mode=$(cat "$mode_file")

if [ "$mode" = "none" ]; then
  echo "go-test.sh: mode is 'none'; nothing to test."
  exit 0
fi

if [ ! -f "$packages_file" ]; then
  echo "go-test.sh: missing $packages_file" >&2
  exit 1
fi
if [ ! -f "$coverpkg_file" ]; then
  echo "go-test.sh: missing $coverpkg_file" >&2
  exit 1
fi

packages=$(cat "$packages_file")
coverpkg=$(cat "$coverpkg_file")

if [ -z "$packages" ]; then
  echo "go-test.sh: mode is '$mode' but $packages_file is empty; failing closed." >&2
  exit 1
fi
if [ -z "$coverpkg" ]; then
  echo "go-test.sh: $coverpkg_file is empty; failing closed." >&2
  exit 1
fi

race_flags=()
if [ "${GO_TEST_RACE:-1}" = "1" ]; then
  race_flags=(-race)
fi

# shellcheck disable=SC2086 # $packages is intentionally word-split, one
# import path per line, into separate `go test` positional arguments.
go test -timeout 30m -covermode=atomic -coverpkg="$coverpkg" \
  -coverprofile="$coverage_out" "${race_flags[@]}" $packages
