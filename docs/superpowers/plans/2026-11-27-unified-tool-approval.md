# One approval mechanism: the host parks the call and runs it. Implementation plan (v3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A call that needs the operator is parked by the host before it runs and shown on Aura's own card. The other calls of the same turn run in that turn. On accept, the host runs the parked call once, as the first step of the continuation run. Its result is committed on the original call id. The four model-relayed challenges (gateway, `shell_exec`, `plugin_pack`, foreign `task cancel`) are deleted.

**Architecture:**
- **The park round.** Pass 2 of the pause detection gets one more producer, after drafts and host review: the gateway source in Phase 1, then the tool source in Phase 2. Neither reserves anything. In a park round the agent proceeds in four steps:
  1. It records the whole assistant message.
  2. It yields one `ParkedTurn` event. The runner commits the assistant turn, the pause rows and the owner-scoped `aura.tool_approvals` records in one transaction.
  3. It dispatches the unparked siblings normally.
  4. It emits the pause events.
- **The resolve.** It moves the record `pending → accepted | declined | expired` inside the resume transaction.
- **The continuation.** When a continuation finds every card of that parked turn decided, with the turn still at the tail of the path, it runs the accepted calls through the normal dispatch before the first model call, as one dispatch batch. A context-scoped `tools.ApprovedCall` authorizes them. The history repair never fakes a result for an open record.
- **While a parked turn is open** (one predicate, `Runner.openParkedTurn`, D24), no turn appends after it: wakes, no-message turns, leftover steer drains and the interrupted-round record all wait, and the steers queued behind it are held without expiry until it closes.

**Tech Stack:** Go 1.27, PostgreSQL + sqlc + golang-migrate, pgx, React + Vitest cockpit, telebot.

**Spec:** [docs/superpowers/specs/2026-11-27-unified-tool-approval-design.md](../specs/2026-11-27-unified-tool-approval-design.md):
- the design at `8ae66ae19`;
- the amended "Siblings" paragraph;
- "Amendments after the plan's three-reviewer validation" items 1–9 (`75f4c36f8`).

Evidence: `.superpowers/sdd/approval-unify/{current-state-map,hitl-research,builder-check,plan-validation-*-2026-11-27,plan-validation-v2-2026-11-27}.md`. v2 lines were read at `75f4c36f8`. v3 re-read every line it cites at `cc4ceebbf`, and the files #300 changed (`internal/agui/translator.go`, `translator_pause_test.go`, `internal/runner/message_draft_resume.go`, `message_draft_scheduled.go`, `internal/messagedrafts/{edits,validation}.go`) at `origin/master` `2ae083107`. The executor works on a tree that contains #300. Re-read before editing.

## v1 → v2 changelog

Finding ids are the reviews': `CR-` code review, `AD-` adversarial, `XS-` cross-source, `AM-` spec amendment.

**Redesigns**

- **Siblings (AM-Siblings, XS §4).** v1 dropped the siblings. v2 runs them in the park round (D19). The rule covers every pass-2 source, so drafts and host review join; pass 1 (`ask_user`) keeps dropping them.
- **History repair (CR-R1, AD-C1, XS-G1, AM-1).** `LoadManagedHistory` takes the open records' call ids and gives them no stand-in. The display drops the placeholder for them (D20, Tasks 6 and 7). A test goes through the real loader and asserts one tool message per call id.
- **Hard deny first (CR-R2, AD-H1, AM-2).** The `Decide` order is rewritten (D7). `server_production || !responderPresent` comes before any authorization or grant. `Review` is never `Needed` under production. The headless always-grant and production tests are added.
- **When an accepted call runs (AD-H3, XS-G2, AM-3).** It runs only when every card of its parked turn is decided and that turn is the tail of the path (D4). Otherwise:
  - a wake pushes its steer and runs no turn;
  - a turn without a message while any card of the conversation is pending runs no model and emits nothing;
  - a user message closes the pending and accepted records as `not_run`;
  - accepted records expire after 30 minutes (D21).
- **Review context (AD-H4, AM-4).** `withholdForApproval` hands every review a context carrying `WithToolCallContext` and the origin. `gateway.Review` takes the reservation key (D14).
- **Record key (AD-M3, XS-G3, AM-5).** The record is keyed on `pause_token`, with `UNIQUE (conversation_id, assistant_seq, call_index)`. `ResultCommitted` looks only at turns after `assistant_seq`. The tracker entry is dropped at `End` (D1, D5).
- **Hooks (AD-L2, XS-G4, AM-6).** A `BeforeTool` rewrite or veto of an approved call becomes a refusal the model can read. The record goes to `not_run`, and the call is never swapped (D22).
- **Decline wording (XS-G5, AM-7).** It ends "Do not retry this call unless the operator asks again." (D12).
- **Phase 1 prompt (AD-H2, AM-8).** Task 9 carries the relay exception. Task 15 removes it. Task 11 measures a destructive `shell_exec` under the Phase 1 prompt.
- **Executed arguments (XS §3, AM-9).** `CallReview.RanNote` tells the model what ran (Phase 2, D23).

**Corrections**

- **Commits.**
  - CR-H1 and CR-H2: the grep gates are anchored (Tasks 8, 14, 15).
  - CR-H3: `NewPoolResumeCommitter` keeps its signature, and the store comes through `SetToolApprovals` (D2), so none of the seven callers change.
  - CR-H4, CR-H5 and CR-M13: the named files are added to Task 8. The Telegram test moves out of the 597-line `hitl_test.go`.
  - CR-H6 and CR-H7: each Phase 2 task deletes what it makes dead and fixes every cross-referencing test. `approval_store.go`, `newChallengeResumeHook`, `chainResumeHooks` and `ResumeHook` go in Task 14, where they lose their last caller.
- **Fixed lines and moves.**
  - CR-M2: the field is `openToolCalls`, and the End-branch repair is bypassed too (D5).
  - CR-M3: the shell review order is corrected, with the reason for checking the pattern before `Route` (Task 12).
  - CR-M4: the digest and question helpers move (Tasks 12 and 14).
  - CR-M5: `pack.go:182-185` `Evict` is deleted.
  - CR-M6: `chat_boot.go:518` is listed in Tasks 12–14.
  - CR-M7: two implementers of `CommitParkedOutcome` are added.
  - CR-M8: the label is fixed.
  - CR-M9 and AD-M2: `pending → cancelled` is added, and cancel closes the parked turn first (D4).
- **Tests and gates.**
  - CR-M1: the integration scenarios are split over two files.
  - CR-M10: `DBI` uses `-p 1` and the env `coverage_docker.sh` exports.
  - CR-M11 and AD-L5: the migration number is a placeholder `NNNN`, taken at landing, with a renumber check before push.
  - CR-M12, CR-M14 and CR-M15: the missed test files are listed.
  - CR-M16: Task 15 uses an explicit pathspec, so `internal/webauth` is never swept.
  - CR-M17: `approve.go:127` takes the identity.
  - AD-M6: every task gate runs `make tagged-tier-compile`.
- **The adversarial review's mediums and lows.**
  - AD-M1: after a settle the branch leaf moves to the new head (Task 6).
  - AD-M4: `WithApprovedCalls` is scoped to the prelude dispatch, and a round-2 test pins it.
  - AD-M5: the switch is flipped in Task 7, after the surfaces, and `gateway_approval` is retired in Task 8.
  - AD-M8: tests for the swarm and agent_job roots, the Telegram button accept, an `ask_user` sibling and mutation targets.
  - AD-L3: the strict-ledger test is on the observed path.
  - AD-L4: `CallReviewer` lands with its first implementer, in Task 12.
  - AD-L6: the dedupe move is named (D18).
  - AD-L7: a missing pinned cwd is a clean refusal (Task 12).
  - AD-L8: an `ask` policy on shell, plugin or task still gives more than one card in Phase 1. Kept as the D16 limit. Task 11 measures it, and Task 12 closes it for the shell (`TestAskPolicyOnShellGivesOneCardWithOptions`).
  - AD-L9: the E2E score is stated in Tasks 11 and 16.
- **Found while checking v2 against the code** (not in the reviews).
  - **A resolve while the park round is still running.** The pause rows commit before the siblings run, so the approvals list can show the card while the run still holds the thread. The resolve now takes the thread lock, the way the draft resolve does (`message_draft_resume.go:81-87`), and returns `ErrThreadBusy`. The surfaces map that to "busy, try again" and keep the card (D2, Tasks 5 and 7).
  - **Mixed parked turns.** A draft or host-review card has no record. D4.4 reads every pending pause of the conversation (`remainingPending`), not only the records. A test accepts a gated call while a draft card of the same turn is pending.
  - **`server_production` and the tool source.** The two spec sentences pull apart. D14 states the reading the plan takes, and the report flags it.
  - **The production deadcode gate.** Its allowlist fails closed both ways. Tasks 1 and 2 add what is not yet wired, and the wiring task deletes the entry.
- **Small.**
  - XS: a parked call whose tool is gone ends with a readable error (Task 6).
  - XS: the fingerprint is a consistency check, not the forgery barrier (D8).
- **Drifted lines corrected.** CR-L4 (`0134:41-51`), CR-L24 (`CanonicalArgs(string(raw))`), CR-L28 (`runner.go:248`), CR-L30 (`bot_dispatch_hitl.go:36-40`, `handleTextReply` returns `resumed`), CR-L32 (`ResolvedApproval` stays), CR-L33 (`serve_adapters.go:411-480`), CR-L39 (`pack.go:65-72`, `serve_plugins.go:29-47`), CR-L41 (stale `ResumeHook` comments, Task 14).

**Rejected, with reason**

- **AD-L1** (mark running only inside `executeBatch`). Kept at the Start event. The error is safe: a consumer stop between Start and execution turns into "outcome unknown", never a second run. Moving the CAS into `executeBatch` would put database I/O inside the concurrent batch path.
- **AD-M7** (Phase 1 rollback). Not covered. A rollback ships as a new release, and the updater never downgrades. The down migration is for development only. The verification file says so. *(Reversed in v3: a database guard plus a release note, see the v2 → v3 changelog.)*
- **AD-M9** (a draft sibling is sent before an accepted call runs). Accepted as a limit and written down (D19). Deferring a draft's send to the continuation changes the draft resolve, which is a non-goal.
- **XS §2** (revalidate stored arguments against the current schema). Not done. The tool's own parse fails readably. A test with the tool gone pins that.
- **AD-M8 "two continuations"**. The extra model round of a second continuation is older than this project and out of scope. "Runs nothing twice" is pinned.

## v2 → v3 changelog

Source: `.superpowers/sdd/approval-unify/plan-validation-v2-2026-11-27.md` (verdict YELLOW), the controller's rulings, and #300 on `origin/master` (`2ae083107`). Each claim below was re-read at `cc4ceebbf`, or at `2ae083107` for #300's files. No report claim was found wrong; three were incomplete, and the extra sites are folded in (marked "found").

**HIGH**
- **N1** (verified). One predicate, `openParkedTurn`, gates D4.4, D4.5, `deliverLeftoverSteer` and `recordInterruptedRound` (new D24; D4 rewritten; Task 6 files, interfaces and tests). "A turn with a message" is `visibleUserMsg != nil` (D4). A wake with only an `ask_user`, draft or host-review card and no record runs as today. Held steers get `expires_at = NULL` through `Hold` and their own TTL back through `Release` when the parked turn closes. The row is changed, not only the sweep, because `Drain` also skips a row past `expires_at`. Tests: the report's four (`TestPartialAGUIResumeRunsNothingAndKeepsTheQueuedSteer` in Task 7; `TestRuntimeLeftoverSteerDuringAPendingCardIsKept`, `TestWakeWithOnlyADraftCardPendingDeliversItsSteer` and `TestDeferredSteerOutlivingItsTTLDoesNotBreakTheParkedTurn` in Task 6), plus `TestRecordInterruptedRoundSkipsAnOpenParkedTurn` and `TestSteerQueuedBeforeTheContinuationLandsAfterTheApprovedResults`.
  - Found: a third drain site, `PreparePendingSteer` (`runner_steer_pending.go:13-33`, from `agui/server_coordinator_wake.go:50`), now gated too (D4.5, `TestPreparePendingSteerDefersBehindAnOpenParkedTurn`).
  - Found: `steer/worker.go:155` reads worker rows' expiry, so `Hold` skips worker-targeted rows (`TestHoldLeavesWorkerRowsAlone`).
  - The predicate counts `running` as open, so a prelude cancelled after its Starts also appends no interrupted turn.
  - `MintApprovalPause`, which the report names as a second out-of-band appender and no ruling covers: it returns `ErrParkedTurnOpen` while the predicate holds. Its reminder sweep already logs an ensure error and retries next cadence (`internal/cron/deliver_approval.go:136-149`, read), so no caller changes and the lapse is unchanged (D24, Task 6, `TestMintApprovalPauseWaitsForAnOpenParkedTurn`).
  - Found: `PushDelegationResultIdempotent` lands a row with no held site, so the steer sweep is the catch-all. Its identity-scoped per-row transaction holds a due row instead of expiring it while the conversation has an open record (D24, `TestSweepHoldsADueRowBehindAnOpenRecord`). NULL `expires_at` is "never expires" in `DrainSteerRows` and both nudge queries (read), so a held result is still nudged.
  - Limits (D24): a close that no turn follows only restarts the steer's clock; a row lapsing between two sweep ticks can still be skipped by `Drain` for one interval; held rows count against the queue cap.
- **N2** (dispatch verified at `llm_agent_dispatch.go:46,160-163`, `llm_agent_message_draft.go:34-40`). A park round drops a `text_response` from the step, from `ParkedTurn.Calls` and from the history message, as pass 2 does today (D19 step 1). Task 3: `TestDraftParkWithATextResponseSiblingDropsTheTerminal` (`[send_email, text_response]`: the card, no final answer, `send_email` alone persisted), the host-review twin, `TestDraftAndGatedWithATextResponseParkBothAndDropTheTerminal` and `TestGatedCallWithATextResponseKeepsTheRejection`. D14's terminal condition is read on the batch the gateway source sees.
- **N3** (cwd and ordering verified: `shell_exec_sandbox.go:197`, `shell_exec_session.go:29`, `llm_agent_parallel.go:35-39,71-82`). Chosen: refuse at execution when the pinned cwd differs from the tracked one. Ordered evaluation would split D19's single `CommitPause` transaction (D23). The binding is `tracked-cwd:<dir>` on `CallReview.Binding`, which moves from Task 14 to Task 12; an explicit model cwd is not rebound. Task 12: `TestShellReviewAfterACdSiblingDoesNotRunInTheOldCwd` (`[cd proj, rm -rf build]`), the converse `TestCdAfterAParkedShellCallRefusesTheAccept`, `TestExplicitCwdIsNotRebound`. Found: the tracked cwd is an in-memory map (`Evict` runs only on Stop and delete, after D4.6 closes the turn; a restart empties it), so an empty tracked cwd at execution is a difference and refuses cleanly (`TestEmptyTrackedCwdAtExecutionRefusesABoundCall`). The report's alternative (skip the park after a same-tool mutating sibling) is recorded in D23 and was not taken. The `[rm (parked), ls build]` order is a D19 limit.
- **N4** (allowlist `:19`, `:20`, `gateway.go:187`, `approval_store.go:70`, `shell_approval.go:83` verified). The allowlist is in the commit paths of Tasks 7, 8, 12 and 14, with the expected changes in Global Constraints and in each task's steps: Task 8 removes `RecordResolvedApproval`; Task 12 removes `ShellApprovals.PendingChallenge` and adds `ApprovalStore.PendingChallenge`; Task 14 removes it; Task 7 removes `toolapprovals.Store.OpenCalls` (Task 4 adds the unreached `Store` methods). Entries are copied as the gate prints them.

**MEDIUM**
- **M1.** The 30-minute window counts from the last decision of the parked turn, records' `decided_at` and the other cards' `resumed_at`, and runs only once no card of the turn is pending (D21; `Store.Decided`, `parkedTurnState.LastDecidedAt`). Tests: `TestSecondCardAfterTheWindowStillRunsTheFirst`, `TestLastDecidedAtReadsRecordsAndPauses`.
- **M3** (`runner_persist_pause.go:29`, `runner_interrupted.go:56-76` verified). `ParkedTurn` sets `tr.paused` and `tr.parked`; `flushPause` skips a parked tracker (D19, Task 4). Tests: `TestCancelDuringASiblingAppendsNoInterruptedTurn`, `TestConsumerStopDuringASiblingAppendsNoInterruptedTurn`.
- **M4** (`gateway/classify.go:180-200` verified). Every strict reservation reads the authorization for the operator, whatever the tier, and never turns a Deny into an Allow (D7 step 8). Task 2: `TestApprovedNormalTierCallReservesWithTheOperator`, `TestAuthorizationNeverAllowsADenyPolicy`. Task 12: `TestToolSourcedAcceptRecordsTheOperator` (db_integration, ledger).
- **N5** (`scripts/go_packages.sh`, `Makefile:17` verified). Task 1 Step 4 runs `git add -N internal/toolapprovals` and checks `go_packages.sh` lists it before the gates. Global Constraints state the rule.
- **M6** (`runner_errorpaths_test.go:283-306`, `runner.go:109` verified). Both are in Task 14's Modify list and paths.
- **D2 lock scope.** Every `SubmitAnswer`/`SubmitAnswers` resolve of a pass-2 pause type (`tool_approval`, `scheduled_task_approval`) takes the thread lock (D2). No pause row says "park round", so the pause type is the discriminator; outside a park round the lock is free, unless another turn is live, and then the resolve answers 409 where the continuation would have anyway. This is a superset of the ruling's "inside a park round". Found: `ExpireApprovalPause` already locks (`runner_expire_approval.go:38-44`); `ExpirePendingApprovals` and `closeStaleAccepted` take no lock and start no continuation, and an answer they commit stays inside the contiguous tool block `validToolResultGroup` requires (`store_helpers.go:342-362`, read). Tests: `TestHostReviewResolveWhileTheParkRoundRunsIsBusy`, `TestAskUserResolveTakesNoLock`.
- **Silent committer path.** The `toolApproval` claim is attached by pause type, never by wiring; `CommitResume`/`CommitResumeBatch` without the store return `errToolApprovalsNotWired`; `runner.New` refuses `Deps.ToolApprovals` without the committer's store (D2, Tasks 4 and 5). Tests: `TestToolApprovalClaimWithoutTheStoreFails`, `TestResumeClaimCarriesTheDecisionWhateverTheWiring`, `TestExpirySweepWithoutTheStoreFailsLoudly`, `TestNewRefusesToolApprovalsWithoutTheCommitterStore`. The D1 trigger is a second guard.

