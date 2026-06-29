# Phase 24: Web Foundation — Serve + Auth + Health - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-16
**Phase:** 24-web-foundation-serve-auth-health
**Areas discussed:** Sign-in experience, Auth gate scope, Non-loopback exposure + boot guard, Health panel depth

---

## Sign-in experience (WEB-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Env operator-secret + form | `AURA_WEB_AUTH_SECRET` passphrase, constant-time validation, signed cookie bound to single operator identity. Simplest, fits DGX-Spark single-operator bundle. | ✓ |
| One-time bootstrap token | Random token printed to stdout/log at first boot (code-server/Jupyter style); no stored password. | |
| Reuse setup-wizard token | Extend the Phase-9a setup wizard (:9081, `/start <token>`) to mint the web session. | |

**User's choice:** Env operator-secret + form
**Notes:** Implies a real login is built and wired this phase, not a stub. Cookie attributes (`HttpOnly + Secure + SameSite=Strict`, identity-bound) are locked by ROADMAP SC3.

---

## Auth gate scope (WEB-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Whole origin private | Every route requires the session except login page + assets + `/healthz`. Cockpit fully private. | ✓ |
| Read/write split (per research) | GET reads stay open; only mutations require the session + capability_grants. Matches GAP-2 §4 but leaves shell/health world-readable without a proxy. | |
| Mutations + capability_grants only | Read/write split plus an immediate capability_grants check on every mutation. | |

**User's choice:** Whole origin private
**Notes:** Operator chose a fully-private cockpit over the documented read/write split — deliberately goes beyond research §4. capability_grants authorization layer still attaches to mutating governance routes as they arrive (Phase 28); this phase wires principal + seam, does not invent write routes early.

---

## Non-loopback exposure + boot guard (WEB-02)

### Sub-decision A — what satisfies the boot guard

| Option | Description | Selected |
|--------|-------------|----------|
| Either secret OR trust-proxy | Non-loopback allowed if `AURA_WEB_AUTH_SECRET` set OR `AURA_WEB_TRUST_PROXY=true`; neither + non-loopback = fail-fast. Matches research §4 (both paths valid). | ✓ |
| Only in-binary secret | Non-loopback requires `AURA_WEB_AUTH_SECRET`; no trust-proxy escape hatch (proxy + in-binary auth both run). | |
| Only trust-proxy flag | Non-loopback requires `AURA_WEB_TRUST_PROXY=true`; always front with a proxy. | |

**User's choice:** Either secret OR trust-proxy

### Sub-decision B — how the bind is expressed

| Option | Description | Selected |
|--------|-------------|----------|
| Widen `AURA_AGUI_BIND` | Keep the one existing bind var; lift the hardcoded-loopback restriction (the guard governs non-loopback). One server, one var. | ✓ |
| New `AURA_WEB_BIND` (alias) | Canonical cockpit bind var + `AURA_AGUI_BIND` as a back-compat alias (precedence rule needed). | |

**User's choice:** Widen `AURA_AGUI_BIND`
**Notes:** Minimal-industrial-shape — one server (the embed mounts additively on the AG-UI server), one bind var. Loopback stays bootable with no config, as today.

---

## Health panel depth (WEB-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Minimal — compose existing | Aggregate existing `/healthz` + `/readyz` + bind + version. No new backend endpoint. Matches SC4 literally. | ✓ |
| Pull forward thin aggregator | Build `GET /api/health/runtime` now (daemon state, scheduler tick, MCP counts, cache hit rate). | |

**User's choice:** Minimal — compose existing
**Notes:** The richer `/api/health/runtime` aggregator stays in its later REST-read phase (research §5 / build-order Phase C). Theme/density-before-boot reuses the Phase-23 pre-paint script (D-08), not rebuilt.

---

## Claude's Discretion

- Session cookie TTL + logout endpoint/behavior (idle vs absolute expiry).
- CSRF posture — `SameSite=Strict` covers the common vector; decide whether an additional token is warranted (default SameSite-only for the same-origin SPA).
- SPA-fallback exclusion list shape — reuse `aguiRoutePrefixes` + carve a forward-compat `/api/` prefix so later REST reads 404 cleanly.
- Login page asset placement in the `internal/webui` leaf embed.
- `crypto/subtle` constant-time compare details; never trust client auth headers (read SPA cookies only).

## Deferred Ideas

- `GET /api/health/runtime` aggregator → later REST-read phase (Phase C).
- Governance write routes + capability_grants enforcement → Phase 28 (MCPW-/SKW-).
- `showReasoning` web policy / CoT exposure → Phase 25.
- assistant-ui chat lane + approval center → Phase 25.
- Typed-display protocol → Phase 26; graph explorer → Phase 27; governance boards + web onboarding → Phase 28.
- `ui_control` operator-OS shell + scheduler write surfaces → follow-up milestone.
- Real multi-user auth / RBAC / OAuth → out of scope for the whole milestone.
