# Design: `aura-pim-mcp` — forked PIM (mail + calendar + contacts) sidecar

- **Date:** 2026-06-16
- **Status:** Approved (brainstorm) — pending implementation plan
- **Author:** Davide + Claude
- **Supersedes / relates to:** MANIFEST Session-16 consolidation requirement; spikes 063 (build/launch) + the live Google/Outlook E2E (2026-06-16); `recipe:calendar` fixture in [catalog.go](../../../internal/mcp/manager/catalog.go) and [calendar_integration_test.go](../../../internal/mcp/calendar_integration_test.go).

## 1. Background & motivation

`MarimerLLC/calendar-mcp` is a C#/.NET 10 MCP server providing **mail + calendar + contacts** across Microsoft 365, Outlook.com, Google Workspace/Gmail, IMAP/SMTP, ICS feeds, and JSON files. During the 2026-06-16 spike session we proved end-to-end (live, through the published server) that:

- It builds on .NET 10 in WSL and speaks MCP (protocol `2025-06-18`, 29 tools) — spike 063.
- A **full E2E passes for both Google and Outlook**: list/create/read-back calendar events **and** send/search self-emails, all through the server.

Two product problems make adopting it *as-is* wrong for Aura:

1. **End-user onboarding pain.** The upstream Google desktop-loopback OAuth has a bug (`LocalServerCodeReceiver` is fed a URL as its close-page text, so the redirect is a random `127.0.0.1:<port>` → needs a *Desktop* OAuth client, contradicting the docs). The upstream "easy connect" surface is its own Blazor admin UI, which Aura does not want — **Aura's own frontend will own all connect/management UI.**
2. **Fit.** 29 tools breach the 30-50-tool degradation cliff; the dependency tree carries a **HIGH-severity CVE** (`Microsoft.Kiota.Abstractions` 1.15.2, GHSA-7j59-v9qr-6fq9); and it overlaps the existing `mail` server (spike 001), which we want to **consolidate away**.

## 2. Goals (locked via brainstorm)

- **Unified PIM** — one server for mail + calendar + contacts; **retire the standalone mail-mcp** (spike 001).
- **Trim + Aura-fit** — reduce the tool surface; align mounting/policy; strip what Aura won't use.
- **All connect/management UI in Aura's own frontend** (the v1.0.0 cockpit) — strip the upstream Blazor admin UI; keep its REST admin API.
- **Thin fork, track upstream** — minimal C# diffs so upstream updates remain mergeable.
- **HTTP sidecar runtime** — REST admin API for the cockpit + MCP-over-HTTP for the agent.
- **Per-deployment OAuth** — the installer configures each customer's OAuth client IDs once via Aura's admin UI; end users then just "Connect → sign in".

### Non-goals

