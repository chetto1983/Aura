# The remote access wizard on the lab VM — 2026-10-31

The operator walked the wizard on the lab VM at revision `ce299a01f`, with a real domain and a
Cloudflare account that had never held a zone: `chetto1983.it`, registered at register.it on
2026-10-27. Every step ran for real. The address now answers from outside, and the phone test
passed. The run also found five defects in the wizard, and three of them cost the operator the
afternoon.

## Timeline

| UTC | What happened |
|---|---|
| 17:06-17:07 | The operator saved the token and the domain. Cloudflare assigned `huxley` and `laila`. |
| 17:11 | The registry, read by `whois.nic.it`, still held `ns1`/`ns2.register.it`, last changed 2026-10-27. The change had never been started: register.it's form sat at its first step. |
| 17:17:31 | The operator submitted the change. The registry accepted it at once, with the zone still pending. |
| 18:08:29 | The zone was active, and on that first pass Aura stopped: HTTP 403, code 10000, row `error/token_rejected`, zone held, no tunnel. |
| 18:09:07 | Google's resolver showed Cloudflare's SOA. |
| 18:22:36 | With a new token holding Cloudflare Tunnel Edit, the tunnel was created and Aura stopped at the next step: HTTP 403, code 9999, the same `token_rejected`. |
| 18:25:40 | Zero Trust activated by the operator (team `spring-forest-dcdc`, Free). A retry on the saved token went `error`, `provisioning`, `connecting` in 15 seconds. |
| 18:27:23 | `https://aura.chetto1983.it` through Cloudflare's edge answered 302 to the Access login. |
| 18:28:24 | `accept-external` arrived from the phone on mobile data. The row went `healthy`, reachable. |
| 18:30:10 | First chat turn after acceptance. |

## Measured

- **The `.it` registry accepts a zone Cloudflare still calls pending.** The change took seconds.
- **Activation took under 52 minutes** from the registry change, with no manual recheck. Cloudflare's
  own page says one or two hours, at most 24.
- **Public NS answers lag the zone's state.** At 18:09:18 Google still served `ns1.register.it` for NS
  while serving Cloudflare's SOA for the same name. A resolver's NS record is not the signal; the
  zone's status is.
- **register.it refuses an external nameserver without an IP**, one IP per row, and warns that add-on
  services stop without refund. Its acceptance box is not enforced: Procedi advanced with it unticked.
- **A Cloudflare zone starts empty here.** No record was imported from register.it, so
  `chetto1983.it` and `www` stop resolving and mail has no MX once the zone activates. The domain was
  in use for nothing else, so nothing was lost.
- **Session duration reads 1 month** in the dashboard's Access application. That is the label for
  the `730h` Aura sets. The exact string was not read from the API.
- **Gates at `ce299a01f`:** `make quality` green on `e58870048`, coverage 50295/57336 = 87.7% with the
  package policy passing, web 3687/3687, mocked Playwright 4/4. All workflows green except CI's Stryker
  job, still running when this was written.

## Defects found

1. **A token that cannot touch tunnels passed the first step.** The refusal came only after the zone
   activated, 51 minutes after the nameservers changed.
2. **The refusal did not say what was missing.** Every 403 read "expired, revoked or lacks a
   permission", though Aura knew which step had failed.
3. **An account without Zero Trust failed with a false reason.** The sentence sent the operator to
   paste a third token that could not help. Neither the wizard nor the guide listed Zero Trust as a
   prerequisite: Andrea's appliance already had it, so a fresh account had never been run.
4. **The wizard asked for a paste it did not need.** Editing a token on Cloudflare keeps its value, but
   Avanti stayed disabled until a token was typed. The retry that recovered this run had to come from
   outside the wizard.
5. **The waiting screen said nothing.** It read the same for a change never made, one queued and one
   refused. The operator's words: "sono fermo qui senza avvisi".

Fixed in the commit after this file: 1 to 5. Aura now reads tunnels and identity providers before it
stores a token, names `tunnel_permission` and `access_unavailable`, retries the saved token, and says
how long the wait lasts and what to check at the registrar.

Left as they are:

- Aura never calls Cloudflare's `activation_check`. Activation came by itself inside Cloudflare's
  stated range, so it buys nothing measured.
- A tab open on an error row does not poll, so a retry from another session leaves it showing the old
  error. That needs a second session, which the wizard itself no longer requires.

## Acceptance items

| Item | Result |
|---|---|
| 1. First setup | Done on a fresh account, through the defects above. |
| 2. Walk back and change the domain while pending | Not run live. The zone activated before it was tried. Unit and mocked coverage only. |
| 3. A token without Account Settings Read, `piermarsync.it` refused as `domain_elsewhere` | Not run live. A token without Cloudflare Tunnel Edit was, by accident. |
| 4. The session read back | Read from the dashboard, not the API: 1 month. |
| 5. WARP retirement | Fake only. Andrea's appliance has not updated. |

## What this run does not prove

- **The attended live suite did not run.** `scripts/cloudflare_tunnel_live_e2e.sh` needs a dedicated
  disposable appliance, a pending zone and a second token. This VM is none of those now.
- **That the fixes work live.** They were written after this run. The first-step refusal has not met
  a real token yet, and code 9999 was seen once, on one account.
- **That the read catches every case.** A token with Tunnel Read and no Edit passes the first step and
  is refused at its step, under the same reason. Whether an account without Zero Trust refuses the
  list or only the create was not separated: `EnsureOTPProvider` does both and the client keeps the
  code, not Cloudflare's message.
- **That a chat reply streamed to the phone.** Two turns ran at 18:30 and 18:31 and the operator said
  it works. The logs do not say which device sent them, and arrival timing was not measured.
- **An outage.** The WAN pull against stopping `aura-cloudflared` was not run, nor a restart at
  nameserver waiting, nor the Conferma flicker, nor Cloudflare's call rate.
- **Turn recall.** Both turns logged `turn recall failed` at the 500 ms budget. The embed server
  answered in 25-175 ms when probed, so the time goes elsewhere. It is unrelated to this wizard and
  still unmeasured.
