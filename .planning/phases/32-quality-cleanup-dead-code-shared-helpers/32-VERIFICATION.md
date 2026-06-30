---
phase: 32-quality-cleanup-dead-code-shared-helpers
verified: 2026-06-30T12:00:00Z
status: human_needed
score: 21/21 must-haves verified
overrides_applied: 0
human_verification:
  - test: "Keyboard tab-cycling in BoardLayout bottom-sheet and McpLifecycleCluster RemoveDialog after the canonical focusTrap adoption"
    expected: "Tab advances through all focusable elements (inputs, links, [tabindex]), not just <button>; focus does not escape the modal; disabled elements are skipped."
    why_human: "McpLifecycleCluster switched from a button-only querySelector to the full isFocusable selector. Playwright confirmed structural rendering but keyboard navigation flow requires a human to tab through the live cockpit dialog."
  - test: "Skeleton visual appearance in ConversationSidebar, SearchPanel, and governanceView loading states"
    expected: "Loading pulse animation is visible and matches the rich Skeleton.tsx CSS-wave system; no layout shift or missing width/height sizing vs the old shadcn animate-pulse skeletons."
    why_human: "The three consumers migrated from shadcn className props to SkeletonBlock h/w/radius props. Playwright confirmed the .skeleton-block marker, but the visual appearance of the CSS-wave token vs animate-pulse is a human judgment."
---

# Phase 32: Quality Cleanup — Dead Code + Shared Helpers Verification Report

**Phase Goal:** Kill cross-package duplication + dead code BEFORE feature phases build on them, so later work reuses clean shared packages.
**Requirements:** QUAL-02, QUAL-03, QUAL-05
**Verified:** 2026-06-30
**Status:** human_needed
**Re-verification:** No — initial verification

