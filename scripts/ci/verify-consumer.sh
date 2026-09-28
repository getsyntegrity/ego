#!/usr/bin/env bash
set -euo pipefail

# verify-consumer.sh proves that every published module path in this
# repository resolves the way an outside consumer resolves it: a plain
# `go get`, with no local `replace`, against the exact tags a release
# would create (#134). It is the local-remote, pre-tag equivalent of
# scripts/ci/verify-published.sh's release-time check.
#
# Why `go list -m <path>@<tag>` alone is not acceptance evidence: it only
# asks the VCS whether *some* module exists at that path and tag; it
# succeeds even when the tagged commit's own go.mod declares a different
# module path (exactly the defect this check was written to catch), and
# it never executes a single line of the module's code, so a corrupted
# generated file compiles but is never caught. A hand-edited protobuf
# `go_package` is one such case: the path sits inside a length-prefixed
# serialized descriptor, so a text rename still compiles but panics at
# init ("slice bounds out of range [-4:]", measured in the pre-migration
# spike for this change). Only `go get` (which validates the module's own
# declared path against the requested import path), plus `go build` and
# `go run` (which executes every package's init), catch both failure
# modes; this script requires all three.
#
# It never talks to the public module proxy or checksum database for
# this repository's own module paths: it clones the committed HEAD into
# a temporary bare repository, creates the release tags only there, and
# redirects this repository's module path (GOPRIVATE plus a git
# url.insteadOf rule) to that local clone through an isolated git
# configuration. Because no tag is pushed to the real repository, the
# public-proxy path stays unverified until the root tag is actually
# published; that check is scripts/ci/verify-published.sh, run from the
# release flow once a real tag exists.
#
# It checks the committed HEAD only: uncommitted changes in the working
# tree (staged or not) are not part of the clone this script verifies,
# because `git clone` reads from .git, not from the working tree.
#
# Usage: scripts/ci/verify-consumer.sh
#   (no arguments; run from anywhere inside the repository)
#
# Environment:
#   VERIFY_CONSUMER_PUBLISHER_VERSION  the tag created for each released
#                                      publisher directory in the
#                                      temporary bare clone (default
#                                      "v0.1.0"). The root tag is never
#                                      configurable here: it is always the
#                                      version every released publisher's
#                                      go.mod already requires for the
#                                      root module, discovered below.
#
# Everything else (GOPRIVATE, GOPROXY, GOFLAGS, GOMODCACHE, the isolated
# git config) is set locally to this script's own temporary directory and
# does not read or change the caller's real configuration, with two
# deliberate exceptions: GOCACHE (the ordinary build cache) is left alone
# and reused, and, when the caller already has a populated module
# download cache, its "cache/download" directory is added to GOPROXY as a
# read-only source for third-party modules, purely to avoid re-downloading
# the world on every local run (GOPRIVATE already bypasses any proxy for
# this repository's own paths, so this can never mask a stale copy of
# them).

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/../.." && pwd)

export GOWORK=off

if ! command -v jq >/dev/null 2>&1; then
  echo "verify-consumer.sh: jq is required but not found on PATH" >&2
  exit 1
fi

modjson() {
  # modjson <go.mod path> prints `go mod edit -json` for that file,
  # regardless of the caller's current directory.
  GOFLAGS=-mod=mod go mod edit -json -modfile="$1"
}

root_gomod="$repo_root/go.mod"
if [ ! -f "$root_gomod" ]; then
  echo "verify-consumer.sh: $root_gomod not found" >&2
  exit 1
fi

root_module_path=$(modjson "$root_gomod" | jq -r '.Module.Path')
if [ -z "$root_module_path" ] || [ "$root_module_path" = "null" ]; then
  echo "verify-consumer.sh: could not read the module path from $root_gomod" >&2
  exit 1
fi

# The VCS repository identity is the module path with any trailing
# "/vN" major-version suffix removed, e.g.
# "github.com/getsyntegrity/ego/v4" -> "github.com/getsyntegrity/ego".
if [[ "$root_module_path" =~ ^(github\.com/[^/]+/[^/]+)(/v[0-9]+)?$ ]]; then
  repo_path="${BASH_REMATCH[1]}"
