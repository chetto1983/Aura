# PIM provider apps: admin sets the OAuth client once, users only connect

Agreed with the operator on 2026-09-23. Today every person who adds a Google or Microsoft account
in the cockpit calendar wizard has to type the OAuth client ID and secret themselves. After this
change the admin sets them once per provider, and everyone else only sees the connect step.

Revised the same day after an independent audit. The audit found a provider-casing bypass,
case-variant keys, the default grants, the idempotency inventory, the source of the relay URI and
a silent client-ID change; the fixes are folded in below.

## Decisions

Stated by the operator:

- The client ID and secret are set **per provider**, and an admin can change them.
- They live in a **dedicated Postgres table**.
- Only the provider apps move there. Everything else stays as it is: the sidecar, its
  per-account config and token files, the relay, the device-code flow and the account list.

Assumed during design and accepted with it ("procedi"):

- **Managed providers.** The managed providers are the three OAuth ones:
  - `google`: client ID + secret;
  - `microsoft365` and `outlook.com`: tenant ID + client ID, no secret, because device code is a
    public-client flow.

  They are two Microsoft rows because the sidecar treats them as two providers, even when both
  point at the same Azure app registration.
- **Unmanaged providers.** `imap` and `ics` keep their per-account fields: those credentials
  belong to the user. `json` keeps its fields too. Its OneDrive source reuses a linked Microsoft
  account through `authAccountId` and is not changed here. The wizard never offered its own
  `clientId`/`tenantId` fields.
- **Who is admin.** "Admin" means holding `identity.create`, the same rule the cockpit's
  `useCapabilities().isAdmin` and the OpenRouter reconcile route use. `governance.write` cannot
  be the gate: every identity holds it from provisioning (`internal/identity/capabilities.go`,
  `userSet`).
- **One app per provider per install.** There is no per-account override and no fallback to the
  old per-account fields: a managed provider that is not configured cannot be connected.

## Why Aura injects the credentials instead of the sidecar reading them

The sidecar reads `clientId`/`clientSecret` from the account's own `providerConfig` on every
credential fetch, refresh included (`GoogleProviderService.cs`, `M365ProviderService.cs`). If Aura
fills those keys when it forwards the account create, the sidecar's storage and code stay
unchanged. The alternative, an install-wide provider section the sidecar reads itself, would
change the sidecar, which is out of scope by the operator's decision.

Accepted consequence: the sidecar writes `providerConfig` verbatim to its `appsettings.json`, so
the one install-wide Google secret ends up copied into every member's account entry. The
mechanism is unchanged; what changes is that it is now one secret in N copies.

## Data

New migration, numbered at landing from `ls internal/db/migrations/ | tail -1` (0131 on
2026-09-23): table `aura.pim_provider_app`.

| Column | Type | Notes |
|---|---|---|
| `provider` | `text PRIMARY KEY` | `CHECK (provider IN ('google','microsoft365','outlook.com'))` |
| `client_id` | `text NOT NULL` | `CHECK (client_id <> '')` |
| `tenant_id` | `text NOT NULL DEFAULT ''` | Microsoft only |
| `client_secret_ciphertext` | `bytea` | Google only |
| `updated_at` | `timestamptz NOT NULL DEFAULT now()` | |
| `updated_by` | `text NOT NULL DEFAULT ''` | the principal, as settings and remote access stamp it |

Row-shape checks, in the style of 0130:

- `CHECK ((provider = 'google') = (client_secret_ciphertext IS NOT NULL))`
- `CHECK ((provider = 'google') = (tenant_id = ''))`

Other decisions:

- **Encryption.** The secret is sealed with `internal/secret.Sealer`, HKDF info
  `aura-pim-provider-app-key-v1`, derived from `AURA_AUTHULA_SECRET`, as `internal/mcpregistry`
  does. The label is unique among the seven existing ones.
- **No row-level security.** This is install-wide data, like `aura.settings`, `aura.mcp_server`
  and `aura.cloudflare_remote_access`.
- **Grants.** No GRANT statement: 0001's default privileges apply, as for 0130.
- **Audit.** No audit ledger row, only `updated_by`, the same as settings and remote-access
  writes. The change does not appear in the admin audit feed.
- **Down migration.** It drops the table.
- **Queries.** Through sqlc, in `internal/db/queries/pim_provider_app.sql`. "Keep the stored
  secret" is done in SQL with
  `COALESCE(EXCLUDED.client_secret_ciphertext, pim_provider_app.client_secret_ciphertext)`,
  not with a read-modify-write in Go.

