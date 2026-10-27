# Remote access from a phone: one hostname, a wizard you can walk back

Date: 2026-10-27. The operator's direction:
- the state today: *"la parte UI è un disastro"*, and during the audit *"non puoi tornare indietro
  nei passi!"*;
- who uses it: *"io da Ipad e telefono"*;
- on WARP: *"si basta il tunnel"*;
- on the email PIN: *"si va bene"*, for once a month;
- at home: *"lasciamo stare basta l'ip del mini pc basta solo firmare la CA"*;
- after setup: the wizard stays, every step green;
- for existing installs: WARP removed automatically.

Amends `docs/superpowers/specs/2026-09-20-cloudflare-tunnel-design.md`. Every decision there not named
below stands: the pinned `cloudflared` sidecar, Aura-owned resources only, Postgres as the secret
authority, Access before Authula, membership from active Aura users, and direct `:443` kept.

## Intent

One person, the operator, opens their own Aura from an iPad and a phone:
- **Away from home:** through a Cloudflare tunnel, on a hostname of a domain they own.
- **At home:** by the box's LAN IP, without the browser's "Not secure" warning.

Setting it up is a one-time job done from the cockpit. It must never leave the operator stuck:
- every step can be reopened and changed;
- every failure says what went wrong and what to do.

What failed on 2026-10-27, in code and on the lab VM:
- **No way back.** Nothing goes back. The token step never returns once a token is saved. A
  domain cannot be changed while the zone waits for its nameservers (`ErrAddressLocked`), and
  Delete renders only once setup is configured. A wrong domain is a dead end.
- **Errors without a reason.** The server replaces `last_error` with a fixed sentence
  (`internal/agui/remote_access_api.go:118-120`), and the UI shows another fixed sentence. The
  `/api/settings/remote-access/events` log exists, but no UI reads it.
- **Steps with no screen.** Seven steps are shown, and "Access" and "WARP" have no screen at all.
- **Hard to read.** After a reload the account shows as a raw id. The text is full of jargon, and
  the Italian is missing its accents ("puo", "e disponibile", "finche").
- **The home certificate.** Over `https://192.168.101.158` the edge presents a leaf whose only
  SAN is `IP:172.18.0.3`, the Docker bridge address. A browser sends no SNI to an IP, so trusting
  the CA alone would still fail on the name.

## Goals

1. From outside, the operator opens `https://<label>.<their domain>` on an iPad or a phone:
   - Cloudflare Access asks for an email PIN at most once a month per device;
   - then the Aura login follows, and the cockpit works, chat streaming included.
2. At home, `https://<LAN IP>` opens with no warning on every device where the operator has
   installed Aura's root certificate once. The cockpit hands out that certificate and explains how
   to install it.
3. The wizard can be walked forwards and backwards at any time, before and after setup. Token,
   account, domain and label can each be changed from their own step.
4. Every failure the operator can act on names its cause and the action.

## Non-goals

- **No WARP.** The WARP hostname, its Access app and policy, and the Gateway posture check are
  removed (decision 1).
- **No split-horizon DNS at home.** The operator uses the IP; nothing is asked of the router.
- **No publicly trusted certificate on the LAN.** No public CA signs a private IP, and this design
  adds no ACME DNS provider for Cloudflare.
- **No mTLS client certificates.** Cloudflare documents Access mTLS as "not included in the Free
  plan".
- **No change to who may pass Access.** It stays every active Aura identity, as in the 2026-09-20
  spec.
- **No purchase of a domain by Aura.** The operator buys it at a registrar.

## Anti-goals: these would be failures even if every test passed

- **Dead ends.** A state the wizard can reach and the cockpit cannot leave.
- **Generic errors.** An error message that could have been written without knowing the error.
- **Touching what Aura did not create.** For example, the zone, another installation's apps and
  tunnels in the same account, or unrelated DNS.
- **A second navigation column** beside the Settings menu, or a look that does not match the rest
  of the cockpit.

## Decisions (the operator's)

1. **The tunnel is enough: WARP goes, everywhere.** It goes from the UI (step, label field,
   section, texts) and from the reconciler:
   - the WARP label validation in `internal/remotetunnel/steps_zone_tunnel.go:15`;
   - the `warp` route in `steps_access_dns.go:17-18`;
   - the Gateway posture in `steps_access_dns.go:129-167`.

   On an existing installation the reconciler deletes the WARP resources it owns, without being
   asked, on the first reconcile after the update:
   - the CNAME carrying its tunnel's comment;
   - the WARP Access app and its policy;
   - the posture check.

   Andrea's appliance is the first such case. On 2026-10-27, `aura-warp.piermarsync.it` resolved
   through Cloudflare beside `aura.piermarsync.it`.
2. **Access keeps the email one-time PIN, with a session of one month** (today `SessionDuration:
   "24h"`, `steps_access_dns.go:63`). Cloudflare documents application and global sessions "from
   immediate timeout to one month", and it reissues an application token without a prompt while
   the global session is valid. The Aura login still follows.
