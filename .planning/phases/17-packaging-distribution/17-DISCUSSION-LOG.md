# Phase 17: Packaging & Distribution - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-14
**Phase:** 17-packaging-distribution
**Areas discussed:** Box model / security posture (the hinge), spike scope, isolation tier

---

## Box model — reconcile the audit jail with the locked SPEC (the hinge)

The discussion surfaced a verified conflict: the SPEC (2026-06-10) locked "the container is
Aura's computer, not a jail," but an audit commit `ec7fe2f6` (2026-06-12) hardened the `aura`
service into a distroless + `cap_drop: ALL` + `read_only` + non-root jail.

| Option | Description | Selected |
|--------|-------------|----------|
| Build per locked SPEC (not a jail) | Rip the audit hardening; fat full-power image | (became the outcome, via spikes) |
| Keep hardening, PRD-amend SPEC | Keep the distroless jail; amend SPEC backward | |
| Hybrid — keep some controls | Fat image but retain some hardening | |

**User's choice:** Redirected — *"look https://github.com/agent-sandbox/agent-sandbox and then
gsd spike"*, then *"look also docker.com/products/docker-sandboxes — make Aura powerful."*
**Notes:** Rather than pick on paper, the operator directed a spike of real isolation runtimes
(agent-sandbox, Docker Sandboxes, gVisor) to ground the decision.

---

## Spike scope

| Option | Description | Selected |
|--------|-------------|----------|
| Lean: 059 + 061 | Parity probe + decision only | |
| Full: 059 + 060 + 061 | + build the real fat runtime image | ✓ |
| Decision-only: 061 | Synthesis only, no builds | |

**User's choice:** Full (059 + 060 + 061), and added Docker Sandboxes as a 4th input → spike 062.
**Notes:** Directive "make Aura powerful" framed the whole investigation toward a *capable* box.

---

## Isolation tier — lock the box model (post-spike)

| Option | Description | Selected |
|--------|-------------|----------|
| Confirm — write 17-CONTEXT.md | Fat full-power baseline + optional gVisor tier; revert ec7fe2f6; sbx deferred; PRD-amendment flagged | ✓ |
| Confirm, but discuss remaining HOW areas first | Lock box model, then deep-dive image/whatsapp/Caddy/install | |
| Adjust the box model | Change direction before locking | |

**User's choice:** Confirm — write 17-CONTEXT.md.
**Notes:** Spikes 059-062 produced on-host evidence: the distroless image can't run `shell_exec`
(exit 127); the fat box delivers parity + the SPEC "free edge" on plain runc; the fat runtime base
is cheap + cache-stable; Docker Sandboxes is the microVM gold standard but not the appliance runtime
(arm64=macOS-only, coding-agent shape). Decision = fat full-power baseline + optional gVisor
`compose.gvisor.yaml` tier; revert `ec7fe2f6`; a small PRD/SPEC amendment is required before execute.

---

## Claude's Discretion

- whatsapp sibling bridge image, Caddy token-enforcement mechanism, and exact install.sh HW
  thresholds were captured at SPEC-default / planner-choice (operator chose not to deep-dive;
  may revisit at plan-review).

## Deferred Ideas

- Docker Sandboxes (sbx) as an optional x86_64-Linux dev/power-user wrapper.
- docker-in-box without the host socket (could lift SPEC Req 3 in a future milestone).
- Kata/Firecracker microVM tier for a future high-isolation x86_64 appliance SKU.
