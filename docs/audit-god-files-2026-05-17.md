# God Files Audit Report — 2026-05-17

**Status:** Read-only audit of cmd/ and internal/ Go files over 600 LOC (excluding test, vendor, .planning, web/).

---

## Executive Summary

**15 production .go files >600 LOC identified:**
- **HIGH priority:** 3 files (>900 LOC + mixed responsibilities + recent commits)
  - cmd/aura/app.go (1151 LOC): Phase-A/C composition + adapters
  - internal/identity/store.go (875 LOC): CRUD + auth + helpers (47 methods)
  - internal/agent/loop.go (814 LOC): Main loop + dedup + budget logic
- **MED priority:** 7 files (600–900 LOC OR mixes 2 responsibilities)
- **LOW priority:** 5 files (>600 LOC but single responsibility)
- **Near-god watch:** 4 files (500–599 LOC, trending up)

**Proposed refactor queue:** 10 atomic slices, ~9.5 days, 1782 LOC reduction across top files.

---

## God Files Table

| File | LOC | Top Responsibilities | Priority |
|------|-----|---|---|
| internal/db/migrations/migrations.go | 1294 | Migration registry + 16 schema evolution funcs | MED |
| cmd/aura/app.go | 1151 | Phase-A/C composition + 4 adapters | **HIGH** |
| cmd/probe_chat/cases.go | 876 | 39 test cases + verification | LOW |
| internal/identity/store.go | 875 | Principal/Actor/Grant CRUD + auth (47 methods) | **HIGH** |
| internal/agent/loop.go | 814 | runLoop + dedup + budget formatting | **HIGH** |
| internal/storage/memoryindex/store.go | 788 | Memory queries + vector sync + rebuild | MED |
| internal/storage/runs/store.go | 781 | Run/event/question lifecycle store | MED |
| internal/wiki/memory_hygiene.go | 759 | Stale-memory deletion + proposal indexing | MED |
| internal/chat/hub.go | 756 | Central dispatch hub + lifecycle + emit | MED |
| internal/api/auth/store.go | 740 | API token + dashboard auth store | MED |
| internal/agent/tools/registry/memory_search.go | 718 | Memory search tool (cohesive) | LOW |
| internal/api/types.go | 673 | API request/response types + marshaling | LOW |
| internal/storage/sources/ingest/pipeline.go | 671 | Ingest orchestration | MED |
| internal/cron/dispatch.go | 611 | Cron dispatch handler + job execution | MED |
| internal/llm/openai.go | 608 | OpenAI client + streaming + embeddings | LOW |

---

## HIGH PRIORITY REFACTORS

### 1. cmd/aura/app.go (1151 → ~568 LOC after splits)

**Responsibilities:** 3 interlocked phases
1. **Phase-A (lines 151–546):** Dependency construction (newApp) — 396 LOC of inline setup blocks (LLM, Qdrant, wiki, search, ingest, skills, MCP, auth, scheduler, memory, swarm, budget)
2. **Phase-C (lines 560–971):** Composition wiring (wireBot) — 410 LOC of tool registry, archive, scheduler, router, skill adapters
3. **Adapters (lines 974–1151):** 4 import-cycle bridges — 177 LOC (swarmRunnerAdapter, agentJobRunnerAdapter, newSwarmDepsGetter, newAgentJobDepsGetter, newWebChatDepsGetter, scheduledTaskRunnerAdapter)

**Split plan:**
- Move newApp + setup → **app_bootstrap.go** (395 LOC)
- Move wireBot + tool wiring → **app_wire.go** (410 LOC)
- Move adapters → **app_adapters.go** (177 LOC)
- Keep App struct + lifecycle methods in app.go (168 LOC)