---

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| SC-1a | `AURA_MEMORY_EMBED_*` keys removed from `settings.AllowedKeys` | VERIFIED | `rg 'AURA_MEMORY_EMBED' internal/settings/settings.go` → 0 production matches; test file confirms removal |
| SC-1b | `agui.indexByte` and `agui.stringList` removed | VERIFIED | `rg 'func indexByte\|func stringList' internal/agui/governance_api.go` → no matches |
| SC-1c | Telebot blank import removed from `internal/channels/deps.go` | VERIFIED | File does not exist; `internal/channels/doc.go` carries the package doc; telegram/bot.go keeps go.mod DIRECT |
| SC-1d | `RequestID` re-stamp confirmed load-bearing (NOT a dead re-stamp) | VERIFIED | `agent.go:134` line still present; `TestDryRun_EveryEventCarriesRequestID_LoadBearing` pins it with RED-on-removal proof |
| SC-1e | `assets.Status{Created,Embedding,Canceled}` — kept with deferred-lifecycle annotation (D-04 operator decision) | VERIFIED (deviation noted) | Constants at `types.go:26,32,37`; annotation at `types.go:14-22` documents designed-but-deferred pipeline; operator signed off over deletion to avoid delete-and-re-add churn when phases 33+ wire it |
| SC-2a | `internal/neostore` exports `HashText`/`AsString`/`AsFloats`/`GraphClient` | VERIFIED | `neostore/neostore.go:33,41,55,25`; parity tests at `neostore_test.go:22,59,83` |
| SC-2b | `reasoningstore`/`toolselectstore`/`activelearn` use `neostore.*`; old copies deleted | VERIFIED | Old `hashText`/`hashQuery`/`asString`/`asFloats` → 0 matches in those packages; neostore.* calls confirmed |
| SC-2c | `internal/pgnumeric` exports `NumericFromFloat`/`FloatFromNumeric`/`DefaultNumericMaxCost` | VERIFIED | `pgnumeric/pgnumeric.go:39,54`; parity at `pgnumeric_test.go:17,66,88`; deviation from plan's `internal/db` documented (import-cycle avoidance) |
| SC-2d | `conversations`/`cachemetrics` use `pgnumeric.*`; old copies deleted | VERIFIED | `rg 'func numericFromFloat\|func floatFromNumeric' internal/conversations internal/cachemetrics` → 0 matches; pgnumeric.* calls confirmed |
| SC-2e | `internal/envutil` exports `IntDefault`/`BoolDefault`; 3 copies deleted | VERIFIED | `envutil/envutil.go:22,37`; `rg 'func envIntDefault\|func envBoolDefault' internal/config internal/channels` → 0 matches; config.go has 35+ `envutil.*` calls |
| SC-2f | `canonicaljson.CanonicalArgs` is the single canonicalizer; agent copies deleted | VERIFIED | `canonicaljson/canonicaljson.go:50`; `rg 'func canonicalArgs\|func canonArgs' internal/agent/` → 0 matches; `TestCanonicalArgs` at line 19 |
| SC-2g | `isTransientNetworkErr` extracted to `llm_agent_retry.go`; `isTransientToolErr` widens to it; stream path stays strict | VERIFIED | `llm_agent_retry.go:65`; `TestIsTransientNetworkErr` and `TestIsTransientToolErr_WidenedNetworkSubset` confirm asymmetric semantics |
| SC-2h | `internal/agentrender` exports all 6 primitives; `chat_render.go`/`capture_cot_eval.go` use it; copies deleted | VERIFIED | `agentrender.go:28,48,55,69,90,112`; `agentrender.*` in both call sites; `rg 'func (flushRemainder\|usageFromStateDelta\|anyInt...)' cmd/aura/chat_render.go internal/eval/` → 0 matches |
| SC-2i | `agentrender` has no `internal/agui` import (boundary guard) | VERIFIED | Comment at `agentrender.go:10` explicitly documents boundary; no `internal/agui` import in file |
| SC-2j | Web `getJSON` has single source; copies in `useConversations.ts`/`governanceApi.ts` deleted | VERIFIED | `rg 'function getJSON\|const getJSON' web/src/conversations/useConversations.ts web/src/governance/governanceApi.ts` → 0 matches; both import `from '../api/json'` |
| SC-2k | `BoardLayout` and `McpLifecycleCluster` use canonical `focusTrap.ts` | VERIFIED | `BoardLayout.tsx:3,58,66` imports `focusFirstDescendant`/`trapTabKey`; `McpLifecycleCluster.tsx:5,220` imports `trapTabKey` |
| SC-2l | `web/src/components/ui/skeleton.tsx` retired; 3 consumers migrated | VERIFIED | File does not exist; `rg 'components/ui/skeleton' web/src/` → 0 matches |
| SC-3a | `internal/web/throttle_test.go` covers acquire/release/ctx-cancel/per-host/race with goleak | VERIFIED | File at `internal/web/throttle_test.go`; references `perHostLimit`, ctx-cancel no-op-release guard, per-host isolation, concurrent race test |
| SC-3b | Setup `TestEventsInvalidateTokenBeforeSSEWrite` pins ordering regression | VERIFIED | `internal/setup/handlers_test.go:241`; asserts token still-valid-at-first-write fails the test |
| SC-3c | `ensureAuthulaSearchPath` table in `authula_test.go` | VERIFIED | `internal/webauth/authula_test.go:16,51` covers empty/malformed/append/idempotent |
| SC-3d | Telegram `TestAnswersFromTextKeywordFallback` covers Italian-keyword fallback | VERIFIED | `profile_onboarding_test.go:306`; 5 cases including zero-value no-match |
| SC-3e | `TestTruncateTailBytes` UTF-8 boundary table | VERIFIED | `llm_agent_completion_internal_test.go:163`; 7 cases including mid-rune walk-back; `utf8.ValidString` asserted |
| SC-3f | `memory_integration` CI leg verified already-live (no-skip-as-green) | VERIFIED | `ci.yml:606-719` sets `CI:"true"`, exports `AURA_AGENT_MEMORY_MCP_URL`, runs `-tags memory_integration`; `memory_recall_integration_test.go:52` `t.Fatal` under `$CI` |

**Score:** 21/21 truths verified

---

### Notable Deviation: `assets.Status{Created,Embedding,Canceled}` (SC-1e)

The ROADMAP SC-1 says these should be "removed." The operator gate (plan 32-01, Task 1) found zero consumers but concluded these are part of a designed 12-state pipeline documented in `docs/superpowers/plans/2026-06-18-industrial-multimodal-asset-pipeline.md`. The operator chose **keep-annotate** over **delete-3-named** to avoid deleting designed work that phases 33+ will wire. The annotation is present at `internal/assets/types.go:14-22`.

This is the correct outcome of the QUAL-02 process ("each confirmed via deadcode/rg before removal" — the confirmation revealed they are designed, not dead). The spirit of QUAL-02 is met. The deviation is operator-signed-off.

### Notable Deviation: `internal/pgnumeric` vs `internal/db/numeric.go` (SC-2c)

