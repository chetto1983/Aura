# aura-pim-mcp Fork Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fork `MarimerLLC/calendar-mcp` into a thin-fork unified mail+calendar+contacts HTTP sidecar (`aura-pim-mcp`) that Aura mounts for the agent and whose connect/management is driven by Aura's own frontend, replacing the standalone mail-mcp.

**Architecture:** HTTP sidecar on `127.0.0.1:8093` exposing (a) a token-gated REST admin API the Aura cockpit drives for OAuth config + connect, and (b) an MCP-over-HTTP endpoint the agent mounts (Deferred + DenyRisk=write). Thin fork: strip the Blazor UI, trim tools, patch the Kiota CVE; keep close to upstream. Per-deployment OAuth config (installer sets client IDs once).

**Tech Stack:** C#/.NET 10 (forked server), Go (Aura `internal/mcp` + `internal/agent/mcptools`), Docker Compose, the v1.0.0 cockpit (React/assistant-ui) for the connect UI.

**Spec:** [docs/superpowers/specs/2026-06-16-calendar-pim-mcp-fork-design.md](../specs/2026-06-16-calendar-pim-mcp-fork-design.md)

**Gating note:** Phase 0 is an architecture-deciding validation gate. Phases 0–1 below are fully detailed; Phases 2–5 are an outline to be expanded into their own plan after Phase 0.

**✅ GATE RESULT (2026-06-16): GREEN.** Ran via the **Linux HTTP sidecar container** (corrected from the original Windows-binary wording — Aura is multi-host). `.NET MCP-over-HTTP ↔ Aura's Go streamable-HTTP client` interops fully: initialize + ping + `tools/list=29` + real `tools/call` (`list_accounts`) + clean tool-error propagation + `mcptools` mount (29 `calendar__*`, all Deferred). **Decision: the agent mounts over streamable-HTTP — no stdio fallback.** Evidence: `.planning/spikes/064-calendar-mcp-http-mount/`.

**Environment prerequisites (already in place from the 2026-06-16 spike session):**
- WSL Ubuntu: .NET 10.0.301 SDK at `~/.dotnet` (needs `export PATH="$HOME/.dotnet:$PATH"; export DOTNET_ROOT=$HOME/.dotnet`), upstream clone at `~/calendar-mcp`, libicu78 installed.
- Connected accounts (`google-personal`, `outlook-personal`) with cached tokens + config at `C:\Users\Davide\AppData\Local\CalendarMcp\appsettings.json`.
- Aura Go module path: `github.com/chetto1983/aura`. Streamable-HTTP mount precedent: `.planning/spikes/032-agent-memory-mcp-live-mount/main.go`.

---

## Phase 0 — Interop validation gate (.NET MCP-over-HTTP ↔ Aura Go client)

**Why:** Aura's Go MCP client is proven against the .NET server over **stdio** (spike 063) and against Python FastMCP over **streamable-HTTP** (spike 032), but never against **.NET over HTTP**. This gate decides whether the agent mounts over HTTP (preferred) or must fall back to stdio. Uses the UPSTREAM server (no fork needed yet).

**Files:**
- Create: `.planning/spikes/064-calendar-mcp-http-mount/main.go` (Go mount harness)
- Create: `.planning/spikes/064-calendar-mcp-http-mount/README.md`
- Reference: `internal/agent/mcptools/mount.go`, `internal/mcp/managed_config.go`, `.planning/spikes/032-agent-memory-mcp-live-mount/main.go`

- [x] **Step 1: Build the Linux HTTP sidecar image (upstream Dockerfile)** — DONE 2026-06-16

Aura is multi-host (Linux appliance), so the gate target is the Docker sidecar, NOT a Windows binary. Run (PowerShell — docker from PowerShell, not Git-Bash):
```powershell
docker build -t aura-pim-mcp:gate -f D:\tmp\calendar-mcp\Dockerfile D:\tmp\calendar-mcp
```
Expected: image `aura-pim-mcp:gate` built (`sdk:10.0` → `aspnet:10.0`).

- [x] **Step 2: Run the sidecar container** — DONE 2026-06-16

