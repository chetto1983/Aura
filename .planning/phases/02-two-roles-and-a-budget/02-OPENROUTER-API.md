# OpenRouter API — the surface Phase 2 depends on

**Source:** `https://openrouter.ai/openapi.json`, retrieved 2026-09-08 (1,933,855 bytes, 81 paths).
Field names and types below are transcribed from that document, not from memory. Behaviours in
the *Measured* section were observed live against the project's own OpenRouter account on the
same day.

**Do not write a line against this API from recall.** If a field is not on this page, re-fetch
`openapi.json` and add it here with the date, rather than guessing a name that reads plausibly.

## Credentials — three kinds, not interchangeable

| Kind | Calls | Does NOT call |
|---|---|---|
| Inference key | `/chat/completions`, `/key` (its own record) | `/keys`, `/analytics/*`, `/workspaces` |
| Management key | `/keys`, `/keys/{hash}`, `/analytics/*`, `/workspaces`, `/credits` | **completion endpoints — documented, and it is the point** |
| Provisioning key | legacy name; `GET /key` reports `is_provisioning_key` | — |

A leaked management key can mint and revoke keys but cannot spend credit. A leaked per-identity
inference key can spend only up to its own cap. That split is why the design puts the cap on the
key rather than in Aura.

Base URL `https://openrouter.ai/api/v1`. Auth is `Authorization: Bearer <key>` on every call.

## POST /api/v1/keys — mint one identity's key

Management key required. Returns **201**.

Request body (`*` = required):

```
  * name: string                    — name for the new key; surfaces as `api_key_id` in analytics
    limit: number|null              — spending cap in USD; null = uncapped
    limit_reset: enum               — "daily" | "weekly" | "monthly" | null (null = never resets)
    include_byok_in_limit: boolean  — count BYOK spend against the cap
    expires_at: string|null         — ISO 8601 UTC
    workspace_id: string            — workspace to create the key in
    creator_user_id: string|null
    external: object                — partner-defined identity for attribution
  *   external.user: string         — the end-user identifier; THIS is where an Aura identity id belongs
      external.api_key: string      — partner-supplied key, min 32 chars, sufficient entropy
```

Response `201`:

```
  * key: string                     — the actual key, SHOWN ONCE AND NEVER AGAIN
  * data.hash: string               — the stable id for GET/PATCH/DELETE; store this
  * data.label: string              — masked form, e.g. "sk-or-v1-caa...61c"; safe to display
  * data.name, data.disabled, data.created_at, data.updated_at, data.expires_at
  * data.limit, data.limit_remaining, data.limit_reset
  * data.usage, data.usage_daily, data.usage_weekly, data.usage_monthly
  * data.byok_usage, data.byok_usage_daily, data.byok_usage_weekly, data.byok_usage_monthly
  * data.external_user: string|null — echoes external.user
  * data.include_byok_in_limit, data.creator_user_id, data.workspace_id
```

The `key` field appears in this response only. Losing it means deleting the key and minting
another — there is no endpoint that returns it again.

## GET /api/v1/keys — the whole roster in one call

Management key. Query params: `include_disabled`, `offset`, `workspace_id`. Returns
`data: array` of the same record shape as above, minus `key`. One call yields every identity's
cap, remaining and spend — this is the admin roster, and it needs no table of ours.

## GET /api/v1/keys/{hash} — one key

Management key. Same record. Returns **404** once the key is deleted, which is how a
deprovisioning saga verifies its own teardown instead of assuming it.

## PATCH /api/v1/keys/{hash} — the admin's controls

Management key. Every field optional; send only what changes.

```
    limit: number|null              — the top-up / the cut
    limit_reset: enum               — "daily" | "weekly" | "monthly" | null
    disabled: boolean               — suspend WITHOUT deleting; the key and its history survive
    name: string
    include_byok_in_limit: boolean
```

`disabled` is the reversible half of removal. `DELETE` is the irreversible half.

## DELETE /api/v1/keys/{hash}

Management key. Returns `{ "deleted": boolean }`.

## GET /api/v1/key — what a key says about itself

Called **with the key being asked about**, not with a management key. Adds three fields the
management-side record does not carry: `is_free_tier`, `is_management_key`,
`is_provisioning_key`. Also carries `limit`, `limit_remaining`, `usage`, `usage_daily|weekly|monthly`.

`data.rate_limit` is present but its own `note` field says it is deprecated and safe to ignore.

`internal/llm/spend.go` already calls this endpoint and already parses `usage`, `usage_daily`,
`usage_weekly`, `usage_monthly`. It ignores `limit` and `limit_remaining`, which are exactly the
two fields this phase needs.

## GET /api/v1/credits — the account pool

```
  * data.total_credits: number      — purchased, lifetime
  * data.total_usage: number        — consumed, lifetime
```

Remaining is the difference. Every per-key cap allocates from this one pool, and OpenRouter does
not stop the sum of the caps from exceeding it. Over-allocation is the operator's problem to see,
so the admin surface must show it.

## POST /api/v1/analytics/query — per-identity everything

Management key. `metrics` is the only required field.

```
  * metrics: array<string>          — 38 available
    dimensions: array<string>       — up to 2, from the 16 below
    granularity: string             — minute | hour | day | week | month
    time_range: { start, end }      — ISO 8601 UTC, seconds REQUIRED (minute precision is rejected)
    filters: array<{field, operator, value, include_unset}>
    order_by: { field, direction }  — direction: asc | desc
    limit, group_limit: integer
    classifier_dimensions, classifier_filters — custom tag grouping; needs an active classifier
```

