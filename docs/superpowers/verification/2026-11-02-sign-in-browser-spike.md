# Sign-in browser spike — 2026-11-02

Status: **S1–S3 passed in the approved dedicated-origin branch, with Edge.**
The original opaque-sandbox branch failed and was superseded after operator approval.
This accepts the spike, not the whole slice. No product code changed.

Plan: [sign-in browser](../plans/2026-11-02-sign-in-browser.md).
Spec: [§4.3 and S1–S3](../specs/2026-11-02-sign-in-browser-design.md).

## Rig

- Lab VM `192.168.101.158`, existing appliance untouched. Three throwaway containers,
  labelled `aura.signin.spike=2026-11-02`: sandbox sidecar/netns owner, egress sidecar,
  Chromium desktop. No published ports, host network, Docker socket or host bind mounts.
- Desktop: `lscr.io/linuxserver/chromium@sha256:b147169cc5ff21c62df977f314598537eecf566591c13ad0726b1e5cbd0d01ca`.
- Sandbox image ID: `sha256:22e037c60a5b16c7fd61d5b12d9984980f4897defd81615636db27a229665821`.
- Egress image ID: `sha256:feff6487428ce271303a7fb23e5765f0d955c49ae0161d523d02f35fc2a2a461`.
- The floor is the exact `AURA_EGRESS_FLOOR_RULESET` read from the live identity's
  egress container, whose builder is `internal/sandbox/usersandbox/egress.go`.
  It was applied to the new netns before creating Chromium. Public HTTPS returned
  200; a private-bridge HTTP dial timed out; nftables showed private-range DROP hits.
- Host config: Docker defaults, `ShmSize=1 GiB`, `Memory=1536 MiB`, `PidsLimit=2048`,
  no `--init`, `NetworkMode=container:aura-signin-spike`. No added desktop capabilities.
- Environment: both `HARDEN_*` switches true, file transfers `none`, sharing `false`,
  `SUBFOLDER=/sign-in/spike/`, initial URL the loopback listener's `/start`. No CDP,
  agent-browser control, stealth switches or persistent user profile.
- **The image declares a `/config` volume.** Docker created an anonymous volume
  even without `-v`. Teardown must remove associated volumes explicitly (`docker rm -v`);
  merely omitting mounts in the create request does not make the profile ephemeral.
- Scratch rig: `D:/tmp/signin-spike`, remote `/tmp/aura-signin-spike`. Go HTTPS
  reverse proxy binds VM loopback, reached through SSH local forwards. Each upstream
  connection runs `docker exec -i ... node relay.mjs` to netns loopback port 3000.
  Authentication uses a synthetic `__Host-` Secure/HttpOnly/SameSite=Strict cookie
  matching the cockpit's cookie attributes; no real cockpit cookie is used or forwarded.
  The mock governance route returns synthetic data, not appliance data.

## S1 — passed

At 13:14 UTC, Canva DCR returned **201** with the exact redirect
`http://127.0.0.1:43121/aura/mcp/oauth/callback`; the listener and consent-page launch
ran behind the floor. In the refreshed attempt, the operator completed Canva consent:
13:41:17 UTC callback state matched, 13:41:18 token **200** with a refresh token,
13:41:19 `tools/list` **200, 49 tools**. The operator confirmed that the spike worked
with Canva. These tokens were temporary and did not update the installed Aura grant.
The spike keeps authorization codes and tokens in memory only and logs no values.
That first consent attempt expired at 13:27 UTC without a callback. It was refreshed
at 13:31 UTC (DCR 201, listener ready, desktop restarted on `/start`) before the
operator's test. This timeout establishes no provider refusal.

## S2 — failed branch, measured fallback

Measured with desktop Google Chrome through Playwright on Windows. No automation
drove or inspected the remote sign-in page; the probes inspected the outer Selkies
client's browser APIs, requests and errors.

1. **As designed:** outer iframe `sandbox="allow-scripts"`, proxy CSP
   `sandbox allow-scripts`, cookie required before dialing. Initial navigation
   carried the synthetic cookie and returned the Selkies HTML in 196 ms. Both
   module/CSS subrequests had `Origin: null`, **no cookie**, and were refused before
   an exec. The frame stayed blank after 15 seconds. Its origin was `null`, and
   `document.cookie` and `localStorage` both threw `SecurityError`.
2. **Diagnostic only:** on the loopback-only rig, allow requests without the cookie
   and return CORS `*` for the proxied resources. The assets loaded (six execs), but
   Selkies still threw an uncaught **SecurityError reading localStorage** from
   `selkies-core-BGbb3Bx7.js`, and the page stayed blank. **A random path credential
   alone cannot repair this sandbox branch.** These relaxations are not product code.
