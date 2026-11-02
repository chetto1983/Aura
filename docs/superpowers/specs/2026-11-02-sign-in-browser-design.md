# Sign-in in a real browser (Slice 1) — design

Date: 2026-11-02
Status: draft, for the operator's approval, one question open in §10. No product code before that. A read-only builder
check ran on this document, and its five questions are settled in §4.3 to §4.7 and §10. Each claim is marked **measured**,
**read** or **assumed**, and the assumed ones are the gates of §6.

## 1. Why and what

Aura runs on a server, and an OAuth sign-in assumes a browser on the same machine as the app. So Canva cannot be
connected from a LAN address, and Google passes through a page on Cloudflare. The operator asked on 2026-11-02 that
logins work "come una app desktop", for mail and for MCPs, Canva included, and that the Cloudflare page goes.

**The slice:** when a person connects Canva, another OAuth MCP, or a Google account, the cockpit shows a real desktop
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
- A person connects Google with no third-party page in the path.
- One button on the card, and the card tells the truth afterwards, a dead grant included.
- The Cloudflare `google/callback` and `mcp/callback` pages go.

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
flow ends or at the flow's 10-minute expiry. There is no volume, so the profile goes with it and nothing is left on disk
to wipe.

- **Image:** `lscr.io/linuxserver/chromium`, pinned by digest in `AURA_SIGNIN_BROWSER_IMAGE`, and not in the appliance
  payload. It is pulled on first use, the cockpit says so with its size and progress, and the attempt is refused
  cleanly under 8 GB of free disk.
- **Environment:** `HARDEN_DESKTOP=true`, `HARDEN_OPENBOX=true`, `SELKIES_FILE_TRANSFERS=none`,
  `SELKIES_ENABLE_SHARING=false`, `SUBFOLDER=/sign-in/<session>/`, `CHROME_CLI=<consent URL>`, `PELORUS` unset.
- **Limits:** one memory cap, a `--shm-size` of 1 GB, a slot from `AdmitBrowser` like any Chrome on the host, no published
  port, no Docker socket, and one running sign-in per identity.
- **Clipboard stays on,** because people paste a password from their manager. It is their own box.

### 4.2 The callback

A listener in a small sidecar on the container's netns, from the sandbox image, takes `http://127.0.0.1:43121/callback`.
The port can be fixed because the netns belongs to this flow, which also keeps the DCR client's redirect stable. It
answers the page with a plain "you can close this", hands query values to Aura over its exec stream, and exits. This is
`chatgpt-browser.mjs`'s listener, so the code is moved and shared rather than copied.

`chatgptplan.BrowserLogin` already is this state machine behind `BrowserLauncher` and `BrowserSession`. It moves to a
package of its own, with the session prefix as a parameter, and ChatGPT keeps its current browser. Aura's own
`chatgpt-` sessions stay as they are.

### 4.3 The stream

The cockpit serves `/sign-in/<session>/` as a reverse proxy to port 3000 of that container, behind the cockpit session,
with the same ownership check as a `chatgpt-` session and an `Origin` check on the upgrade. The connection runs over an
exec stream, a `net.Conn` on `socat - TCP:127.0.0.1:3000` given to `httputil.ReverseProxy` as its dialer, because the
floor leaves no network path in. Nothing is published, and the proxy is the only authentication, so it must refuse before
it dials. Whether an exec stream carries the video well enough is gate S2, and the fallback is the same proxy over a
dedicated bridge with one rule for port 3000.

**The page must not act as the person.** It is a third party's HTML and JavaScript, and whoever takes over the container
controls what port 3000 serves, while the cockpit's origin holds a session that may be an admin's. So every proxied
response carries `Content-Security-Policy: sandbox allow-scripts`, which gives the page an opaque origin with no cookies
and no reach into the cockpit's API. If Selkies cannot run that way, the page moves to a dedicated port with its own
origin, as the MCP views have on `:8444`. That port does not pass through the Cloudflare tunnel, so such an install would
fall back to §4.7 when reached through it. S2 decides.

A cockpit on plain `http://` at a LAN address has no secure context, so the card goes straight to §4.7 instead of showing a
dead page. `http://127.0.0.1` is a secure context and works.

### 4.4 MCP

**Which servers:** the operator's to decide, see §10. Until then this spec assumes the narrow answer. A server uses the
container when its configuration says `MCP_OAUTH_BROWSER=aura`, which Canva's recipe sets. Every other server keeps the
person's own browser, including those with a pre-registered client, whose provider wants the redirect exactly as
registered (`cmd/aura/mcp_login.go`), and Linear and Notion, which work today.

For those that use it, `Flows.Start`, DCR, PKCE, `CompleteAuthorization` and the grant store stay as they are, since the
only change is where the redirect lands. The page closes itself when the flow ends, and the card re-reads its state.
`callbackFor(origin)` stays for everything else and for `aura mcp login`.

