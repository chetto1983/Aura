# Aura Calm Prism Chat Refinement Design

- **Date:** 2026-07-15
- **Revision:** 2 — adversarial-review amendments
- **Status:** Direction approved; revised contract awaiting second-round adversarial approval.
- **Surface:** Aura authenticated chat cockpit in light and dark themes, desktop and mobile.
- **Approach:** Targeted refinement of Aura's existing semantic-token design system.
- **Browser evidence:** Live Aura inspected with user-approved Playwright using the Google Chrome channel.

## 1. Outcome

Aura's chat should feel quieter during conversation, clearer while work is running, and richer when a result deserves structure. The approved direction is **Calm Prism**:

1. **Quiet conversation** — prose and user messages carry the reading experience without extra card chrome.
2. **Expressive progress** — reasoning, tools, and approvals reveal state through concise, semantic rows.
3. **Rich results** — typed displays and artifacts retain stronger visual treatment because they represent durable output.

This is a client-side interaction and presentation refinement. It changes the unsaved-browser reasoning default, adds draft-population actions, introduces a frontend-only usage-settlement signal, and moves pending approvals into the correct task order. It does not change backend fields, event protocols, persistence schemas, approval authorization, or artifact routing.

“Prism” means scarce semantic state color: one calm base system reveals distinct running, success, warning, failure, focus, and selected states only where they carry information.

## 2. Evidence and Current-State Findings

The design is grounded in four authenticated live-product captures:

- [Desktop empty state](../../../output/ui-research/aura-desktop-empty.png) — 1440 × 1000, SHA-256 `BE5D3D1ADFD7FEA6D7A700784DDD674E61B9E413A4AE8F75CB091D4FB85C7171`
- [Desktop tool timeline](../../../output/ui-research/aura-desktop-tool-timeline-clean.png) — 1440 × 1000, SHA-256 `1244ACB9179B4C502C6BBD493F22721E616B0D521EAAB20D1B766752A2C95DFC`
- [Desktop approval card](../../../output/ui-research/aura-desktop-approval-card.png) — 1440 × 1000, SHA-256 `124AE9E2565BEEAD2D2ED7274932D26E6DA80829AD1CA529C877E203AA226930`
- [Mobile timeline](../../../output/ui-research/aura-mobile-timeline.png) — 393 × 852, SHA-256 `CDFAD885DA9EC8EAE58545FFA6E9A1C8B1155E1F3A9A54C65BFF436F34C28231`

The paths are local audit artifacts and are ignored by Git. Their hashes, dimensions, capture date, and states above are the tracked evidence manifest. A fresh implementation audit must recreate these states rather than treating the local files as permanent visual-regression baselines.

The captures and source inspection establish these concrete problems:

- The most visible overlap comes from workspace-level Voice and Artifacts controls mounted in an absolute top-right overlay in `AppShell`, not from the message action bars alone.
- Message action rows do not wrap, rely on opacity/hover behavior that is unsuitable for touch, and use message-root width caps that can constrain richer content.
- Assistant prose uses too much horizontal measure at desktop widths, slowing scanning.
- Reasoning and raw tool activity use similar grey card treatments, so hierarchy depends too heavily on reading every label.
- Pending approvals render after the composer in DOM and visual order; the operator sees a competing input before the decision that blocks the run.
- The current frontend incorrectly treats every `kind="approval"` item as a risky skill installation even though the DTO has no trusted subtype.
- The approval card exposes a resume token and install-specific copy that do not help most decisions.
- The mobile footer is functionally collapsible, but its summary lacks a clear disclosure cue. Its interactive content is inside a live region, and its visual values share the settled-only latch.
- The main empty state explains that the thread is empty but does not help the user begin a useful task; the sidebar repeats similar empty-state copy.

The existing product already gets several important things right and they remain intact:

