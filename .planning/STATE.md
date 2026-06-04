---
gsd_state_version: 1.0
milestone: v0.0.0
milestone_name: milestone
status: executing
stopped_at: Completed 16-02-PLAN.md
last_updated: "2026-06-04T14:12:18.975Z"
last_activity: 2026-06-04
progress:
  total_phases: 18
  completed_phases: 10
  total_plans: 69
  completed_plans: 62
  percent: 56
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-05-29)

**Core value:** Substrate agentico domain-neutral — un runtime Go che esegue un agentic loop multi-tool affidabile con identity, channels, skills e memory come overlay configurabili.
**Current focus:** Phase 16 — add-richer-recipes-doctor-checks-for-whatsapp-and-calendar-e

## Current Position

Phase: 16 (add-richer-recipes-doctor-checks-for-whatsapp-and-calendar-e) — EXECUTING
Plan: 3 of 8
Status: Ready to execute
Last activity: 2026-06-04

Progress: [█████████░] 90%

### Next — Phase 08.1

`/gsd-plan-phase 08.1` — Tool Search hardening to Anthropic `defer_loading` parity (BM25/semantic search over name+description+arg fields, MCP tool namespacing, ≥1-non-deferred guard). Reference study in auto-memory + ROADMAP Phase 08.1 detail. Recommended pre-step: stack-up smoke of `sandbox_exec` (`make sandbox-up` → `python -c "print(40+2)"` → `42`).

## Performance Metrics

**Velocity:**

- Total plans completed: 40
- Average duration: —
- Total execution time: 0.0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 00 | 6 | - | - |
| 03 | 5 | - | - |
| 04 | 5 | - | - |
| 06 | 5 | - | - |
| 07 | 4 | - | - |
| 07.1 | 5 | - | - |
| 08.1 | 4 | - | - |
| 9 | 6 | - | - |

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
| Phase 07 P01 | 35min | 3 tasks | 9 files |
| Phase 07 P02 | ~25min | 2 tasks | 7 files |
| Phase 07 P03 | 1 | 2 tasks | 12 files |
| Phase 08 P01 | 7min | 2 tasks | 3 files |
| Phase 09 P09-01 | ~12min | 2 tasks | 3 files |
| Phase 09 P05 | ~8min | 2 tasks | 10 files |
| Phase 9 P09-06 | ~18min | 2 tasks | 5 files |
| Phase 16 P01 | 20 min | 2 tasks | 5 files |
| Phase 16 P02 | 6 min | 2 tasks | 4 files |

## Accumulated Context

### Roadmap Evolution

