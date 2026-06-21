---
phase: 29-governance-write-mcp-configuration-skills-install
plan: 02
subsystem: api
tags: [mcp, governance, audit-ledger, pgx, capability, react-query, secrets-redaction]

# Dependency graph
requires:
  - phase: 29-governance-write-mcp-configuration-skills-install (plan 01)
    provides: aura.mcp_audit ledger (0022) + MCPAuditStore/InsertMCPAuditTx + governanceWriteCapability const
  - phase: 16-mcp-manager
    provides: ManagedConfig CRUD (SaveManagedConfig), mergeEnvPreserveCredentials/isPlaceholderValue (D-05 substrate), ProbeServer, BuiltInCatalog/RequiredEnv, trust classes
  - phase: 28-governance-read-boards
    provides: governance_seam.go narrow-provider pattern, governance_api.go thin-handler shape, envChips key-only projection, mcpBoardAdapter
  - phase: 24-serve-auth
    provides: RequireCapability + principalFrom/principalIdentityID in internal/agui/auth.go
provides:
  - "internal/mcp/manager/envedit.go — SetServerEnv: four-state secret-preserve merge (placeholder=preserve, real value=rotate, non-secret=edit/clear)"
  - "internal/mcp/manager/configwrite.go — WriteConfigWithAudit: atomic temp→db.WithTx(audit INSERT)→os.Rename (D-04 atomicity + MCPW-01 concurrency edge)"
  - "internal/agui/governance_write_seam.go — MCPWriteProvider interface + GovernanceWriteProviders bundle + SetGovernanceWriteProviders setter + ErrMCPServerExists/NotFound sentinels"
  - "internal/agui/governance_write_api.go — POST/PATCH/DELETE /api/governance/mcp[/{name}/...] six named-action handlers (install/env/trust/enable/disable/remove)"
  - "cmd/aura/serve_governance_write.go — mcpWriteAdapter concrete provider (reload-per-call, atomic write+audit, fail-soft reprobe, recipe soft warnings)"
  - "cmd/aura/serve_webui.go — six RequireCapability(governance.write) mounts as method+path-specific siblings"
affects: [29-03, 29-04, 29-05]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Atomic FS-mutation + audit: temp config write → InsertMCPAuditTx inside db.WithTx → os.Rename on commit / discard temp on tx failure (no applied-but-unaudited, no audited-but-not-applied)"
    - "Four-state env merge: a submitted redacted ${KEY} placeholder preserves the stored secret; a real value rotates; non-secret edits/clears in place — distinct from blanket-preserve mergeEnvPreserveCredentials"
    - "Consumer-side narrow write seam + off-constructor setter (parity with governance_seam.go); a nil provider answers 503"
    - "Reload-per-call concrete provider so concurrent CLI/operator edits are not clobbered"

key-files:
  created:
    - internal/mcp/manager/envedit.go
    - internal/mcp/manager/envedit_test.go
    - internal/mcp/manager/configwrite.go
    - internal/mcp/manager/configwrite_test.go
    - internal/mcp/manager/configwrite_integration_test.go
    - internal/agui/governance_write_seam.go
    - internal/agui/governance_write_api.go
    - internal/agui/governance_write_api_test.go
    - cmd/aura/serve_governance_write.go
  modified:
    - internal/agui/server.go
    - cmd/aura/serve_webui.go
    - cmd/aura/serve.go

key-decisions:
  - "SetServerEnv uses a NEW placeholder-aware mergeSubmittedEnv, NOT the blanket-preserve mergeEnvPreserveCredentials — the latter keeps the existing secret against ANY incoming value so a rotation could never land (Rule-1 fix; the four-state edit needs both untouched-preserve AND rotate)"
  - "WriteConfigWithAudit stages to a sibling temp + renames on tx commit — one mechanism serves BOTH the D-04 atomicity AND the MCPW-01 concurrency-edge temp+rename (SaveManagedConfig is a direct WriteFile today)"
  - "A custom (non-recipe) server defaults to TrustBlocked until an explicit operator trust-approve (parity with the CLI mcpAdd default; T-29-02-03 — no model self-trust)"
  - "The concrete adapter reloads the managed config from disk per call rather than caching, so a concurrent CLI/operator edit is not clobbered by a stale doc"
  - "Env echo is key-only redacted chips (envChips reused); the secret value never crosses the wire (T-29-02-01 / Pitfall 4)"

