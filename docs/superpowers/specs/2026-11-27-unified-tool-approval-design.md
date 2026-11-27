# One approval mechanism: the host parks the call, shows the card, runs the original

Date: 2026-11-27. Status: approach approved by the operator in chat ("sì", option 1). Written for the builder who
plans and implements it. Evidence:
- `.superpowers/sdd/approval-unify/current-state-map.md`, the map of every approval path today with file:line;
- `.superpowers/sdd/approval-unify/hitl-research.md`, the industrial pattern with sources;
- `docs/superpowers/verification/2026-11-27-task-cancel-approval-and-microsoft-sending-address.md`, the live run
  that triggered it.

## Why

On the lab VM, cancelling one task the chat did not create took two approvals and 7 model calls (about 80 s of
model time). The operator: "lentezza e doppia approvazione".

The cause is structural. Four of Aura's approval paths use a **challenge**:
1. The tool or the gateway returns a `*_approval_required` result.
2. The model copies the question and `resume_context` into `ask_user`. That model call alone was 23.8 s, and its
   whole output is a copy of text Aura already has.
3. A resume hook records the accept.
4. The model repeats the call.

The system prompt (`internal/agent/prompt.go:130`) also tells the model to ask before destructive actions, which adds
a model-written question in front of the host's card.

The four challenge paths are:
- the gateway (Destructive tier and `ask` policies);
- `shell_exec`'s destructive patterns;
- `plugin_pack` install and remove;
- `task cancel` of a task the chat did not schedule.

Each has its own in-memory store, digest, question builder, relay text and resume hook. None of them survives a
restart: an accept after a restart commits the answer and then fails in the hook (map §1a "Restart").

Every industrial runtime the research covers does it the other way:
- OpenAI Agents SDK: `needs_approval`;
- Vercel AI SDK: `toolApproval`;
- LangGraph: `HumanInTheLoopMiddleware`;
- Claude Agent SDK: `canUseTool` and PreToolUse.

In all of them the runtime intercepts the call before it runs and stores the pending call with its original name and
arguments. After an accept the runtime executes the stored call; the model never re-emits it. A rejection comes back
to the model as a tool result. Aura already does exactly this in one place: the immediate send of a reviewed email
draft (`ExecuteReviewedMessage`, map §1g).

## Goals

- **The host asks.** When a call needs the operator, the host parks it before it runs and shows Aura's own card. The
  model never writes the approval question and never asks before a gated call.
- **The host runs it.** On accept, the host executes the parked call once, through the gateway funnel, as the first
  step of the continuation run. That step makes no model call and streams like any tool call. The result is committed
  as the tool result of the **original** call id, and the model continues from it: it does not repeat the call.
- **On decline**, the model receives a tool result on the original call id saying the operator declined, with the
  operator's words if any, and that it must not retry.
- **One mechanism.** One interception seam, one pending record, one accept path and one decline path for the four
  challenge paths. Their stores, digests, relay texts and resume hooks are deleted.
- **Durable.** The pending call, and what may run on accept, survive a restart. An accept after a restart either runs
  the call once or refuses it cleanly. It never commits an answer and then fails.
- **Same outcome every time.** Every resolve surface gives the same result: the cockpit approvals API, the AG-UI
  resume, the CLI REPL and Telegram.
- **Measured on the VM.** The foreign-cancel scenario takes at most 4 model calls with a single card.

## Non-goals

- Message drafts (immediate and scheduled) keep their own review card, their edit of the arguments and their resolve
  routes. They are already this pattern, and the operator can edit them, as LangGraph `edit` allows.
- `task schedule` needing approval keeps its accept action, which flips the parked task row to active, and its
  reminder and lapse handling. Phase 2 judges folding it in; this spec does not require it.
- No change to what needs approval. These all stay what they are today:
  - the tiers;
  - the destructive patterns;
  - the foreign-cancel rule;
  - grants and policies;
  - the profile rules: gateway approvals only under `single_user_hardened`, tool approvals on every profile.
- Free `ask_user` (choices, questions) is unchanged.

## Anti-goals (failures even if it "works")

- An approved call runs twice. That covers a retried resolve, two surfaces answering at once, a crash between
  execution and commit, and a restart.
- An approval for one call executes a different call or different arguments than the card showed.
- A headless run (cron, agent_job, background delegation, swarm worker) sits on a card nobody can answer. It must be
  refused with a reason the model can read, as `no_approver` is today.
- The model still writes approval questions, so two cards appear.

## Approaches (decided)

1. **One host-side mechanism on the park shape. Chosen.** It matches the industrial pattern and reuses Aura's own
   park machinery: the host-review seam, the persisted original call, and the `ExecuteReviewedMessage` precedent.
2. **The runner opens the challenge card itself, and the model still repeats the call.** Rejected. It removes the copy
   call but keeps four stores, four hooks and the repeat.
