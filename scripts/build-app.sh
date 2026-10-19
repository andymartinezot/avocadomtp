#!/usr/bin/env bash
# Builds AvocadoMTP.app with the avocadomtp helper inside (libusb linked in, so
# it needs nothing but macOS), and a zip of it.
#
#   scripts/build-app.sh                               ad-hoc signed, no Apple team needed
#   DEVELOPMENT_TEAM=ABCDE12345 scripts/build-app.sh   signed with your team
#   VERSION=1.2.3 scripts/build-app.sh                 default: MARKETING_VERSION in macos/project.yml
#   UNIVERSAL=1 scripts/build-app.sh                   arm64 + x86_64 (what releases use; libusb
#                                                      is built from source by build-libusb.sh)
#
# Needs Xcode, xcodegen, go and libusb (Homebrew). Output: macos/build/, including
# AvocadoMTP.zip and AvocadoMTP.zip.sha256 (fixed names, as attached to releases).
set -euo pipefail
cd "$(dirname "$0")/.."

fail() { printf 'error: %s\n' "$*" >&2; exit 1; }

VERSION="${VERSION:-$(sed -n 's/^ *MARKETING_VERSION: *"\(.*\)"/\1/p' macos/project.yml)}"
[[ -n "$VERSION" ]] || fail "no version: set VERSION or MARKETING_VERSION in macos/project.yml"
out=macos/build
helper="$out/helper/avocadomtp"
app="$out/Build/Products/Release/AvocadoMTP.app"

min_macos=14.0
if [[ -n "${UNIVERSAL:-}" ]]; then
  archs=(arm64 x86_64)
  scripts/build-libusb.sh "$out/libusb"
  libusb="$out/libusb/lib/libusb-1.0.a"
  cflags="-I$out/libusb/include/libusb-1.0"
  destination=(-destination "generic/platform=macOS" ARCHS="arm64 x86_64" ONLY_ACTIVE_ARCH=NO)
else
  archs=("$(uname -m)")
  libusb="$(brew --prefix libusb 2>/dev/null)/lib/libusb-1.0.a"
  [[ -f "$libusb" ]] || fail "libusb not found (brew install libusb)"
  cflags=""
  destination=(-destination "platform=macOS,arch=$(uname -m)")
fi
command -v xcodegen >/dev/null || fail "xcodegen not found (brew install xcodegen)"

echo "==> helper $VERSION (${archs[*]}, static libusb)"
mkdir -p "$(dirname "$helper")"
slices=()
for arch in "${archs[@]}"; do
  goarch=$([[ $arch == x86_64 ]] && echo amd64 || echo arm64)
  CGO_ENABLED=1 GOARCH="$goarch" CC="clang -arch $arch -mmacosx-version-min=$min_macos" \
    CGO_CFLAGS="$cflags -mmacosx-version-min=$min_macos" \
    CGO_LDFLAGS="$libusb -framework IOKit -framework CoreFoundation -framework Security -Wl,-dead_strip_dylibs -mmacosx-version-min=$min_macos" \
    go build -trimpath -ldflags "-X main.version=$VERSION" -o "$helper-$arch" ./cmd/avocadomtp 2>&1 |
    grep -vE "ignoring duplicate libraries|^# |^ld: warning" || true
  [[ -x "$helper-$arch" ]] || fail "helper build failed ($arch)"
  if otool -L "$helper-$arch" | tail -n +2 | grep -vE '^\s+(/System/Library/|/usr/lib/)'; then
    fail "the $arch helper links libraries outside macOS (listed above)"
  fi
  # A weak pipe2 import crashes libusb_init on macOS versions without it.
  if nm -u "$helper-$arch" | grep -q '_pipe2$'; then
    fail "the $arch helper imports pipe2 (libusb built against a too-new SDK)"
  fi
  slices+=("$helper-$arch")
done
lipo -create "${slices[@]}" -output "$helper"
rm -f "${slices[@]}"

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
  -derivedDataPath "$out" "${destination[@]}" MARKETING_VERSION="$VERSION" "${sign[@]}" -quiet build
[[ -x "$app/Contents/Helpers/avocadomtp" ]] || fail "the helper is missing from the app"
codesign --verify --deep --strict "$app"

zip="$out/AvocadoMTP.zip"
rm -f "$zip" "$zip.sha256" "$out"/AvocadoMTP-*.zip
ditto -c -k --keepParent "$app" "$zip"
(cd "$out" && shasum -a 256 AvocadoMTP.zip > AvocadoMTP.zip.sha256)
echo "built $app $VERSION ($(lipo -archs "$app/Contents/MacOS/AvocadoMTP"))"
echo "      $zip ($(du -h "$zip" | cut -f1)) + .sha256"
