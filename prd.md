# PRD — Tabula-rasa, Slice 1 → 4

> Stato di partenza: commit `af4ca65c` (skeleton, 633 LOC src).
> Ordine fissato (utente): **Agent → Sandbox → Swarm → KV**.
> KV è ultimo per design: il prompt-builder ottimizza una superficie che a quel
> punto è già stabile (system + tool manifest + sandbox-tool + swarm-tool), non
> si insegue un bersaglio che si muove.
>
> Discipline non negoziabili per ogni slice (da CLAUDE.md):
> - READ-BEFORE-EDIT, NEVER-SUPPOSE, 3-STRIKE-RULE.
> - Nessun file >600 LOC; refactor on touch.
> - `go vet ./... && go build ./...` verdi PRIMA del commit.
> - Un slice = un commit (atomico, imperativo, Co-Authored-By).
> - `git push` **mai** senza richiesta esplicita nello stesso turno.
> - Niente commenti se il WHY è ovvio dal nome dell'identifier.
> - Tool grandi → `Deferred: true` (manifest pulito, schema on-demand via `tool_search`).
> - **Test discipline** (vedi §Test discipline in fondo): prompts E2E reali, mai citare tool/skill per nome dentro il prompt.

---

## Slice 0.5 — Infra DB (PostgreSQL + sqlc + pgx + golang-migrate)

**Goal.** Fondamenta dati industrial-grade. Container Postgres + connection pool
+ ORM type-safe + migrations versionate. Senza questa slice, Slice 1.5 (PausedState
persistent), Slice 6 (scheduler) e Slice 7 (skill audit) non hanno dove vivere.

**Stack scelto** (validato da search 2026, vedi PRD turn dedicato):
- DB: **PostgreSQL 17** (container `postgres:17-alpine`, ~80 MB image, ~250 MB RAM idle)
- Driver: **`jackc/pgx/v5`** — pure Go, no CGO, performance leader per Postgres in Go
- ORM: **`sqlc`** — SQL-first codegen type-safe (Uber, Pinterest, PlanetScale pattern). Zero runtime overhead. Anti-god-class by design: 1 file `.sql` = 1 funzione Go generata.
- Migrations: **`golang-migrate/migrate/v4`** — file `up/down` versionati in `internal/db/migrations/`
- Pool: `pgxpool.Pool` con config sensible default (`MaxConns=10`, `MinConns=1`, idle 30s)

**Smoke.**
```bash
docker compose -f sandbox/compose.yaml up -d postgres
./aura db migrate           # applica tutte le migration pendenti
./aura db ping              # SELECT 1, stampa "ok + ms"
./aura db status            # lista migration applicate
```

**Acceptance.**
- [ ] Container `aura-postgres` su volume named `aura_aura-postgres` (NO bind-mount Windows — memory `feedback_sqlite_wal_windows_corruption.md` insegna). Healthcheck `pg_isready`.
- [ ] DSN da `internal/config` (`Config.DB.URL`, env override `AURA_DB_URL`, default `postgres://aura:aura@127.0.0.1:5432/aura?sslmode=disable`). Password reale da `.env` (`POSTGRES_PASSWORD`).
- [ ] `sqlc generate` produce codice in `internal/db/sqlc/` da `internal/db/queries/*.sql` + `internal/db/schema.sql`. Make target `make sqlc` lo lancia, CI fa fail se output non sincronizzato col commit (golden test).
- [ ] `aura db migrate` idempotente. Applica solo le migration nuove. Errore esplicito su schema drift (migration applicata + file changed → abort).
- [ ] `internal/db/db.go` espone `Open(ctx, cfg) (*pgxpool.Pool, error)` con ping al boot. Fail-fast se Postgres non raggiungibile.
- [ ] Test integrazione sotto `//go:build db_integration` — salta in CI senza container Postgres (no flaky).
- [ ] Schema iniziale **vuoto**. Le tabelle (`paused_states`, `scheduler_tasks`, `skill_audit`, ecc.) atterrano nelle rispettive slice. Solo migration `0001_init.sql` = `CREATE SCHEMA IF NOT EXISTS aura;` + commento explanatory.

**File targets** (~280 LOC src + ~120 test + infra):
| Path | LOC | Note |
|---|---|---|
| `internal/db/db.go` | ~90 | `Open(ctx, *Config) (*pgxpool.Pool, error)`. Pool config, ping, graceful close. |
| `internal/db/config.go` | ~40 | `DBConfig{URL, MaxConns, MinConns, ConnMaxIdleSec}`. Letto da `internal/config`. |
| `internal/db/migrate.go` | ~80 | Wrapper su `golang-migrate` embedded (`embed.FS` di `migrations/`). `Up`, `Down`, `Status`. |
| `internal/db/schema.sql` | ~20 | Sorgente di verità per `sqlc generate`. Estende con CREATE TABLE quando le slice atterrano. |
| `internal/db/queries/.gitkeep` | — | Directory vuota inizialmente. |
| `internal/db/migrations/0001_init.up.sql` | ~10 | `CREATE SCHEMA IF NOT EXISTS aura; SET search_path TO aura, public;` |
| `internal/db/migrations/0001_init.down.sql` | ~5 | `DROP SCHEMA aura CASCADE;` |
| `sqlc.yaml` | ~30 | Config sqlc v2: engine postgresql, queries dir, out dir, json_tags, emit_interface, emit_exact_table_names. |
| `internal/db/db_test.go` | ~120 | Build tag `db_integration`. Open + ping + migrate + simple SELECT + close. |
| `internal/config/config.go` (diff) | ~+25 | Aggiunge `DB DBConfig` al Config. Carica `POSTGRES_PASSWORD` da `.env`. |
| `cmd/aura/main.go` (diff) | ~+50 | Sub-command `aura db {migrate|ping|status|reset}`. |
| `sandbox/compose.yaml` (diff) | ~+20 | Service `postgres` con healthcheck + volume named + env from `.env`. |
| `Makefile` | ~30 | Target `make sqlc` (regen), `make db-up`, `make db-migrate`, `make db-reset`. |
| `.env.example` | ~10 | Template con `POSTGRES_PASSWORD=changeme`, `OPENROUTER_API_KEY=`. |

**Open questions.**
1. **Schema namespace: `aura` o `public`?** → *Default proposto: `aura` schema dedicato. Permette in futuro multi-tenant (1 db, N schema). Search path settato nella prima migration.*
2. **Migration: file-based (golang-migrate) o sqlc-generated?** → *Default proposto: file-based. sqlc ha modulo migration sperimentale ma non production-grade. golang-migrate è battle-tested.*
3. **Soft-delete by default?** → *Default proposto: NO. Le tabelle che vogliono soft-delete aggiungono `deleted_at` esplicitamente. Mai default invisibile.*

**Acceptance addizionali (post-audit round 1):**
- [ ] `.env` esplicitamente in `.gitignore` (verifica CI: `grep -q '^\.env$' .gitignore`). `.env.example` (committato) ha placeholder vuoti.
- [ ] `aura db migrate` con `.env` mancante o `OPENROUTER_API_KEY` vuota → errore chiaro al boot, no panic, no fallthrough silent.
- [ ] `internal/config` resta SOLO root composite `Config{LLM, DB, RunDir, ToolPreviewCap}`. Ogni subsystem (sandbox/web/...) tiene il proprio `*Config` nel proprio package (es. `internal/web/config.go`). Niente god class config.
- [ ] Servizi compose con `depends_on: condition: service_healthy` per: postgres, neo4j-cypher (quando atterra), aura-llama-embed. Aura container fail-fast se uno non risponde.

**Mini-PC RAM budget — sezione obbligatoria per ogni slice che aggiunge container.**

Cumulativa stimata a fine Slice 7 (mini-PC 16-core, target 32 GB RAM):

| Servizio | RAM idle stimata | Slice che lo aggiunge |
|---|---:|---|
| Postgres 17 | ~250 MB | 0.5 |
| Neo4j 5.x Community + APOC + GDS + vector index | ~1.5-2 GB | 0.7 |
| aura-llama-embed (embeddinggemma CPU, 4 thread) | ~600 MB | (esterno, già presente) |
| SearXNG | ~150 MB | 5 |
| Sandbox Python sidecar | ~80 MB | 2 |
| Pocket-TTS | ~400 MB | (esterno) |
| Whisper.cpp server | ~300 MB | (esterno) |
| Markitdown sidecar | ~150 MB | (esterno) |
| Aura Go binary | ~150 MB | 1 |
| **Totale idle** | **~3.5-4 GB** | |
| Sotto carico (LLM batch + swarm 3 worker) | **+1 GB** | |
| **Peak realistic** | **~5 GB** | |

Headroom su 32 GB: ampio. Su 16 GB: ~11 GB liberi per OS + utente. Accettabile.
Se la stima passa 8 GB peak in futuro, va dedicato un budget review.

**Backup strategy (audit round 2 P0).** I due store stateful (Postgres + Neo4j) hanno backup automatico:

- **Postgres** — `pg_dump` cronato via Slice 6 scheduler (`TaskKind=backup_postgres`, default `daily 03:00`, destination `~/.aura/backups/postgres/aura-<date>.sql.gz`). Retention 14 giorni rolling. Restore manuale via `aura db restore <file.sql.gz>`. Subcommand documentato in `aura db --help`.
- **Neo4j** — `neo4j-admin database backup --to-path=/backups` via `docker exec aura-neo4j` cronato (stessa scheduler TaskKind `backup_neo4j`, default `daily 03:30`). Retention 7 giorni rolling. Restore manuale via runbook in `docs/runbooks/neo4j-restore.md`.
- **Acceptance**: backup TaskKind handler atterra in slice 6b (insieme a `agent_job`). Test: schedule backup_postgres in=1m → file dump esistente in `~/.aura/backups/postgres/` con dimensione >1 KB.

**Commit message template.**
```
slice 0.5: postgres + sqlc + pgx + golang-migrate infrastructure

PostgreSQL 17 container (named volume, no bind-mount), pgxpool.Pool
opened from internal/db. sqlc.yaml + queries/ dir + generated/
scaffolding. golang-migrate wired with embedded migrations FS.
Schema 'aura' dedicated. Empty initial schema — tables land in
their slices (1.5/6/7).

Smoke: aura db {migrate|ping|status} all green against fresh
container.

LOC: +XXX src / +YY test / +ZZ infra.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Slice 0.7 — Infra Neo4j (Community + mcp-neo4j-cypher + Cypher migrations)

**Goal.** Knowledge graph backbone industrial-grade. Container Neo4j produzione
+ MCP server `mcp-neo4j-cypher` subprocess stdio + Cypher schema/index
migrations versionate. Senza questa slice, le slice future che leggono o
scrivono knowledge (post-Slice 7) non hanno dove vivere, e il backup
`backup_neo4j` cronato di Slice 6b punta a un container inesistente.

Le 8 slice 1→7 NON consumano Neo4j: è infrastruttura pre-deployata,
parallela alla Slice 0.5 Postgres. Atterrare 0.7 subito dopo 0.5 elimina
il rischio "container produzione promesso ma mai materializzato".

**Stack scelto** (validato dallo spike `D:/tmp/aura-neo4j-spike-2026-05-27/`
Phase 6b: 22-30 ms p95 + IT recall@5 5/5 su corpus reale Aura):
- DB: **Neo4j 5.x Community** (container `neo4j:5-community`, ~1.5-2 GB RAM idle)
- Plugins: **APOC** (procedure standard) + **GDS** (Graph Data Science, community detection, PPR) + Vector index (built-in 5.x, Apache Lucene HNSW)
- MCP server: **`mcp-neo4j-cypher`** (Apache 2.0) — subprocess stdio spawn-ato da Aura, lifecycle accoppiato al processo principale. **No native Go adapter** (per disciplina CLAUDE.md): tutto accesso Neo4j passa da MCP.
- Embedding dim: **768 nativo** da `aura-llama-embed` (nessuna MRL truncation, l'index Neo4j HNSW è configurato a 768 dim)
- DB name: **`neo4j` default** (Community non supporta multi-database; `CREATE DATABASE aura` richiede Enterprise)
- Migrations: file `.cypher` numerati in `internal/knowledge/migrations/`, eseguiti via MCP `cypher_execute`. Audit applicate registrato in **Postgres** tabella `aura.knowledge_migrations` (centralizza audit con golang-migrate).

**Smoke.**
```bash
docker compose -f sandbox/compose.yaml up -d neo4j
./aura neo4j migrate           # applica tutte le migration .cypher pendenti
./aura neo4j ping              # MATCH (n) RETURN count(n), stampa "ok + ms"
./aura neo4j status            # lista migration applicate (da Postgres)
```

**Acceptance.**
- [ ] Container `aura-neo4j` su volume named `aura_aura-neo4j` (NO bind-mount Windows — coerente con feedback `feedback_sqlite_wal_windows_corruption.md` esteso a Neo4j). Healthcheck `cypher-shell -u neo4j -p $NEO4J_PASSWORD --database neo4j 'RETURN 1'`.
- [ ] Auth via `NEO4J_PASSWORD` da `.env` (default `changeme`, must change al primo boot). `NEO4J_AUTH=neo4j/$NEO4J_PASSWORD` propagato al container.
- [ ] Plugins APOC + GDS abilitati via `NEO4J_PLUGINS='["apoc","graph-data-science"]'` (auto-download Neo4j Community feature 5.x).
- [ ] `mcp-neo4j-cypher` spawn-ato da Aura come subprocess stdio. Endpoint configurato via `Config.Neo4j.MCPBinary` (default `mcp-neo4j-cypher`, PATH-resolvable). Auth bolt URI `bolt://127.0.0.1:7687` + `NEO4J_USER`/`NEO4J_PASSWORD` da env.
- [ ] Migration `0001_init.cypher`:
  ```cypher
  CREATE CONSTRAINT chunk_id IF NOT EXISTS
    FOR (c:Chunk) REQUIRE c.id IS UNIQUE;

  CREATE VECTOR INDEX chunk_embedding IF NOT EXISTS
    FOR (c:Chunk) ON c.embedding
    OPTIONS {indexConfig: {
      `vector.dimensions`: 768,
      `vector.similarity_function`: 'cosine'
    }};

  CREATE FULLTEXT INDEX chunk_text IF NOT EXISTS
    FOR (c:Chunk) ON EACH [c.text];
  ```
  Schema iniziale **minimale**: solo `:Chunk` perché è l'unica label che gli slice 1→7 useranno indirettamente (via tool result sidecar future). Le altre label (UserProfileMemory, Entity, Source, ecc.) atterrano nelle rispettive slice knowledge.
- [ ] `aura neo4j migrate` idempotente. Applica solo le migration nuove. Errore esplicito su schema drift (migration applicata + file `.cypher` changed → abort).
- [ ] `internal/knowledge/client.go` espone `Open(ctx, cfg) (*Client, error)` con ping MCP al boot. Fail-fast se MCP server non risponde entro 10 s.
- [ ] Container Aura con `depends_on: condition: service_healthy` per `neo4j` (oltre a `postgres` e `aura-llama-embed`).
- [ ] Test integrazione sotto `//go:build neo4j_integration` — salta in CI senza container Neo4j (no flaky), parallelo al pattern `db_integration` di Slice 0.5.
- [ ] Backup TaskKind `backup_neo4j` (definito in Slice 0.5 RAM/Backup table, implementato in Slice 6b) ora punta al container produzione `aura-neo4j`, non più allo spike `neo4j-spike`.

**File targets** (~330 LOC src + ~80 test + infra):
| Path | LOC | Note |
|---|---|---|
| `internal/knowledge/client.go` | ~80 | `Open(ctx, *Config) (*Client, error)`. MCP subprocess spawn, stdio framing, graceful close. `Cypher(ctx, query, params) ([]Record, error)` thin wrapper. |
| `internal/knowledge/config.go` | ~40 | `Neo4jConfig{BoltURL, User, Password, MCPBinary, Database, ConnectTimeoutSec}`. Letto da `internal/config`. |
| `internal/knowledge/migrate.go` | ~90 | Legge `migrations/*.cypher` numerati (embed.FS), parse front-matter `-- migrate:up`, esegue via MCP, registra successo/fallimento in Postgres `aura.knowledge_migrations`. `Up`, `Status` (read da Postgres). |
| `internal/knowledge/ping.go` | ~30 | `Ping(ctx) error` → `RETURN 1`, latency tracking. |
| `internal/knowledge/migrations/0001_init.cypher` | ~25 | Constraint + vector index + fulltext index come sopra. |
| `internal/db/queries/knowledge_migrations.sql` | ~30 | **2 query sqlc**: `RecordKnowledgeMigration`, `ListAppliedKnowledgeMigrations`. |
| `internal/db/migrations/0002_knowledge_migrations.up.sql` | ~20 | `CREATE TABLE aura.knowledge_migrations (version int pk, name text, applied_at timestamptz default now(), checksum text)`. |
| `internal/db/migrations/0002_knowledge_migrations.down.sql` | ~3 | `DROP TABLE aura.knowledge_migrations;` |
| `internal/knowledge/client_test.go` | ~80 | Build tag `neo4j_integration`. Open + ping + migrate + simple Cypher + close. |
| `sandbox/compose.yaml` (diff) | ~+30 | Service `neo4j` (`neo4j:5-community`, plugins APOC+GDS, volume named, healthcheck, env auth). |
| `cmd/aura/main.go` (diff) | ~+40 | Sub-command `aura neo4j {migrate|ping|status|reset}`. |
| `Makefile` (diff) | ~+15 | Target `make neo4j-up`, `make neo4j-migrate`, `make neo4j-reset`. |
| `.env.example` (diff) | ~+5 | `NEO4J_PASSWORD=changeme`, `NEO4J_USER=neo4j`. |

**Deferred-tool partition.** Niente tool nuovo in questo slice. È pura infra. Le slice future che esporranno tool knowledge-facing decidono il loro tier.

**Open questions.**
1. **MCP binary distribution: bundled in `aura init-models` o richiesto su PATH?** → *Default proposto: richiesto su PATH (`mcp-neo4j-cypher` installato via `pip install mcp-neo4j-cypher` o equivalente). Fail-fast all'avvio con messaggio chiaro se non trovato. Bundling = scope creep, rinviato.*
2. **Retention `aura.knowledge_migrations`?** → *Default proposto: nessuna. È audit append-only, una riga per migration. A 1000 migration siamo a ~80 KB. Non vale la pena.*
3. **`neo4j_integration` test fixture data: in `testdata/*.cypher` o programmatici?** → *Default proposto: programmatici (Cypher inline nei test). Fixture file `.cypher` è premature optimization a questo punto.*

**Mini-PC RAM budget — delta vs Slice 0.5.**

Neo4j 5.x Community + APOC + GDS + vector index a 768 dim su corpus realistico (≤ 100k chunk) consuma stabilmente 1.5-2 GB RAM idle. A 1M chunk (limite alto del power user) ~3-4 GB. Già contato nella tabella cumulativa di Slice 0.5 riga 87 (ora aggiornata: Slice 0.7).

