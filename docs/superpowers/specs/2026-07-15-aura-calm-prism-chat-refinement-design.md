# Aura Calm Prism Chat Refinement Design

- **Date:** 2026-07-15
- **Status:** Approved direction; implementation plan pending user review of this written spec.
- **Surface:** Aura authenticated chat cockpit, desktop and mobile.
- **Approach:** Targeted in-place refinement of the existing Editorial Graphite design system.
- **Browser evidence:** Live Aura inspected with user-approved Playwright against Chrome.

## 1. Outcome

Aura's chat should feel quieter during conversation, clearer while work is running, and richer when a result deserves structure. The approved direction is **Calm Prism**:

1. **Quiet conversation** — prose and user messages carry the reading experience without extra card chrome.
2. **Expressive progress** — reasoning, tools, and approvals reveal state through concise, semantic rows.
3. **Rich results** — typed displays and artifacts retain their stronger visual treatment because they represent durable output.

This is a presentation-layer refinement. It does not replace Aura's chat runtime, event model, approval protocol, or artifact architecture.

## 2. Evidence and Current-State Findings

The design is grounded in four authenticated live-product captures:

- [Desktop empty state](../../../output/ui-research/aura-desktop-empty.png)
- [Desktop tool timeline](../../../output/ui-research/aura-desktop-tool-timeline-clean.png)
- [Desktop approval card](../../../output/ui-research/aura-desktop-approval-card.png)
- [Mobile timeline](../../../output/ui-research/aura-mobile-timeline.png)

The captures establish these concrete problems:

- Message-level audio/document actions can crowd or overlap message content, especially on narrow screens.
- Assistant prose uses too much horizontal measure at desktop widths, slowing scanning.
- Reasoning and raw tool activity use similar grey card treatments, so hierarchy depends too heavily on reading every label.
- The risky approval card gives a bearer-like resume token and implementation detail more prominence than the operator's decision.
- The mobile footer is functionally collapsible already, but its compact summary and context gauge still compete with the composer.
- The main empty state explains that the thread is empty but does not help the user begin a useful task; the sidebar repeats similar empty-state copy.

The existing product already gets several important things right and they remain intact:

- Semantic design tokens, the Fraunces/Atkinson/Commit Mono type system, and the current high-contrast dark palette.
- `@assistant-ui/react` external-store integration and the AG-UI/SSE adapter.
- Auto-expanding running tool payloads and one-time auto-collapse when tools settle.
- Persisted reasoning visibility preference.
- Typed `DisplayRouter` results, sources, attachments, and the artifacts panel.
- Approval resolution verbs, inline cancellation confirmation, escaped backend text, and terminal states.
- Mobile telemetry disclosure and settled-only live-region announcements.

## 3. Goals

- Eliminate message/action overlap at 393 px and 1440 px reference widths.
- Limit assistant prose to a comfortable reading measure without constraining tool, table, source, or artifact content.
- Make the visual hierarchy legible as **conversation → activity → result → artifact**.
- Put approval intent, consequence, and action ahead of technical identifiers.
- Make the empty state an effective starting surface with realistic prompt starters.
- Retain a restrained, accessible color system in which accent and status colors have distinct jobs.
- Preserve keyboard, screen-reader, reduced-motion, localization, and touch behavior.
- Deliver the work without a new UI dependency or runtime rewrite.

## 4. Non-Goals

- Replacing `ExternalStoreChat` with a canonical assistant-ui `Thread` implementation.
- Rewriting `sseAdapter`, conversation persistence, AG-UI events, or run control.
- Adding new tool protocols, MCP Apps, remote MCP servers, or generative UI payload types.
- Redesigning the app shell, conversations sidebar, artifacts system, or document library.
- Introducing a new palette, font family, icon set, gradient treatment, or decorative illustration.
- Changing approval authorization, endpoint contracts, action names, or backend question content.
- Adding raw provider chain-of-thought persistence.

## 5. Design Principles

### 5.1 Conversation is the base layer

Plain assistant prose remains unboxed. User messages retain a quiet surface fill, but their controls occupy a separate action lane. The thread viewport should read like a conversation, not a stack of dashboards.

### 5.2 Chrome follows information value

Activity receives the least chrome, typed results receive enough structure to scan, and artifacts receive durable object treatment. A border is not added merely to group nearby text.

### 5.3 Progressive disclosure is the default

Reasoning detail, raw tool arguments/results, approval internals, and full mobile telemetry remain available but do not dominate the initial view.

### 5.4 Status is semantic

