# Runtime And Storage Evaluation

Date: 2026-05-07

## Pyodide Runtime Decision

Decision: move Docker installs to a Pyodide sidecar and keep the local bundled runner only as desktop fallback.

Reasons:

- Docker is now Aura's release runtime.
- Keeping Node and the Pyodide bundle inside the Aura app image made the app image larger and mixed application state with sandbox execution.
- The sidecar keeps the stable `execute_code` tool boundary while allowing warm runtime health checks and future worker-pool optimization.

Implementation slice:

- Add `SANDBOX_RUNTIME_MODE=auto|container|local`.
- Add `SANDBOX_RUNTIME_URL`.
- Compose sets `SANDBOX_RUNTIME_MODE=container` and `SANDBOX_RUNTIME_URL=http://pyodide:8787`.
- Aura app image no longer installs Node or copies `/app/runtime/pyodide`.
- The `pyodide` service owns `/runtime` and exposes only the internal Compose port `8787`.
- `execute_code` loads Pyodide packages on demand from submitted Python imports, not the whole office/data profile, so trivial code does not OOM or pay pandas/scipy/matplotlib startup cost.

Image note:

- `pyodide/pyodide:latest` does not publish a current Docker Hub manifest.
- Pyodide's current Docker documentation points to `pyodide/pyodide-env` as the Docker Hub image for the Pyodide environment.
- The sidecar is therefore based on `pyodide/pyodide-env:latest` pinned by digest and overlays only Aura's HTTP runner shim.
- `pyodide/pyodide-env` is a single-manifest image at the time of validation, so Raspberry/ARM support needs a separate image validation before promising ARM Docker releases.

Validation:

- `docker compose build --no-cache pyodide`.
- `AURA_HOST_PORT=18080 docker compose up -d --force-recreate pyodide aura`.
- Live `/status` on `http://127.0.0.1:18080/status`.
- Compose-network `debug_sandbox -tool-smoke -runtime-url http://pyodide:8787`: `5050`, `elapsed_ms=4`.
- Compose-network `debug_sandbox -artifact-smoke -runtime-url http://pyodide:8787`: persisted CSV and PNG artifacts.
- Full all-import `debug_sandbox -smoke` is not promoted as a sidecar release gate yet; it exceeded the 15 minute debug timeout because it tries to load the entire legacy office/data profile in one warm runtime. The production path now uses import-driven package loading and should be evaluated with realistic tool and artifact scenarios.
- `go test ./... -count=1`.
- `npm --prefix web run i18n:check`.
- `npm --prefix web run build`.
- `docker compose config --quiet`.

## SQLite To MongoDB Evaluation

Decision: do not replace SQLite with MongoDB now.

Recommended path: keep SQLite as canonical state and defer MongoDB behind repository interfaces.

Why SQLite stays canonical:

- Aura is a standalone second brain; one local `/data/aura.db` keeps setup, backup, and restore simple.
- Current state is relational and transactional: auth, pending users, settings, scheduler, conversations, proposals, wiki issues, swarm lifecycle, embedding cache, and FTS mirror.
- Qdrant already owns vector/search sidecar duties; Garage already owns artifact/backup duties.
- MongoDB would add a persistent service, auth/secrets, backup complexity, and migration risk without measured pressure from SQLite.

Future MongoDB candidates, only after metrics justify it:

- High-volume conversation archives.
- Swarm execution traces.
- Append-only audit/event logs.
- Large source metadata collections.

Non-candidates for now:

- Auth and dashboard tokens.
- Settings.
- Scheduler.
- Review/proposal queues.

Migration guardrail:

- Finish repository boundaries first.
- Add repository-level export/import contracts before any adapter.
- Prototype one optional adapter behind a feature flag.
- Promote only if measured SQLite pain appears: DB size, write contention, query latency, retention pressure, or multi-instance requirements.
