# Aura — Production Runbook

> This document covers the full operational lifecycle for a production Aura
> deployment: cold install, upgrade, rollback, restore, key rotation, and
> emergency repair. Every section contains copy-pasteable shell commands.
>
> See also: `docs/container.md` for the full container-stack reference.
> See `docs/audit-security-2026-05-17.md` for the threat model that informs
> the hardening choices in `compose.prod.yaml`.

---

## 1. Production Overlay

Aura ships with a `compose.prod.yaml` overlay that tightens dev defaults for
production deployment. Activate it with:

```sh
docker compose -f compose.yaml -f compose.prod.yaml up -d
```

Key differences from the dev `compose.yaml`:

- **Dashboard port**: bound to `127.0.0.1:8080` only (not the LAN-accessible
  `0.0.0.0:18080`). Reach the dashboard via SSH tunnel:
  ```sh
  ssh -L 8080:localhost:8080 <server>
  ```
  Then open `http://localhost:8080` locally.
- **Restart policy**: all long-running services enforce `restart: unless-stopped`.
- **Healthchecks**: 60 s interval checks on all sidecars (searxng, garage,
  qdrant, aura-llama-embed, aura-markitdown).

The overlay is additive — it never modifies `compose.yaml`, so dev and prod
share a single source of truth.

---

## 2. Cold Install

Starting from a bare Linux box with Docker + Docker Compose installed.

**Prerequisites:**

```sh
# Verify Docker is available
docker --version    # Docker 24+ required
docker compose version
git --version
```

**Step 1 — Clone the repo:**

```sh
git clone https://github.com/chetto1983/aura.git
cd aura
```

Or for a specific release tag:

```sh
git clone --branch v1.2.3 https://github.com/chetto1983/aura.git
cd aura
```

**Step 2 — Bootstrap local secrets (run once on a fresh checkout):**

```sh
docker compose run --rm aura-secrets
```

This generates `data/secrets/` (Garage S3 keys, SearXNG secret, Garage TOML)
and sets ownership of `data/` and `runtime-workspace/` to uid 10001. Skipping
this step leaves `data/` root-owned and Aura crashes on first start with
"permission denied" writing `aura.db`.

**Step 3 — Download the embedding model (run once; 265 MB):**

```sh
docker compose run --rm aura-init-models
```

This fetches `embeddinggemma-300m-Q4_0.gguf` into `data/` and SHA-256-verifies
it. Cache-hit on a repeat run exits in ~1 s. The `aura-llama-embed` sidecar
blocks until this exits 0 (Compose dependency).

**Step 4 — Start the stack:**

```sh
# Production:
docker compose -f compose.yaml -f compose.prod.yaml up -d

# Dev / local (exposes dashboard on LAN 0.0.0.0:18080):
docker compose up -d
```

Compose starts services in dependency order: `aura-secrets` → `aura-init-models`
→ `garage` + `garage-init` → `searxng` + `qdrant` + `aura-llama-embed` +
`aura-markitdown` → `aura`.

**Step 5 — First-run setup wizard:**

On the first boot the Telegram token is blank. Aura opens a loopback-only
setup form and blocks until you complete it.

- **Production** (tunnel): `ssh -L 8080:localhost:8080 <server>` → open
  `http://localhost:8080`
- **Dev / local**: open `http://127.0.0.1:18080`

Fill in:
- Telegram bot token (from @BotFather — see §6 for how to create one)
- LLM provider base URL + API key (e.g. OpenAI-compatible endpoint)

These are stored in `data/aura.db` (SQLite, AES-encrypted at rest in the
`secrets` table). Aura restarts automatically after submission.

**Step 6 — Verify:**

```sh
# All services healthy
docker compose ps

# Aura health endpoint
curl -s http://127.0.0.1:8080/health | jq .

# Check version baked in at build time
docker compose exec aura aura --version
```

Expected: `{"status":"ok", ...}` with version, commit, build_date populated.

---

## 3. Upgrade

Zero-downtime swap: Docker Compose starts the new container before stopping the
old one when you use `up -d`.

**From a tagged release (production pattern):**

