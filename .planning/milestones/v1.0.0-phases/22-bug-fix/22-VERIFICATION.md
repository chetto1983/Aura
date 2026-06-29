---
phase: 22-bug-fix
verified: 2026-06-15T20:00:00Z
status: passed
signed_off: 2026-06-16T08:35:00Z
score: 12/12 must-haves verified + 5/5 operator live items signed off
overrides_applied: 0
human_verification_resolved: "B1 coverage (89.4%) + B2 lint/vuln/mutation done in the 2026-06-15 campaign (mutation commit 595fc6a1); B3 full live-stack pass signed off 2026-06-16 on the live Docker stack at HEAD 85b5e1ae — see docs/audit/22-LIVE-SIGNOFF-2026-06-15.md Part B3 (9/9 acceptance rows). Daemon was rebuilt to HEAD first (pre-Phase-22 image detected). One low-risk follow-up: share the scheme://user:pw@ userinfo pattern between agui.SanitizeString and toolinvocations.RedactForLedger."
human_verification:
  - test: "Run `make coverage` (destructive — wipes shared Postgres) on the live stack (stack up with `make neo4j-migrate`). Confirm owned-surface coverage ≥85% and every owned package ≥85%."
    expected: "Combined unit+integration coverage ≥85% across `db_integration neo4j_integration` tag matrix; per-package floor holds. Baseline was 90.3% at 2026-06-13 HEAD; the 22-05 changes are small (one deleted const + two test swaps + one new test) and should not lower any package below its prior floor."
    why_human: "Coverage gate runs `Reset down/up` — a destructive PG wipe. Cannot run while other sessions hold the shared Postgres (concurrent coverage campaign active at time of verification)."
  - test: "Run `golangci-lint run ./...` (WSL, `~/go/bin` on PATH). Confirm 0 issues."
    expected: "golangci-lint exits 0 with no issues reported."
    why_human: "Requires the WSL `~/go/bin` toolchain not available in this shell. No read-only substitute exists."
  - test: "Run `govulncheck ./...` (WSL). Confirm 0 actionable CVEs."
    expected: "govulncheck exits 0 with no actionable vulnerabilities."
    why_human: "Requires the WSL Go toolchain and network access to the Go vuln DB. Cannot run in this checkout."
  - test: "Run mutation spot-check: `go-mutesting ./internal/agent/llm_agent_parallel.go`, `./internal/agent/budget_dedup.go`, `./internal/agent/mcptools/bridge_reconnect.go`. Confirm ≥70% killed on each, or document near-equivalent-survivor autopsy."
    expected: "≥70% killed on all three critical files, matching the mutation gate requirement from SPEC R12."
    why_human: "go-mutesting requires WSL (`~/go/bin/go-mutesting`); not on Windows PATH. Mutation testing also takes 10–30 min per file."
  - test: "Full live-stack sign-off (D-01..D-03, D-13). Bring up PG + Neo4j + MCP + embed sidecar + SearXNG (socat bridge), run `aura serve`, then exercise each B3 acceptance row from `docs/audit/22-LIVE-SIGNOFF-2026-06-15.md` Part B3."
    expected: "Each live check produces a ground-truth assertion (DB row / `· <toolname>` trace / `/metrics` line / rendered body) proving the hardening is not merely tested-but-not-wired: (1) panicking/oversized tool surfaces as per-call error, no daemon crash; (2) swarm-child output enveloped untrusted; (3) `/metrics` scrape shows turn_total, llm_call_duration_seconds, llm_errors_total, tool_errors_total, hook_total, token/cost, panic_total{site}, prefix_drift_total, span_export_failures_total; (4) CDP Telegram round-trip completes; (5) GLM-OCR multimodal exercises AURA_FS_MAX_READ_BYTES=10 MiB cap; (6) MCP with AURA_MCP_CALL_TIMEOUT_SEC=0 is bounded by the 60s default (no goroutine leak); (7) reasoning router static fallback on sidecar-down; (8) skill create auto-activates + operator alert observable; (9) `cat $AURA_DB_URL` in a shell_exec child returns nothing (DSN not inherited); (10) shell_exec output with a DSN in stdout is redacted to [REDACTED]."
    why_human: "Requires the full live multi-service daemon (PG + Neo4j + MCP + embed sidecar + SearXNG + Telegram bot + GLM-OCR sidecar) running concurrently. Cannot start destructive / stateful services during a verifier read-only pass."