- Semantic light/dark tokens, Fraunces/Atkinson/Commit Mono typography, and the existing contrast gate.
- `@assistant-ui/react` external-store integration and the AG-UI/SSE adapter.
- Typed `DisplayRouter` results, sources, attachments, and the artifacts panel.
- Escaped raw tool output, elapsed timing, child activity, and manual disclosure intent.
- Approval resolution verbs, inline cancellation confirmation, escaped server-sanitized question text, authorization checks, and terminal states.
- Mobile telemetry disclosure, numeric guards, and the context progressbar.

## 3. Goals

- Eliminate workspace-control and message-action overlap at 320, 393, 768, and 1440 CSS px and at 200% browser zoom.
- Limit plain assistant prose to exactly `48rem` without constraining tools, tables, sources, typed displays, or artifacts.
- Make the visual hierarchy legible as **conversation → activity → result → artifact**.
- Put an unresolved approval before the composer in task and focus order.
- Use generic, truthful approval framing when the backend supplies no trusted subtype.
- Make the empty state an effective starting surface with localized prompt starters.
- Separate live visual telemetry from settled-only assistive announcements.
- Retain a restrained, accessible color system in which accent, focus, and status colors have distinct jobs.
- Preserve keyboard, screen-reader, reduced-motion, localization, theme, zoom/reflow, and touch behavior.
- Deliver without a new UI dependency or backend/runtime protocol rewrite.

## 4. Non-Goals

- Replacing `ExternalStoreChat` with a canonical assistant-ui `Thread` implementation.
- Rewriting `sseAdapter`, conversation persistence, AG-UI events, or backend run control.
- Adding new tool protocols, MCP Apps, remote MCP servers, or generative UI payload types.
- Adding a backend approval subtype in this phase.
- Inferring an approval subtype from question text, token shape, source, or client heuristics.
- Redesigning the conversations sidebar, artifacts content system, or document library.
- Introducing a new palette, font family, icon set, gradient treatment, or decorative illustration.
- Changing approval authorization, endpoint contracts, action names, or server question sanitization.
- Adding raw provider chain-of-thought persistence.

## 5. Design Principles

### 5.1 Conversation is the base layer

Plain assistant prose remains unboxed. User messages retain a quiet surface fill, but workspace controls and message actions occupy reserved lanes. The thread should read like a conversation, not a stack of dashboards.

### 5.2 Chrome follows information value

Activity receives the least chrome, typed results receive enough structure to scan, and artifacts receive durable object treatment. A border is not added merely to group nearby text.

### 5.3 Progressive disclosure is the default

Reasoning detail, raw tool arguments/results, approval internals, and full mobile telemetry remain available but do not dominate the initial view.

### 5.4 Status is semantic and truthful

Running, success, warning, failure, and neutral refusal use existing semantic tokens plus text or icon labels. Color is never the only signal. The client never invents a risk subtype or reassurance the server did not provide.

### 5.5 Accent and focus have separate jobs

The `accent` family is reserved for primary action and selected/active state. Keyboard focus uses the dedicated `ring` token, which must retain at least 3:1 contrast against every affected adjacent surface. Passive containers and settled content do not receive accent merely for decoration.

## 6. Experience and Component Changes

### 6.1 Workspace controls, thread measure, and message actions

**Targets:**

- `web/src/AppShell.tsx`
- `web/src/chat/voice/VoiceModeToggle.tsx`
- `web/src/shell/ArtifactsShell.tsx`
- `web/src/chat/ExternalStoreChat.tsx`
- `web/src/chat/ExternalStoreChat_messages.tsx`

The visible top-right collision is caused by workspace-level Voice and Artifacts controls. They move from an absolute overlay into a normal-flow workspace control lane or header row with reserved height. The lane stays visually light, but it cannot cover the first message, attachment, reasoning row, or scrollable prose.

The thread layout uses this measurable DOM contract:

- assistant turn root: `width: 100%`, `min-width: 0`;
- plain assistant text wrapper only: `max-width: 48rem`, `overflow-wrap: anywhere`;
- tool, typed display, source, table, attachment, artifact, and error parts: siblings outside the prose cap;
- user content column: `min-width: 0`, wrapping and overflow containment;
- message action row: `max-width: 100%`, `min-width: 0`, `flex-wrap: wrap`;
- workspace-control and frequent message-action targets: at least 44 × 44 CSS px.

Stable semantic hooks support tests without coupling them to Tailwind strings:

- `data-workspace-controls`
- `data-message-content`
- `data-message-actions`
- `data-assistant-prose`
- `data-typed-display`

Hover discovery may reduce action opacity only under a fine-pointer `@media (hover: hover)` condition. Touch/coarse-pointer layouts show actions without requiring hover. Focus always reveals the complete row.

No workspace control or message action may intersect content, crop at the viewport edge, or create page-level horizontal overflow. Long unbroken words, long filenames, attachments, and all optional action controls are required fixtures.

### 6.2 Reasoning treatment and preference migration

**Targets:**

- `web/src/chat/ReasoningDrawer.tsx`
- `web/src/chat/reasoningPref.ts`
- `web/src/chat/__tests__/ReasoningDrawer.test.tsx`
- `web/src/chat/__tests__/reasoningPref.test.ts`
- the reasoning assertions in `web/e2e/chat.spec.ts`

The browser-origin preference contract is frozen:

- key remains `aura.chat.reasoning.shown`;
- `'1'` means shown;
- `'0'` means hidden;
- missing, invalid, unreadable, or unwritable storage means hidden;
- an explicit stored value remains authoritative for that browser profile.

This changes only the default for a browser profile with no valid preference. It does not create identity-scoped persistence.

Each reasoning disclosure uses a `useId()`-derived body ID. The button uses `aria-expanded` and `aria-controls`, not `aria-pressed`. The controlled body remains in the DOM with the `hidden` state when collapsed so every `aria-controls` reference resolves.

The collapsed row becomes visually lighter than a tool result:

- no heavy filled card treatment;
- compact chevron, label, and optional streaming cue;
- a 44 × 44 CSS px minimum interactive target;
- expanded content uses a subtle inset rule and muted text rather than a nested card.

The implementation continues to render only the live reasoning part Aura already receives. No new persistence or extraction is introduced.

### 6.3 Tool activity hierarchy

**Target:** `web/src/chat/ToolActivityCard.tsx`

The immutable disclosure state matrix is:

1. running with raw content at mount → expanded;
2. running with no raw content, then raw content arrives → expand if the user has not toggled;
3. running → done or error → collapse once if the user has not toggled;
4. either manual expanded or collapsed state wins after the first manual toggle;
5. already-settled at mount → collapsed;
6. a later status flicker back to running does not reopen a settled row;
7. raw content remains escaped text inside `<pre>` and never enters a markdown/HTML renderer.

Visual structure becomes:

1. status marker and tool name;
2. human-readable status and locale-aware elapsed time;
3. disclosure control when raw detail exists;
4. raw arguments or result only inside the disclosure;
5. indented one-level child activity where present.

Refinement rules:

- The outer surface uses one low-contrast boundary rather than equally strong fill, border, and status stripe.
- Status marker, text label, and elapsed time carry state; semantic color is supporting information.
- Settled success is quieter than running or error; error remains easy to find.
- Child rows use indentation and a connector rule, not nested full cards.
- Tool names wrap or truncate with a title without pushing the 44 × 44 disclosure control off-screen.
- Settled elapsed duration is available to assistive technology without announcing every running tick.

Typed displays produced by `DisplayRouter` remain the **Result** layer and stay more structured than raw activity.

### 6.4 Approval placement, safety, and decision hierarchy

**Targets:**

- `web/src/AppShell.tsx`
- `web/src/approvals/ThreadApprovalCards.tsx`
- `web/src/approvals/InlineApprovalCard.tsx`
- `web/src/chat/ExternalStoreChat.tsx`
- `web/src/chat/Composer.tsx`
- `web/src/i18n/resources.ts`

