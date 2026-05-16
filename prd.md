# Aura Deep Refactor PRD

**Date:** 2026-05-14  
**Status:** north-star PRD for the deep refactor  
**Purpose:** give Aura a clear architecture direction so every rename, move, and rewrite has a reason.

---

## 1. The Point

Aura is not failing because one package has the wrong name. Aura is failing because the codebase no longer makes the central idea obvious.

The refactor exists to make this true:

> Aura has one agentic core, and everything else is an adapter, capability, or durable memory projection.

This PRD is the way forward. It is not a log of every broken thing in the repo. It defines the target shape, the module responsibilities, the migration order, and the gates that prove the system is getting simpler instead of just moving mess into new folders.

---

## 2. Product North Star

Aura is a local-first personal AI agent with compounding memory.

The user should experience Aura as:

- a private agent reachable from Telegram and web/API chat,
- a second brain that remembers through a readable Markdown wiki,
- a tool-using assistant that can search, ingest sources, schedule work, and operate on local artifacts,
- an agent that learns from failed tool calls and gets harder to fool over time,
- a system whose memory and actions can be inspected, debugged, and recovered.

The codebase must support that product by making the core loop small, testable, and reusable.

---

## 3. Architecture North Star

The target architecture is:

```text
external input
  -> channel adapter
  -> chat/run router
  -> agent core loop
  -> tools and memory ports
  -> observations and learning events
  -> final answer
  -> channel adapter
  -> durable memory/artifacts/events/experience
```

The important rule:

> Dependencies point inward toward stable contracts, not outward toward Telegram, SQLite, Qdrant, cron, or source-specific code.

The agent core should be boring. The adapters may be messy because the world is messy, but that mess must stop at the boundary.

### 3.1 Research, Sources, And Examples

Development must make prior research easy to rediscover.

For every non-trivial architecture or module slice, Aura needs a small
traceability trail:

- authoritative source files in Aura,
- external references or papers that shaped the decision,
- example repos/files that demonstrate the pattern,
- what Aura adopts from each example,
- what Aura explicitly rejects from each example,
- fixtures or example calls that future workers can run or inspect.

This is a product requirement for the refactor, not documentation polish. Aura is
being designed from existing code, local examples, papers, and agent-framework
patterns. If those references are not easy to find, future development will
re-litigate old choices and reintroduce architectural drift.

The traceability rule:

```text
decision -> sources -> examples -> adopted pattern -> rejected pattern -> verification
```

Implementation targets:

- ADR entries keep `sources`, `local_evidence`, and example mappings close to
  the decision.
- PRD sections link to the canonical decisions or source/example files when a
  requirement came from research.
- Complex tools and modules include discoverable examples for minimal, normal,
  and failure/recovery usage.
- Example-derived patterns are mapped to Aura-owned modules before
  implementation starts.
- Stale or rejected examples remain recorded with the reason they were rejected.

### 3.2 Planning-State Reconciliation

Aura already has useful planning work in `.planning/`, but those files are not
the execution queue for the deep refactor. They were written against an older
module shape and several of their target paths are moved, deleted, or absorbed
by this PRD.

The rule:

> Old waves may contribute requirements, examples, and tests. They must not
> execute as-is when they conflict with the deep-refactor architecture.

Planning-state decisions:

| Plan | Decision | What Survives | Landing Path |
| --- | --- | --- | --- |
| `.planning/CONTEXT-ENGINEERING-ROADMAP.md` | Deferred and re-authored by phase | Prompt-cache discipline, deterministic prompt/tool ordering, retrieval capsule, scratchpad, compaction rules, subagent return hygiene | Baseline may run before Phase 1. Prompt/tool determinism lands in Phases 4-5. Working memory and retrieval capsule land in Phase 7. Subagent return hygiene lands in Phase 8. |
| `.planning/wave1/fix_plan.md` | Superseded as a standalone queue | RRF, deferred tool discovery, vector-router removal, core toolset policy, probe harness | Tool discovery and tool-order work land in Phase 5. RRF and hybrid retrieval scoring land in Phase 7. The probe harness becomes Phase 5/7 verification, not a separate wave. |
| `.planning/wave2/fix_plan.md` | Deferred into RAG/memory rebuild | Bidirectional backlinks, in-memory graph index, extraction deltas, multi-page touch, provenance markers | Lands in Phase 7 after memory layers, projection freshness, and `wiki.Store.WritePage` invariants are locked. `internal/wiki` remains the graph substrate; ingest/source paths must be re-authored against the new `storage/sources` layout. |
| `.planning/wave3-agent-swarm/plan.md` | Superseded and re-authored after RunGraph decisions | Live phase visibility, OTel-style trace events, skill-as-code validation, role selection, proposal-only worker writes via `proposed_updates` | Lands in Phase 8 as part of policy-driven RunGraph and `team_collaboration`. Old assumptions such as permanent `max_spawn_depth=1`, fixed read-only-only workers, and lead-only result aggregation are not architecture. |

Operational consequence:

- Do not archive old planning files yet; they remain evidence.
- Do not implement their file paths literally without checking the phase map
  above.
- If a future executor wants to run a wave, first create a small re-authored
  phase plan against this PRD's module map.
- Root `prd.md` and `.planning/aura-deep-refactor-decisions.json` are the
  current source of truth for direction.
- `docs/aura-restructure-prd*.md` and review files are evidence for why the
  reconciliation exists, not a competing route.

---

## 4. Module Map

The rename/move work is correct only if it converges toward this map.

```text
internal/
  agent/          core loop, runtime, governance, run state
  chat/           normalized messages, runs, routing, events
  identity/       principals, channel accounts, actors, capability grants
  channels/       telegram, web, silent, swarm adapters
  agent/tools/    tool registry, tool schemas, tool execution, toolsets
  workflow/       durable tool execution, retries, idempotency, compensation
  learning/       tool experience, self-healing lessons, promotion workflow
  rag/            schema-aware retrieval, hybrid search, rerank, citations
  cache/          disposable acceleration, cache policy, cache metrics
  memory/         wiki semantics, recall behavior, memory policy
  storage/        sqlite, qdrant, artifacts, sources, indexes
  cron/           scheduled entrypoint into chat/agent
  api/            HTTP surface and dashboard endpoints
  config/         configuration loading and runtime settings
```

This map is not just naming. It is the dependency contract.

---

## 5. Responsibility Contracts

### 5.1 `internal/agent`

`agent` is the core.

It owns:

- the loop,
- LLM calls,
- tool-call iteration,
- observation handling,
- in-run self-healing feedback,
- finalization,
- governance,
- context limits,
- run stats,
- interruption/deadline behavior.

It must not own:

- Telegram formatting,
- HTTP response shape,
- cron scheduling,
- Qdrant client details,
- source conversion details,
- wiki file paths,
- dashboard behavior.

The loop contract is:

```text
Run(input, context, tools, memory ports) -> result, events, stats
```

If `agent` imports a channel package, the refactor is going in the wrong direction.

Prompt and context assembly are part of the agent runtime contract.

Aura must treat prompts as versioned runtime artifacts, not invisible prose scattered through the codebase. A prompt bundle should have clear sections for:

- role and operating mode,
- task contract,
- output contract,
- context capsule,
- memory policy,
- tool-use policy,
- safety/risk policy,
- examples,
- verification criteria.

Prompt engineering rules:

- define success criteria and evals before tuning prompts,
- keep fixed instructions separate from variable user/context data,
- use stable structured sections for complex prompts,
- tag source/context blocks with metadata and citation handles,
- put long retrieved/source context before the final task where provider behavior benefits from it,
- prefer positive, explicit behavior instructions over vague prohibitions,
- tune model/effort/context budget separately from prompt wording,
- do not mutate prompts, skills, or memory policy silently based on one failure,
- prompt changes require versioning and eval evidence.

### 5.2 `internal/chat`

`chat` is traffic control.

It owns:

- normalized inbound messages,
- run IDs,
- parent/child run IDs,
- run routing,
- event streams,
- channel-independent reply flow.

It must not own:

- Telegram rendering,
- tool implementations,
- source ingestion,
- wiki mutation logic,
- raw LLM loop internals.

The chat contract is:

```text
InboundMessage -> Run -> OutboundEvents
```

`chat` is the seam where Telegram, web, cron, silent jobs, and swarm become the same kind of work.

The run/event store contract is:

```text
append RunEvent -> update Run snapshot -> enqueue delivery/outbox work
```

Aura uses a pragmatic hybrid model:

- `run_events` is the durable append-only causal record for important run facts,
- `runs` is the current read model/snapshot used by dashboard, API, cancellation, resume, and polling,
- `runs.current_seq` is updated together with each durable event,
- event ordering is per `run_id, seq`, not global process order,
- event payloads are versioned and tolerate additive fields,
- external side effects use idempotency keys and outbox rows rather than direct best-effort delivery,
- transient streaming deltas may be compacted or non-durable, but tool calls, questions, memory writes, child runs, final output, errors, and usage are durable.

Minimum durable events:

```text
run_started
message_received
authorization_denied
prompt_built
llm_started
tool_started
tool_completed
tool_failed
question_requested
question_answered
memory_write_requested
memory_write_committed
child_run_started
child_run_completed
run_completed
run_failed
run_cancelled
```

Minimum tables:

```text
runs
  id, parent_run_id, thread_id, principal_id, actor_id, channel, mode
  status, current_seq, started_at, completed_at
  waiting_question_id, last_error, final_text_preview
  stats_json, metadata_json

run_events
  id, run_id, parent_run_id, seq, type, schema_version
  actor_id, causation_id, correlation_id, idempotency_key
  payload_json, redaction_level, created_at

run_outbox
  id, run_id, event_id, target, idempotency_key
  payload_json, status, attempts, next_attempt_at, last_error, created_at
```

Transaction boundary policy:

- Aura does not use distributed transactions across SQLite, filesystem, Qdrant,
  chat channels, cache, or external tools,
- every operation names its canonical store before implementation,
- SQLite transactions stay short and local; they may append events, update
  snapshots, and enqueue workflow/outbox work, but must not hold locks across
  network calls or long filesystem work,
- external delivery and side effects are performed after commit by durable
  workflow/outbox workers,
- completion, failure, cancellation, or unknown side-effect state is recorded in
  a later transaction,
- blind retry is forbidden when `side_effect_state = unknown`; Aura must
  reconcile, ask, compensate, or fail with an auditable reason,
- volatile queues are allowed only when canonical state can reconstruct the
  latest needed work.

Source-of-truth matrix:

```text
run execution          SQLite run_events; runs is a snapshot
workflow execution     SQLite workflow tables
outbound delivery      outbox rows for Aura intent/attempts/result
inbound messages       inbox/idempotency records
questions/approvals    durable question state linked to run_events
identity/grants        identity SQLite repositories
source corpus          content-addressed raw files plus source metadata
artifacts              content-addressed filesystem blobs plus metadata
wiki memory            Markdown/filesystem governed by memory policy
rag indexes            rebuildable FTS/Qdrant projections
cache                  disposable cache plane, never truth
learning               structured observations until promoted
```

Implementation pattern:

```text
commit canonical intent -> execute side effect -> record observation/result
```

Deletion and forget flows use intent or tombstone records when user-visible truth
changes before derived stores can be purged.

Do not event-source all of Aura. Wiki pages, source files, indexes, and tool artifacts keep their own source-of-truth rules. The run/event store is the backbone for execution, questions, observability, learning, cron, and swarm.

Observability is built from three planes:

- execution trace metadata in `run_events` and derived read models,
- operational logs for short-retention process diagnostics,
- governed payload artifacts and audit events for sensitive or high-risk
  material.

