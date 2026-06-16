# Phase 24: Web Foundation — Serve + Auth + Health - Context

**Gathered:** 2026-06-16
**Status:** Ready for planning

<domain>
## Phase Boundary

Turn the Phase-23 embed pipeline into the **real single-binary SPA host** on
`aura serve`, plus the **minimum GAP-2 web-auth boundary** and a **read-only
runtime health shell** — preserving the single-binary deploy invariant.
Requirements: **WEB-01..04**.

Four deliverables (all on the ONE existing loopback `http.Server` that already
carries the AG-UI gateway + the Phase-23 static embed — no new listener):

1. **WEB-01 — real SPA host.** React Router (locked in Phase 23 D-14) wired; a
   SPA catch-all serves `index.html` for unknown *client* routes, while
   API / `/agent` / health routes are **excluded** so they return real 404s,
   never the SPA shell. (Phase 23's `serve_webui.go` is a static placeholder
   with no fallback — this phase adds the fallback + exclusion.)
2. **WEB-02 — fail-fast non-loopback boot guard.** Today `AURA_AGUI_BIND` is
   hardcoded loopback with no escape hatch (amendment #35); this phase
   introduces the non-loopback capability and guards it.
3. **WEB-03 — minimum web-auth boundary (GAP-2).** Reverse-proxy boundary
   (zero Go change) **and** a thin in-binary signed session cookie that
   activates the dormant `capability_grants` scaffolding.
4. **WEB-04 — read-only runtime health shell.** Theme/density applied before
   boot (reuses Phase 23 D-07/D-08 pre-paint script), composing the existing
   `/healthz` + `/readyz` + status.

**Out of bounds (later phases):** chat lane + approval center (Phase 25),
typed-display protocol (Phase 26), graph explorer (Phase 27), governance
boards + web onboarding (Phase 28), governance *writes* + `ui_control` shell
(follow-up milestone). Auth/RBAC/multi-tenant/OAuth remain **out of scope** for
the whole milestone — this phase ships the *minimum* boundary only
(PROJECT.md §Out of Scope).

</domain>

<decisions>
## Implementation Decisions

### Sign-in experience (WEB-03)
- **D-01 — In-binary login = env operator-secret + login form.** A passphrase
  in `AURA_WEB_AUTH_SECRET` is validated **constant-time** (`crypto/subtle`,
  fail-closed) by a `POST /login`-style endpoint that issues the signed session
  cookie. No new credential storage; fits the single-operator DGX-Spark bundle.
  Rejected: one-time bootstrap token (rotation/recovery story) and reusing the
  Phase-9a setup-wizard token (couples web-auth to the wizard's separate port).
- **D-02 — Session cookie = signed, `HttpOnly + Secure + SameSite=Strict`,
  bound to the operator `identity` row.** (Cookie attributes are locked by
  ROADMAP SC3; the bound identity is the `capability_grants` seam, CORE-03.)
  The login is **really built and wired this phase**, not a skeleton.

### Auth gate scope (WEB-03)
- **D-03 — Whole origin private (in-binary cookie path).** When `aura serve` is
  exposed non-loopback via the in-binary cookie, **every route requires the
  session** EXCEPT: the login page + its static assets, and `/healthz` (a
  liveness probe must stay reachable for proxies/orchestrators). This
  intentionally goes *beyond* the research §4 read/write split — the operator
  chose a fully-private cockpit over leaving the shell/health world-readable.
- **D-04 — `capability_grants` check is for the mutating routes that exist.**
  The cockpit's only mutating route today is `POST /agent/run`; governance
  write routes land in Phase 28. The whole-origin gate (D-03) provides the
  principal; per the research four-layer model
  (proxy → principal → `capability_grants` → risk/approval gate), the
  `capability_grants` authorization layer attaches to mutating governance
  routes as they arrive. This phase wires the principal + the seam; it does NOT
  invent governance write routes early.

### Non-loopback exposure + boot guard (WEB-02)
- **D-05 — Boot guard unlocks on EITHER credential.** A non-loopback bind boots
  **iff** `AURA_WEB_AUTH_SECRET` is set (in-binary cookie active) **OR**
  `AURA_WEB_TRUST_PROXY=true` is asserted (operator vouches a reverse proxy
  terminates auth; Go stays hands-off). **Neither set + non-loopback bind =
  fail-fast exit** with a clear, actionable message. Loopback bind stays
  bootable with no config, exactly as today. (Research §4: both the
  reverse-proxy and in-binary paths are valid.)
- **D-06 — Express the bind by WIDENING `AURA_AGUI_BIND`.** One server, one
  bind var: lift the hardcoded-loopback restriction and let D-05's guard govern
  non-loopback. No new bind env, no alias. Minimal-industrial-shape
  ([[feedback_no_atomic_bombs_minimal_industrial_shape]]). The var name is
  historically `AGUI` but it IS the cockpit server now (the embed mounts
  additively on it — `serve_webui.go`).

### Runtime health shell (WEB-04)
- **D-07 — Minimal panel = compose the existing endpoints.** The read-only
  health shell aggregates the EXISTING `GET /healthz` (PG liveness +
  `scheduler_last_tick`) + `GET /readyz` (postgres + neo4j readiness probes) +
  the bind address + build version. **No new backend endpoint this phase.** The
  richer `GET /api/health/runtime` aggregator (scheduler tick, MCP
  mounted/failed, cache hit rate, tool ledger) stays in its later REST-read
  phase (research §5 / build-order Phase C). Matches SC4 literally
  ("aggregating `/healthz` + `/readyz` + status").
- **D-08 — Theme/density before boot reuses Phase 23.** No flash: the
  pre-hydration inline `<head>` script (Phase 23 D-08) already sets
  `data-theme`/`data-density` before React mounts. WEB-04 consumes it, does not
  rebuild it.

### Claude's Discretion (research/planner-resolved)
Resolve these with golang-security + the locked GAP-2 design; they do not need
operator input:
- **Session cookie TTL + logout** endpoint/behavior (idle vs absolute expiry).
- **CSRF posture** — `SameSite=Strict` already covers the common vector for a
  cookie-auth SPA; decide whether an additional token is warranted given the
  whole-origin gate (D-03) and same-origin SPA. Default: SameSite-only unless a
  concrete cross-origin write path appears.
- **SPA-fallback exclusion list shape** — the existing `aguiRoutePrefixes`
  (`/healthz`, `/readyz`, `/debug/vars`, `/metrics`, `/agent/run`, `/threads/`)
  is the exclusion set; **carve a forward-compat `/api/` prefix** alongside them
  so the later REST reads (Phase C) 404 cleanly rather than serving the SPA,
  even though no `/api/*` routes exist yet.
- **Login page asset placement** in the `internal/webui` embed (keep the embed
  leaf-level — it imports no other `internal/*`, an invariant
  `scripts/agui_boundary_check.sh` enforces).
- **`crypto/subtle` constant-time compare** + fail-closed validation details;
  trust-proxy header handling (read SPA cookies, never trust client auth headers
  — golang-security anti-pattern).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents (researcher + planner) MUST read these before planning or
implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` §"Phase 24: Web Foundation — Serve + Auth + Health" —
  goal, the 4 success criteria, WEB-01..04, depends-on Phase 23, UI hint: yes.
- `.planning/REQUIREMENTS.md` WEB-01..04 (lines 41-44); line 128 (multimodal
  `/agent/run` rejection — unchanged here); line 136 (`showReasoning` web policy
  is Phase 25, NOT this phase).
- `.planning/PROJECT.md` §"Current Milestone v1.0.0" + §Out of Scope — the
  single-binary deploy invariant and the **minimum-boundary / no-RBAC-OAuth**
  mandate that bounds GAP-2.

### GAP-2 + serve/embed + observability design (LOCKED shape)
- `.planning/research/ARCHITECTURE.md` §3 (serve/embed: SPA-fallback handler
  excluding API routes), **§4 (Web Auth GAP-2 — the three options, the
  reverse-proxy-first + in-binary-cookie recommendation, the four-layer write
  protection, the `identity`/`capability_grants` seam)**, §5 (observability
  `runtime_status` — what already exists, the deferred `/api/health/runtime`
  aggregator), §7 Phase A (this phase's slice in the build order).
- `.planning/research/STACK.md`, `.planning/research/PITFALLS.md` — stack +
  pitfalls context for the milestone.

### Prior phase context (carried forward — do not re-decide)
- `.planning/phases/23-frontend-infrastructure-industrial-foundation/23-CONTEXT.md`
  — **D-14** (React Router locked as the SPA router, wired HERE), **D-07/D-08**
  (real dark-operator theme + density, applied **before paint** via the
  pre-hydration `<head>` script — WEB-04 reuses), **D-05/D-06** (committed
  `web/dist` + Node-24 Docker rebuild + CI freshness gate — the host this phase
  serves from).

### UI/UX contract
- `docs/design/aura-deep-search-figma/ux-spec.md` — "Runtime is a product
  surface" (line 118), `runtime_status` panel fields (lines ~503-505),
  auth-posture framing. Drives the WEB-04 health-shell content and the (UI-phase)
  login + health-panel visual contract.

### Brand asset
- `public/Logo.png` — consumed by the app shell (already wired in Phase 23).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`cmd/aura/serve.go` + `cmd/aura/serve_webui.go`** — the single-binary host.
  `bootServe` builds ONE `http.Server` on `chat.cfg.AGUIBind`;
  `newServeHandler(aguiServer.Mux())` is the parent mux that keeps the AG-UI
  route prefixes authoritative over the `/` catch-all. WEB-01 upgrades the `/`
  static handler into an SPA-fallback handler (index.html for unknown client
  routes; the `aguiRoutePrefixes` + a new `/api/` carve-out are the exclusions).
- **`internal/webui`** (`embed.go`, `Handler()`, `dist/`) — the Phase-23 leaf
  embed package; stays leaf-level (no `internal/*` imports —
  `scripts/agui_boundary_check.sh` enforces). Login page assets live here.
- **`internal/agui/server.go` + `internal/agui/readiness.go`** — `/healthz`
  (PG ping + `HealthDetails`/`scheduler_last_tick`), `/readyz`
  (`ReadinessProbe` set: postgres + neo4j), `/metrics`, `/debug/vars` already
  ship. WEB-04 composes these; WEB-03 middleware wraps the mux subtree.
- **`internal/identity/store.go`** — `HasCapability` / `GrantCapability` /
  `GetIdentityByID` over `aura.capability_grants` (Phase 4, migration 0004).
  The session principal binds to an `identity` row here; the dormant
  scaffolding finally gets exercised.
- **`internal/config/config.go`** — `AGUIBind` (`AURA_AGUI_BIND`, default
  `127.0.0.1:9080`), `AGUICORSPermissive`. D-05/D-06 add the
  `AURA_WEB_AUTH_SECRET` + `AURA_WEB_TRUST_PROXY` knobs and lift the
  hardcoded-loopback restriction (the boot guard replaces it).

### Established Patterns
- **Single-binary deploy invariant** — one Go binary, embedded frontend, one
  loopback `http.Server`; the embed mounts additively (no new listener,
  T-23-06). This phase must NOT add a second server/port.
- **Loopback-as-compensating-control → guarded non-loopback** — amendment #35
  made loopback the auth-deferred compensating control; this phase replaces the
  hardcoded restriction with the WEB-02 fail-fast guard (D-05).
- **Go 1.22 `ServeMux` precedence** — longer/more-specific patterns win over
  `/`; the SPA fallback + exclusions rely on this (already the design in
  `serve_webui.go`).
- **golang-security middleware discipline** — read SPA cookies, never trust
  client auth headers; constant-time compare; fail-closed.
- **Minimal-industrial-shape** — [[feedback_no_atomic_bombs_minimal_industrial_shape]]
  drove D-06 (widen one var, no alias) and D-07 (compose existing endpoints, no
  new aggregator).

### Integration Points
- `internal/webui.Handler()` ↔ new SPA-fallback handler ↔ `newServeHandler`
  parent mux ↔ AG-UI route prefixes + new `/api/` exclusion.
- New `internal/agui/auth.go` `RequireAuth` middleware ↔ the parent mux subtree
  ↔ `internal/identity` (`capability_grants`) ↔ `AURA_WEB_AUTH_SECRET`/
  `AURA_WEB_TRUST_PROXY` config.
- `bootServe` boot-guard check on `cfg.AGUIBind` (non-loopback + neither
  credential ⇒ fail-fast `os.Exit`).
- WEB-04 panel ↔ existing `/healthz` + `/readyz` (React Query REST reads, not
  SSE).

</code_context>

<specifics>
## Specific Ideas

- **Fully-private cockpit** — the operator explicitly chose whole-origin auth
  (D-03) over the read/write split, accepting that the shell + health are gated
  behind login when exposed non-loopback (only login assets + `/healthz` open).
- **One server, one bind var** — widen `AURA_AGUI_BIND` rather than introduce a
  parallel `AURA_WEB_BIND` (D-06).
- **Real login this phase, not a stub** — the env-secret + form path is built
  and wired now (D-01/D-02), so the cockpit is usable AND protected on a network
  at the end of Phase 24, not just loopback.
- **Don't pull the future forward** — the rich `/api/health/runtime` aggregator
  and governance write routes stay in their later phases (D-04, D-07).
- **Parity-with-Go discipline** carried from Phase 23 — the boundary should be
  as principled as the backend (constant-time, fail-closed, defense-in-depth).

</specifics>

<deferred>
## Deferred Ideas

- **`GET /api/health/runtime` aggregator** (scheduler tick, MCP mounted/failed,
  cache hit rate, tool ledger) → later REST-read phase (research §5 / Phase C).
  WEB-04 ships the minimal compose-existing panel only (D-07).
- **Governance write routes + their `capability_grants` enforcement** (MCP
  install/remove, skill install/delete, task approve) → **Phase 28** (MCPW-/SKW-)
  over the GAP-2 boundary built here.
- **`showReasoning` web policy / CoT exposure** → **Phase 25** (Chat + Approval
  Center), REQUIREMENTS line 136.
- **assistant-ui chat lane + approval center** → **Phase 25**.
- **Typed-display protocol + router** → **Phase 26**; **graph explorer** →
  **Phase 27**; **read-only governance boards + web onboarding** → **Phase 28**.
- **`ui_control` operator-OS shell** + scheduler write surfaces → follow-up
  milestone (PROJECT.md Deferred).
- **Real multi-user auth / RBAC / OAuth** → out of scope for the entire
  milestone; this phase ships the minimum boundary only.

None of these expand Phase 24 scope — they are later phases' work, recorded so
they are not lost.

</deferred>

---

*Phase: 24-web-foundation-serve-auth-health*
*Context gathered: 2026-06-16*
