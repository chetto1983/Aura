# Phase 46: MCP trust and facade - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-08-16 (first pass) · 2026-08-17 (second pass, after Phase 45.1 closed)
**Phase:** 46-mcp-trust-and-facade
**Areas discussed (first pass):** Trust posture, Native MCP client, comms facade shape,
Per-action risk, accountId
**Areas discussed (second pass):** Fork delivery & pinning, Always-loaded without declaring,
Amendment batch scope, Landing order, Description budget, Live evidence

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

# Second pass — 2026-08-17

Re-opened after Phase 45.1 closed. Every file:line from the first pass was re-read at `02a291530`
before any question was asked. Findings that framed the session: D-01..D-04 and D-17..D-22 all held
verbatim; D-10..D-16 were delivered; **D-06 and D-07 turned out to have been done already** on
2026-08-16; `prd.md` had received **no** amendment for either phase; `prd.md:6150` still catalogued
`AURA_MCP_PING_INTERVAL_SEC` against a file 45.1 deleted; and `bridgePolicy.defaultDeferred()`
returns `true` unconditionally, so MCP-04's "always loaded" had no mechanism at all.

---

## Fork delivery — image pinning

| Option | Description | Selected |
|--------|-------------|----------|
| Immutable `:<sha>` tag | compose defaults move onto the commit-sha tag the forks already publish — what compose's own comment recommends. | ✓ |
| Digest `@sha256` | Matches the Prometheus/Tempo/Grafana rows and amendment #114's Docling pin. | |
| Keep floating `:sidecar` | Curation arrives on `docker compose pull` with no Aura commit. | |

**User's choice:** Immutable `:<sha>` tag.
**Notes:** This choice turned out to be structurally load-bearing — it is what later made the
two-repo cutover collapse into one atomic commit (Landing order, below).

---

## Fork delivery — what lands in Aura's tree

| Option | Description | Selected |
|--------|-------------|----------|
| Design doc + pin | New `docs/superpowers/specs/` doc for the two curated surfaces, mirroring the 2026-06-16 fork precedent. | ✓ |
| Pin + phase SUMMARY only | Contract lives in `.planning/` artifacts. | |
| Pin + checked-in `tools/list` capture | Drift caught by a fixture test. | |

**User's choice:** Design doc + pin.
**Notes:** The `tools/list` capture idea was not discarded — it reappeared as the mount-time
reconciliation in the drift question, which catches the same problem without a fixture to regenerate.

---

## Fork delivery — how work in another repo is tracked

| Option | Description | Selected |
|--------|-------------|----------|
| Fork plans inside phase 46 | Design doc first, one plan per fork committing in the fork repo, pin as the join point. | ✓ |
| Prerequisite, done before the phase | Phase 46 becomes Aura-side only. | |
| Separate phase per fork (46.1/46.2) | The 45.1 precedent. | |

**User's choice:** Fork plans inside phase 46.

---

## Fork delivery — the 28 raw handlers

| Option | Description | Selected |
|--------|-------------|----------|
| Delete them | The curated tool's action branches are the only path. | ✓ |
| Keep handlers, stop advertising | Easier upstream merges; the raw names stay callable on the sidecar port. | |
| Keep + advertise behind an env flag | Debuggable; a second live shape to test. | |

**User's choice:** Delete them.
**Notes:** Recorded with its cost — this is what makes the change one-way, and it is why the COMPAT
hazard (persisted rows referencing `calendar__*` / `whatsapp__*`) is written into CONTEXT.md rather
than left for Phase 47/48 to discover.

---

## Always-loaded without a declaration — the rule

| Option | Description | Selected |
|--------|-------------|----------|
| Tool-count budget | A server exposing ≤ N model-facing tools earns a loaded slot; no declaration anywhere. | ✓ |
| Server declares in `_meta` | Curation stays in the server; any mounted server could self-promote. | |
| Both — `_meta` hint capped by count | Most precise; two mechanisms. | |

**User's choice:** Tool-count budget.
**Notes:** Inventory ran first: MCP's `Tool` carries `Annotations`, `Title`, `Icons`, `OutputSchema`
and `_meta` — **no priority field exists in the protocol**, so an annotation-derived rule was
falsified before being offered.

---

## Always-loaded — overflow, threshold, cap, and where the numbers live

| Question | Options | Selected |
|--------|-------------|----------|
| Overflow | Fail closed (defer the overflow) / panic at boot / grant all + warn | **Fail closed** |
| Threshold | N = 3 / N = 1 / N = 4 | **N = 3** |
| Knobs | Code constants / env vars | **Code constants** |
| Global cap | 2 slots / 3 / 4 | **2** |

**Notes:** N = 3 was chosen specifically so memory's four model-facing tools stay deferred exactly as
today, leaving the promote-memory decision to Phase 48, which owns the 14-slot budget. Code constants
were preferred partly because the `AURA_MCP_*` catalogue is already in measured debt.

---

## Amendment batch — scope

| Option | Description | Selected |
|--------|-------------|----------|
| 46 + 45.1 + env catalog | Also ratifies 45.1's shipped-but-un-amended changes and repairs the `AURA_MCP_*` rows. | ✓ |
| 46 + 45.1, env catalog separate | Amendments about decisions, not bookkeeping. | |
| 46 only | Smallest diff; the PRD keeps describing a deleted ping loop. | |

**User's choice:** 46 + 45.1 + env catalog.

---

## Amendment batch — where the count rule is recorded, and what blocks code

| Question | Options | Selected |
|--------|-------------|----------|
| Tiering home | Inside TOOL-14's amendment / its own amendment under MCP-04 | **Inside TOOL-14** |
| Blocking | Decisions block, bookkeeping rides / all first, one commit / all first, several | **Decisions block** |

