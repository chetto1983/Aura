# Phase 12 — Compounding Memory: Final Review (slice 12u)

**Reviewer:** Claude Opus 4.7 (1M context)
**Reviewed:** 2026-05-02
**Scope:** all source modified or created in 280ef8b..23f56a4
**Verdict:** **BLOCK — 2 CRITICAL bugs prevent v0.12.0 tag.**

---

## Summary

| Severity | Count |
|---|---|
| CRITICAL | 2 |
| HIGH | 7 |
| MEDIUM | 8 |
| LOW | 6 |
| **Total** | **23** |

The shipping pipeline is in good shape — concurrency primitives are sound, SQL migrations are idempotent and ordered correctly, the auth gate covers all 6 new endpoints, and `staticcheck` is clean. However two outright product breakages must land as 12u.N follow-ups before tag:

1. **CR-01** — `ConversationsPanel` reads `data.turns` from a Go endpoint that returns a bare array, so the entire Conversations route is stuck on its empty state regardless of archived data.
2. **CR-02** — `ConversationsPanel` requires a chat_id at the API layer but the panel is rendered with chat_id empty by default; opening `/conversations` without typing a chat ID hits HTTP 400 immediately.

The remaining HIGH items (partial-commit on RepairLink, lost Category on review-approve, archived turns missing tool_calls, summarizer LLM cost when mode=off) are real production correctness issues but won't crash the tag — they should land as 12u.N atomic commits in the same window.

---

## CRITICAL — Block tag

### CR-01: Conversations panel mismatched response shape — UI permanently empty

