# Plan validation (grounding): connecting and repairing a mail account

- **Plan:** `docs/superpowers/plans/2026-11-20-pim-account-connect-repair.md`
- **Spec:** `docs/superpowers/specs/2026-11-20-pim-account-connect-repair-design.md`
- **Date:** 2026-11-20. Read-only: this file is the only one written.
- **Trees read:**
  - Aura `master` at `7f2915b39`. The working tree holds unrelated dirty files from another session (task approval, runner); none of them is cited below.
  - PIM fork `D:/tmp/aura-pim-mcp` `main` at `bb87b82`, clean.
  - GreenMail source at tag `release-2.1.15`, read through `gh api`.

## Verdict: RED until the plan is amended

The architecture holds against the code. These pieces exist and behave as the plan assumes:

- the moving PIM tag;
- the per-resource `last_error`/`failing_since`;
- `RunTaskNow`, `PausedByFailures` and `findWatch`;
- the singletons;
- the key ring on the data volume.

The plan cannot be executed as written. Five findings meet the brief's CRITICAL bar ("would not build or would do the wrong thing"):

- **C1.** The cockpit cannot read the 422's `step`/`reason`, so every verify message in W2 and W4 would be blank.
- **C2.** A member cannot reach the Settings surface at all. The shell bounces every non-admin to chat, so the plan's main user-facing goal, "each person connects their own mail, from their own settings", is not met.
- **C3.** The new PATCH relay lets a member replace the admin-set OAuth client. No compiler and no planned test would notice.
- **C4.** A1 would not build: `t.store` is an interface, and so is its test stub.
- **C5.** P4 would not build: `IMemoryCache` has neither a package nor a registration in Core.

C4 and C5 are CRITICAL only by the brief's definition. The compiler finds both at once, and each is a one-line fix.

A sixth finding is HIGH: the harness P3 names cannot host its tests.

The fixes keep the architecture. C2 is the exception: it needs a product decision about the shell, recorded in the plan.

| Severity | Count |
|---|---|
| CRITICAL | 5 |
| HIGH | 1 |
| MEDIUM | 8 |
| LOW | 13 |

---

## CRITICAL

### C1. W1: `pimVerifyFailure` cannot read `step`/`reason`, because `HttpError` drops them

**Plan:** W1 says `pimVerifyFailure(error: unknown)` "reads a 422 `HttpError` body". W2 step 6 and W4 then build every message from it.

**Evidence:**
- `web/src/api/json.ts:28-31` keeps exactly two fields of the error body:
  ```ts
  export async function httpErrorFrom(res: Response): Promise<HttpError> {
    const body = await errorBody(res);
    return new HttpError(res.status, stringField(body, 'error'), stringField(body, 'message'));
  }
  ```
- `HttpError` (`json.ts:3-21`) has only `status`, `reason` and `detail`.
- `governanceApi.ts:146-163` throws `await httpErrorFrom(res)` from `postJSON` and `patchJSON`.

So a 422 `{"error":"verify_failed","step":"imap","reason":"auth_failed"}` becomes `HttpError(422, "verify_failed", "")`. `pimVerifyFailure` can only return null, or a guess.

**Fix to the plan:** W1 modifies `web/src/api/json.ts`, choosing one of these:
- Add a `readonly body: unknown` (or `fields: Record<string, unknown>`) to `HttpError`, filled by `httpErrorFrom`. `api/json.ts` is already in the Stryker `mutate` list (`web/stryker.config.json:12`), so its tests move too.
- Or let `verifyPimAccount`, `patchPimProviderConfig` and the create path in `AddMailAccount` parse the 422 body themselves.

Then add a W1 test that pins the round trip through the real `httpErrorFrom`, not through a hand-built `HttpError`.

### C2. W3: a member cannot open Settings, so «Posta e calendario» is unreachable for the people it is for

**Plan:**
- W3 is titled "«Posta e calendario» in Settings, for every identity".
- It adds the section `not adminOnly` and renders it on "the non-admin page".
- Its test is `TheSectionIsListedForANonAdmin`.
- Spec goal 4: "mailbox connection moves to Settings, for every identity".

**Evidence:**
- Settings is an admin-only surface in the shell (`web/src/shell/modes.ts:31`):
  ```ts
  export const ADMIN_MODES = ['governance', 'settings'] as const satisfies readonly SurfaceIntent[];
  ```
- `visibleModes` hides those surfaces from a non-admin (`modes.ts:41`).
- `AppShell.tsx:78-80` sends a non-admin who lands on them back to chat:
  ```ts
  if (!capabilitiesLoading && !isAdmin && isAdminMode(surface)) {
    setSurface('chat');
  }
  ```
