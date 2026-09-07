---
spike: 103
idea: agent-runtime-correctness
name: agent-runtime-correctness
type: comparison
verdict: validated-scoped-flows
related: [098, 099, 100, 101]
tags: [agents, librechat, runtime, tools, cancellation, resume, delegation]
---

# Multi-agent correctness

Requested by the operator on 2026-09-07: apply the memory validation discipline to
Aura's agents, using the existing `D:/tmp/LibreChat` clone as a reference.
The operator then narrowed the investigation to multi-agent behavior.
The scope also includes modernizing the worker UI so agents can be observed while
working, and using Aura's mounted memory MCP to maintain current project context.

## Reference and baseline

- LibreChat: `f9f1b2fb951a99e1fd01ee7291304a0370ea6132`, v0.8.8-rc2,
  dated 2026-09-02; clean supplied clone. Lockfile pins `@librechat/agents` 3.7.17.
- Aura starting source: `37211f83d`; deployed application stamp
  `fb07b4d35-launch-docs-dirty`. The only intervening `cmd`/`internal` change
  found at baseline is the backup handler's documentation, not agent behavior.
- Deployed Aura image: `sha256:8dd911d935d65f90ccd7cde5daada3aaf03d02329481563c2995bc22c4c5f3d2`.
- The mounted Playwright MCP drives Aura's real cockpit. Existing Authula browser
  state was restored using Playwright's native cookie API; no account was reset.

LibreChat is a source/test reference in this investigation, not a live performance
baseline: its clone has no installed node_modules or running application here.

## Questions and prior criteria

| ID | Scenario | Required evidence |
|---|---|---|
| M01 | Parallel children and synthesis | Actual child tool calls, separate transcripts, overlapping lifetimes, independently correct parent answer |
| M02 | Repeat delegation in a new turn | Identical goals create fresh jobs and actual new tool executions; same-operation retry still deduplicates |
| M03 | Changed shared context | Fresh workers use the changed input instead of replaying the previous result |
| M04 | Partial failure | A failed child does not lose healthy siblings; final report distinguishes outcomes |
| M05 | Worker asks and resumes | Exact user choice reaches the paused child once, with durable continuation |
| M06 | Reload/background delivery | Completion returns to the owning conversation after browser reload |
| P01 | Nested lineage and limits | Distinct child identities, originating conversation ownership, bounded depth and shared budget |
| P02 | Cancellation and recovery | Honest terminal state; pause/retry/reclaim preserve the documented lifecycle boundaries |

Results must distinguish real-model/browser cases, deterministic protocol tests,
source-only comparisons and unexecuted cases. No score may convert a missing case
into a pass. Model answers are checked against external artifacts and durable events.

All test files use an owned namespace. No operator documents are deleted, no test
facts are planted in the operator's memory, and no email/third-party message is sent.
The old `internal/agenteval/Cases` cannot be run wholesale: its document-deletion
case depends on manually deleting the operator's Clienti.xlsx between cases.

## Initial reference observations

- `packages/api/src/agents/stepBudget.ts` reserves a final-answer superstep and
  counts tool rounds rather than individual calls in a parallel batch.
- `packages/api/src/agents/steering/runtime.ts` checks both injection and replay
  capability; durable FIFO application precedes slow media encoding and failed
  suffixes are restored without allowing later instructions to overtake them.
- `packages/api/src/stream/interfaces/IJobStore.ts` separates running, complete,
  error, aborted and requires_action, with immutable generation/checkpoint scope.
- `api/server/controllers/agents/resume.js` binds resume to the correct owner,
  pending action, request fingerprint and checkpoint generation.
- The legacy Assistants `services/Runs/RunManager.js` is not the modern Agents
  implementation and is excluded from the main comparison.

Prior spike results are dated evidence, not current closure. In particular, the
June Claude parity document and the old no-durable-delegation observations predate
the current implementation and cannot justify skipping fresh execution.

## Live baseline, 2026-09-07

Conversation `01a07d03-3f2f-7909-b59f-77b1ed08856f`, driven through the mounted
Playwright MCP, current runtime `ollama / gemma4:31b-cloud`, 60 steps / 300 seconds.

