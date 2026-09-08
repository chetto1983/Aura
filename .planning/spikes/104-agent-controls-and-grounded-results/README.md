# Industrial multi-agent controls and grounded results

Status: in progress. Goal: **MAKE AURA MULTIAGENT INDUSTRIAL FULLY VALIDATE E2E**.
This continues spike 103; its passing runtime tests do not close the full goal.

## Evidence and references

- Spike 103 records real execution, pause/resume, repeated delegation, changed context,
  failure isolation, parent steering, nested invocation identity, pane reload and SIGKILL
  recovery. Commit `8b1c502ee` passed all 25 CI jobs and the separate workflows.
- Live MCP baseline on 2026-09-08 in conversation
  `01a07e05-4f05-7ba0-ab54-0b133ab83398`: both `w1-3e1331da` and `w2-4c9e8aae`
  were actually running. The selected pane had zero inputs and only its close button.
  No direct child steering or stop was available.
- N01 in spike 103 returned a premature fabricated root summary before the authentic
  reports arrived. Correct arithmetic and later reports do not make that claim true.
- LibreChat clone `f9f1b2fb`, `packages/api/src/agents/control.ts`,
  `background.ts`, `subagentTaskRouting.ts`, and
  `client/src/components/Chat/Subagents/SubagentActivity.tsx`: owner/parent checks,
  invocation fingerprints, separate accepted/applied/rejected receipts, terminal-state
  refusals and explicit owner-unavailable outcomes. Its descriptions explicitly say
  live controls do not survive executor-process restart; this is not a durability claim
  to copy silently into Aura's durable job queue.
- assistant-ui's documented `ReadonlyThreadProvider` remains the transcript runtime.
  Reuse Aura's existing run steering/cancel client and native status stream rather than
  create a second chat runtime or parallel wire protocol.

## Existing substrate and implementation direction

`agui.RunRegistry` already owns run identity, owner scoping, cancellation and terminal
cleanup. Its discovery key currently assumes one parent run per conversation; children
must get their own keys without replacing parent discovery. Both `/agent/runs/{runID}/steer`
and `/cancel` already use the native HTTP operation registry and idempotency keys.

`steer.PostgresStore` is the sole durable inbox. Add explicit worker/run targeting while
preserving the existing parent Push/Drain contract. A worker correction must never be
drained by its parent, sibling or later incarnation. Receipts must distinguish acceptance,
actual application, rejection and unavailable owners. Cancellation must be a terminal
`canceled` outcome, not a generic worker failure eligible for retry. Preserve the queue's
lease fence and existing report-delivery checkpoint.

The status stream already enumerates owned child transcripts. Extend its host-authored
metadata so active nested children can be discovered, identified and controlled while
their parent's synchronous tool call is still pending. Preserve parent/child attribution.

The completion guard must use observed delegation state and reports. A stronger prompt
alone is insufficient. It must still allow the parent to acknowledge a handoff and do
independent work; it must not solve premature summaries by blocking all parent progress.

Primary targets: `internal/agui/runregistry*`, `runsession*`, run control handlers and
worker status streams; `internal/steer`; `internal/swarm`; `internal/agent` completion
and worker context; `cmd/aura/serve_delegation*` and composition; existing SQL queries
and the next migration slot measured at landing; `web/src/chat/workers` and EN/IT copy.
Do not edit the concurrent phase 1 sandbox/provisioning work.

## Required evidence before closing the goal

| Requirement | Required proof | Current state |
| --- | --- | --- |
| All spike 103 regressions remain fixed | Native tests, live transcript/tool audit and CI | Prior checkpoint passed; revalidate changed surfaces |
| Steer a live child | MCP correction during a real tool; final output follows it | Missing |
| Sibling and parent isolation | Their inputs, executions and results stay unchanged | Missing for child controls |
| FIFO and exact logical retry | Multiple corrections; one idempotency key never applies twice | Missing for child controls |
| Control receipts | Accepted and applied are distinct, visible after reload | Missing |
| Stop a live child | Prompt cancellation, terminal canceled report, no retry | Missing |
| Stop lifecycle races | Completion, queued/paused work and accepted controls resolve honestly | Missing |
| Nested control and visibility | Discover/control a live grandchild; preserve siblings and ancestry | Missing |
| Ownership and input bounds | Real scoped API denies foreign/malformed/stale targets and oversized input | Missing for child controls |
| Restart and control settlement | No silent application to a new incarnation; completed/canceled work is not retried as failure | Missing for controls |
| Grounded final answers | Delayed unpredictable outputs match actual reports; no fabricated IDs or premature success | Failed baseline |
| Failure and hostile report data | Honest partial results; report text cannot become operator authority | Prior trust framing passed; broaden final-answer proof |
| Desktop/mobile and EN/IT | MCP controls, keyboard, reload and readable status on both layouts | Missing for new controls |
| Quality gates | vet/build/test/race, disposable full coverage >=85%, mutation >=70%, all CI green | Required after implementation |
| Delivery | Frequent atomic commits, push, healthy updated container and memory MCP evidence | Ongoing |

