# Plan validation 1 of 3 (web): citations against HEAD

- Plan: `docs/superpowers/plans/2026-09-25-mcp-elicitation-question-card.md`
- Scope:
  - Global Constraints, Review Focus and File map (plan lines 29-158);
  - Task 7 and Task 8 (lines 4205-7070);
  - Task 6 (lines 3221-4204), read only for the wire and route contract the web consumes.
- Checked against: `HEAD = 1b92bfcbc5638f8a3f04d4921f3743be62815e53`. The plan names `914317182`, which is now `HEAD~1`.
- Toolchain: go-sdk v1.8.0 and the AG-UI Go SDK in the WSL module cache; `lucide-react` 1.47.0 installed.
- Date: 2026-09-25. Read-only; nothing in the tree was modified.

## Verdict: YELLOW

- The cockpit code in Tasks 7 and 8 would typecheck against the real components, types and helpers.
- Every file:line citation that matters points at the right code. Two are off by a few lines, and the text around each still identifies the target.
- The wire contract with Task 6 lines up: JSON keys, `fields: null` on a refusal, the route's status codes, and the Idempotency-Key.
- Neither the Go nor the web imports form a cycle.
- One High finding would turn CI red after the push: the mutation job would find none of the new test suites.
- The two Medium findings are a lint error and stale visual baselines.
- Fix these before Task 7 starts. None of them changes the design.

| Severity | Count |
|---|---|
| Critical | 0 |
| High | 1 |
| Medium | 2 |
| Low | 11 |

---

## High

### H1. The Stryker vitest config never learns about the new test suites (Task 7 Step 3, Task 8 Step 3)

**Evidence.**
- Task 7 adds five files to `web/stryker.config.json`'s `mutate`: four `src/questions/*.tsx` files and `InlineApprovalCard.tsx`. Task 8 adds eight more.
- Stryker runs vitest through `web/stryker.config.json` → `"vitest": { "configFile": "vitest.stryker.config.ts" }`. That config has an explicit allow-list, `const mutationTests = [...]`, and `include: [...mutationTests]`.
- None of the seven new test files is in that list:
  - `src/questions/__tests__/QuestionFrame.test.tsx`
  - `src/chat/sseAdapter.onElicitation.test.ts`
  - `src/questions/__tests__/{elicitationSteps,useThreadElicitations,elicitationAnswer,useCountdown}.test.ts`
  - `src/questions/__tests__/ElicitationCard.test.tsx`
- The file's own comment records what that costs (`web/vitest.stryker.config.ts`):
  > `// stryker.config.json mutates these modules, so their suites must run here too: missing from this list, all 313 of their mutants scored NoCoverage and pulled the whole run to 69.37% (CI 2026-09-24) although every one of them has a test.`
- The settings that make it fail:
  - `stryker.config.json` has `"thresholds": { "high": 85, "low": 70, "break": 70 }`.
  - CI `web-mutation` (`ci.yml:1541-1603`) runs `npm run mutation` and then `critical_mutation_gate.py`, whose `frontend` scope is the whole `mutate` list.

**What goes wrong.**
- None of Task 8's eight files is reached by any test in the allow-list. `sseAdapter_elicitation.ts`, `useThreadElicitations.ts`, `elicitationAnswer.ts`, `elicitationSteps.ts` and `useCountdown.ts` score entirely NoCoverage.
- The existing `ThreadApprovalCards.test.tsx` never passes `elicitations`, so `ElicitationCard`, `ElicitationHeader` and `FieldInput` are never rendered either.
- Task 7's `QuestionOptions` and `QuestionCard` are reached only through `InlineApprovalCard.test.tsx`. That suite never renders the multi-step `StepBar`, and never presses Home, End or ArrowUp, or uses multi-mode rows.
- The plan runs mutation "in CI only", so this shows up only after the push, as a red `web-mutation` job.

**Fix.**
- Task 7: add `'src/questions/__tests__/QuestionFrame.test.tsx'` to `mutationTests` in `web/vitest.stryker.config.ts`.
- Task 8: add the six new suites listed above.
- Add `web/vitest.stryker.config.ts` to both tasks' **Files** lists and to both `git commit -F - -- …` path lists.

