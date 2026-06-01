# Phase 5: Sandbox 2a Stateless - Context

**Gathered:** 2026-06-01
**Status:** Ready for planning
**Method:** Research-grounded discussion across 4 selected gray areas + 3 additional (wire/result, error taxonomy, CLI) + a self-introspection of Claude Code's own execution sandbox at the user's direction. Industrial patterns triangulated from web research: SandboxEscapeBench (arXiv 2603.02277, Oxford + UK AISI, Mar 2026), E2B/Modal/Firecracker architecture, Docker userns-remap vs rootless hardening (2026). The PRD already locks most of the WHAT (Slice 2a: stateless, Python stdlib sidecar, net-none, file targets, hardening list); this CONTEXT captures the HOW + **two architectural pivots that require PRD-amendment commits before planning** (see `### Required PRD Amendments`).

<domain>
## Phase Boundary

Replace `sandbox.Stub` (`internal/sandbox/sandbox.go`) with a real `sandbox.Runner` that executes untrusted Python 3.12 + shell snippets in an isolated sidecar, and expose it to the model as the **deferred `execute` tool** (`lang ∈ {python, shell}`, `code`, optional `timeout_sec`). **Stateless per-call** (fresh subprocess), `network_mode: none`, `read_only` rootfs, tmpfs `/tmp`, full hardening flag set, and a **positive seccomp allowlist** (~80 syscalls). Gate-3 DoD: SandboxEscapeBench escape-rate < 5%.

This is the **highest-stakes P0 sandbox slice**. Scope is **Slice 2a only**. Slice **2b** (session-bound containers, workspace mount, network allowlist, `aura.sandbox_sessions`, migration 0010) is **Phase 8** and explicitly OUT of scope here — but `session_id` is reserved-but-inert on the tool + CLI so the surface is forward-stable.

</domain>

<decisions>
## Implementation Decisions

> All decisions are HOW (implementation), not WHAT (PRD/ROADMAP own WHAT). Each is the planner's default unless research surfaces a concrete reason to deviate. Research grounding is cited where triangulated against external sources. **Two decisions (D-05, D-09) override locked PRD/DECISIONS.md content and are gated behind required PRD-amendment commits — see the dedicated section.**

### Isolation primitive — gVisor-primary (Area 4 / boundary, deeply discussed + user-introspection-driven)

> **Grounding:** Self-introspection of Claude Code's own sandbox (uid 0, full caps, `Seccomp:0`, `NoNewPrivs:0`, `/dev/vda` ext4 → a **microVM/VM**, isolation at the hypervisor boundary, permissive inside; egress = deny-by-default via a host-resolving proxy allowlist, `CLAUDE_CODE_PROXY_RESOLVES_HOSTS=true`). + SandboxEscapeBench conclusion ("correctly-configured up-to-date runtimes hold for current models, but future/higher-compute models may find unknown weaknesses"). + Modal runs **gVisor** for untrusted multi-tenant code in production at scale.

