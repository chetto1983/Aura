# Aura — Technical Overview

*A local-first, provider-neutral AI agent platform in Go.*

**Audience:** technical decision-makers (CTO / engineering due-diligence). **Date:** 2026-07-17.
**Companion docs:** [ARCHITECTURE.md](ARCHITECTURE.md) · [CAPABILITIES.md](CAPABILITIES.md) · [`.planning/codebase/`](../.planning/codebase/)

---

## 1. Executive summary

Aura is a private AI agent platform that runs on hardware the customer owns. A single Go
binary provides the agent runtime, a broad tool surface (host terminal + filesystem, web,
documents, scheduling, self-authored skills), multi-channel access (CLI, Telegram, and an
embedded web cockpit over AG-UI/SSE), and a graph-backed memory — talking to a swappable
LLM (DeepSeek-V4 over OpenRouter by default) and a set of local GPU/CPU sidecars.

Scale: **~98k LOC of non-test Go** across **68 internal packages** (of which ~7k LOC is
sqlc-generated), against **~143k LOC of tests** (≈1.44:1). The engineering gate is
enforced in CI: an **owned-surface coverage floor of ≥85%** per package, race +
goroutine-leak + property + mutation testing, `golangci-lint` / `staticcheck` /
`govulncheck` / CodeQL, and a hard architectural cap of 600 LOC per file.

**Maturity — stated plainly.** An internal industrial audit (2026-06-21) assessed the
system at **4.6/10** on production-readiness and raised **51 security findings**
(F-001..F-052) alongside a **~64-finding quality audit**. The current milestone (v2.0.0,
"Industrial Hardening & Multi-User Production") exists to close them to an honest 10/10.
**8 of 12 phases are complete**; the formal closeout (Phase 41) is **still open**. The
substrate and the web cockpit are shipped and tagged (v1.0.1); the industrialization of
MCP governance, observability, supply-chain, and production ops is in progress. Section 6
gives the per-phase state without rounding up.

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

- **Runs on your hardware**, one binary plus a Docker Compose sidecar stack. No
  conversation, document, or credential has to leave the machine except the model call
  itself — which is itself swappable.
- **Multi-tenant by construction** — per-identity isolation enforced at the database
  (row-level security, owner-scoped queries) and audited in an append-only ledger; web
  authentication via the embedded Authula provider. Aura began as a single-operator tool;
  identity isolation was retrofitted deliberately in v2.0.0 (Phase 36) and is now the
  default posture, not an add-on.
- **Mediated host access** — the agent has real operating power (host shell, filesystem),
  but every tool call passes a **policy enforcement point** (the Gateway) that classifies
  it, decides allow/deny/approve, and records a reservation in a durable ledger before
  execution. Untrusted or multi-user work additionally runs in a **per-user Docker
  sandbox** with its own filesystem and egress control.
- **Provider-neutral** — the model is an interface, swapped by configuration. OpenRouter
  and llama.cpp are both recognized provider targets in the LLM layer; a fully-offline
  local-model deployment is on the roadmap.
- **Self-extending** — the agent can author and run its own skills, and mount third-party
  capabilities over MCP, so the tool surface grows without a release.
- **Remembers** — a Postgres + Neo4j memory stores conversations, documents (as a
  searchable graph), and learned facts, and feeds them back into the agent, with a
  deterministic context ladder + on-demand graph-memory recall keeping long threads bounded.
- **Multi-channel** — the same agent is reachable from a terminal REPL, from Telegram
  (with voice, photo, and document support), and from an embedded web cockpit.

## 4. Differentiators

What a technical reviewer should notice — each is implemented and locatable in the tree:

