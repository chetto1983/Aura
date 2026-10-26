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

**Aura is the host of the year's work, decided 2026-10-09.** The operator's other
repositories of the twelve months to this date (PMSync, StickyFlow, wpt-iot, sacchi_agent,
Market_MCP, the memory MCP already absorbed, and the OpenDots and SVAR readings of the same
day) feed Aura behaviours, never code: each is a different stack, and two were built at a
quality bar this product no longer accepts. The inventory, ranked and gated by a lab-VM
measurement each, is `docs/superpowers/specs/2026-10-09-consolidation-best-of-design.md`;
the first three behaviours have their own designs and their paragraphs in §5, §12 and §16.
This records a decision and a static reading of eight repositories, not a measurement of
any behaviour on the live stack.

**PII model-egress investigation, measured 2026-10-12/13; not shipped.** Synthetic
cloud probes intercepted 20 known clear occurrences before transmission, then observed
zero after masking and exact restoration in 4/4 tool-argument/text cases. A separate
component benchmark reduced Alcatraz detector calls from 730 to 65 across twenty
growing turns; already-masked block caching reduced the later-turn median from 313.73
to 19.82 ms. Rizzo still took 6.77–7.76 s for a new tool block despite cached history.
The source/runtime audit found that direct Excel/PDF opening bypasses ingestion-only
masking, provider serialization adds media, and non-chat model clients have independent
connections. The [proposed model-egress spec](docs/superpowers/specs/2026-10-13-pii-model-egress-design.md)
therefore places enforcement on the final request before HTTPS and keeps original local
retrieval inputs, with scoped restoration before tool consumers. It contains the target
components, proposed configuration and release gates; these are not an implemented
contract. [Transport evidence](docs/superpowers/verification/2026-10-12-pii-alcatraz-rizzo-lab-vm.md),
[cache evidence](docs/superpowers/verification/2026-10-12-pii-cache-lab-vm.md) and the
[egress audit](docs/superpowers/verification/2026-10-13-pii-egress-audit.md) do not establish
real-corpus recall, durable-vault isolation, full-agent acceptance, second-provider
parity, complete media coverage or production latency. Spike 109 remains pending.

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

**Administering the deployment takes `identity.create` (2026-10-10).** D-01
grants every identity `governance.write`, but about twenty routes kept using it as the
admin gate, and the cockpit hid them with `identity.create` while the server let any
member through. Measured on 2026-10-10 against the real parent mux (`newServeHandler`,
`RequireCapability`) with a principal holding exactly `identity.UserSet()`: a member
reached all 14 routes below, among them revoking the admin's capabilities, lifting their
own credit cap, restarting the daemon and installing a stdio MCP server that runs as a
child of Aura in its container. They now take `identity.create`:

- `/api/admin/*`: the roster, grant and revoke, the activity feed, credit caps, spend,
  restart, the OpenRouter reconcile and the update decisions (removal keeps
  `identity.delete`);
- the MCP registry writes: install, env, trust, enable, disable and remove. The registry
  is one per deployment (migration 0101) and its servers mount for every identity. A
  member keeps their own authorization on a server;
- `PUT` and `DELETE /api/settings/{key}`: `aura.settings` has no identity column, so every
  key configures the deployment, `TELEGRAM_BOT_TOKEN` included. The per-key admin list
  goes: every key is the admin's.

The cockpit needed no change: it already sends a non-admin away from Governance and Settings,
deep links included (`AppShell`, `ADMIN_MODES`). A member keeps `governance.write` for what is
theirs: their skills, their MCP authorizations, their ChatGPT plan and calendar accounts.

Measured again the same day on a running daemon (`aura serve` from master and from the fix, on
Postgres and ArcadeDB, Authula sign-in in Chromium): on master the member revoked a capability
from the admin, replaced the deployment's Telegram bot token and had the daemon spawn
`touch /tmp/aura-member-exec-proof`; with the fix all 18 calls answered 403 and the admin
reached every handler (`docs/superpowers/verification/2026-10-10-member-deployment-administration.md`).

This does not establish:
- that a member reaches nothing else of the deployment: an agent's background shells can be
  polled or killed across identities on `governance.write`. The scheduler, the house skills
  and the skill install, open when this was written, are the next paragraph;
- the appliance: the member was inserted rather than provisioned, the daemon ran on the host
  with `AURA_PROFILE=dev` and isolation off, and nothing ran on the lab VM.

