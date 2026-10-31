# Cloudflare Remote Access

Aura supports a stable named tunnel with one HTTPS hostname, protected by Cloudflare Access OTP
and Authula. A registered domain is required. Aura does not purchase domains. The Access session
lasts a month, so a phone asks for one email PIN a month.

No run of the live acceptance runner below is recorded. The four-step wizard has run against
mocked API responses only. No result from a mock, a local certificate or a Quick Tunnel closes
that acceptance.

## Install and enable

Use the repository installer or the self-extracting Ubuntu appliance installer normally.
Both ship the Compose/Caddy configuration, pull `ghcr.io/chetto1983/aura-cloudflared:edge` on
the edge channel, and wait for the default stack to become healthy. Before onboarding, the
supervisor is healthy-idle; it runs no connector and needs no Cloudflare credential.
The installer never asks for a Cloudflare token or writes one into `.env`.

After initial Authula setup, open **Settings > Remote access** as an administrator. The wizard
has four steps, and you can walk back to any of them at any time:

1. **Cloudflare.** Create the token, verify it and choose the account.
2. **Domain.** Enter the registered domain and the address label. A domain active in another
   Cloudflare account is refused before anything is created. When the zone is pending, copy the
   two nameservers to the registrar. Registrar propagation can take time; Aura continues by
   itself when Cloudflare activates the zone.
3. **Connection.** Aura creates the tunnel, the address and the Access protection.
4. **Try it from your phone.** Open the public hostname on mobile data, complete Access OTP and
   Authula, and choose **Confirm external access** there. Confirmation is available only through
   the public tunnel: the direct cockpit cannot certify the external path.

Refreshing the page or restarting Aura resumes the persisted setup. When every step is green the
wizard stays on screen, and each step can be reopened from there.

The phone test is done once per address. If the tunnel drops, the last step turns red, and it
turns green again by itself when the tunnel is back, with no new test. A new domain, label or
Cloudflare account asks for the test again. A new token in the same account does not.

## Scoped API token

**Create the token on Cloudflare** in the first step opens Cloudflare's token form with the
permissions already chosen. Add Cloudflare Tunnel Edit by hand: Cloudflare documents no pre-fill
key for it. The cockpit needs:

| Permission | Purpose |
| --- | --- |
| Account / Account Settings / Read | Account selection |
| Zone / Zone / Read and Edit | Discover/create the registered zone and read activation |
| Account / Cloudflare Tunnel (Connector) / Edit | Named tunnel, ingress and connector token |
| Zone / DNS / Edit | The owned CNAME |
| Account / Access: Organizations, Identity Providers, and Groups / Edit | Discover/create OTP provider |
| Account / Access: Apps and Policies / Edit | The hostname's application and email policy |

Cloudflare labels write permissions as “Edit” in its token creation UI; the Aura guidance calls
them “Write”. Candidate verification proves token activity/account discovery, and saving verifies
selected-zone readability. It cannot prove write privileges without provisioning: a later
permission refusal remains visible and needs a corrected token plus retry.

