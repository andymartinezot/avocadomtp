# 0005. One volume per storage, named after the device

- **Status:** Accepted
- **Date:** 2026-10-16

## Context
A phone exposes several storages (internal memory, SD card). The user wants the SD card to feel like a card plugged into the Mac.

## Decision
Mount each storage as its own volume. Name the internal storage after the device model (e.g. `Retroid Pocket 6`); other storages keep their label (`Retroid6`). The phone's own description still works as an alias in CLI paths.

## Alternatives considered
| Option | Why not |
|---|---|
| One volume per device with storages as folders | matches the CLI paths, but the SD card doesn't feel like a disk |

## Consequences
Storage naming lives in `vfs` (`DeviceName`), shared by the CLI, daemon and mounts so names always agree.
