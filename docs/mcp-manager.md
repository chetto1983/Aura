# Aura MCP Manager

Aura's MCP manager keeps third-party tools useful without making every local command
available to the model by accident. It keeps MCP servers in one registry, records trust,
runs status/doctor checks, supports Streamable HTTP, and classifies every tool so a
destructive call waits for the operator's approval.

## Where servers are stored

Every MCP server lives in one Postgres table, `aura.mcp_server` (migration 0101), next to
the MCP audit trail and the per-identity OAuth grants. Every `aura mcp` verb, the daemon's
mount and the cockpit read and write that table, and each change is audited. It replaced a
JSON file that was read in two places and written in one (`cmd/aura/mcp_registry.go`).

## Recipes

List available recipes:

```bash
aura mcp recipes
aura mcp recipes --json
```

Install a recipe:

```bash
aura mcp install calendar
aura mcp install whatsapp
aura mcp install browser
```

Built-in recipes are marked as `trusted_recipe`; their tools are classified from the
recipe's own table (see Tool risk).

| Recipe | Purpose | Notes |
|---|---|---|
| memory | Aura's ArcadeDB memory (`cmd/arcadedb-mcp`) over streamable-HTTP | On by default everywhere. |
| browser | agent-browser's MCP server, run in each identity's sandbox box | On by default in the appliance. Logins stay in that identity's box; see Runtime. |
| Calendar | PIM sidecar (forked calendar-mcp) — mail + calendar + contacts over streamable-HTTP | On by default in the appliance. OAuth accounts connected via the sidecar's token-gated admin API (cockpit-driven); subsumes the retired standalone mail recipe. |
| WhatsApp | WhatsApp bridge | On by default in the appliance. Requires a paired account. |

"On by default in the appliance" applies only inside the Aura container; an explicit
`aura mcp disable <name>` still wins (`internal/mcp/manager/runtimeset.go`).

## Profiles

A profile is a group of servers. Membership is stored on each server's row:

```bash
aura mcp profile list
aura mcp profile add work calendar
aura mcp profile remove work calendar
```

Aura mounts the servers of the `default` profile, or every enabled server when no server
belongs to `default`; a server installed from the cockpit joins `default`.

Known defect: `aura mcp profile create` and `aura mcp profile use` answer `ok` but persist
nothing. A profile exists only while a server belongs to it, and the active profile is
always `default`.

## Trust

`aura mcp add` stores a command as `blocked` until it is approved (or added with
`--trust local`), so nothing an operator has not reviewed is ever launched by a mount:

```bash
aura mcp add local-demo -- node server.js
aura mcp status
aura mcp doctor local-demo
aura mcp trust local-demo --reason "reviewed server.js"
```

A streamable-HTTP server is added with `--url` instead of a command, and is stored `blocked`
the same way:

```bash
aura mcp add gh --url https://mcp.example.com/mcp
aura mcp trust gh --class remote_http --reason "reviewed the hosted server"
```

`aura mcp trust <name> --reason <text> [--class <class>]` records the class with who approved
it and why. A server written without any class, such as one in an imported config, gets one
from where it runs: a local command is `trusted_local`, a box-runtime command
`sandboxed_local`, a URL `remote_http`, a catalog recipe `trusted_recipe`.

Trust classes:

| Trust | Meaning |
|---|---|
| `trusted_recipe` | Built-in Aura recipe. |
| `trusted_local` | User-approved local command. |
| `sandboxed_local` | Third-party local server launched through a sandbox/container runtime. |
| `remote_http` | Streamable HTTP server. |
| `blocked` | Visible in status, never launched by chat boot or doctor. |

## Runtime

A stdio server runs in one of two places, named by `runtime.kind`:

| Kind | Where it runs |
|---|---|
| `local` (default) | A child process of Aura. `aura mcp add` first prepares the server's environment, rewrites the launch to absolute paths inside it, and stores the server only after a successful handshake (amendment #211). |
| `box` | Inside the calling identity's sandbox box, one process per identity, reached over an exec's stdin and stdout. |

```json
{
  "command": "agent-browser",
  "args": ["mcp"],
  "runtime": { "kind": "box" }
}
```

Of either kind, a line on the server's stdout that is not JSON-RPC (npm's own install output,
for one) is dropped and logged rather than ending the session, as LibreChat's TypeScript client
does. Closing a `local` server's session ends its whole process group, so a child it forked does
not outlive it.

A box server installs itself, the way LibreChat's stdio servers do: declare it as a command
that fetches its own package, pinned, and its first start in an identity's box fetches it into
that identity's npm or uv cache. There is no install step and nothing is shared between
identities. From the CLI, `--box` declares it and the add completes the handshake in the
operator's box; in the cockpit, a custom stdio install set to run in each identity's box does
the same in the installing operator's box:

