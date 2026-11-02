# Mobile client (Flutter) through the Cloudflare tunnel — design

Date: 2026-11-02
Status: draft. The operator chose the direction on 2026-11-02 (below); no implementation plan
lands until the spike in §6 has measured its gates and this spec records the numbers.

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
- **Crossing Access:** QR pairing with one Cloudflare Access service token per device (§4).

## 2. Inventory (what already exists)

| Need | Already in the tree | Gap |
|---|---|---|
| Native OAuth client | `internal/webauth/mcp_oauth_*.go`: public PKCE-S256 clients, RFC 7591 registration, loopback redirects, rotating refresh tokens on Authula's Ed25519/JWKS (`mcp_token_plugin.go`) | Tokens are audience-bound to MCP resources only |
| Bearer on the cockpit API | Authula v1.48.0 ships `plugins/bearer`, `plugins/jwt`, `plugins/api-key` | `agui.RequireAuth` validates the Authula cookie only (`internal/agui/auth.go`) |
| Access management | `internal/cloudflareapi` (apps, policies, OTP provider), `internal/remotetunnel` reconciler with ownership predicates and teardown | No service-token calls; `validatePolicy` admits only explicit-email + OTP policies (`access.go:116`) |
| Chat stream, approvals, questions, uploads | `/api/conversations*` (AG-UI SSE with resume, `web/src/chat/sseResume.ts`), `/api/approvals*`, `web/src/questions`, assets/documents | None expected; the client consumes the same API |
| Push fan-out | Identity-bound deliveries (`DeliverToIdentity`, `DeliverApproval`) feed Telegram/WhatsApp | No mobile push channel, no relay |

## 3. Why not Cloudflare Managed OAuth

[Managed OAuth](https://developers.cloudflare.com/cloudflare-one/access-controls/applications/http-apps/managed-oauth/)
is Cloudflare's documented path for non-browser clients. Read on 2026-11-02, it does not fit a
store app serving arbitrary installs:

- dynamically registered redirects must be `https`, `localhost` or loopback; custom schemes are
  not documented, and iOS universal links need every domain declared at build time;
- Access then serves `/.well-known/oauth-authorization-server` on the app's hostname, which Aura
  already serves (`cmd/aura/serve_webui_mcp_oauth.go:19`); the page itself says not to enable it
  in front of an app running its own OAuth server;
- tokens are opaque and the page names no plan availability.

This is a reading of the documentation, not a measurement. The spike does not test it.

## 4. Design

### 4.1 Pairing

1. In the cockpit (already behind Access OTP + Authula), **Settings > Devices > Pair a phone**.
2. Aura creates a Cloudflare service token named for the device
   (`POST /accounts/{id}/access/service_tokens`, permission `Access: Service Tokens Write`), adds it
   to a **separate** Service Auth policy on the existing Access application, and mints a one-time
   pairing secret (5 min TTL, single use, bound to the identity and the cockpit session).
3. The cockpit shows a QR with `{hostname, cf_client_id, cf_client_secret, pairing_secret}`. The
   client secret is shown once by Cloudflare; Aura does not store it after the QR expires.
4. The app stores the Access credentials in Keychain/Keystore and exchanges the pairing secret at
   Aura's token endpoint for a refresh token bound to a new `mobile` resource. Every request then
   carries `CF-Access-Client-Id`/`CF-Access-Client-Secret` and `Authorization: Bearer <access>`.

The device row (identity, service-token id, Authula session, push token, last seen) lives in a
new Postgres table; its migration number comes from `ls internal/db/migrations/ | tail -1` when
the slice lands.

### 4.2 Two layers, kept separate

- The existing email+OTP policy and its validator stay **unchanged**. Service tokens get their own
  constructor and validator (only `service_token` includes, Service Auth action), so neither shape
  can drift into the other.
- Access proves the device; Aura proves the identity. A request passing Access with a device token
  still needs a valid `mobile` bearer, and `RequireAuth` maps it to an active identity exactly as
  it does a cookie (deactivated identity = rejected).
- **Revocation** (cockpit or identity deactivation) deletes the service token and the Authula
  session together. Cloudflare's docs state that deleting the token is the only way to revoke it.
- **What this costs:** a stolen phone keychain yields both credentials, equivalent to a stolen
  cockpit session until revoked. The remote-access wizard must ask for one more token permission.

### 4.3 Push through a relay

APNs and FCM credentials belong to the publisher, not to each install, so a store app needs a
central relay (a Cloudflare Worker, by analogy with the `aura-connect` Pages relay). The relay
must carry no content: the install sends `{device push token, opaque id, ciphertext}` encrypted to
a key the app generated at pairing; the app decrypts (iOS Notification Service Extension) or
fetches the item over the tunnel. Mobile push becomes a new channel alongside Telegram/WhatsApp on
the identity-bound deliveries.

### 4.4 The app

Four screens: conversation list, chat (AG-UI SSE with resume), approvals/questions, pairing. Plus
the OS share sheet for uploads. Where the Flutter app lives (monorepo directory vs separate repo)
and its CI are decided in the plan, after the spike.

## 5. Open questions

- OQ1: whether a Service Auth policy and the email policy can coexist on one Access application
  with the precedence the reconciler expects, or the device path needs its own application/hostname.
- OQ2: allowed service-token durations and any per-account count limit (not stated in the docs).
- OQ3: whether SSE streams survive the tunnel to a mobile client on cellular, and for how long.
- OQ4: store review: the reviewer needs a reachable demo install and a pairing QR; how we supply it.
- OQ5: relay hosting, cost and who operates it.

## 6. Spike (Gate 1 — measure before any plan)

On the lab VM, with remote access configured on a real domain, a throwaway Go program and a minimal
Flutter (or plain Dart) client measure:

1. **S1** create a service token and a Service Auth policy beside the email policy via API; a
   request with only the device headers reaches the origin; the browser OTP path still works (OQ1).
2. **S2** deleting the token stops the next request (time to effect measured), and the email policy
   is untouched.
3. **S3** a Dart client streams a full chat turn over SSE through the tunnel on cellular, and
   resumes after a forced network switch (OQ3).
4. **S4** Aura's OAuth server issues and refreshes a `mobile`-audience token, and a bearer request
   to `/api/conversations` is accepted only with a valid token for an active identity.

Each gate records its numbers in `docs/superpowers/verification/` and states what it does not
prove. S1 or S2 failing reopens §4.1; Managed OAuth is reconsidered only then.

## 7. Not proven by this spec

Nothing here is measured yet: §2 is a code inventory, §3 and the service-token behaviour are from
Cloudflare's documentation as read on 2026-11-02. Store acceptance, push delivery latency and the
relay's cost are untested.