Every row needs authoritative evidence. A passing subset or a model's self-reported
success must not be used to mark the goal complete.

## First live control proof

Conversation `01a07eb5-7c05-7c5a-be86-8a29a6bc212f`, image
`d720496a0-controls104`: `w1-5336b66b3d9da4053e3a28227ae3c5a7` ran a real90-second
Python command. The MCP sent two corrections during that command; both returned202,
and replaying the second request with its same key returned202 without a third row.
SQL showed exactly two ordered corrections, both drained at01:52:38.599UTC, and the
final durable report was `{"verificato":144,"nota":"second104D"}`. The sibling
`w2-9258eefdaa83fa444734c9bd56308bc7` ran once and returned196 unchanged. The root
only acknowledged the handoff before genuine reports arrived.

The pane showed both applied receipts but omitted the final JSON until reload,
although a direct browser fetch proved TEXT_MESSAGE_CONTENT carried it. The pane's
effect reopened the transcript when terminal status removed run_id, regressing the
native message-part tree through a partial replay. Fix subscription lifetime before
counting live visibility as passed. The shorter104C probe finished before the attempted
steer observation and is not counted as a successful live correction.

## Additional identity measurement

Nested104N, conversation `01a07ed6-a753-78ed-82f6-0c226a2924ff`, depth3 on image
`c499bf6c7`: both grandchildren were discovered before the coordinator's spawn call
returned. MCP steered `w1-8a482f3772687e55f67fc760ed1bdedc` to JSON and stopped
`w2-70ca1cbacf412a3c60071d9d470725f4`; both routes returned202. The coordinator's
authentic report contained `{"value":1024,"note":"nested104N"}` and a canceled
second child. Its independent sibling finished1331. Parent navigation worked.
At390x844 the drawer was341px with no horizontal overflow. The JSON rendered in
the mobile pane, but reloading lost the open drawer; this restore defect remains
to fix before the mobile row can pass.

Queue104Q, conversation `01a07ec7-283a-73b0-a204-a9596c412a4b`: AURA_SWARM_MAX_CONCURRENT
was4, but five jobs entered running and the fifth command finished while four80-second
commands remained active. `runtimeTenantIngestionProcessor` created a fresh asynchronous
DelegationClaimLoop on every poll, losing the occupied-slot state. Retain these loops
between polls and connect their capacity to the actual configuration before testing
queued-worker visibility and cancellation.

Stop104E, conversation `01a07ec0-572d-7717-8624-8e2962c62523`: the MCP stopped
`w1-b493a281a46ae203127579c6ee0fef15` while its Python process was sleeping before a
scratch-file write. The route returned202 in1886ms; docker top then showed no matching
process, the job was canceled at attempt1, and the marker file remained absent after
the original90-second deadline. Sibling `w2-0892f02f14e467a52a599d6915b3a09f` finished
once with391. This proves the actual process was stopped, not just the model loop.

Pause104F, conversation `01a07ec3-9f07-71ca-b6cc-7664a31e6522`: canceling the parent
approval card returned200, but `w1-998d9da988b814a07df7fdd027cd6956` was rebuilt at
attempt2 and ended `succeeded` with a cancellation sentence. Its stored answer action
was `cancel`. This fails cancellation semantics despite no arithmetic tool execution.
The observer must preserve that action as terminal control intent.

The bounded hash probe found a collision after 102603 candidates. With identity
`11111111-1111-1111-1111-111111111111`, conversation
`22222222-2222-2222-2222-222222222222`, goal `independent task` and goal index 0,
invocations `agent_tool:probe:62646` and `agent_tool:probe:102602` have different
durable keys but both produce `w1-c4ea2263` under the 32-bit child-ID truncation.
`TestDelegationWorkerIDsSeparateMeasuredShortHashCollision` reproduced the collision
through the native Go functions. The new identifier retains 128 bits; re-enqueue must
return the existing stored child ID when the job predates the change. This prevents
new transcript/control collisions without renaming live or historical workers.
