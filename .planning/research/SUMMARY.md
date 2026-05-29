# Project Research Summary — Aura

**Project:** Aura — Go-native agentic AI substrate (personal AI runtime, tabula-rasa rewrite)
**Domain:** Single-binary agentic loop + Docker Compose sidecars, mini-PC self-hosted, Telegram-primary v1
**Researched:** 2026-05-29
**Confidence:** HIGH

> The four researchers operated in parallel against the same PRD (4400 LOC, locked `b3faacbf` 2026-05-27) and codebase map (2721 LOC). The PRD's 14-slice scope is **broadly validated by all four streams**. This SUMMARY surfaces (a) PRD drift requiring amendment BEFORE Slice 0.5 starts, (b) feature gaps small enough to add to v1 without inflating scope, (c) per-slice quality gates the roadmapper must wire into Gate 1 DoR / Gate 3 DoD, (d) architectural invariants that span slices (KV cache prefix, ctx-cancel, audit immutability, embedding dim contract).

## Executive Summary

Aura is a substrate agentic Go-native runtime — domain-neutral, single-binary, multi-channel — with "personal AI on Telegram" as one configurable overlay rather than the essence. The 2025-2026 industry has converged on a **5-layer architecture** (Channels → AG-UI transport → Runtime with unified `Agent` interface → Capabilities → semantic-strict three-store Persistence) and Aura's PRD maps onto that consensus 1:1. The stack picks (Go 1.23+, Postgres 17/pgx/sqlc/golang-migrate, Neo4j 5.x + APOC + GDS + HNSW + `mcp-neo4j-cypher` MCP, llama.cpp embedding sidecar with `embeddinggemma-300m` 768d, openai-compat handrolled wire client targeting DeepSeek-V4 via OpenRouter, AG-UI Go SDK, telebot.v4, Docker sandbox with seccomp+ulimit, SearXNG) are current, idiomatic, minimal-dep, and spike-validated.

The differentiators that emerge: (1) **KV cache stable-prefix discipline** (Slice 4) — byte-identical `messages[0]` produces 80% cache savings / −63% cost on DeepSeek-V4, but must be defended across **every** slice that mutates message construction (1, 1.8, 4, 5, 7e, 10, 11e), not just Slice 4; (2) **Skills as executable code snippets with TTL** (Slice 7e) is the most novel feature in scope (Voyager NeurIPS 2023 + Mem0 procedural memory); (3) **Multi-pause FIFO HITL across nested swarm children** (Slice 1.5 + 3) — rare composition, known liveness risk; (4) **Knowledge graph + vector hybrid retrieval via `mcp-neo4j-cypher`** (Slice 11) — Microsoft GraphRAG + Cognee, spike-validated 15-45× faster. **Anti-features in PROJECT.md Out of Scope are correct and must stay locked**: no marketplace, no multi-user auth/RBAC, no TTS, no native Windows, no native Go Neo4j driver, no embedded UI.

