#!/usr/bin/env bash
# Vendored from the devops-harness kit (D-015). Improve it there first when the change is generic.
#
# Read-only health check for a workspace's agent harness. Prints PASS / WARN / FAIL.
# Never prints file contents beyond names and line numbers.
#
#   harness-lint.sh [workspace]          # default: current directory
#   harness-lint.sh [workspace] --live   # also run each enabled integration's auth check
#
# Checks: memory (name == stem, index pointers, Why/How), agents (frontmatter,
# output contract, no templates registered as agents, every routed agent exists),
# the orchestrator rule, skills (frontmatter, name == dir), commands (kit scripts
# they call exist), {{…}} placeholders left in facts files, specs (Status only in
# spec.md), STATUS.md age, harness-file ignore consistency and a CLAUDE.md in
# every repo, and a long-base64 leak scan over docs/ memory/ specs/ and the root
# .md files.
#
set -uo pipefail

WS="."; LIVE=0
for a in "$@"; do
  case "$a" in
    --live) LIVE=1 ;;
    -h|--help) sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) WS="$a" ;;
  esac
done
cd "$WS" || { echo "no such workspace: $WS" >&2; exit 2; }

fails=0; warns=0
pass() { echo "PASS  $*"; }
warn() { echo "WARN  $*"; warns=$((warns + 1)); }
fail() { echo "FAIL  $*"; fails=$((fails + 1)); }

# Value of a top-level frontmatter key (first occurrence), quotes stripped.
fm() { awk -v k="$2" 'NR==1 && $0!="---" {exit} NR>1 && $0=="---" {exit} NR>1 && index($0, k":")==1 {sub("^" k ":[ ]*", ""); gsub(/^["'\'']|["'\'']$/, ""); print; exit}' "$1"; }

# ---- memory -----------------------------------------------------------------
if [[ -d memory ]]; then
  n=0; bad=0
  for f in memory/*.md; do
    [[ -e "$f" ]] || continue
    stem=$(basename "$f" .md); [[ "$stem" == "MEMORY" ]] && continue
    n=$((n + 1))
    name=$(fm "$f" name)
    [[ "$name" == "$stem" ]] || { fail "memory: $f name '$name' != filename stem"; bad=$((bad + 1)); }
    type=$(grep -m1 -E '^[[:space:]]+type:' "$f" | sed -E 's/^[[:space:]]+type:[[:space:]]*//')
    case "$type" in
      user|reference) ;;
      feedback|project)
        grep -q '\*\*Why:\*\*' "$f" && grep -q '\*\*How to apply:\*\*' "$f" \
          || { fail "memory: $f ($type) lacks **Why:** / **How to apply:**"; bad=$((bad + 1)); } ;;
      *) fail "memory: $f has no valid metadata.type"; bad=$((bad + 1)) ;;
    esac
    grep -q "($stem.md)" memory/MEMORY.md 2>/dev/null || warn "memory: $f is not listed in memory/MEMORY.md"
  done
  if [[ -f memory/MEMORY.md ]]; then
    for p in $(grep -oE '\]\([^)]+\.md\)' memory/MEMORY.md | sed -E 's/^\]\(|\)$//g'); do
      [[ -f "memory/$p" ]] || { fail "memory: MEMORY.md points at missing $p"; bad=$((bad + 1)); }
    done
  else
    fail "memory: memory/MEMORY.md (the index) is missing"; bad=$((bad + 1))
  fi
  [[ $bad -eq 0 ]] && pass "memory: $n memories, names match stems, index resolves"
else
  warn "memory: no memory/ directory"
fi