```bash
aura mcp add fetch --box --init-timeout 60 -- uvx mcp-server-fetch==2026.8.18
aura mcp add files --box -- npx -y @modelcontextprotocol/server-filesystem@2026.8.31 /workspace
aura mcp trust fetch --class sandboxed_local --reason "reviewed mcp-server-fetch"
```

Measured in a box with empty caches (prd.md §12): 3.5-5.4 s for a light server's first start,
10.95 s for a numpy/scipy/sympy one, 0.6-1.8 s once cached; the caches survive a box recreate.
A first start gets `runtime.initTimeoutSec` (default 30, at most 600; `--init-timeout` from the
CLI), however short the mount or first-call budget around it: set it higher for a server that
fetches more. An install checks the declaration before its handshake, so a box server declared
with a secret is refused before any box sees it.

A box server:

- is started in an identity's box the first time that identity calls one of its tools, and a
  call from any other identity is refused. The tool list is read once, at mount, in the
  operator's box;
- takes the box as its environment: the command comes with the image or fetches itself into
  the identity's own caches, one copy per identity (no package state is shared between
  identities; see prd.md on cache poisoning), and nothing is prepared on the Aura host. Adding
  one requires a handshake, run in the installing identity's (or, from the CLI, the
  operator's) box;
- starts cold in the operator's box when it is first mounted, since that is where its tools
  are read, and `aura serve` waits for that start;
- takes no secrets: everything in a box is readable by the agent's own shell, so a
  secret-shaped `env` entry is refused at write time;
- writes its stderr to `/tmp/aura-mcp-<name>.log` in the box;
- is never started on the Aura host. `aura mcp doctor <name>` has no box to run it in and
  fails with "this server runs in the sandbox box, and this caller has no sandbox";
  `aura mcp status` lists it as not probed.

When the box is suspended for idleness the session ends and the next call starts the server
again. The `docker` and `docker_gateway` kinds were retired by amendment #209.

## Status, Doctor, Logs

Inspect configured servers:

```bash
aura mcp status
aura mcp status --json
```

Run non-secret checks for every server. Each prints
`<name>: <startup> trust=<trust> runtime=<runtime>`, then a runtime check and, for the
calendar and WhatsApp recipes, a sidecar or bridge line:

```bash
aura mcp doctor --all
```

Run a single-server startup and tool-list check, which prints `ok: <name> started; N tools`:

```bash
aura mcp doctor calendar
```

Blocked servers report trust-needed without launching the command.

```bash
aura mcp logs calendar
```

`logs` currently exposes the CLI surface and points operators at doctor output; Aura
does not write MCP log tails to git.

## Tool risk

`aura mcp tools <name>` lists the tools a server advertises, one per line with its
description (`--json` prints the full tool objects):

```bash
aura mcp tools calendar
```

There is no per-tool allow or deny list. Instead every tool is classified when it is
bridged (`internal/agent/mcptools/bridge_risk.go`):

- a built-in recipe's tools come from the recipe's own table: read, mutate or destructive;
- any other tool from its MCP annotations: `readOnlyHint` makes it a read, and
  `destructiveHint` decides whether a write is destructive;
- a tool with no annotations, or a write without `destructiveHint`, is treated as
  destructive.

The approval gate grades each call from that classification (`internal/gateway/classify.go`):
a destructive call stops the turn until the operator approves it.

## Connecting calendar/email accounts (OAuth)

The `calendar` recipe is the PIM sidecar (forked calendar-mcp). It manages OAuth accounts
through its OAuth-protected `/admin` REST API, which Aura's cockpit drives via the backend
routes at `/api/connect/pim/*`. The backend obtains the same identity-scoped grant used by
the MCP transport and forwards its access token as `Authorization: Bearer`; the sidecar
validates the standard token and uses its `sub` as the sole tenant selector. The browser
holds no token and cannot select a different subject. The retired `/api/integrations/*`
proxy and `aura mcp console` do not exist: account management and agent tool calls share
the same remote-MCP identity model.

**Provider OAuth apps (admin, once per provider).** The Google, Microsoft 365 and Outlook.com
OAuth client is not typed per account. An admin (`identity.create`) sets it once in the
calendar section's **Provider OAuth apps** panel (`PUT /api/connect/pim/providers/{provider}`,
stored sealed in `aura.pim_provider_app`). A member then adds an account with only an account
ID and a display name, and Aura injects the admin-set client when it forwards the create.
Accounts linked before a change keep the client they were linked with.

**Microsoft / Outlook (device code)** — no redirect, works everywhere:

- The Entra app registration needs the delegated Microsoft Graph permissions `Mail.Read`,
  `Mail.ReadWrite`, `Mail.Send`, `Calendars.ReadWrite` and `Contacts.ReadWrite`, and
  **Allow public client flows: Yes**. The admin enters its tenant and client ID; there is no secret.
- `POST /admin/auth/{accountId}/start` returns a user code + the `microsoft.com/devicelogin` URL.
- The member enters the code there; the cockpit polls
  `GET /api/connect/pim/accounts/{id}/auth/status`, which Aura forwards to the sidecar.

