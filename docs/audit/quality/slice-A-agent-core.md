# Quality Audit — Slice A: Agent Core, Loop, Tools, Runner, Swarm, LLM, CLI Entry Points

**Auditor:** Independent read-only quality audit agent  
**Date:** 2026-06-29  
**Scope:** `cmd/aura/`, `internal/agent/`, `internal/runner/`, `internal/swarm/`, `internal/scoring/`, `internal/llm/`, `internal/boundedbuffer/`, `internal/reasoningfifo/`, `internal/canonicaljson/`  
**PRD reference:** prd.md §Slices 0.9/1/1.5/1.7/1.8/3  

---

## A. Slice Summary

**Overall health: GOOD with targeted concerns.** The agent core is architecturally sound — clean interface hierarchy, well-applied no-god-class splitting, good test coverage discipline, and correct deferred-tool / canonical-args patterns. The CLAUDE.md 600 LOC cap is violated in exactly one file (`cmd/aura/serve_webui.go` at 628 LOC). The primary quality debt is concentrated in three areas: (1) two independent canonical-args implementations that diverge only by name, (2) two independent transient-error classifiers with different coverage, and (3) a `cfg.Validate()` double-call in the happy-path boot that adds dead work.

**Finding counts by severity:**

| Severity | Count |
|---|---|
| Critical | 0 |
| High | 3 |
| Medium | 5 |
| Low | 4 |
| **Total** | **12** |

---

## B. Findings Table

### QA-A-01 — Code Duplication — High

**Category:** Duplication  
**Confidence:** High  
**Evidence:** `internal/agent/workflow/loop.go:345–355` (`canonArgs`) and `internal/agent/llm_agent_args.go:66–76` (`canonicalArgs`). Both functions take a JSON string, unmarshal via `json.Unmarshal`, marshal via `canonicaljson.Marshal`, and fall back to raw bytes on error. The only difference is the function name.

```go
// loop.go:345
func canonArgs(arguments string) []byte { ... }

// llm_agent_args.go:66
func canonicalArgs(rawArgs string) []byte { ... }
```

The workflow layer (`LoopAgent.guardToolCall`) calls `canonArgs`; the LLM agent dispatch layer (`dispatch`) calls `canonicalArgs`. The implementations are functionally identical, so a bug in canonicalization (e.g., a future canonicaljson change) must be fixed in two places.

**Why it matters:** Any divergence between the two implementations would make LlmAgent's dedup fingerprint disagree with LoopAgent's fingerprint for the same tool call — causing either missed or false dedup hits depending on which layer is active.

**Recommended action:** Extract a single exported or package-internal function (e.g., `tools.CanonicalArgs` or a shared internal helper in `internal/agent/`) that both call. The workflow package already imports `internal/canonicaljson` so the refactor is import-safe.

**Effort:** S  
**Safe cleanup:** Extract the body of `canonicalArgs` to a new shared location; update both call sites. Add a test asserting both produce identical output for the same input (already implicitly covered by existing tests, but make it explicit).  
**Regression risk:** Low — pure refactor, behavior unchanged.

---

### QA-A-02 — Code Duplication — High

**Category:** Duplication  
**Confidence:** High  
**Evidence:** `internal/agent/llm_agent_retry.go:56–68` (`isTransientToolErr`) and `internal/agent/llm_agent_stream_retry.go:77–113` (`retryableStreamOpenError`). Both classify network errors as retryable. They share the `net.Error.Timeout()` + `context.DeadlineExceeded` check but diverge significantly: `retryableStreamOpenError` additionally handles `io.ErrUnexpectedEOF`, `io.EOF`, `syscall.ECONNRESET`, `syscall.ECONNREFUSED`, `syscall.ETIMEDOUT`, URL errors, HTTP 429/5xx status codes, and the string-based `retryableNetworkText` table.

