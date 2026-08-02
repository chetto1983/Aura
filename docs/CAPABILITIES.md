# Aura — Capabilities Matrix

**Updated:** 2026-07-17 · Companion to [TECHNICAL_OVERVIEW.md](TECHNICAL_OVERVIEW.md),
[ARCHITECTURE.md](ARCHITECTURE.md), [`.planning/codebase/`](../.planning/codebase/).

Status legend: **✅ Shipped** (implemented, and covered by the CI-enforced ≥85%
owned-surface coverage floor) · **🟡 In progress** (current milestone, not yet
closed) · **🔭 Roadmap** (designed/planned, not built).

Milestones: **v0.0.0** substrate (Phases 0-21) shipped 2026-06-15 · **v1.0.0** web
cockpit (Phases 22-30) shipped 2026-06-29 · **v2.0.0** industrial hardening
(Phases 31-42) in progress.

> A ✅ here means the implementing code exists and was verified in this file's last
> audit. A closed roadmap phase is **not** on its own sufficient — capabilities
> deferred by design (see `.planning/STATE.md` → Deferred Items) stay 🔭 even when
> their phase is checked off.

## Core agent

| Capability | What it does | Status | Key packages |
|---|---|---|---|
| Streaming agent loop | Budget-gated tool-dispatch loop over a streaming LLM; terminal `text_response` ends a turn | ✅ | `agent` (`llm_agent`) |
| Budget tree | Shared step + wall-clock cap across an entire agent tree; per-branch fair-share | ✅ | `agent` (`budget`) |
| Tool-loop dedup | Two-phase dedup ring with result-change progress veto stops repeated calls | ✅ | `agent` (`budget_dedup`) |
| Workflow agents | Sequential / Parallel / Loop composition (leak-safe, escalate-aware) | ✅ | `agent/workflow` |
| Swarm fan-out | `swarm_spawn`: N goals as budget-bounded parallel workers, per-child failure isolation | ✅ | `swarm`, `agent/tools` |
| Adaptive reasoning router | Local embedding classifier routes reasoning effort (`none/low/high`) off the hot path | ✅ | `agent/prompt` (`reasoning_policy`), `semindex` (`classifier`) |
| HITL pause/resume | `ask_user` suspends a turn for clarification/approval/choice; FIFO ledger | ✅ | `agent`, `agent/tools` (`ask_user`), `askuser`, `db` |
| Hooks | In-process + trust-gated out-of-process lifecycle hooks (before/after model & tool) | ✅ | `agent` (`hooks`, `hooks_command`) |
| ToolGateway + policy engine | Central PEP: every tool call passes `Decide` → allow / deny / consent-bound approval | ✅ | `gateway` (`decide`, `classify`, `approvals`) |
| Durable reservation ledger | Crash-safe tool reservation + idempotent replay; orphan reconciler never re-invokes | ✅ | `gateway` (`reserve`, `reconcile`), `toolinvocations` (`store_reserve`) |
| Orchestrator (planner→executor) | Plan→fan-out→verify→synthesize multi-agent workflows | 🔭 | designed (`docs/superpowers/specs/`) |

## Tools

| Capability | What it does | Status | Tool(s) |
|---|---|---|---|
| Deferred-tool discovery | Heavy tool specs hidden from the manifest; found via semantic search | ✅ | `tool_search` |
| Host filesystem | Read / write / edit / grep / glob with walk-budget caps | ✅ | `fs_read/write/edit/grep/glob` |
| Host shell | Full terminal; background jobs; destructive-command approval; secret redaction | ✅ | `shell_exec`, `shell_poll`, `shell_kill` |
| Web | SearXNG search + SSRF-hardened fetch → readable markdown | ✅ | `web_search`, `web_fetch` |
| Document search | Cited retrieval over the Neo4j document graph (rerank-backed) | ✅ | `document_search` |
| Scheduling | Schedule / list / cancel / run background tasks + reminders | ✅ | `task` |
| Self-extension | Author / apply / manage skills + executable snippets | ✅ | `skill` |
| Working memory | Session-scoped multi-step todo list | ✅ | `todo_write` |
| Artifact delivery | Send a host file to the user as an attachment | ✅ | `send_file` |
| Output paging | Page byte ranges out of a spilled-to-sidecar tool result | ✅ | `read_tool_output` |
| Time | The only model-facing wall-clock read (keeps the prompt cache stable) | ✅ | `current_time` |

## Knowledge & memory

