---
phase: phase-kv-plan-revised-2026-05-27
reviewed: 2026-05-27T00:00:00Z
depth: deep
files_reviewed: 9
files_reviewed_list:
  - docs/phase-kv-plan-revised-2026-05-27.md
  - internal/agent/promptplan.go
  - internal/agent/loop.go
  - internal/agent/exec_helpers.go
  - internal/channels/telegram/invocation_builder.go
  - internal/channels/telegram/invocation_builder_helpers.go
  - internal/conversation/system_prompt.go
  - internal/conversation/context.go
  - internal/conversation/overlay.go
  - internal/llm/openai.go
  - internal/llm/cache.go
  - internal/chat/agentloop.go
  - internal/storage/memoryindex/priority_section.go
findings:
  critical: 1
  warning: 4
  info: 3
  total: 8
status: issues_found
---

# Phase-KV Plan Validation — Code Review

## File:line verification

### Section 1 — Cache killers

**Killer #1 — `time.Now()` in cached prefix**

- VERIFIED `internal/channels/telegram/invocation_builder.go:132` — exact line:
  `promptPlan := agent.ComposeAgentPrompt(cfg, b.TimeLocation(), overlay, pinnedOperational, skillsBlock, toolManifest, wikiTOC, time.Now())`.
  Drift note: doc says "line 132" — matches.
- VERIFIED `internal/agent/promptplan.go:32` — exact line:
  `content += "\n\n" + conversation.RenderTurnRuntimeCapsule(now, loc)`.
- VERIFIED `internal/conversation/system_prompt.go:216-233` — `RenderTurnRuntimeCapsule` emits local `"2006-01-02 15:04"` (line 227) and UTC RFC3339 (line 168, `clock.utc = now.UTC().Format(time.RFC3339)`).
  AMBIGUOUS sub-point: the doc says "minute resolution" for local — correct (`15:04`). But UTC is RFC3339 which is **second** resolution (`2006-01-02T15:04:05Z`). The prefix therefore mutates **every second**, not "every minute" as the doc implies. Sharper wording: "local minute + UTC second resolution → prefix mutates every second in practice".

**Killer #2 — `InjectSystemExtras` mutates `messages[0].Content`**

- VERIFIED `internal/conversation/system_prompt.go:323-346` — `InjectSystemExtras` appends to `messages[0].Content` at line 342: `out[0].Content += "\n\n" + extra`. New system message prepended when no system [0] exists (lines 344-345).
- VERIFIED `internal/agent/loop.go:171-193` — three call sites:
  - Line 179: `messagesForModel = conversation.InjectSystemExtras(messagesForModel, capsule)` (Briefer)
  - Line 187: `messagesForModel = conversation.InjectSystemExtras(messagesForModel, block)` (RenderAlreadyDoneBlock)
  - Line 192: `messagesForModel = conversation.InjectSystemExtras(messagesForModel, hint)` (RenderStepHint)
- VERIFIED `internal/conversation/system_prompt.go:296-304` — `RenderStepHint` emits `"You are at step %d/%d..."` — integer changes per iteration. Confirmed.

**Killer #3 — `pinnedOperational` depends on `MessageCount()`**

- VERIFIED `internal/channels/telegram/invocation_builder.go:130` — exact line:
  `pinnedOperational := ib.renderPinnedOperational(ctx, msg.ThreadID, convCtx.MessageCount())`.
- VERIFIED `internal/agent/promptplan.go:43-46`:
  ```
  if strings.TrimSpace(pinnedOperational) != "" {
      content += "\n\n" + strings.TrimSpace(pinnedOperational)
      modules = append(modules, "pinned-operational")
  }
  ```
- AUDIT-A01 RESOLVED EARLY (saves an A01 task): `internal/channels/telegram/invocation_builder_helpers.go:48-53` and `internal/storage/memoryindex/priority_section.go:94-108`. The `turnIdx` argument is passed into `cache.Render(ctx, store, turnIdx)`. Without reading the cache implementation in full I cannot 100% confirm whether the rendered text varies byte-for-byte with `turnIdx`, but the parameter is plumbed through, which is itself a code smell: if it didn't matter, it wouldn't be a parameter. **A01 must read `PrioritySectionCache.Render` to settle this**, but the prudent default is to treat the dependency as real until proven otherwise.

**Killer #4 — tools[] never sorted**

- VERIFIED `internal/llm/openai.go:424-436` — `convertToolDefinitions` iterates the input slice as-given (`for _, def := range defs`). No `sort.Slice`. Confirmed.

**Killer #5 — Single breakpoint, no rolling tail anchor**

- VERIFIED `internal/llm/cache.go:96-106` — `injectCacheControl` places **one** marker on the **last** system message. Confirmed.

**Non-killer — Overlay re-read per turn**

