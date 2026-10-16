# AvocadoMTP

Transfer files between macOS and Android phones over USB, the way Linux does it.
No SD-card swapping and no kernel extensions.

Android phones expose their storage over **MTP (Media Transfer Protocol)**, an extension of PTP.
macOS doesn't ship an MTP stack, so AvocadoMTP implements one in user space on top of libusb
(the same approach OpenMTP and MacDroid take).

## Build

```sh
brew install go libusb
go build -o bin/avocado ./cmd/avocado
```

## Use

Unlock the phone, plug it in, and pick **File transfer** in the phone's USB notification.

```sh
avocado devices
avocado info
avocado ls -l "/Internal shared storage/DCIM/Camera"
avocado pull -r "/Internal shared storage/DCIM/Camera" ~/Pictures/phone
avocado push -r ~/Music "/SD card/Music"
avocado mkdir -p "/SD card/Backups/2026"
avocado mv "/SD card/a.txt" "/SD card/Backups/b.txt"
avocado rm -r "/SD card/Old"
avocado shell            # interactive: cd, ls, pull, push, lcd, lls ...
```

Close other MTP apps first (Android File Transfer, OpenMTP, MacDroid). Image Capture's
`ptpcamerad` is stopped automatically when it grabs the phone.

## Layout

| Package          | Role |
|------------------|------|
| `internal/usbx`  | cgo/libusb: find the MTP interface, claim it, bulk/interrupt I/O |
| `internal/ptp`   | byte-level containers and datasets (DeviceInfo, StorageInfo, ObjectInfo, PropList) |
| `internal/mtp`   | session and transactions (command → data → response), file operations |
| `internal/vfs`   | maps `/Storage/dir/file` paths to MTP handles, with a directory cache |
| `cmd/avocado`    | CLI and interactive shell |

`go test ./...` runs everything against an in-memory fake MTP device, so no phone is needed.

## Roadmap

- [x] MTP engine + CLI
- [ ] Finder integration via an FSKit volume (Swift extension linking the Go engine as a C archive)