3. `fetch('/api/governance/mcp')` from the opaque frame failed with `TypeError`;
   the mock endpoint saw no cookie. No WebSocket was attempted in either opaque
   run, so its `Origin` cannot be claimed measured there.
4. **Separate-origin diagnostic:** a second loopback TLS listener on `:9448`,
   without the opaque sandbox, served the same desktop through the same exec dialer.
   Selkies initialized its video/input pipeline and opened a WebSocket. Its upgrade
   carried `Origin: https://127.0.0.1:9448` and the synthetic cookie. Initial HTML
   took 231 ms; the client used nine exec connections. Storage was available. The
   probe's window was about 16.5 seconds, **not** the required five minutes.
5. From the separate origin, a credentialed fetch to the mock cockpit at `:9447`
   was unreadable due to CORS, but the request **did carry the Strict cookie**.
   A separate port is another origin, not another site. Read isolation alone is
   insufficient proof of write isolation; a dedicated-port implementation must
   reject cross-origin cockpit writes before a handler performs a side effect.

The operator subsequently loaded the separate-origin desktop inside the trusted
host's `sandbox="allow-scripts allow-same-origin"` iframe. Both pages returned 200
and retained separate real origins with usable storage. One observed WebSocket
remained open from 13:50:49 UTC until the rig was stopped after 13:58 UTC. This is
connection-lifetime evidence, not a measurement of uninterrupted visible video.
The reported 404 was inside remote Chromium's Canva page, not the outer desktop
or host. Its precise URL/cause was not measured; the successful OAuth exchange is
independent evidence and does not explain that page.

Explicit clipboard and hardening feedback, and a joint five-minute usability
acceptance, remained pending at that checkpoint. The timing above measures initial HTTP
responses, not time to first usable remote frame.

## S3 — partial

Both harden switches ran. As desktop user `abc`, attempts to execute `sudo -n true`
and `xterm -e true` returned **Permission denied**. `sudo`, `xterm` and `xdg-open`
had mode `----------`. Selkies' settings confirmed commands, printing, files and
apps disabled. A single memory sample was 766.6 MiB of the 1.5 GiB cap, not a peak.
The successful consent above ran with both harden switches on. Explicit human
right-click/file-transfer attempts remained pending. Hardening removes window-manager
menus/helpers; ordinary Chromium page context menus are not claimed removed.

## Combined operator run

The first rig was removed at 13:58 UTC: three containers, the anonymous `/config`
volume and the pulled Chromium image were independently checked absent. The operator
then requested one combined session rather than fragmented checks. The same pinned
image and real floor were recreated, with native frame/WebSocket origins, response
CSP on the dedicated proxy and standard-library write protection on the mock cockpit.
At 14:07 UTC DCR returned 201 and the rig became ready. A new controlled page after
consent offers mouse, typing and paste checks; its telemetry records only the three
event names, never typed/pasted content, passwords, codes or token values.
The callback redirects to that query-free page and keeps the listener alive during
the five-minute check. In the combined run: 14:12:53 UTC callback state matched and
token exchange returned 200 with refresh; 14:12:54 `tools/list` returned 200 and 49
tools; mouse, typing and paste events arrived at 14:13:08, 14:13:17 and 14:13:26.
The operator's screenshot also confirms the changed button and populated test fields.
The operator reports it works in Edge and explicitly confirmed the complete result:
“Sì, prova completa riuscita” after more than five minutes, with no freezes/drops
and no available terminal or file-transfer controls. The connection remained open
through the stability window. The automatic interaction events and screenshot
corroborate the mouse, keyboard and clipboard checks.

The combined page initially failed in the operator's other browser. A reproduction
using normal certificate validation (only the trusted parent response intercepted)
returned `ERR_CERT_AUTHORITY_INVALID` for the dedicated iframe. Earlier Playwright
contexts used `ignoreHTTPSErrors`, which hid this setup issue. Both origins need
trusted certificates or an explicit browser exception; this is not evidence of
Chrome lacking the required client APIs. Product acceptance must cover certificate
onboarding for both origins, with no unconditional TLS-ignore treated as proof.

Further automated results on this same live rig:

- `sudo -n true` and `xterm -e true` as `abc`: Permission denied.
- A real upload POST with `X-Upload-Path: aura-signin-fixture.txt` and fixture bytes:
  403, `uploads disabled`. File listing/download endpoint: 403, `downloads disabled`.
- A cross-origin simple POST from the desktop origin: 403. The mock cockpit's
  deny handler records `cookie=true`, `fetch_site="same-site"`; the mutating handler
  is not reached. The same-origin POST control: 200. The browser probe's request
  header snapshot omitted its cookie, so server-side evidence is authoritative.