- VERIFIED `internal/channels/telegram/invocation_builder.go:92` — `overlay := conversation.LoadPromptOverlay(cfg.PromptOverlayPath)`.
- VERIFIED `internal/channels/telegram/invocation_builder.go:387` — second `LoadPromptOverlay` call inside the hot-reload path (after tool-write).
- VERIFIED `internal/conversation/overlay.go:78-99` — reads files, concatenates `## <heading>\n\n<body>`. Deterministic for unchanged files. Confirmed GREEN.

**Non-killer — `wikiTOC` only mentioned, not embedded**

- VERIFIED `internal/agent/promptplan.go:35-38` — emits fixed string `"## Wiki Access\nThe wiki graph is available..."` when `wikiTOC != ""`. The TOC itself is NOT embedded. Confirmed GREEN.

**Observability gap — `Stats.CacheReadTokens`**

- VERIFIED `internal/agent/loop.go:221` — `stats.CacheReadTokens += resp.Response.Usage.CacheReadTokens`. Match.
- VERIFIED `internal/llm/openai.go:306-311` — populates from `prompt_tokens_details.cached_tokens` (line 307) and `cache_read_input_tokens` (line 309-310). Match.
- VERIFIED `internal/chat/agentloop.go:207` — emitted as `"cache_read_tokens"` in EventUsage payload. Match.

### Section 3 — Story scope citations

- A01 reference to `internal/conversation/archive.go` and `internal/conversation/archive_turns.go` — both files exist (verified via Glob). Plan content for A01 is task list, no specific line claims.
- A01 reference to `internal/llm/cache.go DetectCacheSupport` — VERIFIED at lines 73-89.
- A01 references `internal/llm/openai.go:438-465` for `convertMessage` — VERIFIED. Function spans 438-465 exactly. `json.Marshal(tc.Arguments)` is on line 450. Note: Go 1.12+ does sort string-keyed map keys in `encoding/json` (the doc cites this correctly).
- A01 references `internal/llm/openai.go:486-505` for `parseToolCallArguments` — VERIFIED. Function spans exactly 486-505.
- A02a Fix 1 references `promptplan.go:32` and `SetSystemMessage` — VERIFIED above.
- A02a Fix 3 references `system_prompt.go:323-346` and `loop.go:171-193` — VERIFIED above.
- A03 Fix 2 references `internal/llm/cache.go:96-106` — VERIFIED above.

### Section 4 — Acceptance criteria / test names

The plan invents new test names (e.g. `TestComposeAgentPrompt_StaticByteStableAcrossTime`, `TestAppendTrailingNudge_DoesNotMutateSystem`, `TestRequest_ToolsSortedLexico`) — these do NOT exist today (verified by grep returning zero hits). This is correct because A02a/A02b/A03 will introduce them. No drift to flag.

Existing tests cited indirectly (`TestSetSystemMessage`, `TestSetSearchContext`) — VERIFIED present in `internal/conversation/context_test.go` at lines 55, 76.

## Independent checks

### (a) Other `time.Now()` injections into the cached prefix

Beyond Killer #1 (line 132) and its hot-reload twin at line 396, I traced every `time.Now()` call in `internal/agent/`, `internal/conversation/`, `internal/channels/telegram/`.

- `internal/channels/telegram/invocation_builder.go:396` — second `time.Now()` call inside the overlay hot-reload path that ALSO rebuilds `promptPlan` mid-turn and feeds it through `convCtx.SetSystemMessage(newPromptPlan.Content)` (line 397). This is a **second injection vector for the same bug** and the document does not call it out separately. Same root cause, but the fix in A02a Fix 1 must explicitly cover this site as well, not only line 132. **NOT a new killer, but the doc undercounts injection sites (2, not 1).**
- All other `time.Now()` hits in `internal/agent/` are timing/elapsed-tracking (`exec_helpers.go`, `executor.go`, `loop.go:44/88/92`, `runtime.go:230`, etc.), which do not feed into the cached prefix.
- `internal/conversation/summarizer/applier.go:61,86,102` writes timestamps into wiki/summary documents — these go to disk, not into the cached prefix.

**No Killer #6 from `time.Now()` injection.** But see (b) below.

### (b) `SetSearchContext` / `SetAgentNote` mutate the cached prefix

**CRITICAL FINDING — Killer #6 the document missed.**

Read `internal/conversation/context.go:228-273`:

- `SetSystemMessage(content)` → sets `c.baseSystemPrompt`, then calls `rebuildSystemMessage()`.
- `SetAgentNote(content)` → sets `c.agentNoteContent`, then calls `rebuildSystemMessage()`.
- `SetSearchContext(content)` → sets `c.searchContext`, then calls `rebuildSystemMessage()`.
- `rebuildSystemMessage()` (line 251-273) builds `base + "\n\n## Your current note...\n\n" + agentNoteContent + "\n" + "\n\n" + searchContext` and OVERWRITES `c.messages[0].Content` (line 269: `c.messages[0].Content = content`).

