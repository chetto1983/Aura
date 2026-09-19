# Studio video editor — research (2026-09-19)

Input for the brainstorming of the extended Studio editor, the one after Plan A (trim, audio, rotate and crop on Mediabunny).

- `engine-inventory.md`: browser video engines, timeline widgets and undo libraries, compared by license, React 19 support, export path, features, gzip size and network behaviour.
  The operator chose **VideoFlow** (`@videoflow/core` + renderers, Apache-2.0; it reuses mediabunny).
- `clideo-tour.md`: a UX tour of Clideo's editor (multi-track), ranking what to take and what not to copy.
- `adobe-express-tour.md`: a UX tour of Adobe Express (scenes and templates), with the same ranking plus the comparison of scenes against multi-track.
  Its conclusion: for short AI clips, scenes win.

The screenshots named in the two tour reports (about 50 MB) are not in the repo. They are in `D:/tmp/studio-editor-research/{clideo-tour,adobe-tour}/`.
The feasibility spike that follows is `.planning/spikes/107-studio-timeline-editor/`.

Constraint from the operator: once it works by hand, everything must also be drivable by agents.
The project is declarative JSON, every edit is a command, and the render needs no human.
