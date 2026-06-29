---
phase: 32
slug: quality-cleanup-dead-code-shared-helpers
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-29
---

# Phase 32 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Source of truth for the parity/characterization tables: `32-RESEARCH.md` § Validation Architecture (D-09/D-10).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go: stdlib `testing` + table tests + `go.uber.org/goleak` (`goleak.VerifyNone` in `TestMain` for packages with goroutines, e.g. `internal/web/throttle`) + race detector. Web: Vitest + Stryker + Playwright (skeleton change only). |
| **Config file** | `go.mod` (Go) / `web/package.json` + `web/vitest.config.ts` (web) — no new framework install |
| **Quick run command** | `go test -race ./internal/<touched-pkg>/` (Go) · `cd web && npm test` (web) |
| **Full suite command** | `bash scripts/coverage_gate.sh` (owned-surface ≥85%, each new pkg ≥85%) + `cd web && npm run lint && npm test` + Stryker |
| **Estimated runtime** | quick ~2–15s/pkg; full coverage gate ~several min (live PG/Neo4j/embed/memory stack, `-p 1`) |

---

## Sampling Rate

- **After every task commit:** Run `go test -race ./internal/<touched-pkg>/` (and `cd web && npm test` for web tasks)
- **After every plan wave:** Run `go vet $(bash scripts/go_packages.sh)` + `go build $(bash scripts/go_packages.sh)` + `golangci-lint run $(bash scripts/go_packages.sh)` + `deadcode -test $(bash scripts/go_packages.sh)`
- **Before `/gsd-verify-work`:** `bash scripts/coverage_gate.sh` (≥85% owned + each new package ≥85%) + `cd web && npm run lint && npm test` + Stryker + dist-freshness, all green
- **Max feedback latency:** ~15 seconds for the per-task quick run

---

## Per-Task Verification Map

> **To be filled by the planner / `gsd-nyquist-auditor` once plan task IDs are assigned.**
> Every plan task must map to a row here with an `<automated>` verify command. Until then, the
> authoritative requirement→test map is `32-RESEARCH.md` § "Phase Requirements → Test Map" and
> § "Parity-Test Strategy" (the union-of-inputs cases per extraction). Wave 0 rows (new packages /
> test files) are listed below.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 32-NN-NN | NN | N | QUAL-0X | — | N/A (behavior-preserving refactor) | unit | `go test -race ./internal/<pkg>/` | ⬜ TBD | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

New packages / test files that must exist (parity test GREEN against the OLD duplicated code) before the corresponding extraction moves code:

- [ ] `internal/neostore/{neostore.go,neostore_test.go}` — QUAL-03 (`HashText`/`AsString`/`AsFloats`/`GraphClient`); parity test drives ≥85%
- [ ] `internal/envutil/{envutil.go,envutil_test.go}` — QUAL-03 (`IntDefault`/`BoolDefault`); `t.Setenv` table → ≥85%
- [ ] `internal/agentrender/{agentrender.go,agentrender_test.go}` — QUAL-03 (render primitives + eval `json.Number` fix); ≥85%
- [ ] `internal/web/throttle_test.go` — QUAL-05 (acquire/release/ctx-cancel/per-host/race) with `goleak`
- [ ] Coverage-gate registration check (D-13): confirm `neostore`/`envutil`/`agentrender` are not caught by any `scripts/coverage_gate.sh` exclude pattern (under `internal/` → auto-included); verify each clears 85% with `go test -cover ./internal/<pkg>/`
- [ ] Extend existing tests (no new file): `internal/db` (numeric parity), `internal/agent` (`CanonicalArgs`, transient classifiers, `truncateTailBytes`), `internal/agui` (`indexByte`/`stringList`), `cmd/aura` (RequestID dry-run), `internal/setup`, `internal/channels/telegram`, `internal/webauth`, `internal/settings`, web (`useConversations`, `governanceApi`, `BoardLayout`, `McpLifecycleCluster`, skeleton consumers)

---

## Manual-Only Verifications

These need an operator decision or a doc/inspection verdict rather than an automated assertion (per CONTEXT.md D-04 escalation rules and RESEARCH.md triage):

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `assets.Status{Created,Embedding,Canceled}` keep-vs-delete | QUAL-02 | Intended-but-unwired designed lifecycle — D-04 exported-symbol sign-off | Operator confirms keep+annotate (recommended) or delete-the-3-named; executor re-runs `deadcode`+`knip`+repo-wide `rg` first |
| `memory_integration` CI leg runs (no-skip-as-green) | QUAL-05 (D-12 / QA-A-09) | Verdict is "already live" — verify, don't add | Inspect `ci.yml:606-719` + confirm `-tags memory_integration` + `t.Fatal` under `$CI`; document that the leg exists (no redundant leg added) |
| Authula `ensureAuthulaSearchPath` DSN test placement | QUAL-05 | Source conflict: ROADMAP/REQUIREMENTS say IN-32, audit §E says Phase 34 | Default keep IN 32 (test-only, function exists at `authula.go:292`); defer to 34 only if it needs cutover infra not yet present |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 15s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
