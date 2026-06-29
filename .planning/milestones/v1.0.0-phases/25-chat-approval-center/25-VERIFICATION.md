---
phase: 25-chat-approval-center
verified: 2026-06-17T00:00:00Z
status: closed-carried-forward
closed: 2026-06-18
closure_note: "Closed on 8/8 automated verification; the 6 live human-verification items are carried into the cockpit-overhaul live cutover (the overhaul reworked chat/footer/auth — see docs/cockpit-overhaul/). See 25-HUMAN-UAT.md."
score: 8/8 must-haves verified
overrides_applied: 0
human_verification:
  - test: "Live SSE streaming chat lane: operator types a prompt and watches streamed tokens appear incrementally"
    expected: "Each text chunk appears as it arrives; Stop button aborts the stream mid-turn; no full-page refresh"
    why_human: "Requires a running aura serve + browser; golden-replay E2E confirms the adapter wiring but only a live session proves end-to-end latency + incremental rendering feel"
  - test: "Reasoning drawer toggle persists preference across page reload"
    expected: "Closing the drawer, refreshing, re-opening a conversation — the drawer stays in the last-toggled state (localStorage)"
    why_human: "localStorage persistence behavior requires a browser session; unit test covers the pref write/read but not the cross-reload persistence in a real browser"
  - test: "Conversation branch navigation in-browser: edit a user turn, confirm a new branch appears, navigate Previous/Next on the BranchPicker"
    expected: "The BranchPicker shows '1 / 2' after an edit; Previous/Next cycle between branches; re-running from the selected branch streams a different assistant reply"
    why_human: "Full branch-tree user flow requires live DB (migration 0017 applied) + streaming backend + browser interaction; unit and db_integration tests cover the store seam separately, not the full UI loop"
  - test: "Inline approval card — trigger a real ask_user from a background-scheduled run, verify the badge increments, open the thread, answer/decline/cancel the card"
    expected: "Badge count increments within 5s (refetchInterval poll); navigating to the thread shows the inline card; resolving it causes the badge count to drop and the run resumes or is cancelled as appropriate"
    why_human: "Requires a live aura instance with a running scheduled/background job that issues ask_user; the entire APRV flow involves the runner, SSE, and the approval badge simultaneously — not exercisable by static code analysis or unit tests"
  - test: "Runtime footer shows non-zero tokens/cost during and after a live turn"
    expected: "While a turn is streaming, the footer updates with prompt_tokens + cost_usd from STATE_DELTA; after the turn completes, the session cumulative increments"
    why_human: "Requires a live SSE stream with real OpenRouter STATE_DELTA usage events; golden-replay E2E covers the rendering path but only with synthetic token counts"
  - test: "Context budget gauge microcompact marker appears after a context-rotation event"
    expected: "After the agent has compacted older turns, the gauge shows 'Compacted N older turns' inline at the correct context fill level"
    why_human: "Requires a conversation long enough to trigger the microcompact ladder (hard to create in a unit test) and a live rot-events read from the API"
  - test: "Playwright E2E against a live aura serve (non-golden-replay path)"
    expected: "npx playwright test chat.spec.ts passes against AURA_E2E_ORIGIN pointing at a real running instance with AURA_WEB_AUTH_SECRET unset; the spec asserts >= 1 streamed-token chunk from a real OpenRouter turn"
    why_human: "The golden-replay path is the deterministic CI path; the live-path branch (condition 1 in the spec) requires the actual served binary + OpenRouter connectivity; this is the full goal-backward proof"
---

# Phase 25: Chat + Approval Center Verification Report