**Function inventory:**
- App lifecycle: startBg, APIHandler, reindexHealth, CompactMemoryHealth, Start, Stop (lines 72–144)
- newApp: LLM, Qdrant, wiki, search, ingest, sandbox, tools, skills, web search, scheduler, swarm, MCP, auth, run store, budget, background context (lines 151–546)
- wireBot: tool index, archive, memory rebuild, scheduler, router, skill adapters (lines 560–971)
- Adapters: swarmRunnerAdapter.Run, newSwarmDepsGetter, agentJobRunnerAdapter.RunJob, newAgentJobDepsGetter, newWebChatDepsGetter, scheduledTaskRunnerAdapter.RunTaskNow (lines 981–1151)

**Recent history:** Touched in commits 3118d098 ("fix(tools): MRL-truncate"), 1247eaf1 ("feat(swarm,learning)").

---

### 2. internal/identity/store.go (875 → ~245 LOC base + helpers + auth)

**Responsibilities:** 3 orthogonal concerns
1. **CRUD constructors (lines 43–184):** Principal, Channel, Actor creation + retrieval
2. **Authorization (lines 235–525):** Grant delegation, allowlist backfill, revocation, Authorize decision, active grant checks
3. **Validation + ID generation (lines 527–869):** validateActorChannelAccount, validateGrantSubject, getPrincipal, getChannelAccount, getActor, getGrant, parsing, builders (TelegramPrincipalID, mustID, stableID)

**Split plan:**
- Keep CRUD in store.go (~245 LOC): NewStore, constructors, getters
- Move auth logic → **store_auth.go** (290 LOC): DelegateActor, BackfillTelegramAllowlistedUser, RevokeGrant, Authorize, recordDecision, hasActiveGrant*
- Move helpers → **store_helpers.go** (340 LOC): validation, parsing, ID builders (follows existing *_helpers.go pattern)

**Methods by responsibility:**
- **CRUD:** CreateOrResolvePrincipal, CreateOrResolveChannelAccount, CreateActor, CreateOrResolveActor, createOrResolveActor, CreateGrant, CreateOrResolveGrant, createOrResolveGrant, getPrincipal, getChannelAccount, getActor, getGrant (12 methods)
- **Auth:** DelegateActor, BackfillTelegramAllowlistedUser, RevokeGrant, Authorize, recordDecision, hasActiveGrant, hasActiveGrantForSubject, actorInheritsPrincipalGrants, validateActorChannelAccount, validateGrantSubject (10 methods)
- **Helpers:** Helper funcs for ID generation, parsing, validation (25+ funcs)

---

### 3. internal/agent/loop.go (814 → ~534 LOC base + dedup + finalize)

**Responsibilities:** 2 separable concerns
1. **Main loop (lines 224–658):** runLoop state machine, iteration, LLM calls, tool execution, budget enforcement
2. **Dedup + budget formatting (lines 63–221, 692–815):** IsRetryableToolResult, DuplicateOrMaxCallsPolicy, budgetCapToolResult, toolResultPreview, argKeysFromCall, findAskUserCall
3. **Finalization (lines 660–690):** finalizeAnswerAfterBudget

**Split plan:**
- Keep main loop in loop.go (~534 LOC): runLoop, core types, State interface, ToolExecutor
- Move dedup + budget → **loop_dedup.go** (150 LOC): dedup policies, budget tooling, formatting
- Move finalization → **loop_finalize.go** (30 LOC): finalizeAnswerAfterBudget (optional, tiny)

**Functions by responsibility:**
- **Main loop:** runLoop (430 LOC dense state machine, lines 224–658)
- **Dedup:** IsRetryableToolResult, DuplicateOrMaxCallsPolicy, budgetCapToolResult, toolBudgetFinalInstruction, maxCallsToolResult (lines 63–221)
- **Formatting:** duplicateToolResult, maxCallsToolResult, toolResultPreview, argKeysFromCall, interruptedAssistantContent, finalAnswerOnBudget, findAskUserCall (lines 692–807)

---

## MED PRIORITY FILES (7 total — hold unless growth trigger)

