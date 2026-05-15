# Phase 6 — Industry Patterns for "Agent Learns From Preventable Tool-Call Failures"

**Date:** 2026-05-15
**Mission:** Phase 6 of Aura's masterplan — "Add the Tool Experience Loop"
**Method:** Mine curated D:/tmp sources (nanobot, recursive-llm, cli-printing-press, elysia, picobot, codex, paper, aura-agent-loop-papers, mem0) read-only, cite file:line for every concrete claim, separate "project does X" from "paper says X".

Aura's current shape (do not duplicate, COMPOSE with):
- Structured tool-error classes via `classifyToolError` (timeout / not_found / validation / permission / blocked / rate_limited / io / error) at `d:/Aura/internal/agent/tools/registry/error.go:13`.
- `FormatToolError` returns plain text with optional `specificHint` (e.g., "/bin/sh is dash → use execute_code") at `d:/Aura/internal/agent/tools/registry/error.go:51`.
- Conversation archive in SQLite with per-turn tool_calls JSON (`CONV_ARCHIVE_ENABLED`).
- Governance layer + phantom-guard (proximity-based, see commit 53a0f6b2).

---

## A. CATALOGUE — patterns found in the wild

| # | Pattern name | Source file:line | What it does | Aura applicability |
|---|---|---|---|---|
| 1 | **Per-tool throttle on repeated external lookup (URL/query signature)** | `D:/tmp/nanobot/nanobot/utils/runtime.py:68-102` (`external_lookup_signature`, `repeated_external_lookup_error`); invoked at `D:/tmp/nanobot/nanobot/agent/runner.py:243-244, 750-763` | Per-turn dict keyed by `web_fetch:<url>` or `web_search:<query>` increments on every call; after `_MAX_REPEAT_EXTERNAL_LOOKUPS=2` (file:13) the tool short-circuits with `"Error: repeated external lookup blocked. Use the results you already have to answer, or try a meaningfully different source."` Never reaches the network on attempt 3+. | **VERY HIGH** — Aura has no equivalent. Trivially adds onto `Execute(ctx, tc.Name, tc.Arguments)` call site. Per-turn (not per-conversation) keeps state bounded. |
| 2 | **Per-tool throttle on repeated policy/workspace violation with cross-tool target signature** | `D:/tmp/nanobot/nanobot/utils/runtime.py:107-170` (`workspace_violation_signature`, `_normalize_violation_target`, `repeated_workspace_violation_error`); classifier at `D:/tmp/nanobot/nanobot/agent/runner.py:890-928` (`_classify_violation`) | When a tool returns a workspace-boundary error, extracts the *target path* from arguments (`path`, `file_path`, `target`, `source`, `destination`, or shell `command`/`working_dir`), normalizes it (`Path.expanduser().resolve()`), and counts attempts against that target ACROSS tools. After `_MAX_REPEAT_WORKSPACE_VIOLATIONS=2` (file:16) escalates the LLM-visible error to: *"You have tried to access '%s' (or an equivalent path) %d times in this turn. This is a hard policy boundary -- switching tools, shell tricks, working_dir overrides, symlinks, or base64 piping will NOT change the answer. Stop retrying."* | **VERY HIGH** — directly maps to Aura's `classifyToolError == "blocked"` / `"permission"` classes. Cross-tool signature is the critical insight: the model retries with `read_file` then `exec("cat ...")` then `web_fetch("file://...")` against the same target. |
| 3 | **"Analyze the error above and try a different approach" hint appended to every retryable tool error** | `D:/tmp/nanobot/nanobot/agent/runner.py:749` (`hint = "\n\n[Analyze the error above and try a different approach.]"`); appended at lines 762, 786, 828, 836 | A single static suffix glued to the tool-result string before the model sees it. Costs ~12 tokens. Documented effect: model treats the error as a thinking trigger, not a thing to mechanically retry. **Critical detail** at `runner.py:807-809`: legacy *exception* payloads do NOT get the hint ("Preserve legacy exception payloads without the retry hint") — only structured `Error: ...`-prefixed returns do. | **HIGH** — Aura's `FormatToolError` already returns `"Error: ..."`. A single-line `\n\n[…]` suffix is a one-symbol diff. |
| 4 | **SSRF "non-bypassable boundary" verbose payload — hard-stop without exception** | `D:/tmp/nanobot/nanobot/agent/runner.py:854-861` (`_SSRF_BOUNDARY_NOTE`), `:900-907` (`_classify_violation`) | When SSRF marker detected, returns a *verbose, specific* note to the LLM enumerating bypasses the model must NOT try: *"Do not retry with curl, wget, encoded IPs, alternate DNS, redirects, proxies, or another tool. Ask the user for local files, logs, screenshots, or an explicit safe public URL instead. If the user explicitly trusts this private URL, ask them to whitelist the exact IP/CIDR via tools.ssrfWhitelist."* `fatal_error=None` so the loop continues — model converses out. | **MEDIUM** — Aura blocks SSRF at the network layer but the LLM-facing message is generic. The "enumerate the bypasses that won't work" technique is the load-bearing trick. |
| 5 | **Elysia per-tool error ledger that auto-injects into next call of same tool** | `D:/tmp/elysia/elysia/objects.py:927-951` (`class Error(Update)`); ledger field at `D:/tmp/elysia/elysia/tree/objects.py:639` (`self.errors: dict[str, list[str]] = {}`); appended at `D:/tmp/elysia/elysia/tree/tree.py:1274-1297`; consumed by the decision agent at `D:/tmp/elysia/elysia/tree/prompt_templates.py:95-105` (`previous_errors: list[dict] = dspy.InputField(...)`) | When a tool yields `Error(feedback=..., error_message=...)`, the tree stores it under `errors[function_name].append(...)`. The decision agent's *next* prompt receives `previous_errors` as a first-class field, with an instruction: *"Use this list to avoid repeating the same errors for those functions. If an error appears solvable, you should generally try the same tool again, as the error will be passed to the tool for handling."* Two distinct prefixes are used: `"Avoidable error: ..."` (feedback supplied — solvable) vs `"Unknown error: ..."` (no feedback — may be unfixable). | **HIGH** — this is the **Phase 6 core pattern**. Aura already persists tool_calls per turn in the conversation archive; the inject step is the missing piece. The "Avoidable" vs "Unknown" split aligns with Aura's `classifyToolError` taxonomy (validation/not_found = avoidable; timeout/io = unknown). |
| 6 | **Elysia Error has explicit two-field shape: feedback (for the planner) + error_message (for diagnostics)** | `D:/tmp/elysia/elysia/objects.py:935-942` | `feedback` is what the decision agent sees; `error_message` is the raw exception. Tool authors choose. Default fallback "An unknown issue occurred." triggers the "Unknown error" branch. | **HIGH** — clean separation Aura doesn't have. Currently a tool returns one string. Splitting "this is what the LLM should read" from "this is for the operator log" is a small, high-leverage refactor. |
| 7 | **`cli-printing-press` structured `sync_warning` event with `reason` enum** | `D:/tmp/cli-printing-press/internal/generator/templates/sync.go.tmpl:328-348, 519-525, 634, 650, 1280, 1349, 1361` ; struct definition at `D:/tmp/cli-printing-press/internal/generator/templates/helpers.go.tmpl:172-178` | Every non-fatal failure path emits a one-line JSON event: `{"event":"sync_warning","resource":"%s","parent":"%s","reason":"%s","message":"%s"}`. The `reason` is a small enum: `max_pages_cap_hit`, `stuck_pagination`, `resource_not_incremental`, `exit_policy_default_changed`, `unresolved_path_key`. Caller decides on stdout vs stderr, downstream parses by `reason`. **One-shot semantics**: e.g. `exit_policy_default_changed` is emitted *only once per run* (file:328). | **MEDIUM-HIGH** — Aura's tool-error wire format is plain text. A parallel structured event channel (logged to SQLite for the dashboard + read by the planner) gives the operator a queryable failure-reason index without redesigning the LLM-facing string. |
| 8 | **Nanobot "Dream" — periodic offline LLM pass that distills history.jsonl into MEMORY/SOUL/USER markdown files** | `D:/tmp/nanobot/nanobot/agent/memory.py:784-1075` (`class Dream`); cursor at `:394-400` (`get_last_dream_cursor`, `set_last_dream_cursor`); 2-phase split at `:982-1071` (Phase 1 analyze, Phase 2 edit via AgentRunner) | A cron-scheduled "consolidator" reads unprocessed history since last cursor, runs a Phase-1 LLM call that produces an analysis, then Phase 2 runs an AgentRunner with only `read_file`/`edit_file` tools so it can *targeted-edit* the memory files instead of rewriting. Failure handling: *"Dream incomplete (%s): cursor NOT advanced, will retry next cron cycle"* (`memory.py:1071`). Per-line age annotation (`← Nd`) injected only when blame count matches line count (`:888-932`) — drift detection. | **MEDIUM** — Aura's wiki is already file-based. The pattern that matters here is **cursor-based offline distillation with a retry-safe "don't advance cursor on failure" idiom**. Useful if Phase 6 eventually wants a "tool-failures-of-the-week → wiki page" reflection step. Not core to the primary loop — keep separate. |
| 9 | **Per-target normalized signature (cross-tool deduplication)** | `D:/tmp/nanobot/nanobot/utils/runtime.py:133-139` (`_normalize_violation_target`) | Resolves the argument path via `Path(raw).expanduser().resolve().as_posix()` then lowercases and prefixes `violation:`. So `~/secrets.txt`, `/home/user/secrets.txt`, `./../user/secrets.txt` all collide on the same key — the model can't escape the throttle by spelling variation. | **HIGH** — generalizable beyond paths. Aura should canonicalize URLs (lowercase host, drop default ports, drop trailing slash) and queries (whitespace-collapse, lowercase) before signature-keying. |
| 10 | **Nanobot finalization-retry message + empty-response retry budget (separate from tool retry)** | `D:/tmp/nanobot/nanobot/utils/runtime.py:23-25` (`FINALIZATION_RETRY_PROMPT`), `:27-30` (`LENGTH_RECOVERY_PROMPT`); enforcement at `D:/tmp/nanobot/nanobot/agent/runner.py:41-43` (`_MAX_EMPTY_RETRIES=2`, `_MAX_LENGTH_RECOVERIES=3`, `_MAX_INJECTIONS_PER_TURN=3`) | Distinct retry budgets for distinct degenerate states: (a) model produced blank text → up to 2 retries, last one with `"Please provide your response to the user based on the conversation above."`; (b) finish_reason=length → up to 3 retries with `"Output limit reached. Continue exactly where you left off — no recap, no apology."` Tool failures are NOT counted in these budgets. | **MEDIUM** — orthogonal to Phase 6 but the *budget-by-failure-mode* design is the right shape. Aura currently has one iteration cap. Decoupling "tool-call retried this URL too many times" from "model produced 5 empty responses in a row" matters for diagnosing user-visible misbehavior. |
| 11 | **Recursive-llm catch-and-stringify** | `D:/tmp/recursive-llm/src/rlm/core.py:168-177` | The whole tool-failure story: `except REPLError as e: exec_result = f"Error: {str(e)}"` then `messages.append({"role": "user", "content": exec_result})`. No ledger, no throttle, no class. Bounded only by `max_iterations=30`. | **LOW** (reference baseline) — this is what Aura already does. Listed as evidence that the simple approach hits an iteration cap rather than learning. |
| 12 | **Picobot string-prefix `(tool error) ...`** | `D:/tmp/picobot/internal/agent/loop.go:260-273` | Same minimal approach as recursive-llm. No retry policy, no per-tool counts. The agent loop emits user-visible notification `"📢 %s failed (%s): %v"` on the Telegram channel — operator visibility, no LLM-side learning. | **LOW** (reference baseline) — confirms the field default is "stringify and hope". Aura is already a step ahead with `classifyToolError`. |
| 13 | **Elysia self-healing acceptance test pattern** | `D:/tmp/elysia/tests/requires_env/llm/test_self_healing.py:9-43` | A tool whose first call always returns `yield Error("The first call of this tool _always_ fails.")` and second call succeeds. Test asserts `tree.tree_data.tasks_completed[0]["task"][0]["error"]` (first call recorded as error) AND `"error" not in tree.tree_data.tasks_completed[0]["task"][1]` (second call clean). This is THE regression test that proves the error-ledger plumbing works end-to-end. | **HIGH** — Aura needs the equivalent: a probe whose tool fails-then-succeeds and asserts the planner consumed the failure instead of just hammering. Aligns with CLAUDE.md "VALIDATE WITH VERIFIED BENCHMARKS". |
| 14 | **MCP cache-coherent change-handling (Codex)** | `D:/tmp/codex.md:699-704` | When MCP servers emit `notifications/tools/list_changed` mid-conversation, naïve handling causes a prompt-cache miss for the entire transcript. Codex's response: *append a new `role=developer` message* describing the change instead of mutating the existing tools list. Same idea for sandbox/cwd changes. | **LOW-MEDIUM for Phase 6** specifically (it's about caching, not failure-learning), but **HIGH** if Aura plans to expose mid-conversation tool changes to the LLM. Worth flagging as adjacent constraint. |

