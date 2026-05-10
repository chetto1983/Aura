# Aura Container Stack

Aura can run as a server-style Docker Compose stack. In this mode Aura runs
headless, so the Windows tray is disabled and shutdown is handled by Docker
signals.

## Services

- `aura`: the Telegram bot and embedded dashboard.
- `searxng`: local metasearch for Aura's SearXNG web-search provider.
- `garage`: local S3-compatible object storage for manual backup exports.
- `garage-webui`: optional Garage admin UI behind the `garage-ui` Compose
  profile.
- code execution runs Python directly inside the `aura` container for
  `execute_code`, DOCX, XLSX, charts, and generated artifacts. The same
  process runtime also exposes `execute_shell` for container-scoped shell
  commands, tests, builds, `pip`, `git`, `rg`, `jq`, `sqlite3`, and runtime
  diagnostics. Python packages installed with `pip install ...` go to
  `/data/.local` for the non-root Aura user.
- `qdrant`: local vector database sidecar for rebuildable wiki/memory
  embeddings.
- `test`: optional developer test container with Go 1.26.2, Node 22, and the
  Linux sandbox capability needed for no-network skill tests.

The dashboard is bound to `127.0.0.1:18080` on the host by default. SearXNG is bound to
`127.0.0.1:8088` on the host and is reachable from Aura as
`http://searxng:8080`. Garage's S3 API is bound to `127.0.0.1:3900` on the
host and is reachable from Aura as `http://garage:3900`.
Qdrant's REST API is bound to `127.0.0.1:6333` on the host and is reachable
from Aura as `http://qdrant:6333`.

## First Run

Start the stack directly. Aura creates missing runtime, data, wiki, skills,
MCP, and log paths before opening SQLite, so a fresh checkout no longer needs
manual `data/`, `wiki/`, or `skills/` directory setup. Released installs pull
the published GHCR image; you may leave `TELEGRAM_TOKEN` blank to use the
first-run setup wizard:

```powershell
$env:AURA_IMAGE = "ghcr.io/chetto1983/aura:latest"
docker compose -f compose.yaml -f compose.image.yaml up -d
```

If you prefer to pre-seed `.env` instead of using the wizard:

```powershell
New-Item -ItemType Directory -Force data | Out-Null
Copy-Item .env.example data/.env
```

Pin a specific release by setting `AURA_IMAGE` to a tag such as
`ghcr.io/chetto1983/aura:v1.2.3`.

Open the dashboard:

```text
http://127.0.0.1:18080
```

To use a different host port without changing Aura's in-container port:

```powershell
$env:AURA_HOST_PORT = "18081"
docker compose -f compose.yaml -f compose.image.yaml up -d
```

Then open `http://127.0.0.1:18081`.

For local development, build from the working tree instead:

```powershell
docker compose up -d --build
```

Probe SearXNG from the host:

```powershell
go run ./cmd/debug_searxng -base-url http://127.0.0.1:8088 -q "aura search test" -json
```

Run a manual backup export to Garage:

```powershell
go run ./cmd/debug_backup
```

Probe Qdrant health:

```powershell
go run ./cmd/debug_qdrant -url http://127.0.0.1:6333
```

Rebuild the optional Qdrant wiki index after memory cleanup:

```powershell
go run ./cmd/debug_qdrant -url http://127.0.0.1:6333 -rebuild -timeout 5m
```

This command uses `EMBEDDING_API_KEY`, `EMBEDDING_BASE_URL`, and
`EMBEDDING_MODEL`; Aura's local chromem/SQLite index remains the fallback
runtime index when Qdrant is unavailable or returns no usable result.

By default this writes a complete artifact set:

- `backups/<timestamp>/aura-backup.tar.gz`: full restore point with `.env`,
  `aura.db`, `wiki/`, and `skills/`.
- `artifacts/<timestamp>/source-originals.tar.gz`: source metadata plus
  immutable originals such as PDFs, DOCX, TXT, HTML-derived files, uploads, and
  sandbox artifacts stored under `wiki/raw/src_*/original.*`.
