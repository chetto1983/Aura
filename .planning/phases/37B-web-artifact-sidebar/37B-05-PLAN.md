---
phase: 37B-web-artifact-sidebar
plan: 05
type: execute
wave: 3
depends_on: ["37B-02"]
files_modified:
  - web/src/chat/sseAdapter.ts
  - web/src/chat/ExternalStoreChat.tsx
  - web/src/chat/sseAdapter.onArtifact.test.ts
  - web/src/chat/ExternalStoreChat.rehydration.test.tsx
autonomous: true
requirements: [WEBART-07, WEBART-08]
must_haves:
  truths:
    - "an aura.artifact CUSTOM frame carrying an asset_id fires an onArtifact(assetId) signal threaded through the streamSSE pump (not from the pure reducer)"
    - "ExternalStoreChat exposes an onArtifact prop (mirroring onUsage) forwarded into streamRun/streamPost"
    - "on saved-conversation history load, thread assets are split by source_kind: uploads fold onto user turns, source_kind='agent' fold onto assistant turns"
    - "a rehydrated agent asset renders the same authenticated download chip on its assistant message (D-15) — download survives saved-conversation open with no reload"
    - "Telegram/CLI/user-upload folding is unchanged (non-regression): user uploads still attach to user turns only"
  artifacts:
    - path: "web/src/chat/sseAdapter.ts"
      provides: "onArtifact signal in StreamSSE/Run/Post options, fired in the frame pump"
      contains: "onArtifact"
    - path: "web/src/chat/ExternalStoreChat.tsx"
      provides: "onArtifact prop + foldAgentOntoAssistant + split history fold"
      contains: "foldAgentOntoAssistant"
  key_links:
    - from: "web/src/chat/ExternalStoreChat.tsx"
      to: "web/src/chat/sseAdapter.ts"
      via: "forwards onArtifact prop into streamRun/streamPost"
      pattern: "onArtifact"
  prohibitions:
    - "MUST NOT emit the onArtifact callback from reduceFrame — it is a pure reducer; thread the signal at the streamSSE pump only"
    - "MUST NOT fold source_kind='agent' assets onto user turns (the exact D-15 bug being fixed)"
    - "MUST NOT change the server list order or add a backend endpoint — the fix is entirely client-side"
    - "MUST NOT break the existing attachAssetsToUserMessages behavior for uploads — split, do not replace"
---

<objective>
Wire the live-merge producer and fix the saved-conversation download-persistence bug. Add an `onArtifact(assetId?)` signal threaded through the `streamSSE` pump (mirroring `onUsage`), and split the history-load fold by `source_kind` so agent deliverables rehydrate onto ASSISTANT turns (D-15) instead of being wrongly folded onto user turns by the current `attachAssetsToUserMessages`.

Purpose: Give AppShell (plan 07) the `onArtifact` signal to invalidate the query + auto-open the panel; make the inline `local_artifact` download chip durable across saved-conversation open (WEBART-08 non-regression + D-15).
Output: `onArtifact` plumbed through sseAdapter + ExternalStoreChat; `foldAgentOntoAssistant` + split history fold + an assistant-side attachment chip.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md
@.planning/phases/37B-web-artifact-sidebar/37B-PATTERNS.md
@web/src/chat/sseAdapter.ts
@web/src/chat/ExternalStoreChat.tsx
</context>