---

## B. ACADEMIC concepts (paper-cited, theoretical)

| Technique | One-line definition | Found in |
|---|---|---|
| **Reflexion** | "Reinforces language agents through *verbal self-reflection* stored in episodic memory" — agent writes a short natural-language critique of its own trajectory and that critique becomes part of next-episode context. | `D:/tmp/aura-agent-loop-papers/2604.00356-Signals.txt:92`, cited at `:461` ("Reflexion: Language agents with verbal reinforcement learning.") |
| **ReAct** | Interleaves natural-language *reasoning traces* with task-specific *action* steps in a single LLM stream — the original "think-then-act" pattern that every modern tool-using agent inherits. | `D:/tmp/aura-agent-loop-papers/2604.00356-Signals.txt:89`; also `2605.10052-Swarm-Skills.txt:120, 558` |
| **Toolformer** | Trains LLMs to autonomously decide when to invoke external tools via inline API call syntax learned from self-supervised examples. | `D:/tmp/aura-agent-loop-papers/2604.00356-Signals.txt:90` |
| **Self-Refine** | Single model iteratively *generates feedback on its own output* and refines — no external critic, no environment reset required. | `D:/tmp/aura-agent-loop-papers/2604.00356-Signals.txt:93` |
| **ExpeL** | "Autonomously gathers experiences and *extracts natural-language insights* from training tasks for use at inference" — the offline-distillation analog of nanobot Dream. | `D:/tmp/aura-agent-loop-papers/2604.00356-Signals.txt:94-95` |
| **Voyager** | Builds a *skill library of executable code* through iterative prompting with environment feedback — relevant because Aura's skills system has the same shape. | `D:/tmp/aura-agent-loop-papers/2604.00356-Signals.txt:95-96` |
| **Execution-signal taxonomy: Failure vs Loop** | Two-class behavioral split: *Failure* = single non-advancing tool outcome (empty results, no-op, wrong tool); *Loop* = "repeated calls with identical inputs, repeated calls with systematically varying inputs, and repeated multi-tool cycles." Detected via "sequence analysis over invocation streams, using simple pattern rules." | `D:/tmp/aura-agent-loop-papers/2604.00356-Signals.txt:194-208` |
| **Environment-signal isolation** | Failures arising from infrastructure / API outage / resource boundaries are EXPLICITLY excluded from training supervision because they "do not reflect the quality of the agent's decisions and can introduce spurious correlations." | `D:/tmp/aura-agent-loop-papers/2604.00356-Signals.txt:209-214` |
| **Lemon Agent translates failures to "actionable suggestions"** | "Translating failures into actionable suggestions for subsequent planning cycles, this closed-loop search solution ensures that Lemon Agent maintains a stable and coherent reasoning trajectory even when interfacing with unpredictable external data sources." Three-tier fallback + exponential backoff + regex input sanitization. | `D:/tmp/aura-agent-loop-papers/2602.07092-Lemon-Agent.txt:271-283` |
| **Kimi K2.5 self-critique rubric reward** | Uses Generative Reward Models as "fine-grained evaluators" rather than binary judges, covering helpfulness/relevance/instruction-following — gradient signal from a critic LLM, not from explicit user feedback. | `D:/tmp/paper.md:657-669` |

