# Mobile client (Flutter) through the Cloudflare tunnel — design

Date: 2026-11-02
Status: draft, not ready for a plan. The operator chose the direction on 2026-11-02 (§1). Measured the
same day ([ledger](../verification/2026-11-02-mobile-client-spike.md)): the service-token path they
chose is refused by the reconciler, so §3 and §4 now propose another one, and the operator has not yet
ruled on that change. Gates T1 to T3 in §6 are owed and need a client outside the Sacchi network.

## 1. Why and what

The operator asked for an iOS and Android app for Aura reaching an install through its Cloudflare
remote-access hostname. Decisions taken on 2026-11-02:

- **A targeted client, not a copy of the cockpit.** The web cockpit is ~78k non-test TS/TSX lines
  (`git ls-files 'web/src/*.ts' 'web/src/*.tsx'`, measured 2026-11-02); duplicating it in Dart is
  out of scope. Everything not listed below stays in the web cockpit, which is already an
  installable PWA (`web/vite.config.ts`: manifest, `sw.js`).
- **Flutter**, one codebase for both platforms.
- **Public stores** (App Store and Play Store), usable by any Aura install, not one known host.
- **First release:** streaming chat, approvals and agent questions, push notifications, sending
  files, photos and voice notes to Aura from the phone.
- **Crossing Access:** the operator chose QR pairing with one Cloudflare service token per device.
  §3 records why that shape does not survive as written.

## 2. Inventory (what already exists)

| Need | Already in the tree | Gap |
|---|---|---|
| Device credential | Authula's `plugins/api-key`: hashed keys, name, expiry, enable flag, `last_requested_at`, per-key delete, `Verify` | Not enabled in Aura; needs `plugins/access-control` beside it |
| Cookie-free calls to the cockpit API | None | `agui.RequireAuth` reads the session cookie only (`internal/agui/auth.go`), on purpose (T-24-13) |
| Chat stream, approvals, questions, uploads | `/agent/run` (AG-UI SSE, `Last-Event-ID` resume at `/agent/runs/{id}/events`), `/api/approvals*`, assets | None expected; the client consumes the same API |
| Access management | `internal/cloudflareapi`, `internal/remotetunnel` | Refuses any policy or application it does not own, on purpose |
| Push fan-out | Identity-bound deliveries (`DeliverToIdentity`, `DeliverApproval`) feed Telegram/WhatsApp | No mobile push channel, no relay |

## 3. What does not cross Access