<artifacts_produced>
This plan produces:
- **onArtifact callback** — `onArtifact?: (assetId: string | undefined) => void` added to `StreamSSEOptions`/`StreamRunOptions`/`StreamPostOptions` (sseAdapter) and to the `ExternalStoreChat` prop surface.
- **foldAgentOntoAssistant(messages, agentAssets)** — pure helper in/near `ExternalStoreChat.tsx` mirroring `attachAssetsToUserMessages` but targeting assistant turns, writing `metadata.custom.attachments`.
- **Assistant-side attachment renderer** — reads `messageAttachments(message)` and renders the `LocalArtifactDisplay`-style authenticated download chip on assistant messages.
</artifacts_produced>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Thread onArtifact through the streamSSE pump</name>
  <files>web/src/chat/sseAdapter.ts, web/src/chat/sseAdapter.onArtifact.test.ts</files>
  <behavior>
    - streamSSE fires opts.onArtifact(frame.value.asset_id) exactly when frame.type==='CUSTOM' && frame.name==='aura.artifact' && isArtifactDescriptor(frame.value)
    - reduceFrame is unchanged in its return contract (still a pure reducer, no callback emitted from it)
    - a frame with no asset_id still fires onArtifact(undefined) OR is skipped — assert the chosen contract (recommend fire with undefined so auto-open can still trigger on descriptor presence)
    - onArtifact is optional; absence does not change existing behavior
  </behavior>
  <read_first>
    - web/src/chat/sseAdapter.ts:223-229 (ArtifactDescriptor), :345-363 (the aura.artifact reducer branch), :497-517 (streamSSE pump + StreamSSEOptions) — PATTERNS "sseAdapter.ts (MODIFY)" cites all three.
    - .planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md "Pattern 3 — onArtifact signal".
  </read_first>
  <action>
    In `web/src/chat/sseAdapter.ts`, add an optional `onArtifact?: (assetId: string | undefined) => void` to `StreamSSEOptions` (and thread it through `StreamRunOptions`/`StreamPostOptions` exactly how `onUpdate`/`newId` are threaded). In the `streamSSE` frame loop, after `reduceFrame(state, frame)`, when `frame.type === 'CUSTOM' && frame.name === 'aura.artifact' && isArtifactDescriptor(frame.value)`, call `opts.onArtifact?.(frame.value.asset_id)`. Do NOT emit any callback from inside `reduceFrame`. Write `sseAdapter.onArtifact.test.ts` (mirror the existing `replay.spec`/`sseFromFrames` harness) driving an `aura.artifact` frame and asserting `onArtifact` fires with the asset_id, and does not fire for unrelated frames.
  </action>
  <acceptance_criteria>
    - `onArtifact` appears in `StreamSSEOptions`/`StreamRunOptions`/`StreamPostOptions` and is called in the pump, not in `reduceFrame`.
    - `cd web && npx vitest run src/chat/sseAdapter.onArtifact.test.ts` exits 0.
    - `cd web && npx tsc --noEmit` exits 0; existing sseAdapter tests stay green.
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && grep -q "onArtifact" src/chat/sseAdapter.ts && npx vitest run src/chat/sseAdapter.onArtifact.test.ts && npx tsc --noEmit && echo SSE_ONARTIFACT_OK</automated>
  </verify>
  <done>onArtifact(assetId) fires from the pump on aura.artifact frames; reduceFrame purity preserved; test green.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: onArtifact prop + D-15 split-fold rehydration on history load</name>
  <files>web/src/chat/ExternalStoreChat.tsx, web/src/chat/ExternalStoreChat.rehydration.test.tsx</files>
  <behavior>
    - foldAgentOntoAssistant(messages, agentAssets) attaches agent assets to assistant messages via metadata.custom.attachments (positional heuristic mirroring attachAssetsToUserMessages)
    - on history load, assets split: uploads (source_kind !== 'agent') → attachAssetsToUserMessages (unchanged); agent (source_kind === 'agent') → foldAgentOntoAssistant
    - an agent asset never attaches to a user message (regression assertion on the bug)
    - the assistant-side renderer renders the authenticated /api/assets/{id}/download chip for a folded agent asset (download present with no reload)
    - ExternalStoreChat forwards its onArtifact prop into streamRun/streamPost (mirroring onUsage)
    - user-upload folding is byte-for-byte unchanged
  </behavior>
  <read_first>
    - web/src/chat/ExternalStoreChat.tsx:49-60 (messageAttachments/withMessageAttachments envelope), :62-85 (attachAssetsToUserMessages positional zip), :133-134 (onUsage prop shape to mirror), :295-303 (the history-load setMessages call site to split), :162-169 (invalidateRuntimeReads) — all cited in PATTERNS "ExternalStoreChat.tsx (MODIFY)".
    - .planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md "Pattern 3 — D-15 split-fold" (root-cause + the exact split call).
    - web/src/chat/displays/LocalArtifactDisplay.tsx:65-89 — the download chip markup the assistant-side renderer reuses.
  </read_first>
  <action>
    In `web/src/chat/ExternalStoreChat.tsx`: add `readonly onArtifact?: (assetId: string | undefined) => void;` to the props (mirror the `onUsage` doc-comment) and forward it into the `streamRun`/`streamPost` calls. Add `foldAgentOntoAssistant(messages, agentAssets)` — mirror `attachAssetsToUserMessages`'s positional zip but target `role === 'assistant'` and write the `metadata.custom.attachments` envelope via `withMessageAttachments`. Change the history-load call site (`:303`) from `setMessages(attachAssetsToUserMessages(loaded, assets))` to split first: `const uploads = assets.filter(a => a.source_kind !== 'agent'); const agent = assets.filter(a => a.source_kind === 'agent'); setMessages(foldAgentOntoAssistant(attachAssetsToUserMessages(loaded, uploads), agent));`. Add a small assistant-side attachment renderer (in the assistant message rendering path) that reads `messageAttachments(message)` and renders the `LocalArtifactDisplay.tsx:66`-style authenticated download anchor. Write `ExternalStoreChat.rehydration.test.tsx` asserting: agent assets fold onto assistant turns (never user), uploads still fold onto user turns, the assistant chip renders the `/api/assets/{id}/download` href, and `onArtifact` is forwarded into the stream call.
  </action>
  <acceptance_criteria>
    - `ExternalStoreChat.tsx` defines `foldAgentOntoAssistant` and the history-load path splits by `source_kind` before folding.
    - a test asserts a `source_kind:'agent'` asset attaches to an assistant message and NOT to any user message.
    - the assistant chip href is `/api/assets/{id}/download`.
    - `onArtifact` prop is declared and forwarded into `streamRun`/`streamPost`.
    - `cd web && npx vitest run src/chat/ExternalStoreChat.rehydration.test.tsx` exits 0; `npx tsc --noEmit` exits 0.
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && grep -q "foldAgentOntoAssistant" src/chat/ExternalStoreChat.tsx && grep -q "onArtifact" src/chat/ExternalStoreChat.tsx && npx vitest run src/chat/ExternalStoreChat.rehydration.test.tsx && npx tsc --noEmit && echo REHYDRATION_OK</automated>
  </verify>
  <done>History load splits assets by source_kind; agent assets rehydrate onto assistant turns with a durable download chip; uploads unchanged; onArtifact forwarded; tests green.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| SSE frame stream → runtime state | A CUSTOM frame drives a client callback + query invalidation |
