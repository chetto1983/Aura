# OpenRouter: prefer a provider fast enough, keep the cheap one when it is (2026-11-27)

## Why

On the lab VM a turn from Telegram spent 45 and 50 s per model call, and cockpit turns 6 to 11 s, with
`thinkingmachines/inkling-2-mini`. The per-call log added today (`docs/superpowers/verification/2026-11-27-llm-call-log.md`)
names the cause: OpenRouter's default routing weights providers by price, and the cheapest of the six
endpoints, Darkbloom, read a 94k-token prompt in 43 s at 1.8 tok/s. The same call through `:nitro` went to
PrimeIntellect's priority tier: 4.5 s, at about 5.7 times the cost of an uncached call.

## Goal

Aura asks OpenRouter to deprioritize a provider that is currently slow, so a call reaches a fast provider
when the cheap one is slow, and stays on the cheap one when it is fast.

## Non-goals

- The context window and the compaction trigger: the operator's choice in Settings, unchanged.
- Always routing to the fastest provider (`provider.sort`, `:nitro`): rejected, it pays the higher price on
  every call.
- Routes other than OpenRouter.

## Approaches

| | What it sends | Trade-off |
|---|---|---|
| a. Always fastest | `provider.sort: "throughput"` | 4.5 s instead of 45 s on the Telegram thread, about 6x the cost of every uncached call, always |
| **b. Cheap unless slow (picked)** | `provider.preferred_min_throughput` / `provider.preferred_max_latency` | pays more only while the cheap provider is slow; relies on OpenRouter's own 5-minute statistics, which may not see Darkbloom as slow |
| c. Fixed provider | `provider.only` / `order` | ties Aura to one vendor's availability; `order` turns sticky routing off |

The operator picked **b**. If the VM measurement shows it does not move Darkbloom, that is recorded and a is
the next conversation, not a silent switch.

## Design

- OpenRouter documents, in its provider-routing page, `preferred_min_throughput` (tokens/s) and
  `preferred_max_latency` (seconds), each a number (the p50) or an object of percentile cut-offs over a
  rolling 5-minute window. Endpoints that miss them are moved to the end of the list, never excluded; a
  request is never refused because of them.
- Two operator settings, editable in the cockpit on the model-routing page next to the model, applied
  without a restart like the other runtime settings:
  - `AURA_OPENROUTER_PREFERRED_MIN_THROUGHPUT` (tokens/s, sent as the p50 number);
  - `AURA_OPENROUTER_PREFERRED_MAX_LATENCY_SEC` (seconds, sent as the p50 number).
  0 or empty sends nothing, which is today's behaviour.
- They ride inside the `provider` object the OpenRouter route already sends (`data_collection: deny`), and
  only on that route.
- The defaults are set from a VM measurement, not guessed: the same Telegram turn with the threshold on,
  read through the per-call log (`provider`, `first_token_ms`, `duration_ms`) and OpenRouter's Logs page.
  If no threshold moves Darkbloom, the defaults stay 0 and the result is recorded.

## Left to the builder

- Where the two values live in `llm.Config` and the settings catalog, following the existing runtime
  settings (`AURA_LLM_MODEL`, the services cap).
- The cockpit field labels and help text, in Italian and English, following the routing page.
- Whether sticky routing still holds with the thresholds set: the docs do not say; the VM run records it.

## Done when

- A unit test shows the request carries both fields inside `provider` when set and neither when 0, and
  `data_collection: deny` in both cases.
- The cockpit saves and reloads both values.
- A dated file in `docs/superpowers/verification/` records the VM turn with and without the threshold, and
  what it does not prove.