**Phase Goal:** Chat + Approval Center — assistant-ui chat lane over SSE + conversation management + cost/cache footer + cross-thread HITL approval queue + conversation branch trees (CHAT-01..05, APRV-01..03)
**Verified:** 2026-06-17T00:00:00Z
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|---------|
| 1 | Operator types a prompt and watches streamed assistant response token-by-token over POST /agent/run (CHAT-01) | VERIFIED | `ExternalStoreChat.tsx` wires `useExternalStoreRuntime` over `streamRun`; `sseAdapter.ts` maps AG-UI frames to `ThreadMessageLike[]` parts; mounted in `AppShell.tsx` chatRegion; golden-fixture reducer test passes |
| 2 | Stop/interrupt aborts the active turn (CHAT-01) | VERIFIED | `onCancel` calls `abortRef.current?.abort()` and sets `isRunning=false`; the `ComposerPrimitive.Cancel` maps to this; the AbortController signal is forwarded to the fetch |
| 3 | REASONING_* deltas render in a collapsible reasoning drawer with persisted preference (CHAT-03 / D-01) | VERIFIED | `server.go:254` passes `showReasoning=true` to the cockpit Translate call; `ReasoningDrawer.tsx` renders the reasoning part; `reasoningPref.ts` handles localStorage persistence; Telegram still uses `ShowReasoning` config flag (not a blanket `true`) |
| 4 | Each tool call shows name + status + expandable raw text/JSON result blob (CHAT-03 / D-02) | VERIFIED | `ToolActivityCard.tsx` renders `toolName + status dot + expandable <pre>` blob; no `dangerouslySetInnerHTML` (grep count 0); no per-type typed routing (Phase 26 boundary held) |
| 5 | GET /api/conversations returns the conversation list (incl. archived when ?archived=true) behind RequireAuth; all seven conversation routes map to exactly one Store method; malformed id is a clean 404 (CHAT-02 backend) | VERIFIED | `conversations_api.go` 7 thin handlers; `parseConvID` uuid-guards every `{id}` (grep count 9); `sanitizeErr` wraps all errors (grep count 5); `conversations_branch_api.go` adds 3 D-09 routes on the same subtree; all mounted in `serve_webui.go` as specific `/api/conversations/` subtree (no bare `/api/`) |
| 6 | Operator browses, renames, archives, deletes conversations; FTS search opens thread at match (CHAT-02 frontend) | VERIFIED | `ConversationSidebar.tsx` (recent-first, archive-first), `DeleteConfirmDialog.tsx` (focus-trapped native dialog), `SearchPanel.tsx` (FTS snippet rows → navigate `/c/:id`); `useConversations.ts` fetches with `credentials: 'same-origin'` (grep count 3) |
| 7 | Runtime footer shows per-turn tokens + cache-hit % + cost + context gauge (CHAT-04 / D-10/D-11/D-12) | VERIFIED | `RuntimeFooter.tsx` uses `font-mono` (grep count 4); `/0` cache-% guard present; session seeds from `GET /api/conversations/{id}` aggregates then adds live deltas; `ContextBudgetGauge.tsx` reads `rot-events` (grep count 2 in gauge + useConversations) |
| 8 | Cross-thread pending badge + list (APRV-01 / D-04) | VERIFIED | `useApprovals.ts` polls with `refetchInterval` (grep count 2); `ApprovalBadge.tsx` renders count pill with `aria-live="polite"` (grep count 2); `ApprovalList.tsx` shows cross-thread rows with terminal state for D-06 |
| 9 | Accept/Decline/Cancel resolve over POST /api/approvals/{token}/resolve; decline keeps agent informed; cancel auto-resolves (APRV-02 / D-05) | VERIFIED | `approvals_api.go` calls `Runner.SubmitAnswers` (grep count 3); `ActionDecline` present (grep count 2); `SanitizeString` applied to questions (grep count 3); `InlineApprovalCard.tsx` sends `{action:'decline'}` without operator text (deny!=accept footgun guard explicit at call site); `ValidateRunInput` relaxed per CR-01 fix — empty-messages is now valid for continue-after-resume |
| 10 | Stale/auto-terminated approvals render their terminal state (APRV-03 / D-06) | VERIFIED | `ErrPauseNotFound → 404` in approvals_api.go; `InlineApprovalCard` renders terminal state with verbs disabled; `ApprovalList` renders expired rows with `text-warning` |
| 11 | Operator can edit/regenerate messages producing a navigable branch tree (CHAT-05 / D-09) | VERIFIED | Migration 0017 adds `parent_seq + branch_id` columns tx-safely with canonical backfill; `store_branch.go` has `ForkBranch` + `ListBranches`; `context.go` has `LoadManagedHistoryForBranch`; `conversations_branch_api.go` exposes branch REST (`GET /branches`, `POST /edit`, `POST /branches/{seq}/select`); `BranchPicker.tsx` binds `BranchPickerPrimitive` with `onEdit`/`onReload`; CHAT-05 recorded PRD-first in REQUIREMENTS.md (grep >= 2 hits) and ROADMAP.md (grep >= 1 hit) |
| 12 | messages[0] KV-cache stable-prefix invariant holds across branches (CHAT-05 / CAP-04) | VERIFIED | Cache-invariant audit exits 0 (22 identical hashes per context and 07-SUMMARY verification evidence); `LoadManagedHistoryForBranch` calls the plan-25-06 path loader, never re-implements the walk; Pitfall-3 rules verified in store_branch.go design |
| 13 | Phase-proving Playwright E2E: prompt -> stream -> resolve inline approval -> resume, footer updating; no silent skip under CI (CHAT-01 / APRV-02 / E2E gate) | VERIFIED (automated) | `chat.spec.ts`: `process.env.CI` check count=2 (>=1); `test.skip` count=0 (==0); `toBeGreaterThan` count=1 (>=1); `golden-events.json` reference count=4 (>=1); throws unconditionally when golden fixture missing (line 71-73); golden fixture exists at `internal/agui/testdata/golden-events.json` |

