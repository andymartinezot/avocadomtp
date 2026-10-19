# 0004. Finder volumes through FUSE-T, not a native FSKit extension

- **Status:** Accepted
- **Date:** 2026-10-16

## Context
The planned native FSKit module (Swift app extension) needs the FSKit Module capability. Xcode refused it: personal (free) Apple teams don't support it. The user has a free account.

## Decision
Mount volumes with FUSE-T (kext-less, `brew install --cask fuse-t`) through cgofuse, from inside the daemon. Prefer FUSE-T's FSKit backend (real local volumes); fall back to its NFS backend automatically if FSKit fails or is disabled.

## Alternatives considered
| Option | Why not |
|---|---|
| Paid Apple Developer Program ($99/yr) | the cleanest result, declined for now; revisit with distribution (0016) |
| Own NFS server + built-in client | no install, but reimplements what FUSE-T already does |
| macFUSE 5 FSKit backend | opt-in, needs libfuse3, early reports of read/write problems |

## Consequences
Users must install FUSE-T and enable its extension in System Settings (a script can't: `pluginkit -e use` locks the toggle). FUSE-T's call patterns (O_RDWR opens, repeated fsync) shaped 0008.
