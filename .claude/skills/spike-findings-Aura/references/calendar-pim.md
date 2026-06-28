# calendar pim

Calendar + email + contacts (PIM) capability for Aura, sourced from `MarimerLLC/calendar-mcp`
(the first C#/.NET 10 MCP server mounted into Aura) and consolidated into the `aura-pim-mcp`
thin HTTP-sidecar fork. All three spikes are VALIDATED; the fork itself (later shipped per
project memory `project_calendar_mcp_fork_aura_pim_mcp`) is the production target.

## Requirements

Non-negotiable, from MANIFEST Session-16 (binding for calendar/mail planning) and the spike
verdicts:

- **CONSOLIDATION (operator directive 2026-06-16):** calendar-mcp **REPLACES** the Node `mail-mcp`
  server (spike 001) entirely — one .NET server unifies email + calendar + contacts. The email
  path was proven in 066 via real-OAuth `send_email`→`search_emails` on **both** Google and
  Outlook (stronger than the planned IMAP app-password path mail-mcp used). Do not keep two
  servers.
- **FORK to a thin HTTP sidecar (`aura-pim-mcp`).** The raw upstream server works end-to-end, but
  OAuth onboarding is too hard for end users. Approved fork shape: REST admin API (driven by
  Aura's cockpit frontend) + MCP-over-HTTP for the agent loop, per-deployment OAuth with a proper
  registered **web** redirect, **trimmed tool surface (~16 of 29)**, Kiota CVE patched. Spec:
  `docs/superpowers/specs/2026-06-16-calendar-pim-mcp-fork-design.md`; plan:
  `docs/superpowers/plans/2026-06-16-aura-pim-mcp-fork.md`.
- **Agent mount = streamable-HTTP, `Deferred: true`, namespaced.** Phase-0 interop gate (064) is
  GREEN, so the production agent mount uses `mcp.ServerTypeStreamableHTTP` (no stdio fallback
  needed). The server advertises **29 tools** which alone breaches the 30-50-tool degradation
  cliff — every mounted tool MUST be `Deferred` (validated in 064) and the destructive surface
  gated by `DenyRisk=write` at the fork's server-side trim + the Aura mount-policy layer.
- **MCP registration via the managed config**, never new `AURA_MCP_*_SERVER` env vars:
  `aura mcp add`/`install` → `~/.aura/mcp/servers.json`. `AURA_MCP_*_SERVER_JSON` /
  `AURA_MCP_CALENDAR_URL` remain test-tier overrides only. Secrets (OAuth client id/secret, token
  caches) live in managed-config env entries or operator env — never committed.
- **OAuth gotchas are binding for the fork + onboarding runbook:** Google connect needs a
  **Desktop-app** OAuth client (NOT Web application — the upstream doc is wrong); Outlook
  device-code needs Azure **Allow public client flows = Yes** with the redirect under
  **Mobile & desktop applications**; Microsoft Graph email read-back has **~25s indexing lag**
  (retry-with-backoff mandatory), Gmail is instant. The fork's HTTP admin flow uses a proper
  registered **web** redirect to sidestep the desktop-loopback bug entirely.
- **Reads/writes in tests target the operator's own accounts only**; ground truth = read-back via
  the same MCP server (`send_email`→`search_emails`, `create_event`→`get_calendar_events`).

## How to Build It

### Production sidecar (064 — the gate that decides the architecture)

The production target is the **Linux HTTP sidecar container** (Aura is multi-host; the appliance
is Linux). Build from the upstream `Dockerfile` (`sdk:10.0`→`aspnet:10.0`):

```powershell
docker build -t aura-pim-mcp:gate -f D:\tmp\calendar-mcp\Dockerfile D:\tmp\calendar-mcp
docker run -d --rm --name aura-pim-gate -p 127.0.0.1:8093:8080 \
  -e CALENDAR_MCP_ADMIN_TOKEN=aura-dev-admin-token aura-pim-mcp:gate
```

- Container internal port is **8080** (`ASPNETCORE_URLS=http://+:8080`); mapped to host
  `127.0.0.1:8093` in the gate.
- **MCP endpoint path is `/`** (server log: `MCP endpoint: /`). Health at `/health` →
  `{"status":"healthy"}`.
- Runs zero-config with an empty config (zero accounts) — enough for the interop gate; account
  connect is the admin REST API (fork Phases 3-4).

Drive it through Aura's real `internal/mcp` + `internal/agent/mcptools` path
(`sources/064-calendar-mcp-http-mount/main.go` is the exact harness):

```go
server := mcp.ManagedServer{
    Type:  mcp.ServerTypeStreamableHTTP,
    URL:   "http://127.0.0.1:8093/",            // override via AURA_MCP_CALENDAR_URL
    Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP},
}
cli, _ := mcp.OpenServer(ctx, "calendar", server)        // initialize over .NET-MCP-HTTP
cli.Ping(ctx)                                            // ping OK
defs, _ := cli.ListTools(ctx)                            // 29 tools
cli.CallTool(ctx, "list_accounts", nil)                 // -> {"accounts": []}
// Aura mount: namespacing + Deferred
closer, mounted, _ := mcptools.MountManagedServer(ctx, reg, "calendar", server)
// mounted == 29, every name prefixed calendar__*, every Spec().Deferred == true
```

Expected forensic log: `initialize OK` → `ping OK` → `tools/list=29` → scaffold tools present →
`list_accounts -> {"accounts": []}` → tool-error on `get_calendar_events` (zero accounts) cleanly
surfaced through HTTP (model-self-correctable) → mount 29 `calendar__*` Deferred → `VALIDATED`.

### Building the .NET server from source (063 — kill-switch, dev only)

No prebuilt binaries are published (`gh release list -R MarimerLLC/calendar-mcp` is empty despite
the README's Releases table). Build locally from the .NET 10 SDK in WSL/Ubuntu 26.04:

```bash
# 1. only missing native dep is libicu (root, passwordless from host); do NOT use
#    DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1 — the server parses calendar dates/cultures
wsl -u root -e bash -lc 'apt-get update -qq && apt-get install -y libicu-dev'
# 2. .NET 10 SDK -> ~/.dotnet (no root, no machine-wide footprint, ~235 MB)
wsl -e bash -lc 'curl -fsSL https://dot.net/v1/dotnet-install.sh | bash -s -- --channel 10.0 --install-dir $HOME/.dotnet'
# 3. clone + publish the stdio server (framework-dependent apphost)
wsl -e bash -lc 'export PATH="$HOME/.dotnet:$PATH"; cd ~ && git clone --depth 1 https://github.com/MarimerLLC/calendar-mcp.git && cd calendar-mcp && dotnet publish src/CalendarMcp.StdioServer -c Release -o ~/calendar-mcp-publish'
# 4. raw MCP handshake probe (independent of Aura's Go client) — DOTNET_ROOT is MANDATORY here
wsl -e bash -lc 'export DOTNET_ROOT=$HOME/.dotnet; python3 .../063.../probe_mcp.py'
```

Source ground truth: `src/CalendarMcp.StdioServer/Program.cs` uses `ModelContextProtocol` **1.1.0**
(official MS/Anthropic C# SDK) over `Microsoft.Extensions.Hosting` with
`.WithStdioServerTransport()`, **29 `.WithTools<>` + 3 `.WithPrompts<>`**. Boots zero-config (only
*warns* if no `appsettings.json`, still starts with zero accounts). Serilog logs to a **file**
(`WriteTo.File`), NOT stderr — stdout stays clean for MCP framing. Tool names are explicit
snake_case via `[McpServerTool(Name="...")]`. Provider enum
(`ProviderServiceFactory.cs:44-46`) accepts `ics`/`icalendar`, `json`/`json-calendar`,
`imap`/`imap-smtp` + google/microsoft365/outlook.com (the `docs/configuration.md` "valid
providers" list is stale).

### Real-OAuth E2E (066 — consolidation proof, dev runtime)

The validated dev runtime is the **self-contained single-file Windows binary** (cross-published
from the WSL .NET 10 SDK): `dotnet publish -r win-x64 --self-contained -p:PublishSingleFile=true`.
Self-contained ⇒ no .NET install and **no `DOTNET_ROOT` gotcha** (unlike 063's framework-dependent
apphost), and OAuth browser-loopback / device-code flows work natively.

```powershell
# Server: D:\tmp\calendar-mcp-win\server\CalendarMcp.StdioServer.exe
# config at %LOCALAPPDATA%\CalendarMcp\appsettings.json (accounts + token caches)

# one-time per account: pre-write the account into appsettings.json, then prompt-free reauth
D:\tmp\calendar-mcp-win\cli\CalendarMcp.Cli.exe reauth google-personal    # browser consent
D:\tmp\calendar-mcp-win\cli\CalendarMcp.Cli.exe reauth outlook-personal   # device code at microsoft.com/devicelogin

# E2E via the reusable stdio harness (spawns server, handshakes once, runs a JSON step list)
cd D:\tmp\calendar-mcp-win ; python mcp_harness.py steps_google_e2e.json
python mcp_harness.py steps_outlook_e2e.json
```

`mcp_harness.py` + `steps_*.json` are in `sources/066-...`. Each step is
`{op: list|schema|call, tool, args}`. The Google E2E step list runs
`get_calendar_events`→`create_event`→read-back→`send_email`(self)→`search_emails`; the Outlook
list is the same minus the existing-events read. Proven tool arg shapes (real, from the step
files): `create_event` takes `{subject, start, end, accountId, calendarId, timeZone, location,
body}` with ISO-local datetimes (`2026-06-17T15:00:00`); `send_email` takes `{to:[...], subject,
body, bodyFormat:"text", accountId}`; `search_emails` takes `{query, accountId}`. The Spectre.Console
CLI is interactive and **refuses piped stdin** — `reauth <account-id>` is the prompt-free seam
(reads the account from `appsettings.json`, runs only the OAuth flow).

## What to Avoid

- **Do NOT expect prebuilt release binaries.** `gh release list` is empty despite the README's
  Releases table. The operator-preferred "prebuilt stdio binary" route is unavailable — build from
  source or the Docker image.
- **Do NOT omit `DOTNET_ROOT=$HOME/.dotnet`** for the WSL framework-dependent apphost (063). The
  SDK lives in non-standard `~/.dotnet`, so the apphost can't resolve the runtime → it silently
  exits and the MCP client hangs to timeout (looks like a protocol failure, isn't). Fix: export
  `DOTNET_ROOT`, or invoke `~/.dotnet/dotnet CalendarMcp.StdioServer.dll` directly. The 066
  self-contained binary sidesteps this entirely — prefer it for dev, and the Docker image for prod.
- **Do NOT register Google as a "Web application" OAuth client with `http://localhost:8642/authorize/`.**
  The upstream doc AND a code comment are both wrong. `GoogleAuthenticationService.cs:46` feeds
  that string to `LocalServerCodeReceiver(...)` whose `string` arg is the **close-page HTML, not
  the redirect**. The real redirect is an auto-assigned loopback **random port**
  (`http://127.0.0.1:<random>/authorize/`), which only a **Desktop app** client accepts (Google
  auto-allows loopback for installed apps). `redirect_uri_mismatch` fired twice before this was
  found. The runtime error is ground truth.
- **Do NOT leave Azure "Allow public client flows" off for Outlook.** Device-code requires a public
  client; otherwise `AADSTS70002` "client must be marked as 'mobile'". Set Authentication →
  Advanced → Allow public client flows = **Yes**, redirect under **Mobile & desktop applications**
  (NOT Web).
- **Do NOT assert Microsoft Graph email read-back immediately after send.** `search_emails`
  returned empty right after `send_email`; the message appeared after **~25s** (Graph
  search-indexing lag). Gmail is instant. Retry-with-backoff is required for Graph read-back
  assertions.
- **Do NOT pipe answers into the Spectre.Console account-add prompts** (`add-google-account` via
  piped stdin → "Failed to read input in non-interactive mode"). Pre-write `appsettings.json` +
  run prompt-free `reauth` instead.
- **Do NOT mount all 29 tools un-deferred.** 29 tools alone breach the 30-50-tool degradation
  cliff; combined with Aura's other tools it degrades selection. Always `Deferred: true`, and gate
  the destructive surface (`delete_email`/`bulk_delete_emails`/`delete_event`/`delete_contact`/
  `move_email`/`send_email`/`unsubscribe_from_email`/`update_*`) with `DenyRisk=write`. The fork
  trims the surface to ~16 server-side.
- **Do NOT ignore the Kiota CVE.** `Microsoft.Kiota.Abstractions` 1.15.2 carries a HIGH-severity
  CVE (NU1903, GHSA-7j59-v9qr-6fq9, the MS Graph HTTP layer) plus moderate OpenTelemetry CVEs
  (NU1902). The fork must patch Kiota. Adopting calendar-mcp pulls the full MS Graph + Google +
  MailKit stack (vs mail-mcp's lean Node SMTP/IMAP) — a binding consolidation consideration.

## Constraints

- **Versions/pins:** .NET 10 SDK **10.0.301** (via `dotnet-install.sh --channel 10.0`, ~235 MB);
  `ModelContextProtocol` C# SDK **1.1.0**; server reports `serverInfo: calendar-mcp 1.4.1`;
  negotiated MCP **protocolVersion 2025-06-18**, capabilities `tools`+`prompts`+`logging`.
- **Ports:** container internal **8080** (`ASPNETCORE_URLS=http://+:8080`); gate host map
  **127.0.0.1:8093**; MCP path **`/`**, health **`/health`**. Upstream Google doc port `8642` is a
  red herring (see What to Avoid).
- **Tool surface:** **29** snake_case tools advertised; 4 scaffold-required (`list_accounts`,
  `get_calendar_events`, `create_event`, `send_email`) all present; fork trims to **~16**.
- **Latency:** cold-start spawn→`initialize` **537ms** (warm OS cache) — far under the 30s
  `internal/mcp/calendar_integration_test.go` timeout. `dotnet publish` (StdioServer+Core) 36.77s
  incl. 29.67s first NuGet restore. Graph email indexing **~25s**; Gmail instant.
- **Native deps:** only `libicu` (`libicu78`) needs root; everything else is user-local in
  `~/.dotnet`. `DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1` is rejected (server parses cultures).
- **Build sizes:** framework-dependent publish 59 MB (apphost 78 KB + .dll 14.8 KB); self-contained
  win-x64 single-file is larger but standalone.
- **Env vars:** `CALENDAR_MCP_CONFIG` (config dir; empty → zero accounts, clean handshake),
  `CALENDAR_MCP_ADMIN_TOKEN` (Blazor admin / REST admin), `DOTNET_ROOT` (framework-dependent
  apphost only), test-tier `AURA_MCP_CALENDAR_URL` / `AURA_MCP_CALENDAR_SERVER_JSON`. Managed
  registration via `aura mcp add`/`install` → `~/.aura/mcp/servers.json`.
- **Known warnings:** `CS8602` nullable-deref in `ImapProviderService.cs:453` (IMAP path),
  non-blocking. The Docker image also ships a Blazor admin UI gated by `CALENDAR_MCP_ADMIN_TOKEN`.
- **Existing Aura scaffolds (pre-built to stand in for this server):**
  `internal/mcp/calendar_integration_test.go` (asserts the 4 tools, driven by
  `AURA_MCP_CALENDAR_SERVER_JSON`) and the `recipe:calendar` fixture placeholder in
  `internal/mcp/manager/catalog.go` (`aura-calendar-mcp-fixture`, never a real binary).

## Origin

Synthesized from spikes: **063** (calendar-mcp-build-launch), **064** (calendar-mcp-http-mount,
Phase-0 interop gate), **066** (calendar-oauth-e2e-google-outlook). Session 16 (2026-06-16).
Source files in: `sources/063-calendar-mcp-build-launch/` (README.md, probe_mcp.py),
`sources/064-calendar-mcp-http-mount/` (README.md, main.go),
`sources/066-calendar-oauth-e2e-google-outlook/` (README.md, mcp_harness.py, steps_google_e2e.json,
steps_outlook_e2e.json, steps_outlook_discover.json).
Verdicts: **063 VALIDATED**, **064 VALIDATED (gate GREEN)**, **066 VALIDATED**.
Fork decision banked in `docs/superpowers/specs/2026-06-16-calendar-pim-mcp-fork-design.md` +
`docs/superpowers/plans/2026-06-16-aura-pim-mcp-fork.md`; the `aura-pim-mcp` fork later shipped
(project memory `project_calendar_mcp_fork_aura_pim_mcp`: GHCR + compose + live CI; open item =
cockpit Connect UI, Google needs a Web OAuth client).
