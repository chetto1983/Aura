# Aura MCP Manager Design

Date: 2026-06-04
Status: Phase 16 design contract

## Goal

Phase 16 turns Aura's existing `aura mcp` commands and managed config into a small MCP manager. The manager owns configuration, profiles, recipe/catalog metadata, trust approvals, runtime policy, status, doctor output, logs, Streamable HTTP transport, and tool risk enforcement. The data plane remains the existing MCP client plus `internal/agent/mcptools` bridge.

OpenClaw plugin hosting is explicitly out-of-scope. OpenClaw plugins are arbitrary module code loaded by a future plugin host; MCP servers here are stdio processes or Streamable HTTP endpoints governed by Aura config.

## Architecture

The manager is a control-plane layer under `internal/mcp/manager` and the CLI surface in `cmd/aura/mcp.go`.

- Config and profile commands read and write the durable managed config.
- Recipe/catalog commands expose built-in trusted recipes and optional local catalog entries.
- Trust/runtime commands decide whether a server may run locally, in Docker, through Docker MCP Gateway, or over HTTP.
- Status and doctor commands create on-demand snapshots. They do not imply a restart supervisor.
- The chat boot path consumes only enabled, profile-selected, policy-approved servers.

The MCP data plane remains narrowly scoped:

- stdio client for subprocess servers
- Streamable HTTP client for remote or local HTTP MCP servers
- bridge/mount package for tool definition adaptation and calls
- fail-soft boot for broken or blocked servers

## Config Model

Managed config v2 stays backwards-compatible with current `mcpServers` entries. Legacy entries with only `command`, `args`, `env`, `enabled`, and `source` still load.

New metadata includes:

- `version`
- `profiles`
- server type: `stdio` or `streamable_http`
- trust class
- runtime metadata
- source/catalog metadata
- tool policy
- risk metadata
- last doctor snapshot

Profile export must redact credentials and env values. Import should preserve server identity and policy without silently approving secrets or trust.

## Trust classes

Trust classes are canonical:

- `trusted_recipe`: Aura-curated recipe metadata such as mail, WhatsApp, and Calendar fixture mode.
- `trusted_local`: operator-approved host command. This is high trust and must be explicit.
- `sandboxed_local`: third-party local server launched through a Docker-style runtime with no host mounts by default.
- `remote_http`: Streamable HTTP endpoint with explicit URL/auth configuration.
- `blocked`: configured but not runnable.

New arbitrary local commands default to `blocked`. Chat boot filters blocked and untrusted servers before launch.

## Profiles

Profiles group servers and policy overrides. `default` preserves current behavior. A user can install a recipe into a profile, add or remove a server from a profile, and inspect which servers/tools a profile would mount.

Catalog entries include display name, source, transport, suggested runtime, env placeholders, risk metadata, setup notes, and default tool policy. Built-in catalog entries are trusted recipes. Local catalog files can define custom entries, but they do not auto-approve trust.

Calendar ships first as a deterministic fixture recipe so CI can validate install, status, doctor, and tool census without a live Google or Microsoft account.

## Docker runtime isolation

Direct host commands require `trusted_local`. Third-party local stdio servers should prefer `sandboxed_local`, generated as a Docker command with:

- `--rm -i`
- no host mounts by default
- explicit mount and network metadata if the user approves them
- resource limits where portable
- no printed secret values

Docker MCP Gateway is optional interoperability, not a hard dependency. Aura may generate a gateway-style command or connect to an existing Docker profile, but Aura must still show the server trust and policy posture.

## Streamable HTTP

Streamable HTTP support adds a client that can:

- send the MCP protocol-version header
- initialize and store session id where the server returns one
- list tools
- call tools
- surface HTTP, auth, timeout, and protocol errors clearly

HTTP authorization support in Phase 16 is pragmatic: bearer/header/env credentials and auth status reporting. Full dynamic OAuth client registration is deferred unless a fixture requires it.

## Status, Doctor, And Logs

Status output should work in text and JSON. It reports:

- server name and profile membership
- startup state
- trust class
- runtime
- auth status
- server info
- tools/resources where available
- blocked or failed reason

Doctor is layered:

1. config validation
2. runtime prerequisite checks
3. transport initialize/tools-list
4. recipe checks for mail, WhatsApp, and Calendar fixture/auth
5. policy checks

Logs are stderr tails or configured log paths. Output redacts env-looking secrets and tokens.

## Risk labels

Risk labels describe tool capability:

- `read`
- `write`
- `network`
- `filesystem`
- `destructive`
- `private_data`
- `external_send`
- `unknown`

Unknown risk is conservative. Recipe metadata may provide known labels; otherwise heuristics over tool names/descriptions can add labels. Policy enforcement happens before registration into Aura's runtime tool registry, so denied tools are absent from model reach.

## Command Surface

Phase 16 extends `aura mcp` with manager commands such as:

- `recipes` and richer `install`
- `profiles list|create|use|add|remove`
- `trust approve|block|show`
- `status [--json]`
- `doctor --all`
- `logs <server>`
- `export --profile <name>`

Existing commands remain backwards-compatible.

## Validation

Automated validation is mock-first:

- legacy config migration tests
- profile export redaction tests
- catalog and Calendar fixture recipe tests
- fake stdio MCP server tests
- `httptest.Server` Streamable HTTP tests
- fake Docker command-generation tests
- doctor/status/log redaction tests
- policy tests proving blocked/destructive/unknown-risk tools do not mount

Live operator checks are separate: WhatsApp bridge/session health, mail auth, Calendar live account, and optional Docker MCP Gateway smoke.