---

## C. ANTI-PATTERNS (what the curated projects explicitly DON'T do)

| # | Anti-pattern | Citation |
|---|---|---|
| 1 | **Silent retry until success** — nanobot REFUSES to retry the same external URL/query a third time within a turn. The runtime injects a permanent error string that mentions "Use the results you already have to answer, or try a meaningfully different source" instead of looping. | `D:/tmp/nanobot/nanobot/utils/runtime.py:99-102` |
| 2 | **Letting the model bypass a policy by switching tools** — nanobot's workspace-violation signature is computed *cross-tool* on the normalized target path, so `read_file`, `exec("cat ...")`, `write_file("..")`, etc. all increment the SAME counter. The escalation message names this explicitly: *"switching tools, shell tricks, working_dir overrides, symlinks, or base64 piping will NOT change the answer."* | `D:/tmp/nanobot/nanobot/utils/runtime.py:160-170`, escalation at `D:/tmp/nanobot/nanobot/agent/runner.py:909-925` |
| 3 | **Reusing a generic "Error: ..." string for both exception and policy boundary** — nanobot deliberately omits the "try a different approach" retry hint on *exception payloads* (`runner.py:807-809` comment: *"Preserve legacy exception payloads without the retry hint"*) so the LLM doesn't get nudged into retrying a non-deterministic system failure. The hint is reserved for tool-author-emitted `"Error: ..."` strings. | `D:/tmp/nanobot/nanobot/agent/runner.py:805-818` |
| 4 | **Treating empty tool output as success** — `ensure_nonempty_tool_result` replaces `None` / blank / empty-list returns with `"(<tool_name> completed with no output)"`. Stops the LLM from inferring "the tool gave me nothing → that means there is nothing" when actually the tool plumbing silently dropped data. | `D:/tmp/nanobot/nanobot/utils/runtime.py:33-50` |
| 5 | **Loading environment-failures as training signal** — Signals paper explicitly carves out a third "Environment Signals" class so that infra/API-outage trajectories are routed to *diagnosis*, not to *learning*, because they "can introduce spurious correlations if used for learning." | `D:/tmp/aura-agent-loop-papers/2604.00356-Signals.txt:209-214` |
| 6 | **Advancing a memory-consolidation cursor on partial failure** — nanobot Dream logs *"cursor NOT advanced, will retry next cron cycle"* on incomplete runs. Idempotency-by-cursor is the only thing preventing dropped or double-consolidated history entries. | `D:/tmp/nanobot/nanobot/agent/memory.py:1069-1073` |
| 7 | **Annotating line ages when blame data is stale** — when `git blame` line count disagrees with the current file's line count, Dream *skips annotation entirely* rather than risk attaching the wrong age to the wrong line: *"better to skip annotation than to tag the wrong line"*. Same principle should apply to tool-failure attribution: when the signature is uncertain, don't escalate. | `D:/tmp/nanobot/nanobot/agent/memory.py:910-918` |
| 8 | **Mutating mid-conversation tool definitions** — Codex inserts a *new developer message* describing the change rather than editing the original `tools` block, to preserve prompt-cache validity. Same principle: when the agent's environment changes, *append a note*, don't rewrite history. | `D:/tmp/codex.md:699-704` |

