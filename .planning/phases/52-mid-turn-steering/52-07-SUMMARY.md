---
phase: 52-mid-turn-steering
plan: 07
subsystem: ui
tags: [assistant-ui, react, sse, cockpit, steer, idempotency, i18n, vite, docker]

# Dependency graph
requires:
  - phase: 52-mid-turn-steering (plan 04)
    provides: "POST /agent/runs/{runID}/steer route, SteerEventName = \"aura.steer\" CUSTOM echo frame, the aura.steer payload key set ({conversation_id, round, steers: [{id, source, text, delivery}]})"
  - phase: 52-mid-turn-steering (plan 05)
    provides: "the 410-on-terminal-run refusal contract and the auto_delivery_next_turn delivery form"
provides:
  - "web/src/chat/steerRun.ts: steerRun(runId, text) — one POST, one Idempotency-Key minted per logical send, typed SteerRefusal (invalid/busy/ended)"
  - "web/src/chat/sseAdapter.ts steerNoticeValue + onSteer pump seam threaded through streamRun/streamPost/streamSSE"
  - "web/src/chat/sseResume.ts onSteer threaded through AttachRunOptions/the engine/pumpBody — the reattached-tab echo"
  - "web/src/chat/ExternalStoreChat_steer.ts useSteerSend — the live-run-submit-is-a-steer decision, optimistic append + rollback, notice dedup"
  - "web/src/chat/SteerNotice.tsx — the visible redirect echo"
  - "chat.steer.* i18n keys in web/src/i18n/resources.steer.ts (en/it)"
  - "internal/webui/dist rebuilt via the Docker webbuild stage (Linux Node-24-canonical bytes)"
affects: [52-08]

actuals:
  tokens: 19500
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Dedicated steer control beside Cancel, not an un-disabled Send — forced by the Step-1 measurement (see key-decisions)."
    - "Render-time UI gate (isRunning, a plain reactive boolean) kept strictly separate from the real run-id resolution (resolveRunId(), only ever invoked from event-handler/async-callback contexts) — the react-hooks/refs pattern this plan introduces for any future hook that needs both a cheap render-time gate and a precise call-time resolution of the same ref."
    - "i18n split-file convention (resources.steer.ts) extended to a fourth namespace, following resources.composer.ts/resources.governance.ts/resources.graph.ts."
    - "Docker webbuild-stage extraction (docker build --target webbuild + docker cp) as the sanctioned Windows-host substitute for scripts/web_dist_freshness.sh when no native Linux Node toolchain is available."

key-files:
  created:
    - web/src/chat/steerRun.ts
    - web/src/chat/steerRun.test.ts
    - web/src/chat/sseAdapter.onSteer.test.ts
    - web/src/chat/ExternalStoreChat_steer.ts
    - web/src/chat/ExternalStoreChat_steer.test.tsx
    - web/src/chat/SteerNotice.tsx
    - web/src/i18n/resources.steer.ts
  modified:
    - web/src/chat/sseAdapter.ts
    - web/src/chat/sseResume.ts
    - web/src/chat/ExternalStoreChat.tsx
    - web/src/chat/ExternalStoreChat_liveRun.ts
    - web/src/chat/Composer.tsx
    - web/src/i18n/resources.ts
    - web/src/chat/__tests__/ExternalStoreChat.liveRun.test.tsx
    - internal/webui/dist (rebuilt, 61 files)

