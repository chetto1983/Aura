---
spike: 063
name: calendar-mcp-build-launch
type: standard
validates: "Given .NET 10 SDK absent (Win+WSL/Ubuntu 26.04) + Docker 29.5.3, when calendar-mcp is launched via the lowest-friction stdio route, then it answers a raw MCP initialize + tools/list advertising the calendar/email/contacts tools the scaffold asserts"
verdict: VALIDATED
related: [001, 002, 032, 064]
tags: [phase-9, calendar, mcp, dotnet, build-launch]
---

# Spike 063: calendar-mcp-build-launch

## What This Validates

Given `MarimerLLC/calendar-mcp` is a **C#/.NET 10** MCP server (the first .NET MCP
server for Aura — prior live mounts were Node mail-mcp 001 and Python whatsapp 002 /
agent-memory 032) and **no .NET 10 SDK is installed** on this machine (neither Windows
nor WSL/Ubuntu 26.04; Docker 29.5.3 present), when we obtain and launch it via the
lowest-friction stdio route, then it boots and answers a raw MCP `initialize` +
`tools/list` over stdio, advertising the tool names the existing
`internal/mcp/calendar_integration_test.go` scaffold asserts. This is the **kill-switch**:
if a .NET 10 MCP server can't be stood up here at all, the whole idea dies. The launch
route chosen here cascades into spike 064's mount path.

## Research

**Launch-route comparison** (operator chose "auto: prefer stdio binary"):

