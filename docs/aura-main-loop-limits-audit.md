# Aura Main Loop + Limits Audit

**Date:** 2026-05-15
**Scope:** `internal/agent/loop.go` + all hard limits across the agent path
**Method:** structural inventory + grep + cross-reference against PRD §5.1, §5.5
**Status:** advisory — no code changes; surfaces drift, redundancy, missing limits

---

## 1. Main Loop Structure

After Phase 4 (collapse runtime), the path is **single canonical**:

```
chat.Hub.Receive
  → chat.AgentLoop.Run (impl provided by app)
    → internal/agent/runtime.go::Run
      → internal/agent/loop.go::runLoop  ← THE LOOP
        ↳ executor.go (tool dispatch)
        ↳ governance/governance.go::Apply (microcompact + tool-result cap)
        ↳ phantom_guard.go (post-LLM heuristic)
        ↳ pool.go (permissive tool pool)
        ↳ dedupe.go (in-batch dedupe)
```

`agent.Run` is the canonical loop; `agent.RunTask` is the stateless wrapper
for one-shot callers (web chat `/api/chat`, cron background jobs). Both paths
share the same `loop.go` core — there is no duplicate loop body.

Phase-G (2026-05-15) completed the `agent.Runner` deletion (PRD D1). All
former consumers (`internal/api/web_chat`, `cmd/aura` cron adapter, swarm
adapter) now call `agent.RunTask` directly.

---

## 2. Limit Inventory

### 2.1 Loop-Level Limits (`internal/agent/loop.go`)

| Limit | Type | Default / Ceiling | Where set | Notes |
|---|---|---|---|---|
| `Options.MaxIterations` | int | clamp [1, 50] | caller (telegram/invocation_builder) | `MaxIterationsCeiling = 50` is HARD constant |
| `Options.MaxElapsed` | time.Duration | 5 min if zero | caller | `DefaultMaxElapsed = 5 * time.Minute` |
| `Options.MaxToolCalls` | int | 0 = unlimited | caller | TOTAL tool calls across all iterations |
| `Options.MaxCallsPerTool` | map[string]int | nil | caller | Per-tool budget. Empty map = unbounded |
| `Options.MaxToolResultChars` | int | 8000 (governance) | caller | Caps each tool message before LLM sees it |
| `Options.FinalizationTimeout` | time.Duration | 0 (use remaining) | caller | Wall-clock for the post-budget LLM round |
| `MaxToolResultPreviewChars` | const int | 200 | hardcoded | UI preview only |

### 2.2 Governance Limits (`internal/agent/governance/`)

| Limit | Type | Default | Notes |
|---|---|---|---|
| `MicrocompactKeepRecent` | const int | 10 | Last N tool results kept verbatim |
| `MicrocompactMinChars` | const int | 500 | Only compact tool results > this size |
| `DefaultMaxToolResultChars` | const int | 8000 | Per tool result, after compaction |

### 2.3 Conversation Context Limits (`internal/conversation/`)

| Limit | Type | Default | Notes |
|---|---|---|---|
| `Cfg.MaxMessages` | int | 50 (via config) | Sliding window cap |
| `DefaultRetrievalCapsuleMaxBytes` | const | 10 * 1024 = 10 KB | Retrieval capsule cap |

### 2.4 Phantom Guard (`internal/agent/phantom_guard.go`)

| Limit | Type | Default | Notes |
|---|---|---|---|
| `performativeWindow` | const int | 120 chars | Past-tense pattern proximity window |

### 2.5 LLM Client Limits (`internal/llm/retry.go`)

| Limit | Type | Default | Notes |
|---|---|---|---|
| `MaxRetries` | int | 5 | Transient errors |
| `BaseDelay` | time.Duration | 1 second | Exponential start |
| `MaxDelay` | time.Duration | 30 seconds | Per-attempt cap |
| `MaxContentRetries` | int | 3 | Content (parse) errors |
| `ContentTemperatures` | []float64 | [0.0, 0.3, 0.7] | Temperature escalation |
| `JitterRatio` | float64 | 0.5 | Backoff jitter |

Worst-case retry burst: 5 attempts × (1s + 2s + 4s + 8s + 16s = 31s) + jitter ≈
~45-60 seconds. Plus content-retry path adds up to 3× that for parse errors.

### 2.6 Budget (`internal/budget/`)

