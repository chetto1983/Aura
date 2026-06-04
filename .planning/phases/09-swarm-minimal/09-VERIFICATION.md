---
phase: 09-swarm-minimal
verified: 2026-06-04T13:15:00Z
status: human_needed
score: 9/10 must-haves verified
overrides_applied: 0
human_verification:
  - test: "Run the live cot_eval swarm E2E"
    expected: ">=2 workers spawned via tool_use on a natural prompt, expected facts present, self-mail + self-WhatsApp read-back via MCP, wall-clock < 1.5x single-worker, judge rubric >=90% average, control prompt no-over-spawn; docs/aura-quality-snapshot.md TBD placeholder row updated with real numbers"
    why_human: "SC#5 requires OPENROUTER_API_KEY + live mail/WhatsApp accounts + WSL whatsmeow bridge (REST :8080); this is the deliberately operator-run tier per 09-VALIDATION.md Manual-Only table and 09-06 plan's non-blocking gate"
---

# Phase 9: Swarm (Minimal) Verification Report

**Phase Goal:** Minimal swarm coordinator — reuses ParallelAgent-style fan-out with MAX_SPAWN_DEPTH=2 cap for v1. NO DM-by-ID, NO tier-mapped models. Child budget inheritance from parent's remaining (NOT fresh). Pause-as-report multi-pause propagation with proxied_from_child_id mapping. PLUS first real production Mount of internal/agent/mcptools (mail + WhatsApp MCP) with natural-prompt live E2E dual gate (ground-truth + judge >=90%).
**Verified:** 2026-06-04T13:15:00Z
**Status:** human_needed (SC#5 live E2E is operator-run by design)
**Re-verification:** No — initial verification

---

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | PRD §Slice 3 amended: cut machinery removed, flat-v1, pause-as-report; 2 new AURA_SWARM_* env vars; no AURA_MCP_*_SERVER vars | VERIFIED | `grep -c "AURA_SWARM_MAX_GOALS" prd.md` = 2; `grep -c "AURA_SWARM_CHILD_TIMEOUT_SEC" prd.md` = 2; absence of AURA_MCP_MAIL_SERVER/AURA_MCP_WHATSAPP_SERVER literals confirmed |
| 2 | D-01..D-25 logged in DECISIONS.md; ROADMAP SC#2/SC#3 re-specced, SC#5 added | VERIFIED | `grep -c "D-25" .planning/DECISIONS.md` = 2; `grep -c "needs_user_input" .planning/ROADMAP.md` = 1; `grep -c "cot_eval" .planning/ROADMAP.md` = 2 |
| 3 | SC#1 — 3-child swarm wall-clock < 1.5x single-worker; race+goleak clean | VERIFIED | TestSwarmParallelTiming PASS; `go test -race ./internal/swarm/` exits 0; goleak TestMain active in main_test.go |
| 4 | SC#2 (re-specced) — worker registry excludes swarm_spawn; depth code-guard emits PRD error | VERIFIED | `Without(rc.ParentRegistry, swarmSpawnTool)` called per-child in swarm.go:134; TestWithout PASS; TestSwarmDepthGuard PASS — `checkDepth` at depth>=cap returns "MAX_SPAWN_DEPTH=<cap> exceeded" |
| 5 | SC#3 (re-specced) — 5-children-all-pause yields 5 needs_user_input report entries; goleak clean | VERIFIED | TestSwarmMultiPause: 5 workers each emit needs_user_input; all 5 report entries confirmed in log; `go test -race ./internal/swarm/` clean |
| 6 | SC#4 — depth-2 tree total steps <= parent remaining (shared *atomic.Int32) | VERIFIED | TestSwarmBudgetInheritance PASS; parent budget 20, all child budgets share the *atomic.Int32 via Budget.Child(); tree step sum verified <= parent remaining |
| 7 | MCP plumbing: Deferred:true flip, per-server allowlist, fail-soft boot, mail+whatsapp recipes | VERIFIED | `grep "Deferred:.*true" bridge.go` = 1, `grep "Deferred:.*false" bridge.go` = 0; TestMountAllowlistDeferred PASS (allowlist drops footguns; nil allowlist mounts all; every bridged Spec Deferred:true; WR-04 unmatched-allowlist warn); TestBuildRegistryFailSoft PASS; `grep -c "recipe:whatsapp" mcp.go` = 1, `grep -c "recipe:mail" mcp.go` = 1 |
| 8 | proxied_* 3-layer plumb (ask_user args → AwaitingInput Event → InsertParams/Insert → persistPause) complete; migration 0008 retypes column to text | VERIFIED | `grep -c "ProxiedFromChildID" event.go` = 1, ask_user.go = 4, runner_persist.go = 3; migration 0008 confirmed (ALTER TABLE ... TYPE text); sqlc model shows `pgtype.Text`; TestPersistPause_ForwardsProxiedIDs uses real "w2" flat id and PASS; TestInsertProxied (db_integration) PASS live against migrated Postgres |
| 9 | swarm_spawn Deferred tool: {goals}-only schema, D-24 anti-over-spawn literal, D-13 cap, registered parent-only; cycle-free seam; reg.Validate() holds | VERIFIED | TestSwarmSpawnDescriptionLiteral PASS (4 D-24 phrases confirmed); TestSwarmSpawnGoalsCap PASS; TestSwarmSpawnSchema PASS (no tier param); TestBuildBaseRegistryValidatesWithSwarmSpawn PASS; swarm_spawn.go imports neither internal/swarm nor internal/agent (confirmed); `go build ./...` exits 0 (no import cycle) |
| 10 | SC#5 live cot_eval E2E (dual gate: ground-truth + judge >=90%) — operator-run; compile-time deliverables present | UNCERTAIN (human needed) | Compile-time: `go build -tags cot_eval ./internal/eval/ && go vet -tags cot_eval ./internal/eval/` exits 0; harness_swarm_e2e_test.go has `//go:build cot_eval` tag + OPENROUTER_API_KEY gate; docs/aura-quality-snapshot.md has Phase 9 swarm row (TBD placeholder). Live run numbers: operator-run pending |

**Score:** 9/10 truths verified (truth 10 is UNCERTAIN by design — SC#5 is the one legitimate operator-run gate, not a gap)

---

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `prd.md` | §Slice 3 v1 shape + 2 AURA_SWARM_* env vars | VERIFIED | AURA_SWARM_MAX_GOALS + AURA_SWARM_CHILD_TIMEOUT_SEC present x2 each; AURA_MCP_*_SERVER absent |
| `.planning/DECISIONS.md` | D-01..D-25 + OQ resolutions | VERIFIED | D-25 present x2; OQ1/OQ2/OQ3 resolutions in §8 |
| `.planning/ROADMAP.md` | SC#2/SC#3 re-specced + SC#5 added | VERIFIED | "needs_user_input" in SC#3; "cot_eval" in SC#5 |
| `internal/swarm/swarm.go` | ephemeral runner: fan-out + waves + per-child isolation + budget | VERIFIED | 200 LOC; errgroup; Without() called per-child; D-02 divergence (child err → nil goroutine return); Budget.Child() forked |
| `internal/swarm/report.go` | ChildReport struct + dumpTranscript flat-id | VERIFIED | 85 LOC; ChildReport with goal_index/child_id/status/summary/error/question/options; dumpTranscript present |
| `internal/swarm/brief.go` | D-07 structured brief builder + D-06 worker-overlay constant | VERIFIED | 49 LOC; structuredBrief func present |
| `internal/swarm/registry.go` | Without helper | VERIFIED | 23 LOC; func Without present x2 |
| `internal/swarm/swarm_depth.go` | AURA_SWARM_MAX_DEPTH code guard | VERIFIED | 38 LOC; checkDepth func; "MAX_SPAWN_DEPTH=%d exceeded" literal |
| `internal/swarm/swarm_test.go` | SC#1/#3/#4 + D-02/D-09/D-11/D-18/D-25 tests + goleak TestMain | VERIFIED | 471 LOC; goleak in main_test.go; all named tests PASS under -race |
| `internal/swarm/swarm_property_test.go` | rapid D-25 properties (len+order, tree budget, goleak, per-child isolation) | VERIFIED | 88 LOC; TestSwarmProperties PASS |
| `internal/config/config.go` | MaxSwarmGoals=8 + SwarmChildTimeoutSec=120 | VERIFIED | grep shows AURA_SWARM_MAX_GOALS x2, SwarmChildTimeoutSec x2 |
| `internal/agent/mcptools/bridge.go` | Deferred:true flip + allowlist filter | VERIFIED | Deferred: true x1, Deferred: false x0; allow param threaded through Bridge/Mount/MountServer |
| `internal/agent/mcptools/mount.go` | allow []string param | VERIFIED | grep shows "allow" in mount.go |
| `cmd/aura/main.go` | fail-soft boot (WARN-and-drop per server) + allowlist resolver | VERIFIED | TestBuildRegistryFailSoft PASS; slog.Warn + continue in loop confirmed |
| `cmd/aura/mcp.go` | mail + whatsapp recipes | VERIFIED | recipe:whatsapp x1, recipe:mail x1; AURA_MCP_*_SERVER absent |
| `internal/agent/tools/ask_user.go` | proxied_from_child_id + proxied_tool_call_id optional args | VERIFIED | proxied_from_child_id x4 in ask_user.go; required stays ["question","kind"] |
| `internal/agent/event.go` | AwaitingInput gains ProxiedFromChildID + ProxiedToolCallID | VERIFIED | ProxiedFromChildID x1 in event.go |
| `internal/askuser/store.go` | InsertParams + Insert set proxied_* (text-typed post-CR-01) | VERIFIED | ProxiedFromChildID x2+ in store.go; pgtype.Text confirmed in sqlc models |
| `internal/runner/runner_persist.go` | persistPause reads + forwards proxied_* | VERIFIED | ProxiedFromChildID x3 in runner_persist.go |
| `internal/db/migrations/0008_proxied_child_id_text.up.sql` | ALTER TABLE aura.paused_states ALTER COLUMN proxied_from_child_id TYPE text | VERIFIED | file exists; ALTER TABLE content confirmed |
| `internal/agent/tools/swarm_spawn.go` | Deferred:true {goals} tool; D-24 literal; no internal/swarm or internal/agent import | VERIFIED | Deferred: true x2; swarmRunner interface present x4; imports block has only context/encoding/json/fmt |
| `internal/agent/swarm_context.go` | WithSwarmContext ctx injector (private ctxKey) | VERIFIED | WithSwarmContext x2; private swarmCtxKey type |
| `internal/swarm/runner_adapter.go` | concrete swarmRunner reading swarm context + calling engine | VERIFIED | 59 LOC; func present; Without(registry, "swarm_spawn") call confirmed |
| `internal/eval/harness_swarm_e2e_test.go` | TestSwarmE2E behind cot_eval tag + OPENROUTER_API_KEY gate | VERIFIED | `//go:build cot_eval` present; OPENROUTER_API_KEY gate at line 99; compiles clean |
| `internal/eval/dataset_cot_eval.go` | swarm scenario (natural prompt, no "swarm") + control scenario | VERIFIED | grep confirms "swarm" present in dataset file; `go build -tags cot_eval` exits 0 |
| `docs/aura-quality-snapshot.md` | Phase 9 swarm row with TBD placeholder | VERIFIED | "swarm" x4 in snapshot; row has worker-count/timing-ratio/judge-average/read-back columns with TBD values |

---

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `internal/swarm/swarm.go` | `internal/agent/budget.go Budget.Child` | shared *atomic.Int32 per-child fork after snapshot-once Remaining() | VERIFIED | Budget.Child() call in swarm.go; TestSwarmBudgetInheritance PASS |
| `internal/swarm/swarm.go` | `internal/agent/event.go Actions.AwaitingInput` | pause-as-report detection (D-04) | VERIFIED | AwaitingInput detection in swarm.go event drain loop; TestSwarmMultiPause PASS |
| `internal/swarm/report.go dumpTranscript` | `$AURA_RUN_DIR/<convID>/swarm/<childID>.jsonl` | direct one-JSON-per-line write | VERIFIED | dumpTranscript path: `filepath.Join(runDir, convID, "swarm", childID+".jsonl")`; TestSwarmTranscript PASS |
| `cmd/aura/main.go buildRegistryWithMCP` | `internal/agent/mcptools/MountServer` | per-server allowlist arg + WARN-and-drop on error | VERIFIED | slog.Warn + continue in loop; TestBuildRegistryFailSoft PASS |
| `internal/agent/mcptools/bridge.go Bridge` | allowlist filter | drop defs not in allow before adapting | VERIFIED | TestMountAllowlistDeferred confirms allowlist drops footguns; Deferred:true on every bridged spec |
| `internal/agent/tools/ask_user.go askUserArgs` | `internal/agent/event.go AwaitingInput` | ErrAwaitingUserInput → pauseEvent projection | VERIFIED | ProxiedFromChildID flows through ask_user args → ErrAwaitingUserInput → AwaitingInput; TestPersistPause_ForwardsProxiedIDs PASS |
| `internal/runner/runner_persist.go persistPause` | `aura.paused_states.proxied_from_child_id` | askuser.Store.Insert → sqlc.InsertPausedStateParams | VERIFIED | ProxiedFromChildID x3 in runner_persist.go; TestInsertProxied (db_integration) PASS live |
| `internal/agent/llm_agent.go runTool` | `internal/agent/swarm_context.go WithSwarmContext` | agent injects parent budget/registry/client into tool-call ctx | VERIFIED | WithSwarmContext call present in runTool; swarm_context.go has private ctxKey type |
| `internal/agent/tools/swarm_spawn.go Execute` | injected swarmRunner.Run | interface seam (no agent/swarm import) | VERIFIED | swarmRunner interface in tools package; Execute calls Runner.Run; no cycle (go build exits 0) |
| `cmd/aura/main.go buildBaseRegistry` | `tools.SwarmSpawn (Deferred:true) + reg.Validate()` | register-then-Validate; named test | VERIFIED | TestBuildBaseRegistryValidatesWithSwarmSpawn PASS; Validate returns nil with Deferred swarm_spawn registered |

---

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|-------------------|--------|
| `internal/swarm/swarm.go runChild` | reports[idx] ChildReport | agenttest.FakeClient (unit), real LlmAgent (live) | Yes — worker Run() streams Events; AwaitingInput or final LLMResponse.Content captured | FLOWING |
| `internal/swarm/runner_adapter.go Run` | parent budget/registry/client/llmCfg/convID | agent.SwarmContext(ctx) | Yes — WithSwarmContext injected by runTool before dispatch | FLOWING |
| `internal/runner/runner_persist.go persistPause` | ProxiedFromChildID/ProxiedToolCallID | AwaitingInput event fields | Yes — flows from ask_user args through ErrAwaitingUserInput through pauseEvent; db_integration confirms text round-trip | FLOWING |

---

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| `go build ./...` exits 0 (no import cycle) | `go build ./...` | (no output) | PASS |
| `go test ./...` all pass | `go test ./...` | All packages ok; no Phase 9 failures | PASS |
| `go test -race ./internal/swarm/ ./internal/agent/` | race+goleak | ok (both packages, cached) | PASS |
| SC#1: TestSwarmParallelTiming | `go test -run TestSwarmParallelTiming ./internal/swarm/` | PASS | PASS |
| SC#3: TestSwarmMultiPause (5 needs_user_input) | `go test -run TestSwarmMultiPause ./internal/swarm/` | PASS — 5 workers, all needs_user_input | PASS |
| SC#4: TestSwarmBudgetInheritance | `go test -run TestSwarmBudgetInheritance ./internal/swarm/` | PASS | PASS |
| SC#2: TestSwarmDepthGuard (PRD error literal) | `go test -run TestSwarmDepthGuard ./internal/swarm/` | PASS — "MAX_SPAWN_DEPTH=<cap> exceeded" confirmed | PASS |
| D-25: TestSwarmProperties (rapid, goals 1..8) | `go test -run TestSwarmProperties ./internal/swarm/` | PASS (large output, all rapid iterations ok) | PASS |
| D-18: TestSwarmTranscript (flat SessionID, separate write) | `go test -run TestSwarmTranscript ./internal/swarm/` | PASS | PASS |
| MCP allowlist + Deferred flip | `go test ./internal/agent/mcptools/ -run TestMountAllowlistDeferred` | PASS — all 4 subtests | PASS |
| fail-soft boot | `go test ./cmd/aura/ -run TestBuildRegistryFailSoft` | PASS — WARN logged, err==nil | PASS |
| Validate with Deferred SwarmSpawn | `go test ./cmd/aura/ -run TestBuildBaseRegistryValidatesWithSwarmSpawn` | PASS | PASS |
| proxied_* plumb (unit) | `go test ./internal/runner/ -run TestPersistPause_ForwardsProxiedIDs` | PASS — "w2" flat id, not UUID | PASS |
| TestInsertProxied (db_integration, live Postgres) | `go test -tags db_integration ./internal/askuser/ -run TestInsertProxied` | PASS live against migrated DB (migration 0008 applied) | PASS |
| cot_eval build+vet | `go vet -tags cot_eval ./internal/eval/ && go build -tags cot_eval ./internal/eval/` | OK | PASS |

---

### Probe Execution

No phase-declared probes. `scripts/*/tests/probe-*.sh` pattern not applicable to Phase 9 (unit/property/integration tiers cover all automated claims).

---

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|---------|
| CAP-03 | 09-01 through 09-06 | Swarm coordinator minimale: ParallelAgent fan-out + MAX_SPAWN_DEPTH=2 cap + child budget inheritance + proxied_from_child_id mapping | SATISFIED | All 6 plans executed; engine + tool + MCP plumbing + proxied plumb + eval harness all present and tested; migration 0008 applied; `go test ./...` green |

---

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `docs/aura-quality-snapshot.md` | 30 | TBD placeholder values in Phase 9 swarm row | INFO | Not a code debt marker — explicitly planned placeholder for the operator live run (SC#5); per 09-06 plan design, these TBDs are replaced by the operator after the live E2E run. No unreferenced debt. |

No TBD/FIXME/XXX found in any Go source file under the phase's modified surface. All files under 600 LOC. No stub/empty-return patterns in runtime code paths.

---

### Human Verification Required

#### 1. SC#5 Live cot_eval Swarm E2E (Dual Gate)

**Test:** Bring up the whatsmeow bridge (fork chetto1983/whatsapp-mcp @6de1dcd), health-check REST :8080 (405 on GET /api/send). Source `.env` (OPENROUTER_API_KEY + mail/WhatsApp creds via managed config). Run:
```bash
set -a; . ./.env; set +a
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
export AURA_EVAL_SELF_MAIL=<your-email> AURA_EVAL_SELF_PHONE=<your-number> AURA_EVAL_WA_CHAT_SELF=<phone>@s.whatsapp.net
go test -tags cot_eval -run TestSwarmE2E -timeout 600s -v ./internal/eval/
```

**Expected:** >=2 workers spawned via tool_use blocks on the natural prompt (no "swarm" word in prompt); expected facts present in aggregated answer; self-mail AND self-WhatsApp messages found on read-back via MCP at the correct JID; wall-clock < 1.5x single-worker; judge rubric >=90% average across the 4 D-22 dimensions; control prompt does NOT trigger swarm_spawn. Then: replace the TBD placeholder row in `docs/aura-quality-snapshot.md` with the real observed numbers (worker-count, timing-ratio, judge-average, mail-read-back, WA-read-back).

**Why human:** Requires OPENROUTER_API_KEY (paid API), live mail SMTP/IMAP credentials (managed config), live WhatsApp account + WSL whatsmeow bridge (REST :8080, subprocess), and WhatsApp JID self-chat duality (bridge-sent vs phone-sent). Cannot be verified programmatically without real accounts. Explicitly gated as operator-run by 09-VALIDATION.md Manual-Only table and 09-06 plan's `gate="non-blocking"` Task 2.

---

### Gaps Summary

No gaps. All 9 automated must-haves are VERIFIED. The 10th (SC#5 live E2E) is `UNCERTAIN` by design — it is the one explicitly operator-run tier documented in 09-VALIDATION.md Manual-Only table. The phase plans and ROADMAP explicitly gate it as post-merge operator work with a compile-time deliverable (harness compiles, placeholder row present) verifiable now. This is `human_needed`, not `gaps_found`.

**Review cycle closure:** The 09-REVIEW.md found 2 CRITICALs + 6 WARNINGs. 09-REVIEW-FIX.md confirms all 8 fixed (commits 2e7a702d..a3612973), including migration 0008 retyping `proxied_from_child_id uuid→text` (CR-01), detectPause context threading (CR-02), and 6 WARNINGs. Verified live: TestInsertProxied uses real "w2" flat id and passes against migrated DB.

---

_Verified: 2026-06-04T13:15:00Z_
_Verifier: Claude (gsd-verifier)_
