# AGENTS.md

This file gives Codex repo-level instructions for working on Aura. It bridges
Codex to the existing Claude/Ralph operating rules. Treat this file as the first
context anchor, not the conversation history.

## Authority

1. Existing code behavior and tests.
2. `D:\Aura\CLAUDE.md`.
3. `D:\Aura\docs\aura-master-plan.md`.
4. `D:\Aura\scripts\ralph\CLAUDE.md` for long-running slice execution.
5. Current user instructions in the active turn.
6. Web search or model memory.

If these conflict, stop and name the conflict before editing.

## Operating Rule

Do not start broad implementation work until operationally ready. Being
operational means:

- The requested objective is reduced to one bounded slice.
- The source of truth for that slice is known.
- The affected files are identified with `rg`/direct reads.
- The verification command is known.
- The dirty git state has been checked.
- Any destructive command is explicitly avoided unless the user requested it in
  the current turn.

Conversation context is not durable state. Reconstruct state from files.

## Mandatory Startup For Aura Work

For architecture, refactor, loop, agent, Telegram, tool, runtime, or storage
work, read only the minimum needed, in this order:

1. `D:\Aura\CLAUDE.md`
2. `D:\Aura\docs\aura-master-plan.md`
3. `D:\Aura\.planning\progress.txt` if present
4. `D:\Aura\prd.json` if working from the Ralph queue
5. Directly affected source files

Do not read the whole repository to feel safer. Make a small map, then act.

## Codex Loop For Aura

Use this loop for every non-trivial change:

1. Inspect: read the smallest set of files that define the behavior.
2. Hypothesis: state what is wrong or what must change.
3. Plan: state the precise files and verification.
4. Patch: make the smallest coherent change.
5. Verify: run targeted tests first, broader gates when needed.
6. Record: only append durable learnings when the work actually ships.

Never bundle unrelated cleanup with a slice.

## Ralph Compatibility

Aura already has a Ralph-style loop:

- Prompt: `D:\Aura\scripts\ralph\CLAUDE.md`
- Queue: `D:\Aura\prd.json`
- Progress: `D:\Aura\.planning\progress.txt`
- Script: `D:\Aura\scripts\ralph\ralph.sh`

Use the Ralph files as operating guidance, not as an automatic command to run.
Do not run `scripts/ralph/ralph.sh` unless the user explicitly asks. It is
designed to spawn Claude and commit work; Codex should first follow the same
discipline manually and safely.

When executing a Ralph queue story manually:

- Pick exactly one `passes: false` story, normally the lowest priority number.
- Do not modify other stories.
- Do not reformat the whole `prd.json`.
- Mark `passes: true` only after verification succeeds.
- Append to `.planning/progress.txt` only after the slice is actually shipped.

## Hard Constraints

- Master-direct workflow: do not create branches, PRs, or push unless the user
  asks in the current turn.
- Never run `git reset --hard`, `git clean`, or destructive delete commands
  unless explicitly requested in the current turn.
- Never modify tests just to make them pass. Fix production code unless the task
  explicitly targets tests or deleted code.
- No new dependencies unless the active slice explicitly requires them.
- Preserve tool argument privacy: log tool names and argument keys, never values.
- Preserve user data: do not delete or mutate wiki, Qdrant, SQLite, logs, raw
  sources, or runtime data unless explicitly asked.
- Follow existing patterns before inventing new abstractions.
- Avoid god files. Do not create or grow files past 600 LOC when a refactor is
  available.

## Verification

After Go edits, run the narrow package tests first. For shared agent/runtime,
tooling, Telegram, API, storage, or scheduler changes, escalate to broader gates
as appropriate:

```powershell
go test ./internal/<package>
go vet ./...
go build ./...
go test ./...
```

After web edits:

```powershell
npm --prefix web run build
```

For Aura behavior, prefer ground-truth probes over textual claims. Use
`cmd/probe_chat` and artifact inspection when relevant. Tool-call counts alone
are not sufficient.

If a command fails because of sandbox, cache, or external permission issues,
request escalation instead of weakening the verification.

## Context Rot Guard

When context becomes large, stop adding more chat memory. Build a compact
context pack from files:

- Goal from the user.
- Applicable rule from `CLAUDE.md` or this file.
- Current slice from `prd.json` or master plan.
- Last relevant entry in `.planning/progress.txt`.
- Affected files and tests.

Then continue from that pack.
