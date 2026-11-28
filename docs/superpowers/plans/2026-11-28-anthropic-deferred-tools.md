# Claude deferred tools without moving the cache prefix: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or
> superpowers:executing-plans, task by task, on `master`. Steps use checkbox syntax.

**Goal:** on the `anthropic` route, loading a deferred tool never changes the `tools` array. The request after a
`tool_search` reads the cached prefix and drops no signed thinking.

**Architecture:** the builder sends every registry tool on this route, with deferred ones flagged
(`llm.ToolDef.Deferred`). On a copy of the history, it marks each `tool_search` result and gate refusal with the
deferred tools it loaded (`llm.Message.ToolReferences`). The marks come from one pure function of history and
registry, so they are identical live and on reload. The Claude client turns the flags into `defer_loading: true`
and the marks into a `tool_result` that holds only `tool_reference` blocks. The result's remaining text goes after
the message's tool results. Both fields are `json:"-"`, and no other client reads them, so every other route's
wire and a command hook's stdin are unchanged. Turn-recall preload is off on this route.

**Tech Stack:** Go 1.27.4, `github.com/anthropics/anthropic-sdk-go/v2` v2.15.0 (Beta types already in use). No
migration, no env var, no setting.

**Spec:** `docs/superpowers/specs/2026-11-28-anthropic-deferred-tools-design.md`, proposed 2026-11-28. Task 1
measures what its §6 leaves open and amends it before any code. Code citations are at `e0b995e19`.

## Global Constraints

- **Dependency: this plan executes after the cross-turn cache-miss fix lands.** That fix is in progress on
  2026-11-28, uncommitted, in `internal/llm/anthropic/blocks.go` (`mark` / a new `markHistory`), `request.go`,
  `internal/llm/client.go` and `internal/agent/llm_agent_anthropic_route.go`, and it may touch
  `internal/agent/prompt`. Before Task 2:
  - re-read those files at the new HEAD;
  - re-check every file:line this plan and the spec cite;
  - correct the citations in the same commit as the first code that relies on them.
  Do not start while that work is still uncommitted in the tree.
- Everything new happens only when the provider is `llm.AnthropicProvider` (`"anthropic"`).
- OpenRouter, llama.cpp, Ollama and ChatGPT wire requests, and a command hook's stdin on those routes, stay
  byte-identical. Their packages' existing tests do not change.
- Wire rules (spec §5.1):
  - a loading `tool_result` holds only `tool_reference` blocks;
  - no `cache_control` on a deferred tool, and the tools breakpoint goes on the last non-deferred tool;
  - at least one non-deferred tool, else the client refuses before dialling;
  - `tool_search` is never deferred;
  - a reference is sent only for a name deferred in the same request's `tools`.
- No new beta header. The `thinking-binding-controls-2026-08-01` beta and `drop_block` stay as they are
  (`internal/llm/anthropic/request.go:71-75`).
- `llm.ToolDef.Deferred bool` and `llm.Message.ToolReferences []string` are both `json:"-"`. Only the prompt
  builder sets them, and only the Claude client reads them on the wire.
- The builder never writes to `a.history` or to the caller's slice. It rewrites a copy.
- The log line carries counts only, never tool names or content.
- ≤600 LOC a file, tests included. Near the cap:
  - `internal/agent/llm_agent.go` 567;
  - `spikes/anthropic-provider/probe-defer/main.go` 579;
  - `internal/agent/tools/search.go` 490.
  New code goes in new files there.
- Gates per task, in WSL (`export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"`):
  - `go vet ./...` and `go build ./...`;
  - `go test` and `go test -race` on each touched package;
  - `make quality` green at the commit, production `deadcode` included.
- Mutation ≥70% killed on the new `internal/llm/anthropic/references.go`, added to the scope in Task 5 as
  `6f7fe2262` did.
- Paid calls only in Task 1 and Task 5. Each needs the operator's OK in that session, on a key the operator
  creates and revokes after. The key is read from a scratchpad file deleted after the run, and never enters the
  repo, a log, a verification file or a commit.
- Commit by pathspec on `master`, and never stage a peer session's files. Never `--no-verify` or `--amend`. The
  repository is public.

## Review Focus

1. **A command hook on the anthropic route.** Its JSON round trip drops both fields. The guard must accept an
   unchanged rewrite, and the request sent must still carry the flags and the references. Pinned by
   `TestCommandHookRewriteKeepsReferencesAndDeferral` (Task 3).
