# Plan validation 1 of 3: backend citations against HEAD

- **Plan:** `docs/superpowers/plans/2026-09-25-mcp-elicitation-question-card.md`
- **Scope:**
  - Global Constraints, Review Focus and File map (lines 29-158);
  - Tasks 1-6 (lines 159-4204);
  - Tasks 9-10 (lines 7071-7213).
  - Tasks 7-8 (web) are out of scope.
- **Checked against:** HEAD `1b92bfcbc5638f8a3f04d4921f3743be62815e53`, Go 1.27.1, go-sdk v1.8.0 and jsonschema-go v0.4.3 from the WSL module cache.
- **Date:** 2026-09-25.

## Verdict: YELLOW

The plan is accurate. Almost every file:line citation and SDK claim checks out, and all its Go code compiles against the real types. One High finding stops the plan's own integration proof from passing as written. It has a two-line fix. The two Medium findings are a lint failure that blocks a commit and a missing test on the channel-operator fallback path.

| Severity | Count |
|---|---|
| Critical | 0 |
| High | 1 |
| Medium | 2 |
| Low | 13 |

### How this was measured, not just read

1. I extracted `git archive HEAD` into the session scratchpad. The repository was not touched.
2. I applied every Go code block from Tasks 1-6 to that copy word for word, with a script that asserts each anchor matches exactly once.
3. I ran the results in WSL:

| Check | Result |
|---|---|
| `go build ./...` | ok |
| `go vet ./cmd/... ./internal/...`, untagged and with `db_integration`, `arcadedb_integration`, `docker_integration` | ok, no findings |
| `go test -race` on `internal/pausable` | ok, **97.6%** coverage (goleak green) |
| `go test -race` on `internal/elicit` | ok, **96.3%** coverage (goleak green) |
| `go test -race` on all of `internal/agent` and `internal/runner` | ok. The 4 new Task 2 tests ran and passed. |
| `go test -race` on all of `internal/agent/mcptools` | ok. All 21 elicitation tests pass, both classic tests included, so the 2025-11-25 fallback works. |
| `go test -race` on all of `internal/agui` | **FAIL, only in `TestDetachedRunAnswersAnMCPFormWhileBothClocksStop`, both subtests (H1).** With the H1 fix the whole package is ok (30 s). |
| Coverage of the new agui functions | 90-100% each |
| `go test -race` on the `cmd/aura` subset (`Elicitation\|RenderElicitation\|CapPrompt\|MCPMountOptions\|…`) | ok. The 5 new tests ran. |
| `golangci-lint` 2.13.2 with the repo's `.golangci.yml` | 1 revive finding (M1) and gofmt findings (L1). Nothing else in mcptools, agui, agent or cmd/aura. |
| `deadcode -test` | no findings on new or changed code |
| `wc -l` on every touched file | all under 600. The largest are `cmd/aura/main.go` at 566 and `internal/agui/server.go` at 541. |
| Import cycles (`go list -deps`) | none. `internal/pausable` and `internal/elicit` are leaves (stdlib and jsonschema-go only), `mcptools` does not reach `agui`, and `runner` does not reach `agui`. |

---

## Critical

None.

## High

### H1. The e2e test fails in both modes as written

- **Where:** Task 6, Step 1, `server_run_elicitation_e2e_test.go`, `newRealFormRunner`.
- **What happens:** `TestDetachedRunAnswersAnMCPFormWhileBothClocksStop` fails in both subtests (`mrtr` and `classic`) at the assertion `strings.Contains(joinMessageContents(reqs[1].Messages), "hello Ada")`.
- **Evidence, measured on the scratch copy:**
  - The elicitation itself works. The log shows `INFO mcp elicitation resolved server=forms action=accept fields=1 reason=answered`, and the run ends with `RUN_FINISHED` and `aura.elicitation_resolved`.
  - The tool message the model received was spilled with an empty preview:

    ```
    {Role:tool Content:[output truncated: showing bytes 0-0 of 9; read more via read_tool_output(tool_call_id="call-1-…"…)] ToolCallID:call-1}
    ```