## Package

`internal/pimprovider`: a `Store` with `List(ctx)`, `Get(ctx, provider)` and
`Upsert(ctx, App, updatedBy)`. `App` carries the plaintext `ClientSecret` in memory only.
`Get` returns `ErrNotConfigured` when there is no row.

The package also owns the provider rules, so the handler and the store agree:

- `Canonical(provider)`: exact byte match against the six ids the cockpit sends (`google`,
  `microsoft365`, `outlook.com`, `imap`, `ics`, `json`).
- `Managed(provider)`: true for the three managed ids.
- `OwnedKeys()`: `clientId`, `clientSecret`, `tenantId`.
- Per-provider validation.

It is added to `scripts/coverage_package_policy.json` with `mode: target` (≥85%).

## API

The handlers live in a new sibling, `internal/agui/connect_pim_providers_api.go`, because
`connect_pim_api.go` is already 310 LOC. The routes are mounted in `cmd/aura/serve_webui.go`. The
provider routes read and write Postgres only, so they work while the sidecar is not wired.

### `GET /api/connect/pim/providers`

Gated on `governance.write`, like every other PIM route.

- For a member it returns `[{provider, configured}]` for the three managed providers.
- For an admin it adds `clientId`, `tenantId`, `secretSet` and `redirectUri` (Google only).
- The secret is never returned.
- `redirectUri` is a Go constant, `PIMGoogleRelayRedirectURI`, equal to the sidecar's
  `GoogleOAuthRelayUrl` default. That value is fixed by design: the relay page's address never
  changes. The constant's comment names the sidecar file that must stay equal.

### `PUT /api/connect/pim/providers/{provider}`

Gated on `identity.create`. The body is `{clientId, tenantId?, clientSecret?}`.

- **Google** needs `clientId`. It also needs `clientSecret` when there is no stored secret, or
  when `clientId` differs from the stored one. An unchanged client ID with an empty secret keeps
  the stored secret, so resaving an unchanged app does not demand the secret again. A new client
  ID without its secret would produce consent that fails at the code exchange, which the admin
  never sees.
- **Microsoft** needs `clientId` and `tenantId`. A `clientSecret` is rejected (400).
- **Errors.** Unknown or non-canonical provider: 404. A missing field: 400.
- **Response.** The admin shape of `GET` for that provider.
- **Idempotency.** Registered in `internal/agui/idempotency_http.go` `httpMutationRoutes` as
  `httpMutationMeta("pim_provider_app_put")`. `TestEveryRegisteredUnsafeHTTPRouteIsClassified`
  fails without it. The cockpit sends `Idempotency-Key` through the shared fetch layer.
- **Store unavailable.** When the store is nil (a malformed `AURA_AUTHULA_SECRET`), both
  provider routes answer 503.

### `POST /api/connect/pim/accounts` (existing route)

Before forwarding, the handler:

1. Decodes the body into `map[string]json.RawMessage`, keeping unknown fields semantically
   intact.
2. Rejects with 400 unless `provider` is byte-exactly canonical. Without this, `"Google"` would
   skip injection: the sidecar matches provider names case-insensitively
   (`AccountValidation.cs`, `ProviderServiceFactory.cs`) and would create a working account on
   the member's own client.
3. For a managed provider:
   - loads the app;
   - deletes every `providerConfig` key equal, case-insensitively, to one of `OwnedKeys()`;
   - sets exactly the provider's keys: Google gets `clientId` and `clientSecret`, Microsoft
     gets `tenantId` and `clientId`, with no `clientSecret` key.

   Deleting case-insensitively matters: the sidecar folds the dictionary case-insensitively
   (`AccountInfo.cs`), so a leftover `ClientId` next to the injected `clientId` throws there and
   returns 500.
4. Answers `409 {"error":"provider_not_configured"}` when the managed provider has no row,
   without calling the sidecar; 503 when the store is nil.

The sidecar's own 409 means "account id already exists". The cockpit tells the two apart by
the `error` token and renders `provider_not_configured` through an i18n key.

**Invariant:** Aura mounts no account update route, and the sidecar's
`PUT /admin/accounts/{id}` is reachable only with the daemon-held bearer. Any future update
proxy must re-inject the same way.

## Cockpit

- `pimProviders.ts` splits each managed provider's fields into app fields, which the admin
  edits, and account fields. Google, Microsoft 365 and Outlook.com have no account fields left.