---

## D. AURA-FIT SHORTLIST — the 3 patterns Phase 6 should adopt + 1 to reject

### ADOPT #1 — Per-turn URL/query signature throttle (nanobot pattern 1)

**Source of truth:** `D:/tmp/nanobot/nanobot/utils/runtime.py:68-102` + invocation at `runner.py:243-244, 750-763`.

**Aura shape (proposed):**
- New file `internal/agent/tools/registry/throttle.go` defining `signature(name, args) string` for `web_fetch`, `web_search`, and any tool returning `classifyToolError == "blocked"` or `"permission"`.
- Per-turn `map[string]int` lives in the same struct that owns the iteration counter — no new persistence layer.
- After `MAX_REPEAT=2` (matches nanobot's constant), the runtime returns a synthetic `"Error: repeated <name> for <signature-summary> blocked. Use the results you already have to answer, or try a meaningfully different source."` WITHOUT invoking the tool.

**Why this is the highest-value first step:**
- Aura ships with `classifyToolError` already producing the right labels. The throttle is a *consumer* of that classification — purely additive.
- Per-turn semantics (not per-conversation) means no schema migration, no governance change, no edge-case around session restore.
- Composes with phantom-guard: phantom-guard prevents lying-about-tool-calls, throttle prevents looping-on-real-tool-calls — complementary failure modes.

**Risk:** Cardinality of `signature()` matters. Lowercase + trim whitespace is enough for `web_search`. URLs need normalization (lowercase host, drop default :80/:443, drop trailing slash, drop fragment) — nanobot's `_normalize_violation_target` (`runtime.py:133-139`) is the reference.

---

### ADOPT #2 — Per-tool error ledger injected into next call's planning context (Elysia pattern 5+6)

**Source of truth:** `D:/tmp/elysia/elysia/objects.py:927-951` (`Error` class with `feedback`+`error_message`), `D:/tmp/elysia/elysia/tree/tree.py:1274-1297` (ledger append), `D:/tmp/elysia/elysia/tree/prompt_templates.py:95-105` (planner-side consumption).

**Aura shape (proposed):**
- Conversation archive (SQLite `conversations`) already records `tool_calls` per turn. Add a `tool_errors` column or a parallel `tool_errors` table keyed by `(conversation_id, turn_id, tool_name, error_class, message)`.
- Before each LLM call, gather `SELECT message FROM tool_errors WHERE conversation_id=? AND tool_name IN (<tools_in_prompt>) ORDER BY turn_id DESC LIMIT N` and inject as a system-level fenced block: `## Recent failures of available tools` — only when non-empty.
- Mirror Elysia's "Avoidable error" vs "Unknown error" split using `classifyToolError`:
  - `validation` / `not_found` / `blocked` / `permission` → "Avoidable" prefix (model can fix args).
  - `timeout` / `io` / `rate_limited` / `error` → "Unknown" prefix (model should consider switching).

**Why this is the Phase 6 *core* deliverable:**
- It is literally the "tool experience loop" the masterplan names.
- Reuses the conversation archive Aura already has — no new persistence subsystem.
- Bounded by `LIMIT N` (N≈5 is enough — same scale as Aura's 5-memory recall and nanobot's empty-retry cap).
- The Avoidable/Unknown split is taxonomy Aura already computes; this is wiring, not new classification.

**Risk:** Prompt-bloat if every conversation drags long error ledgers forever. Mitigation: `LIMIT N` + only inject when N > 0 + clear ledger entries older than X turns.

---

### ADOPT #3 — Structured `sync_warning`-style event on the operator side, in parallel with the LLM-facing string (cli-printing-press pattern 7)

**Source of truth:** `D:/tmp/cli-printing-press/internal/generator/templates/sync.go.tmpl:328-348, 634-650`, struct at `helpers.go.tmpl:172-178`.

**Aura shape (proposed):**
- New SQLite table `tool_warnings` with columns `(id, ts, conversation_id, turn_id, tool_name, reason TEXT, message TEXT)` where `reason` is a small enum: `repeated_call_blocked`, `policy_boundary_escalated`, `empty_result`, `validation_loop`, `phantom_suspected`.
- Emit on every nontrivial degenerate path (the throttle from ADOPT #1 fires `repeated_call_blocked`; the future error-ledger from ADOPT #2 can fire `validation_loop` when the same tool fails N times with the same error_class).
- Read endpoint `/api/tool_warnings` for the dashboard so the operator can see WHY a conversation went sideways without grepping logs.

**Why this composes:**
- Aura's wire-format to the LLM is plain text and the user has been clear about not breaking that. Operator-visible warnings ride on a parallel structured channel.
- `reason` is a closed enum — no free-form strings, no leaked LLM values. Compatible with CLAUDE.md "tool argument privacy" rule.
- Once the table exists, future Dream-style offline distillation (pattern 8) has a clean queryable source — but distillation is NOT shipped in Phase 6.

**Risk:** Low. Pure additive write path, no read-path coupling to the agent loop.

---

### REJECT — Nanobot's "Dream" two-phase offline memory consolidator (pattern 8)

**Source:** `D:/tmp/nanobot/nanobot/agent/memory.py:784-1075`.

**Why rejected for Phase 6:**
1. It's a *separate* subsystem (cron-scheduled, own AgentRunner, own tool registry, own cursor, own LLM call) — Phase 6 is "Add the Tool Experience Loop," not "Add a second LLM agent."
2. Aura already has the wiki + a maintenance queue (`proposed_updates`, `wiki_issues` in SQLite per CLAUDE.md). Adding another consolidator competes with that infrastructure.
3. The pattern is genuinely useful — but its right home is a *later* phase ("Tool-failure post-mortem → wiki page"), AFTER ADOPT #1/#2/#3 produce the structured signal it would consume. Building the consumer before the producer is backwards.
4. Risk: temperature=0 wiki writes + LLM-driven git commits + line-age annotation is a lot of moving parts; the masterplan should not couple Phase 6's success to that pipeline.

**Defer to:** Phase 7+ once `tool_warnings` (ADOPT #3) has accumulated enough signal to justify periodic distillation.

---

## Notes on academic vs project evidence

- **Evidence-backed (this project does X):** all rows in Section A with a `file:line` citation from `D:/tmp/{nanobot,recursive-llm,cli-printing-press,elysia,picobot,codex.md}`. Reproducible by re-reading the cited line.
- **Theoretical (paper says X):** all rows in Section B and anti-pattern #5. Citations are to `.txt` page-marked PDFs in `D:/tmp/aura-agent-loop-papers/` and the proceedings index in `D:/tmp/paper.md`. Aura should treat these as design *inspiration* not implementation guidance — none of the papers ship reference code in the curated tree.
- **Searched-and-came-up-empty:** `D:/tmp/mem0/mem0/memory/` has no tool-call-specific memory primitives (only generic textual memory via `add`/`get_all`/`search` at `mem0/memory/main.py:573, 1016, 1126`). Mem0 is irrelevant to Phase 6 as currently scoped.

---

## Files referenced for Phase 6 planning

Concrete, mineable:
- `D:/tmp/nanobot/nanobot/utils/runtime.py` (lines 1-170) — the entire throttle + signature module
- `D:/tmp/nanobot/nanobot/agent/runner.py` (lines 740-928) — `_run_tool` + `_classify_violation`
- `D:/tmp/elysia/elysia/objects.py` (lines 927-951) — `Error` class
- `D:/tmp/elysia/elysia/tree/tree.py` (lines 1274-1297) — ledger append
- `D:/tmp/elysia/elysia/tree/prompt_templates.py` (lines 95-105) — `previous_errors` planner input
- `D:/tmp/elysia/tests/requires_env/llm/test_self_healing.py` — acceptance-test template
- `D:/tmp/cli-printing-press/internal/generator/templates/helpers.go.tmpl` (lines 172-178) — `accessWarning` struct
- `D:/tmp/cli-printing-press/internal/generator/templates/sync.go.tmpl` (lines 328-348, 634-650) — emission sites

Aura side (for the planner to wire into):
- `d:/Aura/internal/agent/tools/registry/error.go` — `classifyToolError`, `FormatToolError`, `specificHint`
- `d:/Aura/internal/agent/tools/registry/registry.go` — Execute call site (signature throttle hook point)
- `d:/Aura/internal/conversation/` — archive that already records tool_calls per turn (ledger hook point)
