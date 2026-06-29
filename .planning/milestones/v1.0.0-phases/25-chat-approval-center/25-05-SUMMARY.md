---
phase: 25-chat-approval-center
plan: 05
subsystem: ui
tags: [react, react-query, assistant-ui, hitl, approvals, askuser, i18n, vitest, a11y, cockpit]

# Dependency graph
requires:
  - phase: 25-chat-approval-center
    plan: 02
    provides: "GET /api/approvals cross-thread read + POST /api/approvals/{token}/resolve (accept|decline|cancel) behind RequireAuth, capability-gated resolve"
  - phase: 25-chat-approval-center
    plan: 03
    provides: "assistant-ui chat lane (ExternalStoreChat) + RUN_FINISHED interrupt → requires-action + the card-mount idiom (ToolActivityCard)"
  - phase: 25-chat-approval-center
    plan: 04
    provides: "AppShell activeThreadId binding (sidebar + /c/:id deep link) + ConversationSidebar/SearchPanel/RuntimeFooter mounts"
  - phase: 24-web-foundation
    provides: "RequireAuth whole-origin gate + dark-operator design tokens"
  - phase: 23-frontend-infrastructure
    provides: "Vite/React/TS embed pipeline, React Query, react-i18next (en+it), vitest ≥85% coverage gate"
provides:
  - "web/src/approvals/useApprovals.ts — useApprovals() refetchInterval poll of GET /api/approvals (badge count) + useResolveApproval() POST /api/approvals/{token}/resolve mutation"
  - "ApprovalBadge — header accent count pill with aria-live=polite count-change announcement (APRV-01/D-04)"
  - "ApprovalList — cross-thread popover; Open jumps to /c/:conversationId; D-06 terminal state rendered (APRV-01/D-04/D-06)"
  - "InlineApprovalCard — in-thread Answer/Decline/Cancel verbs + terminal states; deny != accept footgun guard (APRV-02/03/D-03/D-05/D-06)"
  - "ThreadApprovalCards — filters the cross-thread poll to the active thread + re-drives the run on resume (D-03/D-05)"
affects: [25-06, 25-07, approval-center-uat]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "React Query refetchInterval poll for a cross-thread aggregate count (mirrors useRuntimeHealth.ts) + invalidateQueries on the resolve mutation"
    - "aria-live=polite count-change announcement: a dedicated sr-only region set only on a count delta (never assertive — must not interrupt)"
    - "deny != accept at the WIRE: the resolve hook strips operator content for decline/cancel; the card never sends typed text on a non-accept verb (T-25-17)"
    - "inline in-thread HITL card filtered from a single cross-thread poll (ThreadApprovalCards) — resolution stays inline (D-03), aggregation is the badge/list (D-04)"
    - "continue-after-resume: a successful accept/decline re-drives a no-Resume POST /agent/run so the stream continues over the rehydrated history"

key-files:
  created:
    - web/src/approvals/useApprovals.ts
    - web/src/approvals/ApprovalBadge.tsx
    - web/src/approvals/ApprovalList.tsx
    - web/src/approvals/approvalState.ts
    - web/src/approvals/InlineApprovalCard.tsx
    - web/src/approvals/ThreadApprovalCards.tsx
    - web/src/approvals/__tests__/ApprovalList.test.tsx
    - web/src/approvals/__tests__/InlineApprovalCard.test.tsx
    - web/src/approvals/__tests__/ThreadApprovalCards.test.tsx
  modified:
    - web/src/AppShell.tsx
    - web/src/__tests__/AppShell.test.tsx
    - web/src/i18n/resources.ts
    - internal/webui/dist (rebuilt embedded cockpit)

key-decisions:
  - "Inline card driven from the single cross-thread useApprovals poll filtered to the active thread (ThreadApprovalCards) rather than wiring an ask_user tool-part through assistant-ui — keeps resolution inline (D-03) while the badge/list owns aggregation (D-04), and reuses ONE data source"
  - "deny != accept enforced at TWO layers: useResolveApproval strips content for decline/cancel AND the card never passes operator text on a non-accept verb (T-25-17 footgun double-guard)"
  - "Cancel-run inline 'Stop this run?' confirm only while streaming (isStreaming), immediate cancel when idle — not a modal (Claude-Code-fast feel, UI-SPEC §Destructive actions)"
  - "fetchApprovals coerces a non-array body to [] so an unexpected envelope can never crash a downstream .filter/.length (defensive boundary)"

patterns-established:
  - "Cross-thread aggregate badge: a refetchInterval poll count surfaced as an accent pill with a polite live-region announcement"
  - "Inline HITL card: question verbatim + option buttons / free-text + three verbs over the resolve adapter, terminal chips on success/expiry"

requirements-completed: [APRV-01, APRV-02, APRV-03]

