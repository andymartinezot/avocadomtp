# 0015. Claude Code harness adapted from devops-harness

- **Status:** Accepted
- **Date:** 2026-10-19

## Context
Context and memory must survive long sessions and compactions, and stay with the project.

## Decision
Adopt the devops-harness kit, adapted to a single-repo product: `CLAUDE.md` map with routing, `STATUS.md`, this decision log, specs for risky changes, session logs with `/handoff` and `/resume-handoff`, `memory/` in the project behind a symlink, a read-only `device-inspector-agent`, and a device-safety rule. `memory/` and `docs/sessions/` are gitignored (public repo).

## Alternatives considered
| Option | Why not |
|---|---|
| Link the kit's skills | symlinks to a local folder break for anyone who clones the repo |
| Commit memory | the repo is public and memory holds personal identifiers |

## Consequences
Main orchestrates and is the only writer of memory, commits and pushes. Start of session: `/resume-handoff`; end: `/handoff`.