| Limit | Type | Default | Notes |
|---|---|---|---|
| `SoftBudget` | float64 | $10.00 | Soft warn |
| `HardBudget` | float64 | $20.00 | Hard stop |
| `CostInputPerMTokens` | float64 | $0.20 | Pricing for token-cost accounting |
| `CostOutputPerMTokens` | float64 | $0.80 | Pricing for token-cost accounting |

### 2.7 Streaming (`internal/telegram/entity_messages.go` + `channels/telegram/outbound.go`)

| Limit | Type | Default | Notes |
|---|---|---|---|
| `streamingEditThrottle` | const | 600ms | Telegram edit API rate-limit hedge |

### 2.8 Swarm / Background Agents (`internal/config/config.go`)

| Limit | Env Var | Default | Notes |
|---|---|---|---|
| `AuraBotEnabled` | `AURABOT_ENABLED` | false | Feature flag |
| `AuraBotMaxActive` | `AURABOT_MAX_ACTIVE` | 4 | Concurrent child agents |
| `AuraBotMaxDepth` | `AURABOT_MAX_DEPTH` | 1 | Recursion depth cap |
| `AuraBotTimeoutSec` | `AURABOT_TIMEOUT_SEC` | 300 (5 min) | Per-child wall clock |
| `AuraBotMaxIterations` | `AURABOT_MAX_ITERATIONS` | 5 | Per-child loop cap |

### 2.9 LLM Request Layer (`internal/config/config.go`)

| Limit | Env Var | Default | Notes |
|---|---|---|---|
| `MaxContextTokens` | `MAX_CONTEXT_TOKENS` | 4000 | ⚠️ **MISNAMED** (see §3.1) |
| `MaxHistoryMessages` | `MAX_HISTORY_MESSAGES` | 50 | Conversation window |
| `LLMMaxRetries` | `LLM_MAX_RETRIES` | 5 | LLM transient retries |

---

## 3. Findings

### 3.1 🔴 NAMING SMELL — `MaxContextTokens` is actually `MaxTokens` (output cap)

`internal/config/config.go:47`:
```go
MaxContextTokens int `envconfig:"MAX_CONTEXT_TOKENS" default:"4000"`
```

`internal/channels/telegram/invocation_builder.go:65`:
```go
MaxTokens: cfg.MaxContextTokens,
```

The name says "context tokens" (input context window), the value 4000 + usage as `MaxTokens` (LLM Request field) says it's the **output token cap**. Two different concepts collapsed into one misnamed knob.

**Impact:**
- Operators reading the env var name expect "set input context window" — but setting it changes the OUTPUT cap
- 4000 output tokens is reasonable; 4000 input context is critically low for any modern LLM (Claude 4.7 = 1M, Sonnet 4.6 = 200K+, GPT-4 = 128K)
- The actual input context window is NOT capped explicitly anywhere — Aura relies on the LLM API to error on overflow

**Fix:**
- Rename `MaxContextTokens` → `MaxOutputTokens` + env `MAX_OUTPUT_TOKENS` (with backwards-compat alias for `MAX_CONTEXT_TOKENS`)
- Add a separate `MaxInputContextTokens` knob if explicit pre-flight rejection is desired

---

### 3.2 🟡 DUPLICATION — `streamingEditThrottle = 600ms` in two files

- `internal/telegram/entity_messages.go:20`
- `internal/channels/telegram/outbound.go:30-32`

Both files duplicate the same 600ms constant. Likely a leftover from US-A17 (web_chat move) or US-B07 wrapper extraction.