| # | Differentiator | Why it matters | Where it lives |
|---|---|---|---|
| 1 | **Gateway policy enforcement point** | Every tool call is classified and adjudicated (allow / deny / operator-approve) before execution, and reserved in an append-only ledger keyed by `(conversation, request, tool_call)` — giving idempotent replay and a forensic record. Approval challenges are server-issued and argument-bound, closing the confused-deputy class. | `gateway` (`decide.go`, `classify.go`, `approve.go`, `reserve.go`, `reconcile.go`) |
| 2 | **Multi-user identity isolation** | Every conversation, asset, and tool invocation is owner-scoped, with row-level security as a backstop rather than the only guard, plus an identity audit ledger and a recovery path. Web auth is delegated to the embedded Authula provider. | `identity`, `identityctx`, `agui/auth.go`, migrations `0019`/`0021`/`0023` |
| 3 | **Per-user full-capability sandbox** | Untrusted or per-tenant work runs in a dedicated Docker box with its own workspace volume, lifecycle/reaping, cross-identity denial, and network egress control — so capability and isolation are not a trade-off. | `sandbox/usersandbox` (`docker_backend*.go`) |
| 4 | **Deferred-tool + semantic discovery** | The per-turn prompt stays small even with dozens of tools (incl. dynamic MCP tools). The model finds tools via an embedding `tool_search` instead of carrying every spec every turn. Scales the tool surface at near-zero token cost. | `agent/tools` (registry, `tool_search`), `semindex` |
| 5 | **KV-cache economics** | `messages[0]` is byte-identical across turns and workers; volatile data is appended after history. On a cache-friendly provider this is a large recurring cost saving, observable via `aura cache-stats` and guarded by a dedicated CI job. | `agent/prompt` (builder, hash), `conversations`, CI `cache-invariant` |
| 6 | **Deterministic context ladder + graph memory** | Long threads stay bounded by a three-tier deterministic ladder — L1 tool-output eviction to sidecar pointers, L2 token budget, L2.5 oldest-pair drop (no LLM call, provider-agnostic) — while salient facts are extracted into the Neo4j graph and recalled on demand (L4), so working context stays small by design rather than by summarizing history. | `conversations/context.go`, `agent/mcptools` (`memory_search`), migration `0017` |
| 7 | **Bounded, leak-safe agent loops** | A shared budget tree caps steps + wall-clock across an entire agent tree; a two-phase dedup ring kills tool-call loops; parallel fan-out is goroutine-leak-tested. Predictable cost and no runaways. | `agent` (Budget, dedup), `agent/workflow`, `swarm` |
| 8 | **Adaptive reasoning without an extra round-trip** | A local curated-seed embedding classifier routes each turn to `none/low/high` reasoning instead of spending an LLM round-trip to decide. Per-turn effort can also be fixed explicitly from the cockpit. | `agent/prompt` (classifier), `reasoningtrace`, `semindex` |
| 9 | **Graph-native memory + two-stage retrieval** | Documents become a searchable Neo4j graph (sparse FTS + **1024-d** HNSW vectors, Qwen3-Embedding-0.6B); a reranker sidecar runs a second retrieval stage. | `knowledge`, `documents`, `rerank` |
| 10 | **Self-extension** | The agent authors, validates, and runs its own skills (instruction + executable snippets) and mounts MCP servers from a curated recipe catalog. | `skills`, `agent/mcptools`, `mcp/manager` |
| 11 | **Trust boundaries by construction** | MCP output, web pages, and documents are framed/wrapped as untrusted before re-entering the prompt; secrets are redacted at every egress through one shared denylist. | `agent` (trust), `mcptools`, `secret`, `toolinvocations` |
| 12 | **Embedded operator cockpit** | A Vite/React + assistant-ui web cockpit ships **inside the binary** (embedded dist, no separate deploy): chat, approval center, typed tool display, Neo4j graph explorer, governance boards, MCP config, and skill install. | `webui` (embedded dist), `agui` (AG-UI/SSE) |

## 5. Capability surface (summary)

Full matrix in [CAPABILITIES.md](CAPABILITIES.md). At a glance:

- **Reasoning & tools** — streaming agent loop; **21 built-in tools** + dynamic MCP tools,
  all mediated by the Gateway.
- **Knowledge** — document ingestion (PDF/xlsx/DOCX) → cited graph search with rerank;
  conversation memory with a deterministic context-management ladder.
- **Web** — SearXNG search + SSRF-hardened fetch → readable markdown.
- **Automation** — cron scheduler with agent jobs, reminders, and backups.
- **Channels** — CLI REPL, Telegram (voice/photo/docs/HITL), embedded web cockpit.
- **Identity & access** — multi-user identities, Authula-backed web auth, capability
  grants, approval center, break-glass path, audit ledgers.
- **Self-extension** — skills (author + run), MCP recipes (calculator, calendar,
  whatsapp, memory).
- **Operations** — `aura doctor` full-stack health, migrations, cache stats, audit ledgers.

## 6. Engineering quality & maturity

Aura is held to a gate enforced in CI and runnable locally. Stated without rounding up:

- **Test coverage** — an **owned-surface floor of ≥85%** per package, enforced by
  `scripts/coverage_gate.sh` (`AURA_COVERAGE_MIN`, default 85) in CI. This overrides the
  looser PRD targets. This document deliberately carries **no punctual coverage figure**:
  the gate is the durable, checkable claim; a snapshot number goes stale the week it is
  written. Current measurements live in `docs/aura-quality-snapshot.md`.
- **Test discipline** — table-driven + property-based (gopter/rapid) + fuzzing; race
  detector and `goleak` on concurrent code; mutation spot-checks held to a ≥70%-killed
  floor on each phase's critical files, recorded in the phase `VALIDATION.md`.
- **Integration tiers — the honest position.** `db_integration` and `neo4j_integration`
  are the two tiers the coverage gate runs, and they are wired in CI: their skip-helpers
  `t.Fatal` when the required env is unset under `$CI`, so a silently-skipped tier fails
  the gate rather than passing it. **`docker_integration` (sandbox lifecycle, exec, and
  egress) is not one of them** — it needs a Docker daemon, so in CI it compiles and skips,
  contributing **zero** coverage. Those paths are exercised locally and on WSL/native
  Linux (Phase 37 was live-verified on native dockerd, 2026-07-08); a native-Linux CI job
  to close the gap is tracked as **WR-01**. Daemon-gated logic therefore carries
  daemon-free unit tests for its pure parts so the floor stays meaningful.