key-decisions:
  - "Step-1 measurement (this plan's own required first step): read installed node_modules/@assistant-ui/react source, not its docs. Its native queue/steer capability (capabilities.queue, steerQueueItem, moveQueueItem) exists in the library but is NOT exposed through Aura's legacy useExternalStoreRuntime — the runtime shape Aura built on predates and does not implement that capability surface. Confirmed: useComposerSend's native disabled gate is `!canSend || (isRunning && !capabilities.queue)`, and createActionButton ORs that native disabled with the caller's own `disabled` prop, so nothing short of adopting the native queue subsystem (an architectural change outside this plan's scope, D-10 already ratified) could un-disable assistant-ui's own Send while isRunning. This forced Shape (b) from the plan's own fork: a dedicated 'Redirect the current turn' control rendered beside Cancel, wired directly to useSteerSend, rather than un-disabling Send. Recorded per the plan's explicit instruction to record which shape the measurement forced."
  - "available (the render-time UI gate in useSteerSend) is derived from a plain `isRunning: boolean` argument, not from reading `activeRunIdRef.current` during render — react-hooks/refs forbids a render-time ref read even through a memoized useCallback. The REAL run-id resolution (resolveRunId(), preferring activeRunIdRef.current, falling back to liveRunId) still happens, but only inside trySend, which is always invoked from an event-handler or async-callback context, never render. isRunning tracks the same underlying state closely enough for the UI gate; trySend's own resolution is what actually has to be correct, and it is."
  - "The notice-dedup scheme: pendingTextRef (set on send, matched against the first source==='cockpit' frame entry with the same text) combined with seenIdsRef (a Set, guards against literal duplicate-frame observation) — so a tab that BOTH sends its own steer AND observes the corresponding aura.steer echo frame shows exactly one notice, and a tab that only observes (never sent) still shows one."
  - "internal/webui/dist rebuilt via `docker build --target webbuild` (the Dockerfile's own node:24-bookworm-slim stage, matching CI's AURA_CI_NODE_VERSION=24.x) rather than by literally invoking scripts/web_dist_freshness.sh on this host. No native Linux Node-24 toolchain exists in this Windows/WSL environment — WSL's PATH resolves only the Windows-native node.exe under /mnt/c/Program Files/nodejs, which would produce Windows-hashed chunks, not Linux-canonical ones. The plan's own action text explicitly sanctions this: 'docker compose build aura ... is a legitimate way to produce the canonical bytes.' Verified via `docker cp` extraction + `diff -rq` against the committed dist before replacing it, and the replacement now diffs clean against that same extraction."

patterns-established:
  - "A live-run submit is a steer, decided in onNew before the optimistic append, gated on no attachments (Rule 2 addition — the steer route is text-only, so a submit with attachments still falls through to /agent/run rather than silently dropping them)."
  - "SteerNotice sits between turns as a thread-level role=status line (the CompactionMarker placement precedent), never as a message part the runtime could branch from — the steer is already a real persisted user turn server-side (52-04)."

requirements-completed: [STEER-01, STEER-03]

