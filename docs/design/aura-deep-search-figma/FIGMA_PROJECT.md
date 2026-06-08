# Aura Deep Search Figma Project

This folder now acts as the repeatable setup package for a professional Aura
Figma project. It is meant for a senior UI/UX designer who needs more than a
screen import: project structure, foundations, component inventory, workflow
contracts, governance surfaces, and handoff gates.

## Figma File

- Current file: https://www.figma.com/design/mxtyHm1htJhd2lAg8ZxdN8?node-id=2-2
- Current imported source board: `aura-deep-search-elysia-informed.svg`
- Capture helper: `figma-capture.html`
- Manifest: `figma-project-manifest.json`

The current Figma file already contains the original 8-frame board as editable
text/vector layers. Figma MCP writes hit the Starter-plan call limit during the
last setup run, so the local capture helper is the repeatable path for adding
the richer project infrastructure when the limit resets.

## Professional File Architecture

Use these pages when the Figma file can be edited again:

| Page | Purpose |
| --- | --- |
| `00 README` | Product overview, status, constraints, links, contribution rules. |
| `01 Research` | Elysia and Odysseus source-study notes, product deltas, non-goals. |
| `02 Foundations` | Variables, color, type, spacing, radius, effects, density, grids. |
| `03 Components` | Main components, variants, descriptions, usage notes, QA states. |
| `04 Patterns` | Typed payloads, dockable tools, `ui_control`, approvals, graph evidence. |
| `05 Backend Map` | Source-backed runtime, tools, scheduler, MCP, skills, AG-UI, memory, packaging. |
| `06 Screens` | Desktop, mobile, graph, source explorer, MCP, skills, events. |
| `07 Prototype QA` | Click paths, review checklist, accessibility, red-team flows. |
| `99 Archive` | Old boards, rejected concepts, experiments, screenshots, parking lot. |

## Backend Capability Exposure

The backend map is now a first-class design source:

- Human-readable map: `BACKEND_CAPABILITY_MAP.md`
- Machine-readable map: `backend-capability-map.json`
- Figma capture section: `05 Backend capability map` in `figma-capture.html`

The board must expose these Aura systems as product surfaces:

- Runtime shell: `aura serve`, `aura chat`, `aura shell`, registry boot, AG-UI health.
- Agent loop: `LlmAgent`, Event stream, reasoning chunks, `text_response`,
  tool-call lifecycle, budget/dedup/completion gates.
- Tool registry: non-deferred core tools, deferred `web_search`/`web_fetch`/
  `swarm_spawn`, MCP-mounted tools, `tool_search`, `read_tool_output`.
- Conversations, HITL, identity: persisted threads, FTS search, sidecar spill,
  `ask_user`, `paused_states`, `capability_grants`.
- Web safety: SearXNG, readable fetch, DNS pinning, SSRF block, redirect block,
  unsupported scheme/content, response cap, timeout.
- Execution: host shell, sandbox-agent, filesystem read/write/edit/search,
  mutating warning, sidecar output pager.
- Scheduler: schedule/list/cancel/run_now/approve/runs/doctor, reminders,
  agent jobs, backups, skill TTL sweep.
- Skills and snippets: active/pending/archived roots, create/update/delete gates,
  snippet save/restore/archive, always-on block, append-only audit.
- MCP manager: managed config v2, recipes, profiles, trust classes, runtime,
  status/logs/doctor/tools, risk policy.
- Knowledge and memory: Neo4j schema/MCP today plus Phase 15 document ingest,
  entity resolution, GraphRAG, agent journal/insights.
- Swarm and AG-UI: worker reports, fan-out preflight, SSE event timeline,
  resumable interrupts, redacted errors.
- Planned channels and packaging: Telegram/multimodal setup, artifact delivery,
  fat container, keyless boot, installer/service health.

Every screen that touches one of these systems needs visible provenance, risk,
status, empty/loading/error states, and recovery actions. Mutating paths need an
approval or confirmation pattern; blocked web and MCP states must show safe
reason copy without leaking secrets.

## Design System Decisions

- Use `tokens.json` as the local source of truth for Aura variables.
- Keep variables grouped by collection: `Aura / Color`, `Aura / Space`,
  `Aura / Radius`, `Aura / Typography`, `Aura / Governance`.
- Use slash-separated component names, such as `Aura/Button/Primary` and
  `Aura/Skill/InstallGate`, so assets and swap menus remain searchable.
- Add short descriptions to styles, variables, components, and component sets.
  These descriptions support library search, Assets usage, and Dev Mode review.
