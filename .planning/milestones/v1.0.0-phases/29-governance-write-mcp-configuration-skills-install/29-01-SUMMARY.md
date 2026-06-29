---
phase: 29-governance-write-mcp-configuration-skills-install
plan: 01
subsystem: database
tags: [postgres, sqlc, pgx, audit-ledger, rbac, capability, governance, mcp]

# Dependency graph
requires:
  - phase: 21-identity-audit
    provides: append-only identity_audit ledger template (0021) — dual triggers + role grant pattern
  - phase: 16-mcp-manager
    provides: MCP manager package the audit store lives beside
  - phase: 24-serve-auth
    provides: RequireCapability + principalFrom in internal/agui/auth.go
provides:
  - "aura.mcp_audit append-only ledger (migration 0022): role grant SELECT+INSERT only + BEFORE UPDATE OR DELETE row trigger + separate BEFORE TRUNCATE statement trigger"
  - "MCPAuditStore + InsertMCPAuditTx (tx-bound, atomic with surrounding db.WithTx) + List (newest-first, limit-clamped)"
  - "governanceWriteCapability const (\"governance.write\") — the capability gate every Phase-29 write surface mounts behind"
  - "D-09 SPEC/REQUIREMENTS/ROADMAP amendment: container-isolation framing supersedes --ignore-scripts (install scripts permitted; control = approval gate + Writer validation)"
affects: [29-02, 29-03, 29-04, 29-05]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Append-only ledger mirrors 0021 field-for-field (dual triggers + role grant), flat schema (no D-29 coherence CHECK)"
    - "Tx-bound audit insert (InsertMCPAuditTx accepts tx-bound *sqlc.Queries) commits atomically with the mutation it records"

key-files:
  created:
    - internal/db/migrations/0022_mcp_audit.up.sql
    - internal/db/migrations/0022_mcp_audit.down.sql
    - internal/db/queries/mcp_audit.sql
    - internal/db/sqlc/mcp_audit.sql.go
    - internal/mcp/manager/audit.go
    - internal/mcp/manager/audit_test.go
    - internal/mcp/manager/audit_store_integration.go
    - internal/mcp/manager/audit_store_integration_test.go
  modified:
    - cmd/aura/serve_webui.go
    - internal/db/sqlc/models.go
    - internal/db/sqlc/querier.go
    - .planning/phases/29-governance-write-mcp-configuration-skills-install/29-SPEC.md
    - .planning/REQUIREMENTS.md
    - .planning/ROADMAP.md

key-decisions:
  - "D-09 amendment landed as a SEPARATE first commit reconciling all three locked truth-source artifacts atomically (PRD-first; precedent Phase-28 D-07)"
  - "mcp_audit kept flat (id/created_at/actor_identity_id/action/server_name/reason) — no D-29 coherence machinery; that is skill-specific"
  - "actor_identity_id stores the capability-layer principal (principalFrom), not the raw Authula user id — same choice as 0021"
  - "No HTTP routes mounted in this plan — only the capability const + store + ledger; plans 02/03 add the RequireCapability mounts"

patterns-established:
  - "Append-only DB ledger: GRANT SELECT,INSERT to aura_app + reject-mutation function + row trigger (UPDATE/DELETE) + statement trigger (TRUNCATE)"
  - "Tx-bound audit append: a mutation cannot commit without its audit row (repudiation mitigation T-29-01-02)"

requirements-completed: [MCPW-02, SKW-01]

# Metrics
duration: ~60min
completed: 2026-06-21
---

# Phase 29 / Plan 01: MCP-write foundation Summary

**Append-only `aura.mcp_audit` ledger (migration 0022, dual triggers + role grant), the tx-bound `MCPAuditStore`/`InsertMCPAuditTx`, the `governance.write` capability const, and the D-09 container-isolation amendment across SPEC/REQUIREMENTS/ROADMAP.**

## Performance

- **Duration:** ~60 min
- **Started:** 2026-06-21T10:38:18+02:00
- **Completed:** 2026-06-21T11:37:59+02:00
- **Tasks:** 3
- **Files modified:** 13 (created + modified)

## Accomplishments
- D-09 BLOCKING amendment: replaced the `--ignore-scripts`-as-safe framing with container-isolation (five-item validation checklist) across 29-SPEC.md + REQUIREMENTS.md + ROADMAP.md in one atomic docs commit.
- Append-only `aura.mcp_audit` ledger (0022) mirroring 0021: `aura_app` holds SELECT+INSERT only; UPDATE/DELETE raise insufficient_privilege via a row trigger; TRUNCATE raises insufficient_privilege via a separate statement trigger.
- `MCPAuditStore` + `InsertMCPAuditTx` (tx-bound, atomic with `db.WithTx`) + `List` (newest-first `created_at DESC, id DESC`, limit clamp), plus sqlc regen.
- `governanceWriteCapability = "governance.write"` const matched to the existing `internal/agui/auth_test.go` assertion. No routes mounted yet (plans 02/03 add them).

## Task Commits

Each task was committed atomically:

1. **Task 1: D-09 amendment (SPEC + REQUIREMENTS + ROADMAP)** - `18beb438` (docs)
2. **Task 2: 0022_mcp_audit migration + sqlc queries + integration test** - `91d6a687` (feat)
3. **Task 3: MCPAuditStore + InsertMCPAuditTx + governance.write const + unit tests** - `a65215c1` (feat)

_Note: Tasks 2 and 3 were TDD (test + feat folded into their feat commits)._

## Files Created/Modified
- `internal/db/migrations/0022_mcp_audit.{up,down}.sql` - append-only ledger + reverse
- `internal/db/queries/mcp_audit.sql` - InsertMcpAudit/ListMcpAudit sqlc queries
- `internal/db/sqlc/mcp_audit.sql.go`, `models.go`, `querier.go` - sqlc regen
- `internal/mcp/manager/audit.go` - MCPAuditStore + InsertMCPAuditTx + MCPAuditInsert + List
- `internal/mcp/manager/audit_test.go` - unit (param NULL boundary, list clamp)
- `internal/mcp/manager/audit_store_integration{,_test}.go` - live-PG append-only proof
- `cmd/aura/serve_webui.go` - governanceWriteCapability const
- `.planning/.../29-SPEC.md`, `.planning/REQUIREMENTS.md`, `.planning/ROADMAP.md` - D-09 amendment

## Decisions Made
None beyond plan — executed as specified (see key-decisions frontmatter).

## Deviations from Plan
None - plan executed as written. (This SUMMARY was authored during a safe-resume close-out: the three task commits existed but the SUMMARY and STATE/ROADMAP advance had not been recorded. Work re-verified before closing out — see Issues Encountered.)

## Issues Encountered
- **Missing SUMMARY on resume:** The three 29-01 task commits were present but `29-01-SUMMARY.md` and the STATE/ROADMAP completion advance were never written (executor interrupted after the last task commit). Resolved by the safe-resume close-out: re-verified the committed work (`go build`/`go vet` clean; `mcp/manager` MCPAudit + `agui` RequireCapability unit tests green; `db_integration TestMCPAuditAppendOnly` PASS against the live stack — UPDATE/DELETE/TRUNCATE each denied; D-09 amendment grep PASS), then authored this SUMMARY and advanced tracking.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- The `governance.write` capability const, the append-only audit ledger, and `InsertMCPAuditTx` are in place. Plan 02 (MCP write backend + mutating endpoints) can now mount `RequireCapability(..., governanceWriteCapability)` routes and append audit rows inside the mutation transaction.

---
*Phase: 29-governance-write-mcp-configuration-skills-install*
*Completed: 2026-06-21*