Pending approvals render after the latest assistant/tool activity and before the composer in DOM, reading, visual, and tab order. `ExternalStoreChat` owns this placement. The thread-specific approval selection is extracted into a reusable client hook or selector so the same pending list controls both rendering and composer availability without a second data source.

While at least one approval is unresolved:

- the composer remains visible but disabled;
- it shows a localized hint equivalent to “Answer the request above to continue”;
- Send, attachment, dictation, skill, and reasoning-effort controls cannot create a competing turn;
- approvals retain deterministic backend order;
- after resolution, focus moves to the next pending approval or returns to the composer.

The server-sanitized question string renders as escaped text with `white-space: pre-wrap`, `overflow-wrap: anywhere`, and contained horizontal overflow so commands, paths, hashes, and line breaks remain inspectable. The client does not describe the string as raw or unsanitized verbatim backend input.

The current DTO has no trusted skill-install subtype. Therefore:

- `kind="approval"` receives generic localized **Approval required** framing and a neutral warning to review scope and consequence;
- non-approval input uses generic **Input required** framing;
- no install, container-isolation, activation, source, hash, or preview claim is fabricated by the client;
- install-specific framing remains out of scope until a trusted server-projected subtype exists.

The UI no longer renders `approval.token` as a dedicated label or technical field. The token remains client-visible in the authenticated approvals response and encoded resolution URL; authentication, ownership, capability checks, and one-time resolution remain the authorization boundary. The client never redacts or rewrites the server-sanitized question if that question independently contains the same string.

Approval actions and wire behavior remain unchanged:

- **Answer** sends `accept` with operator content;
- **Decline** sends `decline` without typed answer content;
- **Cancel run** sends `cancel`, with inline confirmation while streaming;
- the token travels in the encoded URL path, not the JSON body.

Cancel confirmation behavior is explicit:

- opening confirmation moves focus to the safe **Keep running** action;
- Escape closes confirmation and restores focus to **Cancel run**;
- all confirmation controls are at least 44 × 44 CSS px.

Terminal semantics are fixed:

- Answered → success;
- Declined → neutral explicit state;
- Cancelled or request failure → danger;
- Expired → warning.

Resolve success, decline, cancellation, expiry, and failure are announced exactly once through a non-interactive status region.

This supersedes the older client presentation that inferred skill installation from `kind="approval"` and displayed the resume token. The approval endpoint and authorization contract do not change.

### 6.5 Runtime footer

**Targets:**

- `web/src/AppShell.tsx`
- `web/src/chat/ExternalStoreChat.tsx`
- `web/src/chat/RuntimeFooter.tsx`
- `web/src/chat/ContextBudgetGauge.tsx`

The existing `onUsage` callback is generalized to a frontend-only usage-state seam. Streaming frames report the latest usage with `settled: false`; run completion, cancellation, or failure reports the last available usage with `settled: true` exactly once. `AppShell` owns `{usage, settled}` and passes both to `RuntimeFooter`. No SSE payload or backend protocol changes.

Visible metrics use the live cluster. A separate visually hidden, non-interactive `role="status" aria-live="polite" aria-atomic="true"` region receives only the latched settled summary. Expanding or collapsing telemetry does not mutate that live region.

Mobile behavior:

- one compact summary button visibly labels both values, for example `Session cost $0.12 · Context 24%`;
- a localized chevron/cue reflects `aria-expanded`;
- full token/cache/cost detail appears only after expansion below `sm`;
- the context gauge belongs to the expanded detail on mobile, preventing a competing second row;
- all metrics remain available from `sm` upward.

Numeric values remain mono, tabular, locale-aware where newly introduced, guarded against `NaN`, and announced only at settlement. The context gauge retains progressbar semantics and the existing compaction marker.

### 6.6 Empty state and prompt starters

**Targets:**

- `web/src/AppShell.tsx`
- `web/src/chat/ExternalStoreChat.tsx`
- `web/src/chat/Composer.tsx`
- `web/src/i18n/resources.ts`