- No baked-in Aura publisher OAuth apps / Google CASA verification for v1 (per-deployment config instead).
- No Go rewrite of the PIM providers (forking the working C# server is cheaper).
- No deep rearchitecture (thin fork only).
- No new provider integrations beyond what upstream ships.

## 3. Architecture

```
Aura cockpit (frontend)
   │  REST (no token in browser)
   ▼
Aura Go backend  ──(admin token)──▶  aura-pim-mcp  127.0.0.1:8093  /admin/*     configure OAuth · connect (Google web-redirect / MS device-code) · list · test · remove
Aura agent loop  ──mcptools.MountManagedServer (streamable-HTTP, Deferred + DenyRisk=write)──▶  aura-pim-mcp  127.0.0.1:8093  /   (MCP tools)
                                                          token cache + appsettings.json → sidecar data volume (DataProtection-encrypted at rest)
```

- Loopback-bound like the existing `agent-memory` (`:8091`) and embed (`:8081`) sidecars.
- Admin REST API is token-gated; the token lives only in Aura's secrets and Go backend, never in the browser (the backend proxies cockpit calls).
- Forked repo (`aura-pim-mcp`, branch `aura/pim-sidecar`) builds a sidecar image referenced from `compose.yaml`; **no C# vendored into the Aura repo** — reproduction recipe + image pin live in-repo (mirrors how agent-memory was adopted).

## 4. Thin-fork C# changes

| Change | Detail |
|---|---|
| Remove Blazor UI | Delete `Components/`, `BlazorAdmin/`, and the Razor/cookie wiring in `HttpServer/Program.cs` (`AddRazorComponents`, `MapRazorComponents`, cookie auth). **Keep** `AdminEndpoints`, `AccountConfigurationService`, `GoogleOAuthManager`, `DeviceCodeAuthManager`, `AdminAuthMiddleware`. |
| Patch CVE | Pin `Microsoft.Kiota.Abstractions` to a fixed version (direct `PackageReference`), re-verify `dotnet list package --vulnerable` is clean. |
| Trim tools | Remove the dropped `.WithTools<>` lines in `HttpServer/Program.cs` (see §5). |
| Drop unused | Remove the LLM "smart routing" Router (Ollama/OpenAI/Anthropic) + `GetContextualEmailSummaryTool`; trim surplus telemetry exporters (keep optional OTLP). |
| Per-deployment OAuth | Keep the existing admin REST endpoints that the Blazor AddAccount page used (configure provider clientId/secret/tenantId); Aura consumes them. Verify the surface covers: configure-account, start-connect, complete/callback, list, test, remove. |

## 5. Trimmed tool surface (29 → ~16)

**Keep:** `list_accounts`, `list_calendars`, `get_calendar_events`, `get_calendar_event_details`, `find_available_times`, `create_event`, `update_event`, `respond_to_event`, `get_emails`, `search_emails`, `get_email_details`, `send_email`, `get_contacts`, `search_contacts`, `get_contact_details`.

**Drop:** `bulk_delete_emails`, `bulk_mark_emails_as_read`, `bulk_move_emails`, `delete_email`, `mark_email_as_read`, `move_email`, `get_email_attachment`, `get_contextual_email_summary`, `get_unsubscribe_info`, `unsubscribe_from_email`, `create_contact`, `update_contact`, `delete_contact`, `get_guide`.

**Off by default, re-enablable + DenyRisk-gated:** `delete_event`, `delete_contact`.

## 6. Aura-side integration

- **compose.yaml:** add `aura-pim-mcp` (image from fork), bind `127.0.0.1:8093`, data volume for tokens + appsettings, env `CALENDAR_MCP_ADMIN_TOKEN` (Aura secret), `ASPNETCORE_URLS=http://+:8093`.
- **Agent mount:** `mcptools.MountManagedServer` streamable-HTTP → `http://127.0.0.1:8093/<mcp-path>`, `Deferred:true`, `DenyRisk=write` policy. Confirm the MCP path (`/` vs `/mcp`) against the fork's `app.MapMcp()`.
- **Catalog recipe:** change `recipe:calendar` ([catalog.go](../../../internal/mcp/manager/catalog.go)) from the `aura-calendar-mcp-fixture` stdio placeholder to this HTTP sidecar; update [calendar_integration_test.go](../../../internal/mcp/calendar_integration_test.go) to drive it live (streamable-HTTP variant of `AURA_MCP_CALENDAR_SERVER_JSON`).
- **Go backend admin proxy:** thin Aura endpoints forwarding to the sidecar `/admin/*` with the token, so the cockpit can configure OAuth + connect accounts without touching the token.
- **Frontend (cockpit):** an "Integrations / Connect" surface — installer configures OAuth client IDs per provider once; users click **Connect** (Google → browser web-redirect; Microsoft → device code rendered in the cockpit).
- **mail-mcp cutover (phased):** run both during transition → validate calendar-mcp's email path per account → retire mail-mcp from `~/.aura/mcp/servers.json` + catalog. Honors MANIFEST Session-16.

## 7. Security & policy

- **DenyRisk=write** at mount + trimmed surface (defense in depth).
- Admin token only in Aura secrets / Go backend; never in the browser.
- OAuth secrets + tokens encrypted at rest (DataProtection) in the sidecar data volume; never in the Aura repo.
- Sidecar bound to loopback; admin API token-gated.
- Google connect uses the registered **web redirect** (`/admin/auth/google/callback`) — avoids the desktop-loopback random-port bug; the per-deployment installer registers that callback in the customer's OAuth client (documented step).

## 8. Validation plan

1. **Interop gate (first):** prove **.NET MCP-over-HTTP/SSE ↔ Aura's streamable-HTTP Go client** interop (we proved .NET-over-stdio in 063; spike 032 proved HTTP only against Python FastMCP — .NET-over-HTTP is unproven). A Go mount harness (the paused spike 064) — handshake, `tools/list`, namespacing as `calendar__*`, `Deferred`, `DenyRisk=write` blocks the destructive tools.
2. **Live E2E through Aura's bridge:** the same create/read-back/send/search we ran today, but via `mcptools` + the bridged `Execute` path, for **both** Google + Outlook.
3. **Admin REST flow:** configure OAuth → start-connect → list/test, against the sidecar.
4. `calendar_integration_test.go` green live; CI integration tier wired (no skip-as-green).

## 9. Risks & open items

- **.NET MCP HTTP/SSE ↔ Aura client interop unproven** — gate #1 above; if it fails, fall back to stdio mount for the agent + REST admin API still over HTTP (hybrid).
- C# maintenance burden in a Go shop — mitigated by the thin fork tracking upstream.
- Per-deployment Google web-redirect registration is a manual installer step — document in the onboarding runbook.
- MCP endpoint path + exact admin REST surface to confirm against the fork during planning.
- Token-cache portability across appliance image rebuilds (data volume must persist).

## 10. Decisions log (this brainstorm)

| Decision | Choice |
|---|---|
| Fork goals | Unified PIM (replace mail-mcp) + trim/Aura-fit + all UI in Aura frontend |
| Runtime | HTTP sidecar (REST admin API + MCP-over-HTTP) |
| OAuth provisioning | Per-deployment, set once by installer |
| Fork depth | Thin fork, track upstream |
| Fork location | Forked git repo + compose sidecar (no C# vendored in Aura) |
