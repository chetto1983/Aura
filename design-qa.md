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

- Full frontend suite: 244 files, 2,047 tests passing.
- Coverage: statements 91.11%, branches 85.30%, functions 90.50%, lines 93.08%.
- TypeScript/production build, type-aware lint, lint contract, formatting and
  dead-code checks passed; Go embedded-web test passed.
- Real artifact E2E: 4 passed, covering inline/expanded/split/download/reload and
  the existing sealed-render modal path on desktop and mobile.
- The follow-up export/highlighting tests also passed on both viewports.
- Original asset routes and opaque-origin sandbox retained. No live HTML is
  injected into the parent document. Highlighting only tokenizes source text.

The browser logs the existing warning about `frame-ancestors` in a meta policy;
the authoritative response-header policy is verified by the live render test.
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
