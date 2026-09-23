# PIM provider apps: admin sets the OAuth client once, users only connect

Agreed with the operator on 2026-09-23. Today every person who adds a Google or Microsoft account
in the cockpit calendar wizard has to type the OAuth client ID and secret themselves. After this
change the admin sets them once per provider, and everyone else only sees the connect step.

## Decisions

Stated by the operator:

- The client ID and secret are set **per provider**, and an admin can change them.
- They live in a **dedicated Postgres table**.
- Only the provider apps move there. Everything else stays as it is: the sidecar, its
  per-account config and token files, the relay, the device-code flow and the account list.

Assumed during design and accepted with the design ("procedi"):

- The managed providers are the three OAuth ones: `google` (client ID + secret),
  `microsoft365` and `outlook.com` (tenant ID + client ID, no secret: device code is a public
  client flow). `imap`, `ics` and `json` keep their per-account fields, because there the
  credentials belong to the user, not to an app.
- "Admin" means holding `identity.create`, the same rule the cockpit's `useCapabilities().isAdmin`
  and the OpenRouter reconcile route already use. `governance.write` cannot be the gate: every
  identity holds it from provisioning (`internal/identity/capabilities.go`, `userSet`).
- One app per provider per install. There is no per-account override and no fallback to the old
  per-account fields: a managed provider that is not configured cannot be connected.

## Why Aura injects the credentials instead of the sidecar reading them

The sidecar reads `clientId`/`clientSecret` from the account's own `providerConfig` on every
token refresh (`GoogleProviderService.cs`), not only at link time. If Aura fills those keys when
it forwards the account create, the sidecar's storage and code stay unchanged. The alternative,
an install-wide provider section the sidecar reads itself, would change the sidecar, which is
out of scope by the operator's decision.

## Data

New migration, numbered at landing from `ls internal/db/migrations/ | tail -1` (0131 on
2026-09-23): table `aura.pim_provider_app`.

| Column | Type | Notes |
|---|---|---|
| `provider` | `text PRIMARY KEY` | `CHECK (provider IN ('google','microsoft365','outlook.com'))` |
| `client_id` | `text NOT NULL` | not secret: Google puts it in every consent URL |
| `tenant_id` | `text NOT NULL DEFAULT ''` | Microsoft only |
| `client_secret_ciphertext` | `bytea` | Google only; NULL for Microsoft |
| `updated_at` | `timestamptz NOT NULL DEFAULT now()` | |
| `updated_by` | `text NOT NULL DEFAULT ''` | capability-layer principal, as in `aura.settings` |

- The secret is sealed with `internal/secret.Sealer`, HKDF info `aura-pim-provider-app-v1`,
  derived from `AURA_AUTHULA_SECRET`, the same pattern as `internal/mcpregistry`.
- No row-level security: install-wide data, like `aura.cloudflare_remote_access`.
- `aura_app` gets `SELECT, INSERT, UPDATE`, and no `DELETE`, because no route deletes a row.
- Queries go through sqlc (`internal/db/queries/pim_provider_app.sql`).

## Package

`internal/pimprovider`: a `Store` with `List(ctx)`, `Get(ctx, provider)` and
`Upsert(ctx, App, updatedBy)`. `App` carries the plaintext `ClientSecret` in memory only.
`Get` returns `ErrNotConfigured` when there is no row. The package also owns `Managed(provider)`
and the per-provider validation, so the handler and the store agree on which fields each
provider needs.

## API

All routes are under the existing `/api/connect/pim/` carve-out and mounted in
`cmd/aura/serve_webui.go`.

- `GET /api/connect/pim/providers`, gated on `governance.write` like every other PIM route.
  Returns `[{provider, configured, clientId, tenantId, secretSet}]` for the three managed
  providers. It never returns the secret.
- `PUT /api/connect/pim/providers/{provider}`, gated on `identity.create`. The body is
  `{clientId, tenantId?, clientSecret?}`.
  - `google` needs `clientId`, plus a secret: from the body, or already stored.
  - Microsoft needs `clientId` and `tenantId`. A `clientSecret` sent for a Microsoft provider is
    rejected (400).
  - An empty `clientSecret` keeps the stored one, so a mistyped client ID can be fixed without
    retyping the secret.
  - Returns the same public shape as `GET` for that provider.
  - An unknown provider is 404. A missing field is 400.
- `POST /api/connect/pim/accounts` is the existing route. For a managed provider the handler
  loads the app and overwrites the `providerConfig` keys it owns (`clientId`, `clientSecret`,
  `tenantId`) before forwarding. The rest of the body is kept byte-for-byte through a
  `map[string]json.RawMessage`. Values the browser sends for those keys are discarded, so a user
  cannot choose the client. When the provider has no row the route returns
  `409 {"error":"provider_not_configured"}` and nothing reaches the sidecar.

## Cockpit

- `pimProviders.ts` splits each managed provider's fields into app fields (edited by the admin)
  and account fields. Google, Microsoft 365 and Outlook.com have no account fields left.
- "Add account" (`CalendarConnect.tsx`) shows only account ID, display name and Advanced for a
  managed provider. If `GET …/providers` says it is not configured, the form shows a note
  ("An administrator has to configure Google first") and the submit is disabled.
- `PimProviderAppsPanel.tsx`, rendered in the same calendar section only when
  `useCapabilities().isAdmin`, holds one form per managed provider:
  - the current client ID pre-filled;
  - the secret field empty with a "stored" hint when `secretSet`;
  - under Google, the relay redirect URI to register.
  - Saving invalidates `['connect','pim','providers']`.
- All copy under `governance.mcp.calendar.*`, in English and Italian.

## What happens when the admin changes the credentials

- Accounts already linked keep the copy they were created with in the sidecar and are not
  touched.
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

- Go unit:
  - handler: the create injects the credentials, discards browser values, keeps the rest of the
    body intact, and answers 409 without calling the sidecar when the provider is not configured;
  - `PUT` validation per provider (including the secret refused for Microsoft) and
    empty-secret-keeps-stored;
  - `GET` never serialises the secret;
  - the route table gates `PUT` on `identity.create` (a member identity gets 403).
- Go `db_integration`:
  - store round-trip;
  - the ciphertext column never contains the plaintext;
  - a different HKDF info cannot open it;
  - the migration up and down.
- Web (vitest):
  - the wizard has no credential fields for a managed provider;
  - the not-configured note and disabled submit;
  - the admin panel hidden for a member and saving for an admin;
  - the secret field never pre-filled.
- Mutation testing (CI) on the injection and validation code.
- E2E on the VM (192.168.101.158) after the updater delivers the build:
  1. the admin sets the Google app;
  2. a member identity adds a Google account with only an account ID and completes consent
     through the relay;
  3. the panel shows "linked";
  4. the admin changes the client ID;
  5. the already-linked account still lists calendars.

## Out of scope

- Deleting a provider app.
- Several apps per provider.
- Migrating existing accounts' credentials into the table.
- Any sidecar change.
- Propagating a rotated secret to linked accounts.

## PRD

Amend prd.md §13 once the E2E has run. It records the table, the admin gate, the injection
point and the rotation behaviour, and says what was not measured.