coverage:
  - id: D1
    description: "steerRun(runId, text) POSTs to /agent/runs/{runId}/steer with a single Idempotency-Key minted once and reused across retries of the same logical send; maps 202/400/429/410/other to resolve/SteerRefusal(invalid)/SteerRefusal(busy, +Retry-After)/SteerRefusal(ended)/generic Error"
    requirement: STEER-01
    verification:
      - kind: unit
        ref: "web/src/chat/steerRun.test.ts"
        status: pass
    human_judgment: false
  - id: D2
    description: "aura.steer CUSTOM frame is narrowed by steerNoticeValue exactly as artifactDescriptorValue narrows aura.artifact, and reduceFrame produces no new message part for it (signal only); onSteer fires from both the driving pump (streamRun/streamPost) and the reattach pump (sseResume attachRun)"
    requirement: STEER-03
    verification:
      - kind: unit
        ref: "web/src/chat/sseAdapter.onSteer.test.ts"
        status: pass
    human_judgment: false
  - id: D3
    description: "A submit while a run is live issues exactly one steer POST and zero /agent/run POSTs; a submit with no live run is unchanged (one /agent/run POST, zero steer POSTs) — the behavioural contract, proven at the full ExternalStoreChat component level"
    requirement: STEER-01
    verification:
      - kind: unit
        ref: "web/src/chat/ExternalStoreChat_steer.test.tsx#a submit while a run is live POSTs the steer route and NEVER a second /agent/run"
        status: pass
      - kind: unit
        ref: "web/src/chat/ExternalStoreChat_steer.test.tsx#a submit with NO live run takes the unchanged /agent/run path (Step-1 measurement)"
        status: pass
    human_judgment: false
  - id: D4
    description: "A refused steer (400/429) rolls back the optimistic user message and renders the refusal text — no orphan message left in the thread"
    requirement: STEER-01
    verification:
      - kind: unit
        ref: "web/src/chat/ExternalStoreChat_steer.test.tsx#rolls back the optimistic steer message on a 400 refusal and shows the refusal text"
        status: pass
      - kind: unit
        ref: "web/src/chat/ExternalStoreChat_steer.test.tsx#rolls back the optimistic steer message on a 429 refusal and shows the refusal text"
        status: pass
    human_judgment: false
  - id: D5
    description: "The visible redirect notice is de-duplicated: a tab that both sends its own steer and observes the corresponding aura.steer echo shows exactly one notice; a tab that only observes shows one too; a duplicate frame observation of the same id is suppressed"
    requirement: STEER-03
    verification:
      - kind: unit
        ref: "web/src/chat/ExternalStoreChat_steer.test.tsx#onFrame renders one notice when this tab both sent and observes its own echo (dedup by pending text, then by id)"
        status: pass
      - kind: unit
        ref: "web/src/chat/ExternalStoreChat_steer.test.tsx#onFrame renders a notice for a steer this tab only observed (never sent)"
        status: pass
      - kind: unit
        ref: "web/src/chat/ExternalStoreChat_steer.test.tsx#a duplicate frame observation (same id twice) does not re-render a second notice"
        status: pass
    human_judgment: false
  - id: D6
    description: "chat.steer.* i18n keys exist in both en and it with zero key drift, proven by the parity gate"
    requirement: STEER-03
    verification:
      - kind: unit
        ref: "web/src/i18n/__tests__/resources.parity.test.ts"
        status: pass
    human_judgment: false
  - id: D7
    description: "The committed internal/webui/dist equals a fresh Linux Node-24 build, proven by extracting the Dockerfile's webbuild stage and diffing byte-for-byte before and after the replacement commit"
    requirement: STEER-03
    verification:
      - kind: other
        ref: "docker build --target webbuild -f docker/aura/Dockerfile . ; docker cp <container>:/internal/webui/dist ; diff -rq — clean after replacement (git diff --exit-code -- internal/webui/dist/ = 0)"
        status: pass
    human_judgment: true
    rationale: "No native Linux Node-24 host toolchain was available to literally invoke scripts/web_dist_freshness.sh on this Windows/WSL machine; the Docker webbuild-stage extraction is a sanctioned equivalent per the plan's own text, but it is a different verification path than the plan's literal <verify> command, so a human should confirm the CI web-dist-freshness job (the byte-canonical proof of record) is green on push."

duration: ~85 min measured between the Task 1 and Task 3 commits (10d58a1d7 2026-08-25T23:20:54+02:00 -> 37bf9dcbe 2026-08-26T00:46:09+02:00), of which ~16 min was the Stryker mutation run alone
completed: 2026-08-26
status: complete
---

# Phase 52 Plan 07: Mid-turn steering — the cockpit composer contract Summary

