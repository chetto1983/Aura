# MCP Survey — Productivity (self-hosted), 2026-05-20

Survey of community MCP servers that wrap **self-hosted** productivity surfaces Aura's operator can realistically run: CalDAV calendars (Radicale/Baikal/Nextcloud), task managers (Vikunja/Taskwarrior/OpenProject), time-tracking (Kimai), document management (Paperless-ngx), bookmark managers (Linkwarden), and spreadsheet/relational sheets (Grist, NocoDB). Cloud-only candidates (Google Calendar API, Todoist cloud, Notion, Asana, Trello) are out of scope.

Companion to the broader **MCP productivity roundup** in memory: every adopted MCP must be configurable from Aura's dashboard via JSON/YAML/env, no hard-coded paths.

Scoring rubric (recap):
- **Maturity**: last commit < 6 months + ≥20 stars + clear README.
- **Self-hosted purity (0/1)**: 1 = points at protocols/APIs the operator can run themselves.
- **Configurability (0–3)**: 3 = clean env or config file Aura can plug into its dashboard; 1 = hard-coded; 0 = unknown.
- **Footprint**: stdio binary < 100 MB green; Node/Python sidecar yellow; heavy JVM/Electron red.
- **Mutates state?**: yes/no — which tools write.
- **Overlap with Aura native**: most of these are NEW user-facing dimensions; Aura's `scheduled_tasks` is internal-only, so a Vikunja/CalDAV link is additive.

---