- **"Add account"** (`CalendarConnect.tsx`) shows account ID, display name and Advanced for a
  managed provider. When `GET …/providers` says it is not configured, the form shows "An
  administrator has to configure Google first" and disables submit.
- **`PimGoogleConnectPanel`** keeps only the consent button and the linked confirmation. The
  "register this redirect URI" block moves to the admin panel, because a member cannot register
  anything.
- **`PimProviderAppsPanel.tsx`** is rendered in the same calendar section only when
  `useCapabilities().isAdmin`. It holds one form per managed provider:
  - the current client ID pre-filled;
  - the secret field never pre-filled, with a "stored" hint when `secretSet`;
  - under Google, the `redirectUri` to register in Google Cloud Console;
  - a per-provider tenant hint: `consumers` for Outlook.com, the directory GUID or `common` for
    Microsoft 365.

  Saving invalidates `['connect','pim','providers']`.
- All copy under `governance.mcp.calendar.*`, in English and Italian.
- Refactor-on-touch: the comments that describe the old per-account model must be updated, in
  `connect_pim_api.go`, `pimApi.ts`, `pimProviders.ts` and the i18n `redirectHint`.
  `pimProviders.ts`'s claim that the sidecar's provider reads are case-sensitive is already
  false (`AccountInfo.cs` folds the keys) and must be corrected.

## What happens when the admin changes the credentials

- Accounts already linked keep the copy they were created with in the sidecar and are not
  touched. This holds for Google and Microsoft alike.
- New credentials apply to new connections only. That matches the protocol: a refresh token is
  bound to the client it was issued to (RFC 6749 §6), so an existing account could not move to
  another client anyway.
- Rotating the secret of the **same** Google client leaves earlier accounts on the old secret.
  They need reconnecting once the old secret is disabled in Google Cloud Console. This is from
  the RFC and Google's console behaviour and has **not been measured**.
- On an install that already exists, the table starts empty after the update: the admin enters
  the credentials once. Accounts already linked keep working, because the sidecar still has
  their copy.

## Testing

**Go unit (`internal/agui`, `internal/pimprovider`)**

- Account create:
  - injects the app credentials for each managed provider;
  - discards browser values in any casing (`clientId`, `ClientId`, `CLIENTSECRET`);
  - rejects `Google`, `GOOGLE` and ` google` with 400;
  - keeps unknown body fields;
  - answers 409 without calling the sidecar when the provider is not configured, and 503 when
    the store is nil;
  - never sets a `clientSecret` key for Microsoft.
- `PUT`:
  - per-provider validation;
  - a secret required when the client ID changes;
  - an empty secret keeps the stored one when the client ID is unchanged;
  - a secret refused for Microsoft;
  - 404 on a non-canonical provider.
- `GET`: the member shape carries only `configured`; the admin shape never carries the secret.
- The route table gates `PUT` on `identity.create`, so a member identity gets 403.
- The idempotency classification test covers the new route.

**Go `db_integration`**

- store round-trip;
- the ciphertext column never contains the plaintext;
- another HKDF info cannot open it;
- the `COALESCE` keep-secret upsert;
- the row-shape CHECKs reject a Google row without a secret and a Microsoft row with one;
- the migration applies up and down.

**Web (vitest)**

- the wizard shows no credential fields for a managed provider;
- the not-configured note and disabled submit;
- the `provider_not_configured` 409 is translated;
- the admin panel is hidden for a member and saves for an admin;
- the secret field is never pre-filled;
- the member panel shows no redirect URI.

**Other gates**

- Coverage: `internal/pimprovider` ≥85% in the policy file.
- Mutation testing (CI) on the injection and validation code.

**E2E on the VM (192.168.101.158)**, after the updater delivers the build:

1. The admin sets the Google app.
2. A member identity adds a Google account with only an account ID and a display name, and
   completes consent through the relay.
3. The panel shows "linked".
4. The admin changes the client ID, with its secret.
5. The already-linked account still lists calendars.

## Out of scope

- Deleting a provider app.
- Several apps per provider.
- Migrating existing accounts' credentials into the table.
- Any sidecar change.
- Propagating a rotated secret to linked accounts.
- An audit-ledger row for provider-app writes.

## PRD

Amend prd.md §13 once the E2E has run. It records the table, the admin gate, the injection
point, the canonical-provider rule and the rotation behaviour, and says what was not measured.