**Notes:** TOOL-14 already changes the tiering axis to "frequency plus a hard count budget", and
D-27's rule *is* a hard count budget — one statement of the axis, inherited by Phase 48.

---

## Amendment batch — document treatment

| Option | Description | Selected |
|--------|-------------|----------|
| Leave the strikethrough inline | The falsification trail is the point. | recommended, **not chosen** |
| Rewrite clean, history in a dated footnote | Current text reads plainly; superseded wording moves one hop away. | ✓ |

**User's choice:** Rewrite clean with dated footnotes — **against the recommendation**, and extended
by a follow-up answer to cover the ROADMAP's Phase 46 section (≈6 SUPERSEDED paragraphs) as well.
**Notes:** The counter-argument was put once and declined. Recorded so the relocation is not later
mistaken for drift; the footnotes are mandatory, and this is never a deletion of the history.

---

## Landing order — how the two sides meet

| Option | Description | Selected |
|--------|-------------|----------|
| One atomic Aura commit | Fork publishes first; pin + re-key + `Multiplexed` + classifier land together. | ✓ |
| Dual-key transition table | Order-independent; a table meaning two things plus a cleanup commit. | |
| Aura first, behind the old pin | Two reviewable halves with a fail-closed-and-unusable window between them. | |

**User's choice:** One atomic Aura commit.

---

## Landing order — fork drift, and the boot-panic hazard

| Question | Options | Selected |
|--------|-------------|----------|
| Unknown action | Loud at mount + fail closed at call / panic at boot / silent fall-through | **Loud at mount** |
| `Multiplexed` inference | Only when a classifier exists / exempt bridged tools from the guard / keep the panic | **Only when a classifier exists** |
| Table shape | Leave mixed + document / explicit key kind per source / split in two | **Leave mixed** |

**Notes:** The second row closed a hazard nobody had noticed: inferring `Multiplexed` from any
`action` enum would make `ValidateClassifiable` **panic on a stranger's server**, directly
contradicting Success Criterion 6. Gating the inference on the classifier's existence keeps the
guard whole and gives an unknown server exactly the fail-closed tier SC#6 promises.

---

## Description budget (opened by measurement, not by a question)

Measured over the captured manifest: calendar's 14 descriptions total **4,504 bytes** and WhatsApp's
**7,578**, against `frameMCPDescription`'s **4,096-byte** cap — and these two tools are now paid on
every turn.

| Question | Options | Selected |
|--------|-------------|----------|
| Budget | Tight ~1.5–2KB / use the full 4,096 / raise the cap for always-loaded tools | **Tight ~1.5–2KB** |
| Does D-19 still stand? | Stands / switch to `oneOf` per action (newly possible under SEP-2106) | **Stands** |

**Notes:** D-19 was re-examined rather than assumed, because SEP-2106's full JSON Schema 2020-12 was
genuinely new leverage. It was upheld on measured grounds: the pressure is on the description, not
the argument shape — 27 and 26 distinct properties are both far under the 128-property cap.

---

## Live evidence

| Question | Options | Selected |
|--------|-------------|----------|
| SC#1/#2/#4 | One driven conversation + `tool_invocations` rows / per-criterion probes / both | **Driven conversation** |
| SC#6 server | The calculator fork / a genuinely unknown public server / one Aura mints herself | **Calculator fork** |

**Notes:** The calculator fork is referenced in Aura's tree (`calculator_integration_test.go`,
`AURA_MCP_CALCULATOR_SERVER_JSON`), so the evidence must show the mount needs no code and no catalog
entry — not that the server was unknown. That caveat is written into CONTEXT.md D-38.

---

## Claude's Discretion

- Exact action names and grouping inside each fork's curated tool.
- Wording of the PRD amendments; whether they land as one commit or several.
- ~~The decimal number for the client-swap phase (`45.1` vs a renumber).~~ Settled — it shipped as 45.1.
- Whether `bridge_risk.go`'s re-keying keeps the `recipeSource` map shape or flattens it.
- Fix-on-touch: the stale `deferred_manifest.json` fixture, re-counted 2026-08-17 at **64** occurrences
  of pre-`34b892512` text.
- *(second pass)* The deterministic order in which the two always-loaded slots are granted; where the
  count predicate lives; how `refreshSpec` is stopped from flipping deferral mid-conversation.

## Deferred Ideas

- `tool_search` scope enforcement → Phase 51, when delegation creates workers with narrower grants.
- Named toolsets, default-off long tail, credential-gated auto-enable (hermes) → if Aura grows
  multi-tenant grants.
- hermes' `_scan_mcp_description` warn-only injection scan at mount.
- `validate_deferred_call_args` probe-validation for deferred tools → Phase 47/48.
- ~~Adopting the SDK's `IdempotentHint`/`OpenWorldHint`~~ → **done in 45.1**, as an escalate-only
  branch in the fallback path.
- Measuring how much of `managed_config`/`classify`/`probe` the SDK's `server/discover` subsumes.
- *(second pass)* Promoting memory into the always-loaded set → Phase 48, which owns the budget.
- *(second pass)* Digest (`@sha256`) pinning for the two sidecars → whenever appliance
  reproducibility is tightened as a whole.
- *(second pass)* A `_meta`-declared always-load hint → if a fork ever legitimately needs 4+ loaded.
- *(second pass)* `oneOf` per-action schemas → if prose-enforced required-per-action proves a real
  failure source.
- *(second pass)* `/gsd-validate-phase 45.1` — Nyquist per-task sampling still owed on the seam this
  phase builds on.