- **Cause:**
  - `newRealFormRunner` builds `runner.Deps` without `PreviewCap`. The field is `internal/runner/runner_deps.go:88` `PreviewCap int`, and `runner.go:407` passes it on as `PreviewCap: r.previewCap`, so it is 0.
  - The bridged MCP result goes through `tools.NewResult` (`bridge_call.go:95`). With a cap of 0, all 9 bytes of "hello Ada" are spilled.
  - The steer e2e test this helper copies never asserts on a tool result, which is why it never met this.
  - `RunDir` is unset too, so the sidecars land in `internal/agui/conversations/<uuid>/call-*.result`. That path is gitignored (`.gitignore:103`), but it is still the source tree.
- **Not affected:** the control test `TestTheShortWallclockCutsAnUnheldTool` is fine. `sleepyTool` returns `ToolResult{Preview: "slept"}` directly, not through `NewResult`. With a 10 s wallclock it fails as it should ("request 1 carries the tool's result"), so it can both pass and fail.
- **Fix:** in `newRealFormRunner`'s `runner.Deps` literal, add:

  ```go
  PreviewCap:      2048,
  RunDir:          t.TempDir(),
  ```

  With exactly this change the test passes under `-race` in both modes: `mrtr` 4.89 s, `classic` 3.21 s. That also proves that a classic elicitation over streamable HTTP at 2025-11-25 completes with a 1 s call bound, a 2 s run wallclock and a 3 s operator.

## Medium

### M1. The `Kind*` constants fail revive's `exported` rule, which blocks the Task 3 commit

- **Where:** Task 3, Step 3, `internal/elicit/elicit.go` (plan lines 1431-1437).
- **What happens:** the `const ( KindString Kind = "string" … )` block has no doc comment. `.golangci.yml` enables revive's `exported` rule.
- **Evidence:** measured with golangci-lint 2.13.2 and the repo's config:

  ```
  internal/elicit/elicit.go:20:2: exported: exported const KindString should have comment (or a comment on this block) or be unexported (revive)
  ```

- **Why it matters:** `lefthook.yml` runs golangci-lint at pre-commit on the staged packages, so the Task 3 commit (Step 5) is refused, and `--no-verify` is forbidden. Task 3 Step 4 runs only `vet` and `test`, so nothing in the plan catches it before the commit.
- **Fix:**
  - Put a block comment above the `const (`, for example `// The kinds a Field can take: exactly the restricted schema's types.`.
  - Add `golangci-lint run ./internal/elicit/... ./internal/pausable/...` to Task 1 Step 4 and Task 3 Step 4.

### M2. Nothing tests the fallback that keeps decline-and-surface working for a run with no asker

- **Where:** Task 4, Step 1 (`elicitation_route_test.go`) and `elicitation_route.go`, `route.fallbackContext`.
- **Evidence:** measured coverage on the scratch copy:

  | Function | Coverage | Branch never run |
  |---|---|---|
  | `fallbackContext` | 66.7% | `return r.calls[0]` |
  | `askRecovered` | 75.0% | the panic recovery |
  | `askRun` | 95.7% | "the run's asker failed" |

- **Why it matters:**
  - `return r.calls[0]` is how a run with no asker (a Telegram or cron run) hands the fallback the call's own context. That context is the one that carries the operator's identity, which `surfacingElicitationConsent.AskOperator` needs: `identityctx.IdentityID(ctx)` at `cmd/aura/elicitation_consent.go:98`.
  - No test sends a call from such a run through `srv.CallToolText`.
  - `TestElicitationReachesHandlerOverARealSession` calls `session.CallTool` directly, so it has no call marker and reaches the handler's own context instead.
  - Task 10 already says the VM run does not prove "a Telegram operator's decline-and-surface after this change". With no test either, the one path that keeps today's behaviour for channel operators is proved by nothing.