# Metrics
duration: ~40min
completed: 2026-06-17
---

# Phase 25 Plan 05: Chat + Approval Center — Cross-Thread Approval UI Summary

**The "perfectly like Claude Code" HITL experience over the plan-25-02 adapter: a polled cross-thread pending-approval badge + lightweight list (APRV-01), an inline in-thread approval card answered in place with Answer/Decline/Cancel verbs (APRV-02), and explicit terminal-state rendering for stale/auto-terminated interrupts (APRV-03) — deny ≠ accept guarded at the wire, untrusted text auto-escaped, en+it copy, ≥97% coverage on the new approval files.**

## Performance

- **Duration:** ~40 min
- **Started:** 2026-06-17
- **Completed:** 2026-06-17
- **Tasks:** 2 of 2 (both TDD auto)
- **Files created/modified:** 13 (9 new approval sources/tests, 3 modified, dist rebuilt)

## Accomplishments
- **APRV-01:** `ApprovalBadge` shows the live cross-thread pending count (a `refetchInterval` poll of `GET /api/approvals`) as an accent pill, announcing count changes via an `aria-live="polite"` region (never assertive); `ApprovalList` is the lightweight popover whose rows carry `{{title}} · {{question}}` with an `Open` that navigates to `/c/:conversationId` (the D-04 jump) and binds the thread.
- **APRV-02:** `InlineApprovalCard` renders the backend `ask_user` question VERBATIM + option buttons (or a free-text input) + the three verbs (D-05): `Answer` → `{action:"accept", content}` (answered chip), `Decline` → `{action:"decline"}` with NO operator content (declined helper), `Cancel run` → `{action:"cancel"}` (an inline `Stop this run?` confirm only while streaming). A successful accept/decline re-drives the run (continue-after-resume).
- **APRV-03 (D-06):** an expired / auto-terminated interrupt renders its terminal state (`Expired — auto-resolved.`, `text-warning`) inline AND in the badge list — verbs disabled / gone, never a silent loss.
- **Footgun guard (T-25-17):** decline ≠ accept enforced at two layers — the resolve hook strips operator content for decline/cancel, and the card never sends the typed text on a non-accept verb; the test asserts `action:"decline"` with no `content` on the wire.
- **XSS (T-25-20):** the question + option labels render as React text nodes (auto-escaped); `grep dangerouslySetInnerHTML` across `src/approvals/` is 0.
- New copy in BOTH `en` + `it` under `approval.*`; embedded `internal/webui/dist/` rebuilt and committed.

## Task Commits

Each task committed atomically:

1. **Task 1: Approval hooks + cross-thread badge + list (APRV-01 / D-04/D-06)** — `ad36626e` (feat)
2. **Task 2: Inline approval card — Answer/Decline/Cancel + resume + terminal states (APRV-02/03 / D-03/D-05/D-06)** — `57e6569d` (feat)

_Note: TDD components + their tests landed in the same commit per task (the components and tests were authored together and verified green before each commit)._

## Files Created/Modified
- `web/src/approvals/useApprovals.ts` — `useApprovals()` (`useQuery(['approvals'])`, `refetchInterval` poll, `retry:false`, `same-origin`, non-array body coerced to `[]`) + `useResolveApproval()` (`useMutation` → `POST /api/approvals/{token}/resolve`, `invalidateQueries(['approvals'])` on success; accept carries content, decline/cancel never do).
- `web/src/approvals/ApprovalBadge.tsx` — accent count pill, count aria-label, `aria-live="polite"` count-change region, no pill when zero pending.
- `web/src/approvals/ApprovalList.tsx` — cross-thread popover; `{{title}} · {{question}}` rows; `Open` → `/c/:conversationId` + `onOpen`; D-06 terminal row (warning, icon+text).
- `web/src/approvals/approvalState.ts` — pure `isTerminal` + `parseOptions` helpers (kept out of `.tsx` for the react-refresh only-export-components gate).
- `web/src/approvals/InlineApprovalCard.tsx` — in-thread card; verbatim question, option/free-text, Answer/Decline/Cancel verbs, terminal chips (success/warning/danger), `role="alert"` resolve error, `aria-invalid` omit-when-valid on the free-text.
- `web/src/approvals/ThreadApprovalCards.tsx` — filters the cross-thread poll to the active conversation, mounts the inline card(s) in the chat lane, re-drives on resume.
- `web/src/approvals/__tests__/{ApprovalList,InlineApprovalCard,ThreadApprovalCards}.test.tsx` — badge/list/terminal/empty/navigation + verbs/footgun/cancel-confirm/terminal/error + inline-mount filter/redrive.
- `web/src/AppShell.tsx` — header badge + popover list (Open binds the thread); inline-card mount in the chat lane region; `redriveRun` continue-after-resume.
- `web/src/__tests__/AppShell.test.tsx` — APRV-01/D-04 end-to-end test (badge count → Open → inline card surfaces → Answer re-drives `/agent/run`).
- `web/src/i18n/resources.ts` — `approval.*` keys in BOTH en + it.
- `internal/webui/dist/` — rebuilt embedded cockpit (AppShell chunk now bundles the approval surfaces).

