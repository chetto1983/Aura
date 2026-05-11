---
phase: 02-llm-reliability-tool-intelligence
plan: 06b
type: execute
wave: 4
depends_on: ["02-06"]
files_modified:
  - internal/telegram/setup.go
  - internal/telegram/bot.go
  - internal/telegram/conversation_tool_exec.go
  - internal/telegram/debug_smoke.go
  - internal/telegram/debug_smoke_test.go
  - internal/tools/exec.go
  - internal/tools/registry_test.go
  - internal/tools/tool_search.go
  - internal/tools/tool_search_test.go
  - cmd/debug_telegram_sandbox/main.go
  - scripts/test-agent-tool-search-smoke.ps1
  - scripts/test-runtime-answer-discipline-smokes.ps1
  - internal/api/router.go
  - internal/api/health.go
  - internal/api/types.go
autonomous: true
requirements:
  - TOOL-01
  - TOOL-02
  - INDEX-01
  - WIKI-01
  - GIT-01

must_haves:
  truths:
    - "setup.go constructs reindex.Worker by capturing the existing `searchEngine` LOCAL variable (search.Repository — verified at setup.go:140 + 207 during 2026-05-10 plan revision) BEFORE Bot is built and passing it directly into reindex.NewWorker(searchEngine, ...). BLOCKER 6 Option B of 2026-05-10 revision — the recommended small-diff path. Bot.search field stays as search.Searcher; no widening required."
    - "Worker.Stop() called in Bot.Shutdown AFTER archiver.Close and BEFORE final logging; nil-safe"
    - "WriteWikiPageTool registered via tools.NewWriteWikiPageTool(wikiStore, reindexWorker) — using the LOCAL *wiki.Store variable, NOT b.wiki. BLOCKER 3 of 2026-05-11 plan revision 2: b.wiki is typed `wiki.Repository` (verified at bot.go:44 during revision 2) — the Repository interface composes PageReader, SlugResolver, PageWriter, Directory, Maintainer, Journal but does NOT expose SetReindexSubmitter (which lives only on the concrete *wiki.Store at store.go:20 and is added in Plan 06 Task 2). The setup.go local `wikiStore *wiki.Store` is the same instance, just before it gets narrowed to the interface; use it for both the SetReindexSubmitter call AND the NewWriteWikiPageTool call."
    - "BuildVectorIndex Collection field updated to aura_tool_search_v2 (T-02-F mitigation; matches Plan 05's production default at registry_search_vector.go:76)"
    - "NewRetryClient call populates the three new RetryConfig fields (MaxContentRetries=3, ContentTemperatures=[0.0,0.3,0.7], JitterRatio=0.5) per Plan 01"
    - "tool_search deletion sweep is COMPLETE — verified by the comprehensive grep at the end of this plan: zero matches in production code (internal/, cmd/, scripts/) AND zero matches in test files except the deleted tool_search_test.go which is removed from disk. BLOCKER 1 of 2026-05-10 plan revision lists ALL the sweep sites — they are all itemized below."
    - "Plan 06b's deletion sweep includes EVERY site enumerated in BLOCKER 1 of 2026-05-10 revision: internal/tools/exec.go (lines 97, 126, 216, 269, 297), internal/tools/registry_test.go (lines 162-204 — TestToolSearchToolReturnsJSONResults + TestToolSearchToolRequiresQuery DELETED), internal/telegram/debug_smoke_test.go (lines 514-562 — every tools.NewToolSearchTool call removed), internal/telegram/conversation_tool_exec.go (the tool_search branch + toolNamesFromToolSearchResult helper), internal/telegram/debug_smoke.go (the tool_search case), cmd/debug_telegram_sandbox/main.go (--expect-tool-search-calls-max flag, ToolSearchCalls counter, and assertion), scripts/test-agent-tool-search-smoke.ps1 (DELETE entirely), scripts/test-runtime-answer-discipline-smokes.ps1 (strip tool_search assertions only)"
    - "Worker.Health() Snapshot is wired into /api/health JSON response under a `reindex` key — closes WARNING 12 of 2026-05-10 revision. Bot exposes a small accessor `ReindexHealth() reindex.Health` (or returns the zero value when worker is nil); the api/health handler reads it via the existing dashboard handler boundary."
  artifacts:
    - path: "internal/telegram/setup.go"
      provides: "reindex.Worker constructed from searchEngine; wikiStore (local *wiki.Store) ↔ Submitter wiring via wikiStore.SetReindexSubmitter; write_wiki_page tool registration; BuildVectorIndex Collection updated; tool_search registration removed; NewRetryClient populated with new Phase 2 fields. BLOCKER 3 of 2026-05-11 revision 2: SetReindexSubmitter is called on the local wikiStore variable, NOT b.wiki (which is the narrower wiki.Repository interface at bot.go:44 and lacks the setter)."
    - path: "internal/telegram/bot.go"
      provides: "Bot struct gains private `reindex *reindex.Worker` field + ReindexHealth() accessor; Shutdown calls Worker.Stop() after archiver.Close"
      exports: ["ReindexHealth"]
    - path: "internal/telegram/conversation_tool_exec.go"
      provides: "tool_search branch removed; toolNamesFromToolSearchResult helper deleted"
    - path: "internal/telegram/debug_smoke.go"
      provides: "tool_search case removed"
    - path: "internal/telegram/debug_smoke_test.go"
      provides: "lines 514-562 cleaned: every tools.NewToolSearchTool(reg) call removed; test fixtures regenerated with ONLY non-deleted tool registrations (BLOCKER 1 of 2026-05-10 revision)"
    - path: "internal/tools/exec.go"
      provides: "lines 97 + 126 (descriptions/parameters): tool_search references replaced with neutral language; line 216 (blockedInternalToolCalls): tool_search entry removed; lines 269 + 297 (error messages): updated to drop the use-tool-search-first phrasing (BLOCKER 1 of 2026-05-10 revision)"
    - path: "internal/tools/registry_test.go"
      provides: "lines 162-204 deleted in full: TestToolSearchToolReturnsJSONResults + TestToolSearchToolRequiresQuery + the surrounding helpers removed (BLOCKER 1 of 2026-05-10 revision). TestRegistrySearchClampsLimitAndExcludesTools at line 160 has the literal `tool_search` test fixture name renamed to a generic placeholder (e.g., \"alpha\") to keep the test functional without referencing the deleted tool"
    - path: "internal/tools/tool_search.go"
      provides: "DELETED"
    - path: "internal/tools/tool_search_test.go"
      provides: "DELETED"
    - path: "cmd/debug_telegram_sandbox/main.go"
      provides: "--expect-tool-search-calls-max flag, ToolSearchCalls counter field, and the corresponding assertion all REMOVED"
    - path: "scripts/test-agent-tool-search-smoke.ps1"
      provides: "DELETED"
    - path: "scripts/test-runtime-answer-discipline-smokes.ps1"
      provides: "tool_search assertions stripped, rest of the script preserved"
    - path: "internal/api/router.go"
      provides: "no new routes — the existing /api/health route is reused; health.go is what changes"
    - path: "internal/api/health.go"
      provides: "Health JSON response includes `reindex: {queue_depth, dropped, dropped_after_stop, last_success, last_error}` populated from Bot.ReindexHealth() — WARNING 12 of 2026-05-10 plan revision"
    - path: "internal/api/types.go"
      provides: "ReindexHealthResponse struct mirroring reindex.Health for the /api/health JSON contract"
      exports: ["ReindexHealthResponse"]
  key_links:
    - from: "internal/telegram/setup.go reindex.NewWorker call"
      to: "internal/telegram/setup.go searchEngine local variable"
      via: "BLOCKER 6 Option B: pass searchEngine directly into NewWorker BEFORE Bot is constructed; Bot.search field stays as search.Searcher"
      pattern: "reindex.NewWorker\\(searchEngine"
    - from: "internal/telegram/setup.go wikiStore.SetReindexSubmitter call"
      to: "internal/wiki/store.go SetReindexSubmitter (added in Plan 06 Task 2)"
      via: "BLOCKER 3 of 2026-05-11 revision 2 — call site uses the local *wiki.Store variable, NOT b.wiki (Repository interface)"
      pattern: "wikiStore\\.SetReindexSubmitter\\(reindexWorker\\)"
    - from: "internal/telegram/bot.go ReindexHealth"
      to: "internal/api/health.go handler"
      via: "Bot exposes reindex Worker.Health() snapshot; api/health handler reads it"
      pattern: "ReindexHealth\\(\\)"