`run_events` is the durable causal timeline. It records what happened, in which
run, under which actor, with which tool/model/source handles, result status,
latency, usage, and redaction state. It does not store full prompts, user
messages, raw tool arguments, raw tool outputs, retrieved chunks, child
transcripts, OCR JSON, or file contents by default. Those are payload artifacts
with explicit classification, retention, access policy, and artifact handles.

Operational logs are not the source of truth. Logs may use JSON and
OpenTelemetry fields such as `trace_id` and `span_id`, but they must stay
short-lived, sanitized, and correlated back to run events. OpenTelemetry is an
export and interoperability projection, not Aura's internal domain model.

Audit events are separate from debug logs. Aura must audit:

- identity, role, and capability-grant changes,
- authorization denials and privilege escalation attempts,
- settings changes,
- memory write requests, approvals, commits, rejects, and rollbacks,
- wiki/source import, delete, purge, re-OCR, and export,
- skill install, update, enable, disable, and delete,
- cron schedule create, update, pause, resume, delete, and manual fire,
- artifact export, backup, restore, purge, and privileged payload access.

Retention defaults:

- operational logs: one day unless local debugging needs more,
- execution trace metadata: `AURA_TRACE_RETENTION_DAYS`, default 30 days,
  bounded to 1..365,
- debug payload artifacts: seven days unless promoted,
- reviewable payload artifacts: 30 days,
- audit metadata: 365 days by default,
- canonical knowledge, sources, wiki pages, and indexes follow their own
  source-of-truth and delete/forget policies, not trace retention.

Traceability for this area lives in
`docs/observability-audit-retention-reference-map.md`.

Question and approval state is part of `chat`, but question eligibility is part of the agent/runtime contract.

Aura must not expose a broad `ask_user` tool as an always-loaded escape hatch. That pattern risks making the model passive: instead of resolving uncertainty with available context and tools, it can ask the user for every small choice. The durable question flow uses a gate:

```text
model proposes action or needs input
-> QuestionGate evaluates need
-> allow action | deny action | emit question_requested/approval_requested
-> run waits only at the relevant blocking scope
-> answer arrives
-> question_answered/approval_answered resumes the run
```

QuestionGate rules:

- ask only when the answer materially changes the run outcome,
- do not ask when tools/context can answer safely,
- do not ask for low-risk reversible defaults,
- ask for missing required slots, conflicting instructions, permission escalation, irreversible side effects, user-memory writes without explicit intent, cross-channel delivery choices, or repeated recoverable failures,
- require a concise `why_blocking`, `blocking_scope`, answer schema, expiry, and fallback policy,
- use canonical choices for approvals and generated, context-specific choices for clarifications,
- include free-text/other input for clarification when fixed choices cannot safely cover the user's intent,
- allow a recommended option only when the runtime can state why it is safe,
- block only the smallest scope possible: tool call, run, child run, or thread,
- cap repeated questions per run and record unnecessary-question eval failures.

The model may request input through a narrow structured `request_input` action when it can justify the block. The runtime may also create the question itself when a deterministic policy, authorization boundary, or tool preflight requires it. The channel adapter only renders the question; it does not decide whether asking was valid.

Question options are not generic canned replies. The backend stores a structured answer schema:

```text
approval     canonical decisions: approve_once, approve_session, approve_persist, deny, cancel
clarification generated options: 2-4 context-specific choices with impact text, plus optional free text
secret_input  no buttons, redacted answer handling
selection     finite choices from a real source of truth, such as channel, file, memory scope, or tool mode
```

The UI may render buttons, numbered choices, or a form depending on the channel. The semantic answer is normalized before resuming the run.

### 5.3 `internal/identity`

Identity and authorization are the authority model.

They own:

- stable principals,
- external channel account mappings,
- per-run actors,
- capability catalog,
- capability grants,
- role-to-grant bootstrap bundles,
- authorization checks,
- delegation constraints,
- audit-friendly authorization decisions.

They must not own:

- Telegram or web transport behavior,
- dashboard rendering,
- raw bearer-token HTTP mechanics,
- tool implementation,
- wiki or source persistence,
- prompt text.

The vocabulary is:

```text
Principal       stable entity: human, system, service, or local owner
ChannelAccount  external account bound to a principal, such as telegram:123 or web session subject
Actor           principal acting in a run, job, API request, cron tick, or child swarm run
Capability      explicit action name such as tool.execute.search_memory or memory.user.write
Grant           capability assigned to a principal or actor with resource scope and constraints
Role            convenience bundle that creates grants; not the authorization source of truth
```

Rules:

- default authorization is deny,
- channel account IDs are not principal IDs,
- dashboard tokens authenticate sessions but do not define authority,
- API routes, tools, memory writes, cron jobs, and swarm runs all call the same authorization boundary,
- every tool declares its required capability and risk level,
- every durable run has an actor,
- cron actors and swarm child actors are delegated from a parent principal or actor,
- delegated actors receive only a subset of the parent capabilities,
- delegated grants can be constrained by resource, expiry, parent run, max depth, budget, delivery mode, and tool allowlist,
- authorization failures are structured events, not hidden branch logic.

Minimum tables:

```text
principals
  id, kind, display_name, status, created_at, metadata_json

channel_accounts
  id, principal_id, provider, external_id, display_name
  created_at, last_seen_at, metadata_json

actors
  id, principal_id, actor_type, parent_actor_id, run_id
  created_at, expires_at, metadata_json

capability_grants
  id, subject_type, subject_id, capability, resource_type, resource_id
  constraints_json, granted_by_actor_id, created_at, expires_at, revoked_at

authz_decisions
  id, actor_id, capability, resource_type, resource_id
  decision, reason, run_id, event_id, created_at
```

This is not a full external IAM system. It is the smallest durable authority model that lets Aura support chat apps, dashboard sessions, cron, swarm, memory, and tools without spreading permission logic through every package.

### 5.4 `internal/channels/*`

Channels are adapters.

They own:

- transport-specific input parsing,
- transport-specific output formatting,
- retries/throttling/fallbacks required by the transport,
- channel-specific fixtures.

They must not own:

- agent reasoning,
- memory policy,
- tool selection,
- runtime governance.

Examples:

- `channels/telegram` owns Telegram entities, progressive edits, CoT marker rendering, send/edit fallback.
- `channels/web` owns web/API delivery.
- `channels/silent` owns background/no-output delivery.
- `channels/swarm` or equivalent owns child-agent dispatch through the same hub contract.

### 5.5 `internal/agent/tools`

Tools are capabilities available to the agent.

They own:

- tool registry,
- tool schemas,
- tool descriptions,
- tool visibility tiers,
- tool discovery/search,
- tool-use examples,
- programmatic tool-call policy,
- allowlists,
- execution wrappers,
- toolsets,
- skill-backed tools,
- swarm-specific tools if exposed as agent capabilities.

They must not own:

- chat routing,
- Telegram output,
- memory persistence decisions,
- arbitrary global access.

Tool requirements:

- typed input,
- stable name,
- deterministic ordering,
- clear first-sentence description,
- documented return format,
- visibility tier,
- risk level,
- retry/idempotency class,
- structured result,
- structured recoverable error,
- retry hint,
- retry safety flag,
- curated examples when schema is insufficient,
- untrusted output envelope,
- secret-safe logging.
- declared required capability.

Tool visibility tiers:

```text
always_loaded             tiny control surface, visible every turn
deferred_discoverable     indexed/searchable, loaded on demand
programmatic_callable     opt-in tools callable from code orchestration
blocked_from_programmatic high-risk tools never callable from code orchestration
runtime_gated             hidden or narrow until policy/state makes it eligible
```

Advanced tool-use policy:

- most tools are deferred, not loaded into the model context upfront,
- broad user-question tools are runtime-gated, not always loaded,
- `tool_search` or equivalent discovery returns full definitions only when needed,
- direct permissive-load is allowed when the model saw a tool name in the manifest,
- active tool visibility is necessary but not sufficient; execution still requires authorization,
- code-level orchestration may call only active-turn, opt-in, non-recursive tools,
- large intermediate tool results stay out of the model context when code can reduce them,
- complex tools need realistic examples showing minimal, partial, and full usage patterns,
- examples are for disambiguation, not decoration.

Tool execution policy:

- every tool attempt emits a structured `ToolObservation`,
- lightweight read-only tools may execute inline,
- stateful, long-running, side-effecting, background, cron, outbound, source-ingest, memory-write, wiki-mutation, and swarm-spawn tools execute through durable workflow steps,
- the agent loop sees the same observation shape for inline tools and workflow-backed tools,
- retry decisions belong to the runtime supervisor, not individual model whim.

### 5.6 `internal/workflow`

Workflow is durable execution for risky work. It is not the agent brain.

It owns:

- persisted tool steps,
- retry scheduling,
- idempotency keys,
- side-effect reconciliation,
- compensation hooks,
- cancellation,
- durable timeouts,
- outbox coordination,
- restart recovery,
- workflow metrics.

It must not own:

- model prompting,
- tool selection,
- chat rendering,
- memory semantics,
- RAG query planning,
- user-facing policy decisions.

The key rule:

> Inline tools are allowed only when failure is cheap. Risky tools must be replayable, inspectable, and bounded.

Workflow-backed tools still emit normal agent observations:

```text
tool_started -> workflow_step_started -> ToolObservation -> workflow_step_completed|failed -> tool_completed|tool_failed
```

### 5.7 `internal/memory`

Memory is the product semantics of remembering. It is not one bucket.

It owns:

- user memory policy,
- knowledge wiki policy,
- operational memory promotion policy,
- what should become durable memory,
- what must remain archive/source/index only,
- provenance expectations,
- compaction and summary policy,
- memory quality rules.

It must not own:

- low-level SQLite implementation,
- Qdrant HTTP details,
- Telegram formatting,
- source-specific parsing.

The key rule:

> The Markdown wiki is the readable curated memory artifact. Raw sources, structured decisions, and run events remain evidence. Search/vector/index/cache state is support, not product truth.

Memory layers are grouped by purpose:

```text
Runtime continuity
active_run_context        current prompt/messages/tool observations; runtime only
thread_session_state      thread metadata, current focus, pending question refs
run_event_log             append-only causal execution record
conversation_archive      turns, compact summaries, cited evidence for consolidation
agent_working_memory      agent_note scratchpad + pinned core block; per conversation

User/project knowledge
user_profile_memory       stable user facts, preferences, constraints, identity notes
project_decision_memory   PRD, ADRs, open questions, roadmap/progress decisions
source_corpus             raw files, OCR/extracts, page spans, immutable provenance
knowledge_wiki            curated pages, concepts, syntheses, comparisons, analyses
wiki_schema_control       AGENTS/CLAUDE/wiki schema, purpose, conventions, templates, workflows
wiki_index_log            index.md catalog plus log.md chronological wiki operations
wiki_graph                page nodes plus [[slug]], related, source, provenance edges
derived_artifacts         reports, charts, decks, generated files, query outputs

Aura learning/procedure
operational_memory        validated Aura lessons, failed approaches, policy notes
experience_store          raw tool attempts, recoverable errors, retries, feedback
proposal_queue            review-gated candidate wiki/user/skill/operational updates
skills                    procedural knowledge and reusable workflows

Retrieval/projections
rag_collection_registry   layer schemas, fields, filters, citation formats
rag_indexes               FTS/Qdrant/chunks; rebuildable retrieval projections
cache_plane               embeddings/OCR/prompt/tool-result cache; not memory
```

Layer movement is explicit, never accidental:

