# Git conventions

- **One repo**, default branch `main`, remote `origin` = `git@github.com-ot:andymartinezot/avocadomtp.git`
  (public). Push through the `github.com-ot` SSH alias: the default GitHub credentials on this Mac
  belong to a different account and get a 403.
- **Harness files are committed** (`CLAUDE.md`, `.claude/`, `STATUS.md`, `DECISIONS.md`,
  `docs/decisions/`, `specs/`) — **except** `memory/*`, `docs/sessions/*` and
  `.claude/settings.local.json`, which are gitignored on purpose (personal identifiers). A clean
  `git status` doesn't mean nothing was written there.
- **Commit only when the user asks** (or approved a plan that includes committing). Then commit
  directly on `main`; use a branch only for experiments the user wants kept apart.
- **Subject:** imperative, ≤ 72 chars, no prefix (`Add …`, `Fix …`, `Rename …`). **Body:** what changed
  and why, wrapped at ~72, with measured results when there are any. Group a change with its tests
  and docs in one commit; split unrelated changes.
- **No AI attribution** of any kind (no `Co-Authored-By: Claude`, no "Generated with" lines, no
  session links). The global git identity is not set: pass the author per commit
  (`git -c user.name=… -c user.email=… commit`), with the identity from memory (`github-identity`).
- **Before committing:** `gofmt -l .` prints nothing, `go vet ./...` and `go test -race ./...` pass,
  and the macOS app builds if `macos/` changed.
- **Never commit:** `bin/`, `macos/build/`, the generated `macos/*.xcodeproj`, `memory/` contents,
  session logs, binaries, test data.
- **Push** only after the user's OK (`.claude/settings.json` asks). **Never** force-push, rewrite
  published history, or delete branches without explicit confirmation.
