# Agent-Loop Forced Finalization — diagnosis + research (2026-06-02)

Pre-planning research for a GSD slice. Source: live Phase-7 E2E surfaced a blocking UX bug; deep-research (19 sources, 18 confirmed / 7 refuted claims) on the fix.

## The bug (confirmed in code)

The `LlmAgent` run loop sometimes ends a run with **ZERO output tokens and NO assistant answer** — the user gets nothing. Reproduced live: "dammi le previsioni meteo a Caraglio" → agent issued `web_fetch`×5, then a `0 tok` footer and empty prose (~1 in 6 runs). The web tools are NOT at fault (10/10 meteo URLs fetch fine at tool level); the loop is.

Root cause — `internal/agent/llm_agent.go` has two termination paths that `return` with only a signal event, never a synthesized answer:

- **Budget trip** (`:124-127`): `ConsumeStep()` fails (max_steps/wallclock) → `terminalBudgetEvent(reason)` → `return`.
- **Dedup trip** (`dispatch`, `:224-227`): `Budget.BeforeToolCall` returns `dedup` (a repeated identical tool call whose result is stable across `AURA_LOOP_DEDUP_WINDOW=3`) → `terminalBudgetEvent("dedup")` → `return true`.

`terminalBudgetEvent` (`llm_agent_events.go:91`) carries only `Actions.Escalate=true` + `termination_reason`/`limit_hit` — **no prose**. The loop only ever produces an answer via the `text_response` terminal tool or the content-stop fallback (`:160-169`). So when dedup or budget fires, the run dies silent.

Aggravating factor: `internal/llm/openai_compat/client.go:141` hardcodes `tool_choice:"auto"`; the abstract `llm.Request` (`client.go:93`) has **no `ToolChoice` field** — so today there is no way to force a tool-free synthesis turn.

## Research verdict (cited)

- **No major framework guarantees a final answer by default — they throw.** LangGraph → `GraphRecursionError`; OpenAI Agents SDK → `MaxTurnsExceeded`. A loop terminates "normally" only when the model emits text with zero tool calls. (langchain GRAPH_RECURSION_LIMIT docs; openai-agents-python running_agents)
- **"Forced finalization" is THE opt-in fix:** on the limit, make ONE extra LLM call that synthesizes prose from steps gathered so far. Canonical: LangChain `early_stopping_method="generate"` (`"force"` = fixed stub; `"generate"` = final LLM synthesis). OpenAI equiv: `error_handlers={"max_turns": …}`. Caveat: the LangChain API itself is buggy/legacy (issues #16263, #24111) — replicate the *pattern*, don't depend on it.
- **Force prose with `tool_choice="none"`** (OpenRouter: "Disable tool usage"; OpenAI: "always produce an answer directly, never a function call"). ⚠️ DeepSeek-V4 intermittently emits tool-call *syntax as plain text* in `content`, and `auto`/`required` are flaky on some vLLM DeepSeek deployments → `none` is most robust, and parse `content` (not `tool_calls`) on that turn. OPEN: smoke-test DeepSeek/OpenRouter honoring `none`.
- **Dedup → recover, don't abort.** Anthropic's documented fix for stalled/empty turns: append a NEW user message ("Please continue") and re-call — do NOT retry the finished response, and NEVER add text in the same message right after a tool_result (teaches the model to end its turn). Apply at the dedup-veto point: inject "you already called X with these args; don't repeat; answer now" and continue one turn before finalizing. (Anthropic handling-stop-reasons; how-tool-use-works) — note: Anthropic `pause_turn` is server-side and NOT applicable to our client-executed tools.
- **Fan-out: budget-awareness beats a bigger budget.** arXiv 2511.17006 (Google/DeepMind, Nov 2025): raising the tool budget alone hits a ceiling; a `<budget>` used/remaining prompt block reaches comparable accuracy with ~10× less budget (≈40% fewer search calls, ≈31% lower cost). Figures are Gemini-2.5-Pro/BrowseComp-specific; direction solid, exact % may not reproduce on DeepSeek.
- **Avoid the one-shot latch.** CrewAI #1656: a `have_forced_answer` boolean that permanently disables the force-answer branch. Gate finalize on the counter, not a one-shot flag.

Refuted (do not rely on): LangGraph `RemainingSteps` as a *recommended* graceful-exit UX (0-3); `parallel_tool_calls=false` as a reliable fan-out cap (1-2); "raise recursion_limit" as the remedy (0-3).

## Proposed fix (priority-ordered)

- **P0 — `finalize()` forced synthesis.** Add a method that builds a tool-free request (full history incl. all tool results + a synthesis nudge) and emits a real `finalEvent`. Call it at BOTH termination paths instead of returning empty. Guarantees the user always gets an answer.
- **P0 — thread `ToolChoice`** through `llm.Request` → `openai_compat` (send `"none"` + omit tools when finalizing); parse `content` as the answer.
- **P1 — dedup recovery:** inject a user-role nudge and continue one turn before finalize (recover-not-abort); gate on counter, no one-shot latch.
- **P2 — `<budget>` used/remaining prompt block** to curb `web_fetch` fan-out.

## Candidate success criteria

1. A run that hits the dedup veto returns a non-empty final answer (reproduces q2c → fixed).
2. A run that exhausts `max_steps` returns a synthesized answer from gathered results, not an empty terminal event.
3. The finalize call uses `tool_choice="none"`; DeepSeek/OpenRouter returns prose (parsed from `content`) — smoke-tested live.
4. Dedup veto injects a recovery nudge + continues one turn before finalize; no one-shot latch (counter-gated).
5. E2E: "dammi le previsioni meteo a Caraglio" yields a weather answer across N consecutive runs (observed failure rate → 0).
6. goleak + `-race` clean; mutation spot-check on the new finalize/dedup-recovery branch; coverage ≥85%.

## Key sources
- https://reference.langchain.com/python/langchain-classic/agents/agent/AgentExecutor/early_stopping_method
- https://openai.github.io/openai-agents-python/running_agents/
- https://docs.langchain.com/oss/python/langgraph/errors/GRAPH_RECURSION_LIMIT
- https://platform.claude.com/docs/en/build-with-claude/handling-stop-reasons
- https://openrouter.ai/docs/guides/features/tool-calling
- https://arxiv.org/pdf/2511.17006
- https://github.com/crewAIInc/crewAI/issues/1656