Running, success, warning, and failure use existing semantic status tokens plus text or icon labels. Color is never the only signal.

### 5.5 Accent stays scarce

The existing `accent` token is reserved for primary action, active navigation, focus, and selected interactive state. It does not tint passive containers or settled content merely for decoration.

## 6. Experience and Component Changes

### 6.1 Thread measure and message actions

**Targets:**

- `web/src/chat/ExternalStoreChat.tsx`
- `web/src/chat/ExternalStoreChat_messages.tsx`

The viewport keeps the existing full-width scroll lane so typed displays and artifacts can use available space. Inside each assistant turn, plain prose is limited to a `max-width` in the 45–50 rem range. Tool cards, typed displays, sources, attachments, errors, and action rows remain outside that prose-only constraint.

User bubbles retain the current right alignment and rounded surface, with these changes:

- The bubble and any attachments form the content column.
- Edit, Copy, Branch, audio, and document-related message actions render in a separate action row or reserved gutter below the content.
- No action may be absolutely positioned over text or crop at the viewport edge.
- Hover discovery remains available on pointer devices, while focus and touch make actions fully visible and operable.
- A message with a long unbroken word, attachment, or multi-line paragraph must not push controls outside the viewport.

Assistant action controls follow the same non-overlap rule. The prose measure must not shrink typed results, source surfaces, code blocks, or artifact cards.

### 6.2 Reasoning treatment

**Targets:**

- `web/src/chat/ReasoningDrawer.tsx`
- `web/src/chat/reasoningPref.ts`

The existing persisted preference remains authoritative for returning users. For users with no saved preference, reasoning starts collapsed. This changes the current builder default from shown to hidden while retaining explicit user control.

The collapsed row becomes visually lighter than a tool result:

- no heavy filled card treatment;
- compact chevron, label, and optional streaming cue;
- a clear 44 px minimum interactive target;
- expanded content uses a subtle inset rule and muted text rather than a second nested card.

The implementation continues to render only the live reasoning part Aura already receives. No new persistence or extraction is introduced.

### 6.3 Tool activity hierarchy

**Target:** `web/src/chat/ToolActivityCard.tsx`

The current state machine is preserved exactly: a running payload may open automatically; the running-to-settled edge closes once unless the user has manually toggled it; raw content stays escaped in a `<pre>`.

Visual structure becomes:

1. status marker and tool name;
2. human-readable status and elapsed time;
3. disclosure control when raw detail exists;
4. raw arguments or result only inside the disclosure;
5. indented one-level child activity where present.

Refinement rules:

- The outer surface uses one low-contrast boundary, not a border plus a strong filled card plus a status stripe of equal weight.
- The status marker, text label, and elapsed time carry state; semantic color is supporting information.
- Settled success is quieter than running or error.
- Error remains prominent enough to find in a long thread.
- Child rows use indentation and a connector rule, not nested full cards.
- The 44 px disclosure target remains, even if the visible icon is smaller.

Typed displays produced by `DisplayRouter` are unchanged in this phase. They already represent the **Result** layer and should remain more structured than raw activity.

### 6.4 Approval decision hierarchy

**Targets:**

- `web/src/approvals/InlineApprovalCard.tsx`
- `web/src/i18n/resources.ts`

The backend question continues to render verbatim as an escaped React text node. Approval actions and wire payloads remain unchanged:

- **Answer** sends `accept` with operator content.
- **Decline** sends `decline` without typed answer content.
- **Cancel run** sends `cancel`, with the existing inline confirmation while streaming.

The pending card is reordered visually:

1. compact decision-type/risk label;
2. verbatim operator question as the strongest text;
3. concise impact or isolation note;
4. options or free-text answer field;
5. Answer, Decline, and Cancel actions;
6. errors or terminal state feedback.

For risky skill-install approvals:

- The existing warning meaning is retained, but the warning is a concise strip rather than a nested card of equal weight.
- The resume token is never rendered in the user-facing card. It remains an internal value used only by the resolution request.
- Source, hash, and preview information contained in the backend question remains visible because the question is preserved verbatim.
- There is still no separate Run or Activate affordance.

This deliberately supersedes the older presentation requirement that showed the resume token. The token adds operator noise and unnecessary exposure without helping the decision.

### 6.5 Runtime footer

**Targets:**

- `web/src/chat/RuntimeFooter.tsx`
- `web/src/chat/ContextBudgetGauge.tsx`

The existing mobile disclosure is retained; it is not rebuilt. The refinement is limited to hierarchy and spacing:

- Mobile shows one compact session summary control with cost and context usage, plus a clear expand/collapse cue.
- Full token/cache/cost detail appears only after expansion below `sm` and remains visible from `sm` upward.
- The context gauge aligns with the summary instead of reading as a second footer row competing with the composer.
- Numeric values remain mono, tabular, guarded against `NaN`, and announced only when a turn settles.
- Desktop keeps the full operator instrument cluster; no essential metric is removed.

The footer remains an instrument surface, not a typed chat display.

### 6.6 Empty state and prompt starters

**Targets:**

- `web/src/chat/ExternalStoreChat.tsx`
- `web/src/i18n/resources.ts`
- the existing draft-prompt seam between `AppShell` and `Composer`

The main empty state remains centered and uses the existing display font. Beneath the short explanation, it gains four compact prompt starters:

- **Research a topic**
- **Analyze a file**
- **Create an artifact**
- **Automate a task**

Each starter inserts a localized, editable prompt into the composer and focuses it. It never auto-submits. The starter uses the same draft-prompt path already used elsewhere in Aura, avoiding a second composer state mechanism.

On mobile, starters form a single-column list or a two-column grid only when labels fit without truncation. On desktop, they may use a two-by-two grid. Each is a real button with a 44 px minimum height.

The conversations sidebar keeps its own empty message because it describes a different region, but its copy remains concise. The main thread owns all starting actions so there is one obvious place to begin.

### 6.7 Color roles

**Targets:** existing semantic token usage in the affected components; no token-value migration in this phase.

The existing palette already passes Aura's contrast gate and remains the source of truth:

| Role | Token family | Usage |
|---|---|---|
| Base canvas | `bg` / `surface` | shell, thread, composer |
| Quiet grouping | `surface-2` | user bubble, expanded detail where needed |
| Boundary | `border` | separation and disclosure edges |
| Primary action | `accent` / `on-accent` | Send, selected/active action, focus |
| Running | `warning` | active tool state and risk context |
| Success | `success` | settled completion and confirmation |
| Failure | `danger` | failed tool, destructive action, error |
| Text hierarchy | `text`, `text-muted`, `text-faint` | content, metadata, tertiary labels |

No raw hex values are added to components. A brighter logo-derived cobalt can be evaluated later as a token experiment with complete contrast and visual-regression evidence; it is not bundled into this layout correction.

## 7. Responsive Behavior

### Desktop reference: 1440 × 1000

- Assistant prose uses the target reading measure while displays can remain wider.
- Action rows stay attached to their message without covering content.
- Tool and reasoning rows remain compact and scan as a timeline.
- The full runtime cluster remains available.

### Mobile reference: 393 × 852

- No horizontal page scroll.
- No message, attachment, or action is obscured by an adjacent control.
- All frequent controls have at least a 44 × 44 px target.
- Long tool names and approval text wrap without pushing actions off-screen.
- Prompt starters fit without clipped labels.
- Footer detail is collapsed initially and does not crowd the composer.

## 8. Accessibility and Localization

- Maintain or add `aria-expanded` and `aria-controls` for reasoning, raw tool data, and telemetry disclosures.
- Every icon-only control has a localized accessible name.
- Focus indicators use the existing semantic accent and remain visible against their surface.
- Status always includes text; warning/success/error color is never the sole cue.
- Dynamic telemetry remains a settled-only polite live region.
- Raw tool content remains text-only and horizontally scrollable within its own region.
- Approval question and tool content remain escaped; no `dangerouslySetInnerHTML` is introduced.
- English and Italian resources remain key-parity complete.
- Reduced-motion behavior remains respected for disclosure icons and transitions.

## 9. Architecture and Data Boundaries

The work is presentation-only and must preserve these seams:

- `ExternalStoreChat` continues to own runtime and streaming state.
- `ExternalStoreChat_messages` remains a presentational renderer.
- `sseAdapter` event handling and message content shapes do not change.
- `DisplayRouter` remains the only typed-result routing seam.
- `ToolActivityCard` remains the safe fallback for untyped tool output.
- Approval resolution continues through `useResolveApproval` with the current payload contract.
- Composer text is populated through the existing external-store/draft prompt API.
- No component accepts or renders a new backend field.

## 10. Test-First Implementation Contract

Implementation follows the repository's existing test stack and starts with failing behavioral tests.

### Component tests

