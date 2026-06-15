# Aura — Capabilities Matrix

**Updated:** 2026-06-15 · Companion to [TECHNICAL_OVERVIEW.md](TECHNICAL_OVERVIEW.md),
[ARCHITECTURE.md](ARCHITECTURE.md), [CODEBASE_MAP.md](CODEBASE_MAP.md).

Status legend: **✅ Shipped** (implemented + tested) · **🟡 In progress** (current
milestone) · **🔭 Roadmap** (designed/planned, not built).

## Core agent

| Capability | What it does | Status | Key packages |
|---|---|---|---|
| Streaming agent loop | Budget-gated tool-dispatch loop over a streaming LLM; terminal `text_response` ends a turn | ✅ | `agent` (`LlmAgent`) |
| Budget tree | Shared step + wall-clock cap across an entire agent tree; per-branch fair-share | ✅ | `agent` (`Budget`) |
| Tool-loop dedup | Two-phase dedup ring with result-change progress veto stops repeated calls | ✅ | `agent` (`budget_dedup`) |
| Workflow agents | Sequential / Parallel / Loop composition (leak-safe, escalate-aware) | ✅ | `agent/workflow` |
| Swarm fan-out | `swarm_spawn`: N goals as budget-bounded parallel workers, per-child failure isolation | ✅ | `swarm`, `agent/tools` |
| Adaptive reasoning router | Local embedding classifier routes reasoning effort (`none/low/high`) in ~10 ms | ✅ | `agent/prompt`, `reasoning*`, `semindex` |
| Self-improving routers | Async learner upgrades reasoning + tool routing off the hot path | ✅ | `activelearn`, `reasoning*`, `toolselect*` |
| HITL pause/resume | `ask_user` suspends a turn for clarification/approval/choice; FIFO ledger | ✅ | `agent`, `agent/tools` (`ask_user`), `db` |
| Hooks | In-process + trust-gated out-of-process lifecycle hooks (before/after model & tool) | ✅ | `agent` (`hooks`, `hooks_command`) |
| Orchestrator (planner→executor) | Plan→fan-out→verify→synthesize multi-agent workflows | 🔭 | designed (`docs/superpowers/specs/`) |

## Tools

| Capability | What it does | Status | Tool(s) |
|---|---|---|---|
| Deferred-tool discovery | Heavy tool specs hidden from the manifest; found via semantic search | ✅ | `tool_search` |
| Host filesystem | Read / write / edit / grep / glob with walk-budget caps | ✅ | `fs_read/write/edit/grep/glob` |
| Host shell | Full terminal; background jobs; destructive-command approval; secret redaction | ✅ | `shell_exec`, `shell_poll`, `shell_kill` |
| Web | SearXNG search + SSRF-hardened fetch → readable markdown | ✅ | `web_search`, `web_fetch` |
| Document search | Cited retrieval over the Neo4j document graph | ✅ | `document_search` |
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
| Context-management ladder | L1 microcompact → L2 budget gate → L2.5 oldest-pair drop + rot events | ✅ | `conversations` (`context.go`) |
| Document ingestion | PDF/xlsx/DOCX → chunks → Neo4j sparse FTS + async vector embeddings | ✅ | `documents`, `knowledge` |
| Graph + vector store | Neo4j Community + APOC + GDS, 384-d HNSW + fulltext | ✅ | `knowledge` |
| Agent-memory MCP | Entities / facts / preferences / sessions via the memory MCP server | ✅ | `mcp/manager` (catalog), `agent/mcptools` |
| User profile | Per-identity `Agent.md` (atomic writes), injected as a protected block | ✅ | `profile`, `onboarding` |
| Identity + capabilities | Single-user identity + capability grants (multi-user scaffolding) | ✅ | `identity` |

## LLM & provider

