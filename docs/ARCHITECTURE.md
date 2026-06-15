# Aura — Architecture

**Module:** `github.com/chetto1983/aura` · **Language:** Go 1.26 · **Updated:** 2026-06-15

Aura is a single-operator, local-first AI agent platform written in Go. One static
binary hosts the agent runtime, the tool surface, the transport channels (CLI,
Telegram, AG-UI/SSE), and the persistence adapters; heavy or untrusted work is
pushed to sidecars and MCP servers over well-defined seams. This document is the
narrative map. For the exhaustive symbol-level inventory see
[CODEBASE_MAP.md](CODEBASE_MAP.md); for the product framing see
[TECHNICAL_OVERVIEW.md](TECHNICAL_OVERVIEW.md) and [CAPABILITIES.md](CAPABILITIES.md).

## 1. Design principles

These recur throughout the code and explain most of the non-obvious decisions:

- **Local-first, single trusted operator.** Aura runs on the user's own machine
  for one user. Filesystem and shell tools have full host access by design
  (amendment #50/D-15c); isolation is reserved for explicitly untrusted inputs
  (MCP server output, web content) rather than the operator's own commands.
- **Provider-neutral core.** The agent loop targets an interface (`llm.Client`),
  never a vendor SDK. The default provider is DeepSeek-V4 over OpenRouter, swapped
  by config alone.
- **KV-cache discipline.** `messages[0]` (the system prompt) is byte-stable across
  turns and workers; volatile data (budget, time, workspace) is appended *after*
  history so the cached prefix is never poisoned. This is load-bearing for cost.
- **Deferred-tool pattern.** Large tool specs are hidden from the per-turn manifest
  and discovered on demand via a semantic `tool_search`, so the tool surface scales
  to dozens of tools (incl. dynamic MCP tools) at near-zero per-turn token cost.
- **Bounded everything.** A shared `Budget` tree caps steps and wall-clock across an
  entire agent tree; a two-phase dedup ring stops tool-call loops; outputs are
  capped-and-spilled; background work is goroutine-leak-safe (goleak-tested).
- **Trust boundaries are explicit.** Anything the model didn't author — MCP results,
  web pages, document text — is wrapped/framed as untrusted before it re-enters the
  prompt (prompt-injection containment), and secrets are redacted at every egress.
- **Self-improvement without latency.** A reusable embedding-index substrate powers
  both reasoning-tier routing and tool discovery, and an async learner upgrades them
  off the hot path so accuracy rises without slowing any user turn.

## 2. Layered view

```
┌──────────────────────────────────────────────────────────────────────────┐
│ Transport & UX        cmd/aura (CLI) · channels (+telegram) · agui (SSE)   │
│                       setup wizard · askuser                               │
├──────────────────────────────────────────────────────────────────────────┤
│ Agent runtime         agent (LlmAgent loop, Budget tree, Event model,      │
│                       hooks, trust, tracing) · agent/workflow (Seq/Par/Loop)│
│                       · swarm (fan-out) · agent/prompt (builder + reasoning)│
├──────────────────────────────────────────────────────────────────────────┤
│ Tools & MCP           agent/tools (registry, deferred pattern, tool_search,│
│                       fs/shell/web/skill/task/doc/swarm/send_file …)        │
│                       · agent/mcptools (bridge) · mcp (client) · mcp/manager│
├──────────────────────────────────────────────────────────────────────────┤
│ Intelligence subst.   llm (+openai_compat) · semindex (embed-index core)   │
│                       · activelearn · reasoning{fifo,learn,store,trace}     │
│                       · toolselect{learn,store} · scoring                   │
├──────────────────────────────────────────────────────────────────────────┤
│ Capabilities          web · skills (+skilladapters) · cron (+handlers)     │
│                       · onboarding · documents · eval · runner             │
├──────────────────────────────────────────────────────────────────────────┤
│ Persistence           db (+sqlc, Postgres) · knowledge (Neo4j) ·           │
│                       conversations · identity · profile · secret          │
├──────────────────────────────────────────────────────────────────────────┤
│ Observability         obs · panicobs · reasoningtrace · toolinvocations ·  │
│                       cachemetrics · (OTel spans + Prometheus/expvar)       │
└──────────────────────────────────────────────────────────────────────────┘
        Sidecars (out of process): embedding (granite), markitdown extractor,
        OCR/STT/TTS, mcp-neo4j-cypher, MCP recipe servers (calculator/mail/
        whatsapp/memory), Postgres, Neo4j.
```

## 3. The agent runtime

The cornerstone is `internal/agent`. Everything composes around one open interface:

```go
type Agent interface {
    Name() string
    Description() string
    Run(InvocationContext) iter.Seq2[*Event, error]
    SubAgents() []Agent
    FindAgent(name string) Agent
}
```

- **`Event` / `Actions`** is the single signal type streamed out of every `Run`. Its
  shape is forward-compatible with AG-UI and carries OTel trace identity. Crucially,
  *termination and budget exhaustion travel as Events* (`Actions.Escalate` + a
  `StateDelta` reason), never through the error slot — the error slot is reserved for
  genuine infrastructure failures.
- **`Budget`** bounds one run: a shared `*atomic.Int32` step counter plus a wall-clock
  deadline, threaded down the whole agent tree. `Budget.Child(fanout)` forks a parallel
  branch that shares the counter but gets its own dedup ring. A two-phase dedup ring
  (`BeforeToolCall` / `AfterToolResult`) detects repeated tool calls and uses a changing
  result preview as a progress veto, so a tool whose output actually changes is never
  falsely throttled.
- **`LlmAgent`** is the concrete loop: build the prompt (cache-safe), pick a reasoning
  tier, open the model stream (with circuit-breaker + bounded retry), consume chunks,
  dispatch tool calls (the terminal `text_response` ends the turn), append results,
  and re-loop until a terminal condition. It owns its budget (`OwnsBudget()=true`), so
  workflow parents observe it without double-charging.

**Workflow agents** (`internal/agent/workflow`) compose `Agent`s into trees:
`SequentialAgent`, `ParallelAgent` (errgroup fan-out, escalate cancels siblings,
goroutine-leak-safe), and `LoopAgent` (re-run until max-iterations / escalate /
budget / dedup / no-progress). These are the adk-go-derived primitives the onboarding
interview, cron handlers, and swarm reuse.

**Swarm** (`internal/swarm`) is an ephemeral per-call fan-out coordinator behind the
`swarm_spawn` tool: it runs N goals as budget-bounded `LlmAgent` workers in
concurrency-capped waves, isolates per-child failure (a failed worker becomes a
`{failed}` report; siblings are never cancelled — D-02), and returns an ordered
`[]ChildReport`. v1 is deliberately flat (workers cannot spawn workers).

### Turn lifecycle (one user message → one answer)

```
user msg ─▶ Runner loads managed history (L1/L2/L2.5 context ladder)
         ─▶ PromptBuilder.Build: messages[0] (stable) + history + volatile <budget> tail
         ─▶ adaptiveReasoningTier: local granite-embedding classifier (~10ms) → none|low|high
         ─▶ LlmAgent.Run loop:
              Budget.ConsumeStep (gate) ─▶ stream open (breaker+retry)
                 ─▶ consume chunks (text / reasoning / tool-call deltas)
                 ─▶ dispatch tool calls:
                      • BeforeTool hooks + dedup gate (serial, in order)
                      • execute runnable calls (concurrent, semaphore-bounded)
                      • each result: cap→preview→sidecar, wrap-if-untrusted, append
                 ─▶ terminal? text_response → completion-gate critic → final Event
         ─▶ persist assistant turn (+ cache metric) atomically; emit Events to channel(s)
```

## 4. Tools & MCP

`internal/agent/tools` is the largest surface. A `Registry` maps tool name → `Tool`;
the agent renders an alphabetical manifest each turn (cache-stable). The **deferred-tool
pattern** keeps that manifest small: tools with heavy specs set `Deferred=true` and
appear only as name + one-line summary until the model calls `tool_search`, which ranks
them with a semantic embedding index (`semindex.Ranker`) plus a guarded BM25 tiebreak.
Every large tool result is capped to a preview and spilled to a per-conversation sidecar
file, paged back via `read_tool_output`.

Built-in tools span the full operator surface: filesystem (`fs_read/write/edit/grep/glob`),
the keystone `shell_exec` (full host terminal, with background jobs via `shell_poll`/
`shell_kill`), web (`web_search` over SearXNG, `web_fetch` SSRF-hardened), knowledge
(`document_search`), orchestration (`swarm_spawn`), self-extension (`skill`),
scheduling (`task`), HITL (`ask_user`), working memory (`todo_write`), artifact delivery
(`send_file`), and `text_response`/`current_time`/`read_tool_output`.

**MCP** is the extension path for third-party capabilities. `internal/mcp` is a generic
JSON-RPC client (stdio + Streamable-HTTP); `internal/agent/mcptools` bridges any MCP
server's tools into the registry — namespaced `<server>__<tool>` so they can't shadow
built-ins, trust-framed as untrusted data, schema-capped, and **deferred by default**
(the `memory` server is the exception, kept visible). `internal/mcp/manager` owns the
durable managed-server registry, trust classification, docker/local launch resolution,
and a curated recipe catalog (calculator, calendar, mail, whatsapp, memory).

## 5. Intelligence substrate

- **`llm` + `llm/openai_compat`** — the provider-neutral streaming contract and a
  hand-rolled OpenAI-compatible SSE client (no SDK: byte-level framing, tool-call delta
  accumulation, idle watchdog, ctx-cancel teardown, bounded error capture). Default model
  is `deepseek/deepseek-v4-flash:exacto` over OpenRouter; cost is read from the provider
  when present and falls back to a price table (never a fabricated `$0`).
- **`semindex`** — Aura's single reusable embedding-index core: a lock-free cosine/
  centroid/margin math layer plus two wrappers, `Classifier` (centroid argmax + top-2
  margin) and `Ranker` (top-K cosine). It powers **both** reasoning-tier routing and
  `tool_search`. Brute-force over small immutable banks (sub-millisecond), no ANN.
- **Adaptive reasoning router** (`agent/prompt` + `reasoning*`) — instead of a per-turn
  LLM round-trip to decide reasoning effort (the original latency root cause), a local
  granite-embedding classifier maps the turn to `none|low|high` in ~10ms; on abstention
  it falls back to the LLM "oracle" router. An async learner (`reasoninglearn` →
  `activelearn` → `reasoningstore` in Neo4j) labels the uncertain turns off the hot path
  so the classifier converges toward oracle accuracy with no added latency.
- **`activelearn`** — the label-agnostic async self-improvement mechanism (bounded queue,
  content-hash dedup, margin gate, drop-on-full, goleak-clean). Two consumers: reasoning
  routing and tool selection (`toolselectlearn` → `toolselectstore`), the latter teaching
  `tool_search`'s per-tool centroids from confirmed routings via a free-ranker/DeepSeek
  two-tier oracle.
- **`scoring`** — pure Risk-Based governance: maps scheduler tasks and skill mutations to
  a `Safe|Normal|Risky|Destructive` tier for advisory gating.

## 6. Persistence

Two stores, each behind a thin per-domain adapter (`Store{q}`) over generated sqlc /
Cypher, with SQLSTATE-classified errors and pgtype boundary conversion:

- **Postgres** (`internal/db`, `internal/db/sqlc`) — pgxpool + golang-migrate, a
  two-role split (`aura_app` runtime vs `aura_migrate` DDL), the `aura.*` schema, and a
  `WithTx` atomic-write seam. 15 migrations (0001–0015). Domains: conversations + turns
  (+ FTS), identity + capabilities, paused states (HITL), scheduler + agent-job runs,
  skill audit, telegram accounts + setup tokens, tool-invocation ledger, cache metrics,
  context-rot events, document-ingest jobs, knowledge-migration audit. DSNs are redacted
  in every error.
- **Neo4j** (`internal/knowledge`) — the LLM-facing runtime interface is the
  `mcp-neo4j-cypher` subprocess over stdio (no native driver for data ops); a separate
  driver-backed `SchemaExecutor` runs DDL the MCP layer can't. HNSW 384-d vector index +
  fulltext, Cypher migrations audited in Postgres. Holds the document graph and the
  self-improvement example stores (`:ReasoningExample`, `:ToolSelectionExample`).

**Conversations** (`internal/conversations`) layers the context-management ladder on top
of Postgres: L1 microcompact (rewrite old tool turns to `read_tool_output` pointers), L2
budget gate, L2.5 oldest-pair drop — each writing a context-rot event — plus an offline
tiktoken estimator, an atomic per-turn append (turn + aggregates + cache metric in one
tx), best-effort auto-titling, and boot/periodic sidecar GC.

**Documents** (`internal/documents`) is the ingestion pipeline: a markitdown sidecar
extracts PDF/xlsx/DOCX to chunks, Neo4j holds the sparse FTS + (async-embedded) vectors,
and `document_search` returns cited chunks. **Identity / profile / secret** hold the
single-user identity + capability grants, the per-identity `Agent.md` profile (atomic
writes), and the one shared secret-env denylist used at every redaction site.

## 7. Transport & UX

- **`agui`** — the AG-UI protocol transport: a one-way bridge from Aura's `iter.Seq2[*Event,
  error]` stream onto the AG-UI community Go SDK over SSE. The translator is a pure
  function (property/golden-testable); the runtime never imports agui (CI-enforced).
- **`channels` + `channels/telegram`** — a `Channel` interface + registry, with Telegram
  as the primary user-facing channel: AG-UI-event renderer, HITL via inline keyboards /
  force-reply, multimodal photo/voice/document handlers, a live status pane, and the
  `/start <token>` onboarding match.
- **`askuser`** — the CLI responder rendering `ask_user` pauses; **`setup`** — the
  loopback setup wizard (HTTP + QR) for pairing a Telegram bot.

## 8. Observability

Every layer is instrumented: OTel spans (`agent.turn` → `llm.request` → `tool.execute`,
crypto-random span ids, api-keys never stamped), dual Prometheus + expvar metrics
(budget steps, tool dispatch, stream open/retry, turn outcomes, token/cost, panics),
the bounded-cardinality `panicobs` recovered-panic counters, the env-gated redacting
`reasoningtrace`, the append-only un-deletable `toolinvocations` forensic ledger (secrets
redacted at the persistence boundary), and `cachemetrics` behind `aura cache-stats`.

## 9. Boundaries & invariants worth knowing

- The agent runtime never imports `agui` (one-way transport boundary, CI-enforced).
- `tools` declares consumer-side interfaces (`swarmRunner`, `taskStore`, `skillLoader`)
  so it never imports `swarm`/`cron`/`skills` — cycles are broken by ctx-injected seams.
- `messages[0]` is byte-identical across every turn and every swarm worker.
- Mutating tools are never retried (at-most-once side effects); only non-mutating
  transient failures retry.
- Secrets are redacted at every egress: logs, errors, the tool ledger, MCP child env,
  shell child env, MCP config export — all routed through the one `secret` denylist.
