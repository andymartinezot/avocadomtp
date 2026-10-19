#!/usr/bin/env bash
# Installs AvocadoMTP.
#
#   curl -fsSL https://raw.githubusercontent.com/andymartinezot/avocadomtp/main/install.sh | bash
#   ./install.sh                     the same, from a clone
#   ./install.sh --from-source       build from source instead (needs Homebrew; no menu bar app)
#
# By default it downloads the latest release (the menu bar app with the service
# inside), checks its sha256, installs FUSE-T, puts the app in /Applications and
# starts the service. Run it again to update.
#
# AVOCADOMTP_RELEASE_URL overrides where the release comes from (a base URL or
# file:///dir holding AvocadoMTP.zip and AvocadoMTP.zip.sha256).
set -euo pipefail

REPO_URL="https://github.com/andymartinezot/avocadomtp.git"
CLONE_DIR="${AVOCADOMTP_DIR:-$HOME/avocadomtp}"
RELEASE_URL="${AVOCADOMTP_RELEASE_URL:-https://github.com/andymartinezot/avocadomtp/releases/latest/download}"
APP=/Applications/AvocadoMTP.app
FUSE_T_LIB=/usr/local/lib/libfuse-t.dylib

step() { printf '\n\033[1;32m==>\033[0m \033[1m%s\033[0m\n' "$*"; }
fail() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

# Everything runs inside main, so `curl | bash` reads the whole script before any
# command (brew, git) can consume the rest of stdin.
main() {
  [[ "$(uname -s)" == Darwin ]] || fail "AvocadoMTP runs on macOS only."
  find_brew

  case "${1:-}" in
    "") install_release ;;
    --from-source) install_from_source ;;
    *) fail "unknown option ${1} (use --from-source, or no option)" ;;
  esac

  cat <<EOF

Done. Now:
  1. Unlock the phone, plug it in and pick "File transfer" in its USB notification.
  2. Optional, faster volumes: System Settings → General → Login Items & Extensions →
     File System Extensions → turn on fuse-t.
  3. Open Finder: the phone's storages appear under Locations.
EOF
}

# find_brew puts Homebrew on PATH when it's installed but not in this shell's PATH.
find_brew() {
  command -v brew >/dev/null 2>&1 && return
  local b
  for b in /opt/homebrew/bin/brew /usr/local/bin/brew; do
    if [[ -x "$b" ]]; then eval "$("$b" shellenv)"; return; fi
  done
}

install_fuse_t() {
  if [[ -f "$FUSE_T_LIB" ]]; then
    echo "fuse-t: already installed"
  elif command -v brew >/dev/null 2>&1; then
    echo "macOS may ask for your password to install it."
    brew install --cask fuse-t
  else
    echo "FUSE-T is needed for Finder volumes: install it from https://www.fuse-t.org"
    echo "(the menu bar app reminds you; copying with the avocadomtp command works without it)."
  fi
}