**Fix:** consolidate to ONE constant exported from `channels/telegram` (since that's the channel-purity destination per the diagram). The `internal/telegram/entity_messages.go` copy stays for backward compat but imports the canonical.

Or — if both files are still in production paths — accept the duplicate but lock with a comment "MIRROR of channels/telegram/outbound.go::streamingEditThrottle. Keep in sync."

---

### 3.3 🟡 BOUNDARY UNCLEAR — `MaxToolResultChars` × `MicrocompactKeepRecent`

`DefaultMaxToolResultChars = 8000` chars per tool result. `MicrocompactKeepRecent = 10` means last 10 tool results stay verbatim.

**Worst case context bloat from tool results alone:** `8000 × 10 = 80,000 chars ≈ 20,000 tokens`.

Plus system prompt (~5K tokens), user history (50 messages × ~500 chars each = 25K chars ≈ 6K tokens), other tool message bodies (compacted, but still ~1-2K each × N).

**Total can easily exceed 30-40K tokens before LLM call.** For an 8K-context model that's broken; for 200K+ it's fine. **Aura assumes large-context LLMs without enforcing the assumption.**

**Fix options:**
1. Add explicit input-context token estimate + reject early with `BeforeLLM` callback
2. Tighten `MicrocompactKeepRecent` default to 5 (still useful but halved)
3. Tighten `DefaultMaxToolResultChars` to 4000 (cut in half)

No urgent action — current production likely runs against 200K+ context models. But a TPM-strict operator on a small model will hit invisible failures.

---

### 3.4 🟡 INCONSISTENCY — `AuraBotMaxIterations = 5` vs `MaxIterationsCeiling = 50`

Two iteration caps, 10× ratio, undocumented relationship:
- Main loop ceiling: 50
- Swarm/background ("AuraBot") child cap: 5

Reasonable that children are tighter, but:
- The naming `AuraBotMaxIterations` is confusing — "AuraBot" appears nowhere else in PRD
- Operators don't know which one applies to their Telegram chat (it's the main loop ceiling, 50, NOT AuraBotMaxIterations 5)
- Inverse asymmetry: main loop has 5min wall clock, child has 5min wall clock — BUT child has 5 iter vs main has 50

**Fix:** rename `AuraBotMaxIterations` → `SwarmChildMaxIterations` + `AURABOT_*` env → `SWARM_CHILD_*` for clarity. Document in PRD §5.17 (swarm) the iteration-vs-clock relationship.

---

### 3.5 🟡 "MISSING" LIMITS — REVISED: DON'T OVER-CONSTRAIN

Initial draft of this section proposed adding several defensive caps (MaxParallelTools=4, MaxToolCallsPerTurn=20, MaxPromptChars, etc.). **User pushback: Aura must NOT be over-limited.** The agent needs room to do powerful work (research-across-N-sources, multi-step debugging, large file inspection).

**Revised stance:**

| Proposed cap | Verdict | Reason |
|---|---|---|
| ~~MaxParallelTools = 4~~ | **REJECT** | Capping parallel dispatch slows wide-fanout workflows (10 web searches in parallel is a feature, not a bug). Mini-PC CPU concern is real but already mitigated by tool-internal semaphores (embed sidecar ≤4 threads, indexConcurrency ≤4 per memory `feedback_minipc_cpu_budget`) |
| ~~MaxToolCallsPerTurn = 20~~ | **REJECT** | LLM batching is a strength. A complex multi-file analysis legitimately needs 20+ calls. Current MaxToolCalls=0 (unlimited per run) is correct |
| ~~MaxPromptChars cap + early reject~~ | **REJECT** | False-positive rejections worse than API error. LLM API tells us when prompt is too big; we already retry/fallback via `internal/llm/retry.go` |
| ~~MaxHistoryChars enforce~~ | **REJECT** | MaxHistoryMessages=50 by count is enough discipline. Char cap would silently chop long messages, breaking continuity |
| ~~Lower MaxIterationsCeiling from 50~~ | **REJECT** | Already powerful but bounded. Consider RAISING to 100 if we see legitimate workflows pegging at 50 |
| **MaxRetrievalCapsuleAggregateBytes** | **MAYBE** | Multiple capsules per turn could accumulate. Low priority — observation-only for now |

**What CAN stay capped (current state, not changed):**

| Cap | Why it stays | Effect |
|---|---|---|
| `MaxIterationsCeiling = 50` | Bounds runaway loops | Latency/cost ceiling |
| `DefaultMaxElapsed = 5 min` | Wall-clock cap per run | Predictable latency |
| `HardBudget = $20` | Cost ceiling | Predictable spend |
| `LLM retry MaxRetries = 5` | Transient error recovery | Retry storm bounded |
| `streamingEditThrottle = 600ms` | Telegram API rate-limit | External dependency |

**Principle:** cap LATENCY and COST (wall clock, budget, retries). Don't cap CAPABILITY (parallel tools, tool count per turn, prompt size).

Aura currently has the RIGHT shape: capability-permissive, latency/cost-strict. Don't break that to fight a phantom.

---

### 3.6 🟢 GOOD — Limits that ARE well-bounded

