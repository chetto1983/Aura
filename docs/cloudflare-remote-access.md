# Cloudflare Remote Access

Aura supports a stable named tunnel with two HTTPS hostnames: one protected by Cloudflare
Access OTP and Authula, and another that additionally requires your organization's Cloudflare
One client through Gateway. A registered domain is required. Aura does not purchase domains.

As of 2026-09-20 the implementation has hermetic browser, installer and isolated Caddy evidence.
Named-tunnel live acceptance is **blocked: the operator has no registered domain**. No result
from a mock, a local certificate or a Quick Tunnel closes that acceptance.

## Install and enable

Use the repository installer or the self-extracting Ubuntu appliance installer normally.
Both ship the Compose/Caddy configuration, pull `ghcr.io/chetto1983/aura-cloudflared:edge` on
the edge channel, and wait for the default stack to become healthy. Before onboarding, the
supervisor is healthy-idle; it runs no connector and needs no Cloudflare credential.
The installer never asks for a Cloudflare token or writes one into `.env`.

After initial Authula setup, open **Settings > Remote access** as an administrator.
Verify the API token, explicitly choose the account if there is more than one, enter the
registered domain and two distinct single-label hostnames, then save. When the zone is pending,
copy the assigned nameservers to the registrar. Refreshing the page or restarting Aura resumes
the persisted setup. Registrar propagation can take time; repeatedly creating a zone does not
speed it up.

Open the public hostname, complete Access OTP and Authula, and choose **Confirm external
access** there. Confirmation is generation-bound and available only through the public tunnel;
the direct cockpit cannot certify the external path. Connector loss or a changed generation
requires a fresh confirmation.

## Scoped API token

