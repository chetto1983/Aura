# Claude deferred-tool probe (staged, not run)

This probe checks one candidate fix before any adapter code is written. It is step 1 of the operator's
OK of 2026-11-28 in the ledger `.superpowers/sdd/2026-11-27-anthropic-provider/progress.md`.

**The problem.** The ledger's "VM read-back 2" recorded what happens on the Claude route today. Every
request right after a `tool_search` showed `cache_read 0`, `cache_write ~40k` and `dropped_blocks 1`. It
cost about $0.21 where its neighbours cost $0.04 to $0.07. The cause is that `tool_search` grows the
`tools` array. Tools are the first part of the prefix, so the whole prefix is rewritten, and the signed
thinking bound to the old prefix is dropped.

**The candidate fix.**
- Send every deferred tool from the first request, with `defer_loading: true`.
- Have Aura's own `tool_search` return `tool_reference` blocks inside its `tool_result`.

The `tools` array then never changes.

## Run it (paid, about 8 billed calls on Sonnet 5.5)

```bash
# WSL, from /mnt/d/Aura. The key lives in a file outside the repository; it is never printed.
go run ./spikes/anthropic-provider/probe-defer -keyfile <file holding a Claude Console key> \
  -model claude-sonnet-5-5 -case all
```

Flags:

| Flag | Values | Default |
|---|---|---|
| `-case` | `contract`, `baseline`, `defer` (runs the replay too), `unreferenced`, `all` | `all` |
| `-result` | `mixed`, `refs`, `spec` (see below) | `mixed` |
| `-display` | `omitted` or `summarized` | `omitted` |
| `-effort` | output effort | `high` |
| `-price-*` | USD per 1M tokens | Sonnet 5.5's 2 / 10 / 0.10 / 2.50 (spec §5.7) |

`-result` sets what the defer case's `tool_search` result holds:
- `mixed`: a short text plus the reference;
- `refs`: the reference only;
- `spec`: Aura's full spec text plus the reference.

Calls:

| Case | Calls | Billed |
|---|---|---|
| `contract` | 3 | No: each expects a 400 |
| `baseline` | 2 | Yes |
| `defer` | 3, plus 2 replays | Yes |
| `unreferenced` | 1 | Yes |

Each case salts the system prompt (`Probe <case>-<random>`), so every case starts cold and a re-run never
reads an earlier run's cache. Each call prints:
- `in`, `read`, `write`, `out` and `prompt` (in + read + write);
- `dropped`: the number of `thinking_dropped` entries;
- the `input_transformations` list;
- a digest of the `tools` array;
- the cost at the `-price-*` rates.

A total is printed at the end.

Build gate (2026-11-28, WSL): the probe builds, `go vet` passes, and `golangci-lint run
./spikes/anthropic-provider/probe-defer/...` reports 0 issues.

## What each case measures

The shapes match Aura's wire. Thinking is adaptive, with the `thinking-binding-controls-2026-08-01` beta
and `prefix_mismatch_behavior: drop_block`, as `internal/llm/anthropic/request.go:71-75` sends. Tools are
sorted alphabetically, as `RenderToolDefs` sorts them.

Cache breakpoints sit where Aura puts them:
- the system prompt;
- the tools;
- the newest message, which is the stable prefix;
- in a second turn, the end of the loaded history.

The catalogue has 21 deferred tools:
- `stock_quote`. Its argument names and its `px_basis` rule cannot be guessed from its name.
- 20 long padding tools. Half sort before `tool_search` and half after it, so the tools breakpoint lands
  mid-array, as it would in Aura.

**`baseline`: today's behaviour.**
1. Round 1 sends only `calc` and `tool_search`. The model calls `tool_search`.
2. Round 2 sends the result as `RenderSpec` text, with `stock_quote` now added to `tools`.

Expected: round 2 has `read` near 0, `write` equal to the whole prompt, and `dropped` at 1 or more. The
round-1 thinking count is printed, because a drop needs a thinking block to drop.

**`defer`: the fix.**
1. Every tool is present from round 1. The model calls `tool_search`.
2. The result holds `tool_reference: stock_quote`. The model calls `stock_quote`. The probe prints whether
   the arguments match the schema. With `-result mixed|refs`, no schema text reached the model, so correct
   arguments prove the API expanded the reference.
3. The model gives its final answer.

It also prints whether the tools digest was identical on every round. Expected: `read` covers the whole
earlier prefix, `write` covers only the new tail, and `dropped` is 0.