---

## Medium

### M1. `Array<T>` fails the `typescript/array-type` lint (Task 7 Step 3, `QuestionOptions.tsx`, plan line 4849)

**Evidence.**
- The plan writes: `const rows = useRef<Array<HTMLButtonElement | null>>([]);`
- `web/.oxlintrc.json` sets `"typescript/array-type": "error"` with no options. The default is `array`, which forbids `Array<T>`.
- Nothing in `web/src` uses `Array<`.
- The same ref already exists in the repo in the accepted form, `web/src/chat/workers/WorkerPicker.tsx:23`:
  > `const refs = useRef<(HTMLButtonElement | null)[]>([]);`

**Fix.** Write `useRef<(HTMLButtonElement | null)[]>([])`.

### M2. The Calm Prism screenshot baselines are not covered (Task 7 Step 1, Playwright part)

**Evidence.**
- `web/e2e/chat-calm-prism.spec.ts:358` asserts `toHaveScreenshot(\`calm-prism-${visual.name}.png\`)` for four cases.
- `openMatrix(page, visual.theme)` defaults to `includeApprovals = true` (`chat-calm-prism.spec.ts:41`). `revealCalmPrismMatrix` waits for the approval card (`calmPrismFixture.ts:303-305`).
- So `web/e2e/__screenshots__/chat-calm-prism.spec.ts/calm-prism-{desktop,mobile}-{dark,light}.png` all show the approval stack. Task 7 redraws it: new frame, radio rows, Approve pill, receipts.
- CI stays green today only because `ci.yml:1834` runs `npm run test:e2e -- --update-snapshots`. That step is marked `TEMP-harvest (revert after committing the PNGs)`.
- The plan lists the Playwright changes as complete in "Gaps found and fixed while writing". It does not mention the baselines.

**Fix.** Add a step to Task 7, or to Task 9 after the push:
1. Download the `calm-prism-snapshots` artifact from the CI run.
2. Commit the four regenerated PNGs. They cannot be regenerated off the runner; the `ci.yml` comment says so.

Alternatively, record in Open points that the baselines are knowingly left stale while the TEMP harvest remains.

---

## Low

### L1. Line citations that are off (Task 7 Files; Task 8 Step 3)

| Plan says | HEAD | Mark |
|---|---|---|
| `web/e2e/chat.spec.ts:472-482` ("from the step-2 comment through `await answer.click();`") | the step-2 comment is at **473** (`// 2) The ask_user interrupt renders an inline approval card IN-thread (D-03): the`) and `await answer.click();` is at **485** | WRONG → 473-485 |
| `sseAdapter.ts` CUSTOM-branch comment "at 305-308" / "at 305" | `303: // aura.steer (amendment #132, STEER-03) is deliberately NOT handled here: it falls` through 306 | WRONG → 303-306 |
| `StreamPostOptions` at 423-434 | opens at 423; its `}` is at 435 | CONFIRMED (off by one) |
| `AttachRunOptions` at 53-71 | opens at 53; its `}` is at 70 | CONFIRMED (off by one) |

Each step also names its target by content, so an executor would still land in the right place.

### L2. `prettier --check` fails on the code as written (Task 7 Step 4, Task 8 Step 4)

- `web/.prettierrc` sets `"printWidth": 100`. Several plan lines exceed it:
  - `export function FieldInput({ field, labelledBy, … }: FieldInputProps) {` (6635)
  - `export function summaryOf(fields: …, labels: BooleanLabels): ReceiptLine[] {` (6557)
  - the `content[field.name] = … ? new Date(value).toISOString() : value;` line (6510)
  - test lines 4303, 5552, 5706 and 6096, among others
- **Fix:** run `npx prettier --write` on the new and touched files before the `--check`.

### L3. The lint check reads only errors, but warnings fail the gate too (Global Constraints line 64; Task 7 Step 4; Task 8 Step 4)

- `npm run lint` is `oxlint --type-aware --max-warnings=0 .`.
- Several relevant rules are warn-level: `import-order/order`, `react/only-export-components`, `react/exhaustive-deps` and `import/no-duplicates`.
- **Fix:** require "Found 0 warnings and 0 errors".

