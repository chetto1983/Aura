# Phase 23: Frontend Infrastructure & Industrial Foundation - Context

**Gathered:** 2026-06-16
**Status:** Ready for planning

<domain>
## Phase Boundary

Stand up the **industrial frontend foundation BEFORE any cockpit feature code**
(operator directive 2026-06-15: foundation first). Two-stage shape:

1. **Research-locked decision record (FND-01)** — the FND-01 industrial-infra
   research pass runs at plan time and produces `RESEARCH.md` / a decision
   record that locks linter ruleset, formatter, design-token architecture,
   `web/` package/repo layout, build + release pipeline, and the frontend test
   harness. This is the Gate-1 artifact the rest of the phase builds against.
2. **Scaffold the foundation** — a Vite 8 + React 19 + TypeScript `web/` package
   whose `//go:embed all:dist` output is baked into the single binary and
   renders a **branded, dark-operator-themed placeholder shell** from
   `aura serve`, behind a **blocking zero-warning** lint/format/type-check CI
   gate, with the **Node-24 multi-stage Docker build** producing the embedded
   asset (no Node in the runtime image).

**This is a foundation phase, not a feature phase.** No chat, no auth, no health
panel, no typed displays, no graph — only the scaffold, the theme, the brand,
the build/test/CI pipeline, and a placeholder shell. Requirements: **FND-01..06**.

**Boundary clarification — `aura serve` placeholder shell is in-scope (SC2).**
Phase 23's SC2 already requires `aura serve` to serve the embedded placeholder
shell via `//go:embed all:dist`. So a Playwright E2E that boots `aura serve` and
asserts the branded dark-operator shell (theme-applied-before-paint, no
marketing hero text) is **squarely Phase 23**, not scope creep. Phase 24 then
adds the *real* SPA host (SPA-fallback route exclusion + web-auth boundary +
runtime health) on top of this embed pipeline.

</domain>

<decisions>
## Implementation Decisions

### Linting & formatting (FND-03)
- **D-01 — ESLint (flat config) + Prettier; NOT Biome.** Chosen over Biome for
  the deeper `react-hooks` / `exhaustive-deps` + `jsx-a11y` plugin coverage that
  an accessibility-sensitive operator UI (and assistant-ui later) needs.
- **D-02 — Ruleset baseline = Airbnb-style comprehensive**
  (`eslint-config-airbnb-typescript`). **Integration constraint:** layer
  `eslint-config-prettier` LAST to turn off Airbnb's stylistic rules that
  overlap Prettier so the two don't fight. Also enable
  `eslint-plugin-react-compiler` (Rules of React enforcement — required by D-12).
- **D-03 — Zero-warning blocking gate.** `npm run lint` + `npm run format
  --check` + `tsc --noEmit` all pass with **zero warnings** and run as a blocking
  CI gate — parity with the Go `golangci-lint` zero-warning discipline.
- **D-04 — Package manager DEFERRED to FND-01 research** (npm vs pnpm). SC
  wording uses `npm run …`; npm is the path of least resistance, but the research
  pass weighs pnpm's speed/strictness and locks the choice in the decision record.

### Embed & build pipeline (FND-02, FND-06)
- **D-05 — Commit `web/dist/` into the repo + a CI freshness gate.**
  `go build ./...` / `go install` work with **zero Node** everywhere (Go
  contributors + Go CI jobs never need a toolchain). A CI job rebuilds `dist`
  and **fails on a non-empty `git diff`** (stale-artifact guard). Accepted cost:
  generated `dist/` churns in git history / PR diffs.
- **D-06 — Node-24 multi-stage Docker rebuilds `dist` reproducibly for the
  runtime image; no Node in the runtime layer.** The committed `dist` (D-05) and
  the Docker-rebuilt `dist` must be byte-reproducible — the freshness gate ties
  them together.

### Theme, tokens & scope (FND-04)
- **D-07 — Lock the real dark-operator theme HERE** (not deferred). `tokens.json`
  architecture + a **real v1 dark-operator palette** (elysia-informed,
  industrial / less-decorative, NO abstract sphere) + **density modes**
  (`compact` | `operator` | `review`), **applied before paint** (satisfies SC2 /
  SC4). **Default density = `operator`** (the primary cockpit tier).
