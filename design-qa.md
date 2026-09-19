# Artifact workspace design QA — 2026-09-08

Target: the user's three ChatGPT screenshots (inline preview, expanded preview,
source beside preview), with Aura's existing navigation, typography and themes.
The weather dashboard is agent-generated demo content, not a weather product or
a claim of pixel-identical reproduction of the supplied dashboard.

## Evidence and findings

1. Baseline: a delivered HTML file opened in a modal over chat. The live asset
   rendered and ran, but did not provide the requested inline workflow.
2. Inline: a 768px maximum-width card, 48px filename toolbar, code/preview/expand
   controls, 480px scrollable preview and separate file/download card. The first
   capture exposed an overly wide card; the width was corrected and recaptured.
3. Expanded: keeps Aura navigation, displays the selected document, and returns
   to the initiating control without discarding the chat runtime or draft.
4. Split: source at left and live preview at right on desktop, independently
   scrollable and resizable. At narrow widths the panes stack vertically.
5. Interaction: showing/hiding source retains iframe state. The live demo's
   Next day button changed its selected-day heading. Download and reload worked.
6. Regression review caught an unnecessary chat remount on thread changes. The
   workspace now clears only artifact selection; conversation/usage tests pass.

Evidence: [desktop split](.planning/tmp/artifact-evidence/aura-artifact-split-final.png),
[inline](.planning/tmp/artifact-evidence/aura-artifact-inline-final.png),
[expanded](.planning/tmp/artifact-evidence/aura-artifact-expanded-final.png).
Desktop reference comparison used a 1349×871 viewport. The real Playwright run
also exercised desktop Chrome and Pixel 5 mobile Chrome with no page overflow.

## Verification

- Full frontend suite: 244 files, 2,049 tests passing.
- Coverage: statements 91.06%, branches 85.28%, functions 90.43%, lines 93.05%.
- TypeScript/production build, type-aware lint, lint contract, formatting and
  dead-code checks passed; Go embedded-web test passed.
- Real artifact E2E: 6 passed, covering composer anchoring, inline/expanded/split/download/reload and
  the existing sealed-render modal path on desktop and mobile.
- The follow-up export/highlighting tests also passed on both viewports.
- Original asset routes and opaque-origin sandbox retained. No live HTML is
  injected into the parent document. Highlighting only tokenizes source text.

The ignored `frame-ancestors` meta directive was removed; it remains enforced in
the authoritative response header and is covered by the render-policy tests.
The temporary Vite proxy initially lacked authentication redirects; final E2E
runs use the rebuilt Aura container on its normal port, with real authentication.

Limits: this implements viewing and export, not direct editing, version history,
arbitrary React builds or unrestricted network access. Those controls are absent.

final result: passed

## References

