# 0001. Userspace MTP over libusb

- **Status:** Accepted
- **Date:** 2026-10-16

## Context
macOS has no MTP stack, so Android phones (which expose storage only over MTP since Android 4) are invisible to Finder. OpenMTP and MacDroid solve this by speaking MTP themselves.

## Decision
Implement an MTP initiator in user space on top of libusb (`internal/usbx`, `internal/ptp`, `internal/mtp`). Claim the phone's MTP interface directly, stopping Image Capture's `ptpcamerad` when it holds it.

## Alternatives considered
| Option | Why not |
|---|---|
| Wrap libmtp (C) | faster start, but less control over transfers and an LGPL dependency |
| ADB instead of MTP | needs USB debugging enabled on the phone; MTP works on any phone in File transfer mode |
| A kernel extension | requires lowering system security on Apple Silicon |

## Consequences
No kernel code and no special entitlements. We own the whole protocol, which made chunked transfers, partial reads and stuck-device recovery possible. Android extensions (GetPartialObject64, edit operations) are used when the device supports them.
