# MCP elicitation and the question card

Agreed with the operator on 2026-09-25. Today a mounted MCP server that needs input in the middle
of a tool call (`elicitation/create`) gets no answer: no mount advertises elicitation, so the SDK
refuses on Aura's behalf, and the tool fails. After this change, a server that asks during a
cockpit turn gets a form in the thread, as in Claude Code. The turn waits while the operator
answers, and the answer goes back to the server. The same change redesigns the card `ask_user`
already shows, so both kinds of question look and behave alike.

This is spec 1 of 2. Spec 2 reviews how every tool renders in the cockpit against the Tool UI
component set. It reuses the foundation this spec lays (the `@tool-ui` registry and the theme
bridge).

## Decisions

Stated by the operator:

- **The form lives in the cockpit thread, like Claude Code.** The turn waits for the answer.
  - The operator can accept with content, decline, or cancel.
  - Turns that do not come from the cockpit (Telegram, scheduled jobs, `aura chat`) keep today's
    decline-and-surface.
- **Approach A: the call stays open.**
  - The pending question lives in memory on the run.
  - The clock stops while the operator answers.
- **Tool UI is the visual reference.**
  - The operator's reference is the Tool UI Question Flow example: a "STEP 1 OF 3" label, a
    segmented progress bar, a bold title with a muted description, radio rows split by thin
    dividers, and a pill button that stays grey until a choice is made.
  - The ask_user card is redesigned in the same language. The change is **design only**: its
    placement and its behaviour stay as they are.
- **One step per field** when a server asks for several fields.
- **Two specs, questions first.** Reviewing every tool's rendering is spec 2.

Assumed during design and confirmed in the section reviews:

- **URL-mode elicitation stays refused.** A server-supplied link that the operator reads as
  Aura-sanctioned is a phishing primitive (T-45.1-31, `elicitation.go`).
- **When calls from different runs are in flight on the session, Aura declines.** The request
  arrives on a session with calls open from more than one run, so there is no single thread to ask
  in.

## Why the call has to stay open

Measured on 2026-09-25, from go-sdk v1.8.0 and Aura's code:

- **A server asks inside its own `tools/call`.** Classic `elicitation/create` is a
  server-to-client request sent while the call is running, and the server's handler blocks on the
  answer.
  - Ending the turn, the way `ask_user` does (`Runner.MintApprovalPause` writes a pause row and
    ends it), would cancel the call. So would persisting the question in `aura.paused_states`,
    which is how `ask_user` survives a restart.
  - Durability would buy nothing: the server's request dies with the process anyway.
- **The SDK hands the handler two different contexts.**
  - With **multi-round-trip** (`mrtr.go`, `fulfillInputRequests`) and **URL elicitation
    required** (`client.go:790-860`), the handler runs on the **tool call's** context.
  - With a **classic** `elicitation/create`, it runs on the **connection's** context. That context
    carries no run, thread or tool call.
  - In every path, `ElicitRequest.Session` is the `*ClientSession` the request came in on.
- **Two clocks would kill the wait.**
  - An MCP call is bounded at 60 s (`AURA_MCP_CALL_TIMEOUT_SEC`, `bridge_call.go`).
  - A run is bounded at 300 s (`AURA_LOOP_MAX_WALLCLOCK_SEC`). It is enforced twice: by the
    context deadline (`Budget.WithDeadline`, `runner.go:393`) and by `ConsumeStep`.
  - The elicitation wait allows 300 s (`AURA_MCP_ELICITATION_TIMEOUT_SEC`). Unless both clocks
    stop, a form that takes a minute to fill in kills the call it belongs to.
- **Delivery and reload come for free.** A detached run keeps a replay ring of AG-UI events on
  its `RunSession` (`internal/agui/runsession.go`, default 2048 events). An event appended there
  reaches the open tab live, and a tab that reconnects or reloads gets it again. Detached runs are
  the default (`AURA_AGUI_RUN_DETACH=true`).
- **The cockpit already renders shadcn components in Aura's palette.**
  - `web/src/styles/shadcn.css` maps every shadcn variable (`card`, `primary`, `muted-foreground`,
    `border`, `radius`) onto the blue tokens.
  - Tool UI components are shadcn registry entries (MIT, `assistant-ui/tool-ui`). They land
    already themed.

## Shape

