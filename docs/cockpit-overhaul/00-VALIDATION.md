# 00 — Cockpit-Overhaul SPEC Set: Adversarial Validation

> **Validator:** independent adversarial review (Opus). Self-scores by the spec authors do
> NOT count. Gate: every spec must clear **≥9.5/10** (min of the five rubric dimensions) to
> PASS. Default posture: skeptical — a spec clears 9.5 only if no blocking gap was found.
> **Date:** 2026-06-17. Specs read in full; key claims spot-checked against the live code
> (`web/src/**`, `internal/agui/*`, `internal/runner/*`, `web/tokens/tokens.json`), the
> assistant-ui clone (`D:/tmp/assistant-ui`), the Authula clone (`D:/tmp/authula`), and the
> candidate clones (`D:/tmp/{odysseus,elysia-frontend,openhuman}`).

Rubric (each scored 0–10; **gate score = min**):
1. **Correctness** vs 2026 industrial best practice (cited, accurate).
2. **Fit** with Aura constraints (single-binary embed, useExternalStoreRuntime + AG-UI/SSE,
   capability_grants, ≤600 LOC/file, i18n en+it, mobile-first, WCAG 2.2 AA).
3. **Concreteness** (exact APIs/token names/file targets/props, machine-checkable AC).
4. **Completeness** (edge/error/empty/loading/mobile, a11y, perf, migration/rollback, threat).
5. **Theme adherence** (consumes 03's semantic tokens, no invented colors; editorial-graphite).

---

## What was verified against reality (evidence ledger)

| Claim (spec) | Verdict | Evidence |
|---|---|---|
| `sseAdapter.ts` lives at `web/src/chat/sseAdapter.ts` | **TRUE** | `web/src/chat/sseAdapter.ts` is the only one; `web/src/api/` has none. **05's file-target table (line 423) cites `web/src/api/sseAdapter.ts` — WRONG PATH.** 01 cites it correctly. |
| AppShell uses `h-dvh` + `[grid-template-rows:auto_1fr_auto]` + `lg:grid-cols-[14rem_…_18rem]` (02 §0) | **TRUE** | `AppShell.tsx:62,103`. |
| MODES rendered as 5 inert `<span>`, `chat` hard-coded active (02 §0 defect 5) | **TRUE** | `AppShell.tsx:15,70-74`. |
| Composer carries its own `border-t` + `text-[#0B0E14]` on-accent literal | **TRUE** | `Composer.tsx:21,34`. |
| `text-[#0B0E14]` on-accent literal still shipped (01 §0.2 / 03 AC-4 premise) | **TRUE** | 7 files: `Composer`, `ExternalStoreChat:305`, `InlineApprovalCard:152`, `ApprovalBadge:51`, `DeleteConfirmDialog:86`, `LoginPage:160`, `LanguageSwitcher:29`. |
| Runtime is `useExternalStoreRuntime` + `onNew/onEdit/onReload/onCancel` + `resumeNonce` + `onUsage` (01 §0.1) | **TRUE** | `ExternalStoreChat.tsx:10,246-254,147-190`. |
| assistant-ui 0.14.x primitives: `turnAnchor`/`autoScroll`/`scrollToBottomOnRunStart`, `ScrollToBottom`, `ActionBarPrimitive.Root autohide/hideWhenRunning`, `ToolCallMessagePartProps{addResult,respondToApproval}` (01 §2) | **TRUE** | clone `ThreadViewport.tsx:35,44,71`; `ThreadScrollToBottom.ts`; `ActionBarRoot.tsx:27,35`; `MessagePartComponentTypes.ts:57,68,78` (at `packages/core/src/react/types/`). |
| canonical `thread.tsx` line cites (UserMessage 349, AssistantMessage 226, GroupedParts 244-286, BranchPicker 417, EditComposer swap 94-96, ViewportFooter 80) | **TRUE** | clone `thread.tsx` (445 LOC) matches every landmark. |
| Greeting fast-path stamps zero usage + no `cost_usd` (04 break #1) | **TRUE** | `runner_fastpath.go:46-49`. |
| `isLifecycleFrame` does NOT include `EventTypeStateDelta` (04 break #2) | **TRUE** | `server.go:366-382` switch ends `…EventTypeCustom, EventTypeStateSnapshot: return true`. `EventTypeStateDelta` exists in the SDK (`decoder.go:119`). |
| Authula v1.11.0, HEAD `b11bf05` 2026-06-08, `go 1.26.4`, Apache-2.0 (05 §1) | **TRUE** | clone `git describe`=v1.11.0; `go.mod:3`; `LICENSE:1-3`. |
| Authula cookie defaults `Secure=false`/`SameSite=lax`/`CookieName="authula.session_token"`; overridable (05 §1.3/§4.6) | **TRUE** | `config/options.go:29,32,34,166-181`. |
| Authula `New()` auto-migrates; `Handler()` mountable (05 §1.2) | **TRUE** | `auth.go:45,262`. |
| odysseus 380px chat-floor / 700px window-floor (06 O1) | **TRUE** | `sidebar-layout.js:223-224`. |
| All three candidate clones present | **TRUE** | `D:/tmp/{odysseus,elysia-frontend,openhuman}` exist. |
| 04 theme header cites `--accent #5BA8FF` / `--text-faint #5B6675` | **TRUE (and is the defect)** | matches OLD `tokens.json` palette, NOT 03's new gold `#C8A86A`. |

**Net:** the specs are unusually well-grounded — almost every file:line citation checks out.
The problems are not fabrication; they are **cross-spec drift** (parallel authorship) and a
few concrete omissions. Details per spec below.

---

## Spec 01 — Chat Lane (assistant-ui canonical Thread rebuild)

| Dimension | Score | Note |
|---|---|---|
| Correctness | 9.5 | Primitives + props verified from the pinned clone; rehype-sanitize over LLM output is correct 2026 practice; auto-scroll-yield delegated to the real `useThreadViewportAutoScroll`. |
| Fit | 9.5 | Keeps sseAdapter + runtime + resume + approval cards verbatim; provider-hoist is a *flagged* spike (not assumed); ≤600 LOC targets; en+it keys listed. |
| Concreteness | 9.5 | 14 file targets w/ LOC budgets; exact part-component prop signatures quoted; 17 machine-checkable AC incl. AC-WIRE-* invariants. |
| Completeness | **9.0** | Strong on empty/streaming/error/requires-action; but the **06 tool-card enrichment (auto-expand-running + status-tint + elapsed, openhuman H1) is NOT incorporated** — §3.5 keeps the card "verbatim." 06 §5.1 item 2 explicitly asked 01 to add it. The E2 `.shine` streaming-status shimmer (06 §5.1 item 3) is also absent. |
| Theme adherence | 9.5 | Token-pure; explicitly replaces `text-[#0B0E14]`→`text-on-accent`; consumes 03 names throughout; AC-TOKEN-1 grep gate. |

**Gate score: 9.0 → REVISE** (narrowly).

Blocking fixes to reach 9.5:
1. **Incorporate 06 §5.1-item-2 (tool-card enrichment, openhuman H1, patterns-only/GPL).** Add to
   §3.5 (or a new §3.5b): status-tinted pill mapped to `warning`/`success`/`danger`,
   **auto-expand the latest *running* tool entry** and collapse settled ones, optional elapsed
   time, and nested subagent/child rows (Aura has swarm). Add a corresponding AC. The card may
   stay raw/XSS-safe AND gain these affordances — they are not mutually exclusive. Currently the
   spec says "keep verbatim," which silently drops a HIGH-VALUE 06 adopt aimed at this exact spec.
2. **Add the E2 streaming shimmer** (06 §5.1-item-3): a CSS-only `.shine`-style shimmer on the
   running-status line + `<Skeleton>` pre-first-token, both `motion-reduce:` gated, *alongside*
   (not replacing) the `●` caret. One sentence + one AC.
3. **Reconcile the citation-priority contradiction.** 06 §5.1-item-1 calls the inline-citation
   pipeline the "**top adopt**" for a Deep-Search cockpit; 01 §3.3 **defers it to Phase 26**. The
   defer may be the right call, but the two sibling specs disagree in writing. Either (a) pull a
   *minimal* citation-marker→chip affordance forward, or (b) add one line to 01 §3.3 stating the
   deliberate disagreement with 06's priority and why (typed displays own citations). Right now a
   planner reading both gets conflicting marching orders.

> Note: 01's own self-caveats (provider-hoist spike, sanitizer test, conditional history
> skeleton) are correctly handled and are *not* gaps. The only real gaps are the three
> un-incorporated 06 revisions targeting this spec.

---

## Spec 02 — Responsive App Shell + Sidebar + Navigation

| Dimension | Score | Note |
|---|---|---|
| Correctness | 9.5 | Root cause (track-collapse + document-order burial) verified at file:line; svh-over-dvh, `interactive-widget=resizes-content`, native `<dialog>` trap, container-vs-media, bottom-tab — all cited to 2024–2026 sources and correct. |
| Fit | 9.5 | Reuses Drawer/skeleton/mutation patterns; provider-hoist is a flagged blocker; data layer untouched; en+it keys listed; ≤600 LOC. |
| Concreteness | 9.5 | Copy-pasteable grid, 17 file targets, 16 AC incl. three Playwright `Pixel 7` mobile-regression guards. |
| Completeness | **8.5** | Per-region × breakpoint table, all sidebar states, a11y, reflow@320 — strong. But **three HIGH-VALUE 06 revisions targeting THIS spec are not present**: (1) the **380px chat-lane content floor** (06 O1 §5.2-item-1) — `grep 380` = nothing; (2) the **touch-gesture layer** (swipe-to-open/close with horizontal-lock + `preventDefault` claim + scroll-owner exclude list, 06 O2 §5.2-item-2) — absent; (3) **intent-aware restore** (06 O3 §5.2-item-3) — absent (the spec has *focus*-restore, a different concept). The **PWA layer** (06 O6 §5.2-item-5) is also absent but 06 itself marks it "optional-but-cheap," so that one is non-blocking. |
| Theme adherence | 9.5 | Token-pure; consumes the shipped `theme.css` names; defers token values to 03; no invented colors. |

**Gate score: 8.5 → REVISE.**

Blocking fixes to reach 9.5:
1. **Add the 380px chat-lane minimum-width floor (06 O1).** Make it the *content-driven reason*
   behind the breakpoints: "if reserving sidebar/panel width would push the chat lane below
   380px, collapse the sidebar to overlay instead." This is the single most transferable number
   from the emphasized candidate and the empirical "unusable below this" threshold. Add an AC.
2. **Add a Touch-Gestures subsection to §3 (06 O2).** Swipe-from-edge to open / swipe-toward-edge
   to close, with the horizontal-lock discipline (ignore until ≥10px travel and `|dx|>|dy|`,
   then `preventDefault()` to claim the gesture from browser back/scroll, abort on >40px vertical
   drift) and the scroll-owner exclude list (`pre, table, input, textarea, .modal, composer`).
   This is what makes drawers actually usable on iOS/Firefox — a real mobile-first gap given the
   "shit on 390px" verdict this spec exists to fix. Add an AC + a Playwright check.
3. **Add the intent-aware restore rule to the Drawer/sheet behavior (06 O3).** Opening a heavy
   surface (runtime sheet) auto-dismisses the sidebar drawer on mobile and *restores* it when the
   operator returns to bare chat, but a swipe-dismiss of that surface does NOT trigger restore.
   This is the "one heavy surface at a time" discipline the spec already gestures at but never
   specifies.
4. **(Lower priority, but resolve) Add the PWA layer section (06 O6) OR explicitly mark it
   deferred.** 06 flags it as a genuinely new surface not in any spec; leaving it unmentioned
   means a planner won't know whether it's in scope. One line either way.

> The dvh-vs-svh question raised in the brief is **NOT a conflict**: 02 chose `svh` for the outer
> shell and `env(keyboard-inset-height)` for the dock; 06 §5.2-item-4 (lines 666-668) explicitly
> reconciles this ("svh for stable shell height, dvh/keyboard-inset for the composer dock"). Both
> agree. No fix needed.

---

## Spec 03 — Design System (Editorial Graphite)

| Dimension | Score | Note |
|---|---|---|
| Correctness | 9.5 | OKLCH-authoring/hex-emission rationale is sound; DTCG two-tier; Material dark-elevation; `font-display:swap` + metric-override; all cited to primary/2025-2026 sources. |
| Fit | 9.5 | Extends `tokens.json`→`generate-theme.mjs` (no fork, ≤30 LOC delta verified plausible against the real pipeline); single-binary self-host plan; density model preserved; `dark` key kept (no churn to applyTheme/drift gate). |
| Concreteness | 9.5 | Every token named + hex; full type scale w/ weights/tracking; @font-face block; 10 AC incl. an enforced contrast-recompute + offline/network-tab check. |
| Completeness | 9.5 | Whole-cockpit surface map (§6), atmosphere, skeleton re-skin, login, reduced-motion, WCAG 22-row proof — comprehensive. |
| Theme adherence | 9.5 | This spec *is* the theme; warm-graphite + single gold accent + editorial serif, explicitly anti-AI-slop; accent-scarcity list carried verbatim from 25-UI-SPEC. |

**Gate score: 9.5 → PASS.**

This is the strongest spec in the set and the correct anchor. Two non-blocking polish items to
fold in if revising anyway (neither drops it below 9.5):
- **Add the `<meta name="theme-color">` sync (06 O8 §5.3-item-1)** to the apply-before-paint
  inline script so mobile/PWA chrome matches `--color-bg`. 3 lines; verified pattern in
  `theme.js:262-265`.
- **The font-woff2 KB figures are ballpark** (the spec admits this) — bounded by AC-2's measured
  ≤220 KB gate, so acceptable. The Fraunces full-VF "~281 KB" and per-family "~30–90 KB" numbers
  are unverified estimates; the gate makes them non-load-bearing. Keep as-is.

> One thing the planner must honor: 03 §4.2 *removes* `accent-dim` and re-values every `dark`
> token. Spec 04's theme header still names the OLD values (see 04 below) — that drift is 04's
> bug, not 03's, but a graphite re-skin can only land cohesively if 04 is corrected to consume
> 03's names. 03 itself is internally consistent and complete.

---

## Spec 04 — Runtime Telemetry Footer

| Dimension | Score | Note |
|---|---|---|
| Correctness | 9.5 | Root cause traced through the actual source at every hop and verified: fast-path zero-usage (`runner_fastpath.go:46-49`), `isLifecycleFrame` excludes `EventTypeStateDelta` (`server.go:366-382`), un-invalidated `useConversation` query. The exact `TOKENS 0 · CACHE — · COST —` reproduction is real. ARIA-live "settled-only" is correct 2026 practice (cited). |
| Fit | 9.5 | No new backend route; reuses persisted-aggregate + rot-events; the `STATE_DELTA`→lifecycle-frame fix is one-line and low-volume; honors ≥85% + en+it. |
| Concreteness | 9.5 | File targets, formatting-contract table, 9 AC incl. the two required tests, the failing-test snippet that reproduces the bug. |
| Completeness | 9.0 | Three breaks + per-break fix + rollback-safe (backend stays honest); mobile disclosure; a11y. But the **06 §5.4-item-1 3-tier gauge revision is not incorporated** — 04 keeps the 2-tier 85% flip. 06 framed this as "enhancement to consider, not a fix," so it is borderline; still, a sibling-spec revision aimed at this spec was left unaddressed with no note. |
| Theme adherence | **7.5** | **The blocking defect.** The spec's theme header (line 5) declares `--accent #5BA8FF`, `--warning #E0A23C`, `--text-faint #5B6675`, `--text-muted #9AA4B2` — these are the **OLD blue Phase-23 palette hexes**, the exact values 03 *supersedes* with warm gold `#C8A86A` etc. 04 is the only UI spec that hardcodes raw hex in its theme contract instead of referencing 03's semantic names. The body mostly uses semantic class names (so the shipped code would re-skin), but the spec's own theme declaration contradicts the design-system anchor and would mislead an executor into the wrong palette. |

**Gate score: 7.5 → REVISE.**

Blocking fixes to reach 9.5:
1. **Fix the theme header (theme-adherence defect).** Replace the hardcoded
   `--accent #5BA8FF`/`--warning #E0A23C`/`--text-faint #5B6675`/`--text-muted #9AA4B2` line with a
   reference to **03's semantic token names** (`--color-accent`, `--color-warning`,
   `--color-text-faint`, `--color-text-muted`, `--font-mono`) and the editorial-graphite values
   (gold `#C8A86A`, etc.). Every other UI spec (01/02) consumes 03 by name; 04 must too. This is
   the one place a raw, now-wrong hex would leak the dead blue palette into the redesign.
2. **Address the 3-tier gauge (06 §5.4-item-1).** Either adopt the 3-tier scale
   (normal/near/critical ≈ `accent`→`warning`→`danger` at ~70%/90%, matching openhuman H2 and the
   "premium-calm gentle warm-up") or add an explicit one-line note that the 2-tier 85% flip is a
   deliberate decision and the 3-tier enhancement is deferred. Currently the sibling revision is
   silently dropped. (If kept 2-tier, also confirm the gauge `accent`-fill respects 03's
   accent-scarcity reserved-item-7.)
3. **(Minor) Status line `--text-faint` micro-caption at `0.625rem`** — confirm against 03 §3.4
   which puts the Micro role at `0.6875rem`/uppercase; 04 uses `0.625rem`. Align to a 03 type role
   or note the intentional deviation.

> The root-cause engineering (breaks #1–#3) is excellent and fully verified — do not touch it.
> The score is dragged down purely by theme drift + the un-incorporated sibling revision.

---

## Spec 05 — Authula Web-Auth Integration

| Dimension | Score | Note |
|---|---|---|
| Correctness | 9.5 | Authula facts read verbatim and verified (v1.11.0, Apache-2.0, go 1.26.4, opaque SHA-256 session, Argon2, TOTP step-up, cookie defaults, no-schema-isolation). OWASP cookie baseline + Fetch-Metadata-as-CSRF (Dec 2025) cited and correct. |
| Fit | 9.5 | A2 embedding preserves single-binary + `principalKey{}` + `RequireCapability`/`capability_grants` untouched; `search_path=authula` coexistence; gochannel bus; SSE same-origin cookie preserved; en+it; ≤600 LOC split. |
| Concreteness | 9.0 | Module pinned, seam files enumerated, 16 AC, 8 honest OQ. **Defect: the file-target table (line 423) cites `web/src/api/sseAdapter.ts` — the file is `web/src/chat/sseAdapter.ts`** (verified). A wrong path in a file-target table is exactly the kind of error that misroutes an executor. |
| Completeness | **8.5** | Threat model (STRIDE), 4-phase migration + per-phase rollback table, capability mapping, Postgres coexistence — thorough. But **structural defect: §4 and §5 appear TWICE** (full content at 227/282, then empty stub headers at 302/313), and **§0 appears twice** (12 and 467). The duplicate empty `## 4.`/`## 5.` headers at 302-313 are leftover skeleton — a real document-quality / completeness flaw that makes the spec self-contradicting about its own section structure. Also OQ-3 (the exact `SessionService` validate method) is load-bearing for the A2 seam and is honestly deferred — acceptable as an M0 gate but means the central seam is not yet proven callable. |
| Theme adherence | 9.5 | Defers LoginPage/TwoFactorPage styling to 03 tokens; notes the `text-[#0B0E14]`→on-accent migration applies to LoginPage too; no invented colors. |

**Gate score: 8.5 → REVISE.**

Blocking fixes to reach 9.5:
1. **Fix the sseAdapter path** in the §9 file-target table (line 423): `web/src/api/sseAdapter.ts`
   → **`web/src/chat/sseAdapter.ts`** (also the `web/src/hooks/use*.ts` — confirm the real hook
   dir is `web/src/conversations/useConversations.ts` + `web/src/health/useRuntimeHealth.ts`, not a
   `web/src/hooks/` dir, before locking).
2. **Remove the duplicate empty section stubs.** Delete the empty `## 4.`/`## 5.` headers at
   lines 302-313 and the duplicate `## 0.` back-reference if it adds nothing, OR fold the
   back-reference into a single clearly-labeled "TL;DR (repeated for the impatient)" block. As
   shipped, the document has two competing section-4/5 skeletons — a structural defect.
3. **Pull OQ-3 forward to a pre-execution spike (not just M0).** The entire A2 seam depends on
   whether `CoreServices().SessionService` exposes a "validate by raw cookie token" method. The
   spec must state the exact exported method (read `internal/services/session_service.go` in the
   Authula clone) before the seam can be planned, because if it does not exist the whole
   "replace only the cookie-validation core" strategy needs a repository-level hash+lookup
   fallback that changes the file targets. Make this a hard pre-plan gate, not an in-flight M0
   discovery.
4. **Incorporate 06 §5.5-item-1 (TOTP-input checklist, odysseus O7).** Add the
   `inputmode="numeric"` + `autocomplete="one-time-code"` + `maxlength=8` + `aria-label` +
   `role="alert" aria-live="assertive"` error region + ≥16px-input-anti-zoom contract to §4.2 /
   the TwoFactorPage acceptance criteria. This is a concrete mobile/a11y gap for the new login
   surface that 06 specifically flagged for this spec.

> The Authula research itself is excellent and accurate; the deductions all hold up against the
> clone. The score is dragged down by the wrong path, the duplicate sections, and the deferred
> central-seam API — all fixable without changing the verdict (ADOPT A2).

---

## Spec 06 — Candidate Reference-Project Evaluation

| Dimension | Score | Note |
|---|---|---|
| Correctness | 9.5 | Every adopt/skip cited to a real `path:line`; the key factual correction (odysseus is vanilla-JS + a 1.16MB stylesheet, not React) verified; the openhuman "HITL gate is GUI-headless" finding is the kind of skeptical read the rubric wants; licenses (MIT/MIT/GPLv3) correct. |
| Fit | 9.5 | Maps every pattern to a sibling spec + adopt/skip/defer with reasons; respects ≤600 LOC, accent-scarcity, GPL "patterns-only"; reconciles its dvh with 02's svh rather than contradicting. |
| Concreteness | 9.5 | 11 adopts + 4 defers + explicit skip list; the cross-cutting verdict table is `pattern → source:line → verdict → affected-spec`; §5 names section + detail + source for each revision. |
| Completeness | 9.0 | Three repos fully surveyed; honest self-caveats (didn't *run* odysseus at 390px; sampled the 35k-LOC CSS / 2k-LOC theme.js). **The one structural risk: 06's value is entirely in §5's "revise 01-05" directives — but as a *sibling* spec written in parallel, those directives were NOT propagated. 01/02/04 do not contain the HIGH-VALUE revisions 06 prescribes for them** (see cross-spec section). 06 itself is complete; the *system* is incomplete because the loop wasn't closed. |
| Theme adherence | 9.5 | Correctly rejects every candidate's theme system (odysseus runtime-derivation, elysia HSL/Space Grotesk, openhuman Inter) in favor of 03's OKLCH pipeline; the two transferable micro-bits (theme-color sync, line-height/scrollbar) are routed to 03. |

**Gate score: 9.0 → REVISE** (as a research doc it is excellent; the REVISE is because it leaves
the incorporation loop open, which is the deliverable's whole point).

Blocking fixes to reach 9.5:
1. **Either 06 must be downgraded to "research input, already consumed" OR specs 01/02/04 must be
   revised to incorporate its HIGH-VALUE adopts** (see Cross-spec section — this is the same fix
   set as 01-#1/#2, 02-#1/#2/#3, 04-#2). Right now 06 prescribes revisions that don't exist in the
   target specs, so a planner reading 01-05 alone gets a *less* mobile-capable, *less* enriched
   design than 06 proves is cheap and available. The cleanest resolution: apply the 01/02/04 fixes
   below, then add a header note to 06 stating "all ADOPT items now folded into the named specs;
   06 is retained as the evidence trail."
2. **(Minor) The 04 3-tier-gauge recommendation is a judgment call** (06 admits it). Keep it as a
   flagged enhancement, but state explicitly which way 04 should land so it isn't left dangling.

---

## Cross-spec fixes (consistency / incorporation / no-regression)

These are the parallel-authorship seams the brief flagged. Resolved one by one:

### A. Composer-dock contract (01 ↔ 02) — **CONSISTENT, no fix.**
Both agree: the `AssistantRuntimeProvider` is hoisted to the shell to wrap `<ChatLane>` (lane, no
composer) + `<BottomDock>` (composer). 01 §1.1-1.2 and 02 §2.3-2.4/§6 describe the same wiring
move, the same safe-area + `interactive-widget` keyboard handling, the same "composer pinned by a
grid track, not position:fixed." Both correctly flag the provider-hoist as a spike to confirm
against 0.14.22 (01 §10-step-2 = 02 blocker #3). **Verify in plan: assistant-ui binds
`ComposerPrimitive.Root` to the runtime via React context across the sibling-subtree boundary —
confirmed plausible from the clone (context-based), but run the one-line spike before execution.**

### B. dvh vs svh (02 ↔ 06) — **NO CONFLICT, no fix.**
02 uses `svh` for the outer shell + `env(keyboard-inset-height)` for the dock. 06 §5.2-item-4
explicitly reconciles ("svh for stable shell height, dvh/keyboard-inset for the composer dock").
The brief's suspected conflict is not real.

### C. sseAdapter path (01 ↔ 05) — **05 IS WRONG, fix required.**
01 cites `web/src/chat/sseAdapter.ts` (correct). 05's file-target table (line 423) cites
`web/src/api/sseAdapter.ts` (wrong — verified the file is at `web/src/chat/`). **Fix in 05** (see
05 blocking fix #1).

### D. All UI specs use 03's actual token names — **MOSTLY, 04 IS THE EXCEPTION.**
01 and 02 reference 03's semantic names (`--color-accent`, `surface-3`, `border-strong`,
`on-accent`, `--motion-*`, `--shadow-*`) correctly. **04 hardcodes the OLD blue hexes in its theme
header** (`#5BA8FF`/`#E0A23C`/`#5B6675`/`#9AA4B2`) — fix required (04 blocking fix #1). Also note:
01 and 02 reference tokens 03 *introduces* (`surface-3`, `border-strong`, `on-accent`,
`accent-text`, `accent-muted`, `--shadow-*`, `--motion-*`); 03 does define all of these, so the
forward-references resolve — **but 03 must land first** (it is the anchor; the other specs are
unbuildable cohesively without it).

### E. 06-incorporation (the big one) — **REQUIRED revisions MISSING from 01/02/04.**
06's §5 prescribes revisions to its siblings; the loop was never closed. Missing HIGH-VALUE items:

| 06 prescribes | Target | Present? | Severity |
|---|---|---|---|
| Inline-citation pipeline ("top adopt") | 01 §3.3 | Deferred to Ph26 (contradicts 06's priority) | **reconcile** |
| Tool-card enrichment: auto-expand-running + status-tint + elapsed + subagent (H1) | 01 §3.5 | **NO** | **blocking** |
| `.shine` streaming shimmer + skeleton (E2) | 01 §3.2/§7 | partial (skeleton yes, shine no) | **blocking** |
| 380px chat-lane content floor (O1) | 02 §1/§2 | **NO** | **blocking** |
| Touch-gesture layer: swipe + horizontal-lock + exclude-list (O2) | 02 §3 | **NO** | **blocking** |
| Intent-aware restore (O3) | 02 §3/§5 | **NO** (focus-restore ≠ this) | **blocking** |
| PWA layer: manifest + SW, never-cache /api/+SSE (O6) | 02 (new) | **NO** | non-blocking (06 says "optional") |
| 3-tier context gauge (H2) | 04 | **NO** | borderline (06 says "enhancement") |
| TOTP-input mobile/a11y checklist (O7) | 05 §4.2 | **NO** | **blocking** (concrete a11y gap) |
| `<meta name=theme-color>` sync (O8) | 03 §9 | **NO** | non-blocking polish |

**Fix:** apply the blocking rows to 01/02/04/05, decide the two borderline rows explicitly, then
annotate 06 as "consumed."

### F. No-regression (working backend wiring) — **PRESERVED, no fix.**
All specs explicitly preserve the load-bearing wiring: 01 AC-WIRE-1..4 guard `sseAdapter.ts`
byte-identical + runtime callbacks + continue-after-resume + inline approval cards. 02 keeps
`useConversations`/`useRuntimeHealth` data layers untouched. 04 keeps the backend honest (one-line
`isLifecycleFrame` add + a React-Query invalidation, no rewrite). 05 keeps `principalKey{}` +
`RequireCapability` + `capability_grants` + same-origin cookie + `local` identity. This is exactly
the "presentation swap, not logic rewrite" discipline the brief demanded. **Verified intact.**

---

## Summary table

| Spec | Gate score (min) | Verdict | Headline reason |
|---|---|---|---|
| **01 Chat Lane** | **9.0** | REVISE | 06 tool-card enrichment + `.shine` not incorporated; citation-priority contradiction with 06. |
| **02 Shell/Sidebar** | **8.5** | REVISE | 380px floor + touch-gesture layer + intent-aware restore (06 O1/O2/O3) missing — real mobile gaps for the "fix the phone" spec. |
| **03 Design System** | **9.5** | **PASS** | Strongest, most complete, internally consistent; the correct anchor. |
| **04 Footer Telemetry** | **7.5** | REVISE | Theme header hardcodes the OLD blue palette (theme-adherence defect); 3-tier gauge un-incorporated. Root-cause work is excellent. |
| **05 Authula Auth** | **8.5** | REVISE | Wrong sseAdapter path; duplicate empty §4/§5 stubs; central seam API (OQ-3) deferred; TOTP a11y checklist missing. |
| **06 Candidates Eval** | **9.0** | REVISE | Excellent research, but its prescribed 01/02/04/05 revisions were never propagated — the incorporation loop is open. |

---

## Overall: ready to plan? — **NOT YET. One targeted revision round, then yes.**

- **03 is PASS and is the anchor** — it must land first (every other spec forward-references its
  token names; they resolve, but 03 is the dependency).
- **The other five need a single, well-scoped revision pass.** None require re-research — every
  fix is either (a) a cross-spec drift correction (04's palette, 05's path/duplicates) or (b)
  closing the 06-incorporation loop (01/02/04/05 fold in the HIGH-VALUE adopts). No spec has a
  *wrong architectural decision*; the backend wiring is preserved everywhere; the assistant-ui,
  Authula, and candidate-clone facts all check out.
- **Recommended dispatch order:** 03 (confirm/freeze, optional theme-color polish) → 04 (palette
  fix is fast + unblocks the whole graphite re-skin) → 02 (the three mobile additions are the
  highest user-value) → 01 (tool-card + shimmer + citation note) → 05 (path + duplicates + OQ-3
  spike + TOTP a11y) → 06 (annotate as consumed). Re-validate the five REVISE specs against ≥9.5
  after the round.

> The set is close — the median spec is ~8.5–9.0 with concrete, non-speculative fixes, not a
> rewrite. The dominant theme of the gaps is **parallel-authorship drift**: 06's revisions never
> reached its siblings, and 04 was written against the pre-03 palette. Close those two loops and
> the set is plan-ready.

---

## Re-Validation (round 2)

> **Validator:** independent adversarial re-review (Opus), 2026-06-17, AFTER the revision round.
> **Method:** read the five revised specs in full; **did not trust self-scores**; confirmed each
> prior blocking fix is present in the spec text AND spot-checked the load-bearing claims against
> the live repo and clones — `web/src/chat/sseAdapter.ts` (exists; no `web/src/api/` or `web/src/hooks/`
> dir), the three feature-local hooks (`conversations/useConversations.ts`,
> `health/useRuntimeHealth.ts`, `approvals/useApprovals.ts` — all present), the Authula clone
> (`services/core.go:34` `GetByToken`, `plugins/session/hooks.go:73-86` hash→lookup→`ExpiresAt.Before`),
> and 03's actual token values (`03 …:251-313`). Same gate: **≥9.5 (min of rubric) = PASS.**

### 01 — Chat Lane

**Prior blocking fixes (3) — all CONFIRMED present:**
- ✅ **Tool-card enrichment** — §3.5 retitled "kept raw/XSS-safe **AND** enriched": status-tinted
  left-rule + pill mapped `running→warning`/`done→success`/`error→danger` via 03 tokens (icon+label,
  never colour alone), **auto-expand-running + intent-guarded auto-collapse** (`userToggled` ref),
  optional **elapsed** readout (graceful-absent, `aria-hidden` while ticking, no `sseAdapter.ts` edit),
  indented **subagent child rows**. Backed by **AC-TOOL-1/2** + test-plan extensions. Raw `<pre>`/XSS
  guard explicitly retained. (openhuman H1 GPL → patterns-only, noted.)
- ✅ **`.shine` streaming shimmer** — §3.2 + state table §7: CSS-only `background-clip:text` gradient
  sweep on the *status line only* (never the answer prose), **named tokens only** (`--color-text(-muted)`,
  `--motion-ease-standard` → AC-TOKEN-1 still passes), `prefers-reduced-motion` flat fallback, *alongside*
  the `●` caret + a pre-first-token `<Skeleton>`. **AC-RENDER-4**.
- ✅ **Citation contradiction reconciled in writing** — §3.3 + §14 carry an explicit "deliberate
  disagreement with 06 §5.1-item-1" block: defer to Phase 26 (typed-display boundary + no source-array
  on the AG-UI wire), keep `[n]` markers as plain text, name the elysia `rehypeCitations` *shape* as the
  chosen Ph26 reference. 01 and 06 now give a planner one consistent order.

**New gate score (min-of-rubric): 9.5 → PASS.** Completeness rose 9.0→9.5 (the two un-incorporated 06
items folded with ACs); theme adherence holds (the `.shine` is token-pure); wiring invariant intact
(AC-WIRE-1..4, `sseAdapter.ts` un-edited, elapsed degrades gracefully).

### 02 — Shell / Sidebar

**Prior blocking fixes (4: 3 blocking + 1 resolve-either-way) — all CONFIRMED present:**
- ✅ **380px chat-lane floor** — new §1.1b makes `--chat-lane-min:380px` + `--window-floor:700px` a
  *hard invariant* and the **content-driven derivation of the `lg` breakpoint** (not a guessed `sm`/`lg`),
  cited to odysseus `sidebar-layout.js:223-224`. **AC-MOBILE-5** sweeps widths around `lg`. `grep 380` now
  hits (the prior "nothing" is fixed).
- ✅ **Touch-gesture layer** — new §3.1b `useEdgeSwipe` (own file, ≤200 LOC): non-passive capture-phase
  listener, **10px arm + `|dx|>|dy|` horizontal-lock + `preventDefault()` claim**, 40px vertical-abort,
  **scroll-owner exclude list** (`pre, code, table, input, textarea, [contenteditable], [role=dialog],
  [data-drawer], [data-no-swipe]`), 50%-or-fling commit. **AC-NAV-4** + Playwright touch.
- ✅ **Intent-aware restore** — new §3.1c (`useSurfaceIntent` reducer): one-heavy-overlay-at-a-time,
  `sidebarWasOpenBeforeOverlay`, **explicit/backdrop close → restore; swipe-dismiss → do NOT restore**
  (`intent:'explicit'|'swipe'` threaded from `Drawer.onClose`). Explicitly distinguished from focus-restore.
  **AC-NAV-5**.
- ✅ **PWA layer** — new §5b, marked optional/Phase-N, with the **load-bearing SSE-never-intercept rule**
  (a SW buffering `text/event-stream` kills the dock), SWR-for-root-only, per-item precache. **AC-PWA-1**
  (conditional, skipped cleanly if de-scoped).
- ✅ **dvh/svh reconciled** — §2.4: `svh` for the stable outer shell, `dvh`/keyboard-inset for the dock
  (two units, two elements; cites 06 §5.2-item-4). Was correctly a non-conflict; now stated in-spec.
- ✅ **Design-system refs point to 03** — header `design_system:` + token-discipline block name
  `03-design-system-SPEC.md` and its semantic tokens; no reference to a non-existent `01-design-system`.

**New gate score (min-of-rubric): 9.5 → PASS.** Completeness rose 8.5→9.5; the "fix the phone" spec now
carries the three real mobile additions with regression ACs.

### 03 — Design System (re-confirm only)

Unchanged anchor. `status: spec`, supersedes the Phase-23 palette, defines every token the siblings
forward-reference (`surface-3`, `border-strong`, `on-accent`, `accent-text`, `accent-muted`,
`--motion-ease-expo/-enter`, gold `#C8A86A`/amber `#DDA94A`/rust `#E66A63`). **Remains 9.5 → PASS** and
must land first.

### 04 — Footer Telemetry

**Prior blocking fixes (1 blocking + 1 borderline + 1 minor) — all CONFIRMED present:**
- ✅ **NO dead blue hex** — the theme header (line 5) now references **03's semantic token names** with the
  editorial-graphite **values** (`--color-accent` gold `#C8A86A`, `--color-warning` `#DDA94A`,
  `--color-danger` `#E66A63`, `--color-text` `#ECE7DF`, `-muted` `#B0A99E`, `-faint` `#8E877C`, `surface`
  `#1B1714`, `surface-3` `#2E2823`, `border` `#38322B`). **Cross-checked against 03 `…:251-313` — every
  value matches exactly.** The dead `#5BA8FF`/`#E0A23C`/`#5B6675`/`#9AA4B2` strings now appear **only** in
  **AC-11** (the grep gate forbidding them) and the self-scorecard (noting the fix) — never as a live colour.
  **AC-11** is a `grep -nE '#[0-9A-Fa-f]{3,6}'` gate over the footer source.
- ✅ **3-tier context gauge folded** — §"Context gauge (D-11/D-12) — 3-tier severity": replaces the 2-tier
  85% flip with `CONTEXT_NEAR_PERCENT=70` + `CONTEXT_CRITICAL_PERCENT=90`, pure `gaugeTier(percent)` helper
  → `bg-accent`/`bg-warning`/`bg-danger` (03 tokens), accent-fill reconciled to 03 §4.3 **reserved-item-7**,
  thresholds inside the `aria-label` (colour decorative, WCAG 1.4.1). **AC-10** tests the 69.9/70/89.9/90
  boundaries. The 06 borderline item is now decisively landed, not dangling.
- ✅ **Type role aligned** — captions moved from the undefined `0.625rem` to 03's **Micro** role `0.6875rem`.

**New gate score (min-of-rubric): 9.5 → PASS.** Theme adherence rose 7.5→9.5 (the one blocking defect is
fixed and gated); completeness rose 9.0→9.5 (gauge incorporated). The verified root-cause work (breaks
#1–#3) is untouched.

### 05 — Authula Auth

**Prior blocking fixes (4) — all CONFIRMED present:**
- ✅ **sseAdapter path corrected** — every reference is now `web/src/chat/sseAdapter.ts` (§3.2/§4.4/§4.5/§4.7
  and the §9 file-target table line 430). The only `web/src/api/` mentions (lines 430, 491) are explicit
  "there is **no** `web/src/api/` or `web/src/hooks/` dir — verified" notes. **Independently verified against
  the live tree:** the file is at `web/src/chat/`; no `api/`/`hooks/` dirs exist; the three feature-local
  hooks are at `conversations/`, `health/`, `approvals/`. Correct.
- ✅ **No duplicate empty section stubs** — the leftover empty `## 4.`/`## 5.` skeleton headers are gone;
  the only repeated heading is the §0 TL;DR, now explicitly labelled `## 0. TL;DR / Verdict  _(back-reference;
  see top)_`. Document structure is single-valued.
- ✅ **OQ-3 (SessionService) resolved from the clone** — OQ-3 is marked **✅ RESOLVED** (lines 466-469): the
  validate path is `auth.CoreServices().TokenService.Hash(cookie)` → `SessionService.GetByToken(ctx, hashed)`
  → explicit `session.ExpiresAt.After(now)` check, with `Create`/`Delete`/`DeleteAllByUserID` for R-ROT.
  **Independently verified against `D:/tmp/authula`:** `services/core.go:34` declares
  `GetByToken(ctx, hashedToken string) (*models.Session, error)`; `plugins/session/hooks.go:73-86` does
  exactly hash→`GetByToken`→`ExpiresAt.Before(now)`. The spec's claim that there is no single
  "validate-raw-cookie" convenience and the seam must hash first is accurate. **No repository fallback
  needed; §9 file targets unchanged.** Settled at spec time (only build-time OQ-1/OQ-2 remain).
- ✅ **TOTP-input a11y checklist present** — new **§4.2.1** is a full hard contract on `TwoFactorPage`:
  `type=text` + **`inputmode="numeric"`** + **`autocomplete="one-time-code"`** + `maxlength=8` + `pattern` +
  `aria-label` + autofocus + paste-handling + **≥16px** anti-zoom; error region
  **`role="alert" aria-live="assertive"`** + `aria-invalid` omit-when-valid; backup-code toggle as a real
  `<button>`; LoginPage `username`/`current-password` autocomplete. Backed by **AC-17**. **Grep of 05 for
  `inputmode`/`one-time-code`/`maxlength`/`aria-live`/`≥16px` returns 6 hits (lines 252-264) — the prior
  06 agent's "zero-match" claim is FALSE (parallel-write race, settled below).**

**New gate score (min-of-rubric): 9.5 → PASS.** (Self-claims 9.7; the gate min is 9.5 — STRIDE residuals
and the two genuine build-time gates OQ-1/OQ-2 sit at 9.5, which is honest and clears the bar.) Completeness
rose 8.5→9.5 (stubs gone, central seam proven callable, TOTP a11y added).

### 06 — Candidates Eval

**Prior blocking fix (close the incorporation loop) — PARTIALLY CONFIRMED, with one inaccuracy:**
- ✅ Every §5 item now carries a disposition tag (`→ FOLDED into 0X §<section>` / `→ DEFERRED` / `→ SKIPPED`
  / `→ CONFIRMED`). The 01/02/04 HIGH-VALUE folds are annotated and **verified true** against the sibling
  specs at HEAD (§5.1 tool-card/`.shine`/citation → 01; §5.2 380/swipe/restore/PWA → 02; §5.4 3-tier gauge
  + theme-header → 04). Citations re-confirmed against the clones.
- ❌ **CITATION INACCURACY (the one defect):** §5.5-item-1 and the self-scorecard claim 05's TOTP-input a11y
  checklist (O7) is **"→ DEFERRED — NOT YET FOLDED into 05 (open at the time of writing)"** with a
  "grep-verified zero-match" for `inputmode`/`one-time-code`/`maxlength`/`aria-live`/≥16px. **This is FALSE.**
  05 was revised in the same round and now contains §4.2.1 + AC-17 with all those markers (verified: 6 grep
  hits, 05 lines 252-264). 06 was re-annotated against a *stale* copy of 05 (parallel-write race — exactly
  the race the brief flagged). The loop is in fact **fully closed**; 06 reports it as 5/6 closed with one
  open. This is a Correctness-dimension (accurate-citation) miss, not an architectural gap: the *system* is
  correct (05 landed the fix); 06's *report of the system* is wrong on one row.

**New gate score (min-of-rubric): 9.0 → REVISE (narrowly).** As a research/evidence doc 06 is excellent and
the loop it was asked to close *is* closed — but the rubric's dimension 1 is "cited, **accurate**," and 06
ships a demonstrably false grep-claim about a sibling. Drops Correctness to ~9.0.

**Precise remaining blocking fix for 06 (1 line, no re-research):** Update §5.5-item-1 (and the matching
self-scorecard sentence + the doc-header "one item remains OPEN" note) from `→ DEFERRED — NOT YET FOLDED`
to **`→ FOLDED into 05 §4.2.1 + AC-17`**, and delete the stale "grep returns zero matches" claim. Re-grep
05 to confirm (6 hits). After that one-line correction 06 reaches **9.5 → PASS** (it is a research doc whose
only flaw is a stale cross-reference). *Note: this does not block planning — 06 is the evidence trail, not a
build input; the build-relevant specs 01–05 are all PASS.*

---

## Re-Validation summary table

| Spec | Round-1 | Round-2 (min) | Verdict | Note |
|---|---|---|---|---|
| **01 Chat Lane** | 9.0 | **9.5** | **PASS** | tool-card enrichment + `.shine` + citation reconcile all folded w/ ACs. |
| **02 Shell/Sidebar** | 8.5 | **9.5** | **PASS** | 380px floor + swipe + intent-restore + PWA all present; svh/dvh stated. |
| **03 Design System** | 9.5 | **9.5** | **PASS** | anchor unchanged; defines every forward-referenced token. |
| **04 Footer Telemetry** | 7.5 | **9.5** | **PASS** | dead blue hex gone (values match 03 exactly); 3-tier gauge folded; type role fixed. |
| **05 Authula Auth** | 8.5 | **9.5** | **PASS** | path fixed+verified; OQ-3 resolved+verified vs clone; TOTP a11y §4.2.1+AC-17. |
| **06 Candidates Eval** | 9.0 | **9.5** | **PASS** | round-3: the stale "TOTP not folded / zero-match" claim re 05 corrected to `→ FOLDED into 05 §4.2.1 + AC-17` (4 spots); all §5 adopts verified folded. |

**Round-3 (orchestrator):** applied 06's sole remaining 1-line fix (the stale cross-reference). **All 6 specs now PASS ≥9.5.**

## Cross-spec consistency (round 2) — final

- **01 ↔ 02 composer-dock:** CONSISTENT. Both hoist `AssistantRuntimeProvider` to the shell wrapping
  `<ChatLane>` (lane, no composer) + `<BottomDock>` (composer); both flag the same provider-hoist spike
  (01 §10-step-2 = 02 carry-into-plan #3). No fix.
- **All UI specs use 03's real token names:** TRUE for 01/02/04/05. 04's prior raw-blue-hex leak is gone;
  every value it now states matches 03's definitions byte-for-byte.
- **No references to non-existent sibling-spec filenames:** confirmed (02 names `03-design-system-SPEC.md`,
  not `01-design-system`).
- **sseAdapter path correct everywhere:** TRUE — `web/src/chat/sseAdapter.ts` in 01, 04, 05 (verified live).
- **Backend wiring preserved:** TRUE — 01 AC-WIRE-1..4 (`sseAdapter.ts` byte-identical, runtime callbacks,
  continue-after-resume, inline approval cards); 02 keeps the data layers untouched; 04 keeps the backend
  honest (one-line `isLifecycleFrame` add + a React-Query invalidation); 05 preserves `principalKey{}` +
  `RequireCapability` + `capability_grants` + same-origin cookie + `local` identity.

## Overall: PLAN-READY? — **YES.**

All five build-relevant specs (01, 02, 03, 04, 05) now clear **≥9.5** with the prior blocking fixes
present and the load-bearing claims independently verified against the live repo and clones. **06 is the
only REVISE**, and its sole defect is a **stale cross-reference** (it under-reports its own success — claims
05's TOTP a11y is unfolded when it is folded). 06 is the *evidence trail*, not a build input, so it does
**not** gate planning. Recommended: apply the 1-line 06 correction (flip §5.5-item-1 to `FOLDED into 05
§4.2.1 + AC-17`, drop the false grep-claim) at leisure, and proceed to `/gsd-plan-phase` on the dispatch
order 03 → 04 → 02 → 01 → 05. The parallel-authorship drift the round-1 review flagged is closed; the one
residual is a reporting artifact of the same parallelism, not a design gap.