- **D-08 — Token pipeline = hand-authored `tokens.json` + a tiny generator**
  (no Style Dictionary / DTCG heavy tooling). The generator (or direct Tailwind 4
  CSS `@theme`) maps tokens → CSS vars + `data-theme` / `data-density`
  attributes. **Apply-before-paint mechanism:** a pre-hydration inline `<head>`
  script reads the persisted theme/density and sets the root attributes before
  React mounts (no flash). Minimal-industrial-shape
  ([[feedback_no_atomic_bombs_minimal_industrial_shape]]).
- **D-09 — Lean scaffold.** NO assistant-ui / chat dependencies in Phase 23 —
  those land in Phase 25. Component-level visuals for real features are deferred
  to the feature phases. The placeholder shell is the only screen.

### PWA & brand (FND-05)
- **D-10 — Full installable PWA + service worker via `vite-plugin-pwa` /
  Workbox.** Content-hash-revisioned precache + `autoUpdate`. **Constraint
  (the one non-trivial bit):** the SW cache MUST version against the build hash
  so a new single-binary release does not serve stale assets — Workbox's
  revisioned precache handles this; verify it in the E2E / release flow.
- **D-11 — Brand: `public/Logo.png` already exists** (repo root) — the `web/`
  scaffold consumes it in the app-shell header + favicon + apple-touch-icon +
  `theme-color` + web manifest. **No marketing hero text / decorative badges /
  tutorial paragraphs in the primary viewport** (ux-spec Copy Contract, §350).

### React baseline & shell robustness
- **D-12 — React Compiler ENABLED now** (`babel-plugin-react-compiler`) so all
  future components get auto-memoization for free. **Implication:** the Vite
  React plugin must be the **Babel-based `@vitejs/plugin-react`** (to host the
  compiler), NOT `@vitejs/plugin-react-swc`. `eslint-plugin-react-compiler`
  stays on (D-02) to enforce Rules of React.
- **D-13 — Root React error boundary** rendering a safe themed fallback (no
  white-screen-of-death — industrial baseline). **No client error telemetry
  yet** — there is no backend sink; defer.

### Routing
- **D-14 — No router in Phase 23.** The shell is a single placeholder screen.
  Lock **React Router** as the *intended* choice in the decision record; wire it
  + real SPA routes in Phase 24 when the serve host exists. Consistent with the
  lean-scaffold call (D-09).

### Testing & CI (FND-06)
- **D-15 — Test harness = Vitest + React Testing Library + full Playwright E2E
  booting `aura serve`.** The E2E asserts the **placeholder shell only**: brand
  renders, dark-operator theme + density applied **before paint**, and **no
  marketing hero text** — NOT chat / auth / health (those are Phase 24/25).
  Harness must be wired and **green in CI** (SC5). Runner = **Playwright**
  (cross-browser, strong CI story).
- **D-16 — The Playwright E2E runs on EVERY PR, blocking.** It's a single fast
  smoke; full Claude-Code-parity discipline. CI must build dist → build the Go
  binary with embedded dist → boot `aura serve` on loopback → run Playwright.
- **D-17 — Web CI jobs integrated into the EXISTING CI workflow** (not a separate
  `web.yml`), with path filters that skip web jobs when only Go changes. One
  unified gate, mirroring the single golangci-lint discipline.
- **D-18 — `web/` follows the repo's master-direct commit discipline**
  ([[feedback_master_direct_workflow]]); no separate versioning / changeset
  tooling — `web/` is not independently published.

### Claude's Discretion
- Package-manager choice (D-04) — deferred to the FND-01 research pass.
- Exact Vite plugin list (beyond the Babel React plugin + vite-plugin-pwa +
  Tailwind 4), Playwright project/browser config, and the exact dark-operator
  palette hex values — research/scaffold detail, informed by the ux-spec Design
  Direction (§147) and the elysia-informed board. The *direction* (real dark
  operator palette, default density `operator`) is locked; the values are not.
- `web/` package/repo layout (single package vs workspace) — FND-01 research,
  default to a single `web/` npm package unless research shows otherwise.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents (FND-01 researcher + planner) MUST read these before
planning or implementing.**

### UI/UX contract (the source of truth for look, copy, and structure)
- `docs/design/aura-deep-search-figma/ux-spec.md` — the full Aura Deep Search
  UI/UX spec. For Phase 23 specifically:
  - §147 **Aura Design Direction** — dark operator cockpit, industrial /
    less-decorative, NO abstract sphere; three-zone primary layout.
  - §350 **Copy Contract** — allowed above-the-fold copy; **no marketing hero
    text / decorative badges / tutorial paragraphs** (drives FND-05).
  - §400 **Implementation Model** — event-payload-driven frontend architecture
    (informs later phases; foundation must not preclude it).
  - §521 **Important Non-Goals** — what NOT to build.

