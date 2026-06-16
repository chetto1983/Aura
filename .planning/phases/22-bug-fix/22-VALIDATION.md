---
phase: 22
slug: bug-fix
status: validated
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-15
updated: 2026-06-16
evidence_head: 28c1f7c7
mutation_score: "budget_dedup 85.5% · bridge_reconnect 73.4% · llm_agent_parallel 73.7% (all ≥70%)"
coverage: "89.4% owned-surface (≥85%)"
---

# Phase 22 — Agent Perimeter Hardening: Validation Strategy

> Per-phase validation contract for the AG-001..064 hardening waves (22-01..22-05).
> The authoritative per-finding dispositions + named regression tests live in
> [`docs/audit/22-finding-ledger.md`](../../../docs/audit/22-finding-ledger.md).
> The full automated/live evidence is in
> [`docs/audit/22-LIVE-SIGNOFF-2026-06-15.md`](../../../docs/audit/22-LIVE-SIGNOFF-2026-06-15.md).
>
> This file splits the gate into **(A) AUTOMATED EVIDENCE — done** and **(B) OPERATOR
> SIGN-OFF — done** (the destructive coverage gate B1, the WSL quality bar B2, and the
> full live-stack pass B3, all signed off — B3 on 2026-06-16). No fabricated pass is
> recorded for anything not run.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | `go test` (`go 1.26.4 windows/amd64`); `goleak.VerifyTestMain(m)` in agent/workflow/swarm `TestMain` |
| **Build tags** | `db_integration`, `neo4j_integration` (integration tiers); untagged = unit, no DB |
| **Quick run** | `go vet ./... && go build ./... && go test ./internal/agent/... ./internal/swarm/...` |
| **Race tier** | `BASH_ENV=~/.aura-toolchain.sh go test -race ./internal/agent/... ./internal/swarm/...` (Windows binutils-shadow workaround) |
| **Cache gate** | `bash scripts/cache_invariant_audit.sh` (in-memory FakeClient, no PG) |
| **Coverage gate** | `make coverage` → `scripts/coverage_gate.sh` (owned-surface ≥85%; **destructive — wipes shared PG**) |
| **WSL quality** | `golangci-lint run ./...` · `govulncheck ./...` · `go-mutesting` (PATH = `~/.local/bin:~/go/bin`) |

---

## Part A — AUTOMATED EVIDENCE (executed 2026-06-15, HEAD `036575b5`) — DONE

| # | Command | Result | Evidence |
|---|---------|--------|----------|
| A1 | `go build ./...` | ✅ PASS (exit 0) | whole module builds |
| A2 | `go vet ./...` | ✅ PASS (exit 0) | no diagnostics |
| A3 | `go test ./...` (untagged) | ✅ all PASS (whole module, exit 0) | the former `cmd/aura` `TestProductionContainerArtifactsMatchFatImageContract` failure was the `:nitro`/`:exacto` compose drift — resolved 2026-06-15 (`e2b0d82a`) by de-hardcoding the test to assert the env-override PATTERN, not a tag (model stays 100% `.env`-configurable) |
| A4 | `go test -race ./internal/agent/... ./internal/swarm/...` | ✅ PASS (exit 0) | race-clean: agent, agenttest, mcptools, prompt, tools, workflow, swarm |
| A5 | `bash scripts/cache_invariant_audit.sh` | ✅ PASS (exit 0) | 22 identical `messages[0]`/`messages[1]`/skill-manifest hashes — KV prefix stable after the 22-05 skill-schema edit |

**Per-finding regression tests:** all named in the ledger; fail-before/pass-after shown
in the wave SUMMARYs (22-01..22-04). 22-05 added `TestSkillSchemaIsHonestNotDishonest`,
`TestMountManagedServer_HTTPBranchInfersFromBareURL`,
`TestMountManagedServer_StdioBranchFailure`,
`TestToolInvocation_ForensicShapeIsRawRedactionRoutedToStore` — all green in A3/A4.

---

## Part B — QUALITY BAR (B1/B2 executed 2026-06-15 on the live stack) + LIVE SIGN-OFF (B3 signed-off 2026-06-16)

> B1/B2 were run by the orchestrator against the live Docker stack (all services
> healthy) on the operator's go-ahead — B1 reset the shared Postgres by design.
> **B3 (full live-stack sign-off) was driven 2026-06-16** at HEAD `85b5e1ae` (9/9
> acceptance rows, each a ground-truth assertion not `r.Reply`) — see the live-signoff
> Part B3 table. Gate-3 is CLOSED.
> Exact commands + acceptance live in
> [`docs/audit/22-LIVE-SIGNOFF-2026-06-15.md`](../../../docs/audit/22-LIVE-SIGNOFF-2026-06-15.md)
> Parts B1–B3.

