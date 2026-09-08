# Industrial multi-agent controls and grounded results

Status: in progress. Goal: **MAKE AURA MULTIAGENT INDUSTRIAL FULLY VALIDATE E2E
and validate critic agent if not necessary delete look librechat**.
This continues spike 103; its passing runtime tests do not close the full goal.

Latest live checkpoint: child steering/FIFO/replay, real process stop and direct nested
controls have been exercised successfully. Paused cancellation was corrected and retested
as canceled with one model invocation. Concurrency retention now keeps a fifth job queued.
Queued cancellation now passes live probe 104Q3, including durable acceptance after
reload and a terminal report without any model/tool invocation. The no-critic natural
two-agent probe below now has an uninterrupted, grounded final answer. Automatic titles
pass the live timing, manual-rename and interrupted-turn cases. The unknown-result replay
defect is corrected and retested on desktop/mobile. The closing coverage/CI matrix remains open.

## Evidence and references

- Recovery result display, `6b8b1fc17`, healthy image
  `e9bad3f9cc249d88b5fa717e350c82878cb01bbd99f1d755f97fad328ec38747`:
  the same retained conversation now displays Errore/Error on Italian desktop and English
  mobile after reload, preserving the unknown-result text and the generated title.
  Mobile390x844 has no horizontal overflow. The native assistant-ui `isError` flag carries
  the exact store recovery marker; arbitrary output is not reclassified.
  Go and two UI regressions failed before correction;30focused UI cases passed after it.
  Full frontend **2032/2032**, statements8139/8931=91.13%, branches5706/6681=85.40%.
  The complete tagged runner suite passed with race detection after `7131d99fa` separated
  title requests from the transactional test scripts. The full coverage rerun is pending.
  Local chat/Calm Prism replay:23passed initially; two desktop PNG comparisons differed
  only by a one-pixel vertical position of the reasoning label/chevron (190/195pixels).
  The operator requested baseline regeneration; both regenerated cases pass. Thresholds
  are unchanged. Profile/capture-only cases are not counted
  as passing scenarios. Mutation tests now run in CI only, as the operator requested;
  the unfinished local replay-mutation run was stopped and has no passing score.

- Automatic titles, `856ae8e52`, image
  `8673ff54ccd8e1a8541a8d305bbe1c509c0a62049fc4c8b16d9c118f89c15fc4`:
  conversation `01a08077-9e09-7950-9534-adcb27c1aa18` received the Italian title
  "Verifica esecuzione shell Aura" in the sidebar without reload while the real
  Python command was still running (10:02:34 UTC). Its output was8123. A manual rename
  survived another answered turn and reload. Conversation
  `01a0807a-f5a6-73ed-a516-0f84b5e9e005` received its title before the90-second
  command was stopped at10:06:46 UTC, and retained it after reload. That replay
  exposed a separate defect: a synthetic unknown-result marker displayed Completed.
  The PRD records the exact observation in `5d7023609`; it does not prove process exit.
  Full frontend2030/2030, statements8139/8931=91.13%, branches5702/6677=85.39%.
  Title Stryker31/34=91.18%; three Go overlay mutations killed (dedup, fallback, Unicode).
  Isolated vet/build/lint and five touched-package race suites passed. Shared-root
  validation was invalidated by another session mutating gateway source; the disposable
  isolated coverage run is the closing authority, not that failed compilation.
- Natural no-critic probe, conversation `01a08081-2adb-7920-9045-c20685343af3`,
  started10:12:44 UTC. The user prompt requested two independent agents generating
  `secrets.token_hex(16)` and an answer containing their actual IDs and values.
  `w1-98d0f0436b563b16a6a0dccbcfdf8d60` executed one shell call and returned
  `5f7704e1618fb01678916be8466ede2a`; `w2-4d46301bb693b72bb71f8822822ebabc`
  executed one shell call and returned `b06d32bb5efa8fe24093ac857af9188f`.
  Both durable transcripts have one start/end pair, status ok and exit_code0.
  The coordinator's answer matched both pairs without a user follow-up or retry.
  The automatic report-delivery continuation repeated the correct pairs. This is one
  bounded natural task, not a claim of universal answer correctness.

