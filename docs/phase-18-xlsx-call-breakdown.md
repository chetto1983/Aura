# Phase 18 — D-03 characterization probe: live xlsx E2E per-call ledger breakdown

One live xlsx E2E run against DeepSeek-V4 (OpenRouter) through the **production `aura chat` surface**
(not the eval harness), dumped from the `aura.tool_invocations` ledger (migration 0011). This grounds
the provisional ~5-call/<40s steady-state target the 18-04 gate asserts (PRD amendment #55/D-01,
plan 18-01 Task 3 / D-03).

- **Date:** 2026-06-06, 12:46:42 → 12:49:04 UTC
- **Conversation:** `019e9cf8-9bca-7c7e-88be-e740a477fc1c`
- **Prompt (natural, no "skill"/"install" words):** "Fammi un file Excel con il mercato di Yahoo Finance di oggi. Voglio un .xlsx vero che si apra in un foglio di calcolo, con i dati di oggi."
- **Artifact:** `Mercato_Yahoo_2026-06-06.xlsx` (8,622 bytes, sheet `Mercato Oggi`, A1:H51 — 5 themed groups, 32 tickers, today's data; opened + content-verified via openpyxl read-back; archived to the probe scratch dir)
- **Run shape:** skill-REUSE authoring (the `xlsx` + `yahoo-finance` instruction skills were already present under `~/.aura/skills/export/.agents/skills/` from prior sessions — no `npx skills add` occurred). The model: read both skills → wrote a 10KB openpyxl script → ran it (22.6s live Yahoo fetch) → fixed + re-ran (5.7s) → verified via 4 read-back calls → deleted its own script.

## Headline numbers

| Metric | Value |
|---|---|
| Wall-clock (max `ended_at` − min `started_at`) | **142.8 s** |
| Tool dispatches (`event_kind='end'` rows) | **21** (all `status=ok`) |
| Distinct `request_id`s | **1** ⚠ see "Metric finding" |
| LLM roundtrips (gap-derived estimate) | **≈19** (18 tool-dispatching assistant turns — 3 of the 21 calls were batched in the same assistant message — + 1 final reply) |
| Total tool exec time | 34.2 s (24%) |
| Total LLM thinking time (inter-call gaps) | 108.6 s (76%) |

## ⚠ Metric finding — `request_id` is NOT an LLM-roundtrip proxy

The plan assumed "total distinct request_ids = the LLM-roundtrip proxy". **Empirically false**: the
runner assigns ONE `request_id` per *user turn*, so this 21-call, ~19-roundtrip run has
`count(DISTINCT request_id) = 1`. The 18-04 steady-state gate MUST NOT assert
`distinct request_id count <= ~5` — that passes trivially for any single-prompt run.

**Replacement gate metric (grounded):** within the 2nd-run window, read from the ledger:
1. `count(*) FILTER (WHERE event_kind='end')` — tool-dispatch count (primary budget)
2. roundtrip estimate = end-events whose `started_at − lag(ended_at)` gap > 0.5 s, + 1 final reply (secondary, diagnostics)
3. wall-clock = `max(ended_at) − min(started_at)` (the <40 s floor)

## Per-call sequence (end events)

| seq | tool | exec ms | LLM gap before (s) | what it did |
|---|---|---|---|---|
| 2 | current_time | 7 | — | timezone Europe/Rome |
| 4 | skill | 2 | 0.00 (batched) | `action=list` |
| 6 | skill | 7 | 3.84 | `action=use name=yahoo-finance` |
| 8 | skill | 1 | 0.00 (batched) | `action=use name=xlsx` |
| 10 | fs_glob | 54 | 2.53 | `**/yahoo-finance/**` |
| 12 | shell_exec | 1,635 | 2.21 | probe skills export dir + `which yf` |
| 14 | shell_exec | 102 | 6.69 | `ls` yahoo-finance skill dir |
| 16 | shell_exec | 123 | 3.76 | `ls -la` yahoo-finance skill dir |
| 18 | fs_read | 2 | 0.00 (batched) | read yahoo-finance SKILL.md |
| 20 | fs_write | 4 | 31.34 | write `make_market_xlsx.py` (~10 KB, generated inline) |
| 22 | shell_exec | 22,571 | 1.72 | run script — live Yahoo fetch, v1 |
| 24 | fs_write | 19 | 21.12 | rewrite script (~8.9 KB fix, generated inline) |
| 26 | shell_exec | 5,681 | 2.82 | re-run script — produces the .xlsx |
| 28 | shell_exec | 112 | 2.70 | `ls -la` the artifact |
| 30 | shell_exec | 740 | 3.29 | openpyxl read-back verify #1 |
| 32 | shell_exec | 1,315 | 5.27 | openpyxl read-back verify #2 |
| 34 | shell_exec | 788 | 5.08 | read-back w/ UTF-8 wrapper #1 |
| 36 | shell_exec | 760 | 7.65 | read-back w/ UTF-8 wrapper #2 |
| 38 | read_tool_output | 4 | 3.10 | page verify output |
| 40 | shell_exec | 95 | 3.15 | `del` own script (cleanup) |
| 42 | shell_exec | 126 | 2.37 | `rm -f` own script (cleanup, retry) |

Per-tool totals: shell_exec ×12 (34.0 s, max 22.6 s), skill ×3, fs_write ×2, fs_glob/fs_read/read_tool_output/current_time ×1.

## Steady-state implication (the 18-04 target)

The two dominant costs of this authoring run disappear under 7e snippet reuse:
- **Inline script generation** (the 31.3 s + 21.1 s LLM gaps before the two `fs_write`s — the model
  emitting ~19 KB of Python): a saved snippet is executed by-path, nothing is regenerated.
- **Author/fix/verify churn** (write → run → rewrite → re-run → 4 verify calls): a vetted snippet
  needs one run + at most one verify.

Projected steady-state window: `skill action=use` (snippet frame) → host `shell_exec` by-path
(~6–23 s, dominated by the live Yahoo fetch) → 1 verify → final reply ≈ **4–5 tool dispatches,
~5–6 LLM roundtrips, ~25–40 s wall-clock**.

**Grounded 18-04 acceptance:** 2nd-run window ≤ **6 tool dispatches** (`event_kind='end'`) AND
wall-clock < **40 s**. The ~5-call intuition survives, but counted as ledger end-events, never as
distinct `request_id`s.

## Reproduce

```bash
docker compose up -d searxng
set -a; . ./.env; set +a
export SEARXNG_URL=http://127.0.0.1:18080/search
export AURA_RUN_DIR=<inspectable scratch>
printf '%s\n' "Fammi un file Excel con il mercato di Yahoo Finance di oggi. Voglio un .xlsx vero che si apra in un foglio di calcolo, con i dati di oggi." "/exit" | ./aura chat new
# then: SELECT ... FROM aura.tool_invocations WHERE conversation_id='<id>' ORDER BY seq;
```