**Why it matters:** A tool call that encounters `io.ErrUnexpectedEOF` (e.g., a mid-response MCP sidecar disconnect) is classified as non-transient by `isTransientToolErr` and thus NOT retried at the tool layer, while the exact same error on the LLM stream layer IS retried. This asymmetry is semantically inconsistent: both represent the same retryable transport failure. A future bug fix to one function (e.g., adding ECONNRESET) will not apply to the other.

**Recommended action:** Either (a) widen `isTransientToolErr` to include the `io.ErrUnexpectedEOF / io.EOF / syscall.*` sentinels (conservative: exclude HTTP-status-code logic which is LLM-stream-specific), or (b) introduce a shared `isTransientNetworkErr(err error) bool` primitive that both functions delegate to for the common subnet, keeping only the domain-specific parts separate.

**Effort:** S  
**Safe cleanup:** Add the shared primitive, update `isTransientToolErr` to use it. Extend existing `isTransientToolErr` test table with `io.ErrUnexpectedEOF` cases to verify parity.  
**Regression risk:** Low to Medium — widening the tool retry set could retry currently-non-retried errors; validate with existing tests before merging.

---

### QA-A-03 — Antipattern — High

**Category:** Antipattern  
**Confidence:** High  
**Evidence:** `cmd/aura/chat.go:155–200`, `bootChatEnvWithConfig`. In the non-overlay (happy-path) branch, `cfg.Validate()` is called twice:

- Line 178: inside the `else` branch before `db.Open`
- Line 197: after the `else` block, unconditionally

The second call (line 197) runs on the config returned by the second `loadConfig()` call (line 188) which re-reads env vars. That re-read can succeed only if the settings overlay (line 186) changed an env var. However, the post-overlay `cfg.Validate()` at line 197 then closes the pool on failure (`pool.Close()`) — but the pool was opened BEFORE the second `loadConfig` call. If the second config load fails validation, the pool is correctly closed. If it passes, validate is a no-op repetition.

The concern is the code path where `Validate()` at line 178 passes, the pool is opened, `loadConfig()` at line 188 returns a config that passes the unconditional `Validate()` at line 197 — but the unconditional Validate does double work. More critically, if line 178's Validate passes and line 197's Validate fails (e.g., a settings overlay injects a bad value), the pool is closed but the function returns an error without ever having called pool.Close on the overlay failure branch (lines 162–173), which goes directly to `return nil, err` without closing `pool` from line 162.

**Why it matters:** Potential pool leak on the overlay-failure-after-validate path (Confidence: Medium — requires careful path tracing). The double-validate is dead work in the common path.

**Recommended action:** Restructure `bootChatEnvWithConfig` to call `cfg.Validate()` exactly once after the final `loadConfig()` call, with a single deferred `pool.Close()` on error. Extract a `defer closePoolOnErr(pool)` guard.

**Effort:** M  
**Safe cleanup:** Introduce a `var poolToClose *pgxpool.Pool` variable and a deferred closer. Remove the early Validate call.  
**Regression risk:** Medium — touches the boot path; must be validated with integration tests.

---

### QA-A-04 — Antipattern / LOC Cap Violation — Medium

**Category:** Antipattern  
**Confidence:** High  
**Evidence:** `cmd/aura/serve_webui.go` is 628 LOC, exceeding the CLAUDE.md hard cap of 600 LOC.

**Why it matters:** CLAUDE.md §Behavioral rules: "Never create a file >600 LOC." This file currently violates that rule. Additional wiring (e.g., Phase 29+ governance write routes) will grow it further.

**Recommended action:** Split out the fallback exclusion logic and the Authula provider bootstrapping into `serve_webui_auth.go` and `serve_webui_routes.go`. The existing `serve_auth.go` already owns some auth primitives — check for related splitting opportunities.

**Effort:** M  
**Safe cleanup:** Mechanical split; no logic change. Run existing serve tests to verify.  
**Regression risk:** Low — pure reorganization.

---

### QA-A-05 — Antipattern — Medium

