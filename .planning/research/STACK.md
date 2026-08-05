# Stack Research

**Domain:** Go backend — LLM context-management subsystem (summarization rung, accurate token
accounting, per-category context breakdown) added to an existing 98k-LOC self-hosted agent
platform (v2.1.0 HERMES-CLAUDE_PARITY, phases numbered from 45)
**Researched:** 2026-08-05
**Confidence:** HIGH — every recommendation below is grounded in either (a) the on-disk
hermes-agent reference implementation at `D:/tmp/hermes-agent/agent/context_compressor.py` and
`context_breakdown.py` with file:line citations, (b) the existing Aura codebase with file:line
citations, or (c) OpenRouter's own current documentation fetched directly on 2026-08-05.

## Headline finding

**This milestone needs zero new direct Go dependencies.** Every mechanism hermes built in
~11k LOC of Python to make LLM summarization safe (cooldown, anti-thrash, fallback, offline
testability) already exists in Aura in smaller, Go-idiomatic form, built for a different call
site (the main chat stream) but directly reusable or mirrorable for the summarization rung. The
work here is wiring and a small schema addition, not library selection. Where hermes needed a
library-shaped solution, grep the tree first — three separate near-misses turned up in this
research alone (see "What NOT to Use").

## Recommended Stack

### Core Technologies

No new core technology. The three capabilities land entirely inside the existing stack:

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| `github.com/pkoukk/tiktoken-go` | v0.1.8 (pinned, unchanged) | cl100k_base token estimation | Stays as-is for RELATIVE sizing decisions (how many pairs to drop, how big the summarizer input is) inside `internal/conversations/context.go` and the new breakdown file — same currency as today, do not swap for a second tokenizer |
| `github.com/jackc/pgx/v5` + `github.com/golang-migrate/migrate/v4` + `sqlc` (v1.31.1, CI-pinned in `Makefile:6`, `.github/workflows/ci.yml:566`) | v5.10.0 / v4.19.1 / v1.31.1 (all pinned, unchanged) | Persistence for the one piece of new durable state (per-conversation anti-thrash streak) | Already the whole persistence stack; one migration + one query file, no new tooling |
| `internal/llm.Client` interface (`internal/llm/client.go:89`) | n/a (in-tree) | The wire abstraction the summarization rung calls through | Provider-neutral, already fakeable, already the interface every other out-of-band LLM call (title generation) uses |

### Supporting Libraries (already vendored — reused, not added)

| Library / in-tree type | Where it already lives | New role in this milestone |
|---|---|---|
| `internal/llm.Breaker` | `internal/llm/breaker.go:1-83` | **Cooldown-after-429/5xx** for the summarizer call. It is already a process-lifetime, mutex-protected, injectable-clock (`now func() time.Time`, line 36) consecutive-failure breaker — exactly hermes' `_summary_failure_cooldown_until` mechanism, just already written and already unit-tested (`internal/llm/breaker_test.go:9-34`, clock injected, no `time.Sleep` in tests). Mint a SECOND instance (`llm.NewBreaker(threshold, cooldown)`) dedicated to the summarizer call path — do not share the main-model breaker instance, the two failure populations are unrelated. |
| `*openai_compat.HTTPError` + the `errors.As` classifier idiom | `internal/llm/openai_compat/httperror.go:24-54`, classifier at `internal/agent/llm_agent_stream_retry.go:83-118`, delay-from-Retry-After at `:153-163` | **429/5xx detection** for the summarizer response. `HTTPError{StatusCode, RetryAfterSec}` already carries everything; the existing `retryableStreamOpenError`/`streamOpenRetryDelayFor` pair (`httpErr.StatusCode == 429 \|\| httpErr.StatusCode >= 500`, honor `RetryAfterSec` capped at a ceiling) is the literal logic to mirror for the summarizer, not reinvent. |
| Hand-rolled fake implementing `llm.Client` | `internal/conversations/title_unit_test.go:16-59` (`titleTestClient`) | **Offline testability** for the summarization rung. `conversations.GenerateTitle` (`internal/conversations/title.go:26-77`) is the existing precedent for "make one out-of-band `llm.Client.Stream` call, drain it, shape the result, let the caller's goroutine/timeout own the lifecycle" — the summarization rung should be `Summarize(ctx, client, model, turns) (string, error)` in the same shape, tested with the same scripted-fake-with-`openErr`-field pattern (no HTTP test server, no mocking library). |
| `aura.conversation_turns.input_tokens` + `GetConversationLastInputTokens` (sqlc, already generated) | `internal/db/queries/conversations.sql:17-30`, `internal/db/sqlc/conversations.sql.go:291-311`, surfaced as `Conversation.LastInputTokens` at `internal/conversations/store.go:172` / `store_identity.go:49-53` | **Accurate token accounting.** The real, provider-native `prompt_tokens` for the most recent request-bearing turn is ALREADY captured (`internal/llm/openai_compat/usage.go:11-56`, populated into `AppendTurnParams.InputTokens` at `internal/runner/runner_persist.go:313`) and already queryable. Nothing new to build here except passing it into `ContextConfig` (see Integration Points). |
| `sync.Mutex` + `time.Time` (stdlib) | throughout `internal/llm`, `internal/conversations` | Any additional in-process counters (if the anti-thrash decision ends up in-memory rather than DB — see Stack Patterns) — no concurrency library needed anywhere in this milestone |
| `regexp`, `strings`, `html` (stdlib) | `internal/conversations/context.go:59,461-489` | Deterministic-fallback text shaping (path/error extraction, truncation) mirrors hermes' `_build_static_fallback_summary` (`context_compressor.py:3203-3403`) using the same stdlib primitives already used for `readToolOutputCallIDRe` etc. |