- M01: both children executed `shell_exec` with Python. Independent oracles: sum
  1..100 = 5050; SHA-256 of UTF-8 `AuraMulti103` =
  `b3936453569f99dd8d7fa94509c3e1f40f5244dd808602147e3c694577dd93c9`.
  Child requests `01a07d05-110f-7c4c-b665-b6b79ed1bc2c` and
  `01a07d05-110a-77e8-bb3c-70ddce656d74` have successful ledger end rows;
  `w1-b9b555a1` and `w2-ee2226e3` have separate terminal transcripts.
  Both queue rows were created at 17:57:57Z and completed at 17:58:19Z.
  The parent synthesized both correct results. Completion also triggered a
  subsequent acknowledgement: answer correctness does not prove ideal delivery UX.
- M02 before correction: at 18:03:24Z, a new parent request
  `01a07d09-fd35-799f-9b75-64413bd833c2` called `swarm_spawn` with exactly the same
  goals. The tool returned `queued:2` and the same child IDs, but SQL still counted
  only two jobs with the original creation time and no new child tool executions.
  The model correctly noticed the reused IDs. `ParentRunID` was empty in both jobs.
- M02 after deployment (`dfc53f64b-multiagent103-dirty`): the same exact goals at
  18:29:14Z created `w1-74c64191` and `w2-b9b521a9`. SQL now counts four jobs in
  the conversation and four successful Python `shell_exec` end rows. Both new
  children produced the independent expected values. New child requests:
  `01a07d21-b62c-73a0-8829-03ddc5c89c6f` and `01a07d21-b62a-7bda-a9b6-59a61bf1a350`.
- M05 before correction: conversation `01a07d1b-b562-7c12-82e8-8f00e5579389`.
  Child `w2-56c5d373` successfully executed Python and returned 385 (sum of squares
  1..10). Child `w1-a5ff6a57` asked the operator to choose 7 or 9 and parked.
  The real button click for 9 returned HTTP 403, `approval decision not allowed`;
  the job remained `awaiting_input`. The production pause/park writer omitted
  `allowed_decisions`. The existing DB lifecycle test called `MarkResumed` directly,
  bypassing the actual Runner validation and claim construction.
  Inspection then found the same route also omitted the pending-action fence and
  would append the worker's tool answer into the parent's transcript. The new
  composition test exercises the production writer plus `Runner.SubmitAnswer` and
  resume observer, including duplicate rejection and parent-history isolation.

## UI measurement and reuse decision

Live captures on 2026-09-07: `.git/multiagent-01-before.png` (collapsed spawn with
no worker entry point in the header), `.git/multiagent-02-worker-before.png`
(expanded 64rem table and the existing read-only child pane). Screenshots include
the operator's conversation navigation and remain private; summarized here.

1. Spawn completion hides still-running children behind a closed tool disclosure.
   Registration/status subscriptions begin only when that rich display mounts.
2. The report's fixed minimum width requires horizontal scrolling in a split pane
   and on mobile. Goals are truncated while IDs dominate.
3. The worker renderer omits reasoning parts: a child processing inputs before its
   first tool/answer can show no activity. The real pane did render the completed
   Python call and answer after opening it; it is not absent from Aura.
4. Console captured `Resource updated before mount` while opening the pane; this
   requires post-change recheck. The subsequent `startTime` error originated in
   browser instrumentation (`<anonymous>`), so it is not yet attributed to Aura.