- **Fix:** add three tests to `elicitation_route_test.go`:
  1. A multi-round-trip mount, called through `srv.CallToolText` on a context that carries a marker value (or an `identityctx` identity) and no asker. A `fakeConsent` records the context it was given. Assert that the consent saw the marker, and that the server got a decline.
  2. The same through the classic path, with one no-asker call in flight.
  3. An `elicit.Asker` that panics. Assert a decline and `reason="the run's asker failed"`.

## Low

### L1. Two Task 3 test files are not gofmt-clean

- **Where:** Task 3, Step 1.
- **What:**
  - `elicit_test.go:10`: the one-line `nopAsker.Ask` is longer than gofmt's limit for a single-line function body, so gofmt splits it.
  - `schema_test.go:128`: the map-literal keys are aligned one column short. The longest key, `"a format outside the set"`, sets the column.
- **Impact:** lefthook's `gofmt` has `stage_fixed: true`, so a commit reformats them automatically.
- **Fix:** paste the gofmt'd form into the plan so it matches what gets committed.

### L2. The Task 2 RED list leaves out a test that also fails

- **Where:** Task 2, Step 2.
- **What:** the Expected FAIL list omits `TestChildBudgetSeesTheParentsHeldTime`. Before the change it fails too, with "a sub-agent's budget refused a step (wallclock)".
- **Fix:** add it to the list.

### L3. Wrong line for `TestBudget_WithDeadline_PropagatesCancellation`

- **Where:** Task 2, Step 4.
- **What:** the plan says `budget_test.go:515`. The test starts at line **516**: `func TestBudget_WithDeadline_PropagatesCancellation(t *testing.T) {`.
- **Also measured:** the test stays green after the change. It builds a `Budget` literal with no clock, and `pausable.WithDeadline` gives a nil clock one of its own.

### L4. Task 4's expected cmd/aura build error is worded wrongly

- **Where:** Task 4, Step 2.
- **What:** the plan expects `undefined: elicit.Question`. `internal/elicit` already exists after Task 3, so the real error is a type mismatch, for example `cannot use elicit.Question{…} (value of struct type elicit.Question) as mcptools.ElicitationRequest value in argument to consent.AskOperator`.

### L5. A go-sdk line is off by one in a code comment

- **Where:** Task 3, the `validate.go` comment.
- **What:** it cites `go-sdk@v1.8.0 mcp/client.go:895-904`. The resolve is at **894** (`resolved, err := schema.Resolve(nil)`) and the validate at 898.
- **Fix:** write 894-904.

### L6. Task 5 gives two line ranges for the same `mount.go` comment

- **Where:** Task 5, Files and Step 3.
- **What:** the Files list says the doc comment is "at 28-31". Step 3 replaces "lines 26-31". 26-31 is the right range for the quoted text: line 26 is `// carries the per-mount choices. cmd/aura's boot mounts …`.
- **Fix:** make the Files list say 26-31.

### L7. Task 6's note about deferral names the wrong rule, and the test depends on a process-wide budget

- **Where:** Task 6, Step 4, the troubleshooting note.
- **What the plan says:** "the one-tool mount came up deferred … read `managedBridgePolicy` and its three-tool slot rule".
- **The real mechanism:**
  - `grantLoadedSlot` (`bridge_deferral.go:97`) grants `maxAlwaysLoadedMCPSlots = 2` slots **per process**, to servers exposing up to `maxAlwaysLoadedMCPTools = 4` tools each.
  - The e2e mounts twice, once for `mrtr` and once for `classic`, so it spends both slots of the agui test binary.
  - No other agui test mounts MCP today, so at `-count=1` it passes (measured).
  - Under `-count=2`, or once another agui test mounts a server, the tool is deferred and the fake model's call returns `tool_not_loaded`.
- **Fix:** use the repo's own pattern from `internal/runner/runner_memory_capture_live_test.go:250-252`: `if tool.Spec().Deferred { registry.Adopt(...) }`, with a local wrapper that clears `Deferred`. Or at least correct the note.

### L8. The PRD replacement starts in the middle of line 656

