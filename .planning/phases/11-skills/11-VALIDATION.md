---
phase: 11
slug: skills
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-05
---

# Phase 11 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) + `go test -fuzz` + `pgregory.net/rapid` + goleak + build-tag tiers (`db_integration`, `sandbox_integration`, `cot_eval`) |
| **Config file** | Makefile (quality/quality-full gates) |
| **Quick run command** | `go vet ./... && go build ./... && go test -race ./internal/skills/... ./internal/agent/tools/...` |
| **Full suite command** | WSL: `make quality-full` (stack up — vet+build+lint+race+coverage≥85%+integration+mutation) |
| **Estimated runtime** | ~60 seconds quick / ~12 min full |

---

## Sampling Rate

- **After every task commit:** `go vet ./... && go build ./... && go test -race ./internal/<touched-package>/`
- **After every plan wave:** full tag-matrix for touched packages (`-tags 'db_integration sandbox_integration'`, composed DSNs, `make sandbox-up` + AURA_SANDBOX_AGENT_TOKEN)
- **Before `/gsd-verify-work`:** `make quality-full` green
- **Max feedback latency:** 120 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 11-01-T1 | 11-01 | 1 | CAP-07/08 | T-11-01-T | PRD staleness removed | doc grep | `grep -c AURA_SKILL_ prd.md` | ❌ W0 | ⬜ |
| 11-01-T2 | 11-01 | 1 | CAP-07/08 | T-11-01-T | ROADMAP/REQ re-spec | doc grep | `grep -n default-ON .planning/ROADMAP.md` | ❌ W0 | ⬜ |
| 11-02-T1 | 11-02 | 2 | CAP-07 | T-11-02-T1/D1 | loader symlink-strip + no blocklist on load | unit + goleak | `go test -race ./internal/skills/...` | ❌ W0 | ⬜ |
| 11-02-T2 | 11-02 | 2 | CAP-07 | T-11-02-I1 | manifest turn-stable; messages[0] frozen | unit | `go test -race ./internal/agent/tools/...` | ❌ W0 | ⬜ |
| 11-03-T1 | 11-03 | 3 | CAP-07 | T-11-03-T1 | NFKC-collapse blocklist rejected (SC#3) | fuzz | `go test -fuzz=FuzzSkillValidator -fuzztime=60s ./internal/skills/` | ❌ W0 | ⬜ |
| 11-03-T2 | 11-03 | 3 | CAP-07 | T-11-03-I1 | catalog lax decode + installs rank | unit (httptest) | `go test -race ./internal/skills/` | ❌ W0 | ⬜ |
| 11-04-T1 | 11-04 | 4 | CAP-07 | T-11-04-R1 | audit append-only + TRUNCATE trigger + role sep (SC#1/SC#2) | db_integration | `go test -tags db_integration -run 'TestAuditImmutable\|TestMigration0010' ./internal/skills/` | ❌ W0 | ⬜ |
| 11-04-T2 | 11-04 | 4 | CAP-07 | T-11-04-T1 | writer scoring-gated + materialize symlink-strip | unit | `go test -race -run 'TestWriter\|TestMaterialize' ./internal/skills/` | ❌ W0 | ⬜ |
| 11-05-T1 | 11-05 | 5 | CAP-07 | T-11-05-E1/E2 | no model approve; headless never self-activates | unit | `go test -race ./internal/agent/tools/... ./internal/skills/` | ❌ W0 | ⬜ |
| 11-05-T2 | 11-05 | 5 | CAP-07 | T-11-05-D1/I1 | messages[1] survives L2.5; messages[0] byte-stable | unit + CI | `go test -race ./internal/conversations/ && bash scripts/cache_invariant_audit.sh` | ❌ W0 | ⬜ |
| 11-06-T1 | 11-06 | 6 | CAP-07 | T-11-06-T1/E1 | native clone + canonical hash + always-strip + red flags (SC#1) | unit (file:// clone) | `go test -race -run 'TestInstall\|TestCanonicalHash' ./internal/skills/` | ❌ W0 | ⬜ |
| 11-06-T2 | 11-06 | 6 | CAP-07 | T-11-06-E2 | install gated, catalog default-ON (SC#5) | unit | `go test -race ./internal/agent/tools/...` | ❌ W0 | ⬜ |
| 11-07-T1 | 11-07 | 7 | CAP-08 | T-11-07-E1/T1 | bearer token + ro /skills mount (D-38) | unit + compose config | `go test -race ./internal/sandboxagent/... && docker compose config` | ❌ W0 | ⬜ |
| 11-07-T2 | 11-07 | 7 | CAP-08 | T-11-07-T2 | snippet by-path exec, output captured (SC#4) | sandbox_integration | `go test -tags 'sandbox_integration db_integration' -run TestSnippetExec ./internal/skills/` | ❌ W0 | ⬜ |
| 11-07-T3 | 11-07 | 7 | CAP-08 | T-11-07-D1 | TTL sweep TaskKind + seed (kind CHECK widened) | db_integration | `go test -tags db_integration -run 'TestSkillTTLSweep\|TestSkillTTLSeed' ./internal/cron/...` | ❌ W0 | ⬜ |
| 11-08-T1 | 11-08 | 8 | CAP-07/08 | T-11-08-T1 | xlsx E2E artifact-not-reply + 2 smokes | cot_eval (operator) + unit | `go build -tags cot_eval ./internal/eval/... && go test -race -run 'TestHaikuFlow\|TestSnippetReuse' ./internal/skills/` | ❌ W0 | ⬜ |
| 11-08-T2 | 11-08 | 8 | CAP-07/08 | T-11-08-R1 | CI all tiers no-skip-as-green + coverage ≥85% | CI | `.github/workflows/skills.yml` (db+sandbox+fuzz+mutation+coverage) | ❌ W0 | ⬜ |
| 11-08-T3 | 11-08 | 8 | CAP-07/08 | T-11-08-T1 | live .xlsx visually verified + judge ≥90% + SC#2 | human-verify | manual (Gate-3 checkpoint) | ❌ W0 | ⬜ |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

All Phase-11 test files are greenfield (`internal/skills/` has zero code today). Each plan's first code task creates the test files it needs alongside the implementation (TDD-leaning per the `<behavior>` blocks). The cross-cutting scaffolds:

- [ ] `internal/skills/main_test.go` — `goleak.VerifyTestMain(m)` (plan 11-02 Task 1) — gates every later skills test for goroutine leaks
- [ ] `internal/skills/validator_fuzz_test.go` — `FuzzSkillValidator` + a deterministic `TestSkillValidator_NFKCCorpus` companion so CI exercises the corpus without `-fuzz` (plan 11-03 Task 1, SC#3)
- [ ] `internal/skills/audit_store_integration_test.go` (build tag `db_integration`) — skip-helper t.Fatal-under-`$CI` honoring no-skip-as-green; SC#1 INSERT + SC#2 immutability/TRUNCATE/role-denied + `TestMigration0010_SchemaRoundTrip` (plan 11-04 Task 1)
- [ ] `internal/skills/snippet_test.go` (build tags `sandbox_integration db_integration`) — `TestSnippetExec` by-path exec; t.Fatal-under-`$CI` (plan 11-07 Task 2, SC#4)
- [ ] `internal/cron/handlers/skill_ttl_test.go` (+ `db_integration` seed test) — kind-CHECK acceptance (A2 landmine) (plan 11-07 Task 3)
- [ ] `internal/eval/scenarios_skills.go` + `skills_cot_eval.go` (build tag `cot_eval`) — xlsx North-Star, OPENROUTER-gated, operator-run NOT CI (plan 11-08 Task 1, D-35)
- [ ] Extend `scripts/cache_invariant_audit.sh` — messages[1] always-block byte-stability + manifest-in-Description turn-stability (plan 11-08 Task 1)
- [ ] `.github/workflows/skills.yml` — composed DSNs + AURA_SANDBOX_AGENT_TOKEN + `make sandbox-up` + CI=true (plan 11-08 Task 2)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live xlsx North-Star E2E (autonomous catalog→ask_user→install→sandbox_exec→.xlsx with today's data) | CAP-07/CAP-08 | OPENROUTER_API_KEY-gated live LLM run; the produced .xlsx must be opened + content-verified visually (artifact-not-reply); not CI (D-35) | Plan 11-08 Task 3 checkpoint: `make sandbox-up` + Postgres@0010 + export OPENROUTER/AURA_EVAL_SELF_*/token/DSNs/SEARXNG_URL → `go test -tags cot_eval -run TestSkillsE2E ./internal/eval/` → open the .xlsx, confirm today's market data + no mojibake + judge ≥90% + tool_use order |
| SC#2 `aura skills audit purge` as aura_app → permission denied | CAP-07 | exercised in the db_integration tier (automated) AND confirmed manually at the Gate-3 checkpoint as the role-separation sign-off | run as the aura_app role and observe the trigger/grant rejection |
| go-mutesting ≥70% spot-check on validator.go + writer.go | CAP-07 | mutation testing runs on WSL go-mutesting (go1.26 fork); documented in the snapshot Manual-Only table per CLAUDE.md | WSL: `GOFLAGS=-tags=db_integration go-mutesting ./internal/skills/validator.go ./internal/skills/writer.go` (PASS=killed) |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify (the only manual task is the final Gate-3 checkpoint, preceded by automated CI)
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 120s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** ready for execution