**PARTIAL v1 findings**
- **AD-H3 scenario 4 / M2** (`llm_agent_dispatch.go:94-109`, `agui/server.go:411` verified). The accepted calls of a turn run as one dispatch batch, every Start before any execution; a failed CAS mid-batch leaves the earlier records `running`, then `unknown` (D4.4, Architecture). Tests: `TestApprovedCallsRunAsOneBatch`, `TestRunCancelDuringThePreludeLeavesNoAcceptedRecord`, `TestFailedCASMidBatchRunsNothing` (Task 6), `TestRunCancelDuringAnApprovedCallLeavesNothingAccepted` through `POST /agent/runs/{runID}/cancel` (Task 7).
- **XS-G3.** The repair skip-set and the cockpit's display and declined sets are keyed by the call: `(assistant_seq, tool_call_id)`, read from the record that the pause token names (D20). No message or snapshot row carries a pause token, so the seq is the join. The managed repair moves before `toMessages`, whose input `[]Turn` carries `Seq` (`context.go:508`, `store.go:182-183`, read). The web fold has no seq; its existing single-carrier guard (`ExternalStoreChat_folds.ts:137-145`) is the closest safe variant, and a test pins it. Tests: `TestRepairKeysTheOpenCallByItsTurn`, `TestOpenCallsKeysByAssistantSeq`, `TestRepeatedCallIDInAnOlderTurnIsNotMarked`. Renames: `OpenCallIDs` → `OpenCalls`, `ListDeclinedToolApprovalCallIDsForIdentity` → `…CallsForIdentity`.
- **AD-M7** (no downgrade guard found, `runner_resume.go:219-239` verified). v2's rejection is reversed. A reverted binary cannot carry new Go, so the guard lives in the database: Task 1's migration adds a `paused_states` trigger that refuses to resume a `tool_approval` pause whose record is `pending`. A revert that forgets to expire them first fails loudly instead of answering a parked call as if it ran (D1). Task 11 Step 5 writes the release note (a revert ships a data upgrade that expires open records first) and measures the guard. `resume_context` is `jsonb` (`0003_paused_states.up.sql:15`), so the trigger's `->>` holds. The guard is in Task 1, where the migration lands; the closing task carries the note and the measure. Tests: `TestTriggerRefusesResumingAToolApprovalPauseWithAPendingRecord`, `TestTriggerAllowsTheResolveThatDecidesTheRecordFirst`, `TestTriggerIgnoresOtherPauseTypes`.

**LOW**
- **L1.** D14's and Task 12's AM-2 hedges are deleted; `6d5f72b4c` settled it, and D14 quotes the amended sentence.
- **L2.** Spec "What done means" no longer says "siblings are dropped": it now states the spec's sibling rule.
- **L3.** Task 7 Step 3 named D18; it means D2 (busy mapping) and D20. Fixed.
- **L4.** `Review.Denied` (D7, added in Task 12): the tool source does not park a call the gateway would refuse, so an `ask` policy under `server_production` gives the `no_approver` refusal, not a card (`TestToolSourceDoesNotParkWhatTheGatewayRefuses`).
- **Cancel during the park round.** It answers 409 and keeps the card; documented in D2 (`TestCancelWhileTheParkRoundRunsIsBusy`).
- **Dedup trip among siblings.** `parkRound` yields the suppressed siblings' synthetic results as End events and skips the trip's `maybeRecover`/`finalize`, so no final answer precedes the pauses. D19 lists every turn-ending exit of `dispatch` and how a park round handles it (`TestDedupTripInAParkRoundYieldsItsResults`).
- **Sibling ledger.** `TestStrictLedgerOfASiblingHasOneStartOneEnd` (Task 4).
- Found: Task 8's new `TestExecToolNeverReturnsAnApprovalRequest` must not spell `gateway_approval_required`, or the anchored grep prints it (report §1, CR-H1 caveat). Written into Task 8 Step 4.

**#300 on `origin/master`** (`8e6a92199`, `67c84e5b9`)
- The translator now reads every contiguous pause of a round and ends the run at the first non-pause event after them. D19 step 4 keeps the pauses last, now as contiguous with nothing after them. `TestPausesAreLastAndContiguous` (Task 3), `TestTranslatorEndsAParkRoundWithEveryCard` (Task 7, a new test file).
- v2's "a second card never appears in the live stream" (report, "An AG-UI stream that stops at the first pause") no longer holds: #300 already delivers every card of a round in one `RUN_FINISHED`. `ParkedTurn` still commits every pause before any is yielded, for the consumers that stop at the first pause. Task 3's test is renamed `TestConsumerStopAtTheFirstPauseStillRanSiblings`.
- `translator.go` is 596 lines after #300. It joins the near-cap list, and no task edits it.
- `message_draft_resume.go`: the lock pattern D2 copies is unchanged at `:83-89`. A draft's answer now carries the operator's edits (`withOperatorEdits`), which D19's boundary keeps unchanged.
- `messagedrafts/{edits,validation}.go` change no assumption of this plan.

## Global Constraints

- Stay on `master`; one atomic commit per task. A commit step is `git add -- <paths> && git commit -m "<msg>" -- <paths>`, because a pathspec commit alone skips new files. Never stage a path outside the task's list: `internal/webauth/*` is a peer's. Push only in a phase-close task, with `LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks push origin master`. Green means every CI job on the full SHA.
- What needs approval does not change:
  - the tiers;
  - the destructive patterns;
  - the foreign-cancel rule;
  - grants and policies;
  - gateway approvals only under `single_user_hardened` (never under `server_production`), tool approvals on every profile.
- Anti-goals, each pinned by a test:
  - no approved call runs twice;
  - no approval runs a different call or different arguments than its record;
  - no headless or delegated run parks;
  - the model writes no approval question;
  - the model never reads two tool messages for one call id.
- Unchanged: drafts' card, edit and resolve routes; `task schedule` approval; free `ask_user`.
- The migration number is taken at landing: `NNNN` = `ls internal/db/migrations/ | tail -1` + 1 (today `0159_data_upgrades`, so `0160`). The phase-close task re-checks it against `origin/master` before pushing.
- No file over 600 lines, tests included (`scripts/check-file-size.sh`). Near the cap:
  - `internal/agui/server.go` 596: renames only, plus at most one interface line;
  - `cmd/aura/main.go` 586;
  - `cmd/aura/chat_boot.go` 579;
  - `internal/agent/llm_agent.go` 558;
  - `internal/channels/telegram/hitl_test.go` 597: it may only shrink;
  - `internal/agui/translator.go` 596 on `origin/master` after #300: no task edits it. A test that needs the translator goes in a new `_test.go` file.
- Refactor on touch. Dead code goes in the task that makes it dead.
- Model-facing texts are English. The cockpit and Telegram keep their Italian/English parity through the `approvaltext` keys.
- `GO <cmd>` means:
  ```bash
  wsl -e bash -lc 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && <cmd>'
  ```
  Every task's last gate runs `go vet ./...`, `go build ./...`, `make tagged-tier-compile`, `make deadcode`, `golangci-lint run ./<touched>/...` and `go test -race ./<touched>/`.
- `DBI <pkgs> -run <re>` is the db_integration tier against the **disposable** stack only:
  1. source `scripts/lib/disposable_stack.sh`;
  2. bring the stack up and export its env the way `scripts/coverage_docker.sh` does, `CI=true` and `AURA_RUN_DIR` included;
  3. `go test -tags=db_integration -race -p 1 <pkgs> -run <re>`;
  4. tear the stack down.

  Never the live `aura` database or the VM.
- Web gates run on Windows, from `D:/Aura/web`: `npx vitest run <files>`, `npm run lint`, `npm run typecheck`.
- Tests: realistic fixtures, `goleak` where the package's `main_test.go` has it, no skip-as-green (`internal/testenv`).
- Coverage stays at 85% on the full matrix. `internal/toolapprovals` joins `scripts/coverage_package_policy.json` as `{"mode": "target"}`.
- **The production deadcode gate.** `make deadcode` also runs `scripts/deadcode_production_gate.sh`, which has no test roots. Its `scripts/deadcode_production_allowlist.txt` fails closed both ways.
  - A task whose new symbols no main package reaches yet adds exactly what the gate reports, with the reason `wired in Task <N> (unified approval)`.
  - Task N deletes the entry.
  - A task that deletes the last production caller of a symbol adds it, and the task that deletes the symbol removes it. A task that deletes an allowlisted symbol removes its entry.
  - Expected additions: Task 1 adds the package `internal/toolapprovals`, and Task 4 removes it. Task 4 then adds the `toolapprovals.Store` methods no production code calls yet, which deadexport reports one by one: `OpenCalls` (removed in Task 7, where the cockpit adapter calls it) and whichever of `Open`, `Decided`, `StaleAccepted`, `ResultCommitted` and `Transition` are not yet called (removed in Tasks 5–6). Task 2 adds `gateway.Gateway.Review` (removed in Task 3), `gateway.ScopeForAnswer` (removed in Task 5) and `tools.WithApprovedCalls` (removed in Task 6). Task 12 adds `tools.ApprovalStore.PendingChallenge`, whose only non-test caller is `shell_approval.go:83`, and Task 14 removes it.
  - Expected removals of today's entries: Task 8 removes `gateway.Gateway.RecordResolvedApproval` (`scripts/deadcode_production_allowlist.txt:20`, the method at `gateway.go:187` that Task 8 deletes). Task 12 removes `tools.ShellApprovals.PendingChallenge` (`:19`, deleted with `shell_approval.go`).
  - An entry is spelled exactly as the gate prints it. The generic receiver `ApprovalStore[T]` is the one at risk: copy the gate's line, do not type it.
  - The gate's output is the truth, not this list. Every task that adds or removes an entry has `scripts/deadcode_production_allowlist.txt` in its commit paths: Tasks 1–8, 12 and 14. Task 13 was checked: no allowlisted plugin symbol exists today; if its gate reports one, it adds the path too.
- **A new package is untracked until its first commit.** `scripts/go_packages.sh` keeps only directories with tracked Go files (`git ls-files '*.go'`), and `GO_PACKAGES` comes from it (`Makefile:17`). So a task that creates a package runs `git add -N <dir>` before its gates; otherwise lint, `test-race`, vuln and both deadcode gates skip it, and the production gate reports its allowlist entry as stale. Only Task 1 creates a package.

## Review Focus

1. **A model `ask_user` with `resume_context.type = "tool_approval"`.** No host card appears, and an accept runs nothing. Tests in Task 4 (strip) and Task 5 (refuse).
2. **Accept card A, then cancel card B of the same turn.** A never runs and is answered "cancelled before it ran". B's record is `cancelled`. Test in Task 5.
3. **A new user message while one card is accepted and another is pending**, sent with `WithBranchLeaf` on the head. Both are closed `not_run` before the user turn, on the user's branch, and nothing runs. Test in Task 6.
4. **A typed Telegram reply while a tool-approval card is first.** `handleTextReply` returns `(false, nil)` and submits nothing; the card is shown again. Test in Task 7.
5. **A background completion while a card is pending or accepted.** The wake pushes its steer, holds it without expiry, runs no model round and executes nothing. No leftover drain consumes it while the parked turn is open, the steer sweep never expires it, and the ready continuation delivers it after the approved results. Tests in Task 6.
6. **A partial resume.** A raw client answers one of two cards through `POST /agent/run`. The run that follows emits nothing, runs no model, and keeps the queued steer. Test in Task 7.
7. **A cockpit stop during the approved calls** (`POST /agent/runs/{runID}/cancel`, a run-ctx cancel). No record stays `accepted`, and nothing runs twice. Tests in Tasks 6 and 7.

---

## Decisions taken while writing this plan

**D1. The record is a dedicated table, `aura.tool_approvals`.**

Why not `paused_states.resume_context`:
- the context is streamed (`event.go:149`);
- the model can write it (`ask_user.go:97,110`);
- the record needs CAS-guarded states;
- `CleanupResumedOlderThan` (`paused_states.sql:99-102`) deletes resumed rows.

The shape copies `0134_message_drafts` (RLS and grants at `:41-51`).

Key and states:
- **Key (AM-5).** `pause_token` is UNIQUE, with `UNIQUE (conversation_id, assistant_seq, call_index)`. `assistant_seq` is the seq the parked assistant turn gets inside `CommitPause`.
- **States.** `pending → accepted | declined | expired | not_run | cancelled`; `accepted → running | not_run | cancelled`; `running → done | unknown | not_run` (`not_run` only for a hook refusal, D22).

The pause's context carries display data only:
```json
{"type": "tool_approval", "tool": "<name>", "approval_presentation": {}, "allowed_decisions": []}
```

**The revert guard (AD-M7).** The migration also installs a trigger on `aura.paused_states`: setting `resumed_at` on a row whose `resume_context->>'type' = 'tool_approval'` raises while that pause's record is still `pending`. The trigger function is `SECURITY DEFINER` with `SET search_path = pg_catalog, aura`, as `aura.conversation_owner` is (`0103_steer_queue.up.sql:113-123`), so RLS cannot hide the record from it.
- **Why a trigger.** A reverted binary cannot contain new Go, but a revert never runs a down migration, so the trigger stays. Without it, a revert's generic `answerTurn` (`runner_resume.go:219-239`) would answer a parked call with the operator's "yes" as if it had run. With it, that resolve fails loudly and the record stays `pending`.
- **What it costs the new code.** Every resolve runs `DecideTx` before `MarkResumedFencedTx` in its transaction (D2). The same trigger also catches a `tool_approval` claim that reached a committer without the store (D2).

**D2. The resolve claim.** `DecideTx` and then `MarkResumedFencedTx` run in one resume transaction, in that order (the D1 trigger needs the record decided first). They ride `ResumeClaim.toolApproval`, the way `scheduledTask` does (`interfaces.go:117-127`, `resume_committer.go:102-112`).
- An accept appends no answer turn.
- A decline or an expiry appends its D12 text.
- **The claim is attached by pause type, never by wiring.** `resumeClaim` sets `toolApproval` whenever the pause's context has `type = tool_approval`, whether or not the runner holds the store. `CommitResume` and `CommitResumeBatch` on a committer without the store return `errToolApprovalsNotWired` for such a claim, and nothing commits. `runner.New` refuses `Deps.ToolApprovals` when the committer has no store. There is no path where a `tool_approval` pause resumes and its record stays `pending`.
- **The thread lock.** Every resolve through `SubmitAnswer` or `SubmitAnswers` of a pause that pass 2 produces takes it with `TryLockThread`, unless `threadLockHeld(ctx)`. That is a `tool_approval` or a host-review (`scheduled_task_approval`) context; drafts already lock in `ResolveMessageDraft` (`message_draft_resume.go:83-89`), and pass 1 (`ask_user`) never makes a park round. A held lock returns `ErrThreadBusy`.
  - **The discriminator is the pause type, not "inside a park round".** No pause row records whether its turn was a park round. Outside one, the lock is free unless another turn is live, and then the continuation would have answered 409 anyway.
  - The lock is needed because D19 commits the pause rows before the siblings run. Without it, an accept or a host-review answer made from the approvals list during the park round would start a continuation that gets a 409, and the record would sit `accepted` until the TTL.
  - `SubmitAnswers` takes the lock once, for the batch's conversation.
  - `ActionCancel` through `SubmitAnswer` takes it too: a cancel during the park round answers 409 and the surface keeps the card. That is accepted and documented in the approvals API.
  - **The sweeps.** `ExpireApprovalPause` already takes `TryLockThread` and returns `ErrThreadBusy` to its cron caller (`runner_expire_approval.go:38-44`); unchanged. `ExpirePendingApprovals` and `closeStaleAccepted` take no lock and start no continuation. An expiry answer committed while siblings still run is one more tool turn in the block that follows the assistant turn, and `validToolResultGroup` accepts that block in any order as long as it is contiguous (`store_helpers.go:342-362`). The CASes keep them one-shot.
  - Nothing runs in the resolve (spec decision 1). The CASes stay the one-run boundary; the lock only orders the resolve after the park round.
  - The REPL answers after its stream ends (`chat_repl.go:170-185`), and Telegram after the pause events, so neither one holds the lock.
- The committer gets the store through `(*PoolResumeCommitter).SetToolApprovals(*toolapprovals.Store)`, not a constructor parameter. A committer without the store refuses records (`errToolApprovalsNotWired`). Callers that never park are untouched (CR-H3).

**D3. The accepted call runs in the continuation, through `dispatch` (`llm_agent_dispatch.go:14`), and the Runner observes it.**
- The gateway's `reserve` writes the one `start` under strict; the Runner writes the `end`.
- The parent operation is the continuation's ingress. The execution step is model round 1.
- The request id is the continuation's.
- These deviate from spec step 3 ("unobserved", "rooted on the resolve"), which assumed in-resolve execution. Decision 1 superseded that.

**D4. When records move.** Everything runs under the thread lock, at turn start, in `prepareParkedTurn` (Task 6). The parked turn is the open records' `assistant_seq` on the loaded path. "A turn with a message" means `visibleUserMsg != nil`; a runtime-source leftover steer sets only `modelUserMsg` (`runner_steer.go:190-208`) and is not a message. The order:
1. **`running` → recovery.** If a tool turn for the call exists after `assistant_seq`, the record becomes `done`. Otherwise `unknown`, with the unknown text.
2. **Accepted past the window** (D21): measured from the parked turn's last decision, and only once the turn has no pending card → `not_run`, with the not-run text.
3. **A turn with a message** closes its parked turn's `pending` and `accepted` records as `not_run` (pending through a resume claim, accepted through `CommitParkedOutcome`). This happens before `appendUserTurn` (`runner.go:248`). When the request's branch leaf was the pre-settle head, it moves to the new head (AD-M1).
4. **A turn without a message, while `openParkedTurn` holds (D24):**
   - **not ready** (a record still `pending`, or a pending pause whose call is in the parked turn: a draft or host-review card of the same turn counts) → no model runs and no event is emitted. The turn ends clean, and the queued steers stay queued and held (D24). The cockpit never starts such a turn (`useThreadApprovals.ts:409-421` resumes only when no token is pending), but the server allows it: the AG-UI resume starts a turn after `SubmitAnswers` and ignores the remaining count (`agui/server_run.go:127-137`; `server_run_detach.go:88-99`). A raw client answering one of two cards lands here;
   - **ready**, and the parked turn is the path's tail followed only by its own tool turns (`conversations.(*Store).PathAfter`): its `accepted` records run (D3) as **one dispatch batch**, in `(assistant_seq, call_index)` order. `dispatch` yields every Start (the CAS `accepted → running`) before `executeBatch` runs anything (`llm_agent_dispatch.go:94-109`), so a run-ctx cancel such as the cockpit stop (`POST /agent/runs/{runID}/cancel`, `agui/server.go:411`) leaves no record `accepted`. A CAS that fails mid-batch stops dispatch before `executeBatch`; the earlier records stay `running` and the next turn's step 1 makes them `unknown`. None runs twice;
   - a branch run whose path does not reach the parked turn changes nothing.
   - While `openParkedTurn` does not hold, a turn without a message behaves exactly as today, whatever other card is pending. An `ask_user` card, a draft card, or a host-review card with no record of this project does not block the model.
