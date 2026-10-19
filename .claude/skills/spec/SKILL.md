---
name: spec
description: Start or resume a spec-driven change as specs/NNN-<slug>/ with spec.md (what and why; the only place Status lives), plan.md (how, rollout, rollback) and task.md (the work and its evidence). Use before changes to the phone write path, the daemon socket protocol, service install/control, distribution or signing, Go+Swift changes, or anything needing a rollback plan. Gathers current-state evidence read-only and gets the user's approval before building.
argument-hint: "<slug>   e.g. bundle-daemon-in-app"
arguments:
  - slug
disable-model-invocation: true
---

# /spec $slug

Only the main session runs this. Criteria for when a spec is needed: `.claude/rules/orchestrator.md`
→ "Spec before you build".

## 1. Locate or create

```bash
ls -1 specs | grep -oE '^[0-9]{3}' | sort -n | tail -1
```

- If `specs/NNN-$slug/` exists, **resume it**: read all three files and its Status. Never renumber.
- Otherwise use the next number (zero-padded, `001` if none), copy `specs/_template/` to
  `specs/NNN-$slug/`, and replace `NNN` and `<Title>` in all three headers.

## 2. The contract (`spec.md`)

- Header: `Status: draft`, areas (Go packages, `macos/`, service, docs), today's date, related
  decisions (`D-NNN`).
- **1 Intent** from the user's request.
- **2 Current state:** evidence only — code facts (file:line, read by main or a subagent) and live
  facts from `device-inspector-agent` (service state, mounts, protocol behavior). Unverified facts are
  `TODO: verify` plus what would settle it.
- **3 Desired behavior**, **4 Invariants** (always: the `CLAUDE.md` invariants this touches, and "the
  user's files on the phone are never modified outside the test folder"), **5 Blast radius** (what
  breaks for the user if this is wrong: data loss? service won't start? menu bar out of sync?),
  **6 Acceptance checks** — tests and on-device checks with expected results.

## 3. The approach (`plan.md`)

Rollout along the ladder in `.claude/rules/device-safety.md` (fake phone → test folder → installed
setup), the **recorded rollback** (the last good commit hash, and the command that restores the
installed service/app from it), alternatives considered and why rejected, open questions.

## 4. The work (`task.md`)

One row per file or tight group, each with one owner (main, or a domain agent once one exists),
plus dependencies.

## 5. Self-review before showing the user

- Every desired behavior has an acceptance check; every check is runnable.
- Every step that touches the installed setup has the user as its gate.
- The rollback names a recorded commit hash, not a description.
- No task depends on a `TODO: verify` or an open question.
- Status appears in `spec.md` and nowhere else.

## 6. Approval

Summarize: intent, desired behavior, tasks, gates, open questions (ask them with
`AskUserQuestion`). On approval set `Status: approved`, add a `DECISIONS.md` row for the decisions
the spec makes, and note the spec under "Now" in `STATUS.md`.

## 7. Execute

Set `Status: in-progress`. Work the task rows (delegate per the routing table), updating each row's
State as it completes and appending to the evidence log. Verify claims that matter. When every row is
`done` or `n/a`, run `/spec-verify NNN-$slug`.