Plan 32-05 targeted `internal/db/numeric.go`. During execution, `go vet -tags db_integration` revealed a test cycle (`db_integration_test.go` is `package db` and imports `internal/cachemetrics`; making `cachemetrics` import `internal/db` yields `db↔cachemetrics`). The executor created `internal/pgnumeric` as a cycle-free leaf. Architecture change is documented in 32-05-SUMMARY, verified functional.

---

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/neostore/neostore.go` | HashText/AsString/AsFloats/GraphClient | VERIFIED | All 4 exports present |
| `internal/neostore/neostore_test.go` | Parity/characterization tests | VERIFIED | 3 test functions covering full union of inputs |
| `internal/pgnumeric/pgnumeric.go` | NumericFromFloat/FloatFromNumeric/DefaultNumericMaxCost | VERIFIED | All 3 exports present |
| `internal/pgnumeric/pgnumeric_test.go` | Parity tests asserting Int+Exp+err-presence (not error string) | VERIFIED | 3 test functions |
| `internal/envutil/envutil.go` | IntDefault/BoolDefault | VERIFIED | Both exports at lines 22,37 |
| `internal/envutil/envutil_test.go` | t.Setenv parity table | VERIFIED | TestIntDefault, TestBoolDefault |
| `internal/canonicaljson/canonicaljson.go` | CanonicalArgs | VERIFIED | Line 50 |
| `internal/agent/llm_agent_retry.go` | `isTransientNetworkErr`; widened `isTransientToolErr` | VERIFIED | Line 65 |
| `internal/agentrender/agentrender.go` | 6 exports: FlushRemainder/IsToolResultPreview/IsTerminalToolCall/UsageFromStateDelta/AnyInt/AnyFloat | VERIFIED | All 6 at lines 28,48,55,69,90,112 |
| `internal/agentrender/agentrender_test.go` | Parity table documenting eval json.Number fix | VERIFIED | 6 test functions |
| `internal/web/throttle_test.go` | acquire/release/ctx-cancel/per-host/race + goleak | VERIFIED | All cases present |
| `internal/setup/handlers_test.go` | InvalidateToken-before-SSE ordering regression | VERIFIED | `TestEventsInvalidateTokenBeforeSSEWrite` at line 241 |
| `internal/webauth/authula_test.go` | ensureAuthulaSearchPath 4-case table | VERIFIED | Lines 16,51 |
| `internal/channels/telegram/profile_onboarding_test.go` | answersFromText keyword fallback | VERIFIED | `TestAnswersFromTextKeywordFallback` at line 306 |
| `internal/agent/llm_agent_completion_internal_test.go` | truncateTailBytes UTF-8 boundary table | VERIFIED | `TestTruncateTailBytes` at line 163 |

---

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `internal/reasoningstore/store.go` | `internal/neostore` | `neostore.HashText`/`AsString`/`AsFloats`/`GraphClient` | WIRED | 4 call sites verified |
| `internal/toolselectstore/store.go` | `internal/neostore` | `neostore.HashText`/`AsString`/`AsFloats`/`GraphClient` | WIRED | 5 call sites verified |
| `internal/activelearn/learner.go` | `internal/neostore` | `neostore.HashText` | WIRED | 2 call sites at lines 66,90 |
| `internal/conversations/store_append.go` | `internal/pgnumeric` | `pgnumeric.NumericFromFloat` | WIRED | Line 176 |
| `internal/cachemetrics/store_helpers.go` | `internal/pgnumeric` | `pgnumeric.NumericFromFloat`/`FloatFromNumeric` | WIRED | Lines 33,79,105 |
| `internal/config/config.go` | `internal/envutil` | 35+ `envutil.IntDefault`/`BoolDefault` call sites | WIRED | Lines 362-466 |
| `internal/channels/telegram/config.go` | `internal/envutil` | `envutil.IntDefault` | WIRED | Copy deleted, calls canonical |
| `cmd/aura/chat_render.go` | `internal/agentrender` | `agentrender.IsTerminalToolCall`/`FlushRemainder`/`UsageFromStateDelta`/`IsToolResultPreview` | WIRED | Lines 66,79,80,81 |
| `internal/eval/capture_cot_eval.go` | `internal/agentrender` | `agentrender.*` | WIRED | Lines 115,125,129,134 |
| `internal/agent/llm_agent_args.go` | `internal/canonicaljson` | `canonicaljson.CanonicalArgs` | WIRED | Local copy deleted, calls canonical |
| `internal/agent/workflow/loop.go` | `internal/canonicaljson` | `canonicaljson.CanonicalArgs` | WIRED | Local copy deleted |
| `web/src/conversations/useConversations.ts` | `web/src/api/json.ts` | `import { getJSON, postJSON }` | WIRED | Line 2 |
| `web/src/governance/governanceApi.ts` | `web/src/api/json.ts` | `import { getJSON }` | WIRED | Line 15 |
| `web/src/governance/BoardLayout.tsx` | `web/src/a11y/focusTrap.ts` | `focusFirstDescendant`/`trapTabKey` | WIRED | Lines 3,58,66 |
| `web/src/governance/McpLifecycleCluster.tsx` | `web/src/a11y/focusTrap.ts` | `trapTabKey` | WIRED | Lines 5,220 |

---

### Behavioral Spot-Checks

Step 7b: The orchestrator confirmed prior to this verification that `go build ./...` exits 0 and that the following test packages pass under the race detector: `internal/canonicaljson`, `internal/agentrender`, `internal/agent`, `internal/agent/workflow`, `internal/web`, `internal/setup`, `internal/webauth`, `internal/channels/telegram`, `cmd/aura`. Web gates (vitest 922 pass, lint/knip/build 0) are also confirmed green. These verifications are treated as already-run.

---

### Requirements Coverage

| Requirement | Plans | Description | Status | Evidence |
|-------------|-------|-------------|--------|----------|
| QUAL-02 | 32-01, 32-02, 32-03, 32-04 | Dead exports / reinvented-stdlib / placeholders removed | SATISFIED | See SC-1a through SC-1e above; all 5 named items resolved with evidence |
| QUAL-03 | 32-05, 32-06, 32-07, 32-08 | Shared helper extractions with parity tests | SATISFIED | See SC-2a through SC-2l; all extractions exist on disk with wired call sites and parity tests |
| QUAL-05 | 32-09, 32-10 | Targeted test gaps closed | SATISFIED | See SC-3a through SC-3f; all 5 named gaps closed plus memory_integration CI verification |

---

### Anti-Patterns Found

None found in the files modified by this phase. No TBD/FIXME/XXX debt markers in the new packages (`neostore`, `pgnumeric`, `envutil`, `agentrender`) or modified files.

---

### Human Verification Required

#### 1. focusTrap Keyboard Navigation in Live Cockpit

**Test:** Open the cockpit, trigger the BoardLayout bottom-sheet panel (e.g. Settings or a right-side panel) and the McpLifecycleCluster RemoveDialog for an MCP server. Tab through the dialog with a keyboard.
**Expected:** Tab cycles through all focusable elements (not just `<button>` — should also reach `<input>`, `<a>`, and `[tabindex]` elements); disabled elements are skipped; Tab does not escape the modal; Shift+Tab works in reverse.
**Why human:** McpLifecycleCluster's inline trap previously queried only `button`; the canonical `trapTabKey` uses the full `isFocusable` selector. Playwright confirmed the structural `.focusTrap` rendering but did not exercise keyboard tabbing through the expanded focusable set.

#### 2. Skeleton Visual Appearance

**Test:** Open the cockpit while data is loading (e.g. switch to the Conversations view on a slow connection or clear IndexedDB cache so the conversation list shows loading skeletons; similarly for the Search panel and Governance view).
**Expected:** Loading skeletons appear with visible animation (CSS wave, not the old Tailwind `animate-pulse`), correct sizing proportional to the content placeholders, and no layout shift compared to the content when it loads.
**Why human:** Three consumers (`ConversationSidebar`, `SearchPanel`, `governanceView`) migrated from shadcn `className` props to the rich `SkeletonBlock` `h`/`w`/`radius` props. The Playwright test confirmed `.skeleton-block` CSS class presence but visual sizing/animation requires human judgment.

---

### Gaps Summary

No BLOCKER gaps. All 21 programmatic must-haves are verified in the codebase. Two human verification items remain (keyboard A11y and visual skeleton appearance) which are standard pre-ship checks for UI changes.

**Phase goal assessment:** "Kill cross-package duplication + dead code BEFORE feature phases build on them" — ACHIEVED. All duplicated helpers have canonical homes. All call sites migrated. All old copies deleted. All test gaps closed. The codebase is materially cleaner.

---

_Verified: 2026-06-30_
_Verifier: Claude (gsd-verifier)_