**Commit message template.**
```
slice 0.7: neo4j community + mcp-neo4j-cypher + cypher migrations

Neo4j 5.x Community container (named volume, APOC+GDS plugins),
mcp-neo4j-cypher subprocess stdio spawned by Aura, Cypher migrations
file-based via MCP with audit in Postgres aura.knowledge_migrations
(sqlc-managed). 0001_init.cypher: :Chunk(id) UNIQUE constraint +
vector index HNSW 768d cosine + fulltext index. Embedding dim 768
native (no MRL truncation, vector index configured at 768).
Subcommand aura neo4j {migrate|ping|status}.

Renames backup_neo4j docker exec target from neo4j-spike to
aura-neo4j (Slice 0.5 row 105 amended).

Smoke: aura neo4j {migrate|ping|status} all green against fresh
container.

LOC: +XXX src / +YY test / +ZZ infra.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Slice 1 — LLM client reale + ToolResult pattern (preview + persist)

**Goal.** Due cose in un commit (sono accoppiate; spezzarle costringerebbe a
toccare due volte gli stessi 5 file):

1. **LLM wire reale.** Sostituire `stubClient` in [cmd/aura/main.go](cmd/aura/main.go)
   con un client OpenAI-compatibile che stream-pa SSE da un endpoint reale
   (DeepSeek/Anthropic/OpenAI), accumula tool-call delta a tool-call completi,
   e li emette sul canale `<-chan llm.Chunk` rispettando il contratto in
   [internal/llm/client.go](internal/llm/client.go).
2. **ToolResult pattern.** Cambiare la firma `tools.Tool.Execute` in
   [internal/agent/tools/spec.go:32](internal/agent/tools/spec.go) da
   `(string, error)` a `(ToolResult, error)` perché tutti i tool che seguono
   (`execute` sandbox slice 2, `swarm.*` slice 3) hanno bisogno del pattern
   preview+persist verificato empiricamente nel turn precedente: 21 MB stdout
   reale → 12 KB nel context tramite preview+sidecar-file. Senza questo cambio
   adesso, slice 2 e 3 lo reintrodurrebbero ad-hoc e slice 4 (KV cache)
   troverebbe la history avvelenata da result-payload grandi.

**Perché un commit solo.** Cambio firma `Execute` tocca `text_response.go`,
`search.go`, `loop.go.runTool`, `spec.go`. Lo stesso commit che porta il client
reale può/deve far landing del pattern: se atterriamo prima il client reale,
poi cambio firma → due commit ricontaminano gli stessi file, riopendendo
file ≤600 LOC e refactor-on-touch per ognuno. Slice 1 cresce di ~120 LOC
ma slice 2 e 3 risparmiano ~150 LOC ciascuno di plumbing duplicato.

**Smoke.**
```bash
AURA_LLM_BASE_URL=https://api.deepseek.com/v1 \
AURA_LLM_API_KEY=sk-... \
AURA_LLM_MODEL=deepseek-chat \
./aura chat "ciao, dimmi 2+2 in tre parole"
```
→ deve stampare una reply reale dal modello (non lo stub), via `text_response`,
in <8 step.

**Acceptance (machine-checkable).**

*Parte 1 — wire:*
- [ ] `go test ./internal/llm/...` passa con almeno 1 fixture SSE golden (tool-call multi-chunk + finish_reason="tool_calls", e plain text + finish_reason="stop").
- [ ] `aura chat "..."` con config settata produce reply dal modello vero.
- [ ] `aura chat "..."` senza config fallisce con messaggio chiaro (no panic, no fallback silenzioso).
- [ ] Cancel context (Ctrl+C) chiude la HTTP connection e drena il channel — verificato con `go test -race` + `go.uber.org/goleak` `goleak.VerifyNone(t)` in TestMain (audit round 2 P1: assert nessun goroutine residuo post-test, copre il caso SSE reader bloccato su `bufio.Scanner.Scan()` post-cancel).
- [ ] Zero allocazioni per `Message`-history mutation: il client legge `req.Messages` ma non lo modifica (test asserisce slice identica pre/post).

*Parte 2 — ToolResult pattern:*
- [ ] `Tool.Execute(ctx, args)` ritorna `(ToolResult, error)`. `ToolResult{Preview string, FullPath string, Bytes int, Truncated bool}`.
- [ ] `Loop.runTool` persiste `ToolResult` su disco se `Bytes > AURA_CONTEXT_PREVIEW_CAP_BYTES` (default 2048). Path = `$AURA_RUN_DIR/conversations/<conv_id>/<tool-call-id>.result` (post-Slice 1.8 layout; vedi sezione "AURA_RUN_DIR layout" sotto). La stringa che entra in `RoleTool.Content` è `Preview + "\n\n[truncated: N bytes total, full output at <FullPath>. Use read_tool_output to fetch ranges.]"`.
- [ ] Se `Bytes <= cap`, nessuna scrittura su disco; `RoleTool.Content = Preview` puro (no overhead).
- [ ] Builtin tool `read_tool_output` (non-deferred) accetta `{tool_call_id, offset?, limit?}` e ritorna la fetta richiesta dal sidecar file. Default `limit=200 righe`. Hard-fail su tool_call_id ignoto.
- [ ] `text_response` continua a essere il terminale del loop: il suo `ToolResult.Preview` è la risposta finale all'utente (anche se `Bytes > cap`, la versione full sta sul disco; il preview va all'utente per default — il chiamante CLI/Telegram decide se servire la versione full).
- [ ] Test: un tool fake che ritorna 100 KB di output → il `Messages` history dopo `Loop.Turn` ha SOLO il preview (≤2 KiB + footer), file su disco ha 100 KB completi, `read_tool_output` recupera fetta arbitraria.
- [ ] **`$AURA_RUN_DIR` layout (Area #9 closed 2026-05-28)**. Default `$AURA_RUN_DIR = ~/.aura/run/`. Layout:
  ```
  $AURA_RUN_DIR/
    conversations/
      <conv_id>/                  # = aura.conversations.id (FK durable via Slice 1.8)
        <tool_call_id>.result     # ToolResult sidecar (Slice 1)
        <seq>.content             # conversation_turns content spillover (Slice 1.8)
    tmp/
      <unix-ts>-<rand4>.<ext>     # oneoff scratch (aura exec senza conversation, etc.)
  ```
  Lifetime: `conversations/<conv_id>/` vive quanto la conversation row in DB; `tmp/` ha TTL 24h, sweep al boot. Cleanup cascade `os.RemoveAll(conversations/<id>/)` al `aura chat delete <id>`. Boot orphan scan: dir senza conversation row corrispondente → log + rm. WARN se `$AURA_RUN_DIR` > `AURA_RUN_DIR_WARN_THRESHOLD_BYTES` (default `1073741824` = 1 GiB) al boot, no auto-delete.

**File targets** (totale ≤ 520 LOC src + ~200 test).

*Wire layer (~340 src + ~120 test):*
| Path | LOC stimato | Note |
|---|---|---|
| `internal/llm/openai_compat.go` | ~280 | `Client` impl, SSE parser, tool-call accumulator (delta-merge per `index`). Connect 10s, global timeout configurabile, no idle, no retry (vedi Open Questions). **Inietta `Config.LLM.Headers` su ogni request** (OpenRouter HTTP-Referer + X-Title). |
| `internal/config/config.go` | ~110 | `Config{LLM: LLMConfig{Provider, Model, BaseURL, APIKey, TotalTimeoutSec, Headers map[string]string}, RunDir, ToolPreviewCap}`. **Load order**: built-in default → `.env` (via `github.com/joho/godotenv`, key `OPENROUTER_API_KEY` → `LLM.APIKey`) → file JSON (`$AURA_CONFIG_DIR/llm.json`, default `~/.aura/llm.json`) → env vars (`AURA_LLM_*`, `AURA_RUN_DIR`, `AURA_CONTEXT_PREVIEW_CAP_BYTES`). **Default built-in**: Provider=`openrouter`, Model=`deepseek/deepseek-v4-flash:exacto`, BaseURL=`https://openrouter.ai/api/v1`, Headers=`{"HTTP-Referer": "https://github.com/chetto1983/aura", "X-Title": "Aura"}`. `Save()` per write-back dal dashboard futuro. |
| `internal/config/config_test.go` | ~50 | Load-order test: default < file < env. Round-trip JSON. |
| `internal/llm/openai_compat_test.go` | ~120 | Fixture SSE in `testdata/` per: text-only stream, tool-call multi-chunk (delta-merge), error 429 (no retry → bubble up), premature close (ctx-cancel), Anthropic ephemeral cache_control passthrough. Niente prompt da asilo nido (vedi §Test discipline). |

*ToolResult pattern (~180 src + ~80 test):*
| Path | LOC stimato | Note |
|---|---|---|
| `internal/agent/tools/result.go` | ~60 | `ToolResult{Preview, FullPath, Bytes, Truncated}`. Helper `NewToolResult(b []byte, cap int) ToolResult` che decide preview vs persist. Helper `Persist(dir, toolCallID string)` che scrive il file. |
| `internal/agent/tools/spec.go` (diff) | ~+5 / -3 | Firma `Execute(ctx, args) (ToolResult, error)`. |
| `internal/agent/tools/text_response.go` (diff) | ~+15 / -10 | Ritorna `ToolResult{Preview: text}` invece di stringa. |
| `internal/agent/tools/search.go` (diff) | ~+15 / -10 | Stesso adattamento. |
| `internal/agent/tools/read_tool_output.go` (NEW) | ~80 | Builtin non-deferred. Args `{tool_call_id, offset?:int default 0, limit?:int default 200 lines}`. Risolve path da `Loop`-mantenuto map `toolCallID → FullPath`. Hard-fail su id ignoto. |
| `internal/agent/loop.go` (diff) | ~+45 / -10 | `runTool` riceve `ToolResult`, decide se persistere su disco se `Bytes > cap`, costruisce stringa per `RoleTool.Content` (preview + footer con path), mantiene `resultPaths map[string]string` per `read_tool_output` lookup. Crea `$AURA_RUN_DIR/conversations/<conv_id>/` lazy alla prima persist (post Slice 1.8: `conv_id` viene da Loop). |
| `internal/agent/tools/result_test.go` | ~80 | Test: 100 KB fake tool → `Messages` ha SOLO preview+footer, file ha 100 KB; `read_tool_output(id, offset=50000, limit=100)` recupera fetta. |
| `cmd/aura/main.go` (diff) | ~+50 / -15 | Sostituisce `stubClient` con `llm.NewOpenAICompat(cfg.LLM)`. Registra `read_tool_output` nel registry. Sub-comando `aura config` legge/scrive il file. |

**Deferred-tool partition.** Niente tool nuovo in questo slice. `text_response` + `tool_search` restano gli unici registrati. La distinzione attiva/deferred si vede solo via `aura tools` (già esistente).

**Open questions — CHIUSE.**
1. **~~Provider primario~~ → OpenRouter + `deepseek/deepseek-v4-flash:exacto`, API key da `.env`.**
   Provider: **OpenRouter** (base URL `https://openrouter.ai/api/v1`, OpenAI-compat nativo).
   Model default: `deepseek/deepseek-v4-flash:exacto` (variant `:exacto` instradata da OpenRouter).
   API key: caricata da `.env` file via `github.com/joho/godotenv` (chiave `OPENROUTER_API_KEY`).
   Config rimane comunque dashboard-editable: `internal/config` legge `~/.aura/llm.json` + override env + `.env` (ordine: built-in default < `.env` < `llm.json` < env vars).
   Default built-in: `Provider="openrouter"`, `Model="deepseek/deepseek-v4-flash:exacto"`, `BaseURL="https://openrouter.ai/api/v1"`, `APIKey=""` (richiesto, vuoto = error chiaro).
   **Headers OpenRouter raccomandati** (`HTTP-Referer: https://github.com/chetto1983/aura`, `X-Title: Aura`): inclusi by-default nel client, override-abili via config. Servono per attribution OpenRouter (visibility nei dashboard, possibili discount tier).
2. **~~Timeout HTTP~~ → Global timeout + ctx-cancel, NO idle/first-byte.**
   Evidenza unanime in 5/5 client production analizzati (vedi sotto). La proposta originale (first-byte 30s) viene scartata: nessuno la implementa, e con reasoning models che possono pensare 2 minuti prima del primo token, un first-byte 30s sarebbe un footgun.
   *Defaults:* `Dial: 10s` (OpenHuman pattern), `TotalTimeout: 120s` configurabile via `Config.LLM.TotalTimeoutSec` (default safe per chat, gli utenti reasoning lo alzano a 600s nel JSON), nessun idle-gap timeout, ctx-cancel propaga end-to-end e chiude la connessione HTTP.
   - Evidenza: [D:/tmp/openhuman/src/openhuman/inference/provider/compatible.rs:283-284](D:/tmp/openhuman/src/openhuman/inference/provider/compatible.rs) (120s + connect 10s), [D:/tmp/nanobot/nanobot/agent/runner.py:632](D:/tmp/nanobot/nanobot/agent/runner.py) (300s default `NANOBOT_LLM_TIMEOUT_S`, env-overridable), [D:/tmp/picobot/internal/providers/openai.go:28](D:/tmp/picobot/internal/providers/openai.go) (60s default `timeoutSecs`), [D:/tmp/codex/codex-rs/exec-server/src/client/reqwest_http_client.rs:56](D:/tmp/codex/codex-rs/exec-server/src/client/reqwest_http_client.rs) (global only, no rolling). Line numbers verified pre-rewrite audit round 1.
3. **~~Retry policy~~ → NO retry nel wire. Errore al chiamante.**
   Spaccatura 3/2 nel sample (codex/picobot/openhuman = no retry; nanobot/agent-infra-sandbox = sì). Aura sceglie il pattern 3/5 perché:
   - Aura ha un caller naturale (Loop → CLI/Telegram/Dashboard) ben posizionato per decidere se ritentare. Il wire non sa se l'utente è in chat sincrona (retry stupido perde l'utente) o batch (retry essenziale).
   - Aggiungere retry adesso = aggiungere superficie (Retry-After parser, jitter, max-attempts config) che dovremmo testare e che reagisce a fault non riproducibili in CI.
   - Mid-stream reset è unanimemente non-resumable. Un retry-from-scratch su uno stream parzialmente consumato significherebbe ri-invio dell'intera history → costo + duplicate-side-effect dei tool eseguiti finora.
   *Implementazione:* `openai_compat.go` propaga errori HTTP wrapped (`HTTPError{StatusCode, RetryAfterSec, Body}`) così il chiamante futuro (se vuole retry) ha tutto il segnale serializzato — senza implementare il retry adesso.
   - Evidenza: [D:/tmp/codex/codex-rs/codex-client/src/sse.rs:14](D:/tmp/codex/codex-rs/codex-client/src/sse.rs) (no retry), [D:/tmp/picobot/internal/providers/openai.go](D:/tmp/picobot/internal/providers/openai.go) (no retry), [D:/tmp/openhuman/src/openhuman/inference/provider/ops.rs:364-376](D:/tmp/openhuman/src/openhuman/inference/provider/ops.rs) (retry at provider chain, not wire). Pattern opposto: [D:/tmp/nanobot/nanobot/providers/base.py:97,307-310,645-679](D:/tmp/nanobot/nanobot/providers/base.py), [D:/tmp/agent-infra-sandbox/sdk/js/src/core/fetcher/requestWithRetries.ts:18-62](D:/tmp/agent-infra-sandbox/sdk/js/src/core/fetcher/requestWithRetries.ts).

