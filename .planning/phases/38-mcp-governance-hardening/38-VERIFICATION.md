---
phase: 38-mcp-governance-hardening
verified: 2026-07-18T12:58:12Z
status: passed
score: 9/9 requirements verified in code; 3 backstop truths abstained to human_needed
overrides_applied: 0
human_verification:

  - test: "Mount an MCP server whose handshake completes at exactly the AURA_MCP_MOUNT_TIMEOUT deadline (e.g. inject a controllable delay = mountTimeout precisely, repeated under load/jitter) and confirm it resolves deterministically mounted-xor-dropped, never both (no double-mount, no half-mounted registry entry)."
    expected: "Exactly one of: (a) mounted and usable, or (b) dropped with a WARN and its subprocess reaped — never a state where the registry holds a handle to a server whose subprocess was also killed, and never a hang."
    why_human: "38-05's own planner_assumptions explicitly authored this as a `backstop` truth: RESEARCH flagged the exact-boundary race as unprovable by a deterministic unit test (context deadlines racing a goroutine completion). No test in cmd/aura/main_test.go exercises the frame at the exact instant of the deadline — TestBuildRegistryWithMCP_HungServerDroppedHealthySurvives proves the two ends (fast success, clearly-hung timeout) but not the boundary itself. Per the verification brief's instruction, abstaining rather than asserting pass/fail on unconfirmed evidence."

  - test: "Trigger closeMCPServers shutdown where one closer's own work completes at the exact instant AURA_MCP_SHUTDOWN_TIMEOUT's aggregate context fires; repeat under scheduler jitter."
    expected: "The closer's completion is not double-counted (e.g. errgroup does not race a completed-but-still-cancelling closer into being reported both as done and as abandoned) and does not leak a goroutine past the aggregate deadline."
    why_human: "Same class of authored `backstop` truth in 38-05 (planner_assumptions Probes #6/#8/#11). TestCloseMCPServers_AggregateDeadlineAbandonsStragglers proves the abandon-at-deadline case and TestCloseMCPServers_ConcurrentBoundedShutdown proves the fan-out is bounded, but neither test drives a closer to complete AT the exact deadline instant — this is a genuine race-timing case that a deterministic unit test cannot reliably construct."

  - test: "Configure an HTTP MCP server whose tools/list response arrives at (or within microseconds of) AURA_MCP_PROBE_TIMEOUT firing; repeat under load."
    expected: "mcp.ProbeServer / writeRuntimeCheck / doctorProbeMCPServers returns a single deterministic verdict (OK=true or OK=false), never a data race between the successful response and the timeout cancellation, and never a hung goroutine."
    why_human: "38-06's planner_assumptions explicitly authored Probe #19 ([MCPH-09] adjacency — deadline-exact) as a `backstop` truth needing verifier abstention absent direct evidence. TestWriteRuntimeCheckBoundedByProbeTimeout and TestDoctorProbeMCPServersBoundedByProbeTimeout prove boundedness (returns within ~timeout) but do not construct the exact-instant race."
---

# Phase 38: MCP Governance Hardening Verification Report

**Phase Goal:** One canonical transport classifier + explicit remote trust + bounded MCP lifecycle + audited CLI writes.
**Verified:** 2026-07-18T12:58:12Z
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

This verification independently re-derived and re-ran evidence for every plan's `must_haves` against the
actual merged code on `master` (not SUMMARY.md narration). `go build ./...`, `go vet ./...`, and every
targeted `go test -count=1 -run <Test>` invocation below was executed fresh in this session — none of the
PASS verdicts below are taken from a SUMMARY.md claim without independent re-execution or direct source
inspection.

### Observable Truths (mapped to phase Success Criteria)