| Capability | What it does | Status | Key packages |
|---|---|---|---|
| Provider-neutral client | Streaming `llm.Client` interface; no vendor SDK | ✅ | `llm` |
| OpenAI-compatible SSE | Hand-rolled streaming client (idle watchdog, tool-call accumulation) | ✅ | `llm/openai_compat` |
| Default model | DeepSeek-V4 over OpenRouter; swap by config | ✅ | `llm` (`config`) |
| Cost tracking | Provider cost preferred, price-table fallback; `aura cache-stats` | ✅ | `llm` (`prices`), `cachemetrics` |
| Circuit breaker + retry | Bounded stream-open retry with Retry-After awareness | ✅ | `agent`, `llm` (`breaker`) |
| KV-cache discipline | Byte-stable `messages[0]`; volatile data appended after history | ✅ | `agent/prompt` |
| Multimodal (vision) | Capability-gated image routing (minimax-m3 cloud / OCR sidecar) | ✅/🟡 | `llm` (`models`), `channels/telegram` |
| Local LLM fallback | vLLM + LMCache dual sidecar for offline operation | 🔭 | roadmap (Slice 13) |

## Channels & transport

| Capability | What it does | Status | Key packages |
|---|---|---|---|
| CLI agent REPL | `aura chat` / `aura shell` — primary interactive operator surface | ✅ | `cmd/aura`, `channels` |
| Telegram | Primary user-facing channel: renderer, HITL keyboards, status pane | ✅ | `channels/telegram` |
| Telegram multimodal | Voice (STT), photo (vision/OCR), document ingest | ✅ | `channels/telegram` |
| Setup wizard | Loopback HTTP + QR pairing for a Telegram bot | ✅ | `setup` |
| AG-UI / SSE | Event-protocol transport (one-way Event → AG-UI bridge) | ✅ | `agui` |
| Web cockpit | Embedded Vite/React + assistant-ui over AG-UI/SSE | 🟡 | v1.0.0 milestone |

## Self-extension (skills & MCP)

| Capability | What it does | Status | Key packages |
|---|---|---|---|
| Instruction skills | Markdown SKILL.md, loaded on-demand by frontmatter description | ✅ | `skills` |
| Executable snippets | Multi-language code snippets with pattern analysis + TTL archive | ✅ | `skills` |
| Skill governance | Validate / gate (risk tier) / audit skill mutations | ✅ | `skills`, `scoring`, `db` |
| MCP client | Generic JSON-RPC client (stdio + Streamable-HTTP) | ✅ | `mcp` |
| MCP tool bridge | Namespaced, trust-framed, deferred-by-default tool mounting | ✅ | `agent/mcptools` |
| MCP governance | Managed config, trust classes, docker/local launch, recipe catalog | ✅ | `mcp/manager` |
| Recipe catalog | calculator · calendar · mail · whatsapp · memory | ✅ | `mcp/manager` (`catalog`) |

## Automation & operations

| Capability | What it does | Status | Key packages |
|---|---|---|---|
| Scheduler | Cron tick loop + crash recovery; agent jobs, reminders, backups | ✅ | `cron`, `cron/handlers` |
| Onboarding | Interview (LoopAgent) → LLM-extracted facts → standard `Agent.md` | ✅/🟡 | `onboarding` |
| Risk-Based governance | Qualitative tier scoring for advisory gates | ✅ | `scoring` |
| Full-stack health | `aura doctor` — Postgres / Neo4j / embedding / sidecar checks | ✅ | `cmd/aura` |
| Forensic ledgers | Append-only, un-deletable tool-invocation + skill + profile audit | ✅ | `toolinvocations`, `db` |
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

`aura chat` · `aura shell` · `aura doctor` · `aura tools` · `aura docs {ingest|search|status|list|bench}`
· `aura task {schedule|list|cancel|run_now|approve|runs|doctor}` · `aura skills {list|info|create|update|delete|approve|always|snippet|audit}`
· `aura mcp {recipes|install|add|profile|trust|status|logs|list|doctor|tools|enable|disable|remove}`
· `aura memory <verb>` · `aura profile {show|add-fact}` · `aura identity {list|get|grant|revoke}`
· `aura config {show|get|set}` · `aura cache-stats --since=<dur>` · `aura web {doctor|tool}`
· `aura db {migrate|ping|status|reset}` · `aura neo4j {migrate|ping|status|reset|cypher}`
· `aura chat search <query>` · `aura paused-states {list|purge}` · `aura version`