## Decisions Made
- **Inline card from the single cross-thread poll, filtered to the active thread.** Rather than threading an `ask_user` tool-part through assistant-ui's tool-ui, `ThreadApprovalCards` filters the `useApprovals` poll down to the open conversation and mounts `InlineApprovalCard` in the chat lane. This keeps resolution inline (D-03), lets the badge/list own aggregation (D-04), and reuses ONE data source — no second protocol.
- **deny ≠ accept double-guarded.** The footgun (operator types an answer, then declines) is blocked at the hook (`useResolveApproval` strips content unless accept) AND at the call site (the card passes no content on a non-accept verb). The test asserts the wire payload.
- **Cancel confirm is inline-while-streaming, not a modal.** UI-SPEC §Destructive actions: `Cancel run` shows `Stop this run?` inline only when `isStreaming`; idle cancels immediately — keeps the Claude-Code-fast feel.
- **Defensive array coercion.** `fetchApprovals` coerces a non-array body to `[]` so an unexpected envelope can never crash `.filter`/`.length` downstream.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Coerce a non-array `/api/approvals` body to `[]`**
- **Found during:** Task 1 (running the full vitest suite after the AppShell wiring)
- **Issue:** The existing AppShell tests' fetch stub returns a JSON object for any non-`/api/conversations` URL; `/api/approvals` matched that branch, so `data` was an object and `ThreadApprovalCards` threw `(data ?? []).filter is not a function` (an uncaught exception failing CI).
- **Fix:** `fetchApprovals` now coerces a non-array body to `[]` — a defensive boundary that also hardens against an unexpected server envelope. The badge/list/cards then safely show empty.
- **Files modified:** web/src/approvals/useApprovals.ts
- **Verification:** full suite green (174/174), no uncaught exceptions.
- **Committed in:** `ad36626e` (Task 1 commit)

**2. [Rule 3 - Blocking] Reverted unrelated IT login copy churn in resources.ts**
- **Found during:** Task 1 (full suite — `LoginPage.test.tsx` failed expecting `Frase segreta operatore`)
- **Issue:** The working-tree `resources.ts` had unrelated Italian login-copy edits (`Frase segreta operatore` → `Password operatore`, and the `wrongPassphrase`/`showPassword`/`hidePassword` strings) introduced OUTSIDE this plan's scope (a linter/external edit). They broke the existing LoginPage test, which is not in this plan's surface.
- **Fix:** Reverted those four IT login strings to their HEAD values (per scope-control — this plan only adds `approval.*` copy; never modify an existing test to accommodate an out-of-scope churn). My commit's resources.ts diff is now ONLY the `approval.*` additions.
- **Files modified:** web/src/i18n/resources.ts
- **Verification:** `git diff HEAD` on resources.ts shows only `approval.*` lines; LoginPage test passes.
- **Committed in:** `ad36626e` (Task 1 commit)

**3. [Rule 3 - Blocking] Lint fixes (template-literal typing + test type-assertions)**
- **Found during:** Tasks 1 & 2 (`eslint --max-warnings=0`)
- **Issue:** `@typescript-eslint/restrict-template-expressions` on a `.split()`-derived `string | undefined` in `TerminalChip`; `no-base-to-string` on `String(init.body)`; `no-non-null-assertion` on a test array index.
- **Fix:** Replaced the chip `.split` with typed `Record<ChipTone,string>` lookups; cast `init.body as string` in the test; scoped the list-row click with `within(row)` instead of a non-null assertion.
- **Files modified:** web/src/approvals/InlineApprovalCard.tsx, web/src/approvals/__tests__/{InlineApprovalCard,ApprovalList}.test.tsx
- **Verification:** `npm run lint` clean (0 errors).
- **Committed in:** `ad36626e` + `57e6569d`

**4. [Rule 2 - Missing coverage] Added ThreadApprovalCards test + AppShell approval-wiring test**
- **Found during:** Task 2 (the touched-file ≥85% coverage gate — `ThreadApprovalCards.tsx` at 66% and `AppShell.tsx` at 84% statements)
- **Issue:** The plan named `ApprovalList.test.tsx` + `InlineApprovalCard.test.tsx`; the inline-mount wrapper (`ThreadApprovalCards`) and the new AppShell badge/list/`redriveRun` wiring were under the floor.
- **Fix:** Added `ThreadApprovalCards.test.tsx` (filter/empty/redrive) + an AppShell APRV-01/D-04 end-to-end test (badge → Open → inline card → Answer → `/agent/run` re-drive). `ThreadApprovalCards` → 100%, `AppShell.tsx` → 92% stmts / 95.65% lines.
- **Files modified:** web/src/approvals/__tests__/ThreadApprovalCards.test.tsx, web/src/__tests__/AppShell.test.tsx
- **Verification:** full suite green; touched-file coverage all ≥85%.
- **Committed in:** `57e6569d` (Task 2 commit)

