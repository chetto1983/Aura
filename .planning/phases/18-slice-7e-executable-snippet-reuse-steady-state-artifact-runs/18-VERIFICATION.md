---
phase: 18-slice-7e-executable-snippet-reuse-steady-state-artifact-runs
verified: 2026-06-06T00:00:00Z
status: passed
score: 9/9 must-haves verified (3 human items closed 2026-06-06: coverage 86.1% stamped + CAP-08.1 traceability Complete in 453dcc0c; writer_activate 45.2% mutation advisory-accepted by operator per Phase-10 precedent — all meaningful/new-handler mutants killed in 60eb932e, 17 survivors documented FS-fault near-equivalents)
overrides_applied: 0
human_verification:
  - test: "Run `bash scripts/coverage_gate.sh` in WSL with full stack up (PG + Neo4j, `db_integration neo4j_integration sandbox_integration` tags) and confirm the owned-surface combined coverage is ≥85%. Update the Phase-18 sub-table rows 368-369 in docs/aura-quality-snapshot.md with the live numbers."
    expected: "Combined coverage ≥85% (prior Phase-11 run was 86.6%; Phase-18 adds new handlers in internal/skills and internal/agent/tools — expect ≈86-87%)."
    why_human: "The Phase-18 detail sub-table in docs/aura-quality-snapshot.md explicitly marks Owned-surface coverage as `pending-operator-run`. The verification context claims 86.1% but this number does not appear in the snapshot file — it is unconfirmed at file level. The main quality-matrix row timestamp says 'coverage/mutation pending WSL make quality-full'. Coverage requires the live PG+Neo4j stack."
  - test: "Update the CAP-08.1 traceability row in .planning/REQUIREMENTS.md line 125 from 'In Progress' to 'Complete' to match the checked `[x]` requirement at line 40."
    expected: "The traceability table status for CAP-08.1 reads 'Complete' — consistent with the requirement checkbox."
    why_human: "The requirement is marked `[x]` (done) but the traceability table row still says 'In Progress'. This is a documentation inconsistency that should be closed manually to keep the file internally consistent."
  - test: "Accept or explicitly document the writer_activate.go mutation score of 45.2% (14/31) against the stated gate of ≥70% killed. The 17 surviving mutants are operator-documented FS-fault-injection near-equivalents in the quality snapshot. If accepted, add an override entry or close the gate advisory (as Phase 10 did for claim.go/heartbeat.go)."
    expected: "Either: (a) an explicit override entry stating the 17 FS-fault near-equivalents are accepted equivalent mutants, or (b) a follow-up mutation run with filesystem fault injection that closes the gap."
    why_human: "The snapshot narrative documents the reasoning (mid-op promote/materialize/dematerialize FS errors = near-equivalent, need cross-platform flaky FS fault injection to kill). The gate target is ≥70% and the score is 45.2%. This needs an explicit human decision to accept or re-test, consistent with the Phase-10 precedent for claim.go/heartbeat.go."
---

# Phase 18: Slice 7e Executable Snippet Reuse Steady-State Verification Report