3. **Everything in the gateway, with a context-aware tier.** Rejected. The classifier is pure by design (no DB), and
   the tool-sourced decisions need IO (a box route, a repository review, a task row).

## Design

### The decision: one interface, two sources

A call needs approval when either source says so. The **tool** source applies when the tool implements the reviewer
interface and its review says the call needs the operator. The **gateway** source is the gateway's decision for the
call (Destructive tier or an `ask` policy, after deny policies and standing grants), evaluated **without** acquiring
the operation or reserving. This is the decide-only half of `Decide` (map R5).

The interface generalizes today's `tools.HostReviewer`, which `TaskTool` implements for scheduling. The review
returns:
- whether approval is needed;
- the card: a presentation key and its parameters (the existing `approvaltext` keys `approval.gateway.mutation`,
  `approval.shell.command`, `approval.plugin.install|remove`, `approval.task.cancel`);
- **the arguments that will execute** on accept. They may differ from the model's: `shell_exec` binds the box cwd,
  and `plugin_pack` pins the reviewed commit and content digest (map R1). The card must show exactly what will run;
- for gateway-sourced reviews, the scope options once / session / always (always omitted under an `ask` policy), as
  today.

Reviews run where the IO they need is available; shell needs the box routed. A review error is not an approval: the
call proceeds to normal dispatch, where the tool or the gateway refuses it as today.

Deny policies are checked before any review, so a denied call is refused, never parked (map R12, for the four paths).

### Parking

- **Where it runs.** Interception runs before dispatch, at the point where host review and outbound drafts park
  today (`pauseCalls` pass 2). That is before `execTool`, hence before the gateway acquires an operation (map R6).
- **What is persisted.** The parked call is written with its original name and arguments, as host review does today.
  The persisted assistant turn keeps the original `tool_call`, which avoids the c038fd2b9 failure.
- **The pending record.** It is host-written and durable. It holds:
  - the arguments that will execute, and their fingerprint;
  - the tool name;
  - the presentation;
  - the review source;
  - the scope options.

  It lives in the pause's `resume_context` under one new type, or in a dedicated table if the plan finds the
  context too small or too exposed. That choice is the builder's, and the plan states it. No in-memory challenge
  remains.
- **Siblings (amended 2026-11-27 after plan validation).** Calls in the same assistant turn that need no approval
  **run in that turn**, as in OpenAI Agents, Vercel AI and LangChain (cross-source review). Only the calls that need
  approval are parked.
  - **Why.** Dropping the siblings cost at least one extra model round, with no guarantee the model would emit them
    again.
  - **Scope.** The rule applies to every park source in this project's mechanism.
  - **Ordering.** A sequential batch keeps its order: a call that comes after a parked call in that batch still
    runs. Its result is committed with the pause.
  - **Several parked calls** in one turn give one card each.
  - **`ask_user`.** A turn that also calls `ask_user` keeps today's precedence (map R4).