- Public HTTPS: 200. Private bridge dial: timeout behind the real floor.
- Kernel `memory.peak`: 1,029,378,048 bytes at 14:15 UTC. This is this VM's peak
  so far, not S6's smallest-install measurement.
- HTTP loopback parent to HTTPS dedicated iframe: the parent is a secure context,
  but the frame receives no Strict cookie and answers 401. Same scheme as well as
  hostname is required for this cookie-authenticated embedding.

The proxy probes also returned: no cookie 404; desktop requested on cockpit origin
404; authenticated desktop on dedicated origin 200 with the intended sandbox and
frame-ancestor CSP; foreign WebSocket Origin 403; foreign-origin write 403; same-origin
write control 200. These probe requests did not drive the remote sign-in browser.

Scope: the identity store and cockpit API here are synthetic fixtures. This proves
the browser/transport/origin branch on the lab VM, not Aura's eventual identity
authorization wiring, release packaging, other providers, small-install limits or
Google Desktop-client refresh. Those stay in the later tasks' own gates.

## Final teardown

At 14:22:14 UTC the combined rig's cleanup ran and exited 0. All three labelled
containers, the new anonymous `/config` volume and the pulled Chromium digest were
checked absent. Both Go proxy processes were stopped. The remote scratch directory
was removed after copying the non-secret evidence to the local scratchpad. The
appliance services, floor and updater configuration were not changed.

## Amendment approved by the operator

After requesting and receiving the repository inventory below, the operator chose
“Use the dedicated-origin fallback” on 2026-11-02. Use §4.3's dedicated-origin branch;
keep the exec transport and existing floor.
Retain the paste-back for installs whose tunnel cannot reach that origin. Do not
remove the opaque sandbox on the cockpit origin or add `allow-same-origin` there.

The spec and Tasks 5–7 now record the dedicated-listener/authentication contract:
reuse Caddy's `:8444` keyhole and existing configuration, same-host Strict cookie
authentication and ownership before any exec, refusal of desktop requests on the
cockpit origin, native frame/WebSocket origin controls, explicit per-attempt launch
options and a regression proving cross-origin cockpit writes are refused. The
standard library already provides `http.CrossOriginProtection`; the repository
currently uses it through Authula for auth routes, not as the whole cockpit's
write boundary. Inventory and documentation should guide reuse rather than a
new custom CSRF protocol.

Cleanup and the remaining live measurements will be recorded before Task 1 closes.

## Repository inventory requested by the operator

