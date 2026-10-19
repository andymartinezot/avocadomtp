#!/usr/bin/env bash
# Builds AvocadoMTP.app with the avocadomtp helper inside (libusb linked in, so
# it needs nothing but macOS), and a zip of it.
#
#   scripts/build-app.sh                               ad-hoc signed, no Apple team needed
#   DEVELOPMENT_TEAM=ABCDE12345 scripts/build-app.sh   signed with your team
#   VERSION=1.2.3 scripts/build-app.sh                 default: MARKETING_VERSION in macos/project.yml
#
# Needs Xcode, xcodegen, go and libusb (Homebrew). Output: macos/build/.
set -euo pipefail
cd "$(dirname "$0")/.."

fail() { printf 'error: %s\n' "$*" >&2; exit 1; }

VERSION="${VERSION:-$(sed -n 's/^ *MARKETING_VERSION: *"\(.*\)"/\1/p' macos/project.yml)}"
[[ -n "$VERSION" ]] || fail "no version: set VERSION or MARKETING_VERSION in macos/project.yml"
out=macos/build
helper="$out/helper/avocadomtp"
app="$out/Build/Products/Release/AvocadoMTP.app"

libusb="$(brew --prefix libusb 2>/dev/null)/lib/libusb-1.0.a"
[[ -f "$libusb" ]] || fail "libusb not found (brew install libusb)"
command -v xcodegen >/dev/null || fail "xcodegen not found (brew install xcodegen)"

echo "==> helper $VERSION (static libusb)"
mkdir -p "$(dirname "$helper")"
CGO_LDFLAGS="$libusb -framework IOKit -framework CoreFoundation -framework Security -Wl,-dead_strip_dylibs" \
  go build -trimpath -ldflags "-X main.version=$VERSION" -o "$helper" ./cmd/avocadomtp 2>&1 |
  grep -vE "ignoring duplicate libraries|^# " || true
[[ -x "$helper" ]] || fail "helper build failed"
if otool -L "$helper" | tail -n +2 | grep -vE '^\s+(/System/Library/|/usr/lib/)'; then
  fail "the helper links libraries outside macOS (listed above)"
fi

echo "==> AvocadoMTP.app"
if [[ -n "${DEVELOPMENT_TEAM:-}" ]]; then
  sign=(DEVELOPMENT_TEAM="$DEVELOPMENT_TEAM" CODE_SIGN_STYLE=Automatic CODE_SIGN_IDENTITY="Apple Development" -allowProvisioningUpdates)
else
  sign=(DEVELOPMENT_TEAM= CODE_SIGN_STYLE=Manual CODE_SIGN_IDENTITY=-)
fi
(cd macos && xcodegen generate --quiet)
# Start from a fresh bundle: an incremental build where only the helper
# changed would skip re-signing the app and leave its seal invalid.
rm -rf "$app"
xcodebuild -project macos/AvocadoMTP.xcodeproj -scheme AvocadoMTP -configuration Release \
  -derivedDataPath "$out" -destination "platform=macOS,arch=$(uname -m)" MARKETING_VERSION="$VERSION" "${sign[@]}" -quiet build
[[ -x "$app/Contents/Helpers/avocadomtp" ]] || fail "the helper is missing from the app"
codesign --verify --deep --strict "$app"

zip="$out/AvocadoMTP-$VERSION.zip"
rm -f "$zip"
ditto -c -k --keepParent "$app" "$zip"
echo "built $app"
echo "      $zip ($(du -h "$zip" | cut -f1))"
