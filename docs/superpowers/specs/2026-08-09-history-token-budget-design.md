# Context composition — measure it first, then bound it

**Date:** 2026-08-09 · **Scope:** what occupies Aura's context window, and the history load bound
**Status:** design, approved in conversation, awaiting spec review
**Supersedes:** the first revision of this file (`d477447eb`), whose token figures were char/4
estimates and whose priority order was wrong. Both are corrected here, with the measurements that
corrected them.

Every number below was measured on the running stack before it was written down, per the PRD-first
principle. Where something is unmeasured this document says so and does not decide it.

---

## 1. The problem, in the order the measurements found it

Aura discards most of every long conversation, and separately carries a scaffolding prefix that is
two to nine times larger than the conversation itself. Nothing reports either. The second is the
larger effect and was found only because the first was measured wrong.

## 2. What was measured

Live operator stack, 2026-08-09. Token counts are **real**, from llama.cpp's `/tokenize` on
`aura-llm` (qwen3.5-9b) and from OpenRouter's own `usage`, recorded in `aura.cache_metrics`. The
earlier char/4 heuristic is retained below only to show its error.

### 2.1 History discarded by the row cap

`Store.loadRecentTurns` bounds the fetch with a **row count** — `AURA_HISTORY_HARD_CAP_TURNS`,
`50` in `.env:56` and as the compose default. `ListRecentTurnsBySeq`
(`internal/db/queries/conversation_turns.sql:26`) keeps the earliest `system` row plus the newest
`hard_cap` non-system rows. Everything older is never read, never tokenized, and never reaches
L1/L2/L2.5. The number is fixed: it stays `50` whether the window is 32K or 1M.

| conversation | turns | loaded | never read | real tokens | tokens lost | % lost | char/4 said | estimate error |
|---|---|---|---|---|---|---|---|---|
| `019fa8ba` | 128 | 50 | 78 | 37,989 | **26,933** | 70.9% | 31,002 | −18.4% |
| `019fa501` | 143 | 50 | 93 | 31,198 | **22,633** | 72.5% | 24,713 | −20.8% |
| `019f83d0` | 124 | 50 | 74 | 21,635 | 10,308 | 47.6% | 16,003 | −26.0% |
| `019fdb4c` | 80 | 50 | 30 | 13,410 | 2,971 | 22.2% | 12,427 | −7.3% |

char/4 undercounts by 7–26%. It is not adequate for sizing a budget, and this document does not use
it for one.

The L2 hard cap is `1,000,000 − max(19767, 20000) − 13,000 = 967,000` tokens
(`AURA_MODEL_CONTEXT_WINDOW` and `AURA_LLM_MAX_TOKENS` from `aura.settings`). The largest
conversation on the box is 37,989 tokens — **3.9%** of that cap.

**L1 is not the culprit.** On `019fa8ba`, `applyL1` evicted **0** turns: it rewrites only
sidecar-backed `role='tool'` turns older than `AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS` (10) *within
the loaded window*, and that conversation has no spilled tool turns. An earlier hypothesis in this
investigation blamed L1; the measurement refuted it.

### 2.2 The prefix dominates, and swings

Modelling `prompt_tokens = prefix + ratio × history` against all 64 usable `cache_metrics` rows
gives **R² = 0.22**. The model does not hold, and the residuals say why: the non-history part of
the prompt is not constant.

Implied prefix (`real prompt_tokens` − reconstructed history), within single conversations:

| conversation | prefix range across its own turns |
|---|---|
| `019fdb4c` | **7,136 → 75,368** |
| `019fa8ba` | 25,732 → 42,033 |
| `019fa501` | 10,214 → 22,200 |

A tokenizer-proxy mismatch is roughly constant and cannot produce a 10× swing turn-to-turn inside
one conversation, so this finding survives the proxy's imprecision.

### 2.3 The cause, isolated by experiment

One live turn through the cockpit, same conversation, cost $0.0019:

| turn | prompt tokens |
|---|---|
| trivial question, no tool loading | 9,865 |
| after one `tool_search` promoting 6 tool schemas | **22,700** |

**+12,835 tokens for 6 tools ≈ 2,100 tokens per promoted schema.** Thirty promoted tools would add
~63,000, which is the order of the 75,368 observed on `019fdb4c`. Promotion is per-run
(`activated` is the per-run set), so the manifest re-inflates on each user turn and deflates again
at the next, which is exactly the swing shape observed.