- `isAdmin` is `hasCapability(capabilities, IDENTITY_CREATE)` (`admin/useAdmin.ts:48`). A member, who holds `governance.write` but not `identity.create`, is therefore a non-admin here.
- `AppShell.tsx:165-166` says it outright about the `?settings=` deep link: "a non-admin who follows such a link is still bounced by the admin-mode guard".
- `ProfilePanel` is imported only by `SettingsWorkspace.tsx:8`.

So the non-admin branch of `SettingsWorkspace` (`:43-71`) cannot be reached from the shell today. `TheSectionIsListedForANonAdmin` would pass on the component while no member can open it. The same gate also hides Governance, so today members cannot reach `CalendarConnect` either. The plan moves the section, but it stays out of reach.

**Fix to the plan:**
- W3 adds a step that opens the Settings surface to members:
  - remove `'settings'` from `ADMIN_MODES`, and update its comment and the mode tests;
  - or add a member entry that opens Settings.
- Keep the admin-only panes gated through `settingsSections.adminOnly`, which `visibleSections` already applies (`settingsSections.ts:90-92`).
- Add a shell-level test: a member opens Settings, and sees «Posta e calendario» and nothing admin-only.
- This changes what members can see in the shell, so the plan should record it as an operator decision.
- The Governance link to the section (W3) should be `<a href="/?settings=mail">`, the existing deep-link idiom:
  - `AppShell.tsx:162-175` handles it, and `settings/remoteAccess/PhoneStep.tsx:68` already uses it as a link.
  - It fires once per AppShell mount, so a full-navigation `href` always works. The plan's direct `storeSettingsSection` call has no `setSurface` to pair with in `CalendarConnect`.

### C3. A2/P3: the PATCH relay lets a member replace the admin-set OAuth client

**Plan:**
- P3: `PATCH /admin/accounts/{id}/provider-config` "merges the non-empty values over the stored ones, validates with `TenantProviderConfig.Validate`, verifies (`imap`)…". It applies to any provider.
- A2 relays it with no rewrite.

**Evidence:**
- `internal/agui/connect_pim_inject.go:14-19`:
  > "For a managed provider it replaces the browser's credentials with the admin-set app, so a member cannot choose the OAuth client. … Aura mounts no account-update proxy. Any future one must run this same rewrite, because the sidecar's PUT replaces providerConfig wholesale."
- `internal/pimprovider/rules.go:74` defines `IsOwnedKey`.
- `internal/identity/capabilities.go:46`: every identity holds `governance.write`:
  ```go
  var userSet = []string{CapAgentRun, CapGovernanceRead, CapGovernanceWrite, CapSharePublic}
  ```

A PATCH `{"providerConfig":{"clientId":"…","clientSecret":"…"}}` on a google or microsoft account would be merged and stored.

**Fix to the plan:**
- P3: the PATCH answers `400 {"error":"not_patchable"}` for any provider other than `imap`, mirroring verify's `not_verifiable`. The spec only needs IMAP repair.
- A2 also refuses, or strips, `pimprovider.IsOwnedKey` keys in the relay. Add tests on both sides: `Patch_OnAGoogleAccount_Is400`, and `TestPatchCannotSetTheOAuthClient`.

### C4. A1: `pimWatchTasks.store` is an interface, and its test stub is a missed call site (does not build)

**Plan:** A1 modifies only `cmd/aura/serve_pimwatch.go` (`pimWatchTasks.RunNow` → `t.store.RunTaskNow`).

**Evidence:**
- `cmd/aura/serve_pimwatch.go:262-274`:
  ```go
  // pimTaskStore is the part of *cron.Store the watch's tasks use.
  type pimTaskStore interface {
  	ListLiveTasksOfKind(...)
  	CreateTask(...)
  	UpdateTask(...)
  	ResumeTask(ctx context.Context, id string, now time.Time) (time.Time, error)
  	CancelTask(ctx context.Context, id string) error
  }
  type pimWatchTasks struct {
  	store pimTaskStore
  }
  ```
  `t.store.RunTaskNow` does not compile until the interface gains `RunTaskNow(ctx context.Context, id string) error`.
- `cmd/aura/serve_pimwatch_test.go:213` (`stubTaskStore`) then stops implementing `pimTaskStore`.

**Fix to the plan:**
- Modify: `pimTaskStore` gains `RunTaskNow`.
- `cmd/aura/serve_pimwatch_test.go`: `stubTaskStore` gains `RunTaskNow`.
- Add a test that `pimWatchTasks.RunNow` reaches the store.

### C5. P4: `IMemoryCache` has no package and no registration in `CalendarMcp.Core` (does not build)