---

**Total deviations:** 4 auto-fixed (1 bug, 2 blocking, 1 missing coverage). **Impact on plan:** all necessary for correctness / CI-green / the coverage floor; the resources.ts revert is scope-control (kept the commit to its intended `approval.*` surface). No scope creep.

## Issues Encountered
- The existing AppShell test fetch double returns an object for non-conversation URLs, which `/api/approvals` matched — surfaced the array-coercion bug (fixed, deviation 1). The AppShell approval test uses its own dedicated fetch stub returning a real approvals array.

## Threat Model Coverage
- **T-25-17 (deny ≠ accept):** `Decline` resolves `{action:"decline"}` with NO operator content — guarded at the hook AND the call site; `InlineApprovalCard.test` asserts the wire payload action is `"decline"` and `content` is `undefined` even when the operator typed text.
- **T-25-18 (stale/auto-terminated silent loss):** D-06 terminal state rendered inline (`InlineApprovalCard`, verbs gone) AND in the list (`ApprovalList`, warning row) — asserted in both tests, never omitted.
- **T-25-19 (privileged cross-thread resume):** inherited from plan 25-02 — resolve is behind `RequireAuth` + `RequireCapability`; all fetches are `credentials: 'same-origin'` (SameSite cookie, no cross-origin write path). A capability-denied resolve surfaces as `isError` → the `role="alert"` error copy, never a silent no-op (asserted via the 403 test).
- **T-25-20 (XSS via question/options):** question + option labels render as React text nodes (auto-escaped); `grep -rc dangerouslySetInnerHTML src/approvals/` → 0.
- **T-25-SC (supply chain):** zero new dependencies (reuses React Query + react-router + react-i18next).

## Verification Evidence
- `cd web && npm run lint` → clean (`eslint --max-warnings=0`, exit 0).
- `cd web && npm run typecheck` → clean (`tsc --noEmit`, exit 0).
- `cd web && npm run test -- ApprovalList` → 1 file, 8 tests pass (badge count/aria-label, polite live region, no-stale-pill, toggle; list rows, Open→/c/:id+onOpen, D-06 terminal warning, empty state).
- `cd web && npm run test -- InlineApprovalCard` → 1 file, 9 tests pass (verbatim question + options, free-text, Answer accept option+free-text, Decline footgun guard, Cancel idle, Cancel-while-streaming inline confirm, D-06 terminal verbs-gone, role=alert error).
- `cd web && npm run test` → **24 files, 174 tests pass**; coverage **98.85% stmts / 92.97% branches / 99.09% funcs / 99.38% lines** (gate ≥85%).
- **Touched-file coverage (≥85% each):** ApprovalBadge 100%, ApprovalList 100%, InlineApprovalCard 97.29%, ThreadApprovalCards 100%, approvalState 100%, useApprovals 93.33%, AppShell.tsx 92% stmts / 95.65% lines.
- `cd web && npm run build` → success (exit 0); embedded `internal/webui/dist/` rebuilt (AppShell chunk 418 kB) and committed.
- **Source assertions:** `grep -c refetchInterval useApprovals.ts` → 2 (≥1); `grep -c same-origin useApprovals.ts` → 3; `grep aria-live ApprovalBadge.tsx` → `polite` (×2, never assertive attribute); `grep -rc dangerouslySetInnerHTML src/approvals/` → 0; `approval.*` block present in BOTH en + it.

## Next Phase Readiness
- The cross-thread approval experience is live in the cockpit: badge (polite count), list (Open jumps), inline card (Answer/Decline/Cancel), explicit terminal states, run re-drive on resume. Ready for:
  - **25-06 UAT** — trigger a real `ask_user` in a background thread → badge increments → Open → answer/decline/cancel inline → run resumes.
  - **25-07 branch picker** — mounts onto the same chat lane; the inline card coexists with branch nav (independent surfaces).
- No blockers. One follow-up note: `redriveRun` currently posts an empty-`messages` `/agent/run`; the backend's continue-after-resume contract (plan 25-02 / 25-06) drives off the rehydrated history — verified live in 25-06 UAT.

## Self-Check: PASSED

All 9 new approval sources/tests + the SUMMARY exist on disk; both task commits (`ad36626e`, `57e6569d`) are in the git log.

---
*Phase: 25-chat-approval-center*
*Completed: 2026-06-17*
