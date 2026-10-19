# 0016. Distribution: Homebrew tap first, signed app later

- **Status:** Proposed
- **Date:** 2026-10-16

## Context
Others should be able to install AvocadoMTP. Opening an app on another Mac without warnings requires Developer ID signing and notarization (paid account).

## Decision
Stage 1: a Homebrew tap that builds from a release tag (depends on libusb; FUSE-T as a cask). Stage 2: `AvocadoMTP.app` with the daemon bundled inside, distributed as a DMG and a cask, once a paid account exists (which would also allow native FSKit, see 0004).

## Alternatives considered
| Option | Why not |
|---|---|
| Ship an unsigned app | every user has to bypass Gatekeeper by hand |

## Consequences
Next step on the roadmap: bundle the daemon inside the app (needs a spec).
