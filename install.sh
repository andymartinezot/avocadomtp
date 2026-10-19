#!/usr/bin/env bash
# Installs AvocadoMTP: dependencies, build, background service.
#
#   ./install.sh                     from a clone of the repository
#   curl -fsSL https://raw.githubusercontent.com/andymartinezot/avocadomtp/main/install.sh | bash
#
# Safe to run again: installed packages are skipped, the binary is rebuilt and the
# service is reinstalled with it (that's also how you update).
set -euo pipefail

REPO_URL="https://github.com/andymartinezot/avocadomtp.git"
CLONE_DIR="${AVOCADOMTP_DIR:-$HOME/avocadomtp}"

step() { printf '\n\033[1;32m==>\033[0m \033[1m%s\033[0m\n' "$*"; }
fail() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

# Everything runs inside main, so `curl | bash` reads the whole script before any
# command (brew, git) can consume the rest of stdin.
main() {
  [[ "$(uname -s)" == Darwin ]] || fail "AvocadoMTP runs on macOS only."

  if ! command -v brew >/dev/null 2>&1; then
    for b in /opt/homebrew/bin/brew /usr/local/bin/brew; do
      if [[ -x "$b" ]]; then eval "$("$b" shellenv)"; break; fi
    done
  fi
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
  if brew list --cask fuse-t >/dev/null 2>&1; then
    echo "fuse-t: already installed"
  else
    echo "macOS may ask for your password to install it."
    brew install --cask fuse-t
  fi

  step "3/4 Building avocadomtp"
  go build -o bin/avocadomtp ./cmd/avocadomtp
  echo "built $PWD/bin/avocadomtp"

  step "4/4 Installing the background service"
  ./bin/avocadomtp daemon install

  cat <<EOF

Done. Now:
  1. Unlock the phone, plug it in and pick "File transfer" in its USB notification.
  2. Optional, faster volumes: System Settings → General → Login Items & Extensions →
     File System Extensions → turn on fuse-t.
  3. Open Finder: the phone's storages appear under Locations.

Check the service any time with:  $PWD/bin/avocadomtp daemon status
EOF
}

main "$@"
