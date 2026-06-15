# FEATURES.md — Agentic AI Substrate / Personal AI Assistant (2025-2026)

**Domain:** Go-native agentic AI substrate, v1 ships as personal AI on Telegram-primary + mini-PC self-hosted
**Researched:** 2026-05-29
**Overall confidence:** MEDIUM-HIGH (primary sources verified for Claude Code, Anthropic Skills, Letta, Mem0, LangGraph, OpenAI; secondary "2026 product landscape" content has heavy SEO-farm signal and is downgraded)
**Mode:** Ecosystem + Comparison (against PRD scope)

---

## Confidence note up front

Searches for "best personal AI assistant 2026" return dozens of pages naming products — `OpenClaw`, `Clawdbot`, `Moltbot`, `ZeroClaw`, `Hermes Agent`, `Paperclip AI`, `HoneyChat`, `Mira.tg` — with viral-growth claims (`200K+ stars early 2026`, `290M downloads`, `13.4% malicious skills in ToxicSkills study`). The publication patterns (cross-citing, identical phrasing across `vellum.ai`, `agensi.io`, `tooljunction`, `runcell.dev`, `chatprd.ai`, `morphllm.com`) match content-farm SEO. I am unable to independently verify these products exist as described. **Treat any claim derived solely from these sources as LOW confidence**, and assume they reflect "what bloggers say a 2026 personal AI bot should have" rather than empirical product data. The category shape they describe is consistent with verifiable products (Letta, mem0, Claude Code, OpenHands, SmolAgents), so the **feature-list pattern** holds even if individual products don't.

Sources I treat as **HIGH confidence**: Anthropic engineering blog + platform.claude.com + github.com/anthropics/skills + code.claude.com (Claude Code, Skills, MCP, Computer Use); openai.com + developers.openai.com + help.openai.com (Memory, Custom Instructions, GPTs, Code Interpreter); docs.letta.com (Letta architecture); mem0.ai/blog (Mem0 design); LangGraph docs (interrupts/checkpointing); openhands.dev + Princeton SWE-Agent paper; huggingface SmolAgents docs.

---

## Feature Landscape

### Table Stakes (Users Expect These — Missing = "Feels Incomplete")

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| **Multi-turn streaming chat with cancel** | Every consumer LLM product since 2023 (Claude.ai, ChatGPT, Gemini) ships this. Non-streaming feels broken. Ctrl-C / stop button is universal. | LOW | PRD covers via Slice 1 SSE streaming + ctx-cancel + `Loop.Cancel` (Slice 0.9). HIGH confidence in scope. |
| **Tool calling discipline** (model invokes tools, sees results, iterates) | OpenAI / Anthropic / Google all ship native tool calling. Any 2026 agent without it is a 2022 chatbot. | LOW | PRD ToolResult pattern (Slice 1) + 7-tool skill surface + sandbox + web + memory tools. Covered. |
| **Persistent multi-thread conversations** (Claude.ai / ChatGPT Projects-style) | Threads in left sidebar are universal across Claude, ChatGPT, Gemini, Mistral Chat. Single-session chat now reads as a toy. | MEDIUM | PRD Slice 1.8 explicit — multi-thread + resume + archive + auto-title. Aligned with Claude.ai pattern. |
| **Long-term memory across conversations** | OpenAI Memory (2024), Claude Memory (2025), Mem0 / Letta. "Forget me every reset" is now unacceptable for a personal assistant. | HIGH | PRD Slice 11 explicit (Documents + Entities + Graph + Agent journal). Aligned with Mem0+Letta+GraphRAG hybrid. |
| **File / document ingestion** (PDF, MD, code, images) | ChatGPT file upload, Claude Projects knowledge, every "chat with your docs" product. | MEDIUM | PRD Slice 11b (ingestion) + Slice 9c (markitdown). Covered. |
| **Web search** | ChatGPT browse, Claude web search, Perplexity baseline. Agent-without-search hallucinates current events. | MEDIUM | PRD Slice 5 (`web_search` SearXNG + `web_fetch` readability). Covered. |
| **Code execution sandbox** | OpenAI Code Interpreter (since 2023), Claude analysis tool, SmolAgents CodeAgent, Anthropic Code Execution beta. Math, charts, file munging all assume this. | HIGH | PRD Slice 2 (Docker + seccomp + ulimit) + Slice 2b session-bound + Slice 7e snippets. Covered with strong security posture. |
| **Configurable system prompt / persona / "custom instructions"** | OpenAI Custom Instructions, Claude Projects custom instructions, GPT system prompt. Without it the agent has no identity. | LOW | PRD Slice 10 (`Agent.md` profile per identity) covers this. |
| **Human-in-the-loop interrupt / approval** | LangGraph interrupts (production pattern), Claude Code permission prompts, Cursor "apply edit" gate. Autonomous agents without HITL are dangerous and known to be. | MEDIUM | PRD Slice 1.5 (`ask_user` + multi-pause FIFO + persistent `paused_states`). Verified pattern (LangGraph interrupts confirmed). |
| **Cost / token visibility** | OpenAI billing dashboard, LangSmith / Langfuse / Helicone exist precisely because users demand this. Hidden cost is unacceptable on metered LLMs. | LOW | PRD Slice 1.8 `conversations` table has `total_*_tokens` + `total_cost_usd`. **Visibility surfacing in CLI/Telegram is implicit, not explicit — see gap below.** |
| **Multimodal input (image)** | GPT-4o, Claude 3.5/4, Gemini all multimodal native. Photo-of-receipt / screenshot-of-error is a baseline use case. | MEDIUM | PRD Slice 9c (Gemma 4 multimodal sidecar). Covered for photo input. |
| **Voice input (STT)** | ChatGPT voice mode, WhatsApp voice notes, Telegram voice messages. Mobile users speak more than type. | MEDIUM | PRD Slice 9c (Gemma 4 native audio, Whisper removed). Covered. |
| **At least one "real" channel beyond CLI** | Nobody outside dev uses CLI. Web UI, mobile app, or messenger is required for "feels like a product". | HIGH | PRD Slice 9b (Telegram primary user channel) + Slice 8 AG-UI gateway. Covered, well-justified by user actually-uses-Telegram. |
| **First-run setup that doesn't require reading docs** | Self-hosted products that survive (Open WebUI, Ollama) have one-command install + sensible defaults. | MEDIUM | PRD Slice 9a (`http://127.0.0.1:9081/setup` wizard with paste-token + QR). Covered. |
| **Sensible defaults / works out of box with one API key** | OpenWebUI works after `docker run`. Khoj works after `khoj` command. Any wizard with 15+ questions is dead-on-arrival for personal use. | MEDIUM | PRD `.env.example` template + OpenRouter default. Reasonable. |
| **Conversation export / Ctrl-F search across history** | Both Claude.ai and ChatGPT have conversation search. Implicit table stakes once you have N>20 conversations. | MEDIUM | **GAP — see PRD gaps below.** |
| **Stop button / abort tool call** | Claude Code Esc, ChatGPT stop, Cursor cancel. Long-running tools without abort are user-hostile. | LOW | PRD `Loop.Cancel` + Telegram `/cancel` command (Slice 9b). Covered. |

