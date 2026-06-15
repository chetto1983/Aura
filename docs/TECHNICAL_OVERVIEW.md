# Aura — Technical Overview

*A local-first, provider-neutral AI agent platform in Go.*

**Audience:** technical decision-makers (CTO / engineering due-diligence). **Date:** 2026-06-15.
**Companion docs:** [ARCHITECTURE.md](ARCHITECTURE.md) · [CAPABILITIES.md](CAPABILITIES.md) · [CODEBASE_MAP.md](CODEBASE_MAP.md)

---

## 1. Executive summary

Aura is a personal AI agent that runs on the user's own hardware. A single Go binary
provides the agent runtime, a broad tool surface (full host terminal + filesystem, web,
documents, scheduling, self-authored skills), multi-channel access (CLI, Telegram,
AG-UI/SSE web), and a graph-backed memory — talking to a swappable LLM (DeepSeek-V4 over
OpenRouter by default) and a small set of local CPU sidecars.

It is engineered as a **product, not a prototype**: ~25k LOC of Go across 49 internal
packages, an industrial quality gate (owned-surface test coverage **90.3%**, hard floor
85%; race + goroutine-leak + property + mutation testing; green CI incl. CodeQL), and a
strict architectural discipline (no file >600 LOC, no god packages, consumer-declared
seams, byte-stable cache invariants).

The strategic context is a **hardware + software bundle**: Aura paired with an NVIDIA DGX
Spark-class local machine, targeted initially at Italian SMBs who want a capable private
assistant without sending their data to a third-party cloud.

## 2. The problem

Most "AI assistant" products are thin wrappers over a hosted model: your data leaves your
premises, you rent capability by the token with no cost control, the tool surface is
fixed, and the agent forgets everything between sessions. For a small business with
sensitive data and a finite budget, that is a poor fit:

- **Privacy / sovereignty** — conversations, documents, and credentials shouldn't have to
  transit a third-party SaaS.
- **Cost predictability** — naive agent loops burn tokens; without cache discipline and
  budget bounds, costs are unbounded and opaque.
- **Capability ceiling** — a fixed tool list can't grow with the user's needs.
- **No memory** — a stateless chatbot can't accumulate context about the user or the work.

## 3. What Aura is

A self-hostable agent platform that closes those gaps:

- **Runs locally**, one binary, one trusted operator. Full host access to terminal and
  files is a feature, not a risk to be sandboxed away — isolation is reserved for
  genuinely untrusted inputs.
- **Provider-neutral** — the model is an interface, swapped by configuration. Cloud
  frontier model today; a local vLLM fallback is on the roadmap for fully-offline operation.
- **Self-extending** — the agent can author and run its own skills, and mount third-party
  capabilities over MCP, so the tool surface grows without a release.
- **Remembers** — a Postgres + Neo4j memory stores conversations, documents (as a
  searchable graph), and learned facts, and feeds them back into the agent.
- **Multi-channel** — the same agent is reachable from a terminal REPL, from Telegram
  (with voice, photo, and document support), and from a web cockpit over AG-UI/SSE.

## 4. Differentiators

What a technical reviewer should notice — each is implemented, not aspirational:

| # | Differentiator | Why it matters | Where it lives |
|---|---|---|---|
| 1 | **Deferred-tool + semantic discovery** | The per-turn prompt stays small even with dozens of tools (incl. dynamic MCP tools). The model finds tools via an embedding `tool_search` instead of carrying every spec every turn. Scales the tool surface at near-zero token cost. | `agent/tools` (registry, `tool_search`, `bm25`, `semindex`) |
| 2 | **KV-cache economics** | `messages[0]` is byte-identical across turns and workers; volatile data is appended after history. On a cache-friendly provider this is a large recurring cost saving, measured by `aura cache-stats`. | `agent/prompt` (builder, hash), `conversations` |
| 3 | **Adaptive reasoning at ~10 ms** | A local granite-embedding classifier routes each turn to `none/low/high` reasoning instead of a per-turn LLM round-trip — the original latency root cause — and an async learner upgrades it toward the LLM oracle's accuracy with no added latency. | `agent/prompt` (classifier), `reasoning*`, `semindex`, `activelearn` |
| 4 | **Bounded, leak-safe agent loops** | A shared budget tree caps steps + wall-clock across an entire agent tree; a two-phase dedup ring kills tool-call loops; parallel fan-out is goroutine-leak-tested. Predictable cost and no runaways. | `agent` (Budget, dedup), `agent/workflow`, `swarm` |
| 5 | **Full-terminal agent** | First-class host shell (foreground + background jobs) and filesystem tools give the agent real operating power, with destructive-command approval gates and secret redaction. | `agent/tools` (`shell_exec`, `fs_*`) |
| 6 | **Graph-native memory** | Documents become a searchable Neo4j graph (sparse FTS + 384-d HNSW vectors); learned exemplars live in the same store and feed the self-improving routers. | `knowledge`, `documents`, `*store` |
| 7 | **Self-extension** | The agent authors, validates, and runs its own skills (instruction + executable snippets) and mounts MCP servers from a curated recipe catalog. | `skills`, `agent/mcptools`, `mcp/manager` |
| 8 | **Trust boundaries by construction** | MCP output, web pages, and documents are framed/wrapped as untrusted before re-entering the prompt; secrets are redacted at every egress through one shared denylist. | `agent` (trust), `mcptools`, `secret`, `toolinvocations` |

