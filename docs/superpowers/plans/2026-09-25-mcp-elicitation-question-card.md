# MCP elicitation and the question card: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A mounted MCP server that asks for input during a cockpit turn gets a form in the thread; the call waits, with its clock and the run's stopped, until the operator answers, and the answer goes back to the server. The ask_user card is redrawn in the same visual language.

**Architecture:**

- **Two new neutral packages.** `internal/pausable` is a context deadline that a hold can stop. `internal/elicit` is the question/answer vocabulary plus the `Asker` seam, so `internal/agent/mcptools` and `internal/agui` never import each other.
- **mcptools routes the request.** The handler finds the run that owns the call:
  - the call's own context on the multi-round-trip path;
  - otherwise an in-flight registry keyed by `*sdkmcp.ClientSession`.
  It asks that run's `elicit.Asker` with every clock of the call held. When no run can be asked, today's decline-and-surface consent takes the request.
- **agui carries the question.** Each `RunSession` owns a `runQuestions` asker, installed on the detached run's context:
  - it publishes `aura.elicitation` into the run's replay ring;
  - it waits for `POST /agent/runs/{runID}/elicitations/{id}`;
  - it publishes `aura.elicitation_resolved` when the question closes.
- **The cockpit renders both kinds of question in one frame.** `web/src/questions/QuestionCard`:
  - the ask_user adapter is `InlineApprovalCard`;
  - the MCP form adapter is `ElicitationCard`, one step per field.
  Both sit in the stack above the composer, as today.

**Tech Stack:** Go 1.27.1, go-sdk v1.8.0 (`sdkmcp`), google/jsonschema-go v0.4.3, goleak, `-race`; React 19, react-i18next, vitest, Stryker, shadcn registry (`@tool-ui`).

**Spec:** `D:\Aura\docs\superpowers\specs\2026-09-25-mcp-elicitation-question-card-design.md` (approved 2026-09-25, revised at 203c62be8: see its `## Revisions`). Executors read the spec and this plan together. Where they disagree, **Open points** at the end says which way this plan went and why.

**v2** folds in the four validation reports (`docs/superpowers/plans/validation/2026-09-25-elicitation-*.md`) and the operator's rulings (`.superpowers/sdd/2026-09-25-elicitation/progress.md`, "Rulings for v2"). The **v2 changelog** after Review Focus says what became of each finding.

## Global Constraints

**Repository and commits**
- Repository: `D:\Aura`, branch `master`. Commit on `master` directly; no feature branch.
- Another session may be working in the same tree. HEAD moved while this plan was written: `914317182` for v1, `fb070f6de` for v2. `fb070f6de` touched only four `internal/channels/telegram` files, none of which this plan cites.
  - Commit with explicit paths only: `git add <new files>`, then `git -c core.hooksPath=.git/hooks commit -F - -- <paths>`.
  - Unstage anything you did not write.
  - Re-read any file immediately before editing it.
- End every commit message with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`.
- **Every `bash` block runs in WSL.** Save it as a script in your scratchpad, then run `MSYS_NO_PATHCONV=1 wsl bash /mnt/c/.../<script>`. Do not use `wsl bash -c`: it expands `$HOME` and `$PATH` before WSL sees them.
  - Git Bash's `git` is `git.exe`, and its hooks would run lefthook's Windows binaries.
  - In WSL, `core.hooksPath` is the Windows path `D:\Aura\.git\hooks`, which WSL git cannot resolve. That is why each block exports `LEFTHOOK_BIN` and passes `-c core.hooksPath=.git/hooks`. Without them, no hook runs, and nothing says so.
  - A commit or push that ran its hooks prints the lefthook banner. No banner means no gate ran.
- No push without the operator's go. Pushing `master` publishes the edge image, and the appliances install it.

**Build and test**
- Go runs in WSL. Create `<scratchpad>/aura_go.sh`:

  ```bash
  #!/usr/bin/env bash
  # Run one command in the Aura tree from WSL, with the Go toolchain on PATH.
  set -uo pipefail
  export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
  export CGO_ENABLED=1
  cd /mnt/d/Aura || exit 1
  "$@"
  ```

- The web runs in WSL too, per the brief. Create `<scratchpad>/aura_web.sh`, identical except `cd /mnt/d/Aura/web`.
  - WSL's own node 24 is linked into `~/.local/bin` (`node`, `npm` and `npx` point to `~/.local/aura-toolchains/node24`). The script puts that directory first on PATH, and it has to: without it, `npx` resolves to the Windows install under `/mnt/c/Program Files/nodejs`, which must never run. Measured 2026-09-25.
  - `web/node_modules` carries both the linux and the win32 rolldown bindings (measured 2026-09-25).
  - Never fall back to Git Bash for the web, or for Go, Python or git. Its bare `node`, `npx`, `go`, `python3` and `git` are Windows executables.
  - A WSL web run is slow: `node_modules` sits on `/mnt/d`. The four directories Task 8 checks take about ten minutes. Run them in the background and read the summary, not the tail of the log.
- Below:
  - "Go: `X`" means `time MSYS_NO_PATHCONV=1 wsl bash /mnt/c/Users/Davide/AppData/Local/Temp/claude/D--Aura/<session>/scratchpad/aura_go.sh X`;
  - "Web: `X`" means the same with `aura_web.sh`.
  - `<session>` is your own session's scratchpad directory.
- Never run a Windows `.exe`. Edit with the Edit tool, never with a script.
- Per task, run the touched packages' tests with `-race -count=1` and `go vet` on them. The full gates run once, in Task 9.
- Mutation testing (go-mutesting, Stryker) runs in CI only. Never mutate locally, by tool or by hand. "The test can fail" is shown by each task's RED run.
- `npm run lint` is `oxlint --type-aware --max-warnings=0 .`. A warning fails it as an error does, and oxlint exits 0 either way. Read its summary line: it must say `Found 0 warnings and 0 errors.`
- Code blocks follow the tree's formatters: gofmt for Go, Prettier (`printWidth: 100`, `web/.prettierrc`) for the web. Each web task runs `prettier --write` on its files before `--check`.
- **File-size limit: 600 lines per file, tests included.** Measured on 2026-09-25, on scratch copies with each task's code applied:

  | File | Lines at HEAD | After this plan |
  |---|---|---|
  | `web/src/chat/ExternalStoreChat.tsx` | 587 | 594 (Task 8). Spec 2 must split it before it grows. |
  | `internal/agent/budget_test.go` | 589 | 589: new tests go in `budget_pause_test.go` |
  | `web/src/i18n/resources.ts` | 580 | 583 (Task 7) |
  | `web/src/chat/sseAdapter.ts` | 554 | 565 (Task 8) |
  | `internal/agui/server.go` | 539 | 543 (Task 6) |
  | `internal/agent/mcptools/bridge_supervisor.go` | 500 | 500: Task 4 swaps two calls |
  | `web/src/chat/sseResume.ts` | 442 | 450 (Task 8) |
  | `web/src/approvals/InlineApprovalCard.tsx` | 371 | 291 (Task 7) |

  - Nothing is vendored (option V), so no file needs a size exemption.
  - Measure with `wc -l` after each edit. Split before a file passes 600; never after.

**Values from the spec** (verbatim, as revised at 203c62be8)
- Caps: 20 fields, 50 enum options, a 2 KiB message, 256 B titles, a 1 KiB description, 4 KiB per string answer.
  - The validation added two more (adversarial H3): 16 KiB for the whole question, restoring today's bound, and 4 questions open at once per session.
- Kinds: `string`, `number`, `integer`, `boolean`, `enum`. Formats: `email`, `uri`, `date`, `date-time`.
- Field order: the required fields first, in the order of the schema's `required` array, then the rest by name.
- Events: `aura.elicitation` carries id, server, tool, message, fields and deadline. `aura.elicitation_resolved` carries `{id, action}`. Neither carries an answer value.
  - The plan adds `run_id` and `refusal` to the first, and `expired` to the second (Open point 1).
- Route: `POST /agent/runs/{runID}/elicitations/{id}` with `{action, content}`:

  | Code | When |
  |---|---|
  | 410 | the run is terminal |
  | 404 | the question id is unknown, or the run is not the caller's |
  | 409 | the question is already resolved or expired |
  | 422 | the content fails `elicit.Validate`: the body carries a problem code per field, never the validator's text or the value. The question stays open. |
  | 202 | the answer was delivered |

  - The plan adds `GET /agent/runs/{runID}/elicitations`, owner-scoped the same way. It lists the run's open questions, so a reload the ring can no longer replay still finds its form (adversarial H4).
- Bounds:
  - The wait ends at the first of these:
    - `AURA_MCP_ELICITATION_TIMEOUT_SEC` (default 300 s; `<= 0` disables). A timeout is a **cancel** ("dismissed without an explicit choice"), and the card shows "expired";
    - the end of the call (cancel);
    - the end of the run (cancel).
  - Only the operator's time is excluded from the clocks. The server's and the model's time still count.
- URL mode stays refused, before any asker is consulted. The handler returns `(*ElicitResult, nil)` in every case.
- A schema whose `pattern` Go's regexp (RE2) cannot compile is refused before anyone is asked.
- Only the action, the server and the field count are recorded; the operator's values never are.
- No new environment variables, no migration. The caps are constants in `internal/elicit`, with their reason next to them.

## Review Focus

1. **A form that takes the operator longer than the call bound and the run bound.**
   - The call bound is 60 s and the run bound is 300 s.
   - Both clocks must stop, and so must the per-tool node timeout when it is set.
   - Pinned in:
     - Task 2: `TestBudgetWallclockSkipsHeldTime`, `TestRunToolNodeTimeoutStopsWhileHeld`;
     - Task 4: `TestAHeldCallOutlivesItsTimeout`;
     - Task 6: `TestDetachedRunAnswersAnMCPFormWhileBothClocksStop`, on 1 s and 2 s bounds with a 3 s operator.
2. **The elicitation timeout silently disappearing.**
   - A held call context reports a deadline earlier than the question's. `context.WithTimeout` trusts the earlier deadline and arms no timer, so the 300 s bound would never fire.
   - Pinned in Task 4: `TestAnUnansweredQuestionExpiresWhileTheCallIsHeld`, where the call bound is shorter than the question's.
3. **Two conversations using one MCP server at the same time.**
   - A classic request with calls from two runs in flight must ask neither operator.
   - Both must be told why, and the server must get a decline.
   - Pinned in Task 4: `TestClassicElicitationWithTwoRunsInFlightAsksNeither`.
4. **A reload in the middle of a form.**
   - The question comes back once, not twice, even when the replay and the run's list both bring it.
   - It comes back even after the ring has rotated past it.
   - Its resolution still applies, and after the run ends a replay still carries both frames.
   - Pinned in:
     - Task 6: the replay half of the integration test, and `TestAFormTheRingRotatedPastIsStillListed`;
     - Task 8: `applyElicitationSignal`'s "holds a replayed question once, and its resolution still applies".
5. **An answer the server's schema refuses.**
   - The question stays open.
   - The card goes back to the failing field's step with the error there, and the 422 carries no value.
   - A corrected answer is then delivered.
   - Pinned in:
     - Task 6: `TestAnAnswerThatFailsTheSchemaLeavesTheQuestionOpen` and `TestARefusedAnswerLeavesNoValueInTheReplayStore`;
     - Task 8: `ElicitationCard`'s "a 422 puts the card back on the failing step, with the error there".

## v2 changelog

One line per finding of the four reports (`docs/superpowers/plans/validation/2026-09-25-elicitation-{codereview-backend,codereview-web,adversarial,crosssource}.md`), then the INFO notes that changed the plan. "Ruling" means the operator's rulings for v2 (`.superpowers/sdd/2026-09-25-elicitation/progress.md`), which win over a validator.

**codereview-backend**
- codereview-backend/H1 → applied: `newRealFormRunner` sets `PreviewCap: 2048` and `RunDir: t.TempDir()` (Task 6).
- codereview-backend/M1 → applied: the `Kind*` block has its comment, and Tasks 1 and 3 run `golangci-lint run` on the package before committing.
- codereview-backend/M2 → applied: `TestAFormFromARunWithNoCockpitReachesItsOperatorsChannel` runs the no-asker fallback through `CallToolText` on both paths and checks it is told on the call's identity. `TestAPanickingAskerDeclines` covers the recovery (Task 4).
- codereview-backend/L1 → applied: the Task 3 test files are shown gofmt'd, as copied from the scratch tree.
- codereview-backend/L2 → applied: `TestChildBudgetSeesTheParentsHeldTime` is in Task 2's RED list.
- codereview-backend/L3 → applied: `budget_test.go:516`.
- codereview-backend/L4 → applied: Task 4 expects the `cannot use elicit.Question{…} … as mcptools.ElicitationRequest value` error.
- codereview-backend/L5 → applied: `mcp/client.go:894-904`.
- codereview-backend/L6 → applied: `mount.go` 26-31 in both places (Task 5).
- codereview-backend/L7 → applied: the e2e wraps the tool and `Adopt`s it, so the per-process loaded slots cannot defer it (Task 6).
- codereview-backend/L8 → applied: Task 10 Step 10 keeps line 656 up to "unlimited execution." and replaces from "The production elicitation …".
- codereview-backend/L9 → applied: the handler logs `redact.Line`, capped with `truncateUTF8Bytes` (Task 4).
- codereview-backend/L10 → applied: "only direct children" in Task 1's rationale, comment and commit body.
- codereview-backend/L11 → applied: `publish`'s comment says `redactEvent` rewrites only `RUN_ERROR` (Task 6).
- codereview-backend/L12 → applied: Task 9 Step 8 says the Playwright run cannot fail on a visual diff under the TEMP harvest.
- codereview-backend/L13 → applied: Task 9 Step 6 quotes the two success lines the scripts print.

**codereview-web**
- codereview-web/H1 → applied: Tasks 7 and 8 add their seven suites to `web/vitest.stryker.config.ts`. Task 7 adds `approvalState.test.ts` too, missing although its module is mutated.
- codereview-web/M1 → applied: `useRef<(HTMLButtonElement | null)[]>([])`.
- codereview-web/M2 → applied: Task 9 Step 9 downloads the `calm-prism-snapshots` artifact from the push's run, checks the four PNGs and commits them.
- codereview-web/L1 → applied: every web edit is now a diff whose hunk headers carry HEAD's lines. The CUSTOM comment is at 303-306. `chat.spec.ts` is not touched (web/L9).
- codereview-web/L2 → applied: each web task runs `prettier --write` before `--check`, and the plan's blocks are the files `--check` accepts. Past 100 columns are only strings Prettier does not break.
- codereview-web/L3 → applied: every lint check requires `Found 0 warnings and 0 errors.`
- codereview-web/L4 → applied: Task 7 says two tests change and six are added. Its commit body counts every changed site.
- codereview-web/L5 → applied: Review Focus 5 names "a 422 puts the card back on the failing step, with the error there".
- codereview-web/L6 → applied: `isStringList` is exported once, as a type guard, from `sseAdapter_elicitation.ts`.
- codereview-web/L7 → applied: the size table is re-measured on copies with each task applied (583, 565, 594, 450, 543), and the HEAD note is updated.
- codereview-web/L8 → changed: the server's message is now the description on every step, as the spec says. A field's own description is a hint under its input. The step title stays the field's, as Question Flow titles a step; the chip names the server.
- codereview-web/L9 → rejected: `chat.spec.ts` is no longer touched. Under the approvals ruling, its no-option approval (`chat.spec.ts:296-307`) keeps free text and **Answer**, so the comment at 14 stays true.
- codereview-web/L10 → applied: `initialValue` converts a date-time default to local `YYYY-MM-DDTHH:mm:ss` (`localDateTime`), and `FieldInput` sets `step: 1`.
- codereview-web/L11 → applied: the `ExternalStoreChat_streams.ts` diff carries the type import.

**adversarial**
- adversarial/H1 → applied: an ambiguous-run refusal keeps only the server and the reason. `assertBareRefusal` pins it in the three shared-session tests of Task 4.
- adversarial/H2 → applied: `FieldErrors` holds problem codes only (Task 3). `TestARefusedAnswerLeavesNoValueInTheReplayStore` goes through the idempotency layer (Task 6).
- adversarial/H3 → changed: the caps apply (`MaxQuestionBytes` 16 KiB, `MaxOpenQuestions` 4 per session and per run, duplicate enum values refused), and so does the flood test. A request past the open cap is declined and logged without telling anyone, since a notice per request would be the flood itself.
- adversarial/H4 → applied: `GET /agent/runs/{runID}/elicitations` (Task 6, `TestAFormTheRingRotatedPastIsStillListed` on an 8-event ring), fetched by every reattach (Task 8).
- adversarial/H5 → changed: per the ruling, every mount advertises elicitation. Task 10 re-checks the three servers' tool counts and drives one Telegram turn through `trigger-elicitation-request`. The PRD records the third-party risk (Open point 9).
- adversarial/H6 → applied: E2E step 5 checks that `trigger-url-elicitation` is absent (Task 10 Step 3, spec step 5). The PRD says the refusal branch was not exercised.
- adversarial/M1 → changed: (b) is applied, since the `resources/read` of a call's links is marked. The identity rule is applied too: a run is keyed by asker and identity (Task 4). (a) cannot be fixed inside go-sdk v1.8.0, which drops the request's call (`mcp/streamable.go:2617-2680`), so it is recorded as a limit (Open point 7, PRD).
- adversarial/M2 → applied as documentation, per the ruling: `Budget`'s comment (Task 2) and the PRD. There is no held-time cap; the 3600 s cap bounds it (Open point 3).
- adversarial/M3 → applied: the refusal notices travel on a tracked goroutine, and the decline returns at once (Task 4).
- adversarial/M4 → changed per the ruling: an approval with no options keeps its free-text reply (a). The pill says Approve only when every option is a gateway scope, and Answer otherwise (b, `offersOnlyScopes`, Task 7).
- adversarial/M5 → applied: once the thread stops streaming, `useThreadElicitations` keeps only the cards of the run still live, and a new run drops the earlier run's cards (Task 8).
- adversarial/M6 → applied per the ruling: the 300 s run bound is on the PRD's "does not prove" list (Task 10).
- adversarial/M7 → applied: the PRD records the TypeScript SDK's 60 s default. The countdown reads "Aura cancels in …", Aura's own bound, and an expiry now cancels.
- adversarial/M8 → applied: Task 9 adds the `pausable` and `elicitation_route` Go scopes.
- adversarial/M9 → applied: Task 4 adds the identity-scoped mount, redial, log-capture, flood and two-identity tests. Task 6 adds the 422 replay-store and ring-rotation tests.
- adversarial/M10 → applied: `closeLocked` publishes with the run's context, `rq.runCtx` (Task 6).
- adversarial/M11 → applied: `min_items`/`max_items` are projected, Next is held outside them, and the card says "Choose 1 to 3.". A `pattern` is validated with RE2 and refused up front if RE2 cannot compile it.
- adversarial/L1 → applied: the test's run context is bounded at 5 s (Task 4).
- adversarial/L2 → applied: the `publish` comment says what `redactEvent` does.
- adversarial/L3 → rejected: a replay re-sends the stored frame unchanged (`runsession.go:126` stores it, `:166` replays it), so a relative `expires_in_ms` would be stale on every reload, while the absolute `deadline` stays true. The server-side bound decides, and its resolution frame settles the card.
- adversarial/L4 → applied: `redact.Line` and `truncateUTF8Bytes` on the logged reason (Task 4).
- adversarial/L5 → applied: Task 10 Step 10 keeps line 656's first half and amends "finite configured bounds" with a paragraph on what bounds a held wait.
- adversarial/L6 → applied per the ruling: Use default, with the default shown on Review and in the receipt.
- adversarial/L7 → applied: `FromSchema` refuses "two options have the same value" (Task 3).
- adversarial/L8 → applied: a URI needs only a scheme, so `file:///tmp/a` passes (Task 3).
- adversarial/L9 → applied: the PRD's "does not prove" list says human time now counts in the latency metrics.
- adversarial/L10 → applied: Task 9 Step 8 tells the operator the push ships before the E2E.
- adversarial/L11 → applied: Task 10 Step 3 waits for `tools/list_changed` before reading the list.
- adversarial/L12 → applied: `elicit.Answer` has no JSON tags. Task 7's commit says the `@tool-ui` registry is there for spec 2.
- adversarial/L13 → rejected: the spec's receipt is "a check with the answer given" (spec §Cockpit, "After answering"). It is rendered only in the operator's own tab and is never sent anywhere.
- adversarial/L14 → applied: in the size table and in Open point 12.
- adversarial/L15 → applied in the spec's Revisions ("URL elicitation required" is not active in v1.8.0). No task tests that path.

**crosssource**
- crosssource/H1 → applied: the same fix as adversarial/H6.
- crosssource/H2 → changed: codes instead of library text, plus the replay-store test. The library error is not logged, even at debug level, because it quotes the value (spec §Security).
- crosssource/M1 → applied per the operator's decision: an expiry answers cancel, `expired` rides on the resolved frame, and `TestElicitationTimesOutToCancel` stays as it is.
- crosssource/M2 → applied per the ruling: a form of more than one field ends on Review, which alone submits, with each row reopening its step (Task 8).
- crosssource/M3 → applied per the ruling: Use default, and the receipt lists what the server receives (`receivedText`).
- crosssource/M4 → applied: `FromSchema` resolves once and keeps the result, and `^(?=a)` is refused as unrenderable (Task 3).
- crosssource/M5 → applied: one Telegram turn in Task 10 Step 7. The Archestra connection split is recorded as considered and not taken (Open point 9).
- crosssource/L1 → applied: `min_items`, `max_items` and `pattern` (Validate-only, `json:"-"`) on `Field`.
- crosssource/L2 → applied: `everythingForm()` is the reference server's schema.
- crosssource/L3 → rejected for now, needs the operator: the spec's handler contract returns `(*ElicitResult, nil)` in every case. Only a non-compliant server reaches the branch (`mcp/server.go:1753-1756`). Open point 10.
- crosssource/L4 → applied in the spec's Revisions. The plan tests only the two live paths.
- crosssource/L5 → applied: `NousResearch/hermes-agent@7b761da2d tools/mcp_tool_sampling.py:292-295`.
- crosssource/L6 → changed: the countdown reads "Aura cancels in …", and the PRD records the 60 s default. The distinct receipt is not taken: the spec's error table says a server cancel shows "cancelled" (Open point 11).
- crosssource/L7 → applied: Open point 8, and Task 10 Step 9 records any ambiguous decline.
- crosssource/L8 → applied: the required fields come first, in the `required` array's order (Task 3).

**INFO**
- crosssource/I2 → applied: the licence is verified and carried in `THIRD_PARTY_NOTICES.md` (Task 7). Facts not verified drops it.
- crosssource/I3 → applied: Open point 14 tells spec 2 which conventions are Aura's.
- crosssource/I6 → applied: it is the evidence for Open point 7.
- crosssource/I7 → applied: Facts not verified says the SDK's version was not read, and Task 10 records the path taken.
- crosssource/I1, I4, I5 → no change: they confirm the design.
- Found while writing v2, not by a validator:
  - two `ExternalStoreChat` approval suites and the live MCP spec also clicked option buttons;
  - every commit block ran Git Bash's `git.exe`; it now runs in WSL, with the hooks;
  - v1 wired `onElicitation` into `foldReRun`, a branch re-run that has no asker. v2 wires only `foldResumeRun`;
  - `FrameIcon` coloured the clarification icon `text-accent`, a fill token the readability gate refuses. It is now `text-accent-text`.

## File map

| File | Task | Responsibility |
|---|---|---|
| `internal/pausable/clock.go` | 1 | `Clock`: held-time accounting, nested holds |
| `internal/pausable/context.go` | 1 | the pausable deadline context, `WithDeadline`, `WithTimeout`, `Hold` |
| `internal/agent/budget.go` | 2 | the run wallclock reads the held total |
| `internal/agent/llm_agent_tool.go` | 2 | the node timeout becomes pausable |
| `internal/elicit/elicit.go` | 3 | `Question`, `Field`, `Answer`, `Asker`, context seam, refusal codes |
| `internal/elicit/schema.go` | 3 | caps, `DecodeSchema`, `FromSchema` |
| `internal/elicit/validate.go` | 3 | `FieldErrors`, `Validate` |
| `internal/agent/mcptools/bridge_inflight.go` | 4 | calls open per session, the call-context marker |
| `internal/agent/mcptools/elicitation.go` | 4 | handler, fallback consent, configuration |
| `internal/agent/mcptools/elicitation_route.go` | 4 | routing, the held wait, refusals |
| `cmd/aura/elicitation_consent.go` | 4 | the fallback reads `elicit.Question` |
| `cmd/aura/main.go`, `mcp_tools.go`, `runtime_tool_handles.go` | 5 | every mount gets the fallback consent |
| `internal/agui/run_elicitation.go` | 6 | `runQuestions`, the run's asker |
| `internal/agui/server_run_elicitation.go` | 6 | the answer route and the open-questions list |
| `internal/agui/runsession.go`, `server_run_detach.go`, `server.go`, `idempotency_http.go` | 6 | publish, installation, route mount, idempotency inventory |
| `web/components.json` | 7 | the `@tool-ui` registry, for spec 2 |
| `THIRD_PARTY_NOTICES.md` | 7 | Tool UI's MIT notice for the ported markup |
| `web/src/questions/QuestionCard.tsx`, `QuestionOptions.tsx`, `QuestionReceipt.tsx`, `CancelControl.tsx` | 7 | the shared frame, the rows, the receipts, the cancel confirmation |
| `web/src/approvals/InlineApprovalCard.tsx`, `approvalState.ts` | 7 | the ask_user adapter |
| `web/src/i18n/resources.questions.ts` | 7 | the copy, in en and it, Task 8's included |
| `web/stryker.config.json`, `web/vitest.stryker.config.ts` | 7, 8 | the mutated files, and the suites Stryker runs against them |
| `web/src/chat/sseAdapter_elicitation.ts`, and the `onElicitation` plumbing in `sseAdapter.ts`, `sseResume.ts`, `ExternalStoreChat*.ts(x)` | 8 | frame parsing from the pump, and the open-forms fetch on reattach |
| `web/src/questions/useThreadElicitations.ts`, `elicitationApi.ts`, `elicitationSteps.ts` | 8 | the thread's forms, the run's routes, the step logic |
| `web/src/questions/FieldInput.tsx`, `ElicitationHeader.tsx`, `ElicitationReview.tsx`, `useCountdown.ts`, `ElicitationCard.tsx` | 8 | the MCP form adapter |
| `scripts/critical_mutation_gate.py`, `.github/workflows/ci.yml` | 9 | the two new Go mutation scopes |

---

### Task 1: `internal/pausable`

**Files:**
- Create: `internal/pausable/clock.go`, `internal/pausable/context.go`
- Create: `internal/pausable/pausable_test.go`, `internal/pausable/main_test.go`
- Modify: `scripts/coverage_package_policy.json`, adding one entry between `internal/packs` and `internal/pgnumeric`.

**Interfaces:**
- Produces:
  - `pausable.Clock`, with `pausable.NewClock(now func() time.Time) *Clock` (a nil `now` means `time.Now`) and `(*Clock).Held() time.Duration` (nil-safe; 0 on a nil clock);
  - `pausable.WithDeadline(parent context.Context, deadline time.Time, clock *Clock) (context.Context, context.CancelFunc)`. A nil clock gets a clock of its own. The context ends at `deadline + clock.Held()`, reports that deadline (or the parent's, if earlier) from `Deadline()`, and reports `context.DeadlineExceeded` to itself and to every child when it expires;
  - `pausable.WithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc)`;
  - `pausable.Hold(ctx context.Context) (release func())`. It holds every distinct clock above ctx. Holds nest, release is idempotent, and on a context with no pausable deadline it is a no-op.

**Why not a wrapper over `context.WithDeadline`:**
- A standard deadline cannot move.
- A context cancelled through a `CancelFunc` reports `context.Canceled` to its children. Code that classifies a timeout reads `DeadlineExceeded` from derived contexts: the obs boundary, the node-timeout test and `bridge_call`'s tests.
- So `deadlineCtx` implements `context.Context` itself, plus the `AfterFunc(func()) func() bool` method. `context.propagateCancel` then attaches its **direct** standard children through that method, and hands them this context's own error.
  - Only direct children: `propagateCancel` checks the immediate parent alone (go1.27.1 `context.go:508`). In production a `context.WithValue` layer usually sits in between (`tools.WithToolCallContext`, `withCallTool`, otel spans), and those children take the standard library's one-goroutine path. That path is correct and does not leak; it just costs a goroutine.

- [ ] **Step 1: Write the failing tests.** Create `internal/pausable/main_test.go`:

```go
package pausable

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
```

Create `internal/pausable/pausable_test.go`:

```go
package pausable

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeNow is a manual time source: the tests move it, never the wall clock, so
// every assertion about held time is exact.
type fakeNow struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeNow() *fakeNow {
	return &fakeNow{t: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)}
}

func (f *fakeNow) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeNow) advance(d time.Duration) {
	f.mu.Lock()
	f.t = f.t.Add(d)
	f.mu.Unlock()
}

func deadlineOf(t *testing.T, ctx context.Context) time.Time {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("a pausable context must report a deadline")
	}
	return deadline
}

type plainKey struct{}

func TestWithTimeoutExpiresLikeAStandardDeadline(t *testing.T) {
	ctx, cancel := WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	child, cancelChild := context.WithCancel(ctx)
	defer cancelChild()

	select {
	case <-child.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the deadline never fired")
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", ctx.Err())
	}
	if !errors.Is(child.Err(), context.DeadlineExceeded) {
		t.Fatalf("child err = %v, want DeadlineExceeded: timeout classification reads derived contexts", child.Err())
	}
}

func TestAHeldDeadlineDoesNotRunDown(t *testing.T) {
	clock := newFakeNow()
	c := NewClock(clock.now)
	start := clock.now()
	ctx, cancel := WithDeadline(context.Background(), start.Add(10*time.Second), c)
	defer cancel()

	release := Hold(ctx)
	clock.advance(30 * time.Second)
	if got := deadlineOf(t, ctx); !got.Equal(start.Add(40 * time.Second)) {
		t.Fatalf("deadline during a 30s hold = start+%v, want start+40s", got.Sub(start))
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("a held context ended: %v", err)
	}
	release()
	if got := c.Held(); got != 30*time.Second {
		t.Fatalf("held = %v, want 30s", got)
	}
	if got := deadlineOf(t, ctx); !got.Equal(start.Add(40 * time.Second)) {
		t.Fatalf("deadline after release = start+%v, want start+40s", got.Sub(start))
	}
}

func TestAReleasedClockPastItsDeadlineExpires(t *testing.T) {
	clock := newFakeNow()
	c := NewClock(clock.now)
	ctx, cancel := WithDeadline(context.Background(), clock.now().Add(time.Second), c)
	defer cancel()

	release := Hold(ctx)
	clock.advance(time.Minute)
	release()
	if err := ctx.Err(); err != nil {
		t.Fatalf("expired although the whole minute was held: %v", err)
	}
	clock.advance(2 * time.Second)
	// A zero-length hold re-arms on release, which is where an expiry is decided.
	Hold(ctx)()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("err = %v one second past the pushed-back deadline, want DeadlineExceeded", ctx.Err())
	}
}

func TestNestedHoldsRunAgainOnlyAfterTheLast(t *testing.T) {
	clock := newFakeNow()
	c := NewClock(clock.now)
	ctx, cancel := WithDeadline(context.Background(), clock.now().Add(time.Hour), c)
	defer cancel()

	outer := Hold(ctx)
	inner := Hold(ctx)
	clock.advance(5 * time.Second)
	inner()
	clock.advance(5 * time.Second)
	if got := c.Held(); got != 10*time.Second {
		t.Fatalf("held = %v while the outer hold is open, want 10s", got)
	}
	outer()
	outer()
	clock.advance(5 * time.Second)
	if got := c.Held(); got != 10*time.Second {
		t.Fatalf("held = %v after the last release, want 10s", got)
	}
}

func TestADeadlineIsNeverShortened(t *testing.T) {
	clock := newFakeNow()
	c := NewClock(clock.now)
	ctx, cancel := WithDeadline(context.Background(), clock.now().Add(time.Hour), c)
	defer cancel()

	last := deadlineOf(t, ctx)
	var open []func()
	for i := range 50 {
		release := Hold(ctx)
		clock.advance(time.Duration(i%7) * time.Second)
		if i%2 == 0 {
			release()
		} else {
			open = append(open, release)
		}
		clock.advance(time.Second)
		now := deadlineOf(t, ctx)
		if now.Before(last) {
			t.Fatalf("step %d: deadline moved back from %v to %v", i, last, now)
		}
		last = now
	}
	for _, release := range open {
		release()
	}
}

func TestHoldReachesEveryPausableAncestor(t *testing.T) {
	clock := newFakeNow()
	run := NewClock(clock.now)
	call := NewClock(clock.now)
	runCtx, cancelRun := WithDeadline(context.Background(), clock.now().Add(time.Hour), run)
	defer cancelRun()
	between := context.WithValue(runCtx, plainKey{}, "a plain context between the two")
	callCtx, cancelCall := WithDeadline(between, clock.now().Add(time.Minute), call)
	defer cancelCall()

	release := Hold(callCtx)
	clock.advance(20 * time.Second)
	release()
	if run.Held() != 20*time.Second || call.Held() != 20*time.Second {
		t.Fatalf("held run=%v call=%v, want 20s each", run.Held(), call.Held())
	}
}

func TestHoldWithoutAPausableDeadlineIsANoOp(t *testing.T) {
	release := Hold(context.Background())
	release()
	release()
	var none *Clock
	if none.Held() != 0 {
		t.Fatal("a nil clock was never held")
	}
}

func TestTheEarlierParentDeadlineWins(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), time.Minute)
	defer cancelParent()
	ctx, cancel := WithTimeout(parent, time.Hour)
	defer cancel()

	want, _ := parent.Deadline()
	if got := deadlineOf(t, ctx); !got.Equal(want) {
		t.Fatalf("deadline = %v, want the parent's %v", got, want)
	}
}

func TestParentCancellationReachesAPausableChild(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := WithTimeout(parent, time.Hour)
	defer cancel()

	cancelParent()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the parent's cancellation never reached the child")
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("err = %v, want Canceled", ctx.Err())
	}
	if ctx.Value(plainKey{}) != nil {
		t.Fatal("an unknown key must fall through to the parent, which does not hold it")
	}
}

func TestADoneParentEndsTheChildAtOnce(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()
	ctx, cancel := WithTimeout(parent, time.Hour)
	defer cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("err = %v right after WithTimeout on a done parent, want Canceled", ctx.Err())
	}
}

func TestCancelEndsTheContextAndItsChildren(t *testing.T) {
	ctx, cancel := WithTimeout(context.Background(), time.Hour)
	child, cancelChild := context.WithCancel(ctx)
	defer cancelChild()

	cancel()
	cancel()
	<-ctx.Done()
	select {
	case <-child.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the child was never cancelled")
	}
	if !errors.Is(child.Err(), context.Canceled) {
		t.Fatalf("child err = %v, want Canceled", child.Err())
	}
}

func TestAfterFuncStopsBeforeTheEndAndRunsAfterIt(t *testing.T) {
	ctx, cancel := WithTimeout(context.Background(), time.Hour)
	pc := ctx.(*deadlineCtx)

	stopped := pc.AfterFunc(func() { t.Error("a stopped AfterFunc ran") })
	if !stopped() {
		t.Fatal("stop before the end must report that it prevented the call")
	}
	ran := make(chan struct{})
	pc.AfterFunc(func() { close(ran) })
	cancel()
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("the AfterFunc never ran")
	}
	late := make(chan struct{})
	lateStop := pc.AfterFunc(func() { close(late) })
	<-late
	if lateStop() {
		t.Fatal("stop after the end must report false")
	}
}
```

- [ ] **Step 2: Run them to verify they fail.** Go: `go test -race -count=1 ./internal/pausable/`.

Expected: build FAIL with `undefined: WithTimeout`, and likewise `NewClock`, `WithDeadline`, `Hold`, `deadlineCtx`.

- [ ] **Step 3: Implement.** Create `internal/pausable/clock.go`:

```go
// Package pausable provides context deadlines that stop while a human answers.
// A held deadline does not run down, and releasing the hold gives back the time
// it lasted. It exists for MCP elicitation: a server's form waits on the operator
// inside a tool call bounded by the call timeout and by the run's wallclock, and
// neither may count the operator's time.
package pausable

import (
	"sync"
	"time"
)

// Clock accounts for the time a set of deadlines spent held. Every context built
// over one Clock moves with it, and a run's Budget reads the same total, so the
// step gate and the context bounding the run cannot disagree.
type Clock struct {
	now func() time.Time

	mu       sync.Mutex
	holds    int
	heldFrom time.Time
	held     time.Duration
	waiting  map[*deadlineCtx]struct{}
}

// NewClock returns a clock that is not held. now is its time source; nil means
// time.Now.
func NewClock(now func() time.Time) *Clock {
	if now == nil {
		now = time.Now
	}
	return &Clock{now: now, waiting: map[*deadlineCtx]struct{}{}}
}

// Held reports how long the clock has been held in total, an open hold included.
// A nil Clock was never held.
func (c *Clock) Held() time.Duration {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.holds == 0 {
		return c.held
	}
	return c.held + c.now().Sub(c.heldFrom)
}

func (c *Clock) isHeld() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.holds > 0
}

// hold stops the clock until release runs. Holds nest: the clock runs again when
// the last one is released. release is idempotent.
func (c *Clock) hold() (release func()) {
	c.mu.Lock()
	if c.holds == 0 {
		c.heldFrom = c.now()
	}
	c.holds++
	c.mu.Unlock()
	var once sync.Once
	return func() { once.Do(c.release) }
}

// release ends one hold. When it is the last, the held time is banked and every
// context over this clock is re-armed for its pushed-back deadline.
func (c *Clock) release() {
	c.mu.Lock()
	c.holds--
	if c.holds > 0 {
		c.mu.Unlock()
		return
	}
	c.held += c.now().Sub(c.heldFrom)
	waiting := make([]*deadlineCtx, 0, len(c.waiting))
	for ctx := range c.waiting {
		waiting = append(waiting, ctx)
	}
	c.mu.Unlock()
	for _, ctx := range waiting {
		ctx.arm()
	}
}

func (c *Clock) watch(ctx *deadlineCtx) {
	c.mu.Lock()
	c.waiting[ctx] = struct{}{}
	c.mu.Unlock()
}

func (c *Clock) forget(ctx *deadlineCtx) {
	c.mu.Lock()
	delete(c.waiting, ctx)
	c.mu.Unlock()
}
```

Create `internal/pausable/context.go`:

```go
package pausable

import (
	"context"
	"sync"
	"time"
)

type ctxKey struct{}

// deadlineCtx ends at its deadline plus the time its clock has been held. It is
// its own context type because the standard library offers neither half: a
// standard deadline cannot move, and a context cancelled through a CancelFunc
// reports context.Canceled to every child, where a passed deadline must report
// context.DeadlineExceeded.
type deadlineCtx struct {
	parent   context.Context
	clock    *Clock
	deadline time.Time
	up       *deadlineCtx

	mu     sync.Mutex
	done   chan struct{}
	err    error
	timer  *time.Timer
	detach func() bool
	afters map[*afterFunc]struct{}
}

type afterFunc struct{ f func() }

// WithDeadline returns a copy of parent that ends at deadline plus the time clock
// has been held. Several contexts may share one clock; a nil clock gets its own.
func WithDeadline(parent context.Context, deadline time.Time, clock *Clock) (context.Context, context.CancelFunc) {
	if clock == nil {
		clock = NewClock(nil)
	}
	up, _ := parent.Value(ctxKey{}).(*deadlineCtx)
	c := &deadlineCtx{
		parent:   parent,
		clock:    clock,
		deadline: deadline,
		up:       up,
		done:     make(chan struct{}),
		afters:   map[*afterFunc]struct{}{},
	}
	clock.watch(c)
	detach := context.AfterFunc(parent, func() { c.cancel(parent.Err()) })
	c.mu.Lock()
	c.detach = detach
	c.mu.Unlock()
	if err := parent.Err(); err != nil {
		c.cancel(err)
	} else {
		c.arm()
	}
	return c, func() { c.cancel(context.Canceled) }
}

// WithTimeout is WithDeadline over a fresh clock, d from now.
func WithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	clock := NewClock(nil)
	return WithDeadline(parent, clock.now().Add(d), clock)
}

// Hold stops every pausable deadline above ctx until release runs; each gets back
// the time the hold lasted. Deadlines that share a clock are held once. release
// is idempotent, and a ctx with no pausable deadline returns a no-op.
func Hold(ctx context.Context) (release func()) {
	var releases []func()
	seen := map[*Clock]bool{}
	for c, _ := ctx.Value(ctxKey{}).(*deadlineCtx); c != nil; c = c.up {
		if !seen[c.clock] {
			seen[c.clock] = true
			releases = append(releases, c.clock.hold())
		}
	}
	return func() {
		for _, r := range releases {
			r()
		}
	}
}

func (c *deadlineCtx) ownDeadline() time.Time {
	return c.deadline.Add(c.clock.Held())
}

// arm schedules the expiry for the current deadline, or expires now. A held clock
// leaves the context waiting: releasing the hold arms it again. The timer calls
// arm too, so a deadline pushed back while the timer ran is re-checked, not trusted.
func (c *deadlineCtx) arm() {
	if c.clock.isHeld() {
		return
	}
	remaining := c.ownDeadline().Sub(c.clock.now())
	if remaining <= 0 {
		c.cancel(context.DeadlineExceeded)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return
	}
	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = time.AfterFunc(remaining, c.arm)
}

func (c *deadlineCtx) cancel(err error) {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return
	}
	c.err = err
	close(c.done)
	if c.timer != nil {
		c.timer.Stop()
	}
	detach, afters := c.detach, c.afters
	c.afters = nil
	c.mu.Unlock()
	if detach != nil {
		detach()
	}
	c.clock.forget(c)
	for a := range afters {
		go a.f()
	}
}

// Deadline reports the deadline as it stands now: pushed back by every hold so
// far, an open one included, and never later than the parent's.
func (c *deadlineCtx) Deadline() (time.Time, bool) {
	own := c.ownDeadline()
	if parent, ok := c.parent.Deadline(); ok && parent.Before(own) {
		return parent, true
	}
	return own, true
}

func (c *deadlineCtx) Done() <-chan struct{} { return c.done }

func (c *deadlineCtx) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *deadlineCtx) Value(key any) any {
	if key == (ctxKey{}) {
		return c
	}
	return c.parent.Value(key)
}

// AfterFunc lets the standard library attach a direct child without a goroutine of
// its own (context.propagateCancel checks only the immediate parent for this
// method) and hands that child this context's own error, DeadlineExceeded
// included. The already-ended branch is reached when the context ends between the
// library's Done check and this call.
func (c *deadlineCtx) AfterFunc(f func()) (stop func() bool) {
	a := &afterFunc{f: f}
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		go f()
		return func() bool { return false }
	}
	c.afters[a] = struct{}{}
	c.mu.Unlock()
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		_, pending := c.afters[a]
		delete(c.afters, a)
		return pending
	}
}
```

Add the coverage entry to `scripts/coverage_package_policy.json`, between the `internal/packs` and `internal/pgnumeric` lines:

```json
    "github.com/chetto1983/aura/internal/pausable": {"mode": "target"},
```

- [ ] **Step 4: Run the package.** Go: `go vet ./internal/pausable/`, then `go test -race -count=1 -cover ./internal/pausable/`, then `golangci-lint run ./internal/pausable/...`.

Expected:
- `ok  github.com/chetto1983/aura/internal/pausable  coverage: 9x.x% of statements`. It must be at least 85% (the backend validator measured 97.6% on this exact code). goleak stays green: every test cancels its contexts, and a cancelled context stops its timer.
- golangci-lint prints `0 issues.` The pre-commit hook runs the same linter on the staged packages, so a finding here would refuse the commit, and `--no-verify` is forbidden.

- [ ] **Step 5: Commit.**

```bash
cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH" LEFTHOOK_BIN="$HOME/go/bin/lefthook"
git add internal/pausable/clock.go internal/pausable/context.go internal/pausable/pausable_test.go internal/pausable/main_test.go
git -c core.hooksPath=.git/hooks commit -F - -- internal/pausable/clock.go internal/pausable/context.go internal/pausable/pausable_test.go internal/pausable/main_test.go scripts/coverage_package_policy.json <<'EOF'
feat(pausable): context deadlines that stop while the operator answers

An MCP server's form waits on the operator inside a tool call that two
clocks bound: the 60 s call timeout and the run's 300 s wallclock. A
form that takes a minute to fill in would kill the call it belongs to.

pausable.WithDeadline builds a context whose deadline moves by the time
its Clock was held, and Hold holds every such deadline above a context.
It is its own context type: a standard deadline cannot move, and a
cancelled context reports Canceled to its children where an expired one
must report DeadlineExceeded. Direct children attach through an
AfterFunc method instead of a goroutine each.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---

### Task 2: The run's clocks honour a hold

**Files:**
- Modify: `internal/agent/budget.go`, in five places:
  - the struct at 49-64;
  - `NewBudget` at 185-195;
  - `ConsumeStep` at 250-252;
  - `Child` at 342-356;
  - `WithDeadline` at 369-374.
- Modify: `internal/agent/llm_agent_tool.go:195-199`, the node timeout.
- Create: `internal/agent/budget_pause_test.go` (package `agent`), `internal/agent/llm_agent_node_hold_test.go` (package `agent_test`).

**Interfaces:**
- Consumes: `pausable.NewClock`, `pausable.WithDeadline`, `pausable.WithTimeout`, `pausable.Hold`, `(*pausable.Clock).Held` (Task 1).
- Produces:
  - `Budget.WithDeadline(parent) (context.Context, context.CancelFunc)`, same signature, now pausable;
  - `ConsumeStep`'s wallclock gate reads `deadlineWallclock + clock.Held()`, and the clock pointer is shared by `Child`.
  - The runner call site at `internal/runner/runner.go:393` is unchanged.

**The agui detached cap stays fixed.** `detachedRunContext` (`internal/agui/server_run_detach.go:40-42`) bounds a detached run at `AURA_AGUI_RUN_MAX_WALLCLOCK_SEC`, 3600 s by default. That is twelve times the 300 s budget it backs up.
- Kept fixed, it is the one bound on a server that asks again and again: each wait is held, so only a fixed clock ends that loop.
- It also bounds the whole-tree pause. `Child` shares the clock, so while one branch waits on a form, parallel branches (`ParallelAgent`, swarm children) keep working with their wallclock stopped. The v2 rulings (`.superpowers/sdd/2026-09-25-elicitation/progress.md`) settle it: this is documented, in the `Budget` comment and the PRD paragraph (Task 10), and no extra cap on held time is added.
- It cuts a legitimate run only when the operator spends more than about 55 minutes answering forms in one turn.
- Task 6 records this in the function's comment.

**The node timeout is the third clock.** `AURA_LOOP_NODE_TIMEOUT_SEC` wraps every tool call in `context.WithTimeout` when set (default off). A fixed timer there would cut every held wait. See Open point 2.

- [ ] **Step 1: Write the failing tests.** Create `internal/agent/budget_pause_test.go`:

```go
package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/pausable"
)

// manualClock drives a Budget's injected clock so held time is exact.
type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func pausableBudget(t *testing.T, wallclockSec int) (*Budget, *manualClock) {
	t.Helper()
	clock := &manualClock{now: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)}
	b, err := NewBudget(BudgetOptions{MaxWallclockSec: &wallclockSec, Now: clock.Now})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	return b, clock
}

func TestBudgetWallclockSkipsHeldTime(t *testing.T) {
	b, clock := pausableBudget(t, 60)
	ctx, cancel := b.WithDeadline(context.Background())
	defer cancel()

	release := pausable.Hold(ctx)
	clock.advance(10 * time.Minute)
	if ok, reason := b.ConsumeStep(); !ok {
		t.Fatalf("step refused (%s) while the operator holds the clock", reason)
	}
	release()
	clock.advance(30 * time.Second)
	if ok, reason := b.ConsumeStep(); !ok {
		t.Fatalf("step refused (%s) after 30s of run time and a 10-minute hold, with a 60s wallclock", reason)
	}
	clock.advance(31 * time.Second)
	if ok, reason := b.ConsumeStep(); ok || reason != "wallclock" {
		t.Fatalf("ConsumeStep = %v %q after 61s of run time, want the wallclock refusal", ok, reason)
	}
}

func TestChildBudgetSeesTheParentsHeldTime(t *testing.T) {
	b, clock := pausableBudget(t, 60)
	child := b.Child(2)
	ctx, cancel := b.WithDeadline(context.Background())
	defer cancel()

	release := pausable.Hold(ctx)
	clock.advance(5 * time.Minute)
	release()
	clock.advance(30 * time.Second)
	if ok, reason := child.ConsumeStep(); !ok {
		t.Fatalf("a sub-agent's budget refused a step (%s): it must share the parent's held time", reason)
	}
}

func TestBudgetDeadlineContextMovesWithAHold(t *testing.T) {
	b, clock := pausableBudget(t, 60)
	ctx, cancel := b.WithDeadline(context.Background())
	defer cancel()

	before, _ := ctx.Deadline()
	release := pausable.Hold(ctx)
	clock.advance(2 * time.Minute)
	release()
	after, _ := ctx.Deadline()
	if got := after.Sub(before); got != 2*time.Minute {
		t.Fatalf("the run context's deadline moved by %v, want the 2m hold", got)
	}
}
```

Create `internal/agent/llm_agent_node_hold_test.go`:

```go
package agent_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/pausable"
)

// heldTool holds its context's clocks for longer than the node timeout, the way
// an MCP call does while the operator fills in a form.
type heldTool struct{ hold time.Duration }

func (heldTool) Spec() tools.Spec {
	return tools.Spec{Name: "held", Summary: "holds its clocks", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (h heldTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	release := pausable.Hold(ctx)
	time.Sleep(h.hold)
	release()
	if err := ctx.Err(); err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Preview: "answered"}, nil
}

func TestRunToolNodeTimeoutStopsWhileHeld(t *testing.T) {
	recordingProvider(t)
	t.Setenv("AURA_LOOP_NODE_TIMEOUT_SEC", "1")

	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(heldTool{hold: 1500 * time.Millisecond})
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "held", `{}`)),
		agenttest.ToolCallTurn(textResponseCall("c2", "done")),
	)
	a := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     fc,
		LLM:        llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
		Registry:   reg,
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "go"}},
	})

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: new(5)})))
	if err != nil {
		t.Fatalf("run errored: %v", err)
	}
	for _, ev := range evs {
		if ti := ev.Actions.ToolInvocation; ti != nil && ti.Event == agent.ToolInvocationEnd && ti.Error != "" {
			t.Fatalf("the 1s node timeout cut a tool that held its clock for 1.5s: %s", ti.Error)
		}
	}
}
```

`recordingProvider`, `collect`, `newIC` and `textResponseCall` are the package's existing test helpers, used the same way by `TestRunToolAppliesNodeTimeout` (`llm_agent_parallel_test.go:246`).

- [ ] **Step 2: Run them to verify they fail.** Go: `go test -race -count=1 -run 'SkipsHeldTime|ParentsHeldTime|MovesWithAHold|StopsWhileHeld' ./internal/agent/`.

Expected FAIL:
- `TestBudgetWallclockSkipsHeldTime`: `step refused (wallclock) while the operator holds the clock`;
- `TestChildBudgetSeesTheParentsHeldTime`: `a sub-agent's budget refused a step (wallclock)`;
- `TestBudgetDeadlineContextMovesWithAHold`: `moved by 0s`;
- `TestRunToolNodeTimeoutStopsWhileHeld`: `context deadline exceeded`.

- [ ] **Step 3: Implement.** In `internal/agent/budget.go`, add `"github.com/chetto1983/aura/internal/pausable"` to the imports.

Replace the doc comment and the whole `Budget` struct (49-64) with the block below. Every field after `clock` is unchanged, but gofmt re-aligns them: the comment above `clock` starts a new alignment section.

```go
// Budget bounds one agent run. The steps counter is shared by pointer across the
// whole tree (D-10), and so is clock; deadlineWallclock and now are shared by
// value; the dedup ring is per-branch (forked by Child, D-09).
type Budget struct {
	steps             *atomic.Int32 // shared step counter (D-10), decrement-then-check-then-restore (D-11)
	deadlineWallclock time.Time     // hard wallclock cap; ConsumeStep refuses new steps past it (D-13)
	// clock banks the time an operator spends answering an MCP elicitation. The
	// wallclock gate and the context WithDeadline builds both push the deadline
	// back by it, so a held run is refused by neither. Child shares it, so a hold
	// stops the wallclock of the whole run tree: parallel branches that keep
	// working while one branch waits on a form run uncounted meanwhile. The
	// detached run's fixed one-hour cap (agui detachedRunContext) bounds that.
	clock          *pausable.Clock
	now            func() time.Time    // injectable clock (W8): tests drive the deadline deterministically; default time.Now
	dedupWindow    int                 // consecutive-repeat threshold (default 3, D-20)
	dedupRing      *dedupRing          // per-branch two-phase dedup state (budget_dedup.go); distinct per Child (D-09)
	branchSoftCap  int                 // passive per-branch fair-share advisory (D-12); 0 = unset (root)
	branchConsumed atomic.Int32        // steps this branch has consumed, for the passive soft-cap check (D-12)
	exemptTools    map[string]struct{} // AURA_LOOP_DEDUP_EXEMPT_TOOLS allowlist (D-19)
	resultCap      int                 // dedup result-preview byte cap (A7)
	nodeTimeout    time.Duration       // optional per-node soft timeout (D-13); 0 = disabled
	softFrac       float64             // AURA_LOOP_BRANCH_SOFT_FRACTION, feeds Child's softCap (D-12)
}
```

In `NewBudget`, add one line to the literal after `now: now,`:

```go
		clock:             pausable.NewClock(now),
```

In `ConsumeStep`, replace the wallclock check:

```go
	if b.now().After(b.deadlineWallclock.Add(b.clock.Held())) {
		return false, "wallclock"
	}
```

In `Child`, add after `now: b.now,`:

```go
		clock:             b.clock, // SHARED pointer: a hold anywhere in the tree stops every branch's wallclock
```

Replace `WithDeadline`:

```go
// WithDeadline derives a context bounded by the budget's wallclock deadline so
// in-flight LLM/tool calls are cancelled end-to-end, not just blocked from new
// steps (D-13). The deadline is pausable and shares the budget's clock: while an
// MCP elicitation waits on the operator it stops, and ConsumeStep reads the same
// held total. The caller owns the returned CancelFunc.
func (b *Budget) WithDeadline(parent context.Context) (context.Context, context.CancelFunc) {
	return pausable.WithDeadline(parent, b.deadlineWallclock, b.clock)
}
```

In `internal/agent/llm_agent_tool.go:195-199`, replace the node-timeout block:

```go
	if d := budget.NodeTimeout(); d > 0 {
		// Pausable like the run's own deadline: an MCP call waiting on the operator
		// holds every clock above it, and a fixed per-node timer would still cut it.
		var cancel context.CancelFunc
		toolCtx, cancel = pausable.WithTimeout(toolCtx, d)
		defer cancel()
	}
```

Add `"github.com/chetto1983/aura/internal/pausable"` to its imports. Keep `"context"`: `context.CancelFunc` still names the type.

- [ ] **Step 4: Run the package.** Go: `go vet ./internal/agent/`, then `go test -race -count=1 ./internal/agent/ ./internal/runner/`.

Expected: `ok` for both. These existing tests must stay green unchanged:
- `TestBudget_WithDeadline_PropagatesCancellation` (`budget_test.go:516`): with nothing held, the deadline is still exactly the budget deadline. It builds a `Budget` literal with no clock, and `pausable.WithDeadline` gives a nil clock one of its own (measured green by the backend validator);
- `TestRunToolAppliesNodeTimeout`: still `context deadline exceeded`;
- `internal/runner`'s `runner_budget_test.go:33`: the turn context still carries a ~7 s deadline.

- [ ] **Step 5: Commit.**

```bash
cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH" LEFTHOOK_BIN="$HOME/go/bin/lefthook"
git add internal/agent/budget_pause_test.go internal/agent/llm_agent_node_hold_test.go
git -c core.hooksPath=.git/hooks commit -F - -- internal/agent/budget.go internal/agent/llm_agent_tool.go internal/agent/budget_pause_test.go internal/agent/llm_agent_node_hold_test.go <<'EOF'
feat(agent): the run's wallclock stops while an operator answers

The run is bounded twice, by Budget.WithDeadline's context and by
ConsumeStep's wallclock gate, and an MCP form that takes a minute would
have tripped both. Both now read one pausable.Clock that Child shares by
pointer, so a hold anywhere in the tree stops every branch. Parallel
branches that keep working meanwhile run uncounted; the Budget comment
says so, and the detached run's fixed cap bounds it.

The per-node tool timeout (AURA_LOOP_NODE_TIMEOUT_SEC, off by default)
becomes pausable too. Set, a fixed timer there would cut every held
wait. The detached run's one-hour outer cap stays fixed on purpose: it
is the one bound on a server that asks again and again.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---

### Task 3: `internal/elicit`

**Files:**
- Create: `internal/elicit/elicit.go`, `internal/elicit/schema.go`, `internal/elicit/validate.go`
- Create: `internal/elicit/elicit_test.go`, `internal/elicit/schema_test.go`, `internal/elicit/validate_test.go`, `internal/elicit/main_test.go`
- Modify: `scripts/coverage_package_policy.json`, adding one entry between `internal/documents/filecard` and `internal/embeddings`.

**Interfaces:**
- Produces:
  - `type Kind string`, with `KindString`, `KindNumber`, `KindInteger`, `KindBoolean`, `KindEnum`;
  - the action constants `ActionAccept = "accept"`, `ActionDecline = "decline"`, `ActionCancel = "cancel"`;
  - the refusal codes `RefusalUnrenderable = "unrenderable"` and `RefusalAmbiguousRun = "ambiguous_run"`;
  - `Field{Name, Title, Description string; Kind Kind; Required bool; Default any; Enum, EnumTitles []string; Multi bool; Format string; Min, Max *float64; MinLength, MaxLength, MinItems, MaxItems *int; Pattern string}`.
    - The JSON keys are snake_case: `name`, `title`, `description`, `kind`, `required`, `default`, `enum`, `enum_titles`, `multi`, `format`, `min`, `max`, `min_length`, `max_length`, `min_items`, `max_items`.
    - `Pattern` is `json:"-"`: only `Validate` reads it, with Go's regexp. A browser's `pattern` attribute is ECMAScript, a different dialect.
  - `Question{ID, Server, Tool, Message string; Fields []Field; Deadline time.Time; Refusal string; Schema *jsonschema.Resolved}`. The JSON keys are `id`, `server`, `tool`, `message`, `fields`, `deadline` and `refusal`; `Schema` is `json:"-"`;
  - `Answer{Action string; Content map[string]any}`, with no JSON tags: nothing marshals it (Task 6's route decodes its own body type);
  - `type Asker interface { Ask(ctx context.Context, q Question) (Answer, error) }`, with `WithAsker(ctx, Asker) context.Context` and `AskerFrom(ctx) Asker` (nil when none);
  - `ErrExpired`;
  - the caps `MaxFields = 20`, `MaxEnumOptions = 50`, `MaxMessageBytes = 2 << 10`, `MaxTitleBytes = 256`, `MaxDescriptionBytes = 1 << 10`, `MaxAnswerBytes = 4 << 10`, `MaxQuestionBytes = 16 << 10`, `MaxOpenQuestions = 4`;
  - `DecodeSchema(raw any) (*jsonschema.Schema, error)`;
  - `FromSchema(server, tool, message string, schema *jsonschema.Schema) (Question, error)`. It orders the fields (the required ones first, in the `required` array's order, then the rest by name) and resolves the schema once;
  - `type FieldErrors map[string]string` (implements `error`). Its values are problem codes and nothing else: `ProblemRequired = "required"`, `ProblemNotAsked = "not_asked"`, `ProblemInvalid = "invalid"`, `ProblemTooShort = "too_short"`, `ProblemTooLong = "too_long"`, `ProblemOutOfRange = "out_of_range"`, `ProblemNotAnOption = "not_an_option"`, `ProblemTooFew = "too_few"`, `ProblemTooMany = "too_many"`, `ProblemFormat = "format"`, `ProblemPattern = "pattern"`. The empty key holds a problem with the answer as a whole;
  - `Validate(q Question, content map[string]any) FieldErrors` (nil when valid).
- The `Asker` contract: `Ask` returns `(answer, nil)` when answered. When ctx ends first it returns `(Answer{}, context.Cause(ctx))`: `ErrExpired` for a deadline, anything else for the call or the run ending. A question with `Refusal` set is shown already resolved, and `Ask` returns a decline at once.

**Why v2 changed this package** (the validators' findings, each pinned below):
- **Codes, never messages.** jsonschema-go v0.4.3 puts the submitted value into its messages: `type: %v has type` (`jsonschema/validate.go:126`), `enum: %v does not equal` (`:145`), `minLength: %q contains` (`:193`), `maxLength` (`:198`), `pattern: %q does not match` (`:203`). The route's 422 body carries `FieldErrors`, and the idempotency layer stores response bodies for 30 days (`internal/agui/idempotency_http.go:23`, `:304-341`). So each field is checked against its projection, and only a code comes back. `TestAProblemNeverQuotesTheAnswer` pins it; Task 6 pins it again through the route.
- **Resolve once, up front.** go-sdk resolves the schema only after the handler (`mcp/client.go:894-897`), and `Resolve` compiles `pattern` with Go's regexp, RE2 (`jsonschema/resolve.go:344-350`). A lookahead such as `^(?=a)` would produce a form the operator fills in and can never submit. `FromSchema` resolves, refuses on failure, and keeps the `*jsonschema.Resolved` for `Validate`.
- **Order.** `RequestedSchema` decodes to a map (`mcp/protocol.go:2139-2145`) and `PropertyOrder` is `json:"-"` (`jsonschema/schema.go:143`), so the server's key order is lost. The spec now puts the required fields first, in the `required` array's order, then the rest by name. With the real reference form this makes `name`, its one required field, step 1 instead of step 8.
- **A byte cap on the whole question.** Every part under its own cap still adds up to about 540 KiB (20 fields × 50 options × 512 B), and each question is a frame in the run's replay ring. `MaxQuestionBytes` restores the 16 KiB bound the old `summariseElicitationSchema` kept (`maxMCPSchemaBytes`, `elicitation.go:271`).
- **Smaller refusals.** Two options with the same value, and a required name the form does not define, are refused: the first cannot be told apart once chosen, the second could never be answered.
- **Item bounds.** `minItems` and `maxItems` are projected, so the card can hold Next until the count is right (Task 8), and `Validate` names `too_few` or `too_many`.
- **`uri`.** A scheme is enough (RFC 3986), so `file:///tmp/a` and `mailto:` addresses pass. v1 also demanded a host.

- [ ] **Step 1: Write the failing tests.** Create `internal/elicit/main_test.go`:

```go
package elicit

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
```

Create `internal/elicit/elicit_test.go`:

```go
package elicit

import (
	"context"
	"testing"
)

type nopAsker struct{}

func (nopAsker) Ask(context.Context, Question) (Answer, error) {
	return Answer{Action: ActionDecline}, nil
}

func TestAskerRidesTheContext(t *testing.T) {
	t.Parallel()
	if AskerFrom(context.Background()) != nil {
		t.Fatal("a bare context carries no asker")
	}
	var asker Asker = nopAsker{}
	if got := AskerFrom(WithAsker(context.Background(), asker)); got != asker {
		t.Fatalf("AskerFrom = %v, want the installed asker", got)
	}
}
```

Create `internal/elicit/schema_test.go`. The fixture is `@modelcontextprotocol/server-everything`'s `trigger-elicitation-request` form, copied verbatim from the file and commit its comment names, so this task exercises what Task 10's E2E sends. The synthetic cases (the order rule, every refusal) have tests of their own:

```go
package elicit

import (
	"slices"
	"strings"
	"testing"
)

// everythingForm is the reference server's trigger-elicitation-request schema,
// verbatim from modelcontextprotocol/servers@baf99300a2c7
// src/everything/tools/trigger-elicitation-request.ts:56-173, as the SDK hands it
// to a client (a map). Task 10's E2E sends exactly this form.
func everythingForm() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"title": "String", "type": "string", "description": "Your full, legal name",
			},
			"check": map[string]any{
				"title": "Boolean", "type": "boolean", "description": "Agree to the terms and conditions",
			},
			"firstLine": map[string]any{
				"title": "String with default", "type": "string",
				"description": "Favorite first line of a story", "default": "It was a dark and stormy night.",
			},
			"email": map[string]any{
				"title": "String with email format", "type": "string", "format": "email",
				"description": "Your email address (will be verified, and never shared with anyone else)",
			},
			"homepage": map[string]any{
				"type": "string", "format": "uri", "title": "String with uri format",
				"description": "Portfolio / personal website",
			},
			"birthdate": map[string]any{
				"title": "String with date format", "type": "string", "format": "date",
				"description": "Your date of birth",
			},
			"integer": map[string]any{
				"title": "Integer", "type": "integer",
				"description": "Your favorite integer (do not give us your phone number, pin, or other sensitive info)",
				"minimum":     1, "maximum": 100, "default": 42,
			},
			"number": map[string]any{
				"title": "Number in range 1-1000", "type": "number",
				"description": "Favorite number (there are no wrong answers)",
				"minimum":     0, "maximum": 1000, "default": 3.14,
			},
			"untitledSingleSelectEnum": map[string]any{
				"type": "string", "title": "Untitled Single Select Enum",
				"description": "Choose your favorite friend",
				"enum":        []any{"Monica", "Rachel", "Joey", "Chandler", "Ross", "Phoebe"},
				"default":     "Monica",
			},
			"untitledMultipleSelectEnum": map[string]any{
				"type": "array", "title": "Untitled Multiple Select Enum",
				"description": "Choose your favorite instruments", "minItems": 1, "maxItems": 3,
				"items": map[string]any{
					"type": "string", "enum": []any{"Guitar", "Piano", "Violin", "Drums", "Bass"},
				},
				"default": []any{"Guitar"},
			},
			"titledSingleSelectEnum": map[string]any{
				"type": "string", "title": "Titled Single Select Enum",
				"description": "Choose your favorite hero",
				"oneOf": []any{
					map[string]any{"const": "hero-1", "title": "Superman"},
					map[string]any{"const": "hero-2", "title": "Green Lantern"},
					map[string]any{"const": "hero-3", "title": "Wonder Woman"},
				},
				"default": "hero-1",
			},
			"titledMultipleSelectEnum": map[string]any{
				"type": "array", "title": "Titled Multiple Select Enum",
				"description": "Choose your favorite types of fish", "minItems": 1, "maxItems": 3,
				"items": map[string]any{"anyOf": []any{
					map[string]any{"const": "fish-1", "title": "Tuna"},
					map[string]any{"const": "fish-2", "title": "Salmon"},
					map[string]any{"const": "fish-3", "title": "Trout"},
				}},
				"default": []any{"fish-1"},
			},
			"legacyTitledEnum": map[string]any{
				"type": "string", "title": "Legacy Titled Single Select Enum",
				"description": "Choose your favorite type of pet",
				"enum":        []any{"pet-1", "pet-2", "pet-3", "pet-4", "pet-5"},
				"enumNames":   []any{"Cats", "Dogs", "Birds", "Fish", "Reptiles"},
				"default":     "pet-1",
			},
		},
		"required": []any{"name"},
	}
}

func fromMap(t *testing.T, raw map[string]any) (Question, error) {
	t.Helper()
	schema, err := DecodeSchema(raw)
	if err != nil {
		t.Fatalf("DecodeSchema: %v", err)
	}
	return FromSchema("everything", "trigger-elicitation-request", "Tell me about you", schema)
}

func fieldNamed(t *testing.T, q Question, name string) Field {
	t.Helper()
	for _, f := range q.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no field %q in %+v", name, q.Fields)
	return Field{}
}

func names(q Question) []string {
	out := make([]string, 0, len(q.Fields))
	for _, f := range q.Fields {
		out = append(out, f.Name)
	}
	return out
}

func TestFromSchemaReadsEveryRestrictedShape(t *testing.T) {
	t.Parallel()
	q, err := fromMap(t, everythingForm())
	if err != nil {
		t.Fatalf("FromSchema: %v", err)
	}
	if q.Server != "everything" || q.Tool != "trigger-elicitation-request" || q.Message != "Tell me about you" {
		t.Fatalf("question header = %+v", q)
	}
	if q.Schema == nil {
		t.Fatal("the resolved schema was not kept")
	}
	// The one required field first, then the rest by name.
	want := []string{
		"name", "birthdate", "check", "email", "firstLine", "homepage", "integer", "legacyTitledEnum",
		"number", "titledMultipleSelectEnum", "titledSingleSelectEnum", "untitledMultipleSelectEnum",
		"untitledSingleSelectEnum",
	}
	if got := names(q); !slices.Equal(got, want) {
		t.Fatalf("field order = %v\nwant %v", got, want)
	}
	f := fieldNamed(t, q, "name")
	if f.Kind != KindString || !f.Required || f.Title != "String" || f.Description != "Your full, legal name" {
		t.Fatalf("name = %+v", f)
	}
	if f := fieldNamed(t, q, "firstLine"); f.Default != "It was a dark and stormy night." || f.Required {
		t.Fatalf("firstLine = %+v, want an optional string with its default", f)
	}
	for name, format := range map[string]string{"email": "email", "homepage": "uri", "birthdate": "date"} {
		if f := fieldNamed(t, q, name); f.Kind != KindString || f.Format != format {
			t.Fatalf("%s = %+v, want format %s", name, f, format)
		}
	}
	if f := fieldNamed(t, q, "integer"); f.Kind != KindInteger || *f.Min != 1 || *f.Max != 100 || f.Default != 42.0 {
		t.Fatalf("integer = %+v", f)
	}
	if f := fieldNamed(t, q, "number"); f.Kind != KindNumber || *f.Min != 0 || *f.Max != 1000 {
		t.Fatalf("number = %+v", f)
	}
	if f := fieldNamed(t, q, "check"); f.Kind != KindBoolean || f.Default != nil {
		t.Fatalf("check = %+v", f)
	}
	f = fieldNamed(t, q, "untitledSingleSelectEnum")
	if f.Kind != KindEnum || f.Multi || len(f.Enum) != 6 || f.EnumTitles != nil || f.Default != "Monica" {
		t.Fatalf("untitledSingleSelectEnum = %+v", f)
	}
	f = fieldNamed(t, q, "untitledMultipleSelectEnum")
	if !f.Multi || strings.Join(f.Enum, ",") != "Guitar,Piano,Violin,Drums,Bass" || *f.MinItems != 1 || *f.MaxItems != 3 {
		t.Fatalf("untitledMultipleSelectEnum = %+v", f)
	}
	f = fieldNamed(t, q, "titledSingleSelectEnum")
	if strings.Join(f.Enum, ",") != "hero-1,hero-2,hero-3" || f.EnumTitles[1] != "Green Lantern" {
		t.Fatalf("titledSingleSelectEnum = %+v", f)
	}
	f = fieldNamed(t, q, "titledMultipleSelectEnum")
	if !f.Multi || f.Enum[2] != "fish-3" || f.EnumTitles[2] != "Trout" || *f.MaxItems != 3 {
		t.Fatalf("titledMultipleSelectEnum = %+v", f)
	}
	if f := fieldNamed(t, q, "legacyTitledEnum"); strings.Join(f.EnumTitles, ",") != "Cats,Dogs,Birds,Fish,Reptiles" {
		t.Fatalf("legacyTitledEnum enumNames = %+v", f)
	}
}

func TestFromSchemaPutsRequiredFieldsFirstInTheirOwnOrder(t *testing.T) {
	t.Parallel()
	q, err := fromMap(t, map[string]any{"type": "object", "properties": map[string]any{
		"alpha": map[string]any{"type": "string"},
		"beta":  map[string]any{"type": "string"},
		"gamma": map[string]any{"type": "string"},
		"delta": map[string]any{"type": "string"},
	}, "required": []any{"gamma", "alpha", "gamma"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(q); !slices.Equal(got, []string{"gamma", "alpha", "beta", "delta"}) {
		t.Fatalf("order = %v, want the required array's order, then the rest by name", got)
	}
}

func TestFromSchemaNilSchemaIsAMessageOnlyForm(t *testing.T) {
	t.Parallel()
	q, err := FromSchema("s", "t", "Confirm?", nil)
	if err != nil || q.Message != "Confirm?" || len(q.Fields) != 0 || q.Schema != nil {
		t.Fatalf("FromSchema(nil) = %+v, %v", q, err)
	}
}

func TestFromSchemaRefusesWhatAFormCannotShow(t *testing.T) {
	t.Parallel()
	prop := func(p map[string]any) map[string]any {
		return map[string]any{"type": "object", "properties": map[string]any{"f": p}}
	}
	many := map[string]any{}
	for i := range MaxFields + 1 {
		many[string(rune('a'+i))] = map[string]any{"type": "string"}
	}
	// Twenty fields, each under every cap of its own, still add up past the
	// question's byte cap.
	heavy := map[string]any{}
	for i := range MaxFields {
		heavy[string(rune('a'+i))] = map[string]any{
			"type": "string", "description": strings.Repeat("d", MaxDescriptionBytes),
		}
	}
	options := make([]any, MaxEnumOptions+1)
	for i := range options {
		options[i] = strings.Repeat("o", i+1)
	}
	overCapName := strings.Repeat("n", MaxTitleBytes+1)
	for name, raw := range map[string]map[string]any{
		"an object field":            prop(map[string]any{"type": "object"}),
		"a format outside the set":   prop(map[string]any{"type": "string", "format": "ipv4"}),
		"a pattern RE2 cannot parse": prop(map[string]any{"type": "string", "pattern": "^(?=a)"}),
		"an array with no items":     prop(map[string]any{"type": "array"}),
		"a non-string enum":          prop(map[string]any{"type": "string", "enum": []any{1, 2}}),
		"two options with one value": prop(map[string]any{"type": "string", "enum": []any{"a", "a"}}),
		"an option with no const": prop(map[string]any{
			"type": "string", "oneOf": []any{map[string]any{"title": "x"}},
		}),
		"too many fields":          {"type": "object", "properties": many},
		"too many options":         prop(map[string]any{"type": "string", "enum": options}),
		"a form over the byte cap": {"type": "object", "properties": heavy},
		"an over-cap title": prop(map[string]any{
			"type": "string", "title": strings.Repeat("t", MaxTitleBytes+1),
		}),
		"an over-cap description": prop(map[string]any{
			"type": "string", "description": strings.Repeat("d", MaxDescriptionBytes+1),
		}),
		"an over-cap option": prop(map[string]any{
			"type": "string", "enum": []any{strings.Repeat("v", MaxTitleBytes+1)},
		}),
		"an over-cap field name": {
			"type": "object", "properties": map[string]any{overCapName: map[string]any{"type": "string"}},
		},
		"a required field the form lacks": {
			"type": "object", "properties": map[string]any{"f": map[string]any{"type": "string"}},
			"required": []any{"g"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			q, err := fromMap(t, raw)
			if err == nil {
				t.Fatalf("FromSchema accepted %s: %+v", name, q)
			}
			if q.Message != "" || q.Fields != nil || q.Schema != nil || q.Server != "everything" {
				t.Fatalf("a refused question must keep only its server and tool: %+v", q)
			}
		})
	}
}

func TestFromSchemaRefusesAnOverCapMessage(t *testing.T) {
	t.Parallel()
	q, err := FromSchema("s", "t", strings.Repeat("m", MaxMessageBytes+1), nil)
	if err == nil || q.Message != "" {
		t.Fatalf("an over-cap message was kept: %v %+v", err, q)
	}
}

func TestDecodeSchemaRefusesWhatIsNotASchema(t *testing.T) {
	t.Parallel()
	if _, err := DecodeSchema(func() {}); err == nil {
		t.Fatal("a value that is not JSON decoded")
	}
	if _, err := DecodeSchema(map[string]any{"type": 5}); err == nil {
		t.Fatal(`{"type": 5} decoded as a schema`)
	}
	if s, err := DecodeSchema(nil); s != nil || err != nil {
		t.Fatalf("DecodeSchema(nil) = %v, %v; want a message-only form", s, err)
	}
}
```

Create `internal/elicit/validate_test.go`:

```go
package elicit

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"
)

func everythingQuestion(t *testing.T) Question {
	t.Helper()
	q, err := fromMap(t, everythingForm())
	if err != nil {
		t.Fatalf("FromSchema: %v", err)
	}
	return q
}

func questionOf(t *testing.T, properties map[string]any) Question {
	t.Helper()
	q, err := fromMap(t, map[string]any{"type": "object", "properties": properties})
	if err != nil {
		t.Fatalf("FromSchema: %v", err)
	}
	return q
}

func TestValidateAcceptsAFullAnswer(t *testing.T) {
	t.Parallel()
	content := map[string]any{
		"name": "Ada Lovelace", "check": true, "firstLine": "Call me Ishmael.",
		"email": "ada@example.com", "homepage": "https://example.com/ada", "birthdate": "1815-12-10",
		"integer": float64(36), "number": 7.5, "untitledSingleSelectEnum": "Ross",
		"untitledMultipleSelectEnum": []any{"Piano", "Bass"}, "titledSingleSelectEnum": "hero-3",
		"titledMultipleSelectEnum": []any{"fish-2"}, "legacyTitledEnum": "pet-2",
	}
	if errs := Validate(everythingQuestion(t), content); errs != nil {
		t.Fatalf("Validate = %v, want nil", errs)
	}
}

func TestValidateGivesEachFailingFieldACode(t *testing.T) {
	t.Parallel()
	errs := Validate(everythingQuestion(t), map[string]any{
		"email":                      "not-an-address",
		"homepage":                   "example.com",
		"birthdate":                  "10/12/1815",
		"integer":                    float64(200),
		"number":                     "seven",
		"check":                      "yes",
		"untitledSingleSelectEnum":   "Janice",
		"untitledMultipleSelectEnum": []any{"Guitar", "Piano", "Violin", "Drums"},
		"titledMultipleSelectEnum":   []any{},
		"firstLine":                  strings.Repeat("c", MaxAnswerBytes+1),
		"stranger":                   "x",
	})
	want := FieldErrors{
		"":                           ProblemNotAsked,
		"name":                       ProblemRequired,
		"email":                      ProblemFormat,
		"homepage":                   ProblemFormat,
		"birthdate":                  ProblemFormat,
		"integer":                    ProblemOutOfRange,
		"number":                     ProblemInvalid,
		"check":                      ProblemInvalid,
		"untitledSingleSelectEnum":   ProblemNotAnOption,
		"untitledMultipleSelectEnum": ProblemTooMany,
		"titledMultipleSelectEnum":   ProblemTooFew,
		"firstLine":                  ProblemTooLong,
	}
	if !maps.Equal(errs, want) {
		t.Fatalf("Validate = %v\nwant       %v", errs, want)
	}
	if !strings.Contains(errs.Error(), "integer: out_of_range") {
		t.Fatalf("Error() = %q, want each field named with its code", errs.Error())
	}
}

func TestValidateChecksLengthsAndPatternsInCharacters(t *testing.T) {
	t.Parallel()
	q := questionOf(t, map[string]any{
		"pin":  map[string]any{"type": "string", "minLength": 4, "maxLength": 4, "pattern": "^[0-9]+$"},
		"nick": map[string]any{"type": "string", "maxLength": 3},
	})
	for value, want := range map[string]string{"123": ProblemTooShort, "12345": ProblemTooLong, "12a4": ProblemPattern} {
		if got := Validate(q, map[string]any{"pin": value})["pin"]; got != want {
			t.Fatalf("pin %q = %q, want %q", value, got, want)
		}
	}
	if errs := Validate(q, map[string]any{"pin": "1234", "nick": "ñoë"}); errs != nil {
		t.Fatalf("three characters in six bytes were refused: %v", errs)
	}
}

func TestValidateChecksDateTimeAndHostlessURIs(t *testing.T) {
	t.Parallel()
	q := questionOf(t, map[string]any{
		"at":   map[string]any{"type": "string", "format": "date-time"},
		"link": map[string]any{"type": "string", "format": "uri"},
	})
	if errs := Validate(q, map[string]any{"at": "2026-09-25T08:00:00.000Z", "link": "file:///tmp/a"}); errs != nil {
		t.Fatalf("a valid date-time and a hostless URI were refused: %v", errs)
	}
	if got := Validate(q, map[string]any{"at": "2026-09-25 08:00"})["at"]; got != ProblemFormat {
		t.Fatalf("an invalid date-time = %q, want %q", got, ProblemFormat)
	}
}

// TestAProblemNeverQuotesTheAnswer pins the 422 rule: a refusal carries a code from
// a closed set, never the validator's text, which quotes the submitted value.
func TestAProblemNeverQuotesTheAnswer(t *testing.T) {
	t.Parallel()
	const secret = "sk-live-0123456789abcdef"
	q := questionOf(t, map[string]any{
		"token": map[string]any{"type": "string", "maxLength": 8},
		"code":  map[string]any{"type": "string", "enum": []any{"a", "b"}},
		"key":   map[string]any{"type": "string", "pattern": "^[a-z]+$"},
	})
	errs := Validate(q, map[string]any{"token": secret, "code": secret, "key": secret, secret: secret})
	encoded, err := json.Marshal(errs)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 4 || strings.Contains(string(encoded), secret) {
		t.Fatalf("the refusal %s quotes the answer, or misses a field", encoded)
	}
	codes := []string{
		ProblemRequired, ProblemNotAsked, ProblemInvalid, ProblemTooShort, ProblemTooLong, ProblemOutOfRange,
		ProblemNotAnOption, ProblemTooFew, ProblemTooMany, ProblemFormat, ProblemPattern,
	}
	for name, problem := range errs {
		if !strings.Contains(strings.Join(codes, " "), problem) {
			t.Fatalf("%q carries %q, which is not a problem code", name, problem)
		}
	}
}

func TestValidateAMessageOnlyForm(t *testing.T) {
	t.Parallel()
	q, err := FromSchema("s", "t", "Confirm?", nil)
	if err != nil {
		t.Fatal(err)
	}
	if errs := Validate(q, nil); errs != nil {
		t.Fatalf("an empty answer to a message-only form = %v", errs)
	}
	if got := Validate(q, map[string]any{"x": float64(1)})[""]; got != ProblemNotAsked {
		t.Fatalf("content for a form that asked for none = %q, want %q", got, ProblemNotAsked)
	}
}

func TestValidateLetsTheLibraryCatchWhatTheProjectionSkips(t *testing.T) {
	t.Parallel()
	q := questionOf(t, map[string]any{
		"n": map[string]any{"type": "number", "multipleOf": 5},
	})
	if got := Validate(q, map[string]any{"n": float64(7)})[""]; got != ProblemInvalid {
		t.Fatalf("7 against multipleOf 5 = %q, want the whole-answer %q", got, ProblemInvalid)
	}
}
```

- [ ] **Step 2: Run them to verify they fail.** Go: `go test -race -count=1 ./internal/elicit/`.

Expected: build FAIL with `undefined: DecodeSchema`, and likewise `FromSchema`, `WithAsker`, `Validate`, `ProblemRequired`.

- [ ] **Step 3: Implement.** Create `internal/elicit/elicit.go`:

```go
// Package elicit is the vocabulary of an MCP server's form elicitation: the
// question a mounted server asks, the answer the operator gives, and the Asker
// seam that carries one from the MCP bridge to a surface that can show it.
// internal/agent/mcptools and internal/agui both import it, so neither has to
// import the other.
package elicit

import (
	"context"
	"errors"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
)

// Kind is a field's type.
type Kind string

// The kinds a Field can take: exactly the set MCP's restricted elicitation schema
// allows.
const (
	KindString  Kind = "string"
	KindNumber  Kind = "number"
	KindInteger Kind = "integer"
	KindBoolean Kind = "boolean"
	KindEnum    Kind = "enum"
)

// The protocol's three actions (MCP ElicitResult.Action).
const (
	ActionAccept  = "accept"
	ActionDecline = "decline"
	ActionCancel  = "cancel"
)

// Why Aura declined a question without asking anyone. A code, not a sentence:
// each surface words it in its own language.
const (
	// RefusalUnrenderable: the form is outside the restricted schema or over a cap.
	RefusalUnrenderable = "unrenderable"
	// RefusalAmbiguousRun: calls from more than one run were open on the session
	// a classic request arrived on, so no single thread could be asked.
	RefusalAmbiguousRun = "ambiguous_run"
)

// ErrExpired is the cause a wait ends with when the question's deadline passed
// before an answer came.
var ErrExpired = errors.New("elicitation expired")

// Field is one top-level property of the requested schema, bounded and ready to show.
type Field struct {
	Name        string   `json:"name"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Kind        Kind     `json:"kind"`
	Required    bool     `json:"required"`
	Default     any      `json:"default,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	EnumTitles  []string `json:"enum_titles,omitempty"`
	Multi       bool     `json:"multi,omitempty"`
	Format      string   `json:"format,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	MinLength   *int     `json:"min_length,omitempty"`
	MaxLength   *int     `json:"max_length,omitempty"`
	MinItems    *int     `json:"min_items,omitempty"`
	MaxItems    *int     `json:"max_items,omitempty"`
	// Pattern stays server-side. Validate checks it with Go's regexp, as the SDK
	// does; a browser's pattern attribute speaks ECMAScript, a different dialect.
	Pattern string `json:"-"`
}

// Question is what a surface shows. Server is the name Aura mounted the server
// under, never a name the server gave itself.
type Question struct {
	ID       string    `json:"id"`
	Server   string    `json:"server"`
	Tool     string    `json:"tool,omitempty"`
	Message  string    `json:"message"`
	Fields   []Field   `json:"fields"`
	Deadline time.Time `json:"deadline"`
	// Refusal, when set, is why Aura declined without asking. The question is
	// still shown, already resolved, so the operator learns that a server asked.
	Refusal string `json:"refusal,omitempty"`
	// Schema is the server's requested schema, resolved once by FromSchema so an
	// answer is checked with the library the SDK checks it with. It never goes on
	// the wire.
	Schema *jsonschema.Resolved `json:"-"`
}

// Answer is the operator's decision. Content rides only an accept.
type Answer struct {
	Action  string
	Content map[string]any
}

// Asker puts a question to an operator and waits for the answer. Ask returns
// (answer, nil) once answered; when ctx ends first it returns context.Cause(ctx),
// which is ErrExpired for a deadline. A refused question is shown resolved and
// answered at once with a decline.
type Asker interface {
	Ask(ctx context.Context, q Question) (Answer, error)
}

type askerKey struct{}

// WithAsker installs the asker of the run ctx belongs to.
func WithAsker(ctx context.Context, asker Asker) context.Context {
	return context.WithValue(ctx, askerKey{}, asker)
}

// AskerFrom returns the run's asker, or nil when the run has no surface to ask on.
func AskerFrom(ctx context.Context) Asker {
	asker, _ := ctx.Value(askerKey{}).(Asker)
	return asker
}
```

Create `internal/elicit/schema.go`:

```go
package elicit

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
)

// The caps on what a server may ask. A server writes every one of these strings
// and they reach a human, so each is bounded. Past a cap the form is declined
// rather than cut, because a cut question can say something the server did not.
const (
	MaxFields           = 20
	MaxEnumOptions      = 50
	MaxMessageBytes     = 2 << 10
	MaxTitleBytes       = 256 // titles, option labels and field names alike: each is shown as a label
	MaxDescriptionBytes = 1 << 10
	MaxAnswerBytes      = 4 << 10 // per string answer, checked by Validate
	// MaxQuestionBytes bounds the whole projected question: every part under its
	// own cap still adds up to about 540 KiB, and each question is a frame in a
	// run's replay ring. It is the bound the old schema summary kept
	// (mcptools maxMCPSchemaBytes).
	MaxQuestionBytes = 16 << 10
	// MaxOpenQuestions bounds the questions one session, and one run, may have open
	// at once. The SDK serves a server's requests concurrently (go-sdk@v1.8.0
	// mcp/client.go:1188-1192), so without it one server could open any number of
	// cards and held clocks, and a run that reaches several servers adds them up.
	MaxOpenQuestions = 4
)

// formats are the string formats the restricted schema allows (go-sdk@v1.8.0
// mcp/client.go:1012-1022 rejects any other before the handler runs).
var formats = []string{"email", "uri", "date", "date-time"}

// DecodeSchema turns an ElicitParams.RequestedSchema, which the SDK hands a client
// as a map, into the typed schema FromSchema reads. A nil schema is a form with a
// message and no fields.
func DecodeSchema(raw any) (*jsonschema.Schema, error) {
	if raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("requested schema: %w", err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("requested schema: %w", err)
	}
	return &schema, nil
}

// FromSchema projects a requested schema into the fields a form shows. The SDK
// hands the schema over as a map, so the server's key order is gone: the required
// fields come first, in the order of the schema's required array (a JSON array,
// so its order survives), then the rest by name.
//
// The schema is resolved here, once, with the call the SDK makes after the handler
// (go-sdk@v1.8.0 mcp/client.go:894-897). A schema it cannot resolve, such as a
// pattern Go's regexp (RE2) cannot compile, could never be accepted, so it is
// refused before anyone is asked. On error the question keeps only the server and
// the tool, so no over-cap text reaches anyone.
func FromSchema(server, tool, message string, schema *jsonschema.Schema) (Question, error) {
	refused := Question{Server: server, Tool: tool}
	if len(message) > MaxMessageBytes {
		return refused, fmt.Errorf("the message is %d bytes, over the %d-byte cap", len(message), MaxMessageBytes)
	}
	q := Question{Server: server, Tool: tool, Message: message, Fields: []Field{}}
	if schema == nil {
		return q, nil
	}
	fields, err := fieldsOf(schema)
	if err != nil {
		return refused, err
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return refused, fmt.Errorf("the schema does not resolve: %w", err)
	}
	q.Fields, q.Schema = fields, resolved
	data, err := json.Marshal(q)
	if err != nil {
		return refused, fmt.Errorf("the form does not encode: %w", err)
	}
	if len(data) > MaxQuestionBytes {
		return refused, fmt.Errorf("the form is %d bytes, over the %d-byte cap", len(data), MaxQuestionBytes)
	}
	return q, nil
}

// fieldsOf reads every property, in the order the form asks them.
func fieldsOf(schema *jsonschema.Schema) ([]Field, error) {
	if len(schema.Properties) > MaxFields {
		return nil, fmt.Errorf("the form has %d fields, over the %d-field cap", len(schema.Properties), MaxFields)
	}
	order, err := fieldOrder(schema)
	if err != nil {
		return nil, err
	}
	fields := make([]Field, 0, len(order))
	for _, name := range order {
		if len(name) > MaxTitleBytes {
			return nil, fmt.Errorf("a field name is %d bytes, over the %d-byte cap", len(name), MaxTitleBytes)
		}
		field, err := fieldOf(name, schema.Properties[name], slices.Contains(schema.Required, name))
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", name, err)
		}
		fields = append(fields, field)
	}
	return fields, nil
}

// fieldOrder is the required fields in the order the schema requires them, then
// the rest by name. A required field the form does not define could never be
// answered, so it is refused.
func fieldOrder(schema *jsonschema.Schema) ([]string, error) {
	order := make([]string, 0, len(schema.Properties))
	for _, name := range schema.Required {
		if _, defined := schema.Properties[name]; !defined {
			return nil, fmt.Errorf("required field %q is not in the form", name)
		}
		if !slices.Contains(order, name) {
			order = append(order, name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(schema.Properties)) {
		if !slices.Contains(order, name) {
			order = append(order, name)
		}
	}
	return order, nil
}

func fieldOf(name string, p *jsonschema.Schema, required bool) (Field, error) {
	if p == nil {
		return Field{}, errors.New("it has no schema")
	}
	if err := capped("title", p.Title, MaxTitleBytes); err != nil {
		return Field{}, err
	}
	if err := capped("description", p.Description, MaxDescriptionBytes); err != nil {
		return Field{}, err
	}
	f := Field{Name: name, Title: p.Title, Description: p.Description, Required: required}
	if len(p.Default) > 0 {
		if err := json.Unmarshal(p.Default, &f.Default); err != nil {
			return Field{}, fmt.Errorf("its default: %w", err)
		}
	}
	switch p.Type {
	case "boolean":
		f.Kind = KindBoolean
	case "number", "integer":
		f.Kind = Kind(p.Type)
		f.Min, f.Max = p.Minimum, p.Maximum
	case "string":
		if len(p.Enum) > 0 || len(p.OneOf) > 0 {
			return enumField(f, p)
		}
		if p.Format != "" && !slices.Contains(formats, p.Format) {
			return Field{}, fmt.Errorf("format %q is not one of %v", p.Format, formats)
		}
		f.Kind, f.Format, f.Pattern = KindString, p.Format, p.Pattern
		f.MinLength, f.MaxLength = p.MinLength, p.MaxLength
	case "array":
		if p.Items == nil {
			return Field{}, errors.New("it is an array with no items")
		}
		f.Multi, f.MinItems, f.MaxItems = true, p.MinItems, p.MaxItems
		return enumField(f, p.Items)
	default:
		return Field{}, fmt.Errorf("type %q is not string, number, integer, boolean or a multi-select array", p.Type)
	}
	return f, nil
}

// enumField reads a single- or multi-select field in the three shapes MCP allows:
// enum (with the legacy enumNames), oneOf, and anyOf of {const, title}.
func enumField(f Field, p *jsonschema.Schema) (Field, error) {
	f.Kind = KindEnum
	switch {
	case len(p.Enum) > 0:
		for _, v := range p.Enum {
			s, ok := v.(string)
			if !ok {
				return Field{}, fmt.Errorf("enum value %v is not a string", v)
			}
			f.Enum = append(f.Enum, s)
		}
		if names, ok := p.Extra["enumNames"].([]any); ok {
			for _, n := range names {
				s, ok := n.(string)
				if !ok {
					return Field{}, fmt.Errorf("enumNames entry %v is not a string", n)
				}
				f.EnumTitles = append(f.EnumTitles, s)
			}
		}
	case len(p.OneOf) > 0 || len(p.AnyOf) > 0:
		for _, entry := range slices.Concat(p.OneOf, p.AnyOf) {
			value, ok := constString(entry)
			if !ok {
				return Field{}, errors.New("an option has no string const")
			}
			f.Enum = append(f.Enum, value)
			f.EnumTitles = append(f.EnumTitles, entry.Title)
		}
	default:
		return Field{}, errors.New("it offers no options")
	}
	if len(f.Enum) > MaxEnumOptions {
		return Field{}, fmt.Errorf("it has %d options, over the %d-option cap", len(f.Enum), MaxEnumOptions)
	}
	// Two options with one value cannot be told apart once chosen.
	if len(slices.Compact(slices.Sorted(slices.Values(f.Enum)))) != len(f.Enum) {
		return Field{}, errors.New("two options have the same value")
	}
	for _, label := range slices.Concat(f.Enum, f.EnumTitles) {
		if err := capped("option", label, MaxTitleBytes); err != nil {
			return Field{}, err
		}
	}
	return f, nil
}

func constString(entry *jsonschema.Schema) (string, bool) {
	if entry == nil || entry.Const == nil {
		return "", false
	}
	s, ok := (*entry.Const).(string)
	return s, ok
}

func capped(what, s string, limit int) error {
	if len(s) > limit {
		return fmt.Errorf("its %s is %d bytes, over the %d-byte cap", what, len(s), limit)
	}
	return nil
}
```

Create `internal/elicit/validate.go`:

```go
package elicit

import (
	"maps"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// The problem codes a refused answer carries: a closed set, never a validator's
// message. jsonschema-go quotes the submitted value in its messages
// (jsonschema-go@v0.4.3 jsonschema/validate.go:126-203), the answer route's 422
// body carries these codes, and the idempotency layer stores response bodies. Each
// surface words a code in its own language.
const (
	ProblemRequired    = "required"
	ProblemNotAsked    = "not_asked"
	ProblemInvalid     = "invalid"
	ProblemTooShort    = "too_short"
	ProblemTooLong     = "too_long"
	ProblemOutOfRange  = "out_of_range"
	ProblemNotAnOption = "not_an_option"
	ProblemTooFew      = "too_few"
	ProblemTooMany     = "too_many"
	ProblemFormat      = "format"
	ProblemPattern     = "pattern"
)

// FieldErrors maps a field's name to the problem code its answer was refused
// with. The empty name holds a problem with the answer as a whole.
type FieldErrors map[string]string

func (e FieldErrors) Error() string {
	parts := make([]string, 0, len(e))
	for _, name := range slices.Sorted(maps.Keys(e)) {
		parts = append(parts, name+": "+e[name])
	}
	return strings.Join(parts, "; ")
}

// Validate checks an accepted answer to q before the SDK does. Each field is
// checked against its projection, so every refusal names its field with a code
// and nothing else. Two of the checks are ones the SDK leaves out: the per-answer
// size cap, and the string formats the library only records as annotations.
//
// Content for a field the form did not ask for is refused rather than passed on.
// It is reported on the empty name, because its key is the client's text. Last,
// the whole answer goes through the resolved schema, the call the SDK makes once
// the handler returns (go-sdk@v1.8.0 mcp/client.go:894-904), so a constraint the
// projection does not model is still caught here and not by the server.
func Validate(q Question, content map[string]any) FieldErrors {
	errs := FieldErrors{}
	for _, f := range q.Fields {
		value, present := content[f.Name]
		switch {
		case !present && f.Required:
			errs[f.Name] = ProblemRequired
		case present:
			if problem := checkValue(f, value); problem != "" {
				errs[f.Name] = problem
			}
		}
	}
	for name := range content {
		if !slices.ContainsFunc(q.Fields, func(f Field) bool { return f.Name == name }) {
			errs[""] = ProblemNotAsked
		}
	}
	if len(errs) > 0 {
		return errs
	}
	if q.Schema != nil && q.Schema.Validate(content) != nil {
		return FieldErrors{"": ProblemInvalid}
	}
	return nil
}

func checkValue(f Field, value any) string {
	switch f.Kind {
	case KindBoolean:
		if _, ok := value.(bool); !ok {
			return ProblemInvalid
		}
	case KindNumber, KindInteger:
		return checkNumber(f, value)
	case KindString:
		s, ok := value.(string)
		if !ok {
			return ProblemInvalid
		}
		return checkString(f, s)
	case KindEnum:
		return checkChoice(f, value)
	}
	return ""
}

// checkNumber reads a number the way encoding/json decodes one, as a float64.
func checkNumber(f Field, value any) string {
	n, ok := value.(float64)
	if !ok || (f.Kind == KindInteger && n != math.Trunc(n)) {
		return ProblemInvalid
	}
	if (f.Min != nil && n < *f.Min) || (f.Max != nil && n > *f.Max) {
		return ProblemOutOfRange
	}
	return ""
}

// checkString counts characters where the schema bounds a length, as JSON Schema
// does, and bytes for Aura's own cap.
func checkString(f Field, s string) string {
	chars := utf8.RuneCountInString(s)
	switch {
	case len(s) > MaxAnswerBytes || (f.MaxLength != nil && chars > *f.MaxLength):
		return ProblemTooLong
	case f.MinLength != nil && chars < *f.MinLength:
		return ProblemTooShort
	case !formatHolds(f.Format, s):
		return ProblemFormat
	case f.Pattern != "" && !patternHolds(f.Pattern, s):
		return ProblemPattern
	}
	return ""
}

// patternHolds matches the way the SDK's library does: Go's regexp, unanchored.
// FromSchema already compiled the pattern once, so an error here can only come
// from a hand-built Field, and it fails closed.
func patternHolds(pattern, s string) bool {
	ok, err := regexp.MatchString(pattern, s)
	return err == nil && ok
}

func checkChoice(f Field, value any) string {
	if !f.Multi {
		if s, ok := value.(string); !ok || !slices.Contains(f.Enum, s) {
			return ProblemNotAnOption
		}
		return ""
	}
	items, ok := value.([]any)
	if !ok {
		return ProblemInvalid
	}
	for _, item := range items {
		if s, ok := item.(string); !ok || !slices.Contains(f.Enum, s) {
			return ProblemNotAnOption
		}
	}
	switch {
	case f.MinItems != nil && len(items) < *f.MinItems:
		return ProblemTooFew
	case f.MaxItems != nil && len(items) > *f.MaxItems:
		return ProblemTooMany
	}
	return ""
}

// formatHolds checks the four formats the restricted schema allows. A uri needs a
// scheme and nothing more, as RFC 3986 does: file:///tmp and mailto:a@b are URIs.
func formatHolds(format, s string) bool {
	switch format {
	case "email":
		addr, err := mail.ParseAddress(s)
		return err == nil && addr.Address == s
	case "uri":
		u, err := url.Parse(s)
		return err == nil && u.Scheme != ""
	case "date":
		_, err := time.Parse(time.DateOnly, s)
		return err == nil
	case "date-time":
		_, err := time.Parse(time.RFC3339, s)
		return err == nil
	default:
		return true
	}
}
```

Add the coverage entry between the `internal/documents/filecard` and `internal/embeddings` lines:

```json
    "github.com/chetto1983/aura/internal/elicit": {"mode": "target"},
```

- [ ] **Step 4: Run the package.** Go: `go vet ./internal/elicit/`, then `go test -race -count=1 -cover ./internal/elicit/`, then `golangci-lint run ./internal/elicit/...`.

Expected:
- `ok … coverage: 9x.x%`, at least 85%. This exact code measured 95.7% under `-race`, goleak green, on a scratch copy of HEAD `61a77c624` (2026-09-25).
- golangci-lint prints `0 issues.` The `Kind*` block carries its own comment because revive's `exported` rule refuses an uncommented exported const block, and the pre-commit hook runs the same linter.
- v1's two unknowns are now measured (backend validator): jsonschema-go keeps `enumNames` in `Extra` (`util.go:365`), and validates `float64(36)` as an integer (`util.go:262-265`).

- [ ] **Step 5: Commit.**

```bash
cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH" LEFTHOOK_BIN="$HOME/go/bin/lefthook"
git add internal/elicit/
git -c core.hooksPath=.git/hooks commit -F - -- internal/elicit/ scripts/coverage_package_policy.json <<'EOF'
feat(elicit): the question an MCP server asks, and the seam that carries it

A neutral package that internal/agent/mcptools and internal/agui both
import, so neither imports the other.

FromSchema projects a server's requested schema into bounded fields:
every restricted type, enum, oneOf and anyOf options, legacy enumNames,
and a multi-select's item bounds. Required fields come first, in the
server's required order: the SDK hands the schema over as a map, so the
key order is gone. The schema is resolved once, up front, because a
pattern Go's regexp cannot compile could never be accepted. Past a cap,
the whole question's 16 KiB included, a form is declined, not cut.

Validate checks an answer field by field and returns problem codes,
never the validator's text: jsonschema-go quotes the submitted value,
and the answer route's 422 body is stored by the idempotency layer.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---
### Task 4: mcptools routes an elicitation to the run that asked

**Files:**
- Create:
  - `internal/agent/mcptools/bridge_inflight.go`
  - `internal/agent/mcptools/elicitation_route.go`
  - `internal/agent/mcptools/elicitation_fixtures_test.go` (the fakes and in-memory fixtures), `elicitation_route_test.go` (where a form goes), `elicitation_wait_test.go` (the held wait). v2's added tests put a single file at 687 lines, so it is split three ways.
- Modify: `internal/agent/mcptools/elicitation.go`, rewritten from line 1 to 295. `ElicitationRequest`, `ElicitationField`, `summariseElicitationSchema`, `askOperatorBounded`, `elicitationPanicError` and `maxElicitationTypeBytes` are deleted.
- Modify: `internal/agent/mcptools/elicitation_test.go`, in these places:
  - `fakeConsent` (15-45);
  - the two summarise tests (246-300);
  - `TestElicitationMessageIsByteCapped` (302-316);
  - `TestElicitationReachesHandlerOverARealSession` (370-395).
  - `TestElicitationTimesOutToCancel` (181-203) stays as it is: an expired wait answers cancel (operator decision, 2026-09-25).
- Modify: `internal/agent/mcptools/bridge_deferral_test.go:190-203`. `captureWarnLogs(t)` becomes `captureLogs(t, level)`, so the new tests can read an Info line without a second copy of the helper. Its five callers change with it.
- Modify: `internal/agent/mcptools/bridge_supervisor.go`:
  - `decodeResult` and its comment (265-279): a result's links are read inside the call's marker;
  - the two `CallTool` sites at 309 and 339.
- Modify: `internal/agent/mcptools/bridge_call.go:41-46`.
- Modify: `cmd/aura/elicitation_consent.go`, `cmd/aura/elicitation_consent_test.go`.
  - They compile against the new `ElicitationConsent` signature, so they ship in the same commit.

**Interfaces:**
- Consumes:
  - `elicit.Question`, `elicit.Answer`, `elicit.Asker`, `elicit.AskerFrom`, `elicit.DecodeSchema`, `elicit.FromSchema`, `elicit.ErrExpired`;
  - `elicit.Action*`, `elicit.Refusal*`, `elicit.Kind*`, `elicit.MaxMessageBytes`, `elicit.MaxOpenQuestions` (Task 3);
  - `pausable.WithTimeout`, `pausable.Hold` (Task 1);
  - `identityctx.IdentityID` (existing).
- Produces:
  - `type ElicitationConsent interface { AskOperator(ctx context.Context, q elicit.Question) (action string, content map[string]any, err error) }`. The fallback now receives the bounded `elicit.Question`. When `Refusal` is set, the question carries only `Server`, `Tool` and `Refusal`, and the consent's answer is ignored.
  - `NewElicitationHandler(server string, consent ElicitationConsent)` keeps its signature. The handler puts a form to `elicit.AskerFrom(<the request's context>)` with the call's clocks held, and falls back to `consent`.
  - A run's asker is reached only through the context of a request made by `MountedServer.CallTool`: the `tools/call` itself, or the `resources/read` of its result links. Task 6 installs it with `elicit.WithAsker`.

**The routing, as implemented:**

| Arrives on | Placed with | Waits on |
|---|---|---|
| a marked request context: the `tools/call` (MRTR, `mrtr.go:73-118`) or the `resources/read` of its links (MRTR covers both, `mrtr.go:76`) | that context's asker | that call |
| the connection's context (classic `elicitation/create`) | the asker of the calls open on `req.Session`, when they all share one asker and one operator identity | every call open on the session, until all have ended |

- **Refused** when calls from more than one run are open (a different asker, or a different identity), or when the form fails `FromSchema`.
  - Every run concerned is told, with `Refusal` set and **none of the server's text**: the form may belong to another conversation, even another operator's (adversarial H1).
  - Each notice goes out on its own goroutine, bounded by the elicitation timeout, so the server hears its decline at once and a slow channel cannot hold the call past its own bound (adversarial M3).
- **Capped** at `elicit.MaxOpenQuestions` questions being decided per session. Past it the request is declined and logged, and nobody is told: a card or a channel message per request would be the flood itself (adversarial H3).
- **No asker:** the fallback consent, on an open call's context (which carries its operator's identity) or on the handler's own.
- **URL mode** is refused first, and so is a timeout `<= 0`.
- **An expired wait answers cancel,** as MCP defines it ("dismissed without making an explicit choice", 2025-11-25 `client/elicitation`), as Hermes (`NousResearch/hermes-agent@7b761da2d tools/approval_prompt.py:325-326`) and Archestra do, and as `TestElicitationTimesOutToCancel` already pins. The reason on the log line still says `expired`.

**Why the connection's context never names a run.** It keeps the values of the context that dialled the session: the streamable client detaches it with `xcontext.Detach`, which keeps values (`go-sdk@v1.8.0 mcp/streamable.go:2074`). A redial runs on the failing call's context (`context.WithoutCancel(parent)`, `bridge_supervisor_redial.go:113`), and an identity pool's first dial on the caller's (`bridge_identity_sessions.go:101`). So the marker is added around the request alone, never before a dial, and a context without the marker is routed by the calls open on its session, never by the asker it happens to carry.

**What the classic path cannot see.** A server that asks after its own call has returned, while another run's call is the one open, is routed to that run. go-sdk v1.8.0 knows which POST a streamable message arrived on and drops it (`mcp/streamable.go:2617-2680`), so no client-side fix exists inside v1.8.0. Task 10's PRD paragraph records the limit.

A test cannot fake the classic path: go-sdk refuses `ServerSession.Elicit` when the client asked for protocol 2026-07-28 (`server.go:1619-1627`). The fixture narrows its server to `2025-11-25`, and the client then falls back to the legacy initialize at that version (`client.go:371-386`). The backend validator measured this fallback green, in memory and over streamable HTTP.

- [ ] **Step 1: Write the failing tests.** Create `internal/agent/mcptools/elicitation_fixtures_test.go`:

```go
package mcptools

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/elicit"
	"github.com/chetto1983/aura/internal/identityctx"
)

// fakeAsker stands in for a run's cockpit: it records every question, takes
// after to answer, and keeps the Asker contract when ctx ends first.
type fakeAsker struct {
	answer elicit.Answer
	after  time.Duration
	panics bool
	asked  chan elicit.Question
	ended  chan error
}

const never = time.Hour

func newFakeAsker(answer elicit.Answer, after time.Duration) *fakeAsker {
	return &fakeAsker{answer: answer, after: after, asked: make(chan elicit.Question, 8), ended: make(chan error, 8)}
}

func acceptName(name string) elicit.Answer {
	return elicit.Answer{Action: elicit.ActionAccept, Content: map[string]any{"name": name}}
}

func (f *fakeAsker) Ask(ctx context.Context, q elicit.Question) (elicit.Answer, error) {
	f.asked <- q
	if f.panics {
		panic("the cockpit blew up")
	}
	if q.Refusal != "" {
		return elicit.Answer{Action: elicit.ActionDecline}, nil
	}
	timer := time.NewTimer(f.after)
	defer timer.Stop()
	select {
	case <-timer.C:
		return f.answer, nil
	case <-ctx.Done():
		f.ended <- context.Cause(ctx)
		return elicit.Answer{}, context.Cause(ctx)
	}
}

func (f *fakeAsker) question(t *testing.T) elicit.Question {
	t.Helper()
	select {
	case q := <-f.asked:
		return q
	case <-time.After(5 * time.Second):
		t.Fatal("the run's asker was never asked")
		return elicit.Question{}
	}
}

func (f *fakeAsker) endedWith(t *testing.T) error {
	t.Helper()
	select {
	case cause := <-f.ended:
		return cause
	case <-time.After(5 * time.Second):
		t.Fatal("the asker's wait never ended")
		return nil
	}
}

// recordingConsent is the fallback as a test sees it: every question it was told,
// with the identity of the context it was told on.
type recordingConsent struct {
	action string
	told   chan toldQuestion
}

type toldQuestion struct {
	identity string
	q        elicit.Question
}

func newRecordingConsent(action string) *recordingConsent {
	return &recordingConsent{action: action, told: make(chan toldQuestion, 8)}
}

func (c *recordingConsent) AskOperator(ctx context.Context, q elicit.Question) (string, map[string]any, error) {
	c.told <- toldQuestion{identity: identityctx.IdentityID(ctx), q: q}
	return c.action, nil, nil
}

func (c *recordingConsent) next(t *testing.T) toldQuestion {
	t.Helper()
	select {
	case got := <-c.told:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("the fallback was never told")
		return toldQuestion{}
	}
}

// none checks the fallback stays silent. The refusal notices travel on their own
// goroutines, so it waits a moment before believing it.
func (c *recordingConsent) none(t *testing.T) {
	t.Helper()
	select {
	case got := <-c.told:
		t.Fatalf("the fallback was told %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func nameForm() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"name": map[string]any{"type": "string", "title": "Name"}},
		"required":   []any{"name"},
	}
}

func textResult(format string, args ...any) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf(format, args...)}}}
}

// greeting is what every fixture answers once it has a reply: the action, or
// the name when the operator accepted.
func greeting(reply *sdkmcp.ElicitResult) *sdkmcp.CallToolResult {
	if reply.Action != elicit.ActionAccept {
		return textResult("%s", reply.Action)
	}
	return textResult("hello %v", reply.Content["name"])
}

// mrtrAsk elicits the way a 2026-07-28 server must: it returns InputRequests, and
// the client calls the tool again with the reply (go-sdk@v1.8.0 mcp/mrtr.go).
func mrtrAsk(message string, form map[string]any) sdkmcp.ToolHandler {
	return func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		if reply, ok := req.Params.InputResponses["who"].(*sdkmcp.ElicitResult); ok {
			return greeting(reply), nil
		}
		return &sdkmcp.CallToolResult{InputRequests: sdkmcp.InputRequestMap{
			"who": &sdkmcp.ElicitParams{Mode: "form", Message: message, RequestedSchema: form},
		}}, nil
	}
}

// floodAsk asks n forms in one round, the way an errant server floods a client,
// and reports how many it saw declined.
func floodAsk(n int) sdkmcp.ToolHandler {
	return func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		if len(req.Params.InputResponses) > 0 {
			declined := 0
			for _, reply := range req.Params.InputResponses {
				if r, ok := reply.(*sdkmcp.ElicitResult); ok && r.Action == elicit.ActionDecline {
					declined++
				}
			}
			return textResult("declined %d", declined), nil
		}
		asks := sdkmcp.InputRequestMap{}
		for i := range n {
			asks[fmt.Sprintf("q%d", i)] = &sdkmcp.ElicitParams{Mode: "form", Message: "again", RequestedSchema: nameForm()}
		}
		return &sdkmcp.CallToolResult{InputRequests: asks}, nil
	}
}

// classicAsk elicits with a server-to-client request made while its call is open.
func classicAsk(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	reply, err := req.Session.Elicit(ctx, &sdkmcp.ElicitParams{Mode: "form", Message: "what is your name", RequestedSchema: nameForm()})
	if err != nil {
		return nil, err
	}
	return greeting(reply), nil
}

// classicOnly narrows a fixture to the last protocol that allows a classic
// elicitation/create (go-sdk@v1.8.0 mcp/server.go:1619-1627).
var classicOnly = &sdkmcp.ServerOptions{SupportedProtocolVersions: []string{"2025-11-25"}}

// elicitingMount serves handlers from an in-memory fixture, mounted with the
// elicitation handler production installs.
func elicitingMount(t *testing.T, opts *sdkmcp.ServerOptions, consent ElicitationConsent, handlers map[string]sdkmcp.ToolHandler) (*MountedServer, *sdkmcp.ServerSession) {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, opts)
	for name, handler := range handlers {
		server.AddTool(mustTool(name, "Elicits.", nil, nil), handler)
	}
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	srv := NewMountedServer("fixture", nil)
	o := mcpSessionOptionsFor(srv)
	o.Elicitation = NewElicitationHandler("fixture", consent)
	session, err := connectClient(ctx, clientTransport, o)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	srv.Attach(session)
	return srv, serverSession
}

func declining() *recordingConsent { return newRecordingConsent(elicit.ActionDecline) }

// holdingTool keeps its call open until release closes, so a second run can
// share the session.
func holdingTool(entered, release chan struct{}) sdkmcp.ToolHandler {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return textResult("held"), nil
	}
}

// sharedSession runs a call that stays open on the fixture's session under first,
// then asks for a name under second, and returns what the second call got.
func sharedSession(t *testing.T, consent ElicitationConsent, first, second context.Context) string {
	t.Helper()
	entered, release := make(chan struct{}), make(chan struct{})
	srv, _ := elicitingMount(t, classicOnly, consent, map[string]sdkmcp.ToolHandler{
		"hold": holdingTool(entered, release), "ask_name": classicAsk,
	})
	held := make(chan error, 1)
	go func() {
		_, err := srv.CallToolText(first, "hold", nil)
		held <- err
	}()
	<-entered
	got, err := srv.CallToolText(second, "ask_name", nil)
	close(release)
	if heldErr := <-held; heldErr != nil {
		t.Fatalf("the held call failed: %v", heldErr)
	}
	if err != nil {
		t.Fatalf("CallToolText: %v", err)
	}
	return got
}

// assertBareRefusal checks an ambiguous-run notice carries none of the server's
// text: the form may belong to another conversation, even another operator's.
func assertBareRefusal(t *testing.T, who string, q elicit.Question) {
	t.Helper()
	if q.Refusal != elicit.RefusalAmbiguousRun || q.Message != "" || q.Fields != nil || q.Server != "fixture" {
		t.Fatalf("%s was shown %+v, want the ambiguous-run refusal with none of the server's text", who, q)
	}
}

// bridgedFormTool mounts one MRTR form tool and bridges it the way a registry
// sees it, so Execute runs the real call bound (bridge_call.go).
func bridgedFormTool(t *testing.T) tools.Tool {
	t.Helper()
	srv, _ := elicitingMount(t, nil, declining(), map[string]sdkmcp.ToolHandler{"ask_name": mrtrAsk("what is your name", nameForm())})
	bridged, err := bridgeDefault(context.Background(), "forms", srv)
	if err != nil || len(bridged) != 1 {
		t.Fatalf("bridgeDefault = %d tools, %v", len(bridged), err)
	}
	return bridged[0]
}

// runCtx is a run's tool-call context, bounded at 5 s so a regression fails the
// test instead of hanging it on a never-answering asker.
func runCtx(t *testing.T, asker elicit.Asker) context.Context {
	ctx, cancel := context.WithTimeout(tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048), 5*time.Second)
	t.Cleanup(cancel)
	return elicit.WithAsker(ctx, asker)
}
```

Create `internal/agent/mcptools/elicitation_route_test.go`:

```go
package mcptools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/elicit"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

func TestMRTRElicitationAsksTheCallsRun(t *testing.T) {
	asker := newFakeAsker(acceptName("Ada"), 0)
	srv, _ := elicitingMount(t, nil, declining(), map[string]sdkmcp.ToolHandler{"ask_name": mrtrAsk("what is your name", nameForm())})

	got, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "ask_name", nil)
	if err != nil || got != "hello Ada" {
		t.Fatalf("CallToolText = %q, %v; want the operator's answer back from the server", got, err)
	}
	q := asker.question(t)
	if q.Server != "fixture" || q.Tool != "ask_name" || q.Message != "what is your name" {
		t.Fatalf("question = %+v", q)
	}
	if len(q.Fields) != 1 || q.Fields[0].Name != "name" || !q.Fields[0].Required || q.Fields[0].Kind != elicit.KindString {
		t.Fatalf("fields = %+v", q.Fields)
	}
	if time.Until(q.Deadline) <= 0 {
		t.Fatalf("deadline %v is not ahead", q.Deadline)
	}
	session, err := srv.currentSession()
	if err != nil {
		t.Fatal(err)
	}
	if open := inFlight.on(session); len(open) != 0 {
		t.Fatalf("%d calls still recorded open after they returned", len(open))
	}
}

func TestClassicElicitationAsksTheOneRunInFlight(t *testing.T) {
	asker := newFakeAsker(acceptName("Ada"), 0)
	srv, _ := elicitingMount(t, classicOnly, declining(), map[string]sdkmcp.ToolHandler{"ask_name": classicAsk})

	got, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "ask_name", nil)
	if err != nil || got != "hello Ada" {
		t.Fatalf("CallToolText = %q, %v; a classic request with one run in flight goes to that run", got, err)
	}
	if q := asker.question(t); q.Tool != "ask_name" {
		t.Fatalf("tool = %q, want the open call's", q.Tool)
	}
}

// A run with no cockpit keeps today's decline-and-surface, told on the identity of
// the call that made the request.
func TestAFormFromARunWithNoCockpitReachesItsOperatorsChannel(t *testing.T) {
	for name, fixture := range map[string]struct {
		opts    *sdkmcp.ServerOptions
		handler sdkmcp.ToolHandler
	}{
		"mrtr":    {nil, mrtrAsk("what is your name", nameForm())},
		"classic": {classicOnly, classicAsk},
	} {
		t.Run(name, func(t *testing.T) {
			consent := declining()
			srv, _ := elicitingMount(t, fixture.opts, consent, map[string]sdkmcp.ToolHandler{"ask_name": fixture.handler})

			ctx := identityctx.WithIdentityID(context.Background(), "identity-a")
			got, err := srv.CallToolText(ctx, "ask_name", nil)
			if err != nil || got != "decline" {
				t.Fatalf("CallToolText = %q, %v, want the fallback's decline", got, err)
			}
			told := consent.next(t)
			if told.identity != "identity-a" || told.q.Message != "what is your name" || told.q.Refusal != "" {
				t.Fatalf("the fallback was told %+v", told)
			}
		})
	}
}

// Review Focus 3.
func TestClassicElicitationWithTwoRunsInFlightAsksNeither(t *testing.T) {
	consent := newRecordingConsent(elicit.ActionAccept)
	runA, runB := newFakeAsker(acceptName("Ada"), 0), newFakeAsker(acceptName("Bob"), 0)

	got := sharedSession(t, consent,
		elicit.WithAsker(context.Background(), runA), elicit.WithAsker(context.Background(), runB))
	if got != "decline" {
		t.Fatalf("CallToolText = %q; with two runs on the session the server must be declined", got)
	}
	assertBareRefusal(t, "run A", runA.question(t))
	assertBareRefusal(t, "run B", runB.question(t))
	consent.none(t)
}

func TestClassicElicitationSharedWithAChannelRunTellsItsOperatorToo(t *testing.T) {
	consent := newRecordingConsent(elicit.ActionAccept)
	cockpit := newFakeAsker(acceptName("Ada"), 0)

	got := sharedSession(t, consent,
		identityctx.WithIdentityID(context.Background(), "identity-b"), elicit.WithAsker(context.Background(), cockpit))
	if got != "decline" {
		t.Fatalf("CallToolText = %q, want decline", got)
	}
	assertBareRefusal(t, "the cockpit run", cockpit.question(t))
	told := consent.next(t)
	if told.identity != "identity-b" {
		t.Fatalf("the channel run's notice went to %q", told.identity)
	}
	assertBareRefusal(t, "the channel run", told.q)
}

// Two runs with no cockpit share a nil asker, and the identity is what tells them
// apart.
func TestClassicElicitationWithTwoOperatorsOnOneSessionTellsEachWithoutTheForm(t *testing.T) {
	consent := newRecordingConsent(elicit.ActionAccept)
	got := sharedSession(t, consent,
		identityctx.WithIdentityID(context.Background(), "identity-a"),
		identityctx.WithIdentityID(context.Background(), "identity-b"))
	if got != "decline" {
		t.Fatalf("CallToolText = %q; two operators' runs on one session must decline", got)
	}
	told := map[string]bool{}
	for range 2 {
		got := consent.next(t)
		assertBareRefusal(t, got.identity, got.q)
		told[got.identity] = true
	}
	if !told["identity-a"] || !told["identity-b"] {
		t.Fatalf("told %v, want each operator once", told)
	}
}

func TestClassicElicitationOutsideAnyCallFallsBack(t *testing.T) {
	consent := declining()
	_, serverSession := elicitingMount(t, classicOnly, consent, map[string]sdkmcp.ToolHandler{"ask_name": classicAsk})

	res, err := serverSession.Elicit(context.Background(), &sdkmcp.ElicitParams{Mode: "form", Message: "anyone there?", RequestedSchema: nameForm()})
	if err != nil || res.Action != elicit.ActionDecline {
		t.Fatalf("Elicit = %+v, %v; want the fallback's decline", res, err)
	}
	if told := consent.next(t); told.q.Message != "anyone there?" || told.q.Server != "fixture" || told.q.Refusal != "" {
		t.Fatalf("fallback saw %+v", told.q)
	}
}

// A form a server asks while its call's result links are read back belongs to the
// same run: the SDK runs it on the resources/read context (mrtr.go:76).
func TestAFormAskedWhileReadingLinksAsksTheSameRun(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	server.AddTool(mustTool("lookup", "Links a resource.", nil, nil), func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.ResourceLink{URI: "fixture://card", Name: "card"}}}, nil
	})
	server.AddResource(&sdkmcp.Resource{URI: "fixture://card", Name: "card"}, func(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
		if reply, ok := req.Params.InputResponses["who"].(*sdkmcp.ElicitResult); ok {
			return &sdkmcp.ReadResourceResult{Contents: []*sdkmcp.ResourceContents{{URI: "fixture://card", Text: fmt.Sprintf("card for %v", reply.Content["name"])}}}, nil
		}
		return &sdkmcp.ReadResourceResult{InputRequests: sdkmcp.InputRequestMap{
			"who": &sdkmcp.ElicitParams{Mode: "form", Message: "whose card?", RequestedSchema: nameForm()},
		}}, nil
	})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	srv := NewMountedServer("fixture", nil)
	o := mcpSessionOptionsFor(srv)
	o.Elicitation = NewElicitationHandler("fixture", declining())
	session, err := connectClient(context.Background(), clientTransport, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	srv.Attach(session)

	asker := newFakeAsker(acceptName("Ada"), 0)
	if _, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "lookup", nil); err != nil {
		t.Fatalf("CallToolText: %v", err)
	}
	if q := asker.question(t); q.Message != "whose card?" || q.Tool != "lookup" {
		t.Fatalf("the links' form reached the run as %+v", q)
	}
}

// An identity-scoped mount recurses into its child's CallTool, so its calls are
// open on the child's session.
func TestAClassicFormThroughAnIdentityScopedMountAsksTheRun(t *testing.T) {
	handler := NewElicitationHandler("remote", declining())
	connect := func(_ context.Context, hctx context.Context, o mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
		server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, classicOnly)
		server.AddTool(mustTool("ask_name", "Elicits.", nil, nil), classicAsk)
		clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
		serverSession, err := server.Connect(context.Background(), serverTransport, nil)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { _ = serverSession.Close() })
		o.Elicitation = handler
		return connectClient(hctx, clientTransport, o)
	}
	parent := NewMountedServer("remote", nil)
	parent.identityPool = newIdentitySessionPool(parent, connect, t.Context())
	t.Cleanup(func() { _ = parent.Close() })
	identity := identityctx.WithIdentityID(t.Context(), "identity-a")
	_, advertised, err := parent.identityPool.openInitial(identity)
	if err != nil {
		t.Fatalf("open initial: %v", err)
	}
	parent.trackAcceptedTools(advertised)

	asker := newFakeAsker(acceptName("Ada"), 0)
	got, err := parent.CallToolText(elicit.WithAsker(identity, asker), "ask_name", nil)
	if err != nil || got != "hello Ada" {
		t.Fatalf("CallToolText = %q, %v; the call open on the child's session must place the form", got, err)
	}
}

// The read-only redial reissues the call at
// bridge_supervisor.go:339, and that attempt must be marked too.
func TestAReissuedReadOnlyCallStillRoutesItsForm(t *testing.T) {
	readOnly := &sdkmcp.ToolAnnotations{ReadOnlyHint: true}
	fixture := &scriptedOpen{t: t, build: func() *sdkmcp.Server {
		server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
		server.AddTool(mustTool("ask_name", "Elicits.", nil, readOnly), mrtrAsk("what is your name", nameForm()))
		return server
	}}
	handler := NewElicitationHandler("fixture", declining())
	srv := NewMountedServer("fixture", func(pctx, hctx context.Context, o mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
		o.Elicitation = handler
		return fixture.open(pctx, hctx, o)
	})
	first, err := fixture.open(context.Background(), context.Background(), mcpSessionOptionsFor(srv))
	if err != nil {
		t.Fatal(err)
	}
	srv.Attach(first)
	t.Cleanup(func() { _ = srv.Close() })
	srv.trackBridgedTools(bridgeTools("fixture", srv, []*sdkmcp.Tool{mustTool("ask_name", "Elicits.", nil, readOnly)}, time.Second))
	killLiveSession(t, srv)

	asker := newFakeAsker(acceptName("Ada"), 0)
	got, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "ask_name", nil)
	if err != nil || got != "hello Ada" {
		t.Fatalf("CallToolText = %q, %v; the reissued call's form must reach its run", got, err)
	}
}

// One server cannot open more than elicit.MaxOpenQuestions
// cards at once.
func TestAFloodOfFormsOpensNoMoreThanTheCap(t *testing.T) {
	asker := newFakeAsker(acceptName("Ada"), time.Second)
	srv, _ := elicitingMount(t, nil, declining(), map[string]sdkmcp.ToolHandler{"flood": floodAsk(elicit.MaxOpenQuestions + 2)})

	got, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "flood", nil)
	if err != nil || got != "declined 2" {
		t.Fatalf("CallToolText = %q, %v; the requests over the cap must be declined", got, err)
	}
	if n := len(asker.asked); n != elicit.MaxOpenQuestions {
		t.Fatalf("the run was asked %d questions at once, want the cap of %d", n, elicit.MaxOpenQuestions)
	}
}

func TestAnOverCapFormIsShownAsARefusal(t *testing.T) {
	asker := newFakeAsker(acceptName("Ada"), 0)
	srv, _ := elicitingMount(t, nil, declining(), map[string]sdkmcp.ToolHandler{
		"ask_name": mrtrAsk(strings.Repeat("m", elicit.MaxMessageBytes+1), nameForm()),
	})

	got, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "ask_name", nil)
	if err != nil || got != "decline" {
		t.Fatalf("CallToolText = %q, %v, want decline", got, err)
	}
	if q := asker.question(t); q.Refusal != elicit.RefusalUnrenderable || q.Message != "" || q.Fields != nil {
		t.Fatalf("question = %+v; an over-cap form is shown as a refusal carrying none of the server's text", q)
	}
}

func TestURLModeIsRefusedBeforeTheRunIsAsked(t *testing.T) {
	asker := newFakeAsker(acceptName("Ada"), 0)
	ctx := withCallTool(elicit.WithAsker(context.Background(), asker), "login")
	res, err := NewElicitationHandler("fixture", newRecordingConsent(elicit.ActionAccept))(ctx, &sdkmcp.ElicitRequest{
		Params: &sdkmcp.ElicitParams{Mode: "url", URL: "https://evil.example/phish", ElicitationID: "e1"},
	})
	if err != nil || res.Action != elicit.ActionDecline {
		t.Fatalf("handler = %+v, %v; want decline", res, err)
	}
	select {
	case q := <-asker.asked:
		t.Fatalf("the run was asked a url-mode question: %+v", q)
	default:
	}
}

func TestAPanickingAskerDeclines(t *testing.T) {
	logs := captureLogs(t, slog.LevelWarn)
	asker := newFakeAsker(acceptName("Ada"), 0)
	asker.panics = true
	srv, _ := elicitingMount(t, nil, declining(), map[string]sdkmcp.ToolHandler{"ask_name": mrtrAsk("what is your name", nameForm())})

	got, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "ask_name", nil)
	if err != nil || got != "decline" {
		t.Fatalf("CallToolText = %q, %v, want decline", got, err)
	}
	if !strings.Contains(logs.String(), `reason="the run's asker failed"`) {
		t.Fatalf("the panic was not recorded as the asker failing:\n%s", logs.String())
	}
}

// The spec's rule: the action, the server and the field count are logged; what the
// operator typed never is.
func TestTheResolvedLogLineCarriesNoAnswerValue(t *testing.T) {
	logs := captureLogs(t, slog.LevelInfo)
	asker := newFakeAsker(acceptName("Ada-7f3a-secret"), 0)
	srv, _ := elicitingMount(t, nil, declining(), map[string]sdkmcp.ToolHandler{"ask_name": mrtrAsk("what is your name", nameForm())})

	if _, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "ask_name", nil); err != nil {
		t.Fatal(err)
	}
	line := logs.String()
	for _, want := range []string{"mcp elicitation resolved", "action=accept", "fields=1"} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in:\n%s", want, line)
		}
	}
	if strings.Contains(line, "Ada-7f3a-secret") {
		t.Fatalf("the operator's answer reached the log:\n%s", line)
	}
}
```

Create `internal/agent/mcptools/elicitation_wait_test.go`:

```go
package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/elicit"
)

// Review Focus 1: the operator's 600 ms must not count against a 200 ms call bound.
func TestAHeldCallOutlivesItsTimeout(t *testing.T) {
	t.Setenv(envMCPCallTimeoutSec, "0.2")
	tool := bridgedFormTool(t)
	asker := newFakeAsker(acceptName("Ada"), 600*time.Millisecond)

	res, err := tool.Execute(runCtx(t, asker), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v; the call's clock must stop while the operator answers", err)
	}
	if !strings.Contains(res.Preview, "hello Ada") {
		t.Fatalf("preview = %q, want the server's greeting", res.Preview)
	}
}

// Review Focus 2: the call's 200 ms bound is earlier than the question's 1 s one,
// and the question must still expire on its own clock. An expiry answers cancel:
// nobody made an explicit choice.
func TestAnUnansweredQuestionExpiresWhileTheCallIsHeld(t *testing.T) {
	t.Setenv(envMCPCallTimeoutSec, "0.2")
	t.Setenv(envMCPElicitationTimeoutSec, "1")
	tool := bridgedFormTool(t)
	asker := newFakeAsker(acceptName("Ada"), never)

	start := time.Now()
	res, err := tool.Execute(runCtx(t, asker), json.RawMessage(`{}`))
	elapsed := time.Since(start)
	if err != nil || !strings.Contains(res.Preview, "cancel") {
		t.Fatalf("Execute = %q, %v; an expired question cancels, it does not fail the call", res.Preview, err)
	}
	if cause := asker.endedWith(t); !errors.Is(cause, elicit.ErrExpired) {
		t.Fatalf("the wait ended with %v, want elicit.ErrExpired", cause)
	}
	if elapsed < time.Second || elapsed > 5*time.Second {
		t.Fatalf("the question closed after %v, want its own 1s bound", elapsed)
	}
}

func TestTheWaitCancelsWhenTheCallEnds(t *testing.T) {
	asker := newFakeAsker(acceptName("Ada"), never)
	srv, _ := elicitingMount(t, nil, declining(), map[string]sdkmcp.ToolHandler{"ask_name": mrtrAsk("what is your name", nameForm())})
	ctx, cancel := context.WithCancel(elicit.WithAsker(context.Background(), asker))
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := srv.CallToolText(ctx, "ask_name", nil)
		done <- err
	}()
	asker.question(t)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("a cancelled call returned no error")
	}
	if cause := asker.endedWith(t); cause == nil || errors.Is(cause, elicit.ErrExpired) {
		t.Fatalf("the wait ended with %v, want the call's own end, not an expiry", cause)
	}
}

func TestAClassicWaitEndsOnlyWhenEveryCallHasEnded(t *testing.T) {
	first, endFirst := context.WithCancel(context.Background())
	second, endSecond := context.WithCancel(context.Background())
	wait, stop := waitContext(context.Background(), []context.Context{first, second})
	defer stop()

	endFirst()
	select {
	case <-wait.Done():
		t.Fatal("the wait ended with a call still open")
	case <-time.After(50 * time.Millisecond):
	}
	endSecond()
	select {
	case <-wait.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the wait outlived every call")
	}
	if !errors.Is(context.Cause(wait), context.Canceled) {
		t.Fatalf("cause = %v, want the calls' own", context.Cause(wait))
	}
}
```

In `internal/agent/mcptools/elicitation_test.go`:
- Add `"github.com/chetto1983/aura/internal/elicit"` to the imports.
- Use the Edit tool with `replace_all` to replace `elicitActionAccept` with `elicit.ActionAccept`, and likewise `elicitActionDecline` and `elicitActionCancel`.
- Leave `TestElicitationTimesOutToCancel` as it is. v1 of this plan rewrote it to decline; the operator ruled that an expiry cancels, so it keeps pinning today's behaviour.
- Change `fakeConsent`'s `seen` field to `seen chan elicit.Question`. Change its method to:

```go
func (f *fakeConsent) AskOperator(ctx context.Context, q elicit.Question) (string, map[string]any, error) {
	if f.seen != nil {
		select {
		case f.seen <- q:
		default:
		}
	}
	if f.panics {
		panic("consent surface blew up")
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return "", nil, ctx.Err()
		}
	}
	return f.action, f.content, f.err
}
```

Delete `TestSummariseElicitationSchemaIsSortedAndCapped` and `TestSummariseElicitationSchemaDegradesToNoFields`. The function is gone, and `internal/elicit`'s tests cover the projection that replaces it.

Replace `TestElicitationMessageIsByteCapped` with:

```go
// TestAnOverCapFormReachesTheFallbackAsARefusal pins T-45.1-29 on the new path:
// an over-cap message is not cut down and shown, it is refused, and the fallback
// learns why without any of the server's text.
func TestAnOverCapFormReachesTheFallbackAsARefusal(t *testing.T) {
	t.Parallel()
	seen := make(chan elicit.Question, 1)
	res := callHandler(t, "fixture", &fakeConsent{action: elicit.ActionAccept, seen: seen},
		&sdkmcp.ElicitParams{Message: strings.Repeat("z", elicit.MaxMessageBytes+1)})
	if res.Action != elicit.ActionDecline {
		t.Fatalf("action = %q, want decline even though the fallback would accept", res.Action)
	}
	if q := <-seen; q.Refusal != elicit.RefusalUnrenderable || q.Message != "" || q.Server != "fixture" {
		t.Fatalf("fallback saw %+v", q)
	}
}
```

In `TestElicitationReachesHandlerOverARealSession`:
- make `seen := make(chan elicit.Question, 1)`;
- replace the fields check with:

```go
		if len(req.Fields) != 1 || req.Fields[0].Name != "name" || !req.Fields[0].Required || req.Fields[0].Kind != elicit.KindString {
			t.Fatalf("fields = %+v, want one required string field named 'name'", req.Fields)
		}
```

In `internal/agent/mcptools/bridge_deferral_test.go`, replace `captureWarnLogs` (190-203) with the helper below, and use the Edit tool with `replace_all` to turn every `captureWarnLogs(t)` into `captureLogs(t, slog.LevelWarn)` (five callers: 212, 238, 258, 356, 386):

```go
// captureLogs swaps slog's default handler for a text handler over a buffer at
// level, restoring the original on cleanup. Shared by every test in this package
// that asserts on log output (task 2, D-27's reconnect-drift warning, and the
// elicitation resolved line) so the swap-and-restore mechanics live in exactly one
// place, mirroring bridge_trust_test.go's
// TestBridgedToolRefreshSpecWarnsOnMutatingAndRequiredArgChanges, which already
// establishes this pattern for refreshSpec's other warn blocks.
func captureLogs(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: level})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &logs
}
```

In `cmd/aura/elicitation_consent_test.go`:
- Replace the `mcptools` import with `"github.com/chetto1983/aura/internal/elicit"`.
- Replace each `mcptools.ElicitationRequest{` with `elicit.Question{`.
- Replace `TestRenderElicitationPromptFields` and `TestRenderElicitationPromptBoundsAFloodOfFields` with the three tests below.
  - The flood test pinned the "and N more field(s)" line. `FromSchema` now refuses a form with more than 20 fields, so that line cannot be reached and is deleted.
  - The byte bound is what still protects the channel, and the new test pins it at the caps' worst case.
  - The old fields test had a field with no type (`nickname`). A `Field` always has a `Kind` now, so that case is gone.

```go
func TestRenderElicitationPromptFields(t *testing.T) {
	t.Parallel()
	got := renderElicitationPrompt(elicit.Question{
		Server:  "fixture",
		Message: "details please",
		Fields: []elicit.Field{
			{Name: "name", Kind: elicit.KindString, Required: true, Description: "your name"},
			{Name: "age", Kind: elicit.KindNumber},
		},
	})
	for _, want := range []string{"- name (string, required): your name", "- age (number)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

// TestRenderElicitationPromptStaysUnderTheChannelLimit renders the largest form
// FromSchema lets through and checks it still fits the channel's message.
func TestRenderElicitationPromptStaysUnderTheChannelLimit(t *testing.T) {
	t.Parallel()
	fields := make([]elicit.Field, 0, elicit.MaxFields)
	for i := range elicit.MaxFields {
		fields = append(fields, elicit.Field{
			Name:        strings.Repeat(string(rune('a'+i)), elicit.MaxTitleBytes),
			Kind:        elicit.KindString,
			Description: strings.Repeat("d", elicit.MaxDescriptionBytes),
		})
	}
	got := renderElicitationPrompt(elicit.Question{Server: "flood", Message: strings.Repeat("m", elicit.MaxMessageBytes), Fields: fields})
	if len(got) > maxRenderedPromptBytes || !strings.HasSuffix(got, "(truncated)") {
		t.Fatalf("rendered %d bytes ending %q, want <= %d and an announced cut", len(got), got[max(len(got)-20, 0):], maxRenderedPromptBytes)
	}
}

func TestRenderElicitationPromptSaysWhyItRefused(t *testing.T) {
	t.Parallel()
	for refusal, want := range map[string]string{
		"":                         "Aura declined it automatically.",
		elicit.RefusalUnrenderable: "its form cannot be shown",
		elicit.RefusalAmbiguousRun: "more than one conversation",
	} {
		if got := renderElicitationPrompt(elicit.Question{Server: "fixture", Refusal: refusal}); !strings.Contains(got, want) {
			t.Fatalf("refusal %q: missing %q in:\n%s", refusal, want, got)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail.** Go: `go vet ./internal/agent/mcptools/ ./cmd/aura/`.

Expected: build FAIL:
- `undefined: inFlight`, and likewise `withCallTool` and `waitContext`;
- `cannot use consent (variable of type *fakeConsent) as ElicitationConsent value … wrong type for method AskOperator`;
- in `cmd/aura`: `cannot use elicit.Question{…} (value of struct type elicit.Question) as mcptools.ElicitationRequest value in argument to consent.AskOperator`, and the same for `renderElicitationPrompt`. `internal/elicit` already exists after Task 3, so the error is a type mismatch, not an undefined name.

- [ ] **Step 3: Implement.** Create `internal/agent/mcptools/bridge_inflight.go`:

```go
package mcptools

import (
	"context"
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/elicit"
	"github.com/chetto1983/aura/internal/mcp"
)

// bridge_inflight.go remembers which of Aura's requests are open on which session,
// so an elicitation can find the run that asked. The SDK runs a multi-round-trip
// elicitation on the context of the request that asked (go-sdk@v1.8.0
// mcp/mrtr.go:73-118, for tools/call and resources/read alike), and callOnSession
// and readLinksOnSession mark that context. A classic elicitation/create arrives on
// the session's connection context instead, and only the calls open on that
// session can say which run it belongs to.
//
// The connection context is never trusted to name a run. It keeps the values of
// whatever context dialled the session (go-sdk@v1.8.0 mcp/streamable.go:2074
// detaches it with xcontext.Detach), and a redial or an identity pool's first dial
// runs on a call's context (bridge_supervisor_redial.go:113,
// bridge_identity_sessions.go:101). So the marker is added around the request
// alone, never before a dial, and a context without it is routed by the calls open
// on its session, never by the asker it happens to carry.

type callToolKey struct{}

func withCallTool(ctx context.Context, tool string) context.Context {
	return context.WithValue(ctx, callToolKey{}, tool)
}

// callToolFrom reports the tool whose request ctx is, if ctx is one of Aura's
// marked requests.
func callToolFrom(ctx context.Context) (string, bool) {
	tool, ok := ctx.Value(callToolKey{}).(string)
	return tool, ok
}

type inFlightCall struct{ ctx context.Context }

// inFlightCalls is process-wide because its keys already are: a *ClientSession
// belongs to exactly one mount, so two mounts never share an entry.
type inFlightCalls struct {
	mu        sync.Mutex
	bySession map[*sdkmcp.ClientSession]map[*inFlightCall]struct{}
	asking    map[*sdkmcp.ClientSession]int
}

var inFlight = &inFlightCalls{
	bySession: map[*sdkmcp.ClientSession]map[*inFlightCall]struct{}{},
	asking:    map[*sdkmcp.ClientSession]int{},
}

func (f *inFlightCalls) enter(ctx context.Context, session *sdkmcp.ClientSession) (leave func()) {
	call := &inFlightCall{ctx: ctx}
	f.mu.Lock()
	calls := f.bySession[session]
	if calls == nil {
		calls = map[*inFlightCall]struct{}{}
		f.bySession[session] = calls
	}
	calls[call] = struct{}{}
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		delete(calls, call)
		if len(calls) == 0 {
			delete(f.bySession, session)
		}
	}
}

// on returns the contexts of the calls open on session, in no order.
func (f *inFlightCalls) on(session *sdkmcp.ClientSession) []context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	open := make([]context.Context, 0, len(f.bySession[session]))
	for call := range f.bySession[session] {
		open = append(open, call.ctx)
	}
	return open
}

// ask takes one of session's elicit.MaxOpenQuestions slots for a question being
// decided, and done gives it back. ok is false when every slot is taken.
func (f *inFlightCalls) ask(session *sdkmcp.ClientSession) (done func(), ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.asking[session] >= elicit.MaxOpenQuestions {
		return nil, false
	}
	f.asking[session]++
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.asking[session]--
		if f.asking[session] == 0 {
			delete(f.asking, session)
		}
	}, true
}

// callOnSession makes one of MountedServer.CallTool's tools/call attempts, with its
// context marked and recorded for as long as it is open.
func callOnSession(ctx context.Context, session *sdkmcp.ClientSession, name string, args map[string]any) (*sdkmcp.CallToolResult, error) {
	ctx = withCallTool(ctx, name)
	defer inFlight.enter(ctx, session)()
	return session.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: args})
}

// readLinksOnSession reads a result's links back as part of the call that returned
// them: those resources/read requests are the same run's, and a form a server asks
// inside one reaches the handler on their context (mrtr.go:76).
func readLinksOnSession(ctx context.Context, session *sdkmcp.ClientSession, tool string, payload mcp.ToolPayload) mcp.ToolPayload {
	ctx = withCallTool(ctx, tool)
	defer inFlight.enter(ctx, session)()
	return resolveLinks(ctx, session, payload)
}
```

In `internal/agent/mcptools/bridge_supervisor.go`:
- replace the last paragraph of `decodeResult`'s comment and its return (270-279) with:

```go
// A successful result's links are read back on session, the one that made the call
// (bridge_links.go), as part of that call (bridge_inflight.go), so CallToolText
// callers pay for those reads too; no server they call returns links today.
func (s *MountedServer) decodeResult(ctx context.Context, session *sdkmcp.ClientSession, name string, res *sdkmcp.CallToolResult) (mcp.ToolPayload, error) {
	payload, isErr := mcp.DecodeToolPayload(res)
	if isErr {
		return mcp.ToolPayload{}, mcp.DecodeToolCallError(s.name, name, payload.Text)
	}
	return readLinksOnSession(ctx, session, name, payload), nil
}
```

- line 309 becomes `res, callErr = callOnSession(ctx, session, name, args)`;
- line 339 becomes `res, callErr := callOnSession(ctx, retry, name, args)`.
- An identity-scoped mount recurses into its child's `CallTool` at 299, so its calls register on the child's session with no further change. `TestAClassicFormThroughAnIdentityScopedMountAsksTheRun` pins it, and `TestAReissuedReadOnlyCallStillRoutesItsForm` pins line 339.
- The file stays at 500 lines.

In `internal/agent/mcptools/bridge_call.go`, replace lines 41-46 with the block below, and add `"github.com/chetto1983/aura/internal/pausable"` after the `internal/obs` import:

```go
	callCtx := ctx
	cancel := func() {}
	if b.callTimeout > 0 {
		// Pausable: while the server's form waits on the operator the call is held,
		// and the operator's time does not count against its bound.
		callCtx, cancel = pausable.WithTimeout(ctx, b.callTimeout)
	}
	defer cancel()
```

Replace `internal/agent/mcptools/elicitation.go` in full:

```go
package mcptools

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/elicit"
	"github.com/chetto1983/aura/internal/obs"
	"github.com/chetto1983/aura/internal/redact"
)

// elicitation.go answers SEP-2322 server-initiated elicitation. A form a mounted
// server asks for inside a cockpit run is put to that run's operator
// (elicitation_route.go), and the call waits, with its clocks held, for the
// answer. Anything Aura cannot place in one run falls back to
// decline-and-surface: the ElicitationConsent the composition root wires declines
// and tells the operator on their channel.
//
// Aura writes the handler body and nothing else. The SDK owns the multi-round-trip
// loop (go-sdk@v1.8.0 mcp/mrtr.go:73-118), the classic elicitation/create request,
// and the schema checks made before and after the handler (mcp/client.go:869-920).

// elicitModeURL is the one mode Aura refuses without consulting anyone. Opening a
// server-supplied URL that the operator reads as Aura-sanctioned is a phishing
// primitive (T-45.1-31), so the URL is never rendered anywhere, not even in a log.
// Hermes declines it for the same reason (NousResearch/hermes-agent@7b761da2d
// tools/mcp_tool_sampling.py:292-295).
const elicitModeURL = "url"

const envMCPElicitationTimeoutSec = "AURA_MCP_ELICITATION_TIMEOUT_SEC"

// defaultElicitationTimeout matches Hermes' default (tools/mcp_tool_sampling.py:262
// at the same commit) and the value recorded in 45.1-06-SUMMARY.md.
//
// A configured value <= 0 DISABLES elicitation: the handler declines at once. It
// does NOT mean "wait forever", the reading a future reader will assume and the
// dangerous one: an unbounded wait holds the call, and with it the turn, until the
// server gives up.
const defaultElicitationTimeout = 300 * time.Second

// maxLoggedValueBytes caps a server- or surface-supplied value that reaches a log
// line: an unrecognised action, a malformed timeout.
const maxLoggedValueBytes = 32

// maxLoggedErrorBytes caps the error on the resolved line. A refusal's error is
// FromSchema's, and it quotes server-supplied names and options.
const maxLoggedErrorBytes = 256

// The boundary reuses the MCP call counter with its own operation value rather
// than registering a second instrument: obs exposes emission ONLY through
// Boundary, whose outcome is derived from the error (nil→success,
// context.Canceled→canceled, context.DeadlineExceeded→timeout, else error). The
// action and the reason ride the structured log line instead, which keeps a
// server-supplied string out of a metric dimension.
var mcpElicitationBoundary = obs.NewGlobalBoundary("github.com/chetto1983/aura/internal/agent/mcptools", obs.BoundaryConfig{
	Operation: "mcp_elicitation", ToolClass: obs.ToolClassMCP, Transport: "in_process",
	Count: obs.MCPCallsID, Duration: obs.MCPDurationID,
})

// ElicitationConsent is the fallback for a request no run can be asked: the
// composition root's decline-and-surface. It is declared here, consumer-side, so
// this package imports neither internal/runner nor internal/channels.
//
// It receives the bounded elicit.Question, never the SDK params, so a
// composition-root implementation cannot reach the raw schema or a URL even by
// mistake. Returning anything other than "accept" yields a non-accept result, and
// an error, a panic or a surface that never returns all decline or cancel.
type ElicitationConsent interface {
	AskOperator(ctx context.Context, q elicit.Question) (action string, content map[string]any, err error)
}

// elicitOutcome is what the handler answers and records. err is for
// observability only: the SDK never sees it.
type elicitOutcome struct {
	action  string
	content map[string]any
	fields  int
	reason  string
	err     error
}

// NewElicitationHandler builds the closure mcp.SessionOptions.Elicitation takes.
//
// It returns (*ElicitResult, nil) in EVERY case. An error returned from here
// propagates through fulfillInputRequests (go-sdk@v1.8.0 mcp/mrtr.go:273-305) and
// fails the whole CallTool with an opaque message, where a decline hands the
// server a protocol answer it can respond to.
func NewElicitationHandler(server string, consent ElicitationConsent) func(context.Context, *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error) {
	return func(ctx context.Context, req *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error) {
		ctx, end := mcpElicitationBoundary.Start(ctx)
		out := decideElicitation(ctx, server, consent, req)
		end.End(out.err)
		level := slog.LevelInfo
		if out.err != nil {
			level = slog.LevelWarn
		}
		// The operator's values never reach a log: the action, the server and the
		// field count do.
		slog.Log(ctx, level, "mcp elicitation resolved", "server", redact.Line(server),
			"action", out.action, "fields", out.fields, "reason", out.reason, "err", loggedError(out.err))
		return &sdkmcp.ElicitResult{Action: out.action, Content: out.content}, nil
	}
}

func decideElicitation(ctx context.Context, server string, consent ElicitationConsent, req *sdkmcp.ElicitRequest) elicitOutcome {
	if req == nil || req.Params == nil {
		return elicitOutcome{action: elicit.ActionDecline, reason: "no params"}
	}
	params := req.Params
	if strings.EqualFold(strings.TrimSpace(params.Mode), elicitModeURL) {
		// The URL is deliberately absent from every record. T-45.1-31.
		return elicitOutcome{action: elicit.ActionDecline, reason: "url mode is refused"}
	}
	timeout := configuredElicitationTimeout()
	if timeout <= 0 {
		return elicitOutcome{action: elicit.ActionDecline, reason: "disabled by " + envMCPElicitationTimeoutSec}
	}
	// A server past its open questions is declined without a card or a channel
	// message: telling the operator once per request would be the flood itself.
	done, ok := inFlight.ask(req.Session)
	if !ok {
		return elicitOutcome{action: elicit.ActionDecline, reason: "too many open questions on the session"}
	}
	defer done()

	r := routeFor(ctx, req.Session)
	q, err := questionFor(server, r.tool, params)
	switch {
	case err != nil:
		out := refuse(ctx, r, consent, q, elicit.RefusalUnrenderable, timeout)
		out.err = err
		return out
	case r.mixed:
		return refuse(ctx, r, consent, q, elicit.RefusalAmbiguousRun, timeout)
	case r.asker != nil:
		return askRun(ctx, r, q, timeout)
	default:
		return askFallback(r.fallbackContext(ctx), consent, q, timeout)
	}
}

func questionFor(server, tool string, params *sdkmcp.ElicitParams) (elicit.Question, error) {
	schema, err := elicit.DecodeSchema(params.RequestedSchema)
	if err != nil {
		return elicit.Question{Server: server, Tool: tool}, err
	}
	return elicit.FromSchema(server, tool, params.Message, schema)
}

// answered maps an operator's decision onto the protocol. Content rides only an
// accept, and an action the protocol does not define declines: an unrecognised
// action is not a permissive one.
func answered(a elicit.Answer, fields int) elicitOutcome {
	switch a.Action {
	case elicit.ActionAccept:
		return elicitOutcome{action: elicit.ActionAccept, content: a.Content, fields: fields, reason: "answered"}
	case elicit.ActionDecline, elicit.ActionCancel:
		return elicitOutcome{action: a.Action, fields: fields, reason: "answered"}
	default:
		return elicitOutcome{action: elicit.ActionDecline, fields: fields,
			reason: "unrecognised action " + redact.Line(strconv.Quote(truncateUTF8Bytes(a.Action, maxLoggedValueBytes)))}
	}
}

// loggedError is err as the resolved line records it, redacted and capped like
// every other server-supplied string in this file.
func loggedError(err error) string {
	if err == nil {
		return ""
	}
	return redact.Line(truncateUTF8Bytes(err.Error(), maxLoggedErrorBytes))
}

// configuredElicitationTimeout reads AURA_MCP_ELICITATION_TIMEOUT_SEC, mirroring
// configuredMCPCallTimeout's convention in timeout.go.
//
// A value <= 0 returns 0, which the handler reads as DISABLED, not infinite. An
// unparseable value falls back to the default rather than failing the mount: a
// malformed knob must not stop a server from being usable.
func configuredElicitationTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv(envMCPElicitationTimeoutSec))
	if raw == "" {
		return defaultElicitationTimeout
	}
	sec, err := strconv.Atoi(raw)
	if err != nil {
		slog.Warn("ignoring malformed elicitation timeout",
			"env", envMCPElicitationTimeoutSec, "value", truncateUTF8Bytes(raw, maxLoggedValueBytes))
		return defaultElicitationTimeout
	}
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}
```

Create `internal/agent/mcptools/elicitation_route.go`:

```go
package mcptools

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/elicit"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/pausable"
)

var errElicitationPanic = errors.New("elicitation surface panicked")

// route is where one elicitation goes: the asker of the run whose call it arrived
// in, that call's tool, and the open calls the wait belongs to. mixed means calls
// from more than one run are open, so the request cannot be placed.
type route struct {
	asker elicit.Asker
	tool  string
	calls []context.Context
	mixed bool
}

// runKey is what makes two open calls one run's: the same asker and the same
// operator. Runs with no cockpit (a Telegram turn, a scheduled job) all have a nil
// asker, so the identity is what keeps one operator's form off another's channel.
type runKey struct {
	asker    elicit.Asker
	identity string
}

func runOf(call context.Context) runKey {
	return runKey{asker: elicit.AskerFrom(call), identity: identityctx.IdentityID(call)}
}

// routeFor finds the run an elicitation belongs to. A multi-round-trip request
// arrives on its request's own marked context, which names the run outright. A
// classic elicitation/create arrives on the connection's context and is placed
// only when every call open on its session belongs to one run.
//
// What the classic path cannot see: a server that asks after its own call has
// returned, while another run's call is the one open. go-sdk v1.8.0 reads which
// request a streamable message belongs to and drops it (mcp/streamable.go:2617-2680),
// so no client-side fix exists; the PRD records the limit.
func routeFor(ctx context.Context, session *sdkmcp.ClientSession) route {
	if tool, ok := callToolFrom(ctx); ok {
		return route{asker: elicit.AskerFrom(ctx), tool: tool, calls: []context.Context{ctx}}
	}
	r := route{calls: inFlight.on(session)}
	var run runKey
	for i, call := range r.calls {
		tool, _ := callToolFrom(call)
		switch {
		case i == 0:
			run, r.tool = runOf(call), tool
		case runOf(call) != run:
			r.mixed = true
		}
		if tool != r.tool {
			r.tool = ""
		}
	}
	if !r.mixed {
		r.asker = run.asker
	}
	return r
}

// fallbackContext is the context the fallback consent is asked on: an open call's,
// which carries its operator's identity (every open call shares it, or the route
// is mixed), or the handler's own when no call is open.
func (r route) fallbackContext(ctx context.Context) context.Context {
	if len(r.calls) > 0 {
		return r.calls[0]
	}
	return ctx
}

// askRun puts q to the run's operator and waits. Every clock of the waiting calls
// is held from the moment the question is put until the wait ends, so only the
// operator's time is excluded. The asker is Aura's own and returns as soon as its
// context ends, so it is called in place: what it decided is exactly what the
// server is told, with no race against the deadline.
func askRun(ctx context.Context, r route, q elicit.Question, timeout time.Duration) elicitOutcome {
	releases := make([]func(), 0, len(r.calls))
	for _, call := range r.calls {
		releases = append(releases, pausable.Hold(call))
	}
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	wait, stopWait := waitContext(ctx, r.calls)
	defer stopWait()
	bounded, stop := withExpiry(wait, timeout)
	defer stop()

	q.Deadline = time.Now().Add(timeout)
	answer, err := askRecovered(bounded, r.asker, q)
	switch {
	case err != nil && bounded.Err() != nil:
		return waitEnded(bounded, len(q.Fields))
	case err != nil:
		return elicitOutcome{action: elicit.ActionDecline, fields: len(q.Fields), reason: "the run's asker failed", err: err}
	}
	return answered(answer, len(q.Fields))
}

// askFallback is decline-and-surface. The consent may ignore ctx or never return,
// so it runs on its own goroutine and is abandoned when the wait ends; the
// channel is buffered so that goroutine can still finish.
func askFallback(ctx context.Context, consent ElicitationConsent, q elicit.Question, timeout time.Duration) elicitOutcome {
	if consent == nil {
		return elicitOutcome{action: elicit.ActionDecline, fields: len(q.Fields), reason: "no consent surface wired"}
	}
	bounded, stop := withExpiry(ctx, timeout)
	defer stop()

	type reply struct {
		action  string
		content map[string]any
		err     error
	}
	done := make(chan reply, 1)
	go func() {
		defer func() {
			if recover() != nil {
				done <- reply{err: errElicitationPanic}
			}
		}()
		action, content, err := consent.AskOperator(bounded, q)
		done <- reply{action: action, content: content, err: err}
	}()
	select {
	case got := <-done:
		switch {
		case got.err != nil && bounded.Err() != nil:
			return waitEnded(bounded, len(q.Fields))
		case got.err != nil:
			return elicitOutcome{action: elicit.ActionDecline, fields: len(q.Fields), reason: "the consent surface failed", err: got.err}
		}
		return answered(elicit.Answer{Action: got.action, Content: got.content}, len(q.Fields))
	case <-bounded.Done():
		return waitEnded(bounded, len(q.Fields))
	}
}

// refuse declines a request Aura will not put to anyone and tells every run
// concerned why. The notice carries none of the server's text: a refused form may
// belong to another conversation, even another operator's. Each notice goes out on
// its own goroutine, so the server hears its decline at once and a slow channel
// cannot hold the call past its own bound.
func refuse(ctx context.Context, r route, consent ElicitationConsent, q elicit.Question, why string, timeout time.Duration) elicitOutcome {
	notice := elicit.Question{Server: q.Server, Tool: q.Tool, Refusal: why}
	told := map[runKey]bool{}
	for _, call := range r.calls {
		if run := runOf(call); !told[run] {
			told[run] = true
			go tell(call, run.asker, consent, notice, timeout)
		}
	}
	if len(told) == 0 {
		go tell(ctx, nil, consent, notice, timeout)
	}
	return elicitOutcome{action: elicit.ActionDecline, fields: len(q.Fields), reason: "refused: " + why}
}

// tell delivers a refusal notice to a run's asker, or through the fallback when the
// run has none. It outlives the call on purpose (WithoutCancel), because the call
// may end the moment the server hears the decline, and it is bounded by the
// elicitation timeout instead.
func tell(ctx context.Context, asker elicit.Asker, consent ElicitationConsent, notice elicit.Question, timeout time.Duration) {
	ctx = context.WithoutCancel(ctx)
	if asker == nil {
		askFallback(ctx, consent, notice, timeout)
		return
	}
	bounded, stop := withExpiry(ctx, timeout)
	defer stop()
	_, _ = askRecovered(bounded, asker, notice)
}

// waitContext ends when ctx does or when every call in calls has ended. A classic
// request cannot say which open call it belongs to, so it is waited on until none
// is left.
func waitContext(ctx context.Context, calls []context.Context) (context.Context, func()) {
	wait, cancel := context.WithCancelCause(ctx)
	var open atomic.Int64
	open.Store(int64(len(calls)))
	stops := make([]func() bool, 0, len(calls))
	for _, call := range calls {
		stops = append(stops, context.AfterFunc(call, func() {
			if open.Add(-1) == 0 {
				cancel(context.Cause(call))
			}
		}))
	}
	return wait, func() {
		for _, stop := range stops {
			stop()
		}
		cancel(nil)
	}
}

// withExpiry ends ctx with elicit.ErrExpired once timeout passes. It runs its own
// timer rather than context.WithTimeout: a held call's context reports a deadline
// earlier than the question's, WithTimeout trusts an earlier parent deadline and
// arms no timer, and the bound would never fire.
func withExpiry(ctx context.Context, timeout time.Duration) (context.Context, func()) {
	bounded, cancel := context.WithCancelCause(ctx)
	timer := time.AfterFunc(timeout, func() { cancel(elicit.ErrExpired) })
	return bounded, func() {
		timer.Stop()
		cancel(nil)
	}
}

// waitEnded is the outcome of a wait whose context ended before an answer. Both
// cancel: MCP defines cancel as "dismissed without making an explicit choice"
// (2025-11-25 client/elicitation), and nobody chose. The reason keeps an expiry
// apart from the call or the run ending.
func waitEnded(ctx context.Context, fields int) elicitOutcome {
	cause := context.Cause(ctx)
	if errors.Is(cause, elicit.ErrExpired) {
		return elicitOutcome{action: elicit.ActionCancel, fields: fields, reason: "expired"}
	}
	return elicitOutcome{action: elicit.ActionCancel, fields: fields, reason: "the call or the run ended", err: cause}
}

func askRecovered(ctx context.Context, asker elicit.Asker, q elicit.Question) (answer elicit.Answer, err error) {
	defer func() {
		if recover() != nil {
			answer, err = elicit.Answer{}, errElicitationPanic
		}
	}()
	return asker.Ask(ctx, q)
}
```

In `cmd/aura/elicitation_consent.go`:
- Replace the `mcptools` import with `"github.com/chetto1983/aura/internal/elicit"`.
- Delete `maxRenderedFields` and its comment (32-35).
- Replace the file comment's first two paragraphs (16-22) with:

```go
// elicitation_consent.go is the composition-root half of SEP-2322 elicitation:
// the fallback for a request no cockpit run can answer (mcptools
// elicitation_route.go). It declines, and delivers the ask to the operator on
// their own channel, so a server that asked is never refused in silence.
```

- Change `AskOperator`'s signature to `AskOperator(ctx context.Context, q elicit.Question) (string, map[string]any, error)`.
- In its body, replace every `req.Server` with `q.Server`, every `"decline"` with `elicit.ActionDecline`, and `renderElicitationPrompt(req)` with `renderElicitationPrompt(q)`.
- Replace `renderElicitationPrompt` with:

```go
func renderElicitationPrompt(q elicit.Question) string {
	var b strings.Builder
	fmt.Fprintf(&b, "MCP server %q is asking for input.\n", q.Server)

	message := strings.TrimSpace(q.Message)
	if message == "" {
		message = "(the server sent no message)"
	}
	b.WriteString("\nIt says:\n")
	for line := range strings.SplitSeq(message, "\n") {
		fmt.Fprintf(&b, "> %s\n", strings.TrimRight(line, "\r"))
	}

	if len(q.Fields) > 0 {
		b.WriteString("\nIt is asking for:\n")
		for _, f := range q.Fields {
			fmt.Fprintf(&b, "- %s (%s", f.Name, f.Kind)
			if f.Required {
				b.WriteString(", required")
			}
			b.WriteString(")")
			if desc := strings.TrimSpace(f.Description); desc != "" {
				b.WriteString(": " + desc)
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n" + declinedBecause(q.Refusal))
	return capPromptBytes(b.String(), maxRenderedPromptBytes)
}

// declinedBecause is the prompt's last line: what Aura did, and why when it did
// not even try to ask anyone.
func declinedBecause(refusal string) string {
	switch refusal {
	case elicit.RefusalUnrenderable:
		return "Aura declined it automatically because its form cannot be shown. Nothing was sent to the server."
	case elicit.RefusalAmbiguousRun:
		return "Aura declined it automatically because calls from more than one conversation are open on that server, so Aura cannot tell which one it belongs to. Nothing was sent to the server."
	default:
		return "Aura declined it automatically. Nothing was sent to the server."
	}
}
```

- Replace the comment on `maxRenderedPromptBytes` with:

```go
// maxRenderedPromptBytes is the last-resort bound on the whole rendered prompt.
// Each part is already capped upstream by internal/elicit (message, titles,
// descriptions, 20 fields), so this bites only on a form at every cap at once; it
// sits under Telegram's 4096-character message limit so the chosen surface can
// actually deliver what it renders.
```

- [ ] **Step 4: Run the packages.** Go: `go vet ./internal/agent/mcptools/ ./cmd/aura/`, then `go test -race -count=1 ./internal/agent/mcptools/`, then `go test -race -count=1 -run 'Elicitation|RenderElicitation|CapPrompt|MCPMountOptions' ./cmd/aura/`, then `golangci-lint run ./internal/agent/mcptools/...`.

Expected: `ok` for both test runs and `0 issues.` from the linter. All of it was measured on a scratch copy of HEAD `61a77c624` with exactly this code (2026-09-25): the mcptools package ran in 5.7 s under `-race`.
- `TestBridgedToolExecuteAppliesConfiguredCallTimeout` still passes unchanged. The pausable context still expires, and it still reports `context deadline exceeded` through the SDK's derived contexts (Task 1's `TestWithTimeoutExpiresLikeAStandardDeadline` is why).
- If either classic test fails with `cannot be sent while serving a request on protocol version 2026-07-28`, the client did not fall back. Stop and read `client.go:314-386` before changing the fixture. Do not pass a client protocol version: production does not.
- Check the size: `wc -l internal/agent/mcptools/bridge_supervisor.go internal/agent/mcptools/elicitation*.go internal/agent/mcptools/bridge_inflight.go`. Every file must be under 600 lines. Measured: `bridge_supervisor.go` 500, `elicitation_test.go` 370, `elicitation_route_test.go` 334, `elicitation_fixtures_test.go` 282, `elicitation_route.go` 243, `elicitation.go` 201, `bridge_inflight.go` 122, `elicitation_wait_test.go` 96.
- Measure coverage: Go: `go test -race -count=1 -coverprofile=/tmp/mcpt.out ./internal/agent/mcptools/ && go tool cover -func=/tmp/mcpt.out | grep -E 'elicitation|inflight'`. Measured: every new function at 100% except `askFallback` (96.4%, the recover arm) and `questionFor` (75%, the `DecodeSchema` error arm). Missing functions go in the task report, not behind a skip.

- [ ] **Step 5: Commit.**

```bash
cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH" LEFTHOOK_BIN="$HOME/go/bin/lefthook"
git add internal/agent/mcptools/bridge_inflight.go internal/agent/mcptools/elicitation_route.go internal/agent/mcptools/elicitation_fixtures_test.go internal/agent/mcptools/elicitation_route_test.go internal/agent/mcptools/elicitation_wait_test.go
git -c core.hooksPath=.git/hooks commit -F - -- internal/agent/mcptools/bridge_inflight.go internal/agent/mcptools/elicitation_route.go internal/agent/mcptools/elicitation_fixtures_test.go internal/agent/mcptools/elicitation_route_test.go internal/agent/mcptools/elicitation_wait_test.go internal/agent/mcptools/elicitation.go internal/agent/mcptools/elicitation_test.go internal/agent/mcptools/bridge_deferral_test.go internal/agent/mcptools/bridge_supervisor.go internal/agent/mcptools/bridge_call.go cmd/aura/elicitation_consent.go cmd/aura/elicitation_consent_test.go <<'EOF'
feat(mcptools): put a server's form to the run that made the call

An MCP server's form elicitation now reaches the run that made the call.

- A multi-round-trip request arrives on its request's own context, the
  tools/call or the resources/read of its result links, and names its
  run outright.
- A classic elicitation/create is matched through the calls open on its
  session. With calls from two runs open, or from two operators, it is
  declined, and each is told why without any of the server's text.
- A session may have four questions open at once; past that a request
  is declined without telling anyone, since a notice per request would
  be the flood itself.

The wait holds every clock of the call, so the operator's time counts
against neither the 60 s call bound nor the run's wallclock. The wait
has its own expiry timer: a held call reports an earlier deadline, and
context.WithTimeout would have armed no timer at all. An expired wait
answers cancel, as MCP defines it and as TestElicitationTimesOutToCancel
already pinned.

The flood-of-fields render test becomes a byte-bound test. FromSchema
now refuses a form of more than 20 fields, so the "and N more" line
could never be reached and is gone.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---

### Task 5: Every runtime mount can be asked

**Files:**
- Modify: `cmd/aura/runtime_tool_handles.go:36-40`. Add the `Elicitation` handle after `MCPFiles`.
- Modify: `cmd/aura/main.go:337-355`. `buildRegistryWithMCP` stores `consent` on the handles, before the empty-set early return.
- Modify: `cmd/aura/main.go:418`. The stdio mount takes `stdioMountOptions`.
- Modify: `cmd/aura/mcp_tools.go:266-276`. `mcpMountOptions` carries the consent. Add `stdioMountOptions` below it.
- Modify: `internal/agent/mcptools/mount.go`, in three places:
  - the doc comments at 26-31 and 41-45;
  - the local variables named `elicit` at 124/127 and 181/185, renamed to `handler`, so that no local shadows the package the sibling files import.
- Test: `cmd/aura/mcp_mount_options_test.go`.

**Interfaces:**
- Consumes: `mcptools.ElicitationConsent` (Task 4); `newElicitationConsent()` (`cmd/aura/elicitation_consent.go:68`).
- Produces:
  - `runtimeToolHandles.Elicitation mcptools.ElicitationConsent`;
  - `stdioMountOptions(handles *runtimeToolHandles) mcptools.MountOptions`.
  - Every boot and live mount now advertises form elicitation.
  - `aura tools` (`main.go:549`) and the one-shot pipe (`toolpipe.go:80-89`) keep passing `nil`. They have no operator, and a nil consent keeps the capability unadvertised.

**What advertising changes, and what was decided.** A server that respects the capability behaves differently once it is advertised: go-sdk advertises `elicitation.form` whenever a handler is set (`mcp/client.go:287-296`).
- A server may add tools or start asking. server-everything registers `trigger-elicitation-request` only for a client that advertises elicitation (`src/everything/tools/trigger-elicitation-request.ts:40-46`).
- In a Telegram, cron or `aura chat` turn the fallback then declines, and tells the operator on their channel, where before the server never asked.
- The spec says every mount advertises, and the v2 rulings keep it. Aura's own three servers (`arcadedb-mcp`, `aura-pim-mcp`, `whatsapp-mcp`) contain no elicitation code (grep, 2026-09-25), so the servers in use today do not change.
- Task 10 re-checks their tool counts, drives one turn outside the cockpit, and the PRD paragraph records the third-party risk.
- Considered and not taken: Archestra keeps capability-bearing connections apart, one per (agent, conversation) with an `:elicitation` suffix (`platform/backend/src/clients/mcp-client.ts:912-920`). It doubles every mount's sessions, and the operator did not ask for it. Open points records it.

- [ ] **Step 1: Write the failing tests.** Append to `cmd/aura/mcp_mount_options_test.go`:

```go
func TestEveryRuntimeMountCarriesTheElicitationFallback(t *testing.T) {
	consent := newElicitationConsent()
	handles := &runtimeToolHandles{MCPFiles: &tools.MCPFileSink{}, Elicitation: consent}

	managed := mcpMountOptions(context.Background(), true, mcp.ManagedServer{URL: "https://mcp.example/mcp"}, handles)
	stdio := stdioMountOptions(handles)

	if managed.Elicitation != consent || stdio.Elicitation != consent {
		t.Fatalf("managed=%v stdio=%v, want both mounts handed the fallback consent", managed.Elicitation, stdio.Elicitation)
	}
	if stdio.Files != handles.MCPFiles {
		t.Fatalf("stdio options lost the file sink: %+v", stdio)
	}
}

// Live mounts read the consent off the boot handles, so it must be there even when
// boot mounted nothing and left through the empty-set early return.
func TestBuildRegistryWithMCP_NoBootServersStillHandsLiveMountsTheConsent(t *testing.T) {
	withMemoryMCPRegistry(t)
	seedMCPRegistry(t, withDefaultOnRecipesOff(mcp.ManagedConfig{}))
	consent := newElicitationConsent()

	_, handles, closers, err := buildRegistryWithMCP(context.Background(), config.LoadDB(), nil, nil, nil, consent)
	if err != nil {
		t.Fatalf("buildRegistryWithMCP: %v", err)
	}
	defer func() { _ = closeMCPServers(closers) }()

	if handles.Elicitation != consent {
		t.Fatalf("handles.Elicitation = %v, want the consent boot was given", handles.Elicitation)
	}
}
```

- [ ] **Step 2: Run them to verify they fail.** Go: `go test -race -count=1 -run 'CarriesTheElicitationFallback|StillHandsLiveMountsTheConsent' ./cmd/aura/`.

Expected: build FAIL with `unknown field Elicitation in struct literal of type runtimeToolHandles` and `undefined: stdioMountOptions`.

- [ ] **Step 3: Implement.** In `cmd/aura/runtime_tool_handles.go`, add after `MCPFiles mcptools.FileSink`:

```go
	// Elicitation is the fallback consent every runtime mount is handed: a mounted
	// server's form reaches a cockpit run through that run's own asker, and this
	// only for the requests no run can answer. Nil on `aura tools` and the one-shot
	// pipe, which have no operator and so advertise no elicitation at all.
	Elicitation mcptools.ElicitationConsent
```

In `cmd/aura/main.go`, after `handles.MCPFiles = &tools.MCPFileSink{Router: sandboxRouter}`, add:

```go
	handles.Elicitation = consent
```

Line 418 becomes:

```go
				return mcptools.MountServer(mountCtx, c, reg, name, mcpServers[name], stdioMountOptions(&handles))
```

In `cmd/aura/mcp_tools.go`, replace `mcpMountOptions` and add `stdioMountOptions` after it:

```go
// mcpMountOptions is the option set every runtime mount of a managed server uses --
// at boot and live after an authorization -- so neither can forget one the other
// carries. That already happened once with the grant store (runtimeMCPOAuth).
func mcpMountOptions(ctx context.Context, strict bool, server mcp.ManagedServer, handles *runtimeToolHandles) mcptools.MountOptions {
	return mcptools.MountOptions{
		Egress:      mcp.RuntimeEgressPolicy(strict, server),
		Views:       handles.MCPViews,
		OAuth:       runtimeMCPOAuth(ctx),
		Files:       handles.MCPFiles,
		Elicitation: handles.Elicitation,
	}
}

// stdioMountOptions is the same for a stdio-configured server, which needs no
// Egress or OAuth: those shape an HTTP connection.
func stdioMountOptions(handles *runtimeToolHandles) mcptools.MountOptions {
	return mcptools.MountOptions{Files: handles.MCPFiles, Elicitation: handles.Elicitation}
}
```

In `internal/agent/mcptools/mount.go`, replace the tail of `MountServer`'s comment (lines 26-31, from "cmd/aura's boot mounts" to "shape an HTTP connection.") with:

```go
// carries the per-mount choices. cmd/aura's boot mounts a stdio-configured server
// through here with stdioMountOptions (Files, Elicitation); a managed server, stdio
// or HTTP, goes through MountManagedServerWithOptions with mcpMountOptions (Egress,
// Views, OAuth, Files, Elicitation) at boot and live. Every runtime mount is given
// Elicitation, so every one advertises form elicitation; `aura tools` and the
// one-shot pipe pass none. A stdio mount has no use for Egress or OAuth, which
// shape an HTTP connection.
```

Replace `MountOptions`'s second paragraph (41-45) with:

```go
// A zero Elicitation is meaningful, not merely absent: it leaves
// SessionOptions.Elicitation nil, and a nil ClientOptions.ElicitationHandler means
// the client does NOT advertise the capability. The paths with no operator at all
// pass none. Every other mount passes the composition root's fallback, and a form
// asked inside a cockpit run reaches that run's own elicit.Asker first
// (elicitation_route.go).
```

At 124 and 181, rename the local `elicit` to `handler`: `handler := elicitationHandlerFor(name, opts.Elicitation)` and `o.Elicitation = handler`.

- [ ] **Step 4: Run the packages.** Go: `go vet ./cmd/aura/ ./internal/agent/mcptools/`, then `go test -race -count=1 -run 'MCPMountOptions|CarriesTheElicitationFallback|StillHandsLiveMountsThe|BuildRegistryWithMCP' ./cmd/aura/`, then `go test -race -count=1 ./internal/agent/mcptools/`.

Expected: `ok`. `TestBuildRegistryWithMCP_HandsMountsTheBoxFileSink` still passes with `nil` consent. Measured on the scratch copy with Tasks 1-5 applied: the `cmd/aura` subset passes under `-race` in 6.4 s, `go build ./...` is clean, and `main.go` ends at 566 lines, `mcp_tools.go` at 283, `mount.go` at 264.

- [ ] **Step 5: Commit.**

```bash
cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH" LEFTHOOK_BIN="$HOME/go/bin/lefthook"
git -c core.hooksPath=.git/hooks commit -F - -- cmd/aura/runtime_tool_handles.go cmd/aura/main.go cmd/aura/mcp_tools.go cmd/aura/mcp_mount_options_test.go internal/agent/mcptools/mount.go <<'EOF'
feat(aura): hand every runtime mount the elicitation fallback

buildRegistryWithMCP took a consent parameter and had dropped it since
89688bd27, so no mount advertised elicitation. It now stores it on the
runtime handles, before the empty-set early return, because live
mounts read their options off those handles too. mcpMountOptions and
the new stdioMountOptions both carry it.

`aura tools` and the one-shot pipe still pass nil. They have no
operator, and a nil consent keeps the capability unadvertised.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---
### Task 6: A detached run carries the form and takes the answer

**Files:**
- Create:
  - `internal/agui/run_elicitation.go`
  - `internal/agui/server_run_elicitation.go`
  - `internal/agui/run_elicitation_test.go`
  - `internal/agui/server_run_elicitation_e2e_test.go`
- Modify: `internal/agui/runsession.go`, in five places:
  - the `RunSession` comment at 47-51, which says the producer is the sole appender;
  - the struct at 52-84 gains `questions`;
  - `newRunSession` at 89-108;
  - the `append` comment at 110-117;
  - `publish` goes after `append`.
- Modify: `internal/agui/server_run_detach.go`, in four places:
  - the `detachedRunContext` comment at 33-39;
  - the asker installation after line 106;
  - `runProducer` at 142-143;
  - the imports.
- Modify: `internal/agui/server.go:365`, which mounts the two routes.
- Modify: `internal/agui/idempotency_http.go:68`, which adds the inventory entry for the POST.

**Interfaces:**
- Consumes:
  - `elicit.Question`, `elicit.Answer`, `elicit.Validate`, `elicit.FieldErrors`, `elicit.Problem*`, `elicit.ErrExpired`, `elicit.WithAsker`, `elicit.Action*`, `elicit.MaxOpenQuestions` (Task 3);
  - the mcptools handler's contract (Task 4): it calls `Ask` with every clock held, sets `Question.Deadline`, and reads the outcome as final. An error whose cause is `elicit.ErrExpired` becomes a cancel there.
- Produces:
  - **The two CUSTOM events** (Tasks 7-8 parse them):
    - `aura.elicitation`, whose value is `{run_id, id, server, tool, message, fields: Field[], deadline (RFC 3339), refusal?}`;
    - `aura.elicitation_resolved`, whose value is `{id, action, expired?}`. An expiry is `{action: "cancel", expired: true}`.
  - **The answer route** `POST /agent/runs/{runID}/elicitations/{id}`, with body `{action, content?}`:

    | Code | Body | When |
    |---|---|---|
    | 202 | `{"status":"delivered"}` | the answer was delivered |
    | 400 | | an unknown action or an invalid body |
    | 404 | | the run or the question is not the caller's |
    | 409 | `{"error":"question already resolved"}` | the question is closed |
    | 410 | | the run is terminal |
    | 422 | `{"errors":{field: code}}` | the content fails `elicit.Validate`; each code is an `elicit.Problem*` constant, never the value |

  - **The list route** `GET /agent/runs/{runID}/elicitations` → 200 `{"questions": elicitationFrame[]}`, oldest first, owner-scoped like the POST (404 otherwise). It is a read, so it is not in `httpMutationRoutes`. Task 8 calls it when a live run is attached.
  - `RunSession.publish(ctx, ev) bool`.
  - A run shows at most `elicit.MaxOpenQuestions` open forms. Past that, `Ask` declines at once and publishes nothing.

**Why the resolution frames use the run's context (adversarial M10).** CUSTOM is a lifecycle frame, so `append` blocks with `s.mu` held until the frame is delivered, the subscriber leaves, or ctx ends (`runsession.go:110-117`, `server_sse.go:177`). A question frame is published with the asking call's ctx, which the call's own expiry ends. A resolution is published under `rq.mu` after that call may already be gone. With `context.Background()`, a tab that stays connected but stops reading would wedge the session past the 3600 s cap. So `bind(dctx)` hands the run's context to `runQuestions`. The ring write comes before the fan-out, so a replay still carries the frame. The client settles a card whose resolution it missed when the run ends (Task 8).

**Why the list route exists (adversarial H4).** `attachRun` subscribes from sequence 0. Once the ring (2048 events by default) has rotated past a form, `subscribeFrom(0)` fails and `handleRunEvents` answers 410 (`server_run_resume.go:77-80`). The client then takes `failWithSnapshot` (`sseResume.ts:214`), and no `aura.elicitation` frame comes back. The list route serves open questions from `runQuestions` itself, so it never depends on the ring.

- [ ] **Step 1: Write the failing tests.** Create `internal/agui/run_elicitation_test.go`:

```go
package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/elicit"
	"github.com/chetto1983/aura/internal/identityctx"
)

type askResult struct {
	answer elicit.Answer
	err    error
}

func questionOf(t *testing.T, raw map[string]any) elicit.Question {
	t.Helper()
	schema, err := elicit.DecodeSchema(raw)
	if err != nil {
		t.Fatal(err)
	}
	q, err := elicit.FromSchema("forms", "ask_name", "what is your name", schema)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func nameQuestion(t *testing.T) elicit.Question {
	return questionOf(t, map[string]any{
		"type":       "object",
		"properties": map[string]any{"name": map[string]any{"type": "string"}},
		"required":   []any{"name"},
	})
}

// openRun starts a run owned by the local identity, as the route resolves it,
// and subscribes to its stream from the first frame.
func openRun(t *testing.T) (*Server, *httptest.Server, *RunSession, <-chan seqEvent) {
	return openRunWith(t, ServerConfig{})
}

func openRunWith(t *testing.T, cfg ServerConfig) (*Server, *httptest.Server, *RunSession, <-chan seqEvent) {
	t.Helper()
	s, srv := newDetachTestServer(t, &scriptedRunner{events: textTurn("hi")}, &fakeConvStore{}, cfg)
	sess, err := s.runs.Start(runParams{runID: "run-" + uuid.NewString(), threadID: "t-forms", identityID: localIdentityID})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ch, cancel, _ := sess.subscribeFrom(0)
	t.Cleanup(cancel)
	t.Cleanup(sess.finish)
	// Registered last, so it runs first: releases any Ask a test left waiting.
	t.Cleanup(sess.questions.cancelAll)
	return s, srv, sess, ch
}

// ask puts q to the run's asker on its own goroutine.
func ask(ctx context.Context, sess *RunSession, q elicit.Question) <-chan askResult {
	out := make(chan askResult, 1)
	go func() {
		answer, err := sess.questions.Ask(ctx, q)
		out <- askResult{answer: answer, err: err}
	}()
	return out
}

// nextCustom reads the session's stream up to the next CUSTOM frame named name.
func nextCustom(t *testing.T, ch <-chan seqEvent, name string) any {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case sev, ok := <-ch:
			if !ok {
				t.Fatalf("the stream closed before a %s frame", name)
			}
			if ce, isCustom := sev.Ev.(*events.CustomEvent); isCustom && ce.Name == name {
				return ce.Value
			}
		case <-timeout:
			t.Fatalf("no %s frame within 5s", name)
			return nil
		}
	}
}

func answerURL(runID, id string) string {
	return "/agent/runs/" + runID + "/elicitations/" + id
}

func answerPost(t *testing.T, srv *httptest.Server, runID, id, body string) (int, string) {
	t.Helper()
	resp, err := http.Post(srv.URL+answerURL(runID, id), "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST answer: %v", err)
	}
	return resp.StatusCode, readFullBody(t, resp)
}

func getStatus(t *testing.T, url string) int {
	t.Helper()
	resp := mustGet(t, url)
	readFullBody(t, resp)
	return resp.StatusCode
}

func result(t *testing.T, got <-chan askResult) askResult {
	t.Helper()
	select {
	case r := <-got:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("Ask never returned")
		return askResult{}
	}
}

func TestAnAnswerIsDeliveredOnce(t *testing.T) {
	_, srv, sess, ch := openRun(t)
	got := ask(context.Background(), sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)
	if q.RunID != sess.RunID || q.Server != "forms" || q.Tool != "ask_name" || len(q.Fields) != 1 {
		t.Fatalf("frame = %+v", q)
	}

	if status, body := answerPost(t, srv, sess.RunID, q.ID, `{"action":"accept","content":{"name":"Ada"}}`); status != http.StatusAccepted {
		t.Fatalf("first answer = %d %s, want 202", status, body)
	}
	if r := result(t, got); r.err != nil || r.answer.Action != elicit.ActionAccept || r.answer.Content["name"] != "Ada" {
		t.Fatalf("Ask = %+v", r)
	}
	if resolved := nextCustom(t, ch, ElicitationResolvedEventName).(elicitationResolvedFrame); resolved.ID != q.ID || resolved.Action != elicit.ActionAccept {
		t.Fatalf("resolved = %+v", resolved)
	}
	if status, _ := answerPost(t, srv, sess.RunID, q.ID, `{"action":"decline"}`); status != http.StatusConflict {
		t.Fatalf("a late answer = %d, want 409", status)
	}
	if sess.questions.close(q.ID, elicit.ActionCancel, true) {
		t.Fatal("an expiry after the answer resolved the question a second time")
	}
}

// Review Focus 5.
func TestAnAnswerThatFailsTheSchemaLeavesTheQuestionOpen(t *testing.T) {
	_, srv, sess, ch := openRun(t)
	got := ask(context.Background(), sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)

	status, body := answerPost(t, srv, sess.RunID, q.ID, `{"action":"accept","content":{}}`)
	var refusal struct {
		Errors map[string]string `json:"errors"`
	}
	if status != http.StatusUnprocessableEntity || json.Unmarshal([]byte(body), &refusal) != nil || refusal.Errors["name"] != elicit.ProblemRequired {
		t.Fatalf("answer without the required field = %d %s, want 422 naming it", status, body)
	}
	if status, body := answerPost(t, srv, sess.RunID, q.ID, `{"action":"accept","content":{"name":"Ada"}}`); status != http.StatusAccepted {
		t.Fatalf("the corrected answer = %d %s, want 202: a 422 must leave the question open", status, body)
	}
	if r := result(t, got); r.answer.Content["name"] != "Ada" {
		t.Fatalf("Ask = %+v", r)
	}
}

// The idempotency layer keeps a mutation's response for its replay, so a 422 must
// name the problem and never the value.
func TestARefusedAnswerLeavesNoValueInTheReplayStore(t *testing.T) {
	s, _, sess, ch := openRun(t)
	registry := &memoryHTTPRegistry{}
	s.SetOperationRegistry(registry)
	ask(context.Background(), sess, questionOf(t, map[string]any{
		"type":       "object",
		"properties": map[string]any{"token": map[string]any{"type": "string", "maxLength": 8}},
		"required":   []any{"token"},
	}))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)

	const secret = "sk-live-0123456789abcdef"
	req := httptest.NewRequest(http.MethodPost, answerURL(sess.RunID, q.ID),
		strings.NewReader(`{"action":"accept","content":{"token":"`+secret+`"}}`))
	req = req.WithContext(identityctx.WithIdentityID(req.Context(), localIdentityID))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "answer-1")
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), elicit.ProblemTooLong) {
		t.Fatalf("an over-long secret = %d %s, want 422 %s", rec.Code, rec.Body, elicit.ProblemTooLong)
	}
	if registry.replay == nil {
		t.Fatal("the 422 never reached the replay store, so this proves nothing")
	}
	for where, text := range map[string]string{"response": rec.Body.String(), "replay store": string(registry.replay.Body)} {
		if strings.Contains(text, secret) {
			t.Fatalf("the %s carries the operator's value: %s", where, text)
		}
	}
}

func TestTheRouteRefusesWhatItCannotDeliver(t *testing.T) {
	s, srv, sess, ch := openRun(t)
	ask(context.Background(), sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)

	foreign, err := s.runs.Start(runParams{runID: "run-88888888-8888-8888-8888-888888888888", threadID: "t-foreign", identityID: "33333333-3333-3333-3333-333333333333"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(foreign.finish)
	for name, tc := range map[string]struct {
		runID, id, body string
		want            int
	}{
		"unknown question": {sess.RunID, "no-such-question", `{"action":"decline"}`, http.StatusNotFound},
		"foreign run":      {foreign.RunID, q.ID, `{"action":"decline"}`, http.StatusNotFound},
		"unknown action":   {sess.RunID, q.ID, `{"action":"sure"}`, http.StatusBadRequest},
		"not json":         {sess.RunID, q.ID, `accept`, http.StatusBadRequest},
	} {
		if status, body := answerPost(t, srv, tc.runID, tc.id, tc.body); status != tc.want {
			t.Errorf("%s: %d %s, want %d", name, status, body, tc.want)
		}
	}
	if status := getStatus(t, srv.URL+"/agent/runs/"+foreign.RunID+"/elicitations"); status != http.StatusNotFound {
		t.Errorf("listing a foreign run's questions = %d, want 404", status)
	}
	sess.finish()
	if status, _ := answerPost(t, srv, sess.RunID, q.ID, `{"action":"decline"}`); status != http.StatusGone {
		t.Fatalf("an answer to an ended run = %d, want 410", status)
	}
}

// A reattach whose replay the ring can no longer serve gets a 410 and no frame, so
// the run lists its open forms itself.
func TestAFormTheRingRotatedPastIsStillListed(t *testing.T) {
	_, srv, sess, ch := openRunWith(t, ServerConfig{RunBufferEvents: 8})
	got := ask(context.Background(), sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)
	for i := range 16 {
		sess.append(context.Background(), events.NewCustomEvent("filler", events.WithValue(i)))
	}
	if status := getStatus(t, srv.URL+"/agent/runs/"+sess.RunID+"/events"); status != http.StatusGone {
		t.Fatalf("a full replay after rotation = %d, want 410, so this proves nothing", status)
	}

	var open struct {
		Questions []elicitationFrame `json:"questions"`
	}
	list := func() {
		t.Helper()
		resp := mustGet(t, srv.URL+"/agent/runs/"+sess.RunID+"/elicitations")
		if body := readFullBody(t, resp); resp.StatusCode != http.StatusOK || json.Unmarshal([]byte(body), &open) != nil {
			t.Fatalf("list = %d %s", resp.StatusCode, body)
		}
	}
	list()
	if len(open.Questions) != 1 || open.Questions[0].ID != q.ID || open.Questions[0].RunID != sess.RunID || len(open.Questions[0].Fields) != 1 {
		t.Fatalf("listed = %+v, want the one open form", open.Questions)
	}
	if status, body := answerPost(t, srv, sess.RunID, q.ID, `{"action":"accept","content":{"name":"Ada"}}`); status != http.StatusAccepted {
		t.Fatalf("answering the listed form = %d %s, want 202", status, body)
	}
	if r := result(t, got); r.answer.Content["name"] != "Ada" {
		t.Fatalf("Ask = %+v", r)
	}
	if list(); len(open.Questions) != 0 {
		t.Fatalf("listed after the answer = %+v, want none", open.Questions)
	}
}

func TestAnExpiredQuestionIsResolvedAndClosed(t *testing.T) {
	_, srv, sess, ch := openRun(t)
	ctx, cancel := context.WithCancelCause(context.Background())
	got := ask(ctx, sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)

	cancel(elicit.ErrExpired)
	if r := result(t, got); !errors.Is(r.err, elicit.ErrExpired) {
		t.Fatalf("Ask = %+v, want the expiry back", r)
	}
	if resolved := nextCustom(t, ch, ElicitationResolvedEventName).(elicitationResolvedFrame); resolved.Action != elicit.ActionCancel || !resolved.Expired {
		t.Fatalf("resolved = %+v, want an expired cancel", resolved)
	}
	if status, _ := answerPost(t, srv, sess.RunID, q.ID, `{"action":"accept","content":{"name":"Ada"}}`); status != http.StatusConflict {
		t.Fatalf("an answer after expiry = %d, want 409", status)
	}
}

func TestACallThatEndsCancelsItsQuestion(t *testing.T) {
	_, _, sess, ch := openRun(t)
	ctx, cancel := context.WithCancel(context.Background())
	got := ask(ctx, sess, nameQuestion(t))
	nextCustom(t, ch, ElicitationEventName)

	cancel()
	if r := result(t, got); !errors.Is(r.err, context.Canceled) {
		t.Fatalf("Ask = %+v, want the call's cancellation", r)
	}
	if resolved := nextCustom(t, ch, ElicitationResolvedEventName).(elicitationResolvedFrame); resolved.Action != elicit.ActionCancel || resolved.Expired {
		t.Fatalf("resolved = %+v, want a cancel", resolved)
	}
}

func TestTheRunEndingCancelsEveryPendingQuestion(t *testing.T) {
	_, _, sess, ch := openRun(t)
	first := ask(context.Background(), sess, nameQuestion(t))
	nextCustom(t, ch, ElicitationEventName)
	second := ask(context.Background(), sess, nameQuestion(t))
	nextCustom(t, ch, ElicitationEventName)

	sess.questions.cancelAll()
	for _, got := range []<-chan askResult{first, second} {
		if r := result(t, got); r.err != nil || r.answer.Action != elicit.ActionCancel {
			t.Fatalf("Ask = %+v, want cancel when the run ends", r)
		}
	}
	for range 2 {
		if resolved := nextCustom(t, ch, ElicitationResolvedEventName).(elicitationResolvedFrame); resolved.Action != elicit.ActionCancel {
			t.Fatalf("resolved = %+v", resolved)
		}
	}
	if r, err := sess.questions.Ask(context.Background(), nameQuestion(t)); err != nil || r.Action != elicit.ActionCancel {
		t.Fatalf("a question put after the run ended = %+v, %v; want cancel at once", r, err)
	}
}

// Each session is capped in mcptools; a run that reaches several servers is
// capped here.
func TestARunOpensNoMoreThanTheCap(t *testing.T) {
	_, _, sess, ch := openRun(t)
	for range elicit.MaxOpenQuestions {
		ask(context.Background(), sess, nameQuestion(t))
		nextCustom(t, ch, ElicitationEventName)
	}
	if r, err := sess.questions.Ask(context.Background(), nameQuestion(t)); err != nil || r.Action != elicit.ActionDecline {
		t.Fatalf("a question over the run's cap = %+v, %v; want a decline at once", r, err)
	}
	if n := len(sess.questions.open()); n != elicit.MaxOpenQuestions {
		t.Fatalf("%d questions open, want the cap of %d", n, elicit.MaxOpenQuestions)
	}
}

func TestARefusedQuestionIsShownAlreadyResolved(t *testing.T) {
	_, _, sess, ch := openRun(t)
	q := elicit.Question{Server: "forms", Tool: "ask_name", Refusal: elicit.RefusalAmbiguousRun}

	if r, err := sess.questions.Ask(context.Background(), q); err != nil || r.Action != elicit.ActionDecline {
		t.Fatalf("Ask = %+v, %v; a refusal declines at once", r, err)
	}
	if shown := nextCustom(t, ch, ElicitationEventName).(elicitationFrame); shown.Refusal != elicit.RefusalAmbiguousRun {
		t.Fatalf("shown = %+v", shown)
	}
	if resolved := nextCustom(t, ch, ElicitationResolvedEventName).(elicitationResolvedFrame); resolved.Action != elicit.ActionDecline {
		t.Fatalf("resolved = %+v", resolved)
	}
}

func TestTheEventsCarryNoAnswerValues(t *testing.T) {
	_, srv, sess, ch := openRun(t)
	got := ask(context.Background(), sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)
	answerPost(t, srv, sess.RunID, q.ID, `{"action":"accept","content":{"name":"Zebedee"}}`)
	result(t, got)

	sess.finish()
	replay, _, _ := sess.subscribeFrom(0)
	for sev := range replay {
		raw, err := json.Marshal(sev.Ev)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "Zebedee") {
			t.Fatalf("frame %d carries the operator's value: %s", sev.Seq, raw)
		}
	}
}

func TestPublishInterleavesWithTheProducer(t *testing.T) {
	_, _, sess, ch := openRun(t)
	// CUSTOM is a lifecycle frame: append blocks on a subscriber that stops reading.
	go func() {
		for range ch {
		}
	}()
	const each = 200
	var wg sync.WaitGroup
	for _, publisher := range []func(context.Context, events.Event) bool{sess.append, sess.publish} {
		wg.Go(func() {
			for i := range each {
				publisher(context.Background(), events.NewCustomEvent("probe", events.WithValue(i)))
			}
		})
	}
	wg.Wait()
	sess.finish()
	replay, _, _ := sess.subscribeFrom(0)
	var last int64
	count := 0
	for sev := range replay {
		if sev.Seq != last+1 {
			t.Fatalf("seq %d after %d: a publish broke the ring's order", sev.Seq, last)
		}
		last = sev.Seq
		count++
	}
	if count != 2*each {
		t.Fatalf("replayed %d frames, want %d", count, 2*each)
	}
}

// A resolution is bounded by the run, not by the call that asked: a tab that stops
// reading holds the session only until the run ends, and the frame still reaches
// the ring for a replay.
func TestAResolutionGivesUpWhenTheRunEnds(t *testing.T) {
	_, _, sess, ch := openRun(t)
	runCtx, endRun := context.WithCancel(context.Background())
	sess.questions.bind(runCtx)
	got := ask(context.Background(), sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)
	// Fill the stalled subscriber's channel, so the next lifecycle frame blocks.
	for i := range fanoutBuffer {
		sess.append(context.Background(), events.NewCustomEvent("filler", events.WithValue(i)))
	}

	delivered := make(chan error, 1)
	go func() {
		_, err := sess.questions.answer(q.ID, elicit.Answer{Action: elicit.ActionDecline})
		delivered <- err
	}()
	select {
	case <-delivered:
		t.Fatal("the resolution did not wait for the stalled tab, so this proves nothing")
	case <-time.After(100 * time.Millisecond):
	}
	endRun()
	select {
	case err := <-delivered:
		if err != nil {
			t.Fatalf("answer = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the resolution still holds the session after the run ended")
	}
	if r := result(t, got); r.answer.Action != elicit.ActionDecline {
		t.Fatalf("Ask = %+v", r)
	}
}
```

`TestARefusedAnswerLeavesNoValueInTheReplayStore` calls the mux directly, not over HTTP:
- with the registry set, `idempotencyMutation` stamps the service identity on a context that has none (`idempotency_http.go:224-249`, `:270-272`), and the run is owned by `localIdentityID`, so the request carries the owner the way `RequireAuth` would;
- and a direct call keeps `memoryHTTPRegistry`'s unsynchronized fields on one goroutine for the race detector.

Create `internal/agui/server_run_elicitation_e2e_test.go`:

```go
package agui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/elicit"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/runner"
)

// server_run_elicitation_e2e_test.go drives a real runner, a real MCP mount and
// the real detached handler: a server's form reaches the SSE stream, the answer
// is POSTed, and the server greets the name it received.

func formSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"name": map[string]any{"type": "string"}},
		"required":   []any{"name"},
	}
}

func greet(reply *sdkmcp.ElicitResult) *sdkmcp.CallToolResult {
	text := reply.Action
	if reply.Action == elicit.ActionAccept {
		text = fmt.Sprintf("hello %v", reply.Content["name"])
	}
	return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: text}}}
}

// formServer serves one tool, ask_name, over streamable HTTP. Classic narrows the
// server to 2025-11-25, the last protocol that allows elicitation/create during a
// call (go-sdk@v1.8.0 mcp/server.go:1619-1627).
func formServer(t *testing.T, classic bool) string {
	t.Helper()
	var opts *sdkmcp.ServerOptions
	if classic {
		opts = &sdkmcp.ServerOptions{SupportedProtocolVersions: []string{"2025-11-25"}}
	}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "forms", Version: "0.0.1"}, opts)
	tool := &sdkmcp.Tool{Name: "ask_name", Description: "Asks the operator for a name.", InputSchema: map[string]any{"type": "object"}}
	server.AddTool(tool, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		if classic {
			reply, err := req.Session.Elicit(ctx, &sdkmcp.ElicitParams{Mode: "form", Message: "what is your name", RequestedSchema: formSchema()})
			if err != nil {
				return nil, err
			}
			return greet(reply), nil
		}
		if reply, ok := req.Params.InputResponses["who"].(*sdkmcp.ElicitResult); ok {
			return greet(reply), nil
		}
		return &sdkmcp.CallToolResult{InputRequests: sdkmcp.InputRequestMap{
			"who": &sdkmcp.ElicitParams{Mode: "form", Message: "what is your name", RequestedSchema: formSchema()},
		}}, nil
	})
	ts := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil))
	t.Cleanup(func() {
		for session := range server.Sessions() {
			_ = session.Close()
		}
		ts.Close()
	})
	return ts.URL
}

// unreachableFallback fails the test if a detached cockpit run's form ever falls
// back to decline-and-surface.
type unreachableFallback struct{ t *testing.T }

func (f unreachableFallback) AskOperator(context.Context, elicit.Question) (string, map[string]any, error) {
	f.t.Errorf("the fallback consent was asked: a detached run must be asked through its own asker")
	return elicit.ActionDecline, nil, nil
}

// mountForms mounts the fixture the way production mounts a managed server.
func mountForms(t *testing.T, reg *tools.Registry, url string) string {
	t.Helper()
	server := mcp.ManagedServer{URL: url, Type: mcp.ServerTypeStreamableHTTP, Env: []string{"MCP_OAUTH_DISABLED=true"}}
	closer, names, _, err := mcptools.MountManagedServerWithOptions(context.Background(), context.Background(), reg, "forms", server,
		mcptools.MountOptions{Egress: mcp.RuntimeEgressPolicy(false, server), Elicitation: unreachableFallback{t}})
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	t.Cleanup(func() { _ = closer() })
	if len(names) != 1 {
		t.Fatalf("mounted %v, want one tool", names)
	}
	// The process grants only maxAlwaysLoadedMCPSlots = 2 always-loaded slots
	// (mcptools bridge_deferral.go grantLoadedSlot), and each subtest spends one, so
	// a rerun would find the tool deferred. The form is under test, not the
	// deferral: load the tool as the memory capture test does.
	if tool, ok := reg.Get(names[0]); ok && tool.Spec().Deferred {
		reg.Adopt([]tools.Tool{alwaysLoadedTool{tool}})
	}
	return names[0]
}

type alwaysLoadedTool struct{ tools.Tool }

func (t alwaysLoadedTool) Spec() tools.Spec {
	spec := t.Tool.Spec()
	spec.Deferred = false
	return spec
}

// newRealFormRunner is newRealSteerRunner without the steer inbox and with a
// short wallclock, over a registry the test has already mounted into. PreviewCap
// keeps the server's reply inline: at 0 every byte spills to a sidecar and the
// model sees only a read_tool_output pointer.
func newRealFormRunner(t *testing.T, client llm.Client, reg *tools.Registry, wallclockSec int) (*runner.Runner, *steerE2EConvStore) {
	t.Helper()
	conv := newSteerE2EConvStore()
	r := runner.New(runner.Deps{
		PreviewCap:      2048,
		RunDir:          t.TempDir(),
		Conv:            conv,
		Pause:           steerE2EPauseStore{},
		ApprovalExpiry:  steerE2EPauseStore{},
		Identity:        steerE2EIdentityStore{},
		CacheMetrics:    steerE2ECacheMetricStore{},
		ToolInvocations: steerE2EToolInvocationStore{},
		Client:          agenttest.TitleClient{Main: client, Title: agenttest.NewFakeClient(agenttest.TextChunks("stop", "Form test conversation"))},
		Registry:        reg,
		LLM:             llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768, LoopMaxWallclockSec: wallclockSec},
		TitleTimeout:    2 * time.Second,
		StopTimeout:     2 * time.Second,
	})
	return r, conv
}

// sseData streams an SSE body's data payloads until the body ends.
func sseData(body io.Reader) <-chan string {
	out := make(chan string, 1024)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(body)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				out <- data
			}
		}
	}()
	return out
}

func awaitQuestion(t *testing.T, frames <-chan string) elicitationFrame {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				t.Fatal("the stream ended before the form arrived")
			}
			var frame struct {
				Name  string          `json:"name"`
				Value json.RawMessage `json:"value"`
			}
			if json.Unmarshal([]byte(data), &frame) != nil || frame.Name != ElicitationEventName {
				continue
			}
			var q elicitationFrame
			if err := json.Unmarshal(frame.Value, &q); err != nil {
				t.Fatalf("decode %s: %v", ElicitationEventName, err)
			}
			return q
		case <-timeout:
			t.Fatalf("no %s frame within 10s", ElicitationEventName)
		}
	}
}

func drainFrames(t *testing.T, frames <-chan string) string {
	t.Helper()
	var b strings.Builder
	timeout := time.After(20 * time.Second)
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				return b.String()
			}
			b.WriteString(data + "\n")
		case <-timeout:
			t.Fatalf("the run did not end within 20s; so far:\n%s", b.String())
		}
	}
}

// Review Focus 1 end to end, and Review Focus 4's replay. The operator takes 3 s,
// over a 1 s MCP call bound and a 2 s run wallclock.
func TestDetachedRunAnswersAnMCPFormWhileBothClocksStop(t *testing.T) {
	for _, mode := range []struct {
		name    string
		classic bool
	}{{"mrtr", false}, {"classic", true}} {
		t.Run(mode.name, func(t *testing.T) {
			t.Setenv("AURA_MCP_CALL_TIMEOUT_SEC", "1")
			reg := tools.NewRegistry()
			reg.Register(tools.TextResponse{})
			toolName := mountForms(t, reg, formServer(t, mode.classic))
			client := agenttest.NewFakeClient(
				agenttest.ToolCallTurn(agenttest.MakeToolCall("call-1", toolName, "{}")),
				agenttest.ToolCallTurn(agenttest.MakeToolCall("call-2", "text_response", `{"text":"done"}`)),
			)
			r, conv := newRealFormRunner(t, client, reg, 2)
			convID, err := r.NewConversationWithID(context.Background(), uuid.NewString())
			if err != nil {
				t.Fatalf("NewConversationWithID: %v", err)
			}
			_, srv := newDetachTestServer(t, r, conv, ServerConfig{})

			resp := postRun(t, srv, steerRunPayload(convID))
			defer resp.Body.Close()
			frames := sseData(resp.Body)
			q := awaitQuestion(t, frames)
			if q.Server != "forms" || q.Tool != "ask_name" || len(q.Fields) != 1 || q.Fields[0].Name != "name" {
				t.Fatalf("question = %+v", q)
			}

			time.Sleep(3 * time.Second)
			if status, body := answerPost(t, srv, q.RunID, q.ID, `{"action":"accept","content":{"name":"Ada"}}`); status != http.StatusAccepted {
				t.Fatalf("answer = %d %s, want 202", status, body)
			}
			rest := drainFrames(t, frames)
			if !strings.Contains(rest, `"type":"RUN_FINISHED"`) || !strings.Contains(rest, ElicitationResolvedEventName) {
				t.Fatalf("the run did not finish with the form resolved:\n%s", rest)
			}
			reqs := client.RecordedRequests()
			if len(reqs) < 2 || !strings.Contains(joinMessageContents(reqs[1].Messages), "hello Ada") {
				t.Fatalf("the model never saw the server's greeting: %+v", reqs)
			}

			replay := readFullBody(t, mustGet(t, srv.URL+"/agent/runs/"+q.RunID+"/events"))
			if strings.Count(replay, `"name":"`+ElicitationEventName+`"`) != 1 || strings.Count(replay, `"name":"`+ElicitationResolvedEventName+`"`) != 1 {
				t.Fatalf("a replay must carry the question and its resolution once each:\n%s", replay)
			}
		})
	}
}

// sleepyTool waits 3 s holding no clock: the control that shows the harness's 2 s
// wallclock is real, without which the held test above would prove nothing.
type sleepyTool struct{}

func (sleepyTool) Spec() tools.Spec {
	return tools.Spec{Name: "sleepy", Summary: "waits three seconds", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (sleepyTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	select {
	case <-ctx.Done():
		return tools.ToolResult{}, ctx.Err()
	case <-time.After(3 * time.Second):
		return tools.ToolResult{Preview: "slept"}, nil
	}
}

func TestTheShortWallclockCutsAnUnheldTool(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(sleepyTool{})
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("call-1", "sleepy", "{}")),
		agenttest.ToolCallTurn(agenttest.MakeToolCall("call-2", "text_response", `{"text":"done"}`)),
	)
	r, conv := newRealFormRunner(t, client, reg, 2)
	convID, err := r.NewConversationWithID(context.Background(), uuid.NewString())
	if err != nil {
		t.Fatalf("NewConversationWithID: %v", err)
	}
	_, srv := newDetachTestServer(t, r, conv, ServerConfig{})

	readFullBody(t, postRun(t, srv, steerRunPayload(convID)))
	for i, req := range client.RecordedRequests() {
		if strings.Contains(joinMessageContents(req.Messages), "slept") {
			t.Fatalf("request %d carries the tool's result: the 2 s wallclock never cut the 3 s tool", i)
		}
	}
}

func mustGet(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}
```

- [ ] **Step 2: Run them to verify they fail.** Go: `go test -count=1 ./internal/agui/`.

Expected: build FAIL, with `undefined: elicitationFrame`, `sess.questions undefined (type *RunSession has no field or method questions)`, `undefined: ElicitationEventName` and `undefined: ElicitationResolvedEventName`, then `too many errors`.

- [ ] **Step 3: Implement.** Create `internal/agui/run_elicitation.go`:

```go
package agui

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/elicit"
)

// run_elicitation.go is a detached run's elicit.Asker. A mounted MCP server's form
// is published into the run's own stream, and the answer arrives on POST
// /agent/runs/{runID}/elicitations/{id} (server_run_elicitation.go). A reload gets
// an open form back from the replay ring, or, once the ring has rotated past it,
// from GET /agent/runs/{runID}/elicitations. Nothing is persisted: the server's
// request does not survive a restart either.

// The CUSTOM events a question and its outcome travel as. Neither carries an
// answer's values: only the action reaches the stream.
const (
	ElicitationEventName         = "aura.elicitation"
	ElicitationResolvedEventName = "aura.elicitation_resolved"
)

// elicitationFrame is the question as the cockpit receives it. run_id rides along
// because the answer is posted to the run, and a card restored from a replay has
// no other place to read it from.
type elicitationFrame struct {
	RunID string `json:"run_id"`
	elicit.Question
}

type elicitationResolvedFrame struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	// Expired tells an expiry from a cancel for another reason: both are a cancel to
	// the server, and only the card needs the difference.
	Expired bool `json:"expired,omitempty"`
}

var (
	errQuestionUnknown = errors.New("question not found")
	errQuestionClosed  = errors.New("question already resolved")
)

type pendingQuestion struct {
	q      elicit.Question
	answer chan elicit.Answer // buffered: the closer never waits for Ask
}

// runQuestions holds a run's open questions. mu also orders the stream: a question
// is published under it and so is its resolution, so a resolution can never reach
// a subscriber before the question it closes, and cancelAll, which runs before the
// session turns terminal, waits for any close already publishing.
type runQuestions struct {
	sess *RunSession

	mu sync.Mutex
	// runCtx bounds the resolution frames. A resolution must still reach the stream
	// after the call that asked has ended, and must give up once the run has, so a
	// tab that stops reading cannot hold mu past the run's own cap.
	runCtx  context.Context
	pending map[string]*pendingQuestion
	closed  map[string]struct{}
	ended   bool
}

func newRunQuestions(sess *RunSession) *runQuestions {
	return &runQuestions{
		sess: sess, runCtx: context.Background(),
		pending: map[string]*pendingQuestion{}, closed: map[string]struct{}{},
	}
}

// bind ties the run's resolution frames to runCtx and returns the asker to install
// on it. A session built without bind, as the tests build one, uses Background.
func (rq *runQuestions) bind(runCtx context.Context) *runQuestions {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	rq.runCtx = runCtx
	return rq
}

// Ask implements elicit.Asker. It returns when the operator answers, when ctx ends
// (with context.Cause(ctx)), or at once for a refusal, a run that has ended, or a
// run already showing elicit.MaxOpenQuestions forms. An answer delivered as ctx
// ends wins: it was already published as the outcome.
func (rq *runQuestions) Ask(ctx context.Context, q elicit.Question) (elicit.Answer, error) {
	q.ID = uuid.NewString()
	p := &pendingQuestion{q: q, answer: make(chan elicit.Answer, 1)}
	rq.mu.Lock()
	if rq.ended {
		rq.mu.Unlock()
		return elicit.Answer{Action: elicit.ActionCancel}, nil
	}
	// Each session is capped on its own (mcptools), so a run that reaches several
	// servers is capped here. Past it nothing is shown: a card per request would be
	// the flood itself.
	if len(rq.pending) >= elicit.MaxOpenQuestions {
		rq.mu.Unlock()
		return elicit.Answer{Action: elicit.ActionDecline}, nil
	}
	rq.pending[q.ID] = p
	rq.sess.publish(ctx, events.NewCustomEvent(ElicitationEventName, events.WithValue(rq.frame(q))))
	rq.mu.Unlock()

	if q.Refusal != "" {
		rq.close(q.ID, elicit.ActionDecline, false)
		return elicit.Answer{Action: elicit.ActionDecline}, nil
	}
	select {
	case a := <-p.answer:
		return a, nil
	case <-ctx.Done():
		cause := context.Cause(ctx)
		if !rq.close(q.ID, elicit.ActionCancel, errors.Is(cause, elicit.ErrExpired)) {
			return <-p.answer, nil
		}
		return elicit.Answer{}, cause
	}
}

func (rq *runQuestions) frame(q elicit.Question) elicitationFrame {
	return elicitationFrame{RunID: rq.sess.RunID, Question: q}
}

// open is every question still waiting, oldest first: they share one timeout, so
// the deadline orders them as they were asked.
func (rq *runQuestions) open() []elicitationFrame {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	frames := make([]elicitationFrame, 0, len(rq.pending))
	for _, p := range rq.pending {
		frames = append(frames, rq.frame(p.q))
	}
	slices.SortFunc(frames, func(a, b elicitationFrame) int { return a.Deadline.Compare(b.Deadline) })
	return frames
}

// answer delivers the operator's answer. An accept that fails the server's schema
// leaves the question open and returns the problem codes.
func (rq *runQuestions) answer(id string, a elicit.Answer) (elicit.FieldErrors, error) {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	p, ok := rq.pending[id]
	if !ok {
		if _, done := rq.closed[id]; done {
			return nil, errQuestionClosed
		}
		return nil, errQuestionUnknown
	}
	if a.Action == elicit.ActionAccept {
		if errs := elicit.Validate(p.q, a.Content); errs != nil {
			return errs, nil
		}
	} else {
		a.Content = nil
	}
	rq.closeLocked(id, a.Action, false)
	p.answer <- a
	return nil, nil
}

// cancelAll resolves every pending question as cancel and refuses new ones: a form
// never outlives its run. The producer calls it as the stream ends, before finish.
func (rq *runQuestions) cancelAll() {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	rq.ended = true
	for id, p := range rq.pending {
		rq.closeLocked(id, elicit.ActionCancel, false)
		p.answer <- elicit.Answer{Action: elicit.ActionCancel}
	}
}

// close resolves a question once, whoever gets there first, and reports whether
// this call was the one.
func (rq *runQuestions) close(id, action string, expired bool) bool {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	return rq.closeLocked(id, action, expired)
}

func (rq *runQuestions) closeLocked(id, action string, expired bool) bool {
	if _, ok := rq.pending[id]; !ok {
		return false
	}
	delete(rq.pending, id)
	rq.closed[id] = struct{}{}
	rq.sess.publish(rq.runCtx, events.NewCustomEvent(ElicitationResolvedEventName,
		events.WithValue(elicitationResolvedFrame{ID: id, Action: action, Expired: expired})))
	return true
}
```

Create `internal/agui/server_run_elicitation.go`:

```go
package agui

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"

	"github.com/chetto1983/aura/internal/elicit"
)

// server_run_elicitation.go carries the operator's side of a mounted MCP server's
// form (run_elicitation.go): POST /agent/runs/{runID}/elicitations/{id} answers
// one, GET /agent/runs/{runID}/elicitations lists those still open. Both resolve
// the run through the same owner-scoped 404 ladder as steer and cancel.

type elicitationAnswerRequest struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content"`
}

var elicitationActions = []string{elicit.ActionAccept, elicit.ActionDecline, elicit.ActionCancel}

func (s *Server) handleRunElicitation(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.resolveRunSession(w, r)
	if !ok {
		return
	}
	if terminal, _ := sess.terminalState(); terminal {
		http.Error(w, "run has ended", http.StatusGone)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRunBodyBytes+1))
	if err != nil || len(body) > maxRunBodyBytes {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	var req elicitationAnswerRequest
	if err := json.Unmarshal(body, &req); err != nil || !slices.Contains(elicitationActions, req.Action) {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// The 422 carries elicit's problem codes and never the submitted value: the
	// idempotency layer keeps this body for its replay (idempotency_http.go).
	fieldErrs, err := sess.questions.answer(r.PathValue("id"), elicit.Answer{Action: req.Action, Content: req.Content})
	switch {
	case errors.Is(err, errQuestionUnknown):
		http.Error(w, "question not found", http.StatusNotFound)
	case errors.Is(err, errQuestionClosed):
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case fieldErrs != nil:
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]any{"errors": fieldErrs})
	default:
		writeJSONStatus(w, http.StatusAccepted, map[string]string{"status": "delivered"})
	}
}

// handleRunElicitations lists the run's open questions, oldest first. A reattach
// whose replay the ring can no longer serve gets a 410 on /events and no frame at
// all, so the cockpit reads the forms from here. A read, like /events.
func (s *Server) handleRunElicitations(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.resolveRunSession(w, r)
	if !ok {
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"questions": sess.questions.open()})
}
```

In `internal/agui/runsession.go`:

1. Replace the `RunSession` comment (47-51), which calls the producer the sole appender, with:

```go
// RunSession is one detached run's identity, replay ring, and live-subscriber set
// (design §2.1). The producer goroutine appends, and so does the run's question
// asker through publish; the producer is the sole caller of finish. Subscribers
// attach at any time via subscribeFrom. All mutable state is guarded by mu —
// append and subscribeFrom serialize on it, which is what makes replay-then-live
// gapless and duplicate-free by construction.
```

2. Add a field after `now func() time.Time`:

```go
	// questions is the run's elicit.Asker (run_elicitation.go), built with the
	// session so it exists before the producer or the route can reach it.
	questions *runQuestions
```

3. In `newRunSession`, bind the literal and set the field:

```go
	s := &RunSession{
		// the existing fields, unchanged
	}
	s.questions = newRunQuestions(s)
	return s
```

4. Replace `append`'s comment (110-117), whose `Producer-only.` is no longer true, with:

```go
// append assigns the next sequence number, writes the event into the ring
// (overwriting the oldest and advancing firstSeq when full), then fans it to every
// live subscriber under the session mutex with the shared pump discipline: a
// non-lifecycle delta drops on a full channel (WARN + metric), a lifecycle frame
// blocks until delivered, ctx-done, or the subscriber unsubscribes (pumpGone → the
// dead entry is pruned). Called by the producer and, through publish, by the run's
// question asker; both serialize on mu, so an asker's frame lands between two
// producer frames and never inside one. Returns false on ctx-cancel (or a terminal
// session — a producer bug, tolerated defensively rather than panicking a detached
// goroutine) so the producer unwinds.
```

5. Add after `append`:

```go
// publish appends a frame that does not come from the turn's own stream. It goes
// through redactEvent like every producer frame, so the ring keeps one rule, but
// redactEvent rewrites only RUN_ERROR (server_project.go): a question frame is
// stored as built, and it carries no operator value to redact.
func (s *RunSession) publish(ctx context.Context, ev events.Event) bool {
	return s.append(ctx, redactEvent(ev))
}
```

In `internal/agui/server_run_detach.go`:

1. Append a paragraph to `detachedRunContext`'s comment, before `func detachedRunContext(`:

```go
//
// This cap stays fixed although the budget's deadline is pausable
// (internal/pausable). An MCP form holds the whole run tree's budget while the
// operator answers it, so this is the one bound left on a server that asks again
// and again; at the default 3600 s it cuts a legitimate run only after some 55
// minutes of forms in one turn.
```

2. After the `Start` error branch (line 106), insert:

```go
	// The run's own asker: a mounted MCP server's form reaches this run's stream
	// and waits for the operator (run_elicitation.go). A non-detached run gets none,
	// the same rule steer follows.
	dctx = elicit.WithAsker(dctx, sess.questions.bind(dctx))
```

3. In `runProducer`, after `defer sess.finish()` (line 143), add:

```go
	// Deferred after finish so it runs before it: every form still open is resolved
	// as cancel while the session can still carry the frame.
	defer sess.questions.cancelAll()
```

4. Add `"github.com/chetto1983/aura/internal/elicit"` to the imports, between `.../encoding/encoder` and `.../internal/runner`.

In `internal/agui/server.go`, after the steer route (line 365):

```go
	// A mounted MCP server's form, answered from the cockpit (run_elicitation.go),
	// and the list of those still open for a tab whose replay no longer reaches them.
	mux.HandleFunc("POST /agent/runs/{runID}/elicitations/{id}", s.handleRunElicitation)
	mux.HandleFunc("GET /agent/runs/{runID}/elicitations", s.handleRunElicitations)
```

In `internal/agui/idempotency_http.go`, insert after the steer entry (line 68), then run `gofmt -w internal/agui/idempotency_http.go`. The new comment ends gofmt's alignment block, so the steer line loses its padding. The result is:

```go
	// The cockpit mid-turn redirect (amendment #132 D-02, T-52-12): a replayed
	// POST with the same Idempotency-Key must not enqueue a second steer.
	"POST /agent/runs/{runID}/steer": httpMutationMeta("agent_run_steer"),
	// A replayed POST with the same Idempotency-Key returns the first answer's
	// response instead of meeting the 409 a second delivery would. Its GET sibling
	// lists open questions and is a read, like /events.
	"POST /agent/runs/{runID}/elicitations/{id}":                  httpMutationMeta("agent_run_elicitation_answer"),
```

- [ ] **Step 4: Run the package.** Go: `go vet ./internal/agui/`, then `go test -race -count=1 ./internal/agui/`, then `go test -race -count=2 -run 'TestDetachedRunAnswersAnMCPFormWhileBothClocksStop|TestTheShortWallclockCutsAnUnheldTool' ./internal/agui/`, then `golangci-lint run ./internal/agui/`.

Expected:
- `ok` for both test runs, and lint reports `0 issues.`.
- Measured on the scratch copy with Tasks 1-6 applied: the package passes under `-race` in 35 s, and the e2e subtests take about 3.2 s (`classic`) and 5 s (`mrtr`).
- The integration test must not skip. It needs no container: the MCP server, the runner and the HTTP server are all in-process.
- `-count=2` is there for the deferral budget. The process grants only `maxAlwaysLoadedMCPSlots = 2` always-loaded slots (`bridge_deferral.go:70-74`, `grantLoadedSlot` at `:97`), and each subtest mounts one server. The second run passes only because `mountForms` adopts an always-loaded copy of the tool.
- `TestEveryRegisteredUnsafeHTTPRouteIsClassified` (`mutation_coverage_test.go:45`) sweeps every unsafe route the mux registers, so it now covers the POST, and the new inventory entry is what keeps it green. Do not delete the entry to watch the sweep fail: a hand mutation is still a mutation, and mutation runs in CI only.
- `wc -l internal/agui/server.go internal/agui/runsession.go internal/agui/server_run_detach.go internal/agui/idempotency_http.go internal/agui/run_elicitation*.go internal/agui/server_run_elicitation*.go` must show every file under 600 lines. Measured: 543, 233, 277, 459, 197, 456, 68 and 307.
- Coverage: Go: `go test -race -count=1 -coverprofile=/tmp/agui.out ./internal/agui/ && go tool cover -func=/tmp/agui.out | grep -E 'run_elicitation|server_run_elicitation'`. Every function must be at 85% or above. Measured: `Ask` 96.0%, `handleRunElicitation` 90.0%, the rest 100%. The one line `Ask` leaves is an answer landing in the same instant the call ends, a race no test can time.

- [ ] **Step 5: Commit.**

```bash
cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH" LEFTHOOK_BIN="$HOME/go/bin/lefthook"
git add internal/agui/run_elicitation.go internal/agui/server_run_elicitation.go internal/agui/run_elicitation_test.go internal/agui/server_run_elicitation_e2e_test.go
git -c core.hooksPath=.git/hooks commit -F - -- internal/agui/run_elicitation.go internal/agui/server_run_elicitation.go internal/agui/run_elicitation_test.go internal/agui/server_run_elicitation_e2e_test.go internal/agui/runsession.go internal/agui/server_run_detach.go internal/agui/server.go internal/agui/idempotency_http.go <<'EOF'
feat(agui): carry an MCP server's form in the run and take the answer

A detached run now installs its own elicit.Asker. A mounted server's
form is published as aura.elicitation into the run's replay ring. The
answer arrives on POST /agent/runs/{runID}/elicitations/{id},
owner-scoped like steer:
- 202 when delivered;
- 422 with a problem code per field, and the question stays open;
- 409 once the question is closed;
- 410 on an ended run.

The 422 names codes, never values: the idempotency layer keeps the
response body for 30 days as its replay.

GET /agent/runs/{runID}/elicitations lists the open forms. A reload
after the ring has rotated gets 410 on /events and no frame at all,
so the cockpit reads its forms from there.

aura.elicitation_resolved closes each question once. Its cause is the
answer, the expiry (a cancel flagged expired), the call ending or the
run ending. Resolutions are published under the question lock on the
run's own context, so a tab that stops reading cannot hold the
session past the run. A run shows at most four forms at once.

The detached run's one-hour cap stays fixed on purpose. It is the one
bound left on a server that asks again and again.

Measured: a 3 s operator over a 1 s call bound and a 2 s run wallclock,
classic and multi-round-trip, through a real runner and a real mount.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---
### Task 7: The question frame, and ask_user drawn in it

The operator chose **option V** (spec §Revisions, 203c62be8):
- register the `@tool-ui` registry;
- port Question Flow's markup and classes into Aura's own files, translated and tested, each under 600 lines;
- install no vendored file until spec 2.

**Files:**
- Modify: `web/components.json`, adding `@tool-ui` to `registries` (line 22).
- Modify: `THIRD_PARTY_NOTICES.md`, adding an `assistant-ui/tool-ui` entry after `smixs/visual-skills` (line 31).
- Create:
  - `web/src/questions/QuestionCard.tsx`
  - `web/src/questions/QuestionOptions.tsx`
  - `web/src/questions/QuestionReceipt.tsx`
  - `web/src/questions/CancelControl.tsx`
  - `web/src/i18n/resources.questions.ts`
- Modify: `web/src/i18n/resources.ts`, which imports the bundle (after line 31) and spreads it into both locales after `...updateEn,` (196) and `...updateIt,` (468).
- Modify: `web/src/approvals/InlineApprovalCard.tsx`, rewritten in full (371 lines today, 291 after).
- Modify: `web/src/approvals/approvalState.ts`, adding `isDestructiveApproval` after `isTerminal` (15) and `offersOnlyScopes` at the end (76).
- Test:
  - Create `web/src/questions/__tests__/QuestionFrame.test.tsx`.
  - Modify `web/src/approvals/__tests__/InlineApprovalCard.test.tsx`: tests 89-97 and 123-143 change; six tests go after the last one (396).
  - Modify `web/src/approvals/__tests__/ThreadApprovalCards.test.tsx` at lines 132, 206 and 213.
  - Modify `web/src/approvals/__tests__/approvalState.test.ts`: the import (2) and two `describe` blocks at the end (73).
  - Modify `web/src/chat/__tests__/ExternalStoreChat.approvals.test.tsx` at lines 168, 195, 207, 218, 263 and 284, and `web/src/chat/__tests__/ExternalStoreChat.test.tsx:451`.
  - Modify `web/e2e/chat-calm-prism.spec.ts:232`, and `web/e2e/mcp-cockpit-live.spec.ts:210-212,229`. The live spec runs only with `AURA_E2E_REAL_AGENT=1` (line 7).
- Modify: `web/stryker.config.json`, adding five files to `mutate` after `src/approvals/approvalState.ts` (line 23).
- Modify: `web/vitest.stryker.config.ts`, adding two suites after `ThreadApprovalCards.test.tsx` (line 7). Stryker runs only the suites listed there (`include: [...mutationTests]`, line 74). A mutated file whose suite is missing scores every mutant NoCoverage, as the comment at 56-58 records. `approvalState.test.ts` is added too: `approvalState.ts` is mutated today (`stryker.config.json:23`) but its suite was never listed.

`web/e2e/chat.spec.ts` is not touched. Its fixture's approval has no options (`chat.spec.ts:296-307`), and under the approvals ruling such an approval keeps its free-text reply and **Answer**. The spec at 473-485 and its header comment at 14 stay true.

**Interfaces:**
- Produces, used by Task 8:
  - `QuestionCard` with props `QuestionCardProps`:
    - `titleId: string`, `title: ReactNode`;
    - `description?: ReactNode`, `descriptionId?: string`;
    - `icon?: ReactNode`, `header?: ReactNode`;
    - `step?: {current: number; total: number}`;
    - `variant?: 'default' | 'destructive'`;
    - `footer?: ReactNode`, `status?: ReactNode`;
    - `dataAttributes?: Readonly<Record<\`data-${string}\`, string>>`;
    - `children?: ReactNode`.
  - `QuestionOptions` with props `{labelledBy: string, describedBy?: string, options: readonly QuestionOption[], mode: 'single' | 'multi', selected: ReadonlySet<string>, disabled?: boolean, onToggle(id: string): void, onSubmit?(): void}`, where `QuestionOption = {id, label, description?}`.
  - `QuestionReceipt` with props `{tone: ReceiptTone, label: string, summary?: readonly ReceiptLine[], announce?: boolean}`, where `ReceiptTone = 'success' | 'neutral' | 'warning' | 'danger'` and `ReceiptLine = {label: string, value: string}`.
  - `CancelControl` with props `{isStreaming?: boolean | undefined, disabled: boolean, labels: CancelLabels, onCancel(): void}`, where `CancelLabels = {cancel, confirm, yes, no}`.
  - The i18n bundle `questionCard.*`, Task 8's keys included.
- Kept: `InlineApprovalCard`'s props and resolve lifecycle are unchanged (`onResolutionStarted`, `onResolved`, `onResolutionFailed`, attempt ids). So `useThreadApprovals` and `useApprovalFocus` (`[data-approval-token]`) need no change.

**What moves where:**

| Today | After |
|---|---|
| option buttons that submit on click (`InlineApprovalCard.tsx:168-185`) | radio rows plus a pill **Answer** that stays grey until a row is chosen (spec §Cockpit). Rows are keyed by index, because nothing makes two options' values distinct. |
| no options: a free-text reply plus **Answer**, for every kind (`:186-204`) | unchanged, in the new frame (the approvals ruling: ask_user's behaviour stays) |
| an approval's options | rows. The pill says **Approve** only when every option is a gateway scope (`offersOnlyScopes`). A model's own options, such as Yes and No, keep **Answer**: Approve on "No" would say the opposite of what is sent. |
| a gateway approval graded Destructive | the destructive variant when `presentation.params.risk === 'destructive'` (`scoring.Destructive`, `internal/scoring/scoring.go:26`) |
| the inline Cancel confirmation (`:95-108`, `:231-275`) | `CancelControl`, shared with Task 8, with the same copy, focus moves and `min-h-11` |
| `TerminalChip` (`:333-371`) | `QuestionReceipt`, which adds the answer given under an **Answered** chip; a success chip shows a check instead of the dot |
| the review line on every approval card, closed ones included (`ApprovalFrame`, `:326-328`) | drawn on a pending approval only: it asks for a decision, and a closed card is a read-only receipt (spec §Cockpit, "After answering") |

- [ ] **Step 1: Write the failing tests.** Create `web/src/questions/__tests__/QuestionFrame.test.tsx`:

```tsx
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import { CancelControl } from '../CancelControl';
import { QuestionCard } from '../QuestionCard';
import { QuestionOptions, type QuestionOption } from '../QuestionOptions';
import { QuestionReceipt } from '../QuestionReceipt';

const OPTIONS: QuestionOption[] = [
  { id: 'rome', label: 'Rome' },
  { id: 'milan', label: 'Milan', description: 'the north' },
  { id: 'turin', label: 'Turin' },
];

const LABELS = {
  cancel: 'Cancel run',
  confirm: 'Stop this run?',
  yes: 'Stop run',
  no: 'Keep running',
};

describe('QuestionCard', () => {
  it('is a form named by its title, with the description kept as plain, wrapped text', () => {
    render(
      <QuestionCard
        titleId="t"
        title="Pick a city"
        descriptionId="d"
        description={'line one\n<b>two</b>'}
      >
        <span>body</span>
      </QuestionCard>,
    );
    const form = screen.getByRole('form', { name: 'Pick a city' });
    expect(form.getAttribute('data-slot')).toBe('card');
    expect(form.getAttribute('data-variant')).toBe('default');
    expect(form.getAttribute('aria-describedby')).toBe('d');
    const description = screen.getByText((_, el) => el?.textContent === 'line one\n<b>two</b>');
    expect(description.className).toMatch(/whitespace-pre-wrap/);
    expect(form.querySelector('b')).toBeNull();
  });

  it('shows the step label and the segmented bar only on a form of more than one step', () => {
    const { rerender } = render(
      <QuestionCard titleId="t" title="One" step={{ current: 1, total: 1 }} />,
    );
    expect(screen.queryByRole('progressbar')).toBeNull();
    rerender(<QuestionCard titleId="t" title="Two" step={{ current: 2, total: 3 }} />);
    expect(screen.getByText('Step 2 of 3')).toBeTruthy();
    const bar = screen.getByRole('progressbar');
    expect(bar.getAttribute('aria-valuenow')).toBe('2');
    expect(bar.getAttribute('aria-valuemax')).toBe('3');
    expect(bar.querySelectorAll('.scale-x-100')).toHaveLength(2);
  });

  it('carries its variant and the data attributes an adapter asks for', () => {
    render(
      <QuestionCard
        titleId="t"
        title="Risky"
        variant="destructive"
        dataAttributes={{ 'data-approval-token': 'tok' }}
      />,
    );
    const form = screen.getByRole('form', { name: 'Risky' });
    expect(form.getAttribute('data-variant')).toBe('destructive');
    expect(form.getAttribute('data-approval-token')).toBe('tok');
  });
});

describe('QuestionOptions', () => {
  function renderOptions(mode: 'single' | 'multi', selected: ReadonlySet<string> = new Set()) {
    const onToggle = vi.fn();
    const onSubmit = vi.fn();
    render(
      <>
        <span id="lbl">Cities</span>
        <span id="hint">Pick one</span>
        <QuestionOptions
          labelledBy="lbl"
          describedBy="hint"
          options={OPTIONS}
          mode={mode}
          selected={selected}
          onToggle={onToggle}
          onSubmit={onSubmit}
        />
      </>,
    );
    return { onToggle, onSubmit };
  }

  it('is a listbox of options that marks the selected row', () => {
    renderOptions('multi', new Set(['milan']));
    const list = screen.getByRole('listbox', { name: 'Cities' });
    expect(list.getAttribute('aria-multiselectable')).toBe('true');
    expect(list.getAttribute('aria-describedby')).toBe('hint');
    expect(screen.getByRole('option', { name: /Milan/ }).getAttribute('aria-selected')).toBe(
      'true',
    );
    expect(screen.getByRole('option', { name: 'Rome' }).getAttribute('aria-selected')).toBe(
      'false',
    );
    expect(screen.getByText('the north')).toBeTruthy();
  });

  it('moves with the arrow keys, Home and End, and wraps', () => {
    renderOptions('single');
    const list = screen.getByRole('listbox');
    const [rome, milan, turin] = screen.getAllByRole('option');
    fireEvent.keyDown(list, { key: 'ArrowDown' });
    expect(document.activeElement).toBe(milan);
    fireEvent.keyDown(list, { key: 'End' });
    expect(document.activeElement).toBe(turin);
    fireEvent.keyDown(list, { key: 'ArrowDown' });
    expect(document.activeElement).toBe(rome);
    fireEvent.keyDown(list, { key: 'ArrowUp' });
    expect(document.activeElement).toBe(turin);
    fireEvent.keyDown(list, { key: 'Home' });
    expect(document.activeElement).toBe(rome);
  });

  it('selects with Space or Enter, and Enter on the chosen single row submits', () => {
    const { onToggle, onSubmit } = renderOptions('single', new Set(['rome']));
    const list = screen.getByRole('listbox');
    fireEvent.keyDown(list, { key: 'Enter' });
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onToggle).not.toHaveBeenCalled();
    fireEvent.keyDown(list, { key: 'ArrowDown' });
    fireEvent.keyDown(list, { key: ' ' });
    expect(onToggle).toHaveBeenCalledWith('milan');
    fireEvent.click(screen.getByRole('option', { name: 'Turin' }));
    expect(onToggle).toHaveBeenLastCalledWith('turin');
  });

  it('never submits from a multi-choice list', () => {
    const { onToggle, onSubmit } = renderOptions('multi', new Set(['rome']));
    fireEvent.keyDown(screen.getByRole('listbox'), { key: 'Enter' });
    expect(onToggle).toHaveBeenCalledWith('rome');
    expect(onSubmit).not.toHaveBeenCalled();
  });
});

describe('QuestionReceipt', () => {
  it('shows the chip in its tone and the answer given', () => {
    render(
      <QuestionReceipt
        tone="success"
        label="Answered."
        summary={[{ label: 'Your answer', value: 'Milan' }]}
      />,
    );
    expect(screen.getByText('Answered.').closest('[data-tone]')?.getAttribute('data-tone')).toBe(
      'success',
    );
    expect(screen.getByText('Your answer')).toBeTruthy();
    expect(screen.getByText('Milan')).toBeTruthy();
  });

  it('announces itself only when asked to', () => {
    const { rerender } = render(<QuestionReceipt tone="warning" label="Expired." />);
    expect(screen.queryByRole('status')).toBeNull();
    rerender(<QuestionReceipt tone="warning" label="Expired." announce />);
    expect(screen.getByRole('status').textContent).toContain('Expired.');
  });
});

describe('CancelControl', () => {
  it('cancels at once while idle', () => {
    const onCancel = vi.fn();
    render(
      <CancelControl isStreaming={false} disabled={false} labels={LABELS} onCancel={onCancel} />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Cancel run' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('asks first while streaming, and Escape keeps the run going', async () => {
    const onCancel = vi.fn();
    render(<CancelControl isStreaming disabled={false} labels={LABELS} onCancel={onCancel} />);
    fireEvent.click(screen.getByRole('button', { name: 'Cancel run' }));
    const keep = screen.getByRole('button', { name: 'Keep running' });
    await waitFor(() => {
      expect(document.activeElement).toBe(keep);
    });
    expect(keep.className).toContain('min-h-11');
    fireEvent.keyDown(keep, { key: 'Escape' });
    await waitFor(() => {
      expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Cancel run' }));
    });
    expect(onCancel).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Cancel run' }));
    fireEvent.click(screen.getByRole('button', { name: 'Stop run' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });
});
```

In `web/src/approvals/__tests__/InlineApprovalCard.test.tsx`:
- the first two hunks below change the first test (89-97) and `Answer (option) resolves …` (123-143);
- the third adds six tests after the last one.

The two changed tests change because of the spec, not the tests: a choice is now a radio row plus a pill **Answer**, so a click on a row no longer submits. Every other assertion in the file stays as it is. That includes:
- the resolve payloads and the attempt identities;
- the Cancel confirmation and its focus moves;
- the terminal and failure states;
- the tokens never shown;
- `whitespace-pre-wrap` and `data-slot="card"`.

The file follows the repo's `getByRole<HTMLButtonElement>(…)` form rather than a cast, which `typescript/no-unnecessary-type-assertion` refuses.

```diff
--- a/web/src/approvals/__tests__/InlineApprovalCard.test.tsx
+++ b/web/src/approvals/__tests__/InlineApprovalCard.test.tsx
@@ -86,14 +86,16 @@
     vi.unstubAllGlobals();
   });
 
-  it('renders the backend question VERBATIM + option buttons', () => {
+  it('renders the backend question VERBATIM + one row per option', () => {
     renderCard({
       approval: approval({ token: 't-1', conversation_id: 'c-1', options: ['Rome', 'Milan'] }),
     });
     // The question string is rendered as-is, no client-side rewrite.
     expect(screen.getByText('Which city should I check?')).toBeTruthy();
-    expect(screen.getByRole('button', { name: 'Rome' })).toBeTruthy();
-    expect(screen.getByRole('button', { name: 'Milan' })).toBeTruthy();
+    expect(screen.getByRole('option', { name: 'Rome' })).toBeTruthy();
+    expect(screen.getByRole('option', { name: 'Milan' })).toBeTruthy();
+    const answer = screen.getByRole<HTMLButtonElement>('button', { name: 'Answer' });
+    expect(answer.disabled).toBe(true);
   });
 
   it('renders a free-text input when the pause offers no options', () => {
@@ -120,19 +122,22 @@
     });
   });
 
-  it('Answer (option) resolves {action:"accept", content} → answered terminal chip', async () => {
+  it('Answer (option) resolves {action:"accept", content} → answered receipt with the answer given', async () => {
     const onResolved = vi.fn();
     renderCard({
       approval: approval({ token: 't-1', conversation_id: 'c-1', options: ['Rome', 'Milan'] }),
       onResolved,
     });
-    fireEvent.click(screen.getByRole('button', { name: 'Milan' }));
+    fireEvent.click(screen.getByRole('option', { name: 'Milan' }));
+    expect(calls).toHaveLength(0);
+    fireEvent.click(screen.getByRole('button', { name: 'Answer' }));
     await waitFor(() => {
       expect(screen.getByText('Answered.')).toBeTruthy();
     });
     expect(screen.getByText('Answered.').closest('[data-tone]')?.getAttribute('data-tone')).toBe(
       'success',
     );
+    expect(screen.getByText('Milan')).toBeTruthy();
     expect(calls).toHaveLength(1);
     expect(calls[0]?.url).toContain('/api/approvals/t-1/resolve');
     expect(calls[0]?.body).toEqual({ action: 'accept', content: 'Milan' });
@@ -394,4 +399,119 @@
     expect(calls.at(-1)?.url).toContain('/failure%20secret%2F3/resolve');
     expect(calls.at(-1)?.body).toEqual({ action: 'accept', content: '' });
   });
+
+  it('Enter on the chosen row answers, from the keyboard alone', async () => {
+    renderCard({
+      approval: approval({ token: 't-1', conversation_id: 'c-1', options: ['Rome', 'Milan'] }),
+    });
+    const list = screen.getByRole('listbox');
+    fireEvent.keyDown(list, { key: 'ArrowDown' });
+    fireEvent.keyDown(list, { key: 'Enter' });
+    fireEvent.keyDown(list, { key: 'Enter' });
+    await waitFor(() => {
+      expect(calls).toHaveLength(1);
+    });
+    expect(calls[0]?.body).toEqual({ action: 'accept', content: 'Milan' });
+  });
+
+  it('an approval shows the gateway scopes as a single choice and Approve sends the chosen scope', async () => {
+    renderCard({
+      approval: approval({
+        token: 't-scope',
+        conversation_id: 'c-1',
+        kind: 'approval',
+        options: [
+          { label: 'Approve once', value: 'gateway_scope:once:shell_exec' },
+          { label: 'Approve for this conversation', value: 'gateway_scope:session:shell_exec' },
+        ],
+      }),
+    });
+    const approve = screen.getByRole<HTMLButtonElement>('button', { name: 'Approve' });
+    expect(approve.disabled).toBe(true);
+    fireEvent.click(
+      screen.getByRole('option', { name: 'Approve shell_exec for this conversation' }),
+    );
+    expect(approve.disabled).toBe(false);
+    fireEvent.click(approve);
+    await waitFor(() => {
+      expect(calls).toHaveLength(1);
+    });
+    expect(calls[0]?.body).toEqual({
+      action: 'accept',
+      content: 'gateway_scope:session:shell_exec',
+    });
+  });
+
+  it('a gateway approval graded destructive draws the destructive variant', () => {
+    renderCard({
+      approval: approval({
+        token: 't-rm',
+        conversation_id: 'c-1',
+        kind: 'approval',
+        question: 'Approve shell_exec?',
+        options: [{ label: 'Approve once', value: 'gateway_scope:once:shell_exec' }],
+        presentation: {
+          key: 'approval.gateway.mutation',
+          params: { tool: 'shell_exec', risk: 'destructive', args: 'rm -rf /tmp/x' },
+        },
+      }),
+    });
+    expect(screen.getByRole('form').getAttribute('data-variant')).toBe('destructive');
+    expect(screen.getByRole('button', { name: 'Approve' }).className).toContain('bg-destructive');
+  });
+
+  // The spec keeps ask_user's behaviour: an approval with no options still takes a reply.
+  it('an approval with no options keeps its free-text reply and Answer', async () => {
+    renderCard({
+      approval: approval({ token: 't-plain', conversation_id: 'c-1', kind: 'approval' }),
+    });
+    expect(screen.queryByRole('button', { name: 'Approve' })).toBeNull();
+    fireEvent.change(screen.getByPlaceholderText('Type your answer'), {
+      target: { value: 'yes, go ahead' },
+    });
+    fireEvent.click(screen.getByRole('button', { name: 'Answer' }));
+    await waitFor(() => {
+      expect(calls).toHaveLength(1);
+    });
+    expect(calls[0]?.body).toEqual({ action: 'accept', content: 'yes, go ahead' });
+  });
+
+  it('an approval whose options are not gateway scopes says Answer, not Approve', async () => {
+    renderCard({
+      approval: approval({
+        token: 't-yn',
+        conversation_id: 'c-1',
+        kind: 'approval',
+        options: ['Yes', 'No'],
+      }),
+    });
+    expect(screen.queryByRole('button', { name: 'Approve' })).toBeNull();
+    fireEvent.click(screen.getByRole('option', { name: 'No' }));
+    fireEvent.click(screen.getByRole('button', { name: 'Answer' }));
+    await waitFor(() => {
+      expect(calls).toHaveLength(1);
+    });
+    expect(calls[0]?.body).toEqual({ action: 'accept', content: 'No' });
+  });
+
+  it('two options with one value are still two rows, and the chosen one is shown', async () => {
+    renderCard({
+      approval: approval({
+        token: 't-dup',
+        conversation_id: 'c-1',
+        options: [
+          { label: 'Keep it', value: 'keep' },
+          { label: 'Keep it for now', value: 'keep' },
+        ],
+      }),
+    });
+    fireEvent.click(screen.getByRole('option', { name: 'Keep it for now' }));
+    expect(screen.getByRole('option', { name: 'Keep it' }).getAttribute('aria-selected')).toBe(
+      'false',
+    );
+    fireEvent.click(screen.getByRole('button', { name: 'Answer' }));
+    await screen.findByText('Answered.');
+    expect(screen.getByText('Keep it for now').tagName).toBe('DD');
+    expect(calls[0]?.body).toEqual({ action: 'accept', content: 'keep' });
+  });
 });
```

In `web/src/approvals/__tests__/approvalState.test.ts`:

```diff
--- a/web/src/approvals/__tests__/approvalState.test.ts
+++ b/web/src/approvals/__tests__/approvalState.test.ts
@@ -1,5 +1,10 @@
 import { describe, expect, it } from 'vitest';
-import { parseOptions, parseScopeChoice } from '../approvalState';
+import {
+  isDestructiveApproval,
+  offersOnlyScopes,
+  parseOptions,
+  parseScopeChoice,
+} from '../approvalState';
 
 // The server persists paused_states.options from agent.PauseOption, which marshals as
 // [{label, value}] — NOT as a string array. parseOptions used to accept only strings, so
@@ -71,3 +76,23 @@
     expect(parseScopeChoice('gateway_scope:everything:shell_exec')).toBeNull();
   });
 });
+
+describe('isDestructiveApproval', () => {
+  it('is true only for a presentation the gateway graded destructive', () => {
+    const graded = (risk: string) => ({
+      presentation: { key: 'approval.gateway.mutation', params: { tool: 't', risk, args: '' } },
+    });
+    expect(isDestructiveApproval(graded('destructive'))).toBe(true);
+    expect(isDestructiveApproval(graded('risky'))).toBe(false);
+    expect(isDestructiveApproval({})).toBe(false);
+  });
+});
+
+describe('offersOnlyScopes', () => {
+  it('is true only when there are options and every one is a gateway scope', () => {
+    const scope = { label: 'Approve once', value: 'gateway_scope:once:shell_exec' };
+    expect(offersOnlyScopes([scope])).toBe(true);
+    expect(offersOnlyScopes([scope, { label: 'No', value: 'No' }])).toBe(false);
+    expect(offersOnlyScopes([])).toBe(false);
+  });
+});
```

Three more places clicked an option button. Each becomes a row choice followed by **Answer**, for the same reason as above. In `web/src/approvals/__tests__/ThreadApprovalCards.test.tsx`:

```diff
--- a/web/src/approvals/__tests__/ThreadApprovalCards.test.tsx
+++ b/web/src/approvals/__tests__/ThreadApprovalCards.test.tsx
@@ -129,7 +129,8 @@
       </QueryClientProvider>,
     );
 
-    fireEvent.click(screen.getByRole('button', { name: 'Yes' }));
+    fireEvent.click(screen.getByRole('option', { name: 'Yes' }));
+    fireEvent.click(screen.getByRole('button', { name: 'Answer' }));
     await waitFor(() => {
       expect(onResolved).toHaveBeenCalledTimes(1);
     });
@@ -203,14 +204,16 @@
       </QueryClientProvider>,
     );
 
-    fireEvent.click(first(screen.getAllByRole('button', { name: 'Yes' })));
+    fireEvent.click(first(screen.getAllByRole('option', { name: 'Yes' })));
+    fireEvent.click(first(screen.getAllByRole('button', { name: 'Answer' })));
     await waitFor(() => {
       expect(onResolved).toHaveBeenCalledTimes(1);
     });
     const firstAnnouncement = screen.getByRole('status');
     expect(firstAnnouncement.textContent).toBe('Answered.');
 
-    fireEvent.click(screen.getByRole('button', { name: 'Yes' }));
+    fireEvent.click(screen.getByRole('option', { name: 'Yes' }));
+    fireEvent.click(screen.getByRole('button', { name: 'Answer' }));
     await waitFor(() => {
       expect(onResolved).toHaveBeenCalledTimes(2);
     });
```

The thread's own approval gate clicks the same buttons, in `ExternalStoreChat.approvals.test.tsx` (its fixture offers one option, `Yes`, at 9-18) and in `ExternalStoreChat.test.tsx`'s approval-resume test.
- Each click becomes a row choice followed by **Answer**.
- One count changes too. The review line ("Review the scope and consequence before continuing.") asks for a decision, so the redesign draws it on a pending approval only. A closed approval is a read-only receipt (spec §Cockpit, "After answering"). So the expired card of the locale test no longer carries it, and the count at 168 goes from 2 to 1.

```diff
--- a/web/src/chat/__tests__/ExternalStoreChat.approvals.test.tsx
+++ b/web/src/chat/__tests__/ExternalStoreChat.approvals.test.tsx
@@ -165,7 +165,9 @@
       renderChat(<ExternalStoreChat threadId="c-1" />);
 
       expect(await screen.findAllByText(frame)).toHaveLength(2);
-      expect(screen.getAllByText(review)).toHaveLength(2);
+      // The review line asks for a decision, so only the pending card carries it: a closed
+      // approval is a read-only receipt (spec 2026-09-25 §Cockpit, "After answering").
+      expect(screen.getAllByText(review)).toHaveLength(1);
       expect(screen.getByText(terminal).textContent).toBe(terminal);
       const composer = await screen.findByTestId('chat-composer');
       const hintId = composer.getAttribute('aria-describedby');
@@ -192,7 +194,8 @@
     expect(screen.getByPlaceholderText('Ask Aura')).toHaveProperty('disabled', true);
     expect(screen.getByRole('button', { name: 'Add files' })).toHaveProperty('disabled', true);
 
-    fireEvent.click(first(screen.getAllByRole('button', { name: 'Yes' })));
+    fireEvent.click(first(screen.getAllByRole('option', { name: 'Yes' })));
+    fireEvent.click(first(screen.getAllByRole('button', { name: 'Answer' })));
     await waitFor(() => {
       expect(runRequests).toHaveLength(0);
     });
@@ -204,7 +207,8 @@
       ).toBe('token-2');
     });
 
-    fireEvent.click(first(screen.getAllByRole('button', { name: 'Yes' })));
+    fireEvent.click(first(screen.getAllByRole('option', { name: 'Yes' })));
+    fireEvent.click(first(screen.getAllByRole('button', { name: 'Answer' })));
     await waitFor(() => {
       expect(runRequests).toHaveLength(0);
     });
@@ -215,7 +219,8 @@
           ?.getAttribute('data-approval-token'),
       ).toBe('token-3');
     });
-    fireEvent.click(first(screen.getAllByRole('button', { name: 'Yes' })));
+    fireEvent.click(first(screen.getAllByRole('option', { name: 'Yes' })));
+    fireEvent.click(first(screen.getAllByRole('button', { name: 'Answer' })));
     await waitFor(() => {
       expect(runRequests).toHaveLength(1);
     });
@@ -260,7 +265,8 @@
     );
     renderChat(<ExternalStoreChat threadId="c-1" />);
 
-    fireEvent.click(first(await screen.findAllByRole('button', { name: 'Yes' })));
+    fireEvent.click(first(await screen.findAllByRole('option', { name: 'Yes' })));
+    fireEvent.click(first(screen.getAllByRole('button', { name: 'Answer' })));
 
     await waitFor(() => {
       expect(
@@ -281,7 +287,8 @@
     );
     renderChat(<ExternalStoreChat threadId="c-1" />);
 
-    fireEvent.click(first(await screen.findAllByRole('button', { name: 'Yes' })));
+    fireEvent.click(first(await screen.findAllByRole('option', { name: 'Yes' })));
+    fireEvent.click(first(screen.getAllByRole('button', { name: 'Answer' })));
 
     await waitFor(() => {
       expect(
```

```diff
--- a/web/src/chat/__tests__/ExternalStoreChat.test.tsx
+++ b/web/src/chat/__tests__/ExternalStoreChat.test.tsx
@@ -448,7 +448,8 @@
     const onUsage = vi.fn();
     renderChat(<ExternalStoreChat threadId="conv-1" onUsage={onUsage} />);
 
-    fireEvent.click(await screen.findByRole('button', { name: 'Yes' }));
+    fireEvent.click(await screen.findByRole('option', { name: 'Yes' }));
+    fireEvent.click(screen.getByRole('button', { name: 'Answer' }));
     await waitFor(() => {
       expect(usageEvents(onUsage).some((event) => event.phase === 'settled')).toBe(true);
     });
```

Playwright runs in CI (`ci.yml:1834`). In `web/e2e/chat-calm-prism.spec.ts:232`, the fixture's options (`e2e/support/calmPrismFixture.ts:119`) are rows now:

```diff
--- a/web/e2e/chat-calm-prism.spec.ts
+++ b/web/e2e/chat-calm-prism.spec.ts
@@ -229,7 +229,7 @@
         .first(),
     ).toBeVisible();
     await expect(page.getByText('Approval required', { exact: true })).toBeVisible();
-    await expect(page.getByRole('button', { name: 'Pilot workspace' })).toBeVisible();
+    await expect(page.getByRole('option', { name: 'Pilot workspace' })).toBeVisible();
     await expect(page.getByText('Expired — auto-resolved.')).toBeVisible();
     await expect(page.getByText('Artifact', { exact: true })).toBeVisible();
     await expect(page.getByText(/calm-prism-release-readiness.*\.xlsx/)).toBeVisible();
```

`web/e2e/mcp-cockpit-live.spec.ts` approves a gateway-gated `calendar` call, and the gateway asks for `kind="approval"` with its three scopes (`internal/gateway/approve.go:169`). So the scope is now a row, sent by the **Approve** pill:

```diff
--- a/web/e2e/mcp-cockpit-live.spec.ts
+++ b/web/e2e/mcp-cockpit-live.spec.ts
@@ -207,7 +207,8 @@
     );
     await composer.press('Enter');
 
-    const approveForConversation = page.getByRole('button', {
+    // The gateway's scopes are rows of one choice, sent by the Approve pill.
+    const approveForConversation = page.getByRole('option', {
       name: /Approve .* for this conversation|Approva .* per questa conversazione/i,
     });
     await expect(approveForConversation).toBeVisible({ timeout: 90_000 });
@@ -227,6 +228,7 @@
       )
       .catch(() => undefined);
     await approveForConversation.click();
+    await page.getByRole('button', { name: /^(Approve|Approva)$/ }).click();
     const resolved = await resolutionResponse;
     const resolvedBody = await resolved.text();
     expect(resolved.ok(), resolvedBody).toBe(true);
```

- [ ] **Step 2: Run them to verify they fail.** Web: `npx vitest run src/questions src/approvals src/chat/__tests__/ExternalStoreChat.approvals.test.tsx src/chat/__tests__/ExternalStoreChat.test.tsx`.

Expected, measured on a copy of HEAD carrying only this step's tests: 17 tests fail and 101 pass (118).
- `QuestionFrame.test.tsx` does not load: `Failed to resolve import "../CancelControl" from "src/questions/__tests__/QuestionFrame.test.tsx"`.
- Seven tests in `InlineApprovalCard.test.tsx` fail with `Unable to find an accessible element with the role "option"` (or `"listbox"`, `"form"`, or the button `"Approve"`).
- The option clicks in `ThreadApprovalCards.test.tsx` (2 tests), `ExternalStoreChat.approvals.test.tsx` (3) and `ExternalStoreChat.test.tsx` (1) fail with `Unable to find … role "option" and name "Yes"`.
- The two locale tests of `ExternalStoreChat.approvals.test.tsx` fail with `expected [ <span …(1)></span>, …(1) ] to have a length of 1 but got 2`.
- `approvalState.test.ts` fails with `isDestructiveApproval is not a function` and `offersOnlyScopes is not a function`.

- [ ] **Step 3: Implement.** In `THIRD_PARTY_NOTICES.md`, after the `smixs/visual-skills` entry, add the entry below. MIT asks that its notice travel with substantial portions, so the licence text is carried whole, as fetched from `LICENSE.md` at the pinned commit. Each ported file's header comment names the same commit, holder and licence.

````markdown

## assistant-ui/tool-ui

- Source: `https://github.com/assistant-ui/tool-ui`, at commit
  `49a870286facdbf28160cd647f0d337ebdc9b275`
- License: MIT (`LICENSE.md` at that commit, reproduced below)
- Use in Aura: the markup and classes of Question Flow
  (`apps/www/components/tool-ui/question-flow/question-flow.tsx`), ported onto Aura's tokens
  and translated. No Tool UI package or vendored file is installed.
- Adapted files:
  - `web/src/questions/QuestionCard.tsx`
  - `web/src/questions/QuestionOptions.tsx`
  - `web/src/questions/QuestionReceipt.tsx`
- Required hygiene:
  - Keep the attribution comment at the top of each adapted file; it names the commit, the
    copyright holder and the license.
  - A component later installed from the `@tool-ui` registry (`web/components.json`) is listed
    here when it lands.

```text
MIT License

Copyright (c) 2025 AgentbaseAI Inc.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
````

In `web/components.json`:

```diff
--- a/web/components.json
+++ b/web/components.json
@@ -19,6 +19,7 @@
     "hooks": "@/hooks"
   },
   "registries": {
-    "@assistant-ui": "https://r.assistant-ui.com/{name}.json"
+    "@assistant-ui": "https://r.assistant-ui.com/{name}.json",
+    "@tool-ui": "https://www.tool-ui.com/r/{name}.json"
   }
 }
```

Create `web/src/i18n/resources.questions.ts`. It carries Task 8's keys too, so the parity gate sees one bundle. The usage gate (`resources.usage.test.ts`) checks only that a key the code uses resolves, so keys Task 8 has not used yet pass.

```ts
// The `questionCard.*` bundle: the frame every question the cockpit asks renders in
// (web/src/questions), shared by ask_user and a mounted MCP server's form. Split out of
// resources.ts to keep that file under the 600-LOC cap, the resources.steer.ts precedent.
// Every key exists in BOTH locales; the parity gate fails on any drift.

export const questionCardEn = {
  questionCard: {
    step: 'Step {{current}} of {{total}}',
    progress: 'Form progress',
    approve: 'Approve',
    next: 'Next',
    back: 'Back',
    skip: 'Skip',
    useDefault: 'Use default',
    review: 'Review',
    submit: 'Submit',
    decline: 'Decline',
    yes: 'Yes',
    no: 'No',
    placeholder: 'Type your answer',
    choose: {
      range: 'Choose {{min}} to {{max}}.',
      atLeast: 'Choose at least {{min}}.',
      atMost: 'Choose up to {{max}}.',
    },
    reviewStep: {
      title: 'Review your answers',
      edit: 'Change {{field}}',
      notGiven: 'Not given',
    },
    form: {
      title: 'A form from {{server}}',
      server: 'MCP server {{server}}',
      expiresIn: 'Aura cancels in {{time}}',
    },
    cancel: {
      label: 'Cancel',
      confirm: 'Cancel this request?',
      yes: 'Cancel request',
      no: 'Keep answering',
    },
    receipt: {
      answered: 'Answered.',
      declined: 'Declined.',
      cancelled: 'Cancelled.',
      expired: 'Expired: cancelled automatically.',
    },
    refusal: {
      unrenderable: "Aura declined this form because it can't be shown here.",
      ambiguous_run:
        'Aura declined this form because more than one conversation was using this server.',
    },
    error: {
      required: 'This field is required.',
      invalid: 'This value is not valid.',
      failed: "Couldn't send your answer. Try again.",
      closed: 'This form was already resolved.',
    },
  },
};

export const questionCardIt = {
  questionCard: {
    step: 'Passo {{current}} di {{total}}',
    progress: 'Avanzamento del modulo',
    approve: 'Approva',
    next: 'Avanti',
    back: 'Indietro',
    skip: 'Salta',
    useDefault: 'Usa il predefinito',
    review: 'Rivedi',
    submit: 'Invia',
    decline: 'Rifiuta',
    yes: 'Sì',
    no: 'No',
    placeholder: 'Scrivi la tua risposta',
    choose: {
      range: 'Scegline da {{min}} a {{max}}.',
      atLeast: 'Scegline almeno {{min}}.',
      atMost: 'Scegline al massimo {{max}}.',
    },
    reviewStep: {
      title: 'Rivedi le risposte',
      edit: 'Modifica {{field}}',
      notGiven: 'Non indicato',
    },
    form: {
      title: 'Un modulo da {{server}}',
      server: 'Server MCP {{server}}',
      expiresIn: 'Aura annulla tra {{time}}',
    },
    cancel: {
      label: 'Annulla',
      confirm: 'Annullare questa richiesta?',
      yes: 'Annulla richiesta',
      no: 'Continua a rispondere',
    },
    receipt: {
      answered: 'Risposto.',
      declined: 'Rifiutato.',
      cancelled: 'Annullato.',
      expired: 'Scaduto: annullato automaticamente.',
    },
    refusal: {
      unrenderable: 'Aura ha rifiutato questo modulo perché qui non si può mostrare.',
      ambiguous_run:
        'Aura ha rifiutato questo modulo perché più di una conversazione stava usando questo server.',
    },
    error: {
      required: 'Questo campo è obbligatorio.',
      invalid: 'Questo valore non è valido.',
      failed: 'Impossibile inviare la risposta. Riprova.',
      closed: 'Questo modulo è già stato risolto.',
    },
  },
};
```

In `web/src/i18n/resources.ts`:

```diff
--- a/web/src/i18n/resources.ts
+++ b/web/src/i18n/resources.ts
@@ -29,6 +29,7 @@
 import { videoStudioEn, videoStudioIt } from './resources.videoStudio';
 import { chatTurnNoticesEn, chatTurnNoticesIt } from './resources.turnnotices';
 import { updateEn, updateIt } from './resources.update';
+import { questionCardEn, questionCardIt } from './resources.questions';
 
 export const resources = {
   en: {
@@ -194,6 +195,7 @@
       ...embeddingRouteEn,
       ...remoteAccessEn,
       ...updateEn,
+      ...questionCardEn,
       ...profileEn,
       ...adminEn,
       ...onboardingEn,
@@ -466,6 +468,7 @@
       ...embeddingRouteIt,
       ...remoteAccessIt,
       ...updateIt,
+      ...questionCardIt,
       ...profileIt,
       ...adminIt,
       ...onboardingIt,
```

Create `web/src/questions/QuestionCard.tsx`:

```tsx
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { cn } from '@/lib/utils';

// QuestionCard is the one frame every question the cockpit asks is drawn in: ask_user's three
// kinds and a mounted MCP server's form (spec 2026-09-25). The markup and classes are ported
// from Tool UI's Question Flow (assistant-ui/tool-ui@49a8702 question-flow.tsx: StepContent,
// ProgressBar; © 2025 AgentbaseAI Inc., MIT, see THIRD_PARTY_NOTICES.md), on Aura's tokens and
// with its copy translated where the component hard-codes English.

export type QuestionVariant = 'default' | 'destructive';

export interface QuestionCardProps {
  readonly titleId: string;
  readonly title: ReactNode;
  readonly description?: ReactNode;
  readonly descriptionId?: string;
  readonly icon?: ReactNode;
  /** Above the step label: the MCP form's server chip, tool name and countdown. */
  readonly header?: ReactNode;
  readonly step?: { readonly current: number; readonly total: number };
  readonly variant?: QuestionVariant;
  readonly footer?: ReactNode;
  readonly status?: ReactNode;
  readonly dataAttributes?: Readonly<Record<`data-${string}`, string>>;
  readonly children?: ReactNode;
}

export function QuestionCard({
  titleId,
  title,
  description,
  descriptionId,
  icon,
  header,
  step,
  variant = 'default',
  footer,
  status,
  dataAttributes,
  children,
}: QuestionCardProps) {
  const { t } = useTranslation();
  const described = description !== undefined && descriptionId !== undefined;
  return (
    <div
      role="form"
      aria-labelledby={titleId}
      {...(described ? { 'aria-describedby': descriptionId } : {})}
      data-slot="card"
      data-variant={variant}
      tabIndex={-1}
      {...dataAttributes}
      className={cn(
        'flex w-full flex-col gap-4 rounded-2xl border bg-surface-2 p-5 text-text shadow-xs',
        'motion-safe:animate-in motion-safe:fade-in motion-safe:duration-300',
        variant === 'destructive' ? 'border-danger/60' : 'border-accent/40',
      )}
    >
      {header}
      {step !== undefined && step.total > 1 ? (
        <div className="flex flex-col gap-2">
          <span className="text-xs font-medium tracking-wide text-text-muted uppercase">
            {t('questionCard.step', { current: step.current, total: step.total })}
          </span>
          <StepBar current={step.current} total={step.total} label={t('questionCard.progress')} />
        </div>
      ) : null}
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          {icon}
          <h2 id={titleId} className="text-lg leading-tight font-semibold">
            {title}
          </h2>
        </div>
        {description !== undefined ? (
          <p
            id={descriptionId}
            className="overflow-x-auto text-sm leading-relaxed break-words whitespace-pre-wrap text-text-muted [overflow-wrap:anywhere]"
          >
            {description}
          </p>
        ) : null}
      </div>
      {children}
      {footer !== undefined ? (
        <div className="flex flex-wrap items-center justify-between gap-2 pt-2">{footer}</div>
      ) : null}
      {status}
    </div>
  );
}

interface StepBarProps {
  readonly current: number;
  readonly total: number;
  readonly label: string;
}

function StepBar({ current, total, label }: StepBarProps) {
  return (
    <div
      className="flex h-1.5 gap-1"
      role="progressbar"
      aria-label={label}
      aria-valuenow={current}
      aria-valuemin={1}
      aria-valuemax={total}
    >
      {Array.from({ length: total }, (_, index) => (
        <div key={index} className="relative flex-1 overflow-hidden rounded-full bg-surface">
          <div
            className={cn(
              'absolute inset-0 origin-left rounded-full bg-accent',
              'motion-safe:transition-transform motion-safe:duration-300',
              index < current ? 'scale-x-100' : 'scale-x-0',
            )}
          />
        </div>
      ))}
    </div>
  );
}
```

Create `web/src/questions/QuestionOptions.tsx`. `describedBy` lets Task 8 tie a field's hint and error to the list.

```tsx
import { Fragment, useRef, useState, type KeyboardEvent } from 'react';
import { Check } from 'lucide-react';
import { cn } from '@/lib/utils';

// QuestionOptions is Question Flow's option list (assistant-ui/tool-ui@49a8702
// question-flow.tsx: OptionItem, SelectionIndicator, the listbox keyboard; © 2025 AgentbaseAI
// Inc., MIT, see THIRD_PARTY_NOTICES.md) as radio rows (single) or checkbox rows (multi). Enter
// on a row that is already chosen submits a single choice, so a keyboard user can answer
// without leaving the list.

export interface QuestionOption {
  readonly id: string;
  readonly label: string;
  readonly description?: string;
}

export interface QuestionOptionsProps {
  readonly labelledBy: string;
  readonly describedBy?: string;
  readonly options: readonly QuestionOption[];
  readonly mode: 'single' | 'multi';
  readonly selected: ReadonlySet<string>;
  readonly disabled?: boolean;
  readonly onToggle: (id: string) => void;
  readonly onSubmit?: () => void;
}

export function QuestionOptions({
  labelledBy,
  describedBy,
  options,
  mode,
  selected,
  disabled = false,
  onToggle,
  onSubmit,
}: QuestionOptionsProps) {
  const rows = useRef<(HTMLButtonElement | null)[]>([]);
  const [active, setActive] = useState(() =>
    Math.max(
      options.findIndex((o) => selected.has(o.id)),
      0,
    ),
  );
  const last = options.length - 1;

  function focusAt(index: number) {
    rows.current[index]?.focus();
    setActive(index);
  }

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (disabled || options.length === 0) return;
    const moves: Record<string, number> = {
      ArrowDown: active === last ? 0 : active + 1,
      ArrowUp: active === 0 ? last : active - 1,
      Home: 0,
      End: last,
    };
    const target = moves[event.key];
    if (target !== undefined) {
      event.preventDefault();
      focusAt(target);
      return;
    }
    if (event.key !== 'Enter' && event.key !== ' ') return;
    event.preventDefault();
    const option = options[active];
    if (option === undefined) return;
    if (event.key === 'Enter' && mode === 'single' && selected.has(option.id)) onSubmit?.();
    else onToggle(option.id);
  }

  return (
    <div
      role="listbox"
      aria-labelledby={labelledBy}
      {...(describedBy !== undefined ? { 'aria-describedby': describedBy } : {})}
      aria-multiselectable={mode === 'multi'}
      tabIndex={-1}
      onKeyDown={onKeyDown}
      className="flex flex-col px-1"
    >
      {options.map((option, index) => {
        const isSelected = selected.has(option.id);
        return (
          <Fragment key={option.id}>
            {index > 0 ? <div aria-hidden="true" className="h-px bg-border-strong/40" /> : null}
            <button
              ref={(el) => {
                rows.current[index] = el;
              }}
              type="button"
              role="option"
              aria-selected={isSelected}
              tabIndex={index === active ? 0 : -1}
              disabled={disabled}
              onFocus={() => {
                setActive(index);
              }}
              onClick={() => {
                setActive(index);
                onToggle(option.id);
              }}
              className="group relative flex min-h-[50px] w-full items-start gap-3 py-2.5 text-left text-sm font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
            >
              <span
                aria-hidden="true"
                className="absolute inset-0 -mx-3 -my-0.5 rounded-xl bg-accent/5 opacity-0 transition-opacity group-hover:opacity-100"
              />
              <span className="relative flex h-6 items-center">
                <SelectionIndicator mode={mode} selected={isSelected} />
              </span>
              <span className="relative flex flex-col">
                <span className="leading-6 text-pretty">{option.label}</span>
                {option.description !== undefined ? (
                  <span className="text-sm font-normal text-pretty text-text-muted">
                    {option.description}
                  </span>
                ) : null}
              </span>
            </button>
          </Fragment>
        );
      })}
    </div>
  );
}

interface SelectionIndicatorProps {
  readonly mode: 'single' | 'multi';
  readonly selected: boolean;
}

function SelectionIndicator({ mode, selected }: SelectionIndicatorProps) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'flex size-4 shrink-0 items-center justify-center border-2',
        'motion-safe:transition-colors motion-safe:duration-200',
        mode === 'single' ? 'rounded-full' : 'rounded',
        selected
          ? 'border-accent bg-accent text-primary-foreground motion-safe:animate-in motion-safe:fade-in motion-safe:zoom-in-75'
          : 'border-text-muted/50',
      )}
    >
      {selected && mode === 'multi' ? <Check className="size-3" strokeWidth={3} /> : null}
      {selected && mode === 'single' ? <span className="size-2 rounded-full bg-current" /> : null}
    </span>
  );
}
```

Create `web/src/questions/QuestionReceipt.tsx`:

```tsx
import { Check } from 'lucide-react';
import { Badge } from '@/components/ui/badge';

// QuestionReceipt is what a question becomes once it closes: Question Flow's read-only receipt
// (assistant-ui/tool-ui@49a8702 question-flow.tsx: QuestionFlowReceipt; © 2025 AgentbaseAI
// Inc., MIT, see THIRD_PARTY_NOTICES.md) as a chip in the outcome's tone, and under an answer
// the value given. Nothing here is interactive.

export type ReceiptTone = 'success' | 'neutral' | 'warning' | 'danger';

const CHIP_TEXT: Record<ReceiptTone, string> = {
  success: 'text-success',
  neutral: 'text-text-muted',
  warning: 'text-warning',
  danger: 'text-danger',
};
const CHIP_DOT: Record<ReceiptTone, string> = {
  success: 'bg-success',
  neutral: 'bg-text-muted',
  warning: 'bg-warning',
  danger: 'bg-danger',
};

export interface ReceiptLine {
  readonly label: string;
  readonly value: string;
}

export interface QuestionReceiptProps {
  readonly tone: ReceiptTone;
  readonly label: string;
  readonly summary?: readonly ReceiptLine[];
  readonly announce?: boolean;
}

export function QuestionReceipt({
  tone,
  label,
  summary = [],
  announce = false,
}: QuestionReceiptProps) {
  return (
    <div className="flex flex-col gap-3">
      <Badge
        variant={tone === 'neutral' ? 'secondary' : tone}
        data-tone={tone}
        {...(announce ? { role: 'status', 'aria-live': 'polite' as const } : {})}
        className={`self-start text-[0.8125rem] ${CHIP_TEXT[tone]}`}
      >
        {tone === 'success' ? (
          <Check aria-hidden="true" className="size-3.5" />
        ) : (
          <span
            aria-hidden="true"
            className={`inline-block h-2 w-2 shrink-0 rounded-sm ${CHIP_DOT[tone]}`}
          />
        )}
        {label}
      </Badge>
      {summary.length > 0 ? (
        <dl className="flex flex-col gap-2 text-sm">
          {summary.map((line) => (
            <div
              key={line.label}
              className="flex flex-col gap-0.5 motion-safe:animate-in motion-safe:fade-in"
            >
              <dt className="text-text-muted">{line.label}</dt>
              <dd className="font-medium break-words whitespace-pre-wrap [overflow-wrap:anywhere]">
                {line.value}
              </dd>
            </div>
          ))}
        </dl>
      ) : null}
    </div>
  );
}
```

Create `web/src/questions/CancelControl.tsx`. Its body is `InlineApprovalCard.tsx:95-108` and `231-275` moved, not rewritten:

```tsx
import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { Button } from '@/components/ui/button';

// CancelControl is a question's Cancel, confirmed inline while its run streams. Escape
// dismisses the confirmation and never cancels anything.

export interface CancelLabels {
  readonly cancel: string;
  readonly confirm: string;
  readonly yes: string;
  readonly no: string;
}

export interface CancelControlProps {
  readonly isStreaming?: boolean | undefined;
  readonly disabled: boolean;
  readonly labels: CancelLabels;
  readonly onCancel: () => void;
}

export function CancelControl({ isStreaming, disabled, labels, onCancel }: CancelControlProps) {
  const [confirming, setConfirming] = useState(false);
  const cancelRef = useRef<HTMLButtonElement | null>(null);
  const keepRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    if (confirming) keepRef.current?.focus();
  }, [confirming]);

  function close() {
    setConfirming(false);
    requestAnimationFrame(() => cancelRef.current?.focus());
  }

  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    if (event.key !== 'Escape') return;
    event.preventDefault();
    close();
  }

  if (confirming) {
    return (
      <span className="flex items-center gap-2">
        <span className="text-[0.8125rem] text-warning">{labels.confirm}</span>
        <Button
          type="button"
          variant="destructive"
          size="sm"
          disabled={disabled}
          onKeyDown={onKeyDown}
          onClick={onCancel}
          className="min-h-11 text-[0.8125rem]"
        >
          {labels.yes}
        </Button>
        <Button
          ref={keepRef}
          type="button"
          variant="ghost"
          size="sm"
          onKeyDown={onKeyDown}
          onClick={close}
          className="min-h-11 text-[0.8125rem] text-text-muted hover:text-text"
        >
          {labels.no}
        </Button>
      </span>
    );
  }
  return (
    <Button
      ref={cancelRef}
      type="button"
      variant="ghost"
      disabled={disabled}
      onClick={() => {
        if (isStreaming === true) setConfirming(true);
        else onCancel();
      }}
      className="text-[0.8125rem] text-danger hover:bg-danger/15 hover:text-danger"
    >
      {labels.cancel}
    </Button>
  );
}
```

In `web/src/approvals/approvalState.ts`:

```diff
--- a/web/src/approvals/approvalState.ts
+++ b/web/src/approvals/approvalState.ts
@@ -14,6 +14,14 @@
   return approval.terminal === true;
 }
 
+/**
+ * True when the gateway graded the approval's action Destructive (scoring.Destructive, carried
+ * as the presentation's `risk` param): the card then draws its destructive variant.
+ */
+export function isDestructiveApproval(approval: Pick<Approval, 'presentation'>): boolean {
+  return approval.presentation?.params.risk === 'destructive';
+}
+
 /** One rendered choice: the label the operator reads, the value the server records. */
 export interface ApprovalOption {
   readonly label: string;
@@ -21,7 +29,7 @@
 }
 
 /**
- * Parse the raw JSON option set into the choices the inline card renders as buttons.
+ * Parse the raw JSON option set into the choices the inline card renders as rows.
  *
  * The server persists `paused_states.options` from agent.PauseOption, which marshals as
  * `[{label, value}]` — NOT as a string array. This function used to accept only strings, so
@@ -74,3 +82,12 @@
   if (!SCOPES.includes(scope as ApprovalScope) || subject === '') return null;
   return { scope: scope as ApprovalScope, subject };
 }
+
+/**
+ * True when every option is a gateway scope: the card then says Approve. A model's own
+ * approval options, such as Yes and No, are answers, and Approve on "No" would say the
+ * opposite of what is sent.
+ */
+export function offersOnlyScopes(options: readonly ApprovalOption[]): boolean {
+  return options.length > 0 && options.every((option) => parseScopeChoice(option.value) !== null);
+}
```

Replace `web/src/approvals/InlineApprovalCard.tsx` in full.
- `useScopeLabel` and `cardStateFor` keep their bodies.
- The misplaced `cardStateFor` doc block at 27-32 moves onto `cardStateFor`, where it belongs.
- `parseOptions` can yield two options with one value: it keeps whatever `{label, value}` pairs the server stored (`approvalState.ts:34-43`). So the chosen row is an index, and the receipt shows the chosen row's label.

```tsx
import { useId, useRef, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { MessageSquareText, ShieldCheck, TriangleAlert } from 'lucide-react';
import { ariaInvalid } from '../a11y/aria';
import { CancelControl } from '../questions/CancelControl';
import { QuestionCard } from '../questions/QuestionCard';
import { QuestionOptions } from '../questions/QuestionOptions';
import { QuestionReceipt, type ReceiptTone } from '../questions/QuestionReceipt';
import { approvalQuestion } from './approvalQuestion';
import {
  isDestructiveApproval,
  isTerminal,
  offersOnlyScopes,
  parseOptions,
  parseScopeChoice,
  type ApprovalOption,
} from './approvalState';
import type { ApprovalResolution, ApprovalResolutionAttempt } from './useThreadApprovals';
import {
  useResolveApproval,
  type Approval,
  type ResolveAction,
  type ResolveOutcome,
} from './useApprovals';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';

// InlineApprovalCard is ask_user's adapter onto QuestionCard (spec 2026-09-25). Options are
// radio rows and a pill that stays grey until one is chosen; with no options the reply is free
// text, for every kind, as before. The pill says Approve only when every option is a gateway
// scope. Recognized approval metadata is rendered in the active locale; legacy/unknown questions
// remain escaped React text, and the URL capability token never appears as a visible field.

type CardState = 'pending' | 'answered' | 'declined' | 'cancelled';

interface Receipt {
  readonly tone: ReceiptTone;
  readonly key: string;
}

const RECEIPTS: Record<Exclude<CardState, 'pending'>, Receipt> = {
  answered: { tone: 'success', key: 'approval.card.answered' },
  declined: { tone: 'neutral', key: 'approval.card.declined' },
  cancelled: { tone: 'danger', key: 'approval.card.cancelled' },
};

/**
 * useScopeLabel renders a gateway approval-scope option in the operator's language. The
 * gateway ships a stable code plus an English fallback label; a scope it does not
 * recognise falls back to that label rather than rendering a raw code.
 */
function useScopeLabel(): (option: ApprovalOption) => string {
  const { t } = useTranslation();
  return (option) => {
    const choice = parseScopeChoice(option.value);
    if (choice === null) return option.label;
    return t(`approval.scope.${choice.scope}`, { subject: choice.subject });
  };
}

/**
 * cardStateFor maps the server's verdict to the receipt. The server is authoritative — a
 * scheduled gate reports approved/rejected regardless of which button produced it — and the
 * action is only the fallback for the in-session verdicts ('continue'/'pending'), where the
 * receipt reflects what the operator just did while the turn goes on.
 */
function cardStateFor(outcome: ResolveOutcome, action: ResolveAction): CardState {
  switch (outcome) {
    case 'approved':
      return 'answered';
    case 'rejected':
      return 'declined';
    case 'terminated':
      return 'cancelled';
    default:
      return action === 'accept' ? 'answered' : action === 'decline' ? 'declined' : 'cancelled';
  }
}

export interface InlineApprovalCardProps {
  readonly approval: Approval;
  /** True while the owning run is actively streaming; gates cancel confirmation. */
  readonly isStreaming?: boolean;
  /** Notify the shared thread gate after a successful local resolution. */
  readonly onResolved?: (resolution: ApprovalResolution) => void | Promise<void>;
  /** Notify the gate before the resolve request starts so Cancel owns the generation. */
  readonly onResolutionStarted?: (attempt: ApprovalResolutionAttempt) => void;
  /** Release a matching lifecycle attempt if its resolve request fails. */
  readonly onResolutionFailed?: (attempt: ApprovalResolutionAttempt) => void | Promise<void>;
}

export function InlineApprovalCard({
  approval,
  isStreaming,
  onResolved,
  onResolutionStarted,
  onResolutionFailed,
}: InlineApprovalCardProps) {
  const { t } = useTranslation();
  const resolve = useResolveApproval();
  const options = parseOptions(approval.options);
  const scopeLabel = useScopeLabel();
  const [freeText, setFreeText] = useState('');
  // An index, not a value: nothing makes two options' values distinct.
  const [chosen, setChosen] = useState<number | null>(null);
  const [state, setState] = useState<CardState>('pending');
  const [given, setGiven] = useState('');
  const attemptSequence = useRef(0);
  const baseId = useId();
  const busy = resolve.isPending;
  const failed = resolve.isError;
  const isApproval = approval.kind === 'approval';
  const destructive = isApproval && isDestructiveApproval(approval);
  const choosing = options.length > 0;
  const chosenOption = chosen === null ? undefined : options[chosen];
  const verb =
    isApproval && offersOnlyScopes(options) ? 'questionCard.approve' : 'approval.card.answer';

  function submit(action: ResolveAction, content?: string, shown = '') {
    attemptSequence.current += 1;
    const attempt: ApprovalResolution = {
      approval,
      action,
      attemptId: `${baseId}:${String(attemptSequence.current)}`,
    };
    onResolutionStarted?.(attempt);
    resolve.mutate(
      action === 'accept'
        ? { token: approval.token, action, content: content ?? '' }
        : { token: approval.token, action },
      {
        onSuccess: (directive) => {
          setGiven(shown);
          setState(cardStateFor(directive.outcome, action));
          // The verdict rides along so the thread gate re-drives the turn only when the model
          // actually has more work ('continue'); a scheduled gate is already complete.
          void onResolved?.({ ...attempt, outcome: directive.outcome });
        },
        onError: () => {
          void onResolutionFailed?.(attempt);
        },
      },
    );
  }

  function answer() {
    if (!choosing) submit('accept', freeText, freeText.trim());
    else if (chosenOption !== undefined) {
      submit('accept', chosenOption.value, scopeLabel(chosenOption));
    }
  }

  function frame(children: ReactNode, footer?: ReactNode) {
    return (
      <QuestionCard
        titleId={`${baseId}-title`}
        descriptionId={`${baseId}-question`}
        icon={<FrameIcon approval={isApproval} destructive={destructive} />}
        title={t(isApproval ? 'approval.frame.approval' : 'approval.frame.input')}
        description={approvalQuestion(approval, t)}
        variant={destructive ? 'destructive' : 'default'}
        dataAttributes={{ 'data-approval-token': approval.token }}
        {...(footer !== undefined ? { footer } : {})}
      >
        {children}
      </QuestionCard>
    );
  }

  if (isTerminal(approval)) {
    return frame(
      <QuestionReceipt tone="warning" label={t('approval.terminal.expired')} announce />,
    );
  }
  if (state !== 'pending') {
    const receipt = RECEIPTS[state];
    const summary =
      state === 'answered' && given !== ''
        ? [{ label: t('approval.card.freeText'), value: given }]
        : [];
    return frame(<QuestionReceipt tone={receipt.tone} label={t(receipt.key)} summary={summary} />);
  }

  const footer = (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          variant="ghost"
          disabled={busy}
          onClick={() => {
            submit('decline');
          }}
          className="text-[0.8125rem] text-text-muted hover:text-text"
        >
          {t('approval.card.decline')}
        </Button>
        <CancelControl
          isStreaming={isStreaming}
          disabled={busy}
          labels={{
            cancel: t('approval.card.cancel'),
            confirm: t('approval.card.confirmCancel'),
            yes: t('approval.card.confirmCancelYes'),
            no: t('approval.card.confirmCancelNo'),
          }}
          onCancel={() => {
            submit('cancel');
          }}
        />
      </div>
      <Button
        type="button"
        variant={destructive ? 'destructive' : 'default'}
        disabled={busy || (choosing && chosenOption === undefined)}
        onClick={answer}
        className="rounded-full text-[0.8125rem]"
      >
        {t(verb)}
      </Button>
    </>
  );

  return frame(
    <>
      {isApproval ? <p className="text-xs text-text-muted">{t('approval.frame.review')}</p> : null}
      {choosing ? (
        <QuestionOptions
          labelledBy={`${baseId}-title`}
          options={options.map((option, index) => ({
            id: String(index),
            label: scopeLabel(option),
          }))}
          mode="single"
          selected={new Set(chosen === null ? [] : [String(chosen)])}
          disabled={busy}
          onToggle={(id) => {
            setChosen(Number(id));
          }}
          onSubmit={answer}
        />
      ) : (
        <div className="flex flex-col gap-1">
          <Label
            htmlFor={`${baseId}-answer`}
            className="text-[0.75rem] font-normal text-text-muted"
          >
            {t('approval.card.freeText')}
          </Label>
          <Textarea
            id={`${baseId}-answer`}
            value={freeText}
            disabled={busy}
            onChange={(event) => {
              setFreeText(event.target.value);
            }}
            placeholder={t('approval.card.freeTextPlaceholder')}
            rows={2}
            aria-invalid={ariaInvalid(failed && freeText.trim().length === 0)}
            className="resize-y bg-surface text-sm"
          />
        </div>
      )}
      {failed ? (
        <Alert
          role="status"
          aria-live="polite"
          data-tone="danger"
          variant="destructive"
          className="bg-surface"
        >
          <AlertDescription>{t('approval.card.error')}</AlertDescription>
        </Alert>
      ) : null}
    </>,
    footer,
  );
}

interface FrameIconProps {
  readonly approval: boolean;
  readonly destructive: boolean;
}

function FrameIcon({ approval, destructive }: FrameIconProps) {
  if (destructive) return <TriangleAlert aria-hidden="true" className="size-5 text-danger" />;
  if (approval) return <ShieldCheck aria-hidden="true" className="size-5 text-warning" />;
  return <MessageSquareText aria-hidden="true" className="size-5 text-accent-text" />;
}
```

In `web/stryker.config.json`:

```diff
--- a/web/stryker.config.json
+++ b/web/stryker.config.json
@@ -21,6 +21,11 @@
     "src/governance/governanceApi.ts",
     "src/governance/governanceView.tsx",
     "src/approvals/approvalState.ts",
+    "src/approvals/InlineApprovalCard.tsx",
+    "src/questions/QuestionCard.tsx",
+    "src/questions/QuestionOptions.tsx",
+    "src/questions/QuestionReceipt.tsx",
+    "src/questions/CancelControl.tsx",
     "src/onboarding/onboardingApi.ts",
     "src/onboarding/onboardingWizardModel.ts",
     "src/chat/share/RevokeConfirmDialog.tsx",
```

In `web/vitest.stryker.config.ts`:

```diff
--- a/web/vitest.stryker.config.ts
+++ b/web/vitest.stryker.config.ts
@@ -5,6 +5,9 @@
   'src/approvals/__tests__/ApprovalList.test.tsx',
   'src/approvals/__tests__/InlineApprovalCard.test.tsx',
   'src/approvals/__tests__/ThreadApprovalCards.test.tsx',
+  'src/approvals/__tests__/approvalState.test.ts',
+  // The question frame (spec 2026-09-25): the ask_user adapter's suites above reach it too.
+  'src/questions/__tests__/QuestionFrame.test.tsx',
   'src/chat/artifacts/artifactMeta.test.ts',
   'src/chat/artifacts/downloadAll.test.ts',
   'src/chat/voice/speechAdapter.test.ts',
```

- [ ] **Step 4: Run the checks.** Web, one command at a time:
  - `npx prettier --write src/questions src/approvals src/i18n src/chat/__tests__ components.json stryker.config.json vitest.stryker.config.ts e2e/chat-calm-prism.spec.ts e2e/mcp-cockpit-live.spec.ts`, then the same paths with `--check`. Expected: `All matched files use Prettier code style!` (`printWidth: 100`, `web/.prettierrc`).
  - `npx vitest run src/questions src/approvals src/i18n src/chat/__tests__/ExternalStoreChat.approvals.test.tsx src/chat/__tests__/ExternalStoreChat.test.tsx src/__tests__/readabilityTokens.test.ts`. Expected: `Tests  145 passed (145)`, measured on a copy of HEAD with this task applied. That includes the i18n parity and usage gates, and the readability gate.
    - The readability gate scans every source file, and it refuses `text-accent` or `text-primary` as a text colour: those tokens are fills. The readable one is `text-accent-text`, which `FrameIcon` uses.
  - `npm run typecheck`. Expected: exit 0 and no output.
  - `npm run lint`. Expected: `Found 0 warnings and 0 errors.` A warning fails the gate as an error does, and oxlint exits 0 either way.
  - `npm run dup`. Expected: `Found 0 clones.` jscpd runs with threshold 0; `CancelControl` exists so the two adapters share no clone.
  - `npm run deadcode`. Expected: knip prints nothing.
  - `wc -l` on every touched `.ts`/`.tsx` file. Each must stay under 600. Measured:
    - `resources.ts` 583;
    - `InlineApprovalCard.test.tsx` 517;
    - `ExternalStoreChat.test.tsx` 487;
    - `ExternalStoreChat.approvals.test.tsx` 301;
    - `InlineApprovalCard.tsx` 291;
    - `ThreadApprovalCards.test.tsx` 223;
    - `QuestionFrame.test.tsx` 196;
    - `QuestionOptions.tsx` 152;
    - `QuestionCard.tsx` 123;
    - `resources.questions.ts` 116;
    - `approvalState.test.ts` 98;
    - `approvalState.ts` 93;
    - `CancelControl.tsx` 85;
    - `QuestionReceipt.tsx` 77.
  - Stryker runs in CI only.

- [ ] **Step 5: Commit.**

```bash
cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH" LEFTHOOK_BIN="$HOME/go/bin/lefthook"
git add web/src/questions/ web/src/i18n/resources.questions.ts
git -c core.hooksPath=.git/hooks commit -F - -- THIRD_PARTY_NOTICES.md web/components.json web/src/questions/ web/src/i18n/resources.questions.ts web/src/i18n/resources.ts web/src/approvals/InlineApprovalCard.tsx web/src/approvals/approvalState.ts web/src/approvals/__tests__/InlineApprovalCard.test.tsx web/src/approvals/__tests__/ThreadApprovalCards.test.tsx web/src/approvals/__tests__/approvalState.test.ts web/src/chat/__tests__/ExternalStoreChat.approvals.test.tsx web/src/chat/__tests__/ExternalStoreChat.test.tsx web/e2e/chat-calm-prism.spec.ts web/e2e/mcp-cockpit-live.spec.ts web/stryker.config.json web/vitest.stryker.config.ts <<'EOF'
feat(cockpit): draw ask_user in Tool UI's question frame

Every question the cockpit asks now renders in one frame, QuestionCard.
Its markup is ported from Tool UI's Question Flow (MIT, credited in
THIRD_PARTY_NOTICES.md), on Aura's tokens and with the copy in en and it.

ask_user is its first adapter:
- options are radio rows and a pill Answer that stays grey until a row
  is chosen, and Enter on the chosen row answers;
- with no options the reply is free text, for every kind, as before;
- the pill says Approve only when every option is a gateway scope, and a
  gateway approval graded Destructive draws the destructive variant.

The resolve API and the lifecycle are unchanged.

Existing tests change because a click on a row no longer submits:
- two tests in InlineApprovalCard, three sites in ThreadApprovalCards,
  five in the thread's approval gate and one approval-resume test now
  choose a row and then press Answer;
- the Calm Prism spec finds its option as a row, and the live MCP spec
  picks its scope as a row and presses Approve;
- the gate's locale test counts the review line once: a closed approval
  is a read-only receipt and no longer asks for a review.

Stryker now runs approvalState.test.ts: approvalState.ts was mutated
but its suite was never in the mutation config.

The @tool-ui registry is registered for spec 2. No vendored component
is installed.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

The four Calm Prism screenshot baselines show the approval stack this task redraws. They can only be taken on the CI runner, so Task 9 Step 9 commits them from the push's run.

---
### Task 8: A mounted server's form in the cockpit thread

**Files:**
- Create:
  - `web/src/chat/sseAdapter_elicitation.ts`
  - `web/src/questions/useThreadElicitations.ts`
  - `web/src/questions/elicitationApi.ts`
  - `web/src/questions/elicitationSteps.ts`
  - `web/src/questions/useCountdown.ts`
  - `web/src/questions/FieldInput.tsx`
  - `web/src/questions/ElicitationHeader.tsx`
  - `web/src/questions/ElicitationReview.tsx`
  - `web/src/questions/ElicitationCard.tsx`
- Modify, each with the diff below, whose hunk headers give the lines at HEAD:
  - `web/src/chat/sseAdapter.ts`: the import, the CUSTOM-branch comment at 303-306, `StreamRunOptions`, `StreamPostOptions`, `StreamSSEOptions`, the pump, `streamPost` and `streamRun` (554 → 565 lines);
  - `web/src/chat/sseResume.ts`: the import, `AttachRunOptions`, `EngineOptions`, `makeEngine` and `pumpBody` (442 → 450);
  - `web/src/chat/ExternalStoreChat_liveRun.ts` (160 → 179);
  - `web/src/chat/ExternalStoreChat_streams.ts` (190 → 196);
  - `web/src/chat/ExternalStoreChat.tsx`, one import and six lines (587 → 594);
  - `web/src/approvals/ThreadApprovalCards.tsx` (68 → 82);
  - `web/stryker.config.json`, adding the nine new non-test files to `mutate`;
  - `web/vitest.stryker.config.ts`, adding the six new suites.
- Test:
  - Create:
    - `web/src/chat/sseAdapter.onElicitation.test.ts`
    - `web/src/questions/__tests__/useThreadElicitations.test.ts`
    - `web/src/questions/__tests__/elicitationApi.test.ts`
    - `web/src/questions/__tests__/elicitationSteps.test.ts`
    - `web/src/questions/__tests__/useCountdown.test.ts`
    - `web/src/questions/__tests__/ElicitationCard.test.tsx`
  - Modify: `web/src/approvals/__tests__/ThreadApprovalCards.test.tsx`, one test after the last.

**Interfaces:**
- Consumes:
  - Task 6's two frames:
    - `aura.elicitation`, with value `{run_id, id, server, tool?, message, fields: Field[] | null, deadline, refusal?}`. `fields` is `null` on a refusal.
    - `aura.elicitation_resolved`, with value `{id, action, expired?}`. An expiry is `action: "cancel"` with `expired: true`.
  - Task 6's routes:
    - `POST /agent/runs/{runID}/elicitations/{id}`, with an `Idempotency-Key`: 202, 409, 410, or 422 with `{"errors": {<field name, or "" for the whole answer>: <problem code>}}`. The codes carry no value.
    - `GET /agent/runs/{runID}/elicitations`: `{"questions": [...]}`, the run's open forms, sorted by deadline.
  - Task 3's field order: required fields first, in the order of the schema's `required` array, then the rest by name.
  - Task 7's `QuestionCard`, `QuestionOptions` (with `describedBy`), `QuestionReceipt`, `ReceiptLine`, `CancelControl` and `questionCard.*`.
- Produces:
  - `elicitationSignalValue(frame: AguiFrame): ElicitationSignal | null`;
  - `elicitationQuestionOf(value: unknown): ElicitationQuestion | null`, shared by the stream and the GET list;
  - `isStringList(value: unknown): value is readonly string[]`;
  - `onElicitation?: (signal: ElicitationSignal) => void` on `streamRun`, `streamPost`, `attachRun` and the resilient run;
  - `postElicitationAnswer(runId, id, body, idempotencyKey): Promise<ElicitationAnswerResult>` and `fetchOpenElicitations(runId): Promise<ElicitationQuestion[]>`;
  - `useThreadElicitations(threadId: string, isRunning: boolean, liveRunId: string | undefined): {items, onSignal}`, with `applyElicitationSignal` and `keepLiveRun`;
  - `ThreadApprovalCards`'s new `elicitations?: readonly ElicitationItem[]` prop.

**What the card does, and why:**
- **One step per field, then Review.** A form of more than one field ends on a Review step, and only Review submits. Each row there reopens its field. The MCP spec says clients MUST let the user review and modify an answer before sending it (2025-11-25 `client/elicitation.mdx:40-45`). A one-field form submits from its only step.
- **Skip, or Use default.** On an optional field with no default, Skip leaves it out. With a default, the button says **Use default**: after the handler returns, go-sdk writes the schema's default into every optional field left out (`ApplyDefaults`, `mcp/client.go:901`). The Review step and the receipt show that default, because it is what the server receives.
- **The server's message is the description on every step** (spec §Cockpit). A field's own description is a hint under its input. Hint, item bounds and error are each tied to the input by `aria-describedby`.
- **Item bounds.** A multi-choice field with `min_items` or `max_items` says how many it takes ("Choose 1 to 3."), and Next stays grey outside them.
- **A date-time default shows in its input.** `<input type="datetime-local">` blanks anything but a local `YYYY-MM-DDTHH:mm[:ss]`. So `initialValue` converts an RFC 3339 default to local time, and `contentFrom` sends RFC 3339 back.
- **A 422 says which field and what kind of problem, never the value.** The card returns to the first failing step:
  - the `required` code gets its own copy;
  - every other code reads "This value is not valid.";
  - a whole-answer problem (key `""`) shows on the card.
- **The receipt follows the stream.** An expiry reads "Expired: cancelled automatically."
- **A form never outlives its run on the client.** A Stop aborts the stream before the cancel's resolution can arrive, and a cut stream loses whatever was in flight. So once the thread stops streaming, the hook keeps only the cards of the run the server still names live (`liveRunId`). A new run's first question also drops every card of an earlier run.
- **A reload gets the forms back from two places.**
  - The attach's replay re-emits every CUSTOM frame the ring still holds.
  - Once the ring has rotated past a form, GET `/events` answers 410, the client rebuilds from the snapshot, and no frame comes back. So the attach also asks the run for its open forms. The hook holds a form both deliver only once.
- **Which streams can meet a form.**
  - The primary send and a reattach, both through the detached run.
  - The HITL resume, which runs detached through `/agent/run`.
  - Not a branch re-run: it streams with no asker (`internal/agui/conversations_branch_api.go:209-212`), so a form there is declined and surfaced.

- [ ] **Step 1: Write the failing tests.** Create `web/src/chat/sseAdapter.onElicitation.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from 'vitest';
import { streamRun, type AguiFrame } from './sseAdapter';
import { elicitationQuestionOf, elicitationSignalValue } from './sseAdapter_elicitation';
import { attachRun } from './sseResume';

// The aura.elicitation pump signal, on the driving pump and the reattach pump alike, shaped
// like sseAdapter.onSteer.test.ts. A form is never reduced into a message part.

function sseResponse(frames: readonly AguiFrame[]): Response {
  const enc = new TextEncoder();
  const wire = frames.map((f) => `event: ${f.type}\ndata: ${JSON.stringify(f)}\n\n`).join('');
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(enc.encode(wire));
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

const RUN_STARTED = { type: 'RUN_STARTED' } as AguiFrame;
const RUN_FINISHED = { type: 'RUN_FINISHED', outcome: { type: 'success' } } as AguiFrame;

const QUESTION = {
  run_id: 'run-1',
  id: 'q-1',
  server: 'forms',
  tool: 'ask_name',
  message: 'what is your name',
  fields: [
    { name: 'name', kind: 'string', required: true },
    { name: 'tags', kind: 'enum', required: false, multi: true, min_items: 1, max_items: 3 },
  ],
  deadline: '2026-09-25T10:05:00Z',
};

function custom(name: string, value: unknown): AguiFrame {
  return { type: 'CUSTOM', name, value };
}

describe('elicitationSignalValue', () => {
  it('reads a question and its resolution', () => {
    expect(elicitationSignalValue(custom('aura.elicitation', QUESTION))).toEqual({
      kind: 'question',
      question: QUESTION,
    });
    const expired = { id: 'q-1', action: 'cancel', expired: true };
    expect(elicitationSignalValue(custom('aura.elicitation_resolved', expired))).toEqual({
      kind: 'resolved',
      resolved: expired,
    });
  });

  it('reads a refusal, whose fields the server sends as null', () => {
    const refused = { ...QUESTION, fields: null, message: '', refusal: 'unrenderable' };
    expect(elicitationSignalValue(custom('aura.elicitation', refused))).toEqual({
      kind: 'question',
      question: { ...refused, fields: [] },
    });
  });

  it('drops what it cannot trust', () => {
    for (const bad of [
      { ...QUESTION, id: 7 },
      { ...QUESTION, fields: [{ name: 'x', kind: 'object', required: true }] },
      { ...QUESTION, fields: [{ name: 'x', kind: 'string', required: true, format: 'ipv4' }] },
      { ...QUESTION, fields: [{ name: 'x', kind: 'enum', required: true, max_items: '3' }] },
      { ...QUESTION, refusal: 'because' },
      'not an object',
    ]) {
      expect(elicitationSignalValue(custom('aura.elicitation', bad))).toBeNull();
    }
    const unknownAction = { id: 'q-1', action: 'sure' };
    expect(elicitationSignalValue(custom('aura.elicitation_resolved', unknownAction))).toBeNull();
    expect(elicitationSignalValue(custom('aura.steer', QUESTION))).toBeNull();
    expect(elicitationSignalValue({ type: 'TEXT_MESSAGE_START', messageId: 'm1' })).toBeNull();
  });

  it('reads the same question shape the run lists', () => {
    expect(elicitationQuestionOf(QUESTION)).toEqual(QUESTION);
    expect(elicitationQuestionOf({ ...QUESTION, deadline: 5 })).toBeNull();
  });
});

describe('the pumps fire onElicitation', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  const frames = [
    RUN_STARTED,
    custom('aura.elicitation', QUESTION),
    custom('aura.elicitation_resolved', { id: 'q-1', action: 'accept' }),
    RUN_FINISHED,
  ];

  it('on the driving pump', async () => {
    const onElicitation = vi.fn();
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(sseResponse(frames))),
    );
    await streamRun({
      threadId: 'conv-1',
      userText: 'ask me',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      onUpdate: () => undefined,
      onElicitation,
    });
    const kinds = onElicitation.mock.calls.map(([signal]) => (signal as { kind: string }).kind);
    expect(kinds).toEqual(['question', 'resolved']);
  });

  it('on the reattach pump, so a reloaded tab gets the form back', async () => {
    const onElicitation = vi.fn();
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(sseResponse(frames))),
    );
    await attachRun({
      threadId: 'conv-1',
      runId: 'run-1',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      onUpdate: () => undefined,
      onElicitation,
    });
    expect(onElicitation).toHaveBeenCalledTimes(2);
  });
});
```

Create `web/src/questions/__tests__/useThreadElicitations.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import type { ElicitationQuestion } from '../../chat/sseAdapter_elicitation';
import {
  applyElicitationSignal,
  keepLiveRun,
  useThreadElicitations,
  type ElicitationItem,
} from '../useThreadElicitations';

function question(id: string, runId = 'run-1'): ElicitationQuestion {
  return {
    run_id: runId,
    id,
    server: 'forms',
    message: 'm',
    fields: [],
    deadline: '2026-09-25T10:05:00Z',
  };
}

const asked = (id: string, runId?: string) => ({
  kind: 'question' as const,
  question: question(id, runId),
});
const resolved = (id: string, action: 'accept' | 'decline' | 'cancel', expired?: true) => ({
  kind: 'resolved' as const,
  resolved: { id, action, ...(expired ? { expired } : {}) },
});

function fold(...signals: Parameters<typeof applyElicitationSignal>[1][]) {
  return signals.reduce<readonly ElicitationItem[]>(applyElicitationSignal, []);
}

describe('applyElicitationSignal', () => {
  it('keeps questions in arrival order', () => {
    expect(fold(asked('a'), asked('b')).map((item) => item.question.id)).toEqual(['a', 'b']);
  });

  // Review Focus 4: a reload replays the run from its first frame, and the run's own list may
  // bring the same form again.
  it('holds a replayed question once, and its resolution still applies', () => {
    const items = fold(asked('a'), resolved('a', 'accept'), asked('a'), resolved('a', 'accept'));
    expect(items).toHaveLength(1);
    expect(items[0]?.outcome).toBe('accepted');
  });

  it('settles a question from its first resolution only', () => {
    expect(fold(asked('a'), resolved('a', 'cancel'), resolved('a', 'accept'))[0]?.outcome).toBe(
      'cancelled',
    );
  });

  it('tells an expiry from a cancel for another reason', () => {
    expect(fold(asked('a'), resolved('a', 'cancel', true))[0]?.outcome).toBe('expired');
    expect(fold(asked('b'), resolved('b', 'cancel'))[0]?.outcome).toBe('cancelled');
    expect(fold(asked('c'), resolved('c', 'decline'))[0]?.outcome).toBe('declined');
  });

  it('ignores a resolution for a question it never saw', () => {
    expect(fold(asked('a'), resolved('zzz', 'accept'))[0]?.outcome).toBeUndefined();
  });

  it('drops every card of an earlier run when a new run asks, settled or not', () => {
    const items = fold(asked('a', 'run-1'), resolved('a', 'accept'), asked('b', 'run-1'));
    expect(applyElicitationSignal(items, asked('c', 'run-2')).map((i) => i.question.id)).toEqual([
      'c',
    ]);
  });
});

describe('keepLiveRun', () => {
  it("keeps only the live run's cards, and the same list when nothing goes", () => {
    const items = fold(asked('a', 'run-1'), asked('b', 'run-1'));
    expect(keepLiveRun(items, 'run-1')).toBe(items);
    expect(keepLiveRun(items, undefined)).toEqual([]);
  });
});

describe('useThreadElicitations', () => {
  it('scopes its forms to the thread it serves', () => {
    const { result, rerender } = renderHook(
      ({ threadId }) => useThreadElicitations(threadId, true, undefined),
      { initialProps: { threadId: 't-1' } },
    );
    act(() => {
      result.current.onSignal(asked('a'));
    });
    expect(result.current.items).toHaveLength(1);
    rerender({ threadId: 't-2' });
    expect(result.current.items).toHaveLength(0);
  });

  // A Stop aborts the stream before the cancel's resolution can arrive: the form still goes
  // with its run instead of staying live-looking for the rest of the thread.
  it('drops an unsettled form once the thread stops and the server no longer runs it', () => {
    const { result, rerender } = renderHook(
      ({ isRunning, liveRunId }) => useThreadElicitations('t-1', isRunning, liveRunId),
      { initialProps: { isRunning: true, liveRunId: undefined as string | undefined } },
    );
    act(() => {
      result.current.onSignal(asked('a', 'run-1'));
    });
    rerender({ isRunning: false, liveRunId: 'run-1' });
    expect(result.current.items).toHaveLength(1);
    rerender({ isRunning: false, liveRunId: undefined });
    expect(result.current.items).toHaveLength(0);
  });
});
```

Create `web/src/questions/__tests__/elicitationApi.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchOpenElicitations, postElicitationAnswer } from '../elicitationApi';

function respond(response: Response) {
  const fetchStub = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) =>
    Promise.resolve(response),
  );
  vi.stubGlobal('fetch', fetchStub);
  return fetchStub;
}

const OPEN = {
  run_id: 'run-1',
  id: 'q-1',
  server: 'forms',
  message: 'm',
  fields: [{ name: 'name', kind: 'string', required: true }],
  deadline: '2026-09-25T10:05:00Z',
};

describe('postElicitationAnswer', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('posts the answer once, owner-cookied and keyed', async () => {
    const fetchStub = respond(new Response('{"status":"delivered"}', { status: 202 }));
    const body = { action: 'accept', content: { name: 'Ada' } } as const;
    await expect(postElicitationAnswer('run-1', 'q/1', body, 'key-1')).resolves.toEqual({
      kind: 'delivered',
    });
    const [url, init] = fetchStub.mock.calls[0] ?? [];
    expect(url).toBe('/agent/runs/run-1/elicitations/q%2F1');
    expect(init?.method).toBe('POST');
    expect(init?.credentials).toBe('same-origin');
    expect(new Headers(init?.headers).get('Idempotency-Key')).toBe('key-1');
    expect(JSON.parse(init?.body as string)).toEqual(body);
  });

  it('classifies every refusal the route has, keeping only string codes', async () => {
    const codes = { email: 'format', '': 'not_asked', bad: 3 };
    respond(new Response(JSON.stringify({ errors: codes }), { status: 422 }));
    await expect(postElicitationAnswer('r', 'q', { action: 'accept' }, 'k')).resolves.toEqual({
      kind: 'invalid',
      errors: { email: 'format', '': 'not_asked' },
    });
    respond(new Response('{"error":"question already resolved"}', { status: 409 }));
    await expect(postElicitationAnswer('r', 'q', { action: 'decline' }, 'k')).resolves.toEqual({
      kind: 'closed',
    });
    respond(new Response('run has ended', { status: 410 }));
    await expect(postElicitationAnswer('r', 'q', { action: 'decline' }, 'k')).resolves.toEqual({
      kind: 'gone',
    });
    respond(new Response('question not found', { status: 404 }));
    await expect(postElicitationAnswer('r', 'q', { action: 'decline' }, 'k')).rejects.toThrow(
      'question not found',
    );
  });

  it('reads a 422 with no usable body as no field errors', async () => {
    respond(new Response('<html>', { status: 422 }));
    await expect(postElicitationAnswer('r', 'q', { action: 'accept' }, 'k')).resolves.toEqual({
      kind: 'invalid',
      errors: {},
    });
  });
});

describe('fetchOpenElicitations', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("lists the run's open forms and drops an entry it cannot trust", async () => {
    const body = { questions: [OPEN, { ...OPEN, id: 9 }] };
    const fetchStub = respond(new Response(JSON.stringify(body), { status: 200 }));
    await expect(fetchOpenElicitations('run-1')).resolves.toEqual([OPEN]);
    const [url, init] = fetchStub.mock.calls[0] ?? [];
    expect(url).toBe('/agent/runs/run-1/elicitations');
    expect(init?.credentials).toBe('same-origin');
  });

  it('reads a body with no list as no forms, and a refusal as an error', async () => {
    respond(new Response('{}', { status: 200 }));
    await expect(fetchOpenElicitations('run-1')).resolves.toEqual([]);
    respond(new Response('run not found', { status: 404 }));
    await expect(fetchOpenElicitations('run-1')).rejects.toThrow('run not found');
  });
});
```

Create `web/src/questions/__tests__/elicitationSteps.test.ts`. Under `exactOptionalPropertyTypes` a fixture leaves a bound out rather than setting it to `undefined`:

```ts
import { describe, expect, it } from 'vitest';
import type { ElicitationField } from '../../chat/sseAdapter_elicitation';
import {
  contentFrom,
  fieldTitle,
  firstFailingStep,
  hasValue,
  initialValue,
  itemsHint,
  localDateTime,
  optionsFor,
  receivedText,
  selectedIds,
  summaryOf,
  toggleValue,
  withinItemBounds,
} from '../elicitationSteps';

const LABELS = { yes: 'Yes', no: 'No' };
type Needed = Pick<ElicitationField, 'name' | 'kind'>;
const field = (over: Partial<ElicitationField> & Needed): ElicitationField => ({
  required: false,
  ...over,
});

describe('elicitationSteps', () => {
  it("starts each field at the server's default, when the default fits", () => {
    const multi = { name: 'a', kind: 'enum', multi: true, enum: ['x'] } as const;
    expect(initialValue(field({ name: 'a', kind: 'boolean', default: true }))).toBe(true);
    expect(initialValue(field({ name: 'a', kind: 'boolean', default: 'yes' }))).toBeUndefined();
    expect(initialValue(field({ name: 'a', kind: 'integer', default: 3 }))).toBe(3);
    expect(initialValue(field({ name: 'a', kind: 'string', default: 'blue' }))).toBe('blue');
    expect(initialValue(field({ name: 'a', kind: 'enum', enum: ['x'], default: 'x' }))).toBe('x');
    expect(initialValue(field({ ...multi, default: ['x'] }))).toEqual(['x']);
    expect(initialValue(field({ ...multi, default: 'x' }))).toBeUndefined();
  });

  it('shows a date-time default in the local shape its input accepts, at the same instant', () => {
    const when = field({ name: 'w', kind: 'string', format: 'date-time' });
    const shown = initialValue({ ...when, default: '2026-09-25T10:30:15Z' });
    expect(shown).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}$/);
    expect(new Date(shown as string).toISOString()).toBe('2026-09-25T10:30:15.000Z');
    expect(localDateTime('not a date')).toBeUndefined();
  });

  it('knows when a step has a value', () => {
    const values = [undefined, '  ', [], 'a', 0, false, ['a']] as const;
    expect(values.map((value) => hasValue(value))).toEqual([
      false,
      false,
      false,
      true,
      true,
      true,
      true,
    ]);
  });

  it('holds a multi choice to its item bounds, and lets an optional one stay empty', () => {
    const bounded = field({ name: 'm', kind: 'enum', multi: true, min_items: 1, max_items: 2 });
    expect(withinItemBounds(bounded, undefined)).toBe(true);
    expect(withinItemBounds({ ...bounded, required: true }, [])).toBe(false);
    expect(withinItemBounds(bounded, ['a'])).toBe(true);
    expect(withinItemBounds(bounded, ['a', 'b', 'c'])).toBe(false);
    expect(withinItemBounds(field({ name: 's', kind: 'string' }), 'x')).toBe(true);
    expect(itemsHint(bounded)).toEqual({ key: 'range', params: { min: 1, max: 2 } });
    const multi = { name: 'm', kind: 'enum', multi: true } as const;
    expect(itemsHint(field({ ...multi, min_items: 1 }))).toEqual({
      key: 'atLeast',
      params: { min: 1 },
    });
    expect(itemsHint(field({ ...multi, max_items: 2 }))).toEqual({
      key: 'atMost',
      params: { max: 2 },
    });
    expect(itemsHint(field(multi))).toBeNull();
    expect(itemsHint(field({ name: 's', kind: 'string', min_items: 1 }))).toBeNull();
  });

  it('sends only what has a value, and a local date-time as an RFC 3339 instant', () => {
    const fields = [
      field({ name: 'when', kind: 'string', format: 'date-time' }),
      field({ name: 'n', kind: 'number' }),
      field({ name: 'empty', kind: 'string' }),
    ];
    const content = contentFrom(fields, { when: '2026-09-25T10:30', n: 2.5, empty: '' });
    expect(content.n).toBe(2.5);
    expect('empty' in content).toBe(false);
    expect(content.when).toBe(new Date('2026-09-25T10:30').toISOString());
  });

  it('draws enum rows with their titles, and a boolean as Yes and No', () => {
    const pet = field({ name: 'p', kind: 'enum', enum: ['cat', 'dog'], enum_titles: ['Cat', ''] });
    expect(optionsFor(pet, LABELS)).toEqual([
      { id: 'cat', label: 'Cat' },
      { id: 'dog', label: 'dog' },
    ]);
    const yesNo = optionsFor(field({ name: 'b', kind: 'boolean' }), LABELS);
    expect(yesNo.map((o) => o.label)).toEqual(['Yes', 'No']);
  });

  it('toggles a row into the value its field keeps', () => {
    const multi = field({ name: 'm', kind: 'enum', multi: true, enum: ['a', 'b'] });
    expect(toggleValue(field({ name: 'b', kind: 'boolean' }), undefined, 'false')).toBe(false);
    expect(toggleValue(field({ name: 's', kind: 'enum', enum: ['a'] }), 'b', 'a')).toBe('a');
    expect(toggleValue(multi, ['a'], 'b')).toEqual(['a', 'b']);
    expect(toggleValue(multi, ['a', 'b'], 'a')).toEqual(['b']);
    expect([...selectedIds(true)]).toEqual(['true']);
    expect([...selectedIds(['a', 'b'])]).toEqual(['a', 'b']);
    expect([...selectedIds(undefined)]).toEqual([]);
  });

  it('goes back to the first refused field, or the first step for a whole-answer error', () => {
    const fields = [field({ name: 'a', kind: 'string' }), field({ name: 'b', kind: 'string' })];
    expect(firstFailingStep(fields, { b: 'invalid' })).toBe(1);
    expect(firstFailingStep(fields, { '': 'not_asked' })).toBe(0);
  });

  it('summarises what the server receives, defaults included', () => {
    const fields = [
      field({ name: 'pet', kind: 'enum', enum: ['cat'], enum_titles: ['Cat'], title: 'Pet' }),
      field({ name: 'agree', kind: 'boolean' }),
      field({ name: 'tags', kind: 'enum', multi: true, enum: ['a', 'b'] }),
      field({ name: 'age', kind: 'integer' }),
      field({ name: 'note', kind: 'string' }),
      field({ name: 'color', kind: 'string', default: 'blue' }),
      field({ name: 'size', kind: 'enum', enum: ['s', 'm'], default: 3 }),
    ];
    const values = { pet: 'cat', agree: false, tags: ['a', 'b'], age: 36 };
    expect(summaryOf(fields, values, LABELS)).toEqual([
      { label: 'Pet', value: 'Cat' },
      { label: 'agree', value: 'No' },
      { label: 'tags', value: 'a, b' },
      { label: 'age', value: '36' },
      { label: 'color', value: 'blue' },
      { label: 'size', value: '3' },
    ]);
    const required = field({ name: 'r', kind: 'string', required: true, default: 'x' });
    expect(receivedText(required, undefined, LABELS)).toBeUndefined();
    expect(fieldTitle(field({ name: 'n', kind: 'string', title: '' }))).toBe('n');
  });
});
```

Create `web/src/questions/__tests__/useCountdown.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { formatRemaining, secondsLeft, useCountdown } from '../useCountdown';

describe('useCountdown', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('counts down each second and stops at zero', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-09-25T10:00:00Z'));
    const { result } = renderHook(() => useCountdown('2026-09-25T10:00:02Z'));
    expect(result.current).toBe(2);
    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(result.current).toBe(0);
  });

  it('reads an unparseable deadline as already passed, and shows m:ss', () => {
    expect(secondsLeft('never', Date.now())).toBe(0);
    expect(formatRemaining(300)).toBe('5:00');
    expect(formatRemaining(59)).toBe('0:59');
  });
});
```

Create `web/src/questions/__tests__/ElicitationCard.test.tsx`:

```tsx
import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import type { ElicitationField, ElicitationQuestion } from '../../chat/sseAdapter_elicitation';
import { ElicitationCard } from '../ElicitationCard';
import type { ElicitationItem } from '../useThreadElicitations';

// The order internal/elicit sends: required fields in the schema's order, then the rest by name.
const NAME: ElicitationField = {
  name: 'name',
  kind: 'string',
  required: true,
  title: 'Name',
  description: 'Your full name',
};
const PET: ElicitationField = {
  name: 'pet',
  kind: 'enum',
  required: true,
  title: 'Pet',
  enum: ['cat', 'dog'],
  enum_titles: ['Cat', ''],
};
const EMAIL: ElicitationField = {
  name: 'email',
  kind: 'string',
  required: false,
  format: 'email',
  title: 'Email',
};
const TOPPINGS: ElicitationField = {
  name: 'toppings',
  kind: 'enum',
  required: false,
  multi: true,
  enum: ['ham', 'egg'],
};

function question(over: Partial<ElicitationQuestion> = {}): ElicitationQuestion {
  return {
    run_id: 'run-1',
    id: 'q-1',
    server: 'forms',
    tool: 'ask_name',
    message: 'Tell me about <b>you</b>',
    fields: [NAME, PET, EMAIL, TOPPINGS],
    deadline: new Date(Date.now() + 300_000).toISOString(),
    ...over,
  };
}

interface Post {
  readonly url: string;
  readonly body: unknown;
  readonly key: string | null;
}

function stubAnswers(posts: Post[], ...responses: Response[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      const key = new Headers(init?.headers).get('Idempotency-Key');
      posts.push({ url, body: JSON.parse(init?.body as string), key });
      const delivered = new Response('{"status":"delivered"}', { status: 202 });
      return Promise.resolve(responses.shift() ?? delivered);
    }),
  );
}

function refusal(errors: Record<string, string>): Response {
  return new Response(JSON.stringify({ errors }), { status: 422 });
}

function renderCard(item: ElicitationItem, isStreaming = true) {
  return render(<ElicitationCard item={item} isStreaming={isStreaming} />);
}

const click = (name: string) => {
  fireEvent.click(screen.getByRole('button', { name }));
};

describe('ElicitationCard', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it('walks one step per field, reviews every answer, and submits only from Review', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    renderCard({ question: question() });

    expect(screen.getByText('Step 1 of 5')).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Name' })).toBeTruthy();
    const name = screen.getByRole('textbox');
    expect(name.getAttribute('aria-describedby')).toBe(
      screen.getByText('Your full name').getAttribute('id'),
    );
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Next' }).disabled).toBe(true);
    fireEvent.change(name, { target: { value: 'Ada' } });
    fireEvent.keyDown(name, { key: 'Enter' });

    expect(screen.getByText('Step 2 of 5')).toBeTruthy();
    expect(screen.getByRole('option', { name: 'dog' })).toBeTruthy();
    fireEvent.click(screen.getByRole('option', { name: 'Cat' }));
    click('Next');

    expect(screen.getByRole('textbox').getAttribute('type')).toBe('email');
    click('Skip');

    expect(screen.getByRole('listbox').getAttribute('aria-multiselectable')).toBe('true');
    fireEvent.click(screen.getByRole('option', { name: 'ham' }));
    fireEvent.click(screen.getByRole('option', { name: 'egg' }));
    click('Back');
    expect(screen.getByRole('heading', { name: 'Email' })).toBeTruthy();
    click('Next');
    expect(screen.queryByRole('button', { name: 'Submit' })).toBeNull();
    click('Review');

    expect(screen.getByText('Step 5 of 5')).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Review your answers' })).toBeTruthy();
    expect(screen.getByRole('button', { name: /Change Name/ }).textContent).toContain('Ada');
    expect(screen.getByRole('button', { name: /Change Email/ }).textContent).toContain('Not given');
    fireEvent.click(screen.getByRole('button', { name: /Change Pet/ }));
    expect(screen.getByRole('option', { name: 'Cat' }).getAttribute('aria-selected')).toBe('true');
    click('Next');
    click('Next');
    click('Review');
    expect(posts).toHaveLength(0);
    click('Submit');

    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    expect(posts[0]?.url).toBe('/agent/runs/run-1/elicitations/q-1');
    expect(posts[0]?.body).toEqual({
      action: 'accept',
      content: { name: 'Ada', pet: 'cat', toppings: ['ham', 'egg'] },
    });
    expect(posts[0]?.key).toMatch(/^[0-9a-f-]{36}$/);
  });

  it('draws a boolean as Yes and No, prefilled, and a number with its bounds', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    const age: ElicitationField = {
      name: 'age',
      kind: 'integer',
      required: true,
      min: 0,
      max: 150,
    };
    const agree: ElicitationField = {
      name: 'agree',
      kind: 'boolean',
      required: true,
      default: true,
    };
    renderCard({ question: question({ fields: [age, agree] }) });

    const input = screen.getByRole('spinbutton');
    const bounds = ['min', 'max', 'step'].map((attribute) => input.getAttribute(attribute));
    expect(bounds).toEqual(['0', '150', '1']);
    fireEvent.change(input, { target: { value: '36' } });
    click('Next');
    expect(screen.getByRole('option', { name: 'Yes' }).getAttribute('aria-selected')).toBe('true');
    click('Review');
    click('Submit');
    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    expect(posts[0]?.body).toEqual({ action: 'accept', content: { age: 36, agree: true } });
  });

  it('bounds a string by its lengths, takes a decimal, and sends nothing for a cleared input', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    const code: ElicitationField = {
      name: 'code',
      kind: 'string',
      required: false,
      min_length: 4,
      max_length: 8,
    };
    const ratio: ElicitationField = { name: 'ratio', kind: 'number', required: false };
    const at: ElicitationField = {
      name: 'at',
      kind: 'string',
      required: false,
      format: 'date-time',
    };
    renderCard({ question: question({ fields: [code, ratio, at] }) });

    const text = screen.getByRole('textbox');
    expect(['minlength', 'maxlength'].map((attribute) => text.getAttribute(attribute))).toEqual([
      '4',
      '8',
    ]);
    fireEvent.change(text, { target: { value: 'abcd' } });
    fireEvent.change(text, { target: { value: '' } });
    fireEvent.keyDown(text, { key: 'Tab' });
    expect(screen.getByText('Step 1 of 4')).toBeTruthy();
    click('Next');

    const decimal = screen.getByRole('spinbutton');
    expect(decimal.getAttribute('step')).toBe('any');
    fireEvent.change(decimal, { target: { value: '0.5' } });
    click('Next');

    // A seconds-precise default must fit, so the picker steps by the second.
    expect(document.querySelector('input[type="datetime-local"]')?.getAttribute('step')).toBe('1');
    click('Review');
    click('Submit');
    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    expect(posts[0]?.body).toEqual({ action: 'accept', content: { ratio: 0.5 } });
  });

  // Review Focus 5.
  it('a 422 puts the card back on the failing step, with the error there', async () => {
    const posts: Post[] = [];
    stubAnswers(posts, refusal({ email: 'format' }));
    renderCard({ question: question({ fields: [NAME, EMAIL] }) });
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Ada' } });
    click('Next');
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'nope' } });
    click('Review');
    click('Submit');

    await waitFor(() => {
      expect(screen.getByText('Step 2 of 3')).toBeTruthy();
    });
    const email = screen.getByRole<HTMLInputElement>('textbox');
    expect(email.value).toBe('nope');
    expect(email.getAttribute('aria-invalid')).toBe('true');
    expect(screen.getByText('This value is not valid.')).toBeTruthy();
    fireEvent.change(email, { target: { value: 'ada@example.com' } });
    expect(email.getAttribute('aria-invalid')).toBeNull();
    click('Review');
    click('Submit');
    await waitFor(() => {
      expect(posts).toHaveLength(2);
    });
    expect(posts[1]?.body).toEqual({
      action: 'accept',
      content: { email: 'ada@example.com', name: 'Ada' },
    });
  });

  it('reads a required-field refusal as required, and a whole-answer one on the card', async () => {
    const posts: Post[] = [];
    stubAnswers(posts, refusal({ name: 'required', '': 'not_asked' }));
    renderCard({ question: question({ fields: [NAME] }) });
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Ada' } });
    click('Submit');
    expect(await screen.findByText('This field is required.')).toBeTruthy();
    expect(screen.getByRole('status').textContent).toBe('This value is not valid.');
  });

  it('says Use default on a defaulted field, and the receipt shows what the server receives', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    const color: ElicitationField = {
      name: 'color',
      kind: 'string',
      required: false,
      default: 'blue',
    };
    const q = question({ fields: [NAME, color] });
    const { rerender } = renderCard({ question: q });
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Ada' } });
    click('Next');
    expect(screen.getByRole<HTMLInputElement>('textbox').value).toBe('blue');
    expect(screen.queryByRole('button', { name: 'Skip' })).toBeNull();
    click('Use default');
    expect(screen.getByRole('button', { name: /Change color/ }).textContent).toContain('blue');
    click('Submit');
    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    expect(posts[0]?.body).toEqual({ action: 'accept', content: { name: 'Ada' } });
    rerender(<ElicitationCard item={{ question: q, outcome: 'accepted' }} isStreaming />);
    expect(screen.getByText('color')).toBeTruthy();
    expect(screen.getByText('blue')).toBeTruthy();
  });

  it('says how many a bounded multi choice takes, and holds Submit outside the bounds', () => {
    const tags: ElicitationField = {
      name: 'tags',
      kind: 'enum',
      required: false,
      multi: true,
      enum: ['a', 'b', 'c'],
      min_items: 1,
      max_items: 2,
    };
    renderCard({ question: question({ fields: [tags] }) });
    const hint = screen.getByText('Choose 1 to 2.');
    expect(screen.getByRole('listbox').getAttribute('aria-describedby')).toBe(hint.id);
    const submit = screen.getByRole<HTMLButtonElement>('button', { name: 'Submit' });
    expect(submit.disabled).toBe(false);
    for (const option of ['a', 'b', 'c'])
      fireEvent.click(screen.getByRole('option', { name: option }));
    expect(submit.disabled).toBe(true);
    fireEvent.click(screen.getByRole('option', { name: 'c' }));
    expect(submit.disabled).toBe(false);
  });

  it('shows the receipt, with the answer given, once the stream says it was accepted', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    const q = question({ fields: [NAME] });
    const { rerender } = renderCard({ question: q });
    expect(screen.queryByText(/Step \d/)).toBeNull();
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Ada' } });
    click('Submit');
    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    rerender(<ElicitationCard item={{ question: q, outcome: 'accepted' }} isStreaming />);
    const chip = screen.getByText('Answered.').closest('[data-tone]');
    expect(chip?.getAttribute('data-tone')).toBe('success');
    expect(screen.getByText('Ada')).toBeTruthy();
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('makes every other ending a muted receipt', () => {
    for (const [outcome, label] of [
      ['declined', 'Declined.'],
      ['cancelled', 'Cancelled.'],
      ['expired', 'Expired: cancelled automatically.'],
    ] as const) {
      const { unmount } = renderCard({ question: question(), outcome });
      const chip = screen.getByText(label).closest('[data-tone]');
      expect(chip?.getAttribute('data-tone')).toBe('neutral');
      unmount();
    }
  });

  it('always offers Decline, and Cancel asks first while the run streams', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    renderCard({ question: question() });
    click('Cancel');
    expect(screen.getByText('Cancel this request?')).toBeTruthy();
    expect(posts).toHaveLength(0);
    click('Cancel request');
    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    expect(posts[0]?.body).toEqual({ action: 'cancel' });
  });

  it('declines without sending anything the operator typed', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    renderCard({ question: question({ fields: [NAME] }) });
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'secret' } });
    click('Decline');
    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    expect(posts[0]?.body).toEqual({ action: 'decline' });
  });

  it('says a closed form was already resolved, and a failed send can be retried', async () => {
    const posts: Post[] = [];
    stubAnswers(posts, new Response('{"error":"question already resolved"}', { status: 409 }));
    const { unmount } = renderCard({ question: question() });
    click('Decline');
    expect(await screen.findByText('This form was already resolved.')).toBeTruthy();
    unmount();

    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new Error('offline'))),
    );
    renderCard({ question: question() });
    click('Decline');
    expect(await screen.findByText("Couldn't send your answer. Try again.")).toBeTruthy();
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Decline' }).disabled).toBe(false);
  });

  it('a form Aura refused says why and offers nothing', () => {
    renderCard({ question: question({ refusal: 'ambiguous_run', fields: [], message: '' }) });
    expect(
      screen.getByText(
        'Aura declined this form because more than one conversation was using this server.',
      ),
    ).toBeTruthy();
    expect(screen.queryByRole('button')).toBeNull();
    expect(screen.queryByRole('timer')).toBeNull();
  });

  it("names the server by its mount, keeps the message as plain text, and counts down Aura's bound", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-09-25T10:00:00Z'));
    renderCard({ question: question({ deadline: '2026-09-25T10:05:00Z' }) });
    expect(screen.getByText('forms')).toBeTruthy();
    expect(screen.getByText('ask_name')).toBeTruthy();
    const message = screen.getByText('Tell me about <b>you</b>');
    expect(screen.getByRole('form').getAttribute('aria-describedby')).toBe(message.id);
    expect(document.querySelector('b')).toBeNull();
    const timer = screen.getByRole('timer');
    expect(timer.getAttribute('aria-label')).toBe('Aura cancels in 5:00');
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(screen.getByRole('timer').textContent).toContain('4:59');
  });
});
```

In `web/src/approvals/__tests__/ThreadApprovalCards.test.tsx`, after the last test:

```diff
--- a/web/src/approvals/__tests__/ThreadApprovalCards.test.tsx
+++ b/web/src/approvals/__tests__/ThreadApprovalCards.test.tsx
@@ -220,4 +220,32 @@
     expect(screen.getByRole('status')).not.toBe(firstAnnouncement);
     expect(screen.getByRole('status').textContent).toBe('Answered.');
   });
+
+  it("draws a mounted server's forms after the approvals, in arrival order", () => {
+    const form = (id: string) => ({
+      run_id: 'run-1',
+      id,
+      server: 'forms',
+      message: 'm',
+      fields: [],
+      deadline: '2026-09-25T10:05:00Z',
+    });
+    render(
+      <QueryClientProvider client={client()}>
+        <ThreadApprovalCards
+          approvals={[approval({ token: 't-1', conversation_id: 'c-1' })]}
+          elicitations={[
+            { question: form('open') },
+            { question: form('done'), outcome: 'declined' },
+          ]}
+          isStreaming
+        />
+      </QueryClientProvider>,
+    );
+    const cards = document.querySelectorAll('[data-approval-token], [data-elicitation-id]');
+    const order = Array.from(cards).map(
+      (el) => el.getAttribute('data-approval-token') ?? el.getAttribute('data-elicitation-id'),
+    );
+    expect(order).toEqual(['t-1', 'open', 'done']);
+  });
 });
```

- [ ] **Step 2: Run them to verify they fail.** Web: `npx vitest run src/chat/sseAdapter.onElicitation.test.ts src/questions src/approvals/__tests__/ThreadApprovalCards.test.tsx`.

Expected, measured on a copy of HEAD with Task 7 applied and only this step's tests: six suites do not load, and one test fails.
- `sseAdapter.onElicitation.test.ts`: `Failed to resolve import "./sseAdapter_elicitation"`; likewise `../useThreadElicitations`, `../elicitationApi`, `../elicitationSteps`, `../useCountdown` and `../ElicitationCard` in the five `src/questions/__tests__` suites.
- `ThreadApprovalCards.test.tsx`: `expected [ 't-1' ] to deeply equal [ 't-1', 'open', 'done' ]`. The other 16 tests pass.

- [ ] **Step 3: Implement.** Create `web/src/chat/sseAdapter_elicitation.ts`:

```ts
import type { AguiFrame } from './sseAdapter_frames';

// sseAdapter_elicitation reads the two CUSTOM frames a mounted MCP server's form travels as
// (internal/agui/run_elicitation.go): aura.elicitation carries the question and
// aura.elicitation_resolved how it closed. Neither carries an answer value. Like aura.steer
// they are a PUMP-level signal, fired from streamSSE and sseResume's pumpBody and never reduced
// into a message part: a form is not part of the assistant's answer.

export type ElicitationKind = 'string' | 'number' | 'integer' | 'boolean' | 'enum';
export type ElicitationFormat = 'email' | 'uri' | 'date' | 'date-time';
export type ElicitationRefusal = 'unrenderable' | 'ambiguous_run';
export type ElicitationAction = 'accept' | 'decline' | 'cancel';

export interface ElicitationField {
  readonly name: string;
  readonly title?: string;
  readonly description?: string;
  readonly kind: ElicitationKind;
  readonly required: boolean;
  readonly default?: unknown;
  readonly enum?: readonly string[];
  readonly enum_titles?: readonly string[];
  readonly multi?: boolean;
  readonly format?: ElicitationFormat;
  readonly min?: number;
  readonly max?: number;
  readonly min_length?: number;
  readonly max_length?: number;
  readonly min_items?: number;
  readonly max_items?: number;
}

export interface ElicitationQuestion {
  readonly run_id: string;
  readonly id: string;
  /** The name Aura mounted the server under, never one the server gave itself. */
  readonly server: string;
  readonly tool?: string;
  readonly message: string;
  readonly fields: readonly ElicitationField[];
  readonly deadline: string;
  readonly refusal?: ElicitationRefusal;
}

export interface ElicitationResolved {
  readonly id: string;
  /** An expiry is a cancel with `expired` set: the server hears cancel either way. */
  readonly action: ElicitationAction;
  readonly expired?: boolean;
}

export type ElicitationSignal =
  | { readonly kind: 'question'; readonly question: ElicitationQuestion }
  | { readonly kind: 'resolved'; readonly resolved: ElicitationResolved };

const KINDS: readonly string[] = ['string', 'number', 'integer', 'boolean', 'enum'];
const FORMATS: readonly string[] = ['email', 'uri', 'date', 'date-time'];
const REFUSALS: readonly string[] = ['unrenderable', 'ambiguous_run'];
const ACTIONS: readonly string[] = ['accept', 'decline', 'cancel'];

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

export function isStringList(value: unknown): value is readonly string[] {
  return Array.isArray(value) && value.every((entry) => typeof entry === 'string');
}

function optional(value: unknown, holds: (value: unknown) => boolean): boolean {
  return value === undefined || holds(value);
}

const isString = (value: unknown) => typeof value === 'string';
const isNumber = (value: unknown) => typeof value === 'number';

function isField(value: unknown): value is ElicitationField {
  return (
    isRecord(value) &&
    typeof value.name === 'string' &&
    typeof value.kind === 'string' &&
    KINDS.includes(value.kind) &&
    typeof value.required === 'boolean' &&
    optional(value.title, isString) &&
    optional(value.description, isString) &&
    optional(value.enum, isStringList) &&
    optional(value.enum_titles, isStringList) &&
    optional(value.multi, (multi) => typeof multi === 'boolean') &&
    optional(value.format, (format) => typeof format === 'string' && FORMATS.includes(format)) &&
    [value.min, value.max, value.min_length, value.max_length, value.min_items, value.max_items]
      .map((bound) => optional(bound, isNumber))
      .every(Boolean)
  );
}

/** Narrow one question as the stream and GET /agent/runs/{runID}/elicitations both carry it. */
export function elicitationQuestionOf(value: unknown): ElicitationQuestion | null {
  if (!isRecord(value)) return null;
  const { run_id: runId, id, server, tool, message, deadline, refusal } = value;
  if (typeof runId !== 'string' || typeof id !== 'string' || typeof server !== 'string') {
    return null;
  }
  if (typeof message !== 'string' || typeof deadline !== 'string' || !optional(tool, isString)) {
    return null;
  }
  if (!optional(refusal, (code) => typeof code === 'string' && REFUSALS.includes(code))) {
    return null;
  }
  // A refused form's fields arrive as null: Go encodes the refusal's nil slice that way.
  const fields = value.fields ?? [];
  if (!Array.isArray(fields) || !fields.every(isField)) return null;
  return {
    run_id: runId,
    id,
    server,
    message,
    deadline,
    fields,
    ...(typeof tool === 'string' ? { tool } : {}),
    ...(typeof refusal === 'string' ? { refusal: refusal as ElicitationRefusal } : {}),
  };
}

function resolvedOf(value: unknown): ElicitationResolved | null {
  if (!isRecord(value) || typeof value.id !== 'string' || typeof value.action !== 'string') {
    return null;
  }
  if (!ACTIONS.includes(value.action)) return null;
  if (!optional(value.expired, (expired) => typeof expired === 'boolean')) return null;
  return {
    id: value.id,
    action: value.action as ElicitationAction,
    ...(value.expired === true ? { expired: true } : {}),
  };
}

/** Narrow a frame to its elicitation signal, or null: the aura.steer twin (steerNoticeValue). */
export function elicitationSignalValue(frame: AguiFrame): ElicitationSignal | null {
  if (frame.type !== 'CUSTOM') return null;
  if (frame.name === 'aura.elicitation') {
    const question = elicitationQuestionOf(frame.value);
    return question === null ? null : { kind: 'question', question };
  }
  if (frame.name === 'aura.elicitation_resolved') {
    const resolved = resolvedOf(frame.value);
    return resolved === null ? null : { kind: 'resolved', resolved };
  }
  return null;
}
```

In `web/src/chat/sseAdapter.ts`:

```diff
--- a/web/src/chat/sseAdapter.ts
+++ b/web/src/chat/sseAdapter.ts
@@ -1,6 +1,7 @@
 import type { ThreadMessageLike } from '@assistant-ui/react';
 import { isDisplayPayload, type DisplayPayload } from './displays/types';
 import { isMcpViewDescriptor } from './mcpapps/hostProtocol';
+import { elicitationSignalValue, type ElicitationSignal } from './sseAdapter_elicitation';
 import { errorDetail, type AguiFrame } from './sseAdapter_frames';
 import {
   ensureReasoning,
@@ -300,10 +301,11 @@
       if (frame.name === 'aura.discard' && isDiscardNotice(frame.value)) {
         discardText(state, frame.value.message_id);
       }
-      // aura.steer (amendment #132, STEER-03) is deliberately NOT handled here: it falls
-      // through to the default no-op below every branch above, exactly as an unrecognized
-      // frame does. steerNoticeValue (above) is the ONLY decision point for it, fired from
-      // the PUMP (streamSSE below, and sseResume.ts's pumpBody) — never from this reducer.
+      // aura.steer (amendment #132, STEER-03) and aura.elicitation* (spec 2026-09-25) are
+      // deliberately NOT handled here: they fall through to the default no-op below every
+      // branch above, exactly as an unrecognized frame does. steerNoticeValue (above) and
+      // elicitationSignalValue are the ONLY decision points for them, fired from the PUMP
+      // (streamSSE below, and sseResume.ts's pumpBody) — never from this reducer.
       return state;
     }
     case 'REASONING_START':
@@ -416,6 +418,9 @@
   /** Fires once per `aura.steer` frame (amendment #132, STEER-03) — the mid-turn redirect
    *  echo, from the PUMP, never from reduceFrame. Drives the cockpit's SteerNotice. */
   readonly onSteer?: (notice: SteerNotice) => void;
+  /** Fires once per aura.elicitation / aura.elicitation_resolved frame: a mounted MCP
+   *  server's form and how it closed, from the PUMP, never from reduceFrame. */
+  readonly onElicitation?: (signal: ElicitationSignal) => void;
   /** Mints the assistant message id; defaults to crypto.randomUUID. */
   readonly newId?: () => string;
 }
@@ -431,6 +436,7 @@
   readonly onArtifact?: (assetId: string | undefined) => void;
   /** Mirrors StreamRunOptions.onSteer — the mid-turn redirect echo. */
   readonly onSteer?: (notice: SteerNotice) => void;
+  readonly onElicitation?: (signal: ElicitationSignal) => void;
   readonly newId?: () => string;
 }
 
@@ -444,6 +450,7 @@
   readonly onUpdate: (message: ThreadMessageLike, usage: TurnUsage | undefined) => void;
   readonly onArtifact?: ((assetId: string | undefined) => void) | undefined;
   readonly onSteer?: ((notice: SteerNotice) => void) | undefined;
+  readonly onElicitation?: ((signal: ElicitationSignal) => void) | undefined;
   readonly newId?: (() => string) | undefined;
 }
 
@@ -471,6 +478,8 @@
     if (artifact !== null) opts.onArtifact?.(artifact.asset_id);
     const steer = steerNoticeValue(frame);
     if (steer !== null) opts.onSteer?.(steer);
+    const elicitation = elicitationSignalValue(frame);
+    if (elicitation !== null) opts.onElicitation?.(elicitation);
     opts.onUpdate(toThreadMessage(state), state.usage);
   }
   return state.usage;
@@ -488,6 +497,7 @@
     onUpdate: opts.onUpdate,
     onArtifact: opts.onArtifact,
     onSteer: opts.onSteer,
+    onElicitation: opts.onElicitation,
     request: () => [
       opts.url,
       {
@@ -513,6 +523,7 @@
     onUpdate: opts.onUpdate,
     onArtifact: opts.onArtifact,
     onSteer: opts.onSteer,
+    onElicitation: opts.onElicitation,
     request: (id) => [
       '/agent/run',
       {
```

In `web/src/chat/sseResume.ts`. `ResilientRunOptions` extends `StreamRunOptions`, so the resilient run inherits the field and hands it to `makeEngine` with no further change.

```diff
--- a/web/src/chat/sseResume.ts
+++ b/web/src/chat/sseResume.ts
@@ -15,6 +15,7 @@
   type StreamRunOptions,
   type TurnUsage,
 } from './sseAdapter';
+import { elicitationSignalValue, type ElicitationSignal } from './sseAdapter_elicitation';
 import { errorDetail } from './sseAdapter_frames';
 import {
   runControlURL,
@@ -60,6 +61,9 @@
   /** Fires once per `aura.steer` frame — the reattach pump's half of the mid-turn redirect
    *  echo (amendment #132, STEER-03), mirroring StreamRunOptions.onSteer exactly. */
   readonly onSteer?: (notice: SteerNotice) => void;
+  /** Mirrors StreamRunOptions.onElicitation on the reattach pump: a reloaded tab's form
+   *  comes back. */
+  readonly onElicitation?: (signal: ElicitationSignal) => void;
   readonly newId?: () => string;
   readonly maxRetries?: number;
   readonly backoffBaseMs?: number;
@@ -79,6 +83,7 @@
   readonly onUpdate: (message: ThreadMessageLike, usage: TurnUsage | undefined) => void;
   readonly onArtifact?: ((assetId: string | undefined) => void) | undefined;
   readonly onSteer?: ((notice: SteerNotice) => void) | undefined;
+  readonly onElicitation?: ((signal: ElicitationSignal) => void) | undefined;
   readonly onRunId?: ((runId: string) => void) | undefined;
   readonly onSnapshotReplace?: ((messages: ThreadMessageLike[]) => void) | undefined;
   readonly onTerminal?: (() => void) | undefined;
@@ -110,6 +115,7 @@
     onUpdate: opts.onUpdate,
     onArtifact: opts.onArtifact,
     onSteer: opts.onSteer,
+    onElicitation: opts.onElicitation,
     onRunId: opts.onRunId,
     onSnapshotReplace: opts.onSnapshotReplace,
     onTerminal: opts.onTerminal,
@@ -177,6 +183,8 @@
     if (artifact !== null) eng.onArtifact?.(artifact.asset_id);
     const steer = steerNoticeValue(frame);
     if (steer !== null) eng.onSteer?.(steer);
+    const elicitation = elicitationSignalValue(frame);
+    if (elicitation !== null) eng.onElicitation?.(elicitation);
     if ((frame.type === 'RUN_FINISHED' || frame.type === 'RUN_ERROR') && !eng.terminal) {
       eng.terminal = true;
       eng.onTerminal?.();
```

In `web/src/chat/ExternalStoreChat_liveRun.ts`:

```diff
--- a/web/src/chat/ExternalStoreChat_liveRun.ts
+++ b/web/src/chat/ExternalStoreChat_liveRun.ts
@@ -7,8 +7,10 @@
   fetchConversation,
   type Conversation,
 } from '../conversations/useConversations';
+import { fetchOpenElicitations } from '../questions/elicitationApi';
 import { attachRun } from './sseResume';
 import type { SteerNotice, TurnUsage } from './sseAdapter';
+import type { ElicitationSignal } from './sseAdapter_elicitation';
 
 // ExternalStoreChat_liveRun — the RS-07 §4.2 reload-attach split out of
 // ExternalStoreChat.tsx (600-LOC cap): when a thread opens while its detached
@@ -40,6 +42,17 @@
   readonly onArtifact?: ((assetId: string | undefined) => void) | undefined;
   /** D-10's reattach-pump half: fires on an aura.steer frame observed by a reloaded tab. */
   readonly onSteer?: ((notice: SteerNotice) => void) | undefined;
+  /** A mounted MCP server's forms, from the replay and from the run's own list. */
+  readonly onElicitation?: ((signal: ElicitationSignal) => void) | undefined;
+}
+
+/** Fold a live run's open forms in. Best effort: the stream is their other source. */
+function announceOpenForms(runId: string, onElicitation: (signal: ElicitationSignal) => void) {
+  return fetchOpenElicitations(runId)
+    .then((questions) => {
+      for (const question of questions) onElicitation({ kind: 'question', question });
+    })
+    .catch(() => undefined);
 }
 
 export function useLiveRunAttach({
@@ -52,6 +65,7 @@
   setMessages,
   onArtifact,
   onSteer,
+  onElicitation,
 }: LiveRunAttachArgs): void {
   const { t } = useTranslation();
   const queryClient = useQueryClient();
@@ -61,6 +75,9 @@
   const attachLiveRun = useCallback(
     async (runId: string) => {
       const terminal = { observed: false };
+      // Once the ring has rotated past an open form, the replay below answers 410 and brings
+      // none back, so the run is asked for its open forms too. A form both deliver is held once.
+      if (onElicitation !== undefined) void announceOpenForms(runId, onElicitation);
       await foldAppendedStream(threadId, (controller, onUpdate) => {
         activeRunIdRef.current = runId;
         return attachRun({
@@ -74,6 +91,7 @@
           },
           ...(onArtifact !== undefined ? { onArtifact } : {}),
           ...(onSteer !== undefined ? { onSteer } : {}),
+          ...(onElicitation !== undefined ? { onElicitation } : {}),
           onUpdate: (assistant, usage) => {
             onUpdate(assistant, usage);
             setMessages(withoutRowsReplayedByRun);
@@ -102,6 +120,7 @@
       t,
       onArtifact,
       onSteer,
+      onElicitation,
       activeRunIdRef,
       setMessages,
       queryClient,
```

In `web/src/chat/ExternalStoreChat_streams.ts`, the field and its type import. Only `foldResumeRun` passes it on:

```diff
--- a/web/src/chat/ExternalStoreChat_streams.ts
+++ b/web/src/chat/ExternalStoreChat_streams.ts
@@ -2,6 +2,7 @@
 import type { ThreadMessageLike } from '@assistant-ui/react';
 import { assistantErrorMessage, isAbortError } from './ExternalStoreChat_folds';
 import { streamPost, type TurnUsage } from './sseAdapter';
+import type { ElicitationSignal } from './sseAdapter_elicitation';
 
 // ExternalStoreChat_streams — the scaffolding every non-primary stream shares: cancel the
 // history load in flight, take the run lock, open one AbortController, spend one usage
@@ -36,6 +37,9 @@
   ) => Promise<boolean>;
   readonly invalidateRuntimeReads: (id?: string) => void;
   readonly onArtifact?: ((assetId: string | undefined) => void) | undefined;
+  /** A mounted MCP server's forms. Only the HITL resume can meet one: it runs detached through
+   *  /agent/run, while a branch re-run streams with no asker (conversations_branch_api.go). */
+  readonly onElicitation?: ((signal: ElicitationSignal) => void) | undefined;
   /** The message shown in place of the assistant turn when the stream fails. */
   readonly streamErrorText: string;
 }
@@ -70,6 +74,7 @@
     prepareUsageBaseline,
     invalidateRuntimeReads,
     onArtifact,
+    onElicitation,
     streamErrorText,
   } = deps;
 
@@ -180,10 +185,11 @@
           body: { threadId: resumeThreadId, messages: [] },
           signal: controller.signal,
           ...(onArtifact !== undefined ? { onArtifact } : {}),
+          ...(onElicitation !== undefined ? { onElicitation } : {}),
           onUpdate,
         }),
       ),
-    [foldAppendedStream, onArtifact],
+    [foldAppendedStream, onArtifact, onElicitation],
   );
 
   return { foldReRun, foldAppendedStream, foldResumeRun };
```

In `web/src/chat/ExternalStoreChat.tsx`:

```diff
--- a/web/src/chat/ExternalStoreChat.tsx
+++ b/web/src/chat/ExternalStoreChat.tsx
@@ -17,6 +17,7 @@
 } from '../conversations/useConversations';
 import { ThreadApprovalCards } from '../approvals/ThreadApprovalCards';
 import { useThreadApprovals } from '../approvals/useThreadApprovals';
+import { useThreadElicitations } from '../questions/useThreadElicitations';
 import type { Approval } from '../approvals/useApprovals';
 import { Composer, type ComposerDraftPrompt } from './Composer';
 import { EmptyThreadStarters } from './EmptyThreadStarters';
@@ -137,6 +138,7 @@
   /** RS-07 §4.2: a set live_run_id means a detached run is in flight for this thread. */
   const liveRunId = conversation?.live_run_id;
   const steer = useSteerSend({ threadId, liveRunId, activeRunIdRef, isRunning, setMessages });
+  const elicitations = useThreadElicitations(threadId, isRunning, liveRunId);
   const { effort, setEffort } = useReasoningEffort(
     threadId,
     hydratedEffort,
@@ -245,6 +247,7 @@
           onSnapshotReplace: setMessages,
           ...(onArtifact !== undefined ? { onArtifact } : {}),
           onSteer: steer.onFrame,
+          onElicitation: elicitations.onSignal,
           onUpdate: (assistant, usage) => {
             usageLifecycle.update(usageRunId, usage);
             setMessages((prev) => {
@@ -294,6 +297,7 @@
       prepareUsageBaseline,
       compaction,
       steer,
+      elicitations.onSignal,
     ],
   );
 
@@ -425,6 +429,7 @@
     prepareUsageBaseline,
     invalidateRuntimeReads,
     onArtifact,
+    onElicitation: elicitations.onSignal,
     streamErrorText: t('chat.error.stream'),
   });
 
@@ -445,6 +450,7 @@
     setMessages,
     onArtifact,
     onSteer: steer.onFrame,
+    onElicitation: elicitations.onSignal,
   });
 
   const threadApprovals = useThreadApprovals(threadId, resumeRun, dispatchApprovalFocus);
@@ -559,6 +565,7 @@
 
             <ThreadApprovalCards
               approvals={threadApprovals.approvals}
+              elicitations={elicitations.items}
               isStreaming={isRunning}
               onResolutionStarted={threadApprovals.onResolutionStarted}
               onResolutionFailed={threadApprovals.onResolutionFailed}
```

Create `web/src/questions/useThreadElicitations.ts`. The pruning happens while rendering, the way React adjusts state that follows a prop. An effect would do the same job, but the React Compiler lint refuses a `setState` in an effect body (`react-compiler(set-state-in-effect)`).

```ts
import { useCallback, useState } from 'react';
import type {
  ElicitationQuestion,
  ElicitationResolved,
  ElicitationSignal,
} from '../chat/sseAdapter_elicitation';

// useThreadElicitations holds the thread's MCP forms as the stream tells them: a question
// arrives, and its resolution settles it. The server persists nothing. A reload gets the forms
// back from the attach's replay, or from the run's own list when the replay no longer reaches
// them (ExternalStoreChat_liveRun).

export type ElicitationOutcome = 'accepted' | 'declined' | 'cancelled' | 'expired';

export interface ElicitationItem {
  readonly question: ElicitationQuestion;
  readonly outcome?: ElicitationOutcome;
}

function outcomeOf(resolved: ElicitationResolved): ElicitationOutcome {
  if (resolved.expired === true) return 'expired';
  if (resolved.action === 'accept') return 'accepted';
  return resolved.action === 'decline' ? 'declined' : 'cancelled';
}

/**
 * Fold one signal into the list. A question already held (a replay) changes nothing. A thread
 * runs one run at a time, so the first question of a new run drops every card of earlier ones.
 * A resolution settles its question once and is ignored for a question never seen.
 */
export function applyElicitationSignal(
  items: readonly ElicitationItem[],
  signal: ElicitationSignal,
): readonly ElicitationItem[] {
  if (signal.kind === 'question') {
    const { question } = signal;
    if (items.some((item) => item.question.id === question.id)) return items;
    const sameRun = items.filter((item) => item.question.run_id === question.run_id);
    return [...sameRun, { question }];
  }
  const outcome = outcomeOf(signal.resolved);
  return items.map((item) =>
    item.question.id === signal.resolved.id && item.outcome === undefined
      ? { ...item, outcome }
      : item,
  );
}

/**
 * The cards still worth showing once the thread stops streaming: only those of the run the
 * server still names live. A form never outlives its run, and a tab can miss the resolution
 * that says so: a Stop aborts the stream first, and a cut stream loses whatever was in flight.
 */
export function keepLiveRun(
  items: readonly ElicitationItem[],
  liveRunId: string | undefined,
): readonly ElicitationItem[] {
  const kept = items.filter((item) => item.question.run_id === liveRunId);
  return kept.length === items.length ? items : kept;
}

interface ThreadForms {
  readonly threadId: string;
  readonly items: readonly ElicitationItem[];
}

export function useThreadElicitations(
  threadId: string,
  isRunning: boolean,
  liveRunId: string | undefined,
): {
  readonly items: readonly ElicitationItem[];
  readonly onSignal: (signal: ElicitationSignal) => void;
} {
  const [state, setState] = useState<ThreadForms>({ threadId, items: [] });
  const onSignal = useCallback(
    (signal: ElicitationSignal) => {
      setState((current) => ({
        threadId,
        items: applyElicitationSignal(current.threadId === threadId ? current.items : [], signal),
      }));
    },
    [threadId],
  );
  // Adjusted while rendering, the way React adjusts state that follows a prop (react.dev, "You
  // Might Not Need an Effect"): the pruned list is stored so a later run cannot bring it back.
  const held = state.threadId === threadId ? state.items : [];
  const items = isRunning ? held : keepLiveRun(held, liveRunId);
  if (items !== held) setState({ threadId, items });
  return { items, onSignal };
}
```

Create `web/src/questions/elicitationApi.ts`. `errorDetail` (`web/src/chat/http.ts:5`) puts the response body's text in the thrown message, which the 404 test reads.

```ts
import { errorDetail } from '../chat/http';
import {
  elicitationQuestionOf,
  type ElicitationAction,
  type ElicitationQuestion,
} from '../chat/sseAdapter_elicitation';

// elicitationApi is the cockpit's side of a run's MCP forms (internal/agui/
// server_run_elicitation.go): POST /agent/runs/{runID}/elicitations/{id} answers one, and
// GET /agent/runs/{runID}/elicitations lists those still open. The POST requires an
// Idempotency-Key (idempotency_http.go: agent_run_elicitation_answer). The card mints one per
// submit, and nothing retries a submit, so a replay can only be the transport's own.

/** internal/elicit ProblemRequired: the one field problem the card has its own copy for. */
export const PROBLEM_REQUIRED = 'required';

export interface ElicitationAnswerBody {
  readonly action: ElicitationAction;
  readonly content?: Readonly<Record<string, unknown>>;
}

export type ElicitationAnswerResult =
  | { readonly kind: 'delivered' }
  | { readonly kind: 'invalid'; readonly errors: Readonly<Record<string, string>> }
  | { readonly kind: 'closed' }
  | { readonly kind: 'gone' };

function runPath(runId: string): string {
  return `/agent/runs/${encodeURIComponent(runId)}/elicitations`;
}

export async function postElicitationAnswer(
  runId: string,
  id: string,
  body: ElicitationAnswerBody,
  idempotencyKey: string,
): Promise<ElicitationAnswerResult> {
  const res = await fetch(`${runPath(runId)}/${encodeURIComponent(id)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Idempotency-Key': idempotencyKey },
    credentials: 'same-origin',
    body: JSON.stringify(body),
  });
  switch (res.status) {
    case 202:
      return { kind: 'delivered' };
    case 409:
      return { kind: 'closed' };
    case 410:
      return { kind: 'gone' };
    case 422:
      return { kind: 'invalid', errors: await fieldErrors(res) };
    default:
      throw new Error(await errorDetail(res));
  }
}

/**
 * The run's open forms. A reattach whose replay the ring can no longer serve gets a 410 on
 * /events and no frame at all, so the forms come from here. An entry that does not parse is
 * dropped, as the stream drops a frame it cannot trust.
 */
export async function fetchOpenElicitations(runId: string): Promise<ElicitationQuestion[]> {
  const res = await fetch(runPath(runId), { credentials: 'same-origin' });
  if (!res.ok) throw new Error(await errorDetail(res));
  const body: unknown = await res.json();
  const questions = (body as { questions?: unknown } | null)?.questions;
  if (!Array.isArray(questions)) return [];
  return questions.flatMap((value) => {
    const question = elicitationQuestionOf(value);
    return question === null ? [] : [question];
  });
}

async function fieldErrors(res: Response): Promise<Record<string, string>> {
  const body: unknown = await res.json().catch(() => null);
  if (typeof body !== 'object' || body === null) return {};
  const errors: unknown = (body as { errors?: unknown }).errors;
  if (typeof errors !== 'object' || errors === null) return {};
  return Object.fromEntries(
    Object.entries(errors).filter(
      (entry): entry is [string, string] => typeof entry[1] === 'string',
    ),
  );
}
```

Create `web/src/questions/elicitationSteps.ts`:

```ts
import { isStringList, type ElicitationField } from '../chat/sseAdapter_elicitation';
import type { ReceiptLine } from './QuestionReceipt';

// elicitationSteps is the MCP form's pure logic: where each step starts, whether it can go on,
// what an accept sends, and what the Review step and the receipt show. Kept out of the .tsx
// files so those export only components.

export type FieldValue = string | number | boolean | readonly string[];
export type FieldValues = Readonly<Record<string, FieldValue | undefined>>;

export interface BooleanLabels {
  readonly yes: string;
  readonly no: string;
}

export interface StepOption {
  readonly id: string;
  readonly label: string;
}

/** A hint's i18n key under `questionCard.choose` and its values. */
export interface ItemsHint {
  readonly key: 'range' | 'atLeast' | 'atMost';
  readonly params: Readonly<Record<string, number>>;
}

const pad = (n: number) => String(n).padStart(2, '0');

/**
 * An RFC 3339 instant as the local YYYY-MM-DDTHH:mm:ss a datetime-local input shows: the input
 * blanks any other shape, so an unconverted default would never be seen.
 */
export function localDateTime(instant: string): string | undefined {
  const at = new Date(instant);
  if (Number.isNaN(at.getTime())) return undefined;
  const date = `${String(at.getFullYear())}-${pad(at.getMonth() + 1)}-${pad(at.getDate())}`;
  return `${date}T${pad(at.getHours())}:${pad(at.getMinutes())}:${pad(at.getSeconds())}`;
}

/** A field's starting value: the server's default when it fits the field, else nothing. */
export function initialValue(field: ElicitationField): FieldValue | undefined {
  const { default: fallback } = field;
  switch (field.kind) {
    case 'boolean':
      return typeof fallback === 'boolean' ? fallback : undefined;
    case 'number':
    case 'integer':
      return typeof fallback === 'number' ? fallback : undefined;
    case 'enum':
      if (field.multi === true) return isStringList(fallback) ? fallback : undefined;
      return typeof fallback === 'string' ? fallback : undefined;
    case 'string':
      if (typeof fallback !== 'string') return undefined;
      return field.format === 'date-time' ? localDateTime(fallback) : fallback;
  }
}

export function initialValues(fields: readonly ElicitationField[]): FieldValues {
  return Object.fromEntries(fields.map((field) => [field.name, initialValue(field)]));
}

export function hasValue(value: FieldValue | undefined): boolean {
  if (value === undefined) return false;
  if (typeof value === 'string') return value.trim() !== '';
  if (isStringList(value)) return value.length > 0;
  return true;
}

/** A multi choice inside its item bounds. An optional one left empty is inside: it is skipped. */
export function withinItemBounds(field: ElicitationField, value: FieldValue | undefined): boolean {
  if (field.multi !== true) return true;
  const count = isStringList(value) ? value.length : 0;
  if (count === 0 && !field.required) return true;
  return count >= (field.min_items ?? 0) && count <= (field.max_items ?? Infinity);
}

/** The "Choose 1 to 3." hint a bounded multi choice shows, or null. */
export function itemsHint(field: ElicitationField): ItemsHint | null {
  const { min_items: min, max_items: max } = field;
  if (field.multi !== true) return null;
  if (min !== undefined && max !== undefined) return { key: 'range', params: { min, max } };
  if (min !== undefined) return { key: 'atLeast', params: { min } };
  if (max !== undefined) return { key: 'atMost', params: { max } };
  return null;
}

/**
 * The content an accept sends: every field with a value. A datetime-local value becomes the
 * RFC 3339 instant the server's date-time check reads (internal/elicit/validate.go).
 */
export function contentFrom(
  fields: readonly ElicitationField[],
  values: FieldValues,
): Record<string, unknown> {
  const content: Record<string, unknown> = {};
  for (const field of fields) {
    const value = values[field.name];
    if (value === undefined || !hasValue(value)) continue;
    const instant = field.format === 'date-time' && typeof value === 'string';
    content[field.name] = instant ? new Date(value).toISOString() : value;
  }
  return content;
}

export function fieldTitle(field: ElicitationField): string {
  return field.title !== undefined && field.title !== '' ? field.title : field.name;
}

/** The rows an enum or a boolean field shows. An option with no title reads as its value. */
export function optionsFor(field: ElicitationField, labels: BooleanLabels): StepOption[] {
  if (field.kind === 'boolean') {
    return [
      { id: 'true', label: labels.yes },
      { id: 'false', label: labels.no },
    ];
  }
  return (field.enum ?? []).map((value, index) => {
    const title = field.enum_titles?.[index];
    return { id: value, label: title !== undefined && title !== '' ? title : value };
  });
}

export function selectedIds(value: FieldValue | undefined): ReadonlySet<string> {
  if (typeof value === 'boolean') return new Set([String(value)]);
  if (typeof value === 'string') return new Set([value]);
  if (isStringList(value)) return new Set(value);
  return new Set();
}

/** A row chosen: a boolean becomes true or false, a single choice its value, and a multi
 *  choice gains or loses it. */
export function toggleValue(
  field: ElicitationField,
  value: FieldValue | undefined,
  id: string,
): FieldValue {
  if (field.kind === 'boolean') return id === 'true';
  if (field.multi !== true) return id;
  const chosen = isStringList(value) ? value : [];
  return chosen.includes(id) ? chosen.filter((entry) => entry !== id) : [...chosen, id];
}

/** The step a 422 sends the card back to: the first refused field, else the first step. */
export function firstFailingStep(
  fields: readonly ElicitationField[],
  errors: Readonly<Record<string, string>>,
): number {
  return Math.max(
    fields.findIndex((field) => errors[field.name] !== undefined),
    0,
  );
}

/**
 * What the server receives for a field, as the operator reads it, or undefined for nothing.
 * An optional field left empty still arrives with its default: go-sdk puts the default back
 * after the handler (mcp/client.go:901), and a default that does not fit the field is sent as
 * written.
 */
export function receivedText(
  field: ElicitationField,
  value: FieldValue | undefined,
  labels: BooleanLabels,
): string | undefined {
  if (value !== undefined && hasValue(value)) return display(field, value, labels);
  if (field.required || field.default === undefined) return undefined;
  const fallback = initialValue(field);
  return fallback === undefined ? JSON.stringify(field.default) : display(field, fallback, labels);
}

/** The receipt's lines: each field the server receives, as the operator saw it. */
export function summaryOf(
  fields: readonly ElicitationField[],
  values: FieldValues,
  labels: BooleanLabels,
): ReceiptLine[] {
  return fields.flatMap((field) => {
    const value = receivedText(field, values[field.name], labels);
    return value === undefined ? [] : [{ label: fieldTitle(field), value }];
  });
}

function display(field: ElicitationField, value: FieldValue, labels: BooleanLabels): string {
  if (typeof value === 'boolean') return value ? labels.yes : labels.no;
  if (typeof value === 'number') return String(value);
  const ids = typeof value === 'string' ? [value] : value;
  if (field.kind !== 'enum') return ids.join(', ');
  const options = optionsFor(field, labels);
  return ids.map((id) => options.find((option) => option.id === id)?.label ?? id).join(', ');
}
```

Create `web/src/questions/useCountdown.ts`:

```ts
import { useEffect, useState } from 'react';

/** Seconds left until an RFC 3339 deadline, re-read every second. Never negative. */
export function useCountdown(deadline: string): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => {
      setNow(Date.now());
    }, 1000);
    return () => {
      window.clearInterval(timer);
    };
  }, []);
  return secondsLeft(deadline, now);
}

export function secondsLeft(deadline: string, now: number): number {
  const at = Date.parse(deadline);
  return Number.isNaN(at) ? 0 : Math.max(0, Math.ceil((at - now) / 1000));
}

/** The countdown's face, m:ss. */
export function formatRemaining(seconds: number): string {
  return `${String(Math.floor(seconds / 60))}:${String(seconds % 60).padStart(2, '0')}`;
}
```

Create `web/src/questions/FieldInput.tsx`:

```tsx
import { useTranslation } from 'react-i18next';
import { ariaInvalid } from '../a11y/aria';
import type { ElicitationField, ElicitationFormat } from '../chat/sseAdapter_elicitation';
import type { FieldValue } from './elicitationSteps';
import { Input } from '@/components/ui/input';

// FieldInput is a string or number step of an MCP form: an input typed for the field's format,
// or a numeric one with the field's bounds. Enter goes on to the next step.

const INPUT_TYPE: Record<ElicitationFormat, string> = {
  email: 'email',
  uri: 'url',
  date: 'date',
  'date-time': 'datetime-local',
};

export interface FieldInputProps {
  readonly field: ElicitationField;
  readonly labelledBy: string;
  readonly describedBy?: string;
  readonly value: FieldValue | undefined;
  readonly invalid: boolean;
  readonly disabled: boolean;
  readonly onChange: (value: FieldValue | undefined) => void;
  readonly onEnter: () => void;
}

export function FieldInput({
  field,
  labelledBy,
  describedBy,
  value,
  invalid,
  disabled,
  onChange,
  onEnter,
}: FieldInputProps) {
  const { t } = useTranslation();
  const numeric = field.kind === 'number' || field.kind === 'integer';
  const type = numeric ? 'number' : field.format === undefined ? 'text' : INPUT_TYPE[field.format];
  return (
    <Input
      type={type}
      aria-labelledby={labelledBy}
      {...(describedBy !== undefined ? { 'aria-describedby': describedBy } : {})}
      aria-invalid={ariaInvalid(invalid)}
      value={typeof value === 'string' || typeof value === 'number' ? value : ''}
      disabled={disabled}
      placeholder={t('questionCard.placeholder')}
      {...(numeric ? { step: field.kind === 'integer' ? 1 : 'any' } : {})}
      {...(field.format === 'date-time' ? { step: 1 } : {})}
      {...(field.min !== undefined ? { min: field.min } : {})}
      {...(field.max !== undefined ? { max: field.max } : {})}
      {...(field.min_length !== undefined ? { minLength: field.min_length } : {})}
      {...(field.max_length !== undefined ? { maxLength: field.max_length } : {})}
      onChange={(event) => {
        const raw = event.target.value;
        if (raw === '') onChange(undefined);
        else onChange(numeric ? Number(raw) : raw);
      }}
      onKeyDown={(event) => {
        if (event.key !== 'Enter') return;
        event.preventDefault();
        onEnter();
      }}
      className="bg-surface text-sm"
    />
  );
}
```

Create `web/src/questions/ElicitationHeader.tsx`:

```tsx
import { useTranslation } from 'react-i18next';
import { Clock3, Server } from 'lucide-react';
import type { ElicitationQuestion } from '../chat/sseAdapter_elicitation';
import { formatRemaining, useCountdown } from './useCountdown';
import { Badge } from '@/components/ui/badge';

// ElicitationHeader says who is asking. The chip carries the name Aura mounted the server
// under, never one the server gave itself. The countdown is Aura's own bound: a server's
// request timeout can end the form sooner, and the card then shows it cancelled.

export interface ElicitationHeaderProps {
  readonly question: ElicitationQuestion;
  readonly countdown: boolean;
}

export function ElicitationHeader({ question, countdown }: ElicitationHeaderProps) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-wrap items-center gap-2 text-xs text-text-muted">
      <Badge variant="secondary" className="gap-1">
        <Server aria-hidden="true" className="size-3.5" />
        <span className="sr-only">
          {t('questionCard.form.server', { server: question.server })}
        </span>
        <span aria-hidden="true">{question.server}</span>
      </Badge>
      {question.tool !== undefined ? <span className="font-mono">{question.tool}</span> : null}
      {countdown ? <Countdown deadline={question.deadline} /> : null}
    </div>
  );
}

function Countdown({ deadline }: { readonly deadline: string }) {
  const { t } = useTranslation();
  const time = formatRemaining(useCountdown(deadline));
  return (
    <span
      role="timer"
      aria-label={t('questionCard.form.expiresIn', { time })}
      className="ms-auto inline-flex items-center gap-1 tabular-nums"
    >
      <Clock3 aria-hidden="true" className="size-3.5" />
      {time}
    </span>
  );
}
```

Create `web/src/questions/ElicitationReview.tsx`:

```tsx
import { useTranslation } from 'react-i18next';
import type { ElicitationField } from '../chat/sseAdapter_elicitation';
import { fieldTitle, receivedText, type BooleanLabels, type FieldValues } from './elicitationSteps';

// ElicitationReview is an MCP form's last step: every field with what the server will receive,
// each row leading back to its step. The MCP spec has clients let the operator review and
// change answers before sending (2025-11-25 elicitation.mdx:40-45).

export interface ElicitationReviewProps {
  readonly fields: readonly ElicitationField[];
  readonly values: FieldValues;
  readonly labels: BooleanLabels;
  readonly disabled: boolean;
  readonly onEdit: (step: number) => void;
}

export function ElicitationReview({
  fields,
  values,
  labels,
  disabled,
  onEdit,
}: ElicitationReviewProps) {
  const { t } = useTranslation();
  return (
    <ul className="flex flex-col divide-y divide-border-strong/40">
      {fields.map((field, index) => {
        const title = fieldTitle(field);
        const shown = receivedText(field, values[field.name], labels);
        return (
          <li key={field.name}>
            <button
              type="button"
              disabled={disabled}
              onClick={() => {
                onEdit(index);
              }}
              className="flex w-full flex-col gap-0.5 rounded-lg px-1 py-2 text-left text-sm outline-none hover:bg-accent/5 focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
            >
              <span className="sr-only">{t('questionCard.reviewStep.edit', { field: title })}</span>
              <span aria-hidden="true" className="text-text-muted">
                {title}
              </span>
              <span className="font-medium break-words whitespace-pre-wrap [overflow-wrap:anywhere]">
                {shown ?? t('questionCard.reviewStep.notGiven')}
              </span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}
```

Create `web/src/questions/ElicitationCard.tsx`:

```tsx
import { useId, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { ChevronLeft } from 'lucide-react';
import type { ElicitationAction } from '../chat/sseAdapter_elicitation';
import { CancelControl } from './CancelControl';
import { ElicitationHeader } from './ElicitationHeader';
import { ElicitationReview } from './ElicitationReview';
import { FieldInput } from './FieldInput';
import { QuestionCard, type QuestionCardProps } from './QuestionCard';
import { QuestionOptions } from './QuestionOptions';
import { QuestionReceipt, type ReceiptTone } from './QuestionReceipt';
import { postElicitationAnswer, PROBLEM_REQUIRED } from './elicitationApi';
import {
  contentFrom,
  fieldTitle,
  firstFailingStep,
  hasValue,
  initialValues,
  itemsHint,
  optionsFor,
  selectedIds,
  summaryOf,
  toggleValue,
  withinItemBounds,
  type FieldValue,
  type FieldValues,
} from './elicitationSteps';
import type { ElicitationItem, ElicitationOutcome } from './useThreadElicitations';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';

// ElicitationCard is a mounted MCP server's form drawn as Question Flow (spec 2026-09-25): one
// step per field with Back and Next, then a Review step that alone submits, and Decline and
// Cancel always in the footer. The server's message is the description on every step. The
// answer goes to the run's route; how the question closed arrives on the stream, so the
// receipt shows the server's state rather than the card's guess.

interface Receipt {
  readonly tone: ReceiptTone;
  readonly key: string;
}

const RECEIPTS: Record<ElicitationOutcome, Receipt> = {
  accepted: { tone: 'success', key: 'questionCard.receipt.answered' },
  declined: { tone: 'neutral', key: 'questionCard.receipt.declined' },
  cancelled: { tone: 'neutral', key: 'questionCard.receipt.cancelled' },
  expired: { tone: 'neutral', key: 'questionCard.receipt.expired' },
};

type Problem = 'failed' | 'closed' | 'invalid';

const PROBLEM_KEYS: Record<Problem, string> = {
  failed: 'questionCard.error.failed',
  closed: 'questionCard.error.closed',
  invalid: 'questionCard.error.invalid',
};

export interface ElicitationCardProps {
  readonly item: ElicitationItem;
  readonly isStreaming?: boolean;
}

export function ElicitationCard({ item, isStreaming }: ElicitationCardProps) {
  const { t } = useTranslation();
  const baseId = useId();
  const { question, outcome } = item;
  const { fields } = question;
  const [step, setStep] = useState(0);
  const [values, setValues] = useState<FieldValues>(() => initialValues(fields));
  const [errors, setErrors] = useState<Readonly<Record<string, string>>>({});
  const [busy, setBusy] = useState(false);
  const [sent, setSent] = useState<ElicitationAction | null>(null);
  const [problem, setProblem] = useState<Problem | null>(null);
  const titleId = `${baseId}-title`;
  const labels = { yes: t('questionCard.yes'), no: t('questionCard.no') };
  const settled = outcome !== undefined || question.refusal !== undefined;

  function frame(children: ReactNode, extra: Partial<QuestionCardProps> = {}) {
    return (
      <QuestionCard
        titleId={titleId}
        title={t('questionCard.form.title', { server: question.server })}
        header={<ElicitationHeader question={question} countdown={!settled} />}
        {...(question.message !== ''
          ? { description: question.message, descriptionId: `${baseId}-message` }
          : {})}
        dataAttributes={{ 'data-elicitation-id': question.id }}
        {...extra}
      >
        {children}
      </QuestionCard>
    );
  }

  if (question.refusal !== undefined) {
    const label = t(`questionCard.refusal.${question.refusal}`);
    return frame(<QuestionReceipt tone="neutral" label={label} />);
  }
  if (outcome !== undefined) {
    const receipt = RECEIPTS[outcome];
    const summary =
      outcome === 'accepted' && sent === 'accept' ? summaryOf(fields, values, labels) : [];
    return frame(
      <QuestionReceipt tone={receipt.tone} label={t(receipt.key)} announce summary={summary} />,
    );
  }

  // A form of more than one field ends on Review, and only Review submits.
  const reviewStep = fields.length > 1 ? fields.length : null;
  const reviewing = step === reviewStep;
  const submits = reviewing || reviewStep === null;
  const field = reviewing ? undefined : fields[step];
  const locked = busy || sent !== null;
  const value = field === undefined ? undefined : values[field.name];
  const error = field === undefined ? undefined : errors[field.name];
  const canGoOn =
    field === undefined || ((!field.required || hasValue(value)) && withinItemBounds(field, value));
  const hint = field === undefined ? null : itemsHint(field);
  const ids = {
    hint: `${baseId}-hint`,
    items: `${baseId}-items`,
    error: `${baseId}-error`,
  };
  const describedBy = [
    field?.description !== undefined ? ids.hint : '',
    hint !== null ? ids.items : '',
    error !== undefined ? ids.error : '',
  ]
    .filter((id) => id !== '')
    .join(' ');
  const described = describedBy === '' ? {} : { describedBy };

  async function send(action: ElicitationAction, current: FieldValues = values) {
    setBusy(true);
    setProblem(null);
    try {
      const result = await postElicitationAnswer(
        question.run_id,
        question.id,
        action === 'accept' ? { action, content: contentFrom(fields, current) } : { action },
        crypto.randomUUID(),
      );
      if (result.kind === 'delivered') {
        setSent(action);
      } else if (result.kind === 'invalid') {
        setErrors(result.errors);
        setStep(firstFailingStep(fields, result.errors));
        if (result.errors[''] !== undefined) setProblem('invalid');
      } else {
        setProblem('closed');
      }
    } catch {
      setProblem('failed');
    } finally {
      setBusy(false);
    }
  }

  function advance(current: FieldValues = values) {
    if (submits) void send('accept', current);
    else setStep(step + 1);
  }

  function update(name: string, next: FieldValue | undefined) {
    setValues((current) => ({ ...current, [name]: next }));
    setErrors((current) =>
      Object.fromEntries(Object.entries(current).filter(([key]) => key !== name)),
    );
  }

  // Skip leaves the field out. On a field with a default the button says Use default: go-sdk
  // puts the default back on every accept (mcp/client.go:901), so leaving it out sends it.
  function skip() {
    if (field === undefined) return;
    const cleared = { ...values, [field.name]: undefined };
    setValues(cleared);
    advance(cleared);
  }

  const next = () => {
    if (canGoOn && !locked) advance();
  };

  let body: ReactNode = null;
  if (reviewing) {
    body = (
      <ElicitationReview
        fields={fields}
        values={values}
        labels={labels}
        disabled={locked}
        onEdit={setStep}
      />
    );
  } else if (field?.kind === 'enum' || field?.kind === 'boolean') {
    body = (
      <QuestionOptions
        key={field.name}
        labelledBy={titleId}
        {...described}
        options={optionsFor(field, labels)}
        mode={field.kind === 'enum' && field.multi === true ? 'multi' : 'single'}
        selected={selectedIds(value)}
        disabled={locked}
        onToggle={(id) => {
          update(field.name, toggleValue(field, value, id));
        }}
        onSubmit={next}
      />
    );
  } else if (field !== undefined) {
    body = (
      <FieldInput
        key={field.name}
        field={field}
        labelledBy={titleId}
        {...described}
        value={value}
        invalid={error !== undefined}
        disabled={locked}
        onChange={(changed) => {
          update(field.name, changed);
        }}
        onEnter={next}
      />
    );
  }

  let title: string | undefined;
  if (reviewing) title = t('questionCard.reviewStep.title');
  else if (field !== undefined) title = fieldTitle(field);
  const primary = submits ? 'submit' : step === fields.length - 1 ? 'review' : 'next';
  const footer = (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          variant="ghost"
          disabled={locked}
          onClick={() => void send('decline')}
          className="text-[0.8125rem] text-text-muted hover:text-text"
        >
          {t('questionCard.decline')}
        </Button>
        <CancelControl
          isStreaming={isStreaming}
          disabled={locked}
          labels={{
            cancel: t('questionCard.cancel.label'),
            confirm: t('questionCard.cancel.confirm'),
            yes: t('questionCard.cancel.yes'),
            no: t('questionCard.cancel.no'),
          }}
          onCancel={() => void send('cancel')}
        />
      </div>
      <div className="flex items-center gap-2">
        {step > 0 ? (
          <Button
            type="button"
            variant="ghost"
            disabled={locked}
            onClick={() => {
              setStep(step - 1);
            }}
            className="gap-1 rounded-full text-text-muted"
          >
            <ChevronLeft aria-hidden="true" className="size-4" />
            {t('questionCard.back')}
          </Button>
        ) : null}
        {field !== undefined && !field.required ? (
          <Button
            type="button"
            variant="ghost"
            disabled={locked}
            onClick={skip}
            className="rounded-full text-text-muted"
          >
            {t(field.default !== undefined ? 'questionCard.useDefault' : 'questionCard.skip')}
          </Button>
        ) : null}
        <Button type="button" disabled={locked || !canGoOn} onClick={next} className="rounded-full">
          {t(`questionCard.${primary}`)}
        </Button>
      </div>
    </>
  );

  const status =
    problem === null ? null : (
      <Alert
        role="status"
        aria-live="polite"
        data-tone="danger"
        variant="destructive"
        className="bg-surface"
      >
        <AlertDescription>{t(PROBLEM_KEYS[problem])}</AlertDescription>
      </Alert>
    );

  return frame(
    <>
      {body}
      {field?.description !== undefined ? (
        <p id={ids.hint} className="text-xs text-text-muted">
          {field.description}
        </p>
      ) : null}
      {hint !== null ? (
        <p id={ids.items} className="text-xs text-text-muted">
          {t(`questionCard.choose.${hint.key}`, hint.params)}
        </p>
      ) : null}
      {error !== undefined ? (
        <p id={ids.error} className="text-[0.8125rem] text-danger">
          {t(
            error === PROBLEM_REQUIRED
              ? 'questionCard.error.required'
              : 'questionCard.error.invalid',
          )}
        </p>
      ) : null}
    </>,
    {
      ...(title !== undefined ? { title } : {}),
      step: {
        current: step + 1,
        total: Math.max(fields.length + (reviewStep === null ? 0 : 1), 1),
      },
      footer,
      ...(status !== null ? { status } : {}),
    },
  );
}
```

In `web/src/approvals/ThreadApprovalCards.tsx`:

```diff
--- a/web/src/approvals/ThreadApprovalCards.tsx
+++ b/web/src/approvals/ThreadApprovalCards.tsx
@@ -1,5 +1,7 @@
 import { useState } from 'react';
 import { useTranslation } from 'react-i18next';
+import { ElicitationCard } from '../questions/ElicitationCard';
+import type { ElicitationItem } from '../questions/useThreadElicitations';
 import { InlineApprovalCard } from './InlineApprovalCard';
 import type { Approval } from './useApprovals';
 import type { ApprovalResolution, ApprovalResolutionAttempt } from './useThreadApprovals';
@@ -12,6 +14,9 @@
 export interface ThreadApprovalCardsProps {
   /** Already-filtered active-thread rows in deterministic backend order. */
   readonly approvals: readonly Approval[];
+  /** A mounted MCP server's forms for this thread, in arrival order. useThreadElicitations
+   *  drops them with their run, so each one here is still worth showing. */
+  readonly elicitations?: readonly ElicitationItem[];
   readonly isStreaming?: boolean;
   readonly onResolutionStarted?: (attempt: ApprovalResolutionAttempt) => void;
   readonly onResolutionFailed?: (attempt: ApprovalResolutionAttempt) => void | Promise<void>;
@@ -20,6 +25,7 @@
 
 export function ThreadApprovalCards({
   approvals,
+  elicitations = [],
   isStreaming,
   onResolutionStarted,
   onResolutionFailed,
@@ -27,6 +33,7 @@
 }: ThreadApprovalCardsProps) {
   const { t } = useTranslation();
   const [announcement, setAnnouncement] = useState<Announcement>({ id: 0, text: '' });
+  const streaming = isStreaming !== undefined ? { isStreaming } : {};
 
   function handleResolved(resolution: ApprovalResolution) {
     const key =
@@ -42,18 +49,25 @@
   return (
     <div
       data-testid="thread-approvals"
-      className={approvals.length > 0 ? 'flex flex-col gap-2 px-3 pb-2 sm:px-4' : undefined}
+      className={
+        approvals.length + elicitations.length > 0
+          ? 'flex flex-col gap-2 px-3 pb-2 sm:px-4'
+          : undefined
+      }
     >
       {approvals.map((approval) => (
         <InlineApprovalCard
           key={approval.token}
           approval={approval}
-          {...(isStreaming !== undefined ? { isStreaming } : {})}
+          {...streaming}
           {...(onResolutionStarted !== undefined ? { onResolutionStarted } : {})}
           {...(onResolutionFailed !== undefined ? { onResolutionFailed } : {})}
           onResolved={handleResolved}
         />
       ))}
+      {elicitations.map((item) => (
+        <ElicitationCard key={item.question.id} item={item} {...streaming} />
+      ))}
       <p
         key={announcement.id}
         role="status"
```

In `web/stryker.config.json`:

```diff
--- a/web/stryker.config.json
+++ b/web/stryker.config.json
@@ -26,6 +26,15 @@
     "src/questions/QuestionOptions.tsx",
     "src/questions/QuestionReceipt.tsx",
     "src/questions/CancelControl.tsx",
+    "src/chat/sseAdapter_elicitation.ts",
+    "src/questions/useThreadElicitations.ts",
+    "src/questions/elicitationApi.ts",
+    "src/questions/elicitationSteps.ts",
+    "src/questions/useCountdown.ts",
+    "src/questions/FieldInput.tsx",
+    "src/questions/ElicitationHeader.tsx",
+    "src/questions/ElicitationReview.tsx",
+    "src/questions/ElicitationCard.tsx",
     "src/onboarding/onboardingApi.ts",
     "src/onboarding/onboardingWizardModel.ts",
     "src/chat/share/RevokeConfirmDialog.tsx",
```

In `web/vitest.stryker.config.ts`:

```diff
--- a/web/vitest.stryker.config.ts
+++ b/web/vitest.stryker.config.ts
@@ -8,6 +8,14 @@
   'src/approvals/__tests__/approvalState.test.ts',
   // The question frame (spec 2026-09-25): the ask_user adapter's suites above reach it too.
   'src/questions/__tests__/QuestionFrame.test.tsx',
+  // A mounted server's form (spec 2026-09-25): the pump signal, the thread's fold, the route
+  // client, the step logic, the countdown and the card.
+  'src/chat/sseAdapter.onElicitation.test.ts',
+  'src/questions/__tests__/useThreadElicitations.test.ts',
+  'src/questions/__tests__/elicitationApi.test.ts',
+  'src/questions/__tests__/elicitationSteps.test.ts',
+  'src/questions/__tests__/useCountdown.test.ts',
+  'src/questions/__tests__/ElicitationCard.test.tsx',
   'src/chat/artifacts/artifactMeta.test.ts',
   'src/chat/artifacts/downloadAll.test.ts',
   'src/chat/voice/speechAdapter.test.ts',
```

- [ ] **Step 4: Run the checks.** Web, one command at a time:
  - `npx prettier --write src/chat src/questions src/approvals stryker.config.json vitest.stryker.config.ts`, then the same paths with `--check`. Expected: `All matched files use Prettier code style!`
  - `npx vitest run src/chat src/questions src/approvals src/i18n src/__tests__/readabilityTokens.test.ts`, in the background: it takes about ten minutes in WSL. Measured on a copy of HEAD with Tasks 7 and 8 applied: 1353 tests, of which 1351 pass. The new `FieldInput` test, added after that run, makes it 1354.
    - The two that fail are WSL-only failures that HEAD shows as well (Task 9 Step 5): `AttachmentChip`'s image preview and `LocalArtifactDisplay`'s inline image. Any other failure is this task's.
    - The existing `sseAdapter.onSteer.test.ts` and `sseResume` suites stay green unchanged.
  - `npm run typecheck`. Expected: exit 0 and no output.
  - `npm run lint`. Expected: `Found 0 warnings and 0 errors.`
  - `npm run dup`. Expected: `Found 0 clones.`
  - `npm run deadcode`. Expected: knip prints nothing.
  - The new files' own coverage. The web gate is global (85% on statements, branches, functions and lines), and the spec asks 85% of `QuestionCard`, the two adapters and `sseAdapter_elicitation`. Web: `npx vitest run src/questions src/approvals src/chat/sseAdapter.onElicitation.test.ts --coverage --coverage.reportOnFailure=true --coverage.reporter=text --coverage.include='src/questions/**' --coverage.include=src/chat/sseAdapter_elicitation.ts --coverage.include=src/approvals/InlineApprovalCard.tsx`. Expected: `Tests  159 passed (159)`. Measured, as statements / branches:
    - `QuestionCard.tsx`, `QuestionReceipt.tsx`, `ElicitationHeader.tsx`, `ElicitationReview.tsx`, `useCountdown.ts`: 100 / 100;
    - `FieldInput.tsx`: 100 / 100;
    - `ElicitationCard.tsx`: 98.85 / 98.05;
    - `InlineApprovalCard.tsx`: 96.66 / 93.33;
    - `elicitationSteps.ts`: 100 / 98.16;
    - `useThreadElicitations.ts`: 100 / 95.83;
    - `elicitationApi.ts`: 96.42 / 94.73;
    - `sseAdapter_elicitation.ts`: 95.74 / 97.05;
    - `CancelControl.tsx`: 94.73 / 87.5;
    - `QuestionOptions.tsx`: 90.62 / 92.85.
  - Check the sizes: `wc -l src/chat/ExternalStoreChat.tsx src/chat/sseAdapter.ts src/chat/sseResume.ts src/questions/*.ts src/questions/*.tsx src/questions/__tests__/*`. Every file must be under 600 lines. Measured: `ExternalStoreChat.tsx` 594, `sseAdapter.ts` 565, `sseResume.ts` 450, `ElicitationCard.test.tsx` 413, `ElicitationCard.tsx` 336, `elicitationSteps.ts` 190.
  - Stryker runs in CI only.

- [ ] **Step 5: Commit.**

```bash
cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH" LEFTHOOK_BIN="$HOME/go/bin/lefthook"
git add web/src/chat/sseAdapter_elicitation.ts web/src/chat/sseAdapter.onElicitation.test.ts web/src/questions/
git -c core.hooksPath=.git/hooks commit -F - -- web/src/chat/sseAdapter_elicitation.ts web/src/chat/sseAdapter.onElicitation.test.ts web/src/chat/sseAdapter.ts web/src/chat/sseResume.ts web/src/chat/ExternalStoreChat.tsx web/src/chat/ExternalStoreChat_liveRun.ts web/src/chat/ExternalStoreChat_streams.ts web/src/questions/ web/src/approvals/ThreadApprovalCards.tsx web/src/approvals/__tests__/ThreadApprovalCards.test.tsx web/stryker.config.json web/vitest.stryker.config.ts <<'EOF'
feat(cockpit): answer a mounted server's form in the thread

aura.elicitation and aura.elicitation_resolved are read from the pump,
like aura.steer. The live stream and the reattach pump both fire them,
and they are never reduced into a message part.

A reload brings an open form back from the replay, or, once the ring
has rotated past it, from the run's own list of open forms. The thread
holds a form once, drops it with its run, and settles it once.

ElicitationCard draws the form as Question Flow:
- one step per field, required fields first, then a Review step that
  alone submits, as the MCP spec asks of clients;
- Back and Next, Skip on an optional field, or Use default when the
  server gave one, since go-sdk sends the default either way;
- Decline and Cancel always in the footer; Cancel confirms while the
  run streams;
- a chip naming the server as Aura mounted it, the message as plain
  text on every step, and a countdown.

The fields:
- an enum is radio or checkbox rows, with the item bounds said;
- a boolean is Yes and No;
- strings and numbers are typed inputs with the server's bounds;
- defaults are prefilled, a date-time one in local time.

A 422 puts the card back on the failing field's step, with the kind of
problem there and never the value. The receipt follows the server's
resolution, and an expiry reads as one.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---
### Task 9: Gates, then push

**Files:**
- Modify: `scripts/critical_mutation_gate.py:18-25` (`GO_SCOPES`) and `:45-56` (`REQUIRED_SCOPE_IDS`).
- Modify: `scripts/critical_mutation_gate_test.py`, adding one test after `test_media_boundaries_are_scoped_on_files_that_exist` (51-61).
- Modify: `.github/workflows/ci.yml`, the `web-mutation` comments: a new paragraph after 1561, and the scope count at 1569-1570.
- Commit: `internal/webui/dist` (the embedded bundle), then the four Calm Prism baselines under `web/e2e/__screenshots__/chat-calm-prism.spec.ts/`.

**Why the mutation scopes are added here (adversarial M8).** The CI mutation job scores six fixed Go files (`critical_mutation_gate.py:18-25`), none of them new. Without a scope, the two files the whole feature rests on would ship with no mutation evidence, and Stryker scores the web as one aggregate. Both files exist once Tasks 1 and 4 have landed, and the existing test at 51-61 checks that every scoped file exists.

- [ ] **Step 1: Write the failing scope test.** In `scripts/critical_mutation_gate_test.py`, after `test_media_boundaries_are_scoped_on_files_that_exist`, add:

```python
    def test_elicitation_boundaries_are_scoped(self) -> None:
        # The held clock and the routing of a server's request to the run that asked: the two
        # files a form elicitation rests on (plan 2026-09-25, Tasks 1 and 4).
        self.assertEqual(
            critical_mutation_gate.GO_SCOPES["pausable"],
            "internal/pausable/context.go",
        )
        self.assertEqual(
            critical_mutation_gate.GO_SCOPES["elicitation_route"],
            "internal/agent/mcptools/elicitation_route.go",
        )
```

- [ ] **Step 2: Run it to verify it fails.** Go: `env PYTHONPATH=scripts python3 -m unittest scripts/critical_mutation_gate_test.py` (`aura_go.sh` runs any command from the tree root, and WSL's `python3` is the Linux one).

Expected (measured on a copy of the tree with Tasks 1-6 applied): `Ran 23 tests`, `FAILED (errors=1)`, the one error being `KeyError: 'pausable'` in `test_elicitation_boundaries_are_scoped`.

- [ ] **Step 3: Add the scopes.** In `scripts/critical_mutation_gate.py`, `GO_SCOPES` gains two entries after `media_watcher`:

```python
    "pausable": "internal/pausable/context.go",
    "elicitation_route": "internal/agent/mcptools/elicitation_route.go",
```

and `REQUIRED_SCOPE_IDS` gains the same two ids after `"media_watcher",`:

```python
        "pausable",
        "elicitation_route",
```

`test_required_ids_and_go_scopes_cannot_drift` holds the two lists together, and release readiness imports `REQUIRED_SCOPE_IDS` (`release_readiness_gate.py:13`), so a report without the new scopes is refused as missing them.

In `.github/workflows/ci.yml`:
- after the `UNRE-MEASURED (2026-09-16)` paragraph (1559-1561), add a comment paragraph:

```yaml
    #
    # UNRE-MEASURED (2026-09-25): the MCP form plan grows the mutate list from 47 files to 61
    # and adds the pausable and elicitation_route Go scopes. Each Go scope runs its package's
    # suite once per mutant: internal/agent/mcptools's takes 4.3s a run, where the six scored
    # before take 0.005-0.15s (go test -count=1, WSL). Time the first run under this budget.
```

- at 1569, `The eight independently-scored` becomes `The ten independently-scored`, and at 1570 `six Go files` becomes `eight Go files`.

Run the test again (Step 2's command). Expected: `Ran 23 tests` and `OK`.

- [ ] **Step 4: Commit.**

```bash
cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH" LEFTHOOK_BIN="$HOME/go/bin/lefthook"
git -c core.hooksPath=.git/hooks commit -F - -- scripts/critical_mutation_gate.py scripts/critical_mutation_gate_test.py .github/workflows/ci.yml <<'EOF'
ci(mutation): score the paused clock and the elicitation routing

The mutation job scored six fixed Go files, none of the two the MCP
form rests on: internal/pausable/context.go, the deadline a hold can
stop, and mcptools/elicitation_route.go, which decides which run is
asked. Both are now boundaries of their own, held to 70% killed like
the others, and release readiness requires them by the shared list.

Their runtime under the job is not measured yet. mcptools's suite takes
4.3 s a run against at most 0.15 s for the six scored before, and it
runs once per mutant, so the job comment says to time the first run.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

- [ ] **Step 5: The pre-push gate.**
  - Check `git status` first: no other session may be mid-change in the tree. Gates run alone.
  - Go: `make quality`. Expected: `ok: quality gate passed (deadcode vet build file-size capability-declaration embedding-model-contract llm-model-contract lint test-race vuln)` (`Makefile:138`).
    - A `deadcode` finding on `summariseElicitationSchema`, `askOperatorBounded` or `elicitationPanicError` means a deletion was missed.
    - A `dupl` finding in non-test code means a helper was copied rather than shared.
  - Web, one command at a time: `npm run lint`, `npm run typecheck`, `npm run format:check`, `npm run dup`, `npm run deadcode`, `npm run test`.
    - `npm run lint` is `oxlint --type-aware --max-warnings=0 .`, so a warning fails it as an error does, and oxlint exits 0 either way. Read the summary line: it must say `Found 0 warnings and 0 errors.`
    - `npm run test` fails below 85% on statements, branches, functions or lines, and that is the gate.
    - Four suites fail in WSL at HEAD already. This was measured 2026-09-25 on an untouched HEAD copy, while CI run 36113809843 at `fb070f6de` is green, so they are the WSL environment. CI's `Web unit tests` job is their gate:
      - `videoflow_fonts.test.ts` does not load: `googlefonts.json needs an import attribute of "type: json"`;
      - `AttachmentChip.test.tsx`'s image preview throws `Cannot read properties of undefined (reading '_buffer')` in vitest's `makeCompatBlob`;
      - `LocalArtifactDisplay.test.tsx`'s inline image test cannot find the image;
      - `AppShell.shell.test.tsx` times out at 5000 ms under load.
    - Any other failure is this plan's. In a full run of the plan's code (2026-09-25, 344 files), the only other failure was `readabilityTokens.test.ts`. The fix, `text-accent-text`, is now in Task 7.
    - Coverage is not reported while a test fails (`reportOnFailure` is off), so in WSL `npm run test` cannot show the 85% figure. CI shows it.

- [ ] **Step 6: The coverage gate.** Bring the stack up if it is down: `make db-migrate memory-up`. Then Go: `env -u AURA_WEB_AUTH_SECRET bash scripts/coverage_docker.sh`.
  - The variable is dropped because `.env` leaks it into the config tests.
  - `env -u` rather than `bash -c '…; …'`: `wsl` hands its arguments to a shell, and a quoted `;` may not survive the trip.
  - The script provisions and drops only the disposable `aura_cov` database.

  Expected, in this order:
  - `ok: <scope> coverage <covered>/<total> (<pct>% displayed) >= 85%` (`scripts/coverage_profile_gate.sh:61`);
  - `ok: package-local coverage policy passed` (`scripts/coverage_package_gate.py:223`).

  Every package this plan touches is a `target` entry, held to 85%: the new `internal/pausable` and `internal/elicit` (Tasks 1 and 3), and `internal/agent`, `internal/agent/mcptools` and `internal/agui` (`coverage_package_policy.json:5,7,14`). If one falls under, write daemon-free tests for the uncovered lines. Never lower a floor.

- [ ] **Step 7: The embedded bundle.** The tree commits the cockpit build (`internal/webui/dist`, `.gitignore:18-22`; last done in `ad7701b41`).
  - The image rebuilds it anyway (`docker/aura/Dockerfile:19,62`). This step keeps a local `go build` in step with the source.
  - Web: `npm run build`.
  - Then commit it on its own:

```bash
cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH" LEFTHOOK_BIN="$HOME/go/bin/lefthook"
git add internal/webui/dist
git -c core.hooksPath=.git/hooks commit -F - -- internal/webui/dist <<'EOF'
build(web): embed the question card and the MCP form

Regenerates the committed cockpit bundle after the QuestionCard frame,
the ask_user redesign and the MCP elicitation card.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

- [ ] **Step 8: Push. ASK THE OPERATOR FIRST.** Tell them two things when you ask:
  - a push of `master` publishes the edge image, and the appliances install it;
  - so the appliances get this behaviour before Task 10's E2E has validated it (adversarial L10).

  With the go, push from WSL so the lefthook gates run on the pushed commit:

```bash
cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks push origin master
```

Expected: the lefthook banner, with every pre-push command green. No banner means no gate ran.

Then, in WSL, `gh run list --branch master --limit 10`. Every job must end green:
- the coverage job;
- the web jobs, including Playwright and `web-mutation`;
- the Go mutation boundaries inside `web-mutation`, the two new ones included. Read the `critical-mutation` artifact's `mutation-report.json`: every scope at 70% or above. A new scope under 70% gets an autopsy of its survivors before any test is added: an error-wrap removal is often near-equivalent;
- `Publish Aura edge image`.

Two things this CI run cannot show:
- **Screenshots.** Playwright runs with `--update-snapshots` while the Calm Prism harvest is on (`ci.yml:1829-1834`, TEMP), so a visual diff cannot fail it. Task 10 Step 6's screenshots are the visual check.
- **The mutation job's new runtime.** Record how long `web-mutation` took, next to the 90-minute budget (`ci.yml:1568`).

A red job is fixed, whether or not it looks related to this change.

- [ ] **Step 9: Commit the Calm Prism baselines.** The four baselines show the approval stack (`chat-calm-prism.spec.ts:40,358`), and Task 7 redrew it. They cannot be regenerated off the runner (`ci.yml:1830-1832`), so take them from this push's run. In WSL:

```bash
cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH" LEFTHOOK_BIN="$HOME/go/bin/lefthook"
run_id=$(gh run list --branch master --workflow ci.yml --limit 1 --json databaseId --jq '.[0].databaseId')
gh run download "$run_id" --name calm-prism-snapshots --dir /tmp/calm-prism
ls /tmp/calm-prism
```

Expected: `calm-prism-desktop-dark.png`, `calm-prism-desktop-light.png`, `calm-prism-mobile-dark.png` and `calm-prism-mobile-light.png`. Open each one and look at the approval stack: the question frame, the option rows and an Answer pill, not the old buttons. Then copy them over the baselines and commit:

```bash
cp /tmp/calm-prism/*.png web/e2e/__screenshots__/chat-calm-prism.spec.ts/
git -c core.hooksPath=.git/hooks commit -F - -- web/e2e/__screenshots__/chat-calm-prism.spec.ts/ <<EOF
test(web): take the Calm Prism baselines of the redrawn approval stack

Task 7 draws ask_user in the question frame, so the four Calm Prism
screenshots of the approval stack changed. They are taken from CI run
${run_id}, the only renderer the baselines are compared on.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

The heredoc is unquoted on purpose, so the shell writes the run id into the body. Push it with the operator's go, as in Step 8. The TEMP harvest itself stays: reverting it belongs to its own playbook (`ci.yml:1829`).

---

### Task 10: E2E on the lab VM, then the PRD records it

The PRD amendment comes after the E2E and records it (CLAUDE.md, "misura, poi emenda"). That is why the brief's Task 10 (PRD §13 and docs) and Task 11 (E2E) are one task here, in that order.

Every step changes the VM only through its updater. Mailbox and chat access is read-only. The operator drives the cockpit and Telegram with their own account; you watch the backend.

The reference form is server-everything's `trigger-elicitation-request`:
- 13 fields, of which only `name` is required;
- eight defaults;
- two multi-choice enums bounded at 1 to 3 items;
- its own request timeout is 10 minutes, so Aura's 300 s bound ends an unanswered form first.

- [ ] **Step 1: Measure the "before".** On the VM (`192.168.101.158`, over the WSL sshpass script pattern), record:
  - the tool count of each MCP server mounted today (memory, calendar, WhatsApp), from the aura mount log lines;
  - the image id.

  Spec step 7 compares against these.

- [ ] **Step 2: Let the updater converge.** Check `sudo systemctl list-timers` for the updater's next run and wait for it. The update applies after about 15 minutes of idle. Do not run the updater by hand.
  - Confirm the running `aura` container uses the new image: its `Image` id must match the pulled tag's `Id`.
  - Re-read the tool counts from Step 1. Each server must mount with the same count as before (spec step 7): every mount now advertises elicitation, and none of the three contains elicitation code.
  - Ask the operator for one ordinary call to each of the three, and check that each still returns.

- [ ] **Step 3: Mount the reference server** (spec step 1). The operator installs it in the cockpit (Governance, MCP, install), as their own server, with command `npx` and args `-y @modelcontextprotocol/server-everything`.
  - This is the body `POST /api/governance/mcp` takes (`MCPInstallRequest`, `governance_write_seam.go:47`).
  - Watch the install verify initialize and tools/list.
  - server-everything registers its conditional tools after `initialized` and then sends `notifications/tools/list_changed` (adversarial L11). So wait for the refreshed tool list before reading the count.
  - In that list, `trigger-elicitation-request` must be present, and `trigger-url-elicitation` must be absent (spec step 5): server-everything registers it only for a client that advertises URL mode.
  - Record which protocol version it negotiates: `2025-11-25` means the classic path, `2026-07-28` means multi-round-trip. The run proves only the path it took.

- [ ] **Step 4: The form** (spec steps 2 and 3). The operator asks Aura to use `trigger-elicitation-request`. Watch, in the cockpit:
  - the header: the `everything` chip, the tool name, and the countdown ("Aura cancels in 4:59");
  - the server's message as the description, on every step;
  - "Step 1 of 14" on `name`, the one required field, first;
  - the defaults prefilled, and **Use default** instead of Skip on a defaulted field;
  - "Choose 1 to 3." on each multi-choice step, with Next grey outside the bounds;
  - the Review step last, listing every field with what the server will receive, and Submit only there.

  Then:
  - The operator reloads the page in the middle of the form. The card comes back with its fields. Record which source brought it back: the replay, or the run's list (the log shows the GET `/agent/runs/{runID}/elicitations`).
  - The operator waits more than 60 s of human time and then submits.
  - The server echoes every value in the tool result, the defaults included.
  - `aura.tool_invocations` shows that call ending `ok`, with a duration over 60 s.
  - The aura log shows `mcp elicitation resolved` with `action=accept` and the field count, and none of the values. Grep the log for one of the values typed; it must not be found.

- [ ] **Step 5: Decline and expire** (spec step 4).
  - The operator declines a second form. The card shows "Declined.", and the server reports the decline.
  - A third form is left alone for `AURA_MCP_ELICITATION_TIMEOUT_SEC` (300 s by default; the VM's configuration is not changed). The card shows "Expired: cancelled automatically.", and the server reports a cancel.

- [ ] **Step 6: ask_user, redrawn** (spec step 6). The operator triggers one ask_user of each shape, and takes a screenshot of each card and its receipt:
  - a choice: radio rows, with Answer grey until one is chosen;
  - a clarification: a text field and Answer;
  - an approval with the model's own options (for example Yes and No): rows and **Answer**, not Approve;
  - a gateway-gated mutation: the scope rows and **Approve**. If a destructive one is at hand, it shows the destructive variant.

- [ ] **Step 7: One turn outside the cockpit** (spec step 7). With `everything` still mounted, the operator asks Aura, from their own Telegram chat, to use `trigger-elicitation-request`. That run has no asker, so the request keeps today's decline-and-surface:
  - the server reports a decline;
  - the notice naming `everything` reaches the chat;
  - no card appears in the cockpit.

  The operator then makes one ordinary request on Telegram, which must answer as before.

- [ ] **Step 8: Clean up** (spec step 8). The operator unmounts `everything` in the cockpit. Confirm the tools are gone from the mount log and that the sidecar environment was removed. Delete any personal copy the test produced, with the operator watching.

- [ ] **Step 9: Score.** Score the run against the spec's E2E list, steps 1 to 8. A score of 9.8 or more closes it. Anything less goes back to the task that owns the gap. Record whether any `ambiguous_run` decline appeared in the log during the run; none is expected, since the operator runs one conversation at a time.

- [ ] **Step 10: Record it in the PRD.** In `prd.md` §13, line 654 starts the paragraph. Keep the paragraph up to "negative call timeouts cannot request unlimited execution." on line 656. Replace the rest, from "The production elicitation wiring …" (656) to "… approval row." (658), with:

```markdown
A held form stretches the call and run bounds: while the operator answers, the
call's clock and the run's stop. What bounds a held wait is the elicitation
timeout, and the detached run's outer cap (`AURA_AGUI_RUN_MAX_WALLCLOCK_SEC`,
3600 s).

A cockpit turn shows a mounted server's form elicitation in its thread. The call stays open,
with its clock and the run's paused, until the operator accepts, declines or cancels, or
`AURA_MCP_ELICITATION_TIMEOUT_SEC` passes.

Turns with no cockpit, and requests Aura cannot place in a single run, keep decline-and-surface.
URL mode stays refused.
```

  The first paragraph is new. It keeps "finite configured bounds" true, as a held form now stretches them.

  Follow it with a dated paragraph (2026-MM-DD, the day of the run) recording what was measured:
  - the protocol path server-everything took;
  - the human time the call outlived, against the 60 s call bound;
  - the reload, and whether the replay or the run's list brought the form back;
  - the decline, and the expiry as a cancel with its "expired" receipt;
  - `trigger-url-elicitation` absent from a form-only mount;
  - the unchanged tool counts of memory, calendar and WhatsApp, with one call each;
  - the Telegram turn's decline-and-surface;
  - the request ids used as evidence.

  It also says what the run does NOT prove:
  - **The run bound.** The run's budget wallclock is 300 s, and so is the elicitation timeout. A form expires before the run bound can bite, so only Task 6's integration test shows the run's clock held.
  - **A server's own request timeout.** The TypeScript SDK's default is 60 s. server-everything sets 10 minutes for this tool, so a server on the default would end a form after 60 s, and its card would read cancelled (adversarial M7).
  - **The pause is tree-wide.** A hold stops the wallclock of the whole run tree, not only the waiting call's. The run had no parallel sub-agent to show it (adversarial M2). The 3600 s cap bounds it.
  - **Human time now counts in the latency metrics.** `mcp_bridge` durations, `tool.execute` spans and `tool_invocations.duration_ms` include the operator's time (adversarial L9).
  - **Third-party servers.** Every mount now advertises elicitation. Aura's own three servers contain no elicitation code. A third-party server that respects the capability may now ask where it used to fall back.
  - **The URL-mode refusal.** The branch is not exercised: server-everything hides the tool from a form-only client. Its unit test covers it.
  - **The classic late-ask limit.** A classic request that arrives while one unrelated run has a call in flight on the session goes to that run (adversarial M1).
  - **Two conversations sharing one session.** Only the integration test covers it.
  - **The per-node timeout.** It is off on the VM.
  - **Forms at the caps.**

  The spec has no status line. Add one sentence after its first paragraph (`docs/superpowers/specs/2026-09-25-mcp-elicitation-question-card-design.md:3-8`): "Implemented on 2026-MM-DD (<the commit range>); measured on the lab VM, prd.md §13."

  Commit with explicit paths:

```bash
cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH" LEFTHOOK_BIN="$HOME/go/bin/lefthook"
git -c core.hooksPath=.git/hooks commit -F - -- prd.md docs/superpowers/specs/2026-09-25-mcp-elicitation-question-card-design.md <<'EOF'
docs(prd): record MCP form elicitation as measured on the lab VM

§13 no longer says decline-and-surface for everything. A cockpit turn
now shows a mounted server's form in its thread, with the call's and
the run's clocks paused, and the bounds paragraph says what still
bounds a held wait. The paragraph carries the VM run's measurements
and what that run does not prove.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

  Push after the operator's go, as in Task 9 Step 8.

---

## Open points

v1's Open points 1 (Tool UI), 3 (field order) and 4 (an expiry declines) are settled: the operator ruled option V and an expiry that cancels, and the spec now orders the required fields first. v1's point 11 (Git Bash as a fallback) is withdrawn: Git Bash's `node` is a Windows executable.

1. **The plan adds seven things the spec's types and routes do not list:**
   - `Question.Refusal`, so a refused form is still shown, resolved;
   - `Question.Schema` (`json:"-"`), the server's schema, resolved once and kept for `Validate`;
   - `Field.MinItems`, `Field.MaxItems` and `Field.Pattern` (`json:"-"`): the card holds Next outside the item bounds, and `Validate` checks the pattern with RE2. The browser never sees the pattern, because its `pattern` attribute is ECMAScript;
   - `run_id` in `aura.elicitation`, because the answer route is run-scoped and a card restored from a replay has nowhere else to read it;
   - `expired` in `aura.elicitation_resolved`: an expiry reaches the server as a cancel, like a call that ended, and only the card tells them apart;
   - `GET /agent/runs/{runID}/elicitations`, because a reload the ring can no longer replay would otherwise lose its form (adversarial H4).

   Each is additive. None carries an answer value.
2. **The per-node tool timeout is a third clock the spec does not name.** When `AURA_LOOP_NODE_TIMEOUT_SEC` is set (it is off by default), it would cut every held wait, so Task 2 makes it pausable.
3. **The detached run's outer cap stays fixed, and a hold is tree-wide.**
   - The cap is `AURA_AGUI_RUN_MAX_WALLCLOCK_SEC`, 3600 s. With every other clock held, it is the one bound on a server that asks again and again. It cuts a real run only after about 55 minutes of forms in one turn. Recorded in `detachedRunContext`'s comment (Task 6).
   - A hold stops the wallclock of the whole run tree, not only the waiting call's, so parallel branches run on while a form is open (adversarial M2). `Budget`'s comment (Task 2) and the PRD say so.
   - Per the ruling, there is no separate cap on held time. The 3600 s cap bounds it.
4. **Only `handleRunDetached` installs an asker.** The coordinator-wake detached runs (`server_coordinator_wake.go:47`) get none, so a form there is decline-and-surfaced. The spec names `handleRunDetached` alone.
5. **"The operator is told" reaches a cockpit operator only if their run has an asker.** A classic request with no call in flight goes to the fallback, and so does a request whose run has no cockpit. The fallback delivers on a channel, and a cockpit-only operator with no channel sees only the log. That is today's behaviour, unchanged.
6. **Routing step 1 is read as "the request's own marked context".** On the multi-round-trip path, the handler's context is the call's, or that of the `resources/read` of its links. So the plan routes on that call alone, asker or none. The in-flight registry is consulted only for a classic request. The spec's order would, for a call with no asker, look at other runs' calls on the same session. That could decline a request the fallback should take.
7. **A classic request can reach the wrong run in one case** (adversarial M1a).
   - The case: a server sends its request after its own call has returned, while one unrelated run has a call open on the shared session. The request goes to that run.
   - go-sdk v1.8.0 knows which POST a classic request came on and drops it (`mcp/streamable.go:2617-2680`, cross-source I6), so the in-flight registry is the best the SDK allows.
   - Identity-scoped mounts cannot mix identities, since their sessions are per identity. On a shared mount, a run is keyed by its asker and its identity (Task 4, `runKey`), so two identities on one session are refused, not mixed.
   - The PRD records the limit.
8. **An ambiguous classic request is declined, not serialized** (cross-source L7). Hermes and Archestra both serialize a server's calls instead. Aura does not, because a 300 s form would block every other run's calls to that mount. Task 10 Step 9 records whether an `ambiguous_run` decline was ever seen.
9. **Every mount advertises elicitation, as the spec says** (operator ruling; adversarial H5, cross-source M5).
   - Aura's own three servers (arcadedb-mcp, aura-pim-mcp, whatsapp-mcp) contain no elicitation code. Task 10 re-checks their tool counts and drives one turn outside the cockpit.
   - A third-party server that respects the capability may now ask where it used to fall back, and a Telegram, cron or `aura chat` run then declines. The PRD records the risk.
   - Considered and not taken: Archestra keeps capability-bearing connections apart, one per (agent, conversation) with an `:elicitation` suffix (`platform/backend/src/clients/mcp-client.ts:912-920`). It doubles every mount's sessions, and the operator did not ask for it.
10. **Needs the operator: a URL-mode request gets a decline, where MCP asks for `-32602`** (cross-source L3).
    - MCP 2025-11-25 says a request in a mode the client did not declare gets `-32602` (`client/elicitation.mdx:698`).
    - Only a non-compliant server can send one. Aura never advertises URL mode, and go-sdk's own server refuses to send it to such a client (`mcp/server.go:1753-1756`).
    - The spec's handler contract returns `(*ElicitResult, nil)` in every case (spec §`internal/agent/mcptools`, "Handler contract"), so the plan declines, as Hermes does.
    - Returning `&jsonrpc.Error{Code: jsonrpc.CodeInvalidParams}` on the classic path only needs the spec amended first. On the multi-round-trip path an error would fail the whole `CallTool` (`mrtr.go:289-291`), so the decline stays there either way.
11. **A cancel the server sends reads "Cancelled.", like any other end of the call** (cross-source L6). The spec's error table says so: "The call ends (server cancel, run cancel, tool timeout) → Cancel; the card shows 'cancelled'".
    - The countdown says "Aura cancels in …", Aura's own bound.
    - A distinct "The server stopped waiting" receipt would need the spec changed first, if the operator wants one.
12. **`ExternalStoreChat.tsx` reaches 594 of 600 lines** (adversarial L14). Spec 2 must split it before adding anything to it.
13. **The Calm Prism harvest stays on.** Task 9 commits the redrawn baselines from the push's run, but reverting the TEMP `--update-snapshots` (`ci.yml:1828-1834`) belongs to its own playbook. Until then, CI cannot fail on a visual diff.
14. **Two conventions are Aura's, not Tool UI's** (cross-source I3). Spec 2 inherits them:
    - Enter on a chosen single row submits, where Tool UI only toggles;
    - `QuestionReceipt` has four tones and an opt-in `announce`, where Tool UI turns the component itself into its receipt.
15. **The brief's facts, checked:**
    - `bridge_supervisor.go` is 500 lines, not 509.
    - It has a second `session.CallTool` site at line 339, the redial retry, and Task 4 covers both.
    - `buildRegistryWithMCP`'s `consent` became dead in 89688bd27, as the brief says. `aura tools` (`main.go:549`) and the one-shot pipe keep passing nil, deliberately.
16. **Task decomposition changed from the brief's eleven to ten, for two reasons:**
    - The web foundation, the ask_user card and the MCP card became two tasks. The frame lands with its first adapter, so no commit ships a component nothing uses.
    - The PRD amendment moved behind the E2E and merged with it. CLAUDE.md says to measure, then amend.

## Facts not verified (each is checked by the step that first depends on it)

v1's other entries were measured since, and are gone from this list:
- the jsonschema-go shapes, the classic streamable-HTTP path at 2025-11-25, and `runner.Deps`: the backend validator ran every Go block of Tasks 1-6 on a scratch copy;
- the one-tool deferral, now bypassed with `Adopt` (Task 6);
- the import order, now checked by lint (`Found 0 warnings and 0 errors.`);
- Tool UI's licence: `LICENSE.md` at `49a8702`, MIT, "Copyright (c) 2025 AgentbaseAI Inc.".

- Whether go-sdk's server answers `server/discover` from a `2025-11-25`-only server in a way that makes the client fall back to `initialize` at `2025-11-25`. Read in `client.go:314-386`; not run. Task 4's classic tests show it.
- Which protocol version `@modelcontextprotocol/server-everything` negotiates, and whether the governance npx resolver installs it. It depends on `@modelcontextprotocol/sdk ^1.30.0`, whose version was not read (cross-source I7). Task 10 Step 3 shows both.
- The mutation job's runtime with two more Go scopes and 61 Stryker files instead of 47. Task 9 Step 8 records it against the 90-minute budget.
- What the redrawn approval stack looks like on the CI runner. Only the `calm-prism-snapshots` artifact shows it (Task 9 Step 9).
- `e2e/mcp-cockpit-live.spec.ts` after Task 7's edit. It runs only with `AURA_E2E_REAL_AGENT=1` against a live stack. No step of this plan runs it.
- Why four web suites fail in WSL at HEAD (`videoflow_fonts`, `AttachmentChip`, `LocalArtifactDisplay`, `AppShell.shell`) while CI is green on the same commit. They are not diagnosed; Task 9 Step 5 lists them and says how to read the run.
- The whole web suite's coverage with this plan applied. In WSL, those four failures stop vitest from reporting it, so CI's `Web unit tests` job shows the figure. Task 8 Step 4 measures the new files on their own.

## Self-review

**Spec coverage.** Each spec section maps to a task:

| Spec section | Task |
|---|---|
| Decisions | 4, 6, 7, 8 |
| `internal/elicit`: types, seam, `FromSchema` (caps, order, one resolve), `Validate` (problem codes) | 3 |
| mcptools: in-flight registry, routing order, the bound, URL mode, handler contract, wiring | 4 and 5 |
| The paused clock: `pausable`, the MCP call, the run, the handler's hold | 1, 2 and 4 |
| agui: the asker, `publish`, the route with its codes, the open-questions list, the events without values | 6 |
| Cockpit: registry, `QuestionCard`, the ask_user shapes, the MCP form with its Review step, Use default and item bounds, inputs, header, footer, receipts, a 422 back to the failing step, placement, streaming file, keyboard and accessibility, en and it | 7 and 8 |
| Errors table: an expiry cancels, 422 codes, RE2 patterns refused | 3 (codes, patterns), 4 (routing, URL, caps, expiry), 6 (409, 410, 422, parallel questions), 8 (card states) |
| Security: plain text, the server's name from Aura, no values in logs or in the replay store, an owner-scoped route, no form outliving its run | 3, 4, 6 and 8 |
| Testing: unit, integration (classic and MRTR through the real detached handler, a shortened bound), web vitest and Stryker in CI, E2E steps 1-8 | 1-8, 9 and 10 |
| PRD | 10 |
| No new env vars, no migration | nothing added in any task |

Gaps found and fixed while writing v2:
- The two `ExternalStoreChat` approval suites and the live MCP spec clicked option buttons too. Running the web suites showed it, and Task 7 now changes them. The same run showed that the review line now appears only on a pending approval.
- `approvalState.test.ts` was missing from Stryker's suite list although `approvalState.ts` is mutated. Task 7 adds it.
- Every commit block ran in Git Bash, which means `git.exe` and lefthook's Windows binaries, against the plan's own rule. They now run in WSL, with the hooks.
- v1's reply-less approval dropped the free-text field. The approvals ruling keeps it, so `chat.spec.ts` stays untouched.
- `useThreadElicitations` pruned in an effect, which the React Compiler lint refuses. It now adjusts while rendering.
- A fixture set `max_items: undefined`, which `exactOptionalPropertyTypes` refuses. The fixture now leaves the bound out.
- The full web suite showed `FrameIcon`'s `text-accent` failing the readability gate. It is now `text-accent-text`, and Tasks 7 and 8 run that gate in their own checks.
- Per-file coverage showed `FieldInput` at 84.6% of statements: nothing drove a `number` step, a date-time step, length bounds or a cleared input. One `ElicitationCard` test now does, and it measures 100%.

**Placeholder scan.** No "TBD", "similar to Task N" or step without code.
- The only date left open is the PRD paragraph's `2026-MM-DD`, which is the day of the run and cannot be known now.
- Each "read X before choosing" sentence names the file and the decision it settles.

**Type consistency.** Checked across tasks:
- **Go:**
  - `elicit.Question`, `Field`, `Answer`, `Asker`, the `Action*`, `Refusal*`, `Kind*` and `Problem*` constants, `MaxQuestionBytes`, `MaxOpenQuestions`, `ErrExpired`, `FieldErrors`, `DecodeSchema`, `FromSchema` and `Validate` (Task 3) are used unchanged in Tasks 4 and 6.
  - `ElicitationConsent.AskOperator(ctx, elicit.Question)` (Task 4) is the one `cmd/aura` implements (Task 4) and hands to mounts (Task 5).
  - `pausable.WithDeadline`, `WithTimeout`, `Hold` and `NewClock` (Task 1) are what Tasks 2 and 4 call.
- **Wire:** `elicitationFrame` and `elicitationResolvedFrame` produce exactly the JSON keys `sseAdapter_elicitation.ts` parses (`run_id`, `enum_titles`, `min_length`, `max_length`, `min_items`, `max_items`, `expired`, `refusal`). `fields: null` on a refusal is handled. The GET list's entries parse with the same `elicitationQuestionOf`.
- **Web:**
  - `QuestionCardProps`, `QuestionOption`, `ReceiptTone`, `ReceiptLine` and `CancelLabels` (Task 7) are what Task 8 imports.
  - `ElicitationAction` and `isStringList` are defined once, in `sseAdapter_elicitation.ts`. `elicitationApi.ts`, `elicitationSteps.ts` and `ElicitationCard.tsx` import them.
  - `PROBLEM_REQUIRED` in `elicitationApi.ts` is `elicit.ProblemRequired`'s value, `"required"`.

**Review Focus.** Each of the five has a test in its owning task:
1. `TestBudgetWallclockSkipsHeldTime`, `TestRunToolNodeTimeoutStopsWhileHeld`, `TestAHeldCallOutlivesItsTimeout`, `TestDetachedRunAnswersAnMCPFormWhileBothClocksStop`;
2. `TestAnUnansweredQuestionExpiresWhileTheCallIsHeld`;
3. `TestClassicElicitationWithTwoRunsInFlightAsksNeither`;
4. the replay counts in the integration test, `TestAFormTheRingRotatedPastIsStillListed`, and `applyElicitationSignal`'s replay test;
5. `TestAnAnswerThatFailsTheSchemaLeavesTheQuestionOpen`, `TestARefusedAnswerLeavesNoValueInTheReplayStore`, and `ElicitationCard`'s "a 422 puts the card back on the failing step, with the error there".