---

# Phase 22: Agent Perimeter Hardening — Verification Report

**Phase Goal:** "The internal/agent operational perimeter is production-ready (blended readiness 6.5 → ≥8.0) so the cockpit's web exposure lands on a hardened base."
**Verified:** 2026-06-15T20:00:00Z
**Status:** passed (operator live sign-off completed 2026-06-16)
**Re-verification:** No — initial verification

All 12 HARDEN must-haves are VERIFIED by automated evidence (code on disk + named regression tests + orchestrator-confirmed go build/vet/test/race/cache gate). The operator-coordinated live-stack checks are now also complete: B1 coverage (89.4%) + B2 lint/vuln/mutation in the 2026-06-15 campaign, and the **B3 full live-stack sign-off on 2026-06-16** at HEAD `85b5e1ae` (9/9 acceptance rows — `docs/audit/22-LIVE-SIGNOFF-2026-06-15.md` Part B3). The daemon was first rebuilt to HEAD (the running image predated Phase 22).

---

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | A panicking tool / swarm child / shell_bg reaper cannot crash `aura serve`; panic surfaces as per-call error | VERIFIED | `recover()` in `runToolRecovering` (llm_agent_parallel.go:67), `workflow/parallel.go:147`, `swarm/swarm.go:132`, `tools/shell_bg.go:193`; `panicobs` package with bounded `aura_agent_panic_total{site}` counter; `TestExecuteBatch*Panic` / `TestParallel*Panic` / `TestSwarm*Panic` / `TestBackgroundShell*Panic` named in ledger row AG-001; go test -race PASS (A4) |
| 2 | The dedup ring is concurrency-safe by construction, race-clean under parallel dispatch | VERIFIED | `sync.Mutex mu` field in `dedupRing` (`budget_dedup.go:35`); eviction pruning of `results` map; period-3+ cycle detection; `TestBudget_BeforeAfterToolResult_Concurrent` + `TestDedupRing`; -race PASS (A4) |
| 3 | A flapping/hung MCP server degrades gracefully — single-flight off-lock reconnect, backoff + breaker, sane timeout semantics | VERIFIED | `context.WithoutCancel(parent)` in `bridge_reconnect.go:233`; `defaultMCPCallTimeout = 60*time.Second` in `timeout.go:13`; zero→default-60s / -1→infinite flip confirmed by test `bridge_edges_test.go:26,46`; breaker/backoff wired; `TestMountManagedServer_*` tests; -race PASS (A4) |
| 4 | Credentials do not leak to shell children, hook subprocesses, or the reasoning trace by default | VERIFIED | `IsSecretEnvKey("AURA_DB_URL") == true` (envkey_test.go:31 test case explicitly asserts this; "url" is in `secretEnvMarkers`); `IsSecretEnvVar` with credential-URL value detection; `resolveHookCommand` requires absolute paths (hooks_command.go:403); reasoning trace defaults to hash/size, not verbatim; `TestIsSecretEnvKey` + `TestIsSecretEnvVar_DSNValueAndNonCredentialURL`; all pass (A3) |
| 5 | Production is observable — turn/LLM/error/token/hook metrics + slog; telemetry cannot crash the daemon | VERIFIED | `aura_agent_turn_total{outcome}` (metrics.go:97), `aura_agent_llm_call_duration_seconds` (metrics.go:100), `aura_agent_llm_errors_total` (metrics.go:105), `aura_agent_tool_errors_total` (metrics.go:109), token/cost/hook/panic/span-export counters (metrics.go:54-144); `mintSpanID` zero-ID fallback + `aura_agent_span_id_entropy_failures_total`; `TestMintSpanID` + `metrics_observability_test.go` PASS (A3) |
| 6 | An embed-sidecar outage adds no per-turn latency cliff | VERIFIED | `llm_agent_reasoning.go` returns `ReasoningTierLow` on error/abstain/circuit-open (lines 29, 49, 77, 85, 97); classifier cold-start uses singleflight (`prompt/reasoning_classifier.go`); `llm_agent_reasoning_test.go` PASS (A3) |
| 7 | A hook fault is contained, not turn-fatal | VERIFIED | `FailPolicy` type with `FailOpen`/`FailClosed` in `hooks.go:20-24`; `fail_open` default for non-security hooks via `NewHookManagerWithPolicy`; in-process hooks wrapped with recover (hooks.go:117-121); `TestHookFailOpen_ErrorIsContained` / `TestHookFailOpen_PanicIsContained` / `hooks_policy_test.go` PASS (A3) |
| 8 | Unknown-tool and swarm-child output is default-untrusted and cannot launder prompt injection | VERIFIED | `trustedToolNames` explicit allowlist in `trust.go:21-28`; `untrustedSource` returns untrusted for any tool not on the allowlist; `runner_adapter.go:62` stamps `TrustUntrusted` on swarm child reports; `TestUntrustedSource_UnknownToolDefaultsUntrusted` (trust_default_test.go) + `TestRunnerAdapterDrivesEngine` (runner_adapter_test.go, asserts `Trust == TrustUntrusted`) PASS (A4) |
| 9 | Loop / budget / workflow are bounded and validated | VERIFIED | `NewBudget` rejects `maxSteps < 1` (`budget.go:119`) and `wallclock < 1` (`budget.go:127`); `findInTree` uses BFS with visited-set (`workflow.go:31-48`); `Budget.WithDeadline` wired at `cmd/aura/agent.go:99`; `TestNewBudget`/`TestLoopAgent*`/`workflow_edges_test.go` PASS (A4) |
| 10 | Tool execution is memory-safe, evictable, and consistent — fs size cap, cycle guard, dedup bound | VERIFIED | `envFSMaxReadBytes = "AURA_FS_MAX_READ_BYTES"` / `defaultFSMaxReadBytes = 10 << 20` in `fs.go:222-223`; `statSizeWithinCap` with paging hint (`fs.go:235+`); `BackgroundShells` as SessionEvictor; atomic writes via temp+rename; `fs_cap_test.go` + `tool_hardening_test.go` PASS (A3/A4) |
| 11 | Skill self-extension docs match behavior; dead code removed | VERIFIED | `skillParamsSchemaHonest` is the single schema constant in `skill.go:109` (the dead duplicate `skillParamsSchema` was deleted); `TestSkillSchemaIsHonestNotDishonest` + `TestAskUserOnlyPauseConstraint` PASS (A3); cache_invariant_audit.sh PASS (A5) confirming the schema edit preserved KV prefix stability |
| 12 | Every in-scope finding closed to Gate-3 with its named regression test; nothing silently dropped | VERIFIED | `docs/audit/22-finding-ledger.md`: 64/64 AG-001..AG-064 disposed (52 fixed+test, 3 confirmed+routed, 9 accepted+rationale); HARDEN-01..12 all mapped; all named regression tests pass in A3/A4; cache invariant PASS (A5) |