**Score:** 13/13 truths verified by static analysis; 7 items require human/live verification

### Code Review Findings — Resolved

| Finding | Severity | Fix Verified |
|---------|----------|-------------|
| CR-01: continue-after-resume re-drove empty-messages -> 400 before resume logic | BLOCKER | `ValidateRunInput` in `types.go` now requires only non-empty threadId OR non-empty Resume (empty Messages is valid for the resume path); confirmed at `types.go:64-69` |
| WR-01: handleSelectBranch accepted arbitrary numeric leaf without membership check | WARNING | `slices.ContainsFunc(branches, ...)` guard at `conversations_branch_api.go:145-148` verifies the leaf exists before re-running |
| WR-02: onReload/onEdit silently forked at seq 0 when parentId not found | WARNING | `if (parentIndex < 0) return;` guard at `ExternalStoreChat.tsx:229` |
| WR-03: streamPost swallowed HTTP error body — operator saw only "HTTP 400" | WARNING | `errorDetail()` function in `sseAdapter.ts:467-472` reads the sanitized body and surfaces it |

### Required Artifacts

| Artifact | Expected | Status | Details |
|---------|---------|--------|---------|
| `internal/agui/conversations_api.go` | 7 thin conversation handlers | VERIFIED | 212 LOC; uuid-guard + sanitizeErr; all 7 routes confirmed |
| `internal/agui/conversations_branch_api.go` | D-09 branch REST | VERIFIED | 201 LOC; WR-01 fix present |
| `internal/agui/approvals_api.go` | Cross-thread approval backend | VERIFIED | 165 LOC; SubmitAnswers + ActionDecline + SanitizeString |
| `internal/agui/types.go` | ValidateRunInput (CR-01 fix) | VERIFIED | Accepts empty Messages for continue-after-resume |
| `internal/conversations/store_branch.go` | ForkBranch + ListBranches | VERIFIED | 229 LOC; atomic tx fork; full-tree queryable |
| `internal/conversations/context.go` | LoadManagedHistoryForBranch | VERIFIED | Path-aware variant confirmed at context.go:199 |
| `internal/db/migrations/0017_*.up.sql` | Additive branch pointer columns | VERIFIED | 2 ALTERs + UPDATE backfill; no CONCURRENTLY in SQL (only in comment) |
| `internal/db/migrations/0017_*.down.sql` | Reversible migration | VERIFIED | Drops parent_seq + branch_id IF EXISTS |
| `cmd/aura/serve_webui.go` | Routes mounted without bare /api/ | VERIFIED | `conversationsRoutePrefix` + `conversationsListRoute` + `approvalsListRoute` + `approvalsResolveRoute`; RequireCapability on all mutating routes |
| `web/src/chat/ExternalStoreChat.tsx` | Runtime provider + onEdit/onReload/resumeNonce | VERIFIED | useExternalStoreRuntime; WR-02 fix; continue-after-resume fold |
| `web/src/chat/sseAdapter.ts` | POST-SSE reducer + streamPost with error body | VERIFIED | WR-03 errorDetail fix; Pitfall-2 tool_call_id guard |
| `web/src/chat/BranchPicker.tsx` | BranchPickerPrimitive bound to branch nav | VERIFIED | Previous/Next/Number/Count; accent aria-live; hideWhenSingleBranch |
| `web/src/chat/ReasoningDrawer.tsx` | Collapsible CoT with persisted preference | VERIFIED | reasoningPref.ts; aria-pressed/aria-expanded |
| `web/src/chat/ToolActivityCard.tsx` | Raw blob, no dangerouslySetInnerHTML | VERIFIED | grep count 0 confirmed |
| `web/src/chat/RuntimeFooter.tsx` | Tokens/Cache/Cost/Context cluster | VERIFIED | font-mono; /0 guard; session seed |
| `web/src/chat/ContextBudgetGauge.tsx` | Gauge with rot-events marker | VERIFIED | rot-events reference confirmed |
| `web/src/conversations/ConversationSidebar.tsx` | List + rename + archive + delete | VERIFIED | archive-first; DeleteConfirmDialog |
| `web/src/conversations/SearchPanel.tsx` | FTS rows open at match | VERIFIED | navigate `/c/:conversationId` |
| `web/src/conversations/useConversations.ts` | React Query hooks over /api/conversations | VERIFIED | same-origin; refetchInterval for search |
| `web/src/approvals/useApprovals.ts` | Poll + resolve mutation | VERIFIED | refetchInterval count 2; deny!=accept contract |
| `web/src/approvals/ApprovalBadge.tsx` | Accent pill + polite aria-live | VERIFIED | aria-live="polite" count 2 |
| `web/src/approvals/ApprovalList.tsx` | Cross-thread list + terminal D-06 state | VERIFIED | D-06 rows rendered in warning color |
| `web/src/approvals/InlineApprovalCard.tsx` | Answer/Decline/Cancel + terminal + footgun guard | VERIFIED | deny!=accept explicit at submit() call site |
| `web/e2e/chat.spec.ts` | Playwright E2E with no-skip-as-green | VERIFIED | process.env.CI guard; test.skip count 0; toBeGreaterThan assertion; golden fixture exists |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `conversations_api.go` | `conversations.Store` | `s.conv.List/Get/Search/Rename/UpdateStatus/Delete/ListContextRotEvents` | WIRED | Each handler calls exactly one store method |
| `approvals_api.go` | `runner.SubmitAnswers` | single-entry `map[string]runner.ResponseInput{token: {Action}}` | WIRED | SubmitAnswers grep count 3 |
| `ExternalStoreChat.tsx` | `POST /agent/run` | `streamRun()` with AG-UI RunAgentInput body | WIRED | sseAdapter.ts:489 |
| `ExternalStoreChat.tsx` | `POST /api/conversations/{id}/edit` | `foldReRun()` via onEdit/onReload | WIRED | ExternalStoreChat.tsx:210 |
| `AppShell.tsx` | `ExternalStoreChat` | chatRegion section mount | WIRED | grep count 2 in AppShell.tsx |
| `useConversations.ts` | `/api/conversations` | React Query useQuery/useMutation same-origin | WIRED | same-origin grep count 3 |
| `useApprovals.ts` | `/api/approvals` | React Query refetchInterval poll | WIRED | refetchInterval grep count 2 |
| `conversations_branch_api.go` | `store_branch.ForkBranch/ListBranches` | ConversationStore interface methods | WIRED | interface widened in types.go |
| `runner.TurnBranch` | `conversations.LoadManagedHistoryForBranch` | branchLeaf dispatch in runner.go | WIRED | runner.go:312; interfaces.go widened |
| `serve_webui.go` | branch routes | `RequireCapability(agentRunCapability)` | WIRED | branchEditRoute + branchSelectRoute capability-gated |
| `serve_webui.go` | `POST /api/approvals/{token}/resolve` | `RequireCapability(agentRunCapability)` | WIRED | approvalsResolveRoute capability-gated |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|---------|--------------|--------|-------------------|--------|
| `ExternalStoreChat.tsx` | `messages[]` | `streamRun()` → sseAdapter reducer → AG-UI SSE from backend | Yes — real fetch against `/agent/run` | FLOWING |
| `RuntimeFooter.tsx` | `usage` | `onUsage` seam from ExternalStoreChat → `usageFromStateDelta` on live STATE_DELTA | Yes — live SSE usage events | FLOWING |
| `RuntimeFooter.tsx` | `conv` (session seed) | `useConversation(conversationId)` → `GET /api/conversations/{id}` | Yes — real DB aggregates | FLOWING |
| `ContextBudgetGauge.tsx` | rot-events marker | `useRotEvents(conversationId)` → `GET /api/conversations/{id}/rot-events` | Yes — real DB via ListContextRotEvents | FLOWING |
| `useApprovals.ts` | `data` (approval list) | `GET /api/approvals` → `ListPendingAll` → DB | Yes — real DB query | FLOWING |
| `ConversationSidebar.tsx` | `conversations` | `useConversations()` → `GET /api/conversations` → `Store.List` | Yes — real DB | FLOWING |

