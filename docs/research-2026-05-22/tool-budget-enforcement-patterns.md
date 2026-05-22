# Tool-Budget Enforcement Patterns — Survey of 7 Curated Agent Repos

**Date**: 2026-05-22
**Trigger**: Live evidence — Aura agent loop made 30+ `web_search`/`web_fetch` calls
in 2 min with 4 ignored 404s, eventually timing out at 28 LLM calls / 33 tool calls
/ 180s, **never delivered a reply**. A prompt-level rule ("prefer wiki first") had
no effect. Need code-level enforcement.

Repos surveyed (source, not READMEs):
`D:/tmp/codex`, `D:/tmp/elysia`, `D:/tmp/nanobot`, `D:/tmp/openhuman`,
`D:/tmp/hermes-agent`, `D:/tmp/picobot`, `D:/tmp/cli-printing-press`.

---

## 1. Hermes-agent (Python · NousResearch) — RICHEST set

### 1a. Per-tool budget caps
**File**: `hermes_cli/tools_config.py:97`
```python
_DEFAULT_OFF_TOOLSETS = {"moa", "homeassistant", "spotify", "discord",
                         "discord_admin", "video", "video_gen", "x_search"}
```
This is NOT a per-call cap — it's a **default-off allow-list** for whole toolsets.
Combined with **per-platform** (cron, telegram, slack…) overrides in
`_get_platform_tools(cfg, "cron")`, expensive toolsets never load at all on
unattended runs.

There is NO per-tool call-count cap in Hermes. Budget is **iteration-shaped** (whole
agent-loop iterations), not per-tool.

### 1b. Force-finalize on budget exhaustion (THE money pattern)
**File**: `agent/conversation_loop.py:3797-3814`
```python
if final_response is None and (
    api_call_count >= agent.max_iterations
    or agent.iteration_budget.remaining <= 0
):
    _turn_exit_reason = f"max_iterations_reached(...)"
    agent._emit_status(f"⚠️ Iteration budget exhausted ...")
    final_response = agent._handle_max_iterations(messages, api_call_count)
```
`_handle_max_iterations` lives in `agent/chat_completion_helpers.py:925`:
```python
def handle_max_iterations(agent, messages, api_call_count) -> str:
    summary_request = (
        "You've reached the maximum number of tool-calling iterations allowed. "
        "Please provide a final response summarizing what you've found and "
        "accomplished so far, without calling any more tools."
    )
    messages.append({"role": "user", "content": summary_request})
    # ... build api_messages WITHOUT tools ...
    summary_response = ... # one extra LLM call, no tools available
    return final_response
```
Key insight: one extra LLM call **with tools stripped** is fired; the user gets a
summary of partial work instead of an empty/timeout reply. Pre-2026 there was a
"grace call" pattern (`_budget_grace_call` flag, `conversation_loop.py:617`) that
gave the model one more chance to finalize naturally before stripping tools.

### 1c. Stop on repeated error
Hermes does NOT have a per-tool "stop after N failures" mechanism in source. It has
a generic "near-max-iterations error" trap (`conversation_loop.py:3789`):
```python
if api_call_count >= agent.max_iterations - 1:
    _turn_exit_reason = f"error_near_max_iterations({error_msg[:80]})"
    final_response = f"I apologize, but I encountered repeated errors: {error_msg}"
    messages.append({"role": "assistant", "content": final_response})
    break
```
This kicks in only when iteration budget is almost done AND an error fires. Not
proactive.

### 1d. Tool-call counter shape
**Thread-safe per-agent counter** (`agent/iteration_budget.py`):
```python
class IterationBudget:
    def __init__(self, max_total: int):
        self.max_total = max_total
        self._used = 0
        self._lock = threading.Lock()
    def consume(self) -> bool:
        with self._lock:
            if self._used >= self.max_total: return False
            self._used += 1
            return True
    def refund(self) -> None: ...  # programmatic tool calls don't burn budget
```
**Scope: per-agent instance** (parent has 1, each subagent gets its own
independent `IterationBudget(50)`). Reset = new agent instance per user turn.

