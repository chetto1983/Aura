# Artifact workspace — 2026-09-08

## Target and evidence

The user supplied three ChatGPT screenshots: inline HTML preview with filename,
code/preview/expand controls; expanded preview retaining navigation; expanded
source beside preview. These supersede the initial side-panel interpretation.

Live baseline: Aura generated and delivered `artifact-workspace-check.html` in
conversation `01a081fb-57e7-73c6-b593-a2ca77b68b4e`. Its asset is accepted and
served by the existing sealed render route. The current artifact list opens a
modal. This does not demonstrate the requested inline or expanded workflow.

Inventory: LocalArtifactDisplay, HtmlPreview, PreviewModal, asset source context,
useAssetContent, useCopyAction, Shiki, Button and resizable panels already exist.
Reuse their authenticated routes, lazy loading and opaque-origin sandbox.
No dependency, migration or environment variable is required.

## Acceptance and implementation

- HTML deliverables render an inline card, filename toolbar and file download.
- Inline code/preview controls switch the visible content.
- Expand opens the selected artifact within the workspace, retaining navigation.
- Expanded Show code displays source and live preview together; hide reverses it.
- Copy uses original source; download uses the existing asset route.
- Close/Escape returns to chat and restores focus; changing thread clears selection.
- Mobile fits without horizontal page overflow; code/preview remain usable.
- Untrusted HTML retains the sealed route and sandbox without same-origin.
- Existing non-HTML delivery, modal and share workflows continue to work.

Risk: frontend interaction/state changes. Verify focused unit tests, typecheck,
lint, build, browser replay and the live delivered artifact. Capture inline,
expanded and split screenshots and exercise JavaScript in the sealed iframe.
Files remain below 600 lines. Record actual limits rather than claiming editing,
version history, React bundling or server functionality not implemented here.

## Completed verification

Implemented the supplied screenshots' inline/expanded/split workflow for live
artifact display payloads and saved-message attachments. Existing non-HTML chips
and modal renderer routes remain covered. The full suite passed 2,047 tests;
all four coverage measures exceed 85%. Four live E2E checks passed on the updated
Aura container (desktop Chrome and mobile Chrome), including sealed policy,
rendered document, download, focus return and persistence after reload.

The broad regression run found a chat remount caused by keying the whole
workspace to the thread. Fixed by resetting only selection, with a regression
test proving draft DOM/runtime preservation. See `design-qa.md` for visual review.

## User-reported follow-up corrections

- Composer anchoring: bounded the shell/artifact/chat wrappers, disabled the
  resizable panel's independent scroll, constrained transcript flex sizing and
  kept the composer non-shrinking. Added browser geometry checks during scrolling,
  multiline drafting and viewport changes.
- Live weather error: the script executed but its fetch was denied by the offline
  CSP. Added the operator-controlled `AURA_ARTIFACT_CONNECT_ORIGINS` configuration,
  reusing MCP policy validation and cockpit-host exclusion. Configured the observed
  `https://api.open-meteo.com` endpoint locally. No broader network grant.
- Browser validation in the sandbox: verified existing installation by launching
  Chromium and clicking a real JavaScript control; baked driver/browser/dependencies
  into the sandbox image and added an offline executable image contract. Updated
  the agent's delivery instructions to exercise browser/network behavior before
  sending HTML. Verified the actual weather file in the running sandbox.
- The real browser-validation run exposed a hidden attachment case with empty
  assistant placeholders. Fixed visible-turn attribution on replay and stamped
  artifact correlation from the trusted tool execution on live events.
- Installed the operator's supplied web-artifacts-builder skill through Aura's
  existing installer. Generated and tested XLSX/DOCX versions, then requested
  correction of inherited coordinate/timestamp errors found during content QA.