### L4. Test counts that do not add up (Task 7 Step 1 and commit body)

- "replace the first test (89-97) and … (123-143) with the four tests below, and add the rest". The block holds **six** tests: two replacements and four additions.
- The commit body says "Six tests change". It then lists 2 (InlineApprovalCard) + 3 (ThreadApprovalCards sites) + 2 (Playwright specs), which is 7.
- **Fix:** reword to "the first two tests below replace 89-97 and 123-143; the other four go after the last test", and fix the commit body's count.

### L5. Review Focus 5 names a test that does not exist (plan line 128)

- It says: `ElicitationCard` "a 422 returns to the failing step".
- The test is `it('a 422 puts the card back on the failing step, with the error there', …)` (plan line 5972). Self-review line 7313 has the right name.

### L6. `isStringList` is written twice (Task 8)

- `sseAdapter_elicitation.ts` (plan line 6181) returns `boolean`.
- `elicitationSteps.ts` (plan line 6469) is the same body as a type guard.
- CLAUDE.md, REUSABLE CODE: export the type-guard form from `sseAdapter_elicitation.ts` and import it in `elicitationSteps.ts`.

### L7. The Global Constraints size table is slightly off (plan lines 67-74)

- `web/src/i18n/resources.ts`: 580 lines; the plan adds **3** (one import, two spreads), not 4.
- `web/src/chat/sseAdapter.ts`: 554 lines; the plan adds about **10-11**, not 9:
  - import: 1
  - `StreamRunOptions`: 3
  - `StreamPostOptions`: 1
  - `StreamSSEOptions`: 1
  - pump: 2
  - `streamPost` and `streamRun`: 2
  - the reworded comment: possibly 1
- Both stay well under 600. Only the numbers need correcting.
- The HEAD noted at line 33 (`914317182`) is now `HEAD~1`; HEAD is `1b92bfcbc`.

### L8. A spec deviation that Open points does not record (Task 8, `ElicitationCard` and `ElicitationHeader`)

- The spec (§Cockpit, "The MCP form header") says: "The server's message is the description, rendered as plain text".
- The plan renders the message in the `header` slot and uses each **field's** description as the `QuestionCard` description.
- Each step's `title` also overrides the form title, so "A form from {{server}}" (`questionCard.form.title`) appears only on receipts and refusals, never while the form is open.
- It is still plain text, so security is unaffected. Record it in Open points, or align it with the spec.

### L9. A stale comment in `chat.spec.ts` (Task 7)

- Line 14 still reads `// inline approval card in-thread -> resolve it (Answer) -> the run resumes`.
- After the change, the fixture presses **Approve**. Update it on touch (CLAUDE.md: comments updated in the same commit).

### L10. A date-time default never shows in its input (Task 8, `initialValue` and `FieldInput`)

- A server `default` for a `date-time` field arrives as RFC 3339, for example `2026-09-25T10:30:00Z`.
- `<input type="datetime-local">` blanks any value that is not a local `YYYY-MM-DDTHH:mm[:ss]`.
- So the input renders empty while the state keeps the default, and the default is still submitted.
- **Fix:** convert the default to a local datetime string in `initialValue` for `format === 'date-time'`, or accept the gap as a known limitation.

### L11. A missing type import in `ExternalStoreChat_streams.ts` (Task 8 Step 3)

- The `_liveRun.ts` bullet says to import `type ElicitationSignal`. The `_streams.ts` bullet only says "add the same field to `StreamFoldDeps`".
- Without `import type { ElicitationSignal } from './sseAdapter_elicitation';`, `npm run typecheck` fails.
- **Fix:** add the import line to that bullet.

---

## Citation ledger

All citations below were checked against `HEAD 1b92bfcbc`.

### Global Constraints and File map

