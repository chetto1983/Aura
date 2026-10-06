# Aura — Product requirements

Consolidated 2026-09-07. This is the current product contract. Historical proposals,
measurements and replaced designs remain in Git. Update the relevant section when
the contract changes; do not keep competing descriptions of the same behavior.
A requirement is not evidence that every implementation path has passed acceptance.

## 1. Product and scope

Aura is an agent for ongoing work on infrastructure controlled by its operator.
It combines durable conversations, attributable memory, document retrieval, tools,
scheduled work, delegation and an operator cockpit. CLI and Telegram are additional
surfaces over the same runtime. The audience includes individuals and small
organizations. A hardware/software bundle is a product direction, not proof that
any particular hardware configuration is launch-ready.

The application is a Go binary with an embedded web frontend, accompanied by
database, object-store, embedding, ingestion and selected integration/model services.
Local storage and local inference are distinct: a cloud model receives the context
sent to that provider. Cloud routes and subscription bridges must not be described
as offline or free without corresponding evidence.

Goals:

- Preserve useful context across work sessions with identifiable sources and corrections.
- Execute authorized work with visible outcomes, bounded resources and recoverable state.
- Keep identity-owned data and capabilities scoped through every public surface.
- Let operators configure models, integrations, skills and jobs without source edits.
- Make deployment, failure, recovery and release evidence inspectable and reproducible.

The current product does not promise universal accuracy, universal prompt-injection
resistance, exactly-once external side effects or unrestricted self-modification.
A feature named in an old design does not authorize recreating it.

## 2. Architecture and ownership

| Layer            | Responsibility                                                    | Source                                                                         |
| ---------------- | ----------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| Composition      | Wire runtime, stores, channels, workers and clients               | `cmd/aura`                                                                     |
| Turns            | History, context, persistence, pause/resume, capture and delivery | `internal/runner`                                                              |
| Agent            | Model rounds, tools, events, budgets and completion               | `internal/agent`                                                               |
| Policy           | Tool decisions, approvals, grants and reservations                | `internal/gateway`, `internal/approvalgrants`                                  |
| Control data     | Identities, conversations, settings, jobs and audit               | Postgres, `internal/db`                                                        |
| Memory/retrieval | Facts, graph relationships and derived search records             | `internal/arcadedb`, `cmd/arcadedb-mcp`                                        |
| Objects          | Original files and identity-bound storage                         | `internal/objectstore`, Garage                                                 |
| Ingestion        | Reconcile sources into cards and passages                         | `internal/ingestsupervisor`, `services/ingest`                                 |
| Extensions       | MCP connections and owned/shared skills                           | `internal/mcp`, `internal/mcpregistry`, `internal/skills`, `internal/skillacl` |
| Execution        | Workspace and per-identity sandbox boxes                          | `internal/sandbox/usersandbox`                                                 |
| Surfaces         | HTTP/SSE, embedded cockpit, Telegram and CLI                      | `internal/agui`, `internal/webui`, `internal/channels`                         |

Consumer interfaces and composition-root injection keep boundaries explicit. The
agent runtime must not depend on AG-UI. Events are the common delivery contract;
channels must not implement independent agent loops or attachment pipelines.

Postgres is authoritative for conversation turns and control state. ArcadeDB
conversation/reasoning projections are derived and rebuildable. Garage owns original
object bytes. An index record must not silently become authority over its source.

## 3. Identity, authentication and authorization

The host resolves identity. Missing, malformed, mismatched or foreign identity
references fail at the relevant boundary rather than falling back to a shared store.
Model-controlled arguments cannot select another user's resources.

Postgres runtime access uses a non-owner, non-superuser role without BYPASSRLS.
Startup verification checks table ownership as well as role attributes: a migration
role can bypass owner-table policies without either elevated attribute. Owner-scoped
transactions carry the identity required by fail-closed RLS policies.

Authentication/bootstrap tables and global scheduler discovery have different needs
from owner-data tables. Do not add RLS mechanically where an operation establishes
identity or requires a global claim. Their actual boundaries still require tests.

ArcadeDB uses one database and one server credential per identity. Validated database
names are deterministically derived; passwords are derived from the deployment secret
and database name. An application credential authorized for every tenant is not an
equivalent isolation boundary. Provisioning creates usable resources; deprovisioning
removes the database, credentials and dependent state with verified postconditions.

Web sessions use Authula. Recovery and administration are audited and require their
actual capabilities. Sharing does not imply administration. A wildcard capability
grant is not a standing approval for every tool action.

