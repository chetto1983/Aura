# Phase-KV — Piano Revisionato 2026-05-27 (v2 post sub-agent validation)

**Status:** DRAFT v2 — incorporates findings from 3 parallel sub-agent reviews on 2026-05-27.
**Validation files:**
- `docs/phase-kv-plan-validation-codereview-2026-05-27.md` (RED → revisions applied below)
- `docs/phase-kv-plan-validation-adversarial-2026-05-27.md` (YELLOW → revisions applied below)
- `docs/phase-kv-plan-validation-crosssource-2026-05-27.md` (GREEN → 2 minor additions folded in)

**Supersedes:** `scripts/ralph/prd-phase-kv-staged.json` (5-story PRD from 2026-05-20).
**Owner:** Davide. **Draft author:** Claude.

**v2 changelog from v1:**
- Added Killer #6 (Context setters / Messages-fold mutate `messages[0]` per turn) — was the single largest miss.
- Sharpened Killer #1 (RFC3339 = second resolution; 2 call sites not 1).
- Split A02 into A02a/A02b (was already split in v1 but scope expanded substantially).
- A02a now covers 5 drift sources, not 3.
- A03 marker placement now by **explicit anchor flag**, not positional walk (HIGH bug fix).
- Nudge-as-trailing-user must NOT persist in `state.Messages()` (HIGH bug fix).
- New `SupportsMultipleSystemMessages` capability flag with fallback to trailing-user-pseudo-system (MEDIUM bug fix).
- Per-provider hit-ratio formula in A04 (MEDIUM bug fix).
- Sequencing: A03 depends on BOTH A02a AND A02b; rollback ordering explicit (MEDIUM bug fix).
- `enforceMessageCap` + siblings must skip ALL leading system messages (MEDIUM bug fix).
- Cost model for rolling tail anchor write fee (HIGH math fix).
- DeepSeek `prompt_cache_hit_tokens` fallback in A04 (cross-source).
- Multi-system pre-flight smoke in A01 (cross-source).

---

## 1. Cache killers — verified findings (2026-05-27)

Every finding cites file:line. Every citation independently verified by the code-review sub-agent.

### Killer #1 — `time.Now()` inside the cached prefix (CRITICAL)

