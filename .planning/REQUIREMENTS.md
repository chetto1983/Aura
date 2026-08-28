# Requirements: Aura — v2.1.0 HERMES-CLAUDE_PARITY

**Defined:** 2026-08-05
**Core Value:** When Aura says she did something, she did it — and she can find what she knew.

> **Where these come from.** Two live sessions of 2026-08-04 were exported from
> `aura.conversations` and audited ([`docs/audit/live-conversations-2026-08-04/`](../docs/audit/live-conversations-2026-08-04/)),
> then cross-checked against `D:/tmp/hermes-agent` and 30+ vendor agent prompts by four
> research agents (`.planning/research/`). Every requirement below traces to an observed
> defect or a cited reference, not to a preference.
>
> **The organising insight.** Aura's long-term memory holds nine `learned_lesson` facts.
> Eight are workarounds for a defective surface, and six restate rules the system prompt
> already contains. She wrote her own bug report. This milestone's job is to make those
> lessons unnecessary — see **ACC-03**.
>
> **How anything here gets marked done — read this before checking a box.** Every
> requirement below is verified by talking to the running Aura and reading what she did.
> Not a unit test. Not an integration test. Not a smoke test. A green suite is not
> evidence and never closes a box (**ACC-01**). The reason is in the audit that produced
> this milestone: the replay defect that made her act on a stale result, report the wrong
> conclusion, and write that conclusion into her own memory as fact was invisible to the
> entire test suite. It surfaced because a human watched a real conversation go wrong.

## v1 Requirements

### Harness correctness

- [x] **HARN-01**: A mutating tool call re-issued in the same turn with identical arguments executes again and returns a fresh result, never a recorded one
- [x] **HARN-02**: A genuinely retried dispatch (CLI or scheduler restart, approval resume) still executes at most once
- [x] **HARN-03**: When a replay is correct, the returned result is labelled as replayed so the model can tell it apart from a fresh execution
- [x] **HARN-04**: A memory correction closes exactly the fact it names and leaves sibling facts sharing the same subject and predicate untouched
- [ ] **HARN-05**: Several memory operations apply as one atomic call, validated on the final state, so a correction cannot destroy what it was meant to replace
- [x] **HARN-06**: A turn does not end on a stated-but-unexecuted intention — either the action runs, or the turn says plainly it did not and why
- [x] **HARN-07**: The reply is in the operator's language, and internal deliberation never reaches it as user-facing text
- [x] **HARN-08**: Two tool calls arriving in one assistant message with the same id are repaired **deterministically** before the request is sent (`<id>_d<n>`, never a random id, which would break prompt-cache prefix stability). Aura has no such repair today and DeepSeek — her default model — is named by hermes as a provider that rejects duplicate ids outright; a degraded long-context turn emitting two calls under one id fails the request
- [x] **HARN-09**: Identical `(name, arguments)` calls are distinguished by **where they arrive**, not by their provider-supplied id: two in the *same* assistant message are a model error and only the first runs; two in *different rounds* are a deliberate re-issue and both run. This is the discrimination F-1 lacks — `[058]` and `[062]` in the audit were separate rounds with an `fs_write` between them, and the second was served from the registry

### Tool surface

- [ ] **TOOL-05**: Recalling memory takes one question; the host chooses graph traversal or hybrid search and reports which it used
- [ ] **TOOL-13**: `read_tool_output` stops occupying a loaded slot. Paging a large output should use the file tool that already exists — **neither reference harness has a bespoke paging tool**: hermes writes the spill to a file (*"the model can `read_file` to access the full output"*) and Claude Code's 16 tools have none either (`Read` takes `offset`/`limit`; `BashOutput` is for live background shells, not completed results). Two independent harnesses, no third way.

  **Deleting it outright is the goal, and it is not trivial — three sub-problems, to be sized before committing to the full form:**

  1. **Filesystem boundary.** Sidecars live host-side in `AURA_RUN_DIR` (absolute, swept on a timer); `fs_read` reads inside the sandbox container under `/workspace`. The spill must land somewhere the sandboxed reader can reach.
  2. **Description debt.** `fs_read`'s own text says *"A large result pages to a sidecar you read with read_tool_output"* — every description naming the tool has to be rewritten with it — and with SURF-02 deleted,
     no requirement owns that rewrite any more; it belongs to whoever does TOOL-13.

  3. **GC contract.** `AURA_RUN_DIR` is swept and `reserve.go`'s `resultExpiredMarker` depends on that. A spill in the persistent `/workspace` either accumulates in the operator's own space or needs its own sweep.

  **Fallback if (1) proves expensive: keep the tool but make it deferred rather than loaded.** That recovers a manifest slot at near-zero cost — though with TOOL-01 deleted (PRD amendment #139:
  `tool_search` measures 164 calls / 0 failures against a deferred tail of 100+ tools), the slot is
  no longer a scarce resource and this fallback is cheaper than the deletion it replaces — and paging a result is rare enough that one search round trip is acceptable. The L1 eviction pointer keeps working unchanged