2. **An MCP server unmounted after one of its tools was referenced.** There must be no 400: the reference is
   dropped, and the message falls back to text. Pinned by `TestUnregisteredToolLeavesTheResultUntouched`
   (Task 3) and `TestReferenceAbsentFromToolsIsDropped` (Task 2).
3. **`tool_search` in parallel with another call, then the hint.** The order must be the `tool_result`(refs),
   the other `tool_result`, the residue, then the hint. Pinned by `TestResidueFollowsTheLastToolResult`
   (Task 2).
4. **A large `select:` truncated by the preview cap.** Only fully rendered tools are referenced, and the
   half-written one is neither referenced nor granted. Pinned by `TestTruncatedSelectReferencesOnlyWholeSpecs`
   (Task 3).
5. **A small context window with a large catalogue.** The preflight must not count unreferenced deferred
   schemas, and must count referenced ones. Pinned by
   `TestPreflightCountsOnlyReferencedDeferredTools` (Task 3).

## Decisions taken while writing this plan

1. **Measure first.** Task 1 runs S0-S5 of spec §6 on Opus 5.5 before any code. Sonnet's F1-F8 alone are PARTIAL
   for this route (spec §3). A 400 on S0-S3 stops the plan and goes to the operator.
2. **Client before producer.** Task 2 teaches the client the two fields with no producer, so master keeps today's
   behaviour. Task 3 lets the builder produce them, together with the two consumers that would break the moment
   it does: the command-hook guard and the token preflight. No commit leaves a state where command hooks on this
   route are refused, or where the preflight counts the whole catalogue.
3. **The residue is computed by the builder, not the client.** `tools.ReferenceResidue` knows `RenderSpec`'s
   layout. The client only places text it is given. When no reference survives the client's filter, it sends
   that residue as the result text (or `(no output)`). The original schema text is not kept for that case: it
   describes a tool the request no longer has.
4. **Where the residue lands.** It goes after the last `tool_result` of its wire user message, in result order.
   The residue blocks are held while consecutive `RoleTool` messages merge and are flushed before the next
   non-tool block. The end of the group's last `RoleTool` message is then its last residue block, so a stable
   breakpoint there covers the residue (S5 decides whether it stays there).
5. **The gate refusal's prefix becomes a constant**, `tools.NotLoadedPrefix = "error: tool_not_loaded: "`. The
   gate's bytes do not change. The derivation anchors on it.
6. **`loadedSchemas` moves to package `tools`**, as `LoadedSchemas`, beside `RenderSpec`. The `tool_search` call
   pairing is extracted once as `tools.SearchCallIDs`. `deriveActivated`, `deriveEverLoaded` and the new
   derivation use them. Their existing tests pin unchanged behaviour (refactor on touch, no duplicated loop).
7. **No recall call on this route.** It has no adaptive effort (`llm_agent_turn_reading.go:68-69`), so without a
   preload the recall answers nothing. `recallMiss = "deferred_loading"`.
8. **Two new log fields**, `deferred_tools` and `tool_references`, on `anthropic request`. Acceptance uses them to
   find the request after a search.
9. **The 256-tool command-hook cap is not raised.** Task 5 records `tools_count`. A count above 256 is a question
   to the operator (spec §5.8).

## File structure