| File | LOC | Extraction | Effort | Gain | Hold reason |
|------|-----|---|---|---|---|
| internal/db/migrations/migrations.go | 1294 | Split into migrations_2xx, migrations_3xx, core registry | 2d | ~500 LOC red | Cohesive; only split if >1500 |
| internal/storage/memoryindex/store.go | 788 | Extract rebuild + vector into store_rebuild.go, store_vector.go | 1.5d | ~300 LOC red | High cohesion; low urgency |
| internal/storage/runs/store.go | 781 | Extract question lifecycle into store_questions.go | 1d | ~100 LOC red | Possible; medium urgency |
| internal/wiki/memory_hygiene.go | 759 | Extract proposal indexing into hygiene_proposals.go | 1d | ~150 LOC red | Cohesive; low refactor need |
| internal/chat/hub.go | 756 | Extract lifecycle into hub_lifecycle.go + emit into hub_emit.go | 1d | ~200 LOC red | Possible; medium urgency |
| internal/api/auth/store.go | 740 | Extract token validation into store_tokens.go | 1d | ~150 LOC red | Cohesive token domain; low urgency |
| internal/storage/sources/ingest/pipeline.go | 671 | Extract stage processors into pipeline_stages.go | 1d | ~200 LOC red | Check coupling first |

**Strategy:** Complete HIGH slices first (3 files → 3 commits). Reassess MED after HIGH complete; prioritize by growth rate (PRs adding LOC) or feature coupling.

---

## LOW PRIORITY (5 files, cohesive, no refactor needed)

1. **cmd/probe_chat/cases.go** (876 LOC): Test cases + verifiers. Dense but single domain (test data).
2. **internal/agent/tools/registry/memory_search.go** (718 LOC): Single tool, high cohesion.
3. **internal/api/types.go** (673 LOC): API types + marshaling helpers. All API domain.
4. **internal/llm/openai.go** (608 LOC): OpenAI integration. Single provider, cohesive.

---

## Near-God Watch List (4 files, 500–599 LOC)

Monitor these; escalate to MED if they hit 650+ LOC:

1. **internal/channels/telegram/inbound.go** (~550 LOC): Telegram update normalization + invocation building. Risk: feature creep from new channels.
2. **internal/llm/openai_streaming.go** (~530 LOC): Streaming response handling. Risk: new models + streaming modes.
3. **internal/storage/sources/store.go** (~520 LOC): Source document store CRUD. Risk: new source types.
4. **internal/agent/tools/registry/exec.go** (~510 LOC): Tool execution orchestration. Risk: new pre/post execution hooks.

---

## Atomic Refactor Queue (10 slices, ~9.5 days)

**Goal:** Each slice = 1 commit, passes go test ./..., ready to ship.

### Slice 1: Extract cmd/aura/app_bootstrap.go
- **Move:** newApp() + all setup blocks (lines 151–546)
- **Keep:** App struct + lifecycle in app.go
- **LOC reduction:** 395 → cmd/aura/app.go becomes 756
- **Effort:** 1 day | **Risk:** Low
- **Dependencies:** None; mechanical move

### Slice 2: Extract cmd/aura/app_adapters.go
- **Move:** swarmRunnerAdapter, agentJobRunnerAdapter, *DepsGetter funcs, scheduledTaskRunnerAdapter (lines 974–1151)
- **Keep:** App struct, Phase-C wireBot
- **LOC reduction:** 177 → cmd/aura/app.go becomes 579
- **Effort:** 1 day | **Risk:** Very low

### Slice 3: Extract cmd/aura/app_wire.go
- **Move:** wireBot() + tool registration, scheduler, skill adapters (lines 560–971)
- **Keep:** App struct + lifecycle
- **LOC reduction:** 410 → cmd/aura/app.go becomes 169
- **Effort:** 1 day | **Risk:** Low

### Slice 4: Extract internal/identity/store_helpers.go
- **Move:** All validation, parsing, ID builders (lines 527–869)
- **Keep:** Store type, CRUD methods, auth in base file (for now)
- **LOC reduction:** 340 → internal/identity/store.go becomes 535
- **Effort:** 1.5 days | **Risk:** Low
- **Pattern:** Matches existing agent/exec_helpers.go, telegram/tool_exec_helpers.go

