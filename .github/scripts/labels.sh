#!/usr/bin/env bash
# Creates (or updates) the labels used by the templates, the CI and the release notes.
# Idempotent. Requires an authenticated gh:  gh auth login
# Usage: .github/scripts/labels.sh [owner/repo]
set -euo pipefail
repo="${1:-$(gh repo view --json nameWithOwner -q .nameWithOwner)}"

while IFS='|' read -r name color desc; do
  [ -z "$name" ] && continue
  gh label create "$name" --repo "$repo" --color "$color" --description "$desc" --force >/dev/null
  echo "  ✓ $name"
done <<'LABELS'
kind/bug|d73a4a|Something is not working as it should
kind/feature|a2eeef|New functionality or API
kind/breaking|b60205|Breaks public API compatibility
kind/deprecation|fbca04|Deprecates existing API
kind/deps|0366d6|Dependency or Go SDK update
kind/chore|c5def5|Internal maintenance with no impact on consumers
kind/docs|0075ca|Documentation
kind/flake|f9d0c4|Flaky test or broken CI
needs-triage|ededed|Pending review by the team
skip-changelog|eeeeee|Does not appear in the release notes
release:major|b60205|Forces a major bump in the release to main
release:minor|1d76db|Forces a minor bump in the release to main
release:patch|0e8a16|Forces a patch bump in the release to main
LABELS
echo "Labels ready in $repo"