- The operator explicitly requested removal of the LLM critic on2026-09-08.
  Removed its request, prompt, parser, digest, history offset and model override;
  `AURA_COMPLETION_GATE` retains only deterministic reply hygiene. Verification,
  delivery, ownership and budget controls remain. The budget-end regression failed
  before removal with3calls for a2-call task, then passed with2 on both terminal
  paths. Hygiene veto/discard and verification tests still run without an auditor.
  Race suites passed; vet/build/lint passed; capability evaluation20/20 without skips.
  Complete disposable coverage **34,602/40,003 = 86.4985%**, package policy passed;
  two local-gate mutations killed. The old critic-only tests were retired with the
  feature, while shared request-history and retry regressions were retained.
- Live no-critic conversation `01a08023-82d1-7675-8a97-42a6fd84f980`:
  workers `w1-af28eb817f860caf7a3cd7214992b1d9` and
  `w2-de453b17fc49d5710b98882454e59357` each executed one real `shell_exec` and
  generated unpredictable32-hex values. The coordinator's follow-up answer matched
  both child IDs and exact outputs `b9b2fb573dc8d8ac81fcaffe0950a768` and
  `7ae0855a20ef382a455c5f500a302a1f`. Original parent synthesis encountered a
  provider stream-open timeout after120s; it recorded an interrupted answer rather
  than success. Thus this is a verified resumed synthesis, not an uninterrupted
  first-turn success claim. Healthy image:
  `3e27918a2fa63cf50e7bd03a5020cafdaa97ef0042efa29f6c3eb356ad08dc30`
  (`4b3ff2be1-no-critic`). Logs `.git/remove-critic-{tests,static,capability,full-coverage}.log`.
- Previous5081 CI failed only the two Calm Prism matrix cases: the ended fixture
  deliberately leaves one tool without a result but expected Running. Its assertion
  now verifies Interrupted and no Running label, matching104X. Desktop/mobile replay
  passed2/2; no screenshot threshold or production behavior was weakened.
-104X, conversation `01a07fe1-0757-7516-9cee-af46d25e8fb3`, child
  `w1-9c520222b53268ab8d9b54881299e6e3`: UI steer202 at07:19:00.517967Z,
  receipt `11ba041e-7968-451c-b75a-6e4bb175278e`, then real backend SIGKILL while
  Python PID7326 slept80. Explicitly restarted Aura; native lease expiry at
  07:22:54.45483Z allowed attempt2. New Python PID14451 ran the command, and the
  job succeeded at07:24:19.078883Z with `{"value":6011}` and no old `nota`.
  The old receipt stayed undrained; the API reports rejected/owner_unavailable.
  Its database expired_at remained NULL with expires_at07:34:00.517909Z: this is
  an honest unavailable-owner projection, not a stored worker_run_ended rejection.
  This proves the missing crash/control combination, not exactly-once execution.
- The104X pane then exposed an unresolved old tool spinning after overall success.
  The renderer now consumes assistant-ui's native part status; a terminal part
  without a result shows localized Interrupted and an explanatory note, with no
  invented result or unknown duration. The retained104X pane passes on desktopIT
  and mobileEN, including reload; drawer341px at390x844 without overflow.
  Live104Y (`01a07ff7-5dfd-7a75-937f-31019238fe2d`, child
  `w1-b641290ed45418d76a66ab623b4e1beb`) still showed a running pulse at26s and
  completed its actual35-second command with7011. Full frontend: **2025 tests**,
  **8135/8927 statements = 91.12%**, **5702/6678 branches = 85.38%**;
  targeted toolStatus mutation **38/39 = 97.44%**. Healthy image:
  `518d81de3fb5acb20c3fdc08ca030752d41773c898e0b1089cffb2cc08ed3064`
  (`0a03cfe24-interruptedpart`). The image build is the frontend build authority;
  no additional webbuild export/build is needed, as the user clarified.
- Replay timing: MCP104W originally showed20s after opening mid-command, then0s
  after reload. The retained start/end timestamps are06:47:03.692734448Z and
  06:47:34.447222259Z; the native tool duration is30,754ms. `Translate` now uses
  AG-UI's existing Event.SetTimestamp for source-backed frames. MCP reload shows
  the correctly rounded31s, desktop Italian and mobile English, same conversation
  and selected worker. Mobile drawer341px at390x844, no horizontal overflow.
  The Go regression failed before correction; AG-UI race tests and two targeted
  timestamp mutations passed/killed respectively. Complete disposable Go coverage:
  **34,704/40,125 = 86.4897%**, package policy passed. Local evidence:
  `.git/replay-timestamps-full-coverage.log`, `.git/replaytime-mutant-{zero,lost}.log`.
