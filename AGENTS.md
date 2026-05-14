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

Codex memory rule:

- For non-trivial Aura work, do not rely on chat memory. Reconstruct the slice
  from durable files, then keep source files, external references, example
  paths, adopted patterns, rejected patterns, and verification fixtures close to
  the decision or module being changed.

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

### Cron And Background Runs

Cron is a scheduled entrypoint, not a private runtime.

- Cron may detect due schedules and create durable schedule-fire records.
- Cron must not call the LLM, run tools, write memory/wiki state, or send
  channel messages directly.
- Every schedule fire needs an idempotency key, delegated actor, capability
  grant snapshot, delivery mode, and `run_id` or `workflow_id`.
- Recurring downtime defaults to coalescing the latest due fire inside the
  catch-up window; never implement unlimited catch-up bursts.
- Overlap defaults to forbid per schedule. Parallel fires require explicit
  idempotent read-only policy, max concurrency, and budget.
- Retries reuse the same fire id and idempotency key and go through
  workflow/outbox semantics.
- Cancellation preserves schedule/fire/run history and prevents future fires.
- Use `D:/Aura/docs/cron-background-run-reference-map.md` when working on cron,
  background jobs, scheduled agent jobs, source watchers, or missed-run policy.

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

### Wiki Graph

Aura's wiki is a graph, not a folder of isolated Markdown pages.

- Pages are graph nodes. Slugs are stable node IDs.
- Body `[[slug]]` links are semantic narrative edges.
- `related:` is only for intentional non-prose edges and automatic backlinks.
  Do not duplicate every body link into `related:`.
- `sources:` and inline `^[src_xxx]` markers connect claims back to source
  evidence.
- Wiki purpose/schema files steer ingest, query, lint, and graph maintenance.
  Treat them as control memory, not ordinary wiki pages.
- Ordinary wiki mutations must go through `wiki.Store.WritePage` or tools that
  call it, so validation, atomic writes, git status, backlinks, materialized
  graph files, graph index refresh, and reindex submission all happen.
- Before creating a page, search the existing wiki/graph and reuse a matching
  slug when one exists.
- Graph traversal should use bounded graph-aware retrieval or dedicated graph
  tools. Do not read large graph dumps when a neighborhood/path query would do.
- Prefer graph relevance signals over raw adjacency dumps: direct links, shared
  sources, common neighbors, and page-type affinity.
- No new graph database while Markdown wiki remains the source of truth.
- GraphRAG for the wiki layer is local-first: hybrid search seeds, GraphIndex
  expands, graph signals rank, community reports guide global sensemaking, and
  cited capsules return the bounded result.
- Community detection and community reports are rebuildable projections with
  freshness state, not canonical knowledge. Promote useful reports into wiki
  synthesis pages only through review/proposal flow.
- Use `D:/Aura/docs/graphrag-local-first-reference-map.md` when working on
  wiki GraphRAG, graph scoring, community reports, or Neo4j-sidecar questions.
- Use `D:/Aura/docs/agent-parallel-loop-2026-reference-map.md` when working on
  swarm, subagents, parallel agent loops, orchestration traces, or worker
  authority.

### Swarm And Parallel Agent Loop

Swarm must stay flexible and powerful without becoming an unbounded hidden
runtime.

- Treat swarm as a policy-driven run graph, not a fixed list of hardcoded
  workers.
- Support two distinct collaboration modes:
  - subagent/delegation mode, where child runs report bounded results back to
    the caller;
  - agent-team mode, where named teammates coordinate through a shared task
    board and durable mailbox.
- A safe read-only fanout is allowed as an implementation slice, but it is not
  the final architecture ceiling.
- Do not make `max_spawn_depth=1`, fixed roles, or read-only-only workers
  permanent architectural invariants.
- Every child agent needs an explicit goal, curated context, tool/capability
  grant, model/provider choice when relevant, budgets, output schema, artifact
  policy, and parent-run authority.
- Prefer topology-aware execution: direct, fanout, plan-execute, critic-review,
  artifact-build, repair-loop, hierarchical, or hybrid DAG execution according
  to task shape.
- Child outputs must return structured observations, citations, confidence, and
  artifact handles. Do not dump full child transcripts into parent context.
- Child durable writes must pass through proposal or workflow gates; children do
  not mutate wiki, memory, skills, or source truth directly.
- Agents may message teammates directly by name when the topology is
  `team_collaboration`. These messages are durable addressed events, not hidden
  free-form transcript dumps.