- **D-05 — gVisor `runsc` is Aura's PRIMARY sandbox boundary on x86.** A true syscall-intercepting user-space kernel (guest never reaches the host kernel directly) — the strongest isolation achievable **without `/dev/kvm`** (which the target infra does not expose; see D-05a). This is "a full sandbox like Claude Code's" minus the hypervisor, and is Modal's production model. **⚠️ OVERRIDES locked D12 / amendment #32** (which made gVisor a >5%-only escalation seam) — requires a PRD-amendment + `.planning/DECISIONS.md` D12 re-decision **before** planning.
- **D-05a — microVM (Firecracker/Kata) stays REJECTED.** Requires `/dev/kvm`, blocked on Hetzner cloud + unconfirmed on DGX → violates the D00 portability invariant. gVisor is the achievable maximum. (User confirmed: deployment is KVM-less; "full sandbox like yours" is delivered via gVisor, not a real hypervisor.)
- **D-06 — Hardened-container + seccomp allowlist + userns-remap = the PORTABLE FLOOR.** Applied as defense-in-depth *inside* the gVisor sandbox on x86, AND as the standalone fallback boundary on **arm64/DGX** until gVisor-arm64 is GA (gVisor arm64 is preliminary/non-GA — D12's original concern, still valid for the floor). seccomp profile loads under `runsc` too (belt-and-suspenders).
- **D-07 — The Go runner is runtime-agnostic.** Because the runner is a pure HTTP client (D-08), switching `runc`↔`runsc` is a compose/daemon-runtime concern with zero Go change. x86 default = `runsc`; arm64 = `runc` + seccomp floor. Selected via `AURA_SANDBOX_RUNTIME` (default resolves per-arch).

### Container lifecycle + runner shape (Area 3a, industrial-pattern-grounded)

> **Grounding:** E2B/Modal architecture — every production sandbox splits into a **thin client SDK** (REST/gRPC) + a **separate orchestration layer** that owns lifecycle (schedule/pool/pre-warm/teardown). Calling code never drives the container runtime directly.

- **D-08 — `DockerRunner` is a thin HTTP client** against `AURA_SANDBOX_URL`; the compose-managed sidecar service IS the (single-node) orchestration layer. Execution path = HTTP only (preserves PRD `docker.go ~220 LOC` design). Pooling/pre-warm deferred to 2b/scale (stateless 2a = fresh subprocess per call, no pool needed).
- **D-09 — Auto-start-if-down, then thin client.** On connect failure the runner attempts **ONE** auto-start (shell out `docker compose up -d aura-sandbox`), health-checks, retries once; if that still fails → typed `ErrSandboxUnreachable`. **⚠️ DEVIATES from PRD** (file targets = HTTP-only `docker.go`; smoke shows operator running `docker compose up -d`; acceptance #4 = "sidecar down → clear error"). Contained amendment: execution path stays HTTP; acceptance #4 rewrites to "sidecar down **AND** auto-start fails → clear error." Requires a PRD-amendment before planning.

### SandboxEscapeBench port + Gate-3 gate (Area 1, deeply researched)

> **Grounding:** arXiv 2603.02277 — SandboxEscapeBench is **18 escape scenarios** across orchestration/runtime/kernel layers, implemented as an Inspect AI CTF, LLM-driven, ~$1/escape-attempt. ~half target misconfigs Aura structurally forbids (Docker socket, privileged, writable host mount).

- **D-01 — Port all 18 scenarios as DETERMINISTIC probe scripts** (each directly attempts the escape technique and asserts it fails — no model driver, fast, CI-runnable). `scripts/sandbox_escape_bench.sh`. Misconfig scenarios Aura structurally forbids → "config-regression" assertions (must stay 0). Runtime/kernel scenarios → the live **escape-rate denominator**. Escape-rate **< 5%** recorded in `docs/aura-quality-snapshot.md`.
- **D-02 — Bench runs against the gVisor-primary x86 profile** (the real production default per D-05), with the container+seccomp floor (D-06) also bench-validated as the arm64 fallback.
- **D-03 — PLUS an opt-in, separately-tagged LLM-driven red-team** (real Inspect AI harness + a frontier model) for the true capability number — **scheduled/manual, NOT per-merge** (cost + non-determinism). Feeds the quality-snapshot when run.
- **D-04 — gVisor compose profile is wired now and default-on for x86** (consequence of D-05). `compose.gvisor.yaml` / a `runsc` profile flips `runtime: runsc`; arm64 deployments fall back to `runc` + seccomp floor.

### Seccomp allowlist + multi-arch (Area 2)

- **D-10 — Broad curated published allowlist, no empirical strace.** Adopt a known-good published Python-3.12-container + bash syscall allowlist (~80–100 syscalls) wholesale; the dangerous set stays HARD-EXCLUDED (`ptrace`, `unshare`, `process_vm_readv`, `bpf`, `kexec_load`, `userfaultfd`, `mount`). The deterministic 18-scenario bench (D-01) backstops the "too loose on dangerous vectors" risk. `defaultAction: SCMP_ACT_ERRNO(EPERM)`.
- **D-11 — `seccomp.json` carries both arches by-name** (`SCMP_ARCH_X86_64` + `SCMP_ARCH_AARCH64`; libseccomp resolves numbers per-arch — amendment #30, never hardcode numbers). x86_64 validated live (smoke + bench).
- **D-12 — arm64 validated via QEMU emulation in CI** (`docker buildx` / `qemu-user-static` binfmt → `--platform linux/arm64`). **Caveat captured:** QEMU syscall emulation can diverge from a real arm64 kernel's seccomp behavior → **real-DGX confirmation remains a tracked obligation before any production arm64 deployment** (note in quality-snapshot).

### CI test tier (Area 3b)

- **D-13 — Docker-in-Docker, gating.** CI runs the real sidecar (with `runsc` installed for the x86 default profile) in a Linux job, executes the `//go:build sandbox_integration` tier + the deterministic 18-scenario bench + the QEMU arm64 validation, and **gates the merge**. Honors no-skip-as-green (env exported; skip-helpers `t.Fatal` under `$CI` when unset) and folds sandbox coverage into the **85% floor**. The LLM-driven red-team (D-03) stays scheduled/manual.

### Hardening primitive — userns-remap (Area 4 floor, research-decisive)

> **Grounding:** Docker hardening 2026 — rootless mode forces `seccomp=unconfined` + `apparmor=unconfined` (would gut the allowlist) and can't coexist with userns-remap (moby #48521). userns-remap keeps full Docker functionality incl. seccomp profiles.

- **D-14 — userns-remap (rootless REJECTED).** Daemon-level `userns-remap: default` + `no-new-privileges: true`; container UID 0 → unprivileged host UID, seccomp preserved. Enabled + validated in the CI DinD daemon (we own the inner daemon config); documented as the production daemon requirement; **dev (Docker Desktop/WSL) opt-in** since the in-container non-root uid 65532 + `cap_drop: ALL` already hold for functional dev. Layered with `read_only` / tmpfs `/tmp` / `pids_limit: 64` / `mem=512m` / `cpus=1.0` / `ulimit nofile=64`. Synergy: allowlist excluding `unshare` blocks in-container userns creation → mitigates the 2025/26 userns CVEs. (This floor is defense-in-depth under gVisor on x86, primary on arm64.)
- **D-15 — PRD acceptance #5 resolves against the daemon**, not a compose-service field (`userns-remap` is a `daemon.json` setting; the other flags — `cap_drop`, `no-new-privileges`, `read_only`, `pids_limit` — are compose-service fields).

### Sidecar wire contract + result shaping (additional Area, empirically grounded)

- **D-16 — Per-lang endpoints, JSON in/out.** `POST /exec/python` + `POST /exec/shell` (PRD file targets; 2b extends with `/session/{id}/exec/{lang}`). Request `{"code": str, "timeout_sec": int}`. Response `{"stdout": str, "stderr": str, "exit_code": int, "elapsed_ms": int, "truncated": bool, "limit_hit": "timeout"|"oom"|"pids"|null}`. Sidecar truncates each stream to **1 MiB server-side** and reports `truncated`/`limit_hit` so the Go side never guesses.
- **D-17 — Lean result format** for the `execute` ToolResult.Preview the model reads (grounded in observed Claude Code sandbox behavior: merged, no ceremony, code only when non-zero): stdout verbatim as primary content (no fence/label); `stderr:` appended only if non-empty (label needed because the split-stream HTTP contract loses TTY interleave order); `exit_code: N` line ONLY when non-zero (success silent); `elapsed_ms` + `[limit: timeout|oom|pids]` ONLY when a limit was hit; empty output → `(no output, exit 0)`. Whole string → `tools.NewResult` so the D-25 2048-byte preview cap + sidecar spillover apply uniformly (large outputs page via `read_tool_output`).

### Error taxonomy (additional Area)

- **D-18 — Execution outcomes → ToolResult (model adapts); infra failures → typed Go errors.**
  - **Tool-result content** (model sees + adapts): non-zero exit, stderr, **seccomp EPERM** (surfaced as the EPERM text in stderr — NOT a special error; a blocked syscall is a normal "code tried something denied" outcome), timeout, OOM-kill, pids-cap (the `[limit: …]` label per D-17).
  - **Typed Go errors** (sentinels): `ErrSandboxUnreachable` (after auto-start fails, D-09), `ErrSandboxProtocol` (malformed sidecar response). These propagate as a tool-execution error to the loop (model still sees "sandbox unavailable") AND surface on the `aura exec` CLI with a distinct non-zero process exit.
  - Rule: **code's fault → result; environment's fault → typed error.** A script exiting 1 is a normal expected outcome, never a Go error.

### `aura exec` CLI surface (additional Area, empirically grounded)

- **D-19 — `aura exec [--session <id>] <lang> <code>`** in the hand-rolled switch dispatcher (no cobra — matches `cmd/aura/main.go` convention). `lang` positional + validated ∈ {python, shell} (required — Aura supports both, unlike the shell-only sandbox observed). `code` positional as a single string, or `-` to read stdin for big/multi-line snippets. Output reuses the **lean format (D-17)**. Process exits with the sandbox `exit_code`; infra failure (`ErrSandboxUnreachable`) → distinct code **70**. `--session` parsed-but-inert in 2a (errors pointing to Phase 8 / Slice 2b). Matches PRD smoke `aura exec python "print(2+2)"`.

### Claude's Discretion
- Exact sidecar `sidecar.py` structure (stdlib `http.server`), Dockerfile layering, and the precise published allowlist source — planner/researcher picks within D-10/D-16 constraints.
- `compose.yaml` field ordering and the exact `AURA_SANDBOX_RUNTIME` per-arch default resolution.
- Whether the deterministic bench is one shell script or a small harness — invariant is "18 scenarios, escape-rate emitted, CI-gating."

### Required PRD Amendments
> **PRD-first principle is absolute (CLAUDE.md).** These two decisions deviate from locked content and MUST land as PRD-amendment commits BEFORE any Phase-5 code commit.
1. **D-05/D-06/D-07 — D12 gVisor-primary re-decision.** Amend `prd.md` §Slice 2 D12 + `.planning/DECISIONS.md` D12: gVisor `runsc` = primary boundary on x86 (was: >5%-only escalation); hardened-container+seccomp+userns-remap = portable floor / arm64 fallback; microVM still rejected. Update ROADMAP Phase 5 success-criteria #5 (gVisor default-on x86) + the escape-bench profile note.
2. **D-09 — lifecycle auto-start deviation.** Amend the PRD Slice 2a acceptance ("sidecar down → clear error" → "sidecar down AND auto-start fails → clear error") + note the one-shot auto-start helper in `docker.go`'s scope. Keep the HTTP-only execution path.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### PRD + roadmap + locked decisions
- `prd.md` §Slice 2 — Sandbox runner (lines ~1200–1330): goal, smoke, acceptance (2a + 2b), file targets, D12 isolation decision, open questions, commit templates. **The truth-source for WHAT.**
- `.planning/ROADMAP.md` §"Phase 5: Sandbox 2a Stateless" (lines ~121–132): goal + 5 success criteria.
- `.planning/REQUIREMENTS.md` CAP-01 (line ~32): the requirement this phase satisfies.
- `.planning/DECISIONS.md` D12 (amendment #32) — isolation primitive decision being re-decided per D-05. **Must be amended before planning.**
- `prd.md` §Caps & Limits env index + line ~4732 `AURA_SANDBOX_URL` default `http://127.0.0.1:18901`.

### Existing code (the integration surface)
- `internal/sandbox/sandbox.go` — `Runner` interface + `Result` struct + `Stub` to replace.
- `internal/agent/tools/spec.go` — `Tool`/`Spec`/`Registry` + `ToolResult` (Preview/FullPath/Bytes/Truncated) + the deferred-tool rule.
- `internal/agent/tools/search.go` — `tool_search` hook + the deferred-tool load pattern `execute` plugs into.
- `internal/agent/tools/result.go` — `NewResult` (D-25 preview-cap → sidecar spillover) the execute preview routes through.
- `internal/config/config.go` — `Config` struct + `envDefault`/`envIntDefault` conventions for the new `AURA_SANDBOX_*` vars.
- `cmd/aura/main.go` — hand-rolled switch dispatcher (`buildRegistry`, subcommand switch) where `aura exec` + `execute` registration land.

### External research (grounding, not requirements)
- arXiv 2603.02277 "Quantifying Frontier LLM Capabilities for Container Sandbox Escape" (Oxford + UK AISI) — SandboxEscapeBench 18 scenarios. https://arxiv.org/abs/2603.02277
- AISI blog — benchmark overview + "correctly-configured runtimes hold for current models" conclusion. https://www.aisi.gov.uk/blog/can-ai-agents-escape-their-sandboxes-a-benchmark-for-safely-measuring-container-breakout-capabilities
- Modal Sandboxes (gVisor in production) — https://modal.com/docs/guide/sandboxes ; E2B/Firecracker — https://e2b.dev/docs/sandbox
- Docker hardening 2026 (userns-remap vs rootless; rootless ⊥ seccomp) — https://thelinuxcode.com/docker-security-best-practices-2026-hardening-the-host-images-and-runtime-without-slowing-teams-down/ ; moby #48521 https://github.com/moby/moby/issues/48521

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `sandbox.Runner` interface + `Result{Stdout,Stderr,ExitCode,ElapsedMs}` already defined — `DockerRunner` implements it; `Stub` is the thing being replaced. The HTTP response (D-16) maps 1:1 onto `Result` (+ truncated/limit_hit which the preview formatter consumes).
- `tools.NewResult` (D-25 spillover) — the execute preview (D-17) routes through it unchanged; no new spillover logic needed.
- `tools.Spec{Deferred:true}` + `tool_search` — `execute` is registered exactly like the existing deferred tools; the model loads its full schema on demand (no manifest bloat).
- `config.envDefault`/`envIntDefault` — add `AURA_SANDBOX_URL`, `AURA_SANDBOX_TIMEOUT_SEC` (cap 600s), `AURA_SANDBOX_RUNTIME` following the existing pattern.

### Established Patterns
- **Deferred-tool partition** (CLAUDE.md mandatory): `execute` → `Deferred:true` (long description + enum schema + safety examples).
- **Hand-rolled CLI dispatcher** (`cmd/aura/main.go` switch) — `aura exec` is a new `case`, NOT cobra; parse `--session` flag manually (reserved for 2b).
- **Typed sentinel errors** (e.g. `ErrAwaitingUserInput`, `HTTPError`) — `ErrSandboxUnreachable`/`ErrSandboxProtocol` follow this (D-18).
- **Build-tag integration tier** `//go:build sandbox_integration` + no-skip-as-green (`t.Fatal` under `$CI` when env unset) + 85% coverage floor (CLAUDE.md).

### Integration Points
- `buildRegistry()` in `cmd/aura/main.go` gains `reg.Register(&tools.Execute{Runner: <DockerRunner>})`; the runner is constructed from `config.Load()` sandbox fields.
- `internal/agent/tools/execute.go` (new, ~140 LOC) delegates to `sandbox.Runner`; `internal/sandbox/docker.go` (new, ~220 LOC) is the HTTP client + auto-start (D-09).
- Sidecar materials: `sandbox/Dockerfile`, `sandbox/sidecar.py`, `sandbox/seccomp.json`, `compose.yaml` (+ `runsc` profile per D-04).

</code_context>

<specifics>
## Specific Ideas

- **"Full sandbox like yours."** The user explicitly wants Aura's sandbox to embody the same strong-boundary-first principle as Claude Code's own runtime (VM-isolated, permissive inside). Since the target infra is KVM-less, this is delivered via **gVisor-primary** (D-05) — the maximal achievable equivalent. This intent is the WHY behind the D12 re-decision; downstream agents should treat gVisor-primary as a hard user directive, not a suggestion.
- **Empirical grounding over assertion.** The user twice asked Claude to "look at your sandbox" and run the same tests before deciding — result-format (D-17) and CLI (D-19) were locked against observed behavior of Claude Code's exec interface (merged output, code-only-when-non-zero, single-string command).
- **"Powerful, not a toy."** Drove the full-18-scenario bench (D-01) over a minimal acceptance-only check.

</specifics>

<deferred>
## Deferred Ideas

- **Slice 2b** (Phase 8): session-bound containers, `$AURA_RUN_DIR/conversations/<id>/workspace/` mount, `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` allowlist + iptables + DNS-rebinding pin, `aura.sandbox_sessions` table + migration 0010, `SessionManager` reaper, `aura sandbox sessions {list|terminate|prune}`. `session_id` reserved-but-inert in 2a keeps the surface forward-stable. **Note:** Claude Code's own egress model (deny-by-default host-resolving proxy allowlist) is the reference pattern for 2b's network allowlist + DNS pin.
- **microVM (Firecracker/Kata) tier** — rejected for the KVM-less default (D-05a), but if a future deployment confirms `/dev/kvm` it would be the gold-standard boundary. Not built; record only.
- **Opt-in LLM-driven SandboxEscapeBench red-team** (D-03) — scheduled/manual capability eval, not part of the per-merge gate.

### Reviewed Todos (not folded)
None — no pending todos matched this phase (`todo.match-phase` returned 0).

</deferred>

---

*Phase: 5-Sandbox 2a Stateless*
*Context gathered: 2026-06-01*