- Cross-owner controls now have a real HTTP/session boundary proof:
  `TestWorkerControlsWithRealAuthulaCookies` passed with race detection against
  disposable `aura-postgres-auth104:5435`, database `aura_cov_auth104`.
  Two native Authula sessions/cookies, real identity links, Postgres conversations,
  worker receipt store, idempotency registry and queued jobs drive a TLS server.
  B gets404 for running history/steer/cancel and queued history/cancel, despite a
  forged identity header. No receipt, cancellation flag or execution stop occurs.
  An invalid cookie gets401; A can read, steer and cancel. Replayed A steer leaves
  one receipt; replayed cancel invokes Stop once; queued cancellation persists.
  The same real HTTP test rejects malformed JSON and oversized corrections with400,
  an unknown run with404, and a stale queued attempt with410.
  This tests real session validation and authorization, not credential/TOTP login
  or an LLM invocation; real process stopping is covered separately by MCP104W/104E.
  Evidence: `.git/authula-controls-e2e.log`. The disposable container/database is
  removed by the existing harness; no live user account was changed.
- PWA stream lifetime correction, `c5c5053c5`: a controlled MCP A/B experiment
  stopped the active service worker while two real owner-scoped SSE connections
  were open. Only the proxied connection closed. Non-navigation requests now use
  the existing precache allowlist; API/SSE goes directly through the browser.
  LibreChat's static/locale-only cache scope and MDN's native fetch-event behavior
  were checked before implementation. The new service-worker-enabled Playwright
  regression failed before the change and passed after it on desktop/mobile Chrome
  (2/2), including 16 seconds after stopWorker, zero stream errors and retained
  static-asset caching. MCP repeated the passing OPEN/zero-error result once the
  new worker was verified active. Healthy deployed image:
  `4500b18afce261da2289318fb29834297fb4776c402bdde3bd39dcf8118202e6`
  (`9988af0b7-pwastream`). Build, TypeScript, lint and generated-dist checks passed.
- Live 104W in conversation `01a07fc2-fa60-7290-a0a5-eaf2fc4823eb`: child
  `w1-5f8a2cbedea2eb3c99ff4da3b56661c1` had a real sleeping Python PID50190.
  Stopped the PWA worker, then canceled through the UI (POST202, network9752).
  The process disappeared; both worker and command displayed Annullato live and
  after reload, with no remaining stop button. The earlier104V in that conversation
  returned actual random value `d0d735c96f52484487ba6d5c` and sibling4011; it finished
  before stopWorker, so104V alone is not a live interruption proof.
  Detailed before/after and transition limits: `.planning/debug/worker-stream-closure.md`.
- Interrupted-command correction: `shell_exec` now records `cancelled` in its footer
  and metadata, the tool invocation carries `canceled`, and the existing code display
  conveys cancellation to the card and group indicator. The same normalizer recognizes
  the validated foreground footer of retained 104R results; replay projects the corrected
  display without rewriting the original audit event. The 104R pane now displays its
  interrupted command as Annullato and its later successful command as Completato.
  Current image: `dc90462efcc27f0ba63bcfc35bdbec694333924d8a7b029bf58b42aed6a24542`
  (`8d1ec73a9-cancelledtool`). Native Go race suites passed; four Go overlay mutations
  were killed. Stryker killed **22/23 = 95.65%** for `toolStatus.ts`, now included in
  the regular mutation contract. Full Go/Postgres coverage passed at
  **34,677/40,106 = 86.4634%**, including the package policy. Full UI passed
  **2022 tests / 239 files**, statements **8133/8925 = 91.12%**, branches **5690/6666 = 85.35%**.
- 104T, conversation `01a07f5d-aa8f-7d09-bab4-b1e9283d961c`: stopped the live
  Python PID79680 for `w1-2c21fd958624f82363a57497b6ab03a6`, POST202, job canceled
  at attempt1. Its retained tool event has `status:canceled`, metadata `cancelled:true`
  and code display `cancelled:true`; sibling `w2-eec4e11bd117f3204ff6a4e12553d36e`
  succeeded at attempt1 with2026. The live pane nevertheless stayed Running with a
  stream error; CDP inspection found BOTH EventSources CLOSED. Fresh authenticated
  SSE fetch returned the complete cancellation frames and RUN_FINISHED. No cause is
  claimed and no speculative stream patch was made.