### Development Tools

No new tools. `golangci-lint`, `go vet`, `govulncheck`, `dupl`, `go-mutesting`, `goleak`,
`pgregory.net/rapid` (already direct deps, `go.mod:37,46`) all apply unchanged.

## Installation

```bash
# No `go get` needed — zero new module dependencies.

# The only new artifact is a migration (exact number resolved at landing time —
# NEVER hardcode; see project rule):
ls internal/db/migrations/ | tail -1   # confirm next free slot before creating one
```

## Per-question findings

### Q1 — LLM summarization rung: anti-thrash, cooldown-after-429, deterministic fallback, offline-testable

**No new library.** Concretely:

- **The LLM call itself**: a new function in a NEW file (`context.go` is already 590/600 LOC,
  `store.go` is 574/600 — see file-size constraint below), e.g.
  `internal/conversations/context_summary.go`, shaped exactly like
  `GenerateTitle`/`generateTitle` (`internal/conversations/title.go:26-77`): take an
  `llm.Client`, build an `llm.Request` with a structured system prompt (mirror hermes' template
  sections — Historical Task Snapshot / Goal / Constraints / Completed Actions / Active State /
  Blocked / Key Decisions / Resolved Questions / Relevant Files / Critical Context,
  `context_compressor.py:3669-3718`), drain the stream, return `(string, error)`. The Runner
  (not the package) owns timeout/goroutine wiring, matching the existing convention.
- **Cooldown after 429/5xx**: mint a dedicated `*llm.Breaker` (`internal/llm/breaker.go:41`)
  for the summarizer call path. hermes' `_TIMEOUT_COOLDOWN_LADDER = (60, 300, 900)`
  (`context_compressor.py:1943`) escalating-on-consecutive-failure shape does NOT need a new
  data structure — `Breaker.Failure`/`Allow` already gates on "N consecutive failures → open
  until T"; if an escalating (not fixed) cooldown is wanted, that is three more lines in the
  existing `Breaker.Failure` shape, not a new type. Detect 429/5xx via the SAME `errors.As`
  pattern already proven at `internal/agent/llm_agent_stream_retry.go:83-118` (429 or ≥500 is
  retryable/cooldown-worthy; honor `HTTPError.RetryAfterSec` capped at a ceiling, same as
  `streamOpenRetryDelayFor:153-163`).
- **Anti-thrash (2 consecutive ineffective compressions → back off)**: this is a
  PER-CONVERSATION content property (was the transcript actually shrunk enough?), not a
  provider-health property, so it does not belong on the same in-memory breaker as the cooldown
  — see Stack Patterns below for the persistence recommendation.
- **Deterministic fallback**: a pure Go function over the same `[]Turn` the ladder already
  walks — no library. Mirror `_build_static_fallback_summary`
  (`context_compressor.py:3203-3403`): walk turns, bucket by role, extract path mentions
  (`regexp`), truncate per-turn (a `const` cap, same idiom as `_FALLBACK_TURN_MAX_CHARS`), emit
  the same structured template with a "generated without an LLM call" note. This runs whenever
  the LLM call errors OR the breaker is open — never blocks a turn.