**The cockpit composer redirects a live turn instead of doing nothing: a dedicated "Redirect the current turn" control (assistant-ui's native Send stays gated — confirmed by reading its installed source, not its docs) POSTs to the steer route with a per-send Idempotency-Key, the `aura.steer` echo reaches both the driving and the reattached tab, and every refusal kind renders instead of silently dropping the message.**

## Performance

- **Duration:** ~85 min measured between the Task 1 and Task 3 commits (see frontmatter `duration` for the exact span and the mutation-run caveat)
- **Started:** 2026-08-25T23:20:54+02:00
- **Completed:** 2026-08-26T00:46:09+02:00
- **Tasks:** 3/3
- **Files modified:** 14 source files (7 created, 7 modified) + 61 rebuilt dist artifacts

## Accomplishments

- `steerRun(runId, text)` — a runId-scoped POST mirroring `cancelRun`'s shape, minting one `Idempotency-Key` per logical send (never the global per-fetch `installMutationIdempotency` wrapper) and mapping 202/400/429/410/other to a typed `SteerRefusal` or a generic error.
- `steerNoticeValue`/`onSteer` land beside `artifactDescriptorValue`/`onArtifact` in `sseAdapter.ts`, fired from the pump (never `reduceFrame`, which stays byte-identical for every existing frame), and threaded through `sseResume.ts`'s reattach pump exactly as `onArtifact` already is — a reloaded or second tab watching a detached run now sees the redirect echo too.
- **Step-1 measurement, performed as the plan required (a test, not a note):** assistant-ui's own `ComposerPrimitive.Send` cannot submit while the runtime reports `isRunning` unless the runtime exposes `capabilities.queue` — which Aura's legacy `useExternalStoreRuntime` does not. This forced the dedicated-control shape: a "Redirect the current turn" button rendered beside Cancel in `Composer.tsx`, wired to `useSteerSend`, rather than un-disabling the native Send.
- `ExternalStoreChat_steer.ts`'s `useSteerSend` hook owns the live-run-submit-is-a-steer decision (preferring the tab's own `activeRunIdRef`, falling back to the conversation DTO's `liveRunId`), the optimistic user-message append and its rollback on refusal, and a notice state fed by both the client's own send and the `onSteer` pump signal, de-duplicated so a tab that does both shows exactly one notice.
- `SteerNotice.tsx` renders that notice (or a refusal) as a `role="status"` thread-level line with a CSS-only fade/slide beat, following the `CompactionMarker` placement precedent — never as a message part the runtime could branch from.
- `chat.steer.*` i18n keys (`sendAria`, `notice.redirected`, `notice.autoDelivered`, `refusal.invalid/busy/ended/failed`) added in both `en` and `it`, in a new `resources.steer.ts` split file (the `resources.composer.ts` precedent) since `resources.ts` was at 566/600 LOC.
- `internal/webui/dist` rebuilt through the Dockerfile's own `webbuild` stage (`node:24-bookworm-slim`, matching CI's pinned `AURA_CI_NODE_VERSION=24.x`) and committed alone; `web-lint`, `web-test` (91.16%/85.19%/90.41%/92.96% stmt/branch/fn/line coverage, floor 85% on all four), and `web-mutation` (Stryker, 74.80 score against a break threshold of 70) all pass.

## Task Commits

1. **Task 1: The steer API client and the aura.steer pump seam** - `10d58a1d7` (feat)
2. **Task 2: Make the composer steer** - `c3c1b5fa8` (feat)
3. **Task 3: Rebuild the committed dist on Linux and run the frontend gates** - `37bf9dcbe` (build)

_Note: no plan metadata commit yet — that follows this SUMMARY per the executor's final_commit step._

## Files Created/Modified

