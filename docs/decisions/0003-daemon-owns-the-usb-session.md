# 0003. A daemon owns the USB session; everything else is a client

- **Status:** Accepted
- **Date:** 2026-10-16

## Context
Only one process can claim the phone's MTP interface. The CLI, two Finder volumes and the menu bar all need the phone at the same time.

## Decision
`avocadomtp daemon` holds the single MTP session and serves clients over a Unix socket (`~/Library/Application Support/AvocadoMTP/avocadomtp.sock`) with a framed JSON protocol (`internal/daemon/proto.go`). The CLI uses it automatically when it runs (`--direct` bypasses it). The Finder mounts run inside the daemon process.

## Alternatives considered
| Option | Why not |
|---|---|
| Link the engine into an FSKit extension | sandboxed extensions can't stop ptpcamerad, and two volumes would need two USB owners |
| Each tool opens USB itself | they would fight over the interface |

## Consequences
One USB owner; clients interleave because transfers are chunked (0007). The protocol is the contract between Go and Swift: changing it needs a spec.