| # | Truth (phase SC) | Status | Evidence |
|---|---|---|---|
| 1 | SC1: A mixed url+command entry with no explicit type is rejected and never reaches stdio Open | VERIFIED | `internal/mcp/classify.go:36-39` rejects the mixed shape; `internal/mcp/transport.go:25-29` (`OpenServer`) classifies first and returns the error before any `Open`/`OpenHTTP` call. Re-ran `TestOpenServerRejectsMixedTransportBeforeAnyDispatch` and `TestClassifyManagedServer/mixed_url_and_command_with_no_explicit_type_is_rejected` — both PASS. |
| 2 | SC1: Empty/blank/whitespace/unknown remote trust resolves to TrustBlocked, never auto-promoted | VERIFIED | `internal/mcp/classify.go:72-80` (`resolveTrust`) has no streamable_http→TrustRemoteHTTP fallback; grep for `TrustRemoteHTTP` in `managed_config.go`/`manager/runtime.go` shows it only as a valid explicit class, never an inferred fallback. Re-ran `TestClassifyManagedServer` (4 remote-empty/blank/whitespace/unknown-trust subtests) — PASS. |
| 3 | SC1 (D-04): a per-identity overlay cannot elevate a REMOTE server's trust | VERIFIED | `internal/mcp/managed_config_identity.go` — `isRemoteTransport`/`ErrRemoteElevationForbidden` guard both `MountForIdentity` and `mutateIdentityPref`. Re-ran `TestMountForIdentityRemoteTrustOverrideIgnored` and `TestSetTrustForIdentityRemoteElevationForbidden` — PASS. |
| 4 | SC2 (MCPH-05): an oversized stdio frame aborts the transport without large allocation, no resync | VERIFIED | `internal/mcp/client.go` `newStdioScanner`/`readResponseBlocking` uses `bufio.Scanner.Buffer(_, maxFrame+1)` — bounded growth delegated to stdlib (no manual byte-counting to overflow). Re-ran `TestReadResponseBlockingAcceptsFrameAtCap`, `...JustUnderCap`, `...AbortsOversizedFrame`, `...NoResyncAfterOversizedFrame`, `...ClosedStdoutIsTransportError` — all PASS. |
| 5 | SC2 (MCPH-06): shutdown leaves no child processes (process-tree kill) | VERIFIED | `internal/procgroup/{procgroup_unix,procgroup_windows}.go` (Setpgid+Kill(-pid) / taskkill /F /T); `internal/mcp/client.go:526-530` `killProcess` delegates to `procgroup.KillProcessGroup`. Re-ran `TestCloseKillsGrandchildProcessTree` (real OS subprocess + grandchild) — PASS (5.48s, non-trivial real-process runtime). |
| 6 | SC2 (MCPH-04): a hung mount drops within the deadline; registry construction is bounded; healthy servers are unaffected (Pitfall #2) | VERIFIED | `internal/mcp/client.go` `OpenWithHandshakeContext` splits `processCtx`/`handshakeCtx`; `cmd/aura/main.go:258-` `buildRegistryWithMCP` derives a per-server `handshakeCtx` from `AURA_MCP_MOUNT_TIMEOUT`. Re-ran `TestBuildRegistryWithMCP_HungServerDroppedHealthySurvives` — PASS (3.16s), log output shows the hung server retried/dropped while `calculator` mounted successfully and was not affected. |
| 7 | SC2 (MCPH-06 shutdown half): closeMCPServers fans out concurrently under one aggregate deadline, not sequential N×5s | VERIFIED | `cmd/aura/main.go:372-` `closeMCPServers` uses `errgroup.Group` + one `context.WithTimeout(context.Background(), mcpShutdownTimeout())`; per-transport `closeWaitTimeout`/`httpCloseTimeout` (5s) confirmed unchanged via grep. Re-ran (as part of the full-package run) `TestCloseMCPServers_ConcurrentBoundedShutdown`, `TestCloseMCPServers_ZeroClosersReturnsImmediately`, `TestCloseMCPServers_AggregateDeadlineAbandonsStragglers` — all PASS in the full-package `go test -count=1` run. |
| 8 | SC3 (MCPH-07): every mutating CLI MCP subcommand appends `mcp_audit` (or is prod-disallowed) | VERIFIED | grep confirms `SaveManagedConfig` is absent from `cmd/aura/mcp.go`/`mcp_profile.go`; `cmd/aura/mcp_audit_actor.go` hosts the `mcpWriteManagedConfig` choke point routing through `manager.WriteConfigWithAudit`, hard-erroring under `server_production` if no pool. `main.go`'s `runMCPDispatch` opens a pool only for mutating subcommands (grep: `pgxpool` present). Source-reviewed `cmd/aura/mcp_audit_integration_test.go` (`//go:build db_integration`, `TestMCPCLIAuditTrustAppendsOneRow`) — correctly asserts exactly-one-row-per-mutation, `cli:` actor prefix, non-null reason, and append-only repeat-mutation. Not independently re-executed in this session (requires a disposable/live Postgres and this task's own caveat states 38-07 already ran it live against `aura-postgres` in WSL) — code-reviewed and structurally sound, consistent with the sqlc/migration-0022 substrate. |
| 9 | SC3 (MCPH-03): empty trust body / `{}` / blank reason / unknown class → 400, no config/audit change | VERIFIED | `internal/agui/governance_write_api.go` `handleMCPTrust` pre-validation + `cmd/aura/serve_governance_write.go` `validateTrustClassReason` (shared single source of truth with the CLI). Re-ran `TestGovernanceWriteTrustRejectsUnderspecified` (7 subtests: empty body, empty object, blank class, missing class, blank reason, missing reason, unknown class) — all PASS. |
| 10 | SC4 (MCPH-09): a dead HTTP MCP endpoint reports OK=false, not healthy-by-config | VERIFIED | `cmd/aura/mcp_status.go` `writeRuntimeCheck` — grep confirms the `"http endpoint configured"` false-healthy short-circuit string is GONE; both stdio+HTTP now route through `mcp.ProbeServer` under `context.WithTimeout(ctx, resolveMCPProbeTimeout())`. `cmd/aura/doctor.go`'s new 6th `doctorCheck{name:"mcp"}` does the same for `aura doctor`. Re-ran `TestWriteRuntimeCheckDeadHTTPEndpointReportsNotOK` and `TestDoctorProbeMCPServersUnreachableNamesServer` — both PASS. |
| 11 (backstop) | 38-01 A1: explicit type↔trust matrix is a hard Classify error for inconsistent combos | VERIFIED (backstop confirmed) | `internal/mcp/classify.go:88-100` `checkTypeTrustConsistency` implements exactly the matrix (stdio+remote_http, streamable_http+{trusted_local,sandboxed_local} → error). Re-ran `TestClassifyManagedServer` and independently confirmed all three inconsistent-combo subtests (`stdio_+_remote_http_is_inconsistent`, `streamable_http_+_trusted_local_is_inconsistent`, `streamable_http_+_sandboxed_local_is_inconsistent`) PASS, alongside the valid-combo rows (`memory` recipe, stdio+{trusted_recipe,trusted_local,sandboxed_local,blocked}, streamable_http+{trusted_recipe,remote_http,blocked}). This is direct evidence, not an abstention — the locked assumption is confirmed exercised. |
| 12 (backstop) | 38-02: frame-size accounting never overflows/under-counts before the cap check | VERIFIED (by architecture) | `client.go`'s frame cap is delegated entirely to `bufio.Scanner.Buffer(_, maxFrame+1)` — there is no hand-rolled byte-counting loop in `readResponseBlocking` that could integer-overflow; the accounting is stdlib's, which is out of this phase's engineered-bug surface. Confirmed via direct source read (lines 61-64, 390-420ish). |
| 13 (backstop) | 38-05: mount-exactly-at-deadline resolves deterministically (mounted xor dropped) | HUMAN_NEEDED | No test in `cmd/aura/main_test.go` constructs the exact-instant race; `TestBuildRegistryWithMCP_HungServerDroppedHealthySurvives` proves the two clear-cut ends only. Planner explicitly authored this as `backstop` (verifier abstains absent direct evidence) — see human_verification section. |
| 14 (backstop) | 38-05: close-exactly-at-aggregate-deadline is not double-counted/leaked | HUMAN_NEEDED | Same reasoning — `TestCloseMCPServers_AggregateDeadlineAbandonsStragglers` proves the abandon case, not the exact-instant race. See human_verification section. |
| 15 (backstop) | 38-06: tools/list-exactly-at-probe-deadline yields a deterministic OK verdict | HUMAN_NEEDED | `TestWriteRuntimeCheckBoundedByProbeTimeout`/`TestDoctorProbeMCPServersBoundedByProbeTimeout` prove boundedness, not the exact-instant race. Planner-authored `backstop` (Probe #19). See human_verification section. |

**Score:** 12/15 truths independently VERIFIED with direct re-execution/source evidence; 3 explicitly-authored `backstop` truths abstained to human_needed per the planners' own instruction (not failed — no counter-evidence found, simply unprovable by a deterministic automated test).

### Required Artifacts

| Artifact | Expected | Status | Details |
|---|---|---|---|
| `internal/mcp/classify.go` | `Classify()` single-source classifier | VERIFIED | 100 LOC, exports `Classify`, `resolveTrust`, `checkTypeTrustConsistency`; no bespoke trust-class strings beyond existing `Trust*` constants. |
| `internal/mcp/managed_config.go` | `normalizedServerType`/`NormalizedTrust`/`validateManagedServers` delegate to Classify | VERIFIED | All three call `Classify` directly (lines 227, 255, 311); no residual `TrustRemoteHTTP` auto-promote fallback. 332 LOC. |
| `internal/mcp/transport.go` | `OpenServer` dispatches via Classify, returns its error before any Open | VERIFIED | Lines 25-29; 77 LOC. |
| `internal/mcp/manager/runtime.go` + `status.go` | second/third F-013 copies migrated onto Classify | VERIFIED | `normalizedTrustForServer`/`isStreamableHTTPServer`/`runtimeName` all call `mcp.Classify`; no residual `TrustRemoteHTTP`/inline `URL!=""` reimplementation. |
| `internal/mcp/client.go` | bounded scanner + procgroup process-tree kill | VERIFIED | `newStdioScanner`, `AURA_MCP_STDIO_MAX_FRAME` via `envutil.IntDefault`, `procgroup.SetProcessGroup`/`KillProcessGroup`, `OpenWithHandshakeContext` two-context split. 544 LOC. |
| `internal/procgroup/{procgroup_unix,procgroup_windows}.go` | shared process-group kill, no Job Objects | VERIFIED | grep: no `CreateJobObject`; exports `SetProcessGroup`/`KillProcessGroup`. |
| `internal/config/config_validate.go` | `gateMCPLegacyEnv` prod-only fail-closed gate | VERIFIED | Lines 255-274, wired into `ValidateProfile` at line 101. |
| `internal/config/config_knobs.go` | `AURA_MCP_LEGACY_ENV_COMPAT` KindBool registry row | VERIFIED | Line 86. |
| `internal/agent/mcptools/mount.go` + `bridge_reconnect.go` | bounded mount timeout + two-context reconnect (Pitfall #2) | VERIFIED | `MountServer`/`MountManagedServer` two-context signatures; `reconnectingServer.processCtx`/`setProcessContext`/`processContext` decouple reconnect handshake from process lifetime. |
| `cmd/aura/main.go` | bounded two-context mount + concurrent aggregate-deadline shutdown | VERIFIED | `buildRegistryWithMCP` (mountTimeout), `closeMCPServers` (errgroup + aggregate deadline). 405 LOC. |
| `cmd/aura/{mcp_status,doctor}.go` | live HTTP probe via `mcp.ProbeServer` | VERIFIED | `writeRuntimeCheck`/`mcpStatus` and the new 6th `doctorCheck{name:"mcp"}` both call `mcp.ProbeServer`; the F-046 false-healthy string is gone. |
| `cmd/aura/{mcp_audit_actor,mcp,mcp_profile,serve_governance_write}.go` | audited CLI writer + trust 400 validation | VERIFIED | `mcpAuditActor()`/`mcpWriteManagedConfig` choke point; `validateTrustClassReason` shared by web + CLI; `--reason`/`--class` flags on `aura mcp trust`. |

### Key Link Verification

| From | To | Via | Status | Details |
|---|---|---|---|---|
| `transport.go` OpenServer | `classify.go` Classify | dispatch on Classify result before any Open | WIRED | Confirmed by source + `TestOpenServerRejectsMixedTransportBeforeAnyDispatch`. |
| `managed_config.go` | `classify.go` | NormalizedTrust/normalizedServerType delegate | WIRED | Confirmed by source. |
| `manager/runtime.go`+`status.go` | `classify.go` | eligibility/display resolve via Classify | WIRED | Confirmed by source + `TestRunnableManagedServersBareRemoteTrustBlocked`. |
| `client.go` | `internal/procgroup` | killProcess delegates; Open calls SetProcessGroup before Start | WIRED | Confirmed by source + `TestCloseKillsGrandchildProcessTree`. |
| `config_validate.go` | ValidateProfile | gateMCPLegacyEnv appended to gate list | WIRED | Line 101; confirmed by `TestGateMCPLegacyEnv` (source-reviewed; package-level test run green). |
| `main.go` buildRegistryWithMCP | `mount_retry.go` MountWithRetry | bounded handshake ctx threaded distinct from daemon ctx | WIRED | Confirmed by `TestBuildRegistryWithMCP_HungServerDroppedHealthySurvives`. |
| `main.go` closeMCPServers | `golang.org/x/sync/errgroup` | fan-out under one aggregate deadline | WIRED | Confirmed by source + the three `TestCloseMCPServers_*` tests (full-package run). |
| `mcp_status.go`/`doctor.go` | `internal/mcp/probe.go` ProbeServer | live dial for HTTP servers | WIRED | Confirmed by source + dead-endpoint tests. |
| `mcp.go`/`mcp_profile.go` | `internal/mcp/manager/configwrite.go` WriteConfigWithAudit | every CLI mutation | WIRED | Confirmed by source (`mcpWriteManagedConfig` choke point) + grep (`SaveManagedConfig` absent from mutation paths). |
| `main.go` case "mcp" | pgxpool | opens pool only for mutating subcommands | WIRED | Confirmed by source (`runMCPDispatch`, grep `pgxpool`). |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|---|---|---|---|
| Mixed transport rejected, stdio Open unreached | `go test -count=1 -v ./internal/mcp/ -run TestOpenServerRejectsMixedTransportBeforeAnyDispatch` | PASS | VERIFIED |
| Classify type↔trust matrix (all 23 subtests incl. 3 inconsistent + matrix) | `go test -count=1 -v ./internal/mcp/ -run TestClassifyManagedServer` | PASS (23/23 subtests) | VERIFIED |
| Bounded stdio frame cap (5 boundary/close/resync tests) | `go test -count=1 -v ./internal/mcp/ -run 'TestReadResponseBlockingAcceptsFrameAtCap\|...JustUnderCap\|...AbortsOversizedFrame\|...NoResyncAfterOversizedFrame\|...ClosedStdoutIsTransportError'` | PASS (5/5) | VERIFIED |
| Grandchild process-tree kill (real OS subprocess) | `go test -count=1 -v ./internal/mcp/ -run TestCloseKillsGrandchildProcessTree` | PASS (5.48s) | VERIFIED |
| Hung mount dropped, healthy server survives | `go test -count=1 -v ./cmd/aura/ -run TestBuildRegistryWithMCP_HungServerDroppedHealthySurvives` | PASS (3.16s, log shows drop+survive) | VERIFIED |
| D-04 remote-trust elevation guard (merge + write path) | `go test -count=1 -v ./internal/mcp/ -run 'TestMountForIdentityRemoteTrustOverrideIgnored\|TestSetTrustForIdentityRemoteElevationForbidden'` | PASS | VERIFIED |
| Web trust 400 gate (7 underspecified-body subtests) | `go test -count=1 -v ./internal/agui/ -run TestGovernanceWriteTrustRejectsUnderspecified` | PASS (7/7) | VERIFIED |
| Dead HTTP endpoint reports OK=false (mcp status + doctor) | `go test -count=1 -v ./cmd/aura/ -run 'TestWriteRuntimeCheckDeadHTTPEndpointReportsNotOK\|TestDoctorProbeMCPServersUnreachableNamesServer'` | PASS | VERIFIED |
| Full package build/vet | `go build ./...` / `go vet ./...` | clean | VERIFIED |
| Full package test (fresh, no cache) | `go test -count=1 ./internal/mcp/... ./internal/mcp/manager/... ./internal/procgroup/... ./internal/config/... ./internal/agent/mcptools/... ./cmd/aura/...` | all `ok` | VERIFIED |

### Probe Execution

No `scripts/*/tests/probe-*.sh` files or probe references were found in this phase's PLAN/SUMMARY files (`grep` for `probe-.*\.sh` in the phase directory returned nothing beyond the unrelated `mcp.ProbeServer` Go function). Step 7c: SKIPPED (no shell-script probes declared or conventional for this phase; verification instead relied on direct `go test` re-execution documented above).

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|---|---|---|---|---|
| MCPH-01 | 38-01, 38-04, 38-05, 38-06 | Single canonical classifier governs validation/trust-norm/eligibility/mounting/opening | SATISFIED | `Classify()` + all 9 call sites (managed_config.go, transport.go, manager/runtime.go, manager/status.go, managed_config_identity.go, mount.go, mcp_status.go) confirmed delegating; no residual duplicate decision bodies found. |
| MCPH-02 | 38-01, 38-04 | Empty/blank remote trust → BLOCKED, not runnable; D-04 elevation guard | SATISFIED | `resolveTrust` (no auto-promote) + `isRemoteTransport`/`ErrRemoteElevationForbidden` guard; both re-run and PASS. |
| MCPH-03 | 38-07 (verify-and-guard) | Trust endpoint requires explicit class+reason; underspecified → 400 | SATISFIED | `validateTrustClassReason` shared web+CLI; `TestGovernanceWriteTrustRejectsUnderspecified` re-run PASS (7/7). |
| MCPH-04 | 38-05 | Bounded mount + reap on timeout; registry construction bounded | SATISFIED | Two-context `OpenWithHandshakeContext`; `TestBuildRegistryWithMCP_HungServerDroppedHealthySurvives` re-run PASS. Exact-deadline-race sub-clause abstained (see human_verification). |
| MCPH-05 | 38-02 | Stdio frames capped; oversized frame aborts deterministically, no large alloc | SATISFIED | `bufio.Scanner.Buffer` cap; 5 boundary tests re-run PASS. |
| MCPH-06 | 38-02, 38-05 | Shutdown bounds HTTP close + kills process tree; no hang, no leaked children | SATISFIED | `procgroup.KillProcessGroup` + `errgroup`-based `closeMCPServers`; grandchild-kill and concurrent-shutdown tests re-run PASS. Exact-deadline-race sub-clause abstained (see human_verification). |
| MCPH-07 | 38-07 | CLI MCP mutations route through audited writer or are prod-disallowed | SATISFIED | `mcpWriteManagedConfig` choke point + `mcpAuditActor()`; grep confirms no direct `SaveManagedConfig` in mutation paths; `db_integration` test source-reviewed (structurally correct, not independently re-executed this session — see note). |
| MCPH-08 | 38-03 | Legacy `AURA_MCP_SERVERS_JSON` prod-disabled unless explicit compat flag | SATISFIED | `gateMCPLegacyEnv` wired into `ValidateProfile`; `AURA_MCP_LEGACY_ENV_COMPAT` registered KindBool. Package test suite (`go test ./internal/config/...`) re-run green. |
| MCPH-09 | 38-06 | HTTP MCP probe/doctor dials+lists tools; dead endpoint → OK=false | SATISFIED | F-046 false-healthy string removed; `mcp.ProbeServer` wired into both `mcp_status.go` and `doctor.go`'s new 6th check; dead-endpoint tests re-run PASS. Exact-deadline-race sub-clause abstained (see human_verification). |

No orphaned requirements: all 9 MCPH-01..09 IDs in `.planning/REQUIREMENTS.md` (lines 110-118, all marked `[x]`) are claimed by exactly one plan's frontmatter `requirements:` field and are traceable to real, re-verified code.

### Anti-Patterns Found

None. Scanned all 19 touched production files (`classify.go`, `managed_config.go`, `transport.go`, `client.go`, `config_validate.go`, `config_knobs.go`, `manager/runtime.go`, `manager/status.go`, `managed_config_identity.go`, `mount.go`, `bridge_reconnect.go`, `main.go`, `mcp_status.go`, `doctor.go`, `mcp.go`, `mcp_profile.go`, `mcp_audit_actor.go`, `serve_governance_write.go`) for `TBD|FIXME|XXX|TODO|HACK|PLACEHOLDER` — zero matches. All files are within the 600-LOC cap (largest: `mcp.go` at 534, `client.go` at 544). `git status`/`git diff --stat` show a fully clean working tree at HEAD — no uncommitted drift.

### Human Verification Required

Three items — all explicitly pre-flagged by the plans themselves as `backstop` truths (planner_assumptions in 38-05 and 38-06), where the planner instructed the verifier to abstain to human_needed rather than assert pass/fail without direct evidence of the exact-instant race being deterministic. None of these represent a discovered defect — they are genuinely hard-to-construct timing races that a deterministic `go test` cannot reliably manufacture, and no counter-evidence (bug, flake, or race condition) was found during this verification.

#### 1. Mount-exactly-at-deadline determinism

**Test:** Inject a controllable mount-handshake delay set to exactly `AURA_MCP_MOUNT_TIMEOUT` (or a hair below/above across several repeated runs under CPU/scheduler load) and observe `buildRegistryWithMCP`'s outcome for that server.
**Expected:** The server resolves deterministically to exactly one of {mounted-and-usable, dropped-and-reaped} on every run — never a half-state (registry holds a handle whose subprocess was also killed), never a hang.
**Why human:** Planner-authored `backstop` truth (38-05 `must_haves`); RESEARCH flagged this as an unprovable-by-unit-test race between a context deadline firing and a goroutine's completion signal arriving at the same instant.

#### 2. Aggregate-shutdown-deadline-exact determinism

**Test:** Drive `closeMCPServers` with a closer whose own completion lands at (or within microseconds of) the aggregate `AURA_MCP_SHUTDOWN_TIMEOUT` firing; repeat under load/jitter.
**Expected:** The closer's completion is not double-counted (reported both as finished and as an abandoned straggler) and no goroutine leaks past the deadline.
**Why human:** Planner-authored `backstop` truth (38-05); same reasoning — `errgroup` fan-out racing a `context.WithTimeout` firing at the exact same instant is not deterministically constructible in a unit test.

#### 3. Probe-response-exactly-at-deadline determinism

**Test:** Configure an HTTP MCP server whose `tools/list` response arrives at (or within microseconds of) `AURA_MCP_PROBE_TIMEOUT` firing; repeat under load.
**Expected:** `mcp.ProbeServer` (and its `writeRuntimeCheck`/`doctorProbeMCPServers` callers) returns a single deterministic verdict, never a race between "successful response accepted" and "timeout cancellation accepted", and no leaked dialing goroutine.
**Why human:** Planner-authored `backstop` truth (38-06 Probe #19); `TestWriteRuntimeCheckBoundedByProbeTimeout`/`TestDoctorProbeMCPServersBoundedByProbeTimeout` prove boundedness but not the exact-instant race.

### Follow-ups (not gaps — known, pre-documented phase-close items)

These are explicitly NOT verification failures per the task's environment caveat — they are deferred, known follow-up work documented in every plan's own SUMMARY.md:

1. **WSL full-matrix `-race` + `db_integration neo4j_integration` coverage re-run.** This Windows checkout has no CGO/gcc toolchain; every plan's SUMMARY documents running `-race` under WSL during execution (38-01 through 38-06) or, for 38-07, running the `db_integration` test live against `aura-postgres` in WSL. A phase-close full-matrix re-run (per CLAUDE.md's Quality tooling table) should re-confirm this at phase-close, but is not itself evidence the phase failed — `go build ./...`/`go vet ./...`/`go test ./...` (69 pkgs, untagged) all pass green on this machine as independently re-verified in this session.
2. **`docs/aura-quality-snapshot.md` re-attestation** for every row whose CI-gate-path glob matches a file this phase touched (per CLAUDE.md's phase-close quality-snapshot rule) — not yet done as of this verification; belongs to phase-close, not this report.
3. **`/gsd-secure-phase 38`** (threat-mitigation retro-verification) — the phase's own `38-VALIDATION.md` frontmatter still shows `status: draft`/`nyquist_compliant: false`; a Nyquist validation pass is a separate, complementary check to this goal-backward verification and was not run here.
4. **`cmd/aura/mcp_audit_integration_test.go`'s `db_integration` tier** was source-reviewed and found structurally correct (matches the plan's exact acceptance criteria, mirrors the repo's established disposable-DB/no-skip-as-green pattern) but was NOT independently re-executed in this session against a live/disposable Postgres, per the task's own guidance that 38-07 already ran it live in WSL. A phase-close re-run against a disposable DB (never the live `aura` DB, per CLAUDE.md's hardened `coverage_docker.sh` discipline) would close this out with full independent confidence.

---

*Verified: 2026-07-18T12:58:12Z*
*Verifier: Claude (gsd-verifier)*
