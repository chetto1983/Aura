# Phase 15: Memory Subsystem - Context

**Gathered:** 2026-06-11
**Status:** Ready for planning

<domain>
## Phase Boundary

Aura gains **long-term memory**: she remembers facts, entities, preferences, past
conversations, and her own reasoning across sessions, and recalls them later — for
both the *user's* world and her *own* agent journal.

**Implementation is locked to adoption, not a build** (Amendment #61, live-validated by
spikes 031–035 on 2026-06-08): adopt the forked `neo4j-labs/agent-memory` MCP sidecar
**off-the-shelf** (v0.5.0, branch `aura/provenance-safe-dedup`). It serves short-term
(messages), long-term (POLE+O entities/prefs/facts), and reasoning (decision/tool traces)
memory from one Neo4j graph over streamable-HTTP MCP — the same shape as
`mcp-neo4j-cypher`/`sandbox-agent`. The bespoke 11a/11b/11d/11e build (~1850 LOC) is
**superseded**.

**Aura's owned surface this phase** (Go, under project gates): MCP sidecar wiring
(mount / fail-soft / reconnect / policy), the embedder hookup, the `aura memory` operator
commands, the reproducible compose `build:`, and KV-cache reconciliation — a few hundred
LOC. The vendored package is **not** re-measured under the 85% coverage floor; the floor
applies to the Go wiring.

**In scope:** conversational + agent (reasoning) memory via the package's native MCP
surface; agent-driven write + recall; default-on managed mount; `aura memory` CLI.
**Out of scope this phase:** document-RAG ingestion (file/URL → chunk → embed pipeline),
proactive cached-insight injection + background journal cron, Leiden community detection,
11f Task Canvas, multi-user isolation. (See Deferred Ideas.)

</domain>

<decisions>
## Implementation Decisions

### Memory capture (what gets remembered, when)
- **D-01:** **Agent-decides.** Aura invokes memory write-tools deliberately when she
  judges something worth remembering — exactly how Claude Code uses its own memory.
  **No** passive every-turn LLM extraction, **no** confirmation prompts/ceremony.
  Rationale: standing operator directive (Claude-Code parity / no ceremony / full
  terminal). Lowest cost and write-churn; the agent owns the judgment.
- **D-02:** The compose flag `--no-auto-preferences` stays consistent with D-01 (no
  automatic background preference inference); preferences are written only when the agent
  chooses to via the write tools.

### Memory recall (how memory reaches the model)
- **D-03:** **Pull-on-demand.** The agent calls `memory_search` (and the other read
  tools) when it decides it needs context — the validated spike-035 path. Nothing is
  injected into the cached prompt prefix.
