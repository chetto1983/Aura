# Plan validation 2 of 3: adversarial logic review

- **Plan:** `docs/superpowers/plans/2026-09-25-mcp-elicitation-question-card.md`
- **Spec:** `docs/superpowers/specs/2026-09-25-mcp-elicitation-question-card-design.md`
- **Date:** 2026-09-25. Read-only review; nothing was run against the stack.

## Verdict: YELLOW

The architecture holds.
- The pausable deadline, the call-context marker, the per-run asker in the replay ring and the owner-scoped answer route are sound.
- The main path works as written: a classic `elicitation/create` from one cockpit run over stdio, answered in the card.
- No finding forces a redesign.

But six High findings would ship broken or leak, and the plan must be amended for them before Task 3 starts. Four are fixed inside the plan's own tasks:
- the ambiguous-run refusal shows one conversation's form to another;
- a 422 echoes the operator's value and is stored for 30 days;
- the old 16 KiB form cap is dropped, with no cap on open questions;
- a reload loses the form once the ring has rotated.

Two need an operator decision or a spec amendment:
- advertising elicitation on every mount changes non-cockpit behaviour;
- E2E step 5 cannot run.

| Severity | Count |
|---|---|
| Critical | 0 |
| High | 6 |
| Medium | 11 |
| Low | 15 |

---

## High

### H1. An ambiguous-run refusal shows one conversation's form to every run on the session, other identities included

**Where:** Task 4 Step 3, `decideElicitation` (`case r.mixed`) and `refuse` in `elicitation_route.go`.

**Failure scenario:**
1. Alice's cockpit run and Bob's run both have a call open on a shared, non-identity-scoped mount.
2. The server sends a classic form for Alice's call: "Confirm transfer of €500 to IT60X…".
3. `routeFor` sets `mixed`. The question built by `questionFor` still carries `Message` and `Fields`, because `FromSchema` strips the server's text only when it returns an error.
4. `refuse` calls `askRecovered(call, asker, q)` for every asker. Bob's cockpit gets an `aura.elicitation` frame with Alice's message, field names, titles, descriptions and options.
5. A run with no asker gets the same text on its identity's Telegram channel, through `askFallback(untold, …)`.

The tests pin only `q.Refusal`, so nothing catches this.

**Fix:**
- In `refuse`, drop `Message` and `Fields` for `RefusalAmbiguousRun`, the same as `RefusalUnrenderable` does. The operator still learns which server asked and why.
- Add assertions to `TestClassicElicitationWithTwoRunsInFlightAsksNeither` and its channel-run twin: `q.Message == ""` and `q.Fields == nil`.

### H2. A 422 echoes the operator's value, and the idempotency layer stores it for 30 days

**Where:**
- Task 3 Step 3, `validate.go`: `checkValue` returns `err.Error()`, and there is a `FieldErrors{"": err.Error()}` fallback.
- Task 6 Step 3, the route: `writeJSONStatus(w, 422, {"errors": fieldErrs})`.

**Failure scenario:** jsonschema-go v0.4.3 puts the instance into its messages:
- `maxLength: %q contains …` and `pattern: %q does not match …` (`validate.go:198,203`);
- `enum: %v does not equal …` and `type: %v has type …` (`validate.go:145,126`).

Suppose a server asks for an "API token" with `maxLength: 32` and the operator pastes a longer secret:
1. The 422 body carries the secret verbatim.
2. The route is in `httpMutationRoutes`, and the cockpit sends an `Idempotency-Key`. `idempotencyMutation` therefore stores the captured response body as the replay envelope (`idempotency_http.go:319-333`) for `httpOperationReplayRetention = 30 days` (`:23`), in Postgres.
3. The value is now durably stored. That contradicts the spec's "values stay out of logs, metrics and traces" and the brief's "422 must not echo secrets".

The cockpit gains nothing from the text: `ElicitationCard` shows only "required" or "invalid".

