# Plan validation 3 of 3: cross-source check of the MCP elicitation plan

- **Plan:** `docs/superpowers/plans/2026-09-25-mcp-elicitation-question-card.md`
- **Spec:** `docs/superpowers/specs/2026-09-25-mcp-elicitation-question-card-design.md`
- **Date:** 2026-09-25. Read-only: no Aura file other than this one was written.

## Verdict: YELLOW

The architecture holds against the real code:

- The run-scoped asker on the call's context.
- The in-flight registry for the classic path.
- The held clocks.
- An answer route bound to the owner, consumed once.
- Pre-validation with the SDK's own library.

Every go-sdk fact the plan builds on checks out at v1.8.0 (see "Verified as the plan states").

Seven findings need a plan change before execution. Two are HIGH:

- One E2E step cannot pass as written, because the server's tool never registers.
- Values the operator typed would be persisted, which breaks the spec's own security rule.

None of the findings requires a different architecture.

| Severity | Count |
|---|---|
| HIGH | 2 |
| MEDIUM | 5 |
| LOW | 8 |
| INFO | 7 |

## Sources read (actual code)

| Source | Revision |
|---|---|
| Hermes, `NousResearch/hermes-agent` | `main` @ `7b761da2d` (2026-09-25). `D:\tmp\hermes-agent` holds only `scripts/install.ps1`, so the source was fetched with `gh api`. |
| go-sdk | `~/go/pkg/mod/github.com/modelcontextprotocol/go-sdk@v1.8.0` (WSL) |
| jsonschema-go | `~/go/pkg/mod/github.com/google/jsonschema-go@v0.4.3` (WSL) |
| Go stdlib | go1.27.1 `src/context/context.go` (WSL) |
| Tool UI, `assistant-ui/tool-ui` | `main`. `question-flow.tsx` last changed in `5ddabe9fd` (2026-02-27). Licence file: `LICENSE.md`. |
| MCP spec | `modelcontextprotocol/modelcontextprotocol` `docs/specification/2025-11-25/client/elicitation.mdx`, plus `2026-07-28/client/elicitation.mdx` for the diff. Published at https://modelcontextprotocol.io/specification/2025-11-25/client/elicitation |
| Archestra PR #8025 | merged 2026-09-21, merge commit `0aa97a660` |
| server-everything | `modelcontextprotocol/servers` `main`, `src/everything` (package 2.0.0, `@modelcontextprotocol/sdk ^1.30.0`) |
| TypeScript SDK | `modelcontextprotocol/typescript-sdk`: `main` and tag `v1.29.0` |

---

## HIGH

### H1. E2E step "`trigger-url-elicitation` must be refused" cannot run: the tool is never registered for a form-only client

**Evidence**
- server-everything registers the tool only when the client advertises URL mode:
  - `src/everything/tools/trigger-url-elicitation.ts:95-106`: `clientSupportsUrlElicitation = clientElicitationCapabilities?.url !== undefined; if (clientSupportsUrlElicitation) { server.registerTool(...) }`.
- With a handler set, go-sdk advertises form only:
  - `mcp/client.go:287-296`: `caps.Elicitation.Form = &FormElicitationCapabilities{}`, and URL is never set.
- The plan keeps it that way. Spec §Wiring says "form only", and Task 5 sets no `Capabilities`.
- So on Aura's mount the model has no `trigger-url-elicitation` tool to call.
  - Task 10 Step 5 expects "No card appears; the log shows `reason="url mode is refused"`, and the server is declined". That log line can never be produced.
  - Spec E2E step 5 and the Step 8 score of 9.8 or more both depend on it.
- Even a server that tried would be stopped by its own SDK:
  - go-sdk's server refuses `mode: "url"` to such a client (`mcp/server.go:1753-1756`).

**Recommended plan change**
- Rewrite Task 10 Step 5's URL bullet as an absence check: "the mount's tool list for `everything` does not contain `trigger-url-elicitation`". That absence is the measured proof that Aura does not advertise URL mode.
- Leave the handler-level refusal to `TestURLModeIsRefusedBeforeTheRunIsAsked`, which calls the handler directly.
- Update the spec's E2E step 5 to match.