```powershell
docker run -d --rm --name aura-pim-gate -p 127.0.0.1:8093:8080 -e CALENDAR_MCP_ADMIN_TOKEN=aura-dev-admin-token aura-pim-mcp:gate
```
Container internal port is **8080** (Dockerfile `ASPNETCORE_URLS=http://+:8080`), mapped to host `127.0.0.1:8093`. Empty config (zero accounts) is fine for the interop gate. Expected logs: `listening on http://[::]:8080`, `MCP endpoint:  /`.

- [ ] **Step 3: Confirm the MCP endpoint path + health (curl)**

Run:
```bash
curl -s http://127.0.0.1:8093/health
```
Expected: a 200/health JSON. Note the exact MCP path the server logs (`/` per `HttpServer/Program.cs:236`); if it differs, use the logged path in Step 4.

- [ ] **Step 4: Write the Go mount harness**

Adapt the spike-032 pattern (`internal/agent/mcptools` + `internal/mcp`). Read `internal/agent/mcptools/mount.go` first to confirm the exact `MountManagedServer` signature and whether a policy-aware variant exists; if DenyRisk is applied via the managed-config `toolPolicy` field rather than a function arg, set it on the `mcp.ManagedServer` value.

Create `.planning/spikes/064-calendar-mcp-http-mount/main.go`:
```go
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

func logf(cat, f string, a ...any) { fmt.Printf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), cat, fmt.Sprintf(f, a...)) }
func failf(f string, a ...any)     { logf("ERROR", f, a...); os.Exit(1) }

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	url := os.Getenv("AURA_MCP_CALENDAR_URL")
	if strings.TrimSpace(url) == "" {
		url = "http://127.0.0.1:8093/" // confirmed MCP path from Phase 0 Step 3
	}
	server := mcp.ManagedServer{
		Type:  mcp.ServerTypeStreamableHTTP,
		URL:   url,
		Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP},
	}

	cli, err := mcp.OpenServer(ctx, "calendar", server)
	if err != nil {
		failf("open streamable-http (.NET) MCP: %v", err)
	}
	defer func() { _ = cli.Close() }()
	logf("MCP", "initialize OK (.NET MCP over HTTP)")

	defs, err := cli.ListTools(ctx)
	if err != nil {
		failf("tools/list: %v", err)
	}
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Name)
	}
	sort.Strings(names)
	logf("MCP", "tools/list=%d: %s", len(names), strings.Join(names, ", "))

	for _, want := range []string{"list_accounts", "get_calendar_events", "create_event", "send_email"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			failf("missing expected tool %q", want)
		}
	}

	reg := tools.NewRegistry()
	closer, mounted, err := mcptools.MountManagedServer(ctx, reg, "calendar", server)
	if err != nil {
		failf("mount: %v", err)
	}
	defer func() { _ = closer() }()

	allDeferred := true
	for _, n := range mounted {
		t, ok := reg.Get(n)
		if !ok {
			failf("mounted tool %q missing from registry", n)
		}
		if !t.Spec().Deferred {
			allDeferred = false
		}
		if !strings.HasPrefix(n, "calendar__") {
			failf("mounted tool %q not namespaced calendar__*", n)
		}
	}
	logf("MOUNT", "mounted=%d allDeferred=%v", len(mounted), allDeferred)
	if !allDeferred {
		failf("mounted tools must be Deferred")
	}
	logf("SUMMARY", "VALIDATED .NET MCP-over-HTTP ↔ Aura Go client: %d tools mounted calendar__*", len(mounted))
}
```

- [ ] **Step 5: Run the harness**

Run:
```bash
cd /d/Aura && go run ./.planning/spikes/064-calendar-mcp-http-mount
```
Expected: `initialize OK` → `tools/list=29` → `SUMMARY VALIDATED`. If `OpenServer` errors on initialize or `ListTools` returns nothing, the HTTP transport is incompatible → **GATE RED** (record the error; agent mount falls back to stdio in Phase 3).

- [ ] **Step 6: Add a bridged Execute read (ground truth, not just listing)**

Append to `main.go` after the mount: call the bridged `calendar__get_calendar_events` through Aura's tool-execution path. Read `internal/agent/tools` for `WithToolCallContext` + `NewResult` + the tool `Execute` signature first (the spike-001 finding: `Execute` requires `ctx = tools.WithToolCallContext(ctx, session, callID, runDir, previewCap)`), then invoke and assert the result contains an event or a clean empty result.

