# 0011. Service control via launchd

- **Status:** Accepted
- **Date:** 2026-10-19

## Context
The user must be able to turn the service on and off and choose whether it starts at login (default on).

## Decision
LaunchAgent `com.andymartinezot.avocadomtp`: `stop` = bootout (stays stopped), `start` = bootstrap, autostart = `launchctl enable/disable` (persistent). `KeepAlive.SuccessfulExit=false` restarts after crashes only; the daemon exits 0 on SIGTERM. `daemon install` copies the binary to `~/Library/Application Support/AvocadoMTP/bin/`.

## Alternatives considered
| Option | Why not |
|---|---|
| RunAtLoad flag for autostart | `SuccessfulExit` implies RunAtLoad, so it can't express "off" |
| Run the binary from the repo | launchd agents hang in dyld on binaries under ~/Documents (TCC) |

## Consequences
The menu bar app drives the same CLI commands, so launchd logic lives in one place (Go).
