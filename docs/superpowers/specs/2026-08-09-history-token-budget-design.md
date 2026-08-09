# History budget — token-driven, not row-driven

**Date:** 2026-08-09 · **Scope:** the conversation-history load bound in `internal/conversations`
**Status:** design, approved in conversation, awaiting spec review
**Evidence:** measured live on the operator's Postgres, 2026-08-09 (queries reproduced in §3)

Every number below was measured on the running stack before it was written down, per the PRD-first
principle. Where something is unmeasured this document says so and does not decide it.

---

## 1. The problem

Aura discards most of every long conversation before the context ladder ever runs, and the amount
it discards has no relationship to the model's context window.

`Store.loadRecentTurns` bounds the fetch with a **row count** — `AURA_HISTORY_HARD_CAP_TURNS`,
which is `50` in `.env:56` and `50` as the compose default. The SQL
(`ListRecentTurnsBySeq`, `internal/db/queries/conversation_turns.sql:26`) keeps the earliest
`system` row plus the newest `hard_cap` non-system rows. Everything older is never read, never
tokenized, and never reaches L1/L2/L2.5.

That number is fixed. It stays `50` whether the model window is 32K or 1M.

## 2. Root cause

The load bound answers the wrong question. It bounds *work* (rows fetched, sidecars rehydrated,
turns tokenized) using a unit — rows — that carries no information about how much of the model's
window the history would occupy. A 50-turn bound is simultaneously far too generous for a 32K
window and absurdly conservative for a 1M one.

Hermes does not have this failure mode because its protected tail is derived from the window:
`tail_token_budget = summary_target_ratio × context_length`
(`agent/context_compressor.py::_find_tail_cut_by_tokens`). The budget is in tokens, and it scales.

## 3. What was measured

Live operator database, 2026-08-09. Token figures are `length(content)/4` — the same char/4
heuristic Hermes uses for its own budget arithmetic, so the numbers are comparable to a compression
threshold, not to a tokenizer.

Loss attributable to the 50-row cap, across every conversation longer than 50 turns:

| conversation | turns | loaded | never read | est. tokens | tokens lost | % lost | spilled tool turns |
|---|---|---|---|---|---|---|---|
| `019fa8ba` | 128 | 50 | 78 | 25,421 | 18,263 | **71.8%** | 0 |
| `019fa501` | 143 | 50 | 93 | 21,794 | 15,528 | **71.2%** | 20 |
| `019f83d0` | 124 | 50 | 74 | 11,149 | 4,417 | 39.6% | 8 |
| `019fdb4c` | 80 | 50 | 30 | 9,306 | 1,758 | 18.9% | 0 |

Reproduce with:

```sql
WITH t AS (SELECT conversation_id cid, seq, role, coalesce(content,'') c
           FROM aura.conversation_turns),
     r AS (SELECT *, row_number() OVER (PARTITION BY cid ORDER BY seq DESC) rn
           FROM t WHERE role <> 'system')
SELECT left(cid::text,8) conv, count(*) turns,
       count(*) FILTER (WHERE rn <= 50) loaded,
       count(*) FILTER (WHERE rn >  50) never_loaded,
       sum(length(c))/4 tok_total,
       coalesce(sum(length(c)) FILTER (WHERE rn > 50),0)/4 tok_lost
FROM r GROUP BY cid HAVING count(*) > 50 ORDER BY tok_lost DESC;
```

Two further measurements frame the loss:

- **The window is nearly empty.** `aura.settings` carries `AURA_MODEL_CONTEXT_WINDOW = 1000000` and
  `AURA_LLM_MAX_TOKENS = 19767`, so the L2 hard cap is
  `1,000,000 − max(19767, 20000) − 13,000 = 967,000` tokens. The largest conversation on the box is
  25,421 tokens — **2.6%** of that cap. Aura discards 72% of a conversation that fits 38 times over.
- **L1 is not the culprit.** On `019fa8ba`, `applyL1` evicted **0** turns: it rewrites only
  sidecar-backed `role='tool'` turns older than `AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS` (10) *within
  the loaded window*, and that conversation has no spilled tool turns at all. An earlier hypothesis
  in this investigation blamed L1; the measurement refuted it. The defect is the same category — a
  fixed row count blind to the window — one layer higher.

Cross-check on the char/4 estimate: `aura.cache_metrics` records 44,414 prompt tokens for
`019fa8ba` seq 70. That is consistent with a ~20K fixed prefix (system + tool manifest + always-block,
independently measured at 19,937 tokens for a trivial CLI question) plus a ~25K conversation. The
estimate is sound at this precision.

