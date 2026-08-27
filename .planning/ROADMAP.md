# Roadmap: Aura

## Overview

v2.1.0 HERMES-CLAUDE_PARITY brings the agent harness to parity with hermes-agent and
Claude Code. The organising insight, from auditing two live 2026-08-04 sessions: Aura's own
long-term memory holds nine `learned_lesson` facts, eight of which are workarounds for a
defective surface, six of which restate rules the system prompt already contains. She wrote
her own bug report. This milestone is done when those lessons become unnecessary — Phase 54
uses her memory as the oracle for that. (Corrected 2026-08-24: this sentence said Phase 53,
which was the summarization spike, not the exit gate. Phase 53 was cancelled and then deleted
2026-08-25 — PRD amendment #137.)

Phase numbering continues from v2.0.0 (which closed at Phase 44) — this milestone is Phases
45-54, **less 47, 48 and 53, which were deleted on 2026-08-25**. Of the **59** surviving v1
requirements in REQUIREMENTS.md, 58 map to exactly one of the 8 remaining phases and CTX-06 maps to
none — it was satisfied by implementation rather than by the deleted Phase 53 spike. The 23
requirements that belonged to the deleted tool-surface phases were deleted with them (PRD
amendments #134, #139). (The figure *"77 v1 requirements"* this sentence carried was already wrong
before the deletion: the document defined 82. See REQUIREMENTS.md §Coverage.)
The v2 items (CTX-V2-01, CTX-V2-02, TOOL-V2-01, TOOL-V2-02) and the Out of Scope table are not
scheduled here.

**Build order and why it isn't negotiable** (ARCHITECTURE.md section 4, traced to specific
call sites, not stylistic preference):

1. **Phase 45 (harness correctness) is foundational.** It introduces the `ReplayPolicy`
   vocabulary every later phase's tool specs need to declare correctly from day one, rather
   than retrofitting it after the fact.

1b. **Phase 45.1 (native MCP client) precedes Phase 46** — inserted 2026-08-16. Every file
   Phase 46 touches (`bridge.go`, `bridge_risk.go`, `bridge_memory.go`) sits on the MCP client
   seam, so swapping the hand-rolled client afterwards would mean writing 46's work twice. It
   also keeps a ~1,970-LOC client rewrite reviewable on its own diff, separate from a security
   decision — the same argument PITFALLS §2 makes for isolating the trust change.

