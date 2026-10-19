# SPEC NNN — <Title>

**What and why.** The contract. `plan.md` holds how, `task.md` holds the work.

| Field | Value |
|---|---|
| Status | draft <!-- draft → approved → in-progress → verified → closed. THE ONLY PLACE STATUS LIVES --> |
| Areas | (Go packages, `macos/`, service/install, docs) |
| Decisions | (D-NNN rows in `DECISIONS.md` this spec relies on or creates) |
| Created / updated | YYYY-MM-DD / YYYY-MM-DD |

## 1. Intent
3–5 lines: the problem and the end state, from the user's point of view. Why, not how.

## 2. Current state (evidence)
What is true today. Every row carries evidence: `file:line`, a test, or a command with abridged
output (live checks via `device-inspector-agent`). Never a guess.

| Area | Fact | Evidence |
|---|---|---|

Unverified → `TODO: verify` + what would settle it. No task may depend on it.

## 3. Desired behavior
What the user sees and what the system guarantees afterwards. Every row needs an acceptance check in §6.

| # | Behavior |
|---|---|
| B1 | |

## 4. Invariants (must NOT change)
- The `CLAUDE.md` invariants this change touches (name them).
- The user's files on the phone are never modified outside the test folder.
- (others: protocol compatibility, resource budget, …)

## 5. Blast radius
What breaks for the user if this is wrong: data loss on the phone, volumes that won't mount, a
service that won't start at login, a menu bar out of sync with the daemon, …

## 6. Acceptance checks
Runnable, with expected results. `/spec-verify` runs them. A check that wasn't run is not a pass.

| # | Rung | Check | Command / steps | Expected |
|---|---|---|---|---|
| A1 | fake phone | | `go test -race ./...` | ok |
| A2 | test folder | | | |
| A3 | installed | | | |