- [ ] **Step 7: Run again, write the README + verdict**

Run the harness; write `.planning/spikes/064-calendar-mcp-http-mount/README.md` with frontmatter, the transport finding (HTTP works / falls back to stdio), tool count, and the bridged-read evidence.

- [ ] **Step 8: Commit (scoped pathspec — concurrent Codex session)**

```bash
git add .planning/spikes/064-calendar-mcp-http-mount/
git commit .planning/spikes/064-calendar-mcp-http-mount/ -m "docs(spike-064): [VERDICT] .NET MCP-over-HTTP <-> Aura Go client interop"
```

**GATE DECISION:** GREEN → Phase 3 mounts the agent over HTTP. RED → Phase 3 mounts the agent over stdio (Aura spawns `CalendarMcp.StdioServer.exe`), while the REST admin API still runs over HTTP for the cockpit.

---

## Phase 1 — Thin C# fork (`aura-pim-mcp`)

**Files (in the fork, WSL `~/aura-pim-mcp`):**
- Fork repo: `chetto1983/aura-pim-mcp` (branch `aura/pim-sidecar`)
- Delete: `src/CalendarMcp.HttpServer/Components/`, `src/CalendarMcp.HttpServer/BlazorAdmin/`
- Modify: `src/CalendarMcp.HttpServer/Program.cs` (remove Razor wiring + trim tools)
- Modify: `src/CalendarMcp.Core/CalendarMcp.Core.csproj` (pin Kiota), `src/CalendarMcp.HttpServer/CalendarMcp.HttpServer.csproj`

- [ ] **Step 1: Fork + branch**

```bash
gh repo fork MarimerLLC/calendar-mcp --clone=false --fork-name aura-pim-mcp
wsl -e bash -lc 'cd ~ && git clone https://github.com/chetto1983/aura-pim-mcp.git && cd aura-pim-mcp && git checkout -b aura/pim-sidecar'
```
Expected: fork exists, branch created.

- [ ] **Step 2: Patch the Kiota HIGH-sev CVE**

Add a direct pin in `src/CalendarMcp.Core/CalendarMcp.Core.csproj` `<ItemGroup>`:
```xml
<PackageReference Include="Microsoft.Kiota.Abstractions" Version="1.20.0" />
```
(Confirm the lowest non-vulnerable version via `dotnet list package --vulnerable` — bump until clean.)

- [ ] **Step 3: Verify CVE cleared**

```bash
wsl -e bash -lc 'export PATH="$HOME/.dotnet:$PATH"; export DOTNET_ROOT=$HOME/.dotnet; cd ~/aura-pim-mcp && dotnet restore src/CalendarMcp.HttpServer && dotnet list src/CalendarMcp.HttpServer package --vulnerable --include-transitive'
```
Expected: no HIGH-severity advisories (GHSA-7j59-v9qr-6fq9 gone).

- [ ] **Step 4: Remove the Blazor admin UI, keep the REST admin API**

Delete `src/CalendarMcp.HttpServer/Components/` and `src/CalendarMcp.HttpServer/BlazorAdmin/`. In `src/CalendarMcp.HttpServer/Program.cs` remove: `AddRazorComponents`/`AddInteractiveServerComponents`, the cookie `AddAuthentication`/`AddCookie` for the UI, `AddCascadingAuthenticationState`, `AddScoped<AuthenticationStateProvider,...>`, `app.MapRazorComponents<...>()`, `MapStaticAssets`, `app.MapAdminAuthEndpoints()`. KEEP: `app.MapAdminEndpoints()`, `AdminAuthMiddleware`, `IAccountConfigurationService`, `DeviceCodeAuthManager`, `GoogleOAuthManager`, `app.MapMcp()`, `MapHealthEndpoints`, `MapAttachmentEndpoints`.

- [ ] **Step 5: Trim the tool surface**