### 1e. Token budget vs call-count budget
**BOTH**, layered:
- Layer 1 — **call count**: `max_iterations` default 90 (`AGENTS.md:97`),
  `cron/scheduler.py:1468` default, configurable per-subagent.
- Layer 2 — **token / char budget per turn**:
  `tools/budget_config.py:18` `DEFAULT_TURN_BUDGET_CHARS: int = 200_000`,
  `DEFAULT_RESULT_SIZE_CHARS: int = 100_000`,
  `DEFAULT_PREVIEW_SIZE_CHARS: int = 1_500`.
- Layer 3 — **spill oversize results to sandbox**:
  `tools/tool_result_storage.py:122` `maybe_persist_tool_result()` writes any tool
  output > threshold into `/tmp/hermes-results/{tool_use_id}.txt` and replaces
  the inline content with a `<persisted-output>` block + preview + filepath.
  Model uses `read_file` to consume.
- Layer 4 — **per-turn aggregate cap**:
  `tools/tool_result_storage.py:181` `enforce_turn_budget()` — if total chars
  across all tool messages in this turn > `turn_budget`, spill the largest
  non-persisted results first.

This is a 4-LAYER defense. Aura currently has Layer 3 partial (truncate at
40KB byte-cap from Phase-DRIFT). Layers 1/2/4 missing.

### 1f. Per-class vs flat
Hermes uses a SINGLE flat `max_iterations` cap across all tool classes — no
per-class (web vs filesystem vs LLM) split. The per-toolset gating happens
**before** the loop (whole toolset enabled/disabled per platform), not during.

### 1g. The $4.63 incident (explicit)
**File**: `cron/scheduler.py:60-88` — `_resolve_cron_enabled_toolsets` docstring:
> "_DEFAULT_OFF_TOOLSETS ({moa, homeassistant, rl}) are removed by
> _get_platform_tools for unconfigured platforms, so fresh installs get cron
> WITHOUT `moa` by default (issue reported by Norbert — surprise $4.63 run)."

Resolution: precedence chain `per-job enabled_toolsets → platform-specific
tools_config → None (safe fallback)`. Cron platform inherits
`_DEFAULT_OFF_TOOLSETS = {moa, homeassistant, spotify, discord, video, video_gen,
x_search}`, so expensive third-party tools never load unless explicitly opted in
per-job. Mapping: `tools_config.py:97`.

### 1h. Failure-mode docs
`website/docs/user-guide/configuration.md:712`:
> "When the iteration budget is fully exhausted, the CLI shows: `⚠ Iteration
> budget reached (90/90) — response may be incomplete`. If the budget runs out
> during active work, the agent generates a summary of what was accomplished
> before stopping."

`AGENTS.md:125` documents the loop predicate:
```python
while (api_call_count < self.max_iterations
       and self.iteration_budget.remaining > 0) or self._budget_grace_call:
```

`release_v0.5.0.md:21` documents a regression: **intermediate budget warnings
caused models to "give up" prematurely on complex tasks** — Hermes since
strips stale budget warnings from history.

`agent/agent_init.py:489-495`:
> "Iteration budget: the LLM is only notified when it actually exhausts the
> iteration budget. At that point we inject ONE message, allow one final API
> call. **No intermediate pressure warnings — they caused models to give up
> prematurely on complex tasks (#7915).**"

This is a load-bearing anti-pattern lesson.

### Summary for Hermes
4-layer defense (per-tool char threshold → persist-to-sandbox → per-turn
aggregate budget → iteration budget) + force-finalize via tools-stripped summary
call. The $4.63 fix is **disable expensive tools by default for unattended
runs**, NOT add per-tool counters. **Translatability to Aura Go: 5/5** —
maps cleanly. Aura's tool registry can grow a `default_off_in_channel(chan)`
predicate, the loop can grow a `force_finalize_without_tools()` helper.

---

## 2. Openhuman (Rust · Tauri/CLI)

### 2a. Per-tool budget caps
NO per-tool counters. Single flat `max_tool_iterations`:
**File**: `src/openhuman/agent/harness/tool_loop.rs:28-29`
```rust
pub(crate) const DEFAULT_MAX_TOOL_ITERATIONS: usize = 10;
```
Used at line 121:
```rust
let max_iterations = if max_tool_iterations == 0 {
    DEFAULT_MAX_TOOL_ITERATIONS
} else { max_tool_iterations };
for iteration in 0..max_iterations { ... }
```

### 2b. Force-finalize on budget exhaustion
**Does NOT finalize.** Returns a TYPED error:
**File**: `src/openhuman/agent/harness/tool_loop.rs:923-934`
```rust
// Return the typed AgentError::MaxIterationsExceeded variant ...
// so downstream wrappers — Agent::run_single in runtime.rs — can downcast
// and suppress Sentry emission for this deterministic agent-state outcome.
Err(anyhow::Error::new(
    AgentError::MaxIterationsExceeded { max: max_iterations },
))
```
The error bubbles up to the caller, which renders it to the user. **Better
than silent timeout, worse than Hermes summary-strip-tools approach.**

### 2c. Pluggable stop-hooks (the standout pattern)
**File**: `src/openhuman/agent/stop_hooks.rs` — 295 lines. Core trait:
```rust
#[async_trait]
pub trait StopHook: Send + Sync {
    fn name(&self) -> &str;
    async fn check(&self, ctx: &TurnState<'_>) -> StopDecision;
}
pub enum StopDecision { Continue, Stop { reason: String } }
pub struct TurnState<'a> {
    pub iteration: u32, pub max_iterations: u32,
    pub cost: &'a TurnCost, pub model: &'a str,
}
tokio::task_local! { pub static CURRENT_STOP_HOOKS: Vec<Arc<dyn StopHook>>; }
```
Two built-ins ship: `BudgetStopHook { max_usd }` (fails CLOSED on NaN / 0 /
Inf — `stop_hooks.rs:125`) and `MaxIterationsStopHook { cap }`. Hooks fire
at the top of every iteration in `tool_loop.rs:175-211`:
```rust
let stop_hooks = current_stop_hooks();
for iteration in 0..max_iterations {
    if !stop_hooks.is_empty() {
        let state = TurnState { iteration, max_iterations, cost: &turn_cost, model };
        for hook in &stop_hooks {
            if let StopDecision::Stop { reason } = hook.check(&state).await {
                anyhow::bail!("Agent turn stopped by hook '{}': {reason}", hook.name());
            }
        }
    }
}
```

### 2d. Tool-call counter shape
Iteration counter is loop-local (scope = single turn). USD cost lives in
`TurnCost` (`src/openhuman/agent/cost.rs`):
```rust
pub struct TurnCost {
    pub input_tokens: u64,
    pub output_tokens: u64,
    pub cached_input_tokens: u64,
    pub charged_usd: f64,
    pub estimated_usd: f64,
    pub call_count: u32,
}
```
Reset = new `run_tool_call_loop` invocation (per turn).

### 2e. Token vs call count
**Both available**: stop-hooks can read iteration count OR `TurnCost.total_usd()`
which combines backend-reported `charged_amount_usd` with a fallback estimate
keyed on model tier. `PRICING_TABLE` at `cost.rs:64` is a static struct of
USD/Mtok rates. Fail-closed on NaN cap (line 125, also covered by test
`budget_hook_fails_closed_on_nan_cap`). **Production-grade.**

### 2f. Per-class vs flat
Single flat cap; class-aware stop-hooks could be implemented (the trait is
generic) but none ship by default.

### 2g. $4.63-equivalent incident
Not documented in openhuman source.

### 2h. Failure-mode docs
`stop_hooks.rs:6-9`:
> "Stop hooks are the policy lever: budget caps, rate limits, custom kill
> switches. They run between iterations of the tool-call loop so a runaway
> turn can be cut short before the next provider call rather than after the
> fact."

### Summary for Openhuman
Stop-hook trait + USD budget hook + iteration hook + per-turn `TurnCost`
accumulator. Hooks fire **before** every LLM call (cuts runaway turns at the
top, not after). NaN/0/Inf safe. **Translatability to Aura Go: 5/5** — trait
maps directly to `interface{}` with `Check(ctx) Decision`, can be installed
via context.Value. **Best single-shot lift candidate.**

---

## 3. Nanobot (Python) — closest to the 404-thrash scenario

### 3a. Per-tool budget caps (per-call signature throttling)
**Direct hit on Aura's bug**:
**File**: `nanobot/utils/runtime.py:13-16,68-102`
```python
_MAX_REPEAT_EXTERNAL_LOOKUPS = 2
_MAX_REPEAT_WORKSPACE_VIOLATIONS = 2