### Differentiators (Competitive Advantage for Aura's Position)

Given Aura's positioning — **substrate agentic Go-native, mini-PC self-hosted, Telegram-primary, DGX-Spark+SMB target** — these differentiators matter most:

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| **Skills as executable code snippets with TTL + cross-conversation pattern auto-suggest** | Voyager (NeurIPS 2023) skill-library pattern. Mem0 procedural memory. Most products treat skills as static instructions; auto-discovering skills from observed usage is rare. | HIGH | PRD Slice 7e is **the most genuinely novel feature in the scope**. If executed well, this is the differentiator. |
| **Persistent KV-cache discipline (byte-identical `messages[0]`) with provider-aware prompt caching** | DeepSeek V3.2/V4 80% cache savings, Anthropic ephemeral cache, OpenAI auto-cache all exist. Most agent frameworks corrupt the cache via dynamic system prompts. Genuine engineering rigor here = big cost win. | HIGH | PRD Slice 4 (KV cache builder + 6 mapped poison sites). Strongly differentiated. |
| **Knowledge graph + vector hybrid retrieval (Neo4j + HNSW + LLM re-rank + GraphRAG community)** | Cognee + GraphRAG community pattern. Most personal AI products do vector-only RAG which is brittle at scale. Neo4j-via-MCP is unusual (most products use native driver). | HIGH | PRD Slice 11d. Aligned with Microsoft GraphRAG + Cognee. Strong. |
| **Risk-tier governance (SAFE/NORMAL/RISKY/DESTRUCTIVE) with audit-immutable Postgres trigger + `ask_user` gate** | Most agent frameworks have no governance model. Snyk-cited "skill marketplace" prompt-injection problem (LOW conf source but the underlying risk is real per OWASP LLM Top 10) makes this credible. | MEDIUM | PRD Risk-Based Governance section is genuinely opinionated. Differentiator vs LangGraph/CrewAI (which leave governance to the developer). |
| **Multi-pause FIFO HITL across nested swarm children** | LangGraph 1.x supports multi-interrupt; few products support `ask_user` propagating from a child agent up to parent's UI. Telegram `proxied_from_child_id` mapping is rare. | HIGH | PRD Slice 1.5 + Slice 3. Genuinely novel composition. Risky to ship — see PITFALLS. |
| **Self-extending via writeable skills (`skill.create`/`skill.update` with HITL gate)** | Claude Code skills are static; user creates via filesystem. Aura's agent can author skills mid-conversation gated by approval. Letta has similar self-edit-memory; few products have self-edit-tooling. | HIGH | PRD Slice 7c. Differentiator if shipped safely. |
| **Multi-channel as a framework, not a vertical** | `internal/channels/<name>/` is the right abstraction. Self-hosted competitors (Open WebUI = web-only; Ollama = local-only) typically pick one channel and stay there. | MEDIUM | PRD Slice 9 framework + Telegram first impl. Differentiator only realized when 2nd channel lands (Discord/WhatsApp); currently latent. |
| **Domain-neutral substrate sold as "personal AI overlay"** | Per PROJECT.md Core Value. Most products bake the domain in (Cursor=code, Devin=software eng, Khoj=docs). Substrate-first is rare and aligns with DGX Spark + SMB bundling. | MEDIUM | Architectural decision, not a shippable feature. Realized via Slices 0.9 (Agent interface) + 10 (`Agent.md` profile). |
| **MCP integration (mcp-neo4j-cypher) + Skills as standard format (`SKILL.md`)** | MCP is the 2025-2026 winning interop standard (verified — Anthropic, OpenAI, and others have adopted). Embracing it = compatibility with Claude Code skills, Anthropic skills repo, ClawHub (if it exists). | MEDIUM | PRD Slice 7 uses Anthropic SKILL.md format + npx skills installer. Strong alignment. |
| **Persistent agent journal + cross-conversation insights (`:AgentEpisode` / `:AgentInsight`)** | Letta has self-edit-memory; rare to have *both* user-memory and *agent self-memory* as separate first-class subgraphs. | HIGH | PRD Slice 11e. Genuine differentiator if executed. |
| **Scheduler (cron + `agent_job`) for proactive work** | Letta + Moltbot-style "message you first" pattern. ChatGPT Tasks added this in 2024. Many self-hosted products lack it. | MEDIUM | PRD Slice 6. Covered. |
| **CPU-first deployment on mini-PC (16GB-32GB RAM target, no GPU required for v1)** | Counter-narrative to "everything needs GPU". Real users have mini-PCs and NUCs. Vellum/`getopenclaw` blog posts (LOW conf) consistently flag self-hosted-without-GPU as underserved. | MEDIUM | PRD constraints: cumulative 5.7-6.2 GB idle, peak 7 GB. Slice 13 GPU path is v2. Already a design choice; selling it as a feature requires `docs/aura-quality-snapshot.md` numbers per the `feedback_aura_as_product` memory. |

