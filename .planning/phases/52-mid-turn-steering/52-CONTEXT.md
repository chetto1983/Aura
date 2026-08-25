# Phase 52: Mid-turn steering - Context

**Gathered:** 2026-08-25
**Status:** Ready for planning

<domain>
## Phase Boundary

The operator can type into a turn that is already running and have the message land — redirecting
the work at the next round boundary instead of waiting for the turn to end or killing it and
starting over. Delivery, addressing, trust framing, persistence and the cockpit/channel affordances
that carry it are in scope. Delegated-child steering is NOT (amendment #132 (D)), and neither is a
hard interrupt — the escalation ladder is steer, then cancel.

**A large part of this phase was ratified before the discussion.** PRD amendment #132 is a
design-gate ratification committed on 2026-08-24, before any steering code existed. What follows
does not restate it. It records the decisions #132 left open, plus **four places where reading the
reference implementations during this discussion contradicted what #132 ratified** — those are
listed under "Corrections required before code" and must land as amendment lines FIRST, per
CLAUDE.md's PRD-amendment-before-code rule.

</domain>

<decisions>
## Implementation Decisions

### Addressing and channel reach

- **D-01:** The steer inbox is keyed on **conversation id**, not run id. STEER-05 and the roadmap's
  SC#5 stay inside this phase. Measured reason: `RunRegistry` lives entirely in `internal/agui` and
  is wired only by `cmd/aura/serve_agui.go:81`; a Telegram turn calls `runner.Turn` directly and has
  no runID at all, so a runID-keyed inbox puts channel steering out of reach by construction. This
  is also hermes' shape — its steer sits on `agent._pending_steer`, an agent-scoped slot, never on a
  run id. — **Reversibility:** costly — the key appears in the inbox, every push site, the drain
  lookup and the persisted echo; changing it later touches all four.

- **D-02:** The HTTP route stays **`POST /agent/runs/{runID}/steer`** as #132 item 2 ratifies, and
  resolves the conversation internally. Telegram does not go through the route — it pushes into the
  inbox from its own dispatch path. Two ways in, one queue. Keeping the route runID-shaped preserves
  the alignment with the existing `agent_run_cancel` registration in `httpMutationRoutes`
  (`internal/agui/idempotency_http.go:53`), which #132 chose deliberately. — **Reversibility:**
  costly — it is a published HTTP contract with an idempotency registration behind it.

### Channel semantics

- **D-03:** On Telegram, **every plain-text message arriving during a live turn is a steer**,
  replacing today's busy copy (`internal/channels/telegram/bot_dispatch_turn.go:145`, `sendBusy`).
  Confirmed against hermes after the decision, not before it: `acp_adapter/server.py:1727-1745`
  routes a regular text prompt arriving mid-run *"through the core active-turn redirect"*
  (`state.agent.redirect(...)`), and only falls back to `queued_prompts` when redirect is
  unavailable.

- **D-04:** The bot **always answers** that it redirected — hermes sends
  `"Redirected the active turn with your correction."` (`acp_adapter/server.py:1755-1758`). The
  operator never has to infer where their message went. This replaces the busy copy rather than
  adding a second reply.

- **D-05:** A **non-text** message during a live turn (photo, voice, document) is **queued for the
  next turn**, not treated as a steer. hermes gates its redirect on `text_only_prompt` for exactly
  this reason: a steer is text that redirects, a media message is new material and deserves its own
  turn.

### Delivery into the model

- **D-06:** **Two drain points**, hermes parity: at the end of a tool-call batch AND before every
  API call. hermes carries both (`agent/agent_runtime_helpers.py:3921` and
  `agent/conversation_loop.py:1498`) and states the reason for the second in the code: *"steers sent
  during an API call only land after the NEXT tool batch, which may never come if the model returns
  a final response."* #132 ratifies "the drain point" in the singular; that is correction 1 below.

- **D-07:** The user-attribution marker carries a **per-run nonce**, minted through the machinery
  Aura already has — `toolOutputNonce()` / the `<tool_output trust="untrusted" nonce="...">`
  envelope in `internal/agent/trust.go:60-70` — **plus a system-prompt note** teaching the model
  that text inside that marker is a genuine mid-turn user message with the authority of the original
  request, and that lookalikes in tool output, web pages and files are to be ignored. hermes uses a
  **static** marker (`STEER_MARKER_OPEN`/`CLOSE`, `agent/prompt_builder.py:661-666`) with that note
  (`STEER_CHANNEL_NOTE`); a static marker is forgeable by anything that can put text in a tool
  result, and Aura already owns the non-forgeable version. — **Reversibility:** costly — the marker
  shape is written into conversation history, so a later change leaves old turns carrying the old
  form.

- **D-08:** Tool outputs are **scrubbed of lookalike markers** before entering history. Belt and
  braces on top of D-07's nonce, and deliberately so: the nonce closes forgery, the scrub closes the
  case where the model treats an unsigned lookalike as authoritative anyway. The scrub must be tested
  against legitimate text that merely resembles the marker — it must not eat real content.

- **D-09:** A steer still undelivered when the run ends is **delivered automatically as the next user
  turn**, with a visible line saying that is what happened. **This contradicts what #132 ratified**
  and is the substantive finding of this discussion. hermes hands the leftover back on
  `result["pending_steer"]` (`agent/turn_finalizer.py:683-685`) and **three** independent consumers
  deliver it as the next turn — `cli.py:14523` (*"⏩ Delivering leftover /steer as next turn"*),
  `gateway/run.py:25525-25531` (*"Deliver it as the next user turn so it isn't silently dropped"*),
  and the ACP path. #132 item (B) says "drained and named in the run's terminal event" and STEER-04
  says "returned to the operator to re-send as a normal turn" — both make the human retype what the
  reference implementation delivers on its own. Corrections 1 and 5-6 below. — **Reversibility:**
  costly — it changes what STEER-04 asserts and what SC#3 validates, so the acceptance evidence
  changes with it.

### Cockpit

- **D-10:** The composer **stays the composer**. While a run is live the send becomes a steer and the
  UI echoes that the turn was redirected; the 409 `ErrThreadBusy` on send
  (`internal/agui/server_run.go:102`) goes away for this path. hermes is the applicable witness here
  and LibreChat is not: LibreChat has no mid-turn steering at all, so its answer — textarea live,
  send replaced by stop, `disabled={... || isSubmitting}` (`client/src/components/Chat/Input/ChatForm.tsx:404-412`)
  — is evidence about an abort-only design. This lands #132 item 12's composer contract **inside this
  phase** rather than in a later amendment, which follows from D-12.

### Flags — no dark code

- **D-11:** `AURA_AGUI_RUN_DETACH` resolves to **`true`**: the code wins. `config_agui_run.go:36`
  already reads `true` and `config_agui_test.go:27` records *"campaign passed 10/10"* with
  `AURA_AGUI_RUN_DETACH=false` as the explicit rollback. `config_knobs.go:116`, which catalogues it
  as `false`, is the side that is corrected. #132 refused to decide this and made it a precondition
  of STEER-01; it is now decided.

- **D-12:** `AURA_AGUI_RUN_STEER` ships **default `true`**. The knob exists as the explicit rollback,
  exactly the shape D-11 just endorsed for detach — it is not the switch that keeps the feature off.
  Operator ruling, verbatim: *"un flag a off è = a dark code"*, which is CLAUDE.md's own
  DARK-CODE-IS-FORBIDDEN rule applied to this phase. #132 item 10 ratifies `false` and item 12 defers
  the composer flip to a later amendment; both are correction 4 below.

### Acceptance

- **D-13:** STEER-02 (a steer buys no budget) is proven **both** ways: a code invariant that the drain
  never touches the step counter or the wallclock deadline, pinned by a test, **and** a live A/B —
  the same scenario run steered and unsteered, compared on `aura.conversation_turns`. ACC-01 rules
  that a green suite closes nothing, so the live comparison is the evidence; the invariant is what
  stops a future change from moving the budget unnoticed.

### Claude's Discretion

- The exact wording of the attribution marker and of the system-prompt note (D-07), within the
  constraint that the note must be byte-stable so it stays cache-prefix safe (#132 item 6, amendments
  #16/#29).
- The wording of the Telegram redirect echo (D-04) and of the auto-delivery line (D-09).
- The shape of the `aura.steer` echo frame's payload, beyond #132 item 4's requirement that it be
  ring-buffered like every other frame.
- Which sibling file of `internal/agent/llm_agent.go` carries the drain points — the file is at
  **561/600 LOC**, so they cannot land in it (#132's own measured correction (b)).

### Folded Todos

**`.planning/todos/pending/approval-resume-defects.md`** — all three defects of PRD amendment #133
are folded into this phase. They were scheduled inside Phase 47, which was deleted on 2026-08-25;
they are resume-path defects rather than tool-surface ceremony, and they live in the same mid-turn
input path this phase opens.

1. **No per-tool decision policy.** `resumeAnswers` (`internal/agui/server_project.go:24`) maps
   `resolved→ActionAccept` / `cancelled→ActionCancel` and nothing expresses "this pause may only be
   declined". LibreChat returns **403** here, on the threat model that *the human's POST is
   untrusted*.
2. **An empty answer resumes silently.** `payloadString(nil)` returns `""` and `answerTurn` injects
   it as the `RoleTool` answer, so an accept carrying no payload resumes the model with an empty
   answer instead of being refused. LibreChat 400s on exactly this.
3. **Pending approvals never expire.** No TTL anywhere in `internal/gateway` expires a pending
   approval. LibreChat has `APPROVAL_EXPIRED_ERROR`, `expireApproval()`, and prunes the paused run's
   durable checkpoint.

**Constraint on any fix, carried from the todo:** Aura's idempotency story is stronger than
LibreChat's and must survive. `MarkResumed`'s `RowsAffected==0` gate returns `ErrPauseNotFound` for
an unknown OR already-resumed token, the `WHERE resumed_at IS NULL` conditional update **is** the
idempotency key (D-06 of Phase 45), and `CommitResumeBatch` claims every pause under sorted-token
deadlock-free ordering in ONE cross-store transaction. New validation goes **inside** that
transaction's front door, never as a second path around it.

**Interaction with steering, stated so the planner does not trip on it:** #132 item 7 rules that
`ask_user`-paused runs are terminal and NOT steerable, so the two surfaces do not overlap at
runtime — they share a package, not a code path.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The ratified contract, and what this discussion corrects in it

- `prd.md` §Mid-turn steering — the contract, ratified before the code (**Amendment #132**,
  2026-08-24, REVISED same day) — the design gate. Read it in full; most of the phase is already
  decided there. Its §"What is ratified" enumerates twelve items carried from the study's §11.
- `prd.md` §The approval resume path, measured against LibreChat (**Amendment #133**, 2026-08-24) —
  the three folded defects and their evidence.
- `docs/superpowers/specs/2026-07-23-mid-turn-steering-design.md` (664 lines, **STUDY ONLY**) — the
  original design. **Its line citations have drifted** (#132 correction (b)): §3 cites
  `llm_agent.go:331` and `:439`; the nudge append is at `:481`. Read the seam, never the citation.
- `.planning/todos/pending/approval-resume-defects.md` — the folded todo, with the idempotency
  constraint any fix must respect.

### Reference implementations — read these, do not infer from them

The operator's standing instruction on this milestone is to read hermes and LibreChat before
designing. Every line below was opened during this discussion; the ones marked ⚠ are where the
reference contradicts amendment #132.

- `D:/tmp/hermes-agent/agent/agent_runtime_helpers.py:3921-3975` — `apply_pending_steer_to_tool_results`:
  the tool-result append, the marker, and the re-queue fallback when a batch carries no tool result.
- ⚠ `D:/tmp/hermes-agent/agent/conversation_loop.py:1490-1512` — the **second** drain point, before
  every API call, with the stated reason. #132 ratifies one drain point (D-06).
- ⚠ `D:/tmp/hermes-agent/agent/turn_finalizer.py:675-690` — the leftover steer handed back on
  `result["pending_steer"]` *"so it can be delivered as the next user turn instead of being silently
  lost"*. #132 item (B) and STEER-04 say the operator re-sends it (D-09).
- ⚠ `D:/tmp/hermes-agent/gateway/run.py:25518-25532` and `D:/tmp/hermes-agent/cli.py:14515-14528` —
  the two consumers that actually perform that auto-delivery.
- `D:/tmp/hermes-agent/agent/prompt_builder.py:660-690` — `STEER_MARKER_OPEN`/`CLOSE`,
  `format_steer_marker`, and `STEER_CHANNEL_NOTE` (the trust note, including the ignore-lookalikes
  clause). The marker is **static** there; D-07 replaces it with a nonce.
- `D:/tmp/hermes-agent/acp_adapter/server.py:1720-1760` — a plain text prompt arriving mid-run is
  routed *"through the core active-turn redirect"*, with `queued_prompts` as the fallback, and the
  operator is told `"Redirected the active turn with your correction."` (D-03, D-04, D-05).
- `D:/tmp/hermes-agent/acp_adapter/server.py:2407-2425` — `_cmd_steer`: running → queue on the active
  turn; idle → queue for the next turn. It never refuses.
- `D:/tmp/LibreChat/client/src/components/Chat/Input/ChatForm.tsx:400-415` — send replaced by stop
  while `isSubmitting`. **Evidence about an abort-only design, not a second witness for steering**
  (#132 says the same about LibreChat's server side).

### Aura seams the phase attaches to

- `internal/agui/runregistry.go`, `runsession.go`, `server_run_detach.go`, `server_run_resume.go` —
  amendment #90's shipped run identity: `RunID`, `ThreadID`, `IdentityID` frozen at start, fixed-cap
  replay ring.
- `internal/agui/idempotency_http.go:53` — `httpMutationRoutes`, where `agent_run_cancel` is
  registered; the steer route copies that shape.
- `internal/agent/trust.go:60-70` — `toolOutputNonce()` and the untrusted-data envelope D-07 reuses.
- `internal/conversations/store_append.go:66,129` — `AppendTurn` / `AppendTurnTx`, the drain-time
  persistence path (#132 item 5).
- `internal/agui/translator.go:19,26,35` — the existing `aura.artifact` / `aura.display` /
  `aura.mcp_view` CUSTOM-frame convention the `aura.steer` echo follows.
- `internal/channels/telegram/bot_dispatch_turn.go:145` — `sendBusy`, the reply D-03 replaces.
- `cmd/aura/serve_agui.go:81` — where the RunRegistry is constructed and wired.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets

- **`internal/agent/trust.go`** already mints per-call random nonces and wraps untrusted content in
  `<tool_output source=... trust="untrusted" nonce="...">`. D-07's marker is a second consumer of
  that machinery, not a new one. Inventory-before-invention applies: do not write a second nonce
  minter.
- **Amendment #90's `RunSession`** already carries an owner-scoped addressable identity and a
  fixed-cap replay ring, which is what makes STEER-03's resume-exact echo nearly free.
- **The mid-loop user-role injection pattern is real but does NOT license the steer position.**
  `llm_agent.go:481` and `:489` append `llm.Message{Role: llm.RoleUser}` inside the loop — but at
  content-stop and completion-veto, i.e. after an assistant message, never between a tool-result
  batch and the next call. #132 measured this; the plan must not cite those appends as precedent.
- **`internal/steer` does not exist.** Net-new package, as the study says.

### Established Patterns

- **`aura.*` CUSTOM frames** (`translator.go`) are the existing shape for anything the AG-UI SDK's
  own event types cannot carry. `RunFinishedEvent` has no free payload, which is why D-09's visible
  line is its own frame rather than a field.
- **File-size rule.** `internal/agent/llm_agent.go` is at **561/600 LOC**. Both drain points land in
  a sibling file (`llm_agent_steer.go` or similar) — appending to `llm_agent.go` trips the pre-commit
  file-size hook.
- **Cache invariant.** Steers are tail-only `messages[3..N]` and must never enter the `[0]/[1]/[2]`
  cacheable prefix (#132 item 6, reaffirming amendments #16/#29). The system-prompt note of D-07 is
  the opposite case — it is static and belongs in the stable prefix.
- **Telegram's per-chat busy gate** is the single seam where a channel steer lands. It already has
  the identity scoping (`scopeTurnToIdentity`, fail-closed per HI-03) that a steer needs.

### Integration Points

- `internal/steer` (new) ← pushed by the AG-UI route (cockpit) and by the Telegram dispatch path
  (channel); drained by the two points in the agent loop.
- The inbox lives in memory on the run. **Stated boundary, carried from amendment #133:** this is
  single-replica by construction. A multi-replica Aura would need LibreChat's cross-replica
  signalling, not a bigger queue.
- `config_knobs.go` gains `AURA_AGUI_RUN_STEER` / `_MAX` / `_MAX_BYTES` and has its
  `AURA_AGUI_RUN_DETACH` default corrected (D-11).

</code_context>

<specifics>
## Specific Ideas

**Corrections required before code — the PRD-amendment-first gate.** STEER-06 already requires the
amendment to precede the implementation, and #132 exists. These are corrections to it, and they land
as amendment lines in one commit before any steering code:

1. **#132 item (B) → auto-delivery.** "Drained and named in the run's terminal event" becomes
   "delivered automatically as the next user turn, with a visible line". Evidence: hermes'
   `turn_finalizer.py` + its three consumers (D-09).
2. **#132's single drain point → two.** Evidence: `conversation_loop.py:1490-1512` (D-06).
3. **#132's marker → per-run nonce + system-prompt note + lookalike scrub.** #132 ratifies the
   tool-result append but not the marker's construction; hermes' is static (D-07, D-08).
4. **#132 items 10 and 12 → `AURA_AGUI_RUN_STEER` default `true`, composer contract in-phase.**
   A flag defaulted off is dark code (D-12, D-10).

And three documents that assert the superseded behaviour and must move with the amendment:

5. **`.planning/REQUIREMENTS.md` STEER-04** — "returned to the operator to re-send as a normal turn"
   is now auto-delivery.
6. **`.planning/ROADMAP.md` Phase 52 SC#3** — "returns the message to the operator to send normally"
   is now auto-delivery. SC#5 (channel parity) is unaffected and stays, per D-01.
7. **`.planning/ROADMAP.md` Phase 52 "Depends on"** — it still reads *"Depends on: Phase 51"*, which
   the execution order contradicts: 52 runs **before** 51, and the block's own REVISED note says
   "promoted ahead of Phase 51". Stale prose, fix-on-touch, not a design question.

Also to reconcile in the same pass: **`config_knobs.go:116`** (`AURA_AGUI_RUN_DETACH` catalogued
`false` against a code default of `true`), which #132 named a precondition of STEER-01 and D-11 now
decides.

</specifics>

<deferred>
## Deferred Ideas

- **Child (delegated-worker) steering.** Non-goal, ratified in #132 (D) as a *scope* decision with
  evidence rather than an assumption of simplicity: hermes exposes both `session.steer` and
  `subagent.steer`, and **all** of its recent hardening was on the child path — two commits in one
  evening, for lifecycle ownership and generation binding. If Phase 51 lands delegation, child
  steering arrives carrying a known-hard lifecycle problem.
- **A hard interrupt rung.** The escalation ladder is steer, then cancel, with nothing in between.
  Deliberate divergence from parity, recorded in #132 item 8 — hermes does carry
  `request_hard_interrupt` (`agent/interrupt_compat.py:9`).
- **Steer withdrawal.** No affordance to take back a queued steer; resolved 2026-07-23 as
  "Claude Code parity" and not reopened.
- **A budget-bonus knob.** Resolved the same day, same way. STEER-02 is the guardrail.
- **Multi-replica steering.** Out of scope by construction, see Integration Points.
- **Provider-path verification of the plain-`user` fallback.** #132 requires STEER-01 to verify
  against Aura's real provider path (OpenRouter, llama.cpp — not Anthropic direct) whether a `user`
  insertion at the steer position is accepted. D-06's two drain points and the tool-result append
  need no such verification; the fallback does. Flagged for the researcher rather than assumed.

### Reviewed Todos (not folded)

None — the single matching todo was folded in full.

</deferred>

---

*Phase: 52-Mid-turn steering*
*Context gathered: 2026-08-25*