`AppShell` remains the sole owner of nonce-backed composer draft events. `documentDraftPrompt` is generalized to `composerDraftPrompt`. `ExternalStoreChat` receives `onRequestDraftPrompt(text)`, and starter buttons call that callback. No second controlled composer value is introduced.

Every click increments the event nonce, including repeated clicks on the same starter. The event replaces the current draft, focuses the composer after application, leaves the text editable, and never submits or creates an `/agent/run` request.

The localized labels and inserted bodies are normative:

| Intent | English label and body | Italian label and body |
|---|---|---|
| Research | **Research a topic** — `Research [topic] and compare the most reliable sources.` | **Cerca un argomento** — `Cerca [argomento] e confronta le fonti più affidabili.` |
| File | **Analyze a file** — `Analyze the file I'll attach and summarize the key findings.` | **Analizza un file** — `Analizza il file che allegherò e riassumi i risultati principali.` |
| Artifact | **Create an artifact** — `Create a [report/table/document] about [topic].` | **Crea un artefatto** — `Crea un [rapporto/tabella/documento] su [argomento].` |
| Automation | **Automate a task** — `Help me plan and automate [repeatable task].` | **Automatizza un'attività** — `Aiutami a pianificare e automatizzare [attività ripetitiva].` |

The File starter prepares the draft but does not open the attachment picker automatically; its copy makes the next action clear.

Starters use a one-column layout at 320 px and may use two columns only when labels fit without truncation. Desktop uses a two-by-two grid. Every starter is a real button with a 44 px minimum height.

The conversations sidebar keeps its concise region-specific empty message. The main thread owns all starting actions.

### 6.7 Color roles

**Targets:** existing semantic token usage in affected components; no token-value migration.

Aura's logo-matched blue semantic-token system remains the source of truth in light and dark themes:

| Role | Token family | Usage |
|---|---|---|
| Base canvas | `bg` / `surface` | shell, thread, composer |
| Quiet grouping | `surface-2` | user bubble, expanded detail where needed |
| Boundary | `border` | separation and disclosure edges |
| Primary action | `accent` / `on-accent` | Send, selected/active action |
| Keyboard focus | `ring` | all focus-visible indicators |
| Running | `warning` | active tool state and generic risk context |
| Success | `success` | completion and accepted confirmation |
| Neutral | text/border tokens | declined and non-error terminal states |
| Failure | `danger` | failed tool, destructive action, request error |
| Text hierarchy | `text`, `text-muted`, `text-faint` | content, metadata, tertiary labels |

No raw hex values are added to components. A brighter cobalt remains a separate future token experiment requiring complete contrast and visual-regression evidence.

## 7. Responsive and Theme Behavior

### Desktop references: 768 and 1440 CSS px

- Workspace controls occupy their reserved lane and never cover the first turn.
- Assistant prose stops at exactly `48rem`; displays can remain wider.
- Action rows wrap without separating from their message.
- Tool and reasoning rows scan as a timeline.
- Full runtime metrics remain available.

### Mobile references: 320 and 393 CSS px

- No page-level horizontal scroll at default zoom or 200% browser zoom/reflow.
- No workspace control, message action, attachment, or disclosure is obscured.
- Frequent controls have at least a 44 × 44 CSS px target.
- Long tool names, commands, approval text, and filenames do not push controls off-screen.
- Prompt starters fit without clipped labels.
- Footer detail is collapsed initially and the disclosure cue is visible.

### Theme references

- All affected states are exercised in dark and light themes.
- Focus, text, border, status, and selected-state contrast use semantic tokens only.
- The contrast gate remains evidence for tested color pairs, not a claim of full WCAG compliance.

## 8. Accessibility and Localization