**Phase Goal:** Collapse the chat-surface xlsx artifact loop from 29-30 LLM roundtrips (151-233s) to a snippet-reuse steady state of ~5 calls under 40s. Flip snippet execution posture to HOST-PRIMARY (D-01), add UNGATED in-loop snippet-save (D-02), fill restore/archive ActionRouter stubs, close eval-to-production registry parity, make the <40s/~5-call acceptance machine-checkable from the committed aura.tool_invocations ledger grounded by a live characterization probe (D-03). PRD amendment + CAP-08.1 land first (D-04).
**Verified:** 2026-06-06
**Status:** human_needed — automated checks passed; three documentation-consistency items need human closure before this phase can be declared fully complete
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | PRD §Slice 7e-core follow-up at ~2214 marked RESOLVED with literal token "RESOLVED" and amendment #55 (not #54 — collision with D-43) flips snippet posture to HOST-PRIMARY | VERIFIED | prd.md line 2214 contains "**[RESOLVED #55/D-01 (Phase 18)..."]; amendment #55 blockquote at 2228; `[SUPERATO #55]` markers at 2177, 2615, 2631-2648 |
| 2 | REQUIREMENTS.md carries a new CAP-08.1 row `[x]` checked (D-04); CAP-08 reconciled as Complete | VERIFIED | REQUIREMENTS.md line 40: `[x] **CAP-08.1**:`; line 124: CAP-08 "Complete (7e-core sandbox-posture shipped 11-07; host-posture follow-up = CAP-08.1 / Phase 18)" |
| 3 | aura.tool_invocations ledger (migration 0011 + store + sqlc + runner wiring + cmd/aura/shell.go) committed; go build/test/race green for internal/toolinvocations and internal/runner | VERIFIED | Glob confirms all files present; `go build ./...` exits 0; `go test -race ./internal/toolinvocations/` passes 1.052s; migration 0011 contains table + append-only triggers + aura_app SELECT/INSERT grant |
| 4 | docs/phase-18-xlsx-call-breakdown.md contains live per-call breakdown of one live xlsx run grounding the provisional target (D-03), with the gate-metric correction (request_id is per-user-turn, not a roundtrip proxy; gate must count event_kind='end' rows) | VERIFIED | File exists with 21-call D-03 run table, headline numbers, explicit metric finding, and replacement gate metric: `count(*) WHERE event_kind='end'` ≤6 AND wall-clock <40s |
| 5 | The model receives a HOST shell_exec command line (not sandbox_exec primary frame) when it calls action=use on an active snippet (D-01 host-primary) | VERIFIED | `renderSnippetUse` in skill_read.go (line 124+) emits `Run this stored snippet by path with shell_exec: command=...` as PRIMARY; sandbox_exec named as escalation only; `go test ./internal/agent/tools/` passes with `TestRenderSnippetUseHostFrame` + `TestActionUseSnippetEmitsHostFrame` |
| 6 | action=save_snippet (D-02) routes the model's call to Writer.SaveSnippet UNGATED — returns a NORMAL ToolResult, NEVER *ErrAwaitingUserInput | VERIFIED | `actionSaveSnippet` in skill_write.go (line 193+) has no pause tail; router maps `save_snippet: t.actionSaveSnippet` (skill.go line 176); `go test ./internal/agent/tools/ -run TestAskUserOnlyPauseConstraint` passes; `actionRestore`/`actionArchive` also return normal results |
| 7 | restore/archive replace notYetWired stubs in the ActionRouter; Writer.Restore is the inverse of Archive | VERIFIED | writer_activate.go line 107: `func (w *Writer) Restore(...)`; skill.go lines 177-178: `"restore": t.actionRestore, "archive": t.actionArchive`; SanitizeName chokepoint confirmed at writer_activate.go line 68 (CR-01 fixed) and line 110 |
| 8 | The steady-state gate drives the scenario through runner.New(deps) with deps.ToolInvocations=toolinvocations.New(pool), asserts the reuse run's ledger window (endEvents≤6, wall<40s) + .xlsx artifact, and the structural parity test (TestRegistrySnippetReuse_HasSkillTool) passes key-free | VERIFIED | skills_snippet_reuse_cot_eval_test.go line 247: `ToolInvocations: toolinvocations.New(pool)`; gate reads ledger via `ListByConversation` (line 418); live paid gate PASSED: endEvents=5, wallClock=11.057s, today=true; structural gate `TestRegistrySnippetReuse_HasSkillTool` PASS confirmed live |
| 9 | Owned-surface combined coverage ≥85%; mutation ≥70% on new handlers | UNCERTAIN | Mutation: skill_write.go=95.5% PASS; writer_activate.go=45.2% (14/31) — 17 FS-fault near-equivalents documented-equivalent, below ≥70% gate. Coverage: main row says "pending WSL make quality-full" with no confirmed number in snapshot sub-table; verification context claims 86.1% but not reflected in the snapshot file. Needs human closure. |