- **Where:** Task 10, Step 9.
- **What:** `prd.md:656` begins with "negative call timeouts cannot request unlimited execution." The sentence being replaced starts halfway through that line.
- **Fix:** tell the executor to keep the first half of line 656 and replace from "The production elicitation …" to "… approval row." (the end of line 658). The section is right: line 644 is `## 13. MCP integrations`.

### L9. Server-supplied text reaches the log without `redact.Line`

- **Where:** Task 4, `NewElicitationHandler` in the new `elicitation.go`.
- **What:**
  - The handler logs `"err", out.err`.
  - When a form is refused, `out.err` is `FromSchema`'s message, which quotes server-supplied strings: field names up to 256 B, enum values, format strings.
  - Today's handler passes every server-supplied value through `redact.Line`.
- **Scope:** the spec's rule, that the operator's values are never logged, still holds. This is only parity with today's hygiene.
- **Fix:** log `redact.Line(err.Error())` instead.

### L10. The AfterFunc comment promises more than Go delivers

- **Where:** Task 1, the "Why not a wrapper" rationale, the `AfterFunc` comment in `context.go`, and the commit body.
- **What they say:** children attach "without a goroutine per child".
- **What Go 1.27.1 does:**
  - `propagateCancel` checks `parent.(afterFuncer)` on the **immediate** parent only: `context.go:508` `if a, ok := parent.(afterFuncer); ok {`.
  - In production, every derivation passes through a `context.WithValue` layer first (`tools.WithToolCallContext`, `gateway.WithResponder`, `withCallTool`, otel spans), and those children take the one-goroutine path.
- **Impact:** the behaviour is correct and nothing leaks (goleak green). Only the claim is too broad.
- **Fix:** say "direct children".

### L11. `redactEvent` does nothing to CUSTOM frames

- **Where:** Task 6, the `publish` comment ("redacted like every producer frame") and the spec's Security section.
- **What:** `redactEvent` (`server_project.go:126-133`) sanitizes only `*events.RunErrorEvent`, so an `aura.elicitation` frame passes through unchanged.
- **Impact:** the comment is accurate, and the frame carries no operator value. But "redaction" protects nothing about the server's own text, so nobody should rely on it.

### L12. The Playwright CI job does not gate on screenshots

- **Where:** Task 9, Step 4, "the web jobs, which include Playwright (`ci.yml:1834`)".
- **What:** that line is `run: cd web && npm run test:e2e -- --update-snapshots`, a **temporary harvest mode** (`ci.yml:1829-1834`). Visual snapshots are regenerated on the runner, not compared.
- **Impact:** the ask_user redesign cannot fail CI on a visual diff. The screenshots in Task 10 Step 6 are the only visual check.
- **Fix:** say so in Task 9.

### L13. The coverage gate's success lines are worded differently

- **Where:** Task 9, Step 2.
- **What the plan expects:** `ok: owned coverage NN.N% >= 85%`.
- **What the scripts print:**
  - `ok: <scope> coverage <covered>/<total> (<pct>% displayed) >= 85%` (`scripts/coverage_profile_gate.sh:61`);
  - `ok: package-local coverage policy passed` (`scripts/coverage_package_gate.py:223`).

---

## Citation and symbol ledger

Unless a row says otherwise, every item is CONFIRMED by the quoted source at HEAD, or at go-sdk v1.8.0 and jsonschema-go v0.4.3.

### Global Constraints, Review Focus and File map

- **File sizes:**

  | File | Lines |
  |---|---|
  | `ExternalStoreChat.tsx` | 587 |
  | `budget_test.go` | 589 |
  | `resources.ts` | 580 |
  | `sseAdapter.ts` | 554 |
  | `agui/server.go` | 539 |
  | `bridge_supervisor.go` | 500 |

- **Toolchain:**
  - `~/.local/bin/node -> ~/.local/aura-toolchains/node24/bin/node` (v24.18.0), and likewise `npm` and `npx`.
  - `web/node_modules/@rolldown` has both `binding-linux-x64-gnu` and `binding-win32-x64-msvc`.
  - `go.mod`:3 `go 1.27.1`, :20 `github.com/google/jsonschema-go v0.4.3`, :30 `github.com/modelcontextprotocol/go-sdk v1.8.0`.
