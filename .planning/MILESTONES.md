# Milestones

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
