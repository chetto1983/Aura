# Cockpit "Connect" integration contracts — WhatsApp + Calendar (Phase-4 connect)

Grounding for the in-cockpit account-linking surface (operator directive 2026-06-21:
"can't setup whatsapp and calendar"). Decisions: **Connect UI inline in the Governance
MCP board**; **WhatsApp first, Calendar after** (pause for Google client + stable HTTPS).

Sources read: `Skill(spike-findings-Aura)` → `references/mcp-live-servers.md` (spikes 001/002);
forks in `D:\tmp\aura-whatsapp-mcp` (`aura/cockpit-connect`, commit 224c579) and
`D:\tmp\aura-pim-mcp` (`aura/pim-sidecar`).

## Current state (2026-06-21)

- `aura-whatsapp` sidecar IS in `compose.yaml` (image `ghcr.io/chetto1983/whatsapp-mcp:sidecar`,
  host `8092`→MCP `:8080`, host `8094`→bridge `:8081`) but was **never started**. Now started,
  healthy, `state: waiting_qr`. Stopgap pairing: render `/api/qr` `code` → PNG (see
  `D:\tmp\genqr.py`).
- `aura-pim-mcp` (calendar) sidecar is **NOT in compose.yaml** — only integration tests +
  the caddy `/admin/auth/google/callback` → `aura-pim-mcp:8080` route reference it.
- No `/api/connect` or `/admin` proxy route exists in the Aura Go server (the cockpit-connect
  Go admin-proxy is unbuilt).
- Cockpit offers `whatsapp` + `calendar` recipes (`McpInstallPanel` RECIPES →
  `internal/mcp/manager/catalog.go` BuiltInCatalog), so config writes succeed but the
  integrations can't function until linked.

## WhatsApp bridge REST contract (host `8094` → container `:8081`)

- `GET /api/status` → `{"state":"initializing|waiting_qr|connected","paired":bool,"jid":string,"connected":bool}`
- `GET /api/qr` → `{"code":"<raw wa.me linked_devices payload>"}` | 409 `{"error":"already paired"}` | 503 `{"error":"qr not ready"}`. **QR rotates ~20-60s**; `code` is a raw payload the client must render as a QR image.
- `POST /api/logout` → `{"success":true}`.
- MCP-over-HTTP at host `8092`→`:8080` path `/mcp/`. Tools: read (search_contacts, list_messages, list_chats, get_chat, …) + write (send_message, send_file, send_audio_message). Mount **Deferred** (spike: 16+10 non-deferred tools degrade the manifest).
- Session persists in `/app/whatsapp-bridge/store/whatsapp.db` after one pairing.

## Calendar (aura-pim-mcp) contract (.NET, container `:8080`)

- `/admin/*` token-gated by `CALENDAR_MCP_ADMIN_TOKEN` (Bearer or `X-Admin-Token`); only `GET /admin/auth/google/callback` is token-exempt.
- Google: `GET /admin/auth/{accountId}/google/start` → `{authUrl,redirectUri}` → operator consents → callback `GET /admin/auth/google/callback?code&state` (caddy already proxies this to `aura-pim-mcp:8080`) → poll `GET /admin/accounts/{accountId}/status` → `{accountId,displayName,provider,enabled,authFlow}` (`authFlow:null` = linked).
- Account CRUD: `POST /admin/accounts {id,displayName,provider,domains,enabled,priority,providerConfig{clientId,clientSecret}}`; `DELETE /admin/accounts/{id}?logout=true`; `POST /admin/accounts/{id}/logout`.
- Outlook device-code: `POST /admin/auth/{id}/start` → `{userCode,verificationUrl,message,expiresIn}`; poll `/admin/auth/{id}/status`.
- MCP-over-HTTP on `:8080` root `/`. 14 trimmed tools (emails/calendars/contacts read + create/respond/update event).
- Env: `CALENDAR_MCP_ADMIN_TOKEN`, `CALENDARMCP__EXTERNALBASEURL` (builds the Google redirect URI), `CALENDAR_MCP_CONFIG=/app/data`, `ASPNETCORE_URLS=http://+:8080`. Volume `/app/data`. Health `GET /health`.
- **Operator-provided**: a Google Cloud OAuth client (clientId/secret, APIs enabled, consent published) registered with redirect `{EXTERNALBASEURL}/admin/auth/google/callback`, AND a **stable public HTTPS URL** (a Cloudflare *quick* tunnel is ephemeral — Google needs a fixed redirect URI).

## Implementation plan

### Backend (Aura Go server, `internal/agui`)
New `connect_api.go` + `registerConnectRoutes(mux)` (call from `server.go` BuildHandler), mounted in `cmd/aura/serve_webui.go` behind `RequireCapability(governance.write)` (operator action), pattern = `registerGovernanceWriteRoutes` + the `image_proxy.go` outbound-HTTP shape:
- `GET /api/connect/whatsapp/status` → forward bridge `/api/status` (JSON passthrough).
- `GET /api/connect/whatsapp/qr.png` → fetch bridge `/api/qr`, **render server-side** via `rsc.io/qr` → `image/png` (no frontend QR dep); 409/503 passthrough so the UI knows paired/not-ready.
- `POST /api/connect/whatsapp/logout` → forward bridge `/api/logout`.
- Bridge base URL: `Server.whatsappBridgeURL` via `SetWhatsAppBridge`, from `AURA_WHATSAPP_BRIDGE_URL` (default `http://whatsapp:8081`). Unset → 503 (graceful).
- Calendar (later): `/api/connect/pim/admin/*` forward to `aura-pim-mcp:8080/admin/*` injecting `CALENDAR_MCP_ADMIN_TOKEN`, exempting the Google callback (caddy already handles it).
- Tests: handler tests with a scripted fake bridge (status/qr/logout) + capability-gate sweep (parity `governance_write_auth_sweep_test.go`).

### Frontend (cockpit, inline in Governance MCP board)
- `governanceApi.ts`: `whatsappStatus()`, `whatsappLogout()`, plus `qr.png` URL with cache-bust.
- `McpServerDetail.tsx`: when the selected server is the WhatsApp one (detect by recipe/source), render a "Link device" section: poll status (5s) → if `waiting_qr` show `<img src="/api/connect/whatsapp/qr.png?ts=…">` refreshed ~3s + instructions; if `connected` show JID + "Unlink"; bridge-offline → graceful note.
- i18n keys en+it. Tests + ≥85% coverage + Stryker.

### Compose / infra
- Make `aura-whatsapp` come up with the stack (add to `aura` depends_on or document `docker compose up -d whatsapp`).
- Add `aura-pim-mcp` service (image `ghcr.io/chetto1983/aura-pim-mcp`, host `8093`→`:8080`, env CALENDAR_MCP_ADMIN_TOKEN/EXTERNALBASEURL/CONFIG, volume `pim-data:/app/data`, health `/health`).

### Validate
Go: build/vet/test (+race) in WSL/container. Web: typecheck/lint/test. Rebuild dist (`npm run build` → `internal/webui/dist`) + `docker compose build aura` + recreate. Live E2E (governance-write.spec.ts) against `aura.localhost:9080` via the Playwright container.

## Caution
A concurrent Codex session is mutating the repo (it reverted uncommitted edits + committed `4a8efec6`). Commit each increment atomically; ideally pause the parallel session before the multi-file Connect build.
