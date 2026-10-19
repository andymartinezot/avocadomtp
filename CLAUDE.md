# CLAUDE.md

AvocadoMTP: Android ↔ macOS file transfer over USB. A user-space MTP stack (Go + cgo/libusb),
a daemon that owns the USB session, Finder volumes through FUSE-T, and a CLI.
User-facing docs: `README.md`; interactive architecture/perf page: `docs/index.html`.

## Commands

```sh
go build -o bin/avocadomtp ./cmd/avocadomtp     # needs: brew install go libusb; FUSE-T for mounts
go test -race ./...                       # all tests run against an in-memory fake phone
gofmt -l . && go vet ./...                # must be clean before committing
./bin/avocadomtp daemon install              # (re)install the LaunchAgent with the fresh binary
tail -f ~/Library/Logs/avocadomtp.log        # daemon log
AVOCADOMTP_FUSE_DEBUG=1 ./bin/avocadomtp daemon # foreground daemon tracing every FUSE call
./bin/avocadomtp watch                   # live activity stream (what the menu bar shows)

# menu bar app (Swift 5 mode, macOS 14+, not sandboxed)
cd macos && xcodegen generate && xcodebuild -project AvocadoMTP.xcodeproj -scheme AvocadoMTP \
  -configuration Release -derivedDataPath build -allowProvisioningUpdates build
```

After changing daemon or fusefs code, the running LaunchAgent still uses the old binary:
run `./bin/avocadomtp daemon install` again (it copies the binary and restarts the agent).

## Architecture (data path)

```
Finder → FUSE-T (FSKit or NFS) → internal/fusefs ─┐
CLI ─ Unix socket (internal/daemon) ──────────────┼→ internal/vfs → internal/mtp → internal/usbx → phone
```

| Package | Responsibility |
|---|---|
| `internal/usbx` | cgo libusb: find/claim MTP interface (kills `ptpcamerad`), bulk I/O, hotplug (`hotplug.c`) |
| `internal/ptp` | pure byte codec: containers, datasets, PTP strings/dates |
| `internal/mtp` | `Session`: one transaction at a time, data phases, ZLPs, Android extensions, stats counters |
| `internal/mtp/mtptest` | in-memory fake device (`Fake`) and `Phone` (a `remote.Remote`) for tests |
| `internal/vfs` | path ↔ handle resolution, TTL dir cache, NFC/NFD + case-insensitive matching, device naming |
| `internal/remote` | `Remote` interface implemented by `device.Device` (USB) and `daemon.Client` (socket) |
| `internal/device` | opens the phone; stuck-device recovery (PTP reset → USB port reset) |
| `internal/daemon` | server (owns the phone, hotplug-driven), socket protocol (`proto.go`), client, LaunchAgent |
| `internal/fusefs` | cgofuse FS per storage, mount manager (FSKit → NFS fallback), metadata shadow store |
| `internal/errcode` | MTP/USB errors → POSIX errno (shared by daemon and fusefs) |
| `internal/bufpool` | pooled transfer buffers |
| `internal/activity` | hub of phone/mount/transfer state; `watch` snapshots for the menu bar (idle: no ticks) |
| `cmd/avocadomtp` | CLI, shell, `daemon` subcommands, `watch`/`eject`/`mount` |
| `macos/` | SwiftUI menu bar app: one `watch` stream for state, the CLI for service control, socket ops for eject/mount |

## Invariants: don't break these

- **Transfers through the daemon stay chunked** (8 MiB partial reads / edit-based writes in
  `daemon/server.go`), so one copy never blocks other clients or the other volume.
- **fusefs uploads only on `Release`**, never on Flush/Fsync: the NFS/FSKit layers sync repeatedly
  while writing, and every upload resends the whole file.
- **fusefs stages lazily**: FUSE-T's FSKit backend opens every file `O_RDWR`; download for staging
  only on the first Write/Truncate (see `stage`). Plain reads must never copy the file.
- **Overwrites upload to `.avocadomtp-<name>` then swap**: MTP can't overwrite, and a failed upload
  must not destroy the original.
- **macOS metadata never reaches the phone**: `isShadow` names (`._*`, `.DS_Store`, …) live in the
  in-memory shadow store and are hidden from listings; `.Trashes` and friends are refused (EACCES).
- **Incoming FUSE paths are NFC-normalized** (`nfc`), and vfs matches NFC first, then case-folded.
- **No polling while idle**: the daemon reacts to USB hotplug; the poll loop is only the fallback.
- **The session is marked broken after any non-MTP error**; callers reconnect (`Healthy`, daemon `drop`).
- **Mount points live in `~/AvocadoMTP`** (`/Volumes` is root-only); empty ones are removed on unmount.
- **Service control is launchd's**: `stop` = bootout (no restart), crash = restart
  (`KeepAlive.SuccessfulExit=false`; the daemon exits 0 on SIGTERM), autostart = `launchctl enable/disable`
  (not a plist flag: `SuccessfulExit` implies `RunAtLoad`).
- **The LaunchAgent runs `~/Library/Application Support/AvocadoMTP/bin/avocadomtp`**, never a binary
  under `~/Documents` (TCC blocks launchd agents there and they hang in dyld).

## Conventions

- Match the surrounding style: small packages, doc comments on exported identifiers, errors
  wrapped with context (`fmt.Errorf("…: %w", err)`), no new dependencies without a reason.
- New behavior gets a test against `mtptest` (fake phone); daemon tests use a real Unix socket
  (keep socket paths short: macOS limits them to 104 bytes, so `/tmp`, not `t.TempDir()`).
- Verify on a real phone for anything touching USB, mounting or Finder behavior; the fake can't
  reproduce kernel/FUSE-T call patterns (use `AVOCADOMTP_FUSE_DEBUG=1` to see them).
- Keep resource use low: pool large buffers (`bufpool`), avoid per-request allocations on hot
  paths, and measure before/after with real transfers (`avocadomtp daemon status` shows counters).
- Commits: no AI/Claude attribution lines. Push via the `github.com-ot` SSH alias
  (`origin` = `git@github.com-ot:andymartinezot/avocadomcp.git`).

## Gotchas

- The phone must be unlocked and in **File transfer** mode; other MTP apps (Android File
  Transfer, OpenMTP, MacDroid) must be closed, or claiming the interface fails with ACCESS.
- FUSE-T's FSKit module must be enabled by the user in System Settings → General → Login Items &
  Extensions → File System Extensions. `pluginkit -e use` does **not** work and locks the toggle.
  `fusefs.FSKitEnabled` probes with `mount -F -t fuset`.
- A native FSKit extension (Swift) needs a **paid** Apple Developer account; personal teams lack
  the FSKit Module capability. That's why FUSE-T is used.
- Some phones stop answering MTP after idle periods; recovery is in `device.open`.
