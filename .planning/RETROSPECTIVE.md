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

## Cross-Milestone Trends

### Process Evolution

| Milestone | Phases | Key Change |
|-----------|--------|------------|
| v0.0.0 | 24 | Established GSD 3-gate discipline, PRD-first, Codex-parallel execution, no-skip-as-green CI |

### Cumulative Quality

| Milestone | Plans | Coverage (owned) | Notable |
|-----------|-------|------------------|---------|
| v0.0.0 | 144 | 90.3% | Every owned package ≥85%; mutation ≥70%; CI + CodeQL + Skills green |

### Top Lessons (Verified Across Milestones)

1. Minimal industrial shape beats bespoke ambition.
2. Validate against live ground truth, not the agent's reply text.
