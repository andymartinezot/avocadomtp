# 0006. Mount points in ~/AvocadoMTP, not /Volumes

- **Status:** Accepted
- **Date:** 2026-10-16

## Context
`/Volumes` is root-owned; creating mount points there needs admin rights. FUSE-T's FSKit backend fails with "invalid file system" when the folder doesn't exist.

## Decision
Mount in `~/AvocadoMTP/<name>`. With the FSKit backend these are local volumes, so Finder still lists them under Locations with eject buttons. Remove empty mount points after unmount so nothing is written to the Mac by mistake.

## Alternatives considered
| Option | Why not |
|---|---|
| sudo-created folders in /Volumes | a manual step per storage name, empty folders left behind while unplugged |
| Symlinks in /Volumes | dead links while unplugged; Finder shows aliases, not disks |

## Consequences
Terminal paths are `~/AvocadoMTP/...`. Deleting recursively inside `~/AvocadoMTP` deletes phone data: denied in `.claude/settings.json`.