```sh
cd /path/to/aura
git fetch --tags
git checkout v1.X.Y       # target release tag

# Rebuild only the images that come from the local tree
docker compose build aura aura-init-models aura-markitdown

# Pull any updated third-party sidecar images
docker compose pull garage qdrant searxng

# Replace running containers (graceful rolling replace)
docker compose -f compose.yaml -f compose.prod.yaml up -d
```

**From master (dev cadence):**

```sh
git pull origin master

# Pass SHA + date so --version and /api/health show the right commit
GIT_COMMIT=$(git rev-parse --short HEAD)
BUILD_DATE=$(date -u +%FT%TZ)
docker compose build \
  --build-arg GIT_COMMIT="$GIT_COMMIT" \
  --build-arg BUILD_DATE="$BUILD_DATE" \
  aura aura-markitdown aura-init-models

docker compose -f compose.yaml -f compose.prod.yaml up -d
```

**Verify the swap:**

```sh
# Confirm new SHA is live
curl -s http://127.0.0.1:8080/health | jq .commit

# Check logs for clean startup
docker compose logs -f aura --tail=50
```

Expected log line: `"msg":"aura starting" "version":"..." "commit":"<new SHA>"`.

**Migrations run automatically** on startup (forward-only, idempotent). No
manual `migrate` step is needed.

---

## 4. Rollback

Rollback time is ~60–90 s (image already cached locally).

```sh
# 1. Identify the last known-good tag
git log --oneline --tags --simplify-by-decoration | head -10

# 2. Check out that tag
git checkout v1.X.Y-1     # the previous tag

# 3. Rebuild only the aura binary (sidecars rarely need rebuild)
docker compose build aura

# 4. Redeploy
docker compose -f compose.yaml -f compose.prod.yaml up -d

# 5. Confirm rollback
curl -s http://127.0.0.1:8080/health | jq '{commit, version}'
```

**Database note**: migrations are forward-only. If the new version ran a
migration before rollback, the schema is at the newer version. This is safe
for rollback because migrations only add tables/columns — the rolled-back code
ignores extra columns it doesn't know about. No schema rollback step is needed
in practice.

---

## 5. SQLite Restore Drill

Run this drill quarterly to verify your backup is actually restorable.
See also: `docs/audit-security-2026-05-17.md` §Recovery for threat context.

**Trigger a manual backup first (if not already done today):**

```sh
# From the repo directory on the server
go run ./cmd/debug_backup
```

This writes to Garage S3 (`aura-artifacts` bucket):
- `backups/<timestamp>/aura-backup.tar.gz` — full restore point
- `artifacts/<timestamp>/` — categorized artifact archives + manifest

Verify upload succeeded (non-zero objects):

```sh
docker compose exec aura aura --version   # confirms container is healthy
curl -s http://127.0.0.1:3900/            # Garage S3 endpoint alive
```

**Download and restore (drill procedure):**

```sh
# 1. Stop aura to release the SQLite write lock
docker compose stop aura

# 2. Back up the current live database before overwriting
cp data/aura.db data/aura.db.pre-restore

# 3. Download the backup tarball from Garage S3
# (Use the Garage web UI at http://127.0.0.1:3909 or the aws CLI)
# Example with aws CLI (Garage is S3-compatible):
aws --endpoint-url http://127.0.0.1:3900 \
    s3 cp s3://aura-artifacts/backups/<timestamp>/aura-backup.tar.gz /tmp/

# 4. Extract
mkdir -p /tmp/aura-restore
tar -xzf /tmp/aura-backup.tar.gz -C /tmp/aura-restore

# 5. Run the backup verifier against the extracted DB
# (built into Aura via internal/dbrecovery/verify.go)
sqlite3 /tmp/aura-restore/aura.db "PRAGMA integrity_check;"
sqlite3 /tmp/aura-restore/aura.db "SELECT COUNT(*) FROM conversations;"
sqlite3 /tmp/aura-restore/aura.db "SELECT COUNT(*) FROM scheduled_tasks;"

# 6. Replace the live DB (while aura is stopped)
cp /tmp/aura-restore/aura.db data/aura.db

# 7. Restore wiki and skills if needed
# rsync -a /tmp/aura-restore/wiki/  <path-to-aura-wiki-volume>/
# (wiki is in Docker volume aura-wiki — mount it or copy via running container)

# 8. Restart aura
docker compose -f compose.yaml -f compose.prod.yaml up -d aura

# 9. Confirm health
curl -s http://127.0.0.1:8080/health | jq .
```

