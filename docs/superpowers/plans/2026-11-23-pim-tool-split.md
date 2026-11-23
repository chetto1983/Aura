# The PIM's tools, one per operation again: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The PIM advertises its 29 operations as 29 tools, and Aura keys risk, approvals, drafts, host calls and display on the tool name, with the operators' stored rules and drafts carried across.

**Architecture:** Three parts, shipped in order, each a working system on its own:
- **Part A**, an Aura release that pins the current PIM image by immutable sha, so the fork's `main` stops reaching appliances.
- **Part B**, the fork split: upstream's tool classes registered under the action names, with the fork's behaviours moved onto them.
- **Part C**, one Aura release that pins the split image and carries the re-keying, migration `0157`, a Go upgrade step and the texts.

**Tech Stack:** Go 1.26, PostgreSQL + sqlc + golang-migrate, the go-sdk MCP bridge, React/Vitest cockpit, .NET 10 + ModelContextProtocol C# SDK (fork), MSTest, Docker Compose, the VM updater.

**Spec:** [docs/superpowers/specs/2026-11-23-pim-tool-split-design.md](../specs/2026-11-23-pim-tool-split-design.md), approved by the operator 2026-11-23. Site inventory: a read-only sweep of 2026-11-23, cited below as `file:line`. Line numbers drift: re-read before editing.

## Global Constraints

- Stay on `master` (Aura) and `main` (fork). No branches, no worktrees (operator, 2026-11-23).
- The 29 tool names are exactly the keys of `trustedRecipeActions["recipe:calendar"]` (`internal/agent/mcptools/bridge_risk.go:54-102`), which equal the fork's `CalendarActionTool.ActionNames`. That includes `get_contextual_email_summary`, `mark_email_read` and `bulk_mark_emails_read`.
- The model-facing names are `calendar__<name>`. None reaches `namespacedName`'s 64-character cap (`name.go:9-13`); the longest is 38.
- Next migration: `ls internal/db/migrations/ | tail -1` at landing. It is `0156_message_draft_sender` today, so `0157`.
- No file over 600 lines. Refactor on touch, and remove dead code in the same commit.
- Go gates after each Go edit, in WSL: `go vet ./...`, `go build ./...`, `go test -race ./<touched packages>/`. Tagged tiers with the stack up: `-tags=db_integration`.
- Coverage floor 85% across the tag matrix (`scripts/coverage_gate.sh`). Mutation ≥70% on the risk table and on the draft target, run in CI.
- After editing `compose.yaml` or `deploy/*`: commit, then `make payload-manifest` in WSL, then commit `scripts/payload_manifest.txt`.
- Push only with the WSL recipe: `LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks push origin master`. "CI green" means every job on the pushed full SHA.
- Never run a `.exe` on Windows. Fork tests run in `mcr.microsoft.com/dotnet/sdk:10.0.403`, copying the tree in. That is `FORK_TEST <filter>` below:
  ```bash
  docker run --rm -v "D:\tmp\aura-pim-mcp:/src:ro" -v pim-nuget:/root/.nuget/packages mcr.microsoft.com/dotnet/sdk:10.0.403 bash -c '
    mkdir -p /work && cd /src && tar --exclude="*/bin" --exclude="*/obj" --exclude=.git -cf - . | tar -xf - -C /work && cd /work &&
    dotnet new globaljson --sdk-version 10.0.403 --roll-forward disable --force >/dev/null &&
    PIM_TEST_GREENMAIL=host.docker.internal:3143 PIM_TEST_DOVECOT=host.docker.internal:31143 \
    PIM_TEST_GREENMAIL_SMTP=host.docker.internal:3025 PIM_TEST_GREENMAIL_AUTH_SMTP=host.docker.internal:3465 \
    dotnet test src/CalendarMcp.Tests/CalendarMcp.Tests.csproj --filter "<filter>"'
  ```
  Thirteen snippet tests fail locally on the baseline too, though CI is green. A task is green when its own tests pass and the failure set is unchanged.
- Live writes on the VM: mail and events only between the operator's three accounts, subject or body marked `[aura-misura]`, one measurement at a time, everything deleted after, and the verification file holds counts and yes/no only. No real addresses or names in any committed file: the repository is public.
- No manual appliance update. The updater applies a release, and "Aggiorna subito" is the operator's click.

## Review Focus

