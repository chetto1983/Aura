# Milestones

## v1.0.0 Aura Deep Search Web Cockpit (Shipped: 2026-06-29)

**Phases completed:** 9 phases (22–30), 45 plans, 113 tasks

**Delivered:** The embedded operator cockpit — a single-binary Vite + React + assistant-ui web UI over the AG-UI/SSE gateway — turning the v0.0.0 substrate into an operable Deep Search product: a hardened agent perimeter, streaming chat with cross-thread HITL approvals, a typed-display evidence protocol + Neo4j graph explorer, read/write governance surfaces, and GPU-reranked two-stage retrieval.

**Stats:**

- Git range: `c3018841` (v0.0.0 close) → `4d873ca3` — 538 commits, ~1,800 files, +200,057 / −5,289 LOC
- Timeline: 2026-06-15 → 2026-06-29 (~14 days)
- Closeout: **override_closeout** — milestone audit `passed` (56/56 requirements, 9/9 phases, 10/10 E2E flows). Known verification overrides: **6** (see STATE.md Deferred Items)

**Key accomplishments:**

- **Agent perimeter hardening (Phase 22)** — `internal/agent` made production-ready before any web exposure: a panic crash-firewall (race + goleak clean), a secret boundary (subprocess creds stripped/redacted, no verbatim reasoning history), MCP resilience (off-lock reconnect + circuit breaker + bounded timeouts), active budget/wallclock caps, and Prometheus turn-outcome + LLM-latency observability — the full `AG-001..064` finding ledger closed with zero audit residue.
- **Embedded single-binary cockpit foundation (Phases 23–24)** — a research-locked React 19 / Vite 8 / TS 6 / Tailwind 4 foundation behind a zero-warning lint/format/type-check CI gate; the dark-operator token theme applied before paint; `//go:embed all:dist` mounted additively into `aura serve` (AG-UI routes keep priority); an HMAC `__Host-` signed-session auth boundary that finally exercises the dormant `capability_grants` scaffolding, a fail-fast non-loopback boot guard, and a read-only runtime health shell.
- **Core-Value chat + approval center (Phase 25)** — an assistant-ui chat lane streaming the AG-UI/SSE event stream token-by-token, a conversation manager (FTS search, rename, archive-first, focus-trapped hard-delete), a Tokens·Cache·Cost·Context footer, a cross-thread HITL approval queue (accept/decline/cancel + terminal-state rendering), and D-09 conversation branch trees — all preserving the `messages[0]` KV-cache byte-invariant.
- **Typed-display protocol + graph explorer (Phases 26–27)** — a namespaced `aura.display` CUSTOM event + Go normalizer feeding a `switch(payload.type)` display router (web/document/code/table/chart/system_event/swarm_report/local_artifact, citation bubbles, source explorer), and a read-only Neo4j WebGL graph explorer (graph-normalizer + read-only Cypher guard + node inspector + path strip + a11y parallel-DOM).
- **Governance read + write surfaces (Phases 28–29)** — read-only MCP/skills/scheduler boards + a full-screen web onboarding wizard over the onboarding LoopAgent (cross-store provisioning saga, zero orphans on every failure-injection point), then the highest-risk write surfaces landing last: MCP install/env-redaction/enable-disable-remove (append-only `mcp_audit`) and skills install → risk-tiered approval queue → activate (operator-resume-only, never model-approve; both ledgers append-only).
- **Retrieval & memory hardening (Phase 30)** — a fail-soft GPU cross-encoder reranker (`internal/rerank`, Qwen3-Reranker-0.6B) + two-stage retrieval (vector seed → rerank seeds → `:NEXT_CHUNK` graph-expand winners) wired into both memory recall and document retrieval, full-document ingest across all markitdown formats, a non-monotonic rerank guard, RRF fallback, and an nDCG@10/Recall@5/MRR eval harness — GPU-host live tiers deferred-by-design but NO-SKIP-AS-GREEN + CI-floored.
- **Premium cockpit overhaul + product layer (post-Phase-25, not a formal phase)** — the logo-matched blue design system (Fraunces / Hanken Grotesk / Commit Mono, WCAG-AA contrast gate), a responsive shell (svh grid, drawers, edge-swipe, intent-restore, 380px chat floor), Authula embedded auth (flag-gated, superseding the passphrase cookie), an `aura.settings` settings page with env overlay, and calendar/PIM + WhatsApp connect.

