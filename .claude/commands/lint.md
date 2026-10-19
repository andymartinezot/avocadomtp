---
description: Health-check this project's harness (memory format and index, agents and routing, orchestrator rule, skills, leftover placeholders, specs, STATUS.md age, ignore consistency, leak scan).
argument-hint: ""
---

Run, from the project root:

```bash
bash scripts/harness-lint.sh .
```

Then report in this shape, and nothing else:

1. The final `N fail, M warn` line.
2. Each FAIL, one line each, with the concrete fix (file to edit and what to change). Fix only if the user asks.
3. Each WARN, one line each, saying whether it's expected or needs action.

**Leak-scan hits:** report them as `file:line` exactly as printed. **Never open, cat or grep those
lines**: they may be a credential. Tell the user and let them decide.
