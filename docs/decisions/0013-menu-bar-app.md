# 0013. SwiftUI menu bar app on one watch stream

- **Status:** Accepted
- **Date:** 2026-10-19

## Context
The user wants to see mounts, paths, transfers and speed, eject volumes, and switch the service on/off from the menu bar.

## Decision
`macos/` (XcodeGen): a menu-bar-only SwiftUI app. It keeps one `watch` connection (activity snapshots pushed on change), calls the CLI for service control, and socket ops for eject/mount. "Start at login" toggles both the service autostart and the app's login item (on by default, registered on first launch). Not sandboxed.

## Alternatives considered
| Option | Why not |
|---|---|
| Poll the daemon | costs CPU while idle |
| Reimplement launchctl logic in Swift | two implementations of the same rules |

## Consequences
Idle: 0% CPU, ~14 MB. A free Apple account is enough to run it on the user's Mac; distributing it needs Developer ID (0016).
