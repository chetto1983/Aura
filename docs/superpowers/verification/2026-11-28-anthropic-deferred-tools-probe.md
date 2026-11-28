# Claude deferred tools: wire probe, 2026-11-28

Probe: `spikes/anthropic-provider/probe-defer/` (f7b99368e, dc2111e30). Model `claude-sonnet-5-5`, Messages API
with the binding beta header and adaptive thinking, operator's Console key (read from a scratch file deleted after
the run, never logged). Three runs, about $0.06 billed in total. Logs: the SDD workspace
`probe-defer-run.log`, `probe-defer-run2.log`.

## Why

On the lab VM (route anthropic, Opus 5.5, conversation 01a2223e), every request right after a `tool_search` read
0 from the cache, rewrote ~40k tokens and dropped one signed thinking block (about $0.21 instead of $0.05).
`tool_search` grows the `tools` array, and tools head the cache prefix.

## Measured

| Case | Result |
|---|---|
| `cache_control` on a deferred tool | 400 "cannot have both defer_loading=true and cache_control set" |
| `tool_reference` to a tool not in `tools` | 400 "Tool reference 'no_such_tool' not found in available tools" |
| every tool deferred | 400 "At least one tool must have defer_loading=false" |
| `tool_result` with a text block and a `tool_reference` (`-result mixed`, and `spec` = Aura's schema text + reference) | **400** "Tool definitions/code execution functions cannot be mixed with other content" |
| `tool_result` with `tool_reference` blocks only (`-result refs`, effort max) | round 1 (thinking + `tool_search`): read 0, write 2774. Round 2: **read 2774**, write 323, dropped 0, called `stock_quote` with all three non-guessable arguments right. Round 3: read 3097, write 147, dropped 0. **`tools` digest identical on every round.** |
| second turn, history replayed verbatim | read 3244, write 65, called the referenced tool again without a new search |
| second turn, history with thinking stripped (Aura's reload shape) | read 2774, write 515, called the referenced tool again without a new search |
| baseline, the array grows (earlier run, no thinking in round 1) | round 2 read 0, write 3179 |
| unreferenced deferred tool | the model searched for it instead of calling it |

## What it settles

- The fix works on the wire. Send every tool from round 1 with `defer_loading: true` on the deferred ones, and
  answer `tool_search` with **only** `tool_reference` blocks. The cache prefix holds, the signed thinking is not
  dropped, the schema reaches the model, and the reference survives into later turns.
- On this route the `tool_search` result must carry no text next to the references. Aura's spec text has to go,
  or move to a separate block outside the `tool_result`.

## What it does not prove

- Opus 5.5, which is the VM's route, was not run. Long conversations, history trimming, and unmounting a tool that
  a past turn referenced were not run either.
- The baseline run with thinking (effort max) hit a transport timeout on round 2. The thinking drop on a growing
  array rests on the VM log, not on this probe.
- "Reload shape" read only tools + system (2774). With the thinking stripped, the replayed history differs from the
  cached one from the first assistant message onward. This matches the production cross-turn cache miss under
  investigation on 2026-11-28. It is a lead, not a measured cause.