### Behavioral Spot-Checks

| Behavior | Evidence | Status |
|---------|---------|--------|
| No bare mux.Handle("/api/") in serve_webui.go | All registrations use named constants `conversationsRoutePrefix`/`approvalsListRoute`; grep for bare /api/ returns 0 matches in non-comment lines | PASS |
| cache_invariant_audit.sh exits 0 (22 hashes) | Per context briefing: "cache-invariant gate exits 0"; confirmed in 07-SUMMARY verification evidence; script exists at scripts/cache_invariant_audit.sh | PASS |
| Playwright E2E no-test.skip guard | test.skip count=0; throws when golden missing (line 71-73); CI guard throws when no live + no golden | PASS |
| ExternalStoreChat mounts in AppShell | grep confirms import + JSX usage in AppShell.tsx (2 occurrences) | PASS |
| Telegram reasoning posture unchanged | `Translate(.*, true)` count=0 in agui_subscriber.go; `Translate(.*ShowReasoning)` count=1 | PASS |
| ValidateRunInput allows empty Messages for resume (CR-01) | types.go:64-69: only checks ThreadID=="" as error; empty Messages is not checked | PASS |
| WR-01 branch leaf membership gate | slices.ContainsFunc(...b.LeafSeq==leaf...) in conversations_branch_api.go:145 | PASS |
| WR-02 onReload unknown parent guard | `if (parentIndex < 0) return;` at ExternalStoreChat.tsx:229 | PASS |
| WR-03 error body surfaced | `errorDetail()` reads res.text() before falling back to "HTTP NNN" | PASS |

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|------------|---------------|-------------|--------|---------|
| CHAT-01 | 25-03, 25-07 | Operator sends prompt + watches streamed reply | SATISFIED | ExternalStoreChat + sseAdapter + Playwright E2E |
| CHAT-02 | 25-01, 25-04 | Browse/FTS/rename/archive/delete conversations | SATISFIED | conversations_api.go (7 routes) + ConversationSidebar + SearchPanel |
| CHAT-03 | 25-01, 25-03 | Reasoning drawer + tool activity, showReasoning policy | SATISFIED | server.go:254 `true` flip + ReasoningDrawer + ToolActivityCard |
| CHAT-04 | 25-04 | Per-turn cost/cache footer | SATISFIED | RuntimeFooter + ContextBudgetGauge + footerMetrics |
| CHAT-05 | 25-06, 25-07 | Branch tree (edit/regenerate) + KV-cache invariant | SATISFIED | Migration 0017 + store_branch.go + LoadManagedHistoryForBranch + BranchPicker; cache gate green |
| APRV-01 | 25-02, 25-05 | Cross-thread pending list with question/options/priority/source | SATISFIED | ListPendingAll (token ASC tiebreaker) + GET /api/approvals + ApprovalBadge + ApprovalList |
| APRV-02 | 25-02, 25-05 | Accept/Decline/Cancel + resume (deny!=accept guard) | SATISFIED | approvals_api.go SubmitAnswers + InlineApprovalCard; CR-01 fix; continue-after-resume fold |
| APRV-03 | 25-02, 25-05 | Stale/auto-terminated terminal state (no silent loss) | SATISFIED | ErrPauseNotFound->404; D-06 terminal rendering in InlineApprovalCard + ApprovalList |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|---------|--------|
| `web/src/i18n/resources.ts` | ~71-75 / ~283-287 | Dead `chat.branch.label` i18n key defined in both bundles but never referenced by BranchPicker | Info | Zero functional impact; BranchPicker uses Number/Count primitives directly; cleanup in a future pass |
| `internal/agui/server.go` | 549-556 | `redactEvent` mutates the shared RunErrorEvent in place (in-place `.Message=` on a pointer) | Info | Harmless in current single-consumer SSE pump; would corrupt a future fanout/multi-subscriber path; no behavioral regression today |
| `web/src/chat/ExternalStoreChat.tsx` | 192-196 | `divergeSeqAt(index) = index + 2` is an untested implicit seq-layout coupling (assumes no gaps, no branch offsets in the visible list) | Info | Correct for linear sequences; fragile if/when a branch switch re-numbers the visible list; noted in codebase comment |
| `web/e2e/chat.spec.ts` | 84-90 | Docstring above `goldenDelta` reads "firstTurnFrames assembles..." — comment belongs to `firstTurnFrames` two functions later | Info | Harmless misleading comment |