**Category:** Antipattern  
**Confidence:** High  
**Evidence:** `internal/agent/llm_agent_parallel.go:98–108`, `maxParallelTools()`. This function reads `AURA_LOOP_MAX_PARALLEL_TOOLS` directly from `os.Getenv` inside the hot per-batch execution path (called every time `executeBatch` runs with >1 tool). There is no caching — every parallel tool batch pays an `os.Getenv` call, which while cheap is architecturally inconsistent with the CLAUDE.md convention that env reads belong in config.Load (the budget's `NewBudget` does the same env-at-call-time reads, but those happen once per turn, not per batch).

**Why it matters:** The env read is called inside `executeBatch` (which can be called multiple times per turn). More practically: setting `AURA_LOOP_MAX_PARALLEL_TOOLS` mid-process (e.g., via a test's `t.Setenv`) can change the parallelism ceiling between batches in the same turn — this was the intended test pattern, but in production it is a hidden env-dependency inside the execution hot path. Compounds with config.Load's intent to be the single source of truth for knobs.

**Recommended action:** Add `MaxParallelTools int` to `LlmAgentConfig` (read once at construction time from the env in the composition root or via a package-level `init`-time read). Inject it into `executeBatch` via the `LlmAgent` struct field. `maxParallelTools()` becomes a one-call-per-construction helper.

**Effort:** M  
**Safe cleanup:** Non-breaking — the existing `t.Setenv` tests would need updating to set the env before construction, which is more correct anyway.  
**Regression risk:** Low — default behavior unchanged; test adjustments trivial.

---

### QA-A-06 — Test Gap — Medium

**Category:** Test Gap  
**Confidence:** High  
**Evidence:** The `truncateBytes` function lives in `internal/agent/llm_agent_finalize.go` but is also called from `internal/agent/llm_agent_completion.go` (lines 168, 206 via `truncateBytesKeepingTail`). `truncateTailBytes` (lines 209–220 of `llm_agent_completion.go`) has no dedicated test — it is only exercised via the `sideEffectDigest` path which requires a full agent setup. The `truncateBytesKeepingTail` combination logic (head+tail split with marker) also has no dedicated test.

**Why it matters:** `truncateBytesKeepingTail` feeds the critic's `completionCriticUser` digest — a malformed digest could cause a false-open or false-closed verdict. The UTF-8 boundary walking in `truncateTailBytes` is non-trivial.

**Recommended action:** Add a table-driven unit test for `truncateTailBytes` and `truncateBytesKeepingTail` analogous to the existing `TestTruncateBytes` in `llm_agent_finalize_internal_test.go`. Ensure UTF-8 boundary and the `\n...[truncated]...\n` marker path are covered.

**Effort:** S  
**Regression risk:** None (test addition only).

---

### QA-A-07 — Dead Code / Underused Export — Medium

**Category:** Dead Code  
**Confidence:** Medium  
**Evidence:** `internal/scoring/scoring.go:30–36`, `type TaskArgs struct`. The struct field `AgentTier string` is declared with the comment "only for agent_job" and is read in `taskModifierBumps` to bump the tier for `AgentTier == "reasoning"`. However, searching the call sites shows `ComputeTaskTier` is called from `cmd/aura/serve_adapters.go:43` (via `scoring.Risky` as an alert threshold — not with a TaskArgs) and in tests. No production call site constructs a `TaskArgs{AgentTier: "reasoning"}` — the field is plumbed but unset in the live `task` tool's `CreateScheduledTask` path (`serve_adapters.go:132–183`).

**Why it matters:** The `AgentTier` bump path is unreachable from production: a scheduled `agent_job` task's tier is never bumped for the reasoning tier because the tool never populates `AgentTier`. This is dead risk classification logic — the score appears higher in tests than in production.

**Note (confidence caveat):** This finding requires verification that no caller sets `AgentTier` anywhere. The grep search covers the in-slice files but cannot rule out callers added in non-slice packages not audited here.

**Recommended action:** Either (a) populate `AgentTier` from the task tool's `CreateTaskInput` when constructing `TaskArgs` at dispatch time, or (b) document explicitly that `AgentTier` is reserved for a future phase and add a `// TODO(Phase-?):` marker.

**Effort:** S  
**Regression risk:** Low.

---

### QA-A-08 — Architecture — Medium

**Category:** Architecture  
**Confidence:** Medium  
**Evidence:** `internal/agent/llm_agent_parallel.go:17` defines `envMaxParallelTools = "AURA_LOOP_MAX_PARALLEL_TOOLS"` as a private constant read via `os.Getenv`. This env var is NOT listed in `internal/llm/config.go`'s env var catalog (the AURA_LLM_* block) and is NOT indexed in `internal/config/`. It is an undocumented knob that exists only in the agent package. This breaks the convention that all `AURA_*` env vars are cataloged in config.

**Why it matters:** Operator-facing tuning knobs that are undiscoverable from the config module create invisible operational gaps. If an operator performance-tunes this variable based on memory constraints (the miniPC concern), they cannot discover it via `aura config show`.

**Recommended action:** Add `AURA_LOOP_MAX_PARALLEL_TOOLS` to the config index (PRD §Caps & Limits → Indice completo env vars) and to `config.Load`'s output (exposed as `Config.MaxParallelTools`). This naturally resolves QA-A-05.

**Effort:** M  
**Regression risk:** Low.

---

### QA-A-09 — Test Gap — Low

**Category:** Test Gap  
**Confidence:** High  
**Evidence:** `internal/agent/live_finalize_test.go` has build tag `//go:build live_finalize` and skips cleanly locally. Correct per CLAUDE.md §no-skip-as-green. However, `internal/agent/memory_recall_integration_test.go:51-53` and `internal/agent/mcptools/memory_integration_test.go:51-53` implement a manual `t.Fatal` under CI when `AURA_AGENT_MEMORY_MCP_URL` is unset — but these are not included in any CI matrix definition (they are behind `memory_integration` tag). This is correct discipline but means the memory-recall integration tier for the agent package is not exercised in CI.

**Why it matters:** The memory recall integration path (reading from the agent-memory MCP server into the LLM context) has zero CI coverage. A regression in the `mcptools.bridge` or `memory_recall_integration` path would be caught only by manual local testing.

**Recommended action:** Either (a) add the `memory_integration` matrix leg to CI with the required env vars (best), or (b) document explicitly in the phase validation matrix why this tier is excluded (already partially documented, but not in the CI matrix spec).

**Effort:** M  
**Regression risk:** None — CI change only.

---

### QA-A-10 — Dead Code — Low

**Category:** Dead Code  
**Confidence:** Medium  
**Evidence:** `internal/agent/agent.go:67-73`, `SpanID [8]byte` and `ParentSpanID *[8]byte` fields of `InvocationContext`. Per the inline comment: "minting DEFERRED to the OTel slice (WR-04) — stays [8]byte{} in Phase 2." These fields are always zero-valued in production (`rootSpanIDs()` in tracing.go mints them per-run for LlmAgent, but the IC fields themselves always start zero). The `dry-run` command explicitly notes "SpanID/ParentSpanID are intentionally left at their zero value here" (agent.go:106).

**Why it matters:** The fields are passed through the entire invocation context tree carrying no information. When OTel integration is delivered (WR-04), they will be activated. Until then, they add cognitive load without value. The emission of `"span_id":"0000000000000000"` on every Event is cosmetically confusing.

**Recommended action:** Add a `// TODO(WR-04): minting deferred` comment if not already present (it is). No code change needed until the OTel slice. Flag as a PRD-tracked deferral, not dead code to remove. **Confidence downgrade:** This is correctly-deferred code, not truly dead — marking Low and noting no action required.

**Effort:** None  
**Regression risk:** None.

---

### QA-A-11 — Antipattern — Low

**Category:** Antipattern  
**Confidence:** High  
**Evidence:** `internal/agent/llm_agent.go:229–237`, `adaptiveReasoningTier` sets `adaptiveTierSet` on the FIRST call but then builds BOTH `req = a.builder.Build(...)` and `req = a.builder.BuildWithReasoningTier(...)` when `adaptiveTierOK`. The non-reasoning Build result is discarded when `adaptiveTierOK` is true (lines 235–238):

```go
req := a.builder.Build(a.history, a.registry, a.cfg.Provider, a.cfg, budget)
if adaptiveTierOK {
    req = a.builder.BuildWithReasoningTier(...)
}
```

This allocates and builds a full `llm.Request` (including tool manifest rendering via `RenderToolDefs`) that is immediately discarded in the common case where the classifier returns a tier (which it does for every turn when `AdaptiveReasoning` is enabled and an embedder is wired).

**Why it matters:** `RenderToolDefs()` iterates the full registry and marshals tool parameters — not free. This is a per-turn allocation discarded on the hot path. Compounds with any large registry (many MCP tools).

**Recommended action:** Restructure to: `var req llm.Request; if adaptiveTierOK { req = a.builder.BuildWithReasoningTier(...) } else { req = a.builder.Build(...) }`. One call, not two.

**Effort:** S  
**Regression risk:** Low — pure optimization, same output.

---

### QA-A-12 — Dead Code — Low

**Category:** Dead Code  
**Confidence:** Medium  
**Evidence:** `cmd/aura/agent.go:89`, `dryRun` function captures `ev.RequestID = requestID` (line 127) on each event. However, `Event.RequestID` is of type `uuid.UUID`, and `InvocationContext.RequestID` is the same UUID that was set at construction. The stamping on every event is redundant: `LlmAgent.newEvent` already copies `ic.RequestID` onto the event at `llm_agent_events.go:21`. So `ev.RequestID = requestID` at line 127 of agent.go overwrites an already-correct value with the same value.

**Why it matters:** Cosmetic dead work — the assignment is harmless but misleading (comment says "stamp the shared run id on every emitted Event" suggesting the loop is doing something the agent loop has not). In test scenarios with agenttest.InfiniteToolCallAgent, the RequestID may not be set by the fake agent — confirm before removing.

**Recommended action:** Verify `agenttest.InfiniteToolCallAgent.Run` copies `ic.RequestID` to events (it does via `workflow.LoopAgent.terminalEventKind` at loop.go:292 which copies `ic.RequestID`). If confirmed, the stamp in agent.go is dead work. Remove with a comment explaining why it was there.

**Effort:** XS  
**Regression risk:** Low — but verify with `TestDryRun_CLIMaxSteps_OverridesEnv_D06`.

---

## C. High-Confidence Quick Wins

Sorted by effort-to-impact ratio:

1. **QA-A-11 (S):** Eliminate the discarded `Build()` call when `adaptiveTierOK` is true. One-liner change, zero regression risk. Saves a full `RenderToolDefs()` call per turn on the hot path.

2. **QA-A-01 (S):** Deduplicate `canonArgs` / `canonicalArgs`. Straightforward extract-and-redirect. Eliminates a latent inconsistency in dedup fingerprinting.

3. **QA-A-06 (S):** Add `truncateTailBytes` and `truncateBytesKeepingTail` unit tests. Test-only change, zero regression risk, closes a coverage gap in the critic gate.

4. **QA-A-04 (M):** Split `serve_webui.go` to comply with the 600-LOC cap. Mechanical file split, well-understood file.

5. **QA-A-02 (S):** Widen `isTransientToolErr` with the shared network sentinels. Closes a silent asymmetry between LLM-stream and tool-call retry coverage.

---

## D. Risky / Uncertain Findings

### D-1: QA-A-03 (pool leak on overlay path)

The `bootChatEnvWithConfig` control flow is complex (two `loadConfig` calls, two `cfg.Validate` calls, a settings overlay). The claimed pool leak on the overlay-failure-after-validate path requires careful path tracing. Specifically: in the overlay branch (lines 162–173), `pool` is opened from `openSettingsOverlayPool`; if `loadConfig` at line 169 returns an error, `pool.Close()` is called correctly. If `loadConfig` succeeds (line 169), then the unconditional `cfg.Validate()` at line 197 runs and could fail, closing the pool from `openSettingsOverlayPool` — which would be correct. The claim needs integration-test coverage of the settings-overlay-then-validate-fails path to confirm or deny.

**Verification needed:** Run `bootChatEnvWithConfig` with a DB config that passes initial load but a settings-overlay-injected value that fails Validate — verify no pool leak.

### D-2: QA-A-07 (AgentTier dead field)

Requires confirming that no call to `ComputeTaskTier` in the scheduler dispatch path ever sets `AgentTier != ""`. The audit checked `internal/cron/handlers/` (not in scope) and `cmd/aura/serve_adapters.go` (in scope). If `handlers.AgentJobHandler` internally constructs a `TaskArgs` with a tier, the finding is invalid. **Confidence: Medium.** Verify with a cross-package search for `TaskArgs{` before acting.

### D-3: QA-A-12 (RequestID stamp in dry-run)

The `agenttest.InfiniteToolCallAgent` delegates to `workflow.LoopAgent`, whose `terminalEventKind` copies `ic.RequestID`. But intermediate chunk/tool-call Events from the inner `LlmAgent` (not used here — the dry-run uses InfiniteToolCallAgent directly) may or may not set RequestID. Confirm by checking `agenttest.CountingAgent` and `InfiniteToolCallAgent.Run` directly.

---

## Cross-Slice Flags

The following patterns observed in Slice A likely have counterparts in other slices and should be cross-checked by the audit synthesizer:

1. **`canonArgs` / `canonicalArgs` deduplication (QA-A-01):** The canonical-args pattern is used in both `internal/agent/` and `internal/agent/workflow/`. If other slices (e.g., memory/graph in Slice 11) also fingerprint tool calls for dedup, they may have a third copy. Search for `canonicaljson.Marshal` in non-agent, non-workflow packages.

2. **`retryableNetworkText` substring table (QA-A-02):** The "connection reset", "unexpected eof" etc. string table in `llm_agent_stream_retry.go` is the ONLY place in the codebase where network error strings are classified. If Slice 5 (web tools, `internal/web/`) or Slice 13 (vLLM sidecar) also classifies retryable HTTP errors, there may be a third independent list.

3. **Double-validate pattern (QA-A-03):** `bootChatEnvWithConfig` calls `cfg.Validate()` twice. If `bootServeChatEnv` or other boot paths use a similar pattern, they may share the same structural issue. Check `serve.go:bootServe` and `serve_onboarding.go`.

4. **`maxParallelTools()` env-read-at-call-time (QA-A-05 / QA-A-08):** Several other tools (fs.go, shell_exec_env.go, shell_bg.go) also read `AURA_*` env vars at call time rather than at construction. The `AURA_FS_MAX_READ_BYTES`, `AURA_FS_WALK_NODE_CAP`, `AURA_SHELL_MAX_TIMEOUT_MS`, `AURA_SHELL_OUTPUT_BUF_CAP`, `AURA_SHELL_BACKGROUND_*` env vars all follow the same undocumented-knob-read-in-hot-path pattern. This is a cross-slice architectural concern: none of these are in the config module.

5. **`agui` import direction:** `internal/agui` imports `internal/agent` and `internal/runner` (confirmed). `internal/runner` does NOT import `internal/agui` (confirmed). `internal/agent` does NOT import `internal/agui` (confirmed). The PRD-mandated `agent ⇸ agui` boundary is clean in Slice A. However, the Telegram channel (`internal/channels/telegram/`) imports `internal/agui` — if any Slice A package imported channels, that would close the cycle. Verify in Slice B (channels/agui audit).
