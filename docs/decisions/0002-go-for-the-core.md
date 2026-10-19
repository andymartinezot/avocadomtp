# 0002. Go for the core, Swift only for the macOS UI

- **Status:** Accepted
- **Date:** 2026-10-16

## Context
The engine needs cgo access to libusb, good concurrency for a daemon, simple distribution, and testability without a phone.

## Decision
Write the MTP engine, daemon, Finder file system and CLI in Go. Use Swift/SwiftUI only where Apple frameworks require it (the menu bar app).

## Alternatives considered
| Option | Why not |
|---|---|
| Swift for everything | would have suited a native FSKit extension, which turned out to be unavailable (see 0004) |
| Rust | fine technically, slower to iterate here; no framework advantage on macOS |

## Consequences
A single static-ish Go binary (`avocadomtp`) holds CLI, daemon and mounts. An in-memory fake phone (`internal/mtp/mtptest`) lets `go test` exercise the full protocol.