**Signing out ends the session (2026-10-02).** Authula runs a plugin's capability hook
only on the routes Aura lists in its route mappings, and `/sign-out` was not listed, so
the session hook never read the cookie: on the lab VM on 2026-10-01, `POST /auth/sign-out`
answered 401 and the same cookie went on answering 200 until the 12 h expiry, while the
cockpit showed the login page. `GET:/me` and `POST:/sign-out` now carry `session.auth`
(Authula's documented core mapping), and the cockpit leaves only when the server ended the
session or holds none. Measured after the fix on the lab VM (image `b71a33dac`, the
operator's account): sign-out answered 200 `signed out` and cleared the session cookie,
and the same cookie then answered 401 on `/api/voice/capabilities`, where it answered 200
a moment before.

**Every state-changing auth route checks CSRF (2026-10-02).** Authula enforces its
double-submit token and its origin check (Go's `http.CrossOriginProtection`) only on routes
mapped to `csrf.protect`, and none was: they ran nowhere, and sign-in, which has no session
for `SameSite=Strict` to protect, could be submitted from another site. All twelve
state-changing routes of the enabled plugins are now mapped, a test walks the registered
routes so an Authula upgrade that adds one fails until it is named, and `/api/auth/config`
keeps the token the browser already holds instead of minting one per call, which would have
failed the first of two login tabs. Measured on the lab VM (image `157373882`, behind Caddy
over HTTPS, the operator's account): with two login tabs open, the first signed in through
the real login page (200) and reached the cockpit; the cockpit's own sign-out answered 200
and the old cookie then answered 401; a cross-site sign-in carrying a matching token pair
answered 403 `csrf validation failed`, and one without the pair 403 `missing csrf cookie`.

This does not establish:
- browsers other than Chromium, or a client reaching the cockpit from an origin that is
  neither its own nor in the trusted list (it is refused by design);
- sign-out from every other session of the same user: the cockpit ends the current one.

**A session lives while it is used, up to seven days (2026-10-03).** Authula slides a
session only inside its own middleware, which guards `/auth/*`; the cockpit's routes are
validated by Aura's `Validator` and never reached it. Measured on the local stack: three
authenticated `/api` calls left `authula.sessions.expires_at` at sign-in + 12 h while one
`GET /auth/me` moved it, and the SPA never calls `/auth/me`, so a session used all day died
12 h after sign-in. A member reported the consequence: a dead cockpit that only a reload and
a new sign-in revived, because no cockpit code acted on a 401 (Authula SPEC §4.8 required
the redirect). `RequireAuth` now renews the session after the identity re-check: a 12 h idle
window renewed at most once an hour, capped at 7 days from sign-in, after which the session
is deleted so Authula's own routes refuse it too. The auth gate's 401 carries
`WWW-Authenticate: Session`; a same-origin 401 carrying it, outside the public pages, sends
the cockpit to `/login?expired=1` with the page to return to, and the login returns there,
accepting only a path on this origin. Measured after the fix on the local stack: a fresh
session was not rewritten; one with 30 min left was renewed to now + 12 h with the hardened
cookie re-issued; one created 7 days and a minute earlier answered 401 on `/api/me` and on
`/auth/me`, its row gone; in the browser, a cleared session led from an open conversation to
the expiry notice and, after signing in, back to it.

The first version redirected on any same-origin 401 outside `/auth/*`, and CI on master
refuted it (2026-10-03, run 37117206196): on a fresh database the cockpit's calendar panel
answers 401 "calendar authorization required" while the session is valid, so opening it
landed on the sign-in page, on chrome and mobile-chrome, retries included. A 401 is not
evidence of an ended session; only the challenge the auth gate adds is.

The same run's PWA spec failed its cleanup `DELETE /api/conversations/{id}` with 500, and
the cause is the web-e2e job, not the delete path (measured 2026-10-03). The job runs
`aura serve` without `ARCADEDB_ADMIN_USER`/`ARCADEDB_ADMIN_PASSWORD`, which every appliance
passes (compose.yaml, `aura` service). With no admin, `TenantClients.Existing` cannot ask
whether the identity's `mem_<uuid>` database exists, so it binds as the tenant, and a
database nobody has created answers 403: the delete lifecycle's reasoning step returned
`reasoning graph: memory for …: http 403: User/Password not valid`. Without admin Aura
cannot create that database either; in that job only the memory MCP could, once the cockpit
authorized memory, and in that run the cockpit spec never got that far. Reproduced locally
against an empty ArcadeDB: without admin the spec fails with exactly that body; with admin
`Existing` answers `ok=false`, the step is skipped and the delete answers 204. The job now
passes the appliance's admin pair. A deployment without that pair is not a supported shape
(operator decision, 2026-10-03): ArcadeDB ships inside the appliance and the installer always
hands Aura its admin credentials, so the no-admin path is not hardened.

This does not establish:
- why the member's runs ended with the interrupted-round marker: that cause is in the
  daemon log of their appliance (`round ended with no answer … cause=`), not read yet;
- the lifetime choices (12 h idle, 1 h renewal, 7 days absolute) as measured needs: they
  are a policy, kept where the passphrase cookie had them for the idle window;
- the behaviour on the lab VM or behind Caddy: measured on the local stack only.

**A PDF opened on an iPad, then a new sign-in.** On 2026-10-04 a member reported that
tapping a PDF the agent had delivered left the cockpit for iOS's file page ("dati – 30
KB", *Apri in Anteprima*), and that coming back asked them to sign in again. The
screenshot shows no browser chrome: the cockpit installed on the home screen. Measured the
same day on the local stack, in WebKit 26.6 with Playwright's iPad Pro 11 profile and in
Chromium 141, with the same results in both:

| What | Result |
|---|---|
| Session cookie | persistent: `Max-Age` up to 12 h, `SameSite=Strict`, `Secure`, `HttpOnly` |
| `GET /api/assets/{id}/download` of a PDF | `application/octet-stream`, `Content-Disposition: attachment` |
| The chat's PDF card | its only action is that download link |
| Tapping that link | a download event; the page stays on `/`; `/api/me` still 200 |
| The same URL without a session | 302 to `/login`, with no return path |
| Daemon log when a session is refused | nothing |

So the engine neither leaves the page nor loses the session. What the screenshot shows is
iOS's home-screen app presenting an attachment as a page with no way back. Playwright does
not reproduce that mode. Three changes follow from what was measured, not from the iPad:

- every refused session is logged with its reason (no cookie, refused, identity gone);
- a refused navigation keeps its page in `next`, as the SPA's own expiry redirect already does;
- the chat's PDF, document, spreadsheet and text cards gain an in-cockpit *Apri* that opens
  the existing preview, so reading a file no longer depends on a download.

This does not establish why the member was asked to sign in again. Both engines stored the
cookie as persistent; a restart of the home-screen app was not measured. Whether iOS sent
the cookie after the file page is what the new log line will answer on their appliance.

**What else left the home-screen app.** Reading the cockpit the same day found exits the
*Apri* card did not close: the Documents page opened a file with `window.open(…, '_blank')`,
and every download (chat card, artifacts panel, preview) was a link the app follows by
navigating. Apple documents the home-screen app as its own WebView, apart from Safari
(`navigator.standalone`, *Configuring Web Applications*), so a page that opens outside it
carries none of its cookies. The changes key on `navigator.standalone === true`, which only
WebKit's iOS family sets; a browser tab and Android or desktop installs keep their behaviour:

- the Documents page shows an opened or downloaded file in the cockpit's preview, read from
  the file manager's own route;
- a download link fetches the bytes and hands them to the share sheet (*Save to Files*), which
  closes onto the cockpit. A tap's activation does not reliably survive a fetch (WebKit, *The
  User Activation API*), so a tap that lapsed leaves a *Save* for a second one.

Measured on the local stack in Chromium 141 and WebKit 26.6 (Playwright's iPhone 13 profile),
with `navigator.standalone` and a share sheet that refuses a lapsed activation emulated: the
Documents page shows the preview with no new tab and the page does not move; the chat card's
download reaches the sheet with the file's name, type and bytes and starts no download.

This does not establish:
- what iOS does with the sheet, or how an iPad renders a PDF in the preview's iframe:
  Playwright runs neither the home-screen app nor iOS's PDF view;
- that iOS's `canShare` takes a Documents file: the listing carries no media type, so the
  probe has none, and a refusal leaves that link downloading as before.

**Clips and several files from the Documents page.** Two exits remained, closed the same day.
The direct route answered no Range request, which iOS requires of a media server, so a clip
opened in the preview could not play; and selecting several files and downloading them still
navigated once per file. No package closes the second: `file-saver` and `browser-fs-access`
fall back to a navigating download on iOS, while the share sheet takes several files natively.
So the direct route now serves through the same `SeekableObject` and `http.ServeContent` the
asset stream uses, and several files -- a Documents selection, or the artifacts panel's
*Scarica tutto* -- go to ONE share sheet, with the same second tap when the first lapsed.

Measured on the local stack against Garage: a clip uploaded to the Documents bucket answers
`Range: bytes=0-99` with 206, `Content-Range: bytes 0-99/<size>` and `video/mp4`; two files
selected on the Documents page reach one emulated sheet with their names and sizes, and no
download starts (Chromium 141 and WebKit 26.6).

This does not establish that iOS plays the clip in the preview, or that the sheet on the device
takes several files whose type is `application/octet-stream`; and each file is fetched whole
before the sheet opens, so a selection of large files is held in memory at once.

## 4. Agent lifecycle, tools and completion

The open `Agent` interface returns `iter.Seq2[*Event, error]`. Termination, budget
exhaustion and pause states are events; infrastructure errors remain errors. Each
turn uses a fresh agent and the run's immutable configuration snapshot. Workflows
and delegation preserve cancellation, error ownership and resource bounds without
double-charging child work.

A shared budget bounds steps and elapsed time. Results, background jobs, network
operations, worker concurrency and context have validated caps. Invalid configuration
must not silently disable a bound. Repeated identical calls must not run indefinitely;
changed results can establish real progress.

Calls pass policy and reservation before execution. Consequences are resolved from
the operation, including multiplexed verbs. Read retries are bounded; indeterminate
mutations are not blindly repeated. A crash after an external side effect but before
durable recording remains a disclosed recovery window.

An outbound message the operator approved is sent (2026-10-05). A WhatsApp `send_message` or
a PIM `send_email` on the managed recipe parks as a draft the operator reviews in the chat
before anything leaves (`70688c526`, 2026-09-28). Measured on the lab VM at `c57322064`:
the operator approved a WhatsApp draft and nothing was sent. The draft went from dispatching to
`uncertain` in 6 ms, the WhatsApp bridge received no call, the model was told delivery was
uncertain and then told the operator the message had been sent. The send runs outside the model
loop, under the operation of the resolve request (`POST /api/message-drafts/{id}/resolve`), and
deriving a tool's child operation requires a model round, so it failed before policy and
transport. The reviewed send is now its own single round; the draft's one-send claim still
prevents a second dispatch. A unit test reproduces the resolve request's operation.

A send that fails before it is dispatched is reported as not sent. Every failure that stops a
call before the tool runs (the operation derivation, the gateway's decision, a denial, a
rejected operation) is marked as not executed. A reviewed send so marked is recorded `failed`
with `no_effect` and the model reads "message was not sent". Only a failure after dispatch stays
`uncertain`. Either is logged with the draft id. The detail is the refusal reason when Aura
refused, and only the error type otherwise, since a transport error may echo the recipient or the
text.

A tool result with no content still reaches the chat. AG-UI requires `TOOL_CALL_RESULT` content,
and the run stream drops a frame that fails validation. The same VM turn logged it at 12:14:51
UTC for a WhatsApp `list_chats` that found nothing, so that call stayed in the chat without its
result. An empty result is now sent as `(empty result)`; the model still sees the empty result.
The translator's property test, which only ever drew non-empty results, now draws empty ones too.

Measured on the VM at `efdc895c0` (2026-10-05):
- The agent drafted the same immediate WhatsApp send, addressed to the operator's linked number
  (checked by hash, never printed). Approved through the review API, the draft went to
  `sent`/`ok` in 151 ms. The bridge logged `POST /api/send` at 13:09:26 UTC, the tool answer
  read `"success": true`, and the operator confirmed the message arrived.
- A `list_chats` with no match reached the stream as `(empty result)`, and no validation warning
  was logged.

This does not establish a pre-dispatch refusal on the VM: no live send was refused.

A terminal answer cannot race runnable sibling tools. Rejected streamed drafts are
explicitly discarded on every surface before another answer begins. A terminal-only
answer must be visible, and an already-streamed answer must not be duplicated.
Completion and persistence agree about the accepted answer.

A tool call leaked as text is not an answer. A production export read on 2026-09-24
ended a run on an assistant turn that opened with GLM's own call markup
(`<tool_call>shell_exec<arg_key>command</arg_key><arg_value>…`): 7.5 KB of inlined
base64 that degenerated into repetition, never closed and carried no max_tokens notice.
The provider had not turned it into a structured call, so the command never ran, the
markup was stored as the answer and the user had to ask again. A content-stop reply
that opens with `<tool_call` or `<tool_exec` is discarded on every surface, never
dispatched and never stored. The model gets one nudge (use the tool interface, never
inline large data); a second leak in the same run finalizes as `tool_call_leaked`.
The stored turn shows what the model emitted; it does not show which provider parser
failed or the finish reason, which that appliance did not record.

On 2026-09-08 the operator retired the paid completion critic in favor of the
LibreChat execution model. The inspected `@librechat/agents` 3.7.17 standard graph
routes pending calls through `toolsCondition` and otherwise terminates; it does not
add an LLM completion judge. Aura's previous critic ran only near the step limit,
failed open and accepted a third attempt unchecked, and did not prevent the measured
early multi-agent claims in spike104. Remove that model call, its prompt/parser,
digest and model override. Preserve deterministic reply hygiene, verification and
delivery checks, operation ownership and budget/status reporting. The existing
`AURA_COMPLETION_GATE` switch now controls only the free final-reply checks;
`AURA_COMPLETION_CRITIC_MODEL` is retired. Verify zero audit calls even at the step
limit and exercise real multi-agent results through the running cockpit.

The 2026-09-08 title inspection found a persisted title for conversation
01a08023-82d1-7675-8a97-42a6fd84f980 while its sidebar still displayed Untitled.
Aura also scheduled title generation only after a successfully drained turn and
at least three stored messages, skipping errors and pauses. LibreChat's agent
endpoint defaults to title generation from the first user message in parallel
with the answer, and its client waits for the persisted title with bounded retries.
Use that lifecycle in Aura: one bounded title worker per conversation, a snapshot
of the first request, language matching, conditional persistence that preserves
manual titles, and a bounded title-read query that refreshes the sidebar. Title
failure must not fail chat; retain a short first-message fallback in that case.

The same live title validation exposed a replay defect on 2026-09-08:
conversation `01a0807a-f5a6-73ed-a516-0f84b5e9e005` was stopped while its
90-second Python command was running. On reload, the store's synthetic
"previous result unknown after crash recovery" tool result appeared as Completed.
Project that exact recovery marker as an error through the existing assistant-ui
`isError` field, preserving the unknown-result text. Do not label an unknown result
as a successful execution or infer cancellation from arbitrary tool output.
This observation proves a replay presentation defect, not the process's final exit.

Deliverables are sent through the channel's artifact mechanism; a path alone is not
delivery. Partial outcomes identify unfinished work and the applicable limit.

The system prompt describes the manifest the model actually receives. A live request
captured on 2026-10-03 ("What do you remember about my sister?") contradicted itself in
three places:

| Prompt said | The same request carried |
|---|---|
| `<memory_context>` is "a bounded current index"; "when that block answers the current request, answer directly from it: do not call tool_search or open deep memory recall" | `<memory_context>`: "You have 1 facts across 2 entities … The content is NOT in this context" (the pointer that replaced the preload on 2026-09-03) |
| Loaded: `shell_exec`, `fs_read`, `document_search`, `document_open`, `ask_user`, `read_tool_output`, `text_response`; memory tools "are deferred" | 19 loaded tools: `read_file`, `write_file`, `patch`, `search_files`, `send_file`, `skill`, `shell_poll`, `shell_kill` and four memory core tools among them; no `fs_read` |
| Deferred families: filesystem write/edit/glob/grep, delivery, background shell, documents "index a file you made" | none of these was deferred; the index/describe tools were deleted on 2026-08-07, and nothing in the roster indexes a file |

`fs_read`, `fs_write`, `fs_edit`, `fs_grep` and `fs_glob` were replaced by `read_file`, `write_file`,
`patch` and `search_files` on 2026-08-07, but `shell_exec`'s description and the
truncated-call nudge still sent the model to the old names. The guard that keeps the prompt
honest only checked the deferred names it found there, so a name matching no tool at all
passed. The prompt now lists the always-loaded set that `TestOnlyTheWorkingSetIsAlwaysActive`
pins, names only the deferred families that exist, and says memory content is not in
context; a `<memory_recall>` block, when the preload attaches one, may still be answered
from directly. Every snake_case word the prompt uses outside a tag must name a
registered tool or a declared argument.

What this does not show: how often a model followed the stale instruction. The
2026-10-03 turn was answered through a bridge by the same assistant that wrote this
change, which is not independent evidence of model behavior.

Deferred-tool discovery stays lexical (in-process BM25 over name, summary and argument
names), measured 2026-10-06. That day the stack ran `aura chat` against a synthetic
OpenAI-compatible endpoint answered by Claude acting as the model under the real system
prompt, roster and sandbox. Following the harness, the model loaded tools by `select:` (every
call hit, every loaded tool was used) and reached for free text only when no roster name
fitted. Two free-text queries failed. "Look up the current price of something online"
loaded `current_time`. An Italian reminder request ranked nothing, and the no-match reply
then pointed to installing a skill — its list even named "recurring workflows", which is
`task`. Anthropic's own tool search uses the same ranker and gives the same remedy: keywords
in the words users describe tasks with, and a system-prompt list of tool categories.

So `retrievalKeywords` (`internal/agent/tools/search_keywords.go`) adds English and Italian
task vocabulary, keyed by name and limited to tools Aura owns (built-ins and `memory__*`).
It enters only the retrieval document. The no-match reply now sends the model to the
`<deferred_tools>` names and `select:` before `find-skills-aura`. Scored by
`TestMeasureRetrievalKeywords` (`-tags measure`), on the dumped corpus of 85 tools, with
the keywords off and on:

| Set | Without | With |
|---|---|---|
| Gate, production failures (26), top-1 | 26 | 26 |
| Held-out, same author (24), top-1 / recall@5 | 6 / 14 | 12 / 19 |
| Blind English (30), top-1 / recall@5 | 12 / 17 | 18 / 23 |
| Blind Italian (30), top-1 / recall@5 | 3 / 3 | 19 / 19 |
| Negatives (58) left with an empty ranking | 35 | 35 |

The blind set (`testdata/blind_eval_2026-10-06.json`) was written by a separate agent from
names and summaries alone, after the keywords were fixed and before they were scored.
Negatives are a family's queries scored without that family, plus requests no tool
serves. A score threshold that would let the ranker abstain was measured and rejected:
no cut separates them, and 0.2 IDF-weighted coverage already drops a production gate
query. On the stack the Italian request now loads `task` first and the reminder is
scheduled. A request that still ranks nothing gets the new reply, and the model recovers
through the roster with `select:skill_manage`.

`scripts/tool_search_usage.sql` reads the same evidence from `aura.tool_invocations` in
production: discovery by mode and outcome, which loaded tool the turn then used, and the
ranked queries themselves. A turn there ends at the next `role='user'` row. An `ask_user`
pause resumes under a new `request_id`, and scoping by `request_id` scored such searches
as unused.

What this does not show:
- how often production models write free text at all; that is the report's first number;
- the Italian vocabulary's reach beyond these 30 queries;
- discovery of third-party MCP tools (`calendar`, `whatsapp`, `linear`), which get no
  keywords and still miss paraphrases;
- abstention: a query for an unmounted family still returns its nearest lexical neighbour.
The blind set is spent once anyone tunes the keywords against its misses.

The report's first run on real data, 2026-10-06, on the lab VM's tenant as the table owner:
14 `tool_search` calls in 9 conversations, all on 2026-10-05. 11 were `select:` and 3 free
text, and all three loaded `task` first, the tool the turn then used. The ranker did not miss.
`select:` did: 4 of the 11 carried `scheduling`, the label the system prompt gives the
scheduling family (`internal/agent/prompt.go`), as if it were a tool name. The reply said it
was not registered and suggested nothing, because no name shares a token with it. Three of
the four then paid a second search before `task` loaded. Conversation `01a10c4a` sent the
same `select:whatsapp__send_message,scheduling` on two consecutive messages.

So an unregistered `select:` entry that shares no token with any registered name is read as
a capability word. Its best BM25 match loads in its place, and the reply says so. Suggesting
the match without loading it would not have helped: in all three cases the model's next call
already found `task` on its own, so that second call was the whole cost. An entry that does
share a token is a misspelling and keeps the old answer: the closest names, and nothing
loaded, so the spelling error stays visible.

What this does not show:
- 14 calls from one day of the operator's own reminder and WhatsApp tests are not a traffic
  distribution, and the ledger holds no rows before 2026-10-05;
- the calls predate the keywords above, which reached the VM with `8631afc7c` on 2026-10-06;
- a family label that shares a token with a tool name (`web`, `memory`) still gets
  suggestions only.

## 5. Approvals and durable grants

Approvals are host-issued and bound to identity, operation and effective arguments.
The model cannot mint approval, broaden its subject or resolve another identity's
pause. Empty accepted answers are invalid; decline and expiry are distinct outcomes.

Supported duration is once, conversation/session, or until explicit revocation,
with scope and action enforced outside model prose. Persisted action approvals use
dedicated semantics rather than the wildcard capability table. Permanent grants are
revocable from the cockpit and CLI.

Unanswered approvals have a bounded lifetime. Expiry, cancellation and resumed answers
converge through an identity-scoped durable lifecycle. One malformed or failed row must
not starve other resumptions; invalid rows are quarantined and failures stay visible.

## 6. Model runtime and hot settings

Provider, endpoint, model, supported reasoning settings, limits, credentials, loop
budgets and compaction trigger form a validated runtime profile. Supported changes
are published atomically after authenticated persistence. New interactive, scheduled
and delegated runs take one snapshot; in-flight runs retain their starting snapshot.
Removing a setting restores the correct pre-overlay fallback.

Settings report whether they are applied live, applied at boot or require restart.
The aggregate restart notice names the keys. Primary-route changes do not recreate
the daemon as their application mechanism.

When a route changes, inherited model limits not explicitly supplied with the change
are cleared before discovery; stale environment values cannot pin the previous
model's window. Metadata proves advertised capability, not generation at that limit.
Context and output budgets leave room for the answer, including provider reasoning.

The OpenAI-compatible client uses the official Go SDK. Provider-specific fields must
not leak into another provider's requests. Adaptive and manual reasoning use the
selected provider's capabilities and effort classes. Keyless local endpoints must
not require fabricated OpenRouter credentials. Unknown billing remains unknown.

The adaptive tier is the nearest neighbours' tier, not the nearest centroid's. Measured
2026-10-06 against the lab VM's EmbeddingGemma sidecar, on the 45-prompt live gate and on the
15 self-contained user prompts of the VM's own traffic (2026-10-02..06), labelled before any
result was seen. Under the shipped centroid the VM prompts scored 8 of 15, and all seven misses
went the expensive way: reminders and scheduler requests landed on `high` and reasoned for
10-21 s. Scores are gate / VM:

| Bank and rule | Gate (45) | VM (15) |
|---|---|---|
| 30 shipped anchors, centroid | 43 | 8 |
| anchors + 40 coverage seeds, centroid | 39 (2 hard turns `none`) | 14 |
| anchors, mean of the 3 nearest per tier | 42 | 10 |
| anchors + coverage, 3 nearest per tier | 42 | 14 |
| anchors, 3 nearest, plus a memory of judged turns read within cosine distance 0.30 | 42 after learning | 13, learning in order |
| the same memory with no distance bound | 39 after learning (3 hard turns `none`) | 15 |

The coverage seeds fill the gap the anchors visibly had: every `low` seed was a web lookup,
so small direct tool use (reminders, messages, calendar, timers) sat far from every tier. A
separate agent wrote them from the tier definitions without reading the repository, but its
brief was shaped by the VM's misses (it asked for "test" and "prova" in a non-programming
sense) and quoted two gate prompts as examples, so the 14 is an optimistic number and fresh
traffic is the real check. The memory rows were read with ArcadeDB's own
`vector.neighbors` (`filter`, `maxDistance`; `groupBy` for the per-tier rows) in 3 ms at the
median on the VM.

The shipped classifier scores the 3 nearest exemplars over the anchors and these seeds,
in process, so the gate still needs only the embedding sidecar. Against the same sidecar it
reproduces the probe exactly: 54 of 58 on the live gate grown by the 13 distinct VM prompts,
where the centroid scored 49. Its floors are 91/91 by the gate's own rule, measured minus one
case.

What this does not show:
- 15 prompts from one operator in five days; four are near repeats of an earlier one;
- where a production memory gets its labels. The memory above learned the labeller's tiers.
  The composer's explicit effort, the only human label Aura records, was set in 0 of the VM's
  13 conversations, so a memory fed by it alone would stay empty;
- that one neighbour inside the bound is the right rule beyond paraphrases; the bound is what
  kept the gate whole, and every bound from 0.10 to 0.30 gave the same scores here.

A teacher for that memory was measured the same day. The candidate is the router prompt Aura
already has, `ReasoningRouterSystemPrompt`. It was sent through Aura's own client on the
route the daemon resolves from `aura.settings`, run inside the VM's `aura` container. That day
the route was `ollama` with `gemma4:31b-cloud`.

- It scored 55 of 58 on the gate, sent no hard turn to `none`, took 515 ms at the median and
  used about 121 tokens a call.
- Its three misses were the scheduler tests, which it sent to `high`. The nearest-neighbour
  classifier gets those right with its widest margins (0.13-0.15).
- The classifier's own four misses all had margins under 0.06, and the teacher got each of
  them right. No prompt was wrong for both.
- Asking the teacher only when the margin is under 0.075 scored 58 of 58, with the teacher
  on 18 of the 58 turns. Under 0.03 it scored 56, with 5 calls.

What this does not show:
- the threshold was chosen on the same gate it was scored on;
- one route and one model; a weaker model is a weaker teacher;
- how many production turns would call the teacher: the 58 are not traffic proportions.

Image and video generation, cloud speech-to-text and text-to-speech, cloud embeddings
and every cloud model picker run on OpenRouter whatever the chat route is. Generation
spends the identity's own OpenRouter key and never the services key; speech and
embeddings spend the services key. Each person's key is minted once the management key is
set, on any chat route, and its cap is administered on any route. The chat route decides
only the chat turn's own credit (a keyless local route is exempt), the vision route when
the primary model reads images, and whether first-run setup requires the management key.
Recorded 2026-09-27 from an operator report: with the chat on Ollama every generation
refused `no_key`, because credential, catalogue, key minting and the Credit panel all
followed the chat route. Code reading the same day found the cloud speech clients built on
the chat base URL, so they would post `/audio/*` to the Ollama server carrying the
OpenRouter key. What this does not prove:
the split is covered by unit and handler tests, not yet by a live generation or cloud
speech call with the chat on Ollama; and the Credit panel's spend reads
`aura.cache_metrics`, so generation spend is enforced by the key's OpenRouter limit but
not shown there.

A key counts as present only while the provider still holds it, not while Aura still has a
row for it. Measured 2026-10-02 on the lab VM, reproducing an operator report: every key of
the account was deleted on OpenRouter and a new management key saved from the cockpit. The
reconcile that write runs minted nothing: the person's row in `aura.identity_llm_key` still
named the deleted key and so counted as a key, and the services key stopped on `the provider
holds 0 live keys named aura-services`. The deleted keys were gone from the provider's
roster, not listed as disabled. The reconciler therefore asks the provider: a person's key
whose `GET /api/v1/keys/{hash}` answers 404 is replaced by a fresh key at the limit its row
held, and a services key that `GET /api/v1/key` refuses with 401 is replaced by a fresh
`aura-services` at the services cap. A key the provider still holds, disabled or not, is
kept. `GET /api/v1/keys` returns the 100 most recent keys and pages with `offset`
(OpenRouter's management-key guide, read the same day), so the roster is read page by page.
Measured the same day on the VM after `9f2032773` reached it, with both keys still deleted:
the boot reconcile, within a second of start, rewrote `OPENROUTER_API_KEY` as
`aura-reconciler` and replaced the person's row (new hash and label, still no limit for the
admin) with nothing logged as a failure; the provider's roster then listed the new key under
that identity, a second reconcile minted and aligned nothing, and the operator's next chat
turn billed on the new key (19,506 prompt tokens, $0.00035) with no 401 logged. The services key is
rewritten only when `GET /api/v1/key` refuses the stored one, so a deleted key answers 401
there. What this does not prove: a replacement starts with no spend at the provider, so a
capped member's monthly allowance restarts with it; a member's replacement was covered by
unit tests only, the VM holding one admin; and when the new management key belongs to
another account, the keys the old account still holds are replaced here but stay live there.

The Ollama route uses the signed-in local server for inference. Its model picker merges
the models available from that server's `/api/tags` with the current cloud catalogue
from the fixed, unauthenticated `https://ollama.com/api/tags` endpoint, deduplicating
ids. Picker ids use the local-API cloud spelling:
an untagged public name gains `:cloud`, while a tagged name gains `-cloud`. Measured
2026-09-21 on the Ubuntu test appliance with Ollama 0.34.2: local `/api/tags` exposed
only `gemma4:31b-cloud`, the public endpoint exposed 20 models, local `/api/show`
rejected the direct-cloud name `gemma4:31b` with 404 and accepted
`gemma4:31b-cloud` with a 262,144-token context. The public `/api/show` accepted
`glm-5.3` and published a 1,048,576-token context without a credential. This proves
the catalogue and metadata shapes observed on that date, not account entitlement or
successful inference: a live request for `glm-5.3:cloud` returned 402 for the test
account. The configured local route must remain reachable so a catalogue choice
cannot hide a broken bridge. Linux Compose maps `host.docker.internal` to Docker's
host gateway; a host-installed Ollama binds to that gateway rather than LAN or host
loopback when the deployment selects this route.

Native subscription OAuth/Responses/Messages integrations require separate measured
transport and credential lifecycles. A configurable base URL does not establish such
support. Credentials require protected identity/operator storage, refresh and
revocation; they do not belong in prompts, logs, source or public artifacts.

ChatGPT plan sign-in uses OpenAI's published OSS registration and Responses contract,
with a separate protected registration per Aura identity, the VM's stable host ID,
PKCE, validated OIDC identity, renewable tokens and explicit plan permission. Its
model picker reads the selected account's catalogue; an identity token alone grants
no inference access. The existing official SDK supplies Responses and streamed local
tool calls; subscription turns do not probe or debit OpenRouter credit.

Measured 2026-10-02 on the Ubuntu appliance at `192.168.101.158`: Chromium in the
existing owner sandbox received a callback at an ephemeral
`http://127.0.0.1:<port>/auth/callback`, opened the official OpenAI authorization
request to the "Welcome back" screen, and supplied a live frame in 26 ms through
agent-browser's existing loopback stream. Thus the cockpit can reuse its authenticated
live browser to let the person sign in on the VM where the callback listener runs.
The login returns an owner-scoped cockpit browser route, never an unreachable
client-PC loopback. No Compose changes, forwarded ports or GitHub relay are needed.
OpenAI's documented loopback URI remains identical in authorization and exchange.
The bounded browser flow closes its listener and temporary browser on completion,
cancel, disconnect, timeout and daemon shutdown; errors preserve an existing account.
These measurements establish browser navigation, streaming and callback placement;
they do not establish human consent, issued credentials, account entitlement or real
Responses inference. Those remain required deployed E2E acceptance measurements.

The same appliance subsequently completed owner sign-in: authenticated status returned
`connected=true`, `plan_enabled=true`, `approved`, and the account catalogue returned
five visible models. With GPT-5.6-Sol active, Aura still returned undetected reasoning
capabilities and only Auto/Off. The official account catalogue measured on that VM
publishes `supported_reasoning_levels` objects: low, medium, high, xhigh and max for
that model, default low, and reasoning summaries enabled. Its additional ultra token
is outside Aura's current effort vocabulary and is excluded until separately supported.
The existing allowlist, composer capability source, fixed/adaptive policy and Responses
SDK must carry the selected model's advertised levels, including after daemon restart;
no inherited provider effort set or fabricated levels may substitute for that catalogue.
This measures catalogue metadata and granted access, not completed inference or a
streamed reasoning summary; those remain separate deployed acceptance checks.

The pinned Playwright 1.63.0 API request path omits Secure CSRF cookies on HTTP
127.0.0.1. Measured with real Authula v1.46.0 and a disposable Postgres fixture:
HTTP sign-in returned 403 `missing csrf cookie`, while the existing TLS proxy returned
sign-in 200 and authenticated ChatGPT status 200, with Secure/HttpOnly cookies intact.
The web CI gate therefore runs its existing complete browser matrix through that
HTTPS proxy. Production authentication and the deployment Compose remain unchanged.
This establishes the login transport, not a passing full browser suite.

A sign-in through the live view stopped on OpenAI's passkey challenge
(`auth.openai.com/mfa-challenge`): the page asked for a passkey and "Try another method"
did not respond. Measured 2026-10-06. A passkey request opens Chrome's own WebAuthn
dialog, which the live view's page screencast never contains, and while it is up the page
takes no DevTools input. In headless Chromium 141 on a cloud host, a click did not reach
the page during a pending `navigator.credentials.get` and did without one, Escape did not
dismiss the request, and a request with an 8 s timeout was still pending after 60 s.
Headed Chromium under Xvfb drew the dialog ("Insert your security key") on the X display,
outside the screencast, and dropped the click the same way, so a headed or "full" browser
does not help. No browser in the box can use the person's passkey in any case: the box has
no platform authenticator, no USB key and no Bluetooth for the phone's hybrid transport,
and WebAuthn binds a credential to its origin, so the cockpit cannot relay one. With the
DevTools `WebAuthn` domain enabled and `enableUI: false` on the tab there is no dialog:
the request ends with `NotAllowedError` at its timeout and the click arrives. The login
helper therefore enables that domain on its tab before opening OpenAI and holds the
DevTools connection until the login ends; losing that connection ends the login with an
error. On the box image (Chrome for Testing 151, agent-browser 0.38.1), with a local TLS
fixture of the challenge page served as `auth.openai.com`, a click relayed as the cockpit
sends it produced no callback before this change and the callback 25 ms after it; the
start-up to the loaded page stayed at about 1.1 s. `TestChatGPTLoginSurvivesPasskeyChallenge`
repeats this in the `docker_integration` tier and fails on the previous helper. Not shown:
OpenAI's real page reacting to the unanswered request, which the fixture only imitates; an
account under OpenAI's Advanced Account Security, which allows only passkeys and security
keys and so cannot sign in through any remote browser; tabs or popups opened after
navigation, which the helper does not cover; and agent sessions opened through the MCP
bridge, whose live views keep the freeze.

## 7. Conversations, compaction and steering

Conversation writes and aggregates are atomic and owner-scoped. Branch history,
ordering, tool/result pairs and attachments survive persistence and replay. A full
conversation must not be reread unboundedly when a verified compaction boundary
permits paging.

Context assembly reuses stored branch compaction, applies configured summarization,
preserves the recent working tail and enforces a hard budget. Summaries retain the
correct source version and branch. Failures are reported; fallback truncation records
what was dropped. Token estimates and provider usage are separate measurements.

The stable prompt prefix excludes volatile per-turn data. Worker framing and current
budgets do not mutate that prefix. Definitions and dynamic context obey the same cache
contract in interactive and delegated runs.

Steering uses a bounded durable queue tied to the owning conversation. Cancellation
is distinct from steering. Queued attachments retain their pending-turn lifecycle.
Stop must produce a terminal client event even when cancellation ends the iterator
without error. That announcement survives the cancellation that caused it. Failed
process termination is observable rather than silently reported as success.

An SSE disconnect does not mean cancellation. Detachment, replay and Last-Event-ID
resumption preserve ordering and identity. A terminal worker stream closing normally
is completion rather than a reconnection error.

The 2026-09-08 cockpit screenshot and mounted-MCP inspection showed a long conversation
list with only individual actions and one archived-visibility checkbox; there was no
selection mode. Add explicit multi-selection, select-all for the currently listed rows,
and bulk archive/restore/delete. Reuse the owner-scoped per-conversation endpoints and existing
delete lifecycle. Confirm permanent deletion with the selected count; disable repeated
submission while pending; preserve failed selections and report partial outcomes. Reset
the active chat only after its deletion succeeds. EN/IT copy and desktop/mobile keyboard
operation are required. A mixed selection archives only active rows and restores only
archived rows; keep unprocessed rows selected. Mobile and desktop sidebars coexist in the
DOM, so checkbox/label IDs must be unique per mounted instance (the live mobile probe found
two identical archive-filter IDs and an unlabeled visible checkbox). Assistant-ui provides per-thread archive/delete primitives; the
current Aura sidebar already owns its external-store conversation list. No new persistence
system or bulk authorization endpoint is required. Verify only disposable fixture chats
through the real MCP browser; the screenshot does not authorize deleting existing history.

The owner's conversation export (`GET /api/conversations/{id}/export`, the sidebar Export
action) is the raw Markdown dump of that conversation. A production export read on 2026-09-24
had 64 sections; 40 of its 50 assistant sections were an empty fence under "Tools used",
because the owner download reused the share snapshot, which drops tool arguments, tool
results, system turns, reasoning and timestamps by design. The dump therefore renders every
persisted turn of every branch in seq order without tool-pair repair (an orphaned result
from an interrupted run stays visible): role, content with sidecars rehydrated, tool calls
with their arguments and ids, reasoning and its duration, timestamps, token counts,
non-linear branch/parent pointers, attachments and delivery keys, then the compaction
summaries and every asset of the thread. Configured secret values are masked; nothing else
is filtered. Share links keep the redacted snapshot (ADR 0039). That measurement shows the
redacted projection cannot explain what a run did; it does not bound the dump's size for a
very long conversation.

Conversation search finds a word inside a message (2026-10-04). Measured on the local
stack: one realistic 430-character Italian message was sent through the cockpit, then
`GET /api/conversations/search` was asked eleven times:
- single words (`fattura`, `commercialista`, `Giulia`, `tagliando`);
- two- and three-word phrases;
- the unaccented `lunedi`, the plural `fatture` and the typo `fatura`;
- the message's own first sentence.

Not one search found it. The query was pg_trgm's `content % $1`, and `similarity` compares
the query with the whole message, so the score falls as the message grows: 0.025 to 0.082
for the words and phrases, 0.226 for the copied sentence, all under the 0.3 threshold. A
message could only be found by a query about as long as the message itself.

On the same text the alternatives scored:
- `word_similarity`, which compares the query with the best-matching extent of the message
  (operator `<%`, threshold 0.6): all eleven queries match;
- a `simple` tsvector over `aura.searchable_text`: nine match, but not the plural and not
  the typo.

Twenty-one words absent from the message scored at most 0.6 under `word_similarity`, and
the operator is a strict "greater than" (pgtrgm documentation), so none would match. A
prefix typed so far (`fatt`, 0.8) matches, which suits a box that searches as you type.

The search therefore uses `$1 <% content`, ordered by `word_similarity` and then by
recency. No migration is needed: the existing `gin_trgm_ops` index carries `%>`
(strategy 7), and the planner turns `$1 <% content` into that index condition. Telegram's
`/search` and the CLI run the same query and gain the same behaviour.

A turn over the 64 KiB cap stays unsearchable: its text lives in a sidecar file and the
column is NULL. Before this change that cost nothing, because a body that long could never
reach the old threshold. Now it is a real gap, bounded by the cap.

Verified the same day on the rebuilt stack: the same eleven searches all find the message.
Searching `Giulia` also returned this deployment's older conversations about her birthday,
which the old query had never surfaced.

Finding a word anywhere in a message exposed a second fault. The panel showed each hit from
its first character, in two lines of about thirty characters, so `fattura` was found but not
visible. The snippet now starts a few words before the first literal occurrence, cut at a
word boundary, and a fuzzy match (a typo, a plural) still shows the start. `aura chat search`
and Telegram's `/search` already cut theirs around the match.

This does not establish:
- ranking quality across many real conversations: the measurement used one message and a
  local database of test conversations;
- that a hit never shows a tool's JSON: tool turns were searched like any turn (seen for
  `Giulia`) until the next amendment left them out;
- speed on a large database: the index condition was read from `EXPLAIN` on 31 rows, not
  timed.

Search reads only the turns the thread shows as messages (2026-10-04). Measured on the
local database, 31 turns: six words returned 41 hits, 20 of them in `tool` turns, and all
five hits for `task` were tool turns (a skill's instructions, a scheduled task's
confirmation). The cockpit never shows a tool turn as a message: its text is the result
inside the assistant's tool card. A `system` turn is not shown at all. Tool turns averaged
4,191 characters against 32 to 58 for user and assistant turns, so they win hits by length.
The operator chose to exclude both, everywhere: the query keeps `role IN ('user',
'assistant')`, and Telegram's `/search` and `aura chat search` share it. A tool's output is
found only where the assistant repeated it.

Verified the same day through the cockpit on the rebuilt stack: the same six words returned
21 hits. The 20 that disappeared were all tool turns, the 21 kept were all user or assistant
turns, and the panel now shows nothing for `task`, where it had listed five tool outputs.

What this does not show: the cost at scale. The trigram index still covers tool text, so a
large deployment may read tool rows that the filter then drops; a partial index on the two
roles would avoid that if a measurement shows it matters. Still searched although the thread
hides them: the context envelope Aura wraps around a user turn sent with pinned knowledge or
attachments, and worker reports (assistant turns keyed `<uuid>:terminal`).

A search hit opens its thread on the match (2026-10-04). Measured on the local stack: in a
twelve-turn conversation with a word in its second message, clicking the hit opened the
thread at the bottom, with the matched message 3,645 px above the screen. The panel passed
the hit's seq, but the AppShell dropped it. assistant-ui keeps a thread at the bottom: it
jumps there when the history loads and follows every content resize
(`useThreadViewportAutoScroll`, 0.15.23), and it has no scroll-to-message API.

A thread opened from a hit now turns that off: `autoScroll`, the initialize jump and the
thread-switch jump. It finds the message carrying the seq, through the same `backendSeq`
mapping as the branch edit and the compaction marker. It centres that message, or aligns its
top when it is taller than the pane, and rings it for 2.4 s. The next run start hands the
viewport back to the library.

The viewport is remounted for every hit. An empty thread leaves the library owing a jump to
the bottom: it settles that only when a scroll reaches the bottom with content overflowing,
and it takes the jump on the next content resize whatever `autoScroll` says. Without the
remount, `e2e/search-open-at-match.spec.ts` lost that race in two runs out of three on
desktop Chrome. With it, the spec passed 22 of 22 runs on desktop and mobile Chrome,
including runs with four parallel workers.

Verified the same day on the local stack:
- the matched message lands in view (scrollTop 323 instead of 4162);
- a second click on the same hit, after scrolling away, returns there;
- a new message hands the view back to the bottom;
- an ordinary open still lands on the last message;
- a 1,230 px message opens at its top.

What this does not show: the `msg-N` ids are positions in the repaired history, not the
stored seq. They matched the stored seq on all seven conversations of the local database, but
a history the repair rewrote (orphan tool results) would shift them, and the branch edit and
the compaction marker with them. Not verified on WebKit or iOS.

A steer the cockpit cannot deliver keeps its text (2026-10-05). The composer empties on
submit, and a refused steer only removed its optimistic message, so its text was gone.
Measured on the local stack, with the browser holding requests the way a slow uplink would:
- A steer that reached its run after the run ended was answered `410 run has ended: message
  was not queued; send it as a normal turn`, up to 70 s after the end. The cockpit dropped all
  7 while telling the operator to send it as a normal message.
- The redirect control appears as soon as this tab starts a run, before `RUN_STARTED` gives
  it the run's id. With the run's POST held 3 s, all 3 redirects sent in that window were
  dropped without a request. Sending one as a new turn instead, as a first version of this
  fix did, collided with the run: 409, and the message was gone from the thread, with no
  error shown.

The cockpit now:
- sends a steer refused with 410 as the next turn, as the steering design's §4.2 has the
  client do, under the notice the server's own auto-delivery uses, and never steers at that
  run again;
- gives the text of any other refusal (400, 429, any other failure) back to the composer,
  ahead of anything typed since;
- gives the text back with "Aura can't take a redirect yet" when this tab's run has no id:
  while it starts, and through a resume or a re-run, which never report one.

Verified the same day on the rebuilt stack, nothing lost:
- 4 of 4 steers refused 410 went out as the next turn, the steer held 0 to 10 s past the
  run's end;
- 3 of 3 redirects sent before the run had an id came back to the composer;
- 5 of 5 redirects to a live run were accepted, and 8 back-to-back sends all arrived.

What this does not show: how wide either window is on a real network. Locally a run's id
arrived about 60 ms after its POST answered, and 25 timed sends without held requests hit
neither window. A 410 that arrives while this tab still streams the ended run starts the next
turn beside that stream's last frames; a component test covers it, the live stack did not. A
resume or a re-run still cannot be redirected at all. The first suspect, a cached
`live_run_id` outliving its run, did not appear in 8 rounds.

## 8. Long-term memory: facts and provenance

The production memory is Aura's Go MCP service over ArcadeDB. Other projects are
research references, not parallel runtime authorities. Reuse native graph, text and
vector facilities rather than duplicating engines.

Facts contain subject, predicate, object, statement, sources and validity. Entity
classification uses POLE (`Person`, `Object`, `Location`, `Event`, `Organisation`,
`Other`) with a finer kind where supported. Writers reuse the shared vocabulary.

The temporal contract is valid-time over retained records: inclusive `valid_from`,
exclusive `valid_to`. It is not full transaction-time history or reconstruction of
deleted entities. Malformed or unsupported temporal requests are rejected.

Actor/run provenance is host-derived. Model writes supply direct supporting memory
identifiers through the accepted source shape, without impersonating another run.
Replaying the same fact/source is idempotent; new sources enrich support. Removing a
source removes only its support and removes a fact only when none remains. Strong
identity erasure is separate.

The active correction key is not the identity of all historical versions. Precise
supersession addresses the intended current fact, closes validity and preserves
retained history. A database-local RID identifies evidence, not correction authority.

Batches use a stable identity-bound idempotency key, ordered operations and final-state
validation. A late invalid operation leaves live memory unchanged. Replay receipts
and whole-transaction retry cannot duplicate logical effects or cross identities.

Entity merge preserves attributable facts, sources and indexed relationships.
Forgetting and source/conversation/identity deletion propagate to derived records
according to ownership and retention authority.

## 9. Memory retrieval, graph paths and evidence classes

`memory_recall` is the unified deep-read operation, with semantic, recent, open, scroll
and explicit-reasoning modes. Exact facts, search, entities, digest, schema, paths and
diagnostics remain available through the current MCP surface. Deferral keeps them
discoverable. Tool schemas come from the actual server.

Facts and conversations are ranked independently with separate candidate bounds,
then composed by quota. Verbose turns must not crowd atomic facts out before
composition. One class cannot silently borrow another's allowance. Full evidence,
including provenance, counts toward context budgets.

Retrieval reports the backend used and evidence classes returned. Weak or empty
support produces explicit abstention. Embedding failure may degrade to a named bounded
lexical path, never masquerading as dense success. Thresholds belong to the measured
embedding contract and require recalibration when it changes.

Conversation hits hydrate bounded Postgres-authoritative windows. Host-derived active
source keys are suppressed where required. Paging makes strict progress, avoids
duplicate boundary turns and terminates. Edits/deletions converge through ordered
idempotent projection. Only committed user messages and accepted final assistant
answers enter ordinary projection; raw tools and reasoning do not.

Native temporal paths require:

- Exact endpoints, closed relation selection (`facts`, `mentions`, `combined`),
  direction and depth of 1 through 6; no arbitrary query interface.
- A nonzero RFC3339 `as_of` normalized to UTC.
- Admissibility checked during traversal, so an expired shortcut does not hide a
  valid alternative through post-filtering.
- Path and supporting facts returned in one query under REPEATABLE_READ, with
  returned validity, identity and provenance checked for consistency.
- Honest isolation: repeatable records permit phantoms, not a serializable graph snapshot.
- Stored-topology semantics without `as_of`; structural diagnostics reject temporal
  projection. Connectivity and coreness are not truth, corroboration or causation.

MENTIONS use a native link to the supporting FACT, unique by endpoints and record.
Legacy active-key links are reconciled without redefining history. Complete sweeps
consider retained historical facts; incomplete entity/fact/edge inventories perform
no reconciliation writes. Every neighborhood hop needs admissible support. Expansion
preserves direct evidence before indirect facts under a result cap.

The graph preflight counts all records loaded by native algorithms, including technical
records. `AURA_MEMORY_GRAPH_MAX_RECORDS` defaults to 10,000; engine memory guards and
client deadlines still apply. Partial output is marked or refused.

The cockpit's Graph page grows the graph in place (2026-10-04). Measured on the local stack
with 75 nodes drawn:
- selecting a node moved all 75 of them. The page built a new graph object on every render,
  and the canvas laid every node out again from random positions;
- expanding Giulia returned her one neighbour, which was already drawn. The page swapped the
  canvas for a loading line, laid everything out again, and showed nothing new.

Expanding now keeps the drawn nodes where they are (fCoSE's `fixedNodeConstraint`) and starts
the new ones at the expanded node. It marks the node's neighbourhood and says what it found:
how many neighbours are new, that all of them are already shown, or that the node has no
connections. A double click on a node expands it, as in ArcadeDB Studio.

Verified on the same stack:
- a selection, and an expansion that adds nothing, moved none of the 75 nodes;
- double-clicking a conversation added 4 turns 152–352 px from it, against a median of
  980 px for the other nodes, and moved none of the 75.

Indexed documents are left out of the unfiltered overview, by the operator's choice. The
overview reads edges first. Then, below the node cap, it reads each vertex type in
alphabetical order. Passage and IndexedDocument have no edges, so on a memory with few links
they took the free slots ahead of Person and the reasoning types. Their chips still select
them, and they are now named by their file name and the start of their text instead of
their type.

What this does not show: on this stack the conversation turns filled the 75 slots before the
alphabetical pass reached Passage. The reported case is therefore reproduced by a unit test,
not by this database. The 12 passages measured here were written with services/ingest's own
schema DDL and plain inserts, not by its pipeline: this stack's identity is a system
identity, which the ingest supervisor does not serve. Not measured on WebKit, iOS, or a touch
double-tap.

## 10. Memory authority, capture and reasoning

Memory is trusted identity-scoped knowledge under the current product policy. Its
prompt content is normalized and escaped so stored strings cannot close a fence or
forge template tokens. Instruction-shaped memory is remembered content, not a new
command. System instructions and the operator's current instruction take precedence;
the gateway remains the capability boundary.

Automatic capture accepts durable attributable user statements and reliable
allowlisted observations. Hypotheses, temporary instructions, secrets, unsupported
assistant prose and reasoning are not capture evidence. Accepted writes are serialized;
a bounded terminal flush establishes durability before completion is claimed.

Only authorized provider-exposed reasoning or summaries may be retained. Hidden
reasoning is not reconstructed. Traces, bounded/redacted tool observations and trusted
entity links form a separate projection. Ordinary recall, preload, compaction,
summarization and fact extraction exclude it.

Trace persistence never holds a turn's completion. The committed answer returns once the
trace is queued; one ordered writer embeds and stores it within 30 seconds, and a full
queue drops the trace with a warning. Source deletion waits for that conversation's
queued and in-flight traces before it removes the graph, so a write cannot outlive the
delete that started after it was queued; a trace queued once that delete has already
begun is not covered, exactly as with the former synchronous write. Graceful shutdown drains the queue; a crash
loses what was queued, as a failed write did before. Measured 2026-09-14 on the appliance:
the longest stored trace (2,289 runes, 561 tokens) embedded in 1.45 s inside turn
completion, and a 2,048-token input takes 5.7 s, past the former 5-second window that
also covered the graph write, so such a trace was lost whole. Only three traces existed,
so production loss frequency is not established.

Reasoning retrieval requires an explicit selector. Successful traces retain for 30
days; failed/cancelled traces for 7 days. Source, conversation, identity and operator
deletion override TTL. Retrieval does not renew it. Concurrent expiry/deletion must
converge: a stale vertex deletion may restart the rolled-back transaction and select
roots again, with bounded retries and no generic missing-resource error suppression.

Digest and relevance preload are bounded and observable. A count capped by `limit=1`
is not the graph's entity total. Conversation-only retrieval must not disappear because
a renderer only supports facts; included anchors carry role/date and are labeled as
something said, not promoted to asserted facts.

Period recall reads the projected user/assistant turns in a half-open interval
(`from` inclusive, `to` exclusive), supplied as RFC3339 instants with the user's
timezone offset. `memory_recall(mode:period)` orders by occurrence, conversation
and stable sequence, and returns bounded pages with the existing opaque cursor
transport. `scroll` continues that interval without dropping or repeating its
boundary turn. Identity ownership, source deletion and host-derived active-chat
exclusions apply before pagination. No reasoning or fact-validity selector enters
this read. The agent groups related chats when composing the answer, preserving
distinct attempts, their outcomes and source references; the store never merges
conversations because their wording is similar.

Measured 2026-10-06 on VM 192.168.101.158, ArcadeDB 26.10.1: the operator's
2026-10-05 Europe/Berlin interval contains 48 projected turns across 9 conversations.
The exported agent run used `recent(limit:30)` (capped at 20 anchor rows), saw 6
conversations and omitted the morning chats. The database's native parameterized
date range and chronological ordering return all 48. This establishes the missing
retrieval contract, not projection completeness against PostgreSQL, automatic topic
clustering, or WhatsApp delivery. Full-day claims require exhausting the cursor;
projection lag remains a limit of every memory read.

Verified after deployment on the same VM, 2026-10-06: the unchanged yesterday
question, run by `gpt-5.6-sol`, used `period` for the local 5 October day and returned
all 48 unique source references in 9 chats, exactly matching the native query,
without a next cursor. The answer recovered morning topics, preserved distinct
attempts and explicitly separated stored messages from external delivery proof.
The real-agent acceptance rubric passed 10/10; scope and reproduction are recorded
in [the appliance verification](docs/verification/2026-10-06-memory-period.md).

## 11. Documents and media

Identity-bound originals live in Garage. The ingestion supervisor resolves existing
provisioning bindings and manages CocoIndex workers. Live refresh actually rescans
sources. Add/modify/delete reconciles cards and passages; source identity and scope
agree between Python writers and Go readers.

Extraction and supported format normalization feed token-bounded chunks. The current
EmbeddingGemma contract is 768 dimensions with a 2,048-token input ceiling, including
prefixes and special tokens. Model artifact, dimension, input format and fingerprint
agree across installation, ingestion, memory and CI.

The Go embedding client fits every input to the selected model's input limit, so no
single text can fail its request or the batch around it. The limit comes from the route's
own catalogue, as the LLM context window does: llama.cpp `meta.n_ctx`, or the lower of
OpenRouter's `context_length` and `top_provider.context_length` for the named embedding
model. A limit that cannot be read fails the call instead of sending blind. Locally, an
input whose UTF-8 bytes plus two fit is sent unchanged; a longer one is tokenized by the
sidecar and, when still over, sent as its leading token IDs plus the closing special
token. The cloud route has no tokenizer and cuts at the limit minus two bytes on a UTF-8
boundary. Requests are also bounded to 4,096 estimated tokens unless one input alone
exceeds that. The sidecar's physical batch equals its context, because llama.cpp refuses
an embedding input above `n_ubatch` and publishes only `n_ctx`.

Measured 2026-09-14 on the appliance sidecar (llama.cpp b10951, Vulkan, `-c 2048 -ub 2048`):
2,048 tokens including BOS/EOS returned 200 in 5.7 s and 2,049 returned HTTP 500 at once;
a batch holding one 6,142-token input failed whole; token count never exceeded UTF-8 bytes
over 3,219 adversarial and prose strings; token-ID input matched string input at cosine
1.0, as did head-token and detokenized truncation. On OpenRouter, `thenlper/gte-base`
(DeepInfra) silently truncated to 512 tokens while `qwen/qwen3-embedding-8b` rejected
about 37k tokens with HTTP 400, batched or alone. The byte bound is proven only for
EmbeddingGemma's tokenizer; a cloud cut drops text a truncating provider would have kept;
timing covers the appliance GPU, not a sidecar queue shared with ingestion.

Changing the embedding model (design `docs/superpowers/specs/2026-09-23-embedding-model-change-design.md`,
five plans; E2E on the lab VM 2026-09-24, below). Every stored vector carries the space that produced it
(`es1-<hash>` of route, model or attested local artifact, width and recipe). A family,
memory or documents, is served densely only while none of its vectors is in another space;
otherwise it answers lexically with `embedding_space_mismatch` (documents: `lexical_only`),
and a scheduled pass and the ingest re-embed the rows until the gate opens. The route
changes only through the cockpit: `PUT`/`DELETE` of `AURA_EMBED_MODEL`, `AURA_EMBED_BASE_URL`
and `AURA_EMBED_CLOUD_BASE_URL` answer 409; the embedding card previews the target space,
width, speed, corpus to re-embed, cost and input limit through one synthetic batch, and its
confirmed apply restarts the daemon. Preview and apply are an admin's (`identity.create`);
the apply writes only the rows the environment does not already name and deletes the
others, so a route set back to local follows compose again. `arcadedb-mcp` re-reads its
route every minute and restarts itself when it moved; the ingest supervisor restarts its
children within one poll; the memory pass fires at the daemon's boot. `aura doctor` warns
with every tenant and family whose gate is closed, and the cockpit's Embedding space panel
shows the per-family counts and the files still in another space (a member sees only their
own tenant).

Measured on the lab VM (2026-09-24, one tenant: 100 memory rows, 43 passages, 13 cards):
local → `qwen/qwen3-embedding-8b` → `perplexity/pplx-embed-v1-0.6b` → local, each through
the cockpit API. `arcadedb-mcp` restarted itself on every change, 30-65 s after the apply,
on the new space. On pplx-embed (0.6B, native 1024 truncated to 768, $0.004/M; preview
$0.00038) the documents family was dense again in ~2 min against a 122 s estimate. Memory
waited 5 min there because the scheduler's boot catch-up skipped the kicked pass (fixed
`020dc7391`); back on local, with the fix, memory re-embedding began within 41 s of the boot
and both families were dense 4 min 22 s after the apply, with no `AURA_EMBED_*` row left.
First abstention datum for pplx-embed, on plan 4's 15 queries with EmbeddingGemma's floors:
6 of 6 out-of-corpus questions abstained, and 3 of 9 in-corpus ones did too, against 2 of 9
on the local model ("prompt engineering intelligenza artificiale" is the extra one). Taking
it needed `aura docs search` to embed on the cockpit's route and key (fixed `816a1f6f3`):
before, every CLI query read the re-embedded library as another space's.
This does not prove: the preview's duration on a hosted route (one request measures latency;
the same qwen probe gave 758 and 184 chars/s), a library larger than the VM's, several
tenants, or relevance with a cloud model — its floors stay uncalibrated.

