# Aura Figma Visual Debug

This folder includes local visual-debug infrastructure for the Aura Figma
capture package. It verifies the HTML capture surface before importing or
refreshing the Figma file.

## Setup

```powershell
cd D:\Aura\docs\design\aura-deep-search-figma
npm install
npm run visual:install-browsers
```

## Commands

```powershell
npm run serve
npm run visual:debug
npm run visual:headed
npm run visual:trace
npm run visual:chrome
npm run visual:edge
```

`visual:debug` starts or reuses a local server, opens the capture page in
Playwright, waits for `data-capture-ready`, and writes screenshots plus a JSON
report under `.visual-debug/<timestamp>/`.

Artifacts produced:

- `desktop-viewport.png`
- `backend-map.png`
- `screens-board.png`
- `prototype-qa.png`
- `report.json`
- `trace.zip` when `visual:trace` is used

## Checks

The runner currently checks:

- Aura project title is present.
- The source SVG imports successfully.
- All 8 project sections exist.
- The 12 backend capability cards render.
- Backend map, screen board, and prototype QA sections are ordered correctly.
- Capability panels, flow steps, component rows, and table cells do not show
  obvious horizontal overflow.
- No runtime page errors are thrown.

Use `visual:headed` when inspecting layout manually, and `visual:trace` when a
capture failure needs Playwright trace replay.
