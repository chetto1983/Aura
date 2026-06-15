---
phase: 18
slug: slice-7e-executable-snippet-reuse-steady-state-artifact-runs
status: validated
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-06
validated: 2026-06-07
---

# Phase 18 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) + goleak + build-tag tiers (`db_integration`, `cot_eval`) |
| **Config file** | Makefile (quality/quality-full gates) |
| **Quick run command** | `go vet ./... && go build ./... && go test -race ./internal/skills/ ./internal/agent/tools/ ./internal/toolinvocations/` |
| **Full suite command** | WSL: `make quality-full` (stack up — vet+build+lint+race+coverage≥85%+integration+mutation) |
| **Estimated runtime** | ~60 seconds quick / ~12 min full |

---

## Sampling Rate

- **After every task commit:** `go vet ./... && go build ./... && go test -race ./internal/<touched-package>/`
- **After every plan wave:** full tag-matrix for touched packages (`-tags 'db_integration'`, composed DSNs from POSTGRES_PASSWORD)
- **Before `/gsd-verify-work`:** `make quality-full` green + the steady-state reuse E2E run live (2nd-run timing measured from the tool_invocations ledger, not the reply) + `cache_invariant_audit.sh`
- **Max feedback latency:** 120 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 18-01-T1 | 18-01 | 1 | CAP-08.1 | T-18-02-I | PRD-first amendment, no code; secret-free docs | grep | `grep -q RESOLVED prd.md && grep -q CAP-08.1 .planning/REQUIREMENTS.md` | ✅ | ✅ (PASS, 2026-06-07) |
| 18-01-T2 | 18-01 | 1 | CAP-08.1 | T-18-01-R | Append-only ledger triggers + role grant; db_integration round-trip (no-skip-as-green) | unit+integration | `go build ./internal/toolinvocations/ ./internal/runner/ ./cmd/aura/ && go test ./internal/toolinvocations/ && go test -tags db_integration -p 1 ./internal/toolinvocations/ ./internal/runner/` (needs `POSTGRES_PASSWORD` + composed DSNs) | ✅ | ✅ (live PASS incl. TestLedgerRoundTrip + TestLedgerAppendOnly, 2026-06-07) |
| 18-01-T3 | 18-01 | 1 | CAP-08.1 | T-18-02-I | D-03 probe; no secret in dump | E2E (operator) | `go test -tags cot_eval -run TestSkillsE2E ./internal/eval/` + ledger dump to docs/ | ✅ (`docs/phase-18-xlsx-call-breakdown.md`) | ✅ (operator-run; artifact committed — see Manual-Only) |
| 18-02-T1 | 18-02 | 2 | CAP-08.1 | T-18-05-T | SanitizeName before filepath.Join | unit | `go test ./internal/skills/ -run 'TestUseSnippet|TestSnippet'` | ✅ | ✅ (5 tests PASS, 2026-06-07) |
| 18-02-T2 | 18-02 | 2 | CAP-08.1 | T-18-04-E | Host frame only for active (approved) snippets | unit | `go test ./internal/agent/tools/ -run 'TestSnippet|TestRenderSnippetUse|TestActionUse' && go build ./cmd/aura/` | ✅ | ✅ (6 tests PASS + build ok, 2026-06-07) |
| 18-03-T1 | 18-03 | 3 | CAP-08.1 | T-18-09-T | Restore audits as activate/cli (no new migration, no AuditRestore constant) | unit+integration | `go test -tags db_integration -p 1 ./internal/skills/ -run 'TestRestore'` | ✅ | ✅ (live PASS ×4 incl. TestRestoreSnippetRoundTrip, 2026-06-07) |
| 18-03-T2 | 18-03 | 3 | CAP-08.1 | T-18-07-E | Save UNGATED never returns the pause sentinel (D-02) | unit | `go test ./internal/agent/tools/ -run 'TestActionRestore|TestActionArchive|TestSnippetSaveAction' && go test -race -run TestAskUserOnlyPauseConstraint ./internal/agent/ && go build ./cmd/aura/` | ✅ | ✅ (5 handler tests + pause-constraint PASS, 2026-06-07 — command fixed: the pause-constraint test lives in `./internal/agent/` as `TestAskUserOnlyPauseConstraint`; the original `TestAskUserOnlyPause` leg matched nothing in `./internal/agent/tools/`) |
| 18-04-T1 | 18-04 | 4 | CAP-08.1 | T-18-12-T | Eval registry registers production skill tool (parity); no silent adapter dup | structural (no-key) | `go test -tags cot_eval -run 'TestRegistry|TestClassify' ./internal/eval/` | ✅ | ✅ (TestRegistrySnippetReuse_HasSkillTool + classify suite PASS key-free, 2026-06-07) |
| 18-04-T2 | 18-04 | 4 | CAP-08.1 | T-18-13-R | Steady-state read from append-only ledger via production runner.Runner (Deps.ToolInvocations), artifact-not-reply | E2E (operator) | live: `go test -tags 'cot_eval db_integration' -run TestSnippetReuseE2E ./internal/eval/` | ✅ | ✅ (operator-run 2026-06-06: **5 dispatches / 11.057s** live PASS — ledger-asserted, < ~5-call/40s gate; see Manual-Only) |
| 18-04-T3 | 18-04 | 4 | CAP-08.1 | T-18-11-I | Phase quality gate + snapshot; no secret leak | E2E + quality (operator) | `make quality-full` + `bash scripts/cache_invariant_audit.sh` | ✅ | ✅ (coverage 86.1% stamped in 453dcc0c 2026-06-06; cache invariant re-run green 2026-06-07: 22-turn ×3-hash constant) |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [x] Commit the in-flight `internal/toolinvocations` + `0011_tool_invocations.{up,down}.sql` + `internal/db/queries/tool_invocations.sql` + generated sqlc + `runner_persist.go` wiring + `cmd/aura/shell.go` (18-01-T2) — the gate's measurement substrate
- [x] A `db_integration` ledger round-trip test (Insert start+end -> ListByConversation returns both correlated) — `TestLedgerRoundTrip` + `TestLedgerAppendOnly` in `internal/toolinvocations/` (live PASS 2026-06-07)
- [x] `internal/agent/tools/skill_test.go` — `TestActionRestore`, `TestActionArchive`, `TestSnippetSaveAction` (new handler coverage, 18-03-T2)
- [x] `internal/skills/snippet_restore_integration_test.go` — Archive→Restore db_integration round-trip (18-03-T1)
- [x] `internal/agent/tools/skill_read_test.go` + `internal/skills/snippet_test.go` — snippet `use`-frame load-bearing literal updated to the HOST shell_exec frame (D-01, 18-02)
- [x] `internal/eval/skills_snippet_reuse_cot_eval_test.go` — the steady-state 2-run + ledger-window gate driven through the production runner.Runner (18-04-T2)
- [x] `internal/eval/classify_cot_eval_test.go` — `TestRegistrySnippetReuse_HasSkillTool` asserts the snippet-reuse registry registers the `skill` tool (key-free parity, 18-04-T1)
- [x] `docs/phase-18-xlsx-call-breakdown.md` — the D-03 live ledger characterization that grounds the steady-state thresholds (18-01-T3)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions | Closure |
|----------|-------------|------------|-------------------|---------|
| D-03 live xlsx call-breakdown probe | CAP-08.1 | Paid live LLM run (OPENROUTER_API_KEY); the ~5-call target is provisional until grounded | Bring up SearXNG + .env; `go test -tags cot_eval -run TestSkillsE2E -timeout 900s ./internal/eval/`; dump ledger per-call breakdown to docs/phase-18-xlsx-call-breakdown.md; OPEN the .xlsx visually (18-01-T3) | **CLOSED 2026-06-06** — probe run, breakdown committed at `docs/phase-18-xlsx-call-breakdown.md` (grounded the end-event ≤6 + <40s gate; D-03 finding: count `event_kind='end'` rows, not request_ids) |
| Steady-state reuse E2E (2nd-run ledger window) | CAP-08.1 | Paid live LLM run + DB-backed; non-deterministic; the release-blocking timing gate | Stack up (SearXNG + Postgres@0011); `go test -tags 'cot_eval db_integration' -run TestSnippetReuseE2E -timeout 900s ./internal/eval/`; confirm 2nd-run ledger end-events ≤ grounded target + wall-clock < 40s + fresh .xlsx (18-04-T2) | **CLOSED 2026-06-06** — live PASS: **5 dispatches / 11.057s** on the 2nd run, ledger-asserted artifact-not-reply (REQUIREMENTS CAP-08.1 traceability + 18-VERIFICATION 9/9) |
| Mutation spot-check ≥70% on new handlers | CAP-08.1 | go-mutesting runs only on WSL (go1.26 fork) | WSL: go-mutesting on Writer.Restore + actionRestore/actionArchive/actionSaveSnippet + SnippetHostPath; PASS=killed (18-04-T3) | **CLOSED 2026-06-06** — skill_write.go **95.5% (21/22)**; writer_activate.go 45.2% **advisory-accepted by operator** per Phase-10 precedent (all meaningful/new-handler mutants killed in 60eb932e; 17 survivors documented FS-fault near-equivalents, quality snapshot) |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 120s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** validated post-execution (2026-06-07)

---

## Validation Audit 2026-06-07

Retroactive Nyquist close-out (`/gsd-validate-phase 18`) — every per-task automated command executed live against the running stack (Postgres@0011 + composed DSNs), not compile-checked.

| Metric | Count |
|--------|-------|
| Per-task rows audited | 10 |
| Gaps found | 1 (18-03-T2 `TestAskUserOnlyPause` leg matched nothing in `./internal/agent/tools/` — silent no-match) |
| Resolved | 1 (command re-pointed to `TestAskUserOnlyPauseConstraint` in `./internal/agent/`; runs green) |
| Escalated | 0 |
| Missing tests generated | 0 (all behaviors already covered) |

Live-run evidence (2026-06-07): grep gates PASS; builds clean; unit filters all real-match green (5+6+5 tests); db_integration PASS (toolinvocations ledger round-trip + append-only, runner suite, TestRestore ×4); eval structural parity green key-free; `cache_invariant_audit.sh` 22-turn ×3-hash constant. Operator-gated items (D-03 probe, steady-state E2E, mutation) were closed 2026-06-06 with committed evidence — recorded in the Manual-Only Closure column.
</content>
