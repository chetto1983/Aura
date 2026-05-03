---
agent: codex
commands:
  - name: status
    run: powershell -NoProfile -ExecutionPolicy Bypass -File scripts/status.ps1
  - name: verify_go
    run: powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify-go.ps1
  - name: verify_web
    run: powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify-web.ps1
args:
  - goal
  - slice
---

# Aura Implementation Loop

Use this loop to implement one Aura slice at a time without context rot.

## Goal

{{ args.goal }}

## Slice

{{ args.slice }}

## Context

Aura is a Go Telegram assistant with an embedded React dashboard. It is evolving into a standalone second brain: source inbox, OCR, wiki, search, graph, review queue, skills, MCP, scheduled routines, and AuraBot swarm.

Before implementing, read:

1. `AGENTS.md`
2. `docs/implementation-tracker.md`
3. `docs/aura-picobot-hermes-inventory-2026-05-03.md`
4. Any slice-specific plan/doc/code touched by the goal

Use Picobot (`D:\tmp\picobot`) as reference for agent-loop, cron, MCP, tool-registry, memory, and skill patterns. Copy patterns only when they fit Aura's review-gated second-brain model.

## Loop

Each iteration starts with fresh context and uses persisted files as handoff, not chat history.

1. Inspect
   - Run `status`.
   - Identify user changes and preserve them.
   - Locate the smallest relevant code/doc surface with `rg`.
   - Read only the files needed for this slice.

2. Plan
   - Write a compact implementation note in the tracker or slice doc when the change is not trivial.
   - Define acceptance checks before editing.
   - Split work into backend, frontend, tests, and docs only if the slice needs all of them.

3. Implement
   - Make focused edits only.
   - Prefer existing Aura packages and patterns.
   - Keep durable writes review-gated unless the user explicitly asked for direct mutation.
   - Do not expose raw tool arguments, secrets, OCR bodies, or logs in user-facing output.

4. Verify
   - Run targeted tests first.
   - Run `verify_go` before a code slice is complete.
   - Run `verify_web` when web code or embedded dashboard assets changed.
   - If a check fails, summarize the failure in the tracker or working note, fix the smallest cause, and repeat with fresh context.

5. Review
   - Inspect `git diff`.
   - Confirm no `.env`, database files, logs, binaries, generated raw wiki data, or unrelated user edits are staged.
   - Update `docs/implementation-tracker.md` with work completed, tests run, and next slice.

6. Commit
   - Stage explicit paths only.
   - Commit one atomic slice.
   - Use a short message and include files touched plus verification in the body.

## Constraints

- Never revert user edits unless explicitly requested.
- Never use `git add -A` or `git add .`.
- Never commit `.env`, `logs/`, database files, raw PDFs/OCR artifacts, binaries, or dashboard temp files.
- Prefer `search_memory`, source/wiki tools, proposals, and review queue over broad filesystem mutation.
- Admin surfaces such as exec, broad filesystem, MCP expansion, and skill mutation must remain gated.
- Scheduled agent jobs must default to propose-only writes.
- If the slice touches concurrency or swarm execution, add or run race-sensitive tests where practical.

## Validation

Current status:

{{ commands.status }}

Go verification:

{{ commands.verify_go }}

Web verification:

{{ commands.verify_web }}

## Exit Conditions

Finish only when:

- The slice goal is handled end to end.
- Relevant tests pass or any skipped checks are explicitly justified.
- The tracker is updated.
- The commit is atomic and excludes unrelated user changes.

When done, output:

`AURA_LOOP_DONE`

Then include:

- commit SHA
- files touched
- checks run
- next recommended slice