**Score:** 8/9 truths verified (truth 9 uncertain — pending human closure)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `prd.md` | Host-primary snippet posture amendment + ~2190 follow-up RESOLVED + CAP-08.1 wording | VERIFIED | RESOLVED token at line 2214; amendment #55 at 2228; [SUPERATO #55] markers present |
| `.planning/REQUIREMENTS.md` | CAP-08.1 requirement row + CAP-08 reconciliation | VERIFIED | `[x] **CAP-08.1**:` at line 40; CAP-08 Complete at line 124 |
| `internal/toolinvocations/store.go` | Append-only tool ledger store with ListByConversation | VERIFIED | File present, substantive (255 LOC), exports Insert + ListByConversation; WR-01/WR-02/WR-03 code review fixes applied (best-effort ledger, RedactForLedger, clampInt32) |
| `internal/db/migrations/0011_tool_invocations.up.sql` | Append-only aura.tool_invocations table with BEFORE UPDATE/DELETE/TRUNCATE reject triggers + aura_app SELECT/INSERT grant | VERIFIED | File present; table + UNIQUE index + 3 indexes + reject function + 2 triggers + GRANT confirmed |
| `docs/phase-18-xlsx-call-breakdown.md` | Empirical per-call breakdown of one live xlsx run (D-03 grounding) | VERIFIED | File present with 21-call per-tool table, metric finding, and replacement gate metric |
| `internal/skills/snippet.go` | SnippetHostPath host-path resolver + UseSnippet returning host path | VERIFIED | SnippetHostPath at line 104; SnippetHostInvocation at line 134; SnippetUse.HostPath populated in UseSnippet at line 327 |
| `internal/agent/tools/skill_read.go` | renderSnippetUse emitting a host shell_exec by-path frame | VERIFIED | renderSnippetUse at line 124 emits shell_exec primary frame; shellQuotePath helper at line 137 for WR-04 |
| `cmd/aura/serve_adapters.go` | skillLoaderAdapter.Snippet returning the host path (export dir from cfg) | VERIFIED | exportDir field wired from cfg.SkillExportDir in newSkillTool; Snippet resolves via SnippetHostInvocation |
| `internal/skills/writer_activate.go` | Writer.Restore — the inverse of Archive | VERIFIED | func (w *Writer) Restore at line 107; SanitizeName chokepoint at line 110; AuditActivate reuse documented |
| `internal/agent/tools/skill_write.go` | actionRestore, actionArchive, actionSaveSnippet handlers | VERIFIED | All three handlers present (lines 185+, 223+, 240+); save is UNGATED (no pause tail) |
| `internal/agent/tools/skill.go` | router wiring restore/archive/save_snippet + widened action enum | VERIFIED | Lines 176-178: save_snippet/restore/archive mapped; property-level enum updated; root stays required=["action"] only |
| `internal/eval/skills_snippet_reuse_cot_eval_test.go` | The steady-state 2-run + ledger-window snippet-reuse gate (TestSnippetReuseE2E) | VERIFIED | File present; ListByConversation at line 418; runner.New with ToolInvocations at line 247; endEvents/wallClock assertions at lines 135-141 |
| `internal/eval/scenarios_skills.go` | The snippet-reuse scenario | VERIFIED | File exists (per imports in reuse test) |
| `docs/aura-quality-snapshot.md` | Phase-18 steady-state row with live numbers | PARTIALLY VERIFIED | Main matrix row contains live numbers (endEvents=5, wallClock=11.057s, mutation numbers); sub-table coverage/mutation rows remain `pending-operator-run` — inconsistency needing human update |
| `internal/skilladapters/skilladapters.go` | Shared adapter package extracted from eval-test-local duplication (IN-04) | VERIFIED | Package present at internal/skilladapters/skilladapters.go — IN-04 resolved, not deferred |
| `internal/toolinvocations/redact.go` | RedactForLedger credential pattern redactor (WR-02 fix) | VERIFIED | File present; ArgsRawCapBytes=8KiB, ResultPreviewCapBytes=2KiB; credential patterns redacted before durable column |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `cmd/aura/chat.go` | `internal/toolinvocations` | toolinvocations.New(pool) injected into runner.Deps.ToolInvocations | VERIFIED | Confirmed by runner_persist.go wiring and eval test using same pattern |
| `internal/runner/runner_persist.go` | `internal/toolinvocations` | persistToolInvocation writes one Event per ToolInvocation start/end (best-effort, WR-01 fixed) | VERIFIED | Lines 61-73: best-effort ledger insert with slog.Warn on failure; nil store is logged no-op |
| `internal/agent/tools/skill_read.go` | `internal/skills (via skillLoader.Snippet seam)` | renderSnippetUse(instructions, hostPath, interpreter) host frame | VERIFIED | renderSnippetUse at line 124; actionUse passes hostPath from seam (line 108) |
| `cmd/aura/serve_adapters.go` | `internal/skills.SnippetHostInvocation` | skillLoaderAdapter.Snippet resolves the host export-dir path | VERIFIED | exportDir field in skillLoaderAdapter; Snippet calls SnippetHostInvocation |
| `internal/eval/skills_snippet_reuse_cot_eval_test.go` | `internal/toolinvocations.Store.ListByConversation` | gate counts event_kind='end' rows + wall-clock over the 2nd-run window | VERIFIED | Line 418: toolinvocations.New(h.pool).ListByConversation; lines 430+: endEvents counting |
| `internal/eval/skills_snippet_reuse_cot_eval_test.go` | `runner.Runner` | runner.New(deps) with Deps.ToolInvocations=toolinvocations.New(pool) wired | VERIFIED | Line 247: ToolInvocations: toolinvocations.New(pool) |
| `internal/agent/tools/skill_write.go` | `internal/skills.Writer.Restore/Archive (via skillWriter seam)` | actionRestore/actionArchive dispatch to Writer lifecycle | VERIFIED | Lines 223-256: actionRestore calls t.Writer.Restore; actionArchive calls t.Writer.ArchiveSnippet |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| `skills_snippet_reuse_cot_eval_test.go` | `window` (ledgerWindow) | `toolinvocations.New(pool).ListByConversation` reads from live `aura.tool_invocations` table | YES — reads durable Postgres rows written by `persistToolInvocation` via runner | FLOWING |
| `internal/runner/runner_persist.go` | `ToolInvocation` events | `ev.Actions.ToolInvocation` from the production agent event stream | YES — fired per dispatched tool in the real runner loop | FLOWING |
| `internal/skills/writer_activate.go::Restore` | archived snippet dir | `promoteDir(archiveDir/name, activeDir/name)` + `Materialize` | YES — real filesystem operations, not mock | FLOWING |
| `internal/agent/tools/skill_read.go::renderSnippetUse` | `hostPath` | `skillLoader.Snippet` seam → `skillLoaderAdapter.Snippet` → `SnippetHostInvocation` → filepath.Join(cfg.SkillExportDir, ...) | YES — resolves from live config, not hardcoded | FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| `go build ./...` — module-wide compile with all Phase-18 artifacts | `wsl go build ./...` | exit 0, no output | PASS |
| `go vet ./...` — module-wide vet | `wsl go vet ./...` | exit 0, no output | PASS |
| Unit tests: toolinvocations + skills + agent/tools | `go test ./internal/toolinvocations/ ./internal/skills/ ./internal/agent/tools/` | ok 0.009s / ok 0.304s / ok 3.047s | PASS |
| Race detector: same packages | `go test -race ./internal/toolinvocations/ ./internal/skills/ ./internal/agent/tools/` | ok 1.052s / ok 1.512s / ok 4.098s | PASS |
| tools-skills boundary (no internal/skills import in internal/agent/tools) | `go list -deps ./internal/agent/tools \| grep -c internal/skills` | 0 | PASS |
| Structural parity key-free: TestRegistry_SeamFree + TestRegistrySnippetReuse_HasSkillTool | `env -u OPENROUTER_API_KEY go test -tags 'cot_eval db_integration' -run 'TestRegistry' ./internal/eval/` | PASS both, 0.008s | PASS |
| `go vet -tags cot_eval ./internal/eval/` | eval package vet under gate tags | exit 0 | PASS |
| `go build -tags cot_eval ./internal/eval/` | eval package build under gate tags | exit 0 | PASS |

