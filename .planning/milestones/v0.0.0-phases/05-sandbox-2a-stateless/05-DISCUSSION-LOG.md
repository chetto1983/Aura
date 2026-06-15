# Phase 5: Sandbox 2a Stateless - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-01
**Phase:** 5-Sandbox 2a Stateless
**Areas discussed:** Escape-bench + gVisor seam, Seccomp allowlist + arm64, Lifecycle + CI tier, Hardening primitive, Wire contract + result shaping, Error taxonomy, CLI surface, Isolation boundary (gVisor-primary pivot)

---

## Gray-area selection (round 1)

User selected ALL four offered: Escape-bench + gVisor seam, Seccomp allowlist + arm64, Lifecycle + CI tier, Hardening primitive.

---

## Escape-bench scope

| Option | Description | Selected |
|--------|-------------|----------|
| Curated subset | ~20-40 probes mapped to our threat model | |
| Full UK-AISI port | complete corpus | |
| Minimal acceptance-only | PRD acceptance probes only, no escape-rate | |

**User's choice:** Free-text — "search online industrial pattern aura must be powerful not a toy."
**Notes:** Triggered web research → SandboxEscapeBench is 18 scenarios (arXiv 2603.02277, Inspect AI CTF). Re-presented with research; user chose **"All 18, deterministic + opt-in LLM (Recommended)"** — port all 18 as deterministic CI-gating probes + a separately-tagged scheduled LLM-driven red-team for the capability number.

## gVisor escalation seam

| Option | Description | Selected |
|--------|-------------|----------|
| Wire opt-in profile now | `runsc` compose profile, default off, x86-only, env-gated | ✓ (later superseded) |
| Document hook only | env stub errors "not wired" | |
| Defer entirely | baseline hardened container only | |

**User's choice:** "Wire opt-in profile now (Recommended)."
**Notes:** Later **superseded** by the isolation-boundary discussion → gVisor elevated to default-on (x86), not opt-in escalation. See "Isolation boundary" below.

## Seccomp allowlist derivation

| Option | Description | Selected |
|--------|-------------|----------|
| Curated reference + empirical verify | published baseline + strace reconciliation | |
| Pure empirical strace | bottom-up from traced workload | |
| Broad curated, no empirical | adopt published allowlist wholesale | ✓ |

**User's choice:** "Broad curated, no empirical."
**Notes:** Dangerous set stays hard-excluded; the deterministic 18-scenario bench backstops the "too loose" risk.

## arm64 multi-arch validation

| Option | Description | Selected |
|--------|-------------|----------|
| Ship both arches, gate arm64 as declared-unvalidated | x86 live, arm64 declared | |
| Add arm64 CI via QEMU emulation | both arches "live" via QEMU | ✓ |
| Block arm64 until DGX | x86-only profile | |

**User's choice:** "Add arm64 CI via QEMU emulation."
**Notes:** Caveat captured — QEMU seccomp behavior can diverge from real arm64; real-DGX confirmation stays a tracked pre-arm64-deployment obligation.

## Container lifecycle ownership

| Option | Description | Selected |
|--------|-------------|----------|
| Operator/compose-managed + thin HTTP client | runner is pure client | |
| Aura auto-manages | Aura owns lifecycle | ✓ (scoped to auto-start) |

**User's choice:** Free-text "search industrial pattern" → research (E2B/Modal client/orchestrator split, recommended thin client) → user nonetheless chose **"Aura auto-manages via Docker API"**, then scoped it to **"Auto-start-if-down, else thin client (Recommended)."**
**Notes:** Flagged as a PRD deviation requiring an amendment. Final scope: execution path stays HTTP-client; one-shot auto-start + health-check + single retry; else `ErrSandboxUnreachable`.

## CI test tier

| Option | Description | Selected |
|--------|-------------|----------|
| CI Docker-in-Docker, gating | real sidecar + seccomp + bench + QEMU arm64, gates merge | ✓ |
| WSL/local-only + documented floor exception | CI compiles, skips execution | |
| Hybrid: deterministic in CI, bench in WSL | split | |

**User's choice:** "CI Docker-in-Docker, gating (Recommended)."
**Notes:** Honors no-skip-as-green + folds sandbox coverage into the 85% floor. LLM red-team stays scheduled.

## Hardening primitive

| Option | Description | Selected |
|--------|-------------|----------|
| userns-remap | daemon-level remap, seccomp preserved | ✓ |
| Rootless Docker | rejected (forces seccomp=unconfined, ⊥ DinD) | |
| In-container non-root only | userns documented but not gated | |