- Phase 07.1 inserted after Phase 7: Forced-finalization loop fix: LlmAgent must always return a final answer (budget/dedup trip currently emits empty); surfaced by Phase 7 meteo E2E. See docs/research/agent-loop-forced-finalization.md (URGENT)
- Phase 08.1 inserted after Phase 8: Tool Search hardening to Anthropic defer_loading parity: BM25/semantic search (reuse PG FTS + embed sidecar) replacing substring match, search argument-name/description fields, MCP tool namespacing, >=1-non-deferred guard. Matters as tool count grows toward the 30-50 selection-accuracy threshold (P11 skills/7e snippets + future stdio MCP mounts via the retained mcptools seam; P8 landed as single-tool sandbox-agent, MCP bridge dormant). (URGENT)
- Phase 16 planned: MCP Sidecar Manager + Third-Party Trust. Scope includes trusted Aura recipes, Calendar fixture recipe, profiles/catalogs, status/doctor/logs, Streamable HTTP, explicit trust approvals, Dockerized/sandboxed third-party local runtime, and mount-time risk-policy enforcement. OpenClaw plugin host remains separate.

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
- [Phase 07]: 07-03: SearXNG client uses a plain http.Client (not the SSRF transport) — the in-network backend is trusted; only model-supplied web_fetch URLs cross the SSRF gate
- [Phase 07]: 07-03: images category OUT for Phase 7 (general/news only); an unknown category is a structured error, not a panic; thumbnail/img_src kept in searxResult for a future images slice
- [Phase 07]: 07-04 gap-closure (2026-06-02, Task 4 checkpoint bug+quality fix): the live Gate-3 found a BLOCKING bug — `AURA_WEB_RESPONSE_CAP_BYTES=24000` was applied to the RAW HTML body in gateAndRead, rejecting every real page (164KB Wikipedia → response_too_large) BEFORE extraction. Confirmed read in EXACTLY ONE place (raw-body LimitReader), never the LLM-facing payload (that is tools.NewResult preview cap). FIX Layer 1: renamed → WebFetchMaxBodyBytes / AURA_WEB_FETCH_MAX_BODY_BYTES, default 24000 → 5MB (raw-body DoS ceiling); PRD env-catalog amended first. FIX Layer 2: html.go post-processes the markdown — strips #cite_note/#cite_ref citation anchors, truncates at the References/Notes/Citations/Bibliography heading OR the first zero-padded reflist marker (the headingless Wikipedia case the live run exposed), strips the "From Wikipedia" boilerplate + <!--THE END--> converter artifact, filters fragment-only links, re-evaluates low_content post-cleanup. Gate-3 re-run LIVE: SC#2 content_md 36070→16429 B clean; SC#1 TestSearch_Live 1.01s; SC#3 4/4 blocked grep-clean; SC#4 TestDNSRebind PASS; ssrf.go mutation 94.4% (untouched); official internal/* coverage gate 87.4% (≥85%); golangci-lint 0; aura web doctor OK. Commits f5764ecc/1e424487/997b8693/00d76c59/8f25cc13. CAP-05 NOT marked complete — Task 4 human-verify still pending (the human's call).
- [Phase 08]: 08-08 (Wave-4 live-surface wiring): the execute tool is now session-bound ALWAYS — empty session_id defaults to the ctx conversation id (D-26, WithToolCallContext, no InvocationContext change), validated by the traversal guard, routed through RunPythonSession/RunShellSession with an optional tools.SessionAcquirer Acquire/Release seam (nil in unit tier, *sandbox.SessionManager in prod); the 2a inert-reject branch is DELETED and Spec() documents the D-02 asymmetric persistence contract (python state + /workspace persist; shell cd/export do not). A non-empty network_allow appends an advisory [advisory] risk_tier/gate_recommended line (scoring.ComputeSandboxTier) — advisory ONLY, no pending-state (D-12). Conversations.Delete cascades via the consumer-declared ConversationCleaner interface -> WorkspaceManager.PurgeConversationDir (os.Root no-follow); go list -deps ./internal/conversations has NO internal/sandbox (cycle-free, landmine #4). NEW reaper-free sandbox.SessionControl backs aura sandbox sessions {list|terminate|prune} (registry reads + docker stop/rm via the LookPath-gated fixed-argv dockerCLI, D-05); aura exec --session is live with ErrSessionCapReached -> exit 75. SessionDeps gained EgressNetwork+ProxyEnv: runArgv appends the egress bridge + HTTP(S)_PROXY env ONLY for a non-empty allowlist (empty keeps the 2a egressless argv). sandbox/seccomp-session.json derives from seccomp.json by ADDING connect ONLY; compose.yaml documents the bridge-gateway-reachable proxy + empty-allowlist-egressless posture EXTENDING AR-05-01 (live spike + 08-SECURITY re-audit are 08-09). bootChat wires WorkspaceManager(cleaner)+SessionManager(reaper)+RecoverOnBoot+session-bound Execute, Closes the manager (goleak-clean). 2 obsolete 2a contract tests rewritten (justified). vet/build/test green, -race clean, gofmt clean, all <=600 LOC. CAP-02 NOT marked complete (live integration + re-audit = 08-09). Commits 78100acd/96723ad3.
- [Phase ?]: [Phase 08]: 08-01 (PRD-amendment gate, doc-only): 5 amendments #38-#42 — D-01 two-tier persistence (per-session interpreter + workspace, not a contradiction), D-05 docker-lifecycle carve-out (CLI lifecycle, execution HTTP-only, NEVER mounts socket), D-08 host-side Go forward proxy egress + hostname-CONNECT allowlist + resolve-then-pin (SUPERSEDES iptables; CAP_NET_ADMIN incompatible with cap_drop:ALL; CAP-02 'via iptables' superseded), D-11 internal/scoring home-slice Slice 6 -> Phase 8 (+D-12 scope guard sandbox-advisory-only), D-13 os.Root/openat2 supersedes O_NOFOLLOW (cascade = manual no-follow openat walk not os.RemoveAll). Schema: 0010 -> 0008 (floor 0007), conversation_id text -> uuid. Wave-0 OQs in 08-DECISIONS-WAVE0.md: SSRF export-minimal over netguard-extract, AURA_PRIVACY_MODE add field + fail-fast under local-only+non-empty-allowlist, session connect(2)-allowed seccomp + proxy at bridge-gateway-IP + empty-allowlist-keeps-egressless + extends-AR-05-01 + live-reachability Wave-5 gate. Commits 21060757/1b18b915.
- [Phase 08]: 08-11 (gap-closure, Tasks 1-3 committed — Task 4 live Gate-3 human-verify PENDING): a live Gate-3 run on 08-10 exposed the LAST CAP-02 gap — the do-not-modify liveManager built the SessionManager with NO Workspace ensurer, so create() skipped EnsureDir, runArgv added no --mount, and the live container's /workspace was the read-only baked image dir under --read-only (1b/2/4a failed with read-only-filesystem on EVERY daemon). Task 1 (afa3a995): injected Workspace: NewWorkspaceManager(runDirOrSkip(t), 0) into liveManager — same AURA_RUN_DIR the cascade test purges, quota 0; no signature/assertion change, endpoint-resolver wiring untouched; Q&A revision documented (the 08-10 do-not-modify constraint was scoped to forcing the shared defaultSessionEndpoint resolver, orthogonal to the workspace injection which restores prod parity at chat.go:129; NOT a PRD amendment, D-04 unchanged). Task 2 (3cd62113): SessionManager.Close now terminateLiveSessions() after the reaper exits — stop+rm+MarkTerminated+Unregister every live session under capMu with a fresh short-deadline ctx, s.mu intentionally not taken, per-container WARN-log, count clamps 0, idempotent via the started guard — killing the orphan aura-sandbox-sess-* contamination that spuriously FAILed 1a/1c in a dirty suite (NO 3-strike escape needed: goleak/reaper/cap/boot-recovery all stayed green). Task 3 (no-op confirm): the CI sandbox.yml sandbox-2b-session-gate job already exports AURA_RUN_DIR=/tmp/aura-run + mkdir -p (line 257/330), builds aura-sandbox:ci baking the 08-10 /workspace (329), wires the egress bridge+host proxy+AURA_SANDBOX_PROXY_ENV before the tier (347-366) + session seccomp (262), runs the full sandbox_integration && db_integration race tier under CI=true (373) — it is the Gate-3 host of record, no edit needed. Unit tier go test -race ./internal/sandbox/ green (5.6s real run); tagged builds (db_integration, sandbox_integration && db_integration) compile. CAP-02 NOT closed + 08-SECURITY threats_open NOT zeroed — both close on the human native-daemon Gate-3 sign-off (Task 4). Docker Desktop CANNOT pass 1b/2/4a (daemon-side device= path unresolvable, 0x100e) — the native-Linux CI DinD job or a stood-up native dockerd is the verification host.
- [Phase 08]: 08-09 (Gate-3 evidence + live tier, Tasks 1-2 — Task 3 live human-verify PENDING): authored the live sandbox_integration && db_integration tier proving the 4 ROADMAP CAP-02 criteria (PythonStatePersists 1a / WorkspacePersists 1b / ConcurrentSerialized_Live 1c / SymlinkEscapeCascade_Live 2 / ReaperLiveContainerRemoved 3 / NetworkPyPIAllowed 4a+landmine-3 reachability spike / NetworkNonAllowlistRefused 4b / BootRecovery_LazyRecreate) + TestMigration0008_SchemaRoundTrip (uuid FK + CHECK + ON DELETE CASCADE). All compile-green under the tags (vet+build+test-compile exit 0); the two RESEARCH-named tests that collided with the untagged unit tier got _Live suffixes. 08-SECURITY.md consolidates the 40-threat T-08-* register with implementing-file citations, EXTENDS AR-05-01 for the 2b connect-allowing session egress posture (host-proxy-contained, bridge-gateway-reachable, empty-allowlist-egressless — documented before live confirmation), records the Claude-Code allowlist-bypass caveat as AR-08-01 (accepted-with-mitigation); threats_open held at 5 (NOT zeroed). sandbox.yml gained the sandbox-2b-session-gate DinD job (Postgres-through-0008 + sidecar + egress bridge/proxy at the bridge gateway; 2b tier race + live egress leg + go-mutesting >=70% on network.go/scoring.go/sessions.go + 85% coverage fold; CI=true no-skip-as-green). quality-snapshot gained the Phase-8 2b rows. CAP-02 NOT closed + threats_open NOT zeroed — both close on the operator's live Gate-3 sign-off (Task 3). Commits f99d9cb4/d4851c59/b1dc7ea7.
- [Phase ?]: [Phase 09]: 09-01 (PRD-amendment gate, doc-only #44): D-01..D-25 logged in DECISIONS.md §8. Amendment #44 supersedes the STALE prd.md Slice-3 acceptance (swarm_talk/swarm_join/bus/tier.go/Responder/children-map all CUT v1, replaced by ephemeral swarm_spawn runner + pause-as-report D-04 + flat-v1 D-10). Env catalog ADDS AURA_SWARM_MAX_GOALS=8 + AURA_SWARM_CHILD_TIMEOUT_SEC=120; OQ1 resolved D-21-supersedes-D-23 (NO AURA_MCP_*_SERVER vars, managed config is the path). ROADMAP SC#2->tool-not-available+depth-code-guard, SC#3->5 needs_user_input report entries, SC#5 added live cot_eval E2E. CAP-03 NOT marked complete (code waves pending). Commits 2c05fbdf/d285f17e.
- [Phase ?]: [Phase 09]: 09-05: swarm_spawn Deferred {goals}-only tool (D-01/D-03) wired to the 09-02 engine via a CYCLE-FREE seam — swarmRunner interface in the tools package (imports neither internal/swarm nor internal/agent) + agent.WithSwarmContext private-ctx-key injector (mirrors WithToolCallContext, set in runTool; config rides on the adapter not the ctx) + internal/swarm.RunnerAdapter. D-24 anti-over-spawn literal (test-asserted) + D-13 goals cap. Registered PARENT-ONLY in buildBaseRegistry (chat.go/runner.go unchanged); TestBuildBaseRegistryValidatesWithSwarmSpawn proves reg.Validate() holds with the Deferred tool (Pitfall 6). Workers excluded via Without (D-08/D-10). aura swarm-demo = no-LLM FakeClient engine proof (D-16). runTool gained budget param (Rule 3). Commits 827169a7/547bed0d.
- [Phase ?]: [Phase 09]: 09-06: live dual-gate swarm E2E (TestSwarmE2E, cot_eval, OPENROUTER+AURA_EVAL_SELF_*-gated, operator-run NOT CI). swarmScenarios() SEPARATE from scenarios(); NATURAL prompt (no swarm/parallel word, asserted) = compute-and-self-mail + compute-and-self-WhatsApp two independent subtasks + a no-over-spawn control. Hard floor: workers off the ChildReport goal_index count, facts present, mail+WhatsApp read-back via the SAME mounted MCP at the right JID (D-19 duality), wall-clock <1.5x single-worker baseline. Judge >=90% equal-weight mean over autonomous-parallelization/sub-answer/aggregation (D-22 fixes dims+gate). Registry = buildBaseRegistry + swarm_spawn (live RunnerAdapter) + mail/whatsapp MCP mounts (D-20 allowlists). Relocated reportPath/dimResult/scenarioMetrics into non-test scoring_cot_eval.go (Rule 3) so go build -tags cot_eval exits 0. Placeholder snapshot row at commit; operator run fills TBDs. CAP-03 NOT closed (Gate-3 verifier/operator). Commits 31599320/ab0b8a10.

### Pending Todos

[From .planning/todos/pending/ — ideas captured during sessions]

None yet.

### Blockers/Concerns

[Issues that affect future work]

- 8 Gate 1 DoR open questions tracked in research/SUMMARY.md "Gaps to Address" — resolve per-phase during plan-phase invocations.

### Quick Tasks Completed

| # | Description | Date | Commit | Status | Directory |
|---|-------------|------|--------|--------|-----------|
| 260604-c4l | hygiene sweep (W-1 + W-NEW-1 + coverage re-run) | 2026-06-04 | 649b0520 |  | [260604-c4l-hygiene-sweep-w-1-w-new-1-coverage-re-ru](./quick/260604-c4l-hygiene-sweep-w-1-w-new-1-coverage-re-ru/) |
| 260604-bq8 | D-15 doc-superseding sweep on REQUIREMENTS.md (CAP-01/02 wording + 5 stale checkboxes) | 2026-06-04 | 0d197ede |  | [260604-bq8-d-15-doc-superseding-sweep-on-requiremen](./quick/260604-bq8-d-15-doc-superseding-sweep-on-requiremen/) |
| 260604-l9u | add an MCP doctor health line for WhatsApp REST :8080 + connected-state | 2026-06-04 | 74790921 | Verified | [260604-l9u-add-an-mcp-doctor-health-line-for-whatsa](./quick/260604-l9u-add-an-mcp-doctor-health-line-for-whatsa/) |

## Deferred Items

Items acknowledged and carried forward from previous milestone close:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| LLM Fallback | LLM-V2-01 (vLLM + LMCache, Slice 13) | Deferred to v2 | 2026-05-29 (GPU-gated, DGX Spark bundle path) |
| Skills | SKILL-V2-01 (Slice 7f cross-conv cluster auto-suggest) | Deferred to v1.x | 2026-05-29 (Amendment #13 scope reduction) |
| Swarm | SWARM-V2-01 (full N-deep + DM-by-ID + tier-mapped) | Deferred to v2 | 2026-05-29 (Amendment #12 scope reduction) |

## Session Continuity

Last session: 2026-06-04T14:12:18.963Z
Stopped at: Completed 16-02-PLAN.md
Resume file: None
