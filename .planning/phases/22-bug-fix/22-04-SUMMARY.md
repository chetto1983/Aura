---
phase: 22-bug-fix
plan: 04
subsystem: agent
tags: [hooks, provenance, trust, fs-tools, shell, workflow, swarm, budget, prefix-cache, tool-search]

# Dependency graph
requires:
  - phase: 22-bug-fix (22-01)
    provides: panic firewall (panicobs) + dedup-ring hardening
  - phase: 22-bug-fix (22-02)
    provides: secret-env stripping + observability metrics substrate (recordHookOutcome, turn/llm/tool counters)
  - phase: 22-bug-fix (22-03)
    provides: MCP reconnect/breaker + reasoning budget bounds
provides:
  - Per-hook fail_open/fail_closed policy with recover-wrapped in-process hook calls
  - Absolute hook-path enforcement + exit-zero rewrite requirement + bounded/audited hook rewrites
  - Default-untrusted provenance for unknown + swarm-child tool output (explicit trusted allowlist)
  - AURA_FS_MAX_READ_BYTES (10 MiB) stat-then-reject cap on fs_read/fs_write/fs_edit + paging hint
  - Atomic fs_edit (temp+rename), unified grep/glob ** semantics, sidecar runDir invariant
  - BackgroundShells SessionEvictor + poll-time finished-shell reclamation
  - Every agent_job schedule gated to pending_approval; cwd validation; normalized approval digest
  - send_file RequireWorkspace fail-closed flag for non-CLI contexts
  - tool_search per-tool description-hash re-embed on MCP reconnect
  - Cycle-safe findInTree; runtime prefix_drift metric; atomic swarm synthesis reservation
affects: [phase-22 live sign-off, channel-runner wiring, future multi-tenant gating]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "FailPolicy (fail_open default for non-security command hooks; fail_closed explicit)"
    - "Default-untrusted provenance with explicit trusted allowlist (fail-safe inversion)"
    - "stat-then-reject fs size cap with offset/limit paging hint"
    - "atomic temp-file + rename for fs writes"
    - "bounded worker pool for executeBatch (limit workers, not N parked goroutines)"
    - "atomic Budget.TryReserve/Release for genuine synthesis-budget protection"

key-files:
  created:
    - internal/agent/llm_agent_prefix.go
    - internal/agent/hooks_policy_test.go
    - internal/agent/hooks_command_hardening_test.go
    - internal/agent/trust_default_test.go
    - internal/agent/workflow_edges_internal_test.go
    - internal/agent/tools/fs_cap_test.go
    - internal/agent/tools/tool_hardening_test.go
    - internal/agent/workflow/workflow_edges_test.go
  modified:
    - internal/agent/hooks.go
    - internal/agent/hooks_command.go
    - internal/agent/trust.go
    - internal/agent/llm_agent.go
    - internal/agent/llm_agent_dispatch.go
    - internal/agent/llm_agent_events.go
    - internal/agent/llm_agent_parallel.go
    - internal/agent/metrics.go
    - internal/agent/budget.go
    - internal/agent/swarm_context.go
    - internal/agent/workflow/workflow.go
    - internal/agent/workflow/sequential.go
    - internal/agent/tools/fs.go
    - internal/agent/tools/fs_read.go
    - internal/agent/tools/fs_write.go
    - internal/agent/tools/fs_edit.go
    - internal/agent/tools/fs_grep.go
    - internal/agent/tools/shell_bg.go
    - internal/agent/tools/shell_exec.go
    - internal/agent/tools/shell_approval.go
    - internal/agent/tools/task.go
    - internal/agent/tools/send_file.go
    - internal/agent/tools/search.go
    - internal/agent/tools/read_tool_output.go
    - internal/swarm/swarm.go

key-decisions:
  - "Hook default policy stays FailClosed for bare NewHookManager/Register to preserve the historical turn-fatal contract; FailOpen is the recommended default for non-security command hooks (NewHookManagerWithPolicy)."
  - "AG-052 inversion required two supporting fixes: dedup hashes the RAW preview (nonce would defeat the progress veto) and control-plane signals (errors, pause sentinel) are never enveloped; the tool-result EVENT projection surfaces the raw preview while only history carries the nonce envelope."
  - "agent_job gating done in task.go (in-package) rather than internal/scoring, so scoring tests are untouched (D-09 stay-in-package)."
  - "AG-038 implemented as a REAL atomic reservation (Budget.TryReserve/Release) rather than ledgered best-effort, since the budget code shape allowed it."
  - "AG-064 implemented as a worker pool rather than ledgered, since it was practical to test."

patterns-established:
  - "FailPolicy: fail_open contains hook faults (log+metric+allow), fail_closed aborts with a clear reason."
  - "Default-untrusted: unknown/MCP/content tools are untrusted unless on trustedToolNames."
  - "Atomic budget reservation: TryReserve before fan-out, defer Release after, for protected sub-budgets."

