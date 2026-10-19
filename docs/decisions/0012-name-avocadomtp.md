# 0012. Everything is named avocadomtp

- **Status:** Accepted
- **Date:** 2026-10-19

## Context
The first versions used `avocado` for the command, agent and log.

## Decision
Use `avocadomtp` for the command, LaunchAgent label, log, socket and env vars (`AVOCADOMTP_*`). `daemon install` migrates the old `com.andymartinezot.avocado` agent and files. The GitHub repo was renamed from `avocadomcp` to `avocadomtp`.

## Consequences
Docs and memory refer only to `avocadomtp`.
