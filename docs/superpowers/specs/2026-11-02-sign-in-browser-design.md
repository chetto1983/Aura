# Sign-in in a real browser (Slice 1) — design

Date: 2026-11-02
Status: approved by the operator on 2026-11-02, with §8's two exceptions. A read-only builder check ran on this document,
and its five questions are settled in §4.3 to §4.7 and §10. Each claim is marked **measured**, **read** or **assumed**,
and the assumed ones are the gates of §6.

## 1. Why and what

Aura runs on a server, and an OAuth sign-in assumes a browser on the same machine as the app. So Canva cannot be
connected from a LAN address, and Google passes through a page on Cloudflare. The operator asked on 2026-11-02 that
logins work "come una app desktop", for mail and for MCPs, Canva included, and that the Cloudflare page goes.

**Scope correction, 2026-11-03:** the operator confirmed that Google access through
`https://chetto1983-aura-connect.pages.dev/google/callback/` currently works and keeps that flow.
Google Desktop migration and relay removal are withdrawn; the existing Google path remains in use.

**The slice:** when a person connects Canva or another OAuth MCP, the cockpit shows a real desktop
Chromium that runs in that person's own container. They sign in there as on a laptop, the redirect lands on `127.0.0.1`
inside that container, and Aura finishes the flow.

Slice 2, the agent's own browser on the same image, is decided later and is not designed here. In this slice the
agent stays on the current browser and has no path to the sign-in one.

## 2. What was found

| Fact | State |
|---|---|
| Canva's `/authorize` accepts only `localhost` and `127.0.0.1` redirects, and anything else answers `Invalid redirect URI` | measured, 2026-09-23, again 2026-11-02 |
| In the current browser box, `Continue with Google` on Canva stopped on Cloudflare's check twice of two, and the operator's click did not pass it | measured, lab VM |
| In `lscr.io/linuxserver/chromium`, an ordinary desktop Chromium streamed by Selkies, the same sign-in passed with two-step verification | measured, PC and lab VM from its own network |
| There, MCP consent ran end to end: DCR, `/authorize`, redirect to `127.0.0.1:43121/callback`, token 200 with refresh, 49 tools | measured, PC only |
| Whether Cloudflare showed the check at all in those runs | unknown |
| A Google client of type Desktop app redirects to loopback; the installs' Web clients refuse it with `redirect_uri_mismatch` | measured for the refusal; the Desktop consent and code exchange were never run |
| A person's Google login inside the current box works | measured, 2026-10-12 |
| Microsoft signs in by device code under `consumers`, with no browser in Aura | measured, 2026-11-02, so Microsoft is not in this slice |
| The box's egress floor drops RFC1918 and the shared bridge, with no rule for established traffic | read, `internal/sandbox/usersandbox/egress.go`. Aura cannot simply connect in |
| The current live view reaches a box only through `docker exec` streams | read, `internal/agui/browser_live.go`, `docker/aura-sandbox/browser-relay.mjs` |
| A `chatgpt-` session is private to its owner and refused to the agent; the sign-in state machine is generic apart from that prefix | read, `internal/chatgptplan/browser.go` |
| The GUI gives whoever reaches it root in the container, with no authentication of its own. `HARDEN_DESKTOP` and `HARDEN_OPENBOX` turn off sudo, terminals, helpers, file transfer and the commands panel | read, docs.linuxserver.io/selkies |
| Selkies wants HTTPS or another secure context, websockets, unbuffered responses, and `SUBFOLDER` to serve under a prefix | read, same docs |
| The image is 4.7 GB on disk, 1.17 GB to download, and holds about 650 to 900 MB of memory with one site open | measured, PC and VM |
| The Canva card says "Autorizzato" while its grant is dead: `AuthorizationState` looks only at whether a row exists | measured on the VM, read in `cmd/aura/mcp_oauth_provider.go` |
| ElevenLabs has no DCR, so it needs Aura's public client-metadata document | read, `internal/mcp/oauth_cimd.go`. Whether it takes a loopback redirect is not known |

