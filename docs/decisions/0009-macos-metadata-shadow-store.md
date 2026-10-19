# 0009. macOS metadata never reaches the phone

- **Status:** Accepted
- **Date:** 2026-10-16

## Context
Finder and tools write `._*` AppleDouble files, `.DS_Store`, Spotlight and Trash folders onto any volume.

## Decision
Keep those names in an in-memory shadow store (hidden from listings). Refuse `.Trashes`, `.Spotlight-V100`, `.fseventsd` so Finder deletes immediately instead of copying into a trash, and report `.metadata_never_index` so Spotlight leaves the phone alone. Normalize incoming names to NFC (macOS sends NFD).

## Alternatives considered
| Option | Why not |
|---|---|
| Write metadata to the phone | pollutes the phone's storage |
| List shadow files | made `rm -r` fail: macOS removes `._` companions itself |

## Consequences
"Move to Trash" on the phone means delete immediately. Renames between the phone and the shadow store are supported (ditto's temp-file pattern).