### Anti-Features (Aura Should NOT Build)

| Anti-Feature | Why Tempting | Why Problematic | What to Do Instead |
|--------------|--------------|-----------------|--------------------|
| **Public skill marketplace / skill-sharing registry** | Anthropic skills.sh exists, `npx skills add` ecosystem is real. "We could have a marketplace too" is a natural ambition. | Skill marketplaces have a documented supply-chain prompt-injection problem (Snyk ToxicSkills claim is LOW-conf, but OWASP LLM Top 10 supply-chain risk is HIGH conf). Aura is single-user single-tenant; a marketplace requires identity reputation, signing, sandboxing of skill execution, abuse handling — none of which v1 has. | PRD already excludes (PROJECT.md "Marketplace skills pubblico" in Out of Scope). Stay there. Use `npx skills add` to leverage existing ecosystems; don't host one. |
| **Native multi-user with auth / RBAC / OAuth** | "It scales!" "We could sell it to teams!" Real demand for self-hosted multi-user assistants exists (Mattermost-AI, etc.). | Multi-user well requires: session management, audit-per-user, RBAC enforcement at every tool call, per-user encryption, password reset flows, GDPR right-to-erasure. PRD Slice 11 OQ6 already estimates +800 LOC. v1 is single-user with capability_grants scaffolding — perfect. | PRD already excludes properly (PROJECT.md "Multi-user con auth/RBAC reale" Out of Scope). The scaffolding (Slice 1.7) is correctly minimal. |
| **TTS / voice output** | ChatGPT voice mode is wildly popular. Telegram supports voice messages natively. "Aura should speak back" feels natural. | TTS adds ~1-3 GB RAM (Pocket-TTS removed per commit `06df9b72`), latency budget for streaming TTS is hard, voice quality matters a lot and bad TTS is worse than no TTS, IT-language TTS quality varies. | PRD already excludes (PROJECT.md "TTS / voice output" Out of Scope). Stay there. Voice-out is a v2 milestone after the foundation is stable. |
| **Real-time collaboration / multiple users in same conversation** | Slack threads, Notion AI, Cursor "Bugbot in PR comments" all do this. | Real-time multi-user adds CRDT or OT, presence indicators, conflict resolution, per-user color coding, message ordering across N clients. Massive complexity for a single-user-first product. | Not in PRD. Keep it not in PRD. |
| **Auto-update / self-rewriting application code** | Skills already self-extend. "Why not let the agent rewrite Aura itself?" | The skill system is the contained answer. Letting the agent rewrite `internal/agent/loop.go` is unbounded blast radius. Voyager-style skills give 90% of the value with 10% of the risk. | PRD correctly scopes self-extension to skills, not to substrate source. |
| **Computer Use / OS-level screen+keyboard control** | Anthropic Computer Use (2024), OpenAI Operator (2025), ChatGPT Cowork are real and growing. | Computer Use needs: VNC/X11 transport, screen capture pipeline, vision model that understands UI, robust click/keystroke playback, dangerous side-effect mitigation. Massive surface that doesn't fit Telegram-as-primary-channel UX. | Out of scope for v1. The skill snippet runner (Slice 7e + sandbox 2b) covers the "automate work" use case without OS control. Computer Use is a v3+ consideration. |
| **Vector DB as a separate component (Qdrant, Pinecone, Weaviate)** | Industry default 2023-2024. "Use a real vector DB" is conventional wisdom. | Spike validated `2026-05-27` (memory `feedback_embedding_backend_stays_mistral`): Neo4j HNSW Lucene = 22-30ms p95 + recall@5 5/5. Adding Qdrant = +1 service to operate, +RAM, schema-sync surface. | PRD correctly uses Neo4j HNSW. Memory `feedback_embedding_backend_stays_mistral` confirms decision. |
| **Real-time websockets / bidirectional event protocol** | "REST is dead, websockets are the future." SSE is unfashionable. | SSE-only is sufficient for AG-UI (Slice 8). Most agentic event streams are server→client (token stream); client→server is request/response. Websockets add reconnect/heartbeat/auth complexity. | PRD Slice 8 OQ2 correctly defers websockets. |
| **Embedded LLM in v1 (no API dependency)** | "Privacy! Local! No API key!" Ollama/LM Studio popularity. | DeepSeek V4 via OpenRouter is $X/month at single-user volume + much higher quality than any locally-runnable 32B. Slice 13 (vLLM+LMCache) is in PRD but explicitly v2 + GPU-gated. Right call. | PRD Slice 13 Out-of-Scope-v1 confirmed (PROJECT.md). |
| **Embedded UI library / web SPA** | "Self-hosted needs a web UI." | Web SPA = React/Vue + build pipeline + auth + WebSocket + frontend QA + browser compat. Aura ships AG-UI (Slice 8) as a protocol; let the user choose a client. Telegram as primary covers the user-facing need without building a UI. | Setup wizard (Slice 9a `/setup` HTML page) is the only HTML/JS surface. Keep it minimal. |
| **Auto-routing across N LLM providers based on cost/quality** | OpenRouter does this; portkey, Helicone gateway do this. Tempting "smart routing layer." | PRD already uses OpenRouter (which does the multi-provider routing). Building Aura's own router is redundant work and a known foot-gun (cache invalidation, per-provider quirks). | PRD `provider-aware KV cache` (Slice 4) is the right abstraction layer. Don't build a routing layer above. |

