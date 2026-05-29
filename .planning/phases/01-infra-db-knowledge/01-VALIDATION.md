---
phase: 1
slug: infra-db-knowledge
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-29
---

# Phase 1 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> See `01-RESEARCH.md` §Validation Architecture for the authoritative dimensions × requirements matrix (8 dimensions × 2 requirements = 16 dimension rows, 17 test commands, 9 Wave 0 gaps). This file is the per-task projection consumed by execute-phase.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | `go test` (Go 1.25+) |
| **Config file** | none (build tags `db_integration` + `neo4j_integration`; `goleak.VerifyNone` in `TestMain`) |
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
| _to be filled by planner from PLAN.md task list_ | | | | | | | | | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Wave 0 gaps surfaced by `01-RESEARCH.md` §Validation Architecture (9 items, paraphrased):

- [ ] `compose.yaml` + `Makefile` + `.env.example` materialized so `make db-up neo4j-up` can run before any test container fixture
- [ ] `sqlc.yaml` + `make sqlc` target so `internal/db/sqlc/` codegen is reproducible before unit tests reference it
- [ ] `internal/db/migrations/0001_init.up.sql` + `0001_init.down.sql` + `0002_knowledge_migrations.up.sql` + `0002_knowledge_migrations.down.sql` present so `db_integration` tests have something to apply
- [ ] `internal/knowledge/migrations/0001_init.cypher` present so `neo4j_integration` smoke can apply constraint + HNSW + fulltext indices
- [ ] `internal/db/db_integration_test.go` test harness with `goleak.VerifyNone(t)` in `TestMain` and ephemeral container helper
- [ ] `internal/knowledge/neo4j_integration_test.go` test harness with `goleak.VerifyNone(t)` in `TestMain` and ephemeral Neo4j + sidecar fixture
- [ ] `scripts/restore_drill.sh` (Slice 0.5 commit) callable from CI with `< 90s` assertion
- [ ] `scripts/neo4j_smoke.sh` + `scripts/fixtures/neo4j-smoke/*.md` Italian corpus + companion seed Cypher (Slice 0.7 commit)
- [ ] Role-separation assertion harness: `aura_app` attempting `aura db migrate` exits non-zero with permission denied — the only place ROADMAP SC#2 is verifiable

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `mcp-neo4j-cypher` license is Apache 2.0 (per CONTEXT.md claim) | INFRA-02 | Upstream PyPI license metadata field is empty; needs in-tree LICENSE check via `gh api repos/neo4j-contrib/mcp-neo4j/contents/LICENSE` | Run `gh api repos/neo4j-contrib/mcp-neo4j/contents/LICENSE \| jq -r '.content' \| base64 -d \| head -1` and confirm `Apache License` literal. Capture output into Slice 0.7 commit body as evidence. |
| MSYS path-mangling regression in `docker compose run` (Git Bash on Windows) | INFRA-01, INFRA-02 | Operator-environment specific; cannot reproduce in CI Linux | Run the same `make db-up` + `make neo4j-up` flow from Git Bash and from PowerShell on a Windows host; confirm PowerShell works as-is and Git Bash requires `MSYS_NO_PATHCONV=1` or PowerShell wrapper. Document in `docs/` runbook. |
| Cold-rebuild RAM headroom on 16-GB mini-PC | INFRA-01, INFRA-02 | Host-resource budget check; CI is not RAM-constrained the same way | On a fresh `docker compose down -v` followed by `make db-up neo4j-up`, observe `docker stats` post-warmup and confirm total resident set (postgres + neo4j + aura-llama-embed) stays within the PRD §Slice 0.5 RAM budget (~2.5 GB headroom on 16 GB shared with user's IDE). |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (the 9 items above)
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s for per-task quick command
- [ ] `nyquist_compliant: true` set in frontmatter once the planner fills the task map and the auditor confirms coverage of the 5 ROADMAP Success Criteria + INFRA-01 + INFRA-02

**Approval:** pending
