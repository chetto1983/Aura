---
phase: 38-mcp-governance-hardening
plan: 06
subsystem: mcp
tags: [mcp, health-check, doctor, cli, probe, governance, http]

# Dependency graph
requires:
  - phase: 38-01
    provides: "mcp.Classify(ManagedServer) (serverType, trust, err) — the single canonical transport+trust classifier"
provides:
  - "cmd/aura/mcp_status.go writeRuntimeCheck live-probes HTTP servers via mcp.ProbeServer (F-046 fixed): a dead/typoed HTTP endpoint reports OK=false / 'runtime missing', not the old false-healthy 'http endpoint configured'"
  - "cmd/aura/mcp_status.go mcpStatus additively surfaces the live probe per server (mcpStatusRow{StatusSnapshot, Probe}) alongside the config-derived columns, skipping disabled/blocked servers"
  - "cmd/aura/doctor.go 6th doctorCheck{name: 'mcp'} — defaultDoctorProbeMCPServers live-probes only enabled+runnable+streamable-HTTP managed servers, D-18-scoped (skips disabled/blocked/stdio)"
  - "resolveMCPProbeTimeout() (mcp_status.go) — AURA_MCP_PROBE_TIMEOUT env knob (seconds, default 5) shared by all three cmd/aura probe call sites"
  - "call-site #9 (mcp_status.go inline Type==streamable_http||URL!='' check) migrated onto mcp.Classify"
affects: [38-verify, 38-secure]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "One probe (mcp.ProbeServer), N surfaces: governance read board (handleMCPProbe), governance write (mcpWriteAdapter.probe), and now aura doctor + aura mcp status all converge on the same already-tested ProbeServer, each with its own per-call context.WithTimeout bound"
    - "Skip-disabled/blocked-before-dialing: a read-only status/health listing must never spawn or dial a server the operator turned off or hasn't trust-approved, mirroring the pre-existing mcpDoctorAll skip"

key-files:
  created:
    - cmd/aura/mcp_status_test.go
  modified:
    - cmd/aura/mcp_status.go
    - cmd/aura/mcp.go
    - cmd/aura/mcp_test.go
    - cmd/aura/doctor.go
    - cmd/aura/doctor_test.go

key-decisions:
  - "resolveMCPProbeTimeout() is named distinctly from the pre-existing serve_governance_write.go mcpProbeTimeout CONST (3s, governance-write's own post-write probe budget) to avoid a redeclare — the two independently resolve the same conceptual knob per D-17's Claude's-Discretion note (reconcile-or-stay-independent); this plan's three surfaces (writeRuntimeCheck, mcpStatus, the new doctor check) all use the env-tunable AURA_MCP_PROBE_TIMEOUT (default 5s)."
  - "mcpStatus skips probing disabled/trust-blocked servers (Rule 2 addition, not explicitly required by the plan's Task 1 text) to prevent a read-only 'status' listing from spawning/dialing a server the operator turned off or never approved — mirrors mcpDoctorAll's own pre-existing disabled/blocked skip."
  - "TestMCPDoctorAllChecksCalendarRecipe's fixture URL moved from the literal 127.0.0.1:8093 (the calendar sidecar's REAL default AURA_PIM_MCP_PORT) to 127.0.0.1:0 (guaranteed-refused) because a live service was found actually bound to 8093 on the dev box during verification — the pre-fix test never dialed it so the collision was latent; probing for real exposed it."
  - "Test-harness fix (not a probe bug): a hung-endpoint test's httptest handler now self-bounds its own block with a fixed 3s fallback in addition to watching r.Context().Done() — client-side context cancellation does not reliably close the underlying TCP connection promptly on this dev box's Windows/net-http stack, which otherwise makes httptest.Server.Close() (not mcp.ProbeServer, independently verified to return in ~1s) hang for many minutes."

requirements-completed: [MCPH-01, MCPH-09]