## 3. Goals and anti-goals

- A person connects Canva from any address the cockpit is served on, with no tunnel and no second machine.
- Google keeps its working Web-client flow through the existing Cloudflare callback, per the 2026-11-03 scope correction.
- One button on the card, and the card tells the truth afterwards, a dead grant included.
- Removal of `mcp/callback` is a later job gated by S5 and installed releases; `google/callback` stays.

**Anti-goals:**
- Nothing here gets a check passed. It is a plain browser with a person at the mouse. No stealth flags, no solver, and
  no change made in order to look less automated. If a site stops a person there, that is the outcome.
- No model ever drives or reads this browser, and no password or code passes through Aura. The person types them
  inside the stream.
- No login left behind by accident: the slice keeps no profile (§4.1).
- Not a general remote desktop. It opens for one sign-in and closes.

## 4. Shape

### 4.1 One container for one sign-in

Pressing the button creates a container for that flow, on the identity's own egress floor, and it is removed when the
flow ends or at the flow's 10-minute expiry. No profile survives the flow: teardown removes its container and the
image-created anonymous profile volume together.

**Measured correction, 2026-11-02:** the image declares `VOLUME /config`, so Docker creates an anonymous profile volume
even when Aura requests no mount. Teardown must remove associated anonymous volumes together with each sign-in
container. The profile remains ephemeral; omitting a volume from the create request alone does not ensure that.
Evidence: [sign-in browser spike](../verification/2026-11-02-sign-in-browser-spike.md).

- **Image:** `lscr.io/linuxserver/chromium`, pinned by digest in `AURA_SIGNIN_BROWSER_IMAGE`, and not in the appliance
  payload. It is pulled on first use, the cockpit says so with its size and progress, and the attempt is refused
  cleanly under 8 GB of free disk.
- **Environment:** `HARDEN_DESKTOP=true`, `HARDEN_OPENBOX=true`, `SELKIES_FILE_TRANSFERS=none`,
  `SELKIES_ENABLE_SHARING=false`, `SUBFOLDER=/sign-in/<session>/`, `CHROME_CLI=<consent URL>`, `PELORUS` unset.
- **Limits:** one memory cap, a `--shm-size` of 1 GB, a slot from `AdmitBrowser` like any Chrome on the host, no published
  port, no Docker socket, and one running sign-in per identity.
- **Clipboard stays on,** because people paste a password from their manager. It is their own box.

### 4.2 The callback

A listener in a small sidecar on the container's netns, from the sandbox image, takes
`http://127.0.0.1:43121/aura/mcp/oauth/callback`. That path is `aura mcp login`'s, one of the two the deployed
`mcp/callback/relay.js` forwards (read 2026-11-02), so a server with no registration still works, through the relay into
this listener. The port can be fixed because the netns belongs to this flow, which also keeps the DCR client's redirect
stable. The sidecar owns the netns and starts first, so the floor is up before Chromium is given the consent URL. It
answers the page with a plain "you can close this", hands query values to Aura over its exec stream, and exits. This is
`chatgpt-browser.mjs`'s listener, so the code is moved and shared rather than copied.

`chatgptplan.BrowserLogin` already is this state machine behind `BrowserLauncher` and `BrowserSession`. It moves to a
package of its own, with the session prefix as a parameter, and ChatGPT keeps its current browser. Aura's own
`chatgpt-` sessions stay as they are.

### 4.3 The stream

The dedicated sign-in origin serves `/sign-in/<session>/` as a reverse proxy to port 3000 of that container, behind the cockpit session,
with the same ownership check as a `chatgpt-` session and an `Origin` check on the upgrade. The connection runs over an
exec stream in the sidecar, a `net.Conn` on a small Node pipe to `127.0.0.1:3000` given to `httputil.ReverseProxy` as its
dialer, because the floor leaves no network path in. Nothing is published, and the proxy is the only authentication, so it must refuse before
it dials. Whether an exec stream carries the video well enough is gate S2, and the fallback is the same proxy over a
dedicated bridge with one rule for port 3000.

