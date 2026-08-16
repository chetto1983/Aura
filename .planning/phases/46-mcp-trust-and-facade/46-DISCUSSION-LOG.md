# Phase 46: MCP trust and facade - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-08-16
**Phase:** 46-mcp-trust-and-facade
**Areas discussed:** Trust posture, Native MCP client, comms facade shape, Per-action risk, accountId

**Standing directives given during the session:** research hermes first, do not invent; use the
native MCP client, no bespoke; no ceremony on installing an MCP; all our MCP servers are forks we
can modify.

---

## Trust posture — result fencing

| Option | Description | Selected |
|--------|-------------|----------|
| Fence every MCP result (hermes parity) | Revert `newResult` to `TrustUntrusted` for all bridged tools, unconditional. | first ✓, later reversed |
| Fence third-party only; memory stays trusted | Split on payload authorship. | |
| Keep everything trusted; ratify `34b892512` | Accept the shipped posture; rewrite MCP-02, delete SC#5. | |

**User's choice:** initially "fence every MCP result", then **reversed** to no fencing at all
(see "No-ceremony line" below).
**Notes:** Discovery that opened the area — commit `34b892512` (2026-08-12) had already shipped
MCP-01 and MCP-03 un-amended, and had additionally removed result fencing and Amendment #110's
memory-block header. PITFALLS §2 had named a fencing regression *"the actually dangerous outcome"*;
hermes fences every `mcp_*` result unconditionally. Both were put to the operator and declined.

---

## Trust posture — the injected memory block

| Option | Description | Selected |
|--------|-------------|----------|
| Restore the #110 header | Put the untrusted-reference framing back. | |
| Keep it dropped, amend #110 | Aura's recalled facts are her own knowledge; amend the PRD. | ✓ |
| Decide with the fencing answer | Treat both paths as one posture question. | |

**User's choice:** Keep it dropped, amend #110.

---

## No-ceremony line (operator-initiated, overrides the fencing answer above)

| Option | Description | Selected |
|--------|-------------|----------|
| Zero-config mount; fence output only | No install ceremony, plain descriptions, one envelope on returned bytes. | |
| Zero-config mount; no fencing either | Operator-installed means operator-trusted; MCP output reads as ordinary text. Requires rewriting MCP-02 and deleting SC#5. | ✓ |
| Fence only what a third party authored | Fence calendar/whatsapp payloads; memory stays plain. | |

**User's choice:** Zero-config mount, no fencing.
**Notes:** Raised by the operator as *"if one user install one mcp why we need stupid safert? when i
install one MCP to you there are no shit ceremony."* The counter-argument put to them — that trusting
the server is not the same as trusting the bytes, since the sender of an email is not the operator,
and that hermes fences despite the same single-operator install model — was heard and declined. The
concern was raised once, reaffirmed, and the decision recorded. Consequence: the fencing decision
needs zero code, because `34b892512` already shipped exactly this posture.

---

## Native MCP client — how the swap lands

| Option | Description | Selected |
|--------|-------------|----------|
| First, as its own phase before 46 | 46's work written once against the surviving seam; swap reviewable on its own diff. | ✓ |
| Inside Phase 46, as its first plans | One phase, one merge, blended diff. | |
| After 46, sequenced later | Fastest to the facade; everything written twice. | |

**User's choice:** Its own phase, before 46.
**Notes:** Prompted by the operator's *"use mcp client native no bespoke"*. Inventory measured
~1,970 LOC non-test of bespoke transport/reconnect the SDK replaces, against ~1,330 LOC of Aura
policy (SSRF, egress, classify, managed-config) with no SDK equivalent.

---

## Native MCP client — swap scope

| Option | Description | Selected |
|--------|-------------|----------|
| Client + transport + reconnect/ping only | Smallest swap; policy re-attached on top. | |
| That, plus adopt the middleware seam | Injection and fencing move onto `AddSendingMiddleware`. | |
| That, plus track the 2026-07-28 RC | Move to the stateless core. | ✓ |

**User's choice:** Track the 2026-07-28 stateless core.
**Notes:** Two caveats I attached to this option were **wrong and corrected in-session**: it does not
pin Aura to an unreleased RC, and the sidecars do not have to speak it. Measured against the module
cache — `go-sdk@v1.7.0`, already in `go.mod:27`, ships `latestProtocolVersion = "2026-07-28"` and
negotiates down five versions. The choice costs no dependency bump and no sidecar work.

---

## Native MCP client — middleware seam

| Option | Description | Selected |
|--------|-------------|----------|
| Full — middleware is the seam | Injection + fencing hook on `AddSendingMiddleware`. | |
| Full, plus `_meta` identity for arcadedb-mcp | Also move Aura's own server off the `user_identifier` argument onto stateless `_meta`. | ✓ |
| Fencing only; injection stays in bridge | Keeps the memory path out of the swap diff. | |

**User's choice:** Full, plus `_meta` identity for `cmd/arcadedb-mcp`.
**Notes:** The operator asked for the stateless spec to be read properly before this was answered.
Deciding evidence: the SDK ships MRTR itself as default-on client middleware (`mcp/mrtr.go:22-31`),
so middleware is the SDK's own idiom rather than an adaptation.

---

## Native MCP client — elicitation / MRTR

| Option | Description | Selected |
|--------|-------------|----------|
| Route through Aura's approval gate (hermes parity) | `ElicitationHandler` on the existing HITL path, bounded timeout. | ✓ |
| Disable MRTR — fail closed | No server can ever prompt the operator. | |
| Defer to the swap phase's discussion | Decide with the rest of the client posture. | |