coverage:
  - id: D16
    description: "writeRuntimeCheck live-dials HTTP MCP servers via mcp.ProbeServer instead of the F-046 false-healthy short-circuit; a dead/typoed endpoint reports OK=false."
    requirement: "MCPH-09"
    verification:
      - kind: unit
        ref: "cmd/aura/mcp_status_test.go#TestWriteRuntimeCheckDeadHTTPEndpointReportsNotOK"
        status: pass
      - kind: unit
        ref: "cmd/aura/mcp_status_test.go#TestWriteRuntimeCheckReachableHTTPEndpointOK"
        status: pass
      - kind: unit
        ref: "cmd/aura/mcp_status_test.go#TestWriteRuntimeCheckZeroToolsHTTPEndpointOK"
        status: pass
      - kind: regression
        ref: "cmd/aura/mcp_test.go#TestMCPDoctorAllChecksCalendarRecipe"
        status: pass
    human_judgment: false
  - id: D16b
    description: "The probe is bounded by AURA_MCP_PROBE_TIMEOUT; a hung endpoint returns within ~the configured deadline instead of blocking indefinitely."
    requirement: "MCPH-09"
    verification:
      - kind: unit
        ref: "cmd/aura/mcp_status_test.go#TestWriteRuntimeCheckBoundedByProbeTimeout"
        status: pass
      - kind: unit
        ref: "cmd/aura/doctor_test.go#TestDoctorProbeMCPServersBoundedByProbeTimeout"
        status: pass
    human_judgment: false
  - id: D17
    description: "aura mcp status additively reflects the live probe per server (mcpStatusRow) alongside SnapshotStatus's config-derived columns, without replacing them."
    requirement: "MCPH-09"
    verification:
      - kind: unit
        ref: "cmd/aura/mcp_status_test.go#TestMCPStatusReflectsLiveHTTPProbe"
        status: pass
      - kind: unit
        ref: "cmd/aura/mcp_status_test.go#TestMCPStatusSkipsProbingDisabledAndBlockedServers"
        status: pass
      - kind: regression
        ref: "cmd/aura/mcp_test.go#TestMCPStatusJSONShowsBlockedServer, TestMCPStatusShowsLifecycleWithoutPolicyColumns"
        status: pass
    human_judgment: false
  - id: D18
    description: "aura doctor gains a 6th check that live-probes only enabled+runnable+streamable-HTTP servers, skipping disabled/blocked/stdio deterministically; one dead server names itself without blocking siblings."
    requirement: "MCPH-09"
    verification:
      - kind: unit
        ref: "cmd/aura/doctor_test.go#TestDoctorChecksIncludesMCPServers"
        status: pass
      - kind: unit
        ref: "cmd/aura/doctor_test.go#TestDoctorProbeMCPServersReachable"
        status: pass
      - kind: unit
        ref: "cmd/aura/doctor_test.go#TestDoctorProbeMCPServersUnreachableNamesServer"
        status: pass
      - kind: unit
        ref: "cmd/aura/doctor_test.go#TestDoctorProbeMCPServersSkipsDisabledBlockedStdio"
        status: pass
      - kind: unit
        ref: "cmd/aura/doctor_test.go#TestDoctorProbeMCPServersNoneConfigured"
        status: pass
    human_judgment: false
  - id: D9
    description: "call-site #9 (mcp_status.go inline Type==streamable_http||URL!='' classification) migrated onto mcp.Classify."
    requirement: "MCPH-01"
    verification:
      - kind: unit
        ref: "cmd/aura/mcp_status.go writeRuntimeCheck (source inspection: mcp.Classify call, no inline Type/URL check remains)"
        status: pass
    human_judgment: false

# Metrics
duration: ~50min active work across two sessions (session interrupted by a rate-limit reset ~12:46-13:10 local, resumed with RED commit + uncommitted GREEN edits intact)
completed: 2026-07-18
status: complete
---

