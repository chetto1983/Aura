# Odysseus Frontend Pattern Study For Aura

Date: 2026-06-04

Source repo cloned to `D:\tmp\odysseus` from
`https://github.com/pewdiepie-archdaemon/odysseus`.

## What Odysseus Is

Odysseus is a self-hosted AI workspace with a Python backend and a static,
module-heavy frontend. It is not a React/Next app. The primary frontend is
`static/index.html`, `static/style.css`, `static/app.js`, and many focused
modules under `static/js/`.

The useful lesson for Aura is not the visual skin or feature breadth. The useful
lesson is the app-shell behavior: it feels like a local operator OS where tools
can open, dock, minimize, resume, and keep background state alive.

## Patterns To Apply To Aura

### Adaptive Rail

Odysseus has both a wide sidebar and a collapsed icon rail. The rail carries
tool entry points and state dots for active, minimized, running, and error
states.

Aura application:
- Keep `Chat`, `Tree`, `Graph`, `Displays`, and `Settings` as primary modes.
- Add a persistent icon rail for secondary tools: Research, Compare, Source
  Explorer, Logs, Theme, Run Inspector.
- Show job state on the rail so operators do not need to keep every panel open.

### Dockable Tools

Odysseus tools are modal windows with minimize, restore, dock, and tile
behavior. Minimized windows become dock chips and keep their state.

Aura application:
- Deep Research can run as a dockable background job while the user continues
  in chat or graph mode.
- Compare can open as a pane set, then minimize without losing streams or vote
  state.
- Graph and Source Explorer can dock side by side for source-to-path review.

### Command Palette And Slash Actions

Odysseus combines `Ctrl+K` global search with slash-command actions.

Aura application:
- Command palette searches runs, source ids, graph nodes, tools, and settings.
- Slash actions are shortcuts to visible UI actions, not a separate hidden UX.
- Suggested commands: `/graph`, `/research`, `/compare`, `/sources`, `/logs`.

### AI UI-Control Events

Odysseus accepts structured UI-control events from backend streams, such as
opening panels, highlighting targets, changing safe toggles, or reporting a
background research job.

Aura application:
- Add a separate `ui_control` event lane next to display payloads.
- Allow only schema-validated actions: `open_panel`, `highlight_source`,
  `set_mode`, `show_job`, `set_density`, `theme_preview`.
- Log every UI-control event in the run log and make it replayable.
- Never allow arbitrary CSS selectors, scripts, URLs, or unbounded DOM mutation.

### Early Theme And Density Boot

Odysseus applies localStorage theme, density, font, favicon, and mobile
theme-color before the app fully boots.

Aura application:
- Persist density modes: `compact`, `operator`, `review`.
- Paint tokens before app hydration or first render.
- Use route/mode-aware title and icon metadata for installed/mobile use.

### Background Job Feedback

Odysseus long-running jobs update the rail, modal card, and notification states
even when the panel is closed.

Aura application:
- Treat research, compare, graph expansion, and source indexing as
  background-job payloads.
- Show running round, warnings, completion, and failure in rail/dock surfaces.

## Patterns Not To Copy

- Do not copy Odysseus's broad personal workspace into Aura. Aura is an
  investigation cockpit, not chat + notes + calendar + gallery + email.
- Do not copy the playful visual language directly. Aura should stay industrial
  and evidence-focused.
- Do not let dockable tools multiply into clutter. Use docking only for tasks
  that benefit from side-by-side evidence work.
- Do not expose arbitrary frontend automation to model output.

## Source Files Read

- `README.md`
- `static/index.html`
- `static/style.css`
- `static/app.js`
- `static/js/MODULE_SUMMARY.md`
- `static/js/init.js`
- `static/js/ui.js`
- `static/js/chatRenderer.js`
- `static/js/chatStream.js`
- `static/js/modalManager.js`
- `static/js/sidebar-layout.js`
- `static/js/theme.js`
- `static/js/keyboard-shortcuts.js`
- `static/js/compare/index.js`
- `static/js/research/panel.js`
