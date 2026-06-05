# Sandbox Runtime (mounts, deps, hardening posture)

## Requirements

- Current posture = **amendment #50 / D-15c full-terminal home**: rw `/skills` bind,
  `--no-token`, full egress on dev. Single trusted operator on their own box; the
  future per-identity gate is **capability_grants (Slice 1.7)**, not ceremonies.
- Snippet/skill execution always by interpreter + path (`python3 /skills/...`) —
  Docker Desktop masks perms 777; native Linux won't.
- Dep strategy is a planner CHOICE (bake / on-demand uv / hybrid) — `deps:`
  frontmatter becomes LOAD-BEARING if on-demand is chosen.
- Prod-parity egress decision is MANDATORY before native-Linux ship — Docker
  Desktop's accidental NAT is a dev-only artifact, never a design input.

## How to Build It

**Mounts (005, posture updated by #50):** compose bind
`${AURA_SKILL_EXPORT_DIR:-~/.aura/skills/export}:/skills` (now rw). Docker Desktop
file-sharing (gRPC-FUSE) handles it — the Phase-8 `docker volume create --opt device=`
0x100e failure class does NOT apply to compose bind mounts. Live visibility is
immediate (file edits AND new directory trees) — do not design a sync/refresh step.

**Deps (006 + 007):** the image bakes python3 + pip + uv (uv COPYed from
`ghcr.io/astral-sh/uv` — static binary). Measured: `uv venv` 45-53ms; openpyxl
install 292ms; pandas+numpy 3.1s; uv's global cache makes repeats ~instant. Three
viable models — bake-only / on-demand-uv / hybrid (bake the heavy Phase-5 set, uv the
long tail). PEP 668: Debian's python is externally-managed → image-level installs
need `--break-system-packages` (the sandbox IS the isolation boundary, no venv needed
at image level). The xlsx skill's full script dep set: openpyxl, defusedxml, lxml,
validators.

**Hardening tiers (008/009/010 — built and shelved by #50 for dev; the PROD menu):**
- Bearer auth (008): `--token` enforces 401 on the whole API including `/v1/health`
  — the compose healthcheck must send the bearer or the container reports unhealthy
  and boot gating refuses it. Client wiring = one header line + `AURA_SANDBOX_AGENT_TOKEN`.
  Superseded on dev by #50's `--no-token`.
- Egress allowlist (009, PARTIAL): the ~80-LOC Go CONNECT proxy
  (`sources/009-sandbox-egress-allowlist/proxy/`) works (ALLOW pypi / DENY github),
  but `HTTPS_PROXY` env is ADVISORY — on Docker Desktop, vpnkit NATs the bridge
  regardless of `enable_ip_masquerade: "false"`, so hostile code egresses directly.
  Enforcement requires the proxy to be the ONLY route: native-Linux dockerd +
  genuinely non-masquerading bridge. npm/github/skills.sh/pypi go on the allowlist
  when the skills CLI lives in-sandbox.
- gVisor (010, PARTIAL): `runsc` cannot run on Docker Desktop (no runtime slot). The
  python/uv workload SURVIVES gVisor (proven via `runsc do`); it's a native-Linux/CI/
  prod tier — `compose.gvisor.yaml` with `runtime: runsc` applied there only.

## What to Avoid

- **Never infer egress posture from the dev stack** — spike 006 concluded "egressless
  → must bake deps" from an unprobed assumption; spike 007 disproved it (full egress
  via vpnkit NAT). Probe, don't inherit.
- **Don't rely on the exec bit** anywhere in skill content.
- **Symlinks in skill content resolve in-container** (005): an absolute symlink to a
  container path (`/etc/passwd`) READS. Host-side walks must Lstat-no-follow;
  materialization should strip/reject symlinks. (The in-container risk is ~none — the
  model can already read any container path via exec.)
- Don't put secrets in the container env beyond what the run needs — full-terminal
  home means the model reads everything the container can.

## Constraints

- Docker Desktop: perms masked 777; vpnkit NAT (advisory-only egress); no runsc; the
  `device=` volume-driver path fails (0x100e) but compose binds work.
- Healthcheck + boot gating couple: any auth/option change must keep
  `/v1/health` green or `bootChat`/serve refuses the sandbox.
- Compose-override-per-spike pattern for service mutations
  (`docker compose -f compose.yaml -f <override> up -d <svc>`; restore = without the
  override) — production compose stays untouched.

## Origin

Synthesized from spikes: 005, 006, 007, 008, 009, 010
Source files: sources/005-skills-ro-mount/ (incl. compose override),
sources/006-xlsx-skill-dry-run/ (dep-baked Dockerfile), sources/007-uv-on-demand-deps/,
sources/008-sandbox-token-auth/ (token override + authed healthcheck),
sources/009-sandbox-egress-allowlist/ (CONNECT proxy), sources/010-sandbox-gvisor-runsc/
