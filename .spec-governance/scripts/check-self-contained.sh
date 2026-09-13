#!/usr/bin/env bash
# Self-check: is .spec-governance/skills/ actually self-contained?
#
# Run from anywhere; it locates its own package root. Exit 0 = clean,
# exit 1 = at least one violation printed to stderr.
#
# Checks:
#   1. No hardcoded references to the installed/runtime copy (~/.claude,
#      or an absolute /Users/... path) inside the canonical package.
#   2. Every `_shared/<file>.md` reference made by a skill in this package
#      resolves to a file that actually exists under skills/_shared/ here
#      — i.e. no skill silently depends on a _shared file this package
#      chose not to include.
#
# This does not replace judgment: a new skill added to the package must
# still be checked for *new* dependencies before it's wired in (see
# README.md "What's in this package, and why"). It only catches drift
# from what's already documented.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PKG_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SKILLS_DIR="$PKG_ROOT/skills"

fail=0

echo "Checking $SKILLS_DIR for references outside the package..."

# 1. Hardcoded paths to the installed copy or the local machine.
hardcoded="$(grep -rn '~/\.claude\|/Users/[A-Za-z0-9_-]*' "$SKILLS_DIR" 2>/dev/null || true)"
if [ -n "$hardcoded" ]; then
  echo "FAIL: hardcoded installed-copy or local-machine path(s) found:" >&2
  echo "$hardcoded" >&2
  fail=1
else
  echo "OK: no hardcoded ~/.claude or /Users/... paths."
fi

# 2. Every referenced _shared/<file>.md must exist in this package.
referenced="$(grep -rohE '_shared/[a-zA-Z0-9_-]+\.md' "$SKILLS_DIR" 2>/dev/null | sort -u || true)"
missing=0
while IFS= read -r ref; do
  [ -z "$ref" ] && continue
  if [ ! -f "$SKILLS_DIR/$ref" ]; then
    echo "FAIL: referenced but missing from package: $ref" >&2
    missing=1
  fi
done <<< "$referenced"

if [ "$missing" -eq 0 ]; then
  echo "OK: every referenced _shared/*.md file is present in the package."
else
  fail=1
fi

if [ "$fail" -eq 0 ]; then
  echo "Self-check passed: package is self-contained."
  exit 0
else
  echo "Self-check FAILED — see above." >&2
  exit 1
fi