**Plan:** P4 step 4 says "Cache: `IMemoryCache`, 24 h". The only package P4 adds to `CalendarMcp.Core.csproj` is `DnsClient`.

**Evidence:**
- `src/CalendarMcp.Core/CalendarMcp.Core.csproj` lists no `Microsoft.Extensions.Caching.*` package.
- Grepping `Caching.Memory` in `src/CalendarMcp.Core/obj/project.assets.json` returns nothing, so there is no transitive copy either.
- Grepping `AddMemoryCache` / `IMemoryCache` over `src` returns nothing. Core is `Microsoft.NET.Sdk`, not the Web SDK, so it does not get the ASP.NET shared framework.

**Fix to the plan:**
- P4 Modify gains `<PackageReference Include="Microsoft.Extensions.Caching.Memory" Version="10.0.13" />` (the version the other `Microsoft.Extensions.*` references pin) and `services.AddMemoryCache()` in `ServiceCollectionExtensions.cs`.
- The DI registrations of `MailDiscovery`, `IMxLookup` and `IIspdbClient` are named too. Today only the typed `HttpClient` is named.

---

## HIGH

### H1. P3: the named harness cannot host these tests

**Plan:** P3 step 1 builds the tests on "`WebApplication.CreateSlimBuilder()` + `UseTestServer()`, the harness of `src/CalendarMcp.Tests/Changes/ChangesPipeline.cs`".

**Evidence:**
- `ChangesPipeline.cs:50-66` registers a static account list and maps only the changes routes, and it exposes only `PostAsync` (`:72`):
  ```csharp
  new TestOptionsMonitor<CalendarMcpConfiguration>(new CalendarMcpConfiguration { Accounts = [.. accounts] }));
  …
  _app.MapGroup("/admin").MapChangesEndpoints();
  ```
  It has no `IAccountConfigurationService`, no file-backed config, no PATCH and no PUT.
- `AdminEndpoints.cs:149` `CreateAccount` is `private static`. It is reachable only through `MapAdminEndpoints(this WebApplication app)` (`:17`).
  - That method also maps handlers that need `DeviceCodeAuthManager`, `GoogleOAuthManager`, `IM365AuthenticationService` and `IAccountRegistry`.
- The working precedent for mapping it is `src/CalendarMcp.Tests/Changes/ChangesRegistrationTests.cs:44-57`:
  ```csharp
  builder.Services.AddCalendarMcpCore();
  builder.Services.AddSingleton<IAccountConfigurationService, AccountConfigurationService>();
  builder.Services.AddSingleton<DeviceCodeAuthManager>();
  builder.Services.AddSingleton<GoogleOAuthManager>();
  …
  app.MapAdminEndpoints();
  ```
- `AccountConfigurationService` reads the real file named by `CALENDAR_MCP_CONFIG`. That is process-global, which is why `AccountConfigurationServiceTests` is `[DoNotParallelize]` (`AccountConfigurationServiceTests.cs:13`, `:27-29`).

**Fix to the plan:** P3 creates its own harness file, for example `src/CalendarMcp.Tests/Admin/AdminPipeline.cs`, built from four pieces:
1. The `ChangesRegistrationTests` DI: `AddCalendarMcpCore()`, then the fake `IImapAccountVerifier` and a Rocks `IImapProviderService` registered **after** it, so they win.
2. `ChangesPipeline`'s `SubjectFromToken` scheme plus `AdminAuthMiddleware`, which binds the tenant.
3. A temp `CALENDAR_MCP_CONFIG` directory with a seeded `appsettings.json`.
4. `[DoNotParallelize]`, plus GET, PATCH and PUT helpers.

`SubjectFromToken` is a private nested class of `ChangesPipeline`, so it must be extracted to share it, not copied.

`CapturingLogs` (`ChangesPipeline.cs:142`) is `internal` and reusable as is.

---

## MEDIUM

### M1. A2: the route-gating test the plan points at does not exist; the real sweep cannot catch a missing mount

**Plan:** A2 says to add the new routes to "the route-gating test that already lists `connectPIM*Route` (grep `connectPIMAccountDeleteRoute` in `cmd/aura/*_test.go`)".

**Evidence:**
- No file under `cmd/aura/*_test.go` mentions `connectPIMAccountDeleteRoute`. The only matches for `api/connect/pim` there are `oauth_callback_public_route_test.go:23` and `pim_provider_routes_test.go:45,54`.
- The list of gated connect routes is `connectGatedRoutes()` in `internal/agui/governance_write_auth_sweep_test.go:56-83`.
  - Its 403 test wraps the agui mux itself: `mount := RequireCapability(s.Mux(), deps, "governance.write")` (`:140`, again at `:177`).
  - So it cannot detect a route that `cmd/aura/serve_webui.go:254-265` forgot to mount.
