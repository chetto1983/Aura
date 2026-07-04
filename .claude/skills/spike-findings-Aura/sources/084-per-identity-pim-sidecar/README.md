---
spike: 084
name: per-identity-pim-sidecar
type: standard
validates: "Given the '3rd MCP class' (calendar/PIM + WhatsApp = per-account, not scope-keyable), when two identities each connect their own account to a per-identity sidecar instance, then A's calendar/messages are invisible to B and the per-account-instance model actually runs on the mini-PC"
verdict: VALIDATED
related: [063, 064, 066, 081, 002, 032]
tags: [mcp, calendar, whatsapp, per-identity, oauth, multi-user, phase-36, v2.0.0]
---

# Spike 084: per-identity calendar/PIM + WhatsApp sidecar (the "3rd MCP class")

## What This Validates

Session-21 concluded MCP isolation has **three classes**: (a) stdio servers run in-box (078 isolates them for free), (b) agent-memory = shared graph + a per-call `user_identifier` scope key (083 memory plane), and (c) **calendar/PIM + WhatsApp = per-user-account sidecars needing a per-identity instance** because "OAuth/pairing per identity — a scope key is NOT enough." Class (c) was the only multi-tenancy piece with **no prior spike** and the hardest. This spike proves the per-identity-instance model live and shows *why* a scope key cannot work here.

## Research (the mechanism, from the real sidecar)

The PIM sidecar (`ghcr.io/chetto1983/aura-pim-mcp:sidecar`, the spike-066 fork) stores **connected accounts + their OAuth tokens in its own `/app/data/appsettings.json`** (`CalendarMcp:Accounts[]`) and encrypts token material with an ASP.NET **data-protection key ring under `/app/data/keys`**. The surface is: MCP at `/`, Admin API at `/admin` (`/admin/accounts`, `/admin/auth/google/callback`, …), guarded by a single `CALENDAR_MCP_ADMIN_TOKEN`.

The load-bearing fact: **there is no per-request identity parameter.** A tool call operates on whatever account *this instance* has connected; `/admin/accounts` returns *this instance's* accounts. Contrast agent-memory (083), where every `memory__*` call carries `user_identifier` against one shared graph. For PIM/WhatsApp the account **is** the server — so multi-tenancy is one instance per identity, each with its own account store, its own admin token, its own OAuth redirect/client, and its own key ring. WhatsApp is structurally identical: the whatsmeow bridge persists a **single paired number's session in `/app/whatsapp-bridge/store` (`whatsapp.db`)** per instance (CONVENTIONS; spike 002) — one number = one bridge instance.

## How to Run

```bash
export MSYS_NO_PATHCONV=1   # distroless-ish: /bin/sh + container paths mangle under Git-Bash
# two per-identity instances, each its own port + admin token + data volume:
#   pim-A :18093 token=TOKEN-ALICE-a1a1 vol=spike084-vol-a
#   pim-B :18094 token=TOKEN-BOB-b2b2   vol=spike084-vol-b
# (docker run --entrypoint /bin/sh …; seed empty appsettings then `dotnet CalendarMcp.HttpServer.dll`)
```

Full command lines are in the Investigation Trail below.

## What to Expect

Each instance's admin token works only on its own instance; each instance's account store and key ring are filesystem-isolated; two instances fit comfortably in RAM.

## Investigation Trail

1. Read the compose service + probed one instance: `/admin/accounts` is `{"accounts":[]}` on a fresh volume, Bearer-gated (401 without/with wrong token), account store is `/app/data/appsettings.json`, MCP endpoint is `/`.
2. **MSYS gotcha (again):** `--entrypoint /bin/sh` and container paths get rewritten by Git-Bash (`/bin/sh` → `C:/Program Files/Git/usr/bin/sh`, container init fails "Created" state); `MSYS_NO_PATHCONV=1` fixes it — same class as 083/059/025.
3. Launched **two** instances (A/B) with distinct ports + admin tokens + data volumes and ran the isolation matrix.

### Live-run evidence (2026-07-04, `aura-pim-mcp:sidecar`)