requirements-completed: [HARDEN-07, HARDEN-08, HARDEN-09, HARDEN-10, HARDEN-12]

# Metrics
duration: ~95min
completed: 2026-06-15
---

# Phase 22 Plan 04: Agent Perimeter Hardening (P2/P3 operational wave) Summary

**Hook fail-soft policy + default-untrusted provenance + fs/shell/tool memory-safety + workflow cycle/leak/prefix-drift bounding — the broad P2/P3 operational hardening wave closing the AG-### tool-surface and workflow findings, all TDD with named regression tests.**

## Performance

- **Duration:** ~95 min
- **Tasks:** 4 (all tdd=true)
- **Files modified:** 24 source + 8 new test files
- **Commits:** 4 atomic task commits, each naming its AG-### findings (D-11)

## Accomplishments

- **Hook reliability (AG-003/004/030/054/058):** per-hook FailPolicy (FailOpen contains faults, FailClosed aborts), recover-wrapped in-process hooks, absolute-path enforcement, exit-zero rewrite requirement, bounded+audited rewrites.
- **Provenance (AG-052):** inverted the trust default — unknown + swarm-child output is untrusted unless explicitly allowlisted; fixed the nonce-vs-dedup interaction and kept control-plane signals/event projection clean.
- **Tool hardening (AG-014..020, AG-045, AG-046, AG-050):** fs size cap + paging hint, atomic edit, evictable background shells, agent_job gating, cwd validation + normalized approval digest, send_file fail-closed flag, MCP description-hash re-embed, sidecar invariant, unified glob.
- **Workflow edges (AG-031, AG-037, AG-038, AG-043, AG-059..064):** cycle-safe findInTree, runtime prefix_drift metric, atomic swarm synthesis reservation, parallel break-at-every-index goleak stress, chain_aborted_at marker, documented swarm-context contract, bounded executeBatch worker pool.

## Task Commits

1. **Task 1: Hook reliability** — `d4280d2f` (fix) — AG-003 (rewrite-bounds slice), AG-004, AG-030, AG-054, AG-058
2. **Task 2: Default-untrusted provenance** — `f99c3ae3` (fix) — AG-052
3. **Task 3: Tool hardening** — `75987d50` (fix) — AG-014..020, AG-045, AG-046, AG-050
4. **Task 4: Workflow correctness edges** — `92d469b7` (fix) — AG-031, AG-037, AG-038, AG-043, AG-059..064

_All four tasks were TDD (RED test written and shown failing, then GREEN). Test+impl landed in one commit per task for atomic per-finding rollback._

## Finding Ledger (AG-### dispositions)

| Finding | Disposition | Notes |
|---------|-------------|-------|
| AG-003 | fixed (slice) | rewrite-bounds + audit landed; exec-by-fd TOCTOU + full-env stripping OUT of scope per 22-CONTEXT |
| AG-004, AG-058 | fixed | FailPolicy + recover; first-result-wins documented |
| AG-030, AG-054 | fixed | exit-zero rewrite gate; absolute-path requirement |
| AG-014, AG-045 | fixed | fs cap + paging hint; atomic temp+rename edit |
| AG-015, AG-017 | fixed | SessionEvictor + poll-prune; byte-exact paging test |
| AG-016 | fixed | every agent_job gated to pending_approval |
| AG-018 | fixed | cwd stat-validation + filepath.Clean approval digest |
| AG-019 | fixed (flag) | RequireWorkspace fail-closed; channel-runner wiring is the composition-root one-liner (see Deviations) |
| AG-020 | fixed | per-tool description-hash → full ranker rebuild on reconnect change |
| AG-046, AG-050 | fixed | unified ** glob; absolute-runDir assertion |
| AG-052 | fixed | default-untrusted inversion + swarm propagation (already stamped) |
| AG-031, AG-037 | fixed | runtime prefix_drift metric; cycle-safe findInTree |
| AG-038 | fixed | real atomic TryReserve/Release (not ledgered best-effort) |
| AG-043 | fixed (proven) | goleak stress, break at every index — no leak |
| AG-061 | fixed | chain_aborted_at StateDelta marker |
| AG-062 | fixed (doc + test) | concurrent-read contract documented; -race fan-out test guards it |
| AG-064 | fixed | bounded worker pool (not ledgered — practical to test) |
| AG-059 | accepted/documented | empty-pass leaf contract: bounded by the wallclock ctx (AG-041, prior wave); intentionally cooperative |
| AG-060 | accepted/documented | escalate cancel is checkpoint-based not preemptive; siblings may spend a few more steps — intentional cooperative cancellation (documented in parallel.go D-03) |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] dedup progress-veto defeated by the untrusted-envelope nonce**
- **Found during:** Task 2 (provenance inversion)
- **Issue:** After inverting the trust default, identical tool results were wrapped with a per-call random nonce, so the dedup progress veto (which hashes the result preview) never saw a stable result → loops ran to budget exhaustion instead of deduping.
- **Fix:** Dedup now hashes the RAW result preview (`run.Result.Preview`), not the nonce-wrapped prompt rendering (`run.Preview`).
- **Files modified:** internal/agent/llm_agent_dispatch.go
- **Verification:** TestLlmAgent_DedupWindow_Trips, TestFinalize_DedupTrip green.
- **Committed in:** f99c3ae3 (Task 2)

