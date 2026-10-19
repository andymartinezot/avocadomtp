# PLAN NNN — <Title>

**How.** Contract and status live in `spec.md`; the work lives in `task.md`. No status here.

## Rollout
Follow the ladder in `.claude/rules/device-safety.md`: fake phone → test folder on the real phone →
the user's installed setup (user approves that step).

| # | Step | Rung | Gate |
|---|---|---|---|
| 1 | | fake phone | tests pass |
| 2 | | test folder | hashes match, cleanup done |
| 3 | | installed | the user's OK |

## Rollback
Recorded **before** the change lands — a commit hash, not a description.

| Thing | Recorded prior value | Restore command |
|---|---|---|
| Last good commit | `<hash>` | `git checkout <hash> -- . && go build -o bin/avocadomtp ./cmd/avocadomtp && ./bin/avocadomtp daemon install` |
| Installed app | `<hash>` | rebuild from `<hash>` and copy to `/Applications` |

If the user's setup breaks after the change: **roll back first, debug second.**

## Alternatives considered

| Option | Why rejected |
|---|---|

## Open questions
Anything undecided that tasks depend on. Resolve it or cut the task.
