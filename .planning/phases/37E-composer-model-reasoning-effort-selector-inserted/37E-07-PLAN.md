---
phase: 37E-composer-model-reasoning-effort-selector-inserted
plan: 07
type: execute
wave: 5
depends_on: ["37E-06", "37E-03"]
files_modified:
  - web/src/chat/composer/useReasoningCapabilities.ts
  - web/src/chat/composer/useReasoningEffort.ts
  - web/src/chat/composer/api.ts
  - web/src/chat/auraRunBody.ts
  - web/src/chat/sseAdapter.ts
  - web/src/chat/ExternalStoreChat.tsx
  - web/src/chat/Composer.tsx
  - web/src/i18n/en.json
  - web/src/i18n/it.json
  - web/src/chat/composer/__tests__/reasoningEffort.test.ts
  - web/tests/e2e/composer-effort.spec.ts
autonomous: true
requirements: [WEBMODEL-01, WEBMODEL-03]
must_haves:
  truths:
    - "The Composer renders a compact, accessible reasoning-effort selector showing ONLY the levels the active model advertises (dynamic, D-13) — never a hard-coded 7, never a placebo"
    - "The selected level rides `POST /agent/run` as `aura.effort` for the six fixed levels and is OMITTED for `auto`; it persists per-conversation and restores on thread reopen"
    - "The selector degrades to `{auto,off}` when detection fails, does NOT break the `/`-skill picker, Enter-send, paste, or drop, and has en+it label parity"
  artifacts:
    - path: "web/src/chat/composer/useReasoningEffort.ts"
      provides: "per-conversation persisted effort state (hydrate on threadId, clamp unsupported→auto, no clear on send)"
      contains: "useReasoningEffort"
    - path: "web/src/chat/composer/useReasoningCapabilities.ts"
      provides: "capability fetch + degrade-to-floor"
      contains: "useReasoningCapabilities"
    - path: "web/tests/e2e/composer-effort.spec.ts"
      provides: "e2e: pick → send with aura.effort → reopen → restored; only advertised levels shown"
      contains: "effort"
  key_links:
    - from: "web/src/chat/Composer.tsx"
      to: "web/src/chat/composer/useReasoningCapabilities.ts"
      via: "selector renders capabilities.levels dynamically"
      pattern: "levels"
    - from: "web/src/chat/auraRunBody.ts"
      to: "POST /agent/run"
      via: "effort folded into the aura envelope for fixed levels, omitted for auto"
      pattern: "effort"
---