**Fix:**
- Make `FieldErrors` values a closed set of codes, never library text: `required`, `invalid`, `too_long`, `not_asked`, `format`.
- Map the library error to a code by the failing keyword, or to `invalid`.
- Add a test that a 422 body never contains the submitted value.
- Add the same assertion on a 422 that goes through the idempotency middleware with `s.operations` set.

### H3. The old 16 KiB form cap is gone, and nothing caps the number of open questions

**Where:**
- Task 3 (`FromSchema` caps each string only).
- Task 4 (no limit per run or per session).
- Task 6 (`runQuestions.pending` has no bound).

**Failure scenario, one:** today `summariseElicitationSchema` refuses any schema over `maxMCPSchemaBytes = 16 KiB` (`elicitation.go:271`), and the plan deletes it. The new worst case for one question:

`20 fields × (256 B name + 256 B title + 1 KiB description + 50 options × (256 B value + 256 B title)) ≈ 540 KiB`

**Failure scenario, two:** `ClientSession.handle` calls `jsonrpc2.Async` (go-sdk `client.go:1188-1191`), so a server can have any number of classic requests in flight at once.
- A buggy or hostile server floods them during one call. Each becomes a card, a held clock and a ring entry.
- The ring holds 2048 events (`runsession.go:25`). That is sized at "~512 B per event, about 1 MiB per run" (`config_agui_run.go:26`), and now reaches about 1 GiB.
- The flood also pushes the turn's own frames out of the replay window, which triggers H4.

This is the threat T-45.1-30 names, and the plan reopens it.

**Fix:**
- Put a total byte cap on the projected question, for example 16 KiB, back to today's bound. Over it, decline as `unrenderable`.
- Cap open questions per run and per session, for example 4. Decline beyond the cap with a new refusal code.
- Refuse duplicate enum values while you are there (L7).
- Add a test with N concurrent requests on one session.

### H4. A reload loses the form once the run's ring has rotated

**Where:** Task 6 relies on the ring for "a reload brings it back". Task 8 relies on `attachRun`'s full-buffer replay.

**Failure scenario:**
1. `attachRun` subscribes with no `Last-Event-ID`, so from sequence 0.
2. When `firstSeq > 1`, `subscribeFrom(0)` returns `ok=false`, and `handleRunEvents` answers 410 "replay window exceeded" (`server_run_resume.go:77-80`).
3. The client takes `failWithSnapshot`, and no `aura.elicitation` frame comes back.

The ring counts every text, reasoning and tool-argument delta, and 2048 of them is little for a multi-step turn with reasoning. So a form asked late in a real turn does not survive a reload:
- the operator cannot answer it;
- the run's clocks stay held until the 300 s expiry;
- the server gets a decline.

The spec's "a tab that reloads gets it again" holds only for short turns. The plan's integration test and the E2E both use short turns, so neither can see it.

**Fix:** open questions must not depend on the ring. Either of these works:
- on `subscribeFrom`, re-emit every pending question from `runQuestions` after the replay;
- add `GET /agent/runs/{runID}/elicitations` (owner-scoped) and have the attach call it when the resume gets a 410.

Add a test that rotates a small ring (`BufferEvents: 8`) past the question, then reattaches.

### H5. Advertising elicitation on every mount changes what non-cockpit turns get from servers that respect the capability

**Where:** Task 5, every runtime mount. Task 10 Step 2 checks only memory, calendar and WhatsApp.

**Failure scenario:** the spec's premise is "today … the SDK refuses on Aura's behalf, and the tool fails". That holds only for a server that asks whatever the client says. A server that respects the capability behaves differently.
- Today it never asks. It skips its confirmation, uses defaults, or hides the tool.
- After Task 5 it asks every client: go-sdk advertises `elicitation.form` whenever a handler is set (`client.go:287-297`).
- In Telegram, cron and `aura chat` turns the fallback then declines. Those servers now get a decline and usually abort a call that used to succeed.
- The operator also gets a Telegram message for each call.

