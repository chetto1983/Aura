---
phase: 37B-web-artifact-sidebar
plan: 05
subsystem: ui
tags: [webart, onartifact, sse-pump, rehydration, source-kind-split, d-15, dupl-fold, vitest, react]

# Dependency graph
requires:
  - phase: 37A-web-artifact-delivery-lane
    provides: "aura.artifact CUSTOM descriptor (asset_id) + GET /api/assets/{id}/download (identity-scoped) — the auth route the durable assistant chip targets"
  - phase: 37B-web-artifact-sidebar (plan 02)
    provides: "Asset.source_kind union widened to include 'agent' — the discriminant the D-15 split filters on"
provides:
  - "onArtifact?(assetId): signal threaded through streamRun/streamPost → streamSSE, fired in the frame pump on an aura.artifact descriptor (mirrors onUsage; NOT from reduceFrame)"
  - "ExternalStoreChat.onArtifact prop (mirrors onUsage), forwarded into streamRun/streamPost"
  - "foldAgentOntoAssistant(messages, agentAssets): pure fold of source_kind='agent' deliverables onto assistant turns (exported for test)"
  - "history load splits listThreadAssets by source_kind: uploads → user-turn fold (unchanged), agent → assistant-turn fold"
  - "AssistantMessage renders the durable authenticated /api/assets/{id}/download chip for folded agent assets"
affects: [37B-06, 37B-07]

# Tech tracking
tech-stack:
  added: []   # client-side only — no new deps, no backend change, no migration, no env
  patterns:
    - "Pump-level SSE signal: onArtifact fires in the streamSSE frame loop (next to reduceFrame), NEVER emitted from the pure reducer (T-37B-15 purity invariant)"
    - "source_kind split-fold: partition thread assets by source_kind BEFORE folding — uploads onto user turns, agent deliverables onto assistant turns (D-15 correctness fix)"
    - "Dupl-fold on touch: attachAssetsToUserMessages + the new agent fold share a foldAssetsPositionally(role) core (jscpd/DRY + 600-LOC cap)"
    - "Presentational split: the assistant download chip lives in ExternalStoreChat_messages.tsx (the existing render-component sibling), not the runtime container"

key-files:
  created:
    - web/src/chat/sseAdapter.onArtifact.test.ts
    - web/src/chat/ExternalStoreChat.rehydration.test.tsx
  modified:
    - web/src/chat/sseAdapter.ts
    - web/src/chat/ExternalStoreChat.tsx
    - web/src/chat/ExternalStoreChat_messages.tsx   # DEVIATION (Rule 3 / 600-LOC cap): assistant-side chip renderer moved here

key-decisions:
  - "onArtifact fires on ANY valid descriptor, passing frame.value.asset_id (which may be undefined on a degraded delivery) — the recommended contract, so the panel auto-open triggers on descriptor presence, not only on a successful ingest"
  - "foldAgentOntoAssistant is EXPORTED (with a scoped react-refresh/only-export-components disable) so the rehydration test can assert turn-attribution as a pure fold — the crispest 'agent never lands on a user turn' regression proof"
  - "Refactor-on-touch: the additions pushed ExternalStoreChat.tsx to 655 LOC (over CLAUDE.md's hard 600 cap enforced by the file-size pre-commit hook) → dupl-folded the two positional zips into foldAssetsPositionally(role) (595 LOC) and moved the chip markup into ExternalStoreChat_messages.tsx"
  - "onArtifact forwarded via conditional spread (...(onArtifact !== undefined ? { onArtifact } : {})) — the codebase idiom under exactOptionalPropertyTypes; keeps the public StreamRun/PostOptions.onArtifact type clean (no | undefined), mirroring onUsage/newId"

requirements-completed: []   # INTENTIONALLY EMPTY — WEBART-07/WEBART-08 are phase-spanning; per the 37B-01/03/04 precedent they stay [ ] until the terminal acceptance plan (37B-08, Playwright e2e + aggregate). requirements mark-complete NOT run.