- **The bounds:**
  - `timeout.go:13` `const defaultMCPCallTimeout = 60 * time.Second`;
  - `budget.go:43` `defaultBudgetWallclockSec = 300`;
  - `elicitation.go:61` `const defaultElicitationTimeout = 300 * time.Second`;
  - `runregistry.go:24` `defaultRunMaxWallclock = 3600 * time.Second`.
- **File map paths:**
  - These exist: `web/components.json`, `web/src/approvals/InlineApprovalCard.tsx`, `approvalState.ts`, `sseAdapter.ts`, `sseResume.ts`, `ExternalStoreChat*.ts(x)`.
  - `web/src/questions/` and `web/src/i18n/resources.questions.ts` are to be created. The name follows the existing `resources.<area>.ts` pattern.

### Task 1

- **Coverage policy placement:** `scripts/coverage_package_policy.json` has
  - :60 `internal/packs` and :61 `internal/pgnumeric`, so `pausable` sorts between them;
  - :35 `internal/documents/filecard` and :36 `internal/embeddings`, so `elicit` sorts between them (Task 3).
  - Both use the format `{"mode": "target"}`.
- **stdlib:** `context.go:508` has the `afterFuncer` path, and `Cause` (`context.go:289-307`) returns `c.Err()` when no ancestor `cancelCtx` has a cause. That is why a child of an expired `deadlineCtx` sees `DeadlineExceeded`, and `TestWithTimeoutExpiresLikeAStandardDeadline` passes.
- **Every Task 1 test passes under `-race` with goleak.**

### Task 2

- **`budget.go`:**

  | Plan range | First line at HEAD |
  |---|---|
  | struct, 49-64 | 49 `// Budget bounds one agent run…`, 52 `type Budget struct {`, 64 `}` |
  | fields replaced, 49-55 | 55 `now func() time.Time` |
  | `NewBudget` literal, 185-195 | 185 `b := &Budget{` |
  | `ConsumeStep`, 250-252 | 250 `if b.now().After(b.deadlineWallclock) {` |
  | `Child`, 342-356 | 342 `func (b *Budget) Child(fanout int) *Budget {` |
  | `WithDeadline`, 369-374 | 373 `return context.WithDeadline(parent, b.deadlineWallclock)` |

- **Other citations:**
  - `llm_agent_tool.go:195-199`: 195 `if d := budget.NodeTimeout(); d > 0 {` and 197 `toolCtx, cancel = context.WithTimeout(toolCtx, d)`.
  - `runner.go:393` `boundedCtx, cancel := bud.WithDeadline(ctx)`.
  - `server_run_detach.go:40-42` `detachedRunContext`.
  - `llm_agent_parallel_test.go:246` `func TestRunToolAppliesNodeTimeout`.
  - `runner_budget_test.go:33` `deadline, ok := ic.Ctx.Deadline()`.
  - `budget_test.go:515`: **WRONG**, it is 516 (L3).
- **Helpers:** `llm_agent_test.go` (package `agent_test`) has `newIC` at :76, `textResponseCall` at :90, `recordingProvider` at :96 and `collect` at :106. `tools.Spec{Name,Summary,Parameters}` and `tools.ToolResult{Preview}` exist.
- **No production `Budget` literal exists outside `budget.go`, and `Held()` is nil-safe.** So test-built budgets keep working.

### Task 3

- **jsonschema-go `schema.go`:**

  | Line | Field |
  |---|---|
  | 62 | `Title` |
  | 63 | `Description` |
  | 64 | `Default json.RawMessage` |
  | 72 | `Type string` |
  | 74 | `Enum []any` |
  | 76 | `Const *any` |
  | 78-79 | `Minimum` and `Maximum *float64` |
  | 82-83 | `MinLength` and `MaxLength *int` |
  | 88 | `Items *Schema` |
  | 102 | `Required` |
  | 104 | `Properties` |
  | 112-113 | `AnyOf` and `OneOf` |
  | 129 | `Format` |
  | 132 | `Extra map[string]any` |

