---
phase: 18
slug: slice-7e-executable-snippet-reuse-steady-state-artifact-runs
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-06
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
| 18-01-T1 | 18-01 | 1 | CAP-08.1 | T-18-02-I | PRD-first amendment, no code; secret-free docs | grep | `grep -q RESOLVED prd.md && grep -q CAP-08.1 .planning/REQUIREMENTS.md` | ✅ (prd.md, REQUIREMENTS.md) | ⬜ pending |
| 18-01-T2 | 18-01 | 1 | CAP-08.1 | T-18-01-R | Append-only ledger triggers + role grant | unit+integration | `go build ./internal/toolinvocations/ ./internal/runner/ ./cmd/aura/ && go test ./internal/toolinvocations/` | ✅ (in-flight, commit-as-is) | ⬜ pending |
| 18-01-T3 | 18-01 | 1 | CAP-08.1 | T-18-02-I | D-03 probe; no secret in dump | E2E (operator) | `go test -tags cot_eval -run TestSkillsE2E ./internal/eval/` + ledger dump to docs/ | ⚠ (new doc artifact) | ⬜ pending |
| 18-02-T1 | 18-02 | 2 | CAP-08.1 | T-18-05-T | SanitizeName before filepath.Join | unit | `go test ./internal/skills/ -run 'TestUseSnippet|TestSnippet'` | ⚠ (UPDATE load-bearing literal) | ⬜ pending |
| 18-02-T2 | 18-02 | 2 | CAP-08.1 | T-18-04-E | Host frame only for active (approved) snippets | unit | `go test ./internal/agent/tools/ -run 'TestSnippet|TestRenderSnippetUse|TestActionUse' && go build ./cmd/aura/` | ⚠ (UPDATE skill_read_test) | ⬜ pending |
| 18-03-T1 | 18-03 | 3 | CAP-08.1 | T-18-09-T | Restore audits as activate/cli (no new migration) | unit+integration | `go test -tags db_integration ./internal/skills/ -run 'TestRestore'` | ❌ Wave 0 (new) | ⬜ pending |
| 18-03-T2 | 18-03 | 3 | CAP-08.1 | T-18-07-E | Save UNGATED never returns the pause sentinel (D-02) | unit | `go test ./internal/agent/tools/ -run 'TestActionRestore|TestActionArchive|TestSnippetSaveAction|TestAskUserOnlyPause' && go build ./cmd/aura/` | ❌ Wave 0 (new) | ⬜ pending |
| 18-04-T1 | 18-04 | 4 | CAP-08.1 | T-18-12-T | Eval registry registers production skill tool (parity) | structural (no-key) | `go test -tags cot_eval -run 'TestRegistry|TestClassify' ./internal/eval/` | ⚠ (REVISE for skill-tool parity) | ⬜ pending |
| 18-04-T2 | 18-04 | 4 | CAP-08.1 | T-18-13-R | Steady-state read from append-only ledger, artifact-not-reply | E2E (operator) | live: `go test -tags 'cot_eval db_integration' -run TestSnippetReuseE2E ./internal/eval/` | ❌ Wave 0 (new) | ⬜ pending |
| 18-04-T3 | 18-04 | 4 | CAP-08.1 | T-18-11-I | Phase quality gate + snapshot; no secret leak | E2E + quality (operator) | `make quality-full` + `bash scripts/cache_invariant_audit.sh` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] Commit the in-flight `internal/toolinvocations` + `0011_tool_invocations.{up,down}.sql` + `internal/db/queries/tool_invocations.sql` + generated sqlc + `runner_persist.go` wiring + `cmd/aura/shell.go` (18-01-T2) — the gate's measurement substrate
- [ ] `internal/agent/tools/skill_test.go` — `TestActionRestore`, `TestActionArchive`, `TestSnippetSaveAction` (new handler coverage, 18-03-T2)
- [ ] `internal/skills/snippet_restore_integration_test.go` — Archive→Restore db_integration round-trip (18-03-T1)
- [ ] `internal/agent/tools/skill_read_test.go` + `internal/skills/snippet_test.go` — UPDATE the snippet `use`-frame load-bearing literal to the HOST shell_exec frame (D-01, 18-02)
- [ ] `internal/eval/skills_snippet_reuse_cot_eval_test.go` — the steady-state 2-run + ledger-window gate (18-04-T2)
- [ ] `internal/eval/classify_cot_eval_test.go` — UPDATE TestRegistry to assert the snippet-reuse registry registers the `skill` tool (key-free parity, 18-04-T1)
- [ ] `docs/phase-18-xlsx-call-breakdown.md` — the D-03 live ledger characterization that grounds the steady-state thresholds (18-01-T3)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| D-03 live xlsx call-breakdown probe | CAP-08.1 | Paid live LLM run (OPENROUTER_API_KEY); the ~5-call target is provisional until grounded | Bring up SearXNG + .env; `go test -tags cot_eval -run TestSkillsE2E -timeout 900s ./internal/eval/`; dump ledger per-call breakdown to docs/phase-18-xlsx-call-breakdown.md; OPEN the .xlsx visually (18-01-T3) |
| Steady-state reuse E2E (2nd-run ledger window) | CAP-08.1 | Paid live LLM run + DB-backed; non-deterministic; the release-blocking timing gate | Stack up (SearXNG + Postgres@0011); `go test -tags 'cot_eval db_integration' -run TestSnippetReuseE2E -timeout 900s ./internal/eval/`; confirm 2nd-run distinct-request-id ≤ grounded target + wall-clock < 40s + fresh .xlsx (18-04-T2) |
| Mutation spot-check ≥70% on new handlers | CAP-08.1 | go-mutesting runs only on WSL (go1.26 fork) | WSL: go-mutesting on Writer.Restore + actionRestore/actionArchive/actionSaveSnippet + SnippetHostPath; PASS=killed (18-04-T3) |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