---

## PRD Gaps (Features 2026 Expects That PRD Doesn't Explicitly Cover)

These are features comparable products ship that the PRD does NOT obviously include. Some may be implicit; flagging for explicit decision.

| Gap | Evidence | Recommendation |
|-----|----------|----------------|
| **Conversation full-text search across all threads** | Both Claude.ai (`Ctrl-F` across history added 2024) and ChatGPT (search in sidebar 2024) ship this. `aura chat list` returns metadata only; no `aura chat search "query"`. | Add as **post-Slice-1.8 enhancement** or **Slice 11d retrieval extension**. Postgres has `conversation_turns.content` already indexable with `pg_trgm` or full-text — small lift (~80 LOC + GIN index migration). **Recommend add to Slice 1.8 or split into Slice 1.8.5.** |
| **Explicit cost visibility in user-facing channels** | Token + USD aggregated per conversation exists in DB (Slice 1.8 schema). No explicit CLI command `aura chat cost` or Telegram `/cost`. | Add a `/cost` command to Slice 9b commands list + `aura chat cost [<id>]` CLI subcommand. ~30 LOC. **Minor scope add.** |
| **Conversation share / export** | Claude.ai share-link, ChatGPT share-conversation, ChatGPT export-data. Self-hosted equivalent = `aura chat export <id> --format markdown`. | Lightweight feature, ~50 LOC. Add to Slice 1.8 polish or post-MVP. Not blocking. |
| **Image generation** | DALL-E, Stable Diffusion, Imagen, FLUX. ChatGPT and Claude both ship image gen. | **Deliberate gap, probably correct.** PRD has Gemma 4 vision for *input*, no image *generation*. Mini-PC RAM budget can't host SD/FLUX. Could be a skill (`skill.install image-gen` that wraps an API like Replicate/fal.ai). Leave as user-installable skill — don't bake in. |
| **Voice output (TTS)** | Already analyzed as Anti-Feature. PROJECT.md explicit Out of Scope. | Confirmed exclusion. |
| **Mobile push notifications beyond Telegram** | iOS/Android push for "your scheduled task finished" / "your job needs approval". | Telegram *is* the mobile push for Aura. This is intentional — Telegram covers mobile push without building iOS/Android apps. **Reframe: not a gap, it's the architecture.** |
| **OpenTelemetry trace export for observability** | Verifiable: Langfuse, Arize, LangSmith all support OTel ingestion; OpenLLMetry is the de-facto standard. | PRD `golang-observability` skill is installed but I don't see explicit Slice for `internal/observability/otel.go`. CLAUDE.md mentions structured logging (slog) + OpenTelemetry but no slice owns the work. **Recommend: add OTel hooks to Slice 1 (LLM client) + Slice 0.9 (Agent runtime).** Without this, debugging multi-step agent runs becomes painful and "production-ready" perception drops. Low cost (~150 LOC), high perceived-quality return. |
| **First-class "Projects" concept (group conversations + shared context)** | Claude.ai Projects, ChatGPT Projects (2024). Group N conversations under a shared knowledge / system prompt. | PRD has `Agent.md` per identity (Slice 10) which is global. No per-project context. Could fit naturally: `aura projects new <name>` creates a sub-identity with its own `Agent.md`. Future enhancement, not v1 blocker. |
| **"Artifacts" / canvas / file outputs as first-class** | Claude Artifacts, ChatGPT Canvas — long-form code/markdown/HTML rendered in a side panel and editable. | Telegram-first UX makes canvas awkward (no side panel in messengers). AG-UI gateway (Slice 8) could expose artifact events for future web client. **Defer to post-v1 + future web client.** |
| **Rate limiting / abuse protection on Setup wizard** | PRD Slice 9a `/setup` endpoint has NO auth (correctly flagged in CONCERNS.md). For LAN deployment this is acknowledged risk. | Already documented as known-acceptable risk. Not a gap, but **consider one-time token gate** (`AURA_SETUP_TOKEN` env, printed to stdout once on first run) for LAN scenarios. ~20 LOC, big security win, doesn't break loopback default UX. |
| **Conversation-level retention policy / auto-delete after N days** | GDPR / privacy concern for European users (Italian SMB target!). ChatGPT has temp-chat, Claude has retention controls. | PRD has manual `aura chat delete` + `aura chat archive`. No automatic retention. **Add `AURA_CONVERSATION_RETENTION_DAYS` env (default unlimited)** as a Slice 1.8 polish item or post-v1. Italian SMB market will ask. |
| **Eval / quality regression tests for the agent itself** | Memory `feedback_aura_as_product` explicit requirement: `docs/aura-quality-snapshot.md` living doc with Recall@5/nDCG@10/p95 numbers + CI gates. | PRD has test discipline (Gate 3) but no explicit "quality eval corpus" slice. **Recommend: Slice 11d should include a `testdata/memory_eval/` corpus with golden retrievals + CI assertion on regression.** Pre-merge benchmark for chunk-size already exists; extend the pattern. |
| **MCP server beyond mcp-neo4j-cypher (e.g. filesystem, github, slack via MCP)** | MCP ecosystem growing (Anthropic engineering posts confirm dozens of community MCP servers). Aura could connect to existing MCP servers as a *consumer* not just a *configurer of one*. | PRD treats MCP as one-off for Neo4j. Consider: `internal/mcp/client.go` generic MCP subprocess manager (~200 LOC) that lets users plug in any MCP server as a tool source. Skill system overlaps; decide which is the canonical extension mechanism. **Open question for roadmap.** |