- `connect_pim_api.go:81-82` warns: "each route MUST be gated there — an unmounted route would be ungated".

**Fix to the plan:** add the three routes to `connectGatedRoutes()`. Then add a `cmd/aura` test on the production handler that sends each of the three routes and checks it reaches the handler for a member.
- Pattern: `cmd/aura/pim_provider_routes_test.go:31-60`, which uses `newServeHandler` and `memberIdentities`.

### M2. A1: `fakeTasks` is behind `db_integration`, so the "unit" Repaired tests cannot use it

**Plan:** A1's tests are in `internal/pimwatch/service_repair_test.go` ("unit, a fake `Tasks`"), and "the `fakeTasks` in `service_integration_test.go` gains `RunNow`". Step 2 runs `go test -race ./internal/pimwatch/ -run Repaired` with no tags.

**Evidence:** `internal/pimwatch/service_integration_test.go:1` is `//go:build db_integration`, and `fakeTasks` lives there (`:64` and on).

**Fix to the plan:** move `fakeTasks` (with `fakeRoutes` if needed) to an untagged `internal/pimwatch/fakes_test.go`, and reuse it in `service_repair_test.go`. Do not write a second fake: CLAUDE.md's REUSABLE CODE rule forbids it.

### M3. P2/P3: provider-config keys are case-insensitive everywhere except in the plan's merge and `ProviderSecrets`

**Plan:**
- `ProviderSecrets.PasswordKey = "password"`, and `Protect` "copies every other key unchanged".
- The PATCH "merges the non-empty values over the stored ones".

**Evidence:**
- Validation folds case: `ValidateRequiredKeys` (`AccountValidation.cs:166-175`) compares with `kv.Key.Equals(key, StringComparison.OrdinalIgnoreCase)` (`:171`).
- The stored dictionary is case-insensitive: `AccountConfigurationService.cs:393`:
  ```csharp
  new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase)
  ```
- The request dictionary is not: `AccountDtos.cs:23`, `public Dictionary<string, string> ProviderConfig { get; init; } = [];`.
- Aura already met the same problem (`internal/pimprovider/rules.go:72-73`):
  > "the sidecar folds providerConfig keys; a leftover "ClientId" beside the injected "clientId" makes its dictionary copy throw."

So a body carrying `"Password"` would pass validation, be read by `ImapProviderService` (case-insensitive `TryGetValue`), and escape `Protect`. A body carrying both `password` and `Password` would make a case-insensitive copy throw, which is a 500.

**Fix to the plan:**
- `ProviderSecrets.Protect` matches `password` with `OrdinalIgnoreCase` and returns an `OrdinalIgnoreCase` dictionary.
- The PATCH answers 400 on duplicate keys that differ only in case.
- P2 adds `Protect_EncryptsAPasswordWhateverItsCase`.

### M4. P2: the startup pass must rewrite the password value in place, not re-serialize accounts

**Plan:** `ProtectStoredPasswordsAsync` is "tenant-less, under `_fileLock`, every account in the file … writes the file only when that is above 0."

**Evidence:** the existing write path rebuilds a whole node from an `AccountInfo` (`AccountConfigurationService.cs:255-268`, `AccountInfoToNode`). That keeps only nine properties, writes a full `Permissions` block, and drops any other key an operator put on the node.

**Fix to the plan:** state that the pass walks `ReadConfigAsync`'s `JsonArray` and replaces only `ProviderConfig.password` (or `providerConfig`, in either case) on the nodes that need it.
- Do not go through `ParseAccounts`. It throws `InvalidDataException` on any account without `TenantId` (`:331-332`), which would turn a legacy entry into a skipped pass.
- `ProtectStored_RewritesOnlyPlainPasswords` should also assert that the other nodes are byte-identical.

### M5. Rollout: the PIM goes live on every host before Aura's half exists, and the SHA check has nothing to read

**Plan:**
- P4 step 7: merge to `main`, and `aura-publish-image.yml` publishes `:latest` and `:<sha>`.
- L1 step 1: "confirm `docker inspect aura-pim-mcp` shows the P4 merge SHA's image".

**Evidence (item 6 of the brief):** the moving tag is confirmed. There is no digest pin and no payload pin for the PIM.
- `compose.yaml:1358-1359`:
  ```yaml
  image: ${AURA_PIM_MCP_IMAGE:-ghcr.io/chetto1983/aura-pim-mcp:latest}
  pull_policy: ${AURA_PIM_MCP_PULL_POLICY:-always}
  ```