- `artifacts/<timestamp>/extractions.tar.gz`: generated OCR/extraction files
  such as `ocr.md`, `ocr.json`, `extract.md`, `extract.json`, cleaned markdown,
  and extracted assets.
- `artifacts/<timestamp>/memory-snapshot.tar.gz`: compiled wiki/memory pages,
  excluding raw sources so OCR bloat does not get duplicated into memory.
- `artifacts/<timestamp>/embedding-index.tar.gz`: a consistent SQLite snapshot
  of `aura.db` plus `wiki/index.md`, enough to preserve embedding cache and
  search/index state without making S3 the live database.
- `artifacts/<timestamp>/audit-bundle.tar.gz`: logs and `reports/` when
  present, for failed ingest cases, E2E artifacts, and debug bundles.
- `artifacts/<timestamp>/manifest.json`: uploaded object manifest.

To upload only the legacy full restore object, run:

```powershell
go run ./cmd/debug_backup -mode full
```

Open the optional Garage Web UI:

```powershell
docker compose --profile garage-ui up -d garage-webui
```

Then browse to `http://127.0.0.1:3909`.

## Test In Docker

Docker is the canonical development and release gate. Run Go tests through the
Compose test profile so the environment matches Aura's Linux container path:

```powershell
docker compose --profile test run --rm test
```

The test image includes Node for local Pyodide runner tests. The service also
adds `SYS_ADMIN` with an unconfined seccomp profile because Linux no-network
skill tests create a temporary network namespace; a plain `docker run
golang:... go test ./...` container cannot do that reliably.
The command excludes incidental Go packages under `web/node_modules`.

The production code-execution path is `SANDBOX_RUNTIME_MODE=process` in the
Aura container. Smoke Python directly in that container:

```powershell
docker compose exec aura python3 - <<'PY'
print(sum(range(101)))
PY
```

Smoke the autonomous CLI surface:

```powershell
docker compose exec aura sh -c "python3 -m pip --version && rg --version && jq --version && sqlite3 --version"
```

Live XLSX/DOCX extraction tests remain opt-in because they exercise large
office/data packages. Run them only after assigning Docker enough memory:

```powershell
docker compose --profile test run --rm -e AURA_SOURCE_PYODIDE_LIVE=1 test go test ./internal/source -run TestPyodide -count=1 -v
```

Inside the Compose network, Aura should use:

```text
http://searxng:8080
```

## Publishing Releases

Aura release tags publish only the Docker image. Push a version tag:

```powershell
git tag v1.2.3
git push origin v1.2.3
```

GitHub Actions builds `ghcr.io/chetto1983/aura:v1.2.3`, also tags it as
`latest`, and publishes linux/amd64 plus linux/arm64 variants. The legacy
desktop binary release has been removed; releases are Docker-image only.

## Data

Visible host folders hold user data:

- `data/`: `.env`, SQLite DB, logs, generated local service secrets, MCP
  config, prompt overlays.
- `runtime-workspace/`: local Aura runtime workspace for agent-facing files
  such as `SOUL.md`, `TOOLS.md`, `AGENT.md`, `HEARTBEAT.md`, `mcp.json`, and
  `inbox/`. `SOUL.md` and `TOOLS.md` are prompt overlays; `AGENT.md` is
  readable runtime guidance but is not injected into the system prompt. Local
  desktop runs also keep wiki and skills under this folder. In Docker the
  equivalent path is `/workspace` through `AURA_RUNTIME_WORKSPACE_PATH`,
  `AURA_WORKSPACE_ROOT`, and `PROMPT_OVERLAY_PATH`.
- Docker volume `aura-wiki`: compiled wiki and source evidence mounted at
  `/workspace/wiki`.
- Docker volume `aura-skills`: installed skills mounted at `/workspace/skills`.
- `garage/`: Garage S3 metadata and object data once the Garage service is enabled.
- `qdrant-storage`: Docker-managed named volume for Qdrant's derived vector
  index. Qdrant warns that Docker Desktop FUSE bind mounts can corrupt vector
  storage, so Aura keeps this one derived index in a named volume and rebuilds
  it from Aura memory when needed.

