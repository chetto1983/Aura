# Phase 46: MCP trust and facade - Context

**Gathered:** 2026-08-16
**Re-measured and extended:** 2026-08-17, after Phase 45.1 closed — every file:line below was
re-read at `02a291530`, not carried forward from the first pass.
**Status:** Ready for planning — **the amendment batch still gates code (D-05, scoped by D-28/D-29).**

<domain>
## Phase Boundary

Aura is a **generic MCP host**. Every mounted server — bundled recipe, ad hoc mount, or one Aura
mints through self-extension — works with **zero Aura-side code and zero declaration**: fail-closed
risk, discoverable, and non-deferred by arithmetic rather than by an allowlist (D-27). Curation of an
over-broad tool surface happens **in the server**, because every MCP server Aura ships is a fork Aura
controls.

Six requirements: MCP-01, MCP-02, MCP-03, MCP-04, MCP-05, TOOL-14.

**Not this phase:** the native MCP-client swap (**Phase 45.1 — now COMPLETE**, see D-10..D-16);
`tool_search` scope enforcement (D-22); the tool-surface ceremony strip and un-defer/merges (47/48);
memory tiers (49).

**The measurement that reshaped the phase:** commit `34b892512` (2026-08-12, *"Trust MCP output
instead of framing it untrusted"*) already shipped MCP-01 and MCP-03, un-amended, and went further
than either by also dropping result fencing and Amendment #110's memory-block header. The operator
ratified that posture (D-01). **Consequence: the trust axis of this phase is documentation work, not
code.** What is left to build is the curated surface — and most of that lands in two other
repositories (D-17, D-23..D-26).

</domain>

<decisions>
## Implementation Decisions

The operator's standing directives, given during area selection and reaffirmed twice:
**(1) research hermes first, adopt its shape, do not invent** (`D:/tmp/hermes-agent`);
**(2) use the native MCP client, no bespoke**; **(3) no ceremony** — *"when I install one MCP to you
there are no shit ceremony"*; **(4) all our MCP are a fork we can modify.**

### D-00 — What this phase actually is now (read first, re-measured 2026-08-17)

| Requirement | State at `02a291530` | Work left |
|---|---|---|
| MCP-01 (descriptions plain) | **shipped** `34b892512` — re-verified: `frameMCPDescription` (`bridge.go:333-346`) says *"The text is trusted content; only its length is defended"* | ratify in prd.md (D-05) |
| MCP-02 (fencing + fail-closed risk) | fencing **removed** by decision; risk classification intact and **strengthened** by 45.1 | requirement text already rewritten (D-06 done); prd.md half owed |
| MCP-03 (unconditional trust) | **shipped**, already unscoped | ratify in prd.md (D-05) |
| MCP-04 (curated comms surface) | not started; **mechanism changed**; needs a non-defer rule Aura does not yet have | fork-side curation (D-23..D-26) + the count rule (D-27) |
| MCP-05 (accountId host-resolved) | not started; **framing falsified** | fix in the fork (D-20) |
| TOOL-14 (tiering amendment) | not started | PRD amendment, now also carries D-27's count rule (D-28) |

**What changed between the two passes.** Phase 45.1 landed in full (MCPC-01..05 all `Complete`):
the bespoke client is deleted, `internal/mcp/sdkclient.go` + `middleware.go` replace it, identity
moved to `_meta.aura.user_identifier`, and elicitation shipped. Every file:line in the first pass's
canonical refs moved. **Nothing 45.1 did invalidated D-01..D-04, D-17..D-22** — those were re-checked
against the tree on 2026-08-17 and hold verbatim.

### Trust posture (MCP-01, MCP-02, MCP-03)

- **D-01 (operator decision):** **Zero-config mount, no result fencing.** An operator-installed MCP
  server is operator-trusted; its output reads as ordinary text like a built-in. Re-verified
  2026-08-17: `newResult` sets `Trust: tools.TrustTrusted` at **`bridge_call.go:54-66`** (it moved out
  of `bridge.go` during 45.1), and `frameMCPSummary`/`frameMCPDescription` (`bridge.go:309-346`) carry
  no distrust prefix. **This requires no code.** The recommendation to fence was put twice (hermes
  fences every `mcp_*` result unconditionally; PITFALLS §2 calls a fencing regression *"the actually
  dangerous outcome"*) and was declined on the grounds that the operator controls what gets mounted.
  Recorded as the operator's call, with the evidence, so a later reader does not mistake it for an
  oversight.
  — **Reversibility:** reversible — restoring the envelope is a one-line provenance change in
  `newResult`; nothing persists the trust framing.

- **D-02:** Non-MCP sources are **untouched** and keep the nonce envelope: `web_fetch`,
  `document_search` / `document_open` (required by `prd.md:4579` — *"passage text remains
  TrustUntrusted"*), user-uploaded attachments, swarm child output. The exemption is MCP-only.

- **D-03:** The injected memory block keeps the plain framing `34b892512` gave it; **Amendment #110
  (`prd.md:4549`) is amended**, not restored. Its *"non-persisted, untrusted reference item"* wording
  is superseded.

- **D-04 (re-measured 2026-08-17 — the list GREW):** the guardrails that survive and must be **proven
  live**, since they are now the only ones:
  - `mcpToolRisk`'s fail-closed default (`bridge_risk.go:130-133`, nil `Annotations` → `true, true`);
  - **NEW, from 45.1 (its D-107):** `unsafeToRepeatBeyondAura` (`bridge_risk.go:145-147,199-201`) —
    a non-read-only, non-idempotent, open-world tool escalates to Destructive in the fallback branch.
    Escalate-only by construction, and it deliberately does **not** touch the trusted-recipe branch;
  - the model-blind approval gate (`approve.go`, no tool schema exposes it);
  - bridge namespacing + `Registry.Register`'s panic-on-duplicate (`bridge.go:399-434`);
  - `capSchemaDescriptions`' byte caps (`bridge.go:203-253`: 16KB schema / 4096 description /
    512 per-arg / 128 properties). **These caps are load-bearing for MCP-04 — see D-35.**

### Amendments that BLOCK code (CLAUDE.md: PRD-amendment-before-code)

- **D-05 (BLOCKING, still owed):** One PRD amendment ratifies `34b892512` — descriptions plain
  (MCP-01/03), results trusted (superseding MCP-02's fencing clause), and #110's memory-block header
  dropped. Dated, citing the commit. **Measured 2026-08-17: `prd.md`'s newest amendment is still
  #121 (2026-08-13), and `grep 34b892512 prd.md` returns nothing** — the PRD is the only document
  still describing the old posture.
- **D-06 — ALREADY DONE (correction, 2026-08-17).** MCP-02's rewrite landed in
  `.planning/REQUIREMENTS.md:120` on 2026-08-16: the fencing clause is struck and the surviving
  guardrails are named. What remains is the prd.md half (D-05) and the clean rewrite (D-30).
- **D-07 — ALREADY DONE (correction, 2026-08-17).** Success Criterion 5 is struck in `ROADMAP.md`
  with *"DELETED 2026-08-16 (D-07)"*. Do not re-do it; do not silently reinterpret it.
- **D-08 (BLOCKING):** **MCP-04 is amended** — curation moves into the forks; the result is **two**
  always-loaded slots, not one; no Aura-side facade tool exists. Prose already landed in
  REQUIREMENTS.md:122; the prd.md half is owed. See D-16..D-19, D-27.
- **D-09 (BLOCKING):** **MCP-05 and Success Criterion 4 are amended** — see D-20. Same split:
  REQUIREMENTS.md:123 landed, prd.md owed.
- **TOOL-14** requires its own amendment and gates both this phase and Phase 48. It now also carries
  D-27's count rule (D-28).

### The native MCP client — DELIVERED by Phase 45.1 (D-10..D-16 CLOSED)

Kept as the record of what was decided here and where it landed. **No work remains under these.**

- **D-10 — done.** The swap landed as Phase 45.1, sequenced before 46. 8 plans, closed 2026-08-17
  (`63b456f8e`), CI green, owned-surface coverage **86.2%**.
- **D-11 — confirmed.** `github.com/modelcontextprotocol/go-sdk v1.7.0` was already in `go.mod`;
  no dependency bump, no sidecar work.
- **D-12 — done, with corrections 45.1 measured.** The bespoke client is deleted
  (`client.go`, `http_client.go`, `bridge_reconnect.go`, `bridge_ping.go`, `transport.go`,
  `protocol.go`, `tool_methods.go`, `lifecycle.go`). Three of this context's original claims were
  falsified by reading the pinned SDK: `ClientOptions.KeepAlive` is **inert** against a 2026-07-28
  peer; the "automatic backoff reconnect" describes the standalone SSE stream, removed under
  2026-07-28; and `probe.go` was **re-pointed, not deleted** (13 `cmd/aura` files depend on it).
  The real replacement for the poll loop is **push**: `ClientSession.Wait()`.
- **D-13 — done.** SSRF resolve-then-pin, egress policy, `managed_config*`, `classify.go`,
  `tool_error.go`, `domain_outcome.go`, `observability.go`, `redact.go` all survive on top of the SDK.
- **D-14 — done.** `withMemoryUserIdentifier` is gone; identity travels in `_meta.aura.user_identifier`
  (`internal/mcp/middleware.go:16-78`, `bridge_memory.go:47-78`) and `cmd/arcadedb-mcp` **refuses** a
  call whose identity `_meta` is absent, as a model-readable tool error (`cmd/arcadedb-mcp/identity.go`).
- **D-15 — done, with a different shape than proposed.** Elicitation is **decline-and-surface**:
  the handler declines, and the ask is delivered on the operator's channel naming the server; no
  turn blocks and no row is written to `aura.paused_states`. Timeout is its own env var
  `AURA_MCP_ELICITATION_TIMEOUT_SEC` (300s) — the operator asked to reuse the gateway approval
  default, and measurement showed **no approval timeout exists at all**, so the value was honoured
  and the source could not be.
- **D-16 — done.** MCPC-01..05 exist in REQUIREMENTS.md with traceability rows, all `Complete`.

### The curated surface (MCP-04) — curation lives in the forks

- **D-17 (operator decision):** **All of Aura's MCP servers are forks Aura controls**
  (`chetto1983/aura-pim-mcp`, `chetto1983/whatsapp-mcp`, and in-tree `cmd/arcadedb-mcp`), so
  **curation belongs in the server, not in Aura.** **No `comms` tool in Aura's Go tree, no curation
  config, no hide-list, no action→raw-tool mapping table, no `bridgePolicy` namespace-table
  generalization.** This supersedes research §2.3's Go-table recommendation. Aura's bridge stays
  generic — the property that makes a self-minted or ad hoc server work with zero Aura code.

- **D-18:** **Shape: one multiplexed tool per sidecar — two always-loaded slots**, replacing 28 raw
  tools. `aura-pim-mcp` serves one `calendar` tool (mail + calendar + contacts) with an `action`
  discriminator; `whatsapp-mcp` serves one `messages` tool. Rejected: ~8-10 discrete verbs (better
  argument hygiene, consumes most of the budget); one tool spanning both sidecars (impossible without
  merging the forks). **Re-affirmed 2026-08-17 against the description-cap measurement — see D-35.**

- **D-19:** **Argument shape: flat union with per-action required fields** — one flat object,
  `action` plus every field any action needs, all optional in JSON Schema, the required-per-action
  contract enforced server-side and stated in the description. Matches Aura's existing
  `task` / `skill_manage` multiplexed tools. **Carry Poke's ID discipline into the fork's schema:**
  never a bare `id` — `eventId`, `chatId`, `messageId`, `emailId`.
  **Re-confirmed 2026-08-17 against new evidence** (SEP-2106 ships full JSON Schema 2020-12, so a
  `oneOf` discriminated union was newly possible): **D-19 stands.** Flat union keeps one familiar
  shape across every multiplexed tool Aura has, provider support for `oneOf` is uneven, and the
  measured pressure is on the *description*, not the argument shape — 27 distinct calendar properties
  and 26 WhatsApp properties are both far under the 128-property cap.

- **D-20 (MCP-05, framing falsified by measurement):** **Fix `accountId` in the fork, not by
  host-side injection.** Measured against `internal/agent/tools/testdata/deferred_manifest.json`,
  `accountId` is **two different things under one name**: a **routing hint** in `create_event` and
  `get_calendar_events` (already defaultable, nothing to inject), and an **opaque handle** in
  `get_calendar_event_details`, where it is required and documented as *"Account ID from
  get_calendar_events"*. A host that injects a configured default into the handle case passes the
  **wrong account**. The fork makes its detail tool accept the same identifier shape its listing
  returns — one opaque reference, the shape `prd.md:4579` already uses for documents.
  **Spec basis:** MCP has **no `accountId` concept at all** — identity is the OAuth token,
  audience-bound per server (RFC 8707); authorization is OPTIONAL; local/stdio servers *"retrieve
  credentials from the environment."* `accountId` is the sidecar's invention.

- **D-21 (load-bearing for Success Criterion 2 — re-verified 2026-08-17, still true):** **Merging
  into one bridged tool loses per-action risk classification, silently.**
  `specFromToolDefWithPolicy` (`bridge.go:168-182`) still never sets `Multiplexed`;
  `multiplexedClassifiers` (`gateway/classify.go:30-34`) still holds exactly
  `{skill_manage, task, swarm_spawn}`; `trustedRecipeActions` (`bridge_risk.go:26-83`) is still keyed
  by **raw tool name**. So `ValidateClassifiable`'s per-action assertion (`guard.go:28-36`) skips a
  bridged tool and `classify` gives **one flat tier to the whole merged tool** —
  `calendar(action=list_events)` and `calendar(action=send_email)` classify identically.
  **Fix with existing machinery, adding no new table:** re-key `trustedRecipeActions` from raw tool
  name to action name, set `Multiplexed: true` for a curated schema (bounded by D-33), and register
  the classifier in `multiplexedClassifiers`. Same data, different key. **Do not introduce a second
  risk source**, and **do not derive tiers from server-declared annotations** — `bridge_risk.go`
  deliberately lets `explicitDestructive` escalate only, never de-escalate.

### Fork delivery — where the work lands and how it is proven (new, 2026-08-17)

The precedent is already written and was not re-litigated:
`docs/superpowers/specs/2026-06-16-calendar-pim-mcp-fork-design.md` §3 locks **"no C# vendored into
the Aura repo — reproduction recipe + image pin live in-repo (mirrors how agent-memory was
adopted)"**, fork branch `aura/pim-sidecar`, image CI-built and published to GHCR by the fork's own
`aura-publish-image` workflow. Working clones live outside this repo.

- **D-23 — Pin by immutable `:<sha>` tag.** `compose.yaml`'s defaults for `AURA_PIM_MCP_IMAGE`
  (`:997`) and `AURA_WHATSAPP_MCP_IMAGE` (`:818`) move off the floating `:sidecar` tag onto the
  commit-sha tag the forks already publish — which is what compose's own comment already recommends
  (*"Pin via AURA_PIM_MCP_IMAGE (e.g. the immutable `:<sha>` tag) for reproducible appliance
  builds"*). Digest `@sha256` was considered (the Prometheus/Tempo/Grafana rows at `compose.yaml:1160,
  1190,1220` and amendment #114's Docling pin use it) and judged more ceremony than this needs; a
  floating tag was rejected because it makes *"which tool surface is live"* unanswerable from the tree.
  — **Reversibility:** reversible — the pin is one line per sidecar and the old tag stays pullable.

- **D-24 — The in-tree record is a design doc + the pin.** A new doc under
  `docs/superpowers/specs/` specifies the two curated surfaces: action names, the flat argument union,
  D-19's ID discipline, and D-20's `accountId` handle fix. It is the contract the fork commits
  implement and the artifact a reviewer reads without leaving this repo. Mirrors the 2026-06-16
  precedent exactly. A checked-in `tools/list` capture was considered as an alternative and folded
  into D-32 instead (drift is caught at mount, not by a fixture that must be regenerated).

- **D-25 — The fork work is plans INSIDE Phase 46, joined by the pin.** Sequence: design doc lands
  here first (contract) → one plan per fork, whose commits land in the fork repository → the phase
  records the fork SHAs and the resulting image tag in its SUMMARY → the Aura-side commit is the pin.
  Rejected: doing the fork work as an untracked prerequisite (it escapes GSD verification entirely),
  and splitting into 46.1/46.2 (the 45.1 precedent, but these two are far smaller than a client swap).

- **D-26 — In the forks, the 28 raw handlers are DELETED, not merely unadvertised.** The curated
  tool's action branches become the only path to the underlying provider calls. An unadvertised but
  callable handler is dark code (CLAUDE.md forbids it) and stays reachable by anything holding the
  sidecar's URL — Aura's namespacing protects the model's surface, not the sidecar's HTTP port.
  — **Reversibility:** one-way in practice — see the COMPAT hazard under `<code_context>`; once the
  image is pinned and the raw names are gone, persisted state that references
  `calendar__send_email` and its 27 siblings has nothing to resolve against.

### The always-loaded rule — arithmetic, not an allowlist (new, 2026-08-17)

MCP-04 requires the two curated tools to be **always loaded, never deferred**, but
`bridgePolicy.defaultDeferred()` (`bridge_memory.go:22-25`) returns `true` **unconditionally** for
every bridged tool. Something must change, and D-17 forbids it being a list of names.

**Inventory first** (`D:/tmp/mcp-go-sdk` @ `64e454e35c23`, `mcp/protocol.go`): a `Tool` carries
`Annotations` (4 hints + title), `Title`, `Icons`, `OutputSchema` and `_meta`. **The protocol has no
priority or "always load" field** — an annotation-derived rule is falsified before it is proposed.

- **D-27 — Non-deferral is earned by tool count.** A mounted server exposing **≤ 3 model-facing
  tools** (counted *after* `bridgePolicy.modelFacing`, so memory's 10 advertised count as its 4
  exposed) earns a loaded slot; more than 3 stays deferred. **Global cap: 2 always-loaded MCP slots.**
  Overflow **fails closed** — slots are granted in a deterministic order until the budget is spent and
  every further qualifying server stays deferred and `tool_search`-discoverable, which still satisfies
  SC#6 (*usable with no code change, fail-closed*). Both numbers are **code constants**, not env vars.
  - Why counting: it needs no declaration anywhere, so a self-minted or ad hoc server with one good
    tool is loaded by the same arithmetic that defers a 28-tool raw surface. A `_meta`-declared hint
    was considered — it keeps curation in the server per D-17 — but it lets any mounted third-party
    server promote itself into the manifest and would need a cap regardless.
  - Why **3** and not 1 or 4: the two curated forks expose 1 tool each; memory exposes 4, so it stays
    deferred **exactly as today** and Phase 48 — which owns the 14-slot budget — decides about memory
    explicitly instead of inheriting it here. N=1 was rejected as brittle (a fork that later splits
    one verb into two would silently fall off the cliff).
  - Why constants: no ceremony at mount time, and the `AURA_MCP_*` catalogue is already in measured
    debt (D-28). Phase 48 edits this code anyway.
  - **Constraint for the planner:** `refreshSpec` (`bridge.go:66-100`) recomputes `spec.Deferred` on
    every reconnect. If a server's tool count crosses the threshold across a reconnect, deferral would
    flip mid-conversation and the manifest would change under the KV cache. Follow the pattern already
    there — `refreshSpec` warns on a changed mutating/destructive flag and on changed required args;
    a deferral flip deserves the same treatment or a mount-time freeze.
  — **Reversibility:** reversible — two constants and one predicate.

### The amendment batch — scope, blocking split, document treatment (new, 2026-08-17)

- **D-28 — The batch covers 46 + 45.1 + the env catalog.** Beyond D-05..D-09 and TOOL-14, the same
  pass ratifies **45.1's shipped-but-un-amended changes** — the `_meta` identity cutover (a *tool
  schema* change), the idempotent/open-world escalation, the elicitation handler — and repairs the
  `AURA_MCP_*` catalogue. Measured basis: `prd.md:6150` still catalogues
  **`AURA_MCP_PING_INTERVAL_SEC`** as a live knob describing `internal/agent/mcptools/bridge_ping.go`,
  a file 45.1 deleted (its only remaining mention in the tree is a comment in
  `internal/sandbox/usersandbox/reap.go:29`); `AURA_MCP_MOUNT_TIMEOUT`, `AURA_MCP_SHUTDOWN_TIMEOUT`
  and `AURA_MCP_ELICITATION_TIMEOUT_SEC` are absent from the catalogue, and
  `AURA_MCP_CALL_TIMEOUT_SEC` appears only in amendment prose while the code reads 21 distinct
  `AURA_MCP_*` names. One pass over the same PRD section, and it stops the PRD describing a client
  that no longer exists.
- **D-29 — D-27's count rule is recorded inside TOOL-14's amendment.** TOOL-14 already changes the
  tiering axis from **size** to **frequency plus a hard count budget** (`REQUIREMENTS.md:93`(c));
  *"a server exposing ≤3 model-facing tools earns a loaded slot, capped at 2"* **is** that hard count
  budget. One statement of the axis, and Phase 48 inherits both halves from one place.
- **D-30 — Decisions block code; bookkeeping rides along.** The decision-bearing amendment (ratifying
  `34b892512`, MCP-04/05's new mechanism, TOOL-14's axis + the count rule) lands **before any code** —
  that is what CLAUDE.md's PRD-amendment-before-code exists for. The 45.1 ratification and the
  env-catalog repair record what already shipped and constrain nothing, so they land in the same phase
  without gating its first commit.
- **D-31 — REQUIREMENTS.md rows and the ROADMAP §46 section get rewritten clean, with dated footnotes.** MCP-02/04/05 currently read as struck-through prose with amendment notes appended, and
  the ROADMAP's Phase 46 section carries roughly six SUPERSEDED paragraphs. Each is replaced by its
  current text, with the superseded wording moved into a dated footnote. **This was the operator's
  call against the recommendation** to leave the strikethrough inline (the argument for leaving it:
  the falsification trail is the point, and moving it one hop away costs evidence for zero behavioural
  gain). Recorded as a decision, with the counter-argument, so a later reader does not mistake it for
  drift. **The footnotes are mandatory — this is a relocation of the history, never a deletion.**

### Landing order and drift (new, 2026-08-17)

- **D-32 — Fork publishes first; then ONE atomic Aura commit.** The bind: re-key
  `trustedRecipeActions` before the forks ship and the 28 raw tools fall through to the fail-closed
  default, so every calendar read starts demanding approval; ship the fork first without the re-key
  and the merged tool classifies flat. **D-23's `:<sha>` pin dissolves it** — the fork's new surface
  only reaches Aura when `compose.yaml` changes, so the pin, the re-keyed table, `Multiplexed` and the
  classifier entry all land in **one commit** and there is no intermediate state to keep green. A
  dual-key transition table was considered and rejected (a table that means two things at once, plus a
  cleanup commit that can be forgotten).
- **D-33 — Fork drift: loud at mount, fail-closed at call.** The action names live in another
  repository. At mount, reconcile the curated tool's `action` enum against the table and WARN-log
  every unknown action **by name**; at call time an unrecognised action saturates upward the way
  `classify` already does. A boot panic was rejected: MCP mounts are fail-soft by design, and a
  rename in another repo must not stop Aura from starting.
- **D-34 — `Multiplexed` is inferred ONLY when a classifier already exists.** Measured hazard:
  `ValidateClassifiable` **panics** on a Mutating+Multiplexed tool with no entry in
  `multiplexedClassifiers` (`guard.go:29-36`). If `Multiplexed` were inferred from any `action` enum
  in a schema, a stranger's server that happens to use an `action` argument would **stop Aura from
  booting** — the exact opposite of SC#6. So the inference is gated on the classifier's existence: no
  classifier → not multiplexed → generic fail-closed tier, which is precisely what SC#6 promises for
  an unknown server. The guard keeps its full meaning for tools Aura actually knows.
- **D-35 — `trustedRecipeActions` stays ONE table with mixed keys, documented.** Calendar and
  WhatsApp become action-keyed; memory is not merged and stays tool-keyed. The key is *"whatever
  discriminator that source's surface exposes"* and the comment must say so. `mcpToolRisk`'s single
  lookup keeps working unchanged, and no second risk source appears (D-21).

### The description budget (new, 2026-08-17 — measured, load-bearing for D-18)

Measured over `internal/agent/tools/testdata/deferred_manifest.json`:

| namespace | tools | description bytes | distinct properties |
|---|---|---|---|
| `calendar` | 14 | **4,504** | 27 |
| `whatsapp` | 14 | **7,578** | 26 |
| `memory` | 4 | 1,654 | — |

`frameMCPDescription` caps a description at **4,096 bytes**; `capSchemaDescriptions` returns an
**empty schema** above 16KB or 128 properties. So for *both* forks, a merged description that simply
carries the existing per-tool text forward is **over the cap and gets truncated** — and because these
two tools are now always-loaded, every byte is paid on **every turn**.

- **D-36 — Write the merged description to a tight budget: ~1.5–2KB.** A preamble plus one line per
  action naming its required fields; the real per-field detail lives in the schema's argument
  descriptions (each capped at 512B, and only read when the model inspects arguments). Roughly 2k
  tokens/turn for both slots instead of ~4k. Using the full 4,096 allowance was rejected on standing
  cost; raising the cap for always-loaded tools was rejected as an Aura-side special case keyed on
  tool class — the ceremony D-17 cut.

### Evidence (new, 2026-08-17)

- **D-37 — One driven conversation, plus `aura.tool_invocations` rows, closes SC#1/#2/#4.** Drive
  the real agent on the running stack through a scenario that reads a calendar, sends something that
  trips the approval gate, and follows an event from listing through to detail; quote the invocation
  rows in VALIDATION.md. This is the *"real E2E, not smoke"* rule the phase already lives under —
  a live smoke test is not a green signal.
- **D-38 — SC#6 is proven with the calculator fork.** `chetto1983/calculator-mcp-server` already has
  an integration fixture (`internal/mcp/calculator_integration_test.go`,
  `AURA_MCP_CALCULATOR_SERVER_JSON`), so mounting it live is cheap. **Caveat to state in the
  evidence:** it *is* referenced in Aura's tree, so the run must demonstrate that the mount needs no
  code change and no catalog entry — not that the server was unknown. It must land deferred (it
  exposes more than 3 tools, or fewer than 3 with the cap already spent by the two curated slots) and
  fail-closed at `Mutating+Destructive`.

### Claude's Discretion

- Exact action names and their grouping inside each fork's curated tool; the wording of the PRD
  amendments; whether the amendments land as one commit or several.
- Whether `bridge_risk.go`'s re-keying keeps the `recipeSource` map shape or flattens it, provided
  no second risk source appears (D-21, D-35).
- The deterministic order in which D-27 grants its two slots (mount order is the obvious choice).
- Where D-27's count predicate lives (`bridgePolicy`, or a mount-time computation feeding it) and
  how `refreshSpec` is prevented from flipping deferral mid-conversation.
- Fix-on-touch: `internal/agent/tools/testdata/deferred_manifest.json` still carries **64 occurrences**
  of `"untrusted MCP server description"` / `"untrusted MCP server summary data"` — re-counted
  2026-08-17. It predates `34b892512` and is stale.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase definition
- `.planning/ROADMAP.md` §"Phase 46: MCP trust and facade" — goal, requirements, success criteria.
  **Success Criteria 4 and 5 are superseded (D-07, D-09); the rationale's "generalizing bridgePolicy
  into a namespace-keyed table" is superseded by D-17. This whole section is rewritten clean by D-31.**
- `.planning/REQUIREMENTS.md` — MCP-01..05 (`:119-123`), TOOL-14 (`:93`), COMPAT-01/02/03 (`:109-111`),
  MCPC-01..05 (`:131-135`, all Complete). **MCP-02/04/05 rows are rewritten clean by D-31.**
- `.planning/PROJECT.md` — core value, constraints, and the named constraint to read the reference
  implementations before designing anything in this milestone.
- `.planning/phases/45.1-native-mcp-client/45.1-CONTEXT.md` — the client swap's own decisions
  (its D-101..D-109); `45.1-VALIDATION.md` — what was proven live, and the Nyquist per-task sampling
  still owed (`nyquist_compliant: false`).
- `.planning/phases/45-harness-correctness/45-CONTEXT.md` — D-09's boot guard is what D-21/D-34
  interact with.
- `prd.md` — truth-source; D-05's amendment lands here. `:4549` (Amendment #110, memory block),
  `:4579` (document passages stay `TrustUntrusted`; the citation-token shape D-20 mirrors),
  `:6150` (the dead `AURA_MCP_PING_INTERVAL_SEC` row, D-28), `:6378` (Amendment #121, the newest).

### Fork delivery (read before touching either sidecar)
- `docs/superpowers/specs/2026-06-16-calendar-pim-mcp-fork-design.md` — **the fork convention**:
  thin fork, no vendoring, image pin in-repo, branch `aura/pim-sidecar`, admin REST kept for the
  cockpit. §3 is the architecture D-23..D-26 extend.
- `docs/superpowers/plans/2026-06-16-aura-pim-mcp-fork.md` — how the fork was built and gated the
  first time, including the WSL .NET 10 prerequisites.
- `compose.yaml:810-835` (whatsapp sidecar) and `:990-1030` (aura-pim-mcp sidecar) — image vars,
  ports, volumes, healthchecks, and the first-boot appsettings seed. The pin lands here (D-23).
- `internal/agui/connect_pim_api.go` — the cockpit's PIM admin proxy; the admin REST surface is
  **not** MCP and is untouched by the curation.

### Research grounding
- `.planning/research/ARCHITECTURE.md` §2 — the facade analysis. **§2.3's "generalize `bridgePolicy`
  into a namespace-keyed table" is superseded by D-17**; §2.1/§2.2 remain accurate as evidence.
- `.planning/research/PITFALLS.md` §"Pitfall 2" — the two-defense analysis and the recommendation to
  scope wrapper removal to code-reviewed recipes. **Declined by operator decision (D-01).** Read it
  as the recorded counter-argument. §4 is the persisted-state hazard D-26 re-raises.
- `.planning/codebase/ARCHITECTURE.md` — component map, always-active tool set.

### Protocol (read before designing anything against MCP — CLAUDE.md documentation-first rule)
- `https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization` — OAuth 2.1,
  optional; stdio → env credentials; RFC 8707 audience binding; **no `accountId` concept** (D-20).
- `https://blog.modelcontextprotocol.io/posts/2026-07-28-release-candidate/` — the stateless core:
  SEP-2575, SEP-2567, SEP-2260, SEP-2322 (MRTR), SEP-2106 (full JSON Schema 2020-12 — the basis on
  which D-19 was re-examined and upheld), SEP-2577.
- `D:/tmp/mcp-go-sdk` — the SDK on disk at `64e454e35c23`. **`mcp/protocol.go` `Tool` and
  `ToolAnnotations` — the inventory behind D-27: there is no priority field.** Also `mcp/client.go:584`
  (`ClientSession.Wait()`), `:1131` (middleware), `mcp/mrtr.go:22-31`.

### Reference implementations (on disk, read before designing)
- `D:/tmp/hermes-agent/agent/tool_dispatch_helpers.py:538-700` — `_UNTRUSTED_TOOL_PREFIXES`,
  `_neutralize_delimiters`. **hermes fences every MCP result; Aura deliberately does not (D-01).**
- `D:/tmp/hermes-agent/tools/mcp_tool.py:543-592` (`_scan_mcp_description`, warn-never-block),
  `:1669-1758` (elicitation through approval — shipped differently in 45.1, see D-15),
  `:5832-5851` (per-server `tools.include`/`.exclude`, hermes' entire curation mechanism).
- `D:/tmp/system-prompts-and-models-of-ai-tools/Poke/Poke agent.txt:57-70` — the ID-type discipline
  D-19 carries into the fork schemas.

### Aura code the decisions bind to (re-read 2026-08-17 — these line numbers are current)
- `internal/agent/mcptools/bridge_call.go:54-66` — `newResult`, `TrustTrusted` (D-01).
- `internal/agent/mcptools/bridge.go:66-100` (`refreshSpec` — the reconnect-churn constraint in D-27),
  `:168-182` (`specFromToolDefWithPolicy` — where `Multiplexed` is never set, D-21/D-34),
  `:203-253` (`capSchemaDescriptions` — D-04, D-36), `:309-346`
  (`frameMCPSummary`/`frameMCPDescription` — D-01, and the 4,096-byte cap in D-36),
  `:399-434` (`registerBridged`, collision handling — D-04).
- `internal/agent/mcptools/bridge_risk.go:26-83` (`trustedRecipeActions`, the table D-21 re-keys),
  `:110-149` (`classifyToolRisk`: fail-closed default and 45.1's escalate-only branch — D-04),
  `:199-201` (`unsafeToRepeatBeyondAura`).
- `internal/agent/mcptools/bridge_memory.go:12-25` (`bridgePolicy`, `defaultDeferred` — D-27),
  `:27-45` (`memoryHiddenFromModel`, `modelFacing` — the counting basis), `:47-78`
  (`IdentityMetaMiddleware` — D-14 as shipped).
- `internal/mcp/middleware.go:11-98` — the `_meta.aura` namespace contract (D-14).
- `internal/mcp/sdkclient.go` — the SDK client that replaced the bespoke one.
- `internal/agent/mcptools/elicitation.go` — the shipped elicitation surface (D-15).
- `internal/agent/trust.go:38-57` (`untrustedSource`, the `TrustTrusted` short-circuit — D-01/D-02).
- `internal/gateway/guard.go:22-38` — `ValidateClassifiable`; D-21's whole argument and D-34's hazard.
- `internal/gateway/classify.go:22-34` — `multiplexedClassifiers`, the map D-21 adds to.
- `internal/mcp/manager/catalog.go` — `BuiltInCatalog`, the mounted recipes.
- `internal/mcp/calculator_integration_test.go` — the third-party server SC#6 uses (D-38).
- `internal/agent/tools/search.go` — `tool_search` over bare `Registry.All()`, no scope (D-22).

### Commit of record
- `34b892512` (2026-08-12) — *"Trust MCP output instead of framing it untrusted."* The un-amended
  change D-05 ratifies.
- `63b456f8e` / `02a291530` (2026-08-17) — Phase 45.1 close; the seam this phase now writes against.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `mcpToolRisk` + `trustedRecipeActions` (`bridge_risk.go`) — the ONLY risk source; D-21 re-keys it
  rather than adding a second.
- `multiplexedClassifiers` + `ValidateClassifiable` (`gateway/classify.go`, `guard.go`) — the shipped
  per-action gating path; D-21 routes the curated tools onto it, D-34 bounds when.
- `bridgePolicy.modelFacing` + `memoryHiddenFromModel` (`bridge_memory.go:27-45`) — already the
  authority on what "model-facing" means; D-27's count is taken *after* this filter, so it needs no
  new notion of visibility.
- `capSchemaDescriptions` / `boundedMCPError` / `refreshSpec` — bridge machinery that keeps working
  unchanged; `refreshSpec` re-syncs a bridged spec on every reconnect, which is what makes fork-side
  curation land in Aura for free (and is also D-27's churn constraint).
- `internal/mcp/ssrf.go` + `transport_ssrf.go` + `egress_policy.go` — survived the client swap as a
  custom `http.RoundTripper`; unchanged by this phase.

### Established Patterns
- **Fail closed on wiring, fail open on outages.** `Registry.Register` panics on duplicate names;
  `ValidateClassifiable` panics on missing classifiers/metadata; **MCP mounts fail soft.** D-33 and
  D-34 both sit on the seam between those two rules — a fork's drift is an *outage*, not a wiring bug.
- **Monotone saturate-upward classification** (`classify.go`): the default is the mutating floor,
  only enumerated reads de-escalate, unknown saturates to Risky. A re-keyed table must preserve this,
  and 45.1's `unsafeToRepeatBeyondAura` is escalate-only for the same reason.
- **Deferred by default for bridged tools** (`bridgePolicy.defaultDeferred`, unconditional today).
  D-27 replaces "always true" with "true unless the server is small enough" — the exception is
  arithmetic, not an entry.
- **No non-test Go file over 600 LOC.** `bridge.go` is now **482** (down from 532 across 45.1),
  `bridge_risk.go` is 202 — both have headroom for D-21/D-27.
- **`messages[0]` byte-stable**; the always-block sits at `messages[1]`. D-03 touches prompt text,
  and D-27 changes what is in the per-turn manifest.

### Integration Points
- `bridge.go` `specFromToolDefWithPolicy` — set `Multiplexed` (D-21, gated by D-34).
- `bridge_memory.go` `defaultDeferred` — the count rule (D-27).
- `bridge_risk.go` — re-key the action table, add the mount-time reconciliation (D-21, D-33, D-35).
- `gateway/classify.go` — one classifier entry per curated tool (D-21).
- `compose.yaml` — the two image pins (D-23), landing in the same commit as the above (D-32).
- The two forks — where the actual curation work happens (D-17..D-20, D-24..D-26).
- `prd.md` / `REQUIREMENTS.md` / `ROADMAP.md` — the blocking amendments and the clean rewrite
  (D-05, D-08, D-09, D-28..D-31).

### Measured hazard the planner must carry (COMPAT, cross-phase)
D-26 deletes 28 registered tool names. `COMPAT-01` (rehydrated history), `COMPAT-02` (paused
approvals) and `COMPAT-03` (scheduled `agent_job`s resolving tools at fire time) are the requirements
that cover exactly this failure — and they are assigned to **Phases 47 and 48**
(`REQUIREMENTS.md:257-259`), i.e. *after* this phase removes the names. Persisted rows in
`aura.tool_invocations`, any paused approval, and any scheduled job referencing `calendar__*` /
`whatsapp__*` will have nothing to resolve against once the pin flips. This phase does not own those
requirements and must not silently absorb them — but it must not blow them up unnoticed either.
Surface it during planning: either the merge is sequenced against them, or the phase records the
blast radius it leaves for 47/48. PITFALLS §4 is the written form of the same warning.

</code_context>

<specifics>
## Specific Ideas

- **"No ceremony."** The organising rule, stated plainly: *"if one user install one mcp why we need
  stupid safety? when i install one MCP to you there are no shit ceremony."* Every mechanism that
  would have made mounting an MCP server require a declaration — curation config, toolsets, scope
  gates, per-server trust tiers — was cut. D-27 is the same instinct applied to deferral: a server
  earns a manifest slot by being small, not by being listed.
- **"All our MCP are a fork we can modify."** The unlock that removed the Aura-side facade entirely.
  A problem solvable in the server should not become a wrapper in the host.
- **The pin is the join point.** The single most useful structural idea from the 2026-08-17 pass:
  because the image is pinned by `:<sha>`, the fork's new surface cannot reach Aura until
  `compose.yaml` says so — which turns a two-repo, two-sided cutover into one atomic commit (D-32).
- **Three roadmap/requirement claims were falsified by reading code and the spec rather than the
  document** — the description wrapper (already gone), result fencing (already gone), and
  `accountId`-as-injectable (two different things, one a handle). A fourth was falsified on the second
  pass: **D-06 and D-07 were recorded as pending work when they had already been done** the same day
  they were written.
- **Measurement changed a design constraint on the second pass, not just a fact.** The 4,096-byte
  description cap against 4,504 (calendar) and 7,578 (WhatsApp) bytes of existing text is the reason
  D-36 exists; nobody had costed the merged description before it was counted.

</specifics>

<deferred>
## Deferred Ideas

- **D-22 — `tool_search` scope enforcement.** Aura ranks over bare `Registry.All()` with no scope
  filter; any registered tool is discoverable and callable by any session, including a swarm worker.
  hermes enforces at two sites. Not a live gap for a single operator with no restricted-grant
  sessions. → revisit at **Phase 51** (durable delegation).
- **Promoting memory into the always-loaded set.** D-27's N=3 deliberately leaves memory's four
  model-facing tools deferred, exactly as today. → **Phase 48**, which owns the 14-slot budget.
- **Digest (`@sha256`) pinning for the two sidecars.** D-23 chose the `:<sha>` tag; the
  Prometheus/Tempo/Grafana rows and amendment #114's Docling pin use digests. → whenever appliance
  reproducibility is tightened as a whole.
- **A `_meta`-declared always-load hint.** Rejected in favour of counting (D-27), but it is the
  natural mechanism if a fork ever legitimately needs 4+ curated tools loaded.
- **`oneOf` per-action schemas.** Newly possible under SEP-2106; D-19 stands for now. → reopen if
  prose-enforced required-per-action contracts prove to be a real failure source.
- **Named toolsets + default-off long tail + credential-gated auto-enable** (hermes). Cut as ceremony
  for a single-operator deployment. → reopen if Aura grows multi-tenant grants.
- **hermes' `_scan_mcp_description`** — warn-only injection-pattern scan at mount. Compatible with
  trusting descriptions (it logs, never blocks). Cut as ceremony for now.
- **`validate_deferred_call_args`** — probe-validate a deferred tool's arguments before dispatch.
  → Phase 47/48.
- **Nyquist per-task validation for Phase 45.1** (`/gsd-validate-phase 45.1`) — owed, `nyquist_compliant`
  is still `false`. Not this phase's work, but it is the one open item on the seam 46 builds on.
- **Retiring the rest of `internal/mcp`'s hand-rolled surface** beyond client/transport — how much of
  `managed_config`, `classify` and `probe` the SDK's `server/discover` subsumes was never measured.

</deferred>

---

*Phase: 46-mcp-trust-and-facade*
*Context gathered: 2026-08-16; re-measured and extended 2026-08-17*