| Claim | Real line / evidence | Mark |
|---|---|---|
| `ExternalStoreChat.tsx` 587 lines | `wc -l` → 587 | CONFIRMED |
| `internal/agent/budget_test.go` 589 | 589 | CONFIRMED |
| `web/src/i18n/resources.ts` 580 | 580 | CONFIRMED (adds 3, see L7) |
| `web/src/chat/sseAdapter.ts` 554 | 554 | CONFIRMED (adds about 10, see L7) |
| `internal/agui/server.go` 539 | 539 | CONFIRMED |
| `internal/agent/mcptools/bridge_supervisor.go` 500 | 500 | CONFIRMED |
| `model-selector.tsx` exemptions (option P) | `.oxlintrc.json:23`, `knip.json:7`, `.prettierignore:13`, `vitest.config.ts:38`, `scripts/check-file-size.sh:43` `grep -v -E '^web/src/components/model-selector(.aui)?.tsx$'` | CONFIRMED |
| Go 1.27.1, go-sdk v1.8.0, jsonschema-go v0.4.3 | `go.mod:3 go 1.27.1`, `:30 go-sdk v1.8.0`, `:20 jsonschema-go v0.4.3`. In WSL `go version` → `go1.27.1` (via `GOTOOLCHAIN=auto` from `~/.local/bin/go` → 1.26.3) | CONFIRMED |
| React 19, react-i18next, vitest, Stryker | `react ^19.3.0`, `react-i18next ^17.0.13`, `vitest ^5.0.1`, `@stryker-mutator/core ^10.0.0` | CONFIRMED |
| WSL node 24 links in `~/.local/bin` | `node -> ~/.local/aura-toolchains/node24/bin/node` (npm, npx likewise) | CONFIRMED |
| `node_modules` has both rolldown bindings | `@rolldown/binding-linux-x64-gnu`, `binding-win32-x64-msvc`; also `@oxlint-tsgolint/{linux,win32}-x64`, `@typescript/typescript-{linux,win32}-x64` | CONFIRMED |
| oxlint exits 0 on errors; read `Found N errors` | script is `oxlint --type-aware --max-warnings=0 .` | CONFIRMED (warnings count too, see L3) |
| File map: existing files to modify | `budget.go`, `llm_agent_tool.go`, `cmd/aura/{elicitation_consent,main,mcp_tools,runtime_tool_handles}.go`, `internal/agui/{runsession,server_run_detach,idempotency_http}.go`, `mcptools/elicitation.go` all exist | CONFIRMED |
| File map: files to create | `internal/pausable`, `internal/elicit`, `bridge_inflight.go`, `elicitation_route.go`, `run_elicitation.go`, `server_run_elicitation.go`, `web/src/questions/` are absent | CONFIRMED (new) |
| Coverage policy entries (Tasks 1 and 3) | `scripts/coverage_package_policy.json:35-36` has `documents/filecard` then `embeddings`; `:60-61` has `packs` then `pgnumeric`. The new entries sort between each pair | CONFIRMED |

### Review Focus test names

| Test | Mark |
|---|---|
| `TestBudgetWallclockSkipsHeldTime`, `TestRunToolNodeTimeoutStopsWhileHeld`, `TestAHeldCallOutlivesItsTimeout`, `TestAnUnansweredQuestionExpiresWhileTheCallIsHeld`, `TestClassicElicitationWithTwoRunsInFlightAsksNeither`, `TestDetachedRunAnswersAnMCPFormWhileBothClocksStop`, `TestAnAnswerThatFailsTheSchemaLeavesTheQuestionOpen` | each defined exactly once in the plan: CONFIRMED |
| Task 8 "a 422 returns to the failing step" | WRONG (see L5) |
| `applyElicitationSignal` ignores a replayed question | CONFIRMED (`'holds a replayed question once, and its resolution still applies'`) |

### Task 6 contract, as the web consumes it

