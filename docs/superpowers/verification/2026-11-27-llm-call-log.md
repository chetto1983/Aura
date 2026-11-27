# The model call's log line, measured on the lab VM (2026-11-27)

`db0f0fa11` and `cce3a5e56` made `agent llm call end` carry the model, the call's duration, the time to its
first text, reasoning or tool call, its prompt, cached and completion tokens, the response id and the
upstream provider. They followed a turn from Telegram that spent about 40 s on every call after a tool,
which the old line (finish reason and tool count) could not split.

## Tree and stack

| | |
|---|---|
| VM | the lab VM at `cce3a5e56`, updated by its updater |
| Model | `thinkingmachines/inkling-2-mini` through OpenRouter, the VM's configuration |
| Turn | one cockpit turn in a fresh conversation, driven by Claude in the operator's session: "Cerca nella tua memoria cosa sai di ArcadeDB e riassumilo in una riga." |
| Cross-check | OpenRouter's Logs page, opened read-only in the operator's browser session |

## What the VM logged

| Call | duration_ms | first_token_ms | prompt | cached | completion | provider |
|---|---|---|---|---|---|---|
| 1 | 6 888 | 6 051 | 20 275 | 0 | 50 | Darkbloom |
| 2 | 11 029 | 9 671 | 28 213 | 0 | 60 | Darkbloom |
| 3 | 7 216 | 4 333 | 29 759 | 18 432 | 116 | Darkbloom |

- OpenRouter names the upstream on the stream (`provider`), although its API reference shows it only in a
  sample response, not in the chunk schema.
- The logged `response_id` is the generation id. OpenRouter's Logs page, opened with
  `?transaction=<id>`, showed the first call as Inkling 2 Mini on Darkbloom, 20 275 prompt and 50 completion
  tokens, provider latency 5.8 s, total 6.7 s, routing 6 ms. Its `Session` was the conversation id, so
  Aura's `session_id` reaches OpenRouter.
- On every call most of the time passed before the first token: 88%, 88% and 60%.

## The Telegram thread, and one other provider

The same request, "Cerca nella tua memoria cosa sai di ArcadeDB e riassumilo in una riga.", sent from the
operator's Telegram Web to the VM's bot, whose single conversation holds about 92k tokens:

| Route | Calls | Prompt | Cached | duration_ms | first_token_ms | Provider (OpenRouter's page) | Cost per call |
|---|---|---|---|---|---|---|---|
| default | 2 | 92 498, 94 044 | 0, 0 | 45 153, 50 177 | 43 607, 44 301 | Darkbloom: latency 43.2 s, 1.8 tok/s | $0.00245 |
| `:nitro` | 1 | 94 093 | 0 | 4 505 | 4 197 | PrimeIntellect, Priority tier: latency 3.5 s, 26.9 tok/s | $0.014 |

- OpenRouter serves `thinkingmachines/inkling-2-mini` from six endpoints; Darkbloom is the cheapest
  ($0.02625/M prompt against $0.0375-0.15/M, public endpoints list), and default routing weights providers
  by price.
- `:nitro` was set for the run from the cockpit (Settings, model routing, free text in the model picker),
  and `AURA_LLM_MODEL` was set back to `thinkingmachines/inkling-2-mini` afterwards (checked in
  `aura.settings`, 16:03:08 UTC).
- On this thread the default route spent 96 s on a two-call turn; the slowness of the Telegram turns is the
  serving provider reading a 92k-token prompt with nothing cached.

## What this does not establish

- **The `:nitro` route at length**: one call, without a tool, and its cache was not exercised (0 cached).
  Its cost is about 5.7 times Darkbloom's per uncached call.
- **Why a call misses the cache.** Call 2 went to the same provider as call 1 with the same prefix and read
  nothing from cache; call 3 read 18 432. Whether Darkbloom writes its cache asynchronously, or something
  else applies, is not measured.
- **The Telegram thread.** Its 40 s calls with 2 048 tokens cached (from `aura.conversation_turns`) predate
  this line; which provider served them is not known.
- **Routes other than OpenRouter.** The provider field and the response id were seen only there.
