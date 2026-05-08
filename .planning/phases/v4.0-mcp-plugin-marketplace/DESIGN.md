# v4.0a Mail-First MCP Provider Design

Date: 2026-05-08

## Summary

Aura v4.0 starts with mail because it gives immediate personal and professional value: inbox search, message reading, Italian drafting, task extraction, meeting prep, follow-up, and durable wiki/graph memory updates.

The design is provider-agnostic. Aura exposes a small canonical mail contract to the agent and maps that contract to approved MCP providers underneath. The model should not see a pile of raw Gmail, Outlook, IMAP, SMTP, EWS, Graph, and database tools in the default loop.

The enterprise database MCP path is included in the same marketplace slice because it serves business users, but it starts as a separate high-risk read-only profile.

## Goals

- Add a mail-first marketplace slice without widening Aura's default hot path.
- Keep Aura provider-agnostic across Gmail, Microsoft 365, generic IMAP/SMTP, and future providers.
- Prefer tested MCP servers, but expose a stable Aura contract instead of raw provider-specific tool chaos.
- Support Italian personal/professional workflows as skills layered on top of mail retrieval and drafting.
- Add an enterprise database profile with read-only defaults.
- Require review, allowlists, smoke checks, and audit logs before any provider tool becomes available to the agent.

## Non-Goals

- Do not expose raw MCP mail/database tools in the default toolset.
- Do not implement silent sending in the first slice.
- Do not make WhatsApp part of this slice.
- Do not make Aura depend on one SaaS broker for mail.
- Do not rewrite the existing MCP client before the provider probe proves the integration shape.

## Candidate MCP Servers

### Primary Mail Candidate

