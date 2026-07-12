---
phase: 42
slug: llm-conversation-compaction
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-12
---

# Phase 42 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Test-tier mapping derived from `42-RESEARCH.md` `## Validation Architecture`. The
> daemon-free vs `db_integration` split is load-bearing for the ≥85% owned-surface
> floor (there is NO `docker_integration` CI job — see CLAUDE.md).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go: `go test` (table-driven + goleak + `-race`); Web: `vitest` + `@testing-library/react` |
| **Config file** | `Makefile`, `.golangci.yml`, `scripts/coverage_gate.sh`; web: `web/vitest.config.ts` |
| **Quick run command** | `go test ./internal/conversations/... ./internal/runner/... ./cmd/aura/... ./internal/config/... ./internal/agui/...` |
| **Full suite command** | `bash scripts/coverage_docker.sh` (full `db_integration neo4j_integration` matrix on the live stack) **+** `cd web && npm test` |
| **Estimated runtime** | Go unit ~30–90s; full matrix (stack up) ~10–20 min; web vitest ~15–40s |

---

## Sampling Rate

- **After every task commit:** Run the quick run command for the touched package(s) (add `-race` on any package touching the auto-compaction lifecycle / goroutine surface).
- **After every plan wave:** Run the full Go suite for touched packages under the `db_integration` tag (`GOFLAGS=-tags=db_integration`, stack up) + `cd web && npm test` for any wave touching `web/src`.
- **Before `/gsd-verify-work`:** `make quality` (vet + build + file-size + lint + `-race` + vuln) green; `bash scripts/coverage_docker.sh` ≥85%; web vitest green.
- **Max feedback latency:** ~90 seconds (per-package quick run).

---

## Per-Task Verification Map

> Rows are filled from the plans during execution (Wave 0 / nyquist auditor). Tier column
> is pre-committed from `42-RESEARCH.md` `## Validation Architecture`:
> **U** = daemon-free unit (counts toward the 85% floor), **DB** = `db_integration`,
> **FE** = web vitest.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 42-01-01 | 01 | 1 | COMPACT-01/02 | — | Empty-summary stub → error, no persistence | U (stub `llm.Client`) | `go test ./internal/conversations/ -run Compact` | ❌ W0 | ⬜ pending |
| 42-0X-XX | — | — | COMPACT-03/10 | T-42-01 | Atomic persist; injected DB failure rolls back both rows | DB (`db_integration`) | `go test -tags=db_integration ./internal/conversations/` | ❌ W0 | ⬜ pending |
| 42-0X-XX | — | — | COMPACT-04 | — | Reconstruct `[system, always-block, summary, turns>K]`; byte-equal x2; L2.5 skips summary | U (pure truncation fn) | `go test ./internal/conversations/ -run Reconstruct` | ❌ W0 | ⬜ pending |
| 42-0X-XX | — | — | COMPACT-08/11 | T-42-02 | Auto-fallback: 1 attempt, no loop; timeout → original err, goleak-clean | U (`fakeConvStore`) + goleak | `go test -race ./internal/runner/ -run AutoCompact` | ❌ W0 | ⬜ pending |
| 42-0X-XX | — | — | COMPACT-09 | — | `aura config validate` type-checks the 4 `AURA_COMPACT_*` knobs | U | `go test ./internal/config/ -run Knob` | ❌ W0 | ⬜ pending |
| 42-0X-XX | — | — | D-08/D-09 | T-42-03 | Web `/compact` POST + `GET /compactions`; gauge marker distinct from rot-events | U (Go handlers) + FE (vitest) | `go test ./internal/agui/` ; `cd web && npm test` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] Stub `llm.Client` returning a canned/empty summary for `CompactConversation` unit tests (reuse the `title.go` test stub pattern if present).
- [ ] `fakeConvStore` extended with an injectable `LoadManagedHistory` error (`ErrContextWindowExceeded`) + a `manErr` hook for the auto-fallback path (per RESEARCH — `scriptedRunner`/`fakeConvStore` pattern).
- [ ] `db_integration` fixture: seed a conversation with N turns for the atomic-persist + reconstruction + FTS-retention assertions (disposable `aura_cov` DB via `coverage_docker.sh`, never live `aura`).
- [ ] **New** `web/src/chat/ContextBudgetGauge.test.tsx` — the gauge has NO existing test (RESEARCH landmine); must be created, not extended.
- [ ] Extend `web/src/chat/composer/skillPickerModel.test.ts` for the `/compact` QuickCommand.

*Existing `go test` + `vitest` infrastructure covers the frameworks; no framework install needed.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `aura db migrate` applies `0036` cleanly; re-run no-op; `.down.sql` reverses; denied as `aura_app`, ok as `aura_migrate` | COMPACT-03 | Requires the live stack + role switch | Bring stack up; run migrate up as `aura_migrate`; re-run (no-op); run `.down`; attempt as `aura_app` (expect deny) |
| Mutation spot-check ≥70% killed on `compact.go` + the reconstruction change | Constraints | `go-mutesting` runs only in WSL (go1.26 fork) | WSL: `GOFLAGS=-tags=db_integration` + DSN env; `PASS`=killed |
| KV-cache stable-prefix test green on a compacted conversation | COMPACT-04 / Phase-6 invariant | Byte-stability of `messages[0]` verified against the Phase-6 cache test | Run the existing cache-stable-prefix test on a compacted-conversation fixture |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