def external_lookup_signature(tool_name, arguments) -> str | None:
    """Stable signature for repeated external lookups we want to throttle."""
    if tool_name == "web_fetch":
        url = str(arguments.get("url") or "").strip()
        if url: return f"web_fetch:{url.lower()}"
    if tool_name == "web_search":
        query = str(arguments.get("query") or arguments.get("search_term") or "").strip()
        if query: return f"web_search:{query.lower()}"
    return None

def repeated_external_lookup_error(tool_name, arguments, seen_counts) -> str | None:
    """Block repeated external lookups after a small retry budget."""
    signature = external_lookup_signature(tool_name, arguments)
    if signature is None: return None
    count = seen_counts.get(signature, 0) + 1
    seen_counts[signature] = count
    if count <= _MAX_REPEAT_EXTERNAL_LOOKUPS: return None
    logger.warning("Blocking repeated external lookup {} on attempt {}",
                   signature[:160], count)
    return (
        "Error: repeated external lookup blocked. "
        "Use the results you already have to answer, or try a meaningfully "
        "different source."
    )
```

This is the exact mechanism that would have killed Aura's 30-web-search thrash.

### 3b. Force-finalize on budget exhaustion
**File**: `nanobot/agent/runner.py:540-557`
```python
else:
    stop_reason = "max_iterations"
    if spec.max_iterations_message:
        final_content = spec.max_iterations_message.format(
            max_iterations=spec.max_iterations)
    else:
        final_content = render_template(
            "agent/max_iterations_message.md",
            strip=True, max_iterations=spec.max_iterations)
    self._append_final_message(messages, final_content)
```
Template content (`nanobot/templates/agent/max_iterations_message.md`):
> "I reached the maximum number of tool call iterations
> ({{ max_iterations }}) without completing the task. You can try breaking
> the task into smaller steps."

**Worse than Hermes** — no model-driven summary, just a static template.
**Better than Openhuman** — at least a coherent user-facing string instead of
a bubbled error.

### 3c. Stop on repeated error / blocking signal flow
At `nanobot/agent/runner.py:820-833` (inside `_run_tool`):
```python
lookup_error = repeated_external_lookup_error(
    tool_call.name, tool_call.arguments, external_lookup_counts)
if lookup_error:
    event = {"name": tool_call.name, "status": "error",
             "detail": "repeated external lookup blocked"}
    if spec.fail_on_tool_error:
        return lookup_error + hint, event, RuntimeError(lookup_error)
    return lookup_error + hint, event, None
```
The error string + hint (`"\n\n[Analyze the error above and try a different
approach.]"`) is FED BACK to the model as the tool result. Soft block, lets
the model adapt.

