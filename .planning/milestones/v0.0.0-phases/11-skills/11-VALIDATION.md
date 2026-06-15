---
phase: 11
slug: skills
status: validated
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-05
validated: 2026-06-07
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
| 11-01-T1 | 11-01 | 1 | CAP-07/08 | T-11-01-T | PRD staleness removed | doc grep | `grep -c AURA_SKILL_ prd.md` | ✅ | ✅ (46 hits, 2026-06-07) |
| 11-01-T2 | 11-01 | 1 | CAP-07/08 | T-11-01-T | ROADMAP/REQ re-spec | doc grep | `grep -n default-ON .planning/ROADMAP.md` | ✅ | ✅ (3 hits, 2026-06-07) |
| 11-02-T1 | 11-02 | 2 | CAP-07 | T-11-02-T1/D1 | loader symlink-strip + no blocklist on load¹ | unit + goleak | `go test -race ./internal/skills/...` | ✅ | ✅ (2026-06-07) |
| 11-02-T2 | 11-02 | 2 | CAP-07 | T-11-02-I1 | manifest turn-stable; messages[0] frozen | unit | `go test -race ./internal/agent/tools/...` | ✅ | ✅ (2026-06-07) |
| 11-03-T1 | 11-03 | 3 | CAP-07 | T-11-03-T1 | NFKC-collapse blocklist rejected (SC#3) | fuzz + deterministic corpus | `go test -fuzz=FuzzSkillValidator -fuzztime=60s -run '^$' ./internal/skills/` + `go test -run TestSkillValidator_NFKCCorpus ./internal/skills/` | ✅ | ✅ (60s fuzz: 4.69M execs, 0 crashers; corpus PASS, 2026-06-07) |
| 11-03-T2 | 11-03 | 3 | CAP-07 | T-11-03-I1 | ~~catalog lax decode + installs rank~~ **superseded by 7g (#51/D-40)**: catalog client deleted; discovery now via find-skills-aura builtin | unit | `go test -race ./internal/skills/` | ✅ | ✅ (surface deleted in 11-09; replacement covered by 11-09-T1, 2026-06-07) |
| 11-04-T1 | 11-04 | 4 | CAP-07 | T-11-04-R1 | audit append-only + TRUNCATE trigger + role sep (SC#1/SC#2) | db_integration | `go test -tags db_integration -p 1 -run 'TestAuditImmutable\|TestMigration0010' ./internal/skills/` (needs `POSTGRES_PASSWORD` + composed DSNs) | ✅ | ✅ (live PASS ×2 tests, 2026-06-07) |
| 11-04-T2 | 11-04 | 4 | CAP-07 | T-11-04-T1 | writer scoring-gated + materialize symlink-strip | unit | `go test -race -run 'TestWriter\|TestMaterialize' ./internal/skills/` | ✅ | ✅ (real matches confirmed, 2026-06-07) |
| 11-05-T1 | 11-05 | 5 | CAP-07 | T-11-05-E1/E2 | no model approve; headless never self-activates | unit | `go test -race ./internal/agent/tools/... ./internal/skills/` | ✅ | ✅ (2026-06-07) |
| 11-05-T2 | 11-05 | 5 | CAP-07 | T-11-05-D1/I1 | messages[1] survives L2.5; messages[0] byte-stable | unit + CI | `go test -race ./internal/conversations/ && bash scripts/cache_invariant_audit.sh` | ✅ | ✅ (22-turn ×3 hashes constant, 2026-06-07) |
| 11-06-T1 | 11-06 | 6 | CAP-07 | T-11-06-T1/E1 | ~~native clone + canonical hash + red flags~~ **superseded by 7g (#51/D-40)**: installer deleted; replacement = Loader blocklist scan + find-skills-aura builtin | unit | `go test -race -run 'TestMaterializeBuiltins\|TestMaterializeFindSkillsAuraAlwaysOn\|TestLoaderBlocklist' ./internal/skills/` | ✅ | ✅ (re-targeted — old `-run 'TestInstall\|TestCanonicalHash'` was a `[no tests to run]` false-green; new command runs 5 real tests PASS, 2026-06-07) |
| 11-06-T2 | 11-06 | 6 | CAP-07 | T-11-06-E2 | ~~install gated, catalog default-ON (SC#5)~~ **SC#5 superseded by 7g**: "install" dropped from action enum (asserted in skill_test.go) | unit | `go test -race ./internal/agent/tools/...` | ✅ | ✅ (2026-06-07) |
| 11-07-T1 | 11-07 | 7 | CAP-08 | T-11-07-E1/T1 | bearer token + /skills mount (rw per #50/D-15c) | unit + compose config | `go test -race ./internal/sandboxagent/... && docker compose config --quiet` | ✅ | ✅ (2026-06-07) |
| 11-07-T2 | 11-07 | 7 | CAP-08 | T-11-07-T2 | snippet by-path exec, output captured (SC#4) | sandbox_integration | `go test -tags 'sandbox_integration db_integration' -p 1 -run TestSnippetExec ./internal/skills/` (needs DSNs + `AURA_SANDBOX_AGENT_URL` + `AURA_SANDBOX_AGENT_TOKEN` + `AURA_SKILL_EXPORT_DIR` = the live /skills mount source) | ✅ | ✅ (live PASS vs running aura-sandbox-agent, 2026-06-07) |
| 11-07-T3 | 11-07 | 7 | CAP-08 | T-11-07-D1 | TTL sweep TaskKind + seed (kind CHECK widened) | db_integration + unit | `go test -tags db_integration -p 1 -run 'TestSkillTTLSweep\|TestSkillTTLSeed' ./internal/cron/...` | ✅ | ✅ (TestSkillTTLSeed live PASS + 4 handler unit PASS, 2026-06-07) |
| 11-08-T1 | 11-08 | 8 | CAP-07/08 | T-11-08-T1 | xlsx E2E artifact-not-reply + 2 smokes | cot_eval (operator) + unit | `go build -tags cot_eval ./internal/eval/... && go test -race -run 'TestHaikuFlow\|TestSnippetReuse' ./internal/skills/` | ✅ | ✅ (build + both smokes PASS, 2026-06-07) |
| 11-08-T2 | 11-08 | 8 | CAP-07/08 | T-11-08-R1 | CI all tiers no-skip-as-green + coverage ≥85% | CI | `.github/workflows/skills.yml` (unit+race / fuzz 60s / db_integration / sandbox_integration / mutation / coverage all present) | ✅ | ✅ (all 6 tiers wired; combined coverage 86.6% stamped 2026-06-06) |
| 11-08-T3 | 11-08 | 8 | CAP-07/08 | T-11-08-T1 | live .xlsx visually verified + judge ≥90% + SC#2 | human-verify | manual (Gate-3 checkpoint) | ✅ | ✅ (closed at Gate-3: chat-E2E gate 6/6 ×2 runs 2026-06-06, fresh .xlsx openpyxl-verified; 11-VERIFICATION score 100) |
| 11-09-T1 | 11-09 | 9 | CAP-07/08 | #51/D-40 | 7g deletion: find-skills-aura builtin always-on + Loader injection-blocklist scan | unit | `go test -race -run 'TestMaterializeBuiltins\|TestMaterializeFindSkillsAuraAlwaysOn\|TestLoaderBlocklist' ./internal/skills/` | ✅ | ✅ (5 tests PASS, 2026-06-07) |
| 11-09-T2 | 11-09 | 9 | CAP-07/08 | #51/D-40 | byte-stable SystemPrompt §Skills shrink + no superseded routing + ask_user-only-pause constraint | unit | `go test -race -run 'TestPrompt_DocSyncByteIdentical\|TestPrompt_NoSupersededSkillRouting\|TestAskUserOnlyPauseConstraint' ./internal/agent/` | ✅ | ✅ (PASS, 2026-06-07) |
| 11-10-T1 | 11-10 | 10 | CAP-07/08 | D-35/D-40 | 7g eval rewrite: action-aware capture from structured tool args + seam-free registry | structural (no-key) | `go test -tags cot_eval -run 'TestRegistry\|TestClassify' ./internal/eval/` | ✅ | ✅ (TestClassifyCall_SelfInstall 10 subtests + registry parity PASS, 2026-06-07) |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

All Phase-11 test files are greenfield (`internal/skills/` has zero code today). Each plan's first code task creates the test files it needs alongside the implementation (TDD-leaning per the `<behavior>` blocks). The cross-cutting scaffolds:

- [x] `internal/skills/main_test.go` — `goleak.VerifyTestMain(m)` (plan 11-02 Task 1) — gates every later skills test for goroutine leaks
- [x] `internal/skills/validator_fuzz_test.go` — `FuzzSkillValidator` + a deterministic `TestSkillValidator_NFKCCorpus` companion so CI exercises the corpus without `-fuzz` (plan 11-03 Task 1, SC#3)
- [x] `internal/skills/audit_store_integration_test.go` (build tag `db_integration`) — skip-helper t.Fatal-under-`$CI` honoring no-skip-as-green; SC#1 INSERT + SC#2 immutability/TRUNCATE/role-denied + `TestMigration0010_SchemaRoundTrip` (plan 11-04 Task 1)
- [x] `internal/skills/snippet_integration_test.go` (build tags `sandbox_integration db_integration`) — `TestSnippetExec` by-path exec; t.Fatal-under-`$CI` (plan 11-07 Task 2, SC#4)
- [x] `internal/cron/handlers/skill_ttl_test.go` (+ `db_integration` seed test `TestSkillTTLSeed` in `internal/cron/`) — kind-CHECK acceptance (A2 landmine) (plan 11-07 Task 3)
- [x] `internal/eval/` cot_eval scenarios — xlsx North-Star, OPENROUTER-gated, operator-run NOT CI (plan 11-08 Task 1, D-35; rewritten action-aware in 11-10 per #51)
- [x] Extend `scripts/cache_invariant_audit.sh` — messages[1] always-block byte-stability + manifest-in-Description turn-stability (plan 11-08 Task 1)
- [x] `.github/workflows/skills.yml` — composed DSNs + AURA_SANDBOX_AGENT_TOKEN + sandbox-agent build/start + CI=true (plan 11-08 Task 2)

¹ Loader gained the 7g injection-blocklist scan in 11-09 (#51/D-28 amended): `TestLoaderBlocklistBodySkipped` / `TestLoaderBlocklistDescriptionSkipped` — load now skips blocklisted bodies (write-boundary validation unchanged).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions | Closure |
|----------|-------------|------------|-------------------|---------|
| Live xlsx North-Star E2E (post-#51 shape: self-install via structured args → .xlsx with today's data) | CAP-07/CAP-08 | OPENROUTER_API_KEY-gated live LLM run; the produced .xlsx must be opened + content-verified visually (artifact-not-reply); not CI (D-35) | Operator directive 2026-06-06: the REAL-binary `scripts/chat-e2e-gate.sh` supersedes the synthetic TestSkillsE2E judge as the closing score (cot_eval stays as structural guard) | **CLOSED 2026-06-06** — chat-E2E gate PASS 6/6 ×2 consecutive runs; fresh .xlsx openpyxl-verified + today-date + PG turns persisted (quality snapshot + 11-VERIFICATION score 100) |
| SC#2 `aura skills audit purge` as aura_app → permission denied | CAP-07 | exercised in the db_integration tier (automated) AND confirmed manually at the Gate-3 checkpoint as the role-separation sign-off | run as the aura_app role and observe the trigger/grant rejection | **CLOSED** — TestAuditImmutable live PASS 2026-06-07 (TRUNCATE trigger + role-denied asserted); Gate-3 sign-off in 11-VERIFICATION |
| go-mutesting ≥70% spot-check on validator.go + writer.go | CAP-07 | mutation testing runs on WSL go-mutesting (go1.26 fork); documented in the snapshot Manual-Only table per CLAUDE.md | WSL: `GOFLAGS=-tags=db_integration go-mutesting internal/skills/validator.go` (PASS=killed) | **CLOSED 2026-06-06** — validator.go **83.3% (15/18) HARD-gate PASS**; writer.go advisory 0.379 (db-subprocess artifact, same documented unreliability as Phase-10 claim.go/heartbeat.go; correctness witnessed by live db_integration tests); live successors measured in Phase 18: skill_write.go 95.5%, writer_activate.go 45.2% advisory-accepted (quality snapshot §mutation) |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify (the only manual task is the final Gate-3 checkpoint, preceded by automated CI)
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 120s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** validated post-execution (2026-06-07)

---

## Validation Audit 2026-06-07

Retroactive Nyquist close-out (`/gsd-validate-phase 11`) — every per-task automated command executed live against the running stack (Postgres + sandbox-agent + composed DSNs), not compile-checked.

| Metric | Count |
|--------|-------|
| Per-task rows audited | 18 original + 3 added (11-09/11-10 waves, post-#51) |
| Gaps found | 4 (1 `[no tests to run]` false-green command, 2 rows referencing 7g-deleted surface, 2 missing wave rows) |
| Resolved | 4 (11-06-T1 re-targeted to Loader-blocklist/builtin tests; 11-03-T2 + 11-06-T2 annotated superseded-by-7g; 11-09-T1/T2 + 11-10-T1 rows added, all green) |
| Escalated | 0 |
| Missing tests generated | 0 (all behaviors already covered) |

Live-run evidence (2026-06-07): race unit tiers green (skills, agent/tools, agent, conversations, sandboxagent); 60s `FuzzSkillValidator` 4.69M execs 0 crashers; db_integration PASS (TestAuditImmutable, TestMigration0010_SchemaRoundTrip, TestSkillTTLSeed); sandbox_integration `TestSnippetExec` PASS against the live `aura-sandbox-agent` container (`AURA_SKILL_EXPORT_DIR` = the mounted /skills source); `cache_invariant_audit.sh` 22-turn ×3-hash constant; `docker compose config` valid; skills.yml carries all 6 tiers.