### Probe Execution

| Probe | Command | Result | Status |
|-------|---------|--------|--------|
| D-03 live characterization probe | `aura chat new` with xlsx prompt via production surface | 21 tool dispatches, 142.8s, 1 distinct request_id — metric correction documented | PASS (operator-run, paid) |
| TestSnippetReuseE2E steady-state gate (paid) | `go test -tags 'cot_eval db_integration' -run TestSnippetReuseE2E ./internal/eval/` | Run 2 (post fixture fix): endEvents=5 ≤6, wallClock=11.057s <40s, today=true | PASS (operator-run, paid, commit 6a3c9d84) |
| TestRegistry_SeamFree / TestRegistrySnippetReuse_HasSkillTool (key-free, re-run this session) | `env -u OPENROUTER_API_KEY go test -tags 'cot_eval db_integration' -run TestRegistry ./internal/eval/` | PASS PASS 0.008s | PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| CAP-08.1 | 18-01 / 18-02 / 18-03 / 18-04 | Snippet reuse steady-state: UNGATED in-loop save (D-02) + HOST-PRIMARY posture (D-01) + restore/archive lifecycle + measured <40s/~5-call steady state from ledger | SATISFIED | `[x]` at REQUIREMENTS.md line 40; all four deliverables verified above; live gate endEvents=5/11.057s PASS |