| Capability | What it does | Status | Key packages |
|---|---|---|---|
| Conversation persistence | Multi-thread, Claude.ai-style; atomic per-turn append | ✅ | `conversations`, `db` |
| Context-management ladder | L1 microcompact → L2 budget gate → L2.5 oldest-pair drop + rot events | ✅ | `conversations` (`context`) |
| Document ingestion | PDF/xlsx/DOCX → chunks → Neo4j sparse FTS + async vector embeddings | ✅ | `documents`, `knowledge`, `assets` |
| Graph + vector store | Neo4j Community + APOC + GDS, **1024-d** HNSW (cosine) + fulltext | ✅ | `knowledge` |
| Rerank | Cross-encoder rerank over hybrid candidates; fail-soft identity degrade | ✅ | `rerank` |
| Agent-memory MCP | Entities / facts / preferences / sessions via the memory MCP server | ✅ | `mcp/manager` (catalog), `agent/mcptools` |
| User profile | Per-identity `Agent.md` (atomic writes), injected as a protected block | ✅ | `profile`, `onboarding` |

## Identity, isolation & storage

| Capability | What it does | Status | Key packages |
|---|---|---|---|
| Multi-user identity isolation | Per-identity RLS carrier + capability grants; owner-scoped conversations / approvals / documents | ✅ | `identity`, `identityctx`, `db` (`WithIdentityTx`) |
| Authula auth | Embedded auth provider: sessions, capability-per-route, password reset, bootstrap | ✅ | `webauth`, `agui` (`auth`, `password_reset`) |
| Break-glass recovery | Offline admin/operator recovery path (`aura identity recover`) | ✅ | `breakglass` |
| Per-identity object store | Garage bucket-per-identity + encrypted credential resolver; fail-closed miss | ✅ | `objectstore` (`identity_store`, `garageadmin`) |
| Per-user sandbox | Full-capability per-identity Docker box; egress floor, lifecycle, TTL reaper; strict-profile routing | ✅ | `sandbox/usersandbox`, `cron/handlers` (`sandbox_reap`) |
| Per-identity skills root | Skills + pyscripts filesystem rooted per identity | ✅ | `skills` (`identity_root`) |
| Identity de-provisioning | Scheduled purge of a de-provisioned identity's durable state | ✅ | `cron/handlers` (`identity_purge`) |
| Conversation sharing / export | Export file + revocable intra-identity link + opt-in expiring public link | 🟡 | `share` (Phase 37F, in progress) |

## LLM & provider

| Capability | What it does | Status | Key packages |
|---|---|---|---|
| Provider-neutral client | Streaming `llm.Client` interface; no vendor SDK | ✅ | `llm` (`client`) |
| OpenAI-compatible SSE | Hand-rolled streaming client (idle watchdog, tool-call accumulation) | ✅ | `llm/openai_compat` |
| Default model | DeepSeek-V4 over OpenRouter; swap by config | ✅ | `llm` (`config`, `models`) |
| Cost tracking | Provider cost preferred, price-table fallback; `aura cache-stats` | ✅ | `llm` (`prices`), `cachemetrics` |
| Circuit breaker + retry | Bounded stream-open retry with Retry-After awareness | ✅ | `agent`, `llm` (`breaker`) |
| KV-cache discipline | Byte-stable `messages[0]`; volatile data appended after history | ✅ | `agent/prompt` |
| Multimodal | Vision + STT + TTS behind one capability-gated client | ✅ | `multimodal`, `llm` (`capabilities`) |
| Local LLM fallback | vLLM + LMCache dual sidecar for offline operation | 🔭 | deferred (GPU-gated, Slice 13 — `LLM-V2-01`) |

## Channels & transport

| Capability | What it does | Status | Key packages |
|---|---|---|---|
| CLI agent REPL | `aura chat` / `aura shell` — primary interactive operator surface | ✅ | `cmd/aura`, `channels` |
| Telegram | Renderer, HITL keyboards, status pane, artifacts | ✅ | `channels/telegram` |
| Telegram multimodal | Voice (STT), photo (vision/OCR), document ingest | ✅ | `channels/telegram`, `multimodal` |
| Telegram multi-user routing | Per-user turn scoping at the single `startTurn` choke point | ✅ | `channels/telegram` (`bot_dispatch_turn`), `identityctx` |
| Setup wizard | Loopback HTTP + QR pairing for a Telegram bot | ✅ | `setup` |
| AG-UI / SSE | Event-protocol transport (one-way Event → AG-UI bridge) | ✅ | `agui` |
| Web cockpit | Embedded Vite/React + assistant-ui over AG-UI/SSE: chat, approval center, typed-display router, Neo4j graph explorer, governance boards, settings, onboarding | ✅ | `webui` (`//go:embed all:dist`), `agui`, `web/` |
| Connect integrations | Calendar/PIM + WhatsApp pairing from the cockpit | ✅ | `web/src/governance` (`CalendarConnect`, `WhatsAppConnect`) |