### Phase scope & requirements
- `.planning/ROADMAP.md` §"Phase 23" — goal, 5 success criteria, FND-01..06,
  research-first note, build-order (foundation before features).
- `.planning/REQUIREMENTS.md` FND-01..06 (lines 32-37) + **line 137** (the
  ESLint+Prettier vs Biome open question explicitly routed to FND-01 — resolved
  here as D-01/D-02).
- `.planning/PROJECT.md` §"Current Milestone v1.0.0" + §Constraints — milestone
  goal, single-binary deploy invariant, tech-stack seals, assistant-ui as the
  Phase-25 chat lib.

### Brand asset
- `public/Logo.png` — existing brand asset the `web/` scaffold consumes (header +
  favicon + apple-touch-icon source). No placeholder needed (D-11).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`cmd/aura serve` + `internal/agui` (AG-UI/SSE gateway)** — the existing
  single-binary serve command is the **embed host**. Phase 23 adds a Go package
  with `//go:embed all:dist` and makes `aura serve` serve the embedded
  placeholder shell (SC2). The real SPA host (route exclusion, auth, health) is
  Phase 24 — Phase 23 only proves the embed → serve → render path.
- **Go quality-gate discipline** (`golangci-lint`, `make coverage` ≥85%, green
  CI, `cache_invariant_audit.sh`) — the **pattern to mirror** for `web/`:
  zero-warning blocking gate (D-03), every-PR E2E (D-16), unified CI (D-17).

### Established Patterns
- **Single-binary deploy invariant** — everything ships in one Go binary; the
  frontend is embedded, never a separate deployable. D-05/D-06 preserve this
  (committed dist for `go build`; Docker rebuilds dist; no Node in runtime).
- **Minimal industrial shape** — [[feedback_no_atomic_bombs_minimal_industrial_shape]]:
  find the minimal industrial form that meets the success criteria (drives
  D-08 tiny-generator, D-09 lean-scaffold, the rejection of Style Dictionary).

### Integration Points
- `//go:embed all:dist` Go package ↔ `aura serve` HTTP handler (serves the
  embedded shell).
- `web/dist/` (committed, D-05) ↔ Go build ↔ CI freshness gate ↔ Node-24 Docker
  multi-stage rebuild (D-06).
- **Greenfield:** no `web/` directory exists yet — this is a from-scratch
  scaffold.

</code_context>

<specifics>
## Specific Ideas

- **Dark operator cockpit, industrial tone** — borrow Elysia's dark/agentic feel
  but less decorative; trust shown via source state / provenance / execution
  structure, NOT an abstract sphere (ux-spec §147).
- **Density tiers** `compact` / `operator` / `review`, default **`operator`**
  (ux-spec `set_density`).
- **Copy Contract is law** — no marketing hero text in the primary viewport.
- **Parity-with-Go discipline** — the web gate should feel like `golangci-lint`:
  zero warnings, blocking, on every PR.

</specifics>

<deferred>
## Deferred Ideas

- **assistant-ui chat stack** (`@assistant-ui/react-ag-ui` + chat lane) → **Phase
  25** (Chat + Approval Center). Not installed in Phase 23 (D-09).
- **React Router wiring + real SPA routes + SPA-fallback route exclusion** →
  **Phase 24** (Web Foundation — Serve + Auth + Health). Router *choice* locked
  here (D-14), not wired.
- **Real SPA host, web-auth boundary (GAP-2), non-loopback boot guard, runtime
  health panel** → **Phase 24**.
- **Typed-display router, graph explorer, governance boards, onboarding** →
  Phases 26-29.
- **Optional deeper visual contract via `/gsd-ui-phase`** — the user chose to
  lock a real palette in Phase 23 (D-07) rather than gate on a separate UI-SPEC;
  a `/gsd-ui-phase 23` pass remains available if a designed visual contract is
  wanted before scaffolding, but is not required by these decisions.
- **Client error telemetry sink** (D-13) → a later phase once a backend endpoint
  exists.

None of these expand Phase 23 scope — they are the next phases' work, recorded
so they are not lost.

</deferred>

---

*Phase: 23-frontend-infrastructure-industrial-foundation*
*Context gathered: 2026-06-16*