5. **A wake** (`WakeWithSteer`, which pushes at `runner_steer_wake.go:55` and runs `turnLocked` at `:59`) recovers, then pushes its steer. While `openParkedTurn` holds, it holds the steer and returns without a turn. Otherwise it runs as today. The AG-UI coordinator's `PreparePendingSteer` (`runner_steer_pending.go:13-33`, called from `agui/server_coordinator_wake.go:50`), the third drain site, checks the predicate before `Drain` and returns `(nil, false, nil)` while it holds.
6. **Cancel and Stop** (`runner_resume.go:297-325,368-372`) close the parked turn as `cancelled` before `injectCancelledAnswers`. That covers both pending and accepted records (CR-M9).

**D5. Calls already in a persisted assistant turn.** `turnTracker.persistedCalls map[string]string` maps a call id to its record id, or `""` for a sibling. It holds the siblings of a park round and the prelude's approved calls.
- **On `ToolInvocationStart`.** The Runner does not touch `openToolCalls` or `pendingToolCalls` (`runner_persist.go:178-232`). For a record id, it makes the CAS `accepted → running`. A failed CAS returns an error, so dispatch stops before `executeBatch` (`llm_agent_dispatch.go:93-97`).
- **On `ToolInvocationEnd`.** The defensive repair at `runner_persist.go:192-197` is skipped. The tool turn is appended through `Conv.AppendTurn`, which spills. Then `running → done` (or `not_run`, D22), and the entry is deleted.

**D6. Interface (Phase 2, AD-L4).** `tools.CallReviewer` lands in Task 12 with its first implementer:
```go
type CallReviewer interface {
    ReviewCall(ctx context.Context, raw json.RawMessage) (CallReview, error) // ctx carries WithToolCallContext (D14)
}
type CallReview struct {
    Needed       bool
    Question     string
    Presentation approvaltext.Presentation
    ExecuteArgs  json.RawMessage
    Binding      string // re-checked by the tool at execution; "" = none
    RanNote      string // shown to the model when ExecuteArgs differ (AM-9)
}
```
`tools.HostReviewer` (`task_message.go:29-31`) stays until Phase 3.

**D7. The decide-only half.** `needFor(ctx, spec, raw, conversationID) need` is shared by `Decide` and `Review` (`internal/gateway/review.go`). It reads the policy and the grants, and writes nothing.

`Decide`, in this order:
1. the reviewed message;
2. non-strict → Allow;
3. classify, subject, policy;
4. a deny policy → Deny (fact `policy`);
5. needs-person;
6. a read-only call → decision fact, Allow;
7. a gated call (`gated(tier) || ask`):
   1. **`profile == ServerProduction || !responderPresent(ctx)` → Deny `no_approver` (WR-01, first, AM-2)**;
   2. an authorization (D8) → Allow;
   3. a session grant, or an always grant (not under ask) → Allow, with operator = identity (CR-M17: replaces `approve.go:127` `"local"`);
   4. `!personPresent(ctx)` → Deny `no_approver` (a sync swarm worker: today it was challenged);
   5. otherwise the challenge route until Task 8, then Deny `approval_not_parked`;
8. then `beginOperation` + `reserve`. **Every** strict reservation reads `tools.ApprovedCallFor(ctx)`: when it authorizes the call (D8), the row carries its `operator_id` and `approval_scope` whatever the tier (M4). A tool-sourced call (`shell_exec`, `plugin_pack` install and remove, `task cancel`) is Normal tier (`gateway/classify.go:180-200`), never reaches 7.2, and would otherwise be reserved without the operator. The authorization never turns a Deny into an Allow outside 7.2; it only names the operator.

`Review` is `Needed` only when 7.1–7.4 all fail and no refusal applies. `Review.Denied` is true when Decide would refuse the call: step 4, or 7.1 on a gated call (read by the tool source, D14, Task 12). The policy is read twice under strict; the cost is accepted and measured.

**D8. Authorization.**
- **The carrier.** `tools.WithApprovedCalls(ctx, []ApprovedCall)`, read by `tools.ApprovedCallFor(ctx)` on (`SessionIDFromContext`, `ToolCallIDFromContext`).
- **Scope (AD-M4).** It is attached only to the prelude's dispatch context, never to `ic`.
- **What `Decide` checks.** The name, the conversation, the call id and `CallFingerprint(args)`. Then it reserves once with `operator_id` and `approval_scope`. That replaces `serve_adapters.go:471`.
- **What is the real guard (XS §2).** The fingerprint is a consistency check against a hook or bug changing the arguments. The forgery barrier is the server-held RLS row.

**D9. Presentation builders.**
- The gateway's builder lives in `review.go`; the shell's in its review (Phase 2).
- Plugin and cancel call `approvaltext.LineCard(key, question, digest)` (the exported `deriveCard`, `presentation_plugin.go:77-99`) once, at park time.
- `FromContext` trusts a `tool_approval` context's stored presentation when `Valid(p)`.
- The question-parsing `derive*` functions leave with their last producer; `deriveScheduled` stays.

**D10. Forgery guard.** A `tool_approval` context is written only from `AwaitingInput.Approval` (`json:"-"`). A model context of that type loses its `type` before `Enrich`. A resolve of such a context without a record gets `ErrResumeDecisionNotAllowed`.

**D11. Answer suffix.** The visible-reply suffix (`runner_resume.go:228-232`) stays on a tool-approval decline. It never reaches an executed result or a settle text.

**D12. Texts** (`internal/runner/tool_approval_text.go`), with `<what>` = `approvaltext.Brief(p)`:
- decline: `The operator declined <tool> <what>. Do not retry this call unless the operator asks again.`, then `\nThe operator said: <content>` when the surface sent content;
- expired: `The approval for <tool> <what> expired before the operator decided; it was not run. Do not retry it unless the operator asks.`;
- unknown: `The approved call <tool> <what> was interrupted; its outcome is unknown. Check its effect before retrying it.`;
- not run, closed by a message or the TTL: `<tool> <what> was not run: the conversation moved on before it could run. Call it again only if it is still needed.`;
- cancelled: `The request was cancelled before <tool> <what> ran; it was not run.`;
- hook refusal: `A hook stopped the approved call <tool> <what>; it was not run.`;
- legacy: `This approval came from an earlier version of Aura and was not carried over: the call was not run. Call the tool again if it is still needed.`

**D13. Scope.**
- `gateway.ScopeForAnswer(pending.Options, answer)` reads the stored option values.
- `gateway.RecordGrant` runs right after the resolve commit and before any execution. It never errors, and it degrades always → session (`grants.go:82-103`).
- A grant answers only the gateway's need.

**D14. Who parks, and with what context (AM-4).**
- **The switch.** `LlmAgentConfig.ParkApprovals` is set only by `Runner.buildAgent`, from Task 7 on. Tasks 3–6 set it in tests.
- **The conditions.** `HasResponder`, `!IsDelegatedDispatch`, an identity, the tool not `isDeferredUnloaded`, and no `text_response` in the batch the gateway source sees. `[G, text_response]` alone keeps today's terminal-exclusivity rejection. Once a draft or host-review call of the same batch parks, the terminal is already dropped (D19), so `[send_email, G, text_response]` parks both and drops the terminal.
- **The profile.** It is read by each source, not by the park step.
  - **The gateway source** never parks under `server_production`: `Review` is not `Needed` there (D7.7.1, AM-2).
  - **The tool source** (Phase 2) parks on every profile, `server_production` included. That is the spec as amended at `6d5f72b4c` ("Under `server_production` the gateway source never parks. Tool-sourced approvals keep reaching the operator on every profile"), and today's behavior, where the shell, plugin and cancel challenges reach the operator on every profile.
  - **Except what the gateway would refuse (L4).** `gateway.Review` reports `Denied` when the call would be refused at execution: a deny policy, or D7.7.1 on a gated call (an `ask` policy or a gated tier under `server_production`). The tool source does not park a `Denied` call. It goes to dispatch, and the gateway refuses it with `no_approver` there, so no card is shown that can never run.
- **The review context.** `withholdForApproval` builds it as `tools.WithOriginConversationID(tools.WithToolCallContext(ctx, a.sessionID, call.ID, a.runDir, a.previewCap), a.originConversationID)`.
- **The gateway.** `gateway.Review(ctx, spec, raw, key ReservationKey, describer)` reads the session grant on `key.ConversationID`.
- **Rebuilders.** Swarm, agent_job, cron and background delegation never park, so their rebuilders (`swarm.go:481-497`, `agentjob.go:324-333`) never see a parked call.

**D15. Retired challenge types** resolve with the legacy text, whatever the action, through `retiredChallengeType(pending)`:
- `gateway_approval` from Task 8, when its route is deleted;
- the three tool types in Tasks 12–14.

**D16. Phase boundary.** Through Phase 1, the shell, plugin and cancel challenges, their stores, hooks and texts, and the `ask_user` `resume_context` property stay. An `ask` policy on those tools gives more than one card until Phase 2; Task 11 measures it or lists it under "Not shown".

**D17. Priority.** Gateway: `ApprovalPriority(tier)`. Tool: `ApprovalPriority(scoring.Destructive)` = 80.

**D18. The dedupe moves before pause detection** (`llm_agent.go:509-518`). It also collapses identical `ask_user` and draft calls in one message. That is a small, named change to "free ask_user is unchanged": the model wrote the same question twice.

**D19. The park round, and the sibling boundary (AM-Siblings).**
- **Agent.** When pass 2 parks at least one call, `parkRound` (`internal/agent/llm_agent_park.go`) does four things:
  1. it drops every `text_response` call from the step, as pass 2 does today, and appends `assistant{every other call}` to history;
  2. it yields `Actions.ParkedTurn = &ParkedTurn{Calls, Pauses}` (`json:"-"`), where `Calls` excludes the dropped terminal;
  3. it dispatches the unparked calls through `dispatch`, in model order, with the round's `modelRound`. No terminal reaches `dispatch`, so `runTerminal` (`llm_agent_dispatch.go:160-163`) never yields a final answer ahead of the pauses (N2);
  4. it yields the pause events, back to back, and nothing after them. Since #300 (`8e6a92199`, on `origin/master`) the AG-UI translator reads every pause of a round and ends the run at the first event that is not one, so the pauses come last, contiguous. #300 already satisfies v2's worry that a second card never reached the stream. `ParkedTurn` additionally commits every pause row before any is yielded, which protects the consumers that stop at the first pause (Telegram, the REPL).
  - `parkRound` returns before the post-dispatch steer drain point (`llm_agent.go:535`), so a park round never drains a steer into a sibling's result.
  - **Dispatch's turn-ending exits, in a park round.** `parkRound` calls `dispatch` with a park-round option, and every exit that could yield ahead of the pauses is suppressed:
    - the terminal-exclusivity rejection (`llm_agent_dispatch.go:46-52`) and `runTerminal` (`:160-163`) are unreachable, because the terminal was dropped in step 1;
    - **a dedup trip** (`:78-85`) today writes its synthetic results to `a.history` only, then calls `maybeRecover` or `finalize`. In a park round it yields those results as `ToolInvocationEnd` events, which the runner persists, and returns without `maybeRecover` or `finalize`, so no final answer precedes the pauses. Otherwise the siblings would reload with the crash stand-in;
    - a consumer stop and an infra error end the round as today; the pause rows are already committed with `ParkedTurn`.
- **Runner.** On `ParkedTurn` it commits `assistant{Calls}`, every pause row and every record in one `CommitPause` transaction, marks the siblings in `persistedCalls` (D5), and sets `tr.paused` and `tr.parked` (M3). The pause events after it are no-ops, and `flushPause` skips a parked tracker. With `tr.paused` set, `recordInterruptedRound` (`runner_interrupted.go:56-58`) never appends an "interrupted" assistant turn after the parked turn, whether the park round is cancelled during a long sibling or the consumer stops. A sibling's result lands right after, through the normal spill path (R8). It is not inside that transaction, because `AppendTurnTx` cannot spill. A crash between the two gives the sibling the loader's "unknown" placeholder, which is true.
- **Boundary.** The rule covers every pass-2 source. Drafts and host review join: their siblings run, while their card, edit, resolve routes and answer stay. Their resolve appends the answer after the siblings' results, which `validToolResultGroup` accepts in any order (`store_helpers.go:342-362`). Since #300 (`67c84e5b9`) a draft's answer also carries the operator's edits (`withOperatorEdits`); that is unchanged by this plan.
- **Pass 1.** `ask_user` keeps dropping siblings (map R4).
- **Accepted limits.**
  - AD-M9: a draft sibling is sent in its resolve, which can be before an accepted call of the same turn runs.
  - A sibling after a parked call runs before it: in `[rm -rf build (parked), ls build]` the `ls` runs first. The spec accepts this ("a call that comes after a parked call … still runs").
  - Phase 2: a sibling that changes the state a tool review pinned makes the accept refuse rather than run in the wrong state (D23, N3).

**D20. Repair and display (AM-1), keyed by the call, not the bare id (XS-G3).**
- **The key.** `toolapprovals.CallKey{AssistantSeq int; ToolCallID string}`. A record is keyed on its pause token and holds `assistant_seq`, so a pause token names exactly one `CallKey`. No history message and no snapshot row carries a pause token, but every persisted turn carries its seq, so the consumers join on `(assistant_seq, tool_call_id)`, which is the (pause token, call id) key read through the record.
- **The repair.** `conversations.ContextConfig.OpenToolCalls map[conversations.OpenCall]struct{}` (`OpenCall` has the same two fields; `conversations` does not import `toolapprovals`, the Runner converts) reaches the five repair call sites of `context.go:196-252`. Today they repair after `toMessages` (`context.go:202,205,219,232,251`), where the seq is gone. So the managed repair runs over the seq-carrying turns first and converts afterwards: `toolPairs[T]` (`store_helpers.go:205-213`) is already generic, and the managed instance becomes a turn-typed one. `appendRecoveredToolResultGroup` (`store_helpers.go:272-290`) adds no stand-in for an open call of **that** assistant turn; a repeated id in an earlier turn still gets its stand-in.
- The Runner fills it with the keys it is about to run, which are the only open calls a loaded continuation can hold (D4).
- **The display.** `markAwaitingInput` (`server_display.go:397-412`) reads `displaySnapshotMessage.seq` (`server_display.go:54-56`) and matches `(seq, call id)` for the conversation's `accepted` and `running` records, through a cmd/aura adapter over `toolapprovals.OpenCalls` (renamed from `OpenCallIDs`; it returns `[]CallKey`). Plain `ask_user` pending pauses keep today's bare-id set: they are outside this project and have no record.
- **The declined list.** `askuser.(*Store).ListDeclinedToolApprovalCallsForIdentity` returns `[]askuser.DeclinedCall{PauseToken, ToolCallID string; AssistantSeq int}`, joined to the record by pause token, and the snapshot marks only the call at that seq.
- **The web fold.** `refuseDeclinedCall` (`web/src/chat/ExternalStoreChat_folds.ts:121-145`) has no seq, and already refuses to paint when more than one call in the thread carries the id ("the wrong call painted is worse than none"). That guard is the safe variant: it moves to `tool_call_id`, and a test pins that a repeated id paints nothing. The reload then paints the right one from the server's keyed set.

**D21. Accepted TTL (M1).** `acceptedRunWindow = 30 * time.Minute`, the drafts' review window (`llm_agent_message_draft.go:44`).
- **From when.** The window counts from the moment the last card of the parked turn is decided: `lastDecidedAt` = the latest of the turn's records' `decided_at` and the `resumed_at` of the turn's other pauses (drafts and host review have no record; their pause rows do). A turn with a card still pending has no window running: accepting A and then B 31 minutes later runs both.
- **Who applies it.** The continuation (D4.2), and the approval expiry sweep (`approval_expiry.go`, via `closeStaleAccepted`). Both read `parkedTurnState` (D24). `StaleAccepted` returns candidates by `decided_at`; the Go side groups them by turn and keeps only turns with no pending card and `lastDecidedAt` before the cutoff. No new environment variable.

**D22. Hooks (AM-6).** During the prelude, `LlmAgent.approvedRun` holds the approved ids. When `dispatch`'s `BeforeTool` loop (`llm_agent_dispatch.go:62-72`) gets a non-nil answer for one of them, it never applies the rewrite. It records a veto result with the hook-refusal text and `Refused: true`. The runner's End handler then moves the record `running → not_run`.

**D23. Executed arguments (AM-9, Phase 2).** `CallReview.RanNote` is stored on the record and carried by `ApprovedCall`. `runTool` prefixes the result preview with `[Aura ran it <note>]`. The note is "in <cwd>" for the shell, and "at commit <c>" for a plugin.

