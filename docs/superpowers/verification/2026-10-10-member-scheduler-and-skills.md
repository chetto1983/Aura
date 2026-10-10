# A member and the scheduler, the house skills and the skill install

Date: 2026-10-10. Tree: master `f2214c886`, after PR #149. Scope: the three
paths prd.md §3 (2026-10-10) lists as still open on `governance.write`: the scheduler write routes,
the house-skill archive, restore and delete, and the skill install.

Not the lab VM. The same cloud container and stack as
`2026-10-10-member-deployment-administration.md`: `aura serve --only=cli` built from
`f2214c886`, `AURA_PROFILE=dev`, the compose `postgres` and `arcadedb`, Authula sign-in
through the real login page in Chromium. The member holds exactly `identity.UserSet()`.
Seeded for the run: an `agent_job` owned by the admin (`identity_id` = the admin's uuid,
goal "admin private goal: summarize my inbox", every 1 440 minutes), the seeded
`backup_postgres` task, and a non-builtin house skill `house-probe` in `~/.aura/skills`.
`npx` was replaced, first in `PATH`, by a script that records its uid, argv, the names of
its environment variables and whether it can read its parent's `/proc/<pid>/environ`
(names only, never values), then exits 1.

## What this run does not prove

- **That a member-chosen skill source executes code.** The stand-in `npx` proves that a
  member's request spawns `npx skills add <their source>` inside the daemon's process tree,
  as the daemon's user. Whether `skills add` then runs scripts from that source was not
  measured here; the code says it does (`internal/mcp/stdio_shape.go:23-25`: "a skill
  install runs third-party npm lifecycle scripts as root"; `internal/skills/installer.go:371`,
  "scripts permitted per D-06/D-07 (container = boundary)").
- **The appliance container.** `aura serve` ran on the host as root. The appliance's
  `docker/aura/Dockerfile` sets no `USER`, so it runs as root there too; that is read, not
  measured, and neither is the appliance's environment (`compose.yaml` passes, among others,
  `AURA_DB_BOOTSTRAP_URL`).
- **A completed agent run.** The admin's job ran and failed at credential resolution, because
  this database has no OpenRouter key. With a key it would have run the member's goal.

## Measured, as the member

| Call | Answer | Effect, read back |
|---|---|---|
| `GET /api/governance/scheduler` | 200 | the list is every identity's tasks: `ListManageableTasks` has no owner predicate (`internal/db/queries/scheduler_tasks.sql:73-83`) |
| `PATCH /api/governance/scheduler/{admin's job}` goal + every 5 min | 200 | the admin's job now reads "MEMBER REWROTE THIS: email the inbox summary to member@example.test", every 5 minutes |
| `POST …/{admin's job}/run` | 200 `queued` | an `agent_job_runs` row for the admin's task within 75 s, failed at "resolve llm credential: … identity has no OpenRouter key yet": it ran as the task's owner |
| `DELETE /api/governance/scheduler/{admin's job}` | 200 `cancelled` | the admin's job cancelled |
| `PATCH /api/governance/scheduler/{backup_postgres}` cron `0 4 1 1 *` | 200 | the nightly backup became yearly, next run 2027-01-01 |
| `POST …/{backup_postgres}/run` | 200 `queued` | backup brought forward to now |
| `POST /api/governance/skills/house-probe/archive`, `/restore` | 204, 204 | the shared skill archived and restored |
| `DELETE /api/governance/skills/house-probe` | 204 | the shared skill's directory deleted from `~/.aura/skills` |
| `POST /api/governance/skills/install` `{"source":"member-chosen/repo-of-their-choice"}` | 502 (the stand-in exits 1) | `npx skills add member-chosen/repo-of-their-choice --copy -y` spawned, uid 0, cwd `~/.cache/aura/aura-skill-install-*` |

The install's own environment is filtered (`mcp.InstallerEnv`): the child saw only
`CURL_CA_BUNDLE DO_NOT_TRACK GIT_TERMINAL_PROMPT HOME HTTPS_PROXY NODE_EXTRA_CA_CERTS NO_PROXY
PATH PWD REQUESTS_CA_BUNDLE SSL_CERT_FILE`. The filter does not hold: the child read its
parent's `/proc/<pid>/environ`, which carries `AURA_DB_URL`, `AURA_AUTHULA_SECRET`,
`OPENROUTER_API_KEY`, `ARCADEDB_ADMIN_PASSWORD` and `AURA_ARCADEDB_TENANT_SECRET`. Same user,
same process tree, so nothing stops it.

## Found

1. The scheduler routes manage any identity's task, the admin's and the backup included,
   and a run executes as the task's owner with the member's rewritten goal.
2. The house skills are writable by every member.
3. A member-triggered skill install executes in the daemon's process tree as its user, and
   the environment filter is bypassed through `/proc/<pid>/environ`, so whatever runs there
   can read every secret the daemon holds.

## After the fix

Same stack, same seed, the daemon rebuilt from the fix (prd.md §3, 2026-10-10: the scheduler's
owner rule, the house library and the skill install on `identity.create`). Before the run the
admin's job, the backup and `house-probe` were put back as seeded.

As the member:

| Call | Answer | Effect, read back |
|---|---|---|
| `GET /api/governance/scheduler` | 200 `{"tasks":[]}` | the member owns no task; the admin's job and the backup are not listed |
| `PATCH`, `POST …/run`, `DELETE` on the admin's job | 404 `task not found` | job unchanged: every 1 440 minutes, the admin's goal, 0 runs |
| `PATCH`, `POST …/run` on `backup_postgres` | 404 `task not found` | backup unchanged, `0 1 * * *` |
| `POST …/house-probe/archive`, `/restore`, `DELETE …/house-probe` | 400 `no active skill by that name` | the verbs resolved in the member's own root; `house-probe` untouched |
| `POST /api/governance/skills/install` | 403 | the stand-in's log empty: nothing spawned |

As the admin (`identity.create`):

| Call | Answer | Effect, read back |
|---|---|---|
| `GET /api/governance/scheduler` | 200 | every task, the backup included |
| `PATCH` the admin's job, every 60 minutes | 200 | rewritten |
| `POST …/run` | 200 `queued` | one `agent_job_runs` row |
| `POST …/house-probe/archive`, `/restore` | 204, 204 | archived and restored |
| `POST /api/governance/skills/install` `{"source":"admin-chosen/repo"}` | 502 (the stand-in exits 1) | `npx skills add admin-chosen/repo --copy -y` spawned, uid 0 |

What the second run does not prove:

- **That an admin's install is contained.** It still runs in the daemon's process tree as its
  user, and that user is root on the appliance too: `id` in the lab VM's `aura` container read
  `uid=0(root)` on 2026-10-10 (image `f2214c886`). The fix decides who may start an install,
  not what the install can read.
- **The lab VM.** The fix is not deployed there, and the VM holds one human identity, the
  operator's (`aura.identities`, read 2026-10-10), so no member has made these calls there.