**Upgrade note.** The release that ships the stamps finds every existing vector unstamped,
so memory and documents answer lexically until they are re-embedded on the running route:
the memory pass runs at boot and every five minutes, and every document is re-extracted and
re-embedded once (measured on the lab VM, 2026-09-24: 43 of 43 passages and 12 of 12 cards
stamped within the deploy's first cycles). A document that keeps failing keeps its old rows
and holds the documents family lexical; the panel names it. This does not prove: the
re-embed time of a library larger than the VM's, the relevance floors of any model but
EmbeddingGemma (every dense answer on another model says `uncalibrated_floors`), or why a
file failed — CocoIndex reports ingest failures as counts, so the panel shows the tenant's
error count beside the file names, and the reason is in the ingest log.

Long-document extraction must retain the configured Tika builder result and reject
reported write-limit truncation. The 2026-09-09 ArcadeDB manual baseline indexed only
23.203% of its non-whitespace text despite a successful reconciliation. Oversized
passages should reuse the native recursive splitter before fixed-width fallback,
preserving overlap and absolute locators. Timings must compare equal source coverage;
see [the measured decision](docs/document-ingestion-benchmark.md).

For direct operator diagnostics, expose the existing `aura docs ingest` and
`aura docs search` handlers through a stdio MCP (`aura docs mcp`), using the
installed MCP SDK. On 2026-09-09 the CLI search returned complete passage evidence
in 113 ms, while the configured memory MCP exposed neither document operation.
The MCP fixes the operator identity at startup, preserves ingress path checks and
returns complete retrieval JSON without an LLM synthesis step. MCP connectivity
and client reload must be verified separately from CLI behavior.

Operator-selected document search/open results retain their source provenance
without an `untrusted` envelope. For every tool, the agent must check truncation
and read the remaining output through `read_tool_output` before answering; the
shared truncation footers carry the same instruction and continuation coordinates.

Documents retain original hashes; passages retain normalized hashes, locators and
citation tokens. Retrieval checks source scope and reports which legs ran. An absent
embedder, unavailable index or missing passage configuration has explicit degraded
status. Unsupported extraction cannot imply content coverage.

`document_search` supplies bounded attributable passages and openable references.
`document_open` supplies original bytes for calculations, aggregates, conversion or
insufficient passage evidence. A few passages cannot establish a whole-table aggregate.
A filename match is a diagnostic, not the answer-quality oracle.

The primary model receives supported media natively where its route advertises that
capability. OCR/transcription/extraction use shared services and settings. Telegram is
an attachment wrapper, not a competing pipeline. Runs that need indexing wait for the
relevant indexed state and show it to the user. Filename prose is not image transmission.

Video is indexed by metadata only, for now (operator, 2026-09-24): an `.mp4` or `.webm`,
the two formats the asset layer calls video (`internal/assets/limits.go`), gets its card,
name and size in the library and no transcript. Measured the same day on the lab VM, after
the embedding-space deploy re-extracted every document: all seven copies of one 3-minute
`videoplayback.mp4` failed every cycle. Speech-to-text (faster-whisper on CPU, beam 5)
outran its 120 s request deadline (`MULTIMODAL_TIMEOUT_SEC`), each failure was retried
twice more with the whole audio while the server kept transcribing the abandoned request,
and seven copies re-extracted at once left `aura-stt` at 232% CPU and unhealthy. The
failing files kept their old rows, so the documents could never all reach the new
embedding space. Audio files are still transcribed. Not measured: how long one
transcription of that file takes on an idle sidecar, and whether the seven ran strictly in
parallel. An audio-only `.webm` loses its transcript too: the ingest and the card read the
extension, not the MIME type. A video whose transcription already succeeded somewhere keeps
it until its bytes change: the extraction memo is fingerprinted on `_extract`'s own code,
not on the `media.py` it calls (cocoindex `_compute_logic_fingerprint`), and invalidating it
would re-run every document's extraction, vision calls included.

**Deleting an asset.** A deleted asset leaves `aura.assets`. The delete first stamps the row
`deleting` with `deleted_at`, which hides it from every identity-scoped read and from every
status write, so a job finishing late cannot bring it back. The object is then removed from
the owner's bucket, a missing key counting as removed, and the row is hard-deleted; its
`asset_events` and `ingestion_jobs` cascade, and since migration 0133 so do the jobs'
`ingestion_events`. A row a `media_job` points at stays as a `deleted` tombstone instead, because a paid clip
keeps its pointer (migration 0128 gives that key no `ON DELETE`). When the object removal or
the row removal fails, the row stays `deleting` and a warning names the asset; the asset API
still answers success. The daily `retention_sweep` visits every identity, takes its oldest
`deleting` rows in a bounded batch, deletes the object, finalizes the row and retries the
rest the next night. The file manager's delete does the same for the row that holds a key
it removes, and still reports a key it could not remove. No migration is needed for the
hard delete: 0001's default privileges already give `aura_app` DELETE on every table
`aura_migrate` creates, and 0020's narrower grant never revoked it (the lab VM reads
`aura_app=arwd/aura_migrate` on `aura.assets`).

