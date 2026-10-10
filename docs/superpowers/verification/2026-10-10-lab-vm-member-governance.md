# A provisioned member on the lab VM: scheduler, house skills, install, admin routes

Date: 2026-10-10. Lab VM `192.168.101.158`, the appliance stack under `/opt/aura`, profile
`single_user_hardened`, `AURA_MUSR_ISOLATION=true`. Image `b9b984372` for the main run
(the identity.create gate of PR #149 and the scheduler and skills rules of prd.md §3,
2026-10-10). Image `9574e11c3` for the pause and resume re-run.

Before the run the VM held one human identity, the operator's, plus the system `aura-cli`. A
member was provisioned for the run and purged after it, on the operator's go-ahead:

- Provisioned with `aura identity create` inside the `aura` container: the cockpit wizard's
  saga, with an Authula account, an ArcadeDB database, and box and egress containers.
  `-operator` was the operator's identity. The secrets were typed through a PTY
  (`scripts/musr_live_run_ptyexpect.py`), never argv.
- Identity `703a683c-b3d3-4d85-8382-ae293b95e2ff`. `GET /api/me` listed exactly
  `agent.run, governance.read, governance.write, share.public`.
- Signed in through Authula with `scripts/musr_live_run_authula_helpers.sh`, without a TOTP
  step, as `scripts/musr_live_run.sh` documents for a first login.
- The admin calls used the operator's own account.
- Fixture: a non-builtin house skill, `house-probe`, written into `AURA_SKILLS_DIR`
  (`/var/lib/aura/skills`).
- `npx` was the appliance's own. The install calls were chosen so that nothing is fetched.

## What this run does not prove

- **A member acting on a task of their own.** The member owned none, and making one takes an
  agent turn. `TestSchedulerLetsAMemberRunTheirOwnTask` and the cloud run cover it.
- **An admin's edit or run of the real backup.** Neither was made on the operator's schedule.
  They were measured on the cloud stack only
  (`2026-10-10-member-scheduler-and-skills.md`).
- **An admin's install that fetches a source.** It would install a skill on the operator's
  deployment. The empty-source 400 shows only that the gate lets an admin through.
- **The cockpit's own controls.** Every call went through the API with the session cookies
  the SPA uses. The operator saw the member appear in the cockpit's identity list, and no
  button was clicked.
- **A first login's TOTP enrollment and password change, and the Telegram link.** The deep
  link was minted and never opened.

## Measured, as the member (image `b9b984372`)

| Call | Answer |
|---|---|
| `GET /api/governance/scheduler` | 200 `{"tasks":[]}` |
| `GET …/{backup}/runs`, `GET …/{the operator's cancelled backup}/runs` | 404 `task not found` |
| `PATCH`, `POST …/run`, `DELETE` on the active backup | 404 `task not found` |
| `POST …/{backup}/pause` | 404 `404 page not found`: the router's, see below |
| `GET /api/governance/skills` | 200, `house-probe` listed |
| `POST …/house-probe/archive`, `/restore`, `DELETE …/house-probe` | 400 `no active skill by that name`: the verbs resolved in the member's own root |
| `POST /api/governance/skills/install` `{"source":"member-chosen/does-not-exist"}` | 403 |
| `GET /api/governance/skills/catalog` | 403 |
| `GET /api/admin/identities`, `/api/admin/audit`, `/api/admin/spend/overview` | 403 |

## Measured, as the admin (image `b9b984372`)

| Call | Answer |
|---|---|
| `GET /api/governance/scheduler` | 200, the backup listed |
| `GET …/{backup}/runs` | 200, the run history |
| `POST …/house-probe/archive`, `/restore` | 204, 204 |
| `POST /api/governance/skills/install` `{"source":""}` | 400 `install source is empty`: past the gate, stopped before `npx` |
| `GET /api/admin/identities` | 200, three identities |

## Read back after the run

- The backup `01a0e1f6…` was unchanged: `0 1 * * *`, next run 23:00 UTC, `updated_at`
  12:17:07 as before the run.
- No install left a trace: no `aura-skill-install-*` directory, no install line in the
  `aura` log.
- `house-probe` was active, where the admin's archive and restore had left it.

## Found: pause and resume were never mounted

On `b9b984372` the member's `POST …/pause` answered `404 page not found` from the parent mux,
not the handler's `task not found`. Pause and resume shipped on 2026-10-07 registered on the
AG-UI mux only, so the board's Pause and Resume buttons had answered that 404 ever since.
Every route in `httpMutationRoutes` (97) was walked through the real parent mux: these two
were the only ones that never reached the AG-UI handler. Fixed in `9574e11c3`.

Re-run on `9574e11c3`, both calls against the active backup:

| Who | `POST …/pause` | `POST …/resume` |
|---|---|---|
| member | 404 `task not found` | 404 `task not found` |
| admin | 403 `the database backup cannot be paused` | 409 `task is not paused` |

Neither writes: the backup is refused before the store, and resume finds no paused task.

## Cleanup

- `aura identity purge 703a683c… --confirm` printed `ok: identity … purged`.
- Read back afterwards: the identity row and the member's box and egress containers were
  gone, and `house-probe` was removed. Authula's own tables were not read.
- Also seen: box and egress containers for `aura-cli` (`…039`). They were created at
  13:13:36, when `aura` started on `9574e11c3`, five minutes before the purge. I did not
  attribute them to the run and did not investigate them.
