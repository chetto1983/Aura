---
gsd_state_version: 1.0
milestone: v0.0.0
milestone_name: milestone
status: executing
stopped_at: 05-04-PLAN.md tasks 1-2 committed; Task 3 Gate-3 human-verify checkpoint PENDING (live escape-rate + userns-remap-live + docker.go mutation sign-off)
last_updated: "2026-06-01T19:10:00.000Z"
last_activity: 2026-06-01
progress:
  total_phases: 16
  completed_phases: 5
  total_plans: 30
  completed_plans: 29
  percent: 31
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-05-29)

**Core value:** Substrate agentico domain-neutral — un runtime Go che esegue un agentic loop multi-tool affidabile con identity, channels, skills e memory come overlay configurabili.
**Current focus:** Phase 05 — sandbox-2a-stateless

## Current Position

Phase: 05 (sandbox-2a-stateless) — EXECUTING
Plan: 4 of 4
Status: Ready to execute
Last activity: 2026-06-01

Progress: [██████████] 100%

## Performance Metrics

**Velocity:**

- Total plans completed: 16
- Average duration: —
- Total execution time: 0.0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 00 | 6 | - | - |
| 03 | 5 | - | - |
| 04 | 5 | - | - |

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
| Phase 04 P01 | 40min | 3 tasks | 29 files |
| Phase 04 P02 | ~35min | 2 tasks | 8 files |
| Phase 04 P03 | ~70min | 3 tasks | 13 files |
| Phase 04 P04 | ~75min | 3 tasks | 15 files |
| Phase 04 P05 | 30 | 3 tasks | 25 files |
| Phase 05 P05-01 | ~12min | 3 tasks | 3 files |
| Phase 05 P02 | ~18min | 3 tasks | 9 files |
| Phase 05 P03 | ~20min | 3 tasks | 11 files |
| Phase 05 P04 | ~18min | 2 tasks (+1 checkpoint) | 3 files |

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
- [Phase 04]: Phase 4 db substrate: db.WithTx is the single atomic-write seam; migrations 0003-0006 apply idempotently under aura_migrate/aura_app role separation
- [Phase 04]: CREATE INDEX CONCURRENTLY isolated as the sole statement in 0006; pg_trgm EXTENSION folded into 0005 (golang-migrate implicit-tx hazard)
- [Phase 04]: Spillover is a sidecar file via content_sidecar_path, not a conversation_spillover table (OQ3); FIFO tiebreaker token ASC (Pitfall 4)
- [Phase 04]: tiktoken-go@v0.1.8 added (operator-approved); llm.Config gains ContextWindow=1000000 + MaxOutputTokens=32768 for the L2 budget
- [Phase 04]: 04-03: HITL pause primitive (agent + store halves, CORE-02). ask_user is a NON-DEFERRED tool returning the *ErrAwaitingUserInput STRUCT sentinel (pure types, no DB — D-A1-04), validated for empty question/unknown kind/exactly-1-or->4 options/non-distinct labels/priority 0-100. Actions.AwaitingInput Event field (sibling to Escalate) round-trips byte-identically via pointer omitempty; carries OriginAgent for swarm forward-compat (D-A1-08). Pause DETECTION split into llm_agent_pause.go (AM-01): errors.As the sentinel BEFORE the generic err fallback, suppress the RoleTool, REWRITE the assistant msg to ask_user-only tool_calls (intra-turn exclusivity, D-A1-07), emit Event-only (never the iter.Seq2 error slot, D-A1-03 — mirrors budget exhaustion). A validation-failed ask_user is NOT a pause (RoleTool error, self-correct). askuser.Store COPIES the 04-02 canonical pattern: FIFO total order priority DESC/created_at ASC/token ASC (token tiebreaker MANDATORY — single-tx rows share created_at, Pitfall 4); crash recovery (fresh New(pool) sees rows); MarkResumed via pool.Exec (RowsAffected->ErrPauseNotFound, not the discard-tag :exec); MarkResumedBatch in-Store via db.WithTx (all-or-nothing rollback, no new sqlc query); AutoResolveForConversation (Loop.Stop Req#11); AM-02 {action,content} resumed_answer; NO timeout/expiry state (Req#4); NEVER imports internal/agent/tools. Agent stays DB-free, askuser stays tools-free — boundary held. Resume orchestration is the Runner's (04-05), NOT here. export_test.go HistoryForTest avoids an agenttest import cycle. db_integration -race -count=10 green; combined coverage askuser 89.5%/agent 95.3%/tools 96.3%; golangci-lint 0; all files <=600 LOC.
- [Phase 04]: 04-04: conversation persistence + deterministic context mgmt (CORE-04/CORE-05). conversations.Store COPIES the canonical pattern and extends it: AppendTurn folds turn INSERT + aggregates UPDATE into ONE db.WithTx (SC-2 crash atomicity — injected failure between them rolls back, no partial turn, live-verified); sidecar spill > AURA_CONVERSATION_TURN_CAP_BYTES happens BEFORE the tx (orphan file reconciled by boot scan, not part of DB atomicity); LoadHistory byte-identical (Req#8, pure fn of rows, sidecar rehydrated from disk); total_cost_usd aggregated in SQL via pgtype.Numeric delta at numeric(10,4) (exact, Pitfall 5); SearchConversationTurns wraps the LOCKED FTS verbatim. context.go L1/L2/L2.5 ladder: L1 rewrites a COPY (byte-identity holds) of role='tool' turns older than evict window to read_tool_output pointers, NEVER seq=1 (KV poisoning Pitfall 1); L2 hard_cap=ContextWindow-max(MaxOutputTokens,20000)-13000, 0.75x warn; L2.5 drops oldest user/assistant PAIR (system L0 preserved, remainder even) + ONE context_rot_events row via a narrow rotEmitter iface (unit-testable, no DB); SC-1 L1-alone writes ZERO rot rows (live-verified); over-hard+unreducible -> ErrContextWindowExceeded (REPL, never iter error slot). OFFLINE tiktoken: vendored cl100k_base.tiktoken blob + custom //go:embed BpeLoader + SetBpeLoader before GetEncoding + sync.Once cache — zero network, zero new dep (NOT tiktoken-go-loader). ScanOrphans symlink-guarded GC (Lstat never follows -> symlink unlinked not traversed, EoP T-04-14 live-verified) + tmp >24h sweep + audit-only size WARN (never purge). generateTitle best-effort llm.Client.Stream body (Runner owns WaitGroup/WithoutCancel, 04-05); errors never block chat. Scope held: NO Runner/composition root. Combined coverage 89.6%; golangci-lint 0; db_integration -race green; all files <=600 LOC.
- [Phase 04]: 04-02: identity.Store PROVES THE CANONICAL STORE PATTERN (D-A4-01) that 04-03/04/05 copy verbatim — Store{pool,q} via sqlc.New(pool); reads via s.q; db.WithTx for atomic writes; SQLSTATE classification via errors.As(&pgErr)+pgErr.Code (NEVER message-match); sentinel errors (ErrWildcardManaged/ErrInvalidCapability/ErrIdentityNotFound); pgtype boundary conversion in fromRow; NO interface in the domain package (consumer declares it, D-A2-02); pre-DB validation gate for operator input. HasCapability wildcard-or-exact; grant/revoke idempotent (ON CONFLICT DO NOTHING + defensive 23505 swallow); '*'/name-grammar rejected pre-DB; FK cascade on DeleteIdentity. CLI: hand-rolled switch runIdentity mirroring runDB (cobra->switch deviation, RESEARCH OQ1/A2 — go.mod has no cobra, CLAUDE.md follows existing patterns, SPEC never requires cobra; no PRD amendment). Test discipline: unit tier (pure logic) + db_integration tier (goleak, envOrSkip t.Fatal-under-CI); 9 integration tests RAN green; combined coverage 98.0%; golangci-lint 0; all files <=600 LOC.
- [Phase ?]: Runner is the SOLE writer of paused_states; resume = fresh agent.Run over rehydrated history (SC-4 no silent re-run)
- [Phase 05]: 05-01 (PRD-amendment gate, doc-only): D12 RE-DECIDED to gVisor-primary x86 (amendment #36, supersedes #32; D-05/06/07) — gVisor `runsc` is the PRIMARY x86 boundary (not a >5%-only escalation seam), hardened-container+seccomp+userns-remap is the portable floor / arm64 fallback, runner runtime-agnostic via AURA_SANDBOX_RUNTIME, microVM stays REJECTED (KVM-less infra); applied consistently across prd.md + DECISIONS.md + ROADMAP.md SC#5. D-09: DockerRunner does ONE best-effort docker-CLI-gated auto-start on connect-failure then ErrSandboxUnreachable, NEVER mounts the docker socket, execution path stays HTTP-only (Slice 2a acceptance #4 = "sidecar down AND auto-start fails -> clear error"). D-20 (amendment #37): curated hash-pinned requirements.txt (numpy/pandas/scipy/sympy/matplotlib/pillow/bs4/lxml/pyyaml/dateutil/openpyxl) baked at IMAGE-BUILD time so user code is batteries-included, runtime stays net-none + read_only + stateless with NO runtime pip; sidecar.py server stays stdlib-only; on-demand pip remains a 2b/Phase-8 pypi.org-allowlist capability. Tracked obligation: QEMU-arm64 CI seccomp emulation can diverge from real arm64 kernel -> real-DGX confirmation pre-production arm64. Task 1 was already committed (93e8c5a) by a prior run and verified in place; Tasks 2 (d924466) + 3 (5d8c46e) landed this run.
- [Phase ?]: 05-02 (sidecar artifacts wave): sidecar.py stdlib http.server (/exec/python+/exec/shell+/healthz, D-16 JSON contract, 1 MiB truncation, timeout/oom/pids limit_hit); Dockerfile python:3.12-slim manifest-list-digest-pinned non-root uid 65532 BUILD-time --require-hashes curated bake (numpy/pandas/scipy/sympy/matplotlib/pillow/bs4/lxml/pyyaml/dateutil/openpyxl + 12 transitive, amd64+arm64) + import smoke + ZERO runtime pip (D-20/D-20b); seccomp.json positive allowlist hardened from moby v27.5.1 by subtracting dangerous(ptrace/unshare/process_vm_readv/bpf/kexec_load/userfaultfd/mount)+network socket syscalls, 394 allowed, both arches by-name no numbers (D-10/D-11); compose aura-sandbox full CAP-01 SC#5 floor + urllib /healthz; compose.gvisor.yaml x86-only runsc overlay; make sandbox-up arch-gated operator default (gVisor default-on x86, runc+seccomp arm64, D-04/SC#5); userns-remap deferred to daemon.json (D-15). Docker unavailable -> live build/run/escape-bench DEFERRED to 05-04 CI DinD + Gate-3 (artifacts static-validated, all <=600 LOC). Commits acb23dd/cea5eba/baef9e1.
- [Phase ?]: 05-03: Runner interface extended to 3-arg timeoutSec (D-16/D-19); integration tier split into a tagged file with its own goleak TestMain; aura exec uses LoadDB + exit 70/71/64; FormatLean exported for tool+CLI reuse
- [Phase 05]: 05-04 (Gate-3 evidence, tasks 1-2 committed c3d90b0/c807553; Task 3 human-verify PENDING): scripts/sandbox_escape_bench.sh is a DETERMINISTIC SandboxEscapeBench port (no LLM driver) — 14 runtime/kernel live-denominator probes posted to the live /exec/python wire (escape only if the technique succeeds) + 4 config-regression assertions (docker socket/privileged/writable host mount/excess caps must stay 0, SEPARATE gate not in the denominator) + 4 explicit N/A kubernetes lines (auditable denominator, OQ1); escape-rate=escapes/applicable, FAIL on >=5% or any config-regression>0; asserts userns-remap LIVE (Pitfall 3, hard-fail under $CI); runs internal/sandbox/docker.go go-mutesting spot-check (>=70%, avito-tech path same as Makefile); writes escape-rate+mutation+QEMU-arm64 caveat into docs/aura-quality-snapshot.md. .github/workflows/sandbox.yml is the REQUIRED gating DinD job: runsc install + daemon.json(userns-remap+runsc) + QEMU arm64 buildx + live sidecar under runsc + sandbox_integration negative tier + live bench + docker.go mutation + arm64 leg + 85% coverage fold; exports AURA_SANDBOX_URL/_TIMEOUT_SEC/_RUNTIME+CI=true (no-skip-as-green); documents Pitfall 2 (inner sidecar keeps seccomp despite --privileged outer DinD) + Pitfall 3. Docker daemon unavailable here -> live escape-rate/userns-remap-live/mutation-score are CI-populated (NOT fabricated) and are exactly the human-verify checkpoint sign-off items. CAP-01 NOT marked complete (verifier's call post-checkpoint).

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

Last session: 2026-06-01T18:39:46.828Z
Stopped at: Completed 05-03-PLAN.md (Go runner + execute tool + aura exec CLI)
Resume file: None