- The updater header documents `AURA_PIM_MCP_IMAGE=ghcr.io/chetto1983/aura-pim-mcp:sidecar` (`deploy/aura-image-update.sh:23`). The fork workflow moves both tags (`aura-publish-image.yml:53-56`: `:latest`, `:sidecar`, `:${{ github.sha }}`), so either tag gets the new image.
- The PIM is pulled on every tick, and a sidecar behind its tag counts as a pending update on its own, independent of the aura image:
  - `deploy/aura-image-update.sh:51`: `SIDECARS=(arcadedb-mcp aura-pim-mcp whatsapp …)`
  - `deploy/aura-image-update.sh:577-579`: `pull_sidecar` on every tick.
  - `deploy/aura-update-consent.sh:145-147`: `! service_behind "${svc}" || return 0`.
  - `deploy/aura-image-update.sh:526-528`: `update_sidecar` at apply.

So the plan's "the update pulls the new image" is **confirmed**. The consequence the plan does not mention follows from it: once P4 merges, every host on the tag can apply the new PIM, at night or on an admin's click, while it still runs the old Aura.
- The old relay uses `pimClient` with an 8 s timeout (`connect_pim_api.go:43`).
- The new `CreateAccount` verifies for up to 20 s.
- So a slow or unreachable IMAP host gives the old cockpit a 502, and a wrong password reads "HTTP 422: verify_failed".

For L1:
- The workflow sets no `org.opencontainers.image.revision` label: `aura-publish-image.yml` has no metadata/labels step, and the fork `Dockerfile` has no `LABEL`.
- So `docker inspect` cannot show a SHA.

**Fix to the plan:**
- Either hold the P4 merge until A2 is merged and its image published, or record the compatibility window and accept it.
- In L1, compare `docker inspect --format '{{.Image}}' aura-pim-mcp` with the image ID of `ghcr.io/chetto1983/aura-pim-mcp:<P4 merge sha>` pulled on the VM. Do not look for a SHA in `inspect`.

### M6. P1 / Decision 2: what the CI servers can and cannot produce

The brief asks four questions about the CI servers. All four are answered from source.

1. **Can Dovecot reject a wrong password? Yes.**
   - CI runs it with one password for every user: `-e USER_PASSWORD='{PLAIN}secret'` (`.github/workflows/ci.yml`).
   - The existing tests log in as fresh GUID users with `secret` (`ImapMailbox.cs:75-89`).
   - A different password therefore fails, and MailKit raises `AuthenticationException`.
2. **Does Dovecot allow plaintext auth without TLS? Yes.** `TestData/Dovecot/test.conf` has `auth_allow_cleartext = yes` and `ssl = no`.
3. **Does GreenMail standalone 2.1.15 expose SMTP on 3025? Yes.** `greenmail-docker/standalone/Dockerfile` has `EXPOSE 3025 …` and `-Dgreenmail.setup.test.all`. CI publishes only `-p 3143:3143` today, so the plan's `-p 3025:3025` is needed.
4. **Does GreenMail require or advertise AUTH? It advertises it but never refuses it.**
   - The image runs with `-Dgreenmail.auth.disabled` (same Dockerfile, `GREENMAIL_OPTS`).
   - EHLO always advertises AUTH and never STARTTLS (`HeloCommand.java`): `conn.send("250-AUTH " + AuthCommand.SUPPORTED_AUTH_MECHANISM);`. So `SecureSocketOptions.Auto` on 3025 stays plain.
   - With auth disabled, `UserManager.test` returns `true` for any credentials (`UserManager.java:137-145`).

Consequences:
- `Verify_RightPassword_Passes` (Dovecot IMAP + GreenMail SMTP) works.
- `Verify_SmtpDown_IsSmtpUnreachable` and `Verify_ClosedPort_IsUnreachable` work: a refused connect raises `SocketException`.
- `smtp_auth_failed` **cannot** be produced by the CI GreenMail. Producing it needs a second GreenMail started without `-Dgreenmail.auth.disabled` and with a configured user.
- Whether either server can produce `tls_failed` was not established (see "Not verified").
- `mailbox_refused` could get an integration test through the existing `RefusingImapProxy` (`src/CalendarMcp.Tests/Changes/RefusingImapProxy.cs:8-12`, "Neither GreenMail nor Dovecot can be made to answer a temporary NO on demand"). Today it refuses an EXAMINE of the Sent folder, and the verifier EXAMINEs INBOX.

**Fix to the plan:**
- Record these answers in Decision 2, as it asks.
- Add `3025` to the CI readiness loop: `for port in 3143 31143; do` → `for port in 3025 3143 31143; do`.

### M7. W2: the i18n registration points at the wrong file

**Plan:** W2 creates `resources.settings.mail.ts` "registered where `resources.governance.mailWatch.ts` is", with keys `settings.mail.*`.

