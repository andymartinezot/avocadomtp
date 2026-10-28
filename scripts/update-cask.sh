#!/usr/bin/env bash
# Points Casks/avocadomtp.rb at a published release: sets its version and the
# sha256 that the release workflow published next to AvocadoMTP.zip. Run it
# after the release is out, then commit the cask.
#
#   scripts/update-cask.sh 1.2.3
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

fail() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

version="${1:-}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "usage: scripts/update-cask.sh X.Y.Z"
url="https://github.com/andymartinezot/avocadomtp/releases/download/v$version/AvocadoMTP.zip.sha256"
sha="$(curl -fsSL --proto '=https' --proto-redir '=https' "$url" | awk 'NR == 1 { print $1 }')" ||
  fail "couldn't download $url (is v$version published?)"
[[ "$sha" =~ ^[0-9a-f]{64}$ ]] || fail "unexpected checksum file at $url"

cask=Casks/avocadomtp.rb
sed -i '' -E "s/^  version \"[^\"]*\"/  version \"$version\"/; s/^  sha256 \"[0-9a-f]*\"/  sha256 \"$sha\"/" "$cask"
grep -q "version \"$version\"" "$cask" && grep -q "sha256 \"$sha\"" "$cask" || fail "couldn't update $cask"
echo "$cask: $version, sha256 $sha"