## PRD Over-Scope (Features That May Not Be Needed for Category)

| Over-scope candidate | Why it's in PRD | Why it may not be needed | Recommendation |
|----------------------|-----------------|--------------------------|----------------|
| **Swarm coordinator (Slice 3) with N-deep spawn + DM-by-ID + tier-mapped models** | Inspired by CrewAI/AutoGen multi-agent patterns. PRD calls out "Slice 0.9 ParallelAgent" reuse. | Real personal-AI use cases that need swarm are rare. Letta + LangGraph users typically work with single agents + tool calls. Most "swarm" demos are research curiosities. Mini-PC RAM/CPU budget makes large swarms infeasible anyway (3-agent parallel = 3x KV cache load). | **Don't drop, but defer to post-MVP and ship Slice 3 minimal** (just `ParallelAgent` reuse + 2-deep cap, no DM-by-ID, no tier mapping). The full N-deep coordinator may be premature optimization for a single-user mini-PC product. Re-evaluate after Telegram launches and you see what real conversations want. |
| **`skill.catalog` HTML scrape of skills.sh** | Was in pre-rewrite, "worked fine". | If skills.sh has the supply-chain prompt-injection problem the Snyk article alleges (LOW conf source but the risk pattern is real per OWASP LLM Top 10), pulling random skills from a catalog is *bad* UX (you're installing untrusted code with HITL gate as only defense). | **Keep as opt-in only.** Default mode = user types `/aura skills install <known-source>`. Don't proactively browse the catalog mid-conversation. The PRD's HITL gate is correct; the question is whether to surface catalog browsing at all in v1. Consider: remove `skill.catalog` tool from default manifest; require `aura skills enable-catalog` to expose it. |
| **Telegram setup wizard at `:9081` with no auth** | "Headless container UX" reason. | LAN-without-auth is a real foot-gun. Documented as known risk but the convenience-vs-security trade is questionable. | Add a one-time token gate (see PRD Gap above). Small lift, big security win. |
| **Pattern analysis cross-conversation auto-suggest skills (Slice 7e)** | Voyager-inspired, genuinely novel. Mem0 procedural memory parallel. | Auto-suggesting skills requires: similarity clustering across N conversations, threshold tuning, false-positive suppression, UX to surface suggestions without being annoying. Easy to over-suggest and feel spammy. Adds Neo4j HNSW load for cross-conv clustering. | **Ship the snippet save/execute first (Slice 7e core), defer the auto-suggest** to a 7f sub-slice or v2. The "save snippet" pattern is the high-value piece; "auto-suggest from patterns" is the differentiated-but-risky polish. |
| **AG-UI gateway as a separate transport (Slice 8)** | "Substrate domain-neutral" rationale + future web client. | If Telegram is the *only* user channel for v1 and CLI is dev-only, AG-UI gateway is solving a future problem (web client doesn't exist yet). Slice 9b explicitly uses **in-process subscriber pattern**, not HTTP. The HTTP gateway adds ~600 LOC for a consumer that doesn't exist yet. | **PRD already correctly defers this concern** (Slice 8 OQ4: CLI default in-process). Just make sure the gateway is a *thin wrapper* over the in-process emitter, not the canonical path. Don't over-invest until a real second consumer (web client) is in scope. |
| **Identity + `capability_grants` table when single-user `local` with wildcard `'*'`** | Scaffolding for future multi-user. | Building the storage schema before any consumer (CONCERNS.md: "no consumer that calls `HasCapability`") is YAGNI risk. However: refactor cost is high if added later (PRD Slice 11 OQ6 estimates +800 LOC). | **Keep minimal as PRD does.** The 2 tables + `HasCapability()` stub is the right amount of scaffolding. Don't expand to roles/groups/policies until a real consumer arrives. |

---

## Feature Dependencies (For Roadmap Ordering)

```
[Postgres infra 0.5]
    └─requires─> [Identity 1.7]
                     └─requires─> [Conversation persistence 1.8]
                                       └─requires─> [Memory 11]
                                       └─requires─> [Channels 9]
    └─requires─> [Paused states 1.5]
                     └─requires─> [Skills mutation 7c]
                     └─requires─> [Scheduler 6 high-cost confirm]
                     └─requires─> [Swarm 3 spawn-depth approval]

[Neo4j infra 0.7]
    └─requires─> [Memory 11]
    └─requires─> [Skill snippet semantic search 7e]

[Agent interface 0.9]
    └─requires─> [LLM agent (Slice 1)]
    └─requires─> [Parallel agent / Swarm (Slice 3)]
    └─requires─> [Scheduler handlers (Slice 6)]
    └─requires─> [Onboarding interview agent (Slice 10)]

[LLM client 1]
    └─requires─> [ToolResult pattern → all tool slices]
    └─requires─> [KV cache builder (Slice 4)]

[Sandbox 2a stateless]
    └─requires─> [Sandbox 2b session-bound]
                     └─requires─> [Skill snippets 7e]

[ask_user 1.5] ──enables──> [HITL across all mutation tools (7c, 6, 3)]

[Skills 7a/b/c/d] ──enables──> [Skill snippets 7e via reuse of writer/validator]

[Conversation persistence 1.8] ──enables──> [Memory ingestion of past conversations 11b]

[Memory 11] ──conflicts──> [Naive "always inject full history" — must defeat via budget 1.8 L2 + microcompact L1]

[Telegram 9b] ──requires──> [Multimodal sidecar 9c for voice/photo input]

[Scheduler 6] ──requires──> [Agent interface 0.9 for agent_job handler]
```

**Dependency notes**:
- **Cost visibility surfacing (new feature) requires Slice 1.8** (already has token aggregates in DB). Add `/cost` command in Slice 9b. Cheap to add at right time, expensive if forgotten.
- **OpenTelemetry export (new feature) requires Slice 1 LLM client + Slice 0.9 Agent runtime**. Add hooks early. Retrofitting OTel after 14 slices is painful.
- **Conversation full-text search (new feature) requires Slice 1.8** (`conversation_turns.content` to index). Could co-land with Slice 1.8 or split into Slice 1.8.5.
- **Per-project context "Projects" (new feature) requires Slice 1.7 identity** + likely a sub-identity concept. Defer to post-v1 — current `Agent.md` per identity covers single-user case.
- **Skill snippet auto-suggest (Slice 7e cluster analyzer) requires Slice 11 memory** for cross-conv similarity. Already correctly sequenced in PRD.

---

## MVP Recommendation

### Launch With (v1 = end of Slice 11)

The PRD's 14-slice scope is **mostly right for MVP**. The MVP shape:

- [x] **Postgres + Neo4j infra** (Slice 0.5, 0.7) — table-stakes persistence
- [x] **Agent runtime + LLM client + KV cache** (Slice 0.9, 1, 4) — core agentic loop
- [x] **ask_user + identity + conversations** (Slice 1.5, 1.7, 1.8) — HITL + multi-thread
- [x] **Sandbox + web tools** (Slice 2a/2b, 5) — table-stakes capabilities
- [x] **Skills (read + mutation + install + snippet core)** (Slice 7a/b/c/d + 7e core *without* auto-suggest) — differentiator
- [x] **Scheduler** (Slice 6) — proactive table-stakes
- [x] **AG-UI thin gateway + Telegram + multimodal** (Slice 8, 9a/b/c) — channel for real users
- [x] **Onboarding `Agent.md` + Memory ingestion + GraphRAG retrieval** (Slice 10, 11a/b/c/d/e) — personalization + long-term memory

**Add to v1 (small lifts, table-stakes, currently gaps)**:
- [ ] **Conversation full-text search** (`pg_trgm` GIN index + `aura chat search` CLI) — ~80 LOC, table-stakes
- [ ] **Cost visibility commands** (`/cost`, `aura chat cost`) — ~30 LOC, table-stakes
- [ ] **OpenTelemetry export** — ~150 LOC, "production-ready" signal
- [ ] **Setup wizard one-time token** — ~20 LOC, big security win

### Defer Beyond v1

- [ ] **Swarm coordinator full N-deep with DM-by-ID + tier-mapped models** (Slice 3) — ship minimal Slice 3 (ParallelAgent + 2-deep cap), defer the rest until a real use case
- [ ] **Skill `catalog` proactive surfacing** — keep `skill.install` tool, hide `skill.catalog` behind opt-in
- [ ] **Skill snippet auto-suggest analyzer** (Slice 7e cluster part) — ship snippet save/execute first, defer auto-suggest to v1.x
- [ ] **Local LLM fallback (Slice 13)** — already correctly v2 in PRD
- [ ] **Conversation export / share-link** — polish item
- [ ] **Generic MCP server consumer** (use multiple MCP servers, not just neo4j) — ~200 LOC, defer until skill system limitations show up

### Future (v2+)

- [ ] **TTS / voice output** (PRD-confirmed future milestone)
- [ ] **Computer Use / OS-level control** (out of category scope)
- [ ] **Image generation** (skill-installable, don't bake in)
- [ ] **Native multi-user with auth** (PRD-confirmed future milestone)
- [ ] **Public skill marketplace** (PRD-confirmed Out of Scope, supply-chain risk)
- [ ] **Web client** (when AG-UI gateway has its first non-CLI/non-Telegram consumer)
- [ ] **Projects-style conversation grouping**
- [ ] **Mobile-native iOS/Android apps** (Telegram covers mobile push)
- [ ] **Real-time multi-user collaboration** (out of category scope for personal AI)

---

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority | In PRD? |
|---------|------------|---------------------|----------|---------|
| Streaming chat + cancel | HIGH | LOW | P1 | YES |
| Multi-thread persistence | HIGH | MEDIUM | P1 | YES (1.8) |
| Tool calling + sandbox | HIGH | HIGH | P1 | YES (1, 2) |
| Long-term memory (GraphRAG hybrid) | HIGH | HIGH | P1 | YES (11) |
| Telegram channel | HIGH | MEDIUM | P1 | YES (9b) |
| HITL `ask_user` + multi-pause | HIGH | MEDIUM | P1 | YES (1.5) |
| Skills (read + mutation + install) | HIGH | HIGH | P1 | YES (7a-d) |
| Scheduler proactive | MEDIUM | MEDIUM | P1 | YES (6) |
| Multimodal voice + photo input | HIGH | MEDIUM | P1 | YES (9c) |
| Web search + fetch | HIGH | LOW | P1 | YES (5) |
| KV cache provider-aware | HIGH (cost) | MEDIUM | P1 | YES (4) |
| Conversation full-text search | MEDIUM | LOW | **P1 — ADD** | NO ⚠️ |
| Cost visibility CLI/Telegram | MEDIUM | LOW | **P1 — ADD** | Partial (DB only) ⚠️ |
| OTel trace export | MEDIUM | LOW | **P1 — ADD** | Implicit, not slice-owned ⚠️ |
| Setup wizard auth token | MEDIUM (sec) | LOW | **P1 — ADD** | NO ⚠️ |
| Agent journal + insights | MEDIUM | HIGH | P2 | YES (11e) |
| Skill snippets (save/execute) | MEDIUM | HIGH | P1 | YES (7e core) |
| Skill snippet auto-suggest | LOW | HIGH | **P3 — DEFER** | YES (7e analyzer) ⚠️ |
| Swarm coordinator full | LOW | HIGH | **P3 — MINIMIZE** | YES (3) ⚠️ |
| Conversation export | LOW | LOW | P2 | NO |
| MCP generic consumer | MEDIUM | MEDIUM | P2 | NO |
| Retention policy auto-delete | MEDIUM (GDPR) | LOW | P2 | NO |
| AG-UI HTTP gateway | LOW (v1) | MEDIUM | P2 | YES (8, thin OK) |
| Local LLM fallback | LOW (v1) | HIGH | P3 (v2) | YES (13, deferred) |
| TTS voice output | LOW (v1) | HIGH | P3 (v2) | NO (PROJECT.md OoS) |
| Image generation | LOW (v1) | HIGH | P3 (skill) | NO |
| Multi-user auth | LOW (v1) | HIGH | P3 (v2) | NO (scaffolding only) |
| Computer Use | LOW (v1) | HIGH | P3 (v3+) | NO |
| Public skill marketplace | LOW (v1) | HIGH | NEVER (anti-feature) | NO |

**Priority key:** P1 = must-have for v1 launch; P2 = should-have in v1 polish; P3 = defer to v2+

---

## Competitor Feature Analysis (Selected Comparison)

| Feature | Claude Code | OpenAI ChatGPT | Letta | Mem0 | LangGraph | SmolAgents | Open WebUI | **Aura (PRD)** |
|---------|-------------|----------------|-------|------|-----------|------------|------------|----------------|
| Multi-thread conversations | ✅ (sessions) | ✅ | ✅ | N/A (lib) | ✅ (checkpointer) | N/A | ✅ | ✅ (Slice 1.8) |
| Persistent memory cross-conv | partial | ✅ (Memory) | ✅ (core+archival) | ✅ (4-op extract) | partial (state) | ❌ | partial | ✅ (Slice 11 GraphRAG) |
| Knowledge graph backend | ❌ | ❌ | ❌ (vector) | ✅ (graph+vector) | ❌ | ❌ | ❌ | ✅ (Neo4j) |
| Skills (instruction format) | ✅ (SKILL.md) | ✅ (GPTs) | ❌ | N/A | ❌ | partial (tools) | ❌ | ✅ (Slice 7 SKILL.md compat) |
| Skills (executable code snippets, persistent) | ❌ | ❌ | ❌ | partial (proc mem) | ❌ | ✅ (CodeAgent) | ❌ | ✅ (Slice 7e) |
| Code execution sandbox | ✅ (Bash tool) | ✅ (Code Interp) | N/A | N/A | ❌ (delegates) | ✅ (E2B/Pyodide) | partial | ✅ (Slice 2 Docker+seccomp) |
| Web search | ✅ | ✅ (browse) | ❌ (delegates) | N/A | ❌ | partial | partial | ✅ (Slice 5 SearXNG) |
| Tool calling discipline | ✅ | ✅ | ✅ | N/A | ✅ | ✅ | ✅ | ✅ |
| Human-in-the-loop interrupt | ✅ (permissions) | ❌ | ✅ | N/A | ✅ (interrupts) | ❌ | ❌ | ✅ (Slice 1.5 multi-pause FIFO) |
| Multi-agent / swarm | ✅ (sub-agents) | partial (Assistants) | ❌ | N/A | ✅ | ✅ | ❌ | ✅ (Slice 3) |
| Scheduler / cron | ❌ | ✅ (Tasks) | partial | ❌ | ❌ | ❌ | ❌ | ✅ (Slice 6) |
| Telegram channel | ❌ | unofficial | ✅ (lettabot) | N/A | DIY | DIY | ❌ | ✅ (Slice 9b primary) |
| Voice input | ✅ (CLI dictate) | ✅ | ❌ | N/A | ❌ | ❌ | partial | ✅ (Slice 9c Gemma 4) |
| Image input | ✅ | ✅ | ❌ | N/A | ❌ | ❌ | ✅ | ✅ (Slice 9c) |
| Cost visibility | partial | ✅ | partial | N/A | ✅ (LangSmith) | ❌ | partial | partial (DB only — **GAP**) |
| OpenTelemetry export | ❌ (proprietary) | ❌ | partial | N/A | ✅ | ❌ | ❌ | **GAP** |
| Self-hosted | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Single-binary Go | ❌ (Node) | ❌ | ❌ (Python) | ❌ (Python) | ❌ (Python) | ❌ (Python) | ❌ (Python) | ✅ |
| MCP support | ✅ | ❌ (own SDK) | ❌ | ❌ | partial | partial | ❌ | ✅ (mcp-neo4j-cypher) |
| Conversation FTS | ✅ | ✅ | partial | N/A | partial | ❌ | ✅ | **GAP** |

---

## Sources

### HIGH confidence (authoritative)

- Anthropic — Equipping agents for the real world with Agent Skills (anthropic.com/engineering/equipping-agents-for-the-real-world-with-agent-skills)
- Anthropic — Agent Skills API docs (platform.claude.com/docs/en/agents-and-tools/agent-skills/overview)
- Anthropic — Skill authoring best practices (platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices)
- Anthropic — Claude Code docs (code.claude.com/docs/en/overview)
- Anthropic — Claude Code product page (anthropic.com/product/claude-code)
- GitHub — anthropics/claude-code
- GitHub — anthropics/skills (SKILL.md examples)
- Letta — Core concepts (docs.letta.com/core-concepts/)
- Letta — Research background (docs.letta.com/concepts/letta/)
- Letta — Core memory guide (docs.letta.com/guides/ade/core-memory/)
- Mem0 — State of AI Agent Memory 2026 (mem0.ai/blog/state-of-ai-agent-memory-2026)
- OpenAI — Memory and new controls for ChatGPT
- OpenAI — Code Interpreter API guide
- OpenAI Help — Custom Instructions
- OpenAI Help — Creating and editing GPTs
- OpenHands — official site (openhands.dev)
- LangGraph — interrupts (docs.langchain.com/oss/python/langgraph/interrupts)

### MEDIUM confidence (vendor / commercial-blog, generally accurate)

- DeepWiki — SKILL.md Format Specification
- SurePrompts — Letta Walkthrough
- Medium / Piyush Jhamb — Stateful AI Agents Deep Dive into Letta
- Vectorize.io — Mem0 vs Letta comparison
- Vectorize.io — Best AI Agent Memory Systems 2026
- aiagentslist.com — SmolAgents Review
- AI Discoveries — SmolAgents tutorial 2026 guide
- Pecollective — LangGraph vs CrewAI vs AutoGen
- Toolhalla — Devin vs OpenHands vs SWE-agent 2026
- DigitalApplied — Agent observability platforms 2026
- Firecrawl — Best LLM Observability Tools

### LOW confidence (SEO-farm signal, treat product claims as unverified)

These pages exist and may reflect real category-level patterns, but specific product names/stats (OpenClaw 200K stars, Snyk ToxicSkills 36%, Moltbot/Clawdbot/HoneyChat features, Hermes Agent stack, ZeroClaw) could not be independently verified. Cited only for **pattern triangulation**, not as evidence for specific claims.

- vellum.ai — Best Open-Source Personal AI Assistants 2026
- vellum.ai — Best Personal AI Assistants 2026
- vellum.ai — Best Private Personal AI Assistants 2026
- getclawdbot.com — Open Source AI Assistants Compared 2026
- hippotool.com — Personal AI Agents 2026 OpenClaw vs Perplexity vs Antigravity vs Claude
- honeychat.bot — Best AI Bots on Telegram 2026
- goinsight.ai — 12 Best Bots on Telegram 2026
- mira.tg — AI Agents in Telegram
- snyk.io — ToxicSkills study (methodology unverified; supply-chain risk pattern is real per OWASP LLM Top 10)
- owasp.org — Agentic Skills Top 10 (verify project exists at OWASP before citing)
- agensi.io — Agent Skills Marketplace
- Medium Bumurzaqov — Top 10 AI Memory Products 2026
- Medium ATNO — 10 AI Agent Frameworks 2026

---

## Quality Gate Self-Check

- [x] Categories are clear and mutually exclusive (Table Stakes / Differentiators / Anti-Features / PRD Gaps / PRD Over-Scope)
- [x] Complexity noted per feature (LOW/MEDIUM/HIGH)
- [x] Dependencies between features identified (Dependency block above)
- [x] More than 5 comparable products surveyed by name (Claude Code, ChatGPT, Letta, Mem0, LangGraph, SmolAgents, OpenHands, CrewAI, AutoGen, Open WebUI, Devin, Cursor)
- [x] PRD gaps section present (Conversation FTS, cost visibility surface, OTel, setup token, retention)
- [x] PRD over-scope section present (Swarm full, skill catalog, auto-suggest analyzer)
- [x] Confidence levels assigned honestly (HIGH/MEDIUM/LOW per source, explicit downgrade for SEO-farm content)

---

## Roadmap Implications for Downstream Consumer

**The PRD scope is largely correct.** Aura's 14 slices map well onto 2026 category expectations. The differentiators (KV cache discipline, Neo4j+graph hybrid memory, executable skill snippets with Voyager pattern, multi-pause FIFO HITL, risk-tier governance with audit trigger) are genuinely novel and well-grounded in verified patterns (Letta core memory, Mem0 extract+ADD, GraphRAG community, Claude Code Skills format, LangGraph interrupts).

**Four small adds recommended before MVP**:
1. **Conversation full-text search** (Slice 1.8 or 1.8.5, ~80 LOC + GIN index)
2. **Cost visibility surfacing** (Slice 9b `/cost` command + `aura chat cost` CLI, ~30 LOC)
3. **OpenTelemetry export hooks** in Slice 1 + Slice 0.9 (~150 LOC)
4. **Setup wizard one-time token** in Slice 9a (~20 LOC)

**Three scope reductions recommended**:
1. **Slice 3 swarm**: ship minimal `ParallelAgent` + 2-deep cap; defer DM-by-ID and tier-mapped models until real demand
2. **Slice 7e auto-suggest**: ship snippet save/execute first, defer cross-conv cluster analyzer to 7f or v1.x
3. **Slice 7 `skill.catalog`**: hide behind opt-in (`aura skills enable-catalog`) to reduce supply-chain risk surface

**One reframe**: The "single-channel via Telegram only" decision is the right MVP move and should be **explicitly sold as the architecture**, not apologized for. Mobile push, voice in/out via voice messages, photo in via image messages — Telegram covers everything mobile-native without building iOS/Android apps. This is the correct asymmetric bet for the mini-PC self-hosted personal AI category, and is well-supported by the (LOW-conf but pattern-consistent) competitor blog landscape.
