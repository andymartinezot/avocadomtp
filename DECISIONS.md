# DECISIONS — index

This file is an index only. Each decision's context, alternatives and consequences live in its record
under `docs/decisions/`.

- **New decision:** add a record `docs/decisions/NNNN-<slug>.md` (Status, Date, Context, Decision,
  Alternatives considered, Consequences) and a row here, in the same change.
- **Changing a decision:** set the old record's Status to `Superseded by D-NNN` and write a new one;
  never rewrite history in place.
- Spec-scoped decisions also go in the spec's `task.md` "Decisions made while executing".

| ID | Decision | Status | Date | Record |
|---|---|---|---|---|
| D-001 | Userspace MTP over libusb | Accepted | 2026-10-16 | [`docs/decisions/0001-userspace-mtp-over-libusb.md`](docs/decisions/0001-userspace-mtp-over-libusb.md) |
| D-002 | Go for the core, Swift only for the macOS UI | Accepted | 2026-10-16 | [`docs/decisions/0002-go-for-the-core.md`](docs/decisions/0002-go-for-the-core.md) |
| D-003 | A daemon owns the USB session; everything else is a client | Accepted | 2026-10-16 | [`docs/decisions/0003-daemon-owns-the-usb-session.md`](docs/decisions/0003-daemon-owns-the-usb-session.md) |
| D-004 | Finder volumes through FUSE-T, not a native FSKit extension | Accepted | 2026-10-16 | [`docs/decisions/0004-fuse-t-instead-of-native-fskit.md`](docs/decisions/0004-fuse-t-instead-of-native-fskit.md) |
| D-005 | One volume per storage, named after the device | Accepted | 2026-10-16 | [`docs/decisions/0005-one-volume-per-storage.md`](docs/decisions/0005-one-volume-per-storage.md) |
| D-006 | Mount points in ~/AvocadoMTP, not /Volumes | Accepted | 2026-10-16 | [`docs/decisions/0006-mounts-in-home-not-volumes.md`](docs/decisions/0006-mounts-in-home-not-volumes.md) |
| D-007 | Chunked transfers so one copy never blocks the others | Accepted | 2026-10-16 | [`docs/decisions/0007-chunked-transfers.md`](docs/decisions/0007-chunked-transfers.md) |
| D-008 | Writes are staged on the Mac and uploaded once, on close | Accepted | 2026-10-16 | [`docs/decisions/0008-write-staging.md`](docs/decisions/0008-write-staging.md) |
| D-009 | macOS metadata never reaches the phone | Accepted | 2026-10-16 | [`docs/decisions/0009-macos-metadata-shadow-store.md`](docs/decisions/0009-macos-metadata-shadow-store.md) |
| D-010 | Resource policy: no idle polling, adaptive read-ahead, pooled buffers | Accepted | 2026-10-16 | [`docs/decisions/0010-resource-policy.md`](docs/decisions/0010-resource-policy.md) |
| D-011 | Service control via launchd | Accepted | 2026-10-19 | [`docs/decisions/0011-service-control-via-launchd.md`](docs/decisions/0011-service-control-via-launchd.md) |
| D-012 | Everything is named avocadomtp | Accepted | 2026-10-19 | [`docs/decisions/0012-name-avocadomtp.md`](docs/decisions/0012-name-avocadomtp.md) |
| D-013 | SwiftUI menu bar app on one watch stream | Accepted | 2026-10-19 | [`docs/decisions/0013-menu-bar-app.md`](docs/decisions/0013-menu-bar-app.md) |
| D-014 | Icons: menu bar A (USB-C pit), app icon B (plugged in) | Accepted | 2026-10-19 | [`docs/decisions/0014-icons.md`](docs/decisions/0014-icons.md) |
| D-015 | Claude Code harness adapted from devops-harness | Accepted | 2026-10-19 | [`docs/decisions/0015-agent-harness.md`](docs/decisions/0015-agent-harness.md) |
| D-016 | Distribution: Homebrew tap first, signed app later | Proposed | 2026-10-16 | [`docs/decisions/0016-distribution.md`](docs/decisions/0016-distribution.md) |