Create a [custom Cloudflare API token](https://dash.cloudflare.com/profile/api-tokens), scoped
to the selected account and test/production zone. The cockpit needs:

| Permission | Purpose |
| --- | --- |
| Account / Account Settings / Read | Account selection |
| Zone / Zone / Read and Edit | Discover/create the registered zone and read activation |
| Account / Cloudflare Tunnel (Connector) / Edit | Named tunnel, ingress and connector token |
| Zone / DNS / Edit | The two owned CNAMEs |
| Account / Access: Organizations, Identity Providers, and Groups / Edit | Discover/create OTP provider |
| Account / Access: Apps and Policies / Edit | Both hostname applications and email policies |
| Account / Zero Trust / Edit | Organization Gateway posture check |

Cloudflare labels write permissions as “Edit” in its token creation UI; the Aura guidance calls
them “Write”. Candidate verification proves token activity/account discovery, and saving verifies
selected-zone readability. It cannot prove write privileges without provisioning: a later
permission refusal remains visible and needs a corrected token plus retry.

Only active Aura identity emails are admitted; a last administrator cannot be removed by a
membership sync. OTP is delivered to that identity's mailbox. Check spam, the exact identity
email and the Access policy if no code arrives. Do not add an Everyone/bypass rule as a workaround.
See [Cloudflare OTP documentation](https://developers.cloudflare.com/cloudflare-one/integrations/identity-providers/one-time-pin/).

## WARP and network boundaries

Install the Cloudflare One client, enable device enrollment for your users in Zero Trust, and
enroll it in **the same organization**. Its team name is chosen in the Cloudflare dashboard.
Consumer WARP alone is insufficient: Aura uses the [Require Gateway check](https://developers.cloudflare.com/cloudflare-one/reusable-components/posture-checks/client-checks/require-gateway/).
Aura's current status API does not expose the team slug, so the cockpit gives generic enrollment
guidance. Do not infer an enrollment URL from the zone; the live harness requires the operator's
`CLOUDFLARE_TEAM_NAME` separately.

No LAN, Docker, Postgres, Garage-admin, MCP or CIDR route is published. Cloudflared joins only
the dedicated egress-capable tunnel network, with Caddy bridging to the application network.
Garage browser PUT/download paths still pass through Caddy on the same public hostname.
The private Caddy listener explicitly forwards HTTPS as the browser scheme even though the
connector-to-Caddy hop is HTTP; this prevents HTTP presigned upload URLs.

Tunnel needs no inbound router port. Permit outbound DNS, HTTPS to the Cloudflare control API,
and port 7844 TCP (HTTP/2) and UDP (QUIC) to the documented tunnel destinations. Follow the
[current firewall reference](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/configure-tunnels/tunnel-with-firewall/)
for endpoint ranges and optional traffic. Do not expose Caddy's private port 8080.

Direct `https://<server-ip>` on port 443 remains available. It bypasses Cloudflare Access and
relies on Authula, including if the router forwards it. Trusting Caddy's local CA on Ubuntu
does **not** establish trust in remote browsers; direct clients each need their own trust
configuration. Named-tunnel clients use Cloudflare's publicly trusted edge certificate.

[Quick Tunnels](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/do-more-with-tunnels/trycloudflare/)
are temporary development/testing tools, with no SLA, a concurrent-request limit, and no SSE
support. An existing Quick Tunnel may help inspect a page; it cannot certify Aura production
chat or substitute for the registered-domain acceptance. No random temporary URL belongs in
the installed configuration.

## Lifecycle and recovery

- **Disable** stops the local connector and removes its token projection; it keeps Cloudflare
  resources and encrypted PostgreSQL settings. **Re-enable** uses the saved credentials.
- **Replace API token** verifies and stores a new scoped API credential without changing the
  enabled state or hostnames. It does not rotate the connector credential.
- **Rotate connector token** in the Cloudflare Dashboard using the
  [documented action](https://developers.cloudflare.com/tunnel/reference/tunnel-tokens/), then
  click **Refresh Dashboard-rotated token**. Aura retrieves and encrypts the new current token
  and hands it to the supervisor. There is no documented public rotation mutation; refresh is
  not revocation. Record handoff interruptions rather than assuming zero downtime.
- **Delete remote access** requires the exact current public hostname. It removes only resources
  whose persisted IDs and ownership markers agree. The zone, unrelated DNS and account-wide
  providers reused from other applications remain. Failed deletion preserves IDs for retry;
  fixing a credential resumes deletion. Hostname/account changes require delete and re-onboard.

PostgreSQL is the durable authority for desired state and both AES-GCM encrypted credentials.
Back up PostgreSQL **and the existing Authula wrapping secret** using the appliance's protected
backup procedure. The `aura-cloudflared-state` volume is a disposable projection, not a secret
backup. Aura reconstructs it after boot. Normal installer reruns and edge updates preserve both
the PostgreSQL and projection volumes; do not use `docker compose down -v` to update.

On an existing edge appliance the updated payload/updater adds missing cloudflared image/pull
defaults before starting the new service, including after updater self-reexec. Explicit image
pins and pull-policy values are preserved. Version-pinned installations remain release-controlled;
the updater does not switch their sidecar to edge.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| Waiting for nameservers | Registrar delegation exactly matches the cockpit; let Cloudflare activate the existing zone. |
| Permission/token refused | Replace the scoped API token, then retry; verification alone cannot prove write privileges. |
| Connector disconnected | Outbound 7844/DNS, `docker compose ps aura-cloudflared`, sanitized sidecar logs; Aura remains available directly. |
| Access loop | Correct hostname, active Aura email, OTP mailbox, browser cookies and organization enrollment on the WARP host. |
| Connecting after login | Confirm from the public cockpit after both logins; do not spoof the ingress marker from a direct client. |
| Buffered chat | Use a named tunnel; remove extra proxy buffering and verify incremental SSE arrivals. |
| Upload/save failure | PUT and download URLs must use the same HTTPS public hostname, with Caddy routing the per-identity bucket to Garage. |
| Delete remains in error | Repair the credential and retry; do not remove the persisted IDs or delete matching names manually. |

## Acceptance commands and truthful boundaries

Hermetic tests do not contact Cloudflare or need a token:

```bash
bash scripts/install_remote_access_test.sh
bash scripts/install_config_test.sh
bash scripts/install_lib_test.sh
bash scripts/build_installer_test.sh       # requires makeself; actually executes the archive
bash scripts/cloudflare_update_test.sh
bash scripts/aura_image_update_test.sh
bash scripts/cloudflare_tunnel_live_e2e_test.sh
AURA_CADDY_TEST_IMAGE=aura-caddy:local bash scripts/cloudflare_caddy_origin_test.sh
cd web
AURA_E2E_ORIGIN=http://127.0.0.1:5173 npx playwright test e2e/remote-access.spec.ts --project=chrome
```

Serve the frontend on that origin first (`npm run dev -- --host 127.0.0.1`). API responses are
controlled browser fixtures. The 390px Italian case measures page/panel scroll widths, actual
control dimensions and clipped labels, and the workflow case checks keyboard dialog focus.

The live runner requires an attended Linux desktop connected to a **dedicated disposable Ubuntu
appliance**, a registered test domain already added as a **pending** Cloudflare zone, an unrelated
pre-seeded TXT record, mailbox access, a funded configured chat model and working Garage/Studio.
A pending zone is necessary to measure restart/resume at nameserver waiting. Start by installing
that appliance with the generated self-extracting installer; finish Authula setup but leave Remote
Access pristine. Run the browser on a machine whose One client you can enroll/disconnect; Docker
must address only that dedicated appliance. `AURA_E2E_APPLIANCE_DIR` names its Compose directory.

Provide these variables through a private session, never a committed file or command-line token:

The replacement API token must be a second valid scoped token; keep the initial token active
until the read-only Cloudflare inventory checks finish. The denied email must not belong to an
active Aura identity. The harness never fetches an OTP from email or fabricates an Access session.

```text
AURA_E2E_CLOUDFLARE=1
AURA_E2E_CLOUDFLARE_DEDICATED=1
CLOUDFLARE_API_TOKEN
CLOUDFLARE_REPLACEMENT_API_TOKEN
CLOUDFLARE_ACCOUNT_ID
CLOUDFLARE_TEST_ZONE
CLOUDFLARE_TEAM_NAME
AURA_E2E_ADMIN_EMAIL
AURA_E2E_DENIED_EMAIL
AURA_E2E_ORIGIN
AURA_E2E_APPLIANCE_DIR
AURA_E2E_UNRELATED_TXT_NAME
```

Run `bash scripts/cloudflare_tunnel_live_e2e.sh`. Missing prerequisites are fatal when opted in;
without opt-in it exits 2 with BLOCKED, not a green skipped acceptance. CI cannot provide the
attended OTP/WARP/Dashboard steps and is refused. The browser is headed and all traces, videos
and screenshots are disabled before credentials enter it. Do not enable debug/network logging.

The runner uses unique `aura-e2e-<run>` labels. It registers cleanup before configuration and
deletes only through Aura's guarded exact-host endpoint, never a direct Cloudflare DELETE. It
compares before/after Cloudflare inventories and confirms the zone/TXT remain. A forced process
kill can prevent cleanup: the failure message names the exact host; reopen the direct cockpit
and retry deletion there. Never “clean up” by deleting an entire zone or any name match.

Interactive checkpoints supply OTP, enrollment, image-edit actions and Dashboard rotation; the
subsequent network/state assertions determine success. Attachments contain only non-secret
measurements, including continuous health samples during rotation. A failure or missing checkpoint
leaves acceptance incomplete. The live suite has not run for this delivery because the domain
prerequisite is absent; full release/coverage gates belong to Task 7.