| File | Responsibility | Task |
|---|---|---|
| `spikes/anthropic-provider/probe-defer/shapes.go` (new) | the cases S1-S5 on the probe's existing wire helpers | 1 |
| `internal/llm/client.go` | `ToolDef.Deferred`, `Message.ToolReferences` (both `json:"-"`) | 2 |
| `internal/llm/anthropic/references.go` (new) | a result's reference-only content and its residue blocks, filtered against the request's deferred tools | 2 |
| `internal/llm/anthropic/tools.go`, `request.go`, `blocks.go`, `cost.go` | `defer_loading`, the breakpoint on the last non-deferred tool, residue placement, the two log counts | 2 |
| `internal/llm/{openai_compat,chatgpt}/request_deferred_test.go` (new) | the other wires ignore both fields | 2 |
| `internal/agent/tools/references.go` (new) | `LoadedSchemas` (moved), `SearchCallIDs`, `NotLoadedPrefix`, `ReferenceResidue`, `Registry.References` | 3 |
| `internal/agent/tools/manifest.go` | `RenderCatalog` beside `RenderToolDefs`, one shared body | 3 |
| `internal/agent/prompt/deferred_anthropic.go` (new) | `withReferences`: the history copy carrying references and residues | 3 |
| `internal/agent/prompt/builder.go` | `buildBase` picks catalogue + references on the anthropic route only | 3 |
| `internal/agent/llm_agent_promote.go`, `llm_agent_tool.go` | use the moved parser, the shared pairing and the prefix constant | 3 |
| `internal/agent/hooks_model_guard.go` | deep-copy `ToolReferences`; the command guard ignores both fields | 3 |
| `internal/conversations/request_budget.go` | count only the tools the API puts in context | 3 |
| `internal/agent/llm_agent_turn_reading.go` | no recall and no preload on the anthropic route | 4 |
| verification files, `prd.md`, parent spec §7.6, mutation scope files | records and gates | 1, 5 |

---

### Task 1: Measure the open shapes on Opus 5.5, then amend the spec

**Files:**
- Create: `spikes/anthropic-provider/probe-defer/shapes.go` (the new cases; `main.go` is at 579 lines)
- Modify:
  - `spikes/anthropic-provider/probe-defer/main.go`: only the `cases` table and the `-case` usage string;
  - `spikes/anthropic-provider/probe-defer/README.md`: the new cases and how to run them;
  - `docs/superpowers/specs/2026-11-28-anthropic-deferred-tools-design.md`: a new §0 amendment.
- Create: `docs/superpowers/verification/<date>-anthropic-deferred-tools-opus.md`

**Interfaces:** produces the verdicts that Tasks 2-4 rely on.

- [ ] **Step 1: Ask the operator** for the OK (about 15 billed calls on Opus 5.5) and a fresh key in a
  scratchpad file. Pass Opus 5.5's list rates as `-price-*`, read from `GET https://openrouter.ai/api/v1/models`
  (id `anthropic/claude-opus-5.5`); the defaults are Sonnet's.
- [ ] **Step 2: Write the cases** in `shapes.go`, reusing `send`, `show`, `body`, `deferredCatalogue`, `marked`
  and `answer`. Each runs with `-effort max` and `-display omitted`:
  - `s1-residue`: the `defer` loop with round 2's user message `[tool_result(refs), text(residue),
    text(hint)]`;
  - `s2-parallel`: a prompt that yields `tool_search` and `calc` in one response, answered as
    `[tool_result(refs: stock_quote, archive_00, stock_quote), tool_result(text), text(residue), text(hint)]`;
  - `s3-orphan-use`: a history whose assistant turn is a `tool_use` of `warehouse_03`, never referenced, then
    its `tool_result` text, then a new user question;
  - `s4-mount`: three rounds of `defer`, with one new deferred tool added to `tools` before round 2 and one
    unreferenced padding tool removed before round 3;
  - `s5-marker`: `s1-residue` with the stable breakpoint on the residue block, then on the `tool_result`, each
    read back on the next round.

  S0 is the existing `-case defer -result refs`, plus its replay, on Opus. Print what `show` prints, plus
  whether each round's arguments match the schema.
- [ ] **Step 3: Build gate:** `go vet ./spikes/anthropic-provider/probe-defer/...`, `golangci-lint run
  ./spikes/anthropic-provider/probe-defer/...` (0 issues), and each file under 600 LOC.
- [ ] **Step 4: Run** `go run ./spikes/anthropic-provider/probe-defer -keyfile <file> -model claude-opus-5-5
  -case <name> -effort max -result refs` for S0-S5. Delete the key file afterwards, and keep the log in the SDD
  workspace.
- [ ] **Step 5: Write the verification file.** For each case, record the raw line, the verdict (VALIDATED /
  PARTIAL / INVALIDATED), and what it does not show. No key, and no key prefix beyond `sk-ant-`.
- [ ] **Step 6: Amend the spec** with a §0 that records the verdicts and the sections each one changes. **Stop
  and report to the operator** if S0, S1, S2 or S3 returns a 400 or a dropped block. The design of §5.1 item 4,
  §5.4 or §5.6 then changes before Task 2. S4 and S5 amend §5.7 and §5.1 item 4 in place.
- [ ] **Step 7: Commit** `spike(anthropic): measure deferred-tool shapes on Opus 5.5`.

### Task 2: The Claude client sends deferred tools and references

**Files:**
- Modify:
  - `internal/llm/client.go`: `Message` :29-36, `ToolDef` :52-59;
  - `internal/llm/anthropic/tools.go:21-44`;
  - `internal/llm/anthropic/request.go:53-60`;
  - `internal/llm/anthropic/blocks.go`: `convert` :39-97, `toolResult` :194-204;
  - `internal/llm/anthropic/cost.go:38-56`.
- Create:
  - `internal/llm/anthropic/references.go` and `references_test.go`;
  - `internal/llm/openai_compat/request_deferred_test.go`;
  - `internal/llm/chatgpt/request_deferred_test.go`.

**Interfaces:**
- Produces:
  - `ToolDef.Deferred bool \`json:"-"\``;
  - `Message.ToolReferences []string \`json:"-"\``, documented as the deferred tools this tool result loaded,
    set only by the prompt builder on the anthropic route;
  - in `references.go`: `func resultContent(message llm.Message, deferred map[string]bool) (content
    []anthropic.BetaToolResultBlockParamContentUnion, residue []anthropic.BetaContentBlockParamUnion)`.
    `deferred` holds the request's deferred tool names, built once in `buildRequest` from `req.Tools`;
  - `builtRequest` gains `deferred, references int` for the log.