```
MCP server ──elicitation/create──▶ go-sdk ──▶ mcptools handler
                                              │ which run?  call ctx (MRTR, URL-required)
                                              │             or in-flight registry by *ClientSession
                                              ▼
                                   elicit.Asker from the run's ctx ──none──▶ decline-and-surface (today)
                                              │
                          RunSession.publish(aura.elicitation)  ──SSE/replay──▶ cockpit QuestionCard
                                              │                                        │
                          wait (clock paused) ◀── POST /agent/runs/{run}/elicitations/{id}
                                              │
                          RunSession.publish(aura.elicitation_resolved)
                                              ▼
                              ElicitResult{action, content} ──▶ go-sdk validates, applies defaults ──▶ server
```

## `internal/elicit` (new)

A neutral package that both `internal/agent/mcptools` and `internal/agui` import, so neither has
to import the other.

- **Types.**
  - `Question{ID, Server, Tool, Message, Fields, Deadline}`
  - `Field{Name, Title, Description, Kind, Required, Default, Enum, EnumTitles, Multi, Format, Min, Max, MinLength, MaxLength}`
  - `Answer{Action, Content}`
  - `Kind` is one of `string`, `number`, `integer`, `boolean`, `enum`. `Format` is one of `email`,
    `uri`, `date`, `date-time`. These are exactly the types MCP's restricted elicitation schema
    allows.
- **The seam.**
  - `type Asker interface { Ask(ctx, Question) (Answer, error) }`
  - `WithAsker(ctx, Asker)` and `AskerFrom(ctx)`.
- **`FromSchema(server, tool, message, *jsonschema.Schema)`** projects the requested schema into
  `Fields`.
  - It enforces the caps: 20 fields, 50 enum options, a 2 KiB message, 256 B titles and a 1 KiB
    description, 4 KiB per string answer.
  - It extends `summariseElicitationSchema` and `maxElicitationTypeBytes` and replaces them.
  - Anything over a cap, or any type outside the restricted set, is an error that the handler
    turns into a decline.
- **`Validate(*jsonschema.Schema, content)`** checks an answer before the waiting handler is woken.
  - It uses the same `google/jsonschema-go` resolve-and-validate the SDK uses (`client.go:888-905`).
  - It returns per-field errors for the cockpit.

## `internal/agent/mcptools`

- **In-flight registry (`bridge_inflight.go`, new).** `MountedServer.CallTool` registers the call
  around `session.CallTool` (`bridge_supervisor.go:309`), keyed by the concrete
  `*sdkmcp.ClientSession`.
  - The entry holds the call's context. That context carries the asker, the tool name and the
    pause hook.
  - Identity-scoped mounts recurse into their child server's `CallTool`, so they register on the
    child's session.
- **Routing (`elicitation.go`).** The handler finds its asker in this order:
  1. `elicit.AskerFrom(ctx)`. It is set on the MRTR and URL-required paths, whose context is the
     call's.
  2. The in-flight entries for `req.Session`:
     - if they all belong to one run, the handler uses that run's asker and pauses that call's
       clock;
     - if they belong to different runs, it declines and surfaces why;
     - if there are none, because the server asked outside any tool call, it falls through to
       step 3.
  3. No asker: today's `ElicitationConsent` (decline-and-surface), unchanged.
- **The wait is bounded** by the first of these:
  - `AURA_MCP_ELICITATION_TIMEOUT_SEC` (default 300 s; `<= 0` keeps meaning "disabled"), which
    declines;
  - the end of the call's context, which cancels;
  - the end of the run, which cancels.
- **Mode.** `url` is still refused before any asker is consulted.
- **Handler contract.** The handler keeps returning `(*ElicitResult, nil)` in every case, as
  today.