No TBD/FIXME/XXX debt markers found in the phase-modified files.

### Human Verification Required

### 1. Live SSE Streaming Chat

**Test:** Start `aura serve` (with AURA_WEB_AUTH_SECRET unset for unauthenticated local access), open the cockpit, type a prompt and observe the streaming response
**Expected:** Text tokens appear incrementally as they arrive; the Stop button aborts mid-stream; the running-status row shows while streaming
**Why human:** Incremental rendering feel and Stop timing require a live browser + OpenRouter connection; unit tests cover the reducer but not the visible streaming cadence

### 2. Reasoning Drawer Persistence

**Test:** Open the cockpit, send a prompt, toggle the reasoning drawer closed, refresh the page, send another prompt
**Expected:** The drawer stays closed after page reload (localStorage preference persisted by reasoningPref.ts)
**Why human:** Cross-reload localStorage behavior requires a real browser session; cannot be verified by static analysis or jsdom unit tests

### 3. Branch Tree Navigation (Full User Flow)

**Test:** Send a prompt, click Edit on a user message, type a new version and save, observe the BranchPicker appear showing "1 / 2", navigate with Previous/Next
**Expected:** BranchPicker appears only after the edit; Previous/Next cycle between the original and edited branch; the chat lane shows different assistant replies per branch
**Why human:** Full edit-fork-navigate loop requires live DB (migration 0017), live streaming backend, and browser interaction; unit tests cover ForkBranch and ListBranches in isolation