**A member's schedule is their own; the house skills and the skill install are the admin's
(2026-10-10).** Measured the same day on the same stack, the member holding exactly
`identity.UserSet()` (`docs/superpowers/verification/2026-10-10-member-scheduler-and-skills.md`):
the scheduler board listed every identity's tasks; the member rewrote the goal of the admin's
`agent_job`, ran it (a run executes as the task's owner) and cancelled it, and made the
nightly `backup_postgres` yearly; archived, restored and deleted a house skill; and
`POST /api/governance/skills/install` spawned `npx skills add <the member's source>` as uid 0
in the daemon's process tree, where the child read the daemon's `/proc/<pid>/environ` (the
database URL, the Authula secret, the OpenRouter key, the ArcadeDB secrets) past the filtered
environment `mcp.InstallerEnv` hands it. Now:

- the board shows a member the tasks whose `identity_id` is theirs, and answers every verb on
  another identity's task, its run history included, with the 404 of a missing one. An admin
  (`identity.create`) sees and manages every task, the backup included. A member keeps every
  verb on their own reminders and jobs;
- writes reach the house library (`AURA_SKILLS_DIR`) only for an admin. Commit `e1118908b`
  (2026-09-07) opened them to `governance.write`, read then as "may change shared deployment
  configuration"; D-01 gives that to every identity. A member keeps authoring in their own root;
- the cockpit's install and catalog routes and the `install` action of the agent's
  `skill_manage` take `identity.create`.

Measured again on the same stack with the fix: as the member the list came back empty, every
verb on the admin's job and on the backup answered 404 with the database unchanged, the house
verbs failed in the member's own root (400, "no active skill by that name") with the house
untouched, and the install answered 403 with no `npx` spawned; the admin kept the full list,
the edit, the run (a run row written), house archive and restore, and the install.

Measured on the lab VM the same day, with a member provisioned through `aura identity create`
and purged after (`docs/superpowers/verification/2026-10-10-lab-vm-member-governance.md`).
The member got the same refusals, plus a 403 on the `/api/admin/*` reads. The admin passed
every gate it was sent through. The backup was read back unchanged. The run also found the
board's pause and resume never mounted on the parent mux since they shipped (2026-10-07); both
now reach the handler.

This does not establish:
- that an admin's install is contained: it still runs as the daemon's user, root in the
  appliance image (`id` in the lab VM's `aura` container, 2026-10-10), and can read the
  daemon's environment. Closing that takes a non-root daemon or secrets mounted as files, not
  a capability;
- an owner rule in the store: the check is in the board's handlers, `scheduler_tasks` has no
  RLS, and the agent's `task` tool keeps its own owner scoping, not re-measured here;
- background shells across identities, still open;
- on the lab VM: a member acting on a task of their own, an admin's edit or run of the real
  backup, and an install that fetches a source. None of the three was made there.

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

**A tool policy per identity, `ask` and `deny`, decided 2026-10-09 at `1c872a1`.** Read against
OpenDots' per-Dot tool toggles (CopilotKit, MIT, released 2026-10-01): every MCP tool can be
turned off for one Dot, and every tool not marked read-only starts with "Ask first" on. The
gateway's three scopes, `once`, `session` and `always` (amendment #127), all widen: an operator
can stop being asked, and cannot ask to be asked more, nor forbid one tool. Measured in the
tree, not on a live run:
- only the Destructive tier stops a turn (`gated`, `internal/gateway/decide.go`), and a
  read-only call is recorded as a decision fact and never reserved;
- the tier of an MCP tool comes from the server's own annotations or a recipe table
  (`internal/agent/mcptools/bridge_risk.go`), so a server that declares a write read-only
  runs it ungated and unreserved, and nothing lets the operator correct that one tool;
- the only per-identity row the gateway reads is the `always` grant
  (`aura.gateway_approval_grants`, migration 0099); the registry toggles whole servers and
  profiles (`aura mcp profile`, `/api/governance/mcp/{name}/enable`), never one tool, and no
  file in `internal/gateway`, `internal/mcpregistry` or the cockpit names a per-tool policy.

The decision: a durable per-identity policy keyed like a grant (identity, tool, multiplexed
action) with two values. `deny` refuses the call at `Decide` with a reason the model reads.
`ask` routes the subject to approval whatever its tier, takes it through the reservation
funnel, and its prompt offers no `always`. Precedence is deny, then ask, then grant, then
tier. A policy read that fails falls to the prompt, as a grant read does. It applies under the
strict profiles only, since `Decide` is a no-op under dev and local_trusted; it binds at the
next decision and does not interrupt a running call. No agent tool writes a policy. Design:
`docs/superpowers/specs/2026-10-09-tool-approval-policy-design.md`. This reading does not
measure a live turn: how a model behaves when a tool it was offered answers "disabled by
policy", whether it retries or tells the operator, is for the lab VM, and so is the cost in
operator prompts of `ask` on a busy read-only tool.

**A denied tool on the lab VM, and a confirmed draft that passes a deny, 2026-10-10.** With
gemma4:31b-cloud, `deny` on `web_search` and ten questions that needed it, run twice an hour
apart: every turn called it once and never retried. The first run told the operator it was
disabled in 2 of 10 turns and answered anyway in 8, seven of them through the agent-browser and
one through `web_fetch`. The second told the operator in 6 and went to the browser in 2. A deny
stops a tool, not what the tool does, and how often the model says so varies from run to run. `deny` on `calendar__calendar` action `send_email` did not stop an email. For a
trusted outbound tool the agent drafts the message before the gateway
(`withholdOutboundMessage`), so the model never hears of the policy, and once the operator
confirms the draft, `Decide` allows the send through `decideReviewedMessage`, ahead of the policy
check (`internal/gateway/decide.go`). The send carried `gateway_verdict: allow` while the deny
was set. The operator decided that this stands: a drafted message leaves only on their own
confirmation, which is the stronger statement, so a confirmed draft passes a `deny`. Ledger:
`docs/superpowers/verification/2026-10-10-lab-vm-wave1-acceptance.md`. Not shown: a second
model; an outbound tool that is not drafted; whether the cockpit should tell the operator, as
they confirm, that a deny is set on the tool.

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

Turn recall shipped 2026-10-07 (plan 2, `1bb3c0630`..`05ccf5906`; spec amended
`2952f7a5d`/`9b4d58eee`). The turn is read once, before its first request, against the
typed message, never the catalog-composed one. The first matching row decides:

| Situation | Effort | Source | Teacher |
|---|---|---|---|
| the composer set an effort | that effort | `user` | — |
| standalone greeting | `none` | `greeting` | — |
| a compatible `user`, then `teacher`, label within cosine 0.10 | its requested effort, clamped now | `memory` | — |
| seed margin ≥ 0.075 | the seed tier | `seeds` | — |
| seed margin < 0.075 | the seed tier | `seeds` | asked in the background |
| no classifier | static `low` | `fallback` | asked in the background |

Remembered tools of the nearest compatible tool turn (at most 3) are loaded before round 1.

**The teacher no longer blocks the turn.** It used to run synchronously within 2 s. On the
lab VM (image `2882a46bf`, `gpt-5.6-sol`) none of 6 attempts answered inside that bound.
Every attempt ended at 2001–2003 ms, which is where the bound cuts in, so the timeout and
not the answers was the limit. The route's own time to first token is 1.8–2.5 s. A failed
teacher labels nothing, so memory never learned.

Since the amendment, the runner asks the teacher after the turn's decision is written. It
uses the auto-title worker pattern: `wg.Go`, a detached context, a 30 s bound and the
breaker. The same code path runs for every provider, and a successful answer upgrades the
recorded decision to a reusable `teacher` label.

Measured after rollout (image `d633f8b9d`, `docs/verification/turn-recall-vm-e2e-2026-10-07.md`):

- **Teacher latency.** On `gpt-5.6-sol` it took 6.8 s and 8.3 s, 2 answers out of 2. On
  `gemma4:31b-cloud` via Ollama it took 0.4 s, and every attempt answered.
- **End-to-end checks.** All passed on both routes:
  - a paraphrase B was decided by `memory`, with its origin on turn A, at 0.0738;
  - deleting A's conversation made the label disappear;
  - another context did not reuse it;
  - a label learned on one route was not reused after the operator switched routes.
- **Warm/cold workload** (5 fresh families, Ollama):
  - memory decided 3 of the 5 warm turns; in W1 and W3 the preload removed `tool_search`
    entirely, saving about 18k tokens (−33%) each;
  - median completion was 11.9 s cold and 11.0 s warm, and no warm turn missed an
    outcome its cold pair met;
  - the slowest-turn gate failed on one pair, W4: an unanswerable price question whose warm
    turn browsed for 80.8 s against 25.8 s cold. Repeating W4 three times, each from a clean
    slate, gave warm 22.7 / 11.4 / 23.7 s against cold 19.9 / 46.6 / 15.1 s. Every warm
    turn was decided by `memory`, and none called `tool_search`. Warm used 23% fewer tokens
    in total. So the 80.8 s did not reproduce. Single pairs still go either way: r3's warm
    turn was slower than its cold one and used 62% more tokens. The depth of a search on a
    question with no answer varies more from run to run than memory changes it.
- **Frozen evaluation, calibration split** (51-turn set, 3 trials, `gemma4:31b-cloud`):
  - all three arms scored 60/66, with 0 hard→none;
  - memory decided 3/66 readings, all of them correctly;
  - the teacher answered 24/24 (p50 0.40 s, p95 2.2 s), and recall took 30 ms at the median.
  - The final split was not run: memory fires only once per trial in calibration, so the
    memory-never-ran gate would fail for lack of opportunity. The set needs more
    same-language uncertain pairs, in a new dated file.

What this does not show:
- a production label rate: one operator, about 30 measured turns on two routes;
- the 0.10 radius beyond paraphrases. Pairs measured 0.037–0.100, and two of five fell outside;
- cost: both routes report no per-token cost, so cost is unknown, not zero;
- the final-split release criterion, which is still unrun;
- preload fit across models. It replays the recalled turn's tools, not the current model's
  preferences: gemma was handed `web_fetch` from a gpt turn, and still searched for
  `web_search`. That carries over to plan 3, "intelligent tools".

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

Amended 2026-10-23, measured on the lab VM with the chat on ChatGPT
(`docs/superpowers/verification/2026-10-23-cloud-service-credentials.md`):
- **The finding.** The daemon's embedders and the cloud speech clients took the services key
  from the chat profile, and the ChatGPT profile clears it. The embedding space had no name,
  the memory embed backfill failed every five minutes, and every `document_search` answered
  `lexical_only`.
- **The rule now.** The shared services read the services key from the settings on every
  call, through one helper (`settings.Credentials`), and send it only to OpenRouter.
  - Vision through a primary on any other host sends no key.
  - A manual embedding endpoint has its own sealed key, `AURA_EMBED_CLOUD_API_KEY`, and never
    receives the services key.
- **After `d46cedab6`.** The space is named, the backfill runs clean and a real agent turn's
  `document_search` answers `complete`.
- **What this does not prove:**
  - no manual endpoint was tried;
  - the per-call read's cost is unmeasured;
  - cloud speech was not called live.
- **What the same run found.** A ChatGPT primary gives the media indexer no vision route: the
  capability source has no ChatGPT case. A scan then goes to the local OCR sidecar, which the
  VM does not run. The operator decided that the chosen model reads images whenever it can,
  ChatGPT included. It landed on 2026-10-25: §11, "The chosen model reads images".

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

Tool-only reasoning traces and the ArcadeDB floor, 2026-10-06 (`799880d07`..`7838d194b`).
The first valid terminal tool event opens a reasoning trace, so a turn that exposes no
reasoning still records the tools it ran. An empty provider summary is accepted only
where tool calls stand in for it; no reasoning text is synthesized. A discarded round
drops its reasoning and keeps the calls earlier rounds executed, and a set-aside
tool-only trace is not counted as rejected by the embedding report. `TenantClients.For`
refuses an ArcadeDB older than 26.10.1 before it provisions or returns the first tenant
client (ArcadeData/arcadedb#8959, GHSA-h2j4-28h8-cj5v). It reads the version once per
resolver, with the admin when present and otherwise the tenant credential, which
26.10.1 allows (measured live); a refusal is retried on the next call.

Measured on VM 192.168.101.158 after the updater rolled out `b28f34af1` at 20:19 UTC:
ArcadeDB 26.10.1, no floor refusal, conversation projection healthy. Before the change
the graph reached 9 of 16 tool turns. Four reminder turns were then driven through the
cockpit. On Gemma 4 31B over Ollama at effort `off` the turn exposed no reasoning, and its
trace was stored with an empty summary, one step and both of its tools (`tool_search`,
`task`). On `gpt-5.6-sol`, whose ChatGPT backend offers no `off` effort, every completed
turn exposed reasoning: the turn that finished in a single run reached both of its tools;
the one paused by `ask_user` reached only the `task` run after the answer, because a
paused run commits no trace; the one whose second model call hit `stream_open_deadline`
at effort `low` (120 s) committed none.

What this does not show: four turns on two routes are not a coverage rate. Tool calls
made before an `ask_user` pause or in a failed run remain outside the graph. The backfill
pass had not yet run on the new trace, so its one-time set-aside rests on the live
ArcadeDB test, not on the VM.

Turn recall memory, 2026-10-07 (plan 2, `1bb3c0630`..`05ccf5906`).

- **Postgres row.** Each user turn records how its effort was decided on its own
  `aura.conversation_turns` row (migration 0137). The columns are the context key, the effort
  sent, the effort requested before the clamp, the source, the route key, the policy version
  and the origin of a reused label.
- **ArcadeDB vertex.** The projection carries the same seven fields to `ConversationTurn`
  as `recall_context_key`, `effort`, `effort_requested`, `effort_source`, `effort_route_key`,
  `effort_policy_version` and `effort_origin_ref`. A NOTUNIQUE index covers the context key.
  `ConversationTurn` gains these projected fields and nothing else, so the "memory only" scope
  of 2026-07-31 holds.
- **Three recall pools.** `RecallTurns` reads user labels, teacher labels and tool turns. Each
  pool filters on the context key, the route and the policy before top-k, never after, so
  closer incompatible rows cannot crowd out a compatible one. The tool pool is not route
  filtered, because tools do not depend on the model.
- **What counts as a label.** Only `user` and `teacher` decisions are reusable labels. A turn
  decided by `memory`, `seeds`, `greeting` or `fallback` copies or guesses, and never teaches.
- **Teacher labels.** The background teacher upgrades a recorded `seeds` or `fallback`
  decision to `teacher` through a conditional write. Its periodic reconciliation replays from
  the start, so the upgrade reaches `ConversationTurn` without a new projection path.

Measured on VM 192.168.101.158 (`docs/verification/turn-recall-vm-e2e-2026-10-07.md`):
- **Labels reached the graph:** 15 s and 55 s after the teacher answered.
- **Deletes reached recall:** a deleted conversation left recall within 65–68 s on three
  deletions.
- **Origins were readable from Postgres:** a reused label's origin pointed at the right user
  turn, on `gpt-5.6-sol` and on `gemma4:31b-cloud`.

What this does not show:
- the lag under load: one identity, a handful of turns at a time;
- whether the 0.10 radius is right beyond paraphrases. Paraphrase pairs measured 0.037–0.100
  apart, and two of five warm/cold pairs fell outside the radius;
- the set-read `LIMIT` truncation parked in plan 1, which is unchanged.

ArcadeDB 26.10.2 on the JVM, 2026-10-23
([verification](docs/superpowers/verification/2026-10-23-cocoindex-neo4j-connector-and-arcadedb-26-10-2.md)).
- **The pin and the floor.** The compose pin moves from 26.10.1 to 26.10.2. The floor in
  `TenantClients.For` stays 26.10.1, because no statement needs 26.10.2.
- **Aura's live tier.** On throwaway servers from both digests, the `arcadedb_integration` tier
  (`internal/arcadedb`, `internal/docfields`) gives 613 passes and the same 5 embedder skips on
  each, with the same 8 server errors.
- **The lab VM's data.** Its databases were restored from their archives under 26.10.1. They open on
  26.10.2, and back on 26.10.1, with identical types, indexes and record counts, and the same 33
  vector and full-text answers.
- **The release's `UPSERT` rule** (#9339, a UNIQUE index on every property of the WHERE) holds for
  every Aura upsert.
- **Constraint names.** 26.10.2 records the names of Cypher constraints (#9707). The cocoindex
  connector's `DROP CONSTRAINT` therefore removes the unique index it created; on 26.10.1 it was a
  silent no-op.
- **The ingest schema stays SQL.** ArcadeDB's Cypher still refuses vector and full-text indexes, a
  vector property type and database creation, so `services/ingest/arcade.py` keeps writing them.

- **CI and the lab VM.** CI on the pin passed. Agent Memory MRS scored 100/100 with
  `arcadedb_version` 26.10.2. The updater moved the VM, whose schema and indexes read back unchanged.
  The real-agent E2E scored 10.0 (13/13): document search with citations, memory write, recall from
  a new conversation, and period recall of the day's turns.
- **A timed-out recall now costs one graph build.** 26.10.2 terminates a commit whose HTTP client
  has left. The first turn recall after a restart missed its 500 ms budget while the server rebuilt
  the `ConversationTurn` graph; the server dropped that graph, and the next query rebuilt it in 9 ms.

What this does not show:
- the operator schema's types newer than 2026-10-13, opened from 26.10.1 data;
- a cold graph build on a large memory against the 500 ms recall budget;
- any load.

The native image stays with the 2026-10-17 spec, which waits for 26.11.1.

## 11. Documents and media

Measured 2026-10-21 on a disposable database in the running ArcadeDB 26.10.1:
`source_key IN (SELECT source_key FROM Passage WHERE SEARCH_INDEX('Passage[text]', :term))`
returned one indexed document for 55 matching passages. Library content search uses this
uncapped membership predicate together with existing metadata filters, rather than agent
retrieval's top-k. Literal phrase parameters keep the simple search from becoming a Lucene
query language. Filename search remains the compatible default. `source_key NOT IN (SELECT
source_key FROM DocumentFields WHERE reviewed_at IS NOT NULL)` included the document with no
field record and excluded the reviewed one: Inbox is independent of suggested values.
`values CONTAINS (field = :field AND value_key = :value)` matched the stored custom value;
custom filters initially expose exact value/unassigned and preserve the existing review filter.
`ORDER BY ordinal SKIP 50 LIMIT 6` returned the last five of 55 indexed passages. The Content
API checks document membership first and returns at most 50 passages, with lookahead for paging.
It displays indexed content, which may overlap or omit original formatting; it is not lossless OCR.

Saved document views use tenant-local `DocumentSavedView` named records in the existing
ArcadeDB schema, with version 1 URL queries (maximum 16 KiB), validated library parameters,
1–80 character names, table/compact/cards display and pin/dashboard flags. Source-key filters
support exact uploaded-object lookup without inventing queue records. No Postgres migration
or new environment setting is needed. These measurements establish SQL behavior on synthetic
fixtures, not latency or memory bounds on a large archive, ingestion status, OCR fidelity or
browser acceptance. Evidence:
`docs/superpowers/verification/2026-10-22-paperless-documents-acceptance.md` (final acceptance).
The Task 1 measurements were written to the plan's git-ignored workspace, which was not kept.

Measured 2026-10-21 on disposable ArcadeDB 26.10.1 databases: the Documents library's
`file_type` filter accepts `all|documents|pdf|word|spreadsheet|presentation|text|image|video|audio|code`.
Omitted or empty means Documents across GET, bulk selection, saved queries and direct store
calls; All files is explicit. A 27-file mixed fixture returned exactly the 12 PDF/Word/
spreadsheet/presentation/prose documents for the default and all 27 for `all`, with identical
List/Select membership, exact totals and stable paging. One extension inventory supplies both
bound case-insensitive SQL suffix predicates and `DocumentRow.file_type`, including `other`
for unknown extensions. Code reuses `filecard.SourceExtensions()` plus the existing separately
handled JSON/XML/HTML/HTM formats; prose TXT/Markdown stays Text. Broader library categories
identify indexed files independently of the narrower upload-admission allowlists. Type inference
does not inspect MIME or file bytes: ambiguous suffixes such as `.db` retain their registered
source category even if the actual bytes are binary. These checks establish grouping and filter
composition on synthetic records, not extraction/editor support, large-corpus latency or browser
acceptance. Extraction is unchanged. Evidence:
`docs/superpowers/verification/2026-10-21-paperless-document-file-types.md`.

Identity-bound originals live in Garage. The ingestion supervisor resolves existing
provisioning bindings and manages CocoIndex workers. Live refresh actually rescans
sources. Add/modify/delete reconciles cards and passages; source identity and scope
agree between Python writers and Go readers.

Measured 2026-10-21 with the deployed Craft CLIs: isolated native processes rendered
recognizable 480-pixel PNG previews for PDF, DOCX, XLS, PPTX and JPEG. Documents thumbnails
reuse these apps inside the caller's existing SandboxRouter box: PdfCraft native batch
`render_preview`, WordCraft PDF conversion, GridCraft first-sheet fit-to-page PDF export,
DeckCraft slide-zero PDF export, and PhotoCraft's native `serve` preview. A subsequent live
FilmCraft probe rendered a 1920x1080 MP4 as a recognizable 480x270 PNG (70,076 bytes) in
1.912 seconds including Docker transport. Video previews reuse FilmCraft's native probe,
isolated project import, sequence creation from the returned item id and one-frame render;
metadata dimensions are checked before frame import. Data/preset directories remain inside
the request scratch directory. This one MP4 does not establish every codec/container or
offline-media behavior; failed or unsupported videos retain their full viewer. Each request has
its own scratch directory and process; the mounted agent editor session is never used.
The authenticated document lookup precedes source access. Source bytes stream from Garage,
using the object's current size, with no user-provided source URL or host renderer fallback.
Thumbnail responses are PNG and private/no-store; no cross-request preview cache exists.
Input/output, concurrent work and elapsed conversion time are bounded, and output dimensions
are checked before pixel allocation. Unsupported or failed previews retain the full viewer.
These sample measurements do not establish fidelity for every supported Office import,
large-archive latency, cold box startup time or safe decoding of every malformed native input;
Word converts every page before previewing the first, spreadsheet fit-to-page is a first-sheet
overview, and native font substitution remains visible. Evidence:
`docs/superpowers/verification/2026-10-21-craft-document-previews.md`.

The cockpit opens and edits a file in the Craft app for its kind (measured 2026-10-19/20; the
versions moved on 2026-10-24). aura serves `AURA_APPS_DIR/<app>` (`/usr/share/aura/apps` in the
image) at `/apps/<app>/` under its own page policy. GridCraft is its official release zip, checked
by SHA-256. PdfCraft, WordCraft, DeckCraft, PhotoCraft and FilmCraft are built from their tags plus
`docker/craft-web/<app>.patch` in `docker/aura-craft-web`, and the aura image copies a pinned build.
One frame, `CraftFrame`, speaks the apps' `?host=parent` bridge. `<app>:save` stores the bytes in
the library under the file's own name. While `<app>:dirty` is true the frame says the edit is
unsaved, and the preview holds its first close. On the lab VM (build `5d12271d7`, 2026-10-20) the
operator confirmed the pass in the cockpit. An edit reached its file, checked outside the app, for
WordCraft and PhotoCraft by the operator's hand and for GridCraft and DeckCraft by a driven
browser. At the 2026-10-24 versions each app again opened, edited and saved a real file through the
bridge, in a dev container. These runs do not show PdfCraft or FilmCraft edits on the VM, WebGPU,
Firefox, Safari or phones, memory use, or more than one small file per app. Evidence:
`docs/superpowers/verification/2026-10-19-craft-web-apps.md`,
`2026-10-24-craft-apps-upgrade.md`.

Documents workspace amendment, recorded 2026-10-22 before Task 3 implementation:
the live Paperless comparison and frontend inventory measured a folders-first entry,
no document route, and a 17 px filename column at 390 px. The running content-search
probe returned 61 distinct documents across four pages from 122 matching passages;
native Craft produced PDF/Office/image/video previews. These are the reasons to route
Overview, Library, Inbox, Processing, saved views and organization within Documents,
use compact rows on narrow screens, and explicitly request content search and the
Documents file family. React Router's installed useBlocker requires a data router;
the existing route table will use createBrowserRouter without replacing query fetching.
File manager uploads return the stored absolute object ID; each queue item needs its
own uploads/UUID/ prefix, exact source-key polling with file_type=all, and identity-scoped
storage/cancellation. Ingest counters measure components, not files. The measurements
do not establish the new frontend's usability, refresh behavior or ingestion latency;
those require Task 3 tests and controller-run live acceptance. Evidence:
`docs/superpowers/verification/2026-10-21-paperless-comparison.md`. The frontend inventory and
the comparison screenshots were written to git-ignored folders and were not kept.

Task 3 implementation inventory also confirms that library facets enumerate only type,
counterparty and tag values. Custom-field Diff.set works for both explicit and filter-wide
selections; Diff.remove needs the actual held value_key, available for explicitly selected
rows only. The UI therefore offers custom clears on explicit selection and explains how
to select documents when a filter-wide selection cannot clear them. No clear-by-field
backend operation or typed-value coercion is inferred from the existing Diff contract.

Measured 2026-10-22 during review: closing the execution stream after SIGTERM did not prove
native process exit; a TERM-resistant child was still running when cancellation returned.
The shared execution mechanism now escalates TERM to KILL, checks process identity and exit,
and keeps Kill nonblocking for registry callers. The live regression confirmed the child PID
was absent before return. If Docker prevents exit confirmation, previews fail closed: close
the source, retain scratch and admission, and promptly refuse queued/new previews. Recovery
requires confirmed process-tree or box cleanup before resetting the service; a blind restart
is not confirmation. This deliberately trades thumbnail availability for bounded outstanding
work under infrastructure uncertainty, without changing extraction or the full viewers.
Evidence: `docs/superpowers/verification/2026-10-22-craft-preview-cancellation.md`.

`GET /api/documents/ingestion` exposes the existing tenant-local IngestState reader as
`{indexing: state|null}`. Status, observed_at, in_progress, finished and errors keep their
existing meanings: counts are work components, not files; ready can coexist with errors.
A missing state is null and a failed read is an error. Extraction and its publisher remain
unchanged; these aggregate counters cannot identify a particular failed upload.

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

**Document floors for pplx-embed-v1-4b, measured 2026-10-23.** Space `es1-b604719dc5fc0ce4`,
the operator's 8 documents, 29 questions: 24 written by Aura's agent on the operator's request
to collaborate, plus the five out-of-corpus questions of 2026-09-09.
- **The bands.** Each question ran twice in one `aura docs search` process with
  `AURA_DOCUMENT_MIN_RELEVANCE` and `AURA_DOCUMENT_DENSE_MAX_DISTANCE_RATIO` lowered, while the
  daemon kept its floors. True top-1 matches scored 0.4047..0.6523, out-of-corpus questions
  0.2637..0.2961, and "fattura" 0.3833 on an unrelated Markdown example.
- **The floors.** Relevance 0.39, the operator's choice over 0.35: it drops "fattura" and no
  measured true match, on a band 0.0214 wide. The 0.72 distance dropped no true match and stays.
- **Per family.** The floors table is one per family from this measurement on, because memory
  was not measured. This space's memory keeps EmbeddingGemma's floors and still says
  `uncalibrated_floors`, and the cockpit's `floors_calibrated` is true only when both families
  are measured.

This does not prove: a library beyond 8 documents, questions written by someone who had not read
the documents, short queries in general (one word scored high against a card), or exact phrases,
which the cosine rerank can bury (0.199 for a font named verbatim in the DOCX). Record:
`docs/superpowers/verification/2026-10-23-document-floors-pplx.md`.

On the lab VM on v1.2.0 (2026-10-25), the agent's own check confirmed that "fattura" abstains,
alone and with the tag `Cliente`. It also confirmed the fixes for the tools it had got wrong
while writing the questions:
- a listing with nothing to narrow it returns all 8 documents;
- Office files open with their real media type;
- `memory-aura` arrives whole.

This proves abstention on those two queries only. Record:
`docs/superpowers/verification/2026-10-25-document-tools-fixes-lab-vm.md`.

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

**Source files (measured 2026-10-16).** The extractor routed only office, PDF and markup
suffixes, so a `.go`, `.py` or `.st` file was indexed by name only. Through the production
extractor (iscc-tika in `aura-ingest:edge` at `426753ef9`), Tika returned the exact text of
`.go`, `.py`, `.sh`, IEC 61131-3 Structured Text and Siemens `.scl`/`.awl`/`.db`/`.udt`
sources, UTF-8 accents included. It routes on sniffed content, not on the name, and that
failed three ways:
- a `.ts` and a `.tsx` holding markup went to the HTML parser, which dropped the code;
- an SCL comment `(* *)` was typed as Mathematica, harmlessly this time;
- a Go test whose string literal starts a PDF (`%PDF-1.4` in its first bytes) went to the PDF
  parser and failed the file. Indexing Aura's own 5,504 source files left out exactly that one.

Source files and `.txt` are therefore read directly, without Tika:
- a UTF-16 BOM decodes as UTF-16;
- any other file with a NUL byte keeps its name-only row, which is what a SQLite `.db` gets;
- otherwise the text is strict UTF-8 without its BOM, then cp1252, the encoding STEP 7
  exports often carry, and a file that neither decodes keeps its name-only row too.

The extensions are the common languages of the GitHub-linguist list the operator chose (gist
`ppisarczyk/43962d06686722d26d176fad46879d41`), plus Structured Text (`.st`, `.iecst`, as the
ControlForge extension names them), Siemens TIA Portal sources (`.scl`, `.awl`, `.db`,
`.udt`), and `.css`, `.cypher`, `.jsx` and `.mjs`, which indexing Aura's own repository found
missing. The list has one copy, `internal/documents/filecard/source.go`. The ingest reads it
from `aura-filecard -source-extensions`.

A source file's card names it ("nastro.go — source file, 55 bytes.") and claims nothing about
its contents. Before, the card said "Aura did not read this file", which was false on 5,046 of
the 5,983 cards over Aura's sources. A NUL byte outside a UTF-16 BOM still makes a source-named
file an unread file, on the card as in the ingest.

This does not prove:
- Siemens' own documentation of those four extensions. Only secondary sources agree on them;
- chunks that follow a language's structure. They still split by size;
- the card's wording for ranking. It is measured at the change's acceptance;
- files named without an extension (`Makefile`, `Dockerfile`). They keep their name-only row.

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

**The reserved place (measured 2026-10-16).** One place, right behind the best hit, is held
for a file that has no passage at all: a spreadsheet, answered from the file itself, whose card
cannot outscore the documents that merely mention its subject (measured 2026-09-09). A workbook
with row passages leaves the place, since its `passage_count` is no longer 0. That is the
configuration the 2026-10-16 spike measured ("Workbook rows as passages", below).

The place used to go to any document the query retrieved no passage of. Over Aura's own sources
that promoted files found by their name alone, such as a `.service` unit, or a skill reference
scoring 0.344 above hits at 0.48. Keyed on the card's passage count instead, 14 of 74 golds rose
and none fell. The 74 questions are the coding questions in English and Italian, six find-by-name
questions and the office fixture; the record is
[the verification](docs/superpowers/verification/2026-10-16-coding-agent-questions-over-aura-sources.md).
In lexical-only mode the same change moved 6 golds up and 2 down, each by one or two places.

This does not prove:
- that the 2026-09-09 pilot still passes. Its 118-document corpus is gone from the lab VM, which
  holds one 9-document library, so it could not be re-run;
- anything beyond small sets of 6 to 24 questions;
- the right answer when only cards answer. There the place now reaches a passage-less file further
  down, where before it changed nothing.

**Exact identifiers rank first (measured 2026-10-16).** A document whose title, card or returned
passages carry every identifier of the question ranks ahead of one that does not, before the
reserved place and the limit. The score decides within each group. An identifier is memory
recall's: a token with a letter and a digit or an underscore (`arcadedb.QueryIdentifiers`).
Similarity cannot establish an exact technical identity. The final `vector.rerank` orders by
cosine alone, so the document holding a complaint number fell behind one that only resembled the
question.

Measured over the complex corpus's 34 questions and the office fixture's 20, through the real
ingest:

| Set | r@1 | MRR | Golds moved |
|---|---|---|---|
| complex | 0.735 → 0.794 | 0.848 → 0.882 | `RC-2026-040` 3 → 1, `NC-03` 2 → 1, none down |
| office | 0.500 | 0.692 | none |

The engine has no rerank option for this. `vector.rerank` takes no options in 26.10.1, and
ordering by the fusion's score was rejected the same day. The record is
[the verification](docs/superpowers/verification/2026-10-16-complex-documents-retrieval-quality.md).

This does not prove:
- **anything on the code sets.** The English, Italian and find-by-name questions over Aura's
  sources were not run: on the measuring host their ingest failed on embedding timeouts. They gate
  the merge;
- **a whole-table lookup, until 2026-10-17.** A code that lived only in a workbook's rows, such
  as a store's part number, was in no card or passage, so this rule could not find it. Since
  workbook rows became passages ("Workbook rows as passages", below), `RIC-10959` ranks first;
- anything beyond 54 questions written by the corpus's own generator.

**Exact terms admit and rank first (measured 2026-10-25).** This extends the rule above. An
identifier is now one kind of exact term, beside a phrase the caller quotes (`"…"`, `“…”`, `«…»`)
and a file name. A document that carries every exact term of the question is admitted whatever
its cosine and ranks ahead of every other. Memory recall and the fact search follow the same
rule. Nothing else is an exact term: no capitals, no sentence position, no word list.
- **Where carriers come from.** Each full-text index is asked once, the terms' Lucene phrases OR-ed,
  with no floor. Go then checks every text: ArcadeDB 26.10.2's phrase ignores word order
  (`FullTextQueryExecutor.collectPhraseMatches`). A document only that query found is kept when
  its title, its card without Aura's own notes, or its returned passages carry every term.
- **What it does not score.** Such a document reports score 0; its exact evidence carries the
  phrase's BM25, which is not the legs' scale.
- **A failed lane is named** (`exact_terms_unavailable` in the documents, `exact_terms_failed` in
  memory), and the documents never abstain on it.
- **The response names what it checked word for word** (`exact_terms`, absent when nothing was).
  Aura's agent, validating on the lab VM, could not tell an unquoted name's abstention from a
  missing name's until it could read the list.

Measured on the lab VM's library (8 documents, pplx-embed space, floors 0.39 and 0.72), on the 29
questions of the 2026-10-23 floors set plus 11 for this rule:
- s04 with "Ubuntu Mono" quoted, and the bare `"Ubuntu Mono"`, now return `calibre_demo.docx`
  first. Unquoted, it still abstains.
- The 5 out-of-corpus questions and "fattura" still abstain. The hard negatives admitted are 7 of
  12, as before.
- 16 identifiers cost what none do: medians of 904 and 901 ms per search.
- Aura's agent quoted the name on its first call, unprompted.

The record is [the verification](docs/superpowers/verification/2026-10-25-document-exact-terms-lab-vm.md).

This does not prove:
- **anything on the complex corpus.** `complex_qrels.json` was not run: no stack holds the complex
  identity today;
- **that an identifier extracted from a question never admits a non-answer.** `A4`, `Q3`, `24h`
  and `COVID-19` are in no document of this library;
- **a quoted name behind an elided article.** StandardAnalyzer keeps "l’italia" one token, so the
  exact lane misses `"Italia è una Repubblica democratica"`; the legs still rank it first;
- **the lane's candidate cap.** It reads each index up to 200 records in the documents and 400 per
  kind in memory, and 8 documents never fill it;
- anything about memory on the VM: memory was measured by the live tests alone.

**The card's vector (measured 2026-10-16).** A card's vector is computed from its
description: the card without what it says about Aura. That means no caveat, no cut notice, and
no trailer ("Written by Aura at ingest … open it with document_open …").
- **The card itself is unchanged.** It is stored whole in `IndexedDocument.card`, full-text
  indexed and read by the agent, caveats included.
- **The trailer was on all 6,035 cards measured.** It and the caveats made every card look a
  little like every question about documents.

Each card vector was rewritten in place from its description and five question sets re-run:

| Set | Questions | r@8 | MRR |
|---|---|---|---|
| English code | 24 | 0.792 → 0.833 | 0.552 → 0.557 |
| Italian code | 24 | 0.625 → 0.667 | 0.453 → 0.462 |
| find-by-name | 6 | 1.000 | 0.750 → 1.000 |
| office | 20 | 0.800 → 0.900 | 0.650 → 0.667 |
| PDF corpus | 38 | 0.895 → 0.947 | 0.757 → 0.768 |

- **What moved.** Twelve golds rose and four fell, by near-ties or because a spreadsheet newly
  took the reserved place.
- **What did not.** PDF golds were already first in 21 of 22 cases.
- **Noise.** Restoring the vectors reproduced every baseline, one question moving one place.

`filecard.Card.Description()` is the rendering, and `aura-filecard -json` emits it beside the
card. The design is
[the spec](docs/superpowers/specs/2026-10-16-card-description-vector-design.md); the record is
[the verification](docs/superpowers/verification/2026-10-16-coding-agent-questions-over-aura-sources.md).

Then through the ingest itself: one catch-up pass of the shipped code (97fec747e) over the same
three libraries reproduced all five rows exactly. It ran `_extract` once per file and embedded
one description per card, and it re-embedded no passage whose text was unchanged.

This does not prove:
- media cards. No image, audio or video was in any corpus;
- large libraries on one topic. The PDF corpus is 30 short, distinct files;
- anything beyond sets of 6 to 38 questions written by one agent.

**Workbook rows as passages (measured 2026-10-17, spiked 2026-10-16).** A value that lives only
in a workbook's rows cannot be searched: a workbook has no passages, its card answers for it, and
`document_open` reads it. A spike over copies of the 2026-10-16 indexes gave each sheet groups of
15 rows, 2 shared with the next, headed by the sheet's name, each row written as
"Riga N: Header: value; …". `TestCodingAgentQuestions`, production arm at b0c264a:

| Configuration | complex r@1 | MRR | office r@1 | MRR | MRR, all 54 |
|---|---|---|---|---|---|
| as shipped | 0.794 | 0.882 | 0.500 | 0.692 | 0.812 |
| rows as passages | 0.912 | 0.951 | 0.700 | 0.794 | 0.893 |
| rows and file name in the title slot | 0.971 | 0.985 | 0.650 | 0.757 | 0.900 |

The file name in the title slot alone moved nothing (0.812). With rows, every gold of both sets
is within the first 8.

- **What rows broke.** A row's text does not say which file it is in. The 2026 budget lost
  places to the 2025 one (`budget-centro-2026` 2 → 3, `budget-top-centre` 1 → 2). Fourteen
  near-identical invoices, each with a row naming its client, pushed the answering files from
  first to fourth on office q12 and q15. The title slot fixed the budgets and cost office q13,
  1 → 6.
- **The operator's constraint (2026-10-17).** Every row passage names its file and its sheet in
  its own text, because a row does not say whose it is. Read by a model, `fx300-alarm-A119` was
  answered from the FX200's row. On 2 of 22 PDF questions the asked document ranked first
  without its answering passage, and retrieval counts both as first place
  ([the verification](docs/superpowers/verification/2026-10-17-answers-read-by-a-model.md)).
- **The 2026-09-09 decision is narrowed, not reversed.** A spreadsheet is still queried, not
  read (`services/ingest/media.py`). Generic text chunking still does not apply to a workbook:
  it put the Caraglio row in a 2,114-character chunk beside fourteen other municipalities, with
  no header to name a value's column. Aggregates still go to `document_open`. What changes, if an
  arm ships, is key-value row passages from filecard, with the header repeated in every row.
- **Three arms were measured through the real ingest, and B ships.**
  - **The arms.** A has rows with the sheet line only. B adds the file name to that line
    (`File: <name> — Foglio: <sheet>`), which is the operator's constraint. C is B without rows
    for a sheet its card shows whole.
  - **The method.** Each arm was one catch-up upgrade of fresh copies of the as-shipped index,
    run through the bounded embedder.

    | Set | as shipped | A | B | C |
    |---|---|---|---|---|
    | complex MRR | 0.882 | 0.971 | **0.985** | 0.978 |
    | office MRR | 0.692 | 0.746 | **0.804** | 0.739 |

  - **What B fixed.** The store's part numbers, the rota legend (no documents before) and the
    plan legend are first. The invoices' numbers, which filecard leaves out of their rows, are
    recovered through the file name (office q01, q02).
  - **Why C lost.** Without rows, the invoices fell back to their cards, and C was reverted.
  - **Code subset.** On 62 files (the coding questions' 36 gold files and the repository's 26
    workbooks), B against the exact as-shipped counterfactual moved three questions:
    - English MRR 0.917 → 0.938, Italian 0.812 → 0.833, find-by-name 1.000;
    - no workbook reached a moved question's top.
  - **Answers.** Read blind by a fresh agent: 54 of 54 correct. 49 came from the search response,
    5 needed `document_open` (two aggregates and two PDF look-alikes among them), and none needed
    a second search.
- **A format change reaches only new or changed files.** No memo's key covers the row passages:
  `rows.group` and `index_object` are plain functions, and a plain function is not part of a
  memo's key (`services/ingest/media.py`, cocoindex `_compute_logic_fingerprint`). A change to
  the grouping or the file-and-sheet line needs `process_file`'s memo versioned in the same
  change; a change to filecard's row output needs `_extract`'s, which holds the rows with the
  card. No version mechanism ships with this.

The design is [the spec](docs/superpowers/specs/2026-10-17-workbook-row-passages-design.md); the
records are [the spike](docs/superpowers/verification/2026-10-16-rag-full-version-techniques-spike.md)
and [the ingest run](docs/superpowers/verification/2026-10-17-workbook-row-passages.md).

This does not prove:
- the full code index. The code sets ran on 62 documents, not 6,000; the binding comparison is
  the full index on the operator's workstation;
- filecard's header detection. It still drops an invoice's header rows from its rows, takes a
  rota's first employee as its header and shifts the registry summary's quarter rows;
- the appliance's embedding rate and storage. Here the two libraries grew from 11,984 KB to
  18,156 KB and from 12,240 KB to 20,980 KB, and the 5,000-row ledger alone made 385 passages in
  410 s;
- an exact lookup by vector. A 15-row group embeds as one averaged vector, so an exact lookup
  rides on the full-text leg and on identifiers-first;
- aggregates. Rows do not answer them, and the retrieval gold counts a lookup and an aggregate
  the same.

**Ingest embeddings are bounded to the sidecar's slots (measured 2026-10-17).** The ingest sends
its embedding requests to a sidecar that serves one at a time, and a request that waits in the
server's queue is timed out by the client. Measured on this host on 2026-10-17:
- **The sidecar has one slot.** llama.cpp runs with `-np 1`, and `GET /props` reports
  `total_slots: 1`. The pinned image (`ghcr.io/ggml-org/llama.cpp:server-v0.5.0`, build `b11146`,
  `compose.cpu.yaml`) documents the field in `tools/server/README.md`: "`total_slots` - the total
  number of slots for process requests (defined by `--parallel` option)".
  `embed._embed_batch` sends each request with `urlopen(timeout=120)`, and the
  120 s count from the send, queueing at the server included.
- **One large workbook passes, two do not.** The 5,000-row ledger alone ingested cleanly: 385 row
  passages, about 2 requests in flight, 410 s. The arm-A upgrade of the complex library embedded
  two large workbooks in one pass. About 17 requests (136 inputs) were queued at the first
  `TimeoutError`, a retry of those two files reached about 37 (291 inputs) and timed out again,
  and both kept their old documents with no row passages. The code ingest of 2026-10-16 failed
  the same way: 5,918 requests waited past 120 s and 20 of 6,001 objects were indexed.
- **A failed rewrite passed the catch-up.** The upgrade printed "33 indexed, 0 missing" and exited
  0, while CocoIndex's `IngestStatus` row said `errors: 2`. The audit compares the bucket's keys
  with the index's, a document whose rewrite failed keeps its old row, and the exit code looks at
  nothing else.

Shipped:
- `embed._embed_batch` takes a process-wide semaphore before it sends, sized from the local
  sidecar's `total_slots` (read once from `/props`, 1 if it cannot be read). The wait is not part
  of the 120 s. The hosted route, the one with `AURA_EMBED_MODEL` set, stays unbounded.
- The catch-up exits 1 when the last snapshot's `stats.total.num_errors` is above 0, and the audit
  line prints the count.
- No new env var, no retry on timeout, no change to the batch bounds (32 inputs, 4,096 tokens).

Measured after the change, from fresh copies of the same volumes:
- **Reproduced first, with a timestamp and an in-flight count on every `[embed]` line.**
  - In flight went from 1 to 16 within 2 s, and stayed at 16 while the slot completed one request
    every ~9 s.
  - There were 19 timeouts and `IngestStatus.errors = 19`, yet the audit said "0 missing" and the
    process exited 0.
- **The same upgrade, bounded.**
  - In flight stayed at 1, and there were no timeouts.
  - Both workbooks have their row passages: 120 and 385.
  - The audit line reads "33 indexed, 0 missing, 0 errors" and the process exits 0, in 768 s
    against 357 s for the incomplete failed run.
  - Every later upgrade of the day also ran without a timeout.
- **Concurrency is not throughput on this CPU.** Over 32 inputs of ~500 tokens:
  - one slot gives 47–50 inputs a minute sequential, and 48 with 4 concurrent requests;
  - four slots give 50 to 54;
  - only the slowest request grows, from 11 to 40 s.

The design is [the spec](docs/superpowers/specs/2026-10-17-embedding-burst-design.md); the record
is [the verification](docs/superpowers/verification/2026-10-17-embedding-burst.md).

This does not prove:
- **why CocoIndex keeps 16 requests in flight.** The plateau points to a fixed cap upstream. The
  bound makes the cause irrelevant to correctness, not to speed;
- **a full library.** The catch-up of Aura's own 6,027 objects, about 7 h on this CPU, was cut
  by the operator. A 62-file subset ran instead in under 20 minutes with no timeout;
- **the appliance.** Its sidecar runs on the Intel iGPU through Vulkan (`compose.vulkan.yaml`),
  and its ingest rate was not measured;
- **a multi-slot sidecar or a hosted route under load.**
- **that a workbook's card always arrives in time.** Measured by the final reviewer, and older
  than the bound: a 29 KB workbook referencing one 1 MiB shared string from 12,736 cells takes
  aura-filecard about 4 minutes (`clean()` and `truncateRunes` walk the whole string for every
  cell), past the ingest's 120 s card timeout. That workbook loses its card and its rows while
  the audit still counts it indexed; a follow-up should bound the per-cell work to a prefix of
  the raw value.

**A billed media call runs once per file content (measured 2026-10-16).** The ingest's `_extract`
writes a file's bytes to a random temporary path. The vision and speech-to-text calls inside it
were keyed on that path (`extract_scanned_pdf`) or not memoized at all (`media.derive`). So any
change to `_extract`'s code described every image and read every scan again, in every library,
cloud routes included. Both calls are now memoized on the file's SHA-256 and its name, through
cocoindex 1.0.25's `memo_key`.
- **Measured** through the production memos, with a stub bridge counting its calls
  (`services/ingest/tests/test_billed_media_memo.py`). Re-running `_extract` cost two calls per
  image and per scan before, and one after.
- **Only a changed file is read again** (operator, 2026-10-16). A new media route reads
  nothing again, a new vision or speech-to-text model included. Until then a scan was re-read
  on every route change, through a route fingerprint (`media.CONFIG_FINGERPRINT`,
  `aura-media-index -fingerprint`). Nothing else read that fingerprint, and it is removed. The
  test's stub records every invocation, so a pipeline that asked the bridge for its route would
  fail it.
- **What the rule costs.** A file keeps the description or transcript of the route that first
  read it. An image indexed while no route accepted images stays card-only until its bytes
  change; that is read from the code, not measured.
- **The upgrade still pays once.** Every memo key is new, so the first pass of the release
  calls the media routes once per image, recording and scan. Counted on the lab VM: one
  recording, transcribed locally, and no billed call. The appliance's count is a gate before
  the edge image is published.

This does not prove:
- a real vision or speech-to-text route: the bridge was a stub;
- that a live library keeps a nested memo's entries across an outer re-run beyond the two
  passes the test makes.

The primary model receives supported media natively where its route advertises that
capability. OCR/transcription/extraction use shared services and settings. Telegram is
an attachment wrapper, not a competing pipeline. Runs that need indexing wait for the
relevant indexed state and show it to the user. Filename prose is not image transmission.

**The chosen model reads images, ChatGPT included (measured 2026-10-25).** With the chat on the
ChatGPT plan, the selected model reads the media indexer's images and scanned PDF pages and the
daemon's upload summaries, each with the document owner's own ChatGPT account (spec
`2026-10-25-media-vision-primary-design.md`).
- **A reader, not the HTTP arm.** Measured on the lab VM: the plan's token gets 401
  `rejected_by_access_enforcement` on `/v1/chat/completions` and reads the page on `/v1/responses`.
  `(*chatgpt.Client).Describe` sends one Responses message, and the vision route asks it first.
- **What falls back.** A model that reads no images, an owner with no connected account, a refusal
  or a non-image type is "cannot read here": the sidecar takes the image, or the file is card-only.
  Any other error is a failure that retries. It never hands the file to another model.
- **The indexer opens the daemon's store** (`chatgptplan.Open`, which creates nothing), so
  `aura-ingest` mounts `aura-home`.
- **The daemon reads the route per image,** from `llm.Runtime`, so a model switched in the cockpit
  reaches the upload summary without a restart. The indexer starts a process per file, so it
  always did.
- **Measured on the lab VM**
  (`docs/superpowers/verification/2026-10-25-media-vision-primary-lab-vm.md`): a scan with no text
  layer was indexed with its exact text and found by the agent; an image attached in chat was
  summarised by `gpt-6.1-sol`. On `c034fb3c5`, the chat was switched from ChatGPT to Ollama and
  back through the cockpit API, with no restart. Each attached screenshot was summarised by the
  model in force (`gemma4:31b-cloud`, then `gpt-6.1-sol`), quoting its text, and `aura` kept its
  start time.
- **What it does not show:** a member's own account (the VM has one identity), Telegram
  attachments, and the latency and plan cost per image.

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
extension, not the MIME type. A video transcribed before that decision kept its transcript
while `_extract`'s memo held, because the memo is fingerprinted on `_extract`'s own code, not
on the `media.py` it calls (cocoindex `_compute_logic_fingerprint`). The 2026-10-16 change to
`_extract` runs it once more for every file, and such a video then keeps its card, name and
size and loses the transcript. That pass still calls the media routes once per image,
recording and scan, since every memo key is new; after it, a change to `_extract` repeats
none of them (see "A billed media call runs once per file content").

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

**Index rows without an object.** CocoIndex deletes a row only when its own state recorded
writing it (cocoindex 1.0.25 `_RecordHandler.reconcile`). Measured 2026-10-19 on the lab VM:
- the 2026-10-14 wipe restored ArcadeDB, but not Garage or the volume holding each identity's
  `coco.db`;
- the identity's bucket held 0 objects and its index held 10 `IndexedDocument` rows, nine of them
  from before the wipe;
- the restored rows had no owner, so Documenti listed nine files that the file manager could not
  show.

Each live cycle now applies CocoIndex's rule to such a row as well. A key that is indexed but was
absent from the bucket listing in two consecutive cycles has its passages and its document row
deleted, and the log reads `[audit] ORPHAN <key> removed`
(`docs/superpowers/specs/2026-10-19-ingest-index-orphans-design.md`). A catch-up run does not
sweep.

This does not prove:
- that a lost `coco.db` costs only a re-index: an object the bucket still holds is upserted under
  the same keys, but the cost of that re-index was not measured;
- anything about the `aura.assets` rows of the lost objects, whose downloads still answer 404.

**The agent learns the operator's lists from the search, 2026-10-23.** On the lab VM, the real
agent answered the document fields set with a score of 9.44, against the 9.8 gate. The two tag
questions failed because the agent never saw that the tags `interno` and `bozza` existed
(`docs/superpowers/verification/2026-10-23-document-fields-lab-vm.md`).

Every `document_search` result now carries `library_lists`:
- the library's types and tags with their document counts;
- most documents first, at most 30 per list.

The tool description stays the same text for every identity, so the prompt cache holds.
Counterparties are not listed; the question names them. Specified in
`docs/superpowers/specs/2026-10-18-document-fields-design.md` §5, amended the same day.

**What the agent itself found, the same day.** On build `d46cedab6`, without the lists, the
operator asked Aura's agent to search by tag and report friction. The agent ran 14 searches on
the operator's own library and reported five frictions:
- `tags_mode: all` with an unknown tag dropped the tag and widened the filter. That was a bug,
  and it is fixed.
- There was no way to discover the tags. `library_lists` is the answer.
- "Show me the documents with this tag" went through ranking. A filter that admitted one document
  returned none, because no passage cleared the floors.
- The hits did not show their own fields.
- Passages that were not relevant still came back under `uncalibrated_floors`.

The operator decided two of them:
- **A search without `query`, with at least one filter, lists the documents the filters
  admit.** It uses the cockpit's own list: most recently added first, with how many match, and
  no ranking. Since 2026-10-23 a search with no query and nothing to narrow it lists the whole
  library: refused, the agent had missed two of its eight documents taking an inventory.
- **Every result names each document's type, counterparty, date and tags**, each marked
  reviewed or suggested.

The floors are out of this slice.

**Measured on build `489815bf7` the same day.** The operator's conversation was continued.
- **Four frictions resolved, one improved.** The `all` bug, the list without a query and the
  per-document fields are resolved; tag discovery is much improved. The agent found no
  regression.
- **Four residuals, none a wrong result:**
  - no paging past `limit`;
  - three counts with unlabelled scopes;
  - known ids cannot be listed without a query;
  - closest names that are not selective.

  On build `ee662da70` the same day, the agent reported all four resolved: `offset` and
  `next_offset`, the scopes in the tool's description, `document_ids` listed without a query, and
  no closest name for `fatture` or `contratti` while `Clente` still gets `Cliente`. It walked two
  documents, not 50, against one tag.
- **The floors friction is still there.** Measured and recorded the same day: relevance 0.39 for
  the VM's space (§11, "Document floors for pplx-embed-v1-4b").
- **What it does not show:**
  - no 9.8 score, and the 23 passing questions were not re-run;
  - f20 and f21 were not exercised, because the library holds a single tag;
  - one model.

Record: `docs/superpowers/verification/2026-10-23-document-search-lists-lab-vm.md`.

## 12. Workspace, shell and web

Tools and artifact delivery resolve the persistent working root consistently. Sandbox
materialization and original-object access cannot expose another identity's files.
Container paths, host staging and user-facing attachments are distinct.

**The agent's tools run only in the per-identity box (since 2026-08-03, `0d76f84a8`; recorded
2026-10-26).** `shell_exec`, the five `fs_*` tools, `send_file`, `skill_read` and `document_open`
have no host arm. `buildSandboxRouter` builds the router on every profile, and a box that cannot
be created makes the tool deny. Before, `document_open` wrote into the aura container's
`/workspace` while `shell_exec` read the box's, and the agent found the file missing. The aura
container keeps what Aura itself runs there:
- poppler for `pdftotext` and `pdftoppm`;
- git for pack fetches, and `postgresql-client` for backups;
- Node/npm, uv and python3, for stdio MCP servers whose runtime is not the box (prepared with
  uv or npm, amendment #211), for `npx skills add`, and for skill snippets.

Measured on the lab VM on 2026-10-26: the image is 1.43 GB, Node 222 MB and uv 64 MB of it, and
the container ran no process but `aura`. Only `serve`, `chat`, `shell`, `identity` and `mcp`
build a router; the other CLI verbs need no Docker. Not shown: the live tabular acceptance the
plan named (699 on `Clienti.xlsx`) was never recorded, and moving the remaining in-container
installers into the box is not decided. Record:
`docs/superpowers/verification/2026-10-26-one-path-sandbox-only.md`.

**The boxes and the Aura container run on runc; gVisor retired 2026-10-11.** The operator
retired gVisor: runc works well, and gVisor caused problems in the past. Until then two
settings could select it. `server_production` gave every box the `runsc` runtime, and
`install.sh --gvisor` installed `runsc` and set `AURA_RUNTIME=runsc` for the `aura` container.
Measured that day on the lab VM: every container ran on `runc`, `AURA_RUNTIME` was unset, and
`runsc` was installed and registered in Docker but unused
(`docs/superpowers/verification/2026-10-11-browser-sidecar-lab-vm.md`).
- **Installed machines are left as they are.** Hosts installed with `--gvisor` keep the
  package and its Docker registration, which nothing selects any more. The updater does not
  touch apt or dockerd.
- **The FQDN egress allowlist now works under `server_production`.** It needs the nat table
  that gVisor lacks, and `server_production` boxes now run on runc.
- **Boxes created on gVisor are recreated on runc.** `Resolve` treats a box whose runtime is
  `runsc` as stale, so the allowlist never meets a gVisor box.
- **Not covered:** a box names no runtime, so a host whose dockerd default runtime was changed
  by hand applies that default to its boxes.
- **Not covered:** `create-aura-appliance` up to 0.2.10 runs the installer it bundles, which
  still offers gVisor. A host installed that way starts with the `aura` container on `runsc`,
  and its first payload update moves it to runc without telling the operator. The window
  closes when a new version of the wizard is published.

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

**Five browsers in two minutes exhaust a box; the bridge keeps three, measured 2026-10-10.** On
the lab VM, the ten turns of the tool-policy acceptance (`web_search` denied, gemma4:31b-cloud
through Ollama, one conversation per question, about 25 s apart) sent the agent to the browser
in eight. Each of those turns loaded the `browser-aura` skill, which says to keep one session
per site and close it when done, then named a new session (`weather`, `finance`, `news-it`,
`movies_it`, `strikes_check`, ...) and closed none. Four browsers were still up under the
10-minute idle shutdown when the fifth launched, 131 s after the first. Its open timed out after
27 s, and the next launch died with `pthread_create: Resource temporarily unavailable`. The
box's cgroup counted 136 forks refused at its 1024 pids (`pids.events`), and its memory reached
the 2 GiB cap 2403 times, with no OOM kill. Then `agent-browser mcp` itself panicked (`failed to
spawn thread`): the call ended in EOF, the bridge's redial failed in the box (`rm -rf "/skills"
exited 128`), and all seven browser calls of the last four turns failed. Each browser closed
afterwards gave back 137-263 pids and 178-868 MiB, ANSA's the most. The box recovered by itself:
the idle shutdown closed each browser about 10 minutes after its last call, and two
`agent_browser_close` calls the operator asked for freed two sooner.

The bridge now keeps at most three browsers per box: a call on a fourth session first closes the
one used least recently, never one the operator holds in the live view (the call is refused if
they hold all three), and its result says which. agent-browser lists its sessions only in its
`state` MCP profile, which would also mount its cookie, auth and state tools for the model, so
the list is the bridge's own; a browser the box already shut down stays on it until it is the
one closed, which is then a no-op. The rule also rides on the tool that starts a browser:
`agent_browser_open`'s description says what a session costs and to close it, and every open
that succeeds ends with its session's name and the close tool. Idle browsers now close after
three minutes (`AGENT_BROWSER_IDLE_TIMEOUT_MS=180000`). Replayed on this timeline, three minutes
alone would not have prevented the failure, because all four browsers were inside it when the
fifth launched. The cap bounds the pids at about 850 of 1024 with three of the heaviest pages
measured, which would still fill the 2 GiB. Not shown: whether a model closes its sessions
because of the new lines; whether a live view watched without input survives three idle minutes
(on 2026-09-27 only a key every 5 s was measured); browsers started through `shell_exec`, which
the bridge does not see; and how many browsers an operator keeps open in real use.

Measured on `16602b330` the same afternoon, with the same model:

- **The cap.** A turn that asked for four sites in four sessions ended with three browsers. The
  bridge closed the oldest to make room, and all four titles came back with their opens.
- **The box,** with that turn and the ten policy questions again, sampled every 10 s: at most
  3 browsers, `pids.peak` 625, no refused fork, `memory.peak` 1204 MiB, and memory never at
  its cap.
- **Idle shutdown.** Each browser closed 3:00-3:10 after its last call.
- **Closes.** The model closed none of its seven sessions, though every open's result named
  the close tool. The bridge's list therefore lags the box: three of the cap's closes were
  no-ops on browsers the idle shutdown had ended, and the model was still told it had closed
  them.
- **The live view.** A session the operator took over in the live view was still up 4:11 after
  the agent's last call.

Not shown: a passive viewer with no input past three minutes; a second model.

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

**The operator takes the browser over, decided 2026-10-09 at `1c872a1`.** Read against
OpenDots' Dot computers (CopilotKit, MIT, released 2026-10-01): taking over pauses the agent's
input while the person clicks and types, and after release the agent must take a fresh
snapshot before element actions resume. Aura's live view has no such state. Measured in the
tree, not on a live box:
- the relay forwards every allowed viewer event to the browser as it arrives
  (`docker/aura-sandbox/browser-relay.mjs`), and the input route checks only that a viewer
  stream is open (`handleBrowserInput`, `internal/agui/browser_live.go`);
- nothing on the agent's call path reads the viewer registry: `browserViewers` is named in
  `server.go` and `browser_live.go` alone, and the one browser-specific hook on that path,
  `withBrowserProfile` in `internal/agent/mcptools/bridge_call.go`, rewrites the profile
  argument only;
- so a viewer's click and the agent's `agent_browser_click` reach the same Chromium with no
  order between them, and the `@eN` references the agent holds survive the operator's
  navigation in its context while the page has renumbered them. The browser skill advises a
  new snapshot after anything that navigates, and the 2026-09-27 run above shows a model
  skipping the skill.

The decision: control of a session is explicit and belongs to one viewer. "Take over" on the
live view holds it; "release", or the end of that viewer's stream, gives it back. While held, a
`browser__agent_browser_*` call on that session that the recipe grades as a write is refused at
once with an error naming the session and the operator, never paused and never an approval;
reads still run, so the agent can confirm the page after the handoff. After a release, the
first call on the session that carries a `selector` is refused until a snapshot of that
session has succeeded, and the error says so. The chat's inline view and `/browser/<session>`
share the control. Design: `docs/superpowers/specs/2026-10-09-browser-takeover-design.md`.
This reading does not measure the race itself on a live box, which read tools renumber
references (`snapshot` does; `read` and `screenshot` are not measured), nor how a model answers
the refusal; those are the lab-VM acceptance in the design.

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

A real conversation on the lab VM (2026-10-07, "Caratteristiche motore Siemens
1FK22064AF001MA0", gpt-5.6-sol) answered a part-number question from search snippets
because three of its four fetches failed. Each failure was then reproduced from the VM's
own egress with a Go client built like the hardened transport (custom dialer,
HTTP/2 forced), two requests per variant:
- SiePortal (Akamai) and PARTcommunity (Cloudflare) answered 403 to the pinned
  `Chrome/120` User-Agent. A current `Chrome/154` UA alone still drew 403, and so did the
  browser header set (`Accept`, `Accept-Language`, `Sec-Fetch-*`,
  `Upgrade-Insecure-Requests`) alone. Both together drew 200 on 2 of 2 at first. Two
  later series of 10 per site showed how far that holds. The old UA drew 403 on all 20
  requests. SiePortal served the new UA and headers 10 of 10. The first series drew 18
  of 20 across both sites; in the second, PARTcommunity drew only 4 of 10. Cloudflare
  scores the client, and repeated probing from one address lowers the score. The
  PARTcommunity page, when served, carried the specification. SiePortal returned a
  31,526 B JavaScript shell with no specification in it, so no fetch without a browser
  can read it.
- The RS datasheet answered 200 `application/pdf` (168,726 B) to every variant and died
  at the MIME allowlist. The `pdftotext` already in the aura image read it in 34 ms
  (1 page), including weight, inertia, maximum speed and currents the answer lacked.
- Wikipedia, br-automation.com, reddit.com and a distributor page kept answering 200
  under the new header set.
- `aura.tool_invocations` recorded all three failures as `status='ok'` with no error,
  because web errors travel inline to the model, so the ledger could not count them.
- The answer cited `[3]` twice for claims its snippet does not make. `[3]` was the
  SiePortal page that had refused the fetch, and the source list gave no sign of that.

Fetch therefore sends the current UA with the browser header set. A `application/pdf`
response is read through `pdftotext` under the same body cap and deadline. A page whose
markup is all script and no readable text answers `javascript_required`, so the model
knows a browser is needed. A source whose fetch failed is listed as not read. An inline
web error is recorded as `status='error'` with its sanitized code. SearXNG is pinned
from `ghcr.io`, and the updater removes the superseded tag of the same image whatever
registry served it.

This does not establish:
- that the header set survives a WAF policy change: each answer is that day's policy;
- a dependable read behind Cloudflare: the header set raises the odds and does not
  guarantee a 200;
- PDF extraction beyond text: a scanned PDF still yields no text without OCR;
- the gpt-5.6-sol latency: 53 of the turn's 62 s were model time, 46 s of it reasoning
  clamped from `none` to `low` because the plan catalogue offers no lower effort. No change
  is made there.

The same day, after about 45 minutes without probes from the VM, the refusals were
measured again with the same Go client:
- PARTcommunity served the new UA with the header set on 8 of 8 requests 20 s apart, and the
  old UA with the header set on 2 of 2. The 4 of 10 above came from probing one address
  for minutes.
- The original conversation's request shape, the old UA without the header set, drew
  403 on 2 of 2 with `cf-mitigated: challenge` and Cloudflare's "Just a moment..." page:
  a managed challenge, not a block.
- SiePortal's 403 to the old UA is Akamai's plain "Access Denied" page, with no challenge
  in it.
- agent-browser, run from the sandbox image with Aura's entry point (automation flag off,
  the image's Chrome UA), opened both pages from the same address without a challenge.
  SiePortal rendered in 21 s to 3,730 characters carrying 12 Nm, 2.85 kW and 1.72 kW;
  PARTcommunity in 16 s to 7,092 characters carrying 9.1 Nm, 12 Nm, 2.85 kW and 1.72 kW.
- The system prompt's deferred families named no browser, and no browser tool appears in
  `aura.tool_invocations`, although the browser MCP was mounted with 29 tools.

So a fetch refused with `cf-mitigated: challenge` answers `http_error` with reason
`bot_challenge`. The web family line names the browser as the way to read a page that a
fetch cannot: one that needs JavaScript, answers with a bot check, or refuses the fetch.
It also tells the model to ask the operator to complete a visible human check in the
browser's live view, never to solve one itself. The handoff follows OpenBot's
`agent-computer/src/challenge.ts`, which detects the challenge and hands control to a
person.

Live on the lab VM at `2882a46bf`, the same day, with the same question and model:
- The turn read SiePortal as `javascript_required` (324 ms) and Siemens Industry Support
  as a page (964 ms). The ledger recorded its two failed fetches as `status='error'` with
  `javascript_required` and `http_error status 403`.
- Every citation was backed. `[1]`, the SiePortal page listed as not read, was cited only
  for "active product", which its snippet states. The 5,800 rpm maximum speed, missing
  from the first answer, was cited to a snippet that states it.
- Asked to read the RS datasheet by URL, a second turn fetched the PDF in 622 ms and
  answered 8.9 kg, 15.1 kg·cm² and 5,800 rpm, as the datasheet states.
- The turn still took 63 s and billed 134,436 input tokens, 11,136 of them cached.
- A third turn asked what the SiePortal page says. web_fetch answered
  `javascript_required`, and the model loaded the `browser-aura` skill and the browser
  tools. It opened the page in 4.5 s, read it, closed the browser, and answered from the
  rendered page: the product data, the 8.9 kg net weight, the packaging size, "Active
  Product" and the order-status notice. The turn ran from 10:26:50 to 10:28:05.

This does not establish:
- the bot-check route: no live turn met `bot_challenge`, so the switch was seen only from
  `javascript_required`;
- a challenge inside the browser: none appeared, so the handoff path ran nowhere.

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

A default-on recipe toggled from the cockpit has to stay runnable. Measured on the lab VM
on 2026-10-10. The lifecycle acceptance run disabled and re-enabled `calendar` at
16:53-16:54 UTC. That left an explicit `enabled=true` row with no profile, while
`default` held only `elevenlabs` and `linear`. The runnable set dropped the row, and
default-on injection deferred to it. Aura's own sidecars take a 15-minute token with no
refresh token, renewed every 5 minutes for runnable servers only. The calendar's last
renewal was at 16:51:32 and it expired at 17:06:32. From then on the board read "dial
failed" and every call logged `token expired and refresh token is not set`. The sidecar
stayed healthy and reachable throughout. Two changes followed:
- An enable now puts the server in the active profile when that profile is defined.
- A mount an operator waits on (install or enable) renews the caller's first-party grant
  first, because a sidecar disabled for longer than one token lifetime comes back with a
  dead one.

Measured on the VM at 18:28 UTC with `facb60b1b`. The operator disabled and enabled
`calendar` once. The row joined `default`, a grant was self-issued 0.2 s before the live
mount, and the board's probe answered `ok (1 tools)`. One tool is the shape:
`calendar__calendar` is a single multiplexed tool.

This does not establish:
- an agent turn that calls calendar after the repair;
- whatsapp or browser: no run toggled them, though they are default-on the same way.

The cockpit offers no authorization for a sidecar Aura signs itself (2026-10-14). Measured on
the lab VM, the panel had treated memory, calendar and whatsapp as somebody else's OAuth server:
- "Riautorizza" ran the browser flow's discovery against `http://whatsapp:8080` and failed:
  `metadataURL … does not use HTTPS or is not a loopback address`.
- "Disconnetti" deleted a grant the keeper mints again within five minutes.
- The time beside "Autorizzato" was the expiry of the 15-minute token.

What changed (`379f41c06`):
- **The state.** A sidecar the keeper's own predicate (`selfIssuable`) covers now reports its
  authorization as unsupported, with the reason.
- **The API.** A start or revoke that reaches such a sidecar answers 400
  (`agui.ErrNoAuthorizationFlow`).

On the VM running `379f41c06`, the operator's memory panel shows only "Aura issues this
sidecar's credential itself and renews it before it expires".

This does not establish:
- a re-mint on demand: after a host suspend the sidecars still wait for the keeper's next tick,
  up to five minutes;
- the expiry shown for other OAuth servers, which is still the access token's.

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
creates, and one shared relay page
(`https://chetto1983-aura-connect.pages.dev/google/callback/`) as its only redirect URI,
identical for every install — Home Assistant's my.home-assistant.io shape. The site is built by
Cloudflare Pages from gitlab.com/Chetto1983/aura-connect. It moved there from GitHub Pages on
2026-10-13, after the flagged GitHub account turned every address into a 404, and the move is
permanent ([ledger](docs/superpowers/verification/2026-10-13-aura-connect-cloudflare-pages.md)).
The PIM sidecar builds the consent URL from its own `GoogleOAuthRelayUrl`, which `compose.yaml`
sets as `CALENDAR_MCP_CalendarMcp__GoogleOAuthRelayUrl`.
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
`https://chetto1983-aura-connect.pages.dev/mcp/client-metadata.json` (until 2026-10-13 at
`chetto1983.github.io/aura-connect/…`, the address the 2026-09-23 measurements below used). The
document's only
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

Measured 2026-10-13 on the lab VM, after the document moved
([ledger](docs/superpowers/verification/2026-10-13-aura-connect-cloudflare-pages.md)):
- ElevenLabs re-fetches the document named by the grant's `client_id` on refresh. It refused
  every refresh of the grant issued to the old address with `invalid_client: Client metadata
  document returned HTTP 404`.
- Re-authorize then failed on that same refresh, because the flow restored the stored grant.
  Since `e6222a6c1`, a session that runs an authorization (the cockpit flow, `aura mcp login`)
  does not restore the stored grant. It obtains a new one; unattended mounts still restore.
- After a fresh consent for the new document, the server mounted 160 tools.

Still not measured: a successful refresh. Not observable from Aura's logs: which
token-endpoint auth style ElevenLabs accepted. The SDK
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
not a skill catalog that discarded their grouping. Missing endpoints and unsupported
entries are reported. Reuse the existing skill/MCP installers rather than creating a
second marketplace or execution system.

**The CLI install did not reach the running daemon** (measured on the lab VM,
2026-10-23). `aura pack install` and `aura pack trust` wrote a correct row for
`chetto1983/broker-analysis-mcp/broker-analysis`: runtime box, `initTimeoutSec` 120,
trust `sandboxed_local`. The daemon never mounted it, because it mounts registry servers
at boot and, while running, only through the cockpit's MCP write path (`MountNow`). The
agent saw the skills and none of the tools.

**What the operator decided** (2026-10-23 and 2026-10-25, spec
`2026-10-25-pack-install-design.md`):
- A plugin is installed by the operator, from Governance, or by the agent with the
  operator's confirmation on an approval card. There is one daemon-side backend, and the
  install mounts live.
- The confirmation is the trust approval. It is recorded with the operator, the ref and
  the reviewed commit; a pack still cannot trust itself.
- Aura reads Agent Plugins 1.0 beside the Claude format, conformantly, with `PLUGIN_ROOT`
  and `PLUGIN_DATA` in the identity's box. Install and remove are in scope; update is not.
- A connector may not take a catalog recipe's name, and an installed skill never arrives
  `always`-on.
- The CLI keeps `aura pack list` and `aura pack show` only.

**What this does not show:** one plugin and one identity measured, and no Agent Plugins
plugin measured yet.

## 15. Scheduling, delegation and asynchronous delivery

Tasks support `at`, `every` and `cron`, with durable owner, payload and route. The cockpit
and CLI expose inspection/control. Timezone, quiet hours, claim/lease, concurrency and
catch-up have explicit semantics.

Agent jobs use the common runtime and one model/budget snapshot. Claims and notification
intent preserve their transaction boundary. Delivery retry does not rerun completed
model/tool work. Multi-goal enqueue is atomic for the identity.

An operator can pause an active reminder or agent job and resume it, from the cockpit, the
CLI and the `task` tool. A paused task never fires. Resuming recomputes the next fire from
the resume time, so missed windows are not replayed; a one-shot whose time has passed must
be edited instead. The scheduler also pauses a task by itself after
`AURA_SCHEDULER_PAUSE_AFTER_FAILURES` failed runs in a row (default 3, `0` disables it). It
says so in that run's failure notification, and the cockpit shows the count. A success
resets the count. The database backup and system sweeps are never paused, matching the rule
that they are never cancelled. Measured 2026-10-07 by reading the code, not on a running
stack: `paused` existed only in the `scheduler_tasks` status constraint (migration 0009) and
no code wrote it. Dispatch recorded and notified every failure but never changed the task,
so a job whose credential had expired failed and notified on every occurrence until someone
cancelled it. This does not cover the in-flight `unknown_recovery` run, which recovery still
re-fires.

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

An internet outage loses a scheduled job and its failure notice (2026-10-09). An operator's
appliance, which stays on and had no build pending at that hour, did not deliver an
`agent_job` on Telegram around 10:00.

Reproduced on the lab VM at `7099f88a5`, with `gemma4:31b-cloud` through the host's Ollama:
- A one-shot `agent_job` with `notify=telegram` was scheduled with `aura task schedule` for
  12:23:00 UTC.
- The VM's internet was cut from 12:21:30 to 12:27:30 with iptables. The LAN and the Docker
  networks were kept, and DNS was refused.
- The same job with the network up was delivered.

What happened:
- The run started at 12:23:10 and failed at 12:23:31. Ollama could not resolve `ollama.com` and
  answered HTTP 502. The LLM clients run with `WithMaxRetries(0)`; the agent retries a failed
  stream open only once, 750 ms later (`streamWithOpenRetry`), and the `agent_job` handler does
  not retry, so the job never ran again.
- The failure notice failed at 12:23:39 because `api.telegram.org` did not resolve. Its three
  retries failed at 12:23:47, 12:23:55 and 12:24:11, 32 s in all.
- At 12:24:11 the scheduler deleted the settled one-shot together with its run and notification.

When the network came back nothing was sent, and the board had nothing to show. So a job that
runs while the network is down is lost, and its failure notice is lost too unless the network
returns within about 40 s. This task had no origin conversation, so nothing recorded the
outcome anywhere.

This does not measure a recurring task, a `reminder` (no model call), a direct cloud provider
instead of Ollama, or an outage shorter than the 21 s the model call took to fail.

Decision (2026-10-09): the scheduler outlasts an outage of about an hour. The operator chose two
fixes and declined a third, keeping a failed one-shot visible on the board.

1. A failed notice backs off. The sweep retries it on the next tick, then waits 30 s and doubles
   the wait after each failure (1, 2, 4, 8, 16, 32 min). The default
   `AURA_SCHEDULER_NOTIFY_RETRY_ATTEMPTS` rises from 3 to 8, so the last retry comes about
   64 min after the first failure. A delegation nudge shares the same retry rows and backs off
   the same way. A one-shot is kept until its notice settles, so a failed one now stays up to
   that hour.
2. A job that fails on a transient model error before any tool that changes state ran is
   retried as a new fire, 2, 5, 15 and 30 min after each failure, about 52 min in all. These
   count as transient:
   - an HTTP 429 or 5xx from the provider;
   - a timeout, a dropped or refused connection, or a stream that went silent;
   - a DNS lookup that timed out or failed temporarily, which Go reports as `IsTimeout` or
     `IsTemporary` for a SERVFAIL or a socket error (`net/dnsclient_unix.go`, go1.27.2).

   A name that does not exist, another 4xx, a cancel and the job's own deadline do not count.

   A tool changes state when its spec says `Mutating`, for example `send_message`, a file write
   or `shell_exec`. A job that asked only for read-only tools is retried: web search and fetch,
   and memory and document reads, leave nothing behind to repeat. A job that asked for a
   mutating tool is not retried, by the operator's decision: a fresh run could send the same
   message twice, because Aura's duplicate protection keys on the run and the model may reword
   the call. A tool name the registry does not know counts as mutating.

   The first version of this rule (`72e5eece7`) also refused a retry after a read-only tool. The
   operator rejected it the same day, since it lost the common scheduled job, one that searches
   the web and then summarizes.

   The retry is a fire, not a wait inside the run. A tick waits for its runs to finish, and
   `/readyz` reports `scheduler_stalled` after 90 s without progress, so a run that slept
   through an outage would mark the whole scheduler stalled. A retried run is recorded as failed
   on the run ledger, but it is not notified, not written to the origin conversation and not
   counted toward the auto-pause. The last failure is reported like any other. A recurring task
   takes whichever comes first, the retry or its next regular fire. The retry count lives on the
   task (`transient_retries`). It is reset by any reported outcome and by a resume.

This does not cover:
- an outage longer than those windows: the job and its notice are lost as before, and the
  settled one-shot is still deleted;
- a model failure after a mutating tool ran, which is not retried because the tool may have
  acted.

Measured on the lab VM at `5145b43f0` (2026-10-09), with the same cut as above from 14:14:30 to
14:20:30 UTC. A one-shot `agent_job` and a one-shot `reminder` were both due at 14:16:00, both
with `notify=telegram`. Both messages reached the operator's Telegram.
- The job's first run failed at 14:16:48 on the same HTTP 502 and was re-armed for 14:18:48. Its
  first retry ran at 14:18:58 and failed at 14:19:18, and was re-armed for 14:24:18. The second
  retry ran at 14:24:28 and was delivered at 16:24 local time. Nothing was sent for the two
  failed runs.
- The reminder ran at 14:16:28 and its push failed at 14:16:36. The retries failed at 14:16:53,
  14:17:33 and 14:19:25, each pushing the next one back by 30 s, 1 min and 2 min. The retry due
  at 14:21:25 was delivered, at 16:21 local time.
- Each one-shot was deleted on the tick that settled it, at 14:21:28 and 14:24:29.

The retry of 14:18:33 ran only at 14:19:25. The tick that would have swept it was running the
job's retry, and a tick waits for its runs before it sweeps notices. So every wait is a minimum.

This does not measure an outage longer than the first two retry delays, a direct cloud provider
whose lookup fails inside Aura (that path is covered only by unit tests), or a recurring task.

Measured again at `6d42f82cb` (2026-10-09), for the read-only rule. A one-shot `agent_job` asked
the model to call `tool_search` and then answer. A script followed the log and cut the VM's
internet 38 ms after the model's reply asked for the tool, at 14:49:03 UTC. **The job was not
retried.**
- The model call after the tool had already left when the cut landed. It hung until the
  per-call timeout (`TotalTimeoutSec`, 120 s on the VM) and failed at 14:51:03 with
  `context deadline exceeded`.
- The retry rule had excluded that error as if it were the job's own deadline.
- The failure notice backed off (14:51:41, 14:53:07, 14:55:35). It was delivered at 14:55:35,
  32 s after the network returned, and the one-shot was deleted at 14:56:02.

The run also logged `gateway: decision-fact insert failed`. A job with no origin conversation
keys the gateway ledger on `agent_job:<run id>`, which is not a UUID, so the decision facts of
such jobs are never recorded. Only the CLI creates such a job: the `task` tool always forwards the
conversation it was called from, and the boot seeders create system sweeps, not agent jobs. The
CLI is a debug tool nobody uses in production, so the operator closed this as not relevant
(2026-10-09).

Measured on the lab VM (2026-10-12), replaying the exported noleggio-sociale conversation's
exact `payload.goal`: run `01a12ee6-c00a-7257-a11c-de02d003fc93` repeatedly called `task schedule`
instead of checking the official sites. Every call failed with `invalid UUID length: 46`:
the task tool forwarded the ephemeral `agent_job:<run id>` session as its origin conversation,
even though this job DID have a real origin. The previous CLI-only finding does not cover this
production path. The completed summary incorrectly promised a newly scheduled recurring check.

Decision: keep the technical session for run isolation, but propagate the durable origin
conversation separately to conversation-bound tools in cron and swarm workers. Seed scheduled
runs with execution context: this is the current occurrence of an existing schedule, not a new
scheduling request, and the scheduler handles notification of its final result. Keep the original
goal intact and retain this context when auto-rejecting human-input requests. File targets:
`internal/agent/llm_agent*.go`, `internal/agent/tools/{result,task}.go`,
`internal/cron/handlers/{agentjob,handler}.go`, `internal/swarm/swarm.go` and their regression tests.
No migration or environment variable is required.

This reproduction used the real agent and a persisted recurring job, with final notification
redirected to the test conversation (`stdout`) to avoid unsolicited Telegram messages. The row
inherited its newly created step budget (0), not the exported budget (12). It demonstrates the
UUID failure and scheduling confusion, not Telegram delivery or the correctness of legal claims.
Raw evidence: `.planning/tmp/scheduler-conversation-20261012-085639/` (local, not versioned).

Measured in the same authenticated replays and reported with a Cockpit screenshot by the
operator (2026-10-12): accepting the task's approval returns `{outcome:approved, remaining:0}`
and activates the task but leaves no assistant explanation. `classifyResolve` treats every
scheduled approval as terminal, so neither Cockpit nor Telegram drives the continuation that
would read `scheduledApprovalAnswer` (which already tells the model to confirm activation and
not schedule again).

Decision, at the operator's request: interactive scheduled-task approvals return Continue
after their existing activation/cancellation hook, so the agent acknowledges the outcome and
finishes the person's request. Keep worker-owned approvals terminal, pending FIFO groups pending,
and cancellation terminal. Retain the explicit task-state answer and no-rescheduling instruction;
prove the confirmation and absence of duplicate schedules on the real VM. Targets:
`internal/runner/runner_resume_directive.go` and runner regression/integration tests. The
channels already implement Continue; no frontend API, schema, migration or env change is needed.
This supersedes the previous terminal-scheduled-approval contract, not the task activation or
owner-validation contract. Actual notification delivery remains outside this measurement.

The operator additionally requires the actual `ask_user` tool answer to instruct the agent to
always respond visibly after the person's decision, explaining the outcome or next step and
never claiming an action succeeded before it did. Apply this to interactive accept/decline
answers for clarification, choice and approval in `internal/runner/runner_resume.go`, preserving
the answer and existing scheduled-task state/no-rescheduling instruction. Cancellation and
expiry still terminate without a model continuation; worker-owned answers retain their own
continuation contract. Empty accepts must remain invalid, not become valid through appended
instructions. This is a model instruction, not proof of universal model compliance.

Decision (2026-10-09): a model call that runs out of its own time while the job still has time
counts as transient. In an `agent_job` the job's context is the parent of every model call, and
the agent's wallclock budget ends a turn as an event, not an error. So a deadline error while
the job's context is still live can only be the call's own timeout. The job's own deadline is
still not transient.

This does not measure a call left hanging by a direct cloud provider. A provider that is merely
slower than the call timeout, with the network up, is retried too, at most four times.

Measured at `f3a8b9b4f` (2026-10-09), with the same job and cut. The model asked for
`tool_search` at 15:14:27 UTC and the internet was cut 43 ms later, until 15:20:27.
- The model call after the tool hung and timed out at 15:16:27. The job was re-armed for
  15:18:27, and nothing was sent.
- The first retry ran at 15:18:56 and failed at 15:19:16 on the provider's 502, with the
  network still down. It was re-armed for 15:24:16.
- The second retry ran at 15:24:26. The model called `tool_search` again and answered at
  15:24:28. The answer reached the operator's Telegram, with no failure message before it, and
  the one-shot was deleted on the same tick.

This does not measure a job that asked for a mutating tool, which the rule reports by design.

**A scheduled goal's ledger and its conversation (2026-10-14).** Measured on the lab VM, while
the image bridge was being proved (§17).

*The ledger rows were never closed.*
- **What the VM showed.** A daily goal's 12 tool calls left their gateway `start` rows with no
  `end`.
- **Why.** An `agent_job` runs outside a Runner, so nothing wrote the `end` rows. The reconciler
  stamped them as `indeterminate` errors 30 minutes later, over calls that had succeeded.
- **What it cost.** Until then the activity report counted them as live runs, and that held the
  updater's quiet-spell apply back.
- **Decision.** The cron drain marks its dispatches as unobserved (`tools.WithUnobservedDispatch`),
  and the gateway closes each row itself, with the real status and `meta.unobserved`.
  `gateway.FinishCall` closes read-only calls too. The drain does not mark a job as a swarm
  worker, because a worker takes a per-run MCP session that the pool never releases (open,
  separate). Landed in `779db66af`.

*Every scheduled `web_fetch` failed.* The job's session was `agent_job:<run id>`, and a long
result spills to a sidecar file named after that session. The sidecar grammar is
`[A-Za-z0-9_-]`, so the `:` broke it. The session is now `agent_job-<run id>` (`82a49761c`).

*The task's conversation had been deleted.* The operator bulk-deleted their conversations, and the
goal's origin was among them. Every ledger row then failed on
`tool_invocations_conversation_id_fkey`, and the run's outcome was "conversation not found".

Decision (operator, 2026-10-14): **a task outlives the conversation it was scheduled from.**
- **When the dispatch acts.** When the origin conversation is absent or soft-deleted, the dispatch
  opens a new one as the task's identity, titled "⏰ <goal>". It records that conversation as the
  task's origin with `SetTaskOriginRow`, and the run answers there.
- **When it leaves the task alone.** An archived conversation keeps its task. A failed existence
  check leaves the task where it was.
- **Where it lives.** `internal/cron/task_home.go` and `cmd/aura/serve_dispatch.go`, landed in
  `4c0dbe8e6`. No migration or env var.

Measured at `82a49761c` (13:10:19-13:11:34 UTC). The operator started the task with ▶ on the
Scheduler board.
- **The move.** The task moved from the deleted conversation into a new one: same owner, active,
  titled "⏰ Cerca le notizie principali del giorno…".
- **The tool calls.** There were 12 tool calls, among them 7 `web_fetch`. Each has its `end`, with
  `status=ok` and `{"unobserved": true}`.
- **The answer.** It landed in the new conversation.
- **`live_runs`.** It read 1 while the job's heartbeat was fresh and 0 right after.
- **Evidence.** `docs/superpowers/verification/2026-10-14-gitlab-image-bridge.md`.

Not measured:
- an owner who no longer exists;
- an archived origin;
- a goal that calls a mutating tool;
- the swarm worker's MCP session leak.

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

**A work board for the identity and its agent, decided 2026-10-09 at `1c872a1`.** Read against
OpenDots' Spaces and Pages (CopilotKit, MIT, released 2026-10-01: a wiki the Dot drafts into,
with a review card before a page is saved) and against StickyFlow (`chetto1983/stickyflow`
at `e36c4eb`): a Kanban board whose copilot creates, moves, edits and schedules the cards
through tools, reads the board as a bounded snapshot (at most 32 cards of 160 characters)
and may name only ids that snapshot carries. The operator chose the board over the wiki.
Measured in the tree, not on a live run:
- Aura has no durable unit of work the operator and the agent share. `todo` is a session
  scratchpad the model rewrites whole and nothing persists; `task` is the scheduler's verb;
  a delegation ends as a record card in `aura.conversation_turns`; the governance scheduler
  board is a master-detail list of jobs, not a board of work;
- StickyFlow's board is hand-written (`components/Board/BoardView.tsx`) over Supabase; its
  SVAR dependency is the Gantt, not the Kanban. What transfers is the model: card, column
  with a WIP limit, priority, tags, due date, a project that owns its columns, and the
  copilot's tool set and snapshot rule;
- `@svar-ui/react-kanban` 2.6.0 is MIT (npm registry, 2026-06-23), React 18 and up, built
  on `@svar-ui/react-core` 2.6.0, which the cockpit already pins at 2.6.1 through an
  override for the file manager. Its free edition carries the board, the editor, the menu,
  filter, sort, search, custom cards, virtualization and a REST provider that maps
  `add-card`, `update-card`, `move-card`, `delete-card` and `duplicate-card` to
  `/cards` routes; undo and redo, dynamic loading and export are the paid edition, and its
  documentation names no realtime channel. A card is an `id` plus any fields.

The decision: one board per identity, with its own columns and WIP limits, stored in
Postgres under the 0087 fail-closed RLS pair, and a `board` tool, action-multiplexed and
deferred like `task`, through which the agent adds, updates, moves, lists and searches cards
and, as the one Destructive verb, deletes them. The cockpit renders the board with SVAR
React Kanban, driven by its REST provider against a mount that speaks its dialect, the way
the file manager is, with the self-hosted theme and fonts. A card may name the conversation
it came from and the scheduled task it waits on. The agent sees the board only when it
asks or when a turn is opened from a card. Design:
`docs/superpowers/specs/2026-10-09-work-board-design.md`. This reading does not measure
the board on a phone, the provider's behaviour on a failed write or on a stale card, nor
whether a model keeps a board tidy over a week of real use; those are the lab-VM acceptance
in the design.

Traces and dashboards run in the `observability` Compose profile on digest-pinned Tempo and
Grafana images. Measured 2026-10-09 when moving from Tempo 2.9.4 and Grafana 12.4.12 to Tempo
3.1.0 and Grafana 13.2.3:
- Tempo 3.1.0 refuses the 2.x config: `field ingester not found`, `field compactor not
  found`. Retention moves to `backend_worker.compaction.block_retention`; 3.1.0 reported
  336h for it on `/status/config`. Tempo 2.9.4 also accepts the new file under
  `-config.verify`, so the running 2.9.4 stays healthy while the updater replaces the file.
- On a copy of the lab VM's 2.2 GB Tempo volume, 3.1.0 returned traces that 2.9.4 had stored
  on 23 September, 6 October and 7–8 October. It accepted new OTLP spans and found them by
  ID and by TraceQL, using 86 MiB where 2.9.4 used 155 MiB. Within its first hour its
  retention marked every block past the 14-day window for deletion: 21 before, none after.
  It does not replay the 2.x ingester WAL, so traces not yet flushed when Tempo is replaced
  are lost. It writes
  vParquet5 blocks, and Tempo's migration guide states there is no downgrade from 3.0 to 2.x:
  rolling back to an image older than this change also needs the Tempo volume removed.
- A power loss can leave a block's `meta.json` or `meta.compacted.json` empty, in 2.9.4 and
  3.1.0 alike. The local backend writes `meta.json` with `os.Create` and `io.Copy`, with no
  fsync or rename, and marks a block compacted by renaming that file. One such file
  aborts the poll of the whole tenant, so no stored trace is found and retention stops. On
  the lab VM this held from 2026-10-07: 89 failed polls, three blocks with an empty meta,
  21 blocks past the 14-day window. With those three removed from the copy, 3.1.0 polled
  224 blocks without error. A running 3.1.0 needs no restart: with an empty meta added, its
  poll failed and `tempodb_blocklist_tenant_index_errors_total` rose by 6 in 50 s; once that
  block was gone, its next poll wrote the tenant index without error.
  The `tempo-blocks-repair` service removes such blocks on every `compose up`, the one
  aura.service runs at boot included, where Docker's restart policy may already have started
  Tempo. It removes an empty meta written before the current boot. Otherwise it skips one
  younger than ten minutes, which a running Tempo may still be writing. It logs a failure
  and never returns one: a failed one-shot would fail the whole `up`, at boot and on every
  update.
- An empty meta that appears while Tempo runs stays until the next `up`, so the
  `AuraTempoBlocklistFailing` warning reports it. Measured on a throwaway 3.1.0 on the lab VM
  that polled every 20 s:
  - Each failed poll added 2 to `tempodb_blocklist_tenant_index_errors_total`: 6 after 60 s
    and 12 after 120 s.
  - `tempodb_blocklist_poll_errors_total` was never exported.
  - `tempodb_blocklist_length` kept reporting the last good list.
  - The error counter does not exist until the first error, so `increase()` misses that
    first poll.
  - After the block was removed the counter stayed at 12 while polls went on.

  The alert fires when the 10-minute increase stays above zero for 15 minutes. With the
  default 5-minute poll, a broken tenant keeps that window above zero, and one failed poll
  holds it there for 10 minutes only, so a lone failure never fires.
- Two hard resets of the lab VM on 2026-10-09 reproduced the empty meta. The VMware reset
  dropped the guest's memory and kept its virtual disk.
  - Compaction was writing about 2.5 metas a minute. The first reset left one empty
    `meta.json`, and the second left three more.
  - Docker's restart policy started Tempo 8 s and then 14 s before `aura.service` ran the
    repair. Each time, Tempo's first poll failed with `failed to poll or create index for
    tenant`.
  - The repair removed the blocks. The next poll, 5 minutes later, rebuilt the tenant index
    with no restart.

  After an unclean boot Tempo is therefore blind for one poll interval, too short for the
  alert. Tempo's WAL replay also dropped a block it could not read, so traces not yet
  flushed are lost. `docker logs` stopped at the line the reset truncated; the container's
  raw JSON log still held every later line.
- Grafana 13.2.3 started on a volume initialised by 12.3.9, and on one initialised by
  12.4.12. Each time both datasources were healthy, and the four provisioned dashboards
  and their folder migrated with none rejected. It used 249 MiB of its 384 MiB limit. None
  of the removals in 13.0 touches Aura: the checks call `grafana --version`, not
  `grafana-cli`, and look datasources up by UID.

This does not measure Tempo under sustained load, a real power loss on an appliance, or the
updater applying the change to an appliance. The alert timing comes from that 20-second spike
and the 5-minute default, not from an alert firing on the appliance stack.

Moved on 2026-10-24 to Prometheus 3.13.5, Tempo 3.1.1 and Grafana 13.2.4, together with
Postgres 18.6, SearXNG 2026.10.23-629368e40 and the whisper mirror 2026.10.21.1401: patch
releases, and for SearXNG and whisper the newest rolling build. Each service ran from
`compose.yaml` with `--no-deps` in a throwaway Compose project, first on the old pin and then on
the new one over the same volumes (`docs/superpowers/verification/2026-10-24-compose-pin-refresh.md`):
- Postgres 18.6 opened a data directory that 18.4 had migrated to 0142. It kept 10,000 probe
  rows byte for byte. Its schema dump equals a fresh 18.4's after the same two `db migrate` runs,
  and `db migrate` found nothing pending.
- Tempo 3.1.1 found by ID and by TraceQL a trace 3.1.0 had flushed to a block, and took new
  OTLP spans. One empty `meta.json` still fails the tenant's poll. Grafana 13.2.4 kept both
  datasources healthy and the four dashboards. Prometheus 3.13.5 loaded all 47 rules, kept the
  3.13.4 samples, and passes `promtool test rules`.
- SearXNG returned 27 results per query on both builds. Whisper transcribed an Italian clip
  word for word on both, in about 8 s.

This does not measure the aura daemon or the agent against these images, the lab VM, or the
updater applying them to an appliance.

## 17. Deployment, backup and recovery

The appliance is Docker Compose. Installation validates target prerequisites, prepares
artifacts, generates configuration and selects CPU/CUDA embeddings from target hardware.
CPU configuration must also remove incompatible GPU reservations. Installation payloads
are verified against their manifest.

The appliance installer and updater enforce `single_user_hardened` with
`AURA_MUSR_ISOLATION=true` on a single-node appliance, including upgrades whose
configuration omits those keys or still selects a development profile. An explicit
`server_production` profile remains unchanged and retains its separate durability
prerequisites. Since 2026-10-11 it has no container-runtime prerequisite: gVisor is retired
and every box runs on runc (§12).

An unset or unrecognised `AURA_PROFILE` is `single_user_hardened` everywhere: in the
binary and in bare Compose, not only on an installed appliance. `dev` is an explicit
opt-in. Development harnesses that boot the daemon on sample credentials state it
themselves: the web E2E and the AG-UI smoke. Measured 2026-10-07 on a local Compose
Postgres 18.4, with the default flipped and no profile set:
- The unit suite is green except tests that relied on the old default: 2 tests of the
  profile contract itself and 8 `cmd/aura` tests that mount a loopback MCP fixture,
  which the strict egress policy blocks as SSRF by design.
- The `db_integration` tier for `db`, `cron`, `agui` and `documents` is green, and so is
  the race tier for `conversations` and `runner`.
- `aura serve` refused to boot in the AG-UI smoke and under the web E2E environment.
  `aura config validate` named four fatal violations: the sample object-store access
  and secret keys, the empty Garage RPC secret, and the missing embedding revision.
This does not measure the full Playwright E2E, the MUSR E2E, the memory, ingest or
sandbox tiers, or a live appliance under the new default.

Measured 2026-10-08 on the CI of the pull request that shipped the default: the MUSR job's
load and chaos harness started `aura serve` without a profile, and it exited 71 on the same
sample object-store keys. On the next push the Agent Memory job, whose `compose up
arcadedb-mcp` starts the aura container, restart-looped on the same keys for ten minutes.
That harness and that job now state `dev`. The Production Readiness workflow stated it too
while its rollback rehearsal started the aura container; that stack went with the rehearsal on
2026-10-08 (§17). Its remaining strict-profile tests passed in an empty environment that day.

Measured 2026-09-14: the running appliance had neither key in `.env` and
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

**Image home (2026-10-14).** Since 2026-10-11, GitHub has refused every pull from
`ghcr.io/chetto1983`, while the account is under review. The appliance images therefore live at
`registry.gitlab.com/chetto1983/aura`.
- **Where the home is set.** `IMAGE_HOME` in `deploy/aura-image-update.sh` names it.
  `IMAGE_HOMES` lists every home a host may still name.
- **Who publishes.** `scripts/publish_images.sh` builds and pushes the nine images from the
  operator's machine. It publishes only a commit already on `gitlab/master`. It runs the image
  contracts before any push and moves `aura:edge` last. The whisper pin is mirrored by digest.
- **How a host moves.** Every tick downloads the home copy of each key that still names another
  home (`deploy/aura-image-home.sh`). The tick stops below 12 GiB free, and it does this before
  any consent is asked.
  - **The move itself.** It runs inside the apply, before the payload sync. It repoints only the
    keys whose home copy is already on the host, and it never pulls.
  - **The update check.** A key whose copy has arrived counts as a pending update.
  - **The reclaim.** The images the move superseded are reclaimed afterwards.
  - **Fresh installs.** They start at the home: `scripts/install.sh`, compose's fallbacks and
    create-aura 0.2.12 and later.
- **A host whose updater predates this.** Its updater cannot fetch its successor, so it needs
  `deploy/aura-registry-bridge.sh` once.
  - **The pull.** The patch pulls the GitLab copy of every key that still names ghcr, measuring
    free space in KiB. If any of those pulls fails, it refuses and changes nothing.
  - **The move.** Then it moves `AURA_IMAGE` alone and starts the updater.
- **What "behind" means.** On the containerd image store an image's ID is its index digest, and
  every attested publish writes a new index. So the updater calls a container behind only when
  its platform manifest (`ImageManifestDescriptor`) differs from the tag's, which is what compose
  compares. A container without a descriptor falls back to the image ID (`4144693ec`).
- **Why the patch wants 12 GiB free.** The published set takes up to 10 GiB on a host that shares
  no layer with it, because the containerd image store keeps each layer both compressed and
  extracted. The old images stay on disk until the first apply, and the rest of the 12 GiB is
  the databases'.

Measured 2026-10-14 on the lab VM (`docs/superpowers/verification/2026-10-14-gitlab-image-bridge.md`):
- **The patch.** It refused at 10.49 GiB free. With room made, it moved `AURA_IMAGE`.
- **The update.** After "update now", the updater installed its successor, moved the other
  eight keys, brought the stack up on `e04aefcad` and removed every ghcr image.
- **The second tick.** It moved nothing and exited 0.
- **A clean install.** `npx create-aura-appliance@0.2.12` on the wiped VM pulled only from
  GitLab.
- **The review fixes (`1a5c4137c`).** The operator applied them with "update now". The payload
  brought the image-home library and the new updater, which re-executed itself.
- **The VM fixes (`82a49761c`).** A tick applied them in a quiet spell at 13:00:55 UTC, with
  nobody pressing anything. The next tick found the box current.
- **The re-apply loop.** Before `4144693ec`, a republished, unchanged sidecar had kept the old
  updater re-applying the same build every ~7 minutes.
- **The scheduled goal.** It no longer pins `live_runs` (§15, 2026-10-14).
- **CI.** GitLab pipelines #2 and #3 were green: `make quality`, plus `appliance-shell`, which runs
  the 11 appliance shell suites.
- **The fixed patch on a frozen box.** The VM was put back to ghcr keys, the pre-bridge updater and
  an older build. The patch then ran again from GitLab raw master at 17:40 UTC: it pulled all nine
  copies and moved `AURA_IMAGE`.
  - The old updater applied `379f41c06` in a quiet spell and installed its successor.
  - The successor moved the other eight keys.
  - The box was healthy and `current` 5 min 20 s after the patch, with nobody pressing anything.
- **The first tick of a clean install.** `install.sh` now records the applied image and payload
  manifest (`9c10e3da7`).
  - Run on the VM's real image and payload, it recorded what the updater records itself.
  - The next tick found the box current.
  - create-aura-appliance 0.2.13 ships it. Read back from npm, its installer's `install.sh`
    carries the function.
- **The review's minors, closed (evening).** The VM was frozen again, with the update timer
  disabled.
  - **Docker stopped.** The patch, piped from GitLab raw master, refused with "Docker does not
    answer". `.env` was unchanged, the timer stayed disabled, and Docker stayed down.
  - **Docker back.** The patch, run on a frozen box whose stack was down, pulled all nine
    copies in 60 s and moved `AURA_IMAGE`. It left the timer `enabled active`.
  - **The publisher.** It published `a8541174d` to the home that commit's own `IMAGE_HOME`
    names, and left no image on the operator's Docker. That held on the run whose push timed
    out too, which also moved no tag.
  - **create-aura's preflight.** It probes ghcr.io again, for llama.cpp, searxng and kokoro.
    Its remote probe, run on the VM, reached all four hosts.
- **A stack that is down did not come back up (evening, fixed).**
  - **How it went down.** Stopping Docker stopped `aura.service` first
    (`Requires=docker.service`), and its `compose down` removed every container. Starting Docker
    does not start the unit again.
  - **The unit's own start.** The unit's `compose up -d` honoured `pull_policy: always`. Once
    ghcr refused a pull, compose interrupted the rest, and no container started. A reboot takes
    the same path. The unit now runs `up -d --pull missing` (`f9b4d2268`); an explicit `--pull`
    replaces every service's policy.
  - **The updater on that stack.** It replaced aura alone, and aura reports ready only once its
    memory MCP is mounted, while arcadedb-mcp starts after aura. It waited 600 s and exited 1.
    When arcadedb-mcp is not running, the apply now brings the whole stack up (`70e4398b8`).
  - **The open gap.** A frozen box that reboots before the patch stays down. Its unit cannot be
    updated except through the patch.
  - **Proved on the VM.** With the stack taken down and the `a8541174d` updater installed, one
    tick applied `3e15e058c` by itself in 5 min 11 s. It installed the new updater and unit,
    brought aura up healthy in two minutes instead of failing at 600 s, and ended `current`:
    19 containers, 0 unhealthy, no ghcr image left.
  - **The publisher.** It retries a dropped push once (`aa5401fcf`): three of five publishes
    that evening lost one upload.
- **Docker coming back (2026-10-15, fixed).**
  - **The unit.** `aura.service` is also `WantedBy=docker.service` (`35864cc2f`), so every
    Docker start runs the ordered `up` again.
  - **The links.** `apply_update` re-enables each enabled unit that lacks a link its
    `WantedBy=` names (`fac26edd4`), whoever copied the file. On an existing host the new unit is
    copied by the old updater, which never re-enables it, so the re-executed updater is the one
    that has to make the link.
  - **Measured on the VM** (`docs/superpowers/verification/2026-10-15-docker-restart-and-install-dir.md`).
    The starting state was the pre-fix unit, no `docker.service.wants` link, and last night's
    updater. One tick to `37e9e9a61` linked the unit by itself, and `systemctl restart docker`
    then brought 17/17 containers back healthy in 100 s. With the link in place, a full stop and
    start came back in 75 s.
  - **create-aura 0.2.15** ships the unit and the updater to fresh installs.

The way back is the same move in reverse: one commit sets `IMAGE_HOME` back and deletes the
patch (done 2026-10-21, below).

Not shown by these measurements:
- the way back itself, which runs only in the tests;
- the fixed patch's space refusal on a frozen box, and its reclaim of ghcr images that differ
  from GitLab's. In the re-runs 20 GiB were free and the ghcr names were tags on the GitLab
  images; the first run did reclaim distinct ones;
- a containerd root on its own filesystem: the lab VM keeps it on Docker's;
- a Docker upgrade through apt. `systemctl restart docker` stood in for it (2026-10-15);
- an appliance other than the lab VM;
- a hermetic build;
- GitLab's pull limits;
- pinned release installs, whose tags only ghcr carries.
- **A publish racing a tick.** A tick that pulls in the middle of a publish can run a newer
  sidecar than `aura` until the next tick.
- **The install fix inside a clean install.** `record_applied_update` ran on the lab VM's real
  image and payload, not at the end of a fresh install.

**Return to ghcr (2026-10-21).** GitHub reinstated the chetto1983 account. Measured that day
with anonymous registry requests, no login:
- **ghcr answers again.** All ten `ghcr.io/chetto1983` repositories list their tags (HTTP 200),
  and every tag a host's `.env` pins resolves.
- **What ghcr holds.** `aura-arcadedb-mcp:edge` was already rebuilt from `5f7966aa5` by the
  Actions run of that push. The other Aura images were still the pre-block builds (`64ec61820`,
  2026-10-08/11) while that run's edge publish was in progress. The two fork images were older still,
  `aura-pim-mcp:sidecar` and `whatsapp-mcp:latest` from 2026-09-24, against GitLab copies from
  2026-10-14. Whisper is the same digest on both registries.

So `IMAGE_HOME` names `ghcr.io/chetto1983` again, and every shipped default follows it
(`TestShippedDefaultsNameTheUpdatersImageHome` failed on six files before they moved).
`IMAGE_HOMES` keeps both registries, so a bridged host comes back through the same move, with no
patch run on it, and `deploy/aura-registry-bridge.sh` is deleted. create-aura 0.2.16 probes
ghcr.io, get.docker.com and huggingface.co, as before the bridge.

The move does not compare builds: it repoints a key as soon as the ghcr copy of its tag pulls.
So every image must be current on ghcr before a bridged host receives this updater, or the host
would step back: to the 2026-10-08/11 builds for Aura's own images until the edge workflow
publishes this commit, and to the 2026-09-24 builds for the forks until they are republished.
Hosts receive the updater only from the GitLab `aura:edge`, which the operator publishes (Return
trip step 3), so that publish waits for both.

The edge workflow itself timed out on this commit, the first GitHub build of the aura image
since the Craft apps joined it on 2026-10-19: with a cold cache, pdfcraft took 717 s and
wordcraft 508 s, and the 30-minute cap cut the job off in the third of five Rust builds. Its cap
is now 90 minutes.

Andrea's box ran the bridge patch (operator, 2026-10-21), so its updater reads GitLab and comes
back only through that publish.

Not shown by these measurements: a host making the return trip, Andrea's included; and the fork
workflows republishing on GitHub.

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

A payload file a service mounts reaches that service only when the service is started again.
Measured 2026-10-09:
- Compose 5.5.1 recreated a service neither when a file it bind-mounts changed nor when a
  top-level `configs:` file did.
- On the lab VM, the updater's `install` gave the file a new inode, and the running container
  kept reading the old content until `docker restart`.

So until this change a payload change confined to such a file reached nothing until the next
reboot. The files concerned are `garage.toml`, the Caddyfile, SearXNG's settings and limiter,
Prometheus' config and rules, `tempo.yml`, Grafana's provisioning and dashboards, and
ArcadeDB's `backup.json`. The updater now restarts, after the stack-wide `up`, every running
service that bind-mounts a payload file changed since the stack was last applied. It keeps the
changed paths until that apply succeeds. An appliance with no record of what changed (its last
tick predates the record, or the record was lost) restarts every service mounting any payload
file. One-shot services that have exited are not started.

Garage keeps the object store's metadata in SQLite and its data in block files, on one node.
With `metadata_fsync` off, its SQLite runs `synchronous=OFF` (Garage 2.4.1 `sqlite_adapter.rs`),
which SQLite documents as corruptible on power loss. Garage's own documentation calls metadata
corruption harmless only because other nodes rebuild it, and an appliance has none. With
`metadata_fsync` on, SQLite runs WAL with `synchronous=NORMAL`, which never corrupts but may
roll back the last commits. `data_fsync` syncs each block file before the write is
acknowledged. Measured 2026-10-09 on the lab VM:
- A throwaway Garage 2.4.1 on SQLite ran three passes of 200 4 KiB, 60 1 MiB and 15 16 MiB
  PUTs, plus 400 4 KiB PUTs from 8 writers, with both options off, metadata only, and both on.
- The medians stayed within the spread between passes of one setting: 4–7 ms, 9–15 ms and
  77–105 ms. The 8 writers took 2.0–2.8 s in every setting.
- The VM's disk does sync: `dd oflag=dsync` took 0.8 ms per 4 KiB write against 17 µs
  buffered, with the device in write-through.
- Aura had written 2 objects since that Garage was recreated, that morning.

Both options are now on.

A hard reset of the lab VM on 2026-10-09 confirmed the rollback under load. One writer was
putting 64–320 KiB objects into a throwaway bucket, about 7 a second.
- Of 3,721 PUTs that Garage acknowledged, 3,714 read back with their sha256, and none was
  corrupt.
- The last 7, acknowledged in the final 0.6 s, were gone (404).
- The guest's journal had lost its last 27 s as well, so the reset discarded unsynced writes
  the way a power loss does.

A third reset measured the window at Aura's own pace, one 64–320 KiB PUT every 5 s for
4.5 minutes:
- Of 51 acknowledged objects, 47 read back intact, and none was corrupt.
- The last 4 were gone; the oldest of them had been acknowledged 15 to 20 s before the
  reset. The one acknowledged 5 s earlier survived.
- The journal again ended about 20 s early.

A Garage acknowledgement is therefore not durable until SQLite's WAL reaches the disk. LMDB is
no way out:
- Garage's configuration reference calls it prone to corruption after an unclean shutdown.
- With `metadata_fsync` on, it still runs `MDB_NOMETASYNC`, which may undo the last
  transaction.

Aura marks an asset `accepted` in PostgreSQL as soon as Garage acknowledges its object
(`internal/assets/ingest_agent.go`). A power loss inside that window leaves an accepted asset
whose object is gone.
- Documents and images then still reach `complete`, since their processors never read the
  object.
- Downloads answer 404.
- `Retry` re-arms the asset without checking the object.

The gap is open by decision, as of 2026-10-09. A boot-time check that marks such assets
`failed` was designed and deferred. Upstream,
[garage#1571](https://git.deuxfleurs.fr/Deuxfleurs/garage/issues/1571) asks for a durable
acknowledgement mode.

This does not measure an appliance's own SSD, a filesystem tuned differently from the VM's
ext4 defaults, or a power cut that also drops the host's cache.

Postgres uses a seeded `0 1 * * * Europe/Rome` `backup_postgres` task, atomic dump promotion and 14-day
retention. ArcadeDB loads `docker/arcadedb/backup.json`, covers all databases including
new identities, and backs up every 60 minutes to a separate volume. Retention is
`maxFiles=60` with hourly/daily/weekly/monthly buckets of 24/7/4/6.

Garage objects have a seeded `30 1 * * * Europe/Rome` `backup_objects` task (migration 0142,
2026-10-19). The measured cause: on 2026-10-14 the lab VM was reinstalled, and the 2026-10-13
backups restored Postgres and ArcadeDB. Nothing restored Garage, so every uploaded file was lost
while Documenti still listed nine of them.

What the job does:
- It copies each bucket Aura holds a key for (the shared bucket and each identity's own) with
  that bucket's own key, into `AURA_BACKUP_DIR/objects/<bucket>/`: one sha256-named blob per
  version, plus `manifest.json`.
- It downloads only what changed.
- It keeps a deleted or replaced version for 14 days, then deletes it with its blob.

`aura objectstore restore [--as-of] [--bucket]` puts back the versions live at a moment in those
14 days whose key the bucket lacks. It never overwrites a key. A wiped bucket is recovered by
restoring as of before the wipe, because the first run after a wipe records every file as
deleted. The job copies nothing off the host. Spec:
`docs/superpowers/specs/2026-10-19-object-backup-design.md`.

The restore drill covers Postgres, conversation sidecars, Garage and a tenant-shaped
ArcadeDB database, with checksum and cleanup verification. Restore targets are new and
verified. Existing archives must be testable without replacing live state. Cadence and
fixture timings are not production RPO/RTO guarantees.

Complete recovery needs originals, workspaces, runtime/integration state, configuration
and encryption/derivation secrets as well as databases. Volumes on one host do not cover
host loss; off-host retention is an operational requirement. No atomic cross-service
snapshot is promised.

Going back to a previous release is restoring the pre-upgrade backup into a new database
and starting the previous image on it. Never overwrite the last known-good backup during
recovery.

Measured 2026-10-08: every binary requires the migration tracker at exactly its own head
(`CheckMigrationHead`), so a previous image refuses a database a newer candidate migrated.
On the first Production Readiness run with a previous image (run 37754232583, candidate
`b2dd1fb08`, previous v1.0.2-rc1), v1.0.2-rc1 stopped with `migration tracker incompatible:
version=138 dirty=false want=112`. A rehearsal that restored the dump instead (run
37759221702) failed on v1.0.2-rc1's memory mount under the candidate's compose file; the
cause was not isolated. The operator then removed the image-rollback rehearsal and its
`rollback-report.json` from release readiness, which now requires eleven reports. What this
leaves unproven: no release gate starts a previous image on restored data. The Postgres
restore itself is the disaster-recovery report's restore drill.

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

A tagged release requires eleven current reports at the exact full candidate SHA:
security, unit/database coverage, Docker coverage, agent memory, mutation, capability,
load, chaos, disaster recovery, observability and audit closure. Evidence
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

### Document-field coverage tiers — measured 2026-10-22

The official unit/database run's native coverage union measured the owned source at
48,423/55,407 statements (87.3951%). Its `db_integration` package inventory found
`internal/docfields` at 392/1,112 (35.2518%), versus the previous pinned baseline
217/932 (23.2833%). The existing baseline mechanism records the new measured counts;
the owned aggregate and target-package floors remain 85%, and denominator drift still
fails closed. No delegated authority is added. This database tier cannot execute the
ArcadeDB-backed document-field store: the separate same-source
`arcadedb_integration` package run measured 90.6%, recorded in
`docs/superpowers/verification/2026-10-21-paperless-document-file-types.md`.
These denominators are separate measurements and are never averaged or concatenated.
The database baseline is explicit non-regression debt, not an 85% package-coverage claim;
the live result does not establish whole-repository or native Docker coverage. Final
browser, mutation and publication evidence remains separately required.

### Documents detail and shared previews — measured 2026-10-22

The native Office browser/save probes in
`docs/superpowers/verification/2026-10-21-craft-office-browser.md` and
`2026-10-22-craft-office-save.md` establish Word DOCX/DOC/ODT/RTF, Grid XLS/ODS/CSV
and Deck PPTX imports. Shared preview dispatch uses those existing Craft apps and PdfCraft;
the superseded PDF/DOCX viewers are retired. Native dirty=false precedes host persistence,
so pending and failed writes remain guarded, retries retain bytes, and writes are serialized.
Converted Save As filenames remain visible. Tab changes preserve the mounted native draft.

`2026-10-21-code-preview-inventory.md` measured 262,144 source bytes expanding to 957,753
HTML bytes and 1,225,037 bytes to 4,475,241 HTML bytes (784 ms, excluding DOM layout).
Original-source previews therefore bound streaming reads at 256 KiB and syntax highlighting
at 65,536 UTF-16 code units/2,000 lines (8,192 per line), with explicit truncated/plain/binary
notices, complete-original copy for bounded text and original download for every size.
The existing lazy Shiki engine supplies common grammars; unsupported languages remain escaped
text. Canonical document file_type supplies the source/media routing fallback. Existing HTML
Source/Preview retains its opaque-origin sandbox, and incomplete srcdoc is never rendered.
The live code/media ingestion evidence confirms that indexed HTML/XML passages omit markup
and .db can contain SQLite bytes: indexed Content and original Preview remain separate tabs.

Detail reuses the routed result query and existing resizable panels, with current-result
previous/next across pages, Details/Content/History and mobile Preview. Metadata drafts track
only intended changes across ingest refresh; file Save and Save details are distinct. Label
creation uses existing APIs without resetting the form; failed lists remain explicit. These
measurements do not establish every Office import, legacy binary PPT support, arbitrary source
encodings, worst-case grammar latency, or the final rebuilt workspace's live acceptance.

### Saved-view deletion in navigation — measured 2026-10-22

The user reported a missing Delete action beside the pinned saved view. On the running
Documents workspace, `Navigazione documenti` contained the synthetic `UX study — Cedar archive`
link and zero buttons. Source inspection confirmed that deletion already exists in the saved-view
management screen, using the tenant-scoped DELETE API and ConfirmDialog, but the sidebar exposes
only links. Evidence: `docs/superpowers/verification/2026-10-22-saved-view-delete-gap.md`.

Expose a named, visible Delete action beside each pinned view in desktop navigation and its
mobile drawer, reusing the existing deletion API, mutation invalidation and confirmation UI.
Cancel sends no deletion; a server error retains the view and offers a visible retry. Success
removes only that saved configuration and refreshes views for the current identity, preserving
the current query and documents. This measurement establishes a discoverability gap, not a
missing server capability or a need for a second persistence path.

The first desktop and 390 px live runs of that action also measured a keyboard defect:
Cancel closed confirmation without returning focus to Delete. The existing ConfirmDialog
opens from an external button and has no Radix DialogTrigger reference. Reuse DialogTrigger
with `asChild` through an optional trigger slot in `web/src/components/ui/confirm-dialog.tsx`;
retain the existing safe initial Cancel focus and untriggered callers. A focused regression and
both live viewport runs must prove focus returns to the originating button after cancellation.

The subsequent native-trigger run passed desktop cancellation, Escape and deletion. On 390 px,
Escape dismissed both confirmation and its outer navigation drawer: Drawer's document keydown
handler ignored the inner modal's already-prevented event. `web/src/shell/Drawer.tsx` must respect
`event.defaultPrevented` before handling Escape/Tab, preserving its normal keyboard dismissal
when no nested component consumed the key. The nested-dialog regression retains the drawer and
returns focus to Delete; it must not replace the existing dialog or drawer focus mechanisms.

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