---

<objective>
Second half of the integration wave (split per WARNING 10 of the 2026-05-10 plan revision). Plan 06 finished the package-local artifacts (LatestUserMessageText, wiki.Store ↔ Submitter, ToolsProvider closure, prompt cleanup, maxCallsPerTool). Plan 06b finishes the **system-wide wiring** and **deletion sweep**.

Five coordinated changes:

1. **setup.go wiring** — Construct `reindex.Worker` by capturing the existing `searchEngine` LOCAL variable BEFORE Bot is built (BLOCKER 6 Option B of the 2026-05-10 revision — recommended small-diff path; Bot.search field stays as `search.Searcher`). Inject Submitter into wiki.Store via the LOCAL `wikiStore *wiki.Store` variable, NOT b.wiki (BLOCKER 3 of 2026-05-11 revision 2 — b.wiki is the narrower wiki.Repository interface and lacks the setter). Register `write_wiki_page` tool. Update Collection name. Populate NewRetryClient new fields. Remove the NewToolSearchTool registration block.
2. **bot.go shutdown wiring** — Bot struct gains `reindex *reindex.Worker` field + `ReindexHealth()` accessor. Shutdown calls Worker.Stop() AFTER archiver.Close.
3. **tool_search deletion sweep** — Delete `tool_search.go` + `tool_search_test.go`. Clean every site itemized in BLOCKER 1 of the 2026-05-10 revision (verified during the read pass): exec.go (5 sites), registry_test.go (lines 162-204), debug_smoke_test.go (lines 514-562), conversation_tool_exec.go, debug_smoke.go, cmd/debug_telegram_sandbox/main.go, scripts/test-agent-tool-search-smoke.ps1 (DELETE), scripts/test-runtime-answer-discipline-smokes.ps1 (strip).
4. **Worker.Health() /api/health passthrough** — Bot exposes `ReindexHealth() reindex.Health`; api/health.go reads it; api/types.go gains `ReindexHealthResponse`. Closes WARNING 12 of the 2026-05-10 revision (D-16 lock — no Phase 3 deferral).
5. **Final integration smoke** — `go build ./...`, `go test -race -count=1 ./...`, full-repo grep for tool_search/ToolSearchTool/tierSearch/toolNamesFromToolSearchResult returns ZERO production hits.

Purpose: TOOL-02 fully closed (deletion sweep observable via grep guard in Plan 08). INDEX-01 producer→consumer wiring complete. WIKI-01 + GIT-01 reachable end-to-end through the running system. WARNING 12 closed (D-16 health surface wired without Phase 3 deferral).

Output: 13 modified files, 3 deleted files (tool_search.go, tool_search_test.go, test-agent-tool-search-smoke.ps1).
</objective>

<execution_context>
@D:/Aura/.claude/get-shit-done/workflows/execute-plan.md
@D:/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@D:/Aura/CLAUDE.md
@D:/Aura/.planning/phases/02-llm-reliability-tool-intelligence/02-CONTEXT.md
@D:/Aura/.planning/phases/02-llm-reliability-tool-intelligence/02-RESEARCH.md
@D:/Aura/.planning/phases/02-llm-reliability-tool-intelligence/02-PATTERNS.md
@D:/Aura/.planning/phases/02-llm-reliability-tool-intelligence/02-06-SUMMARY.md
@D:/Aura/.planning/phases/02-llm-reliability-tool-intelligence/02-02-SUMMARY.md
@D:/Aura/.planning/phases/02-llm-reliability-tool-intelligence/02-04-SUMMARY.md
@D:/Aura/.planning/phases/02-llm-reliability-tool-intelligence/02-05-SUMMARY.md

<interfaces>
<!-- Plan 02 outputs — public Worker surface that 06b consumes: -->
type Worker struct { ... }
func NewWorker(reindexer Reindexer, cfg Config) *Worker
func (w *Worker) Stop()
func (w *Worker) Health() Health
type Health struct { QueueDepth int; Dropped int64; DroppedAfterStop int64; LastSuccess time.Time; LastError string }

<!-- VERIFIED VERBATIM at internal/telegram/setup.go during 2026-05-10 plan revision: -->
// line 140: var searchEngine search.Repository
// line 163: searchEngine, err = search.NewQdrantRepository(...)
// line 207: passes searchEngine into the BotDeps struct as Search:
// line 419: stores searchEngine on the Bot struct as `search` (search.Searcher narrows here)

<!-- VERIFIED VERBATIM at internal/telegram/bot.go during 2026-05-11 plan revision 2: -->
type Bot struct {
    // ...
    wiki   wiki.Repository  // line 44 — Repository INTERFACE. Composes PageReader+SlugResolver+PageWriter+Directory+Maintainer+Journal. Does NOT expose SetReindexSubmitter.
    search search.Searcher  // line 45 — search.Searcher narrows from search.Repository. Does NOT include ReindexWikiPage.
    tools  *tools.Registry  // line 46 — CONCRETE struct (not an interface).
    // ...
}

<!-- VERIFIED VERBATIM at internal/wiki/store.go during 2026-05-11 plan revision 2: -->
// line 20: type Store struct { ... reindexSubmitter reindex.Submitter (added in Plan 06 Task 2) ... }
// line 86: type Repository interface { PageReader; SlugResolver; PageWriter; Directory; Maintainer; Journal }
//          ^ Repository does NOT include SetReindexSubmitter — it lives only on *Store.