| # | Gate | Command (abridged) | Acceptance | Status |
|---|------|--------------------|------------|--------|
| B1 | Coverage ≥85% owned-surface | `make coverage` (stack up, **PG wipe**) | ≥85% combined across `db_integration neo4j_integration` | ✅ **89.4%** (exit 0) |
| B2a | Lint | `golangci-lint run ./...` | 0 issues | ✅ **0 issues** (whole module) |
| B2b | Vuln | `govulncheck ./...` | 0 actionable CVEs | ✅ **no vulnerabilities** |
| B2c | Mutation ≥70% killed | `go-mutesting` on `llm_agent_parallel.go`, `budget_dedup.go`, `mcptools/bridge_reconnect.go` | ≥70% killed each, or documented near-equivalent-survivor autopsy | ✅ **budget_dedup 85.5% · bridge_reconnect 73.4% · llm_agent_parallel 73.7%** |
| B3 | Full live stack | `aura serve` + acceptance matrix | per the live-signoff B3 table (metrics scrape, CDP Telegram, GLM-OCR fs-cap, MCP reconnect, reasoning fallback, skill self-extension, DSN boundary, ledger redaction) | ✅ **signed-off 2026-06-16** (HEAD `85b5e1ae`, 9/9 rows) |

### B2c mutation autopsy (post-hardening)

Initial spot-check found two files below 70% — resolved by adding targeted kill tests
(no production-code change, no test weakening):

- **`mcptools/bridge_reconnect.go` 58.5% → 73.4%** (+14 killed): `bridge_reconnect_mutation_test.go`
  pins the `reconnectBackoff` schedule, the `currentClient` nil-guard, the breaker
  elapsed/open gate, successful-reconnect state resets (client swap + failure reset),
  refresh-hook dispatch, and lock-release on the already-closed `Close` path.
- **`llm_agent_parallel.go` 47.4% → 73.7%** (+5 killed): `llm_agent_parallel_internal_test.go`
  pins `maxParallelTools` env-guard fallbacks (a 0 would deadlock the worker pool);
  `llm_agent_parallel_metrics_test.go` pins the panic-recovery observability (recovered-panic
  + per-tool error counters). The 5 remaining survivors are **provably equivalent** — the
  `len(calls)<=1` fast-path bypass and idle-worker `limit` reductions preserve behavior
  because the worker-pool path handles a single call correctly; advisory-accepted.

---

## Per-Wave → Finding → Requirement Map

| Wave | Findings closed | Requirements | Disposition |
|------|-----------------|--------------|-------------|
| 22-01 | AG-001, AG-002, AG-039, AG-040 | HARDEN-01, HARDEN-02 | fixed+test |
| 22-02 | AG-003(slice), AG-009, AG-010, AG-012, AG-013, AG-033, AG-047, AG-056, AG-057 | HARDEN-04, HARDEN-05 | fixed+test |
| 22-03 | AG-005, AG-006, AG-007(slice→1.7), AG-008, AG-022..027, AG-029, AG-032, AG-035, AG-036, AG-041(confirm) | HARDEN-03, HARDEN-06, HARDEN-09 | fixed+test / confirmed+routed |
| 22-04 | AG-003(slice), AG-004, AG-014..020, AG-030, AG-031, AG-037, AG-038, AG-043, AG-045, AG-046, AG-050, AG-052, AG-054, AG-058, AG-059..064 | HARDEN-07, HARDEN-08, HARDEN-09, HARDEN-10 | fixed+test / accepted+rationale |
| 22-05 | AG-011, AG-044, AG-051 (skill honesty); AG-028, AG-034, AG-041, AG-043 (confirm/route); ledger | HARDEN-11, HARDEN-12 | fixed+test / confirmed+routed |

**Accepted+rationale (no code, named rationale):** AG-021, AG-042, AG-048, AG-049,
AG-053, AG-055, AG-059, AG-060, AG-063.
**Confirmed+routed (fix outside internal/agent+swarm, D-09):** AG-007 (→Slice 1.7),
AG-034 (→`internal/toolinvocations`), AG-041 (→`cmd/aura`).

---

## Formerly out-of-scope — resolved 2026-06-15 (operator directive "fix all findings")

- ✅ `cmd/aura` `TestProductionContainerArtifactsMatchFatImageContract` — the compose
  `:nitro`/`:exacto` drift is fixed by de-hardcoding the test (`e2b0d82a`): it now
  asserts the env-override pattern `AURA_LLM_MODEL: ${AURA_LLM_MODEL:-…}` rather than a
  fixed tag, so the model stays fully `.env`-configurable. `config.go`/`prices.go`
  untouched. `go test ./...` is now whole-module green.
- ✅ `internal/skills` `deadcode` flags (`BM25Corpus`, `SnippetInvocation`,
  `ValidateNameAgainstDir`) — investigated: **not a defect.** Each is tested and
  intended (spike consumer / installer chokepoint / overflow-ranker backing); `deadcode`
  flags them only because it traces from `cmd/aura` main and can't see the
  channel/swarm/test entry surfaces (same false-positive class as ~37 other entries).
  No deletion — removing tested code would be a regression. `deferred-items.md`.

---

## Gate-3 status