Workspace-violation throttling uses the same shape
(`repeated_workspace_violation_error`, `runtime.py:142-170`), with a stronger
"this is a hard policy boundary — switching tools, shell tricks, working_dir
overrides, symlinks, or base64 piping will NOT change the answer" payload.

### 3d. Tool-call counter shape
**Per-turn, per-signature**:
- `external_lookup_counts: dict[str, int]` initialized at turn start
  (`runner.py:259`)
- `workspace_violation_counts: dict[str, int]` likewise
- `_MAX_INJECTIONS_PER_TURN = 3`, `_MAX_INJECTION_CYCLES = 5`
  (`runner.py:56-57`) — separate counter for user-injection floods

Reset = new turn.

### 3e. Token vs call count
Both — `max_tool_result_chars: int = 16_000` (`config/schema.py:126`) caps
each tool result; truncated via `truncate_text_fn` at `loop.py:1409`. Plus
the call-count caps above.

### 3f. Per-class vs flat
**Class-aware via signature function.** `external_lookup_signature` only fires
for `web_fetch` / `web_search`; `workspace_violation_signature` only fires
for `exec`/`shell`/filesystem tools. Other tools ignore the throttle.

### 3g. $4.63-equivalent incident
Not in source. The throttle pattern itself reads like a post-mortem fix
("Use the results you already have to answer, or try a meaningfully different
source") — implies prior thrash.

### 3h. Failure-mode docs
Inline in the error string. No external docs found.

### Summary for Nanobot
**Per-signature retry budget** keyed on `tool_name + normalized args`. Same
URL twice → fine. Same URL three times → blocked with model-facing soft error.
Class-aware (web tools + filesystem tools only). Reset per turn. Fed back to
the model as a tool result with a "try a different source" nudge.
**Translatability to Aura Go: 5/5** — direct port; signature function is a
single switch on `tool.Name()` reading `args[key]`.

---

## 4. Codex (Rust · OpenAI)

### 4a. Per-tool budget caps
**None in source.** No counters per tool, no `max_tool_calls`, no
`max_iterations`. Loop is naked:
**File**: `codex-rs/core/src/session/turn.rs:239`
```rust
loop {
    let pending_input = if can_drain_pending_input { ... };
    // ... build request, call model ...
}
```

### 4b. Force-finalize on budget exhaustion
N/A — relies on the API's `UsageLimitExceeded` error:
`codex-rs/core/src/session/turn.rs:149-160`
```rust
if err.to_codex_protocol_error() == CodexErrorInfo::UsageLimitExceeded
    && let Err(err) = sess
        .goal_runtime_apply(GoalRuntimeEvent::UsageLimitReached { ... })
        .await {
    warn!("failed to usage-limit active goal after usage-limit error: {err}");
}
```
When the OpenAI quota fires, a `UsageLimitReached` event is dispatched to
goal runtime; an active goal is marked `UsageLimited` and the turn ends.
**Token-budget-only — entirely externalized to the API quota.**

### 4c. Stop on repeated error
Hooks via `hook_runtime.rs:294 run_turn_stop_hooks` — `turn.rs:363-393`:
```rust
let stop_outcome = run_turn_stop_hooks(&sess, &turn_context, stop_hook_active,
                                       last_agent_message.clone()).await;
if stop_outcome.should_block { ... }
if stop_outcome.should_stop { break; }
```
Hooks are user-defined plugins (similar shape to openhuman) — not built-in
per-tool retry logic.

### 4d. Tool-call counter shape
None.

### 4e. Token vs call count
**Token-only**, externalized:
`AutoCompactTokenLimitScope::Total` (`config/mod.rs:571`,
`config/config_tests.rs:311`):
```
min_rate_limit_remaining_percent = 12
```
Codex compacts conversation history when rate-limit-remaining drops below 12%
and surfaces `hide_rate_limit_model_nudge` (`config/edit.rs:1180`) for
operator UX.

### 4f. Per-class vs flat
N/A.

### 4g. $4.63-equivalent
Not documented.

### 4h. Failure-mode docs
`goals.rs:158-394` documents `UsageLimitReached` flow — a goal-tracking
side effect when API quota dies, not a per-loop control.

### Summary for Codex
**Has NO budget enforcement in source.** Relies on (a) OpenAI API
rate/usage limits + auto-compact + nudges, (b) operator-installed stop_hooks
(same trait shape as openhuman). The single-shot lift is the stop-hook
pattern, but openhuman documents it better.
**Translatability to Aura Go: 2/5** — Aura's LLM provider is self-hosted
(llama.cpp / OpenAI-compatible), no quota-based stopping. Codex's pattern
doesn't fit.

---

## 5. Elysia (Python · DSPy/Weaviate)

### 5a. Per-tool budget caps
NO per-tool counters. Single `recursion_limit`:
**File**: `elysia/tree/objects.py:626-629`
```python
if recursion_limit is None:
    self.recursion_limit = 3
else:
    self.recursion_limit = recursion_limit
```
Default 3 (tree.py:152 uses 5 as the demo default). Counter is
`num_trees_completed`.

### 5b. Force-finalize on budget exhaustion
**Cleanest pattern in the survey.** The model itself terminates via the
`text_response` tool:
**File**: `elysia/tree/tree.py:1624-1630`
```python
completed = (
    self.current_decision.function_name == "text_response"
    or self.current_decision.end_actions
    or self.current_decision.impossible
    or self.tree_data.num_trees_completed > self.tree_data.recursion_limit
)
```
**No separate "summary call"** — the answer is an argument to a normal
tool call (`text_response(answer="...")`). When the recursion limit fires,
the loop exits and the last-collected `text_response` is the reply.

The user-facing nudge as the model approaches the cap is in
`tree/objects.py:808-814`:
```python
def tree_count_string(self):
    out = f"{self.num_trees_completed+1}/{self.recursion_limit}"
    if self.num_trees_completed == self.recursion_limit - 1:
        out += " (this is the last decision you can make before being cut off)"
    if self.num_trees_completed >= self.recursion_limit:
        out += (" (recursion limit reached, write your full chat response "
                "accordingly - the decision process has been cut short, and "
                "it is likely the user's question has not been fully answered "
                "and you either haven't been able to do it or it was impossible)")
    return out
```
This string is injected into the model's prompt every iteration. **Pre-emptive
pressure** (the opposite of Hermes's lesson, see anti-pattern below).

### 5c. Stop on repeated error
Not enforced per-tool, but successful_action flag (`tree.py:1684-1690`) +
recursion limit acts as a fuzzy guard.

### 5d. Tool-call counter shape
Per-conversation `num_trees_completed`, scope = single user query.

### 5e. Token vs call count
Call count only.

### 5f. Per-class vs flat
Flat.

### 5g. $4.63 incident
N/A.

### 5h. Failure-mode docs
The `tree_count_string()` text IS the user-facing failure-mode doc — embedded
in the prompt.

### Summary for Elysia
**Terminal tool pattern**: `text_response(answer="...")` IS the way to
finalize, not a separate "strip-tools then ask for summary" call. Recursion
limit + the prompt-injected progress meter is the budget. Already lifted as
Aura's LAT-03 plan.
**Translatability to Aura Go: 4/5** — already on the Aura roadmap.
**Caveat**: Elysia's pre-emptive pressure warning IS the anti-pattern
Hermes warned about (`agent_init.py:489` "models give up prematurely").
Adopt the terminal-tool, NOT the warning string.

---

## 6. Picobot (Go)

### 6a. Per-tool budget caps
None.

### 6b. Force-finalize on budget exhaustion
**Falls back to last tool result**:
**File**: `internal/agent/loop.go:283-287`
```go
if finalContent == "" && lastToolResult != "" {
    finalContent = lastToolResult
} else if finalContent == "" {
    finalContent = "I've completed processing but have no response to give."
}
```
Or the static "Max iterations reached without final response"
(`loop.go:368` in `ProcessDirect`). **Worst failure-mode in the survey** —
the user gets a tool-result-shaped reply (often raw JSON or HTML) instead
of a coherent answer.

### 6c. Stop on repeated error
None.

### 6d. Tool-call counter shape
Single `iteration` int local to the for-loop.

### 6e. Token vs call count
Call count only. Default 100 (`internal/config/onboard.go:21`
`MaxToolIterations: 100`).

### 6f. Per-class vs flat
Flat.

### 6g. $4.63 incident
N/A.

### 6h. Failure-mode docs
None.

### Summary for Picobot
**Minimal, naive.** The Go shape is closest to Aura but the pattern is the
one to AVOID, not lift.
**Translatability to Aura Go: 1/5** (don't lift — Aura is already past this
shape).

---

## 7. CLI-printing-press (Go)

**NO TOOL-BUDGET ENFORCEMENT.** It's a CLI-code generator, not an agent loop.
Grep hits refer to API rate-limit budgets in the GENERATED client, not agent
tool-call budgets. **Translatability: 0/5.**

---

## CROSS-REPO SYNTHESIS

### Universal patterns (4+ repos)
1. **Iteration cap with finalize on exhaustion** — Hermes (90), Openhuman (10),
   Nanobot (200 default, 15 in dream), Elysia (3), Picobot (100). All five
   force the loop to terminate at a hard ceiling. The cap value is
   model-and-task-dependent; per-call config knob is universal.
2. **Per-tool / per-result char cap** — Hermes
   (`DEFAULT_RESULT_SIZE_CHARS: 100_000`), Nanobot (`max_tool_result_chars:
   16_000`), Openhuman (per-tool truncate inside each tool impl). Per-call
   byte clamp is universal at this scale.
3. **The terminator is the LLM's own assistant message** — every loop ends
   normally when the model emits text-without-tool-calls. The budget guards
   are FALLBACKS, not the primary path.

### Promising for Aura's web-tool-thrash (2-3 repos)
4. **Per-signature retry throttle** — Nanobot's
   `_MAX_REPEAT_EXTERNAL_LOOKUPS = 2` keyed on `tool + normalized args`.
   Returns a model-facing soft error fed back as the tool result. THE direct
   antidote to Aura's 30-web_search bug. **Only nanobot ships this.**
5. **Stop-hook trait** — Openhuman (`StopHook` trait with USD/iteration
   built-ins) + Codex (`run_turn_stop_hooks`). Pluggable, pre-LLM-call,
   policy-shaped. Two repos = enough signal to be a pattern.
6. **Force-finalize via tools-stripped summary call** — Hermes
   (`handle_max_iterations` strips tools from `api_messages`, fires one extra
   LLM call asking "summarize what you've found"). Nanobot ships a weaker
   static-template variant. Hermes-shape is strictly better.
7. **Channel/platform default-off allow-list for expensive tools** — Hermes
   `_DEFAULT_OFF_TOOLSETS` resolved per-platform (`cron` strips spotify,
   moa, etc.). Mitigates the entire class of "scheduled job runs an
   expensive default tool" bugs ($4.63 incident).

### Anti-patterns Aura should NOT adopt
- **Pre-emptive budget pressure warnings injected into prompts**
  (Elysia's `tree_count_string`). Hermes explicitly removed these:
  `agent_init.py:489` — "No intermediate pressure warnings — they caused
  models to give up prematurely on complex tasks (#7915)." Aura's prompt
  rule "prefer wiki first" failed for the same reason — prompt-level
  pressure is noise, code-level enforcement is signal.
- **Bubble typed error to caller without finalizing** (Openhuman's
  `AgentError::MaxIterationsExceeded`). Tauri callers render the error
  string, but a Telegram chat user gets a stack-trace-shaped message.
  Translate to a `final_content` instead.
- **Naked `loop { ... }` with no iteration counter** (Codex). Works for an
  API-quota-gated SaaS but not for self-hosted llama.cpp where there is no
  external quota.
- **Per-tool flat counter without signature normalization.** Counting raw
  `web_search` calls without normalizing the query/url means 100 unique
  searches and 1 repeated 30× both look the same. Nanobot's signature
  function is what makes the throttle useful.
- **Fall back to lastToolResult on budget exhaustion** (Picobot). User gets
  raw JSON / HTML / search-result-dump. Strictly worse than a model-driven
  summary or even a coherent error string.

### Direct mapping to the 30-web_search-with-404 live evidence

The combination that would have killed the thrash, in priority order:

1. **Nanobot per-signature throttle** (`_MAX_REPEAT_EXTERNAL_LOOKUPS = 2`).
   The bug shows ≥4 ignored 404s on the SAME url → same signature →
   would have blocked on attempt 3 with `"Error: repeated external lookup
   blocked. Use the results you already have to answer, or try a meaningfully
   different source."`. **Single highest-impact fix.**
2. **Aura already has `AURA_AGENT_LOOP_MAX_STEPS = 5` per CLAUDE.md** — but
   the live log shows 28 LLM calls / 33 tool calls. Either the cap is not
   wired into the agent_loop hot path or it's scoped wrong (probably
   per-round, not per-turn). **Audit `internal/chat/agentloop.go` for
   the actual counter location.**
3. **Hermes-shape finalize**: on hitting the cap, strip tools, fire one
   summary LLM call. Aura's current behavior is "never delivered" — i.e.
   it hung. Even a 1-line "I tried to fetch X but kept hitting 404s; here's
   what I found from my wiki: …" is strictly better than a 180s timeout.
4. **Per-tool-class soft cap** (web tools 5/turn, wiki/memory 10/turn,
   filesystem unlimited). Web is the class that thrashes; cap it tighter
   than the loop's overall iteration budget.
5. **Stop-hook for total turn elapsed** — openhuman-shape `TurnState` extended
   with `elapsed: Duration`. Add `WallClockStopHook { cap: 60s }` — kills
   any turn that crosses 60s regardless of iteration count. Live evidence
   capped at 180s, so 60s would have saved the user 120s of waiting.

Recommended Aura PR shape (one commit):
```
internal/agent/budget/
    repeat_lookup.go    // nanobot port: RepeatLookupThrottle{ web_search, web_fetch, …}
    stop_hook.go        // openhuman port: StopHook interface, BudgetStopHook, ElapsedStopHook
    finalize.go         // hermes port: ForceFinalize(messages, "summarize w/o tools")
internal/agent/agentloop.go
    + per-class counters: webCallCount, wikiCallCount, fsCallCount
    + at loop top: run stop hooks; on Stop → ForceFinalize and return
    + at tool dispatch: signature lookup → if blocked, inject error as tool result
```

### Final translatability scorecard
| Repo                 | Score | Lift               |
|----------------------|-------|--------------------|
| Hermes               | 5/5   | finalize-strip-tools + default-off allow-list + 4-layer char budget |
| Openhuman            | 5/5   | StopHook trait + BudgetStopHook (USD) + MaxIterationsStopHook |
| Nanobot              | 5/5   | per-signature throttle (`_MAX_REPEAT_EXTERNAL_LOOKUPS`) **highest impact** |
| Elysia               | 4/5   | terminal-tool pattern (already LAT-03) |
| Codex                | 2/5   | stop-hook shape only; API-quota-gated approach doesn't fit |
| Picobot              | 1/5   | don't lift — Aura is past this shape |
| Cli-printing-press   | 0/5   | not applicable |
