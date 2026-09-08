# Native artifact toolchain — 2026-09-08

The operator requested a sandbox containing the complete artifact toolchain and
a native artifact skill after inspecting conversation
`01a0825d-83d6-7d2b-a6e9-0db5603d26c2`.

## Observed failures

The original skill scaffold installed current dependencies against incompatible
bundled component APIs. Its generated favicon broke Parcel, TypeScript rejected
baseUrl, and calendar/panel types failed. It delivered bundle.html before checking
the full project and left that old bundle available after source changes. The
artifact described fixed values as real-time and ran no browser validation.

## Implemented behavior

- Native skill and full resource tree under `internal/skills/embed` with the
  upstream Apache-2.0 license and maintenance notice.
- Prepared dependency tree in `/opt/aura-artifacts/template`; normal creation
  copies it without network. pnpm, TypeScript and Vite commands are available.
- Calendar and panel dependencies match the shipped archive (DayPicker 9.14.0,
  react-resizable-panels 2.1.9). Relative TypeScript paths need no baseUrl.
- Full project build and exact-HTML browser smoke before bundle publication.
  A failed replacement removes the old bundle.
- Explicit `/skills/web-artifacts-builder/scripts/` paths in the instructions.
- Native resources exported at bootstrap. A real agent probe found that loading
  the instructions alone did not export scripts; a regression test reproduced the
  missing export before the fix and passed afterward.

## Evidence

The final sandbox contract runs with `--network none` and passes full build,
bundling, desktop/mobile screenshots, calendar/panels, a counter click and
rejection of an intentionally invalid TypeScript replacement. Native materializing
and export tests pass with the Go race detector.

The contract also injects a JavaScript exception into a type-correct application:
the browser gate rejects it and leaves no deliverable bundle. This independently
proves the browser check is a gate rather than a screenshot-only success message.

Real Aura conversation `01a0827c-9ad6-79fb-b1b5-fa920a43fb5f` loaded the native
instructions, cloned the prepared template and ran the complete bundle command.
TypeScript/Vite, Parcel and both browser smoke checks passed before send_file.
The delivered bundle rendered in Aura; its Celsius/Fahrenheit toggle changed
21.3°C to 70.3°F and back, including the perceived temperature. The screenshot is
saved locally in `.planning/tmp/native-artifact-aura-preview.png`.

The smoke check proves rendering/error and basic geometry conditions, not every
application interaction or data claim. The Celsius/Fahrenheit assertion above
was independently exercised in Aura. The agent also attempted bare tsc/vite after
delivery; those commands are now exposed from the prepared tree to avoid that
unnecessary failure. Existing workspace files were preserved during installation.

Conversation `01a08290-60e5-75f0-97bd-be93f4f13ef2` independently used the prepared
template for a Bitcoin chart. Its final bundle passed the complete build and both
browser smoke checks before delivery. Opening accepted asset
`399992e0-141f-4ac5-9b59-69c7246540ba` through Aura's render route produced no
JavaScript errors; hovering the chart displayed the date and matching embedded
price. The screenshot is `.planning/tmp/bitcoin-artifact-preview.png`.
This does not establish independent market-data accuracy. The conversation also
exposed a missing `yf` executable in the separately installed Yahoo Finance skill,
unnecessary data-retrieval retries and no agent-authored interaction assertions.

## Installed skill resource check

The operator additionally requested verification that installing any skill carries
its scripts. A local fixture containing `SKILL.md`, `scripts/check.sh` and
`assets/value.txt` was installed through the authenticated governance endpoint,
using the real `npx skills add --copy` transport. Both resource files reached the
identity's active and export trees. In conversation
`01a0829e-9562-7f12-8af9-f59aa5858477`, Aura executed the script from `/skills` and
read its bundled asset: `script-and-asset-ok`, exit code 0, with no stderr after
correcting Windows line endings in the test fixture and reinstalling it. The
temporary skill was archived through the same governance API afterward.

This proves installation and reinstall preserve supplied nested resources and the
next agent shell command refreshes its sandbox copy. It does not prove that an
upstream package contains every script mentioned by its instructions. Yahoo
Finance's installed source tree lacks `yf`. The original pinned repository URL
returns 404; the public [package listing](https://playbooks.com/skills/openclaw/skills/yahoo-finance)
also contains only `SKILL.md` and `_meta.json`, and the
[public mirror](https://github.com/sundial-org/awesome-openclaw-skills/tree/main/skills/yahoo-finance)
contains only instructions. This corroborates an incomplete package but does not
reconstruct the unavailable original tree. The integration fixture now also checks
exact script and asset bytes in both active and export trees; its db_integration
package run passed in the disposable coverage database.