This is the deferred-tool pattern working as designed (CLAUDE.md §Tool design): schemas are hidden
until `tool_search` loads them, protecting the cached manifest. The defect is not that it grows —
it is that **nothing measures it**, so its size was never a design input.

### 2.4 Reference point: what a tuned agent's context looks like

Claude Code's own `/context`, same 1M window, measured in this session:

| category | tokens | share of window |
|---|---|---|
| System prompt | 4.1k | 0.4% |
| System tools (15 tools) | 16.2k | 1.6% |
| Custom agents | 2.9k | 0.3% |
| Memory files | 18.2k | 1.8% |
| Skills | 9.9k | 1.0% |
| **Messages** | **283.9k** | **28.4%** |
| total used | 334.7k | 33% |

Scaffolding is **15%** of what is used; the work is 85%. Aura is inverted: scaffolding of 9,865 to
75,368 tokens against conversations of 13,410 to 37,989, of which 22–72% is then discarded.

Two comparisons worth keeping:

- Claude Code's **entire** 15-tool system manifest is 16.2k. Six promoted Aura tools cost 12.8k.
  Aura's schemas are roughly twice as expensive per tool, against a favourable comparator (this
  manifest includes several very long descriptions).
- Both use the deferred pattern. The difference is that Claude Code exposes the breakdown to its
  operator and Aura exposes it to nobody.

### 2.5 What is NOT broken

Prompt caching is healthy: **64.5%** of prompt tokens cached across the 100 recorded calls, 15 of
which are zero. The zero-cache calls examined were first calls in fresh conversations, where there
is nothing to cache. An earlier suspicion in this investigation that manifest churn was destroying
the cache is not supported.

## 3. What these measurements do NOT prove

- No **answer quality** was measured. This quantifies tokens loaded, discarded and carried — not
  whether the agent answered worse. No A/B on answers was run.
- The history token counts use **qwen3.5-9b's tokenizer** as a proxy; the shipped model is
  deepseek-v4-flash via OpenRouter, whose exact counts are only knowable from returned `usage`.
  The proxy is exact-per-tokenizer, not exact-per-shipped-model.
- The prefix figures are **implied** (`real prompt_tokens` − reconstructed history), not observed
  directly. They are attributed to tool-schema promotion by §2.3's experiment, which reproduces the
  mechanism and the order of magnitude, but does not prove promotion is the *only* contributor.
  Isolating the remainder is what Part A is for.
- `~2,100 tokens per schema` is one sample of six tools. Per-tool cost certainly varies.
- Conversations under 50 turns are unaffected by §2.1.

## 4. Part A — the context breakdown (first)

Nothing in Aura answers "what are these 22,700 tokens made of". Until it does, every other context
decision is guesswork — including this document's own first revision, which optimised the smaller
of the two effects because the larger one was invisible.

Port the category model of Hermes's `compute_session_context_breakdown`
(`agent/context_breakdown.py`) to Go, mapped onto Aura's actual composition:

| category | Aura source |
|---|---|
| system prompt | `messages[0]`, the agent's own prepended system message |
| always-block / skills | `messages[1]`, `ContextConfig.AlwaysBlock` |
| tool definitions | `reg.RenderToolDefs(activated)`, excluding MCP-named tools |
| MCP | the same render, tools carrying the MCP name prefix |
| promoted schemas | the subset of `activated` that `tool_search` loaded this run |
| transient context | `ContextConfig.TransientContext` |
| volatile hints | `prompt.Budget.block()` — budget, workspace, time, sources, deferred roster |
| conversation | the post-ladder history |

**Where.** `PromptBuilder.buildBase` is the single chokepoint that assembles every wire request
(D-01, `internal/agent/prompt/builder.go:146`). The breakdown is computed there, over the same
values the request is built from, so it cannot drift from what is actually sent.

**How tokens are counted — no estimates where a real count exists.**

- Local llama.cpp path: `/tokenize` returns the exact count for the serving model. Verified working
  on `aura-llm:8084` and `aura-llama-embed:8081`.
- OpenRouter path: no pre-call tokenizer exists for deepseek, so the pre-call figure is an
  estimate. But the response carries counts from **the model's own native tokenizer** — per
  OpenRouter's usage-accounting contract, automatically, in the last SSE message; the
  `usage: {include: true}` and `stream_options: {include_usage: true}` parameters are deprecated
  no-ops. The reconciliation target is therefore exact, and **the estimate's error is measured on
  every call and exposed** rather than assumed. This also retires the guesswork behind
  `ProviderErrorReserveTokens`, which today reserves headroom for an estimation error nobody
  measures.

