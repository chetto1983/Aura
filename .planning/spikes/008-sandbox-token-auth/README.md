---
spike: 008
name: sandbox-token-auth
type: standard
validates: "Given --token, when Aura's client sends the bearer, then unauthenticated :2468 calls are 401 and authed calls pass + the healthcheck survives"
verdict: VALIDATED
related: [009, 010]
tags: [sandbox, auth, hardening, phase-8, phase-11]
---

# Spike 008: sandbox-token-auth

## What This Validates

The cheapest sandbox-hardening knob: Aura runs the sandbox-agent with `--no-token`, so anything that can reach `127.0.0.1:2468` can execute arbitrary commands in the sandbox. The `--token` flag exists; this spike proves it closes the hole end-to-end (including the healthcheck, which would otherwise mark the container unhealthy and block Aura's boot gating).

## How to Run

```bash
docker compose -f compose.yaml -f .planning/spikes/008-sandbox-token-auth/compose.token.yaml up -d aura-sandbox-agent
# probe (see Results); restore:
docker compose -f compose.yaml up -d aura-sandbox-agent
```

## Results

**VALIDATED** — bearer auth is enforced on the whole API and the authed healthcheck keeps the container healthy:

| Request | Result |
|---|---|
| `GET /v1/health` no auth | **401** |
| `POST /v1/processes/run` no auth | **401** |
| run + wrong token | **401** |
| run + `Authorization: Bearer spike008-secret` | **200**, `stdout: authed-ok` |
| healthcheck (authed `curl -H` probe) | `healthy` |

Findings:
- **`/v1/health` itself requires the token** — the compose healthcheck MUST send `Authorization: Bearer <token>`, or the container reports unhealthy and `bootChat`/`serve` gating refuses it. Proven in the override.
- Auth is a flat shared bearer (single `--token`), not per-caller — perfect for Aura's single-binary one-client model.

## Plan obligations (small, real)

1. **`internal/sandboxagent.Client` needs a token field** — one new line in `Run()`: `httpReq.Header.Set("Authorization", "Bearer "+c.token)` (today it sets only Content-Type/Accept). Wire `Config.Token` from a new env `AURA_SANDBOX_AGENT_TOKEN`.
2. **compose**: `--no-token` → `--token ${AURA_SANDBOX_AGENT_TOKEN}` + authed healthcheck; generate the token at first boot (mirror `AURA_SETUP_TOKEN` Amendment #10 pattern) and surface it in `.env`.
3. Loopback-only publish stays (defense in depth); token closes the same-host / other-container reach.

This is a Phase-8 hardening item (sandbox-wide), cheaply also unblocking Phase-11 (skills execute model-authored code — the auth gate matters more once skills exist). ~5 LOC + compose/env.
