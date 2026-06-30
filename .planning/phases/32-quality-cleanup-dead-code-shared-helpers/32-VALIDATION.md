---
phase: 32
slug: quality-cleanup-dead-code-shared-helpers
status: verified
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-29
validated: 2026-06-30
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

> Filled post-execution (2026-06-30). Each requirement maps to an automated test that exists on
> disk and passes under the race detector (see `32-VERIFICATION.md` Behavioral Spot-Checks and the
> owned-surface coverage 90.3%, every owned package ≥85%). The three rows that are inherently
> manual (operator sign-off / verify-don't-add) are listed in **Manual-Only Verifications** below,
> all resolved.

| Plan | Requirement | Threat Ref | Behavior | Test Type | Automated Command / File | Status |
|------|-------------|------------|----------|-----------|--------------------------|--------|
| 32-01 | QUAL-02 | T-32-01-A1 | dead-constant triage (assets.Status kept-annotated) | build/deadcode | `go build ./...` + `deadcode -test` (assets keep is Manual-Only) | ✅ green |
| 32-02 | QUAL-02 | T-32-02-AL | agui `indexByte`/`stringList` removed; NetworkAllowlist marshals `[]` not `null` | unit | `go test -race ./internal/agui/` → `TestGovernanceMCPEmptyAllowlistIsArrayNotNull` | ✅ green |
| 32-03 | QUAL-02 | T-32-03-PIN/REQ | telebot pin stays DIRECT; LLM Build branch-parity | unit | `go mod tidy` grep + `go test -race ./internal/agent/` branch-parity | ✅ green |
| 32-04 | QUAL-02 | T-32-04-SEC/CFG | `AURA_MEMORY_EMBED_*` removed from `settings.AllowedKeys` | unit | `go test -race ./internal/settings/` | ✅ green |
| 32-05 | QUAL-03 | T-32-05-PAR/NUM/CYC | `neostore`/`pgnumeric`/`envutil` extraction parity | unit | `go test -race ./internal/{neostore,pgnumeric,envutil}/` (parity tables) | ✅ green |
| 32-06 | QUAL-03 | T-32-06-STR/TOOL | `canonicaljson.CanonicalArgs`; `isTransientNetworkErr` extraction + widened tool retry | unit | `go test -race ./internal/canonicaljson/ ./internal/agent/` (`TestCanonicalArgs`, `TestIsTransientNetworkErr`, `TestIsTransientToolErr_WidenedNetworkSubset`) | ✅ green |
| 32-07 | QUAL-03 | T-32-07-BND/FIX/PAR | `agentrender` primitives; no agui back-edge; eval json.Number fix | unit | `go test -race ./internal/agentrender/` + `go list -deps` boundary | ✅ green |
| 32-08 | QUAL-03 | T-32-08-A11Y/VIS | `getJSON` dedup; canonical focusTrap; rich SkeletonBlock | unit + e2e | `cd web && npm test` (`McpLifecycleCluster`, `Skeleton`, `api/json`) + `npx playwright test e2e/phase32-uat.spec.ts` | ✅ green |
| 32-09 | QUAL-05 | T-32-09-DOS/RACE/DSN | throttle per-host; setup SSE ordering; Authula DSN parser | unit | `go test -race ./internal/web/ ./internal/setup/ ./internal/webauth/` (throttle goleak, `TestEventsInvalidateTokenBeforeSSEWrite`, `ensureAuthulaSearchPath`) | ✅ green |
| 32-10 | QUAL-05 | T-32-10-UTF | telegram Italian keyword fallback; `truncateTailBytes` UTF-8 | unit | `go test -race ./internal/channels/telegram/ ./internal/agent/` (`TestAnswersFromTextKeywordFallback`, `TestTruncateTailBytes`) | ✅ green |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

New packages / test files that must exist (parity test GREEN against the OLD duplicated code) before the corresponding extraction moves code — **all delivered and green**:

- [x] `internal/neostore/{neostore.go,neostore_test.go}` — QUAL-03 (`HashText`/`AsString`/`AsFloats`/`GraphClient`); parity test present
- [x] `internal/pgnumeric/{pgnumeric.go,pgnumeric_test.go}` — QUAL-03 (`NumericFromFloat`/`FloatFromNumeric`); parity present (leaf chosen over `internal/db` to avoid the `db↔cachemetrics` test cycle — see VERIFICATION deviation)
- [x] `internal/envutil/{envutil.go,envutil_test.go}` — QUAL-03 (`IntDefault`/`BoolDefault`); `t.Setenv` table
- [x] `internal/agentrender/{agentrender.go,agentrender_test.go}` — QUAL-03 (render primitives + eval `json.Number` fix)
- [x] `internal/web/throttle_test.go` — QUAL-05 (acquire/release/ctx-cancel/per-host/race) with `goleak`
- [x] Coverage-gate registration check (D-13): `neostore`/`pgnumeric`/`envutil`/`agentrender` under `internal/` are auto-included; owned-surface 90.3%, every owned package ≥85% (CLAUDE.md)
- [x] Extend existing tests (no new file): `internal/agent` (`CanonicalArgs`, transient classifiers, `truncateTailBytes`), `internal/agui` (`indexByte`/`stringList` removed; allowlist marshal), `cmd/aura` (RequestID dry-run), `internal/setup`, `internal/channels/telegram`, `internal/webauth`, `internal/settings`, web (`McpLifecycleCluster`, skeleton consumers, `api/json`, plus `web/e2e/phase32-uat.spec.ts`)

---

## Manual-Only Verifications

These need an operator decision or a doc/inspection verdict rather than an automated assertion (per CONTEXT.md D-04 escalation rules and RESEARCH.md triage):

| Behavior | Requirement | Why Manual | Verdict (resolved 2026-06-30) |
|----------|-------------|------------|-------------------------------|
| `assets.Status{Created,Embedding,Canceled}` keep-vs-delete | QUAL-02 | Intended-but-unwired designed lifecycle — D-04 exported-symbol sign-off | ✅ **keep + annotate** (operator). Designed 12-state pipeline; annotation at `internal/assets/types.go:14-22`; re-ran `deadcode`+`knip`+repo-wide `rg` first (VERIFICATION SC-1e) |
| `memory_integration` CI leg runs (no-skip-as-green) | QUAL-05 (D-12 / QA-A-09) | Verdict is "already live" — verify, don't add | ✅ **already live** — `ci.yml:606-719` sets `CI:"true"`, runs `-tags memory_integration`, `t.Fatal` under `$CI` (`memory_recall_integration_test.go:52`). No redundant leg added (VERIFICATION SC-3f) |
| Authula `ensureAuthulaSearchPath` DSN test placement | QUAL-05 | Source conflict: ROADMAP/REQUIREMENTS say IN-32, audit §E says Phase 34 | ✅ **kept in 32** — test-only, function exists at `authula.go`; `authula_test.go` 4-case table (VERIFICATION SC-3c) |

---

## Validation Audit 2026-06-30

| Metric | Count |
|--------|-------|
| Requirements audited | 3 (QUAL-02, QUAL-03, QUAL-05) across 10 plans |
| COVERED (automated) | 10/10 plan rows |
| Gaps found (MISSING/PARTIAL) | 0 |
| Manual-only (resolved) | 3 |

State-A audit: no automatable gaps found, so the `gsd-nyquist-auditor` was not spawned (workflow Step 3 "no gaps → Step 6"). Every requirement maps to an on-disk automated test verified present (see Per-Task Map) and passing under race per `32-VERIFICATION.md`; owned-surface coverage 90.3% (each owned package ≥85%). The focusTrap + skeleton rows additionally carry a live Playwright spec (`web/e2e/phase32-uat.spec.ts`, 4/4 green).

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 15s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** verified 2026-06-30
