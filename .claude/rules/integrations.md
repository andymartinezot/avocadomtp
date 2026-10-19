# Integrations

How this project reaches external systems. Rows are added on first use, never pre-wired: ask which
tool, check auth with a read-only command, record identifiers (never tokens), and add gotchas learned
by using it. No `|` characters inside cells.

| Channel | Tool | Status | Auth check | Identifiers | Gotchas |
|---|---|---|---|---|---|
| Code hosting | `git` over SSH | enabled (2026-10-19) | `ssh -T git@github.com-ot` → "Hi andymartinezot!" | `andymartinezot/avocadomtp` (public; renamed from avocadomcp) | Use the `github.com-ot` alias: default credentials are another account (403). Ask before every push. |
| GitHub API (issues, releases) | `gh` | not enabled | `gh auth status` | — | `gh` is logged in as a different account; switch or add the right one before any write. |
| Apple developer tools | Xcode, `xcodebuild`, Icon Composer | enabled | `security find-identity -v -p codesigning` shows 1 valid identity | free personal team (see memory) | Free teams can't use the FSKit Module capability. A missing WWDR G3 intermediate makes the certificate "invalid". |
| Finder mounts | FUSE-T (`brew install --cask fuse-t`) | enabled | `ls /usr/local/lib/libfuse-t.dylib` | — | FSKit backend needs the user's toggle in System Settings → File System Extensions; `pluginkit -e use` locks it. |
