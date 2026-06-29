# Project Retrospective

*A living document updated after each milestone. Lessons feed forward into future planning.*

## Milestone: v0.0.0 — Substrate

**Shipped:** 2026-06-15
**Phases:** 24 | **Plans:** 144 | **Tasks:** 233

### What Was Built

- A complete Go-native agentic substrate: PG+Neo4j persistence, the `Agent` interface + workflow agents, a hand-rolled OpenAI-compat LLM client, HITL/`ask_user`, multi-thread conversations + microcompact, provider-aware KV cache, sandbox (sandbox-agent), swarm, web tools, persistent scheduler, skills + executable snippets, AG-UI SSE gateway, Telegram multimodal channel, onboarding + Agent.md, agent-memory MCP, MCP manager, and in-process hooks.
- Industrial quality gates: owned-surface coverage 90.3% (every owned package ≥85%), mutation ≥70%, goleak + race clean, no-skip-as-green CI (CI + CodeQL + Skills).

### What Worked

- **PRD-first discipline** — locking `prd.md` as the truth-source (validated by 4 parallel sub-agents + 3 review rounds) kept 24 phases coherent.
- **Off-the-shelf over bespoke** — adopting rivetdev/sandbox-agent (D-15) and the forked neo4j-labs/agent-memory MCP (amendment #61) replaced ~thousands of LOC of fragile custom code.
- **Codex parallel-session pattern** — Codex did heavy implementation atomically; Claude reviewed/committed/validated. Precision over speed.
- **Reusable substrates** — the deferred-tool pattern + a single `internal/semindex` embedding core served tool_search, the reasoning classifier, and active learning.
- **Live ground-truth validation** — every phase closed against the real stack (real DeepSeek-V4 agent, live Telegram via CDP), not compile-checks.

### What Was Inefficient

- **Sandbox churn** — Phase 8 was built bespoke, rewritten twice ("nuclear bomb"), then replaced off-the-shelf. The minimal-industrial-shape search should have come first.
- **Router latency hunt** — the adaptive-reasoning router's per-turn LLM round-trip was the slowness root cause; an embedding classifier was the fix, found late.
- **Coverage campaign** — a dedicated 2026-06-13 pass was needed to lift 16 sub-floor packages to ≥85%, rather than holding the floor per-phase.

### Patterns Established

- Deferred-tool pattern (big tools out of the manifest, fetched via `tool_search`).
- Normalize/structure server-side; never regex natural-language prose.
- Host-primary `shell_exec`; sandbox is deliberate escalation only.
- All Neo4j + memory access flows through MCP servers (no native Go drivers).
- Scoring-gated, human-approved self-extension (skills never self-activate).
- Identity-keyed channel delivery (scheduler routes back to the origin channel).

### Key Lessons

1. Find the **minimal industrial shape** that meets the success criteria before building; flag PRD over-engineering early.
2. **Probe the runtime before planning waves** — ground-truth (DB/API/filesystem), not the model's reply.
3. **`golangci-lint` catches what audit agents miss** — run it before dead-code deletions.
4. **No skip-as-green** — integration tiers must actually run in CI (env exported, `t.Fatal` under `$CI`).
5. **Default to precision over speed** for implementation; delegate heavy multi-story work to Codex.

### Cost Observations

- Model mix: orchestration/planning on Opus; bulk implementation delegated to Codex CLI (parallel sessions).
- Notable: paid live runs (cot_eval, real-agent E2E) were batched and gated on explicit go — never run speculatively.

---

## Milestone: v1.0.0 — Aura Deep Search Web Cockpit

**Shipped:** 2026-06-29
**Phases:** 9 (22–30) | **Plans:** 45 | **Tasks:** 113

### What Was Built

- The embedded operator cockpit: a single-binary Vite + React + assistant-ui web UI over the AG-UI/SSE gateway — hardened agent perimeter (Phase 22), industrial frontend foundation (23), serve/auth/health host (24), streaming chat + cross-thread HITL approval center + branch trees (25), the `aura.display` typed-display protocol + router (26), the read-only Neo4j WebGL graph explorer (27), read/write governance surfaces + web onboarding (28–29), and GPU-reranked two-stage retrieval (30).
- A post-Phase-25 premium overhaul (not a formal phase): the logo-matched blue design system, Authula embedded auth, an `aura.settings` settings page, and calendar/PIM + WhatsApp connect.

### What Worked

- **Foundation-first directive** — building the research-locked frontend industrial foundation (Phase 23) and hardening the agent perimeter (Phase 22) *before* any feature code meant every later phase landed on a stable, gated base.
- **Risk-ordered surface sequencing** — read-only governance boards (28) before the write surfaces (29); the highest-abuse surfaces (MCP config, skills install) landed LAST, after auth + the approval center + read boards were proven.
- **Frontend gates mirrored the Go floors** — Vitest ≥85% + Stryker ≥70% + a zero-warning lint/format/type-check CI gate gave the `web/` package the same discipline as `internal/*`.
- **One cross-cutting invariant held everywhere** — the `messages[0]` KV-cache byte-invariant survived chat branch trees, the typed-display tail-inject, source-list injection, and Phase-30 retrieval wiring, enforced by a single CI gate.
- **Honest deferral over fake green** — the GPU live tiers this host can't run are NO-SKIP-AS-GREEN + CI-floored, not skipped-as-passing.

### What Was Inefficient

- **The cockpit overhaul ran outside formal GSD phases** — a large premium-bar rework of the Phase-23/24/25 surfaces accumulated as a big uncommitted working-tree layer for a stretch, tracked in `docs/cockpit-overhaul/` rather than as numbered phases. Powerful, but harder to audit than a phase chain.
- **Auth churn mid-milestone** — the Phase-24 HMAC passphrase cookie was superseded by Authula before the milestone closed, leaving two auth providers (flag-gated) converging late.
- **Hardware-gated verification** — a 4GB-GPU host forced 4 Phase-30 retrieval live tiers into deferred-by-design status; the rerank precision lift is proven by the eval harness but not on this machine end-to-end.
- **STATE.md frontmatter drift** — `completed_phases` / `stopped_at` lagged the real progress and needed re-baselining at close.

### Patterns Established

- **Typed-display normalizer protocol** — normalize tool output server-side into a typed `DisplayPayload` union; the frontend is a pure `switch(type)` router with a raw-card fallback (never null).
- **Operator-resume-only approval** — cockpit-initiated risky actions (skill install) mint an `ask_user` pause in the SAME unified approval queue and activate only on operator resume; no model-facing approve.
- **Fail-soft sidecar mirroring** — the GPU `RerankClient` mirrors `EmbeddingClient` and degrades to RRF/seed order with a nil error on every failure mode; the sidecar never blocks boot.
- **Read-surface-before-write-surface** ordering for governance.

### Key Lessons

1. **Foundation-first compounds** — locking the toolchain/theme/build + perimeter hardening before feature code removed whole classes of rework downstream.
2. **Defer on hardware limits with real harnesses** — when a host can't run a tier, ship a NO-SKIP-AS-GREEN + CI-floored harness and record it as a deferred override, never a green skip.
3. **One byte-level invariant, one gate** — a single cross-cutting CI invariant (`messages[0]`) is cheaper to defend than per-feature cache reasoning.
4. **In-place overhauls need phase-grade ledgers** — large non-phase reworks (`docs/cockpit-overhaul/`) should still carry specs + adversarial validation + per-surface implementation ledgers to stay auditable.

### Cost Observations

- Model mix: orchestration/planning on Opus; bulk implementation delegated to Codex CLI (parallel sessions).
- Notable: GPU-gated live retrieval runs were deferred rather than forced on inadequate hardware — no speculative paid/GPU runs.

---

## Cross-Milestone Trends

### Process Evolution

| Milestone | Phases | Key Change |
|-----------|--------|------------|
| v0.0.0 | 24 | Established GSD 3-gate discipline, PRD-first, Codex-parallel execution, no-skip-as-green CI |
| v1.0.0 | 9 | Foundation-first (perimeter + frontend infra before features), risk-ordered surfaces, frontend gates mirroring Go floors, override-close on hardware-gated tiers |

### Cumulative Quality

| Milestone | Plans | Coverage (owned) | Notable |
|-----------|-------|------------------|---------|
| v0.0.0 | 144 | 90.3% | Every owned package ≥85%; mutation ≥70%; CI + CodeQL + Skills green |
| v1.0.0 | 45 | 88.1% | Frontend Vitest ≥85% + Stryker ≥70%; full Go + web CI green; `messages[0]` invariant held; 6 GPU/live-CI tiers deferred (NO-SKIP-AS-GREEN) |

### Top Lessons (Verified Across Milestones)

1. Minimal industrial shape beats bespoke ambition.
2. Validate against live ground truth, not the agent's reply text.
3. Build the foundation (perimeter + toolchain) before features; it compounds.
4. Defer hardware-gated verification with real CI-floored harnesses, never a green skip.