| Route | Mechanism | Transport | Pros | Cons | Status |
|-------|-----------|-----------|------|------|--------|
| Prebuilt self-contained binary | download from GitHub Releases | stdio | no SDK, no build | **none published** (`gh release list` empty despite README's table) | ✗ unavailable |
| .NET 10 SDK in WSL → `dotnet publish` stdio | `dotnet-install.sh --channel 10.0` to `~/.dotnet`, publish apphost | stdio | production-shape = catalog `recipe:calendar` stdio shape + `calendar_integration_test.go`; locally-built "stdio binary" recovers the preferred shape | one-time SDK provisioning (~235 MB) | **CHOSEN** |
| Docker HTTP server | repo `Dockerfile` (`sdk:10.0`→`aspnet:10.0`) | streamable-HTTP `:8080` | no host SDK; spike-032 mount path | diverges from stdio catalog recipe; Blazor admin UI + `CALENDAR_MCP_ADMIN_TOKEN` | fallback (unused) |

**Source ground truth** (cloned `D:/tmp/calendar-mcp`, also `~/calendar-mcp` in WSL):

- `src/CalendarMcp.StdioServer/Program.cs`: `ModelContextProtocol` **1.1.0** (official MS/Anthropic C# SDK) over `Microsoft.Extensions.Hosting`, `.WithStdioServerTransport()`, **29** `.WithTools<>` + 3 `.WithPrompts<>`. Boots with **zero config** — only *warns* if no `appsettings.json` (`Program.cs:71`), still starts with zero accounts.
- Logs go to a **file** via Serilog (`WriteTo.File`, `Program.cs:35`), NOT stderr (the StdioServer README's "logs to stderr" line is stale) → stdout stays clean for MCP framing.
- `src/CalendarMcp.Core/Providers/ProviderServiceFactory.cs:44-46`: provider enum accepts `ics`/`icalendar`, `json`/`json-calendar`, `imap`/`imap-smtp` + google/microsoft365/outlook.com. `docs/configuration.md:489` "valid providers" list (only the 3 cloud providers) is **stale**.
- Tool names are explicit snake_case via `[McpServerTool(Name="...")]`.

## How to Run

One-time toolchain provisioning (WSL/Ubuntu 26.04):

```bash
# libicu is the only native dep .NET 10 needs that's missing (root, passwordless from host)
wsl -u root -e bash -lc 'apt-get update -qq && apt-get install -y libicu-dev'

# .NET 10 SDK -> ~/.dotnet (no root, no machine-wide footprint)
wsl -e bash -lc 'curl -fsSL https://dot.net/v1/dotnet-install.sh | bash -s -- --channel 10.0 --install-dir $HOME/.dotnet'

# clone + publish the stdio server (framework-dependent apphost)
wsl -e bash -lc 'export PATH="$HOME/.dotnet:$PATH"; cd ~ && git clone --depth 1 https://github.com/MarimerLLC/calendar-mcp.git && cd calendar-mcp && dotnet publish src/CalendarMcp.StdioServer -c Release -o ~/calendar-mcp-publish'
```

Raw MCP handshake probe (independent of Aura's Go client):

```bash
wsl -e bash -lc 'export DOTNET_ROOT=$HOME/.dotnet; python3 /mnt/d/Aura/.planning/spikes/063-calendar-mcp-build-launch/probe_mcp.py'
```

## What to Expect

Forensic log: process spawns → `initialize` returns protocolVersion `2025-06-18` +
serverInfo `calendar-mcp 1.4.1` in ~0.5s → `tools/list` returns 29 snake_case tools →
`list_accounts` callable with zero accounts (`{"accounts": []}`) → `SUMMARY VALIDATED`,
exit 0.

## Investigation Trail

1. **`gh release list -R MarimerLLC/calendar-mcp` → empty.** No prebuilt binaries despite the README's Releases table → the operator-preferred "prebuilt stdio binary" route is unavailable. Pivot to building one locally from the .NET 10 SDK.
2. **libicu missing** (`ls /usr/lib/.../libicu*` empty) — .NET globalization needs it. Installed `libicu78` as root. (Alternative `DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1` rejected — the server parses calendar dates/cultures.)
3. **`dotnet-install.sh --channel 10.0` → SDK 10.0.301** installed to `~/.dotnet`. 235 MB download. No root needed.
4. **`dotnet publish src/CalendarMcp.StdioServer -c Release` → SUCCESS** in 36.77s (incl. 29.67s first NuGet restore: MSAL, Google.Apis, MailKit, Ical.Net, ModelContextProtocol, OpenTelemetry, Serilog). Output: framework-dependent, 59 MB, apphost `CalendarMcp.StdioServer` (78 KB) + `.dll` (14.8 KB). Warnings recorded under Results.
5. **Run 1 of probe → FAIL: no initialize response in 40s.** Not a protocol failure — the probe set `stderr=DEVNULL` and waited on a dead pipe. Running the apphost manually surfaced the cause: `You must install .NET to run this application … .NET location: Not found`. The SDK lives in **non-standard `~/.dotnet`**, so the framework-dependent apphost can't resolve the runtime.
6. **Run 2 with `DOTNET_ROOT=$HOME/.dotnet` → full green.** initialize 537ms, 29 tools, all 4 scaffold-expected present, `list_accounts` clean. (Equivalent fix: invoke `~/.dotnet/dotnet CalendarMcp.StdioServer.dll` — `dotnet` on PATH self-resolves.)

## Results

**VALIDATED ✓** — a .NET 10 MCP server can be stood up and speaks conformant MCP in this environment.

| Link | Evidence |
|---|---|
| .NET 10 toolchain feasible | SDK 10.0.301 via `dotnet-install.sh` to `~/.dotnet`, only `libicu78` needed root |
| Compiles on Ubuntu 26.04 | `dotnet publish` StdioServer+Core OK, 36.77s (restore-cached rebuilds faster) |
| Boots zero-config | empty `CALENDAR_MCP_CONFIG` dir → starts, no creds |
| MCP wire handshake | `initialize` → protocolVersion **2025-06-18**, capabilities `tools`+`prompts`+`logging`, serverInfo `calendar-mcp 1.4.1` |
| cold-start latency | **537ms** spawn→initialize (warm OS cache) — far under the 30s `calendar_integration_test.go` timeout |
| tool surface | **29** snake_case tools |
| scaffold tools present | `list_accounts`, `get_calendar_events`, `create_event`, `send_email` all advertised |
| `list_accounts` callable | zero accounts → `{"accounts": []}` (clean result, not error) — matches the scaffold's non-empty-content assert |
| stdout clean | Serilog→file; no log lines pollute the JSON-RPC stream |

**Critical findings for the build (cascade into 064 + the catalog recipe):**

1. **`DOTNET_ROOT=$HOME/.dotnet` is mandatory** for the WSL stdio launch (SDK is in a non-standard dir). The catalog `recipe:calendar` stdio command must be either `wsl -e bash -lc 'export DOTNET_ROOT=$HOME/.dotnet && ~/calendar-mcp-publish/CalendarMcp.StdioServer'` or `wsl … ~/.dotnet/dotnet ~/calendar-mcp-publish/CalendarMcp.StdioServer.dll`. Omit it → silent boot failure (apphost exits, client hangs to timeout).
2. **Supply-chain: a known HIGH-severity CVE rides the dependency tree** — `Microsoft.Kiota.Abstractions` 1.15.2 (NU1903, GHSA-7j59-v9qr-6fq9, the MS Graph HTTP layer) + moderate OpenTelemetry CVEs (NU1902). Binding consideration for the consolidation decision (adopting calendar-mcp pulls the full MS Graph + Google + MailKit stack vs mail-mcp's lean Node SMTP/IMAP).
3. **29 tools alone breaches the 30-50-tool degradation cliff** (8.1) when combined with Aura's in-repo + other-MCP tools → MUST mount `Deferred: true` (spike 001 finding #1 re-confirmed), and the destructive surface (`delete_email`/`bulk_delete_emails`/`delete_event`/`delete_contact`/`move_email`/`send_email`/`unsubscribe_from_email`/`update_*`) demands the `DenyRisk=write` allowlist (064).
4. **Minor:** `CS8602` nullable-deref warning in `ImapProviderService.cs:453` (the IMAP path 065 exercises) — non-blocking, noted.

The .NET MCP server runs. Whether **Aura's Go MCP client** can interop with the .NET SDK 1.1.0 (protocol 2025-06-18 negotiation across a third SDK family) is spike 064.