**User's choice:** "userns-remap (Recommended — forced by seccomp+DinD)."
**Notes:** Research-decisive — rootless forces `seccomp=unconfined` and can't coexist with userns-remap (moby #48521).

---

## Gray-area selection (round 2)

User chose "Explore more gray areas" and selected ALL three additional: Wire contract + result shaping, Error taxonomy, CLI surface.

## Sidecar wire contract

| Option | Description | Selected |
|--------|-------------|----------|
| Per-lang path, JSON in/out | `/exec/python` + `/exec/shell` | ✓ |
| Single /exec with lang field | one route | |
| You decide | invariants only | |

**User's choice:** "Per-lang path, JSON in/out (Recommended)."
**Notes:** Response includes truncated + limit_hit so the Go side never guesses; sidecar truncates 1 MiB server-side.

## Result shaping (execute preview)

| Option | Description | Selected |
|--------|-------------|----------|
| Labeled block, stderr only if present | structured | ✓ (as lean synthesis) |
| Raw stdout, metadata appended | | |
| JSON passthrough | | |

**User's choice:** Free-text "look how your sandbox run make same test and we decide together" → Claude ran exec tests in its own sandbox (merged output, no exit-code on success) → re-presented; user chose **"Lean synthesis (Recommended)."**
**Notes:** stdout verbatim; stderr only if present (labeled); exit_code only when non-zero; elapsed+limit only when limit hit; empty → "(no output, exit 0)"; via NewResult.

## Error taxonomy

| Option | Description | Selected |
|--------|-------------|----------|
| Execution outcomes → result; infra → typed error | clean split | ✓ |
| Everything as tool-result content | | |
| Everything as Go errors | | |

**User's choice:** "Execution outcomes → result; infra → typed error (Recommended)."
**Notes:** seccomp EPERM = normal stderr outcome (model adapts), not a special error. `ErrSandboxUnreachable`/`ErrSandboxProtocol` sentinels for infra.

## aura exec CLI surface

| Option | Description | Selected |
|--------|-------------|----------|
| Positional lang+code, --session reserved | | ✓ |
| Flags for everything | | |
| You decide | | |

**User's choice:** Free-text "look your sandbox again" → Claude observed exit-code-on-non-zero + single-string command at the tool boundary → re-presented; user chose **"Positional lang+code, lean output, --session reserved (Recommended)."**
**Notes:** code positional or `-` for stdin; process exits with sandbox exit_code; infra → code 70; `--session` inert in 2a.

---

## Isolation boundary (gVisor-primary pivot)

| Option | Description | Selected |
|--------|-------------|----------|
| Elevate gVisor to default-where-supported (PRD-amendment) | container+seccomp floor, gVisor default on x86 | (precursor) |
| Keep D12 as-is; observation = rationale only | no change | |
| Floor + document microVM as future KVM-tier | gVisor default + microVM future | |

**User's choice:** Free-text "look your sandbox we do same for Aura" (×2) → Claude introspected its own runtime (microVM/VM, uid 0, no seccomp, hypervisor boundary; deny-by-default proxy egress) → user: **"no we must have full sandbox like yours."** Claude surfaced the hard `/dev/kvm` blocker (D12 rejected microVM: Hetzner cloud + DGX). Final clarifying question → user chose **"gVisor as primary now (no KVM needed) (Recommended)."**
**Notes:** Major architectural pivot. gVisor `runsc` = primary boundary on x86 (closest to VM-grade without KVM, Modal's prod model); hardened-container+seccomp+userns-remap = portable floor / arm64 fallback; microVM stays rejected. **Requires PRD-amendment + DECISIONS.md D12 re-decision before planning.** This supersedes the round-1 "gVisor opt-in seam" choice.

---

## Claude's Discretion

- Exact `sidecar.py` structure, Dockerfile layering, the precise published allowlist source.
- `compose.yaml` field ordering + `AURA_SANDBOX_RUNTIME` per-arch default resolution.
- Bench packaging (single script vs small harness) — invariant: 18 scenarios, escape-rate emitted, CI-gating.

## Deferred Ideas

- **Slice 2b** (Phase 8) — session-bound + workspace + network allowlist + `sandbox_sessions` + migration 0010. Claude Code's deny-by-default proxy egress is the reference for 2b's network allowlist + DNS pin.
- **microVM (Firecracker/Kata)** — rejected for KVM-less default; gold-standard if a future deployment confirms `/dev/kvm`. Record only.
- **Opt-in LLM-driven SandboxEscapeBench red-team** — scheduled/manual capability eval, not per-merge.
