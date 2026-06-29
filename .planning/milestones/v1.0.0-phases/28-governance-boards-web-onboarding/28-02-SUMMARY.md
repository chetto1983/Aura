---
phase: 28-governance-boards-web-onboarding
plan: 02
subsystem: api
tags: [agui, rest, governance, mcp, skills, audit, scheduler, redaction, dependency-injection]

# Dependency graph
requires:
  - phase: 28-01
    provides: "agui SetGovernanceProviders + MCPBoardProvider/SkillsBoardProvider/SchedulerBoardProvider narrow interfaces; mcp.ProbeServer (bounded structured probe); skills.ListStage (no-body per-stage reader); cron.Store.ListRunsForTask (paginated run history); manager.SnapshotStatus (by-name static rows)"
  - phase: 27-graph-explorer
    provides: "the graph_api.go thin-handler template (503-when-unwired, sanitized-502, parent-mux mount behind RequireAuth) + the SetGraphView off-constructor DI precedent"
provides:
  - "GET /api/governance/mcp: static MCP registry rows by-name with redacted env-KEY chips (env values NEVER serialized), lastError redacted (GOV-01)"
  - "GET /api/governance/mcp/{name}/probe: per-row LIVE probe bounded 3s, configured-servers-only (404 if absent), hung server isolates to its row (GOV-01)"
  - "GET /api/governance/skills?stage=active|pending|archived: lifecycle list, pending rows carry NO action field, body never serialized (GOV-02)"
  - "GET /api/governance/skills/audit: append-only ledger newest-first via AuditStore.List, default limit 100 (GOV-02)"
  - "GET /api/governance/scheduler[/{id}/runs]: active tasks + paginated run history (uuid-guarded 404 pre-store, default limit 25/offset 0), mutates nothing (GOV-03)"
  - "cmd/aura serve_governance.go board adapters (mcpBoardAdapter/skillsBoardAdapter) + buildGovernanceProviders best-effort wiring"
affects: [Plan 03 frontend governance workspace, Plan 04 board UI]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Thin governance REST adapter over Wave-0 seams (graph_api.go template): nil-check provider → 503, one provider call, project to JSON, sanitizeErr on every wire error"
    - "Redacted env-KEY chips: env VALUES never serialized; only secret.IsSecretEnvKey-flagged KEY names + {redacted:true} reach the wire (T-28-02-01)"
    - "Per-row-isolated live probe: each MCP probe is its own request under context.WithTimeout(3s); a hung server fails only its own row, never stalls the static list"
    - "Off-constructor probe timeout override (s.probeTimeout): production uses 3s; tests shrink it to exercise the deadline-honoring path deterministically without a 3s wait"
    - "Best-effort governance provider construction: a provider that cannot be built is left nil (board answers 503), never aborting daemon boot (SetGraphView precedent)"

key-files:
  created:
    - internal/agui/governance_api.go
    - internal/agui/governance_api_test.go
    - cmd/aura/serve_governance.go
  modified:
    - internal/agui/server.go
    - internal/agui/governance_seam_test.go
    - cmd/aura/serve_webui.go
    - cmd/aura/serve.go

key-decisions:
  - "MCP list source reconciles the seam + RESEARCH: the seam's MCPBoardProvider.Servers() returns mcp.ManagedConfig; the handler calls manager.SnapshotStatus(doc) ITSELF for the by-name static rows (RESEARCH §REST shape) and walks doc.MCPServers[name].Env for redacted KEY chips — keeping the handler thin (one provider call + a pure projection helper) while honoring both contracts."
  - "Added an off-constructor s.probeTimeout field (default 3s via defaultProbeTimeout): the plan mandates a 3s probe deadline but a 3s test wait is unacceptable, so tests set 50ms to exercise the deadline-honoring path; production keeps 3s. Mirrors the existing off-constructor DI seam discipline (D-A2-02)."
  - "*cron.Store satisfies agui.SchedulerBoardProvider verbatim (ListActiveTasks + ListRunsForTask match the seam), so the daemon passes the live store DIRECTLY — no scheduler adapter needed. Only MCP + skills need thin adapters (their method names differ from the loader/store)."
  - "Updated the Wave-0 seam test (TestGovernanceSeamsAddNoRoutes → TestOnboardingSeamAddsNoRoutes): its no-routes-yet invariant is exactly what Plan 02 changes for governance. The onboarding 404 assertion stays (Plan 05 adds it); the governance assertion flips to non-404 (Plan 02 registered it)."