**A dead grant reads as dead.** `MCPAuthorization` gains `Expired`, set when a refresh answers `invalid_grant`, and the
card offers to authorize again.

### 4.5 Google

The admin panel asks for a client of type Desktop app. The PIM's `StartGoogleOAuth` takes the loopback `redirectUri` per
request and uses it for the consent and for the exchange, and Aura forwards the code to the PIM's existing callback.

Accounts already connected keep refreshing with the client they were created with, as Microsoft's do, so nothing breaks
when the admin swaps the app. A sign-in with a Web client left in place fails at Google with `redirect_uri_mismatch`, and
the panel's text says which type to create.

An account that must sign in again, after a logout or a dead refresh token, would stay on its old Web client, because
`StartGoogleOAuth` reads the client from the account. So finishing a sign-in first rewrites the account's client with the
admin's current app, through the same rewrite `injectPIMProviderApp` does on create. The account keeps its id, and so
its watch.

Then `GoogleOAuthRelay.cs`, `GoogleOAuthRelayUrl`, `PIMGoogleRelayRedirectURI`, `returnBase`, `pimReturnOrigin` and the
public `/admin/auth/google/callback` route go from the code, in this slice. Leaving them would be dark code.

### 4.6 The Cloudflare site

The relay's address is compiled into released binaries, so the pages cannot go when this ships. They go in a job of their
own, once every known install runs a release with this slice. Until then `mcp/client-metadata.json` names both redirects.
That document must stay on a public https address for as long as a server with no DCR is supported, because the client id
is that URL. So the pages go and the site stays as one file, unless gate S5 says otherwise.

### 4.7 Without the container

The operator's fallback, 2026-11-02, for when the image cannot run: a low disk, a blocked pull, a cockpit on plain `http://`
at a LAN address, a site that stops a person in the container.

The card opens the consent page in the person's own browser, with the same loopback redirect. That browser lands on
`http://127.0.0.1:43121/callback?...`, which does not load. They copy the address into a field on the card, and Aura
reads `code` and `state` from it, checks `state` against the flow, and goes on exactly as if the listener had taken it. It
covers MCP and Google, needs no relay and no image, and is what keeps Google reachable once the relay is gone. A person's
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
| **S4** | A new Google Desktop client: consent in the container, loopback redirect, exchange through the PIM, and a refresh a day later |
| **S5** | ElevenLabs with the client-metadata document naming a loopback redirect, if an account is available. If not, `mcp/callback` stays and the verification file says why |
| **S6** | Pull time and peak memory on a small install. The 8 GB floor and the memory cap are guesses until then |

## 7. Order of landing

1. S1 to S3, as a throwaway spike with no product code. If S2 fails, §4.3 takes its fallback before anything else.
2. The dead-grant state, on its own, because it is a bug today with or without this slice.
3. The paste-back of §4.7, which works with no container and is Canva's first way in.
4. The login state machine moved, with ChatGPT's tests unchanged.
5. The container, the callback sidecar and the proxy.
6. MCP on it.
7. S4, then Google, in the fork and in Aura, with the relay code removed once the VM has run a full sign-in.

The Cloudflare pages are a later job (§4.6). Plan: `superpowers:writing-plans` after approval. Risk tier high: this adds an
authenticated path to a privileged GUI.

## 8. The engineer's calls, for the operator to overturn

- **No profile kept.** Connecting Canva and then Google means signing in to Google twice, once per connection. A kept
  profile would make a stored live session Aura's to guard.
- **Pulled on first use,** so an install that never connects Canva or Google never pays the 4.7 GB.
- **Same image for everyone,** with no slimmer build, because this is the image that was measured.
- **Fixed port 43121,** since one netns holds one flow.
- **ChatGPT stays on its box.** It works today, and moving it would add risk with no gain.
- **Exec stream first,** because it keeps the floor untouched.

## 9. What this does not establish

- That Cloudflare or Google will keep treating this browser as they did on 2026-11-02. Nothing here controls that, and
  a site that stops admitting it stops this path. The fallback is `callbackFor` on a loopback cockpit.
- That it works from an appliance on a residential or a data-centre address. Only the Sacchi network and this PC ran.
- Any site other than Canva and Google.
- That a person on a phone can drive it. Selkies has a touch mode, and nobody has tried it.
- Licence: the image is GPL-3.0 and runs as a separate, unmodified container, which is an engineer's reading, not advice.

## 10. Open for the operator

**Which servers go through the container?** The narrow answer, §4.4: Canva and Google, and any server set to it. The
others keep the person's own browser, which works and costs nothing. The wide answer is every OAuth server, which is one
way of doing things and the closest to "come una app desktop", but it means a 1.17 GB download and a fresh login for
Linear and Notion, and it needs an exception for pre-registered clients anyway. This spec recommends the narrow one.
