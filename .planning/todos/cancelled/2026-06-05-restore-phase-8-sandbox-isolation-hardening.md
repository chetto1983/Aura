---
created: 2026-06-05T10:29:17.159Z
title: Restore Phase-8 sandbox isolation hardening lost in sandbox-agent pivot
area: general
files:
  - compose.yaml:90-118
  - docker/sandbox-agent/Dockerfile
  - internal/sandboxagent/client.go
  - .planning/spikes/008-sandbox-token-auth/README.md
  - .planning/spikes/009-sandbox-egress-allowlist/README.md
  - .planning/spikes/010-sandbox-gvisor-runsc/README.md
---

> **❌ CANCELLED — superseded by amendment #50 / D-15c (full-terminal home, 2026-06-05).**
> This todo wanted to RESTORE the sandbox token auth + egress allowlist + gVisor/seccomp
> hardening. The owner decision went the opposite way: Aura gets a full terminal with no
> security toys (prd.md §Slice 2 amendment #50; docs/aura-toolset-design-claude-code-parity-2026-06-05.md).
> The one real future gate is capability_grants (Slice 1.7), dormant for single-user. Kept for history.

## Problem

The 2026-06-03 sandbox-agent pivot (amendment #44 / D-15b — replaced the bespoke
sandbox with `rivetdev/sandbox-agent`) silently DROPPED the Phase-8 isolation
hardening that the original CAP-02 sandbox had specced. The rivet agent is a
production-grade exec adapter but provides **no isolation of its own** — Rivet's
own docs say "sandboxes provide the isolation" (the surrounding container does).
Today `aura-sandbox-agent` runs with only Docker defaults (namespaces + cgroups
+ default seccomp) and `--no-token`, i.e. **unauthenticated arbitrary code exec
on loopback** with the host kernel exposed. This becomes load-bearing the moment
Phase 11 (Skills) lets the model author + run its own code.

Spike-validated gaps (session 2, 2026-06-05 — see the three spike READMEs):

1. **No auth** — `:2468` exec API is unauthenticated. Spike 008 proved `--token`
   enforces a bearer (401 unauth incl. `/v1/health` → healthcheck must carry it;
   200 + exec on correct token). `internal/sandboxagent.Client` sends no
   `Authorization` header today.
2. **No egress control** — the `aura-sandbox-local` bridge sets
   `enable_ip_masquerade:false` but Docker Desktop's vpnkit NATs it anyway, so
   the container has unrestricted internet egress. The Phase-8 D-08 host
   forward-proxy + pypi-allowlist (resolve-then-pin) was specced then lost.
   Spike 009: an ~80-LOC CONNECT-allowlist proxy works, but proxy-env is
   ADVISORY on Docker Desktop — enforceable only on a native-Linux
   non-masquerading bridge where the proxy is the container's only route.
3. **No gVisor / weak seccomp** — Phase 8 specced gVisor-primary-x86
   (amendment #36). Spike 010: the python/uv workload survives gVisor
   (`runsc do` clean), but Docker Desktop CANNOT host the `runsc` runtime
   ("unknown runtime") — it is a native-Linux/CI/prod-only tier. The
   `compose.gvisor.yaml` overlay was lost in the pivot; only the Dockerfile
   survived. seccomp was never re-tightened for the new image.

This is broader than skills (it is the whole sandbox's security posture), which
is why it is captured as a standalone todo rather than folded into Phase 11.
Phase-11 CONTEXT D-37/D-38 DEPEND on the portable hardening floor existing but
explicitly scope the gVisor overlay + seccomp re-tightening OUT to here.

## Solution

Track as a Phase-8 sandbox-hardening regression (likely a small dedicated
phase or a Phase-8 follow-up plan). Concretely:

- **Token auth (cheap, do first, ~5 LOC):** add `Config.Token` +
  `httpReq.Header.Set("Authorization", "Bearer "+token)` in
  `internal/sandboxagent/client.go`; compose `--no-token` →
  `--token ${AURA_SANDBOX_AGENT_TOKEN}` + authed healthcheck; generate the
  token at first boot (mirror the `AURA_SETUP_TOKEN` Amendment #10 pattern).
- **Portable hardening floor (runs on Docker Desktop AND prod):** tightened
  seccomp profile + read-only rootfs (+ `no-new-privileges`, already set) +
  the egress allowlist. This is the floor Phase 11's 7e executor relies on.
- **Egress boundary (explicit decision):** restore the Phase-8 D-08 host
  forward-proxy with a hostname-CONNECT allowlist (spike 009 `proxy/main.go`
  is a working reference) OR re-enable masquerade with nftables egress rules;
  `needs_network:true` snippets route through it at the RISKY tier;
  `needs_network:false` → no route = truly offline (enforced on native Linux).
- **gVisor prod/CI tier:** restore a `compose.gvisor.yaml` overlay adding
  `runtime: runsc` to `aura-sandbox-agent`, applied on native-Linux/CI only;
  CI gates it via DinD+runsc (the Phase-8 `sandbox.yml` pattern), dev asserts
  the portable floor. No-skip-as-green: the gVisor leg `t.Fatal`s under `$CI`
  if `runsc` is absent.

Reference verdict (research, 2026-06-05): keep rivet sandbox-agent (it is the
validated harness/sandbox split, Apache-2.0, 96% covered, already integrated),
do NOT adopt daytona/E2B/Modal (same runc isolation class + heavy control plane
= the "no atomic bombs" anti-pattern). The work is "A + C": keep the adapter,
harden the box around it.
