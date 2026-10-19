# 0008. Writes are staged on the Mac and uploaded once, on close

- **Status:** Accepted
- **Date:** 2026-10-16

## Context
MTP can't modify a file in place. FUSE-T's NFS layer syncs repeatedly while writing (uploading on every sync never finished a 500 MB copy), and its FSKit backend opens every file O_RDWR even for reads.

## Decision
Stage writes in an unlinked temp file; upload only on Release. Stage lazily: download for staging only on the first Write/Truncate. Overwrites upload under `.avocadomtp-<name>` and then swap, so a failed upload never destroys the original.

## Alternatives considered
| Option | Why not |
|---|---|
| Upload on flush/fsync | quadratic re-uploads |
| Stage on open | every read (thumbnails, Quick Look) downloaded whole files to the SSD |

## Consequences
A written file appears on the phone when it's closed. Lazy staging cut CPU 32% on large reads and removed all SSD writes for reads.