**Google (web redirect through a shared relay)** — one redirect URI for every install:

1. In Google Cloud Console create an OAuth client of type **Web application** and add exactly
   this **Authorized redirect URI** (trailing slash included):
   `https://chetto1983.github.io/aura-connect/google/callback/`. It never changes and does not
   depend on the address Aura is reached by, so the same line works for a LAN IP, a tunnel
   or a public hostname. The admin enters that client's ID and secret once in **Provider
   OAuth apps**, which also shows this URI.
2. Connect: a member adds a Google account; the cockpit calls `GET /api/connect/pim/accounts/{id}/google/start`; Aura adds
   `returnBase` = the cockpit origin (`AURA_WEB_PUBLIC_URL` when set, otherwise the origin the
   request arrived on) and forwards it to the sidecar, which answers `{authUrl, redirectUri}`
   with the relay URI as `redirectUri` and `state` = `<nonce>.<base64url(callback)>`.
3. After consent Google sends the browser to the relay page
   ([chetto1983/aura-connect](https://github.com/chetto1983/aura-connect)), which forwards it to
   `<cockpit origin>/admin/auth/google/callback`. Aura serves that path on every origin as a
   public route and forwards it to the sidecar without a token; the sidecar accepts only a
   `state` it issued, then exchanges the code with the client secret and a PKCE verifier.
4. The cockpit polls `GET /api/connect/pim/accounts/{id}/status` and closes the Google panel
   when `linked` turns true.

Why a relay (measured 2026-09-23, see prd.md §13): a **Desktop app** client redirects to the
loopback of the machine running the browser, which a server install cannot receive; a Web
client needs a public, non-IP redirect URI of its own, which a LAN install does not have.
Aura ships no shared Google client: each install brings its own, so a code forwarded by the
public relay is worthless without that install's secret.

## Remote MCP servers with no client registration (ElevenLabs)

A remote MCP server authorizes through the cockpit's **Connect** control. Aura presents the
client in `MCP_OAUTH_CLIENT_ID` when one is configured, else registers itself dynamically
(Linear, Atlassian, Notion). When the authorization server offers neither, Aura signs in
with its own Client ID Metadata Document instead:
`https://chetto1983.github.io/aura-connect/mcp/client-metadata.json`. No configuration is
needed. The consent screen shows "Aura" as a self-declared app, and after consent the browser
returns through the relay `https://chetto1983.github.io/aura-connect/mcp/callback/`, which
forwards it to `<cockpit origin>/api/governance/mcp/authorization/callback`. Like the Google
relay, it forwards only to an `https` origin or to loopback, so the cockpit must be reached
over HTTPS or on `localhost`.

For **ElevenLabs** add a custom HTTP server with the URL
`https://api.us.elevenlabs.io/v1/mcp`, not the `https://api.elevenlabs.io/v1/mcp` in its
documentation. The server declares itself as `api.us.elevenlabs.io`, and the MCP SDK refuses
metadata that names a different resource than the URL it dialled (RFC 9728 §3.3). The hosted
server accepts no API key.

## Live Checks

Unit tests use fake stdio servers and `httptest`. CI also runs live tiers: the
`whatsapp_integration` and `calendar_integration` tiers against the published `:latest`
sidecar images, and the `docker_integration` tier against a built sandbox image for box
servers.

Operator checks:

| Check | Command | Expected |
|---|---|---|
| WhatsApp bridge | `aura mcp doctor whatsapp` | REST bridge reachable; connected-state reported when endpoint exists. |
| Calendar PIM sidecar | `aura mcp doctor --all` | `calendar pim sidecar: accounts managed via admin API at <url>`; `aura mcp doctor calendar` prints `ok: calendar started; 1 tool`. |

Do not commit credentials, phone numbers, access tokens, or live doctor output that
contains private account identifiers.

## Troubleshooting

| Symptom | Likely Cause | Fix |
|---|---|---|
| `blocked` in the `startup` column of `aura mcp status` | Manual command has no trust approval | Review command/source, then run `aura mcp trust <name>` if appropriate. |
| `doctor <name>` says trust approval required | Server is blocked | Trust it or keep it blocked; Aura did not launch it. |
| A server's tools are not in the per-turn manifest | Deferred: only memory's core holds an always-loaded slot | The model reaches them through `tool_search`; nothing to fix. |
| A server's tools are missing entirely | Server disabled, blocked, outside the `default` profile, or its mount failed | Check `aura mcp status`, then `aura mcp doctor <name>`. |
| Mail/WhatsApp send tool unavailable | Bridge or account authorization | Check `aura mcp doctor --all`, then the cockpit's authorization for that server. |
| Streamable HTTP auth fails | Missing bearer/header env | Configure `MCP_BEARER_TOKEN` or `MCP_HEADER_*` env entries for that server. |
