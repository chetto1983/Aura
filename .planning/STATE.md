---
gsd_state_version: 1.0
milestone: v0.0.0
milestone_name: milestone
status: ready_to_plan
stopped_at: Phase 03 complete (5/5) — ready to discuss Phase 4
last_updated: 2026-05-30T16:07:32.958Z
last_activity: 2026-05-30
progress:
  total_phases: 16
  completed_phases: 3
  total_plans: 21
  completed_plans: 21
  percent: 19
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-05-29)

**Core value:** Substrate agentico domain-neutral — un runtime Go che esegue un agentic loop multi-tool affidabile con identity, channels, skills e memory come overlay configurabili.
**Current focus:** Phase 4 — hitl + identity + conversations

## Current Position

Phase: 4
Plan: Not started
Status: Ready to plan
Last activity: 2026-05-30

Progress: [██████████] 98%

## Performance Metrics

**Velocity:**

- Total plans completed: 11
- Average duration: —
- Total execution time: 0.0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 00 | 6 | - | - |
| 03 | 5 | - | - |

**Recent Trend:**

- Last 5 plans: —
- Trend: —

*Updated after each plan completion*
| Phase 02 P02-01 | 25min | 2 tasks | 4 files |
| Phase 02 P02-02 | ~5min | 2 tasks | 6 files |
| Phase 02 P02-03 | ~9min | 2 tasks | 5 files |
| Phase 02 P02-04 | ~12min | 2 tasks | 2 files |
| Phase 02 P02-05 | ~22min | 2 tasks | 7 files |
| Phase 02 P02-06 | ~28min | 2 tasks | 2 files |
| Phase 02 P02-07 | ~14min | 3 tasks | 12 files |
| Phase 03 P03-01 | ~40min | 2 tasks | 13 files |
| Phase 03 P03-02 | ~50min | 2 tasks | 15 files |
| Phase 03 P03-03 | 25min | 2 tasks | 10 files |
| Phase 03 P03-04 | ~45min | 2 tasks | 14 files |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.
Recent decisions affecting current work:

- Pre-init: PRD `prd.md` 4400 LOC locked as single source of truth (commit `b3faacbf`, 2026-05-27, validated by 4 parallel sub-agents)
- Pre-init: Tabula-rasa rewrite — prior implementation preserved at tag `pre-rewrite-2026-05-27`
- Roadmap: PROJECT_MODE=standard (Horizontal Layers) — 16 phases derived from PRD's 14 slices + P0 amendments; architecture-validated dependency chain enforced (P2 cornerstone, P6 KV cache deliberately near-late, P15 memory most downstream)
- [Phase ?]: canonicaljson NOT RFC-8785 (D-08/A3); uuid v1.6.0 direct + rapid v1.3.0 test-only added
- [Phase ?]: 02-02: Agent interface OPEN (no seal, D-01); SpanID [8]byte not uuid.UUID (D-16/A4 supersedes SPEC Req#1); Event byte-identical round-trip via custom MarshalJSON + eventWire (D-21); ErrBudgetExhausted exported sentinel (D-04)
- [Phase 02]: 02-03: Budget = single *atomic.Int32 shared by pointer across the tree (D-10) + TOCTOU decrement-then-check-then-restore (D-11); fail-fast NewBudgetFromEnv (D-06); Child forks dedup ring + passive soft cap, shares counter (D-09/D-12); two-tier dedup with consecutive-unchanged result-veto counter fails SAFE not open (D-18/A2); wallclock via injectable clock not synctest (W8)
- [Phase 02]: 02-04: shared internal/agent/agenttest mock package (D-07, one-direction import) — InfiniteToolCallAgent (SC#2, same tool call forever), EmitNThenEscalate, RecordingAgent, CountingAgent (SC#3, consumes shared ic.Budget only, never NewBudgetFromEnv — Pitfall 3 guard); iter.Seq2 yield discipline exercised by a runtime drain test + break-after-one no-panic test (D-22 footgun 2, W5)
- [Phase 02]: 02-05: SequentialAgent + LoopAgent in internal/agent/workflow (constructors return agent.Agent interface, structs exported — D-02). Budget consumed PER TOOL-CALL Event so the infinite SC#2 fixture hits max_steps (terminal Event REPLACES the would-be step Event → exactly 26 lines for the Plan-07 smoke). Shared-ring dedup via WithSubAgent (D-09); caller canonicalizes args (B2) → BeforeToolCall; AfterToolResult uses Event content as the progress veto (D-18). Budget exhaustion Event-only (D-04): StateDelta{termination_reason,limit_hit,steps_consumed}. SC#1 goleak.VerifyTestMain wired; SC#2 test exempts the tool from dedup (D-19) so max_steps wins. No synctest (W8). Coverage 93.8%
- [Phase 02]: 02-07: aura agent dry-run subcommand (cmd/aura/agent.go) drives a mock LoopAgent over InfiniteToolCallAgent through the real Budget tree, emits one Event per JSON line via Event.MarshalJSON (W7, NOT canonicaljson) with a shared UUIDv7 request_id on every line (SC#4). CLI>env>default precedence (D-06) via env injection around NewBudgetFromEnv (no new Budget setters — scope control); dry-run tool always appended to AURA_LOOP_DEDUP_EXEMPT_TOOLS so the constant-result fixture terminates on the HARD max_steps cap (26-line SC#2 contract). DESTRUCTIVE SUBSTITUTION (atomic, SPEC boundary): internal/agent/loop.go DELETED + cmd/aura/main.go case chat/chatOnce/stubClient removed + case agent wired. scripts/loop_budget_smoke.sh = SC#2 CI-grep contract (26 lines + limit_hit:max_steps) + B4 phase-close coverage gate scoped to the Phase-2 surface = 91.5% (>=85% CLAUDE.md floor; the literal ./internal/agent/... command's 54.7% was diluted by the pre-rewrite tools skeleton + other-slice db/neo4j CLI). A1-A7 + adk-go attribution verified PRD-first (W9); the four A7 env vars were drift-fixed into prd.md. go vet/build/test/-race all green, golangci-lint 0 issues module-wide, all files <=600 LOC
- [Phase 02]: 02-06: ParallelAgent in internal/agent/workflow (NewParallel returns agent.Agent — D-02) CLOSES INFRA-03. Adapts adk-go parallelagent channel choreography (Apache-2.0, attributed) with TWO divergences: D-03 escalate fires a captured context.CancelFunc (never a sentinel error → keeps errgroup.Wait()/error slot clean for D-04), D-05 cancelled/broken siblings drain (nil,nil) not ctx.Err(). errgroup fan-out + serial fan-in yield from the iterator frame (D-22 footgun 3) + synchronous per-Event ack backpressure. Leak-safety (D-23): defer cancel + defer close(done) + multi-arm selects + Go#61611 spawn guard. Two-step child IC (W6): WithContext(egCtx).WithSubAgent(sub) + Budget.Child(len(subs)) forks a distinct dedup ring while sharing the *atomic.Int32 (D-09/D-10). SC#3 proven: depth-3 fan-3 (9 leaf) tree consumes ≤25 total, NOT 25³. Corrected the RESEARCH skeleton's `_ = eg.Wait()` to forward real child errors through the error slot (D-04 observable). golang.org/x/sync promoted indirect→direct. Race-clean under -count=10; coverage 90.3%
- [Phase ?]: Phase 03 03-01: llm.Config 4-tier load-order with fail-fast empty-key ErrMissingAPIKey, APIKey structural-redacted (D-28); CostUSD provider-first/table/n-a never zero (D-18/D-23); config.Load composes LLM + AURA_OTEL_* otlp/localhost:4317; OTel v1.44.0 train pinned via otel_deps.go anchor; PRD A1-A5 amended first.
- [Phase 03]: 03-03: Tool.Execute migrated to (ToolResult, error) coupled (spec+text_response+search); tools.NewResult ctx-injected spillover (D-25) with .. /separator id validation before filepath.Join (T-03-07); read_tool_output BYTE ranges + unknown-id hard-fail (D-27/D-15); current_time RFC-3339 UTC+IANA never in messages[0] (D-08); sidecar-fail degrades clean (D-29)
- [Phase 03]: 03-04: LlmAgent (first real Agent) budget-gated tool-dispatch run-loop driving llm.Client; ConsumeStep BEFORE each call -> terminal Event reason max_steps/wallclock/dedup (never error slot, D-04); full-tree crypto/rand SpanID minting in tracing.go REPLACES otel_deps.go anchor (resolves Phase-2 agent.go:51-52 deferral); per-call llm.request span w/ model/provider/token attrs, NEVER api_key (D-28); byte-stable EN system prompt mechanism-not-enumeration + 'Always respond in Italian', no timestamp (D-08/D-09); Registry.RenderToolDefs() alphabetical ManifestEntry->llm.ToolDef (cache-stable, BLOCKER-4); sequential multi-tool dispatch RoleTool in tool_call_id order (D-14); error tool-result self-correction, infra-fail to error slot (D-15); length->[risposta troncata: max_tokens] no auto-continue (D-21); added llm.Usage+Chunk.Usage trailing chunk so usage reaches the span (Rule-3 blocking-fix). ConsumeStep reason is REAL 'wallclock' not AI-SPEC 'max_wallclock'. agenttest.FakeClient goleak-clean. race+goleak green, lint 0, all <=600 LOC.

### Pending Todos

[From .planning/todos/pending/ — ideas captured during sessions]

None yet.

### Blockers/Concerns

[Issues that affect future work]

- 8 Gate 1 DoR open questions tracked in research/SUMMARY.md "Gaps to Address" — resolve per-phase during plan-phase invocations.

## Deferred Items

Items acknowledged and carried forward from previous milestone close:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| LLM Fallback | LLM-V2-01 (vLLM + LMCache, Slice 13) | Deferred to v2 | 2026-05-29 (GPU-gated, DGX Spark bundle path) |
| Skills | SKILL-V2-01 (Slice 7f cross-conv cluster auto-suggest) | Deferred to v1.x | 2026-05-29 (Amendment #13 scope reduction) |
| Swarm | SWARM-V2-01 (full N-deep + DM-by-ID + tier-mapped) | Deferred to v2 | 2026-05-29 (Amendment #12 scope reduction) |

## Session Continuity

Last session: 2026-05-30T14:06:19.957Z
Stopped at: Phase 3 Plan 05 — checkpoint:human-verify (live aura chat acceptance). Tasks 1 (chat REPL + config + cost footer + two-stage Ctrl+C + OTel) and 2 (scripts/llm_smoke.sh) committed (4a5f312c, 51785099). Awaiting operator "approved" after running `bash scripts/llm_smoke.sh` with a real OPENROUTER_API_KEY and eyeballing streamed prose + a non-zero token+USD footer.
Resume file: .planning/phases/03-llm-client-toolresult/03-05-PLAN.md
