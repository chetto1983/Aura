---
gsd_state_version: 1.0
milestone: v2.1.0
current_phase: 51
current_phase_name: Durable delegation
status: executing
stopped_at: Phase 51 context gathered; design-gate spike is the next action
last_updated: "2026-08-27T09:25:16.181Z"
last_activity: 2026-08-25
last_activity_desc: Phase 52 execution resumed (wave continue)
state_head: 4eac0a97371d58c153babc7ecc7b37cdefb0352c
progress:
  total_phases: 8
  completed_phases: 1
  total_plans: 45
  completed_plans: 34
milestone_name: HERMES-CLAUDE_PARITY
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-08-05)

**Core value:** When Aura says she did something, she did it — and she can find what she knew.
**Current focus:** Phase 52 — Mid-turn steering

## Current Position

Phase: 51 (Durable delegation) — READY TO EXECUTE
(corrected against hermes before any code existed), and every prerequisite was verified present.
Status: Ready to execute

**The roadmap shrank on 2026-08-25 (operator decision).** Phases **47** (tool-surface ceremony
strip), **48** (un-defer and merges) and **53** (summarization spike) are DELETED, not annotated —
their blocks are gone from ROADMAP.md and the **23 requirements** they carried are gone from
REQUIREMENTS.md. The evidence lives in the PRD, which is where measurements belong: amendment
**#139** (`tool_search` = 164 calls / 0 failures against a deferred tail of 100+ tools, which
falsifies TOOL-01's *"hard cap, not a target"*), **#134** (TOOL-01's `comms` row is unbuildable
without reopening D-17 or D-27) and **#137** (Phase 53's deliverable shipped as L3 compaction on
2026-08-12). Execution order is now `45 ✓ → 45.1 ✓ → 46 ✓ → 52 → 50 → 49 → 51 → 54`.

Phase 46 closed the same day at 9/9: seven plans executed, 46-08 a recorded no-go (amendment #131 —
the WhatsApp view calls back into three tools, so a curated WhatsApp would be four model-facing,
over the ceiling), and 46-09 closed by the operator. **What 46-09 did not leave behind, stated
plainly:** the MCP-01 distrust-framing and MCP-03 `TrustTrusted` tripwire tests do not exist, and
`deferred_manifest.json` still holds its pre-curation 55 entries. See `46-09-SUMMARY.md`.