### 4. Cross-Thread HITL Approval Full Flow

**Test:** Schedule or trigger a background agent run that issues an `ask_user` pause; watch the header badge increment; click the badge, open the ApprovalList, click Open to jump to the thread; answer/decline/cancel the inline card; observe the run resuming or aborting
**Expected:** Badge count increments within 5s (refetchInterval=5000ms); jumping to the thread shows the InlineApprovalCard with the verbatim question; resolving via Answer causes the resumed turn to render; Decline shows the "agent will continue, informed" helper; Cancel aborts and auto-resolves
**Why human:** Requires a live runner in the paused state, the approval API, the badge poll, and the inline card simultaneously — no automated test exercises this full cross-component flow

### 5. Runtime Footer Live Update

**Test:** Send a live prompt to a real OpenRouter endpoint; observe the footer updating during and after the turn
**Expected:** Tokens·Cache·Cost values update while streaming from STATE_DELTA events; the Context gauge fills as prompt_tokens grows; session-cumulative increments after each turn; no NaN values
**Why human:** Requires real OpenRouter STATE_DELTA usage events; the golden-replay E2E uses synthetic token values (120/18 prompt/completion)

### 6. Playwright E2E Against Live Stack (Non-Golden Path)

**Test:** Provision a live `aura serve` (unauthenticated, separate port); run `AURA_E2E_ORIGIN=http://localhost:PORT npx playwright test chat.spec.ts`
**Expected:** The spec takes the live-path branch (condition 1 in beforeAll), streams real tokens from OpenRouter, triggers a real ask_user, resolves it inline, footer shows real cost values; toBeGreaterThan(0) assertion executes against real token chunks
**Why human:** The deterministic CI golden-replay path is fully automated; the live branch exercises real OpenRouter connectivity + the full backend stack; this is the definitive goal-backward proof

### Gaps Summary

No gaps remain. All 8 CHAT-01..05 + APRV-01..03 must-haves are verified in the codebase. The 4 code-review findings (CR-01 BLOCKER + WR-01/02/03 WARNINGs) are confirmed fixed in the actual source files. The phase is structurally complete and correct; the 6 human verification items above are live-stack / browser behavioral tests that cannot be asserted without a running system.

---

_Verified: 2026-06-17T00:00:00Z_
_Verifier: Claude (gsd-verifier)_