1. **A grant or policy on an aliased PIM mount** (a server name other than `calendar`, or one long enough to be hashed). The upgrade must find it through `NamespacedName(server, "calendar")`, not by suffix. Test in Task C6.
2. **A draft in `dispatching` at the upgrade.** It is not rewritten and not expired; whatever its send did stays the truth. Test in Task C6.
3. **A stored grant whose `action` is not one of the 29** (the gateway stored the model's raw string). It is logged and dropped, never mapped to a tool. Test in Task C6.
4. **A conversation from before the split, replayed after it.** Its 3-key display marker still renders its PIM tables. Test in Task C5.
5. **The notifier on a PIM with no `list_accounts` tool mounted** (a partial or failed mount). The notification fails with an error naming the missing tool; it is never sent without an account. Test in Task C4.

## Decisions taken while writing this plan

- **The development image override gets its own name, `AURA_PIM_MCP_DEV_IMAGE`.** The spec says `AURA_PIM_MCP_IMAGE` itself would stay the override. But `TestRetiredEnvKeysStayRetired` (`cmd/aura/image_pin_artifacts_test.go:54-77`) fails when compose reads a retired key, and that test is right: a stale appliance value would outrank the pin. Task A1 corrects spec §5.7.
- **The pin is a tag, not a digest:** `ghcr.io/chetto1983/aura-pim-mcp:<full 40-character fork sha>`. The fork publishes that tag on every push to `main` (`aura-publish-image.yml:53-56`).
- **The Go upgrade step runs in `dbMigrate`** (`cmd/aura/db.go:51-74`), after `CheckMigrationHead`, under the migration lock. That is the precedent of `EnsureAuthulaReadable`. Migrations are SQL only (`internal/db/migrate.go:27`) and no version-keyed repair exists. The step runs on every migrate, so it is idempotent: it touches only rows still in the old shape. It runs as `aura_migrate`, which bypasses RLS.
- **It lives in a new package, `internal/pimsplit`.** It uses two exports added for it: `mcptools.NamespacedName` and `messagedrafts.Fingerprint`.
- **An account-less pending or approved email draft is expired** with `status='expired'`, `resolved_at=now()`, `outcome_code='account_required'`.
- **The fork modifies upstream's classes** (spec §4.2). No wrapper layer.
- **Parts A and B can each be pushed on their own. Part C is pushed once, after Task C8.** Every push to Aura's `master` publishes an `aura:edge` image that the updater applies on consent, or at 03:00 UTC with nothing running. A half-pushed Part C would run Aura's new keys against the old PIM image. Part C's commits therefore stay local until C8 is green. Parallel sessions share this working tree and its `master`, so none may push while Part C is open. The operator holds them off.
- **The deferred roster's calendar line grows from 1 name to 29** (`tools/manifest.go:100-135`), on every turn. The spec does not price this. Task C8 measures the manifest's size before and after, and records it in the verification file.

## File structure

| Part | Files | Responsibility |
|---|---|---|
| A | `compose.yaml`, `scripts/install_env.sh`, `deploy/aura-image-update.sh` (+ `.service` header), `.github/workflows/ci.yml`, `scripts/pim_mail_e2e.sh`, `scripts/pim_tls_mail_up.sh`, `.claude/skills/aura-model-in-the-loop/pim_up.sh`, `cmd/aura/image_pin_artifacts_test.go`, `cmd/aura/container_artifacts_test.go`, `scripts/payload_manifest.txt` | one pinned PIM image everywhere |
| B | fork `src/CalendarMcp.Core/Tenancy/TenantBinding.cs` (new), the 29 upstream `Tools/*Tool.cs`, `Tools/EventRef.cs`, `Tools/CalendarMcpSurface.cs` (new; replaces `CalendarActionTool*.cs` and `CalendarActionArguments.cs`), `Apps/CalendarView.cs`, `Apps/ui/view.html`, `Configuration/ServiceCollectionExtensions.cs`, tests under `src/CalendarMcp.Tests/Tools/` and `Tenancy/`, `AURA-FORK.md`, `docs/`, `.github/workflows/aura-publish-image.yml` comment | per-operation surface |
| C | `internal/agent/mcptools/{bridge.go,bridge_risk.go,bridge_display_source.go,bridge_call.go,name.go}` (delete `bridge_multiplex.go`), `internal/gateway/classify.go`, `internal/messagedrafts/{validation.go,review.go,store.go}`, six `TrustedOutboundTarget` callers, `internal/agent/tools/{task_message.go,task.go,mail_watch.go}`, `internal/cron/{notify.go,notify_email.go}`, `cmd/aura/{serve_scheduled_message.go,serve_notify.go,serve_notify_account.go,serve_pim_senders.go,db.go}`, `internal/agent/display/{mcp_source.go,sidecars.go}`, `web/src/settings/PolicyToolPicker.tsx`, `internal/db/migrations/0157_*`, `internal/pimsplit/` (new), skills, scripts, fixtures | tool-keyed Aura and the upgrade |

---

## Part A: pin the current PIM (Aura release)

### Task A1: One pinned PIM image, no appliance override

**Files:**
- Modify: `compose.yaml:1353-1359`, `scripts/install_env.sh:273`, `deploy/aura-image-update.sh:23,49`, `deploy/aura-image-update.service:16`, `.github/workflows/ci.yml:1494,1978`, `scripts/pim_mail_e2e.sh:14`, `scripts/pim_tls_mail_up.sh:24-30`, `.claude/skills/aura-model-in-the-loop/pim_up.sh:23`, `docs/superpowers/specs/2026-11-23-pim-tool-split-design.md` §5.7
- Test: `cmd/aura/image_pin_artifacts_test.go`, `cmd/aura/container_artifacts_test.go:118-119`

**Interfaces:**
- Produces: `PIM_PIN`, the 40-character fork sha. Read it with `git -C D:/tmp/aura-pim-mcp rev-parse origin/main` and check it with `docker manifest inspect ghcr.io/chetto1983/aura-pim-mcp:$PIM_PIN`. Task C8 replaces it.

- [ ] **Step 1: Write the failing test** `TestThePIMImageIsPinnedEverywhere` in `image_pin_artifacts_test.go`:
  - `composeDefault` of the `aura-pim-mcp` image in `compose.yaml` matches `^ghcr\.io/chetto1983/aura-pim-mcp:[0-9a-f]{40}$`;
  - `ci.yml`, `scripts/pim_mail_e2e.sh` and `scripts/pim_tls_mail_up.sh` name that same reference and no `aura-pim-mcp:latest` or `:sidecar`.

  Then extend `TestRetiredEnvKeysStayRetired` to scan `scripts/install_env.sh` as well as `scripts/install.sh`, and add `AURA_PIM_MCP_IMAGE` to `RETIRED_ENV_KEYS`.
- [ ] **Step 2: Run** `go test ./cmd/aura/ -run 'TestThePIMImageIsPinnedEverywhere|TestRetiredEnvKeysStayRetired|TestContainerArtifacts'`. Expected: FAIL on the compose default, the CI reference, compose reading `${AURA_PIM_MCP_IMAGE`, and `install_env.sh` writing it.
- [ ] **Step 3: Implement.**
  - compose: `image: ${AURA_PIM_MCP_DEV_IMAGE:-ghcr.io/chetto1983/aura-pim-mcp:<PIM_PIN>}` and `pull_policy: ${AURA_PIM_MCP_PULL_POLICY:-missing}`. Replace the "MOVING tag" comment with the reason for the pin: on 2026-11-23 the fork's `main` reached appliances before the Aura that matched it.
  - Delete the `ensure_env_default AURA_PIM_MCP_IMAGE` line and fix the function's comment.
  - Update the updater's header lines.
  - CI and scripts use the pinned reference; the scripts honour `AURA_PIM_MCP_DEV_IMAGE`.
  - Update `container_artifacts_test.go`'s two pinned lines to the new text.
  - In spec §5.7, the override sentence names `AURA_PIM_MCP_DEV_IMAGE` and cites the test.
- [ ] **Step 4: Run** the step 2 tests and `go vet ./cmd/aura/`. Expected: PASS.
- [ ] **Step 5: Commit**, then `make payload-manifest`, then commit the manifest:
  ```bash
  git add compose.yaml scripts/install_env.sh deploy/aura-image-update.sh deploy/aura-image-update.service .github/workflows/ci.yml scripts/pim_mail_e2e.sh scripts/pim_tls_mail_up.sh .claude/skills/aura-model-in-the-loop/pim_up.sh cmd/aura/image_pin_artifacts_test.go cmd/aura/container_artifacts_test.go docs/superpowers/specs/2026-11-23-pim-tool-split-design.md
  git commit -m "Pin the PIM image by sha and retire the appliance override"
  make payload-manifest && git add scripts/payload_manifest.txt && git commit -m "Regenerate the payload manifest for the PIM pin"
  ```

### Task A2: Ship the pin and prove a fork push no longer moves the VM

- [ ] **Step 1:** Push with the WSL recipe. Wait until every job on the full SHA is green and `Publish Aura edge image` has finished.
- [ ] **Step 2:** Ask the operator for "Aggiorna subito". Confirm in `journalctl -u aura-image-update.service` that the release applied. Check that `docker inspect aura-pim-mcp --format '{{.Config.Image}}'` ends in `PIM_PIN`, and that `/opt/aura/.env` no longer holds `AURA_PIM_MCP_IMAGE`: report the count of matching lines, never the value.
- [ ] **Step 3:** Record the pin release in a new `docs/superpowers/verification/<date>-pim-tool-split.md`: what was measured, and what it does not show. That a fork push leaves the VM alone is proven in Task B5. Commit and push.

---

## Part B: the fork (`chetto1983/aura-pim-mcp`, `main`)

Start only after Task A2: from then on a push to the fork's `main` reaches no appliance.

### Task B1: Tenant binding for every tool

**Files:**
- Create: `src/CalendarMcp.Core/Tenancy/TenantBinding.cs`
- Modify: `Tools/CalendarActionTool.cs:220-232` (its own bind goes)
- Test: `src/CalendarMcp.Tests/Tenancy/TenantBindingTests.cs`

**Interfaces:**
- Produces: `public static IMcpServerBuilder WithTenantBinding(this IMcpServerBuilder builder)`. It registers a call-tool filter that binds `TenantIdentity.FromPrincipal(context.User)` through `ITenantContext.Bind` around `next`. An `ArgumentException` becomes `McpException(ex.Message)`, as today.

- [ ] **Step 1: Write the failing tests:**
  - `EveryToolCallRunsInsideTheCallersTenant`: an in-process session as tenant A calls `list_accounts` and sees only A's accounts;
  - `ACallerWithoutATenantIsRefusedBeforeTheTool`.
- [ ] **Step 2: Run** `FORK_TEST TenantBinding`. Expected: FAIL, `WithTenantBinding` undefined.
- [ ] **Step 3: Implement** the filter, add it to `WithCalendarMcpSurface()` before `WithCalendarActionTool()`, and delete the bind from `CalendarActionTool`.
- [ ] **Step 4: Run** `FORK_TEST "TenantBinding|CalendarActionTool|Tenancy"`. Expected: PASS.
- [ ] **Step 5: Commit** `Bind the caller's tenant in a call-tool filter`.

### Task B2: The event tools speak EventRef

**Files:**
- Modify: `Tools/GetCalendarEventsTool.cs`, `CreateEventTool.cs`, `GetCalendarEventDetailsTool.cs`, `UpdateEventTool.cs`, `DeleteEventTool.cs`, `RespondToEventTool.cs`; `Tools/CalendarActionTool.Calendar.cs` (shrinks to direct forwarding)
- Test: move `CalendarActionToolEventRefTests.cs` cases into the six `*ToolTests.cs`

**Interfaces:**
- Consumes: `EventRef` (`Tools/EventRef.cs`), unchanged.
- Produces:
  - `GetCalendarEvents` and `CreateEvent` return each event's `eventId` as an `EventRef` and no raw `id`;
  - the other four take `eventId` (an `EventRef`) and no `accountId` parameter.

- [ ] **Step 1: Write the failing tests**, the `CalendarActionToolEventRefTests` cases retargeted at each class:
  - `CreateEvent_ReturnsAReferenceToTheCreatedEvent`;
  - the update, delete and respond tools forward the decoded event and echo the reference;
  - `EventDetails_NotFoundNamesTheReferenceNotTheProviderId`;
  - `EventTools_RejectAProviderIdPassedAsReference`.

  Add: none of the four reference-taking tools has an `accountId` parameter in its input schema. `MarkRead_RequiresAnExplicitIsRead` moves to `MarkEmailAsReadToolTests` and `BulkMarkEmailsAsReadToolTests` in Task B4.
- [ ] **Step 2: Run** `FORK_TEST "EventRef|CalendarEvent|CreateEvent|UpdateEvent|DeleteEvent|RespondToEvent"`. Expected: FAIL.
- [ ] **Step 3: Implement.** Move the reference minting and resolving out of `CalendarActionTool.Calendar.cs` into the six classes. The curated tool forwards to them unchanged until Task B4 deletes it.
- [ ] **Step 4: Run** the step 2 filter and `FORK_TEST CalendarActionTool`. Expected: PASS.
- [ ] **Step 5: Commit** `Move the event references onto the event tools`.

### Task B3: send_email requires the account; the attachment answers with a link

**Files:**
- Modify: `Tools/SendEmailTool.cs`, `Tools/GetEmailAttachmentTool.cs`, `Tools/CalendarActionTool.Delegated.cs:60`, `Tools/CalendarActionTool.Attachments.cs`
- Test: `SendEmailToolTests.cs`, `GetEmailAttachmentToolTests.cs`

**Interfaces:**
- Produces:
  - `SendEmail(string accountId, …)`, with a non-nullable, required `accountId`. The recipient-domain routing and the first-account fallback are deleted.
  - An empty `accountId` throws `McpException("send_email requires accountId, the account to send from: call list_accounts and pass its accountId. When the user has more than one account and has not named one, ask which.")`.
  - `GetEmailAttachment` returns the `attachment://` resource link plus the id, as the curated action does today.

- [ ] **Step 1: Write the failing tests:**
  - `SendEmail_SchemaRequiresAccountId`: `accountId` is in the tool's `required` list;
  - `SendEmail_WithoutAnAccount_IsRefusedAndNothingIsSent`;
  - `GetEmailAttachment_AnswersWithAResourceLink`.
- [ ] **Step 2: Run** `FORK_TEST "SendEmailTool|GetEmailAttachmentTool"`. Expected: FAIL.
- [ ] **Step 3: Implement.** Move the curated action's guard and link-building into the two classes. The curated actions forward unchanged.
- [ ] **Step 4: Run** `FORK_TEST "SendEmail|Attachment|CalendarActionTool"`. Expected: PASS.
- [ ] **Step 5: Commit** `Require the account on send_email and link attachments from the tool`.

### Task B4: Register the 29 tools; delete the curated tool

**Files:**
- Create: `Tools/CalendarMcpSurface.cs` (`WithCalendarMcpSurface`, moved here)
- Delete: `Tools/CalendarActionTool.cs`, `.Calendar.cs`, `.Delegated.cs`, `.Attachments.cs`, `Tools/CalendarActionArguments.cs`, `Tests/Tools/CalendarActionTool*Tests.cs` (after porting what Tasks B2 and B3 did not)
- Modify: the 29 tool classes (explicit `[McpServerTool(Name = "<action name>")]`; one short description each), `Apps/CalendarView.cs`, `Apps/ui/view.html:5,540-560`, `Configuration/ServiceCollectionExtensions.cs:118`, `Tests/Helpers/InProcessMcpSession.cs`
- Test: `src/CalendarMcp.Tests/Tools/SurfaceContractTests.cs` (new)

**Interfaces:**
- Produces: `tools/list` returns exactly the 29 names. The read tools whose results the view renders carry `_meta.ui.resourceUri = ui://calendar/view.html`: `list_accounts`, `get_emails`, `get_email_details`, `search_emails`, `list_calendars`, `get_calendar_events`, `get_calendar_event_details`, `get_contacts`, `search_contacts`, `get_contact_details`.

- [ ] **Step 1: Write the failing test** `SurfaceContractTests`, through `InProcessMcpSession`:
  - the advertised name set equals the 29 names;
  - no schema has an `action` property;
  - pinned `required` lists:
    - `send_email` = {`accountId`,`to`,`subject`};
    - `get_email_details` = {`accountId`,`emailId`};
    - `get_calendar_events` = {`timeZone`};
    - `get_calendar_event_details` = {`timeZone`,`calendarId`,`eventId`};
    - `create_event` = {`subject`,`start`,`end`};
    - `mark_email_read` = {`accountId`,`emailId`,`isRead`};
  - exactly the ten tools above carry `_meta.ui`.

  Retarget the view test so `view.html`'s detail calls name `get_email_details` and `get_calendar_event_details` with no `action`.
- [ ] **Step 2: Run** `FORK_TEST "SurfaceContract|CalendarView"`. Expected: FAIL, one tool named `calendar`.
- [ ] **Step 3: Implement.**
  - `WithCalendarMcpSurface()`: `WithTenantBinding()`, `WithTools<…>` over the 29 classes, the view, the attachment resource, `WithStringScalarArguments()`, and the three prompt classes.
  - Set `_meta.ui` per tool through `McpApps.SetAppUi`.
  - Delete the curated files and the now-dead `ServiceCollectionExtensions` comment.
  - Keep each description within D-36's budget: one or two sentences, with per-field detail in the parameter descriptions.
- [ ] **Step 4: Run** the full suite `FORK_TEST "FullyQualifiedName~CalendarMcp"`. Expected: the only failures are the 13 known snippet failures.
- [ ] **Step 5: Commit** `Advertise one tool per operation again`.

### Task B5: Record, publish, and check the image

- [ ] **Step 1:** Rewrite `AURA-FORK.md` change 4 and items 14 and 4's bullet, so they record each upstream class modification. Update `docs/` (providers, configuration) where they name the `calendar` tool, and the `aura-publish-image.yml` comment that says Aura follows `latest`.
- [ ] **Step 2:** Commit and push `main`. Wait for `aura-publish-image` and CI to be green on the sha, then record `SPLIT_PIN` = that full sha.
- [ ] **Step 3: Live check.** Run `ghcr.io/chetto1983/aura-pim-mcp:$SPLIT_PIN` locally (`scripts/pim_tls_mail_up.sh` with `AURA_PIM_MCP_DEV_IMAGE`) and call `tools/list`. Expected: 29 names, the `send_email` required list above.
- [ ] **Step 4:** On the VM, confirm the PIM container still runs `PIM_PIN`. The appended line in the verification file says a fork push left the VM unchanged.

---

## Part C: Aura keyed on the tool (one release)

Commits stay local until Task C8 (see Decisions).

### Task C1: Bridge and risk, tool-keyed

**Files:**
- Delete: `internal/agent/mcptools/bridge_multiplex.go`, `bridge_multiplex_test.go`, `bridge_multiplex_classifier_test.go`, `export_test.go:9-13`
- Modify: `bridge.go:147-157,175-187`, `bridge_risk.go:38-50,197-245`, `bridge_display_source.go:6-28`, `bridge_call.go:124`, `name.go:49`, comments `bridge_policy.go:30-33`, `bridge_deferral.go:20-27`
- Test: `bridge_risk_test.go`, `bridge_display_source_test.go:67-73`, `bridge_deferral_test.go:178-196`

**Interfaces:**
- Produces:
  - `func NamespacedName(namespace, tool string) string`, the renamed export of `namespacedName`. All callers are updated.
  - `func CalendarToolNames() []string`, the sorted keys of `trustedRecipeActions["recipe:calendar"]`.
  - `classifyToolRisk` returns Destructive for any `recipe:calendar` tool absent from the table, whatever its annotations.
  - `displaySource(source, tool string)` drops its args parameter and returns `{recipe, tool}` for calendar too.

- [ ] **Step 1: Write the failing tests:**
  - `TestUnknownCalendarToolIsDestructiveDespiteReadOnlyHint`: a `recipe:calendar` tool `brand_new` with `readOnlyHint:true` is `(Mutating, Destructive)`;
  - `TestCalendarReadToolIsNotMutating`: `get_emails` gives `Mutating == false`;
  - `TestCalendarSendIsDestructive`: `send_email`;
  - `TestCalendarDisplayMarkerIsRecipeAndTool`: `{"recipe":"recipe:calendar","tool":"get_emails"}`, two keys;
  - `TestCalendarMountDefersEveryTool`: 29 tools, all deferred;
  - `TestCalendarToolNameDriftWarns`: an advertised name missing from the table, and a table key not advertised, each produce a WARN naming it.
- [ ] **Step 2: Run** `go test ./internal/agent/mcptools/`. Expected: FAIL, compile errors on the deleted multiplex symbols.
- [ ] **Step 3: Implement.** Delete the multiplex file and the `Multiplexed` block. Move the name-set drift warning into `bridgeToolsWithPolicy`. Make an unknown `recipe:calendar` tool fail closed. Remove the calendar branch from `displaySource`. Export `NamespacedName`.
- [ ] **Step 4: Run** `go vet ./... && go test -race ./internal/agent/mcptools/`. Expected: PASS. `go build ./...` fails only in `internal/gateway`, which Task C2 fixes.
- [ ] **Step 5: Commit** together with C2 (the build is red between them): see C2 step 5.

### Task C2: Gateway without the calendar classifier

**Files:**
- Modify: `internal/gateway/classify.go:32-43,265-290`, comments `scope.go:34-36`, `approve.go:119`, `cmd/aura/gateway_grants.go:26-29`
- Delete: `internal/gateway/classify_multiplexed_comms_test.go`
- Test: `grants_test.go:195-234`, `scope_test.go:29-149`

- [ ] **Step 1: Write the failing tests:**
  - `TestCalendarSubjectIsTheToolAlone`: `subjectFor(calendar__send_email, {"to":…})` is `(calendar__send_email, "")`;
  - `TestAliasedCalendarToolKeepsItsOwnSubject`: `work__send_email` likewise;
  - `TestGrantOnSendEmailDoesNotCoverDeleteEmail`.

  Convert `grants_test.go`'s per-action grants to per-tool grants.
- [ ] **Step 2: Run** `go test ./internal/gateway/`. Expected: FAIL.
- [ ] **Step 3: Implement.** Delete `classifyCalendarAction` and its `multiplexedClassifiers` entry, and fix the comments and the CLI usage text.
- [ ] **Step 4: Run** `go vet ./... && go build ./... && go test -race ./internal/gateway/ ./internal/agent/mcptools/ ./internal/agui/`. Expected: PASS.
- [ ] **Step 5: Commit** C1 and C2: `Key the PIM's risk and approvals on its tool names`.

### Task C3: Message drafts target `send_email`

**Files:**
- Modify:
  - `internal/messagedrafts/validation.go:37-50,69-70,202-204`, `review.go:13,36,61`, `store.go:404-413`;
  - the six callers `cmd/aura/serve_scheduled_message.go:38`, `internal/agent/llm_agent_message_draft.go:27-28`, `internal/gateway/message_draft.go:30,60`, `internal/cron/handlers/message.go:147`, `internal/cron/handlers/agentjob_outbound.go:26`, `internal/runner/message_draft_resume.go:162`;
  - `web/src/approvals/__tests__/MessageDraftCard.test.tsx:13`.
- Test: `validation_test.go`, `review_test.go`, `store_test.go`, `store_integration_test.go`, `sender_test.go`, `pause_test.go`, `llm_agent_message_draft_test.go`, `gateway/message_draft_test.go`, `cron/handlers/message_test.go:341`, `agui/message_drafts_api_test.go`, `telegram/hitl_message_draft_test.go`, `bot_dispatch_review_state_test.go`

**Interfaces:**
- Produces:
  - `func TrustedOutboundTarget(spec tools.Spec) (Target, bool)`. It returns `Target{Recipe:"recipe:calendar", Tool:"send_email"}` when `spec.TrustedRecipeSource == "recipe:calendar" && spec.TrustedRecipeTool == "send_email"`, and the WhatsApp target as today.
  - `func Fingerprint(t Target, args json.RawMessage) (string, error)`, the export of `argsFingerprint`, unchanged in what it hashes.
  - `reviewArgs` loses `Action`.
  - The email allow-list loses `"action"`.

- [ ] **Step 1: Write the failing tests:**
  - `TestEmailTargetIsTheSendEmailTool`;
  - `TestEmailArgsWithActionAreRefused`: `"action"` is now an unknown key, and the refusal names it;
  - `TestAccountIdStaysOptionalBeforeReview` (operator decision);
  - `TestFingerprintIsStableAcrossKeyOrder`.

  Convert the listed test files to the new target and args.
- [ ] **Step 2: Run** `go test ./internal/messagedrafts/`. Expected: FAIL.
- [ ] **Step 3: Implement** the signature change and update the six callers.
- [ ] **Step 4: Run** `go vet ./... && go build ./... && go test -race ./internal/messagedrafts/ ./internal/agent/ ./internal/gateway/ ./internal/cron/... ./internal/runner/ ./internal/agui/ ./internal/channels/telegram/ ./cmd/aura/`, plus `-tags=db_integration ./internal/messagedrafts/`, and `npm test -- MessageDraftCard` in `web/`. Expected: PASS.
- [ ] **Step 5: Commit** `Review an email draft as a send_email call`.

### Task C4: Host-built calls name the tool

**Files:**
- Modify: `internal/agent/tools/task_message.go:65,75`, `task.go:176`, `mail_watch.go:47`, `internal/cron/notify.go:14,73-75`, `notify_email.go:20`, `cmd/aura/serve_scheduled_message.go:25`, `serve_notify.go:53-66`, `serve_notify_account.go`, `serve_pim_senders.go:29,43`
- Test: `task_message_test.go:171`, `spec_test.go:136`, `cron/notify_test.go:121,126,257`, `cmd/aura/serve_notify_test.go:69`, `serve_notify_account_test.go`, `serve_pim_senders_test.go`

**Interfaces:**
- Consumes: `ByTrustedRecipe(recipe, tool string)` (`internal/agent/tools/spec.go:295`).
- Produces:
  - The email route is `{recipe:"recipe:calendar", tool:"send_email"}`, and `emailArgs` has no `"action"`.
  - `pimSelfSendTool{send selfSendTool; reg *tools.Registry}` resolves `ByTrustedRecipe("recipe:calendar","list_accounts")` per send and executes it with `{}`.
  - `pimSenders.SendingAddress` does the same.

- [ ] **Step 1: Write the failing tests:**
  - `TestEmailNotificationRidesSendEmail`: it resolves `recipe:calendar send_email`, and the args have no `action`;
  - `TestNotificationListsAccountsThroughItsOwnTool`: two fake tools, `list_accounts` called with `{}`, then `send_email` with the recipient's account;
  - `TestNotificationWithoutListAccountsToolFails` (Review Focus 5): the error names `list_accounts`, and nothing is sent;
  - `TestRecipeArgsCarryNoAction`;
  - `TestSendingAddressUsesListAccountsTool`.
- [ ] **Step 2: Run** `go test ./cmd/aura/ ./internal/cron/ ./internal/agent/tools/`. Expected: FAIL.
- [ ] **Step 3: Implement**, and update the texts in `errMessageNeedsAccount`, the task description and the mail-watch description to name `calendar__list_accounts`.
- [ ] **Step 4: Run** `go vet ./... && go test -race ./cmd/aura/ ./internal/cron/... ./internal/agent/tools/`. Expected: PASS.
- [ ] **Step 5: Commit** `Call the PIM's send_email and list_accounts tools by name`.

### Task C5: Display reads the new marker and the old one

**Files:**
- Modify: `internal/agent/display/mcp_source.go:19-58`, `display/sidecars.go:93-103`, comment `web/src/settings/PolicyToolPicker.tsx:49`
- Test: `display/mcp_source_test.go`, `display/sidecars_test.go:9-10,68`, `internal/agent/llm_agent_events_preview_test.go:15-38`, `agui/server_display_mcp_test.go:118-129`, `agui/approval_policies_tools_test.go:38,53`, `web/src/settings/__tests__/ToolPoliciesPanel.test.tsx`, `web/src/chat/__tests__/ToolActivityCard_mcp.test.tsx:15`, `sseAdapter_snapshot.test.ts:121`, `web/src/chat/mcpapps/hostProtocol.test.ts:119`

**Interfaces:**
- Produces:
  - `TrustedMCPFromMeta` accepts `{recipe:"recipe:calendar", tool:<one of 29>}` (2 keys), and still accepts the legacy 3-key `{recipe, tool:"calendar", action}`.
  - `normalizeSidecarPreview` looks up `calendarReadSpecs[marker.Tool]` for the new marker and `[marker.Action]` for the legacy one.

- [ ] **Step 1: Write the failing tests:**
  - `TestNewCalendarMarkerRendersItsTable`, with `get_emails`;
  - `TestLegacyCalendarMarkerStillRenders` (Review Focus 4): the exact 3-key marker stored before the split gives the same table;
  - `TestCalendarMarkerWithUnknownToolRendersNothing`.

  The policy picker test shows `calendar__send_email` as a plain tool with no action list.
- [ ] **Step 2: Run** `go test ./internal/agent/display/ ./internal/agui/` and `npm test -- ToolPoliciesPanel ToolActivityCard sseAdapter hostProtocol` in `web/`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the step 2 commands, plus `go test -race ./internal/agent/...`. Expected: PASS.
- [ ] **Step 5: Commit** `Render PIM tables from the tool name, and from the old marker`.

### Task C6: Migration 0157 and the upgrade step

**Files:**
- Create: `internal/db/migrations/0157_pim_tool_split.up.sql` / `.down.sql`, `internal/pimsplit/upgrade.go`, `internal/pimsplit/upgrade_integration_test.go` (`db_integration`), `internal/pimsplit/upgrade_test.go`
- Modify: `cmd/aura/db.go:51-74`

**Interfaces:**
- Consumes: `mcptools.NamespacedName` and `mcptools.CalendarToolNames` (Task C1), `messagedrafts.Fingerprint` (Task C3).
- Produces: `func Upgrade(ctx context.Context, conn *pgx.Conn, log *slog.Logger) (Report, error)`, with `Report{Rewritten, FannedOutDenies, Dropped, DraftsRewritten, DraftsExpired int}`. It runs in one transaction and is idempotent.

- [ ] **Step 1: Write the migration.**
  - Look up the unnamed CHECK's name from `pg_constraint` in a `DO` block; never assume `message_drafts_check`.
  - Drop it and add `message_drafts_target_check`: `(recipe='recipe:calendar' AND ((tool_name='send_email' AND action='') OR (tool_name='calendar' AND action='send_email'))) OR (recipe='recipe:whatsapp' AND tool_name='send_message' AND action='')`.
  - The down migration restores 0134's original CHECK. It fails while new-shape rows exist, and its comment says so.
- [ ] **Step 2: Write the failing integration tests.** Seed a migrated database. Each `ns` is every `aura.mcp_server` row with source `recipe:calendar`, including one alias long enough to be hashed.
  - **Grants and policies:**
    - `TestPerActionGrantBecomesAToolGrant`: `(NamespacedName(ns,"calendar"), "send_email")` → `(NamespacedName(ns,"send_email"), "")`.
    - `TestToolWideDenyFansOutToEveryTool`: 29 `deny` rows, none left on `…__calendar`.
    - `TestToolWideGrantAndAskAreLoggedAndDropped`: gone, and the log names tool, action and kind.
    - `TestHandTypedNewNameRowIsDropped`: a pre-existing `calendar__send_email` row.
    - `TestUnknownActionIsDropped` (Review Focus 3).
    - `TestDenyWinsOverARewrittenAsk`.
    - `TestAliasIsMatchedByNamespacedName` (Review Focus 1).
  - **Drafts:**
    - `TestPendingDraftWithAccountIsRewritten`: target `(recipe:calendar, send_email, "")`, `registered_tool_name = NamespacedName(ns,"send_email")`, and no `action` in `original_args` or `effective_args`. Both fingerprints equal what `Create` computes today for the same args under the new target, compared against a draft made through `Create`.
    - `TestApprovedScheduledDraftWithAccountIsRewritten`.
    - `TestDraftWithoutAccountIsExpired`: `expired`, `account_required`, `resolved_at` set.
    - `TestDispatchingDraftIsUntouched` (Review Focus 2).
    - `TestResolvedDraftKeepsItsOldShape`.
  - **Idempotence:** `TestUpgradeTwiceChangesNothingTheSecondTime`, where the second `Report` is all zeros.
- [ ] **Step 3: Run** `go test -tags=db_integration ./internal/pimsplit/` against a disposable database (`bash scripts/coverage_docker.sh` style; never the live `aura` database). Expected: FAIL, `Upgrade` undefined.
- [ ] **Step 4: Implement** `Upgrade`, and call it in `dbMigrate` after `CheckMigrationHead` on a `cfg.DB.MigrateURL` connection. Log the `Report` and each dropped row at INFO. Read `NamespacedName(ns,"calendar")` per server row; never match by suffix.
- [ ] **Step 5: Run** step 3 again, plus `go test -race ./cmd/aura/ ./internal/pimsplit/` and `make db-migrate` twice on the dev stack. Expected: PASS, and the second migrate logs a zero report.
- [ ] **Step 6: Commit** `Carry approvals and drafts across the PIM tool split`.

### Task C7: Texts, corpus and the search gate

**Files:**
- Modify:
  - `internal/skills/embed/briefing-aura/SKILL.md:26,28,30,54-57`;
  - `.claude/skills/aura-model-in-the-loop/SKILL.md:95`;
  - `scripts/briefing_baseline.py:12-13,117-130` and `scripts/briefing_baseline_test.py:99-110`;
  - `web/e2e/mcp-cockpit-live.spec.ts:238`;
  - `internal/agent/tools/testdata/deferred_manifest.json` (regenerated), `testdata/blind_eval_2026-10-06.json:343,350,357,364`;
  - `search_gate_test.go:107,168`, `bm25_variants_measure_test.go:47`.

- [ ] **Step 1: Write the failing test** `TestEveryPIMQueryFindsItsToolInTopFive` in `search_gate_test.go`: for each eval query whose gold label is a `calendar__*` tool, that tool is in `tool_search`'s top 5. Set the gold labels to the specific tools; for example a "send" query's gold is `calendar__send_email`. Change the counted family literal `"calendar": 1` to 29.
- [ ] **Step 2: Run** `go test ./internal/agent/tools/ -run 'SearchGate|TopFive'`. Expected: FAIL; the corpus still holds `calendar__calendar`.
- [ ] **Step 3: Implement.**
  - Regenerate the corpus with `aura toolpipe --manifest-json` (`search_gate_test.go:15-17`) from a dev stack running `SPLIT_PIN`.
  - Rewrite the skill and prompt texts to call `calendar__get_emails`, `calendar__send_email` and so on, with no `action`.
  - `briefing_baseline.py`'s `kept_source` accepts both the old `(calendar__calendar, action)` shape and `calendar__<tool>`, since old runs replay too.
- [ ] **Step 4: Run** the step 2 command, `python -m pytest scripts/briefing_baseline_test.py`, and `go test ./internal/skills/...`. Expected: PASS. If a query misses its top 5, that is a finding: record it in the verification file and stop for the operator. Never relax the gate.
- [ ] **Step 5: Commit** `Name the PIM's tools in skills, prompts and the search corpus`.

### Task C8: Pin the split image, live tests, gates, ship

**Files:**
- Modify:
  - `compose.yaml` and the Task A1 references (`PIM_PIN` → `SPLIT_PIN`);
  - `internal/mcp/calendar_integration_test.go:55-130`: 29 tools, per-tool `required`, no `action`;
  - `internal/mcp/calendar_call_integration_test.go:14`, `pim_mail_e2e_test.go:53-113`, `internal/webauth/mcp_resources_live_integration_test.go:103-104`: call the tools by name.

- [ ] **Step 1:** Replace the pin, and update the live tests to the per-tool surface.
- [ ] **Step 2: Run** in WSL with the stack up and the PIM at `SPLIT_PIN`:
  - the tagged live tests: `go test -tags=calendar_integration ./internal/mcp/`, `go test -tags=pim_mail_e2e ./internal/mcp/` and `go test -tags=mcp_live_integration ./internal/webauth/`;
  - `make quality-full`;
  - `bash scripts/coverage_docker.sh`.

  Expected: all green, and coverage ≥85%.
- [ ] **Step 3: Commit**, regenerate the payload manifest, commit it. Then push all of Part C with the WSL recipe. Wait until every job on the full SHA is green, mutation included.
- [ ] **Step 4:** Ask the operator for "Aggiorna subito". Confirm that `aura-migrate` logged the upgrade `Report`, and that the PIM runs `SPLIT_PIN`.
- [ ] **Step 5: Run spec §7 checks 1–10 on the VM, one at a time,** under the live-write rules. Use the mounted PIM for checks 1, 2's Sent copy and 4. Use the cockpit API for 2, 3, 6, 7 and 10. Use `aura task schedule --kind reminder --notify email` for 9. Before the upgrade, seed for 5 and 6:
  - a `deny` on `calendar` / `send_email`;
  - a tool-wide `deny` on `calendar`, removed after check 5;
  - one pending draft with an account and one without;
  - one approved scheduled draft with an account.

  Also record the deferred manifest's size before and after (Decisions). Append each result to the verification file.

### Task C9: Documents after the measurement

- [ ] **Step 1: PRD amendments.** Each cites the date and the verification file and says what the measurement does not show:
  - §4: `prd.md:354,359,513`;
  - §5: `:581,598`;
  - §13: `:3454,3844-3862`;
  - §15: `:4259,4559`.
- [ ] **Step 2:** Put a superseded banner on `2026-08-17-mcp-curated-surface-design.md` that names the split spec, and rewrite the PIM paragraph of `PROVENANCE.md`.
- [ ] **Step 3:** Commit and push (documents only).

Not in this plan: reconciling `plans/2026-11-23-pim-mail-calendar-rows.md` (spec §6) and the pre-review `accountId` decision (spec §5.3). Both start from Task C8's verification file.