**The shell's working directory (N3, Phase 2).** Pass-2 reviews run in `pauseCalls`, before any sibling runs, and the shell review reads the tracked cwd (`shell_exec_sandbox.go:197`) that a sibling `cd` changes (`shell_exec_session.go:29`). In `[shell_exec "cd proj", shell_exec "rm -rf build"]` the rm would be pinned to the pre-`cd` directory.
- **Chosen: refuse at execution when the pinned cwd is no longer the tracked one.** Ordered evaluation is not possible: D19 commits every pause in one `CommitPause` transaction before any sibling runs, so a review cannot follow a sibling without splitting that transaction.
- **How.** When the review took the cwd from tracking (the model passed none), its `Binding` is `tracked-cwd:<dir>`. At execution, `ShellExec.Execute` compares it with the tracked cwd, and on a difference returns a refusal the model can read: `` `<command>` was approved for <pinned> but the shell is now in <tracked>: it was not run. Call it again if it is still needed. `` The record ends `done` with that result; nothing ran. A cwd the model passed explicitly has `Binding` "" and runs where it says. A call reviewed with no tracked cwd (the image `WORKDIR`) has no binding either.
- **The tracked map is in memory** (`s.cwd`, cleared by `Evict` and by a restart). `Evict` runs only on Stop and delete (`runner_resume.go:373`, `runner_delete.go:190`), and Stop closes the parked turn first (D4.6). After a restart, though, the tracked cwd is empty, and a sibling `cd` that ran before the restart is invisible. So an empty tracked cwd at execution counts as a difference: the accept is refused with `` `<command>` was approved for <pinned>, but the shell's working directory was reset since: it was not run. Call it again if it is still needed. `` That is the spec's "an accept after a restart … refuses it cleanly".
- **The converse is refused too.** In `[rm -rf build (parked), cd other]` the `cd` runs in the park round, so the accept is refused the same way. A test pins it; it fails closed, and the model re-emits with a card that shows the real directory.
- The report's other option (skip the tool park for a call that follows a same-tool mutating sibling, and refuse it readably so the model re-emits it next round) avoids a card that cannot run, but adds a second rule on the batch shape. It was not taken.
- **Task cancel** has the same shape (a sibling `task update` changes `taskCancelDigest`), and already fails closed through its `Binding` (Task 14).

**D24. One predicate: an open parked turn (N1, M3).** `Runner.openParkedTurn(ctx, convID) (parkedTurnState, bool, error)` is true while any record of the conversation is `pending`, `accepted` or `running` (`Store.Open`). `parkedTurnState` carries the turn's `AssistantSeq`, its records, the count of pending pauses whose call is in that turn, and `lastDecidedAt` (D21). It is **ready** when no record is `pending`, no pause of the turn is pending, and no record is `running` after step D4.1. The same predicate gates:
- **D4.4** (a turn without a message runs nothing unless ready), **D4.5** (the wake and `PreparePendingSteer` defer);
- **`deliverLeftoverSteer`** (`runner_steer.go:153-187`, wrapping every turn at `runner.go:206`, `runner_steer_wake.go:60` and `runner_steer_pending.go:32`): while the predicate holds after the inner turn, it skips `Drain` and holds the queue, so the steer waits for the ready continuation;
- **`recordInterruptedRound`** (`runner_interrupted.go:56-76`): it returns nil while the predicate holds, besides `tr.paused` (D19). That covers a prelude cancelled after its Starts, where the records are `running` and the next turn's D4.1 commits their results adjacent to the parked turn;
- **`MintApprovalPause`** (`runner_mint_approval.go:37-80`): it returns `ErrParkedTurnOpen` while the predicate holds instead of appending an assistant turn after the parked turn. Its one caller is the scheduled-approval ensurer (`cmd/aura/serve_scheduled_approval.go:81`), driven by the reminder sweep. The sweep already logs an ensure error and retries on the next cadence (`internal/cron/deliver_approval.go:136-149`), so no caller changes: the reminder card is minted one cadence after the parked turn closes. The reminder's lapse is unchanged.

**Held steers.** A steer held behind an open parked turn is exempt from `AURA_STEER_QUEUE_TTL_SEC` (900 s, `config.go:441`); its clock starts when the parked turn closes.
- **Why on the row.** The sweep is not the only expiry: `Drain` also skips a row past `expires_at` (lazy expiry, `sqlc.AuraSteerQueue.ExpiresAt`). So the store clears the row's expiry rather than teaching the sweep an exception.
- **The seam.** `runner.SteerHolder` with `Hold(ctx context.Context, convID string) error` and `Release(ctx context.Context, convID string) error`, implemented by `*steer.PostgresStore` and the `steertest` fake, wired as `Deps.SteerHold` next to `Deps.Steer` (`runner_deps.go:177-188`, which is Drain-only).
- **`Hold`** sets `expires_at = NULL` on the conversation's undrained, unexpired rows whose `target_worker_id IS NULL` and whose `expires_at` is set. A NULL `expires_at` already means "never expires" (the column comment), and `ListDueSteerRows` skips it. Worker-targeted rows are never held: `steer/worker.go:155` reads their expiry.
- **`Release`** sets `expires_at = now() + <the kind's own TTL>` on those rows, from the store's per-kind knobs (`pg_store.go:152`). A kind whose knob is ≤0 was NULL from its push and stays NULL.
- **Who calls them.** `deliverLeftoverSteer`'s skip branch, D4.4's no-op turn and D4.5's deferral call `Hold`. Every runner method that moves a record to a terminal state calls `releaseIfClosed(ctx, convID)` once after its commit, which calls `Release` when the predicate no longer holds. These are the resolve (decline, expiry), `closeParkedTurn`, `closeStaleAccepted`, the D4.1 recovery and the prelude's last End.
- **The sweep is the catch-all.** Not every producer runs through a held site: `PushDelegationResultIdempotent` (`pg_store.go:86`) lands a swarm result at any time, with no wake. So the steer sweep's per-row transaction (`queue_sweep.go:96-121`, identity-scoped, where the RLS'd records are visible) checks the conversation for an open record first (a new query, `ConversationHasOpenToolApproval`). When there is one, it holds the row (`expires_at = NULL`) instead of expiring it and writes no trace. `Release` restores it later.
- **NULL is read as "never expires" everywhere.** `DrainSteerRows`, `ListPendingDelegationResults` and `MarkFanoutNudged` all test `expires_at IS NULL OR expires_at > now()` (`steer_queue.sql:66-87,114-175`). So a held delegation result is still nudged as today.
- **Limits.**
  - A close that no turn follows (an expiry sweep) delivers nothing by itself. The released steer gets its full TTL and reaches the conversation's next turn, or expires with its trace, as any steer does today. The trace then lands after a closed parked turn, which breaks nothing.
  - A row whose TTL lapses between two sweep ticks while no held site ran is skipped by `Drain`'s lazy expiry until the next tick holds it. If the parked turn closes inside that window, the row expires with its trace. The window is one sweep interval.
  - Held rows count against the queue cap (`Config.Max`, `pg_store.go:153`) for as long as the parked turn is open, up to the 48 h card TTL. A full queue refuses further pushes loudly (`diagnosePushRefusal`), as it does today.

---

## File structure

| Phase | Files | Responsibility |
|---|---|---|
| 1 | `internal/db/migrations/NNNN_tool_approvals.{up,down}.sql`, `internal/db/queries/tool_approvals.sql`, `internal/toolapprovals/{store.go,main_test.go,store_integration_test.go}` | the record |
| 1 | `internal/agent/tools/approved_call.go` (+ test) | `ApprovedCall`, the context, the fingerprint |
| 1 | `internal/gateway/review.go` (+ test); `decide.go`, `approve.go`, `grants.go`, `scope.go`, `approvals.go` | the decide-only half and the hard deny |
| 1 | `internal/agent/{llm_agent_approval.go,llm_agent_park.go,llm_agent_approved.go}` (+ tests); `event.go`, `llm_agent_pause.go`, `llm_agent.go`, `llm_agent_construct.go`, `llm_agent_dispatch.go` | park, siblings, prelude |
| 1 | `internal/runner/tool_approval_{park,resume,run,open,text}.go` (+ tests); `runner_persist_pause.go`, `runner_persist.go`, `runner_resume.go`, `resume_committer.go`, `interfaces.go`, `runner_deps.go`, `runner.go`, `runner_steer.go`, `runner_steer_wake.go`, `runner_steer_pending.go`, `runner_interrupted.go`, `runner_mint_approval.go`, `approval_expiry.go` | persist, resolve, continue, the open-parked-turn predicate (D24) |
| 1 | `internal/steer/pg_store.go`, `internal/db/queries/steer_queue.sql`, `internal/steer/steertest` | held steers (D24) |
| 1 | `internal/conversations/{context.go,store_helpers.go,context_budget.go,store_path_after.go}` | repair skip, path tail |
| 1 | `internal/approvaltext/{presentation.go,presentation_tool.go}` | stored presentation, `Brief` |
| 1 | `internal/askuser/store_identity.go`, `paused_states.sql`, `internal/agui/{approvals_api.go,server.go,server_messages.go}`, `internal/channels/telegram/{hitl.go,hitl_tool_approval_test.go}`, `cmd/aura/{chat_boot.go,serve_agui.go,serve_adapters.go}`, `web/src/...` | surfaces |
| 2 | `internal/agent/tools/{call_review.go,shell_review.go,pack_review.go,task_cancel.go}`; deletions per task | tool sources |
| 3 | spec section "Phase 3 ruling" | the `task schedule` ruling |

---

## Phase 1: the mechanism, then the gateway

### Task 1: The `tool_approvals` record

**Files:**
- Create: `internal/db/migrations/NNNN_tool_approvals.{up,down}.sql`, `internal/db/queries/tool_approvals.sql`, `internal/toolapprovals/{store.go,main_test.go}`
- Modify: `internal/db/db_unit_test.go:405-407` (head pin), `scripts/coverage_package_policy.json`, `internal/db/sqlc/*` (regenerated)
- Test: `internal/toolapprovals/store_integration_test.go`, `internal/db/migrate_NNNN_integration_test.go`

**Interfaces:**
- Produces, package `toolapprovals`:
  ```go
  type Status string // pending accepted running done unknown declined expired cancelled not_run
  type Source string // "tool" | "gateway"
  type CallKey struct { AssistantSeq int; ToolCallID string } // D20: the (pause token, call id) key read through the record
  type Input struct {
      IdentityID, ConversationID, PauseToken, ToolCallID string
      AssistantSeq, CallIndex                          int // AssistantSeq set by CommitPause
      ToolName                                         string
      Source                                           Source
      ExecuteArgs                                      json.RawMessage
      Fingerprint, Binding, RanNote, Brief, GrantTool, GrantAction string
  }
  type Record struct { ID string; Input; Status Status; Scope, OperatorID string; DecidedAt time.Time }
  type Decision struct { PauseToken string; To Status; Scope, OperatorID string } // To: accepted|declined|expired|not_run|cancelled
  var ErrUnavailable = errors.New("tool approval unavailable")
  func NewStore(pool *pgxpool.Pool) *Store
  func (s *Store) InsertTx(ctx context.Context, q *sqlc.Queries, in Input) error
  func (s *Store) DecideTx(ctx context.Context, q *sqlc.Queries, d Decision) (Record, error)
  func (s *Store) Open(ctx context.Context, ownerID, conversationID string) ([]Record, error) // pending|accepted|running, (assistant_seq, call_index)
  func (s *Store) OpenCalls(ctx context.Context, ownerID, conversationID string) ([]CallKey, error) // accepted|running, keyed (assistant_seq, tool_call_id)
  func (s *Store) Decided(ctx context.Context, ownerID, conversationID string, assistantSeq int) ([]Record, error) // every record of one parked turn, for lastDecidedAt (D21)
  func (s *Store) StaleAccepted(ctx context.Context, ownerID string, decidedBefore time.Time) ([]Record, error)
  func (s *Store) TransitionTx(ctx context.Context, q *sqlc.Queries, id string, from, to Status) error
  func (s *Store) Transition(ctx context.Context, ownerID, id string, from, to Status) error
  func (s *Store) ResultCommitted(ctx context.Context, ownerID string, r Record) (bool, error) // a tool turn for r.ToolCallID with seq > r.AssistantSeq
  ```
  - `DecideTx` is `UPDATE … WHERE pause_token=$1 AND status='pending' RETURNING *`.
  - `Transition` follows D1's table. Any other pair, or 0 rows, is `ErrUnavailable`.
- **Table.** It holds:
  - `id uuid PK`;
  - `identity_id` and `conversation_id` (uuid), with FKs `ON DELETE CASCADE`;
  - `pause_token uuid UNIQUE`, `assistant_seq int`, `call_index int >= 0`, `tool_call_id`, `tool_name` (1–128 chars);
  - `source` CHECK;
  - `execute_args jsonb` object, `execute_fingerprint` (64 chars);
  - `review_binding`, `ran_note`, `brief`, `grant_tool`, `grant_action` (bounded text);
  - `status` CHECK of the nine;
  - `approval_scope` CHECK or NULL, `operator_id uuid`;
  - `created_at`, `decided_at`, `started_at`, `finished_at`.

  Constraints:
  - `UNIQUE (conversation_id, assistant_seq, call_index)`;
  - the decided-state and started-state CHECKs;
  - a partial index `(identity_id, conversation_id, assistant_seq, call_index) WHERE status IN ('pending','accepted','running')`;
  - the grants and both RLS policies, copied from `0134_message_drafts.up.sql:41-51`;
  - the D1 revert guard: `aura.refuse_resume_of_pending_tool_approval()` (`SECURITY DEFINER`, `SET search_path = pg_catalog, aura`, `REVOKE ALL … FROM PUBLIC`, as `0103_steer_queue.up.sql:113-123`) and a `BEFORE UPDATE OF resumed_at ON aura.paused_states` trigger that calls it when `OLD.resumed_at IS NULL AND NEW.resumed_at IS NOT NULL AND OLD.resume_context->>'type' = 'tool_approval'`. It raises when a record with that `pause_token` is `pending`. The down migration drops the trigger and the function first.

- [ ] **Step 0: Take the slot.** `ls internal/db/migrations/ | tail -1` → `NNNN` = that number + 1. Use it in every name below and in the head pin.
- [ ] **Step 1: Write the failing tests.**
  - `TestDecideIsOneShot`: two goroutines; one record back, the other `ErrUnavailable`.
  - `TestTransitionsFollowTheTable`: every allowed pair once; `pending→running` and `done→running` are refused.
  - `TestRepeatedCallIDAcrossRoundsIsTwoRecords` (AM-5): the same `tool_call_id` at `assistant_seq` 4 and 9 inserts twice.
  - `TestResultCommittedOnlyAfterTheParkedTurn`: a tool turn with that id at seq 3 does not count for `assistant_seq` 4; one at seq 6 does.
  - `TestOpenOrdersByCallIndex`.
  - `TestRecordsAreOwnerScoped`.
  - `TestStaleAcceptedHonoursDecidedAt`.
  - `TestOpenCallsKeysByAssistantSeq` (XS-G3): the same call id accepted at seq 4 and done at seq 9 returns only `{4, id}`.
  - `TestDecidedReturnsEveryRecordOfTheTurn`.
  - `TestMigrationNNNNCreatesToolApprovals`: RLS on, both policies, the trigger present.
  - `TestTriggerRefusesResumingAToolApprovalPauseWithAPendingRecord` (AD-M7): a bare `MarkPausedStateResumed` on such a pause, as a reverted binary would run it, fails, and `resumed_at` stays NULL.
  - `TestTriggerAllowsTheResolveThatDecidesTheRecordFirst`: `DecideTx` then `MarkResumedFencedTx` in one transaction commits.
  - `TestTriggerIgnoresOtherPauseTypes`: an `ask_user` pause resumes as today.
- [ ] **Step 2: Run** `DBI ./internal/toolapprovals/ ./internal/db/ -run 'TestDecideIsOneShot|TestTransitions|TestRepeatedCallID|TestResultCommittedOnly|TestOpenOrders|TestRecordsAreOwner|TestStaleAccepted|TestOpenCallsKeys|TestDecidedReturns|TestMigrationNNNN|TestTrigger'`. Expected: FAIL.
- [ ] **Step 3: Implement.** Migration, queries, `GO 'make sqlc'`, the store (owner methods through `db.WithIdentityTx`, as `messagedrafts/store.go:319`), the head pin, the policy entry.
- [ ] **Step 4: Track the new package for the gates (N5).** `git add -N internal/toolapprovals` (intent to add, nothing staged). Then `GO 'bash scripts/go_packages.sh | grep -x ./internal/toolapprovals'` prints the package. Without this, `scripts/go_packages.sh` skips the untracked directory, and the production deadcode gate reports Task 1's allowlist entry as stale.
- [ ] **Step 5: Run** Step 2's command, plus `GO 'go test ./internal/db/ -run TestMigrationHead'` and the common gates. Expected: PASS, and the production deadcode gate prints no stale entry.
- [ ] **Step 6: Commit** "Add the durable record of a parked tool call". Paths: the two migration files, `tool_approvals.sql`, `internal/db/sqlc`, `internal/toolapprovals`, `db_unit_test.go`, `migrate_NNNN_integration_test.go`, `coverage_package_policy.json`, `scripts/deadcode_production_allowlist.txt`.

### Task 2: The authorization and the gateway's decide-only half

**Files:**
- Create: `internal/agent/tools/approved_call.go`, `internal/gateway/review.go`
- Modify: `internal/gateway/decide.go:69-127`, `approve.go:104-128` (order of D7; `:127` operator = identity), `scope.go:143-150`, `grants.go:82-103`, `approvals.go:86-89` (the fingerprint moves)
- Test: `internal/agent/tools/approved_call_test.go`, `internal/gateway/review_test.go`, `internal/gateway/grants_test.go`, `scope_test.go`, `policy_test.go` (callers of `gatewayArgsFingerprint`)

