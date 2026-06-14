# Phase 17: Packaging & Distribution - Pattern Map

**Mapped:** 2026-06-14
**Files analyzed:** 14 (8 CREATE + 6 MODIFY; `internal/knowledge/client.go` is a touch-point only, NOT modified)
**Analogs found:** 14 / 14 (every file has an in-repo analog — this is a refactor/packaging phase over a mature tree, not a greenfield)

> **For the planner:** Every artifact in this phase has a concrete, verbatim shape already shipped in the repo. The directive is **replicate the established shape**, not invent. The two highest-value templates are (a) the `aura-agent-memory-mcp` compose sibling block (`compose.yaml:143-206`) — the topology template for the whatsapp sibling, the `aura-migrate` one-shot gating, and the Caddy split — and (b) the `docker/agent-memory/` + `docker/markitdown/` pinned-pip sidecar Dockerfiles — the runtime-stage template for `docker/aura/Dockerfile`.

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `docker/aura/Dockerfile` (CREATE) | config (image build) | build / file-I/O | `docker/agent-memory/Dockerfile` + `docker/markitdown/Dockerfile` + root `Dockerfile` (build stage) | exact (pinned-pip slim sidecar) |
| `compose.gvisor.yaml` (CREATE) | config (compose override) | request-response (runtime tier) | `.planning/spikes/008-sandbox-token-auth/compose.token.yaml` (override-per-tier) | role-match (override pattern, new field) |
| `scripts/install.sh` (CREATE) | config (installer) | batch / file-I/O | `scripts/restore_drill.sh` + `scripts/coverage_gate.sh` (shell guards/style) | role-match (shell idiom, new domain) |
| `cmd/aura/doctor.go` (CREATE) | command (CLI subcommand) | request-response (aggregate health) | `cmd/aura/web.go` (`runWebDoctor`) + `cmd/aura/mcp_status.go` (`mcpDoctorAll`) | exact (per-check + exit-code) |
| `scripts/aura.service` (CREATE) | config (systemd unit) | event-driven (boot autostart) | `deploy/aura-scheduler.service` | exact (in-repo systemd unit) |
| `caddy/Caddyfile` + caddy compose service (CREATE) | config (reverse proxy) | request-response (TLS front) | `compose.yaml` loopback sibling services (`searxng`/`aura-agent-memory-mcp`) | role-match (no in-repo Caddy yet) |
| `.goreleaser.yaml` (MODIFY) | config (release) | build / pub | existing `builds`/`archives`/`release` blocks (same file) | exact (extend in place) |
| `compose.yaml` aura service (MODIFY) | config (compose) | request-response | `aura-agent-memory-mcp` sibling block (`compose.yaml:143-206`) | exact (de-harden + mirror sibling) |
| `Dockerfile` (root, MODIFY/REMOVE) | config (image build) | build | superseded by `docker/aura/Dockerfile` | n/a (removal/repoint) |
| `internal/mcp/manager/runtime.go` (MODIFY) | service (MCP dispatch) | request-response (launch dispatch) | the existing `RuntimeLaunchConfig` switch + `errMCPServerBlocked` sentinel (same file) | exact (extend the dispatch) |
| `internal/mcp/manager/catalog.go` (MODIFY) | service (recipe catalog) | CRUD (recipe records) | the `memory` streamable-HTTP recipe (`catalog.go:130-146`) + `memoryRecipeURL()` | exact (mirror the HTTP recipe) |
| `internal/config/config.go` (MODIFY) | config (root composite) | transform (env → Config) | the empty-`SEARXNG_URL` fail-soft (`config.go:63-69, 270`) + `LoadDB` (`config.go:205-207`) | exact (mirror SEARXNG fail-soft) |
| `internal/llm/config.go` (MODIFY) | config (LLM load) | transform (env → Config) | `ErrMissingAPIKey` + `Load` (`llm/config.go:82, 208-211`) | exact (defer the sentinel) |
| `cmd/aura/main.go` (MODIFY) | command (dispatch) | request-response | the `switch os.Args[1]` table + `usage()` (`main.go:43-96`) | exact (add a `case "doctor"`) |
| `internal/cron/handlers/backup.go` (MODIFY) | service (cron handler) | batch / file-I/O | `BackupHandler.Run` + `dumpArgv` (same file) | partial (in-box `docker exec` paradox — see Shared Patterns) |

---

## Pattern Assignments

### `docker/aura/Dockerfile` (config, build/file-I/O) — CREATE

**Analogs:** `docker/agent-memory/Dockerfile` (pinned-pip slim runtime + cache-stable COPY-metadata-first), `docker/markitdown/Dockerfile` (apt curl + `pip install --no-cache-dir` pinned-version block), root `Dockerfile` (the multi-stage golang build stage to keep).

**Build stage to KEEP (root `Dockerfile:1-10`)** — the cross-compile shape is correct; only the *runtime* stage changes from distroless to fat:
```dockerfile
FROM golang:1.26.4-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/aura ./cmd/aura
```
> Note D-06 / RESEARCH Pattern 1 names `golang:1.26.4` (Debian) for the build stage too; the existing root uses `-alpine`. Either compiles `CGO_ENABLED=0` cleanly — planner picks; the load-bearing part is the runtime stage below.