**Commit message template.**
```
slice 1: real OpenAI-compat streaming client

Replace stubClient with internal/llm/openai_compat: SSE parser,
tool-call delta accumulator (index-merged), env-driven config.
Cancel-propagation through ctx; first-chunk timeout 30s; no retry.

Smoke: aura chat "..." against deepseek-chat returns a real reply.

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Slice 1.5 — ask_user (loop pause + resume primitive)

**Goal.** Riportare il tool `ask_user` pre-rewrite — una primitive
del loop che permette all'agente di pausare il run e attendere input
strutturato dall'utente. È prerequisito per Slice 7 (governance C delle
skill via human-in-the-loop) e per qualsiasi altro tool futuro che voglia
"fermarsi e chiedere" (scheduler conferma cron costoso, swarm conferma
spawn ad alta depth, web_fetch su URL ambigui).

Separata da Slice 1 per disciplina: tocca `loop.go` una seconda volta
ma su una concern semanticamente diversa (state machine del loop:
running → waiting_for_user → running). Far landing Slice 1 prima
permette di validare il client streaming senza la complessità della
pausa/resume. Tre commit separati che toccano `loop.go` (Slice 1 →
Slice 1.5 → eventuale altro) violerebbero refactor-on-touch.

**Pattern preservato dal pre-rewrite** ([internal/agent/tools/registry/ask_user.go](git:pre-rewrite-2026-05-27/internal/agent/tools/registry/ask_user.go), 164 LOC verified):
- Tool standard (non-deferred), sempre visibile nel manifest.
- Args: `{question: string, options?: [2-4 string | {label, value}], kind: clarification|approval|choice, priority?: int 0-100 default 0}`. `priority` è un order_hint: l'agente può marcare le sue ask più urgenti per renderle prime nella coda UI.
- `Execute` non ritorna `ToolResult`: ritorna un **sentinel error** `ErrAwaitingUserInput{Question, Options, Kind, Priority, ToolCallID}` (campi 1:1 dal pre-rewrite + `Priority`). Il loop lo intercetta e NON appende un fake tool result.
- **Campo NUOVO aggiunto in Slice 1.5**: `ResumeContext map[string]any` — payload serializzato che il loop persiste in Postgres insieme alla PausedState. Permette ai resume handler (es. skill.create approve → activate; skill.delete approve → cascade) di rieseguire l'azione differita senza ricostruire lo stato. Pre-rewrite non aveva questo perché le proposal skill passavano per la dashboard API, non per ask_user.
- **Esclusivo intra-turn**: se l'LLM batcha `ask_user` con altri tool call nello stesso turn, `ask_user` vince — gli altri call vengono droppati e re-emessi al resume. Più `ask_user` nello stesso turn dello STESSO loop sono coalesce-ati: tutti diventano `PausedState` separate ma il Loop rimane pausato finché tutte sono resumed.
- 3 `kind` semanticamente distinti:
  - `clarification` — info mancante per procedere
  - `approval` — azione rischiosa, conferma esplicita richiesta
  - `choice` — scelta strutturata fra opzioni note (ideale per skills.sh registry browse, swarm tier selection, ecc).

**Smoke E2E (prompt reale — vedi §Test discipline):**
```bash
./aura chat "ho 47 file in d:/tmp che non tocco da più di 6 mesi, sono al sicuro a cancellarli?"
# → modello deve chiamare ask_user(kind=approval, options=["Sì cancella", "Mostra prima la lista", "Lascia stare"])
# → Loop entra in stato Paused, stampa la question + options nel terminale
# → utente digita "2" (o "Mostra prima la lista")
# → Loop riprende, modello procede col list invece di delete
```

**Acceptance.**
- [ ] `Loop.Turn` ritorna `(reply string, pending []*PausedState, err error)` invece di `(string, error)`. Se `len(pending) > 0`, il caller (CLI/Telegram/dashboard) sa di dover renderizzare TUTTE le pending (ordinate per `priority DESC, created_at ASC`) e raccogliere le risposte.
- [ ] `Loop.Resume(ctx, token, answer string) (reply string, pending []*PausedState, err error)` resume singolo per token. Se restano altre pending dopo il resume, ritorna `pending != empty` e il Loop resta pausato. Token è un opaco ID (UUID v4).
- [ ] `Loop.ResumeBatch(ctx, answers map[uuid.UUID]string) (reply string, pending []*PausedState, err error)` resume di N pending in un colpo. Le pending non incluse nel batch restano attive. Comodo per CLI batch input e per dashboard multi-resolve.
- [ ] Loop NON appende `RoleTool` message per un `ask_user` finché la sua PausedState non è resumed. Quando arriva: appende `RoleTool{ToolCallID: <original>, Content: answer}`. Quando TUTTE le pending sono resumed, il Loop riprende un giro LLM con tutti i RoleTool messages accumulati.
- [ ] Esclusività intra-turn: test con LLM stub che ritorna 3 tool call nello stesso turn (ask_user + read_tool_output + text_response) → solo ask_user dispatch-ato, gli altri due loggati come dropped, Loop pausato con 1 PausedState.
- [ ] Multi-pause coalesce: test con LLM stub che ritorna 2 ask_user + 1 read_tool_output nello stesso turn → 2 PausedState create (priorità default 0, ordering FIFO da created_at), read_tool_output droppato, Loop pausato con `len(pending)==2`.
- [ ] Args validation: `question` non vuota (trimmed), `options` 0 o 2-4 (mai 1), labels distinct, `priority` intero `0-100` (default 0, cap 100).
- [ ] CLI rendering: `aura chat` e `aura shell` mostrano **tutte** le pending numerate `[1]..[N]` con prefisso `<kind>[priority]`, leggono `stdin` per la risposta. Sintassi: `1: <answer>` singolo, `all: <answer>` stessa risposta a tutte, `batch: 1=<a>, 2=<b>, 3=<c>` batch multi. Re-prompt 3 volte poi abort.
- [ ] Test resume cycle singolo: Loop.Turn → 1 pending → Loop.Resume(token, answer) → modello vede `RoleTool` con answer → procede.
- [ ] Test resume cycle multi: Loop.Turn → 3 pending → Loop.ResumeBatch(3 answers) → modello vede 3 `RoleTool` messages → procede.

**File targets** (~330 LOC src + ~120 test):
| Path | LOC | Note |
|---|---|---|
| `internal/agent/tools/ask_user.go` | ~180 | Tool def + `ErrAwaitingUserInput` sentinel (con `Priority int`) + args parser (options string/object polimorfico + priority int cap 0-100). Quasi 1:1 dal pre-rewrite + priority field. |
| `internal/agent/pending.go` | ~95 | `PausedState{Token, ConversationID, Question, Options, Kind, Priority, ResumeContext, ToolCallID, ProxiedFromChildID *uuid.UUID, ProxiedToolCallID *string, CreatedAt, ResumedAt *time.Time, ResumedAnswer *string}`. Helper per costruzione, serializzazione, sorting (priority DESC, created_at ASC). |
| `internal/askuser/cli.go` | ~120 | Renderer CLI: stampa lista numerata `[N] <kind>[prio]: <question> + options`, legge stdin con sintassi `1: ...` / `all: ...` / `batch: 1=a, 2=b`. Re-prompt invalido. Implementa interfaccia `Responder`. |
| `internal/agent/loop.go` (diff) | ~+110 / -10 | `runTool` intercetta `ErrAwaitingUserInput` → costruisce `PausedState` con priority + (se proxied) `ProxiedFromChildID` → upsert in `aura.paused_states` → `Turn` accumula pending → se ≥1 pending, ritorna `(empty, pending, nil)`. Nuovi metodi `Resume(token, answer)` e `ResumeBatch(answers)`. Esclusività check su batch tool calls (multi-ask_user coalesce, altri tool drop). |
| `internal/db/queries/paused_states.sql` | ~70 | **6 query sqlc**: `InsertPausedState`, `GetByToken`, `ListPendingForLoop` (ordered), `MarkResumed`, `MarkResumedBatch`, `CleanupResumedOlderThan` (per future retention). |
| `internal/agent/tools/ask_user_test.go` | ~80 | Args validation, options polimorfismo, priority cap, sentinel error format. |
| `internal/agent/loop_pause_test.go` | ~160 | Pause+Resume singolo, ResumeBatch, multi-pause coalesce, priority sort, esclusività intra-turn, invalid token rejection, stub Responder. |
| `cmd/aura/main.go` (diff) | ~+60 | `aura chat` gestisce loop pause: stampa via `askuser.CLI`, raccoglie risposta (singolo/all/batch), chiama `Resume` o `ResumeBatch`. |

**Deferred-tool partition.** `ask_user` → **non-deferred** (sempre visibile, è infrastruttura del loop). Description corta (1 riga). Schema piccolo ma con `oneOf` per options polimorfico + `priority` int 0-100 — comunque sotto i 2 KiB.

**Open questions.**
1. **~~Persistent vs in-memory `PausedState`~~ → CHIUSA: persistent in Postgres da subito.**
   Slice 0.5 ha già lanciato il Postgres → `PausedState` vive in `aura.paused_states` table. Risolve crash-recovery, multi-istanza future-proof, audit trail di tutte le pause. Schema: `(token uuid pk, conversation_id uuid NOT NULL REFERENCES aura.conversations(id) ON DELETE CASCADE, question text NOT NULL, options jsonb, kind text NOT NULL CHECK (kind IN ('clarification','approval','choice')), priority int NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 100), resume_context jsonb, tool_call_id text, proxied_from_child_id uuid NULL, proxied_tool_call_id text NULL, created_at timestamptz NOT NULL DEFAULT now(), resumed_at timestamptz NULL, resumed_answer text NULL)`. Indice su `(conversation_id, resumed_at) WHERE resumed_at IS NULL` per scan O(log n) della lista pending attive. `priority` + `proxied_*` aggiunti da Area #4 closed 2026-05-28 (multi-pause FIFO). `conversation_id` (era `loop_id`) e `resumed_answer` aggiunti da Slice 1.8 closed 2026-05-28 (#15 multi-conversation persistence).
   Migration: `0003_paused_states.up.sql` aggiunta in Slice 1.5.
   File targets aggiunti: `internal/db/queries/paused_states.sql` (~50 LOC, 4 query: insert/get-by-token/mark-resumed/cleanup-stale) + generated code via sqlc. `internal/agent/pending.go` (~70 LOC) usa il client sqlc invece di in-memory map. Test integrazione sotto build tag `db_integration`.
2. **~~Quante pending ask simultanee per Loop?~~ → CHIUSA: lista FIFO piena (Area #4 closed 2026-05-28).**
   Pattern industriale verificato: LangGraph 1.2 (maggio 2026) supporta multi-interrupt mappato per ID; Temporal supporta signal-based concurrent. Singleton (pattern OpenAI Agents SDK handoff) serializza la concurrency e contraddice Slice 3 `SpawnInteractive`.
   *Decisione:* multiple `PausedState` per `conversation_id` sono permessi. Loop pausato finché esiste ≥1 `PausedState` con `resumed_at IS NULL`. Ordering: `priority DESC, created_at ASC` (priority è hint dall'agente, default 0, cap 100). Un child `SpawnInteractive` che emette `ask_user` accoda una nuova `PausedState` al parent con `proxied_from_child_id` + `proxied_tool_call_id`. N child possono pausare simultaneamente. Reject di una pending proxied: il parent risponde "reject" → `Coordinator.ResumeChild(child_id, "reject")` → il child decide se procedere o cancellarsi (no forced kill).
   Riferimenti: [LangGraph interrupts docs](https://docs.langchain.com/oss/python/langgraph/interrupts), [Temporal signals](https://docs.temporal.io/workflow-execution).
3. **Timeout sulla pausa?** Se l'utente non risponde mai, il loop rimane Paused indefinitamente? → *Default proposto: no timeout di default. Caller (CLI/Telegram) decide se forzare un timeout esterno. Aggiungere timeout dentro Loop = stato terzo (timed_out) che complica la state machine senza beneficio reale.*

**Commit message template.**
```
slice 1.5: ask_user primitive — loop pause + resume + multi-pause FIFO

Implements the ask_user tool (non-deferred) and the corresponding
Loop pause/resume state machine. Tool returns ErrAwaitingUserInput
sentinel (+ Priority field, order_hint from agent). Loop.Turn returns
(reply, pending []*PausedState, error) — lista FIFO ordered by
(priority DESC, created_at ASC). Loop gains Resume(token, answer) for
singles and ResumeBatch(answers map) for multi-resolve. Exclusive batching
intra-turn: if the LLM emits ask_user alongside other tool calls in one
turn, ask_user wins and the others drop. Multiple ask_user in same turn
coalesce as separate PausedState rows (multi-pause pattern LangGraph
verified). PausedState persisted in aura.paused_states with new columns:
priority int, proxied_from_child_id uuid nullable, proxied_tool_call_id
text nullable. CLI responder renders ALL pending numbered + supports
'1: ans' single / 'all: ans' apply-all / 'batch: 1=a, 2=b' multi syntax.

Smoke: aura chat triggers ask_user with kind=approval, user answers,
loop resumes and produces final reply.

Prerequisite for: skill governance C (Slice 7), scheduler high-cost
confirmation (Slice 6), swarm spawn-depth approval (Slice 3).

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Slice 1.7 — Identity minimal (single-user `local` + capability grants)

**Goal.** Single source of truth per identity e capability del single-user
`'local'`, e base estendibile per multi-user futuro. Tabelle `aura.identities`
+ `aura.capability_grants` con seed via migration + CLI `aura identity
{list|get|grant|revoke}`. Sblocca Slice 7c per usare `actor_id` FK su
`aura.skill_audit` invece di un campo `text` opaco, eliminando l'ambiguità
"chi ha approvato la skill X" nei log.

Separata da Slice 1.5 per disciplina: 1.5 introduce `paused_states` (state
machine del Loop), 1.7 introduce identity (autorizzazione). Concern diverse,
commit separati. Far landing 1.5 prima permette di validare il pattern
`PausedState + sqlc` su un caso semplice prima di estenderlo a
identity + capability.

**Smoke.**
```bash
./aura db migrate                              # applica 0004_identity
./aura identity list                           # mostra 1 riga: local (system)
./aura identity get local                      # capabilities=['*']
./aura identity grant local memory.user.write  # idempotente (gia' coperto da '*')
./aura identity revoke local memory.user.write # esplicitamente toglie (no-op se non era grant esplicito)
./aura identity grant local memory.user.write
./aura identity revoke local '*'               # ERRORE: wildcard system-managed
```

**Acceptance.**
- [ ] Migration `0004_identity.up.sql`:
  ```sql
  CREATE TABLE aura.identities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL UNIQUE,
    kind text NOT NULL CHECK (kind IN ('system','user','channel','service')),
    created_at timestamptz NOT NULL DEFAULT now()
  );
  CREATE TABLE aura.capability_grants (
    identity_id uuid REFERENCES aura.identities(id) ON DELETE CASCADE,
    capability text NOT NULL,
    granted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (identity_id, capability)
  );
  INSERT INTO aura.identities (id, name, kind)
    VALUES ('00000000-0000-0000-0000-000000000001', 'local', 'system')
    ON CONFLICT (name) DO NOTHING;
  INSERT INTO aura.capability_grants (identity_id, capability)
    VALUES ('00000000-0000-0000-0000-000000000001', '*')
    ON CONFLICT DO NOTHING;
  ```
  Seed UUID fisso `0...001` permette FK stabili da `skill_audit.actor_id` senza lookup runtime.
- [ ] `HasCapability(ctx, identityID uuid, cap string) (bool, error)` legge da `aura.capability_grants`. Wildcard `'*'` = grant universale (match qualsiasi capability). Capability name regex `^[a-z][a-z0-9._-]{0,63}$` (no `'*'` per grant non-wildcard).
- [ ] CLI `aura identity {list | get <name> | grant <name> <cap> | revoke <name> <cap>}`:
  - `list`: tabella `name | kind | created_at | capabilities_count`.
  - `get <name>`: dettaglio + lista capability.
  - `grant <name> <cap>`: insert idempotente; fallisce con messaggio chiaro se `cap='*'` (wildcard è system-managed, modifica solo via migration).
  - `revoke <name> <cap>`: delete idempotente (no-op se grant non esiste); fallisce con messaggio chiaro se `cap='*'`.
- [ ] Test integrazione build tag `db_integration`: round-trip su identities, grant/revoke idempotenti, wildcard rejection, FK cascade su delete identity.

**File targets** (~280 LOC src + ~100 test):
| Path | LOC | Note |
|---|---|---|
| `internal/identity/types.go` | ~40 | `Identity{ID uuid, Name, Kind, CreatedAt}`, `Kind` enum. |
| `internal/identity/store.go` | ~80 | Thin adapter su sqlc `Queries`. Helper `LocalIdentityID() uuid` (constante UUID seed). |
| `internal/identity/capability.go` | ~60 | `HasCapability(ctx, id, cap) (bool, error)` con wildcard logic + regex validation per `cap`. |
| `internal/db/queries/identities.sql` | ~60 | **6 query sqlc**: `GetIdentityByName`, `ListIdentities`, `InsertIdentity`, `GrantCapability`, `RevokeCapability`, `ListCapabilities`. |
| `internal/db/migrations/0004_identity.up.sql` | ~50 | Tabelle + seed + check constraint. |
| `internal/db/migrations/0004_identity.down.sql` | ~5 | `DROP TABLE aura.capability_grants; DROP TABLE aura.identities;` |
| `internal/identity/store_test.go` | ~100 | Build tag `db_integration`. |
| `cmd/aura/main.go` (diff) | ~+60 | Sub-command `aura identity {list|get|grant|revoke}`. |

**Deferred-tool partition.** Niente tool LLM-facing in questo slice. Identity è infra interna; l'agente non grant/revoke direttamente capability (sarebbe self-elevation rischiosa). Se in futuro serve, atterra come tool `identity_grant` deferred + `ask_user` approval gate, fuori scope qui.

**Open questions.**
1. **Wildcard `'*'` interpretazione: match-all vs trigger-default?** → *Decisione*: match-all. `HasCapability(id, 'memory.user.write')` ritorna true se l'identity ha grant `'*'` OPPURE grant `'memory.user.write'`. Niente pattern-glob (`'memory.*'`) per ora — semplifica logica, atterrerà con multi-user.
2. **Seed UUID fisso vs random?** → *Decisione*: fisso `'00000000-0000-0000-0000-000000000001'`. Permette FK stabili da `skill_audit.actor_id` senza runtime lookup. Idempotenza su `ON CONFLICT DO NOTHING`.
3. **Audit delle modifiche a `capability_grants`?** → *Decisione*: NO in questo slice. Per single-user `'local'` con wildcard, audit è premature. Atterrerà con multi-user (tabella `aura.identity_audit` separata).

**Commit message template.**
```
slice 1.7: identity minimal (single-user 'local' + capability grants)

Two tables aura.identities + aura.capability_grants with seeded 'local'
identity (fixed UUID 0...001) and '*' wildcard grant. HasCapability(id,
cap) lookup via sqlc with wildcard match-all semantics. CLI aura identity
{list|get|grant|revoke}, wildcard '*' rejected on grant/revoke (system-
managed). Unblocks Slice 7c skill_audit.actor_id FK.

Smoke: aura db migrate -> aura identity list shows 'local' with '*'.
aura identity grant local foo + revoke local foo idempotent.

Prerequisite for: Slice 7c skill_audit FK, future multi-user/auth slices.

LOC: +XXX src / +YY test / +ZZ migration.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Slice 1.8 — Conversation persistence (multi-thread Claude.ai-style)

**Goal.** Aura passa da single-session in-memory a multi-conversation persistente.
L'utente può aprire N conversazioni separate (`aura chat new`), riprendere quelle
vecchie (`aura chat resume <id>`), e la history (turns + tool calls + tool results +
usage tokens) sopravvive ai restart. Sblocca Area #7 (auto-resolve di pending stale
quando il Loop di una conversation si chiude) e Area #15 (multi-conversation come
feature prodotto). Pattern derivato da LangGraph PostgresSaver verificato per
production, schema per-message (1 row per message) come raccomandato da AWS data
modeling per AI chatbots e best-practice schema design per scale + analytics.

**Out of scope rimosso:** la riga "Persistenza disk dello stato conversazionale (Loop
è in-memory)" della sezione Out-of-scope è rimossa da Slice 1.8. Lo stato Loop
resta in-memory durante esecuzione, ma viene ricostruito dalla history Postgres
al `aura chat resume <id>`.

**Smoke.**
```bash
./aura db migrate                      # applica 0005_conversations
./aura chat                            # nuova conversation, salva ogni turn
> ciao
> dimmi 2+2
^D                                     # exit, conversation salvata

./aura chat list                       # mostra 1 conversation (titled "Saluto + calcolo" o equivalente)
./aura chat resume                     # riprende l'ultima
> ora in inglese
^D

./aura chat new                        # forza nuova conversation parallela
> totalmente altro tema
^D