```text
source_corpus -> knowledge_wiki        via ingest or curated synthesis
conversation_archive -> user/project   via validated consolidation or question flow
experience_store -> operational/skills via repeated-success validation
derived_artifacts -> wiki/source       only when intentionally filed or attached
proposal_queue -> durable layer        only after approval or policy-backed acceptance
```

Write policy:

- user profile memory requires explicit intent, validation, or a question when ambiguous,
- user memory writes require `memory.user.write` or a narrower capability grant,
- project decision memory is changed through PRD/ADR/progress edits, not casual chat recall,
- agent working memory can be written by Aura during a run, but it is scoped and garbage-collected with the conversation lifecycle,
- operational memory belongs to Aura and must not pollute the user wiki,
- raw tool failures live in `experience_store`; only validated lessons may be promoted,
- source corpus is immutable evidence until curated,
- wiki pages are curated knowledge, not chat logs, raw tool failures, or private scratchpad noise,
- wiki purpose/schema files steer ingest, query, lint, and graph maintenance; they are control memory, not ordinary wiki content,
- wiki schema/control files are edited deliberately because they change future agent behavior,
- proposal queue entries keep provenance, evidence, target layer, proposed action, and review state,
- indexes and cache are disposable projections.

Wiki graph contract:

- every normal wiki page is a graph node identified by slug,
- body `[[slug]]` links are semantic narrative edges,
- `related:` is for intentional non-prose edges and automatically maintained backlinks,
- `sources:` and inline `^[src_xxx]` markers are evidence edges into the source corpus,
- source ingest writes a source-summary hub plus entity/concept pages when extraction succeeds,
- all wiki graph writes go through `wiki.Store.WritePage` or higher-level tools that call it,
- the store maintains backlinks, materialized `graph/graph.json`, `graph/context.md`, and the in-memory adjacency index,
- graph relevance uses multiple signals: direct links, shared source provenance, common-neighbor structure, and page-type affinity,
- graph health checks own broken refs, orphans, stale backlinks, missing graph documents, and safe repairs,
- no graph database is introduced while Markdown remains the product truth.

Agent graph-writing policy:

- before creating a page, search existing wiki/graph hits and reuse a matching slug,
- when editing an existing page, read it first and use `expected_updated_at`,
- write `[[slug]]` in prose whenever the relationship matters to a reader,
- do not mirror prose links into `related:` because backlinks are automatic,
- use `related:` only for important relationships that do not naturally appear in prose,
- cite source-backed claims with `^[src_xxx]` or source IDs,
- prefer append/edit over replace unless the task is a deliberate rewrite,
- never use raw file writes for ordinary wiki page mutation.

### 5.8 `internal/learning`

Learning is operational experience, not secret model training.

It owns:

- tool attempt history,
- recoverable error taxonomy,
- self-heal outcome tracking,
- lesson retrieval,
- promotion workflow into memory, skills, or tool policy,
- learning metrics.

It must not own:

- raw model fine-tuning,
- channel behavior,
- silent memory writes,
- unvalidated prompt mutation,
- permanent storage backend details.

The key rule:

> Failed tool calls are feedback. Raw failures are not automatically wisdom.

The minimum loop is:

```text
tool failure -> structured feedback -> retry in same run -> persist outcome -> retrieve similar lesson later -> promote only after validation
```

The common observation contract is:

```text
ToolObservation
  status: ok | recoverable_error | blocked_error | fatal_error | cancelled
  error_kind: validation | missing_required | invalid_action | not_found | conflict | timeout | rate_limited | permission | policy_blocked | transient_upstream | tool_bug | side_effect_unknown
  retry_policy: no_retry | model_correct_args | auto_retry_idempotent | ask_user | reconcile_first
  side_effect_state: none | not_started | committed | unknown
  message_for_model: concise, redacted, corrective feedback
  idempotency_key, tool_name, tool_version, schema_version, arg_keys, args_hash, redaction_level
```

Retry rules:

- automatic retry is allowed only for read-only, idempotent, or safely transient work,
- writes and external side effects require idempotency keys,
- `side_effect_unknown` means reconcile first, ask the user, or fail with an auditable reason,
- same tool plus same args plus same error cannot loop indefinitely,
- retry budgets are per run, per tool, and per error kind,
- durable workflow execution is the target substrate for risky or long-running tools,
- no prompt, skill, memory, or code mutation happens automatically from one failure.

### 5.9 `internal/rag`

RAG is retrieval intelligence.

It owns:

- collection metadata,
- query planning,
- hybrid keyword/vector retrieval,
- filter/type validation,
- chunk-to-parent expansion,
- reranking,
- retrieval result shaping,
- citations and follow-up handles,
- RAG evaluation metrics.

It must not own:

- the meaning of memories,
- channel behavior,
- raw source conversion,
- permanent storage clients as concrete dependencies,
- unbounded context injection.

The RAG contract is:

```text
RecallRequest -> RetrievalPlan -> RetrievalHits -> cited context capsule
```

RAG must be schema-aware. Before searching a corpus, Aura should know:

- which layer it belongs to,
- what fields exist,
- which fields are filterable,
- whether vector search is available,
- which text field is the content field,
- how a hit should be cited,
- whether a child chunk needs parent expansion.

Default retrieval should be hybrid where available:

```text
query
  -> keyword/FTS candidates
  -> vector candidates
  -> metadata filters
  -> Reciprocal Rank Fusion
  -> optional rerank
  -> bounded cited hits
```

The tool surface should avoid `memory(mode=...)`. Prefer task-level tools:

- `recall_user` for user facts and preferences,
- `recall_knowledge` for wiki, sources, and curated project knowledge,
- `recall_operational` for Aura's own lessons and failed approaches.

A federated recall path is allowed only if every hit keeps its layer label and citation handle.

Graph-aware retrieval is required for the wiki layer.

Aura should not force the model to manually read `graph.json` for ordinary graph traversal. The retrieval layer should:

```text
query
  -> hybrid keyword/vector seed hits
  -> exact slug/page lookup when query contains [[slug]]
  -> bounded graph expansion from seed slugs
  -> path/neighborhood scoring using direct-link, source-overlap, common-neighbor, and type-affinity signals
  -> cited context capsule with slugs, path hints, edge direction, and read handles
```

Traversal budgets:

- default graph depth is 1; depth 2 is allowed for exploratory synthesis,
- depth above 2 requires an explicit tool call or workflow budget,
- cap seed count, neighbors per seed, and total graph candidates,
- down-rank high-degree generic hubs unless the user asks for overview/navigation,
- preserve edge provenance: body link, related/backlink, source edge, or derived extraction edge where available.

RAG freshness is part of the retrieval contract.

Every retrieval index is a projection with explicit state, not a silent source of
truth. Aura must track at least:

```text
projection_id, corpus_layer, source_scope
status: fresh|stale|rebuilding|degraded|disabled
source_watermark, indexed_watermark
schema_hash, embedding_model_hash
last_success_at, last_error_at, last_error_kind
pending_jobs, documents_indexed, chunks_indexed
```

Projection rules:

- canonical stores remain the source of truth,
- GraphIndex is refreshed synchronously on wiki writes,
- FTS and Qdrant are rebuildable projections with durable reindex state,
- Qdrant upserts/deletes must be idempotent and op-aware,
- delete, rename, and embedding-model changes must invalidate affected projections,
- full rebuilds must be forceable and must not be skipped by startup warm-cache reuse.

Query rules:

- fresh projections participate normally in retrieval fusion,
- stale projections inside a bounded grace window may participate only with a freshness warning and lower trust weight,
- rebuilding projections may provide last-known-good hits only if the context capsule marks them stale,
- degraded or config-mismatched vector projections are excluded from fusion,
- exact, FTS, and GraphIndex fallback must remain available when vector retrieval is degraded,
- an empty result from a stale or degraded projection is never proof that evidence does not exist,
- retrieval capsules and tool observations include projection freshness.

Graph maintenance should also surface insight work, not only broken-state repair:

- surprising cross-community or cross-type connections worth reviewing,
- isolated pages and sparse communities that need links or synthesis,
- bridge nodes that deserve extra care because many knowledge paths depend on them,
- review proposals with evidence and suggested searches when the graph suggests a missing page or research gap.

Local-first GraphRAG is the target for the wiki layer.

Aura should implement the useful GraphRAG ideas without adding a graph database
to the core refactor:

```text
raw sources
  -> curated Markdown wiki
  -> GraphIndex + FTS/Qdrant projections
  -> community detection and reports
  -> cited retrieval capsules
```

Implementation rules:

- Markdown wiki pages remain the canonical graph nodes,
- GraphIndex owns ordinary low-latency traversal,
- FTS/Qdrant seed retrieval feeds graph expansion,
- graph ranking uses direct links, source overlap, common neighbors, type affinity, degree/hub penalties, and explicit traversal budgets,
- community detection runs offline or as a background maintenance job,
- community reports are derived projections with freshness state and evidence handles,
- useful community reports may be promoted into wiki `synthesis` pages only through review/proposal flow,
- Neo4j or another graph database is a future optional sidecar only, not a core dependency.

Traceability for this area lives in `docs/graphrag-local-first-reference-map.md`.

The agent tool surface should expose task-level graph tools rather than one polymorphic `graph(mode=...)` tool:

- `wiki_neighbors` for adjacent pages around a known slug,
- `wiki_path` for a shortest/explanatory path between known slugs,
- `wiki_graph_health` for hubs, orphans, broken refs, and index health.

These tools are for deliberate graph reasoning. Normal answers should use `recall_knowledge`, which performs bounded graph expansion internally and returns a compact cited capsule.

### 5.10 `internal/cache`

Cache is acceleration, not memory and not truth.

It owns:

- cache namespaces,
- cache keys,
- TTL and eviction policy,
- hit/miss/size metrics,
- persistent cache metadata,
- cache pruning,
- cache corruption handling,
- process-local hot cache adapters,
- durable cache backend adapters.

It must not own:

- run/event truth,
- workflow truth,
- outbox/inbox truth,
- identity grants,
- question state,
- memory semantics,
- delivery status,
- side-effect state.

The key rule:

> A cache can disappear without changing what Aura knows to be true.

Default cache architecture:

```text
L1 process cache      bounded, disposable, cost-aware in-memory cache
L2 persistent cache   dedicated SQLite cache database
large payload cache   content-addressed files plus SQLite metadata
```

Cache policy:

- cache is never authoritative,
- cache writes are best-effort and must not roll back canonical writes,
- persistent cache lives outside the canonical run/event database,
- cache keys include namespace, content hash, producer version, model/provider identity, output dimension where relevant, schema version, codec version, and redaction class,
- embeddings, OCR/extract outputs, rendered prompt/tool blocks, tool schemas, and expensive retrieval intermediates are valid cache domains,
- runs, events, workflow steps, outbox/inbox, questions, approvals, identity grants, memory write authority, delivery status, and side-effect state are not cache domains,
- cache deletion must be a supported recovery operation,
- Badger, bbolt, Valkey, or Redis are backend options only after measured contention or scale requires them.

The existing `embedding_cache` table is a compatibility bridge. The target shape is a cache plane that can move deterministic caches into a separate `cache.db` or equivalent cache namespace without changing agent semantics.

### 5.11 `internal/storage`

Storage is persistence infrastructure.

It owns:

- SQLite access,
- Qdrant access,
- artifact/blob storage,
- source files,
- conversion outputs,
- indexes,
- backup-related storage clients.

It must not own:

- agent loop decisions,
- user-facing memory semantics,
- chat routing,
- channel behavior.