- Every disclosure instance has a unique generated body ID; `aria-controls` resolves in expanded and collapsed states.
- Disclosure buttons use `aria-expanded`; reasoning does not use `aria-pressed`.
- Every icon-only control has a localized accessible name.
- Focus indicators use `ring`, never the darker accent fill.
- Status always includes text; warning/success/error color is never the sole cue.
- Interactive footer content is outside the settled-only live region.
- Approval confirmation manages and restores focus; approval outcomes announce once.
- Raw tool content and approval questions remain escaped, preserve meaningful whitespace, and contain their own overflow.
- Touch/coarse-pointer layouts do not rely on hover for discoverability.
- English and Italian tests assert visible labels, accessible names, inserted prompts, wrapping, and locale-aware durations rather than key parity alone.
- Reduced-motion behavior remains respected for disclosure icons and transitions.
- Manual gates include keyboard-only use, 200% zoom/reflow, reduced motion, forced colors, one screen-reader pass, and touch/coarse-pointer behavior. The design does not claim full WCAG conformance from screenshots or contrast automation alone.

## 9. Architecture and Data Boundaries

The work changes client interaction seams but preserves backend contracts:

- `ExternalStoreChat` continues to own runtime and streaming state.
- `AppShell` owns composer draft events and footer usage state.
- `ExternalStoreChat` emits draft requests upward and usage settlement upward; neither adds a backend field.
- `ThreadApprovalCards` moves inside the chat task order and shares a single thread-approval selection with composer-disabled state.
- `ExternalStoreChat_messages` remains a presentational renderer.
- `sseAdapter` event handling and message content shapes do not change.
- `DisplayRouter` remains the only typed-result routing seam.
- `ToolActivityCard` remains the safe fallback for untyped tool output.
- Approval resolution continues through `useResolveApproval`; the token stays in the encoded URL and verb content stays in JSON.
- The client does not infer an approval subtype.
- No component accepts or renders a new backend field.

Before production code, `prd.md` must receive a separate PRD-amendment commit recording:

- the new unsaved-browser reasoning default;
- generic approval framing and removal of dedicated resume-token display;
- approval placement before the composer and composer-disabled behavior;
- client-owned localized prompt starters rather than backend-supplied suggestions;
- the frontend-only usage-settlement seam;
- supersession of older conflicting cockpit presentation requirements.

## 10. Test-First Implementation Contract

Implementation follows Aura's existing test stack and starts with failing behavioral tests after the PRD amendment lands.

### Component and integration tests

- Reasoning preference tests cover missing, invalid, `'0'`, `'1'`, read failure, and write failure.
- Two simultaneous reasoning drawers produce unique, resolving control/body relationships without `aria-pressed`.
- The chronology E2E expands reasoning before asserting its content; a separate fresh-profile test asserts default-collapsed behavior.
- Tool tests pin the complete seven-case disclosure state matrix, safe text rendering, long-name containment, child rows, and locale-aware settled duration.
- Generic approval tests cover ordinary input, generic approval risk, multiline whitespace, token-field removal, and no invented install/container copy.
- Token sentinel tests assert no dedicated token field in pending, terminal, and failure states while the encoded resolve URL still contains the token and JSON bodies remain `{action, content?}`.
- Approval tests cover DOM order before composer, composer disablement, deterministic multiple-approval order, focus movement/restoration, Escape, one-time announcements, and terminal tone mapping.
- Footer tests prove visual values update mid-stream while the visually hidden status remains latched, then announces exactly once on settlement; disclosure mutations do not alter the live region.
- Starter tests cover exact English/Italian labels and bodies, repeated same-starter nonce, replacement, focus, editability, and zero run requests.
- English/Italian resource parity remains green.

### Semantic layout assertions

- Plain prose and typed display hooks exist on separate width surfaces.
- Assistant root, content, action rows, and workspace controls expose stable semantic hooks.
- Fine-pointer hover behavior and coarse-pointer visible behavior are both asserted.
- Long content contains overflow at the correct boundary.
- Frequent controls retain 44 × 44 CSS px minimum targets.

### Playwright browser verification

Playwright uses an explicit `channel: 'chrome'` project against the authenticated Aura instance.