The risks warranting explicit pre-Slice-0.5 PRD amendments: **6 drift flags from Stack** (Neo4j CalVer ambiguity, go-readability deprecation 2025-12-05, telegramify supply-chain risk, **Go 1.23 insufficient for AG-UI SDK requiring 1.24.4+**, untagged telebot.v4, untagged AG-UI Go SDK), **4 feature gaps from Features** (conversation FTS, cost visibility commands, OTel hooks, setup wizard one-time token), **1 architectural spec gap** (`:AgentInsight` retrieval must be cached N minutes so `messages[2]` cache-stable — breaks Slice 4 invariant if added naively in Slice 11e), **3 cross-cutting Pitfalls** that must be wired into Gate 1 DoR / Gate 3 DoD: audit-table TRUNCATE bypass (#6), embedding-dim contract (#7), KV cache cross-slice invariant test (#3). Overall confidence HIGH — the PRD reflects serious engineering investment validated by spikes and a 4-agent convergence loop on 2026-05-28; these items are refinements, not redirections.

## Key Findings

### Recommended Stack (8/13 pass clean; 5 need amendment)

- **Go 1.25+** (PRD says 1.23+, **must bump** — AG-UI SDK requires 1.24.4)
- **Postgres 17-alpine + pgx v5.9.2 + sqlc v1.31.1 + golang-migrate v4.19.1** — clean
- **Neo4j `5.26-community` LTS** (PRD ambiguous "5.x" post-CalVer; **must pin**) + APOC + GDS plugins
- **`mcp-neo4j-cypher` v0.6.0** (Apache 2.0, FastMCP `<3.x` lock) — uniform MCP discipline
- **OpenAI-compat handrolled ~280 LOC** targeting DeepSeek-V4 via OpenRouter — official `openai/openai-go v3.37.0` is over-spec
- **EmbeddingGemma 300m GGUF + llama.cpp server `b9401+`** 768d native, ~600 MB / 4 thread
- **AG-UI Go SDK community** (pseudo-version, **pin SHA from 2026-05-14+**)
- **`gopkg.in/telebot.v4`** (untagged, **pin SHA post-2026-05-08**) — alt: `go-telegram/bot` semver-tagged
- **html-to-markdown v2.5.1** + **`codeberg.org/readeck/go-readability/v2`** (PRD's `go-shiori/go-readability` deprecated 2025-12-05, **must migrate**)
- **Custom ~80 LOC MarkdownV2 escaper** (PRD's `eekstunt/telegramify-markdown-go` 4-star supply-chain risk — **promote port to default** OR pivot to HTML mode)
- **SearXNG** container (AGPL-3.0 safe as IPC), **`go.uber.org/goleak` v1.3.0** mandatory in TestMain

**Anti-stack:** LangChain Go, GORM/Ent, generic agent frameworks imported, `lib/pq`, `bytedance/sonic`, `spf13/viper`, `logrus`/`zap`/`zerolog`, WhisperX, Qdrant/Milvus/Weaviate, wiki markdown filesystem.

### Expected Features

**Must have (table stakes — PRD covers all):** Multi-turn streaming + cancel; tool calling with ToolResult; multi-thread conversations Claude.ai-style; long-term memory; file/doc ingestion; web search+fetch; code sandbox; `Agent.md` persona; HITL `ask_user`; multimodal voice+image; Telegram channel; first-run wizard; sensible defaults; stop button.

**Should have (differentiators — Aura's competitive position):** Skill snippets with Voyager pattern (7e); KV cache stable-prefix discipline (4); GraphRAG hybrid retrieval (11); Risk-tier governance + audit trigger; multi-pause FIFO HITL across swarm children (1.5+3); self-extending writeable skills (7c); multi-channel framework (9); domain-neutral substrate; MCP + SKILL.md format; agent journal (11e); proactive scheduler (6); CPU-first mini-PC deployment.

**Add to v1 (4 gaps):** Conversation FTS (~80 LOC + GIN index, Slice 1.8.5); cost visibility commands (~30 LOC, 9b); OTel hooks (~150 LOC, 1 + 0.9); setup wizard one-time token (~20 LOC, 9a).

**Defer/scope-reduce within v1:** Slice 3 swarm → minimal ParallelAgent + 2-deep cap; Slice 7e → snippet save/execute only (auto-suggest analyzer → 7f / v1.x); Slice 7 `skill.catalog` → hide behind opt-in; Slice 13 → v2 (already PROJECT.md).

### Architecture Approach

**5-layer system, Slice 0.9 is the cornerstone.** Skipping 0.9 forces re-architecture of every downstream slice. Layers: (1) Channels (`internal/channels/`), (2) AG-UI transport (`internal/agui/` pure-function translator + fanout; **`internal/agent` MUST NOT import `internal/agui`** — most important boundary), (3) Runtime (`internal/agent/` with `Agent` interface yielding `iter.Seq2[*Event, error]`, ADK-go shape ~380 LOC stolen-not-imported), (4) Capabilities (sandbox, web, skills, cron, memory, conversations, identity, llm/prompt; **deferred-tool pattern mandatory** for big tools per Anthropic `advanced-tool-use-2025-11-20`), (5) Persistence (Postgres app state only + Neo4j knowledge+vectors only + filesystem artifacts only — **no crossover**).

**8 patterns:** Unified `Agent` interface; deferred-tool loading; KV cache stable-prefix; ToolResult preview+sidecar; multi-channel fanout pure-stream; pause/resume via sentinel error; 3-store persistence; workflow agents composition (saves ~680 LOC).

**8 coupling risks (enforce via static analysis):** Tools must NOT import channels; agent MUST NOT import agui; only LlmAgent.runTool writes conversation_turns; identity check at tool dispatch level; **microcompact must NOT invalidate KV cache** + **spec gap: `:AgentInsight` retrieval must be cached N minutes so `messages[2]` cache-stable** (breaks Slice 4 if added naively in 11e); sandbox session per-conversation (per-identity is v2); MCP-neo4j-cypher subprocess lifecycle (health check + restart loop, fail as RUN_ERROR not silent); Telegram + AG-UI HTTP both writing conversation (FK + `SELECT FOR UPDATE`).

### Critical Pitfalls (Top 5 cross-cutting — must wire into multi-slice Gate 1/3 checklists)

1. **Sandbox escape via permissive seccomp (Slice 2a, P0)** — PRD's "default-deny + 7 named blocks" is deny-list disguise. Run **SandboxEscapeBench** (UK AISI March 2026), document escape rate < 5%; ratify as **positive allowlist** ~80 syscalls; add `ptrace`/`unshare`/`/proc/self/root` test cases.
2. **KV cache poisoning across slices (cross-cutting, P0)** — Slice 4 owns invariant but Slices 1.8, 5, 7e, 10, 11e all mutate prefix. CI job `scripts/cache_invariant_audit.sh` 20-turn replay asserts SHA-256(`messages[0]`) constant. Architectural rule: **two system messages** (`messages[0]` cache-stable, `messages[1]` mutable with N-minute cached `:AgentInsight`).
3. **Audit log immutability bypass via TRUNCATE/DROP (Slice 0.5 + 7c, P0)** — Postgres `BEFORE UPDATE OR DELETE` does NOT fire for TRUNCATE/DROP. Role separation: `aura_app` (INSERT, SELECT only) + `aura_migrate` gated by `AURA_DB_MIGRATE_URL` + `BEFORE TRUNCATE` trigger.
4. **Embedding model swap silent corruption (Slice 0.7 + 11a + 11b, P0)** — Industry hits `neo4j#13387`, `langchain#16336`. Env contract `AURA_EMBED_DIMENSIONS=768`; sidecar boot asserts `model.output_dim == env`; runbook "no in-place upgrade".
5. **Infinite tool-call loop (Slice 0.9 + 1 + 3 + 6, P0)** — 3 orthogonal caps: `MAX_STEPS=25`, `MAX_WALLCLOCK_SEC=300`, `DEDUP_WINDOW=3`; per-child budget inherits parent's **remaining** budget (not fresh — otherwise swarm depth=3 × 25 each = 15625 steps).

**Honorable mentions (P0/P1 hitting specific slices):** Workspace symlink escape (2b), skill injection via Unicode NFKC (7a/c), SSRF IPv6+DNS rebinding (5), secrets leak conversation_turns (1.8+9b), ctx cancel propagation (cross-cutting), multi-pause swarm deadlock (1.5+3), preview truncation poisoning (1), sidecar boot order race (0.5+0.7+9c), cron `FOR UPDATE SKIP LOCKED` race (6), entity resolution race (11a+11b), HNSW `M=32` not default 16 (11a+11d), Telegram MarkdownV2 inject (9b — prefer HTML mode).

## PRD Amendments Required BEFORE Slice 0.5 Starts (20 items, ~120 LOC PRD edits)

Single commit `prd: pre-implementation drift fixes from independent research convergence` before any code commit.

| # | Amendment | Source | PRD Location |
|---|-----------|--------|--------------|
| 1 | Bump Go minimum to 1.25 (AG-UI requires 1.24.4) | Stack P1 | Slice 0.9 + CLAUDE.md |
| 2 | Pin `neo4j:5.26-community` LTS (or 2026.05 rolling) | Stack P1 | Slice 0.7 + compose.yaml |
| 3 | Migrate to `codeberg.org/readeck/go-readability/v2` | Stack P1 | Slice 5 |
| 4 | Promote custom ~80 LOC MarkdownV2 escaper to default (or HTML mode) | Stack P2 | Slice 9b |
| 5 | Pin telebot.v4 commit SHA post-2026-05-08 | Stack P3 | Slice 9b |
| 6 | Pin AG-UI Go SDK commit SHA post-2026-05-14 | Stack P3 | Slice 8 |
| 7 | Add conversation FTS sub-slice 1.8.5 (`pg_trgm` GIN + `aura chat search`) | Features gap | Slice 1.8 extension |
| 8 | Add `/cost` Telegram + `aura chat cost` CLI | Features gap | Slice 9b |
| 9 | Add OTel hooks (request_id UUIDv7 in InvocationContext) | Features gap + Pitfall #23 | Slice 0.9 + 1 cross-cutting |
| 10 | Add setup wizard one-time token `AURA_SETUP_TOKEN` | Features gap | Slice 9a |
| 11 | Specify `:AgentInsight` retrieval caching N minutes | Architecture spec gap (Risk #5) | Slice 11 |
| 12 | Reduce Slice 3 swarm v1 to ParallelAgent + 2-deep cap | Features over-scope | Slice 3 |
| 13 | Split Slice 7e → 7e-core (v1) + 7f (v1.x auto-suggest) | Features over-scope | Slice 7e |
| 14 | Hide Slice 7 `skill.catalog` behind opt-in | Features over-scope | Slice 7d |
| 15 | Extend `goleak.VerifyNone` mandate to all goroutine packages | Pitfall #10 | PRD §1455 |
| 16 | Add CI job `cache_invariant_audit.sh` cross-slice | Pitfall #3 | New §Cross-cutting |
| 17 | Add audit-table role separation `aura_app` vs `aura_migrate` + TRUNCATE trigger | Pitfall #6 | Slice 0.5 + 7c |
| 18 | Add embedding dim env contract + boot assertion + runbook | Pitfall #7 | Slice 0.7 + 11a + 11b |
| 19 | Add agent loop budget contract (3 env vars + child budget inheritance) | Pitfall #9 | Slice 0.9 + 1 + 3 + 6 |
| 20 | Establish `docs/aura-quality-snapshot.md` living doc + CI gate | Pitfall #27 + memory `feedback_aura_as_product` | New + Gate 1/3 |

## Implications for Roadmap

**Confirmed v1 scope:** PROJECT.md "Active" requirements unchanged at slice level. 4 feature gaps land inside existing slices (PRD Amendments #7-10). Slice 13 stays out (GPU-gated, v2). Slice 3 + 7e get **internal scope reductions** (#12-13) — same slice number, less surface for v1.

### Phase Structure (16 phases — architecture-validated dependency chain)

| Phase | Name | Slices | Rationale | Avoids |
|-------|------|--------|-----------|--------|
| **P0** | PRD Amendments | (no code) | Single commit applying 20 amendments | All Stack drift + Architecture spec gaps + cross-cutting Pitfalls before code |
| **P1** | Infra DB + Knowledge | 0.5, 0.7 | Persistence first; independent — can parallelize post-amendment | Pitfalls #6, #7, #13, #17, #24 |
| **P2** | Agent Cornerstone | 0.9 | Every later slice implements this interface — skipping forces re-architecture | Pitfalls #9, #10, #20, #23 |
| **P3** | LLM Client + ToolResult | 1 | Every LLM call + preview-spill pattern used by all later capabilities | Pitfalls #10, #12, #29; Anti-Pattern #3 |
| **P4** | HITL + Identity + Conversations | 1.5, 1.7, 1.8 (+1.8.5 FTS) | Tight cluster: ask_user, identity FK source, conversation persistence | Pitfalls #8, #11, #19; Risk #3, #5 |
| **P5** | Sandbox 2a Stateless | 2a | Unblocks `execute`. **Pitfall #1 is highest-stakes P0** | **Pitfall #1 (sandbox escape)** + #13 |
| **P6** | KV Cache Builder | 4 | **Deliberately near-late** — must come AFTER stable system prompt is real | **Pitfall #3** (KV cache cross-slice); Risk #5; Anti-Pattern #3 |
| **P7** | Web Tools | 5 | Standalone useful immediately post-Slice 4 | **Pitfall #5** (SSRF IPv6 + DNS pin) |
| **P8** | Sandbox 2b Session-Bound | 2b | Builds on 2a + workspace + network allowlist; needed by 7e | **Pitfall #2** (workspace symlink) + #1 re-audit |
| **P9** | Swarm (Minimal) | 3 | Reuses ParallelAgent; scope-reduced per Amendment #12 | Pitfall #11, #9; Anti-Pattern #5 |
| **P10** | Scheduler | 6 | Needs 1.7, 1.8, 0.9 | Pitfall #14 (advisory lock + heartbeat) |
| **P11** | Skills | 7a, 7b, 7c, 7d, 7e-core | 5-sub-slice atomic; 7e-core only (auto-suggest → 7f v1.x) | **Pitfall #4** (NFKC), #6 (TRUNCATE trigger), #30 |
| **P12** | AG-UI Gateway | 8 | Thin wrapper over in-process emitter | Pitfall #28; Risk #2 |
| **P13** | Channels + Telegram + Multimodal | 9a, 9b, 9c | Telegram primary channel v1 | Pitfalls #18, #25, #13 |
| **P14** | Onboarding + Agent.md | 10 | Needs 0.9, 1.5, 1.7; Agent.md at `messages[1]` not `[0]` | Pitfall #3 (Agent.md placement) |
| **P15** | Memory Subsystem | 11a, 11b, 11c, 11d, 11e | Most downstream — needs every other slice | **Pitfalls #7, #15, #16**, #20, #3 |

### Phase Ordering Rationale

- **Architecture-validated dependency chain**: Persistence (0.5/0.7) → Cornerstone (0.9) → LLM client (1) → Cluster (1.5/1.7/1.8) → Sandbox 2a → KV cache 4 (deliberately near-late) → Web 5 → Sandbox 2b → Swarm 3 → Scheduler 6 → Skills 7 → Transport 8 → Channels 9 → Onboarding 10 → Memory 11.
- **Risk-front-loaded**: P0 sandbox escape (#1) attacked at P5; P0 SSRF (#5) at P7; P0 KV cache invariant (#3) anchored at P6 then defended cross-slice; P0 audit immutability (#6) anchored at P1 (role separation) then enforced at P11 (TRUNCATE trigger); P0 embedding dim (#7) anchored at P1 (env contract) then enforced at P15.
- **Capabilities (5, 6, 7) deferred AFTER KV cache (4)**: every capability mutates message construction; building them before invariant is enforced risks each capability planting its own poisoning site.
- **Memory (P15) last**: most downstream + pre-merge benchmarks (recall@5 / nDCG@10 / p95 @ 1K/10K/100K) gate via `docs/aura-quality-snapshot.md` living doc.

### Research Flags

**Phases likely needing `/gsd-plan-phase --research-phase`:**
- **P5 (Sandbox 2a)** — SandboxEscapeBench + 2026 escape vectors + gVisor/Firecracker fallback. Highest-stakes P0, low margin.
- **P6 (KV Cache)** — provider-specific `cache_control` semantics across DeepSeek/Anthropic/OpenAI/Gemini; revisit `reference_aura_cache_poisoning_sites_2026-05-27`.
- **P8 (Sandbox 2b)** — workspace symlink escape + network allowlist iptables; CVE-2024-21626 lessons.
- **P11 (Skills 7c/7e)** — NFKC + Unicode TR15 + fuzz design; `npx --ignore-scripts` post-install verification.
- **P15 (Memory)** — Cognify 6-stage detail, mem0 2-fase vs UNIQUE constraint trade-offs, Leiden tuning, HNSW @ 100K.

**Phases with standard patterns (skip dedicated research):** P1 (standard Go OLTP + Neo4j; spike done 2026-05-27), P2 (ADK-go is the reference), P3 (5 ref impls in `D:/tmp/`), P4 (LangGraph PostgresSaver + Letta), P10 (standard `FOR UPDATE SKIP LOCKED` cron), P12 (AG-UI spec is documented), P13 (telebot.v4 + setup wizard plain HTTP), P14 (LoopAgent + ask_user composition).

### Per-Slice Quality Gates / Pre-Merge Benchmarks

Wire into Gate 1 DoR / Gate 3 DoD per slice. Living doc `docs/aura-quality-snapshot.md` updated on each merge; CI gate fails if bench OQ closes without snapshot update.

Key thresholds: **0.5** nightly restore drill < 90s; **0.7** embed dim assertion + spike recall@5 5/5; **0.9** 3 budget caps + zero goroutine leak; **2a SandboxEscapeBench escape rate < 5%**; **4 SHA-256(`messages[0]`) constant across 20-turn cross-slice replay**; **5** DNS rebinding + IPv6 blocklist tests; **6** chaos test network partition + advisory lock; **7a/c** 10K Unicode fuzz on validator; **7c** `aura_app` cannot DELETE/TRUNCATE/DROP audit; **9b** `409 Conflict` integration; **11a** Entity UNIQUE chaos test 10 goroutines "Mario Rossi"; **11d recall@5 ≥ 0.8 @ 1K/10K/100K**.

### Anti-Features Reaffirmed (PROJECT.md Out of Scope — STAY LOCKED)

Slice 13 (v2 GPU-gated); native Windows; TTS; public marketplace; multi-user auth/RBAC real; native Go Neo4j driver. Plus per Features research: real-time multi-user collab; auto-rewrite substrate; Computer Use; embedded LLM v1; web SPA; auto-routing across N LLM providers (OpenRouter does this); image generation (skill-installable); marketplace abuse protection (no marketplace = no need).

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH (8/13) / MEDIUM on 4 drift | May 2026 GitHub + Context7 + spike alignment; drift items have clear amendment paths |
| Features | MEDIUM-HIGH | HIGH on table stakes (Anthropic + OpenAI + Letta + Mem0 + LangGraph + Claude Code primary sources); MEDIUM on SEO-farm content honestly flagged |
| Architecture | HIGH | 5-layer is industry consensus (MS Agent Framework + OpenAI SDK + LangGraph + ADK-go converge); deferred-tool is Anthropic Nov 2025 pattern |
| Pitfalls | HIGH | Cross-referenced to SandboxEscapeBench, Neo4j #13387, langchain #16336, CVE-2024-21626, OWASP LLM Top 10 |

**Overall:** HIGH

### Gaps to Address (Gate 1 DoR open questions per slice)

- **Slice 0.7** — Neo4j 5.26 LTS vs 2026.05 rolling
- **Slice 9b** — telebot.v4 untagged vs go-telegram/bot semver
- **Slice 9b** — MarkdownV2 custom escaper vs HTML mode
- **Slice 11e** — `:AgentInsight` TTL pruning policy (Amendment #11 covers retrieval cache; broader prune is open)
- **Slice 13 (v2)** — vLLM CPU vs llama.cpp CPU "13-bis" fallback
- **Slice 8** — AG-UI SSE resumability vs poll-on-reconnect
- **Post-v1 OQ** — generic MCP client subprocess manager vs Skills convergence
- **Slice 7d** — npx installer outbound network policy (sidecar vs trust host)

## Sources

**Primary HIGH:** D:/Aura/prd.md 4400 LOC, .planning/codebase/ 7 docs / 2721 LOC, PROJECT.md, CLAUDE.md, 28+ Aura memories cited inline, Context7 (jackc/pgx, sqlc, telebot, ag-ui, neo4j, vllm, openai-go), GitHub repo inspection May 2026, Anthropic engineering (Agent Skills + Claude Code + `advanced-tool-use-2025-11-20` + Memory tool), OpenAI engineering, Letta docs, Mem0 2026 blog, LangGraph docs, OpenHands V0→V1, MS Agent Framework v1.0, ADK-go docs, AG-UI Events spec, GraphRAG Neo4j Labs + Microsoft, Neo4j Vector Index docs, **UK AISI SandboxEscapeBench arXiv 2604.23425**, Trail of Bits, OWASP LLM Top 10, CVE-2024-21626.

**Secondary MEDIUM:** Vellum.ai + DeepWiki + Vectorize.io (Letta/Mem0/SmolAgents comparison), Pecollective (LangGraph vs CrewAI), Toolhalla (Devin vs OpenHands), DigitalApplied (observability), Red Hat 2025-09-30 (vLLM vs llama.cpp), Blaxel + Augment Code + Bunnyshell (sandbox guides), Medium series (Agentic Resource Exhaustion + KV cache attacks + prompt caching), redteams.ai.

**Tertiary LOW (pattern-triangulation only):** "Best personal AI assistant 2026" SEO content (OpenClaw/Clawdbot/Moltbot/HoneyChat/Mira.tg) — specific product claims unverified; Snyk ToxicSkills methodology unverified though OWASP supply-chain risk is real.