**Cloudflare Managed OAuth** ([docs](https://developers.cloudflare.com/cloudflare-one/access-controls/applications/http-apps/managed-oauth/), read 2026-11-02, not measured). Dynamically registered redirects must be `https`, `localhost` or loopback, custom schemes are not documented, and iOS universal links need every domain at build time. Access would also serve `/.well-known/oauth-authorization-server`, which Aura already serves (`cmd/aura/serve_webui_mcp_oauth.go`), and the page says not to enable it in front of an app with its own OAuth server.

**A service token per device** (measured, on the fake Cloudflare). `policy()` refuses any second policy on the application, because "an extra policy could bypass the restrictive allowlist". The first pass after one appears parks the row in `error/not_owned`, which is terminal: removing the policy does not recover it, and `SyncMembers` stops, so a deactivated person stays in the Access policy until an operator saves the configuration again. A path-scoped application on the hostname answers `hostname_taken`, and a second hostname's ingress is rewritten by `publish`. Keeping this path means weakening that rule, adding a token permission, adding device resources to ownership and teardown, and letting a device through Access with no check of who holds it. It stays the fallback if T1 fails.

## 4. Design

Two doors, each with its own credential, because they answer different questions: Access asks whether this person may reach the host at all, Aura asks who is calling.

### 4.1 Cloudflare: the person's own Access session

The app opens the hostname once in an in-app WebView, and the person completes the same OTP as in a phone browser. The app reads `CF_Authorization` from the native cookie store and sends it on every request. This needs no change on Cloudflare, in the reconciler or in the API token, the email policy stays the only rule on the application, and the cadence stays the one the docs promise: a PIN a month.

When the token expires, Access answers with a login redirect instead of JSON. The client must treat that as "reopen the WebView", never as a parse error. A certificate failure on the hostname must read as "this network may be blocking the address", because a phone on the Sacchi Wi-Fi meets exactly that: its DNS filter answers with a block page under another certificate.

Not measured: T1.

### 4.2 Aura: one API key per device

The credential is an Authula API key, not an OAuth refresh token. The lifecycle measurements rule the refresh chain out for a phone:

- it dies with its session, so signing out of the cockpit, or that cookie returning past the absolute cap, logs the phone out;
- a phone unused for 7 days is out, though the Access session lasts a month;
- a rotation whose response is lost and is retried after the 10 s grace answers `invalid_grant`, while the token never delivered stays valid, so one bad moment on cellular costs a new pairing;
- a retry within the grace, or two refreshes at once, forks the chain;
- the access token still verifies after sign-out, so a signature check alone would leave up to 15 minutes.

An API key has none of these. It survived sign-out, deleting one device left the other valid and took effect on the next check, an expired key was refused, and `Verify` averaged 430 µs against local Postgres. It does not rotate, so a stolen phone keychain is good until the key is revoked or expires.

`RequireAuth` gains one branch: a key header goes through `Verify`, the owner through the existing identity resolver, then the same `Deactivated` check a cookie passes. This is a deliberate exception to T-24-13, which forbids trusting a client's claim of identity: a key is a secret the server verifies, not a claim. `RequireCapability` is unchanged, and no CSRF token applies, since the header is not ambient. Deactivating an identity deletes its keys with `DeleteAllByOwner`.

The plugin's HTTP routes stay unreachable: unmapped to `session.auth` they answer 401 to everyone, so the cockpit calls the plugin's Go API. `Verify` is not exposed.

### 4.3 Pairing

1. **Settings > Devices > Pair a phone** mints a one-time pairing secret: 5 minute TTL, single use, bound to the identity. The QR carries `{hostname, pairing_secret}` and no Cloudflare secret.
2. The app scans it and opens the hostname in the WebView, where the person completes OTP.
3. The app posts the pairing secret, Aura mints a key named for the device with an explicit expiry, and returns it once. The app keeps it in Keychain/Keystore.

### 4.4 Chat stream

Measured from a Dart client against the VM's Caddy, the existing contract holds with no new server code: `POST /agent/run`, on a cut `GET /agent/runs/{id}/events` with `Last-Event-ID`, on a cold start `live_run_id` from the conversation, and `/threads/{id}/messages` once the run is gone. A turn continues and is persisted with no viewer. What the client has to allow for:

- **The snapshot is a normal path, not an error.** A finished run answers 404 somewhere between 180 and 270 s after it ended. A phone locked for a few minutes lands there.
- **A resume can stay silent.** With nothing to replay, the GET's headers came with the next frame, about 10 s later, so its timeout must sit above the model's silence, not at a usual HTTP value.
- **Do not design for progressive text.** On the VM's route each turn delivered all its text in one frame after 11 to 16 s. The screen needs a working state driven by `RUN_STARTED` and `STATE_DELTA`.

### 4.5 Push through a relay

APNs and FCM credentials belong to the publisher, not to each install, so a store app needs a central relay (a Cloudflare Worker, by analogy with the `aura-connect` Pages relay). The relay must carry no content: the install sends `{device push token, opaque id, ciphertext}` encrypted to a key the app generated at pairing, and the app decrypts it (iOS Notification Service Extension) or fetches the item over the tunnel. Mobile push becomes a new channel alongside Telegram/WhatsApp on the identity-bound deliveries.

### 4.6 The app

Four screens: conversation list, chat, approvals and questions, pairing. Plus the OS share sheet for uploads. Where the Flutter app lives and its CI are decided in the plan.

## 5. Open questions

- OQ1: how long a captured `CF_Authorization` lasts in practice, and what Access answers a non-browser client when it lapses.
- OQ2: uploads. Presigned PUTs go to the public hostname, so they cross Access too and must carry its cookie. Not tried.
- OQ3: store review needs a reachable demo install, a mailbox for the OTP and a pairing QR.
- OQ4: relay hosting, cost and who operates it.
- OQ5: the key's expiry. A year with visible `last_requested_at` is the starting proposal.

## 6. Owed gates

All three need a client outside the Sacchi network, whose DNS filter blocks the address: the phone on mobile data, or a home line.

1. **T1, the Access session from a non-browser client.** Take a `CF_Authorization` from a browser OTP and send it from Dart with no browser: `/api/conversations`, then a full `/agent/run`. Also a value past its expiry. Failure brings §3's fallback back.
2. **T2, the stream through Cloudflare.** One full turn from Dart through the edge, a turn longer than the 125 s read timeout, and the resume after a real network change on cellular.
3. **T3, the same on a phone.** Flutter on a real iPhone and Android: WebView cookie read, app suspended mid-turn, upload.

Before a plan, also run the API key through `RequireAuth` with the `db_integration` CSRF route walk and an upgrade of an existing database.

## 7. Found on the way

A refresh token used weekly outlives `sessionAbsoluteTTL`, for the MCP OAuth clients in production today. The ledger has the numbers. It is a defect in its own right and does not depend on this app.

## 8. Not proven by this spec

Nothing has crossed Cloudflare. §4.2 ran on Authula's API, §4.4 against the VM's Caddy on the LAN, and §3 on the repository's fake Cloudflare. Store acceptance, push latency and the relay's cost are untested.