**User's choice:** Route through the approval path (confirmed twice, including after the no-ceremony
ruling — it is plumbing that makes the server's question reach the operator, not install ceremony).

---

## Native MCP client — traceability

| Option | Description | Selected |
|--------|-------------|----------|
| New `MCPC-*` requirements | Own REQ-ID family with traceability rows. | ✓ |
| Fold under TOOL-14's amendment | No new REQ-IDs. | |
| New requirements + a PRD amendment | Both. | |

**User's choice:** New `MCPC-*` requirements.

---

## comms facade — how curation is defined

| Option | Description | Selected |
|--------|-------------|----------|
| Declarative curation in managed-config | Data-driven per-server curation block. | first ✓, later superseded |
| Go table now, declarative later | Generalize `bridgePolicy` into a namespace-keyed table. | |
| Declarative curation + hermes toolsets | Both, plus named groups and scope enforcement. | |

**User's choice:** initially declarative curation, then **superseded** by *"all our MCP are a fork we
can modify"* — curation moved into the servers, and every Aura-side curation mechanism was cut.
**Notes:** The operator's earlier pushback — *"stop assume only this MCP we can install more
independent, look hermes no aura code"* — correctly identified that a per-integration facade in
Aura's tree does not generalize. Research established that hermes' own general answer is
per-server `include`/`exclude` plus named toolsets and scoped discovery, and that hermes has no
facade or multiplexer anywhere.

---

## comms facade — argument shape

| Option | Description | Selected |
|--------|-------------|----------|
| Flat union, per-action required fields | Matches Aura's existing `task`/`skill_manage`. | ✓ |
| Discriminated `oneOf` per action | Strongest against ambiguous ids; new pattern, provider support unverified. | |
| `action` + typed `params` object | Middle ground. | |

**User's choice:** Flat union — now applied inside the fork's tool rather than an Aura tool.

---

## comms facade — shape given the 14-slot budget

| Option | Description | Selected |
|--------|-------------|----------|
| One multiplexed tool per sidecar (2 slots) | `calendar` + `messages`, multiplexing inside the forks. | ✓ |
| Discrete curated verbs (~8-10 slots) | Claude Code / Poke shape; forces Phase 48 to re-size. | |
| One tool across both sidecars (1 slot) | MCP-04 read literally; needs the forks merged. | |

**User's choice:** One multiplexed tool per sidecar.
**Notes:** Surfaced a tension inside MCP-04 itself — it demands "a single loaded slot" while citing
Claude Code's "16 tools, all loaded, every one a curated surface" as its reference point.

---

## Per-action risk classification

| Option | Description | Selected |
|--------|-------------|----------|
| `classifyComms` reads the same curation data | One classifier fed by the curation block. | |
| Keep `trustedRecipeActions` as the source | Resolve action → raw tool → existing risk table; zero new risk data. | ✓ (implied) |
| Derive from MCP annotations, table as override | Uses the spec's own hints; trusts server-declared data for authorization. | |

**User's choice:** *"stop make stupid fence you d0n't have"* — no new table; reuse what is shipped.
**Notes:** A measured consequence was recorded rather than re-asked (CONTEXT.md D-21): a bridged
tool never sets `Multiplexed`, so `ValidateClassifiable` skips it and `classify` assigns one flat
tier to the whole merged tool — which would either gate every calendar read or leave `send_email`
un-gated, failing Success Criterion 2. The fix re-keys the existing `trustedRecipeActions` from
tool name to action name and adds no second risk source.

---

## accountId (MCP-05 / SC#4)

| Option | Description | Selected |
|--------|-------------|----------|
| Omit hints; opaque ref for handles | Host omits the routing-hint uses; facade returns one opaque reference. | |
| Omit hints only; leave handles alone | Smallest change; SC#4 then fails on the detail path. | |
| Fix it in the sidecar instead | The fork accepts the identifier shape its own listing returns. | ✓ |

**User's choice:** Fix it in the sidecar.
**Notes:** The first framing of this question was rejected by the operator with *"stop make shit and
study how MCP protocol and configuration need to be done"* — a fair hit, since the MCP specification
had not been read at that point. Reading it established that MCP has no `accountId` concept at all:
identity is the OAuth token, audience-bound per server (RFC 8707), authorization is OPTIONAL, and
local/stdio servers are told to take credentials from the environment. Measuring the captured
manifest then showed `accountId` is two different things under one name — a defaultable routing hint
in `create_event`/`get_calendar_events`, and a required opaque handle in
`get_calendar_event_details` — so host-side injection would pass the wrong account for an event that
came from another account's listing.

---

## Claude's Discretion

- Exact action names and grouping inside each fork's curated tool.
- Wording of the PRD amendments; whether they land as one commit or several.
- The decimal number for the client-swap phase (`45.1` vs a renumber).
- Whether `bridge_risk.go`'s re-keying keeps the `recipeSource` map shape or flattens it.
- Fix-on-touch: the stale `deferred_manifest.json` fixture still carrying pre-`34b892512` text.

## Deferred Ideas

- `tool_search` scope enforcement → Phase 51, when delegation creates workers with narrower grants.
- Named toolsets, default-off long tail, credential-gated auto-enable (hermes) → if Aura grows
  multi-tenant grants.
- hermes' `_scan_mcp_description` warn-only injection scan at mount.
- `validate_deferred_call_args` probe-validation for deferred tools → Phase 47/48.
- Adopting the SDK's `IdempotentHint`/`OpenWorldHint` → client-swap phase or Phase 48.
- Measuring how much of `managed_config`/`classify`/`probe` the SDK's `server/discover` subsumes.