Storage is allowed to be subdivided. A `storage` namespace is good; a `storage` god package is bad.

Persistence implementation policy:

- core storage stays on explicit `database/sql` repositories and versioned SQL migrations,
- do not adopt GORM as the central ORM for Aura,
- do not use auto-migration for durable core tables,
- run/event, outbox, identity grants, FTS/RAG, recovery, backups, and migrations must keep explicit SQL,
- ORM-style helpers may be considered only for isolated, non-critical CRUD after a spike proves they do not weaken migrations, transactions, logging, or testability.

### 5.12 `internal/cron`

Cron is a scheduled entrypoint.

It owns:

- recurring jobs,
- due-work lookup,
- schedule persistence,
- schedule-fire materialization,
- missed-run and catch-up policy,
- overlap policy,
- delegated background actor creation.

It must not own:

- a separate agent loop,
- separate tool execution rules,
- direct memory or wiki writes,
- direct channel delivery,
- special hidden chat behavior,
- long-running side effects.

Cron submits work into `chat`, `agent`, or `workflow` through the same contracts used by other entrypoints.

The core contract is:

```text
Schedule due
-> durable ScheduleFire
-> delegated Actor
-> RunRequest or WorkflowStep
-> RunEvents + RunOutbox
```

A schedule row describes future intent. A schedule-fire row describes one scheduled occurrence.

Minimum schedule-fire fields:

- `schedule_id`,
- `schedule_version`,
- `scheduled_at`,
- `detected_at`,
- `fire_id`,
- `idempotency_key`,
- `owner_principal_id`,
- `delegated_actor_id`,
- `delivery_mode`,
- `capability_grant_snapshot`,
- `run_id` or `workflow_id`,
- `status`,
- `attempt_count`,
- `last_error`.

The default idempotency key is:

```text
cron:{schedule_id}:{schedule_version}:{scheduled_at}
```

Default policies:

- recurring schedules coalesce missed downtime to the latest due fire inside the catch-up window,
- one-shot schedules fire once if still inside their grace window, otherwise they become `missed`,
- overlap defaults to `forbid_per_schedule`,
- parallel fires require explicit idempotent read-only policy, max concurrency, and budget,
- retryable failures reuse the same `fire_id` and idempotency key,
- side-effect-unknown failures reconcile before retry,
- cancellation preserves schedule and run history,
- notifications go through `run_outbox`, never direct cron sends.

Scheduled work must run as a delegated actor with explicit capabilities, expiry, tool allowlist, budget, and notification policy. A cron job is never implicitly the owner.

Traceability for this area lives in `docs/cron-background-run-reference-map.md`.

### 5.13 `internal/api`

API is an external surface.

It owns:

- HTTP routes,
- request/response DTOs,
- dashboard endpoints,
- bearer/session authentication middleware,
- setup/health surfaces.

It must not own:

- core loop logic,
- Telegram behavior,
- tool internals,
- wiki mutation policy.

API must not own the permission model. API handlers authenticate a request into an actor, then ask `identity` whether that actor can perform the requested capability.

`/api/chat` must keep the public JSON shape stable while its backend migrates.

---

## 6. Rename Philosophy

Renaming is part of the refactor, but only when the new name teaches the system.

Good rename:

- makes responsibility obvious,
- removes a false abstraction,
- makes imports easier to reason about,
- supports the target dependency direction.

Bad rename:

- only changes labels,
- hides a god package under a better name,
- moves transport behavior into core,
- makes the next worker read more files to understand the same thing.

Every rename must answer:

> Is this code core, traffic, identity, channel, capability, memory, storage, cron, API, or config?

If the answer is unclear, the module is not ready to move.

---

## 6.5. Status Checklist

Snapshot at **2026-05-16**. The inline closure notes inside each phase (§7) are the
canonical evidence; this table is the at-a-glance overview. Three states:

- ✅ **closed** — gate met, in production, commits on master
- 🟡 **in-flight** — Ralph queue active or planning underway
- ⬜ **open** — scaffolded only, not started

### 7.1 Phase status