- [ ] **Step 1: Failing tests** (`httptest.NewTLSServer`, every body decoded, as `client_test.go`):
  - `TestDeferredToolsCarryDeferLoading`: two of four tools `Deferred` → exactly those two have
    `defer_loading: true`; the others carry no `defer_loading` key, and their bytes equal a run with no tool
    deferred.
  - `TestToolsBreakpointSkipsDeferredTools`: sorted tools `[a(d), b, c(d)]` with `ToolsCacheControl
    "ephemeral"` → `cache_control` only on `b`.
  - `TestEveryToolDeferredRefusesWithoutDialing`: an error, and zero hits.
  - `TestReferencesOnlyResult`: a `RoleTool` with `ToolReferences ["x","y","x"]` and `Content "Already
    active…"` → `tool_result.content` is `[tool_reference x, tool_reference y]`, and the residue text follows
    as a sibling block.
  - `TestResidueFollowsTheLastToolResult`: an assistant message with two calls, the first result with
    references and the second with text, then a user hint → one user message `[tool_result(refs),
    tool_result(text), text(residue), text(hint)]`.
  - `TestReferenceAbsentFromToolsIsDropped`: a reference to a tool not in `req.Tools`, and one to a
    non-deferred tool → neither is sent. If none is left, `tool_result.content` is `[text(residue)]`, and
    `(no output)` when the residue is blank.
  - `TestStableBreakpointCoversTheResidue`: `StablePrefixMessages` ending on that `RoleTool` → the marker is
    on the residue block, never inside `tool_result.content`.
  - `TestRequestLogCountsDeferredToolsAndReferences`: `deferred_tools=2`, `tool_references=2`, and no tool name
    in the line.
  - In `openai_compat` and `chatgpt`, `TestDeferredFieldsNeverReachTheWire`: the same request, with and
    without `Deferred` and `ToolReferences`, gives equal body bytes. `json.Marshal(llm.Request)` is equal both
    ways.
- [ ] **Step 2: Run, expect FAIL**: `go test ./internal/llm/anthropic/ ./internal/llm/openai_compat/
  ./internal/llm/chatgpt/`.
- [ ] **Step 3: Implement.** `convertTools` sets `DeferLoading: param.NewOpt(true)` only for a deferred tool.
  The breakpoint loop walks `tools` backwards to the first tool with `DeferLoading` unset. `convert` holds the
  residue blocks while `RoleTool` messages follow each other, appends them before the next non-tool block or at
  the end, and points `ends` of the group's last `RoleTool` at the last residue block. `toolResult` takes its
  content from `resultContent`.
- [ ] **Step 4: Run, expect PASS**, then `-race`, `make quality`, and each file under 600 LOC. `blocks_test.go`
  and `request_test.go` pass unchanged.
- [ ] **Step 5: Commit** `feat(llm): Claude sends deferred tools and tool references`.

### Task 3: The builder sends the catalogue and the references on the anthropic route