**2. [Rule 1 - Bug] control-plane signals were enveloped as untrusted content**
- **Found during:** Task 2
- **Issue:** The inversion wrapped agent-synthesized error strings and the ErrAwaitingUserInput pause sentinel in the untrusted envelope, breaking TestAskUserOnlyPauseConstraint and the tool-error contract.
- **Fix:** The runTool error path no longer re-wraps the agent's own error string; the tool-result EVENT projection surfaces the raw preview (UI/REPL/AG-UI/audit) while only the model-facing history carries the nonce envelope.
- **Files modified:** internal/agent/llm_agent.go, internal/agent/llm_agent_events.go
- **Verification:** TestAskUserOnlyPauseConstraint, TestLlmAgent_ToolExecuteError, TestLlmAgent_EmitsToolInvocationStartAndEndMetadata green.
- **Committed in:** f99c3ae3 (Task 2)

**3. [Rule 3 - Blocking] llm_agent.go reached 600 LOC after AG-031 wiring**
- **Found during:** Task 4
- **Issue:** Adding the prefix-drift snapshot/check inline pushed llm_agent.go to 609 LOC, violating the 600 LOC cap (file-size hook would reject the commit).
- **Fix:** Extracted prefixSnapshot + checkPrefixDrift into internal/agent/llm_agent_prefix.go; llm_agent.go is back to 599.
- **Files modified:** internal/agent/llm_agent.go, internal/agent/llm_agent_prefix.go
- **Verification:** file-size hook passed on the Task 4 commit; TestPrefixDrift green.
- **Committed in:** 92d469b7 (Task 4)

### Test-contract updates (AG-052 inversion + AG-038 reservation)

Several existing tests encoded the PRE-inversion unknown→trusted default or the
old best-effort budget reserve. They were updated WITH explicit AG-### justification
(CLAUDE.md: a test may be rewritten when its premise changed), not to mask a broken
implementation. The security-relevant invariants (veto skips execution, pause sentinel
unwrapped) now pass unmodified. Files: llm_agent_finalize_test.go, llm_agent_parallel_test.go,
llm_agent_hooks_test.go (AG-052); swarm_test.go TestSwarmBudgetInheritance (AG-038).

---

**Total deviations:** 3 auto-fixed (2 bug, 1 blocking) + scoped test-contract updates.
**Impact on plan:** All auto-fixes were correctness-required consequences of the AG-052 inversion and the 600-LOC cap. No scope creep beyond the plan's named findings.

## Known Stubs / Wiring Notes

- **AG-019 send_file RequireWorkspace** is implemented and tested at the tool layer
  (fail-closed when root empty + RequireWorkspace). The composition-root flip for
  non-CLI runners is a one-liner that belongs to the channel-runner wiring (the
  plan's files_modified does not include cmd/aura/main.go). The CLI default
  (RequireWorkspace=false) preserves existing behavior; no silent downgrade exists
  for CLI. This is the documented mechanism-then-wire split, not a stub of the fix.

## Issues Encountered

- The container-artifact test `TestProductionContainerArtifactsMatchFatImageContract`
  (cmd/aura) fails PRE-EXISTING and UNRELATED to this plan (compose.yaml `:nitro`
  vs the test's `:exacto`, from commit 136325dc). Logged to
  `.planning/phases/22-bug-fix/deferred-items.md`; out of scope (no plan-04 file
  touches compose.yaml or cmd/aura).

## Next Phase Readiness

- The tool surface and workflow perimeter are hardened enough for the Phase-22 full
  live sign-off (D-01/D-03), including the GLM-OCR tool-surface pass that empirically
  validates the AURA_FS_MAX_READ_BYTES cap.
- New observable metric: `aura_agent_prefix_drift_total` (scrape during the live pass
  to confirm no hook busts the prompt cache).
- Remaining 22-04-adjacent wiring: flip `SendFile.RequireWorkspace` true in the
  non-CLI/channel composition root.

## Self-Check: PASSED

All created files exist on disk (llm_agent_prefix.go, hooks_policy_test.go,
trust_default_test.go, fs_cap_test.go, tool_hardening_test.go,
workflow_edges_test.go, 22-04-SUMMARY.md) and all four task commits are in git
history (d4280d2f, f99c3ae3, 75987d50, 92d469b7). Plan-level verification
(go test + go test -race on internal/agent/..., internal/agent/tools,
internal/agent/workflow, internal/swarm) is green; all 7 agent+swarm packages pass.

---
*Phase: 22-bug-fix*
*Completed: 2026-06-15*
