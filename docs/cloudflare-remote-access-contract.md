# Cloudflare remote access control-plane contract

Status: official-document contract and controlled fixture tests, 2026-09-20.
**Live account measurement is OPEN until Task 6.** No Cloudflare credential was available
for Task 1. Fixtures are synthetic; they are not captured live account responses.

## Inventory and reuse

`go.mod` has no Cloudflare SDK dependency. Existing dependencies provide `net/http`,
`encoding/json`, pgx v5 and sqlc. `internal/settings` already provides authenticated
AES-GCM secret storage, `enc:v1:` wire values, settings reads and the secret-excluding
environment overlay. This implementation reuses that store and generated queries.
The task explicitly authorizes the narrow Cloudflare REST client; no SDK or new module
dependency was added. Cloudflare also publishes a comprehensive
[Go SDK API surface](https://developers.cloudflare.com/api/go/resources/zero_trust/),
which was inventoried as a possible alternative to the approved narrow client.

## HTTP behavior

The API root is `https://api.cloudflare.com/client/v4`. Requests use bearer authentication,
JSON request/response bodies, a 15-second timeout and a 2 MiB response limit. Redirects
are refused to prevent credential forwarding and redirected mutations. `success: true`
is required together with a successful HTTP status and an empty errors list. Typed
reads also require a non-null, decodable result. Delete endpoints may return null.
Only HTTP 429, HTTP 5xx and transport timeouts are retryable. The caller schedules retries;
the client never automatically repeats a write.

Error messages carry only a fixed reason, HTTP status and numeric Cloudflare code.
Response text, transport error strings, request URLs and tokens are never included.
`Secret` redacts formatting and JSON/text marshaling; `Reveal()` is the deliberate
credential boundary. Formatting the Client itself also redacts its unexported fields.

[Accounts](https://developers.cloudflare.com/api/resources/accounts/methods/list/) use
`page`, `per_page` and `result_info` pagination. Account, zone, tunnel, DNS, identity-provider
and application discovery consume all pages, using total pages or total count when present.
Pagination refuses inconsistent page numbers, unexpected empty intermediate pages and more
than 1,000 pages. The posture list is a non-paginated endpoint.

## Resource contracts and permissions

| Purpose | Endpoint beneath API root | Request/permission contract |
| --- | --- | --- |
| Verify user token | `GET /user/tokens/verify` | Require returned status `active`; account-owned token verification is outside this onboarding contract. |
| Discover accounts | `GET /accounts` | Account Settings Read. |
| Discover/create zone | `GET/POST /zones`, `GET /zones/{id}` | Zone Read / Zone Write; creation sends account ID, registered domain name and type `full`. |
| Tunnel lifecycle | `/accounts/{account}/cfd_tunnel` | Cloudflare Tunnel Write (or documented equivalent connector permission); create sends `config_src: cloudflare`. |
| Connector credential | `GET .../cfd_tunnel/{id}/token` | Result is a string; wrap it as Secret immediately. Retrieval itself does not rotate/revoke the credential. |
| Ingress | `PUT .../cfd_tunnel/{id}/configurations` | Body has a `config.ingress` array and final `http_status:404` catch-all. No private network routes are created. |
| CNAME | `/zones/{zone}/dns_records` | DNS Write; proxied CNAME to `{tunnel}.cfargotunnel.com`, TTL 1, Aura ownership comment. |
| One-time PIN | `/accounts/{account}/access/identity_providers` | Access: Organizations, Identity Providers, and Groups Write; wire type is `onetimepin`, config `{}`. Reuse an existing provider of that type. |
| Access application/policy | `/accounts/{account}/access/apps`, `.../{app}/policies` | Access: Apps and Policies Write; self-hosted app restricts allowed IdPs; policy includes explicit individual emails and requires OTP provider. |
| Enrolled-client posture | `/accounts/{account}/devices/posture` | Zero Trust Write; create type `gateway` with Aura ownership description. Require its ID with `device_posture.integration_uid` in the email policy. |

Sources read before implementation:
[token verification](https://developers.cloudflare.com/api/resources/user/subresources/tokens/methods/verify/),
[zone creation](https://developers.cloudflare.com/api/resources/zones/methods/create/),
[remote tunnel API guide](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/get-started/create-remote-tunnel-api/),
[tunnel configurations](https://developers.cloudflare.com/api/resources/zero_trust/subresources/tunnels/subresources/cloudflared/subresources/configurations/methods/update/),
[DNS records](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/create/),
[OTP](https://developers.cloudflare.com/cloudflare-one/integrations/identity-providers/one-time-pin/),
[identity providers](https://developers.cloudflare.com/api/resources/zero_trust/subresources/identity_providers/methods/create/),
[applications](https://developers.cloudflare.com/api/resources/zero_trust/subresources/access/subresources/applications/),
[application policies](https://developers.cloudflare.com/api/resources/zero_trust/subresources/access/subresources/applications/subresources/policies/),
[posture creation](https://developers.cloudflare.com/api/resources/zero_trust/subresources/devices/subresources/posture/methods/create/).

The permission table is a documentation-derived contract, **not a measured least-privilege
token recipe**. Task 6 must verify the actual permission set on the dedicated account,
including zone creation and account discovery. The read-only probe cannot prove write access.

## WARP-required means organization-enrolled

The operator-facing hostname remains “WARP-required.” Its API type is Gateway:
Cloudflare's [Require Gateway](https://developers.cloudflare.com/cloudflare-one/reusable-components/posture-checks/client-checks/require-gateway/)
checks enrollment and traffic through the configured organization's Gateway.
[Require WARP](https://developers.cloudflare.com/cloudflare-one/reusable-components/posture-checks/client-checks/require-warp/)
also admits the consumer client, which does not satisfy the spec's enrolled-client acceptance.
The orchestration ruling therefore selects `EnsureGatewayPosture` and `GatewayPostureID`.
The planned SQL column `warp_posture_id` stores that Gateway ID and has a clarifying comment.
The historical fixture filename `posture-warp.json` describes the hostname purpose; its
wire type is correctly `gateway`.

Gateway enrollment alone does not create a LAN or Docker route. Actual allowed enrolled
traffic, rejected consumer/unmanaged traffic, and absence of private routes remain Task 6
live acceptance requirements.

## Ownership and persistence boundaries

The singleton row contains only non-secret desired state, generation, progress and resource
IDs. `SaveDesired` increments generation and `Advance` uses compare-and-swap on the expected
generation. Zero rows updated means `ErrStaleGeneration`. `Advance` never changes desired
fields, and replaces arbitrary last-error text with a fixed safe diagnostic.
`NewStore` accepts the existing sqlc DBTX seam, including a transaction.

Secrets use `settings.Store` under `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_TUNNEL_TOKEN`.
They are encrypted at rest and skipped by `settings.OverlayEnv`.

`EnsureCNAME` refuses an existing hostname unless both its persisted ID and ownership
comment match. `EnsureGatewayPosture` validates the persisted ID, type and ownership
description. Deleting DNS/posture checks ownership again. Raw application, policy and tunnel
methods are transport primitives: Task 2 must verify persisted ID plus its resource-specific
ownership name before updating/deleting them, serialize reconciliation per generation, and
commit each returned ID before the next operation. It must not infer ownership from a name
match or retry a creation blindly after an ambiguous timeout. The client never deletes zones
or account-wide identity providers.

SQL contracts use [named sqlc parameters](https://docs.sqlc.dev/en/latest/howto/named_parameters.html),
[PostgreSQL conditional UPDATE/RETURNING](https://www.postgresql.org/docs/current/sql-update.html)
and the installed [pgx pool API](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool).

## Credential-free probe verification and open live measurement

Run `bash scripts/cloudflare_remote_access_probe_test.sh` with Bash and jq installed.
It executes the actual probe with controlled curl/jq functions; jq delegates to the real
parser so the actual allowlist filter is exercised. Assertions cover URLs, bearer header
on stdin, no token in curl arguments, success and failure exit codes, and absence of
IDs, email, nameservers and tokens from stdout/stderr. No source-grep test is used.

For the live read-only probe, export `CLOUDFLARE_API_TOKEN` from the password manager and
`CLOUDFLARE_TEST_ACCOUNT_ID`, then run `bash scripts/cloudflare_remote_access_probe.sh`.
Do not enable shell tracing in the calling shell. The probe disables inherited tracing,
passes the bearer header through curl stdin, suppresses raw curl/jq errors and prints
only structural summaries. It fails closed on HTTP failures and unsuccessful envelopes.

Still open: real envelope and pagination observations, account permission refusal codes,
least-privilege write permission measurements, token rotation, zero-trust organization
configuration and full Ubuntu-appliance acceptance. No Task 1 result closes those gates.
