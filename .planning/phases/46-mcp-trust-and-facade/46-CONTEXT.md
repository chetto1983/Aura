# Phase 46: MCP trust and facade - Context

**Gathered:** 2026-08-16
**Status:** Ready for planning — **but see D-00: this phase's shape changed materially during
discussion, and three of its six requirements need amending before any code.**

<domain>
## Phase Boundary

Aura is a **generic MCP host**. Every mounted server — bundled recipe, ad hoc mount, or one Aura
mints through self-extension — works with **zero Aura-side code and zero declaration**: deferred,
discoverable, risk fail-closed. Curation of an over-broad tool surface happens **in the server**,
because every MCP server Aura ships is a fork Aura controls.

Six requirements: MCP-01, MCP-02, MCP-03, MCP-04, MCP-05, TOOL-14.

**Not this phase:** the native MCP-client swap (its own phase, lands BEFORE this one — see D-10..D-15);
`tool_search` scope enforcement (D-22); the tool-surface ceremony strip and un-defer/merges (47/48);
memory tiers (49).

**The measurement that reshaped the phase:** commit `34b892512` (2026-08-12, *"Trust MCP output
instead of framing it untrusted"*) already shipped MCP-01 and MCP-03, un-amended, and went further
than either by also dropping result fencing and Amendment #110's memory-block header. The operator
ratified that posture during this discussion (D-01). **Consequence: the trust axis of this phase is
now documentation work, not code.**

</domain>

<decisions>
## Implementation Decisions

The operator's standing directives, given during area selection and reaffirmed twice:
**(1) research hermes first, adopt its shape, do not invent** (`D:/tmp/hermes-agent`);
**(2) use the native MCP client, no bespoke**; **(3) no ceremony** — *"when I install one MCP to you
there are no shit ceremony"*; **(4) all our MCP are a fork we can modify.**

### D-00 — What this phase actually is now (read first)

The roadmap's Phase 46 assumed the description wrapper was still in place and that result fencing
survived. Neither is true. What remains:

| Requirement | State after measurement | Work left |
|---|---|---|
| MCP-01 (descriptions plain) | **shipped** `34b892512` | ratify by amendment |
| MCP-02 (fencing + fail-closed risk) | fencing **removed** by decision; risk classification intact | rewrite the requirement |
| MCP-03 (unconditional trust) | **shipped**, already unscoped | ratify by amendment |
| MCP-04 (curated comms surface) | not started; **mechanism changed** | fork-side curation, 2 slots |
| MCP-05 (accountId host-resolved) | not started; **framing falsified** | fix in the fork |
| TOOL-14 (tiering amendment) | not started | PRD amendment, gates 46 and 48 |

### Trust posture (MCP-01, MCP-02, MCP-03)

- **D-01 (operator decision, overrides the earlier answer in this same session):** **Zero-config
  mount, no result fencing.** An operator-installed MCP server is operator-trusted; its output reads
  as ordinary text like a built-in. `newResult` stays `TrustTrusted`, `frameMCPDescription` /
  `frameMCPSummary` stay plain. **This requires no code — `34b892512` already shipped it.**
  The recommendation to fence was put twice (hermes fences every `mcp_*` result unconditionally;
  PITFALLS §2 calls a fencing regression *"the actually dangerous outcome"*) and was declined on the
  grounds that the operator controls what gets mounted. Recorded as the operator's call, with the
  evidence, so a later reader does not mistake it for an oversight.
  — **Reversibility:** reversible — restoring the envelope is a one-line provenance change in
  `newResult`; nothing persists the trust framing.

- **D-02:** Non-MCP sources are **untouched** and keep the nonce envelope: `web_fetch`,
  `document_search` / `document_open` (required by `prd.md:4579` — *"passage text remains
  TrustUntrusted"*), user-uploaded attachments, swarm child output. The exemption is MCP-only.

- **D-03:** The injected memory block keeps the plain framing `34b892512` gave it; **Amendment #110
  (`prd.md:4549`) is amended**, not restored. Its *"non-persisted, untrusted reference item"* wording
  is superseded.

- **D-04:** Guardrails that survive and must be **proven live**, since they are now the only ones:
  `mcpToolRisk`'s fail-closed default (`bridge_risk.go:112-118`, unannotated → `true, true`);
  the model-blind approval gate (`approve.go`, no tool schema exposes it); bridge namespacing +
  `Registry.Register`'s panic-on-duplicate; `capSchemaDescriptions`' byte caps (16KB schema /
  4096 description / 512 per-arg / 128 properties).

### Amendments that BLOCK code (CLAUDE.md: PRD-amendment-before-code)

- **D-05 (BLOCKING):** One PRD amendment ratifies `34b892512` — descriptions plain (MCP-01/03),
  results trusted (superseding MCP-02's fencing clause), and #110's memory-block header dropped.
  Dated, citing the commit. It is recording a decision already in the tree, which is exactly the
  case CLAUDE.md's PRD-first principle exists for.
- **D-06 (BLOCKING):** **MCP-02 is rewritten.** *"Per-call result fencing … remain in force and are
  proven by test"* is false by decision. What it asserts instead is D-04's list.
- **D-07 (BLOCKING):** **Success Criterion 5 is deleted.** It proves *"the result-fencing envelope …
  was what carried the defense"*; there is no envelope. Do not silently reinterpret it — remove it,
  citing D-01.
- **D-08 (BLOCKING):** **MCP-04 is amended** — curation moves into the forks; the result is **two**
  always-loaded slots, not one; no Aura-side facade tool exists. See D-16..D-19.
- **D-09 (BLOCKING):** **MCP-05 and Success Criterion 4 are amended** — see D-20.
- **TOOL-14** already requires its own amendment and gates both this phase and Phase 48. It lands
  with the above; whether as one commit or several is the implementer's call.

### The native MCP client — ITS OWN PHASE, BEFORE 46

- **D-10:** The swap lands as **a separate phase sequenced before Phase 46**, so 46's remaining work
  is written once against the seam that survives, and the client rewrite is reviewable on its own
  diff. Numbering per ROADMAP's decimal convention (`45.1`) or a renumber — mechanical, planner's call.
  It needs its own `/gsd-phase` insertion and its own discuss pass; **its decisions are recorded here
  only so they are not lost.**

- **D-11 (measured, corrects an assumption made earlier in this session):**
  `github.com/modelcontextprotocol/go-sdk v1.7.0` — **already in `go.mod:27`** — has
  `latestProtocolVersion = protocolVersion20260728 = "2026-07-28"` (`mcp/shared.go:50-51`) and
  negotiates down five versions (`2026-07-28`, `2025-11-25`, `2025-06-18`, `2025-03-26`,
  `2024-11-05`), capping legacy peers at `2025-11-25` via `negotiatedVersion`.
  **No dependency bump. No sidecar work.** The SDK is used server-side only today
  (`cmd/arcadedb-mcp/*`); the client in `internal/mcp` is hand-rolled.

- **D-12:** Scope = client + transport + reconnect + keepalive, **plus** the stateless 2026-07-28
  core. Deletes ≈1,970 LOC non-test plus ≈900 LOC of tests:
  `internal/mcp/client.go` (583), `http_client.go` (426),
  `internal/agent/mcptools/bridge_reconnect.go` (481), `bridge_ping.go` (113),
  `transport.go`/`protocol.go`/`tool_methods.go`/`lifecycle.go`/`probe.go` (364).
  — **Reversibility:** one-way — the bespoke transport and its tests are deleted, and the bridge is
  re-pointed at a different client type; reverting means restoring a hand-rolled JSON-RPC client
  against a spec version that no longer has the session model it was built on.

- **D-13:** **Aura policy that must survive on top of the SDK:** `ssrf.go` (194) +
  `transport_ssrf.go` (111) + `egress_policy.go` (104) — resolve-then-pin, re-attached as a custom
  `http.RoundTripper`; `managed_config.go` (346) + `managed_config_identity.go` (263);
  `classify.go` (100) trust taxonomy; `tool_error.go` (125); `domain_outcome.go`;
  `observability.go`; `redact.go`.

- **D-14:** **Full middleware seam, plus `_meta` identity for Aura's own server.**
  `Client.AddSendingMiddleware` (`mcp/client.go:1131`, `Middleware func(MethodHandler) MethodHandler`,
  `mcp/shared.go:110`) carries host-side argument derivation; `withMemoryUserIdentifier` moves onto
  it. `cmd/arcadedb-mcp` moves off the `user_identifier` **argument** onto stateless `_meta`, since
  Aura owns both ends. Third-party forks keep argument-shaped inputs.
  Rationale: middleware is the SDK's own idiom, not an adaptation — MRTR itself ships as client
  middleware, enabled by default (`mcp/mrtr.go:22-31`).
  — **Reversibility:** costly — `cmd/arcadedb-mcp`'s tool contract changes, so the memory tool's
  schema shifts; PITFALLS §4 applies (rehydrated history, paused approvals, scheduled jobs).

- **D-15:** **Elicitation routes through Aura's approval path** with a bounded timeout, naming the
  asking server. hermes parity (`tools/mcp_tool.py:1669-1758`, `request_elicitation_consent`, 5-min
  default mirroring the gateway approval). Not ceremony on the operator's install — it is the
  plumbing that makes a server's mid-call question reach the operator at all; the SDK auto-fulfills
  via client handlers by default and Aura currently registers none. Constrained by SEP-2260: a
  server may only initiate a request while actively processing a client request.

- **D-16:** The swap phase gets **new `MCPC-*` requirements** in REQUIREMENTS.md with traceability
  rows (e.g. native client adopted; policy layers preserved on top; bespoke transport/reconnect
  deleted), preserving the mapped/0-unmapped invariant STATE.md tracks.

### The curated surface (MCP-04) — curation lives in the forks

- **D-17 (operator decision):** **All of Aura's MCP servers are forks Aura controls**
  (`chetto1983/aura-pim-mcp`, `chetto1983/whatsapp-mcp`, and in-tree `cmd/arcadedb-mcp`), so
  **curation belongs in the server, not in Aura.** A fork exposes a small good surface instead of a
  large raw one. **No `comms` tool in Aura's Go tree, no curation config, no hide-list, no
  action→raw-tool mapping table, no `bridgePolicy` namespace-table generalization.**
  This supersedes research §2.3's Go-table recommendation and the "declarative curation in
  managed-config" answer given earlier in this same session: both were rejected as per-integration
  bespoke that the *next* mounted server would not benefit from.
  Aura's bridge stays generic — the property that makes a self-minted or ad hoc server work with
  zero Aura code.

- **D-18:** **Shape: one multiplexed tool per sidecar — two always-loaded slots**, replacing 28 raw
  tools. `aura-pim-mcp` serves one `calendar` tool (mail + calendar + contacts) with an `action`
  discriminator; `whatsapp-mcp` serves one `messages` tool. Fits Phase 48's 14-slot budget with
  room. Rejected: ~8-10 discrete verbs (Claude Code / Poke shape — better argument hygiene, but
  consumes most of the budget and forces Phase 48 to re-size); one tool spanning both sidecars
  (one MCP tool cannot be served by two servers without merging the forks).

- **D-19:** **Argument shape: flat union with per-action required fields** — one flat object,
  `action` plus every field any action needs, all optional in JSON Schema, the required-per-action
  contract enforced server-side and stated in the description. Matches Aura's existing
  `task` / `skill_manage` multiplexed tools. **Carry Poke's ID discipline into the fork's schema:**
  never a bare `id` — `eventId`, `chatId`, `messageId`, `emailId`
  (`D:/tmp/system-prompts-and-models-of-ai-tools/Poke/Poke agent.txt:60-67`, *"CRITICAL: Always
  reference the correct ID type… Never use ambiguous 'id' references"*).

- **D-20 (MCP-05, framing falsified by measurement):** **Fix `accountId` in the fork, not by
  host-side injection.** Measured against the captured manifest
  (`internal/agent/tools/testdata/deferred_manifest.json`), `accountId` is **two different things
  under one name**:
  - a **routing hint** in `create_event` (`"default": null`, *"Omitting uses the first configured
    account"*) and `get_calendar_events` (*"omit to query all enabled accounts"*) — already
    defaultable, nothing to inject;
  - an **opaque handle** in `get_calendar_event_details`, where it is **required** and documented as
    *"Account ID from get_calendar_events"*, beside required `calendarId` and `eventId`.

  A host that injects a configured default into the handle case passes the **wrong account** for an
  event that came from another account's listing. So MCP-05's *"resolved host-side … like
  `user_identifier`"* cannot work as written, and Success Criterion 4 cannot be met by injection.
  The fork makes its detail tool accept the same identifier shape its listing returns — one opaque
  reference, the shape `prd.md:4579` already uses for documents
  (`document:<search_document_id>@<version>#<locator>`). Considered and rejected as inferior:
  host-side splitting of a synthesized token (adapts to the problem instead of removing it).
  **Note the spec basis:** MCP has **no `accountId` concept at all** — identity is the OAuth token,
  audience-bound per server (RFC 8707), authorization is OPTIONAL, and for local/stdio servers the
  spec says *"retrieve credentials from the environment."* `accountId` is the sidecar's invention.

- **D-21 (CONSEQUENCE — load-bearing for Success Criterion 2, measured):** **Merging into one
  bridged tool loses per-action risk classification, silently.**
  `specFromToolDefWithPolicy` never sets `Multiplexed`, so a bridged tool is `Multiplexed: false`;
  `ValidateClassifiable`'s per-action assertion (`guard.go:28`) skips it, and `classify` falls
  through to the generic branch — **one flat tier for the whole merged tool**. `calendar(action=
  list_events)` and `calendar(action=send_email)` would classify identically: everything Destructive
  (approval on every read, unusable) or everything Normal (`send_email` un-gated, SC#2 fails).
  This is exactly the failure `guard.go:16-20` documents in prose, reached by a path the guard
  cannot see, because it only fires when `Multiplexed` is already true.
  **Fix with existing machinery, adding no new table:** re-key `trustedRecipeActions`
  (`bridge_risk.go:23-80`) from *raw tool name* to *action name*, have the bridge set
  `Multiplexed: true` when a curated schema carries an `action` enum, and register the classifier in
  `multiplexedClassifiers`. Same data, different key. **Do not introduce a second risk source**
  (operator: no new fences), and **do not derive tiers from server-declared annotations** —
  `bridge_risk.go` deliberately lets `explicitDestructive` escalate only, never de-escalate.

### Claude's Discretion

- Exact action names and their grouping inside each fork's curated tool; the wording of the PRD
  amendments; whether the amendments land as one commit or several.
- The decimal number for the client-swap phase (`45.1` vs a renumber).
- Whether `bridge_risk.go`'s re-keying keeps the `recipeSource` map shape or flattens it, provided
  no second risk source appears.
- Fix-on-touch: `internal/agent/tools/testdata/deferred_manifest.json` still carries
  `"untrusted MCP server description"` / `"untrusted MCP server summary data"` text — it predates
  `34b892512` and is stale.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase definition
- `.planning/ROADMAP.md` §"Phase 46: MCP trust and facade" — goal, requirements, success criteria.
  **Success Criteria 4 and 5 are superseded (D-07, D-09); the rationale's "generalizing bridgePolicy
  into a namespace-keyed table" is superseded by D-17.**
- `.planning/REQUIREMENTS.md` — MCP-01..05, TOOL-14 verbatim. **MCP-02, MCP-04, MCP-05 need
  amending before code (D-06, D-08, D-09).**
- `.planning/PROJECT.md` — core value, constraints, and the named constraint to read the reference
  implementations before designing anything in this milestone.
- `.planning/phases/45-harness-correctness/45-CONTEXT.md` — D-09's boot guard is what D-21 interacts
  with; D-08 there already narrowed 46's dependency to the risk-override work only.
- `prd.md` — truth-source; D-05's amendment lands here. `:4549` (Amendment #110, memory block),
  `:4579` (document passages stay `TrustUntrusted`; the citation-token shape D-20 mirrors).

### Research grounding
- `.planning/research/ARCHITECTURE.md` §2 — the facade analysis. **§2.3's "generalize `bridgePolicy`
  into a namespace-keyed table" is superseded by D-17** (curation moved into the forks); §2.1/§2.2
  remain accurate as evidence.
- `.planning/research/PITFALLS.md` §"Pitfall 2" — the two-defense analysis and the recommendation to
  scope wrapper removal to code-reviewed recipes. **Declined by operator decision (D-01, and the
  standing MCP-03 decision of 2026-08-05).** Read it as the recorded counter-argument.
- `.planning/codebase/ARCHITECTURE.md` — component map, always-active tool set.

### Protocol (read before designing anything against MCP — CLAUDE.md documentation-first rule)
- `https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization` — OAuth 2.1,
  optional; stdio → env credentials; RFC 8707 audience binding; **no `accountId` concept** (D-20).
- `https://blog.modelcontextprotocol.io/posts/2026-07-28-release-candidate/` — the stateless core:
  SEP-2575 (no `initialize`/`initialized`), SEP-2567 (no `Mcp-Session-Id`), SEP-2260 (server→client
  requests only during active processing), SEP-2322 (MRTR), SEP-2106 (full JSON Schema 2020-12),
  SEP-2577 (roots/sampling/logging deprecated).
- `D:/tmp/mcp-go-sdk` — the SDK cloned to disk at `64e454e35c23` (2026-08-13). Key sites:
  `mcp/shared.go:44-79` (protocol versions, `negotiatedVersion`), `mcp/client.go:199-206`
  (`KeepAlive`), `:1131,1146` (middleware), `mcp/mrtr.go:22-31` (MRTR as default-on middleware),
  `mcp/protocol.go:1967-1991` (`ToolAnnotations`, incl. `IdempotentHint`/`OpenWorldHint`),
  `mcp/streamable_client.go:122-139` (reconnect/resume), `docs/protocol.md` (both lifecycle models).

### Reference implementations (on disk, read before designing)
- `D:/tmp/hermes-agent/agent/tool_dispatch_helpers.py:538-700` — `_UNTRUSTED_TOOL_PREFIXES =
  ("browser_", "mcp_")`, `_neutralize_delimiters`, the no-fast-path rule. **hermes fences every MCP
  result; Aura deliberately does not (D-01).** Recorded as the divergence it is.
- `D:/tmp/hermes-agent/tools/mcp_tool.py:543-592` — `_scan_mcp_description`, warn-never-block.
  `:1669-1758` — elicitation through the approval system (D-15). `:5515-5526` — `mcp__<server>__<tool>`
  naming. `:5832-5851` — per-server `tools.include`/`.exclude`, hermes' entire curation mechanism.
- `D:/tmp/hermes-agent/hermes_cli/tools_config.py:96-165` — named toolsets, `_DEFAULT_OFF_TOOLSETS`,
  credential-gated auto-enable. `D:/tmp/hermes-agent/tools/tool_search.py:946-963` —
  `scoped_deferrable_names`. **Both deferred (D-22), not adopted here.**
- `D:/tmp/system-prompts-and-models-of-ai-tools/Poke/Poke agent.txt:57-70` — the ID-type discipline
  D-19 carries into the fork schemas.

### Aura code the decisions bind to
- `internal/agent/mcptools/bridge.go:129-143` (`newResult`, `TrustTrusted` — D-01), `:226-240`
  (`specFromToolDefWithPolicy`, `applyMCPOperationMetadata` — where `Multiplexed` is never set, D-21),
  `:248-273` (`capSchemaDescriptions` — D-04), `:358-395` (`frameMCPSummary`/`frameMCPDescription`).
- `internal/agent/mcptools/bridge_risk.go:23-80` (`trustedRecipeActions`, the table D-21 re-keys),
  `:95-119` (`mcpToolRisk`, fail-closed default — D-04).
- `internal/agent/mcptools/bridge_memory.go` — `bridgePolicy`, `withMemoryUserIdentifier` (D-14).
- `internal/agent/trust.go:38-57` (`untrustedSource`, the `TrustTrusted` short-circuit — D-01/D-02).
- `internal/gateway/guard.go:22-38` — `ValidateClassifiable`; D-21's whole argument.
- `internal/gateway/classify.go:22-31` — `multiplexedClassifiers`, the map D-21 adds to.
- `internal/mcp/client.go:88-91` — `ToolAnnotations` drops `IdempotentHint`/`OpenWorldHint`.
- `internal/mcp/manager/catalog.go:112-173` — `BuiltInCatalog`, the three mounted recipes.
- `internal/agent/tools/search.go:308,367` — `tool_search` over bare `Registry.All()`, no scope (D-22).
- `internal/agui/connect_pim_api.go` — the cockpit's PIM admin proxy; where accounts are created (D-20).
- `compose.yaml:993-1020` — the `aura-pim-mcp` sidecar and its env.

### Commit of record
- `34b892512` (2026-08-12) — *"Trust MCP output instead of framing it untrusted."* The un-amended
  change D-05 ratifies. Read its message: it states its own scope accurately.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `mcpToolRisk` + `trustedRecipeActions` (`bridge_risk.go`) — the ONLY risk source; D-21 re-keys it
  rather than adding a second.
- `multiplexedClassifiers` + `ValidateClassifiable` (`gateway/classify.go`, `guard.go`) — the shipped
  per-action gating path; D-21 routes the curated tools onto it.
- `withMemoryUserIdentifier` (`bridge_memory.go:45-58`) — the host-side derivation pattern; moves to
  SDK middleware in the swap phase (D-14).
- `capSchemaDescriptions` / `boundedMCPError` / `refreshSpec` — bridge machinery that keeps working
  unchanged; `refreshSpec` re-syncs a bridged spec on every reconnect, which is what makes fork-side
  curation land in Aura for free.
- `internal/mcp/ssrf.go` + `transport_ssrf.go` + `egress_policy.go` — resolve-then-pin; survives the
  client swap as a custom `http.RoundTripper` (D-13).

### Established Patterns
- **Fail closed on wiring, fail open on outages.** `Registry.Register` panics on duplicate names;
  `ValidateClassifiable` panics on missing classifiers/metadata; MCP mounts fail soft. D-21 sits on
  the wiring side.
- **Monotone saturate-upward classification** (`classify.go`): the default is the mutating floor,
  only enumerated reads de-escalate, unknown saturates to Risky. A re-keyed table must preserve this.
- **Deferred by default for bridged tools** (`bridgePolicy.defaultDeferred`). D-18's two curated
  tools are the deliberate exception — always loaded, never deferred (MCP-04).
- **No non-test Go file over 600 LOC.** `bridge.go` is 532 and is touched by D-21 — refactor on touch
  is likely required in the same commit.
- **`messages[0]` byte-stable**; the always-block sits at `messages[1]`. D-03 touches prompt text.

### Integration Points
- `bridge.go` `specFromToolDefWithPolicy` — set `Multiplexed` from the curated schema (D-21).
- `bridge_risk.go` — re-key the action table (D-21).
- `gateway/classify.go` — one classifier entry per curated tool (D-21).
- The two forks — where the actual curation work happens (D-17, D-18, D-19, D-20).
- REQUIREMENTS.md / ROADMAP.md / prd.md — the blocking amendments (D-05..D-09).

</code_context>

<specifics>
## Specific Ideas

- **"No ceremony."** The organising rule of this phase, stated plainly: *"if one user install one mcp
  why we need stupid safety? when i install one MCP to you there are no shit ceremony."* Every
  mechanism that would have made mounting an MCP server require a declaration — curation config,
  toolsets, scope gates, per-server trust tiers — was cut. What remains is what already exists and
  costs the operator nothing at install time.
- **"All our MCP are a fork we can modify."** The unlock that removed the Aura-side facade entirely.
  A problem solvable in the server should not become a wrapper in the host.
- **Three roadmap/requirement claims were falsified by reading code and the spec rather than the
  document** — the description wrapper (already gone), result fencing (already gone), and
  `accountId`-as-injectable (it is two different things, one of them a handle). All three are
  recorded as amendments with dates and evidence rather than worked around, which is the PRD-first
  principle running the direction it is meant to.
- The SDK-client research corrected one of my own claims mid-discussion: "track the RC" was presented
  as costing a dependency bump and sidecar work, and the pinned `v1.7.0` already ships it with
  five-version negotiation. Recorded so the planner does not re-inherit the wrong cost.

</specifics>

<deferred>
## Deferred Ideas

- **D-22 — `tool_search` scope enforcement.** Aura ranks over bare `Registry.All()` with no scope
  filter; any registered tool is discoverable and callable by any session, including a swarm worker.
  hermes enforces at two sites (`scoped_deferrable_names`, checked by both the bridge dispatch and
  the executor unwrap). Not a live gap for a single operator with no restricted-grant sessions.
  → revisit at **Phase 51** (durable delegation), when workers with narrower grants first exist.
- **Named toolsets + default-off long tail + credential-gated auto-enable** (hermes
  `CONFIGURABLE_TOOLSETS`, `_DEFAULT_OFF_TOOLSETS`, `check_fn`). Cut as ceremony for a single-operator
  deployment. → reopen if Aura grows multi-tenant grants or the mounted-server count makes
  "which integrations are on" a real question.
- **hermes' `_scan_mcp_description`** — warn-only injection-pattern scan over server-declared
  descriptions at mount. Compatible with trusting descriptions (it logs, never blocks) and would give
  the operator a signal when a mounted server's text looks weaponized. Cut as ceremony for now.
- **`validate_deferred_call_args`** (`hermes tools/tool_search.py:966+`) — probe-validate a deferred
  tool's arguments before dispatch, because models call deferred tools blind by name and *"cheap
  models loop on it until the iteration budget dies."* Aura has the same deferred pattern and no such
  probe. → Phase 47/48 (tool-surface work).
- **Adopting the SDK's `IdempotentHint` / `OpenWorldHint`** — `internal/mcp/client.go:88-91` drops
  both. `idempotentHint` is the spec's own signal for what `applyMCPOperationMetadata` hardcodes.
  → the client-swap phase, or Phase 48.
- **Retiring the rest of `internal/mcp`'s hand-rolled surface** beyond client/transport — how much of
  `managed_config`, `classify` and `probe` the SDK's `server/discover` (SEP-2575) subsumes was not
  measured. → the client-swap phase's own research.

</deferred>

---

*Phase: 46-mcp-trust-and-facade*
*Context gathered: 2026-08-16*