**Files:**
- Create:
  - `internal/agent/tools/references.go` and `references_test.go`;
  - `internal/agent/prompt/deferred_anthropic.go` and `deferred_anthropic_test.go`.
- Modify:
  - `internal/agent/tools/manifest.go:67-93` (`RenderToolDefs` and the new `RenderCatalog` share one body);
  - `internal/agent/prompt/builder.go:134-178`: `buildBase` takes `provider`, so the catalogue is rendered once;
  - `internal/agent/llm_agent_promote.go:51-188`: use `tools.LoadedSchemas` and `tools.SearchCallIDs`, and
    delete the local `loadedSchemas`;
  - `internal/agent/llm_agent_tool.go:172-177`: `tools.NotLoadedPrefix`, bytes unchanged;
  - `internal/agent/hooks_model_guard.go:14-43`, `:86-95`;
  - `internal/conversations/request_budget.go:36-43`.
- Test:
  - `internal/agent/hooks_model_guard_test.go` or its existing sibling;
  - `internal/conversations/request_budget_test.go`;
  - `internal/agent/prompt/builder_test.go` (`TestCacheControlSeam` stays unchanged).

**Interfaces:**
- Consumes: `ToolDef.Deferred` and `Message.ToolReferences` (Task 2).
- Produces:
  - `const tools.NotLoadedPrefix = "error: tool_not_loaded: "`;
  - `func tools.LoadedSchemas(content string) []string`, moved unchanged;
  - `func tools.SearchCallIDs(history []llm.Message) map[string]struct{}`;
  - `func tools.ReferenceResidue(content string) string`: the content before its first line beginning with
    `"## "`, with trailing whitespace trimmed;
  - `func (r *Registry) References(history []llm.Message) map[int][]string`: message index → deferred names,
    from `tool_search` results and gate refusals, per spec §5.3, deduplicated in order;
  - `func (r *Registry) RenderCatalog() []llm.ToolDef`: every tool, `Deferred` from its spec, with
    `RenderToolDefs`' description rule and sort;
  - in `prompt`: `func withReferences(history []llm.Message, reg *tools.Registry) []llm.Message`, which returns a
    copy whenever it changes anything.

- [ ] **Step 1: Failing tests:**
  - `TestRenderCatalogFlagsDeferredTools`: every tool present, alphabetical, `tool_search` not deferred, and
    promoted and unpromoted tools rendered alike.
  - `TestReferencesFromSearchResults`: a `select:` result naming two deferred tools and one non-deferred → the
    two deferred names. A RoleTool whose call is not `tool_search` but echoes `## send_file\n…\nParameters:\n{}`
    → nothing.
  - `TestReferencesFromGateRefusal`: a refusal of `x` built with `tools.NotLoadedPrefix` → `[x]`. The same text
    answering a call to `y` → nothing.
  - `TestTruncatedSelectReferencesOnlyWholeSpecs`: the second block cut before its `Parameters:` body → only the
    first name.
  - `TestReferenceResidue`: the unknown-name report plus specs → the report; the always-active report plus
    specs → the report; a refusal → its sentence; specs only → `""`.
  - `TestUnregisteredToolLeavesTheResultUntouched`: the referenced tool is unregistered → `ToolReferences` nil,
    and `Content` is byte-equal to the history entry.
  - `TestAnthropicBuildSendsTheCatalogueAndReferences`: three rounds with a search in round 1 → identical
    `Tools` on every round, the search result referenced from round 2 on, and the caller's history unchanged
    (deep-equal before and after).
  - `TestReloadedHistoryDerivesTheSameReferences`: the same messages rebuilt as `LoadHistory` returns them
    (`Role`, `Content`, `ToolCallID`, `ToolCalls`) → the same `ToolReferences` and `Content`, including a
    history from another route with OpenRouter-style ids.
  - `TestOtherRoutesBuildAsToday` (openrouter, llamacpp, ollama, chatgpt): `Tools` deep-equals
    `RenderToolDefs(activated)`, no `Deferred`, and `Messages` deep-equal to the history plus the hint.
  - `TestCommandHookRewriteKeepsReferencesAndDeferral`: an unchanged JSON round trip passes
    `validateCommandModelHookMutation`, and the live request keeps both fields.
    `TestInProcessHookCannotChangeReferences`: a mutation of either field → `protectedModelHookFieldError`.
  - `TestPreflightCountsOnlyReferencedDeferredTools`: the same request with 40 deferred tools unreferenced, and
    with one referenced → the estimates differ by about that one definition. With none deferred, the estimate
    equals today's.
  - The existing tests of `deriveActivated`, `deriveEverLoaded` and the gate pass unchanged.