<!-- BLOCKER 6 Option B of 2026-05-10 plan revision (recommended): the searchEngine
     LOCAL variable in setup.go (line 140 / 163, type search.Repository) is captured
     BEFORE Bot is constructed and passed directly into reindex.NewWorker. This way
     Bot.search keeps the narrower search.Searcher interface; the worker holds the
     broader search.Repository (which is what it needs for ReindexWikiPage). NO
     interface widening on Bot.search. -->

<!-- BLOCKER 3 of 2026-05-11 plan revision 2: there is an analogous concern on the
     wiki side. b.wiki is `wiki.Repository` (interface). The Plan 06 Task 2 setter
     `SetReindexSubmitter` lives ONLY on the concrete `*wiki.Store`. setup.go must
     therefore call `wikiStore.SetReindexSubmitter(reindexWorker)` on the LOCAL
     variable, NOT `b.wiki.SetReindexSubmitter(...)` (which would fail to compile
     because the interface lacks the method). The local `wikiStore` and the bot
     field `b.wiki` are the same Store instance — the interface just narrows the
     visible method set. -->

<!-- exec.go references that the deletion sweep MUST clean (BLOCKER 1 of 2026-05-10 revision): -->
// line 97 (description): "...first use tool_search, then pass tools_allowed..."
// line 126 (parameter description): "Optional tool names returned by tool_search and visible in this turn..."
// line 216 (blockedInternalToolCalls map): "tool_search":   true,
// line 269 (error msg): "...not available in the active turn; use tool_search first"
// line 297 (error msg): "...not available in the active turn; use tool_search first"