**Interfaces:**
- Produces:
  ```go
  // tools
  type ApprovedCall struct {
      ConversationID, ToolCallID, ToolName string
      Arguments                            json.RawMessage
      Fingerprint, OperatorID, Scope       string
  }
  func WithApprovedCalls(ctx context.Context, calls []ApprovedCall) context.Context
  func ApprovedCallFor(ctx context.Context) (ApprovedCall, bool)
  func (c ApprovedCall) Authorizes(toolName string, raw json.RawMessage) bool
  func CallFingerprint(raw json.RawMessage) string // hex(sha256(canonicaljson.CanonicalArgs(string(raw))))
  // gateway
  type Review struct {
      Needed       bool
      Tier         scoring.RiskTier
      Question     string
      Presentation approvaltext.Presentation
      Options      []ScopeOption
      SubjectTool, SubjectAction string
  }
  func (g *Gateway) Review(ctx context.Context, spec tools.Spec, raw json.RawMessage, key ReservationKey, describer tools.ApprovalDescriber) (Review, error)
  func ScopeForAnswer(options json.RawMessage, answer string) ApprovalScope
  type GrantRequest struct { ConversationID, IdentityID, OperatorID, Tool, Action string; Scope ApprovalScope }
  func (g *Gateway) RecordGrant(ctx context.Context, r GrantRequest) ApprovalScope
  ```
  `Binding` and `RanNote` are added to `ApprovedCall` in Task 12, where both are first read (the shell's `tracked-cwd:` binding, D23).

- [ ] **Step 1: Write the failing tests.**
  - `approved_call_test.go`:
    - `TestCallFingerprintIgnoresKeyOrder`;
    - `TestApprovedCallForMatchesSessionAndCallID`;
    - `TestAuthorizesRequiresNameAndFingerprint`.
  - `review_test.go`:
    - `TestReviewNeedsADestructiveCallWithAPerson`: three options; the presentation is `Valid`; `args` holds keys only, no values.
    - `TestReviewUnderAskPolicyOffersNoAlways`.
    - `TestReviewIsNotNeededWhen`, table-driven: dev, **server_production**, deny policy, session grant on `key.ConversationID`, always grant, no responder, delegated, Normal tier.
    - `TestReviewWritesNothing`.
    - `TestDecideHonoursAnApprovedCall`: one `Reserve` with `operator_id` and `approval_scope`.
    - `TestApprovedNormalTierCallReservesWithTheOperator` (M4): a Normal-tier call (a `task cancel`) authorized by an `ApprovedCall` is reserved once with its `operator_id` and `approval_scope`; the same call without one is reserved with neither, as today.
    - `TestAuthorizationNeverAllowsADenyPolicy` (M4): an `ApprovedCall` for a call under a deny policy is still Deny `policy`.
    - `TestDecideRefusesAnApprovedCallWithOtherArgs`.
    - `TestDenyPolicyAddedAfterTheParkStillRefuses`.
    - `TestApprovedCallUnderProductionIsDenied` (AM-2): `no_approver`, nothing reserved.
    - `TestHeadlessWithAnAlwaysGrantStaysDenied` (AM-2): no responder plus an always grant → Deny `no_approver`.
    - `TestSyncWorkerWithASessionGrantIsAllowed`: unchanged from today.
    - `TestSyncWorkerWithoutAGrantIsDeniedNoApprover`.
    - `TestAlwaysGrantRecordsTheIdentityAsOperator` (CR-M17).
    - `TestScopeForAnswerReadsStoredOptions`.
- [ ] **Step 2: Run** `GO 'go test -race ./internal/agent/tools/ ./internal/gateway/ -run "TestCallFingerprint|TestApprovedCallFor|TestAuthorizes|TestReview|TestDecide|TestApprovedNormalTier|TestAuthorizationNeverAllows|TestDenyPolicyAdded|TestApprovedCallUnderProduction|TestHeadlessWithAnAlways|TestSyncWorker|TestAlwaysGrantRecords|TestScopeForAnswer"'`. Expected: FAIL.
- [ ] **Step 3: Implement** D7 (step 8 reads the authorization for every strict reservation, M4) and D8. `ApproveChallenge` calls `RecordGrant`. `gatewayArgsFingerprint` is replaced by `tools.CallFingerprint`.
- [ ] **Step 4: Run** Step 2's command, `GO 'go test -race ./internal/gateway/ ./internal/agent/tools/'`, `DBI ./internal/gateway/ -run .` and the common gates. Expected: PASS.
- [ ] **Step 5: Commit** "Decide a gateway approval without reserving; keep the hard deny first". Paths: `internal/agent/tools/approved_call.go`, `internal/agent/tools/approved_call_test.go`, `internal/gateway`, `scripts/deadcode_production_allowlist.txt`.

### Task 3: The agent's park round, with siblings running

**Files:**
- Create: `internal/agent/llm_agent_approval.go`, `internal/agent/llm_agent_park.go`
- Modify:
  - `internal/agent/event.go:143-155`: `ParkedApproval`, `AwaitingInput.Approval`, `Actions.ParkedTurn`;
  - `llm_agent_pause.go:62-95,134-145`: the pass-2 producer and the `approval` field;
  - `llm_agent.go:505-519`: dedupe first, then `parkRound`, after the round's `ic.WithContext(withModelRound(...))`;
  - `llm_agent_construct.go:105`: `ParkApprovals`.
- Test:
  - `internal/agent/llm_agent_approval_test.go`, `internal/agent/llm_agent_park_test.go`;
  - **Tests that pin dropped siblings.** Measured at `75f4c36f8` across `internal/agent`, `internal/runner`, `internal/agui`, `cmd/aura` and `web/src`, only `internal/agent/llm_agent_pause_test.go:135-170` (`TestPause_IntraTurnExclusivity`) asserts it. That test is pass 1 (`ask_user`), and it stays as is. The pass-2 tests (`llm_agent_host_review_test.go:57`, `llm_agent_message_draft_test.go:51`) park a single call and pin no sibling. No existing test changes; the new tests below pin "siblings run";
  - **Comments.** `llm_agent_pause.go:97-99` and `runner_persist.go:46` say the assistant message is rewritten to the pause calls. That becomes true of pass 1 only;
  - `internal/swarm/swarm_park_test.go`, `internal/cron/handlers/agentjob_park_test.go`.

**Interfaces:**
- Consumes: `gateway.Review`, `tools.CallFingerprint` (Task 2).
- Produces:
  ```go
  type ParkedApproval struct {
      Source               string // "gateway" (Phase 1), "tool" (Phase 2)
      ToolName             string
      ExecuteArgs          json.RawMessage
      Fingerprint, Binding, RanNote string
      Presentation         approvaltext.Presentation
      SubjectTool, SubjectAction string
      CallIndex            int
  }
  type ParkedTurn struct { Calls []llm.ToolCall; Pauses []*AwaitingInput } // Actions.ParkedTurn, json:"-"
  func (a *LlmAgent) withholdForApproval(ctx context.Context, call llm.ToolCall, index int) (pauseCall, bool)
  func (a *LlmAgent) parkRound(ic InvocationContext, spanID [8]byte, requestID string, calls []llm.ToolCall, pauses []pauseCall, acc *turnUsage, yield func(*Event, error) bool) (infraErr error)
  ```

- [ ] **Step 1: Write the failing tests.**
  - `llm_agent_approval_test.go` (strict gateway, scripted client, `ParkApprovals` on):
    - `TestGatedCallParksInsteadOfExecuting`: `Approval.Fingerprint == CallFingerprint(args)`.
    - `TestSeveralParksInOneTurn`: `CallIndex` 0 and 1, model order.
    - `TestDraftsAndHostReviewParkFirst`.
    - `TestNoParkWithout`, table-driven: switch off, no responder, delegated, no identity, deferred and unloaded, deny policy, review error, `text_response` in the batch. Every case dispatches as today.
    - `TestReviewSeesTheCallContext` (AM-4): through `pauseCalls`, a session grant recorded for the conversation skips the park; the describer sees `SessionIDFromContext == a.sessionID`.
    - `TestIdenticalGatedCallsInOneTurnParkOnce`.
    - `TestAskUserWithAGatedCallKeepsAskUserPrecedence`: one ask_user pause; the gated call is dropped, unrun.
  - `llm_agent_park_test.go`:
    - `TestSiblingsRunInTheParkRound`, with `[read A, gated G, read B]`: the events are one `ParkedTurn` carrying all three calls, then start and result of A and B, then one pause for G. G's `Execute` is never called; the history is `assistant{A,G,B}, tool(A), tool(B)`.
    - `TestSequentialBatchAfterAParkStillRuns`: a mutating sibling after G runs, in order.
    - `TestDraftSiblingsRun`.
    - `TestHostReviewSiblingsRun`.
    - `TestConsumerStopAtTheFirstPauseStillRanSiblings`: a consumer that stops at the first pause (Telegram, the REPL) has seen every sibling's start and result, and `ParkedTurn` carried every pause. (Since #300 the AG-UI translator reads every contiguous pause, so the AG-UI case is Task 7's.)
    - `TestPausesAreLastAndContiguous`: after the first pause event of a park round, every event until the end of the stream is a pause.
    - `TestParkRoundDrainsNoSteer`: a steer queued before the park round stays queued; no `SteerDelta` event, no sibling result carries it.
    - `TestDraftParkWithATextResponseSiblingDropsTheTerminal` (N2): `[send_email, text_response]` yields the draft card and no final answer. The events have no text-response chunk and no `runTerminal` output; `ParkedTurn.Calls` and the history's assistant message hold `send_email` only.
    - `TestHostReviewParkWithATextResponseSiblingDropsTheTerminal` (N2): the same for a `task schedule` that host review parks.
    - `TestDraftAndGatedWithATextResponseParkBothAndDropTheTerminal` (D14): `[send_email, G, text_response]` gives two cards and no final answer.
    - `TestGatedCallWithATextResponseKeepsTheRejection` (D14): `[G, text_response]` alone gives today's terminal-exclusivity rejection, and no card.
    - `TestDedupTripInAParkRoundYieldsItsResults`: a sibling the dedup guard suppresses yields a `ToolInvocationEnd` with the suppression text, no `finalize` model call and no final answer, and the pauses still follow.
  - `TestSwarmWorkerGatedCallIsRefusedNotParked` (internal/swarm) and `TestAgentJobGatedCallIsRefusedNotParked` (cron/handlers): no `AwaitingInput`, a `no_approver` refusal, and the rebuilders never invoked (AD-M8).
- [ ] **Step 2: Run** `GO 'go test -race ./internal/agent/ ./internal/swarm/ ./internal/cron/handlers/ -run "TestGatedCallParks|TestSeveralParks|TestDraftsAndHostReview|TestNoParkWithout|TestReviewSeesTheCall|TestIdenticalGated|TestAskUserWithAGated|TestSiblings|TestSequentialBatch|TestDraftSiblings|TestHostReviewSiblings|TestConsumerStop|TestPausesAreLast|TestParkRoundDrainsNoSteer|TextResponseSibling|TestDraftAndGatedWithA|TestGatedCallWithATextResponse|TestDedupTripInAPark|IsRefusedNotParked"'`. Expected: FAIL.
- [ ] **Step 3: Implement** D14, D18 and D19 (the terminal drop, the pauses last and contiguous, the dedup-trip results, no drain after the park round). The tool card's borrowing of gateway options waits for Task 12.
- [ ] **Step 4: Run** `GO 'go test -race ./internal/agent/... ./internal/swarm/ ./internal/cron/...'` and the common gates. Expected: PASS.
- [ ] **Step 5: Commit** "Park a gated call and run its siblings in the same turn". Paths: `internal/agent`, `internal/runner/runner_persist.go`, `internal/swarm/swarm_park_test.go`, `internal/cron/handlers/agentjob_park_test.go`, `scripts/deadcode_production_allowlist.txt`.

### Task 4: The runner commits the park round

**Files:**
- Create: `internal/runner/tool_approval_park.go`, `internal/approvaltext/presentation_tool.go`
- Modify:
  - `internal/runner/runner_persist.go:117-232`: `ParkedTurn` handling, `persistedCalls` in Start/End per D5;
  - `runner_persist.go:50`: tracker fields (`persistedCalls`, `parked`);
  - `runner_persist_pause.go:28-73`: the approval branch, the reserved-type strip; `:126` `flushPause` skips a parked tracker (M3);
  - `resume_committer.go:37-47,118-133,233-243`: `SetToolApprovals`; `CommitPause` gains `approvals []toolapprovals.Input`, sets `AssistantSeq` and inserts after the pauses; split → `errToolApprovalsNotWired` when non-empty;
  - `interfaces.go:149`;
  - the fakes `approval_expiry_test.go:38` and `runner_pause_exposure_integration_test.go:71`;
  - the call sites `resume_committer_test.go:134` and `runner_mint_approval.go:80` (they pass `nil`);
  - `runner_deps.go`: `ToolApprovals ToolApprovalStore`; `New` refuses it when the committer reports no store (`HasToolApprovals() bool` on the committer interface);
  - `cmd/aura/chat_boot.go:491`: `SetToolApprovals` and `Deps`;
  - `internal/approvaltext/presentation.go:72-110`.
- Test: `internal/runner/tool_approval_park_test.go`, `internal/runner/tool_approval_park_integration_test.go`, `internal/approvaltext/presentation_tool_test.go`

**Interfaces:**
- Produces:
  ```go
  // approvaltext
  const ToolApprovalType = "tool_approval"
  func ToolApprovalContext(tool string, p Presentation) (json.RawMessage, error)
  func IsToolApproval(raw json.RawMessage) bool
  func Valid(p Presentation) bool
  func Brief(p Presentation) string // ≤120 runes; per key as below
  // runner
  type ToolApprovalStore interface {
      Open(ctx context.Context, ownerID, conversationID string) ([]toolapprovals.Record, error)
      Transition(ctx context.Context, ownerID, id string, from, to toolapprovals.Status) error
      ResultCommitted(ctx context.Context, ownerID string, r toolapprovals.Record) (bool, error)
      StaleAccepted(ctx context.Context, ownerID string, decidedBefore time.Time) ([]toolapprovals.Record, error)
  }
  func (p *PoolResumeCommitter) SetToolApprovals(s *toolapprovals.Store)
  ```
  `Brief` gives `(args: …)[ on <target>]` for the gateway, `` `<first line>` in <cwd> `` for the shell, `plugin <plugin>` for a plugin, and `cancel of task <task>` for a cancel.

- [ ] **Step 1: Write the failing tests.**
  - `TestToolApprovalContextRoundTrips`, `TestToolApprovalContextRejectsInvalidPresentation`, `TestBriefPerKey`.
  - `TestParkedTurnCommitsAllCallsOnce`: one assistant turn with `[A,G,B]` in order, one pause row, one record with `AssistantSeq` equal to that turn's seq and no `execute_args` in the context. A's and B's tool turns follow. No second assistant turn, from either the Start or the End branch (CR-M2).
  - `TestForgedToolApprovalTypeIsStripped` (Review Focus 1).
  - `TestPausesAfterParkedTurnAreNoOps`: and `flushPause` writes no second assistant turn.
  - `TestCancelDuringASiblingAppendsNoInterruptedTurn` (M3): the run ctx is cancelled while a long sibling runs; no `RoleAssistant` "interrupted" turn follows the parked turn, which stays the path's tail with its pause rows.
  - `TestConsumerStopDuringASiblingAppendsNoInterruptedTurn` (M3): the same when the consumer returns false mid-sibling.
  - `TestCommitPauseWithoutTheStoreRefusesRecords`.
  - `TestNewRefusesToolApprovalsWithoutTheCommitterStore`.
  - `TestParkRoundIsAtomicUntilTheSiblings` (db_integration): a record violating the CHECK rolls back the assistant turn and the pause.
  - `TestSiblingResultSpills` (db_integration): an over-cap sibling result lands through the spill path.
  - `TestStrictLedgerOfASiblingHasOneStartOneEnd` (db_integration, strict gateway): a park-round sibling writes one gateway start and one runner end under the park round's request id.
- [ ] **Step 2: Run** `GO 'go test -race ./internal/approvaltext/ ./internal/runner/ -run "TestToolApprovalContext|TestBriefPerKey|TestParkedTurnCommits|TestForgedToolApproval|TestPausesAfterParked|TestCancelDuringASibling|TestConsumerStopDuringASibling|TestCommitPauseWithout|TestNewRefusesToolApprovals"'` and `DBI ./internal/runner/ -run "TestParkRoundIsAtomic|TestSiblingResultSpills|TestStrictLedgerOfASibling"`. Expected: FAIL.
- [ ] **Step 3: Implement** D1, D5, D10 and D19's runner half: `ParkedTurn` sets `tr.paused` and `tr.parked`.
- [ ] **Step 4: Run** `GO 'go test -race ./internal/runner/ ./internal/approvaltext/ ./cmd/aura/'`, the DBI command and the common gates. Expected: PASS.
- [ ] **Step 5: Commit** "Commit a park round: one assistant turn, its pauses and records, then the siblings". Paths: `internal/runner`, `internal/approvaltext`, `cmd/aura/chat_boot.go`, `scripts/deadcode_production_allowlist.txt`.

### Task 5: The resolve, decline, expiry and cancel

**Files:**
- Create: `internal/runner/tool_approval_resume.go`, `tool_approval_text.go`, `tool_approval_run.go` (`closeParkedTurn` only)
- Modify:
  - `runner_resume.go`: `:92-178` grants after commit; `:219-239` D11/D12; `:297-325,368-372` cancel and Stop close first; `:356-362` the decision in `resumeClaim`, which also covers expiry and the operator paths through `operatorResumeClaim`;
  - `resume_committer.go:102-112,195-231` and `interfaces.go:117-149`: `toolApproval`, `CommitParkedOutcome` on the pool and split committers;
  - `approval_expiry_test.go:24-40`: the scripted committer implements it (CR-M7).
- Test: `internal/runner/tool_approval_resume_test.go`, `internal/runner/tool_approval_resume_integration_test.go`

**Interfaces:**
- Consumes: `DecideTx`, `TransitionTx` (Task 1); `ScopeForAnswer`, `RecordGrant` (Task 2).
- Produces:
  ```go
  // ResumeCommitter
  CommitParkedOutcome(ctx context.Context, recordID string, from, to toolapprovals.Status, turn conversations.AppendTurnParams) error
  func (r *Runner) closeParkedTurn(ctx context.Context, convID string, records []toolapprovals.Record, to toolapprovals.Status) error
  ```
  `closeParkedTurn` closes the records:
  - a **pending** one through `CommitResume`, with a claim whose answer is `{action: expired, content: <D12 text>}` and a decision to `to`;
  - an **accepted** one through `CommitParkedOutcome`, `accepted → to`.

- [ ] **Step 1: Write the failing tests.**
  - `TestAcceptOfAToolApprovalAppendsNoAnswer`.
  - `TestDeclineAnswersOnTheOriginalCall`: it ends `unless the operator asks again.`, keeps the suffix, and appends `The operator said: wait` for content `wait`.
  - `TestExpiryAnswersExpiredNotRun`.
  - `TestScopeFromTheStoredOptions`.
  - `TestAcceptClaimsOnceAcrossSurfaces` (db_integration): `SubmitAnswer` races `SubmitAnswers`; one wins, and `operator_id` is the context identity.
  - `TestForgedContextAcceptRunsNothing` (db_integration, Review Focus 1).
  - `TestAcceptACancelBSettlesBoth` (db_integration, Review Focus 2): A ends `cancelled` with the cancelled text, B ends `cancelled`, and each call has exactly one tool turn.
  - `TestSessionScopeRecordsAGrantBeforeExecution` (db_integration).
  - `TestResolveWhileTheParkRoundRunsIsBusy`: with the thread lock held, `SubmitAnswer` and `SubmitAnswers` on a `tool_approval` pause return `ErrThreadBusy`. The pause stays unresumed, and the record stays `pending`.
  - `TestHostReviewResolveWhileTheParkRoundRunsIsBusy` (D2 lock scope): the same for a `scheduled_task_approval` pause of the park round; the task row is not flipped.
  - `TestCancelWhileTheParkRoundRunsIsBusy`: `ActionCancel` answers `ErrThreadBusy` and changes nothing.
  - `TestAskUserResolveTakesNoLock`: a plain `ask_user` pause resolves with the lock held elsewhere, as today.
  - `TestResolveUnderAHeldLockProceeds`: the same call with `WithThreadLockHeld` succeeds.
  - `TestToolApprovalClaimWithoutTheStoreFails` (silent committer path): `CommitResume` and `CommitResumeBatch` on a committer without `SetToolApprovals` return `errToolApprovalsNotWired` for a claim on a `tool_approval` pause. Nothing commits: the pause stays unresumed and no answer turn is appended.
  - `TestResumeClaimCarriesTheDecisionWhateverTheWiring`: `resumeClaim` on a `tool_approval` pause sets `toolApproval` with or without `Deps.ToolApprovals`.
  - `TestExpirySweepWithoutTheStoreFailsLoudly` (db_integration): `ExpirePendingApprovals` over such a pause returns the error; the D1 trigger also refuses a hand-made `MarkResumed`.