Reference source `LibreChat/client/src/components/Chat/Messages/Content/Parts/SubagentCall.tsx`
renders a compact activity preview and opens a shared child pane.
[assistant-ui multi-agent documentation](https://www.assistant-ui.com/docs/tools/multi-agent)
documents `MessagePartPrimitive.Messages` for nested tool-call messages and
`ReadonlyThreadProvider` for messages outside a tool scope. Aura already uses the
latter, matching the separate durable transcript stream. The installed core source
shows that `PartMessages` itself delegates to `ReadonlyThreadProvider`; no new
runtime or protocol is needed. The docs also disclose reload/nested-resume limits
in the optional AG-UI adapter; adopting that adapter is not this UI fix.

Acceptance: inline worker cards; visible status and goal; direct transcript access;
an agent button that remains reachable after scrolling; live activity and completed
results; refresh and conversation switching preserve ownership; narrow-screen cards
and drawer fit without horizontal overflow. Reuse Aura tokens, native read-only
provider, message/tool renderers, status stream and existing resizable/drawer shell.

## Validation and delivery checkpoint

- Go unit/database coverage: **34,325 / 39,711 statements, 86.4%**, package policy
  passed on the final Go changes, using the disposable coverage database.
- Frontend full suite: **1,988 tests passed**, statement coverage **91.07%**, branch
  coverage **85.23%**. Subsequent localization changes were checked with the affected
  suites and exact EN/IT key parity. Two pre-existing navigation tests were corrected:
  one mocked the retired workspace, the other treated a loading screen as settled.
- Native Go overlay mutation checks: **8/8 viable mutants killed**, covering enqueue
  identity, pause fence, worker-history isolation, trust envelope, report notification,
  resumed replay and source attribution. The first invocation mutant failed compilation
  and was replaced with a compilable mutation; it is not counted as killed.
- M03: in conversation `01a07d27-c7f1-7388-9096-3df80ad4d6fb`, unchanged goals with
  shared context N=6 then N=7 created four distinct jobs. Independent results were
  21/720 then 28/5040. The second pair used `w1-91b3a9d1` / `w2-b167a6ef`.
- M04: conversation `01a07d4f-2a86-7937-afb2-d18090269108`, one child executed the
  requested controlled shell failure (exit 17), the other computed 225. Both reported
  honestly and both durable reports appeared without manually refreshing the chat.
- M05 after correction: conversation `01a07d31-2794-7efd-befd-95af305e8738`, choice 9
  was accepted, `w1-f843d485` resumed (attempt 2) and executed Python to return 99;
  sibling `w2-4130e222` executed once and returned 385. The final answer is visible
  in the worker pane. Its question and resume remain separate from parent tool history.
- M06: controlled reload in the same conversation retained the selected worker and
  open pane (one pane before and after), with the same owner/child identifiers.
- S01 steer: conversation `01a07d6f-5777-7c5f-a147-ea97df54ac25`. The correction was
  sent while `shell_exec` visibly ran its 30-second command. POST steer returned 202;
  the command executed once, the correction was persisted once, and the final answer
  was exactly `{"verificato":144,"nota":"sterzo103B"}` instead of XML.
  LibreChat's `agents/steering/runtime.ts` is the source reference for durable FIFO
  injection and generation ownership; no live LibreChat score is asserted.

The operator requested visible worker reasoning and complete Italian labels. The
owner-scoped stream now matches the parent cockpit reasoning policy. The final UI
includes localized agent titles/actions/statuses and keeps its read-only provider
mounted throughout streaming so late text and tool arguments remain visible.

This checkpoint records the tested flows, not universal task success, arbitrary-depth
agent teams, or a crash/restart guarantee.

## Final cockpit checks

The image stamped `9186b9ee8` includes the translated UI. In the mounted Playwright
MCP, switching EN/IT changed the pane title between `Agent activity` and
`Attività dell'agente`, with no raw translation key on screen. The resumed worker's
final 99 and its tool arguments were visible. A controlled reload retained the selected
worker and open pane. At 390×844, the mobile drawer measured 341px and the document
had no horizontal overflow. Expanding reasoning displayed 399 characters of actual
provider output and zero redaction placeholders. The accepted screenshot remains
private at `.git/multiagent-04-mobile-final.png` because the cockpit includes operator
navigation. The browser was restored to desktop size afterward.

The post-localization checks passed after updating old copy assertions. Pre-push also
found lint errors in the new test callbacks; commit `984c41703` adds proper callback
types, async flushing and JSON encoding without changing production behavior.

## Extended validation: nested delegation

N01 baseline, 2026-09-07 21:02 UTC, container `9186b9ee8`: the temporary depth cap
of 3 allowed a coordinator to spawn two grandchildren. In conversation
`01a07d70-a80c-7be0-b748-ecadc8bfcfea`, coordinator `w1-fe190d77` reported 1024 and
2187; sibling `w2-e3c60696` really executed Python and returned 1331. The grandchildren
were named flat `w1` / `w2`. Their transcripts were incorrectly under the coordinator's
flat session directory, and their shell calls ended with `reservation failed` (four
failed attempts in `w1.jsonl`). There were no successful grandchild shell ledger rows.
**N01 failed despite correct arithmetic in the final answer.**

The agent tool dispatcher passes `sessionID` to `WithSwarmContext`, losing the root
`ledgerConvID` at the next depth. Synchronous children also retain flat IDs across
invocations and share their parent's operation context. The correction must reuse the
existing trusted operation and delegation-key primitives to preserve origin and isolate
workers. Regression checks cover origin, sibling operation scopes, distinct fresh
invocations and stable retries; the live retest must prove actual tool execution.
The default depth configuration was restored after the baseline.

The previous release CI completed with only the two obsolete swarm-table browser
assertions failing. Commit `8b28a38e3` matches the requested inline-card UI; its Chrome
and mobile Chrome cases passed locally. No live LibreChat runtime is installed: its
recursive `buildSubagentConfigs` and isolated child inputs remain source references.

N01 retest in conversation `01a07dc3-6d38-79a9-84cc-23e228c56e80`, image
`3855812fe-nested` (production source committed as `cd8d59c52`), successfully executed
three shell calls under the originating UUID: sibling `w2-9f67aa68` returned 1331;
coordinator `w1-329a066a` spawned `w1-555e3ba8` and `w2-d96bb730`, which each ran the
same exact command and returned `1024 2187` independently. New unit/race tests passed
after reproducing both defects; three viable mutations were all killed.

Two separate failures remained visible: the root model fabricated a premature summary
with nonexistent `w1-1` / `w1-2` identifiers before real reports arrived; and opening
a grandchild from the coordinator pane immediately closed the pane when its source
card unmounted. The latter is a UI registration-lifetime defect, not a server denial:
the native transcript endpoint already enforces conversation ownership before SSE.

The pane correction `8b1757342` removes the mounted-card ownership gate while keeping
the conversation-switch fence and the server's opaque ownership checks. Thirty-five
worker/shell tests pass, including source-card unmount, restored child, server rejection
and conversation change. Tests that had treated card registration as authorization were
updated with this explicit justification. In the rebuilt image stamped `8b1757342`,
Playwright MCP opened `w1-555e3ba8` from its parent, showed its actual command and
`1024 2187`, and retained the same child and visible final answer after reload.

## Live crash and restart

R01 used conversation `01a07dcb-41f3-7ed8-9409-5ddc4b8ef8e1`. At 21:36:17 UTC,
after the fast child had a committed report and the slow child's command had started,
the test killed Aura with SIGKILL and started the same container. No other delegation
was running or queued. The original 300s lease expired at 21:40:31.935 UTC and the
daemon reclaimed the slow job at 21:40:32.642 UTC, without changing the database lease.

- `w1-7c10833c`: one attempt, one successful shell execution, result 391.
- `w2-198e91b7`: two attempts, two shell starts, one successful observed end after
  recovery, result 551. Its command included a real 60-second sleep.
- Both rows ended `succeeded`; SQL counted one conversation turn for each terminal
  delivery key. The MCP browser showed both completed cards and one report per child.
- The completed sibling was never rerun. The interrupted command can execute again:
  the test confirms at-least-once recovery, not exactly-once external side effects.

The final container is healthy at image
`sha256:7208d3ceb58ed33777da849da0c6a66bc6678d9221ca71565d9f8b691d175af4`,
stamped `8b1757342`. `docker compose up -d --no-deps aura` restored the ordinary
configuration, removing the temporary nesting override. Go vet/build/race passed;
the disposable full Go/DB coverage measurement is **34337/39724 = 86.4%**, with
package-local policy passing. The extended mutation report records **3/3** viable
mutants killed.

## Remaining capability and quality limits

Direct child steer/stop is absent: Aura registers parent-run cancel/steer and worker
transcript/status reads, while workers have no scoped steering ingress. LibreChat's
child controls are a reference capability, not an Aura test passed by proxy. The root
model's premature fabricated summary in N01 is also unresolved. Its later authentic
reports are correct, but they do not make the earlier claim true. No universal agent
reliability score or completed parity with LibreChat is asserted.
