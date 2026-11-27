# Claude Messages wire probe (Task 1 of the anthropic-provider plan)

- **Date:** 2026-11-27, from the dev host (WSL), against `api.anthropic.com`.
- **Plan:** `docs/superpowers/plans/2026-11-27-anthropic-provider.md`, Task 1.
- **Probe:** `spikes/anthropic-provider/probe/main.go`, built on `github.com/anthropics/anthropic-sdk-go/v2`
  v2.15.0 with `WithoutEnvironmentDefaults`, `WithAPIKey`, `WithMaxRetries(0)`, a connect-timeout client
  with keep-alives off and no redirects. Bodies are raw JSON sent through the SDK's `Client.Post`, so the
  transport and headers are the SDK's and each case sends exactly the shape under test.
- **Key:** a house key the operator created for this run, read from a scratchpad file, deleted after the run;
  the operator revokes it. Not in this file, a log or a commit.
- **Models:** `claude-sonnet-5-5` (every case), `claude-opus-5-5` (headers, loop, foreign history).
- **Shape used by the loop cases:** a 3,024-token system prompt and two tools (cache breakpoints on both),
  `thinking: {type: adaptive, display: summarized|omitted}`, `output_config.effort: high`,
  `max_tokens: 16000`; a three-step arithmetic task through a `calc` tool; a per-round hint
  `<current_time>…</current_time> <budget>…</budget>` as a text block after the tool results, as Aura sends it.

## Results

| Case | Sonnet 5.5 | Opus 5.5 |
|---|---|---|
| One call through the SDK | 200. Sent `authorization: Bearer`, `anthropic-version: 2026-10-21`, no `x-api-key` | same |
| `temperature: 1.0` | **400** `temperature: Extra inputs are not permitted` | — |
| `tool_choice: none`, tools present | 200 | — |
| No tools, no `tool_choice` | 200 | — |
| `tool_choice: none`, no tools | 200 (accepted) | — |
| Loop, hint kept in history, blocks replayed verbatim (`drop_block`) | 4 rounds, all 200, `input_transformations: []` every round; round 2 returned `thinking, text, tool_use` (interleaved) | 4 rounds, all 200, `[]`; thinking on every round |
| Same with `display: omitted` | 4 rounds, all 200, `[]`; the replayed block is `thinking: ""` + signature | — |
| Branch: hint rotated (only the newest kept), binding `error` | **400** `Invalid signature in thinking block. The block is bound to a different conversation … Content that preceded this block when it was created is missing` | **400**, same |
| Branch: hint rotated, binding `drop_block` | 200, `thinking_dropped@messages.3.content.0` | 200, `thinking_dropped` ×3 |
| Branch: hint rotated, GA (no beta header, no `block_binding`) | 200, no report; input 49 tokens higher than `drop_block`, i.e. the block was left in place | 200; input 137 higher |
| Branch: reload shape (earlier turns without thinking and without old hints) | 200 | 200 |
| Branch: last assistant turn rebuilt from its calls (thinking removed) | 200 | 200 |
| Foreign ids sanitized (`functions_calc_0`), no thinking | 200 | 200 |
| Foreign ids raw (`functions.calc:0`) | **400** `tool_use.id: String should match pattern '^[a-zA-Z0-9_-]+$'` | **400**, same |
| PDF (base64 document block) inside `tool_result` | 200; the answer named the product in the PDF | — |
| Turn 2 after the loop, reload shape vs verbatim shape (tail breakpoint only) | read 3,024 / write 406 vs read 3,611 / write 118 | read 3,024 / write 509 vs read 3,688 / write 221 |

### Cache across turns, steady state (Sonnet 5.5)

Every earlier turn sent in the reload shape, with four breakpoints: tools, system, the last block of the
loaded history, and the last stable block of the current turn (before the hint).

| Request | read | write |
|---|---|---|
| turn 1 round 1 | 3,024 | 149 |
| turn 1 rounds 2-4 | 3,273 → 3,461 | 0-188 |
| turn 2 round 1 | 3,173 (system + tools + turn 1's question) | 392 (turn 1's loop, reloaded) |
| turn 2 rounds 2-3 | 3,565 → 3,664 | 99-135 |
| turn 3 round 1 | 3,565 (everything up to turn 2's question) | 220 (turn 2's loop, reloaded) |

So without persisting anything, a new turn re-writes only the previous turn's tool loop, in its reload
shape, and reads everything before it from cache.

### Mid-stream errors

Read from the SDK source at v2.15.0 (`packages/ssestream/ssestream.go:99-104,247-250`, confirmed in the
plan's code review): an `event: error` inside a 200 stream surfaces as `*anthropic.Error` whose
`StatusCode` is the stream's 200 and whose `Type()` carries the error type. Not provoked live.

## Verdicts for the plan

1. **Thinking is bound to its prefix and enforced** on both models when the binding beta is sent: a rotated
   hint is a 400 under `error`, a reported drop under `drop_block`. On GA it is silently allowed today.
   Keeping the hint in the run's history removes the mismatch (zero transformations over four rounds,
   both display modes). Plan decision 1 holds.
2. **The reload shape is accepted**: earlier turns without thinking or hints, and a last turn rebuilt from
   its calls. A resumed pause and a new user turn are therefore safe without persistence (decision 3).
3. **Persistence is not needed for caching** if the client adds a breakpoint at the end of the loaded
   history: the steady state re-writes only the previous turn's loop. This becomes plan decision 11 and a
   Task 4/5 change (`Request.LoadedHistoryMessages`).
4. **Sampling cannot be sent** under API version 2026-10-21 (400). **Tool ids must be sanitized**
   (400 on `.` and `:`). **PDF inside `tool_result` works.** `tool_choice: none` without tools is accepted;
   the plan still sends it only with tools.
5. **`display: omitted` round-trips** with `thinking: ""` and its signature.

## What this does not show

- Haiku 5.5 and Fable 5.2 were not probed. Every case is one sample; no rate limit, 429 or 529 was seen.
- Thinking was light (16-186 tokens a round): the arithmetic task rarely needs more. Long thinking, long
  tool loops (over ten rounds, where the 20-block cache lookback matters), images and compaction mid-run
  were not exercised.
- Streaming was not used (`Client.Post` returns the whole message); stream event shapes rest on the SDK
  source and the spec's 2026-11-27 measurement.
- The Console balance was not read before and after; the run's cost is estimated at about $0.20 from the
  logged usage. The operator confirms the charge landed on the Console credit.
- That GA allows a mismatched block today is one observation, not a contract.