# Phase 38 · Plan 06 — Live HTTP MCP health check (aura doctor + aura mcp status)

**The F-046 false-healthy "http endpoint configured" short-circuit in `writeRuntimeCheck` is deleted; both `aura doctor` (new 6th check) and `aura mcp status` now live-dial HTTP MCP servers via the already-tested `mcp.ProbeServer`, bounded by `AURA_MCP_PROBE_TIMEOUT` (default 5s), so a dead/typoed endpoint reports OK=false instead of healthy-by-config.**

## Task Commits

Each task committed atomically (Task 1 is TDD; Task 2 is a single execute commit per its plan frontmatter):

1. **Task 1 — RED**: failing tests for live HTTP MCP probe wiring — `9ec5b025` (test)
2. **Task 1 — GREEN**: writeRuntimeCheck + mcp status live-probe HTTP servers — `63ce74c0` (feat)
3. **Task 2**: new 6th `aura doctor` check (D-18) — `b2466835` (feat)

## Files Created/Modified

- `cmd/aura/mcp_status.go` (194 LOC) — `writeRuntimeCheck` routes BOTH stdio and HTTP through `mcp.ProbeServer` under `context.WithTimeout(ctx, resolveMCPProbeTimeout())`; transport resolved via `mcp.Classify` (call-site #9); `mcpStatus` gained ctx parameter and an additive `mcpStatusRow{StatusSnapshot, Probe}` per server (JSON + a new `probe` text column), skipping disabled/blocked servers via `probeStatusRow`; new `resolveMCPProbeTimeout()` reads `AURA_MCP_PROBE_TIMEOUT` (seconds, default 5) via `envutil.IntDefault`.
- `cmd/aura/mcp.go` (503 LOC) — `status` subcommand call site updated to `mcpStatus(ctx, args[1:], out)`.
- `cmd/aura/mcp_status_test.go` (195 LOC, new) — dead/reachable/zero-tools/bounded-timeout tests for `writeRuntimeCheck`; live-probe reflection + disabled/blocked-skip tests for `mcpStatus`.
- `cmd/aura/mcp_test.go` (533 LOC) — `TestMCPDoctorAllChecksCalendarRecipe` rewritten from the pre-fix buggy assertion to the fixed behavior (dead-endpoint fixture moved off the real `AURA_PIM_MCP_PORT` default 8093 onto port 0); `TestMCPStatusShowsLifecycleWithoutPolicyColumns`'s stdio fixture swapped from `npx` to a deterministic-fail command so the new live-probe wiring can't hang the test on a real subprocess.
- `cmd/aura/doctor.go` (216 LOC) — new `defaultDoctorProbeMCPServers` (6th `doctorCheck{name: "mcp"}`) filtering `mcpmanager.RunnableManagedServers` to streamable-HTTP-only via `mcp.Classify`, probing each bounded by `resolveMCPProbeTimeout()`, aggregating to one pass/fail detail; `doctorProbeMCPBinary`/`defaultDoctorProbeMCPBinary` untouched.
- `cmd/aura/doctor_test.go` (449 LOC) — `installDoctorFakeProbes` extended to fake the new check (host-state hazard guard); registry, reachable/unreachable/skip/none-configured/bounded-timeout coverage for `defaultDoctorProbeMCPServers`.

## Decisions Made

See `key-decisions` frontmatter. Summary: (1) named the new timeout resolver `resolveMCPProbeTimeout` to avoid a redeclare collision with the pre-existing `serve_governance_write.go` `mcpProbeTimeout` const; (2) `mcpStatus` skips disabled/blocked servers before dialing (a safety addition beyond the plan's literal text, justified as Rule 2); (3) two test-fixture corrections were needed once probing became real (a literal AURA_PIM_MCP_PORT collision and a hang-prone httptest handler pattern) — both are test-only fixes, the production probe itself was independently verified correct and bounded.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking / naming collision] Renamed the timeout resolver to avoid redeclaring `mcpProbeTimeout`**
- **Found during:** Task 1, first `go vet` after implementing `mcp_status.go`
- **Issue:** `serve_governance_write.go` already declares `const mcpProbeTimeout = 3 * time.Second` for the governance-write post-install probe; my new function of the same name failed to compile (`redeclared in this block`).
- **Fix:** Renamed my function to `resolveMCPProbeTimeout()`. Both timeouts now coexist independently per D-17's "Claude's Discretion" note (reconcile-or-stay-independent); this plan's three surfaces (writeRuntimeCheck, mcpStatus, the new doctor check) all use the env-tunable one.
- **Files modified:** `cmd/aura/mcp_status.go`
- **Verification:** `go vet ./cmd/aura/...` clean.
- **Committed in:** `63ce74c0`

**2. [Rule 2 - Missing critical] `mcpStatus` skips probing disabled/blocked servers before dialing**
- **Found during:** Task 1, while designing `mcpStatus`'s additive probe column
- **Issue:** The plan's literal text says "ADD a live-probe column/row per server" without an explicit disabled/blocked carve-out for this surface (D-18's carve-out is stated only for `aura doctor`). Probing every server unconditionally would mean a read-only `aura mcp status` invocation could spawn/dial a stdio command the operator explicitly disabled or never trust-approved — a pre-existing regression test (`TestMCPStatusJSONShowsBlockedServer`) adds exactly such a blocked server with command `node`.
- **Fix:** `probeStatusRow` short-circuits with a `"skipped (disabled)"`/`"skipped (blocked)"` `ProbeResult` for those startup states, mirroring `mcpDoctorAll`'s own pre-existing disabled/blocked skip, before ever calling `mcp.ProbeServer`.
- **Files modified:** `cmd/aura/mcp_status.go`
- **Verification:** `cmd/aura/mcp_status_test.go#TestMCPStatusSkipsProbingDisabledAndBlockedServers`; pre-existing `TestMCPStatusJSONShowsBlockedServer` still passes without spawning `node`.
- **Committed in:** `63ce74c0`

**3. [Rule 1 - Bug in test fixture, not production code] Dead-endpoint fixture collided with a real listening service on the dev box**
- **Found during:** Task 1, first green run of `TestMCPDoctorAllChecksCalendarRecipe` after the F-046 fix went live
- **Issue:** The pre-existing test used the literal `127.0.0.1:8093` (the calendar PIM sidecar's real default `AURA_PIM_MCP_PORT`) as a "dead" endpoint — harmless under the old code (which never dialed it), but once `writeRuntimeCheck` started dialing for real, `netstat` confirmed a live service actually listening on port 8093 on this dev machine, so the probe returned `OK=true` ("runtime ok") instead of the expected "runtime missing".
- **Fix:** Moved the fixture URL to `127.0.0.1:0` (guaranteed-refused, matching `internal/mcp/probe_test.go`'s own dead-endpoint convention) so the test no longer depends on host network state.
- **Files modified:** `cmd/aura/mcp_test.go`
- **Verification:** `TestMCPDoctorAllChecksCalendarRecipe` passes deterministically.
- **Committed in:** `63ce74c0`

**4. [Rule 1 - Bug in test fixture, not production code] Hung-endpoint test's handler could block the whole suite for minutes**
- **Found during:** Task 1, verifying `TestWriteRuntimeCheckBoundedByProbeTimeout`
- **Issue:** A handler that ONLY does `<-r.Context().Done()` relies on the client's context-cancellation reliably closing the underlying TCP connection so the server sees it. On this dev box's Windows/net-http stack that propagation was observed to take far longer than the client-visible 1s cancellation (one run hit the test binary's 10-minute alarm), even though `mcp.ProbeServer` itself independently returned in ~1s in isolation (confirmed via a standalone repro). The hang was in `httptest.Server.Close()`'s teardown wait, not in the probe.
- **Fix:** The test handler now self-bounds its own block with `select { case <-r.Context().Done(): case <-time.After(3*time.Second): }` so the handler always returns within a fixed ceiling regardless of platform-specific cancellation-propagation timing. Applied to both `cmd/aura/mcp_status_test.go` and `cmd/aura/doctor_test.go`'s equivalent bounded-timeout tests.
- **Files modified:** `cmd/aura/mcp_status_test.go`, `cmd/aura/doctor_test.go`
- **Verification:** Both bounded-timeout tests now complete deterministically in ~3s; `mcp.ProbeServer`'s own ~1s bound was independently reproduced and confirmed correct before this fix (no change to production probe code).
- **Committed in:** `63ce74c0` (mcp_status_test.go), `b2466835` (doctor_test.go)

---

**Total deviations:** 4 auto-fixed (1 blocking/naming, 1 missing-critical safety addition, 2 test-fixture host-state/hang fixes)
**Impact on plan:** All four were necessary to reach a correct, deterministic green state; none touched `internal/mcp/probe.go` (reused verbatim as the plan required) or changed the feature's actual behavior beyond what the plan specified. No scope creep.

## Issues Encountered

The RED→GREEN TDD sequence for Task 1 required a genuine before/after comparison: the two production files (`cmd/aura/mcp_status.go`, `cmd/aura/mcp.go`) were reverted to HEAD via `git checkout -- <files>` (a targeted, sanctioned revert of files this task itself was actively editing — not a blanket reset) so the new/rewritten tests could be run against the pre-fix code and confirmed to fail, before reapplying the implementation for the GREEN commit. A concurrent sibling worktree agent's parallel `golangci-lint` run twice produced a stale/garbled cached result pointing at an unrelated file in a DIFFERENT worktree path (`..\agent-a9b4b8d74b723470c\...`); `golangci-lint cache clean` (a global, safe cache reset) followed by a fresh run confirmed `0 issues` and the commit proceeded — documented here as an infrastructure/concurrency artifact, not a code defect.

A mid-plan session interruption (rate-limit reset) occurred between the RED commit (`9ec5b025`) and verifying/committing GREEN; the resumed session confirmed the RED commit and uncommitted GREEN edits were intact in the worktree, diagnosed and fixed the httptest hang (deviation #4 above), then completed both tasks.

## User Setup Required

None — no external service configuration required. `AURA_MCP_PROBE_TIMEOUT` is an optional, silently-defaulted (5s) env knob; no action needed unless an operator wants to tune it.

## Next Phase Readiness

- All three cmd/aura/internal/agui call sites that need a live MCP HTTP probe now converge on `mcp.ProbeServer` (reused verbatim, unmodified): the governance read board (`handleMCPProbe`, pre-existing), governance write (`mcpWriteAdapter.probe`, pre-existing), `writeRuntimeCheck`/`mcpStatus` (this plan), and the new `aura doctor` 6th check (this plan).
- `go build ./...`, `go vet ./cmd/aura/...`, and `go test ./cmd/aura/` (full package, not just this plan's tests) all pass cleanly (~13s). `go test ./internal/mcp/...` (untouched by this plan) also verified green.
- **`-race` NOT run in this session**: this Windows dev box has `CGO_ENABLED=0` and no `gcc`/`w64devkit` on PATH, and the documented `~/.aura-toolchain.sh` BASH_ENV shim referenced in CLAUDE.md is absent here. Per the same precedent already noted in 38-01-SUMMARY.md ("`-race` re-run belongs to the phase-close full-matrix verification (WSL)"), the `-race` pass for this plan's new tests is deferred to that WSL phase-close verification pass, not skipped silently.
- No new migrations, CLI flags, or DB changes. `internal/mcp/probe.go` was read but never modified (verified via `git diff` — zero changes to that file across both commits).

---
*Phase: 38-mcp-governance-hardening*
*Completed: 2026-07-18*