patterns-established:
  - "Governance board handler shape: provider-nil → 503; success → writeJSON({key:[...]}) with a safe empty array on no data; backend error → writeJSONStatus(502, sanitizeErr)"
  - "uuid-guard pre-store 404 for the scheduler subtree (parseTaskID), mirroring parseConvID for conversations"

requirements-completed: [GOV-01, GOV-02, GOV-03]

# Metrics
duration: ~50 min
completed: 2026-06-20
status: complete
---

# Phase 28 Plan 02: Governance Boards Backend Summary

**Six authenticated read-only `/api/governance/*` GET endpoints (GOV-01/02/03) — the MCP registry + bounded per-row live probe, the skills lifecycle + append-only audit ledger, the scheduler tasks + paginated run history — as a thin REST adapter over the Wave-0 seams following the graph_api.go template, with env-secret redaction by construction, per-row probe isolation, and best-effort composition-root wiring that never aborts boot.**

## Performance

- **Duration:** ~50 min
- **Started:** 2026-06-20T09:35Z (approx)
- **Completed:** 2026-06-20T10:25Z
- **Tasks:** 2 (both `type="auto"`, Task 1 `tdd="true"`)
- **Files created/modified:** 7

## Accomplishments

- **Six read-only governance endpoints** registered on `Server.Mux()` and mounted on the parent mux behind `RequireAuth` (no `RequireCapability` — read-only): `GET /api/governance/mcp`, `GET /api/governance/mcp/{name}/probe`, `GET /api/governance/skills`, `GET /api/governance/skills/audit`, `GET /api/governance/scheduler`, `GET /api/governance/scheduler/{id}/runs`.
- **No-raw-secret control (T-28-02-01):** the MCP list serializes env entries as redacted KEY chips ONLY — the env VALUE is never carried; `secret.IsSecretEnvKey` flags the key, `mcp.RedactSecrets` cleans `lastError`. Asserted by `TestGovernanceMCPNoSecretAndOrdering` (the body never contains the secret value, rows are by-name ordered).
- **Per-row probe isolation (GOV-01 / R1 boundary):** the live probe runs under `context.WithTimeout(r.Context(), 3s)`; a hung server resolves to `ok:false` for ITS row only and a sibling probe (separate request) succeeds independently. Asserted by `TestMCPProbeIsolation`.
- **Configured-servers-only (Prohibition #5):** the probe looks `{name}` up in the loaded config (404 if absent) and never dials an unknown name — `TestMCPProbeConfiguredOnly` asserts no probe is dialed for a ghost name.
- **Skills lifecycle + audit:** pending/archived rows read via the Wave-0 per-stage reader (never a body); pending rows carry NO action field; audit is newest-first via `AuditStore.List`.
- **Scheduler pagination:** a non-UUID `{id}` is a clean 404 BEFORE the store call; run history paginates with default limit 25 / offset 0.
- **Cross-cutting:** empty datasets → 200 + empty array; unwired provider → 503; backend error → sanitized 502 (no DSN/host/secret leak); unauthenticated → 401 (inherited `RequireAuth`).
- **Best-effort composition-root wiring:** `buildGovernanceProviders` assembles the MCP/skills/scheduler providers over the existing seams; a provider that cannot be constructed is left nil so its board answers 503, never aborting daemon boot.

## Task Commits

1. **Task 1: governance_api.go — six read handlers over the Wave-0 seams** — `407308c6` (feat)
2. **Task 2: parent-mux mount + composition-root wiring** — `e65e3fa7` (feat)

_TDD note: Task 1 is `tdd="true"`. The project pre-commit hook enforces `go build`/`go vet`, so a test-only commit referencing the not-yet-registered routes cannot be committed in isolation; tests + handlers land in one `feat` commit. RED was proven out-of-band: the handler tests fail (404 on the unregistered routes / nil-deref) before the handlers + Mux wiring exist; GREEN after._

## Files Created/Modified

**Task 1 (handlers):**
- `internal/agui/governance_api.go` — six handlers + `registerGovernanceRoutes` + the narrow projection helpers (env chips, row shapes); 362 LOC.
- `internal/agui/governance_api_test.go` — configurable scripted board fakes + the full behavior matrix (no-secret, by-name, probe isolation/configured-only/sanitized, skills stages/pending-no-action, audit newest-first, scheduler non-UUID-404/pagination, empty/unwired-503/backend-502, auth-gate 401).
- `internal/agui/server.go` — `s.registerGovernanceRoutes(mux)` in `Mux()` + the off-constructor `probeTimeout` field.
- `internal/agui/governance_seam_test.go` — Wave-0 no-routes test updated (governance now has a handler; onboarding still 404 until Plan 05).

**Task 2 (mount + wiring):**
- `cmd/aura/serve_webui.go` — six `/api/governance/*` route consts mounted as read-GETs beside the graph reads (sibling under the `/api/` carve-out, never a bare `/api/`).
- `cmd/aura/serve_governance.go` — `mcpBoardAdapter` + `skillsBoardAdapter` over the existing seams + `buildGovernanceProviders` (best-effort).
- `cmd/aura/serve.go` — `aguiServer.SetGovernanceProviders(buildGovernanceProviders(chat.cfg, chat.pool, store))` after the SetGraphView block.

## Decisions Made

See `key-decisions` frontmatter. Most consequential:
1. **MCP list source reconciliation** — the seam exposes `Servers() mcp.ManagedConfig`, while RESEARCH §REST names `SnapshotStatus(doc)` as the source. The handler calls `manager.SnapshotStatus(doc)` itself for the by-name static rows and walks `doc.MCPServers[name].Env` for the redacted KEY chips, keeping the handler thin while honoring both contracts. No import cycle (`internal/mcp/manager` imports only `internal/mcp`; `internal/agui` did not import `manager` before).
2. **`*cron.Store` satisfies the scheduler seam directly** — its `ListActiveTasks` + `ListRunsForTask` match `agui.SchedulerBoardProvider` verbatim, so the daemon passes the live store with no adapter. Only MCP + skills need thin adapters.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Added an off-constructor `s.probeTimeout` field for a fast, deterministic probe-isolation test**
- **Found during:** Task 1 (TestMCPProbeIsolation)
- **Issue:** The plan mandates the MCP probe run under a 3s `context.WithTimeout`, and the acceptance test asserts a hung server "resolves within ~3s". A test that actually waits 3s for the deadline is unacceptably slow and brittle.
- **Fix:** Added an unexported `probeTimeout time.Duration` field on `Server` (off the constructor, defaulting to `defaultProbeTimeout = 3s` via `probeDeadline()`); `govServer` sets it to 50ms so the hung-probe test exercises the real deadline-honoring path deterministically while production keeps 3s. This mirrors the existing off-constructor DI seam discipline (D-A2-02).
- **Files modified:** internal/agui/server.go, internal/agui/governance_api.go, internal/agui/governance_api_test.go
- **Verification:** `TestMCPProbeIsolation` passes in 0.05s (asserts the hung probe returns 200 + ok=false under the deadline AND a sibling probe succeeds independently); production default unchanged at 3s.
- **Committed in:** 407308c6 (Task 1 commit)

**2. [Rule 1 - Test correctness] Updated the Wave-0 seam test whose no-routes invariant Plan 02 deliberately changes**
- **Found during:** Task 1 (registering registerGovernanceRoutes)
- **Issue:** `TestGovernanceSeamsAddNoRoutes` (28-01) asserted `/api/governance/mcp` returns 404 because Wave 0 registered no routes. Registering the governance routes (this plan's whole purpose) makes a wired Server answer non-404, which would correctly break that stale assertion.
- **Fix:** Renamed to `TestOnboardingSeamAddsNoRoutes`; kept the `/api/onboarding/start` → 404 assertion (Plan 05 still owns that route) and flipped the `/api/governance/mcp` assertion to non-404 (Plan 02 registered it). The compile-time `var _ CapabilitySource = (*identity.Store)(nil)` check is preserved.
- **Files modified:** internal/agui/governance_seam_test.go
- **Verification:** the renamed test passes; the onboarding 404 + governance non-404 are both asserted.
- **Committed in:** 407308c6 (Task 1 commit)

---

**Total deviations:** 2 auto-fixed (1 blocking test-infrastructure field, 1 test-correctness). **Impact:** Both necessary for a correct, fast, non-brittle result. The `probeTimeout` field is a test seam with a production-safe 3s default (no behavior change in production); the seam-test update reflects the deliberate Wave-0→Wave-2 route-table change. No scope creep — the public surface matches the plan's artifacts.

## Issues Encountered

- **WSL toolchain invocation gotcha:** `wsl bash -lc` (login shell) swallowed `go version`/`go build` output (a profile function/alias intercepts), and `go env GOROOT` returned empty under the `go1.26.3`-symlinked wrapper. Resolved by using a plain non-login `wsl bash -c` with the explicit `PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"` prepend (the canonical recipe) and invoking `gofmt` by its absolute path `/home/davide/.local/go1.26.3/bin/gofmt`. No code impact.
- **Parallel Codex frontend session active:** a frontend commit (`decfac1b` "fix: allow parallel graph relationships in sigma") landed around my work. All staging was by explicit path (never `git add -A`); both my commits contain only my seven declared files. No contamination.

## User Setup Required

None - no external service configuration required. The endpoints read the existing managed MCP config, skills dirs, and scheduler store; no new env vars or accounts.

## Next Phase Readiness

- **Plan 03 (frontend governance workspace)** can consume the six `/api/governance/*` endpoints. The response shapes are: `{servers:[{name,trust,runtime,startupState,authStatus,envKeys:[{key,redacted}],lastError?}]}`, the probe `{name,ok,tool_count,detail,err?}`, `{skills:[{name,description,type,language?,contentHash?}]}`, `{rows:[AuditRow…]}`, `{tasks:[cron.Task…]}`, `{runs:[cron.Run…]}`.
- **Plan 05 (onboarding saga)** is unaffected — the onboarding seam (`SetOnboardingService`) remains unwired and its routes still 404 (asserted by `TestOnboardingSeamAddsNoRoutes`); Plan 05 registers `registerOnboardingRoutes` + the create mutation's `RequireCapability` gate.
- No blockers. `go build ./... && go vet ./...` clean module-wide; `go test -race ./internal/agui/` green.

## Self-Check: PASSED

- All 3 created files + 4 modified files verified present on disk (`[ -f ]`).
- Both task commits verified in git history: `407308c6` (Task 1), `e65e3fa7` (Task 2).
- All task `<acceptance_criteria>` re-run and passing: `registerGovernanceRoutes` present + wired (grep), 6 routes registered, the no-secret/by-name/probe-isolation/configured-only/pending-no-action/non-UUID-404/pagination/empty/503/502/401 tests all PASS; the six serve_webui consts + mounts + the SetGovernanceProviders call present (grep).
- Plan `<verification>` commands re-run: full module `go build ./...` + `go vet ./...` clean; `go test ./internal/agui/` green (full package + `-race`).

---
*Phase: 28-governance-boards-web-onboarding*
*Completed: 2026-06-20*
