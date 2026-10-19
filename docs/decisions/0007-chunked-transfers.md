# 0007. Chunked transfers so one copy never blocks the others

- **Status:** Accepted
- **Date:** 2026-10-16

## Context
A whole-file MTP transfer is one long transaction. During a 2 GiB pull, a folder listing on the other volume waited 67 s.

## Decision
Through the daemon, read in 8 MiB `GetPartialObject64` chunks and write with Android edit operations (BeginEdit / SendPartialObject / EndEdit) in 8 MiB chunks, so other requests run between chunks. Fall back to single transactions on devices without the Android extensions.

## Alternatives considered
| Option | Why not |
|---|---|
| Single-transaction transfers | simplest and marginally faster, but browsing freezes during copies |

## Consequences
Listings during a copy dropped to 0.3-0.5 s; throughput unchanged (USB 2 bound). An interrupted chunked upload deletes its partial file.
