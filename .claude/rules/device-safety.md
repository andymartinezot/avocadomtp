# Device and system safety

This project's "production" is the user's own phone (real photos, ROMs, backups) and the user's Mac
(the installed service, app and login items). This rule replaces the kit's change-safety ladder.

## The ladder — prove each rung before the next

1. **Fake phone** — `go test -race ./...` against `internal/mtp/mtptest`. Every behavior change starts here.
2. **Real phone, test folder** — on-device checks only inside a dedicated folder:
   `/<storage>/avocadomtp-test` (e.g. `/Retroid6/avocadomtp-test`). Create it for the check and
   remove it afterwards.
3. **The user's installed setup** — `avocadomtp daemon install`, `/Applications/AvocadoMTP.app`,
   login items. Only with the user's OK, and only after rung 2 passed.

## On the phone

- **Never create, modify, move or delete anything outside the test folder**, through the CLI, the
  Finder volumes, or code. Reads (listing, `stat`, reading files) are fine anywhere.
- **Use random test data** (`head -c N /dev/urandom`), never copies of the user's files, and verify
  transfers by hash (`shasum -a 256`), not by size.
- **Clean up** the test folder and local scratch copies at the end of every check.
- **Never `rm -rf` inside `~/AvocadoMTP/`**: those directories are mounted phone storage, and a
  recursive delete there deletes the user's data. Delete test data with `avocadomtp rm -r
  /<storage>/avocadomtp-test`. (`.claude/settings.json` denies the obvious forms; this rule is the wall.)
- **If the phone stops answering** (`LIBUSB_ERROR_TIMEOUT`), don't loop: ask the user to unlock it or
  replug it / toggle its USB mode. Don't port-reset repeatedly.

## On the Mac

**Ask before:**
- installing, restarting, stopping or uninstalling the service (`avocadomtp daemon install|start|stop|uninstall`,
  `launchctl` on `com.andymartinezot.avocadomtp`);
- copying a new build to `/Applications`, or changing login items;
- changing global config: `~/.gitconfig`, `~/.claude/settings.json`, keychain, System Settings
  (extensions, privacy), Homebrew packages;
- anything that needs the user's password (`sudo`, installers).

An approval covers the action described, not the next one. If the user explicitly asked for the
step in the current request, that is the approval.

**Always fine:** building, running tests, `avocadomtp daemon status`, `avocadomtp watch`, reading
`~/Library/Logs/avocadomtp.log`, `mount`, `system_profiler SPUSBHostDataType`, a foreground
`avocadomtp daemon` while the LaunchAgent is stopped.

**Before replacing the installed service or app**, know the way back: the previous commit builds
the previous binary; `avocadomtp daemon install` from it restores the service. When something breaks
after an install: **roll back first, debug second.**

**Never kill** processes you didn't start, other than the ones this project owns
(`avocadomtp`, `AvocadoMTP`, the `ptpcamerad` handling in `usbx`).
