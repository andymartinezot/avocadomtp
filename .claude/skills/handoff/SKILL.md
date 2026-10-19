---
name: handoff
description: Write a session log to docs/sessions/<date>-<role>-<slug>.md summarizing what this session changed, its outcome and open threads, so the next session can /resume-handoff. Main also sweeps for memory candidates, refreshes STATUS.md, and records new decisions in DECISIONS.md. Use at the end of a session, when the context is getting long, when switching workstream, or when the user says "handoff", "wrap up" or "log this session".
---

# Handoff — session log

Write one markdown file under `docs/sessions/` (gitignored: it stays local). The only other changes
are main's steps 5–7.

## Steps

1. **Today's date** as `YYYY-MM-DD`.
2. **Role:** `main`, or the named agent's area (its name without `-agent`). Keeps `/resume-handoff`
   from picking up another console's log.
3. **Slug:** 2–4 kebab-case words for the session's main focus. Derive it; don't ask.
4. **Write `docs/sessions/<date>-<role>-<slug>.md`** with the template below, from this session's
   actual context — edits, commands, results, decisions. Do not invent.
5. **Main only — memory sweep.** Re-read the session for what belongs in memory per
   `.claude/rules/memory-policy.md`: a confirmed preference or correction about how to work, a trap
   that will recur, an environment fact not in the repo. Test each against the policy's boundary
   table; propose each in one line; write only what the user approves. "No new memories" is normal.
6. **Main only — STATUS.md and DECISIONS.md.** Update `STATUS.md` if the session changed anything it
   tracks (component state, next steps, open threads, last verified). Add a `DECISIONS.md` row and a
   `docs/decisions/` record for any decision made this session that isn't recorded yet.
7. **Main only — uncommitted work.** If `git status` shows changes, say so in one line; don't commit
   from here unless the user asks.
8. **Tell the user** the log path, one line each on STATUS/DECISIONS updates, memories written, and
   uncommitted work.

## Template

```markdown
# YYYY-MM-DD — <Title Case Session Topic>

## Goal
One paragraph: what we set out to do, as the user framed it.

## What changed
- **Files:** `path` — one-line why. (One bullet per file or tight group.)
- **Decisions:** D-NNN — title — one line. Or "none".
- **System state:** service/app installs or restarts, login items, global config, anything done on
  the phone (test folders created/removed). Or "none".
- **Version control:** commits (hash + subject), pushes. Or "none".

## Outcome
One paragraph: what works now, verified how (tests, device checks), and what is half-done or broken.
Built is not installed; committed is not pushed.

## Open threads
- One bullet per unresolved item, with enough context (file, command, symptom) to pick it up cold.

## Context for next /resume-handoff
2–4 lines: what's blocked on what, where to look first, gotchas from this session not written down
anywhere else yet.
```

## Guardrails

- Don't run tests, commit or push as part of /handoff — it's documentation.
- No secrets, no personal phone contents, no serials in the log beyond what memory already holds.
- The file is the summary: don't repeat it back to the user.
- Be honest about broken or half-done state under Outcome.
