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

| Layer | Responsibility | Source |
|---|---|---|
| Composition | Wire runtime, stores, channels, workers and clients | `cmd/aura` |
| Turns | History, context, persistence, pause/resume, capture and delivery | `internal/runner` |
| Agent | Model rounds, tools, events, budgets and completion | `internal/agent` |
| Policy | Tool decisions, approvals, grants and reservations | `internal/gateway`, `internal/approvalgrants` |
| Control data | Identities, conversations, settings, jobs and audit | Postgres, `internal/db` |
| Memory/retrieval | Facts, graph relationships and derived search records | `internal/arcadedb`, `cmd/arcadedb-mcp` |
| Objects | Original files and identity-bound storage | `internal/objectstore`, Garage |
| Ingestion | Reconcile sources into cards and passages | `internal/ingestsupervisor`, `services/ingest` |
| Extensions | MCP connections and owned/shared skills | `internal/mcp`, `internal/mcpregistry`, `internal/skills`, `internal/skillacl` |
| Execution | Workspace and per-identity sandbox boxes | `internal/sandbox/usersandbox` |
| Surfaces | HTTP/SSE, embedded cockpit, Telegram and CLI | `internal/agui`, `internal/webui`, `internal/channels` |

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

A terminal answer cannot race runnable sibling tools. Rejected streamed drafts are
explicitly discarded on every surface before another answer begins. A terminal-only
answer must be visible, and an already-streamed answer must not be duplicated.
Completion and persistence agree about the accepted answer.

Deliverables are sent through the channel's artifact mechanism; a path alone is not
delivery. Partial outcomes identify unfinished work and the applicable limit.

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

Native subscription OAuth/Responses/Messages integrations require separate measured
transport and credential lifecycles. A configurable base URL does not establish such
support. Credentials require protected identity/operator storage, refresh and
revocation; they do not belong in prompts, logs, source or public artifacts.

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

Reasoning retrieval requires an explicit selector. Successful traces retain for 30
days; failed/cancelled traces for 7 days. Source, conversation, identity and operator
deletion override TTL. Retrieval does not renew it. Concurrent expiry/deletion must
converge: a stale vertex deletion may restart the rolled-back transaction and select
roots again, with bounded retries and no generic missing-resource error suppression.

Digest and relevance preload are bounded and observable. A count capped by `limit=1`
is not the graph's entity total. Conversation-only retrieval must not disappear because
a renderer only supports facts; included anchors carry role/date and are labeled as
something said, not promoted to asserted facts.

## 11. Documents and media

Identity-bound originals live in Garage. The ingestion supervisor resolves existing
provisioning bindings and manages CocoIndex workers. Live refresh actually rescans
sources. Add/modify/delete reconciles cards and passages; source identity and scope
agree between Python writers and Go readers.

Extraction and supported format normalization feed token-bounded chunks. The current
EmbeddingGemma contract is 768 dimensions with a 2,048-token input ceiling, including
prefixes and special tokens. Model artifact, dimension, input format and fingerprint
agree across installation, ingestion, memory and CI.

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

## 12. Workspace, shell and web

Tools and artifact delivery resolve the persistent working root consistently. Sandbox
materialization and original-object access cannot expose another identity's files.
Container paths, host staging and user-facing attachments are distinct.

File operations enforce name, boundary, symlink and regular-file rules. Archive entry
names are metadata and cannot choose a host path. Staging uses exclusive creation and
private permissions. Lexical path containment alone does not contain a tree.

Background shell completion returns to its conversation without indefinite manual
polling. Cancellation targets the process group with bounded cleanup and visible
failures. Reuse supported subprocess/runtime mechanisms before building alternatives.

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

Deferral follows usage and bounded slots. The current bridge qualifies servers with
at most four model-facing tools for two always-loaded slots in deterministic order.
Overflow stays discoverable. Four memory entry points remain loaded; the rest can be
found through search. An unmeasured global tool-count target cannot justify making
tools unreachable. MCP UI resources are rendering metadata, not extra model instructions.

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
does not close that answer-quality gap; the existing budget-triggered completion critic
is not a general validator of every background-delegation summary.

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

Edge and tagged releases have distinct publication contracts. Systemd appliance updates
may apply edge images automatically; pinned deployments have an explicit update process.
Acceptance records source revisions and image digests. Hot settings are not rollouts.

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
