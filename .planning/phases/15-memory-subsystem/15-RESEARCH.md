# Phase 15: Memory Subsystem - Research

**Researched:** 2026-06-11
**Domain:** MCP sidecar wiring (Go) for the adopted `neo4j-labs/agent-memory` fork — NOT a memory/RAG engine build
**Confidence:** HIGH (every owned-surface claim is grounded in repo code + the 4 VALIDATED spikes 032–035; package internals are trusted, not re-derived)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01 Agent-decides capture.** Aura invokes memory write-tools deliberately (Claude-Code parity). No passive every-turn extraction, no confirmation prompts.
- **D-02** Compose `--no-auto-preferences` stays (no background preference inference); preferences written only when the agent chooses.
- **D-03 Pull-on-demand recall.** Agent calls `memory_search`/read tools when it decides; nothing injected into the cached prefix (spike-035 path).
- **D-04 No proactive `messages[2]` insight injection.** KV-cache invariant holds trivially; the `messages[2]` AgentInsight-cache machinery + `AURA_AGENT_INSIGHT_CACHE_TTL_SEC` are NOT built this phase. Planner confirms the cache-invariant audit still passes unchanged.
- **D-05 Conversational/agent memory only.** Ship the package's native MCP surface. Document-RAG (file/URL → chunk → embed → entity) is deferred to a future phase.
- **D-06 Full 16-tool surface.** Agent gets read + all writes + reasoning-trace tools (`memory_start_trace`/`memory_record_step`/`memory_complete_trace`/`memory_get_observations`) + read-only `graph_query`.
- **D-07** Memory tools mount **Deferred** and **namespaced `memory__*`** (spikes 032/035). Reached via `tool_search`.
- **D-08 Phase-16 managed recipe, default-on (trusted), fail-soft.** Register agent-memory as a trusted managed MCP recipe inheriting the Phase-16 control plane (doctor/status/logs/policy/profiles) but mounts by default and fail-soft if the sidecar is down.
- **D-09 Fail-soft + reconnect-on-use** posture (no supervisor/ping). A down sidecar degrades to a structured, self-correctable result, never loop-fatal.
- **D-10 Single-user `local`, one global dedup scope.** All memory under identity `local`; the provenance-safe-dedup fork prevents cross-*run* over-merge; single persistent session = effectively one scope (intended same-user merge). No work/personal isolation. Multi-user is a future refactor.
- **D-11 384d `granite-embedding-97m-multilingual`** (`--embedding-dimensions 384`). (User flag if 768d wanted instead.)
- **D-12 PRD amendment #62 (doc-only) lands BEFORE any Go code** (PRD-first Wave-0). Records: UX-06 → deferred; UX-07 → already deferred; UX-08 → recall/latency become advisory snapshots vs the package's retrieval; UX-09 → agent-written reasoning/insight memories recalled on demand (no cached `messages[2]`, no journal cron).

### Claude's Discretion

- KV-cache invariant confirmation via `cache_invariant_audit.sh` (no user input).
- Reproducible compose `build:` replacing `:spike-fixed` (same shape as built sidecars).
- Upstreaming the `provenance-safe-dedup` fork as a PR (optional, post-merge).
- Exact `aura memory` CLI verb → `memory__*` tool mapping.
- How reasoning-trace memory is exercised/asserted in tests (package owns behavior; Aura asserts wiring + recall).

### Deferred Ideas (OUT OF SCOPE)