else
  echo "verify-consumer.sh: root module path '$root_module_path' does not look like github.com/<owner>/<repo>[/vN]" >&2
  exit 1
fi

release_list="$script_dir/release-modules.txt"
if [ ! -f "$release_list" ]; then
  echo "verify-consumer.sh: $release_list not found" >&2
  exit 1
fi

# Non-comment, non-blank, non-"." lines are the released publisher
# directories (scripts/ci/release-modules.txt documents "." as the root
# module and excludes benchmark/example/cluster/test/compat: they are
# never released, D5).
mapfile -t publisher_dirs < <(grep -vE '^[[:space:]]*(#|$)' "$release_list" | grep -vxF '.')

if [ "${#publisher_dirs[@]}" -eq 0 ]; then
  echo "verify-consumer.sh: no released publisher directories found in $release_list" >&2
  exit 1
fi

publisher_paths=()
root_version=""
for d in "${publisher_dirs[@]}"; do
  gomod="$repo_root/$d/go.mod"
  if [ ! -f "$gomod" ]; then
    echo "verify-consumer.sh: $d has no go.mod (listed in $release_list)" >&2
    exit 1
  fi

  pub_json=$(modjson "$gomod")
  pub_path=$(jq -r '.Module.Path' <<<"$pub_json")
  pub_root_version=$(jq -r --arg root "$root_module_path" \
    '(.Require // [])[] | select(.Path == $root) | .Version' <<<"$pub_json")

  if [ -z "$pub_path" ] || [ "$pub_path" = "null" ]; then
    echo "verify-consumer.sh: could not read the module path from $gomod" >&2
    exit 1
  fi
  if [ -z "$pub_root_version" ]; then
    echo "verify-consumer.sh: $d's go.mod has no require line for $root_module_path" >&2
    exit 1
  fi

  if [ -z "$root_version" ]; then
    root_version="$pub_root_version"
  elif [ "$pub_root_version" != "$root_version" ]; then
    echo "verify-consumer.sh: released publishers require different root versions: $d requires $pub_root_version, but an earlier publisher requires $root_version; all released publishers must require the same root version" >&2
    exit 1
  fi

  publisher_paths+=("$pub_path")
done

publisher_tag_version="${VERIFY_CONSUMER_PUBLISHER_VERSION:-v0.1.0}"

head_sha=$(git -C "$repo_root" rev-parse HEAD)
if ! git -C "$repo_root" diff --quiet HEAD -- 2>/dev/null; then
  echo "verify-consumer.sh: note: the working tree has uncommitted changes; this check verifies the committed HEAD ($head_sha) only." >&2
fi

work_dir=$(mktemp -d)
cleanup() {
  # The module cache this script populates is read-only by design
  # (GOFLAGS=-modcacherw below makes it writable, but this is a
  # defensive fallback for any Go version or step that ignores that
  # flag).
  chmod -R u+w "$work_dir" 2>/dev/null || true
  rm -rf "$work_dir"
}
trap cleanup EXIT

bare_repo="$work_dir/repo.git"
git clone -q --bare "$repo_root" "$bare_repo"

# Tags are created only in this temporary bare clone, never in the real
# repository: the root tag is the version every released publisher
# already requires; each publisher gets "<dir>/<publisher_tag_version>";
# and the first publisher additionally gets a "/v2.0.0" tag with no "/v2"
# path suffix, which Go's major-version rule must reject (negative case).
git -C "$bare_repo" tag "$root_version" "$head_sha"
for d in "${publisher_dirs[@]}"; do
  git -C "$bare_repo" tag "$d/$publisher_tag_version" "$head_sha"
done
first_publisher_dir="${publisher_dirs[0]}"
first_publisher_path="${publisher_paths[0]}"
git -C "$bare_repo" tag "$first_publisher_dir/v2.0.0" "$head_sha"

git_config_global="$work_dir/gitconfig"
export GIT_CONFIG_GLOBAL="$git_config_global"
export GIT_CONFIG_NOSYSTEM=1
export GIT_TERMINAL_PROMPT=0
git config -f "$git_config_global" protocol.file.allow always
git config -f "$git_config_global" url."file://$bare_repo".insteadOf "https://$repo_path"