**Quality:** owned-surface `make coverage` 88.1% (≥85% floor), frontend Vitest ≥85% + Stryker ≥70%, full Go + web CI green, the `messages[0]` cache-invariant gate green throughout.

---

## v0.0.0 Substrate (Shipped: 2026-06-15)

**Phases completed:** 24 phases (Phase 5 superseded by the Phase 8 sandbox-agent pivot), 144 plans, 233 tasks

**Delivered:** The tabula-rasa Go-native agentic substrate — a reliable multi-tool agent loop with identity, channels, skills, and memory as configurable overlays, end-to-end live-proven on the real stack.

**Key accomplishments:**

- **Persistence + knowledge substrate** — Postgres 17 with role separation (`aura_app` / `aura_migrate`), `pgxpool`, `golang-migrate` + `embed.FS`, `sqlc` bindings, 14 versioned migrations; Neo4j 5.26 Community (APOC + GDS + HNSW 768d vector index) driven exclusively through the `mcp-neo4j-cypher` MCP server.
- **Agent cornerstone + LLM client** — the open `Agent` interface + Sequential/Loop/Parallel workflow agents (adk-go shape, reimplemented not imported), single-Run `InvocationContext`, budget contract with `ErrBudgetExhausted`; a hand-rolled OpenAI-compat client (DeepSeek-V4 via OpenRouter) with ToolResult preview+sidecar, SSE streaming, and an additive reasoning/chain-of-thought data-plane.
- **HITL + conversations + KV cache** — crash-recoverable FIFO `ask_user` pause/resume, multi-thread Claude.ai-style conversations with `pg_trgm` FTS and an L1/L2/L2.5 microcompact context ladder; a provider-aware stable-prefix KV cache enforced by a cross-slice `messages[0]` byte-invariant CI gate.
- **Capabilities** — sandbox via rivetdev/sandbox-agent (host-primary `shell_exec` with deliberate container escalation), a budget-bounded leak-safe swarm coordinator, web tools (SearXNG `web_search` + readability `web_fetch` with IPv6/DNS-pin SSRF defense), a persistent scheduler (cron + `agent_job` with `FOR UPDATE SKIP LOCKED` + held-conn advisory lock + heartbeat + missed-catch-up), and a skills system (instruction-based + executable snippets, scoring-gated human-approved self-extension, append-only audit ledger).
- **Tool legibility** — the deferred-tool pattern plus semantic `tool_search` over a unified `internal/semindex` embedding substrate with a tool-selection active-learning loop.
- **Transport + channels** — the AG-UI SSE gateway (pure translator + REASONING lifecycle + drop-on-full fanout, `agent ⇸ agui` import boundary enforced); Telegram as the primary user channel (setup wizard, multimodal STT/OCR/TTS sidecars, command intercepts, inline HITL, artifact delivery, MarkdownV2 + table→PNG); onboarding LoopAgent + identity-aware `Agent.md` profile injected at `messages[1]`.
- **Memory** — adopted the forked `neo4j-labs/agent-memory` MCP sidecar off-the-shelf (POLE+O long/short-term + reasoning over Streamable HTTP), with Go wiring + `aura memory` CLI, superseding the bespoke build.
- **MCP manager + third-party trust** — managed config v2 (profiles, trust classes, recipe catalog, env redaction, redacted profile export), a common stdio + Streamable-HTTP transport, `doctor`/`status`/`logs`, and conservative mount-time risk-policy enforcement before tools enter the registry.
- **Snippet-reuse steady state** — host-primary executable-snippet save/activate/run-by-path collapsing the ~29-roundtrip re-authoring loop to ~5 calls / ~11s, grounded by the durable `tool_invocations` ledger.
- **Hardening** — Phase-19 audit fixes (shell never-answer cluster, SSE/HITL error-swallowing, microcompact wire-validity, LLM stream-error + MCP reconnect), Phase-20 scheduler origin-channel routing (reminders and deferred/failed sweeps route back to the scheduling channel identity-keyed), and the Phase-21 in-process `HookManager` (5 LlmAgent insertion points, first-non-nil-wins, in-process Go + trust-gated command-program hooks).
- **Quality** — owned-surface coverage 90.3% (every owned package ≥85%), mutation spot-checks ≥70% killed, goleak + race clean, no-skip-as-green CI (CI + CodeQL + Skills all green).

---
