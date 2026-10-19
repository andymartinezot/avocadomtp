# Secrets and personal data

The repo is **public**. Nothing below may appear in committed files, commit messages, GitHub issues,
or release notes.

## Secrets

- **Code signing:** the Apple Development certificate and its private key live in the login keychain.
  Refer to the identity by name (`security find-identity -v -p codesigning` lists names and hashes,
  never keys). Never export, print or copy the private key or a `.p12`.
- **Apple account / provisioning:** handled by Xcode (`-allowProvisioningUpdates`). Never print
  account tokens or provisioning-profile contents.
- **Git credentials:** SSH keys in `~/.ssh/` — refer to them by alias (`github.com-ot`), never print
  or open key files.
- This project has no API keys or tokens. If one is ever needed, it goes in the keychain or an
  ignored file, never in code; record only its name here.

## Personal data

| What | Rule |
|---|---|
| The user's phone contents (file names, photos, ROMs, backups) | read only for checks; never copy into the repo, logs or memory beyond what a check needs |
| Device serials, USB serials | memory only (gitignored), never committed |
| Apple team ID, email, account names | memory and `project.yml`/git config only where technically required; never in docs or commit messages |
| Absolute home paths | avoid in committed docs; use `~/…` |

## If something leaks

Tell the user immediately, name every file or commit that now holds it, and don't copy the value
anywhere else (including summaries). Rewriting public history needs the user's explicit decision.
