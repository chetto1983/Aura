# A member administering the deployment — before and after the identity.create gate

Date: 2026-10-10. Trees: master `d25c4a027` and PR #149 (`ccr-56123334-7n4k4s`). Scope: the
routes prd.md §3 (2026-10-10) moves to `identity.create`.

Not the lab VM. This ran in a cloud container with Docker: `aura serve --only=cli` built from
each tree, run the way the web-e2e CI job runs it (`AURA_PROFILE=dev`, Authula behind
`web/e2e/https-proxy.mjs` on `https://127.0.0.1:9443`), on the compose `postgres` and
`arcadedb` services. Two identities: the seeded operator (`local`, the bootstrap admin) and a
member inserted in `aura.identities` with exactly `identity.UserSet()` (`agent.run`,
`governance.read`, `governance.write`, `share.public`), its Authula account seeded and linked
like the operator's (`scripts/authula_seed_e2e.go`, `LinkUser`). Each identity signed in
through the real login page in Chromium; every call below is a same-origin `fetch` riding that
session cookie.

## What this run does not prove

- **Not the provisioning saga.** The member was inserted, not provisioned: no Garage bucket,
  no sandbox box, no Telegram link, no TOTP. The routes under test read only the session and
  `capability_grants`, so the boundary is the same; what a provisioned member's other planes
  allow is not measured here.
- **`AURA_MUSR_ISOLATION` off and `AURA_PROFILE=dev`.** No route here branches on either, by
  reading the code, not by measurement under the strict profile.
- **Not inside the appliance container.** The stdio exec proof ran as a child of `aura serve`
  on the host; in the appliance that process is the aura container.
- **Not the other open paths** the amendment lists: scheduler, house skills, `npx skills add`,
  background shells.

## Measured

A status that is not 403 means the request reached the handler: 400, 404, 409, 502 and 503
are the handler's own answers (no management key, no restart supervisor, no such server).

| Call | master, member | PR, member | PR, admin |
|---|---|---|---|
| `GET /api/me` | 200 | 200 | 200 |
| `GET /api/admin/identities` | **200**, the roster | 403 | 200 |
| `DELETE /api/admin/identities/{admin}/capabilities/share.public` | **200**, the admin lost `share.public` (read back in Postgres) | 403, the grant intact | 200 |
| `POST /api/admin/identities/{member}/capabilities` | **200** | 403 | 200 |
| `GET /api/admin/audit` | 400 (reached) | 403 | 400 |
| `GET`/`POST /api/admin/identities/{member}/credit` | 409 / 400 (reached) | 403 / 403 | 409 / 400 |
| `GET /api/admin/spend/overview` | 503 (reached) | 403 | 503 |
| `POST /api/admin/restart` | 409 (reached; no supervisor outside the container) | 403 | 409 |
| `POST /api/governance/mcp` `{command: touch, args: [/tmp/aura-member-exec-proof]}` | **502 after spawning it: the file exists** | 403, no file | 502, file exists |
| `PATCH …/env`, `POST …/trust`, `POST …/enable`, `…/disable`, `DELETE /api/governance/mcp/{name}` | 404 / 400 (reached) | 403 each | 404 / 400 |
| `PUT`/`DELETE /api/settings/AURA_LOOP_MAX_STEPS` | **200 / 200** | 403 / 403 | 200 / 200 |
| `PUT`/`DELETE /api/settings/TELEGRAM_BOT_TOKEN` | **200 / 200**, the deployment's bot token replaced | 403 / 403 | 200 / 200 |
| `POST`/`DELETE /api/governance/mcp/memory/authorization` (own account) | 200 / 200 | 200 / 200 | 200 / 200 |

So on master a member revoked a capability from the admin, replaced the deployment's Telegram
bot token and had `aura serve` spawn a process of their choosing. On the PR every one of those
18 calls answers 403 before its handler, the member still authorizes their own MCP account,
and the admin reaches every handler exactly as the member did on master.

The cockpit needed no change: `AppShell` already sends a non-admin away from Governance and
Settings, deep links included (`web/src/AppShell.tsx`, `ADMIN_MODES`).

## Found, not fixed in this run

The first boot after the member was inserted exited with
`reconcile ArcadeDB tenant …: ensure memory schema: arcadedb: http 500: Cannot create the
property 'name' in type 'Entity' because it already exists`. Two reconcilers ensure the new
tenant's schema at the same time, and the loser's "already exists" ends the daemon; the next
boot, with the schema in place, came up. Reproduced once; it is outside this PR.