- `web/src/chat/steerRun.ts` (65 LOC) - the steer API client, one Idempotency-Key per logical send
- `web/src/chat/steerRun.test.ts` (166 LOC) - route/method/headers/body, key reuse/uniqueness, 202/400/429/410 classification
- `web/src/chat/sseAdapter.ts` (534 LOC, was 466) - `SteerNotice`/`SteerNoticeEntry`, `isSteerNotice(Entry)`, `steerNoticeValue`, `onSteer` on `StreamRunOptions`/`StreamPostOptions`/`StreamSSEOptions`
- `web/src/chat/sseAdapter.onSteer.test.ts` (192 LOC) - narrowing + rejection cases, pump firing via both `streamRun` and `attachRun`, reduceFrame byte-equality regression
- `web/src/chat/sseResume.ts` (419 LOC, was 410) - `onSteer` threaded through `AttachRunOptions`/`EngineOptions`/`pumpBody`
- `web/src/chat/ExternalStoreChat_steer.ts` (129 LOC) - `useSteerSend`: run-id resolution, optimistic append/rollback, notice dedup
- `web/src/chat/ExternalStoreChat_steer.test.tsx` (297 lines) - 11 tests: the behavioural contract, refusal rollback, notice dedup at both the hook and component level
- `web/src/chat/SteerNotice.tsx` (41 LOC) - the visible redirect echo / refusal line
- `web/src/i18n/resources.steer.ts` (34 LOC, new) - `chatSteerEn`/`chatSteerIt`
- `web/src/i18n/resources.ts` (571 LOC, was 568) - imports + nests `chat.steer`; also updated `chat.liveRun.hint` copy (see Deviations)
- `web/src/chat/ExternalStoreChat.tsx` (557 LOC, was 545; +17/-5 added lines, within the ≤20 cap) - `useSteerSend` wiring, the `onNew` early steer branch, `sendBlocked={false}`, `steerAvailable`/`onSteerSubmit`, `<SteerNotice/>`
- `web/src/chat/ExternalStoreChat_liveRun.ts` (129 LOC, was ~118 — see Deviations) - `onSteer` threaded onto the reattach pump
- `web/src/chat/Composer.tsx` (570 LOC, was 540; +30/-0 added lines, exactly at the ≤30 cap) - the dedicated Redirect control, `steering` gate, Enter-while-steering interception
- `web/src/chat/__tests__/ExternalStoreChat.liveRun.test.tsx` (modified — see Deviations) - two hardcoded `chat.liveRun.hint` literals updated for the copy change
- `internal/webui/dist` (61 files rebuilt) - Linux Node-24-canonical build output, no source changes

## Decisions Made

See `key-decisions` in the frontmatter for: the Step-1 measurement finding and why it forced a dedicated control instead of un-disabling Send; the `isRunning`-vs-ref-read split behind `useSteerSend`'s `available` gate (the `react-hooks/refs` constraint); the notice-dedup scheme; and the Docker-webbuild-stage substitution for the freshness script.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] `ExternalStoreChat_liveRun.ts` needed `onSteer` wiring though not listed in `files_modified`**
- **Found during:** Task 2, wiring the reattach pump
- **Issue:** The plan's `files_modified` for Task 2 does not list `ExternalStoreChat_liveRun.ts`, but the plan's own `must_haves.truths` requires the reattached tab to see the steer echo too, and that pump's `attachRun({...})` call lives in this file, not in `ExternalStoreChat.tsx`.
- **Fix:** Added `onSteer` to `LiveRunAttachArgs`, threaded it into the `attachRun({...})` call and into the hook's `useCallback` deps.
- **Files modified:** `web/src/chat/ExternalStoreChat_liveRun.ts`
- **Verification:** `ExternalStoreChat_steer.test.tsx`'s hook-level `onFrame` tests plus the existing `ExternalStoreChat.liveRun.test.tsx` regression suite both pass.
- **Committed in:** `c3c1b5fa8`

**2. [Rule 1 - Test tracked intentional behavior change] `ExternalStoreChat.liveRun.test.tsx`'s hardcoded `chat.liveRun.hint` literals**
- **Found during:** Task 2
- **Issue:** Two tests asserted the OLD `chat.liveRun.hint` string verbatim, which this plan intentionally changed (see deviation 6) since the old copy ("Sending re-enables when it finishes") became false the moment steering shipped.
- **Fix:** Updated both literal-string assertions to the new copy. This is not "modifying a test to force a pass" — the underlying behavior legitimately changed and the test was asserting the literal old string, not a broken assumption.
- **Files modified:** `web/src/chat/__tests__/ExternalStoreChat.liveRun.test.tsx`
- **Committed in:** `c3c1b5fa8`