- Document-RAG ingestion (file/URL → markitdown → chunk → embed → entity pipeline) — own future phase.
- Proactive cached-insight injection (`messages[2]` + TTL cache) and a background agent-journal cron.
- Leiden community detection + summaries (UX-07 / 11c) — already deferred (amendment #27).
- 11f Task Canvas (amendment #25) — sequencing-independent, not this phase.
- Multi-user memory isolation — future scope refactor.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description (current REQUIREMENTS.md) | Research Support / Re-scope (PRD amendment #62, D-12) |
|----|--------------------------------------|--------------------------------------------------------|
| UX-06 | Memory ingestion documents → chunks → embeddings (Document → Chunk → Entity pipeline). | **DEFERRED** to a future phase. The agent-memory package is conversation/entity memory, not a chunked doc-RAG engine [VERIFIED: spike 031 finding #1–8, CONTEXT D-05]. Amendment #62 records the deferral; no Aura ingest pipeline this phase. |
| UX-07 | Entity resolution + GDS Leiden community detection + chaos test. | **Already deferred** (amendment #27); unchanged. Entity resolution is now the package's responsibility, live-proven by spike 034 (Mario Rossi chaos collapsed to one entity, `over_merge=false` ×2) [VERIFIED: spike 034]. GDS stays installed for ad-hoc PageRank/WCC. |
| UX-08 | GraphRAG hybrid retrieval, recall@5 ≥ 0.8, p95 ≤ 30ms, snapshot in `docs/aura-quality-snapshot.md`. | Recall/latency become **advisory snapshots** measured against the *package's* retrieval, not an Aura-owned WRRF gate. The `docs/aura-quality-snapshot.md` update gate (amendment #20) still applies as advisory. Hybrid retrieval is internal to the package. |
| UX-09 | Agent journal (`:AgentEpisode`/`:AgentInsight`) + cached `messages[2]` insight injection. | Becomes **agent-written reasoning/insight memories recalled on demand** (the package's reasoning-trace tools, D-06). NO cached `messages[2]` injection (D-04), NO background journal cron. |

**The four UX-06..09 requirement checkboxes must be re-stated by PRD amendment #62 before any Go code** (D-12; the established P8 #44 / P9 D-23 Wave-0 pattern). This amendment is the planner's first plan/wave.
</phase_requirements>

## Summary

Phase 15 is an **adoption, not a build**. The bespoke 11a/11b/11d/11e memory engine (~1850 LOC) is superseded by the forked `neo4j-labs/agent-memory` MCP sidecar (v0.5.0, branch `aura/provenance-safe-dedup`, HEAD commit `c1c2d65`), which serves short-term + long-term (POLE+O) + reasoning memory from one Neo4j graph over streamable-HTTP MCP — the exact shape Aura already mounts for `mcp-neo4j-cypher`/`sandbox-agent`. Four VALIDATED spikes (032 mount, 033 write/read, 034 dedup-chaos, 035 real `LlmAgent` recall loop) prove the entire integration path works against the live `aura-agent-memory-mcp:spike-fixed` image. The package is a **vendored dependency** (trusted via its upstream CI + 2443 tests + TCK); Aura's 85% coverage floor applies only to the Go wiring.

Aura's owned surface is a few hundred LOC of Go wiring plus two doc/compose changes: (a) a PRD amendment #62 (Wave 0, doc-only); (b) a `memory` catalog recipe in `internal/mcp/manager/catalog.go` made trusted, default-on, and fail-soft; (c) the 16-tool Deferred + `memory__*`-namespaced exposure (already proven — `MountManagedServer` does this with zero new code); (d) `cmd/aura/memory.go` mapping `aura memory` verbs to the raw `memory_*` MCP tools; (e) a reproducible compose `build:` Dockerfile replacing `image: ...:spike-fixed`; (f) confirmation that the KV-cache invariant audit still passes unchanged. The embedder (`aura-llama-embed`, granite-97m) already serves 384d natively, matching the compose `--embedding-dimensions 384` and Aura's already-384d `0001_init.cypher`.

The single genuine design gap the spikes did **not** close is **"default-on"**: existing recipes (calculator/calendar/mail/whatsapp) require an explicit `aura mcp install`, and `LoadManagedConfig` returns an empty registry when the file is absent — there is no built-in default profile that auto-mounts a recipe. The planner must design the default-on mechanism (seed-on-first-run vs. inject-unless-disabled). Everything else is proven.

**Primary recommendation:** Wave 0 = PRD amendment #62 (doc-only). Then wire the `memory` recipe into `BuiltInCatalog()` as a `streamable_http` / `trusted_recipe` server pointed at `http://127.0.0.1:${AURA_AGENT_MEMORY_MCP_PORT:-8091}/mcp/`, solve default-on at the config-load/`RunnableManagedServers` layer, add `cmd/aura/memory.go` reusing the `mcp.OpenServer` + `CallTool` pattern from `cmd/aura/mcp_tools.go`, replace the `:spike-fixed` image with a `docker/agent-memory/Dockerfile` modeled on `docker/markitdown/Dockerfile` + the fork's `deploy/cloudrun/Dockerfile`, and assert the wiring (unit) + live recall path (integration tier mounting the live sidecar) without re-measuring the package.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Memory storage (short/long/reasoning) | Vendored sidecar (Python `agent-memory` over MCP) | Neo4j (bolt) | Package owns the engine + its own Neo4j schema (`SchemaManager`); Aura does NOT touch the graph directly. |
| Entity extraction / dedup / POLE+O taxonomy | Vendored sidecar | — | Spikes 033/034 prove the package owns this; the fork added provenance-scoped dedup. |
| Embeddings | `aura-llama-embed` sidecar (granite-97m, 384d) | — | Existing Slice 0.7 sidecar; package points at `OPENAI_BASE_URL=http://aura-llama-embed:8081/v1`. |
| MCP transport (open/ping/close, tools/list, tools/call) | `internal/mcp` (`OpenServer`/`HTTPClient`) | — | Proven by spike 032; no new transport. |
| Tool bridging (Deferred, `memory__*` namespace, reconnect) | `internal/agent/mcptools` (`Mount`/`Bridge`/`MountManagedServer`) | — | Proven by spikes 032/035; zero new code for the bridge itself. |
| Recipe registration + trust + default-on + fail-soft mount | `internal/mcp/manager` (catalog) + `cmd/aura/main.go` boot loop | `internal/config` (`loadMCPServers`) | D-08; the Phase-16 control plane. Default-on is the new piece. |
| Operator CLI (`aura memory …`) | `cmd/aura/memory.go` (NEW) | `internal/mcp` (`OpenServer`+`CallTool`) | Direct MCP calls, bypassing the agent loop — same pattern as `cmd/aura/mcp_tools.go`. |
| Agent recall in the loop | `internal/agent` (`tools.Registry` + `tool_search` + `LlmAgent`) | mcptools bridge | Proven by spike 035 (`tool_search` → `memory__memory_search` → `text_response`). |
| KV-cache prefix invariant | `internal/agent/prompt` + `scripts/cache_invariant_audit.sh` | — | D-04: pull-on-demand never touches the prefix → invariant holds trivially; audit confirms unchanged. |

## Standard Stack

This phase **adds no Go libraries**. The "stack" is the already-shipped Aura wiring + the vendored Python sidecar.

### Core (already in the repo — reused, not installed)
| Component | Version / Location | Purpose | Why Standard |
|-----------|--------------------|---------|--------------|
| `neo4j-agent-memory` (forked) | v0.5.0, branch `aura/provenance-safe-dedup`, HEAD `c1c2d65` | The memory engine (3 layers, POLE+O, dedup) | Off-the-shelf adoption (amendment #61); 2443 tests + TCK; live-validated 032–035 [VERIFIED: D:/tmp/agent-memory git, pyproject.toml version=0.5.0]. |
| `internal/mcp` `OpenServer`/`HTTPClient` | repo | Streamable-HTTP MCP client | The exact client spike 032 used to reach `127.0.0.1:8091/mcp/` [VERIFIED: spike 032 main.go]. |
| `internal/agent/mcptools` `MountManagedServer`/`Bridge` | repo | Bridge MCP tools → `tools.Registry`, Deferred + `memory__*` | Whole mount path proven by spikes 032/035 [VERIFIED: mount.go, bridge.go]. |
| `internal/mcp/manager` `BuiltInCatalog()` | `internal/mcp/manager/catalog.go` | The recipe registry D-08 plugs into | This IS the recipe registry (CONTEXT noted it's "not under an obvious `recipe` symbol") [VERIFIED: catalog.go]. |
| `aura-llama-embed` sidecar | `ghcr.io/ggml-org/llama.cpp:server`, granite-97m-r2 Q8 | 384d embeddings | Slice 0.7 sidecar; memory points at it via `OPENAI_BASE_URL` [VERIFIED: compose.yaml:62-93]. |
| markitdown built-sidecar pattern | `docker/markitdown/Dockerfile` + `build:`/`image:` in compose | Template for the reproducible memory `build:` | Aura's canonical in-repo built-sidecar shape [VERIFIED: compose.yaml:278-281, docker/markitdown/Dockerfile]. |

### Supporting (the fork's runtime container deps)
| Component | Version | Purpose | When to Use |
|-----------|---------|---------|-------------|
| Python | 3.10+ (fork pins `python_version = "3.10"`; upstream Dockerfile uses 3.11-slim) | Runs the sidecar | In the reproducible `build:` image only [VERIFIED: pyproject.toml, deploy/cloudrun/Dockerfile]. |
| `neo4j-agent-memory[mcp,google,openai]` extras | install via `pip install -e ".[mcp,google,openai]"` | MCP server + OpenAI-compat embeddings/LLM provider | Exactly what the fork's `deploy/cloudrun/Dockerfile` installs [VERIFIED: deploy/cloudrun/Dockerfile]. |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Adopting the package | Building the bespoke 11a–11e engine | ~1850 LOC + ongoing maintenance vs. a vendored dep — rejected by amendment #61 ("no atomic bombs / minimal industrial shape"). |
| 384d granite | 768d (legacy PRD assumption) | 768d is stale: the live migration + `DefaultEmbedDimensions` are ALREADY 384d, and granite-97m emits 384d natively. 768d would require re-introducing a dimension mismatch. D-11 = 384d. |
| Reproducible `build:` in compose | Keep hand-built `:spike-fixed` image | `:spike-fixed` is not reproducible for packaging (Phase 17) — must become a Dockerfile build (D-08 discretion, node #4). |

**Installation:** No `npm`/`go get`. The only "install" is the container build (see Reproducible Compose Build below) and Python extras inside it.

**Version verification:** Fork version confirmed `0.5.0` at HEAD `c1c2d65` [VERIFIED: D:/tmp/agent-memory `git show` + `pyproject.toml`]. Embedder model dimension confirmed 384d [VERIFIED: HuggingFace ibm-granite/granite-embedding-97m-multilingual-r2 model card + arxiv 2605.13521].

## Package Legitimacy Audit

The phase installs **no new Go modules**. The single external dependency is the vendored Python package, already present locally as a fork.

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| `neo4j-agent-memory` (forked at `c1c2d65`) | PyPI (upstream) + local fork at `D:/tmp/agent-memory` | upstream Beta, active | n/a (vendored from a pinned fork commit, not a registry pull) | github.com/neo4j-labs/agent-memory (fork: `aura/provenance-safe-dedup`) | n/a (vendored fork, not registry-resolved) | **Approved — vendored from a known fork at a pinned commit.** Supply-chain trust comes from the pinned commit + the fork being the operator's own (`davide marchetto`), not from registry resolution. |

**Packages removed due to slopcheck [SLOP] verdict:** none.
**Packages flagged as suspicious [SUS]:** none.

**Notes for the planner:**
- The container `pip install -e ".[mcp,google,openai]"` will resolve transitive PyPI deps *inside the build*. The planner should pin the build to the fork commit (`git checkout c1c2d65` or a vendored copy) so the image is reproducible. The `aura-memory-mcp-placeholder-key` default for `NAM_LLM_API_KEY` keeps compose parseable in CI without a secret [VERIFIED: compose.yaml:116-119].
- slopcheck/registry verification is N/A here because the dependency is a **pinned fork commit**, not a registry name resolution. This is strictly safer than a registry pull.

## Architecture Patterns

### System Architecture Diagram

```
                          ┌─────────────────────────────────────────────┐
   user / agent           │                 aura (Go)                    │
       │                  │                                              │
       │  aura memory ───►│  cmd/aura/memory.go ──► mcp.OpenServer ──┐   │
       │  <verb>          │   (operator path, direct CallTool)        │   │
       │                  │                                           ▼   │
       │  aura chat ─────►│  LlmAgent loop ─► tool_search ─► memory__* │   │
       │  (agent recall)  │   tools.Registry ◄── mcptools.Mount       │   │
       │                  │     (Deferred, namespaced, reconnect)     │   │
       │                  │            ▲                              │   │
       │                  │   config.loadMCPServers ──► MCPPolicies   │   │
       │                  │   (RunnableManagedServers, fail-soft)     │   │
       └──────────────────┴───────────│──────────────────────────────│───┘
                                       │  streamable-HTTP MCP         │
                                       │  127.0.0.1:8091/mcp/         │
                          ┌────────────▼──────────────────────────────▼──┐
                          │   aura-agent-memory-mcp  (Python sidecar)     │
                          │   profile=extended, 16 tools, --user-id       │
                          │   aura-local, persistent session, 384d        │
                          │     │                          │              │
                          │     │ bolt://neo4j:7687        │ OpenAI-compat │
                          │     ▼                          ▼              │
                          │  ┌──────────┐          ┌────────────────┐     │
                          │  │  Neo4j   │          │ aura-llama-embed│     │
                          │  │ (POLE+O, │          │  granite-97m    │     │
                          │  │ reasoning│          │  384d /v1       │     │
                          │  │  traces) │          └────────────────┘     │
                          │  └──────────┘                                 │
                          └───────────────────────────────────────────────┘
```

Two entry paths into the same sidecar: the **operator CLI** (`aura memory`, direct MCP CallTool) and the **agent loop** (`tool_search` → deferred `memory__*` → MCP bridge). Both reach the same Neo4j-backed sidecar. The sidecar owns embeddings (via the embed sidecar) and the graph.

### Component Responsibilities

| File / Symbol | Responsibility | New or Reused |
|---------------|----------------|----------------|
| `prd.md` §Slice 11 + `REQUIREMENTS.md` UX-06..09 + `ROADMAP.md` P15 | PRD amendment #62 re-scope (Wave 0, doc-only) | Edit (doc) |
| `internal/mcp/manager/catalog.go` `BuiltInCatalog()` | Add `memory` `streamable_http` recipe, `trusted_recipe` | Edit |
| default-on mechanism (config-load or `RunnableManagedServers`) | Make `memory` mount without an explicit `aura mcp install` | **NEW (gap — see Open Questions)** |
| `cmd/aura/main.go` `buildRegistryWithMCP` | Fail-soft mount loop (`MountManagedServer` for managed) | Reused as-is (memory flows through `cfg.MCPPolicies`) |
| `cmd/aura/memory.go` | `aura memory <verb>` → raw `memory_*` MCP CallTool | **NEW** |
| `docker/agent-memory/Dockerfile` + compose `build:` | Reproducible image replacing `:spike-fixed` | **NEW** |
| `scripts/cache_invariant_audit.sh` (run, not edit) | Confirm KV-cache invariant unchanged | Run only |

### Pattern 1: Streamable-HTTP managed-server mount (proven)
**What:** A `streamable_http` `ManagedServer` flows config → `RunnableManagedServers` → `cfg.MCPPolicies` → `buildRegistryWithMCP` → `MountManagedServer` → `mcp.OpenServer` → `Mount`/`Bridge` (Deferred, `memory__*`). 
**When to use:** Exactly D-06/D-07/D-08.
**Example (the config that makes it managed + HTTP):**
```go
// Source: internal/mcp/manager/catalog.go (existing recipe shape) + internal/mcp/managed_config.go
mcp.ManagedServer{
    Type:   mcp.ServerTypeStreamableHTTP,           // "streamable_http"
    URL:    "http://127.0.0.1:8091/mcp/",           // AURA_AGENT_MEMORY_MCP_PORT
    Source: "recipe:memory",
    Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
}
// RunnableManagedServers keeps streamable-HTTP servers (runtime.go:59-65);
// loadMCPServers puts them in MCPPolicies (config.go:331-334);
// buildRegistryWithMCP mounts via MountManagedServer (main.go:194) -> mcp.OpenServer.
```

### Pattern 2: Operator CLI direct MCP call (reuse `mcp_tools.go`)
**What:** `aura memory <verb>` opens the sidecar over streamable-HTTP and calls the **raw** `memory_*` tool (no `memory__` namespace at the MCP wire layer — namespacing is only for the agent registry).
**Example:**
```go
// Source: cmd/aura/mcp_tools.go openAndListManagedMCPTools (the exact reuse pattern)
cli, err := mcp.OpenServer(ctx, "memory", server)   // server from cfg.MCPPolicies["memory"]
defer cli.Close()
text, err := cli.CallTool(ctx, "memory_search", map[string]any{"query": q, "session_id": sid})
```

### Pattern 3: Fail-soft + reconnect-on-use (proven, no new code)
**What:** `buildRegistryWithMCP` already WARN-and-drops a dead/misconfigured server and continues boot (main.go:186-201). The reconnecting server wrapper (`newReconnectingServer`) re-lists tools on reconnect. A down memory sidecar yields a structured tool error the agent self-corrects, never a loop-fatal error (D-09).
**When to use:** Default posture — no supervisor/ping (locked Phase 9 lifecycle).

### Anti-Patterns to Avoid
- **Re-introducing a `messages[2]` insight block / TTL cache.** D-04 forbids it this phase. The `cache_invariant_audit.sh` only audits `messages[0]`, `messages[1]`, and the skill manifest stream — there is no `messages[2]` stream, by design.
- **Writing to Neo4j directly from Go for memory.** The package owns its schema (`SchemaManager` creates `:Entity`/`:Preference`/`:Fact`/`ReasoningTrace` indexes, incl. the new `deduplication_scope` indexes). Aura's `internal/knowledge/migrations/0001_init.cypher` `:Chunk` index is for the *bespoke/superseded* path — do not couple memory to it.
- **`DenyRisk=write` policy filter for the agent mount.** Spike 032's README mentions a read-only variant, but the VALIDATED 032 run and D-06 mount **all 16 tools** (full surface). Do not block writes for the agent (operator may still expose a read-only `aura memory` subset if desired, but the agent gets everything).
- **An explicit `aura mcp install memory` as the only path.** D-08 requires default-on; an install-only recipe violates it.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| MCP tool bridging (Deferred, namespacing, 64B cap, collision hash, reconnect) | A memory-specific mount | `mcptools.MountManagedServer` | Already proven by spikes 032/035; zero new code [VERIFIED: mount.go, bridge.go, name.go]. |
| Streamable-HTTP MCP transport | A custom HTTP/JSON-RPC client | `mcp.OpenServer`/`HTTPClient` | The exact client spike 032 validated [VERIFIED: spike 032]. |
| Entity resolution / dedup / POLE+O extraction | An Aura entity merger | The package (fork) | Spike 034 proves it; the fork added provenance-safe scoping [VERIFIED: long_term.py `_deduplication_scope`]. |
| Hybrid retrieval (vector+BM25+graph) | An Aura WRRF re-ranker | The package's `memory_search` | UX-08 re-scoped to advisory; the package owns retrieval. |
| Container build | A bespoke runtime | `docker/agent-memory/Dockerfile` modeled on `docker/markitdown` + fork `deploy/cloudrun/Dockerfile` | Reproducible + matches Aura's built-sidecar convention. |

**Key insight:** The entire memory *engine* is a black box behind MCP. Aura's job is config + CLI + a Dockerfile + one audit run — anything more is re-building what amendment #61 deleted.

## Runtime State Inventory

> This is an adoption that changes a compose image + adds config. It is not a code rename, but it touches stored data and live-service config, so the inventory applies.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | Neo4j graph: agent-memory writes `:Entity`/`:Preference`/`:Fact`/`:ReasoningTrace`/message nodes under `--user-id aura-local`. The fork's `c1c2d65` adds a `deduplication_scope` property + 3 indexes (`entity_/preference_/fact_deduplication_scope_idx`) created by the package's `SchemaManager` on first run. | None by Aura — the package creates its own schema/indexes. Spike data tagged `AURA-SPIKE-03x-*` may linger in the dev Neo4j; harmless (distinct tags). The planner may note a clean-graph step for a fresh measurement. |
| Live service config | `aura-agent-memory-mcp` compose service config (image, command flags) lives in `compose.yaml` (in git). The `:spike-fixed` **image** is hand-built and NOT reproducible from git. | Replace `image: ...:spike-fixed` with a `build:` stanza + `docker/agent-memory/Dockerfile` (node #4). The managed MCP recipe will live in `~/.aura/mcp/servers.json` (NOT git) once installed/seeded — same as every other recipe. |
| OS-registered state | None — the sidecar is a compose container, no OS task/service registration. | None — verified: no Task Scheduler / launchd / pm2 entries for memory. |
| Secrets/env vars | `NEO4J_PASSWORD`, `OPENROUTER_API_KEY` (→ `NAM_LLM_API_KEY`), `AURA_LLM_MODEL`, `AURA_AGENT_MEMORY_MCP_PORT`, `AURA_AGENT_MEMORY_MCP_IMAGE`, `AURA_NEO4J_DATABASE`, `AURA_LLM_BASE_URL` — all already referenced in compose. No new secret. | None new. The planner should catalog `AURA_AGENT_MEMORY_MCP_*` in the PRD env index (amendment #62). |
| Build artifacts | The `aura-agent-memory-mcp:spike-fixed` Docker image (hand-built from `D:/tmp/agent-memory@c1c2d65`) is the only stale artifact — it won't rebuild from git. | The reproducible `build:` resolves this. After the change, `docker compose build aura-agent-memory-mcp` reproduces it from the pinned fork. |

**Canonical question answered:** After the compose `build:` lands and the recipe is registered, the only runtime state carrying the adoption is (1) the Neo4j graph the package self-manages, and (2) the `~/.aura/mcp/servers.json` recipe entry — both expected and self-correcting.

## Common Pitfalls

### Pitfall 1: "Default-on" assumed to be free
**What goes wrong:** Planner adds the `memory` catalog entry and assumes it mounts automatically. It does NOT — recipes require `aura mcp install` and `LoadManagedConfig` returns an empty registry on a fresh machine.
**Why it happens:** Existing recipes (calculator/calendar/mail/whatsapp) are opt-in install; there is no auto-include default profile [VERIFIED: catalog.go, mcp.go `mcpInstall`, managed_config.go `LoadManagedConfig`].
**How to avoid:** Design an explicit default-on mechanism (Open Question 1). Test it on an empty `AURA_MCP_CONFIG`.
**Warning signs:** A green unit test that asserts the catalog entry exists, but `aura chat` on a fresh machine shows `mounted=0` memory tools.

### Pitfall 2: Mounting all 16 vs. spike-035's `mounted=13 blocked=3`
**What goes wrong:** Copying spike 035's policy mount (which blocked 3 write/reasoning tools) instead of D-06's full surface.
**Why it happens:** Spike 032/035 explored a `DenyRisk=write` read-only posture; the README headline numbers (`mounted=13 blocked=3`) reflect that exploration, not D-06.
**How to avoid:** Mount via plain `MountManagedServer` (no deny policy) — spike 032's VALIDATED run mounted all 16, all Deferred [VERIFIED: spike 032 main.go, expects `len(mounted) == len(rawNames)`].
**Warning signs:** `aura mcp tools memory` or the agent manifest missing `memory__memory_add_entity` / `memory__memory_start_trace`.

### Pitfall 3: Provenance scope silently global
**What goes wrong:** Planner expects per-source dedup isolation but writes carry no scope key, so everything dedups in one global scope.
**Why it happens:** The fork's `_deduplication_scope` engages **only** if write metadata contains one of the `_PROVENANCE_SCOPE_KEYS` (`session_id`, `source_id`, `run_id`, `tenant_id`, `tag`, … 13 keys). With a single persistent session and no key, scope is `None` → historical global behavior (intended same-user merge — D-10) [VERIFIED: long_term.py:143-193].
**How to avoid:** This is **correct for D-10** (one user, intended merge). The planner must *consciously decide* whether agent writes pass `session_id` (spike-035 used a session). If future multi-user is wanted, pass a scope key — but that's deferred.
**Warning signs:** Two distinct entities expected to stay separate get merged at ~0.95 similarity (the original 033/034 failure mode — now fork-fixed for cross-*run*, but global within one scope by design).

### Pitfall 4: Stale SKILL.md version (0.1.1) vs. adopted 0.5.0
**What goes wrong:** Trusting `.claude/skills/neo4j-agent-memory-skill/SKILL.md` "Current version 0.1.1" and "16 tools = extended".
**Why it happens:** The skill is point-in-time; Aura adopted **v0.5.0** at fork HEAD `c1c2d65`.
**How to avoid:** Treat SKILL.md as authoritative for **profiles** (core=6, extended=16) and POLE+O, but verify the tool list against spike 032's live `tools/list` (the ground truth: 16 tools enumerated). The 6 core tools = `memory_search`, `memory_get_context`, `memory_store_message`, `memory_add_entity`, `memory_add_preference`, `memory_add_fact`.
**Warning signs:** Planning around a tool count or API that doesn't match the live `tools/list`.

### Pitfall 5: Rebuilding the image without pinning the fork commit
**What goes wrong:** `pip install neo4j-agent-memory[mcp]` from PyPI (not the fork) loses the provenance-safe-dedup fix → cross-run over-merge returns (spike 034 INVALIDATED state).
**Why it happens:** The fix (`c1c2d65`, +165 lines in `long_term.py`, +3 schema indexes) is **fork-internal**, not upstreamed yet.
**How to avoid:** The Dockerfile must `pip install -e .` from the **pinned fork commit** (vendored copy or `git checkout c1c2d65`), never the PyPI release.
**Warning signs:** A rebuilt image where `memory_add_entity` returns `deduplication: action=merge` across distinct runs (033 re-validation expects `action=none`, `similarity_score=0.0` for a genuinely new entity).

## Code Examples

### Live tool surface (ground truth from spike 032)
```
// Source: spike 032 README Investigation Trail — live tools/list returned 16 tools:
graph_query                 (read-only Cypher — no write-via-Cypher escape)
memory_add_entity           memory_add_fact            memory_add_preference
memory_complete_trace       memory_create_relationship memory_export_graph
memory_get_context          memory_get_conversation    memory_get_entity
memory_get_observations     memory_list_sessions       memory_record_step
memory_search               memory_start_trace         memory_store_message
// (spike 032 main.go also tolerantly expected memory_get_facts; the live surface = the 16 above)
```

### Provenance-safe-dedup mechanism (fork `c1c2d65`)
```python
# Source: D:/tmp/agent-memory@c1c2d65 src/neo4j_agent_memory/memory/long_term.py
_PROVENANCE_SCOPE_KEYS = ("tenant_id","workspace_id","user_identifier","user_id",
    "actor_id","session_id","source_id","source_url","document_id","run_id",
    "memory_scope","scope","tag")

def _deduplication_scope(metadata):       # stable JSON of any present scope keys, else None
    ...                                    # None => historical GLOBAL dedup (D-10 same-user merge)

def _node_matches_scope(data, scope):     # a scoped write only dedups against same-scope candidates
    if scope is None: return True
    return data.get("deduplication_scope") == scope or _deduplication_scope(_node_metadata(data)) == scope
# schema.py adds: entity_/preference_/fact_deduplication_scope_idx
```
**What the planner must assert:** with D-10 (one `--user-id aura-local`, persistent session), the intended same-user merge holds, AND cross-run over-merge does NOT (spike 033/034 re-validation: a genuinely new entity writes with `action=none, similarity_score=0.0`).

### Reproducible compose build (model)
```dockerfile
# Source pattern: docker/markitdown/Dockerfile + D:/tmp/agent-memory deploy/cloudrun/Dockerfile
FROM python:3.11-slim
RUN apt-get update && apt-get install -y --no-install-recommends gcc \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
# Vendor the fork at the pinned commit (c1c2d65) — NOT the PyPI release.
COPY pyproject.toml README.md README-pypi.md ./
COPY src/ ./src/
RUN pip install --no-cache-dir -e ".[mcp,google,openai]"
EXPOSE 8080
# compose `command:` already overrides with the extended/persistent/384d flags.
```
```yaml
# compose.yaml — replace image:...:spike-fixed with (markitdown shape):
aura-agent-memory-mcp:
  build:
    context: ./docker/agent-memory     # vendored fork or submodule pinned at c1c2d65
  image: ${AURA_AGENT_MEMORY_MCP_IMAGE:-aura-agent-memory-mcp:local}
  # ... existing environment/command/healthcheck unchanged ...
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Bespoke 11a/11b/11d/11e build (~1850 LOC) | Adopt forked `neo4j-labs/agent-memory` MCP sidecar | Amendment #61, 2026-06-08 | ~poche centinaia di LOC of Go wiring only. |
| 768d embeddings (PRD §11a Cypher hardcoded 768) | 384d granite-97m | D-11 / amendment #61 | The live `0001_init.cypher` + `DefaultEmbedDimensions` are ALREADY 384d — no migration drop needed (see Open Q2). |
| Cached `messages[2]` AgentInsight injection + TTL | Pull-on-demand reasoning/insight recall | D-04 | KV-cache invariant holds trivially; no `messages[2]` machinery. |
| Stock semantic dedup (over-merges at 0.95) | Provenance-scoped dedup (fork `c1c2d65`) | 2026-06-08 | Cross-run isolation; same-user merge preserved. |

**Deprecated/outdated:**
- The PRD §11a Cypher schema (Document/Chunk/Entity/Community/AgentEpisode/AgentInsight + 5 vector indexes, 768d): `[SUPERSEDED #61]` — the package owns its schema.
- The `DenyRisk=write` read-only mount explored in spike 032: superseded by D-06 full-surface mount.
- SKILL.md version "0.1.1": stale (adopted 0.5.0).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The `memory` recipe should be `streamable_http` `trusted_recipe` (matching the existing recipe trust convention + spike 032's `TrustRemoteHTTP` open). | Architecture Patterns / Standard Stack | Low — both routes reach `OpenServer`; trust class only affects policy display. Planner should pick `trusted_recipe` (D-08 says "trusted") over `remote_http`. |
| A2 | The reproducible image vendors the fork at `c1c2d65` (copy `src/` + `pyproject.toml`), not a git submodule. | Code Examples | Low — submodule also works; vendoring is simpler for Phase-17 packaging and avoids a network fetch at build. |
| A3 | `aura memory` operator verbs map to raw `memory_*` tools via direct `CallTool` (bypassing the agent loop), per `mcp_tools.go`. | Architecture Patterns Pattern 2 | Low — this is the established operator-CLI pattern; confirmed by `cmd/aura/mcp_tools.go`. |
| A4 | The dev Neo4j may carry `AURA-SPIKE-03x-*` residue; harmless for a fresh advisory snapshot but a clean run wants a scoped wipe. | Runtime State Inventory | Low — distinct tags; only matters for a pristine recall@5 measurement. |

**Note:** No `[ASSUMED]` claim affects a locked decision or a security/compliance control. All are low-risk planning conveniences.

## Open Questions

1. **Default-on mechanism (THE gap the spikes did not close).**
   - What we know: A catalog entry alone does NOT mount; recipes need `aura mcp install`; `LoadManagedConfig` returns empty on a fresh machine; `RunnableManagedServers` only mounts servers present in the active profile [VERIFIED: catalog.go, mcp.go, managed_config.go, runtime.go].
   - What's unclear: How "default-on" (D-08) is realized. Two viable designs: (a) **seed-on-first-run** — write the `memory` server into `~/.aura/mcp/servers.json`'s default profile when the file is created; or (b) **inject-unless-disabled** — special-case `memory` in `loadMCPServers`/`RunnableManagedServers` so it's always in `MCPPolicies` unless the operator explicitly `disable`s it.
   - Recommendation: Option (b) is more robust (survives a deleted config file, true "core capability → on out of the box") but touches a shared load path; Option (a) is lower-blast-radius but a fresh `rm servers.json` loses memory. The planner should pick one and gate it with an empty-`AURA_MCP_CONFIG` test asserting `memory__memory_search` is registered with no prior `install`.

2. **Embedding dimension / "legacy 768d migrations" — already resolved, confirm and document.**
   - What we know: The repo's ONLY Neo4j migration (`internal/knowledge/migrations/0001_init.cypher`) is **already 384d**; `DefaultEmbedDimensions = 384`; granite-97m-r2 emits 384d natively [VERIFIED: 0001_init.cypher:12, config.go:13, HF model card]. There is **no 768d migration `0001/0002`** in the repo — the D-11/PRD "768d legacy becomes dead" framing is stale.
   - What's unclear: Nothing material. The package manages its own vector indexes at its configured 384d; the bespoke `:Chunk` index in `0001_init.cypher` is for the superseded doc-RAG path.
   - Recommendation: PRD amendment #62 should record that 384d is already the live state (no drop/migration needed); optionally note the bespoke `:Chunk` index is now dormant (leave it — dropping it is out-of-scope churn).

3. **Does the agent pass a provenance scope key on memory writes?**
   - What we know: Scope engages only with a `_PROVENANCE_SCOPE_KEYS` value; spike 035 used a `session_id`. D-10 = one user, one global scope, intended merge.
   - What's unclear: Whether the agent's write-tool calls should carry `session_id` (per-conversation isolation) or write unscoped (fully global). Both are valid under D-10; the difference is whether two conversations' "Mario Rossi" merge (unscoped: yes; session-scoped: only within a conversation).
   - Recommendation: For single-user same-person merge (the D-10 intent), **unscoped writes** (or a stable `user_id=aura-local`) maximize cross-conversation entity unification. The planner should decide explicitly and assert the chosen behavior in the live-mount integration test.

4. **`memory_get_facts` presence.**
   - What we know: spike 032's `expected` list included `memory_get_facts`, but the live `tools/list` enumerated 16 *without* it; facts are read back via `graph_query` in spike 033.
   - Recommendation: Treat the live `tools/list` as ground truth (16 tools, no standalone `memory_get_facts`); the `aura memory` fact-read verb should use `graph_query` or `memory_search`, as spike 033 did. Re-confirm against the rebuilt image's `tools/list` in Wave 1.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Neo4j (compose `neo4j`) | sidecar bolt backend | ✓ | 5.x + APOC + GDS | — (hard dep) |
| `aura-llama-embed` sidecar | 384d embeddings | ✓ | granite-97m-r2 Q8 | — (hard dep; serves 384d natively) |
| Docker (compose) | building + running the sidecar | ✓ | Docker Desktop | — |
| `aura-agent-memory-mcp:spike-fixed` image | the running sidecar | ✓ (hand-built) | fork `c1c2d65` | **must become a reproducible `build:`** (node #4) |
| `D:/tmp/agent-memory` fork `@c1c2d65` | the reproducible build source | ✓ | v0.5.0, branch `aura/provenance-safe-dedup` | vendor `src/` into `docker/agent-memory/` |
| OpenRouter key (`OPENROUTER_API_KEY`) | sidecar's `NAM_LLM` extraction LLM | ✓ (placeholder default for CI) | — | `aura-memory-mcp-placeholder-key` keeps compose parseable; live extraction needs a real key |

**Missing dependencies with no fallback:** none — every runtime dependency is already up (the spikes ran live against this exact stack on 2026-06-08).
**Missing dependencies with fallback:** the reproducible `build:` (the only artifact gap) — fallback is vendoring the fork `src/`.

## Validation Architecture

> `nyquist_validation: true` in `.planning/config.json` — this section is REQUIRED.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` (stdlib) + table tests; build tags for tiers (`db_integration`, `neo4j_integration`; this phase adds a `memory_integration` tier) |
| Config file | none (Go convention); CI matrix env per CLAUDE.md (composed DSNs, `mcp-neo4j-cypher` on PATH) |
| Quick run command | `go test ./cmd/aura/ ./internal/agent/mcptools/ ./internal/mcp/manager/` |
| Full suite command | `go test -tags 'db_integration neo4j_integration memory_integration' ./...` (stack up) + `scripts/cache_invariant_audit.sh` |

### Owned-Surface Deliverable → Observable Validation Signal
| Deliverable | Validation signal (observable) | Test type | Automated command | Exists? |
|-------------|--------------------------------|-----------|-------------------|---------|
| PRD amendment #62 (Wave 0) | UX-06..09 checkboxes re-stated in `REQUIREMENTS.md`/`ROADMAP.md`/`prd.md`; amendment commit precedes any Go commit | doc gate | `git log` ordering + grep for amendment #62 marker | ❌ Wave 0 |
| `memory` catalog recipe | `aura mcp recipes` lists `memory` with `trusted_recipe` + `streamable_http` source | unit | `go test ./internal/mcp/manager/ -run Catalog` + CLI golden in `cmd/aura/mcp_test.go` | ❌ Wave 0 (extend `mcp_test.go`) |
| Default-on mount | On empty `AURA_MCP_CONFIG`, `memory__memory_search` is registered with NO prior `install` | unit | `go test ./cmd/aura/ -run MemoryDefaultOn` (set `AURA_MCP_CONFIG` to a temp empty path) | ❌ Wave 0 |
| 16-tool Deferred + `memory__*` exposure | mounted tool count == live `tools/list`; all Deferred; names `memory__*` | integration (live sidecar) | `go test -tags memory_integration ./internal/agent/mcptools/ -run MemoryLiveMount` (mirrors spike 032) | ❌ Wave 0 |
| Fail-soft when sidecar down | sidecar stopped → boot WARNs + continues; non-deferred built-ins keep registry valid; no fatal | unit | `go test ./cmd/aura/ -run BuildRegistryWithMCP_FailSoft` (point recipe URL at a dead port) | ❌ Wave 0 (extend existing fail-soft test) |
| `aura memory <verb>` CLI | each verb returns the seeded tagged content via direct `CallTool` | integration (live sidecar) | `go test -tags memory_integration ./cmd/aura/ -run MemoryCLI` (seed+read, mirrors spike 033) | ❌ Wave 0 |
| Agent recall path | `tool_search` → `memory__memory_search` → `text_response` returns the seeded tag | integration (live sidecar) | `go test -tags memory_integration ./internal/agent/ -run MemoryLoopRecall` (mirrors spike 035 with `agenttest.FakeClient`) | ❌ Wave 0 |
| Reasoning-trace memory wiring | a `memory_start_trace`/`memory_record_step`/`memory_complete_trace` round-trip is recallable via `memory_search`/`memory_get_observations` (Aura asserts WIRING + recall, not the package's trace semantics) | integration (live sidecar) | `go test -tags memory_integration ./cmd/aura/ -run MemoryReasoningTrace` | ❌ Wave 0 |
| Reproducible compose build | `docker compose build aura-agent-memory-mcp` succeeds from git; rebuilt image's `tools/list` == 16; dedup `action=none` for a new entity | smoke (operator) | `docker compose build aura-agent-memory-mcp` + the live-mount integration test against the rebuilt image | ❌ Wave 0 |
| KV-cache invariant unchanged | `scripts/cache_invariant_audit.sh` still prints 22 identical `messages[0]`/`messages[1]`/`skillman` hashes | audit-script pass | `scripts/cache_invariant_audit.sh` (Postgres-free) | ✅ exists — run, assert unchanged (no `messages[2]` stream added) |
| Recall@5 / p95 | advisory snapshot appended to `docs/aura-quality-snapshot.md` (amendment #20 gate) | advisory snapshot | manual/scripted measurement against the live sidecar; CI gate = snapshot file updated on the P15 PR | ❌ Wave 0 (advisory, not a hard pass/fail) |

### Sampling Rate
- **Per task commit:** `go test ./cmd/aura/ ./internal/agent/mcptools/ ./internal/mcp/manager/` (unit, sub-30s).
- **Per wave merge:** `go test -tags 'db_integration neo4j_integration memory_integration' ./...` with the stack up + `scripts/cache_invariant_audit.sh`.
- **Phase gate:** full tagged matrix green + owned-surface coverage ≥85% (Go wiring only; the vendored package is NOT measured) + the advisory snapshot appended + `cache_invariant_audit.sh` green before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `internal/agent/mcptools/` (or a new `memory_integration`-tagged test) — live-mount 16-tool assertion (port spike 032's harness into a `-tags memory_integration` test).
- [ ] `cmd/aura/memory_test.go` — `aura memory` verb→tool mapping (unit with a fake MCP transport) + live seed/read (integration, port spikes 033/035).
- [ ] `cmd/aura/mcp_test.go` — extend with the `memory` recipe golden + default-on assertion.
- [ ] `internal/agent/` — loop-recall integration test (port spike 035; reuse `agenttest.FakeClient`).
- [ ] `memory_integration` build tag + CI job env (sidecar URL `AURA_AGENT_MEMORY_MCP_URL`, stack up) wired to `t.Fatal` under `$CI` when unset (no-skip-as-green).
- [ ] `docs/aura-quality-snapshot.md` advisory section for memory recall@5/p95 (amendment #20 CI gate: file must be updated on the P15 PR).
- *(No framework install needed — Go stdlib testing is in place.)*

## Security Domain

> `security_enforcement` not explicitly `false` in config — included.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | Single-user `local`; sidecar is loopback-only (`127.0.0.1:8091`), no auth surface. |
| V3 Session Management | partial | `--session-strategy persistent --user-id aura-local`; sessions are memory-scope keys, not auth sessions. |
| V4 Access Control | yes | `graph_query` is **read-only** Cypher (spike 032 verified — no write-via-Cypher escape). Trust class `trusted_recipe`. Mount policy = full surface for the agent (D-06). |
| V5 Input Validation | yes | MCP args validated by the package's tool schemas; the bridge marshals model args through `json.Unmarshal` and tags results `TrustUntrusted` (`bridge.go newUntrustedResult`). |
| V6 Cryptography | no | No new crypto; the namespace hash suffix is explicitly NOT a security control (`name.go`). |
| V14 Config | yes | Loopback-only port binding (`127.0.0.1`), secrets via env (no values printed — D-15 Phase-16 posture), `servers.json` written `0o600`. |

### Known Threat Patterns for {Go MCP wiring + Python memory sidecar}
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Write-via-Cypher escape through `graph_query` | Tampering / Elevation | `graph_query` is read-only (spike 032 verified); do not expose a write-Cypher tool. |
| Untrusted MCP tool output treated as trusted | Tampering | Bridge tags every result `Trust: TrustUntrusted, Source: "mcp:<name>"` [VERIFIED: bridge.go]. |
| Dead/hostile sidecar stalls the agent loop | DoS | Fail-soft WARN-drop + reconnect-on-use + per-call timeout (`configuredMCPCallTimeout`) [VERIFIED: main.go, bridge.go]. |
| Secret leakage in MCP config/logs | Info disclosure | `servers.json` `0o600`; Phase-16 redaction (D-15/D-22); `NAM_LLM_API_KEY` placeholder default in CI. |
| Cross-run memory over-merge (provenance) | Integrity | Fork `c1c2d65` provenance-scoped dedup; assert `action=none` for a new entity (spike 033/034). |
| Manifest bloat (16 full schemas in every turn) | DoS (cost) | Deferred + `memory__*` namespacing; reached via `tool_search` (D-07). |

## Sources

### Primary (HIGH confidence)
- Repo code: `internal/mcp/manager/catalog.go`, `internal/mcp/manager/runtime.go`, `internal/mcp/managed_config.go`, `internal/config/config.go`, `cmd/aura/main.go`, `cmd/aura/mcp.go`, `cmd/aura/mcp_tools.go`, `internal/agent/mcptools/{mount,bridge,name}.go`, `internal/knowledge/migrations/0001_init.cypher`, `internal/knowledge/config.go`, `scripts/cache_invariant_audit.sh`, `docker/markitdown/Dockerfile`, `compose.yaml` — the owned-surface ground truth.
- Spikes (live, 2026-06-08): `.planning/spikes/032` (mount, 16 tools, all Deferred), `033` (write/read, fork-fix re-validation, `action=none`), `034` (dedup-chaos, `over_merge=false` ×2), `035` (real `LlmAgent` recall, `tool_search`→`memory__memory_search`→`text_response`).
- Fork source: `D:/tmp/agent-memory` @ `c1c2d65` (branch `aura/provenance-safe-dedup`, `pyproject.toml` version=0.5.0, `memory/long_term.py` `_deduplication_scope`, `graph/schema.py` dedup indexes, `deploy/cloudrun/Dockerfile`).
- CONTEXT.md / DISCUSSION-LOG.md (D-01..D-12), prd.md §Slice 11 Amendment #61, ROADMAP P15, REQUIREMENTS UX-06..09.

### Secondary (MEDIUM confidence)
- `.claude/skills/neo4j-agent-memory-skill/SKILL.md` — authoritative for profiles (core=6 / extended=16) + POLE+O; **version 0.1.1 is stale** (adopted 0.5.0).
- Phase-16 CONTEXT (`16-CONTEXT.md`) — recipe/trust/profile/doctor/policy control plane D-08 reuses.

### Tertiary (LOW confidence) — verified up to MEDIUM
- HuggingFace `ibm-granite/granite-embedding-97m-multilingual-r2` model card + arxiv 2605.13521 — 384d output dimension (cross-verified with the live compose flag + Aura's already-384d migration).

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — every component is in-repo and exercised by live spikes; no new library.
- Architecture / wiring: HIGH — the mount/bridge/recipe/config chain is read end-to-end in code and proven by spikes 032/035.
- Provenance/dedup: HIGH — read the actual fork diff (`c1c2d65`) and the `_deduplication_scope` logic.
- Embedding dimension: HIGH — live migration + model card + compose flag all agree on 384d.
- Default-on mechanism: MEDIUM — confirmed it is NOT free (a real gap); the two design options are sound but unimplemented (Open Q1).
- Pitfalls: HIGH — each is grounded in a specific spike finding or code path.

**Research date:** 2026-06-11
**Valid until:** 2026-07-11 (stable — vendored fork pinned at a commit; re-check only if the fork is upstreamed/rebased or the package version bumps)

## Sources (external links)
- [ibm-granite/granite-embedding-97m-multilingual-r2 (HuggingFace)](https://huggingface.co/ibm-granite/granite-embedding-97m-multilingual-r2) — 384d output dimension.
- [Granite Embedding Multilingual R2 (arxiv 2605.13521)](https://arxiv.org/abs/2605.13521) — model specifications.
- [neo4j-labs/agent-memory (GitHub)](https://github.com/neo4j-labs/agent-memory) — upstream of the adopted fork.