### H2. A 422 persists the operator's rejected values for 30 days, against spec §Security

**Evidence**
- The plan's per-field message is jsonschema-go's error string.
  - `checkValue` returns `err.Error()` from `resolved.Validate(value)` (plan Task 3, `validate.go`).
  - `Validate` returns `FieldErrors{"": err.Error()}` for whole-object failures.
- jsonschema-go v0.4.3 puts the submitted value in those strings (`jsonschema/validate.go`):
  - 145: `enum: %v does not equal any of`;
  - 173 and 176: `minimum: %s is less than`;
  - 193 and 198: `minLength: %q contains …`;
  - 203: `pattern: %q does not match`;
  - 126: `type: %v has type`.
- The route writes `{"errors": fieldErrs}` with status 422 (plan Task 6, `server_run_elicitation.go`).
- The route goes through the idempotency adapter:
  - it is registered in `httpMutationRoutes` (plan Task 6);
  - the cockpit sends an `Idempotency-Key` on every answer (plan Task 8, `elicitationAnswer.ts`).
- That adapter stores any JSON-valid response body, of any status, as `replay_body`:
  - `internal/agui/idempotency_http.go:304-341`;
  - `internal/idempotency/maintenance.go:74` (`replay_body jsonb`);
  - retention is `httpOperationReplayRetention = 30 * 24 * time.Hour` (`idempotency_http.go:23`).
- The spec says the opposite: "The operator's values stay out of logs, metrics and traces. Only the action, the server and the field count are recorded."
  - `TestTheEventsCarryNoAnswerValues` checks only the SSE ring, so no test catches this.