| Claim | Evidence | Mark |
|---|---|---|
| CUSTOM wire shape `{type, name, value}` | AG-UI Go SDK `custom_events.go:58-62`: `Name string \`json:"name"\``, `Value any \`json:"value,omitempty"\`` | CONFIRMED |
| Published frames are not rewritten | `server_project.go:126` `redactEvent` touches only `*events.RunErrorEvent` | CONFIRMED |
| `aura.elicitation` keys | `elicitationFrame{RunID \`json:"run_id"\`; elicit.Question}`. The Question tags are `id`, `server`, `tool,omitempty`, `message`, `fields`, `deadline`, `refusal,omitempty`. The Field tags include `enum_titles`, `min_length`, `max_length`, `multi` and `format` | CONFIRMED against `sseAdapter_elicitation.ts` |
| `fields: null` on a refusal | `Fields []Field \`json:"fields"\`` with no `omitempty`; the web maps `?? []` | CONFIRMED |
| `aura.elicitation_resolved` `{id, action, expired?}` | `Expired bool \`json:"expired,omitempty"\`` | CONFIRMED |
| 422 body `{"errors":{…}}`, and `""` for whole-answer errors | `FieldErrors{"": err.Error()}` in `elicit.Validate` | CONFIRMED (the web handles `''`) |
| The date-time the web sends is accepted | web sends `new Date(v).toISOString()`; Go parses with `time.Parse(time.RFC3339, s)`, which accepts fractional seconds | CONFIRMED |
| The route needs an Idempotency-Key in production | `idempotency_http.go:203-208` returns 400 without one, and wraps only when `s.operations != nil` (`server.go:502-507`). The web sends an explicit key, like `steerRun.ts:45-49` | CONFIRMED |
| `server.go:365`, the steer route | `mux.HandleFunc("POST /agent/runs/{runID}/steer", s.handleRunSteer)` | CONFIRMED |
| `/agent/run` runs detached | `server_run.go:102 s.handleRunDetached(...)` | CONFIRMED |
| No import cycle in the Task 6 e2e test | `go list -deps` of `agenttest`, `mcptools`, `tools`, `llm`, `mcp` and `runner`: none reaches `internal/agui` | CONFIRMED |

### Task 7