**The wire boundary is already correct — do not rebuild it.** `internal/llm/openai_compat/usage.go`
parses `cost`, `prompt_tokens_details.cached_tokens` and `cache_write_tokens`, keeping reads and
writes distinct because conflating them misreports the hit ratio. `internal/llm/prices.go:38-39`
prefers the provider's reported `cost` and falls back to the price table only when it is nil
(D-18). `stream_options` is set only on the llama.cpp target, matching the no-op rule above.

**What it emits.** Per-turn: a `aura_agent_context_tokens{category}` gauge on the existing obs
registry (`internal/obs/catalog.go`), and the same payload on the `agent.turn` span already wrapping
the loop. Both surfaces are already scraped/collected for the `serve` path — verified live this
session: an agent turn takes the family from 0 to 43 series on `:9464`.

**Prerequisite defect — persistence, not parsing.** The usage exists on the `llm.Usage` boundary
for *every* call; it is written only for the terminal assistant turn of a round. `019fa8ba` has 57
assistant turns and 7 `aura.cache_metrics` rows, `019fa501` 63 and 23 — roughly 50 LLM calls on one
conversation carry no token or cost record, so `total_cost_usd` understates spend by about the
tool-iteration factor and the reconciliation above has nothing to reconcile against on intermediate
calls. Persisting what is already captured is part of Part A, not a follow-up, and is a smaller
change than parsing would have been.

**Reasoning contributes nothing to context, and the breakdown must not invent a category for it.**
Chain-of-thought is persisted DISPLAY-ONLY (`internal/conversations/store_append.go:46`) and the
projection that reads it is documented as "structurally incapable of carrying CoT back into the
model context" (`store_reasoning.go:14`). The mechanism is `turnToMessage`
(`store_helpers.go:96-106`), which builds `llm.Message{Role, Content, ToolCallID, ToolCalls}` — it
has no reasoning field, so reasoning cannot reach the wire. The ladder agrees: `totalTokens` sums
content, tool calls and tool-call id only.

This deployment reasons heavily — 62 turns carry `reasoning_duration_ms`, the largest 129,516 ms —
and none of it occupies context. `completion_tokens_details.reasoning_tokens` is therefore an
**output-accounting** question, not a context one, and stays out of this spec. `cache_write_tokens`
stays out for its own reason: its owner is already assigned and nothing measured here implicates it.

## 5. Part B — the history budget (second)

Separate two bounds that are currently conflated into one row count.

| bound | purpose | value |
|---|---|---|
| **correctness cap** | exceeding it is a provider error | `ContextWindow − max(MaxOutputTokens, 20000) − 13000 − ProviderErrorReserveTokens`, floored by `smallWindowHardCapFloor` — **unchanged** |
| **history budget** | quality; how much history may occupy the window | **`min(correctnessCap, ContextWindow / 2)`** |

`ContextWindow / 2` is not a new constant: `smallWindowHardCapFloor` already returns `window / 2`
(`internal/conversations/context.go:139-144`). This promotes an existing small-window floor to the
general rule, so small-window behaviour is unchanged:

| window | correctness cap | window/2 | history budget |
|---|---|---|---|
| 32,000 | 16,000 (floor) | 16,000 | 16,000 — unchanged |
| 200,000 | 167,000 | 100,000 | 100,000 |
| 1,000,000 | 967,000 | 500,000 | 500,000 |

### Why 50%

Chroma's *context rot* research (18 models) finds performance degrading with input length even on
trivial tasks, that **a single distractor** lowers accuracy against a no-distractor baseline, and
that focused prompts beat full prompts substantially on LongMemEval.

**The paper prescribes no percentage.** It establishes the direction — less and more relevant beats
more — and 50% is this project's engineering choice on top of that evidence, recorded as such. It
is a knob, not a finding.

### Load path

1. **SQL stops deciding.** `ListRecentTurnsBySeq` returns the newest rows up to a work ceiling,
   rehydrating nothing. The `system` head keeps its existing protection.

   **The ceiling must rise, or this change does nothing.** It is `50` today, so leaving it there
   would fetch 50 rows and let the token budget trim *further* — the loss in §2.1 would survive the
   fix untouched. The default moves from `50` to `1000` (the existing validated maximum,
   `maxHistoryHardCapTurns`), and both configured values — `.env:56` and `compose.yaml:104` — move
   with it.

   **Residual limit, stated plainly:** a conversation beyond 1000 turns still loses its oldest rows
   to the ceiling rather than the budget. At the measured **219** real tokens/turn (104,232 tokens
   over 475 turns, §2.1), 1000 turns is ~219,000 tokens — inside a 500,000 budget — so past that
   length the ceiling, not the budget, binds. The longest conversation on the box is 143 turns.
   Going further means raising
   `maxHistoryHardCapTurns` itself, a code change, and should be driven by a measurement showing a
   real conversation hitting it.