**Recommended plan change**
- Make `FieldErrors` carry codes, never library text. For example: `required`, `invalid`, `too_short`, `too_long`, `out_of_range`, `not_an_option`, `format`, `pattern`, `too_many`, `too_few`.
  - The cockpit already shows everything except `required` as "invalid" (plan Task 3's `ErrRequired` comment), so nothing on screen changes.
- Log the library error at debug level, capped with `truncateUTF8Bytes`.
- Add a test that posts an over-bound value and asserts two things: the 422 body does not contain it, and the stored replay body does not either.

---

## MEDIUM

### M1. An expired question answers **decline**. The MCP spec and Hermes both make it **cancel**.

**Evidence**
- MCP 2025-11-25 `elicitation.mdx:548-567`:
  - Decline is "User explicitly declined the request".
  - Cancel is "User dismissed without making an explicit choice".
  - Servers are told "Decline: offer alternatives; Cancel: prompt again later".
- Hermes:
  - `tools/approval_prompt.py:325-326`: `return "cancel"  # nobody answered (timeout / prompt withdrawn) — not a user refusal`;
  - `:336`: the CLI timeout also cancels;
  - `tools/mcp_tool_sampling.py:310-312`: the outer timeout returns `cancel`.
- Archestra throws on timeout, which the TypeScript SDK sends back as a JSON-RPC error (`chat-mcp-elicitation.ts:193-197`). It never sends decline.
- Today's `TestElicitationTimesOutToCancel` already pins cancel. The plan rewrites it to decline (Task 4, Open point 4).
- The E2E server will then tell the model something false. server-everything prints "❌ User declined to provide the requested information." (`trigger-elicitation-request.ts:213-216`) for an operator who never answered.

**Recommended plan change**
- Amend the spec's error table so an expiry answers `cancel`.
- Keep the card's "expired" chip through the `expired` flag on `aura.elicitation_resolved`. The plan already adds it (Open point 2), so the card loses nothing.
- Keep `TestElicitationTimesOutToCancel` as it is.

### M2. There is no review before sending, which the MCP spec requires of clients

**Evidence**
- MCP 2025-11-25 `elicitation.mdx:40-45`, restated in 2026-07-28 `:40-44`: "MCP clients **MUST** … For form mode, allow users to review and modify their responses before sending."
- In the plan's `ElicitationCard`, on the last step, **Next**, **Enter** and **Skip** all call `send('accept')` at once (`advance()` → `if (last) void send('accept', current)`). **Back** allows changing an answer, but nothing ever shows all the answers together.
- server-everything's form has 13 fields (`trigger-elicitation-request.ts:56-172`), so the operator walks 13 single-field steps and never sees them side by side.
- The references:
  - Archestra puts every non-choice form in one dialog with all fields visible (`mcp-elicitation-fields.tsx:55-62`);
  - its choice card submits only "on the final tab … when every question is answered", with every tab reachable (`mcp-elicitation-card.tsx:39-46`).

**Recommended plan change**
- Add a final "Review" step when the form has more than one field. It shows `summaryOf(fields, values, labels)`, which already exists for the receipt, and each row goes back to its step.
- **Submit** lives only there. On the last field step, Enter and Skip go to Review.
- The step label counts Review, for example "Step 14 of 14".

### M3. Skip does not skip: go-sdk puts the server's default back after the handler returns

**Evidence**
- `contentFrom` leaves out any field without a value. `hasValue` treats `''` and `[]` as empty (plan Task 8, `elicitationSteps.ts`).
- `skip()` clears the field (plan Task 8, `ElicitationCard.tsx`).
- go-sdk then runs `resolved.ApplyDefaults(&res.Content)` on every accept (`mcp/client.go:901`).
- jsonschema-go fills every missing **optional** property that has a default (`jsonschema/validate.go:721-736`). Only required properties are skipped (`:722-725`).
- server-everything gives defaults to `firstLine`, `integer`, `number` and all five enums (`trigger-elicitation-request.ts:73-169`). Skipping any of them sends the default.
  - The receipt's summary shows nothing for that field, while the server received a value.
- Archestra declares `form: { applyDefaults: true }` (`platform/backend/src/clients/mcp-elicitation.ts`), so defaults being applied is the production norm and not a go-sdk quirk.

**Recommended plan change**
- Choose one of these and pin it in `ElicitationCard.test.tsx`:
  - hide **Skip** on a field that has a default;
  - label it "Use default" and show the default in the receipt.
- Either way, the receipt must list what the server will actually receive.
  - Compute it with the same rule: for an optional field with no value, show its default.
- Record in the spec that clearing a defaulted field cannot be expressed over MCP.

### M4. A schema go-sdk cannot resolve is shown as a form that can never be accepted

**Evidence**
- jsonschema-go compiles `pattern` with Go's `regexp`, which is RE2, inside `Resolve` (`jsonschema/resolve.go:345-350`). ECMAScript-only syntax (lookahead, backreferences) fails there.
- MCP allows `pattern` in form mode (2025-11-25 `elicitation.mdx`, String Schema).
- go-sdk's check before the handler never calls `Resolve` (`mcp/client.go:880-884`, `validateElicitSchema`). It resolves only **after** the handler (`:894-897`).
- The plan's `FromSchema` never resolves either, so such a form reaches the operator. Then:
  - every accept hits `Validate`, where `schema.Resolve(nil)` fails;
  - the route answers `422 {"": …}` every time;
  - the operator can only decline, after filling everything in.

**Recommended plan change**
- In `questionFor` (Task 4), or at the end of `FromSchema`, call `schema.Resolve(nil)` once. On failure, return an error, so the request is refused as `RefusalUnrenderable` before anyone is asked.
- Keep the resolved schema on `Question` rather than resolving it again on every answer.
- Add a `FromSchema` rejection case: `"pattern": "^(?=a)"`.

### M5. Advertising elicitation on every mount changes what servers do for runs outside the cockpit

**Evidence**
- Servers branch on the client's capability:
  - server-everything registers `trigger-elicitation-request` only when `clientCapabilities.elicitation !== undefined` (`trigger-elicitation-request.ts:40-46`);
  - it registers its URL tool only when URL mode is advertised (H1).
  - A server can equally choose between "elicit" and "proceed with a default or return an error" on the same flag.
- After Task 5, every runtime mount advertises the capability, including the sessions Telegram, cron and `aura chat` runs use.
  - Those runs then receive elicitations, which the fallback declines.
  - Before, the server knew not to ask.
  - So the spec's "Turns that do not come from the cockpit … keep today's decline-and-surface" is not literally true. Today's behaviour is that the capability is absent.
- Archestra keeps capability-bearing connections apart for exactly this reason.
  - `platform/backend/src/clients/mcp-client.ts:912-920`: "Elicitation support is declared during MCP initialize. Keep these clients separate so a connection opened without the capability is not reused for a tool call that may receive elicitation/create requests." The connection key gets an `:elicitation` suffix.

**Recommended plan change**
- Do not add a second connection per mount without the operator's decision.
- Instead, extend spec E2E step 7 and plan Task 10 Step 2 with one check on a non-cockpit surface (Telegram or a scheduled job): a call to `everything`'s `trigger-elicitation-request` is declined, the operator is told on the channel, and the tool result is acceptable.
- Record the Archestra alternative in Open points as considered and not taken.

---

## LOW

### L1. `Field` drops `minItems`/`maxItems` and `pattern`, which both MCP and Tool UI model

**Evidence**
- MCP's multi-select allows `minItems`/`maxItems`, and its string schema allows `pattern` (2025-11-25 `elicitation.mdx`).
- Both multi-selects in server-everything set `minItems: 1, maxItems: 3` (`trigger-elicitation-request.ts:129-130, 152-153`).
- Tool UI's `OptionList` has `minSelections`/`maxSelections` (`option-list/schema.ts:151-152`) and enforces them (`option-list.tsx:220-221, 244, 422`).
- The plan's `Field` has neither, so the operator learns about a bound only from a 422 after submitting.

**Recommended plan change**
- Add `min_items`, `max_items` and `pattern` to `Field`.
- Use the item bounds to disable **Next** in `QuestionOptions` multi mode.
- Pass `pattern` through only as a hint: the browser's `pattern` attribute uses ECMAScript syntax and Go's regexp uses RE2 (M4).

### L2. The `everythingForm()` fixture says it mirrors server-everything, but it does not

**Evidence**
- The plan's fixture (Task 3) has fields `name, color, email, homepage, birthday, age, score, agree, pet, legacy, size, toppings, extras`, requires `name` and `email`, and has no `minItems`/`maxItems`.
- The real form (`trigger-elicitation-request.ts:56-172`) has `name, check, firstLine, email, homepage, birthdate, integer, number, untitledSingleSelectEnum, untitledMultipleSelectEnum, titledSingleSelectEnum, titledMultipleSelectEnum, legacyTitledEnum`.
  - Only `name` is required.
  - Array defaults and item bounds are present.

**Recommended plan change**
- Use the real schema verbatim, citing the file and commit, so Task 3 exercises what the E2E will send.
- Keep the synthetic extras (`date-time`, the over-cap cases) in separate tests.

### L3. A mode the client did not declare gets a decline, where the MCP spec requires `-32602`

**Evidence**
- MCP 2025-11-25 `elicitation.mdx:698`: "Clients MUST return standard JSON-RPC errors … Server sends an `elicitation/create` request with a mode not declared in client capabilities: `-32602` (Invalid params)."
- go-sdk passes `url` straight through to the handler without checking what the client declared (`mcp/client.go:907-915`).
- The plan answers `(*ElicitResult{decline}, nil)`, as Hermes does (`mcp_tool_sampling.py:292-295`). Only a non-compliant server can reach this path (H1).

**Recommended plan change**
- Classic path, meaning the context carries no call marker: return `&jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "url elicitation is not supported"}`.
- MRTR path: keep the decline. An error there fails the whole `CallTool` (`mrtr.go:289-291`).
- State this exception to "never returns an error" in the handler's comment and in the spec.

### L4. The spec's third SDK path, "URL elicitation required", does not exist in v1.8.0

**Evidence**
- `urlElicitationMiddleware` is unexported and carries `TODO(rfindley): … Propose exporting it` (`mcp/client.go:765-775`).
- `NewClient` installs only the MRTR middleware (`mcp/client.go:79-81`).
- So a server's `-32042` reaches Aura as a plain tool error, and the handler never runs for it.
- The spec still says the handler runs on the call's context "with URL elicitation required (`client.go:790-860`)", and its Testing section lists "Routing on all three SDK paths". The plan's routing table silently lists two.

**Recommended plan change**
- Add an Open point recording that the path is dead in v1.8.0.
- Correct the spec's §"Why the call has to stay open" and its Testing list.

### L5. The Hermes citation carried into the rewritten `elicitation.go` is stale

**Evidence**
- The plan keeps "Hermes declined it for the same reason (`tools/mcp_tool.py:1720-1731`)".
- `tools/mcp_tool.py` is now 751 lines. The URL decline is `tools/mcp_tool_sampling.py:292-295` @ `7b761da2d`.

**Recommended plan change**
- Cite `NousResearch/hermes-agent@7b761da2d tools/mcp_tool_sampling.py:292-295`.
- The 300 s default is at `:262` of the same file.

### L6. The server's own request timeout bounds the wait, and the 300 s countdown does not know it

**Evidence**
- The TypeScript SDK's default outbound request timeout is 60 s:
  - `packages/core-internal/src/shared/protocol.ts:96` on `main`;
  - `src/shared/protocol.ts:106` at `v1.29.0`.
- A server that calls `elicitInput()` without its own timeout cancels at 60 s.
- go-sdk turns the server's `notifications/cancelled` into a cancelled handler context (`mcp/transport.go:259-272`). The plan then resolves the question as `cancel`, which is correct.
- The card's countdown, "Declines itself in {{time}}", keeps showing up to 5:00 until then.
- The E2E will not show this: server-everything passes `timeout: 10 * 60 * 1000` (`trigger-elicitation-request.ts:177`).

**Recommended plan change**
- Copy change: say "Aura declines in", and use a distinct receipt, "The server stopped waiting", for a cancel that ends the handler's context.
- Add to Task 10 Step 9's "what the run does NOT prove": servers on the TypeScript SDK default cancel at 60 s.

### L7. Both production references rule out an ambiguous classic request by serializing, not by declining

**Evidence**
- Hermes:
  - serializes every RPC per server: `async with server._rpc_lock` (`tools/mcp_tool_handlers.py:572`; rationale at `tools/mcp_tool.py:384-386`);
  - routes by the single `_pending_call_context` (`:573-577`).
- Archestra:
  - keys connections per (agent, conversation) (`mcp-client.ts:873-895`);
  - sets `concurrencyLimit = 1` for elicitation-capable calls (`:1426-1431`: "Serialize elicitation-capable calls so a cached client's elicitation handler is not replaced while another tool call on the same connection is active").
- Aura's plan lets calls run concurrently and declines when two runs are open on the session. That is operator-confirmed.

**Recommended plan change**
- No code change. Add to Open points that both references serialize instead, and why Aura does not: a 300 s form would block every other run's calls to that mount.
- Have the E2E record whether an ambiguous decline was ever observed.

### L8. Fields in alphabetical order put the only required field at step 8 of 13

**Evidence**
- The plan's Open point 3 is correct: order is lost.
  - `ElicitParams.RequestedSchema` is `any`, and on the client it decodes to a `map[string]any` (`mcp/protocol.go:2139-2145`).
  - `PropertyOrder` is `json:"-"` (`jsonschema/schema.go:143`).
- Archestra renders fields in the server's key order (`mcp-elicitation-fields.tsx:64-81`, JavaScript `Object.entries`).
- With server-everything, sorting by name makes `name` step 8.

**Recommended plan change**
- Order required fields first, in the order of the server's `required` array (a JSON array, so its order survives), then the rest by name.
- Pin that order in `TestFromSchemaReadsEveryRestrictedShape`.

---

## INFO

- **I1. No reference pauses a clock.**
  - Hermes gives the tool call and the elicitation the same bound: `_DEFAULT_TOOL_TIMEOUT = 300` (`mcp_tool_common.py:41`) and an elicitation timeout of 300 (`mcp_tool_sampling.py:262`). The tool clock keeps running through the wait (`mcp_tool_handlers.py:335`, `_run_on_mcp_loop(call, timeout=tool_timeout)`).
  - Archestra's elicitation waits up to 10 minutes (`chat-mcp-elicitation.ts:26`).
  - `internal/pausable` is therefore novel. Its premise is sound on go1.27.1:
    - `context.propagateCancel` attaches children through `afterFuncer` (`src/context/context.go:343, 508-512`);
    - `parentCancelCtx` rejects a context whose own `Done()` differs (`:383`).
- **I2. Tool UI confirms plan Open point 1, and option V.** Every claim holds in the source:
  - "Step N of M", "Back", "Next" and "Complete" are hard-coded (`question-flow.tsx:197, 477-479, 553, 565`);
  - steps are option lists only (`question-flow/schema.ts:16-22`);
  - `ProgressBar`, `SelectionIndicator` and `OptionItem` are not exported (only `QuestionFlow` and `getQuestionFlowStepIds` are);
  - `size="lg"` is used (`:126`);
  - `ApprovalCard` denies on Escape (`approval-card.tsx:107-115`) and has only cancel and confirm actions plus a metadata list, with no slot for scopes (`:119-130, 189-203`);
  - the file sizes are 793 and 625 lines.
  - The registry entries also pull in `registryDependencies: ['button','separator']` and 3 to 9 `shared/*` files each (`apps/www/public/r/*.json`).
  - `approval-card.tsx:9` imports lucide's whole `icons` map.
  - Both points strengthen option V over P.
  - Licence: `LICENSE.md` is MIT, "Copyright (c) 2025 AgentbaseAI Inc.". This closes the plan's unverified licence fact.
- **I3. The plan's Tool UI conventions differ from Tool UI's, as the spec intends.**
  - In Tool UI, Enter and Space only toggle an option; submitting takes **Next** (`question-flow.tsx:355-361`). The plan submits on Enter over a chosen single row, so a double Enter answers.
  - Tool UI turns a component into its receipt with a `choice` prop on the same component: `approved|denied` (`approval-card/schema.ts:12`), `{title, summary}` (`question-flow/schema.ts:37-42`), selection ids (`option-list/schema.ts:147`). It has no declined, cancelled or expired tone, and its receipts are always `role="status"`.
  - The plan's separate `QuestionReceipt` with four tones and an opt-in `announce` is Aura-built. Spec 2 should know that this, and not Tool UI, is the convention it inherits.
- **I4. Hermes as a reference.**
  - It routes through the call's contextvars snapshot plus `_rpc_lock` (above).
  - It declines URL mode.
  - The user sees an approve/deny prompt: the message, and a list of field names with types and descriptions (`mcp_tool_sampling.py:234-244`). No values are collected: accept sends `content: {}` (`:276`).
  - It does not validate. It is a routing and timeout reference, never a form reference.
  - Its `api_server` runs answer through a run-scoped `POST /v1/runs/{id}/approval` (`approval_prompt.py:300-309`), the same shape as the plan's route.
- **I5. Archestra's answer path matches the plan's.**
  - A pending marker is written before the question streams (`chat-mcp-elicitation.ts:142-155`).
  - The answer is bound to its conversation and consumed once with `getAndDelete`: the 404 and 409 equivalents (`:278-319`).
  - A resolved frame reports `answered|unanswered|cancelled` (`:157-215`).
  - The run's abort signal is joined with the upstream request's `extra.signal` (`:223-245`), the equivalent of the plan's `waitContext` over the handler's context.
  - Archestra does not validate against the schema before delivering. The plan's 422 is stricter, and better.
- **I6. go-sdk knows which request a classic elicitation belongs to, and drops it.**
  - The streamable client reads each POST's SSE stream with its `forCall`, but pushes every message onto the shared `c.incoming` (`mcp/streamable.go:2617-2680`).
  - Exact routing would need an upstream change. The in-flight registry is the only option inside v1.8.0.
- **I7. Which path the E2E will prove.**
  - server-everything 2.0.0 depends on `@modelcontextprotocol/sdk ^1.30.0`. At v1.29.0 the TypeScript SDK's `LATEST_PROTOCOL_VERSION` is `2025-11-25` (`src/types.ts:4-6`). v1.30's version was not checked.
  - The lab run will most likely take the classic path. MRTR is then proven only by Task 6's integration test.
  - The MCP spec's "Clients SHOULD implement rate limiting" (2025-11-25 `:708`) was dropped in 2026-07-28. None of the references implements it for elicitation.

## Verified as the plan states

- **MRTR runs the handler on the call's context.**
  - `clientMultiRoundTripMiddleware` is installed by default (`mcp/client.go:79-81`).
  - It calls `fulfillInputRequests(ctx, …)` with the `CallTool` context (`mcp/mrtr.go:73-118`).
  - Input requests run **in parallel** under an errgroup (`:273-293`), so parallel cards are correct, and a handler error would cancel the others (`:289-291`).
  - It retries at most 10 times (`:17`).
- **A classic request can be cancelled by the server.** `canceller.Preempt` turns `notifications/cancelled` into `conn.Cancel(id)` (`mcp/transport.go:259-272`). The plan's `waitContext` over the handler's context covers a cancel from the server.
- **A long wait blocks nothing else.** Incoming calls are marked `jsonrpc2.Async` (`mcp/client.go:1188-1192`), so a 300 s wait does not block pings or a second elicitation.
- **What the SDK does around the handler.**
  - Before: it checks the schema is restricted (`mcp/client.go:880-884`, formats at `:1014`).
  - After, only when `accept && content != nil`: resolve, validate, then `ApplyDefaults` (`:888-905`).
  - The plan's `Validate` uses the same library and adds format checks, which the library only records as annotations: `validate.go` has no format check.
- **What a handler advertises.** Setting one advertises `{form:{}}` at 2025-11-25 and later (`mcp/client.go:287-296`).
- **Protocol versions, as the plan's classic fixture needs them.**
  - The server refuses `Elicit` at 2026-07-28 (`mcp/server.go:1612-1627`, cited as 1619-1627).
  - The client falls back to `initialize` at 2025-11-25 (`mcp/client.go:371-383`).
- **`ElicitationCompleteHandler`** only signals URL-mode waiters and calls the user hook (`mcp/client.go:1522-1540`). With URL mode refused, the plan correctly leaves it unset.

## Per-question answers

1. **Hermes**
   - It routes through a contextvars snapshot and serializes calls per server.
   - The tool timeout is not paused: both clocks are 300 s.
   - URL mode is declined.
   - It does not validate.
   - The user sees an approve/deny prompt with a field list, and accept sends `{}`.
   - The plan diverges on the expiry action (M1) and on serialization (L7). Its citation is stale (L5).
2. **go-sdk v1.8.0**
   - The handler contract matches the SDK: validation, defaults, cancellation and `notifications/cancelled` (see "Verified as the plan states").
   - It has three gaps:
     - Skip against `ApplyDefaults` (M3);
     - no `Resolve` before asking (M4);
     - `-32602` for an undeclared mode (L3).
   - One spec path is dead (L4).
3. **Tool UI**
   - Open point 1 is accurate (I2).
   - Tool UI provides option rows, a progress bar, the receipt shape and selection bounds. The plan must build the text, number and date steps, the per-step errors, Skip, Decline and Cancel, the countdown and the i18n. It already plans to.
   - It leaves out selection bounds, which Tool UI does have (L1).
4. **MCP spec**
   - Met:
     - the server is named by Aura;
     - decline and cancel are always present;
     - server text is plain, and URLs are not linkified;
     - defaults are prefilled;
     - answers are validated.
   - Missed:
     - review before sending, a MUST (M2);
     - action semantics on expiry (M1);
     - `-32602` for an undeclared mode (L3).
5. **Archestra #8025**
   - It correlates with a bridge per conversation, a handler per call, a connection per (agent, conversation), a separate connection for elicitation-capable calls, and serialized calls.
   - Its answer path matches the plan's (I5).
   - The plan diverges on isolating the capability (M5) and on concurrency (L7).