## 5. Capability surface (summary)

Full matrix in [CAPABILITIES.md](CAPABILITIES.md). At a glance:

- **Reasoning & tools** — streaming agent loop; ~21 built-in tools + dynamic MCP tools.
- **Knowledge** — document ingestion (PDF/xlsx/DOCX) → cited graph search; conversation
  memory with a context-management ladder.
- **Web** — SearXNG search + SSRF-hardened fetch → readable markdown.
- **Automation** — cron scheduler with agent jobs, reminders, and backups.
- **Channels** — CLI REPL, Telegram (voice/photo/docs/HITL), AG-UI/SSE web cockpit.
- **Self-extension** — skills (author + run), MCP recipes (calculator, mail, calendar,
  whatsapp, memory).
- **Operations** — `aura doctor` full-stack health, migrations, cache stats, audit ledgers.

## 6. Engineering quality & maturity

Aura is held to an industrial gate, enforced in CI and runnable locally:

- **Test coverage** — owned-surface **90.3%** (re-measured on the live integration stack),
  hard floor **85%** per package, overriding the looser PRD targets.
- **Test discipline** — table-driven + property-based (gopter/rapid) + fuzzing; race
  detector and `goleak` on concurrent code; mutation spot-checks (≥70% killed) on critical
  files; integration tiers (`db_integration`, `neo4j_integration`, `sandbox_integration`,
  `multimodal_integration`) that actually run in CI (a skipped tier fails the gate — no
  false-green).
- **Static + supply-chain** — `golangci-lint` (incl. `dupl`), `staticcheck`,
  `govulncheck`, and CodeQL in CI.
- **Architectural rules** — every file ≤600 LOC; no god packages; one concept per package;
  deferred-tool pattern; consumer-declared interfaces to break import cycles; byte-stable
  cache invariants asserted by tests.
- **Persistence** — 15 Postgres migrations + Cypher migrations, two-role DB separation,
  atomic per-turn writes, append-only forensic ledgers.
- **Observability** — OTel spans end-to-end, Prometheus + expvar metrics, panic counters,
  a redacting reasoning trace, and an un-deletable tool-invocation ledger.

**Status:** 20 of 23 build phases shipped; CI green (CI + CodeQL + Skills). The current
milestone (v1.0.0) is an embedded web "Deep Search Cockpit" over the AG-UI/SSE transport.

## 7. Deployment & operations

- **Footprint** — one Go binary + Docker Compose for Postgres, Neo4j, and the CPU sidecars
  (embedding, document extractor, optional OCR/STT/TTS). Designed for a 16-core mini-PC /
  DGX Spark-class machine; the embedding workload is CPU-first and thread-bounded so it
  never saturates the operator's machine.
- **Configuration** — a locked 4-tier precedence (built-in → `.env` → `~/.aura/llm.json` →
  `AURA_*` env), `AURA_<DOMAIN>_<UNIT>` naming, secrets never logged.
- **Operability** — `aura doctor` checks the full dependency stack; migrations, cache
  stats, MCP governance, skill audit, and pause-ledger purge are all first-class CLI verbs.

## 8. Go-to-market context

The commercial thesis is a **DGX Spark + Aura bundle** for Italian SMBs: a private,
capable assistant that runs entirely on a machine the customer owns, sold and supported
locally. Engineering completes Aura technically; the business motion (NVIDIA Partner
Program, sales, support, legal) is run separately. Because the substrate is
domain-neutral — "personal assistant" is a set of overlays, skills, and channel wiring on
top of a general agent platform — the same engine can be re-pointed at other verticals
without a rewrite.

## 9. Roadmap highlights

- **v1.0.0** — embedded web cockpit (Vite/React + assistant-ui over AG-UI/SSE): chat,
  approvals, typed tool display, graph view, governance, MCP config + skill install.
- **Local LLM fallback** — vLLM + LMCache dual sidecar for fully-offline operation.
- **Orchestrator** — a planner→executor upgrade to the swarm (designed, deferred
  post-v1.0.0) for plan→verify→synthesize multi-agent workflows.

---

*This document describes the system as implemented at the stated date. Figures (coverage,
phase count) are drawn from the project's own quality snapshot and CI.*
