# Phase 8: Sandbox 2b Session-Bound — Discussion Log

**Date:** 2026-06-03
**Mode:** research-grounded discussion (user directive: "deep research on D:/tmp + industrial 2026 pattern" + "look how your sandbox works and we do same for aura")

> Human-reference record only. Not consumed by downstream agents (researcher/planner/executor) — CONTEXT.md is the canonical output.

## Gray areas presented

User selected ALL FOUR offered gray areas + the freeform research directive:
1. Session state model
2. Container isolation model
3. Network egress enforcement
4. Risk-tier governance scope
+ "deep research on d:/tmp and industrial 2026 pattern"

## Research performed

Four parallel agents:
- **agent-infra-sandbox (AIO)** — `D:/tmp/agent-infra-sandbox`: persistent Jupyter-kernel sessions (Python `x=42` survives) vs fresh bash; single shared container = anti-isolation model; NO egress firewall (TinyProxy convenience only); no quota/symlink guard.
- **OpenAI Codex** — `D:/tmp/codex/codex-rs`: proxy-as-policy-engine (`network-proxy`), deny-wins precedence, glob domains, DNS-rebinding-by-IP-classification, fresh-process-per-command, default read-only/network-off.
- **Industrial 2026 (web)** — E2B/Modal/Daytona/OpenAI CI/Anthropic: two session tiers (kernel + exec); one instance per session via control plane; **decisive finding: in-container iptables needs CAP_NET_ADMIN, incompatible with cap_drop:ALL, wrong layer under gVisor → enforce egress at a host proxy**; idle TTL 5-20min; CVE-2026-39861 symlink escape; Go `os.Root` (no RemoveAll yet).
- **adk-go session model** — `D:/tmp/adk-go-study/session`: composite (appName,userID,sessionID) keying, state scopes, lifecycle. Nanobot=bwrap; picobot/hermes=no sandbox.

## Decisions (each ratified via AskUserQuestion)

| Area | Options offered | User choice |
|---|---|---|
| Session state | Both tiers (persistent interp+files) / Files-only / Lazy opt-in | **Both tiers (persistent interp + files)** → D-01..D-03 |
| Isolation | Per-conversation container (control plane) / Shared sidecar logical | **Per-conversation container** → D-04..D-07 |
| Network egress | Host-side proxy allowlist (amend PRD) / Keep PRD iptables | **Host-side proxy allowlist (amend PRD)** → D-08..D-10 |
| Risk-tier + symlink | Minimal seam + os.Root / Full framework now / Literal PRD | **Full governance framework now** → D-11..D-12; symlink guard taken as os.Root (D-13..D-14, literal-O_NOFOLLOW option not chosen) |

## Empirical sandbox introspection

Probed the live Claude Code runtime: a local install is **unsandboxed** (host user via Git Bash, no container/seccomp/proxy). Reference = cloud/`--sandbox` model (Phase-5 capture + Anthropic published runtime): strong boundary + host-resolving proxy allowlist + ephemeral session + symlink-escape CVE. **Validated all four decisions as-chosen.** Aura's deliberate divergence: boundary always-on (vs Claude Code's permissive-on-host local mode).

## Required PRD amendments flagged (before planning)

1. Session sidecar gains per-session persistent interpreter (both tiers).
2. SessionManager control-plane carve-out from D-08 (Go drives container *lifecycle*; execution stays HTTP).
3. Network egress: iptables OUTPUT rules → host-side proxy allowlist.
4. Symlink guard: `O_NOFOLLOW` → `os.Root`/openat2.
5. `internal/scoring/` shared module pulled forward from Slice 6 to Phase 8.

## Deferred ideas

Memory-snapshot resume; Firecracker per-session; per-identity sessions (v2); MITM/method-level egress; scheduler/skills application pipelines (P10/P11); swarm parallel-sandbox (P9).

## Claude's discretion

Interpreter mechanism (REPL vs IPython kernel), port/naming scheme, proxy shape (CONNECT handler vs SOCKS5), DNS cache TTL, CLI output formatting.