patterns-established:
  - "Atomic temp→tx→rename config-write wrapper for FS-mutation+audit-row all-or-nothing"
  - "Four-state placeholder-aware env merge (preserve / rotate / edit / clear)"
  - "MCP write seam: one consumer-declared interface, concrete adapter at the composition root, nil → 503"

requirements-completed: [MCPW-01, MCPW-02, MCPW-03]

# Metrics
duration: ~24min
completed: 2026-06-21
---

# Phase 29 Plan 02: MCP write backend + mutating endpoints Summary

**Six operator-only MCP config write endpoints (install/env-edit/trust/enable/disable/remove) over the Phase-16 manager, each atomic with its `aura.mcp_audit` row via a temp→tx→rename wrapper, gated behind `RequireCapability(governance.write)`, with a four-state secret-preserve env merge that never leaks a value on the wire.**

## Performance

- **Duration:** ~24 min
- **Started:** 2026-06-21T12:10:16+02:00 (Task 1 commit)
- **Completed:** 2026-06-21T12:34:25+02:00 (Task 3 commit)
- **Tasks:** 3
- **Files modified:** 12 (9 created, 3 modified)

## Accomplishments
- `SetServerEnv` + `WriteConfigWithAudit`: the in-place env-edit path on a four-state placeholder-aware merge, and the atomic temp→`db.WithTx`(audit INSERT)→`os.Rename` wrapper that makes every MCP mutation+audit all-or-nothing (proven against live PG: an induced tx failure leaves `servers.json` byte-identical with zero audit rows; a clean write flips the config + exactly one row; one row per action across the six verbs).
- `MCPWriteProvider` seam + six named-action handlers mirroring the read-board thin-handler shape: install previews the CLI-equiv + `ManagedConfigPath()` destination + a live reprobe; env-edit echoes ONLY key-only redacted chips (no value); trust-approve populates the today-empty `ApprovedBy/At/Reason` + reprobes; enable/disable idempotent; double-remove 404s — each with one audit row.
- Six `RequireCapability(governance.write)` mounts as method+path-specific siblings (never a bare `/api/`) + the concrete `mcpWriteAdapter` wired best-effort at the composition root (nil pool/path → 503, never aborts boot); the auth gate (200 grantee / 403 no-grant / 403 no-principal) is pinned by a serve-level mount test.

## Task Commits

Each task was committed atomically (TDD: test + impl folded per task):

1. **Task 1: SetServerEnv four-state merge + atomic config-write wrapper** - `0ed8d7dd` (feat)
2. **Task 2: MCPWriteProvider seam + six named-action handlers** - `28ed1b6a` (feat)
3. **Task 3: mount routes behind governance.write + wire concrete provider** - `95e541ae` (feat)

## Files Created/Modified
- `internal/mcp/manager/envedit.go` - SetServerEnv + mergeSubmittedEnv (four-state secret-preserve)
- `internal/mcp/manager/configwrite.go` - WriteConfigWithAudit (temp→tx→rename) + ErrServerNotFound
- `internal/mcp/manager/{envedit,configwrite}_test.go` - secret-preserved property, unknown-server 404, non-secret clear, temp staging
- `internal/mcp/manager/configwrite_integration_test.go` - tx-failure-leaves-prior-config + one-row-per-action (live PG)
- `internal/agui/governance_write_seam.go` - MCPWriteProvider + bundle + setter + 409/404 sentinels
- `internal/agui/governance_write_api.go` - the six thin write handlers + envChips echo + sanitized errors
- `internal/agui/governance_write_api_test.go` - 12 handler/mount tests (httptest)
- `internal/agui/server.go` - governanceWrite field + registerGovernanceWriteRoutes
- `cmd/aura/serve_governance_write.go` - mcpWriteAdapter concrete provider
- `cmd/aura/serve_webui.go` - six governance.write route consts + mounts
- `cmd/aura/serve.go` - buildMCPWriteProvider + SetGovernanceWriteProviders wiring