<objective>
Ship the Composer capability-aware reasoning-effort selector (WEBMODEL-01/03, D-01/D-02/D-13). A compact ARIA selector near the Send affordance renders ONLY the levels the active model advertises (fetched from `GET /api/composer/reasoning-capabilities`), holds a per-conversation persisted value (hydrated from the conversation DTO's `reasoning_effort`, plan 03), folds the chosen fixed level into the `aura.effort` run-body field (omitting `auto`), and restores on thread reopen. It degrades to `{auto,off}` on detection failure and must not disrupt the `/`-skill picker, Enter-send, paste, or drop (37D precedent). New state/fetch logic is EXTRACTED to hooks so `ExternalStoreChat.tsx` and `sseAdapter.ts` stay ≤600 LOC.

Purpose: the user-facing surface that makes the whole phase real. Depends on plan 06 (endpoint + run field) and plan 03 (DTO hydration field).
Output: two hooks, the fold, the selector, en+it i18n, vitest unit + Playwright e2e.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-CONTEXT.md
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-RESEARCH.md
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-PATTERNS.md
@.planning/phases/37D-composer-skill-picker/37D-CONTEXT.md
@web/src/chat/Composer.tsx
@web/src/chat/ExternalStoreChat.tsx
@web/src/chat/auraRunBody.ts
@web/src/chat/composer/usePinnedSkill.ts
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Capability + effort hooks, the fetch client, the run-body fold, the options type</name>
  <files>web/src/chat/composer/useReasoningCapabilities.ts, web/src/chat/composer/useReasoningEffort.ts, web/src/chat/composer/api.ts, web/src/chat/auraRunBody.ts, web/src/chat/sseAdapter.ts, web/src/chat/composer/__tests__/reasoningEffort.test.ts</files>
  <read_first>
    - web/src/chat/composer/usePinnedSkill.ts (the per-turn state hook to mirror — but effort is per-conversation, does NOT clear on send) and web/src/chat/composer/useComposerSkills.ts + api.ts:25-32 (`fetchComposerSkills` mount-fetch + degrade-on-throw)
    - web/src/chat/auraRunBody.ts:8-19 (the `skill` fold to extend) + sseAdapter.ts:466-483 (`StreamRunOptions` — `skill?: string` to mirror)
    - 37E-RESEARCH.md §P2.6 (dynamic capability-driven selector, clamp unsupported→auto) + §6 (frontend seam) + 37E-PATTERNS.md "Frontend send-payload + hydrate" section
  </read_first>
  <behavior>
    - `fetchReasoningCapabilities()` → `{levels,default,detected}`; on ANY throw → `{levels:['auto','off'],default:'auto',detected:false}` (degrade, never break).
    - `useReasoningEffort(threadId, hydratedEffort)`: hydrates from the conversation DTO's `reasoning_effort` on `threadId` change (default 'auto'); exposes `effort`/`setEffort`; does NOT clear on send; if the stored value is not in the current `levels`, clamp to 'auto'.
    - `buildAuraRunBody`: folds `effort` for the six fixed levels, OMITS it for 'auto' (`opts.effort && opts.effort !== 'auto'`).
    - `StreamRunOptions` gains `readonly effort?: string`.
    - vitest: fold omits auto / includes fixed; capability degrade; clamp unsupported→auto.
  </behavior>
  <action>
    Create `web/src/chat/composer/api.ts` `fetchReasoningCapabilities` (GET `/api/composer/reasoning-capabilities` via the existing `getJSON` helper, degrade to `{levels:['auto','off'],default:'auto',detected:false}` on throw). Create `useReasoningCapabilities.ts` (mount-fetch + degrade, mirror `useComposerSkills`). Create `useReasoningEffort.ts` — per-conversation persisted state: hydrate from the passed conversation `reasoning_effort` on `threadId` change (default 'auto'), expose `effort`/`setEffort`, NEVER clear on send, clamp a stored value not in `levels` back to 'auto'. Extend `auraRunBody.ts` `buildAuraRunBody` with `...(opts.effort && opts.effort !== 'auto' ? { effort: opts.effort } : {})`. Add `readonly effort?: string;` to `StreamRunOptions` in sseAdapter.ts. Write the vitest FIRST. Keep sseAdapter.ts ≤600 LOC (it is at the cap — add ONLY the one field, no logic).
  </action>
  <acceptance_criteria>
    - `cd web && pnpm vitest run reasoningEffort` passes: fold omits 'auto' + includes fixed levels; capability fetch degrades to the floor on throw; unsupported stored value clamps to 'auto'.
    - `StreamRunOptions` has `effort?: string`; `buildAuraRunBody` folds it correctly.
    - `useReasoningEffort` does NOT clear on send (no analog to the pinned-skill clear-after-send).
    - sseAdapter.ts and any touched file remain ≤600 LOC.
  </acceptance_criteria>
  <verify>
    <automated>cd web && pnpm vitest run reasoningEffort</automated>
  </verify>
  <done>The state/fetch/wire layer exists as hooks (600-LOC safe), sends the fixed effort, omits auto, and clamps to advertised levels.</done>
</task>

<task type="auto">
  <name>Task 2: The Composer selector control + ExternalStoreChat wiring + en/it i18n</name>
  <files>web/src/chat/Composer.tsx, web/src/chat/ExternalStoreChat.tsx, web/src/i18n/en.json, web/src/i18n/it.json</files>
  <read_first>
    - web/src/chat/Composer.tsx:14 (`useTranslation` already imported), :48-55 (`ComposerProps`), :370-405 (the `/`-picker combobox + Send affordance — must NOT break)
    - web/src/chat/ExternalStoreChat.tsx:151 (send spread — mirror for effort but do NOT clear), :169 (pinned-skill clear — effort has NO analog), :506-513 (Composer props — mirror `pinnedSkill`/`onPinSkill`)
    - 37D en/it i18n keys (the parity pattern) + 37E-RESEARCH.md §P2.6 (7 labels; suggested it: auto/off/basso/medio/alto/extra/max)
  </read_first>
  <action>
    Add a compact, accessible reasoning-effort selector to Composer.tsx near the Send affordance (Claude's discretion: a small pill/segmented control or a `<select>` with an `aria-label`; if a radiogroup, use `role="radiogroup"` with labelled options). It renders `capabilities.levels` DYNAMICALLY (D-13 — never a hard-coded 7), reads `effort`, and calls `onEffortChange`. It must NOT reclassify the input, break the `/`-picker combobox semantics, or interfere with Enter-send/paste/drop. Add `effort`, `onEffortChange`, and `effortLevels` (or a `capabilities` prop) to `ComposerProps`. In ExternalStoreChat.tsx: destructure `useReasoningCapabilities()` + `useReasoningEffort(threadId, hydratedEffort)`; pass `effort`/`onEffortChange`/levels to `<Composer>` (mirror the `pinnedSkill`/`onPinSkill` props); include `effort` in the `streamRun({...})` call (mirror the pinned-skill spread) but do NOT clear it after send. Add the 7 level labels + the selector aria-label to BOTH `en.json` and `it.json` under a `composer.effort.*` namespace (en: auto/off/low/mid/high/extra/max; it: auto/off/basso/medio/alto/extra/max) — parity-checked by the existing i18n CI. Follow the Frontend_aesthetics guidance in CLAUDE.md — distinctive, cohesive, not generic. Keep Composer.tsx and ExternalStoreChat.tsx ≤600 LOC (all state lives in the Task-1 hooks).
  </action>
  <acceptance_criteria>
    - `cd web && pnpm vitest run Composer` passes: the selector renders exactly the advertised `levels` (+ auto), shows the hydrated value, and calls `onEffortChange`; the `/`-picker and Enter-send paths are unaffected.
    - `en.json` and `it.json` both define the `composer.effort.*` labels + aria-label with matching keys (i18n parity CI green).
    - The selector has an accessible name (aria-label or associated label).
    - ExternalStoreChat.tsx includes `effort` in the send call and does NOT clear it after send; both files ≤600 LOC.
  </acceptance_criteria>
  <verify>
    <automated>cd web && pnpm vitest run Composer && pnpm run i18n:check 2>/dev/null || cd web && node scripts/i18n-parity.mjs 2>/dev/null; echo done</automated>
  </verify>
  <done>The Composer shows a real, accessible, capability-driven effort selector with en/it parity that carries the choice on send without disrupting existing input behavior.</done>
</task>

<task type="auto">
  <name>Task 3: Playwright e2e — pick → send with aura.effort → reopen → restored; dynamic levels</name>
  <files>web/tests/e2e/composer-effort.spec.ts</files>
  <read_first>
    - The existing 37D composer Playwright spec (the send-and-assert-payload + reopen pattern) + web/playwright.config.ts
    - 37E-RESEARCH.md §Validation (e2e rows: pick→send→reopen restored; a model advertising a subset shows only those + auto) + 37E-VALIDATION.md
  </read_first>
  <action>
    Create `web/tests/e2e/composer-effort.spec.ts` (mock/route the capabilities endpoint + run + conversation DTO as the 37D spec does): (1) with a model advertising `{none,low,high}`, assert the selector shows exactly auto/off/low/high (NOT the full 7 — D-13); (2) pick `high`, send, assert the intercepted `POST /agent/run` body carries `aura.effort: "high"`; (3) reopen the thread, assert the selector restores to the persisted level; (4) on `auto`, assert the run body OMITS `effort`. Tag/name it so `pnpm test:e2e -g reasoning-effort` (or `-g composer-effort`) selects it. It must NOT depend on a live LLM/OpenRouter — route the endpoints.
  </action>
  <acceptance_criteria>
    - `cd web && pnpm test:e2e -g composer-effort` (or the repo's e2e invocation) passes all four assertions.
    - The spec routes the capability/run/conversation endpoints — no live backend dependency.
    - A subset-advertising model shows only its levels + auto (dynamic, not the full 7).
  </acceptance_criteria>
  <verify>
    <automated>cd web && pnpm test:e2e -g composer-effort</automated>
  </verify>
  <done>End-to-end: the selector is capability-driven, sends the fixed effort, omits auto, and restores per-conversation — proven without a live model.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries
| Boundary | Description |
|----------|-------------|
| SPA → `POST /agent/run` (`aura.effort`) | The client-chosen symbol; authoritative validation is server-side (plan 06). |
| capability endpoint → SPA | The advertised levels shown to the user. |

## STRIDE Threat Register
| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37E-07-ADVISORY | EoP / Tampering | client-chosen effort | mitigate | The selector is ADVISORY only; the server two-stage-validates every fixed level (plan 06 Stage 1+2 → 400). A tampered client sending an unadvertised/non-enum effort is rejected server-side. The UI clamps unsupported stored values to `auto` as defense-in-depth. |
| T-37E-07-DEGRADE | Availability | capabilities endpoint down | mitigate | The fetch degrades to `{auto,off}` (37D D-09); a failed capability fetch never breaks the Composer or blocks sending. Tested (degrade path + e2e route). |
| T-37E-07-INPUT | Tampering | disrupting `/`-picker / Enter-send | mitigate | The selector must not reclassify the input or break the combobox/Enter-send/paste/drop (37D precedent); vitest + e2e assert existing input behavior is preserved. |
| T-37E-07-XSS | XSS | rendering the effort value | mitigate | The effort is rendered as a controlled selector value from a fixed symbol set, never as raw HTML; labels come from i18n resources, not user input. |

No new npm packages (all deps already in web/package.json — RESEARCH Package Legitimacy: vacuously satisfied). No new auth surface.
</threat_model>

<verification>
- `cd web && pnpm vitest run` green (hooks, fold, Composer selector).
- `cd web && pnpm test:e2e -g composer-effort` green.
- i18n en+it parity CI green; ExternalStoreChat.tsx, sseAdapter.ts, Composer.tsx all ≤600 LOC.
- Manual-Only (deferred to /gsd-verify-work): graduated-effort FIDELITY on a real backend (D-09) — assert only on/off in CI; do NOT assert low<mid<high on DeepSeek.
</verification>

<success_criteria>
- The Composer shows a dynamic, accessible, capability-driven effort selector; the choice sends via `aura.effort` (auto omitted), persists per-conversation, and restores on reopen.
- Degrades safely; does not disrupt existing Composer input; en+it parity.
- WEBMODEL-01 (selector + persist) + WEBMODEL-03 (advisory client, server-authoritative, e2e) covered.
</success_criteria>

<output>
Create `.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-07-SUMMARY.md` when done.

**PHASE-CLOSE STEP (this is the FINAL 37E plan — MANDATORY before the phase-close `git push`):** Per CLAUDE.md "QUALITY SNAPSHOT AT PHASE CLOSE", 37E touched `internal/llm`, `internal/agent`, `internal/agui`, `internal/conversations`, `internal/runner`, `internal/settings`, `cmd/aura`, and `web/src/chat`. Update `docs/aura-quality-snapshot.md`: for EVERY row whose CI-gate-path glob matches a file changed in this phase, bump `Last measured` to today and PREPEND a re-attestation note (a fresh measurement if the metric moved, else a metric-neutral justification naming exactly what changed and why the number cannot move — keep the prior notes as `Prior …`). Then verify LOCALLY FIRST — it MUST print `ok: … checked N row(s)` — with the exact CLAUDE.md invocation:
`AURA_QUALITY_CHANGED_FILES="$(git diff --name-only origin/master..HEAD)" AURA_QUALITY_BASE_DATE="$(git log -1 --format=%cs origin/master)" bash scripts/quality_snapshot_gate.sh`
The CI job `scripts/quality_snapshot_gate.sh` FAILS the phase-close push otherwise (PRD amendment #20). Do NOT push until this prints `ok`.
</output>