- [ ] **Step 2: Run** `GO 'go test -race ./internal/runner/ -run "TestAcceptOfATool|TestDeclineAnswers|TestExpiryAnswers|TestScopeFromTheStored|TestResolveWhileTheParkRound|TestHostReviewResolveWhile|TestCancelWhileTheParkRound|TestAskUserResolveTakesNoLock|TestResolveUnderAHeldLock|TestToolApprovalClaimWithout|TestResumeClaimCarries"'` and `DBI ./internal/runner/ -run "TestAcceptClaimsOnce|TestForgedContextAccept|TestAcceptACancelB|TestSessionScopeRecords|TestExpirySweepWithoutTheStore"`. Expected: FAIL.
- [ ] **Step 3: Implement** D2 (the lock by pause type, `DecideTx` before `MarkResumedFencedTx`, the claim by pause type), D11, D12, D13 and D4.6.
- [ ] **Step 4: Run** `GO 'go test -race ./internal/runner/'`, the DBI command and the common gates. Expected: PASS.
- [ ] **Step 5: Commit** "Resolve a parked call in the resume transaction". Paths: `internal/runner`, `scripts/deadcode_production_allowlist.txt`.

### Task 6: The continuation, the repair, and every non-ready turn

**Files:**
- Create: `internal/agent/llm_agent_approved.go`, `internal/conversations/store_path_after.go`, `internal/runner/tool_approval_open.go` (D24: `openParkedTurn`, `parkedTurnState`, `releaseIfClosed`)
- Modify:
  - `internal/conversations/context_budget.go:17`: `OpenToolCalls map[OpenCall]struct{}`;
  - `context.go:196-252`: the five repair call sites take it, and the managed repair runs over the seq-carrying turns before `toMessages` (D20);
  - `store_helpers.go:192-290`: `repairToolPairs` and `appendRecoveredToolResultGroup` skip the open calls of their own assistant turn;
  - `internal/agent/llm_agent.go:216-238`: the prelude after `OnTurnStart`, with `modelRoundOrdinal.next`, before the round loop's first `drainSteer` (`:312`);
  - `llm_agent_construct.go`: `ApprovedCalls []tools.ApprovedCall`;
  - `llm_agent_dispatch.go:62-72`: D22;
  - `internal/runner/tool_approval_run.go`: `prepareParkedTurn`, `closeStaleAccepted`;
  - `internal/runner/tool_approval_resume.go`: `releaseIfClosed` after a decline or an expiry;
  - `runner.go:218-300`: `prepareParkedTurn` before `appendUserTurn`, its open calls into `cfg`, its approved calls into `buildAgent`;
  - `runner_steer.go:153-187`: `deliverLeftoverSteer` skips `Drain` and calls `Hold` while `openParkedTurn` holds;
  - `runner_steer_wake.go:55-65`: D4.5;
  - `runner_steer_pending.go:13-33`: the predicate before `Drain`;
  - `runner_interrupted.go:56-58`: the predicate;
  - `runner_mint_approval.go:37-80`: `ErrParkedTurnOpen`. The reminder sweep already retries an ensure error next cadence (`internal/cron/deliver_approval.go:136-149`), so no caller changes;
  - `runner_deps.go:177-188`: `SteerHold SteerHolder`; `cmd/aura/chat_boot.go`: wire the same `*steer.PostgresStore`;
  - `internal/steer/pg_store.go`: `Hold`, `Release`; `internal/steer/queue_sweep.go:96-121`: hold instead of expire while the conversation has an open record (D24 catch-all); `internal/db/queries/steer_queue.sql`: `HoldSteerRows`, `ReleaseSteerRows`, `ConversationHasOpenToolApproval`; `internal/db/sqlc` regenerated; `internal/steer/steertest`: the fake's no-op `Hold`/`Release`;
  - `approval_expiry.go:17-41`: `closeStaleAccepted`, then `releaseIfClosed` per conversation;
- Test:
  - `internal/conversations/store_helpers_open_test.go`, `internal/agent/llm_agent_approved_test.go`, `internal/runner/tool_approval_open_test.go`;
  - `internal/steer/hold_integration_test.go`;
  - `internal/runner/tool_approval_run_integration_test.go` (accept path);
  - `internal/runner/tool_approval_settle_integration_test.go` (recovery, settle, TTL);
  - `internal/runner/tool_approval_steer_integration_test.go` (wake, leftover, held steers). Three files, each under 600 lines (CR-M1).

**Interfaces:**
- Produces:
  ```go
  func (s *Store) PathAfter(ctx context.Context, conversationID string, leafSeq, anchorSeq int) (after []PathTurn, onPath bool, err error) // conversations
  type PathTurn struct { Seq int; Role, ToolCallID string }
  type OpenCall struct { AssistantSeq int; ToolCallID string } // conversations
  func (a *LlmAgent) runApprovedCalls(ic InvocationContext, spanID [8]byte, requestID string, acc *turnUsage, yield func(*Event, error) bool) (stop bool, infraErr error) // one dispatch batch
  func (r *Runner) prepareParkedTurn(ctx context.Context, convID string, input turnInput) (ctxOut context.Context, run []tools.ApprovedCall, open map[conversations.OpenCall]struct{}, err error)
  func (r *Runner) openParkedTurn(ctx context.Context, convID string) (parkedTurnState, bool, error)
  type parkedTurnState struct { AssistantSeq int; Records []toolapprovals.Record; PendingCards int; LastDecidedAt time.Time }
  func (s parkedTurnState) Ready() bool
  func (r *Runner) releaseIfClosed(ctx context.Context, convID string) error
  func (r *Runner) closeStaleAccepted(ctx context.Context, now time.Time) (int, error)
  var ErrParkedTurnOpen = errors.New("conversation has an open parked turn")
  type SteerHolder interface { Hold(ctx context.Context, convID string) error; Release(ctx context.Context, convID string) error } // runner
  ```