**The page must not act as the person.** It is a third party's HTML and JavaScript, and whoever takes over the container
controls what port 3000 serves, while the cockpit's origin holds a session that may be an admin's. The operator selected
the **dedicated-origin fallback** on 2026-11-02 after S2 failed. The desktop keeps a real origin so its unmodified client
can use storage. The cockpit page frames that other origin with `sandbox="allow-scripts allow-same-origin"` and explicit
clipboard permissions; the stream must never be served on the cockpit's own origin with those privileges. The proxy
enforces the same isolation in its response CSP and restricts the allowed embedding parent. Native
`SELKIES_FRAME_ANCESTORS` and `SELKIES_ALLOWED_ORIGINS` name the cockpit parent and desktop client origins respectively;
the proxy also verifies the WebSocket Origin before dialing. Native token mode exists, but it does not replace Aura's
ownership check before an exec or repair the opaque-origin storage failure.

**Measured correction, 2026-11-03:** the native MCP Apps proxy also grants a real
origin to its view. In isolated Edge, an app framed by that unmodified proxy read
a synthetic marker from a sibling desktop sharing its origin, despite the
cockpit's different origin and the desktop sandbox. Therefore the sign-in stream
must refuse an origin reserved by `MCPSandboxOrigin` before dialing. A start that
would use that configured MCP Apps origin must use the existing explicit
own-browser flow before image preflight or admission. The default inline desktop
remains available while the MCP Apps origin is unset. This introduces no extra
listener or origin setting; co-hosting both GUIs on one origin is unsupported.

Use the existing Caddy `:8444` keyhole for the authenticated stream, retaining its refusal of unrelated routes. The
desktop origin must use the cockpit's hostname and another HTTPS port, so its Strict session cookie arrives without
cross-host credential handoff. A configured origin on another hostname, a tunnel without that port, or an unreachable
desktop origin takes §4.7. Reuse the existing sandbox-origin/port/bind settings; do not add another image or broker.
The stream's HTML, scripts and errors must never forward Cookie, Authorization or forwarded-client credentials into
the desktop. The host page alone polls the flow, cancels on close, and closes its tab.

A second port is another origin but remains the same site, so SameSite=Strict alone is insufficient for write isolation.
Wrap cockpit browser writes in the standard library's `http.CrossOriginProtection`, with regressions proving that a
credentialed form/simple POST from the desktop cannot reach a mutating handler. The desktop origin is not a trusted
write origin. No opaque-sandbox or arbitrary-origin relaxation is made on the cockpit origin.

**S2 measurement, 2026-11-02:** the opaque iframe's initial navigation carried the synthetic Strict
cookie, but its module/CSS requests did not and were refused. Allowing the resources in the throwaway rig exposed a
second failure: the unmodified Selkies 2.1.0 client unconditionally reads `localStorage` and crashes with `SecurityError`.
The same image initialized its stream on a separate TLS origin over the same exec transport. That is partial evidence
for the dedicated-origin fallback. The subsequent combined operator run in Edge passed Canva consent, mouse, keyboard,
paste, hardening and more than five minutes without freezes/drops; token and `tools/list` returned 200 with 49 tools.
A credentialed request across two ports on the same hostname still carried the Strict cookie, so write isolation also
needs a measured origin boundary. Native embedding/token controls and the exact source inventory are recorded in the
[spike file](../verification/2026-11-02-sign-in-browser-spike.md). The operator approved the dedicated-origin choice;
S1–S3 are now accepted for this branch. Self-signed certificate trust for the second origin failed in Chrome until
trusted, while Edge worked; acceptance must cover trust onboarding for both origins instead of ignoring certificate
errors unconditionally.

A cockpit on plain `http://` at a LAN address has no secure context, so the card goes straight to §4.7 instead of showing a
dead page. `http://127.0.0.1` is a secure context, but cookie-authenticated framing also requires the same scheme as the
desktop origin: measured HTTP-loopback parent → HTTPS frame withheld the Strict cookie and returned 401. With the
default HTTPS Caddy desktop listener, HTTP-loopback cockpits therefore also take §4.7; a configured same-scheme secure
loopback origin may use the container. Secure context alone is insufficient.

