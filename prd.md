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
  learning/       tool experience, self-healing lessons, promotion workflow
  rag/            schema-aware retrieval, hybrid search, rerank, citations
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

Do not event-source all of Aura. Wiki pages, source files, indexes, and tool artifacts keep their own source-of-truth rules. The run/event store is the backbone for execution, questions, observability, learning, cron, and swarm.

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
- block only the smallest scope possible: tool call, run, child run, or thread,
- cap repeated questions per run and record unnecessary-question eval failures.

The model may request input through a narrow structured `request_input` action when it can justify the block. The runtime may also create the question itself when a deterministic policy, authorization boundary, or tool preflight requires it. The channel adapter only renders the question; it does not decide whether asking was valid.

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

### 5.6 `internal/memory`

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

> The Markdown wiki is the readable memory projection. Search/vector/index state is rebuildable support, not the product truth.

Memory layers:

```text
active run context        runtime only, not source of truth
conversation archive      audit/history, consolidation input
user memory               stable user facts, preferences, constraints
knowledge wiki            curated pages, concepts, syntheses, decisions
source corpus             raw files, OCR/extracts, page spans, provenance
Aura operational memory   tool lessons, failed approaches, open questions
skills                    procedural knowledge and workflows
RAG indexes               rebuildable projections over durable layers
```

Write policy:

- user memory requires explicit intent, validation, or a question when ambiguous,
- user memory writes require `memory.user.write` or a narrower capability grant,
- operational memory belongs to Aura and must not pollute the user wiki,
- source corpus is immutable evidence until curated,
- wiki pages are curated knowledge, not chat logs or raw tool failures,
- indexes are disposable projections.

### 5.7 `internal/learning`

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

### 5.8 `internal/rag`

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

### 5.9 `internal/storage`

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

### 5.10 `internal/cron`

Cron is a scheduled entrypoint.

It owns:

- recurring jobs,
- due-work lookup,
- schedule persistence,
- triggering work at the right time,
- delegated background actors.

It must not own:

- a separate agent loop,
- separate tool execution rules,
- special hidden chat behavior.

Cron should submit work into `chat` or `agent` through the same contracts used by other entrypoints.

Scheduled work must run as a delegated actor with explicit capabilities, expiry, and notification policy. A cron job is never implicitly the owner.

### 5.11 `internal/api`

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
- keep payloads redacted and schema-versioned.

Gate:

- duplicate inbound delivery with the same idempotency key does not create a second run,
- events replay into the same run snapshot,
- terminal run state survives process restart,
- failed outbound delivery remains retryable through outbox state,
- tests cover per-run ordering, cancellation, and parent/child correlation.

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

### Phase 4 - Collapse the Agent Runtime

Goal: one loop, one runtime path.

Current reality:

- the duplicate `agent.Runner` body was reduced into an adapter,
- the `agent.Runner` type still exists,
- production references still exist.

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

### Phase 6 - Add the Tool Experience Loop

Goal: Aura improves from preventable tool-call failures instead of repeating them.

Steps:

- define structured tool observation and tool error contracts,
- classify tool results as ok, recoverable error, or fatal error,
- inject recoverable error feedback into the same run,
- cap retries and record why a retry was attempted or refused,
- persist tool attempts and outcomes as learning events,
- retrieve validated lessons for similar future tool calls,
- promote repeated lessons into memory, skills, or tool policy only after validation.

Gate:

- a recoverable tool error can be corrected in the same run,
- repeat failures are visible by tool and error kind,
- secrets and raw sensitive args are redacted from learning records,
- retrieved lessons are versioned against tool schema/version,
- no automatic prompt/code mutation happens without validation.

### Phase 7 - Rebuild RAG On Typed Memory Layers

Goal: retrieval stops being one broad memory soup.

Steps:

- define memory layer IDs and citation handles,
- create a collection metadata registry for wiki, sources, user memory, archive, and operational memory,
- split recall behavior by task intent instead of one polymorphic mode parameter,
- implement hybrid FTS/vector retrieval with RRF fusion where available,
- preserve chunk-to-parent source expansion,
- return structured retrieval hits with score components and follow-up handles,
- make retrieval errors recoverable learning events,
- add golden RAG evals for user facts, wiki/source answers, and operational lessons.

Gate:

- user facts do not appear in wiki unless intentionally promoted,
- tool failures do not appear in wiki,
- source hits cite source/page/span or stable artifact handle,
- wiki hits cite `[[slug]]`,
- retrieval fixtures prove hybrid beats vector-only and keyword-only on the golden set,
- repeated bad filters/searches produce self-healing feedback.

### Phase 8 - Route Cron and Swarm Through the Same Shape

Goal: background and child-agent work stop being special cases.

Cron:

- triggers scheduled work,
- submits through the same contracts,
- does not own a private loop.

Swarm:

- uses parent/child run IDs,
- dispatches via chat/hub or equivalent normalized path,
- caps subagent outputs,
- requires artifact references.

Gate:

- parent run ID propagation tested,
- background jobs observable,
- no hidden alternate runtime.

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

---

## 8. Immediate Queue Direction

`prd.json` remains the machine queue. The queue should be judged against this PRD, not treated as the architecture itself.

Current completed direction:

- `api` consolidation has started,
- `storage` namespace has started,
- `chat` and `channels` naming has started,
- the old duplicate agent runner body was reduced.

Next correct work:

1. Finish the Telegram fixture before porting outbound behavior.
2. Port Telegram outbound only with fixture protection.
3. Merge runtime only after channel risk is controlled.
4. Rename scheduler to cron.
5. Consolidate tools under agent capabilities.
6. Add structured learning events for recoverable tool errors.
7. Rebuild RAG around typed memory layers and schema-aware retrieval.
8. Route swarm and Telegram through the normalized path.

If a queue item conflicts with this direction, update the queue before executing it.

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
- `prd.json` and progress notes are updated only after verification,
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
5. What temporary compatibility code is being added, and when can it be removed?

If the worker cannot answer these, the task is not ready.

---

## 14. The One-Sentence Direction

Aura becomes maintainable when every path enters through a channel or scheduled entrypoint, flows through `chat`, uses one `agent` runtime, accesses capabilities through `agent/tools`, learns from recoverable tool failures through `learning`, persists through `memory` and `storage`, and returns through an adapter without the core ever learning about the adapter's world.