# Coverage metadata
coverage:
  - id: D1
    description: "onArtifact(assetId) fires from the streamSSE pump on an aura.artifact descriptor frame, NEVER from reduceFrame (T-37B-15); degraded (no asset_id) fires onArtifact(undefined); malformed/unrelated frames do not fire"
    requirement: "WEBART-07"
    verification:
      - kind: unit
        ref: "web/src/chat/sseAdapter.onArtifact.test.ts (4 tests: fire-with-asset_id / degraded-undefined / malformed-descriptor / unrelated-frames)"
        status: pass
    human_judgment: false
  - id: D2
    description: "History load splits assets by source_kind — agent deliverables fold onto assistant turns and NEVER onto user turns (T-37B-13); user uploads keep the user-turn fold (non-regression)"
    requirement: "WEBART-08"
    verification:
      - kind: unit
        ref: "web/src/chat/ExternalStoreChat.rehydration.test.tsx (foldAgentOntoAssistant: agent→assistant/never-user, deleted-dropped, no-assistant-turn; render: chip on assistant + upload on user + exactly-one-link)"
        status: pass
    human_judgment: false
  - id: D3
    description: "A folded agent asset renders the durable authenticated /api/assets/{id}/download chip on its assistant message — download survives saved-conversation open with no reload (D-15); chip uses asset_id only, never object_key/host path (T-37B-14)"
    requirement: "WEBART-08"
    verification:
      - kind: unit
        ref: "web/src/chat/ExternalStoreChat.rehydration.test.tsx (findByRole link name=report.xlsx → href /api/assets/ag-1/download)"
        status: pass
    human_judgment: false
  - id: D4
    description: "ExternalStoreChat forwards its onArtifact prop into the stream so an aura.artifact frame drives the panel signal end-to-end (prop → streamRun → streamSSE pump → callback)"
    requirement: "WEBART-07"
    verification:
      - kind: unit
        ref: "web/src/chat/ExternalStoreChat.rehydration.test.tsx (send prompt → sseArtifactResponse → onArtifact called with 'asset-9')"
        status: pass
    human_judgment: false
  - id: D5
    description: "Saved-conversation open in a real browser shows the agent download chip inline AND keeps user uploads on their turns (visual/interaction fidelity)"
    verification: []
    human_judgment: true
    rationale: "jsdom cannot exercise the real assistant-ui external-store rehydration + a real authenticated download navigation — the live browser verification lands at the terminal Playwright plan (37B-08)"

# Metrics
duration: 20min
completed: 2026-07-09
status: complete
---

# Phase 37B Plan 05: onArtifact Signal + D-15 Split-Fold Rehydration Summary

**An `onArtifact(assetId?)` signal threaded through the `streamSSE` pump (mirroring `onUsage`, never from the pure reducer) plus a `source_kind` split-fold that rehydrates agent deliverables onto assistant turns with a durable authenticated download chip — fixing the download-disappears-on-saved-conversation-open bug (D-15).**

## Performance

- **Duration:** ~20 min
- **Completed:** 2026-07-09
- **Tasks:** 2 of 2
- **Files created:** 2 · **Files modified:** 3

## Accomplishments

- **onArtifact pump signal** (`bf7856e0`): added `onArtifact?(assetId)` to `StreamRunOptions`/`StreamPostOptions`/`StreamSSEOptions` and fired it inside the `streamSSE` frame loop — after `reduceFrame`, when `frame.type==='CUSTOM' && frame.name==='aura.artifact' && isArtifactDescriptor(frame.value)` — passing `frame.value.asset_id`. `reduceFrame` stays a pure reducer (no callback emitted from it — T-37B-15). A valid descriptor with no `asset_id` fires `onArtifact(undefined)` so the panel still auto-opens on descriptor presence. 4 unit tests (fire / degraded / malformed / unrelated).
- **D-15 split-fold + durable chip** (`acd9b317`): history load now partitions `listThreadAssets` by `source_kind` — `!== 'agent'` uploads keep the existing user-turn fold; `=== 'agent'` deliverables go through the new `foldAgentOntoAssistant` onto assistant turns (T-37B-13). `AssistantMessage` renders the `LocalArtifactDisplay`-style authenticated `/api/assets/{id}/download` anchor for each folded agent asset (asset_id only, never object_key/host path — T-37B-14), so the inline download survives saved-conversation open with no reload. `onArtifact` prop added to `ExternalStoreChat` (mirrors `onUsage`) and forwarded into `streamRun`/`streamPost`. 5 tests (pure attribution + render + forwarding).

## How the pieces mirror the existing patterns

| New behavior | Mirrored existing pattern |
|--------------|---------------------------|
| `onArtifact` option on the stream runners | `onUpdate`/`newId` threading through `streamRun`/`streamPost` → `streamSSE` |
| `ExternalStoreChat.onArtifact` prop | the `onUsage?(usage)` prop doc-comment + call-site shape (`:134`) |
| `foldAgentOntoAssistant` positional zip | `attachAssetsToUserMessages` (now both share `foldAssetsPositionally(role)`) |
| the assistant download chip | `LocalArtifactDisplay.tsx:66` same-origin `<a href download>` (37A-proven auth path, D-12) |

## Security Invariants — how each is realized