**Evidence:**
- `resources.governance.mailWatch.ts` is registered inside the `governance` object (`resources.governance.ts:8`, `:277`, `:552`: `mailWatch: mailWatchEn`).
- The `settings.*` tree is `settingsEn = { settings: { … } }` in `resources.settings.ts:3` and `:252`, spread at the root in `resources.ts:207`, `:469`.
- Registering `{ settings: { mail } }` anywhere else would shadow the whole `settings` subtree through the shallow spread.

**Fix to the plan:**
- W2 Modify gains `web/src/i18n/resources.settings.ts`, adding `mail: mailEn` inside `settingsEn.settings` and `mail: mailIt` inside `settingsIt.settings`.
- The file is 584 LOC, so it stays under 600 after four lines.

### M8. W4: the stale sentence doubles its preposition, and the decide clock needs a template change too

**Plan:**
- The stale wording is `«Non riesco a leggere la posta da {{since}}…»`, with `since` from FailingLine's formatter.
- The decision-model time "gets its day, with the formatter `FailingLine` already uses".

**Evidence:**
- FailingLine formats with `failingSince` (`AccountWatch.tsx:209`). That returns a sentence fragment, not a date (`mailWatchLogic.ts:275-281`). The IT copy is `sinceTime: 'dalle {{time}}'` and `sinceDay: 'dal {{date}}'` (`resources.governance.mailWatch.ts:256-257`).
  - The result would read «… da dal 14 nov». The spec also asks for «giorno e ora», and `failingSince` drops the time on another day.
- The day-and-time formatter already exists: `dayAndTime` (`mailWatchLogic.ts:315`).
- `decide.failing` is `'Il modello non risponde dalle {{time}} ({{reason}})'` (`resources.governance.mailWatchDecide.ts:44`). "dalle" fits a clock time, not a day plus time.

**Fix to the plan:**
- `governance.mailWatch.failure.stale` takes `{{since}}` from `dayAndTime`.
- `decide.failing` (both locales) moves to a wording that fits `dayAndTime`.
- W4 Modify gains `resources.governance.mailWatchDecide.ts`.

---

## LOW

1. **P1: the nested type needs qualifying.** `ImapAccountConfig` is nested (`ImapProviderService.Support.cs:199`, inside `public partial class ImapProviderService`). In the new `ImapConnections` / `ImapAccountVerifier` files it is `ImapProviderService.ImapAccountConfig`, or reached through `using static CalendarMcp.Core.Providers.ImapProviderService;`. The plan's signatures are written unqualified. Either qualify them, or move the record to top level in `ImapConnections.cs`.
2. **P1 test constructors.** The compile-proven forms are the ones in `ProviderFailuresTests.cs:40`, `:72-73`:
   ```csharp
   new AuthenticationException("bad password")
   new ImapCommandException(ImapCommandResponse.No, "EXAMINE failed")
   new SslHandshakeException("handshake failed", new IOException("reset"))
   ```
   The plan's parameterless `AuthenticationException()` / `SslHandshakeException()` and its three-argument `ImapCommandException(..., "SELECT", "x")` were not compiled here. Copy the proven forms.