**Automated daily verification**: `internal/dbrecovery/verify.go`
(`VerifyBackup`) runs every day at 04:00 local time via the cron scheduler.
Failures emit a Telegram alert to the operator. Check the cron log if you
suspect silent failures:

```sh
docker compose logs aura | grep -i "backup\|verify\|integrity"
```

---

## 6. Telegram Token Rotation

Use this when you revoke or regenerate a bot token via @BotFather.

**Step 1 — Generate a new token via @BotFather:**

Open Telegram and send these messages to `@BotFather`:

```
/mybots
→ select your bot (e.g. Aura_bot)
→ API Token
→ Revoke current token
→ Confirm revocation → copy the NEW token
```

**Step 2 — Update the token in Aura's dashboard:**

1. Open the dashboard: `http://localhost:8080` (via SSH tunnel if prod)
2. Go to **Settings** → find the **Telegram Token** field (under Authentication)
3. Paste the new token and save

Aura stores the new value in the `secrets` SQLite table under key
`TELEGRAM_TOKEN`. The old token is immediately invalid.

**Alternatively, update via SQLite directly (while aura is stopped):**

```sh
docker compose stop aura
sqlite3 data/aura.db \
  "INSERT OR REPLACE INTO secrets (key, value, created_at) VALUES ('TELEGRAM_TOKEN', '<new-token>', datetime('now'));"
docker compose -f compose.yaml -f compose.prod.yaml up -d aura
```

**Step 3 — Verify:**

```sh
docker compose logs aura --tail=30 | grep "telegram\|bot\|start"
```

Expected: Aura connects to the Telegram API and logs the bot username. If the
token is wrong you will see: `unauthorized` in the startup error.

---

## 7. Mistral API Key Rotation

Mistral OCR key (used for PDF ingestion) is separate from the LLM API key.

**Step 1 — Generate a new key in the Mistral console:**

Log into `console.mistral.ai` → API Keys → Create new key → copy it.
Immediately revoke the old key in the same UI.

**Step 2 — Update in the dashboard:**

1. Open `http://localhost:8080` → **Settings** → **OCR** group
2. Update **Mistral OCR API key** field → save

**Or via SQLite (while aura is stopped):**

```sh
docker compose stop aura
sqlite3 data/aura.db \
  "INSERT OR REPLACE INTO secrets (key, value, created_at) VALUES ('MISTRAL_API_KEY', '<new-key>', datetime('now'));"
docker compose -f compose.yaml -f compose.prod.yaml up -d aura
```

**For LLM API key rotation** (provider base URL + API key), same procedure but
use the **Provider** group in Settings and key `LLM_API_KEY`.

**Step 3 — Verify OCR is working:**

Send a PDF to Aura via Telegram. If OCR fails you will see `401 Unauthorized`
in Aura's logs:

```sh
docker compose logs aura | grep -i "mistral\|ocr\|401"
```

---

## 8. llama-embed Rebuild

The local embedding server (`aura-llama-embed`) uses
`embeddinggemma-300m-Q4_0.gguf` stored in `data/`. Use this procedure if the
model file is corrupted, deleted, or you are migrating to a new quantization.

```sh
# 1. Stop only the embedding sidecar and aura (aura depends on it)
docker compose stop aura aura-llama-embed

# 2. Delete the stale or corrupt model file
rm -f data/embeddinggemma-300m-Q4_0.gguf

# 3. Re-run the init-models container (downloads + SHA-256 verifies)
docker compose run --rm aura-init-models
# Downloads ~265 MB. Cache-hit exits in ~1 s if file is already valid.

# 4. Restart
docker compose -f compose.yaml -f compose.prod.yaml up -d aura-llama-embed aura

# 5. Verify the embed server is healthy
curl -s http://127.0.0.1:8081/health   # prod: accessible inside docker network only

# 6. Rebuild the Qdrant vector index (derived, rebuildable)
go run ./cmd/debug_qdrant -url http://127.0.0.1:6333 -rebuild -timeout 5m
```