## Decisions Made
See `key-decisions` frontmatter. The load-bearing one: the env-edit merge had to be a NEW placeholder-aware `mergeSubmittedEnv`, because the existing `mergeEnvPreserveCredentials` preserves a stored secret against *any* incoming value (it can never rotate) — that was a Rule-1 correctness fix (see Deviations).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Env-edit could never rotate a secret with the blanket-preserve merge**
- **Found during:** Task 1 (SetServerEnv)
- **Issue:** The plan pointed at `mergeEnvPreserveCredentials` (config.go:95) as the D-05 substrate, but that function preserves an existing real secret against ANY incoming override (it is the `OverwriteCredentials:false` import path). The plan's own `<behavior>` requires "a non-placeholder submitted value overwrites" (secret rotation), and the four-state MCPW-02 edit needs both untouched-preserve AND rotate. Using the blanket-preserve merge directly made the rotation test fail: a real submitted `TOKEN=rotated-secret` left the stored value unchanged.
- **Fix:** Implemented a focused placeholder-aware `mergeSubmittedEnv` in `envedit.go` that preserves the stored secret ONLY when the submitted value is the redacted `${KEY}` placeholder (or empty for a secret key), and lets a real value overwrite (rotate) / a non-secret edit-or-clear in place. It reuses the same `cutEnv`/`isPlaceholderValue`/`secret.IsSecretEnvKey` substrate, so the redaction denylist and placeholder grammar stay single-sourced.
- **Files modified:** internal/mcp/manager/envedit.go
- **Verification:** `TestSetServerEnvPreservesUntouchedSecret` asserts both the placeholder-preserve AND the real-value-rotate paths; `TestGovernanceWriteEnvEditNoSecretOnWire` asserts the placeholder preserves end-to-end through the handler with no value on the wire.
- **Committed in:** `0ed8d7dd` (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 bug)
**Impact on plan:** The fix is required for MCPW-02 correctness (rotation) and stays within the D-05 secrets-never-leaked contract — no scope creep. All other tasks executed as written.

## Issues Encountered
None beyond the deviation above. Two interleaved `graphify` tooling commits (`58b3f413`, `d356a65d`) from a parallel process landed between my Task commits on master; they touch only `.claude`/`scripts/graphify` and are unrelated to this plan — my four-file-per-task staging kept each 29-02 commit clean.

## User Setup Required
None - no external service configuration required. (The `governance.write` capability is held implicitly by the seeded `local` identity via its `*` wildcard; a future per-identity grant is the only setup, and that is identity-administration, not service config.)

## Next Phase Readiness
- The MCP write backend + endpoints are live: plan 29-04 (the React MCP write UI — install panel, four-state env-edit form, trust/enable/disable/remove controls) can call `POST/PATCH/DELETE /api/governance/mcp[/{name}/...]` and render the key-only env chips + CLI-equiv/destination preview + soft warnings + probe the handlers return.
- Plan 29-03 (skills install) reuses the same `governance.write` mount idiom + the `GovernanceWriteProviders` bundle (add a `Skills` field) established here.

## Self-Check: PASSED

All created files present on disk (envedit.go, configwrite.go, configwrite_integration_test.go, governance_write_seam.go, governance_write_api.go, governance_write_api_test.go, serve_governance_write.go) and all three task commits present in git history (`0ed8d7dd`, `28ed1b6a`, `95e541ae`). `go build ./...` + `go vet ./...` clean; `golangci-lint` 0 issues on the touched packages; unit + `db_integration` (live PG) + `-race` all green.

---
*Phase: 29-governance-write-mcp-configuration-skills-install*
*Completed: 2026-06-21*
