#!/usr/bin/env bash
# Counts the tests that `go test -json` reports, per package, split into top-level tests and subtests.
#
# Usage: .github/scripts/count-tests.sh [go test flags and packages...]
#   .github/scripts/count-tests.sh ./engine ./compose/goakt/...
#   .github/scripts/count-tests.sh -skip '^TestCluster' ./engine
#   .github/scripts/count-tests.sh -run '^TestCluster' ./engine ./compose/goakt/...
#
# It prints one line per package (top-level tests, subtests, total, and how many of them failed or skipped)
# and a sum line. A test is counted once, by its final pass/fail/skip event. Needs go and jq.
set -euo pipefail

go test -count=1 -json "$@" |
  jq -rs '
    map(select(.Test != null and (.Action == "pass" or .Action == "fail" or .Action == "skip")))
    | group_by(.Package)
    | map({
        pkg: (.[0].Package | sub("^github.com/getsyntegrity/urd/"; "")),
        top: (map(select(.Test | contains("/") | not)) | length),
        sub: (map(select(.Test | contains("/"))) | length),
        fail: (map(select(.Action == "fail")) | length),
        skip: (map(select(.Action == "skip")) | length)
      })
    | (.[] | "\(.pkg)\ttop=\(.top)\tsub=\(.sub)\ttotal=\(.top + .sub)\tfail=\(.fail)\tskip=\(.skip)"),
      "SUM\ttop=\(map(.top) | add // 0)\tsub=\(map(.sub) | add // 0)\ttotal=\(map(.top + .sub) | add // 0)\tfail=\(map(.fail) | add // 0)\tskip=\(map(.skip) | add // 0)"
  '
