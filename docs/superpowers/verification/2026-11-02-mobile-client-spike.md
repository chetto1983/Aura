# Mobile client spike: 2026-11-02

Scope: the four gates of `docs/superpowers/specs/2026-11-02-mobile-client-design.md`, run from the operator's workstation at `11c06be83`, the revision on the lab VM's image label and at `master`. Two of the gates could not run from this network. The rest changed the design: the spec's service-token path is refused by the reconciler, and its refresh-token path does not suit a phone.

The Go tests and the plugin patch ran on a copy of HEAD in WSL, against a disposable Postgres, and nothing of theirs is in the tree. Nothing changed on Cloudflare or in the VM's configuration; on the VM, four throwaway conversations and three sessions came and went.

## What could not run

S1 (a service token beside the email policy), S2 (time for a revocation to take effect) and S3 through the tunnel are not measured. Three things stand in the way, each enough alone:

1. **The network blocks the address.** The Sacchi network's FortiGuard DNS filter answers `aura.chetto1983.it` with `208.91.112.55` on both the workstation and the VM, and TLS there presents `CN=Fortiguard SDNS Blocked Page`. DoH shows the real Cloudflare addresses. Pinning one with `curl --resolve` would pass the filter. It is a corporate security control, so it was not tried. The tunnel itself is unaffected: `aura-cloudflared` is healthy and the row reads `healthy`, because the connector dials Cloudflare's edge, not the blocked name.
2. **No credential can create a service token.** The workstation holds no Cloudflare token. The operator's token is encrypted in the VM's settings, was not decrypted, and by `docs/cloudflare-remote-access.md` it lacks `Access: Service Tokens`.
3. **The reconciler refuses the change**, below. Trying S1 on the live application would have parked remote access in `error`.

## The reconciler refuses a device policy

`policy()` treats any second policy on the application as a conflict (`internal/remotetunnel/steps_access_dns.go:102`: "An extra policy could bypass the restrictive allowlist, regardless of its name"). On the healthy phase a pass runs every 5 minutes (`remoteAccessDelay`). Measured on the real reconciler over the repository's fake Cloudflare, not on Cloudflare:

| Step | Result |
|---|---|
| Service Auth policy appears beside the owned one | `error`, reason `not_owned`, `observed_healthy=false`, connector left running |
| Next pass | Terminal: "save desired configuration to retry" |
| The policy is removed by hand, next pass | Still terminal, reason unchanged |
| `SyncMembers` while parked | Terminal, so a deactivated person is not removed from the Access policy |
| Path-scoped application `aura.example.com/mobile` instead | `error`, reason `hostname_taken` |

A second hostname does not help either: `publish` rewrites the tunnel's ingress from the owned routes (`steps_access_dns.go:137`), so an added ingress is dropped by the next pass.

## Refresh tokens do not suit a phone

Aura's OAuth server and the real Authula v1.48.0 on Postgres 18, with a fake identity resolver only. The grant was issued for an existing MCP resource: a `mobile` audience does not exist, and the lifecycle does not depend on the audience.

| Case | Refresh | What it means |
|---|---|---|
| Control, twice | 200, 200 | Rolling window 168 h |
| Session idle-expired, cookie refused | **200** | The chain ignores `expires_at` |
| Session 30 days past sign-in, cookie never shown again | **200, 200** | The chain ignores the 7-day absolute cap |
| Cookie shown past the cap, row deleted | 400 `invalid_grant` | The cockpit's browser can end the phone's grant |
| Sign-out | 400 `invalid_grant`, but the access token still verifies | A signature check alone leaves up to 15 minutes |
| Refresh token past its 7 days | 400 `invalid_grant` | A phone unused for a week is out |
| Response lost, retried within the 10 s grace | 200, and both tokens then rotate | The chain forks |
| Response lost, retried after 11 s | **400**, while the token never delivered still answers 200 | One lost response on a bad line and the phone is out |
| Two refreshes at once | 200, 200 | The chain forks |

Read on the VM: `authula.sessions` holds 440 rows for one user, 2 live, the oldest from 2026-09-21. Nothing cleans them, because Aura leaves Authula's `AutoCleanup` off, so a row outlives its expiry unless its cookie comes back past the cap or someone signs out. `MaxSessionsPerUser: 5` is enforced only by that same cleanup, so it does not apply either.

**A defect that exists today, apart from the app.** MCP OAuth clients use this chain. A refresh token used at least once a week keeps rotating past `sessionAbsoluteTTL`, which `session_renew.go` states ends a session however busy it is. Only a sign-out, or the cookie returning past the cap, stops it. The VM holds one such case now: its only unrevoked refresh token sits on an expired session. It was not fixed here.

## An Authula API key does suit one

`plugins/api-key` ships in the Authula Aura already depends on, and needs `access_control` beside it. Enabled on the copy:

| Check | Result |
|---|---|
| Both plugins start on the real Authula; the package's unit tests still pass | Yes |
| Key minted for a user, owner read back | Yes, 64 characters, stored as a hash |
| Verify, average of 200 | 430 µs on the workstation's local Postgres |
| After the cockpit signs out | Still valid |
| One of two devices deleted | That key refused at once, the other valid |
| Key past `expires_at` | Refused |
| `/auth/api-keys`, `/auth/api-keys/verify` with no session | 401, 401, 401 |

`GetAll` listed 0 keys in this run because the test passed an empty owner-type filter. That is the test's fault and says nothing about the plugin.

## The stream from Dart

`spikes/mobile-client/sse_spike.dart`: Dart 3.13.7 `HttpClient` with no dependency, in a container, signed in with the operator's account through the repo's helper, against the VM's Caddy at `https://192.168.101.158`. Eleven turns on the cockpit's own model route, each asking for the integers 1 to 300, so a lost or repeated piece shows. Four throwaway conversations, each deleted (204), and each session signed out (200).

| Case | Result |
|---|---|
| Uninterrupted, 4 turns | `text/event-stream`, `no-cache`, chunked. 8 frames, ids without a gap, integers intact, and on the conversation's first turn the text verbatim in `/threads/{id}/messages` |
| Cut at frame 2 (10 to 13 s), resumed 3 s later with `Last-Event-ID: 2` | 200, first id 3, no gap, intact. The turn had ended, so this was replay |
| Cut at frame 2, resumed 5 s after the turn ended | Same |
| Cut at `RUN_STARTED` (30 ms), resumed 1 s later with `Last-Event-ID: 1` | 200, first id 2, no gap, intact, on a turn still running. The response headers came with the next frame, about 10 s later |
| Cut at `RUN_STARTED`, cold attach through `live_run_id` | The id matched, replay from id 1, then 10.2 s live to `RUN_FINISHED`, intact |
| Viewer gone after 21 ms, fresh conversation | The turn was persisted all the same |
| Resume about 215 s after the turn ended, twice | 404 once, 200 once |

The last row is not a contradiction. The reaper ticks at `linger/2` (`internal/agui/runregistry.go:218`), so with `AURA_AGUI_RUN_LINGER_SEC=180` a finished run disappears between 180 and 270 s after it ended.

**Text arrived whole.** Every one of the eleven turns carried its 1,091 characters in a single `TEXT_MESSAGE_CONTENT` frame, 11.4 to 15.9 s after the request on the four that were not cut. On this route nothing readable reaches the screen before the answer is complete. The frames were counted on the wire, so this is not the client. Other routes were not tried.

The first attempt at this test measured nothing about resume. Its cut waited for the 20th text frame and each turn has one, so those four turns only count as uninterrupted. The next run's scenario E is void too: `live_run_id` was already empty and the script called a URL with no id, answered by a 307. Both were corrected and rerun above.

## Cloudflare limits read, not measured

- Proxy read timeout 125 s, write 30 s, idle 900 s ([connection limits](https://developers.cloudflare.com/fundamentals/reference/connection-limits/)). The 15 s heartbeat sits well inside the read timeout.
- `cloudflared access login` returns a token sent as `cf-access-token` ([CLI guide](https://developers.cloudflare.com/cloudflare-one/tutorials/cli/)). The application token page shows `curl` sending `CF_Authorization` by hand, to the get-identity endpoint only. Neither page covers a phone.
- Access rechecks a person against its policies when their session expires, and the page describes no check during a session ([sessions](https://developers.cloudflare.com/cloudflare-one/identity/users/session-management/)).

## What this does not prove

- **Anything through Cloudflare.** Neither the edge, nor Access, nor `cloudflared` carried one byte of these runs.
- **That a device policy would work on Cloudflare.** The reconciler refused it before Cloudflare was asked. How Service Auth and Allow combine on one application remains unmeasured.
- **That a `CF_Authorization` value read from a WebView works from Dart**, on the stream or at all, or how it behaves when it expires.
- **Anything on a phone.** The client was Dart 3.13.7 in a Linux container, not Flutter on iOS or Android. No cellular link, no handover, no OS suspension, no background fetch.
- **A real network loss.** Every cut was a client-side close, which the server sees at once. A dropped radio sends nothing, and how long the server keeps writing to a dead socket was not measured.
- **Progressive text on any other route**, or a turn with tools, approvals or a question in it. Every turn here was plain text.
- **The API key in the daemon.** It ran through the plugin's API, not through `RequireAuth`, and the plugin's routes are not mapped to `session.auth`, which is why they answer 401 to everyone. Its effect on `make quality`, the CSRF route walk under its build tag and the migrations on an upgrade were not run.
- **The API key's rate limit**, which was not exercised.