- 104U instrumented repeat, conversation `01a07f71-f1f0-7d20-9783-72153c9df612`:
  stopped live Python PID17455 for `w1-74adc0eaa2edb60039cb626560fad1c9`, POST202,
  canceled at attempt1; sibling `w2-b645be32d955e1ac75eb046348cab15e` succeeded once
  with3026. Native EventSource open/error/close tracing showed the child receive
  RUN_FINISHED and close normally, while the global status stream remained open.
  The live pane displayed both worker and command as Annullato. Observation did not
  replace responses or alter the application handlers. The temporary observer was
  removed and the page reloaded afterward. The intermittent 104T closure remains
  a follow-up, not a passing stream-lifecycle proof.

- 104R, conversation `01a07f35-3786-76e0-97ac-292e4635c742`: while R1
  `w1-09dc869e2a6ec074f571657db7314658` executed its 80-second command, a UI steer
  returned 202 with receipt `8900aef9-c80d-4acb-b5c7-360aa33819f6`, still accepted
  at `04:10:57.427 UTC` for run `run-01a07f35-a208-7ca3-a134-3536fc42ab6e`.
  An actual `docker compose restart aura` stopped both current commands; jobs were
  queued with `context canceled` then reclaimed at attempt2. The receipt was
  durably rejected with `worker_run_ended`. R1 ended with `{"value":2401}` and R2
  `w2-ddd946311300f3aa2a6de6886cc63d92` with `{"value":6561}`; neither adopted
  the old correction's requested `nota` field. Both finished succeeded.
  This proves pending control settlement through graceful restart, not SIGKILL
  settlement (the crash/reclaim substrate was separately exercised in spike103).
  It exposed another UI correctness gap: the interrupted shell result explicitly
  says `[command cancelled]`, yet its structured tool invocation status is `ok`
  and its tile says Completed. That classification remains to be corrected.
- CI at `d522023a5` failed 21 Web E2E cases because three mocked conversation
  fixtures omitted the now-unconditional worker discovery stream. Their synthetic
  IDs reached the real server and returned 401/404. Add empty native SSE responses
  only for those fixture IDs; preserve strict browser-health assertions and live
  worker streams. Local replay then passed 31 cases; two Windows desktop screenshot
  comparisons still differ by 190 pixels in the Reasoning label. Their identical
  baselines passed the screenshot assertions on Linux CI; that run failed afterward
  at browser health. No screenshot threshold or baseline is changed by this fix.

- 104Q3, 2026-09-08, conversation `01a07f06-eab6-7d19-9134-6916a6180066`,
  image `58cda375926a32d28e3f90bb6e7a113d9c4d9a2b515f9bc4f7f2d12d4957bc39`
  (`f387ad6ca-queuedcancel`): four running jobs and fifth
  `w5-31eb9d42da4d0f5a9440acfc47776370` queued at attempt0. Its UI stop sent
  `{job_id:"7266cf44-e8b7-4134-ba21-ba5d424d16ee",attempt_count:0}` and returned
  202 in 1948ms. Reload kept `cancel_requested:true`, no remaining Stop button,
  and the pending acceptance text. At `03:22:29.062 UTC` normal delivery recorded
  `canceled`: the entire transcript was one terminal marker, no model/tool events.
  `/workspace/scratch/aura-control104-Q3-must-not-exist.txt` remained absent.
  This proves pre-start cancellation, not ordinary completion of the siblings:
  the test's 150-second silent commands exceeded the configured 120-second idle
  watchdog. All four retried once, then were explicitly stopped via UI (four 202s).
  The queued worker's empty terminal pane still said Connecting. Commit `fd7919f04`
  fixes that. The retest on image
  `e4bcff495d85791331bebdc1b3561b98f45081f74dd57e9d20a0fd3db729b408`
  (`fc16e8cad-emptyactivity`) passed Italian desktop reload and English mobile reload:
  correct empty terminal copy, retained drawer 341px at viewport 390px, no overflow.
  The first coordinator turn only searched for swarm_spawn but claimed Avviati;
  a corrective user turn was necessary to actually enqueue. This is another
  grounded-answer failure, not a passing autonomous task-execution result.
- Queued-control native verification: 60 worker UI tests passed, TypeScript build
  and targeted ESLint passed; Go agui/documents/swarm race suites passed; the two
  disposable-Postgres queued-control tests passed with race detection. Native Go
  overlays killed all three SQL mutants (attempt fence, conversation scope,
  pending-delivery protection). The complete native Go/Postgres matrix now passed:
  **34,654/40,085 = 86.4513%**, including the package policy. `internal/documents`
  is **708/824 = 85.9223%** and now enforces the full 85% target. Its earlier
  683/824 result exposed a stale denominator; new invalid-target, delivery-lease
  heartbeat and terminal counter tests raised coverage instead of lowering a gate.
  Full frontend lint passed after the mobile test stub correction. Release CI remains pending.