Response: `data.data` (the rows), `data.metadata.{query_time_ms,row_count,truncated}`,
`data.warnings`, `data.cachedAt`.

**Dimensions (16):** `model`, `variant`, `api_key_id`, `provider`, `origin`, `country`,
`data_region`, `skin`, `finish_reason`, `workspace`, `app`, `user`, `external_user`,
`context_length_bucket`, `generation_id`, `session_id`.

**Metrics (38):** `request_count`, `total_usage`, `tokens_total`, `tokens_prompt`,
`tokens_completion`, `reasoning_tokens`, `cached_tokens`, `possible_cached_tokens`,
`avg_latency`, `p50_latency`, `p90_latency`, `p99_latency`, `cache_hit_rate`,
`possible_cache_hit_rate`, `cache_capture_rate`, `blended_cost_per_million_tokens`,
`avg_throughput`, `p50_throughput`, `p90_throughput`, `p99_throughput`,
`guardrail_invoked_count`, `guardrail_invoked_rate`, `response_cached_count`,
`response_cached_rate`, `byok_usage`, `credits_usage`, `openrouter_usage`, `byok_fees`,
`byok_request_count`, `usage_upstream`, `usage_cache`, `usage_data`, `usage_web`,
`usage_upstream_web`, `usage_file`, `usage_upstream_file`, `usage_web_fetch`,
`usage_upstream_web_fetch`.

**Operators (8):** `eq`, `neq`, `in`, `not_in`, `gt`, `gte`, `lt`, `lte`.

`api_key_id` comes back as the key's **name**, not its hash — so naming the key after the
identity makes the analytics read as identity rows without a join.

`GET /api/v1/activity` also exists but is coarser: it groups by date/model/endpoint only,
`group_by` accepts nothing but `"workspace"`, and it covers the last 30 **completed** UTC days,
so today's spend is not in it.

## Usage accounting on a completion

`usage` is returned on **every** `/chat/completions` response, streaming included — in the last
SSE message for a stream, in the body otherwise. The documentation states that
`usage: {include: true}` and `stream_options: {include_usage: true}` are **deprecated and have no
effect**; full usage is always present.

```
  cost: number                      — total charged to the account for this call
  cost_details.upstream_inference_cost, .upstream_inference_prompt_cost,
              .upstream_inference_completions_cost
  prompt_tokens, completion_tokens, total_tokens
  prompt_tokens_details.cached_tokens, .cache_write_tokens, .audio_tokens, .video_tokens
  is_byok: boolean
```

`internal/llm/openai_compat/response.go:112` already reads `cost` opportunistically out of the
usage extra fields, so Aura receives the exact per-call cost today and no request change is
needed to start billing against it.

`GET /api/v1/generation?id=<gen id>` returns the authoritative per-generation record —
`usage`, `native_tokens_prompt`, `native_tokens_completion`, `native_tokens_cached`, `latency`,
`generation_time`, `external_user`, `is_byok`, `finish_reason`.

## Measured behaviour — 2026-09-08, live

| Observation | Value |
|---|---|
| Key created at `limit: 1.0` | serves inference on the **first** call, no warm-up |
| Key at `limit: 0` | inference refused **HTTP 403** `Key limit exceeded (monthly limit)` — **not** 402 |
| `PATCH limit` downward | denies within **5s** |
| `PATCH limit` upward on an exhausted key | **~25s** before inference recovers (403 at t+18s, 200 at t+25s) |
| `DELETE` | inference returns 401 within **5s**; `GET /keys/{hash}` returns 404 |
| `GET /key` usage counter | lags a spend by **30–40s** (read `usage: 0` at t+30s, correct at t+40s) |
| In-band `cost` vs provider total | 4 calls × `0.000004158` = `0.000016632` reported — **exact** |
| Account pool at the time | `total_credits: 90`, `total_usage: 74.078437781` |

Two consequences the code must carry rather than rediscover: a top-up is **not** immediate, so
the admin UI must say so; and `GET /key` is reconciliation, never a live balance, so the balance
Aura shows must come from its own in-band ledger.

## Not measured — do not assume

- Whether `limit_reset` actually rolls over at a period boundary. Never observed.
- Whether `/activity`'s `api_key_hash` parameter filters or is silently ignored. It returned an
  empty set for a key created the same day, which the endpoint's "last 30 completed UTC days"
  rule explains equally well.
- Whether `external.api_key` (partner-supplied key material) behaves differently from a minted
  key with respect to caps.
- Deleted keys keep their consumption in the analytics. Nothing observed here removes it, so an
  identity deletion is complete on our planes and incomplete on the provider's.

## Adjacent surface, deliberately not used in Phase 2

`GET|POST /workspaces`, `GET|PUT|DELETE /workspaces/{id}/budgets/{interval}` — a second ceiling
above the per-key one, `limit_usd` per `daily|weekly|monthly|lifetime`, and the documentation
states limits must strictly decrease as the interval narrows. `GET|POST /guardrails` with
`/guardrails/{id}/assignments/keys` — policy guardrails assignable per key. Both are recorded
here so the next phase that wants them starts from the surface rather than from a search.
