---
name: resume-handoff
description: Fast session orientation, the counterpart of /handoff. Reads STATUS.md, this role's latest session log, the DECISIONS.md index, any in-progress spec and git state, then prints six bullets and asks what we're working on. Use at the start of a session, after a compaction, on `/resume-handoff`, or when the user says "resume", "where were we" or "catch me up".
---

# Resume-handoff — session orientation

Do **only** these reads, in parallel. Pull more context only if the user's answer needs it.

1. **`CLAUDE.md`** — usually already loaded as project instructions; skip if so.
2. **`STATUS.md`** — the "right now" page; it outranks the session log for current state.
3. **The newest `docs/sessions/*-<role>-*.md`** (role = `main` or the agent's area). If none, the
   newest `docs/sessions/*.md`, marked "(untagged log)". If the folder is empty: "no prior session log".
4. **`DECISIONS.md` index table only** — never the decision bodies.
5. **Specs in progress:** `grep -l 'Status | \(approved\|in-progress\)' specs/*/spec.md` — names only.
6. **Git:** `git status -sb` and `git log --oneline -5`.

Memory: `MEMORY.md` is injected automatically — never read it. Treat its entries as leads to verify.

## Output — exactly this shape

Six bullets, then one question. No headers, no preamble.

```
- **Project:** <one line from CLAUDE.md>
- **Last session:** <date + one-line goal, or "no prior session log">
- **Now:** <next steps / blockers from STATUS.md — note "Last updated" if older than 7 days>
- **What changed:** <one line from the log's "What changed", or recent commits>
- **Open threads:** <from the log and STATUS; flag uncommitted or unpushed work and in-progress specs>
- **Recent decisions:** <last 2–3 rows of DECISIONS.md>

What are we working on today?
```

## Guardrails

- Orientation, not research: don't open decision bodies, specs or source files yet.
- Don't act on open threads; surface them and wait.
- If the answer names a spec, decision or file, read it then — not before.