| Claim | Real line | Mark |
|---|---|---|
| `web/components.json` `registries` | `"registries": { "@assistant-ui": "https://r.assistant-ui.com/{name}.json" }` | CONFIRMED |
| `resources.ts` update import | `31: import { updateEn, updateIt } from './resources.update';` | CONFIRMED |
| `...updateEn,` at 196 | `196:       ...updateEn,` (at the root of `translation`) | CONFIRMED |
| `...updateIt,` at 468 | `468:       ...updateIt,` | CONFIRMED |
| The `resources.steer.ts` precedent | the header comment has the same split and parity note | CONFIRMED |
| i18n parity and usage gates | `__tests__/resources.parity.test.ts`; `resources.usage.test.ts` checks only static `t('…')` keys resolve, and does not reject unused keys | CONFIRMED (the Task 8 keys can land in Task 7) |
| No `questionCard` key collision | `grep questionCard web/src` → none | CONFIRMED |
| Existing copy the tests rely on | `approval.card.answered: 'Answered.'`, `declined: 'Declined.'`, `cancelled: 'Run cancelled.'`, `cancel: 'Cancel run'`, `confirmCancel: 'Stop this run?'`, `confirmCancelYes: 'Stop run'`, `confirmCancelNo: 'Keep running'`, `answer: 'Answer'`, `freeTextPlaceholder: 'Type your answer'`, `approval.scope.session: 'Approve {{subject}} for this conversation'`, `approval.terminal.expired: 'Expired — auto-resolved.'`, `approval.frame.approval: 'Approval required'` | CONFIRMED |
| `InlineApprovalCard.tsx` rewritten from line 1 to 371 | 371 lines | CONFIRMED |
| option buttons `:168-185` | `168: {options.length > 0 ? (` … `185: </div>` | CONFIRMED |
| inline Cancel confirmation `:231-275` | `231: {confirmingCancel ? (` … `275: )}` | CONFIRMED |
| `TerminalChip` `:333-371` | `333: type ChipTone = …` … `371: }` | CONFIRMED |
| `CancelControl` body `:95-108` | `95: useEffect(() => {` … `108: }` (`handleCancelConfirmationKeyDown`) | CONFIRMED |
| misplaced `cardStateFor` doc block `27-32` | `27: /**` `28: * cardStateFor maps the server's verdict…` … `32: */`, then `useScopeLabel`'s doc | CONFIRMED |
| `useScopeLabel`, `cardStateFor` bodies | lines 38-45 and 47-58 | CONFIRMED (the param type changes to `ApprovalOption`, which is exported at `approvalState.ts:18`) |
| `isTerminal` in `approvalState.ts` | `13: export function isTerminal(approval: Pick<Approval, 'terminal'>): boolean {` | CONFIRMED |
| `presentation.params.risk` typing | `useApprovals.ts`: `presentation?: ApprovalPresentation`, `params: Readonly<Record<string, string>>` | CONFIRMED (`isDestructiveApproval` compiles) |
| `scoring.Destructive` at `scoring.go:26` | `26: Destructive RiskTier = "destructive"`. It reaches `risk` through `gateway/approve.go:217 "tier": string(tier)` → `approvaltext/presentation.go:105,114` | CONFIRMED |
| `ask_user.go` rejects duplicate labels | `ask_user.go:188: if _, dup := seen[o.Label]; dup {` | CONFIRMED |
| `approvalQuestion(approval, t)` | `approvalQuestion.ts:13 export function approvalQuestion(approval: Approval, t: TFunction): string` | CONFIRMED |
| `ariaInvalid` | `a11y/aria.ts:3 export function ariaInvalid(invalid: boolean): AriaInvalid` | CONFIRMED |
| `Badge` tone variants | `badge.tsx`: `secondary`, `warning`, `danger`, `success` | CONFIRMED |
| `Button` ref, `variant` and `size` | `ButtonProps extends ComponentProps<'button'>`; variants `default`/`ghost`/`destructive` (`bg-destructive`); size `sm` | CONFIRMED |
| `Alert`, `AlertDescription`, `Label`, `Textarea`, `Input`, `cn` | present in `components/ui/*` and `lib/utils.ts:4` | CONFIRMED |
| lucide icons | `MessageSquareText`, `ShieldCheck`, `TriangleAlert`, `Check`, `ChevronLeft`, `Clock3`, `Server` in `lucide-react.d.ts` (1.47.0) | CONFIRMED |
| Test tests 89-97 | `89: it('renders the backend question VERBATIM + option buttons', () => {` … `97: });` | CONFIRMED |
| Test tests 123-143 | `123: it('Answer (option) resolves {action:"accept", content} → answered terminal chip', …` … `143: });` | CONFIRMED |
| test helpers `approval()`, `renderCard`, `calls`, `ApprovalResolution` | lines 10-17, 52-77, 80-84, 7 | CONFIRMED |
| `ThreadApprovalCards.test.tsx:132` | `fireEvent.click(screen.getByRole('button', { name: 'Yes' }));` | CONFIRMED |
| `:206` | `fireEvent.click(first(screen.getAllByRole('button', { name: 'Yes' })));` | CONFIRMED |
| `:213` | `fireEvent.click(screen.getByRole('button', { name: 'Yes' }));` (StatefulCards has dropped the first row by then) | CONFIRMED |
| `first`, `client`, `QueryClientProvider` | lines 44-48, 38-42, 4 | CONFIRMED |
| `approvalState.test.ts` import | `2: import { parseOptions, parseScopeChoice } from '../approvalState';` | CONFIRMED |
| `chat.spec.ts:302` `kind: 'approval'` | `302: kind: 'approval',` | CONFIRMED |
| `chat.spec.ts:472-482` | 473-485 | WRONG (L1) |
| `chat-calm-prism.spec.ts:232` | `232: await expect(page.getByRole('button', { name: 'Pilot workspace' })).toBeVisible();` | CONFIRMED |
| `calmPrismFixture.ts:119` | `119: options: ['Pilot workspace', 'All workspaces'],` | CONFIRMED |
| The calm-prism error card is unaffected | `ERROR_TOKEN` is `kind: 'input'` (`calmPrismFixture.ts:108-110`), so the text field and Answer stay | CONFIRMED |
| `ci.yml:1834` | `run: cd web && npm run test:e2e -- --update-snapshots` | CONFIRMED (see M2) |
| `stryker.config.json` `mutate` | present; `approvalState.ts` already listed | CONFIRMED (see H1 for the include list) |