- `MaxIterationsCeiling = 50` enforced as hard clamp → no runaway loop
- `DefaultMaxElapsed = 5 min` wall clock → no runaway run
- `streamingEditThrottle = 600ms` → no Telegram API rate-limit
- `HardBudget = $20` → no runaway cost
- `MicrocompactMinChars = 500` → small tool results untouched (no over-compaction)
- Phantom guard `performativeWindow = 120 chars` → bounded grep window
- LLM retry max ~60s burst → bounded retry storm

---

## 4. Cross-Cutting Smell — "5" appears everywhere with different meanings

- `MaxIterations` swarm child = 5
- `LLMMaxRetries` = 5
- `MaxRetries` LLM retry = 5
- `AuraBotMaxIterations` = 5

Each "5" means a different thing. None of them coordinate. An operator who lowers `AURABOT_MAX_ITERATIONS=3` does NOT lower the other 5s. If you grep for `5` in the codebase it shows up everywhere; the intent is opaque.

**Fix (cosmetic):** name each constant clearly + add godoc explaining the scope (per-turn, per-run, per-attempt, per-loop). Don't reuse "5" without a reason.

---

## 5. Comparison Against PRD §5.1 (Agent Contract)

PRD §5.1 says agent owns: loop, LLM calls, tool-call iteration, observation handling, in-run self-healing feedback, finalization, governance, context limits, run stats, interruption/deadline behavior.

| Capability | Implemented? | Where |
|---|---|---|
| Loop | ✅ | `loop.go::runLoop` |
| LLM calls | ✅ | `ChatClient` interface |
| Tool-call iteration | ✅ | `loop.go` body |
| Observation handling | 🟡 partial | `Stats` aggregation exists; no `ToolObservation` contract yet (Phase 6) |
| In-run self-healing | 🟡 partial | Dedupe, max-calls fallback exist; learning loop not yet (Phase 6) |
| Finalization | ✅ | `finalizeAnswerAfterBudget`, `TerminalHandler` |
| Governance | ✅ | `governance.Apply` |
| Context limits | 🟡 partial | MaxIterations + MaxElapsed + MaxToolCalls; missing token-window check |
| Run stats | ✅ | `Stats` struct |
| Interruption/deadline | ✅ | `CompleteOnDeadline`, ctx wiring |

`Phase 6` (Tool Experience Loop) is the explicit next step in masterplan to fill the partial gaps. Phase 5 (Consolidate Tools) doesn't add new limits but standardizes the surface.

---

## 6. Recommendations (REVISED — capability-permissive)

| Priority | Action | Effort | Risk |
|---|---|---|---|
| **HIGH** | Rename `MaxContextTokens` → `MaxOutputTokens` (with env compat) | 30 min | Low (rename + alias) — fixes naming confusion |
| **MEDIUM** | Consolidate `streamingEditThrottle` duplication | 15 min | Zero (drop dup) |
| **LOW** | Rename `AuraBot*` → `SwarmChild*` for clarity | 30 min | Low (rename + alias) |
| **CONSIDER (if pegged at 50)** | RAISE `MaxIterationsCeiling` from 50 → 100 | 15 min | Low (additive headroom) |

**EXPLICITLY NOT DOING:**
- ~~MaxParallelTools cap~~ — capability throttle, rejected
- ~~MaxToolCallsPerTurn cap~~ — capability throttle, rejected
- ~~MaxPromptChars early-reject~~ — false-positive risk, rejected
- ~~MaxHistoryChars enforcement~~ — silent content loss, rejected
- ~~Lower default iteration ceiling~~ — would constrain power, rejected

**Principle (memory-worthy):** Aura caps LATENCY and COST, not CAPABILITY. The agent must have room to do hard things; what it doesn't have is room to take forever or spend unbounded money.

**Total scope of real-changes:** ~75 min of work, all renames + dedup. No new behavior caps. Fold the HIGH + MEDIUM items into a Phase 5 (Consolidate Tools) sub-slice or a quick standalone US.

---

## 7. Files Inspected

- `internal/agent/loop.go` (665+ LOC, the core)
- `internal/agent/governance/governance.go`
- `internal/agent/phantom_guard.go`
- `internal/conversation/context.go`
- `internal/conversation/retrieval_capsule.go`
- `internal/budget/budget.go`
- `internal/llm/retry.go`
- `internal/llm/client.go`
- `internal/config/config.go`
- `internal/config/applier.go`
- `internal/telegram/entity_messages.go`
- `internal/channels/telegram/outbound.go`
- `internal/channels/telegram/invocation_builder.go`

— END AUDIT —
