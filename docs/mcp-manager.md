# Aura MCP Manager

Aura's MCP manager keeps third-party tools useful without making every local command
available to the model by accident. It stores MCP servers in a managed config, groups
them into profiles, records trust, runs status/doctor checks, supports Streamable
HTTP, and blocks risky tools before they enter the runtime registry.

## Config Location

By default Aura reads and writes:

```bash
~/.aura/mcp/servers.json
```

For tests or isolated runs, set:

```bash
AURA_MCP_CONFIG=/path/to/servers.json
```

The file uses the familiar `mcpServers` shape plus Aura metadata: `profiles`,
`trust`, `runtime`, `toolPolicy`, and `riskLabels`. Env values may contain local
placeholders, but exported profiles redact secrets.

## Recipes

List available recipes:

```bash
aura mcp recipes
aura mcp recipes --json
```

Install a recipe:

```bash
aura mcp install calculator
aura mcp install calendar
aura mcp install whatsapp
```

Built-in recipes are marked as `trusted_recipe` and include policy metadata.

| Recipe | Purpose | Notes |
|---|---|---|
| calculator | Local arithmetic MCP over stdio | Good smoke test. |
| Calendar | PIM sidecar (forked calendar-mcp) — mail + calendar + contacts over streamable-HTTP | OAuth accounts connected via the sidecar's token-gated admin API (cockpit-driven); subsumes the retired standalone mail recipe. |
| WhatsApp | WhatsApp bridge | Requires a paired account and bridge process. |

## Profiles

Profiles decide which configured servers are active for a run.

```bash
aura mcp profile list
aura mcp profile create work
aura mcp profile add work calendar
aura mcp profile remove work calendar
aura mcp profile use work
```

If a profile exists with no servers, it mounts none. If a named profile is missing,
Aura falls back to enabled managed servers.

## Trust

Manual local commands are blocked by default.

```bash
aura mcp add local-demo -- node server.js
aura mcp status
aura mcp doctor local-demo
```

To approve a local command after reviewing its source, command, runtime, and profile:

```bash
aura mcp trust local-demo
```

Trust classes:

| Trust | Meaning |
|---|---|
| `trusted_recipe` | Built-in Aura recipe. |
| `trusted_local` | User-approved local command. |
| `sandboxed_local` | Third-party local server launched through a sandbox/container runtime. |
| `remote_http` | Streamable HTTP server. |
| `blocked` | Visible in status, never launched by chat boot or doctor. |

## Runtime

Local stdio servers can run directly or through Docker metadata.

Docker runtime example:

```json
{
  "runtime": {
    "kind": "docker",
    "image": "example/mcp:1",
    "command": ["server", "--stdio"],
    "mounts": ["type=bind,src=/safe,dst=/data,readonly"],
    "network": ["api.example.com"],
    "cpus": "0.5",
    "memory": "256m"
  },
  "trust": { "class": "sandboxed_local" }
}
```

Aura generates `docker run -i --rm`, adds `--network none` by default, and never
adds host mounts unless they are explicit.

Docker MCP Gateway example:

```json
{
  "runtime": {
    "kind": "docker_gateway",
    "profile": "team"
  },
  "trust": { "class": "trusted_local" }
}
```

This launches:

```bash
docker mcp gateway run --profile team
```

Docker and Docker MCP Gateway live checks are operator-only because they depend on
the local Docker installation and MCP Toolkit version.

## Status, Doctor, Logs

Inspect configured servers:

```bash
aura mcp status
aura mcp status --json
```

Run non-secret checks for every server:

```bash
aura mcp doctor --all
```

Run a single-server startup and tool-list check:

```bash
aura mcp doctor calculator
```

Blocked servers report trust-needed without launching the command.

```bash
aura mcp logs calculator
```

`logs` currently exposes the CLI surface and points operators at doctor output; Aura
does not write MCP log tails to git.

## Tool Risk Policy

`aura mcp tools <name>` lists live advertised tools with risk labels and whether each
tool is mounted or blocked:

```bash
aura mcp tools calendar
```

Risk labels include:

- `read`
- `write`
- `network`
- `filesystem`
- `destructive`
- `private_data`
- `external_send`
- `unknown`

Managed policy supports:

```json
{
  "toolPolicy": {
    "allow": ["send_email", "fetch_emails", "search_emails", "get_thread"],
    "deny": ["delete_mailbox"],
    "denyRisk": ["destructive", "unknown"]
  },
  "riskLabels": ["private_data", "external_send"]
}
```

Aura denies `destructive` and `unknown` risk by default in managed policy decisions.
Blocked tools never enter the agent registry.

## Connecting calendar/email accounts (OAuth)

The `calendar` recipe is the PIM sidecar (forked calendar-mcp). It manages OAuth accounts
through its OAuth-protected `/admin` REST API, which Aura's cockpit drives via the backend
routes at `/api/connect/pim/*`. The backend obtains the same identity-scoped grant used by
the MCP transport and forwards its access token as `Authorization: Bearer`; the sidecar
validates the standard token and uses its `sub` as the sole tenant selector. The browser
holds no token and cannot select a different subject. The retired `/api/integrations/*`
proxy and `aura mcp console` do not exist: account management and agent tool calls share
the same remote-MCP identity model.

**Microsoft / Outlook (device code)** — no redirect, works everywhere:

- `POST /admin/auth/{accountId}/start` returns a user code + the `microsoft.com/devicelogin` URL.
- The operator enters the code there; the cockpit polls `/admin/auth/{accountId}/status`.

**Google (web redirect)** — needs a deterministic, registered redirect URI:

1. Set `AURA_PIM_EXTERNAL_BASE_URL` to the Caddy-fronted host the operator's browser uses
   (e.g. `https://aura.local`). The sidecar then builds the redirect URI
   `<base>/admin/auth/google/callback` deterministically (regardless of the
   cockpit→proxy→sidecar path). Same-host operators may instead use
   `http://localhost:8093` (Google allows `localhost` over http).
2. In the Google Cloud Console OAuth client (**Web application** type), add that exact URI as
   an **Authorized redirect URI**: `https://<host>/admin/auth/google/callback`.
3. Caddy already routes `/admin/auth/google/callback` to the sidecar **token-exempt** (Google's
   redirect carries `?code&state`, not an Aura token).
4. Connect: `GET /admin/auth/{accountId}/google/start` returns `{authUrl, redirectUri}`; the
   cockpit opens `authUrl`; after consent Google redirects to the callback, the sidecar
   exchanges the code and renders a result page; the cockpit polls account status.

## Live Checks

Automated tests use fake stdio servers and `httptest`; they do not run Docker, npx,
uv, WSL, or public network services.

Operator-only live checks:

| Check | Command | Expected |
|---|---|---|
| WhatsApp bridge | `aura mcp doctor whatsapp` | REST bridge reachable; connected-state reported when endpoint exists. |
| Calendar PIM sidecar | `aura mcp doctor calendar` | `http endpoint configured` + `pim sidecar: accounts managed via admin API at <url>`. |
| Docker runtime | `aura mcp status` plus a local Docker smoke | Docker metadata visible; actual launch depends on local daemon. |

Do not commit credentials, phone numbers, access tokens, or live doctor output that
contains private account identifiers.

## Troubleshooting

| Symptom | Likely Cause | Fix |
|---|---|---|
| `startup=blocked` | Manual command has no trust approval | Review command/source, then run `aura mcp trust <name>` if appropriate. |
| `doctor <name>` says trust approval required | Server is blocked | Trust it or keep it blocked; Aura did not launch it. |
| Tool missing from registry | Not in allowlist or blocked by risk | Run `aura mcp tools <name>` and inspect the block reason. |
| Mail/WhatsApp send tool unavailable | Recipe policy or bridge/auth issue | Check `aura mcp tools`, then `aura mcp doctor --all`. |
| Docker server has no mounts | Default least-privilege runtime | Add explicit read-only `runtime.mounts` entries. |
| Streamable HTTP auth fails | Missing bearer/header env | Configure `MCP_BEARER_TOKEN` or `MCP_HEADER_*` env entries for that server. |