Measured 2026-09-28 on the lab VM, read-only: 258 rows were `deleting`, every one with
`deleted_at` NULL, against 19 `accepted`, 26 `complete`, 15 `processing` and 7 `presigned`.
The delete set `deleting` and nothing ever took a row further: `MarkAssetDeleted` had no
caller, and a failed object removal was discarded without a log. All 258 belonged to one
identity and were created between 2026-09-27 17:48 and 2026-09-28 07:42 UTC, mostly E2E
fixtures (`clip-a.mp4` 87, `project.json` 68, `music.wav` 51); 108 video, 79 document, 71
audio. None was referenced by a `media_job`; 99 had an `ingestion_jobs` row and those jobs
206 `ingestion_events`; no row had `asset_events`. Read from the code, not measured: with
`deleted_at` NULL those rows still came back from the thread listing, whose web consumers
drop only `deleted` and `canceled`, and a library file, whose object key is fixed by its
name, could not be presigned again while its old row held the key. The sweep, brought
forward on the operator's request, ran at 09:53:15 UTC: "swept 331 item(s)" (the backlog had
grown from 258), and `next_run_at` returned to the cron time by itself.

**Abandoned uploads.** A `presigned` row untouched for the upload URL's lifetime
(`AURA_ASSET_PRESIGN_TTL_SEC`, 600 s by default) plus one hour is abandoned. The URL can no
longer start an upload, and the hour covers one that began just before expiry and may still
be streaming: the largest cap, 100 MiB, at about 29 KB/s. Every web client finalizes as soon
as its PUT returns, and every server-side ingest puts its bytes right after creating the row,
so nothing legitimate waits longer. The same `retention_sweep` marks those rows `deleting`
with `deleted_at`, oldest first and bounded per identity, and finishes them like any other
delete. `created` is written by no code path. An `uploaded` row strands only when finalize
fails after the upload is recorded. Neither is swept: the VM held none of either.

Measured 2026-09-28 10:15 UTC on the lab VM, read-only, after that sweep: 7 `presigned` rows,
all `web`, all of one identity, none touched since creation, aged 10 h 17 m to 6 d 19 h. Five
are `photo-edited.png` from the photo editor's save; their objects are in the bucket (12,451
bytes each, PUT and never finalized). Two are videos whose object was never written. No
`media_job`, `ingestion_jobs` or `asset_events` row references them. There are 0 `created`
and 0 `uploaded` rows, and the VM runs the 600 s TTL.

**Settling a processed asset.** The document processor names an upload, and the document leg
of an image, the way the ingest sidecar will index it. It used to report `processing`, and
nothing moved an asset on from there: the sidecar indexes from the bucket and writes no
Postgres row. It now reports `complete`: the asset pipeline's part is done. Searchability
stays ArcadeDB's answer, asked by the knowledge catalog, and the cockpit already treated
both statuses as ready. Migration 0132 settles the rows left behind, only those whose
`asset_process` job succeeded. Measured 2026-09-28 10:51 UTC on the lab VM, read-only: 3
`processing` rows (an image from 2026-09-21, two `project.json` from 2026-09-27), each over
an `asset_process` job that had succeeded. The 15 counted that morning were not checked
against their jobs before most of them were deleted.

**`searchable` and `embedding` are not asset statuses.** No code writes either to
`aura.assets` since the in-process pipeline was deleted. The Go constants had no writer and
`StatusEmbedding` had no reference at all, yet both values survived in the CHECK, in a
`searchable_at` column, in the Studio's asset filter and in the cockpit's status union.
Measured 2026-10-04: the local database held `accepted` 9, `complete` 29 and `presigned` 5,
and no row in either status. The live deployment held 0 `searchable` on 2026-08-13, the
last time it was counted. Migration 0136 drops both values and the column. It refuses to run
while any row still holds either status rather than guess what that row should become. The
cockpit's union also listed `indexed` and `recovered`, attributed to the retention sweeper;
the CHECK has never admitted them, and nothing writes them. What this does not show: the
state of appliances not measured here, which is why the migration checks instead of
assuming.