./aura chat list                       # 2 conversations, ordered last_active_at DESC
./aura chat archive <id>               # status='archived', non in list di default
./aura chat list --archived            # mostra anche archived
./aura chat delete <id> --confirm      # cascade delete turns + paused_states
```

**Acceptance.**
- [ ] Migration `0005_conversations.up.sql`:
  ```sql
  CREATE TABLE aura.conversations (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title           text,
    identity_id     uuid NOT NULL REFERENCES aura.identities(id),
    created_at      timestamptz NOT NULL DEFAULT now(),
    last_active_at  timestamptz NOT NULL DEFAULT now(),
    status          text NOT NULL DEFAULT 'active'
                      CHECK (status IN ('active','archived','deleted')),
    model           text,
    total_input_tokens  bigint NOT NULL DEFAULT 0,
    total_output_tokens bigint NOT NULL DEFAULT 0,
    total_cached_tokens bigint NOT NULL DEFAULT 0,
    total_cost_usd      numeric(10,4) NOT NULL DEFAULT 0,
    metadata        jsonb
  );
  CREATE INDEX conversation_active_by_identity
    ON aura.conversations (identity_id, last_active_at DESC)
    WHERE status = 'active';

  CREATE TABLE aura.conversation_turns (
    conversation_id uuid NOT NULL REFERENCES aura.conversations(id) ON DELETE CASCADE,
    seq             int NOT NULL,
    role            text NOT NULL CHECK (role IN ('system','user','assistant','tool')),
    content         text,
    content_sidecar_path text NULL,
    tool_call_id    text NULL,
    tool_calls      jsonb NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    input_tokens    int NULL,
    output_tokens   int NULL,
    cached_tokens   int NULL,
    PRIMARY KEY (conversation_id, seq)
  );
  ```
- [ ] `paused_states.loop_id` rinominato a `conversation_id`, FK `aura.conversations(id) ON DELETE CASCADE`. Migration drop+recreate column (table è ancora vuota allo step 1.8). Aggiunge colonna `resumed_answer text NULL` (era persa nel resume_context jsonb, ora esplicita per query).
- [ ] CLI `aura chat`:
  - `aura chat` (no args): se esiste conversation con `last_active_at` < 5min → resume, altrimenti new. Comportamento Claude.ai-like.
  - `aura chat new` (esplicito): sempre crea nuova conversation.
  - `aura chat list [--archived] [--limit N]`: tabella `id | title | created_at | last_active_at | turns | total_cost_usd`.
  - `aura chat resume <id|prefix>`: ricarica history nel Loop, prosegue dal turn successivo.
  - `aura chat resume` (no id): ultima conversation `last_active_at DESC LIMIT 1`.
  - `aura chat archive <id>` / `unarchive <id>`: toggle status.
  - `aura chat delete <id> --confirm`: hard delete (cascade turns + paused).
  - `aura chat rename <id> "<title>"`: manual title set.
- [ ] **Auto-title LLM-generated**: dopo `seq >= 3` (1 user + 1 assistant turn completo + magari tool turns), trigger 1 LLM call separata via `openai_compat.Client.Generate(prompt="Generate a 4-6 word title for this chat", messages=first_3_turns)` → `UPDATE conversations SET title=:t WHERE id=:id AND title IS NULL`. Idempotente (no-op se title già settato). Atomica (transazione separata, errore non blocca chat). Best-effort: se LLM call fallisce, title resta NULL (CLI mostra `(untitled <created_at>)`).
- [ ] Per-turn write atomico: il Loop scrive `conversation_turns` per ogni messaggio (system al primo turn, user/assistant/tool ad ogni step). Transazione singola per turn, `BEGIN; INSERT turn; UPDATE conversations SET last_active_at=now(), total_*_tokens+=..., total_cost_usd+=...; COMMIT;`.
- [ ] Content cap: se `len(content) > AURA_CONVERSATION_TURN_CAP_BYTES` (default `65536` = 64 KiB), scrivi a `$AURA_RUN_DIR/conversations/<conv_id>/<seq>.content` (sidecar) e set `content_sidecar_path`, `content=NULL`. Riusa pattern Slice 1 ToolResult preview+persist (Caps & Limits sez.).
- [ ] Resume contract: `aura chat resume <id>` ricostruisce `Loop.Messages` da `SELECT * FROM conversation_turns WHERE conversation_id=:id ORDER BY seq`. Per ogni row: build `Message{Role, Content, ToolCallID, ToolCalls}`. Loop in-memory ricreato byte-identico (modulo `tool_calls` jsonb deserialization).
- [ ] **Loop.Stop() auto-resolve (chiude Area #7 Caso 2)**: quando un Loop termina cleanly (status `completed` / `errored` / `interrupted_by_user`) chiama:
  ```sql
  UPDATE aura.paused_states
     SET resumed_at = now(),
         resumed_answer = '<auto-terminated: conversation ended>'
   WHERE conversation_id = :id AND resumed_at IS NULL;
  ```
  Audit chiaro: nessuna pending orphan in DB visibile.
- [ ] **`$AURA_RUN_DIR` cleanup cascade (Area #9 closed 2026-05-28)**:
  - `aura chat delete <id>`: dopo COMMIT del DELETE in Postgres, esegui `os.RemoveAll($AURA_RUN_DIR/conversations/<id>/)`. Se rm fallisce → log warning + emit Notifier "FS orphan: <path> requires manual cleanup". Boot orphan scan recupera al prossimo restart.
  - **Boot orphan scan**: al boot Aura legge tutti i `conv_id` da `aura.conversations`, lista `$AURA_RUN_DIR/conversations/*`, ogni dir senza riga corrispondente → `os.RemoveAll` + log. Idempotente, sweep singolo al boot, non cron.
  - **`tmp/` TTL 24h**: al boot, `$AURA_RUN_DIR/tmp/*` con `mtime < now() - 24h` → rm. Coerente con pattern Slice 7c skill pending TTL.
  - **WARN size**: al boot, se `du -sb $AURA_RUN_DIR > AURA_RUN_DIR_WARN_THRESHOLD_BYTES` (default 1 GiB) → log warning + Notifier "$AURA_RUN_DIR is N MiB, consider `aura chat archive` or `aura chat delete` to free space". No auto-purge (audit-only).
- [ ] **CLI `aura paused-states {list|purge}` (chiude Area #7 escape hatch)**:
  - `aura paused-states list [--conversation-id X] [--include-resumed]`: tabella delle pending.
  - `aura paused-states purge --before <ISO-date> --confirm`: hard delete delle resumed più vecchie di N (skill_audit FK è `ON DELETE SET NULL`, audit log resta consistente).
- [ ] Identity scoping: ogni conversation è scoped a un identity (oggi sempre `'local'`). `aura chat list` filtra `WHERE identity_id = LocalIdentityID()`. Future multi-user: filtra su identity autenticato.
- [ ] Test integrazione `db_integration`: nuova chat → 3 turn → restart processo → resume → assistant vede full history.
- [ ] Test auto-title: stub LLM client → conv con 3 turn → trigger generation → title set.
- [ ] Test cascade delete: insert conv + 5 turns + 2 paused_states → delete conv → tutto purgato.

**File targets** (~520 LOC src + ~180 test):
| Path | LOC | Note |
|---|---|---|
| `internal/conversations/types.go` | ~50 | `Conversation{ID, Title, IdentityID, CreatedAt, LastActiveAt, Status, Model, TokenStats}`, `Turn{ConvID, Seq, Role, Content, ToolCallID, ToolCalls, CreatedAt, *Tokens}`. |
| `internal/conversations/store.go` | ~120 | Thin adapter su sqlc. `Create`, `Get`, `List`, `Archive`, `Delete`, `AppendTurn`, `LoadHistory`, `UpdateStats`, `SetTitle`. Atomic transaction per AppendTurn (insert + UPDATE conversations.last_active_at + token aggregates). |
| `internal/conversations/title.go` | ~60 | Auto-title LLM call. `GenerateTitle(ctx, client, firstTurns) (string, error)`. Best-effort, no panic. Background goroutine kick-off da `AppendTurn` quando `seq` cross 3. |
| `internal/conversations/sidecar.go` | ~40 | Helper per content > 64 KiB: write `$AURA_RUN_DIR/conversations/<conv_id>/<seq>.content`, read back on resume. |
| `internal/conversations/cleanup.go` | ~70 | Boot orphan scan (`$AURA_RUN_DIR/conversations/*` vs DB `conv_id` set), `tmp/` TTL 24h sweep, `du -sb` size check + WARN. Cascade rm su `DeleteConversation(id)`. |
| `internal/db/queries/conversations.sql` | ~80 | **8 query sqlc**: `CreateConversation`, `GetConversation`, `ListConversations`, `AppendTurn`, `LoadTurns`, `UpdateLastActive`, `UpdateStatus`, `UpdateStats`. |
| `internal/db/queries/conversation_turns.sql` | ~30 | **2 query sqlc**: `InsertTurn`, `ListTurnsForConv` (ORDER BY seq). |
| `internal/db/migrations/0005_conversations.up.sql` | ~70 | Tabelle + index + rename paused_states.loop_id → conversation_id + aggiunta resumed_answer col. |
| `internal/db/migrations/0005_conversations.down.sql` | ~10 | DROP tables + rename back + drop col. |
| `internal/agent/loop.go` (diff) | ~+80 / -20 | `Loop.AppendMessage(msg)` ora persiste via `store.AppendTurn` invece di solo in-memory append. `Loop.Stop()` chiama `store.AutoResolvePendings(convID)`. `Loop.NewFromHistory(convID)` ricostruisce dalle turns. |
| `internal/conversations/store_test.go` | ~120 | Build tag `db_integration`. Round-trip + resume + cascade + auto-title. |
| `cmd/aura/main.go` (diff) | ~+90 | `aura chat {list|resume|new|archive|unarchive|delete|rename}` + `aura paused-states {list|purge}`. |

**Deferred-tool partition.** Niente tool LLM-facing in questo slice. È infra CLI + persistence. Future: tool `conversation_search`/`conversation_summarize` come deferred, scope dedicato.

**Open questions.**
1. **Cosa succede al `system` message?** → *Decisione*: il `system` message è il primo turn (seq=1, role='system'). Generato dal `PromptBuilder` (Slice 4) al primo turn. Su `resume`, ricaricato as-is. Se il system prompt cambia tra una session e l'altra (es. nuova skill installata), il vecchio system message della conv già esistente resta intatto — coerenza temporale. Future "system upgrade" è scope di una slice dedicata.
2. **Cache KV cross-conversation?** → *Decisione*: lascio a Slice 4 (KV cache) la valutazione. Stable-prefix di Slice 4 è system + tool manifest, che è identico cross-conv (se non cambiano le skill) → cache hit automatico. Niente refactor di Slice 4 per 1.8.
3. **Quanti turn conservare per il modello al resume?** → *Default proposto*: TUTTI fino a `AURA_MAX_HISTORY_TOKENS` (env, default 100k). Sopra, autocompact: tronca i turn più vecchi tranne system + 5 most recent + paused_states pending. Autocompact è scope di Slice 1.8b futura (la slice 1.8 base resume tutta la history senza compact).
4. **Concurrent write a stessa conversation?** → *Default proposto*: PRIMARY KEY `(conversation_id, seq)` + `last_active_at` lock via `SELECT ... FOR UPDATE` in `AppendTurn` previene race. Multi-session sulla stessa conv (raro per single-user) ottengono conflitto chiaro `unique_violation`.

**Mini-PC RAM budget.** Postgres conversations + turns + indici: ~50 MB per 100k turns. Trascurabile. Nessun servizio in più.

**Commit message template.**
```
slice 1.8: conversation persistence (closes grey areas #7 + #15)

PostgreSQL-backed multi-conversation support Claude.ai-style. Tables
aura.conversations (id, title, identity_id, status, model, token stats)
+ aura.conversation_turns (conv_id, seq, role, content, tool_call_id,
tool_calls, *_tokens) with per-message granularity (LangGraph
PostgresSaver pattern + AWS data modeling best-practice for AI chat).
Migration 0005_conversations renames paused_states.loop_id ->
conversation_id with FK ON DELETE CASCADE and adds resumed_answer
column for query-friendly audit.

CLI multi-conv: aura chat {list|resume|new|archive|delete|rename}.
Auto-title LLM-generated after seq>=3, best-effort background. Per-turn
atomic write (BEGIN; INSERT turn; UPDATE last_active_at + token aggregates;
COMMIT). Content >64 KiB spillover to $AURA_RUN_DIR sidecar (reuses
Slice 1 ToolResult pattern). Resume reconstructs Loop.Messages byte-
identical from history.

Closes grey area #7: Loop.Stop() auto-resolves pending paused_states
to 'auto-terminated' on conversation end. Adds CLI escape hatch
aura paused-states {list|purge} for manual cleanup.

Closes grey area #15: multi-conversation persistence as product feature
(Claude.ai-style threading). Out of scope row "Persistenza disk dello
stato conversazionale (Loop e' in-memory)" removed. Migrations
renumbered: 0006_scheduler (was 0005), 0007_skill_audit (was 0006).

Sequencing rationale bullet 2.ter added.

LOC: +XXX src / +YY test / +ZZ migration.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Slice 2 — Sandbox runner (Docker sidecar + seccomp + ulimit)

**Goal.** Implementare `sandbox.Runner` reale (in [internal/sandbox/sandbox.go](internal/sandbox/sandbox.go)
oggi è `Stub`) come HTTP client verso un sidecar Docker isolato. Esporre il
runtime al modello come tool `execute` (Deferred=true) che accetta `lang ∈ {python, shell}` + `code` e restituisce stdout/stderr/exit_code/elapsed_ms.

**Smoke.**

*Isolato (bypass agent loop, test del runner direttamente):*
```bash
docker compose -f sandbox/compose.yaml up -d
./aura exec python "print(2+2)"     # → 4
./aura exec shell  "echo hello"     # → hello
./aura exec python "import socket; socket.socket().connect(('1.1.1.1', 80))"
                                    # → exit_code != 0, stderr contains EPERM/network-denied
```

*E2E (modello sceglie il tool da solo — vedi §Test discipline):*
```bash
./aura chat "quanto fa 2 alla 64 meno 1? rispondi solo col numero"
# → modello deve invocare `execute` (python), ricevere 18446744073709551615, risponderlo
./aura chat "che giorno della settimana era il 14 luglio 1789?"
# → modello deve invocare `execute` (python con datetime), rispondere "martedì"
```
Nessuno dei due prompt nomina `execute`. Se il modello non lo invoca, è bug del system prompt o del manifest, NON del test.

**Acceptance.**
- [ ] Sidecar gira come container non-root (`uid:gid 65532:65532`), `read_only: true`, `tmpfs:/tmp`, `network_mode: none`, ulimit nofile=64, cpus=1.0, mem=512m.
- [ ] Seccomp profile rifiuta: `socket`, `connect`, `bind`, `mount`, `unshare`, `ptrace`, `clone(CLONE_NEWNET)`. Profile sotto VCS in `sandbox/seccomp.json`.
- [ ] Default timeout esecuzione 30s, override via `AURA_SANDBOX_TIMEOUT_SEC` (cap 600s — ricorda [aura_lan_exposure_2026-05-17](memory)).
- [ ] `aura exec` chiamato senza sidecar su → errore chiaro, no panic, no hang.
- [ ] Container exit con stdout/stderr troncati a 1 MiB ciascuno (oltre → `... [truncated]`).
- [ ] Test integrazione marcato `//go:build sandbox_integration` salta se sidecar non raggiungibile (no flaky CI).

**File targets** (≤ 600 LOC Go + Dockerfile/compose/seccomp materials):
| Path | LOC | Note |
|---|---|---|
| `internal/sandbox/docker.go` | ~220 | `DockerRunner` impl `Runner`. HTTP POST a sidecar `/exec/python` e `/exec/shell`. Timeout, truncate, ctx-cancel. |
| `internal/sandbox/config.go` | ~50 | Env: `AURA_SANDBOX_URL` (default `http://127.0.0.1:18901`), `AURA_SANDBOX_TIMEOUT_SEC`. |
| `internal/sandbox/docker_test.go` | ~120 | Integration test sotto build-tag `sandbox_integration`. |
| `internal/agent/tools/execute.go` | ~140 | Tool `execute` con `Deferred: true`. Schema: `{lang: enum, code: string, timeout_sec?: int}`. Delega a `sandbox.Runner`. |
| `sandbox/Dockerfile` | ~30 | `FROM python:3.12-slim` + apt: bash, coreutils. USER non-root. ENTRYPOINT sidecar. |
| `sandbox/sidecar.py` | ~150 | Server HTTP minimo (stdlib `http.server`) con 2 endpoint. `subprocess.run` con timeout. Trunc stdout/stderr. Niente deps. |
| `sandbox/seccomp.json` | ~80 | Default-deny + allow-list syscall syscall, blocca network/mount/ptrace. |
| `sandbox/compose.yaml` | ~25 | Service `aura-sandbox`: build sandbox/, security_opt seccomp, network none, read_only, ulimits. |
| `cmd/aura/main.go` (diff) | ~+60 | Subcommand `aura exec <lang> <code>` + registrazione del tool `execute` nel registry. |

**Deferred-tool partition.** `execute` → **Deferred=true** (description lunga + schema enum + esempi safety). `tool_search` lo carica on-demand.

**Open questions.**
1. **Sidecar implementation language.** Python (zero-build, leggi+rispondi) o Go (single binary, no Python runtime in container)? → *Proposto: Python stdlib. Il sidecar è 1 file, niente deps, niente compile step, container minimo.*
2. **State tra exec.** Il sidecar è stateless (ogni call = subprocess fresco) o mantiene una REPL persistente? → *Proposto: stateless. REPL persistente è un'ottimizzazione futura (Slice X), oggi è complessità non richiesta.*
3. **Filesystem out.** Il modello deve poter scrivere file persistenti? → *Proposto: NO in questo slice. Solo tmpfs effimero. Filesystem condiviso = slice separato (workspace mount).*
4. **Windows host.** Docker Desktop su Windows è OK ma seccomp è solo Linux-container. Sviluppo locale Windows + container Linux → OK. Sviluppo locale Windows nativo (no Docker)? → *Proposto: no-fallback. Aura runs in container or against a Docker sidecar. Punto.*

**Commit message template.**
```
slice 2: sandbox runner — Docker sidecar with seccomp + ulimit + no-net

Implements sandbox.Runner against an isolated Python sidecar
(read-only rootfs, tmpfs /tmp, network_mode none, seccomp default-deny,
ulimit nofile=64, cpus=1.0, mem=512m). Exposes `execute` tool
(deferred) and `aura exec` CLI for smoke.

Smoke:
  aura exec python "print(2+2)" → 4
  aura exec python "import socket; socket.socket().connect(...)" → EPERM

LOC: +XXX src / +YY test / +ZZ sidecar+infra.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Slice 3 — Swarm coordinator (bus + DM-by-ID + tier model)

**Goal.** Implementare `swarm.Coordinator` reale (oggi `Stub` in [internal/swarm/swarm.go](internal/swarm/swarm.go))
con: spawn di agenti figli (tier `chat|reasoning|worker`), shared message bus
con broadcast E DM-by-ID, `Join(id)` che blocca fino a final report del figlio,
MAX_SPAWN_DEPTH=3 enforced, payload summarizer al return-to-parent.

**Smoke.**

*Isolato (unit-style del coordinator, parent hardcoded):*
```bash
./aura swarm-demo
# parent (id=root) spawns 3 workers (w1,w2,w3) con goal diversi
# parent broadcasts "go" sul bus → ognuno reagisce
# parent DM-a-w2 "switch to plan B"
# parent join(w1), join(w2), join(w3) → riceve 3 final report sintetizzati
# stdout deve mostrare timeline ordinata: spawn(3) → broadcast → dm → join(3)
```

*E2E (modello sceglie swarm da solo — vedi §Test discipline):*
```bash
./aura chat "trovami in parallelo: (1) il PIL italiano del 2023, (2) la capitale dell'Australia, (3) l'autore de I Promessi Sposi. Quando hai tutte e tre, rispondi in tre righe."
# → modello deve spawn 3 worker, join, aggregare. Nessuna menzione di "swarm" nel prompt.
./aura chat "leggi questi tre file in d:/tmp: dante.html, collodi.html, paper.md, e dimmi quale è più lungo in righe"
# → modello deve parallelizzare 3 read+wc invece di serializzare. Test fallisce se serializza (timing assertion).
```

**Acceptance.**
- [ ] `coordinator.Spawn(req)` con `Depth >= MaxSpawnDepth` → errore `spawn depth exceeded` (test).
- [ ] `coordinator.Talk(from, "broadcast", msg)` recapita a tutti tranne `from`. `Talk(from, "<id>", msg)` recapita solo a `<id>`. Test asserisce delivery.
- [ ] `coordinator.Join(id)` blocca finché il figlio non chiama `text_response` (terminale dell'agent loop) e ne restituisce il payload (summarizzato se >2 KiB).
- [ ] Payload summarizer triggera sopra `AURA_CONTEXT_PREVIEW_CAP_BYTES` (default 2048): tronca + appende `... [N bytes truncated, M total]`.
- [ ] Tier → model mapping in `tier.go`: `chat→<AURA_SWARM_MODEL_CHAT>`, `reasoning→<...REASONING>`, `worker→<...WORKER>`. Default tutti = env `AURA_LLM_MODEL`.
- [ ] Goroutine leak test (`go test -race`): dopo `Join` di tutti i figli, `runtime.NumGoroutine()` torna al baseline ±2.
- [ ] Bus capacity bounded (channel buf 64); over-flow blocca producer con timeout **60s** + errore `bus backpressure` (audit round 2 P0: 5s era sotto la latency LLM first-token tipica 10-30s → producer in mezzo a `runTool` riceveva errore spurio durante LLM warmup).

**File targets** (≤ 800 LOC):
| Path | LOC | Note |
|---|---|---|
| `internal/swarm/coordinator.go` | ~240 | `LiveCoordinator` impl `Coordinator`. Gestisce children map (id→agent), depth check, lifecycle. |
| `internal/swarm/bus.go` | ~140 | Shared bus: `subscribe(id) <-chan Envelope`, `publish(from, to, body)`. `to=="broadcast"` fan-out. |
| `internal/swarm/tier.go` | ~70 | `TierConfig{Chat, Reasoning, Worker string}`. `ModelFor(tier) string`. |
| `internal/swarm/payload.go` | ~60 | `Summarize(b []byte, cap int) string`. Strategia: tronca a `cap`, appendi nota. |
| `internal/swarm/coordinator_test.go` | ~150 | Spawn-depth, broadcast, DM, Join, goroutine-leak, bus backpressure. |
| `internal/agent/tools/swarm_spawn.go` | ~70 | Deferred=true. Args: `{tier, goal}`. Returns `{id}`. |
| `internal/agent/tools/swarm_talk.go` | ~50 | Deferred=true. Args: `{to_id, message}` (use "broadcast" for all). |
| `internal/agent/tools/swarm_join.go` | ~50 | Deferred=true. Args: `{id}`. Blocking. |
| `cmd/aura/main.go` (diff) | ~+30 | `aura swarm-demo` subcommand. |

**Deferred-tool partition.** Tutti e tre i tool swarm → **Deferred=true** (description con safety constraints + schema con enum tier + esempi).

**Open questions.**
1. **Spawn = nuova istanza `agent.Loop`?** Sì, child è una `Loop` indipendente con system prompt parametrizzato dal `goal`, condivide il `Coordinator` per talk-back. → *Conferma proposta: SÌ. Niente "agent-as-tool"; child è un loop reale.*
2. **Children share tools registry?** Child eredita tutti i tool del parent, MA il parent può passare un filtro (whitelist) → primo slice: ereditarietà piena, no filter. → *Proposto: full inherit. Filtro è slice futura.*
3. **Persistent state.** I figli scrivono in un store comune (Neo4j MCP)? → *Proposto: NO in questo slice. Coordinator è in-memory. Persistenza è una concern ortogonale.*
4. **Cancellazione.** Se il parent termina (Loop.Turn returns), i figli devono ricevere ctx-cancel? → *Proposto: SÌ. `Coordinator` ha un `parentCtx`; quando si chiude tutti i child loops droppano.*
5. **Child pause propagation (audit round 1 P0).** Cosa succede se un child Loop ritorna `*PausedState` (perché ha invocato `ask_user`)?
   → *Decisione*: ogni child Loop spawn-ato dal Coordinator ha un proprio `Responder` configurabile.
   - **Default per swarm spawn**: `RejectingResponder` — il child non può pausare. Se chiama `ask_user`, il Loop riceve immediatamente `answer="<auto-rejected: child loop has no human responder>"`. Il child decide se procedere comunque o terminare.
   - **Override esplicito**: il parent può chiamare `Coordinator.SpawnInteractive(req, parentResponder)` per propagare la pausa al parent. Il parent ottiene una nuova `PausedState` con `proxied_from_child_id=<child_id>` + `proxied_tool_call_id=<child_original_tool_call>`. La pending si **accoda** alle eventuali altre pending del parent (lista FIFO con priority, Area #4 closed 2026-05-28): N child con `SpawnInteractive` in pausa simultanea sono permessi e contribuiscono ciascuno una PausedState alla lista del parent.
   - **`Coordinator.ResumeChild(childID, answer)`**: nuovo metodo. Necessario quando `SpawnInteractive` è usato e il parent ha raccolto la risposta dell'utente. Quando il parent risolve una pending proxied (anche con `answer="reject"`), il sistema chiama `Coordinator.ResumeChild(child_id, answer)` — il child Loop riceve la risposta via il proprio `Responder`, decide se procedere o cancellarsi (**no forced cancellation**: rispetta autonomy del child, audit logged). **Children map mutex-protected (audit round 2 P0):** ResumeChild + Spawn + Join condividono `sync.RWMutex` su `children map[string]*childState` — race su N child paused simultaneously rifiutata strutturalmente. Test `go test -race` + assertion N=10 child interactive paralleli paused+resumed senza data race.
   - **Acceptance addizionale**: test che assert deadlock guard — child spawned senza `Interactive` flag + child chiama ask_user → child termina con `answer=auto-reject` entro 100ms, parent `Join` non blocca.
   - **Acceptance multi-pause**: test con 3 child `SpawnInteractive` che pausano simultaneamente → parent ottiene `len(pending)==3` in `Loop.Turn`, sortate per priority+FIFO; `Loop.ResumeBatch` con 3 risposte (incluso 1 `reject`) → 3 `Coordinator.ResumeChild` invocazioni, ciascun child decide procedere/cancellare, parent riprende solo dopo che tutti i RoleTool sono accodati.

**Commit message template.**
```
slice 3: swarm coordinator — spawn, bus broadcast+DM, tier model

Implements swarm.Coordinator with in-memory child registry, shared
bus (channel-buffered, backpressure-bounded), tier→model mapping,
payload summarizer at return-to-parent, MAX_SPAWN_DEPTH=3 enforced.
Exposes swarm.spawn / swarm.talk / swarm.join tools (deferred).

Smoke: aura swarm-demo spawns 3 workers, broadcasts, DMs, joins all.

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Slice 4 — KV cache builder (stable-prefix + provider-aware)

**Goal.** Estrarre la costruzione del prompt da `agent.Loop.Turn` in un
`PromptBuilder` dedicato (`internal/llm/prompt.go`) che garantisca:
- `messages[0]` (system) byte-identico turn-su-turn;
- ordering deterministico del tool manifest (già: alphabetical in [manifest.go](internal/agent/tools/manifest.go));
- inserimento dei breakpoint di cache provider-specifici (Anthropic `cache_control: ephemeral` sui blocchi system+tools, DeepSeek auto-cache che è no-op lato client ma misura il hit-rate dal `usage`);
- misurazione `cache_hit_rate` per turn, esposta via subcommand `aura cache-stats`.

**Smoke.**
```bash
./aura chat-loop   # REPL 5 turn back-to-back
> ciao
> dimmi una poesia su pollo arrosto
> riscrivila in inglese
> ora in giapponese
> grazie

./aura cache-stats
# turn 1: 0.00 hit, 230 prompt_tokens
# turn 2: 0.78 hit, 245 prompt_tokens (191 cached)
# turn 3: 0.81 hit, 310 prompt_tokens (252 cached)
# ...
# avg hit-rate: 0.74
```

**Acceptance.**
- [ ] `PromptBuilder.Build(history, tools)` ritorna `[]llm.Message` con `messages[0]` byte-identico tra turn (test: hash SHA-256 di `json.Marshal(messages[0])` costante su 5 turn consecutivi).
- [ ] Per provider Anthropic: il system message e il tool manifest portano `cache_control: {"type":"ephemeral"}` (fixture wire test).
- [ ] Per provider DeepSeek: nessun cache_control (DeepSeek auto-cache); il client parsa `usage.prompt_cache_hit_tokens` dalla response.
- [ ] `cache.Tracker` aggrega per turn → `aura cache-stats` stampa hit-rate per turn + media sessione.
- [ ] Test invariante: `agent.Loop.Turn(ctx, "anything")` chiamato 5 volte → `messages[0]` identico, lunghezza monotona crescente di history (no in-place mutation di entries precedenti).
- [ ] Test invariante: il tool manifest renderizzato 5 volte di seguito è byte-identico (cache poisoning guard, link a [reference_aura_cache_poisoning_sites_2026-05-27](memory) — prefix `reference_` corretto post-audit round 1).

**File targets** (≤ 400 LOC):
| Path | LOC | Note |
|---|---|---|
| `internal/llm/prompt.go` | ~140 | `PromptBuilder` con `Build(history, tools, provider) []Message`. Stable-prefix discipline. |
| `internal/llm/cache_anthropic.go` | ~70 | Inietta `cache_control: ephemeral` su system + tool manifest. |
| `internal/llm/cache_deepseek.go` | ~50 | No-op + parse `usage.prompt_cache_hit_tokens` dalla response. |
| `internal/llm/cache_metrics.go` | ~80 | `Tracker.Record(turn, promptTokens, cachedTokens)` + `Report() string`. |
| `internal/llm/prompt_test.go` | ~100 | Invariant test su 5-turn (hash stability, monotonic growth, no-mutation, cache_control presence). |
| `internal/agent/loop.go` (diff) | ~-15 / +10 | Sostituisce inline `llm.Request{Messages: l.Messages, ...}` con `l.Prompt.Build(...)`. Loop ora prende un `*PromptBuilder` invece di costruire il request inline. |
| `internal/llm/openai_compat.go` (diff) | ~+25 | Parsa `usage.prompt_cache_hit_tokens` dal final chunk SSE e lo espone su un campo `Chunk.Usage`. |
| `cmd/aura/main.go` (diff) | ~+40 | `aura chat-loop` (REPL multi-turn) + `aura cache-stats`. |

**Deferred-tool partition.** Nessun tool nuovo. Pure plumbing interno + misurazione.

**Open questions.**
1. **~~Provider detection~~ → CHIUSA: provider primario = OpenRouter (vedi Slice 1 OQ1).**
   OpenRouter è OpenAI-compat ma fa pass-through verso il modello sottostante (DeepSeek v4 flash :exacto). Le ottimizzazioni cache:
   - **Anthropic `cache_control: ephemeral`** non si applica (DeepSeek non lo supporta).
   - **DeepSeek auto-cache nativo** è preservato anche via OpenRouter: la response include `usage.prompt_cache_hit_tokens` (formato OpenAI-compat esteso). Il parser deve gestirlo come optional field.
   - **OpenRouter aggiunge `usage.cost`** (USD totale della call) — utile da loggare in `cache.Tracker` accanto al hit-rate per misurare ROI reale del KV-discipline.
   *Decisione:* `Config.LLM.Provider="openrouter"` è una stringa documentale (per log/UI). Il client OpenAI-compat è invariante. Le routine cache_control restano in `cache_anthropic.go` (no-op in pratica) per non rompere se in futuro aggiungiamo un secondo provider Anthropic-diretto.
2. **Cache stats persistenza.** Solo in-memory per processo, o flush su file? → *Proposto: in-memory. Stats sono debug, non telemetria.*
3. **Tools come breakpoint cache separato.** Su Anthropic il tools array è un blocco a parte (non dentro messages). Dobbiamo modificare `llm.Request` per supportare un `tools_cache_control`? → *Proposto: SÌ. Aggiunta a `Request{ ToolsCacheControl string }`. OpenAI/DeepSeek lo ignorano.*
4. **Threshold cache_hit_rate per CI.** Test che fallisce sotto X%? → *Proposto: NO. Test asserisce **invariant** (byte-identity), non **percentage** (provider-dipendente, flaky).*

**Commit message template.**
```
slice 4: KV cache builder — stable-prefix prompt + provider-aware cache_control

Extracts prompt construction from agent.Loop into PromptBuilder.
Guarantees byte-identical messages[0] across turns and stable tool
manifest ordering (cache-friendly). Injects Anthropic ephemeral
cache_control on system + tools. Parses DeepSeek prompt_cache_hit
from usage. Exposes `aura cache-stats` for measurement.

Smoke: 5-turn REPL shows monotone hit-rate >0.7 from turn 2 onward.

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## §Test discipline (vale per tutti e 4 gli slice)

Regola fondamentale, applicabile a ogni test E2E che esercita l'agent loop
contro un modello reale:

**Un test E2E è valido SE e SOLO SE il prompt sembra qualcosa che un utente
vero scriverebbe.** Niente "use the X tool", niente "call swarm.spawn",
niente "execute python code that...". Mai citare per nome:
- un tool del registry (`execute`, `text_response`, `swarm.spawn`, ecc.)
- una skill o un overlay
- una funzione interna o un modulo Go
- la parola "tool" stessa, salvo che sia parte naturale della domanda
  ("che tool useresti per X?" è una domanda meta valida; "use the execute
  tool to..." è asilo nido).

**Perché.** Un prompt che nomina il tool by-name testa solo che il dispatcher
funzioni, cosa già coperta dai unit test del registry. Un prompt naturale
testa l'intero pipeline: system prompt → manifest visibility → modello sceglie
il tool giusto → tool funziona → risposta finale è sensata. Se quel pipeline
si rompe (manifest poisonato, system prompt che nasconde il tool, schema con
description ambigua), un test "naturale" lo cattura mentre un test "asilo"
no. Cfr [feedback_agent_must_know_tools_exist](memory).

**Pattern.**

| Tipo test | Esempio bad (asilo) | Esempio good (reale) |
|---|---|---|
| LLM client (slice 1) | "say hello using text_response" | "ciao, dimmi 2+2 in tre parole" |
| Sandbox tool (slice 2) | "use the execute tool to print 4" | "quanto fa 2 alla 64 meno 1?" |
| Swarm (slice 3) | "spawn a worker with goal=foo" | "trovami in parallelo PIL Italia 2023, capitale Australia, autore Promessi Sposi" |
| Cache (slice 4) | "trigger ephemeral cache_control on system" | turn-by-turn REPL su un argomento conversazionale (poesia, ricetta, traduzione) |

**Eccezioni esplicite.** I unit test interni (SSE fixture parser, Coordinator
unit test, PromptBuilder invariant test) NON sono tenuti a questa regola
perché non passano dal modello — testano direttamente la primitiva Go.
La regola si applica ai test che chiamano `agent.Loop.Turn(ctx, userText)`.

**Cosa testare invece del prompt-by-name.**
- L'artefatto, non la reply (memo: [feedback_probe_must_verify_artifact_not_reply](memory)).
  Per `execute`: assert che il subprocess abbia girato (log/event), non solo
  che la reply contenga "4". Per swarm: assert che `Coordinator.children` abbia
  3 entry, non solo che la reply menzioni "PIL".
- Timing (per swarm parallelismo): wall-clock(3 worker paralleli) < 1.5 ×
  wall-clock(1 worker singolo), altrimenti il modello ha serializzato.
- Side-effect provider (per cache): `usage.prompt_cache_hit_tokens > 0`
  dal turn 2 in poi.

**Failure mode da evitare.** Test che passa perché la reply *contiene la
stringa attesa* mentre dietro le quinte il tool non è mai stato chiamato
(es. il modello ha hallucinato "18446744073709551615" senza eseguire
python). Soluzione: hook nel `tools.Registry` che logga ogni `Execute`
con `tool_name + args_hash + duration`. Il test asserisce **(reply matches
expected) AND (tool was invoked)**. Mai solo il primo.

---

---

# Backlog post 4-slice — riprendere e riscrivere bene

I tre sottosistemi che seguono **funzionavano già** nel pre-rewrite (git tag
`pre-rewrite-2026-05-27`). Vanno riportati ma "scritti meglio": stesso
contratto LLM-visible, architettura interna pulita, no god class. Pianificati
come slice 5/6/7, ordinati per indipendenza dalle 4 fondamentali (più
indipendente prima).

## Slice 5 — Web tools (web_search + web_fetch via SearXNG)

**Goal.** Riportare due tool LLM-facing:
- `web_search({query, max_results?, category?, language?, time_range?}) → {results: [{title, url, snippet}]}`
- `web_fetch({url}) → {title, content_md, links}`

Entrambi alimentati da un container SearXNG self-hosted (estensione del
`sandbox/compose.yaml` di Slice 2). HTML→Markdown via `go-shiori/go-readability`
+ `JohannesKaufmann/html-to-markdown/v2`. SSRF defense custom (DNS resolution
+ private-IP block) preservata dal pre-rewrite.

**Pre-rewrite reference** (git tag `pre-rewrite-2026-05-27`):
- [internal/agent/tools/registry/search.go](git:pre-rewrite-2026-05-27/internal/agent/tools/registry/search.go) — 562 LOC GOD CLASS (12 action branches, va smontato)
- [internal/agent/tools/registry/searxng.go](git:pre-rewrite-2026-05-27/internal/agent/tools/registry/searxng.go) — 123 LOC client puro (OK, da riportare quasi 1:1)
- [internal/agent/tools/registry/direct_fetch.go](git:pre-rewrite-2026-05-27/internal/agent/tools/registry/direct_fetch.go) — 474 LOC, monster `fetch()` function inline (200+ LOC) da splittare

**Smoke E2E (vedi §Test discipline — prompt reali, no nomi tool):**
```bash
./aura chat "qual è la versione più recente di Go al 2026?"
# → modello sceglie web_search, poi eventualmente web_fetch sulla golang.org page, risponde col numero
./aura chat "leggi https://en.wikipedia.org/wiki/Pasta_carbonara e dimmi gli ingredienti originali in 5 punti"
# → modello chiama web_fetch direttamente (URL esplicita), parsa markdown, estrae lista
```

**Acceptance.**
- [ ] `SEARXNG_URL` non settato → tool ritornano errore strutturato `"web_search unavailable: SEARXNG_URL not configured"` (no panic).
- [ ] **SSRF defense espansa (audit round 1 P0)** — blocklist enumerata:
  - **IPv4**: loopback `127.0.0.0/8`, private `10.0.0.0/8` `172.16.0.0/12` `192.168.0.0/16`, link-local `169.254.0.0/16`, CGNAT `100.64.0.0/10`, multicast `224.0.0.0/4`, broadcast `255.255.255.255/32`, "this network" `0.0.0.0/8`, cloud metadata `169.254.169.254`.
  - **IPv6**: loopback `::1/128`, link-local `fe80::/10`, ULA `fc00::/7`, multicast `ff00::/8`, IPv4-mapped `::ffff:0:0/96` (per evitare bypass via `::ffff:127.0.0.1`), discard `100::/64`, documentation `2001:db8::/32`, cloud metadata `fd00:ec2::254`.
  - Override solo via env: `AURA_WEB_FETCH_ALLOW_LOOPBACK=1` (dev), `AURA_WEB_FETCH_ALLOW_HOSTS=host1,host2` (ops bypass mirato).
- [ ] **SSRF defense: DNS-rebinding protection** — `safeDialContext` risolve host → valida IP contro blocklist → dial esplicito su IP risolto, NON re-lookup tra resolve e dial.
- [ ] **SSRF defense: HTTP redirect interception (audit round 1 P0)** — `http.Client.CheckRedirect` custom che ri-valida ogni Location header contro la blocklist. Test: `web_fetch("https://safe.example.com/r")` che ridirige a `http://169.254.169.254/` → rifiutato al redirect step, NON al primo dial.
- [ ] Response cap: `AURA_WEB_RESPONSE_CAP_BYTES = 24000` (era hardcoded `maxWebToolChars`, promosso a env in Area #8 closed 2026-05-28). Hard limit anti-DOS della response HTTP prima di qualunque preview/sidecar logic. Distinto da `AURA_CONTEXT_PREVIEW_CAP_BYTES` (preview/sidecar threshold) per semantica.
- [ ] Timeout HTTP: SearXNG 20s, direct_fetch 30s, entrambi config-overrideable.
- [ ] Readability filter: pagine con <250 char di main content → ritornano `{warning: "low-content page"}` invece di noise.
- [ ] **Riusa il `ToolResult` pattern di Slice 1**: web_fetch su page grande (>2 KiB) → preview + sidecar file; modello può fare `read_tool_output(id, offset, limit)` per estrarre fette.

**File targets** (≤ 550 LOC src, no file >300):
| Path | LOC | Note |
|---|---|---|
| `internal/web/searxng.go` | ~130 | Client HTTP puro: `Query(ctx, params) ([]Result, error)`. Da pre-rewrite quasi 1:1. |
| `internal/web/fetcher.go` | ~120 | `Fetch(ctx, url) (Page, error)`. HTTP con `safeDialContext` SSRF defense. |
| `internal/web/html.go` | ~90 | `ExtractMarkdown(html []byte) (title, contentMD string, links []Link)`. Wrapper su go-readability + html-to-markdown. |
| `internal/web/config.go` | ~40 | `Config{SearXNGURL, FetchTimeoutSec, SearchTimeoutSec, MaxChars, AllowLoopback, AllowHosts []string}`. Lette da `internal/config` (esteso). |
| `internal/agent/tools/web_search.go` | ~70 | Deferred. Args→`web.SearXNG.Query`→`ToolResult`. No business logic qui, è un thin adapter. |
| `internal/agent/tools/web_fetch.go` | ~80 | Deferred. Args→`web.Fetcher.Fetch`→`ExtractMarkdown`→`ToolResult`. |
| `internal/web/searxng_test.go` | ~80 | Fixture JSON SearXNG response → test parser. |
| `internal/web/fetcher_test.go` | ~100 | SSRF tests (loopback/private IP rejected), readability filter. |
| `sandbox/compose.yaml` (diff) | ~+15 | Aggiunge service `searxng` (image `searxng/searxng`), shared network col sandbox. |
| `cmd/aura/main.go` (diff) | ~+15 | Registra i due tool. |

**Cosa NON riportare dal pre-rewrite.**
- L'aggregazione `SearchTool` con 12 azioni (search/list/read/lessons/user_facts/god_nodes/subgraph/path/diff/gaps/surprises/suggest_questions). Quelle 10 azioni "memory/wiki" appartengono a una slice futura sulla wiki/graph layer (post-MCP-Neo4j), non a web tools. Slice 5 = SOLO web_search + web_fetch.
- Il pattern action-enum singolo-tool con `oneOf` schema. Per due tool indipendenti due tool separati sono più chiari.

**Commit message template.**

```
slice 5: web tools (web_search + web_fetch via SearXNG)

Two LLM-facing deferred tools backed by self-hosted SearXNG container
(extends sandbox/compose.yaml). SSRF defense enumerated (IPv4 private/
loopback/link-local/CGNAT/metadata + IPv6 ULA/link-local/IPv4-mapped/
metadata) + HTTP redirect interception. Readability filter 250-char
threshold. ToolResult preview+persist for large pages. Reuses pre-rewrite
SearXNG client + safeDialContext SSRF pattern; drops SearchTool god class
(12 actions) which belongs to future wiki/graph slice.

Smoke: aura chat "qual è la versione più recente di Go al 2026?" → modello
sceglie web_search → web_fetch su golang.org → estrae numero.

LOC: +XXX src / +YY test / +ZZ infra.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

**Open questions.**
1. **SearXNG self-hosted vs cloud (DuckDuckGo Lite/Brave/Tavily)?** Pre-rewrite era self-hosted. Trade-off: self-host = zero-cost + privacy ma serve container; cloud = $$ + nessun container ma instradi le query a terzi. → *Default proposto: self-hosted (continuità + privacy). Cloud come fallback se `SEARXNG_URL` non risolve dopo N tentativi → out of scope qui.*
2. **`go-readability` mantenuto?** È stato fork-ato lentamente; alternative: `dom-distiller` port, scraping minimal manuale. → *Default proposto: SÌ go-readability + html-to-markdown/v2, sono stabili e il pre-rewrite li usava bene.*

---

## Slice 6 — Scheduler (cron + agent jobs persistente)

> **Atomicity note (audit round 1 P0):** ~1300 LOC distribuiti = troppo per 1 commit.
> Si committa in **2 sub-slice ordinati**:
> - **6a**: types + migration `0006_scheduler` + sqlc queries + store thin adapter + scheduler tick loop + Notifier interface + handler `reminder` + tool `task_list`/`task_cancel`. ~700 LOC. Funzionante end-to-end per reminder, base infra.
> - **6b**: handler `agent_job` (con swarm Coordinator integration) + tool `task_schedule`/`task_run_now` + `ActionRouter` helper (primo uso multi-action, vedi §Pattern condiviso). ~600 LOC.
> Ogni sub-slice atomic-commit, smoke green prima del successivo.

**Goal.** Riportare il tool `task` LLM-facing con azioni `schedule | list | cancel | run_now`,
supportato da un cron-core in-process (tick loop DIY ogni 30s, nessuna libreria
cron esterna) e un Repository persistente. Supporta `TaskKind` estensibile:
`reminder`, `agent_job` (extension futura per altri kind in slice dedicata).

**Pre-rewrite reference**:
- [internal/agent/tools/registry/scheduler.go](git:pre-rewrite-2026-05-27/internal/agent/tools/registry/scheduler.go) — 587 LOC tool wrapper (LARGE, da splittare per azione)
- [internal/cron/scheduler.go](git:pre-rewrite-2026-05-27/internal/cron/scheduler.go) — 255 LOC tick loop (OK)
- [internal/cron/store.go](git:pre-rewrite-2026-05-27/internal/cron/store.go) — **594 LOC GOD CLASS** (Upsert+Cancel+Delete+DueTasks+MarkFired+RecordManualRun+RecordAgentJobResult tutti in un file)
- [internal/cron/dispatch.go + dispatch_handlers.go](git:pre-rewrite-2026-05-27/internal/cron/dispatch.go) — 244+246 LOC con 5 `dispatchXxx` privati (`dispatchReminder`, `dispatchWikiMaintenance`, `dispatchLessonPromotion`, `dispatchProposalTTLSweep`, `dispatchMemoryDecay`) + 2 arm inline nel `Dispatch` switch (`BackupVerify`, `WALCheckpoint`). Tutto senza strategy pattern (verificato pre-rewrite tag, round 1 reality check)

**Smoke E2E.**
```bash
./aura chat "ricordami fra 10 minuti di controllare il forno"
# → modello chiama task.schedule(kind=reminder, in=10m, payload={text: ...})
# → 10 min dopo: il dispatcher chiama Notifier → utente riceve notifica (CLI/Telegram)

./aura chat "ogni giorno alle 9 del mattino fai un riassunto degli articoli che ho letto ieri"
# → modello chiama task.schedule(kind=agent_job, daily=09:00, payload={goal: ..., toolsets: [wiki, web]})
# → il dispatcher invoca l'agent loop come sub-job con goal serializzato
```

**Acceptance.**
- [ ] `MinScheduleEveryMinutes` configurabile (default 5, era hardcoded pre-rewrite — fix).
- [ ] Validation: `daily HH:MM` accetta solo `00:00`–`23:59`; nomi task `^[a-zA-Z0-9_-]+$` (no path traversal).
- [ ] Persistence: i task sopravvivono al restart del processo. Tick loop riprende `DueTasks` allo startup e cattura up i missed run (con flag `MissedSince`).
- [ ] **`DueTasks` query usa `SELECT ... FOR UPDATE SKIP LOCKED` (audit round 1 P0)**: pattern atomico per multi-instance safety. Anche con 1 sola Aura attiva oggi, blocca double-dispatch se un manual `task.run_now` parte contemporaneo al tick. Costo zero, sblocca future multi-instance.
- [ ] Dispatcher: ogni `TaskKind` ha un `Handler` separato in `internal/cron/handlers/<kind>.go` implementando `Handle(ctx, *Task) error`, `MaxDuration() time.Duration`, `ReschedulesOnRecovery() bool`. `MaxDuration` definisce la soglia oltre la quale un run senza `finished_at` è considerato in limbo al restart. `ReschedulesOnRecovery=true` (reminder, idempotenti) → il task viene riportato in `DueTasks` al boot; `=false` (agent_job, side-effecting) → limbo audit-only, no auto re-run. Aggiungere un nuovo kind = aggiungere 1 file, non editare un god switch.
- [ ] **`agent_job` handler auto-rejecta child Loop pause (audit round 1 P0)**: il sub-loop spawn-ato via `swarm.Coordinator.Spawn` riceve un `RejectingResponder` come default — un agent_job non può bloccare aspettando un umano (è cron, gira anche di notte). Se l'agent_job invoca `ask_user`, riceve risposta automatica `"<auto-rejected: scheduled job has no human responder>"` e decide come procedere. Test asserisce: cron job che invoca ask_user non blocca, completa in <30s con stato `auto-rejected`.
- [ ] `LastError` persistito su failure, leggibile via `task.list()`.
- [ ] **Risk-Based Governance (Area #5 closed 2026-05-28)**: ogni chiamata a `task.schedule` calcola `tier = scoring.ComputeTaskTier(args)` via modulo condiviso `internal/scoring/` (vedi §Risk-Based Governance). Se `tier in {RISKY, DESTRUCTIVE}` il task viene creato con `status='pending_approval'` (non gira), il tool result include `{task_id, risk_tier, gate_recommended:true, status:'pending_approval'}`. L'agente decide autonomamente se ri-emettere `ask_user(kind=approval, ResumeContext={action:'approve', task_id})` o procedere silente. Se silente: Notifier IMMEDIATE alert all'utente (tier ≥ RISKY trigger threshold). Tutti i campi audit (`computed_risk_tier`, `gate_recommended`, `gate_taken`) registrati in `agent_job_runs`.
- [ ] Nuovo tool `task.approve(task_id)` (deferred): chiama `Queries.ApproveTask(id)` → `UPDATE scheduler_tasks SET status='active' WHERE status='pending_approval'`. Idempotente, no-op su task già attivi. Usato dal resume handler dell'ask_user approval.
- [ ] `scheduler_tasks.status` enum: `'active' | 'paused' | 'cancelled' | 'pending_approval'`. Tick loop `DueTasks` filtra `WHERE status='active'` (task in pending_approval mai dispatchati).
- [ ] **Crash recovery boot query (Area #3 closed 2026-05-28)**: il tick loop, **prima** del primo tick, esegue:
  ```sql
  UPDATE aura.agent_job_runs
     SET exit_status   = 'unknown_recovery',
         finished_at   = now(),
         recovered_at  = now(),
         summary       = 'aura process restart, outcome unknown'
   WHERE finished_at IS NULL
     AND started_at  < now() - <handler.MaxDuration()>
  RETURNING id, task_id;
  ```
  Per ogni riga RETURNING, se `Handler.ReschedulesOnRecovery() == true` il task viene riportato in `DueTasks` via `UPDATE aura.scheduler_tasks SET next_run_at=now() WHERE id=<task_id>`; altrimenti resta in limbo audit-only (no auto re-run, side-effect safe). Decisione: **mai ri-eseguire automaticamente un job con side effect committati** (pattern HKUDS/nanobot `cron/service.py` verificato).
- [ ] **Recovery notifier**: se la query touch ≥1 riga, `Notifier.Notify(local, "N agent_job interrotti dal restart, audit via aura task audit")` al boot. Stdout in CLI ora, Telegram quando atterra.
- [ ] Test: schedule → restart processo → tick loop ripicka il task → handler invocato.
- [ ] Test crash recovery: insert manual `agent_job_runs(started_at=now()-1h, finished_at=NULL)` → boot Aura → query marca `exit_status='unknown_recovery'`, Notifier emette msg, task NON ri-eseguito automaticamente.

**File targets** (≤ 950 LOC src — risparmio ~550 LOC vs pre-rewrite grazie a sqlc):
| Path | LOC | Note |
|---|---|---|
| `internal/cron/types.go` | ~100 | `Task`, `TaskKind`, `ScheduleKind`, `Status` enums (Go domain types, distinti dai sqlc-generated). |
| `internal/cron/scheduler.go` | ~240 | Tick loop, lifecycle (Start/Stop), missed-run recovery, crash-recovery boot query (chiama `MarkRunsRecovered` prima del primo tick, itera RETURNING per reschedule selettivo, notifica utente). Usa `*cron.Queries` (sqlc client). |
| `internal/cron/store.go` | ~80 | Thin adapter: domain `Task` ↔ sqlc rows. Trasforma enum string ↔ tipo Go. |
| `internal/db/queries/scheduler_tasks.sql` | ~120 | **8 query sqlc**: `UpsertTask`, `GetByName`, `ListTasks`, `DueTasks`, `MarkFired`, `CancelTask`, `DeleteTask`, `RecordRunResult`. Una query per concept, anti-god-class. |
| `internal/db/queries/agent_job_runs.sql` | ~80 | **4 query sqlc**: `RecordManualRun`, `RecordAgentJobResult`, `ListRuns`, `MarkRunsRecovered`. `MarkRunsRecovered` è la boot recovery query (UPDATE finished_at IS NULL AND started_at < threshold → exit_status='unknown_recovery'). RETURNING task_id per il reschedule loop. |
| `internal/db/migrations/0006_scheduler.up.sql` | ~85 | `CREATE TABLE aura.scheduler_tasks` (id, name unique, kind, schedule_kind, schedule_payload jsonb, next_run_at, `status text NOT NULL CHECK (status IN ('active','paused','cancelled','pending_approval'))`, last_error, created_at, updated_at). `CREATE TABLE aura.agent_job_runs` (id, task_id fk, started_at, finished_at, `exit_status text NOT NULL DEFAULT 'running' CHECK (exit_status IN ('running','completed','failed','cancelled','timeout','unknown_recovery'))`, `recovered_at timestamptz NULL`, `computed_risk_tier text NOT NULL DEFAULT 'normal' CHECK (computed_risk_tier IN ('safe','normal','risky','destructive'))`, `gate_recommended boolean NOT NULL DEFAULT false`, `gate_taken boolean NOT NULL DEFAULT false`, summary text, tokens jsonb). Indici su `next_run_at WHERE status='active'` (scheduler_tasks) e su `(exit_status, started_at) WHERE finished_at IS NULL` (boot recovery scan). |
| `internal/db/migrations/0006_scheduler.down.sql` | ~5 | DROP TABLEs. |
| `internal/cron/handlers/handler.go` | ~40 | Interface `Handler{ Kind() TaskKind; Handle(ctx, t *Task) error }` + registry. |
| `internal/cron/handlers/reminder.go` | ~85 | Notifier-driven (CLI/Telegram). `MaxDuration=30s`, `ReschedulesOnRecovery=true` (idempotente: ri-notificare "controlla il forno" è safe). |
| `internal/cron/handlers/agent_job.go` | ~160 | Spawn `agent.Loop` via Slice 3 swarm `Coordinator.Spawn`. `MaxDuration=600s` (default, override via task payload), `ReschedulesOnRecovery=false` (side-effect committati non ricostruibili). |
| ~~`internal/cron/handlers/graph_maintenance.go`~~ | — | **RIMOSSO (audit round 1 P0)**: scope-creep esplicito (placeholder per future), va in slice dedicata post-MCP-Neo4j. Slice 6 supporta solo `reminder` e `agent_job` per ora. |

**Commit message templates (sub-slice 6a + 6b):**

```
slice 6a: scheduler infrastructure — types + sqlc + tick loop + reminder handler

PostgreSQL-backed scheduler base. Types (Task/TaskKind/ScheduleKind/Status),
migration 0006_scheduler (tables scheduler_tasks + agent_job_runs with
exit_status check constraint (running|completed|failed|cancelled|timeout|
unknown_recovery), recovered_at nullable column, indices on next_run_at
WHERE status='active' and on (exit_status, started_at) WHERE finished_at
IS NULL, FOR UPDATE SKIP LOCKED on DueTasks). 12 sqlc queries split across
scheduler_tasks.sql + agent_job_runs.sql (incl. MarkRunsRecovered boot
query). Tick loop DIY 30s with missed-run recovery and crash recovery
(boot query marks finished_at IS NULL && started_at < MaxDuration as
unknown_recovery; reschedules only handlers with ReschedulesOnRecovery=true,
i.e. reminders; agent_job stays audit-only). Handler interface gains
MaxDuration() + ReschedulesOnRecovery(). Notifier interface + stdout impl
emits boot summary if >=1 row recovered. Handlers registry + reminder
handler (MaxDuration=30s, reschedules=true). Tools task_list/task_cancel.

Smoke: aura schedule reminder in=10m → tick loop fires → Notifier stdout.
Persistent across restart verified.

LOC: +XXX src / +YY test / +ZZ migration.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

```
slice 6b: scheduler agent_job + ActionRouter helper

Adds agent_job handler that delegates to swarm.Coordinator.Spawn
(RejectingResponder default → cron jobs auto-reject child Loop pauses,
deadlock-guarded). Tools task_schedule/task_run_now (deferred). ActionRouter
helper introduced in internal/agent/tools/action.go (~90 LOC) with sentinel
passthrough contract — first multi-action consumer is `task`. Slice 7 reuses.

Smoke: aura chat "ogni giorno alle 9 fai un riassunto degli articoli che ho
letto ieri" → task_schedule → handler agent_job → swarm child → join.

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```
| `internal/agent/tools/task_schedule.go` | ~110 | Deferred. Args validation + delega a `Queries.UpsertTask`. Chiama `scoring.ComputeTaskTier(args)` post-validation; se RISKY/DESTRUCTIVE setta `status='pending_approval'` e Notifier IMMEDIATE alert. Include `risk_tier` + `gate_recommended` nel tool result. |
| `internal/agent/tools/task_list.go` | ~50 | Non-deferred. Mostra anche task `pending_approval` con annotazione `[awaiting approval]`. |
| `internal/agent/tools/task_cancel.go` | ~40 | Deferred. |
| `internal/agent/tools/task_run_now.go` | ~80 | Deferred. Chiama Handler.Handle direttamente, bypass tick. **Refuse task in pending_approval** (errore strutturato chiede `task.approve` prima). |
| `internal/agent/tools/task_approve.go` | ~50 | Deferred. Args `{task_id}`. `UPDATE scheduler_tasks SET status='active' WHERE id=:id AND status='pending_approval' RETURNING id`. Idempotente, no-op se già active. Audit `gate_taken=true`. |
| `internal/scoring/scoring.go` | ~100 | Modulo condiviso. `RiskTier` enum (safe/normal/risky/destructive), `ComputeTaskTier(args)`, `ComputeSkillTier(action, body)`, `GateRecommended(tier)`, `RequiresImmediateAlert(tier)`. Mapping kind→tier hard-coded + modificatori (frequency, silent, payload regex destructive keywords). |
| `internal/scoring/scoring_test.go` | ~80 | Test esaustivo del mapping per ogni kind + tutti i modificatori. Niente DB. |
| `internal/cron/scheduler_test.go` | ~150 | Build tag `db_integration`. Round-trip + tick loop + missed-run recovery + crash recovery + risk_tier=RISKY → pending_approval. |
| `internal/cron/handlers/agent_job_test.go` | ~100 | Goal serialization + child loop spawn (mocked Coordinator). |

**Open questions.**
1. **~~CONFLITTO con CLAUDE.md "Persistence: Neo4j via MCP"~~ → CHIUSA: Postgres come terzo pilastro.**
   Slice 0.5 ha introdotto Postgres come application-state store. Architettura persistence finale:
   - **Neo4j (via MCP)** = knowledge graph + vector index. **Unica fonte di knowledge E di vector embeddings.** Semantic memory, entity graph, conversational memory, derivati, embedding HNSW nativo (Apache Lucene). Embedder: **`embeddinggemma` via sidecar `aura-llama-embed`** (porto 8081, OpenAI-compat), **768d nativo** scritti direttamente su `:Chunk.embedding` (vector index Neo4j configurato a 768d, no MRL truncation — Slice 0.7 closed 2026-05-28). NIENTE wiki markdown filesystem (deprecato 2026-05-27). NIENTE Qdrant: lo spike `D:/tmp/aura-neo4j-spike-2026-05-27/` Phase 6b ha misurato 22-30ms p95 + IT recall@5 5/5 su Neo4j vector index nativo con corpus reale Aura → Qdrant decommissionabile senza regressione. Container produzione `aura-neo4j` atterra in Slice 0.7 (rinominato da `neo4j-spike` dello spike).
   - **Postgres (via sqlc)** = application state. Scheduler tasks, paused states, audit log, identity, capability grants.
   - **Filesystem** = solo artefatti operativi (SKILL.md tree, tool result sidecar files). NESSUN knowledge in filesystem.
   Distinzione semantica chiara: **knowledge + vectors → solo Neo4j; infrastructure → Postgres; operational artifacts → filesystem**. CLAUDE.md aggiornata in coda al PRD.
2. **Notifier interface.** Slice 6 ha bisogno di un sink per le reminder. Tabula-rasa "non ha Telegram". → *Default proposto: interface `Notifier{ Notify(ctx, recipient, msg) error }` con default impl stdout (printf nel terminale `aura serve`). Telegram impl arriverà in una slice plugin separata.*
3. **Agent-job sub-loop = swarm child?** Lo Slice 3 swarm `Coordinator.Spawn` può servire come spawner per i `agent_job` schedulati. → *Default proposto: SÌ, agent_job handler chiama `swarm.Coordinator.Spawn` con tier="reasoning" e join sincrono. Evita di duplicare la logica spawn-loop.*

---

## Slice 7 — Skills (self-extension via ask_user governance)

> **Atomicity note (audit round 1 P0):** ~1400 LOC totali = troppo per 1 commit.
> Si committa in **4 sub-slice ordinati**, ognuno atomic + smoke green:
> - **7a**: types + loader (filesystem + parser + cache 4-way split) + `internal/skills/validator.go` (single chokepoint, riusato da tutti) + `internal/skills/paths.go` (`SanitizeName` single source) + tool `skill.list` + tool `skill.info`. Read-only, no governance. ~500 LOC.
> - **7b**: catalog (skills.sh HTML scrape 1:1 pre-rewrite) + tool `skill.catalog`. Read-only. ~350 LOC.
> - **7c**: writer (atomic pending→active) + deleter + tool `skill.create` + `skill.update` + `skill.delete` (mutation tools con ask_user governance). Schema `aura.skill_audit` (migration `0004`) + sqlc queries + adapter. ~600 LOC.
> - **7d**: installer (`npx skills add` con `--ignore-scripts` + sanitizedEnv stretto + post-install ParseSkill re-validation) + tool `skill.install`. ~450 LOC.
> Ogni sub-slice atomic-commit, smoke green prima del successivo.

> **Governance: Risk-Based (Area #5 closed 2026-05-28).** Tutte le mutation
> skill (`create`/`update`/`install`/`delete`) chiamano
> `scoring.ComputeSkillTier(action, body)` dal modulo condiviso
> `internal/scoring/`. Default mapping: `create`/`update`/`install` →
> `RISKY`, `delete` → `DESTRUCTIVE`. **Tutte le skill mutation oggi sono
> RISKY o DESTRUCTIVE** — il sistema crea/aggiorna la skill in
> `pending/<name>/` (mai `active/`) e ritorna `gate_recommended=true` al
> tool result. L'agente decide se ri-emettere `ask_user(kind=approval,
> ResumeContext={action:'activate', skill_name})` o procedere silente.
> Se silente: la skill RESTA in `pending/` (non viene caricata dal Loader),
> Notifier IMMEDIATE alert all'utente (`aura skills approve <name>` per
> attivare manualmente). Pattern uniformato con Slice 6 (cron `task.schedule`).
> Razionale: una skill scritta dall'agente entra nel system prompt del turn
> successivo (cache TTL 1s + `Invalidate()`) — è auto-modifica persistente.
> Future skill read-only (es. `SAFE`) potranno essere auto-promosse senza
> gate via la stessa pipeline; oggi non esistono.

**Goal.** Sette tool LLM-facing per la self-extension completa:
- Read-only: `skill.list`, `skill.catalog`, `skill.info`
- Mutations (tier RISKY/DESTRUCTIVE da `internal/scoring/`, atterrano in `pending/`, agente decide se gate via `ask_user`, Notifier alert se gate skipped): `skill.create`, `skill.update`, `skill.delete`, `skill.install`
- Approval helper (deferred): `skill.approve(name)` per attivare manualmente una skill in `pending/`

Alimentati da:
- Loader filesystem multi-root (TTL cache 1s, pattern pre-rewrite ben tarato).
- Catalog search via skills.sh — fetch HTTP del catalog HTML + regex parser ([catalog.go](git:pre-rewrite-2026-05-27/internal/skills/catalog.go) 1:1).
- Install via **`npx skills add <source> --agent claude-code -y`** ([admin.go](git:pre-rewrite-2026-05-27/internal/skills/admin.go) 1:1) — funzionava bene pre-rewrite, riusabile. **Migliorie di sicurezza**: `sanitizedEnv()` whitelist più stretta (drop `NPM_CONFIG_USERCONFIG` che può puntare a file arbitrari), post-install `ParseSkill()` re-validation prima di `Invalidate()` (catch corrupted downloads), 90s timeout esplicito.
- Writer in `~/.aura/skills/pending/<name>/` per le mutation in attesa di approval; al `Approva` move atomico in `~/.aura/skills/active/`.
- Audit log append-only in **Postgres** tabella `aura.skill_audit` di OGNI mutation con `{id, ts, actor_id (fk aura.identities), action, name, content_hash, source, approval_id, paused_state_token fk, computed_risk_tier, gate_recommended, gate_taken}`. Migrate `0007_skill_audit.up.sql` aggiunta in Slice 7. Query via sqlc `internal/db/queries/skill_audit.sql` (~45 LOC, 4 query: `RecordSkillMutation`, `ListAuditSince`, `GetByName`, `ListPendingApproval`).

**SKILL.md format** (preservato dal pre-rewrite, lievemente irrigidito):
```
---
name: aura-implementation-loop
description: One-line summary surfaced in manifest.
when_to_use: Optional structured trigger conditions.
tools: [optional, list, of, tool, names, this, skill, expects]
---

# Body markdown freeform
```
La sezione frontmatter cresce di un campo (`when_to_use`) per chiarire al
modello quando invocare la skill, riducendo invocazioni speculative.

**Pre-rewrite reference**:
- [internal/agent/tools/registry/skill.go](git:pre-rewrite-2026-05-27/internal/agent/tools/registry/skill.go) — 347 LOC (LARGE, da splittare per azione)
- [internal/skills/loader.go](git:pre-rewrite-2026-05-27/internal/skills/loader.go) — 273 LOC (mixing 4 concerns: FS scan + YAML parse + cache + name validation)
- [internal/skills/admin.go](git:pre-rewrite-2026-05-27/internal/skills/admin.go) — 326 LOC (installer + deleter + network — splittare)
- [internal/skills/catalog.go](git:pre-rewrite-2026-05-27/internal/skills/catalog.go) — 133 LOC (OK)

**Smoke E2E (prompt reali — vedi §Test discipline):**
```bash
./aura chat "fammi vedere quali competenze hai disponibili"
# → modello chiama skill.list (read-only, no ask_user). Riassume.

./aura chat "ho bisogno di un'analisi statistica avanzata. cerca su skills.sh"
# → modello chiama skill.catalog → trova 3 candidati
# → modello chiama ask_user(kind=choice, options=[3 skill candidati])
# → utente sceglie → skill.install → ask_user(kind=approval, "Confermi install di X?")
# → approve → install, Loader.Invalidate(), next turn la usa

./aura chat "scrivi una skill che ti faccia rispondere sempre in haiku"
# → modello chiama skill.create({name, description, body})
# → tool scrive ~/.aura/skills/pending/haiku-mode/SKILL.md (NON active)
# → tool ritorna ErrAwaitingUserInput(kind=approval, "Creare skill 'haiku-mode'? ...")
# → utente approva → move pending → active, Invalidate()
# → next turn: agente è in modalità haiku permanente fino a skill.delete

**Acceptance.**
- [ ] **SKILL.md format minimo**: frontmatter YAML obbligatorio (`name`, `description`), corpo non vuoto. `name` regex `^[a-z0-9-]+$`. **NO** `when_to_use:` o `tools:` field (rimossi dal design — la `description` ben scritta incorpora il when-to-use inline, pattern Anthropic-style confermato da `D:/tmp/assistant-ui/.claude/skills/tap/SKILL.md`). File invalidi loggati + skippati (no crash).
- [ ] Multi-root precedence: `~/.aura/skills/active/` override `internal/config/defaults/skills/`. Test asserisce override visibile in `list()`.
- [ ] TTL cache 1s preservato dal pre-rewrite.
- [ ] **Manifest packing — pattern Claude Code, non pre-rewrite**: TUTTE le skill listate nel manifest (anche 100+) ma con SOLO `name + description` (1 riga). Il body si carica on-demand via `skill.info`. Niente più `maxSkillsBlockChars` cap. Coerente col pattern deferred-tool di tutto il PRD.
- [ ] **Mutation flow (create/update/install/delete) Risk-Based (Area #5 closed 2026-05-28)**: il tool scrive in `pending/<name>/`, calcola `tier = scoring.ComputeSkillTier(action, body)` (oggi: create/update/install → RISKY, delete → DESTRUCTIVE). Ritorna tool result `{name, content_hash, risk_tier, gate_recommended:true, status:'pending'}`. **NON** ritorna automaticamente `ErrAwaitingUserInput`: l'agente decide se ri-emettere `ask_user(kind=approval, ResumeContext={action:'activate'|'install_active'|'delete_active', name})`. Resume handler: approve → `Writer.Activate()` (move atomico pending→active) + `Loader.Invalidate()` + audit `gate_taken=true`. Reject → delete pending + audit `gate_taken=true`. Se agente skippa gate: skill RESTA in `pending/` (non caricata), Notifier IMMEDIATE alert, audit `gate_taken=false`, l'utente attiva con `aura skills approve <name>` o ignora. Edit ("approva con modifiche"): out of scope Slice 7.
- [ ] **Audit log**: ogni mutation (anche le reject) scritte nella tabella `aura.skill_audit` di Postgres (sqlc-managed). `aura skills audit --since=1h` query SQL. Postgres trigger `BEFORE UPDATE/DELETE` su `skill_audit` → `RAISE EXCEPTION` (audit append-only enforced a livello DB, audit round 1 P1).
- [ ] **`Validator.SanitizeName` chokepoint (audit round 1 P0)**: `internal/skills/paths.go` espone `SanitizeName(name) (clean string, err error)` — UNICA via per derivare path filesystem da user input. Regex `^[a-z0-9-]+$`, length 1-64, no reserved (`init`, `delete`, `.`, `..`). Writer + Deleter + Installer DEVONO chiamarlo prima di `filepath.Join(skillsDir, name)`. Test asserisce ogni file-touch site via static analysis (`grep -L 'SanitizeName' internal/skills/{writer,deleter,installer}.go` → empty).
- [ ] **skills.sh integration via `npx skills add` (pre-rewrite 1:1 + safety hardening)**:
  - `node`+`npm` runtime requisito host.
  - Catalog browse: HTTP fetch + regex parse del catalog HTML (pre-rewrite pattern).
  - Install: subprocess `npx --yes skills add <source> --agent claude-code -y` (preservato) + **`--ignore-scripts` aggiunto (audit round 1 P0)** per bloccare `package.json` postinstall hooks (supply chain risk).
  - **90s timeout preservato dal pre-rewrite** (`skillInstallToolTimeout` già esistente, non nuovo).
  - sanitizedEnv whitelist stretta: drop `NPM_CONFIG_USERCONFIG` (può puntare a file arbitrari), drop `NPM_CONFIG_GLOBALCONFIG`, drop `NPM_CONFIG_PREFIX`.
  - **Acceptance**: install di una skill malevola con `postinstall: "rm -rf ~"` → `--ignore-scripts` la blocca, test asserisce.
- [ ] **Capability boundary open-by-default per single-user**: nel tabula-rasa Aura locale, l'identity seed `'local'` (Slice 1.7) ha capability grant `'*'` (wildcard) — l'agente può self-extend liberamente (gate-ato comunque da `ask_user`). Capability lookup via `aura.capability_grants` (sqlc), non hard-coded: struttura estendibile per future multi-user senza toccare il codice.
- [ ] `skill.info(name)` ritorna corpo intero come `ToolResult` — usa il pattern Slice 1 (preview + sidecar file se >2 KiB).
- [ ] **Prompt injection guard espanso (audit round 1 P0)**: body size cap `AURA_SKILL_BODY_CAP_BYTES` (default `32768` = 32 KiB, era hardcoded, promosso a env in Area #8 closed 2026-05-28) a write time. Refuse write se body contiene una di queste sequence (literal blocklist):
  - OpenAI ChatML: `<|im_start|>`, `<|im_end|>`, `<|endoftext|>`
  - Anthropic: `</system>`, `</human>`, `</assistant>`, `\n\nHuman:`, `\n\nAssistant:`
  - Llama / Mistral: `[INST]`, `[/INST]`, `<<SYS>>`, `<</SYS>>`
  - Meta / Llama 3: `<|begin_of_text|>`, `<|start_header_id|>`, `<|end_header_id|>`, `<|eot_id|>`
  - DeepSeek / Gemma / Qwen: `<|fim_begin|>`, `<|fim_hole|>`, `<start_of_turn>`, `<end_of_turn>`
  Basic literal check, no semantic detection. Lista refresh-abile via config (`AURA_SKILL_INJECTION_BLOCKLIST`).

**File targets** (≤ 1050 LOC src, no file >200):
| Path | LOC | Note |
|---|---|---|
| `internal/skills/types.go` | ~50 | `Skill{Name, Description, Body, Path}`. Frontmatter minimo. |
| `internal/skills/loader/filesystem.go` | ~100 | FS scan multi-root (active + defaults), `ReadDir` + walk. |
| `internal/skills/loader/parser.go` | ~80 | YAML frontmatter + body split + validation. |
| `internal/skills/loader/cache.go` | ~80 | sync.RWMutex + TTL 1s, `Invalidate()`. |
| `internal/skills/loader/loader.go` | ~60 | Coordina i tre: List/Get → cache → parser → filesystem. |
| `internal/skills/validator.go` | ~120 | Single source of truth: regex name, size cap `AURA_SKILL_BODY_CAP_BYTES` (default 32 KiB), parse roundtrip, dup-name, prompt injection literal-check (blocklist espansa, vedi Acceptance). Usato sia da writer che da install. |
| `internal/skills/paths.go` | ~40 | `SanitizeName(name) (string, error)` — **single chokepoint path-traversal guard** (audit round 1 P0). Riusato da writer/deleter/installer. Test static-analysis: ogni file-touch site DEVE chiamarlo prima di `filepath.Join`. |
| `internal/skills/writer.go` | ~120 | Atomic write a `pending/<name>/` + move pending→active. Path-traversal guard. Usato da create/update. |
| `internal/skills/deleter.go` | ~70 | FS remove da active + `Invalidate()`. Path-traversal guard. |
| `internal/skills/catalog.go` | ~140 | skills.sh fetch HTML + regex parse + search by query (pre-rewrite 1:1, HTTP timeout config-overrideable). |
| `internal/skills/installer.go` | ~140 | `npx skills add <source> --agent claude-code -y` con sanitizedEnv stretto, 90s timeout. Post-install ParseSkill re-validation prima di Invalidate. Path-traversal guard. |
| `internal/skills/audit.go` | ~70 | Thin adapter su sqlc `Queries.RecordSkillMutation` + `Queries.ListAuditSince`. Niente più file IO. |
| `internal/db/queries/skill_audit.sql` | ~40 | 3 query sqlc. |
| `internal/db/migrations/0007_skill_audit.up.sql` | ~55 | `CREATE TABLE aura.skill_audit` (id pk, ts timestamptz, actor_id uuid REFERENCES aura.identities(id), action, name, content_hash, source, approval_id, paused_state_token uuid REFERENCES aura.paused_states(token) ON DELETE SET NULL, `computed_risk_tier text NOT NULL DEFAULT 'risky' CHECK (computed_risk_tier IN ('safe','normal','risky','destructive'))`, `gate_recommended boolean NOT NULL DEFAULT true`, `gate_taken boolean NOT NULL DEFAULT true`). Indice su `ts DESC`, indice su `(gate_recommended, gate_taken) WHERE gate_recommended=true AND gate_taken=false` (forensics per gate-skipped). **Function `raise_audit_immutable()` + trigger `skill_audit_append_only BEFORE UPDATE OR DELETE ON aura.skill_audit FOR EACH ROW EXECUTE FUNCTION raise_audit_immutable()`** — audit append-only enforced a livello DB (audit round 2 P0). |
| `internal/agent/tools/skill_list.go` | ~50 | Non-deferred (output piccolo). |
| `internal/agent/tools/skill_catalog.go` | ~60 | Deferred. Query skills.sh, ritorna candidati. |
| `internal/agent/tools/skill_info.go` | ~60 | Deferred. Ritorna body via `ToolResult` (preview+persist se grande). |
| `internal/agent/tools/skill_create.go` | ~110 | Deferred. `Writer.WritePending` → `scoring.ComputeSkillTier(Create, body)` → tool result include risk_tier+gate_recommended. **Niente** auto-ask_user: agente decide. Resume handler (se ask_user triggered): `Writer.Activate` + `Audit.Record(gate_taken=true)`. Skip: audit `gate_taken=false` + Notifier alert. |
| `internal/agent/tools/skill_update.go` | ~100 | Stesso pattern di create + diff before/after disponibile per il prompt che l'agente costruisce. |
| `internal/agent/tools/skill_delete.go` | ~80 | Deferred. `scoring.ComputeSkillTier(Delete, "")` → DESTRUCTIVE. Mark pending delete (move active/→pending_delete/). Tool result include risk_tier=destructive + gate_recommended:true. Skip → Notifier alert critico + skill resta marked. |
| `internal/agent/tools/skill_install.go` | ~90 | Deferred. `scoring.ComputeSkillTier(Install, fetched_body)` → RISKY. Installer scarica via npx in `pending/`, NON in `active/`. Tool result + alert pattern. |
| `internal/agent/tools/skill_approve.go` | ~50 | Deferred. Args `{name}`. Atomic move pending→active (o pending_delete→deleted) + `Loader.Invalidate()` + audit `gate_taken=true`. Idempotente. |
| `internal/skills/loader/loader_test.go` | ~120 | Multi-root precedence + cache TTL + invalid SKILL.md skip. |
| `internal/skills/installer_test.go` | ~100 | Catalog fetch (fixture HTTP) + path traversal rejection + ask_user flow. |
| `internal/skills/writer_test.go` | ~80 | Atomic write + pending→active move + concurrent write race. |
| `internal/skills/audit_test.go` | ~50 | JSON-lines round-trip + concurrent record. |
| `cmd/aura/main.go` (diff) | ~+30 | Registra i 7 tool skill. Sub-command `aura skills audit`. |

**Open questions.**
1. **~~`when_to_use` field~~ — ELIMINATO.** Confermato dall'esempio reale `D:/tmp/assistant-ui/.claude/skills/tap/SKILL.md` e dal pattern Claude Code: frontmatter minimo, description ricca incorpora il when-to-use.
2. **~~skills.sh endpoint JSON~~ → CHIUSA: HTML scrape pre-rewrite, funzionava.** Regex `catalogItemRE` da [catalog.go](git:pre-rewrite-2026-05-27/internal/skills/catalog.go) riportato 1:1. Se skills.sh refactor-a HTML, fix sul momento (rischio noto e accettato — il pre-rewrite ha vissuto così senza incidenti).
3. **~~Remote install source pattern~~ → CHIUSA: delega a `skills` CLI.** Il source string passato a `npx skills add <source>` è qualsiasi cosa quel CLI accetti (slug skills.sh, owner/repo GitHub, npm package). Validazione lato Aura = solo regex `^[A-Za-z0-9@:._/\-]{1,200}$` (no path traversal) + length cap, come pre-rewrite.
4. **Skill versioning?** → *Default proposto: NO, fuori scope. Una skill = uno stato corrente nel filesystem. Audit log permette rollback manuale ("riapplica il content_hash X di ieri"). Versioning automatico è feature futura.*
5. **~~Cleanup pending stale~~ → CHIUSA (audit round 2 P0):** TTL 24h sui `pending/<name>/`, cleanup eseguito allo startup di Aura + ogni ora via tick. Implementato in Slice 7c con `internal/skills/cleanup.go` (~40 LOC). Logged via audit log (`action="cleanup_pending_stale"`, `name=<dir-name>`, `age_hours=<value>`).

**Commit message templates (sub-slice 7a + 7b + 7c + 7d).**

```
slice 7a: skills loader + validator + read-only tools

Filesystem loader split 4-ways (filesystem/parser/cache/loader), single-source
Validator (size cap 32 KiB + name regex + prompt injection blocklist espansa:
ChatML/Anthropic/Llama/Llama-3/DeepSeek-Gemma-Qwen literal blocklist), single
chokepoint internal/skills/paths.go SanitizeName (static-analysis test asserts
writer/deleter/installer use it). Tools skill.list + skill.info (read-only,
no governance). TTL cache 1s preserved from pre-rewrite.

Smoke: aura chat "fammi vedere quali competenze hai disponibili" → skill.list
returns multi-root precedence resolved.

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

```
slice 7b: skills catalog (skills.sh HTML scrape)

catalog.go ported 1:1 from pre-rewrite (HTTP fetch + catalogItemRE regex
parse, 8 MiB cap, 20s timeout). Tool skill.catalog (deferred). No npx, no
subprocess in this sub-slice — just discovery.

Smoke: aura chat "cerca su skills.sh una skill per analisi statistica" →
skill.catalog returns candidate list, model presents via ask_user kind=choice.

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

```
slice 7c: skills mutation (create/update/delete) + audit + governance C

Writer (atomic pending→active move via os.Rename), deleter (path-traversal
guarded), audit log on Postgres via sqlc (migration 0007_skill_audit with
CREATE TABLE aura.skill_audit + index ts DESC + trigger raise_audit_immutable
BEFORE UPDATE/DELETE → RAISE EXCEPTION). Tools skill.create/update/delete
all returning ErrAwaitingUserInput(kind=approval) — Loop pauses, user
approves/rejects, resume handler activates or discards. Cleanup pending TTL
24h on startup + hourly tick. ActionRouter from Slice 6b reused.

Smoke: aura chat "scrivi una skill che ti faccia rispondere sempre in haiku"
→ skill.create writes pending → ask_user → approve → next turn Aura in
haiku mode.

LOC: +XXX src / +YY test / +ZZ migration.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

```
slice 7d: skills installer (skills.sh via npx)

Installer wraps `npx --yes skills add <source> --agent claude-code -y
--ignore-scripts` (--ignore-scripts NEW addition vs pre-rewrite for supply
chain safety, blocks package.json postinstall hooks), sanitizedEnv whitelist
stretto (drops NPM_CONFIG_USERCONFIG/GLOBALCONFIG/PREFIX), 90s timeout
preserved from pre-rewrite, post-install ParseSkill re-validation prior to
Loader.Invalidate(). Tool skill.install with ErrAwaitingUserInput approval
gate showing catalog entry preview.

Smoke: malicious skill with postinstall:"rm -rf ~" → --ignore-scripts blocks
execution, test asserts.

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Pattern condiviso da estrarre (vale per Slice 5/6/7)

Tutti e tre i tool seguono lo stesso shape:
1. Tool LLM-facing accetta `action` enum + args
2. Dispatch su action → metodo Go privato
3. Validazione args, esecuzione, errore strutturato o `ToolResult`

Pre-rewrite questo pattern era duplicato in `search.go`, `scheduler.go`, `skill.go`. Per evitare di farlo di nuovo:

**File proposto:** `internal/agent/tools/action.go` (~90 LOC).

**Posizionamento (audit round 1 P1): introdotto in Slice 6, non Slice 5.** Slice 5 (`web_search`/`web_fetch`) sono 2 tool indipendenti senza azioni multiple — l'ActionRouter sarebbe written-and-unused (YAGNI). Slice 6 è il primo uso reale (`task_schedule`/`list`/`cancel`/`run_now` sotto un singolo tool `task` action-dispatched). Slice 7 lo riusa.

```go
type ActionRouter struct {
    name    string
    actions map[string]ActionHandler
}
type ActionHandler func(ctx context.Context, args json.RawMessage) (ToolResult, error)

func (r *ActionRouter) Dispatch(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
    var env struct{ Action string `json:"action"` }
    if err := json.Unmarshal(raw, &env); err != nil {
        return ToolResult{}, fmt.Errorf("%s: parse action: %w", r.name, err)
    }
    h, ok := r.actions[env.Action]
    if !ok { return ToolResult{}, fmt.Errorf("%s: unknown action %q", r.name, env.Action) }
    return h(ctx, raw)
}
```

**Sentinel passthrough contract (audit round 1 P0):**
Se un `ActionHandler` ritorna `ErrAwaitingUserInput` (sentinel di Slice 1.5), `Dispatch` lo propaga UNCHANGED al chiamante. NON lo wrap, NON lo trasforma in `ToolResult`. Questo è critico per Slice 7 (mutation tools che gateano via `ask_user`). Test in `action_test.go`: handler che ritorna `ErrAwaitingUserInput` → `Dispatch` ritorna `(ToolResult{}, *ErrAwaitingUserInput)` byte-identico, Loop lo riceve e pausa correttamente.

---

## Risk-Based Governance (Area #5 closed 2026-05-28, vale per Slice 6 + 7)

Pattern **Hybrid C — System computes tier, agent decides**. Non c'è formula numerica e non c'è hard force gate dell'agente: l'autonomia LLM è preservata ma il sistema ha veto strutturale (pending_approval state) sulle azioni che ritiene RISKY/DESTRUCTIVE.

### Risk tier (4 valori discreti)

```text
SAFE         reversible, local, ephemeral, no side effect
NORMAL       reversible o easily recoverable
RISKY        irreversibile O blast esteso O auto-modifica persistente
DESTRUCTIVE  rm-rf, drop table, force push, send-to-third-party, etc.
```

Modello qualitativo (non numerico): più chiaro per il modello LLM che lo legge nel tool result, più chiaro per audit (`aura task audit --tier=destructive`), più estendibile (nuovo kind = aggiungere riga al mapping).

### Mapping kind → tier (hard-coded in `internal/scoring/`)

```text
Slice 6 (cron):
  reminder           → SAFE       (notifica, reversibile via task_cancel)
  backup_postgres    → SAFE       (additive only, mai destructive)
  backup_neo4j       → SAFE
  agent_job          → NORMAL     (parent layer = solo spawn)
  agent_job + payload matches /\b(rm|delete|drop|purge|truncate)\b/ → DESTRUCTIVE

Slice 7 (skills):
  skill.create       → RISKY      (system prompt auto-modify)
  skill.update       → RISKY
  skill.install      → RISKY      (supply chain, mitigato da --ignore-scripts)
  skill.delete       → DESTRUCTIVE (irreversibile)
```

### Modificatori (bumpano UP, mai DOWN)

```text
schedule_kind every_minute|every_hour    → +1 tier (SAFE→NORMAL, NORMAL→RISKY, ...)
silent: true (agent_job senza notifier)  → +1 tier
agent_job senza Handler.Notifier wired   → +1 tier
update aumenta frequenza > 10x           → +1 tier
```

Saturano a DESTRUCTIVE. Nessun modifier scende il tier base.

### Pipeline di applicazione

```
1. agent chiama mutation tool (task.schedule | skill.create | ...)
2. system: tier = ComputeTier(args)
3. system writes audit row (computed_risk_tier=tier, gate_recommended=...)
4. SE tier in {SAFE, NORMAL}:
     mutation eseguita immediatamente, status='active'
     tool result: { ..., risk_tier, gate_recommended:false }
   SE tier in {RISKY, DESTRUCTIVE}:
     mutation parcheggiata in pending state (scheduler_tasks.status='pending_approval'
       o skills/pending/<name>/)
     tool result: { ..., risk_tier, gate_recommended:true, status:'pending_approval' }
5. agent vede il result:
     opzione A: ri-emette ask_user(kind=approval, ResumeContext={action, target_id})
       → utente risponde
       → resume handler chiama task.approve / skill.approve / cancellation
       → audit: gate_taken=true
     opzione B: agent skippa gate
       → mutation resta in pending (NON gira, NON viene caricata)
       → Notifier.Notify IMMEDIATE (`aura {task|skills} approve <id>` per attivare)
       → audit: gate_taken=false
       → l'utente può approvare via CLI in qualsiasi momento
```

### Threshold di alert

`AURA_RISK_ALERT_THRESHOLD` (env, default `risky`): tier minimo per scatenare Notifier alert quando `gate_taken=false`. Possibili valori: `safe` (alert sempre), `normal`, `risky`, `destructive` (alert solo destructive). `RequiresImmediateAlert(tier)` in `internal/scoring/` ritorna `tier >= AURA_RISK_ALERT_THRESHOLD`.

### Modulo condiviso `internal/scoring/`

```go
// ~100 LOC
package scoring

type RiskTier string
const (
    Safe        RiskTier = "safe"
    Normal      RiskTier = "normal"
    Risky       RiskTier = "risky"
    Destructive RiskTier = "destructive"
)

type TaskArgs struct {
    Kind          string  // reminder | agent_job | backup_postgres | backup_neo4j
    ScheduleKind  string  // oneoff | daily | every_hour | every_minute | ...
    Silent        bool
    AgentTier     string  // worker | chat | reasoning (only for agent_job)
    Payload       []byte  // raw, regex'd for destructive keywords
}
func ComputeTaskTier(a TaskArgs) RiskTier { ... }

type SkillAction string
const (
    SkillCreate  SkillAction = "create"
    SkillUpdate  SkillAction = "update"
    SkillInstall SkillAction = "install"
    SkillDelete  SkillAction = "delete"
)
func ComputeSkillTier(action SkillAction, body string) RiskTier { ... }

func GateRecommended(t RiskTier) bool { return t == Risky || t == Destructive }
func RequiresImmediateAlert(t RiskTier) bool { /* env-driven */ }
```

### Audit log schema (entrambe le tabelle)

```sql
-- aura.agent_job_runs (Slice 6)
ALTER TABLE aura.agent_job_runs ADD COLUMN
  computed_risk_tier text NOT NULL DEFAULT 'normal'
    CHECK (computed_risk_tier IN ('safe','normal','risky','destructive')),
  gate_recommended boolean NOT NULL DEFAULT false,
  gate_taken boolean NOT NULL DEFAULT false;

