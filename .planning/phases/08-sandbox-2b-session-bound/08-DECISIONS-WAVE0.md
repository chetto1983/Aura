# Phase 8 — Wave-0 Decisions (open-question resolutions)

**Resolved:** 2026-06-03
**Status:** Planning-state contract (NOT a PRD amendment). Waves 2–5 cite this doc.
**Source:** `08-RESEARCH.md` §Open Questions (1–5) + §Assumptions Log (A2/A4/A5) + `08-CONTEXT.md` D-08/D-10/D-11.

> These three resolutions close the Wave-0 open questions on paper so no code task stalls
> on an unmade decision. They are HOW-level commitments for the code waves; they do not
> change the truth-source (the five PRD amendments landed in `08-01`, doc-gate). The PRD
> amendments are the contract for the *truth-source*; this doc is the contract for the
> *implementation sequencing*.

---

## OQ2/A4 — `internal/web` SSRF reuse mechanism: **export minimal surface (Option a)**

**Decision.** The host-side egress proxy (`internal/sandbox/network.go`, D-08/D-09) reuses
the Slice-5 SSRF machinery by **exporting a minimal surface from `internal/web`** — NOT by
extracting a new `internal/netguard` package.

**Concretely, export (Wave-2, plan 08-04):**
- `web.ClassifyIP(netip.Addr) (reason string, blocked bool)` — the exported form of the
  currently-unexported `classify` (`internal/web/ssrf.go:35`).
- A **dial-guard / `validateAndPin` constructor** the proxy can call to resolve-then-pin a
  CONNECT target against the same IP-classification + DNS-rebinding pin used by web_fetch
  (the unexported `newGuard`/`guard.validateAndPin` at `ssrf.go:75,85` + `dnsPin` at
  `dnspin.go`). Export a thin constructor + method, not the internals wholesale.

**Why Option (a) over Option (b) extract `internal/netguard`:** scope control. The guard is
small and entangled with the web transport; a full package extraction risks a wider blast
radius than the phase needs (A4 risk: "if the export entangles too much, extract"). Start
minimal; escalate to extraction only if the export drags in unrelated web-transport state.

**Mandates for the code wave:**
1. **Re-test the Slice-5 web tier** after the export (the symbols change visibility; the
   web_fetch SSRF behavior must remain byte-for-byte unchanged — run the existing
   `internal/web` unit + integration tier, incl. `TestDNSRebind`).
2. **FORBID copy-pasting `classify`** into `internal/sandbox` (dupl/audit flags — CLAUDE.md
   `dupl` gate + "REUSABLE CODE / never duplicate"). The proxy calls the exported symbol;
   it does not re-implement IP classification.

---

## OQ4/A5/D-10 — Privacy-mode handling: **fail-fast under `local-only` + non-empty allowlist**

**Ground truth (verified 2026-06-03).** `AURA_PRIVACY_MODE` is **currently unread** anywhere
in the codebase (`grep -rn "PRIVACY_MODE\|PrivacyMode" internal/` returns nothing). It is
referenced only as architectural intent D00.5 in `DECISIONS.md`. So D-10's "honor
`AURA_PRIVACY_MODE=local-only`" requires *introducing* the config field, not reading an
existing one.

**Decision.** Add a `PrivacyMode string` field to the sandbox/config surface
(`internal/config/config.go`), read via `envDefault("AURA_PRIVACY_MODE", "")` following the
existing `envDefault`/`envIntDefault` pattern. At **session-create time**, **FAIL-FAST** when:

```
PrivacyMode == "local-only"  AND  AURA_SANDBOX_NETWORK_ALLOW_HOSTS is non-empty
```

→ an **explicit operator error** (typed sentinel, e.g. `ErrPrivacyModeEgressDenied`), NOT a
silent-inert allowlist. Rationale (A5): a non-empty egress allowlist under maximum-privacy
mode is an operator misconfiguration; silently ignoring it would make "local-only" a hope,
not a guarantee (D00.5 invariant). Fail loud at the boundary where the operator can fix it.

**Scope guard.** Phase 8 introduces the field and the one fail-fast check on the *sandbox
egress* path only. It does NOT wire privacy-mode into the LLM/embedder/web routing paths
(those are D13's `LLMRouter` concern, Slice 13 / their own slices) — that would be scope
creep. The field is shared-surface; only the sandbox check lands here.

---

## OQ1/A2 (HIGHEST landmine #3) — Session-container network + seccomp posture

**Tension.** The 2a posture (AR-05-01) is **`connect(2)`-denied in seccomp** + a
non-masquerading `aura-sandbox-egressless` bridge (egress = 0.0% live bench). But 2b's
host-side proxy (D-08) **requires the session container to `connect(2)` to the proxy**.
These are in direct conflict for session containers.

**Decision (the contract Waves 3–5 implement):**

1. **Session containers need a seccomp variant that ALLOWS `connect(2)`.** Egress is contained
   **host-side by the forward proxy** (hostname-CONNECT allowlist + resolve-then-pin), NOT by
   connect-denial in seccomp. The 2a egressless profile stays the default for stateless calls;
   the session profile is the connect-allowed variant.

2. **The proxy must be reachable at the BRIDGE GATEWAY IP** — not container-local
   `127.0.0.1`. A loopback-published host port is unreachable from inside the container's
   network namespace; the container reaches the host across the bridge gateway. The sidecar's
   `HTTP_PROXY`/`HTTPS_PROXY`/`PIP_PROXY` env point at `<bridge-gateway-ip>:<proxy-port>`.

3. **An empty allowlist keeps the 2a egressless posture.** When
   `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` is empty (the default), the session container gets the
   egressless treatment (no proxy route, no reachable egress) — backwards-compatible with 2a
   deny-totale. The connect-allowed variant + proxy route activate only when the allowlist is
   non-empty.

4. **This deviation EXTENDS AR-05-01** and must be **re-stated in `08-SECURITY`** (authored in
   plan **08-09**, the security re-audit) — mirroring AR-05-01's reconciliation obligation:
   the shipped session egress control (connect-allowed seccomp + host proxy + bridge-gateway
   reachability) must be documented in the threat register, not shipped silently.

5. **Live reachability spike is a Wave-5 gate item** (A2 HIGH risk): the session container →
   host proxy reachability at the bridge gateway is the single highest-risk unverified
   assumption (if unreachable, the proxy is dead and ROADMAP success-criterion #4 fails). The
   Wave-5 (08-08 wiring / live finalize) gate MUST include a live network-reachability probe of
   the session container reaching the host proxy, before declaring criterion #4 met.

---

## Consumption map (which wave cites which decision)

| Decision | Consumed by |
|---|---|
| OQ2/A4 — export minimal SSRF surface | **08-04** (export `ClassifyIP` + dial-guard, re-test web tier); **08-06** (proxy reuses the export, no copy) |
| OQ4/A5 — privacy-mode fail-fast | **08-02** (add `PrivacyMode` config field); **08-05/08-08** (session-create fail-fast check) |
| OQ1/A2 — session network+seccomp posture | **08-07** (sidecar/session exec path); **08-08** (network/seccomp posture wiring + live reachability spike); **08-09** (08-SECURITY re-state, extends AR-05-01) |

---

*Phase: 8 — Sandbox 2b Session-Bound*
*Wave-0 decisions resolved: 2026-06-03*