- [x] **TOOL-14**: The PRD amendment ratifying the tool-surface tier change is committed **before** any of its code. It must (a) supersede amendment **A4** (2026-05-30, `prd.md:751,783`) by name, which declares `read_tool_output` a *"Builtin non-deferred"* with a byte-paging contract that TOOL-13 changes; (b) extend amendment **#44** (`prd.md:1371`), which already ratified `sandbox_exec` as *"non-deferred di proposito"* because a live E2E showed the model cramming a whole command line into one argument when it could not see the schema — *"il modello DEVE vedere lo schema"*; and (c) change the tiering axis stated at `prd.md:154` from **size** (*"Tool grandi → Deferred: true"*) to **frequency plus a hard count budget**. Covers TOOL-13 and MCP-04 (it also covered TOOL-01, deleted 2026-08-25). This is an extension of ratified reasoning, not a reversal of it

### Automation

<!-- Directive: "simplify AND automatize more". A step the host can take is not the model's to remember. -->

> **Narrowed 2026-08-25.** AUTO-01 (host-side file delivery), AUTO-02 (automatic indexing) and
> AUTO-04 (host-filled parameters everywhere) were deleted with Phases 47-48. `send_file` measures
> 18 calls and 0 failures, so no evidence said the operator was losing files; AUTO-01 additionally
> REVERSED D-05/D-06. Consequence recorded rather than hidden: the `always-deliver-files` skill
> stays, because the host-side delivery that would have made it redundant is not being built.

- [ ] **AUTO-03**: A durable fact revealed during a turn is captured as part of doing the work — and never harvested from reasoning traces (see CTX-05)

### Compatibility

