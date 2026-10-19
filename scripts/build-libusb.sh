#!/usr/bin/env bash
# Builds a universal (arm64 + x86_64) static libusb for release builds:
#
#   scripts/build-libusb.sh OUTDIR   → OUTDIR/lib/libusb-1.0.a, OUTDIR/include/libusb-1.0/
#
# Homebrew's libusb only has the host architecture, so releases build it from
# the pinned source tarball. Nothing is rebuilt if OUTDIR already has both.
set -euo pipefail

LIBUSB_VERSION=1.0.30
LIBUSB_SHA256=fea36f34f9156400209595e300840767ab1a385ede1dc7ee893015aea9c6dbaf
LIBUSB_URL="https://github.com/libusb/libusb/releases/download/v$LIBUSB_VERSION/libusb-$LIBUSB_VERSION.tar.bz2"
MIN_MACOS=14.0

fail() { printf 'error: %s\n' "$*" >&2; exit 1; }

[[ $# -eq 1 ]] || fail "usage: $0 OUTDIR"
out="$1"
lib="$out/lib/libusb-1.0.a"
if [[ -f "$lib" ]] && [[ "$(lipo -archs "$lib")" == *arm64* ]] && [[ "$(lipo -archs "$lib")" == *x86_64* ]]; then
  exit 0
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
echo "==> libusb $LIBUSB_VERSION (arm64 + x86_64, static)"
curl -fsSL -o "$work/libusb.tar.bz2" "$LIBUSB_URL"
echo "$LIBUSB_SHA256  $work/libusb.tar.bz2" | shasum -a 256 -c - >/dev/null || fail "libusb tarball checksum mismatch"

for arch in arm64 x86_64; do
  host=$([[ $arch == arm64 ]] && echo aarch64 || echo x86_64)-apple-darwin
  mkdir -p "$work/src-$arch"
  tar -xjf "$work/libusb.tar.bz2" -C "$work/src-$arch" --strip-components 1
  # ac_cv_func_pipe2=no: newer SDKs declare pipe2(), so configure uses it and
  # weak-links it; on older macOS it's missing and libusb_init jumps to NULL.
  (cd "$work/src-$arch" &&
    ac_cv_func_pipe2=no CFLAGS="-arch $arch -mmacosx-version-min=$MIN_MACOS -O2" \
      ./configure --quiet --host="$host" --disable-shared --enable-static --prefix="$work/$arch" &&
    make -s -j"$(sysctl -n hw.ncpu)" >/dev/null &&
    make -s install >/dev/null) || fail "libusb build failed for $arch"
done

mkdir -p "$out/lib" "$out/include"
lipo -create "$work/arm64/lib/libusb-1.0.a" "$work/x86_64/lib/libusb-1.0.a" -output "$lib"
rm -rf "$out/include/libusb-1.0"
cp -R "$work/arm64/include/libusb-1.0" "$out/include/"
echo "built $lib ($(lipo -archs "$lib"))"