- **D-04:** **No proactive `messages[2]` insight injection.** Consequence: the KV-cache
  invariant (amendment #11/#29) holds *trivially* — pull-via-tool never touches the
  cacheable prefix, so the `messages[2]` AgentInsight-cache machinery and its TTL
  (`AURA_AGENT_INSIGHT_CACHE_TTL_SEC`) are NOT built this phase. Planner confirms the
  cache-invariant audit still passes unchanged.

### Ingestion scope (what feeds memory)
- **D-05:** **Conversational/agent memory only.** Phase 15 ships the package's native MCP
  surface (messages, entities, prefs, facts, reasoning traces + recall). Document-RAG
  (file/URL → chunk → embed → entity pipeline) is **deferred to a future phase** — the
  agent-memory package is conversation/entity memory, not a chunked doc-RAG engine, and
  "no atomic bombs / minimal industrial shape" applies.

### Tool exposure
- **D-06:** **Full 16-tool surface.** The agent gets read + all writes + the
  reasoning-trace memory tools (`memory_start_trace`/`memory_record_step`/
  `memory_complete_trace`/`memory_get_observations`) — the package's differentiator vs
  mem0 — plus read-only `graph_query` (spike-032 confirmed `graph_query` is read-only
  Cypher, so no write-via-Cypher escape). Max capability; the agent decides what to use.
- **D-07:** Memory tools mount **Deferred** and **namespaced `memory__*`** (proven in
  spikes 032/035; required so the default manifest doesn't carry all 16 full schemas —
  the agent reaches them via `tool_search`).

### Mount / governance
- **D-08:** **Phase-16 managed recipe, default-on (trusted).** Register agent-memory as a
  trusted managed MCP recipe so it inherits the Phase-16 control plane (doctor / status /
  logs / policy / profiles) but mounts **by default** and **fail-soft** if the sidecar is
  down. Memory is a core capability → on out of the box. Reuses the shipped manager rather
  than a one-off bespoke mount.
- **D-09:** Fail-soft + reconnect-on-use posture (the established MCP lifecycle: no
  supervisor/ping loop). A down sidecar must degrade to a structured, self-correctable
  result, never a loop-fatal error.

### Dedup / provenance (carried from prior decisions, recorded for the planner)
- **D-10:** **Single-user `local`, one global dedup scope.** All memory lives under
  identity `local`; the provenance-safe-dedup fork prevents cross-*run* over-merge, but
  with a single persistent session there is effectively one scope (intended same-user
  entity merge — e.g. "Mario Rossi" across two conversations resolves to one entity). No
  work/personal isolation. Multi-user isolation is a future refactor (accepted pre-merge).

### Embedding dimension (Claude's recommendation, accepted by default)
- **D-11:** **384d `granite-embedding-97m-multilingual`** — what the validated compose
  service runs (`--embedding-dimensions 384`). The legacy 768d migrations `0001/0002`
  become dead with this amendment. (User did not object; flag if 768d is wanted instead.)

### Requirements re-scope → PRD amendment #62 (planner's FIRST plan)
- **D-12:** A doc-only **PRD amendment #62** lands *before* any Go code (PRD-first
  principle; the established Wave-0 pattern for superseding phases). It records:
  - **UX-06** (document ingestion) → **deferred** to a future phase.
  - **UX-07** (Leiden communities) → already deferred (amendment #27); unchanged.
  - **UX-08** (hybrid retrieval recall@5 ≥ 0.8 / p95 ≤ 30ms) → recall/latency become
    **advisory snapshots** measured against the *package's* retrieval, not an Aura-owned
    WRRF gate. The `docs/aura-quality-snapshot.md` update gate (amendment #20) still applies
    as an advisory snapshot.
  - **UX-09** (agent journal + cached insight injection) → becomes **agent-written
    reasoning/insight memories recalled on demand**; no cached `messages[2]` injection, no
    background journal cron.

### Claude's Discretion
- KV-cache invariant confirmation (expected to hold trivially under D-04) — researcher/
  planner verifies via `cache_invariant_audit.sh`, no user input needed.
- Reproducible compose `build:` for the memory image (replace the hand-built
  `:spike-fixed` with a Dockerfile that `pip install`s from the fork at the fix commit) —
  same shape as `aura-sandbox-agent`.
- Upstreaming the `provenance-safe-dedup` fork as a PR — process, optional, post-merge.
- Exact mapping of `aura memory` CLI verbs to the underlying `memory__*` MCP tools.
- How "reasoning-trace memory" is exercised/asserted in tests (the package owns the
  behavior; Aura asserts the wiring + recall path, per spike 035).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The adoption decision + open plan-time nodes (READ FIRST)
- `prd.md` §Slice 11 **Amendment #61** (≈ line 3662) — the off-the-shelf adoption, what it
  supersedes, the 5 open plan-time nodes (KV-cache, embedding dim, provenance-key,
  reproducible build, fork divergence), and the owned-surface definition.
- `prd.md` §Slice 11 **"Decisioni cumulate"** table (≈ lines 3691–3702) — taxonomy (POLE+O),
  3-tier storage, entity extraction, retrieval, privacy isolation, community-deferral,
  memify, agent-memory cached injection (the last now superseded by D-04).
- `.planning/ROADMAP.md` **Phase 15** (lines 510–526) — goal, depends-on, the bespoke
  Success Criteria retained as design reference (re-derived by D-12).
- `.planning/REQUIREMENTS.md` **UX-06..UX-09** (lines 51–54) — the requirements being
  re-scoped by PRD amendment #62 (D-12).

### Live validation evidence (the spikes this phase rests on)
- `.planning/spikes/031-phase15-memory-source-audit/README.md` — source audit, patterns,
  landmines (PARTIAL by design).
- `.planning/spikes/032-agent-memory-mcp-live-mount/README.md` — VALIDATED mount via
  Aura's streamable-HTTP bridge; 16 tools; `DenyRisk=write` policy filter; deferred
  `memory__*`.
- `.planning/spikes/033-agent-memory-write-read-ground-truth/README.md` — VALIDATED
  write/read; long-term isolation fix history (fork commit `c1c2d65`).
- `.planning/spikes/034-agent-memory-dedup-chaos/README.md` — VALIDATED dedup-chaos;
  cross-run over-merge fix + the standing provenance-scope caveat (D-10).
- `.planning/spikes/035-agent-memory-loop-recall/README.md` — VALIDATED real `LlmAgent`
  loop: `tool_search` → `memory__memory_search` → `text_response` on the real bridge
  (`mounted=13 blocked=3`).

### Wiring surface Aura reuses
- `compose.yaml` — `aura-agent-memory-mcp` service (≈ lines 95–160): `127.0.0.1:8091/mcp/`,
  `NAM_BACKEND=bolt`, `--profile extended --session-strategy persistent --user-id
  aura-local --embedding-dimensions 384 --no-auto-preferences`; image
  `aura-agent-memory-mcp:spike-fixed` (needs a reproducible `build:`). `aura-llama-embed`
  embedder sidecar (≈ line 62).
- `internal/mcp/` — `OpenHTTP`/`HTTPClient` (streamable-HTTP client used by spike 032),
  `Open` (stdio); `ServerConfig`/`HTTPConfig`.
- `internal/agent/mcptools/` — `Mount`/`Bridge` (namespacing, 64B cap, collision-hash;
  Deferred flip), reconnect-on-use behavior (`TestReconnectServer*`), `MountManagedServer`
  (managed-policy mount used in the spikes).
- `cmd/aura/mcp.go` + `mcp_profile.go` + `mcp_status.go` + `mcp_tools.go` — the Phase-16
  manager CLI surface a `memory` recipe plugs into.
- `.planning/phases/16-add-richer-recipes-doctor-checks-for-whatsapp-and-calendar-e/16-CONTEXT.md`
  — Phase-16 recipes/profiles/trust/policy/doctor/status/logs design (D-08 reuses this).

### Package reference
- `.claude/skills/neo4j-agent-memory-skill/SKILL.md` — authoritative reference for the
  `neo4j-agent-memory` package (POLE+O, MCP tools, profiles, settings).
- `D:/tmp/agent-memory` (branch `aura/provenance-safe-dedup`) — the forked source the
  `:spike-fixed` image was built from (fix commit `c1c2d65`).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`internal/mcp` streamable-HTTP client** (`OpenHTTP`/`HTTPClient`): exactly the client
  spike 032 used to reach `127.0.0.1:8091/mcp/`. No new transport needed.
- **`internal/agent/mcptools`** (`Mount`/`Bridge`/`MountManagedServer` + reconnect): bridges
  MCP tools into `tools.Registry` as Deferred, namespaced (`memory__*`), with reconnect-on-use
  — the whole mount path is already proven by spikes 032/035.
- **Phase-16 MCP manager** (`cmd/aura/mcp*.go` + managed config/recipes/trust/policy/doctor):
  D-08 registers memory as a trusted, default-on recipe inside this control plane.
- **`aura-llama-embed` sidecar**: the embedder the memory service points at
  (`OPENAI_BASE_URL=http://aura-llama-embed:8081/v1`); no separate embedder to build.
- **`tools.Registry` + `WithToolCallContext` + `tool_search`**: the loop dispatch + deferred
  discovery path the agent uses to find and call `memory__*` (spike 035).

### Established Patterns
- **Deferred-tool + `tool_search`**: memory's 16 tools must mount Deferred/namespaced or the
  default manifest bloats (08.1 hardening already in place).
- **Fail-soft MCP boot + reconnect-on-use, no supervisor/ping** (mail/whatsapp lifecycle):
  D-09 follows this; a down sidecar yields a self-correctable structured result.
- **Managed recipe registration** (Phase 16): trust + risk-policy + profile assignment.
- **PRD-first Wave-0 doc amendment** for superseding phases (e.g. P8 #44, P9 D-23): D-12's
  amendment #62 lands before any Go code.

### Integration Points
- `compose.yaml` already carries the `aura-agent-memory-mcp` service → needs a reproducible
  `build:` (replace `:spike-fixed`).
- New `cmd/aura/memory.go` (`aura memory …`) wraps the `memory__*` tools for the operator.
- Recipe/managed-config registration point in the Phase-16 manager (planner locates the exact
  recipe registry — not under an obvious `recipe` symbol in `internal/`).

</code_context>

<specifics>
## Specific Ideas

- **Claude-Code parity is the north star** for memory UX: Aura should treat memory the way
  Claude Code treats its file-based memory — explicit, agent-judged writes; on-demand recall;
  no ceremony, no background magic. (Operator directives: "full terminal like you",
  "no ceremony", "no atomic bombs / minimal industrial shape".)
- Trust the package's upstream CI + TCK (2443 tests / 9 workflows) for the memory engine
  itself; Aura's gates apply to the Go wiring only.

</specifics>

<deferred>
## Deferred Ideas

- **Document-RAG ingestion** (file/URL → markitdown → chunk → embed → entity pipeline,
  the original UX-06): its own future phase. The agent-memory package isn't a chunked
  doc-RAG engine, so this is net-new owned surface — deliberately out of Phase 15.
- **Proactive cached-insight injection** (`messages[2]` AgentInsight block + TTL cache,
  amendment #11) and a **background agent-journal cron** (post-conv episode summaries +
  cross-conv insight extraction): revisit only if pull-on-demand (D-03) proves
  insufficient in practice.
- **Leiden community detection + community summaries** (UX-07 / 11c): already deferred
  (amendment #27); GDS stays installed for ad-hoc PageRank/WCC/Node-Similarity.
- **11f Task Canvas** (ephemeral symbolic working-memory, amendment #25): sequencing-
  independent, not part of this phase.
- **Multi-user memory isolation**: requires a scope refactor; future work (D-10).

</deferred>

---

*Phase: 15-memory-subsystem*
*Context gathered: 2026-06-11*