| persisted history → rehydrated messages | Wrong fold mis-attributes a file to the wrong turn/role |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37B-13 | Tampering (mis-attribution) | history-load fold | mitigate | Split by `source_kind`: agent → assistant, uploads → user; regression test asserts agent never lands on a user turn |
| T-37B-14 | Information Disclosure (path leak on rehydrated chip) | assistant-side renderer | mitigate | Chip uses only `asset_id` → `/api/assets/{id}/download` (37A-proven auth path); never `object_key`/host path |
| T-37B-15 | Reliability (purity break) | reduceFrame | mitigate | onArtifact fired only at the streamSSE pump; reduceFrame stays a pure reducer (no side-effect callback) |
</threat_model>

<verification>
- `npx vitest run src/chat/sseAdapter.onArtifact.test.ts src/chat/ExternalStoreChat.rehydration.test.tsx` green.
- Existing `ExternalStoreChat`/`sseAdapter` tests stay green (upload fold non-regression).
- `npx tsc --noEmit` clean.
</verification>

<success_criteria>
- onArtifact(assetId) is emitted from the pump on aura.artifact frames and forwarded through ExternalStoreChat.
- Saved-conversation load rehydrates agent deliverables onto assistant turns with a durable download chip; upload folding unchanged.
</success_criteria>

<output>
Create `.planning/phases/37B-web-artifact-sidebar/37B-05-SUMMARY.md` when done.
</output>
</content>