export GOPRIVATE="$repo_path"
export GOMODCACHE="$work_dir/modcache"
mkdir -p "$GOMODCACHE"
export GOFLAGS="-mod=mod -modcacherw"

host_modcache=$(env -u GOMODCACHE go env GOMODCACHE)
if [ -d "$host_modcache/cache/download" ]; then
  export GOPROXY="file://$host_modcache/cache/download,https://proxy.golang.org,direct"
fi

consumer_dir="$work_dir/consumer"
mkdir -p "$consumer_dir"
(cd "$consumer_dir" && go mod init example.com/verifyconsumer) >/dev/null

{
  echo "package main"
  echo
  echo "import ("
  echo "	\"fmt\""
  echo
  echo "	_ \"$root_module_path\""
  for p in "${publisher_paths[@]}"; do
    echo "	_ \"$p\""
  done
  echo ")"
  echo
  echo "func main() {"
  echo "	// A blank import runs every imported package's init; this is what"
  echo "	// catches a corrupted generated descriptor that a plain build"
  echo "	// cannot, since a build alone never executes init."
  echo "	fmt.Println(\"verify-consumer: resolved, built and ran every published module path with no local replace\")"
  echo "}"
} >"$consumer_dir/main.go"

(
  cd "$consumer_dir"
  for p in "${publisher_paths[@]}"; do
    go get "$p@$publisher_tag_version"
  done
  go mod tidy
  go build ./...
)

echo "verify-consumer.sh: running the consumer binary..."
(cd "$consumer_dir" && go run .)

consumer_gomod="$consumer_dir/go.mod"
if grep -qE '^[[:space:]]*replace[[:space:]]' "$consumer_gomod"; then
  echo "verify-consumer.sh: consumer go.mod unexpectedly contains a replace directive" >&2
  exit 1
fi

module_graph=$(cd "$consumer_dir" && go list -m all)
if ! grep -qxF "$root_module_path $root_version" <<<"$module_graph"; then
  echo "verify-consumer.sh: consumer's module graph does not show $root_module_path at $root_version" >&2
  echo "$module_graph" >&2
  exit 1
fi
for p in "${publisher_paths[@]}"; do
  if ! grep -qxF "$p $publisher_tag_version" <<<"$module_graph"; then
    echo "verify-consumer.sh: consumer's module graph does not show $p at $publisher_tag_version" >&2
    echo "$module_graph" >&2
    exit 1
  fi
done

for i in "${!publisher_dirs[@]}"; do
  d="${publisher_dirs[$i]}"
  p="${publisher_paths[$i]}"
  ref="$p@$publisher_tag_version"
  if ! subdir=$(cd "$consumer_dir" && go list -m -f '{{.Origin.Subdir}}' "$ref" 2>/dev/null); then
    # Older go tool versions may not support -f on a nested Origin
    # field; fall back to parsing -json for the same value.
    subdir=$(cd "$consumer_dir" && go list -m -json "$ref" | jq -r '.Origin.Subdir // empty')
  fi
  if [ "$subdir" != "$d" ]; then
    echo "verify-consumer.sh: $ref resolved from subdirectory '$subdir', expected '$d'" >&2
    exit 1
  fi
done

# Negative case: a "/v2.0.0" tag on a module path with no "/v2" suffix
# must be rejected by Go's major-version rule, never silently resolved.
if (cd "$consumer_dir" && go list -m "$first_publisher_path@v2.0.0" >/dev/null 2>&1); then
  echo "verify-consumer.sh: $first_publisher_path@v2.0.0 unexpectedly resolved; a v2+ tag on a module path with no matching /v2 suffix must be rejected" >&2
  exit 1
fi

echo "verify-consumer.sh: OK"
echo "  root:       $root_module_path@$root_version"
for i in "${!publisher_dirs[@]}"; do
  echo "  publisher:  ${publisher_paths[$i]}@$publisher_tag_version (${publisher_dirs[$i]})"
done
echo "  negative:   $first_publisher_path@v2.0.0 correctly rejected"