- [ ] **Step 2: Run, expect FAIL**: `go test ./internal/agent/tools/ ./internal/agent/prompt/ ./internal/agent/
  ./internal/conversations/`.
- [ ] **Step 3: Implement.** On `llm.AnthropicProvider`, `buildBase` uses `reg.RenderCatalog()` and
  `withReferences(msgs, reg)`; every other provider runs today's code path. The preflight counts tools that are
  not `Deferred`, plus the `Deferred` ones named in any message's `ToolReferences`.
- [ ] **Step 4: Run, expect PASS**, then `-race`, `make quality` (no deadcode allowlist entry needed: Task 3
  wires every new symbol), and `go test ./internal/llm/... ./internal/runner/` unchanged.
- [ ] **Step 5: Commit** `feat(agent): load Claude's deferred tools by reference, keeping the tools array fixed`.

### Task 4: No turn-recall preload on the anthropic route

**Files:**
- Modify: `internal/agent/llm_agent_turn_reading.go:72-78` (`readTurn`)
- Test: `internal/agent/llm_agent_turn_reading_test.go`

**Interfaces:** consumes `llm.ReasoningTarget(...) == llm.ReasoningTargetAnthropic`, already used at `:68-69`.

- [ ] **Step 1: Failing test** `TestAnthropicRouteNeitherRecallsNorPreloads`: a recaller that would return a tool
  turn with `web_search` → zero recaller requests, `a.activated` empty, `read.preloaded` nil, `read.recallMiss ==
  "deferred_loading"`. The existing preload tests on other routes (`llm_agent_turn_reading_test.go:141`, `:345`,
  `:357`, `:402`, `:438`) pass unchanged.
- [ ] **Step 2: Run, expect FAIL**: `go test ./internal/agent/ -run 'TurnRead|Preload|Recall'`.
- [ ] **Step 3: Implement**: on that route, skip `recallTurns` and `preloadTools`, and set `read.recallMiss`.
  One comment names the reason (spec §5.5: no persisted shape a reload reproduces).
- [ ] **Step 4: Run, expect PASS**, then `-race` and `make quality`.
- [ ] **Step 5: Commit** `feat(agent): no turn-recall preload where Claude loads tools by reference`.

### Task 5: Live acceptance on the lab VM, Opus 5.5

**Files:**
- Create: `docs/superpowers/verification/<date>-anthropic-deferred-tools-live.md`
- Modify:
  - `prd.md`: in the LLM routes section, the measurement and what it does not prove, citing both specs and the
    date;
  - `docs/superpowers/specs/2026-11-27-anthropic-provider-design.md` §7.6: a one-line pointer to the new spec;
  - `.github/workflows/ci.yml`, `scripts/critical_mutation_gate.py`, `scripts/critical_mutation_gate_test.py`:
    `internal/llm/anthropic/references.go`.

- [ ] **Step 1: Add the mutation scope**, run `scripts/critical_mutation_gate_test.py`, and commit.
- [ ] **Step 2: Push** with the WSL recipe of CLAUDE.md. Watch every CI job go green on the pushed SHA, mutation
  and coverage included.
- [ ] **Step 3: Operator OK.** The VM takes master through the appliance's update path, never by a manual update.
- [ ] **Step 4: Run spec §7 items 1-5 through the cockpit.** Record the `anthropic request` lines (`cache_read_tokens`,
  `cache_write_tokens`, `dropped_blocks`, `deferred_tools`, `tool_references`, `cost_usd`) and `tools_count`
  for each request, and the operator's confirmation on the UI. Pass:
  - on the request after the search (`tool_references` ≥ 1):
    - read ≥ 95% of the previous request's read + write;
    - write < 5,000;
    - dropped 0;
    - cost < $0.08;
  - turn 2 calls the tool with no new search and drops 0;
  - a `tools_count` above 256 → stop and ask (spec §5.8).
- [ ] **Step 5: Write the verification file and the PRD amendment**, commit, push, and wait for CI green.
