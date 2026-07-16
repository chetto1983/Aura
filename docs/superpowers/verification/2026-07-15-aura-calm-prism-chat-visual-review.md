# Calm Prism Chat Visual Review — 2026-07-15

## Inputs

- Baseline commit: `de700c425`
- Implementation commit: `a6345417c`
- Browser: installed Google Chrome through Playwright `channel: 'chrome'`
- Deterministic fixture: identical conversation, long prompt/upload name, reasoning, running and completed tools, typed system result, local artifact, three approval states, and settled telemetry
- States: desktop 1440 × 1000 dark/light; mobile 393 × 852 dark/light; reflow at 320, 720, and 768 CSS px

## Same-input comparisons

| State | Reference | Implementation | Combined comparison | Inspected verdict |
| --- | --- | --- | --- | --- |
| Desktop dark | `2026-07-15-aura-calm-prism-chat/reference-desktop-dark.png` | `2026-07-15-aura-calm-prism-chat/implementation-desktop-dark.png` | `2026-07-15-aura-calm-prism-chat/comparison-desktop-dark.png` | PASS — the reference control/user-content collision and composer-before-approvals ordering are removed; measure, hierarchy, borders, radii, and typography remain coherent. |
| Mobile dark | `2026-07-15-aura-calm-prism-chat/reference-mobile-dark.png` | `2026-07-15-aura-calm-prism-chat/implementation-mobile-dark.png` | `2026-07-15-aura-calm-prism-chat/comparison-mobile-dark.png` | PASS — the reference top composer/control overlap is removed; approval cards remain contained, the collapsed footer cue is legible, text wraps, and no horizontal crop is visible. |

Both combined images were inspected at original detail after Playwright generated them from the same fixture and viewport.

## Automated evidence

- Workspace controls, user bubble, attachment, first prose, and each message action row were measured against their owning content rectangles: no intersections.
- Every action row and prose block was contained in the viewport after its owning region was scrolled into view.
- Horizontal page overflow was absent at 320, 393, 720, 768, and 1440 CSS px.
- Mobile Chrome measured every `[data-required-touch-target]` control at least 44 × 44 CSS px. The initial operator-density measurement exposed a 42.625 px target; required controls now use literal 44 px minima and the rerun passed.
- Fine-pointer action rows appeared on hover and keyboard focus; coarse-pointer action rows were visible before interaction.
- Browser health was clean: no unallowlisted console errors, page errors, request failures, auth/polling/asset failures, or HTTP errors. The exercised approval conflict is constrained to the exact expected `POST` path and 409 status.
- Four accepted goldens passed non-update comparison in installed Chrome: desktop dark/light and mobile dark/light.

## Manual and assistive-mode evidence

- 200% zoom/reflow equivalent: PASS at a 720 × 500 CSS canvas, equivalent to the 1440 × 1000 desktop canvas at 200%; no horizontal overflow or lost content was detected.
- Keyboard-only: PASS. Focus entered the artifacts toggle and drawer, Escape closed the drawer, and focus returned to the opener. This smoke initially found missing focus restoration; `Drawer` now captures and restores the invoking element, and the rerun passed.
- Reduced motion: PASS with Chrome reporting `prefers-reduced-motion: reduce`; the deterministic state settled without animation-dependent disclosure.
- Forced colors: PASS with Chrome reporting `forced-colors: active`; controls, status text, focus ownership, and content remained operable.
- Screen-reader smoke proxy: PASS against Chrome's full accessibility tree. There is one settled telemetry `StaticText` under the single polite status region, `Approval required` occurs before the disabled `Ask Aura` textbox in reading order, and the composer remains locked while approvals are pending. This is an accessibility-tree smoke, not an assistive-technology certification.
- Touch/coarse pointer: PASS in mobile Chrome; all required targets measured at least 44 × 44 CSS px and message actions required no hover.

## Final verdict

PASS — no unresolved crop, overlap, spacing, typography, border, radius, focus, hierarchy, or truthfulness defect remains in the tested states. This is not a claim of full WCAG conformance.