# ---- agents -----------------------------------------------------------------
# Commits is optional: read-only agents have nothing to commit.
CONTRACT="## Result|## Changes|## Validation|## Needs from others|## Risks / follow-ups|## Memory candidates"
if [[ -d .claude/agents ]]; then
  n=0; bad=0
  for f in .claude/agents/*.md; do
    [[ -e "$f" ]] || continue
    stem=$(basename "$f" .md)
    n=$((n + 1))
    name=$(fm "$f" name); desc=$(fm "$f" description)
    [[ "$name" == "$stem" ]] || { fail "agents: $f name '$name' != filename stem"; bad=$((bad + 1)); }
    [[ -n "$desc" ]] || { fail "agents: $f has no description"; bad=$((bad + 1)); }
    missing=""
    IFS='|'; for h in $CONTRACT; do grep -qF "$h" "$f" || missing="$missing, $h"; done; unset IFS
    [[ -z "$missing" ]] || { fail "agents: $f output contract lacks${missing#,}"; bad=$((bad + 1)); }
  done
  [[ $bad -eq 0 ]] && pass "agents: $n agents, frontmatter and output contract ok"

  # Claude Code registers EVERY .md in .claude/agents/ — a template left there becomes a real agent.
  bad=0
  for f in .claude/agents/*.md; do
    [[ -e "$f" ]] || continue
    name=$(fm "$f" name)
    case "$(basename "$f")|$name" in
      _*|*'<'*|*'{{'*) fail "agents: $f registers as agent '$name' (template or placeholder) — move templates to .claude/templates/"; bad=$((bad + 1)) ;;
    esac
  done
  [[ $bad -eq 0 ]] && pass "agents: no templates or placeholder agents registered"

  # Every agent the root CLAUDE.md routes to must exist.
  if [[ -f CLAUDE.md ]]; then
    missing=""
    for a in $(grep -oE '`[a-z][a-z0-9-]*-agent`' CLAUDE.md | tr -d '`' | sort -u); do
      [[ "$a" == "main-agent" ]] && continue
      [[ -f ".claude/agents/$a.md" ]] || missing="$missing $a"
    done
    if [[ -n "$missing" ]]; then fail "routing: CLAUDE.md names agents with no file in .claude/agents/:$missing"
    else pass "routing: every agent named in CLAUDE.md exists"; fi
  fi
fi

# ---- orchestrator rule -----------------------------------------------------
if [[ -f .claude/rules/orchestrator.md ]]; then pass "orchestrator: .claude/rules/orchestrator.md present"
else warn "orchestrator: .claude/rules/orchestrator.md missing — the main session has no shared protocol (copy it from the kit's template/)"; fi

# ---- skills -----------------------------------------------------------------
if [[ -d .claude/skills ]]; then
  n=0; bad=0
  for d in .claude/skills/*/; do
    [[ -d "$d" ]] || continue
    dir=$(basename "$d"); f="${d%/}/SKILL.md"
    [[ -f "$f" ]] || { fail "skills: $dir has no SKILL.md"; bad=$((bad + 1)); continue; }
    n=$((n + 1))
    name=$(fm "$f" name); desc=$(fm "$f" description)
    [[ "$name" == "$dir" ]] || { fail "skills: $f name '$name' != directory"; bad=$((bad + 1)); }
    [[ -n "$desc" ]] || { fail "skills: $f has no description"; bad=$((bad + 1)); }
  done
  [[ $bad -eq 0 ]] && pass "skills: $n skills, name == directory"
fi

# ---- commands: the kit scripts they call still exist ------------------------
if [[ -d .claude/commands ]]; then
  n=0; bad=0
  for f in .claude/commands/*.md; do
    [[ -e "$f" ]] || continue
    n=$((n + 1))
    for p in $(grep -oE 'bash /[^ ]+\.sh' "$f" | sed 's/^bash //' | sort -u); do
      [[ -f "$p" ]] || { fail "commands: $f calls $p, which doesn't exist (kit moved? fix the path)"; bad=$((bad + 1)); }
    done
  done
  [[ $bad -eq 0 ]] && pass "commands: $n commands, referenced scripts exist"
fi

# ---- placeholders left in the project's facts files -------------------------
# Skills and .claude/templates/ are excluded: they are behavior, and quote {{…}} or <…> on purpose.
facts=""
for p in CLAUDE.md STATUS.md DECISIONS.md .claude/settings.json .claude/rules/*.md .claude/commands/*.md .claude/agents/*.md; do
  [[ -f "$p" ]] && facts="$facts $p"
done
if [[ -n "$facts" ]]; then
  hits=$(grep -cHE '\{\{[^}]+\}\}' $facts 2>/dev/null | grep -v ':0$' || true)
  if [[ -n "$hits" ]]; then
    warn "placeholders: {{…}} left to fill (run /harness-init, or replace with TODO: <what settles it>):"
    for h in $hits; do
      f=${h%:*}; l=$(grep -nE '\{\{[^}]+\}\}' "$f" | head -1 | cut -d: -f1)
      echo "      $f: ${h##*:} (first at line $l)"
    done
  else
    pass "placeholders: none left in the facts files"
  fi
fi

# ---- specs ------------------------------------------------------------------
if [[ -d specs ]]; then
  n=0; bad=0
  for d in specs/[0-9][0-9][0-9]-*/; do
    [[ -d "$d" ]] || continue
    n=$((n + 1))
    [[ -f "$d/spec.md" ]] || { fail "specs: $d has no spec.md"; bad=$((bad + 1)); continue; }
    grep -qE '^\|[[:space:]]*Status[[:space:]]*\|' "$d/spec.md" || { fail "specs: $d/spec.md has no Status row"; bad=$((bad + 1)); }
    for other in plan.md task.md; do
      [[ -f "$d/$other" ]] || continue
      l=$(grep -nE '^(\|[[:space:]]*Status[[:space:]]*\||Status:)' "$d/$other" | cut -d: -f1 | head -1)
      [[ -z "$l" ]] || { fail "specs: $d$other:$l restates Status (it lives only in spec.md)"; bad=$((bad + 1)); }
    done
  done
  [[ $bad -eq 0 ]] && pass "specs: $n specs, Status only in spec.md"
fi