- **"Facts not verified" items, now measured:**
  - `enumNames` does land in `Extra`, through `unmarshalStructWithMap` (`util.go:365`).
  - `float64(36)` does validate as `integer`: `util.go:262-265` `if _, f := math.Modf(v.Float()); f == 0 { return "integer", true }`.
  - Both are proved by passing tests.
- **go-sdk citations:**
  - `client.go:1012-1022` has the allowed formats (1014 `allowedFormats := map[string]bool{`).
  - `client.go:895-904`: off by one, see L5.

### Task 4

- **`elicitation.go` and its tests:**
  - `elicitation.go` is 295 lines.
  - `elicitation_test.go` ranges: `fakeConsent` 15-45, `TestElicitationTimesOutToCancel` 181-203, the summarise tests 246-300, `TestElicitationMessageIsByteCapped` 302-316, `TestElicitationReachesHandlerOverARealSession` 370-395.
  - No other file references `ElicitationRequest`, `ElicitationField`, `summariseElicitationSchema`, `askOperatorBounded`, `elicitationPanicError`, `maxElicitationTypeBytes` or `elicitAction*`.
  - The old package-level `func elicit` (`elicitation.go:136`) disappears in the rewrite, so the new `elicit` import does not collide with it. `mount.go`'s local `elicit` variables are in a file that does not import the package.
- **Call sites:**
  - `bridge_supervisor.go:309` `res, callErr = session.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: args})`;
  - :339 `res, callErr := retry.CallTool(ctx, …)`;
  - :299 `return child.CallTool(ctx, name, args)`.
  - `bridge_call.go:41-46`: the `callCtx` and `cancel` block.
- **go-sdk behaviour:**
  - `mrtr.go:73` is `clientMultiRoundTripMiddleware`, and `mrtr.go:273` is `fulfillInputRequests`, which calls `errgroup.WithContext(ctx)`. The call's context values survive, so `callToolFrom` and `AskerFrom` work.
  - `server.go:1619` `assertServerInitiatedRequestAllowed`.
  - `client.go:381` `protocolVersion = protocolVersion20251125`.
  - `server.go:2004` and `client.go:1190` call `jsonrpc2.Async`, so two calls can be in flight on one session.
  - `protocol.go:255` `CallToolParamsRaw.InputResponses`, and `unmarshalInputResponse` decodes to `*ElicitResult` on the `action` key.
  - `server.go:200` `ServerOptions.SupportedProtocolVersions`.
  - `shared.go:618` `ClientRequest.Session *ClientSession`.
- **Test helpers:**
  - `helpers_test.go`: `mustTool` :21, `connectClient` :42, `mcpSessionOptionsFor` :53, `bridgeDefault` :71;
  - `elicitation_test.go:47` `callHandler`;
  - `mcptools/main_test.go` goleak `TestMain`;
  - `internal/mcp/sdkclient.go:58` `SessionOptions.Elicitation`;
  - `obs.BoundaryEnd.End(err error)` at `boundary.go:124`.
- **`cmd/aura/elicitation_consent.go`:** the comment at 16-22, `maxRenderedFields` at 32-35, `AskOperator` at 90, `renderElicitationPrompt` at 133.
- **`TestBridgedToolExecuteAppliesConfiguredCallTimeout`** (`bridge_test.go:129`) stays green with the pausable call context, measured.

### Task 5

- **cmd/aura:**
  - `runtime_tool_handles.go:36-40` `MCPFiles`;
  - `main.go:337` `func buildRegistryWithMCP(`, :352 `handles.MCPFiles = …`, :353-355 the early return, :418 `return mcptools.MountServer(… mcptools.MountOptions{Files: handles.MCPFiles})`, :549 `buildRegistryWithMCP(… nil, nil, nil, nil)`;
  - `mcp_tools.go:266-276` `mcpMountOptions`;
  - `toolpipe.go:80-89`, which passes a nil consent;
  - `elicitation_consent.go:68` `newElicitationConsent`;
  - `chat_boot.go:388-395` passes the real `elicitation`;
  - `mcp_live_mount.go:178` `mcpMountOptions(ctx, m.strict, server, m.handles)`, so live mounts get the consent too.