**Replay (inside `defer`).** A second user turn ("Now the same for GLOBEX, in pounds.") is sent twice:
- verbatim, with the thinking kept;
- in Aura's reload shape, with every assistant turn's thinking stripped.

A direct `stock_quote` call with correct arguments, and no new search, means the reference in the earlier
`tool_result` still loads the tool across turns.

**`unreferenced`.** The prompt asks the model to call `warehouse_03`, which nothing has referenced. The
result shows whether the model can emit a call to a deferred tool it was never shown. That decides whether
Aura's dispatch gate still has work on this route.

**`contract`.** Each of these should be a 400, and none should be billed:
- `cache_control` on a deferred tool;
- a `tool_reference` to an unknown name;
- every tool deferred.

## 1. Docs contract

All pages were read on 2026-11-28 with WebFetch.
- The tool search page and the prefix-check page were read in full.
- The tool-use caching page and the change-tools page were read in full.
- The thinking page was 82 KB. Only its "Thinking with tool use", "Preserved thinking" and "Thinking and
  prompt caching" sections were read.
- The mid-conversation page was read in two passes, at offset 0 and offset 100000. The first pass came back
  as a summary with quotes, not verbatim text.

### Tool search: the custom client-side form is documented and GA

Source: [tool-search-tool](https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-search-tool).
The page says `status: stable` and lists `claude-opus-5-5` and `claude-sonnet-5-5`. It names no beta
header: the quick start sends only `anthropic-version`.

The custom search may be any custom tool:

> "You can implement your own tool search logic (for example, using embeddings or semantic search) by
> returning `tool_reference` blocks from a custom tool. When Claude calls your custom search tool, return a
> standard `tool_result` with `tool_reference` blocks in the content array:
> `{"type": "tool_result", "tool_use_id": "toolu_your_tool_id", "content": [{ "type": "tool_reference",
> "tool_name": "discovered_tool_name" }]}`"
>
> "Every tool referenced must have a corresponding tool definition in the top-level `tools` parameter,
> normally with `defer_loading: true`."
>
> "For custom client-side implementations, always use the standard `tool_result` format with
> `tool_reference` content blocks."

The whole catalogue is sent on every request:

> "You still send every tool's full definition in the `tools` array on every request, including the
> deferred ones. The API needs them server-side to run the search and expand `tool_reference` blocks."
>
> "Never set `defer_loading: true` on the tool search tool itself."

At least one tool must stay non-deferred:

> "At least one tool, normally the tool search tool itself, must stay non-deferred."

The 400 for breaking that rule reads: "At least one tool must have defer_loading=false. All tools cannot
be deferred."

References persist across turns:

> "The API expands `tool_reference` blocks throughout the conversation history, so Claude can reuse
> discovered tools in later turns without re-searching."

That sentence is in the server-side section. The custom section says the API "expands the returned
`tool_reference` blocks the same way".

Limits:
- "Maximum deferred tools: 10,000 tools with `defer_loading: true` for each request".
- An unknown reference is a 400: "Tool reference 'unknown_tool' not found in available tools".

Caching:

> "A tool with `defer_loading: true` can't also carry `cache_control`: the API returns a 400. Put the cache
> breakpoint on a non-deferred tool."

Usage:

> "the tool definitions that search loads into context count as input tokens like any other tool
> definition."

Strict mode:

> "The grammar for strict mode ... builds from the full list of tools, so using `defer_loading` with strict
> mode doesn't make the API recompile the grammar."

### Prompt caching keeps the prefix

Source: [tool-use-with-prompt-caching](https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-use-with-prompt-caching).

> "Deferred tools are not included in the system-prompt prefix. When the model discovers a deferred tool
> through tool search, the definition is appended inline as a `tool_reference` block in the conversation
> history. The prefix is untouched, so prompt caching is preserved."

Its invalidation table, for the rows that matter here:

| Change | Invalidates |
|---|---|
| "Modifying tool definitions" | "Entire cache (tools, system, messages)" |
| "Changing `tool_choice`" | "Messages cache" |

### Preserved thinking: an unreferenced deferred tool is not part of the prefix