# ---- STATUS.md age ----------------------------------------------------------
if [[ -f STATUS.md ]]; then
  d=$(grep -m1 -oiE 'last updated:?[^0-9]*[0-9]{4}-[0-9]{2}-[0-9]{2}' STATUS.md | grep -oE '[0-9]{4}-[0-9]{2}-[0-9]{2}')
  if [[ -n "$d" ]]; then
    then_s=$(date -j -f %Y-%m-%d "$d" +%s 2>/dev/null || date -d "$d" +%s 2>/dev/null)
    age=$(( ($(date +%s) - then_s) / 86400 ))
    if [[ $age -gt 7 ]]; then warn "STATUS.md: last updated $d ($age days ago)"; else pass "STATUS.md: updated $d"; fi
  else
    warn "STATUS.md: no 'Last updated: YYYY-MM-DD' line"
  fi
fi

# ---- harness files: ignored consistently across repos -----------------------
repos=$(find . -maxdepth 3 -name .git \( -type d -o -type f \) 2>/dev/null | sed 's|/\.git$||' | sort)
if [[ -n "$repos" ]]; then
  total=0; ignored=0; mixed=""
  for r in $repos; do
    total=$((total + 1))
    if git -C "$r" check-ignore -q --no-index CLAUDE.md 2>/dev/null; then ignored=$((ignored + 1)); else mixed="$mixed $r"; fi
  done
  if [[ $ignored -eq 0 ]]; then pass "ignore: harness files are committable in all $total repos"
  elif [[ $ignored -eq $total ]]; then pass "ignore: harness files are ignored in all $total repos"
  else warn "ignore: harness files ignored in $ignored/$total repos — not ignored in:$mixed"; fi

  # Each sub-repo should get its own CLAUDE.md (template: .claude/templates/repo-CLAUDE.md).
  have=0; without=""; nrepos=0
  for r in $repos; do
    [[ "$r" == "." ]] && continue
    nrepos=$((nrepos + 1))
    if [[ -f "$r/CLAUDE.md" ]]; then have=$((have + 1)); else without="$without ${r#./}"; fi
  done
  if [[ $nrepos -gt 0 ]]; then
    if [[ -z "$without" ]]; then pass "repo CLAUDE.md: all $nrepos repos have one"
    else warn "repo CLAUDE.md: $have/$nrepos repos have one (fine if written lazily) — missing in:$without"; fi
  fi
fi

# ---- leak scan: long base64-ish runs in harness docs ------------------------
scan=""
for p in docs memory specs STATUS.md DECISIONS.md CLAUDE.md; do [[ -e "$p" ]] && scan="$scan $p"; done
if [[ -n "$scan" ]]; then
  hits=$(grep -rnoIE '[A-Za-z0-9+/=]{100,}' $scan 2>/dev/null | cut -d: -f1,2 | sort -u)
  if [[ -n "$hits" ]]; then
    fail "leak scan: long token-like strings at (inspect by line number, don't print the line):"
    printf '%s\n' "$hits" | sed 's/^/      /'
  else
    pass "leak scan: no token-like runs >=100 chars in$scan"
  fi
fi

# ---- live: integration auth checks -------------------------------------------
# Reads rows of .claude/rules/integrations.md whose Status cell is "enabled" and
# runs the command in the "Auth check" cell. Output is discarded; only the exit code is shown.
if [[ $LIVE -eq 1 ]]; then
  f=.claude/rules/integrations.md
  if [[ -f "$f" ]]; then
    header=$(grep -m1 -E '^\|.*Auth check' "$f")
    col_auth=$(printf '%s' "$header" | awk -F'|' '{for(i=1;i<=NF;i++) if ($i ~ /Auth check/) print i}')
    col_status=$(printf '%s' "$header" | awk -F'|' '{for(i=1;i<=NF;i++) if ($i ~ /Status/) print i}')
    col_chan=2
    to=""; command -v timeout >/dev/null 2>&1 && to="timeout 20"   # coreutils; absent on stock macOS
    if [[ -z "$col_auth" || -z "$col_status" ]]; then
      warn "live: $f has no 'Auth check' / 'Status' columns"
    else
      while IFS= read -r row; do
        status=$(printf '%s' "$row" | awk -F'|' -v c="$col_status" '{gsub(/[ `]/, "", $c); print $c}')
        [[ "$status" == "enabled" ]] || continue
        chan=$(printf '%s' "$row" | awk -F'|' -v c="$col_chan" '{gsub(/^[ ]+|[ ]+$/, "", $c); print $c}')
        cmd=$(printf '%s' "$row" | awk -F'|' -v c="$col_auth" '{print $c}' | sed -nE 's/.*`([^`]+)`.*/\1/p')
        [[ -n "$cmd" ]] || { warn "live: $chan has no auth command"; continue; }
        if $to bash -c "$cmd" >/dev/null 2>&1 </dev/null; then pass "live: $chan auth ok"
        else fail "live: $chan auth check failed — log in or see that row's Gotchas, then re-run"; fi
      done < <(grep -E '^\|' "$f" | tail -n +3)
    fi
  else
    warn "live: no $f"
  fi
fi

echo "---"
echo "$fails fail, $warns warn"
[[ $fails -eq 0 ]]