Note: Traceability row at line 125 still reads "In Progress" — inconsistent with the `[x]` at line 40. This is a documentation gap needing human update (see Human Verification section).

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `.planning/REQUIREMENTS.md` | 125 | Traceability row "In Progress" contradicts `[x]` requirement at line 40 | Warning | Documentation inconsistency; no code impact |
| `docs/aura-quality-snapshot.md` | 368-369 | Phase-18 sub-table coverage/mutation rows still `pending-operator-run` after mutation was done and documented in the main matrix row | Warning | Sub-table not updated after results were stamped in the main row; needs one-line update |
| `docs/aura-quality-snapshot.md` | Phase-18 main row | writer_activate.go mutation 45.2% documented below the ≥70% gate, but 17 survivors labeled near-equivalent — not yet formally accepted as an override or advisory | Warning | Below stated gate threshold; Phase-10 precedent exists (claim.go/heartbeat.go advisory); needs explicit human decision |

No `TBD`, `FIXME`, or `XXX` markers found in Phase-18 modified files that lack formal follow-up references.

### Human Verification Required

#### 1. Coverage Gate Confirmation

**Test:** In WSL with full stack (PG + Neo4j migrated through 0011), run `bash scripts/coverage_gate.sh` and record the owned-surface combined coverage number. Update the Phase-18 sub-table rows (lines 368-369) in `docs/aura-quality-snapshot.md` with the live number and today's date.
**Expected:** Combined coverage ≥85% (verification context claims 86.1%; Phase-11 last measured was 86.6%).
**Why human:** The Phase-18 detail sub-table explicitly marks this `pending-operator-run`. The number in the verification context (86.1%) is not reflected in the snapshot file — it needs to be confirmed live and stamped in the file for the quality gate contract.

#### 2. CAP-08.1 Traceability Row Update

**Test:** Edit `.planning/REQUIREMENTS.md` line 125 from `| CAP-08.1 | Phase 18 — Slice 7e snippet reuse steady-state | In Progress |` to `| CAP-08.1 | Phase 18 — Slice 7e snippet reuse steady-state | Complete |`.
**Expected:** The traceability table is internally consistent with the `[x]` requirement checkbox at line 40.
**Why human:** A one-line documentation correction — no code involved, but needs a deliberate commit to close the phase cleanly.

#### 3. writer_activate.go Mutation Score Acceptance

**Test:** Explicitly decide whether the writer_activate.go mutation score of 45.2% (14/31) is accepted as advisory (per Phase-10 precedent for claim.go/heartbeat.go) or requires a follow-up run with FS fault injection.
**Expected:** Either: (a) an explicit note in the quality snapshot (or an override) accepting the 17 FS-fault near-equivalents as documented-equivalent — making the score advisory, not blocking — or (b) a follow-up mutation run that raises the score to ≥70%.
**Why human:** The 45.2% is below the ≥70% stated gate. The snapshot narrative documents why (FS-fault-injection error-wrap near-equivalents; killing them requires cross-platform-flaky FS fault injection, deliberately not chased). The Phase-10 precedent (claim.go/heartbeat.go scored unreliably due to subprocess bleeding) was explicitly marked advisory. The same decision is needed here. Only the operator can accept or escalate.

### Gaps Summary

No blocking gaps. The phase goal is structurally achieved: the steady-state reuse path collapses 21-call/142.8s authoring runs to 5-call/11.057s reuse runs (verified by the paid live gate from the durable ledger). Three documentation-consistency items are pending human closure before the phase can be declared fully complete:

1. Coverage number needs confirmation and stamping in the snapshot sub-table.
2. The CAP-08.1 traceability row needs "In Progress" → "Complete".
3. The writer_activate.go 45.2% mutation score needs explicit advisory acceptance or a follow-up run.

None of these are code correctness issues. All code-level review findings (CR-01 archive path traversal, WR-01 best-effort ledger, WR-02 redactor, WR-03 clampInt32, WR-04 shell-safe path, WR-05 name normalization, IN-01 StartedAt default, IN-02 meta unmarshal warn, IN-03 anyFloat, IN-04 skilladapters extraction, IN-05 error messages) were fixed and are verified present in the codebase.

---

_Verified: 2026-06-06_
_Verifier: Claude (gsd-verifier)_