3. **The domain is the operator's own, in their own Cloudflare account.** It is bought at
   register.it, where the first year is free.
   - Cloudflare documents that a tunnel "only proxies traffic for DNS records in the same
     Cloudflare account", and a subdomain zone in another account needs Enterprise.
   - So a domain whose zone is active in someone else's account, like `piermarsync.it` in
     Andrea's, cannot be used.
   - The nameserver wait stays, and shows only while the zone is not active.
4. **Layout: the first-run wizard's step strip, at the top of the Settings panel.**
   - The strip is `web/src/onboarding/OnboardingStepper.tsx`, approved as a mockup on 2026-10-27.
   - **Steps.** There are four: *Cloudflare → Dominio → Collegamento → Prova dal telefono*, in
     English *Cloudflare → Domain → Connection → Try it from your phone*. The nameserver
     instructions live inside *Dominio* while the zone is pending.
   - **Moving between steps.** A done step has a green dot and reopens on click. The current step
     is one card, with *Indietro* and *Avanti* at its foot. A disabled *Avanti* says why, beside
     it.
   - **On a phone** the strip becomes "Passo N di M · <step>", as in the first-run wizard.
5. **After setup the same wizard stays, every step green.** The last step shows:
   - whether the hostname is reachable, and when it was last checked;
   - the open link;
   - *Disattiva* and *Elimina…*.

   To change anything, reopen its step. There is no separate status page.
6. **At home: the IP, signed by Aura's own CA.** The edge already signs with Caddy's local CA
   (`caddy/Caddyfile:45`). On the VM its root is "Caddy Local Authority - 2026 ECC Root", valid
   until 2036-08-22. Two changes:
   - **The leaf names the LAN IP**, not the container's address. Caddy documents `default_sni` as
     "a default TLS ServerName for when clients do not use SNI". Its interplay with
     `on_demand` + `tls internal` is not documented and must be measured before it is relied on.
   - **The cockpit offers the root as a download**, with install steps for Windows (Trusted Root
     Certification Authorities), iPadOS and iOS. Apple's path is "Settings > General > About >
     Certificate Trust Settings". The card *A casa, su questo dispositivo* sits on the wizard's
     last step.

7. **The phone test is done once per address, and an outage heals by itself.** The operator: *"Se
   il tunnel cade, l'ultimo passo diventa rosso e torna verde da solo. VERDE DA SOLO"*.
   - Today external acceptance is lost on any configuration change and whenever Cloudflare
     reports the tunnel unhealthy (`internal/remotetunnel/acceptance.go:17`,
     `reconciler.go:169-188`). A box restart would send the wizard back to *Prova dal telefono*.
   - Instead, the acceptance holds for the hostname it was given on:
     - a tunnel outage turns the last step red, "not reachable";
     - when the tunnel is back, the step turns green again by itself, with nothing to redo;
     - only a new domain or label asks for the phone test again, and a new token does not.
8. **Finding F-1 is closed in this work** (the operator, 2026-10-27).
   - **The finding.** The 2026-09-21 review measured that every `ports:` entry in `compose.yaml`
     installs an accept rule matching any input interface. From the tunnel network it reached
     Postgres, Garage, ArcadeDB and arcadedb-mcp, even with the port bound to `127.0.0.1` on the
     host (`docs/superpowers/verification/2026-09-21-cloudflare-remote-access-review.md`, §F-1).
   - **The outcome.** From the `cloudflared` container only `caddy:8080` and the internet are
     reachable, on every installation.
   - **Remedies, the builder's choice after measuring:**
     - a drop for traffic from the tunnel bridge to the other bridges, in Docker's user chain;
     - no published data-plane ports on the appliance.

     Both are named in the review.
   - **Constraints on the remedy:**
     - Read Docker's firewall pages (iptables and the nftables backend) before choosing.
     - An existing appliance, Andrea's first, gets it through its own updater: no manual step.
     - It survives a Docker or host restart.
     - It breaks nothing that reaches the stack through a published port today, so inventory
       those callers first.

9. **A domain already active in another Cloudflare account is refused before any zone is
   created, and the reason is written** (the operator: *"SI"*).
   - Today the zone step creates the zone whenever this account lacks it
     (`steps_zone_tunnel.go:57-67`). Cloudflare would accept that as a pending zone, and its
     nameserver instructions would move the other account's zone.
   - Instead, *Dominio* says the domain is active in another Cloudflare account and creates
     nothing.

## Calls made in writing (not the operator's; check them)


- **How a domain active in another account is recognised** (decision 9): before creating, Aura
  reads the domain's public NS. If they are already `*.ns.cloudflare.com` and the zone is not in
  this account, it refuses. On 2026-10-27, `piermarsync.it` answered
  `jo`/`yahir.ns.cloudflare.com`.
- **A new token that cannot see the old resources never blocks the change.** A replaced token
  is often a revoked one, and a refusal would be a dead end.
  - Aura saves the new token, forgets the resources it can no longer reach, and lists them by
    name and kind under *Cloudflare*, so the operator can delete them in the dashboard.
  - It never deletes anything through an account the operator did not choose.