**3. [Rule 1 - Bug, self-caught before any test run] Steer button missing `type="button"`**
- **Found during:** Task 2, self-review of the new `Composer.tsx` button before writing its test
- **Issue:** The new steer button sits inside `ComposerPrimitive.Root`'s `<form>`. Without an explicit `type="button"`, the HTML default `type="submit"` would fire the form's native submit handler IN ADDITION TO the button's own `onClick` — a double-submission (both `onClick`'s `submitSteer()` and the composer's native `send()`).
- **Fix:** Added `type="button"`, matching the codebase's existing convention (`AddAttachment`, the mic-fallback buttons).
- **Files modified:** `web/src/chat/Composer.tsx`
- **Committed in:** `c3c1b5fa8`

**4. [Rule 1 - Bug] `react-hooks/refs` violation in the render-time `available` gate**
- **Found during:** Task 2, first lint pass on `ExternalStoreChat_steer.ts`
- **Issue:** `const available = resolveRunId() !== undefined;` computed at render time invoked a `useCallback` (`resolveRunId`) that reads `activeRunIdRef.current` — an unsafe render-time ref read regardless of the `useCallback` wrapper (`react-hooks/refs` flags this).
- **Fix:** Added an `isRunning: boolean` field to `UseSteerSendArgs`; `available` is now `isRunning` (a plain reactive boolean, safe at render time). The real run-id resolution stays inside `trySend`, which only ever runs from an event-handler/async-callback context. `ExternalStoreChat.tsx`'s call site passes its own `isRunning`.
- **Files modified:** `web/src/chat/ExternalStoreChat_steer.ts`, `web/src/chat/ExternalStoreChat.tsx`, `web/src/chat/ExternalStoreChat_steer.test.tsx` (fixture updated to pass `isRunning`)
- **Committed in:** `c3c1b5fa8`

**5. [Rule 2 - Missing Critical] Attachment guard added to `onNew`'s new steer branch**
- **Found during:** Task 2, wiring `onNew`
- **Issue:** The steer route (`internal/agui/server_run_steer.go`, as read from source) is text-only. Without a guard, a submit carrying attachments during a live run would have silently routed through `steer.trySend` and dropped the attachments with no signal to the operator.
- **Fix:** Gated the early steer branch on `(message.attachments?.length ?? 0) === 0`, so a submit WITH attachments during a live run falls through to the unchanged `/agent/run` path instead (today's behavior, preserved for that case) rather than silently discarding the attachment.
- **Files modified:** `web/src/chat/ExternalStoreChat.tsx`
- **Committed in:** `c3c1b5fa8`

**6. [Rule 1 - Bug, string now false] `chat.liveRun.hint` copy update**
- **Found during:** Task 2
- **Issue:** The pre-existing hint "A run is still in progress in this conversation. Sending re-enables when it finishes." became factually wrong the moment steering shipped — sending no longer waits for the run to finish.
- **Fix:** Changed to "A run is still in progress. Type to redirect it, or tap Stop." (en) / "Un'esecuzione è ancora in corso. Scrivi per reindirizzarla, oppure premi Ferma." (it).
- **Files modified:** `web/src/i18n/resources.ts`
- **Committed in:** `c3c1b5fa8`

**7. [Rule 3 - Blocking] No native Linux Node-24 toolchain for `scripts/web_dist_freshness.sh`**
- **Found during:** Task 3
- **Issue:** The script's literal form (`cd web && npm ci && npm run build`) needs to run on Linux to produce byte-canonical chunk hashes matching CI. This machine's WSL has no native Linux Node install — only the Windows-native `node.exe` is reachable from WSL's PATH (`/mnt/c/Program Files/nodejs`), which would produce Windows-hashed output, not Linux-canonical bytes, and the plan's own prohibitions explicitly forbid a Windows Vite build for this artifact.
- **Fix:** Used `docker build --target webbuild -f docker/aura/Dockerfile .` (the exact `node:24-bookworm-slim` stage `docker compose build aura` also uses, matching CI's `AURA_CI_NODE_VERSION=24.x`), extracted `/internal/webui/dist` via `docker cp`, diffed it against the committed dist (confirmed stale, as expected from Tasks 1-2's source changes), replaced the committed dist, and re-diffed clean. The plan's own action text explicitly names this as a legitimate route: "docker compose build aura ... is a legitimate way to produce the canonical bytes."
- **Files modified:** `internal/webui/dist` (61 files)
- **Verification:** `git diff --exit-code -- internal/webui/dist/` clean after the replace; `diff -rq` against the Docker extraction clean.
- **Committed in:** `37bf9dcbe`

---

**Total deviations:** 7 auto-fixed (2 missing-critical additions, 1 test-tracks-intentional-behavior-change, 3 bugs, 1 blocking toolchain substitution).
**Impact on plan:** All auto-fixes were necessary for correctness (no double-submit, no silently-dropped attachments, no render-time ref-read lint violation) or were the direct, in-scope consequence of this plan's own intentional changes (the hint copy, the reattach-pump wiring the plan's own truths required). No scope creep.

