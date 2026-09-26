#!/usr/bin/env bash
set -euo pipefail

# verify-published.sh proves a nested module builds against a real
# published version of the root module — "published verification" in
# openspec/changes/ego-arch-001/design.md §8, as opposed to the
# "integrated verification" scripts/ci/verify-module.sh performs against
# the checked-in `replace ../../` (or `../`) directive. It is a release
# condition, not a PR gate: release.yml runs it for each publisher right
# before tagging.
#
# It never touches the module as checked out: it copies it into a scratch
# directory, drops the local replace there, requires the given root
# version instead, and builds. A missing published version fails with one
# clear ::error:: line instead of a confusing go.sum/build failure.
#
# Usage: verify-published.sh <module-dir> <ego-version>
#   <module-dir>   repo-relative path to the nested module, e.g.
#                  "publisher/kafka".
#   <ego-version>  the root module version to verify against, e.g.
#                  "v4.5.0" (no local replace may satisfy this: it must
#                  already be resolvable from the module proxy).

usage() {
  echo "usage: $0 <module-dir> <ego-version>" >&2
  exit 2
}

if [ "$#" -ne 2 ]; then
  usage
fi

module_dir=$1
ego_version=$2
root_module="github.com/pablogore/ego/v4"

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/../.." && pwd)
src="$repo_root/$module_dir"

if [ ! -f "$src/go.mod" ]; then
  echo "verify-published.sh: $module_dir has no go.mod" >&2
  exit 1
fi

export GOWORK=off
export GOFLAGS=-mod=mod
export GOPROXY=https://proxy.golang.org,direct

if ! go list -m "${root_module}@${ego_version}" >/dev/null 2>&1; then
  echo "::error::root module ${root_module} ${ego_version} is not published; local replace cannot be used for release verification" >&2
  exit 1
fi

work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT
cp -r "$src/." "$work_dir/"

(
  cd "$work_dir"
  # -dropreplace and -require together, in one `go mod edit` (a pure text
  # edit, no network): dropping the replace first and only then trying to
  # "go get" the new version would still have `go` resolve the *old*,
  # unpublished require line first, and fail on it before the update ever
  # applies. Editing both directives at once means the module graph is
  # never built against the unpublished version at all.
  go mod edit -dropreplace="$root_module" -require="${root_module}@${ego_version}"
  go mod tidy
  go build ./...
)

echo "verify-published.sh: $module_dir builds against published ${root_module}@${ego_version}"