## 4. What this measurement does NOT prove

- It does not measure **answer quality**. It quantifies bytes never loaded, not whether the agent
  answered worse for lack of them. No A/B on answers was run.
- The token figures are a **char/4 estimate**, not tokenizer output. They are accurate enough to
  size a budget and to compare before/after; they are not exact billing numbers.
- It says nothing about conversations **under** 50 turns, which are unaffected.
- It does not establish that compaction is unnecessary in general — only that on *this routing*
  (OpenRouter, 1M window) no conversation on the box comes near the cap. See §8.

## 5. The design

Separate two bounds that are currently conflated into one row count.

| bound | purpose | value |
|---|---|---|
| **correctness cap** | exceeding it is a provider error | `ContextWindow − max(MaxOutputTokens, 20000) − 13000 − ProviderErrorReserveTokens`, floored by `smallWindowHardCapFloor` — **unchanged** |
| **history budget** | quality; how much history may occupy the window | **`min(correctnessCap, ContextWindow / 2)`** |

`ContextWindow / 2` is not a new constant. `smallWindowHardCapFloor` already returns `window / 2`
(`internal/conversations/context.go:139-144`); this promotes an existing small-window floor to the
general rule. The two coincide exactly on small windows, so small-window behaviour does not change:

| window | correctness cap | window/2 | history budget |
|---|---|---|---|
| 32,000 | 16,000 (floor) | 16,000 | 16,000 — unchanged |
| 200,000 | 167,000 | 100,000 | 100,000 |
| 1,000,000 | 967,000 | 500,000 | 500,000 |

### Why 50%

The Chroma *context rot* research (18 models) finds that performance degrades with input length
even on trivial tasks, that **a single distractor** already lowers accuracy against a
no-distractor baseline, and that on LongMemEval focused prompts beat full prompts substantially.

**The paper does not prescribe 50%, or any percentage.** It establishes the direction — less and
more relevant beats more — and the 50% figure is this project's engineering choice on top of that
evidence, recorded here as such. It is a knob, not a finding.

### Load path

1. **SQL stops deciding.** `ListRecentTurnsBySeq` returns the newest rows up to a work ceiling,
   without rehydrating anything. The head (`system` seq 1) keeps its existing protection.

   **The ceiling must rise, or this change does nothing.** It is `50` today, so leaving it there
   would fetch 50 rows and let the token budget trim *further* — the 72% loss would survive the
   fix untouched. The default moves from `50` to `1000` (the existing validated maximum,
   `maxHistoryHardCapTurns`), and `.env:56`, which sets `50` explicitly, must be updated with it;
   the compose default (`compose.yaml:104`) likewise. The ceiling is then a genuine
   work bound — an upper limit on rows transferred — and the token budget is the criterion.

   A 1000-row fetch is affordable precisely because of step 3: rows arrive without sidecar
   content, and spilled turns carry an empty `content` column, so the transferred bytes are
   bounded by the inline turns.

   **Residual limit, stated plainly:** a conversation longer than 1000 turns still loses its
   oldest rows to the ceiling rather than to the budget. At the measured ~200 tokens/turn, 1000
   turns is ~200,000 tokens — inside a 500,000 budget — so on a 1M window the ceiling, not the
   budget, would be the binding constraint past that length. No conversation on the box is close
   (longest: 143 turns), so this is a known bound, not a fix deferred. Going beyond it would mean
   raising `maxHistoryHardCapTurns` itself — a code change, not a config one — and should be driven
   by a measurement showing a real conversation hitting it.
2. **The cut happens in Go, against the history budget**, porting the four invariants of Hermes's
   `_find_tail_cut_by_tokens`:
   - the token budget is the primary criterion;
   - a **message-count floor** keeps a bounded run of recent turns verbatim even when the budget is
     exhausted;
   - a **soft ceiling at 1.5× budget** prevents cutting inside a single oversized message (a file
     read, a large tool result);
   - the cut **never lands inside a tool_call/result group**, and the most recent user message is
     always inside the retained window.
3. **Sidecars rehydrate only for surviving turns.** Today `loadRecentTurns` reads the sidecar for
   every fetched row (`internal/conversations/store.go:454-470`), including rows the ladder then
   discards. Moving rehydration after the cut makes this path cheaper even while it loads more
   conversation.