<!-- registry_test.go that the sweep MUST clean (BLOCKER 1 of 2026-05-10 revision): -->
// lines 162-174 (TestRegistrySearchClampsLimitAndExcludesTools): uses literal string "tool_search" as a test fixture name
//   — this test verifies the EXCLUDE-list functionality of Search; the literal "tool_search" is just a string for that test.
//   The test stays, but rename the fixture to something neutral (e.g., "alpha") so the term tool_search is gone.
// lines 177-201 (TestToolSearchToolReturnsJSONResults): DELETE entirely
// lines 203-208 (TestToolSearchToolRequiresQuery): DELETE entirely
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: setup.go + bot.go wiring — capture searchEngine for reindex.NewWorker, register write_wiki_page, update collection name, update RetryConfig, add ReindexHealth accessor + Worker.Stop in Shutdown</name>
  <files>internal/telegram/setup.go, internal/telegram/bot.go</files>
  <read_first>
    - internal/telegram/setup.go (read FULL — locate ALL these sites: NewRetryClient call ~lines 792-805; BuildVectorIndex call ~lines 519-528; NewToolSearchTool registration ~lines 744-748; the searchEngine local variable at lines 140 + 163 + 207 + 419; the Bot struct definition reference; **AND the local wikiStore variable used during wiki.Store construction — BLOCKER 3 of 2026-05-11 revision 2 requires its exact name. Grep `internal/telegram/setup.go` for `wiki.NewStore(` to find where the *wiki.Store is created and what local name is in scope at the SetReindexSubmitter call site.**)
    - internal/telegram/bot.go (read FULL — Bot struct at lines 38-74; **VERIFY that line 44 reads `wiki wiki.Repository` (interface, NOT *wiki.Store)** — BLOCKER 3 of 2026-05-11 revision 2 hinges on this; Shutdown chain — see archiver.Close ordering)
    - internal/wiki/store.go (read lines 20-95 — confirm `type Store struct` at line 20 carries the `reindexSubmitter reindex.Submitter` field added by Plan 06 Task 2 and that the `wiki.Repository` interface at line 86 does NOT compose `SetReindexSubmitter`. BLOCKER 3 of 2026-05-11 revision 2 verification.)
    - internal/reindex/worker.go (verify NewWorker signature: NewWorker(reindexer Reindexer, cfg Config) *Worker; Worker.Stop signature; DefaultConfig)
    - internal/search/search.go lines 70-91 (verify search.Repository — has ReindexWikiPage method via WikiPageReindexer composition; search.Searcher does NOT)
    - .planning/phases/02-llm-reliability-tool-intelligence/02-PATTERNS.md (lines 962-1035 — setup.go changes)
    - .planning/phases/02-llm-reliability-tool-intelligence/02-04-SUMMARY.md (write_wiki_page tool registration pattern)
  </read_first>
  <behavior>
    - Bot struct gains private field `reindex *reindex.Worker`
    - Bot exposes ReindexHealth() reindex.Health accessor returning the worker's snapshot or the zero value if nil
    - In setup.go, BEFORE Bot is constructed, capture the searchEngine local variable (already assigned at line 140 / 163, type search.Repository) and pass it into reindex.NewWorker. This is BLOCKER 6 Option B of the 2026-05-10 revision: smaller diff than widening Bot.search.
    - **Call wikiStore.SetReindexSubmitter(reindexWorker)** — using the LOCAL *wiki.Store variable created earlier in setup.go (NOT b.wiki, which is the narrower wiki.Repository interface and does not expose the setter). BLOCKER 3 of 2026-05-11 plan revision 2.
    - Register write_wiki_page tool: `if t := tools.NewWriteWikiPageTool(wikiStore, reindexWorker); t != nil { toolRegistry.Register(t) }` — same wikiStore local; placement after the tool registry has the other core tools registered
    - DELETE the `tools.NewToolSearchTool(toolRegistry)` registration block (3 lines)
    - Update NewRetryClient call to populate the new RetryConfig fields: MaxContentRetries=3, ContentTemperatures=[0.0, 0.3, 0.7], JitterRatio=0.5
    - Update `BuildVectorIndex(...)` Collection field: `Collection: "aura_tool_search_v2"` (matches Plan 05's production default)
    - In Bot.Shutdown, add `b.reindex.Stop()` AFTER `b.archiver.Close(...)` and BEFORE any final logging; nil-safe guard
  </behavior>
  <action>
    Step 1 — Add to imports in `internal/telegram/setup.go`:

    ```go
    import (
        // ... existing ...
        "github.com/aura/aura/internal/reindex"
    )
    ```

    Step 2 — In `internal/telegram/bot.go`, add the field to the Bot struct (alongside other private fields):

    ```go
    type Bot struct {
        // ... existing fields ...
        reindex *reindex.Worker // optional; constructed in setup.go AFTER searchEngine is created
    }
    ```

    Add the public accessor:

    ```go
    // ReindexHealth returns the current reindex worker's operational snapshot,
    // or the zero value when the worker is not constructed (e.g. in tests).
    // Surfaced via /api/health (Phase 2 D-16). WARNING 12 of 2026-05-10 plan
    // revision wires this into the dashboard health JSON.
    func (b *Bot) ReindexHealth() reindex.Health {
        if b == nil || b.reindex == nil {
            return reindex.Health{}
        }
        return b.reindex.Health()
    }
    ```

    (Add the `reindex` import to bot.go if it's not already there.)

    Step 3 — In `internal/telegram/setup.go`, locate the bot construction sequence. The searchEngine local variable is created at line 140 / 163 and passed into the BotDeps struct at line 207. INSERT the reindex.Worker construction BEFORE the Bot is fully constructed; capture searchEngine directly:

    ```go
    // BLOCKER 6 Option B (2026-05-10 plan revision): construct reindex.Worker
    // by capturing the searchEngine local (search.Repository — which includes
    // ReindexWikiPage via WikiPageReindexer). This avoids widening Bot.search
    // from search.Searcher to search.Repository. The worker holds the broader
    // interface; Bot.search stays narrow for read-side consumers.
    var reindexWorker *reindex.Worker
    if searchEngine != nil {
        reindexWorker = reindex.NewWorker(searchEngine, reindex.DefaultConfig())
    }
    ```

    Place this block AFTER `searchEngine` is assigned (after the search.NewQdrantRepository call) and BEFORE the BotDeps struct is built / Bot is constructed.

    Step 4 — **BLOCKER 3 of 2026-05-11 plan revision 2**: wire the worker into the Bot struct AND inject the Submitter into wiki.Store via the LOCAL `wikiStore *wiki.Store` variable. **DO NOT use `b.wiki` for the setter call.** b.wiki is typed `wiki.Repository` (verified at bot.go:44 during this revision) — the interface composes PageReader, SlugResolver, PageWriter, Directory, Maintainer, Journal but does NOT expose `SetReindexSubmitter` (which lives only on the concrete *wiki.Store, added in Plan 06 Task 2). Calling `b.wiki.SetReindexSubmitter(...)` would fail to compile.

    Use the LOCAL `wikiStore` variable that setup.go already keeps in scope from the earlier `wikiStore, err = wiki.NewStore(...)` call. The local `wikiStore` and the bot field `b.wiki` are the same Store instance — the interface just narrows the visible method set.

    The wiring can happen EITHER before Bot construction (the recommended ordering — set the submitter so any writes during bot startup are properly enqueued) OR immediately after b is non-nil (functionally equivalent because the Store is shared). The plan picks BEFORE for clarity:

    ```go
    // BLOCKER 3 of 2026-05-11 plan revision 2: SetReindexSubmitter lives only on
    // the concrete *wiki.Store, NOT on the wiki.Repository interface. Use the
    // local wikiStore variable — NEVER b.wiki for this setter call (the
    // interface at bot.go:44 lacks the method and would fail to compile).
    if reindexWorker != nil && wikiStore != nil {
        wikiStore.SetReindexSubmitter(reindexWorker)
    }

    // ... existing Bot construction code (NewBot / &Bot{...}) ...

    // After b is non-nil:
    b.reindex = reindexWorker
    ```

    (If the local variable is named differently in setup.go — e.g., `store` or `ws` — adapt the variable name. The invariant is: use the LOCAL concrete-typed *wiki.Store, never the b.wiki field.)

    Step 5 — Register the write_wiki_page tool, also using the local `wikiStore` (BLOCKER 3 of 2026-05-11 plan revision 2 invariant). Find the section of setup.go where existing core tools are registered (~lines 700-750 alongside other `tools.NewXTool` calls). ADD:

    ```go
    if t := tools.NewWriteWikiPageTool(wikiStore, reindexWorker); t != nil {
        toolRegistry.Register(t)
    }
    ```

    Place it BEFORE the (about-to-be-deleted) ToolSearch block. (If NewWriteWikiPageTool's first parameter is declared as the *wiki.Store concrete type, passing wikiStore is the correct match. If it accepts the narrower wiki.Repository interface — verify by reading Plan 04's signature in 02-04-SUMMARY.md — wikiStore still satisfies it because the concrete type implements the interface.)

    Step 6 — DELETE the existing block at ~lines 744-748:

    ```go
    if tool := tools.NewToolSearchTool(toolRegistry); tool != nil {  // DELETE
        toolRegistry.Register(tool)                                   // DELETE
    }                                                                 // DELETE
    ```

    Step 7 — Update the BuildVectorIndex call (~lines 519-528). Change the Collection field:

    Before:
    ```go
    Collection:   "aura_tool_search",
    ```

    After:
    ```go
    Collection:   "aura_tool_search_v2", // Phase 2 T-02-F + matches Plan 05 production default
    ```

    Step 8 — Update the NewRetryClient call (~lines 792-805). Add the three new RetryConfig fields:

    After:
    ```go
    return llm.NewRetryClient(openaiClient, llm.RetryConfig{
        MaxRetries:          cfg.LLMMaxRetries,
        BaseDelay:           time.Second,
        MaxDelay:            30 * time.Second,
        MaxContentRetries:   3,                              // D-07 CONTENT bucket budget
        ContentTemperatures: []float64{0.0, 0.3, 0.7},      // D-07 temperature staircase
        JitterRatio:         0.5,                            // D-07 jitter ratio
    })
    ```

    Step 9 — In `internal/telegram/bot.go` (or wherever Bot.Shutdown / Bot.Stop is defined), add `b.reindex.Stop()` AFTER the archiver close, BEFORE the final logging:

    ```go
    if b.archiver != nil {
        if err := b.archiver.Close(context.Background()); err != nil {
            logger.Error("telegram shutdown: archiver close failed", "error", err)
        }
    }
    // PHASE 2 ADD:
    if b.reindex != nil {
        b.reindex.Stop() // cancels in-flight Reindex; waits for drain goroutine exit
    }
    ```
  </action>
  <verify>
    <automated>cd D:/Aura && go vet ./... && go build ./... && go test -race -count=1 ./internal/telegram/ ./internal/wiki/ ./internal/reindex/ ./internal/tools/ ./internal/llm/</automated>
  </verify>
  <acceptance_criteria>
    - `grep -n "reindexWorker = reindex.NewWorker(searchEngine," internal/telegram/setup.go` matches once (BLOCKER 6 Option B — passing searchEngine directly)
    - `grep -n "wikiStore.SetReindexSubmitter(reindexWorker)" internal/telegram/setup.go` matches once (BLOCKER 3 of 2026-05-11 revision 2 — local *wiki.Store, NOT b.wiki)
    - `grep -n "b.wiki.SetReindexSubmitter" internal/telegram/setup.go` returns ZERO matches (BLOCKER 3 of 2026-05-11 revision 2 — b.wiki is the wiki.Repository interface at bot.go:44 and lacks the setter; this call would not compile)
    - `grep -n "tools.NewWriteWikiPageTool(wikiStore, reindexWorker)" internal/telegram/setup.go` matches once (uses the local *wiki.Store consistently with Step 4)
    - `grep -n "tools.NewToolSearchTool" internal/telegram/setup.go` returns ZERO matches (deleted)
    - `grep -n "aura_tool_search_v2" internal/telegram/setup.go` matches once (T-02-F)
    - `grep -n "\"aura_tool_search\"" internal/telegram/setup.go` returns ZERO matches (old name fully gone in production code)
    - `grep -n "ContentTemperatures: \\[\\]float64{0.0, 0.3, 0.7}" internal/telegram/setup.go` matches once
    - `grep -n "MaxContentRetries: *3" internal/telegram/setup.go` matches once
    - `grep -n "JitterRatio: *0.5" internal/telegram/setup.go` matches once
    - `grep -n "reindex *\\*reindex.Worker" internal/telegram/bot.go` matches once (Bot struct field)
    - `grep -n "func (b \\*Bot) ReindexHealth() reindex.Health" internal/telegram/bot.go` matches once (WARNING 12 accessor)
    - `grep -n "b.reindex.Stop()" internal/telegram/bot.go` matches once (Shutdown wiring)
    - `grep -E "^\\s*wiki\\s+wiki\\.Repository" internal/telegram/bot.go` matches once at line ~44 (BLOCKER 3 of 2026-05-11 revision 2 — Bot.wiki field type UNCHANGED; this plan does NOT widen it to *wiki.Store)
    - `grep -E "^\\s*search\\s+search\\.Searcher" internal/telegram/bot.go` matches once at the original line (BLOCKER 6 Option B — Bot.search NOT widened)
    - `go build ./...` exit 0 (would fail if the setter were called on b.wiki — BLOCKER 3 of 2026-05-11 revision 2 compile-time check)
    - `go vet ./...` clean
    - `go test -race -count=1 ./internal/telegram/` exit 0
  </acceptance_criteria>
  <done>
    setup.go captures searchEngine for reindex.NewWorker (BLOCKER 6 Option B; Bot.search stays narrow). wikiStore (local *wiki.Store) ↔ Submitter wired — call site uses the concrete-typed local variable, NEVER b.wiki (BLOCKER 3 of 2026-05-11 revision 2 — the wiki.Repository interface at bot.go:44 does not expose SetReindexSubmitter and would fail to compile). write_wiki_page registered with the same local. tool_search registration removed. Collection → aura_tool_search_v2. NewRetryClient new fields populated. Bot.Shutdown calls Worker.Stop. ReindexHealth accessor in place for WARNING 12.
  </done>
</task>

<task type="auto">
  <name>Task 2: tool_search deletion sweep — exhaustive cleanup of every site enumerated in BLOCKER 1 of 2026-05-10 revision</name>
  <files>internal/tools/tool_search.go, internal/tools/tool_search_test.go, internal/tools/exec.go, internal/tools/registry_test.go, internal/telegram/conversation_tool_exec.go, internal/telegram/debug_smoke.go, internal/telegram/debug_smoke_test.go, cmd/debug_telegram_sandbox/main.go, scripts/test-agent-tool-search-smoke.ps1, scripts/test-runtime-answer-discipline-smokes.ps1, internal/agentloop/loop.go</files>
  <read_first>
    - internal/tools/tool_search.go (verify it can be deleted — no external imports except tests)
    - internal/tools/tool_search_test.go (verify it tests only ToolSearchTool — safe to delete)
    - internal/tools/exec.go (lines 97, 126, 216, 269, 297 — five sites to clean per BLOCKER 1 of 2026-05-10 revision)
    - internal/tools/registry_test.go (lines 160-208 — TestRegistrySearchClampsLimitAndExcludesTools at 160 RENAMES the fixture; TestToolSearchToolReturnsJSONResults at 177 DELETED; TestToolSearchToolRequiresQuery at 203 DELETED)
    - internal/telegram/conversation_tool_exec.go (locate the tool_search branch and toolNamesFromToolSearchResult helper)
    - internal/telegram/debug_smoke.go (locate the tool_search case)
    - internal/telegram/debug_smoke_test.go (lines 514-562 — every tools.NewToolSearchTool(reg) call removed AND the assertion blocks that depend on tool_search results updated; if a test no longer makes sense without tool_search, DELETE the test function entirely)
    - cmd/debug_telegram_sandbox/main.go (line 61 flag, line 195 counter struct field, line 319 increment, line 816 assertion)
    - scripts/test-agent-tool-search-smoke.ps1 (DELETE entirely — D-27)
    - scripts/test-runtime-answer-discipline-smokes.ps1 (REVIEW — keep file, remove tool_search assertions)
    - internal/agentloop/loop.go (verify no `tierSearch` references remain — RESEARCH.md notes none today; sweep is paranoid)
  </read_first>
  <action>
    Step 1 — DELETE files:

    ```bash
    rm internal/tools/tool_search.go
    rm internal/tools/tool_search_test.go
    rm scripts/test-agent-tool-search-smoke.ps1
    ```

    Step 2 — In `internal/tools/exec.go` (BLOCKER 1 of 2026-05-10 plan revision lists FIVE sites in this file):
    - **Line 97** (description text): replace the `"...first use tool_search, then pass tools_allowed..."` clause with `"...pass tools_allowed and have the script write..."` (drop the use-tool-search-first phrase entirely; the new sentence reads naturally without it).
    - **Line 126** (parameter description for tools_allowed): replace `"Optional tool names returned by tool_search and visible in this turn..."` with `"Optional tool names visible in this turn..."`. Remove the tool_search reference.
    - **Line 216** (`blockedInternalToolCalls` map): REMOVE the `"tool_search": true,` line. The map now contains only `execute_code` and `execute_shell`.
    - **Line 269** (error message): replace `"...is not available in the active turn; use tool_search first"` with `"...is not available in the active turn"`. Drop the use-tool-search-first phrasing.
    - **Line 297** (error message): same change as line 269.

    Step 3 — In `internal/tools/registry_test.go` (BLOCKER 1 of 2026-05-10 plan revision):
    - **Lines 160-175** (TestRegistrySearchClampsLimitAndExcludesTools): the test verifies the EXCLUDE-list mechanism of Registry.Search. The literal string `"tool_search"` is just a fixture name. Rename it to a neutral string (e.g., `"alpha"`) so the term tool_search is gone from this test:
      ```go
      // Before
      for _, name := range []string{"tool_search", "mail_one", "mail_two", "mail_three", "mail_four", "mail_five", "mail_six"} {
      // After
      for _, name := range []string{"alpha", "mail_one", "mail_two", "mail_three", "mail_four", "mail_five", "mail_six"} {
      ```
      And the corresponding `reg.Search("mail email", 99, "tool_search")` → `reg.Search("mail email", 99, "alpha")`, plus the inner check `if result.Name == "tool_search"` → `if result.Name == "alpha"`.
    - **Lines 177-201** (TestToolSearchToolReturnsJSONResults): DELETE the entire test function.
    - **Lines 203-208** (TestToolSearchToolRequiresQuery): DELETE the entire test function.
    - Verify the surrounding helper functions (e.g., `namedDescribedTool`) are still used by other tests; if any helper became orphaned, delete it too.

    Step 4 — In `internal/telegram/conversation_tool_exec.go`:
    - Find and DELETE the `tool_search` branch (around line 114). If it's structured as `if name == "tool_search" { ... }` or a switch case, remove it entirely.
    - Find and DELETE the `toolNamesFromToolSearchResult` helper function (around lines 121-137). If anything still calls it, remove the caller too.

    Step 5 — In `internal/telegram/debug_smoke.go`:
    - Find and DELETE the `tool_search` case (around line 186). It's likely inside a `switch name {}` block — remove the `case "tool_search":` and its body.

    Step 6 — In `internal/telegram/debug_smoke_test.go` (BLOCKER 1 of 2026-05-10 plan revision — lines 514-562):
    - DELETE every `tools.NewToolSearchTool(reg)` call (line 514 inside one test, line 537 inside another).
    - In `modelToolNames` test: remove `"tool_search"` from the expected-allowlist slice (line ~526).
    - In the `TestRunToolCallingLoopAddsToolSearchDiscoveries` test: this test's whole purpose is exercising tool_search — DELETE the entire function (lines 535 to its closing brace).
    - Verify any helper variables (`searchTool`, `mailTool`) are no longer referenced after the deletion; if they are dangling, remove their declarations.

    Step 7 — In `cmd/debug_telegram_sandbox/main.go`:
    - DELETE the `--expect-tool-search-calls-max` flag definition at line ~61.
    - DELETE the `ToolSearchCalls` counter struct field at line ~195.
    - DELETE the increment at line ~319 (likely inside a tool-call dispatch branch).
    - DELETE the assertion at line ~816 (`if ... .ToolSearchCalls > expected ...`).

    Step 8 — In `scripts/test-runtime-answer-discipline-smokes.ps1`:
    - Open the file, find every line mentioning `tool_search`, and DELETE those lines. Preserve all other content.

    Step 9 — In `internal/agentloop/loop.go`:
    - Run `grep -n "tierSearch" internal/agentloop/loop.go`. If anything matches, remove it. RESEARCH.md notes nothing exists today — sweep is paranoid.

    Step 10 — Sweep verification:

    ```bash
    grep -RIn 'tool_search\|ToolSearchTool\|tierSearch\|toolNamesFromToolSearchResult' internal/ cmd/ scripts/
    ```

    The grep MUST return ZERO matches. If anything survives:
    - Production .go file → fix the production code immediately.
    - Test file → fix or delete.
    - PowerShell script → fix or delete.
    - .planning/ directory → leave alone (planning artifacts are excluded by the Plan 08 CI guard).

    The `_test.go` exclusion is INTENTIONALLY ABSENT from this sweep — BLOCKER 1 of the 2026-05-10 plan revision points out that the original Plan 08 CI guard hid tool_search references inside test files. After this task lands, ZERO matches everywhere is the correct invariant.
  </action>
  <verify>
    <automated>cd D:/Aura && go vet ./... && go build ./... && go test -race -count=1 ./... 2>&1 | tail -40 && echo "--- sweep ---" && grep -RIn "tool_search\|ToolSearchTool\|tierSearch\|toolNamesFromToolSearchResult" internal/ cmd/ scripts/ | grep -v "Binary file" | head -20</automated>
  </verify>
  <acceptance_criteria>
    - `test -f internal/tools/tool_search.go` returns false (file deleted)
    - `test -f internal/tools/tool_search_test.go` returns false (file deleted)
    - `test -f scripts/test-agent-tool-search-smoke.ps1` returns false (file deleted)
    - `grep -RIn "tool_search\\|ToolSearchTool\\|tierSearch\\|toolNamesFromToolSearchResult" internal/ cmd/ scripts/` returns ZERO matches in BOTH production AND test files (BLOCKER 1 of 2026-05-10 plan revision: the previous `_test.go` exclusion is lifted because Plan 08's CI guard relies on this exhaustive sweep)
    - `grep -n "blockedInternalToolCalls\\[\"tool_search\"\\]\\|\"tool_search\":[[:space:]]*true" internal/tools/exec.go` returns ZERO matches (line 216 cleaned)
    - `grep -c "use tool_search" internal/tools/exec.go` returns 0 (lines 269 + 297 cleaned)
    - `grep -n "TestToolSearchToolReturnsJSONResults\\|TestToolSearchToolRequiresQuery" internal/tools/registry_test.go` returns ZERO matches (test functions deleted)
    - `grep -n "tools.NewToolSearchTool" internal/telegram/debug_smoke_test.go` returns ZERO matches (every call removed)
    - `grep -n "TestRunToolCallingLoopAddsToolSearchDiscoveries" internal/telegram/debug_smoke_test.go` returns ZERO matches (test function deleted)
    - `grep -n "ToolSearchCalls\\|expect-tool-search-calls-max" cmd/debug_telegram_sandbox/main.go` returns ZERO matches
    - `grep -n "tool_search" scripts/test-runtime-answer-discipline-smokes.ps1` returns ZERO matches
    - `go build ./...` exit 0
    - `go vet ./...` clean
    - `go test -race -count=1 ./...` exit 0 (full suite GREEN after the sweep)
  </acceptance_criteria>
  <done>
    Every site itemized in BLOCKER 1 of 2026-05-10 plan revision is cleaned. tool_search.go + tool_search_test.go + test-agent-tool-search-smoke.ps1 deleted. exec.go five sites updated. registry_test.go: fixture renamed at line 160, two test functions deleted at 177 + 203. debug_smoke_test.go: tool_search test fixtures and test function deleted. debug_telegram_sandbox/main.go: flag + counter + assertion removed. test-runtime-answer-discipline-smokes.ps1: assertions stripped. agentloop/loop.go: tierSearch verified absent. Full-repo grep returns ZERO matches in production AND test files. Full Go test suite GREEN.
  </done>
</task>

<task type="auto">
  <name>Task 3: Wire Worker.Health() into /api/health JSON response (WARNING 12)</name>
  <files>internal/api/health.go, internal/api/types.go</files>
  <read_first>
    - internal/api/router.go (read FULL — verify `mux.HandleFunc("GET /health", handleHealth(deps))` at line 153; the existing handler is what gets the new field)
    - internal/api/health.go (read FULL — current health response shape; this plan ADDS a `reindex` field)
    - internal/api/types.go (read FULL — locate the existing health-response struct that handleHealth marshals; ADD ReindexHealthResponse)
    - internal/api/dependencies.go OR wherever Deps is defined (find the boundary that handleHealth reads from — does it have a way to reach Bot.ReindexHealth()?)
    - internal/telegram/bot.go (Task 1 added ReindexHealth() — verify how Deps gets a reference to the Bot or how the existing health handler reaches Bot data; CompactMemoryHealth accessor at line 85 is the analog)
    - internal/reindex/types.go (Health struct shape from Plan 02)
  </read_first>
  <behavior>
    - api/types.go declares `ReindexHealthResponse` with json-tagged fields mirroring reindex.Health
    - The existing health response struct gains a `Reindex ReindexHealthResponse` field (omitempty NOT used — always present so TS strict types stay stable, mirroring the Plan 07 unversioned passthrough decision)
    - api/health.go's handler populates the field by calling whatever boundary already exists for Bot data. If Deps does not yet have a way to reach Bot.ReindexHealth(), introduce a minimal `Deps.ReindexHealth func() reindex.Health` callback; setup.go's wiring sets it to the bot's accessor.
    - When ReindexHealth callback is nil (test fixtures), populate the field with the zero value
  </behavior>
  <action>
    Step 1 — In `internal/api/types.go`, append:

    ```go
    // ReindexHealthResponse mirrors reindex.Health for the /api/health JSON
    // contract. Surfaces D-16 worker telemetry to the dashboard. WARNING 12
    // of 2026-05-10 plan revision wires this in (NOT deferred to Phase 3).
    type ReindexHealthResponse struct {
        QueueDepth       int       `json:"queue_depth"`
        Dropped          int64     `json:"dropped"`
        DroppedAfterStop int64     `json:"dropped_after_stop"`
        LastSuccess      time.Time `json:"last_success"`
        LastError        string    `json:"last_error,omitempty"`
    }
    ```

    Add the existing health-response struct a `Reindex ReindexHealthResponse` field. (Read the file to find the actual struct name; this plan does not preselect it — typical names are `HealthResponse`, `Health`, `HealthPayload`. Whatever the existing name is, add the field.)

    Step 2 — In `internal/api/dependencies.go` (or wherever Deps is defined — read first), add a small callback if one is not already present:

    ```go
    type Deps struct {
        // ... existing fields ...
        ReindexHealth func() reindex.Health // optional; nil yields the zero-value response
    }
    ```

    (Add the `reindex` import to that file.)

    Step 3 — In `internal/api/health.go`, populate the new field:

    ```go
    var rh reindex.Health
    if deps.ReindexHealth != nil {
        rh = deps.ReindexHealth()
    }
    response.Reindex = ReindexHealthResponse{
        QueueDepth:       rh.QueueDepth,
        Dropped:          rh.Dropped,
        DroppedAfterStop: rh.DroppedAfterStop,
        LastSuccess:      rh.LastSuccess,
        LastError:        rh.LastError,
    }
    ```

    (Adapt to the existing handler's variable names. The existing handler likely already builds a struct it marshals at the end; the new lines slot before that marshal.)

    Step 4 — In `internal/telegram/setup.go` where the api/Deps is constructed for `b.api`, set the ReindexHealth callback:

    ```go
    apiDeps := api.Deps{
        // ... existing ...
        ReindexHealth: b.ReindexHealth,
    }
    ```

    Add a small test in `internal/api/health_test.go` (append; do not clobber). **WARNING 7 of 2026-05-11 plan revision 2 note**: the test body below uses `int64(rx["dropped"].(float64))` — this is a valid Go composition (type assertion on the `any` value to extract the JSON-decoded `float64`, then explicit type conversion to int64). Both operations parse cleanly and are idiomatic. Just verify the test compiles before claiming it is green; see the `<done>` block:

    ```go
    func TestHealth_ReindexFieldPresent(t *testing.T) {
        // Build a Deps with a ReindexHealth callback returning known values.
        deps := newTestDeps(t)
        deps.ReindexHealth = func() reindex.Health {
            return reindex.Health{QueueDepth: 7, Dropped: 3, DroppedAfterStop: 1}
        }
        srv := httptest.NewServer(api.NewRouter(deps))
        defer srv.Close()
        resp, err := http.Get(srv.URL + "/health")
        if err != nil { t.Fatal(err) }
        defer resp.Body.Close()
        var body map[string]any
        json.NewDecoder(resp.Body).Decode(&body)
        rx, ok := body["reindex"].(map[string]any)
        if !ok { t.Fatalf("response missing reindex field: %v", body) }
        if int(rx["queue_depth"].(float64)) != 7 {
            t.Fatalf("queue_depth = %v, want 7", rx["queue_depth"])
        }
        // WARNING 7 of 2026-05-11 plan revision 2: int64(...(float64)) is
        // valid Go — type assertion on `any` to extract float64, then
        // explicit conversion to int64. Compiles cleanly.
        if int64(rx["dropped"].(float64)) != 3 {
            t.Fatalf("dropped = %v, want 3", rx["dropped"])
        }
        if int64(rx["dropped_after_stop"].(float64)) != 1 {
            t.Fatalf("dropped_after_stop = %v, want 1", rx["dropped_after_stop"])
        }
    }

    func TestHealth_ReindexFieldZero_WhenCallbackNil(t *testing.T) {
        deps := newTestDeps(t)
        deps.ReindexHealth = nil
        srv := httptest.NewServer(api.NewRouter(deps))
        defer srv.Close()
        resp, _ := http.Get(srv.URL + "/health")
        defer resp.Body.Close()
        var body map[string]any
        json.NewDecoder(resp.Body).Decode(&body)
        rx, ok := body["reindex"].(map[string]any)
        if !ok { t.Fatalf("reindex field missing even when callback nil: %v", body) }
        if int(rx["queue_depth"].(float64)) != 0 {
            t.Fatalf("queue_depth = %v, want 0 (zero value)", rx["queue_depth"])
        }
    }
    ```
  </action>
  <verify>
    <automated>cd D:/Aura && go vet ./... && go test -race -count=1 -run "TestHealth_Reindex" ./internal/api/ && go build ./...</automated>
  </verify>
  <acceptance_criteria>
    - `grep -n "type ReindexHealthResponse struct" internal/api/types.go` matches once (WARNING 12 of 2026-05-10 plan revision)
    - `grep -n "Reindex *ReindexHealthResponse" internal/api/types.go` matches once (the field on the health response struct)
    - `grep -n "ReindexHealth func() reindex.Health" internal/api/dependencies.go` matches once (or wherever Deps lives) OR `grep -rn "ReindexHealth func()" internal/api/` matches at least once
    - `grep -n "ReindexHealth: b.ReindexHealth" internal/telegram/setup.go` matches once (callback wired)
    - `grep -n "TestHealth_ReindexFieldPresent\\|TestHealth_ReindexFieldZero_WhenCallbackNil" internal/api/health_test.go` matches both tests
    - `go test -race -count=1 ./internal/api/` exit 0
    - `go build ./...` exit 0
    - The /api/health response now carries `reindex` as a stable always-present object — TypeScript dashboard consumers can rely on it (no Phase 3 deferral)
  </acceptance_criteria>
  <done>
    Worker.Health() Snapshot is reachable through /api/health under the `reindex` key. ReindexHealthResponse mirrors the Plan 02 reindex.Health struct. Two integration tests verify both populated and zero-value cases. WARNING 12 of 2026-05-10 plan revision is closed. WARNING 7 of 2026-05-11 plan revision 2: verify the test compiles cleanly with `go test -run TestReindexHealthInHealthEndpoint ./internal/api/` (or the actual test names emitted: `TestHealth_ReindexFieldPresent`, `TestHealth_ReindexFieldZero_WhenCallbackNil`) — the `int64(rx["dropped"].(float64))` composition is valid Go (type assertion then explicit conversion) but the executor must confirm by running the test, not just inspecting the source.
  </done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| Bot.Shutdown → reindex.Worker.Stop | Shutdown sequence ordering matters; archiver must close before worker so any final reindex enqueues drain. |
| Bot.ReindexHealth → /api/health | Read-only snapshot; no PII; auth-gated by the existing /api/* bearer wrapper. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-02-D | DoS | reindex.Worker goroutine on bot shutdown | mitigate | `b.reindex.Stop()` placed AFTER `b.archiver.Close(...)` in the Shutdown sequence so any final reindex enqueues from archiver flushes drain first. Stop() cancels worker ctx (aborts in-flight HTTP within ms) and waits on `<-done`. Plan 02's `TestWorker_NoGoroutineLeak` covers the goroutine-leak invariant. |
| T-02-RED-27 | Spoofing | Plan 02 Worker constructed against the wrong reindexer | mitigate | BLOCKER 6 Option B of 2026-05-10 plan revision: `searchEngine` (search.Repository — broader than search.Searcher; includes ReindexWikiPage) is captured BEFORE the BotDeps struct narrows it to search.Searcher and passed directly into reindex.NewWorker. The narrower Bot.search field stays unchanged. Verified by `go build` (would fail if Worker.Reindexer constraint not satisfied). |
| T-02-RED-28 | Repudiation | Worker telemetry invisible to operators | mitigate | WARNING 12 of 2026-05-10 plan revision: ReindexHealthResponse is wired into /api/health JSON. Dashboard surfaces queue_depth, dropped, dropped_after_stop, last_success, last_error. Operators can detect a degraded reindex worker without grep-ing logs. D-16 lock honored without Phase 3 deferral. |
| T-02-RED-29 | Tampering | Future PR re-introduces tool_search | mitigate | Plan 08's CI guard scripts run before tests; any reintroduction fails CI. The `.planning/` directory is excluded so historical planning artifacts don't false-trigger. The 2026-05-10 plan revision tightens the guard to NOT exclude `_test.go` (BLOCKER 1) so test files cannot mask references either. |
| T-02-RED-33 | DoS / Compile failure | `b.wiki.SetReindexSubmitter(...)` called on the wiki.Repository interface | mitigate | BLOCKER 3 of 2026-05-11 plan revision 2: the call site is locked to the local `wikiStore *wiki.Store` variable. b.wiki at bot.go:44 is `wiki.Repository` (PageReader + SlugResolver + PageWriter + Directory + Maintainer + Journal) which does NOT compose SetReindexSubmitter — that setter lives only on the concrete *wiki.Store. The acceptance grep enforces zero `b.wiki.SetReindexSubmitter` matches. |
</threat_model>

<verification>
- `go vet ./...` clean
- `go build ./...` clean
- `go test -race -count=1 ./...` full suite GREEN
- `grep -RIn "tool_search\|ToolSearchTool\|tierSearch\|toolNamesFromToolSearchResult" internal/ cmd/ scripts/` returns ZERO matches in production AND test files (BLOCKER 1 of 2026-05-10 plan revision invariant)
- `grep -n "aura_tool_search_v2" internal/telegram/setup.go` matches once
- `grep -n "ContentTemperatures: \\[\\]float64{0.0, 0.3, 0.7}" internal/telegram/setup.go` matches once
- `grep -n "b.reindex.Stop()" internal/telegram/bot.go` matches once
- `grep -n "func (b \\*Bot) ReindexHealth() reindex.Health" internal/telegram/bot.go` matches once
- `grep -n "type ReindexHealthResponse struct" internal/api/types.go` matches once
- `grep -n "ReindexHealth: b.ReindexHealth" internal/telegram/setup.go` matches once
- `grep -n "wikiStore.SetReindexSubmitter(reindexWorker)" internal/telegram/setup.go` matches once (BLOCKER 3 of 2026-05-11 revision 2)
- `grep -n "b.wiki.SetReindexSubmitter" internal/telegram/setup.go` returns 0 (BLOCKER 3 of 2026-05-11 revision 2)
- 2 new health-passthrough tests GREEN in /internal/api
</verification>

<success_criteria>
- INDEX-01 wired end-to-end: searchEngine → reindex.Worker → wikiStore.SetReindexSubmitter; Worker.Stop in Shutdown
- WIKI-01 reachable: write_wiki_page registered as a core tool with submitter wired through the local *wiki.Store
- GIT-01 reachable: Plan 03's Unversioned set/clear is reachable from real LLM-driven writes
- LLM-01/02 wired: NewRetryClient gets the new fields
- TOOL-01 + TOOL-02 fully closed: aura_tool_search_v2 in setup.go; tool_search deletion sweep complete across BOTH production AND test files
- WARNING 12 closed: ReindexHealth surfaces under /api/health (D-16 lock honored without Phase 3 deferral)
- BLOCKER 1 of 2026-05-10 plan revision closed: every itemized site in exec.go, registry_test.go, debug_smoke_test.go, conversation_tool_exec.go, debug_smoke.go, debug_telegram_sandbox/main.go, scripts/*.ps1 cleaned
- BLOCKER 6 of 2026-05-10 plan revision closed: Option B used; searchEngine captured for NewWorker; Bot.search not widened
- BLOCKER 3 of 2026-05-11 plan revision 2 closed: SetReindexSubmitter is called on the local `wikiStore *wiki.Store`, NEVER on `b.wiki` (the wiki.Repository interface at bot.go:44, which lacks the setter)
- WARNING 7 of 2026-05-11 plan revision 2 closed: Task 3 `<done>` block explicitly requires the executor to run the test (not just inspect) to confirm the `int64(rx["dropped"].(float64))` composition compiles
- ROADMAP success criteria #1 (no heuristics), #4 (git audit), #5 (async reindex), #6 (per-turn tool injection) all observable through the running system
</success_criteria>

<output>
After completion, create `.planning/phases/02-llm-reliability-tool-intelligence/02-06b-SUMMARY.md`:
- setup.go: reindex.Worker constructed from searchEngine local (BLOCKER 6 Option B); wikiStore (local *wiki.Store) ↔ Submitter wired via wikiStore.SetReindexSubmitter — NEVER b.wiki (BLOCKER 3 of 2026-05-11 revision 2: the wiki.Repository interface at bot.go:44 lacks the setter, which lives only on the concrete *wiki.Store at store.go:20); write_wiki_page registered with the same local *wiki.Store; Collection: aura_tool_search_v2; new RetryConfig fields populated; ToolSearch registration deleted
- bot.go: Bot.reindex field + ReindexHealth() accessor; Shutdown calls Worker.Stop after archiver.Close
- Comprehensive deletion sweep across exec.go (5 sites), registry_test.go (1 rename + 2 deletions), debug_smoke_test.go (full deletion of NewToolSearchTool calls and one test function), conversation_tool_exec.go (branch + helper deleted), debug_smoke.go (case deleted), debug_telegram_sandbox/main.go (flag + counter + assertion deleted), scripts/test-agent-tool-search-smoke.ps1 (deleted), scripts/test-runtime-answer-discipline-smokes.ps1 (assertions stripped); full-repo grep returns zero matches in production AND test files
- /api/health JSON gains `reindex` field via ReindexHealthResponse — WARNING 12 closed without Phase 3 deferral; WARNING 7 of 2026-05-11 revision 2: test bodies use `int64(x.(float64))` composition (valid Go: assertion then conversion) and the executor confirms compile-clean by running the tests
- Notes for Plan 08: the CI grep guard SHOULD NOT exclude `_test.go` files (BLOCKER 1 of 2026-05-10 plan revision); the godclass test must continue to pass; the precision@5 retrieval fixture references actual registered tool names
</output>