- [ChatGPT writing/code blocks](https://help.openai.com/en/articles/20001246)
- [LibreChat artifacts](https://www.librechat.ai/docs/features/artifacts)
- [assistant-ui example](https://www.assistant-ui.com/examples/artifacts)
- [assistant-ui workbench source](https://github.com/assistant-ui/assistant-ui/blob/main/examples/with-artifacts/app/artifact-surface.tsx)

The assistant-ui example owns its workbench UI in application code. Aura reuses
its installed assistant-ui message rendering and existing artifact routes,
React context selection, lazy renderers, Shiki and resizable-panel components.

---

# Studio mobile design QA — 2026-09-19

## Comparison target

- Source visual truth: `.playwright-mcp/source-mobile.png` and
  `.playwright-mcp/source-mobile-expanded.png`, captured from the BytePlus Lumina video page.
- Implementation: `.playwright-mcp/aura-studio-after.png` and
  `.playwright-mcp/aura-studio-after-expanded.png`.
- Side-by-side evidence: `.playwright-mcp/aura-studio-after-comparison.png` and
  `.playwright-mcp/aura-studio-after-expanded-comparison.png`.
- Viewport and density: both source and implementation are 390 × 844 CSS px and 390 × 844
  image px at deviceScaleFactor 1; no density normalization was required.
- State: mobile, light Aura theme, video mode, empty prompt, collapsed and expanded composer.

## Findings

No actionable P0, P1 or P2 mismatches remain. The mobile composer now measures 366.75 px wide
with 11.625 px side margins; the source measures 366 px with 12 px side margins. Both keep one
bottom control rail, a fixed primary action, a large angled material tile and an expand control.
The expanded state uses the same 12 px side inset as the source.

Aura intentionally retains its own shell, bottom navigation, Fraunces/Atkinson typography,
light/dark tokens, localized copy, model catalog, cost estimate and image/video segmented mode.
Those are product constraints rather than source drift. The composer geometry, hierarchy and
interaction placement follow the source.

## Focused evidence

- Settings popover: `.playwright-mcp/aura-studio-after-options.png`; it opens upward at nearly
  full mobile width and remains above the composer.
- Model picker: `.playwright-mcp/aura-studio-after-models.png`; it opens upward and remains fully
  reachable above the fixed action edge.
- Desktop regression: `.playwright-mcp/aura-studio-after-desktop.png` at 1440 × 1000.
- Real authenticated baseline: `.playwright-mcp/aura-studio-before.png`, captured from
  `localhost:9080` with the delivered video/history state before implementation.

## Fidelity surfaces

- Fonts and typography: Aura brand families, hierarchy, weights, wrapping and line heights are
  preserved; mobile prompt copy remains readable without truncation.
- Spacing and layout rhythm: source-width composer, 14 px radius, 12 px insets, 88 px material
  tile, 62 px prompt field and single 32 px control rail verified at 390 px.
- Colors and tokens: all surfaces use existing Aura theme tokens in light and dark modes; no
  BytePlus brand colors were copied.
- Image and icon fidelity: Aura's real logo and Lucide controls are retained; no placeholder,
  handmade SVG or CSS-drawn asset was introduced.
- Copy and content: Aura's Italian localization, live model names, prices and actions are
  preserved.

## Interaction and verification history

1. Baseline showed a 319.25 px composer split across three rows because the mobile history rail
   consumed 40 px.
2. The history trigger moved to an overlay, the composer became 366.75 px wide, options became a
   horizontal rail and the action edge remained fixed.
3. First visual pass exposed an uncentered empty state; responsive alignment was corrected and
   recaptured.
4. Focused popover capture led to full-width settings and an upward-opening model picker.
5. Expand, collapse, settings and model-picker interactions were exercised. Browser console
   errors checked: 0.

Verification: 174 Studio tests pass; TypeScript, type-aware lint, targeted formatting and the
production build pass. The repository-wide format check remains blocked only by the unrelated
pre-existing `web/src/settings/__tests__/ModelSettingsPanel.voice.test.tsx` change.

final result: passed

## Follow-up operational checks

- The current weather artifact loaded seven live forecast cards inside Aura's
  opaque-origin preview after enabling its specific API origin. Next day changed
  the selection. Unconfigured origins remain blocked.
- Composer geometry remained within 14px of the chat bottom during transcript
  scrolling, multiline drafting and viewport changes on desktop and mobile.
- Python Playwright 1.62.0 and matching Chromium are baked into the sandbox image.
  The offline image test launches Chromium, executes JavaScript and captures PNG.
- A real Aura run authored a counter, ran a Playwright check, corrected its test,
  reran it successfully and only then called send_file. The run also exposed an
  empty-message attachment bug: saved assets now use visible answer turns, and
  live artifact events receive the actual execution's tool-call correlation ID.
- The supplied anthropics/web-artifacts-builder skill was installed and is active
  for the operator. Aura's instruction to validate before delivery takes precedence
  over the upstream skill's optional-testing advice.
- The first XLSX/DOCX pair reopened successfully, matched all seven rows, rendered
  in Aura and downloaded. XLSX contained one chart. Content QA identified wrong
  coordinates inherited from the original HTML; corrected versions were requested
  using verified Caraglio geocoding (44.41725, 7.43281), a single forecast response
  and a distinct actual UTC retrieval timestamp.

Final Office verification: the corrected XLSX and DOCX contain the same seven
unique dates and temperatures; XLSX retains its chart. A final comparison caught
different per-file timestamps, so both files were synchronized from one new API
response with the shared retrieval time `2026-09-08T18:25:51+00:00`, reopened and
compared, then redelivered by Aura. Both formats render and download successfully.