### Task 8

| Claim | Real line | Mark |
|---|---|---|
| `sseAdapter.ts` imports 1-28 | lines 1-28 are the import block | CONFIRMED |
| `StreamRunOptions` 398-422 | `398: export interface StreamRunOptions {`; `418: readonly onSteer?: …` | CONFIRMED |
| `StreamPostOptions` 423-434 | `423: export interface StreamPostOptions {`; `433: readonly onSteer?` | CONFIRMED |
| `StreamSSEOptions` 442-448 | `442: interface StreamSSEOptions {`; `446: readonly onSteer?: ((notice: SteerNotice) => void) \| undefined;` | CONFIRMED |
| pump 465-476 | `465: for await (const { frame } of readSSEFrames(res.body)) {`; `472: const steer = steerNoticeValue(frame);` | CONFIRMED |
| `streamPost`/`streamRun` pass `onSteer: opts.onSteer,` | both present | CONFIRMED |
| CUSTOM-branch comment at 305-308 | 303-306 | WRONG (L1) |
| `AguiFrame` from `./sseAdapter_frames`; the barrel re-exports it | `sseAdapter_frames.ts` `CustomFrame {type:'CUSTOM'; name: string; value: unknown}`; `sseAdapter.ts: export type { AguiFrame, … } from './sseAdapter_frames';` | CONFIRMED |
| `steerNoticeValue` pattern | `sseAdapter.ts:141-144` | CONFIRMED |
| `sseResume.ts` `AttachRunOptions` 53-71 | `53: export interface AttachRunOptions {` … `70: }` | CONFIRMED |
| `EngineOptions` 73-85 | `73: interface EngineOptions {` … `85: }` | CONFIRMED |
| `makeEngine` at 112 | `112: onSteer: opts.onSteer,` | CONFIRMED |
| `pumpBody` at 179 | `179: if (steer !== null) eng.onSteer?.(steer);` | CONFIRMED |
| `ResilientRunOptions extends StreamRunOptions` | `40: export interface ResilientRunOptions extends StreamRunOptions {`; `368: const eng = makeEngine(state, opts);` | CONFIRMED |
| `attachRun` options used by the test | `threadId`, `runId`, `signal`, `onUpdate` required; `newId` optional | CONFIRMED |
| `_liveRun.ts:28-43` | `28: export interface LiveRunAttachArgs {` … `43: }` | CONFIRMED |
| `_liveRun.ts:45-55` | destructuring, ending `55: }: LiveRunAttachArgs): void {` | CONFIRMED |
| `_liveRun.ts:75-76` | `75: ...(onArtifact !== undefined ? { onArtifact } : {}),`, `76: ...(onSteer …)` | CONFIRMED |
| `_liveRun.ts:103-104` | `103: onSteer,` `104: activeRunIdRef,` (the `attachLiveRun` deps) | CONFIRMED |
| `_streams.ts:38` | `38: readonly onArtifact?: ((assetId: string \| undefined) => void) \| undefined;` | CONFIRMED |
| `_streams.ts:72` | `72: onArtifact,` (destructure) | CONFIRMED |
| `_streams.ts:118` | `118: ...(onArtifact !== undefined ? { onArtifact } : {}),` (`foldReRun`) | CONFIRMED |
| `_streams.ts:135-140` | the `foldReRun` dependency list | CONFIRMED |
| `_streams.ts:182` | `182: ...(onArtifact !== undefined ? { onArtifact } : {}),` (`foldResumeRun`) | CONFIRMED |
| `_streams.ts:186` | `186: [foldAppendedStream, onArtifact],` | CONFIRMED (the type import is missing, L11) |
| `ExternalStoreChat.tsx:139` | `139: const steer = useSteerSend({ threadId, liveRunId, activeRunIdRef, isRunning, setMessages });` | CONFIRMED |
| `:247` | `247: onSteer: steer.onFrame,` | CONFIRMED |
| `:296` | `296: steer,` (the `onSend` deps) | CONFIRMED |
| `:414-429` | `useStreamFolds({ … onArtifact, streamErrorText … })` | CONFIRMED |
| `:438-448` | `useLiveRunAttach({ … onSteer: steer.onFrame, })` | CONFIRMED |
| `:560-566` | `<ThreadApprovalCards approvals={threadApprovals.approvals} isStreaming={isRunning} … />` | CONFIRMED |
| the `useThreadApprovals` import | `19: import { useThreadApprovals } from '../approvals/useThreadApprovals';` | CONFIRMED |
| 587 → 594 after the 7 added lines | the new `useThreadElicitations` line is about 90 characters at a 2-space indent, so Prettier will not wrap it | CONFIRMED |
| `ThreadApprovalCards.tsx` props and container | `approvals`, `isStreaming`; `className={approvals.length > 0 ? … : undefined}` | CONFIRMED |
| `web/src/chat/http.ts:5` `errorDetail` | `5: export async function errorDetail(res: Response): Promise<string> {` (it returns the body text, so the 404 test's `'question not found'` holds) | CONFIRMED |
| `sseAdapter.onSteer.test.ts` shape | the same `sseResponse`, `RUN_STARTED` and `RUN_FINISHED` helpers | CONFIRMED |
| vitest thresholds 85/85/85/85 | `vitest.config.ts:44-49` | CONFIRMED |
| `npm run typecheck`, `lint`, `dup`, `deadcode`, `test` | `package.json` scripts present | CONFIRMED |
| jscpd threshold 0 | `web/.jscpd.json`: `"minTokens": 100, "threshold": 0`, tests ignored | CONFIRMED |
| knip reports no unused exports | `ignoreExportsUsedInFile: true`; `--exclude types,…`. Every new value export has a caller | CONFIRMED |
| `tsconfig` strictness | `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, `noUnusedLocals/Parameters`. Every code block was read against these | CONFIRMED |
| Web import cycles | `questions/*` import only `chat/sseAdapter_elicitation` (a leaf over `sseAdapter_frames`), `chat/http` (a leaf), `a11y/aria` and `@/components/ui/*`. `chat` and `approvals` import `questions`, never the reverse | CONFIRMED, no cycle |
| No test asserts the stream functions' exact options | `grep` finds no `toHaveBeenCalledWith` on `streamRunResilient`, `attachRun` or `streamPost` | CONFIRMED |

## Notes on the code blocks (read line by line)

- **InlineApprovalCard rewrite.** Every remaining assertion in `InlineApprovalCard.test.tsx` still holds against the new markup:
  - `data-slot="card"` is on the frame;
  - `whitespace-pre-wrap` is on the description `<p>`;
  - the terminal Badge still carries `role=status`;
  - `Run cancelled.` keeps the `danger` tone;
  - clarification keeps its textarea and Answer.
- **InlineApprovalCard, new tests.** They pass against the implementation. The keyboard test works because the listbox's handler reads the fresh `active` and `selected` after each `act`-wrapped `fireEvent`.
- **ElicitationCard tests.** All ten walkthroughs were traced against the implementation and pass:
  - step order and Skip;
  - `canGoOn`;
  - `toggleValue` on multi;
  - the 422 → step 0 → `aria-invalid` cleared on edit → resubmit;
  - the receipt summary gated on `sent === 'accept'`;
  - no buttons on a refusal;
  - the countdown under fake timers.
- **Timers and ids in the test environment.**
  - `useState(() => Date.now())` has a precedent that already passes `react/purity`: `durationFormat.ts:42`, `GenerationFrame.tsx:63`.
  - `crypto.randomUUID()` in handlers has precedent: `ExternalStoreChat_folds.ts:17`, `steerRun.ts:45`.
- **The `elicitationSignalValue` narrowing.** `value.fields ?? []` → `Array.isArray` → `.every(isField)` narrows to `ElicitationField[]`. The same pattern as `isSteerNotice` compiles and lints clean.

## Disclosure

- Two host binaries were run by mistake, both read-only:
  - `node -e` printed the installed `lucide-react` version. The check was then repeated with `grep`.
  - `python3 -c "print(1)"` did nothing else.
- Nothing was written or modified by either.
- Every other check was a file read, `grep`, or `go list` / `go version` in WSL.