In `Program.cs`, remove the `.WithTools<>` lines for the dropped tools (spec §5): `DeleteEmailTool`, `MarkEmailAsReadTool`, `MoveEmailTool`, `BulkDeleteEmailsTool`, `BulkMarkEmailsAsReadTool`, `BulkMoveEmailsTool`, `GetEmailAttachmentTool`, `GetContextualEmailSummaryTool`, `GetUnsubscribeInfoTool`, `UnsubscribeFromEmailTool`, `CreateContactTool`, `UpdateContactTool`, `DeleteContactTool`, `GetGuideTool`, `DeleteEventTool`. Keep the 15 in spec §5 keep-list (+ `delete_event`/`delete_contact` stay removed at server level; Aura's DenyRisk is defense-in-depth on top).

- [ ] **Step 6: Drop the unused Router/LLM smart-routing**

Remove the Router configuration + the LLM backend services from `AddCalendarMcpCore` registration path if cleanly separable (else leave dormant — it only activates when `Router` config is present, which Aura won't set). Document the decision in the fork README.

- [ ] **Step 7: Build + verify the trimmed surface live**

```bash
wsl -e bash -lc 'export PATH="$HOME/.dotnet:$PATH"; export DOTNET_ROOT=$HOME/.dotnet; cd ~/aura-pim-mcp && dotnet publish src/CalendarMcp.HttpServer -c Release -o ~/aura-pim-publish'
```
Then run it and `tools/list` (reuse `D:\tmp\calendar-mcp-win\mcp_harness.py` adapted for HTTP, or curl the MCP endpoint). Expected: exactly the 15 kept tools, no Blazor routes, `/health` + `/admin` still respond.

- [ ] **Step 8: Commit the fork branch**

```bash
wsl -e bash -lc 'cd ~/aura-pim-mcp && git add -A && git commit -m "aura: thin fork — strip Blazor UI, trim tools, patch Kiota CVE" && git push -u origin aura/pim-sidecar'
```
(Push only when the user explicitly authorizes — per Aura git-push discipline.)

---

## Phases 2–5 — Outline (detail after Phase 0 verdict)

> Expanded into their own plan once Phase 0 decides HTTP vs stdio for the agent mount and the cockpit's connect surface is ready. Listed here for coverage tracking against the spec.

### Phase 2 — Sidecar image + compose
- Build a runtime Docker image from the fork's `Dockerfile` (already targets `aspnet:10.0`; adjust `CALENDAR_MCP_CONFIG` data dir + non-root). Pin the image.
- Add `aura-pim-mcp` service to `compose.yaml`: `127.0.0.1:8093`, named data volume (tokens + appsettings persist across rebuilds), `CALENDAR_MCP_ADMIN_TOKEN` from Aura secrets, healthcheck on `/health`. (Covers spec §6 compose.)

### Phase 3 — Aura Go integration ✅ (2026-06-16, admin-proxy deferred to Phase 4)
- [x] Repointed `recipe:calendar` in `internal/mcp/manager/catalog.go` from the `aura-calendar-mcp-fixture` stdio placeholder → the streamable-HTTP `aura-pim-mcp` sidecar via `calendarRecipeURL()` (loopback 8093 / compose-DNS `aura-pim-mcp:8080` under `AURA_IN_CONTAINER`, WR-01 port-validation). Install-on-demand, NOT default-on. (Spec §6 catalog.)
- [x] Agent mount is automatic through the existing `MountManagedServer` path: tools mount `Deferred` (all non-`memory` namespaces) + `calendar__*`-namespaced; write tools carry `Mutating:true` (from the server's `ReadOnlyHint`) into Aura's execution-time permission layer. **Correction:** there is no `DenyRisk` mount-time filter in the codebase (the memory tier proves "NO DenyRisk filter") — the write-deny posture is the fork's server-side tool trim (no destructive tools) + the `Mutating` flag, not a per-recipe mount filter.
- [x] Rewrote `internal/mcp/calendar_integration_test.go` to drive the live sidecar over streamable-HTTP (`OpenServer` + `ManagedServer`) with the `AURA_PIM_MCP_URL`/`_PORT` knob + no-skip-as-green `$CI` gate; asserts the trimmed 14-tool surface (dropped tools absent) + clean `list_accounts`. Added a `calendar-integration-test` CI job that boots the published GHCR sidecar (zero accounts) and runs the tier (`.github/workflows/ci.yml`).
- [ ] Aura Go backend admin-proxy endpoints (`/admin/*` forward + token) — **moved to Phase 4** (same surface as the cockpit connect UI). (Spec §4, §6.)
- [x] **Mail cutover (Phase 5 catalog/doctor/image bits, folded in per user request):** removed the `recipe:mail` catalog entry + the `recipe:mail`/`writeMailChecks` doctor branch (`cmd/aura/mcp_status.go`); dropped the vendored `mail-mcp` from the image (`docker/aura/Dockerfile`, `.goreleaser.yaml`, `docker/aura/PROVENANCE.md`, `mail-mcp-src.tar.gz` git-rm). Node-24 stays (JS skill snippets + `npx skills`). The cron email self-send route survives via `calendar__send_email` (the resolver matches the bare suffix).

### Phase 4 — Cockpit connect (admin-proxy SHIPPED 2026-06-16; UI deferred to the v1.0.0 frontend roadmap)
- [x] **Backend admin-proxy SHIPPED** (commit 64e8333f): `cmd/aura/integrations_proxy.go` — a registry-driven reverse proxy at `/api/integrations/<name>/<sub>` mounted on the loopback, auth-deferred daemon mux (ahead of the `/` embed). The `calendar` leg forwards `/api/integrations/calendar/*` → PIM sidecar `/admin/*` with `X-Admin-Token` injected server-side (the cockpit never holds the token). `mcpmanager.PIMSidecarBaseURL` extracted + shared with the MCP recipe URL. httptest coverage: rewrite + token, method/body/query + default-token, unknown→404, down→502. (Spec §4, §6.)
- [ ] **Cockpit "Integrations" UI deferred** to the v1.0.0 frontend roadmap (Phase 28/29): the cockpit is foundation-only today (`web/src/` has no router/API client) and serve is auth-deferred (amendment #35). The UI (per-provider OAuth config form, Connect Google / Connect Microsoft device-code card, account list/status/remove) wires onto the shipped proxy when the cockpit reaches its Integrations surface.
- [ ] **WhatsApp leg blocked on a bridge prerequisite:** the whatsmeow bridge (`docker/whatsapp`) pairs via terminal QR (qrterminal) and exposes NO REST QR/status/logout endpoint to proxy. "Same stuff for whatsapp" needs a bridge-side management REST endpoint added first (a Go fork patch to chetto1983/whatsapp-mcp), then it's a one-entry add to `builtinIntegrations()`.
- [ ] **Google OAuth redirect leg deferred:** Phase 1 removed the Blazor UI the fork's `/admin/auth/{id}/google/start` + callback depended on (cookie auth → `/admin/ui` redirects). Google connect needs headless rework in the fork (token-gated start + callback → a configurable cockpit URL via `ExternalBaseUrl`). Device-code (Outlook) + account-CRUD JSON paths proxy cleanly today.

### Phase 5 — mail-mcp cutover
- Validate calendar-mcp's email path per connected account through Aura's bridge (send→search self).
- Retire `mail` from `~/.aura/mcp/servers.json` + the catalog; update docs. (Spec §6; MANIFEST Session-16.)

---

## Self-review

- **Spec coverage:** §3 architecture → Phase 0 + Phase 3; §4 thin-fork changes → Phase 1; §5 tool trim → Phase 1 Step 5; §6 integration → Phases 2–4; §7 security (DenyRisk/token) → Phase 0 Step 4 + Phase 3; §8 validation → Phase 0 + Phase 3 test; §9 risk #1 → Phase 0 gate. mail cutover (§6) → Phase 5. All covered.
- **Placeholders:** Phase 0–1 are concrete (commands + code). Phases 2–5 are intentionally an outline pending the gate verdict (stated in the gating note), not hidden TODOs.
- **Type consistency:** `mcp.ManagedServer{Type,URL,Trust}`, `mcp.OpenServer`, `mcptools.MountManagedServer(ctx,reg,name,server)`, `reg.Get(name).Spec().Deferred` — all match the spike-032 reference; Phase 0 Step 4 verifies the exact signatures before relying on them.
