#!/usr/bin/env bash
# Updates the repo's Go SDK from ONE source of truth: .go-version
#
# Usage:
#   go-sdk-update.sh                  applies the current .go-version (syncs go.mod)
#   go-sdk-update.sh 1.27.1           writes .go-version and applies it
#   go-sdk-update.sh latest           uses the latest published stable version
#   go-sdk-update.sh ... --raise-min  also raises the MINIMUM version (go directive)
#   go-sdk-update.sh --resolve latest only prints the resolved version
#
# What it touches:
#   .go-version            -> the exact version (ALL workflows read it through setup-go)
#   go.mod  "toolchain"    -> go<version>: the SDK used to develop and test
#   go.mod  "go"           -> ONLY with --raise-min. In a library it is the minimum version
#                             required from consumers: raising it can break them.
#   Dockerfile*            -> golang:X.Y.Z images, if any
#   .tool-versions         -> golang line, if it exists (asdf/mise)
# And it checks that no workflow has a hand-written Go version.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

die() { echo "::error::$*" >&2; exit 1; }
lt()  { [ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -1)" = "$1" ]; }
latest() { curl -fsS 'https://go.dev/dl/?mode=json' | jq -r '[.[] | select(.stable)][0].version | ltrimstr("go")'; }

raise_min=false; version=""; resolve=false
for a in "$@"; do
  case "$a" in
    --raise-min) raise_min=true ;;
    --resolve)   resolve=true ;;
    -h|--help)   sed -n '2,20p' "$0"; exit 0 ;;
    *)           version="$a" ;;
  esac
done

[ "$version" = "latest" ] && version=$(latest)
if $resolve; then echo "$version"; exit 0; fi

# Version: argument or .go-version. EVERYTHING is validated before writing anything.
if [ -z "$version" ]; then
  [ -f .go-version ] || die "There is no .go-version (pass a version: go-sdk-update.sh 1.27.1)"
  version=$(tr -d '[:space:]' < .go-version)
fi
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "Invalid version: '$version' (format X.Y.Z)"
minor="${version%.*}"

mapfile -t mods < <(git ls-files --cached --others --exclude-standard '*go.mod' | grep -v '^vendor/')
for mod in "${mods[@]}"; do
  cur=$(awk '$1 == "go" { print $2; exit }' "$mod")
  $raise_min && lt "$cur" "${minor}.0" && cur="${minor}.0"
  lt "$version" "$cur" && die "$mod requires Go >= $cur; SDK $version cannot be used"
done

echo "$version" > .go-version
echo "Go SDK -> $version"

# ── go.mod (every module in the repo) ──
for mod in "${mods[@]}"; do
  dir=$(dirname "$mod")
  cur=$(awk '$1 == "go" { print $2; exit }' "$mod")
  if $raise_min && lt "$cur" "${minor}.0"; then
    echo "  $mod: minimum $cur -> ${minor}.0"
    (cd "$dir" && go mod edit -go="${minor}.0")
    cur="${minor}.0"
  fi
  echo "  $mod: toolchain go$version (minimum: $cur)"
  (cd "$dir" && go mod edit -toolchain="go$version" && go mod tidy)
done

# ── Dockerfiles and .tool-versions (if they exist) ──
while IFS= read -r f; do
  sed -i -E "s#(golang:)[0-9]+\.[0-9]+(\.[0-9]+)?#\1${version}#g" "$f" && echo "  $f: golang:$version"
done < <(git ls-files '*Dockerfile*' | xargs -r grep -l 'golang:[0-9]' || true)
if [ -f .tool-versions ] && grep -q '^golang ' .tool-versions; then
  sed -i -E "s#^golang .*#golang ${version}#" .tool-versions && echo "  .tool-versions: golang $version"
fi

# ── Workflows must NOT pin versions: everything comes from .go-version ──
hardcoded=$(grep -rnE "go-version:[[:space:]]*['\"]?[0-9]" .github/workflows .github/actions 2>/dev/null || true)
if [ -n "$hardcoded" ]; then
  echo "$hardcoded" >&2
  die "Some workflows have a hand-written Go version; they must use .go-version (go-setup)"
fi
echo "OK"