- **Who never parks.** Headless and delegated runs: no person (`personPresent` false), swarm workers, agent_job and
  background delegation. There:
  - a gateway-sourced need is denied with `no_approver` as today;
  - a tool-sourced need is refused with a tool result the model can read ("needs the operator, who is not in this
    run"), recorded as a deny fact.

  The swarm and agent_job pause rebuilders (map R11) therefore never see a parked call. A test pins it.

### Accept: the host executes the original call once

This generalizes `ExecuteReviewedMessage` (map §4, the precedent's list), split between the resolve and the
continuation run (decision 1 below):
1. In the resolve:
   - take the thread lock;
   - take a **durable one-shot claim** on the pending record, in the resolve's transaction. This is the CAS, like
     the draft's `ClaimSend`. A retried or concurrent resolve finds it taken and runs nothing.
2. The continuation run then takes over (steps 3 and 4).
3. Execute through `execTool` with:
   - a parent operation rooted on the resolve (approval or HTTP scope);
   - model round 1;
   - the original `tool_call_id`;
   - a UUID request id;
   - unobserved dispatch, so the gateway closes the ledger row;
   - an authorization carried in the context.

   The gateway checks that authorization against the key triple and the fingerprint of the stored execute-arguments,
   in place of today's in-memory `Consume`. Under a strict profile the call is reserved and recorded exactly once, with
   the resolving identity as operator. That replaces the hard-coded `"local"` (map R15).
4. Spill the result through the normal tool-result path, outside the resume transaction (map R8). Then commit the
   resume: the answer turn is the tool result on the original call id.

   Draft resumes already append "do not claim success unless …" (map R9). Whether the generic answer suffix applies to
   an executed result is the builder's call; it must not mislabel a real result as an operator answer.
5. A scope of session or always records the grant, as the gateway hook does today, before execution.
6. Continue the turn as today (`continue` directive).

**Crash between claim and commit.** The record is claimed and the outcome unknown. Recovery commits an "outcome
unknown, check before retrying" tool result, as `messagedrafts.Recover` does for drafts, and never executes again.

### Decline and expiry

- **Decline** commits a tool result on the original call id: "The operator declined `<tool>` `<short what>`. Do not
  retry this call." The operator's words are included when given. No execution and no hook.
- **The declined-call marking** collapses onto the parked call's own `tool_call_id`, replacing the
  `gateway_approval` + `withheld_tool_call_id` stamp (map R13). The cockpit fold follows.
- **Expiry** through the generic sweep (48 h, `AURA_ASKUSER_PAUSE_TTL_SEC`) commits an "expired, not run" tool
  result and never executes.

### Cards on every surface

Each surface gets the card from the stored presentation. The question no longer has to be parsed back out of a
model-written string, so the "question must match exactly" checks (CR-01) disappear with the copy:
- the cockpit (`web/src/approvals`);
- Telegram (`RenderItalian` and the scope keyboard);
- the CLI REPL ([y/N]).

The Italian and English renderings stay in parity.

### Model doctrine

- **System prompt.** The operator decided on 2026-11-27: "Mai, chiede l'host". `prompt.go:130` (and `:136` if
  related) is rewritten. The model **never** asks for approval before a tool call. It calls the tool directly; when a
  call needs the operator, Aura stops it and asks. It never repeats a call that the host already ran or the operator
  declined. The "snapshot or back up when in doubt" caution stays. The pre-question rule is removed, not kept for a
  class of actions.
- **Consequences.**
  - Actions no gate covers run without a confirmation. Under `single_user_hardened`, which is what the appliance ships,
    that means:
    - a `write_file` overwrite;
    - a shell command that matches no destructive pattern.

    Under `dev` and `local_trusted` (opt-in, development) the gateway gates nothing at all.
  - Closing those gaps means adding gates: a pattern or a tool review. This spec does not add any. The plan lists the
    ungated destructive actions it finds, so the operator can decide case by case.
- **Retired texts.** Every relay text goes: `approve.go:221`, `shell_approval.go:137`, `approval_store.go:164`,
  `pack.go:44`, the `task.go:156/178` clauses, and the `ask_user` `resume_context` schema text where it only served
  relays. The `deferred_manifest.json` golden is updated accordingly.

### What is deleted

- `ApprovalStore[T]`, `ShellApprovals`, `PluginApprovals`, `TaskCancelApprovals`, `approvalPrompt`;
- the pending and approved maps of `GatewayApprovals`, while session grants stay;
- the four `*_approval_required` payloads;
- `newChallengeResumeHook` and the shell, plugin, task-cancel and gateway resume hooks with their chain;
- `stampGatewayChallenge`;
- the challenge tests, replaced by tests of the new contract.

No compatibility shim. A pause row of an old challenge type that exists at upgrade resolves as today's decline
("expired, not run").

## Phases

1. **The mechanism, then the gateway.** The interface, park, durable record, accept, decline, expiry and every
   resolve surface, with the gateway source migrated first (it is the broadest). The prompt doctrine and the deletion
   of the gateway's challenge go with it.
2. **The tool paths.** `shell_exec`, `plugin_pack` and `task cancel` move to the tool source, and their stores and
   hooks are deleted.
3. **Optional.** Judge whether `task schedule` approval folds in, with its accept action as a variant. A ledger
   ruling may close it as "kept".

Each phase ships green on the full CI matrix and is checked live on the lab VM before the next.

## What done means

The unit and integration tests cover:
- park instead of execute;
- accept runs once, and a second resolve runs nothing;
- concurrent resolves on two surfaces;
- a crash after the claim becomes "outcome unknown", never a second run;
- a restart between park and accept: the accept runs it once;
- decline gives a tool result and no run;
- expiry gives no run;
- the executed arguments are the reviewed ones (shell cwd, plugin pin);
- deny policy before park;
- grants skip the park;
- headless gives a refusal, never a park;
- siblings are dropped;
- several parks in one turn;
- the strict-profile ledger records one start and one end with the resolving operator;
- every resolve surface;
- Telegram and web cards render each presentation key in Italian and English.

Then live on the VM, as dated files in `docs/superpowers/verification/`:
- a foreign `task cancel` gives one card and at most 4 model calls;
- a destructive `shell_exec` gives one card, accept runs it, and the result reaches the model;
- a gateway-gated call under `single_user_hardened` gives one card, with scope options;
- `plugin_pack` remove gives one card, if a disposable plugin is available;
- for each: the model call count and seconds, against the 2026-11-27 baseline of 7 calls and about 80 s.

## Decisions taken after the builder check (controller's calls; the operator may overturn them)

1. **Where the accepted call runs.** It runs as the **first step of the continuation run**, not inside the resolve
   request.
   - The reason: `shell_exec` can stay in the foreground up to 10 minutes and plugin writes have a 10-minute
     timeout. Running them inside a cockpit, Telegram or AG-UI resolve would block that request and hold the thread
     lock, show no live tool card, and leave a client disconnect mid-call as "outcome unknown".
   - The split:
     - the resolve takes the durable one-shot claim and records the operator's decision in its transaction;
     - the continuation run executes the claimed call with the live event stream;
     - the result is committed as the tool result on the original call id;
     - then the model continues.

   This is the industrial order: OpenAI `Runner.run(agent, state)` executes the approved call as the run's first
   step. Drafts keep their in-request send.
2. **One call, one card.** The sources are consulted in this order:
   1. outbound drafts;
   2. host review (`task schedule` approval and messages);
   3. the tool source;
   4. the gateway source.

   The first that parks owns the call, and the later ones are not consulted for it. So a Destructive `task schedule`
   gets host review's card only, and `send_email` gets the draft only. When the tool source parks a call that an
   `ask` policy also names, the single card is the tool's, and it shows the gateway's scope options too. Grants keep
   today's meaning: a session or always grant answers only the gateway's need, never a tool's own one-shot need.
   What needs approval does not change.
3. **Gateway cards keep argument keys only**, plus the target, as today (`approve.go:237-246`), so that secrets and
   message bodies stay off the channel. "The card shows exactly what will run" applies to tool-sourced cards (shell
   command and cwd, plugin commit and digest, task). For a gateway card, what runs is pinned by the stored
   fingerprint of the arguments, not displayed.
4. **Several parked calls in one turn.**
   - Each card is answered on its own.
   - The continuation starts when the last one is answered, and executes the accepted calls in the **model's
     order**.
   - A decline does not cancel the other accepted calls.
   - `SubmitAnswers` keeps committing a batch of decisions in one transaction, because execution is not in that
     transaction (see 1).
5. **Decline gives no new UI.** The decline result carries the operator's words only when a surface already sent
   some content. No reason field is added in this project.
6. **Ungated destructive actions** are listed by the plan, not gated (see Model doctrine).

## Amendments after the plan's three-reviewer validation (2026-11-27, controller's rulings)

The evidence is `.superpowers/sdd/approval-unify/plan-validation-{codereview,adversarial,crosssource}-2026-11-27.md`.

1. **History repair.** The history loader's repair (`repairManagedToolMessagePairs`) must not give a stand-in result to
   a call whose approval record is open, whether pending, accepted or running. The same holds for the display. On the
   accept path the model sees exactly one tool message per call id.
2. **The hard deny stays first.** `server_production`, or no responder, is denied before any authorization or grant
   is read, as today (WR-01). Under `server_production` the **gateway source** never parks. Tool-sourced approvals keep reaching the operator on
every profile, as they do today ("what needs approval does not change"). An "always" grant never lets a
   headless run through.
3. **When an accepted call runs.** An accepted call runs only when:
   - no card of the same parked assistant turn is still pending;
   - that turn is the last one on the conversation's path, followed only by its own tool results.

   In every other case:
   - a background wake (a steer, a completion) never runs an accepted record;
   - a new user message closes the turn's pending and accepted records as "not run";
   - an accepted record expires on a TTL the plan sets, and is then closed "not run".
4. **Reviews get the call context.** A review receives the call's context (conversation and call id) at park time,
   exactly as the host-review seam does. Session grants, the foreign-cancel check and the shell's working directory
   depend on it.
5. **The record key.** A record is keyed on the pause token plus the assistant turn, never on
   (conversation, tool_call_id), because call ids repeat across rounds.
6. **Hooks.** A `BeforeTool` hook that rewrites or refuses an accepted call makes the call refused, with a result the
   model can read. The call is never swapped.
7. **Decline wording** ends: "Do not retry this call unless the operator asks again."
8. **Phase 1 prompt.** It keeps working with the three tool challenges that still exist in Phase 1: "unless a tool's
   result tells you to relay its approval". The final wording lands with Phase 2. Phase 1's live check measures one
   destructive `shell_exec` under the Phase 1 prompt.
9. **Executed arguments.** When the arguments that ran differ from the model's (shell cwd, plugin pin), the tool
   result says what ran.

## Open for the builder

- Where the durable pending record lives: the paused_states context or a table.
- The exact interface name and signature.
- The answer-suffix treatment.
- How the decide-only gateway half is factored out of `Decide` without duplicating policy and grant logic.
- Whether the four presentation builders move next to their tools or stay in `approvaltext`.

## What this design does not settle

- Message drafts' missing deny-policy check (map R12) is noted, not fixed here.
- Phase 3's fold of `task schedule`.
- Telegram's long-result rendering after an executed accept.
