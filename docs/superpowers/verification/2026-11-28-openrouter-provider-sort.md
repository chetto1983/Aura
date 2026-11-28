# OpenRouter's provider sort on the lab VM (2026-11-28)

`c656f2590`, `15bde6498` and `33e905569` replaced the two speed thresholds with one hot setting,
`AURA_OPENROUTER_PROVIDER_SORT`, which sends OpenRouter's `provider.sort`. They also gave the OpenRouter
route a 90 s stream window and a 180 s call bound when neither is configured. This run measured the
sort on the lab VM's Telegram thread, the thread of `2026-11-28-openrouter-provider-speed.md`.

## Tree and stack

| | |
|---|---|
| VM | the lab VM at `031fdc60c`, updated by its updater |
| Model | `tencent/hy4-flash` through OpenRouter. The operator had switched the model at 10:50 UTC, after the thresholds run; it was not changed for this run |
| Endpoints | nine, from OpenRouter's public endpoints list: Tencent ($0.035/M prompt), Fireworks, DeepInfra twice, Darkbloom ($0.02625/M, the cheapest), PrimeIntellect three times, Io Net |
| Turn | the operator's Telegram Web to the VM's bot, same conversation: "Cerca nella tua memoria cosa sai di ArcadeDB e riassumilo in una riga." |
| Settings | the select on the cockpit's model-routing page, checked in `aura.settings` after each save |
| Cross-check | OpenRouter's Logs page in the operator's browser session, read-only |

## What the VM logged

| Turn (UTC) | Sort | Calls: provider, duration_ms, first_token_ms, prompt, cached | Turn |
|---|---|---|---|
| 12:37:58 | none | Tencent 3 197 / 2 494 / 17 415 / 0; Tencent 4 113 / 2 942 / 24 759 / 0; Tencent 4 153 / 3 218 / 27 521 / 16 384 | 16 s |
| 12:39:20 | throughput | Tencent 6 945 / 2 956 / 27 566 / 0; Tencent 3 908 / 3 295 / 28 832 / 16 384 | 12 s |
| 12:39:58 | latency | Tencent 3 894 / 2 655 / 28 919 / 16 384 | 5 s |
| 12:40:34 | price | Tencent 5 537 / 3 450 / 21 794 / 0 | 6 s |

There was no `agent llm call error` and no stream idle timeout.

## What OpenRouter's Logs page showed

The `price` generation (`gen-1795869635-…`) shows the following:
- provider responses read "Routing 10ms, Tencent 200, 3.1s", with no fallbacks;
- 54.7 tok/s, $0.000794;
- the same OpenRouter session as the morning's turns.

Darkbloom, the cheapest endpoint, was not tried.

## The settings afterwards

The sort row was reset from the cockpit (`Ripristina`), so `aura.settings` holds no
`AURA_OPENROUTER_PROVIDER_SORT`, as before the run. The shipped default stays empty.

## What this does not establish

- **That `sort` changes the provider.** Every value, `price` included, went to Tencent on a thread whose
  OpenRouter session had been routed to Tencent since the model switch. Three explanations are open:
  - OpenRouter's guide says a session id makes routing sticky and that only `provider.order` overrides
    it, so stickiness may outrank `sort`;
  - `data_collection: deny` may exclude Darkbloom for this model;
  - the live request may not carry the field: `TestOpenRouterProviderCarriesSort` pins the body, but the
    Logs page does not show the request's provider object.

  A fresh conversation was not tried.
- **The 90 s and 180 s route bounds.** They were not in force on the VM. `compose.yaml` forwarded
  `AURA_LLM_TOTAL_TIMEOUT_SEC:-120` and `AURA_LLM_STREAM_IDLE_TIMEOUT_SEC:-60`, so both timeouts were
  explicitly set inside the container and the route defaults never applied. The final review found it
  and the compose lines were left empty in the next commit. No call stalled in this run either way. The
  bounds are pinned by `TestRouteTimeoutsFollowTheRoute` and
  `TestPrimaryLLMRouteReloaderSwitchesRouteTimeouts`.
- **The morning's slowness on this model.** The slow provider and the 95k-token prompt belonged to
  `inkling-2-mini`. On `tencent/hy4-flash` the thread's prompt was 17-29k tokens and no call took more
  than 7 s.
