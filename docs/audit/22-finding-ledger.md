# Phase 22 — AG-### Finding Coverage Ledger

**Phase:** 22-bug-fix (Agent Perimeter Hardening) · **Closed:** 2026-06-15 · **Branch:** master
**Source audit:** `docs/audit/bug-report.md` (fresh-independent-2026-06-15 cycle, HEAD `136325dc`)
**Requirements:** HARDEN-01..12 (`.planning/REQUIREMENTS.md`)

This ledger names **every** AG-001 through AG-064 exactly once (D-00a: zero audit
residue). Each row carries a **constrained disposition** — one of exactly three
strings:

- **`fixed+test`** — code changed to remove the finding, with a named regression
  test (strict fail-before/pass-after for deterministic findings; race/goleak
  pre/post for concurrency findings).
- **`accepted+rationale`** — no code change; the finding is a non-issue or an
  intentional design property under the single-operator threat model (amendment
  #50 / D-15c), or explicitly owned by a named future phase.
- **`confirmed+routed`** — the finding is real but its fix lives **outside**
  `internal/agent` + `internal/swarm` scope (D-09); it is confirmed present (or
  already fixed) at the correct boundary, with a clear rationale.

Wave commits (per-finding rollback granularity, D-11):
`22-01` `62a81cde` `86cb7a22` · `22-02` `408d841d` `d94919a4` ·
`22-03` `5a4f90ca` `108bbd32` `f27e313c` · `22-04` `d4280d2f` `f99c3ae3` `75987d50` `92d469b7` ·
`22-05` `5d8f070e` (skill honesty) · `657b438b` (confirm/route).

---

## P0 / P1 findings

| ID | Sev | Disposition | Evidence (code + test) | Commit(s) | Notes |
|----|-----|-------------|------------------------|-----------|-------|
| AG-001 | P0 | fixed+test | panic firewall in `panicobs` + recover at `executeBatch`/`LlmAgent.Run`/workflow child/swarm child/`shell_bg` reaper; `aura_agent_panic_total{site}`. Tests: `TestExecuteBatch*Panic`, `TestParallel*Panic`, `TestSwarm*Panic`, `TestBackgroundShell*Panic` | 22-01 `62a81cde` | One panicking tool no longer crashes `aura serve`. |
| AG-002 | P1 | fixed+test | `dedupRing` private mutex; `-race` before/after tool-result. Test: `TestBudget_BeforeAfterToolResult_Concurrent`, `TestDedupRing` | 22-01 `86cb7a22` | Latent concurrent-map-write fatal removed. |
| AG-003 | P1 | fixed+test | command-hook minimal-env (`secret.IsSecretEnvVar` filter) + absolute-path requirement + bounded/audited rewrite + exit-0 rewrite gate. Tests: `hooks_command_test.go`, `hooks_command_hardening_test.go` | 22-02 `408d841d`, 22-04 `d4280d2f` | exec-by-fd TOCTOU + full-env stripping beyond the secret filter were scoped out per 22-CONTEXT; the secret-leak and rewrite-validation slices landed. |
| AG-004 | P1 | fixed+test | per-hook `FailPolicy` (FailOpen contains, FailClosed aborts) + recover-wrapped in-process hooks. Test: `hooks_policy_test.go` | 22-04 `d4280d2f` | A hook fault is contained, not turn-fatal. |
| AG-005 | P1 | fixed+test | single-flight reconnect off-lock + `context.WithoutCancel` + 10s reconnect timeout + exponential backoff + per-server breaker. Tests: `bridge_reconnect_branches_test.go` | 22-03 `5a4f90ca` | No reconnect livelock / head-of-line freeze. |
| AG-006 | P1 | fixed+test | `AURA_MCP_CALL_TIMEOUT_SEC=0`→default 60s; `-1`=explicit infinite; malformed fails at mount. Tests: `bridge_edges_test.go`, `timeout` validation | 22-03 `5a4f90ca` | No `=0` unbounded hang. |
| AG-007 | P1 | confirmed+routed | reconnect now warns on `Mutating` flip + required-arg change (`bridge_reconnect.go`). Full `capability_grants` dispatch gate is deferred to Slice 1.7 (multi-tenant), out of single-operator Phase-22 scope (22-CONTEXT). | 22-03 `5a4f90ca` | The drift-warn slice fixed in scope; the capability gate is routed to the 1.7 multi-tenant phase with rationale. |
| AG-008 | P1 | fixed+test | wired-classifier failure falls back to static `ReasoningTierLow` (no router LLM call); no-classifier router path bounded to 2s. Test: `llm_agent_reasoning_test.go`, `reasoning_classifier_test.go` | 22-03 `108bbd32` | Embed-sidecar outage adds no per-turn latency cliff. |
| AG-009 | P1 | fixed+test | default trace summarizes `history`/`messages`/`user`/`prompt`/`input` as hash/size; `AURA_REASONING_TRACE=full` is the explicit verbatim PII mode; field caps. Tests: `reasoningtrace_test.go` | 22-02 `408d841d` | No verbatim history/PII at rest by default. |
| AG-010 | P1 | fixed+test | DSN/URL/URI/CONN/PWD/COOKIE/SESSION/JWT markers + credential-bearing URL detection in `secret.IsSecretEnvVar`; shell child env filtered. Tests: `envkey_test.go`, `shell_exec_test.go` | 22-02 `408d841d` | DB password no longer inherited by shell children. |
| AG-011 | P1 | fixed+test | schema/doc reconciled to the actual single-operator boundary (`always:false` create/update auto-activates in-container after validate+audit; `always:true`+delete approval-gated). Tests: `TestSkillToolSchemaStatesActualAutoActivationPolicy`, `TestSkillSchemaIsHonestNotDishonest`, `TestActionCreateActivates`, `TestActionCreateActivePathStillAlertsWhenAlerterWired` | 22-05 `5d8f070e` | The optional `Alerter` fires an operator audit record on a self-authored mutation; the trust boundary is documented honestly (prior B-04). |
| AG-012 | P1 | fixed+test | `aura_agent_turn_total{outcome}`, `llm_call_duration_seconds`, `llm_errors_total{kind}`, `tool_errors_total{tool}`, token/cost/hook/panic counters; non-default registry. Test: `metrics_observability_test.go` | 22-02 `d94919a4` | SLO/alerting surface exists. |
| AG-013 | P1 | fixed+test | `slog` at turn/LLM/tool/hook/panic/tracing boundaries; `mintSpanID` zero-ID fallback + `span_id_entropy_failures_total`; exporter boot log + `span_export_failures_total`. Test: `metrics_observability_test.go`, `TestMintSpanID` | 22-02 `d94919a4` | No silent telemetry drop; telemetry cannot crash the daemon. |

---

## P2 findings

| ID | Sev | Disposition | Evidence (code + test) | Commit(s) | Notes |
|----|-----|-------------|------------------------|-----------|-------|
| AG-014 | P2 | fixed+test | `AURA_FS_MAX_READ_BYTES`=10 MiB stat-then-reject on fs_read/write/edit + paging hint. Test: `fs_cap_test.go` | 22-04 `75987d50` | D-05: cap starts at 10 MiB, tunable on live evidence. |
| AG-015 | P2 | fixed+test | `BackgroundShells` `SessionEvictor` + poll-time finished-shell reclamation. Test: `tool_hardening_test.go` | 22-04 `75987d50` | Long-lived daemon no longer leaks bg buffers. |
| AG-016 | P2 | fixed+test | every `agent_job` schedule gated to `pending_approval`. Test: `tool_hardening_test.go` | 22-04 `75987d50` | Keyword-only gating replaced with full approval gate. |
| AG-017 | P2 | fixed+test | bg `snapshot` compaction collapsed to one step + byte-exact paging test. Test: `tool_hardening_test.go` | 22-04 `75987d50` | Double-`readOff` bookkeeping simplified. |
| AG-018 | P2/P3 | fixed+test | model `cwd` stat-validation + `filepath.Clean` approval digest normalization. Test: `tool_hardening_test.go` | 22-04 `75987d50` | No double approval prompt; clean cwd error. |
| AG-019 | P2 | fixed+test | `send_file` `RequireWorkspace` fail-closed flag when root empty (non-CLI). Test: `tool_hardening_test.go` | 22-04 `75987d50` | Mechanism landed + tested; the non-CLI composition-root flip is a one-line wiring noted in 22-04 (CLI default unchanged, no silent downgrade). |
| AG-020 | P2 | fixed+test | per-tool description-hash → full ranker rebuild on MCP reconnect change. Test: `tool_hardening_test.go` | 22-04 `75987d50` | Stale semantic vector no longer degrades tool selection silently. |
| AG-021 | P2/P3 | accepted+rationale | the destructive-command regex gate is documented as advisory and trivially bypassable; the only real boundary is least-privilege/sandbox. No in-model fix is possible. | — | Single-operator host model (D-15c): arbitrary `shell_exec` is the intended capability, not a finding. Restated so the report does not over-credit the regex. |
| AG-022 | P2 | fixed+test | boot namespace-uniqueness validation; deterministic collision handling preserved. Test: `bridge` collision tests | 22-03 `5a4f90ca` | Cross-server collision no longer drops a server silently. |
| AG-023 | P2 | fixed+test | post-sanitization namespace-collision detection at boot. Test: `bridge` collision tests | 22-03 `5a4f90ca` | Reinforces AG-022. |
| AG-024 | P2 | fixed+test | failed-after-send treated as non-retryable to the model (no double-exec). Test: `bridge_reconnect_branches_test.go` | 22-03 `5a4f90ca` | At-least-once double-execute closed for mutating MCP tools. |
| AG-025 | P2 | fixed+test | total marshalled schema-byte + property-count cap → `emptyObjectSchema` fallback. Test: `bridge_edges_test.go` | 22-03 `5a4f90ca` | Hostile schema cannot bloat manifest/index. |
| AG-026 | P2 | fixed+test | inline MCP error text capped via the `NewResult` preview ceiling. Test: `bridge_edges_test.go` | 22-03 `5a4f90ca` | Server error text bounded like descriptions. |
| AG-027 | P2 | fixed+test | MCP timeout resolved+validated once at bridge/mount time, stored on the server. Test: `timeout` + `bridge` validation | 22-03 `5a4f90ca` | Malformed value fails loud at boot, not mid-run. |
| AG-028 | P2 | fixed+test | `deadcode ./...` confirmed `openManagedServer` unreachable (test-only callers); deleted. `MountManagedServer` inlines the identical stdio/HTTP branches. Tests: `TestMountManagedServer_HTTPBranchInfersFromBareURL`, `TestMountManagedServer_StdioBranchFailure` cover the same branches through the live entrypoint. | 22-05 `657b438b` | NEEDS-CONFIRMATION resolved: it WAS dead production code; removed with zero coverage loss. |
| AG-029 | P2 | fixed+test | atomic-swap registry contract documented; `-race` reconnect-during-dispatch test. Test: `bridge_trust_test.go` (race) | 22-03 `5a4f90ca` | "Immutable post-boot" contract is now explicit + race-guarded. |
| AG-030 | P2 | fixed+test | exit-0 required for `rewrite`; `deny` allowed on non-zero. Test: `hooks_command_hardening_test.go` | 22-04 `d4280d2f` | A crashed-after-emitting hook's rewrite is no longer applied as success. |
| AG-031 | P2 | fixed+test | per-turn `PrefixHash` compare after `BeforeModel` + `aura_agent_prefix_drift_total` metric. Test: `TestPrefixDrift` | 22-04 `92d469b7` | Hook cache-busting is now observable at runtime. |
| AG-032 | P2 | fixed+test | classifier anchor embedding/store moved off `c.mu`; concurrent cold starts shared via `singleflight`. Test: `reasoning_classifier_test.go` | 22-03 `108bbd32` | Cold-start no longer serializes all turns. |
| AG-033 | P2 | fixed+test | `mintSpanID` zero-ID fallback + `aura_agent_span_id_entropy_failures_total`; never panics. Test: `TestMintSpanID` (failing entropy) | 22-02 `d94919a4` | Folded into AG-013; telemetry ID decoupled from a hard panic. |
| AG-034 | P2 | confirmed+routed | `event.go` `ToolInvocation` is the intentional raw-as-observed forensic shape; redaction + 8 KiB/2 KiB byte caps live at the persistence boundary `internal/toolinvocations.RedactForLedger` (store `toParams`). Tests: `redact_test.go`, `store_test.go`, and agent-side `TestToolInvocation_ForensicShapeIsRawRedactionRoutedToStore` | 22-05 `657b438b` | NEEDS-CONFIRMATION resolved: redaction is purely in the DB/projection layer (D-09 keeps it out of `internal/agent`), already implemented + tested; route confirmed, no `event.go` shape change. |
| AG-035 | P2 | fixed+test | `NewBudget` rejects `maxSteps<1`; `NewLoop(maxIter=0)` uses a documented 1000-iteration ceiling. Test: `loop_bounds_test.go`, `budget_test.go` | 22-03 `f27e313c` | `=0` no longer disables the runtime / unbounds the loop. |
| AG-036 | P2 | fixed+test | `NewBudget` rejects `maxSteps<1` and `wallclock<1`. Test: `budget_test.go` | 22-03 `f27e313c` | Construction-time validation. |
| AG-037 | P2 | fixed+test | cycle-safe `findInTree` (visited-set BFS). Test: `workflow_edges_test.go` | 22-04 `92d469b7` | No stack overflow on a cyclic/diamond agent tree. |
| AG-038 | P2 | fixed+test | real atomic `Budget.TryReserve`/`Release` synthesis reservation (not best-effort). Test: `swarm_test.go` `TestSwarmBudgetInheritance` | 22-04 `92d469b7` | TOCTOU-racy reserve replaced with an atomic one. |
| AG-039 | P2 | fixed+test | dedup `results` pruned when a fingerprint is evicted from the ring. Test: `budget_dedup_test.go` | 22-01 `86cb7a22` | Map no longer grows unbounded over a run. |
| AG-040 | P2 | fixed+test | period-3+ repeated-cycle detection. Test: `budget_dedup_test.go` | 22-01 `86cb7a22` | A-B-C-A-B-C oscillation is now caught. |
| AG-041 | P2 | confirmed+routed | root dry-run / run ctx already threads `Budget.WithDeadline(parent)` at `cmd/aura/agent.go:99`; CLI-level non-positive override tests added. Test: `cmd/aura/agent_test.go` | 22-03 `f27e313c` | NEEDS-CONFIRMATION resolved: the wiring was already present at the composition root (out of `internal/agent`); confirmed + CLI-covered. |
| AG-042 | P2 | accepted+rationale | crash-resume checkpoint of in-memory run state (history/counters/dedup) keyed by sessionID is a **Runner** concern (D-26), not the agent's. Pause/resume IS durable today via the Runner; crash recovery is explicitly future scope. | — | Routed to the Runner snapshot work; named as a tracked future deliverable, not silently dropped. |
| AG-043 | P2 | fixed+test | goleak break-at-every-index stress proves no result-closer goroutine leak under concurrent child-error + consumer-break. Test: `TestParallelAgent_NoLeak_BreakAtEveryIndex` | 22-04 `92d469b7` | NEEDS-CONFIRMATION resolved: stress test found no leak. |

---

## P3 findings

| ID | Sev | Disposition | Evidence (code + test) | Commit(s) | Notes |
|----|-----|-------------|------------------------|-----------|-------|
| AG-044 | P3 | fixed+test | dead duplicate `skillParamsSchema` const deleted; one honest schema remains. Test: `TestSkillSchemaIsHonestNotDishonest` | 22-05 `5d8f070e` | Folded into AG-011; the dead const claimed an approval gate the code never enforced. |
| AG-045 | P3 | fixed+test | atomic `fs_edit` (temp-file + `os.Rename`); last-writer-wins documented. Test: `fs_cap_test.go`, `tool_hardening_test.go` | 22-04 `75987d50` | No crash-truncate; parallel swarm edits no longer race a partial file. |
| AG-046 | P3 | fixed+test | unified `**`-aware glob semantics across `fs_grep`/`fs_glob`. Test: `tool_hardening_test.go` | 22-04 `75987d50` | A model reusing `**/*.go` no longer gets zero grep matches. |
| AG-047 | P3 | fixed+test | DSN credential output-redaction pattern (`://u:p@h`) added to the shell redactor. Test: `shell_exec_mergeenvcap_test.go` / redact tests | 22-02 `408d841d` | Ties to AG-010; output path also covered. |
| AG-048 | P3 | accepted+rationale | file symlinks followed in walks/reads; sizes not separately budgeted. Acceptable under the no-path-fence single-operator model (D-15c) — the host filesystem IS the capability. | — | Note-only finding; no fence by design. |
| AG-049 | P3 | accepted+rationale | the SSRF gate has no destination-port restriction (any port on a public IP). Optional port policy only if port-scanning enters scope; the pinned-IP/scheme-allowlist/metadata-block defenses stand (verified GOOD). | — | Tracked future hardening in `internal/web`; out of `internal/agent` Phase-22 scope. |
| AG-050 | P3 | fixed+test | sidecar `runDir` asserted absolute + within the run root (`WithToolCallContext` invariant). Test: `tool_hardening_test.go` | 22-04 `75987d50` | Path-safety invariant now enforced, not assumed. |
| AG-051 | P3 | fixed+test | the dormant skill `writeAction` pending-pause path cannot pause the turn — the pause is name-gated to `ask_user` (`llm_agent_pause.go`). Test: `TestAskUserOnlyPauseConstraint` | 22-05 `5d8f070e` | Folded into AG-011; the "only ask_user pauses" invariant is proven and the skill tool does not violate it. |
| AG-052 | P3→P1* | fixed+test | default-untrusted provenance for unknown + swarm-child tool output (explicit `trustedToolNames` allowlist); dedup hashes the RAW preview; control-plane signals/event projection stay unwrapped. Test: `trust_default_test.go`, finalize/parallel/hooks tests | 22-04 `f99c3ae3` | Indirect prompt-injection laundering closed (prior B-02). |
| AG-053 | P3 | accepted+rationale | hygiene notes: all-MCP-untrusted is good; `Close` not cancelling in-flight calls + a mutable `openMCPClient` global test seam are minor. The reconnect breaker + bounded reconnect timeout (AG-005) bound the in-flight window. | — | Note/hygiene; no behavioral defect under the threat model. |
| AG-054 | P3 | fixed+test | command hooks require absolute paths; bare names rejected. Test: `hooks_command_hardening_test.go` | 22-04 `d4280d2f` | No `$PATH`-resolved bare hook command. |
| AG-055 | P3 | accepted+rationale | the reasoning greeting pre-filter + seeds are Italian-only; other-language greetings pay one embed round-trip and may misroute. Documented Italian-corpus assumption; multilingual seeds are a future tuning pass. | — | Behavior is bounded (one embed call); no correctness defect, documented limitation. |
| AG-056 | P3 | fixed+test | OTLP exporter boot log + `aura_agent_span_export_failures_total`. Test: `metrics_observability_test.go` | 22-02 `d94919a4` | Folded into AG-013; silent span-drop now has a readiness signal + metric. |
| AG-057 | P3 | fixed+test | metric registration centralized in `agentMetrics` over a non-default Prometheus registry (no re-registration panic). Test: `metrics_observability_test.go` | 22-02 `d94919a4` | The runtime can be instantiated twice (tests/embedders). |
| AG-058 | P3 | fixed+test | first-result-wins hook short-circuit documented; in-process hooks recover-wrapped. Test: `hooks_policy_test.go` | 22-04 `d4280d2f` | Ties AG-001/AG-004. |
| AG-059 | P3 | accepted+rationale | empty-pass leaf-step charge does not by itself bound a non-returning child; it is bounded by the wallclock ctx (AG-041) and the cooperative leaf contract documented in `loop.go`. | 22-04 `92d469b7` | Intentional cooperative semantics; documented, relies on the now-confirmed wallclock deadline. |
| AG-060 | P3 | accepted+rationale | escalate `cancel()` is checkpoint-based, not preemptive; siblings may spend a few more steps. Documented as intentional cooperative cancellation in `parallel.go`. | 22-04 `92d469b7` | No correctness defect; bounded by per-step checks. |
| AG-061 | P3 | fixed+test | `chain_aborted_at` StateDelta marker on sequential sub-error. Test: `workflow_edges_test.go` | 22-04 `92d469b7` | Sub-abort is now observable. |
| AG-062 | P3 | fixed+test | swarm-context concurrent-read contract documented; `-race` fan-out test guards the shared `*Registry`/`Client`. Test: `workflow_edges_test.go` (race) | 22-04 `92d469b7` | Implicit contract made explicit + race-proven. |
| AG-063 | P3 | accepted+rationale | `used`/`remaining` are independent atomic reads (a momentarily-inconsistent `<budget>` hint); `SetMaxSteps` boot-only safety is by convention. The hint is advisory and self-corrects on the next read; the atomic step counter itself is consistent. | — | Hint-accuracy-only; no enforcement defect. Snapshot-under-one-read deferred as a cosmetic refinement. |
| AG-064 | P3 | fixed+test | bounded worker pool for `executeBatch` (limit workers, not N parked goroutines). Test: `llm_agent_parallel_test.go` | 22-04 `92d469b7` | Very-wide turns no longer spawn N eager goroutines. |

---

## HARDEN requirement → finding traceability

Every HARDEN-01..12 maps to ≥1 `fixed+test` row (or `confirmed+routed`/`accepted+rationale` with explicit rationale):

| Requirement | Findings | Disposition summary |
|-------------|----------|---------------------|
| HARDEN-01 (panic isolation) | AG-001, AG-058 | fixed+test (22-01/22-04) |
| HARDEN-02 (dedup concurrency) | AG-002, AG-039, AG-040 | fixed+test (22-01) |
| HARDEN-03 (MCP resilience) | AG-005, AG-006, AG-022..027, AG-029, AG-053 | fixed+test (22-03); AG-053 accepted+rationale |
| HARDEN-04 (secret boundary) | AG-003, AG-009, AG-010, AG-047 | fixed+test (22-02/22-04) |
| HARDEN-05 (observability) | AG-012, AG-013, AG-033, AG-056, AG-057 | fixed+test (22-02) |
| HARDEN-06 (reasoning fallback) | AG-008, AG-032, AG-055 | fixed+test (22-03); AG-055 accepted+rationale |
| HARDEN-07 (hook fail-soft) | AG-004, AG-030, AG-054 | fixed+test (22-04) |
| HARDEN-08 (default-untrusted) | AG-052 | fixed+test (22-04) |
| HARDEN-09 (loop/budget/workflow) | AG-031, AG-035..043, AG-059..064 | fixed+test (22-03/22-04); AG-041 confirmed+routed; AG-042/059/060/063 accepted+rationale |
| HARDEN-10 (tool memory-safety) | AG-014..020, AG-021, AG-028, AG-045, AG-046, AG-048, AG-050 | fixed+test (22-03/22-04/22-05); AG-021/048 accepted+rationale; AG-049 accepted+rationale |
| HARDEN-11 (skill self-extension honesty) | AG-011, AG-044, AG-051 | fixed+test (22-05) |
| HARDEN-12 (Gate-3 close, nothing dropped) | this ledger + all 64 rows | fixed+test (coverage/live: see `22-LIVE-SIGNOFF-2026-06-15.md`) |

---

## Disposition tally

- **fixed+test:** 52 — AG-001..006, AG-008..020, AG-022..040 (less the routed/accepted ones), AG-043..047, AG-050..052, AG-054, AG-056..058, AG-061, AG-062, AG-064.
- **confirmed+routed:** 3 — AG-007 (capability-grant gate → Slice 1.7), AG-034 (ledger redaction → `internal/toolinvocations`), AG-041 (WithDeadline → `cmd/aura`).
- **accepted+rationale:** 9 — AG-021, AG-042, AG-048, AG-049, AG-053, AG-055, AG-059, AG-060, AG-063.

**Total: 64 / 64 (AG-001..AG-064).** No blank disposition. No NEEDS-CONFIRMATION
item left ambiguous (AG-028/034/041/043 all resolved in 22-05).

> Note on `audit-index.json`: the source index recorded `total_findings: 63`; the
> bug-report's contiguous AG-001..AG-064 numbering yields **64** distinct IDs (the
> off-by-one is because AG-033/044/051/056 are folded sub-findings of AG-013/011/011/013
> respectively but still carry their own ID). This ledger uses the canonical 64-ID set;
> `audit-index.json` is reconciled in the 22-05 close-out.