Only active Aura identity emails are admitted; a last administrator cannot be removed by a
membership sync. OTP is delivered to that identity's mailbox. Check spam, the exact identity
email and the Access policy if no code arrives. Do not add an Everyone/bypass rule as a workaround.
See [Cloudflare OTP documentation](https://developers.cloudflare.com/cloudflare-one/integrations/identity-providers/one-time-pin/).

## Installs that used WARP

Aura no longer offers a WARP hostname. After the update it deletes the WARP hostname's CNAME, its
Access application and policy, and the Gateway posture check, each only after checking that the
resource is its own. The public hostname is not touched. A token that cannot delete one of them
does not stop the install: what is left appears under **To delete by hand on Cloudflare** in the
first step, and **Done** clears the list once it is gone. An older token keeps the Zero Trust
permission for this; a new one needs none.

## Network boundaries

No LAN, Docker, Postgres, Garage-admin, MCP or CIDR route is published. Cloudflared joins only
the dedicated egress-capable tunnel network, with Caddy bridging to the application network.
That network is not a data-plane barrier: Docker's rules for ports published in `compose.yaml`
admit traffic from any bridge, and those ports were reachable from it when measured on Docker
Desktop (finding F-1 of the
[2026-09-21 review](superpowers/verification/2026-09-21-cloudflare-remote-access-review.md)).
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
  resources and encrypted PostgreSQL settings. Pressing **Next** on *Domain* turns it on again
  with the saved credentials.
- **Replace API token** in *Cloudflare* verifies and stores a new scoped API credential without
  changing the enabled state or the address. It does not rotate the connector credential.
- **Change the domain, label or account** from its own step. Aura first checks that the new zone
  can be had: if it cannot, it refuses and the old address keeps serving. Otherwise it stops the
  connector, deletes what it owns and builds the new address. A new domain still pending at the
  registrar means no address at all until Cloudflare activates its zone. If the old account is
  out of the new token's reach, the wizard lists what stayed there under **To delete by hand on
  Cloudflare**.
- **Rotate connector token** in the Cloudflare Dashboard using the
  [documented action](https://developers.cloudflare.com/tunnel/reference/tunnel-tokens/), then
  click **Refresh Dashboard-rotated token** in *Connection*. Aura retrieves and encrypts the new
  current token and hands it to the supervisor. There is no documented public rotation mutation;
  refresh is not revocation. Record handoff interruptions rather than assuming zero downtime.
- **Delete remote access** requires the exact current public hostname. It removes only resources
  whose persisted IDs and ownership markers agree. The zone, unrelated DNS and account-wide
  providers reused from other applications remain. Failed deletion preserves IDs for retry;
  fixing a credential resumes deletion. *Domain* then opens with the old address, ready to save
  again.

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
| Domain active in another account | Use a domain of this account, or the account that holds it; the old address keeps serving meanwhile. |
| Access loop | Correct hostname, active Aura email, OTP mailbox and browser cookies. |
| Connecting after login | Confirm from the public cockpit after both logins; do not spoof the ingress marker from a direct client. If the tunnel is not ready yet, wait a few seconds and confirm again. |
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

Serve the frontend on that origin first (`npm run dev -- --host 127.0.0.1`), with a running Aura
behind it: the suite signs in for real, while the remote access API responses are controlled
browser fixtures. The 390px Italian case measures page/panel scroll widths, actual control
dimensions and clipped labels.

The live runner requires an attended Linux desktop connected to a **dedicated disposable Ubuntu
appliance**, a registered test domain already added as a **pending** Cloudflare zone, an unrelated
pre-seeded TXT record, mailbox access, a funded configured chat model and working Garage/Studio.
A pending zone is necessary to measure restart/resume at nameserver waiting. Start by installing
that appliance with the generated self-extracting installer; finish Authula setup but leave Remote
Access pristine. Docker must address only that dedicated appliance. `AURA_E2E_APPLIANCE_DIR` names
its Compose directory.

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
AURA_E2E_ADMIN_EMAIL
AURA_E2E_DENIED_EMAIL
AURA_E2E_ORIGIN
AURA_E2E_APPLIANCE_DIR
AURA_E2E_UNRELATED_TXT_NAME
```

Run `bash scripts/cloudflare_tunnel_live_e2e.sh`. Missing prerequisites are fatal when opted in;
without opt-in it exits 2 with BLOCKED, not a green skipped acceptance. CI cannot provide the
attended OTP/Dashboard steps and is refused. The browser is headed and all traces, videos
and screenshots are disabled before credentials enter it. Do not enable debug/network logging.

The runner uses unique `aura-e2e-<run>` labels. It registers cleanup before configuration and
deletes only through Aura's guarded exact-host endpoint, never a direct Cloudflare DELETE. It
compares before/after Cloudflare inventories and confirms the zone/TXT remain. A forced process
kill can prevent cleanup: the failure message names the exact host; reopen the direct cockpit
and retry deletion there. Never “clean up” by deleting an entire zone or any name match.

Interactive checkpoints supply OTP, image-edit actions and Dashboard rotation; the
subsequent network/state assertions determine success. Attachments contain only non-secret
measurements, including continuous health samples during rotation. A failure or missing checkpoint
leaves acceptance incomplete. No live-suite result is recorded; the 2026-09-21 security and
release review linked above lists the thirteen live assertions still unrun.
