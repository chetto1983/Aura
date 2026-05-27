# Phase-KV Revised Plan — Adversarial Validation (2026-05-27)

Reviewer: adversarial sub-agent. Brief: find LOGICAL BUGS in the plan
`docs/phase-kv-plan-revised-2026-05-27.md` before it ships, not in the code.

Mandate: be aggressive. If something feels glossed over, mark it. The user has
made clear they are tired of paying for bugs found post-hoc.

---

## Bugs found

### BUG-1 — A03 marker placement contradicts A02a's layout (HIGH)

**Where.** `internal/llm/cache.go:96-106` `injectCacheControl` iterates
`for i := len(out) - 1; i >= 0; i--` and marks the **first system seen walking
from the tail** — that is the **last** `role=system` in the slice. A02a (plan
§3 A02a Fix 1) deliberately introduces a **second** system message that holds
the **volatile** `RenderTurnRuntimeCapsule + pinnedOperational`. The static
cacheable prefix is at `messages[0]`; the volatile mutable block is at
`messages[1]`.

A03 (plan §3 A03 Fix 2) says: *"Place breakpoints in order: 1. Last `role=system`
message (the **first** system message after A02a's split — i.e. the static one.
Not the volatile second-system)."* The two clauses contradict each other: "Last
system message" and "the **first** system message after the split" are not the
same node once A02a lands.

If A03 is implemented as written (keep `for i := len(out)-1` walk and call it
"last system"), the breakpoint will land on the **volatile** capsule — the very
content A02a moved out of the prefix to protect cache stability. The breakpoint
on a mutating block invalidates every turn. We'd ship a plan whose two stories
cancel each other out.

**Severity:** HIGH. This is the precise failure mode the plan exists to prevent.

**Fix.** A03 must rename/respec the helper. Concrete options:

- Option A (safer): replace the loop with a forward walk that anchors on the
  **first** system message: `for i := 0; i < len(out); i++ { if out[i].Role ==
  "system" ... break }`. Document that this assumes A02a's "static first,
  volatile second" invariant and add an assertion.
- Option B: introduce an explicit `chatMessage.IsCacheableAnchor bool` set by
  the message builder when it emits the static system message. Cache injection
  then targets that flag, not positional walking. This is the more
  defensive design and survives future "third system block" surprises.

A03's acceptance criteria must add a test like
`TestInjectCacheControl_LandsOnStaticNotVolatile` that synthesizes two
adjacent system messages (static at [0], volatile at [1]) and asserts the
breakpoint is on [0].

---

### BUG-2 — A02a's Fix 3 nudges-as-trailing-user defeats the 20-block lookback (HIGH)

**Where.** Plan §3 A02a Fix 3: "each `InjectSystemExtras` call emits a **separate
trailing `role=user` message** instead of mutating system[0]". Today the loop
calls this **3× per iteration** (briefer + already-done + step hint). Over a
typical 8-turn Aura conversation with ~3 tool calls each, that adds
`3 nudges × 8 turns = 24` trailing user pseudo-messages, on top of real
assistant + tool_result blocks.

Anthropic's lookback window (`docs/kv-cache-research/providers-state-2026-05-20.md`
§ Anthropic § Eligibility) is **20 content blocks** from a breakpoint backward.
A03 plans to add a "rolling tail anchor" breakpoint on the **last user/tool**
message to compensate — but the new pseudo-user nudges will be the tail. The
rolling anchor will land on a nudge that mutates every iteration (`RenderStepHint`
content changes byte-by-byte: "step 1/5", "step 2/5", ...). Anchor on mutating
block → anchor never hits in cache.

Worse: between iterations of the **same turn**, the previous iteration's nudges
become history. By turn 3 a single turn has injected 9 nudge blocks. After 8
turns, ~72 extra user-role blocks live in `state.Messages()`. The static-prefix
breakpoint at `messages[0]` has slid >>20 blocks back — out of lookback range.