- **Wiring.** Every mount gets the handler, so every mount advertises the elicitation capability
  (form only). That means filling `MountOptions.Elicitation` at the composition root
  (`cmd/aura/chat_boot.go`, `buildRegistryWithMCP`'s dead `consent` parameter). The fallback
  consent stays `surfacingElicitationConsent`.

## The paused clock

- **`internal/pausable` (new).** `WithTimeout(parent, d)` returns a context whose timer can be
  held.
  - `Hold(ctx) (release func())` pauses every pausable deadline in the context's ancestry. When
    the hold is released, each one gets back the time that passed.
  - Holds nest, and a released hold never shortens a deadline.
- **The MCP call.** `bridge_call.go` switches from `context.WithTimeout` to `pausable.WithTimeout`.
- **The run.**
  - `Budget.WithDeadline` builds its context with `pausable`.
  - `ConsumeStep`'s wallclock check reads the same paused total. That total is shared by pointer,
    so budgets derived for sub-agents (`budget.go:345`) see it too.
- **The handler** holds the clock from the moment the question is published until the answer
  arrives or the wait ends. Only the operator's time is excluded; the server's and the model's
  time still count.

## `internal/agui`

- **The asker (`run_elicitation.go`, new).**
  - `handleRunDetached` installs an asker bound to the `RunSession` on `dctx` before calling
    `s.run.Turn`.
  - A non-detached run gets no asker, which is the same rule steer follows.
  - `Ask` stores the question under a fresh id in a per-session pending map, then publishes the
    CUSTOM event `aura.elicitation` through `redactEvent`. It waits on a channel for the answer
    and publishes `aura.elicitation_resolved` with the outcome.
  - When the run ends, every pending question is resolved as cancel.
- **Publishing from outside the producer.** `RunSession.publish(ev)` appends under the existing
  mutex, following the rules `append` already has (ring, sequence, subscriber drop policy). The
  comment that says "producer-only" is updated to state what is true.
- **The route (`server_run_elicitation.go`, new).** `POST /agent/runs/{runID}/elicitations/{id}`
  with `{action, content}`.
  - It resolves the run through `resolveRunSession`, the same owner-scoped ladder steer uses, so a
    run the caller does not own is a 404.
  - It returns:

    | Code | When |
    |---|---|
    | 410 | the run is terminal |
    | 404 | the question id is unknown |
    | 409 | the question is already resolved or expired |
    | 422 | the content fails `elicit.Validate`; the body carries the per-field errors |
    | 202 | the answer was delivered |

  - The question is not closed on a 422.
- **The events carry no answer values.**
  - `aura.elicitation` carries the question: id, server, tool, message, fields and deadline.
  - `aura.elicitation_resolved` carries `{id, action}`.

## Cockpit (`web/`)

- **Components.**
  - `web/components.json` gains the `@tool-ui` registry.
  - `npx shadcn@latest add @tool-ui/question-flow @tool-ui/option-list @tool-ui/approval-card` is
    run from WSL (node_modules is single-platform) and lands the components in
    `web/src/components/tool-ui/`. They are versioned code, adapted only where Aura's tokens or
    lint require it.
- **`QuestionCard` (`web/src/questions/`, new).** One card, fed by two adapters:

  | Source | Shape |
  |---|---|
  | ask_user `choice` | one step: radio rows, a pill **Answer** that stays grey until a choice is made |
  | ask_user `clarification` | one step: a text field in the same frame, pill **Answer** |
  | ask_user `approval` | `ApprovalCard`: icon, title, what will happen. The destructive variant when the gateway grades the action Destructive. The gateway's scope options (`approval.scope.*`) become a single choice. |
  | MCP form | Question Flow, **one step per field**: "STEP 2 OF 3", a segmented bar, **Back** / **Next**, **Submit** on the last step |

- **Field inputs.**
  - A single-choice enum is radio rows.
  - A multi-choice enum is checkbox rows.
  - A boolean is two rows, Yes and No.
  - A string is a text input, typed for `email`, `uri` and `date`.
  - A number is a numeric input with its min and max.
  - Defaults are prefilled. An optional field has **Skip**.
- **The MCP form header.**
  - A chip names the server. The name comes from Aura's mount configuration, not from anything
    the server says. The tool name sits next to it.
  - The server's message is the description, rendered as plain text: no markdown or HTML from the
    server is ever interpreted.
  - A quiet countdown to the deadline shows, because at zero the form declines itself.
- **Always there.** **Decline** and **Cancel** sit as secondary actions in the footer. Cancel asks
  for confirmation while the run is streaming, as today.
- **After answering.** The card becomes a Tool UI read-only receipt: a check with the answer
  given, or a muted chip for declined, cancelled or expired.
  - A 422 puts the card back on the failing field's step, with the error shown there.
- **Where it sits.** The same place as today, in the stack above the composer
  (`ThreadApprovalCards`). ask_user keeps its resolve API and its lifecycle. Only its rendering
  changes, from `InlineApprovalCard` to `QuestionCard`.
- **Streaming.** `aura.elicitation` and `aura.elicitation_resolved` are parsed in a new
  `web/src/chat/sseAdapter_elicitation.ts`, following `steerNoticeValue`'s pattern.
  `sseAdapter.ts` is at 554 lines and does not grow past 600.
- **Keyboard and accessibility.**
  - Arrow keys move through rows, and Enter selects or advances. Escape never cancels a run.
  - Rows use `role="listbox"`/`option`.
  - The fade-in respects `prefers-reduced-motion`.
- **Copy.** Every string exists in en and it.

## Errors

| Situation | Result |
|---|---|
| No asker on the run (Telegram, cron, `aura chat`, detach off) | Today's decline-and-surface |
| Classic request with no call in flight on the session | Today's decline-and-surface |
| Classic request while calls from different runs are in flight on the session | Decline, and the operator is told which server asked and why |
| URL mode | Refused, as today |
| Schema outside the restricted set, or over a cap | Decline, and the operator is told |
| No answer within `AURA_MCP_ELICITATION_TIMEOUT_SEC` | Decline; the card shows "expired" |
| The call ends (server cancel, run cancel, tool timeout) | Cancel; the card shows "cancelled" |
| Answer to a closed question, or on a terminal run | 409 or 410; the card refreshes to its real state |
| Answer that fails the schema | 422 with field errors; the question stays open |
| Several questions at once (parallel tool calls) | One card each, in arrival order |

The handler never returns an error to the SDK: an error would fail the whole `CallTool` with an
opaque message (`elicitation.go`).

## Security

- **Nothing the server supplies is rendered as markup.** Its message, titles and descriptions are
  plain text, and they go through `redactEvent` like every other frame.
- **The server is named by Aura.** The chip shows the name Aura mounted it under.
- **The operator's values stay out of logs, metrics and traces.** Only the action, the server and
  the field count are recorded. The existing `mcp_elicitation` boundary gets the final action.
- **An answer is accepted only from the run's owner.** The route is owner-scoped: 404 hides a run
  the caller does not own, the same as steer.
- **A form never outlives its run, and a run never waits past its wait bound.**

## Testing

- **Go unit tests (`-race`, goleak):**
  - `elicit.FromSchema`: every restricted type, the caps, the rejects. `elicit.Validate`: the
    per-field errors.
  - Routing on all three SDK paths:
    - MRTR (call ctx);
    - URL-required (refused);
    - classic with one run, classic with two runs (declined), and no asker (fallback).
  - The wait: timeout, call cancel, run end, and a late answer.
  - `pausable`: hold and release, nested holds, a deadline that is never shortened, the
    `ConsumeStep` wallclock under a hold, and a derived sub-agent budget.
  - The route: 202, 404, 409, 410 and 422, and owner scope.
  - `RunSession.publish` interleaved with the producer. Replay after a reconnect carries the
    question and its resolution.
- **Integration.** An in-process go-sdk MCP server whose tool elicits a form, run once classic
  and once in MRTR mode. It is driven through the real AG-UI detached-run handler over httptest:
  - the event arrives;
  - the POST answers;
  - the server receives the content;
  - the MCP call outlives its 60 s bound while held, measured on a shortened bound.
- **Web:**
  - vitest ≥ 85% and Stryker ≥ 70% on `QuestionCard`, the two adapters and
    `sseAdapter_elicitation`;
  - en and it keys;
  - `aria-invalid` only when invalid.
- **E2E on VM .158, with the operator's own account:**
  1. Mount the reference server `@modelcontextprotocol/server-everything` as the operator's own
     MCP server.
  2. Ask Aura to use `trigger-elicitation-request`. Its form covers string, a string with a
     default, email, uri, date, integer and number with bounds, and single and multi enums, with
     and without titles.
  3. Fill it in the cockpit. Check:
     - that the server echoes the values;
     - that the turn outlived 60 s of human time;
     - that a reload in the middle of the form brings it back.
  4. Decline one form and let one expire.
  5. `trigger-url-elicitation` must be refused.
  6. Trigger an ask_user of each kind (choice, clarification, approval) and take screenshots of
     the new cards and their receipts.
  7. Every mount now advertises elicitation. Check that the servers already in use (memory,
     calendar, WhatsApp) still mount with the same tool counts as before, and that one call to
     each still works.
  8. Unmount the test server afterwards.

## Out of scope

- **Spec 2:** the review of every tool's rendering against Tool UI.
- **URL-mode elicitation.**
- **Forms on Telegram or other channels.** They keep decline-and-surface.
- **Surviving an aura restart.** The server's request does not survive one either.
- **Moving the question stack into the message flow.** The operator asked for design only.
- **Sampling and roots,** the other two server-to-client requests.

## PRD

§13 changes: *"The production elicitation wiring follows decline-and-surface"* becomes:

> A cockpit turn shows a mounted server's form elicitation in its thread. The call stays open,
> with its clock and the run's paused, until the operator accepts, declines or cancels, or
> `AURA_MCP_ELICITATION_TIMEOUT_SEC` passes.
>
> Turns with no cockpit, and requests Aura cannot place in a single run, keep decline-and-surface.
> URL mode stays refused.

The E2E's measurements are recorded next to it, together with what the E2E does not prove.

No new environment variables and no migration. The caps are constants in `internal/elicit`, with
their reason stated next to them.
