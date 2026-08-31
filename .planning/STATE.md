---
gsd_state_version: 1.0
milestone: v2.1.0
current_phase: 49
current_phase_name: Memory tiers
current_plan: 4
status: executing
stopped_at: Completed 49-08-PLAN.md
last_updated: "2026-08-31T23:47:22.689Z"
last_activity: 2026-08-31
last_activity_desc: Completed Phase 49 Plan 08
state_head: 263848a5d7076ec3ac39fc93f14fb70e3b5a8379
progress:
  total_phases: 8
  completed_phases: 2
  total_plans: 62
  completed_plans: 54
milestone_name: HERMES-CLAUDE_PARITY
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-08-05)

**Core value:** When Aura says she did something, she did it — and she can find what she knew.
**Current focus:** Phase 49 — Memory tiers

## Current Position

Phase: 49 (Memory tiers) — EXECUTING
Phase 51 is complete: 14/14 plans, 5/5 success criteria, and 12/12 requirements verified.
Status: Ready to execute
Current Plan: 4
Total Plans in Phase: 14

**The roadmap shrank on 2026-08-25 (operator decision).** Phases **47** (tool-surface ceremony
strip), **48** (un-defer and merges) and **53** (summarization spike) are DELETED, not annotated —
their blocks are gone from ROADMAP.md and the **23 requirements** they carried are gone from
REQUIREMENTS.md. The evidence lives in the PRD, which is where measurements belong: amendment
**#139** (`tool_search` = 164 calls / 0 failures against a deferred tail of 100+ tools, which
falsifies TOOL-01's *"hard cap, not a target"*), **#134** (TOOL-01's `comms` row is unbuildable
without reopening D-17 or D-27) and **#137** (Phase 53's deliverable shipped as L3 compaction on
2026-08-12). Execution order is now `45 ✓ → 45.1 ✓ → 46 ✓ → 52 → 50 → 49 → 51 → 54`.

Phase 46 is closed at 9/9 dispositions: seven plans executed, 46-08 is the measured no-go recorded
by amendment #131, and 46-09 was closed by the operator. The old 46-06/07/09 summaries and draft
validation are historical execution records, not the current work queue.

Current evidence (2026-08-28) supersedes their stale gaps. External fork commit
`5909c808f75bb1c612256666dd0f1aacf6921dd4` restores exhaustive coverage of the merged 29-action
`CalendarActionTool`: 233/233 tests pass, including every dispatch arm, destructive validation,
unknown actions and opaque event references; its CI and image-publish workflows are green. Aura
pins the immutable published index at
`sha256:1d0e9c3f62aba446eb123ea39d5abb9d97e6be7784d2dac403ea1e1e5e4a6986`, and the running
`aura-pim-mcp` container is healthy on that exact digest. Aura's `calendar_integration` tier passed
under `-race` against the pin, proving the one-tool schema, two-identity isolation, and the complete
`get_calendar_events` → opaque `eventId` → `get_calendar_event_details` round-trip with no
caller-supplied `accountId`. MCP-05 is therefore no longer half-proven.

The two trust-posture tripwires that 46-09-SUMMARY called absent are present on the current tree:
`TestBridge_CapsDescriptions`/`TestBridge_CapsManifestSummaries` forbid distrust framing while
retaining byte caps, and `TestBridgedTool_Execute_MarksResultTrusted` pins MCP result provenance.
The remaining unchecked MCP-02 item in REQUIREMENTS is its separate live unlisted-server proof;
it is a roadmap acceptance obligation, not an unrecorded Calendar fork gap.

Last activity: 2026-08-31 — Completed Phase 49 Plan 08

Progress: [█████████░] 87% (54/62 milestone plans)

## Performance Metrics

**Velocity:**

