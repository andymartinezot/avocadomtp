# STATUS — AvocadoMTP

The "right now" page. Each fact carries how it was verified; re-verify before acting on it.

Last updated: 2026-10-19. Sources: on-device checks with the Retroid Pocket 6, `go test -race ./...`,
git state (`main` at `4505887` before the harness commit).

## Critical path

1. **Bundle the daemon inside `AvocadoMTP.app`** so one install sets everything up — needs a `/spec`
   (service install and login items change). Step toward distribution (D-016, proposed).
2. **Homebrew tap** for the CLI + daemon (stage 1 of D-016).

## Components

| Component | State | Last verified |
|---|---|---|
| MTP engine + CLI (`avocadomtp`) | working | 2026-10-16: 2 GiB round trips, hashes match, ~40 MiB/s (USB 2) |
| Daemon + socket protocol | working, installed as LaunchAgent | 2026-10-19: status, watch, eject/mount via socket |
| Finder volumes (FUSE-T FSKit backend) | working: `~/AvocadoMTP/Retroid Pocket 6`, `~/AvocadoMTP/Retroid6` | 2026-10-19: 300 MB copies both ways, hashes match |
| Service control (start/stop/autostart) | working | 2026-10-19: stop/start/autostart off-on, crash restart |
| Menu bar app (`/Applications/AvocadoMTP.app`) | installed, login item | 2026-10-19: built, running, watch connection, 0% CPU idle; UI checked by the user |
| App + menu bar icons | in the app | 2026-10-19: system-rendered icon checked |
| Distribution | not started | — |

## Blockers & open decisions

- D-016 (distribution) is **proposed**: a signed, notarized app for other Macs needs the paid Apple
  Developer Program. Not needed for the Homebrew tap.

## Follow-ups (not blocking)

- The Retroid's MTP service sometimes stops answering after idle periods (replug fixes it). Hotplug
  removed idle polling, which may have been the cause; watch for recurrences.
- macOS creates files empty, then fills them: each Finder copy also does an empty upload before the
  real one (hidden from the activity feed). Could be optimized by deferring empty files.
- Mount reads top out near 33 MiB/s vs ~40 for the CLI: kernel 1 MiB reads + per-transaction
  latency; prefetching the next read-ahead block could close the gap.
- `gh` is logged in as another account; GitHub API writes (issues, releases) need the right one.