- Treat graph, MCP, skills, approval, and system-event surfaces as product
  components, not generic cards.
- Keep exploratory patterns in `99 Archive` until they have token coverage,
  component coverage, responsive behavior, and safety states.

## Component Library Map

Core publish candidates:

- `Aura/Button`
- `Aura/Chip`
- `Aura/Input`
- `Aura/Table`
- `Aura/Panel`
- `Aura/RiskBadge`
- `Aura/Shell/Rail`
- `Aura/DisplayRouter`
- `Aura/Graph/Canvas`
- `Aura/Graph/Inspector`
- `Aura/MCP/ServerRow`
- `Aura/MCP/ToolAllowlist`
- `Aura/Skill/InstallGate`
- `Aura/Skill/LibraryRow`
- `Aura/Approval/QueueItem`
- `Aura/SystemEvent`

Each candidate should have:

- Main component or component set.
- Variants and properties for state, density, risk tier, and intent.
- Description with intended use and search keywords.
- Example instances in screen context.
- Empty, loading, warning, blocked, disabled, error, and destructive states
  where relevant.

## Capture Workflow

1. Serve this folder locally:

   ```powershell
   python -m http.server 8877 -d D:\Aura\docs\design\aura-deep-search-figma
   ```

2. Use Figma MCP `generate_figma_design` to create a capture ID for the target
   file.

3. Open:

   ```text
   http://127.0.0.1:8877/figma-capture.html#figmacapture=<captureId>&figmaendpoint=<encodedEndpoint>&figmadelay=2500&figmaselector=%23aura-figma-project
   ```

4. Poll the capture ID until complete.

5. Move imported sections into the page structure above. Keep the source screen
   board on `05 Screens`.

## Visual Debug Workflow

Install and run the local Playwright harness before importing or refreshing the
Figma file:

```powershell
cd D:\Aura\docs\design\aura-deep-search-figma
npm install
npm run visual:install-browsers
npm run visual:debug
```

The runner creates `.visual-debug/<timestamp>/` with desktop and section
screenshots plus `report.json`. Use `npm run visual:headed` for manual
inspection, `npm run visual:trace` for Playwright trace replay, and
`npm run serve` to keep the capture page available at
`http://127.0.0.1:8877/figma-capture.html`.

## Figma Guidance Used

- Figma libraries share reusable components, styles, and variables; published
  updates can be reviewed and accepted in consuming files.
- Figma recommends smaller reusable components as building blocks before larger
  patterns.
- Figma organizes component assets by file, page, and frame, and recommends
  slash-separated naming such as `Button/Active`.
- Figma sections help organize canvas content and can be marked Ready for Dev.
- Figma variables support tokens, collections, groups, aliasing, and modes for
  contexts such as themes or device sizes.
- Figma Dev Mode exposes variables, statuses, annotations, component metadata,
  and Ready for Dev views for handoff.
- Component properties and variants should model named states instead of
  duplicating unmanaged components.
- Figma descriptions on components, styles, and variables help search,
  libraries, Assets, and Dev Mode.

Sources:

- https://www.figma.com/best-practices/components-styles-and-shared-libraries/
- https://help.figma.com/hc/en-us/articles/360038663994-Name-and-organize-components
- https://help.figma.com/hc/en-us/articles/9771500257687-Organize-your-canvas-with-sections
- https://help.figma.com/hc/en-us/articles/15023124644247-Guide-to-Dev-Mode
- https://help.figma.com/hc/en-us/articles/39636737843735-Components-collection-Variants-and-component-set-fundamentals
- https://help.figma.com/hc/en-us/articles/5579474826519-Explore-component-properties
- https://help.figma.com/hc/en-us/articles/14506821864087-Overview-of-variables-collections-and-modes
- https://help.figma.com/hc/en-us/articles/360041051154-Guide-to-libraries-in-Figma
- https://help.figma.com/hc/en-us/articles/7938814091287-Add-descriptions-to-styles-components-and-variables

## Done Criteria

The Figma project is ready for senior design review when:

- Pages follow the architecture above.
- Aura variables and text/effect styles exist locally.
- Component names use `Aura/...` slash naming.
- Components have descriptions and usage examples.
- Screens are backed by component instances where practical.
- Graph, MCP, skills, approvals, and system events have explicit state models.
- Backend capability groups have matching Figma surfaces and components.
- Prototype paths cover desktop, compact rail, mobile drawer, graph inspector,
  MCP doctor/tools, skill approval, blocked URL, and destructive action flows.
- Handoff notes map screens back to Aura event payloads and implementation
  tokens.