**A library upload that reuses a name replaces the file.** A library object's key derives
from the file name (`libraryObjectID`), so that ingesting `report.pdf` twice replaces
`report.pdf`. Agent and CLI ingest honour that through `reingestTarget`. `POST
/api/assets/presign` with `scope: "library"` did not: it inserted a new row on the same
key. Measured 2026-10-04 on the local stack, the second presign of a library name answered
400 with Postgres's text, `duplicate key value violates unique constraint
"assets_identity_object_key_idx" (SQLSTATE 23505)`. Its unit test passed because the fake
store does not enforce that index. The presign now re-arms the row already holding the key:
it goes back to `presigned` with the new upload's name, type and declared size, so finalize
checks the new bytes against the new declaration and not against the old file's size. A name
whose row is being deleted answers 409 rather than racing the delete. No cockpit screen
sends `scope: "library"`: chat, Studio and Video Studio presign into a thread, and the Files
workspace writes objects directly. What this does not fix: the processing job keeps its
idempotency key (`asset_process:<id>`), so a replaced document stays `accepted` and is not
reprocessed, as with agent re-ingest. Its passages are re-indexed by the sidecar, which
watches the bucket key.

**Refused and failed uploads.** A `refused` or `failed` row records an outcome, not content.
A refusal removes the bytes, and a failed row is retried within minutes by its job or once
by hand from the chat chip. The same sweep therefore marks both `deleting` once untouched
for the deployment's metadata-trace lifetime (`AURA_RETENTION_METADATA_TRACE_HOURS`, 14 days
by default), and finishes them like any delete. No code writes `canceled` for an asset, so
it is not swept. A refusal whose object removal fails now logs a warning naming the asset.
The refused row keeps the key, so its retirement removes the object. An ingestion job's
`ingestion_events` now leave with the job (migration 0133, both keys on `job_id` `ON DELETE
CASCADE`). Every writer names the job, no query reads a gone job's timeline, and
`aura.audit_logs`, not this table, is the one kept forever. Measured 2026-09-28 10:51 UTC on
the lab VM, read-only: 0 `refused`, 0 `failed`, 0 `canceled` rows, so the rule is from the
code and not from an observed pile. 384 of 406 `ingestion_events` had `job_id` NULL, all
written since 2026-09-27 17:48. The cockpit offers a retry on a `failed` chip only.

**Moving a file.** The file manager's move and rename are copy-then-delete. Between the two,
the asset rows of every key they relocate now move with it, folders included, in one
identity-scoped transaction limited to the bucket written. A rename also gives the renamed
file's row its new name, because the listing shows the row's name. A copy is a new file,
not a new upload: it gets no row, and the original's row keeps its provenance. A move,
rename or copy onto a key any asset row holds, in any status, is refused before anything is
copied. It would otherwise replace that asset's bytes under its row, or hand them to the
sweep removing a deleting row's object. Measured 2026-09-28 11:52 UTC on the lab VM,
read-only: the bucket held five objects (`prd.md`, two under `media/`, two under `chat/`),
and 16 of 20 live rows named a key with no object. Nothing had been moved there, so those
rows lost their bytes to deletes, not to moves.

This does not prove:
- that the bucket holds no objects without a row;
- why 16 live rows name a missing object. Until 2026-09-28 the file manager's delete left
  the row live, which would produce exactly this, but the cause was not established and the
  rows are not repaired here (reported);
- that a copy's bytes are indexed as a document: the ingest sidecar reads the bucket, but no
  row names the copy for the chat's knowledge catalog;
- that Garage removes each of the 258 objects: nothing was deleted by the measurement;
- that the document index drops the passages of a deleted document (the ingest reconciler
  owns that);
- that a chip retry of a `failed` asset works once its job is exhausted: `Retry` re-queues
  through the job's idempotency key, which returns the finished job, so the asset sits in
  `accepted` (read from the code, reported, not fixed here);
- why the photo editor's five uploads were never finalized (reported, not investigated);
- whether Garage lets a PUT keep streaming past its URL's expiry: the hour assumes it can;
- anything about a `processing` row with no job, which an inline Telegram ingest interrupted
  mid-processing would leave: 0132 leaves such a row alone.

## 12. Workspace, shell and web

Tools and artifact delivery resolve the persistent working root consistently. Sandbox
materialization and original-object access cannot expose another identity's files.
Container paths, host staging and user-facing attachments are distinct.

Delivered HTML artifacts have an inline preview with filename, source/preview and
expand controls. The expanded workspace retains conversation navigation, offers a
resizable source/preview split (stacked on narrow screens), copies original source
and downloads the delivered file. Closing returns to the originating chat without
remounting its runtime. The same card is used for live deliveries and saved-message
attachments. Rendering reuses the authenticated sealed document route and an
opaque-origin iframe; source highlighting never executes artifact markup.

The multi-track video editor is one dense full-screen workspace, not a preview,
inspector and timeline stacked as unrelated blocks. Measured 2026-09-21 against the
live Clideo editor at 1,920 x 945 and 1,100 x 599: a 54--58 px project bar spans the
top, a 70--76 px tool rail continues beside both canvas and timeline, the contextual
property panel occupies about 34% of the 1,100 px viewport and caps near 443 px, the
preview always fits without cropping, and the timeline consumes the lower third while
excluding only the rail. Aura keeps its existing command/project model, local media,
44 px edit targets, translated refusals, save and export semantics; the reference is
a workspace and interaction-density contract, not a claim of Clideo feature parity.
Implementation ownership is `web/src/videoStudio/` plus
`web/src/styles/video-studio.css`. The same measurement does not establish long-project
performance, real-phone touch behavior, or support for Clideo-only stock media,
effects, subtitles, recording and generation tools.

Every video `Edit` entrance opens that workspace directly; the retired single-clip
surface is not a second editor. A selected clip exposes the measured Clideo control
families in one contextual panel: transform/crop/flip/rotate, VideoFlow transition
presets, opacity and colour adjustments, audio, playback speed and source timing.
Animations belong to a clip edge. A transition belongs to the junction between two
adjacent clips: it overlaps their project-time windows, is selected from the junction
node on the timeline and has its own duration panel.
Those controls use the installed VideoFlow renderers, dnd-timeline and shared
Radix/shadcn primitives. The photo editor and its save-to-library path remain unchanged.
The source-available VideoFlow React editor and the React-Native-only 100ms virtual
background plugin are references, not shipped dependencies; the dependency and license
decision is recorded in
`docs/superpowers/specs/2026-09-21-video-studio-clideo-controls.md`.

The audio lane was measured on 2026-09-27 with throwaway probes (`spikes/video-studio-audio/`)
before any product code was written.

**VideoFlow volume (S1).** VideoFlow's mixer applies volume only from a layer's compiled
`animations`. It reads keyframe times as absolute source seconds: a keyframe at source 3.001 s
lands at 1.0 s on a clip trimmed by 2 s, and at 0.5 s at speed 2. It steps between keyframes and
starts from a gain of 1. It ignores a static `volume` on audio and video layers alike, so the clip
Volume slider has been a no-op on clips without transitions. The compile therefore writes every
volume curve into `animations`, beginning at `sourceStart`.

**VideoFlow opacity and media (aura-video-mcp spikes S1.3 and S1.4, 2026-09-30).** Measured on the
lab VM: a clip's fade-out frame read RGB (233, 100, 90) where opacity 0.4 over black allows 102
(S1.3), and a source behind a refused fetch came out as a black, silent 26,604 B MP4 that the export
reported as a success, with every source fetched twice, once by VideoFlow's media cache and once by
the pre-decode (S1.4). Read in VideoFlow 1.3.4's source: handed a keyframe array as a layer's
initial property, `compile()` stores the array as the value of one step keyframe and writes it out
as a static property (core `dist/VideoFlow.js:629-638`, `:856-864`); the renderer unit-converts each
keyframe object in it to the number NaN (renderer-browser `dist/layers/RuntimeBaseLayer.js:311-323`,
`:472-494`), and a NaN opacity draws as full opacity (`dist/LayerRasterizer.js:909`). VideoFlow
disables a layer whose media fails to load, logs a warning and lets the export resolve
(`dist/BrowserRenderer.js:427-445`). The rules: the compile is to move every keyframed property into
the layer's `animations`, on its source clock, as volume already is; a clip's fade is to last half a
second of film at any speed; the export is to load each source once, through VideoFlow's
`loadedMedia` cache, and to write no file when a layer was disabled or a sound that is not muted
will not decode, naming the source by its file name and saying whether its bytes never arrived or
did not play.
Measured after the fix on 2026-10-02 on the lab VM (image `b31109815`,
`web/e2e/video-studio-export.spec.ts`): against its twin 4 s earlier, a clip's fade-out frame at
7.8 s now keeps 0.4040 of the level, where a control clip drawn at a static 0.4 keeps 0.3980 and
the fade is due 0.40 (the image before the fix kept 0.9994); the frame before the fade keeps
0.9993. The export now fetches a source the Stage already holds 0 times, where the image before
the fix fetched it once. A local run of the same spec gave the same six numbers: the three levels,
the due level, the frame time and the fetch count. When a Playwright stub answers 404 for a
source's bytes, the export now stops with the sentence that names the source's file and downloads
nothing, and so does a source whose bytes the renderer cannot read (`unplayable.mp4`).

This does not establish:
- that title fades and the fade-to-black and fade-to-white washes are lost on an exported frame:
  they take the same path, but that was computed on the renderer's own runtime layers in a unit
  test, not measured on a frame;
- a clip's fade-in, or a fade at a speed other than 1×, on an exported frame: the E2E measured one
  muted fade-out at 1×, encoded and decoded by one Chromium;
- the HTTP status of a failed fetch: VideoFlow keeps it in its console warning and the page never
  sees it;
- what an expired presigned URL or a 403 does: S1.4 exercised only a missing CORS rule, and the
  E2E after the fix only a stubbed 404;
- the Stage preview on screen: it compiles the same JSON and builds the same runtime layers
  (`@videoflow/renderer-dom` imports them from renderer-browser), but only the export was measured;
- anything about a video whose own audio will not decode: the mixer drops that audio, so such a
  clip is to export silent, with no error.

**Waveform (S2).** wavesurfer 8 draws the waveform from cached peaks inside a dnd-timeline item in
10–15 ms, and redraws after a zoom in 104–136 ms. Envelope points drag without moving the item,
provided four conditions hold:
- presses on an envelope `<ellipse>` are stopped by composed path;
- the waveform is created only after its host has a width;
- it is mounted over the full item box, not `itemContentStyle`;
- strokes use `::part` non-scaling.

**Noise reduction (S3).** RNNoise from `@sapphi-red/web-noise-suppressor` lowers the noise floor
of the 10 dB SNR fixture by 37.7 dB for at most 0.5 dB of speech, faster than real time, with a
20.67 ms delay. GTCRN manages 14.9 dB and Speex 4.5 dB. The worklets must be given time to
instantiate their WASM before an offline render, or they return silence.

**Speech detection (S4).** WebRTC VAD (`fvad-wasm`, mode 3) run on the RNNoise output places every
speech edge within 95 ms on both fixtures and adds 35,491 B to the dist. Silero via `vad-web` ends
0.28–0.88 s late and would add 30.2 MB.

**Uploads and saved projects.** Landed in T1a (`cdfdae5c5`) and measured on the lab VM on
2026-09-27: the Studio's media uploads use `POST /api/assets/{id}/finalize?use=media`, and
`FinalizeMedia` accepts only image, video and audio and enqueues no processing. A WAV finalized
that way sits under `media/`, stays `accepted` with an empty summary, while the same file through
the plain finalize is processed; a PDF sent to the same door is refused with 400. After the final
review the door also refuses a document whose client hinted it as a sound: the recorded modality
can be the client's hint, so the name and declared type must infer an allowed modality too. A saved
project, by contrast, is finalized as a document today and receives a `document_id` (M1): that was
reported as its own issue; its rule follows this section's details. The door reads no bytes: a
file named and typed as a sound whose content is something else is accepted as a sound.

This does not establish:
- long-project memory use;
- VAD or denoise quality on real noise, music under speech, babble or other languages (the fixtures
  are synthetic, one voice at 10 dB SNR);
- behaviour on Safari or a phone CPU;
- the MP4 mux or the server render (sub-project 2);
- whether the saved project's chunks reach ArcadeDB.

Details: `docs/superpowers/specs/2026-09-27-video-studio-audio-design.md`.

**Saved Studio projects and the document index (M1; aura-video-mcp Plan A).** Measured 2026-10-01 on
the lab VM, read-only: 2 live JSON assets with a `.json` key, 2 of them named as documents, none
at a key the file manager named: every key is `chat/<uuid>.json`, with no file name in it, and in
none of them is the uuid the row's own id; it is the one Presign minted for the object. 2 rows look
like Studio saves by the row rule below, and the index holds the keys of 2 of them
(`IndexedDocument` rows, read with the ingest's own `arcade.indexed_source_keys`). The ingest's
matcher reads only the object key (CocoIndex 1.0.24 `connectors/amazon_s3/_source.py:304-317`), and
its audit lists keys without reading a single object's metadata (`services/ingest/source.py`
`expected_keys`), so a project can be told apart by its key and by nothing else. The rules:
- the Studio is to name a project `<slug>.aura-video.json`, and the object key is to keep that
  suffix whole (`objectstore.StudioProjectSuffix`);
- the ingest is to exclude `**/*.aura-video.json` from its walk and from its audit, and to keep
  indexing every other `.json`;
- a project saved before the change is to be moved once, at boot, to `chat/<uuid>.aura-video.json`
  through the file manager's own transfer, keeping its row and its id. Only a row that is a cockpit
  upload with no thread and thread scope, a JSON document named `<slug>.json` at `chat/<uuid>.json`
  in the file manager's bucket, at most 4 MiB, whose bytes have the Studio's typed project shape,
  is moved. Saving again is no remedy: every save is a new asset;
- no code is to read an asset id out of an object key.

Measured after the fix on 2026-10-02 on the lab VM (image `b31109815`): at its first start on that
image the daemon moved 2 of the 2 candidates and left none; the index held both of their old keys
before, and now holds none of their keys, old or new. Its next start, on image `851f32dec` at 09:32
UTC, met no candidate: `studio project re-key done moved=0`, with no row moved or left where it
is. A project saved through the cockpit's upload door as `plan-a-probe.aura-video.json` now gets
the key `chat/3bd55ef5-2c38-48cf-a1a0-85727bc727df.aura-video.json` while its asset id is
`5aed9fc8-73a0-4117-b60b-6dc89c3f89b4`, and two ingest cycles later the index holds no row for it,
while the operator's `plan-a-control.json` saved beside it is indexed; both were deleted afterwards,
and two cycles later neither their rows nor their keys were left.

This does not establish:
- that the boot pass would refuse every JSON an operator saved that is not a project: on the VM it
  met the 2 candidates above, and the other shapes were shown by the unit tests;
- that a later boot finds nothing to move: the second boot met an index with no pre-change save
  left, so it shows the moved rows no longer pass the row rule, not how the pass treats a `.json`
  saved after the change by a cockpit tab still running the old bundle;
- that the counts hold beyond their sample: before the fix they are 2 rows of one identity on one
  VM, both named `project.json`, and after it one project and one control the probe saved;
- anything about a project an operator renamed in the file manager before the change: Rename
  writes the typed name into the key (`internal/assets/filemanager_ops.go` `Rename`), so its key
  fails the `chat/<uuid>.json` test, and it stays indexed until it is deleted or renamed to end in
  `.aura-video.json`;
- that the asset stops receiving a `document_id`: `DocumentProcessor` names one for every document,
  and the knowledge catalog asks ArcadeDB whether a document is indexed
  (`internal/assets/document_processor.go:11-33`).

Measured 2026-09-08 using an agent-generated weather demo: the accepted HTML asset
renders inline and expanded, its button executes, and showing source preserves
the preview state. This is a viewing/export workflow; it does not establish direct
editing, version history, React bundling or arbitrary external network access.
Acceptance and verification details: `.planning/artifact-workspace-plan.md`.

The chat composer remains anchored to the workspace bottom; only the transcript
scrolls. HTML preview fetch/XHR may reach any HTTPS origin: `connect-src` is `*`,
and there is no operator allowlist. `AURA_ARTIFACT_CONNECT_ORIGINS` was retired on
2026-09-09 along with its config field and compose plumbing.

What replaced it is the CSP `sandbox allow-scripts` directive (never
`allow-same-origin`, which would let the document drop its own sandbox), carried by
the response header because a meta policy ignores `sandbox`. The document therefore
holds an opaque origin, so a call to Aura's own API is cross-origin and carries no
session cookie. Measured against Chromium on 2026-09-09: a top-level artifact tab
without the directive read `/api/secret`, received HTTP 200 with the operator's
cookie, and shipped the body to an external origin by both fetch and an `<img>`
beacon; with the directive the same fetch fails as cross-origin. Framed previews
were already opaque through the iframe's own sandbox attribute — the unframed path
was the gap, and it is what the allowlist had actually been standing in for.
Verified end-to-end on a running stack: inside a real rendered artifact a live
`api.coinbase.com` fetch succeeds while `/api/me` is refused.

This does not enable remote scripts, forms, or same-origin access: only data
connections are opened, and every other directive keeps the sealed floor. It does
NOT prevent an artifact sending its OWN contents outward — that channel is open by
design and is the accepted trade for artifacts that update themselves. It also does
not make every API reachable in practice: the browser still discards a response
whose origin sends no CORS headers, which is why `query1.finance.yahoo.com` (200,
no `Access-Control-Allow-Origin`, measured 2026-09-09) cannot back a self-updating
artifact while `api.coinbase.com`, `api.coingecko.com`, `api.binance.com` and
`api.open-meteo.com` can. The shared MCP CSP validator still rejects malformed
origins and removes the cockpit host across ports for MCP views, whose connect
domains are declared by a mounted server rather than the operator; the wildcard is
opt-in (`ViewPolicy.AllowConnectWildcard`) and only the artifact renderer sets it.

Package caches (uv, npm and pip) belong to the identity just like its workspace.
They survive suspension and are removed when that identity is deprovisioned. A box
with older shared cache mounts must be recreated before reuse, preserving its
workspace and starting with empty private caches. Shared cached packages must never
seed a private cache. Measured 2026-09-14 on the appliance sandbox image: B changed a
cached wheel and A's next pip install by name/version reused and executed it. The
disposable proof covers cache reuse, not the whole authenticated adversarial suite.

The per-identity sandbox image includes Python Playwright 1.62.0, its matching
Chromium and system dependencies. HTML delivery instructions require browser
validation before `send_file`: console/page and HTTP/network errors, primary
interactions and screenshots at desktop/mobile sizes under preview constraints.
The image contract launches the actual browser without network, executes a button
handler and captures a PNG. The active sandbox was also measured loading the real
weather artifact's seven forecast cards and changing its selected day. Browser
validation is an agent instruction and available capability, not an automatic
server-side rejection gate for every file delivery.

**Authenticated browsing reuses agent-browser, not a bespoke connector.** The box carries
`agent-browser` 0.38.1 (Vercel, Apache-2.0; npm tarball and native binary sha256-pinned), the
engine under Hermes Agent's ~14.7k-LOC browser stack. Measured 2026-09-26 against a disposable
login fixture (password → TOTP → HttpOnly cookie → protected PDF), first natively and then in an
`aura-sandbox` box that Aura's own `DockerBackend` resolved, suspended, resumed and recreated
behind the egress floor (runc): the accessibility snapshot, the credential vault (`auth save
--password-stdin` / `auth login`), TOTP entry, protected download, encrypted session restore and
a human login driven only through its viewport stream all worked without a new component
(`spikes/agent-browser-auth/FINDINGS.md`). Four constraints came out of the box, not the README:

- the state key is read by agent-browser's daemon at spawn, and `Exec` scrubs secret-named
  variables, so a key passed as env never arrives and the vault mints `.encryption-key` beside its
  ciphertext. Aura therefore delivers a per-identity key as a 0600 file outside the workspace
  volume on every `Resolve`, and the in-box entry point refuses to run without it;
- `Suspend` stops a `sleep infinity` PID 1 with a 2 s grace, so the browser is SIGKILLed: a login
  younger than the default 30 s autosave was lost, a 2 s autosave kept it;
- state under `HOME=/root` died with a box recreate, state under `/workspace` survived it;
- the viewport stream binds loopback inside the box netns, and Aura's only channel into a box is
  `exec`. A relay over `docker exec -i` logged in from the host with no published port (14 fps,
  first frame 56 ms after the click, capture-to-host median 10 ms), so the cockpit live view rides
  a stdin-capable `ExecStream` rather than a published port.

**Threat model.** Nothing inside the box is secret from the model's shell: a second `exec` read
the key from the daemon's `/proc/<pid>/environ`, and with it the vault and session state decrypt.
The vault keeps passwords out of tool output and encrypted at rest outside the box; it does not
stop a prompt-injected model running `shell_exec`. Sensitive accounts are therefore logged into by
the user through the live view, so the box holds a session, never a reusable password; a stored
password is offered only as "readable by the agent's sandbox". The measurement does not cover
gVisor (not used), real sites (anti-bot, CAPTCHA, SSO, iframes, passkeys), the cockpit viewer and
its latency across LAN or Cloudflare, or redaction through Aura's tool pipeline.

**Live view, measured end to end on 2026-09-26.** A signed-in Authula operator opened
`/browser/<session>` on a running `aura serve` (Postgres, the real box image, the egress floor)
and logged into the fixture — email, password, TOTP — by clicking and typing in the cockpit only;
the fixture granted one new session and the box's browser reached `/docs`, three runs out of
three (`spikes/agent-browser-auth/live_view.e2e.ts`). The run corrected three assumptions:

- a frame is the viewport, not the screen: `deviceHeight` reported 720 for 1280x577 JPEGs, so the
  viewer maps both axes by one width scale;
- Enter must carry `text: "\r"`, or CDP submits no form;
- each open agent-browser session is its own Chromium, about 142 tasks and 180 MB on the fixture,
  so the 512-pid box held three at most, and the box skill tells the agent to keep one per site
  and close it. Real sites take more, and the default is now 1024 (2026-09-27, below).

It also found that the box's keep-alive PID 1 never reaped orphans: four sessions opened and
closed left 177 zombies counting against the pid cap, which starves `shell_exec` as well as the
browser. Boxes now run with Docker's init (`HostConfig.Init`), and the same runs ended at 3 tasks
and 0 zombies. Existing boxes built before a change used to keep their old image and host config
forever, since Docker cannot change them in place; the first E2E ran into exactly that. `Resolve`
now recreates a box whose image, init or cache mounts are not current, keeping its volumes and
logging why; an image that is not present locally is never a reason, because `Resolve` must not
pull. The measurement does not cover the viewer on a phone, over Cloudflare, or with a second
concurrent viewer in another browser.

**Viewer on the assistant-ui element, and the MCP server in the box, measured 2026-09-26.** The
live view now renders inside an owned copy of `@assistant-ui/elements-computer-use` (address
chrome, the operator's last clicks as a cursor trail); the same E2E passed three runs out of three,
and screenshots at 1440x900 and 390x844 put the cursor on the click. Separately,
`agent-browser mcp` ran inside a production box behind `ExecStream` with stdin, reached by the
go-sdk client over `IOTransport` and nothing else (`spikes/agent-browser-auth/mcpbox`): handshake
61-133 ms, 29 tools and 64 KB of schemas in one page, snapshot 33 ms, click 55 ms, first
screenshot 9.6 s then 50 ms. A `Suspend` ends the session cleanly within 2.3 s and a new exec
reconnects in under 100 ms, with the cookies restored but not the open page. The server's stderr
must be redirected, because `ExecStream` merges it into stdout. This does not measure a mount:
no agent turn, no `tool_search` deferral, no bridge redial, no redaction of tool results.

**Box runtime for stdio MCP servers, measured 2026-09-26.** A registry entry with
`runtime.kind: "box"` runs in the calling identity's box, one process per identity, over the
go-sdk `IOTransport` on an `ExecStream`; `ServerConfig.Box` makes `OpenSDKSessionForConfig` the
single place that either starts it in a box or refuses it (`ErrNoBox`), so no path runs a box
server on the host. Box servers default to `sandboxed_local`, take no secret-shaped env, and are
identity-scoped like OAuth servers: the tool list is read at mount in the operator's box, each
identity's first call opens its own process. The catalog adds `browser` (agent-browser, default-on
in the appliance) with its 29 tools graded by a recipe table, reads and reversible writes, since
the server states no destructive hint and the fail-closed default would gate every click.
Through `aura toolpipe` on the production registry: boot mounts 29 deferred tools, `tool_search`
loads them, a fixture page is driven in the box, and after the box is stopped the next call
brings it back, three runs out of three. The run found that the bridge refused a mutating call
on a session it already knew was dead, "reconnected but not replayed", though nothing had been
sent, which failed the first action after every idle suspend; such a call is now sent once, and
the no-replay rule is kept for calls that reached a transport. It also found that the idle reaper
counts only new execs, so a working MCP session or live view would be suspended under it; tool
calls and live-view input now mark the box as used. Not measured: a model choosing these tools,
two identities' boxes on one host at once (unit-tested only), and redaction of browser output.

**Counter-proof with an unrelated server, 2026-09-26.** `chetto1983/calculator-mcp-server`
(Python, FastMCP) was installed by `shell_exec` into a venv on the operator's `/workspace`
volume and declared with the new `aura mcp add --box`, which completes the handshake in the
operator's box: until then only a catalog recipe could declare the runtime. Boot mounted its 23
tools deferred; three runs out of three computed correct results in the box, came back after the
box was stopped, rendered a plot, and ran beside the browser recipe with separate stderr logs and
no process left 0.45 s after the caller exited. What it shows: the runtime carries a stdio server
it was not written for. What it does not: a server installed on one identity's volume is absent
from the others' boxes; an unannotated server's calls are graded destructive in a model turn;
the cockpit install form cannot yet declare the runtime.

**Per-identity install for box servers, measured 2026-09-26.** Chosen over a shared read-only
volume and over the image: every identity gets its own copy, so no package state crosses
identities (the 2026-09-14 cache-poisoning finding). `runtime.install` (CLI `--install`, box only,
shape-checked at save and before it runs) is a shell line run in an identity's box before its first
session and again when the line changes; the box records the hash of the last completed line, a
lock makes concurrent starts install once, and a failure records nothing and returns the tail of
its log. The install runs before any handshake clock (mount, first session, install
verification) under its own 5-minute bound: the docker test first failed because only the connect
deadline had been extended and `tools/list` still ran on the expired one. Measured: calculator
installs in 39 s and 479 MB per identity; two identities each installed a 12 s line once, on first
use; a changed line reinstalled; three concurrent starts installed once; on the production
registry an empty box was served after a 44 s install (2.4 s warm). Not solved: a new line delays
`aura serve`'s boot mount while it installs in the operator's box; each other identity pays the
install on its first call; deleting an installed tree but not its record breaks it until the
record is removed.

**LibreChat's model measured in the box, 2026-09-26.** LibreChat (7b2362d) declares stdio servers
only in operator YAML, runs them on its host, shares one process across users unless the config
carries user context, installs nothing (`npx -y <pkg>` fetches at spawn into a host-wide npm
cache) and gives a first spawn a per-server `initTimeout`, 30 s by default. In an Aura box the same
declaration installs per identity, because npm, uv and pip caches are already per identity: in a
production box with empty caches, `npx -y @modelcontextprotocol/server-filesystem@2026.8.31`
answered `tools/list` in 5.4 s cold and 0.66 s warm, `uvx mcp-server-fetch` in 3.5 s and 0.56 s,
and a numpy/scipy/sympy server from git through `uvx --from git+...` in 10.95 s and 1.76 s (39 s
through venv + pip). The caches survived a box recreate, and nothing but JSON-RPC reached stdout.
What it shows: the install line is not needed for npx/uvx servers, while a cold-start budget is,
since 10.95 s exceeds both the 10 s first-call redial budget and the 10 s default mount timeout.
What it does not: runtime downloads beyond the package, unpinned packages, a registry outage, or
concurrent identities.

**Self-installing box servers replace the install line, 2026-09-26.** Following that
measurement, `runtime.install`, its record, lock and `--install` were removed: a box server
declares a pinned command that fetches itself, as LibreChat's do, and gets
`runtime.initTimeoutSec` (default 30 s, at most 600) for its first start at the mount, at each
identity's first session and at the install verification. A docker test mounts a server whose
every start takes 12 s under a 10 s mount budget and serves a second identity's first call; with
no init timeout it fails at the mount. The first real run then found that a self-installing server
may print to stdout: `mcp-server-fetch`'s first `fetch` ran `npm install` and wrote seven lines
there, and the go-sdk ends a session on the first line that is not JSON, while the TypeScript SDK
LibreChat uses reports the line and keeps reading. Box sessions now drop and log such lines. On the
production registry with empty caches the server was added in 3.3 s and its first call answered in
4.1 s, then 1.0-1.2 s. The paragraph above on per-identity install is superseded. Not covered: local
host stdio servers have the same stdout intolerance and are unchanged.

**Host stdio servers get the same stdout filter, and their children are reaped, 2026-09-26.**
Measured against the go-sdk's `CommandTransport` (v1.8.0): a host server that printed one line of
npm output before answering lost its session at `initialize` ("invalid character 'a' looking for
beginning of value"), and a child it had forked (`sleep 300 &`) was still running after the session
closed, although `procgroup.SetProcessGroup` makes every server lead its own group for exactly that
(D-10): nothing ever signalled the group. `CommandTransport` pipes stdout itself and hides its
connection, so `internal/mcp/stdio_command.go` replaces it: the same pipes read through the box
path's `protocolLines`, and the spec's shutdown ladder (close stdin, 5 s, SIGTERM, 5 s, kill) with
the kill, and a context cancel, taken by the whole group, which is also killed after a clean exit.
Both tests fail against the previous code (negative controls). Not shown: that any server mounted
today prints to stdout; the process-group kill on Windows (`taskkill /T`) is not exercised.

**An install validates its declaration before the handshake, 2026-09-26.** A box server declared
with a secret-shaped env (`GITHUB_TOKEN=ghp_...`) was started in the installing identity's box by
the install's verification, with the secret in its environment, and only then refused by the
save's validator: a unit test with a recording launcher saw the box started with that env.
`prepareAndVerify` now runs the save's validation (`mcp.ValidateManagedServer`) first, for the CLI
and the cockpit alike. The cockpit install then gained what `aura mcp add --box` had: a custom
stdio server's `runtime` (`local` or `box`) and `initTimeoutSec`, with `--init-timeout` added to
the CLI so the previewed command is one it runs. Not shown: an install from the cockpit against a
live box; the handler, builder, form and CLI are unit-tested.

**The chat shows the live view itself, after a model skipped the skill, 2026-09-27.** On the lab
VM an operator asked Aura (`gemma4:31b-cloud`) to open YouTube so they could sign in. The model saw
`browser-aura` in its installed skills, reasoned that listed meant loaded, and never called the
`skill` tool. It opened the page with no `session` and no `restore`, so agent-browser used its
`default` session and saved nothing, and it told the operator to sign in without a link. Asked for
the link "as per /browser-aura", it answered with the page's own URL. The handoff therefore no
longer waits on the model. Under the latest turn's last message, the cockpit shows the live view of
the session that turn left open: the session of its last settled `browser__agent_browser_*` call,
`default` when that call names none, and nothing after a `close`. A replayed turn arrives as one
assistant message per model call, so the turn is every message after the user's last. Only the
latest turn shows a view, because the server keeps one viewer per session. `/browser/<session>`
stays for other channels. The same run found Google's sign-in refusing the box browser: through
the live view the operator reached `accounts.google.com/v3/signin/rejected` ("Couldn't sign you
in"), and the box's Chromium reports `HeadlessChrome/151` with `navigator.webdriver = true`. Not
shown: the chat view on the VM or on a phone (it is tested in jsdom through the real runtime and
snapshot replay), and a login made without `restore` surviving a box suspend.

**The box browser stops announcing automation, measured 2026-09-27.** Three sessions in the same
box took Google's sign-in page and a made-up address. The stock launch was sent to
`signin/rejected` ("This browser or app may not be secure"). Two launches passed the email step,
reaching "Couldn't find this account", so Google looked the address up. One was headless with
`--disable-blink-features=AutomationControlled` and a Chrome user agent, and read
`webdriver=false`, `Chrome/151`. The other was headed under the box's Xvfb with the same flag and
no user-agent override. The operator then signed in to YouTube with their own account through the
live view, password and second factor included, on a session launched headless with the flag,
the user agent and `restore`. Afterwards the page had the account avatar and no sign-in link,
Google's session cookies were set, and the encrypted state was saved. The entry point now starts
every browser that way. The user agent names the Chromium the image ships, read from the binary at
build time, and the image contract fails when the page sees `navigator.webdriver` or a headless
user agent. It failed on the image before this change and passed on that image plus the new
layers. Headless was chosen over headed because agent-browser exempts headed browsers from its
idle shutdown, and a box has room for few browsers. Wirasm/helm#374 reports the same refusal from
a spoofed user agent plus a debugging port, and a plain Chrome that signs in. The login did not
survive a box recreate: reopened with `restore` in the recreated box, the page showed "Sign in"
and Google's `SID`, `SAPISID` and `LOGIN_INFO` cookies were gone. That open then saved the
signed-out state over the old one, so whether the saved state lacked the cookies or Google
refused them in a new browser is not known. Not shown either: Google's later re-checks, other
sites' bot defences, and whether the user-agent override is needed once the flag is set.

**Browsers on real sites: the box's pids, not its CPU, measured 2026-09-27.** An agent turn on
lastampa.it opened the site in 49 s, then read, clicked and read again in 0.08-0.24 s each.
The live view was not the slow part: from the cockpit, a wheel event showed a scrolled frame
in a median 42 ms and a key a new frame in 51-79 ms. A browser on a real site holds about 200
pids and 400 MB. YouTube and lastampa.it open together kept a box at 398 pids and 783 MiB. A
third browser took it to 560-594 pids: at the 512 cap its launch hung for 153 s and failed,
and the whole box refused every exec, `shell_exec` included, until the third daemon was killed
from the host. The default cap is now 1024 pids. CPU and memory stay at 2 CPUs and 2 GiB. With
4 CPUs the box was no longer throttled, but lastampa.it still took 30-60 s or timed out. The
slow loads came from the lab VM's network, not the box: the same image and script loaded the
site to `domcontentloaded` in 16-20 s on the VM, in a plain container too, with no egress
sidecar and no limits. On the workstation's Docker they took 0.7-0.8 s. On the VM, DNS
answered 30 names in 0.1 s and a 10 MB download ran at 12 MB/s, while Chromium waited 19.4 s
for the page's main document, which curl fetched in 0.43 s. Idle browsers now close after 10
minutes instead of agent-browser's hour. On the box, a 15 s timeout closed an untouched browser
within 25 s. The same timeout kept a browser alive for 32 s while the live view sent a key
every 5 s, so an operator signing in is not cut off. Not shown: why Chromium is slow on the
lab VM's network, an appliance's page loads, and four heavy browsers together under the new
cap.

**Chrome no longer signs itself in, measured 2026-09-27.** After the operator signed in to
YouTube through the live view, Chrome's account consistency (DICE) opened `chrome://signin-error`
and a new tab beside the page. From 13:11 the session's daemon answered no command: a snapshot
was cancelled mid-way, then three opens and a `get_title` timed out at 60 s. The daemon kept
autosaving the state all along. Closing the two tabs over CDP did not free it, and neither did
SIGTERM; SIGKILL did. The box image now carries the policy `BrowserSignin: 0`, in the directory
Chrome for Testing reads, `/etc/opt/chrome_for_testing/policies` and not Chrome's
`/etc/opt/chrome/policies` (`components/policy/core/common/policy_paths.cc`). Driven through
agent-browser, `chrome://policy` lists it, and `chrome://signin-internals` reports "Account
Consistency: None" instead of "DICE". The image contract checks that row: it failed on the edge
image and passed on that image plus the policy layer. The same run showed why a Google login
does not outlive its browser. Every launch uses a fresh profile under `/tmp`, and after a
restart with `restore` only `__Secure-1PSIDTS` and `__Secure-3PSIDTS` came back, with `SID`,
`SAPISID` and `LOGIN_INFO` gone and the page signed out. Not shown: that those two tabs are what
hung the daemon (it takes a real account to reproduce), and a Google login kept by a persistent
profile.

**A persistent profile per browser session keeps the login, measured 2026-09-27.** The operator
signed in to Google through the live view on a session whose Chrome ran on agent-browser's own
persistent profile (`--profile`, a directory on the workspace volume) instead of a fresh one under
`/tmp`. On the lab VM box the account stayed signed in, with all 24 Google cookies (`SID`,
`SAPISID`, `LSID` and `__Secure-*PSID` among them), in three cases: with the browser alive; after
Chrome was closed and relaunched on that profile; and after the box container was restarted,
killing Chrome rather than closing it. In the last case the page showed a sign-out link and no
"Sign in". `restore`, which replays saved cookies into a fresh profile, had brought back only the
two `*PSIDTS` cookies. Hermes Agent also keeps logins in a profile directory that outlives the
browser: a copy of the user's own Chrome profile, or Camofox's per-user one. LibreChat has no
browser of its own. After the login the profile took 33 MB, 25 MB of it caches. Three constraints
came from agent-browser, not its README:

- launch options are hashed per command (`launch_hash`, `cli/src/native/actions.rs`). A call of the
  same session without `--profile` therefore relaunches Chrome on a temporary profile without a
  word, and the page goes back to `about:blank`. This happens through the CLI and through
  `agent-browser mcp` alike, so the profile has to ride on every call, not only on `open`;
- two sessions on one profile directory cannot run together: Chrome's `SingletonLock` aborts the
  second. With one directory per session, they ran side by side;
- `agent-browser mcp` runs each tool call as a child of its own binary, so the box's entry point
  never sees a call's session. The MCP schema has no `profile`, but all 29 tools take `extraArgs`.
  `--profile` appended there left free text untouched: `fill` and `type` with `--profile` inside
  the text typed it as given.

The bridge therefore gives every `browser__agent_browser_*` call `extraArgs: ["--profile",
"~/profiles/<session>"]`. It sets `session` to `default` when the call names none. It refuses the
call when the name is not 1-48 letters, digits, `-` or `_`, and when the model passes its own
`--profile`. agent-browser expands `~` against the `HOME` the entry point sets, so the directory
layout stays in the image. The entry point gives CLI calls the same directory, and the skill no
longer asks for `restore`; the `restore` advice in the paragraphs above is superseded.

A box killed with its browser leaves the profile's `SingletonLock` behind, naming the container's
hostname and Chrome's pid. With the same hostname, Chrome takes the lock over, even when that pid
now belongs to another process. With another hostname, it refuses the profile as "in use by another
Chromium process on another computer". A recreated box would hit exactly that, since its hostname
was the new container's id. Boxes are now created with one fixed hostname, `aura-box`, and
`Resolve` recreates a box with any other. It is not the box's name, because the name carries the
identity id and Docker refuses a hostname over 64 bytes ("is too long (maximum 64 bytes)"), which
the integration tests' identities exceed. Not shown: that the login lasts for days, since
Google may end it for other reasons; a stale lock whose pid now belongs to another Chrome; how far
a profile's caches grow; and what `restore`, which the tool schema still offers, does on top of a
profile.

**agent-browser 0.38.2 keeps reading Chrome's stderr, measured 2026-10-06.** 0.38.1 pipes
Chrome's stderr and stops reading it once the browser is up; upstream #2003 reports Chrome
freezing when a log line meets the full 64 KiB pipe. In the box image under 0.38.1, that pipe held
5.4 KB unread after launch and 8.3 KB after five navigations, mostly TLS handshake failures and
D-Bus errors. Under 0.38.2 the same sequence left it at 0. The image now pins 0.38.2: the tarball
matched the registry's sha512 integrity, and the tarball and both binaries are sha256-pinned. The
image contract and the whole `docker_integration` tier pass on it. Not shown: a browser actually
frozen by a full pipe, which no run here reached, nor whether that freeze is what an operator saw
as an unresponsive live view. The release's other changes (auth vault controls, chat mode, Kernel,
Browser Use) were not exercised, and its `/dev/shm` detection changes nothing here: 0.38.1 already
passes `--disable-dev-shm-usage` to a browser running as root, as the box's does.

`web-artifacts-builder` is a native, on-demand skill shipped in the binary,
including scripts, component archive and license. Bootstrap exports native
resources to the same `/skills/<name>/` path used by the sandbox; a catalog entry
without executable resources is incomplete. The sandbox image carries a prepared
React/TypeScript/Tailwind/shadcn/Vite/Parcel project and pnpm. Normal initialization
copies this dependency tree and needs no registry access. The shipped calendar
and panel components use tested compatible dependency versions.

The native bundle command checks the full TypeScript/Vite project before Parcel,
then runs Playwright smoke checks at desktop/mobile sizes on the candidate HTML.
It publishes `bundle.html` only after success and removes an older bundle when a
rebuild fails. Screenshot review, requested interaction assertions and source-data
accuracy remain explicit author responsibilities. The offline image contract
exercises initialization, complete build, bundling, browser rendering, calendar,
resizable panels, a state-changing button and stale-output rejection.

File operations enforce name, boundary, symlink and regular-file rules. Archive entry
names are metadata and cannot choose a host path. Staging uses exclusive creation and
private permissions. Lexical path containment alone does not contain a tree.

Background shell completion returns to its conversation without indefinite manual
polling. Cancellation targets the process group with bounded cleanup and visible
failures. Reuse supported subprocess/runtime mechanisms before building alternatives.

A woken turn is a run an open cockpit can see (2026-10-04). Measured on the local stack
with a scripted model: a tool call moved to the background woke its conversation 45 s
later, and a tab that had stayed open on that conversation still showed nothing of the
woken turn 10 s after it finished; a reload showed it. Polled every second through the
wake, `GET /api/conversations/{id}` never carried a `live_run_id`, and the conversation
stream (`/api/conversations/{id}/swarm/events`) announced only the first turn's run,
`running` then `finished`. The background-completion dispatcher drove the woken turn
through the runner alone, so the turn was in neither place a tab looks for a run it did
not start. Shell and video wakes go through the same dispatcher. A coordinator's
continuation after its workers report never had the gap: `ResumePendingSteer` starts it
as a detached run. A wake now starts the same way. It waits for the conversation, registers
a run, and lets the runner push and answer under the lock it already holds, so the steer
is still pushed only once the lock is held. A wake that finds the run registry full runs
unobserved, as every wake did before, rather than not at all; so does every wake with
`AURA_AGUI_RUN_DETACH` off, which builds no registry.

Verified the same day with the committed `background-shell-completion-real.spec.ts`, its
prompt answered by a scripted model instead of a real one. Before the change the wake ran
and answered, and the open tab still showed no answer after 90 s; after it the tab showed
the answer with no reload and no request of its own. A probe that held the woken turn open
for 4 s saw `live_run_id` name the wake's run throughout, saw the tab attach through
`GET /agent/runs/{id}/events`, and found the turn rendered once, exactly as after a reload.
The first measurement, repeated (a tool call moved to the background, woken 45 s later),
showed the woken turn in the open tab once, with no reload. The cockpit needed no change:
the coordinator frame, the conversation refetch and the reload-attach it reacts to were
already there for the coordinator's continuation.

This does not establish:
- a real model's woken turn: only the scripted one ran;
- a video wake in the cockpit: only shell and tool-call wakes ran, and a video wake reaches
  the same host through the same dispatcher;
- the full-registry fallback on a live stack: it is unit-tested only.

Web search uses the configured service. Fetch enforces URL/redirect limits, SSRF
protection, MIME handling, response caps and readable extraction, including supported
non-HTML. Documents, pages, attachments and delegated output remain untrusted data.

## 13. MCP integrations

The bridge uses the official MCP SDK and namespaced registration. Curation of Aura-owned
sidecars lives at the source. The host must not invent an `accountId` semantics that
confuses routing defaults with opaque handles returned by a previous operation.

The registry is Postgres-backed. Launch kinds are local stdio and Streamable HTTP.
Retired Docker declarations fail with a useful migration message rather than falling
through to an empty stdio command. The UI cannot display an unenforced network allowlist.

Profiles and the active profile are registry state, not process state. Measured on
2026-10-04 with the release binary against the local registry: `aura mcp profile create
work` answered `ok` and wrote a `profile_create` audit row, and `profile list` did not show
`work`; after `profile add work calendar`, `profile use work` answered `ok` and wrote a
`profile_use` row, and the active profile stayed `default`. Membership lived on server
rows (migration 0101), so a profile existed only while a server belonged to it, and
nothing stored which one was active. The ledger therefore recorded two changes that never
happened. A profile table now holds every profile and the single active one. A profile
persists after its last server leaves, and an empty active profile mounts no registry
server. Before, an empty `default` fell back to every enabled server, so removing the last
member from `default` mounted everything else. There is still no command to delete a
profile. This measures the CLI; the cockpit offers no profile switch.

Session termination is observed through the SDK lifecycle rather than a duplicate
liveness poller. Mount, call, elicitation and shutdown have finite configured bounds;
negative call timeouts cannot request unlimited execution. The production elicitation
wiring follows decline-and-surface: identify the requesting server to the operator,
decline the protocol request, and do not invent a blocked turn or approval row.

Installing supported resolver-based stdio servers prepares a durable environment,
resolves its executable, verifies initialize/tools-list, then persists the declaration.
Failed verification does not save a broken server. Preparation and mounting restrict
inherited environments, preserve public CA paths and withhold credential-bearing values.
Entrypoints remain inside their environment; removal cleans up and reports failures.

Check stdio declarations at save and spawn. An invalid entry must not make the registry
unreadable. Narrow persistence-abuse checks are not a command allowlist or exhaustive
malware detection. Stdio in the daemon is not automatically inside the user's tool box.

Mounted MCP descriptions and results are trusted by the accepted policy, with schema/
result caps and fail-closed consequence handling. This exception does not change web,
document, attachment or delegated-data trust. Mounting is an infrastructure trust
decision; lack of result fencing remains a residual risk.

OAuth is identity-bound and resource/audience-bound. Grants, access/refresh tokens and
revocation use protected storage and the existing native flow, also for Aura-owned
sidecars. A JWKS outage is infrastructure failure, not an invalid token. Log the cause
without flooding each turn with the same error.

Google account linking in the PIM sidecar uses a Web OAuth client that the operator
creates, and one shared relay page (github.com/chetto1983/aura-connect) as its only
redirect URI, identical for every install — Home Assistant's my.home-assistant.io shape.
The install names its own callback in `state`, the relay forwards the browser there, and
the install exchanges the code itself with PKCE; the relay holds no secret and forwards
only to `/admin/auth/google/callback`. Aura ships no shared Google client: its secret
would be public in the published images, and a shared client behind a public relay lets
anyone redeem codes consented to "Aura". Measured 2026-09-23 on a LAN VM with the browser
on another PC: a Desktop client's loopback redirect reached the sidecar only while an SSH
forward bridged the browser's `localhost:8093` (two links through it, zero callbacks after
it closed); through the relay a Web client linked with no forward. The same run measured
account writes answered 250-500 ms before the sidecar's registry reloaded them, so the
admin API now reloads configuration before responding. Not measured: Google refusing a
raw LAN IP as a Web redirect URI (its documented rule) and a run through a Cloudflare
tunnel.

The install's side of that return lives in the daemon, not in Caddy. `google/start` passes
the sidecar `returnBase` = the origin of `AURA_WEB_PUBLIC_URL`, else the origin the cockpit
request arrived on. The daemon serves `/admin/auth/google/callback` as a public route on every
origin, because the cross-site return carries no session cookie. It forwards the route to the
sidecar without a token; the sidecar authenticates it by the one-time `state`. The Caddy route
and `AURA_PIM_EXTERNAL_BASE_URL` are gone.

Measured 2026-09-23 on the same VM after the edge updater delivered image revision `7967294`:
- the key was removed from `.env`;
- Caddy was recreated without the route;
- a connect from a Windows browser at `https://192.168.101.158` stored the Google token at
  09:01:30 UTC through the daemon route;
- `GET /api/connect/pim/accounts/{id}/status` answered `linked: true` through the proxy. The
  cockpit's Google panel polls this every 2 s to replace itself with a confirmation.

Also measured, the same morning:
- An Outlook.com account linked by device code, with no redirect at all: code issued at
  09:10:56 UTC, success at 09:11:42, MSAL cache written with mode 600.
- Asked in the cockpit chat, the agent read both accounts through one `list_calendars` call:
  three Google calendars and four Outlook calendars. That proves the stored tokens against the
  Google Calendar API and Microsoft Graph, not only the link.

Not measured live: the panel's switch itself (unit tests only) and the return through a
Cloudflare tunnel.

A remote MCP server whose authorization server offers no registration endpoint is reached
with Aura's Client ID Metadata Document, published beside the Google relay at
`https://chetto1983.github.io/aura-connect/mcp/client-metadata.json`. The document's only
redirect URI is a relay page next to it (`.../mcp/callback/`). The relay works like the
Google one and forwards only to the cockpit's `/api/governance/mcp/authorization/callback` and
to the loopback listener of `aura mcp login`. The document is a fallback: a mount presents the
operator's pre-registered client, else registers dynamically, and signs in with the document
only when the SDK reports no registration method. Linear and Notion advertise metadata
documents too (measured 2026-09-23); they keep signing in through dynamic registration on the
caller's own redirect.

Measured 2026-09-23 against ElevenLabs' hosted MCP:
- Its authorization metadata advertises `client_id_metadata_document_supported` and no
  `registration_endpoint`. An `xi-api-key` is refused on the hosted MCP.
- Its protected-resource metadata names `https://api.us.elevenlabs.io/v1/mcp`, not the
  documented `https://api.elevenlabs.io/v1/mcp`. go-sdk v1.7.0 enforces that match (RFC 9728
  §3.3) and fails discovery on the documented URL, so the server is configured with the
  `api.us` one.
- Given an unknown document URL, it fetched it and refused with `invalid_client: Client
  metadata document returned HTTP 404`: no domain allowlist before the fetch.
- Given Aura's published document and relay redirect, its consent screen named "Aura" and
  warned that the app is self-declared. Its API answered 200 with `metadata_document_host:
  chetto1983.github.io`, `has_localhost_only_redirects: false` and `requires_paid_plan: false`.

Measured 2026-09-23 through Aura, on the LAN VM running image revision `b43b57ff4`, with the
cockpit reached over HTTPS and the server configured at the `api.us` URL:
- The mount before consent refused with "this identity has not authorized this server"
  (10:33:32 UTC), instead of the SDK's "no configured client registration methods".
- Connect → ElevenLabs consent → relay → cockpit callback → code exchange stored the grant
  at 10:33:52 and mounted 120 tools at 10:33:56. The grant holds all 7 advertised scopes, a
  refresh token, and an access token that expires one hour after issue.
- Asked in the cockpit chat, the agent found `elevenlabs__agents_list` through `tool_search`
  and called it. The stored tool turn is ElevenLabs' own answer, `{"agents": [], "has_more":
  false}`, for a workspace with no agents. That proves the token against ElevenLabs' API,
  not only against the MCP endpoint.
- After the updater restarted the daemon on revision `835efa363` (10:44:10), the server
  remounted from the stored grant with 120 tools and no consent. Linear remounted the same
  way, through its dynamic registration (68 tools).

Not measured yet: the refresh leg (the first access token expired at 11:33:52 UTC). Not
observable from Aura's logs: which token-endpoint auth style ElevenLabs accepted. The SDK
leaves a document client on x/oauth2's auto-detection, which tries HTTP Basic first and
the body on refusal; the exchange succeeded, but the retry is proven only in-process.

A file an MCP result carries (an embedded blob, or a `resource_link` Aura reads back on the
calling session) lands in the caller's box at `/workspace/mcp-files/<request-id>/<server>/<name>`
for one turn. The model sees only a footer with path, type, size and sha256, and the turn's end
removes the directory. A directory an unclean exit left behind is swept, once it is 24 h old, by
the next call that writes a file into that box. Caps: 25 MiB per file, 50 MiB per call.
Measured 2026-09-24 on the LAN VM, driven through the cockpit API with the operator's own
account and real mailbox and chat (read-only). The images were `aura` `7098a07e4`, then
`1b7af89ce`, `aura-pim-mcp` `6734385` and `whatsapp-mcp` `7a0e479`, then `1ec0233`, all
installed by the updater:
- **Mail** (request `01a0d47b-f658-7058-a063-e707cdbc05da`). `get_email_attachment` returned a
  link to an 814,316-byte PDF. The stash declared `application/octet-stream`; the footer said
  `application/pdf`, taken from the name. `pdfinfo` on the path gave 7 pages.
  - The call took 4,975 ms, including the upstream mail fetch, the link read and the box write.
  - The ledger kept 623 bytes of result and none of the file.
  - The file existed at 17:35:36 UTC. The turn ended at 17:35:42.3, and the file was gone at
    17:35:43.
- **WhatsApp.**

  | File | Size | `download_media` |
  |---|---|---|
  | chat image (`image/jpeg`, 1152×2048) | 226,802 B | 1,605 ms |
  | status image | 93,018 B | 1,455 ms |
  | channel image | 137,329 B | 1,117 ms |
  | document (sender's name `…pranzo.md` kept) | 17,214 B | 893 ms |

  Every file was gone within seconds of its turn ending. `turn cleanup failed` was logged 0 times
  across the run's 9 file-writing turns.
- **Two fork defects the run found**, fixed and measured again the same day:
  - Channel (newsletter) media failed with "incomplete media information". Channel media is
    unencrypted: the rows had no `media_key` or `file_enc_sha256`, and the bridge demanded both.
    Fixed in `98b8d64`.
  - A `.md` document was declared `application/octet-stream`, because Python 3.11's table has no
    `.md`. Fixed in `1ec0233`.
- **Orphan sweep** (request `01a0d4c1-b472-7074-9bcc-63329a598d1d`). A 2-day-old
  `req-e2e-orphan` planted in the box's volume was removed by the next file-writing call, and a
  fresh sibling survived.
- **Largest attachments in the mailbox** (requests `01a0d4d3-34d6-…` and `01a0d4d3-e4f1-…`).
  There were four photos of 9.5 to 10.5 MB, two per turn, fetched by parallel calls within one
  minute, and each byte count matched in the box.

  | | Before | Peak | Delta | Container limit |
  |---|---|---|---|---|
  | `aura` memory | 99 MiB | 204 MiB | +105 MiB for two ~10 MB files | 768 MiB |
  | `aura-pim-mcp` memory | 473 MiB | 662 MiB | +189 MiB | — |

  - `aura` settled at 189 MiB after the turn.
  - The pair of calls took 8.1 s and 9.9 s, then 5.9 s and 9.8 s. The sink's lock serializes
    the two box writes.

What this does NOT prove:
- servers other than these two forks;
- a file near the 25 MiB cap. The largest in the mailbox was 10.5 MB. By linear extrapolation,
  about 5× the bytes in `aura`'s memory, a call at the 50 MiB cap would add ~260 MiB, and two
  such calls in parallel would approach the 768 MiB limit. That is unmeasured;
- a PNG the bridge saved as `.jpg`: no such media was in the chats, and only the fork's unit test
  covers it.

The added latency of the link read alone was not separated from each call's own work.
A file the model derives outside `mcp-files` (a `pdftotext` dump in `/workspace`) outlives the
turn by design, and this run deleted it by hand.

**An image in the box reaches the model natively.** This was measured on VM .158 with the chat
model `gemma4:31b-cloud` on Ollama. The first run of "what is in this photo" went unanswered: a
native image part then came only from the user's own uploads, and `read_file` refused binaries.

Now `read_file` on an image in `/workspace` attaches it as a user message after the tool results.
- It rides every later request of the turn, the forced final answer included, up to 8 images per
  turn.
- It is gated on the model's image capability, as uploads are
  (`openai_compat/request_tool_media.go`).
- Before sending, the image is downscaled to a 1024 px long edge and capped at 2 MiB. A JPEG or
  PNG that already fits goes out as it is. Any other format goes out as a JPEG (a GIF as its first
  frame). An image that does not fully decode is refused.

| Run (revision, UTC) | What the model answered | Checked against |
|---|---|---|
| Latest WhatsApp photo: 850×850 JPEG, 11,858 B, from a channel (`9c92e6855`, 2026-09-24 22:03) | A white STRONG router with two external antennas | The image itself. The caption names only "Strong Router 4G WiFi". |
| Follow-up on the same photo (`9c92e6855`, 22:05) | "WPS" under the last light, and 6 lights | The image has 7 lights. "WPS" is printed only on the image. The count is the model's error, not a lost pixel: 850 px goes out unscaled. |
| Two 12 MP mail photos: 4032×3024, 9.5 and 10.5 MB, fetched and read in parallel (`9c92e6855`, 22:06) | A gas hob, a moka, a coffee pack reading "Intenso", a yellow tape measure | Text on the pack, not in the mail |
| A 6000×4000 JPEG planted in the box, a random 4-digit number drawn on it (`a13c0bedc`, 22:29) | `read_file` refused it as over the pixel bound. The model shrank it with Pillow through `shell_exec`, read the copy, and answered "5403, orange". | The number was written only to a file outside the VM; it matched. |

- **Memory, two 12 MP reads** (`9c92e6855`, before the per-color-model bound). `aura` went from 126 MiB to 273 after the two fetches, 398 after
  the two reads, and a 452 MiB peak (VmHWM 507) of 768.
  - The anon memory, 415 MiB just after, was back at 241 MiB five minutes later. The Go scavenger
    returns it, and no `GOMEMLIMIT` is set.
  - On `a13c0bedc`, the 24 MP refusal cost nothing, because it is decided from the header. The
    2000×1333 copy the model then read added ~66 MiB.
- **The decode bound depends on the color model.** One `DownscaleForVision` call was measured in
  WSL, each image in a fresh process: VmHWM minus that of an empty process.

  | Image | 12 MP | 40 MP |
  |---|---|---|
  | baseline 4:2:0 JPEG | 120 MiB | 235 MiB |
  | progressive 4:4:4 JPEG | 280 MiB | 756 MiB |
  | progressive CMYK JPEG | 390 MiB | 1,116 MiB |

  - With a 300 MiB budget per decode, the bounds are YCbCr 12.36 MP, CMYK 8.47 MP, gray 22.77 MP
    and anything else 10.11 MP (`a13c0bedc`). Go keeps every coefficient of a progressive JPEG
    until its last scan.
  - Over the bound, `read_file` refuses, and says `shell_exec` can shrink the image. An upload goes
    to the OCR sidecar unshrunk.

What this does NOT prove:
- the `tool_media_count` trace field on the VM. The reasoning trace is off there
  (`AURA_REASONING_TRACE`), so delivery is proven by what only the image carries;
- the WSL decode figures inside the aura container, whose own heap comes on top;
- an animated GIF (only its first frame is decoded), lossy WebP, or a 16-bit gray PNG;
- the forced final answer after an image read (a tripped step, wallclock or dedup budget): only a
  fake-client test covers it (`b1b31cd02`);
- chat models other than `gemma4:31b-cloud`.

Deferral follows usage and bounded slots. The current bridge qualifies servers with
at most four model-facing tools for two always-loaded slots in deterministic order.
Overflow stays discoverable. Four memory entry points remain loaded; the rest can be
found through search. An unmeasured global tool-count target cannot justify making
tools unreachable. MCP UI resources are rendering metadata, not extra model instructions.

The calendar recipe never takes an always-loaded slot (2026-10-03). Its one model-facing
tool qualified by count, but the manifest a real turn sent measured 43,403 characters
across 19 tools, and `calendar__calendar` alone was 13,136 of them (30%): the PIM schema
rode in every turn, including turns about nothing on the calendar. It is now reached
through `tool_search` like the WhatsApp surface. Native tools and the memory core are
unchanged: of the 43,403, the 15 native tools were 26,664 and the memory core 3,603.

The 3,603 came from the stale pinned E2E image and its three core tools. Measured again
the same day on a live turn (master `1d83f3ee5`, memory MCP built from that commit, the
request captured at the OpenAI-compatible endpoint): the manifest is 35,729 characters
across 19 tools, the 15 native tools at 26,664 and the memory core at 9,065 (`batch`
3,675, `upsert_fact` 2,596, `recall` 2,043, `entities` 751), calendar at 0. The second
always-loaded slot stayed unused: the WhatsApp image the stack ran (`sha-a463da5`, the
one CI pins) advertises 15 tools, over `maxAlwaysLoadedMCPTools`, so it is deferred by
count. The 18,554 first written here for the memory core counted the raw `tools/list`
entries, whose output schemas, annotations and titles never reach the manifest.

This does not establish:
- how often a turn needs the calendar, or what the extra `tool_search` round trip costs
  when it does: no usage data was read;
- token counts: every figure here is characters of serialized JSON;
- whether the WhatsApp tool schemas changed between the two images. The tool count did
  not: `whatsapp-mcp:latest` as published on 2026-09-24 (`1ec0233`) advertises the same
  15 tool names as the CI pin, so it is deferred on an appliance too. The curated
  WhatsApp merge that would bring it to 3 tools has not landed.

## 14. Skills and sharing

Postgres owns catalog and grants; filesystem roots hold bodies. Names are owner-scoped.
Deployment skills win over personal collisions; shared skills override neither. Two
shared owners contesting one name must not produce a mixed materialized directory.

A grant authorizes one skill tree, not an owner's full export root. Recheck access at
read and materialization. Revocation removes a skill at the next box mirror. Stage
valid sources before clearing/replacing the mirror, including an empty authorized set.

Shared-source faults may be isolated and reported without denying the owner their box.
Owner/deployment faults remain visible failures. Tar staging uses bounded memory and
files under the existing cleanup-owned run directory.

Cockpit list/body reads, composer selection, pinning, archive/restore and writes resolve
the same identity and roots. Completed writes invalidate the loader view. Shared-library
administrative rights are explicit; ordinary ownership labels do not authorize editing
deployment policy. Builtins are application-owned and must not offer lifecycle actions
that boot materialization immediately undoes.

`find-skills-aura` loads on demand, not in every turn (2026-10-03). Its body was the only
always-on skill: 3,762 bytes injected into messages[1] of every turn, about half of that
6,788-character block on the measured turn, to teach a path the `skill` tool description
already teaches (look at the installed list first, install through `skill_manage`, never
through the CLI). The installed-skills list in messages[1] still names it with its
description, and the capability-gap replies of `tool_search` and `skill action=list` point
to `skill action=use name=find-skills-aura`. Measured on a live turn the same day (master
`1d83f3ee5`): messages[1] is 3,283 characters, down from 6,788. This does not establish
how often a turn needed the body: no usage data was read.

Skills and snippets are not permanent model self-modification. Retired pending stages,
`Agent.md` provisioning, orphan pyscripts/MCP roots and hidden legacy landing zones are
not current mechanisms. Group principal support and moving bodies into Postgres require
their own implementation contract rather than being inferred from the generic ACL schema.

Knowledge-work packs preserve repository membership across the upstream plugin
manifest, skills, MCP declarations and commands. Resolve them from the repository,
not a skill catalog that discarded their grouping. Connectors arrive blocked until
one explicit pack trust decision covers the imported members; a pack cannot trust
itself. Missing endpoints and unsupported entries are reported. Reuse the existing
skill/MCP installers rather than creating a second marketplace or execution system.
Composer discovery/invocation and governance management remain the intended UI
contract; CLI installation alone is not evidence that every UI path is complete.

## 15. Scheduling, delegation and asynchronous delivery

Tasks support `at`, `every` and `cron`, with durable owner, payload and route. The cockpit
and CLI expose inspection/control. Timezone, quiet hours, claim/lease, concurrency and
catch-up have explicit semantics.

Agent jobs use the common runtime and one model/budget snapshot. Claims and notification
intent preserve their transaction boundary. Delivery retry does not rerun completed
model/tool work. Multi-goal enqueue is atomic for the identity.

Mounted-MCP probe 104Y on 2026-09-08 executed three independent random-token commands
through a coordinator and two grandchildren, with correct IDs and ancestry. The parent
turn ended at13:16:47 UTC with an explicitly partial table; the coordinator report arrived
at13:17:00 and remained visible without triggering the requested final synthesis. The
existing end-of-turn steer drain does not handle results arriving after that boundary.
Completed fan-outs must therefore wake their owning coordinator through the existing
durable steer queue, conversation lock and normal agent runtime. Coalesce sibling reports,
skip an already-consumed batch, and defer while the parent is busy. The cockpit must discover
and attach to this ordinary run and retain the final synthesis after reload. Cancellation,
ownership, bounded execution and untrusted report framing remain in force; no LLM critic
or polling loop of model calls is added. LibreChat's `subagentCompletionWakeup.ts` registers
idempotent continuation intent, checks parent readiness and claims a saved result before
dispatch. Aura reuses its existing queues and run registry for those responsibilities.
Probe104Z passed partial-result and hostile-instruction handling, but also exposed a
200-character status excerpt described as an integral transcription. A bounded status
preview is not a complete report and must disclose truncation. These probes do not prove
universal prompt-injection resistance or recovery of an in-flight model call after crash.
Evidence: `.planning/spikes/104-agent-controls-and-grounded-results/final-answer-e2e.md`.

The late-wake retest on 2026-09-08 (conversation `01a0817d-8ee3-7741-b6be-6121ea957541`)
resumed automatically after the initial turn ended, and the cockpit attached without user
input. It also exposed report truncation that triggered repeated status/recovery calls and
a worker substituting its hostname for its ID. Model notifications must retain a larger
summary than UI cards, disclose truncation, and expose the complete saved report for retrieval.
Worker briefs carry the exact host-issued worker and parent IDs. The raw internal report
messages shown in that probe confused the user: the cockpit's main chat must hide host-marked
delegation receipts while retaining coordinator answers and the activity/report view.
The1328ec796 closing probe reproduced late wake and returned all three actual tokens and
host-issued executor/parent IDs without a status call. The partial/hostile probe retained
exit17 and the full quoted report without executing its instructions. Reload preserves the
answers and hides internal receipts. The short hostile probe made two status reads, so this
does not establish zero polling for every model request. Shared scratch-file isolation is
also outside that proof: the grandchildren used the same temporary filename.

The 2026-09-08 five-worker live probe exceeded the configured concurrency of four:
the tenant polling wrapper recreated the asynchronous delegation loop on each pass,
discarding its occupied slots. Retain stateful delegation processors across polls and
apply the configured swarm width to their claim capacity. Retire idle processors when
their identity is no longer active; observation must not reset execution admission.
The corrected admission probe kept a fifth job queued at attempt0 while four ran.
Its card still said Running and opened a nonexistent transcript. Queued workers must
be labeled as queued, and their activity view must wait for an actual execution.
The same probe exposed no pre-start stop control. The existing worker controls read
must expose a queued cancellation target from the durable job, scoped to its owner,
conversation, child, job ID and observed attempt count. The existing cancel endpoint
must persist operator cancellation only while that exact attempt remains queued;
a claim that wins the race invalidates the queued target. Pending terminal delivery
is not executable work to cancel. Acceptance remains visible after reload, and the
normal claim/delivery path records cancellation without constructing a model.
The native status-stream regression on 2026-09-08 also found that a recorded
`canceled` marker was projected as `failed`. The stream must preserve cancellation
as its own terminal outcome, matching the durable job and report.
Live MCP probe 104Q3 then confirmed cancellation before any model/tool invocation,
but the empty terminal pane still said Connecting. A terminal worker with no visible
activity must show a localized empty outcome rather than an ongoing connection.

The live 2026-09-08 MCP inspection found no child controls while two real workers were
running (spike 104). Operators must be able to steer and stop an individual worker,
including a nested worker, without redirecting its parent or siblings. Controls belong
to a particular execution, use the existing owner-scoped run and idempotency rails, and
distinguish acceptance from application. Operator cancellation is a terminal outcome,
not a retryable failure. Stale targets and interrupted owners must be reported honestly;
a later incarnation must not silently consume a previous one's corrections. The worker
transcript remains on assistant-ui's native read-only runtime, with localized controls
and receipt state around it. Spike 104 carries the closing evidence matrix.
The 104S live probe confirmed that stopping a coordinator also terminates its
descendants' running processes and records their cancellation, while its independent
sibling completes once. This scope must remain intact for keyboard and pointer controls.
The 104R graceful-restart probe rejected an accepted, undrained correction as
`worker_run_ended`; neither resumed worker applied it. Its interrupted shell results
also exposed a remaining classification defect: `[command cancelled]` was marked
`ok` and displayed as Completed. Command cancellation must carry an explicit
structured outcome through the existing tool event and visual-status pipeline.
The 2026-09-08 MCP 104U service-worker lifetime experiment opened two real,
owner-scoped status streams: one through the PWA worker and one with native CDP
bypass. Stopping that worker closed only the proxied stream (OPEN to CLOSED);
the direct stream remained OPEN. The PWA must intercept only its explicit static
precache assets and navigation fallback, leaving API/event requests on the browser's
native network path. LibreChat's Vite PWA config similarly limits caching to static
assets and locale chunks. Add a service-worker-enabled browser regression because
the existing mocked Playwright suites block service workers. This proves a transport
lifetime defect; it does not establish the cause of the separate intermittent
conversation navigation or close the coordinator-grounding and cross-owner gaps.
The same 104W MCP probe displayed the canceled command's elapsed time as 20 seconds
live but 0 seconds after reload. Retained agent events already carry their original
timestamps; AG-UI replay must preserve those timestamps with the SDK's native
Event.SetTimestamp instead of replacing execution time with replay time. This
measurement concerns displayed elapsed time, not a repeated command execution.
The native Authula-cookie TLS regression on 2026-09-08 verifies the same control
ownership against real Postgres conversations, receipts, operation keys and queued
jobs: a foreign session receives404 for history, steer and both cancel targets,
without a side effect; the owner retains access and idempotent replay. Sessions
are minted by Authula's native service on an isolated database. This covers session
validation/authorization, while credential/TOTP login and actual model execution
remain separate test scopes.
The 104X live SIGKILL probe recovered its worker after the native lease expired,
completed attempt 2 with {"value":6011}, and left the old correction undrained;
the control API reports it rejected as owner_unavailable. The finished pane still
showed the first, result-less shell invocation as Running with a growing timer.
The tool renderer must honor assistant-ui's native part status: a terminal part
without a result has an interrupted/unknown outcome, not ongoing execution or a
successful command. Do not manufacture output or an elapsed duration when the
original completion was never recorded.
The 2026-09-08 paused-worker probe accepted `cancel` but rebuilt the model and marked
the job succeeded. The resume observer must carry the explicit cancellation into the
existing terminal-delivery path, rather than interpret it as another model-facing answer.

Each fan-out and worker queue key belongs to the trusted `swarm_spawn` operation:
retrying that operation preserves its identities; a new turn or model round, including
changed shared context, creates new workers. On 2026-09-07 the live cockpit reproduced
two accepted calls with identical goals but only two total queue rows: the enqueue
path discarded the runtime operation and used an unset parent-run field. The scoped
correction and validation are recorded in spike 103. This measurement establishes a
repeat-delegation defect, not universal multi-agent reliability or restart recovery.
On 2026-09-08 two distinct invocation keys (`agent_tool:probe:62646` and
`agent_tool:probe:102602`, under the fixture identity/conversation in spike 104)
produced the same short worker ID `w1-c4ea2263`. Worker identifiers must retain at
least 128 bits of the invocation digest. Replayed durable jobs retain their stored
identity, including legacy identifiers; an enqueue acknowledgement must use the row
actually returned by the queue rather than recomputing an incompatible identifier.

Outcomes, transcripts, reports and status stay in the originating conversation.
Cross-channel delivery is explicit. Status includes elapsed time; terminal reports and
stalled/orphan states are distinct. The cockpit resets worker watches on conversation
change and handles named terminal SSE events.

The 2026-09-07 nested-delegation probe (depth cap temporarily raised to 3) returned
correct numbers while every grandchild command was denied with `reservation failed`.
The nested adapter received the parent's flat worker session instead of the originating
conversation UUID. Every depth must retain that UUID for the gateway and transcript
ownership, with separate worker identities and mutation scopes for each invocation.
Correct final arithmetic alone does not establish successful delegated execution;
spike 103 records the tool-level failure and requires successful grandchild audit rows.

The 2026-09-07 cockpit inspection found worker activity hidden behind the collapsed
spawn tool and its 64rem report table. Worker cards must therefore remain visible
inline, outside settled-tool grouping, with responsive goals, real lifecycle status,
elapsed time and direct transcript access. The existing read-only worker pane uses
assistant-ui's documented `ReadonlyThreadProvider` for its separate streamed messages;
it must expose streamed activity, retain conversation ownership on reload/switch, and
leave parent composition untouched. Reference: assistant-ui `/docs/tools/multi-agent`
and LibreChat's `SubagentCall`/`SubagentActivity`, pinned in spike 103.
The extended nested probe found that selecting a grandchild unmounted its source card
and immediately closed its pane. Mounted cards are not an ownership authority: the
existing conversation-scoped transcript endpoint validates access before opening SSE.
The pane must retain that server boundary and clear on conversation changes, while
allowing restored or nested workers absent from the currently mounted cards.
The 2026-09-08 child-steer probe delivered the correct final JSON over SSE and
persisted it, but the pane lost it when terminal status removed the live run ID.
Completion metadata must not restart an already open transcript replay. Reconnect
for a new execution or scope, retaining the stream through its own terminal event.
The nested mobile probe also retained the selected child but lost its drawer on reload.
Restore the saved open intent once the owning conversation is known, including the mobile
overlay; an explicit close or a different conversation must not reopen it.

Continuation retains the exact model-facing trust-framed tool preview. Static worker
policy and delegated goal/context stay at their correct authority levels. Bad resume/
dead-letter rows cannot starve others. The substrate remains at-least-once across the
disclosed external-side-effect/ledger crash window.
The 2026-09-07 live SIGKILL/restart probe preserved a completed child's single
attempt and report. Its unfinished sibling was reclaimed after the original 300s
lease and completed on attempt 2; the conversation contained one durable report per
child. This establishes recovery for the measured arithmetic tools, not exactly-once
execution of an unfinished external action. A separate nested probe exposed a root
model inventing child identifiers before the real reports arrived. Runtime correctness
does not by itself close that answer-quality gap; validate final answers against actual
worker reports rather than treating the removed completion critic as a guarantee.

Worker pause creation must persist the same host-authored decision policy as a
normal runner pause in its atomic pause/park transaction. On 2026-09-07 a live child
asked for a number but every answer returned HTTP 403 (`approval decision not allowed`):
the worker pause writer omitted `allowed_decisions`. Reuse the runner's policy builder;
the resume path must continue rejecting absent or restricted policy, never infer an
authorization from UI buttons. Spike 103 records the failing conversation and retest.

The read-only worker stream renders pause questions as activity and continues replay
through later attempts; the parent approval card owns answers. A bounded `reported`
status flag follows the committed report write. The cockpit then refreshes history
when its parent stream is idle, rejecting stale refreshes after a new send or route
change. Model completion alone is insufficient evidence that the report is persisted.
Worker completion steers retain the existing untrusted source envelope on parent
wakeup and are not persisted again as operator-authored messages.

The operator explicitly requested visible worker reasoning on 2026-09-07. The
identity-scoped cockpit worker stream therefore uses the same reasoning projection
as the parent cockpit stream; ownership checks still precede SSE headers. Aggregate
worker status carries no reasoning text. This does not change other channels or
the independent reasoning-retention policy.

A tool call that outlives its window moves to the background (2026-10-04). Measured through
the real mount path (`MountServer` against a stdio server whose one tool waits as long as it
is told, every timeout env unset): a 90 s call returned `context deadline exceeded` after
60.04 s, the server saw its request cancelled after 60.043 s, and no request carried a
progress token. That is `AURA_MCP_CALL_TIMEOUT_SEC`. A generation that takes longer -- the
ElevenLabs hosted MCP is the case that asked -- is paid for and thrown away, and the model's
only move is to call it again. The SDK offers nothing to defer to: go-sdk v1.8.0 implements
no `tasks/*` method, so a call cannot be resumed anywhere; it can only be waited for.

The runtime therefore does for every tool what `shell_exec` already does for a command
(§12): a call still running after `AURA_LOOP_BACKGROUND_AFTER_SEC` (default 60) is not
cancelled but moved to the background, and the model reads a task id and goes on. The call
keeps the turn's values and loses its cancellation, bounded by
`AURA_LOOP_BACKGROUND_MAX_SEC` (default 1800: LibreChat's 30 minutes, and the maximum the
MCP lifecycle says a client should always enforce). Both are rows in `aura.settings`, set
in the cockpit's turn-budget pane beside the steps and wallclock caps and live the same
way: the Settings profile publishes them, and every turn's budget is built from them. A
save whose ceiling is not above its window is refused, because every turn after it would
fail to build its budget. When it settles, the existing
background-completion dispatcher wakes the conversation -- a third source beside the shell
and the video watcher -- and the model reads the result once with `tool_poll`, which also
cancels a call no longer wanted. The wrapper sits where every agent tool call already
crosses (`execTool`), so an MCP call and a built-in are treated alike. A tool whose own
mechanism already answers this (`shell_exec`, `video_generate`, `swarm_spawn`) is declared
foreground and never moved. An MCP call made outside the agent loop -- the tool pipe, a
notification send -- keeps the 60 s bound: nothing there could collect a later result.

LibreChat (`packages/api/src/agents/background.ts`, 2026-09-30) builds the same thing as a
`run_in_background` argument the model sets per call. Aura promotes after the fact instead,
because a call's length cannot be known before it runs: `shell_exec` had that argument
before its own promotion landed, and across every recorded turn it was set in none of 28
tool-call turns (2026-09-09, `internal/agent/tools/shell_bg_promote.go`). The rest follows
LibreChat: an exclusion set, a poll tool, a 30-minute bound, and a call lost if the process
dies.

What a moved call keeps from its turn is decided, not inherited. The files an MCP result
carries go to a directory of the call's own, removed when the turn that reads the result
ends, or when a result nobody read expires, never when the turn that started the call ends.
A form a server asks for after that turn has ended is cancelled at once, which the cockpit's
asker already does for an ended run. The idempotency operation records the in-progress
result, so replaying the same mutating call returns the task id instead of running it again.

This does not establish:
- anything about ElevenLabs: how long its tools take, whether they send progress, whether
  audio comes back as a file or a link, or whether a cancelled generation is billed. Only
  `agents_list` has been called (§13, 2026-09-23);
- the model's reaction to the 60 s error, or to the task id: no real agent turn ran, because
  the local stack's OpenRouter key had expired;
- survival across a restart: an in-flight call dies with the process, as LibreChat's does;
- a box-runtime stdio server outliving the suspension of its box.

A reminder on an external route reached nobody (2026-10-05). An operator could not get
reminders on WhatsApp. Reproduced on the lab VM through a real cockpit turn: the agent
scheduled a reminder with `notify=whatsapp`, the run completed at fire time, and the MCP
`send_message` answered `Recipient must be provided`. Nothing supplied a recipient: the task
tool has no recipient argument, the dispatcher passes an empty one, and
`AURA_SCHEDULER_NOTIFY_RECIPIENT` was unset. The three bounded retries then failed for a
second reason, `remote MCP call requires an authenticated identity`: the sweep sent with the
tick's context instead of the row's identity, so no retry of an MCP route could ever succeed,
and its error overwrote the first one. Meanwhile the conversation received the reminder text
as if it had been delivered, and the run ledger said `completed`. The same probe fired one
reminder per remaining route: `email` failed at fire time with no mounted `send_email` tool,
although the task tool had accepted it; `stdout` reached the conversation and the container
log; `telegram` was recorded as delivered to the identity's linked account; `none` left
nothing, in the conversation or anywhere else.

WhatsApp therefore defaults to the account the identity linked in the cockpit. When
`AURA_SCHEDULER_NOTIFY_RECIPIENT` is unset, the recipient is the number the bridge reports
for that tenant (`GET /api/status`, its `jid` without the device suffix). The WhatsApp MCP
has no tool that returns its own number, so the source is the bridge management REST that the
cockpit's Connect panel already calls. The task tool resolves the destination before it
persists: a route that cannot deliver for this identity now (no mounted send tool, no
recipient, WhatsApp not linked) is refused with the reason, and an accepted route names its
recipient, so the model has nothing to invent. A sweep sends under the row's identity. A push
that fails is written into the origin conversation beside the outcome, never presented as a
delivery.

This does not establish:
- that a message to one's own number notifies the phone: one such self-send through the
  bridge returned `success:true` on 2026-10-05, and whether it rang was not observed;
- Telegram delivery as seen on the device: only the database records it;
- the operator's own appliance: its environment was not inspected, so the same cause there
  is inferred, not measured;
- a schedule-time check for a route changed in the cockpit scheduler board, which still
  validates only the route name.

The same probe on the corrected image (revision `c7f6c74c7`, 2026-10-05 09:02 UTC) moved
both failures one step further. WhatsApp: the task tool named the paired number, and at fire
time the bridge logged `POST /api/send` four times (09:02:43, 09:02:44, 09:03:13, 09:03:43),
while the run recorded each send as failed with `tools.NewResult: missing tool-call context`.
A bridged MCP tool builds its result through `tools.NewResult`, which refuses to run outside
a tool call, so every delivered message was reported undelivered and retried. That success
path had never run: until then the send had stopped earlier, at the empty recipient. The
scheduler's self-send now runs under a tool-call context of its own, with a reserved session,
as the tool pipe and the docs MCP already did. Email: the route resolved a tool named
`send_email`, a name left over from the retired mail MCP. The PIM that replaced it
multiplexes mail behind its `calendar` tool (`action: send_email`, `to` as a list; without an
`accountId` it picks the sending account itself). A route now resolves its tool by the
managed recipe the host mounted it from (`TrustedRecipeSource`/`TrustedRecipeTool`, as
message drafts already did), never by a registered name. Email defaults to the address the
identity signs in with: a user identity's name, which sign-in joins to the Authula email.
The task tool had refused the email route at schedule time with the missing-tool reason, and
the model relayed it verbatim. `stdout` and `telegram` reached the conversation; `none` left
nothing.

The operator confirmed that the WhatsApp and Telegram messages of both probes reached the
phone, so a message to one's own number does arrive. On revision `c57322064` (09:33 UTC)
every route delivered once: the bridge logged a single `POST /api/send` and no retry row was
written; the PIM logged `Email sent successfully` from the identity's one Google account to
its sign-in address; Telegram and `stdout` reached the conversation; `none` left nothing.
The task tool named both external recipients at schedule time, and the model repeated them.
The operator confirmed that this probe's WhatsApp message, Telegram push and email arrived.

This does not establish:
- which account the PIM picks when an identity has connected several, or one whose domains
  match the recipient;
- delivery when the bridge or the PIM is down: only the retry rows of the first probe were
  observed, and those failed for the identity defect fixed above.

A one-shot task that has fired is deleted (2026-10-05). Measured on the lab VM after the probes
above: every fired `at` reminder stayed `status=active` with `next_run_at` NULL, because firing
only clears the next fire. The cockpit board listed all of them as active, among eight system
sweeps and the database backup, each with a delete verb except the sweeps. The operator decided
that a fired one-shot, of any kind, is removed from the database together with its runs and
notification rows: the existing `ON DELETE CASCADE` from tasks to runs to notifications,
so the run ledger does not outlive the task. It is removed only once nothing is left for it to
do: no run still running, and no notification still owed a retry. A notification is settled once
delivered or past `AURA_SCHEDULER_NOTIFY_RETRY_ATTEMPTS`. The scheduler deletes such tasks on
every tick, after the notification sweep. What happened stays in the conversation the task was
scheduled from.

A cancelled one-shot is deleted by the same rule, fired or not. Measured on the lab VM before the
fix shipped: 17 cancelled `at` reminders, the ones the operator had removed from the board
(whose delete is a soft cancel), plus the 5 fired ones still active. Keeping only the fired
ones would have left the cancelled rows in the database for good. Cancelled recurring tasks are
not touched.

The cockpit board lists only operator-managed kinds (reminder, agent_job, the database backup);
the system sweeps no longer appear. The database backup cannot be cancelled: the board offers no
delete verb for it and the API refuses one with 403. The same measurement showed the system
sweeps owned by the operator's identity, which the enrolment hands every `local` row to, so the
agent's identity-scoped `task list` showed them and its `cancel` could stop them. On that VM the
backup had stayed `local`; where it was handed over too, the agent could have cancelled it. The
agent's list therefore shows only operator-managed kinds, and its `cancel` refuses the system
sweeps and the backup, by the same rule the board uses.

The cascade works under the application role, which has no `DELETE` on the run ledger or the
notification table (measured 2026-10-05, `TestDeleteSettledOneShots`, `aura_app` on a disposable
Postgres 18.4 with all 111 migrations). One `DeleteSettledOneShots` removed four one-shots: a
delivered one, one whose notification had exhausted its retries, one cancelled after firing and
one cancelled before firing, together with their runs and notifications. It kept a one-shot
whose run was still running, one with a pending notification, one with a failed notification
still under the attempt bound, one that had not fired, a cancelled one-shot with a pending
notification, a recurring task with no next fire, and a cancelled recurring task.

Measured on the lab VM at `efdc895c0`, then `174adbe23` (2026-10-05):
- The 5 fired and 17 cancelled one-shots were gone, with their runs.
- The cockpit board listed one row, the backup, with `Cancellable=false`, and its DELETE answered
  403 "the database backup cannot be cancelled".
- Three new reminders fired at 13:40:27, 13:41:27 and 13:41:57 UTC. Each was sent by the WhatsApp
  bridge and recorded in its conversation, and the same tick logged
  `deleted settled one-shot tasks count=1`. No `at` task was left.

This does not establish behaviour for a fired one-shot whose run never ended, which waits for
orphan recovery.

A reminder on a channel is scheduled, not sent (2026-10-05). Reported by an operator on his own
appliance and reproduced on the lab VM at `c57322064` with `gemma4:31b-cloud`. Asked "mandami un
promemoria su WhatsApp, scrivendomi ricordati di fare un test tra 10 minuti", the agent never
loaded the scheduler. It loaded the WhatsApp tools and searched the contacts for the operator's
name, then sent the text at once with `send_message` to a contact it picked.

The first fix rewrote the `task` summary and description, and it was not enough. The `task`
summary had offered "reminders ... later" but never said a reminder is delivered on WhatsApp,
Telegram or email, and on the tool search `send_message` outranked `task` for four of five
reminders that name a channel. The summary now says a reminder or message is sent to the operator
later on WhatsApp, Telegram or email, and `task` ranks first for all five. The description adds
that such a reminder is a task with `notify` set to that channel, never sent now with a messaging
tool, and never addressed by looking up the operator's contact. Probed on the VM at `efdc895c0`
(13:08 UTC) with the same request for 2 minutes: the model again loaded the WhatsApp tools
straight from the roster with `select:`, searched the contacts and drafted an immediate send.
The `<deferred_tools>` roster carries tool names only, so a summary feeds the search ranking and
nothing else. The only text that says what a deferred family covers before the model picks one
is the family line in the system prompt, and the scheduling line read "background tasks and
reminders". It now says that a reminder or message the operator wants at a later time is scheduled
and delivered then, even when the request names WhatsApp, Telegram or email. The connected
accounts line says those tools send now.

Probed on the VM at `174adbe23` with the same request three times, each in a new conversation:
all three scheduled a `reminder` with `notify=whatsapp` two minutes out, and all three were
delivered and deleted (above). Every probe still looked for the operator's number first: two
searched the WhatsApp contacts, two asked memory for it, and two tried to load the family name
"scheduling" as a tool. The turns took 4 to 7 tool calls and 12.6 to 24.4 s.

This does not establish:
- the choice on another model or with other phrasings;
- whether the WhatsApp server's own description, which does not say it sends immediately, also
  needs changing.

## 16. Observability and operator experience

Expose structured logs, traces, metrics, health and readiness. Process health does not
prove dependency readiness, instrumentation or scraping. Checks must detect a healthy-
but-blind observability pipeline.

Common infrastructure failures identify their shared cause without merging unrelated
ones. Rate-limit repetitions but restate persistent outages. Secrets and large raw
payloads do not belong in logs or audit previews. Cost uses provider evidence; unknown
cost remains unknown.

The cockpit shows actual settings application, job outcomes, limits, approvals and
document state. It does not imply unenforced controls. Identity read/write views agree.
Public documentation describes current behavior and measured limits rather than stale
phase counts or prototype APIs.

Localized surfaces render user-facing labels themselves. Wire status and approval
scope use stable machine codes, so translated labels or model-written wording cannot
alter the authorization they represent.

## 17. Deployment, backup and recovery

The appliance is Docker Compose. Installation validates target prerequisites, prepares
artifacts, generates configuration and selects CPU/CUDA embeddings from target hardware.
CPU configuration must also remove incompatible GPU reservations. Installation payloads
are verified against their manifest.

The appliance installer and updater enforce `single_user_hardened` with
`AURA_MUSR_ISOLATION=true` on a single-node appliance, including upgrades whose
configuration omits those keys or still selects a development profile. An explicit
`server_production` profile remains unchanged and retains its separate durability
and runtime prerequisites. Bare development Compose invocations keep their opt-in
profile. Measured 2026-09-14: the running appliance had neither key in `.env` and
therefore ran as `dev`; the installer's existing-file path never filled the pair,
and the updater did not migrate it. Config validation of that appliance under
`single_user_hardened` returned no violations. This does not establish multi-node
durability or validate the full live workload under the changed profile.

Edge and tagged releases have distinct publication contracts. Systemd appliance updates
may apply edge images automatically; pinned deployments have an explicit update process.
Acceptance records source revisions and image digests. Hot settings are not rollouts.

An edge appliance downloads every build on each tick but restarts into it only when an
admin (`identity.create`) asks from the cockpit, when Aura has been idle for 15 minutes
with no tool call or job running, or once the first pending build has waited 24 hours;
an admin may defer up to that deadline, and members see only the restart. The channel is
`/opt/aura/update`, bind-mounted at `AURA_UPDATE_STATE_DIR` (`/run/aura-update`): the
updater writes `status`, Aura writes `request` and a per-minute `activity` report, each
one writer per file by atomic rename, and a stale report reads as idle so a down Aura
never blocks the update that may fix it. A systemd `.path` unit on `request` starts the
updater at once; the updater enables it itself, so hosts installed earlier gain it on
their next tick. The policy lives in `/etc/default/aura` (`AURA_UPDATE_IDLE_SECONDS`,
`AURA_UPDATE_MAX_DEFER_SECONDS`, `AURA_UPDATE_ACTIVITY_STALE_SECONDS`). A host rebooted
onto the new image applies the rest without asking, because image and payload must not
run apart. Measured 2026-09-24 on the lab VM: the path unit started the updater about
300 ms after the write, and a trigger arriving while the oneshot ran was folded into it
and lost, so the updater re-reads `request` before it exits. The activity query ran
under 1 ms over 1M `tool_invocations` rows (Postgres 18 skip scan, no new index);
`conversations` is fail-closed under row security, so the last chat is read per
identity. Live on the same VM: the first build carrying the channel (`d47aadd3a`) applied
without consent, as it must, because the updater that pulled it predates consent; it
installed the path unit on the way and finished in about 2 minutes. The next build
(`e3ad18ab5`) applied on its own in 1 min 38 s with the reason "nobody is using Aura",
the activity report putting the last use 17 hours back. With a chat message sent first,
`18203eb74` waited instead; the admin's "defer one hour" reached the updater in the same
second through the path unit and was recorded 11 s later, after the tick's pulls, and
"update now" started the updater within 2 s of the click, restarted aura (down about
8 s) and finished the sidecars 1 min 52 s after the click, the page reloading itself onto
the new build. Not shown by these measurements: what a member sees (the VM has one
account), behaviour under a slow or failing registry, several appliances at once, and
whether 15 minutes suits real usage.

`aura.service` stops the stack with `compose down`, which removes the containers, so every
volume an image declares without a compose name is orphaned and the next start creates a new
one. Measured 2026-09-30 on the lab VM: 38 orphaned anonymous volumes, four of them 1.62 GB
whisper model caches next to the live one, on a disk 87% full; each stop had cost a 1.6 GB
model download. `aura-stt` now mounts the named volume `aura-stt-models`, and an apply ends
by pruning unused volumes that carry `com.docker.volume.anonymous`, so the named per-user box
workspaces a sandbox refresh detaches are never candidates. Still anonymous and recreated on
every `compose down`: SearXNG's rendered settings and cache, and ArcadeDB's `log`,
`replication` and `config`. The last holds `server-users.jsonl`, and losing it is healed at
boot: measured the same day with `systemctl restart aura` on the VM, the new `config` volume
started with root alone, two tenant binds were refused, and within four seconds `aura`'s boot
reconcile and `arcadedb-mcp` recreated the tenant user from its derived password (the second
create answering "already exists"); `aura`, whose boot fails on a reconcile error, came up
healthy. So `config` needs no backup. Not shown: an identity that is not an active human is
reprovisioned only on its first memory use. Separately, ArcadeDB logged 841 refused binds in
the six days before, from the memory backfill sweep probing identities without memory by
binding as them; the sweep now asks the admin's `DatabaseExists`, as `TenantClients.Existing`
already did, and a tenant walk without the admin pair is disabled rather than binding.

Postgres uses a seeded `0 1 * * * Europe/Rome` `backup_postgres` task, atomic dump promotion and 14-day
retention. ArcadeDB loads `docker/arcadedb/backup.json`, covers all databases including
new identities, and backs up every 60 minutes to a separate volume. Retention is
`maxFiles=60` with hourly/daily/weekly/monthly buckets of 24/7/4/6.

The restore drill covers Postgres, conversation sidecars, Garage and a tenant-shaped
ArcadeDB database, with checksum and cleanup verification. Restore targets are new and
verified. Existing archives must be testable without replacing live state. Cadence and
fixture timings are not production RPO/RTO guarantees.

Complete recovery needs originals, workspaces, runtime/integration state, configuration
and encryption/derivation secrets as well as databases. Volumes on one host do not cover
host loss; off-host retention is an operational requirement. No atomic cross-service
snapshot is promised.

Rollback starts with the recorded application/configuration image. Database rollback
requires compatibility evidence; otherwise restore separately and switch after validation.
Never overwrite the last known-good backup during recovery.

## 18. Engineering and acceptance

Measure dependency behavior on a disposable live stack before changing architecture.
Reuse native APIs and document the measured gap before building an alternative. Update
this contract from evidence and state its limits. Preserve operator data and concurrent
work throughout testing and editing.

Keep source small and separated by concern; remove dead paths and duplication on touch.
The 2026-09-08 dependency review measured failed npm installs for TypeScript7 with
the existing typescript-eslint peer range, and for separately upgraded Vitest/coverage
majors. The operator requested the updates after reviewing those failures. Move the
compiler and lint pipeline together to TypeScript7.0.2, Oxlint1.82 and tsgolint7.0.2001;
update Vitest and its coverage provider together. Preserve Node24 and matching types.
The official Oxlint migrator transferred202 rules with type-aware/nursery support;
retain import ordering and React Compiler config/gating through its native JS-plugin
interface. Strict parsing covers the retired duplicate-argument and octal rules.
This inventory proves rule mapping, not diagnostic equivalence or application behavior:
verify the retained rules, complete functional/E2E gates and the ordinary Aura image build.
Mutation execution belongs to CI under the operator's explicit instruction.
Migration numbers come from the directory at landing time. Commit generated sqlc output
with its defining queries. Current versions, artifacts, defaults and environment keys
live in manifests, the configuration registry, Compose and `.env.example`.

Verification exercises the affected real boundary. Unit tests do not replace integration
or mounted-MCP acceptance. Use realistic fixtures, race/leak checks, suitable properties
and disposable databases. Missing required environments fail under CI. Tagged compilation
proves compilation only; skipped execution is never a passing behavioral result.

Coverage has separate exact-candidate authorities:

- Unit/database owned-surface aggregate: at least 85%, with explicit inventory, target
  floors, named non-regression baselines and declared delegated authorities.
- Native Docker coverage: at least 85% for the declared owned sandbox/tool surface.
- Agent memory: live ArcadeDB package coverage at least 85%.

Use native coverage union, not percentage averaging or profile concatenation. Strong
packages cannot hide weak-package regressions. Unknown packages, wrong tiers, empty
evidence and mismatched revisions fail. Critical mutation checks require at least 70%
killed per declared boundary; non-compiling mutants are not assertion kills.

Vitest isolation stays on under Stryker. Measured 2026-09-27: without it the 52 mutation suites
ran in 9.3 s instead of 44.3 s in one Vitest run, but the CI Stryker step went from 39 min 42 s
(run 36308693762) to 39 min 13 s (run 36311844463), with all 4,042 mutants in the same status. A
mutant runs only the tests that cover it, usually one file, so there is little isolation to save.
Not measured: where the 39 minutes go.

A Go mutation scope is re-measured only when its inputs change. Measured 2026-09-28: the pinned
go-mutesting has no result cache, and with Stryker incremental (1 min 38 s on job 108847222120)
the eight Go scopes dominate: 20 min 28 s for 338 mutants in run 36382875100. Timed one by one in
WSL under load average 9-18 they took 1,626 s: elicitation_route 622 s (70 mutants), media_clamp
462 s (158), gateway 249 s (17), media_watcher 99 s (22), pausable 99 s (48), sandbox 69 s (4),
identity 20 s (13), profile 6 s (6), every count equal to CI's. The Skills job spent 7-11 min of
11-17 in go-mutesting (runs 36374190122, 36376741314, 36380553590, 36382875089): validator.go
2 min 10 s-3 min 23 s, writer.go 4 min 47 s-7 min 25 s, 71/82 each time. A scope now reuses the
result stored under the same sha256 over its file, go.mod, go.sum, the go env `go test` sees
(GOFLAGS carries build tags), the go-mutesting build, and every build, test, embed and testdata
file of the main-module packages in its `go list -deps -test` closure, all taken before any mutant
runs. The report names the measuring commit and fingerprint; release readiness accepts and lists
it. Replayed over 263 master pushes (2026-09-05 to 09-28), a scope is reused on 64% (gateway and
elicitation_route share one 1,245-file closure holding every other scope's package) to 98% of
pushes; media_clamp on 87%. Not proven: that a result is deterministic (a 10 s per-mutant timeout
counts as a kill, and pausable is one mutant above 70%), inputs a test reads outside its testdata
or from a live service, and the replay used today's closures for older commits.

Mutation evidence is measured in parallel (652ba82d7). Stryker, three groups of Go scopes and the
gate are separate CI jobs. The groups are balanced on cold CI durations: elicitation_route alone
(534-577 s), media_clamp with media_watcher (449-475 s), the five small scopes (201 s). Each scope
still runs alone on its runner. The gate job runs no mutant: it merges the groups' entries, fails
on a missing or stale one, and saves the cache once. A scope a group job measured on the candidate
has no `reused_from`. Skills measures validator.go and writer.go in two jobs beside its gate.
Measured 2026-09-28 on b52c69880 (CI run 36419041159, Skills run 36419041053) against 3dc8fd7ef
(runs 36412624433, 36412624404). Both measured the same six Go scopes and reused identity_isolation
and pausable; here the six closures hold internal/db and internal/db/sqlc, which a6033efad changed.
Before, the serial mutation job took 27 min 07 s (Stryker 8 min 01 s, Go 18 min 27 s) and ended a
28 min 36 s run. Now the longest mutation job is elicitation at 10 min 11 s, Stryker takes 4 min
20 s, the gate 39 s, and the run 16 min 06 s. The media group waited 5 min 07 s for a runner (29
jobs against the 20-job limit), so the gate still ended the run, 23 s after Agent Memory MRS. The
Skills run went from 17 min 51 s to 6 min 50 s: gate 5 min 00 s, validator.go 5 min 38 s, writer.go
6 min 20 s, both measured afresh (the per-file cache key had no older entry, and internal/db is in
their closure too). Not shown: this is one run each, so queueing and runner speed are not known in
general (writer.go took 276 s here, 509 s on run 36412624404); Stryker's work differs by push
(6,406 of 6,653 mutants reused here); and the gate failing closed on a missing group artifact is
shown by unit tests with fakes, not by a CI run.

MRS is an operational reliability score with a strict threshold above 96.5 and hard
gates for integrity, isolation, provenance, live MCP, embedding contract, abstention,
coverage and bounded latency. Retain cold samples; use at least 25 sequential calls
and p95 at or below 1,000 ms on the declared CLI/identity/MCP/search path. A perfect
score cannot override a failing suite. Diagnostics identify the failing suite/test.

Running-Aura answer evaluation is explicitly armed and requires its model, operator
and observability environment. Unarmed means NOT_EVALUATED, never PASS. Guided answers,
retrieval ranks, MRS and independent answer-quality benchmarks remain separate metrics.

Document evaluation judges answers through production tools and original-file access,
not a preferred filename. Declined corpora cannot be downloaded by CI/builds; the active
corpus policy and replacement oracles are in ADR 0045. Restoring an unused statistical
gate requires a consumer, permissible corpus and measured operating point.

A tagged release requires twelve current reports at the exact full candidate SHA:
security, unit/database coverage, Docker coverage, agent memory, mutation, capability,
load, chaos, disaster recovery, observability, rollback and audit closure. Evidence
older than 24 hours, missing reports and failed required gates block release. The
readiness report hashes its inputs; checkboxes and historical scores do not replace it.

The CLI subprocess used for idempotent commands must inherit the caller's standard
input. On 2026-09-14, `aura chat new` on the appliance printed its prompt and exited
without a turn, with both piped input and a TTY. Its child executor omitted stdin;
the native subprocess regression received zero bytes instead of two supplied lines.
`aura shell`, which uses the same Runner without that subprocess, completed the
operator's sandbox command and wrote all three identity-private caches. This measures
input transport and sandbox execution, not CLI retry semantics or HTTP authorization.

## 19. Evidence, exclusions and remaining limits

The 2026-09-07 backup check passed four restore planes and restored one real scheduled
memory archive into a disposable database. Temporal memory passed 11 mounted-MCP cases;
the six-query context comparison improved retention of direct evidence. Eighteen guided
Codex answers met their stated criteria. These bounded observations establish neither
universal quality nor production scale.

Deployment-dependent limits remain visible: native isolation differs from Docker
Desktop; stdio MCP is not automatically sandboxed; installer and mounted-server trust
must be accounted for; async work has a side-effect crash window; local volumes need
off-host protection; capability metadata is not a generation benchmark; and independent
answer acceptance is distinct from backend correctness. Implementation gaps remain gaps,
not silently weaker requirements.

Retired architectures are excluded: Neo4j memory, independent competing memory runtimes,
the old adaptive shadow control plane, the unused semantic-compaction rollout engine,
`Agent.md` profiles, file-backed MCP registry and Docker MCP launch kinds. The current
compaction, explicit reasoning projection and durable delegation described above remain
in scope. A future planner/executor hierarchy, provider-native subscription integration,
new graph-ranking policy or full transaction-time history needs a measured new contract.

Maintained references:

- [Architecture](docs/ARCHITECTURE.md) and [capabilities](docs/CAPABILITIES.md).
- [Backup and restore](docs/BACKUP-RESTORE.md) and [recovery evidence](docs/launch-validation-2026-09-07.json).
- [Memory graph validation](docs/memory-graph-validation.md), [retrieval validation](docs/memory-retrieval-validation.md) and [concurrent deletion](docs/memory-concurrent-delete-validation.md).
- [Document ingestion](docs/document-ingestion.md) and [corpus policy](docs/adr/0045-evaluation-corpora-licensing.md).
- [Release readiness](docs/release-readiness.md), [CI](.github/workflows/ci.yml) and [quality measurements](docs/aura-quality-snapshot.md).