Back up these folders and Docker volumes before moving hosts or upgrading major versions.

### SQLite Safety

The Docker stack bind-mounts `./data` into the Aura container. On Docker
Desktop, that path crosses the Windows host / Linux VM filesystem boundary.
Aura therefore sets `AURA_SQLITE_JOURNAL_MODE=DELETE` in `compose.yaml` so the
live bind-mounted `aura.db` does not depend on WAL's shared-memory `-shm`
sidecar.

Do not run host-side write/debug commands directly against `data/aura.db` while
the Aura container is running. Use the dashboard/API or run the command inside
the Compose network instead. If a host command must mutate the DB, stop Aura
first:

```powershell
docker compose stop aura
# run the repair/debug command that writes data/aura.db
docker compose up -d aura
```

Manual file copies of a live SQLite DB are not a reliable backup mechanism.
`cmd/debug_backup` and the dashboard backup action create a temporary SQLite
snapshot first, then archive that snapshot. Host-side debug commands that write
`data/aura.db`, such as `debug_memory_closure -apply` and `seed_e2e_env`, refuse
to run while the Compose `aura` service is up.

## Notes

- `compose.yaml` sets `AURA_HEADLESS=true`; desktop builds still keep the tray.
- `compose.yaml` sets `AURA_ENV_PATH=/data/.env`; the setup wizard writes the
  Telegram token there rather than to an ephemeral container filesystem. Aura
  creates the parent directory before the wizard starts.
- `compose.yaml` sets Aura's runtime workspace to `/workspace`; bounded file
  tools and prompt overlays use that narrow workspace instead of the full
  implementation tree.
- `compose.yaml` sets `AURA_SQLITE_JOURNAL_MODE=DELETE` for Docker Desktop
  safety. Desktop/local runs still default to WAL unless explicitly overridden.
- `compose.yaml` sets `WEB_SEARCH_PROVIDER=searxng`, so Aura registers the
  stable `web_search` tool against the bundled SearXNG service instead of
  requiring Ollama web credentials. The paired `web_fetch` tool uses Aura's
  bounded direct HTTP fetcher in this mode.
- `compose.yaml` starts `aura-secrets` before SearXNG, Garage, and Aura. That
  one-shot container generates local service secrets under `data/secrets/` and
  renders the Garage and SearXNG config files consumed by the stack. Existing
  files are preserved, so secrets survive rebuilds and container recreates.
- `compose.yaml` starts Garage with `--single-node --default-bucket`, matching
  Garage's quick-start path. Aura reads the generated Garage S3 key from
  `data/secrets/aura.env` and also supports `GARAGE_S3_ACCESS_KEY_FILE` /
  `GARAGE_S3_SECRET_KEY_FILE` for Docker-secret style deployments.
- Backup export is manual first. It writes a full restore point under
  `backups/YYYY-MM-DD-HHMMSS/` plus categorized artifact archives under
  `artifacts/YYYY-MM-DD-HHMMSS/`. SQLite and wiki storage remain local files.
- The generated SearXNG settings file enables JSON output. Without `json` in
  `search.formats`, SearXNG returns `403` for API requests with `format=json`.
- `compose.yaml` starts Qdrant from `qdrant/qdrant:latest` with storage in the
  Docker-managed `qdrant-storage` volume. Aura exposes `QDRANT_URL`,
  `QDRANT_COLLECTION`, optional `QDRANT_API_KEY`, and `SEARCH_BACKEND` in the
  dashboard settings. `SEARCH_BACKEND=chromem` keeps local chromem/SQLite search
  as the default; `SEARCH_BACKEND=qdrant` queries the Qdrant sidecar first and
  falls back locally.
- The app container enables `SANDBOX_ENABLED=true` and uses
  `SANDBOX_RUNTIME_MODE=process`. `execute_code` runs through `python3` inside
  the Aura container with the same mounted workspace/data access as Aura.
- SQLite remains Aura's canonical state store. MongoDB was evaluated for future
  high-volume archives/traces/audit logs, but it is not part of the default
  stack and should not replace `aura.db` until repository metrics justify it.