install_release() {
  tmp="$(mktemp -d)" # global: the EXIT trap runs after this function returns
  trap 'rm -rf "$tmp"' EXIT

  step "1/4 Downloading AvocadoMTP"
  # HTTPS only (redirects included); file:// only when the override asks for it.
  local proto="=https"
  [[ "$RELEASE_URL" == file://* ]] && proto="=file"
  local get=(curl -fsSL --proto "$proto" --proto-redir "$proto" --tlsv1.2)
  if ! "${get[@]}" -o "$tmp/AvocadoMTP.zip" "$RELEASE_URL/AvocadoMTP.zip" 2>/dev/null ||
    ! "${get[@]}" -o "$tmp/AvocadoMTP.zip.sha256" "$RELEASE_URL/AvocadoMTP.zip.sha256" 2>/dev/null; then
    # No published release yet: build from source as before.
    if [[ -z "${AVOCADOMTP_RELEASE_URL:-}" ]] && command -v brew >/dev/null 2>&1; then
      echo "No release to download yet; building from source instead (no menu bar app)."
      install_from_source
      return
    fi
    fail "couldn't download $RELEASE_URL/AvocadoMTP.zip; nothing was changed (./install.sh --from-source builds it instead)"
  fi
  # Compare the zip's own hash with the published one (shasum -c would check
  # whatever file name the .sha256 lists).
  local want got
  want="$(awk 'NR == 1 { print $1 }' "$tmp/AvocadoMTP.zip.sha256")"
  got="$(shasum -a 256 "$tmp/AvocadoMTP.zip" | awk '{ print $1 }')"
  [[ "$want" =~ ^[0-9a-f]{64}$ && "$got" == "$want" ]] ||
    fail "the download doesn't match its checksum; nothing was changed"
  ditto -x -k "$tmp/AvocadoMTP.zip" "$tmp/app"
  local new="$tmp/app/AvocadoMTP.app"
  [[ -x "$new/Contents/Helpers/avocadomtp" ]] || fail "the download has no AvocadoMTP.app; nothing was changed"
  codesign --verify --deep --strict "$new" 2>/dev/null || fail "the downloaded app's signature is invalid; nothing was changed"
  local version
  version="$("$new/Contents/Helpers/avocadomtp" version)"
  echo "AvocadoMTP $version, checksum OK"

  step "2/4 FUSE-T (Finder volumes)"
  install_fuse_t

  step "3/4 Installing $APP"
  local sudo=""
  if [[ ! -w /Applications ]] || { [[ -e "$APP" ]] && [[ ! -w "$APP" ]]; }; then
    echo "macOS will ask for your password to write to /Applications."
    sudo="sudo"
  fi
  osascript -e 'quit app "AvocadoMTP"' >/dev/null 2>&1 || true
  local i
  for i in 1 2 3 4 5 6 7 8 9 10; do
    pgrep -x AvocadoMTP >/dev/null || break
    sleep 0.5
  done
  # Keep the old app until the new one is in place.
  $sudo rm -rf "$APP.old"
  [[ -e "$APP" ]] && $sudo mv "$APP" "$APP.old"
  if ! $sudo ditto "$new" "$APP"; then
    $sudo rm -rf "$APP"
    [[ -e "$APP.old" ]] && $sudo mv "$APP.old" "$APP"
    fail "couldn't copy the app to /Applications; the previous version was kept"
  fi
  $sudo rm -rf "$APP.old"

  step "4/4 Starting the background service"
  "$APP/Contents/Helpers/avocadomtp" daemon install
  open "$APP"
  echo "The 🥑 menu is in the menu bar. For the avocadomtp command in Terminal, use"
  echo "\"Install command-line tool…\" in that menu."
}

install_from_source() {
  command -v brew >/dev/null 2>&1 || fail "Homebrew is required. Install it from https://brew.sh and run this again."

  # Use the clone this script lives in; when piped from curl, clone (or update) one.
  local src="${BASH_SOURCE[0]:-}"
  if [[ -n "$src" && -f "$(dirname "$src")/go.mod" ]]; then
    cd "$(dirname "$src")"
  elif [[ -d "$CLONE_DIR/.git" ]]; then
    step "Updating $CLONE_DIR"
    git -C "$CLONE_DIR" pull --ff-only
    cd "$CLONE_DIR"
  else
    step "Cloning into $CLONE_DIR"
    git clone "$REPO_URL" "$CLONE_DIR"
    cd "$CLONE_DIR"
  fi

  step "1/4 Build dependencies (go, libusb)"
  local f
  for f in go libusb; do
    if brew list --formula "$f" >/dev/null 2>&1; then echo "$f: already installed"; else brew install "$f"; fi
  done

  step "2/4 FUSE-T (Finder volumes)"
  install_fuse_t

  step "3/4 Building avocadomtp"
  go build -o bin/avocadomtp ./cmd/avocadomtp
  echo "built $PWD/bin/avocadomtp"

  step "4/4 Installing the background service"
  ./bin/avocadomtp daemon install
  echo "Check the service any time with:  $PWD/bin/avocadomtp daemon status"
}

main "$@"