| Invariant | Realization |
|-----------|-------------|
| reduceFrame purity (T-37B-15) | `onArtifact` fired ONLY at the `streamSSE` pump loop, next to `reduceFrame(state, frame)`; the `CUSTOM/aura.artifact` reducer branch is unchanged and emits no callback. |
| Correct turn attribution (T-37B-13) | history load splits `assets` by `source_kind` BEFORE folding; `foldAgentOntoAssistant` targets `role === 'assistant'` only. The rehydration test asserts an agent asset lands on the assistant turn and the user turn's `metadata` stays `undefined`. |
| No path leak on the chip (T-37B-14) | the assistant chip anchor is `href={`/api/assets/${asset.id}/download`}` — `asset.id` only; `object_key`/`object_bucket`/host path never rendered. |

## Deviations from Plan

### Auto-adjusted (Rule 3 — blocking issue: CLAUDE.md 600-LOC cap enforced by the file-size pre-commit hook)

**1. [Rule 3 / CLAUDE.md] Refactor-on-touch to stay under the 600-LOC cap**
- **Found during:** Task 2 (the fold helper + the chip renderer additions).
- **Issue:** The additions pushed `ExternalStoreChat.tsx` to **655 LOC**, over CLAUDE.md's hard 600-LOC cap — the `file-size` pre-commit hook blocked the commit (no `--no-verify` allowed).
- **Fix:** (a) dupl-folded the two positional zips (`attachAssetsToUserMessages` + the new agent fold) into a shared `foldAssetsPositionally(messages, assets, role)` core — `attachAssetsToUserMessages` and `foldAgentOntoAssistant` are now thin wrappers → **595 LOC**; (b) moved the presentational download-chip markup out of the runtime container into the existing presentational sibling `ExternalStoreChat_messages.tsx` (`AssistantMessage` now reads `messageAttachments(message)` and renders the chip). This is the idiomatic refactor-on-touch (the file already split its render components into `_messages.tsx`).
- **Files:** `web/src/chat/ExternalStoreChat.tsx`, `web/src/chat/ExternalStoreChat_messages.tsx`
- **Commit:** `acd9b317`
- **Note:** `ExternalStoreChat_messages.tsx` was NOT in the plan's `files_modified`. Per CLAUDE.md-enforcement (project directives override plan instructions), the 600-LOC cap took precedence; the change is a presentational-code move, no behavior added beyond the plan's assistant-side chip requirement.

### Adjustment for lint/type invariants (no behavior change)

**2. [Rule 3] Conditional spread for the onArtifact option**
- Under `exactOptionalPropertyTypes: true`, passing a possibly-`undefined` `onArtifact` into a `StreamRun/PostOptions.onArtifact?: (…) => void` param type-errors. Used the codebase idiom `...(onArtifact !== undefined ? { onArtifact } : {})` at the three stream call sites, keeping the public option type clean (mirrors `onUsage`/`newId`, which are `?:` without `| undefined`).

**3. [Rule 3] Scoped `react-refresh/only-export-components` disable**
- Exporting `foldAgentOntoAssistant` (a non-component) from a component module trips `react-refresh/only-export-components` (enforced, `--max-warnings=0`). Added a single scoped `eslint-disable-next-line` with a justification comment — the standard test-helper-export case.

No architectural (Rule 4) changes. No auth gates. No package installs. No backend change, no server list-order change (both explicitly prohibited by the plan).

## Validation Results

- `npx tsc --noEmit` — clean.
- `npx vitest run src/chat/sseAdapter.onArtifact.test.ts` — 4 pass. `…/ExternalStoreChat.rehydration.test.tsx` — 5 pass.
- **Full `src/chat/` suite:** 453 tests pass (41 files) — the existing `sseAdapter` (54) and `ExternalStoreChat` attachments/branch/runtime suites all green (upload-fold + onUsage non-regression verified both branches).
- `eslint --max-warnings=0` + `prettier --check` — clean on all touched files (`sseAdapter.ts`, `sseAdapter.onArtifact.test.ts`, `ExternalStoreChat.tsx`, `ExternalStoreChat_messages.tsx`, `ExternalStoreChat.rehydration.test.tsx`).
- Pre-commit hooks (dup/vet/file-size) green on both task commits — no `--no-verify`. `ExternalStoreChat.tsx` = 595 LOC (under cap).

## Deferred / Known Stubs

None. `onArtifact` is a real pump signal wired to real `aura.artifact` frames; the split-fold reads the real `listThreadAssets` list and renders the real 37A download route. The `onArtifact` CONSUMER (invalidate `['assets', threadId]` + one-time auto-open) is 37B-07's job (AppShell) — this plan produces the PRODUCER signal + the D-15 chip fix (`affects: 37B-06, 37B-07`). No stubs, no placeholder data.

## Commits

- `bf7856e0` feat(37B-05): thread onArtifact signal through the streamSSE pump
- `acd9b317` feat(37B-05): D-15 split-fold rehydration + onArtifact prop

## Self-Check: PASSED

All 5 files (2 created + 3 modified) verified on disk; both task commits (`bf7856e0`, `acd9b317`) present in git history.