- 104S, conversation `01a07f23-7c8d-72ae-ba64-123588329a85`, temporarily using
  the existing depth3 configuration: coordinator `w1-f84b088f001e8cef1a460474729ce79d`
  spawned `w1-53d51046a61b59926b75c7215e07c459` and
  `w2-0b193a610f9a379087ea262ccb8f1a48`. Docker showed their Python PIDs 80063
  and 80144 executing the 80/85-second commands. Focusing the coordinator's Stop
  button and pressing Enter through MCP returned 202 in 87ms. Both descendant
  processes disappeared, both transcripts ended `canceled`, and the coordinator
  job ended canceled at attempt1. Both marker files stayed absent after their
  original deadlines. Independent sibling `w2-65c37720a7c30a949fbfe75974f2836f`
  kept PID79801, completed its one 95-second shell call, and ended succeeded at
  attempt1; its visible final text was `The output of the command is 1331.`.
  This proves subtree stop, actual process isolation and a live keyboard control.
  Normal depth2 configuration was restored afterward; no active test workers remain.

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
| Steer a live child | MCP correction during a real tool; final output follows it | Passed 104D and 104N |
| Sibling and parent isolation | Their inputs, executions and results stay unchanged | Passed 104D, 104E, 104F2 and 104N; 104Q3 is not a regular sibling-completion proof |
| FIFO and exact logical retry | Multiple corrections; one idempotency key never applies twice | Passed 104D |
| Control receipts | Accepted and applied are distinct, visible after reload | Passed 104D; queued acceptance reload passed 104Q3 |
| Stop a live child | Prompt cancellation, terminal canceled report, no retry | Passed 104E, including actual process termination |
| Stop lifecycle races | Completion, queued/paused work and accepted controls resolve honestly | Paused 104F2 and queued 104Q3 passed; completion fence covered natively |
| Nested control and visibility | Discover/control a live grandchild; preserve siblings and ancestry | Passed 104N direct controls and 104S coordinator subtree stop |
| Ownership and input bounds | Real scoped API denies foreign/malformed/stale targets and oversized input | Passed real Authula-cookie TLS/Postgres regression: foreign404, malformed/oversize400, stale run404, stale queued attempt410; owner controls/replay remain valid |
| Restart and control settlement | No silent application to a new incarnation; completed/canceled work is not retried as failure | Passed graceful104R and real SIGKILL104X; the lost owner's undrained receipt is reported rejected/owner_unavailable |
| Interrupted tool outcome | Canceled commands have an honest structured and visual status | Passed retained104R and live104U/104W; PWA stream closure corrected and replay retains original execution duration |
| Grounded final answers | Delayed unpredictable outputs match actual reports; no fabricated IDs or premature success | Failed baseline |
| Failure and hostile report data | Honest partial results; report text cannot become operator authority | Prior trust framing passed; broaden final-answer proof |
| Desktop/mobile and EN/IT | MCP controls, keyboard, reload and readable status on both layouts | Reload, final text and EN/IT empty state passed; live Stop via keyboard passed 104S |
| Quality gates | vet/build/test/race, disposable full coverage >=85%, mutation >=70%, all CI green | Local Go and frontend matrices passed; release CI still pending |
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

Pause104F2 retest, conversation `01a07ee8-ff17-77f2-9965-fbca4a57f344`: the canceled
child `w1-4a3b1082a4ba5f26fd2a9f08c537faa7` ended canceled with durable intent; its
transcript contains one model invocation only. The second queue claim delivered the
terminal report without constructing another model. The sibling returned169 once.

After restoring the mobile open intent and using assistant-ui's native Parts render
function, the nested JSON and341px drawer survived a real390x844 reload with no overflow.
A later language-switch reload stalled in browser navigation (the app stayed healthy);
that language-switch attempt is not counted as passed. The browser was returned to
Italian and desktop after navigating through the login route; authenticated fetch200.

Queue104Q2 on image`c499bf6c7`, conversation`01a07edc-960d-7044-8ff9-532133817025`:
four workers ran while `w5-341a2701ecce7c60810642b82e87dc2e` stayed queued at attempt0
across later polls. Admission is corrected. The fifth card still said In corso and
opened a nonexistent transcript; no stop button existed before execution. Queued
visibility and pre-start cancellation remain separate work, not passing evidence.

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