- Total plans completed: 23
- Average duration: — min
- Total execution time: 0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 45 | 9 | - | - |
| 51 | 14 | - | - |

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
| Phase 51-durable-delegation P01 | 5h | 3 tasks | 19 files |
| Phase 51 P04 | 215min | 3 tasks | 21 files |
| Phase 51 P03 | ~55 min | 2 tasks | 12 files |
| Phase 51 P05 | 40min | 2 tasks | 6 files |
| Phase 51 P10 | ~1h30m | 2 tasks | 22 files |
| Phase 51-durable-delegation P06a | 90min | 1 tasks | 16 files |
| Phase 51 P06b | ~4h (2 sessions) | 3 tasks | 22 files |
| Phase 51 P07 | single session (continuation) | 2 tasks | 7 files |
| Phase 51 P09 | ~3h (continuation, after prior-session Task 1/2) | 3 tasks | 30 files |
| Phase 51 P11 | 375min | 5 tasks | 44 files |
| Phase 51 P12a | 45min | 2 tasks | 13 files |
| Phase 51 P12b | 9h38m | 4 tasks | 209 files |
| Phase 49 P01 | 1h 5m | 2 tasks | 11 files |
| Phase 49 P02 | 1h 24m | 2 tasks | 8 files |
| Phase 49 P06 | 56min | 2 tasks | 9 files |
| Phase 49 P07 | 1h 9m | 3 tasks | 12 files |
| Phase 49 P03 | 51 min | 2 tasks | 11 files |
| Phase 49 P08 | 29min | 2 tasks | 11 files |

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
- [Phase 46]: 46-05's deleted raw-tool MSTest coverage was replaced after the phase by fork commit `5909c808f75bb1c612256666dd0f1aacf6921dd4`: 233/233 tests now cover all 29 merged dispatch arms and destructive validation paths; CI/publish and Aura's immutable pin were verified 2026-08-28
- [Phase 51]: 51-01: minted a new idempotency.ScopeSwarmDelegation trusted root for the daemon-resident delegation claim loop, mirroring the scheduler's own root-minting pattern
- [Phase 51]: 51-01: scoped IngestionJobWorker.JobType per typed worker after live driving surfaced a cross-worker claim race between asset-processing and swarm delegation
- [Phase 51]: D-10 write-path shape: split-write-shape — MemoryUpsertFactInput.Source got its own MemoryUpsertFactWriteSource with no run_id field; memory_forget/toHits keep MemoryFactSource.RunID unchanged
- [Phase 51]: D-10 actor transport: host-derived actor rides HTTP connection headers on a per-actor MCP session (Rule-4 expansion, operator-approved), not JSON-RPC _meta
- [Phase 51]: D-11: worker supersede refusal reuses existing FactWrite{Refused,Reason} fields, zero Command calls issued for a refused correction
- [Phase 51]: D-09 concurrency fix: CAS then server-side append then explicit ArcadeDB transaction each independently failed under real Go goroutine concurrency; final fix is a per-fact_key striped in-process mutex (fact_lock.go) plus the transaction kept as cross-process defense-in-depth, verified with 70+ live -race stress runs, zero failures
- [Phase 51]: 51-03: widened the existing swarmRunner interface (Run(ctx, goals, context)) instead of adding a second interface, per 51-PATTERNS.md's explicit instruction
- [Phase 51]: 51-03: exported SwarmCaps and swarm.MaxDepth() so cmd/aura's composition root can construct/read them cross-package, without adding AURA_SWARM_MAX_DEPTH to the Tier A/B knob registry
- [Phase 51]: workerRegistry(rc RunConfig) not workerRegistry(parent, depth): the plan's two-arg signature could not actually bound recursion since the shared static RunnerAdapter's Depth never advances across nesting levels — A literal (parent, depth) reading would reuse the same tool object at every nesting level, pinning checkDepth's comparison at depth=1 forever and turning any AURA_SWARM_MAX_DEPTH>2 into unbounded recursion (T-51-18 DoS). The fix rebinds swarm_spawn to a fresh RunnerAdapter{Depth: rc.Depth+1} per grant.
- [Phase 51]: 51-10 widened aura.pending_notifications (migration 0105) with a nullable steer_queue_id sibling to run_id rather than reusing agent_job_runs' FK or a second outbox table — the row the absent-operator nudge retries is a steer_queue row, not the (already-succeeded) delegation job; CHECK (run_id IS NOT NULL OR steer_queue_id IS NOT NULL) enforces exactly one owner
- [Phase 51]: 51-10 claims a steer_queue nudge row (MarkSteerRowNudged's conditional UPDATE) BEFORE pushing to the channel, not after — generalizes DrainSteerRows' "the drain IS the claim" idiom to a sweep that spans a network call and cannot hold one transaction open across it; proven under real concurrency against live Postgres (TestNudgeOnceUnderConcurrency)
- [Phase 51]: 51-10 wraps the RECORDED conversation copy with an explicit worker/goal attribution (attributedWorkerReport, T-51-38) but leaves the pushed steer copy raw/unchanged from 51-01 — one shared wrapper would have been wrong for either reader, since the steer copy earns its own attribution downstream at drain time
- [Phase 51]: [Phase 51] 51-06a: checkpoint resolved `new-columns` — owning_worker_id is a NEW host-written column; proxied_from_child_id/proxied_tool_call_id keep meaning exactly what they mean today (synchronous, model-relayed). paused_states_worker_attribution_exclusive CHECK makes the two mutually exclusive at the DB layer, enforcing T-51-47 rather than merely documenting it.
- [Phase 51]: [Phase 51] 51-06a: MarkResumedBatchTx's map value type changed to FencedResumeAnswer (Tx-bound internal method only); the PUBLIC MarkResumedBatch/PauseStore interface signature stayed byte-identical to avoid rippling through 5+ existing test fakes across the repo.
- [Phase 51]: [Phase 51] 51-06a: PoolResumeCommitter.CommitResume (single-claim) was also switched onto the fenced path, not only CommitResumeBatch -- ResumeClaim.ExpectActionID is one shared struct field for both, so leaving CommitResume unfenced would have been a silent gap for whichever resume shape 51-06b needs.
- [Phase 51]: [Phase 51] 51-06b: a worker pause's expiry trace is a plain ASSISTANT turn in the origin conversation, not the RoleTool answer ExpirePendingApprovals writes -- the worker's ask_user tool_call lives in its own persisted history, so a RoleTool turn keyed by it in the origin conversation would be an orphan (wire-invalid); same shape 51-10's DelegationDelivery used to surface the question
- [Phase 51]: [Phase 51] 51-06b: the TTL sweep's read joins from aura.ingestion_jobs into paused_states on the pause TOKEN this park cycle minted (ListExpiredAwaitingInputJobs), keeping internal/askuser untouched and the sweep per-identity/RLS-scoped like the observer; pause.created_at is the TTL clock as in ListExpiredPendingApprovals
- [Phase 51]: [Phase 51] 51-06b: PoolWorkerPauseExpirer commits fenced claim + trace + queue-row resolution in ONE db.WithIdentityTx (D-08 extended to the queue row); a resolution matching zero rows after a successful claim is a hard rollback, never a skip. ExpireWorkerPauses takes Lister/Expirer per call (WorkerPauseSweepDeps) rather than widening runner.Deps for one sweep
- [Phase 51]: [Phase 51] 51-06b: recovered from a session that died mid-plan with ~2000 uncommitted lines and notes describing Task 3 files that never existed; every claim was re-verified in WSL before commit and Task 3 was rebuilt RED->GREEN. One TestNudgeSkipsDrained (51-10) flake seen in 1/3 full swarm db_integration runs, green in isolation -- recorded, not hidden
- [Phase 51]: [Phase 51] 51-07: shared transcriptPath hardening between dumpTranscript (writer) and ReadTranscript (reader) so both enforce one path-traversal rule; report_test.go stayed byte-identical
- [Phase 51]: [Phase 51] 51-07: SetSwarmTranscripts wiring lands in cmd/aura/serve_agui.go's existing wireAGUIServer (not serve.go, which the plan's files_modified listed) -- the already-correct composition point for every other agui seam
- [Phase 51]: [Phase 51] 51-09: D-03 replaces the ~240s-effective wall clock with an inactivity deadline (child_staleness.go) that resets on every worker event; the boot gate refuses idle >= lease so a reclaim can never race a live goroutine
- [Phase 51]: [Phase 51] 51-09: Task 3's live checkpoint found and this plan fixed three defects the design measurement could not see -- SC#1's delivery failing under RLS because deliverSuccess/openPauseAndPark called Deliver on an identity-less ctx (defect A), a reaped shell_exec leaving its process running in the sandbox because Exec never signalled the box-side process group (defect E, fixed by reusing ExecStream's own PID-file kill mechanism), and compose.yaml never mapping .env's loop budget (defect D) -- recorded as PRD Amendment #171
- [Phase 51]: [Phase 51] 51-09: internal/runner's 51-06b expiry-trace write was audited for the same RLS gap defect A found and cleared -- it threads identity explicitly through db.WithIdentityTx rather than reading it off ctx
- [Phase 51]: [Phase 51] 51-09: two findings recorded but not fixed, both named decisions for plan 51-08 -- Finding B (the shipped retry policy re-runs a reaped worker AND a record-only-failure worker identically) and Finding C (AURA_SWARM_CHILD_IDLE_SEC equals AURA_LLM_TOTAL_TIMEOUT_SEC by default, so an upstream stall and a genuine reap both read "stalled")
- [Phase 51]: 51-11 keeps delegation delivery origin-scoped: a cockpit-owned conversation is not pushed to Telegram; a Telegram proof starts from a Telegram-owned conversation.
- [Phase 51]: 51-11 roots Telegram mutating turns with convID(chatID) plus inbound messageID; chatID identifies the private conversation, while messageID prevents separate turns from sharing one idempotency root.
- [Phase 51]: 51-11 renders grouped delivery from the complete rows returned by the claiming UPDATE, never from the earlier candidate snapshot.
- [Phase 51]: 51-12b subscribes worker clients to native named AG-UI EventSource records and treats terminal RUN_FINISHED/RUN_ERROR EOF as successful completion; only a pre-terminal transport error selects the artifact fallback.
- [Phase 51]: 51-12b keeps the complete worker report in the Markdown artifact and sends only a rune-bounded projection through the steer rail.
- [Phase 51]: 51-12b closes private Telegram evidence from structural timing and counts only; message text, identifiers, screenshots and session data are never retained.

- [Phase 51]: 51-08 accepts Amendment #183's six-verdict fresh-image result instead of fabricating or rerunning the obsolete monolithic drive-sc.sh.
- [Phase 51]: Amendment #177 retires the quality-snapshot freshness gate; plan 51-08 did not run or recreate the deleted script and did not touch the historical snapshot.
- [Phase 51]: Amendment #187's four authenticated Ollama runs passed, but a later shared-container recreation invalidates the old process tuple as a current final no-restart baseline; crash-after-partial-side-effects remains open.
- [Phase 49]: Phase 49 uses measured Amendment #201 because #181 and #200 were already occupied; all executable ancestry gates bind to #201.
- [Phase 49]: Phase 49 evaluator keeps tier contribution separate from actual backend path and represents absent final evidence as not_observed.
- [Phase 49]: PostgreSQL remains the only conversation authority; ArcadeDB stores stable source refs, hashes, and derived high-water metadata only.
- [Phase 49]: Conversation projection uses a bounded ordered queue plus full source replay and pruning, with no outbox or independent retention store.
- [Phase 49]: Plan 49-07 is the sole owner that aggregates conversationSchemaStatements into EnsureMemorySchema after Wave-2 ownership clears.
- [Phase 49]: HAS_TURN and NEXT_TURN use regular edges with unique endpoint indexes for deployed ArcadeDB 26.8.1 replay compatibility.
- [Phase 49]: Phase 49 Plan 06 keeps authenticated identity and actor outside MemoryBatchRequest; the host passes MemoryBatchActor separately.
- [Phase 49]: Phase 49 Plan 06 commits the identity-bound idempotency receipt in the same ArcadeDB transaction as the final graph diff.
- [Phase 49]: Phase 49 Plan 06 retries the complete memory decision from fresh committed state and performs no embedding or telemetry side effect inside the retry loop.
- [Phase 49]: Post-commit projection offers the authenticated identity only after the source append returns; the projector pages PostgreSQL instead of guessing a sequence outside the transaction. — PostgreSQL remains the only conversation authority and reconciliation repairs any lost offer.
- [Phase 49]: One chat-boot ConversationProjector instance is shared by Runner, deletion recovery, periodic reconciliation, and shutdown. — A single ordered process-lifetime worker preserves source order and provides one join owner.
- [Phase 49]: Conversation projection reuses TenantClients and DeleteReconciler; no outbox or independent retention authority is introduced. — Existing tenant credentials enforce identity isolation and full source replay closes the crash window.
- [Phase 49]: Nested ArcadeDB vector.fuse calls independently rank dense plus lexical fact and conversation sources before final rank-only RRF; no Go fusion helper or fixed tier quota exists.
- [Phase 49]: Recall evidence contribution and executed backend path remain independent and populate the response and OTel from the same result.
- [Phase 49]: Open and scroll cursors are versioned canonical base64url JSON treated as unsigned and untrusted, with every request field revalidated before access.
- [Phase 49]: Reasoning remains a reserved explicit mode until Plan 49-04 connects its isolated graph contract.
- [Phase 49]: Phase 49 Plan 08 carries active context as the host tool-call session ID plus per-turn request ID, and the server requires the turn ID to match the actor run header.
- [Phase 49]: Phase 49 Plan 08 resolves OAuth identity before active-source decode and converts only identity-owned conversations into sorted negative filters.
- [Phase 49]: Phase 49 Plan 08 keeps active-source state out of MemoryRecallInput and MCP _meta; only memory_recall consumes the host header.

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
- ~~46-05 deleted 14 orphaned raw-tool MSTest files without replacement coverage.~~ **CLOSED 2026-08-28:** fork commit `5909c808f75bb1c612256666dd0f1aacf6921dd4` adds exhaustive 29-action route and destructive-validation coverage (233/233), with green CI/publish and an immutable Aura pin.
- 46-05 found `ci.yml` structurally never triggers on `aura/pim-sidecar` (push/PR to `main` only) -- if a future plan expects a green `ci.yml` run on this branch as evidence, it will not exist; use `aura-publish-image.yml`'s run history plus a live pulled-image probe instead
- ~~**52-04 MUST wire the steer inbox from config.**~~ **CLOSED 2026-08-25 by 52-04** (dbec0dcc4/03b7d7d32): wired at the composition root with a cap-pinning test. Original text:  `internal/steer/inbox.go` carries package-level fallbacks `defaultMax=32` / `defaultMaxBytes=32768`, while the ratified amendment #132 item 10 values in `internal/config/config_agui_steer.go` are `Max=8` / `MaxBytes=16384`. There are currently ZERO non-test callers of `steer.New`, so nothing yet resolves the disagreement. If 52-04 constructs the inbox with a zero `steer.Config`, the caps silently become 4x the ratified numbers and no test fails -- the D-11 catalogue-vs-loader drift reproduced one layer down. 52-04 must pass `config.AGUISteer` explicitly AND add a test pinning the wired caps to the config values.
- **PHASE 52 DOES NOT CLOSE: the Telegram leg of STEER-05 is not live-proven, and cannot be from this host.** The bot long-polls `getUpdates` against the real Telegram Bot API; there is no local-bot-api sidecar, no Telethon/Pyrogram session, and no API_ID/API_HASH in .env, so an inbound message cannot be scripted as a real Telegram user. Three claims need a HUMAN with their own Telegram client: (1) a plain-text message redirects a live turn with the exact turnSteeredMessage echo; (2) a photo sent mid-turn is queued and its own turn ACTUALLY RUNS afterwards (a conversation-turn row, not just a reply promising it); (3) /cancel during a queued photo announces turnQueuedNotDeliveredMessage rather than going silent. 52-06's unit tests assert the wiring but ACC-01 says a green suite alone is not evidence.
- **52-08 process miss, recorded rather than buried:** the earliest backend tests (23:31:59Z-23:49:20Z) ran against the PRIOR container incarnation whose build commit was not verified first, against the plan's own "verify BEFORE anything else" instruction. Not blind -- the pre-fix RESUME-01 500 was observed in that window, proving the prior container ran genuinely pre-fix code -- and the browser leg ran after the 23:54 rebuild.
- **STEER-03's user_message_fallback delivery branch is unproven:** every attempt to land a steer while the history tail was NOT a tool result raced drain-point A and lost, landing as auto_delivery_next_turn instead. Would need a test-only instrumented delay in drain-point A, or a scenario shape where the tail is reliably a plain assistant message.
- **52-07 leaves must-have truth #1 literally unsatisfied, by measurement, and it needs an amendment.** The 52-07 plan's truth #1 says "the input is not dead and the send is not disabled"; its action text (line 280) says "whatever the Step-1 measurement requires -- a sendDisabled change AND/OR the dedicated control". The measurement forced the dedicated control, so assistant-ui's Send REMAINS disabled during a live run and the steer goes through a separate affordance. Truth and action text contradicted each other inside the same plan; per PRD-first (misura poi emenda) the measurement wins and the DOCUMENT must be corrected with date + evidence. 52-08 must either carry that amendment or the verifier will read truth #1 as an unmet must-have. Not a code defect -- a record defect.
- **52-07's dist freshness is unproven by CI on this host.** The bundle was produced by a docker webbuild extraction rather than by `scripts/web_dist_freshness.sh`, which needs a native Linux Node 24. The CI job `web-dist-freshness` is the only real check; confirm it green on push before treating the committed dist as canonical.
- Env catalog gap (found in 45.1-07, PRD-amendment shaped): the whole AURA_MCP_* family is uncatalogued in prd.md -- AURA_MCP_MOUNT_TIMEOUT and AURA_MCP_SHUTDOWN_TIMEOUT are absent, AURA_MCP_CALL_TIMEOUT_SEC appears only in amendment prose, and AURA_MCP_ELICITATION_TIMEOUT_SEC was deliberately not added alone
- ~~**51-10 owes a WSL/CI `-race` run**~~ — **discharged 2026-08-28** by the phase-51 orchestrator. Run natively in WSL (go1.26.6, `CGO_ENABLED=1`, gcc 15.2): `go test -race ./internal/swarm/... ./internal/config/... ./internal/steer/... ./internal/cron/... ./cmd/aura/...` — exit 0, zero races (`internal/swarm` 3.388s, `internal/config` 1.440s, `internal/steer` 1.079s, `internal/cron` 1.173s, `internal/cron/handlers` 1.264s, `cmd/aura` 35.051s). The SUMMARY's "this Windows host has no cgo toolchain" was true of the Windows-native toolchain only; CLAUDE.md's WSL environment was available and is the project's authoritative race host. What this does NOT prove: the `db_integration`/`arcadedb_integration` tiers under `-race`, which were not part of this run.
- **51-10's channel fan-out and owns-but-failed leg are unexercised by real multi-channel concurrency**: Telegram is the only shipped `ChannelDeliverer`, so the candidate-choice-between-two-channels path has never actually been taken live; the `pending_notifications` retry/backoff itself is the SAME machinery `internal/cron` already relies on, reused not re-verified (Amendment #154 disclaimer, per plan text).

## Deferred Items

Items acknowledged and carried forward from previous milestone close:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| Context | CTX-V2-01 (LLM summarization rung) | **Promoted by implementation 2026-08-12** (L3 compaction), not by a spike — Phase 53 deleted. Its anti-thrash/cooldown/fallback guards were the spike's deliverable and are unverified in the shipped code | v2.1.0 requirements definition |
| Context | CTX-V2-02 (durable cross-restart anti-thrash state) | Deferred | v2.1.0 requirements definition |
| Tool surface | TOOL-V2-01 (merge fs_glob/fs_grep) | Deferred. Was promoted to TOOL-11 on 2026-08-05 on TOOL-01's slot cap; both deleted 2026-08-25, so it is back here and the vendor evidence against the merge stands | v2.1.0 requirements definition |
| Tool surface | TOOL-V2-02 (provider reasoning-block replay) | Deferred, not needed by current provider | v2.1.0 requirements definition |

## Session Continuity

Last session: 2026-08-31T23:47:22.316Z
Stopped at: Completed 49-08-PLAN.md
The accepted fresh-image delivery envelope is reconciled in 51-VALIDATION.md at 6/6 and 9.9/10.
The final image passed the complete repository Playwright suite (145 pass, 39 intentional skips,
zero failures) and four hot-route cycles without a restart or an OpenRouter request.
Crash-after-partial-side-effects remains an explicit residual risk and is not an exactly-once claim.

Resume file: None
Next action: plan Phase 49 from .planning/phases/49-memory-tiers/49-CONTEXT.md.