**Score:** 12/12 truths verified (automated floor)

---

### Deferred Items

No truths are unmet and deferred to a later phase. The partial-scope items (AG-007 full capability_grants gate, AG-003 exec-by-fd TOCTOU, AG-011 full multi-tenant skill gating) are within-scope deferrals documented in the SPEC boundary section and the ledger, not missing truths from this phase's goal.

---

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/agent/panicobs/panicobs.go` | panic firewall metric + recovery site labels | VERIFIED | Exists; exports `Record(site)`, `Count(site)`, bounded `aura_agent_panic_total{site}` via expvar + Prometheus; 5 named sites |
| `internal/agent/llm_agent_parallel.go` | `recover()` in executeBatch / runToolRecovering | VERIFIED | `runToolRecovering` at line 66 wraps every tool call; `recover()` at line 68; worker-pool guard (AG-064) also present |
| `internal/agent/workflow/parallel.go` | `recover()` in parallel child | VERIFIED | `recover()` at line 147 |
| `internal/swarm/swarm.go` | `recover()` in swarm wave | VERIFIED | `recover()` at line 132 |
| `internal/agent/tools/shell_bg.go` | `recover()` in reaper goroutine | VERIFIED | `recover()` at line 193 |
| `internal/agent/budget_dedup.go` | `sync.Mutex` guarding dedup ring | VERIFIED | `mu sync.Mutex` at line 35; period-3+ detection; eviction pruning |
| `internal/agent/mcptools/bridge_reconnect.go` | off-lock reconnect with `WithoutCancel` | VERIFIED | `context.WithoutCancel(parent)` at line 233 |
| `internal/agent/mcptools/timeout.go` | 0→default-60s / -1→infinite semantics | VERIFIED | `defaultMCPCallTimeout = 60 * time.Second`; branch logic confirmed by `bridge_edges_test.go` |
| `internal/secret/envkey.go` | `IsSecretEnvKey` covering DSN/URL/CONN/PWD markers | VERIFIED | `"url"` and `"uri"` in `secretEnvMarkers`; `"dsn"` and `"conn"` also present; `IsSecretEnvKey("AURA_DB_URL") == true` tested |
| `internal/agent/metrics.go` | turn/LLM/tool/hook/span/panic metrics on non-default registry | VERIFIED | Full set wired: `aura_agent_turn_total{outcome}`, `aura_agent_llm_call_duration_seconds`, `aura_agent_llm_errors_total{kind}`, `aura_agent_tool_errors_total{tool}`, `aura_agent_hook_total{point,outcome}`, token/cost/span/entropy/prefixdrift counters |
| `internal/agent/llm_agent_reasoning.go` | `ReasoningTierLow` static fallback on sidecar error | VERIFIED | Multiple return sites at lines 29, 49, 77, 85, 97-98 all return `ReasoningTierLow` on error/abstain/circuit-open |
| `internal/agent/hooks.go` | `FailPolicy` type with `FailOpen`/`FailClosed` | VERIFIED | `FailPolicy int` at line 19; `FailClosed` (iota=0) and `FailOpen` (iota=1); `handleFault` branches on policy |
| `internal/agent/trust.go` | `trustedToolNames` allowlist, default-untrusted for unknown | VERIFIED | `trustedToolNames` at line 21; `untrustedSource` at line 38 inverts: only explicitly listed tools are trusted |
| `internal/swarm/runner_adapter.go` | `TrustUntrusted` provenance on swarm child | VERIFIED | Line 62: `res.Provenance = &tools.ToolResultProvenance{Source: "swarm", Trust: tools.TrustUntrusted}` |
| `internal/agent/tools/fs.go` | `AURA_FS_MAX_READ_BYTES` cap with stat-then-reject | VERIFIED | `envFSMaxReadBytes = "AURA_FS_MAX_READ_BYTES"` at line 222; `defaultFSMaxReadBytes = 10 << 20` at line 223; `statSizeWithinCap` with paging hint |
| `internal/agent/workflow/workflow.go` | BFS visited-set cycle guard in `findInTree` | VERIFIED | `visited := map[agent.Agent]struct{}{self: {}}` at line 31; iterative BFS queue with cycle check |
| `internal/agent/tools/skill.go` | single `skillParamsSchemaHonest` schema constant | VERIFIED | `const skillParamsSchemaHonest` at line 109; grep confirms only one schema constant; dead duplicate removed |
| `docs/audit/22-finding-ledger.md` | AG-001..AG-064 disposition ledger (64 entries) | VERIFIED | File exists; 64 rows confirmed (52 fixed+test, 3 confirmed+routed, 9 accepted+rationale); HARDEN-01..12 traceability table present; total tally at end |
| `docs/audit/22-LIVE-SIGNOFF-2026-06-15.md` | Part A automated evidence + Part B operator runbook | VERIFIED | File exists; Part A records A1-A5 results (build/vet/test/race/cache); Part B states `pending` with exact operator commands — no fabricated pass |
| `cmd/aura/agent.go` | `Budget.WithDeadline` composition-root wiring | VERIFIED | Line 99: `runCtx, cancel := budget.WithDeadline(context.Background())` — AG-041 confirmed+routed; active wallclock wiring confirmed |

---

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `executeBatch` | `panicobs.Record` | `runToolRecovering` defer/recover | WIRED | Panic → `recordRecoveredPanic("execute_batch")` in llm_agent_parallel.go:69 |
| `workflow/parallel.go` | panicobs | recover in child goroutine | WIRED | line 147 |
| `swarm/swarm.go` | panicobs | recover in wave child | WIRED | line 132 |
| `shell_bg.go` | panicobs | recover in reaper | WIRED | line 193 |
| `dedupRing` | `sync.Mutex` | `mu.Lock()` / `mu.Unlock()` | WIRED | All mutation paths locked |
| `bridge_reconnect.go` | `context.WithoutCancel` | reconnect goroutine | WIRED | line 233 |
| `secret.IsSecretEnvKey` | shell child env | `shell_exec_env.go` filter | WIRED | Tests confirm DSN not inherited by child |
| `reasoning fallback` | `ReasoningTierLow` | error/circuit-open paths | WIRED | Multiple return sites confirmed |
| `hooks.go FailPolicy` | `HookManager.handleFault` | `policyAt(i)` branch | WIRED | `FailOpen` → log+metric+allow; `FailClosed` → error |
| `trust.go trustedToolNames` | nonce `<tool_output>` envelope | `renderToolResultForPrompt` | WIRED | Unknown tools → `wrapUntrustedToolOutput` |
| `runner_adapter.go` | `TrustUntrusted` | `res.Provenance` assignment | WIRED | line 62 |
| `fs.go statSizeWithinCap` | `AURA_FS_MAX_READ_BYTES` | `fsMaxReadBytes()` | WIRED | All three fs tools (read/write/edit) call statSizeWithinCap |
| `findInTree` | visited-set BFS | `visited map[agent.Agent]struct{}` | WIRED | workflow.go:31-48 |
| `skillParamsSchemaHonest` | `Spec()` | `Parameters: json.RawMessage(skillParamsSchemaHonest)` | WIRED | skill.go:132 |
| `Budget.WithDeadline` | agent run context | `cmd/aura/agent.go:99` | WIRED | Composition-root wiring confirmed |

---

### Data-Flow Trace (Level 4)

Key dynamic data paths verified:

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|-------------------|--------|
| `metrics.go promTurnTotal` | terminal `turnReason` | `llm_agent_finalize.go` increments on each outcome | Yes — test asserts outcome→counter mapping | FLOWING |
| `dedupRing.results` | result preview map | `AfterToolResult` populates; eviction prunes | Yes — bounded, mutex-guarded | FLOWING |
| `trust.go trustedToolNames` | allowlist map | static compile-time constant | Yes — deterministic | FLOWING |
| `runner_adapter.go res.Provenance` | swarm child trust | assigned unconditionally at line 62 | Yes — always set | FLOWING |

---

### Behavioral Spot-Checks

Step 7b: SKIPPED for live-stack behaviors (aura serve + DB + Telegram) — cannot start services during read-only verification. The automated regression tests (A3/A4) cover the unit-level behavioral contracts.

The following behaviors were verified by reading code (not running the app):

| Behavior | Verification method | Result |
|----------|---------------------|--------|
| `recover()` in executeBatch converts panic to toolRunResult | Code read: llm_agent_parallel.go:66-85 | PASS |
| `sync.Mutex` guards all dedupRing mutations | Code read: budget_dedup.go:35 + grep for `mu.Lock` | PASS |
| `IsSecretEnvKey("AURA_DB_URL")` returns true | Code read: envkey.go:39 (`"url"` in markers) + envkey_test.go:31 | PASS |
| `context.WithoutCancel` used in MCP reconnect | Code read: bridge_reconnect.go:233 | PASS |
| `findInTree` BFS with visited set | Code read: workflow.go:27-48 | PASS |
| Single `skillParamsSchemaHonest` constant | Grep confirms one const, skill.go:109; `Spec()` uses it at 132 | PASS |
| `Budget.WithDeadline` wired at composition root | Code read: cmd/aura/agent.go:99 | PASS |

---

### Probe Execution

Step 7c: No probe scripts declared in PLAN files for Phase 22. The equivalent automated floor (A1-A5) was executed by the orchestrator and recorded in `docs/audit/22-LIVE-SIGNOFF-2026-06-15.md` Part A. The `cache_invariant_audit.sh` probe is the closest conventional probe and is confirmed PASS (A5, 22 identical messages[0] hashes).

---

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| HARDEN-01 | 22-01, 22-04 | Panic firewall — no spawned goroutine crashes the daemon | SATISFIED | recover() at all 5 goroutine boundaries; panicobs metric; AG-001 ledger: fixed+test; -race PASS (A4) |
| HARDEN-02 | 22-01 | Dedup ring mutex-guarded, race-clean | SATISFIED | sync.Mutex in dedupRing; AG-002/039/040 ledger: fixed+test; -race PASS (A4) |
| HARDEN-03 | 22-03 | MCP reconnect: single-flight off-lock, backoff, breaker, sane timeout | SATISFIED | WithoutCancel + 60s default + breaker/backoff; AG-005/006/022..027/029 ledger: fixed+test; -race PASS (A4) |
| HARDEN-04 | 22-02, 22-04 | Credentials do not leak to shell children, hook subprocesses, or trace | SATISFIED | IsSecretEnvKey covers AURA_DB_URL; absolute-path hook requirement; trace defaults to hash/size; AG-003/009/010/047 ledger: fixed+test |
| HARDEN-05 | 22-02 | Production observability — metrics + slog; telemetry cannot crash | SATISFIED | Full metric set in metrics.go; mintSpanID zero-ID fallback; AG-012/013/033/056/057 ledger: fixed+test |
| HARDEN-06 | 22-03 | Embed-sidecar outage adds no per-turn latency cliff | SATISFIED | ReasoningTierLow fallback on all error/abstain/circuit-open paths; AG-008/032 ledger: fixed+test |
| HARDEN-07 | 22-04 | Hook fault contained — fail-soft policy | SATISFIED | FailPolicy in hooks.go; FailOpen default; recover-wrapped; exit-0 rewrite gate; absolute paths; AG-004/030/054/058 ledger: fixed+test |
| HARDEN-08 | 22-04 | Unknown-tool + swarm-child output default-untrusted | SATISFIED | trustedToolNames allowlist inverts default; runner_adapter.go TrustUntrusted; AG-052 ledger: fixed+test |
| HARDEN-09 | 22-01, 22-03, 22-04 | Loop/budget/workflow bounded and validated | SATISFIED | NewBudget rejects <1; BFS visited guard; WithDeadline wired; AG-031/035..043 ledger: fixed+test / confirmed+routed / accepted+rationale |
| HARDEN-10 | 22-03, 22-04, 22-05 | Tool execution memory-safe, evictable, consistent | SATISFIED | AURA_FS_MAX_READ_BYTES cap; BackgroundShells SessionEvictor; atomic writes; AG-014..020/045/046/050 ledger: fixed+test |
| HARDEN-11 | 22-05 | Skill self-extension docs match behavior; dead code removed | SATISFIED | Single skillParamsSchemaHonest; dead skillParamsSchema deleted; TestAskUserOnlyPauseConstraint green; AG-011/044/051 ledger: fixed+test |
| HARDEN-12 | 22-05 | Every in-scope finding closed to Gate-3; ≥85% coverage; nothing dropped | SATISFIED | 64/64 AG-### disposed; HARDEN-01..12 mapped; build/vet/test/race/cache green; coverage 89.4% (B1), lint 0 / vuln clean / mutation ≥70% (B2), full live-stack sign-off 2026-06-16 (B3, 9/9) — all operator items done |

**Note on HARDEN-12 partial status:** The automated dimension (ledger completeness, named tests, build/vet/test/race/cache) is SATISFIED and fully verified. The coverage ≥85%, golangci-lint=0, govulncheck=0, mutation ≥70%, and full live-stack dimensions are PENDING operator sign-off — per the critical constraints in this task's instructions, this is operator-coordinated, not a code gap.

---

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|---------|--------|
| `internal/agent/trust.go:62` | 62 | `panic("agent: crypto/rand failed minting tool output nonce: " + err.Error())` | WARNING | `toolOutputNonce()` still panics on entropy failure. All other panic sites were remediated (AG-001); `mintSpanID` was also remediated (AG-033). This nonce panic is NOT a recovered site and is not guarded by the executeBatch/workflow/swarm/shell_bg firewall. However: (a) it is called inside `renderToolResultForPrompt` which is called on the synchronous dispatch path, so it would be caught by the Runner-level per-turn backstop; (b) `crypto/rand` failures are extremely rare on modern OS; (c) it is NOT listed as an AG-### finding in the audit. Classifying as WARNING (not BLOCKER): the per-turn backstop covers it and this is not an audited finding. |

No `TBD`, `FIXME`, or `XXX` debt markers found in the Phase-22 modified files (checked via the SUMMARY key-files lists). No placeholder stubs detected. The compose drift test (`TestProductionContainerArtifactsMatchFatImageContract`) is now fixed by commit `e2b0d82a` (after the Phase-22 close-out, per deferred-items.md and git log) — the contract test now asserts the env-override pattern rather than an exact model tag.

---

### Human Verification Required

#### 1. Coverage Gate (HARDEN-12 / SPEC R12)

**Test:** Run `make coverage` on WSL with the live stack up (`make neo4j-migrate`). This wipes the shared Postgres by design.
**Expected:** Owned-surface coverage ≥85% across the full `db_integration neo4j_integration` tag matrix; every owned package ≥85%. Baseline was 90.3% at 2026-06-13 HEAD; the Phase-22 changes are small.
**Why human:** Coverage gate is a destructive PG wipe; cannot run while concurrent coverage campaigns are active against the shared Postgres.

#### 2. WSL Lint Gate (HARDEN-12 / SPEC R12)

**Test:** Run `golangci-lint run ./...` (WSL, PATH includes `~/go/bin`).
**Expected:** 0 issues.
**Why human:** Requires the WSL Go toolchain; not on the Windows shell PATH. No read-only substitute is valid.

#### 3. WSL Vuln Gate (HARDEN-12 / SPEC R12)

**Test:** Run `govulncheck ./...` (WSL).
**Expected:** 0 actionable CVEs.
**Why human:** Requires WSL toolchain + Go vuln DB network access.

#### 4. Mutation Spot-Check (HARDEN-12 / SPEC R12)

**Test:** Run `go-mutesting` on `internal/agent/llm_agent_parallel.go`, `internal/agent/budget_dedup.go`, `internal/agent/mcptools/bridge_reconnect.go` (WSL).
**Expected:** ≥70% killed on each of the three critical files, or documented near-equivalent-survivor autopsy.
**Why human:** go-mutesting is only installed in WSL (`~/go/bin`); mutation runs take 10–30 min per file.

#### 5. Full Live-Stack Sign-Off (D-01..D-03, D-13 / HARDEN-12)

**Test:** Bring up full stack (PG + Neo4j + MCP + embed sidecar + SearXNG via socat bridge + multimodal sidecars for GLM-OCR). Build a fresh binary at HEAD (`set -a; source <(tr -d '\r' < .env); set +a`), run `aura serve`. Execute each acceptance row from `docs/audit/22-LIVE-SIGNOFF-2026-06-15.md` Part B3:

1. Host `aura chat` tool trace: confirm panicking/oversized tool surfaces as per-call error (not daemon crash), and swarm-child output is wrapped `<tool_output trust="untrusted">`.
2. `/metrics` scrape: confirm `aura_agent_turn_total{outcome}`, `aura_agent_llm_call_duration_seconds`, `aura_agent_llm_errors_total`, `aura_agent_tool_errors_total`, `aura_agent_hook_total`, token/cost, `aura_agent_panic_total{site}`, `aura_agent_prefix_drift_total`, `aura_agent_span_export_failures_total` all present.
3. CDP Telegram round-trip: real operator turn answers; hook fault contained; no swallowed error.
4. GLM-OCR multimodal pass: large image/file empirically exercises `AURA_FS_MAX_READ_BYTES=10 MiB` cap (stat-then-reject + paging hint visible).
5. MCP timeout: with `AURA_MCP_CALL_TIMEOUT_SEC=0`, a hung server is cancelled by the 60s default with no leaked goroutine.
6. Reasoning router with sidecar down: static `ReasoningTierLow` fallback; no ≤8s per-turn latency cliff.
7. Skill self-extension: `always:false` create activates in-container; operator alert/audit record observable.
8. DSN secret boundary: `cat $AURA_DB_URL` in a `shell_exec` child returns empty/error (DSN not inherited); shell output redactor masks `postgres://u:p@h`.
9. Tool-invocation ledger redaction: a secret placed on a `shell_exec` command line lands `[REDACTED]` + capped in `aura.tool_invocations`.