Before navigation, the test registers collectors for:

- console errors;
- `pageerror`;
- `requestfailed`;
- same-origin HTTP responses with status `>= 400`.

The only allowed request failures are intentional run cancellation/abort events named in the test. Authentication, polling, and asset failures are not broadly allowlisted.

Deterministic fixtures fix locale, theme, density, viewport, font readiness, reduced/disabled animation, and representative long content. The browser checks:

1. empty state with all starters;
2. mixed reasoning plus multiple running, settled, and error tools;
3. ordinary and generic-risk approvals, including option, free-text, terminal, and error states;
4. typed display and artifact hierarchy;
5. mobile footer collapsed and expanded;
6. dark and light themes.

Bounding-box assertions prove:

- workspace controls do not intersect the first user bubble, attachment, action row, or prose;
- message actions do not intersect message content;
- every required rectangle stays inside the viewport;
- `document.documentElement.scrollWidth <= document.documentElement.clientWidth`;
- 44 × 44 target bounds hold at coarse pointer.

Screenshots are captured at 1440 × 1000 and 393 × 852. Each current-state reference and implementation screenshot is placed in the same visual-review input, as required by the Product Design comparison protocol. The current reference is used to prove defect removal, not as a fidelity target. The written measurable contract above is the Calm Prism target.

After the first accepted implementation, stable golden-route screenshots are committed as Playwright `toHaveScreenshot` baselines for regression. A review record names the state, viewport, theme, comparison input, defects found, fixes applied, and reviewer verdict.

### Required repository gates

- PRD-amendment commit before any production-code commit.
- Targeted Vitest suites for every affected component.
- TypeScript typecheck and ESLint.
- Existing contrast script with all required pairs passing in both themes.
- Authenticated Chrome Playwright E2E flow.
- Production web build and verification of the generated `internal/webui/dist` diff.
- Path-scoped `git diff --check` and owned-file review.

### Dirty-worktree protocol

- Record `git status --short` before edits.
- Declare the owned paths for each slice.
- Never stash, reset, clean, or reformat unrelated user changes.
- Never use `git add -A`; stage only owned paths.
- Stop and request direction if an owned path overlaps an unrelated change.
- Verify source and generated-bundle diffs against the owned-path allowlist before every commit.

## 11. Acceptance Criteria

The refinement is complete only when all of the following are true:

1. At 320, 393, 768, and 1440 CSS px and 200% zoom, workspace-control and message-action rectangles do not intersect content and no page-level horizontal overflow exists.
2. Assistant root is full-width/min-width-zero; plain prose alone is capped at `48rem`; tools, typed displays, tables, sources, and artifacts remain outside the cap.
3. Fine-pointer hover may quiet actions, while coarse-pointer/touch always exposes them; frequent targets measure at least 44 × 44 CSS px.
4. The reasoning preference key and `'1'`/`'0'` encoding remain unchanged; missing, invalid, and storage-error states default collapsed; explicit values are honored.
5. Multiple reasoning disclosures have unique resolving IDs, use `aria-expanded`, and do not use `aria-pressed`.
6. Tool raw-data behavior passes the complete seven-case state matrix and remains XSS-safe.
7. Pending approvals render before the disabled composer in DOM, reading, visual, focus, and tab order.
8. Generic approval/input framing never claims skill installation, container isolation, or activation without a trusted subtype.
9. Server-sanitized question whitespace is preserved and long content remains contained.
10. No dedicated resume-token label or field is rendered; the encoded resolution URL and unchanged verb JSON remain correct.
11. Approval cancel confirmation manages Escape and focus; every resolution/expiry/failure outcome is announced once with the specified semantic tone.
12. Empty-state starters insert the exact localized editable body, replace through the sole nonce-backed draft owner, focus the composer, and create no run request.
13. Visible telemetry updates during streaming; a separate non-interactive status region announces only once when the turn settles.
14. Mobile telemetry starts compact with labeled cost/context and a visible disclosure cue; its gauge appears in expanded detail; desktop telemetry remains complete.
15. English and Italian tests cover actual visible/accessibility copy and locale-aware values, not only key parity.
16. Dark and light contrast gates pass with no required-pair regression; focus uses `ring`.
17. No new dependency, backend field, runtime protocol change, raw component color, or unsafe HTML path is introduced.
18. Chrome Playwright collectors report no unallowlisted console, page, request, or HTTP errors.
19. Same-input baseline/implementation visual review finds no unresolved crop, overlap, spacing, typography, border, radius, focus, or hierarchy defect.
20. The PRD amendment, source diff, generated bundle, tests, and visual-review record satisfy the dirty-worktree and repository gates.

