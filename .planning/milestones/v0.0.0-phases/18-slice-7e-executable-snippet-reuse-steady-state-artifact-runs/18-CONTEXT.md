# Phase 18: Slice 7e executable snippet reuse — steady-state artifact runs sotto i 40s - Context

**Gathered:** 2026-06-06
**Status:** Ready for planning
**Source:** Plan-phase inline Q&A (user answers to 18-RESEARCH.md Open Questions; full discuss-phase deliberately skipped)

<domain>
## Phase Boundary

Collapse the chat-surface xlsx artifact loop from 29–30 LLM roundtrips (151–233s) to a snippet-reuse steady state of ~5 calls under 40s. 7e-core snippets already shipped in Phase 11-07 (`internal/skills/snippet.go`, `snippet_usage.go`, `skill_ttl_sweep` cron, CLI, ro `/skills` mount) — this phase is **wiring + posture + measurement, not a greenfield build**: resolve the host-vs-sandbox execution posture left open at prd.md ~2190, give the model an in-loop snippet-save path, fill the `restore`/`archive` stubs, and make the <40s / call-budget acceptance machine-checkable from the `tool_invocations` ledger.

</domain>

<decisions>
## Implementation Decisions

### Execution posture (load-bearing)
- **D-01 — Host-primary for approved snippets.** Stored snippets run via host `shell_exec` by-path, like instruction skills: once saved they are vetted artifacts, not fresh model code. `sandbox_exec` remains available as deliberate per-run escalation. Supersedes 11-07's sandbox-by-path wording — requires the Wave-0 PRD amendment (prd.md §Slice 7e-core Idea/Acceptance/Componenti/commit-template + line ~2190 follow-up marked RESOLVED).

### Model-facing snippet save
- **D-02 — In-loop snippet-save action, UNGATED.** A `skill` action routes to `SaveSnippet` directly — Claude-Code parity, no ask_user ceremony (2026-06-05 no-ceremony directive). Future gating lands with capability_grants (Slice 1.7), not here.

### Call-budget grounding
- **D-03 — Wave-0 characterization task.** The first execution task runs one live xlsx E2E and dumps the per-call breakdown from the `tool_invocations` ledger to ground the call-budget acceptance BEFORE any code lands. No paid run during planning; the ~5-call target is provisional until that probe.

### Requirements mapping
- **D-04 — New CAP-08.1.** Add CAP-08.1 ("snippet reuse steady-state") to REQUIREMENTS.md in the Wave-0 PRD amendment; CAP-08 is reconciled as completed-by-Phase-11. Phase 18 plans carry CAP-08.1 in their `requirements` field.

### Claude's Discretion
- Param-passing contract for stored snippets (argv vs env vs stdin) — pick what the shipped `UseSnippet` frame and host shell_exec support most naturally.
- Exact shape of the posture-parametrized `use` frame (RESEARCH Pattern 2) and the `restore`/`archive` ActionRouter fill-in (Pattern 1).
- How the chat-surface E2E gate asserts the steady-state window from the ledger (RESEARCH Pattern 3 — artifact-not-reply discipline mandatory).
- Host xlsx deps strategy (python3 + openpyxl not guaranteed on host): uv on-demand (D-36) vs documented host prerequisite — planner picks per RESEARCH §Environment Availability.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Truth source + amendments
- `prd.md` §Slice 7e-core (~2389–2600) — Componenti / Smoke / Acceptance (amended #48) / Open questions / commit template
- `prd.md` amendment #48 (~2116) — skill tool with `action` enum, by-path execution seam, migration floor
- `prd.md` amendment #53/D-42 (commit `b3546824`) + line ~2190 — fs-tool authoring, host pivot, snippet-posture follow-up (resolved by D-01 above)

### Shipped substrate (Phase 11)
- `.planning/phases/11-skills/11-CONTEXT.md` — locked decisions D-01..D-38 of the skills system
- `.planning/phases/11-skills/11-07-PLAN.md` + summary — what 7e-core shipped (snippet store, TTL sweep, CLI, `/skills` mount)
- `internal/skills/snippet.go`, `internal/skills/snippet_usage.go`, `internal/cron/handlers/skill_ttl.go`, `cmd/aura/skills_snippet.go`

### Measurement substrate (in-flight, uncommitted)
- `internal/toolinvocations/` + `internal/db/migrations/0011_tool_invocations.*` + `internal/runner/runner_persist.go` wiring — the ledger the acceptance reads

### This phase's research
- `.planning/phases/18-slice-7e-executable-snippet-reuse-steady-state-artifact-runs/18-RESEARCH.md` — patterns, pitfalls, validation architecture, environment availability

</canonical_refs>

<specifics>
## Specific Ideas

- Driver metrics (registration commit `0c7267b0`): 151–233s per xlsx run, 29–30 roundtrips, prefix cache 90–95% hit (NOT the bottleneck); target steady-state <40s at ~5 calls.
- Acceptance must measure from the `tool_invocations` ledger / filesystem ground truth, never `r.Reply` (probe-must-verify-artifact rule).
- Eval registry must mirror the production registry (2026-06-06 directive) — no eval-only seams.
- No new migration for snippets: live-state stays in the sidecar JSON (Phase 11 D-19); snippet-run forensics ride `tool_invocations` filtered by tool name.

</specifics>

<deferred>
## Deferred Ideas

- **Slice 7f / SKILL-V2-01** — cross-conversation pattern analyzer auto-suggest (Neo4j HNSW clustering). Explicitly OUT of v1.
- **Neo4j `:UserSnippet` mirror** (prd.md ~3538) — deferred with 7f; v1 discovery stays BM25 (Phase 11 D-21).
- **Machine-profile env pinning** (Slice 10 Agent.md, ~7 probe calls) — secondary lever registered in STATE.md, out of scope here.
- **capability_grants gating of snippet save** — Slice 1.7 scope.

</deferred>

---

*Phase: 18-slice-7e-executable-snippet-reuse-steady-state-artifact-runs*
*Context gathered: 2026-06-06 via plan-phase inline Q&A*