3. **P1: the 20 s expiry throws a different type.** The linked `CancellationTokenSource` throws `OperationCanceledException`, not the `TimeoutException` the mapping test pins. Add `OperationCanceledException` (when the verifier's own token fired) to `Reasons_MapEachFailure`.
4. **P3: reload before evicting.** The order "`ForgetConnectionAsync`, then `AccountConfigurationReload.Apply`" leaves a window. A watch tick between the two reopens with the old config, because `ResolveConfigAsync` reads the in-memory registry (`ImapProviderService.cs:115`). Reload first, then evict.
5. **P3: `DescribePermissions` is private.** It is `private static` (`AdminEndpoints.cs:77`), and the PATCH answers "the same shape as `UpdateAccount`" from a new file, so it must become `internal`, like `LinkedAsync` (`:126`).
6. **P2: no step records the key-ring location.** The key ring is on the volume, as the spec requires:
   - `DataProtectionExtensions.cs:21-27` puts the keys at `ConfigurationPaths.GetDataProtectionKeysDirectory()`, which is `<data dir>/keys` (`ConfigurationPaths.cs:97-99`).
   - The fork `Dockerfile:64` sets `ENV CALENDAR_MCP_CONFIG=/app/data`.
   - `compose.yaml` mounts `aura-pim-data:/app/data`.

   Record this in `AURA-FORK.md`.
7. **W2: the disclosure holds five fields, not six.** «Impostazioni server» holds "today's six IMAP fields", but the sixth is `password` (`pimProviders.ts:118`), which is already the main field. The disclosure holds five. The component is `ProviderConfigFields` (`CalendarConnectFields.tsx:50`). `CalendarConnectFields` is the module.
8. **W2/W3: `AddAccountForm` moves twice and has no destination.** W2 exports it from `CalendarConnect.tsx` (today `function AddAccountForm` at `:165`, not exported). W3 then moves it out without naming a destination file. Name the file once, in W2.
9. **W1: two id helpers would coexist.** `accountIdFromAddress` sits beside `normalizePimAccountId`, `pimAccountIdError` and the module-private `PIM_ACCOUNT_ID_RE` (`pimProviders.ts:211-229`). Check the generated id with the exported `pimAccountIdError(...) === null`. Do not restate the slug rule.
10. **W4: `staleTransient` needs the provider threaded through.** `AccountWatch` receives `account: PimAccount`, which carries `provider` (`AccountWatch.tsx:27`). `WatchState` and `FailingLine` (`:131`, `:199`) do not, so the plan must say the prop is threaded through.
11. **A2: `TestCreateWaitsForASlowVerification` sleeps 10 s of wall clock.** It runs in the package that `pim_account_relay` will mutate. Make the sidecar delay relative to a test-shortened `pimClient` timeout (both are package vars), or assert on `pimVerifyClient.Timeout` directly.
12. **G1: frontend mutation is not covered.**
    - G1 confirmed:
      - `GO_SCOPES` and `REQUIRED_SCOPE_IDS` are in `scripts/critical_mutation_gate.py:18-39`, `:59`.
      - The three groups are in `ci.yml:1780-1790`: `turn-reading`, `elicitation-boundaries`, `media`.
      - The cap is `scripts/mutation_workflow_test.py:119`: `assertLessEqual(len(groups), 3, …)`.
      - The ×2.7 convention is at `ci.yml:1762`.
      - `agui` and `pimwatch` are `"mode": "target"` in `coverage_package_policy.json:15`, `:83`, so no baseline pin moves.
    - G1 does not add `src/settings/mail/mailConnectLogic.ts` to `web/stryker.config.json` `mutate`, where `mailWatchLogic.ts` already is (`:83`). It is pure logic and belongs there.
13. **W3: one existing test is missing from the list.** W3 names "the existing `CalendarConnect` and `SettingsWorkspace` tests". `governance/__tests__/CalendarConnectRow.test.tsx` also renders `<CalendarConnect />` (`:42`, `:51`) and tests the account row (`describe('CalendarConnect account row', …)`, `:73`). Once W3 moves the list, that test must move to `MailAccountsSection` too.

---

## Claims confirmed

| Plan claim | Evidence |
|---|---|
| LOC 598 / 460 / 452 / 367 / 489 / 410 / 422 / 584 / 554 / 337 / 263 / 331 | `wc -l` on both trees, all exact |
| Nested `internal sealed record ImapAccountConfig` at `Support.cs:199` | `ImapProviderService.Support.cs:199` |
| Openers have no callers outside `ImapProviderService.cs` | `OpenImapAsync` used at `:261`, `OpenSmtpAsync` at `:512`; nothing in `.Changes`/`.Folders`/`.Support`; the CLI opens no IMAP/SMTP client |
| `ResolveConfigAsync` holds the defaults and `Unprotect` | `ImapProviderService.cs:113-147` |
| Singletons at `Program.cs:107`, `ServiceCollectionExtensions.cs:24,36` | exact |
| `InternalsVisibleTo` Tests only; `ImapAccountConfig` internal to Core | `Core/Properties/AssemblyInfo.cs:3`, `HttpServer.csproj:30`. The public `IImapAccountVerifier` taking a dictionary is therefore the right seam |
| `IImapProviderService` fakes | Rocks `[assembly: Rock(typeof(IImapProviderService), BuildType.Create)]` (`RockSetup.cs:12`) regenerates; there is no hand-written fake |
| `IAccountConfigurationService` implementers | only `AccountConfigurationService`; no fake (`ChangesRegistrationTests.cs:51` uses the real one) |
| `AccountExistsAsync` tenant-scoped; ids tenant-prefixed | `AccountConfigurationService.cs:162-175`; `TenantContext.cs:77-81` (`{tenant}__{local}`) |
| `GetAccountFromConfigAsync` tenant-scoped, so a foreign id is 404 | `AccountConfigurationService.cs:34-55` |
| `SlugRegex` `^[a-z0-9][a-z0-9\-_]*$` | `AccountValidation.cs:213-214` (private; mirrored by `pimProviders.ts:214`) |
| `PasswordProtector` passes plaintext through; `IsProtected` | `PasswordProtector.cs:49-66` |
| `CryptographicException` → `provider_auth` | `ProviderFailures.cs:42` |
| `MapChangesEndpoints(this RouteGroupBuilder)` pattern for `MapAccountRepairEndpoints` | `ChangesEndpoints.cs:17` |
| `AddHostedService` pattern | `Program.cs:113` |
| `CapturingLogs` reusable | `ChangesPipeline.cs:142`, `internal` |
| `ImapServers` holds the server variables | `ImapMailbox.cs:20-43` |
| Mail-watch status carries `last_error` / `failing_since` per resource | Go `pimwatch/service.go:292-297`, wire `agui/pim_watch_api.go:117-123`, TS `mailWatchApi.ts:26-33` |
| `GET /admin/accounts` exposes `provider` | `AdminEndpoints.cs:60`; parsed at `pimApi.ts:100` |
| `provider_transient` is a wire code | `pimwatch/client.go:271`; i18n `resources.governance.mailWatch.ts:49`, `:279` |
| `findWatch`, `Tasks.Resume`, `WatchTask.PausedReason` | `service.go:185`, `:69`, `:40` |
| `cron.PausedByFailures` / `PausedByOperator` | `cron/pause.go:23-24` |
| `Store.RunTaskNow` requires an active task | `cron/store_manage.go:131`; SQL `WHERE id = $1 AND status = 'active'` (`scheduler_tasks.sql:164-166`), so Resume-then-RunNow is the right order |
| `mailWatchService`; its only fake | `pim_watch_api.go:22-38`; `fakeWatchService` (`pim_watch_api_test.go:56`, `pim_watch_decide_api_test.go:12`) |
| `forwardPIM(…, client)`, `statusCapture`, `sanitizeErr`, `context.WithoutCancel` precedent | `connect_pim_api.go:307`, `:163`, `:151-153` |
| `pimClientTimeout` 8 s | `connect_pim_api.go:43` |
| Route constants and mounts | `serve_webui_routes.go:292-303`, `serve_webui.go:254-265` |
| `governance.write` for every identity | `identity/capabilities.go:46` |
| `PIM_PROVIDER_NOT_CONFIGURED` | `pimApi.ts:21`; produced at `connect_pim_inject.go:37`; consumed at `CalendarConnect.tsx:197` |
| `patchJSON` exists | `governanceApi.ts:171` |
| `storeSettingsSection`, `SettingsSectionId`, `group: personal` | `useSettingsSection.ts:24`; `settingsSections.ts:8-18` |
| Other `SettingsSectionId` switches | only `SettingsPane` (`SettingsWorkspace.tsx:94-118`); `SettingsRail.tsx` has no map |
| `CalendarConnectFields`, `PimConnectPanel`, `startConnect`, `PimAccountRow`, `MailWatchOverview` | all exist (`CalendarConnectFields.tsx`, `PimConnectPanel.tsx:10`, `pimConnect.ts:19`, …) |
| Decide clock at `AccountWatch.tsx:255-257` | exact, `clockTime(watch.decide_failed_at, …)` |
| The PIM CI runs GreenMail 2.1.15 and Dovecot 2.4.5 | fork `.github/workflows/ci.yml` |
| Decision 3: every reader of a stored `password` already unprotects | `ImapProviderService.cs:136`, `DavProviderService.cs:65`, `Cli/Commands/TestAccountCommand.cs:156`; no other non-test reader in `src` |
| Names agree across tasks | yes, with one exception. P3's Consumes line names `ImapAccountVerifier`, while P1 produces both `IImapAccountVerifier` and `ImapAccountVerifier`. The endpoints should consume the interface, as P3 step 3 says. Otherwise `VerifyFailure`, `ProviderSecrets.Protect`, `Repaired`, `RunNow`, `discoverPim` / `verifyPimAccount` / `patchPimProviderConfig` / `pimVerifyFailure` / `verifyMessageKey` are named the same wherever they appear |
| L1 assumption: the update pulls the new PIM image | confirmed; see M5 for what it does not cover |

## Not verified

- The lab VM's `.env` (`:latest` versus `:sidecar`, and whether its image home is ghcr or the GitLab bridge). Both tags move on a `main` push, so either works; the GitLab bridge would need `scripts/publish_images.sh`.
- Whether Dovecot or GreenMail can produce `SslHandshakeException` in CI (`tls_failed`). No source was read for it.
- MailKit 4.18.2's parameterless and three-argument exception constructors. These were not compiled (LOW 2).
- Whether Outlook.com still accepts an app password over IMAP. P4 step 1's rule ("copy the rule from the provider's page, drop what you cannot find") governs it. W2's "Usa una password per app" fallback for `outlook.com` exists only if that page still offers one.
- The run time the mutation gate adds per mutant for the `agui` scope (LOW 11).