Read the full `linuxserver/docker-chromium` README and Dockerfile at
`a42c24fc3671b0fa5a3cc299d5b9e9e896e01f59` (also the running image's revision),
the baseimage README, Nginx template and init scripts at
`f8b8473f5e6bb3abfdf9b06a0e505cf4a65812ca`, and the Selkies 2.1.0 settings,
secure-mode documentation and shared client source at
`6ee68a7a815956e6745e916741f13335e0b3ff53`. The running container reports Selkies 2.1.0.

- Native `SUBFOLDER` and HTTP port 3000 are the documented proxy integration;
  HTTPS belongs at the external proxy. The rig uses both. No unconfined seccomp,
  extra desktop capability or privileged mode was needed.
- Native embedding controls are `SELKIES_FRAME_ANCESTORS` (the embedding portal)
  and `SELKIES_ALLOWED_ORIGINS` (the desktop client's WebSocket origin). These
  authorize origins; they do not change the browser's opaque-sandbox storage rules.
- Native secure mode exists: `SELKIES_MASTER_TOKEN`, `/api/tokens` or
  `SELKIES_TOKENS_FILE`, and a client token in the URL fragment/query. The master
  stays on the server. This is an existing authentication facility to consider
  before inventing token plumbing; it still needs a usable client origin and
  does not replace Aura's requirement to check ownership before any exec dial.
- Both shipped dashboards use the shared client. Its `determineStreamingMode()`
  **unconditionally** reads `localStorage` before selecting a transport, even if
  a runtime transport is injected. The settings inventory has no persistence-off
  option that avoids that access. The measured crash follows from this code;
  changing WebSocket origin settings or using native tokens does not remove it.
- Hardening is already native. `HARDEN_DESKTOP` disables helpers, sudo and
  terminals; `HARDEN_OPENBOX` also configures labwc on the Wayland default. The
  existing flags should be used, keeping clipboard enabled for password-manager paste.

Source references: [Chromium README](https://github.com/linuxserver/docker-chromium/blob/a42c24fc3671b0fa5a3cc299d5b9e9e896e01f59/README.md),
[native secure mode](https://github.com/selkies-project/selkies/blob/6ee68a7a815956e6745e916741f13335e0b3ff53/docs/secure-mode.md),
[storage access](https://github.com/selkies-project/selkies/blob/6ee68a7a815956e6745e916741f13335e0b3ff53/addons/selkies-web-core/selkies-core.js#L117).

The operator approved the dedicated-origin choice after this source inventory.
No native token broker or modified Selkies client is introduced by that choice.

## Documentation consulted

- [LinuxServer Chromium](https://docs.linuxserver.io/images/docker-chromium/).
- [Selkies hardening](https://docs.linuxserver.io/selkies/user-guide/security/).
- [Selkies configuration](https://docs.linuxserver.io/selkies/user-guide/configuration/).
- [Selkies reverse proxy](https://docs.linuxserver.io/selkies/user-guide/reverse-proxy/).
- [Go ReverseProxy](https://pkg.go.dev/net/http/httputil#ReverseProxy).
- [Go CrossOriginProtection](https://pkg.go.dev/net/http#CrossOriginProtection).

## 2026-11-03 — MCP integration checkpoints (S7 remains open)

Measured locally in WSL with real Docker and CI=1:

- The actual owner-bound DELETE route cancelled the MCP attempt and removed all three
  labelled containers: `TestClosingTheSignInPageEndsTheFlowAndItsContainers`, 6.662 s.
  Flows.Cancel removes the flow; the card now handles its 404 as an ended sign-in.
- Race suites: agui 56.181 s, browserlogin 1.230 s, mcp 31.942 s, usersandbox 1.917 s,
  cmd/aura 113.681 s. Image download ownership, shared progress, request/root cancellation,
  retry and low disk passed. Native SDK CIMD fallback still packs the container's loopback
  callback into relay state; tools/list succeeds.
- UI: card/image gate 14/14, sign-in page 6/6. Typecheck, type-aware lint and knip passed;
  web build completed in 46.02 s, with the existing chunk-size advisory.
- CI fix `97b3409ef` exports AURA_SIGNIN_BROWSER_IMAGE in the Docker job and pulls it
  before the tier. All five sign-in Docker lifecycle cases also passed with the shell
  override unset (20.742 s), using production config defaults. Published to both remotes
  with all pre-push gates passing; this does not assert remote CI completion.

These checkpoints do **not** close S7's operator sign-ins for Linear, Notion and a
pre-registered GitHub client, or Task 6's certificate onboarding. The earlier Edge/Canva
acceptance remains evidence for the spike only.

Slack's [ordinary OAuth flow](https://docs.slack.dev/authentication/installing-with-oauth/)
requires HTTPS redirect URIs. Its [PKCE documentation](https://docs.slack.dev/authentication/using-pkce/)
describes localhost desktop redirects for apps opted into PKCE and prohibits bot scopes
for them. Opting in changes the app to a public client and requires Slack support to undo.
This does not establish literal 127.0.0.1 support for a particular app. No live Slack
sign-in or app-setting change was performed.

## 2026-11-03 — Linear and Notion live acceptance

The appliance ran `dd053799714beb60515e9976cd110a2d89ad877a`. Its
[CI run](https://github.com/chetto1983/Aura/actions/runs/41615149472) and the
[edge publisher](https://github.com/chetto1983/Aura/actions/runs/41615149513)
were green. The operator performed the private-browser sign-ins in Edge and
reported “linear ok” and “anche notion OK”. The assistant did not inspect or
control the private browser.

Backend metadata and native `aura mcp tools <name> --json` established:

| Connector | Endpoint | Grant saved (UTC) | Access expiry (UTC) | Tools listed |
|---|---|---|---|---|
| `signin-linear` | `https://mcp.linear.app/mcp` | 2026-11-03 11:15:36 | 2026-11-04 11:10:36 | 38 |
| `signin-notion` | `https://mcp.notion.com/mcp` | 2026-11-03 11:16:40 | 2026-11-03 19:16:40 | 72 |

Linear used the explicit `read` OAuth scope. Only tool discovery was performed;
no provider tool was called. At 11:21:22 UTC, no running or stopped container
carried the `aura.signin` label, and both Aura and Caddy were healthy. The saved
grant metadata was rechecked at 11:21:34 UTC. Existing connectors were preserved.

These results close S7 after the operator explicitly excluded the GitHub trial
on 2026-11-03 (“github non mi interessa”). GitHub remains unmeasured; no GitHub
connector or app was created. The specific untrusted-certificate
onboarding case in Task 6 remains pending operator clarification: successful
sign-ins alone do not show whether the dedicated origin was already trusted.
No claim is made here about Google Desktop consent, next-day refresh, minimum-host
resource limits or the plan's final whole-range gates.