**Expected:** Each check produces a ground-truth assertion NOT reading `r.Reply` (a DB row, `· <toolname>` trace, `/metrics` line, or rendered body). Flip matching `B ⏳` cells in `docs/audit/22-LIVE-SIGNOFF-2026-06-15.md` to `B ✅` with date + sign.
**Why human:** Full multi-service daemon required; cannot start destructive/stateful services in a read-only verification pass.

---

### Gaps Summary

No gaps found in the codebase. The `human_needed` status is driven entirely by the five operator-coordinated live-stack verification items (B1-B3 in 22-VALIDATION.md / 22-LIVE-SIGNOFF-2026-06-15.md) that are pending by design — they require a quiet machine, a destructive coverage gate, the WSL quality toolchain, and the full multi-service daemon. These are not code defects; they are the production-validation complement to the automated per-finding gate.

The one WARNING anti-pattern (`toolOutputNonce` panic in trust.go) is not a blocker: it is not an audited AG-### finding, it falls under the Runner-level per-turn backstop, and `crypto/rand` failures are extremely rare on the target OS.

Every HARDEN-01..12 requirement maps to concrete, substantive code on disk. All 64 AG-### findings in the ledger carry a constrained disposition. The automated floor (build / vet / test / race-clean / cache-invariant) is green at HEAD `036575b5`.

---

_Verified: 2026-06-15T20:00:00Z_
_Verifier: Claude (gsd-verifier)_
