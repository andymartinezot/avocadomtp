# Orchestration — the main session

> **If you are running as a named agent** (any `*-agent` in `.claude/agents/`), **skip this file** and
> follow your agent definition. Rules load into subagents too; this one is for the main session only.

Adapted from the devops-harness kit's orchestrator rule for a single-repo product. Behavior only:
project facts (routing table, risky areas) live in the root `CLAUDE.md` under "Routing".

## Roles

| Role | Who | Does |
|---|---|---|
| **Main** (orchestrator) | this session | holds the map, routes work, writes and verifies specs, reviews results, owns every user-facing decision, is the only one that writes memory, commits, or pushes |
| **Device inspector** | `device-inspector-agent` | read-only evidence from the Mac and the phone (service state, logs, mounts, USB, listings); never changes anything |
| **Owners** | domain agents, once they exist (`.claude/templates/domain-agent.md`) | code work inside their scope; until then main does it |

Agents run with the Agent tool disallowed: only main delegates, and main routes each agent's
`## Needs from others`.

## Delegate by default — main's context is the scarcest resource

Everything main reads stays in its window until the session ends or compacts. Delegate when the work is:
- reading-heavy: more than two or three files, a log trawl, a dependency or call-graph search;
- a test run whose output is bulkier than its result;
- a live check on the Mac or the phone (always `device-inspector-agent`);
- producing output bulkier than its conclusion.

**Main does itself:** a quick lookup of a file or two, cross-package reasoning, specs and their
verification, reviewing results and re-running the decisive check, anything needing back-and-forth
with the user, and — while there are no domain agents — the code edits.

**Before acting, state the owner and the reason in one line.** Run independent delegations in
parallel in one message.

## Every delegation prompt

Subagents start cold: the prompt plus the files are all they have.

1. **Goal**, plus the spec path if there is one.
2. **Files or areas in scope**, and what **not** to touch. For anything touching the phone: the
   allowed test folder (`.claude/rules/device-safety.md`).
3. **Permissions:** may it edit? Default for code: edit, **no commit, no push**. Inspector: read-only.
4. **What to return:** conclusions and abridged evidence, never full logs or file dumps.
5. "End with the output contract."

**Output contract.** Every agent ends with `## Result` (done / partial / blocked), `## Changes`,
`## Validation`, `## Needs from others`, `## Risks / follow-ups`, `## Memory candidates`.

**Verify the claims that matter** by rereading the diff or re-running the check — a confident report
is not evidence. Send a wrong result back with the specific defect rather than redoing it.

## Write state down — so context loss costs nothing

Assume the session can compact or end at any point. State that only exists in the conversation is
already lost.

| What | Where | When |
|---|---|---|
| Progress on a spec'd change | the spec's `task.md` (rows, evidence log, decisions) | after each task or check |
| "Right now": components, next steps, open threads | `STATUS.md` | whenever it changes |
| A decision (with why and alternatives) | `DECISIONS.md` row → `docs/decisions/NNNN-*.md` | when made |
| How the code works, commands, invariants | `CLAUDE.md` (and package doc comments) | in the same change |
| What happened this session | `docs/sessions/<date>-<role>-<slug>.md` via `/handoff` | end of session or workstream |
| A durable trap, preference or external fact | `memory/` (main only, per `memory-policy.md`) | when it will recur |

**Memory is not a state store.** Its index is injected into every session; live state there goes
stale and clutters every future session. Progress belongs in `task.md` and `STATUS.md`.

**Session hygiene**
- One workstream per session. When a session gets long or the workstream changes: `/handoff`, start
  a new session from the project root, `/resume-handoff`. A fresh window with good files beats a long
  window with a summary of itself.
- After a compaction, re-read `STATUS.md` and the active spec's `task.md` before acting. Don't trust
  recollection for hashes, versions, paths or device identifiers — re-read or re-run.
- Launch from the project root: a session started in a subdirectory is a different project for
  Claude Code (no memory, no harness).

## Gates

- **Memory is main's to write.** Agents propose under `## Memory candidates`; main verifies, then writes.
- **Ask the user before:** `git push`, GitHub writes (issues, releases, repo settings), installing or
  restarting the user's service or app (`avocadomtp daemon install|start|stop`, copying to
  `/Applications`), changing global config, and anything on the phone outside the test folder —
  see `device-safety.md`. An approval covers the action described, not the next one.

## Spec before you build

Write a spec (`/spec`) before implementing a change that:
- touches the **write path to the phone** (fusefs staging/commit, uploads, deletes, renames) or could
  otherwise lose user data;
- changes the **daemon socket protocol** or the activity snapshot shape (the CLI and the menu bar app
  must change together);
- changes **service control or install/uninstall** (launchd, binary location, login items);
- touches **distribution or signing** (Homebrew, DMG, notarization);
- spans Go and Swift, or needs a rollback plan.

Close it with `/spec-verify`. Everything else just gets done, tested, and committed.

## Models

- **Main** runs on the strongest available model with the largest context window.
- **The inspector** uses `model: inherit`: it reasons about logs and live state.
