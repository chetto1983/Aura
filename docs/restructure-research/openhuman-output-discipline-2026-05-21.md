# openhuman — output discipline patterns

> Research date 2026-05-21. Source: `D:\tmp\openhuman` (Rust). Concepts only — openhuman is GPLv3.
> Question: how does openhuman keep tool outputs from leaking raw to the user?
> Aura symptom: `execute_code` reads 90 rows → agent dumps 90 rows verbatim (36 s reply).

## TL;DR

openhuman treats raw tool output as an **intermediate artifact**, never a candidate reply: it caps every tool result at 16 KiB before history ingestion, routes anything bigger through a dedicated `summarizer` sub-agent that returns a structured note with *identifiers preserved*, and binds every speaking agent to a short-bubbles "text-a-friend" prompt. The substrate Aura already has (TokenJuice + payload summarizer plumbing, lifted in Phase-OP+) covers half of this — what's missing in Aura is (a) the inline byte budget being **always-on**, (b) the orchestrator-style response-style contract, and (c) a `delegate_run_code` pattern so the LLM that summarises the result is never the same call that produced it.

## Patterns

### 1. Hard byte cap before history ingestion (always-on)

**Where:** `src/openhuman/context/tool_result_budget.rs:28` — `DEFAULT_TOOL_RESULT_BUDGET_BYTES = 16 * 1024`; applied at `src/openhuman/agent/harness/session/turn.rs:1060` via `self.context.tool_result_budget_bytes()`; runs even on the small-payload path that bypasses the summarizer.
**Rule:** every tool result is passed through `apply_tool_result_budget(raw, 16384)` *inline before it enters the history vector*. Truncation marker is verbose and machine-parseable: `[… 42_384 bytes truncated by tool_result_budget — re-run with a narrower query to see the rest …]`. Cut is on a UTF-8 char boundary (`tool_result_budget.rs:76` + `floor_char_boundary`), with the last 256 bytes reserved for the trailer (`TRAILER_RESERVED`).
**Why it kills the 90-row dump:** 90 rows of structured rows easily exceed 16 KiB; the LLM gets the head + an explicit "you were cut off, re-run narrower" signal that *trains it to ask for a filter* instead of restating the head.
**Aura port:** ~80 LOC. Add `apply_tool_result_budget(content string, budget int) (string, BudgetOutcome)` next to `internal/tokenjuice/` (helper already lives in `internal/tools`). Call site: the existing tool-result append point inside `internal/agent/loop` (mirrors openhuman's `Agent::execute_tool_call` hook). **Phase-OP+ shipped TokenJuice but not this stage** — TokenJuice operates on history reduction, not per-call inline truncation. This is a distinct, simpler stage and should land first.

### 2. `prefer_markdown_tool_output = true` default

**Where:** `src/openhuman/config/schema/context.rs:97-105`.
**Rule:** the harness asks tools to render markdown when they support it; the agent loop prefers `markdown_formatted` over JSON. Comment: "Markdown is materially cheaper than JSON in tokens, especially on tool-heavy loops." Default is `true`.
**Why it matters:** for `execute_code` that returns table data, a markdown table fits more rows under the byte budget than JSON-with-keys, so the truncation marker fires later and the model has a cheaper substrate to summarise from.
**Aura port:** ~40 LOC. `ToolResult` already has multiple representations in `internal/tools/`. Add `MarkdownFormatted *string` field + prefer it in the loop's append path. **Not in Phase-OP+.**

### 3. Payload-summarizer sub-agent with circuit breaker

**Where:** `src/openhuman/agent/harness/payload_summarizer.rs` (top-to-bottom — 487 LOC, the whole file is the pattern). Trigger point in `session/turn.rs:1006-1037`.
**Rule sequence (`maybe_summarize` body, lines 199-308):**
1. estimate tokens = `chars / 4` (`payload_summarizer.rs:315`);
2. if `tokens < threshold_tokens` → pass-through, no LLM call;
3. if `tokens > max_payload_tokens` (default 2 M) → pass-through, fall to truncation;
4. if circuit breaker tripped (3 consecutive failures within session) → pass-through;
5. else dispatch `summarizer` sub-agent → if returned summary is empty OR `>= raw.len()` → pass-through + count as failure.
**Production knob:** threshold is **currently 0** in the orchestrator's `agent.toml:62-66` ("disabled after recursive dispatch was observed") — they keep the plumbing wired so it can be re-enabled, but inline truncation is the workhorse. Useful lesson: *don't ship summarizer as the primary lever, ship truncation*.
**Aura port:** **Phase-OP+ already shipped this as US-P8-G payload summarizer** (memory `project_2026-05-18_phase_8_substrate_revised`). Audit: is Aura's threshold actually firing? If currently 0 like openhuman's, document explicitly that truncation must be the primary defence.

### 4. Summarizer extraction contract (the prompt is the algorithm)

**Where:** `src/openhuman/agent/agents/summarizer/prompt.md` (full 72-line prompt) + `payload_summarizer.rs:326-334` (`build_summarizer_prompt`).
**Rule:** the summarizer is a single-shot, no-tools sub-agent (`agent.toml: max_iterations = 1`, `tools.named = []`, `temperature = 0.2`) that is *contractually obligated* to preserve identifiers ("Identifiers are the single most important thing. Never drop them"), discard markup noise, and report original size. Output schema is fixed (overview + Key facts + Identifiers preserved + Original size). Hard fail: "If the summary is the same size as the payload, you have failed."
**Why this is the model that doesn't dump 90 rows:** the summarizer is a *different LLM call from the one that answers the user*. The orchestrator never sees the 90 rows — it sees `## Key facts\n- 90 customers total, first record: <id> <name>\n## Identifiers preserved\n- cust_a31, cust_b12, ...`.
**Aura port:** **Phase-OP+ shipped the summarizer plumbing**, but check the actual prompt body — if it does not have the "if same size you failed" line and the explicit `Identifiers preserved` section, port those two clauses (~20 LOC, prompt-only).

### 5. Sub-agent role contract suffix

**Where:** `src/openhuman/agent/harness/subagent_runner/ops.rs:42-74` — `SUBAGENT_ROLE_CONTRACT_SUFFIX` is appended (idempotently, line 49) to every sub-agent's system prompt.
**Rule:** four bullets injected after the archetype prompt:
- "Stay tightly scoped to the delegated task."
- "Keep tool arguments and follow-up prompts compact."
- "Keep your final response concise and synthesis-ready for the parent, prefer short bullets or short paragraphs."
- "Do not restate the full task/context unless strictly required for correctness."

Plus per-agent `max_result_chars` cap that **truncates the sub-agent's *final reply*** before it returns to the parent (`ops.rs:249-272`, char-safe truncation on multi-byte boundaries, appends `\n[...truncated]`). Production values from `agent.toml`s: researcher 8000, planner 8000, code_executor 16000, skill_creator 16000.
**Why this matters:** openhuman draws a clean line between the agent that *runs the tool* and the agent that *replies to the user*. The runner is bound to ≤16k chars output; the orchestrator never gets to dump.
**Aura port:** ~30 LOC if Aura adopts an orchestrator/runner split. Without that split, the "concise and synthesis-ready" suffix can still be appended to the *main* system prompt as a global rule (~10 LOC). **Not in Phase-OP+.** Recommend appending to `AGENT.md` overlay as a permanent contract.

### 6. Orchestrator response-style contract

**Where:** `src/openhuman/agent/agents/orchestrator/prompt.md:82-132` ("Response Style" section, plus 4 worked examples).
**Rule:** "Reply like you're texting a friend: casual, lowercase-ok, as few words as possible without losing meaning. No preamble, no recap, no 'I'll now…'." Em-dashes banned. Emojis default off. Split thoughts into separate bubbles via blank line. **First bubble acknowledges ("on it", "checking gmail"), then next bubble has the result.** Worked examples show one-bubble or two-bubble replies, never a wall of text.
**Why this is the missing piece for Aura:** Aura's 36-second 90-row dump is partly a *prompt failure*, not a substrate failure. The model was never told that "the result" ≠ "the tool output". openhuman's prompt has examples that explicitly show: tool returns full inbox → reply is `one, 2pm: "lunch friday?", wants to grab food, no agenda`.
**Aura port:** ~120 lines of prompt text into `AGENT.md` or `SOUL.md` overlay. Italianise the examples ("ok, controllo" / "fatto, ecco il cliente: Mario Rossi, P.IVA 01234..."). **Not in Phase-OP+.** Highest behavioural ROI for the lowest LOC cost.

### 7. `ask_user_clarification` as first-class tool

**Where:** `src/openhuman/tools/impl/agent/ask_clarification.rs:1-80`; listed in `orchestrator/agent.toml:109` and called out in `orchestrator/prompt.md:7` ("identify ambiguity, ask clarifying questions when needed").
**Rule:** the tool emits `[CLARIFICATION NEEDED]\n<question>\nOptions: <list>` and (in a full runtime) blocks on a user-response channel. Description text trains the LLM: "Use sparingly — only when the answer cannot be inferred from context."
**Why it matters for "Trova un cliente e stampalo":** the request is vague (which customer? print where? for what purpose?). With `ask_user_clarification` available and the orchestrator prompt telling the agent to look for ambiguity first, the model has an *exit ramp* from the "fetch + dump" pattern.
**Aura port:** ~150 LOC. Aura already has the Telegram outbound channel — a `clarify` tool would emit one Telegram bubble and wait for the next user message before resuming the loop. Note this needs careful integration with the agent loop's blocking model (Aura is `1 goroutine = 1 chat`, so a channel-receive is natural). **Not in Phase-OP+.**

### 8. Tools-versus-reply separation via delegation tools

**Where:** `orchestrator/agent.toml:47-69` — orchestrator delegates `code_executor`, `researcher`, etc. via auto-synthesised `delegate_*` tools, but the orchestrator itself "never writes code, executes shell commands, or directly modify files" (`orchestrator/prompt.md:3`).
**Rule:** the agent that *speaks to the user* and the agent that *runs `execute_code`* are different LLM calls with different prompts, different `max_result_chars`, and different tool budgets. The orchestrator only sees the code_executor's *synthesised reply*, capped at 16 000 chars (`code_executor/agent.toml:7`).
**Aura port:** ~400 LOC if done fully — this is a real architectural shift. Cheaper version: keep one agent loop but introduce a "tool-result handler" hop that summarises any oversized result via a sub-call to the same LLM with the summarizer prompt before continuing the main loop. The wiring already exists post-Phase-OP+ for the payload summarizer; this would generalise the same shape to *all* tool outputs above a configurable threshold (say 2 KiB), not just multi-MB ones.

## Adoption priority for Aura

| # | Pattern | Already in Phase-OP+? | LOC | Behavioural impact on "90-row dump" |
|---|---------|----------------------|-----|--------------------------------------|
| 1 | Hard 16 KiB inline cap | No | 80 | High — forces narrower re-runs |
| 6 | Texting-a-friend prompt contract | No | 120 (prompt) | Highest |
| 5 | Sub-agent role contract suffix | No | 10-30 (prompt) | Medium |
| 2 | `prefer_markdown_tool_output` | No | 40 | Medium |
| 7 | `ask_user_clarification` tool | No | 150 | Medium (for vague asks) |
| 4 | Summarizer prompt audit | Partial — verify clauses | 20 (prompt) | Medium |
| 3 | Payload summarizer + circuit breaker | **Yes** (US-P8-G) | 0 (audit only) | Already shipped |
| 8 | Full orchestrator/runner split | No | 400+ | High but heavy |

Recommended first slice: **#1 + #6 + #4 audit** as a single Phase. ~200 LOC + 140 lines of prompt, covers the byte-budget gap and the "tool result ≠ reply" gap simultaneously. #5 and #7 follow as small independent stories.
