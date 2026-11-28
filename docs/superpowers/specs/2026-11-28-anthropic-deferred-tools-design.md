# Claude route: deferred tools through Anthropic's documented deferred loading

- **Date:** 2026-11-28
- **Status:** Proposed, awaiting operator review. The operator approved the direction on 2026-11-28 ("si",
  ledger `.superpowers/sdd/2026-11-27-anthropic-provider/progress.md`, line "Operator OK").
- **Parent:** `docs/superpowers/specs/2026-11-27-anthropic-provider-design.md`. This spec **supersedes its §7.6**
  ("A cache miss after `tool_search` loads a tool is accepted and measured, not engineered around"). The plan's
  last task writes the pointer into the parent spec. This file does not edit it.
- **Measurement this rests on:** `docs/superpowers/verification/2026-11-28-anthropic-deferred-tools-probe.md`
  (probe `spikes/anthropic-provider/probe-defer/`, Sonnet 5.5, about $0.06). The docs contract and the SDK
  inventory, with quotes, are in that probe's `README.md` §1-§2. They are not repeated here.
- **Risk tier: medium.** One provider's request shape changes on the agent's hot path. Two `json:"-"` fields
  are added to provider-neutral `llm` types. Every other route must stay byte-identical.
- **Schema:** none. **Env vars:** none. **Settings:** none.
- **Code cited at** `e0b995e19`. A peer session is editing `internal/llm/anthropic/blocks.go`, `request.go`
  and `internal/llm/client.go` for the cross-turn cache miss. The line numbers here are HEAD's, not that
  work's.

## 1. Why

On the lab VM, the Claude route ran on Opus 5.5 (conversation 01a2223e, ledger "VM read-back 2"). Every request
right after a `tool_search` had:
- `cache_read 0`;
- `cache_write ~40k`;
- `dropped_blocks 1`.

Those requests cost $0.213 and $0.221, where their neighbours cost $0.04 to $0.07. Two tool turns cost $0.393
and $0.485.

The cause: `tool_search` promotes a deferred tool into `a.activated`, and the builder renders
`Tools: reg.RenderToolDefs(activated)` (`internal/agent/prompt/builder.go:172`). The promoted tool is inserted
into the alphabetical array (`internal/agent/tools/manifest.go:67-93`). Tools head the cached prefix, so the
whole prefix is rewritten, and the signed thinking bound to the old prefix is dropped (`drop_block`, counted in
`internal/llm/anthropic/cost.go:38-56`).

## 2. Goals, non-goals, anti-goals

**Goals**
- On the `anthropic` route, the `tools` array of a conversation never changes when the model loads a tool.
  The request after a `tool_search` reads the prefix from the cache and drops no thinking block.
- The model still gets the full schema of every tool it loads, and keeps it in later turns, including after a
  reload from the store.

**Non-goals**
- Anthropic's server-side tool search (`tool_search_tool_regex` / `bm25`). Aura's search stays client-side.
- Changing what `tool_search` returns on any other route, or its ranking.
- The cross-turn cache miss under investigation on 2026-11-28 (a separate fix, §9).

**Anti-goals (failures even if it works)**
- Any byte change in an OpenRouter, llama.cpp, Ollama or ChatGPT request, or in a command hook's stdin on those
  routes.
- A request the API refuses for a reason the client could see: a reference to a tool that is not deferred in
  `tools`, `cache_control` on a deferred tool, every tool deferred, or text and a reference in one
  `tool_result`.
- A deferred tool the gate treats as loaded while the model never saw its schema.

## 3. What is measured, and its verdict

Vocabulary from `Skill("spike-findings-Aura")`: VALIDATED, PARTIAL, INVALIDATED. Every row below was measured on
**Sonnet 5.5** only. On **Opus 5.5**, the VM's route, every row is **unmeasured**, so for this route each row is
PARTIAL until the plan's Task 1 repeats it.