**Severity:** HIGH. A02a Fix 3 directly enables what A03 Fix 2 step (4) tries
to mitigate, and the mitigation only works when the trailing block is stable.

**Fix.**

- Nudges should NOT be persisted into `state.Messages()` history at all. They
  are per-iteration, single-LLM-call ephemera. Today's `InjectSystemExtras`
  returns a fresh slice without mutating state — it builds
  `messagesForModel` only. The plan's renamed `AppendTrailingNudge` must
  preserve that property: append to `messagesForModel` only, never to
  `state.Messages()`. Add an explicit test
  `TestAppendTrailingNudge_StateUnchanged` asserting `state.Messages()` is
  unchanged after 3 nudges.
- For the rolling-anchor breakpoint, document that it must point at the **last
  real user message or last real tool_result** — explicitly skip nudge
  messages. A03 needs a `findRollingAnchor` that walks tail-to-head and
  ignores messages with a `Ephemeral bool` flag (or marker text). Otherwise
  the anchor lands on the mutating step-hint.
- Alternatively (simpler): collapse all 3 nudges into a single trailing user
  message with a deterministic ordered concatenation, and place it
  **immediately before** the real user turn so the anchor on the real user
  turn is the tail.

The plan does not address either of these. Mark §3 A02a "Risks" as incomplete.

---

### BUG-3 — A03's "rolling tail anchor moves per turn" → no second-turn hit (HIGH)

**Where.** Plan §3 A03 Fix 2 step (4): *"Rolling tail anchor: last `role=user`
or `role=tool` message ... **This moves with the conversation** — that's the
whole point."*

The cache hierarchy on Anthropic is **prefix-exact**: a breakpoint marks "cache
content up to and including this block". A breakpoint moved one block to the
right between turn N and turn N+1 means **turn N+1's anchor was not present at
turn N**, so there is **no cache entry to hit at that exact prefix length**.
What works is: turn N writes cache up to anchor_N; turn N+1 attempts a read at
anchor_N+1 = anchor_N + (new content). The cache LOOKUP, however, uses the
prefix up to anchor_N+1, which doesn't exist as a saved entry — turn N saved
the entry at anchor_N. The provider falls back to the longest existing prefix
match, which IS anchor_N. So the rolling anchor "works" as long as the
new blocks at the tail are also stable on the wire, which they aren't if
nudges are interleaved (see BUG-2).

The hand-wavy line "this moves with the conversation — that's the whole point"
glosses over what the lookback window does. Per Anthropic's docs: 20 blocks
**from the breakpoint backward** in the cache engine's saved index. If the saved
entry is from anchor_N (turn 1), and turn 8's request has anchor_N+1 ... +7
appended **before** sending, the saved index at anchor_N is still visible if
≤20 blocks have been added since. Past 20 blocks the saved entry expires from
the index.

The plan says "after ~5 turns with multiple tool_results per turn ... falls out
of the lookback window" (§1 Killer #5). This is the killer the plan
acknowledges, but the proposed mitigation is "rolling tail anchor", which only
helps if you **re-write** the cache at the new tail position. Cache writes are
billed at 1.25× (5m TTL) or 2× (1h TTL). The plan does not model the per-turn
cache-write cost of moving the anchor. If every turn pays a fresh cache-write
on the rolling anchor, the math in §3 (target hit ratio ≥ 0.70) needs revising:
the **read** portion is the tail blocks (small); the **prefix** read is still
the big win. Plan doesn't break this down.