Prior status (Phase 46 execution):
Phase 46 discussion are recorded in `46-CONTEXT.md` D-10..D-16 and in ROADMAP §45.1.
Last activity: 2026-08-25 — Phase 52 execution resumed (wave continue)
failure is closed: a bridged tool never set `Multiplexed`, so `classify` gave ONE flat tier to the
whole merged tool — `calendar(action=list_calendars)` and `calendar(action=send_email)` scored
identically, with no panic to warn anyone. Now `bridge.go:211` sets `spec.Multiplexed =
isKnownMultiplexedMCPTool(name)` (D-34: earned by having a classifier, NEVER inferred from a schema
carrying an `action` property, or a stranger's server would panic boot), `internal/gateway`
registers `classifyCalendarAction`, and `trustedRecipeActions[calendarRecipeSource]`'s 14 keys are
read as action names — no relabeling needed, the fork's enum already matched. The mixed key spaces
(calendar action-keyed, memory/whatsapp raw-tool-name-keyed) are documented ON the table per D-35.
Per D-32 the pin, the re-key, the Multiplexed flip and the classifier entry landed in ONE commit
(`2edbc3910`), preceded by its RED (`09e87b8d5`), so no window existed where calendar reads demanded
approval or the merged tool classified flat.

Proven live, not just green: the `aura-pim-mcp` service was recreated onto
`:38c94fd9d22d85c4b89f3d5b1f8202970faed117` and `tools/list` now returns exactly ONE tool,
`calendar`; the `calendar_integration` tier ran against it (`TestCalendarServerLive` PASS, protocol
`2025-11-25`). `-race` green in WSL on both touched packages.

Carried forward, honestly: **the MCP-05 round-trip is only half-proven** — this host has ZERO
connected accounts, so `get_calendar_events` returns no `eventId` to chain a detail call from. The
schema half is exercised, the opaque-reference round-trip is not. 46-07 must connect a real account
or MCP-05 stays unproven for the whole phase. Also inherited from 46-05 and still open: the fork
deleted 14 MSTest files (2,062 LOC) with no replacement and its `ci.yml` never triggers on
`aura/pim-sidecar`, so the `calendar_integration` tier is the ONLY automated proof of that surface.

Process note: three gsd-executor dispatches were killed by the harness's 600s stream-idle watchdog
(the same failure that killed three executors in Phase 45.1). Both plan commits are the executor's,
made across resumes; the orchestrator ran the gates, recreated the sidecar, ran the live tier and
wrote the SUMMARY inline at the operator's direction. No plan content was weakened to finish.

Next: Plan 46-07 — the tracer GATE. Drive one real conversation through the running stack, capture
the per-action evidence rows, and score the scenario into 46-VALIDATION.md. Connect at least one
real account first, or the MCP-05 round-trip cannot be closed. See
`.planning/phases/46-mcp-trust-and-facade/46-06-SUMMARY.md` for full detail.

Progress: [████████░░] 82%

## Performance Metrics

**Velocity:**

- Total plans completed: 9
- Average duration: — min
- Total execution time: 0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 45 | 9 | - | - |

**Recent Trend:**

- Last 5 plans: —
- Trend: —

*Updated after each plan completion*
**Per-Plan Metrics:**

| Plan | Duration | Tasks | Files |
|------|----------|-------|-------|
| Phase 45 P03 | 55min | 3 tasks | 3 files |
| Phase 45 P04 | 70min | 3 tasks | 6 files |
| Phase 45 P06 | 150min | 3 tasks | 7 files |
| Phase 46 P01 | 35min | 3 tasks | 1 files |
| Phase 46 P02 | 55min | 2 tasks | 1 files |
| Phase 46 P05 | 180min | 2 tasks | 9 files (external repo) |
| Phase 46 P06 | 145min | 3 tasks | 12 files |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table. Recent decisions affecting current work:

- ~~Roadmap creation: build order is F-1 idempotency fix (45) → MCP trust/facade (46) →
  tool-surface ceremony strip (47) → tool-surface un-defer/merges (48) → memory tiers (49) →
  context ladder (50, parallel-safe) → durable delegation (51) → mid-turn steering (52) →
  summarization spike (53) → milestone exit (54).~~ **SUPERSEDED 2026-08-25** — 47, 48 and 53
  deleted; order is now 45 → 45.1 → 46 → 52 → 50 → 49 → 51 → 54.

- Durable delegation (51) and mid-turn steering (52) were added to the milestone on
  2026-08-05 by `1844cbfd9`, after the original 8-phase roadmap was written — both are
  operator decisions with pre-existing approved designs under `docs/superpowers/specs/`,
  not fresh design work. They pushed the spike 51→53 and the milestone exit 52→54.

- ~~Tool-surface work deliberately split across two phases (47, 48)~~ **VOID 2026-08-25** — both
  phases deleted, and COMPAT-01/02/03 with them: they existed to survive renames nobody is doing.

- MEM-06 (PRD amendment extending #91) is a committed step inside Phase 49, landing before
  any reasoning-tier code commit — not a separate phase, since it's a within-phase sequencing
  rule (CLAUDE.md PRD-amendment-before-code), not a standalone deliverable.

- ~~CTX-06 (Phase 53) is a spike~~ **VOID 2026-08-25** — the summarization arm shipped as L3
  compaction on 2026-08-12 (amendment #137), so CTX-V2-01 was promoted by implementation rather
  than by a spike. CTX-06 is marked satisfied-by-implementation and maps to no phase. What that
  does NOT settle: the retrieval arm was never measured against the summarization arm.

- v1 requirement count: **59** (2026-08-25). The prior figure of **77** was wrong when it was
  written and stayed wrong: counted mechanically at the commit before the deletion, REQUIREMENTS.md
  defined **82** requirements and its traceability table held **82** rows — self-consistent with
  each other, five above the prose. Nothing was unmapped; the summary line had simply stopped being
  recounted. 82 − 23 deleted = 59, of which 58 map to a phase and CTX-06 maps to none. History: the
  count was corrected from a stated 51 to an actual 52 at definition time, then grew as delegation,
  steering and the tool-surface commits landed on 2026-08-05.

- [Phase 45]: Boot-time operation-metadata guard (D-09) checks the three Mutating-tool fields for EMPTINESS only, positioned inside ValidateClassifiable's existing reg.All() loop BEFORE the multiplexed-classifier continue, so non-multiplexed mutating tools are also covered
- [Phase 45]: 45-03's Task-2 span attributes are set inside execTool on the span already carried by ctx (stampReplayAttributes), the smaller of the two acceptable plumbing shapes named in the plan
- [Phase 45]: 45-04: RED commits ship a compiling identity-passthrough stub instead of an undefined symbol, because the pre-commit vet gate runs go vet ./... over the whole tree
- [Phase 45]: 45-04: llm_agent.go crossed the 590-LOC refactor threshold after both dedup call sites landed (583->592); both measured extraction candidates split into new llm_agent_round.go (roundBudget, recordRequestBuilt), landing at 556/70 for headroom against plans 45-05..08
- [Phase 45]: 45-04: three pre-existing test fixtures (fanout-cap, two panic-recovery tests) used byte-identical (name,args) calls in one message as a convenience fixture; D-12's new dedup now correctly collapses those, so each fixture was given distinct per-call args rather than weakening the new dedup
- [Phase 45]: 45-06: A refused correction (fact_key miss, or 0/>1 candidates) writes NOTHING -- no fact closed AND the new fact itself is not created, beyond the plan's literal 'closes nothing' text, to avoid orphaning or adding to an already-ambiguous candidate set
- [Phase 45]: 45-06: proseObjectRuneBound=80, measured live against the operator identity graph (longest legit Entity.name 36 runes, shortest measured prose violation 96 runes) rather than guessed
- [Phase 45.1]: 45.1-03: dark-code guard (darkcode_test.go) scans non-test .go files with comments stripped via go/scanner rather than a bare recursive grep -- this phase's own SDK-era wire-emulation test fixtures and doc comments legitimately contain deleted-symbol tokens (Mcp-Session-Id, notifications/initialized, decodeToolResult) that a literal substring scan would false-positive on
- [Phase 45.1]: 45.1-03: full aggregate coverage gate (scripts/coverage_docker.sh) not run -- a concurrent unrelated session (plan 45.1-04) held compose.yaml and bridge_risk.go dirty in the same checkout for the whole plan; per-package measurement (internal/mcp 90.5%, internal/agent/mcptools 92.1%) used as the honest substitute, aggregate gate deferred to when the tree is quiescent
- [Phase 45.1]: 45.1-04: trusted-recipe branch left byte-identical when adopting IdempotentHint/OpenWorldHint -- operator curation outranks a server hint, and wiring the new hints there would have re-tiered create_event, download_media and memory_upsert_fact, putting an approval prompt in front of ordinary work
- [Phase 45.1]: 45.1-04: in the fallback path destructiveHint:false alone no longer earns the mutating tier -- idempotentHint defaults false and openWorldHint defaults true, so a server must now declare the call repeatable or closed-world to earn it; an untrusted server must not talk itself out of an approval gate with the cheapest hint
- [Phase 45.1]: 45.1-04: completed inline by the orchestrator after three gsd-executor dispatches died to a 600s stream watchdog (one at launch with an empty transcript, two mid-run); operator authorised the deviation
- [Phase 45.1]: 45.1-06: elicitation_surface = decline-and-surface — the handler declines but delivers the ask on the operator's channel naming the server; no in-flight turn is blocked and no row is written to aura.paused_states. Option B (mint-and-wait) stays available later behind the same ElicitationConsent seam
- [Phase 45.1]: 45.1-06: elicitation timeout = 300s via its OWN env var AURA_MCP_ELICITATION_TIMEOUT_SEC — the operator asked to reuse the gateway approval default, but measurement showed no approval timeout/TTL exists anywhere (approvals are an async cross-turn ledger with nothing held waiting); the value was honoured, the source could not be
- [Phase 45.1]: 45.1-07: on protocol 2026-07-28 a server does NOT send elicitation/create mid-request -- the SDK refuses it outright and the live path is an InputRequests map fulfilled by clientMultiRoundTripMiddleware; the first test draft had it wrong and only a real in-memory CallTool revealed it
- [Phase 45.1]: 45.1-07: obs exposes emission ONLY through Boundary (outcome derived from the error), so the plan's seven-valued action counter was not buildable without widening a shared package; reused MCPCallsID with a new catalog operation value and put the finer action in the structured log
- [Phase 45.1]: 45.1-07: the consent surface is late-bound because MCP mounts run before the channels Registry exists; follows the existing cron.ChannelDeliverer pattern rather than reordering boot
- [Phase 46]: Amendment numbers 122/123/124 assigned to the MCP trust/curated-surface, TOOL-14 tiering-axis, and Phase 45.1+env-catalogue amendments (46-01)
- [Phase 46]: 46-02 selected views-exempt: WhatsApp's list_chats/list_messages stay raw, advertised and unmerged (their live MCP Apps views break under CallReadOnlyTool's Mutating gate if merged); WhatsApp ends the phase at 3 model-facing tools, not 1, amending D-18's worked example for WhatsApp only
- [Phase 46]: 46-02 confirmed model-facing curated tool names calendar__calendar and whatsapp__messages (D-18's names taken literally through name.go's namespacedName over catalog.go's calendar/whatsapp mount namespaces)
- [Phase 46]: 46-04 froze the deferral decision at mount rather than recomputing it on reconnect — a fork briefly advertising a different tool count during a bad deploy would otherwise add or remove a tool from the model's manifest mid-conversation, invalidating the KV-cache prefix it is relying on; drift is WARNed, never applied
- [Phase 46]: 46-04 kept warnIfDeferralWouldFlip out of grantLoadedSlot — recomputing the real decision per reconnect would spend or refuse the global 2-slot budget as a side effect of a health-check-shaped call, corrupting the budget the freeze exists to keep stable
- [Phase 46]: 46-04 corrected TestBridge_MemoryNamespaceToolsAreDeferredByDefault's fixture from 2 tools to the real cmd/arcadedb-mcp surface (10 advertised, 6 hidden, 4 model-facing) — the old fixture would have wrongly earned a slot under the new arithmetic, so its assertion was accidentally true
- [Phase 46]: 46-06 sets Multiplexed ONLY for a namespaced name that already has a gateway classifier (isKnownMultiplexedMCPTool), never inferred from a schema carrying an `action` property — inferring it would panic Aura's boot on a stranger's server, the exact opposite of what a generic MCP host promises (D-34)
- [Phase 46]: 46-06 kept ONE risk table with two key spaces rather than splitting it — calendar action-keyed, memory/whatsapp raw-tool-name-keyed — and documented the asymmetry on the table itself (D-35), because a second table is precisely the class of bug this phase closes
- [Phase 46]: 46-06 corrected docker/aura/PROVENANCE.md's "tool surface trimmed 29->14" prose in the same edit as the pin; 46-05 made it false (29->1 curated tool with 14 actions) and a stale provenance claim is worse than none
- [Phase 46]: 46-06 was finished inline by the orchestrator after three gsd-executor dispatches died to the 600s stream-idle watchdog; both plan commits are the executor's, made across resumes, and no plan content was weakened to finish
- [Phase 46]: 46-05 registers the curated `calendar` tool via `McpServerTool.Create(...)` + a `WithCalendarActionTool()` extension, not the SDK's attribute-scanning `.WithTools<T>()` — that overload accepts no schema customization, and a genuine JSON Schema `enum` on `action` together with a named/listed unknown-action error (T-46-18) needs a plain-string parameter with the schema patched after construction
- [Phase 46]: 46-05 found `AIJsonSchemaCreateOptions.TransformSchemaNode` carries no per-parameter identity when generating a schema from a MethodInfo's parameters (empty Path, null PropertyInfo, measured live via debug instrumentation) — the working fix instead parses and rewrites the already-built `Tool.InputSchema` after `McpServerTool.Create` returns
- [Phase 46]: 46-05's MCP-05 fix encodes `{accountId, eventId}` as an opaque base64 JSON token minted by `get_calendar_events` and decoded only by `get_calendar_event_details` — the provider call still needs a real accountId server-side, so the reference carries it instead of a caller-supplied argument; a missing/malformed reference is rejected outright, never defaulted to an account
- [Phase 46]: 46-05 found `ci.yml` structurally never triggers on `aura/pim-sidecar` (push/PR to `main` only, zero runs in the branch's history) — the plan's literal "ci.yml green" acceptance wording cannot be satisfied on this branch; `aura-publish-image.yml` is the actual gate and it succeeded, additionally verified by pulling and re-probing the published `:<sha>` image live
- [Phase 52]: 52-01 assigned PRD amendment #142 (not #141 — a concurrent session claimed #141) for the five hermes/live-tree corrections to #132; RESUME-01 minted as its own requirement ID and amendment #133 re-pointed from the deleted Phase 47 to Phase 52
- [Phase 52]: 52-08 scored the phase 9.0/10 and REFUSED to close it. SC#1-4 all 2.0, live-proven at both the curl-backend and the Playwright-browser level; SC#5 scored 1.0 because the Telegram leg cannot be driven from this environment at all
- [Phase 52]: 52-08 landed PRD amendment #146 (composer contract is a dedicated control, not an un-disabled Send) closing the 52-07 record defect, plus a Rule-1 live find: askuser.ErrInvalidAnswer returned 500 instead of 400 on /api/approvals/{token}/resolve (99c07b5ba), and closed internal/steer/inbox.go mutation to 100% killed
- [Phase 52]: 52-07 measured the INSTALLED @assistant-ui/react source (not its docs) and found capabilities.queue/steerQueueItem exist but are NOT reachable through Aura's legacy useExternalStoreRuntime: useComposerSend gates on `!canSend || (isRunning && !capabilities.queue)` and createActionButton ORs that with the caller's disabled prop, so nothing short of adopting the native queue subsystem could un-disable Send. Shipped a dedicated "Redirect the current turn" control beside Cancel instead
- [Phase 52]: 52-07's dist was rebuilt via `docker build --target webbuild` + `docker cp` (the Dockerfile's own node:24-bookworm-slim stage, matching CI's Node 24.x) because no native Linux Node toolchain exists on this host -- WSL's PATH resolves only the Windows node.exe, which would emit Windows-hashed chunks. Verified by diff against the extraction before and after replacing
- [Phase 52]: 52-06 confirmed D-05's premise against the live tree before building: a non-text message during a live turn WAS dropped (startTurn called onBusy() and returned, no queue on that path). The queue is new work, not a repair
- [Phase 52]: 52-06 gives Telegram one inbox with TWO consumers (cockpit route + channel), pinned by cmd/aura/steer_wiring_test.go; media queues in a per-chat pending slot delivered under the SAME registration and identity-scoped ctx, chain-capped at one, proven by invocation COUNTS not reply inspection
- [Phase 52]: 52-05 auto-delivers a leftover steer via turnLocked under the ALREADY-HELD lock (never Turn/runTurn, which would deadlock re-acquiring it), capped at steerAutoDeliverMaxChain=1 so a steer queued during the auto-delivered turn cannot start a second hop
- [Phase 52]: 52-05's exactly-once persistence is proven by a ROW COUNT (TestLeftoverSteerPersistsExactlyOneTurn), not a status code; 52-04's drain-time persistSteerTurn is guarded against the next-turn form via steerDeliveryForms
- [Phase 52]: 52-05 moved Deps/ResumeHook/New out of runner.go (596/600 before the change) into runner_deps.go as refactor-on-touch, named explicitly in the commit body -- the Wave-2 mis-titled-commit defect did not repeat
- [Phase 52]: 52-04 closes the Wave-2 cap-drift by wiring newSteerInbox(cfg.AGUISteer) at the composition root (cmd/aura/chat_boot.go) and pinning the WIRED caps to 8/16384 in chat_boot_test.go. internal/steer's own 32/32768 fallbacks are kept DELIBERATELY divergent so the package never imports internal/config -- a zero Config is then visibly wrong rather than silently plausible
- [Phase 52]: 52-02 delivers a steer by appending it AFTER the closing </tool_output> of the last tool-result message, behind a nonce marker minted by trust.go's existing toolOutputNonce() -- there remains exactly one nonce minter in internal/agent
- [Phase 52]: 52-02's GREEN commit for Task 2 (60836960e) carries the steer drain implementation (llm_agent.go, llm_agent_steer.go, trust.go) but its SUBJECT describes only a bundled Rule-3 lint unblock; the message misrepresents the diff. Left unrewritten deliberately: a concurrent session pushed ad0bee571 and merge 18231becc around it, and rewriting shared history another session may have pulled is worse than a wrong subject line. Recorded here so a later `git log --grep=steer` miss is explainable
- [Phase 46]: 46-05 deleted the 14 orphaned MSTest files (2062 LOC) for the deleted raw tool classes rather than porting them to `CalendarActionTool` — no replacement unit coverage was written for the merged tool in this plan; flagged as a known gap/follow-up, not silently absorbed

### Pending Todos

None yet.

### Blockers/Concerns

- ~~Phase 45's key-shape fix direction depends on an unverified `tool_call_id` fact
  (Pitfall 5).~~ **Resolved 2026-08-05 by `657c9e383`**, from hermes
  (`agent/message_sanitization.py:536-566`, `run_agent.py:4601-4648`): do NOT key on
  `tool_call_id` at all. Providers reuse one id across a batch and strict providers —
  DeepSeek, Aura's default — reject duplicates, so uniqueness is a property the harness must
  enforce, not one it can rely on. The chosen direction is a per-turn **round ordinal** in
  the child operation key, discriminating at the round boundary the way hermes does.

- What remains open for Phase 45 is narrower and is a discuss/research item, not a blocker:
  confirm the round-ordinal shape against Aura's own dispatch loop before building (ROADMAP
  Phase 45 says so explicitly), and decide how far MEM-04/05 entity resolution goes here
  versus in Phase 49.

- ~~Phase 48's un-defer step needs a live before/after comparison against
  `aura.tool_invocations` in place of an eval harness (ACC-02).~~ **CLOSED 2026-08-25 by the
  evidence itself.** That comparison is what killed the phase: `tool_search` measures 164 calls
  and 0 failures over a deferred tail of 100+ tools, and all 22 errors across 531 invocations are
  execution failures, not selection failures (amendment #139).

- **Open, and deliberately unscheduled:** the MCP-01 and MCP-03 trust tripwires do not exist
  (`46-09-SUMMARY.md`). A ratified trust posture with no named test is one refactor away from
  silent regression. Not re-scheduled — operator's call — recorded so it is a known edge.

- **Partially closed 2026-08-25:** amendment #133's empty accepted answer and per-pause decision
  policy defects are closed with fail-closed wire/Runner/database tests, migration 0102, 100%
  policy mutation, and a real-agent E2E. **Only pending-approval expiry remains open**, tracked in
  `.planning/todos/pending/approval-resume-defects.md`; it remains a resume-path security gap rather
  than tool-surface ceremony.

- 45.1-08 (phase close) must re-run bash scripts/coverage_docker.sh (full aggregate 85% owned-surface gate) once plan 45.1-04 lands and the tree is quiescent -- not run during 45.1-03 because a concurrent unrelated in-flight session held compose.yaml and internal/agent/mcptools/bridge_risk.go dirty in the same checkout
- 45.1-08 must also run a mutation spot-check on internal/agent/mcptools/bridge_risk.go -- not run during 45.1-04; the file is at 100% per-function coverage and bridge_supervisor.go scored 99/99, but neither is a mutation score for this file
- 45.1-08 must also cover 45.1-07: no mutation spot-check on elicitation.go/elicitation_consent.go, and no LIVE E2E of a real mounted server issuing a real elicitation to a real channel -- the in-memory pair proves the protocol path, not a Telegram delivery
- 46-05 (calendar fork curation) deleted 14 orphaned MSTest files (2062 LOC) for the raw tool classes it removed, with no replacement unit coverage written for the merged `CalendarActionTool` -- flagged for a future fork-side follow-up, not something this milestone's own coverage gates measure (aura-pim-mcp is an external repo)
- 46-05 found `ci.yml` structurally never triggers on `aura/pim-sidecar` (push/PR to `main` only) -- if a future plan expects a green `ci.yml` run on this branch as evidence, it will not exist; use `aura-publish-image.yml`'s run history plus a live pulled-image probe instead
- ~~**52-04 MUST wire the steer inbox from config.**~~ **CLOSED 2026-08-25 by 52-04** (dbec0dcc4/03b7d7d32): wired at the composition root with a cap-pinning test. Original text:  `internal/steer/inbox.go` carries package-level fallbacks `defaultMax=32` / `defaultMaxBytes=32768`, while the ratified amendment #132 item 10 values in `internal/config/config_agui_steer.go` are `Max=8` / `MaxBytes=16384`. There are currently ZERO non-test callers of `steer.New`, so nothing yet resolves the disagreement. If 52-04 constructs the inbox with a zero `steer.Config`, the caps silently become 4x the ratified numbers and no test fails -- the D-11 catalogue-vs-loader drift reproduced one layer down. 52-04 must pass `config.AGUISteer` explicitly AND add a test pinning the wired caps to the config values.
- **PHASE 52 DOES NOT CLOSE: the Telegram leg of STEER-05 is not live-proven, and cannot be from this host.** The bot long-polls `getUpdates` against the real Telegram Bot API; there is no local-bot-api sidecar, no Telethon/Pyrogram session, and no API_ID/API_HASH in .env, so an inbound message cannot be scripted as a real Telegram user. Three claims need a HUMAN with their own Telegram client: (1) a plain-text message redirects a live turn with the exact turnSteeredMessage echo; (2) a photo sent mid-turn is queued and its own turn ACTUALLY RUNS afterwards (a conversation-turn row, not just a reply promising it); (3) /cancel during a queued photo announces turnQueuedNotDeliveredMessage rather than going silent. 52-06's unit tests assert the wiring but ACC-01 says a green suite alone is not evidence.
- **52-08 process miss, recorded rather than buried:** the earliest backend tests (23:31:59Z-23:49:20Z) ran against the PRIOR container incarnation whose build commit was not verified first, against the plan's own "verify BEFORE anything else" instruction. Not blind -- the pre-fix RESUME-01 500 was observed in that window, proving the prior container ran genuinely pre-fix code -- and the browser leg ran after the 23:54 rebuild.
- **STEER-03's user_message_fallback delivery branch is unproven:** every attempt to land a steer while the history tail was NOT a tool result raced drain-point A and lost, landing as auto_delivery_next_turn instead. Would need a test-only instrumented delay in drain-point A, or a scenario shape where the tail is reliably a plain assistant message.
- **52-07 leaves must-have truth #1 literally unsatisfied, by measurement, and it needs an amendment.** The 52-07 plan's truth #1 says "the input is not dead and the send is not disabled"; its action text (line 280) says "whatever the Step-1 measurement requires -- a sendDisabled change AND/OR the dedicated control". The measurement forced the dedicated control, so assistant-ui's Send REMAINS disabled during a live run and the steer goes through a separate affordance. Truth and action text contradicted each other inside the same plan; per PRD-first (misura poi emenda) the measurement wins and the DOCUMENT must be corrected with date + evidence. 52-08 must either carry that amendment or the verifier will read truth #1 as an unmet must-have. Not a code defect -- a record defect.
- **52-07's dist freshness is unproven by CI on this host.** The bundle was produced by a docker webbuild extraction rather than by `scripts/web_dist_freshness.sh`, which needs a native Linux Node 24. The CI job `web-dist-freshness` is the only real check; confirm it green on push before treating the committed dist as canonical.
- Env catalog gap (found in 45.1-07, PRD-amendment shaped): the whole AURA_MCP_* family is uncatalogued in prd.md -- AURA_MCP_MOUNT_TIMEOUT and AURA_MCP_SHUTDOWN_TIMEOUT are absent, AURA_MCP_CALL_TIMEOUT_SEC appears only in amendment prose, and AURA_MCP_ELICITATION_TIMEOUT_SEC was deliberately not added alone

## Deferred Items

Items acknowledged and carried forward from previous milestone close:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| Context | CTX-V2-01 (LLM summarization rung) | **Promoted by implementation 2026-08-12** (L3 compaction), not by a spike — Phase 53 deleted. Its anti-thrash/cooldown/fallback guards were the spike's deliverable and are unverified in the shipped code | v2.1.0 requirements definition |
| Context | CTX-V2-02 (durable cross-restart anti-thrash state) | Deferred | v2.1.0 requirements definition |
| Tool surface | TOOL-V2-01 (merge fs_glob/fs_grep) | Deferred. Was promoted to TOOL-11 on 2026-08-05 on TOOL-01's slot cap; both deleted 2026-08-25, so it is back here and the vendor evidence against the merge stands | v2.1.0 requirements definition |
| Tool surface | TOOL-V2-02 (provider reasoning-block replay) | Deferred, not needed by current provider | v2.1.0 requirements definition |

## Session Continuity

Last session: 2026-08-26T17:58:22.565Z
Stopped at: Phase 51 context gathered; design-gate spike is the next action
exited at its CONTEXT.md gate — Phase 45 has no CONTEXT.md, and discuss-phase must run as a
top-level command (nested invocation breaks AskUserQuestion, GSD #1009). No phase directory
was created and no planning agents were spawned.

While reporting that, STATE.md was found describing the milestone's *first* shape (8 phases,
45-52, 52 requirements) rather than its current one. ROADMAP.md and the REQUIREMENTS.md
traceability table were already correct; only the prose around them had drifted. Reconciled
on 2026-08-13: STATE.md frontmatter `total_phases` 8→10, current position 45-of-52→45-of-54,
build order extended with Phases 51/52 and the 53/54 renumber, requirement count re-measured
to 77, the `tool_call_id` blocker marked resolved by `657c9e383`, and the CTX-V2-01 deferral
re-pointed at Phase 53. REQUIREMENTS.md's two stale prose lines corrected to match.

Resume file: .planning/phases/51-durable-delegation/51-CONTEXT.md
Next action: none assigned. Phase 52 is the next in order and is plan-ready, but nothing is
scheduled — the operator drives.