2. **The cut happens in Go, against the history budget**, porting the four invariants of Hermes's
   `_find_tail_cut_by_tokens`:
   - the token budget is the primary criterion;
   - a **message-count floor** keeps a bounded run of recent turns verbatim even when the budget is
     exhausted;
   - a **soft ceiling at 1.5× budget** prevents cutting inside a single oversized message;
   - the cut **never lands inside a tool_call/result group**, and the most recent user message is
     always inside the retained window.

3. **Sidecars rehydrate only for surviving turns.** Today `loadRecentTurns` reads the sidecar for
   every fetched row (`internal/conversations/store.go:454-470`), including rows the ladder then
   discards. Moving rehydration after the cut makes this path cheaper even while it loads more
   conversation — which is what makes the 1000-row ceiling affordable.

`AURA_HISTORY_HARD_CAP_TURNS` survives as the **work ceiling** — a bound on rows transferred, not
the criterion for what enters the context.

## 6. Testing

**Part A.** The breakdown is a pure function of (system message, always-block, rendered tool defs,
transient context, volatile hints, history), so its unit tests need no database and no LLM: given a
constructed set, assert each category's token count and that the categories sum to the total the
builder would send. One live assertion completes it: drive a turn through `serve`, and the sum of
the emitted categories must reconcile with the provider's returned `prompt_tokens` within the
measured estimate error — the §2.3 probe (9,865 → 22,700 across a 6-tool promotion) is the fixture.

**Part B.** The §2.1 query is the before/after instrument, run with real tokenization
(`/tokenize`), not char/4. After the change, at a 1M window, tokens lost must be **0** for all four
conversations — `019fa8ba` 26,933 → 0, `019fa501` 22,633 → 0, `019f83d0` 10,308 → 0, `019fdb4c`
2,971 → 0.

Unit tests on the cut function, one per invariant: budget respected; message floor survives a
near-zero budget; a single oversized message is retained whole up to 1.5×; a cut that would land
inside a tool group moves to the group boundary; the last user message is always retained; and at
`ContextWindow = 32000` the retained set is byte-identical to today's behaviour.

Integration (`db_integration`): a seeded >50-turn conversation loads completely at a 1M window, and
cuts on the budget at a small one.

Coverage floor for the touched packages is the project's 85%, per CLAUDE.md.

## 7. Files

| file | change |
|---|---|
| `internal/agent/prompt/context_breakdown.go` | **new** — the category model and its token counts |
| `internal/agent/prompt/builder.go` | emit the breakdown at the `buildBase` chokepoint |
| `internal/obs/catalog.go` | `aura_agent_context_tokens{category}` instrument |
| `internal/agent/metrics.go` | record it; persist per-call usage on every LLM call, not only terminal |
| `internal/conversations/context_budget.go` | **new** — history budget and the tail cut |
| `internal/conversations/context.go` | budget derivation in `ContextConfig` |
| `internal/conversations/store.go` | `loadRecentTurns` — cut before sidecar rehydration |
| `internal/conversations/store_branch.go` | `loadRecentBranchTurns` — same cut, path-aware |
| `internal/db/queries/conversation_turns.sql` | `ListRecentTurnsBySeq` — work ceiling, no criterion |
| `internal/config/config.go`, `config_knobs.go` | `AURA_HISTORY_HARD_CAP_TURNS` default `50` → `1000` |
| `.env:56`, `compose.yaml:104` | same, explicitly configured values |

No migration. No new environment variable: the budget derives from `AURA_MODEL_CONTEXT_WINDOW`,
already DB-driven via `aura.settings`.

`context.go` is 590 LOC and `store.go` 574 against the 600-LOC cap, so new logic lands in its own
files rather than growing either past it. Per the deep-refactor-on-touch rule, dead code and
duplication found in touched files is removed in the same commit.

## 8. Out of scope, with reasons

