# Cloudflare Tunnel remote access — design

Date: 2026-09-20. Status: approved in conversation, section by section. This document is
the source for the implementation plan.

## Goal

Make Aura work reliably outside the operator's LAN through a Cloudflare Tunnel managed from
the cockpit, while preserving direct `https://<server-ip>` access for operators who prefer to
open port 443 on their router.

The delivered system provides both:

1. a stable public hostname protected by Cloudflare Access and Aura's existing Authula login;
2. a second hostname whose Access policy requires an enrolled Cloudflare One/WARP client.

The operator configures the integration from Aura. They do not edit Compose files, copy a
tunnel token into an environment variable, or run `cloudflared` by hand.

## Decisions approved by the operator

| Question | Decision |
|---|---|
| Tunnel shape | A pinned `cloudflared` sidecar managed by Aura. |
| Public and private access | Support a public hostname and a second WARP-required hostname on one remotely managed tunnel. |
| Cloudflare ownership | Aura creates and reconciles Cloudflare resources through the API; it does not merely accept a pre-created tunnel token. |
| Domain setup | Guided zone onboarding: create or discover the zone, display assigned nameservers, and wait for activation. Domain purchase remains external. |
| Public authentication | Cloudflare Access is enabled, in addition to Authula. |
| Access membership | Reconcile Access membership from active Aura users; the local administrator is protected from lockout. |
| WARP scope | Protect a second Aura hostname with WARP device posture. Create no private-network or CIDR route. |
| Runtime token reload | A minimal supervisor in the sidecar watches the derived token projection and restarts `cloudflared` without Docker-socket access. |
| Direct ingress | Keep Caddy on `0.0.0.0:443`, so direct IP/LAN/router access remains possible. The cockpit must state that this path bypasses Cloudflare Access and relies on Authula. |
| Secret authority | PostgreSQL is the sole durable source for every Cloudflare credential. Runtime files are disposable projections. |

## Why Quick Tunnel is excluded

TryCloudflare Quick Tunnels need no domain, but Cloudflare documents them as development-only,
without an uptime SLA, capped at 200 concurrent in-flight requests, and without Server-Sent
Events. Aura's chat stream uses SSE, so a Quick Tunnel would make the core interaction fail by
design. Aura therefore supports only a remotely managed named tunnel.

References:

- [Quick Tunnel limits](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/do-more-with-tunnels/trycloudflare/)
- [Create a remotely managed tunnel through the API](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/get-started/create-remote-tunnel-api/)
- [Tunnel token permissions](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/configure-tunnels/remote-tunnel-permissions/)

## Architecture

### Components

```text
Browser
  │
  ├─ public: Cloudflare Access → Cloudflare edge ─┐
  │                                               │
  └─ WARP-only hostname → Access posture check ───┤
                                                  ▼
                                     cloudflared + supervisor
                                                  │ Docker-only HTTP
                                                  ▼
                                            internal Caddy
                                             ├─ Aura :9080
                                             └─ Garage :3900

Aura daemon
  ├─ Cloudflare API client
  ├─ persisted reconciliation state
  ├─ encrypted secrets in PostgreSQL
  └─ disposable sidecar control projection
```

The existing external Caddy listener remains unchanged. A second listener is available only
inside the Compose network and imports the same route definitions as the external listener.
The sidecar targets this internal listener over Docker networking. This preserves one routing
authority for cockpit traffic, per-identity Garage bucket paths, setup paths, and future Caddy
routes; the tunnel must not duplicate those path rules.

The public Cloudflare certificate terminates at the edge. External clients do not need to trust
Caddy's internal CA. Direct IP/LAN clients continue to use the existing Caddy certificate and
Authula flow.

### Package boundaries

- `internal/cloudflareapi`: typed Cloudflare REST client, request/response validation, pagination,
  permission probes, redaction, and a narrow interface for tests.
- `internal/remotetunnel`: desired state, persisted reconciliation state machine, ownership
  checks, retry policy, disable/delete semantics, and status projection for the API.
- `internal/agui`: admin-only HTTP handlers. Handlers validate and delegate; they do not contain
  Cloudflare orchestration.
