---
name: web-artifacts-builder
description: Create and validate HTML artifacts in Aura using React, TypeScript, Tailwind and shadcn/ui. Use when the operator requests this skill or asks for a dashboard, interactive page, visualization or web artifact. The sandbox provides the complete build and browser toolchain; use the scripts and validate the final bundle before delivery.
license: Complete terms in LICENSE.txt
---

# Web Artifacts Builder

This is Aura's native artifact skill. Use it when explicitly requested, including
follow-ups to the same artifact task. Do not substitute hand-written CDN-based
HTML for this workflow. The Aura sandbox includes Node, pnpm, the complete React/
TypeScript/shadcn/Parcel dependency tree, Python Playwright and Chromium.
`init-artifact.sh` copies the prepared template, so normal creation and bundling
need no package installation or network access. Source data retrieval is separate.

To build frontend artifacts in Aura, follow these steps:
1. Initialize the frontend repo using `/skills/web-artifacts-builder/scripts/init-artifact.sh`
2. Develop your artifact by editing the generated code
3. Bundle all code into a single HTML file using `/skills/web-artifacts-builder/scripts/bundle-artifact.sh`
4. Inspect the browser smoke report and screenshots; test the requested interactions
5. Deliver only the final verified bundle to the user with send_file

**Stack**: React + TypeScript + Vite + Parcel (bundling) + Tailwind CSS + shadcn/ui

## Design & Style Guidelines

VERY IMPORTANT: To avoid what is often referred to as "AI slop", avoid using excessive centered layouts, purple gradients, uniform rounded corners, and Inter font.

## Quick Start

### Step 1: Initialize Project

Run the initialization script to create a new React project:
```bash
bash /skills/web-artifacts-builder/scripts/init-artifact.sh <project-name>
cd <project-name>
```

This creates a fully configured project with:
- ✅ React + TypeScript (via Vite)
- ✅ Tailwind CSS 3.4.1 with shadcn/ui theming system
- ✅ Path aliases (`@/`) configured
- ✅ 40+ shadcn/ui components pre-installed
- ✅ All Radix UI dependencies included
- ✅ Parcel configured for bundling (via .parcelrc)
- ✅ Node 18+ compatibility (auto-detects and pins Vite version)

### Step 2: Develop Your Artifact

To build the artifact, edit the generated files.

### Step 3: Bundle to Single HTML File

To bundle the React app into a single HTML artifact:
```bash
bash /skills/web-artifacts-builder/scripts/bundle-artifact.sh
```

This creates `bundle.html`, a self-contained artifact with JavaScript, CSS and dependencies inlined, ready for delivery in Aura.

**Requirements**: Your project must have an `index.html` in the root directory.

**What the script does**:
- Uses the preinstalled bundling dependencies (installs them only in environments without the prepared template)
- Creates `.parcelrc` config with path alias support
- Builds with Parcel (no source maps)
- Inlines all assets into single HTML using html-inline

### Step 4: Validate Before Delivery (Required in Aura)

The bundle script first runs the complete TypeScript/Vite build, then Parcel,
then Python Playwright against the exact candidate HTML at desktop and mobile
sizes. It creates `bundle.html` only when these checks pass. A failed build removes
an old `bundle.html`: never deliver a stale file after changing its source.

Read `artifact-validation/report.json` and inspect both screenshots. Write and run
additional Playwright assertions for the actual task's controls, data loading and
visible results. The smoke check alone does not prove every interaction.

All JS, CSS, fonts and images must be bundled. API fetch/XHR may use exact HTTPS
origins listed in `AURA_ARTIFACT_CONNECT_ORIGINS`, but only if the operator has
actually granted those origins in Aura. Setting this variable for a test cannot
grant network permission in the cockpit. Without a grant, fetch data during
creation and embed a timestamped snapshot.

For current facts, read the source/API; search snippets alone are not a current
measurement. Preserve source, location, units and retrieval time. Never label
fixed values as real-time. Never claim the build or browser checks passed when
any check failed, including errors in bundled UI components.

### Step 5: Deliver the Verified Bundle

After every source change, rerun `bash /skills/web-artifacts-builder/scripts/bundle-artifact.sh` and the task
assertions. Only then use `send_file` on that final `bundle.html`. Build success
is not proof of data accuracy; report exactly what the verification checked.

## Reference

- **shadcn/ui components**: https://ui.shadcn.com/docs/components
