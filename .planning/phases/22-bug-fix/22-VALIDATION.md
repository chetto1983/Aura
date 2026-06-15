---
phase: 22
slug: bug-fix
status: automated_passed_manual_pending
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-15
updated: 2026-06-15
evidence_head: 036575b5
mutation_score: pending-operator
coverage: pending-operator
---

# Phase 22 — Agent Perimeter Hardening: Validation Strategy

> Per-phase validation contract for the AG-001..064 hardening waves (22-01..22-05).
> The authoritative per-finding dispositions + named regression tests live in
> [`docs/audit/22-finding-ledger.md`](../../../docs/audit/22-finding-ledger.md).
> The full automated/live evidence is in
> [`docs/audit/22-LIVE-SIGNOFF-2026-06-15.md`](../../../docs/audit/22-LIVE-SIGNOFF-2026-06-15.md).
>
> This file splits the gate into **(A) AUTOMATED EVIDENCE — done** and **(B) PENDING
> OPERATOR SIGN-OFF** (the destructive coverage gate, the WSL quality bar, and the
> full live-stack pass). No fabricated pass is recorded for anything not run.

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
| A3 | `go test ./...` (untagged) | ✅ all `internal/...` PASS; ⚠️ `cmd/aura` 1 failure | only `TestProductionContainerArtifactsMatchFatImageContract` (pre-existing `:nitro`/`:exacto` compose drift from `136325dc`; out of scope, in `deferred-items.md`) |
| A4 | `go test -race ./internal/agent/... ./internal/swarm/...` | ✅ PASS (exit 0) | race-clean: agent, agenttest, mcptools, prompt, tools, workflow, swarm |
| A5 | `bash scripts/cache_invariant_audit.sh` | ✅ PASS (exit 0) | 22 identical `messages[0]`/`messages[1]`/skill-manifest hashes — KV prefix stable after the 22-05 skill-schema edit |

**Per-finding regression tests:** all named in the ledger; fail-before/pass-after shown
in the wave SUMMARYs (22-01..22-04). 22-05 added `TestSkillSchemaIsHonestNotDishonest`,
`TestMountManagedServer_HTTPBranchInfersFromBareURL`,
`TestMountManagedServer_StdioBranchFailure`,
`TestToolInvocation_ForensicShapeIsRawRedactionRoutedToStore` — all green in A3/A4.

---

## Part B — PENDING OPERATOR SIGN-OFF (NOT run in this checkout)

> Deferred to the operator: B1 wipes the shared Postgres (parallel sessions active);
> B2 needs the WSL `~/go/bin` toolchain; B3 needs the full live multi-service daemon.
> Exact commands + acceptance live in
> [`docs/audit/22-LIVE-SIGNOFF-2026-06-15.md`](../../../docs/audit/22-LIVE-SIGNOFF-2026-06-15.md)
> Parts B1–B3.

| # | Gate | Command (abridged) | Acceptance | Status |
|---|------|--------------------|------------|--------|
| B1 | Coverage ≥85% owned-surface | `make coverage` (stack up, **PG wipe**) | ≥85% combined across `db_integration neo4j_integration`; every owned pkg ≥85% | `pending` |
| B2a | Lint | `golangci-lint run ./...` | 0 issues | `pending` |
| B2b | Vuln | `govulncheck ./...` | 0 actionable CVEs | `pending` |
| B2c | Mutation ≥70% killed | `go-mutesting` on `llm_agent_parallel.go`, `budget_dedup.go`, `mcptools/bridge_reconnect.go` | ≥70% killed each, or documented near-equivalent-survivor autopsy | `pending` |
| B3 | Full live stack | `aura serve` + acceptance matrix | per the live-signoff B3 table (metrics scrape, CDP Telegram, GLM-OCR fs-cap, MCP reconnect, reasoning fallback, skill self-extension, DSN boundary, ledger redaction) | `pending` |

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

## Known out-of-scope (not fixed in Phase 22)

- `cmd/aura` `TestProductionContainerArtifactsMatchFatImageContract` — pre-existing
  compose `:nitro`/`:exacto` drift (`136325dc`). `deferred-items.md`.
- `internal/skills` dead funcs (`BM25Corpus`, `SnippetInvocation`,
  `ValidateNameAgainstDir`) flagged by `deadcode` — outside the Phase-22 audited
  surface; NOT AG-### findings. `deferred-items.md`.

---

## Gate-3 status

- **Automated floor:** ✅ green (build/vet/test/race/cache), modulo the documented
  out-of-scope compose-drift test.
- **Coverage / mutation / live:** ⏳ `pending` operator sign-off (Part B).
- **Audit residue:** ✅ zero — 64/64 AG-### disposed in the ledger; HARDEN-01..12 all
  mapped.