## Self-extension (skills & MCP)

| Capability | What it does | Status | Key packages |
|---|---|---|---|
| Instruction skills | Markdown SKILL.md, loaded on-demand by frontmatter description | ✅ | `skills` |
| Executable snippets | Multi-language code snippets with pattern analysis + TTL archive | ✅ | `skills`, `cron/handlers` (`skill_ttl`) |
| Skill governance | Validate / gate (risk tier) / audit skill mutations; web install + lifecycle | ✅ | `skills` (`installer`, `audit_store`), `scoring`, `agui` |
| MCP client | Generic JSON-RPC client (stdio + Streamable-HTTP) | ✅ | `mcp` |
| MCP tool bridge | Namespaced, trust-framed, deferred-by-default tool mounting; fail-soft boot + bounded retry | ✅ | `agent/mcptools` |
| MCP governance | Managed config, trust classes, docker/local launch, recipe catalog | ✅ | `mcp/manager` (`config`, `runtime`, `catalog`) |
| Recipe catalog | calculator · calendar (PIM: mail+calendar+contacts) · whatsapp · memory | ✅ | `mcp/manager` (`catalog`) |

## Automation & operations

| Capability | What it does | Status | Key packages |
|---|---|---|---|
| Scheduler | Cron tick loop + crash recovery; agent jobs, reminders, backups, sweeps | ✅ | `cron`, `cron/handlers` |
| Runtime profiles | Typed deployment profile gates config + tool routing | ✅ | `config` (`config_runtimeprofile`) |
| Config validation | KnobSpec registry + `aura config validate [--profile] [--json]` | ✅ | `config` (`config_knobs`), `cmd/aura` |
| Onboarding | Interview (LoopAgent) → LLM-extracted facts → standard `Agent.md`; CLI + web | ✅ | `onboarding`, `agui` (`onboarding_session`) |
| Settings | Allowlisted, typed runtime settings store behind the cockpit settings page | ✅ | `settings`, `web/src/settings` |
| Risk-Based governance | Qualitative tier scoring for advisory gates | ✅ | `scoring` |
| Full-stack health | `aura doctor` — Postgres / Neo4j / embedding / sidecar checks | ✅ | `cmd/aura` (`doctor`) |
| Forensic ledgers | Append-only, un-deletable tool-invocation + skill + profile audit | ✅ | `toolinvocations`, `skills` (`audit_store`), `db` |
| Eval harness | Live CoT / tool-use evaluation against the spec dimensions | ✅ | `eval` |

## Observability

| Capability | What it does | Status | Key packages |
|---|---|---|---|
| Distributed tracing | OTel spans `agent.turn → llm.request → tool.execute` | ✅ | `agent` (`tracing`), `obs` |
| Metrics | Prometheus + expvar (budget, tools, streams, tokens, cost, panics) | ✅ | `agent` (`metrics`), `obs` |
| Panic observability | Bounded-cardinality recovered-panic counters | ✅ | `agent/panicobs` |
| Reasoning trace | Env-gated, redacting JSONL trace of the reasoning/wire path | ✅ | `reasoningtrace` |
| Cache metrics | Per-turn cache hit-rate, windowed via `aura cache-stats` | ✅ | `cachemetrics` |

## CLI surface (selected)

`aura serve` · `aura shell` · `aura chat <sub>` · `aura doctor` · `aura tools`
· `aura config {show|get|set|validate}`
· `aura identity {list|get|grant|revoke|recover|recover-operator}`
· `aura profile {show|add-fact}` · `aura paused-states {list|purge}`
· `aura task {schedule|list|cancel|run_now|approve|runs|doctor}`
· `aura skills {list|info|create|update|delete|always|snippet|audit}`
· `aura mcp <sub>` · `aura memory <sub>` · `aura agent <sub>` · `aura swarm-demo`
· `aura web {doctor|tool}` · `aura docs {ingest|search|status|list}`
· `aura db {migrate|ping|status|reset}`
· `aura objectstore <sub>` · `aura cache-stats --since=<dur>` · `aura version`

`aura chat` subcommands: `list|new|resume|archive|unarchive|delete|rename|search`.
Source of truth: the dispatch switch in `cmd/aura/main.go`.