## Issues Encountered

- **Coverage floor was close on branches:** the full-suite vitest run measured 85.19% branch coverage against an 85% floor — passing, but with very little headroom. No action taken since this plan's own new files (`ExternalStoreChat_steer.ts`, `SteerNotice.tsx`, `steerRun.ts`) are well-covered (11 dedicated tests plus the 4 Task-1 tests); the branch-coverage pressure comes from pre-existing files unrelated to this plan's scope.
- **Stryker's `mutate` list does not include any file this plan touched** (`stryker.config.json`'s fixed list predates this plan and was not asked to change). The 74.80 mutation score recorded in Task 3's commit is therefore the pre-existing baseline, not a measurement of this plan's own new code — flagging this explicitly so a future reader does not mistake it for steer-specific mutation coverage.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- 52-08's live cockpit E2E can now assert: no 409, no dead input, a visible "Redirect the current turn" control while a run is live, the exact `chat.steer.*` strings recorded in `key-files`/this SUMMARY's Accomplishments, and a reload showing the steer at its persisted position (52-04's persistence, unchanged by this plan).
- `Composer.tsx` (570/600) and `ExternalStoreChat.tsx` (557/600) both have materially less headroom than before — the next plan touching either should re-`wc -l` first and expect to do the refactor-on-touch split promptly if it adds more than a few lines.
- `resources.ts` is at 571/600; a fifth i18n namespace should very likely go straight to its own split file rather than testing that ceiling further.
- The CI `web-dist-freshness` job is the byte-canonical proof of record for the Task 3 dist rebuild (see coverage `D7`'s `human_judgment: true` — this plan's local verification used the Docker webbuild-stage extraction as a sanctioned substitute, not the literal script, since no native Linux Node-24 host was available); confirm it is green after push.

## Self-Check: PASSED

- FOUND: web/src/chat/steerRun.ts
- FOUND: web/src/chat/steerRun.test.ts
- FOUND: web/src/chat/sseAdapter.onSteer.test.ts
- FOUND: web/src/chat/ExternalStoreChat_steer.ts
- FOUND: web/src/chat/ExternalStoreChat_steer.test.tsx
- FOUND: web/src/chat/SteerNotice.tsx
- FOUND: web/src/i18n/resources.steer.ts
- FOUND: commit 10d58a1d7
- FOUND: commit c3c1b5fa8
- FOUND: commit 37bf9dcbe

---
*Phase: 52-mid-turn-steering*
*Completed: 2026-08-26*