## 12. Implementation Sequence

The later implementation plan uses small vertical slices in this order:

1. Amend and commit `prd.md` with the approved contract supersessions.
2. Move workspace controls into reserved flow; fix message containment and exact prose measure.
3. Migrate reasoning default/semantics and lighten the disclosure treatment.
4. Preserve and test the full tool state matrix while refining activity hierarchy.
5. Move approvals before the composer; use generic truthful framing; remove the dedicated token field; add focus/status behavior.
6. Generalize the AppShell-owned composer draft seam and add localized starters.
7. Add the frontend usage-settlement seam and separate live visual from settled assistive telemetry.
8. Run responsive, theme, localization, accessibility, contrast, build, and Chrome Playwright visual verification.

Each slice starts with a failing test, leaves targeted tests green, and commits only declared owned paths.

## 13. Risks and Mitigations

- **Risk: workspace-control movement changes shell layout.** Reserve one small normal-flow lane and verify every shell mode at required widths.
- **Risk: width wrappers constrain typed displays.** Put the cap only on the Text renderer and assert typed-display bounding width separately.
- **Risk: action discoverability falls.** Restrict hover-only quieting to fine pointers; preserve focus and coarse-pointer visibility.
- **Risk: reasoning migration surprises existing browser profiles.** Preserve the storage key/encoding and change only missing/invalid/error fallback.
- **Risk: tool visual edits change disclosure behavior.** Pin the complete state matrix before styling.
- **Risk: generic approval framing loses useful context.** Preserve the complete server-sanitized question with whitespace; remove only fabricated subtype claims and the dedicated token field.
- **Risk: approval movement creates competing state owners.** Use one thread-approval selector for placement and composer disablement.
- **Risk: token removal breaks resolution.** Keep token transport in the encoded URL and assert it independently from the JSON verb body.
- **Risk: prompt starters duplicate composer state.** Keep AppShell as the sole nonce-backed draft-event owner.
- **Risk: footer settlement becomes another backend protocol.** Keep it as a client run-phase signal emitted from existing lifecycle boundaries.
- **Risk: visual comparison rewards similarity to a defective baseline.** Use the baseline only for defect-removal evidence and judge against the measurable Calm Prism contract.
- **Risk: dirty worktree captures unrelated planning changes.** Enforce path-scoped ownership and staging.

## 14. Research References

- assistant-ui Tool UI guidance, treated as pattern evidence rather than API compatibility for Aura's pinned version: <https://www.assistant-ui.com/docs/tools/tool-ui>
- assistant-ui Chain-of-Thought UI guidance, likewise version-gated: <https://www.assistant-ui.com/docs/guides/chain-of-thought>
- WAI-ARIA disclosure pattern: <https://www.w3.org/WAI/ARIA/apg/patterns/disclosure/>
- WCAG 2.2 target size minimum: <https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html>
- WCAG 2.2 reflow: <https://www.w3.org/WAI/WCAG22/Understanding/reflow.html>
- WCAG 2.2 focus appearance: <https://www.w3.org/WAI/WCAG22/Understanding/focus-appearance.html>

These sources inform the interaction model. Aura's installed versions, semantic tokens, runtime seams, and tests remain authoritative for implementation.