**Severity:** HIGH for the hit-ratio target (≥0.70 §2 goal #1); MEDIUM for
correctness (the cache still works, the math is just off).

**Fix.** A03 acceptance criteria must include a "cache cost model" comment
block in the commit that breaks down per-turn:

- prefix write (turn 1 only): N tokens × 1.25
- prefix read (turn 2..8): N tokens × 0.10
- rolling-anchor write (every turn): K tokens × 1.25 (where K is the delta
  added since previous anchor)
- output: unchanged

And the hit-ratio target should be specified as `cached_input_tokens /
prompt_tokens_total`, **NOT** divided by `(prompt_tokens_total -
cache_creation_input_tokens)` as A04 currently says (see BUG-7).

---

### BUG-4 — A02a's "multiple system messages universally supported" is undertested (MEDIUM)

**Where.** Plan §3 A02a Fix 1 Decision: "Multiple system messages are
universally supported in OpenAI-compatible APIs and natively by Anthropic."
The plan §6 Risks gives this a "LOW probability, HIGH impact" rating with
mitigation "A01 surveys the providers Aura currently targets".

This is **not** universally supported. Verifying:

- **OpenAI:** accepts multiple `role=system` but only the first is treated as
  the "system prompt"; subsequent ones are interpreted as user-side
  declarations on some model variants. Documented inconsistently — works on
  4o/5.x, was different on 3.5-turbo, undocumented for fine-tuned models.
- **Anthropic native messages API:** does NOT accept multiple system messages.
  It accepts a single `system` parameter that can be a string OR an **array of
  content blocks**. The wire format is fundamentally different. If Aura is on
  the OpenAI-compatible passthrough to Anthropic (via Bedrock or OpenRouter),
  the OpenAI→Anthropic translator decides what to do with multiple system
  messages — and the answer varies by translator.
- **vLLM:** depends on the chat template baked into the model. Llama-3.x
  chat template tolerates a second system; Mistral-Instruct templates emit
  warnings and may concatenate or drop.
- **llama-server:** same — depends on `--chat-template`. Default Jinja
  templates from HF do not all support repeated system.
- **Mistral via OpenAI-compat (`api.mistral.ai/v1`):** documented to accept
  only ONE system. Will 400 on two.
- **OpenRouter:** depends on upstream. Anthropic upstream: see above. OpenAI
  upstream: works.

The plan's mitigation "fall back to trailing user pseudo-system" is described
in §3 A02a Fix 1 Option 3 as an alternative. The plan picks Option 1 (multi-
system) as primary. There is **no detection mechanism specified** to know
when to fall back. The capability matrix in A03 has `SupportsCacheControl`
but no `SupportsMultipleSystemMessages`.

**Severity:** MEDIUM. The bug is the plan's confidence, not the design.

**Fix.**

- Add `SupportsMultipleSystemMessages bool` to A03's `CacheCapability`
  struct. Default false. Set true for `openai_auto`, `anthropic_ephemeral`
  (native messages API where the array-of-blocks is the canonical form), and
  known-good OpenRouter routes.
- A02a Fix 1 must implement Option 1 AND Option 3 (trailing user
  pseudo-system) and select between them based on the capability flag.
- Add a test `TestComposeAgentPrompt_FallbackToTrailingUserOnStrictProvider`
  that flips the flag and asserts the volatile capsule ends up as a
  trailing user message, not a second system.
- A01 audit scope must enumerate Aura's currently-tested endpoints and their
  multi-system support. If A01 finds zero providers that strictly forbid it,
  YELLOW is acceptable, but the fallback code must still exist for future
  endpoints.

---

### BUG-5 — A02a's test `TestComposeAgentPrompt_StaticByteStableAcrossTime` is necessary but insufficient (MEDIUM)

**Where.** Plan §3 A02a Acceptance criteria: *"New test `TestCompose
AgentPrompt_StaticByteStableAcrossTime` proves it: call with `now=t1`,
then `now=t2`, assert `Static` is identical bytes."*

This proves `ComposeAgentPrompt`'s return value `Static` is stable. It does
NOT prove what we actually care about: **the full pipeline including
`convCtx.Messages()` after `SetSystemMessage(Static) +
SetVolatileSystemMessage(Volatile)` produces a request payload where the
cacheable prefix is byte-stable across turns.**

Specifically, the bug surface is in `rebuildSystemMessage`
(`internal/conversation/context.go:251-273`): today it concatenates
`baseSystemPrompt + agentNoteContent + searchContext` and writes the result
to `messages[0].Content`. After A02a, the plan adds
`SetVolatileSystemMessage` and a sibling `rebuildVolatileSystemMessage`. If
those two rebuild paths share any mutable state, or if `SetSearchContext`
fires mid-turn and rewrites `messages[0]`, the static prefix is no longer
stable on the wire even if `ComposeAgentPrompt` is deterministic.

Also: `SetAgentNote` (`context.go:238`) and `SetSearchContext` (`context.go:245`)
**also** mutate `messages[0]` via `rebuildSystemMessage`. A02a does not list
these as touch points — but `agentNoteContent` and `searchContext` are
turn-volatile by design. So `messages[0]` continues to mutate per turn even
after A02a "splits" the prompt. The static prefix is NOT actually static.

**Severity:** MEDIUM (correctness) but HIGH (the test gives false confidence).

**Fix.**

- A02a scope must explicitly cover `SetAgentNote` and `SetSearchContext`.
  Decision: move both into the volatile second-system message OR exclude
  both from the cacheable prefix.
- Acceptance criteria must add a test
  `TestContext_StaticPrefixByteStable_AcrossTurnsWithVolatileChurn` that:
  1. Sets static (`SetSystemMessage(static)`).
  2. Calls `SetSearchContext("turn 1 ctx")` + `SetVolatileSystemMessage(v1)`.
  3. Captures `Messages()[0].Content` (the cacheable static).
  4. Calls `SetSearchContext("turn 2 ctx")` + `SetVolatileSystemMessage(v2)`.
  5. Asserts `Messages()[0].Content` is identical bytes to step 3.
  6. Asserts `Messages()[1].Content` did change (volatile is mutable).

Without this test, A02a "passes" with a regression intact.

---

### BUG-6 — A02a "Behavior change risk" Fix 3 mitigation is hopeful, not evidenced (MEDIUM)

**Where.** Plan §3 A02a Fix 3: *"For 'briefer capsule' (tool-failure summary)
it's a downgrade in authority. Mitigation: prefix the briefer capsule string
with `'## Runtime briefing (system-injected)'` so the model still treats it
as authoritative."*

There is no evidence in the plan that prefixing a `role=user` message with
`"## Runtime briefing (system-injected)"` causes the LLM to treat it as
system-authoritative. Empirically:

- Claude (Anthropic) treats `role=user` content as user-side regardless of
  in-band markers. Anthropic's own guidance is to use the `system` parameter
  for system-authoritative content. Markdown prefixes are not a privilege
  channel.
- OpenAI models similarly. Roles are the privilege boundary.
- This is the same pattern as "prompt injection defense" — and the consensus
  is roles + structure, NOT in-band labels.

The mitigation is hopeful. It may work for benign nudges (the model usually
follows the briefing-style markdown out of cooperation) but is NOT a real
authority restoration.

**Severity:** MEDIUM. The mitigation MIGHT work but the plan presents it as
a solved risk.

**Fix.** Either:

- Accept the authority downgrade explicitly: state that briefer capsules are
  now hints, not policy. Add a probe test that fires a briefer-triggering
  fixture (tool failure → capsule) and asserts the model's next turn
  acknowledges the failure constraint. If the test passes consistently, the
  downgrade is harmless in practice. If it doesn't, choose path 2.
- Move the briefer capsule into the **volatile second-system message**
  (option 1, with the multi-system support flag from BUG-4). Other two
  nudges (step hint, already-done) can stay as trailing user.

The plan currently bundles all 3 nudges identically; that's wrong because
they have different authority requirements.

---

### BUG-7 — A04's hit-ratio denominator is wrong on at least one provider (MEDIUM)

**Where.** Plan §3 A04: *"`kv_cache.hit_ratio_24h` denominator is
`prompt_tokens_total - cached_create_tokens` (i.e. the billed-at-full-rate
portion is what we measure cache against, not the gross prompt)."*

This formula is wrong for the stated purpose:

- The "billed-at-full-rate portion" on Anthropic = `input_tokens` (uncached
  input) + 0 (cached reads are 0.1×). Cache *creations* are 1.25× or 2×, not
  full rate.
- `prompt_tokens_total` on the OpenAI wire shape is the gross prompt token
  count `input_tokens + cached_tokens` (or some providers report it as the
  uncached portion + cached separately). Subtracting `cache_creation_input_tokens`
  from that gives... a meaningless number on most providers, because
  cache_creation is typically already counted in input_tokens, not
  prompt_tokens_total.

The canonical hit ratio per `docs/kv-cache-research/providers-state-2026-05-20.md`
§ Consensus point 5 is: `cache_read_input_tokens / total_input_tokens`
(NOT minus anything). The Anthropic reference target ≥0.70 uses this formula.

A04 also says it tracks rolling 24h. On OpenAI auto-cache, the provider
only returns `prompt_tokens_details.cached_tokens` (a single integer per
response). There is no `cache_creation_input_tokens` on OpenAI. On Gemini,
there's `cached_content_token_count`. The formula needs per-provider
specialization.

**Severity:** MEDIUM. Bad metric → false-positive PASS at the §2 goal #1 gate.

**Fix.**

- A04 must define hit_ratio per-provider:
  - Anthropic: `cache_read_input_tokens / (cache_read_input_tokens +
    cache_creation_input_tokens + input_tokens)`.
  - OpenAI: `prompt_tokens_details.cached_tokens / prompt_tokens`.
  - Gemini: `cached_content_token_count / prompt_token_count`.
- Document in a code comment which provider each branch handles.
- A04 acceptance criteria must include 3 unit tests, one per provider's
  usage shape, each asserting the formula.

---

### BUG-8 — Dependency claim "A02b before A03" not justified; A02a alone insufficient for A03 (MEDIUM)

**Where.** Plan §4 Sequencing: `A01 → A02a → A02b → A03 → A04 → A05` with the
note "A02b ... can be parallel to A02a in principle, but serial keeps Ralph
one-story-exit clean".

Two issues:

1. **A03 depends on A02b too.** A03 places a cache breakpoint on the last tool
   in `tools[]` (plan §3 A03 Fix 2 step (2)). If A02b's lexicographic ordering
   has NOT landed, the last tool varies across MCP reloads → the breakpoint
   moves → cache miss. So A03 depends on **both** A02a and A02b, not just
   A02a. The plan's dependency line "A02a blocks A03" is incomplete.

2. **Rollback claim is wrong.** Plan §3 A02a Rollback: *"revert the commit.
   The system goes back to per-turn cache miss but otherwise works."* But by
   the time A02a is reverted, A02b and A03 have shipped on top. A03 expects
   a two-system-message layout. Reverting A02a (which split the prompt)
   without also reverting A03 means A03's `injectCacheControlAnthropic`
   walks the messages looking for a static-second-system pattern that no
   longer exists. Behavior: at best, the breakpoint lands on the old
   single-system block (which now contains the volatile capsule), and we
   poison the cache again. At worst, it crashes if A03 added struct fields
   A02a was supposed to populate.

**Severity:** MEDIUM. The plan's stated rollback story is wrong.

**Fix.**

- Sequencing should explicitly list A03 as depending on **both** A02a AND
  A02b. The diagram should be `A01 → A02a → A02b → A03` with A02a→A03 and
  A02b→A03 as separate edges.
- Each story's Rollback section must say "revertable as long as no later
  Phase-KV story has shipped on top. If A03 has shipped, revert A03 first."
- Or: design A03's cache-control injection to be **defensive** — detect
  whether there are 1 or 2 system messages and adapt. This is more code but
  decouples the stories.

---

### BUG-9 — Rolling 50-msg sliding window can evict the static cached prefix (MEDIUM)

**Where.** `internal/conversation/context.go:156-194` `enforceMessageCap`
drops oldest non-system messages when count exceeds `maxMessages` (CLAUDE.md
says default 50). The system message at `[0]` is preserved.

Plan §3 A02a introduces a **second** system at `[1]` (volatile). The current
`enforceMessageCap` only excludes `messages[0]` from the cap counter
(`hasSystem := ... messages[0].Role == "system"; if hasSystem: body = body[1:]`).
The second system at `[1]` is treated as part of the body and counts against
the 50-message cap. Worse: when `enforceMessageCap` drops oldest non-system
messages, it `body[split:]` — which preserves messages from `split` to end.
If `[1]` (the second system) is in the dropped range, it's gone. Likely it
won't be dropped because `split = len(body) - maxMessages` keeps the
**latest** maxMessages, and the second system at `[1]` is the OLDEST
non-system → it WILL be evicted once the conversation grows past 50 body
messages.

Result: A02a's second-system disappears mid-conversation. The model's next
turn has only the static prefix, no runtime capsule. Behavior change.

The plan's §3 A02a Fix 1 implementation steps say `Context.SetVolatileSystem
Message(content)` exists and "the volatile message lives at `messages[1]`
if a system [0] exists, else [0]". It does NOT say what happens when
`enforceMessageCap` runs.

**Severity:** MEDIUM. Manifests only on long conversations (>50 msg), but
they exist in Aura's archive.

**Fix.**

- `enforceMessageCap` must skip **all leading system messages**, not just
  `[0]`. Generalize: `for hasSystem && body[0].Role == "system" { body =
  body[1:] }`.
- Same applies to `toolSafeBoundary`, `truncateMessages`, `trimOldest`,
  `Summarize` — every place that excludes `messages[0]` because it's
  system. A02a must audit all of them. The plan mentions
  "`governance.ScrubOrphanToolCalls` and the rest of the message-list
  transforms must tolerate two consecutive `role=system` messages" but
  doesn't enumerate the sliding-window code paths inside
  `internal/conversation`.
- Add a test `TestContext_VolatileSystemSurvivesMessageCap` that pushes
  60 messages and asserts both system messages remain.

---

### BUG-10 — A01 audit scope omits `SetAgentNote` and `SetSearchContext` as cache mutators (LOW)

**Where.** Plan §3 A01 Scope. Lists Killers #1-#5 to confirm. Does NOT list
the fact that `Context.SetAgentNote` and `Context.SetSearchContext` write
into `messages[0].Content` via `rebuildSystemMessage`. These are turn-
volatile mutations of the cached prefix — a Killer in their own right (call
it Killer #6).

A01 will produce an audit that fails to flag the real bug because the audit
didn't look for it. A02a will then ship with the static-prefix-still-mutates
regression (see BUG-5).

**Severity:** LOW (process) but contributes to BUG-5 (test) and BUG-9
(eviction).

**Fix.** Add to A01 scope an item: *"7. Enumerate every call site that
mutates `messages[0]` after `SetSystemMessage` has been called.
`SetAgentNote`, `SetSearchContext`, `InjectSystemExtras` are the known three.
Any unknown fourth must be documented."*

---

## Open questions the plan does not answer

1. **What is `cfg.PromptVersion`?** §6 Risks table mentions it as a known
   "minor cache-bust on prompt-version bump". Where is it injected? If it's
   in the static prefix, every operator upgrade busts the cache for all
   users. Should be documented in A01. Currently a black box.
2. **Cache_control on `toolWrapper` MarshalJSON** — A03 Fix 2 step (2) says
   *"add `CacheBreakpoint bool` + custom MarshalJSON on `toolWrapper`."*
   What's the wire shape on Anthropic vs OpenAI for "marker on a tool def"?
   On Anthropic native it's a `cache_control` key inside the tool object.
   On OpenAI-wire to Anthropic-via-OpenRouter, is it the same? Or wrapped
   somewhere else? Test fixtures need to be explicit.
3. **What happens on streaming SSE when usage cache fields appear only on
   the last event?** `docs/kv-cache-research/providers-state-2026-05-20.md`
   § Pitfalls flags this explicitly. A04 must verify the streaming path
   captures the final-event usage, not a mid-stream zero. Plan does not
   call this out.
4. **`AURA_LLM_CACHE_ENABLED=auto` — how does "auto" detect Anthropic?** By
   model name? Base URL? Both? If both, what's the precedence? Plan §3 A03
   Fix 1 says "prefer explicit env `LLM_PROVIDER`; fall back to hostname
   heuristic on `cfg.LLMBaseURL`". What if the hostname is
   `api.openrouter.ai`? Plan doesn't say if OpenRouter is anthropic_
   ephemeral or openai_auto. It's neither — it depends on the *upstream*
   model. Needs spec.
5. **Conversation archive cross-session replay.** The plan says A01 will
   audit `archive.go` literal-bytes preservation but defers to "matters for
   cross-session cache scenarios but not single-conversation cache hit rate."
   On Anthropic 5m TTL, cross-session within 5m IS a thing for follow-ups
   (the §3 §1 ROI model in the providers-state doc assumes a 30-60min
   follow-up burst). If cross-session re-replay isn't byte-faithful, the
   1h TTL recommendation breaks. Plan should commit to: byte-faithful
   archive replay is in-scope for Phase-KV, or explicitly defer with
   a follow-up phase named.

---

## Verdict

**YELLOW — proceed only after the plan is amended.**

The plan correctly identifies the three independent cache killers in the
current codebase. The story decomposition (A01 audit, A02a prefix de-poison,
A02b wire-time, A03 markers, A04 observability, A05 probe) is sound.

But the plan has **three HIGH-severity bugs** that, if shipped as written,
will produce a Phase-KV that "passes its own tests" while delivering ~0%
real cache hit rate:

- **BUG-1:** A03's `injectCacheControl` walks tail-to-head and marks
  the volatile second system, undoing A02a's separation.
- **BUG-2:** A02a's Fix 3 turns 3 nudges per iteration into 3 trailing
  user messages, blowing past Anthropic's 20-block lookback within one
  long turn.
- **BUG-3:** A03's "rolling tail anchor" sleight-of-hand doesn't address
  the lookback-window math. The §2 goal #1 hit-ratio target is unreachable
  as specified.

Plus four MEDIUM bugs around test coverage (BUG-5), multi-system support
detection (BUG-4), authority-downgrade hopeful mitigation (BUG-6), wrong
hit-ratio formula (BUG-7), broken rollback claims (BUG-8), and a sliding-
window eviction edge case (BUG-9).

**Required amendments before promotion to `prd.json`:**

1. A03 must specify breakpoint placement by **explicit anchor flag**, not
   positional walking (BUG-1).
2. A02a Fix 3 must guarantee nudges do NOT enter `state.Messages()` history
   (BUG-2). Add the explicit test.
3. A02a scope must include `SetAgentNote` and `SetSearchContext` (BUG-5,
   BUG-10).
4. A03's `CacheCapability` must add `SupportsMultipleSystemMessages` and
   A02a must implement the fallback (BUG-4).
5. A04's hit_ratio formula must be per-provider (BUG-7).
6. Sequencing diagram must list A03's dependencies on BOTH A02a and A02b;
   rollback section per story must state ordering constraint (BUG-8).
7. `enforceMessageCap` and siblings must skip all leading system messages,
   not just `[0]` (BUG-9).
8. A01 scope must enumerate every `messages[0]` mutator and flag
   `cfg.PromptVersion` injection site (BUG-10, OQ-1).

After these amendments the plan can promote to GREEN. Without them, shipping
this plan is the exact "10000 bug" pattern the user wants stopped before
code is written.