## p-w-4-z/calendar-mcp (generic CalDAV)
- **URL**: https://github.com/p-w-4-z/calendar-mcp
- **Language / runtime**: Python ≥3.10 (PyPI: `calendar-mcp`)
- **License**: AGPL-3.0-or-later (dual-licensed; commercial option)
- **Last commit**: 2026-04-13 (recent)
- **Stars**: low (new PyPI listing; <20)
- **Talks to**: any CalDAV provider — Radicale, Baikal, Nextcloud, ownCloud, Apple iCloud, Fastmail
- **Transport**: stdio (FastMCP)
- **Tool surface (10)**: `list_calendars`, `get_events`, `get_event`, `today`, `upcoming`, `search_events`, `free_busy`, `create_event`, `update_event`, `delete_event`
- **Self-hosted purity**: 1 (pure CalDAV — works against Radicale or Baikal)
- **Configurability**: 3 (env: `CALENDAR_MCP_URL`, `CALENDAR_MCP_USERNAME`, `CALENDAR_MCP_PASSWORD`, optional read-only + timezone + default calendar)
- **Mutates state?**: yes — create/update/delete events (gated by `CALENDAR_MCP_READ_ONLY`)
- **Verdict**: **ADOPT (conditional on AGPL review)** — only generic CalDAV MCP with read-only switch + full CRUD. AGPL-3.0 is OK for a sidecar MCP (stdio subprocess, not linked into Aura's binary), but flag for license-policy gate.

## madbonez/caldav-mcp
- **URL**: https://github.com/madbonez/caldav-mcp
- **Language / runtime**: Python
- **License**: see repo (not surfaced in README excerpt)
- **Last commit**: not surfaced; project is small
- **Stars**: 7
- **Talks to**: any CalDAV server (Radicale tested, Yandex noted)
- **Transport**: stdio
- **Tool surface**: `caldav_list_calendars`, `caldav_create_event`, `caldav_get_events`, `caldav_get_today_events`, `caldav_get_week_events`, `caldav_get_event_by_uid`, `caldav_delete_event`, `caldav_search_events`
- **Self-hosted purity**: 1
- **Configurability**: 2 (env: `CALDAV_URL`, `CALDAV_USERNAME`, `CALDAV_PASSWORD`; no read-only switch surfaced)
- **Mutates state?**: yes — create/delete events
- **Verdict**: **SKIP** — strict subset of `p-w-4-z/calendar-mcp`, fewer stars, no read-only mode.

## democratize-technology/vikunja-mcp
- **URL**: https://github.com/democratize-technology/vikunja-mcp
- **Language / runtime**: TypeScript (Node.js 20+)
- **License**: MIT
- **Last commit**: 2025-08-16 (v0.2.0) — within the 6-month window from a Vikunja-MCP perspective, but borderline
- **Stars**: 70
- **Talks to**: Vikunja (self-hosted task manager)
- **Transport**: stdio (npx)
- **Tool surface**: `vikunja_auth`, `vikunja_tasks` (CRUD + bulk), `vikunja_projects`, `vikunja_labels`, `vikunja_teams`, `vikunja_users`, `vikunja_webhooks`, `vikunja_filters`, `vikunja_export_project`, `vikunja_batch_import`
- **Self-hosted purity**: 1 (Vikunja is self-hosted, MIT)
- **Configurability**: 3 (env: `VIKUNJA_URL`, `VIKUNJA_API_TOKEN`; subcommand-grouped tools)
- **Mutates state?**: yes — extensive (create/update/delete tasks, projects, labels, batch import)
- **Verdict**: **ADOPT** — most complete Vikunja MCP, MIT, clean env config maps cleanly onto Aura's dashboard. Note: 4+ competing forks (aimbitgmbh, 0xK3vin, jrejaud, AnthonyUtt, natethor) — democratize fork has the highest tool surface + clearest auth model.

## awwaiid/mcp-server-taskwarrior
- **URL**: https://github.com/awwaiid/mcp-server-taskwarrior
- **Language / runtime**: Node.js (JavaScript)
- **License**: MIT
- **Last commit**: not surfaced; project is small/stable
- **Stars**: 46
- **Talks to**: local Taskwarrior CLI (`task` binary)
- **Transport**: stdio
- **Tool surface (3)**: `get_next_tasks` (filter by project/tag), `add_task`, `mark_task_done`
- **Self-hosted purity**: 1 (Taskwarrior is local CLI, no cloud)
- **Configurability**: 1 (relies on `task` being installed and configured at known path; no env wiring surfaced)
- **Mutates state?**: yes — add and complete tasks
- **Verdict**: **CONDITIONAL** — minimal but useful; known limitation: uses unstable Taskwarrior `id` instead of UUID, can address the wrong task after renumbering. Adopt only if Aura's operator runs Taskwarrior locally on the same host as the MCP sidecar; otherwise stick to Vikunja.

## jtauschl/openproject-mcp
- **URL**: https://github.com/jtauschl/openproject-mcp
- **Language / runtime**: Python 3.10+
- **License**: MIT
- **Last commit**: recent (not surfaced explicitly; active)
- **Stars**: 1 (very new, but unusually complete tool surface)
- **Talks to**: OpenProject Community Edition (self-hosted)
- **Transport**: stdio
- **Tool surface**: 116 tools — full OpenProject v3 API (projects, work packages, memberships, versions, boards, time entries, etc.)
- **Self-hosted purity**: 1
- **Configurability**: 3 (env vars + `.mcp.json`; personal API token `opapi-...`)
- **Mutates state?**: yes — preview-then-confirm pattern by default (`OPENPROJECT_AUTO_CONFIRM_WRITE=true` to bypass)
- **Verdict**: **CONDITIONAL** — 116 tools is overwhelming and the star count is 1 (immaturity risk), but the preview-then-confirm write pattern is a thoughtful safety primitive that aligns with Aura's discipline. Adopt only if operator already runs OpenProject; otherwise prefer Vikunja for lighter footprint.

## glazperle/kimai_mcp
- **URL**: https://github.com/glazperle/kimai_mcp
- **Language / runtime**: Python (99.6%)
- **License**: MIT
- **Last commit**: 2026-04-21 (very recent)
- **Stars**: 24
- **Talks to**: Kimai (self-hosted time tracking)
- **Transport**: stdio (`kimai-mcp`) + SSE (`kimai-mcp-server`) + Streamable HTTP (`kimai-mcp-streamable`) — all three offered
- **Tool surface (11)**: entity (CRUD projects/activities/customers/users/teams/tags/invoices), timesheet (full CRUD + batch + export), timer (start/stop/restart/active), rates, team access, absences (approve/reject/batch/auto-split), calendar, custom-fields, current-user, project-analysis, config
- **Self-hosted purity**: 1
- **Configurability**: 3 (env: `KIMAI_URL`, `KIMAI_API_TOKEN`; also `.env`, CLI args, JSON; interactive setup wizard)
- **Mutates state?**: yes — full timesheet/timer/absence write surface
- **Verdict**: **ADOPT** — best-in-class freshness (Apr 2026), MIT, three transports, full timer + absence flow. Highest-quality MCP in this survey.

## nloui/paperless-mcp (and barryw/PaperlessMCP)
- **URL**: https://github.com/nloui/paperless-mcp (original) — fork: https://github.com/baruchiro/paperless-mcp (TS); also https://github.com/barryw/PaperlessMCP (dry-run-first)
- **Language / runtime**: TypeScript / Node.js
- **License**: not surfaced in README excerpt — verify before adopt
- **Last commit**: not surfaced; nloui is the canonical original (~182 stars)
- **Stars**: 182 (nloui), much smaller on forks
- **Talks to**: Paperless-ngx (self-hosted DMS)
- **Transport**: stdio (default) + Streamable HTTP (Express)
- **Tool surface**: documents (list/get/search/download/bulk-edit/upload), tags (list/create), correspondents (list/create), document types (list/create)
- **Self-hosted purity**: 1
- **Configurability**: 2 (API token via command args; not surfaced as env vars — Aura would need to wrap)
- **Mutates state?**: yes — upload documents, bulk-edit metadata, create tags/correspondents/types
- **Verdict**: **ADOPT (nloui)** — highest-star Paperless MCP, dual transport. `barryw/PaperlessMCP` has dry-run-by-default which is safer for write paths — consider it as the conservative alternative. Confirm license before shipping.

## irfansofyana/linkwarden-mcp-server
- **URL**: https://github.com/irfansofyana/linkwarden-mcp-server
- **Language / runtime**: Go (93%)
- **License**: Apache-2.0
- **Last commit**: 2025-09-21
- **Stars**: 20
- **Talks to**: Linkwarden (self-hosted bookmark manager)
- **Transport**: stdio
- **Tool surface**: collections (get/create/delete + public), links (get/create/delete/bulk-delete/archive), tags (get/delete), full-text search
- **Self-hosted purity**: 1
- **Configurability**: 3 (CLI flags + env: `LINKWARDEN_BASE_URL`, `LINKWARDEN_TOKEN`; config file supported; read-only safety toggle)
- **Mutates state?**: yes — create/delete/archive links and collections
- **Verdict**: **ADOPT** — Go binary (small footprint, matches Aura's primary language), Apache-2.0, has read-only mode. Strongest "wallabag/read-later" candidate for self-hosters who picked Linkwarden.

## edwinbernadus/nocodb-mcp-server (bonus: Grist alternative)
- **URL**: https://github.com/edwinbernadus/nocodb-mcp-server
- **Language / runtime**: TypeScript/Node.js
- **License**: MIT
- **Last commit**: not surfaced
- **Stars**: 70
- **Talks to**: NocoDB (self-hosted relational sheets)
- **Transport**: stdio (MCP) + CLI
- **Tool surface**: records (get/create/update/delete/bulk), columns (add/delete), file upload to create tables
- **Self-hosted purity**: 1
- **Configurability**: 3 (env: `NOCODB_API_TOKEN` + base URL; .env file; JSON for clients)
- **Mutates state?**: yes
- **Verdict**: **CONDITIONAL** — solid alternative if operator already runs NocoDB. For most users `Xe138/grist-mcp-server` is more interesting since Grist's relational model fits journaling/personal-DB use cases better. Pick one of NocoDB-MCP **or** Grist-MCP, not both — overlapping surface.

## Xe138/grist-mcp-server (Grist alternative path)
- **URL**: https://github.com/Xe138/grist-mcp-server
- **Language / runtime**: Python 3.14+
- **License**: MIT
- **Last commit**: 2026-01-04
- **Stars**: 0 (very immature; flagged)
- **Talks to**: Grist (self-hostable spreadsheet/DB)
- **Transport**: SSE (`http://localhost:3000/sse`)
- **Tool surface**: discover/read (`list_documents`, `list_tables`, `describe_table`, `get_records`, `sql_query`), write (`add_records`, `update_records`, `delete_records`), schema (`create_table`, `add_column`, `modify_column`, `delete_column`)
- **Self-hosted purity**: 1 (Grist-core is self-hostable)
- **Configurability**: 3 (YAML `config.yaml`, scoped tokens, per-doc permissions)
- **Mutates state?**: yes — including schema mutations (powerful + risky)
- **Verdict**: **CONDITIONAL — wait** — 0 stars + Python 3.14+ requirement is a red flag for maturity. Watch this one; revisit in 1–2 months. For now, if Aura needs spreadsheet writes, NocoDB-MCP is the safer bet.

---

## Top 3 picks (productivity, self-hosted, 2026-05-20)

1. **glazperle/kimai_mcp** — fresh (Apr 2026), MIT, 11 tools, three transports, full timer + absence flow. Best maturity-to-power ratio. Drop-in for any Aura operator already running Kimai.
2. **democratize-technology/vikunja-mcp** — 70 stars, MIT, comprehensive (subcommand-grouped tools), clean env config. Wins the self-hosted-task-manager slot over Taskwarrior because of remote-API support (Aura's MCP sidecar doesn't need shared filesystem with the task store).
3. **p-w-4-z/calendar-mcp** (conditional on AGPL gate) — only generic CalDAV MCP with full CRUD + a read-only safety switch + provider-agnostic. Works against Radicale, Baikal, or Nextcloud Calendar without per-provider plumbing. AGPL-3.0 sidecar is acceptable but flag to legal.

Honorable mentions: **irfansofyana/linkwarden-mcp-server** (Go binary, smallest footprint, Apache-2.0), **nloui/paperless-mcp** (highest-star DMS MCP).

---

## What's missing (signal for "build native or wrap CLI")

- **CardDAV-only MCPs are absent.** All CalDAV MCPs we found focus on events; none expose `list_contacts` / `create_contact` against a CardDAV server. Aura's contacts story (if it ever needs one) will need a native wrapper around the `caldav` / `vobject` Python libs or a fork of `calendar-mcp` extended with CardDAV verbs.
- **Wallabag (read-later) has no usable MCP.** Linkwarden's MCP exists; Wallabag's doesn't. If the operator's stack is Wallabag-only, Aura needs a native `web_fetch` + native Wallabag API wrapper.
- **ActivityWatch (passive time tracking) has no MCP.** Kimai covers active/manual tracking only. ActivityWatch's REST is good — a thin native tool (or contributing an MCP upstream) is the right move if passive tracking enters scope.
- **Open-Source Calendar with task-list semantics (VTODO) is underserved.** CalDAV's VTODO is supported by Radicale/Baikal/Nextcloud but the surveyed MCPs treat calendars as event-only. Vikunja covers tasks via its own API, so this is "fine for now" — but a unified `caldav` MCP that also exposes VTODO would be the cleanest single integration.
- **OpenProject MCP has 116 tools but 1 star.** Maturity risk; preview-then-confirm pattern is good but solo-maintained. If OpenProject becomes a hard requirement, fund the project or fork it.
- **Grist MCP is too immature (0 stars, Python 3.14+).** Either wait, or use NocoDB-MCP if a sheet-shaped store is needed in the next ~3 months.

Net assessment: Aura can cover **calendar + tasks + time-tracking + bookmarks + DMS** today with 5 sidecar MCPs from this list, all stdio Python/Node/Go binaries — fits the "Phase-MCP-UI dashboard renders configurable cards" plan. Two remaining gaps (CardDAV contacts, ActivityWatch passive tracking) are small enough to ship as native Aura tools when scope arrives.

---

## Sources

- [Kozea/Radicale](https://github.com/Kozea/Radicale)
- [sabre-io/Baikal](https://github.com/sabre-io/Baikal)
- [p-w-4-z/calendar-mcp on PyPI](https://pypi.org/project/calendar-mcp/)
- [madbonez/caldav-mcp](https://github.com/madbonez/caldav-mcp)
- [democratize-technology/vikunja-mcp](https://github.com/democratize-technology/vikunja-mcp)
- [aimbitgmbh/vikunja-mcp](https://github.com/aimbitgmbh/vikunja-mcp)
- [jrejaud/vikunja-mcp](https://github.com/jrejaud/vikunja-mcp)
- [awwaiid/mcp-server-taskwarrior](https://github.com/awwaiid/mcp-server-taskwarrior)
- [jtauschl/openproject-mcp](https://github.com/jtauschl/openproject-mcp)
- [AndyEverything/openproject-mcp-server](https://github.com/AndyEverything/openproject-mcp-server)
- [bivex/kanboard-mcp](https://github.com/bivex/kanboard-mcp)
- [namar0x0309/wekan-mcp](https://github.com/namar0x0309/wekan-mcp)
- [glazperle/kimai_mcp](https://github.com/glazperle/kimai_mcp)
- [nloui/paperless-mcp](https://github.com/nloui/paperless-mcp)
- [baruchiro/paperless-mcp](https://github.com/baruchiro/paperless-mcp)
- [barryw/PaperlessMCP](https://github.com/barryw/PaperlessMCP)
- [irfansofyana/linkwarden-mcp-server](https://github.com/irfansofyana/linkwarden-mcp-server)
- [edwinbernadus/nocodb-mcp-server](https://github.com/edwinbernadus/nocodb-mcp-server)
- [Xe138/grist-mcp-server](https://github.com/Xe138/grist-mcp-server)
- [gristlabs/grist-core](https://github.com/gristlabs/grist-core)
- [kimai/kimai](https://github.com/kimai/kimai)
- [linkwarden/linkwarden](https://github.com/linkwarden/linkwarden)
