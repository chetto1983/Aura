# OpenRouter's speed thresholds on the lab VM (2026-11-28)

`b782503b5`, `60530c1c3` and `0422273e3` made two hot settings send OpenRouter's
`provider.preferred_min_throughput` and `provider.preferred_max_latency`, so that the Telegram thread's
slow, cheap provider (Darkbloom, `2026-11-27-llm-call-log.md`) would be tried after a faster one. This run
measured whether either threshold moves the thread's calls. Neither did, and the run found where most of a
slow turn goes.

## Tree and stack

| | |
|---|---|
| VM | the lab VM at `61e0ef7ad`, updated by its updater |
| Model | `thinkingmachines/inkling-2-mini` through OpenRouter, the VM's configuration, no suffix |
| Turn | the operator's Telegram Web to the VM's bot, the same single conversation (about 95k tokens): "Cerca nella tua memoria cosa sai di ArcadeDB e riassumilo in una riga." |
| Settings | set and reset from the cockpit's model-routing page, checked in `aura.settings` |
| Cross-check | OpenRouter's Logs page in the operator's browser session, read-only |

## What the VM logged

| Turn (UTC) | Threshold | First attempt | Answered call: provider, duration_ms, first_token_ms, prompt, cached |
|---|---|---|---|
| 10:35:08 | none | `stream idle timeout` at 10:36:21 | Darkbloom, 49 175, 46 432, 94 977, 0 |
| 10:38:26 | min throughput 20 tok/s | none | Darkbloom, 44 027, 39 311, 95 047, 0 |
| 10:41:58 | max latency 10 s | `stream idle timeout` at 10:43:11 | Darkbloom, 27 140, 23 548, 95 117, 92 160 |

Each answer was one call with no tool. The turns took 122 s, 45 s and 99 s.

## What OpenRouter's Logs page showed

- The first attempts of turns 1 and 3 are generations routed for 77.9 s and 78.0 s. On the second
  (`gen-1795862521-…`), the provider responses read: Darkbloom `504` after 77.9 s, then PrimeIntellect
  `499` after 5.1 s, 14 tokens generated, 88.7 s in all. OpenRouter fell back after Darkbloom's gateway
  timeout, and PrimeIntellect had started answering when Aura closed the stream. A `499` is the client
  going away.
- Aura's stream watchdog (`internal/llm/openai_compat/stream_idle.go`) resets on any byte, keep-alive
  comments included, and fires after 60 s (`defaultStreamIdleTimeoutSec`). It fired about 60 s into each
  first attempt, so OpenRouter sent no bytes while it waited on Darkbloom.
- The answered calls are Darkbloom generations with a routing time of 74 ms, provider latency 23.1 s and
  2.1 tok/s (turn 3). The three turns carry the same OpenRouter session.
- Darkbloom also served Aura's 128-token side calls in the same minutes, in 0.53-0.61 s at 23.8-28.1 tok/s.

## The settings afterwards

Both rows were reset from the cockpit (`Ripristina`): `aura.settings` holds no
`AURA_OPENROUTER_PREFERRED_*` key, as before the run. The shipped defaults stay 0.

## What this does not establish

- **That the thresholds never work.** Aura sends `session_id`. OpenRouter's provider-selection guide
  (read 2026-11-28) says that with a session id, sticky routing starts on the first successful request when
  cache reads are cheaper, and says only that `provider.order` overrides it. It is silent on `sort` and the
  `preferred_*` thresholds, and its threshold examples always pair them with `sort`. Three turns of one
  sticky session on a warm Darkbloom do not show what the thresholds do on a fresh session or under `sort`.
- **That the request carried them.** The body is pinned by `TestOpenRouterProviderCarriesSpeedPreferences`
  and the hot reload by `TestPrimaryLLMRouteReloaderResolvesOpenRouterSpeedPreferences`. OpenRouter's Logs
  page does not show the request's provider object, so the live body was not seen.
- **Why the thresholds did not move Darkbloom.** OpenRouter's thresholds use a p50 over a 5-minute window
  and the guide does not say whether latency depends on prompt size. Darkbloom's fast small calls could keep
  its p50 under 10 s, but this is not measured.
- **How often Darkbloom answers 504.** Two of three first attempts here, on one thread, in one hour.
- **What happens with a longer idle window.** That PrimeIntellect would have finished is read from its
  start, not measured.