-- aura.skill_audit (Slice 7)
ALTER TABLE aura.skill_audit ADD COLUMN
  computed_risk_tier text NOT NULL DEFAULT 'risky'
    CHECK (computed_risk_tier IN ('safe','normal','risky','destructive')),
  gate_recommended boolean NOT NULL DEFAULT true,
  gate_taken boolean NOT NULL DEFAULT true;
```

Indici per forensics:
- `aura.agent_job_runs (gate_recommended, gate_taken, computed_risk_tier) WHERE gate_recommended=true AND gate_taken=false`
- `aura.skill_audit (gate_recommended, gate_taken) WHERE gate_recommended=true AND gate_taken=false`

### Notifier template

```text
[risk:RISKY] Aura ha [schedulato cron 'news-summary' (daily 02:00)
              | proposto skill 'haiku-mode' (create)
              | proposto skill 'aggressive-cleanup' (delete)]
senza chiederti conferma. Per attivare: `aura task approve 7f3a...`
(o `aura skills approve haiku-mode`). Per ignorare: `aura task cancel 7f3a...`.
Audit: `aura task audit --recent` o `aura skills audit --recent`.
```

### Razionale anti-auto-elevation

Il pattern B puro (LLM-computed score) ha un vettore di attacco noto:
**l'agente sotto-stima lo score per evitare gate**. 5 esempi reali coperti:
(1) cron costoso "non rischioso", (2) skill auto-modificante con prompt injection,
(3) cron irreversibile (rm/delete), (4) frequency escalation via task.update,
(5) silent cumulative damage. Pattern Hybrid C li mitiga tutti senza togliere
autonomia all'agente: il sistema computa il tier deterministicamente, l'agente
può ancora decidere di non gate-are, MA la mutation è parcheggiata in pending
finché non c'è approval (agente via ask_user OPPURE utente via CLI). Audit log
+ Notifier IMMEDIATE rendono il gate skip visibile.

---

## Caps & Limits (Area #8 closed 2026-05-28)

Aura ha 4 cap distinti con **semantica diversa** — non un valore unico polimorfico. Tutti env-overrideable, default tarati per chat tipica.

```text
AURA_CONTEXT_PREVIEW_CAP_BYTES = 2048
  Quanto di un output può entrare nel prompt context senza spillover.
  Usato da:
    - Slice 1 ToolResult: se Bytes > cap → sidecar in $AURA_RUN_DIR/<session>/<tool_call>.result,
      RoleTool.Content = preview + footer "Use read_tool_output(...) to fetch ranges."
    - Slice 3 swarm payload summarizer: se child report > cap → tronca + footer.
  Pattern: preview-in-context + sidecar-on-disk + offset/limit read.
  Stile Claude Code (error-with-pointer): l'agente vede il footer e sa COME
  recuperare il resto via read_tool_output, non perde dati.

