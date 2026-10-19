#!/usr/bin/env bash
# Publishes a release: checks the tree, tags vX.Y.Z and pushes the tag. GitHub
# Actions (.github/workflows/release.yml) then builds the universal app and
# creates the GitHub Release with AvocadoMTP.zip.
#
#   scripts/release.sh 0.2.1             asks before pushing the tag
#   scripts/release.sh --dry-run 0.2.1   every check, no tag
#   scripts/release.sh --yes 0.2.1       no question
#
# First set MARKETING_VERSION in macos/project.yml to the new version and commit it.
# With gh installed, the script follows the build and prints the release.
set -euo pipefail
cd "$(dirname "$0")/.."

REPO=andymartinezot/avocadomtp

fail() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }
ok() { printf '  \033[32m✓\033[0m %s\n' "$*"; }

dry=0 yes=0 version=""
for arg in "$@"; do
  case "$arg" in
    --dry-run) dry=1 ;;
    --yes) yes=1 ;;
    -*) fail "unknown option $arg" ;;
    *) version="${arg#v}" ;;
  esac
done
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "usage: scripts/release.sh [--dry-run] [--yes] X.Y.Z"
tag="v$version"

echo "Checking release $tag"
branch="$(git rev-parse --abbrev-ref HEAD)"
[[ "$branch" == main ]] || fail "on branch $branch; releases are tagged on main"
ok "on main"
git diff --quiet && git diff --cached --quiet || fail "uncommitted changes to tracked files"
ok "no uncommitted changes"
git fetch --quiet origin main --tags
[[ "$(git rev-parse HEAD)" == "$(git rev-parse origin/main)" ]] || fail "main differs from origin/main: push or pull first"
ok "in sync with origin/main"
current="$(sed -n 's/^ *MARKETING_VERSION: *"\(.*\)"/\1/p' macos/project.yml)"
[[ "$current" == "$version" ]] || fail "macos/project.yml has MARKETING_VERSION $current; set it to $version and commit first"
ok "MARKETING_VERSION is $version"
if git rev-parse -q --verify "refs/tags/$tag" >/dev/null || [[ -n "$(git ls-remote --tags origin "refs/tags/$tag")" ]]; then
  fail "tag $tag already exists"
fi
ok "tag $tag is new"
[[ -z "$(gofmt -l .)" ]] || fail "gofmt: run gofmt -w on the files above"
go vet ./... || fail "go vet failed"
go test -race ./... >/dev/null || fail "tests failed (go test -race ./...)"
ok "gofmt, vet and tests pass"

if [[ $dry == 1 ]]; then
  echo "Dry run: everything checks out; $tag was not created."
  exit 0
fi
if [[ $yes == 0 ]]; then
  read -r -p "Publish $tag ($(git log -1 --format='%h %s'))? [y/N] " answer
  [[ "$answer" == [yY] ]] || fail "cancelled"
fi

git tag -a "$tag" -m "AvocadoMTP $version"
git push --quiet origin "$tag"
ok "pushed $tag; GitHub Actions is building the release"

if ! command -v gh >/dev/null; then
  echo "Follow the build at https://github.com/$REPO/actions"
  exit 0
fi
run=""
for _ in $(seq 1 30); do
  run="$(gh run list -R "$REPO" --workflow release.yml --branch "$tag" --limit 1 --json databaseId --jq '.[0].databaseId' 2>/dev/null || true)"
  [[ -n "$run" ]] && break
  sleep 2
done
[[ -n "$run" ]] || fail "no workflow run found for $tag; see https://github.com/$REPO/actions"
gh run watch "$run" -R "$REPO" --exit-status --interval 10 >/dev/null || fail "the release build failed: gh run view $run -R $REPO --log-failed"
gh release view "$tag" -R "$REPO" --json url,assets --jq '"released " + .url, (.assets[] | "  " + .name)'
