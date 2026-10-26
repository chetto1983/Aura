# One path: every tool runs in the box — closing record

Date: 2026-10-26. This closes `docs/superpowers/plans/2026-08-03-one-path-sandbox-only.md`.
Steps 1–2 landed in `0d76f84a8` (2026-08-03). The plan's Status of 2026-10-15 listed two Step 3
items and Steps 4–5 as open. The operator chose to record what was measured and close
("Registra e chiudi").

**Verdict:** the agent's tools have one path, the box. Step 3's image list was wrong on three
packages, as `0d76f84a8` had already said: Node/npm, uv and python3 have live callers in the aura
container. They stay. The dead pip cache volume is gone, and the stale comments are corrected.

## Step 3: what the aura container still runs

The processes Go code starts outside tests (`exec.Command` call sites, 2026-10-26):

| Caller | Binary | Runs in the aura container because |
|---|---|---|
| `internal/mcp/sdkclient.go` | the server's command | a stdio MCP server whose runtime is not `box` |
| `cmd/aura/mcp_prepare.go` | `uv`, `npm` | it prepares that server's environment (amendment #211) |
| `internal/skills/installer.go` | `npx` | `npx skills add/find` |
| `cmd/aura/skills_snippet.go` | `python3`, `node`, `sh` | a skill snippet's interpreter |
| `internal/packs/fetch.go` | `git` | pack fetches |
| `internal/cron/handlers/backup.go` | `pg_dump` | backups |
| `internal/pdftext`, `cmd/aura-media-index` | `pdftotext`, `pdftoppm` | ingest |
| `internal/agent/hooks_command.go` | the hook's command | operator command hooks |

`internal/agent/tools` has no `os/exec` import: every tool that needs a shell or a file goes
through the router.

The running image on the lab VM (`c034fb3c5`, read with `docker history`):

| Layer | Size |
|---|---|
| whole image | 1.43 GB |
| Node 24 (NodeSource) and npm 11.21.1 | 222 MB |
| curl, git, gnupg, python3, python3-pip, python3-venv | 179 MB |
| uv and uvx | 64 MB |
| the Craft apps (two layers) | 247 MB + 27.9 MB |

The `aura` container itself ran one process, `aura`. One box, `aura-box-448ddbe1…`, existed,
stopped by the idle reaper 30 minutes before the read.

**The caches.** `/root/.npm` and `/root/.cache/uv` serve the installers above, so their volumes
stay. Nothing in the container runs pip: `mcpenv` calls `uv pip install`, which caches under
`/root/.cache/uv`. So `aura-pip-cache-host` is removed from `compose.yaml`. An existing
deployment keeps the old volume, unused, until someone removes it.

**Comments corrected.** `compose.yaml`'s sandbox comments still described a router that only the
strict profile builds, a socket mounted only into the proxy, and a "host-direct shell_exec path".
The Dockerfile said the agent reaches uv from `shell_exec`. The same phrase sat in
`internal/config/config.go`, `cmd/aura/chat_boot.go`, `cmd/aura/main.go`,
`internal/gateway/{decide,gateway}.go`, `internal/mcp/manager/catalog.go` and two test comments.
All now describe the code as it runs.

## Step 4: the CLI boundary

- Only five verbs build a router: `serve`, `chat`, `shell`, `identity` (create, deprovision)
  and `mcp` (through `operatorBoxLauncher`). The others (`db`, `docs`, `memory`, `skills`,
  `pack`, `task`, `retention`, `doctor`, `objectstore`, `web`, `config`, `version`) never
  construct one and need no Docker.
- `buildSandboxRouter` never returns nil. A Docker client that cannot be composed gives a
  backend-less router, so every routed tool denies. `TestBuildSandboxRouterStrictClientErrorFailsClosed`
  and `TestBuildSandboxRouterWiresEgress` cover it without a daemon.

## Step 5: coverage

The plan's third consequence was coverage moving into a tier that counted for nothing. Amendment
#203 settled it.
- `scripts/coverage_package_policy.json` holds `internal/agent/tools` at the 85% target.
- It delegates `internal/sandbox/usersandbox` to the native `docker_integration` report, which
  must meet the same floor.
- On `df12d57e8` (2026-10-26, run 40285370268), the owned-surface coverage gate and the Sandbox
  docker_integration tier both passed.

## What this does not show

- The plan's live acceptance, 699 on `Clienti.xlsx` computed in `aura-box-<identity>`, was never
  recorded, and it was not re-run here.
- Whether the in-container installers should move into the box too. That would drop roughly 300
  MB from the image, and it is a new decision, not part of this plan.
- python3-pip and python3-venv may have no caller left in the container. They were not removed,
  because the image was not to change in this close.
