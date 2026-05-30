---
phase: 1
slug: infra-db-knowledge
status: validated
nyquist_compliant: true
wave_0_complete: true
created: 2026-05-29
updated: 2026-05-30
audited: 2026-05-30
mutation_score: 0.828
---

# Phase 1 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> See `01-RESEARCH.md` §Validation Architecture for the authoritative dimensions × requirements matrix (8 dimensions × 2 requirements = 16 dimension rows, 17 test commands, 9 Wave 0 gaps). This file is the per-task projection consumed by execute-phase.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | `go test` (Go 1.25+; actual `go.mod` is `go 1.26.0`) |
| **Config file** | none (build tags `db_integration` + `neo4j_integration`; `goleak.VerifyTestMain(m)` in `TestMain`) |
| **Quick run command** | `go vet ./... && go build ./... && go test ./internal/...` |
| **Full suite command** | `go test -race -tags 'db_integration neo4j_integration' ./internal/... && make smoke` |
| **Estimated runtime** | ~120 seconds (vet+build+unit ~30s; race+integration+smoke ~90s with containers warm) |

---

## Sampling Rate

- **After every task commit:** Run `go vet ./... && go build ./... && go test ./internal/<touched_pkg>/`
- **After every plan wave:** Run full integration suite for the slice (`db_integration` for Slice 0.5, `neo4j_integration` + `make smoke` for Slice 0.7)
- **Before `/gsd:verify-work`:** Full suite must be green; `make smoke` reports recall@5 = 5/5 and p95 ≤ 30ms
- **Max feedback latency:** 30 seconds (per-package unit + race)

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 02/T1 — config scaffolding + compose + Makefile + sqlc.yaml + .env.example + PRD D-02 amendment | 02 | 1 | INFRA-01 | T-1.05-04 (.env placeholder safety), T-1.05-09 implicit (PRD scope amendment) | Placeholders `changeme` only in `.env.example`; `.env` gitignored; compose fail-fast `${POSTGRES_PASSWORD:?}` | unit + structural | `go vet ./internal/config/... && go build ./... && go test ./internal/config/... -race -count=1` + `grep -c 'sandbox/compose.yaml' prd.md == 0` | ✅ files present | ✅ green |
| 02/T2 — Postgres pool + migrate + 4 SQL migrations + sqlc bindings + ping/status/reset + restore drill + cmd/aura/db.go + integration tests | 02 | 1 | INFRA-01 | T-1.05-01 (DSN redaction), T-1.05-02 (role separation), T-1.05-03 (advisory lock), T-1.05-06 (Go-side role bootstrap, no plaintext in error) | redactDSN helper masks password; aura_app lacks TRUNCATE/DROP/CREATE; D-07 literal error string verbatim; EnsureRoles uses parametrized queries | unit + integration + smoke (shell) | `go test ./internal/db/... -race` + `go test -tags db_integration -race ./internal/db/...` + `bash scripts/restore_drill.sh` + `make sqlc && git diff --exit-code internal/db/sqlc/` | ✅ files present | ✅ green |
| 03/T1 — compose + Makefile + .env extensions + config composite + knowledge.Config + Italian fixture corpus + queries.txt | 03 | 2 | INFRA-02 | T-1.07-02 (sidecar loopback), T-1.07-03 (Neo4j loopback), T-1.07-09 (Neo4j password fail-fast) | All 3 services use `127.0.0.1` host binding; `${NEO4J_PASSWORD:?}` interpolation; healthcheck `cypher-shell ... 'RETURN 1'` (Pitfall #3) | unit + structural | `go vet ./... && go test ./internal/config/... ./internal/knowledge/... -race` + loopback grep gate `grep -E '^\s+-\s+"[^"]+:[0-9]+:[0-9]+"' compose.yaml \| grep -cv '127.0.0.1' == 0` | ✅ files present | ✅ green |
| 03/T2 — MCP client + Cypher migration runner + 0001_init.cypher + ping/status/reset + integration tests + cmd/aura/neo4j.go + smoke harness | 03 | 2 | INFRA-02 | T-1.07-01 (Cypher injection via params), T-1.07-04 (stderr redaction), T-1.07-05 (dim self-test), T-1.07-07 (D-06 fail-fast), T-1.07-08 (healthcheck race retry) | All Cypher uses params map (never string concat); redactNeo4jSecrets masks password in errors; literal Pattern 5 dim-mismatch error; D-06 literal error on MCP crash; retry loop honors `cfg.ConnectTimeoutSec` | unit + integration + smoke (shell) | `go test ./internal/knowledge/... -race` + `go test -tags 'db_integration neo4j_integration' -race ./internal/knowledge/...` + `make smoke` (recall@5 = 5/5, p95 ≤ 30ms) | ✅ files present | ✅ green |
| 03/T3 — License legitimacy gate (blocking-human checkpoint) | 03 | 2 | INFRA-02 | T-1.07-SC (mcp-neo4j-cypher supply-chain) | gh api fetch + LICENSE first line `Apache License` (or MIT / BSD-3-Clause) | manual (blocking-human) | `gh api repos/neo4j-contrib/mcp-neo4j/contents/LICENSE \| jq -r '.content' \| base64 -d \| head -10` + operator approval | ✅ MIT in commit body | ✅ approved |
| 03/T4 — PRD amendment (acceptance row 182 → Pattern 5) + ROADMAP amendment (SC#4 `knowledge`→`neo4j`) + slice atomic commit | 03 | 2 | INFRA-02 | n/a (documentation) | All literal corrections applied; D-02 carry-forward verified; license evidence + Wave 0 probe in commit body | structural | `grep -c '/v1/embeddings round-trip returns 768d' prd.md >= 1 && grep -c 'aura knowledge ping' .planning/ROADMAP.md == 0 && grep -c 'aura neo4j ping' .planning/ROADMAP.md >= 1 && git log -1 --format='%B' \| grep -qiE '(Apache\|MIT\|BSD-3)'` | ✅ | ✅ green |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## ROADMAP Success Criteria → Task Map

| ROADMAP SC | Description | Closing Task(s) | Test Type | Automated Command |
|-----------|-------------|----------------|-----------|-------------------|
| Phase 1 SC#1 | `aura db migrate` is idempotent | 02/T2 | integration | `go test -tags db_integration -run TestMigrate_Idempotent ./internal/db -race` + CLI re-run smoke |
| Phase 1 SC#2 | `aura db migrate` as `aura_app` returns permission denied | 02/T2 | integration | `go test -tags db_integration -run TestRoleSeparation_AppDenied ./internal/db -race` |
| Phase 1 SC#3 | restore drill < 90s | 02/T2 | smoke (shell) | `bash scripts/restore_drill.sh` (exit 0, ELAPSED_MS < 90000) |
| Phase 1 SC#4 (per D-05 + Pattern 5 amendment) | `aura neo4j ping` returns Neo4j 5.26.x + sidecar dim 768 via /v1/embeddings round-trip | 03/T2, 03/T4 | integration | `go test -tags neo4j_integration -run TestPing_ReturnsServerVersion ./internal/knowledge -race` + `go test -tags neo4j_integration -run TestPingEmbed_Live ./internal/knowledge -race` |
| Phase 1 SC#5 | smoke recall@5 = 5/5, p95 ≤ 30ms on Italian corpus | 03/T2 | smoke (shell) | `make smoke` (exit 0, stdout contains `recall@5 = 5/5` and p95 ≤ 30) |

---

## Wave 0 Requirements

Wave 0 gaps surfaced by `01-RESEARCH.md` §Validation Architecture (9 items) — every gap is now mapped to a Task in 02-PLAN.md or 03-PLAN.md:

- [x] `compose.yaml` + `Makefile` + `.env.example` materialized so `make db-up neo4j-up` can run before any test container fixture → **02/T1** (postgres scaffolding) + **03/T1** (neo4j + sidecar extension)
- [x] `sqlc.yaml` + `make sqlc` target so `internal/db/sqlc/` codegen is reproducible before unit tests reference it → **02/T1** (sqlc.yaml + Makefile) + **02/T2** (sqlc generate + commit bindings)
- [x] `internal/db/migrations/0001_init.up.sql` + `0001_init.down.sql` + `0002_knowledge_migrations.up.sql` + `0002_knowledge_migrations.down.sql` present so `db_integration` tests have something to apply → **02/T2**
- [x] `internal/knowledge/migrations/0001_init.cypher` present so `neo4j_integration` smoke can apply constraint + HNSW + fulltext indices → **03/T2**
- [x] `internal/db/db_test.go` test harness with `goleak.VerifyTestMain(m)` in `TestMain` and ephemeral container helper → **02/T2**
- [x] `internal/knowledge/client_test.go` test harness with `goleak.VerifyTestMain(m)` in `TestMain` and ephemeral Neo4j + sidecar fixture → **03/T2** (plus the B1 split: `internal/knowledge/client_unit_test.go` carries the unit-safe TestMain without a build tag — see 03-PLAN.md Task 2 step 8)
- [x] `scripts/restore_drill.sh` (Slice 0.5 commit) callable from CI with `< 90s` assertion → **02/T2**
- [x] `scripts/neo4j_smoke.sh` + `scripts/fixtures/neo4j-smoke/*.md` Italian corpus + companion seed Cypher (Slice 0.7 commit) → **03/T1** (fixture corpus) + **03/T2** (smoke harness)
- [x] Role-separation assertion harness: `aura_app` attempting `aura db migrate` exits non-zero with permission denied — the only place ROADMAP SC#2 is verifiable → **02/T2** (TestRoleSeparation_AppDenied)

All 9 Wave 0 gaps are covered by the per-task verification map above.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `mcp-neo4j-cypher` license is Apache 2.0 (per CONTEXT.md claim) | INFRA-02 | Upstream PyPI license metadata field is empty; needs in-tree LICENSE check via `gh api repos/neo4j-contrib/mcp-neo4j/contents/LICENSE` | Blocking-human checkpoint in **03/T3**. Run `gh api repos/neo4j-contrib/mcp-neo4j/contents/LICENSE \| jq -r '.content' \| base64 -d \| head -1` and confirm `Apache License` (or MIT / BSD-3-Clause). Capture output into Slice 0.7 commit body as evidence. Resume signal: `approved: license verified <X>` or `halt: license is <X> — escalating to PRD amendment`. |
| MSYS path-mangling regression in `docker compose run` (Git Bash on Windows) | INFRA-01, INFRA-02 | Operator-environment specific; cannot reproduce in CI Linux | Run the same `make db-up` + `make neo4j-up` flow from Git Bash and from PowerShell on a Windows host; confirm PowerShell works as-is and Git Bash requires `MSYS_NO_PATHCONV=1` or PowerShell wrapper. Document in `docs/` runbook (informational; not blocking). |
| Cold-rebuild RAM headroom on 16-GB mini-PC | INFRA-01, INFRA-02 | Host-resource budget check; CI is not RAM-constrained the same way | On a fresh `docker compose down -v` followed by `make db-up neo4j-up`, observe `docker stats` post-warmup and confirm total resident set (postgres + neo4j + aura-llama-embed) stays within the PRD §Slice 0.5 RAM budget (~2.5 GB headroom on 16 GB shared with user's IDE). Informational; not blocking. |
| Wave 0 MCP JSON-RPC envelope probe (RESEARCH Assumption A10) | INFRA-02 | The `tools/call` response shape was NOT exercised by hands-on probe during research | **03/T2 step 1** — spawn `mcp-neo4j-cypher --transport stdio` against running Neo4j; send `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_neo4j_cypher","arguments":{"query":"RETURN 1 AS one","params":{}}}}`; capture response; align Go decoder if envelope differs. Document captured response in Slice 0.7 commit body. |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (every row in per-task table either has an `Automated Command` column populated or names a Wave 0 dependency closed by another task)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify (every task has at least 1 automated assertion in `<verify>`)
- [x] Wave 0 covers all MISSING references (the 9 items above; all marked [x])
- [x] No watch-mode flags (Go tests are one-shot; smoke scripts are one-shot)
- [x] Feedback latency < 30s for per-task quick command (unit tests + `go vet` + `go build` measured well below 30s on this codebase)
- [x] `nyquist_compliant: true` set in frontmatter — per-task verification map populated from 02-PLAN.md + 03-PLAN.md; coverage closes the 5 ROADMAP Success Criteria + INFRA-01 + INFRA-02; auditor confirmation complete (see Validation Audit 2026-05-29)

**Approval:** planner — 2026-05-29 · auditor (validate-phase) — 2026-05-29

---

## Validation Audit 2026-05-29

Retroactive Nyquist audit (`/gsd:validate-phase 1`). Ground-truth probe, not claim-trust: every test function named in the per-task map was located on disk; the unit gate was executed live; tagged integration + smoke files were compile-checked for API drift.

| Metric | Count |
|--------|-------|
| Requirements audited | 2 (INFRA-01, INFRA-02) |
| Gaps found | 0 |
| Resolved | 0 |
| Escalated | 0 |

**Evidence captured:**
- `go vet ./...` exit 0 · `go build ./...` exit 0
- Unit gate green: `internal/config` 97.1% · `internal/db` 47.8% (unit-only; success paths covered by container-gated `db_integration` suite) · `internal/knowledge` 73.4% (unit-only; 94.1% with integration per 03-04-SUMMARY)
- Tagged suites compile clean (no drift): `db_integration` ✓ · `neo4j_integration db_integration` ✓ · `smoke` ✓
- All 6 task rows' `Automated Command` references resolve to existing test functions; actual suite (60+ test funcs) exceeds the map.
- Manual-Only items (license, MSYS regression, RAM headroom, MCP envelope probe) all closed during execution per 01-02-SUMMARY + 03-04-SUMMARY.

**Verdict: NYQUIST-COMPLIANT.** No test generation required.

---

## Validation Audit 2026-05-30 (deep — Phase-2 parity)

Re-run at the same depth applied to Phase 2: the 2026-05-29 audit only executed the **unit** gate and **compile-checked** the tagged tiers. This pass **executed** every tier live against the healthy container stack (`aura-postgres` / `aura-neo4j` / `aura-llama-embed`), measured real combined coverage, and ran a mutation spot-check on the security-critical file.

**Live execution evidence (not compile-check):**

| Gate | Command | Result |
|------|---------|--------|
| vet + build | `go vet ./... && go build ./...` | exit 0 |
| `internal/config` | unit | **97.1%** |
| `internal/db` | `-race -tags db_integration` | **90.0%**, 2.2–2.6 s (real run, not skip-tell) |
| `internal/knowledge` | `-race -tags 'db_integration neo4j_integration'` | **94.1%**, 11–17 s |
| SC#1 idempotent migrate | `-run TestMigrate_Idempotent` | PASS |
| SC#2 role separation denied | `-run TestRoleSeparation_AppDenied` | PASS |
| SC#3 restore drill | `bash scripts/restore_drill.sh` | 321–362 ms < 90 s |
| SC#4 neo4j ping + embed dim | `-run TestPing_ReturnsServerVersion\|TestPingEmbed_Live` | PASS |
| SC#5 smoke | `make smoke` / `TestKnowledgeSmoke` | **recall@5 = 5/5, p95 = 1 ms** |
| lint (all tags) / file-size | `golangci-lint` / `check-file-size.sh` | 0 issues / clean |
| Mutation (db.go, `GOFLAGS=-tags=db_integration`) | `go-mutesting ./internal/db/db.go` | **82.8% killed (24/29)** after hardening |

> Note on p95: a first smoke run under heavy host contention (a parallel mutation campaign) measured 111 ms and failed the 30 ms gate. On a quiet host it is 1 ms. The gate is correctness-strict (recall@5) and latency-tunable via `AURA_SMOKE_P95_MS` by design — the 111 ms was contention, not regression.

**3 findings surfaced and FIXED (the shallow audit missed all three):**

| # | Sev | Finding | Fix |
|---|-----|---------|-----|
| A | Med | `pingEmbed` (`internal/knowledge/ping.go`) leaked an HTTP idle keep-alive goroutine (`&http.Client{}` + `Do`, no `CloseIdleConnections`) — made `goleak.VerifyTestMain` order-dependent (full suite passed, short subset failed). | Added `defer client.CloseIdleConnections()`. Subset now green. |
| B | Low | `db.go` mutation score was **44.8%** (<70% PRD floor): `Open`'s pool-tuning assignments + `>0` boundaries executed (90% line cov) but their effect was never asserted (`TestOpen_AppliesExplicitPoolTuning` skipped on the expected connect failure). | Added `TestOpen_PoolTuning_AppliedAndDefaulted` (db_integration) asserting `pool.Config()` for both explicit and defaulted branches → score **82.8%**. |
| C | Low | `restore_drill.sh` used bash `(( ))`; under non-bash shells (busybox/dash) it parses as nested subshells and `> 90000` silently created a junk file named `90000`. | Switched line 56 to POSIX `[ "$ELAPSED_MS" -gt 90000 ]`. |

**Mutation residue:** the 5 remaining db.go survivors are equivalent or cosmetic — `if err != nil || false` (identity), `if idx <= 0` vs `< 0` (idx is never 0 for a parsed scheme — equivalent), and two error-message-text mutants (non-security). The security spine (password redaction, role separation) is fully killed. Chasing equivalent mutants is coverage-theater.

| Metric | Count |
|--------|-------|
| Tiers executed live | unit + db_integration + neo4j_integration + smoke + restore drill |
| Combined coverage | config 97.1% · db 90.0% · knowledge 94.1% (all ≥85%) |
| Stale `-run` patterns | 0 (all referenced tests resolve) |
| Findings fixed | 3 (A impl leak, B test-hardening, C script portability) |
| Mutation (db.go) | 44.8% → **82.8%** killed |

**Verdict: NYQUIST-COMPLIANT (deep).** All 5 ROADMAP SCs + INFRA-01/02 verified by live execution; 3 quality findings fixed; mutation spot-check ≥70%.
