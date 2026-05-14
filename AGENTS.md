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

## Architecture Decisions To Preserve

### Cache Plane

Aura's cache is a separate plane, not part of canonical state.

- Default L1 cache: bounded process-local disposable cache for hot reads.
- Default L2 cache: dedicated SQLite cache database such as `cache.db`.
- Large cached payloads: content-addressed filesystem blobs plus SQLite
  metadata.
- The canonical run/event database must not absorb cache churn.
- Never store runs, run events, workflow steps, outbox/inbox state, questions,
  approvals, identity grants, memory write authority, delivery status, or
  side-effect state as cache entries.
- Embeddings, OCR/extract outputs, rendered prompt/tool blocks, tool schemas,
  query vectors, and expensive retrieval intermediates are valid cache domains.
- Existing `embedding_cache` in the main database is only a compatibility
  bridge. The target architecture moves deterministic caches behind the cache
  plane.
- Cache deletion must be a supported recovery path. If deleting the cache
  changes what Aura believes to be true, the design is wrong.
- Do not introduce Badger, bbolt, Valkey, Redis, or another cache backend unless
  a measured slice proves SQLite cache storage is the limiting factor.

### Transaction Boundaries

Aura does not use distributed transactions across SQLite, filesystem, Qdrant,
cache, chat channels, or external tools.

- Every operation must name its canonical store before implementation.
- Keep SQLite transactions short and local: append events, update snapshots, and
  enqueue workflow/outbox work. Do not hold DB transactions across network calls
  or long filesystem work.
- External delivery and risky side effects run after commit through durable
  workflow/outbox rows with idempotency keys.
- If a side effect is `unknown`, reconcile, ask, compensate, or fail with an
  auditable reason. Never retry blindly.
- Qdrant, FTS, RAG indexes, and cache are rebuildable projections.
- Filesystem writes must be content-addressed or temp-file-plus-rename. Orphaned
  blobs are acceptable only when they are garbage-collectable.
- Volatile queues are allowed only when canonical state can reconstruct the work.

### Memory Layers

Do not treat "memory" as one bucket.

- Runtime continuity: active run context, thread/session state, run event log,
  conversation archive, and agent working memory are separate layers.
- User/project knowledge: user profile memory, project decision memory, source
  corpus, knowledge wiki, wiki schema/control files, wiki index/log, and derived
  artifacts have different write rules.
- Aura learning/procedure: operational memory, raw experience store, proposal
  queue, and skills are separate from user memory.
- Retrieval and acceleration: RAG collection registry, RAG indexes, and cache are
  projections or support systems, not canonical memory.
- Ambiguous user-memory writes require the normal question flow. Do not invent a
  Telegram-only memory approval path.
- Raw tool failures go to experience/learning. Only validated repeated lessons
  may become operational memory, skills, or tool policy.
- The wiki is curated knowledge. Do not put chat logs, raw tool failures, private
  scratchpad notes, or heartbeat/status noise into it.
- Schema/control files such as AGENTS.md, CLAUDE.md, and wiki schema docs change
  future agent behavior. Edit them deliberately and with evidence.

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