```
1) PER-INSTANCE ADMIN-TOKEN BOUNDARY (why a scope key can't work):
   A's token on A : 200      A's token on B : 401   <- A's credential is worthless on B's instance
   B's token on A : 401      B's token on B : 200
2) PER-INSTANCE ACCOUNT / OAUTH-TOKEN STORE ISOLATION:
   seeded alice-gmail into A's store only (simulates A connecting a Google account)
   A /admin/accounts : {"accounts":[{"id":"alice-gmail","provider":"Google",…}]}   (reloadOnChange picked it up live)
   B /admin/accounts : {"accounts":[]}                                             <- B never saw A's account
   A store file: …"Accounts":[{"Id":"alice-gmail",…}]…    B store file: …"Accounts":[]…
3) PER-INSTANCE DATA-PROTECTION KEY RING (OAuth token encryption):
   A keys: key-849dd927-b67e-4911-b7e8-03f46e5543bd.xml
   B keys: key-2b44e684-e1d9-4b8f-a561-e1e62c63b993.xml    <- different keys: A's encrypted tokens are undecryptable by B
4) IDLE FOOTPRINT (mini-PC fit):
   pim-a: mem=33.12MiB cpu=0.06%      pim-b: mem=34.5MiB cpu=0.21%      (two instances ≈ 67 MiB)
```

## What to Avoid

- **Do NOT try to multi-tenant PIM/WhatsApp with a shared instance + a scope key.** There is no per-request identity on the surface, and the OAuth token / WhatsApp session is a singular per-server secret. A scope key would either leak A's connected account to B or have nothing to scope. Session-21's "class (c)" call is correct.
- **Do NOT share the `/app/data` volume across identities.** The account store, OAuth tokens, and the data-protection key ring all live there; sharing it collapses the isolation. One volume per identity instance.
- The single `CALENDAR_MCP_ADMIN_TOKEN` per instance is the admin boundary — Phase 36 must mint a distinct admin token per identity instance (as done here), not reuse one.

## Constraints

- **Each identity's PIM instance needs its own OAuth redirect/client registration** (`ExternalBaseUrl` → `/admin/auth/google/callback`). Google Desktop-app-client + Outlook device-code gotchas from spike 066 apply per instance. This is the real onboarding cost of class (c) and the reason the fork moved OAuth into the cockpit (spike 066 decision).
- **~33 MiB idle per instance** → N identities cost ~33·N MiB just for PIM + a similar amount for WhatsApp bridges. On the mini-PC this is comfortable for a handful of identities; a large multi-user deployment would want on-demand start/stop (suspend idle identities' sidecars — pairs with spike 082's `OperatingMode: Suspended` finding).
- WhatsApp per-instance pairing is QR-based and rotates ~20-60s (002); each identity pairs its own number once, session persists in that instance's `whatsapp.db`.

## Results

**VALIDATED ✓** — the per-identity-**instance** model is the correct and only shape for class (c), proven live for PIM: cross-instance admin tokens are rejected (401), account stores + OAuth-token key rings are filesystem-isolated per instance, and two instances cost ~67 MiB idle. WhatsApp is structurally identical (single paired number per `whatsapp.db` bridge instance) and inherits the same verdict by construction.

**Signal for the build (Phase 36):** provision **one PIM sidecar instance + one WhatsApp bridge instance per identity**, each with (a) its own data volume (`~/.aura/pim/<identity>/` / `~/.aura/whatsapp/<identity>/` filesystem rooting, like `Agent.md` 036-039 and 081), (b) a distinct admin token, (c) its own OAuth client/redirect registration or QR pairing. Route the identity's agent MCP mount to that identity's instance port. This is additive to 081's filesystem-rooting pattern — the difference from classes (a)/(b) is that the sidecar **process itself** is per-identity, not just its config. Consider on-demand start + idle-suspend (082 `OperatingMode: Suspended`) to bound the per-identity RAM cost at scale.

**Open (gated on operator accounts, like spike 066):** the actual 2-account OAuth E2E — A connects a real Google account and B a real Outlook account, then A's `get_calendar_events` returns A's events and B's returns B's, with no cross-visibility — needs two real accounts + interactive browser/device-code flows and is a Phase-36 onboarding-runbook proof, not a kill risk. The architecture that makes it isolated (per-instance store + key ring + admin token) is proven here.