- **Files:** [`web/src/components/ConversationsPanel.tsx`](web/src/components/ConversationsPanel.tsx#L30-L32), [`web/src/api.ts`](web/src/api.ts#L225-L233), [`internal/api/conversations.go`](internal/api/conversations.go#L34-L49)

The Go handler returns a bare array (`[]ConversationTurn`), but `api.ts` types the response as `{ turns: ConversationTurn[] }` and the panel does `const turns = data?.turns ?? []`. `data.turns` is always `undefined` because `data` is the array itself, so `turns` is always `[]` and the UI shows the empty state forever, regardless of how many rows live in the SQLite archive.

**Fix direction:** make the API client typing match the actual array response (drop the `{ turns: ... }` envelope) OR wrap the Go response in `{ turns: [...] }`. The array shape is consistent with the rest of the API (`/wiki/pages`, `/sources`, `/tasks`, `/skills`, `/summaries`, `/maintenance/issues` all return bare arrays), so the cheaper fix is on the TS side.

---

### CR-02: `/conversations` opens with HTTP 400 by default

- **Files:** [`internal/api/conversations.go`](internal/api/conversations.go#L17-L25), [`web/src/components/ConversationsPanel.tsx`](web/src/components/ConversationsPanel.tsx#L17-L28)

`handleConversationList` rejects requests with empty `chat_id` as `400 chat_id is required`. The panel initializes `chatId` to `''`, computes `numericChatId` as `undefined`, and calls `api.conversations(undefined, ...)` which omits the `chat_id` query param. Result: opening Conversations from the sidebar always fires a 400 and never lists anything until the user types a numeric chat ID. There's no way to discover chat IDs from inside the app.

**Fix direction:** make `chat_id` optional server-side (return all turns ordered by created_at DESC when omitted, paginated by `limit`). This also enables the "browse archive globally" flow the panel's filter UI implies. Keep the 400 for malformed (non-integer) values only.

---

## HIGH — Land as 12u.N before tag

### HR-01: `RepairLink` partial-commit leaves wiki inconsistent on mid-loop failure

- **File:** [`internal/wiki/store.go`](internal/wiki/store.go#L495-L519)

`RepairLink` iterates pages, mutates `page.Body`, calls `WritePage` (which commits to git), and bails on the first `WritePage` error with `return fmt.Errorf(...)`. If page 3 of 5 fails, pages 1 and 2 are already mutated, written, and committed; pages 4 and 5 still contain `[[broken]]`; and `AppendLog` at line 517 never runs so the audit trail is missing the auto-fix entry. `MaintenanceJob` then logs the error at the broken-link severity but the wiki is in an indeterminate state — the slug is partially renamed.

**Fix direction:** collect failures into a slice, continue the loop on per-page errors, append the log entry unconditionally, and return a multi-error (or summary error with counts) so the caller can report partial success. Alternatively wrap the whole pass in a git stash/restore pattern, but the multi-error path is cheaper.

**Severity rationale:** wiki is the canonical knowledge store; an inconsistent half-renamed link is harder to detect and fix than an all-or-nothing failure.

---

### HR-02: `Category` and `RelatedSlugs` lost when proposal is approved

- **Files:** [`internal/conversation/summarizer/applier.go`](internal/conversation/summarizer/applier.go#L141-L154), [`internal/api/summaries.go`](internal/api/summaries.go#L70-L86), [`internal/scheduler/store.go`](internal/scheduler/store.go#L58-L70)

`ReviewApplier.Apply` writes only `chat_id, fact, action, target_slug, similarity, source_turn_ids` to `proposed_updates` — it drops `Candidate.Category` and `Candidate.RelatedSlugs` entirely. On approval, `handleSummariesApprove` rebuilds a `Candidate` with `Category: "fact"` hardcoded (`internal/api/summaries.go:75`). So a "person" or "project" candidate that was scored at 0.92 in auto mode and would land in the right category is silently downgraded to "fact" if review mode is enabled. This subtly corrupts the wiki's category index over time.

**Fix direction:** add `category TEXT` and `related_slugs TEXT` (JSON) columns to `proposed_updates`, persist them in `ReviewApplier.Apply`, restore them on the approve path. Schema migration is straightforward (ALTER TABLE ADD COLUMN with default '').

---

### HR-03: Archived turns drop `tool_calls`, `tool_call_id`, and per-turn telemetry

- **Files:** [`internal/telegram/bot.go`](internal/telegram/bot.go#L937-L948), [`internal/conversation/archive.go`](internal/conversation/archive.go#L17-L33)

The `Turn` struct has fields for `ToolCalls`, `ToolCallID`, `LLMCalls`, `ToolCallsCount`, `ElapsedMS`, `TokensIn`, `TokensOut`. The schema has columns for all of them. The API DTO (`ConversationDetail`) has matching JSON fields. But the production write path at bot.go:941 only sets `ChatID, UserID, TurnIndex, Role, Content` — the assistant `ToolCalls` slice is never serialized to the DB, so the dashboard's tool-calls expansion (which is the headline feature of the drawer) shows nothing for any real conversation. The per-turn telemetry (`stats.llmCalls`, `stats.toolCalls`, `time.Since(turnStart).Milliseconds()`) is logged at line 962 but never persisted alongside the turn it belongs to.

**Fix direction:** in `handleConversation`, marshal `msg.ToolCalls` to JSON and pass `msg.ToolCallID` to the archive Append. The telemetry is harder because it's per-turn (not per-message); attach it to the assistant message at the end of the loop, or accept that telemetry only appears on the assistant role's row.

---

### HR-04: `turnMsgIdx` becomes stale if context summarization fires mid-turn

- **File:** [`internal/telegram/bot.go`](internal/telegram/bot.go#L879-L948)

`turnMsgIdx := convCtx.MessageCount()` is captured at line 880, then `convCtx.AddUserMessage()` runs, then `convCtx.EnforceLimit()` runs (line 904) which can call `Summarize` and trim the message slice. After trimming, `len(c.messages) < turnMsgIdx` is possible. `MessagesSince(turnMsgIdx)` correctly returns nil (per its bounds check) but the archive will then silently skip the entire turn — no user message archived, no assistant response archived. The `turn_index = turnMsgIdx + i` arithmetic is also wrong even when MessagesSince returns data, because the new messages occupy indices `[len(c.messages) - new_count, len(c.messages))`, not `[turnMsgIdx, turnMsgIdx + i)`.

**Fix direction:** capture the user message and assistant response into a local slice as they're produced (before any trim) rather than relying on snapshots of `c.messages`. Or use a monotonic per-chat turn counter from the DB (`MAX(turn_index) WHERE chat_id = ?`) so turn_index is always derived from the archive, not the in-memory context state.

**Severity rationale:** silent data loss on long conversations — exactly the workload the Phase 12 archive was built to capture.

---

### HR-05: Summarizer LLM scorer call fires even in `mode=off`, costing money silently

- **File:** [`internal/telegram/bot.go`](internal/telegram/bot.go#L304-L334), [`internal/conversation/summarizer/runner.go`](internal/conversation/summarizer/runner.go#L121-L148)

The default config is `SUMMARIZER_ENABLED=true` and `SUMMARIZER_MODE=off`. In off mode, `OffApplier.Apply` is a no-op — but `Runner.MaybeExtract` still calls `r.scorer.Score(ctx, recentTurns)` (line 122 of runner.go), which is a real LLM round-trip with the conversation pasted into the prompt. Every TurnInterval (default 5) turns, the user pays for an extra LLM call that produces a result that's then thrown away. On mid-tier OpenAI pricing this is non-trivial drift in cost vs the user's expectation of "off".

**Fix direction:** either short-circuit in `MaybeExtract` when the applier is `*OffApplier` (type-assert), OR (cleaner) treat off as "Runner not constructed" — set `b.summRunner = nil` when `cfg.SummarizerMode == "off"` and skip the scorer/dedup wiring entirely. The latter matches the intent of the off mode and is one switch arm in bot.go:313.

---

### HR-06: `dispatchWikiMaintenance` constructs a fresh `IssuesStore` every nightly run

- **File:** [`internal/telegram/bot.go`](internal/telegram/bot.go#L574-L582)

Each invocation of the nightly job calls `scheduler.NewIssuesStore(b.schedDB.DB())` to build a new store wrapper. The wrapper itself is cheap (just a `*sql.DB` reference) so this is not a leak per se, but `Deps.Issues` at bot.go:440 already holds an IssuesStore on the same DB. They are two separate wrappers around the same DB, both with the same idempotent enqueue contract. The duplication invites future drift if either side adds caching or stateful behavior. More importantly, if the IssuesStore ever grows internal state (e.g. a metrics counter), the dashboard's view will diverge from the maintenance job's view.

**Fix direction:** thread a single `*scheduler.IssuesStore` from bot.New into both `Deps.Issues` and `dispatchWikiMaintenance`. Store it on `*Bot` as `b.issuesStore`.

---

### HR-07: `IssuesStore.Resolve` swallows DB errors in the not-found check

- **File:** [`internal/scheduler/issues.go`](internal/scheduler/issues.go#L97-L115)

When `RowsAffected == 0`, the code does `s.db.QueryRowContext(...).Scan(&count)` with both errors ignored (line 109). If the SELECT fails (DB closed, permission, etc.), `count` stays at its zero value of 0 and the function returns `ErrIssueNotFound` — a wrong error that hides a real DB fault from the caller and from the API's 404 vs 500 branching.

**Fix direction:** `if err := s.db.QueryRowContext(...).Scan(&count); err != nil { return fmt.Errorf("issues resolve count check: %w", err) }`. Then the API handler's `errors.Is(err, ErrIssueNotFound)` branch only fires for genuine not-found.

---

## MEDIUM — Improvements

### MR-01: `Runner.MaybeExtract` loads up to 100,000 turns just to count them

- **File:** [`internal/conversation/summarizer/runner.go`](internal/conversation/summarizer/runner.go#L84-L91)

`r.archive.ListByChat(ctx, chatID, 100000)` is used solely so `len(turns) % r.cfg.TurnInterval` can be checked. On a long-lived chat, this materializes thousands of full Turn structs (with all their content) every time MaybeExtract fires. The next ListByChat at line 113 is the one that actually matters.

**Fix direction:** add `Count(ctx, chatID) (int64, error)` to `TurnArchive` (single `SELECT COUNT(*)`) and use it for the interval check.

### MR-02: `bot.go` constructs `SummariesStore` and `IssuesStore` even when their features are disabled

- **File:** [`internal/telegram/bot.go`](internal/telegram/bot.go#L437-L440)

`Deps.Summaries` and `Deps.Issues` are always set to non-nil stores. This means the dashboard's "review mode disabled" empty-state copy in `SummariesPanel.EmptyState` is misleading — the API does work, it just returns 0 rows. Since proposals can persist across mode changes (a row written in review mode stays after switching to off), the UX should not pretend the feature is disabled.

**Fix direction:** either keep the stores wired and rewrite the empty-state copy to "No pending proposals", or skip wiring when `cfg.SummarizerMode != "review"` and let the panel show the disabled message. The first option is cheaper.

### MR-03: `SetStatus` fetch-then-update is racy under concurrent approve

- **File:** [`internal/conversation/summarizer/proposals.go`](internal/conversation/summarizer/proposals.go#L94-L108)

Two concurrent approves on the same proposal both see `Status == "pending"` in `Get`, both UPDATE, and the second silently overwrites the first's audit trail (e.g. timestamp, if it gets added later). No 409 returned to the loser.

**Fix direction:** make the UPDATE conditional: `UPDATE ... SET status = ? WHERE id = ? AND status = 'pending'`, then check `RowsAffected`. If 0, look up the row to disambiguate not-found vs already-decided, exactly as `IssuesStore.Resolve` does (and exactly the pattern HR-07 says to firm up).

### MR-04: `handleSummariesApprove` continues after AutoApplier fails — silent partial state

- **File:** [`internal/api/summaries.go`](internal/api/summaries.go#L82-L86)

If `applier.Apply` fails (wiki write rejected, disk full, validation error post-fix-applier), the comment says "Don't block the status flip — log and continue". So the proposal flips to `approved` even though the wiki was never updated. Operator sees "approved" in the UI and assumes the fact landed.

**Fix direction:** either return 500 on apply failure (and don't flip status), or introduce a separate `apply_status` column to track the two state machines independently. The first option is correct for v1.

### MR-05: `BufferedAppender.Close` never times out

- **File:** [`internal/conversation/archive.go`](internal/conversation/archive.go#L201-L205)

`Close` calls `close(a.ch)` and `a.wg.Wait()` with no deadline. If the underlying SQLite is wedged (file lock from a long transaction), bot shutdown hangs indefinitely. The `ctx context.Context` parameter is currently a TODO. The doc says "ctx is reserved for future timeout support" — that future is now, given Phase 12 now has a wiki maintenance task that can hold the DB at unpredictable times.

**Fix direction:** `select { case <-done: ; case <-ctx.Done(): }` where `done` is signaled from the drain goroutine after wg.Wait. Bot.Stop should pass a context with a 5-10s deadline.

### MR-06: `BufferedAppender.Append` returns `nil` even when it drops

- **File:** [`internal/conversation/archive.go`](internal/conversation/archive.go#L189-L197)

The contract says non-blocking, which is correct, but `nil` masks the drop. Callers that wanted "best-effort" semantics get them transparently; callers that wanted to surface a metric have no signal beyond the warning log line. With Phase 12 introducing this hot path for the first time, the loss is invisible to operators except via grep.

**Fix direction:** add a `Stats() (enqueued, dropped uint64)` method on BufferedAppender (atomic counters), surface in /api/health alongside `embed_cache.hits/misses`. Optional, but it's the cheap monitoring improvement to land before Phase 13.

### MR-07: `dropLegacyConversations` swallows pragma error and proceeds

- **File:** [`internal/scheduler/store.go`](internal/scheduler/store.go#L168-L193)

If `PRAGMA table_info(conversations)` fails (DB locked, permission), the function returns nil — the caller treats it as "no legacy table", and then `conversationsSchemaSQL` runs, possibly hitting the same legacy table that `IF NOT EXISTS` short-circuits around. Net effect: the cleanup is conditionally skipped silently. The migration succeeds but the legacy table survives.

**Fix direction:** propagate the error from the PRAGMA call. The current code is a copy of "best effort cleanup" but the test in 12.cleanup specifically validates the legacy-table drop on real DBs — silent skipping defeats that.

### MR-08: `SummariesPanel` source-turn deep-link is a fragment that nothing reads

- **File:** [`web/src/components/SummariesPanel.tsx`](web/src/components/SummariesPanel.tsx#L108-L120)

`window.location.href = '/conversations#turn-${firstTurnId}'` navigates to the conversations route with a fragment, but `ConversationsPanel` never reads `window.location.hash` and never scrolls to / opens the matching turn. The link is decorative — clicking it just closes the modal and shows an empty conversations panel (per CR-02).

**Fix direction:** wire fragment → preselect via a `useEffect(() => { const id = parseInt(window.location.hash.slice('#turn-'.length)); if (!isNaN(id)) setSelectedId(id); }, [])`. Or drop the link until the receiving side is wired.

---

## LOW — Nits

### LR-01: `cmd/debug_summarizer/main.go` still includes the `patchingWikiWriter` workaround for a fixed bug

- **File:** [`cmd/debug_summarizer/main.go`](cmd/debug_summarizer/main.go#L7-L9)

Comments at the top of the file say AutoApplier doesn't set SchemaVersion/PromptVersion. Slice 12.fix-applier (commit d514c84) fixed exactly that. The workaround should be removed and the comment updated to reflect that the harness exercises the production path directly.

### LR-02: `archive.go` reimplements `strings.Contains`

- **File:** [`internal/conversation/archive.go`](internal/conversation/archive.go#L229-L240)

`contains` and `indexInString` are hand-rolled instead of using `strings.Contains`. No correctness issue but it's strange and worth deleting.

### LR-03: `Decision.TargetSlug` is empty for ActionNew but the comment says non-empty when ActionPatch

- **File:** [`internal/conversation/summarizer/types.go`](internal/conversation/summarizer/types.go#L24-L29)

Comment "non-empty when Action==ActionPatch" is true but reads as "empty otherwise". For ActionSkip the deduper fills TargetSlug too (top result), so the comment should be "non-empty for ActionPatch and ActionSkip; empty for ActionNew when no near match exists".

### LR-04: `CompoundingRate` JSON tag mismatch tolerated by `omitempty`

- **Files:** [`internal/api/types.go`](internal/api/types.go#L40-L44), [`web/src/types/api.ts`](web/src/types/api.ts#L26-L27)

Go side is non-pointer (always serialized), TS side is `compounding_rate?` (optional). They're not in conflict but the `?` is misleading — the field is always present in 12i+ builds. Drop the `?` for clarity.

### LR-05: `MaintenancePanel`'s `SEVERITY_COLOR.split(' ')[1]` is brittle

- **File:** [`web/src/components/MaintenancePanel.tsx`](web/src/components/MaintenancePanel.tsx#L97)

Header color is derived by `SEVERITY_COLOR[sev].split(' ')[1]` which depends on the second token of the colorClass string being the text-color class. If anyone reorders the tokens for a future contrast theme, the heading silently de-styles. Extract a separate `SEVERITY_HEADING_COLOR` map.

### LR-06: `parts < 4` gate in `computeCompoundingRate` is loose

- **File:** [`internal/api/health.go`](internal/api/health.go#L130-L137)

`strings.Split(line, "|")` always returns at least 1; a malformed row with 3 segments still passes `len(parts) < 4` (it doesn't), so the action lookup at `parts[2]` is safe but parsing wonky lines silently. Combined with the next `time.Parse` failure → continue, the loop tolerates anything. That's actually intentional and correct, but a comment would help future readers.

---

## Sign-off checklist

- [x] Concurrency primitives reviewed: BufferedAppender, Runner cooldown sync.Map, scheduler maintenance fan-out, owner-notifier — all sound.
- [x] SQL migrations idempotent and ordered — confirmed in scheduler/store.go and testdb.go.
- [x] Auth gate covers the 6 new endpoints — confirmed in router.go (single mux wrapped in RequireBearer).
- [x] No new public unauthenticated routes.
- [x] Frontend uses the existing api.ts client (no hardcoded `/api/...` URLs in the new panels).
- [x] No TODO/FIXME/XXX added in production paths beyond the BufferedAppender.Close ctx note (MR-05).
- [ ] `-race` not run on dev (Windows linker conflict — to verify on Linux CI before tag).
- [ ] CR-01 + CR-02 fixes — required before tag.

**Recommendation:** Land CR-01 and CR-02 as 12u.1 and 12u.2 atomic commits, run E2E against a populated archive to confirm Conversations panel works, then tag v0.12.0. The HIGH items can either land in the same window (preferred — they're all small) or roll into a 12u.cleanup pass tagged as v0.12.1. Do not tag with the CRITICALs open.

---

_Reviewed by Claude Opus 4.7 (1M context) — final senior pass before milestone tag._