### 4.4 MCP

**Every OAuth server, one scheme** (operator, 2026-11-02, "per tutti, stesso schema ovunque"). The cockpit starts every
flow on the loopback redirect, in the container or through §4.7. Linear and Notion move too, and pay the download and a
fresh login like the rest. `Flows.Start`, DCR, PKCE, `CompleteAuthorization` and the grant store stay as they are, since
the only change is where the redirect lands. The page closes itself when the flow ends, and the card re-reads its state.

**Pre-registered clients** follow the same scheme. A provider wants the redirect exactly as registered
(`cmd/aura/mcp_login.go`), so the admin registers `http://127.0.0.1:43121/aura/mcp/oauth/callback`. `MCP_OAUTH_REDIRECT_URL` stays for a
client registered on another loopback address, and the listener binds the port and path it names. A provider that
refuses every loopback redirect takes an https address the admin chooses and §4.7, since the person only has to copy
the address they land on. Which providers those are is gate S7.

**What goes.** The cockpit's own callback goes with its last caller: `callbackFor`, `cockpitOrigin`,
`configuredCallbackURL`, and the public route `GET /api/governance/mcp/authorization/callback`. That also removes one
route that carries no capability. `AURA_WEB_PUBLIC_URL` goes if nothing else reads it. `aura mcp login` stays: it is
the same scheme with a person at their own terminal.

**Grants already stored keep working:** a refresh carries no redirect. Only a new consent takes the new path.

**A dead grant reads as dead.** `MCPAuthorization` gains `Expired`, set when a refresh answers `invalid_grant`, and the
card offers to authorize again.

### 4.5 Google

The operator retained the current Google flow on 2026-11-03 and confirmed that access works.
The provider app remains a Web application, with the existing Cloudflare callback and PIM exchange.
No Desktop client, new start/exchange endpoint, account-client rewrite or Google paste-back is introduced here.
The relay helpers, public callback and their configuration remain live code with real callers.
The operator's report establishes current access only; it does not measure the withdrawn Desktop/next-day-refresh trial.

### 4.6 The Cloudflare site

The Google relay stays under §4.5. The MCP relay's address is compiled into released binaries, so its page cannot go
when this ships. Its removal is a later job, once every known install runs a release with this slice and S5 allows it.
Until then `mcp/client-metadata.json` names both redirects.
That document must stay on a public https address for as long as a server with no DCR is supported, because the client id
is that URL. The site continues serving the Google relay and that metadata document, plus the MCP relay while required.

### 4.7 Without the container

The operator's fallback, 2026-11-02, for when the image cannot run: a low disk, a blocked pull, a cockpit on plain `http://`
at a LAN address, a site that stops a person in the container.

The card opens the consent page in the person's own browser, with the same loopback redirect. That browser lands on
`http://127.0.0.1:43121/aura/mcp/oauth/callback?...`, which does not load. They copy the address into a field on the card, and Aura
reads `code` and `state` from it, checks `state` against the flow, and goes on exactly as if the listener had taken it. It
covers MCP and needs no image. Google retains §4.5. A person's
own browser is the most ordinary browser there is.

## 5. Failure

| What happens | What the person sees |
|---|---|
| The image cannot be pulled, or the disk is short | The reason, with the size needed, and §4.7 on the same card. Nothing half-created remains |
| The person closes the page | The flow is cancelled, the container goes, the card is as it was |
| Ten minutes pass | The same, with "try again" |
| A site stops them | Nothing special: it is their browser. They can try another sign-in method, or §4.7 |
| Aura restarts mid-flow | The reconciler removes containers left behind by name, and the person presses the button again |
| The pasted address has the wrong `state`, or the flow has expired | The card says so, and nothing is exchanged |
| The stream cannot carry video | The page says so and offers §4.7. The gate below decides whether this ships |