- [internal/channels/telegram/invocation_builder.go:132](internal/channels/telegram/invocation_builder.go#L132): `agent.ComposeAgentPrompt(... time.Now())` on every inbound user message.
- [internal/channels/telegram/invocation_builder.go:396](internal/channels/telegram/invocation_builder.go#L396): **second** call site inside the overlay hot-reload path. A02a must patch BOTH sites; updating only line 132 leaves the regression on overlay-file-write.
- [internal/agent/promptplan.go:32](internal/agent/promptplan.go#L32): `content += "\n\n" + conversation.RenderTurnRuntimeCapsule(now, loc)` — capsule lands inside the prefix.
- [internal/conversation/system_prompt.go:216-233](internal/conversation/system_prompt.go#L216-L233): emits local time `2006-01-02 15:04` (minute resolution) **AND** UTC RFC3339 `2006-01-02T15:04:05Z` (**second resolution**). The cached prefix mutates **every second**.
- **Severity:** RED.

### Killer #2 — `InjectSystemExtras` mutates `messages[0].Content` per iteration (CRITICAL)

- [internal/conversation/system_prompt.go:323-346](internal/conversation/system_prompt.go#L323-L346): line 342 `out[0].Content += "\n\n" + extra`.
- [internal/agent/loop.go:179, 187, 192](internal/agent/loop.go#L171-L193): three call sites per iteration (Briefer / Already-done / StepHint).
- [internal/conversation/system_prompt.go:296-304](internal/conversation/system_prompt.go#L296-L304): `RenderStepHint` integer changes per iteration.
- **Severity:** RED. Independent from #1.

### Killer #3 — `pinnedOperational` depends on `MessageCount()` (HIGH)

- [internal/channels/telegram/invocation_builder.go:130](internal/channels/telegram/invocation_builder.go#L130): `pinnedOperational := ib.renderPinnedOperational(ctx, msg.ThreadID, convCtx.MessageCount())`.
- [internal/agent/promptplan.go:43-46](internal/agent/promptplan.go#L43-L46): appends to cached prefix.
- The `turnIdx` parameter plumbs through [internal/channels/telegram/invocation_builder_helpers.go:48-53](internal/channels/telegram/invocation_builder_helpers.go#L48-L53) into [internal/storage/memoryindex/priority_section.go:94-108](internal/storage/memoryindex/priority_section.go#L94-L108) `PrioritySectionCache.Render(ctx, store, turnIdx)`. The code-review sub-agent could not verify byte-level dependence without reading the cache implementation in full. **A01 must read `PrioritySectionCache.Render`**; if `turnIdx` is a cache-key only and does not affect bytes, drop the parameter as part of A02a. If it does affect bytes, move the whole block to VolatileContent.
- **Severity:** HIGH pending A01 resolution.

### Killer #4 — `tools[]` never sorted (YELLOW)

- [internal/llm/openai.go:424-436](internal/llm/openai.go#L424-L436): `convertToolDefinitions` iterates as-given. No `sort.Slice`.
- MCP reconciler ([memory `project_wave_2_10_b_tool_reconciler_shipped`](C:\Users\Davide\.claude\projects\d--Aura\memory\project_wave_2_10_b_tool_reconciler_shipped.md)) can reorder. This is the codex#2611 bug.
- **Severity:** YELLOW (time-bomb, not continuous bleed).

### Killer #5 — Single breakpoint, no rolling tail anchor (HIGH)

- [internal/llm/cache.go:96-106](internal/llm/cache.go#L96-L106): one marker on **last** system. After A02a splits static/volatile this walk lands on the volatile (BUG-1 fix below).
- Anthropic 20-block lookback (`docs/kv-cache-research/providers-state-2026-05-20.md` § Anthropic § eligibility).
- **Severity:** HIGH on Anthropic.

### Killer #6 — `Context` setters and `Messages()` folding mutate `messages[0]` (CRITICAL, missed in v1)

The single largest miss in v1. The plan's `{Static, Volatile}` split would be defeated by these even if Killers #1–#3 were all fixed.

- [internal/conversation/context.go:228-273](internal/conversation/context.go#L228-L273): `SetSystemMessage` / `SetAgentNote` / `SetSearchContext` all call `rebuildSystemMessage()`, which concatenates `baseSystemPrompt + agentNoteContent + searchContext` into `messages[0].Content` (line 269).
- [internal/conversation/context.go:293-307](internal/conversation/context.go#L293-L307): `Messages()` folds rolling `summary` into `messages[0].Content` on every call when present.
- [internal/channels/telegram/invocation_builder.go:140](internal/channels/telegram/invocation_builder.go#L140): `convCtx.SetAgentNote(noteContent)` when agent_note exists. Rebuilds [0].
- [internal/channels/telegram/invocation_builder.go:202-203](internal/channels/telegram/invocation_builder.go#L202-L203): `grounding := agent.BuildTurnGroundingCapsule(ctx, ib.memoryStore, userText)` is **per-turn user-derived content**, then `convCtx.SetSearchContext(grounding)` rebuilds [0].
- **Effect:** every turn the cached system [0] is overwritten with a different concatenation. Independent of Killers #1, #2, #3.
- **Severity:** RED.

### Non-killer — Overlay re-read per turn (GREEN, confirmed)

- [internal/channels/telegram/invocation_builder.go:92, 387](internal/channels/telegram/invocation_builder.go#L92): two read sites (initial + hot-reload), [overlay.go:78-99](internal/conversation/overlay.go#L78-L99) deterministic for unchanged files.

### Non-killer — `wikiTOC` only mentioned, not embedded (GREEN, confirmed)

- [internal/agent/promptplan.go:35-38](internal/agent/promptplan.go#L35-L38) emits fixed string; TOC bytes not in prompt.

### Observability gap — `Stats.CacheReadTokens` collected, never surfaced (MEDIUM)

- [internal/agent/loop.go:221](internal/agent/loop.go#L221): accumulates.
- [internal/llm/openai.go:306-311](internal/llm/openai.go#L306-L311): populates from `prompt_tokens_details.cached_tokens` AND `cache_read_input_tokens`.
- [internal/chat/agentloop.go:207](internal/chat/agentloop.go#L207): emitted in EventUsage payload.
- Not in `/api/health`, no dashboard, no per-run hit-ratio computation, no log line.
- **Severity:** MEDIUM. Pipe built; only last mile missing.

---

## 2. Goal — what "Phase-KV done" actually means

On a representative 8-turn Telegram conversation with ≥3 tool calls per turn against an Anthropic-class endpoint:

1. **Hit ratio ≥ 0.70 on turns 2–8**, measured per-provider as:
   - Anthropic: `cache_read_input_tokens / (cache_read_input_tokens + cache_creation_input_tokens + input_tokens)`
   - OpenAI: `prompt_tokens_details.cached_tokens / prompt_tokens`
   - Gemini: `cached_content_token_count / prompt_token_count`
   - DeepSeek: `prompt_cache_hit_tokens / prompt_tokens`
   - Threshold from `docs/kv-cache-research/providers-state-2026-05-20.md` § Consensus point 5.

2. **TTFT** on turns 2–8 drops by ≥ 30% versus turn 1 in the same conversation (Anthropic paper says 13–31%; we target upper bound because our prefix is large).

3. **`go test ./... -race` green**, including:
   - `TestRequest_StableAcrossRuns` — same state → byte-identical payload.
   - `TestContext_StaticPrefixByteStable_AcrossTurnsWithVolatileChurn` — static [0] unchanged even as agentNote / searchContext / summary mutate.
   - `TestInjectCacheControl_LandsOnStaticNotVolatile` — anchor flag, not positional walk.
   - `TestAppendTrailingNudge_StateUnchanged` — nudges don't leak into history.
   - `TestContext_VolatileSystemSurvivesMessageCap` — 50-msg eviction doesn't drop the second system.
   - `TestComposeAgentPrompt_FallbackToTrailingUserOnStrictProvider` — multi-system fallback.

4. **/api/health surfaces `kv_cache` block** with rolling 24h hit ratio.

5. **`cmd/probe_kv_cache -rolling`** exits 0 against a live endpoint; exits 0-skip on unsupported provider.

6. **Master is byte-faithful** — same conversation state → identical request bytes.

7. **Cost model documented** — per-turn cache-write fee for rolling anchor accounted in commit message.

---

## 3. Story plan (6 stories — A01, A02a, A02b, A03, A04, A05)

### A01 — Byte-faithfulness audit (PURE READ, no code change)

**Why first:** locks the baseline. Without a documented audit every later story risks "I thought we already fixed that".

**Scope (read-only):**

1. Confirm or refute each of Killers #1–#6 against current master. For each, `✓ GREEN / ⚠ YELLOW / ✗ RED` with file:line citations.
2. **NEW**: enumerate every call site that mutates `messages[0]` after `SetSystemMessage`. The known ones are `SetAgentNote`, `SetSearchContext`, `InjectSystemExtras`, plus `Messages()` folding rolling `summary`. Any unknown fourth MUST be flagged.
3. **NEW**: read [internal/storage/memoryindex/priority_section.go:94-108](internal/storage/memoryindex/priority_section.go#L94-L108) `PrioritySectionCache.Render` in full. Determine whether `turnIdx` affects rendered bytes or is a pure cache key. Verdict drives A02a Fix 2 scope.
4. **NEW**: enumerate `cfg.PromptVersion` injection site ([internal/agent/promptplan.go:33](internal/agent/promptplan.go#L33) `content += fmt.Sprintf("...Prompt Version: %s\n...", version)`). Document that this lives inside the static prefix today. Decision in A02a: leave it (operator-controlled, infrequent) OR move to volatile. Audit must STATE the verdict.
5. **NEW**: read [internal/conversation/archive.go](internal/conversation/archive.go) + [internal/conversation/archive_turns.go](internal/conversation/archive_turns.go) in full. Does the archive store `tool_calls` as raw JSON (good) or re-marshaled struct (cache-killing on cross-turn reload)? Audit verdict drives A02b scope (literal-bytes preservation in or out).
6. **NEW** (multi-system pre-flight smoke): identify Aura's currently configured LLM endpoint per `cfg.LLMBaseURL`. Document whether the endpoint accepts two adjacent `role=system` messages without 400. If unknown, list as "untested" — A02a will add the capability flag fallback. Survey the 5 most likely endpoints Aura's users point at: OpenAI direct, OpenRouter, Anthropic native (via Bedrock), Mistral via OpenAI-compat, vLLM, llama-server. For each, GREEN/YELLOW/RED on multi-system support. Cite source (provider docs URL or live curl smoke output if accessible).
7. **NEW**: verify `convertMessage` ([internal/llm/openai.go:438-465](internal/llm/openai.go#L438-L465)). `json.Marshal(tc.Arguments)` of `map[string]any` — Go 1.12+ sorts string-keyed maps. Cite Go spec or `encoding/json` package doc. Document as ✓ GREEN.
8. **NEW**: verify `parseToolCallArguments` ([internal/llm/openai.go:486-505](internal/llm/openai.go#L486-L505)) round-trip stability.
9. **NEW**: trace every `time.Now()` call in `internal/agent/`, `internal/conversation/`, `internal/channels/telegram/`. Confirm only the two `invocation_builder.go:132,396` sites feed into the cached prefix. (Code-review sub-agent already did this — A01 must independently confirm and cite.)
10. Determine **provider detection**: is there an explicit `LLM_PROVIDER` config field anywhere, or only baseURL/model heuristic ([internal/llm/cache.go:73-89](internal/llm/cache.go#L73-L89))? Plus does Aura's `cfg.LLMBaseURL` ever contain `openrouter.ai` — if yes, the upstream-model dependency (OpenRouter routes to many models with different caching semantics) must be addressed in A03.

**Output:** `docs/kv-cache-audit-2026-05-27.md` with:
- Per-killer subsection with `✗ RED / ⚠ YELLOW / ✓ GREEN` + file:line evidence + one-paragraph rationale.
- Section 7 = `messages[0]` mutator enumeration (all call sites).
- Section 8 = `PrioritySectionCache.Render` verdict.
- Section 9 = `cfg.PromptVersion` placement decision.
- Section 10 = `archive.go` literal-bytes verdict.
- Section 11 = Multi-system pre-flight smoke matrix.
- Section 12 = `FIX-FIRST list` — concrete code changes A02a needs.
- Section 13 = `Phase-KV proceed verdict` — explicit YES/NO with reasoning.

**Acceptance criteria:**
- File exists at `docs/kv-cache-audit-2026-05-27.md`.
- Every claim has file:line. Zero "grep yourself if you want to verify".
- All 6 Killer subsections present with ✗/⚠/✓.
- Sections 7–12 all present.
- `Phase-KV proceed verdict` YES/NO documented.
- `go build ./... && go vet ./...` green (trivial — no code).
- Single atomic commit `docs(kv-cache): byte-faithfulness audit (US-KV-A01)`.

**Out-of-scope:** zero code mutation in `internal/`. Fixes are A02a's job.

**Risks:** None. Pure read.

**Rollback:** revert commit. Doc disappears, no behavior change.

---

### A02a — Surgical de-cache-poisoning of the prefix (5 fixes)

**Why second:** A02b (canonical tools ordering) and A03 (markers) both depend on a stable prefix.

**Scope (write):**

#### Fix 1 — Move `RenderTurnRuntimeCapsule` out of the cached prefix

- Split `ComposeAgentPrompt` return into `{StaticContent, VolatileContent}`. `StaticContent` = base + clarification + overlay + skills + tool-surface notes + `cfg.PromptVersion` (per A01 verdict §9). `VolatileContent` = `RenderTurnRuntimeCapsule(now, loc)` + `pinnedOperational` (conditional on A01 verdict §3).
- Patch BOTH call sites: [invocation_builder.go:132](internal/channels/telegram/invocation_builder.go#L132) AND [invocation_builder.go:396](internal/channels/telegram/invocation_builder.go#L396) (hot-reload).
- Add `Context.SetVolatileSystemMessage(content)`. Lives at `messages[1]` when system [0] exists, else [0].
- `Context.rebuildSystemMessage` analog `rebuildVolatileSystemMessage` maintains the second system at the correct index.

#### Fix 2 — Migrate `agentNoteContent` + `searchContext` + folded `summary` out of `messages[0]` (NEW per Killer #6)

- `Context.SetAgentNote` and `SetSearchContext` today rewrite `messages[0]` via `rebuildSystemMessage`. After A02a:
  - `agentNoteContent` lives in the volatile second-system message (joined with `\n\n## Your current note (working memory)\n\n` prefix).
  - `searchContext` lives in the volatile second-system message (joined as today, but in volatile).
  - The rolling `summary` folding in `Messages()` (`internal/conversation/context.go:296-307`) must also write into VolatileContent, not into the static [0].
- The order in the volatile block: `RenderTurnRuntimeCapsule + pinnedOperational + agentNoteContent + searchContext + summary`. All five are turn-volatile.

#### Fix 3 — `InjectSystemExtras` no longer mutates `messages[0]`; nudges do NOT persist in `state.Messages()`

- Today: [system_prompt.go:323-346](internal/conversation/system_prompt.go#L323-L346) returns a fresh slice but appends to `messages[0].Content`. The loop builds `messagesForModel` (the per-turn copy) but the mutated [0] also leaks via `Messages()` calls elsewhere (need to verify in A01 §7).
- Target: rename to `AppendTrailingNudge(msgs []llm.Message, content string) []llm.Message`. Appends a **`role=user` message at the tail** of the returned slice. Does NOT mutate the caller's slice OR system [0]. Does NOT enter `state.Messages()` — only `messagesForModel`.
- All 3 call sites in [internal/agent/loop.go:179, 187, 192](internal/agent/loop.go#L171-L193) updated.
- **HARD INVARIANT:** `state.AddUserMessage` is never called by the nudge path. The nudges live in `messagesForModel` only and are gone next iteration.
- **HARD INVARIANT for A03's rolling anchor:** since nudges don't persist, the rolling anchor (A03 Fix 2 step 4) is always positioned on a **real** user message or tool_result. No marker on `RenderStepHint` text.
- For the **briefer capsule** authority concern: ship the capsule as the last nudge but prefix with `"## Runtime briefing (authoritative)"`. Acknowledge this is a hopeful mitigation (not evidenced). Add a probe test (A05) that fires a briefer-triggering fixture and asserts model behavior. If the test fails consistently, move the briefer capsule into the volatile second-system message in a follow-up (NOT this story).

#### Fix 4 — `enforceMessageCap` + siblings skip ALL leading system messages

- [internal/conversation/context.go:156-194](internal/conversation/context.go#L156-L194) `enforceMessageCap` today only skips `messages[0]` if it's system. Once A02a adds a volatile [1], the volatile counts against the 50-msg cap and gets evicted as oldest non-system once the conversation grows past 50 body messages.
- Generalize the skip: `for hasSystem := len(body) > 0 && body[0].Role == "system"; hasSystem; hasSystem = len(body) > 0 && body[0].Role == "system" { body = body[1:] }`.
- Apply the same generalization to `toolSafeBoundary` (lines 457-482), `truncateMessages` (lines 198-226), `trimOldest` (lines 437-455), `Summarize` (lines 369-435 — careful, this one's complex).
- Audit ALL message-list transforms in `internal/conversation/` and `internal/agent/governance/` for the `messages[0].Role == "system"` pattern. Update each to skip a leading run of system messages.
- Specifically also check [governance.dropOrphanToolResults](internal/agent/governance/governance.go), [governance.backfillMissingToolResults](internal/agent/governance/governance.go), [governance.Microcompact](internal/agent/governance/governance.go), [governance.ScrubOrphanToolCalls](internal/agent/governance/history_hygiene.go). These transform tool messages, not system; verify they tolerate two consecutive system at head.

#### Fix 5 — `SupportsMultipleSystemMessages` capability flag + trailing-user fallback

- Some providers reject two `role=system` messages (Mistral via OpenAI-compat 400s; vLLM/llama-server depend on chat template).
- Add **provisional** capability flag `SupportsMultipleSystemMessages bool`. Detection identical to A03's `SupportsCacheControl` but with its own matrix:
  - openai_direct: `true` (multi-system tolerated, only first treated as policy)
  - openrouter: `true` (upstream-dependent but generally yes for the openai-wire route)
  - anthropic_native (via Bedrock or OpenRouter Anthropic): **special case** — Anthropic native uses `system: [content_blocks]` not multiple `role=system`. A02a must emit the wire format Anthropic actually expects (array of blocks inside the single `system` parameter). This is a translator concern owned by A03's marker injection helper.
  - mistral: `false`
  - vllm: `false` (conservative; configurable via env)
  - llama_server: `false` (conservative; configurable via env)
  - default: `false`
- When `SupportsMultipleSystemMessages == false`:
  - `Context.SetVolatileSystemMessage(content)` emits the volatile content as a **trailing `role=user` message** with a marker prefix `"## Runtime context (auto-prepended each turn)"`.
  - Cost: the volatile content lives in the user-role lane, downgrading authority. Acceptable for runtime capsule (date/time), more concerning for pinnedOperational (operator policy). A01 §11 multi-system smoke must surface whether Aura's current configured provider supports multi-system — if YES, ship Option 1; if NO, ship Option 3 fallback.
- Detection at runtime: `cfg.LLMBaseURL` heuristic + optional explicit env `AURA_LLM_MULTISYSTEM=auto|force_on|force_off`. Default `auto`.

**Acceptance criteria:**

- `ComposeAgentPrompt` returns `AgentPromptPlan{StaticContent, VolatileContent, ...}`. `StaticContent` is byte-stable across two calls with identical inputs and any `now`/`loc`.
- `Context.SetVolatileSystemMessage(content)` exists; `Context.SetSystemMessage`, `SetAgentNote`, `SetSearchContext`, and `Messages()` summary-folding all populate VolatileContent, not the static [0].
- `InjectSystemExtras` renamed `AppendTrailingNudge`; all loop call sites updated; nudges do not enter `state.Messages()`.
- `enforceMessageCap` + 4 sibling transforms skip all leading system messages.
- `SupportsMultipleSystemMessages` capability flag added; trailing-user fallback implemented.
- New tests (all required):
  - `TestComposeAgentPrompt_StaticByteStableAcrossTime` — two calls with different `now` → identical `Static`.
  - `TestComposeAgentPrompt_FallbackToTrailingUserOnStrictProvider` — flag off → volatile is trailing user, not second system.
  - `TestContext_StaticPrefixByteStable_AcrossTurnsWithVolatileChurn` — sets static, then 3 turns of `SetSearchContext` + `SetVolatileSystemMessage` churn; static [0] bytes identical across all 3 turns; volatile [1] bytes change.
  - `TestAppendTrailingNudge_DoesNotMutateSystem` — system [0] bytes unchanged after 3 nudges.
  - `TestAppendTrailingNudge_StateUnchanged` — `state.Messages()` is unchanged after 3 nudges (only the returned `messagesForModel` slice has them).
  - `TestContext_VolatileSystemSurvivesMessageCap` — push 60 messages; both system messages remain at [0] and [1].
  - `TestEnforceMessageCap_SkipsAllLeadingSystem` — synthetic test with 3 leading system messages, asserts none evicted.
- All existing tests pass. Tests that asserted `messages[0]` content match the new shape (updated, not weakened).
- `go build ./... && go vet ./... && go test -race ./...` green.
- Single atomic commit `feat(kv-cache): de-cache-poison the system prefix (US-KV-A02a)`.

**Out-of-scope:**
- No `cache_control` markers — A03 owns them.
- No tools[] sorting — A02b owns it.
- No observability — A04.
- No probe binary — A05.
- No literal tool_call preservation — A02b conditional on A01.

**Risks (revised):**
| Risk | Mitigation |
|---|---|
| Multi-system rejected by Aura's current endpoint | Capability flag + trailing-user fallback; A01 §11 surveys the live endpoint first |
| Briefer capsule loses authority as trailing user | Marker prefix; A05 probe fires briefer-triggering fixture; follow-up moves to volatile-second-system if probe fails |
| Test churn from `messages[0]` assertions | Expected — update tests to new shape, never weaken |
| Hidden 4th `messages[0]` mutator | A01 §7 enumeration catches it pre-A02a |
| `Summarize` path complex; refactor risks regression | Add `TestSummarize_PreservesAllLeadingSystem` |

**Rollback (revised):** revert A02a commit ONLY IF no later Phase-KV story has shipped on top. If A02b or A03 have landed, revert them first in reverse order. Rolling back A02a after A03 leaves A03's anchor flag pointing into a layout that no longer exists → likely panics or silent miss.

---

### A02b — Canonical tools[] ordering + literal tool_call preservation

**Why third:** wire-time marshal stability. Independent layer from A02a, sequential for one-story-exit discipline.

**Scope (write):**

1. **Canonical tools[] ordering** in [internal/llm/openai.go convertToolDefinitions](internal/llm/openai.go#L424-L436). Add `sort.Slice(tools, func(i,j int) bool { return tools[i].Function.Name < tools[j].Function.Name })` after the conversion loop. Idempotent.
2. **Sort schema-like collections too**: also sort `properties{}` map keys at marshal time (Go already does this for `map[string]any`, but verify by adding `TestRequest_PropertiesByteStableAcrossMaps`). Audit also `enum` value arrays and `anyOf/oneOf` arrays — if any are not lexicographic, sort them. **Do NOT** sort `messages[]` (chronological order is semantic) or content blocks within a message.
3. **Literal tool_call preservation** — conditional on A01 §10 verdict:
   - If A01 verdict = `re-serializes` (RED), implement migration v25 + `tool_calls_raw BLOB` column + writer captures literal bytes + reader prefers raw. Add `TestArchive_ToolCallLiteralBytesRoundTrip`.
   - If A01 verdict = `byte-faithful` (GREEN), skip this fix. Document the verdict in the commit message.

**Acceptance criteria:**

- `convertToolDefinitions` sorts by function name lexicographically.
- New tests:
  - `TestRequest_ToolsSortedLexico` — 100 random shuffles → same JSON.
  - `TestRequest_StableAcrossRuns` — build same request twice → byte-identical.
  - `TestRequest_PropertiesByteStableAcrossMaps` — same `map[string]any` properties from different insertion orders → same JSON.
- If A01 verdict RED: migration v25 lands; archive writer captures literal bytes; reader prefers raw; `TestArchive_ToolCallLiteralBytesRoundTrip` passes.
- If A01 verdict GREEN: commit message cites the verdict; tests for literal-bytes preservation are optional.
- `go build ./... && go vet ./... && go test -race ./internal/llm/... ./internal/conversation/... ./internal/db/...` green.
- Existing probe_chat suite still passes.
- Single atomic commit `feat(kv-cache): canonical tools ordering + literal tool_call preservation (US-KV-A02b)`.

**Risks:** test churn from any test pinning specific `tools[]` order; surface in commit message.

**Rollback:** revertable IF A03 has not shipped on top.

---

### A03 — Per-provider capability flag + cache_control marker injection by anchor flag

**Why fourth:** wires markers onto a now-stable prefix.

**Scope (write):**

#### Fix 1 — Capability matrix

- New file [internal/llm/cache_capability.go](internal/llm/cache_capability.go) (~100 LOC).
- ```go
  type CacheCapability struct {
      ProviderID                  string
      SupportsCacheControl        bool
      SupportsMultipleSystemMessages bool  // also lives here for unified gating
      MarkerStyle                 string  // "anthropic_ephemeral" | "openai_auto" | "gemini_context_cache" | ""
      LookbackBlocks              int     // 20 for Anthropic, 0 (N/A) for OpenAI auto
      MaxBreakpoints              int     // 4 for Anthropic, 0 for OpenAI auto
  }
  ```
- Default registry:
  - `anthropic_native`: `{true, true, "anthropic_ephemeral", 20, 4}`
  - `openai_direct`: `{true, true, "openai_auto", 0, 0}`
  - `openrouter_anthropic`: `{true, true, "anthropic_ephemeral", 20, 4}` — upstream-routed to Anthropic
  - `openrouter_openai`: `{true, true, "openai_auto", 0, 0}`
  - `openrouter_other`: `{false, false, "", 0, 0}` — conservative
  - `deepseek`: `{true, false, "openai_auto", 0, 0}` — uses `prompt_cache_hit_tokens`, OpenAI-wire compatible (per [cross-source review](docs/phase-kv-plan-validation-crosssource-2026-05-27.md))
  - `gemini`: `{true, true, "gemini_context_cache", 0, 0}` — stub, marker injection deferred
  - `mistral`: `{false, false, "", 0, 0}`
  - `vllm`: `{false, false, "", 0, 0}` (env-overridable)
  - `llama_server`: `{false, false, "", 0, 0}` (env-overridable)
  - `default`: `{false, false, "", 0, 0}`
- Provider detection: prefer explicit env `LLM_PROVIDER` (operator-set); fall back to hostname heuristic on `cfg.LLMBaseURL`. OpenRouter requires sub-detection based on `cfg.LLMModel` (claude-* → openrouter_anthropic; gpt-* → openrouter_openai; else → openrouter_other).
- Replace `DetectCacheSupport` with `CapabilityFor(baseURL, model, envOverride) CacheCapability`.

#### Fix 2 — Marker injection by anchor flag (BUG-1 fix)

- Replace [internal/llm/cache.go injectCacheControl](internal/llm/cache.go#L96-L106) with `injectCacheControlAnthropic(msgs []chatMessage, tools []toolWrapper, cap CacheCapability) ([]chatMessage, []toolWrapper)`.
- Add `chatMessage.IsCacheableAnchor bool` (the message builder sets this on the **static** system message during A02a's split; volatile second-system does NOT get the flag).
- Add `toolWrapper.IsCacheableAnchor bool` (the marshal path sets it on `tools[-1]` and on the MCP-boundary tool).
- Place breakpoints in order:
  1. The `chatMessage.IsCacheableAnchor == true` system message (the **static** one, by flag not position).
  2. `tools[-1]`.
  3. MCP/builtin boundary in `tools[]` if discoverable: walk from tail; first non-`mcp_*` is the boundary. If no MCP tools present, skip this breakpoint.
  4. **Rolling tail anchor** on the last `role=user` OR `role=tool` message in `msgs`. Skip any message with `Ephemeral bool` flag (we won't actually have ephemera in `msgs` because A02a guarantees nudges live in `messagesForModel`, not state — but the skip logic is defensive).
- Cap at `MaxBreakpoints=4`. If MCP boundary absent, use 3 active breakpoints. NEVER exceed `cap.MaxBreakpoints`.
- The breakpoint encoding stays `CacheBreakpoint = true` triggering content-block-array MarshalJSON.

#### Fix 3 — `toolWrapper.MarshalJSON` for cache_control on tools

- Add `CacheBreakpoint bool` field to `toolWrapper` (json:"-").
- Add custom `MarshalJSON` on `toolWrapper`: when `CacheBreakpoint == true` AND wire format is anthropic_ephemeral, emit the Anthropic native tool shape with `cache_control: {"type": "ephemeral"}` field. On OpenAI-wire (OpenRouter passthrough), the same field rides through; OpenAI direct silently ignores unknown keys.
- **Wire-shape verification**: A03 acceptance criteria includes a test that captures the EXACT marshaled bytes for a fixture conversation against each `MarkerStyle` and pins them via `testdata/golden_*.json` files.

#### Fix 4 — OpenAI auto-cache eligibility

- For `MarkerStyle == "openai_auto"`, no markers injected; the request must satisfy OpenAI's eligibility (≥1024 tokens prefix, identical first-256-token routing key, byte-stable across calls). A02a + A02b guarantee stability. A03 documents this in code comment + commit message.

#### Fix 5 — Streaming SSE usage capture

- Per `docs/kv-cache-research/providers-state-2026-05-20.md` § Pitfalls: some providers only surface `cache_*_input_tokens` on the **final** SSE event. Aura's [openai.go streaming path](internal/llm/openai.go#L389-L422) already handles `streamOptionsJSON{IncludeUsage: true}` and reads usage from the final chunk ([openai.go:208-215](internal/llm/openai.go#L208-L215)). A03 must verify the cache fields are populated from the same final-chunk usage — add `TestStream_CacheUsageOnlyFromFinalChunk`.

#### Fix 6 — Config wiring

- New env `AURA_LLM_CACHE_ENABLED`: `auto` (default — capability-driven), `off` (force disable), `on` (force enable WITH WARNING log: "this may 400 on strict providers; nanobot test #167 is the citation").
- Surface in `internal/config/config.go`. Settings catalog dashboard exposure is A04.

#### Fix 7 — Gemini stub

- Capability declared. Marker injection deferred (Gemini requires explicit cache-create API call upstream of message send — story-sized refactor).
- Code comment documents.

#### Cost model (HIGH bug fix, BUG-3)

- A03 commit message must include a "Cache cost model" section breaking down per-turn:
  - Turn 1: prefix write (~N tokens × 1.25 for 5m TTL, or × 2 for 1h TTL) + uncached input (per-turn user msg + tool_results).
  - Turn 2..K: prefix read (~N tokens × 0.10) + rolling-anchor write (delta added since previous anchor × 1.25) + uncached input.
- Hit ratio measured per-provider per §2 goal #1.
- The §2 hit-ratio target ≥0.70 assumes:
  - Prefix is ≥10K tokens (Aura today ~5-25K; targets pass).
  - Rolling anchor delta is small (<5% of prefix per turn).
  - Conversation is short enough that 20-block lookback doesn't expire (≤4 turns Anthropic ephemeral default).
- For long conversations (>4 turns Anthropic 5m, >2 turns 1h TTL since the prefix anchor was written), expected hit rate drops. Document this explicitly so the §2 target isn't misread as "any conversation, any length".

**Acceptance criteria:**

- `internal/llm/cache_capability.go` exists with full registry, detection, and `CapabilityFor(baseURL, model, envOverride)`.
- `internal/llm/cache.go injectCacheControlAnthropic(msgs, tools, cap)` exists.
- Anchor placement by **`IsCacheableAnchor` flag**, NOT positional walking.
- New tests:
  - `TestRequest_AnthropicCacheMarkers_OnFourLocations` — fixture conversation with system [0] + system [1] (volatile) + 3 tools (1 mcp_*, 2 builtin) + 3 turns. Markers: static [0] (by flag), `tools[-1]`, mcp-boundary, last user/tool_result.
  - `TestInjectCacheControl_LandsOnStaticNotVolatile` — synthesizes two adjacent systems, static at [0] (`IsCacheableAnchor=true`), volatile at [1]. Asserts breakpoint is on [0], NOT [1].
  - `TestRequest_AnthropicCacheMarkers_NoMCP` — no mcp_* tools, 3 markers not 4.
  - `TestRequest_RollingTailMovesPerTurn` — 3-turn fixture, anchor index moves with conversation length.
  - `TestRequest_RollingTailSkipsEphemeralIfPresent` — defensive: inject an ephemeral-flagged message at tail, anchor lands on the last non-ephemeral.
  - `TestRequest_MistralNoCacheMarkers` — capability `SupportsCacheControl=false`, request has zero markers.
  - `TestRequest_OpenAIAutoCacheNoMarkers` — `MarkerStyle="openai_auto"`, zero markers, byte-stable across calls.
  - `TestCapabilityFor_EnvOverride` — `LLM_PROVIDER=mistral` overrides heuristic.
  - `TestCapabilityFor_OpenRouterUpstreamRouting` — `cfg.LLMBaseURL=openrouter` + `cfg.LLMModel="claude-*"` → `openrouter_anthropic`; same baseURL + `gpt-*` → `openrouter_openai`.
  - `TestStream_CacheUsageOnlyFromFinalChunk` — usage cache fields populated from final SSE event, not zero from mid-stream chunks.
  - `TestToolWrapper_MarshalJSON_AnthropicEphemeralShape` — pins the exact wire shape for the tool-side marker.
  - `testdata/golden_anthropic.json`, `testdata/golden_openai.json` — golden request payloads.
- `AURA_LLM_CACHE_ENABLED=off` → request payload byte-identical to pre-Phase-KV master (cross-check with A02b's `TestRequest_StableAcrossRuns`).
- `go build ./... && go vet ./... && go test -race ./internal/llm/...` green.
- Existing probe_chat suite still passes.
- Live smoke documented in commit message: against an Anthropic endpoint, two consecutive turns of same conversation produce non-zero `cache_read_input_tokens` in response.
- Cost model section in commit message.
- Single atomic commit `feat(kv-cache): cache_control markers + per-provider capability gate (US-KV-A03)`.

**Out-of-scope:** observability (A04), probe binary (A05), Gemini actual wiring.

**Rollback:** revertable IF A04 has not shipped on top.

---

### A04 — Cache-hit observability — per-provider hit-ratio + /api/health + dashboard panel

**Why fifth:** without a hit-rate signal, the optimization is invisible.

**Scope (write):**

#### Fix 1 — Normalized usage helper (per-provider, per BUG-7)

- New file [internal/llm/usage.go](internal/llm/usage.go) (~80 LOC).
- ```go
  type Usage struct {
      PromptTokens         int
      CompletionTokens     int
      CachedReadTokens     int  // cache hits, billed at discount
      CachedCreateTokens   int  // cache writes, billed at premium (Anthropic only; 0 elsewhere)
      ProviderID           string
  }

  func ParseUsage(providerID string, raw json.RawMessage) (Usage, error) {
      // Anthropic native + OpenRouter passthrough:
      //   usage.cache_read_input_tokens (top-level) + cache_creation_input_tokens (top-level) + input_tokens
      // OpenAI direct + DeepSeek (some routes):
      //   usage.prompt_tokens_details.cached_tokens (nested) + prompt_tokens
      // DeepSeek (other routes):
      //   usage.prompt_cache_hit_tokens (top-level) + prompt_tokens
      // Gemini:
      //   usage_metadata.cached_content_token_count + prompt_token_count
      // Unknown: just PromptTokens populated; CachedRead/Create stay 0.
  }

  func (u Usage) HitRatio() float64 {
      // Per-provider semantics:
      switch u.ProviderID {
      case "anthropic_native", "openrouter_anthropic":
          den := u.CachedReadTokens + u.CachedCreateTokens + u.PromptTokens
          if den == 0 { return 0 }
          return float64(u.CachedReadTokens) / float64(den)
      case "openai_direct", "openrouter_openai", "deepseek":
          if u.PromptTokens == 0 { return 0 }
          return float64(u.CachedReadTokens) / float64(u.PromptTokens)
      case "gemini":
          if u.PromptTokens == 0 { return 0 }
          return float64(u.CachedReadTokens) / float64(u.PromptTokens)
      default:
          return 0
      }
  }
  ```

#### Fix 2 — Pipe into runs-aggregates

- Find the Phase-TJ runs-aggregates table location (grep `tj_runs|tj_aggregates|runs_aggregates` in `internal/`). Extend with migration v26: `kv_cached_read_tokens INTEGER`, `kv_cached_create_tokens INTEGER`, `kv_hit_ratio REAL` (computed at write-time per provider formula), `kv_provider TEXT`. Legacy rows NULL → reader treats NULL as "unknown" not 0.

#### Fix 3 — /api/health extension

- [internal/api/health.go](internal/api/health.go) (or wherever /api/health lives) gains `kv_cache` JSON block:
  ```json
  {
    "kv_cache": {
      "enabled": true,
      "provider": "anthropic_native",
      "hit_ratio_24h": 0.74,
      "hit_ratio_7d": 0.71,
      "cached_read_tokens_24h": 1234567,
      "cached_create_tokens_24h": 12345,
      "sample_count": 234,
      "confidence": "high"  // "low" if sample_count < 10
    }
  }
  ```

#### Fix 4 — Dashboard panel

- New [web/src/components/KVCachePanel.tsx](web/src/components/KVCachePanel.tsx) (or extend existing observability panel — grep for Phase-TJ panel as template). Sparkline + 24h aggregates. Read-only. i18n keys `kv.cache.title`, `kv.cache.hit_ratio`, `kv.cache.confidence_low_warning` in `en.json` + `it.json`.

#### Tests

- `internal/llm/usage_test.go`:
  - `TestParseUsage_AnthropicShape` — cache_read + cache_creation + input.
  - `TestParseUsage_OpenAIShape` — nested cached_tokens.
  - `TestParseUsage_DeepSeekShape` — top-level prompt_cache_hit_tokens.
  - `TestParseUsage_GeminiShape` — usage_metadata.cached_content_token_count.
  - `TestParseUsage_UnknownProvider` — only PromptTokens populated.
  - `TestHitRatio_AnthropicFormula` — `CachedRead / (CachedRead + CachedCreate + Input)`.
  - `TestHitRatio_OpenAIFormula` — `CachedRead / PromptTokens`.
  - `TestHitRatio_ZeroDenominatorReturnsZero` — defensive.
- `internal/api/health_test.go`: validates JSON shape of `kv_cache` block.
- Frontend: `npm --prefix web run build && npm --prefix web run lint && npm --prefix web run i18n:check` green.

**Acceptance criteria:**

- `internal/llm/usage.go` exports `Usage`, `ParseUsage`, `Usage.HitRatio`.
- Migration v26 adds 4 columns. Existing rows NULL.
- Runs-aggregates writer populates from `ParseUsage` on every response.
- `/api/health` includes `kv_cache` block.
- Dashboard panel renders.
- All 8 unit tests pass.
- `go build ./... && go vet ./... && go test -race ./internal/llm/... ./internal/api/... ./internal/db/...` green.
- Frontend gates green.
- Single atomic commit `feat(kv-cache): observability — usage normalize + /api/health + dashboard panel (US-KV-A04)`.

**Out-of-scope:** probe binary (A05); CI gate (A05); LLM cost calculation (Phase-TJ owns).

**Rollback:** revertable IF A05 has not shipped on top.

---

### A05 — 2-call probe + 8-turn rolling probe + multi-system smoke + CI gate

**Why last:** validation layer; regression-proof Phase-KV's deliverables.

**Scope (write):**

#### Fix 1 — 2-call probe

- New [cmd/probe_kv_cache/main.go](cmd/probe_kv_cache/main.go) (~200 LOC).
- Mode 1 (default): two POSTs to Aura's chat endpoint with same long-prefix fixture. Query `/api/health.kv_cache.hit_ratio_24h` after call 2.
- Assert `hit_ratio >= 0.70` (`const MinHitRatio = 0.70`, pinned).

#### Fix 2 — 8-turn rolling-anchor mode

- Mode 2 (`-rolling`): 8 turns of agent loop with multiple tool_results per turn.
- Fixture accumulates ≥30 content blocks by turn 8 (deliberately stresses the 20-block lookback).
- Asserts hit ratio stays `>= MinHitRatio` across **all 8 turns**.
- Fixture exercises BOTH the briefer-capsule path (force a tool failure mid-turn) AND the already-done block accumulation (each turn re-calls a tool to trigger duplicate-guard).

#### Fix 3 — Static-byte-stability mode

- Mode 3 (`-static-byte-stability`): build same conversation state twice, assert byte-identical request payloads. Cross-check of A02a + A02b together.

#### Fix 4 — Multi-system smoke

- Mode 4 (`-multi-system-smoke`): send a single chat with 2 adjacent `role=system` messages to the configured endpoint. Assert HTTP 200 OR HTTP 400 with the body text explaining the failure. Used pre-A02a rollout to validate the endpoint supports multi-system, OR post-rollout to verify the fallback path was correctly selected.

#### Fix 5 — Briefer-authority smoke

- Mode 5 (`-briefer-authority`): fires a fixture that triggers a tool failure → briefer capsule. Asserts the model's next turn acknowledges the failure constraint (e.g. "I tried X, it failed because Y, switching to Z"). Threshold: 80% of 5 consecutive runs must show acknowledgment substring. If the threshold fails, opens a follow-up story to move briefer capsule into volatile-second-system.

#### Fix 6 — Provider-skip gate

- If `/api/health` reports provider not in `{anthropic_*, openai_*, openrouter_*, deepseek}` OR `AURA_LLM_CACHE_ENABLED=off`, the probe SKIPS (not FAILS) with logged reason. Same pattern as `cmd/probe_chat -tts` gate.

#### Fix 7 — CI invocation

- Extend `.github/workflows/ci.yml` (or wherever CI lives) with opt-in step `if env.AURA_RUN_KV_PROBE == 'true'`. Default OFF.

#### Fix 8 — Makefile target

- `kv-cache-probe` invokes the probe with default flags. Sub-targets `kv-cache-probe-rolling`, `kv-cache-probe-byte`, `kv-cache-probe-multisystem`, `kv-cache-probe-briefer`.

#### Fix 9 — Runbook

- `docs/kv-cache-runbook.md`: when/how/why each mode runs; interpretation of hit_ratio bands; what `<0.5` vs `0.5-0.7` vs `0.7-0.85` vs `0.85+` vs `>0.95` mean; what failure indicates per band.

#### Fix 10 — Golden-file regression

- Add `cmd/probe_kv_cache/testdata/golden_rolling_request.json` capturing the EXACT marshaled request payload for the rolling-anchor fixture. `internal/llm/client_test.go` gains `TestRequest_GoldenStable` that compares actual vs golden. `-update-golden` flag on probe regenerates with a warning print.

**Acceptance criteria:**

- `cmd/probe_kv_cache/main.go` implements 5 modes + provider-skip gate.
- Fixtures committed: `fixtures/long_prefix.json`, `fixtures/rolling_8turn.json`, `fixtures/briefer_trigger.json`.
- `MinHitRatio = 0.70` pinned in code.
- All 5 modes either PASS or SKIP cleanly.
- Makefile targets exist.
- CI YAML has opt-in step.
- `docs/kv-cache-runbook.md` exists with all band interpretations.
- `TestRequest_GoldenStable` exists; `-update-golden` flag works.
- Live smoke documented in commit: `go run ./cmd/probe_kv_cache -rolling` against live Anthropic produces `hit_ratio >= 0.70` across all 8 turns.
- `go build ./... && go vet ./... && go test -race ./...` green.
- Single atomic commit `feat(kv-cache): probe + CI gate + rolling-anchor regression fixture (US-KV-A05)`.

**Out-of-scope:** anything new (no marker placement, no observability fields). Pure validation layer.

**Rollback:** revertable any time.

---

## 4. Sequencing & dependencies (REVISED per BUG-8)

```
                  A01 (audit, read-only)
                       ↓
                  A02a (prefix de-poisoning)
                       ↓
                  A02b (wire-time stability)
                       ↓
                  A03 (markers — depends on BOTH A02a AND A02b)
                  /                                          \
                 ↓                                            ↓
              A04 (observability)                       (A03 depends on A02a's IsCacheableAnchor flag
                  ↓                                      AND A02b's stable tools[-1])
               A05 (probe + CI gate)
```

A03 depends on **both** A02a (the `IsCacheableAnchor` flag on the static system, and the volatile-system layout) AND A02b (the canonical tools[] ordering — A03 anchors on `tools[-1]`, which is only stable post-A02b).

**Rollback ordering (REVISED):**
- A05 → A04 → A03 → A02b → A02a → (no Phase-KV in master).
- Rolling back out-of-order is **forbidden** without first reverting later stories. Each story's commit message references its predecessor commit SHA so `git revert` ordering is obvious.

Estimated LOC delta:
- A01: ~600 LOC (markdown only)
- A02a: ~+350/-80 LOC (Go) + ~10-15 test updates
- A02b: ~+50/-5 LOC (Go) + new tests
- A03: ~+300 LOC (Go) + new tests + 2 golden files
- A04: ~+200 LOC (Go) + ~+100 LOC frontend + migration
- A05: ~+250 LOC (Go binary) + fixtures + Makefile + CI yaml + runbook

Total Phase-KV: ~+1400/-100 LOC + 1 frontend panel + 1 migration + 6 commits.

---

## 5. Test matrix (REVISED to cover BUGs 1-10)

| Test | Story | Type | What it proves | Maps to bug |
|---|---|---|---|---|
| TestComposeAgentPrompt_StaticByteStableAcrossTime | A02a | unit | `Static` identical across `now=t1, t2` | Killer #1 fix |
| TestComposeAgentPrompt_FallbackToTrailingUserOnStrictProvider | A02a | unit | Flag off → volatile is trailing user | BUG-4 |
| TestContext_StaticPrefixByteStable_AcrossTurnsWithVolatileChurn | A02a | unit | Static [0] unchanged across 3 SetSearchContext turns | Killer #6, BUG-5 |
| TestAppendTrailingNudge_DoesNotMutateSystem | A02a | unit | System [0] bytes unchanged after 3 nudges | Killer #2 fix |
| TestAppendTrailingNudge_StateUnchanged | A02a | unit | `state.Messages()` unchanged after 3 nudges | BUG-2 |
| TestContext_VolatileSystemSurvivesMessageCap | A02a | unit | 60-msg push, both system messages remain | BUG-9 |
| TestEnforceMessageCap_SkipsAllLeadingSystem | A02a | unit | 3 leading system messages, none evicted | BUG-9 |
| TestSummarize_PreservesAllLeadingSystem | A02a | unit | Summarize doesn't drop volatile [1] | BUG-9 |
| TestRequest_ToolsSortedLexico | A02b | unit | 100 shuffles → same JSON | Killer #4 fix |
| TestRequest_StableAcrossRuns | A02b | unit | Same state → byte-identical | Killer #4 fix |
| TestRequest_PropertiesByteStableAcrossMaps | A02b | unit | map[string]any → same JSON regardless of insertion order | Killer #4 fix |
| TestArchive_ToolCallLiteralBytesRoundTrip | A02b (conditional) | unit | Bytes in → bytes out | A01 §10 |
| TestRequest_AnthropicCacheMarkers_OnFourLocations | A03 | unit | 4 markers at correct sites | Killer #5 fix |
| TestInjectCacheControl_LandsOnStaticNotVolatile | A03 | unit | Anchor flag, not positional | BUG-1 |
| TestRequest_AnthropicCacheMarkers_NoMCP | A03 | unit | 3 markers when no mcp_* | Killer #5 fix |
| TestRequest_RollingTailMovesPerTurn | A03 | unit | Anchor moves with conversation | Killer #5 fix |
| TestRequest_RollingTailSkipsEphemeralIfPresent | A03 | unit | Defensive: skip ephemeral-flagged | BUG-2 |
| TestRequest_MistralNoCacheMarkers | A03 | unit | Strict provider → zero markers | BUG-4 |
| TestRequest_OpenAIAutoCacheNoMarkers | A03 | unit | Auto-cache → zero markers, byte-stable | – |
| TestCapabilityFor_EnvOverride | A03 | unit | `LLM_PROVIDER` env wins | – |
| TestCapabilityFor_OpenRouterUpstreamRouting | A03 | unit | OpenRouter + claude-* → anthropic; + gpt-* → openai | OQ-4 |
| TestStream_CacheUsageOnlyFromFinalChunk | A03 | unit | Streaming captures final-event usage | OQ-3 |
| TestToolWrapper_MarshalJSON_AnthropicEphemeralShape | A03 | unit | Pins wire shape | OQ-2 |
| TestParseUsage_* (5 variants) | A04 | unit | Per-provider usage parsing | BUG-7 |
| TestHitRatio_* (3 variants) | A04 | unit | Per-provider hit-ratio formula | BUG-7 |
| /api/health kv_cache shape | A04 | api | JSON shape stable | – |
| Dashboard panel renders | A04 | frontend smoke | i18n + chart present | – |
| probe_kv_cache 2-call | A05 | live | hit_ratio ≥ 0.70 on call 2 | §2 goal #1 |
| probe_kv_cache 8-turn rolling | A05 | live | hit_ratio ≥ 0.70 on turns 2-8 | §2 goal #1 |
| probe_kv_cache -static-byte-stability | A05 | live | Same state → same bytes | – |
| probe_kv_cache -multi-system-smoke | A05 | live | Endpoint accepts or 400s gracefully | BUG-4 |
| probe_kv_cache -briefer-authority | A05 | live | Model acknowledges briefer constraint | BUG-6 |
| probe_kv_cache skip-on-unsupported | A05 | live | Exit 0 skip on Mistral | – |
| TestRequest_GoldenStable | A05 | unit | Wire payload matches golden | – |

---

## 6. Risks & mitigations (REVISED)

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| Aura's current endpoint doesn't support 2 system messages | MEDIUM | HIGH (A02a blocks) | A01 §11 multi-system smoke pre-flight; A02a Fix 5 fallback to trailing-user-pseudo-system |
| Briefer capsule loses authority as trailing user | MEDIUM | MEDIUM | Marker prefix `## Runtime briefing (authoritative)`; A05 briefer-authority smoke probe; follow-up moves to volatile if probe fails |
| Test-suite churn from messages[0] assertions | HIGH | LOW | Expected — update tests to new shape, never weaken |
| Hidden 4th `messages[0]` mutator | LOW | HIGH | A01 §7 enumeration catches it pre-A02a |
| 50-msg sliding window evicts volatile [1] | KNOWN (A02a Fix 4) | MEDIUM | Generalize all 4 transforms to skip leading-system run; TestContext_VolatileSystemSurvivesMessageCap |
| Rolling tail anchor charges write fee every turn | KNOWN | LOW (~3-5% overhead) | Documented in A03 cost model |
| `cfg.PromptVersion` busts cache on operator upgrade | KNOWN (acknowledged) | LOW | Operator-controlled, infrequent. A01 §9 decision: leave it in static (current) or move to volatile |
| Multi-system rejected mid-deploy when provider changes | MEDIUM | MEDIUM | `AURA_LLM_MULTISYSTEM=auto` re-evaluates on `cfg.LLMBaseURL` change; A05 multi-system smoke catches regression |
| Anthropic 20-block lookback expires on long conversations | KNOWN | MEDIUM | A05 rolling-anchor mode catches it; cost model documents the trade-off; recommendation in runbook: 1h TTL for prefix on burst workloads, 5m TTL for rolling anchor |
| Live smoke against Anthropic not reproducible in CI | MEDIUM | LOW | A05 opt-in gate `AURA_RUN_KV_PROBE=true`; default CI doesn't run; local smoke documented in A03 commit |

---

## 7. Out-of-scope for Phase-KV (REVISED)

- TOON / alternate serialization formats (discussed 2026-05-27, deferred).
- Gemini explicit context-cache wiring (separate follow-up phase).
- Cross-conversation cache sharing (requires conv-id routing + provider sticky session).
- Tool-result compression beyond existing `governance.Microcompact` (out of cache scope; lives in token-budget work).
- Multi-provider failover (orthogonal).
- LLM cost calculation (Phase-TJ owns; A04 only adds kv_* dimensions).
- Briefer-capsule → volatile-second-system migration (deferred to follow-up if A05 briefer-authority probe fails).

---

## 8. Open questions answered post-v1

| OQ | Resolution | Where in plan |
|---|---|---|
| OQ-1: `cfg.PromptVersion` injection site | Lives in [promptplan.go:33](internal/agent/promptplan.go#L33). A01 §9 decides static vs volatile | A01 §9, A02a Fix 1 |
| OQ-2: `toolWrapper.MarshalJSON` wire shape | Golden test in A03 fixtures pins it per `MarkerStyle` | A03 Fix 3, Test `TestToolWrapper_MarshalJSON_AnthropicEphemeralShape` |
| OQ-3: Streaming SSE usage cache fields | Verified in [openai.go:208-215](internal/llm/openai.go#L208-L215); A03 Fix 5 adds explicit test | A03 Fix 5, Test `TestStream_CacheUsageOnlyFromFinalChunk` |
| OQ-4: OpenRouter capability detection | Upstream-model dependent; A03 Fix 1 adds `openrouter_anthropic` vs `openrouter_openai` distinction by `cfg.LLMModel` | A03 Fix 1, Test `TestCapabilityFor_OpenRouterUpstreamRouting` |
| OQ-5: Archive byte-faithfulness scope | A01 §10 reads `archive.go`; A02b conditionally implements migration | A01 §10, A02b Fix 3 |

---

## 9. Validation gate

This document is DRAFT v2. **Validation completed for v1 on 2026-05-27** by 3 parallel sub-agents:

| Sub-agent | Verdict | Findings folded |
|---|---|---|
| Code reviewer (`gsd-code-reviewer`) | RED | Killer #6 added; line citation fixes (RFC3339 = second res, 2 call sites); A01 scope expansion |
| Adversarial reviewer (`general-purpose`) | YELLOW | 10 bugs (3 HIGH + 7 MEDIUM/LOW) all addressed in v2; see changelog at top |
| Cross-source reviewer (`Explore`) | GREEN | 2 minor adds: A01 multi-system smoke; A04 DeepSeek `prompt_cache_hit_tokens` fallback |

**Promotion gate:** v2 has not been re-validated. Before promoting to `scripts/ralph/prd-phase-kv-staged.json`, run a **light spot-check** with the same 3 sub-agents asking each:
- "Has your v1 finding been fully addressed in v2? Quote the section and verify."
- 200-300 word reply per agent.
- If all 3 return GREEN, promote.
- If any return YELLOW with new findings, fold them in (v3).

---

## 10. References

- `docs/kv-cache-research/providers-state-2026-05-20.md` — provider matrix, breakpoint rules, billing.
- `docs/kv-cache-research/local-implementations-2026-05-20.md` — nanobot/hermes/openhuman/codex patterns.
- `docs/phase-kv-plan-validation-codereview-2026-05-27.md` — v1 code review (RED → v2 incorporates).
- `docs/phase-kv-plan-validation-adversarial-2026-05-27.md` — v1 adversarial (YELLOW → v2 incorporates).
- `docs/phase-kv-plan-validation-crosssource-2026-05-27.md` — v1 cross-source (GREEN, 2 adds folded).
- `scripts/ralph/prd-phase-kv-staged.json` — original 5-story PRD (this revision supersedes).
- arXiv 2601.06007 — "Don't Break the Cache".
- [memory `feedback_master_direct_workflow`](C:\Users\Davide\.claude\projects\d--Aura\memory\feedback_master_direct_workflow.md).
- [memory `feedback_one_module_per_slice`](C:\Users\Davide\.claude\projects\d--Aura\memory\feedback_one_module_per_slice.md).
- [memory `feedback_thorough_subagent_prompts`](C:\Users\Davide\.claude\projects\d--Aura\memory\feedback_thorough_subagent_prompts.md).
- [memory `feedback_codex_more_precise_than_claude`](C:\Users\Davide\.claude\projects\d--Aura\memory\feedback_codex_more_precise_than_claude.md).
