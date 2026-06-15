---
phase: 15
slug: memory-subsystem
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-11
---

# Phase 15 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Phase 15 is an ADOPTION, not a build. The vendored `agent-memory` package owns the memory
> engine (trusted via its 2443 tests + TCK) and is NOT measured under the 85% coverage floor.
> All validation below targets the OWNED Go wiring + the two doc/compose changes.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` (stdlib) + table tests; one build tag per live tier |
| **Config file** | none (Go convention); CI matrix env per CLAUDE.md (composed DSNs, sidecar URL, stack up) |
| **New build tag** | `memory_integration` (added to the per-tier family — NOT overloaded onto db/neo4j) |
| **Quick run command** | `go test ./internal/mcp/manager/ ./internal/config/ ./cmd/aura/` |
| **Full suite command** | `go test -tags 'db_integration neo4j_integration memory_integration' ./...` (stack up, `AURA_AGENT_MEMORY_MCP_URL=http://127.0.0.1:8091/mcp/`) + `scripts/cache_invariant_audit.sh` |
| **Estimated runtime** | unit ~20-30s; full tier (stack up) ~2-4 min |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/mcp/manager/ ./internal/config/ ./cmd/aura/` (unit, sub-30s)
- **After every plan wave:** Run `go test -tags 'db_integration neo4j_integration memory_integration' ./...` (stack up) + `scripts/cache_invariant_audit.sh`
- **Before `/gsd-verify-work`:** Full tagged matrix green + owned-surface coverage ≥85% (Go wiring only) + advisory snapshot appended + `cache_invariant_audit.sh` green
- **Max feedback latency:** 30 s (unit tier)

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 15-01-01 | 01 | 0 | UX-06/07/08/09 | T-15-01-01 | re-scope copied verbatim from D-12; no runtime surface | doc gate | `grep -q "Amendment #62" prd.md && grep -q "AURA_AGENT_MEMORY_MCP_PORT" prd.md` | ✅ | ⬜ pending |
| 15-01-02 | 01 | 0 | UX-06/07/08/09 | T-15-01-01 | requirement checkboxes preserved, not silently dropped | doc gate | `grep -q "amendment #62" .planning/REQUIREMENTS.md` | ✅ | ⬜ pending |
| 15-02-01 | 02 | 1 | UX-09 | T-15-02-01 | recipe exposes package tools as-is; no write-Cypher tool added | unit | `go test ./internal/mcp/manager/ ./cmd/aura/ -run 'Catalog|Recipes|Memory'` | ❌ W0 (extend mcp_test.go) | ⬜ pending |
| 15-02-02 | 02 | 1 | UX-09 | T-15-02-04 | default-on is trusted-only + respects `aura mcp disable`; Deferred-namespaced | unit | `go test -race ./internal/config/ -run MemoryDefaultOn` | ❌ W0 (new config_mcp_default_on_test.go) | ⬜ pending |
| 15-03-01 | 03 | 1 | UX-08 | T-15-03-01 | only read-only `graph_query` exposed as a CLI verb; no write-Cypher | build+vet | `go build ./cmd/aura/ && go vet ./cmd/aura/ && go test ./cmd/aura/ -run Memory` | ❌ W0 (new memory.go) | ⬜ pending |
| 15-03-02 | 03 | 1 | UX-08 | T-15-03-03 | per-call 20s timeout; RAW wire names; no agent-loop bypass risk | unit | `go test -race ./cmd/aura/ -run MemoryVerbMapping` | ❌ W0 (new memory_test.go) | ⬜ pending |
| 15-04-01 | 04 | 1 | UX-08 | T-15-04-SC | image installs from vendored fork pinned at c1c2d65, NOT PyPI | build smoke | `test -f docker/agent-memory/Dockerfile && grep -q c1c2d65 docker/agent-memory/Dockerfile` | ❌ W0 (new Dockerfile) | ⬜ pending |
| 15-04-02 | 04 | 1 | UX-08 | T-15-04-02 | no secret baked; runtime env only; compose parses | config gate | `grep -q "context: ./docker/agent-memory" compose.yaml && docker compose config >/dev/null` | ✅ (compose.yaml exists) | ⬜ pending |
| 15-05-01 | 05 | 2 | UX-06/09 | T-15-05-01 | 16-tool Deferred + memory__*; no DenyRisk; dedup action=none | integration (live) | `go test -tags memory_integration -run 'MemoryLiveMount\|MemoryCLI\|MemoryReasoningTrace\|MemoryDedup' ./internal/agent/mcptools/ ./cmd/aura/` | ❌ W0 (new memory_integration_test.go) | ⬜ pending |
| 15-05-02 | 05 | 2 | UX-08/09 | T-15-05-04 | recall path live; KV-cache prefix unchanged (no messages[2]); advisory snapshot | integration + audit | `go test -tags memory_integration -run MemoryLoopRecall ./internal/agent/ && bash scripts/cache_invariant_audit.sh && grep -qi memory docs/aura-quality-snapshot.md` | ❌ W0 (new memory_recall_integration_test.go) | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

**Sampling continuity check:** No 3 consecutive tasks lack an automated verify — every task above
has an `<automated>` command. The two doc-gate tasks (15-01-01/02) use grep-ordering gates; all
Go tasks have `go test`/`go build`/`go vet` commands; the live tier tasks have
`-tags memory_integration` commands that `t.Fatal` under `$CI` when the sidecar URL is unset.

---

## Wave 0 Requirements

New test files this phase creates (all Go stdlib testing — no framework install needed):

- [ ] `cmd/aura/mcp_test.go` — EXTEND with the `memory` recipe golden + default-on assertion (15-02-01)
- [ ] `internal/config/config_mcp_default_on_test.go` — NEW: `TestMemoryDefaultOn` + disable + explicit-install (15-02-02)
- [ ] `cmd/aura/memory_test.go` — NEW: `TestMemoryVerbMapping` over a fake streamable-HTTP transport (15-03-02)
- [ ] `internal/agent/mcptools/memory_integration_test.go` — NEW (`//go:build memory_integration`): `TestMemoryLiveMount` (15-05-01)
- [ ] `cmd/aura/memory_integration_test.go` — NEW (`//go:build memory_integration`): `TestMemoryCLI`, `TestMemoryReasoningTrace`, `TestMemoryDedupNewEntityActionNone` (15-05-01)
- [ ] `internal/agent/memory_recall_integration_test.go` — NEW (`//go:build memory_integration`): `TestMemoryLoopRecall` (15-05-02)
- [ ] `memory_integration` build tag wired into `.github/workflows/ci.yml` with `AURA_AGENT_MEMORY_MCP_URL` exported + stack up (`t.Fatal` under `$CI` when unset — no-skip-as-green) (15-05-01)