`server-everything` shows the pattern in the plan's own E2E: it registers `trigger-elicitation-request` only when the client advertises elicitation (`tools/trigger-elicitation-request.ts`, "If the client does not support the elicitation capability, the tool is not registered"). So tool lists change too.

Scheduled jobs are the risk. They have no asker, and nothing in the E2E drives one.

**Fix:** measure before Task 5 ships.
1. List the servers mounted on the VM and on the appliance, including the operator's own (Linear, Notion and the like).
2. Record which of them change their tool list or ask when elicitation is advertised.
3. Drive one scheduled job and one Telegram turn through a server that asks.

If any real server regresses, stop and ask the operator. Options: a per-mount opt-in (a governance flag), or advertising only on mounts the operator marks. Also add a Task 10 step that checks one non-cockpit turn after the change.

### H6. E2E step 5 cannot run: `trigger-url-elicitation` exists only for clients that advertise URL mode

**Where:** Task 10 Step 5, and spec step 5.

**Failure scenario:**
- `server-everything` registers `trigger-url-elicitation` "only when the client advertises URL-mode elicitation capability (`clientCapabilities.elicitation.url`)" (`tools/trigger-url-elicitation.ts`).
- Aura advertises form mode only: go-sdk sets `Form` and never `URL`.
- The tool will not be in the tool list, so the operator cannot ask for it and the "url mode is refused" log line can never appear.
- The E2E score is "≥ 9.8 on spec steps 1-8", so it cannot pass as written.

**Fix:**
- Amend spec step 5 and Task 10 Step 5. The proof becomes that the tool is absent from the mount's tool list: Aura never offers URL mode, so the server never asks.
- The refusal branch itself stays covered by `TestURLModeIsRefusedBeforeTheRunIsAsked`.
- Record in the PRD paragraph that the E2E does not exercise the URL-refusal branch.

---

## Medium

### M1. The classic heuristic can hand a request to an unrelated run

**Where:** Task 4, `routeFor` and `callOnSession`.

**Failure scenarios:**
- **(a) The call that asked has already ended.** A server's request is processed after its own call returned. This happens with a server that does not await its request, or when the request travels on the standalone GET stream. At that moment a different run has the single call open on a shared session, and `routeFor` gives that run the question. Its operator is asked, and its clocks are held. That run can belong to another identity.
- **(b) Requests Aura makes outside `callOnSession` carry no marker.** `resolveLinks` (`bridge_links.go:54`) and `hydrateViews` (`bridge_views.go:160`) issue `resources/read`. The SDK's multi-round-trip middleware covers `resources/read` too (`mrtr.go`, `clientMultiRoundTripMiddleware`). A form asked there takes the classic branch and is routed through the in-flight calls of other runs.

**Fix:**
- Mark every Aura-originated SDK request context, not only `tools/call`.
- In `routeFor`, treat "marker present or `elicit.AskerFrom(ctx) != nil`" as the call's own route.
- For classic requests, route only when every in-flight call carries the same identity (`identityctx`). Otherwise decline as ambiguous.

### M2. A hold stops the wallclock for the whole run tree, not only for the waiting call