> **DELETED 2026-08-25.** COMPAT-01/02/03 existed to keep live persisted state valid across the
> tool renames and merges of Phases 47-48. Those phases were deleted (PRD amendments #134, #139),
> nothing is renaming a tool, and the compatibility work has no change to be compatible with.

### MCP trust and facade

> **▶ Amended 2026-08-16** by the Phase 46 discussion, which measured this block against the tree
> and the MCP specification. MCP-02, MCP-04 and MCP-05 changed. Evidence and rationale:
> `.planning/phases/46-mcp-trust-and-facade/46-CONTEXT.md`.

- [x] **MCP-01**: MCP tool descriptions reach the model as ordinary text, without the untrusted-data wrapper. **Delivered ahead of the phase by `34b892512` (2026-08-12), un-amended** — `frameMCPDescription`/`frameMCPSummary` carry no distrust prefix. Phase 46 ratifies it by amendment; there is no code left to write
- [ ] **MCP-02**: Fail-closed risk classification for unknown tools remains in force and is proven **live**. The guardrails this requirement asserts are `mcpToolRisk`'s fail-closed default (`bridge_risk.go:112-118`, unannotated → `true, true`, escalate-only), `unsafeToRepeatBeyondAura`'s escalate-only escalation, the model-blind approval gate, bridge namespacing plus `Registry.Register`'s panic-on-duplicate, and `capSchemaDescriptions`' byte caps. **Amended 2026-08-16 (operator decision; ratified in prd.md Amendment #122):** per-call result fencing for MCP was removed by `34b892512` and that removal is the ratified posture — an operator-installed server is operator-trusted, and mounting must cost no ceremony. The counter-recommendation (hermes fences every `mcp_*` result; PITFALLS §2 calls a fencing regression *"the actually dangerous outcome"*) was put to the operator and declined ([superseded wording](#fn-mcp-02-1)). **Non-MCP sources are unaffected** and keep the nonce envelope: `web_fetch`, `document_search`/`document_open` (required by `prd.md:4579`), user attachments, swarm child output
- [x] **MCP-03**: Trust is unconditional across every mounted MCP server — those Aura ships, those added later, and those minted by her own self-extension alike. **Operator decision, 2026-08-05**, taken against the research recommendation to scope removal to code-reviewed recipes: the residual risk is carried by fail-closed risk classification and by the operator's control over what gets mounted at all ([superseded wording](#fn-mcp-03-1)). **Delivered by `34b892512`, already unscoped.** Reaffirmed and widened 2026-08-16: mounting a server must require **no declaration of any kind** — no curation entry, no trust tier, no allowlist. A server with no entry anywhere works immediately: deferred, discoverable, fail-closed
- [x] **MCP-04**: Calendar, mail and contacts are reachable through **one curated always-loaded Calendar slot**. WhatsApp remains reachable as a generic deferred mount: amendment #131 measured three raw-name view callbacks (`list_chats`, `list_messages`, `get_media_data`), so a curated tool plus its three exemptions would exceed the `<=3` ceiling and save no always-loaded manifest bytes. Curation lives in the forks; Aura adds no per-integration facade, hide-list or declaration. **Amended 2026-08-16 (D-17/D-18; ratified in prd.md Amendment #122), then narrowed by amendment #131 on 2026-08-24:** every MCP server Aura ships is a fork Aura controls (`chetto1983/aura-pim-mcp`, `chetto1983/whatsapp-mcp`, in-tree `cmd/arcadedb-mcp`), so a bad shipped surface is fixed at the source. **No `comms` tool, no curation config, no hide-list and no `bridgePolicy` namespace table is built in Aura's tree** — that would be per-integration bespoke the next mounted server gets no benefit from, and Aura's bridge stays generic ([superseded wording](#fn-mcp-04-1)). The two-slot cap remains a ceiling, not a target: Calendar spends one; WhatsApp, memory, Notion and Linear exceed the per-server loaded threshold and remain deferred. The original budget reasoning stands: skills take two of the fourteen slots and connected accounts take one, deliberately — hermes calls skills *"your procedural memory — reusable approaches for recurring task types"*, and Aura extends herself many times a day where she consults a calendar occasionally. Reference point: Claude Code ships **16 tools, all loaded, no deferred pattern** — every one a curated surface rather than an endpoint wrapper
- [x] **MCP-05**: The fork stops requiring a caller-supplied account identity: its detail tools accept the same opaque reference its listing tools return. **Amended 2026-08-16 (D-20; ratified in prd.md Amendment #122)** — the original framing was falsified by measurement. Against `internal/agent/tools/testdata/deferred_manifest.json`, `accountId` is two different things under one name: a defaultable **routing hint** in `create_event` (*"Omitting uses the first configured account"*) and `get_calendar_events` (*"omit to query all enabled accounts"*), and a **required opaque handle** in `get_calendar_event_details` (*"Account ID from get_calendar_events"*, beside required `calendarId` and `eventId`). Host-injecting a configured default into the handle case passes the **wrong** account for an event that came from another account's listing, so `user_identifier`-style injection cannot satisfy this ([superseded wording](#fn-mcp-05-1)). The fork now mints an opaque `eventId` carrying the server-side account coordinate; Aura's live two-identity `calendar_integration` tier resolves list → detail without dispatching `accountId`, and fork commit `5909c808f75bb1c612256666dd0f1aacf6921dd4` covers the route contract exhaustively. The reference shape is the one `prd.md:4579` already uses for documents (`document:<search_document_id>@<version>#<locator>`). **Spec basis:** MCP defines **no `accountId` concept** — identity is the OAuth token, audience-bound per server (RFC 8707); authorization is OPTIONAL; local/stdio servers are told to *"retrieve credentials from the environment"*

#### Footnotes — superseded MCP requirement wording

<a name="fn-mcp-02-1"></a> **MCP-02, superseded 2026-08-16:** "Per-call result fencing and"

<a name="fn-mcp-03-1"></a> **MCP-03, superseded 2026-08-16:** "MCP-02's per-call result fencing and"

<a name="fn-mcp-04-1"></a> **MCP-04, superseded 2026-08-16:** "**one** curated `comms` surface — a single loaded slot"

<a name="fn-mcp-05-1"></a> **MCP-05, superseded 2026-08-16:** "`accountId` is resolved host-side from the operator's configuration, like `user_identifier`"

### Native MCP client

> **▶ Added 2026-08-16.** New requirement family for **Phase 45.1**, inserted before Phase 46 by
> operator decision (*"use mcp client native no bespoke"*). Phase 45.1 delivers no MCP-0x
> requirement, so without these it would have nothing to verify goal-backward against.

- [x] **MCPC-01**: Aura speaks MCP through the official `github.com/modelcontextprotocol/go-sdk` client — already a dependency (`go.mod:27`, v1.7.0) but used server-side only today (`cmd/arcadedb-mcp/*`). The pinned version already ships the 2026-07-28 stateless core (`latestProtocolVersion`, `mcp/shared.go:50-51`) and negotiates down five protocol versions, so this needs **no dependency bump and no sidecar change**
- [x] **MCPC-02**: Aura's own policy layers survive **on top of** the SDK rather than being reimplemented beside it — SSRF resolve-then-pin and egress allowlist re-attached as a custom `http.RoundTripper`, plus `classify.go`'s trust taxonomy, `managed_config*.go`, `tool_error.go`, `domain_outcome.go`, `observability.go`, `redact.go`. Proven by a live SSRF/egress probe still being refused after the swap
- [x] **MCPC-03**: The bespoke transport and reconnect layer is **deleted, not left dormant** — `internal/mcp/client.go`, `http_client.go`, `transport.go`, `protocol.go`, `tool_methods.go`, `lifecycle.go`, ~~`probe.go`~~, and `internal/agent/mcptools/bridge_reconnect.go` + `bridge_ping.go` (≈1,970 LOC non-test, ≈900 LOC of tests), ~~replaced by `ClientOptions.KeepAlive` and the SDK's own backoff/`Last-Event-ID` reconnect~~ **replaced by the SDK's native session lifecycle: `ClientSession.Wait()` for death detection, `ToolListChangedHandler` for drift, `StreamableClientTransport.MaxRetries` for HTTP retry**. Dark code is forbidden: no hand-rolled JSON-RPC framing or reconnect loop may reappear elsewhere. **Amended 2026-08-16 — the original replacement mechanism was falsified by reading the pinned SDK source (`45.1-RESEARCH.md` § SDK liveness surface).** Three corrections: (1) `ClientOptions.KeepAlive` is **inert against a 2026-07-28 peer** — it is not version-gated client-side (`client.go:403` is a bare `if KeepAlive > 0`), it starts, the SDK server refuses `ping` with `CodeMethodNotFound` (`server.go:1879-1887`), and the keepalive goroutine **silently retires itself** (`shared.go:869-872`), so it looks configured and does nothing; (2) the "automatic backoff/`Last-Event-ID` reconnect" describes the **standalone SSE stream**, unconditionally removed under 2026-07-28 (`streamable.go:2095-2098`) — recovery from a killed sidecar instead works because every call is a fresh HTTP POST; (3) **`probe.go` is re-pointed, not deleted** — it is a use-case function that calls the SDK, not a facade over its types, and 13 `cmd/aura` diagnostic files depend on it. The replacement for `bridge_ping.go`'s poll loop is **push, not poll**: `ClientSession.Wait()` (`client.go:584`) blocks until the connection closes and returns why, so no Aura interval loop is needed. Redial *policy* (when/how often/with what backoff) remains Aura's — supervision, not protocol
- [x] **MCPC-04**: Host-side argument derivation moves onto the SDK's own extension point, `Client.AddSendingMiddleware` (`mcp/client.go:1131`) — the idiom the SDK uses for its own MRTR feature (`mcp/mrtr.go:22-31`). `cmd/arcadedb-mcp` additionally moves off the `user_identifier` **argument** onto stateless `_meta`, since Aura owns both ends. **This changes the memory tool's schema — PITFALLS §4 applies** (rehydrated history, paused approvals, scheduled jobs). **Extended 2026-08-16 (operator decision):** the transport-level fail-closed guard that `HTTPConfig.ToolIdentityArgument` provides today does not disappear with the argument — `cmd/arcadedb-mcp`'s memory tool handlers **reject any call whose identity `_meta` key is absent or empty** with a **tool error** (`IsError: true`) the model can see and self-correct on, never a protocol error. **Corrected 2026-08-16 (second pass): the mechanism first recorded here was backwards.** In a `ToolHandlerFor`, returning a plain Go `error` is ALREADY the tool-error path — `server.go:383-392` returns a protocol error only when the error is a `*jsonrpc.Error`, and routes everything else through `CallToolResult.SetError` → `IsError: true` + `TextContent`. So the handler should simply `return nil, nil, fmt.Errorf(...)`. Hand-building a `&CallToolResult{IsError: true}` with a nil error is the **worse** option: it skips that short-circuit, falls through to output-schema validation (`server.go:400-424`), and for a struct `Out` with required fields fails `applySchema` → `fmt.Errorf("validating tool output")` — a genuine protocol error, the exact outcome this requirement exists to avoid. The check lives in the server because the fork is ours and because middleware wraps `Client`, not the params type — a caller constructing `*CallToolParams` directly bypasses any client-side guard. Each identity has its own database, so an empty identity is a correctness bug, not a missing feature. The SDK's own `_meta` triple does not collide: `injectRequestMeta` leaves keys already present untouched (`client.go:~527`)
- [x] **MCPC-05**: A server-initiated elicitation (SEP-2322 multi-round-trip) reaches the operator through Aura's existing approval path with a bounded timeout, naming the asking server — hermes parity (`tools/mcp_tool.py:1669-1758`). ~~The SDK auto-fulfills input requests by default and Aura registers no handler today, so the current posture is permissive-by-omission~~ **Amended 2026-08-16 — the stated rationale was falsified by measurement; the requirement stands on a different basis.** The SDK does **not** auto-fulfill: with no `ElicitationHandler`, `c.elicit` returns `&jsonrpc.Error{Code: CodeInvalidParams, Message: "client does not support elicitation"}` (`mcp/client.go:862-864`, v1.7.0). Aura's current posture is therefore already **fail-closed**, not permissive — registering a handler closes no hole. The real justification is SEP-2322 compliance and operator visibility: today a server that needs input gets a flat refusal the operator never sees, so a legitimate multi-round-trip tool simply cannot work. Setting `ClientOptions.ElicitationHandler` to a non-nil value auto-advertises the elicitation capability

### Context management

- [ ] **CTX-01**: The context budget decision uses the provider's real reported token count, not a foreign-tokenizer estimate
- [ ] **CTX-02**: A `tool_search` result whose schemas were later re-loaded becomes evictable, so repeated lookups stop accumulating permanently
- [ ] **CTX-03**: A pruned skill body leaves a marker, so the model can tell a skill's instructions are gone rather than believing it still holds them
- [ ] **CTX-04**: The operator can see what is consuming the context window by category, not only how full it is
- [ ] **CTX-05**: Reasoning traces never reach a summarizer or fact extraction, so scratch-work conclusions cannot be preserved as facts
- [x] **CTX-06**: ~~A spike measures, on real exported conversations with known-correct answers, whether retrieval over indexed history recovers what the ladder drops — against summarization, and against both — and its result decides which ships.~~ **Satisfied by implementation, not by a spike (PRD amendment #137).** L3 LLM-driven compaction shipped 2026-08-12 (`prd.md:1190`) with an operator `/compact` command and an AG-UI endpoint. A spike decides whether to build a thing; this thing runs. What it does NOT settle: the retrieval arm was never measured against the summarization arm, so which is *better* is still unknown — only which one exists
- [ ] **CTX-07**: When the context is over threshold and cannot be reduced, the reason is stated rather than the session simply failing or silently degrading
- [ ] **CTX-08**: Tool output is bounded **per turn**, not only per result. After a round's results are collected, if their total exceeds the turn budget the largest are spilled to disk until it is under. Aura caps each result (`AURA_CONTEXT_PREVIEW_CAP_BYTES`) and has no aggregate: ten medium results in one parallel batch each clear the per-result cap and still overflow the turn — the exact shape of a swarm fan-out or a wide multi-tool round. Hermes calls this the third of three defenses and sets it at 200K chars

### Memory tiers

- [ ] **MEM-01**: Past conversation is semantically searchable, with Postgres remaining the system of record for turns and ArcadeDB holding a derived per-identity projection
- [ ] **MEM-02**: One retrieval call spans short-term conversation and long-term facts
- [ ] **MEM-03**: Reasoning traces are persisted to the graph with edges to the entities they touched, and enter context only when explicitly retrieved
- [x] **MEM-04**: One person is one entity — the operator's profile and preferences do not split across a name and an identity UUID
- [x] **MEM-05**: Recording a multi-valued fact does not create a junk entity node per distinct value
- [ ] **MEM-06**: The PRD amendment extending #91 (reasoning persisted to the graph, retrieved only on demand, never summarized or harvested) is committed **before** any reasoning-tier code

### Surface legibility

- [ ] **SURF-04**: The model can tell that memory was already injected, and that a tool dropped from the manifest is still callable
- [ ] **SURF-05**: The obsolete `learned_lesson` facts are retired once the defects they compensate for are fixed. **Narrowed 2026-08-25:** the `always-deliver-files` skill is NO LONGER part of this — AUTO-01 was deleted rather than built, so the gap that skill compensates for stays open and the skill stays with it

### Delegation

<!-- Hermes parity. Aura's swarm today: one goal string per worker, parent blocks, workers
     cannot delegate further (their registry is Without(reg, "swarm_spawn")). -->

- [ ] **SWARM-01**: A worker brief separates *what to accomplish* from *the context it needs* — file paths, error messages, constraints — instead of forcing both into one string
- [ ] **SWARM-02**: The model sees the operator's actual concurrency and depth limits in the tool schema, rather than discovering them by failing
- [x] **SWARM-03**: A top-level delegation returns the turn immediately; its results re-enter the conversation when the work finishes, and the model cannot opt out of this
- [ ] **SWARM-04**: A delegation issued *by a worker* runs synchronously — an orchestrating worker needs its own workers' results inside its own turn
- [ ] **SWARM-05**: A worker can itself orchestrate, bounded by the configured depth — opening the nesting the PRD designed and the current registry-minus-`swarm_spawn` implementation forecloses
- [ ] **SWARM-06**: A worker that needs the operator reaches them, attributed to the worker that asked — the relay survives — TOOL-03's approval rework was deleted 2026-08-25, so it rides the shipped `ask_user` shape rather than a reworked one
- [x] **SWARM-07**: Concurrent workers writing durable facts (AUTO-03) and reasoning traces (MEM-03) into one identity's graph neither corrupt nor duplicate, and each write names the worker that made it
- [x] **SWARM-08**: Workers reason over the same flattened tool surface the parent does, verified against the live surface rather than assumed from registry inheritance — the un-defer this was written to follow was deleted 2026-08-25
- [x] **SWARM-09**: Delegated work is durable — a task survives a process restart, is claimable from Postgres, and is never silently lost nor silently retried. Implements the approved-but-unbuilt [durable swarm messaging design](../docs/superpowers/specs/2026-06-29-durable-swarm-messaging-design.md); SWARM-03's background delegation is this substrate's first consumer, not a parallel mechanism
- [x] **SWARM-10**: The operator (and the parent) can watch a worker work — a tail-able live transcript per child, rather than waiting blind for the consolidated report
- [x] **SWARM-11**: The PRD amendment ratifying the durable swarm substrate is committed **before** any of its code

### Steering

<!-- Implements the design study at docs/superpowers/specs/2026-07-23-mid-turn-steering-design.md
     (study only, no code, no amendment yet). Its own finding: the loop already injects
     user-role messages mid-run on three paths, so a steer is a fourth instance of an
     existing pattern. -->

- [x] **STEER-01**: The operator can type into a running turn; the message is injected at the next round boundary as ordinary user input, never interrupting a tool mid-execution
- [x] **STEER-02**: A steer does not extend the step or wallclock budget — steering redirects the work, it does not buy more of it. Closed 52-08 by live A/B: unsteered baseline (4 rounds, ~54s) vs. cockpit-steered (4 rounds, ~36s) on the same scenario — identical round COUNT (the step ceiling) and no wallclock deadline extension; the steer redirected work inside the existing budget, it did not buy more of it. Faster wallclock is corroboration only, per the reading rule (ceiling+deadline are the gate, consumption is not)
- [x] **STEER-03**: A steer is echoed on the wire and persisted where it belongs in sequence, so a reload, a resume, or a later replay shows it at the point it actually landed
- [x] **STEER-04**: A steer that arrives after its run has ended is delivered automatically as the next user turn, preceded by a visible line saying that happened, never silently swallowed. Closed 52-08 by live evidence: a steer POSTed after a single-round run's own drain points had already passed landed as `auto_delivery_next_turn`, produced exactly one follow-on turn (count query = 1), and that turn opened with the byte-exact `steerAutoDeliveryNotice` string, never silently swallowed
- [x] **STEER-05**: Steering works from the operator's channels, not only the cockpit. The cockpit leg is live-proven (52-08, this session, via the same A/B run). The Telegram leg's wiring is proven only by 52-06's realistic unit tests (`internal/channels/telegram/bot_dispatch_steer_test.go`, `bot_dispatch_queue_test.go`, `cmd/aura/steer_wiring_test.go`, all `human_judgment: false`) — per ACC-01 a green suite alone is not "done". 52-08 could not live-corroborate the Telegram leg this session: the bot uses long-polling `getUpdates` against the real Telegram API with no local-bot-api sidecar, no Telethon/Pyrogram session, and no API_ID/API_HASH configured, so there is no way to script an inbound message as a real Telegram user without a human physically using their own Telegram client. See `52-VALIDATION.md` for the full not-proven record; this is a genuine open gap, not a rubber stamp
- [x] **STEER-06**: The PRD amendment ratifying mid-turn steering is committed **before** any of its code. Closed 52-08 by git history: Amendment #132 (`9b783bd54`, 2026-08-25 08:05:15+02:00, "Ratify the mid-turn steering contract before writing any of it") predates the phase's first steering code commit (`43c9cb5cf`, 2026-08-25 15:41:27+02:00, "add the steer + pause-TTL knob surface") by over 7 hours, and every subsequent steering commit (`b53bf2320` inbox, `dbec0dcc4` cockpit route, `68dd40585` Telegram) follows both
- [x] **RESUME-01**: The approval resume path refuses an accept carrying no answer, refuses a decision the pause's policy does not permit, and expires a pending approval as an expiry rather than as a yes — without weakening the `WHERE resumed_at IS NULL` conditional update that IS the idempotency key. Folded from PRD amendment #133 (Phase 47, deleted 2026-08-25); all three defects shipped ahead of the phase's own Gate 3 (empty-answer refusal, per-tool decision policy, pending-approval TTL — see `52-03-SUMMARY.md`). Closed 52-08 by live E2E against the real cockpit resolve route (`POST /api/approvals/{token}/resolve`): empty-content accept now returns 400 (a real bug was found and fixed here — the REST route fell through to a generic 500 before this session, see `52-VALIDATION.md`); a seeded restricted-decision pause (`allowed_decisions:["accept"]`) returns 403 for a disallowed decline; a seeded 3-day-old pause was left to the real 60s sweep and resolved as an expiry (`resumed_answer.action="expired"`), never as a yes

### Acceptance

- [x] **ACC-01**: **Mandatory, every requirement, no exceptions.** A requirement is verified only by a real conversation with the running Aura, scored on the answer she actually gave and the artifact or state she actually produced. A passing test — unit, integration, race, or smoke — is **not** evidence that a requirement is met. Tests keep the code honest; they say nothing about whether the agent behaves. Any requirement whose only evidence is a green suite is **not done**
- [x] **ACC-02**: Phase evidence is read from OpenTelemetry traces, `aura.tool_invocations`, `aura.conversation_turns` and `aura.context_rot_events` — no new eval harness is built
- [ ] **ACC-03**: **Milestone exit gate.** With the nine `learned_lesson` facts deleted, replaying the audited scenarios produces correct tool choice, file delivery and successful self-retrieval — and Aura does not re-learn any retired lesson. **Narrowed 2026-08-25** with SURF-05: the `always-deliver-files` skill is not deleted and delivery is not automatic, because AUTO-01 was deleted rather than built

## v2 Requirements

Deferred. Tracked, not in this roadmap.

### Context

- **CTX-V2-01**: LLM summarization rung with anti-thrash, cooldown and deterministic fallback — **promoted by implementation, not by a spike** — L3 compaction shipped 2026-08-12 (PRD amendment #137). CTX-V2-01's own anti-thrash/cooldown/fallback guards were the spike's deliverable and were NOT part of that shipment; whether they exist in the shipped compaction is unverified
- **CTX-V2-02**: Durable cross-restart anti-thrash state (needs a migration; in-memory is the simpler default)

### Tool surface

- **TOOL-V2-01**: Merge `fs_glob` and `fs_grep` — **blocked on telemetry**, see Out of Scope
- **TOOL-V2-02**: Provider reasoning-block replay for models that require it for multi-turn tool use (not needed by DeepSeek via OpenRouter today)
- **TOOL-V2-03**: **The three-tool bridge** — `tool_search` + `tool_describe` + `tool_call`, where a deferred tool is invoked THROUGH the bridge and never enters the manifest at all. Hermes' model; it makes the manifest constant regardless of catalog size (they carry ~3,300 Cloudflare tools whose names alone are ~32K tokens) and dissolves the promotion machinery outright — `activated`, `everLoaded`, `maxPromotedDeferredTools`, `promoteFromMeta`, `deriveActivated` all become dead code, and with them F-3's manifest-versus-callability confusion and the catalog-drift class hermes warns about (*"a session-keyed catalog that drifts out of sync with the live tool registry produces silent tool dropouts"* — which is what `deriveActivated` is).
  **Deferred deliberately, not overlooked.** Two reasons: Aura's gateway classifies risk by tool NAME and fails closed, and the idempotency key is built from `tools.Spec` + args — behind a bridge both must unwrap, which inserts a step into precisely the two code paths Phase 45 is fixing bugs in. And the benefit scales with catalog size — but that clause is now wrong, and the trigger below has already fired: the `comms` facade was never built (PRD amendment #134 — the row cannot be built without reopening D-17 or D-27), and the deferred tail measures **100+ tools** today (Linear 53, Notion 28, WhatsApp 15, memory 4). What kept the bridge deferred is no longer catalog size but the two unwrap points named above, plus amendment #139's measurement that `tool_search` is running at 164 calls / 0 failures over exactly that tail.
  **Note it would NOT fix F-4** — `tool_describe` results accumulate in the transcript exactly as `tool_search` results do today; that is CTX-02's job either way.
  **Trigger to reopen:** the deferred tail passes ~30 tools, or a self-extension-minted MCP server mounts a surface of Cloudflare's order.

## Out of Scope

| Feature | Reason |
|---------|--------|
| Merging `fs_glob` + `fs_grep` | **Back out of scope 2026-08-25.** Promoted to TOOL-11 on 2026-08-05 on the strength of TOOL-01's 14-slot cap; both were deleted with Phase 48, and the vendor evidence against the merge (five of six harnesses keep them separate) never stopped standing |
| Restoring `internal/eval/` | It was broken, which is why it was deleted. OTel, `tool_invocations` and `conversation_turns` already carry the evidence (ACC-02) |
| `make_document` routing tool | The friction it was meant to fix was the F-1 replay bug; fixing the harness removes the need |
| `remind_me` / `remember` as new wrapper tools | Delivered by flattening `memory_recall` in place (TOOL-05), so there stays one obvious way to do it. TOOL-04's `task` flatten was deleted 2026-08-25, so `task`'s five time fields stay as they are |
| Moving conversation persistence off Postgres | RLS fail-closed, branch replay, token/USD aggregation and rot events all live there. ArcadeDB gets a derived projection (MEM-01), never the source of truth |
| Reasoning in the default context window | Amendment #91's budget rationale holds — hermes measured 27% of a 214-turn payload sitting in reasoning blobs. MEM-03 keeps retrieval explicit |
| Un-deferring every tool | Anthropic's guidance and Aura's own measured 56-definition / ~20k-token incident both cap the useful manifest. The long tail stays deferred |
| Cockpit surfaces for the new signals | CTX-04 and the memory tiers are agent-facing first. A UI pass is a separate milestone unless the operator asks otherwise |

## Fix-on-touch

Not requirements — corrections to make in whatever phase touches the file (CLAUDE.md: *fix on touch, never skip*).

| Item | Detail |
|------|--------|
| `internal/toolinvocations/redact.go:23` | Documents `AURA_CONTEXT_PREVIEW_CAP_BYTES=2048`; the real default in `config_knobs.go:98` is `30000` — 14× off, in a comment guiding redaction logic |

## Traceability

Populated during roadmap creation (`.planning/ROADMAP.md`, Phases 45-54).

| Requirement | Phase | Status |
|-------------|-------|--------|
| ACC-01 | Phase 45 | Complete |
| ACC-02 | Phase 45 | Complete |
| ACC-03 | Phase 54 | Pending |
| AUTO-03 | Phase 49 | Pending |
| CTX-01 | Phase 50 | Pending |
| CTX-02 | Phase 50 | Pending |
| CTX-03 | Phase 50 | Pending |
| CTX-04 | Phase 50 | Pending |
| CTX-05 | Phase 49 | Pending |
| CTX-06 | — (Phase 53 deleted) | Satisfied by implementation — L3 compaction, 2026-08-12 |
| CTX-07 | Phase 50 | Pending |
| CTX-08 | Phase 50 | Pending |
| HARN-01 | Phase 45 | Complete |
| HARN-02 | Phase 45 | Complete |
| HARN-03 | Phase 45 | Complete |
| HARN-04 | Phase 45 | Complete |
| HARN-05 | Phase 49 | Pending |
| HARN-06 | Phase 45 | Complete |
| HARN-07 | Phase 45 | Complete |
| HARN-08 | Phase 45 | Complete |
| HARN-09 | Phase 45 | Complete |
| MCP-01 | Phase 46 | Shipped early (`34b892512`) — ratify by amendment |
| MCP-02 | Phase 46 | Pending (amended 2026-08-16 — fencing clause dropped) |
| MCP-03 | Phase 46 | Shipped early (`34b892512`) — ratify by amendment |
| MCP-04 | Phase 46 | Complete (amendment #131: Calendar loaded/curated; WhatsApp generic/deferred) |
| MCP-05 | Phase 46 | Complete (`calendar_integration` opaque-reference round-trip; fork 233/233) |
| MCPC-01 | Phase 45.1 | Complete |
| MCPC-02 | Phase 45.1 | Complete |
| MCPC-03 | Phase 45.1 | Complete |
| MCPC-04 | Phase 45.1 | Complete |
| MCPC-05 | Phase 45.1 | Complete |
| MEM-01 | Phase 49 | Pending |
| MEM-02 | Phase 49 | Pending |
| MEM-03 | Phase 49 | Pending |
| MEM-04 | Phase 45 | Complete |
| MEM-05 | Phase 45 | Complete |
| MEM-06 | Phase 49 | Pending |
| RESUME-01 | Phase 52 | Complete (closed 52-08 by live E2E: 400/403/expiry all exercised against the real resolve route; a 500-vs-400 bug found and fixed) |
| STEER-01 | Phase 52 | Complete |
| STEER-02 | Phase 52 | Complete (closed 52-08 by live A/B, identical round ceiling) |
| STEER-03 | Phase 52 | Complete |
| STEER-04 | Phase 52 | Complete (closed 52-08 by live auto-delivery proof) |
| STEER-05 | Phase 52 | Partial — cockpit leg live-proven 52-08; Telegram leg unit-proven only (52-06), no scriptable live Telegram session this session |
| STEER-06 | Phase 52 | Complete (closed 52-08 by git history: amendment predates first code commit) |
| SURF-04 | Phase 50 | Pending |
| SURF-05 | Phase 54 | Pending |
| SWARM-01 | Phase 51 | Pending |
| SWARM-02 | Phase 51 | Pending |
| SWARM-03 | Phase 51 | Complete |
| SWARM-04 | Phase 51 | Pending |
| SWARM-05 | Phase 51 | Pending |
| SWARM-06 | Phase 51 | Pending |
| SWARM-07 | Phase 51 | Complete |
| SWARM-08 | Phase 51 | Complete |
| SWARM-09 | Phase 51 | Complete |
| SWARM-10 | Phase 51 | Complete |
| SWARM-11 | Phase 51 | Complete |
| TOOL-05 | Phase 49 | Pending |
| TOOL-13 | Phase 50 | Pending |
| TOOL-14 | Phase 46 | Complete |

**Coverage:**

- v1 requirements: **59 total** — 82 defined, 23 deleted 2026-08-25 with Phases 47-48
- Mapped to phases: 58
- Unmapped: 1 — CTX-06, which needs no phase: it was satisfied by implementation, not by the
  Phase 53 spike that was deleted around it

- **The count above was wrong before this edit, and the correction is separate from the deletion.**
  This block read *"77 total / 77 mapped / 0 unmapped"* since 2026-08-13. Counted mechanically at
  the commit before the deletion, the document defined **82** requirements and the traceability
  table held **82** rows — self-consistent with each other, and both five higher than the prose
  claimed. Nothing was unmapped; the summary line had simply stopped being recounted as
  requirements were added. 82 − 23 = 59, verified by counting bullets and table rows again after

- Deleted, and why: **TOOL-01/02/03/04/06/07/08/09/10/11/12, SURF-01/02/03/06/07/08,
  AUTO-01/02/04, COMPAT-01/02/03.** They were the whole content of Phases 47-48. PRD amendment
  **#139** measures `tool_search` at **164 calls / 0 failures** against a deferred tail of 100+
  tools, which falsifies TOOL-01's *"hard cap, not a target"* — the premise every one of them
  inherited. Amendment **#134** separately shows TOOL-01's `comms` row cannot be built at all.
  They are deleted rather than deferred to v2 because deferring an unmeasured premise only moves
  it; nothing here is blocked on them, and if tool choice ever degrades it comes back with the
  measurement attached

- What did NOT go with them: PRD amendment **#133**'s approval-path defects, kept in
  `.planning/todos/pending/approval-resume-defects.md`. Empty accepted answers and per-pause
  decision policy closed on 2026-08-25; pending-approval expiry remains open. These are resume-path
  security gaps that happened to be scheduled inside Phase 47, not tool-surface ceremony

---
*Requirements defined: 2026-08-05*
*Last updated: 2026-08-25 — **23 requirements deleted with Phases 47-48** (see Coverage above);
CTX-06 marked satisfied-by-implementation as Phase 53 was deleted; SURF-05 and ACC-03 narrowed to
stop promising the `always-deliver-files` skill's retirement, which AUTO-01's deletion makes
permanent surface rather than a workaround awaiting removal. Prior: 2026-08-13 — reconciled the
prose around the traceability table with the table itself. The table was already current; the surrounding sentences still described the roadmap's
first shape. History: 2026-08-05 roadmap created over Phases 45-52 with 52 requirements (the
count corrected from a stated 51 to an actual 52 at that time), then expanded the same day to
Phases 45-54 by `1844cbfd9` (durable delegation + mid-turn steering) and grown to 77 by the
follow-up commits through `528d811c7`. Coverage re-measured 2026-08-13 against ROADMAP.md:
77 defined, 77 mapped, each to exactly one phase, 0 unmapped, 0 double-mapped.*