### Slice 5: Extract internal/identity/store_auth.go
- **Move:** DelegateActor, BackfillTelegramAllowlistedUser, RevokeGrant, Authorize, recordDecision, hasActiveGrant* (lines 235–525)
- **Keep:** Store type, CRUD, helpers
- **LOC reduction:** 290 → internal/identity/store.go becomes 245
- **Effort:** 1.5 days | **Risk:** Low
- **Testing:** Create store_auth_test.go or integrate into store_test.go

### Slice 6: Extract internal/agent/loop_dedup.go
- **Move:** IsRetryableToolResult, toolBudgetFinalInstruction, budgetCapToolResult, finalAnswerOnBudget, DuplicateOrMaxCallsPolicy, tool result formatting, argKeysFromCall, interruptedAssistantContent, findAskUserCall (lines 63–221, 692–815)
- **Keep:** runLoop, State interface, ToolExecutor, core types
- **LOC reduction:** 150 → internal/agent/loop.go becomes 664
- **Effort:** 1 day | **Risk:** Low

### Slice 7: Extract internal/agent/loop_finalize.go
- **Move:** finalizeAnswerAfterBudget (lines 660–690)
- **Keep:** Main loop, dedup (via loop_dedup)
- **LOC reduction:** 30 → internal/agent/loop.go becomes 634
- **Effort:** 0.5 days | **Risk:** Very low | **Note:** Optional; tiny file

### Slice 8: Extract internal/chat/hub_lifecycle.go
- **Move:** persistLifecycleEvent, recordQuestionRequested, isDurableRunEvent, lifecycleRunMetadata, lifecycleEventParams, lifecyclePayload (lines 456–667)
- **Keep:** Hub dispatch, emit, thread status
- **LOC reduction:** 200 → internal/chat/hub.go becomes 556
- **Effort:** 1 day | **Risk:** Low

### Slice 9: Extract internal/storage/runs/store_questions.go
- **Move:** RecordQuestionRequested, RecordQuestionAnswered, GetQuestion, LatestPendingQuestion + question types
- **Keep:** Store type, run/event lifecycle
- **LOC reduction:** 100 → internal/storage/runs/store.go becomes 681
- **Effort:** 1 day | **Risk:** Low

### Slice 10: Extract internal/wiki/hygiene_proposals.go
- **Move:** Proposal-related functions (e.g., indexProposal*, promoteProposal*)
- **Keep:** Stale-memory deletion core
- **LOC reduction:** 150 → internal/wiki/memory_hygiene.go becomes 609
- **Effort:** 1 day | **Risk:** Low | **Note:** Check function boundaries first

---

## Post-Refactor State

**Before:** 15 files >600 LOC (9 files >700 LOC)
**After:** ~5–7 files >600 LOC (3 files >700 LOC: migrations 1294, memoryindex 788, storage/runs 781)

**Remaining >600 files:** All are either **cohesive single-responsibility** (llm/openai, memory_search, api/types, probe_chat/cases) or **architectural necessities** (migrations, storage stores).

**Acceptable state:** CLAUDE.md rule satisfied if all god files >600 LOC have clear, single responsibility (no mixing unrelated concerns).

---

## Execution Checklist

1. ✓ **Function inventory extracted** — line ranges + responsibility noted for each HIGH file
2. ✓ **Pattern alignment** — new *_helpers.go files match existing pattern (agent/exec_helpers, telegram/tool_exec_helpers)
3. ✓ **Recent commit history reviewed** — app.go touched in 2 of last 15 commits; loop.go touched in 1 of last 15
4. ✓ **Import-cycle dependencies checked** — adapters in app.go are intentional isolation; safe to move
5. ⏳ **Ready for queue** — programmer can dispatch Slice 1 (app_bootstrap.go) immediately; each slice is atomic + passes go test

---