- **Automated floor:** ✅ green (build/vet/test/race/cache) — whole module, zero failures.
- **Quality bar (B1/B2):** ✅ coverage 89.4% · lint 0 · vuln clean · mutation ≥70% on all
  three critical files (autopsy above).
- **Live sign-off (B3):** ✅ signed-off 2026-06-16 (HEAD `85b5e1ae`, 9/9 acceptance rows,
  each a ground-truth assertion — live-signoff Part B3). **Gate-3 CLOSED.**
- **Audit residue:** ✅ zero — 64/64 AG-### disposed in the ledger; HARDEN-01..12 all
  mapped.

---

## Validation Audit 2026-06-16

`/gsd-validate-phase 22` re-audit at HEAD `28c1f7c7` (clean tree). The predicted Part-A
PASSes were **re-executed live**, not trusted — and the named regression tests were
proven to actually run (not silently skip).

| Metric | Count |
|--------|-------|
| Requirements (HARDEN-01..12) | 12 |
| COVERED (real runnable green test) | 12 |
| PARTIAL | 0 |
| MISSING | 0 |
| Gaps found | 0 |
| Resolved | 0 |
| Escalated (manual-only) | 0 |

**Live re-execution (HEAD `28c1f7c7`, `go 1.26.4 windows/amd64`):**

| Gate | Command | Result |
|------|---------|--------|
| A1 | `go build ./...` | ✅ exit 0 |
| A2 | `go vet ./...` | ✅ exit 0 |
| A3 | `go test ./internal/agent/... ./internal/swarm/... ./internal/secret/... ./internal/reasoningtrace/... ./cmd/aura/...` (untagged) | ✅ all `ok`, exit 0 — incl. `cmd/aura` (the compose-drift `TestProductionContainerArtifactsMatchFatImageContract` is green after de-hardcode `e2b0d82a`) |
| A4 | `BASH_ENV=~/.aura-toolchain.sh go test -race ./internal/agent/... ./internal/swarm/...` | ✅ race-clean, all 7 packages `ok`, exit 0 |
| A5 | `bash scripts/cache_invariant_audit.sh` | ✅ 22 identical messages[0]/messages[1]/skill-manifest hashes, exit 0 |

**Execution proof (`-run … -v`):** `TestExecuteBatch_PanickingToolSurfacesModelVisibleError`
(+ `/inline_single_call`, `/parallel_batch_call`), `TestExecuteBatch_PanicRecordsObservabilityMetrics`,
`TestBudget_BeforeAfterToolResult_Concurrent`, `TestToolInvocation_ForensicShapeIsRawRedactionRoutedToStore`,
`TestUntrustedSource_UnknownToolDefaultsUntrusted`, `TestRunnerAdapterDrivesEngine`,
`TestSwarmPanicIsolation`, `TestConfiguredMCPCallTimeout` (6 subtests),
`TestBridge_BadTimeoutEnvFailsBeforeListTools`, `TestBridgedTool_Execute_NoDeadlineWhenTimeoutMinusOne`,
`TestMountManagedServer_StdioBranchFailure` — all printed `RUN` + `PASS`, none skipped.

**Stale-cited-name correction (no coverage impact — behaviors are COVERED):** the
`22-VERIFICATION.md` HARDEN-08 row and the live-signoff row 1b cite `TestTrustDefault`
and `TestSwarmRunnerAdapter`. Those exact names do **not** exist — `go test -run
'TestTrustDefault$'` / `-run 'TestSwarmRunnerAdapter$'` both print `[no tests to run]`
(silent false-green trap). The real, green tests are:

- HARDEN-08 default-untrusted → `internal/agent/trust_default_test.go`
  `TestUntrustedSource_UnknownToolDefaultsUntrusted` / `_SafeBuiltinsStayTrusted` /
  `_ContentToolsStayUntrusted` / `_ExplicitProvenanceWins`.
- HARDEN-08 swarm-child untrusted provenance → `internal/swarm/runner_adapter_test.go`
  `TestRunnerAdapterDrivesEngine` (asserts `res.Provenance.Trust == tools.TrustUntrusted`,
  line 64).

The cited names were corrected in `22-VERIFICATION.md` + the live-signoff doc by this audit.

**Not re-run by this audit (deliberate, already signed off):** B1 `make coverage` is a
**destructive shared-PG wipe** — re-running it would have corrupted concurrent work and
duplicated the 2026-06-15 evidence (owned-surface **89.4%**, commit `595fc6a1`); B2 lint/vuln
need the WSL `~/go/bin` toolchain (done 2026-06-15). These are Gate-3 quality bars, not
Nyquist test-coverage gaps, and were signed off in `22-VERIFICATION.md` (`status: passed`).

**Verdict:** Phase 22 is **NYQUIST-COMPLIANT** — every requirement has automated
verification that was observed to run green. Doc staleness (B3 still marked `pending` in
this file + `22-UAT.md`, two stale cited test names) was the only audit finding; corrected
here.