- `cmd/aura-cloudflared-supervisor`: minimal process supervisor. It knows nothing about accounts,
  zones, identities, or the Cloudflare API.
- `web/src/settings/remoteAccess`: guided wizard, status, diagnostics, disable and delete controls.
- `docker/cloudflared`: pinned `cloudflared` binary plus the supervisor, running non-root.

Each unit has one direction of dependency. The reconciler depends on the Cloudflare client and
PostgreSQL interfaces. The sidecar depends only on its projection files and `cloudflared`.

## Persistent data and secrets

PostgreSQL is authoritative.

Secret rows in `aura.settings`, encrypted by the existing AES-GCM settings store:

- `CLOUDFLARE_API_TOKEN`
- `CLOUDFLARE_TUNNEL_TOKEN`

Neither secret is overlaid into the Aura process environment. The Cloudflare client reads the
API token through `settings.Store.Secret`; the sidecar receives only the tunnel token through a
derived runtime projection. API responses expose `has_value`, never the plaintext.

A dedicated singleton state row records non-secret desired and observed state:

- enabled flag and reconciliation generation;
- account ID;
- zone ID and zone name;
- tunnel ID and name;
- public and WARP-required hostnames;
- both DNS records, both Access applications/policies, the OTP identity-provider ID, and the
  WARP posture-check identifier;
- current phase, observed health, last successful reconciliation and sanitized last error;
- administrator who changed the desired state and timestamps.

The migration number is chosen at implementation time from the live migration directory.

### Sidecar projection

Aura and the supervisor share a dedicated named volume. Aura atomically writes:

- `token` — mode `0600`, the decrypted tunnel token;
- `desired.json` — non-secret enabled flag and monotonically increasing generation.

Writes use temporary files, fsync and rename. On Aura boot the projection is rebuilt from
PostgreSQL. Disabling the integration removes the token projection. A token file is never a
backup, migration input, settings authority, or API response.

The official `cloudflared --token-file` option reads the token only at process start. The
supervisor watches the projection generation, launches a candidate process, waits for its
readiness endpoint, then retires the old process. Failed candidates leave the last healthy
connector running. Tokens never appear in argv, logs, health responses, or process titles.

References:

- [Tunnel run parameters and token-file](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/configure-tunnels/run-parameters/)
- [cloudflared token-file startup behavior](https://github.com/cloudflare/cloudflared/blob/master/cmd/cloudflared/tunnel/subcommands.go)

## Guided onboarding and reconciliation

The onboarding is a persisted, idempotent state machine. Closing the browser, restarting Aura,
or retrying a failed Cloudflare request resumes the incomplete phase.

1. **Credential check** — accept the API token once, verify it against Cloudflare, discover
   accounts, and report missing permissions without storing an unusable token.
2. **Account selection** — auto-select a single account or require an explicit choice.
3. **Zone** — discover an existing zone or create one for the entered registered domain.
4. **Nameservers** — display Cloudflare's assigned nameservers and poll until the zone is active.
   Aura cannot buy the domain or modify an arbitrary registrar.
5. **Tunnel** — create an Aura-owned remotely managed tunnel or reconcile the persisted tunnel ID.
6. **Access first** — discover or create Cloudflare's One-time PIN identity provider, then create
   both Access applications and their email allow policies before publishing DNS. An empty
   allowlist is a hard refusal.
7. **Public route** — configure tunnel ingress to internal Caddy and create the Aura-owned CNAME.
8. **WARP-only route** — create a second published hostname with the same origin, then require the
   Cloudflare One Client's WARP posture check in that hostname's Access policy. No private-network,
   CIDR or Docker route is created.
9. **Connector** — retrieve the tunnel token, store it encrypted in PostgreSQL, project it, and
   wait for the supervisor plus Cloudflare tunnel health.
10. **Interactive acceptance** — an administrator opens the public hostname, completes Access OTP
    and Authula, then confirms the current generation through an admin-only endpoint reached through
    Caddy's unexposed tunnel listener. The direct HTTPS listeners strip the tunnel marker; only the
    internal `:8080` listener sets it. Aura therefore needs no Access service token or third durable
    credential. The accepted generation is persisted and invalidated by a desired generation change
    or loss of Cloudflare connector health.

The default public label is `aura`, producing `aura.<zone>`. The WARP-required label defaults to
`aura-warp`, producing `aura-warp.<zone>`. Both are editable single DNS labels and both use
Cloudflare's public certificate, so WARP clients need no Caddy CA.

Cloudflare resources are tracked by their IDs and, where the API supports it, an Aura ownership
comment/tag. Reconciliation mutates only resources whose persisted ID and ownership marker agree.
Name matching alone never grants ownership.

## Identity and Access synchronization

Cloudflare Access is a first gate; Authula remains the application gate.

- One-time PIN is the default Cloudflare identity provider, so an operator needs no separate
  Okta/Entra/SAML setup. Aura creates it only when the account has no suitable OTP provider.
- Active Aura identity emails form the Access allowlist.
- Identity creation, disable and deletion enqueue reconciliation instead of blocking the identity
  transaction on a Cloudflare network call.
- The administrator who enabled remote access remains included until another active administrator
  is confirmed in both systems.
- Reconciliation refuses to publish the hostname if it would produce an empty policy or lock out
  the last administrator.
- Cloudflare API failure never rolls back a valid Aura identity mutation; status becomes degraded
  and retry converges later.

The WARP-required hostname uses the same user set plus a required WARP posture check. Because Aura
creates no private-network route at all, enrollment grants no path to the LAN or Docker network.

References:

- [Require WARP in an Access policy](https://developers.cloudflare.com/cloudflare-one/reusable-components/posture-checks/client-checks/require-warp/)
- [Cloudflare Access policy selectors](https://developers.cloudflare.com/cloudflare-one/access-controls/policies/)
- [One-time PIN identity provider](https://developers.cloudflare.com/cloudflare-one/integrations/identity-providers/one-time-pin/)

## Cockpit experience

Add an administrator-only **Remote access** entry to the existing Settings rail. It mounts only
when selected and follows existing settings-panel, capability and secret-input patterns.

### Unconfigured

- Explain ordinary public access versus the WARP-required hostname.
- API token field with a link to the exact least-privilege permissions Aura needs.
- `Connect Cloudflare` starts validation; the token is never redisplayed.

### Guided setup

- Stepper: Account → Domain → Nameservers → Tunnel → Access → WARP → Verify.
- Every step shows completed, active, waiting or failed state and a retry action.
- Nameserver instructions are copyable and remain visible while Cloudflare reports pending.
- Long operations continue server-side; page refresh reattaches to persisted state.

### Configured

- Public and WARP-required hostnames, copy/open actions, tunnel and connector health.
- Last successful reconciliation, last sanitized error and retry.
- Access membership summary, WARP posture status and enrollment instructions.
- API-token replacement, connector-token refresh after an operator rotates it in Cloudflare,
  disable and delete actions.
- A permanent warning states that direct `0.0.0.0:443` / router access bypasses Cloudflare Access
  and relies on Authula.

Secret replacement is write-only. Destructive deletion requires typed confirmation naming the
public hostname.

## API surface

Dedicated admin endpoints are preferable to exposing Cloudflare configuration as generic key/value
settings:

- `GET /api/settings/remote-access` — desired state, progress, resource summary and health;
- `POST /api/settings/remote-access/token/verify` — validate a candidate API token and enumerate
  accounts without storing it;
- `PUT /api/settings/remote-access` — save desired account/domain/hostnames and encrypted token;
- `POST /api/settings/remote-access/reconcile` — resume or retry immediately;
- `POST /api/settings/remote-access/token/refresh` — retrieve, encrypt and project the current
  connector token after the operator rotates it in Cloudflare's dashboard;
- `POST /api/settings/remote-access/accept-external` — persist authenticated acceptance of the
  current generation only when called by an Aura administrator through the tunnel listener;
- `POST /api/settings/remote-access/disable` — stop the connector while preserving remote resources;
- `DELETE /api/settings/remote-access` — delete only verified Aura-owned remote resources;
- `GET /api/settings/remote-access/events` — bounded recent reconciliation events, sanitized.

Every route requires the existing governance-write capability. Request bodies are strict-decoded,
bounded and idempotency-protected where they mutate external state.

Cloudflare's documented public API exposes `GET /accounts/{account_id}/cfd_tunnel/{tunnel_id}/token`
but no tunnel-token rotation mutation. The cockpit must not pretend that a GET rotated or revoked a
credential: it links to Cloudflare's documented Dashboard rotation, then refreshes the newly current
token into encrypted PostgreSQL state. Aura does not use undocumented Cloudflare endpoints.

References:

- [Cloudflare Tunnel token retrieval and Dashboard rotation](https://developers.cloudflare.com/tunnel/reference/tunnel-tokens/)
- [Get a Cloudflare Tunnel token API](https://developers.cloudflare.com/api/resources/zero_trust/subresources/tunnels/subresources/cloudflared/subresources/token/methods/get/)

## Sidecar and Compose contract

The sidecar:

- is part of the default Compose file but reports healthy-idle before configuration;
- runs as non-root with a read-only root filesystem, dropped capabilities, no host ports and bounded
  memory/CPU/PIDs;
- joins only the network needed to reach internal Caddy;
- receives no database URL, API token, Docker socket or Aura session secret;
- uses a pinned cloudflared version/digest and `--no-autoupdate`; Aura upgrades it with the rest of
  the appliance;
- exposes health and process status only on the internal Compose network;
- forwards structured, token-redacted logs to the normal container log path.

Aura does not gate its own readiness on Cloudflare. A broken or unconfigured tunnel degrades only
remote access. Direct Caddy access remains available.

## Failure and lifecycle semantics

Visible states are `disabled`, `validating`, `waiting_nameservers`, `provisioning`, `connecting`,
`healthy`, `degraded` and `error`.

- Transient API failures use bounded exponential backoff and retain the last healthy connector.
- Authentication or permission failures stop automatic retry and request a new API token.
- A pending zone remains waiting; it is not recreated.
- Conflicting DNS or Cloudflare resources not owned by Aura are reported, never overwritten.
- A connector crash is restarted by the supervisor and Compose policy; repeated crashes become
  degraded with the last redacted diagnostic.
- **Disable** removes the local token projection and stops cloudflared, but preserves the tunnel,
  zone, DNS, Access and WARP posture resources for quick re-enable.
- **Delete integration** removes only the persisted Aura-owned tunnel, CNAME, Access applications
  and policies, and Aura-created posture check. It never deletes the Cloudflare zone, domain,
  unrelated DNS, other tunnels or account-wide settings it did not create.
- Deleting Cloudflare resources is best-effort and resumable. Local IDs are retained until remote
  absence is confirmed.

## Security properties

- All Cloudflare credentials are encrypted in PostgreSQL with the existing settings key hierarchy.
- The API token is least-privilege and never reaches cloudflared.
- The tunnel token grants connector execution only and never reaches the browser.
- Logs, audit rows and errors pass through secret redaction.
- The sidecar has no host ingress and no Docker control plane.
- Cloudflare Access is installed before DNS publication.
- WARP is an Access requirement on one hostname; no private IP/CIDR route exists.
- Direct port-forwarded access is deliberately supported and clearly identified as an Authula-only
  path that bypasses Cloudflare Access.
- No operation trusts a client-supplied Cloudflare resource ID without matching persisted ownership.

## Testing and acceptance

### Unit and component tests

- Cloudflare response-envelope decoding, pagination, permission errors and redaction.
- Reconciler transition table, retry classification, resume after every phase, idempotency and
  ownership mismatch refusal.
- Access membership synchronization and last-admin lockout property tests.
- Atomic runtime projection, deletion on disable and absence of plaintext credentials in logs.
- Supervisor startup idle, initial token, candidate handoff, failed candidate fallback, signal
  forwarding and crash throttling using a fake child process.
- Cockpit wizard states, refresh/resume, secret write-only behavior, permission gate, disable and
  typed destructive confirmation.

### Container integration

- Sidecar starts healthy-idle with no token and Aura remains ready.
- Saving a token starts cloudflared without recreating Aura or using Docker APIs.
- Token replacement hands over to a new process and removes the old projection.
- Only internal Caddy is reachable from the sidecar network; Postgres, Garage and other sidecars
  are not directly reachable.
- Caddy's internal listener preserves per-identity Garage PUTs and cockpit routes.

### Installer acceptance

- Both the repository installer and the self-extracting appliance installer ship the updated
  Compose/Caddy payload, pull the published cloudflared sidecar image and start it healthy-idle.
- The installer never asks for or writes a Cloudflare credential: onboarding remains in the
  authenticated cockpit and PostgreSQL remains the only durable credential authority.
- A fresh Ubuntu Server install exposes the guided Remote Access entry point after setup, while an
  installer rerun or edge-channel update preserves the PostgreSQL state and projection volume.
- Installer completion identifies the cockpit as the place to enable remote access and does not
  claim that the temporary local-CA workflow makes remote browsers trusted.

### Live Cloudflare acceptance

A separately gated live suite uses a dedicated test domain/account and cleans up every owned
resource. It must prove:

1. guided zone onboarding pauses and resumes across a daemon restart;
2. Cloudflare Access denies an unlisted identity and admits an active Aura identity;
3. Authula login works after Access;
4. chat SSE streams incrementally through the public hostname;
5. steer/cancel and long-lived requests survive the tunnel;
6. Garage upload, download, image editing and Studio save work through the same hostname;
7. the Service Worker registers under the public certificate;
8. an enrolled WARP client reaches the WARP-required Aura hostname and an unenrolled client is denied;
9. the Cloudflare account contains no Aura-created private-network route, and no Docker service or
   LAN address is reachable through the connector;
10. API-token replacement plus Dashboard tunnel-token rotation and cockpit refresh leave no
    plaintext in API responses, logs, process args, PostgreSQL raw values or runtime files after
    disable;
11. direct `https://<server-ip>` access still works through Authula;
12. delete integration removes only Aura-owned resources and preserves the zone plus unrelated DNS;
13. a fresh self-extracting Ubuntu Server install starts the idle sidecar, completes cockpit
    onboarding without an installer credential, and survives an installer rerun/update.

The phase closes only after this live path passes on the Ubuntu Server appliance. Mocked Cloudflare
tests alone are not completion evidence.

## Documentation and operator handoff

- The cockpit links to the exact token-creation permissions and explains why each is needed.
- Installation docs state that outbound connectivity to Cloudflare is required and no inbound port
  is required for Tunnel operation.
- WARP enrollment instructions are generated from the configured Zero Trust organization.
- Troubleshooting covers pending nameservers, permission refusal, connector down, Access loop,
  SSE buffering, upload failure and direct-ingress fallback.
- Backup/restore includes Cloudflare state and encrypted credentials because PostgreSQL is the sole
  authority; runtime projection volumes are explicitly excluded.

## Alternatives rejected

- **Quick Tunnel:** no SSE, no SLA, development-only.
- **Dashboard/token-only setup:** contradicts cockpit-managed onboarding and leaves configuration
  split across two authorities.
- **Locally managed tunnel:** Cloudflare recommends remotely managed tunnels for most deployments;
  local certificate and JSON credentials broaden secret handling.
- **Direct cloudflared → Aura:** bypasses Caddy's Garage routing and duplicates front-door rules.
- **Docker-socket restart:** grants an unnecessary container control plane to Aura.
- **Environment-variable secrets:** exposes durable credentials to child processes and requires
  Compose recreation.
- **Whole-LAN WARP route:** violates the operator's Aura-only requirement.
- **Automatic zone deletion:** risks destroying unrelated DNS and is never necessary to disable Aura.

## Out of scope

- Purchasing or renewing a domain.
- Managing non-Aura DNS records, unrelated Cloudflare applications, or arbitrary Gateway policy.
- Publishing Postgres, Garage, MCP servers, SSH, the Docker network or the host LAN through WARP.
- Quick Tunnels.
- Multiple connector replicas on separate physical hosts; the design does not prevent adding them
  later, but this phase owns one appliance sidecar.
- Replacing Authula with Cloudflare Access.
- Closing the existing `0.0.0.0:443` direct ingress.