Source: [prefix-checks](https://platform.claude.com/docs/en/build-with-claude/preserved-thinking/prefix-checks).
It applies to Opus 5.5, Sonnet 5.5 and Haiku 5.5.

The prefix check fails "if `system`, `tools`, or an earlier message changed after Claude produced a block."

Its "What doesn't count" table lists two rows that matter here:

| Doesn't count | Why |
|---|---|
| "A tool with `defer_loading: true` that nothing has referred to" | "It isn't in Claude's context until something refers to it" |
| "`tool_choice`" | "Only `system`, `tools`, and messages are compared" |

Source: [change-tools-or-instructions](https://platform.claude.com/docs/en/build-with-claude/preserved-thinking/change-tools-or-instructions).

> "A tool with `defer_loading: true` stays out of Claude's context and the prefix check until a message or
> tool search result refers to it. Until then, adding, updating, or removing it isn't a change. Afterward
> it is. Turning `defer_loading` on or off counts as changing `tools`, too."

The `thinking-binding-controls-2026-08-01` beta adds `block_binding.prefix_mismatch_behavior` and the
response's `input_transformations` array.

### Tool choice with thinking

Source: [thinking](https://platform.claude.com/docs/en/build-with-claude/thinking). Adaptive thinking
supports forced tool use "except on Claude Opus 5.5, Claude Sonnet 5.5, ...". Aura sends only `auto` and
`none` (`request.go:40-42`), so forced tool use is out of scope here.

### The alternative route, recorded and not chosen

Sources: [mid-conversation-system-messages](https://platform.claude.com/docs/en/build-with-claude/mid-conversation-system-messages)
and change-tools-or-instructions.

A `role: "system"` message can carry a `tool_addition` block: `{"type": "tool_reference", "name": "..."}`.
It offers a declared deferred tool from that point on.

It needs a beta header:
- `inline-tools-2026-09-15`; or
- `mid-conversation-tool-changes-2026-07-01`.

Two constraints:
- "A `system` message that carries content ... must immediately follow a `user` turn".
- "It cannot sit between a `tool_use` block and its `tool_result`."

It is the documented way to load a tool with no search result to hang a reference on, which is the case
for Aura's turn-recall preload (see §3). Aura hoists every system message into top-level `system` today
(`internal/llm/anthropic/blocks.go:58-61`), so this route would be new wire work.

## 2. SDK inventory

Version: `github.com/anthropics/anthropic-sdk-go/v2` v2.15.0, in the WSL `GOMODCACHE`. **The SDK supports
everything the fix needs, in the Beta types Aura already uses.**

| Need | Type and field | `betamessage.go` |
|---|---|---|
| Defer a tool | `BetaToolParam.DeferLoading param.Opt[bool]` (`json:"defer_loading,omitzero"`), beside `CacheControl` and `Strict` | 19521 (field at 19539) |
| A reference block | `BetaToolReferenceBlockParam{ToolName string; CacheControl BetaCacheControlEphemeralParam; Type constant.ToolReference}`, documented as "Tool reference block that can be included in tool_result content." | 20338 |
| A reference inside a tool result | `BetaToolResultBlockParamContentUnion.OfToolReference *BetaToolReferenceBlockParam`. Its siblings are `OfText`, `OfImage`, `OfSearchResult`, `OfDocument` and `OfBrowserState`, so text and a reference can share one result. | 20390 |
| The system-message route | `BetaToolChangeToolReferenceParam` (19840); `OfToolAddition *BetaRequestToolAdditionBlockParam` (7604) | |
| The drop signal | `BetaThinkingDroppedInputTransformation`, type `"thinking_dropped"` | 13398 |

Beta constants in `beta.go`:
- `AnthropicBetaThinkingBindingControls2026_08_01` (109);
- `AnthropicBetaMidConversationToolChanges2026_07_01` (101);
- `AnthropicBetaInlineTools2026_09_15` (112).

There is **no** `advanced-tool-use` constant, which fits tool search being GA.

GA mirrors exist in `message.go`:
- `ToolParam.DeferLoading` (13394);
- `ToolReferenceBlockParam` (13694);
- `ToolResultBlockParamContentUnion.OfToolReference` (13736).

Two server-side tool types exist and are not needed: `BetaToolSearchToolRegex20251119Param` and
`BetaToolSearchToolBm25_20251119Param`. Aura's search is client-side.

The 400 for an unknown reference is typed as `shared.ToolReferenceUnresolvedErrorDetails` (`aliases.go:1275`).

## 3. Aura inventory

Paths are under `internal/`, read on 2026-11-28 at `015c660ed`.

### How `tool_search` works today

- `agent/tools/spec.go:53`: `Spec.Deferred` hides a tool's full spec until `tool_search` loads it.
- `agent/tools/spec.go:321-325`: `Registry.Validate` requires one non-deferred tool besides
  `tool_search`, so the API's at-least-one rule holds.
- `agent/tools/search.go:58-80`: `tool_search` is itself non-deferred (`Deferred: false`, line 78).
- `agent/tools/search.go:150-252`: `Execute` writes each loaded schema as text with
  `RenderSpec`, which prints `## name`, the description and `Parameters:` (lines 205 and 225, renderer at
  273). It puts the deferred names in `Meta[MetaActivatedTools]` (line 250).
- `agent/llm_agent_dispatch.go:139-143`: the dispatch loop runs `promoteFromMeta`
  (`agent/llm_agent_promote.go:195`), which adds the names to `a.activated` and `a.everLoaded`. The
  `RoleTool` message stores only `ToolCallID` and `Content`; `Name` is unset.

### How the loaded set grows `llm.Request.Tools`

1. `agent/llm_agent.go:561-566` calls the builder with `a.activated`.
2. `agent/prompt/builder.go:172` sets `Tools: reg.RenderToolDefs(activated)`.
3. `agent/tools/manifest.go:67-93`: `RenderToolDefs` skips a deferred tool absent from `activated`
   (line 74) and sorts by name.

So a promoted tool is inserted into the alphabetical array. That is what moves the prefix.

Across turns, the promoted set is rebuilt from the history:
- `agent/llm_agent_construct.go:46` calls `deriveActivated` (`llm_agent_promote.go:51`). It parses earlier
  `tool_search` results with `loadedSchemas` (line 166), paired with their call by `ToolCallID`.
- `deriveActivated` keeps the 10 most recently called tools (`maxPromotedDeferredTools`, line 33) plus the
  last lookup.
- `deriveEverLoaded` (line 131), which the dispatch gate reads, never forgets.

### Three writers to the loaded set

1. The `tool_search` result.
2. The dispatch gate, `agent/llm_agent_tool.go:169-181`. It refuses an unloaded deferred call, returns
   `RenderSpec`, and promotes through `MetaActivatedTools`.
3. Turn-recall `preloadTools`, `agent/llm_agent_turn_reading.go:175-190`. It writes into `a.activated`
   directly, with **no tool result at all**.

### The Claude client

- `llm/anthropic/tools.go:21-44`: `convertTools` builds `BetaToolParam{Name, InputSchema, Description}`
  (line 37). It has no `DeferLoading`, and schema bytes are re-encoded with sorted keys.
- `llm/anthropic/request.go:53-56`: when `ToolsCacheControl == "ephemeral"`, it marks
  `tools[len(tools)-1]`, the last tool alphabetically. With deferred tools present, that tool is often
  deferred, which is a 400.
- `llm/anthropic/request.go:71-75`: adaptive thinking, the binding beta and `drop_block`.
- `llm/anthropic/request.go:80-82`: `tool_choice: none` keeps `tools`.
- `llm/anthropic/blocks.go:78-87`: a `RoleTool` message becomes `toolResult`.
- `llm/anthropic/blocks.go:194-204`: `toolResult` builds a text block, followed by the call's media. It has
  no reference blocks.
- `llm/anthropic/cost.go`, `logRequest`: counts `thinking_dropped` into `dropped_blocks`.

### Fields the client could read: none today

- `llm/client.go:52-59`: `llm.ToolDef` holds only `Type` and `Function{Name, Description, Parameters}`. It
  has no deferred flag.
- `llm/client.go:29-36`: `llm.Message` has nothing that names the tools a result loaded.
- `llm/client.go:132-188`: `llm.Request` carries `ToolsCacheControl`, `StablePrefixMessages` and
  `LoadedHistoryMessages`, all anthropic-only, and no deferred or loaded set.

The client therefore cannot learn either set without a new field.

### Other routes

Each route maps the `ToolDef` fields explicitly:
- `llm/openai_compat/request.go:272-287` (`toSDKTools`);
- `llm/chatgpt/request.go:87-100`.

A new `json:"-"` field is invisible to both, and to a command hook's stdin. `prompt/cache_anthropic.go:16-23`
is the existing anthropic-only seam in the builder.

### MCP deferral is frozen at mount

`agent/mcptools/bridge_deferral.go:71` (`grantLoadedSlot`) decides deferral when a server mounts.
`warnIfDeferralWouldFlip` (line 104) logs a reconnect that would flip it, and never applies it. So
`defer_loading` cannot flip mid-conversation through a reconnect. A mount or unmount still adds or removes
tools.

## 4. Proposed design (to measure first, then spec and plan)

1. **`llm.ToolDef` gains `Deferred bool` with `json:"-"`.** Other routes ignore it, and the hook stdin is
   unchanged. On the anthropic route the builder renders every registry tool, flagging the deferred ones,
   in place of `RenderToolDefs(activated)`. It decides this in the provider seam beside
   `injectCacheControl`. The array then depends only on the registry: it is byte-stable across rounds and
   turns, and `a.activated` stops shaping the Claude manifest. It still shapes the `<deferred_tools>`
   roster hint (`llm_agent_round.go:42`).
2. **`llm.Message` gains `ToolReferences []string` with `json:"-"`.** These are the deferred tools a tool
   result loaded.
   - On the anthropic route the builder fills it on a copy of the history. It never writes to `a.history`.
   - It is filled for every `RoleTool` message answering a `tool_search` call, using
     `loadedSchemas(content)` intersected with the registry's deferred tools: the same derivation
     `deriveEverLoaded` uses.
   - One function serves the round that produced the result and every later reload. The bytes then match
     across turns, which `Meta` cannot guarantee once a preview is truncated.
   - The dispatch gate's `tool_not_loaded` result needs the same treatment. Its text carries
     `RenderSpec`, so the derivation can read it once it is anchored to that refusal.
3. **`internal/llm/anthropic` changes in three places.**
   - `convertTools` sets `DeferLoading` from `ToolDef.Deferred`.
   - The tools breakpoint moves to the last **non-deferred** tool.
   - `toolResult` appends one `BetaToolReferenceBlockParam` per name in `ToolReferences` after the text,
     and drops any name absent from `req.Tools`. Otherwise an unmounted MCP tool makes every later
     request a 400.
4. **Stays the same.**
   - `tool_search` text and `Meta` stay as they are.
   - The dispatch gate's authority, `everLoaded`, stays.
   - openai_compat, chatgpt and llama.cpp see byte-identical requests, because `RenderToolDefs(activated)`
     still feeds them and the new fields are ignored.
5. **Open in the design.** Turn-recall `preloadTools` has no tool result. Its options:
   - (a) a `tool_addition` system message, which is a beta and new wire;
   - (b) a synthetic `tool_search` call and result pair, which the docs' "Write the tool call yourself"
     pattern allows;
   - (c) not preloading on this route.

## 5. Open questions only the probe can answer

1. Does a `tool_result` that mixes a text block and a `tool_reference` pass, as `mixed` and `spec` test?
   Or must it hold references only, as `refs` tests and as the docs example shows?
2. Does a client-side reference keep the prefix and the thinking? The expected result is `read` near the
   whole prompt and `dropped` at 0 on rounds 2 and 3. A server-side expansion that rewrote the prefix
   would show up here.
3. Do unreferenced deferred tools add input tokens? Compare round-1 `prompt` in `defer` with round-1
   `prompt` in `baseline`. They should differ by about the size of the two always-loaded tools at most,
   not by about 20 padding definitions.
4. Is the tools breakpoint accepted on a non-deferred tool that is not the last array element? Does it
   then read back as cached?
5. Does the reference load the tool across a turn boundary? Check both the verbatim history and Aura's
   reload shape with thinking stripped, and what the replay costs in each.
6. Can the model call a deferred tool nothing referenced (`unreferenced`)? If the API rejects the call or
   the model refuses, the dispatch gate becomes a backstop only.
7. Are the three contract shapes really 400s, with nothing billed?

## 6. Not yet proven

- **Nothing above is measured.** This probe is staged and not run. Every claim in §1 is the docs' claim.
- The probe uses one deferred tool, one search and one replay, on Sonnet 5.5 and not Opus 5.5, the route
  the VM runs. It does not show behaviour on long conversations.
- Not covered by the probe:
  - several references in one result;
  - a reference whose tool is later unmounted;
  - context editing that clears a referenced result;
  - Aura's history trimming (`agent/llm_agent_truncation.go`);
  - preview spillover of a large `select:`.
- It does not show the `<deferred_tools>` roster hint working on this route, or that the model still
  prefers `select:` once the schema stops arriving as text.
- Cost figures use list prices passed as flags, not the bill.
- Spec and plan follow only after the measurement, per PRD-first: measure, then amend, then implement.