- **Static + supply-chain** — `golangci-lint` (incl. `dupl`), `staticcheck`,
  `govulncheck`, and CodeQL, as CI jobs (`build-and-lint`, `vulncheck`, `codeql`).
- **Architectural rules** — every file ≤600 LOC (`scripts/check-file-size.sh`, CAP=600);
  no god packages; one concept per package; deferred-tool pattern; consumer-declared
  interfaces to break import cycles; byte-stable cache invariants asserted by a dedicated
  `cache-invariant` CI job.
- **Persistence** — **40 Postgres migrations** (`0001`..`0040`) + **7 Cypher migrations**,
  two-role DB separation, row-level security, atomic per-turn writes, append-only forensic
  ledgers.
- **Observability** — OTel spans end-to-end, Prometheus + expvar metrics, panic counters,
  a redacting reasoning trace, and an un-deletable tool-invocation ledger. Deepening this
  into a full idempotency + observability pack is Phase 39, **open**.

**Status.** Two milestones shipped: **v0.0.0 substrate** (Phases 0–21, 2026-06-15) and
**v1.0.0 web cockpit** (Phases 22–30, 2026-06-29). Latest tag **v1.0.1**.

**v2.0.0 Industrial Hardening & Multi-User Production** (Phases 31–42) is **in progress —
8 of 12 complete**:

| Phase | Scope | State |
|---|---|---|
| 31 | Stabilization & CI unblock | ✅ 2026-06-29 |
| 32 | Quality cleanup — dead code + shared helpers | ✅ 2026-06-30 |
| 33 | Runtime profiles + config validation | ✅ |
| 34 | Agent-loop correctness + durable ledger | ✅ 2026-07-03 |
| 35 | ToolGateway + policy engine (closes F-001) | ✅ 2026-07-04 |
| 36 | Multi-user identity isolation + Authula cutover | ✅ |
| 37 | Per-user full-capability sandbox (closes F-001) | ✅ 2026-07-08 |
| 42 | Industrial conversation compaction | ❌ removed 2026-07-20 (dark; Amendment #86) |
| 38 | MCP governance hardening | ⬜ open |
| 39 | Idempotency + observability pack | ⬜ open |
| 40 | Security & supply-chain pack | ⬜ open |
| 41 | Production ops + capability-eval + **honest 10/10 closeout** | ⬜ open |

Known residual gaps, tracked rather than papered over: the native-Linux `docker_integration`
CI job (WR-01), a 32 GB sandbox soak envelope and a gVisor `runsc` smoke (both hardware-gated,
listed as Phase-41 release must-runs), and a `SECURITY.md` threat-mitigation retro-verification
for the sandbox phase.

## 7. Deployment & operations

- **Footprint** — one Go binary (cockpit embedded) + Docker Compose for Postgres, Neo4j,
  and the sidecars: embedding, reranker, document extractor, and optional OCR/STT/TTS.
- **Accelerator posture — GPU-first.** The product default offloads the embedding model
  fully to the GPU (`AURA_EMBED_NGL=99`, Qwen3-Embedding-0.6B), and the
  reranker is a GPU sidecar. The base Compose file is CPU-startable so CI and the
  installer work on accelerator-less hosts, with the GPU layer applied via
  `compose.gpu.yaml` — but **CPU-only is the fallback, not the target**. A deployment
  without a GPU runs the retrieval path in a degraded mode. This makes the DGX
  Spark-class target the natural fit; it also means "any 16-core mini-PC" is not a
  supported full-capability configuration, and the entry-tier hardware floor is a
  commercial question (see §8), not an engineering one.
- **Configuration** — a locked 4-tier precedence (built-in default → `.env` →
  `~/.aura/llm.json` → `AURA_LLM_*` / `AURA_*` env; see `internal/llm/config.go`),
  `AURA_<DOMAIN>_<UNIT>` naming, secrets never logged.
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

**Shipped** — v0.0.0 substrate (agent runtime, tools, skills, MCP, memory, channels);
v1.0.0 embedded web cockpit (chat, approvals, typed tool display, graph view, governance,
MCP config + skill install); v2.0.0 Phases 31–37 (Gateway PEP, multi-user identity
isolation + Authula, per-user sandbox).

**In flight (v2.0.0)** — MCP governance hardening (38), idempotency + observability pack
(39), security & supply-chain pack (40), production ops + the honest-10/10 closeout (41).

**Beyond** — a fully-offline local-model deployment (llama.cpp is already a recognized
provider target in the LLM layer; the packaged offline path is not yet shipped); an
**orchestrator** planner→executor upgrade to the swarm (designed, deferred post-v1.0.0)
for plan→verify→synthesize multi-agent workflows.

---

*This document describes the system as implemented at the stated date, at commit-level
accuracy for structural figures (LOC, package/migration counts, phase state). It carries
no dated performance or coverage measurement by design — those live in
`docs/aura-quality-snapshot.md` and the phase `VALIDATION.md` records, which are
maintained as the code moves. Maturity is reported against the project's own internal
audit rather than against a marketing target.*