**Where:** Task 2 (the clock is shared by `Child`) and Task 4 (`Hold` reaches the run's clock).

**Failure scenario:**
- While one branch waits on a form, parallel branches keep running: sub-agents under `ParallelAgent` and swarm children sharing the budget. None of their time counts against the 300 s.
- The spec's "only the operator's time is excluded; the server's and the model's time still count" is false under parallelism.
- The only bounds left are `max_steps` and the fixed 3600 s cap.

**Fix:** at least:
- state it in the PRD paragraph's "does not prove" list and in `Budget`'s comment;
- consider capping the total held time per run, for example the elicitation timeout times a small N, so a run that asks again and again does not rely on the 3600 s cap alone.

### M3. `refuse` can block the handler for the full 300 s before it declines, with no clock held

**Where:** Task 4, `refuse` calls `askFallback(untold|ctx, consent, q, timeout)` synchronously.

**Failure scenario:**
- A refusal is only a notice, yet it waits on the channel delivery for up to `AURA_MCP_ELICITATION_TIMEOUT_SEC`.
- If Telegram delivery is slow or hangs, the call is not held, so its 60 s bound fires first.
- The tool fails with a timeout instead of the server getting a clean, immediate decline.

**Fix:** send the refusal notice on a bounded background goroutine, tracked the way `BoundedCall` tracks abandoned calls, and return the decline at once.

### M4. The ask_user redesign changes behaviour the spec says must stay

**Where:** Task 7 Step 3: `typing = !choosing && !isApproval`, the Approve pill.

**Failure scenarios:**
- **(a) The free-text reply to an approval is removed.** Today an `approval` with no options renders a free-text field plus Answer (`InlineApprovalCard.tsx:166-203`). The plan removes the field and always sends `content: ''`. Spec §Decisions says "design only: its placement and its behaviour stay as they are". The Playwright spec is edited to match.
- **(b) An approval whose options are not scopes is labelled Approve.** A model's `ask_user(kind="approval", options=["Yes","No"])` gets an "Approve" pill, in the destructive variant when graded, that submits "No" when "No" is chosen.

**Fix:** get the operator's decision on (a); the spec's table and its own "behaviour stays" rule contradict each other. For (b), label the pill Approve only when every option is a gateway scope (`parseScopeChoice` non-null), and Answer otherwise.

### M5. Unsettled cards outlive their run on the client

**Where:** Task 8, the filter in `ThreadApprovalCards`, `item.outcome === undefined || isStreaming === true`.

**Failure scenario:** any path where the resolution frame never reaches the tab leaves a live-looking form, with its countdown and enabled buttons, for the rest of the thread's life, across later runs:
- Stop aborts the fetch before the cancel POST (the `sseResume.ts` Stop comment);
- the H4 410 fallback;
- a stream that was cut.

Each action then gets 410 and "This form was already resolved."

**Fix:** when the run is terminal on the client (`onTerminal`, a snapshot replace, or a new run starting), settle or drop the unsettled items. Add a test for it.

### M6. The E2E cannot prove the run bound with default configuration, yet the PRD paragraph plans to claim it

**Where:** Task 10 Steps 4 and 9.

**Failure scenario:** the elicitation timeout and the run wallclock are both 300 s. A human cannot outlast the run's remaining budget on the VM without the form expiring first, so the run proves the 60 s call bound only.

**Fix:** pick one:
- run one extra form with `AURA_LOOP_MAX_WALLCLOCK_SEC` lowered for the E2E, for example to 90 s;
- move "the 300 s run bound" to the "does not prove" list, where only the integration test covers it.

### M7. A third-party server's own request timeout bounds the wait, and the reference server hides it

**Where:** Task 10 Steps 4-5, and the countdown in Task 8.

**Failure scenario:**
- TypeScript-SDK servers time out server-to-client requests after 60 s by default.
- `server-everything` passes `timeout: 10 * 60 * 1000` explicitly (`trigger-elicitation-request.ts`), which masks it.
- With a typical server, the form is cancelled at 60 s through `notifications/cancelled` while the card still shows about 4:00. The operator loses the answer they were typing.

**Fix:** record it in the PRD paragraph as a limit Aura does not control. Word the countdown as Aura's bound, not the server's; for example, "Aura declines in …".

### M8. The new critical Go files get no mutation evidence

**Where:** Task 9 Step 4 ("the Go mutation job").

**Failure scenario:**
- The CI job scores six fixed scopes (`scripts/critical_mutation_gate.py:19-24`). None is new.
- `internal/pausable/context.go`, `elicitation_route.go`, `run_elicitation.go` and `elicit/validate.go` are never mutated, so Gate 3's "≥ 70% killed on the phase's critical files" has no evidence.
- On the web side, Stryker is scored as an aggregate, so a weak new file can be averaged away.

**Fix:** add scopes for `pausable/context.go` and `mcptools/elicitation_route.go` at least, then read the evidence in CI as the memory rule requires.

### M9. Test gaps on the risky paths

**Where:** Tasks 4, 6 and 8.

**Failure scenario:** each of these can break without any test going red:
- a classic request through an identity-scoped mount, which the plan says registers on the child's session "with no further change";
- the redial retry at line 339;
- a slog capture showing that the `mcp elicitation resolved` line holds no value;
- a 422 body with no value in it (H2);
- ring rotation (H4);
- two identities on one session, with the server's text stripped (H1);
- a flood (H3).

**Fix:** add one test per item, each written to fail against today's plan.

### M10. `closeLocked` publishes with `context.Background()` while holding `rq.mu`, and `append` then holds `s.mu`

**Where:** Task 6, `run_elicitation.go`.

**Failure scenario:**
- CUSTOM is a lifecycle frame, so `append` blocks until the frame is delivered or the subscriber leaves, with `s.mu` held.
- A tab that stays connected but stops reading, such as a suspended laptop or a half-open TCP connection, wedges the whole session behind a Background context: the producer's appends, `subscribeFrom` and the answer route all wait. The 3600 s cap cannot unstick it.
- The comment's reason is inaccurate. The producer uses its own context, which aborts the fan-out after the ring write (`server_run_detach.go`, `runProducer`); it does not use Background.

**Fix:** publish with the run's own context (`dctx`, kept on the `RunSession`). The ring write already comes first, so a replay still carries the frame.

### M11. Constraints the server sends never reach the operator

**Where:** Task 3, `Field`; Task 8, `FieldInput`.

**Failure scenario:**
- `minItems` and `maxItems` on multi-selects, and `pattern` on strings, are not projected.
- The reference form uses `minItems: 1` and `maxItems: 3` on both multi-selects.
- Picking 4 instruments gets a 422 and the generic "This value is not valid." with no hint.

**Fix:**
- Project `MinItems` and `MaxItems`, enforce them in the card (Next disabled outside the range) and show "Choose 1-3".
- For `pattern`, either project it or refuse it as unrenderable.

---

## Low

- **L1. Task 4, `TestAnUnansweredQuestionExpiresWhileTheCallIsHeld`.** Against a regression it fails by hanging, because the asker waits an hour. Bound the `Execute` in the test with a 5 s context.
- **L2. `redactEvent` does nothing to CUSTOM frames.** It redacts `RUN_ERROR` only (`server_project.go:126`). The spec's "they go through `redactEvent`" and the `publish` comment ("redacted like every producer frame") promise protection that does not exist. Correct the text, or decide explicitly what the server's message needs.
- **L3. The countdown mixes clocks.** It compares the client's clock with an absolute server deadline, so skew between the appliance and the browser misstates the time left. Send `expires_in_ms` next to `deadline`.
- **L4. The log line carries server-controlled text.** `out.err` in `mcp elicitation resolved` holds a field name of up to 256 B, with no `redact.Line` and no cap. The old code capped such values at 32 B. Apply `redact.Line` and `truncateUTF8Bytes`.
- **L5. The PRD edit is ambiguous and incomplete.** "Replace lines 656-658" would also delete "negative call timeouts cannot request unlimited execution." on line 656. The sentence "Mount, call, elicitation and shutdown have finite configured bounds" becomes misleading once a hold can extend a call past its configured bound. Amend it too.
- **L6. Skip does not mean "no value".** After the handler, the SDK applies the schema's defaults (`client.go`, `ApplyDefaults`). A skipped optional field with a default reaches the server with the default, while the receipt says nothing was given.
- **L7. Duplicate enum values are not refused.** React keys collide, and the selection is ambiguous.
- **L8. The `uri` check is stricter than the SDK.** It requires a host, so `file:///…` and other hostless URIs the SDK accepts come back as 422.
- **L9. Human time skews latency metrics.** `mcp_bridge` durations, `tool.execute` spans and `tool_invocations.duration_ms` now include it. Note it in the PRD, or record the held time separately.
- **L10. The push comes before the E2E.** Task 9 publishes the edge image before Task 10, so the appliance installs behaviour the E2E has not validated. Say so to the operator when asking for the push.
- **L11. The reference server may list the form tool late.** `server-everything` registers its conditional tools in `oninitialized`, so `trigger-elicitation-request` can appear only after `tools/list_changed`. Task 10 Step 3 should wait for the refreshed tool count.
- **L12. Small dead surfaces.** The JSON tags on `elicit.Answer` are never used; nothing marshals it. Under option V, the `@tool-ui` registry entry has no consumer. That one is acceptable as configuration, but say so in the commit.
- **L13. The clarification receipt echoes what was typed.** It is local only, but an operator who typed a secret sees it re-rendered in the thread.
- **L14. `ExternalStoreChat.tsx` reaches 594 of 600 lines.** The next touch, spec 2, must split it first.
- **L15. The spec cites a dead code path.** Its "URL elicitation required (`client.go:790-860`)" path is `urlElicitationMiddleware`, which go-sdk v1.8.0 never installs (it is unexported, with a TODO to export it). Nothing reaches it in production. Drop it from the spec's routing narrative so no one writes a test for it.

---

## Checked and sound

These were attacked and hold:
- **An answer racing the expiry.** Both sides resolve under `rq.mu`. The loser reads the buffered answer or gets 409. The frame published and the result returned always agree.
- **The pending map at run end.** `cancelAll` is deferred after `finish`, so it runs first. It sets `ended` and cancels every pending `Ask`, and a later `Ask` returns cancel at once.
- **Holds.** Every hold is released by `defer`. A release runs once. Nested holds bank time only on the last release, and `arm` re-checks when its timer fires, so a deadline can never be shortened.
- **`pausable` under the standard library.**
  - `propagateCancel` uses the `AfterFunc` method; `parentCancelCtx` sees a different `Done` channel.
  - `context.Cause` in Go 1.27 falls back to `c.Err()` for custom contexts, and `net/http`'s uses of `Cause` stay correct.
  - `DeadlineExceeded` reaches children.
- **`ConsumeStep` and the context deadline.** They read the same injected `now` and the same held total, and `Child` shares the clock.
- **The detached 3600 s cap.** It stays a plain `WithTimeout` above the pausable deadlines. `Deadline()` reports whichever is earlier.
- **Classic routing across identity-scoped mounts.** The connection context inherits the dialling caller's values (`notDone`), which could have tied a session to its first run. The plan routes classic requests on in-flight calls, not on `AskerFrom(handler ctx)`, so it is not affected.
- **Concurrency on the SDK side.** Classic requests are handled concurrently (`jsonrpc2.Async`), and the multi-round-trip path elicits in parallel through an errgroup.
- **The integration test's harness.**
  - Its test servers are not idempotency-guarded (`operations == nil`), so its POSTs without a key are fine.
  - Every `RunSession` is built through `newRunSession`, so `questions` is never nil.
  - Worker sessions are refused by `resolveRunSession`.
- **Coverage.** `internal/agent`, `internal/agent/mcptools` and `internal/agui` are all `target` mode. The two new `target` entries sort correctly.
- **File sizes.** Every touched file stays under 600 lines by the plan's own counts, which were checked here.
- **Dead code.** `summariseElicitationSchema`, `maxElicitationTypeBytes`, `askOperatorBounded`, `elicitationPanicError`, `ElicitationRequest` and `ElicitationField` are all deleted. The `maxMCP*` constants they used are still used in `bridge.go`. `buildRegistryWithMCP`'s `consent` is live again.
- **The web pump.** It keeps reading after `RUN_FINISHED`, so a resolution published by `cancelAll` still arrives.
