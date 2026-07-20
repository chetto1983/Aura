# Aura — Architecture

**Module:** `github.com/chetto1983/aura` · **Language:** Go 1.26 · **Updated:** 2026-07-17

Aura is a local-first, multi-user-capable AI agent platform written in Go. One static
binary hosts the agent runtime, the tool surface, the policy enforcement point, the
transport channels (CLI, web cockpit, Telegram), and the persistence adapters; heavy or
untrusted work is pushed to sidecars, per-user containers, and MCP servers over
well-defined seams. This document is the narrative map. For the generated package-level
inventory see [`.planning/codebase/`](../.planning/codebase/) (regenerate with
`/gsd-map-codebase`); for the product framing see
[TECHNICAL_OVERVIEW.md](TECHNICAL_OVERVIEW.md) and [CAPABILITIES.md](CAPABILITIES.md).

## 1. Design principles

These recur throughout the code and explain most of the non-obvious decisions:

- **Local-first, N isolated identities.** Aura runs on hardware the operator controls,
  but a single deployment hosts multiple identities, isolated in depth: owner-scoped
  Postgres RLS as a backstop under the application's own scoping, per-identity
  object-store prefixes, and per-identity sandboxes. The seed `local` identity is now
  just the default owner, not the only one.
- **Posture is a config axis, not a constant.** `AURA_PROFILE` selects one of `dev`
  (the default) | `local_trusted` | `single_user_hardened` | `server_production`. The
  first two are lenient — filesystem and shell tools have full host access by design
  there (amendment #50/D-15c), and isolation is reserved for explicitly untrusted
  inputs. The latter two are `Strict()`: the same tools cross a policy decision and may
  be routed into a per-user container. **No claim about Aura's blast radius is true
  without naming the profile.**
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
- **Every consequential act leaves a durable fact.** Tool dispatch reserves a row in an
  append-only ledger *before* execution, so a crash mid-tool is reconcilable and a
  replay is detectable rather than re-executed.
- **Trust boundaries are explicit.** Anything the model didn't author — MCP results,
  web pages, document text — is wrapped/framed as untrusted before it re-enters the
  prompt (prompt-injection containment), and secrets are redacted at every egress.
- **Self-improvement without latency.** A reusable embedding-index substrate powers
  both reasoning-tier routing and tool discovery, and an async learner upgrades them
  off the hot path so accuracy rises without slowing any user turn.

## 2. Layered view

```text
┌──────────────────────────────────────────────────────────────────────────┐
│ Transport & UX        cmd/aura (CLI) · agui (SSE bridge + cockpit REST)   │
│                       webui (embedded React) · webauth (Authula) ·        │
│                       identityctx · channels (+telegram) · setup ·        │
│                       askuser · agentrender                               │
├──────────────────────────────────────────────────────────────────────────┤
│ Policy & posture      config (RuntimeProfile) · gateway (tool PEP) ·      │
│                       scoring (risk tiers) · breakglass                   │
├──────────────────────────────────────────────────────────────────────────┤
│ Agent runtime         agent (LlmAgent loop, Budget tree, Event model,     │
│                       hooks, trust, tracing, panicobs) · agent/workflow   │
│                       (Seq/Par/Loop) · swarm (fan-out) · agent/prompt     │
│                       (builder + reasoning) · agent/display               │
├──────────────────────────────────────────────────────────────────────────┤
│ Tools & MCP           agent/tools (registry, deferred pattern,            │
│                       tool_search, fs/shell/web/skill/task/doc/swarm/     │
│                       send_file …) · sandbox/usersandbox (per-user Docker │
│                       routing) · agent/mcptools (bridge) · mcp (client) · │
│                       mcp/manager (recipes, trust, audit)                 │
├──────────────────────────────────────────────────────────────────────────┤
│ Intelligence subst.   llm (+openai_compat) · semindex (embed-index core)  │
│                       · activelearn · reasoning{fifo,learn,store,trace}   │
│                       · toolselect{learn,store} · multimodal · rerank     │
├──────────────────────────────────────────────────────────────────────────┤
│ Capabilities          web · skills (+skilladapters) · cron (+handlers)    │
│                       · onboarding · documents · assets · settings ·      │
│                       eval · runner                                       │
├──────────────────────────────────────────────────────────────────────────┤
│ Persistence           db (+sqlc, Postgres) · knowledge (Neo4j) ·          │
│                       neostore · conversations (+cl100k) · objectstore    │
│                       (+garageadmin) · identity · profile · secret        │
├──────────────────────────────────────────────────────────────────────────┤
│ Observability         obs · agent/panicobs · reasoningtrace ·             │
│                       toolinvocations · cachemetrics · (OTel spans +      │
│                       Prometheus/expvar)                                  │
└──────────────────────────────────────────────────────────────────────────┘
        Sidecars (out of process): embedding (granite-311m, 768d, GPU via
        llama.cpp), markitdown extractor, OCR/STT/TTS, reranker,
        mcp-neo4j-cypher, MCP recipe servers (calculator/calendar/whatsapp/
        memory), per-user Docker sandboxes, Postgres, Neo4j.
```

This diagram is **not exhaustive**: `internal/` currently holds 68 packages, and the
leaf utilities (`boundedbuffer`, `canonicaljson`, `envutil`, `pgnumeric`) plus the
test-support packages (`agent/agenttest`) are deliberately omitted — they carry no
architectural weight. Anything else absent here is a gap in this document, not a
statement that the package doesn't matter. The authoritative package list is the tree
itself (`go list ./internal/...`), mirrored into [`.planning/codebase/`](../.planning/codebase/)
by `/gsd-map-codebase`; this document is the story, not the index.

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
`[]ChildReport`. v1 is deliberately flat (workers cannot spawn workers). Worker framing
rides in `messages[1]` (`swarm/brief.go`), never in `messages[0]` — that is how the
KV-cache invariant survives fan-out (D-06).

### Turn lifecycle (one user message → one answer)

```text
user msg ─▶ Runner loads managed history (L1/L2/L2.5 context ladder)
         ─▶ PromptBuilder.Build: messages[0] (stable) + history + volatile <budget> tail
         ─▶ adaptiveReasoningTier: local granite-embedding classifier → none|low|high
         ─▶ LlmAgent.Run loop:
              Budget.ConsumeStep (gate) ─▶ stream open (breaker+retry)
                 ─▶ consume chunks (text / reasoning / tool-call deltas)
                 ─▶ dispatch tool calls:
                      • BeforeTool hooks + dedup gate (serial, in order)
                      • gateway.Decide (classify → profile policy)
                          ─▶ Deny    → ErrDenied, tool never executes
                          ─▶ Approve → approval-required result, action withheld
                          ─▶ Allow   → Reserve in ledger (idempotency key)
                      • execute runnable calls (concurrent, semaphore-bounded)
                      • each result: cap→preview→sidecar, wrap-if-untrusted, append
                 ─▶ terminal? text_response → completion-gate critic → final Event
         ─▶ persist assistant turn (+ cache metric) atomically; emit Events to channel(s)
```

## 4. Policy, posture & isolation

Three packages decide *whether* a tool call happens and *where* it lands. They are the
newest load-bearing layer and the one most often missing from older mental models.

**Runtime profiles** (`internal/config/config_runtimeprofile.go`) define the posture.
`RuntimeProfile` is a string enum — `ProfileDev` | `ProfileLocalTrusted` |
`ProfileSingleUserHardened` | `ProfileServerProduction` — read from `AURA_PROFILE`.
`ParseProfile` is **total**: any unknown or empty value resolves to `ProfileDev`, never
panics and never errors (D-03), which means a typo'd env var degrades to the *lenient*
posture. `Strict()` is true for `single_user_hardened` and `server_production` and is
the single predicate the rest of the codebase branches on.

**The ToolGateway** (`internal/gateway`) is the Policy Enforcement Point interposed on
every tool dispatch. `New(profile config.RuntimeProfile, store reservationStore)` binds
a posture to an append-only ledger; the runner injects it into every per-turn agent, and
a nil Gateway degrades to an Allow no-op. Its vocabulary is `Decision`, `Verdict`,
`ReservationKey`, and `ErrDenied`. The split is deliberate (D-02d):

- `classify.go` — maps a call to a risk tier, delegating to `internal/scoring`.
- `decide.go` — the PEP proper: the per-profile enforcement branch.
- `approve.go` / `approvals.go` — responder-presence routing (is there a human to ask?).
- `reserve.go` — the pre-execution reservation. `Reserve`'s rows-affected count is the
  **GATE-04 idempotency key**: `rows==1` acquire, `rows==0` replay (fetch the prior end
  fact via `GetEnd`), error → deny.
- `reconcile.go` — closes orphaned starts left by a crash mid-tool.

`gateway.Decide` is interposed at the **top** of `execTool`, before the retry loop
(`llm_agent_retry.go`, GATE-01), so every non-`ask_user` dispatch crosses exactly one
policy decision before `tool.Execute`. A Deny returns `*gateway.ErrDenied` and the tool
never runs. An Approve returns an approval-required `ToolResult` as a **normal result**
(no error, `Execute` not called, the mutating action withheld) — so the real tool call
and args are still persisted and the model sees why it was stopped.

**Per-user sandboxes** (`internal/sandbox/usersandbox`) are where strict profiles put
the operator tool surface. `SandboxRouter` translates host tool calls (`shell_exec`,
`fs_*`) into in-box execs against a per-identity Docker container: `spec.go` and
`translate.go` build the container spec and rewrite paths, `materialize.go` stages files
in, `egress.go` applies the network policy, `router_tools.go` exposes `Exec`,
`ExecStream`, `WriteFile`, and `CopyArtifactOut` (streaming artifacts back out), and
`reap.go` plus the `sandbox_reap` scheduler kind (migration `0034`) reclaim idle boxes.

> **Coverage caveat — read this before trusting the sandbox.** The container-touching
> runtime here is `//go:build docker_integration`, and **there is no `docker_integration`
> job in CI**. The coverage gate runs `db_integration neo4j_integration` only, so those
> tests *compile and skip* in CI and contribute **zero** coverage: the DockerBackend
> lifecycle, exec, and egress paths are effectively untested by the pipeline. This is not
> hypothetical — it is why the CAP_NET_ADMIN capability-assertion bug (WR-01) stayed
> latent. Daemon-free unit tests for the pure logic (spec/tar builders, path-traversal and
> symlink guards, nil/disabled early returns) are the only part of this package the gate
> actually measures, and adding daemon-gated code without them silently drops the
> owned-surface aggregate below its floor.

## 5. Tools & MCP

`internal/agent/tools` is the largest surface. A `Registry` maps tool name → `Tool`;
the agent renders an alphabetical manifest each turn (cache-stable). The **deferred-tool
pattern** keeps that manifest small: tools with heavy specs set `Deferred=true` (13 specs
today) and appear only as name + one-line summary until the model calls `tool_search`,
which ranks them with a semantic embedding index (`semindex.Ranker`) plus a guarded BM25
tiebreak. Every large tool result is capped to a preview and spilled to a
per-conversation sidecar file, paged back via `read_tool_output`.

Built-in tools span the full operator surface: filesystem (`fs_read/write/edit/grep/glob`),
the keystone `shell_exec` (full host terminal under lenient profiles, sandbox-routed under
strict ones, with background jobs via `shell_poll`/`shell_kill`), web (`web_search` over
SearXNG, `web_fetch` SSRF-hardened), knowledge (`document_search`), orchestration
(`swarm_spawn`), self-extension (`skill`), scheduling (`task`), HITL (`ask_user`), working
memory (`todo_write`), artifact delivery (`send_file`), and
`text_response`/`current_time`/`read_tool_output`. Every one of them crosses the gateway
(§4) before executing.

**MCP** is the extension path for third-party capabilities. `internal/mcp` is a generic
JSON-RPC client (stdio + Streamable-HTTP); `internal/agent/mcptools` bridges any MCP
server's tools into the registry — namespaced `<server>__<tool>` so they can't shadow
built-ins, trust-framed as untrusted data, schema-capped, and **deferred by default**
(the `memory` server is the exception, kept visible). `internal/mcp/manager` owns the
durable managed-server registry, trust classification, docker/local launch resolution,
an audit store, and a curated recipe catalog: **calculator, calendar, whatsapp, memory**.
The standalone `mail` recipe was retired once the forked calendar-mcp became the unified
PIM sidecar — its send/search email tools subsume mail-mcp.

## 6. Intelligence substrate

- **`llm` + `llm/openai_compat`** — the provider-neutral streaming contract and a
  hand-rolled OpenAI-compatible SSE client (no SDK: byte-level framing, tool-call delta
  accumulation, idle watchdog, ctx-cancel teardown, bounded error capture). Default model
  is `deepseek/deepseek-v4-flash:nitro` over OpenRouter; cost is read from the provider
  when present and falls back to a price table (never a fabricated `$0`).
- **`semindex`** — Aura's single reusable embedding-index core: a lock-free cosine/
  centroid/margin math layer plus two wrappers, `Classifier` (centroid argmax + top-2
  margin) and `Ranker` (top-K cosine). It powers **both** reasoning-tier routing and
  `tool_search`. Brute-force over small immutable banks, no ANN.
- **Adaptive reasoning router** (`agent/prompt` + `reasoning*`) — instead of a per-turn
  LLM round-trip to decide reasoning effort (the original latency root cause), a local
  granite-embedding classifier maps the turn to `none|low|high` with a single local
  embed + cosine argmax; on abstention it falls back to the LLM "oracle" router. The design
  target recorded in `reasoning_classifier.go` is ~10 ms CPU at 90% accuracy over a
  60-prompt held-out set. An async learner (`reasoninglearn` → `activelearn` →
  `reasoningstore` in Neo4j) labels the uncertain turns off the hot path so the classifier
  converges toward oracle accuracy with no added latency.
- **`activelearn`** — the label-agnostic async self-improvement mechanism (bounded queue,
  content-hash dedup, margin gate, drop-on-full, goleak-clean). Two consumers: reasoning
  routing and tool selection (`toolselectlearn` → `toolselectstore`), the latter teaching
  `tool_search`'s per-tool centroids from confirmed routings via a free-ranker/DeepSeek
  two-tier oracle.
- **`multimodal` + `rerank`** — the sidecar clients for vision/STT/TTS and for the
  llama.cpp `/v1/rerank` reranker used to re-order retrieval hits.
- **`scoring`** — pure Risk-Based governance: maps scheduler tasks, skill mutations, and
  gateway classifications to a `Safe|Normal|Risky|Destructive` tier.

## 7. Persistence

Two stores, each behind a thin per-domain adapter (`Store{q}`) over generated sqlc /
Cypher, with SQLSTATE-classified errors and pgtype boundary conversion:

- **Postgres** (`internal/db`, `internal/db/sqlc`) — pgxpool + golang-migrate, a
  two-role split (`aura_app` runtime vs `aura_migrate` DDL), the `aura.*` schema, and a
  `WithTx` atomic-write seam. **40 migrations (0001–0040).** Domains include:
  conversations + turns (+ FTS + branches), identity + capabilities + audit + recovery +
  soft-delete, Authula's schema, paused states (HITL), scheduler + agent-job runs, skill
  audit, MCP audit, telegram accounts + setup tokens, tool-invocation ledger, cache
  metrics, context-rot events, document-ingest jobs + control plane, assets + content
  parts, object-store bindings, the saga journal,
  and knowledge-migration audit. DSNs are redacted in every error.
- **Multi-user isolation** — migration `0032` enables owner-scoped **row-level security**
  on identity-owned tables. `db.WithIdentityTx` sets the `app.current_identity` GUC for
  the transaction; the policy is fail-closed-on-mismatch and permissive-on-unset
  (D-06/D-07), so a scoped read sees only its owner's rows *even if the application
  forgets its own `*ForIdentity` clause* — RLS is the backstop, not the primary control.
  It is `ENABLE`, not `FORCE`, because `aura_migrate` owns the tables and must still
  bypass for backfills, while `aura_app` is a non-owner, non-superuser, non-BYPASSRLS
  role. Both halves of that assumption are asserted live (`TestAuraAppLacksRLSBypass`,
  `TestRLSBackstop`).
- **Neo4j** (`internal/knowledge`, `internal/neostore`) — the LLM-facing runtime interface
  is the `mcp-neo4j-cypher` subprocess over stdio (no native driver for data ops); a
  separate driver-backed `SchemaExecutor` runs the DDL the MCP layer can't. **HNSW 768-d
  vector index** + fulltext, Cypher migrations audited in Postgres. Holds the document
  graph and the self-improvement example stores (`:ReasoningExample`,
  `:ToolSelectionExample`).
- **`objectstore`** — the S3/filesystem blob seam with per-identity prefixes
  (`identity_store.go`) and a Garage admin client for provisioning.

**Conversations** (`internal/conversations`) layers the context-management ladder on top
of Postgres. The ladder is three deterministic tiers (Amendment #21; `context.go`, no LLM
call). The dark Phase-42 durable L2.4 compaction engine was removed (Amendment #86); the
anti-rot core is L4 extractive graph memory (Neo4j), not transcript compaction:

- **L1 microcompact** — rewrite old tool turns to `read_tool_output` pointers.
- **L2 budget gate** — the hard token cap (`ContextWindow − max(MaxOutputTokens, 20000) − 13000`).
- **L2.5 oldest-pair drop** — when L1 alone cannot bring the history under the L2 cap, drop
  the oldest user/assistant pairs (protecting the system turn + the messages[1] always-block)
  until it fits, writing one `context_rot_events` row.

Each tier writes a context-rot event. Around the ladder sit an offline tiktoken estimator
(`cl100k`), an atomic per-turn append (turn + aggregates + cache metric in one tx),
branch-aware history (`store_branch.go`, migration `0017`), identity-scoped reads
(`store_identity.go`), best-effort auto-titling, and boot/periodic sidecar GC.

**Documents** (`internal/documents`) is the ingestion pipeline: a markitdown sidecar
extracts PDF/xlsx/DOCX to chunks, Neo4j holds the sparse FTS + (async-embedded) vectors,
and `document_search` returns cited chunks. `internal/assets` handles the upload side —
image/audio/document processors and content parts. **Identity / profile / secret** hold
the identities + capability grants, the per-identity `Agent.md` profile (atomic writes),
and the one shared secret-env denylist used at every redaction site.

## 8. Transport & UX

Three surfaces, in rough order of how much traffic they carry:

- **The web cockpit** — `internal/webui` embeds the built Vite/React `dist/`, and
  `internal/agui` serves it. `agui` is no longer just a translator: alongside the AG-UI
  SSE bridge it is the cockpit's REST surface — conversations and branches, documents,
  assets, approvals, governance (read + write, incl. skills), audit, graph, onboarding
  and provisioning, connect (WhatsApp / PIM), password reset, and
  deprovision. The event bridge itself is still a pure function over Aura's
  `iter.Seq2[*Event, error]` stream (property/golden-testable), and the runtime still
  never imports agui.
- **Auth** — `internal/webauth` wraps **Authula** (`github.com/Authula/authula v1.15.0`)
  as the identity provider: cookie sessions, capability-per-route, identity linking, and
  session validation. `internal/identityctx` carries the resolved identity down to the
  RLS boundary; `internal/breakglass` is the audited escape hatch.
- **`channels` + `channels/telegram`** — a `Channel` interface + registry, with Telegram
  as the mobile/remote channel: AG-UI-event renderer, HITL via inline keyboards /
  force-reply, multimodal photo/voice/document handlers, a live status pane, and the
  `/start <token>` onboarding match.
- **`cmd/aura`** — the CLI (`chat`, `serve`, `mcp`, `skills`, `identity`, `doctor`, …),
  with **`askuser`** as the CLI responder rendering `ask_user` pauses and **`setup`** as
  the loopback setup wizard (HTTP + QR) for pairing a Telegram bot.

## 9. Observability

Every layer is instrumented: OTel spans (`agent.turn` → `llm.request` → `tool.execute`,
crypto-random span ids, api-keys never stamped), dual Prometheus + expvar metrics
(budget steps, tool dispatch, stream open/retry, turn outcomes, token/cost, panics),
the bounded-cardinality `agent/panicobs` recovered-panic counters, the env-gated redacting
`reasoningtrace`, the `toolinvocations` forensic ledger (secrets redacted at the
persistence boundary), and `cachemetrics` behind `aura cache-stats`.

The ledger is **append-only, with one deliberate exception**: a trigger rejects standalone
`DELETE`/`UPDATE` and `TRUNCATE`, but migration `0016` permits the `ON DELETE CASCADE`
teardown of a deleted parent conversation. 0011 had rejected *every* delete, which made
`/clear` fail for any conversation that had ever called a tool — the two guarantees were
mutually exclusive and anti-tampering won only for standalone mutation. The ledger's
second role is as the gateway's reservation store (§4).

## 10. Boundaries & invariants worth knowing

- The agent runtime never imports `agui`, and `webui` depends on no other internal
  package. Both are CI-enforced by `scripts/agui_boundary_check.sh`.
- `tools` declares consumer-side interfaces (`swarmRunner`, `taskStore`, `skillLoader`)
  so it never imports `swarm`/`cron`/`skills` — cycles are broken by ctx-injected seams.
- `messages[0]` is byte-identical across every turn and every swarm worker: worker framing
  rides in `messages[1]` (`swarm/brief.go`, D-06), and the property is test-asserted
  (`TestBuildPrefixStable`, `TestCacheAuditSourceListMessages0Stable`,
  `TestBudgetBlockByteStable`).
- Mutating tools are never retried (at-most-once side effects); only non-mutating
  transient failures retry, up to 3 total attempts with linear backoff
  (`llm_agent_retry.go`). The Mutating bit is resolved *before* execution so a tool that
  panics after a side effect is still classified correctly (F-031).
  **Known gap:** `skill`, `task`, and `swarm_spawn` are not flagged `Mutating` despite
  having side effects — a Phase-35 classification-hardening gap called out in
  `llm_agent_dispatch.go`. Code that must be safe therefore treats the flag as
  untrustworthy: a terminal `text_response` mixed with *any* runnable sibling is rejected
  wholesale rather than filtered by classification.
- Every tool dispatch crosses exactly one gateway decision before `tool.Execute`
  (GATE-01), and an allowed call holds a ledger reservation before it runs (GATE-04).
- Secrets are redacted at every egress: logs, errors, the tool ledger, MCP child env,
  shell child env, MCP config export — all routed through the one `secret` denylist.
- The owned-surface coverage floor is ≥85%, CI-enforced across the
  `db_integration neo4j_integration` matrix. Code reachable only under other build tags
  (above all `docker_integration`) counts as **uncovered** — see the §4 caveat.