| # | Finding (Sonnet 5.5, 2026-11-28) | Verdict (Sonnet) |
|---|---|---|
| F1 | A `tool_result` holding **only** `tool_reference` blocks works. With thinking and effort max: round 2 read 2,774 (round 1's whole write), wrote 323, dropped 0, and called `stock_quote` with all three arguments that cannot be guessed. Round 3 read 3,097 and dropped 0. The `tools` digest was identical on every round. | VALIDATED |
| F2 | The reference survives the turn boundary: turn 2 verbatim read 3,244. In the reload shape, with thinking stripped, it read 2,774. Both called the tool again correctly with no new search. | VALIDATED |
| F3 | Text and a reference in one `tool_result` is a 400: "Tool definitions/code execution functions cannot be mixed with other content". This held both for a short text and for Aura's `RenderSpec` text. | VALIDATED |
| F4 | `cache_control` on a deferred tool is a 400. A breakpoint on the last **non-deferred** tool, mid-array, is accepted and read back (F1's round 2). | VALIDATED |
| F5 | Every tool deferred is a 400: "At least one tool must have defer_loading=false". | VALIDATED |
| F6 | A reference to a tool absent from `tools` is a 400. | VALIDATED |
| F7 | Unreferenced deferred tools are nearly free. Round-1 prompts were 2,690 and 2,685 with 2 tools, against 2,777 and 2,778 with the same 2 tools plus 21 deferred ones. The 21 include 20 padding tools of about 12 repeated sentences each. The difference is about +90 tokens, or about 4 tokens a tool. The system prompts differed only by the salt (`probe-defer/main.go:334`). | VALIDATED for these sizes |
| F8 | Asked to call an unreferenced deferred tool directly, the model called `tool_search` for it instead. | PARTIAL (n=1; a search is neither a call nor a 400) |

**What the measurement does not show.** None of this ran on Opus 5.5. Not measured:
- text **beside** a refs-only `tool_result` in the same user message. That is exactly where Aura's per-round
  hint merges (`internal/llm/anthropic/blocks.go:101-109`), and where a parallel call's `tool_result` goes;
- several references in one result, or the same tool referenced twice;
- a history whose assistant `tool_use` names a deferred tool that no message references;
- adding or removing an unreferenced deferred tool between rounds (an MCP mount);
- long conversations, compaction, and unmounting a referenced tool.

The thinking drop on a growing array rests on the VM log. The probe's baseline with thinking hit a transport
timeout. Costs are list-price arithmetic from flags, not the bill. Section 6 turns the unmeasured shapes that
block the design into stop conditions.

## 4. Approaches

- **A. Anthropic's documented deferred loading (picked).** Send every tool on every request, the deferred ones
  with `defer_loading: true`. Answer `tool_search` with `tool_reference` blocks. GA, no beta header
  (README §1). F1 and F2 show it keeps the prefix and the thinking.
- **B. Accept the miss (the parent spec's §7.6, superseded).** Measured at about $0.16 extra per `tool_search`
  on Opus 5.5, plus one dropped thinking block each time, a quality loss the happy path does not show.
- **C. Load every tool non-deferred on this route (rejected).** The array is stable, but at the ~368 tokens an
  MCP definition averages (`internal/agent/llm_agent_promote.go:28-33`), the whole catalogue costs tens of
  thousands of prompt tokens on every request.
- **D. `tool_addition` in mid-conversation system messages (not picked).** It needs a beta header
  (`inline-tools-2026-09-15` or `mid-conversation-tool-changes-2026-07-01`) and new wire work. Aura hoists every
  system message into `system` (`blocks.go:58-61`). It is unmeasured. It stays the documented fallback if Task 1
  invalidates A for some shape.

## 5. Design

### 5.1 The wire shape on the `anthropic` route

1. `tools` holds every registry tool, in today's alphabetical order. Each deferred tool carries
   `defer_loading: true` (`BetaToolParam.DeferLoading`). Non-deferred tools are byte-identical to today
   (`omitzero`).
2. The tools breakpoint goes on the **last non-deferred tool** in array order, not on `tools[len-1]`
   (`internal/llm/anthropic/request.go:53-56`) (F4).
3. A `tool_result` that loads tools holds **only** `BetaToolReferenceBlockParam` blocks, one per name, in the
   order derived, with duplicates removed (F1, F3).
4. That result's other text, its *residue* (§5.4), becomes a text block **after the last `tool_result` of the
   same wire user message**, in result order, ahead of any later text such as the hint. The docs put text after
   all of a message's tool results. This shape is unmeasured (§6, S1).
5. The client sends a reference only for a name that is deferred in this request's own `tools`. It drops any
   other name. If none is left, the message goes as today's text `tool_result`, carrying its residue, or
   `(no output)` when the residue is blank (F6).
6. A request in which every tool is deferred is refused by the client before dialling (F5). The registry
   already guarantees a non-deferred tool besides `tool_search` (`internal/agent/tools/spec.go:316-326`), and
   `tool_search` is never deferred (`internal/agent/tools/search.go:78`).

### 5.2 How the client learns the two sets

- `llm.ToolDef` gains `Deferred bool` with `json:"-"`. `llm.Message` gains `ToolReferences []string` with
  `json:"-"`. Both are set only by the prompt builder, and only when the provider is `anthropic`.
- **Why fields and not a client-side lookup.** The client cannot see the registry, and it cannot tell a
  `tool_search` result from any other tool result. `ToolCallID` pairs a result with its call, but the call's
  name lives on an earlier message, and the grant rule (§5.3) belongs to the agent, not to a wire package.
- **Why `json:"-"`.** No other client reads either field. Each route maps `ToolDef` field by field
  (`internal/llm/openai_compat/request.go:272-288`, `internal/llm/chatgpt/request.go:87-103`), and neither
  maps `Messages` wholesale. A command hook's stdin, the token preflight and every serialized copy of a request
  stay as they are. `ProviderState` and `ToolMedia` set the precedent (`internal/llm/client.go:29-36`,
  `:187`).

### 5.3 One derivation, live and on reload

- The builder rewrites a **copy** of the history. It never writes `a.history`. For each `RoleTool` message:
  - **A `tool_search` result.** Its `ToolCallID` belongs to a `tool_search` call on an earlier assistant
    message. Its names are `LoadedSchemas(content)` intersected with the registry's deferred tools. This is the
    anchoring and the parser `deriveEverLoaded` already uses (`llm_agent_promote.go:131-188`).
  - **A dispatch-gate refusal.** It answers a call to tool X, its content starts with the gate's fixed prefix
    `error: tool_not_loaded: ` (`internal/agent/llm_agent_tool.go:172-177`), and `LoadedSchemas(content)` is
    exactly `[X]`, with X deferred in the registry. Then the name is X. The anchor allows only the called tool's
    own name, so no tool can mint a reference to another one.
  - When at least one name survives, the copy gets `ToolReferences = names` and `Content = residue`. Otherwise
    the message is left untouched.
- **Why one pure function of (history, registry).** The same function runs in the round that produced the
  result and on every later build, so the bytes match across rounds and turns. Reading `Meta` would not: it
  is not persisted.
- **Live content equals reloaded content for both anchors.** The live history stores `run.Preview`
  (`internal/agent/llm_agent_dispatch.go:143`). The store keeps `run.Result.Preview`
  (`internal/agent/llm_agent_events.go:114`, `internal/runner/runner_persist.go:201-206`). For `tool_search`
  the two are equal, because it is a trusted tool (`internal/agent/trust.go:22-29`) and only the steer-marker
  scrub runs on it (`:41-46`). For a gate refusal, `Preview` is written once for both
  (`llm_agent_tool.go:172-179`).
- A lead for the cache-miss fix, outside this scope: an **untrusted** tool's live message holds the nonce-framed
  rendering, while its persisted turn holds the raw preview. So those bytes differ between live and reload.
- `loadedSchemas` moves from package `agent` to package `tools`, as `tools.LoadedSchemas`, beside `RenderSpec`
  (the format it parses), because `agent/prompt` cannot import `agent`. The one `tool_search` pairing loop is
  shared by the three derivations (§5.3, `deriveActivated`, `deriveEverLoaded`). Their behaviour does not
  change.
- `LoadedSchemas` requires each block's `Parameters:` body. When a large `select:` is truncated by the preview
  cap, a half-written tool is therefore neither granted nor referenced. On this route its partial text goes too
  (§5.4). The model sees the tools that arrived, and a later call to the missing one meets the gate (§5.6),
  which answers with its reference.

### 5.4 What the model sees instead of Aura's schema text

- The API expands each reference into the tool's own definition from `tools`: name, description and
  `input_schema`. That is the same definition a promoted tool carries today
  (`RenderToolDefs`: `Description`, else `Summary`, plus `Parameters`), as `convertTools` encodes it, including
  the flattening of a top-level combinator schema into the description (`internal/llm/anthropic/tools.go:46-78`).
  The docs: "the tool definitions that search loads into context count as input tokens like any other tool
  definition".
- The `## name / description / Parameters:` text (`RenderSpec`, `search.go:273-275`) no longer reaches the model
  on this route. The *residue* is the content before its first line beginning with `## `:
  - the unknown-name report, which `Execute` writes first (`search.go:186-189`);
  - the always-active report of a ranked query (`search.go:219-222`, `:258-265`);
  - the gate's refusal sentence.

  Everything from the first `## ` line on is dropped on this route. That covers the deferred specs, a
  non-deferred spec rendered by `select:` (`search.go:202-209`), which the model already has in `tools`, and a
  truncation notice. `alwaysLoadedReport` lines begin with `- ` and unknown-name lines with `"`, so none of them
  starts with `## `.
- The gate's sentence still says "Its schema follows". On this route the reference precedes it. The text stays
  byte-identical on every route. The wording is a known, accepted mismatch.

### 5.5 Turn-recall preload: none on this route

`preloadTools` (`internal/agent/llm_agent_turn_reading.go:179-191`) writes remembered tools into `a.activated`
with no tool result. On this route the catalogue is already in `tools`. The choice is between the three options
of README §4.5:

- **(a) a `tool_addition` system message.** Rejected. It needs a beta, new wire work, a position rule (right
  after a user turn), and it is unmeasured. Nothing is persisted, so a reload could not reproduce it byte for
  byte.
- **(b) a synthetic `tool_search` call and result.** Rejected. The runner rebuilds turns from events
  (`runner_persist.go:178-218`). A pair the model did not emit either becomes a persisted event and a transcript
  entry the user never saw, or it is not persisted, and then the next turn's history differs from this turn's
  (a cache miss from that message on).
- **(c) no preload on this route (picked).** It is the only option a reload reproduces byte for byte with
  nothing persisted. It also closes a hole: on this route a preloaded name would leave the `<deferred_tools>`
  roster (`internal/agent/llm_agent_round.go:42`) and pass the gate (`llm_agent_promote.go:228-238`) while the
  model never saw its schema.

  The cost is at most one `tool_search` round on a turn that recall would have preloaded. After this change that
  round is a cache-read round: F1's round 2 cost $0.0022 on Sonnet's 3k prefix. On this route `readTurn` already
  takes no adaptive effort (`llm_agent_turn_reading.go:68-69`), so with no preload the recall answers nothing, and
  the call is skipped. The read logs `recall_miss` as `deferred_loading`.

### 5.6 The dispatch gate

The gate (`llm_agent_tool.go:169-181`) stays as it is, on every route, with the same authority (`everLoaded`,
then `activated`). On this route its refusal reaches the model as a reference to the refused tool, with the
refusal sentence as residue (§5.3). F8 suggests the model searches rather than calling an unreferenced deferred
tool, so on this route the gate is a backstop.

A gate-allowed call to a deferred tool with no surviving reference leaves a `tool_use` naming a deferred tool that
no message references. Two cases produce one:
- `everLoaded` from a history that compaction has cut;
- a conversation that came from another route.

Whether the API accepts such a `tool_use` is unmeasured (§6, S3).

### 5.7 MCP tools mounted or unmounted mid-conversation

- MCP deferral is frozen at mount (`internal/agent/mcptools/bridge_deferral.go:71`). A reconnect that would flip
  it is only logged (`:104`). So `defer_loading` cannot flip inside a process.
- **A mount of deferred tools** adds entries to `tools`. The docs say an unreferenced deferred tool is outside
  both the cached prefix and the thinking prefix check (README §1). This is unmeasured (§6, S4). A mount that
  earns an always-loaded slot changes the non-deferred part of the prefix. That costs one full rewrite, as any
  mount does today, and is accepted.
- **An unmount** removes the tool from the registry and from `tools`. The derivation then names nothing for its
  old results, and the message falls back to its stored text (§5.3). The client's own filter (§5.1 item 5)
  covers a registry change between the two reads in one build. From that message on, the request rewrites the
  cache once and drops any thinking after it (counted). Unmounts are rare, and the alternative is a 400 on every
  later request.

### 5.8 Two consumers of `req.Tools` that see the whole catalogue

- **Token preflight** (`internal/conversations/request_budget.go:36-43`). It would count every deferred schema,
  the ~4 tokens of F7 at full size, and could refuse a request that fits. It therefore counts:
  - the non-deferred tools;
  - the deferred tools that some message's `ToolReferences` names, which the API expands.
- **Model-hook guard** (`internal/agent/hooks_model_guard.go`).
  - `cloneModelRequest` (`:14-43`) deep-copies `ToolReferences`.
  - `validateCommandModelHookMutation` (`:86-95`) clears `ToolReferences` and `ToolDef.Deferred` on both clones,
    as it clears `ProviderState`. A command hook's JSON round trip drops both fields, and the `DeepEqual` at
    `:63-66` would otherwise refuse every command hook on this route.
  - The in-process guard keeps protecting both fields.
- A command-hook rewrite is capped at 256 tools (`internal/agent/hooks_command.go:32`, `:374-376`). On this route
  a hook sees the whole catalogue. The acceptance run records the route's `tools_count`
  (`internal/agent/llm_agent_round.go:74`). A count above 256 is a stop and a question to the operator, not a
  silent raise.

### 5.9 Other routes stay byte-identical

- The builder calls `RenderToolDefs(activated)` exactly as today unless the provider is `anthropic`, and sets
  neither new field. A builder test on `openrouter`, `llamacpp`, `ollama` and `chatgpt` checks three things:
  `Tools` deep-equals `RenderToolDefs(activated)`, no `Deferred` is set, and every message deep-equals its
  history entry.
- Wire tests in `openai_compat` and `chatgpt` build one request twice, with and without `Deferred` and
  `ToolReferences` set, and assert equal body bytes. `json.Marshal` of an `llm.Request` with the fields set
  equals the marshal without them, which pins the command hook's stdin.
- The existing tests of those packages do not change.

### 5.10 Cost

- **Unreferenced deferred tools on every request:** about 4 tokens each (F7, Sonnet 5.5, list-price
  arithmetic). They sit inside the cached prefix, so they are written once and read after that.
- **Removed:** the per-`tool_search` rewrite. On the VM that was ~40k tokens written and one thinking block
  dropped, about $0.16 over its neighbours per search on Opus 5.5.
- **Unchanged:** a referenced tool's definition counts as input "like any other tool definition", about what its
  `RenderSpec` text costs today.

### 5.11 Logging

The per-request line `anthropic request` (`cost.go:38-56`) gains two fields:
- `deferred_tools`: the number of tools sent with `defer_loading`;
- `tool_references`: the number of reference blocks sent.

The request after a `tool_search` is then identifiable in the log. Never content, never names.

## 6. Still to measure before code (the plan's Task 1, Opus 5.5)

Each of S1 to S3 blocks a decision above. A 400 there stops the plan, and the result goes to the operator.

| # | Shape | Decides | On a 400 or a drop |
|---|---|---|---|
| S0 | F1 and F2 repeated on Opus 5.5, effort max, thinking on | the whole design on the VM's model | stop |
| S1 | `tool_result`(refs) followed by a text block in the same user message (the residue, then the hint) | §5.1 item 4, §5.4 | stop: residue and hint placement are redesigned |
| S2 | `tool_result`(refs), a parallel `tool_result`(text), then the residue and the hint; and two references plus a duplicate in one result | §5.1 items 3-4 | stop |
| S3 | a history whose assistant `tool_use` names a deferred tool no message references, then its `tool_result`, then a new round | §5.6, compaction | stop: the gate and compaction need a design change |
| S4 | an unreferenced deferred tool added to `tools`, then one removed, between rounds | §5.7 | no stop: record the cost and amend §5.7 |
| S5 | the stable breakpoint on a residue text block after a refs `tool_result`, read back next round | §5.1 item 4 | no stop: move the marker to the `tool_result` |

## 7. What done means

- Gate 2 green on every touched package. Coverage and mutation are as the repo rules require, with the new
  `internal/llm/anthropic` file added to the mutation scope.
- **Live on the lab VM, route `anthropic`, model `claude-opus-5-5`,** recorded in
  `docs/superpowers/verification/<date>-anthropic-deferred-tools-live.md` against the "VM read-back 2" baseline
  (read 0, write ~40k, dropped 1, $0.21):
  1. A turn that calls `tool_search` and then the loaded tool. On the `anthropic request` line right after the
     search (`tool_references` ≥ 1):
     - `cache_read_tokens` is at least 95% of the previous request's `cache_read_tokens + cache_write_tokens`;
     - `cache_write_tokens` is under 5,000;
     - `dropped_blocks` is 0;
     - the request costs under $0.08.
  2. In the same conversation, a second turn that calls the loaded tool again with no new `tool_search`. Its
     requests show `dropped_blocks` 0. Its first request's cache read is recorded beside the cross-turn fix's
     own expectation, which this spec does not set.
  3. A gate refusal is provoked, or recorded as not reproducible after three asks (F8). If provoked: the next
     request is accepted, and the call runs with correct arguments.
  4. `tools_count` and `deferred_tools` of the route are recorded. A `tools_count` above 256 → §5.8.
  5. The route switched to OpenRouter: one turn, with a request body the unit tests already pin (§5.9). The
     operator confirms on the UI.
- The PRD records the measurement and what it does not prove, citing this spec and the date.

## 8. Decisions

1. Approach A, client-side search, GA, no new beta header (§4).
2. The client learns the two sets from `ToolDef.Deferred` and `Message.ToolReferences`, both `json:"-"`, set by
   the builder on this route only (§5.2).
3. One pure derivation from (history, registry), anchored to `tool_search` calls and to gate refusals of the
   called tool, live and on reload (§5.3).
4. A loading result holds only references. Its residue goes after the message's last `tool_result`, pending S1
   and S2 (§5.1, §5.4).
5. No turn-recall preload on this route, and no recall call (§5.5).
6. The gate is unchanged. Its refusal is a reference on this route (§5.6).
7. A reference whose tool is gone is dropped. With none left, the message goes as text (§5.1, §5.7).
8. The preflight counts only what the API puts in context. The command-hook guard ignores the two fields (§5.8).

## 9. Open questions for the operator

1. **The gate across turns, on every route today.** `deriveEverLoaded` does not count a gate refusal on reload,
   so a tool the gate "loaded" is refused again in the next turn. Should it count? That would be a separate fix.
   It is outside this spec's scope.
2. **Preload.** Is one cached `tool_search` round accepted on the turns where recall would have preloaded (§5.5)?
   Option (b) would come back as its own spec only if the VM shows it matters.
3. **Paid runs.** Task 1 is about 15 calls on Opus 5.5, and Task 5 is a live turn set. Each needs the OK and a
   fresh key.
4. **Mounts.** A mid-conversation mount that earns an always-loaded slot still rewrites the prefix once (§5.7).
   Is that accepted?

## 10. Sources

- `spikes/anthropic-provider/probe-defer/README.md` §1, with the docs read on 2026-11-28: tool-search-tool,
  tool-use-with-prompt-caching, preserved-thinking/prefix-checks, change-tools-or-instructions, thinking,
  mid-conversation-system-messages.
- `docs/superpowers/verification/2026-11-28-anthropic-deferred-tools-probe.md`. Logs: the SDD workspace
  `probe-defer-run.log` and `probe-defer-run2.log`.
- SDK `github.com/anthropics/anthropic-sdk-go/v2` v2.15.0: `BetaToolParam.DeferLoading`,
  `BetaToolReferenceBlockParam`, `BetaToolResultBlockParamContentUnion.OfToolReference`.