## 6. Gates before code

Each one ends in a line of a dated file in `docs/superpowers/verification/`, and a failed one comes back here first.

| Gate | What it must show |
|---|---|
| **S1** | On the lab VM, with Aura's egress floor attached, Canva from `/authorize` to a 200 token and `tools/list` |
| **S2** | Through the Aura proxy on `https://`, over an exec stream: picture, mouse, keyboard and paste, five minutes with no drop, and a sign-in completed by a person, with the page under `sandbox allow-scripts` and refused when it calls the cockpit's API |
| **S3** | With both harden switches on, the same sign-in passes, and sudo, terminals and file transfer are gone when tried |
| **S4** | Withdrawn on 2026-11-03: the operator keeps the working Google Web-client relay; Desktop consent and next-day refresh remain unmeasured |
| **S5** | ElevenLabs with the client-metadata document naming a loopback redirect, if an account is available. If not, `mcp/callback` stays and the verification file says why |
| **S6** | Available-VM measurement recorded 2026-11-03: 225 s earlier cold pull, 663.56 MiB public-page desktop peak, zero OOM kills and 14.66 GB free after cleanup. Retain 1.5 GiB memory/8 GiB free disk conservatively; smaller hosts and authenticated-page peaks remain unmeasured |
| **S7** | Linear and Notion through the container on the VM. Slack's rules are read before anything is promised for it. The operator excluded the GitHub live trial on 2026-11-03 (“github non mi interessa”); no GitHub acceptance is claimed |

## 7. Order of landing

1. S1 to S3, as a throwaway spike with no product code. If S2 fails, §4.3 takes its fallback before anything else.
2. The dead-grant state, on its own, because it is a bug today with or without this slice.
3. The paste-back of §4.7, which works with no container and is Canva's first way in.
4. The login state machine moved, with ChatGPT's tests moved beside it because they read its internals, and their
   assertions unchanged.
5. The container, the callback sidecar and the proxy.
6. MCP on it, S7, and the cockpit's own callback removed.
7. Final MCP resource, lifecycle and release gates. Google migration and relay removal were withdrawn on 2026-11-03.

The Cloudflare pages are a later job (§4.6). Plan: `superpowers:writing-plans` after approval. Risk tier high: this adds an
authenticated path to a privileged GUI.

## 8. The engineer's calls, for the operator to overturn

- **No profile kept.** Connecting Canva and then another MCP means signing in again, once per connection. A kept
  profile would make a stored live session Aura's to guard.
- **Pulled on first use,** so an install that never connects an OAuth MCP server never pays the 4.7 GB.
- **Same image for everyone,** with no slimmer build, because this is the image that was measured.
- **Fixed port 43121,** since one netns holds one flow.
- **ChatGPT stays on its box until Slice 2.** It already has this shape, a browser in the person's own container and a
  loopback redirect, only on the older image. It moves when the box's browser does.
- **Microsoft stays on device code.** It needs no browser in Aura and was measured working, so "one scheme" covers what
  has a redirect.
- **Exec stream first,** because it keeps the floor untouched.

## 9. What this does not establish

- That Cloudflare or Google will keep treating this browser as they did on 2026-11-02. Nothing here controls that, and
  a site that stops admitting it stops this path. The fallback is §4.7.
- That it works from an appliance on a residential or a data-centre address. Only the Sacchi network and this PC ran.
- Additional MCP providers beyond the accepted Canva, Linear and Notion trials.
- That a person on a phone can drive it. Selkies has a touch mode, and nobody has tried it.
- Licence: the image is GPL-3.0 and runs as a separate, unmodified container, which is an engineer's reading, not advice.

## 10. Decided by the operator

**Every OAuth server goes through the container,** 2026-11-02: "per tutti, stesso schema ovunque". The spec had
recommended Canva and Google alone. The operator took the wider answer knowing its price, a 1.17 GB download and a fresh
login for Linear and Notion, in exchange for one way of signing in. §4.4 carries it.
