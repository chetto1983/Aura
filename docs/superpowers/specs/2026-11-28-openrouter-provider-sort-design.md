# OpenRouter: sort the providers, and let the fallback finish

## Why

On the lab VM's Telegram thread a turn waits 45-120 s
(`docs/superpowers/verification/2026-11-28-openrouter-provider-speed.md`). Two causes were measured:

1. OpenRouter's default routing favours the cheapest of the six endpoints of
   `thinkingmachines/inkling-2-mini`, Darkbloom. It reads a 95k-token prompt in 23-46 s.
2. On two of three first attempts, Darkbloom answered `504` after 78 s. OpenRouter then fell back to
   PrimeIntellect, but Aura's 60 s stream watchdog had already closed the stream, because no byte
   arrives during that wait. Aura retried, and the sticky session sent the retry back to Darkbloom.

The two speed thresholds of `2026-11-27-openrouter-provider-speed-design.md` moved nothing on that
thread. The operator decided on 2026-11-28 to replace them with a sort, and to give OpenRouter's
fallback time to answer. Excluding Darkbloom by name was rejected, because the models and their
providers change.

## Goals

- One cockpit choice, "OpenRouter: sort providers by", with the values OpenRouter documents for
  `provider.sort`: `price`, `throughput`, `latency`. Empty means OpenRouter's default routing, and
  nothing is sent. It is hot: a save reaches the next call.
- On the OpenRouter route, Aura's stream idle window and whole-call timeout are long enough for one
  fallback to answer: 90 s and 180 s. The other routes keep 60 s and 120 s.
- The two threshold settings are removed, code, catalog, cockpit and tests alike.

## Non-goals

- No provider allow-list or deny-list, and no `provider.order`.
- No change to the `:nitro` suffix. It stays free text in the model picker.
- No change to sticky routing or to `session_id`.

## Decisions

- **The sort values are OpenRouter's** (provider-selection guide, read 2026-11-28). Two things matter:
  - with `sort` set, default load balancing is bypassed and providers are tried in that order;
  - the body field `sort: "throughput"` is not `:nitro`, because `:nitro` also admits priority-tier
    endpoints. The cockpit help says so.
- **The default stays empty** unless the VM measurement shows that a value takes the thread off a slow
  first provider. The measurement runs on the same Telegram thread and the same session.
- **The OpenRouter timeouts are route defaults, not a new setting.**
  - `AURA_LLM_STREAM_IDLE_TIMEOUT_SEC` and `AURA_LLM_TOTAL_TIMEOUT_SEC`, set in the env or the file,
    still win on every route.
  - Unset, they resolve by the route at use time, so a hot Cloud→Local switch takes the local values.
  - The 90/180 figures come from the measured fallback: `504` at 77.9 s, the fallback's first token
    5.1 s later, 88.7 s in all. They leave about 90 s of streaming after a late first token.
- **The watchdog comment in `internal/llm/config.go` is corrected.** OpenRouter sent no keep-alive
  while it waited on Darkbloom.

## What is left to the builder

- Where the route-resolved timeouts live, as long as every reader uses the same resolution: the
  openai_compat, chatgpt and anthropic clients, the agent call, finalize, the runner and the reasoning
  caps.
- How the cockpit renders a closed set of values, reusing `components/ui/select.tsx`.

## What it costs if wrong

- A slow provider that never answers now holds a turn for up to 90 s instead of 60 s on OpenRouter.
- `throughput` or `latency` routing can cost several times the cheapest provider: `:nitro` cost 5.7
  times Darkbloom per uncached call on 2026-11-27.