**Runtime stage — mirror the pinned-pip sidecar shape (`docker/markitdown/Dockerfile:4-17`):**
```dockerfile
# markitdown analog — apt for the healthcheck/runtime tools, then pinned pip with --no-cache-dir
FROM python:3.12-slim
RUN apt-get update && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir \
    "markitdown[all]==0.1.6" \
    ...
```
**Cache-stable metadata-first ordering — agent-memory analog (`docker/agent-memory/Dockerfile:29-44`):**
```dockerfile
RUN apt-get update && apt-get install -y --no-install-recommends gcc \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
# Copy metadata first for layer caching, then the vendored package source.
COPY pyproject.toml README.md README-pypi.md constraints.txt ./
COPY src/ ./src/
RUN pip install --no-cache-dir -c constraints.txt -e ".[mcp,google,openai]"
```

**Pattern to replicate (D-06 / Pitfall #2 cache-stability):** debian/python:slim runtime base → `apt-get … --no-install-recommends … && rm -rf /var/lib/apt/lists/*` (one layer) → `COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /usr/local/bin/` → `pip install --no-cache-dir --break-system-packages mcp-neo4j-cypher==0.6.0` → recipe pre-bake warm layers (Req 7) → `ENV AURA_IN_CONTAINER=1` (the D-08 marker; the ONLY ENV, and it is NOT a secret — Pitfall #4) → **`COPY --from=build /out/aura /usr/local/bin/aura` as the LAST layer**. The exact illustrative file is RESEARCH Pattern 1 (17-RESEARCH.md:197-222). Pin every version (markitdown/agent-memory both pin exactly — supply-chain discipline). **NO secret ARG/ENV/RUN echo** (Pitfall #4 — `docker history` must be clean).

---

### `compose.yaml` — the `aura` service (config, request-response) — MODIFY (DE-HARDEN)

**Analog (de-harden FROM):** the current `aura` service block — `compose.yaml:10-51` — IS the audit jail `ec7fe2f6`. **Analog (mirror TO):** the `aura-agent-memory-mcp` sibling block — `compose.yaml:143-206` — the canonical depends_on-healthcheck + loopback-publish + named-volume topology.

**The jail to REMOVE (D-01, `compose.yaml:17-22`):**
```yaml
    user: "65532:65532"     # REMOVE (D-01 — breaks self-extension/write)
    read_only: true          # REMOVE (D-01 — breaks writable rootfs parity)
    cap_drop:
      - ALL                  # REMOVE (D-01 — breaks shell_exec, exit 127 spike 059)
    security_opt:
      - no-new-privileges:true   # planner-confirm DROP (A6 — spike 059 ran with none)
```
**The structural invariants to KEEP / ADD (D-02):** `mem_limit` + `cpus` (already at `compose.yaml:23-24`) + ADD `pids_limit`; NO host `/var/run/docker.sock` mount (the lone invariant); `restart: unless-stopped` (already `:16`).

**The sibling topology to MIRROR (`aura-agent-memory-mcp`, `compose.yaml:143-206`):**
```yaml
  aura-agent-memory-mcp:
    build:
      context: ./docker/agent-memory
    image: ${AURA_AGENT_MEMORY_MCP_IMAGE:-aura-agent-memory-mcp:local}
    pull_policy: never
    container_name: aura-agent-memory-mcp
    restart: unless-stopped
    depends_on:
      neo4j:
        condition: service_healthy
      aura-llama-embed:
        condition: service_healthy
    environment:
      NEO4J_URI: bolt://neo4j:7687
      ...
    ports:
      - "127.0.0.1:${AURA_AGENT_MEMORY_MCP_PORT:-8091}:8080"
    healthcheck:
      test: ["CMD-SHELL", "python -c \"import socket; ...\""]
```

**Pattern to replicate:**
- **`aura` service:** build-from-`docker/aura` (or `image: ${AURA_IMAGE:-...}` pinned ghcr tag for appliance, `pull_policy: never` for dev — already `:13-14`), `depends_on` healthchecks for `postgres`/`neo4j`/`aura-llama-embed` (extend the current `:25-27` which only waits on postgres), secrets via `environment:` `${VAR:?...}` (already `:30-37`), loopback publish `127.0.0.1:...` only.
- **`aura-migrate` one-shot (Req 8, D-13):** a sibling running `aura db migrate && aura neo4j migrate`, exit 0; gate the `aura` service with `depends_on: { aura-migrate: { condition: service_completed_successfully } }` — `service_completed_successfully` is the new condition; `service_healthy` (used everywhere, e.g. `:151`) is the established sibling for it. `db migrate` uses `LoadDB()` so it boots keyless (db.go:26) — independent of D-10.
- **`aura-home` volume (Req 2):** replace `aura-runs`/`aura-skills`/`aura-exported-skills` (`compose.yaml:40-43, 341-343`) with a single named `aura-home` → `AURA_CONFIG_DIR`, declared in the top-level `volumes:` map (`compose.yaml:340-348`).
- **whatsapp sibling (Req 4):** a new service mirroring `aura-agent-memory-mcp` exactly (build-context or image, loopback `127.0.0.1:<port>`, healthcheck, `restart: unless-stopped`); fail-soft when down (Pattern below).
- **Caddy service (Req 11):** the ONLY service that publishes on a non-loopback interface; everything else stays `127.0.0.1` (Pitfall #10 — do NOT publish `aura` :9080 on `0.0.0.0`, Caddy fronts it).

---

### `compose.gvisor.yaml` (config, runtime tier) — CREATE

**Analog:** `.planning/spikes/008-sandbox-token-auth/compose.token.yaml` (and siblings 005/006/007) — the established compose-override-per-tier pattern.

**Pattern excerpt (spike 008):**
```yaml
# Spike 008 override — runs ... WITH a bearer token instead of --no-token.
# Usage:
#   docker compose -f compose.yaml -f .planning/spikes/008-.../compose.token.yaml up -d ...
services:
  aura-sandbox-agent:
    command: [...]
    healthcheck:
      test: [...]
```

**Pattern to replicate:** a minimal override that names ONLY the keys it changes (a header comment with the exact `docker compose -f compose.yaml -f compose.gvisor.yaml up -d` invocation, then `services:` → `aura:` → `runtime: runsc`). **Landmine #3 (D-03):** `runtime: runsc` requires host pre-registration in `/etc/docker/daemon.json` (`runsc install`) — compose cannot install it; the installer attempts this only on native-Linux (OFF on Docker-Desktop dev). Keep the override tiny — just the `runtime:` field on the `aura` service.

---

### `internal/mcp/manager/runtime.go` — in-container docker guard (service, dispatch) — MODIFY

**Analog:** the existing `RuntimeLaunchConfig` switch (same file, `runtime.go:83-99`) + the `errMCPServerBlocked` sentinel pattern (`runtime.go:19, 85-87`).

**The dispatch to extend (`runtime.go:83-99`):**
```go
func RuntimeLaunchConfig(name string, server mcp.ManagedServer) (mcp.ServerConfig, error) {
	trust := normalizedTrustForServer(server)
	if trust == mcp.TrustBlocked {
		return mcp.ServerConfig{}, fmt.Errorf("%w: %q trust approval required", errMCPServerBlocked, name)
	}
	switch runtimeKind(server) {
	case RuntimeDocker:
		return dockerRuntimeConfig(server)
	case RuntimeDockerGateway:
		return dockerGatewayRuntimeConfig(name, server)
	default:
		...
	}
}
```

**Pattern to replicate (D-08, Req 3):** add the in-box guard to the `RuntimeDocker`/`RuntimeDockerGateway` cases — gate on `os.Getenv("AURA_IN_CONTAINER") == "1"` (the marker baked in the Dockerfile, Pitfall #5) and return the literal acceptance error BEFORE `dockerRuntimeConfig`/`dockerGatewayRuntimeConfig` build a `docker run` line:
```
"docker runtime unavailable inside the container — deploy as a compose sibling and mount via URL"
```
Follow the existing sentinel-error idiom (`%w: …` with a package-level `var err… = errors.New(...)`, like `errMCPServerBlocked` at `:19`). The error must be a clear constant string (V5 ASVS — non-injectable). Note `os` is NOT yet imported in `runtime.go` — add it (catalog.go in the same package already imports `os`).

---

### `internal/mcp/manager/catalog.go` — whatsapp recipe → sibling (service, CRUD) — MODIFY

**Analog:** the `memory` streamable-HTTP recipe (same file, `catalog.go:130-146`) + the `memoryRecipeURL()` helper (`catalog.go:19-25`).

**The recipe to REWRITE (current `wsl.exe` jail, `catalog.go:113-129`):**
```go
{
	Name:       "whatsapp",
	Summary:    "chetto1983/whatsapp-mcp (whatsmeow bridge in WSL, stdio via wsl.exe)",
	...
	Server: mcp.ManagedServer{
		Command: "wsl.exe",                          // REMOVE — impossible in a Linux container
		Args: []string{"-e", "bash", "-lc", "cd ~/whatsapp-mcp/... && uv run main.py"},
		...
	},
},
```
**The HTTP recipe shape to MIRROR (`memory`, `catalog.go:130-146`):**
```go
{
	Name:       "memory",
	Summary:    "neo4j-labs agent-memory (...) over streamable-HTTP",
	Source:     "recipe:memory",
	TrustClass: mcp.TrustTrustedRecipe,
	Runtime:    "local",
	Server: mcp.ManagedServer{
		Type:   mcp.ServerTypeStreamableHTTP,
		URL:    memoryRecipeURL(),
		Source: "recipe:memory",
		Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
	},
},
```
**The port-validated URL helper to MIRROR (`memoryRecipeURL`, `catalog.go:19-25`):**
```go
func memoryRecipeURL() string {
	port := strings.TrimSpace(os.Getenv("AURA_AGENT_MEMORY_MCP_PORT"))
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		port = "8091"
	}
	return fmt.Sprintf("http://127.0.0.1:%s/mcp/", port)
}
```

**Pattern to replicate (D-07, Req 4):** rewrite the whatsapp `Server` to `Type: mcp.ServerTypeStreamableHTTP` + `URL:` (no `Command`/`Args`); add a `whatsappRecipeURL()` helper cloning `memoryRecipeURL()` (validate a `AURA_WHATSAPP_MCP_PORT` 1-65535, fallback to the sibling's loopback port); update the `Summary` (drop the "in WSL, stdio via wsl.exe" string). Fail-soft when the sibling is down is automatic — the streamable-HTTP path in `runtime.go` (`RunnableManagedServers`, `:59-65`) admits an HTTP recipe with a non-empty URL without dialing it at boot, and `main.go` mount is WARN-and-drop (`main.go:207-224`). **PLANNER-CHOICE FLAG (D-07/A4):** the concrete whatsmeow bridge image is unresolved — surface for a quick user confirm.

---

### `internal/config/config.go` — D-22 keyless boot (config, transform) — MODIFY

**Analog (mirror exactly):** the empty-`SEARXNG_URL` fail-soft already in this file — the field comment (`config.go:58-69`), the empty-default load (`config.go:268-270`), and the `LoadDB` LLM-skip seam (`config.go:200-207`).

**The fail-soft field + load to MIRROR (`config.go:63, 268-270`):**
```go
	// ... an empty value is NOT boot-fatal — it is
	// surfaced as web_search_unavailable{searxng_not_configured} at call time
	// ... so `aura db migrate` and every non-web subcommand keep working.
	SearxngURL string // SEARXNG_URL — local SearXNG /search endpoint; empty default, fail-closed at call time
	...
	// Phase 7 web knobs. SEARXNG_URL has an empty default on purpose (D-05):
	// missing is fail-closed at call time, never a boot error.
	SearxngURL: os.Getenv("SEARXNG_URL"),
```
**The LLM-skip seam to MIRROR (`config.go:200-207`):**
```go
// LoadDB loads the non-LLM configuration only. DB-admin commands ... must NOT
// require an LLM API key ... LLM is left zero-value; DB commands never read it.
func LoadDB() *Config {
	return loadBase()
}
```
**The current fail-fast to RELAX on the serve path (`config.go:169-179`):**
```go
func Load() (*Config, error) {
	cfg := loadBase()
	llmCfg, err := llm.Load()   // returns ErrMissingAPIKey on empty key (D-22)
	if err != nil {
		return nil, fmt.Errorf("config: load llm: %w", err)
	}
	cfg.LLM = *llmCfg
	return cfg, nil
}
```

**Pattern to replicate (D-10, Req 9):** add a `LoadServe()` (or a `Load` option) that loads the LLM config but tolerates an empty key — return a Config with empty `LLM.APIKey` instead of erroring — exactly as `LoadDB` skips the key and `SearxngURL` tolerates empty. `Load()` (the interactive `chat` path) and `LoadDB()` stay byte-identical (preserve the friendly fail-fast UX). The call-time guard at the agent-run path emits a structured `{"error":"llm_not_configured","hint":…}` — the surface analog is `web_search_unavailable{searxng_not_configured}`. **Test analog:** mirror `TestSearxngURL*` in `config_test.go:445-455, 482, 505` (assert serve-path load returns empty APIKey without error; `Load()`/`LoadDB()` behavior unchanged).

---

### `internal/llm/config.go` — defer `ErrMissingAPIKey` seam (config, transform) — MODIFY

**Analog:** the `ErrMissingAPIKey` sentinel (`llm/config.go:79-82`) + the `Load` tail that returns it (`llm/config.go:208-211`).

**The seam to add a deferral to (`llm/config.go:208-211`):**
```go
	if cfg.APIKey == "" {
		return nil, ErrMissingAPIKey   // the D-22 fail-fast — defer this on the serve path
	}
	return cfg, nil
```

**Pattern to replicate (D-10):** add a load variant (e.g. `LoadAllowEmptyKey()` or a `Load(opts)` flag) that returns the fully-resolved `*Config` with an empty `APIKey` rather than `ErrMissingAPIKey`. Keep `ErrMissingAPIKey` exported and the default `Load()` behavior unchanged (the sentinel is asserted by existing tests via `errors.Is`). The `config.LoadServe()` from the previous entry calls this variant.

---

### `cmd/aura/doctor.go` — aura doctor aggregate (command, request-response) — CREATE

**Analogs:** `cmd/aura/web.go` (`runWebDoctor`, `web.go:42-65` — config.LoadDB + per-check + distinct exit codes) and `cmd/aura/mcp_status.go` (`mcpDoctorAll`, `mcp_status.go:53-77` — iterate checks, one line each, seamed probes via `mcpLookPath`).

**The single-check + exit-code shape to MIRROR (`runWebDoctor`, `web.go:42-65`):**
```go
func runWebDoctor() {
	cfg := config.LoadDB()                       // LLM-free load — doctor needs no key
	if strings.TrimSpace(cfg.SearxngURL) == "" {
		fmt.Fprintln(os.Stderr, "aura web doctor: SEARXNG_URL is not configured — ...")
		os.Exit(exitUsage)                       // 64
	}
	...
	results, err := client.Search(ctx, web.SearchParams{Query: "...probe", MaxResults: 1})
	if err != nil {
		fmt.Fprintf(os.Stderr, "aura web doctor: SearXNG probe failed: %s\n", err.Error())
		os.Exit(exitUnreachable)                 // 70
	}
	fmt.Println("status: OK")
	os.Exit(0)
}
```
**The per-check iteration + seamed LookPath to MIRROR (`mcp_status.go:53-77, 100-103`):**
```go
func mcpDoctorAll(ctx context.Context, out io.Writer) error {
	...
	for _, status := range mcpmanager.SnapshotStatus(doc) {
		...
		if err := writeRuntimeCheck(out, status.Name, server); err != nil { return err }
		...
	}
}
// writeRuntimeCheck: if _, err := mcpLookPath(server.Command); err != nil { ... }
```
**The exit codes to REUSE (`exit_codes.go:5-9`):**
```go
const (
	exitUnreachable = 70 // a required service is unreachable
	exitInfra       = 71 // other infrastructure fault
	exitUsage       = 64 // EX_USAGE — bad arguments / invocation
)
```

**Pattern to replicate (D-09, Req 10):** one `runDoctor(args)` printing one pass/fail line per check, non-zero exit on any hard failure, using `config.LoadDB()` (no LLM key). Checks: (1) Postgres ping; (2) Neo4j round-trip via `knowledge.Open` + `RETURN 1`; (3) embed dimension match (`AURA_EMBED_DIMENSIONS` vs the `/v1/embeddings` sidecar); (4) `mcp-neo4j-cypher` spawn; (5) LLM-key configured-or-not. **MUST NOT use `docker compose ps`** (no socket in-box, D-09) — direct probes only. Inject fakes via seams (mirror `mcpLookPath = exec.LookPath` at `mcp_status.go:15`) so each check is unit-testable; the live leg is a `db_integration neo4j_integration`-tagged test (no-skip-as-green). Keep the file ≤600 LOC (CLAUDE.md).

---

### `cmd/aura/main.go` — wire `case "doctor"` (command, dispatch) — MODIFY

**Analog:** the `switch os.Args[1]` table (`main.go:43-92`) + `usage()` (`main.go:94-96`).

**The dispatch table to extend (`main.go:54-55`):**
```go
	case "web":
		runWeb(os.Args[2:])
	case "db":
		runDB(os.Args[2:])
```
**Pattern to replicate:** add `case "doctor": runDoctor(os.Args[2:])` and append `doctor` to the `usage()` string (`main.go:95`). `_ = godotenv.Load()` already runs at `main.go:38` so doctor sees `.env`. One-line addition, exactly mirroring the `web`/`db`/`neo4j` cases.

---

### `scripts/install.sh` — curl|sh installer (config, batch/file-I/O) — CREATE

**Analogs:** `scripts/restore_drill.sh` (shell guards, docker detection, env-with-fail-fast) and `scripts/coverage_gate.sh` (`set -euo pipefail`, `git rev-parse` root, fail-loud).

**The shell-guard header to MIRROR (`restore_drill.sh:18-38`):**
```bash
set -euo pipefail
export MSYS_NO_PATHCONV=1            # Git Bash path-translation guard
...
DB_PASS="${POSTGRES_PASSWORD:?POSTGRES_PASSWORD required in environment}"   # fail-fast env
if [[ -z "${DOCKER:-}" ]]; then
    if command -v docker.exe >/dev/null 2>&1; then DOCKER=docker.exe; else DOCKER=docker; fi
fi
```
**The fail-loud + parse-or-die idiom to MIRROR (`coverage_gate.sh:19-21, 59-62`):**
```bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
...
if [[ -z "${PCT}" ]]; then
  echo "FAIL: could not parse ..." >&2
  exit 1
fi
```

**Pattern to replicate (D-12, Req 12/13):** `set -euo pipefail`; HW preflight (RAM/disk/CPU — warn below comfortable, abort below a hard floor, non-zero exit, no hang — the mini-PC target is 16-core/32GB, 16 GB min); Docker check + `command -v` detection (mirror restore_drill's docker/docker.exe probe) + best-effort auto-install (`get.docker.com` / `brew --cask docker`) + guided fallback + non-zero exit; **idempotent secret-gen** — guard `openssl rand` on `[ ! -f .env ]` (Pitfall #9 — re-run leaves `.env` byte-identical), `chmod 600 .env`; `docker compose up`; print wizard URL + access token + next steps. **PLANNER-CHOICE FLAG (D-12):** exact HW warn/abort floors. Windows = a documented Docker-Desktop + PowerShell secret-gen door in the README (no bespoke installer).

---

### `scripts/aura.service` — systemd autostart (config, event-driven boot) — CREATE

**Analog:** `deploy/aura-scheduler.service` — an in-repo systemd unit, exact template.

**The unit shape to MIRROR (`deploy/aura-scheduler.service:22-51`):**
```ini
[Unit]
Description=Aura scheduler daemon (cron + agent_job queue)
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/aura serve
KillSignal=SIGTERM
TimeoutStopSec=120
Restart=on-failure
RestartSec=5
EnvironmentFile=%h/.aura/env

[Install]
WantedBy=default.target
```

**Pattern to replicate (D-13, Req 15):** for the appliance, `ExecStart=/usr/bin/docker compose -f /opt/aura/compose.yaml up -d` (compose-up at boot, not `aura serve`); `After=network-online.target docker.service` + `Wants=network-online.target` (already correct in the analog); `WantedBy=multi-user.target` (system-wide appliance, vs the analog's per-user `default.target`). Keep the install-recipe header-comment convention (`deploy/aura-scheduler.service:9-20`). `restart: unless-stopped` in compose already covers crashes (Req 8); systemd covers reboots. **Decide CREATE location:** the analog lives in `deploy/`; the RESEARCH file-map names `scripts/aura.service` — planner picks (a `deploy/aura.service` placement is the more consistent choice with the existing analog).

---

### `caddy/Caddyfile` + Caddy compose service (config, TLS front) — CREATE

**Analog:** the loopback sibling services in `compose.yaml` (`searxng` `:211-229`, `aura-agent-memory-mcp` `:143-206`) for the compose-service shape. **No in-repo Caddy/reverse-proxy analog exists** — the Caddyfile content has no codebase template (use the RESEARCH/Caddy-docs `tls internal` directive).

**The loopback-vs-LAN split already established (every sidecar binds `127.0.0.1`, e.g. `compose.yaml:62, 90-93, 128, 200, 216, 260, 275, 309, 332`):**
```yaml
    ports:
      - "127.0.0.1:${POSTGRES_PORT:-5432}:5432"   # loopback-only — the established pattern
```

**Pattern to replicate (D-11, Req 11):** add a `caddy` compose service modeled on the sibling blocks (official `caddy:2` image, `restart: unless-stopped`, a healthcheck) — but it is the **only** service that publishes on a non-loopback interface (`0.0.0.0:443` / the LAN). Caddy `tls internal` issues a local-CA cert; it fronts ONLY the wizard (`:9081`) + AG-UI (`:9080`) and enforces a generated shared token. Every data/sidecar port stays `127.0.0.1` (Pitfall #10). **PLANNER-CHOICE FLAG (D-11/A1):** token enforcement mechanism (`forward_auth` vs a header/`@token` matcher) — verify with the 401-without-token live probe.

---

### `.goreleaser.yaml` — buildx multi-arch + ghcr push (config, build/pub) — MODIFY

**Analog:** the existing `builds`/`archives`/`release` blocks in the same file (`.goreleaser.yaml:12-69`).

**The blocks to KEEP + extend (`.goreleaser.yaml:12-26, 64-69`):**
```yaml
builds:
  - id: aura
    main: ./cmd/aura
    binary: aura
    env:
      - CGO_ENABLED=0
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
...
release:
  github:
    owner: chetto1983
    name: Aura
```

**Pattern to replicate (D-13, Req 14):** keep the host-binary `builds`/`archives` (dev retains them) and ADD a multi-arch docker image block (`dockers_v2`/buildx) pushing `ghcr.io/chetto1983/aura:<tag>` pinned per release tag, **never `latest`** (Constraint). The `owner: chetto1983` is already the org. **PLANNER-CHOICE FLAG (A3):** the exact goreleaser v2 multi-arch directive — verify with `goreleaser build --snapshot --clean` (the header comment at `.goreleaser.yaml:2` already names the local-validate command). `compose.yaml:13` already parameterizes `${AURA_IMAGE}` — the appliance pins a real ghcr tag; dev keeps `aura:local` + `pull_policy: never`.

---

### `Dockerfile` (root, distroless) — REMOVE / REPOINT — MODIFY

**Analog:** n/a (removal). The current root `Dockerfile:12-15` (`gcr.io/distroless/static-debian12:nonroot` + `USER 65532`) is the audit jail D-01 reverts.

**Pattern to replicate:** remove the distroless root `Dockerfile` (or repoint it to a thin shim that builds `docker/aura/Dockerfile`). `compose.yaml:11-12` currently `build: { context: . }` against this root file — repoint it to `dockerfile: docker/aura/Dockerfile` (or move the build to `context: ./docker/aura`). Salvage the build stage (`Dockerfile:1-10`) into `docker/aura/Dockerfile` (see the first entry).

---

### `internal/cron/handlers/backup.go` — host-visible AURA_BACKUP_DIR (service, batch/file-I/O) — MODIFY

**Analog:** `BackupHandler.Run` + `dumpArgv` (same file, `backup.go:75-119, 150-155`) — but this is a **partial** match because of the `docker exec` paradox (see Shared Patterns → Backup landmine).

**The current docker-exec dump (`backup.go:102-105, 150-155`):**
```go
	cmd := exec.CommandContext(runCtx, docker, args...) //nolint:gosec
	out, runErr := cmd.CombinedOutput()
...
func (h BackupHandler) dumpArgv(dest string) []string {
	if h.Variant == BackupNeo4j {
		return []string{"exec", neo4jContainer, "neo4j-admin", "database", "dump", "neo4j", "--to-path", dest}
	}
	return []string{"exec", pgContainer, "pg_dump", "-U", "aura_migrate", "-Fc", "-f", dest, "aura"}
}
```
**The AURA_BACKUP_DIR resolution already present (`backup.go:180-193`):**
```go
func backupDir() (string, error) {
	dir := strings.TrimSpace(os.Getenv("AURA_BACKUP_DIR"))
	if dir == "" {
		dir = filepath.Join("~", ".aura", "backups")
	}
	...
}
```

**Pattern to replicate (D-14, Req 16):** wire the scheduled backups (the `backup_postgres`/`backup_neo4j` handlers) to a host-visible `AURA_BACKUP_DIR` by default. **BUT — RESEARCH conflict (Landmine #1, HIGHEST PRIORITY):** `BackupHandler.Run` (`backup.go:102`) shells out to `docker exec` (and `resolveDocker` LookPaths `docker`, `backup.go:133-142`) — the socket-less `aura` box (Req 1/3) **cannot do this**. The in-box-safe alternative the RESEARCH recommends (option b) is a **network `pg_dump`** from inside the `aura` container over the DSN (no socket) + a sidecar/network dump for Neo4j. The host-side template for the network approach already exists in `scripts/restore_drill.sh` (which uses `docker compose exec` from the *host*, restore_drill.sh:41). **This is an explicit plan decision — surface it.** (The `backup.go` comment at `:43-53` itself notes the design was written for a *host-run* Aura with socket access.)

---

## Shared Patterns

### Compose sibling over streamable-HTTP (applies to: whatsapp sibling, Caddy split, aura-migrate gating)
**Source:** `compose.yaml:143-206` (`aura-agent-memory-mcp`)
**Apply to:** the whatsapp bridge sibling, any container-needing MCP, the migrate one-shot.
The canonical template: build-context-or-image + `pull_policy: never` (dev) + `container_name:` + `restart: unless-stopped` + `depends_on: { <dep>: { condition: service_healthy } }` + secrets via `${VAR:?...}` env + loopback `127.0.0.1:<port>:<int>` publish + a `healthcheck`. `RuntimeServers`/`RunnableManagedServers` already exclude streamable-HTTP servers from subprocess launch (`runtime.go:30-33, 59-65`).

### Fail-soft degrade for a downed dependency (applies to: whatsapp sibling, keyless boot, SEARXNG)
**Source:** `internal/mcp/manager/runtime.go:49-78` (`RunnableManagedServers` skips disabled/blocked) + `cmd/aura/main.go:207-224` (mount WARN-and-drop) + `config.go:268-270` (empty-`SEARXNG_URL`).
**Apply to:** every optional sibling/key. An unreachable HTTP recipe URL must not crash the boot; a missing optional config surfaces at call time, not boot. The whatsapp `down → fail-soft` (Req 4) and the keyless-boot `no key → llm_not_configured at call time` (Req 9) are the same shape.

### Empty-config fail-soft at call time (applies to: D-10 keyless boot)
**Source:** `internal/config/config.go:58-69, 268-270` + `internal/web` (`web_search_unavailable{searxng_not_configured}`) + `config_test.go:445-455`.
**Apply to:** the LLM key on the serve path. Boot with empty → structured `{"error":"llm_not_configured",...}` at the agent call. The SEARXNG test is the literal test template.

### Per-check + sysexits exit-code CLI (applies to: aura doctor)
**Source:** `cmd/aura/web.go:42-65` + `cmd/aura/mcp_status.go:53-77` + `cmd/aura/exit_codes.go:5-9`.
**Apply to:** `aura doctor`. `config.LoadDB()` (no LLM key), one human-readable line per check, `os.Exit(70/71/64/0)`, seamed probes (`mcpLookPath = exec.LookPath` idiom) for unit-testability.

### Pinned-version supply-chain discipline (applies to: docker/aura/Dockerfile, .goreleaser ghcr, recipe pre-bake)
**Source:** `docker/markitdown/Dockerfile:10-17` (exact `==` pins) + `docker/agent-memory/Dockerfile:25, 44` (`-c constraints.txt`, fork-pinned commit) + CLAUDE.md supply-chain rule.
**Apply to:** every base image + pip/npm/uv artifact. `mcp-neo4j-cypher==0.6.0` exact; ghcr image pinned-per-tag, never `latest`; recipes are own forks (`chetto1983/calculator-mcp-server`, `martinzarfl/mail-mcp` — `catalog.go:54, 99`). **NO secret baked** (`docker history` clean — Pitfall #4).

### systemd unit + install-recipe header (applies to: scripts/aura.service)
**Source:** `deploy/aura-scheduler.service` (entire file).
**Apply to:** the appliance autostart unit — same `[Unit]/[Service]/[Install]` skeleton, `After=docker.service`, `KillSignal=SIGTERM`/`TimeoutStopSec`, header-comment install recipe.

### Shell-script guards (applies to: scripts/install.sh)
**Source:** `scripts/restore_drill.sh:18-38` + `scripts/coverage_gate.sh:19-21`.
**Apply to:** the installer — `set -euo pipefail`, `${VAR:?msg}` fail-fast, `command -v` docker detection, fail-loud-and-exit-non-zero (never hang), idempotent `[ ! -f .env ]` guard.

---

## No Analog Found

No file in this phase lacks an in-repo analog. The two artifacts with the weakest match (still role-matched) are:

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `caddy/Caddyfile` (the Caddyfile *content*) | config (reverse proxy) | request-response | No Caddy/reverse-proxy exists in-repo. The *compose service* has a strong analog (the loopback siblings); the Caddyfile *directives* (`tls internal` + token) have no codebase template — use RESEARCH/Caddy docs (A1). The compose service and loopback-split are NOT new patterns. |
| `compose.gvisor.yaml` (the `runtime: runsc` field) | config (override) | request-response | The compose-override-per-tier *mechanism* is fully templated (spikes 005-008); the `runtime: runsc` field itself is new (gVisor not previously in compose). The pattern (minimal override + usage-comment header) holds. |

---

## Metadata

**Analog search scope:** `docker/`, `compose.yaml` + spike compose overrides (`.planning/spikes/00[5-8]`), `internal/mcp/manager/`, `internal/config/`, `internal/llm/`, `internal/cron/handlers/`, `internal/knowledge/`, `cmd/aura/`, `scripts/`, `deploy/`, `.goreleaser.yaml`, root `Dockerfile`.
**Files scanned (read in full or targeted):** root `Dockerfile`, `docker/agent-memory/Dockerfile`, `docker/markitdown/Dockerfile`, `compose.yaml`, `compose.token.yaml` (spike 008), `internal/mcp/manager/runtime.go`, `internal/mcp/manager/catalog.go`, `internal/config/config.go`, `internal/llm/config.go`, `cmd/aura/web.go`, `cmd/aura/mcp_status.go`, `cmd/aura/exit_codes.go`, `cmd/aura/main.go`, `cmd/aura/db.go`, `internal/cron/handlers/backup.go`, `internal/knowledge/client.go` (touch-point confirm), `.goreleaser.yaml`, `scripts/restore_drill.sh`, `scripts/coverage_gate.sh`, `deploy/aura-scheduler.service`, `config_test.go` (SEARXNG test analog).
**Pattern extraction date:** 2026-06-14

---

## PATTERN MAPPING COMPLETE

**Phase:** 17 - packaging-distribution
**Files classified:** 14 (8 CREATE + 6 MODIFY; `internal/knowledge/client.go` confirmed UNCHANGED, touch-point only)
**Analogs found:** 14 / 14

### Coverage
- Files with exact analog: 10 (`docker/aura/Dockerfile`, `compose.yaml` aura+siblings, `runtime.go`, `catalog.go`, `config.go`, `llm/config.go`, `doctor.go`, `main.go`, `aura.service`, `.goreleaser.yaml`)
- Files with role-match analog: 4 (`compose.gvisor.yaml`, `scripts/install.sh`, `caddy/Caddyfile`+service, `backup.go` — partial: `docker exec` paradox)
- Files with no analog: 0

### Key Patterns Identified
- **The `aura-agent-memory-mcp` sibling block (`compose.yaml:143-206`) is the master compose template** — replicate it for the whatsapp sibling, the `aura-migrate` one-shot (swap `service_healthy` → `service_completed_successfully`), and the Caddy loopback-vs-LAN split.
- **The pinned-pip slim sidecar (`docker/markitdown` + `docker/agent-memory`) is the runtime-stage template** for `docker/aura/Dockerfile` — metadata-COPY-first cache-stability + exact `==` version pins + `COPY aura` LAST.
- **Two established fail-soft seams cover three requirements:** the empty-`SEARXNG_URL` pattern (`config.go:268-270` + `config_test.go:445`) is the literal template for D-10 keyless boot, and the MCP WARN-and-drop mount (`main.go:207-224`) gives the whatsapp-down fail-soft for free.
- **`runWebDoctor`/`mcpDoctorAll` + `exit_codes.go` are the exact `aura doctor` template** — per-check line + sysexits exit codes + seamed probes (`config.LoadDB`, no LLM key, no `docker compose ps`).
- **`deploy/aura-scheduler.service` is a ready systemd template; `restore_drill.sh`/`coverage_gate.sh` are the shell-guard templates** for `aura.service` and `install.sh`.
- **One genuine cross-requirement conflict surfaced (NOT a pattern gap):** `backup.go`'s `docker exec` (`backup.go:102, 150-155`) cannot run in the socket-less box — the planner must choose the in-box-safe network-`pg_dump` alternative (Req 16, Landmine #1). Flagged in the `backup.go` assignment + Shared Patterns.

### File Created
`.planning/phases/17-packaging-distribution/17-PATTERNS.md`

### Ready for Planning
Pattern mapping complete. The planner can reference each analog (file:line) directly in the PLAN.md action sections — every new/modified file replicates an established in-repo shape. Three planner-choice flags (whatsapp bridge image, Caddy token mechanism, HW thresholds) and one BLOCKING design decision (the Req 16 backup execution model) are surfaced in the relevant assignments for plan-review.