- Agent teams need a shared task board with pending, in-progress, completed,
  blocked, and failed states; dependencies; explicit assignment; self-claim;
  and claim locking or compare-and-swap semantics.
- Plan approval and quality hooks are part of team coordination. Risky
  teammates can remain read-only until their plan is approved.
- Persist orchestration traces for spawn, delegate, message/workspace update,
  task create/claim/complete, mailbox message, tool call, return, aggregate,
  plan approval, stop, retry, cancellation, and budget events.
- Optimize for critical path, task quality, useful-agent utilization, protocol
  overhead, useful-message ratio, blocked-task latency, error amplification,
  and trace debuggability, not raw agent count.
- Do not attempt RL-like self-evolution of swarm policy before traces, evals,
  rollback, and validation gates exist.

### Observability, Audit, And Retention

Aura must be inspectable without turning logs into a private-data landfill.

- Treat execution traces, operational logs, and governed artifacts/audit events
  as three separate planes.
- `run_events`/trace metadata is the durable causal record for runs, tools,
  questions, memory writes, cron fires, workflows, and swarm graph events.
- Operational logs are short-retention process diagnostics. They correlate with
  runs via `run_id`, `trace_id`, and `span_id`, but they are not source of truth.
- Full prompts, user messages, tool arguments, tool outputs, retrieved chunks,
  child transcripts, OCR JSON, and file contents are payload artifacts, not
  default log fields.
- Default trace policy is metadata-only: tool names, call ids, argument keys,
  status, elapsed time, token/cost counters, error class, redacted preview,
  source ids, and artifact handles.
- Audit events are required for identity/grant changes, authorization denials,
  settings changes, memory writes, wiki/source changes, skill lifecycle changes,
  cron schedule changes, exports, purges, backups/restores, and privileged
  payload access.
- OpenTelemetry is an export/correlation projection, not Aura's canonical
  store. Keep local-first SQLite events and artifact metadata authoritative.
- Retention defaults: operational logs one day, trace metadata 30 days via
  `AURA_TRACE_RETENTION_DAYS`, debug payload artifacts seven days, reviewable
  payload artifacts 30 days, audit metadata 365 days.
- Purges append tombstone/audit events and delete payload artifacts before
  metadata when causal integrity needs a redacted trail.
- Use `D:/Aura/docs/observability-audit-retention-reference-map.md` when
  working on tracing, logging, audit bundles, redaction, retention, dashboards,
  exporters, or privileged trace payload access.

### RAG Freshness

RAG indexes are projections. They accelerate recall but do not define truth.

- Canonical stores must be named before implementing retrieval changes.
- Every FTS, Qdrant, graph-document, or embedding-cache projection needs an
  explicit freshness state: fresh, stale, rebuilding, degraded, or disabled.
- Projection state should include source and indexed watermarks, schema or
  embedding-model hash, last success/error, pending jobs, and indexed counts.
- GraphIndex may stay synchronous for wiki writes; FTS and Qdrant reindexing
  must be durable when losing the job would leave stale user-visible retrieval.
- Reindex jobs must be op-aware. Delete, rename, and embedding-model changes are
  not page upserts.
- Full rebuilds must be forceable. Startup warm-cache reuse must not make an
  explicit reindex request a no-op.
- Retrieval capsules must expose stale/degraded projections. An empty stale
  vector result is never evidence that no source exists.
- Prefer exact, FTS, and GraphIndex fallback before allowing stale vector state
  to shape an answer.

### Sources And Examples

Development must make prior research easy to find again.

- Every architectural slice must name its source files, external references, and
  example projects before planning implementation.
- Keep source links close to the decision they support: ADR `sources`,
  `local_evidence`, PRD references, or a dedicated reference map.
- Prefer concrete example paths over vague notes. Use full paths such as
  `D:/tmp/llm_wiki/src/lib/wiki-graph.ts` and stable URLs for online sources.
- When adopting a pattern from an example repo, record what is adopted, what is
  rejected, and why. Do not leave "inspired by X" without a bounded mapping.
- Complex tools and modules need discoverable fixtures or examples that show
  minimal, normal, and failure/recovery usage.
- If a source or example changes a decision, update the decision log in the same
  slice. Conversation memory is not enough.

## Mandatory Startup For Aura Work

For architecture, refactor, loop, agent, Telegram, tool, runtime, or storage
work, read only the minimum needed, in this order:

1. `D:\Aura\CLAUDE.md`
2. `D:\Aura\docs\aura-master-plan.md`
3. `D:\Aura\.planning\progress.txt` IF NOT PRENDET CREATE UP UPDATE EVERY TIME
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