2. ~~**Phase 46 (MCP trust/facade) needs Phase 45's vocabulary** — `applyMCPOperationMetadata`
   can't assign anything but the uniform default to a bridged tool without it.~~ **SUPERSEDED
   2026-08-13** (prd.md Amendment #121, and again by the 2026-08-16 discussion): the vocabulary
   was never introduced — `ReplayToolResult` remains the only value — and
   `applyMCPOperationMetadata` already fills all three fields correctly. Phase 46's real
   dependency on Phase 45 is `ValidateClassifiable`'s boot guard (D-09), which the two-tool merge
   must satisfy. The one remaining metadata-assignment site (native tool files) has no phase to
   audit it in since 47-48 were deleted; it is unaudited, and stated as such rather than assumed clean.

3. ~~**Phases 47-48 (tool-surface) need both prior phases settled**~~ — **DELETED 2026-08-25.**
   Both phases are gone. The measurement that killed them is PRD amendment #139: `tool_search`
   records **164 calls and 0 failures** against a deferred tail of 100+ tools, so TOOL-01's
   *"hard cap, not a target"* was an unmeasured assertion that the only available measurement
   contradicts. Amendment #134 separately established that TOOL-01's `comms` row cannot be built
   at all without reopening D-17 or D-27. The whole ordering argument above was about sequencing
   work that is no longer scheduled.

4. **Phase 49 (memory tiers) is a secondary track** that emerged from the operator's
   post-briefing decisions, not from the original architecture research. It depends on
   Phase 45 (the entity-resolution baseline in the same `internal/arcadedb/memory.go` code
   MEM-04/05 touch) and Phase 46 (the `bridgePolicy` generalization its new short-term/
   reasoning-tier tools reuse for hide-list/risk/replay classification).

5. **Phase 50 (context ladder) has zero package overlap with 45-49** (`internal/conversations`
   + `internal/runner` only) and could run in parallel. Sequenced last among the technical
   phases so its real-token budget (CTX-01) tunes against the *final* manifest shape. With 48
   deleted, nothing is renumbering the manifest any more — that constraint is satisfied by
   default rather than by sequencing.

6. ~~**Phase 53 (the spike) needs Phase 49's retrieval mechanism**~~ — **VOID 2026-08-24.**
   Phase 53 is **CANCELLED**: the summarization arm it was meant to evaluate was BUILT and
   shipped while the roadmap sat still. `prd.md:1190` records L3 LLM-driven compaction as
   **LANDED 2026-08-12**; `internal/conversations/compaction{,_durable,_request,_transcript}.go`
   exist, an operator `/compact` command exists, and `internal/agui/conversations_compaction_api.go`
   exposes it. A spike decides whether to build a thing; this thing is running. The block is
   deleted; PRD amendment #137 records what the cancellation does and does not settle.

7. **Phases 51-52 (delegation, steering) were added by operator decision on 2026-08-05**, after
   reading hermes' delegation against Aura's. Both implement designs that already exist in the
   repo and were never built: the durable swarm-messaging substrate (approved) and the
   mid-turn steering study.
   **REVISED 2026-08-24 — the order is now steering FIRST, and this is measured, not preferred.**
   The stated reason for delegation-first was that steering should attach to one run model
   rather than two. But the substrate does not exist — the design doc is 26 KB of paper and
   there is no mailbox, relay or claimable-task code anywhere in the tree — so today there IS
   only one run model, and steering attaches to it cleanly. Building 51 first would CREATE the
   second model that the original ordering was worried about. Meanwhile Phase 52 is
   plan-ready and Phase 51 is not: every prerequisite the steering study names was verified
   present (amendment #132), while Phase 51's own rationale still carries an unresolved
   inventory question — whether background delegation is a new execution path or a second
   caller of `agent_job`/AG-UI run-detach — which must be answered before it can be planned.

8. **Phase 54 (milestone exit) depends on everything** — it retires the compensating lessons
   only once the defects they compensate for are actually fixed, and validates the whole
   milestone by replay, not by a green test suite (ACC-01).

**On evidence:** every phase's success criteria below are checked by having a real
conversation with Aura against the live stack and reading OpenTelemetry traces,
`aura.tool_invocations`, `aura.conversation_turns`, and `aura.context_rot_events` — never by a
green test suite (ACC-01, ACC-02, established as day-one policy in Phase 45). No new eval
harness is built; `internal/eval/` stays deleted.

## Phases

**Phase Numbering:**

- Continues from v2.0.0's Phase 44. This milestone is Phases 45-54.
- Decimal phases (45.1, 45.2, ...) would be urgent insertions between these, if needed.

- [x] **Phase 45: Harness correctness** - Idempotency replay fix and memory-write guardrails close the two headline audit defects (completed 2026-08-15)
- [x] **Phase 45.1: Native MCP client** - The official Go SDK replaces ~1,970 LOC of bespoke transport and reconnect; Aura's own policy layers survive on top (inserted 2026-08-16)
- [x] **Phase 46: MCP trust and facade** - Ratify the trust posture that already shipped; curation moves into the forks, not into Aura — **closed 2026-08-25: 7 plans executed, 46-08 a recorded no-go (amendment #131), 46-09 closed by the operator**
- [ ] **Phase 49: Memory tiers** - Short-term searchable retrieval and a PRD-amendment-gated reasoning tier — **scoped down: `memory_recall` and `internal/reasoningtrace` already exist**
- [ ] **Phase 50: Context ladder legibility** - Real token accounting, eviction, and per-category visibility — **re-hosted: the consumer is now the compaction trigger**
- [ ] **Phase 51: Durable delegation** - The approved swarm substrate gets built; workers get a real brief, real limits, and a turn that no longer blocks — design gate closed 2026-08-27 (PRD #154); 8 plans in 5 waves
- [ ] **Phase 52: Mid-turn steering** - The operator can type into a running turn and redirect it at the next round boundary — **8/8 plans executed, Gate 3 live E2E scored 9.0/10 (2026-08-26): SC#1-#4 and RESUME-01 fully live-proven (backend + browser), SC#5's Telegram leg not live-proven this session (structural: no scriptable Telegram session) — see `52-VALIDATION.md`. Does not close per CLAUDE.md's >9.8 bar; needs one human Telegram check.**
- [ ] **Phase 54: Milestone exit** - Retire the nine compensating `learned_lesson` facts and validate parity live — **narrowed 2026-08-25: the `always-deliver-files` skill STAYS, because AUTO-01 was deleted with Phase 47**

## Phase Details

### Phase 45: Harness correctness

**Goal**: Aura's harness never reports a tool result it did not produce this call, and a
memory correction touches exactly the fact it names — the two headline defects the
2026-08-04 audit found are closed at the root, not patched at the symptom.
**Depends on**: Nothing (first phase of this milestone)
**Requirements**: HARN-01, HARN-02, HARN-03, HARN-04, HARN-06, HARN-07, HARN-08, HARN-09, MEM-04, MEM-05, ACC-01, ACC-02
**Rationale**: Foundational and lowest-risk (ARCHITECTURE.md §1/§4) — touches only
`tools.Spec`, `idempotency_operation.go`, `reserve.go`, and `internal/arcadedb/memory.go`; no
new tools, no new packages. **Corrected 2026-08-13 (measured against commit `09f91a865`,
prd.md Amendment #121): this phase does NOT introduce a new `ReplayPolicy` value.**
`ReplayToolResult` remains the only value; the discriminator is a `RoundOrdinal` field added
to the child operation key's `FingerprintTyped` struct in `deriveToolOperationContext`, and
the round-ordinal mechanism it depends on already exists in `internal/agent/model_round.go`.

**The open `tool_call_id` question is answered, and the answer is "do not key on it"**
(hermes `agent/message_sanitization.py:536-566`, `run_agent.py:4601-4648`). Models and
providers reuse one call id across different calls in a single batch — observed on Kimi
Responses replays, Ollama-compatible endpoints, and degraded models at long context — and
strict providers, **DeepSeek among them and DeepSeek is Aura's default**, reject duplicate ids
outright. Hermes derives a deterministic id from `(fn_name, arguments, index)` when the API
omits one, and repairs collisions with an `<id>_d<n>` suffix, never a UUID, because random ids
break prompt-cache prefix stability. Uniqueness is a property the harness must ENFORCE, not one
it can key at-most-once on.

**The discriminator is the round boundary.** Hermes drops identical `(name, arguments)` pairs
*within one assistant message* (a model error) but lets identical calls in *different rounds*
both execute (a deliberate re-issue). That is exactly the distinction Aura collapses: audit
turns `[058]` and `[062]` were separate assistant messages with an `fs_write` between them, and
the second was served from the operation registry. The evidenced fix direction is therefore a
per-turn ROUND ORDINAL in the child operation key — deterministic, reproducible across a
replayed dispatch (so the CLI/scheduler at-most-once protection the architecture research
worried about survives), and discriminating in exactly the place hermes discriminates. Confirm
it against Aura's own dispatch loop before building; do NOT adopt `tool_call_id`.

HARN-08 and HARN-09 land here because both are the same seam. Ship the `replayedMarker` (mirroring `resultExpiredMarker`)
regardless of which direction is chosen — it is the cheapest, highest-leverage fix in the
whole milestone (it directly targets the misdiagnosis Aura wrote into her own memory as
fact). For HARN-04, prefer an explicit-fact-identifier path over a pure count-threshold
refusal (Pitfall 6) — a guardrail that only checks match count still misses the
subject-mismatch variant of the same silent-data-loss symptom. MEM-04 and MEM-05 are bundled
here, not with the later Memory tiers phase, because they touch the identical entity/fact
upsert logic in `internal/arcadedb/memory.go` the F-2 guardrail touches, and because
entity-resolution correctness is a precondition for HARN-04's guarantee to mean anything — if
the subject can't be reliably identified, "exactly the fact it names" can't be reliably kept.
ACC-01/ACC-02 are established here, in the first phase, because every subsequent phase's
validation depends on this methodology already being lived practice, not policy text.
**Success Criteria** (what must be TRUE):

  1. In a live conversation, asking Aura to re-run the same mutating command twice in one turn (after changing the world in between) produces two distinct executions in `aura.tool_invocations`, not one recorded replay served twice.
  2. A genuinely retried dispatch (e.g., a scheduler restart reclaiming the same run) still shows exactly one real execution for that operation in `aura.tool_invocations` — no duplicated side effect.
  3. When a call is legitimately replayed, the tool result surfaced to the model carries a visible replay marker, observable in the transcript / OTel span attributes.
  4. Asking Aura to correct one fact among several sharing the same subject and predicate leaves the sibling facts valid — inspecting the ArcadeDB graph afterward shows only the named fact's validity window closed.
  5. Across this scenario, Aura's replies are in the operator's language, no raw deliberation leaks into user-facing text, and every stated intention either ran or the turn says plainly it didn't.

**Plans**: 9/9 plans executed in 5 waves

- [x] 45-09-PLAN.md

- [x] 45-01-PLAN.md — BLOCKING D-08 amendments (ROADMAP §45/§46 + prd.md) and the two fix-on-touch doc corrections *(wave 1)*
- [x] 45-02-PLAN.md — TRACER: `RoundOrdinal` in the child operation key, fail-closed, proved with SQL against `aura.tool_invocations` *(wave 2)*
- [x] 45-03-PLAN.md — `replayedMarker` on both replay layers, OTel replay attributes, boot-time operation-metadata guard *(wave 3)*
- [x] 45-04-PLAN.md — deterministic tool-call-id repair and same-message `(name, args)` dedup *(wave 3)*
- [x] 45-05-PLAN.md — completion critic on every voluntary termination, veto budget 2, reply-discipline prompt rule *(wave 3)*
- [x] 45-06-PLAN.md — `fact_key` exact-match supersede, ambiguity refusal, prose guard on the object endpoint *(wave 3)*
- [x] 45-07-PLAN.md — MCP surface: `supersedes_fact_key`, refusal payload, operator-subject canonicalization *(wave 4)*
- [x] 45-08-PLAN.md — live scenario scored >9.8, full gate matrix, quality-snapshot re-attestation *(wave 5)*

### Phase 45.1: Native MCP client

**Inserted 2026-08-16** by operator decision (*"use mcp client native no bespoke"*), during the
Phase 46 discussion. See `.planning/phases/46-mcp-trust-and-facade/46-CONTEXT.md` D-10..D-16 for
the decisions and the inventory behind them.

**Goal**: Aura speaks MCP through the official Go SDK client instead of a hand-rolled one. The
bespoke JSON-RPC/stdio client, the streamable-HTTP client, and the reconnect/keepalive layer are
deleted; Aura's own policy — SSRF resolve-then-pin, egress allowlist, trust classification,
managed-config and per-identity overlay — is preserved on top of the SDK rather than reimplemented
beside it.
**Depends on**: Phase 45
**Requirements**: MCPC-01, MCPC-02, MCPC-03, MCPC-04, MCPC-05 (added to REQUIREMENTS.md § Native MCP client, 2026-08-16)
**Blocks**: Phase 46 — every file Phase 46 touches (`bridge.go`, `bridge_risk.go`,
`bridge_memory.go`) sits on this client seam, so landing 46 first would mean writing its work twice.

**Rationale**: `github.com/modelcontextprotocol/go-sdk v1.7.0` is **already a dependency**
(`go.mod:27`) but is used **server-side only** (`cmd/arcadedb-mcp/*`). Measured 2026-08-16:

- **Deleted, ≈1,970 LOC non-test plus ≈900 LOC of tests** — `internal/mcp/client.go` (583),
  `http_client.go` (426), `internal/agent/mcptools/bridge_reconnect.go` (481), `bridge_ping.go`
  (113), and `transport.go`/`protocol.go`/`tool_methods.go`/`lifecycle.go` (364; ~~`probe.go`~~ — see
  amendment). ~~The SDK ships equivalents: `ClientOptions.KeepAlive` + `KeepAliveFailureThreshold`
  (`mcp/client.go:199-206`) and automatic reconnect with exponential backoff, `Last-Event-ID` resume
  and SEP-1699 server-initiated reconnect (`mcp/streamable_client.go:122-139`).~~
  **Amended 2026-08-16 — the replacement mechanism was falsified by reading the pinned SDK source
  (`45.1-RESEARCH.md` § SDK liveness surface; the citations above were doc comments and design notes,
  not shipped behaviour).** What the SDK actually provides: **`ClientSession.Wait()`**
  (`client.go:584`) blocks until the connection closes and returns why — a **push** death signal that
  removes the need for any Aura poll loop, which is what `bridge_ping.go` really deletes into.
  `ClientOptions.KeepAlive` is **inert against a 2026-07-28 peer**: not version-gated client-side
  (`client.go:403`), refused by the SDK server with `CodeMethodNotFound` (`server.go:1879-1887`), and
  silently self-retiring (`shared.go:869-872`) — configured-looking, dead in practice. The
  "automatic reconnect with backoff" is the **standalone SSE stream**, unconditionally removed under
  2026-07-28 (`streamable.go:2095-2098`); recovery works because every call is a fresh HTTP POST.
  `StreamableClientTransport.MaxRetries` (default 5) is the real HTTP retry knob.
  **`probe.go` is re-pointed, not deleted** — a use-case function over the SDK, not a facade over its
  types, with 13 `cmd/aura` diagnostic files depending on it.

- **Preserved, ≈1,330 LOC with no SDK equivalent** — `ssrf.go` + `transport_ssrf.go` +
  `egress_policy.go` (re-attached as a custom `http.RoundTripper`), `managed_config.go` +
  `managed_config_identity.go`, `classify.go`, `tool_error.go`, `domain_outcome.go`,
  `observability.go`, `redact.go`.

- **The clock**: Aura's client implements the session model (`sessionGate`) that the 2026-07-28
  spec **deletes** — SEP-2575 removes `initialize`/`initialized`, SEP-2567 removes `Mcp-Session-Id`,
  and client info moves to `_meta` on every request. **The pinned v1.7.0 already ships that core**
  (`latestProtocolVersion = "2026-07-28"`, `mcp/shared.go:50-51`) and negotiates down five versions,
  so this needs **no dependency bump and no sidecar work** — the three sidecars stay where they are.

**Scope decisions carried from the Phase 46 discussion:**

- Middleware is the seam. `Client.AddSendingMiddleware` (`mcp/client.go:1131`) carries host-side
  argument derivation; `withMemoryUserIdentifier` moves onto it. This is the SDK's own idiom — MRTR
  itself ships as default-on client middleware (`mcp/mrtr.go:22-31`), not an adaptation.

- `cmd/arcadedb-mcp` moves off the `user_identifier` **argument** onto stateless `_meta`, since Aura
  owns both ends. Third-party forks keep argument-shaped inputs. **This changes the memory tool's
  schema — PITFALLS §4 applies** (rehydrated history, paused approvals, scheduled jobs).

- Elicitation (SEP-2322 MRTR) routes through Aura's existing approval path with a bounded timeout,
  naming the asking server — hermes parity (`tools/mcp_tool.py:1669-1758`). ~~The SDK auto-fulfills
  input requests by default and Aura registers no handler today.~~ **Amended 2026-08-16 — falsified
  by measurement.** With no `ElicitationHandler` the SDK returns
  `&jsonrpc.Error{Code: CodeInvalidParams, Message: "client does not support elicitation"}`
  (`mcp/client.go:862-864`, v1.7.0) — it fails **closed**, it does not auto-fulfill. Aura is
  therefore already fail-closed and the handler closes no hole; the real justification is SEP-2322
  compliance and operator visibility (today a server needing input gets a flat refusal nobody sees,
  so a legitimate multi-round-trip tool cannot work at all).

**Success Criteria** (what must be TRUE):

1. A live conversation drives calendar, WhatsApp and memory tools through the SDK client, and
   `aura.tool_invocations` shows the same tool surface and results as before the swap.

2. `internal/mcp/client.go`, `http_client.go`, `bridge_reconnect.go` and `bridge_ping.go` are gone
   from the tree, and no hand-rolled JSON-RPC framing or reconnect loop replaces them elsewhere.

3. Killing a sidecar mid-conversation and restarting it recovers on the SDK's own reconnect, live —
   not on a reimplementation.

4. An SSRF/egress probe against a mounted HTTP server is still refused, proving the policy layers
   survived re-attachment to the SDK transport.

5. A live turn shows `user_identifier` reaching `cmd/arcadedb-mcp` through `_meta` and no longer as
   a tool argument.

**Plans**: 7/8 plans executed in 6 waves

Plans:
**Wave 1**

- [x] 45.1-01-PLAN.md — TRACER: SDK session construction (both transports) + SSRF/egress/redirect policy re-attached + probe.go re-pointed *(wave 1)*

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 45.1-02-PLAN.md — mcptools seam on *ClientSession: result-decode re-plumb, `MountedServer` supervisor on `Wait()`, bridge_reconnect.go + bridge_ping.go deleted *(wave 2)*

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 45.1-03-PLAN.md — 16 cmd/aura consumers re-pointed, bespoke client deleted, anti-dark-code guard, coverage floor restored *(wave 3)*
- [x] 45.1-04-PLAN.md — D-107: all four SDK tool annotations, escalate-only proven over every hint combination *(wave 3)*

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 45.1-05-PLAN.md — `_meta` identity cutover, server-side fail-closed, and the three PITFALLS §4 blast radii disposed *(wave 4)*
- [x] 45.1-06-PLAN.md — checkpoint: which surface a server-initiated elicitation reaches the operator on *(wave 4)*

**Wave 5** *(blocked on Wave 4 completion)*

- [x] 45.1-07-PLAN.md — bounded fail-closed elicitation handler + the chosen consent surface *(wave 5)*

**Wave 6** *(blocked on Wave 5 completion)*

- [x] 45.1-08-PLAN.md — live SC#1-5 scored >9.8, full gate matrix, amendments, quality-snapshot re-attestation *(wave 6)*

### Phase 46: MCP trust and facade

> **REVISED 2026-08-24 (measured on the running stack).** 7 of 9 plans are complete. **Plan
> 46-08's fork curation is a recorded NO-GO**, not pending work — PRD amendment #131: the
> WhatsApp view calls back by raw name into THREE tools (`list_chats`, `list_messages`,
> `get_media_data`), not two, so a curated WhatsApp lands at 4 model-facing tools, over D-27's
> ceiling, and stays deferred exactly as it is deferred today. The manifest collapse the curation
> was for saves nothing that is being paid. **Only plan 46-09 remains.** The design doc's §5b
> exemption table and `ui://` bindings are stale and amendment #131 supersedes them.

See `.planning/phases/46-mcp-trust-and-facade/46-CONTEXT.md` for the decisions and their rationale.

**Goal**: The trust posture that already shipped is ratified in the PRD rather than re-implemented;
every mounted MCP server — bundled recipe, ad hoc mount, or one Aura mints herself — works with
**zero Aura-side code and zero declaration**; and calendar/WhatsApp collapse from raw tools into
**two always-loaded curated slots** — one multiplexed tool per sidecar — curated inside the forks,
not behind a facade in Aura's tree.

**Depends on**: **Phase 45.1** (native MCP client) — complete (`63b456f8e`/`02a291530`), landed
first so this phase's remaining work is written once against the surviving seam. And Phase 45's
`ValidateClassifiable` boot guard (D-09), which the two-tool merge must satisfy per D-21
([earlier dependency narrowing](#fn-46-a)).
**Requirements**: MCP-01, MCP-02, MCP-03, MCP-04, MCP-05, TOOL-14
**Rationale**: The trust half already shipped in `34b892512` (2026-08-12, *"Trust MCP output
instead of framing it untrusted"*) and needs only an amendment — ratified in `prd.md` Amendment
#122; the facade half moved into the forks (D-17): every MCP server Aura ships is a fork Aura
controls, so curation belongs in the server, not behind an Aura-side namespace table
([earlier same-change framing](#fn-46-b)). ARCHITECTURE.md §2.3's Go-table recommendation no
longer applies; §2.1/§2.2 remain accurate as evidence. **Operator decision, 2026-08-05:** wrapper
removal is UNCONDITIONAL — every mounted server, present and future, including ad hoc mounts and
any MCP server Aura mints through self-extension. The residual prompt-injection risk is carried
entirely by the surviving guardrails ([earlier framing](#fn-46-c)) — `mcpToolRisk`'s fail-closed
default (`bridge_risk.go:112-118`, unannotated → `true, true`); the model-blind approval gate
(`approve.go` — no tool schema exposes it); bridge namespacing plus `Registry.Register`'s
panic-on-duplicate; and `capSchemaDescriptions`' byte caps ([earlier verification
instruction](#fn-46-d)) — and by the operator's control over what gets mounted. Non-MCP sources
keep their envelopes unchanged: `web_fetch`, `document_search`/`document_open` (required by
`prd.md:4579`), user attachments, swarm child output. Independent of the context-ladder work
(Phase 50) — no package overlap.

**Load-bearing consequence of the two-tool merge (D-21, measured).** A bridged tool never sets
`Multiplexed`, so `ValidateClassifiable`'s per-action assertion (`guard.go:28`) skips it and
`classify` assigns **one flat tier to the whole merged tool** — `calendar(action=list_events)` and
`calendar(action=send_email)` would classify identically, so either every read demands approval or
`send_email` is un-gated and **Success Criterion 2 fails**. No panic warns you: the guard only fires
when `Multiplexed` is already true. Fix with existing machinery and no second risk source — re-key
`trustedRecipeActions` (`bridge_risk.go:23-80`) from raw tool name to action name, set
`Multiplexed: true` when a curated schema carries an `action` enum, and register the classifier in
`multiplexedClassifiers`. Do **not** derive tiers from server-declared annotations:
`explicitDestructive` is deliberately escalate-only.

**TOOL-14 lands here.** Tool tiering is PRD-declared, not an
implementation detail: `prd.md:154` states the rule (amended by `prd.md` Amendment #123), each
slice carries a "Deferred-tool partition", and two named amendments fix specific tools' tiers — A4
makes `read_tool_output` non-deferred (which TOOL-13 changes) and #44 makes `sandbox_exec`
non-deferred with live evidence. Phase 46 is the earliest consumer because MCP-04 changes which MCP
tools exist at all, so the amendment gates this phase. (It also gated Phase 48, deleted 2026-08-25.) Amendment #123's count
rule (`<=3` model-facing tools/server earns an always-loaded slot, global cap 2) is the arithmetic
Phase 46 actually spends: under the operator's `views-exempt` selection
(`.planning/phases/46-mcp-trust-and-facade/46-02-SUMMARY.md`), calendar exposes **1** curated tool
and WhatsApp exposes **3** (1 curated + 2 exempted view-bound reads, `list_chats` and
`list_messages`) — both qualify under `<=3`, and WhatsApp has zero headroom left for a future
addition. Under the rejected `views-drop` alternative the count would have been 1 each.

**The MCP-trust question is settled, by reading, as instructed.** Nothing in the PRD ever
established description wrapping for MCP output, so MCP-01/MCP-03 needed no amendment on that
count; the live doctrine that `34b892512` actually contradicted was **Amendment #110**
(`prd.md:4549`), which specified the injected memory block as *"a non-persisted, **untrusted**
reference item."* That header was dropped without an amendment, so **Amendment #110 is now amended,
not restored** (`prd.md` Amendment #122(c)) — Aura's recalled facts are her own knowledge, and
framing them as hostile makes her distrust her own memory. `prd.md:4579` is untouched: document
passages remain `TrustUntrusted`, so instructions inside a document stay data, not authority.

**Success Criteria** (what must be TRUE):

  1. In a live conversation, the tool manifest shows calendar and WhatsApp reachable through **two always-loaded curated slots**, not the raw 28 underlying MCP tools — and the curation is visible in the forks' own `tools/list`, with no Aura-side hide-list doing it.
  2. A calendar or WhatsApp destructive action (send a message, send an email) produces the fail-closed risk gate / approval flow live, **while a read action in the same merged tool does not** — proving per-action classification survived the merge (see D-21).
  3. Reading the rendered tool descriptions in a live turn shows every mounted MCP server — bundled recipes and any ad hoc mount alike — presented as ordinary text, with no untrusted-data framing anywhere.
  4. `accountId` never appears in the model's dispatched arguments for a live calendar call — `aura.tool_invocations` shows the model passing back only an opaque reference the fork itself issued. ([earlier rewording note](#fn-46-e))
  5. ~~A live turn whose MCP tool result carries instruction-shaped text does not act on it, proving the result-fencing envelope carried the defense.~~ **DELETED 2026-08-16 (D-07):** there is no envelope; the criterion cannot pass as written and must not be silently reinterpreted.
  6. Mounting a **new** MCP server — one with no entry anywhere in Aura's tree — makes its tools usable in a live turn with no code change and no configuration beyond the mount itself, fail-closed at `Mutating+Destructive`.

**Plans**: 7/9 plans executed

Plans:
**Wave 1**

- [x] 46-01-PLAN.md — prd.md amendment batch: ratify 34b892512, MCP-04/05's new mechanism, TOOL-14's tiering axis + count budget, 45.1 ratification + AURA_MCP_* catalogue repair (BLOCKING, docs only)
- [x] 46-02-PLAN.md — curated-surface design doc (the fork contract) + the one-way operator decision on the WhatsApp action scope and the curated tool names

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 46-03-PLAN.md — REQUIREMENTS.md MCP-02/04/05 rows and ROADMAP §46 rewritten clean, superseded wording relocated to dated footnotes (D-31)
- [x] 46-04-PLAN.md — D-27's deferral count rule: <=3 model-facing tools earns a slot, global cap 2, frozen at mount, drift warned on reconnect
- [x] 46-05-PLAN.md — calendar fork (aura-pim-mcp) curated into one multiplexed tool, accountId handle fixed, immutable :<sha> image published

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 46-06-PLAN.md — TRACER: re-key the risk table to action names, gate Multiplexed on classifier existence, register the classifier, reconcile at mount, pin the image — one atomic commit

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 46-07-PLAN.md — TRACER GATE: one driven conversation proving SC#1/#2/#4 for calendar live, evidence quoted from aura.tool_invocations, scored >9.8

**Wave 5** *(blocked on Wave 4 completion)*

- [ ] 46-08-PLAN.md — WhatsApp fork curated + its table re-key, classifier and pin in the second atomic commit

**Wave 6** *(blocked on Wave 5 completion)*

- [ ] 46-09-PLAN.md — SC#6 via the calculator mount, trust-posture tripwire tests, tool-search fixture repair, and the phase-close gates

#### Footnotes — superseded Phase 46 wording

<a name="fn-46-a"></a> **Depends on, superseded 2026-08-16:** "The remaining dependency on Phase 45 is the risk-override and hide-list work only."

<a name="fn-46-b"></a> **Rationale, superseded 2026-08-16:** "Trust-wrapper scoping and facade groundwork are the same code change (generalizing `bridgePolicy` into a namespace-keyed table), so they land together (ARCHITECTURE.md §2)."

<a name="fn-46-c"></a> **Rationale, superseded 2026-08-16:** "MCP-02's two independent guardrails"

<a name="fn-46-d"></a> **Rationale, superseded 2026-08-16:** "Verify explicitly that `trust.go`'s nonce-wrapped result-fencing and `mcpToolRisk`'s fail-closed classification are untouched by this change (MCP-02)."

<a name="fn-46-e"></a> **Success Criterion 4, reworded 2026-08-16 (D-20):** the original *"host-injected, exactly like `user_identifier`"* is superseded — injection cannot work for the handle case.

### Phase 49: Memory tiers

> **REVISED 2026-08-24 — scope narrowed by measurement.** Parts of this phase already exist:
> `memory_recall` is served by `cmd/arcadedb-mcp` alongside `memory_search`, `memory_facts_about`,
> `memory_digest` and `memory_upsert_fact`; `internal/reasoningtrace` exists at ~838 LOC; and the
> recall-injection seam is wired (`internal/runner/runner_context.go:16`). What remains genuinely
> open is MEM-01's short-term searchable tier, the MEM-06 amendment gate, and the boundary that
> keeps reasoning content out of any summarizer or fact extraction. **Plan against what is there,
> not against the original blank slate.**

**Goal**: Aura's memory grows a searchable short-term tier and a reasoning tier that only
enters context on demand — gated by a PRD amendment committed before any of the
reasoning-tier code, so the boundary between scratch-work and durable fact is a decision on
record, not an implementation detail.
**Depends on**: Phase 45 (the entity-resolution baseline MEM-04/05 already fixed in the same `internal/arcadedb/memory.go` code), Phase 46 (the `bridgePolicy` generalization this phase's new short-term/reasoning-tier tools reuse for hide-list/risk/replay classification).
**Requirements**: MEM-01, MEM-02, MEM-03, MEM-06, TOOL-05, AUTO-03, CTX-05, HARN-05
**Rationale**: MEM-06 gates MEM-03 — the PRD amendment extending amendment #91 (reasoning
persisted to the graph, retrieved only on demand, never summarized or harvested) is its own
committed step, landing before any commit that touches reasoning-tier implementation, per
CLAUDE.md's PRD-amendment-before-code rule. A short-term memory tier in ArcadeDB is settled,
operator-decided scope — Postgres stays the system of
record for turns, ArcadeDB gets a derived, searchable projection (MEM-01). TOOL-05 lands here
because it's the model-facing shape of this exact mechanism (MEM-02's unified retrieval call) —
its flatten siblings were deleted with Phase 48 on 2026-08-25, so this is now the only place it
could land. HARN-05 (atomic multi-op memory write) lands here because it needs the same ArcadeDB memory-bridge
atomic-commit path this phase's other work touches — net-new transactional semantics, not a
schema widening. AUTO-03 and CTX-05 are the same boundary this phase must enforce now that
a reasoning tier exists: a durable fact is captured as part of doing the work, and reasoning
content never reaches a summarizer or fact extraction — hermes' own failure mode (a
speculative reasoning conclusion preserved as a fact) is one Aura's own audited session
already exhibited once.
**Success Criteria** (what must be TRUE):

  1. `git log` shows the PRD amendment extending #91 committed as its own commit, dated before any commit touching the reasoning-tier implementation.
  2. A live question about something said several turns back — past what the deterministic ladder still holds in context — is answered correctly via one `memory_recall` call spanning recent conversation and long-term facts; the tool result / OTel span shows which retrieval path (graph traversal or hybrid search) was actually used.
  3. After a turn involving extended reasoning, the ArcadeDB graph shows the reasoning trace persisted with edges to the entities it touched, and a later turn's injected context does NOT include that reasoning content unless explicitly retrieved.
  4. A durable fact revealed mid-task (stated during a live shell/file task) is captured as a memory fact by the time the task completes — checking its recorded provenance shows it was captured directly, never sourced from a reasoning-trace summarizer.

**Plans**: TBD

### Phase 50: Context ladder legibility

> **REVISED 2026-08-24 — the diagnosis holds, the target moved.** CTX-01's premise is still true:
> the real token count is persisted and NOT used for the decision. `LastInputTokens` is read only
> by `internal/conversations/store_identity.go:49` (the cockpit gauge), while the trigger still
> computes with tiktoken — `compaction.go:10`, `compaction_durable.go:10`, `compaction_request.go:83`.
> **But the consumer is no longer "the ladder": it is the COMPACTION trigger, which was built
> after this phase was written.** Point CTX-01 there. Also: this block's rationale says
> `context.go` is "at 590/600 LOC" — it is now **516**, already split into
> `context_budget.go`, `context_repeat.go`, `context_rot.go`, `context_tail.go` and
> `context_tool_names.go`, so the sibling-file constraint is satisfied rather than pending.

**Goal**: The context ladder's existing deterministic machinery becomes legible and
accurate — the eviction/budget decisions the model is already subject to are now visible to
the operator and correct against the provider's real token count.
**Depends on**: Nothing structurally (zero package overlap with Phases 45-49: touches only `internal/conversations` + `internal/runner`). Its former sequencing dependency on Phase 48 is void — that phase was deleted 2026-08-25, so the tool-surface shape CTX-01's real-token budget tunes against is already final.
**Requirements**: CTX-01, CTX-02, CTX-03, CTX-04, CTX-07, CTX-08, TOOL-13, SURF-04
**Rationale**: `internal/conversations/context.go` is at 590/600 LOC — new work lands in
sibling files (`context_summary.go`/`context_breakdown.go`), never appended to it. The
cheapest, highest-value item in the whole milestone lives here: the real `prompt_tokens`
count is already persisted every turn (`AppendTurnParams.InputTokens`) and already queryable
— it's used today only for the cockpit's display gauge, never fed into the ladder's own
budget decision (CTX-01). Order matters within this phase (CONTEXT-MANAGEMENT.md's own
stated order): evict superseded `tool_search` results first (CTX-02), then wire the real
token budget (CTX-01), then the ghost-skill marker (CTX-03) — about 30 lines, independent of
any LLM rung — then the per-category breakdown (CTX-04, a new sibling file, categories
reflecting Aura's actual manifest shape, not hermes' 8 categories copied verbatim). SURF-04
(model can tell memory was injected, dropped tool still callable) is bundled here as the
same class of legibility signal as the ghost-skill marker. CTX-07 (stated reason when
context can't be reduced) closes the ladder's failure-mode legibility gap.
**Success Criteria** (what must be TRUE):

  1. In a live long conversation, the context-budget trigger visibly uses the real reported `prompt_tokens` from the last provider response (inspectable via the ladder's logged trigger value / `aura.context_rot_events`), not a tiktoken estimate.
  2. A `tool_search` result whose schema was reloaded later is evicted from context on a subsequent turn — verified by inspecting what's actually sent on the wire that turn.
  3. A skill pruned from context by the ladder leaves a visible marker in a later turn, and Aura reloads it via `skill` rather than acting as if it's still loaded.
  4. Asking the operator-facing diagnostic (or the operator inspecting a live turn) what's consuming the context window shows a breakdown by category — tools, memory, skills, history — not just a single fullness percentage.
  5. When context is over threshold and cannot be reduced further, the turn states the reason rather than failing silently or truncating without explanation.

**Plans**: TBD

### Phase 51: Durable delegation

> **DESIGN GATE CLOSED 2026-08-27 — PLANNED.** PRD Amendment #154 (`prd.md:8504-8585`)
> closed the gate with four live spikes (`.planning/spikes/098`, `099`, `100`, `101`): the durable
> substrate is a generalization of `aura.ingestion_jobs` (no new table), `agent_message_send` and
> the 4-table messaging schema are OUT (the steer rail plus the shipped outbox suffice), and worker
> termination reaps on inactivity — the nominal 120s child timeout is measured at ~240s effective.
> The banner below is the pre-gate text, kept for provenance; it no longer blocks planning.
>
> **SUPERSEDED — REVISED 2026-08-24 — NOT plan-ready, and now runs AFTER Phase 52.** Two measured reasons.
> First, the substrate is entirely on paper: the approved design doc exists (26 KB) but there is
> **no mailbox, relay or claimable-task code anywhere in the tree** — `swarm_spawn.go` is a
> 112-line tool delegating to an injected runner. Second, this block's own rationale still carries
> an unresolved inventory question — *"establish whether background delegation is a new execution
> path or a second caller of [`agent_job` / AG-UI run-detach]"* — and CLAUDE.md forbids designing
> past it. **Add a design gate (spike or AI-SPEC) that answers that question before /gsd-plan-phase
> is run against this phase.** Its declared dependencies on 47/48/49 were re-examined and are all
> SOFT, and 47/48 no longer exist at all (deleted 2026-08-25): the worker registry is DERIVED
> (`Without(reg, "swarm_spawn")`, `swarm_context.go:20`), SWARM-07's concurrency surface exists
> against today's memory, and SWARM-06's build-it-twice risk is gone with the approval rework that
> caused it — the relay now rides the `ask_user` shape that is already shipped and staying.
> **Only Phase 49 remains as a real dependency.**
>
> **Reference reading (do this before designing, not after):** `hermes-agent` has already built
> this and hardened it twice in one evening (`a94ebf5f5` steer lifecycle ownership, `9d4ef04ed`
> bind steering to session generation). `LibreChat`'s `GenerationJobManager` (1,985 LOC) is a
> durable, multi-replica answer to the same problem, with approval expiry and cross-replica abort.

**Goal**: A delegated worker gets a brief worth acting on and limits it can see, a worker can
orchestrate workers of its own, and a top-level delegation stops holding the operator's turn
hostage — results re-enter the conversation when the work is actually done.
**Depends on**: Phase 49 (SWARM-07 needs the memory and reasoning tiers to exist before deciding what concurrent workers may write into them). The former dependencies on Phases 47 and 48 died with those phases on 2026-08-25 — SWARM-08 now verifies workers against a tool surface nobody is renumbering, and SWARM-06's relay rides the shipped `ask_user` shape rather than a reworked one.
**Requirements**: SWARM-01, SWARM-02, SWARM-03, SWARM-04, SWARM-05, SWARM-06, SWARM-07, SWARM-08, SWARM-09, SWARM-10, SWARM-11
**Rationale**: Operator decision, 2026-08-05, taken after reading hermes' `delegate_task`
against Aura's `swarm_spawn`.

**Do not design the durable half — it is already designed and approved.**
`docs/superpowers/specs/2026-06-29-durable-swarm-messaging-design.md` (551 lines) specifies a
Postgres-first substrate: claimable tasks, short leases (1m — crash-recovery latency, not task
duration) extended by a heartbeat, fencing on `attempt_count` + `locked_by` so a zombie worker
whose lease expired matches zero rows and gets a typed `ErrLeaseLost`, at-least-once delivery
with idempotency keys, transient-vs-permanent retry with exponential backoff and full jitter,
and an A2A lifecycle in which `waiting_input` is a non-terminal pause woken transactionally by
the arriving reply. It was approved and never built. SWARM-03's background delegation is that
substrate's first consumer (SWARM-09), not a second mechanism beside it — the same
inventory-before-invention call that killed `make_document`. SWARM-11 lands its PRD amendment
first. SWARM-10 (a tail-able live transcript per child) is hermes' answer to waiting blind for
a consolidated report, and is what makes a backgrounded delegation observable at all. Three of these are cheap surface fixes against a defect Aura
already documents in her own tool description (*"the worker cannot see the conversation, the
user, the other workers, or anything outside the goal text you give it"*): SWARM-01 splits
`goal` from `context`, SWARM-02 rebuilds the schema per manifest render so the model reads the
operator's real `AURA_SWARM_*` caps instead of discovering them by failing, and SWARM-06 keeps
the child-question relay alive — no longer through an approval rework, since Phase 47 was deleted,
but against the `ask_user` shape as it stands.

SWARM-03/04 are the substantial change and are NOT a surface clean: today the parent blocks
until every worker reports. Hermes makes top-level delegation background-by-default with the
flag deprecated so the model cannot opt out, and keeps a depth>0 orchestrator synchronous
because it needs its workers inside its own turn. **Inventory before invention applies hard
here** — Aura already has machinery for "work happens later and reports back": the scheduler's
`agent_job` runs a fresh agent turn at fire time, and AG-UI carries run-detach with
Last-Event-ID resume (Amendment #90). Establish whether background delegation is a new
execution path or a second caller of those before designing one.

SWARM-05 opens nesting the PRD already designed (Slice 3's 2-deep cap) but the implementation
forecloses by handing workers `Without(reg, "swarm_spawn")`. SWARM-07 is the one nobody asked
for and everybody needs: AUTO-03 fires fact-capture inside every worker, so N concurrent
workers writing one identity's graph is a concurrency surface that does not exist today and
lands squarely on the memory correctness Phase 45 and 49 just established.
**Success Criteria** (what must be TRUE):

  1. Delegating in a live conversation returns Aura's turn immediately — the operator can keep talking, and the consolidated worker result arrives in the conversation when the work finishes, observable in `aura.conversation_turns`.
  2. A worker that itself delegates receives its own workers' results within its turn — its delegation does not return early, verified in a live nested run.
  3. A live worker brief carries its context separately from its goal, and the rendered tool schema shows the operator's configured concurrency and depth caps, not framework defaults.
  4. A worker that needs the operator surfaces the question in the operator's channel, naming which worker raised it, and answering it resumes that worker's line of work.
  5. After a live fan-out where several workers each learn something durable, the graph holds one correctly-attributed fact per worker — no duplicates, no lost writes, no fact attributed to the parent.

**Plans**: 2/11 plans executed in 6 waves

Plans:
**Wave 1**

- [x] 51-01-PLAN.md — TRACER: durable delegation enqueue + claim loop + worker-envelope delivery (SWARM-03, SWARM-09) — wave 1
- [x] 51-04-PLAN.md — Host-derived fact provenance, worker supersede refusal, concurrent fan-out proof (SWARM-07) — wave 1

**Wave 2** *(blocked on Wave 1 completion)*

- [ ] 51-02-PLAN.md — Postgres steer queue, kind-typed rows, two TTLs, one sweep, trace on expiry (D-06/07/08) — wave 2
- [ ] 51-03-PLAN.md — Worker brief goal/context split + live-rendered cap schema (SWARM-01, SWARM-02) — wave 2

**Wave 3** *(blocked on Wave 2 completion)*

- [ ] 51-05-PLAN.md — Depth-bounded nesting, synchronous nested delegation, SWARM-08 guard extension (SWARM-04, SWARM-05, SWARM-08) — wave 3
- [ ] 51-06a-PLAN.md — Pause fencing column + fenced conditional resume inside the shipped transaction (SWARM-06, D-12/D-13) — wave 3
- [ ] 51-10-PLAN.md — SC#1's conversation write + the absent-operator outbox leg (SWARM-03, SWARM-09, D-02) — wave 3

**Wave 4** *(blocked on Wave 3 completion)*

- [ ] 51-06b-PLAN.md — Worker opens its own pause, and answering RESUMES that worker (SWARM-06, SC#4) — wave 4
- [ ] 51-07-PLAN.md — Live transcript read surface + SWARM-11 verification (SWARM-10, SWARM-11) — wave 4

**Wave 5** *(blocked on Wave 4 completion)*

- [ ] 51-09-PLAN.md — D-03's measured termination model: reap on inactivity, retire the wall clock (SWARM-03/04/09) — wave 5

**Wave 6** *(blocked on Wave 5 completion)*

- [ ] 51-08-PLAN.md — Live SC#1–SC#5 driver, quality snapshot re-attestation, PRD measurement — wave 6

### Phase 52: Mid-turn steering

> **REVISED 2026-08-24 — PLAN-READY, and promoted ahead of Phase 51.** STEER-06's PRD amendment
> is committed (#132), which the original block required before any code. Every prerequisite the
> study names was verified present: `internal/agui/{runregistry,runsession,server_run_detach,
> server_run_resume}.go`, the `httpMutationRoutes` registration `cancel` already uses,
> `Store.AppendTurn`, and `internal/agent/model_round.go`.
>
> **Two things the amendment corrected after reading the reference implementations — the plan must
> follow the amendment, not this block's original text.** (1) A steer is delivered by appending it
> to the **last tool result** of the in-flight batch behind a user-attribution marker, NOT by
> inserting a plain `user` message; hermes does this deliberately to preserve role alternation, and
> Aura's own user-role nudges fire at termination points, never between tool results and the next
> call. (2) An undrained steer is **drained and named** in the terminal event rather than inferred
> from a missing echo.
>
> **Two preconditions of STEER-01.** `AURA_AGUI_RUN_DETACH` defaults `true` in
> `config_agui_run.go:36` and is catalogued `false` in `config_knobs.go:116` — steering requires
> detach, so the flag gate is decorative until they agree. And `llm_agent.go` is at **561/600 LOC**,
> so the drain point lands in a sibling file.

**Goal**: The operator can type into a running turn and have it land — redirecting work at
the next round boundary instead of waiting for the turn to end or killing it and starting
over.
**Depends on**: None — Phase 52 runs BEFORE Phase 51 in the actual execution order (`45 ✓ → 45.1 ✓ → 46 ✓ → 52 → 50 → 49 → 51 → 54`, see Progress below). The original reasoning — that steering should reconcile with the durable swarm substrate first — inverts once you observe the substrate does not exist yet: today there is only one run model, and building 51 first is what would create the second.
**Requirements**: STEER-01, STEER-02, STEER-03, STEER-04, STEER-05, STEER-06, RESUME-01
**Rationale**: Operator decision, 2026-08-05. The design study already exists
(`docs/superpowers/specs/2026-07-23-mid-turn-steering-design.md`, 664 lines, its three
operator-level open questions already resolved to "Claude Code parity") and is explicitly
marked STUDY ONLY — no code, no amendment. STEER-06 lands that amendment first; §11 of the
study is already an amendment checklist.

The study's own finding is why this is a phase and not an epic: **the seam is unusually
clean.** The agent loop already injects user-role messages mid-run on three established
paths — recovery nudge, empty-response nudge, completion-gate feedback
(`llm_agent.go:331`, `:439`, `llm_agent_finalize.go`) — so a steer is a fourth,
operator-sourced instance of an existing in-loop pattern rather than a new message
discipline. The runner persists turns incrementally per round
(`runner_persist.go:187-204`, `:144-151`), so a steer appended at the drain point lands at
the right `seq` with no in-flight-assistant-row conflict. And Amendment #90's RunRegistry
already gives every live run an owner-scoped addressable identity plus a replay ring, which
makes STEER-03's resume-exact echo close to free.

Today the composer is dead while the agent runs: the thread lock returns 409 `ErrThreadBusy`
and the client blocks send while `live_run_id` is set. STEER-02 is the guardrail that keeps
this from becoming an escape hatch — a steer redirects the work, it does not buy more budget.
STEER-04 exists because the failure mode of any queue-into-a-running-thing is silent loss.
**Success Criteria** (what must be TRUE):

  1. Typing a redirect while Aura is mid-task changes what she does next, live — observable as her next round acting on the new instruction, with no tool killed mid-execution.
  2. That steer appears in the persisted conversation at the point it actually landed — reloading the thread or resuming the run shows it in the right place, not appended at the end.
  3. Steering a run that has just finished is delivered automatically as the next user turn, preceded by a visible line saying that happened — it is never silently swallowed.
  4. A steered turn consumes no more steps or wallclock than an unsteered one — the budget is unchanged by steering.
  5. The same steer works from a channel, not only the cockpit.

**Plans**: 8/8 plans executed

Plans:
**Wave 1**

- [x] 52-01-PLAN.md — STEER-06 amendment gate: the five corrections to #132, the three superseded documents, the minted RESUME-01 id, and the knob surface

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 52-02-PLAN.md — The agent half: the conversation-keyed bounded inbox, both drain points, the nonce marker OUTSIDE the tool-output envelope, the teaching note and the pre-wrap lookalike scrub
- [x] 52-03-PLAN.md — RESUME-01 remainder: pending-approval TTL through the resume front door (empty-accept refusal and per-pause decision policy closed 2026-08-25)

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 52-04-PLAN.md — Tracer: the cockpit steer route, the aura.steer echo frame, drain-time persistence, the single-inbox wiring, plus the rehydration and resume-replay proofs

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 52-05-PLAN.md — STEER-04: auto-deliver a leftover steer as the next user turn, and refuse a terminal run with an actionable 410
- [x] 52-06-PLAN.md — STEER-05: Telegram steers the live turn, and the media queue D-05 requires (today a photo on a busy chat is dropped, not queued)
- [x] 52-07-PLAN.md — D-10's composer contract: the cockpit composer steers a live run, the aura.steer frame reaches the UI on both pumps, and the committed dist is rebuilt on Linux

**Wave 5** *(blocked on Wave 4 completion)*

- [x] 52-08-PLAN.md — Gate 3: live E2E from both surfaces, the D-13 budget A/B judged on ceiling+deadline, Go and frontend gates re-measured on this tree, quality snapshot re-attested (scored 9.0/10 — see `52-VALIDATION.md`; phase does not close, Telegram leg of SC#5 pending a human check)

### Phase 54: Milestone exit

**Goal**: The nine `learned_lesson` facts and the `always-deliver-files` skill — Aura's own
bug report on herself — are retired, and a live replay of the audited scenarios proves the
defects they compensated for are actually gone, not just believed gone.
**Depends on**: Phases 45-53 — this is the milestone exit gate; retiring the compensating lessons presupposes the defects they compensate for are fixed, and the replay validates everything shipped, not one phase in isolation.
**Requirements**: SURF-05, ACC-03
**Rationale**: This is the milestone's own stated finish line (PROJECT.md, REQUIREMENTS.md
organising insight): the milestone is done when Aura's memory no longer needs to hold
workarounds for a defective surface. SURF-05 (retire the lessons/skill) and ACC-03 (validate
the retirement holds under replay) are the same closing action — the deletion is a
precondition of the validation, not a separate cleanup step. ACC-01/ACC-02's policy
(established in Phase 45) governs this phase too: evidence is a live replay against OTel and
the four named signals, never a green test suite.
**Success Criteria** (what must be TRUE):

  1. The nine `learned_lesson` facts and the `always-deliver-files` skill are deleted from Aura's live memory/skill store — confirmed by querying ArcadeDB and the skills directory directly.
  2. Replaying the audited 2026-08-04 scenarios (or an equivalent live re-run of the same tasks) against the current stack produces correct tool choice on the first attempt, automatic file delivery, and successful self-retrieval of what she needed — verified via `aura.tool_invocations`/`aura.conversation_turns`.
  3. Across a fresh live run of those scenarios, Aura does not re-learn or re-write any of the nine retired lessons back into memory — confirmed by inspecting ArcadeDB after the run.

**Plans**: TBD

## Progress

**Execution Order (REVISED 2026-08-24 — no longer numeric):**

```
45 ✓ → 45.1 ✓ → 46 ✓ → 52 → 50 → 49 → 51 → 54
```

Three departures from numeric order, each measured rather than preferred:

- **52 before 51.** Phase 52 is plan-ready (amendment #132; every prerequisite verified present);
  Phase 51 is not (substrate unbuilt, inventory question unresolved). The original "delegation
  first so steering attaches to one run model" reasoning INVERTS once you observe that the
  substrate does not exist: today there is only one run model, and building 51 first is what
  would create the second.

- **47, 48 and 53 are DELETED, not annotated** (operator decision, 2026-08-25). Their detail
  blocks are gone from this document; the evidence that killed them stays in the PRD, which is
  where measurements live: amendment **#139** (`tool_search` 164 calls / 0 failures against a
  deferred tail of 100+ tools — TOOL-01's hard cap was never measured and this contradicts it),
  amendment **#134** (TOOL-01's `comms` row cannot be built at all) and amendment **#137**
  (Phase 53's deliverable shipped as L3 compaction on 2026-08-12). **The 23 requirements those two
  phases carried are deleted with them** — see REQUIREMENTS.md, which now defines 54, not 77.
  What did NOT go with them: amendment #133's approval-path defects, kept in
  `.planning/todos/pending/approval-resume-defects.md`, because they are resume-path security
  gaps rather than tool-surface ceremony. Empty accepted answers and per-pause decision policy
  closed on 2026-08-25; only pending-approval expiry remains open.

- **50 before 49.** 50's target is now the compaction trigger, which exists; 49's remaining scope
  is smaller than written and does not gate it.

Phase 54 stays last, but its causal gate is gone with Phase 47: the `always-deliver-files` skill
survives the milestone, because the AUTO-01 host-side delivery that would have made it redundant
was deleted rather than built. Phase 54 retires the nine `learned_lesson` facts and validates the
replay; the skill is now permanent surface, not a compensating workaround awaiting removal.

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 45. Harness correctness | 9/9 | Complete    | 2026-08-15 |
| 46. MCP trust and facade | 9/9 | Complete — 7 executed, 46-08 no-go, 46-09 operator-closed | 2026-08-25 |
| 49. Memory tiers | 0/TBD | Not started | - |
| 50. Context ladder legibility | 0/TBD | Not started | - |
| 51. Durable delegation | 2/11 | In Progress|  |
| 52. Mid-turn steering | 7/8 | In Progress|  |
| 54. Milestone exit | 0/TBD | Not started | - |

## Notes on conditional scope

**VOID 2026-08-24, phase DELETED 2026-08-25 — CTX-V2-01 was promoted by implementation, not by a
spike: the summarization arm shipped as L3 compaction on 2026-08-12 (PRD amendment #137), so the
spike had nothing left to decide. The paragraph below is kept only as the record of what was
planned.** ~~Phase 53's spike may promote `CTX-V2-01` (LLM summarization rung) from the v2
deferred list into a scheduled phase~~ — this roadmap deliberately does NOT pre-schedule that phase, since
its design, dependency set, and even which package it touches depend entirely on the spike's
still-unknown outcome (SUMMARY.md's own research flag: "cannot be planned in detail until the
spike concludes"). If promoted, add it as Phase 55 (or a decimal insertion if urgent) via
`/gsd-phase`, carrying ARCHITECTURE.md §3's prepared design and Pitfall 3's mandatory
anti-thrash/cooldown/fallback guards in the same phase, not a follow-up.