AURA_WEB_RESPONSE_CAP_BYTES = 24000
  Hard cap della response di web_fetch/web_search (Slice 5).
  Anti-DOS: protegge dal caricare HTML di N MiB in memoria.
  Diversa semantica: è il limite della response HTTP, NON il preview-to-context.
  Pagine grandi vengono troncate alla sorgente (con "...[truncated]"), poi il
  ToolResult applica AURA_CONTEXT_PREVIEW_CAP_BYTES per spillover sidecar.

AURA_SKILL_BODY_CAP_BYTES = 32768  (32 KiB)
  Write-time refuse cap per il body di una SKILL.md (Slice 7).
  Semantica diversa: validation/rejection a write, non preview.
  Limite di prudenza: una skill > 32 KiB è quasi certamente garbage o
  prompt-injection payload nascosto in un blob lungo.

AURA_CONVERSATION_TURN_CAP_BYTES = 65536  (64 KiB)
  Soglia oltre cui content di una conversation_turns row va in sidecar
  invece di occupare la cella Postgres (Slice 1.8).
  Semantica: storage layout decision, non preview.
  Riusa pattern Slice 1 (sidecar file in $AURA_RUN_DIR/conversations/<id>/<seq>.content).

AURA_RUN_DIR_WARN_THRESHOLD_BYTES = 1073741824  (1 GiB)
  Soglia di WARN-only sulla dimensione totale di $AURA_RUN_DIR (Slice 1.8
  cleanup, Area #9).
  Semantica: alert, NON auto-purge. Al boot, du -sb $AURA_RUN_DIR > soglia
  → log + Notifier "consider aura chat archive/delete to free space".
  Filesystem cleanup avviene cascade su aura chat delete o boot orphan
  scan (dir senza conv_id in DB), non per età.
```

### Pattern condiviso "Large Output Handling"

Tutti i cap che implicano "preview + sidecar" (`AURA_CONTEXT_PREVIEW_CAP_BYTES`, `AURA_CONVERSATION_TURN_CAP_BYTES`) seguono lo stesso shape:

```
1. content_size := len(payload)
2. SE content_size <= cap:
     persisti payload as-is (in context, o in DB cell)
3. SE content_size > cap:
     write_full := persisti su disk a path predicibile
     write_preview := first <cap> bytes + footer "[truncated: N bytes total. <how to retrieve>]"
     persisti preview, NON il payload completo
4. read_back: chiamante usa offset/limit (read_tool_output, sidecar_read,
   Postgres fetch + sidecar load) per recuperare ranges arbitrari.
```

Pattern derivato da Claude Code per `Bash` (output truncation > 30k char) e per `WebFetch` (full content salvato in `tool-results/<tool>-<timestamp>.txt`). Vedi anche
[Anthropic Claude Code docs](https://docs.claude.com/en/docs/claude-code/overview) per il tool result truncation pattern.

### Naming convention

Tutti i cap usano lo schema `AURA_<DOMAIN>_<UNIT>` dove `<UNIT>` è esplicito:
- `_BYTES` per cap di dimensione (i 4 sopra)
- `_SEC` per timeout (es. `AURA_LLM_TOTAL_TIMEOUT_SEC`, `AURA_SANDBOX_TIMEOUT_SEC`)
- `_MS` per latenze fine-grained (raramente usato)

Niente cap senza unità nel nome (`AURA_TOOL_PREVIEW_CAP` → `AURA_CONTEXT_PREVIEW_CAP_BYTES`). Niente cap hardcoded nei file `.go` per valori >100 — devono essere env-overrideable.

---

## Sequencing rationale (Postgres infra → Neo4j infra → Agent → AskUser → Identity → Conversations → Sandbox → Swarm → KV → Web → Scheduler → Skills)

0. **Slice 0.5 (Postgres infra)** è il prerequisito di Slice 1.5/6/7. Indipendente da Slice 1 (LLM client non tocca DB), quindi può essere committato in parallelo. Atterrarla per prima dà a tutte le altre slice un substrate persistence pronto.
0.bis. **Slice 0.7 (Neo4j infra)** atterra subito dopo Slice 0.5. Le 8 slice 1→7 NON la consumano direttamente, ma la slice 0.7 sblocca: (a) il backup TaskKind `backup_neo4j` di Slice 6b che riferisce un container produzione, non lo spike fuori repo; (b) le slice knowledge-facing post-7 (in arrivo) che scrivono `:Chunk` / `:UserProfileMemory` / `:Entity`; (c) la disciplina CLAUDE.md "knowledge + vectors → solo Neo4j" diventa eseguibile dall'inizio del progetto invece che essere una promessa.
1. **Slice 1 (LLM client + ToolResult)** è prerequisito di tutto il resto: senza un client reale e senza il pattern preview+persist sui tool result, le altre slice non hanno modo di osservare comportamento end-to-end senza avvelenare il context window.
2. **Slice 1.5 (ask_user)** subito dopo: tocca `loop.go` una seconda volta su una concern semanticamente diversa (state machine pause/resume + PausedState persistent in Postgres). Atterrarlo prima di Slice 2-4 evita di rifattorare `loop.go` un'altra volta in mezzo. È anche prerequisito di Slice 7 governance C.
2.bis. **Slice 1.7 (Identity minimal)** chiude la wave persistence applicativa. Atterra dopo 1.5 (paused_states) e prima di Slice 2 perché: (a) sblocca Slice 7c per usare `actor_id` FK su `aura.skill_audit` invece di campo `text` opaco; (b) i suoi consumer (Slice 7c) sono lontani, ma il pattern "seed identity + capability_grants" deve essere stabile prima che skill_audit lo referenzi. Indipendente da Slice 2/3/4: può essere committata in parallelo a Slice 2 se preferito.
2.ter. **Slice 1.8 (Conversation persistence)** chiude la wave persistence applicativa con multi-conversation Claude.ai-style. Atterra dopo 1.7 (identity FK su conversations) e prima di Slice 2. Sblocca: (a) `aura chat list/resume/new` cross-session; (b) paused_states ora FK a conversations con cascade; (c) auto-resolve di pending stale quando Loop.Stop() (Area #7 closed); (d) audit forensics per token cost + cache hit ratio per conversazione. Out of scope riga "Persistenza disk dello stato conversazionale" rimossa. Migrations rinumerate: 0005_conversations (NEW), 0006_scheduler (era 0005), 0007_skill_audit (era 0006).
3. **Slice 2 (Sandbox)** prima dello Swarm: lo swarm spawn-a agenti che — nella realtà — useranno `execute`. Avere `execute` funzionante prima rende lo smoke dello swarm meno artificiale.
4. **Slice 3 (Swarm)** prima della KV: la KV cache discipline deve coprire ANCHE i prompt dei figli swarm-spawn. Costruire il PromptBuilder dopo aver visto come il parent passa goal/tools al child evita un secondo refactor.
5. **Slice 4 (KV)** chiude le 4 fondamentali: ora la superficie del prompt (system + manifest + tool descriptions + parent/child contracts + ask_user) è stabile. Il builder ottimizza un bersaglio fermo.
6. **Slice 5 (Web tools)** apre il backlog post-tabula-rasa: indipendente dalle 4 fondamentali, riusa `ToolResult` per fetch di pagine grandi. NON introduce `ActionRouter` (web_search/web_fetch sono 2 tool indipendenti senza azioni multiple — YAGNI). Vedi §Pattern condiviso.
7. **Slice 6 (Scheduler)** prima di Skills: si committa in 2 sub-slice (6a infrastructure + reminder handler, 6b agent_job + ActionRouter helper). 6b introduce `ActionRouter` come primo consumer reale (tool `task` con 4 azioni). Slice 7 lo riusa.
8. **Slice 7 (Skills)** ultima: si committa in 4 sub-slice (7a loader+validator+read-only tools, 7b catalog, 7c mutation governance, 7d installer). Richiede il maggior numero di primitive pre-esistenti (ask_user da 1.5, ToolResult da 1, ActionRouter da 6b, persistent state da 0.5 + 1.5). Atterrarla per ultima evita di riscrivere il flow governance ogni volta che una primitive cambia forma.

## Out of scope per tutte e 4 le slice (esplicito)

- Telegram, dashboard, wiki, Qdrant, FTS5 (vedi [README.md](README.md)).
- Setup wizard, tray, OTA update.
- Auth dell'endpoint `aura serve` (slice CLI-server separato).

## Non-goals di processo

- **Nessuna feature flag.** Se uno slice spegne lo stub, lo stub viene rimosso. No toggle.
- **Nessun re-export.** File spostati = rimossi all'origine, ZERO shim.
- **Nessun TODO comment** lasciato in-tree. Se non è nello scope dello slice corrente, è in `prd.md` (qui), non nel sorgente.
- **CI deve restare verde** dopo ogni commit. Test integrazione che richiedono sidecar/network → behind build tag (`//go:build sandbox_integration`, `//go:build db_integration`).

---

## Nota CLAUDE.md (da applicare prima di Slice 0.5)

La sezione `## Persistence` di [CLAUDE.md](CLAUDE.md) va aggiornata per riflettere la decisione architetturale finale:

```markdown
## Persistence

Tre store, ciascuno con la sua semantic responsibility:

- **Neo4j via `mcp-neo4j-cypher` MCP server (stdio)** — **unica fonte di knowledge E di vector embeddings**. Semantic memory, entity graph, conversational memory, derivati relazionali, vector index nativo HNSW Lucene. Accessed solo via MCP, no native Go adapter. La precedente architettura wiki-markdown filesystem + `[[wiki-links]]` è stata deprecata 2026-05-27 dopo test (spike in `D:/tmp/aura-neo4j-spike-2026-05-27/`).
- **Embedder dedicato**: `embeddinggemma` via sidecar `aura-llama-embed` (porto 8081, OpenAI-compat). **768d nativo**, scritti direttamente su nodi `:Chunk.embedding` in Neo4j. Vector index HNSW configurato a 768 dim (no MRL truncation, no client-side resize). NON in store separato.
- **PostgreSQL 17 via `sqlc` + `jackc/pgx/v5`** — application state. Scheduler tasks, paused states, audit log, identity, capability grants. Schema `aura`. Migrations versionate in `internal/db/migrations/`. Industrial-grade, type-safe queries generate da SQL files.
- **Filesystem** — solo artefatti operativi: SKILL.md tree (`~/.aura/skills/active/`), tool result sidecar files (`$AURA_RUN_DIR/<session>/<tool-call-id>.result`). **Mai knowledge.**

Distinzioni semantiche **non negoziabili**: knowledge + vectors → solo Neo4j (via MCP); application state → solo Postgres (via sqlc); operational artifacts → filesystem. Le slice future che hanno bisogno di persistenza scelgono lo store giusto in base a cosa stanno salvando, non a "che dep era già lì". Nessuna knowledge in Postgres; nessun task scheduler in Neo4j; nessun markdown wiki da nessuna parte; nessun vector index dedicato fuori da Neo4j (Qdrant deprecato 2026-05-27 dopo validazione spike Phase 6b).
```

Questa nota va committata nello stesso commit di Slice 0.5 (sono accoppiate: codice + contract documentale insieme).