`AURA_HISTORY_HARD_CAP_TURNS` survives as the **work ceiling** in step 1 — a bound on rows fetched,
not the criterion for what enters the context. Its existing validation range (min 4, max 1000,
`internal/conversations/context.go:36-38`) is unchanged; only its default and the two configured
values move to `1000`, as step 1 requires.

## 6. Testing

**Regression measure (the acceptance criterion).** The §3 query is the before/after instrument.
After the change, with a 1M window, `tokens lost` must be **0** for all four conversations —
`019fa8ba` 18,263 → 0, `019fa501` 15,528 → 0, `019f83d0` 4,417 → 0, `019fdb4c` 1,758 → 0.

**Unit tests (pure, no database)** on the cut function, one per invariant:

- budget respected: a history exceeding the budget is cut to at or under it;
- message floor: with a budget of ~0, the floor count of recent turns still survives;
- soft ceiling: a single message larger than the budget is retained whole rather than split, up to
  1.5×;
- tool-group integrity: a cut that would land between an assistant `tool_calls` turn and its
  `role='tool'` results moves to the group boundary;
- last user message: always present in the retained window;
- small-window equivalence: at `ContextWindow = 32000` the budget equals today's
  `smallWindowHardCapFloor`, so the retained set is byte-identical to current behaviour.

**Integration** (`db_integration`): load a seeded >50-turn conversation and assert every turn is
present at a 1M window, and that the cut lands on the budget at a small one.

Coverage floor for the touched package is the project's 85%, per CLAUDE.md.

## 7. Files

| file | change |
|---|---|
| `internal/conversations/context.go` | history-budget computation; `ContextConfig` gains the budget derivation |
| `internal/conversations/store.go` | `loadRecentTurns` — cut before sidecar rehydration |
| `internal/conversations/store_branch.go` | `loadRecentBranchTurns` — same cut, path-aware variant |
| `internal/db/queries/conversation_turns.sql` | `ListRecentTurnsBySeq` — work ceiling, no criterion |
| `internal/config/config.go`, `config_knobs.go` | `AURA_HISTORY_HARD_CAP_TURNS` default `50` → `1000` |
| `.env:56`, `compose.yaml:104` | same, explicitly configured values |

No migration. No new environment variable: the budget derives from `AURA_MODEL_CONTEXT_WINDOW`,
which already exists and is already DB-driven via `aura.settings`.

`context.go` is 590 LOC and `store.go` 574 against the project's 600-LOC cap, so the cut logic
lands in its own file (`internal/conversations/context_budget.go`) rather than growing either past
it. Per the deep-refactor-on-touch rule, any dead code or duplication found in the touched files is
removed in the same commit.

## 8. Out of scope, and why

**Compaction of the middle — rolling summary or graph retrieval — is not part of this change.**

Spike 0 (§3) established that on the current routing no conversation approaches the budget: at
~200 tokens/turn, a 500,000-token budget admits roughly 2,500 turns. Compressing the middle answers
"the window is full"; this window is 95% empty. Building compaction now would be building for a
condition that has not been observed.

That condition becomes real on the **local model**. `aura-llm` serves `qwen3.5-9b`, whose window is
far smaller than OpenRouter's 1M; at 32K the budget is 16,000 tokens and a 25,421-token
conversation genuinely does not fit. Routing is DB-driven (`aura.settings`), so this is a
configuration change away, not a rewrite.

Two spikes are therefore **deferred, not cancelled**, to be run against a simulated small window:

- **Spike 1 — lossless graph.** Index conversation turns as `:Message` with embeddings in a
  disposable ArcadeDB and retrieve by relevance at compose time. Requires porting
  `ShortTermMemory` (from `neo4j-labs/agent-memory`: `add_message`, `search_messages`,
  `get_context`, `ConversationSummary`) into `cmd/arcadedb-mcp`, which today carries only long-term
  tools.
- **Spike 2 — Hermes rolling summary.** Cumulative one-exchange-at-a-time summarization with
  cursor, defrag and 3-strike skip, answering from the summary alone.

The context-rot evidence bears on that future choice and is recorded here for it: if a single
distractor measurably lowers accuracy, then *retrieve only what is relevant* is better motivated
than *summarize everything*. That is an argument, not a measurement, and the spikes are what would
settle it.

A related finding from the same investigation, **not addressed here**: token and cost accounting
records only the terminal assistant turn of a round. Conversation `019fa8ba` has 57 assistant turns
and 7 `aura.cache_metrics` rows; `019fa501` has 63 and 23. Roughly 50 LLM calls on one conversation
carry no token or cost record, so `total_cost_usd` understates spend by about the tool-iteration
factor. It needs its own change.