- `reasoningPref` defaults to collapsed with no stored preference and restores an explicit saved preference.
- Reasoning, tool, and telemetry disclosures expose correct expanded state and controls.
- Raw tool activity preserves running/settled/manual-toggle behavior and XSS-safe text rendering.
- Risky approvals never render the resume token but still submit the correct token to the existing endpoint.
- Answer, Decline, Cancel, terminal, failure, and inline confirmation behavior remain unchanged.
- Empty-state starters populate and focus the composer without submitting.
- English/Italian key parity remains green.

### Layout assertions

- Plain assistant prose receives a reading-measure wrapper; typed displays do not.
- Message actions have a dedicated non-overlay container.
- Mobile controls retain minimum target sizing.
- Long content applies wrapping/overflow containment at the correct boundary.

### Browser verification

Playwright runs in the user-approved Chrome browser against the authenticated Aura instance:

1. capture the same empty, tool timeline, approval, and mobile states used as baselines;
2. verify primary interactions, keyboard focus, disclosures, prompt insertion, and approval controls;
3. assert no console errors, page errors, failed application requests, or horizontal overflow;
4. create implementation screenshots at exactly 1440 × 1000 and 393 × 852;
5. compare each baseline and implementation screenshot together in the same visual review input;
6. fix visible spacing, crop, typography, border, radius, and overlap defects before completion.

### Required project gates

- Targeted Vitest suites for every affected component.
- TypeScript typecheck.
- ESLint on affected sources.
- Existing contrast script with all required pairs passing.
- Focused authenticated Playwright E2E flow.

## 11. Acceptance Criteria

The refinement is complete when all of the following are true:

1. At 393 × 852 and 1440 × 1000, no message action overlaps or crops message content.
2. Plain assistant prose has a bounded reading measure while tool/display/artifact content retains appropriate width.
3. A new browser profile starts with reasoning collapsed; a stored preference is honored.
4. Tool raw-data behavior and escaped rendering pass all existing and new regression tests.
5. Risky approval cards do not expose the resume token in visible text or the rendered DOM.
6. Approval resolve requests still use the correct token and verb payloads.
7. Empty-state starters insert localized editable text, focus the composer, and do not auto-send.
8. Mobile telemetry is initially compact, expands accessibly, and desktop telemetry remains available.
9. No new dependency, runtime protocol change, raw component color, or unsafe HTML path is introduced.
10. English and Italian resources remain complete.
11. The contrast gate passes with no required-pair regression.
12. Baseline and implementation screenshots pass same-input visual comparison with no unresolved overlap, crop, or hierarchy defect.

## 12. Implementation Sequence

The later implementation plan should use small vertical slices in this order:

1. Message action containment and assistant reading measure.
2. Reasoning default and light disclosure treatment.
3. Tool activity visual hierarchy without state-machine changes.
4. Approval hierarchy and resume-token removal.
5. Empty-state starter prompts through the existing composer seam.
6. Runtime footer spacing polish.
7. Full responsive, accessibility, contrast, and Playwright visual verification.

Each slice must leave its targeted tests green before the next slice begins.

## 13. Risks and Mitigations

- **Risk: width wrappers accidentally constrain typed displays.** Keep prose width on the text renderer only and assert displays retain the surrounding turn width.
- **Risk: action discoverability falls when hover chrome is reduced.** Preserve visible focus, touch behavior, accessible names, and a dedicated action row.
- **Risk: changing the reasoning default surprises existing users.** Apply the new default only when no saved preference exists.
- **Risk: token removal breaks resolution.** Remove only the rendered token; keep `approval.token` in the mutation payload and cover it with a request assertion.
- **Risk: empty-state starters create duplicate composer state.** Use the existing draft-prompt/external-store seam and never maintain a second input value.
- **Risk: footer changes regress live announcements.** Preserve `useSettledCluster`, the progressbar semantics, and existing telemetry tests.
- **Risk: visual polish dilutes Aura's design language.** Use only existing tokens, type families, radii, components, and Lucide icons.

## 14. Research References

- assistant-ui Tool UI guidance: <https://www.assistant-ui.com/docs/guides/ToolUI>
- Model Context Protocol Apps extension overview: <https://modelcontextprotocol.io/extensions/apps/overview>
- WAI-ARIA disclosure pattern: <https://www.w3.org/WAI/ARIA/apg/patterns/disclosure/>
- WAI-ARIA feed pattern for dynamic content streams: <https://www.w3.org/WAI/ARIA/apg/patterns/feed/>
- WCAG 2.2 target size minimum: <https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html>

These sources inform the interaction model, but the implementation remains native to Aura's established design system and component contracts.