[tecnologicachile/mail-mcp](https://github.com/tecnologicachile/mail-mcp)

- Rust server for IMAP, SMTP, EWS, Microsoft Graph, OAuth2, multi-account, Microsoft 365, Hotmail, Gmail, Zoho, Fastmail, and generic IMAP/SMTP.
- Good provider-agnostic fit because one server can cover multiple mail backends.
- Tool surface is broad, so Aura must map only approved tools into the canonical contract.
- Initial allowlist:
  - `list_all_accounts`
  - `imap_verify_account`
  - `imap_list_mailboxes`
  - `imap_search_messages`
  - `imap_get_message`
  - `ews_search_messages`
  - `ews_get_message`
- Block by default:
  - all send tools
  - all delete tools
  - all move/bulk mutation tools
  - raw message append/update tools

### Google Workspace Candidate

[aaronsb/google-workspace-mcp](https://github.com/aaronsb/google-workspace-mcp)

- Gmail, Calendar, Drive, account management, OAuth, multi-account, and MCPB/npm packaging.
- Useful when users want a Workspace bundle instead of only mail.
- Exposes composite tools such as `manage_email`, `manage_calendar`, and `manage_drive`; Aura must constrain operations inside those tools.
- Initial use should focus on `manage_email` read/search and draft-like flows if available; calendar can be a later profile capability.

### Gmail Fallback Candidate

[navbuildz/gmail-mcp-server](https://github.com/navbuildz/gmail-mcp-server)

- Gmail-specific server with multi-account support, Gmail query syntax, OAuth2, encrypted token storage, Docker/Railway deployment, archive/label/unsubscribe operations.
- Good fallback if `mail-mcp` is too broad or if Gmail-specific behavior is better.
- Initial Aura mapping should use list/read/batch triage before any archive/label/unsubscribe operation.

### Microsoft Fallback Candidate

[littlebearapps/outlook-assistant](https://github.com/littlebearapps/outlook-assistant)

- Outlook email, calendar, contacts, Microsoft Graph, OAuth, token refresh, local token storage.
- Good fallback if Microsoft 365 through `mail-mcp` is unreliable for a tenant.
- Initial Aura mapping should use mail search/read and avoid contacts/calendar writes.

### Enterprise Database Candidate

[executeautomation/mcp-database-server](https://github.com/executeautomation/mcp-database-server)

- Supports SQLite, SQL Server, PostgreSQL, and MySQL.
- Useful for business users who want Aura to inspect operational data.
- High-risk because it exposes `write_query`, `create_table`, `alter_table`, and `drop_table`.
- Initial Aura allowlist:
  - `list_tables`
  - `describe_table`
  - `read_query`
  - `export_query`
- Block by default:
  - `write_query`
  - `create_table`
  - `alter_table`
  - `drop_table`
  - any tool with schema/table mutation semantics

### Hosted Broker Candidates

[Composio Connect](https://docs.composio.dev/docs/composio-connect) is useful for quick experiments because it exposes many apps through one hosted MCP connection and handles OAuth. Aura should treat it as optional SaaS, not as the default architecture, because core mail should stay portable and auditable.

## Canonical Aura Mail Contract

Aura exposes stable internal capabilities. Provider plugins map into these capabilities.

- `mail.accounts`: list configured accounts and provider health.
- `mail.search`: search messages with query, mailbox, account, date range, and limit.
- `mail.read`: read one message or thread with attachments metadata.
- `mail.thread`: fetch neighboring thread context.
- `mail.draft_reply`: produce a draft response and store it for human review.
- `mail.extract_tasks`: extract tasks, deadlines, people, and follow-up facts.
- `mail.label`: optional reviewed label operation.
- `mail.archive`: optional reviewed archive operation.

First slice exposes only search/read/task extraction/draft creation. Send, delete, bulk move, unsubscribe, and destructive database actions remain outside the first agent-visible contract.

## Agent Tool Surface

Default Aura remains tiny: `search_memory` and `schedule_task`.

Mail tools are available only through an explicit `mail` toolset or a dashboard-approved MCP provider profile. The model sees the Aura contract, not the raw provider tool list.

Example agent-facing descriptions must include concrete tool-call examples so the model knows the exact shape, following the existing Hermes-style tool definition work.

## Provider Adapter

Add a provider adapter layer between Aura tool definitions and MCP tool calls.

Responsibilities:

- Normalize provider account identities.
- Translate Aura contract arguments to provider tool arguments.
- Normalize provider results to compact message/thread records.
- Redact secrets and raw tokens from logs/results.
- Enforce allowlists before every MCP call.
- Mark write/send/mutate operations as review-required.
- Emit audit events with provider, account alias, capability, provider tool, status, and latency.

Provider adapters should be data-driven where practical, but not regex-driven. The first version can ship with explicit provider manifests for the shortlisted MCP servers.

## Dashboard Flow

Add a Mail Connectors view inside the MCP marketplace milestone:

- Provider cards: `mail-mcp`, Google Workspace, Gmail fallback, Outlook fallback.
- Risk badges: reads mail, drafts mail, sends mail, mutates mailbox, external SaaS, database read, database write.
- Setup hints: required secrets, OAuth/device-code/app-password notes, Docker/stdin/runtime type.
- Probe button: start provider smoke check without enabling agent tools.
- Enable button: expose only approved Aura capabilities.
- Audit/log view: show setup/probe/tool failures with secrets redacted.

The database provider lives under an Enterprise profile, not inside personal mail.

### Frontend Configuration Shape

The current dashboard route `/mcp` is implemented by `web/src/components/MCPPanel.tsx`. Today it lists MCP servers connected at boot and lets the operator manually invoke every advertised tool by editing a JSON textarea. That is useful as a diagnostic surface, but it is the wrong primary UX for mail and database setup.

v4.0a should split MCP UI into two concepts:

- **Connectors**: guided configuration for approved provider profiles.
- **Raw MCP**: diagnostic view for already-connected MCP servers and manual tool invocation.

The first implementation can keep the same `/mcp` route and render tabs inside the existing lazy-loaded `MCPPanel` instead of adding a new top-level sidebar item. This avoids bloating navigation and preserves existing dashboard muscle memory.

Recommended tabs:

- `Connectors`: provider cards, setup status, risk badges, required secrets, probe/enable controls.
- `Installed`: enabled/disabled managed providers and exposed Aura capabilities.
- `Health`: probe status, last error, last successful tool call, latency.
- `Raw MCP`: current server/tool/schema/manual invoke view, renamed from the existing panel body.

Provider cards should be compact operational cards, not marketing cards. Each card shows:

- provider name and runtime type;
- status: not configured, configured, probe failed, ready, enabled;
- capabilities: mail read, mail draft, calendar, database read, database write blocked;
- risk badges;
- setup fields or setup instructions;
- probe button;
- enable button only after probe success;
- logs/audit link.

Mail/database secrets should not be added to the generic `SettingsPanel` as dozens of global keys. The existing settings page is good for runtime-wide values such as `MCP_SERVERS_PATH`, `LLM_BASE_URL`, Qdrant, Garage, and OCR. Provider credentials are account-scoped and should be managed through connector-specific API endpoints with secret references and redaction.

The frontend type boundary should add explicit DTOs in `web/src/types/api.ts` rather than reusing raw `MCPServerSummary`:

- `ConnectorProviderSummary`
- `ConnectorCapability`
- `ConnectorRiskBadge`
- `ConnectorProbeResponse`
- `ConnectorAuditEvent`

The API client in `web/src/api.ts` should add connector methods under `/mcp/providers` or `/connectors`, while the existing `mcpServers()` and `invokeMCPTool()` stay for Raw MCP diagnostics.

The UI must support Italian and English strings in `web/src/i18n/locales/it.json` and `web/src/i18n/locales/en.json`. Labels should avoid tool jargon where possible:

- "Connettori" instead of only "MCP".
- "Test connessione" for probe.
- "Abilita per Aura" for reviewed enablement.
- "Scrittura bloccata" for blocked write/send/delete capabilities.

No first slice should render every provider tool in the Connectors tab. The operator configures provider capability profiles; raw tool lists stay in Raw MCP.

## Italian Workflows

Mail value comes from workflows, not from provider plumbing. Add skills or prompt procedures that consume the canonical mail contract:

- `email-triage-it`: summarize important unread mail and group by action.
- `professional-reply-it`: draft concise Italian professional replies.
- `client-followup-it`: detect stalled client/vendor threads and draft follow-ups.
- `meeting-brief-it`: collect recent mail context before a meeting.
- `admin-deadlines-it`: extract invoices, payment deadlines, quotes, order numbers, PEC-like messages, and tasks into Aura memory.

These workflows write durable facts through existing memory/wiki paths after review.

## Security Model

Mail and database MCP servers receive sensitive access, so Aura must assume provider code can be risky.

Controls:

- Pin provider package/image/version where possible.
- Prefer container runtime or reviewed local binary paths over `npx -y latest` in production.
- Store secrets as secret references, not raw API response fields.
- Keep all write/send/delete/database mutation tools disabled by default.
- Require dashboard review for enablement.
- Audit provider tool metadata at probe time.
- Detect dangerous metadata changes between probes.
- Run smoke tests before enabling any provider.
- Keep raw provider tools in admin/marketplace only.

Rationale: public MCP incidents have already shown email exfiltration through malicious or squatted packages, including hidden BCC behavior in email tooling. Aura should make this class visible through pinning, allowlists, and audit rather than trusting package names.

## First Implementation Slice

Slice v4.0a delivers provider discovery and read-only value before full marketplace install automation.

1. Add mail provider manifests for the shortlisted servers.
2. Add canonical mail capability definitions and allowlists.
3. Add provider probe code using existing MCP clients where possible.
4. Add read-only `mail` toolset backed by provider adapters.
5. Add dashboard cards for provider status and risk.
6. Add enterprise database provider manifest in read-only mode.
7. Add smoke tests with fake MCP servers for mail search/read and database list/read.

## Acceptance Criteria

- Aura can list shortlisted mail/database provider manifests without live network calls.
- Aura can probe a fake MCP mail provider and map `mail.search` and `mail.read`.
- Aura can probe a fake database MCP provider and expose only read-only enterprise capabilities.
- Raw provider tools are not exposed in the default toolset.
- Write/send/delete/database mutation tools stay blocked unless a later reviewed slice enables them.
- Dashboard/API responses redact secrets.
- Audit logs show provider probe, enable, blocked call, and successful read call.
- Docker smoke confirms Aura starts with no configured providers and no startup penalty.

## Sources Checked

- MCP Registry live search API, checked 2026-05-08.
- `tecnologicachile/mail-mcp`: <https://github.com/tecnologicachile/mail-mcp>
- `aaronsb/google-workspace-mcp`: <https://github.com/aaronsb/google-workspace-mcp>
- `navbuildz/gmail-mcp-server`: <https://github.com/navbuildz/gmail-mcp-server>
- `littlebearapps/outlook-assistant`: <https://github.com/littlebearapps/outlook-assistant>
- `executeautomation/mcp-database-server`: <https://github.com/executeautomation/mcp-database-server>
- Composio Connect: <https://docs.composio.dev/docs/composio-connect>
- MCP email supply-chain risk reference: <https://semgrep.dev/blog/2025/so-the-first-malicious-mcp-server-has-been-found-on-npm-what-does-this-mean-for-mcp-security/>