- **`mount.go`:** the locals are at 124/127 and 181/185, and the doc comments at 26-31 and 37-45. The 28-31 in the Files list is L6.
- **Test helpers:** `withMemoryMCPRegistry` :22, `seedMCPRegistry` :48 and `withDefaultOnRecipesOff` :63 in `mcp_registry_fake_test.go`; `closeMCPServers` at `main.go:508`.

### Task 6

- **`runsession.go`:** struct 52-84, `newRunSession` 89-108, the `append` comment 110-117.
- **`server_run_detach.go`:** the comment at 33-39, the `Start` error branch ending at 106, and 142-143 `defer cancel()` / `defer sess.finish()`.
- **Routes:** `server.go:365` is the steer route, and `idempotency_http.go:68` is the steer entry.
  - The idempotency guard applies only when `s.operations != nil` (`server.go:502-509`). Tests need no key. Production requires one (`keyPolicyRequiredHeader`), and Task 8 sends one.
- **Existing symbols:**
  - `resolveRunSession` (`server_run_resume.go:27`), and `scopedIdentityID` falls back to `localIdentityID` (`auth.go:364-368`);
  - `isLifecycleFrame` includes `EventTypeCustom` (`server_sse.go:177`);
  - `writeJSONStatus` (`conversations_api.go:95`);
  - `maxRunBodyBytes` (`server.go:29`);
  - `TestEveryRegisteredUnsafeHTTPRouteIsClassified` (`mutation_coverage_test.go:45`).
- **Test helpers:**
  - `newDetachTestServer` (`server_run_detach_test.go:44`);
  - `scriptedRunner` (`server_test.go:28`), `textTurn` :150, `postRun` :180;
  - `readFullBody`, `joinMessageContents`, `steerRunPayload` and `steerE2E*` in `server_run_steer_e2e_test.go`;
  - `agenttest.TitleClient` and `FakeClient.RecordedRequests`;
  - `events.NewCustomEvent` / `events.WithValue`;
  - `agui/main_test.go` goleak `TestMain`.
- **Missing from the plan:** `runner.Deps.PreviewCap` (`runner_deps.go:88`). See H1.

### Tasks 9 and 10

- `Makefile:138` prints `ok: quality gate passed …`.
- `scripts/coverage_docker.sh` exists. The success wording differs (L13).
- `.gitignore:18-22` holds the `internal/webui/dist` negation.
- `ad7701b41` is `build(web): embed the unified video editor bundle`.
- `docker/aura/Dockerfile`:19 `RUN npm run build` and :62 `COPY --from=webbuild …`.
- `ci.yml:1834` is Playwright, in harvest mode (L12).
- `governance_write_seam.go:47` `type MCPInstallRequest struct`.
- `prd.md:644` `## 13. MCP integrations`, with the target sentence at 656-658 (L8).
- Spec lines 3-8 are its first paragraph.

---

## The plan's "Facts not verified", now measured

| Fact | Result |
|---|---|
| A 2025-11-25-only server makes the client fall back to `initialize` at 2025-11-25 | **Yes.** Both classic tests pass in memory, and the classic subtest passes over streamable HTTP. |
| jsonschema-go keeps `enumNames` in `Extra` and treats `float64(36)` as an integer | **Yes, both.** |
| A classic request over streamable HTTP completes at 2025-11-25 | **Yes**, once H1 is fixed: 3.21 s with a 3 s operator. |
| A one-tool managed mount is always loaded | **Yes, at `-count=1`.** It depends on the process-wide 2-slot budget (L7). |
| `runner.Deps` accepts a nil `Steer`, and `LoopMaxWallclockSec` reaches the budget | **Yes, both.** The control test fails with a 10 s wallclock and passes with 2 s. |