*Framework: existing Go stdlib testing is in place. `scripts/cache_invariant_audit.sh` already exists — run, assert unchanged.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| PRD amendment #62 commit ordering | UX-06..09 / D-12 | Git history ordering is a human/CI log inspection, not a unit test | `git log --oneline` — assert the amendment-#62 doc commit precedes every Go-file commit in the phase (PRD-first gate) |
| Reproducible image rebuild from git | UX-08 | Requires Docker + the live stack (run in WSL/CI Linux, ~minutes) | `docker compose build aura-agent-memory-mcp` succeeds; then the `memory_integration` tier (Task 15-05) asserts the rebuilt image's live `tools/list` == 16 + dedup action=none |
| Advisory recall@5 / p95 snapshot values | UX-08 (re-scoped) | Advisory measurement against the package's retrieval over a seeded set; NOT a hard pass/fail gate (amendment #20 only requires the file be updated) | Seed a small set via `aura memory add-*`, measure recall@5/p95 via `aura memory search`, append numbers + method + date to `docs/aura-quality-snapshot.md` |

*All other phase behaviors have automated verification (unit + the live `memory_integration` tier).*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies / Manual-Only justification
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (the new test files above)
- [x] No watch-mode flags
- [x] Feedback latency < 30s (unit tier)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-06-11