- **"Reachable" means what Cloudflare reports for the tunnel's connections.** A request from
  the box to the public hostname proves nothing: Access stops it before Aura.

## Constraints

- **Cockpit coherence.**
  - Tokens from `web/src/styles/theme.css`; the shadcn `Button` variants (`default`, `outline`,
    `destructive`); the step strip's look from `OnboardingStepper`.
  - Both locales in `web/src/i18n/resources.remoteAccess.ts`, the Italian with its accents. The
    keys no code reads go: `token.verified`, `domain.prerequisite`, `status.accepted`,
    `rotation.documentation`.
- **Plain words.** No "named tunnel", "SSE", "reconciliation", "projected token" or "membership" in
  the operator's view. Cloudflare's own product names (Access, tunnel) may appear where the
  operator meets them in Cloudflare's dashboard.
- **Reasons, not raw errors.** A reason comes from a fixed vocabulary that the server maps from
  what Cloudflare answered. The operator sees no raw upstream text, which could carry identifiers
  or token fragments: the 2026-09-20 spec's "no plaintext" rule still holds.
- **Files.** Every file stays ≤ 600 LOC. Migrations are numbered from
  `ls internal/db/migrations/ | tail -1`.
- **Gates.** CLAUDE.md's gates apply in full: unit and web tests, the integration tiers that do
  not skip under CI, the 85% coverage policy, mutation on the reconciler's critical files, and
  the dated verification file of the live run.

## What is left to the builder

- **The reconciler.** How it migrates the WARP leg away, and whether that needs a migration (the
  `WARPAppID`, `WARPPolicyID` and `GatewayPostureID` fields live in the state).
- **Changing domain after resources exist.** How the old Aura-owned resources are removed before
  the new ones are created. The contract the operator sees is: the change is allowed at any step,
  and the old address stops working.
- **The session value.** The exact value Cloudflare's API accepts for one month, read from its API
  reference. Also whether Aura sets the organisation's global session or only the application's,
  given decision 2 and the anti-goal on unrelated resources.
- **The error vocabulary.** It must cover at least:
  - a token without *Account Settings Read*: the account list comes back empty, which is the
    2026-09-21 trap;
  - a domain not in this account;
  - nameservers not changed yet;
  - a hostname already used by another Access app or a wildcard (`steps_access_dns.go:58`);
  - tunnel not connected.
- **Event history.** Whether `/events` feeds a short history under the last step.
- **The token step.** Whether it can open Cloudflare's token page with the permissions
  pre-filled. If Cloudflare supports that, it replaces the permission paragraph; WARP's
  permissions leave the list either way.
- **The home certificate.** How the edge learns the LAN IP for `default_sni` (installer, `.env`
  or detection), and what the cockpit shows while it is not known.

## To verify before relying on it

- **The `.it` registry check.** It must accept a nameserver change to a zone added to Cloudflare
  but not yet active. Secondary sources say the registry checks nameservers before applying a
  change, hourly, and point to `dns-check.nic.it`; none is the registry's own documentation. Run
  the domain through that validator before the change.
- **`default_sni` with an IP** and Caddy v2.11.7's `on_demand` internal issuance (decision 6).

## Acceptance

Measured on the lab VM and recorded in `docs/superpowers/verification/`. The test domain is
`chetto1983.it`:
- it is registered at register.it for this test and added to the operator's own Cloudflare
  account;
- on 2026-10-27 it did not resolve yet (NXDOMAIN at 1.1.1.1).

1. **The first setup, from scratch.** From the cockpit: token → domain → nameservers changed at
   register.it → connection → try it from the phone. Then the phone, on mobile data and off the
   home network:
   - opens `https://<label>.<domain>`;
   - receives the email PIN, then the Aura login;
   - streams a chat reply.
2. **Back and change.** Each step reopens. The domain can be changed while the zone is still
   pending. The token can be replaced after setup. In each case the wizard reaches green again.
3. **Errors.** A token without *Account Settings Read* and a domain whose zone is in another
   account each give their own reason and action in the step that caused them.
4. **The session.** Cloudflare's API shows the Access application's session at one month.
5. **WARP removed.** From a state with the old WARP leg (created by the previous code, or the
   integration fake seeded with it), one reconcile leaves:
   - no WARP CNAME, app, policy or posture;
   - the public hostname, the zone and unrelated DNS untouched.
6. **At home.** `https://192.168.101.158` presents a leaf naming that IP. After the root is
   installed, Chrome on Windows and Safari on iPad open it with no warning.
7. **F-1 closed.** Measured on the lab VM after an update through the updater, with the
   review's own probe block from a container on the tunnel network:
   - every data-plane address it lists is `blocked`;
   - `caddy:8080` and an internet address are reachable;
   - the same holds after a Docker restart and after a host reboot;
   - `verifyTunnelNetwork()` (`web/e2e/support/remoteAccessLive.ts`) passes unweakened.
8. **Gates.** Every gate of the constraints, and `make payload-manifest` if compose or payload
   files change.