- **Offline testability**: already solved. Follow `titleTestClient`
  (`internal/conversations/title_unit_test.go:16-59`) verbatim — a hand-rolled `llm.Client` fake
  with a queue of scripted `titleTestTurn{chunks []llm.Chunk, openErr error}`. To simulate a 429,
  set `openErr: &openai_compat.HTTPError{StatusCode: 429, RetryAfterSec: 1}`. This is a
  **hard requirement to reuse, not a suggestion**: it is the only pattern in the codebase that
  satisfies "testable offline" for an `llm.Client` caller, and it costs zero new dependencies.

### Q2 — Accurate token accounting vs the current tiktoken-go/cl100k_base estimate

**No new library; the accurate number already exists and already flows through the codebase —
the gap is a missing wire between two already-built pieces.**

OpenRouter confirmed (docs fetched 2026-08-05,
[Usage Accounting cookbook](https://openrouter.ai/docs/cookbook/administration/usage-accounting)):
token counts in the inline `usage` object are "calculated using the model's native tokenizer"
(i.e., DeepSeek's own tokenizer for DeepSeek requests, not a cross-provider normalized count),
`usage: {include: true}` is deprecated — "full usage details are now always included
automatically in every response" — and the object is present in the last SSE chunk for
streaming, matching how `internal/llm/openai_compat/usage.go:5` already documents and parses it
(`usageWire.PromptTokens`/`CompletionTokens`/`PromptTokensDetails.CachedTokens`). This is
strictly better than the tiktoken-go estimate for DeepSeek, which cl100k_base was never trained
on.

That real number is ALREADY persisted every turn
(`AppendTurnParams.InputTokens` ← `u.PromptTokens`, `internal/runner/runner_persist.go:313`,
into `aura.conversation_turns.input_tokens`) and ALREADY has a query pulling the latest one back
out (`GetConversationLastInputTokens`, `internal/db/queries/conversations.sql:17-30`, already
generated into `sqlc.Queries` and already surfaced as `Conversation.LastInputTokens`,
`internal/conversations/store.go:172`). It is used TODAY only for the cockpit's display gauge
(`internal/agui/conversations_api.go` per the grep hits) — never fed into the ladder's own
budget decision. **C-4 in the audit doc names this precisely: "Aura ha il dato e non lo usa per
decidere."**

Fix is wiring, not a library:
1. Add `LastRealPromptTokens int` to `ContextConfig` (`internal/conversations/context.go:80-106`).
2. `runner.contextConfig()` (`internal/runner/runner_context.go:32-42`) populates it from
   `Store.GetConversationLastInputTokens` (already exists) or, cheaper, from the in-memory
   `llm.Usage` of the immediately-preceding turn if the Runner already holds one in this call
   chain — either source is the same number.
3. `applyContextLadder`'s L2 trigger (`internal/conversations/context.go:314-325`) gains a second
   condition: when `LastRealPromptTokens > 0`, prefer it (or `max` of it and the tiktoken
   estimate) over the pure estimate for the "are we near/over the cap" decision — mirroring
   hermes' `should_compress_info` gating on `last_prompt_tokens`
   (`context_compressor.py:2554-2585`) rather than a rough guess.
4. `tiktoken-go`/`countTokens` (`internal/conversations/context.go:558-568`, `tiktoken.go:86-95`)
   STAYS — it is still the only tool for the RELATIVE question `dropOldestPairs` must answer
   ("how many pairs until we're under cap"), since the real count from the last response cannot
   reflect turns added since that response. The existing comment calling it "a fast ~5-10%
   approximation used ONLY for L2/L2.5 budget gating" remains honest; it is simply no longer the
   sole source for the trigger.
5. `ProviderErrorReserveTokens` (`internal/conversations/context.go:98-105`) is unaffected — it
   exists for the local llama.cpp chat path, which is out of scope here (llama.cpp in this
   milestone is embeddings-only; OpenRouter/DeepSeek is the chat provider).

### Q3 — Per-category context breakdown (system / tool manifest / tool results / summary / protected tail)

**No new library.** A pure aggregation function over structures the ladder already has (plus,
once Q1 lands, the new summary-turn marker). Land it in a NEW file,
`internal/conversations/context_breakdown.go` (both `context.go` and `store.go` are at/near the
600-LOC ceiling), exposing something like `func Breakdown(turns []Turn, cfg ContextConfig,
manifest []llm.ToolDef) CategoryBreakdown`, token-counted with the SAME `tiktoken-go` encoder
used everywhere else in the ladder — do not introduce hermes' `chars/4` shortcut
(`context_breakdown.py:31-34`); that heuristic exists in Python only to avoid a real-tokenizer
call in a hot path, and Aura's embedded, in-memory cl100k_base encoder (`tiktoken.go:14-32`,
network-free, cached via `sync.Once`) already pays that cost for free elsewhere in the same
call.

Categories should be named for Aura's actual manifest shape, not transplanted 1:1 from hermes'
eight (`system_prompt`/`tool_definitions`/`rules`/`skills`/`mcp`/`subagent_definitions`/
`memory`/`conversation`, `context_breakdown.py:19-28`) — Aura has no separate "rules" or
"subagent definitions" block today, and its "memory" block is already a distinct, easily
isolated field (`ContextConfig.TransientContext`). A first cut close to what Aura's ladder
actually assembles: `system`, `always_block` (D-07 skill/memory block), `tool_manifest`
(un-deferred + deferred-summary line, once the tool-surface flattening lands), `tool_results`
(role=tool turns), `summary` (once Q1 lands), `remaining_history`. This is a phase-design
decision, flagged here only so the roadmapper does not treat "copy hermes' 8 categories" as a
given.

`internal/prometheus` / `go.opentelemetry.io/otel` (already direct deps, `go.mod:28-36`) can
expose the same numbers as gauges if the phase wants alerting on category drift — optional,
not required; the primary consumer is the cockpit's existing AG-UI/SSE JSON channel, which
already renders the rot-event gauge (`ListContextRotEvents`, `internal/conversations/context.go:184-219`)
the same way.

## Alternatives Considered

| Recommended | Alternative | Why Not |
|-------------|-------------|---------|
| Mint a second `internal/llm.Breaker` for the summarizer cooldown | `github.com/sony/gobreaker` (already an INDIRECT dependency, `go.mod:180` — pulled in by something else, zero direct callers anywhere in the tree) | Promoting an indirect dependency to direct for a feature Aura already hand-rolls (and already unit-tests with an injectable clock) adds an abstraction layer with a different API shape than the one every other LLM-health guard in the codebase already uses. Violates "follow existing patterns." |
| Fixed/escalating cooldown via a `[N]time.Duration` ladder + `time.Time` comparison | `github.com/cenkalti/backoff/{v3,v4,v5}` (all three present as INDIRECT deps, `go.mod:81-83` — pulled in by AWS SDK / redis / Authula, zero direct callers) | hermes' own cooldown ladder is a 3-element const array indexed by `min(count, len-1)` — a general-purpose exponential-backoff-with-jitter library is solving a harder problem than the one that exists. Aura already has a hand-rolled full-jitter helper for the ONE place that genuinely needs it (`internal/documents/retry_backoff.go:11-40`, document pipeline retries) — if jitter is ever wanted here, copy that shape, don't add a library. |
| `errors.As` against `*openai_compat.HTTPError` for 429/5xx detection | `golang.org/x/time/rate` (INDIRECT dep, `go.mod:210`, zero direct callers anywhere in the tree) | `x/time/rate` is a token-bucket *rate limiter* (throttling outgoing requests to a budget) — a different problem from "back off after a failure was observed." Confusing the two would add a dependency that does not even solve Q1's actual need. |
| Hand-rolled fake implementing `llm.Client` (mirrors `titleTestClient`) | `github.com/stretchr/testify/mock` (testify is present but marked `// indirect`, `go.mod:182` — no first-party package imports it directly today) | The codebase's own established idiom for faking a small interface is a hand-written struct with a scripted turn queue (see also `fakeConvStore`, `internal/runner/fakes_test.go:49-264`). Introducing `testify/mock` for this one new file would be the only mock-based test in the tree — inconsistent, and `llm.Client` has exactly one method, so a mocking framework buys nothing. |
| Reuse the already-parsed `usage.prompt_tokens` (native tokenizer, confirmed by OpenRouter docs) | Calling OpenRouter's `/api/v1/generation` endpoint per turn for "more precise" counts | The inline usage object already returns native-tokenizer counts on every response at zero extra cost; `/generation` requires a second round-trip per turn and its only field the inline object lacks (`cost_details.upstream_inference_cost`) is BYOK-only and irrelevant to token accounting. Pure latency cost for no accuracy gain. |
| Keep `tiktoken-go` cl100k_base for relative/internal sizing only | Any "DeepSeek-native tokenizer in Go" replacement | No such library exists in the Go ecosystem today (not found via WebSearch), and it would only serve the internal relative-sizing role that cl100k_base already serves adequately (the code's own comment already frames it honestly as a "~5-10% approximation" — not a claim of exactness). The REAL number now comes from the provider response (Q2), which makes chasing a better estimator moot for the one place it used to matter (the trigger decision). |
| Persist per-conversation anti-thrash streak as 1-2 small columns on `aura.conversations` (mirrors `total_input_tokens` etc., `internal/db/queries/conversations.sql:1-16`) | An in-process `map[conversationID]*state` with TTL cleanup | Aura's ladder reloads turns fresh from Postgres every call — there is no long-lived per-conversation Go object the way hermes has a long-lived Python `ContextCompressor` instance. An in-process map would leak across many conversations, would not survive a restart (unlike hermes, which persists specifically so it does), and duplicates state that already has a natural home next to the other per-conversation aggregate counters. |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| A second HTTP client / SDK for the summarizer call | Project convention is explicitly no-SDK for LLM calls (`internal/llm/openai_compat/client.go:1-6`: "There is deliberately no SDK") — a summarizer-specific client would violate that and duplicate SSE/error handling already solved once | `internal/llm.Client` — the same interface every other caller uses |
| Any mocking/DI framework (`gomock`, `testify/mock`, `mockery`) | Zero precedent in the tree for `llm.Client` faking; the interface is one method | Hand-rolled fake, mirroring `titleTestClient` |
| Promoting `sony/gobreaker`, `cenkalti/backoff/*`, or `golang.org/x/time/rate` from indirect to direct | All three are dependency-graph noise (pulled in by unrelated third-party deps), none has a single first-party caller today, and each solves a problem Aura already hand-rolls in a smaller, already-tested shape | `internal/llm.Breaker` (cooldown), a `const` ladder array (escalation), plain `time.Time` comparison |
| Calling `/api/v1/generation` per turn for token counts | Extra round-trip for data already inline in the streamed response | The already-parsed `usage` object (`openai_compat/usage.go`) |
| A JSON-byte or `chars/4` token estimator for the per-category breakdown | Introduces a second, inconsistent unit of account next to the ladder's existing tiktoken-go counts; hermes only does this to dodge a real-tokenizer call Aura has already made free (embedded vocab, `sync.Once`-cached) | `countTokens`/the cached cl100k_base encoder (`tiktoken.go:69-95`) |
| Adding new code to `internal/conversations/context.go` or `store.go` | Both are at/near the 600-LOC ceiling (590 and 574 lines respectively as of this research) — CLAUDE.md forbids non-test Go files over 600 LOC | New sibling files: `context_summary.go` (LLM call + fallback), `context_breakdown.go` (Q3), `store_context_summary.go` (new Store methods for the anti-thrash columns) |
| Reusing the main-model `*llm.Breaker` instance for the summarizer's cooldown | Conflates two unrelated failure populations (main chat stream health vs. auxiliary summarizer health); a summarizer 429 storm would then wrongly open the breaker guarding the user-facing chat stream | A second, dedicated `*llm.Breaker` instance |

## Stack Patterns by Variant

**If the phase decides the anti-thrash streak must survive a process restart (matches hermes exactly):**
- Add 1-2 columns to `aura.conversations` via a new migration (next free slot per
  `ls internal/db/migrations/ | tail -1` — 0094 as of 2026-08-05, but re-check at landing time
  per project rule, never hardcode) — e.g. `context_summary_ineffective_streak smallint NOT NULL
  DEFAULT 0`, and reuse `updated_at`-style bookkeeping already on that table.
- New query file `internal/db/queries/conversation_context_summary.sql`, generated by the
  CI-pinned `sqlc@v1.31.1`.
- New Store methods in `internal/conversations/store_context_summary.go` (not `store.go`).

**If the phase decides restart-durability is not worth the migration for a v1 (simpler, still viable):**
- Hold the streak in-memory only, scoped to the Runner's per-conversation session state (if one
  already exists per active chat) rather than a global map — accept that a process restart resets
  the anti-thrash guard to "not tripped," which is a SAFE default (worst case: one wasted
  compression attempt, never a stuck block).
- Either way, the cooldown-after-429 (provider-health, not conversation-content) stays a single
  process-lifetime `*llm.Breaker`, independent of this choice.

**Previous-summary retrieval (for iterative "update the existing summary" compaction, hermes'
second-most-important design piece after the fallback):**
- Because Aura persists every turn (unlike hermes' ephemeral in-memory `_previous_summary`),
  the previous summary automatically survives restarts for free — it is just a row in
  `conversation_turns`. Locate it the same way the ladder already locates the always-block: a
  reserved sentinel in an otherwise-unused field (mirror `alwaysBlockMarker`/`isAlwaysBlock`,
  `context.go:53-57,375-381`), e.g. tag the persisted summary turn's `ToolCallID` with a
  `summaryTurnMarker` constant so a future ladder pass can find "the newest summary turn" without
  string-sniffing content. This is architecturally simpler than hermes' `_find_latest_context_summary`
  content-prefix scan (`context_compressor.py:4401-4409` and onward) precisely because Aura
  already has a marker-field mechanism hermes has to fake with content prefixes.

## Version Compatibility

No new packages, so no new compatibility surface. Everything cited above is already pinned and
already interoperating in the current `go.mod` (Go 1.26.5): `pkoukk/tiktoken-go v0.1.8`,
`jackc/pgx/v5 v5.10.0`, `golang-migrate/migrate/v4 v4.19.1`, sqlc `v1.31.1` (CI-pinned tool, not
a module dependency), `go.uber.org/goleak v1.3.0`, `pgregory.net/rapid v1.3.0`,
`prometheus/client_golang v1.24.1`, the `go.opentelemetry.io/otel*` family `v1.44.0`/`v0.66.0`.

## Sources

- `D:/tmp/hermes-agent/agent/context_compressor.py` — constants block (lines 360-664),
  `resolve_model_threshold` (1291-1314), `update_from_response` (2429-2484),
  `should_compress`/`should_compress_info`/`_automatic_compression_blocked_locally`
  (2539-2726), `_generate_summary` incl. 429/timeout/cooldown branches (3469-4054),
  `_build_static_fallback_summary` (3203-3403), `compress()` (5934-6300+) — read directly,
  HIGH confidence (primary source).
- `D:/tmp/hermes-agent/agent/context_breakdown.py` — full file (361 lines) — read directly,
  HIGH confidence (primary source).
- `D:/Aura/internal/conversations/context.go`, `tiktoken.go`, `title.go`, `store.go` (partial),
  `store_append.go`, `internal/runner/runner_context.go`, `runner_persist.go`,
  `internal/llm/client.go`, `breaker.go`, `breaker_test.go`,
  `internal/llm/openai_compat/usage.go`, `httperror.go`,
  `internal/agent/llm_agent_stream_retry.go`, `llm_agent_retry.go`,
  `internal/db/queries/conversations.sql`, `internal/db/migrations/` listing,
  `internal/conversations/title_unit_test.go`, `internal/runner/fakes_test.go`,
  `internal/documents/retry_backoff.go`, `go.mod` — read directly, HIGH confidence
  (current codebase, ground truth).
- [OpenRouter Usage Accounting cookbook](https://openrouter.ai/docs/cookbook/administration/usage-accounting) —
  fetched 2026-08-05, confirms native-tokenizer counts, deprecated `usage.include` flag,
  streaming-final-chunk delivery, `/generation` endpoint semantics. HIGH confidence (official,
  current docs).
- `D:/Aura/docs/audit/live-conversations-2026-08-04/CONTEXT-MANAGEMENT.md` — C-1, C-4, C-6
  findings that motivate this milestone's scope — read directly, HIGH confidence (primary
  project source).
- WebSearch: "OpenRouter API usage accounting prompt_tokens native_tokens_prompt" and
  "OpenRouter usage: {include: true} response object fields" — used only to locate the cookbook
  URL above before fetching it directly; not relied on as a standalone source.

---
*Stack research for: v2.1.0 HERMES-CLAUDE_PARITY context-management additions*
*Researched: 2026-08-05*
