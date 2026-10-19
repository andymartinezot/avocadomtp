# Memory policy

## Where memory lives

The real files are **`memory/` in the project root**, so they stay with the project and the user can
read and edit them. They are **gitignored** (the repo is public and memory holds personal
identifiers), so they never reach GitHub.

Claude Code reads them through a symlink:

```
~/.claude/projects/-Users-<user>-Documents-github-andymartinezot-avocadomtp/memory  →  <project>/memory
```

- **Don't "fix" that symlink.** It is deliberate: the harness path is keyed to the project's absolute
  path, so if the files lived there, moving or renaming the project would orphan every memory.
- **If the project moves**, re-point that one symlink (`ln -sfn <new>/memory <harness dir>/memory`).
  The memories themselves are safe.
- **On a new machine** (fresh clone): `memory/` doesn't exist (it's gitignored). Create it and copy
  the files over from the old machine, or start fresh; then create the symlink (`ls ~/.claude/projects/`
  shows the encoded folder name for this project).
- `MEMORY.md` is the index and is **injected into every session automatically** — never read it again
  to "check".

Memory exists for one thing: knowledge that would otherwise be re-learned from scratch next session.

## The boundary — one source of truth per fact

| Kind of fact | Home | Not memory because |
|---|---|---|
| "Right now": next steps, component state, open threads | `STATUS.md` | it changes weekly |
| A decision, its why and its alternatives | `DECISIONS.md` → `docs/decisions/` | it has a date and a status |
| How the code works, commands, invariants, gotchas | `CLAUDE.md`, doc comments | the repo already records it |
| How to reach an external tool | `.claude/rules/integrations.md` | it is config |
| What happened in one session | `docs/sessions/<date>-<role>-<slug>.md` | it is a narrative |
| Progress on a change | the spec's `task.md` | it is live state |
| **Durable, cross-session, not in the repo** | **memory** | — |

What genuinely belongs in memory:
- How the user wants work done, especially after a correction or a confirmed preference.
- A trap that already cost time and will recur, when it isn't a code fact for `CLAUDE.md`.
- Facts about the user's environment that aren't in the repo: the test device, accounts, identities.
- Stable identifiers that must stay out of the public repo (team ID, serials, emails).

## Who writes

**Main only.** Agents end their output contract with `## Memory candidates`; main verifies the claim
first — re-running the decisive check — and then writes. This keeps near-duplicate memories out.

## Never in memory

- Secret values, credentials, tokens, private keys.
- Data from the user's phone (file names of personal files, photos, contents).
- Code structure, git history, or anything `CLAUDE.md` / the repo already records.
- Anything true only for the current conversation.

## Format

- One file, one fact. Filename stem **must** equal the frontmatter `name:`, which is also the
  `[[link]]` target. Breaking that silently breaks links.
- Frontmatter: `name`, `description` (one line — recall matches on it), `metadata.type` of
  `user` | `feedback` | `project` | `reference`.
- `feedback` and `project` bodies end with `**Why:**` and `**How to apply:**`.
- Every memory gets one index line in `MEMORY.md`: `- [Title](<name>.md) — hook`.
- Link related memories with `[[name]]`. A link to a memory that doesn't exist yet is fine.
- Convert relative dates to absolute.
- `/lint` checks all of the above.

## Maintenance

A memory records what was true when it was written: treat it as a lead, not as truth.

- Before acting on a memory that names a file, flag, path or command, **confirm it still exists.**
- When a memory is wrong, **correct or delete that file**; never add a second one beside it.
- When you correct a memory, note the date and what was wrong.
- Fix the places the wrong fact already spread to (docs, decision records, `STATUS.md`) in the same pass.