To use a different model URL or SHA256 (operators with a private mirror):

```sh
AURA_EMBEDDING_MODEL_URL=https://your-mirror.example/model.gguf \
AURA_EMBEDDING_MODEL_SHA256=<sha256-hex> \
docker compose run --rm aura-init-models
```

See Wave 2.10 notes in `docs/wave-2.10-install-bootstrap.md` for background.

---

## Appendix: Common Failure Modes

### A. Container restart loop (aura keeps restarting)

**Symptom:** `docker compose ps` shows aura as `Restarting`. Logs cycle through
startup errors every ~10 s.

**Diagnose:**

```sh
docker compose logs aura --tail=50
```

Common causes and fixes:

| Log message | Cause | Fix |
|---|---|---|
| `open database /data/aura.db: permission denied` | `data/` is root-owned (skipped `aura-secrets`) | `docker compose run --rm aura-secrets` |
| `open /data/aura.db: no such file or directory` | Volume not mounted | Check `docker compose ps`; ensure `data/` exists on host |
| `failed to create telegram bot: unauthorized` | Token blank or invalid | See §6 (Telegram Token Rotation) |
| `setup wizard: listen tcp :8080: bind: address already in use` | Port conflict | Check `ss -tlnp \| grep 8080` |
| `aura-llama-embed not ready` | Embed sidecar crash-looping | See §8; check `docker compose logs aura-llama-embed` |

### B. SQLite locked (`database is locked`)

**Symptom:** Aura logs `database is locked` or `SQLITE_BUSY`. Agent turns fail.

**Cause:** A host-side tool (e.g. `sqlite3`, `debug_memory_closure`) opened
`data/aura.db` while the container is writing to it. The compose stack uses
`AURA_SQLITE_JOURNAL_MODE=DELETE` (not WAL) to handle Docker Desktop
cross-filesystem bind mounts — this means only one writer at a time.

**Fix:**

```sh
# Stop the offending host-side process first, then:
docker compose restart aura
```

Never run host-side write commands against `data/aura.db` while the Aura
container is running. See `docs/container.md` §SQLite Safety.

### C. Qdrant collection missing (`collection aura_memory_v1 not found`)

**Symptom:** Aura logs `collection aura_memory_v1 not found` on wiki memory
searches. Vector search silently returns zero results.

**Cause:** Qdrant volume was wiped (e.g. `docker volume rm qdrant-storage`) or
the collection was never built.

**Fix:** Rebuild the vector index from the existing SQLite embedding cache:

```sh
go run ./cmd/debug_qdrant -url http://127.0.0.1:6333 -rebuild -timeout 5m
```

The command re-embeds all wiki pages and repopulates the Qdrant collection.
Elapsed time depends on wiki size; typical small wiki: ~30 s.

See also `docs/audit-code-health-deep-2026-05-17.md` for vector index
observability notes.

### D. Mistral OCR returns 429 (rate limit)

**Symptom:** PDF ingestion fails. Aura logs `Mistral OCR: 429 Too Many Requests`.

**Cause:** Mistral OCR tier rate limit hit (concurrent PDF uploads or burst).

**Fix:** Mistral OCR has a per-minute token limit on lower tiers. Options:

1. **Wait** — Aura's ingest pipeline retries with exponential backoff. Most
   429s resolve within 60 s.
2. **Reduce concurrency** — avoid uploading multiple large PDFs simultaneously.
3. **Upgrade tier** — higher Mistral tiers have larger rate-limit buckets.

Check current usage in the Mistral console: `console.mistral.ai` → Usage.

### E. searxng returns 403 on JSON queries

**Symptom:** `web_search` tool fails. Aura logs `SearXNG: 403 Forbidden`.

**Cause:** The `data/secrets/searxng/settings.yml` file is missing the
`json` entry under `search.formats`.

**Fix:**

```sh
# Regenerate the SearXNG config (idempotent — existing secrets preserved)
docker compose run --rm aura-secrets

# Restart searxng to pick up new config
docker compose restart searxng
```

Verify:

```sh
go run ./cmd/debug_searxng -base-url http://127.0.0.1:8088 -q "test" -json
```

Expected: JSON result object with at least one result.