- [ ] **Step 1: Write the failing tests.**
  - `store_helpers_open_test.go`:
    - `TestRepairGivesNoStandInToAnOpenCall`: `assistant{A,G}, tool(A)` with `{seq, G}` open stays as is.
    - `TestRepairStillRecoversAClosedDanglingCall`.
    - `TestRepairKeysTheOpenCallByItsTurn` (XS-G3): `G` dangling at seq 4 and open at seq 9; seq 4 gets its stand-in, and seq 9 none.
    - `TestManagedAndDisplayRepairAgree`: the same fixture through `LoadManagedHistory` and `LoadDisplayTurns` gives the same pairs.
  - `llm_agent_approved_test.go`:
    - `TestApprovedCallRunsBeforeTheFirstModelCall`.
    - `TestApprovedCallsRunAsOneBatch` (AD-H3 sc. 4): two accepted calls yield both Starts before either `Execute` runs.
    - `TestSteerQueuedBeforeTheContinuationLandsAfterTheApprovedResults`: the round-1 `drainSteer` appends after the last approved result; the assistant group stays adjacent.
    - `TestApprovedCallsRunInTheModelsOrder`.
    - `TestApprovedCallsUseTheExecuteArgs`.
    - `TestAuthorizationDoesNotOutliveThePrelude` (AD-M4): round 2 re-emits the same id and args; it is not authorized and is parked again or refused.
    - `TestHookRewriteOfAnApprovedCallIsARefusal` (AM-6): the result has the hook-refusal text, `Refused`, and the original args were never executed.
    - `TestNoApprovedCallsNoPrelude`: byte-identical first request.
  - `tool_approval_open_test.go` (D24, unit, fakes):
    - `TestOpenParkedTurnCountsPendingAcceptedAndRunning`.
    - `TestReadyNeedsEveryCardOfTheTurnDecided`: an accepted record with a pending draft pause of the same turn is not ready; a pending `ask_user` pause of an older turn does not block.
    - `TestLastDecidedAtReadsRecordsAndPauses` (M1): a draft card resumed after the last record's decision moves `LastDecidedAt`.
    - `TestRecordInterruptedRoundSkipsAnOpenParkedTurn` (ruling 1): a prelude cancelled after its Starts (records `running`) appends no "interrupted" turn; the next turn's D4.1 commits `unknown` adjacent to the parked turn, and the model sees no `crash recovery` text.
    - `TestMintApprovalPauseWaitsForAnOpenParkedTurn`: `ErrParkedTurnOpen`, nothing appended; after the turn closes it mints.
  - `hold_integration_test.go` (db_integration, `internal/steer`):
    - `TestHeldSteerNeverExpiresUntilReleased`: a held row past its original `expires_at` is neither listed by `ListDueSteerRows` nor skipped by `Drain`.
    - `TestReleaseRestartsTheKindsOwnTTL`: a steer and a delegation result each get `now() + <own knob>`.
    - `TestHoldLeavesWorkerRowsAlone`.
    - `TestSweepHoldsADueRowBehindAnOpenRecord`: a delegation result pushed with no wake, past its TTL, while a record is `pending`; the sweep sets `expires_at` NULL, appends no trace, and `ListPendingDelegationResults` still returns it.
  - `tool_approval_run_integration_test.go` (real runner, `LoadManagedHistory`, strict gateway):
    - `TestAcceptRunsOnceAndTheModelSeesOneResult` (AM-1): the first `llm.Request` of the continuation has exactly one `RoleTool` per id of the parked turn and none containing `crash recovery`. The tool ran once, the record is `done`, and a second continuation runs nothing.
    - `TestOneAcceptedOneDeclined`.
    - `TestDeclineRunsNothingAndTheModelSeesIt`.
    - `TestExpiryRunsNothing`.
    - `TestLargeResultSpills`.
    - `TestStrictLedgerHasOneStartOneEndWithTheResolver` (AD-L3).
    - `TestRestartBetweenParkAndAcceptRunsOnce`.
    - `TestApprovedCallWhoseToolIsGoneEndsWithAReadableError`.
    - `TestApprovedDeferredToolStillRuns`.
    - `TestRunCancelDuringThePreludeLeavesNoAcceptedRecord` (AD-H3 sc. 4): two accepted calls; the run ctx is cancelled while the first executes (the cockpit stop's path). Afterwards no record is `accepted`; each is `done` or, after the next turn's D4.1, `unknown`. A second continuation runs nothing.
    - `TestFailedCASMidBatchRunsNothing`: the second record was moved out of `accepted` behind the runner's back; dispatch stops before `executeBatch`, the first record becomes `unknown` on the next turn, and no tool ran.
  - `tool_approval_settle_integration_test.go`:
    - `TestCrashAfterTheClaimBecomesOutcomeUnknown`.
    - `TestRunningWithACommittedResultBecomesDone`.
    - `TestUserMessageClosesPendingAndAcceptedOnTheUsersBranch` (Review Focus 3, `WithBranchLeaf` = head): both records `not_run`, both texts before the user turn, the user turn's parent the last settle seq, the next request wire-valid, nothing executed.
    - `TestContinuationWithAPendingCardRunsNoModel`: no error, no event, 0 model calls, nothing executed.
    - `TestContinuationWithOnlyAnAskUserCardRunsTheModel`: no record of this project, an `ask_user` card pending; a turn without a message runs the model as today.
    - `TestAcceptedGatedCallWaitsForADraftCardOfTheSameTurn`: a park round with a gated call and a draft. The gated call is accepted while the draft is pending, so the continuation runs nothing. The draft's resolve then gives `OutcomeContinue`, and the next continuation runs the gated call once.
    - `TestBranchRunWithoutTheParkedTurnRunsNothing`.
    - `TestAcceptedPastTheWindowIsClosedNotRun`: by the continuation and by `closeStaleAccepted`, 31 minutes after the turn's last decision.
    - `TestSecondCardAfterTheWindowStillRunsTheFirst` (M1): accept A, accept B 31 minutes later; the continuation runs A and B once each, in model order. `closeStaleAccepted` between the two decisions closes nothing.
    - `TestParkedTurnNoLongerTheTailDoesNotRun`.
  - `tool_approval_steer_integration_test.go` (real runner, PG steer store, N1):
    - `TestWakeWithAnOpenParkedTurnRunsNothing` (Review Focus 5): the steer is queued and held, no turn runs, and the next ready continuation delivers it after the approved results.
    - `TestRuntimeLeftoverSteerDuringAPendingCardIsKept`: a background completion pushed during the park round; the round's `deliverLeftoverSteer` drains nothing, no follow-on turn runs, the cards stay `pending`, and the steer reaches the ready continuation.
    - `TestWakeWithOnlyADraftCardPendingDeliversItsSteer`: no record, a draft card pending; the wake runs the model and delivers the steer, as today.
    - `TestDeferredSteerOutlivingItsTTLDoesNotBreakTheParkedTurn`: `AURA_STEER_QUEUE_TTL_SEC=1`, the steer sweep runs past it; no expiry trace is appended, the parked turn stays the tail, and the accepted call runs on the ready continuation. After the turn closes, the steer's clock restarts from the close.
    - `TestReleaseAfterASweepCloseStartsTheClock`: `closeStaleAccepted` closes the turn; the held steer gets `now() + TTL` and no turn runs.
    - `TestPreparePendingSteerDefersBehindAnOpenParkedTurn`: `(nil, false, nil)`, the queue untouched.
- [ ] **Step 2: Run** `GO 'go test -race ./internal/conversations/ ./internal/agent/ ./internal/runner/ -run "TestRepair|TestManagedAndDisplay|TestApprovedCall|TestSteerQueuedBefore|TestAuthorizationDoesNot|TestHookRewrite|TestNoApprovedCalls|TestOpenParkedTurn|TestReadyNeeds|TestLastDecidedAt|TestRecordInterruptedRoundSkips|TestMintApprovalPauseWaits"'`, then `DBI ./internal/steer/ -run "TestHeldSteer|TestReleaseRestarts|TestHoldLeaves|TestSweepHolds"`, then `DBI ./internal/runner/ -run "TestAcceptRunsOnce|TestOneAccepted|TestDeclineRuns|TestExpiryRuns|TestLargeResult|TestStrictLedger|TestRestartBetween|TestApprovedCallWhoseTool|TestApprovedDeferred|TestRunCancelDuringThePrelude|TestFailedCASMidBatch|TestCrashAfter|TestRunningWith|TestUserMessageCloses|TestContinuationWith|TestWakeWith|TestBranchRunWithout|TestAcceptedPastTheWindow|TestSecondCardAfter|TestParkedTurnNoLonger|TestAcceptedGatedCallWaits|TestRuntimeLeftoverSteer|TestDeferredSteerOutliving|TestReleaseAfterASweep|TestPreparePendingSteerDefers"`. Expected: FAIL.
- [ ] **Step 3: Implement** D3, D4, D5, D20, D21, D22 and D24. `GO 'make sqlc'` after the steer queries.
- [ ] **Step 4: Run** `GO 'go test -race ./internal/conversations/ ./internal/agent/ ./internal/runner/ ./internal/steer/... ./cmd/aura/'`, the three DBI commands, `GO 'bash scripts/check-file-size.sh internal/agent/llm_agent.go internal/runner/runner.go internal/runner/runner_persist.go internal/runner/runner_steer.go internal/conversations/context.go internal/conversations/store_helpers.go internal/steer/pg_store.go internal/runner/tool_approval_run_integration_test.go internal/runner/tool_approval_settle_integration_test.go internal/runner/tool_approval_steer_integration_test.go'` and the common gates. Expected: PASS, every file ≤600.
- [ ] **Step 5: Commit** "Run an accepted call as the continuation's first step, and never in any other turn". Paths: `internal/conversations`, `internal/agent`, `internal/runner`, `internal/steer`, `internal/db/queries/steer_queue.sql`, `internal/db/sqlc`, `cmd/aura/chat_boot.go`, `scripts/deadcode_production_allowlist.txt`.

### Task 7: Every surface, then the switch on

**Files:**
- Modify:
  - `internal/agui/approvals_api.go:168-227`: `tool_call_id` replaces `withheld_tool_call_id`;
  - `server.go:124-126`: rename, plus one `OpenToolApprovalCalls` line;
  - `server_messages.go:104-130`: the declined rename; `pendingToolCalls` adds the open records keyed `(assistant_seq, call id)` (D20);
  - `server_display.go:397-412`: `markAwaitingInput` matches an open record on `(seq, call id)`, a plain pause on its bare id as today;
  - `internal/askuser/store_identity.go:91-122`, `paused_states.sql:130-143`, sqlc: `ListDeclinedToolApprovalCallsForIdentity` joins the record by pause token;
  - `cmd/aura/serve_agui.go:127`: an adapter over the pause store and `toolapprovals.OpenCalls`;
  - `internal/channels/telegram/hitl.go:274-290`;
  - `web/src/approvals/useApprovals.ts:40`, `web/src/chat/ExternalStoreChat_folds.ts:131-136`, `ExternalStoreChat_liveRun.ts:215-238`;
  - `internal/runner/runner.go:427-470`: `buildAgent` sets `ParkApprovals` when `r.toolApprovals != nil`, the production switch (AD-M5).
- Test:
  - `internal/agui/approvals_api_unit_test.go`, `owner_scoping_test.go:174`, `internal/askuser/store_unit_test.go:220,224`, `store_declined_test.go`;
  - `internal/channels/telegram/hitl_tool_approval_test.go`;
  - `cmd/aura/chat_repl_tool_approval_test.go`, `internal/agui/approvals_tool_approval_integration_test.go`;
  - `internal/agui/translator_park_round_test.go` (new file: `translator.go` is 596 lines after #300 and is not edited);
  - `internal/agui/server_display_open_call_test.go`;
  - `web/src/chat/__tests__/{refuseDeclinedCall.test.ts,ExternalStoreChat.approvals.test.tsx,ExternalStoreChat_liveRun.parked.test.ts}`.

**Interfaces:**
- Produces:
  - `askuser.(*Store).ListDeclinedToolApprovalCallsForIdentity(ctx, conversationID, identityID string) ([]askuser.DeclinedCall, error)`, with `type DeclinedCall struct { PauseToken, ToolCallID string; AssistantSeq int }` (D20, XS-G3);
  - the approval item's JSON field `tool_call_id`;
  - the TS field `readonly tool_call_id?: string`.

- [ ] **Step 1: Write the failing tests.**
  - `TestListApprovalsProjectsTheParkedCall`.
  - `TestDeclinedToolApprovalsAreListed` (db_integration).
  - `TestAcceptedCallShowsAwaitingNotCrashed` (D20): the snapshot during the accepted window has `awaitingInput` and no recovery placeholder.
  - `TestRepeatedCallIDInAnOlderTurnIsNotMarked` (XS-G3, `server_display_open_call_test.go`): the same call id at seq 4 (crashed, with its placeholder) and seq 9 (accepted); only seq 9 shows `awaitingInput`, and seq 4 keeps its placeholder. The declined list marks only the declined record's seq.
  - `TestTranslatorEndsAParkRoundWithEveryCard` (#300, `translator_park_round_test.go`): `ParkedTurn`, two sibling start/result pairs, then two pauses. The wire has both siblings' tool frames, emits nothing for `ParkedTurn`, and has one `RUN_FINISHED` whose interrupt names both cards in model order.
  - `TestPartialAGUIResumeRunsNothingAndKeepsTheQueuedSteer` (N1, db_integration): two cards, a steer queued; `POST /agent/run` resumes one card. The run emits no model output, the second record stays `pending`, the steer stays queued and held, and no card is closed `not_run`.
  - `TestRunCancelDuringAnApprovedCallLeavesNothingAccepted` (AD-H3 sc. 4, db_integration): two accepted calls; `POST /agent/runs/{runID}/cancel` while the first runs. No record is `accepted` afterwards, and the next resume runs nothing again.
  - `TestResolveViaApprovalsAPIThenContinue` (db_integration).
  - `TestAGUIResumeAcceptExecutesInTheDetachedRun` (db_integration).
  - Telegram:
    - `TestToolApprovalWithOptionsShowsTheScopeKeyboard`;
    - `TestToolApprovalRendersItalian`, for each of the five keys;
    - `TestTypedReplyNeverAcceptsAToolApproval` (Review Focus 4);
    - `TestTelegramButtonAcceptDrivesOneContinuation` (AD-M8).
  - `TestREPLAcceptOfParkedCallDrivesTheContinuation`.
  - `TestBuildAgentParksOnlyWithTheStore`, in `internal/runner/runner_build_agent_park_test.go`.
  - `TestResolveOfABusyConversationIs409`: `approvals_api.go` maps `ErrThreadBusy` to 409 "conversation busy", as `message_drafts_api.go:168` does.
  - `TestTelegramBusyAcceptKeepsTheCard`: the callback answers a busy notice and drives no continuation. The card can be tapped again.
  - Web:
    - `refuseDeclinedCall.test.ts`, on `tool_call_id`; and "paints nothing when two calls of the thread carry the declined id" (XS-G3: the fold has no seq, so the existing single-carrier guard at `ExternalStoreChat_folds.ts:137-145` is the key's safe variant);
    - `ExternalStoreChat_liveRun.parked.test.ts`: "continuation replays the accepted call under its id without dropping a declined sibling or a sibling result";
    - `ExternalStoreChat.approvals.test.tsx`: each key in `it` and `en`; the composer is `approvalLocked` while a tool approval is pending.
- [ ] **Step 2: Run** `GO 'go test -race ./internal/agui/ ./internal/askuser/ ./internal/channels/telegram/ ./cmd/aura/ ./internal/runner/ -run "TestListApprovalsProjects|TestAcceptedCallShows|TestRepeatedCallIDInAnOlderTurn|TestTranslatorEndsAParkRound|TestToolApproval|TestTypedReply|TestTelegramButton|TestREPLAccept|TestBuildAgentParks|TestResolveOfABusy|TestTelegramBusyAccept"'`, then `DBI ./internal/askuser/ ./internal/agui/ -run "TestDeclinedToolApprovals|TestResolveViaApprovalsAPI|TestAGUIResumeAccept|TestPartialAGUIResume|TestRunCancelDuringAnApprovedCall"`, then the three vitest files. Expected: FAIL.
- [ ] **Step 3: Implement** D2 (the busy mapping) and D20 (the keyed display and declined sets). v2 named D18 here; D18 is the dedupe move and landed in Task 3. `withoutRowsReplayedByRun` drops only the replayed tool-call parts. A typed reply follows the draft rule at `hitl.go:281-285`; `bot_dispatch_hitl.go:36-40` re-renders the card. `ErrThreadBusy` from a resolve is a 409 "conversation busy" in `approvals_api.go` and a busy callback answer in Telegram; neither drives a continuation (D2).
- [ ] **Step 4: Run** Step 2's commands, `npm run lint` and `npm run typecheck` in `web/`, and the common gates. Expected: PASS.
- [ ] **Step 5: Commit** "Show a parked call's card on every surface, then park in production". Paths: `internal/agui`, `internal/askuser`, `internal/db/queries/paused_states.sql`, `internal/db/sqlc`, `internal/channels/telegram/hitl.go`, `internal/channels/telegram/hitl_tool_approval_test.go`, `internal/runner/runner.go`, `internal/runner/runner_build_agent_park_test.go`, `cmd/aura/serve_agui.go`, `cmd/aura/chat_repl_tool_approval_test.go`, `web/src/approvals/useApprovals.ts`, `web/src/chat`, `scripts/deadcode_production_allowlist.txt` (N4: remove `toolapprovals.Store.OpenCalls` if Task 4 listed it, and any entry the gate now reports stale).

### Task 8: Delete the gateway challenge and retire its pauses

**Files:**
- Modify:
  - `internal/gateway/approve.go`: the challenge half of `:104-188` becomes the D7.7.5 deny; delete `:190-235`, `:264-283` and the describer context at `:55-77`. `ResolvedApproval` stays, for the session grants;
  - `approvals.go`: the pending and approved maps and their methods. `sessions` stays;
  - `gateway.go:187-255`: four methods; `Verdict.ApprovalRequest`, `Approve`;
  - `internal/agent/llm_agent_retry.go:93-106,130-131,171-180`;
  - `internal/runner/runner_persist_pause.go:46-49,75-115`, `runner_deps.go:145,199-202,282`, `runner.go:110-111`;
  - `internal/runner/tool_approval_resume.go`: retire `gateway_approval` (D15, AD-M5);
  - `cmd/aura/serve_adapters.go:411-480`, `chat_boot.go:518-521`;
  - `internal/approvaltext/presentation.go:97-98,112-129`.
- Test, rewrite or delete:
  - in `internal/gateway`: `approve_test.go`, `approve_describe_test.go`, `approve_question_test.go`, `approvals_test.go`, `gateway_integration_test.go`, `decide_test.go:115,157,183,221,264,283,300`, `decide_memory_test.go:51,66`, `decide_mail_watch_test.go:106`, `gateway_round_ordinal_integration_test.go:105-106`, `injection_suite_test.go:156`, `grants_test.go`, `policy_test.go`, `scope_test.go`, `reservation_cross_identity_test.go`;
  - `internal/agent/llm_agent_retry_gateway_test.go`, `internal/agent/llm_agent_describe_test.go:61,115` (CR-H4);
  - `internal/runner/runner_persist_test.go` (the stamp cases);
  - delete `cmd/aura/gateway_resume_hook_test.go` and `gateway_approval_roundtrip_test.go`; edit `cmd/aura/skills_cli_surface_test.go:40-42` (CR-H4);
  - `internal/approvaltext/presentation_test.go`;
  - `internal/channels/telegram/hitl_test.go:171-190`: delete it and move its intent to `hitl_tool_approval_test.go` (CR-H5);
  - `internal/agui/approvals_api_test.go:89-110` (db_integration, CR-H5).

- [ ] **Step 1: Write the failing tests.**
  - `TestGatedCallThatWasNotParkedIsRefused`: Deny `approval_not_parked`.
  - `TestRetiredGatewayChallengeResolvesExpired`.
  - `TestExecToolNeverReturnsAnApprovalRequest`.
- [ ] **Step 2: Run** `GO 'go test ./internal/gateway/ ./internal/runner/ ./internal/agent/ -run "TestGatedCallThatWasNotParked|TestRetiredGatewayChallenge|TestExecToolNeverReturns"'`. Expected: FAIL.
- [ ] **Step 3: Implement** the deletions. Keep the headers true. Delete the allowlist entry `github.com/chetto1983/aura/internal/gateway.Gateway.RecordResolvedApproval` (`scripts/deadcode_production_allowlist.txt:20`) with the method (`gateway.go:187`), N4.
- [ ] **Step 4: Run** the common gates over `./internal/gateway/ ./internal/agent/... ./internal/runner/ ./internal/approvaltext/ ./internal/channels/telegram/ ./internal/agui/ ./cmd/aura/`, then `DBI ./internal/gateway/ ./internal/agui/ -run .`. Then `git grep -nE 'gateway_approval_required|stampGatewayChallenge|ApprovalChallengeDetails|newGatewayResumeHook|func \(g \*Gateway\) ApproveChallenge|gatewayApprovalContext' -- '*.go'` prints nothing (CR-H1); `TestExecToolNeverReturnsAnApprovalRequest` must not spell `gateway_approval_required`. `make deadcode` reports no stale entry. Expected: PASS.
- [ ] **Step 5: Commit** "Delete the gateway's model-relayed approval challenge". Paths: `scripts/deadcode_production_allowlist.txt`, `internal/gateway`, `internal/agent/llm_agent_retry.go`, `internal/agent/llm_agent_retry_gateway_test.go`, `internal/agent/llm_agent_describe_test.go`, `internal/runner`, `internal/approvaltext`, `internal/channels/telegram/hitl_test.go`, `internal/channels/telegram/hitl_tool_approval_test.go`, `internal/agui/approvals_api_test.go`, `cmd/aura/serve_adapters.go`, `cmd/aura/chat_boot.go`, `cmd/aura/skills_cli_surface_test.go`, `cmd/aura/gateway_resume_hook_test.go`, `cmd/aura/gateway_approval_roundtrip_test.go`.

### Task 9: The Phase 1 doctrine

**Files:** Modify `internal/agent/prompt.go:130,136`. Test `internal/agent/prompt_test.go`.

The copy (AM-8, XS-G5):
- **`<safety>`.** `- Call a tool directly; never ask for approval before calling it, unless a tool's result tells you to relay its approval through ask_user. When a call needs the operator, Aura stops it, asks them on its own card, and gives you the result or their refusal. Do not repeat a call Aura already ran, nor one the operator declined unless they ask again. When in doubt, snapshot or back up before acting.`
- **`<operator>`, first sentence.** `- Use ask_user only for decisions or information no tool can give you — not for things you can infer or look up, and never to ask approval for a tool call on your own.`
- **Deleted.** "Approvals for installs and risky actions are the operator's alone."

- [ ] **Step 1: Write** `TestPrompt_HostOwnsApprovals`. The prompt contains:
  - `never ask for approval before calling it`;
  - `unless a tool's result tells you to relay its approval`;
  - `unless they ask again`;
  - `snapshot or back up`.

  It does not contain `require operator approval first via ask_user`.
- [ ] **Step 2: Run** `GO 'go test ./internal/agent/ -run TestPrompt_'`. Expected: FAIL.
- [ ] **Step 3: Implement** the copy.
- [ ] **Step 4: Run** `GO 'go test -race ./internal/agent/ ./internal/agent/prompt/'`. Expected: PASS.
- [ ] **Step 5: Commit** "Tell the model the host asks for approvals". Paths: `internal/agent/prompt.go`, `internal/agent/prompt_test.go`.

### Task 10: Inventory of ungated destructive actions (no gating added)

**Files:** Modify the spec, adding the section `## Ungated destructive actions (inventory, <date>)`.

- [ ] **Step 1: Record** each verified row as `action | profile | why no gate fires | file:line`. Start from:
  - `write_file` overwrite;
  - `patch`;
  - `shell_exec` commands no default pattern matches (`shell_exec_env.go:90-101`): `find -delete`, `truncate`, `mv` over a file, `git reset --hard`, `git clean -fd`, a bare `dd` (an `of=/dev/x` is matched, `:97`), and a `>` redirect over a file;
  - `shell_kill`;
  - `plugin_pack upgrade` (`pack.go:45`);
  - `board update` and `board move`;
  - `task pause` and `task run_now` on a task the chat did not create;
  - `skill_manage archive`;
  - MCP tools annotated non-destructive;
  - under `dev` and `local_trusted`, every gateway-Destructive tool (`bridge_risk.go:57-146`, `classify.go:98-150`).

  Add what `git grep -n "Mutating: *true" internal/agent/tools` finds.
- [ ] **Step 2: Check** every cited line with `git grep -n`.
- [ ] **Step 3: Commit** "List the destructive actions no approval gate covers". Path: the spec.

### Task 11: Phase 1 close: full matrix, mutation, then the lab VM

**Files:**
- Create: `docs/superpowers/verification/<YYYY-MM-DD>-unified-approval-phase1-gateway.md`
- Modify: `prd.md` (the amendment), `CHANGELOG.md` (the revert note), `scripts/critical_mutation_gate.py` (and its test, if it pins the map)

- [ ] **Step 1: Mutation targets.** Add to `critical_mutation_gate.py`: `tool_approvals_store` → `internal/toolapprovals/store.go`, `gateway_review` → `internal/gateway/review.go`, `parked_turn_run` → `internal/runner/tool_approval_run.go`, `parked_turn_open` → `internal/runner/tool_approval_open.go` (D24). The CI job measures them, ≥70% killed.
- [ ] **Step 2: Run** `GO 'make quality-full'`, `bash scripts/coverage_docker.sh` (aggregate ≥85%, `internal/toolapprovals` ≥85%) and `npm run test` in `web/`.
- [ ] **Step 3: Before pushing**, `git fetch origin && git ls-tree --name-only origin/master internal/db/migrations/ | tail -1`. If the slot `NNNN` is taken, rename the migration, its integration test and the head pin, then re-run Task 1's DBI command. Push, and wait for every job on the full SHA, including mutation and `Publish Aura edge image`.
- [ ] **Step 4: Live, the controller's run, after the operator's "Aggiorna subito".** Three cases under `single_user_hardened`:
  1. **A Destructive gateway call**, such as deleting a disposable `[aura-misura]` board item, in a turn that also has a read sibling. Measure:
     - one card with the scope options, and no `ask_user` row;
     - the sibling's result is present before the card;
     - after "once", one execution;
     - model calls and model seconds against the 2026-11-27 baseline of 7 calls and about 80 s, counted as in `docs/superpowers/verification/2026-11-27-task-cancel-approval-and-microsoft-sending-address.md`;
     - one ledger start and one end with `operator_id`.
  2. **A decline.** Its text, and nothing run.
  3. **A destructive `shell_exec` under the Phase 1 prompt (AM-8).** Does the model call it directly, does the relayed challenge still complete, and with how many calls?

  Then check Telegram's Italian card with the scope keyboard.
- [ ] **Step 5: The revert note (AD-M7).** In the verification file, the PRD amendment and `CHANGELOG.md`'s release entry, write: "A release that reverts the unified approval must first expire every open `tool_approval` pause: a Go data upgrade (the `aura.data_upgrades` mechanism of migration 0159, run by `aura db migrate`) moves each `pending`/`accepted` record to `expired` and appends the legacy text (D12) on its call id, before the reverted binary serves." Then state the guard that holds when a revert forgets it: the D1 trigger stays (no down migration runs on upgrade), so the reverted binary's resolve fails loudly instead of answering a parked call as if it ran. Measure it on the disposable stack: `DBI ./internal/toolapprovals/ -run TestTriggerRefusesResumingAToolApprovalPauseWithAPendingRecord`, and cite the run.
- [ ] **Step 6: Write** the verification file:
  - the measures and the verdict;
  - the E2E score against the >9.8 bar (AD-L9);
  - "Not shown": Phase 2 paths, D16, an actual revert release (only its guard is measured, Step 5).

  Write the PRD amendment, then commit, with explicit paths (the verification file, `prd.md`, `CHANGELOG.md`, `scripts/critical_mutation_gate.py`), and push.

---

## Phase 2: the tool paths

Each task deletes what its change makes dead and fixes every test that references it, so the task passes `make deadcode` and the tagged tiers on its own (CR-H6, CR-H7).

### Task 12: `CallReviewer`, and `shell_exec` as its first source

**Files:**
- Create: `internal/agent/tools/call_review.go`, `internal/agent/tools/shell_review.go`
- Modify:
  - `internal/agent/llm_agent_approval.go`: the tool source runs first; the tool card borrows the gateway's options and subject (spec decision 2); a `Review.Denied` call is not parked (D14, L4);
  - `internal/gateway/review.go`: `Review.Denied` (D7);
  - `approved_call.go`: `RanNote` and `Binding` (both first read here; v2 put `Binding` in Task 14);
  - `internal/agent/llm_agent_tool.go:186-222`: the `RanNote` prefix; `Refused` for `*tools.OperatorRequiredError`;
  - `shell_exec.go:32,174-245`: `Approvals` gone; `Execute` calls `RequireApproval` after `Route`, where `requireShellApproval` was, then checks a `tracked-cwd:` binding against the tracked cwd (D23, N3);
  - `cmd/aura/main.go:195,239`, `runtime_tool_handles.go:15`, `serve_adapters.go:365-371`, `chat_boot.go:518`: the shell hook leaves the chain (CR-M6);
  - `internal/approvaltext/presentation.go:99-100,131-153`;
  - `internal/runner/tool_approval_resume.go`: retire `shell_exec_approval`.
- Move: `ShellApprovalDigest` and `shellApprovalQuestion` from `shell_approval.go` to `shell_review.go` (CR-M4).
- Delete: `internal/agent/tools/shell_approval.go`, `shell_exec_approval_test.go`, `shell_approval_lifecycle_test.go`, `cmd/aura/shell_resume_hook_test.go`.
- Edit the tests that reference the shell store (CR-H6):
  - `cmd/aura/plugin_resume_hook_test.go:56-62`;
  - `cmd/aura/task_cancel_resume_hook_test.go:72`;
  - `cmd/aura/skills_cli_surface_test.go:40-41`;
  - `internal/agent/tools/evict_test.go:64-65`;
  - `cmd/aura/document_agent_live_test.go:335` (tag `document_live_e2e`).
- Test: `internal/agent/tools/shell_review_test.go`, `shell_exec_destructive_default_test.go` (rewrite), `internal/agent/llm_agent_approval_test.go`

**Interfaces:**
- Produces: D6, and
  ```go
  type OperatorRequiredError struct{ Tool, What string } // "<tool> <what> needs the operator's approval and the operator is not in this run: it was not run. Do not retry it; tell the operator what you would do."
  func RequireApproval(ctx context.Context, toolName string, raw json.RawMessage, what string) error
  func (s *ShellExec) ReviewCall(ctx context.Context, raw json.RawMessage) (CallReview, error)
  ```
- **The review's order.** CRLF normalization, then the `skillsInstallSource` bypass, then `destructiveShellMatch`, then `Route`, then `boxWorkdir` (`shell_exec_sandbox.go:192`, reading the tracked cwd from the call context).
  - `Execute` routes before matching (`shell_exec.go:192-213`, CR-M3). The review matches first so that a harmless command never routes the box at park time. The outcome is the same: a box that cannot route refuses in both.
- **What the review returns.**
  - `ExecuteArgs`: the command and `cwd` = the box workdir.
  - The `ShellKey` presentation.
  - `RanNote`: `in <cwd>`.
  - `Binding`: `tracked-cwd:<cwd>` when the cwd came from tracking, "" when the model passed one (D23).
- **A pinned cwd that is gone.** At execution it is refused by `rejectMissingBoxDir` (AD-L7), and the card text states the cwd.
- **A pinned cwd that is no longer the tracked one** (N3). At execution, before running, `Execute` refuses with the D23 text, and nothing runs.

- [ ] **Step 1: Write the failing tests.**
  - `TestShellReviewBindsTheTrackedBoxCwd` (AM-4): after a `cd` in the same conversation, the review goes through `pauseCalls`, and `ExecuteArgs.cwd` is the tracked one.
  - `TestShellReviewSkipsInstallsAndHarmlessCommands`.
  - `TestUnauthorizedDestructiveCommandIsRefused`, on every profile.
  - `TestAuthorizedDestructiveCommandRunsAndSaysWhere`: the preview starts with `[Aura ran it in /workspace/…]`.
  - `TestHeadlessShellRefusalIsARefusal`: the ledger carries `gateway_verdict=deny`.
  - `TestToolSourceParksUnderServerProduction` (D14, spec at `6d5f72b4c`): under `server_production`, with a person present, a destructive command parks one card; the gateway source parks nothing for the same call.
  - `TestToolSourceDoesNotParkWhatTheGatewayRefuses` (L4): under `server_production` with an `ask` policy on `shell_exec`, a destructive command shows no card; dispatch returns the gateway's `no_approver` refusal, and no record is written.
  - `TestAskPolicyOnShellGivesOneCardWithOptions`: closes D16 for the shell.
  - `TestShellReviewAfterACdSiblingDoesNotRunInTheOldCwd` (N3): `[shell_exec "cd proj", shell_exec "rm -rf build"]`. The `cd` runs in the park round, and the card shows the pre-`cd` cwd. On accept, `rm` is not executed: the result is the D23 refusal naming both directories, and the fake box receives no exec for the `rm`.
  - `TestCdAfterAParkedShellCallRefusesTheAccept` (N3, the converse): `[rm -rf build (parked), cd other]` is refused the same way.
  - `TestExplicitCwdIsNotRebound`: a model-passed `cwd` has no binding and runs there after a sibling `cd`.
  - `TestEmptyTrackedCwdAtExecutionRefusesABoundCall` (N3, restart): the binding names `/workspace/proj`, the tracked map is empty (a restart); the accept is refused with the "was reset since" text and nothing runs.
  - `TestToolSourcedAcceptRecordsTheOperator` (M4, `internal/runner/tool_approval_tool_source_integration_test.go`, db_integration, strict gateway): an accepted destructive `shell_exec` writes one ledger start with the resolving identity as `operator_id` and the record's `approval_scope`, and one end.
  - `TestRetiredShellChallengeResolvesExpired`, in `internal/runner/tool_approval_resume_test.go`.
- [ ] **Step 2: Run** `GO 'go test -race ./internal/agent/... ./internal/gateway/ ./internal/runner/ -run "TestShellReview|TestUnauthorizedDestructive|TestAuthorizedDestructive|TestHeadlessShellRefusal|TestToolSourceParksUnder|TestToolSourceDoesNotPark|TestAskPolicyOnShell|TestCdAfterAParked|TestExplicitCwd|TestEmptyTrackedCwd|TestRetiredShellChallenge"'` and `DBI ./internal/runner/ -run TestToolSourcedAcceptRecords`. Expected: FAIL.
- [ ] **Step 3: Implement**, and delete what the Files list names. In `scripts/deadcode_production_allowlist.txt` (N4): delete `tools.ShellApprovals.PendingChallenge` (`:19`), and add `tools.ApprovalStore.PendingChallenge` exactly as the gate prints it, reason `wired out in Task 14 (unified approval): its last production caller was shell_approval.go:83`.
- [ ] **Step 4: Run** the common gates over `./internal/agent/... ./internal/gateway/ ./internal/runner/ ./internal/approvaltext/ ./cmd/aura/`, and `DBI ./internal/runner/ -run "TestAcceptRunsOnce|TestToolSourcedAcceptRecords"`. Expected: PASS, and the production deadcode gate prints no unlisted finding and no stale entry.
- [ ] **Step 5: Commit** "Park a destructive shell command and run the reviewed one". Paths: `internal/agent`, `internal/gateway/review.go`, `internal/gateway/review_test.go`, `internal/approvaltext`, `internal/runner`, `scripts/deadcode_production_allowlist.txt`, `cmd/aura/main.go`, `cmd/aura/runtime_tool_handles.go`, `cmd/aura/serve_adapters.go`, `cmd/aura/chat_boot.go`, `cmd/aura/shell_resume_hook_test.go`, `cmd/aura/plugin_resume_hook_test.go`, `cmd/aura/task_cancel_resume_hook_test.go`, `cmd/aura/skills_cli_surface_test.go`, `cmd/aura/document_agent_live_test.go`.

### Task 13: `plugin_pack` install and remove

**Files:**
- Create: `internal/agent/tools/pack_review.go`
- Modify:
  - `pack_install.go:28-134`: the challenge goes; `install` and `remove` call `RequireApproval`; `operator()` and `MayAdminister` stay at execution;
  - `pack.go:37,44,65-72,182-185`: `Approvals` and `Evict` go (CR-M5); the description says Aura sets `commit` and `digest`;
  - `cmd/aura/serve_plugins.go:29-47`, `serve_adapters.go:293`, `chat_boot.go:518`;
  - `internal/approvaltext/presentation_plugin.go:77-99`: `LineCard` exported;
  - `internal/runner/tool_approval_resume.go`: retire `plugin_pack_approval`;
  - `deferred_manifest.json`, by hand.
- Delete: `cmd/aura/plugin_resume_hook_test.go`.
- Edit: `cmd/aura/task_cancel_resume_hook_test.go:78`, `skills_cli_surface_test.go`, `internal/agent/tools/evict_test.go:90`, `pack_test.go:163`, `internal/approvaltext/presentation_plugin_test.go:17,175` (CR-M15).
- Test: `internal/agent/tools/pack_review_test.go`, `pack_install_test.go`, `pack_upgrade_test.go`

**Interfaces:**
- Produces: `func (t *PackTool) ReviewCall(ctx context.Context, raw json.RawMessage) (CallReview, error)`.
  - **Install.** `Backend.Review`; an installed plugin → not needed. `ExecuteArgs` = `{action, ref, commit: review.Commit, digest: review.Digest}`. `RanNote` = `at commit <c>`.
  - **Remove.** `Backend.Get`. `ExecuteArgs` = `{action, name, commit: rec.Commit, delete_data}`.
  - **Presentation.** `LineCard`.

- [ ] **Step 1: Write the failing tests.**
  - `TestPluginReviewPinsTheReviewedCommitAndDigest`.
  - `TestPluginRemoveReviewBindsTheInstalledCommit`.
  - `TestUnauthorizedInstallIsRefused`.
  - `TestNonOperatorAcceptStillRefused`.
  - `TestRetiredPluginChallengeResolvesExpired` (in `tool_approval_resume_test.go`).
- [ ] **Step 2: Run** `GO 'go test -race ./internal/agent/tools/ ./internal/runner/ -run "TestPluginReview|TestPluginRemoveReview|TestUnauthorizedInstall|TestNonOperatorAccept|TestRetiredPluginChallenge"'`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the common gates over `./internal/agent/tools/ ./internal/approvaltext/ ./internal/runner/ ./cmd/aura/`. `search_gate_test.go` must pass on the edited manifest. Expected: PASS.
- [ ] **Step 5: Commit** "Park a plugin install or removal and run the reviewed pin". Paths: `internal/agent/tools`, `internal/approvaltext`, `internal/runner`, `cmd/aura/serve_plugins.go`, `cmd/aura/serve_adapters.go`, `cmd/aura/chat_boot.go`, `cmd/aura/plugin_resume_hook_test.go`, `cmd/aura/task_cancel_resume_hook_test.go`, `cmd/aura/skills_cli_surface_test.go`.

### Task 14: Foreign `task cancel`, and the shared machinery it leaves dead

**Files:**
- Modify:
  - `internal/agent/tools/task_cancel.go:11-52,89-92`: the review; `actionCancel` checks `RequireApproval` and the `Binding` (the field landed in Task 12); `Evict` goes;
  - `task.go:49-51,156,178`;
  - `cmd/aura/serve_adapters.go:45,345-409`: `chainResumeHooks` and `newChallengeResumeHook` go;
  - `serve_adapters_cancel.go:94-100`, `chat_boot.go:518`: the chain is gone;
  - `internal/runner/runner_deps.go:140,195-197`, `runner.go:109` (the `resumeHook ResumeHook` field), `runner_resume.go:120-122,172-176,206-211`, `approval_expiry.go:36-38`: `ResumeHook` and `applyResumeHook` go;
  - `internal/runner/runner_errorpaths_test.go:283-306` (M6): delete `TestSubmitAnswer_ResumeHookError`, the `seedPendingWithContext` helper if it loses its last caller, and the `applyResumeHook` section comment at `:283`;
  - `scripts/deadcode_production_allowlist.txt` (N4): delete `tools.ApprovalStore.PendingChallenge`, added in Task 12;
  - the stale `ResumeHook` comments (CR-L41): `agui/approvals_api.go:266`, `telegram/hitl.go:203`, `telegram/bot_dispatch.go:283`, `web/src/approvals/useApprovals.ts:83`, `useThreadApprovals.ts:12,388`;
  - `internal/approvaltext/presentation.go:95-110`: `derive` keeps `scheduled_task_approval` only;
  - `internal/runner/tool_approval_resume.go`: retire `task_cancel_approval`;
  - `deferred_manifest.json`.
- Move: `approvalDigest` from `approval_store.go` to `call_review.go` (CR-M4).
- Delete: `internal/agent/tools/approval_store.go`, `approval_store_test.go`, `cmd/aura/task_cancel_resume_hook_test.go`, `internal/runner/runner_more_test.go:117` `TestSubmitAnswer_RunsResumeHookForContext` (CR-M14).
- Edit: `internal/approvaltext/presentation_plugin_test.go:209`.
- Test: `internal/agent/tools/task_cancel_test.go` (rewrite)

**Interfaces:**
- Produces: `func (t *TaskTool) ReviewCall(ctx context.Context, raw json.RawMessage) (CallReview, error)`.
  - **When it is needed.** Only for a `cancel` whose task's origin is not `callerConversationID(ctx)`, read from the call context (AM-4).
  - `Binding` = `taskCancelDigest(task)`.
  - The presentation is `LineCard`.
  - `ScheduledTask` keeps `HostReview`, which the agent consults first.
- The `task.go:178` clause: `call action=cancel directly and never ask the operator beforehand; for a task this conversation did not schedule, Aura asks the operator itself.`

- [ ] **Step 1: Write the failing tests.**
  - `TestForeignCancelReviewNeedsTheOperator`.
  - `TestOwnCancelNeedsNoReview` (AM-4): through `pauseCalls`, the conversation's own task does not park.
  - `TestTaskChangedBetweenCardAndAcceptIsRefused`.
  - `TestUnauthorizedForeignCancelIsRefused`.
  - `TestRetiredCancelChallengeResolvesExpired` (in `tool_approval_resume_test.go`).
- [ ] **Step 2: Run** `GO 'go test -race ./internal/agent/... ./internal/runner/ -run "TestForeignCancelReview|TestOwnCancelNeeds|TestTaskChangedBetween|TestUnauthorizedForeignCancel|TestRetiredCancelChallenge"'`. Expected: FAIL.
- [ ] **Step 3: Implement** the change and the deletions.
- [ ] **Step 4: Run** the common gates over `./...`. `git grep -nE '\bApprovalStore\[|newChallengeResumeHook|chainResumeHooks|\bapprovalPrompt\b|_approval_required"|applyResumeHook' -- '*.go'` prints nothing (CR-H2, CR-H7). Expected: PASS.
- [ ] **Step 5: Commit** "Park a foreign task cancel and delete the in-tool approval machinery". Paths: `internal/agent/tools`, `internal/approvaltext`, `internal/runner` (it includes `runner.go` and `runner_errorpaths_test.go`), `scripts/deadcode_production_allowlist.txt`, `internal/agui/approvals_api.go`, `internal/channels/telegram/hitl.go`, `internal/channels/telegram/bot_dispatch.go`, `web/src/approvals/useApprovals.ts`, `web/src/approvals/useThreadApprovals.ts`, `cmd/aura/serve_adapters.go`, `cmd/aura/serve_adapters_cancel.go`, `cmd/aura/chat_boot.go`, `cmd/aura/task_cancel_resume_hook_test.go`.

### Task 15: The final doctrine, `ask_user` and the relay texts

**Files:**
- Modify:
  - `internal/agent/prompt.go`: remove `, unless a tool's result tells you to relay its approval through ask_user` (AM-8);
  - `internal/agent/tools/ask_user.go:97,110`: delete the `resume_context` property and field when `git grep -n 'resume_context' -- 'internal/**/*.go' ':!*_test.go'` shows no model-written reader. Otherwise its description becomes `Omit it.`, with the reason in the commit body;
  - the stale `skill_approval` comments (`llm_agent_pause.go:12-20`, `runner/interfaces.go:75-85`);
  - `deferred_manifest.json`.
- Test: `internal/agent/tools/relay_texts_test.go`, `internal/agent/prompt_test.go`, `internal/agent/tools/ask_user_test.go`

- [ ] **Step 1: Write the failing tests.**
  - `TestNoToolTellsTheModelToRelayAnApproval`: every registry description and parameter description, and every entry of `deferred_manifest.json`, contains none of `Call ask_user with kind=`, `_approval_required`, `again with exactly the arguments`.
  - `TestPrompt_HostOwnsApprovals`: it asserts the exception sentence is gone.
- [ ] **Step 2: Run** `GO 'go test ./internal/agent/... -run "TestNoToolTellsTheModel|TestPrompt_HostOwns"'`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the common gates over `./...`, and `git grep -nE 'Call ask_user with kind=' -- '*.go' ':!*_test.go'`, which prints nothing. Expected: PASS.
- [ ] **Step 5: Commit** "Retire the last relay texts and finish the approval doctrine". Paths: `internal/agent/prompt.go`, `internal/agent/prompt_test.go`, `internal/agent/llm_agent_pause.go`, `internal/agent/tools/ask_user.go`, `internal/agent/tools/ask_user_test.go`, `internal/agent/tools/relay_texts_test.go`, `internal/agent/tools/testdata/deferred_manifest.json`, `internal/runner/interfaces.go`. Never `internal` as a whole (CR-M16).

### Task 16: Phase 2 close: full matrix, then the lab VM

**Files:** Create `docs/superpowers/verification/<YYYY-MM-DD>-unified-approval-phase2-tool-paths.md`. Modify `prd.md`, `scripts/critical_mutation_gate.py` (and `scripts/critical_mutation_gate_test.py` if it pins the map).

- [ ] **Step 1: Run** `GO 'make quality-full'`, `bash scripts/coverage_docker.sh` and the web tests. Add `internal/agent/tools/call_review.go` and `shell_review.go` to the mutation map. Push, and wait for green on the full SHA.
- [ ] **Step 2: Live, the controller's run.**
  1. **The 2026-11-27 foreign-cancel seed** (a CLI reminder `[aura-misura]`, then "Cancella il promemoria …"): one card, no `ask_user` from the model, **at most 4 model calls**, and model seconds against 7 calls and about 80 s.
  2. **A destructive `shell_exec`** (`rm -rf /workspace/aura-misura-tmp` after creating it): one card showing the cwd, one run, and the result has the "ran it in" note. If the model emits a `cd` sibling before it, record that the accept is refused with the D23 text and how many extra calls the re-emitted card cost (N3).
  3. **`plugin_pack remove`** of a disposable plugin, if one is installed.
  4. **An `ask` policy on `shell_exec`**: one card with options.

  For each case, record whether the turn had siblings and that they ran in the park turn.
- [ ] **Step 3: Write** the verification file: counts, seconds, verdict, the E2E score and "Not shown". Write the PRD amendment, then commit with explicit paths and push.

---

## Phase 3: `task schedule` approval

### Task 17: The ruling on folding `task schedule` approval

**Files:** Modify the spec, adding the section `## Phase 3 ruling (<date>)`.

- [ ] **Step 1: Measure the surface.**
  - **What a fold removes:** `HostReviewer`, `deriveScheduled` (`presentation.go:155-229`) and `scheduledTaskDecision`'s question check (`scheduled_task_resume.go:51-83`).
  - **What it cannot remove:**
    - the row written at park time (`task_approval.go:104-110`, made idempotent by migration 0155);
    - the accept that flips the row inside the transaction;
    - the headless `pending_approval` route and its sweep;
    - `MintApprovalPause` and `ExpireApprovalPause`.
- [ ] **Step 2: Rule "fold" or "kept"** on two criteria: fewer mechanisms, and no change to reminders or lapses. Include the line counts. A "fold" ruling names a new spec and is not executed from this plan.
- [ ] **Step 3: Commit** "Rule on folding task schedule approval into the parked-call mechanism". Path: the spec.