| Phase | Status | Closed | Commits / Stories | Notes |
| --- | --- | --- | --- | --- |
| **1** — Stabilize the Map | ✅ | early 2026-05 | dir merge 49→22 | Validated by §4 module map being the live shape; no import cycles |
| **1A** — Persist Run/Event Foundation | ✅ | early 2026-05 | `runs`, `run_events`, `run_outbox` tables in `internal/db/migrations` | mig v1-v3 |
| **1B** — Identity + Capability Grants | ✅ | early 2026-05 | `principals`, `channel_accounts`, `actors`, `grants` tables | mig v6-v7; `internal/api/auth` + `internal/identity` wired |
| **1C** — Question Gate | ✅ | 2026-05-16 | `chat_questions` mig v11 + `internal/storage/runs/questions.go` + Telegram resume wiring | commit `1584a8fa` |
| **2** — Protect Telegram | ✅ | 2026-05-15 | record/replay fixture harness | `internal/channels/telegram/fixture` |
| **3** — Move Channels Behind Chat | ✅ | 2026-05-15 (Telegram-streaming arc) | `internal/channels/{telegram,web,silent,cron}` + Hub adapter | post-Wave 3.0 chathub merge |
| **4** — Collapse Agent Runtime | ✅ | 2026-05-15 (Runner-removal arc) | US-G01..G07 — `agent.Runner` deleted, `agent.Run` unico | |
| **5** — Consolidate Tools | ✅ | 2026-05-15 | US-I01..I05 — commits `009639ae..28ae9324` | tools/registry consolidation |
| **6** — Tool Experience Loop | ✅ closed (full scope) | 2026-05-16 | US-J01..J06 — commits `73ddea04..fa7d4559`; US-N01..N04 — Phase-N lesson promotion | `tool_attempts` + ToolObservation + retry budgets + `internal/learning` PromoteLessons + WriteApprovedLesson + recall_operational tool |
| **7A** — Compact Archive Hygiene | ✅ | 2026-05-16 | US-K01..K05 — `9d74809d..3a777e36` | role=tool excluded from compact archive |
| **7B** — Typed Collection Registry | ✅ | 2026-05-16 | US-L01..L06 — `1a6a609a..13b66d7e` | Collection enum + score components + follow-up handles + SourceID filter |
| **7C** — Projection Freshness Registry | 🟡 in-flight | — | Phase-M queue: US-M01 shipped, US-M02..M04 queued | Ralph background; `projection_state` table + content_hash drift + degraded_read annotations |
| **7D** — User/Operational Memory typed tiers | ⬜ | — | scaffolded enum in `collections.go` US-L01 | wire `KindUserMemory` + `KindOperational` as first-class writers (Mem0/Letta typed-tier pattern) |
| **7E** — Source span/byte offsets | ⬜ | — | — | per audit `docs/phase07b-current-types-audit-2026-05-16.md §G.2.3` |
| **7F** — Wiki frontmatter schema/prompt-version promotion | ⬜ | — | — | per audit `§G.2.4` |
| **Phase-O** — User Memory Promotion (sub-phase of 9) | ✅ | 2026-05-16 | US-O01..O04 — `2642ce0b..`(US-O04) | wired KindUserMemory: triage → proposed_updates kind=user_memory → WriteApprovedUserFact → recall_user_memory tool |
| **Phase-P** — Agent Note Scratchpad (capability #4) | ✅ | 2026-05-16 | US-P01..P04 — `79159b4a..`(US-P04) | agent_note scratchpad wired (capability #4 closed): SQLite table + Store API + action-dispatch tool + system-prompt injection + GC + web-path fix + probe |
| **Phase-Q** — User Memory Write Guards (Phase 9 partial close) | ✅ | 2026-05-16 | US-Q01..Q04 — `c0189ac4..`(US-Q04) | user_memory write guards wired (Phase 9 partial close — capability check + question gate): memory.user.write Authorize gate at WriteApprovedUserFact + ambiguity question gate Score<0.7 + integration tests |
| **Phase-R** — Subagent Dispatch Primitive (Phase 8 read-only fanout first slice) | ✅ | 2026-05-16 | US-R01..R04 — `b4124b54..`(US-R04) | subagent dispatch primitive wired (Phase 8 read-only fanout — capability #5 closed): NodeSpec + HubBridge.Dispatch + WaitForRun + subagent_dispatch LLM tool + E2E integration test |
| **Phase-S** — Write-capable Workers via propose_patch (Phase 8 second slice) | ✅ | 2026-05-16 | US-S01..S04 — `64b6623d..`(US-S04) | write-capable workers wired (Phase 8 second slice): propose_patch tool (3 actions, idempotency, provenance) + RiskTier=write_proposal enforcement + subagent_dispatch risk_tier param + E2E integration test + SweepStaleProposals TTL cron |
| **8** — Autonomous Durable Work Runtime (Cron + Swarm RunGraph) | 🟡 second-slice closed | 2026-05-16 (Phase-S) | Phase08 first slice (Phase-R) + second slice (Phase-S) closed 2026-05-16; full Phase 8 (team_collaboration, plan-execute, critic-review, hierarchical/hybrid DAG) remains deferred | **Capability #5 extended by Phase-S.** write_proposal workers via propose_patch wired. Full Phase 8 substrate (8B–8G) gated on open decisions. |
| **9** — Memory and Source Discipline | 🟡 partial-close | 2026-05-16 (partial) | Phase-O + Phase-Q closed (write-policy + guards); remaining: clarify memory vs storage docs, conversion fixtures, SQLite concurrency hardening | **Phase 9 partial-close 2026-05-16** — capability #1 (autonomous write-policy) core wired; doc/invariant hardening deferred |
| **10** — Single Source of Truth Config | ✅ | 2026-05-15 | US-H01..H06 | SQLite-backed secrets, setup wizard rewrite, `.env` legacy reference only |

### 7.2 Outstanding deferrals (lesson promotion + memory active loop)

Three deferrals from closed phases that together implement what the 2026-05-16
brainstorm called "automiglioramento + memoria industry-level". Each maps to a
specific package or §5 contract:

| Deferral | Source phase | Owning package (§5) | Maps to capability |
| --- | --- | --- | --- |
| ~~**Lesson promotion**~~ (`experience_store → operational_memory → skills`) | Phase 6 (§5.8 line 776-779) | `internal/learning` | End-of-turn reflection + telemetry-driven self-improvement — **CLOSED 2026-05-16** (US-N01..N04) |
| ~~**Active write-policy**~~ **CLOSED 2026-05-16** (extraction+routing+approval+retrieval via Phase-O; capability check + ambiguity question gate via Phase-Q — both US-Q01..Q03 shipped) | Phase 9 (§5.7 lines 712-725) | `internal/learning` + `internal/memory` | Autonomous write-policy |
| ~~**`agent_note` scratchpad + pinned core block**~~ | §5.7 line 678 (`agent_working_memory`) | `internal/memory` runtime continuity | TodoWrite cross-turn checklist — **CLOSED 2026-05-16** (US-P01..P04) |

Together these three close gaps **1 (autonomous write)**, **2 (end-of-turn reflection)**,
**3 (telemetry-driven improvement)**, and **4 (cross-turn checklist)** from the
"Aura come Claude Code" brainstorm. Gap **5 (dynamic subagent dispatch)** — **CLOSED
2026-05-16** by Phase-R (US-R01..R04): read-only fanout subagent dispatch primitive
wired (NodeSpec + HubBridge.Dispatch + WaitForRun + `subagent_dispatch` LLM tool).
Full Phase 8 remaining scope (write-capable workers, team_collaboration, plan-execute,
critic-review, hierarchical DAG) remains explicitly deferred. Gap **6 (self-coding)**
is an explicit non-goal (§12).

### 7.3 Logical next phase after Phase-M (Phase 7C) closes

The high-leverage candidate is the **Phase 6 lesson-promotion deferral**, naming it
**Phase-N** in the Ralph queue convention. Rationale:

1. `tool_attempts` table + ToolObservation contract are already shipped (Phase 6 backbone)
2. Combines two of the four open capability gaps (reflection + telemetry) into one slice
3. Unlocks the §5.7 "promotion workflow": `experience_store → operational_memory → skills`
4. Manageable size (5-7 atomic stories) because foundations exist
5. Validates `internal/learning` as a real package before Phase 9 promotes write-policy further

After Phase-N, the natural progression is Phase 9 (write-policy hardening) → Phase 8
(RunGraph subagent dispatch) → Phase 7D-F (typed memory tiers + frontmatter promotion).

---

## 7. Migration Strategy

The refactor uses a strangler approach.

Do not stop the world and rewrite all of Aura. Create the clearer architecture and move one path at a time behind tests.

### Phase 1 - Stabilize the Map

Goal: make the package names reflect the target architecture.

Current queue direction:

- health/setup/dashboard auth surfaces belong under `api`,
- principal, channel-account, actor, grant, and authorization logic belong under `identity`,
- qdrant/search/reindex/memoryindex/memoryquality/sources belong under `storage`,
- chathub becomes `chat`,
- adapters become `channels`,
- scheduler becomes `cron`,
- tool-related packages move under `agent/tools`.

Gate:

- no import cycles,
- build/vet/test green,
- no god package created,
- package name explains responsibility.

### Phase 1A - Persist the Run/Event Foundation

Goal: make `chat` durable before more channels depend on it.

Steps:

- add SQLite migrations for `runs`, `run_events`, and `run_outbox`,
- persist `run_started` and terminal events before adapter delivery becomes more complex,
- update `runs` snapshot in the same SQLite transaction as durable events,
- make event sequence monotonic per run,
- add idempotency keys for inbound messages, tool calls, delivery attempts, and child-run dispatch,
- keep payloads redacted and schema-versioned,
- define the trace metadata schema separately from payload artifacts,
- carry `run_id`, `parent_run_id`, `correlation_id`, and exporter trace/span
  identifiers where available,
- persist tool names, call ids, argument keys, status, timing, usage, and error
  class without raw argument values by default,
- add audit event types for memory writes, settings, grants, skills, exports,
  purges, and privileged payload reads.

Gate:

- duplicate inbound delivery with the same idempotency key does not create a second run,
- events replay into the same run snapshot,
- terminal run state survives process restart,
- failed outbound delivery remains retryable through outbox state,
- tests cover per-run ordering, cancellation, and parent/child correlation,
- logs can be correlated to run events without being required to reconstruct a
  run,
- trace payload access has a redaction policy and an audited path.

### Phase 1B - Establish Identity and Capability Grants

Goal: give every run, tool call, cron job, and swarm child a single authority model.

Steps:

- add durable principal, channel account, actor, capability grant, and authorization decision tables,
- migrate Telegram allowlisted user IDs into `channel_accounts` plus owner/user principals,
- make dashboard tokens authenticate a session actor instead of acting as permissions,
- add an `Authorize(actor, capability, resource)` boundary,
- map existing tool allowlists into capability checks without removing allowlists yet,
- add delegated actors for cron and child swarm runs,
- record authorization denials as run events or audit decisions.

Gate:

- Telegram, web/API, cron, and swarm test fixtures all resolve an actor,
- a dashboard token cannot bypass a missing capability,
- a visible tool still fails closed when the actor lacks its required capability,
- a child swarm actor cannot receive capabilities its parent lacks,
- revoked grants stop future runs without rewriting historical events.

### Phase 1C - Add the Question Gate

Goal: support clarification and approval without making the agent ask instead of think.

Steps:

- add durable `chat_questions` or equivalent question snapshot state linked to `run_events`,
- model question events as `question_requested`, `question_answered`, `approval_requested`, and `approval_answered` or a shared typed variant,
- implement `QuestionGate` before risky tool execution and before durable memory writes,
- define `request_input` as a narrow structured action, not a broad always-loaded tool,
- require `why_blocking`, `blocking_scope`, answer schema, expiry, fallback, and producer metadata,
- route channel button/free-text replies back into the same question id,
- treat late, duplicate, or wrong-channel answers as explicit states, not loose chat text.

Gate:

- a clear instruction does not produce a needless question,
- missing required slots produce one scoped question,
- risky or irreversible tool calls produce approval rather than a generic clarification,
- the answer resumes the same run or a continuation run with parent correlation,
- repeated unnecessary question attempts are counted by evals,
- question state survives process restart.

### Phase 2 - Protect Telegram

Goal: create a fixture before moving Telegram behavior.

Telegram is high risk because it contains subtle product behavior:

- progressive edit throttling,
- CoT marker rendering,
- entity rendering,
- fallback behavior,
- tool progress display.

Gate:

- record-and-replay fixture exists,
- fixture covers simple reply, CoT, tool/entity table,
- later adapter output can be byte-compared against the fixture.

**Phase 2 closed 2026-05-15** — Telegram record/replay fixture protection is
in place, including fallback entity-edit-to-plain-text behavior.

### Phase 3 - Move Channels Behind Chat

Goal: make `chat` the normalized traffic layer.

Steps:

- port Telegram outbound into `channels/telegram`,
- route web chat through `chat` behind a conservative flag,
- keep `/api/chat` JSON stable,
- later route Telegram through hub behind a flag.

Gate:

- fixture diff zero,
- `/api/chat` shape unchanged,
- default behavior conservative until soak.

**Phase 3 closed for the Telegram-streaming arc 2026-05-15** — Telegram
streaming routes through `channels/telegram.Outbound` with fixture byte parity.
The later web `/api/chat` Hub migration closed during Phase01B/Phase01C repair
work.

### Phase 4 - Collapse the Agent Runtime

Goal: one loop, one runtime path.

Current reality:

- the legacy `agent.Runner` type has been removed,
- production references now call stateless `agent.RunTask`,
- current source search finds no production `*agent.Runner` reference.

Target:

- governance extracted,
- prompt/context assembly extracted into a versioned agent-owned bundle,
- `agentloop` and `agentruntime` merged into `agent`,
- production paths call the canonical runtime,
- compatibility wrapper removed only when no longer needed.

Gate:

- no duplicate loop body,
- prompt bundle snapshots are deterministic,
- prompt evals cover context utilization, tool triggering, question behavior, and output contract adherence,
- no production-only inferior path,
- agent/chat/swarm/cron tests green,
- loop behavior verified by tests, not comments.

**Phase 4 closed for the Runner-removal arc 2026-05-15** — US-G01..US-G07
shipped `RunTask`, removed `runner.go`/`runner_test.go`, and refreshed agent
docs. Prompt snapshot/eval hardening remains a future selected slice if needed.

### Phase 5 - Consolidate Tools

Goal: tools become agent capabilities, not random global services.

Steps:

- move registry/toolindex/toolsets/swarmtools under `agent/tools`,
- preserve subpackage boundaries,
- stabilize tool order,
- enforce typed schemas and structured errors,
- assign every tool a visibility tier,
- assign every tool a required capability,
- assign every tool a retry/idempotency/risk class,
- keep only the smallest control surface always loaded,
- make deferred discovery the default for the rest,
- harden programmatic tool orchestration through explicit allowlists,
- add curated examples to complex/action-enum/nested-schema tools,
- preserve secret-safe logging.

Gate:

- deterministic tool list,
- deterministic tool pool order before provider calls,
- tool discovery top-k evals pass,
- parameter accuracy evals pass for example-backed tools,
- programmatic internal calls are bounded, redacted, audited, and active-turn-only,
- active-turn visibility never bypasses authorization,
- no secret value logging,
- no broad path/URI access,
- tests/probes inspect tool behavior.

**Phase 5 closed 2026-05-15** — all gate criteria met (US-I01..I05, commits 009639ae..28ae9324).

### Phase 6 - Add the Tool Experience Loop

Goal: Aura improves from preventable tool-call failures instead of repeating them.

Steps:

- define `ToolObservation` as the single result/error contract for inline and workflow-backed tools,
- classify tool results as ok, recoverable error, blocked error, fatal error, or cancelled,
- add a Tool Supervisor that enforces retry policy, redaction, idempotency, and retry budgets,
- inject recoverable error feedback into the same run,
- cap retries by run, tool, and error kind, and record why a retry was attempted or refused,
- route stateful, long-running, side-effecting, background, cron, outbound, source-ingest, memory-write, wiki-mutation, and swarm-spawn tools through durable workflow execution,
- require idempotency keys for every retryable side-effecting operation,
- require reconcile-first behavior for `side_effect_unknown`,
- persist tool attempts and outcomes as learning events,
- retrieve validated lessons for similar future tool calls,
- promote repeated lessons into memory, skills, or tool policy only after validation.

Gate:

- a recoverable tool error can be corrected in the same run,
- repeat failures are visible by tool and error kind,
- workflow-backed tool steps survive process restart and do not double-apply side effects,
- `side_effect_unknown` is never blindly retried,
- secrets and raw sensitive args are redacted from learning records,
- retrieved lessons are versioned against tool schema/version,
- no automatic prompt/code mutation happens without validation.

**Phase 6 closed 2026-05-16 (in-scope slice — durable workflow + idempotency + reconcile-first + lesson promotion deferred to Phase-K)** — US-J01..J06 shipped (commits 73ddea04..fa7d4559).

### Phase 7 - Rebuild RAG On Typed Memory Layers

**Phase 7A closed 2026-05-16 (compact archive hygiene — role=tool exclusion from compact_memory_documents). Phase 7B-F remain scaffolded.** US-K01..K04 shipped (commits 9d74809d, cfda6bee, 43504082, 7ebbf083, 770eed0a).

**Phase 7B closed 2026-05-16 (typed collection registry — typed enum + score components + follow-up handles + SourceID filter). Phase 7C-F remain scaffolded.** US-L01..L05 shipped (commits 1a6a609a, 92e446fb, ca6a86e3, bb0ed864, 508b32a1). Deferrals to 7C/7D: freshness registry (G.2.1), user/operational memory (G.2.2), span offsets (G.2.3), frontmatter promotion (G.2.4).

Goal: retrieval stops being one broad memory soup.

Steps:

- define memory layer IDs and citation handles,
- create a collection metadata registry for wiki, sources, user memory, archive, and operational memory,
- split recall behavior by task intent instead of one polymorphic mode parameter,
- implement hybrid FTS/vector retrieval with RRF fusion where available,
- preserve chunk-to-parent source expansion,
- add a projection freshness registry for FTS, Qdrant, graph documents, and embedding caches,
- make wiki/source/user-memory reindex jobs durable, idempotent, op-aware, and watermark-based,
- implement true per-slug wiki upsert/delete reindex plus a separate force full-rebuild command,
- extend GraphIndex with typed weighted edges, source edges, degree, and bounded neighborhood/path queries,
- add local community detection and graph insight jobs for wiki graph maintenance,
- generate community reports as derived projections with citation/evidence handles,
- use community reports as hints for global sensemaking, not as hidden source of truth,
- return structured retrieval hits with score components and follow-up handles,
- return projection freshness and degraded-read warnings with retrieval hits,
- make retrieval errors recoverable learning events,
- add golden RAG evals for user facts, wiki/source answers, operational lessons, stale vectors, deletes, renames, and embedding-model changes.

Gate:

- user facts do not appear in wiki unless intentionally promoted,
- tool failures do not appear in wiki,
- source hits cite source/page/span or stable artifact handle,
- wiki hits cite `[[slug]]`,
- retrieval fixtures prove hybrid beats vector-only and keyword-only on the golden set,
- stale/degraded projections are visible to the agent and dashboard,
- stale vector-only empty results cannot suppress exact, FTS, or graph evidence,
- delete and rename fixtures prove stale projection records are removed or marked invalid,
- wiki GraphRAG evals cover local entity questions and global sensemaking questions separately,
- community reports carry evidence handles and freshness state,
- Neo4j or another graph database is not required for Phase 7,
- repeated bad filters/searches produce self-healing feedback.

### Phase 8 - Autonomous Durable Work Runtime (Cron + Swarm RunGraph)

Goal: Aura stops waiting for manual commands as the normal operating mode.
Background work, scheduled work, child-agent work, maintenance, and future team
collaboration enter one durable runtime that can plan, run, delegate, recover,
and ask for human input only when policy requires it.

Manual controls remain for bootstrap, debug, and explicit operator override.
They are not the architecture and cannot be used as the only proof that Aura is
autonomous.

Planning status as of 2026-05-16: the Phase08 planning folder is
`ready-for-discussion` after independent re-verification. It is not
`ready-for-implementation` until the open decisions are accepted or deliberately
changed and the first bounded implementation slice is locked. Phase07C remains
owned by the Ralph loop in Claude Code; Phase08 consumes context capsule handles
and does not implement retrieval freshness.

Reference maps:

- `docs/cron-background-run-reference-map.md`
- `docs/agent-parallel-loop-2026-reference-map.md`
- `docs/observability-audit-retention-reference-map.md`
- `.planning/deep-refactor/Phase08_Cron_And_Swarm_RunGraph/{source.md,plan.md,benchmark.md,progress.md}`

Operating contract:

```text
trigger or work intent
  -> policy decision
  -> durable ScheduleFire, RunGraph, or WorkflowStep
  -> delegated Actor with bounded capabilities
  -> normalized run/workflow/outbox execution
  -> metadata-first trace and audit evidence
  -> retry, reconcile, escalate, or complete
```

Autonomy means:

- Aura can detect due or useful work without a user clicking `run_now`,
- Aura materializes work before execution, using SQLite rows as ground truth,
- Aura applies risk, budget, authority, and approval policy before delegation,
- Aura can split work into child nodes or a team when topology justifies it,
- Aura records enough trace evidence to resume, replay, debug, and improve,
- Aura asks the user only for high-risk, ambiguous, permission-lacking, or
  irreversible actions.

Cron:

- detects due schedules,
- creates durable schedule-fire records with idempotency keys,
- records schedule version, source/manual marker, delegated actor, grant
  snapshot, delivery mode, run/workflow link, attempts, and terminal status,
- submits each fire as a delegated `RunRequest` or workflow-backed step,
- applies explicit missed-run, catch-up, overlap, retry, and cancellation policy,
- routes reminder, agent job, wiki maintenance, source-watch, and silent jobs through the same contracts,
- does not own a private loop, private tool runner, or direct channel delivery.

Swarm:

- uses parent/child run IDs,
- dispatches child work through the chat/hub or equivalent normalized run path,
- models swarm as a policy-driven run graph, not a fixed worker list,
- supports topology tiers over time: direct, read-only fanout, team collaboration, plan-execute, critic-review, artifact-build, repair-loop, hierarchical, and hybrid DAG execution,
- defines each child as a bounded `NodeSpec` with goal, instruction, immutable curated context capsule reference, tool/capability grant snapshot, model/provider when relevant, budgets, output schema, artifact policy, risk tier, and allowed spawn depth,
- defines graph `EdgeSpec` records for dependency, artifact consumption, aggregation, critic/review, reroute, and cancellation relationships,
- models agent teams as lead-managed `RunGraph` instances with a shared task board and durable mailbox,
- lets named teammates message each other directly when the topology requires debate, handoff, challenge, or coordination,
- represents broadcast as separate addressed messages, not invisible shared context,
- uses task states, dependencies, assignment, self-claim, claim locking, plan approval, and quality hooks to keep team work coordinated,
- caps subagent outputs and returns structured observations, citations, confidence, and artifact handles instead of raw transcripts,
- treats read-only fanout as a safe first slice, not the permanent architecture ceiling,
- allows future write-capable workers only through proposal or workflow gates,
- persists orchestration traces for spawn, delegate, task create/claim/complete, mailbox message, message/workspace update, tool call, return, aggregate, plan approval, stop, retry, cancellation, and budget events,
- evaluates swarm by critical path, task quality, useful-agent utilization, protocol overhead, useful-message ratio, blocked-task latency, error amplification, cost, and trace debuggability rather than number of agents.

Implementation order:

- **8A Planning and decision lock**: source-backed plan, benchmark contract, PRD
  coverage matrix, independent verifier; done as a discussion-ready plan on
  2026-05-16.
- **8B Durable work substrate**: additive storage/contracts for `RunGraph`,
  nodes, edges, context capsule refs, task board, mailbox, plan approvals, and
  metadata-first trace events; no production dispatch change.
- **8C Swarm child runs through normalized runtime**: production swarm work
  creates canonical child `runs` or workflow-backed steps and cannot complete
  only inside `swarm.Manager`.
- **8D ScheduleFire store and policy**: materialize due work before execution
  with `scheduled_task_fires`, schedule version, idempotency, missed/coalesced
  policy, overlap defaults, retry, and cancellation state.
- **8E Cron execution migration**: move `agent_job`, `run_now`, reminders, wiki
  maintenance, source-watch, and silent jobs behind fire/run/workflow/outbox
  semantics while preserving current user-visible behavior until benchmarks pass.
- **8F Team collaboration runtime**: named teammates, race-safe task claiming,
  durable addressed mailbox, plan approval gates, budget limits, and team trace
  events.
- **8G Autonomous runtime E2E and observability**: prove non-manual due/policy
  work, policy approvals, restart safety, trace replay, and background-job
  observability from SQLite rows.

Open decisions before implementation:

- graph root identity: default to a canonical supervisor/root `runs` row for
  autonomous or swarm graph work,
- trace storage: default to `run_events` as the canonical metadata timeline,
  with graph/task/mailbox tables for query shape,
- cron fire storage: default to dedicated `scheduled_task_fires` beside
  `scheduled_tasks`,
- `NodeSpec.curated_context`: default to immutable capsule reference and
  metadata, not inline source bytes or Phase07C retrieval-owned state,
- first implementation slice: default to Phase08B durable work substrate,
- `run_now` response: preserve synchronous semantics initially by waiting for
  terminal fire/run state,
- reminder migration: do not remove direct reminder delivery until
  `run_outbox` delivery is wired and benchmarked,
- approval policy: low risk may auto-approve; medium risk uses verifier/critic
  policy; high risk and destructive/write actions use durable user approval.

Gate:

- parent run ID propagation tested,
- child actor grants cannot exceed parent authority,
- child context capsule reference, tool grants, budgets, output schema, and artifact policy are persisted for every swarm node,
- run graph edges are visible enough to replay or debug aggregation,
- team task board and mailbox are persisted, replayable, and visible in the run timeline,
- task claiming is race-safe and tested under concurrent claim attempts,
- teammate-to-teammate messages are addressed, scoped, redacted, and auditable,
- plan approval can hold risky teammates in read-only mode until approved,
- fixed roles, read-only-only workers, and `max_spawn_depth=1` are not encoded as permanent architectural limits,
- cron fire rows link to run IDs or workflow IDs,
- missed, coalesced, skipped, retried, cancelled, and failed fires are observable,
- recurring downtime coalesces by policy and cannot burst unbounded work,
- one-shot stale jobs have a tested grace/missed policy,
- reminder delivery is produced through outbox,
- scheduled agent jobs run as delegated actors with explicit capabilities,
- source-watch, wiki maintenance, silent jobs, and generic background jobs route through the same fire/run/workflow contracts,
- background jobs observable,
- swarm traces expose critical-path latency, useful-agent utilization, protocol overhead, and error amplification,
- autonomous E2E proof rejects manual-only `run_now` evidence and requires
  non-manual `fire_source` plus run/workflow/outbox or child-node SQL evidence,
- no hidden alternate runtime.

**Phase 8 first-slice closed 2026-05-16 (read-only fanout subagent dispatch).** Phase-R
(US-R01..R04) wired the read-only fanout primitive: NodeSpec + HubBridge.Dispatch +
WaitForRun + `subagent_dispatch` LLM tool. Capability #5 from the "Aura come Claude
Code" brainstorm is now closed. Full Phase 8 remaining scope (8B durable substrate,
8C swarm normalization, 8D–8E cron migration, 8F team collaboration, 8G autonomous
E2E) remains explicitly deferred.

**Phase 8 second slice closed 2026-05-16 (write_proposal workers via propose_patch + TTL sweep).** Phase-S
(US-S01..S04) extended the fanout primitive with `RiskTier='write_proposal'`: `propose_patch` tool
(wiki / user_memory / operational actions, sha256[:16] idempotency, provenance), NodeSpec allowlist
enforcement via `DirectWriteToolNames`, `subagent_dispatch` optional `risk_tier` per node, E2E
integration test, and `SweepStaleProposals` TTL cron (daily 03:00, 30-day pending-only purge).
Full Phase 8 (team_collaboration, plan-execute, critic-review, hierarchical/hybrid DAG) remains deferred.

### Phase 9 - Memory and Source Discipline

Goal: protect the second-brain product semantics after the core is clean.

Steps:

- clarify `memory` versus `storage`,
- keep Markdown wiki as readable projection,
- keep raw sources immutable,
- make indexes rebuildable,
- add conversion fixtures for important source types,
- harden SQLite concurrency.

Gate:

- wiki output inspected,
- source conversion fixtures use must-include/must-not-include checks,
- SQLite WAL/busy-timeout/retry behavior verified per connection,
- Qdrant/search treated as projections.

**Phase 9 partial-close 2026-05-16** — preference/fact/person/todo extraction (Phase-O:
US-O01..O04) + `memory.user.write` capability check + ambiguity question gate (Phase-Q:
US-Q01..Q03) shipped. Capability #1 (autonomous write-policy) core is wired. Remaining
Phase 9 scope: clarify memory vs storage docs, conversion fixtures for important source
types, harden SQLite concurrency — documentation and invariant hardening, deferred to a
future closure pass.

### Phase 10 - Single Source of Truth Config

Goal: make SQLite the only place an operator (or the bot) reads or writes
configuration. Eliminate `.env` as a runtime input. The dashboard becomes the
sole config UX; first-run setup writes directly to SQLite. Existing installs
migrate transparently on first post-upgrade boot.

Steps:

- add a `secrets` table (separate from `settings` to mark the privacy
  boundary) for TELEGRAM_TOKEN, LLM_API_KEY, EMBEDDING_API_KEY, GARAGE_S3_*,
- hardcode the bootstrap meta-config (DB_PATH, HTTP_PORT, AURA_HEADLESS,
  AURA_TIMEZONE) with env override fallback,
- rewrite `internal/setup/` first-run wizard to write SQLite,
- one-shot migration helper: import existing `.env` values into SQLite on
  first post-upgrade boot,
- drop `env_file:` from `compose.yaml`; replace with a `data/` volume mount,
- update INSTALL.md, README, and `.env.example` (or delete the latter).

Gate:

- fresh install boots without `.env` (wizard reachable at HTTP_PORT default),
- wizard persists secrets to SQLite, never writes `.env`,
- existing install with `.env` migrates automatically on first boot; logs
  list imported keys; secret values never logged,
- bootstrap env vars (DB_PATH, HTTP_PORT) still respected when set,
- docker compose boots without `env_file:` and reaches the wizard.

**Phase 10 closed 2026-05-15** — US-H01..US-H06 shipped SQLite-backed secrets,
setup persistence to SQLite, one-shot `.env` import, bootstrap defaults, and
compose/install cleanup with no `env_file:` directives.

Non-goals: no encryption-at-rest in this phase (filesystem permissions +
full-disk encryption documented in INSTALL.md); no new setup-UX invention
(reuse the existing wizard, only redirect its persistence target).

---

## 8. Derived Queue Policy

The canonical deep-refactor route is this PRD plus
`.planning/aura-deep-refactor-decisions.json`. The executable state is
`.planning/deep-refactor/INDEX.md`, the current handoff files, and the selected
phase or sub-phase `source.md`, `plan.md`, `benchmark.md`, and `progress.md`.

`scripts/ralph/prd.json`, GSD artifacts, old wave plans, and audit plans are
derived queues or evidence. They are valid only after being checked against the
canonical PRD/ADR/phase files. If a queue conflicts with the PRD, current
handoff, or phase progress, update or re-author the queue before execution.

Current execution state on 2026-05-16:

- Phase01A, Phase01B, and Phase01C are closed for their implemented
  foundation/identity/question-gate scopes.
- Phase01C was pushed as `ecb4cf3e fix(chat): close Phase01C question gate`;
  GitHub Actions CI run `25958870299` passed.
- Phase02, Phase03 Telegram-streaming arc, Phase04 Runner-removal arc, Phase05,
  Phase06 in-scope slice, Phase07A, Phase07B, and Phase10 have closure evidence
  in their phase folders.
- Phase07C-F, Phase08, and Phase09 remain planned/scaffolded. Select one and
  promote its `source.md`, `plan.md`, and `benchmark.md` before implementation.

---

## 9. Dependency Rules

These rules define architectural progress.

Allowed:

- `channels/* -> chat`
- `channels/* -> agent` only through narrow interfaces if unavoidable
- `chat -> agent`
- `agent -> agent/tools`
- `agent -> learning` through interfaces
- `agent -> rag` through interfaces
- `agent -> memory` through interfaces
- `rag -> memory` through interfaces
- `rag -> learning` through interfaces
- `rag -> storage` through interfaces
- `learning -> storage`
- `memory -> storage`
- `api -> chat`
- `cron -> chat` or `agent`

Forbidden:

- `agent -> channels/*`
- `agent -> api`
- `agent -> telegram`
- `agent -> concrete qdrant/sqlite/source parser details`
- `memory -> channels/*`
- `storage -> agent`
- `rag -> channels/*`
- `rag` writing durable memory directly
- `tools -> chat` unless the tool is explicitly a chat-facing capability and reviewed
- `learning -> channels/*`
- `learning` auto-editing prompts, code, or skills without validation
- programmatic tool orchestration calling destructive, recursive, credential-minting, or inactive-turn tools
- `cron` owning a separate loop
- `swarm` owning a separate loop

When a forbidden dependency exists, either introduce a small interface or move the code to the layer that actually owns it.

---

## 10. Test Strategy

The refactor succeeds only if tests prove behavior moved safely.

### 10.1 Always Required

```powershell
go build ./...
go vet ./...
go test ./...
```

### 10.2 Boundary Tests

Add or preserve tests that prove:

- `agent` can run without Telegram/API imports,
- prompt/context assembly is deterministic for the same inputs,
- `chat` can route without knowing transport formatting,
- each channel adapter preserves transport-specific behavior,
- tools execute through registry contracts,
- deferred tool discovery works without loading every schema upfront,
- complex tool calls use examples and pass argument-shape probes,
- programmatic orchestration returns summaries/artifact handles instead of dumping intermediate data,
- recoverable tool errors become retry feedback and learning events,
- retrieval returns layer-labelled cited hits, not undifferentiated memory soup,
- RAG evals cover user memory, wiki/source knowledge, and operational lessons separately,
- learning retrieval uses validated lessons and redacts sensitive inputs,
- memory writes inspect durable wiki output,
- storage/indexes can be rebuilt from durable state.

### 10.3 Prompt Evals

Required for every meaningful prompt/runtime change:

- task fidelity on representative user asks,
- context utilization on retrieved wiki/source/user-memory/operational-memory capsules,
- correct tool triggering versus direct answer,
- correct question emission when intent or memory write is ambiguous,
- output contract adherence,
- citation/source faithfulness where sources are used,
- latency and token cost envelope,
- style/tone stability for chat channels.

### 10.4 Golden/Fixture Tests

Required for:

- Telegram streaming edits,
- source conversions,
- wiki write output,
- public API response shape.

Use actual artifacts and snapshots where behavior matters.

### 10.5 Concurrency Tests

Required for:

- SQLite hot paths,
- tool execution fan-out,
- background/cron runs,
- swarm child runs.

Prior `SQLITE_BUSY` evidence means database settings must be tested under pressure, not assumed.

---

## 11. Definition of Done

A refactor slice is done when:

- the module responsibility is clearer than before,
- dependencies move toward the allowed direction,
- no god package is created,
- old behavior is protected by tests or fixtures,
- build/vet/test are green,
- public behavior is unchanged unless the PRD explicitly says otherwise,
- sources, example files, and rejected alternatives are recorded for decisions
  that came from research or external patterns,
- `scripts/ralph/prd.json` and progress notes are updated only after verification,
- the commit is atomic and named for the architectural change.

A slice is not done when:

- files were merely moved,
- tests only prove compilation,
- behavior was "probably" preserved,
- a package got a better name but still owns too many concerns,
- a temporary compatibility wrapper becomes the new permanent architecture.

---

## 12. Non-Goals

Do not use this refactor to add:

- a dashboard redesign,
- a new graph database,
- an embedding backend migration,
- a GPU embedding path,
- a new web chat product,
- a broad source ingestion rewrite,
- a new orchestration framework,
- a Responses API dependency,
- raw RL/PARL/model fine-tuning inside this refactor,
- dynamic tool mutation mid-conversation.

These may be valid later. They are distractions before the core is understandable.

---

## 13. Operating Rule For Workers

Before editing any file, a worker must answer:

1. Which target module owns this responsibility?
2. What dependency direction does this change improve?
3. What behavior could regress?
4. What test or fixture proves it did not regress?
5. Which source files, external references, and example patterns justify this
   change?
6. What temporary compatibility code is being added, and when can it be removed?

If the worker cannot answer these, the task is not ready.

---

## 14. The One-Sentence Direction

Aura becomes maintainable when every path enters through a channel or scheduled entrypoint, flows through `chat`, uses one `agent` runtime, accesses capabilities through `agent/tools`, learns from recoverable tool failures through `learning`, persists through `memory` and `storage`, and returns through an adapter without the core ever learning about the adapter's world.

---

## 15. Web Chat Product Surface

This section consolidates the chat-product requirements previously documented in `docs/chat-interface-prd.md`. That doc is preserved as historical evidence; the active requirements live here.

### 15.1 Product Decisions (closed)

| Theme | v1 Decision |
| --- | --- |
| Memory sharing | Web and Telegram share the curated memory of the same Aura principal when accounts are linked; threads stay per-channel. |
| Identity | Channel-neutral canonical principal. Telegram user, dashboard bearer user, heartbeat, cron all map to that principal. New endpoints DO NOT accept `user_id` from the client. |
| Interactive chat runtime | Chat Hub uses a channel-neutral runtime extracted from `agentruntime/agentloop`. `agent.Runner` legacy is removed (Phase 4 kill-runner). |
| Model selector | Read-only v1: shows the active model. Per-thread model switch only after a reliable model catalog and `chat_threads.model` persistence. |
| Upload default | Source-first always. File without prompt = `store only`. File + prompt or explicit choice = `store and analyze`. |
| Upload async | Upload endpoint returns attachment/source ref quickly. OCR/extract/ingest continue async with `attachment_status` events. |
| Delete thread | Soft-delete v1, retention/purge later. |
| Questions | Backend primitive (not frontend heuristic). Empty state can show conservative starter prompts; clarifications come from backend. |
| Clarification format | `chat_questions` table, reusable for future automation/tasks. |
| Heartbeat/Cron silent | Supported via `DeliveryMode=silent`: update thread/memory without notification unless task opts in. |
| `/api/chat` | Stays as compat wrapper over Chat Hub during migration. No new features land here only. |

### 15.2 UX Target

`/chat` is the primary web conversational surface. First screen must be directly usable.

**Layout desktop:**

- viewport full-height, no body scroll
- chat sidebar 248-280px
- thread content centered, max-width 760-920px
- composer sticky/fixed bottom, centered
- separate scroll for sidebar and thread
- no outer card around main chat

**Layout mobile:**

- sidebar collapsed in drawer
- compact top bar (menu, thread title, actions)
- composer always reachable, safe-area aware
- messages full-width with reduced lateral padding

**Shell:** `/chat` uses a dedicated `ChatShell` (or existing shell with `fullscreenChat` mode). Body/root stays no-scroll; only sidebar and thread scroll. Composer requires spacer / `scroll-padding-bottom` equal to its max visible height.

### 15.3 Sidebar

- toggle collapse
- `New Chat` (lucide icon)
- `Launch` (quick access to operational dashboard)
- `Settings` (quick access to `/settings`)
- conversations grouped by period: Today, This Week, Older
- thread title from first user message or first assistant summary
- active state via `--sidebar-accent` + `--primary`
- context menu per thread: rename, delete, export markdown/json

### 15.4 Thread and Messages

**Functional requirements:**

- create new empty thread; send user messages; render assistant streaming reply; preserve per-thread history; resume thread without losing context
- show `user`, `assistant`, `tool`, `system/error` with distinct styles
- copy message, retry last response, stop generation
- auto-scroll to bottom ONLY if user was near bottom; never force-scroll while reading older content

**Assistant messages:** full GFM markdown; readable tables; code blocks with horizontal overflow; tables in `overflow-x-auto` wrapper with `min-width: 0` container; links open in new tab; raw-text fallback on markdown crash.

**Tool messages:** collapsed by default; show tool name + status + duration + error; expandable for tech output; NO secrets or sensitive args in clear text.

### 15.5 Composer

Fixed bottom, includes:

- multiline autosize textarea (1 row min, ~8 rows max)
- placeholder `Send a message` (i18n)
- Enter to send, Shift+Enter for newline
- `+` button for attachments/actions
- web/search or tool mode button when available
- model selector (active value, e.g. `deepseek-v4-flash:cloud`)
- circular send button; stop button while generating
- disabled state when LLM unconfigured, token expired, or hard budget reached

`+` menu: upload file → source inbox; paste/import text as source; create task/reminder from selected prompt; quick link to workspace files when available.

**Upload requirements:** drag-and-drop on thread and composer; multi-file; per-file preview (name, ext, size, status); per-file removal pre-send; per-file progress; per-file retry; size limit aligned to `MaxUploadMB`; type validation aligned to supported extractors; attachments sendable with text prompt; explicit `store only` vs `store and analyze`; post-upload chip/link to `src_*`; status chain `queued`→`uploading`→`stored`→`ocr_complete`→`extracting`→`extract_complete`→`ingested`/`failed`/`cancelled`; if upload produces wiki pages, response links them.

**Upload constraints:**

- never show full browser local paths
- never forward files to LLM provider if Aura can save them to source store first
- never block composer during long uploads
- upload cancellable until request body completes
- real progress needs `XMLHttpRequest.upload.onprogress` equivalent; with `fetch` declare indeterminate progress, do not fake percentages
- each file has stable `client_attachment_id` for preview/response/SSE/retry correlation
- multi-file v1 can upload sequentially but UI shows per-file state and never loses the batch on partial failure

### 15.6 Question UX

Questions are interaction elements, not free-form text.

**Types:**

- **Starter questions** — empty state suggestions, aligned with Aura + available memory

- **Follow-up questions** — post-reply suggestions, generated from thread context
- **Clarification questions** — Aura asks when missing data for action
- **Question cards** — compact 2-4 option blocks when structured choice is safer than free text

**Requirements:**

- empty state shows 3-5 starter questions
- after each assistant reply, show up to 3 follow-up questions
- click compiles and sends composer, or pre-fills if modification needed
- clarifications block only the specific action, never the whole chat
- question cards support single, multi-select, free response
- question card answers enter thread as readable user messages
- questions can be disabled per thread
- no suggestions if assistant errored or thread is streaming
- questions never invent capability — respect available tools and backend

**Question contract (minimum):**

```go
type Question struct {
    ID               string
    RunID            string
    ThreadID         string
    MessageID        string
    Kind             string // starter | follow_up | clarification | approval
    Prompt           string
    Options          []QuestionOption
    Multiple         bool
    FreeTextAllowed  bool
    FreeTextRequired bool
    DefaultOptionID  string
    BlockingScope    string // none | run | tool_call | attachment | thread
    Dismissible      bool
    Status           string // pending | answered | dismissed | expired
}
```

This is the web-surface projection of the §5.2 QuestionGate contract. The two must stay aligned.

### 15.7 Theme

Use existing tokens — never raw Tailwind colors when a semantic Aura token exists.

| Element | Token |
| --- | --- |
| Thread canvas | `--bg`, `--surface-sunken` |
| Chat sidebar | `--sidebar`, `--sidebar-border`, `--sidebar-accent` |
| User bubble | `--user-bubble` |
| Assistant bubble | `--assistant-bubble` |
| Tool event | `--tool-bubble`, `--brand-soft` |
| Composer | `--surface-raised`, `--border`, `--brand` |
| Error state | `--destructive`, `--destructive-foreground` |
| Focus ring | `--brand` |

**Visual constraints:** dark mode is the reference; cyan accent visible but measured; no hero/marketing-card/decorative-orb/gradient outside the existing global background; sidebar darker than thread with thin border; thread on clean canvas (no outer card); composer raised surface, thin border, generous-but-consistent radius; icon-only buttons with accessible tooltip; `lucide-react` icons; Geist font; no viewport-width-based font-size.

### 15.8 API Surface (web chat)

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/chat/threads` | List threads |
| `POST` | `/api/chat/threads` | Create thread |
| `GET` | `/api/chat/threads/{id}` | Thread detail + messages |
| `PATCH` | `/api/chat/threads/{id}` | Rename, archive, update model |
| `DELETE` | `/api/chat/threads/{id}` | Delete thread |
| `POST` | `/api/chat/threads/{id}/messages` | Non-streaming fallback |
| `POST` | `/api/chat/threads/{id}/stream` | Streaming send |
| `POST` | `/api/chat/threads/{id}/stop` | Best-effort cancel |
| `POST` | `/api/chat/threads/{id}/attachments` | Upload |
| `POST` | `/api/chat/threads/{id}/attachments/{aid}/ingest` | Start analysis |
| `DELETE` | `/api/chat/threads/{id}/attachments/{aid}` | Remove unused attachment |
| `GET` | `/api/chat/threads/{id}/questions` | Current questions |
| `POST` | `/api/chat/threads/{id}/questions/{qid}/answer` | Answer a question |
| `GET` | `/api/chat/models` | Active + available models (best-effort) |

**Streaming:** Server-Sent Events preferred; `fetch + ReadableStream` alternative. Minimum events: `run_started`, `message_created`, `message_delta`, `message_done`, `tool_start`, `tool_delta`, `tool_end`, `attachment_status`, `question_requested`, `question_answered`, `usage`, `done`, `error`. Each event has `thread_id`, `message_id` or `client_message_id`. Heartbeats during long waits. Mid-stream errors must be recoverable UI events, not parser crashes. Provider format (OpenAI/OpenRouter/Anthropic SSE, Ollama NDJSON) adapted before reaching frontend. Each persistible event has `event_id`, `run_id`, `seq`, `created_at` and `idempotency_key` when from retry. Order is per `run_id, seq`. Client tolerates duplicates and partial replay. Backend MAY coalesce `message_delta`, but never reorder tool/question/error events. Non-streaming fallback persists at least `message_created`, `message_done`, `usage`, `done` in `chat_events`.

### 15.9 Non-Goals (web chat)

- Replace Telegram as supported channel — both stay.
- Create a new agent/prompt for web chat.
- Hardcode an LLM provider on the frontend.
- Expose destructive tools without the gates already in Aura.
- Build a landing or marketing page.
- Create a second agent loop just for web chat.

### 15.10 Where this lands in the 9-Phase Plan

- **Phase 2-3** (channels behind chat): provides the Hub seam that web chat plugs into.
- **Phase 4** (collapse runtime): the channel-neutral runtime that web chat needs.
- **Phase 1C** (QuestionGate): the backend primitive for §15.6.
- **Phase 8** (cron + silent channels): heartbeat/cron `DeliveryMode=silent` flow.
- **Post-Phase 9**: SSE streaming endpoint hardening, model catalog v1, full UX polish.

The slice ordering documented in the historical `docs/chat-interface-prd.md` §18 is superseded by the 9-phase plan; the requirements above survive.

---

## 16. Decision Records (D1-D13)

These are the cross-cutting decisions inherited from the predecessor `docs/aura-master-plan.md` (now historical). Each was validated by the deep-refactor work that followed. Authoritative ADR storage is `.planning/aura-deep-refactor-decisions.json`; this section is the human-readable narrative.

### D1. `agent.Runner` — cancel or extend?

**Winner: Cancel.** Route swarm + chatPipe + scheduler agent_job onto `agent.Run`. The legacy `runner.go` exists because nobody replaced it; the canonical runtime by design is `agentruntime`+`agentloop`. Wave 3 Pack A's PhaseSink work would have invested 350-450 LOC into the wrong runtime. **Cost-if-wrong:** 1 day revert. *Implemented in Phase 4 / past commit chain.*

### D2. Restructure-first vs feature-first

**Winner: Surgical-first** — kill duplicate runtime + chathub merge, then waves resume on the right shape. Avoids rework where Wave 3 would have written code on `runner.go` that we'd cancel. **Cost-if-wrong:** user perceives 4 weeks of restructure as "more disorder" — mitigated by per-step user-verifiable G-criteria and atomic revert-safe slices.

### D3. Package count target

**Winner: 22 (stretch 21).** Verified arithmetic 49→22 via mechanical merges. Empty `orchestration/`, `tracing/`, `release/` are delete-free. Merge packs `agent/*`, `mcp/*`, `storage/search/*`, `storage/sources/*`, `agent/tools/*` reduce by 14; `config/*`, `api/*`, `db/*`, `session/*` by 7 more. **Cost-if-wrong:** 23-25 instead of 22 — acceptable.

### D4. chathub layer — keep, integrate, delete?

**Winner: Integrate now.** The spine (`types.go`+`hub.go`+`agentloop.go` ≈ 620 LOC) is sound. Web router + silent + chat_service are fine. Only `adapters/telegram/outbound.go` needed replacement (port from `streaming.go`). **Cost-if-wrong:** CoT/entity regression — mitigated by feature flag + record-replay fixture.

### D5. chathub Telegram outbound regression — delete now or after?

**Winner: Replace before adoption.** Build fixture harness (Phase 2 record-and-replay) first, then port `streaming.go` atomically as `channels/telegram/outbound.go`. Snapshot diff is the gate. **Cost-if-wrong:** 1 edit+revert cycle.

### D6. `.planning/` waves — superseded, deferred, interleaved?

**Winner:** see PRD §3.2 table for the full per-wave disposition. Wave 1 task 3 SHIPPED; Wave 1 tasks 1/2/4/5 deferred; Wave 2 deferred to Phase 7; Wave 3 Pack A SUPERSEDED (events flow via Hub `OutboundEvent`); Pack B/C deferred to Phase 6/8; Context-Eng Fase 0 INTERLEAVED-now, Fase 1.1 promoted as PREREQ to Phase 4 (kill runner), Fase 3.3 inside Phase 4, Fase 5 inside Phase 4 mapping table.

### D7. Hub responsibility — thick or thin?

**Winner: Thick ≤700 LOC.** Already verified (hub 251 + types 159 + agentloop 210 = 620). Carries 7 EventType cases, dispatcher with event-tap, run/event ID minting, `/stop` registry. Cannot be reduced below 600 without dropping features. **Cost-if-wrong:** if it grows >700, refactor.

### D8. Swarm — first-class channel or tool?

**Winner: First-class channel.** D1 cancels `agent.Runner`; D8 closes the loop. Swarm sub-task = `InboundMessage{Channel: swarm, Mode: silent, ChannelData: {parent_run_id, assignment_id}}`. Manager collects results via `Router.WaitForRun(runID)`. Wave 3 Pack A folds into Hub event stream. **Cost-if-wrong:** 1 day revert (swarm back to calling `agent.Run` directly).

### D9. External references (Kimi K2.5 + OpenAI Codex)

**Winner: Reference only — cite both, adopt neither as architecture.** Kimi K2.5 (parallel decomposition, swarm as tool opt-in for decomposable tasks). OpenAI Codex Jan 2026 (single flat agent loop as default — what Aura IS and stays). Aura confirms single-loop default; swarm is `swarmtools.delegate` opt-in for decomposable tasks, NOT a replacement for the main loop.

### D10. Effort horizon — sprint or campaign?

**Winner: Short campaign (3-4 weeks).** Step 1-8 of the historical plan ≈ 70 productive hours ≈ ~4 calendar weeks at 4h/day. Wave 1 residue + Wave 2 + Wave 3 Pack B/C after. Context-Eng Fasi 0+1 INTERLEAVED. **Cost-if-wrong:** 1-2 week slip — absorbable.

### D11. `streaming.go` port — verbatim or rewrite?

**Winner: Verbatim + thin wrapper.** `consumeStream` becomes `chat.OutboundAdapter.Deliver`; `tele.Bot` helpers stay private in `channels/telegram`. 30 LOC of wrapper. Fixture harness captures pre-port snapshot; post-port byte-compares. **Cost-if-wrong:** fixture errors — revert.

### D12. Composition root — `cmd/aura/app.go` or `telegram/setup.go`?

**Winner: `cmd/aura/app.go`.** Honors "telegram is only an adapter"; composition root visible. Implemented in deep-refactor Phase 1 (US-A13 series). **Cost-if-wrong:** shutdown race — mitigated by `go test -race ./...` gate.

### D13. Prompt-cache discipline — feature wave or config-engineering policy?

**Winner: Policy locked BEFORE Phase 4 (kill runner).** Codex Jan 2026 explicitly: *"place static content at the beginning of your prompt, and put variable content at the end."* Static-first prefix + tool order stability + append-not-mutate runtime context. Phase 4 touches prompt assembly; without lock, cache-miss regression is invisible. **Cost-if-wrong:** cache hit <50% → p95 doubles. Gauge: prompt token reuse ratio (Context-Eng Fase 1.2 telemetry).
