---
spike: 066
name: calendar-oauth-e2e-google-outlook
type: standard
validates: "Given calendar-mcp running on Windows with real OAuth-connected Google + Outlook.com accounts, when Aura drives create_event/get_calendar_events + send_email/search_emails to self, then events and tagged emails round-trip on both real providers"
verdict: VALIDATED
related: [063, 064, 001]
tags: [calendar, oauth, google, outlook, email, e2e, consolidation, dotnet]
---

# Spike 066: calendar-oauth-e2e-google-outlook

## What This Validates

Given `MarimerLLC/calendar-mcp` (the .NET 10 server built in spike 063) running natively
on Windows as a self-contained binary, with **real OAuth-connected** Google (`dvdmarchetto@gmail.com`)
and Outlook.com (`chetto983@hotmail.it`) accounts, when the operator drives the full PIM
loop — `list_accounts` → `list_calendars` → `get_calendar_events` → `create_event` →
read-back → `send_email` (to self) → `search_emails` — then calendar events and uniquely-tagged
emails round-trip live on **both** providers. This is the real-world capstone (originally
split as 065 no-auth + 066 single-provider-OAuth; the operator went straight to full real
OAuth for both, which is strictly stronger) and the **consolidation proof**: one .NET server
covers mail + calendar for both Google and Microsoft.

## Research

- Runtime decision: run calendar-mcp **natively on Windows as a self-contained single-file binary**
  (cross-published from the WSL .NET 10 SDK: `dotnet publish -r win-x64 --self-contained -p:PublishSingleFile=true`).
  Self-contained ⇒ no .NET install and **no `DOTNET_ROOT` gotcha** (vs the WSL framework-dependent apphost in 063).
  Windows-native also makes the OAuth browser/loopback + device-code flows work natively, and gives Aura
  the simplest mount (`Command: ...exe`).
- Provider auth flows (confirmed from source): **Google = browser loopback** via Google.Apis
  `LocalServerCodeReceiver`; **Outlook.com = Device Code Flow** via MSAL (`AuthenticateWithDeviceCodeAsync`).
- The CLI prompts are interactive (Spectre.Console); `reauth <account-id>` is **prompt-free** (reads
  the account from `appsettings.json` and runs only the OAuth flow) — the seam used to drive auth
  after pre-writing the account config.

## How to Run

Server (Windows, self-contained): `D:\tmp\calendar-mcp-win\server\CalendarMcp.StdioServer.exe`,
config at `%LOCALAPPDATA%\CalendarMcp\appsettings.json` (accounts + token caches).

```powershell
# one-time per account (interactive consent; config pre-written, then prompt-free reauth):
D:\tmp\calendar-mcp-win\cli\CalendarMcp.Cli.exe reauth google-personal     # browser consent
D:\tmp\calendar-mcp-win\cli\CalendarMcp.Cli.exe reauth outlook-personal    # device code at microsoft.com/devicelogin

# E2E via the reusable MCP harness (spawns the stdio server, runs JSON step lists):
cd D:\tmp\calendar-mcp-win ; python mcp_harness.py steps_google_e2e.json
python mcp_harness.py steps_outlook_e2e.json
```

`mcp_harness.py` + `steps_*.json` are copied into this spike dir. Each step is
`{op: list|schema|call, tool, args}`; the harness handshakes once and runs them in one session.

## Investigation Trail (the OAuth onboarding pain — motivated the fork)

1. **Spectre refuses piped stdin** (`add-google-account` via piped answers → "Failed to read input
   in non-interactive mode"). Pivot: **pre-write `appsettings.json` account config + run prompt-free `reauth`**.
2. **Google `redirect_uri_mismatch` ×2.** The docs say register `http://localhost:8642/authorize/`
   as a **Web application** redirect — WRONG. `GoogleAuthenticationService.cs:46` feeds that string to
   `LocalServerCodeReceiver(...)`, whose `string` arg is the **close-page HTML, not the redirect**. The
   real redirect is an auto-assigned loopback **random port** (`http://127.0.0.1:50442/authorize/`),
   which only a **Desktop app** OAuth client accepts (Google auto-allows loopback for installed apps).
   Fix: create a **Desktop app** client (not Web application). Upstream doc + code-comment are both wrong;
   the runtime error is ground truth.
3. **Google ✓** after switching to a Desktop client: cold start + consent → token cached →
   `test-account` green.
4. **Outlook device code `AADSTS70002` "client must be marked as 'mobile'."** Root cause: **"Allow
   public client flows" was not set to Yes** (device-code requires a public client). Fix: Azure →
   Authentication → Advanced → Allow public client flows = **Yes**; redirect under **Mobile & desktop
   applications** (not Web). Then a fresh device code worked.
5. **Outlook email search lag.** `search_emails` returned empty immediately after `send_email`;
   found after ~25 s — **Microsoft Graph search-indexing lag** (Gmail was instant). Retry-with-backoff
   is required for Graph email read-back assertions.

## Results

**VALIDATED ✓** — full PIM loop round-trips live on both providers.

| Step | Google (`dvdmarchetto@gmail.com`) | Outlook.com (`chetto983@hotmail.it`) |
|---|---|---|
| list_accounts / list_calendars | ✓ (3 cals incl. primary + Famiglia) | ✓ (4 cals incl. default + La tua famiglia) |
| get_calendar_events (existing) | ✓ ("Butta il ferroso" 2026-07-11) | ✓ (empty range) |
| create_event | ✓ `isq59b1228b7fefd6paagvf5pk` | ✓ `AQMk…JO0EAAAA=` |
| read-back created event | ✓ 2026-06-17 15:00 Caraglio | ✓ 2026-06-17 16:00 Caraglio |
| send_email (self) | ✓ `19ed002c2df47290` | ✓ `sent-20260616105017` |
| search_emails (tag `AURA-E2E-20260616`) | ✓ instant | ✓ after ~25 s (Graph indexing) |

**Key findings carried into the fork design** ([spec](../../../docs/superpowers/specs/2026-06-16-calendar-pim-mcp-fork-design.md), [plan](../../../docs/superpowers/plans/2026-06-16-aura-pim-mcp-fork.md)):

1. The PIM capability is real and consolidation-ready — one .NET server, both providers, calendar + email.
2. **OAuth onboarding is too hard for end users** (Desktop-vs-Web client, device-code public-client toggle,
   per-deployment app registration). → Decision: **fork to a thin HTTP sidecar (`aura-pim-mcp`)** whose
   connect/management is driven by Aura's own frontend; the HTTP admin flow uses a proper registered web
   redirect, sidestepping the desktop-loopback bug entirely.
3. Self-contained Windows binary = clean runtime (no `DOTNET_ROOT`); but production target is the HTTP
   sidecar image (Phase 0 of the plan gates .NET-MCP-over-HTTP ↔ Aura Go interop).
4. Graph email read-back needs retry-with-backoff; Gmail is instant.

## Artifacts

- `mcp_harness.py` — reusable stdio MCP harness (handshake + JSON step list).
- `steps_google_e2e.json`, `steps_outlook_e2e.json`, `steps_outlook_discover.json` — the exact E2E steps run.