Trace in `invocation_builder.go`:
- Line 133: `convCtx.SetSystemMessage(promptPlan.Content)` — set baseline.
- Line 140 (conditional, if agent_note exists): `convCtx.SetAgentNote(noteContent)` — REBUILDS messages[0].
- Line 203: `convCtx.SetSearchContext(grounding)` — REBUILDS messages[0].
- `grounding` comes from `agent.BuildTurnGroundingCapsule(ctx, ib.memoryStore, userText)` at line 202 — its content varies with the user query, so messages[0] mutates per turn.

**Effect:** Even if Killer #1 (RenderTurnRuntimeCapsule) is fixed, every turn calls `SetSearchContext(grounding)` with per-turn `userText`-derived content that is then **concatenated into the cached system [0]**. The cached prefix mutates per turn for this reason alone.

The `Messages()` method at lines 293-307 ALSO concatenates the rolling `summary` into `messages[0]` on every call when present, further mutating what the LLM sees vs. what is stored.

**Severity:** CRITICAL (RED). This is independent from Killers #1, #2, #3 and is the same root anti-pattern (mutating system [0] with per-turn dynamic content). A02a as currently written addresses RenderTurnRuntimeCapsule + pinnedOperational + InjectSystemExtras, but it does NOT call out:
- The `agentNoteContent` injection (mutates between turns when the agent updates its note),
- The `searchContext` injection (mutates EVERY turn — `grounding` is per-turn),
- The `summary` injection in `Messages()` (mutates whenever the rolling summarizer fires).

A02a's `{StaticContent, VolatileContent}` split must be extended to push `agentNote` + `searchContext` + folded `summary` into VolatileContent as well. Otherwise the static prefix still drifts.

### (c) `state.AddUserMessage` with formatted timestamps

Grep `AddUserMessage` in `internal/channels/telegram/`:
- Line 187: `convCtx.AddUserMessage("Answer to pending " + resume.Kind + " question: " + resume.Content)` — no timestamp.
- Line 200: `convCtx.AddUserMessage(userText)` — raw user text only.

No `time.Now()` or formatted timestamps leak into user messages. **No cleanliness flag for (c).**

## Additional killers found

- **Killer #6 — `SetAgentNote` / `SetSearchContext` / folded `summary` rebuild `messages[0]` with per-turn content** (CRITICAL, missed by the doc). See independent check (b). Must be folded into A01 audit scope and A02a fix scope.
- **Sub-finding under Killer #1** — `time.Now()` is called TWICE on the hot path (`invocation_builder.go:132` AND `:396`). The doc cites only one. A02a Fix 1 must update both call sites or the hot-reload path will reintroduce drift. (WARNING.)
- **Sub-finding under Killer #1 wording** — UTC is RFC3339 (second resolution), not minute. Doc undersells the bleed rate. (INFO.)

## Verdict

**RED.** Do NOT promote this plan to `prd.json` as-is.

Required fixes before promotion:

1. **Add Killer #6 to Section 1** with file:line evidence:
   - `internal/conversation/context.go:228-273` (the three setters + `rebuildSystemMessage`)
   - `internal/conversation/context.go:293-307` (the `Messages()` folding of `summary`)
   - `internal/channels/telegram/invocation_builder.go:140` (SetAgentNote call site)
   - `internal/channels/telegram/invocation_builder.go:202-203` (BuildTurnGroundingCapsule + SetSearchContext call site).
   Severity RED. This is the largest cache-poisoning vector currently in master; A02a is incomplete without it.

2. **Expand A02a scope** to introduce `Context.SetVolatileSystemMessage` and migrate `agentNoteContent`, `searchContext`, and the folded `summary` into the volatile second-system block (or a trailing user pseudo-system block), not the cached system [0]. The current A02a wording covers only RenderTurnRuntimeCapsule + pinnedOperational + InjectSystemExtras; it must explicitly enumerate all five drift sources.

3. **Patch Killer #1 wording** in Section 1: cite BOTH `invocation_builder.go:132` AND `:396` (hot-reload path). State "second resolution" not "minute resolution".

4. **A01 audit scope addition** — explicitly require A01 to read `internal/conversation/context.go` `rebuildSystemMessage` and document the search-context / agent-note / summary cross-turn drift. Without this the A01 baseline is wrong, and every downstream story inherits the gap.

5. **Recommended** — A01 should also read `internal/storage/memoryindex/priority_section.go` `PrioritySectionCache.Render` to determine whether `turnIdx` actually drives byte-level variance. The plan defers this to A01 (correct), but should add a one-sentence pre-commitment: "if `turnIdx` is used only as a cache key and does not affect rendered bytes, drop the parameter as part of A02a; if it does, move the whole block to VolatileContent."

Once items 1–4 are folded in, the plan goes from RED to YELLOW pending the A01 audit results (because Killer #3 and the priority-section dependency are still officially unresolved until A01 reads the code). After A01, GREEN is reachable.

The plan is fundamentally sound in its architectural approach (multi-system split + capability gating + multi-breakpoint + rolling anchor). The failure mode is **incomplete enumeration of the drift sources**, which is exactly the failure mode that would make Phase-KV ship a "fix" that still measures ~0% hit rate.

---

_Reviewed: 2026-05-27_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