**Compaction of the middle — rolling summary or graph retrieval — is not part of this change.** At
the measured 219 tokens/turn, a 500,000-token budget admits roughly 2,280 turns; the longest
conversation on the box is 143. Compressing the middle answers "the window is full", and the
largest conversation here occupies 3.9% of the correctness cap. Building it now would be building
for a condition never observed.

That condition is real on the **local** model: `aura-llm` serves `qwen3.5-9b` with a far smaller
window, where a 32K setting yields a 16,000-token budget that a 37,989-token conversation does not
fit. Routing is DB-driven (`aura.settings`), so this is a configuration change away.

Two spikes are therefore **deferred, not cancelled**, to be run against a simulated small window:

- **Spike 1 — lossless graph.** Index turns as `:Message` with embeddings in a disposable ArcadeDB
  and retrieve by relevance at compose time. Requires porting `ShortTermMemory` (from
  `neo4j-labs/agent-memory`: `add_message`, `search_messages`, `get_context`, `ConversationSummary`)
  into `cmd/arcadedb-mcp`, which today carries only long-term tools.

  The target shape is that project's own graph model (`img/memory-graph-model.png`), three planes:

  | plane | model | what Aura has today |
  |---|---|---|
  | short-term | `Conversation -FIRST_MESSAGE-> Message -NEXT_MESSAGE-> Message` | `aura.conversation_turns` in Postgres — the rows §2.1 discards |
  | long-term | `Entity -WORKS_AT-> Entity`, `Entity -MENTIONED_IN-> Message` | `cmd/arcadedb-mcp` facts/entities/recall — already built |
  | reasoning | `Message -TRIGGERED-> ReasoningTrace -HAS_STEP-> ReasoningStep -USED_TOOL-> ToolCall -CALL_OF-> Tool` | `internal/reasoningtrace` — a flat `Record(stage, fields)` to an encrypted, row-capped file sink |

  The reasoning plane is the part worth noting now, because it bears on a finding this
  investigation already made and did not act on. Aura's reasoning trace is **write-only**: neither
  the agent nor an operator can query it. Modelling it as a graph would make it retrievable
  *without* putting it in context — which is the right shape given §4's finding that CoT is
  deliberately excluded from the wire. It is also where grinding would become visible: the 22
  tool-calls-in-one-round measured on `019fa8ba` are reconstructable today only by hand-querying
  Postgres.

  `MENTIONED_IN` and `RETRIEVED` are the edges that make this more than three separate stores —
  they link a retrieved entity back to the message and the tool call that produced it. That is
  provenance Aura currently has nowhere.
- **Spike 2 — Hermes rolling summary.** Cumulative one-exchange-at-a-time summarization with
  cursor, defrag and 3-strike skip, answered from the summary alone.

- **Spike 3 — durable artifacts instead of carried context** (`snarktank/ralph`). A third answer to
  the same question, and the only one that makes compaction unnecessary rather than cheaper: do not
  compress the middle, do not index it — **do not carry it**. Ralph spawns a fresh instance per
  iteration with clean context; the memory between iterations is git history, an append-only
  `progress.txt`, a `prd.json` of stories with `passes: true/false`, and an `AGENTS.md` the tool
  re-reads. It halts when every story passes.

  **This does not apply to interactive chat** — a user mid-conversation expects continuity, and
  discarding context every turn would break it. It applies squarely to Aura's autonomous paths: 13
  rows in `aura.scheduler_tasks`, the swarm children, and `aura.agent_job_runs`, which have no
  human waiting on continuity.

  There it lands on a real gap. Aura's nearest analogue to `prd.json` is `TodoTool`
  (`internal/agent/tools/todo.go`), whose own comment names the need — "the coherence aid an
  autonomous swarm/cron run lacks today" — but whose state is `byID map[string][]todoItem` behind a
  mutex, session-scoped and evicted at session end. **It does not survive the process.** The
  artifact exists as a concept and not as durable state, which is the one thing Ralph's design
  depends on.

The context-rot evidence bears on the choice between these and is recorded for it: if a single
distractor measurably lowers accuracy, *retrieve only what is relevant* is better motivated than
*summarize everything*, and *carry nothing and re-derive* is better motivated still where a human
is not waiting. That is an argument, not a measurement; the spikes are what would settle it.

**Reducing per-schema tool cost** is out of scope here and is the most likely thing Part A will
make actionable. ~2,100 tokens per promoted schema against Claude Code's ~1,080 average is a signal,
not yet a verdict: it is one sample of six tools, and no per-tool breakdown exists until Part A
ships.
