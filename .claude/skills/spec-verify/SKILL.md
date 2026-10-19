---
name: spec-verify
description: Run a spec's acceptance checks (tests, plus on-device checks through device-inspector-agent where the check is read-only), record the evidence in task.md, and move the spec to verified — or back to in-progress with the specific failures. Use after the /spec tasks are done.
argument-hint: "<NNN-slug>   (directory under specs/)"
arguments:
  - spec
disable-model-invocation: true
---

# /spec-verify $spec

Reads `specs/$spec/`: `task.md` for completion, `plan.md` for rollback, `spec.md` for the checks.

1. **Work done?** Every `task.md` row is `done` (with a commit if committed) or `n/a` with a reason.
   Otherwise stop and list them.
2. **Rollback recorded?** `plan.md` must name the last good commit and the restore command. Otherwise stop.
3. **Run the checks** from `spec.md` §6, exactly:
   - tests and builds: main runs them (`go test -race ./...`, `go vet`, the xcodebuild line);
   - read-only live checks: `device-inspector-agent`, with the table verbatim, "report actual vs
     expected", "read-only";
   - on-device write checks (transfers into the test folder): main runs them per
     `.claude/rules/device-safety.md` — random data, hashes, cleanup.
   Add checks for the §4 invariants.
4. **Compare.** Each check is `pass`, `fail` or `blocked` (phone asleep, extension disabled). **A check
   that wasn't run is not a pass.** Verify anything surprising yourself.
5. **Record** in the `task.md` evidence log: date, each outcome, abridged command.
6. **Decide.**
   - **All pass:** `Status: verified` in `spec.md`; update `STATUS.md`; tell the user what's ready to
     commit, install or push — each still needs their OK.
   - **Any fail:** leave `Status: in-progress`; fix or send back the specific defect. If the user's
     installed setup is broken, propose the `plan.md` rollback first — roll back, then debug.
7. **Memory sweep:** a durable trap or a correction about how to work → propose it per
   `.claude/rules/memory-policy.md`.
8. `Status: closed` once the user has what they wanted (committed / installed / released).
