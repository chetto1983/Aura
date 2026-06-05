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

> **D00 — Binario portabile unico (amendment #30, 2026-06-01).** Aura è UN binario che gira su 3 target — **Hetzner cloud / mini-PC 32GB / NVIDIA DGX Spark** — con locale-vs-remoto e provider selezionati da **config per-deployment**, NON build separate (registro `.planning/DECISIONS.md` §0). **6 invarianti di portabilità non-negoziabili:**
> 1. **Multi-arch arm64 + amd64.** DGX Spark è ARM (Grace); cloud/mini-PC x86. Binario Go cross-compila; **ogni sidecar deve avere immagine arm64** → guardrail CI (build multi-arch).
> 2. **seccomp è arch-specifico** (numeri syscall x86≠ARM) → la sandbox (Slice 2) usa `libseccomp` (risolve per-nome) o due profili. Vedi acceptance Slice 2.
> 3. **Routing LLM/embedder config-driven** (endpoint `AURA_LLM_BASE_URL`/`AURA_EMBED_BASE_URL` + provider): nessun path hardcoda un provider. Il client OpenAI-compat già abilita remoto + locale (vLLM/llama.cpp). L'`LLMRouter` (Slice 13) estende, ma il **contratto endpoint-config-driven è day-1**.
> 4. **Un solo embedder/dim su tutti i deployment**: no index 768d sul mini-PC e 1024d sul DGX — lo stesso Aura li legge entrambi. Embedder scelto sul target più piccolo (32GB).
> 5. **`AURA_PRIVACY_MODE=local-only`** fail-fast se una capability (LLM/embedder/web) è remota → "massima privacy DGX" come garanzia verificabile, non speranza.
> 6. **Footprint locale ≤ target più piccolo**: chat LLM **remoto** su 32GB (non regge modello forte + Neo4j + Postgres + embedder + OS), **locale** su DGX 128GB.

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
docker compose -f compose.yaml up -d postgres
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
- [ ] **Postgres role separation (amendment #17, Pitfall #6 P0)**: migration `0001_init.sql` creates TWO roles: `aura_app` (granted `INSERT, SELECT` on `aura.*` audit tables, granted `INSERT, SELECT, UPDATE, DELETE` on `aura.*` mutable tables — NO TRUNCATE, NO DROP) and `aura_migrate` (granted ALL on `aura.*` including DDL, TRUNCATE). The runtime binary `aura serve` connects with `AURA_DB_URL` (role `aura_app`). Migrations run via `aura db migrate` connecting with `AURA_DB_MIGRATE_URL` (role `aura_migrate`). If `AURA_DB_MIGRATE_URL` is unset and `aura serve` boots: continue, but `aura db migrate` subcommand fails fast with explicit error "AURA_DB_MIGRATE_URL required for DDL operations — see prd.md amendment #17". Acceptance test (`db_integration`): connect as `aura_app`, attempt `TRUNCATE aura.skill_audit;` → must return permission denied. Connect as `aura_migrate`, same TRUNCATE → succeeds (consumed by migration only).

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
| `compose.yaml` (diff) | ~+20 | Service `postgres` con healthcheck + volume named + env from `.env`. (D-02: compose lives at repo root, not under `sandbox/`.) |
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
| Neo4j 5.26-community LTS + APOC + GDS + vector index | ~1.5-2 GB | 0.7 |
| aura-llama-embed (embeddinggemma CPU, 4 thread) | ~600 MB | 0.7 (sidecar embedding per Neo4j HNSW vector index) |
| SearXNG | ~150 MB | 5 |
| Sandbox Python sidecar | ~80 MB | 2 |
| ~~Pocket-TTS~~ | — | **Rimosso dalla tabella**: nessuna slice del PRD attuale lo cita o lo usa. Era residuo di scope precedente. TTS resta out-of-scope. |
| aura-llama-multimodal (Gemma 4 E4B Q4 baseline, vision+STT unified) | ~3 GB | 9c |
| Markitdown sidecar | ~150 MB | 9c (sostituisce attribuzione "(esterno)" precedente — è sidecar attivo aggiunto da Slice 9c per document conversion) |
| Aura Go binary | ~150 MB | 1 |
| **Totale idle** | **~5.7-6.2 GB** | (dopo correzione Pocket-TTS rimosso, Gemma 4 incluso) |
| Sotto carico (LLM batch + swarm 3 worker) | **+1 GB** | |
| **Peak realistic** | **~7 GB** | |

Headroom su 32 GB: ampio (~25 GB liberi). Su 16 GB: ~9 GB liberi per OS + utente. Ancora accettabile.
Se la stima passa 10 GB peak in futuro, va dedicato un budget review.

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
- DB: **Neo4j 5.26-community LTS** (container `neo4j:5.26-community`, ~1.5-2 GB RAM idle; LTS pinned per amendment #2 — avoids CalVer ambiguity post-5.x)
- Plugins: **APOC** (procedure standard) + **GDS** (Graph Data Science, community detection, PPR) + Vector index (built-in 5.x, Apache Lucene HNSW)
- MCP server: **`mcp-neo4j-cypher`** (Apache 2.0) — subprocess stdio spawn-ato da Aura, lifecycle accoppiato al processo principale. **No native Go adapter** (per disciplina CLAUDE.md): tutto accesso Neo4j passa da MCP.
- Embedding dim: **768** — l'index Neo4j HNSW è il contratto. **D11 risolto (amendment #31, vedi `.planning/DECISIONS.md`)**: l'embedder DEVE emettere 768 (nativo, o **MRL-truncato-a-768** per modelli con dim nativa ≠ 768). Default = `embeddinggemma-300m@768d` (nativo, sub-200MB). Il nome modello NON è hardcoded: qualsiasi 768-native-GGUF servito da llama.cpp `/v1/embeddings` è drop-in (2 arg compose). Pick finale = benchmark P15 su corpus IT (front-runner Granite-r2 311m: 768 nativo + Apache + MMTEB ~65).
- DB name: **`neo4j` default** (Community non supporta multi-database; `CREATE DATABASE aura` richiede Enterprise)
- Migrations: file `.cypher` numerati in `internal/knowledge/migrations/`, eseguiti via MCP `cypher_execute`. Audit applicate registrato in **Postgres** tabella `aura.knowledge_migrations` (centralizza audit con golang-migrate).

**Smoke.**
```bash
docker compose -f compose.yaml up -d neo4j
./aura neo4j migrate           # applica tutte le migration .cypher pendenti
./aura neo4j ping              # MATCH (n) RETURN count(n), stampa "ok + ms"
./aura neo4j status            # lista migration applicate (da Postgres)
```

**Acceptance.**
- [ ] Container `aura-neo4j` su volume named `aura_aura-neo4j` (NO bind-mount Windows — coerente con feedback `feedback_sqlite_wal_windows_corruption.md` esteso a Neo4j). Healthcheck `cypher-shell -u neo4j -p $NEO4J_PASSWORD --database neo4j 'RETURN 1'`.
- [ ] Auth via `NEO4J_PASSWORD` da `.env` (default `changeme`, must change al primo boot). `NEO4J_AUTH=neo4j/$NEO4J_PASSWORD` propagato al container.
- [ ] Plugins APOC + GDS abilitati via `NEO4J_PLUGINS='["apoc","graph-data-science"]'` (auto-download Neo4j Community feature 5.26 LTS).
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
  Schema iniziale **minimale**: solo `:Chunk` perché è l'unica label che gli slice 1→7 useranno indirettamente (via tool result sidecar future). Altre label (`:Entity`, `:Source`, ecc.) atterrano nelle rispettive slice knowledge future. **Nota Slice 10**: il profile utente (Agent.md) NON va su Neo4j — vive su filesystem `~/.aura/agents/<id>/` (Slice 10) per ispezionabilità + git-friendliness; quindi nessuna label `:UserProfileMemory` è prevista (potrebbe atterrare in futura slice di memory consolidation se serve query semantica sul profile, ma non oggi).
- [ ] `aura neo4j migrate` idempotente. Applica solo le migration nuove. Errore esplicito su schema drift (migration applicata + file `.cypher` changed → abort).
- [ ] `internal/knowledge/client.go` espone `Open(ctx, cfg) (*Client, error)` con ping MCP al boot. Fail-fast se MCP server non risponde entro 10 s.
- [ ] Container Aura con `depends_on: condition: service_healthy` per `neo4j` (oltre a `postgres` e `aura-llama-embed`).
- [ ] **Embedding dim env contract (amendment #18, Pitfall #7 P0)**: env `AURA_EMBED_DIMENSIONS=768` (default 768 for `embeddinggemma-300m`). On boot, embed sidecar `aura-llama-embed` performs self-test: load one dummy embedding, assert `len(vector) == AURA_EMBED_DIMENSIONS`. Mismatch → exit code 78 (`EX_CONFIG`) with explicit error `embedding model output_dim=N != AURA_EMBED_DIMENSIONS=M — refuse to start (Pitfall #7 silent corruption)`. Aura `aura neo4j ping` validates sidecar `/v1/embeddings round-trip returns 768d (Pattern 5 dim probe)` matching `AURA_EMBED_DIMENSIONS`. Mismatch → boot fail. (Amended Slice 0.7: the `/health` endpoint returns `{"status":"ok"}` with no dim field — ground truth — so the probe POSTs a dummy input to `/v1/embeddings` and asserts `len(data[0].embedding)`.) Reference industry incidents: `neo4j#13387`, `langchain#16336`.
- [ ] **Embedding model swap runbook (amendment #18)**: docstring section in Slice 0.7 PRD body documents the rule: "NO in-place embedding model upgrades. To change embed model: (1) stop ingest, (2) snapshot Neo4j via `neo4j-admin database dump`, (3) drop vector index, (4) re-create with new `vector.dimensions`, (5) re-embed all `:Chunk.embedding` from `:Chunk.text`, (6) re-create index. Half-state = silent retrieval corruption."
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
| `compose.yaml` (diff) | ~+30 | Service `neo4j` (`neo4j:5.26-community`, plugins APOC+GDS, volume named, healthcheck, env auth). (D-02: compose lives at repo root, not under `sandbox/`.) |
| `cmd/aura/main.go` (diff) | ~+40 | Sub-command `aura neo4j {migrate|ping|status|reset}`. |
| `Makefile` (diff) | ~+15 | Target `make neo4j-up`, `make neo4j-migrate`, `make neo4j-reset`. |
| `.env.example` (diff) | ~+5 | `NEO4J_PASSWORD=changeme`, `NEO4J_USER=neo4j`. |

**Deferred-tool partition.** Niente tool nuovo in questo slice. È pura infra. Le slice future che esporranno tool knowledge-facing decidono il loro tier.

**Open questions.**
1. **MCP binary distribution: bundled in `aura init-models` o richiesto su PATH?** → *Default proposto: richiesto su PATH (`mcp-neo4j-cypher` installato via `pip install mcp-neo4j-cypher` o equivalente). Fail-fast all'avvio con messaggio chiaro se non trovato. Bundling = scope creep, rinviato.*
2. **Retention `aura.knowledge_migrations`?** → *Default proposto: nessuna. È audit append-only, una riga per migration. A 1000 migration siamo a ~80 KB. Non vale la pena.*
3. **`neo4j_integration` test fixture data: in `testdata/*.cypher` o programmatici?** → *Default proposto: programmatici (Cypher inline nei test). Fixture file `.cypher` è premature optimization a questo punto.*

**Mini-PC RAM budget — delta vs Slice 0.5.**

Neo4j 5.26-community LTS + APOC + GDS + vector index a 768 dim su corpus realistico (≤ 100k chunk) consuma stabilmente 1.5-2 GB RAM idle. A 1M chunk (limite alto del power user) ~3-4 GB. Già contato nella tabella cumulativa di Slice 0.5 riga 87 (ora aggiornata: Slice 0.7).

**Commit message template.**
```
slice 0.7: neo4j community + mcp-neo4j-cypher + cypher migrations

Neo4j 5.26-community LTS container (named volume, APOC+GDS plugins),
mcp-neo4j-cypher subprocess stdio spawned by Aura, Cypher migrations
file-based via MCP with audit in Postgres aura.knowledge_migrations
(sqlc-managed). 0001_init.cypher: :Chunk(id) UNIQUE constraint +
vector index HNSW 768d cosine + fulltext index. Embedding dim 768
(default EmbeddingGemma nativo; un modello dim≠768 MRL-trunca a 768 —
contratto D11/amendment #31). Subcommand aura neo4j {migrate|ping|status}.

Renames backup_neo4j docker exec target from neo4j-spike to
aura-neo4j (Slice 0.5 row 105 amended).

Smoke: aura neo4j {migrate|ping|status} all green against fresh
container.

LOC: +XXX src / +YY test / +ZZ infra.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Slice 0.9 — Agent runtime abstraction (`Agent` interface + workflow agents)

> **Pattern rubato (non importato) da Google adk-go** ([github.com/google/adk-go](https://github.com/google/adk-go), v1.3.0 May 2026, 8k stars). Adottiamo il *design* — `Agent` interface unificata + `iter.Seq2[*Event, error]` streaming + workflow agents Sequential/Loop/Parallel — senza importare il package (35 deps GCP/OTel/Gemini-heavy → footprint inaccettabile per Aura minimal stack). ~380 LOC totali, riusati cross-slice con saving netto ~−460 LOC.

**Goal.** Definire un'interfaccia `Agent` unica che tutto Aura riusa: LLM agent (Slice 1 Loop), workflow agents (Sequential/Loop/Parallel built-in), scheduler handlers (Slice 6), skills come Agent virtuali (Slice 7), swarm workers (Slice 3). Streaming events idiomatico Go 1.25+ via `iter.Seq2[*Event, error]`. Termination propagata across agent tree tramite `event.Actions.Escalate`.

Risolve un design smell latente del PRD pre-Slice 0.9: ogni slice aveva il suo "runtime" (Loop, Scheduler.Handler, Swarm.Coordinator, Skill.execute, onboarding state machine) — quattro runtime diversi da mantenere, testare, debuggare. Con Slice 0.9 c'è **un solo runtime**, l'interfaccia `Agent`, e ogni slice ne fornisce una o più implementations.

### Pre-requisiti

- Go 1.25+ (per `iter.Seq2` range-over-func; AG-UI Go SDK requires 1.24.4+ — pin to 1.25 for headroom; amendment #1 from SUMMARY.md table)
- Slice 0 Postgres infra (per `InvocationContext.SessionStore`)
- Slice 0.7 Neo4j infra (per `InvocationContext.GraphStore`)
- `github.com/google/uuid` v1.6.0+ (UUIDv7 — amendment #9 OTel correlation)

### Architettura

```
internal/agent/
  agent.go              # ~80   Agent interface + InvocationContext + Config base
  event.go              # ~70   Event struct {LLMResponse, Author, Branch, Actions}
                        #       Actions{Escalate bool, StateDelta map[string]any,
                        #               ArtifactDelta map[string]any}
  workflow/
    sequential.go       # ~30   SequentialAgent — itera sub-agents una volta in ordine
    loop.go             # ~40   LoopAgent — ripete sub-agents N volte o fino a Escalate
    parallel.go         # ~70   ParallelAgent — errgroup + chan, sub-agents concurrent
  workflow/
    workflow_test.go    # ~100  Test 3 workflow + escalation propagation
```

### Interface base (rubato da adk-go agent.go)

```go
package agent

import (
    "context"
    "iter"
)

// Agent is the base interface which all agents must implement.
type Agent interface {
    Name() string
    Description() string
    Run(InvocationContext) iter.Seq2[*Event, error]
    SubAgents() []Agent
    FindAgent(name string) Agent
}

// InvocationContext carries cross-cutting state per Run invocation.
// Composed once at Loop top-level, propagated to all sub-agents.
type InvocationContext struct {
    Ctx           context.Context
    Agent         Agent              // self-reference, used by workflow agents to access SubAgents()
    SessionID     string             // conversation_id (Slice 1.8)
    IdentityID    string             // identity_id (Slice 1.7)
    Branch        string             // hierarchical path for nested agents (e.g. "swarm.worker-3")
    SessionStore  SessionStore       // Postgres-backed (Slice 0)
    GraphStore    GraphStore         // Neo4j MCP (Slice 0.7)
    LLMClient     llm.Client         // shared client (Slice 1)
    RequestID     string             // UUIDv7 per Run invocation — OTel-compatible correlation id; root of trace span tree (amendment #9)
    RemainingSteps           int           // budget contract amendment #19: steps remaining for THIS Run before parent-budget exhaustion. Workflow agents decrement + propagate.
    RemainingWallclockDeadline time.Time   // budget contract amendment #19: absolute deadline (parent-anchored). Children inherit this instant unchanged.
    // ... extension points: Tools, Memory, Artifacts (added incrementally by later slices)
}
```

### Event type (rubato + Aura extension)

```go
package agent

type Event struct {
    Author       string             // agent name OR "user"
    Branch       string              // invocation branch (mirror of InvocationContext.Branch)
    LLMResponse  *LLMResponse        // nil if non-LLM event (e.g. tool result, lifecycle)
    Actions      Actions
    Timestamp    time.Time
}

type Actions struct {
    Escalate      bool                 // signal upward: stop loop / abort branch
    StateDelta    map[string]any       // mutations to session state (merge into SessionStore)
    ArtifactDelta map[string]any       // file/blob produced (for sidecar persistence)
}

type LLMResponse struct {
    Content      string                // streamed text chunk OR full final
    ToolCalls    []ToolCall
    FinishReason string                // "stop", "tool_use", "max_tokens", "escalate"
}
```

### Workflow agents (built-in)

**`SequentialAgent.Run`** — esegue sub-agents una volta, in ordine:
```go
func (a *SequentialAgent) Run(ctx InvocationContext) iter.Seq2[*Event, error] {
    return func(yield func(*Event, error) bool) {
        for _, subAgent := range ctx.Agent.SubAgents() {
            for event, err := range subAgent.Run(ctx) {
                if !yield(event, err) { return }
                if event != nil && event.Actions.Escalate { return }  // upward propagation
            }
        }
    }
}
```

**`LoopAgent.Run`** — ripete sub-agents N volte o fino a `Escalate`:
```go
type LoopAgent struct {
    name          string
    maxIterations uint                // 0 = infinite (Caps & Limits applies hard cap)
    subAgents     []Agent
}

func (a *LoopAgent) Run(ctx InvocationContext) iter.Seq2[*Event, error] {
    return func(yield func(*Event, error) bool) {
        iter := uint(0)
        for {
            if a.maxIterations > 0 && iter >= a.maxIterations { return }
            iter++
            for _, sub := range a.subAgents {
                for event, err := range sub.Run(ctx) {
                    if !yield(event, err) { return }
                    if event != nil && event.Actions.Escalate { return }
                }
            }
        }
    }
}
```

**`ParallelAgent.Run`** — sub-agents concorrenti, errgroup + chan:
```go
func (a *ParallelAgent) Run(ctx InvocationContext) iter.Seq2[*Event, error] {
    return func(yield func(*Event, error) bool) {
        type result struct { event *Event; err error; ackChan chan struct{} }
        resultsChan := make(chan result)
        eg, egCtx := errgroup.WithContext(ctx.Ctx)
        for _, sub := range ctx.Agent.SubAgents() {
            sub := sub
            eg.Go(func() error {
                subCtx := ctx.WithSubInvocation(sub)
                for event, err := range sub.Run(subCtx) {
                    ack := make(chan struct{})
                    select {
                    case resultsChan <- result{event, err, ack}:
                    case <-egCtx.Done(): return egCtx.Err()
                    }
                    <-ack  // backpressure: yield must consume before next event
                }
                return nil
            })
        }
        doneChan := make(chan error, 1)
        go func() { doneChan <- eg.Wait(); close(resultsChan) }()
        for res := range resultsChan {
            if !yield(res.event, res.err) { return }
            close(res.ackChan)
            if res.event != nil && res.event.Actions.Escalate {
                // Escalate from ANY child cancels siblings via egCtx
                return
            }
        }
        if err := <-doneChan; err != nil { yield(nil, err) }
    }
}
```

### Termination model (escalation propagation)

```
Tree composition:
  Root (LoopAgent maxIter=5)
    └─ SequentialAgent
         ├─ LlmAgent (writer)
         └─ LlmAgent (critic)

Critic emits Event{Actions:{Escalate:true}} when satisfied
  → SequentialAgent yields it, then returns (skip remaining siblings)
  → LoopAgent yields it, then returns (skip remaining iterations)
  → Root returns to caller

No magic, just bubble-up via the `return` after every `yield` check.
```

Coerenza con primitives Aura esistenti:
- `ask_user.Pause` (Slice 1.5) → emette `Event{FinishReason:"escalate", Actions:{Escalate:true, StateDelta:{paused_state_token: ...}}}` → propaga upward
- `Loop.Cancel` (Slice 1) → `InvocationContext.Ctx` cancel → tutti i sub-agents si fermano via context

### Reuse cross-slice (saving cumulativo)

| Slice | Implementation di Agent | Pre-0.9 LOC | Post-0.9 LOC | Δ |
|---|---|---:|---:|---:|
| 1 | `LlmAgent` (LLM streaming + tool dispatch + MaxSteps) | 520 | 480 | −40 |
| 3 | Swarm Coordinator riusa `ParallelAgent`, worker = `LlmAgent` | 800 | 600 | **−200** |
| 6 | `Handler` interface = `Agent`, dispatch uniforme | 1300 | 1100 | **−200** |
| 7 | Skill come template che produce un `Agent` runtime | 1400 | 1380 | −20 |
| 8 | AG-UI emitter consume `iter.Seq2[*Event, error]` direttamente | 700 | 600 | −100 |
| 10 | Onboarding = `LoopAgent[InterviewStepAgent]` + escalation on "Conferma" | 700 | 580 | −120 |
| **Sum** | | | | **−680** |
| **0.9 cost** | | | 280 | +280 |
| **NET** | | | | **−400** |

(Stima conservativa: rifinire post-impl, ma la direzione è chiara.)

### Smoke

```bash
go test ./internal/agent/workflow/ -run TestSequentialAgent
# Sequential[A,B,C] yield ordered: eventA, eventB, eventC.

go test ./internal/agent/workflow/ -run TestLoopAgentMaxIter
# Loop[X] maxIter=3 yield 3 events. Stops after 3.

go test ./internal/agent/workflow/ -run TestLoopAgentEscalate
# Loop[X] maxIter=10, X emits Escalate at iter 2. Yield 2 events, returns.

go test ./internal/agent/workflow/ -run TestParallelAgent
# Parallel[A,B,C] yield events concurrently. Order non-deterministic.
# Escalate from B cancels A,C via errgroup ctx.
```

### Acceptance

- [ ] `internal/agent/agent.go` definisce `Agent` interface + `InvocationContext` + builder helpers (`NewSequential(name, subAgents...)`, `NewLoop(name, maxIter, subAgents...)`, `NewParallel(name, subAgents...)`).
- [ ] **`InvocationContext.RequestID` UUIDv7 (amendment #9)**: every top-level `agent.Run(ctx)` invocation populates `RequestID` with a fresh UUIDv7 (time-ordered, 128-bit) via `github.com/google/uuid` v1.6.0+ `uuid.NewV7()`. Child sub-agents (Sequential/Loop/Parallel) inherit the parent's `RequestID` unchanged. Every emitted `*Event` carries the `RequestID` in `Event.Branch` prefix `req:<uuid7>::<branch-path>` for OTel correlation. Acceptance test: spawn nested 3-level agent tree, assert all emitted events share the same `req:<uuid7>::` prefix.
- [ ] **Agent loop budget contract (amendment #19, Pitfall #9 P0)**: three orthogonal caps enforced per `Run(InvocationContext)`:
  - `AURA_LOOP_MAX_STEPS=25` (default; hard cap on tool-call iterations within a single Run)
  - `AURA_LOOP_MAX_WALLCLOCK_SEC=300` (default; hard cap on wall-clock seconds before returning `Event{FinishReason:'max_wallclock', Actions.Escalate=true}`)
  - `AURA_LOOP_DEDUP_WINDOW=3` (default; sliding window — if the last N=3 tool calls had identical `tool_name + args_hash`, return `Event{FinishReason:'dedup_loop', Actions.Escalate=true}`)
  All three caps are orthogonal: ANY one tripping terminates the Run. The triggering cap is reported in `Event.FinishReason`.
- [ ] **Hardening knobs (A7, applied 02-00; verified 02-07)**: four optional env vars refine the three-cap contract without changing its default behavior — `AURA_LOOP_DEDUP_EXEMPT_TOOLS` (CSV allowlist exempt from dedup, D-19), `AURA_LOOP_BRANCH_SOFT_FRACTION=1.0` (passive per-branch fair share in `(0,1]`, non-terminal, D-12), `AURA_LOOP_NODE_TIMEOUT_SEC=0` (optional per-node `context.WithDeadline`, disabled at 0, D-13), `AURA_LOOP_DEDUP_RESULT_CAP=2048` (byte cap on the dedup result-preview progress veto, D-18). All four catalogued in §Caps & Limits and documented in `.env.example`.
- [ ] **Child budget inheritance (amendment #19, Pitfall #9 P0)**: workflow agents Sequential/Loop/Parallel propagate the parent `InvocationContext`'s **remaining** step + wallclock budgets to children (NOT fresh per child). Otherwise swarm depth=3 × 25 = 15625 total steps (compounding explosion). Implementation: `InvocationContext.RemainingSteps int` + `RemainingWallclockDeadline time.Time` fields, decremented + checked by each agent's Run before spawning sub-agents. Acceptance test: depth-3 spawn chain with parent budget=20 steps → total steps across the tree ≤ 20, NOT 20×3=60. Test: TestBudgetInheritance_3Deep_ParallelSpawn covers Parallel propagation; TestBudgetInheritance_LoopWithChildSwarm covers Loop→Parallel composition.
- [ ] `internal/agent/event.go` definisce `Event` + `Actions` + `LLMResponse`. Helper `NewEscalateEvent(author, reason string)`.
- [ ] `internal/agent/workflow/{sequential,loop,parallel}.go` implementano i 3 workflow agents. ParallelAgent usa errgroup + ackChan backpressure (rubato da adk-go pattern).
- [ ] Escalation propagation testata: child Escalate → parent ferma yield ai siblings.
- [ ] Go 1.25+ floor enforced in `go.mod` (oggi `go 1.26.3` — floor superato, fix doc-verify 2026-06-01).
- [ ] **Niente import di `google.golang.org/adk`**. Rubiamo il pattern, non la dependency.
- [ ] Test coverage workflow/ ≥ 85%.

### File targets cumulativi

`internal/agent/agent.go` ~80 + `event.go` ~70 + `workflow/{sequential,loop,parallel}.go` ~140 + test ~100 = **~390 LOC totali**.

### Open questions

1. **`InvocationContext.Branch`**: stringa free-form (es. `"swarm.worker-3.loop.iter-2"`) o nested struct con parent pointer? *Default proposto*: stringa free-form, parsing on-demand (più leggera).
2. **`Actions.StateDelta` merge semantics**: shallow merge in `SessionStore` o `jsonb_set` deep? *Default proposto*: deep merge via `jsonpatch` (RFC 6902) per evitare overwrite involontari di sub-tree.
3. **Backpressure ParallelAgent**: ackChan synchronous (slow consumer = slow producer) o buffered chan size N? *Default proposto*: synchronous (rubato da adk-go), evita memory bloat con N grosso.

### Mini-PC RAM budget — delta

Negligibile. Agent runtime è puro Go code, nessun sidecar/dep nuovo.

### Commit message template

```
slice 0.9: Agent runtime abstraction (interface + workflow agents)

Define `Agent` interface unificata + `iter.Seq2[*Event, error]` streaming
(Go 1.25+) + workflow agents built-in (Sequential, Loop, Parallel).

Pattern rubato (non importato) da google/adk-go v1.3.0 (8k stars).
Importare adk-go come dependency e' inaccettabile per Aura: 35 deps
GCP/OTel/Gemini-heavy footprint. Rubiamo il design (3 interfaces +
3 workflow agents = ~280 LOC totali), zero coupling esterno.

Risolve design smell pre-0.9: ogni slice aveva il proprio runtime
(Loop, Scheduler.Handler, Swarm.Coordinator, Skill.execute, onboarding
state machine). Con Agent interface unificata = 1 runtime, 4+ impl
(LlmAgent in Slice 1, ParallelAgent riuso in Slice 3 Swarm,
Scheduler.Handler in Slice 6 = Agent, LoopAgent[InterviewStepAgent]
in Slice 10 onboarding).

Termination propagata via event.Actions.Escalate (bubble-up):
child Escalate -> SequentialAgent yields then returns (skip siblings)
                -> LoopAgent yields then returns (skip remaining iterations).
Coerente con ask_user.Pause (Slice 1.5) e Loop.Cancel (Slice 1).

InvocationContext composto a top-level Loop, propagato via SubAgents().
Carries: Ctx, Agent (self-ref), SessionID, IdentityID, Branch,
SessionStore (Slice 0 Postgres), GraphStore (Slice 0.7 Neo4j),
LLMClient (Slice 1). Extension points per Tools/Memory/Artifacts.

Saving cumulativo (stima): -680 LOC sulle slice 1/3/6/7/8/10 grazie
a riuso pattern unificato. Net dopo +280 LOC Slice 0.9 = -400 LOC
sul progetto totale + qualita' architettonica (1 mock, 1 test
infrastructure, 1 streaming pattern).

Go 1.25 minimum (range-over-func iter.Seq2, AG-UI SDK 1.24.4+ requirement); go.mod oggi `1.26.3` (floor superato — fix doc-verify 2026-06-01).

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Slice 1 — LLM client reale + ToolResult pattern (preview + persist)

> **Slice 0.9 amendment**: il `Loop` introdotto qui è ridefinito come `LlmAgent` — implementazione dell'interface `Agent` (Slice 0.9). `Loop.Turn` → `(*LlmAgent).Run(ctx) iter.Seq2[*Event, error]` (yield streaming chunks + tool_call events + lifecycle). `MaxSteps` rimane invariato. `Loop.Cancel` propaga via `InvocationContext.Ctx`. Saving stimato: −40 LOC (no plumbing custom per streaming, riusa Event/iter.Seq2).

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
`search.go`, `llm_agent.go` (`runTool`), `spec.go`. Lo stesso commit che porta il client
reale può/deve far landing del pattern: se atterriamo prima il client reale,
poi cambio firma → due commit ricontaminano gli stessi file, riopendendo
file ≤600 LOC e refactor-on-touch per ognuno. Slice 1 cresce di ~120 LOC
ma slice 2 e 3 risparmiano ~150 LOC ciascuno di plumbing duplicato.

**Smoke.**
```bash
# A1 amendment (2026-05-30): `aura chat` è un REPL interattivo multi-turn,
# NON un comando single-shot. La history è in-memory only — nessuna
# persistenza/auto-title/resume (quelli restano Phase 4 / Slice 1.8).
OPENROUTER_API_KEY=sk-or-... \
AURA_LLM_BASE_URL=https://openrouter.ai/api/v1 \
AURA_LLM_MODEL=deepseek/deepseek-v4-flash:exacto \
./aura chat
# poi al prompt REPL:
> ciao, dimmi 2+2 in tre parole
```
→ deve stampare una reply reale dal modello (non lo stub), via `text_response`,
streamata token-by-token, in <8 step. `/exit` o EOF chiude il REPL.

**Acceptance (machine-checkable).**

*Parte 1 — wire:*
- [ ] `go test ./internal/llm/...` passa con almeno 1 fixture SSE golden (tool-call multi-chunk + finish_reason="tool_calls", e plain text + finish_reason="stop").
- [ ] **A1 amendment**: `aura chat` è un REPL interattivo multi-turn (in-memory history only). Con config settata produce reply dal modello vero, streamata token-by-token; senza config (APIKey vuota) fallisce con messaggio chiaro (no panic, no fallback silenzioso). Persistenza/auto-title/resume restano Phase 4 (Slice 1.8).
- [ ] **OTel span per LLM call (amendment #9 + A2 amendment 2026-05-30)**: every LLM call emits an OTel span via `go.opentelemetry.io/otel/trace` against a **real configured exporter** (NOT the emit-only no-op provider the original PRD assumed). The OTel SDK (`go.opentelemetry.io/otel/sdk`) is initialized this slice with a `TracerProvider` whose exporter is env-gated: `AURA_OTEL_EXPORTER ∈ {stdout,otlp,none}` (default `otlp` → `localhost:4317` via `AURA_OTEL_ENDPOINT`, silent-drop without a collector); `none` = true no-op. Span name `llm.request`, attributes: `llm.model`, `llm.provider`, `llm.prompt_tokens`, `llm.completion_tokens`, `llm.cache_hit_tokens` (= `usage.prompt_tokens_details.cached_tokens`), `aura.request_id` (from `InvocationContext.RequestID`). The 8-byte `crypto/rand` SpanID (Phase 2 deferred) is minted full-tree this slice. Acceptance test: with an in-memory recorder, assert 1 span emitted per call, attributes populated, span_id stable across SSE chunks of the same call.
- [ ] **Budget contract enforcement (amendment #19 cross-ref)**: `LlmAgent.Run` checks `InvocationContext.RemainingSteps` and `RemainingWallclockDeadline` before each LLM call. Mid-Run trip → emit terminal Event with appropriate `FinishReason` ('max_steps' | 'max_wallclock' | 'dedup_loop'). Test: TestLlmAgent_StepCap_Trips, TestLlmAgent_WallclockCap_Trips, TestLlmAgent_DedupWindow_Trips.
- [ ] Cancel context (Ctrl+C) chiude la HTTP connection e drena il channel — verificato con `go test -race` + `go.uber.org/goleak` `goleak.VerifyNone(t)` in TestMain (audit round 2 P1: assert nessun goroutine residuo post-test, copre il caso SSE reader bloccato su `bufio.Scanner.Scan()` post-cancel).
- [ ] Zero allocazioni per `Message`-history mutation: il client legge `req.Messages` ma non lo modifica (test asserisce slice identica pre/post).
- [ ] **A3 amendment (2026-05-30) — cost honesty**: il costo USD per turn = `usage.cost` **reale** riportato da OpenRouter (sempre preferito, già netto di cache reads/writes/inference), con fallback a una **price-table statica** (seeded con `deepseek/deepseek-v4-flash:exacto`, override-abile via `prices` in `~/.aura/llm.json`) quando `usage.cost` è assente. Un model id sconosciuto riporta `n/a` (MAI `$0` — non si mente sul costo). Acceptance test: provider-cost presente → USD esatto; assente + model noto → USD da tabella; model sconosciuto → `n/a`.

*Parte 2 — ToolResult pattern:*
- [ ] `Tool.Execute(ctx, args)` ritorna `(ToolResult, error)`. `ToolResult{Preview string, FullPath string, Bytes int, Truncated bool}`.
- [ ] `(*LlmAgent).runTool` persiste `ToolResult` su disco se `Bytes > AURA_CONTEXT_PREVIEW_CAP_BYTES` (default 2048). Path = `$AURA_RUN_DIR/conversations/<conv_id>/<tool-call-id>.result` (post-Slice 1.8 layout; vedi sezione "AURA_RUN_DIR layout" sotto). La stringa che entra in `RoleTool.Content` è `Preview + "\n\n[truncated: N bytes total, full output at <FullPath>. Use read_tool_output to fetch ranges.]"`.
- [ ] Se `Bytes <= cap`, nessuna scrittura su disco; `RoleTool.Content = Preview` puro (no overhead).
- [ ] **A4 amendment (2026-05-30)**: Builtin tool `read_tool_output` (non-deferred) accetta `{tool_call_id, offset?, limit?}` dove `offset`/`limit` sono **BYTE** (non line-based — la semantica a righe originale era fragile su contenuto newline-free come un blob JSON da 100 KB). Default `limit≈2048` byte (allineato al preview cap). Ritorna la fetta di byte richiesta dal sidecar file con footer deterministico `showing bytes X-Y of Z, next offset Y` per paging dal modello. Hard-fail su tool_call_id ignoto.
- [ ] `text_response` continua a essere il terminale del loop: il suo `ToolResult.Preview` è la risposta finale all'utente (anche se `Bytes > cap`, la versione full sta sul disco; il preview va all'utente per default — il chiamante CLI/Telegram decide se servire la versione full).
- [ ] Test: un tool fake che ritorna 100 KB di output → il `Messages` history dopo `(*LlmAgent).Run` ha SOLO il preview (≤2 KiB + footer), file su disco ha 100 KB completi, `read_tool_output` recupera fetta arbitraria.
- [ ] **`$AURA_RUN_DIR` layout (Area #9 closed 2026-05-28)**. Default `$AURA_RUN_DIR = <os.UserCacheDir>/aura` (es. `~/.cache/aura`; fallback `<os.TempDir>/aura`) — **non `~/.aura/run/`** (corretto vs codice shipped `internal/config/config.go`, amendment #28). Layout:
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

**File targets** (totale ≤ 480 LOC src + ~200 test — saving −40 LOC riapplicato da Slice 0.9 amendment).

*Wire layer (~340 src + ~120 test):*
| Path | LOC stimato | Note |
|---|---|---|
| `internal/llm/openai_compat/client.go` | ~280 | `Client` impl, SSE parser, tool-call accumulator (delta-merge per `index`). Connect 10s, global timeout configurabile, no idle, no retry (vedi Open Questions). **Inietta `Config.LLM.Headers` su ogni request** (OpenRouter HTTP-Referer + X-Title). |
| `internal/config/config.go` | ~110 | `Config{LLM: LLMConfig{Provider, Model, BaseURL, APIKey, TotalTimeoutSec, Headers map[string]string}, RunDir, ToolPreviewCap}`. **Load order**: built-in default → `.env` (via `github.com/joho/godotenv`, key `OPENROUTER_API_KEY` → `LLM.APIKey`) → file JSON (`$AURA_CONFIG_DIR/llm.json`, default `~/.aura/llm.json`) → env vars (`AURA_LLM_*`, `AURA_RUN_DIR`, `AURA_CONTEXT_PREVIEW_CAP_BYTES`). **Default built-in**: Provider=`openrouter`, Model=`deepseek/deepseek-v4-flash:exacto`, BaseURL=`https://openrouter.ai/api/v1`, Headers=`{"HTTP-Referer": "https://github.com/chetto1983/aura", "X-Title": "Aura"}`. `Save()` per write-back dal dashboard futuro. |
| `internal/config/config_test.go` | ~50 | Load-order test: default < file < env. Round-trip JSON. |
| `internal/llm/openai_compat/client_test.go` | ~120 | Fixture SSE in `testdata/` per: text-only stream, tool-call multi-chunk (delta-merge), error 429 (no retry → bubble up), premature close (ctx-cancel), Anthropic ephemeral cache_control passthrough. Niente prompt da asilo nido (vedi §Test discipline). |

*ToolResult pattern (~180 src + ~80 test):*
| Path | LOC stimato | Note |
|---|---|---|
| `internal/agent/tools/result.go` | ~60 | `ToolResult{Preview, FullPath, Bytes, Truncated}`. Helper `NewToolResult(b []byte, cap int) ToolResult` che decide preview vs persist. Helper `Persist(dir, toolCallID string)` che scrive il file. |
| `internal/agent/tools/spec.go` (diff) | ~+5 / -3 | Firma `Execute(ctx, args) (ToolResult, error)`. |
| `internal/agent/tools/text_response.go` (diff) | ~+15 / -10 | Ritorna `ToolResult{Preview: text}` invece di stringa. |
| `internal/agent/tools/search.go` (diff) | ~+15 / -10 | Stesso adattamento. |
| `internal/agent/tools/read_tool_output.go` (NEW) | ~80 | Builtin non-deferred. Args `{tool_call_id, offset?:int default 0, limit?:int default ~2048 BYTES}` (A4 amendment 2026-05-30: byte-based, NON lines). Footer `showing bytes X-Y of Z, next offset Y`. Risolve path da `runtime`-mantenuto map `toolCallID → FullPath`. Hard-fail su id ignoto. Path-traversal-shaped ids (`..`/separators) rifiutati prima di `filepath.Join`. |
| `internal/agent/llm_agent.go` (diff) | ~+45 / -10 | `(*LlmAgent).runTool` riceve `ToolResult`, decide se persistere su disco se `Bytes > cap`, costruisce stringa per `RoleTool.Content` (preview + footer con path). **`read_tool_output` ri-deriva il path in modo deterministico via `sidecarPath(runDir, sessionID, toolCallID)` — NIENTE `resultPaths map` in-memory** (corretto vs questo testo originale: il design a map non è stato implementato, amendment #28). Crea `$AURA_RUN_DIR/conversations/<conv_id>/` lazy alla prima persist (post Slice 1.8: `conv_id` viene da `InvocationContext.SessionID`). File `loop.go` rinominato in `llm_agent.go` (Slice 0.9 amendment), `Loop` struct rinominato in `LlmAgent` (implementa `Agent` interface Slice 0.9). `Run() iter.Seq2[*Event, error]` sostituisce `Turn() (Result, error)`. |
| `internal/agent/tools/result_test.go` | ~80 | Test: 100 KB fake tool → `Messages` ha SOLO preview+footer, file ha 100 KB; `read_tool_output(id, offset=50000, limit=100)` recupera fetta. |
| `cmd/aura/main.go` (diff) | ~+50 / -15 | Sostituisce `stubClient` con `openai_compat.New(cfg.LLM)` (costruttore reale; non `NewOpenAICompat` — fix doc-verify 2026-06-01). Registra `read_tool_output` nel registry. Sub-comando `aura config` legge/scrive il file. |

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
| `internal/agent/llm_agent.go` (diff) | ~+110 / -10 | `(*LlmAgent).runTool` intercetta `ErrAwaitingUserInput` → costruisce `PausedState` con priority + (se proxied) `ProxiedFromChildID` → upsert in `aura.paused_states` → `Run` accumula pending → se ≥1 pending, yield `Event{Actions.Escalate=true, StateDelta:{pending: [...]}}` (Slice 0.9 escalation). Nuovi metodi `Resume(token, answer)` e `ResumeBatch(answers)`. Esclusività check su batch tool calls (multi-ask_user coalesce, altri tool drop). |
| `internal/db/queries/paused_states.sql` | ~70 | **6 query sqlc**: `InsertPausedState`, `GetByToken`, `ListPendingForLoop` (ordered), `MarkResumed`, `MarkResumedBatch`, `CleanupResumedOlderThan` (per future retention). |
| `internal/db/migrations/0003_paused_states.up.sql` | ~30 | `CREATE TABLE aura.paused_states (token uuid PRIMARY KEY, conversation_id text NOT NULL, kind text NOT NULL, question text NOT NULL, options jsonb, priority int NOT NULL DEFAULT 0, resume_context jsonb, tool_call_id text NOT NULL, proxied_from_child_id uuid, proxied_tool_call_id text, created_at timestamptz NOT NULL DEFAULT now(), resumed_at timestamptz)`. Indici su `(conversation_id, resumed_at) WHERE resumed_at IS NULL`. Foreign key a `conversations(id)` + promozione `conversation_id` a uuid + colonna `resumed_answer jsonb` aggiunti solo in Slice 1.8/migration 0005 (qui in 0003 `conversation_id` è plain text e `resumed_answer` non esiste ancora — indipendenza Slice 1.5 ↔ 1.8). |
| `internal/db/migrations/0003_paused_states.down.sql` | ~3 | `DROP TABLE aura.paused_states;`. |
| `internal/agent/tools/ask_user_test.go` | ~80 | Args validation, options polimorfismo, priority cap, sentinel error format. |
| `internal/agent/loop_pause_test.go` | ~160 | Pause+Resume singolo, ResumeBatch, multi-pause coalesce, priority sort, esclusività intra-turn, invalid token rejection, stub Responder. |
| `cmd/aura/main.go` (diff) | ~+60 | `aura chat` gestisce loop pause: stampa via `askuser.CLI`, raccoglie risposta (singolo/all/batch), chiama `Resume` o `ResumeBatch`. |

**Deferred-tool partition.** `ask_user` → **non-deferred** (sempre visibile, è infrastruttura del loop). Description corta (1 riga). Schema piccolo ma con `oneOf` per options polimorfico + `priority` int 0-100 — comunque sotto i 2 KiB.

**Open questions.**
1. **~~Persistent vs in-memory `PausedState`~~ → CHIUSA: persistent in Postgres da subito.**
   Slice 0.5 ha già lanciato il Postgres → `PausedState` vive in `aura.paused_states` table. Risolve crash-recovery, multi-istanza future-proof, audit trail di tutte le pause. Schema: `(token uuid pk, conversation_id uuid NOT NULL REFERENCES aura.conversations(id) ON DELETE CASCADE, question text NOT NULL, options jsonb, kind text NOT NULL CHECK (kind IN ('clarification','approval','choice')), priority int NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 100), resume_context jsonb, tool_call_id text, proxied_from_child_id uuid NULL, proxied_tool_call_id text NULL, created_at timestamptz NOT NULL DEFAULT now(), resumed_at timestamptz NULL, resumed_answer jsonb NULL)`. Indice su `(conversation_id, resumed_at) WHERE resumed_at IS NULL` per scan O(log n) della lista pending attive. `priority` + `proxied_*` aggiunti da Area #4 closed 2026-05-28 (multi-pause FIFO). `conversation_id` promosso da `text` a `uuid`+FK e `resumed_answer jsonb` aggiunti da Slice 1.8/migration 0005 (#15 multi-conversation persistence). (NB: `conversation_id` non è mai stato `loop_id` — fix doc-verify 2026-06-01.)
   Migration: `0003_paused_states.up.sql` aggiunta in Slice 1.5.
   File targets aggiunti: `internal/db/queries/paused_states.sql` (~70 LOC, 6 query sqlc: `InsertPausedState`, `GetByToken`, `ListPendingForLoop` ordered, `MarkResumed`, `MarkResumedBatch`, `CleanupResumedOlderThan` per future retention) + generated code via sqlc. `internal/agent/pending.go` (~70 LOC) usa il client sqlc invece di in-memory map. Test integrazione sotto build tag `db_integration`.
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
| `internal/identity/store.go` | ~80 | Thin adapter su sqlc `Queries`. La identity locale è il seed UUID `00000000-0000-0000-0000-000000000001` (migration 0004). (Il piano citava un helper `LocalIdentityID()` mai implementato — fix doc-verify 2026-06-01.) |
| `internal/identity/capability.go` | ~60 | `HasCapability(ctx, id, cap) (bool, error)` con wildcard logic + regex validation per `cap`. |
| `internal/db/queries/identity.sql` (+ `capability_grants.sql`) | ~60 | query sqlc: `GetIdentityByName`, `ListIdentities`, `CreateIdentity` (non `InsertIdentity`), `GrantCapability`, `RevokeCapability`, `ListCapabilities` — fix doc-verify 2026-06-01. |
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
- [ ] `paused_states.conversation_id` promosso da `text` a `uuid` + FK `aura.conversations(id) ON DELETE CASCADE` via `ALTER COLUMN ... TYPE uuid USING` (table vuota allo step 1.8, non drop+recreate). Aggiunge colonna `resumed_answer jsonb NULL` (era persa nel resume_context jsonb, ora esplicita per query). (NB: nessun `loop_id` preesistente — fix doc-verify 2026-06-01.)
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
- [ ] Identity scoping: ogni conversation è scoped a un identity (oggi sempre il seed `'local'` = `00000000-0000-0000-0000-000000000001`). `aura chat list` filtra `WHERE identity_id = <seed local>`. Future multi-user: filtra su identity autenticato.
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
| `internal/db/migrations/0005_conversations.up.sql` | ~70 | Tabelle + index + promozione `paused_states.conversation_id` text→uuid+FK (ALTER, non rename da loop_id) + aggiunta `resumed_answer jsonb` col. |
| `internal/db/migrations/0005_conversations.down.sql` | ~10 | DROP tables + rename back + drop col. |
| `internal/agent/llm_agent.go` (diff) | ~+80 / -20 | `(*LlmAgent).AppendMessage(msg)` ora persiste via `store.AppendTurn` invece di solo in-memory append. `(*LlmAgent).Stop()` chiama `store.AutoResolvePendings(convID)`. `(*LlmAgent).NewFromHistory(convID)` ricostruisce dalle turns. Cumulative budget cap warning: file `llm_agent.go` post-Slice 1+1.5+1.8+4+8+10 stimato ~520-580 LOC; se oltrepassa 600 LOC split in `llm_agent.go` (core Agent.Run) + `llm_agent_pause.go` (pause/resume) + `llm_agent_history.go` (AppendMessage/NewFromHistory/Stop). Refactor-on-touch enforcement. |
| `internal/conversations/store_test.go` | ~120 | Build tag `db_integration`. Round-trip + resume + cascade + auto-title. |
| `cmd/aura/main.go` (diff) | ~+30 | Wiring sub-commands + Cobra setup. |
| `cmd/aura/chat.go` (NEW) | ~60 | Sub-command `aura chat {list|resume|new|archive|unarchive|delete|rename}` estratto in file dedicato per pulizia (main.go cap 600 LOC). Stesso file viene poi rinominato/migrato in `internal/channels/cli/cli.go` da Slice 9a. |
| `cmd/aura/paused_states.go` (NEW) | ~30 | Sub-command `aura paused-states {list|purge}`. |

**Deferred-tool partition.** Niente tool LLM-facing in questo slice. È infra CLI + persistence. Future: tool `conversation_search`/`conversation_summarize` come deferred, scope dedicato.

**Open questions.**
1. **Cosa succede al `system` message?** → *Decisione*: il `system` message è il primo turn (seq=1, role='system'). Generato dal `PromptBuilder` (Slice 4) al primo turn. Su `resume`, ricaricato as-is. Se il system prompt cambia tra una session e l'altra (es. nuova skill installata), il vecchio system message della conv già esistente resta intatto — coerenza temporale. Future "system upgrade" è scope di una slice dedicata.
2. **Cache KV cross-conversation?** → *Decisione*: lascio a Slice 4 (KV cache) la valutazione. Stable-prefix di Slice 4 è system + tool manifest, che è identico cross-conv (se non cambiano le skill) → cache hit automatico. Niente refactor di Slice 4 per 1.8.
3. **~~Quanti turn conservare per il modello al resume?~~ → CHIUSA: layered context management Cursor-style (Area #17 closed 2026-05-28).**
   Decisione presa dopo ricerca pattern industriali (Claude Code 3-tier auto-compact, Cline middle truncation, Cursor dynamic context discovery, LangChain ConversationSummaryBufferMemory).
   *Scelta*: pattern **Cursor "dynamic context discovery"** puro. Aura ha già il 90% del lavoro fatto (Slice 1 ToolResult sidecar = stesso pattern). Implementazione minimale aggiunta a Slice 1.8:
   - **L1 — Microcompact tool result eviction (`internal/conversations/microcompact.go` ~60 LOC)**: su ogni `LoadHistory(conv_id)`, per i `role='tool'` turn con `seq < (max_seq - AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS)` (default 10), sostituisci `content` (preview da Slice 1) con un puntatore `"[tool_call_id=X: evicted from context, re-fetch via read_tool_output(X) — sidecar at $AURA_RUN_DIR/conversations/<conv_id>/<tool_call_id>.result]"`. Niente LLM call, cheap, riusa sidecar che già esistono.
   - **L2 — Budget dinamico (`internal/agent/budget.go`; path reale, non `internal/conversations/` — fix doc-verify 2026-06-01)**: calcolo Claude Code-style `hard_cap = Model.ContextWindow - max(MaxOutputTokens, AURA_CONTEXT_MAX_OUTPUT_TOKENS=20_000) - AURA_CONTEXT_RESERVE_TOKENS=13_000`. Warn cap `= hard_cap * 0.75`. Esempi (~): DeepSeek-V4 (1M) → ~1.003M hard / ~752K warn; OpenAI/Anthropic (200K) → 167K hard / 125K warn. Sopra warn → log WARN. Sopra hard → errore esplicito al `LlmAgent.Run` (ex `Loop.Turn`, amendment #28): "history exceeds hard cap; use `chat_compact` tool or `aura chat new` to start fresh".
   - **L3 — Full LLM-driven compaction: NON IMPLEMENTATA in Slice 1.8**. Skip esplicito: il modello 1M DeepSeek + pattern Ralph (stato in DB + Neo4j knowledge graph) rendono raro raggiungere il cap. Atterrerà come Slice futura opzionale (`chat_compact` tool LLM-facing + CLI `aura chat compact <id>`) se uso reale lo richiede.
   - **Token estimation**: usa `tiktoken-go` (cl100k_base BPE) come approssimazione fast. Errore tipico 5-10% vs actual provider tokenization, accettabile per gating.
   - **Modello info**: `ContextWindow` + `MaxOutputTokens` sono const del modello attivo in `internal/llm/config.go` (deepseek-v4: ContextWindow **1.048.576** + MaxOutputTokens **32.768**; NON `internal/llm/openai_compat/models.go`, che non esiste — fix doc-verify 2026-06-01). Il lookup multi-modello (gpt-4o/claude) è una feature futura non shippata.

   File targets aggiuntivi (~150 LOC):
   - `internal/conversations/microcompact.go` ~60
   - `internal/conversations/budget.go` ~50
   - `internal/conversations/store.go` (diff) ~+30 (LoadHistory chiama microcompact + budget check)
   - `internal/agent/llm_agent.go` (diff) ~+20 (passa ModelConfig al budget check)
   - `internal/llm/config.go` (ContextWindow + MaxOutputTokens come const del modello attivo; il file `openai_compat/models.go` del piano originale non è stato creato — logica consolidata in config.go, fix doc-verify 2026-06-01)

   Sources: [Claude Code auto-compact](https://claudelog.com/faqs/what-is-claude-code-auto-compact/), [Cursor dynamic context discovery](https://cursor.com/blog/dynamic-context-discovery), [Cline ContextManager](https://medium.com/@balajibal/dissecting-cline-cline-context-management-260aec3d84cb), [Context compaction research gist](https://gist.github.com/badlogic/cd2ef65b0697c4dbe2d13fbecb0a0a5f).
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
Migration 0005_conversations promotes paused_states.conversation_id
text->uuid with FK ON DELETE CASCADE (ALTER, not a loop_id rename) and
adds resumed_answer jsonb column for query-friendly audit.

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
stato conversazionale (Loop e' in-memory)" removed. Nota numbering:
la FTS di 1.8.5 ha preso 0006 (`0006_conversation_turns_fts`), quindi i
numeri assoluti delle migration a valle (scheduler/skill_audit/…) sono
indicativi e assegnati all'atterraggio per ordine-fase — vedi §Persistence
"Migration numbering — fonte di verità".

Sequencing rationale bullet 2.ter added.

LOC: +XXX src / +YY test / +ZZ migration.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Slice 1.8.5 — Conversation full-text search (pg_trgm GIN + aura chat search)

**Goal.** Aggiunge conversation full-text search a Slice 1.8 — index Postgres `pg_trgm` GIN su `aura.conversation_turns.content`, CLI subcommand `aura chat search "<query>"`, e Telegram command `/search` (Slice 9b reuse). Per amendment #7 (SUMMARY.md PRD Amendments table, 2026-05-29).

> **Atomicity note.** Sub-slice atomico ~80 LOC + 1 migration `0006_conversation_turns_fts.up.sql` (`CREATE EXTENSION pg_trgm IF NOT EXISTS` + `CREATE INDEX CONCURRENTLY conversation_turns_content_trgm ON aura.conversation_turns USING GIN (content gin_trgm_ops)`).

### Pre-requisiti

- Slice 0.5 Postgres infra (sqlc + golang-migrate)
- Slice 1.8 conversation persistence (`aura.conversation_turns` table)

**Smoke.**
```bash
aura chat search "specific phrase"   # → top-N excerpts ordered by similarity, p95 ≤ 50ms su 10K turns
```

**Acceptance.**
- [ ] Migration `0006_conversation_turns_fts.up.sql` crea l'estensione `pg_trgm` + GIN index `conversation_turns_content_trgm` su `aura.conversation_turns(content gin_trgm_ops)`; reverse in `0006_conversation_turns_fts.down.sql` droppa l'index + estensione (idempotent).
- [ ] sqlc query `SearchConversationTurns(query text, limit int) returns []ConversationTurn` usando `content % $1 ORDER BY similarity(content, $1) DESC LIMIT $2`.
- [ ] CLI subcommand `aura chat search "<query>" [--conversation <id>] [--limit N]` stampa `<conv_id> | <turn_seq> | <similarity_score> | <excerpt>` per row.
- [ ] Telegram `/search <query>` command aggiunto a Slice 9b commands MVP list (cross-slice reference; il binding atterra in Slice 9b).
- [ ] Cross-slice invariant test: Slice 9b `/search` ritorna risultati identici a CLI `aura chat search` per la stessa query.

**File targets** (~80 LOC src + ~30 test).

| Path | LOC | Note |
|---|---|---|
| `internal/db/migrations/0006_conversation_turns_fts.up.sql` | ~15 | `CREATE EXTENSION pg_trgm` + `CREATE INDEX CONCURRENTLY ... USING GIN (content gin_trgm_ops)`. |
| `internal/db/migrations/0006_conversation_turns_fts.down.sql` | ~5 | `DROP INDEX IF EXISTS conversation_turns_content_trgm;` + `DROP EXTENSION IF EXISTS pg_trgm;` (idempotent). |
| `internal/db/queries/conversation_search.sql` | ~10 | 1 sqlc query `SearchConversationTurns`. |
| `internal/conversations/search.go` | ~40 | `Search(ctx, query, opts) ([]TurnHit, error)` — wrappa sqlc query, handle conversation-scoped filter + limit. |
| `cmd/aura/chat.go` (diff) | ~+20 | Nuovo sub-command `aura chat search` (wiring spf13/cobra). |

**Open questions.** Nessuna (chiuse al momento dell'amendment).

**Migration numbering note.** `0005` in questa sub-slice è dopo `0004` (paused_states con resume_context, Slice 1.5) e prima di `0006` (scheduler, Slice 6). Verificare prima del commit di Phase 4 che il numero sia ancora disponibile (Slice 1.7 identity migration potrebbe occupare uno slot — re-numerare on conflict).

**Commit message template.**
```
slice 1.8.5: conversation FTS via pg_trgm GIN + aura chat search

Amendment #7 — adds full-text search across conversation_turns.content
via Postgres pg_trgm GIN index. CLI subcommand `aura chat search` +
Telegram /search command (Slice 9b reuse). ~80 LOC src + 1 migration.

LOC: +80 src / +30 test / +2 migration.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## §Phase 4 PRD Amendments (AM-01 / AM-02 / AM-03 — 2026-05-30)

> Grouped PRD-amendment block for the HITL + Identity + Conversations cluster
> (Slices 1.5 / 1.7 / 1.8 / 1.8.5), landed at the head of Phase 4 before any
> implementation (mirrors Phase 3's grouped amendments). Source: phase
> `04-CONTEXT.md` §PRD/SPEC Amendments Required, grounded in ADK-Go / LangGraph /
> picobot / MCP-elicitation research. Where these amendments and the per-slice
> prose above conflict, **these amendments win** for Phase 4.

- **AM-01 — `_history.go` is NOT in the agent (agent stays DB-free).** The earlier
  prose hinted at splitting `llm_agent.go` into `llm_agent.go` + `_pause.go` +
  `_history.go`. Amendment: only the pause-**detection** half lives in the agent
  (new `internal/agent/llm_agent_pause.go` — catch `tools.ErrAwaitingUserInput`,
  suppress the `RoleTool`, rewrite the persisted assistant message to
  `ask_user`-only tool_calls, emit `Actions.AwaitingInput`). `LoadHistory` is
  `conversations.Store.LoadHistory`, NOT a method on the agent; the Runner seeds
  the agent via the existing `LlmAgentConfig.UserTurns`. Pause persistence +
  resume orchestration live in the new `internal/runner.Runner`, never the agent.
  Industrial separation: ADK-Go / LangGraph / picobot all keep storage out of the
  agent leaf.

- **AM-02 — `aura.paused_states.resumed_answer` gains an action (MCP three-action).**
  The earlier prose modelled `resumed_answer` as plain `text` (accept-only).
  Amendment: it stores a JSON object `{action: accept|decline|cancel, content}`
  (MCP elicitation three-action model, D-A3-01). `accept` → inject `content` as a
  `RoleTool`; `decline` → inject a "user declined" `RoleTool` so the model adapts;
  `cancel` → abort the turn (reuses the Ctrl+C → `Runner.Stop` auto-resolve path).
  `decline`/`cancel` are inherent to any HITL pause. The `0005` migration that
  ALTERs `paused_states` therefore adds `resumed_answer` as `jsonb`, not `text`.

- **AM-03 — SPEC's `Loop.Turn` / `Loop.Stop` are renamed `Runner.Turn` /
  `Runner.Stop`.** The orchestrator that drives the conversation turn-by-turn and
  owns `conversation_id` is a new `runner.Runner` in `internal/runner/`, NOT an
  `Agent` and NOT named `Loop` — that would collide with the existing
  `internal/agent/workflow.LoopAgent` (a control-flow Agent). ADK-Go independently
  names this orchestrator `Runner`. SPEC's `Loop.Turn`/`Loop.Stop`/`Loop.Resume`/
  `Loop.ResumeBatch` verbs map to `Runner.Turn`/`Runner.Stop` +
  `askuser.Store.MarkResumed`/`MarkResumedBatch`.

**L2 budget inputs (resolves Phase-4 OPEN QUESTION 2).** The deterministic L2
budget formula `hard_cap = ContextWindow − max(MaxOutputTokens, 20000) − 13000`
needs two inputs that did not exist in `llm.Config`. Amendment: `internal/llm.Config`
gains `ContextWindow int` (default `1000000`, the ~1M DeepSeek-V4 window) and
`MaxOutputTokens int` (default `32768`), wired through the same 4-tier load order
via the fail-fast `envInt` helper under `AURA_MODEL_CONTEXT_WINDOW` /
`AURA_MODEL_MAX_OUTPUT_TOKENS`. The four conversation/context tuning knobs land in
`internal/config.Config` via the non-fatal `envIntDefault`:
`AURA_CONVERSATION_TURN_CAP_BYTES=65536`, `AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS=10`,
`AURA_HISTORY_HARD_CAP_TURNS=50`, `AURA_RUN_DIR_WARN_THRESHOLD_BYTES=1073741824`.

**Spillover is a sidecar FILE, not a table (resolves Phase-4 OPEN QUESTION 3).**
Conversation-turn content over `AURA_CONVERSATION_TURN_CAP_BYTES` spills to
`$AURA_RUN_DIR/conversations/<id>/<seq>.content` with `conversation_turns.content
= NULL` + `content_sidecar_path` set (reusing the Slice 1 `tools/result.go` path
scheme). There is NO `aura.conversation_spillover` table — the earlier CORE-04
mention of one is superseded by the locked SPEC sidecar approach.

**New dependency:** `github.com/pkoukk/tiktoken-go@v0.1.8` (cl100k_base token
estimation for L2 gating only, ~5-10% approximation — not billed accuracy).
Operator-verified for supply-chain legitimacy (real GitHub origin, 8 tagged
releases) before `go get`, per the Phase-4 package-legitimacy checkpoint.

---

## Slice 2 — Sandbox runner (Docker sidecar + seccomp + ulimit)

> **▶ CURRENT — amendment #50 / D-15c (full-terminal home, 2026-06-05; driver = direttiva utente "aura must do the same: a full terminal on sandbox with workspace and skills folder — no stupid security toys. Whatever Claude Code can do, Aura can do the same").** Il sandbox smette di essere una command-box lockata e diventa la **home a terminale pieno** di Aura — parità col toolbelt Bash+fs di Claude Code. **Estende #44/D-15b** (NON lo supersede): il transport resta `rivetdev/sandbox-agent`, un singolo container locale su loopback. Cambiano TRE cose. **(1) `sandbox_exec` → `shell_exec`**: shell piena e persistente (`sh -lc "…"`, cwd/env persistono cross-call, background job) sulla sandbox session-bound (Slice 2b) — non più un comando-per-POST-HTTP. **(2) Tool fs nativi in-process** `fs_read`/`fs_write`/`fs_edit`/`fs_grep`/`fs_glob` che operano sul **lato host del bind-mount** del workspace: zero HTTP per le operazioni su file (la shell nel container e i fs tool host vedono gli STESSI file). **(3) Mount**: il workspace **E la cartella skills (`$AURA_SKILLS_DIR`) montati RW** nella home (inverte spike 005 ro-mount). **Security toys RIMOSSI** — operatore singolo fidato sul proprio mini-PC, il threat model multi-tenant che giustificava l'isolamento NON è il deployment v1, e il costo era l'agente debole vs Claude Code: bearer token su ogni call (D-38 / spike 008 → `--no-token`), egress allowlist proxy (#40 / D-08 / spike 009), gVisor `runsc` default + seccomp allowlist positiva + `SandboxEscapeBench` Gate-3 (#32 / #36 / spike 010). Molti erano già deprecati di fatto dal pivot #44; #50 ne rende la rimozione **esplicita e totale**, e il fence `/workspace`-only è sostituito da workspace+skills montati nella home. **Cancella** il pending TODO `2026-06-05-restore-phase-8-sandbox-isolation-hardening.md` (era l'inverso esatto di questa decisione → `.planning/todos/cancelled/`). **Il container come boundary RESTA** solo perché è *dove Aura vive* (come l'host è dove vive Claude Code), non come gabbia: bind loopback preservato. **L'unica control non-toy preservata = capability_grants identity-scoped (Slice 1.7)**, dormiente per single-user (= full power), è il vero gate per-identità il giorno in cui Aura affronta Telegram/multi-utente — **obbligo di re-valutazione** della postura di sicurezza al passaggio multi-utente. Le AC di sicurezza di Slice 2a/2b sotto (seccomp / gVisor / escape-bench / network-proxy / token) sono **superate** da questo blocco; le AC funzionali (workspace mount RW, session-bound, persistenza cross-call) **restano** ed estese con lo skills-mount + i fs tool nativi. Toolset study completo: `docs/aura-toolset-design-claude-code-parity-2026-06-05.md`.

> **⚠ SUPERSEDED² — amendment #44 / D-15b (pivot-del-pivot, 2026-06-03; driver = direttiva utente "Phase 8 è chiusa, riscritta completamente" + "una sola immagine, niente bloat").** Anche il rimpiazzo **code-sandbox-mcp** dell'amendment #43 (sotto) è stato **superato**: la soluzione LANDED è **[sandbox-agent](https://github.com/rivetdev/sandbox-agent)** (rivetdev), un singolo container locale — Aura NON è più un MCP-bridge per il sandbox. `internal/sandboxagent.Client` fa POST HTTP a `sandbox-agent` su loopback (`AURA_SANDBOX_AGENT_URL`, default `http://127.0.0.1:2468`, endpoint `/v1/processes/run`) ed espone all'agente UN SOLO tool **non-deferred** `sandbox_exec` (`command`/`args`/`cwd`/`env`/`timeout_ms`/`max_output_bytes` → `stdout`/`stderr`/`exit_code`/`timed_out`/`*_truncated`/`duration_ms`). `sandbox_exec` è non-deferred di proposito: un E2E live con il tool deferred faceva infilare al modello l'intera command line dentro `command` (`"python3 -c …"`) → sandbox-agent 502 "failed to spawn" — il modello DEVE vedere lo schema `command`/`args` per chiamarlo (vedi anche Slice 8.1 Tool Search hardening). L'immagine `aura-sandbox-agent:py3` (build da `docker/sandbox-agent/Dockerfile` = sandbox-agent `-full` + python3) è avviata dall'operatore con `make sandbox-up` da immagine **preloaded** (`pull_policy: never` — NESSUN pull online); Aura NON provisiona/scarica NULLA al boot di chat (supersede l'auto-provision dell'#43). Posture di sicurezza: bind solo loopback, bridge Docker non-masquerading (no egress pubblico), nessun socket Docker montato, `mem_limit 2g`/`cpus 2.0`/`pids_limit 256`/`no-new-privileges`, volume `/workspace` persiste cross-call. Il bridge MCP→agent-tool generico dell'#43 (`internal/agent/mcptools` + client `internal/mcp`) RESTA in-tree come seam riusabile (attualmente NON montato) per montare futuri stdio MCP server. I delta di comportamento accettati dell'#43 restano validi (persistenza filesystem-only; no egress allowlist; `internal/scoring` retained). Provato e committato 2026-06-03 (`b98ddaff` "Use local sandbox-agent container" + `341e595e` CI fallout fix). Il testo #43 e il bespoke sotto restano come record storico del percorso pre-pivot.

> **⚠ SUPERSEDED — amendment #43 / D-15 (pivot, 2026-06-03; driver = direttiva utente "stop making a nuclear bomb. Aura is a product must work" + "this is the solution: one container and one MCP no bloat").** L'intera implementazione bespoke descritta sotto (sidecar Python `sandbox/sidecar.py`, seccomp allowlist gVisor-primary, `SessionManager`/reaper, host forward-proxy egress, tabella `aura.sandbox_sessions`, DinD `sandbox.yml`, `cmd/aura/{exec,sandbox,sandbox_proxy}.go`) è **deprecata e da rimuovere**. Rimpiazzo: **[code-sandbox-mcp](https://github.com/Automata-Labs-team/code-sandbox-mcp)** — un MCP server Go stdio (v0.0.30, windows-amd64.exe pubblicato) che possiede il lifecycle del container Docker e espone `sandbox_initialize`(→`container_id`)/`sandbox_exec`(container_id, commands[])/`write_file_sandbox`/`copy_file`/`copy_project`/`sandbox_stop` + un logs resource. Aura lo monta via un **bridge MCP→agent-tool GENERICO** (Shape B, scelto dall'utente 2026-06-03): un adapter riusabile che fa `tools/list` su QUALSIASI stdio MCP server (config Claude-Desktop-style `mcpServers`) e ne registra i tool come tool d'agente Aura di prima classe. È la visione platform-shaped (substrate domain-neutral + MCP wiring). **Provato end-to-end su Docker Desktop 2026-06-03** (probe `D:/tmp/csmcp`): newline-delimited JSON-RPC (stesso framing di `internal/knowledge/client.go`), `print(40+2)`=42 in container reale, filesystem persiste cross-exec, container removed da `sandbox_stop`. Funziona su Docker Desktop dove il bespoke `device=`-bind non poteva (no DinD/gVisor/device-bind richiesti). **Delta di comportamento accettati:** (1) persistenza Python in-memory cross-call ELIMINATA (ogni `sandbox_exec` è un `docker exec` separato → persistenza filesystem-only, scrivi file); supersede D-01/amendment #38 two-tier. (2) egress allowlist/SSRF proxy ELIMINATO (networking docker default); supersede D-08/D-09/D-10/amendment #40. Il modulo `internal/scoring/` (D-11/#41) resta — è cross-cutting, non sandbox-specifico. Ordine di rimozione: costruire+provare il bridge nel live `aura chat` PRIMA, cancellare il bespoke DOPO. Spawn del server con `-no-update`. Il testo sotto resta come record storico del design pre-pivot.

> **Atomicity note:** Sub-slice 2a (base stateless ~600 LOC, no deps esterne) + 2b (session-bound + workspace mount + network allowlist ~350 LOC, dipende da Slice 1.8 per `conversation_id`). 2a atterra prima di Slice 3 (Swarm); 2b atterra dopo Slice 1.8.

> **Pattern reference**: 2a è "sandbox per tool call" (snippet untrusted isolato, pattern OpenAI Code Interpreter MVP). 2b lo estende a "session-bound workstation" (state cross-call entro conversation, pattern Claude Code on the Web + E2B + Anthropic Code Execution beta). Aura supporta entrambi i mode via flag/arg, default rimane stateless per backwards-compat.

**Goal.** Implementare `sandbox.Runner` reale (in [internal/sandbox/sandbox.go](internal/sandbox/sandbox.go)
oggi è `Stub`) come HTTP client verso un sidecar Docker isolato. Esporre il
runtime al modello come tool `execute` (Deferred=true) che accetta `lang ∈ {python, shell}` + `code` (+ opzionale `session_id`) e restituisce stdout/stderr/exit_code/elapsed_ms.

> **D-20 — set di pacchetti curato baked at build-time, runtime resta net-none + read-only (`05-CONTEXT.md`; direttiva utente "voglio un sandbox come il tuo, non un giocattolo", 2026-06-01).** Il codice utente NON è più "stdlib only": il sidecar `Dockerfile` esegue `pip install` di un `sandbox/requirements.txt` curato e **version-and-hash-pinned** (numpy, pandas, scipy, sympy, matplotlib, pillow, beautifulsoup4, lxml, pyyaml, python-dateutil, openpyxl) a **IMAGE-BUILD time**, quando la rete È disponibile. I pacchetti vivono nei layer read-only dell'immagine; a **RUNTIME** il container resta `network_mode: none` + `read_only: true` + stateless e fa **ZERO pip a runtime** — `import numpy` funziona con zero rete, zero pip, zero cambio di postura di sicurezza. Distinzione preservata esplicitamente: il **server `sidecar.py` resta stdlib-only** (nessuna dipendenza Python per il server stesso — vedi file target `sandbox/sidecar.py`); solo il runtime del codice-utente guadagna le librerie baked (D-20a). `requirements.txt` è hash-pinned (`pip install --require-hashes` ideale) e ogni pacchetto passa il package-legitimacy audit; le lib C-extension girano sotto la seccomp allowlist (smoke `import numpy; numpy.arange(9).reshape(3,3)@…` sotto il runtime indurito, D-20b). **Il `pip install` arbitrario on-demand resta una capability 2b/Phase-8**, gated dalla network-allowlist `AURA_SANDBOX_NETWORK_ALLOW_HOSTS=pypi.org` (vedi il test 2b `pip install requests SUCCEEDS` più sotto — invariato): 2a consegna un toolkit fisso curato, 2b aggiunge gli install on-demand.

**Smoke.**

*Isolato (bypass agent loop, test del runner direttamente):*
```bash
docker compose -f compose.yaml up -d
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

> **D12 — primitiva di isolamento (RE-DECISO amendment #36, 2026-06-01; supersede amendment #32; driver = direttiva utente "voglio un sandbox come il tuo, non un giocattolo" su infra KVM-less; D-05/D-06/D-07 in `05-CONTEXT.md`; registro `.planning/DECISIONS.md`).** **Primario su x86 = gVisor `runsc`** (D-05): un user-space kernel che intercetta le syscall — il guest non raggiunge mai direttamente il kernel host. È l'isolamento più forte ottenibile **senza `/dev/kvm`** (che l'infra target non espone) ed è il modello di produzione di Modal per codice untrusted multi-tenant. È "un sandbox completo come quello di Claude Code" meno l'hypervisor. gVisor è quindi **default-on su x86**, NON più un seam di escalation attivato solo sopra il 5% (era così in amendment #32). **Floor portabile = container indurito + seccomp allowlist positiva + userns-remap** (D-06): applicato come defense-in-depth *dentro* gVisor su x86 E come boundary standalone di fallback su **arm64/DGX** finché gVisor-arm64 non è GA (preliminary/non-GA, 240/294 syscall, 4KB-page — la preoccupazione originale di D12, ancora valida per il floor). **Runner runtime-agnostico** (D-07): essendo un puro client HTTP (D-08), lo switch `runc`↔`runsc` è una concern di compose/daemon-runtime con zero modifiche Go; selezione per-arch via `AURA_SANDBOX_RUNTIME` (default risolve `runsc` su amd64, `runc` su arm64). Il profilo seccomp carica anche sotto `runsc` (belt-and-suspenders). **microVM (Firecracker/Kata) RESTA RIGETTATO**: richiede `/dev/kvm`, bloccato su Hetzner cloud + non confermato su DGX → fallisce la portabilità D00 in partenza; gVisor è il massimo achievable. **wazero = fast-path futuro** (in-process, no-Docker, honors `AURA_PRIVACY_MODE=local-only`) solo per snippet pure-compute; NON regge il matrice py/shell/js di 7e (no subprocess/socket/thread) → non è dipendenza di Slice 2. **Obbligo tracciato**: l'emulazione syscall QEMU su arm64 (CI) può divergere dal seccomp di un kernel arm64 reale → conferma su DGX reale resta obbligazione pre-produzione arm64.

**Acceptance.**

*Slice 2a (base stateless):*
- [ ] Sidecar gira come container non-root (`uid:gid 65532:65532`), `read_only: true`, `tmpfs:/tmp`, `network_mode: none`, ulimit nofile=64, cpus=1.0, mem=512m.
- [ ] **Seccomp = ALLOWLIST POSITIVA** (amendment #32 / PITFALLS Pitfall 1 — NON la vecchia deny-list di 7 syscall, che è più debole della default Docker ~44): `defaultAction: SCMP_ACT_ERRNO(EPERM)` + enumerazione esplicita dei ~80 syscall che Python 3.12 + bash richiedono. Profilo OCI **by-name** in `sandbox/seccomp.json` con `architectures: ["SCMP_ARCH_X86_64","SCMP_ARCH_AARCH64"]` (amendment #30: numeri syscall x86≠arm64 → mai numeri hardcoded; validato live su x86 **e** arm64). `unshare`/`ptrace`/`process_vm_readv`/`bpf`/`kexec_load`/`userfaultfd`/`mount` esclusi dall'allowlist. Test escape obbligatori: `ctypes…ptrace(...)` e `open('/proc/self/root/etc/shadow')` → EPERM/ENOENT, non il contenuto.
- [ ] **Hardening flags completi (amendment #32 / PITFALLS)**: oltre ai sopra, `cap_drop: ALL`, `no-new-privileges: true`, `pids_limit: 64`, userns-remap (o rootless Docker). **Gate-3 DoD**: `SandboxEscapeBench` escape-rate **<5%** misurato sul **profilo di produzione gVisor-primary x86** (amendment #36: gVisor `runsc` default-on x86, è il profilo contro cui gira l'escape-bench; il floor container+seccomp è bench-validato come fallback arm64), documentato in PHASE_REPORT.md.
- [ ] Default timeout esecuzione 30s, override via `AURA_SANDBOX_TIMEOUT_SEC` (cap 600s — ricorda [aura_lan_exposure_2026-05-17](memory)).
- [ ] `aura exec` con sidecar giù **E auto-start fallito** ("sidecar down AND auto-start fails → clear error", D-09, `05-CONTEXT.md`) → errore chiaro, no panic, no hang: alla connect-failure il runner tenta UN auto-start best-effort (`docker compose up -d aura-sandbox`), health-check, retry-once della chiamata, e solo allora restituisce il sentinel tipizzato `ErrSandboxUnreachable`.
- [ ] Container exit con stdout/stderr troncati a 1 MiB ciascuno (oltre → `... [truncated]`).
- [ ] Test integrazione marcato `//go:build sandbox_integration` salta se sidecar non raggiungibile (no flaky CI).
- [ ] Default execution mode: **stateless** (subprocess fresh per call). Pattern OpenAI Code Interpreter MVP.

*Slice 2b (session-bound + workspace + network — atterra dopo Slice 1.8):*
- [ ] **Session-bound containers**: tool `execute` accetta arg opzionale `session_id` (default = `conversation_id` dal `InvocationContext` Slice 0.9). Container manager (`internal/sandbox/sessions.go`) mantiene mappa `session_id → containerID`. Prima call con session_id crea container persistente; call successive riusano. TTL idle `AURA_SANDBOX_SESSION_TTL_SEC=1800` (30 min), reaper goroutine ogni 60s. Hard cap `AURA_SANDBOX_MAX_CONCURRENT_SESSIONS=5` per istanza Aura — sopra → errore esplicito (no LRU evict silenzioso).
- [ ] **Workspace mount RW**: directory `$AURA_RUN_DIR/conversations/<conv_id>/workspace/` mountata come `/workspace` dentro container session-bound (mount RW, owner uid:gid 65532). Permette persist di file generati dal code (CSV, immagini, intermediate output). Cleanup cascade con `aura chat delete <conv_id>` (Slice 1.8). Quota `AURA_SANDBOX_WORKSPACE_MAX_BYTES=104857600` (100 MiB per conversation), check pre-write via `du` periodico. Sopra quota → errore tool result + log.
- [ ] **Network policy granulare** (amendment #40, D-08 — host-side forward proxy, NON iptables): env `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` (CSV, es. `pypi.org,files.pythonhosted.org,data.gov`) configura allowlist. Default empty = deny totale (compat Slice 2a). Se non empty: l'egress è enforcato **fuori dalla sandbox** da un **forward proxy Go host-side** con **hostname-CONNECT allowlist** + modello **resolve-then-pin** (DNS-rebinding guard, riusa la SSRF machinery di Slice 5); il container resta egress-isolato e instrada solo il traffico opt-in al proxy (`HTTP_PROXY`/`HTTPS_PROXY`/`PIP_PROXY` puntano al proxy alla **bridge gateway IP**, non `127.0.0.1` container-local). **Le `iptables OUTPUT rules` / `network_mode: bridge` originali sono SUPERSEDUTE**: iptables/nftables richiedono `CAP_NET_ADMIN`, incompatibile con il floor `cap_drop: ALL`/D12; sotto gVisor iptables tocca solo il netstack virtuale (parziale), e far possedere il firewall al codice untrusted è l'anti-pattern. La wording CAP-02 "via iptables" è superseduta da questo modello host-proxy. Richiede **`scoring.ComputeSandboxTier(args)` (Risk-Based, sezione cross-cutting)**: se network_allow non vuoto al call → tier RISKY → gate consigliato. Hardcoded sample: `pypi.org` SAFE-bump (use case legittimo install), domains arbitrari → tier IRREVERSIBLE. **Default mai vuoto in produzione** = strict allowlist.
- [ ] **Persistence sandbox_sessions**: tabella `aura.sandbox_sessions` (id uuid pk, **conversation_id uuid fk conversations** `ON DELETE CASCADE` — PK di `conversations` è uuid, verificato 0005; `paused_states` ebbe la stessa promozione text→uuid, container_id text, image_digest text, started_at, last_used_at, status enum {active, idle, terminated, evicted}). Aura tracks all'avvio (recovery: status='terminated' per session active al boot, container ricreato lazy alla prima call). Pattern parity con `agent_job_runs` Slice 6.
- [ ] **Due tier di persistenza, NON una contraddizione** (amendment #38, D-01): il sidecar session-bound tiene un **interprete Python long-lived per `session_id`** (in-memory `x=42` set nella call 1 → leggibile nella call 2, onora prd.md:1252) come PRIMO tier; il **workspace mount RW** è il SECONDO tier (file persistono cross-call). I due claim di persistenza del PRD sono i due tier, non in conflitto. Estende il sidecar 2a `subprocess.run`-per-call. Asimmetria documentata al modello: vars Python in-memory persistono; shell `cd`/`export` NON persistono cross-call (solo il working dir API-managed è ri-applicato); i file workspace persistono per entrambi i linguaggi. Lo stato in-memory muore col container reaped (solo i file workspace + la row `sandbox_sessions` sopravvivono a un restart di Aura).
- [ ] **Carve-out docker-lifecycle contenuto** (amendment #39, D-05): il `SessionManager` shella `docker run`/`stop`/`rm` per il *lifecycle* del container per-session — ma l'*esecuzione* resta HTTP verso la porta del container per-session. L'invariante 2a "Go non guida mai il runtime Docker" è ristretta a "Go non guida mai l'*esecuzione* via docker; l'orchestrazione lifecycle è compito del control-plane" (precedente: D-09 già shella `docker compose up -d` per l'auto-start). **MUST NEVER mount the docker socket** dentro Aura o un sidecar (re-introdurrebbe il vettore di escape #1 di `SandboxEscapeBench` — config-regression a 0).
- [ ] **Symlink-escape guard host-side via `os.Root`/openat2** (amendment #42, D-13 — supersede `O_NOFOLLOW`): i walker host-side del workspace (`WorkspaceManager.walkSize` quota check + la cascade di `Conversations.Delete`) usano `os.Root`/`os.OpenInRoot` (openat2-based, rifiuta `..` + symlink che escono dalla root), strettamente più forte di `O_NOFOLLOW` letterale (CVE-2026-39861 è la shape esatta). La cascade delete è un walk openat no-follow manuale, NON `os.RemoveAll` (`os.Root` non ha ancora `RemoveAll`, golang/go#67002): pulizia ricorsiva di un albero attacker-controlled che rifiuta di seguire symlink fuori dalla workspace root.
- [ ] **`aura exec --session <conv_id>` flag** per CLI smoke session mode. Senza flag = stateless 2a comportamento, backwards-compat.
- [ ] Test integration session: 2 exec sequenziali su stesso session_id → stato preservato (var Python `x=42` nella prima call → leggibile nella seconda). Stesso session_id da 2 process Aura concurrent → strutturalmente serializzati via container lock (no race).
- [ ] Test integration workspace: `execute python "open('/workspace/a.txt','w').write('hello')"` → file persiste su host `$AURA_RUN_DIR/conversations/<id>/workspace/a.txt`.
- [ ] Test integration network: con `AURA_SANDBOX_NETWORK_ALLOW_HOSTS=pypi.org` → `pip install requests` SUCCEEDS, `urllib.request.urlopen('https://example.com')` FAILS con DNS/conn refused.

**File targets — Slice 2a** (≤ 600 LOC Go + Dockerfile/compose/seccomp materials):
| Path | LOC | Note |
|---|---|---|
| `internal/sandbox/docker.go` | ~220 | `DockerRunner` impl `Runner`. HTTP POST a sidecar `/exec/python` e `/exec/shell`. Timeout, truncate, ctx-cancel. **Path di esecuzione HTTP-only** (D-08: Go non guida mai il runtime Docker). **Auto-start one-shot (D-09)**: alla connect-failure tenta UN best-effort `docker compose up -d aura-sandbox`, health-check, retry-once, poi `ErrSandboxUnreachable`. L'auto-start è **docker-CLI-gated** (nessun binario `docker` sul PATH → skip diretto a `ErrSandboxUnreachable`) e **MUST NEVER mount the docker socket** dentro Aura — montare `/var/run/docker.sock` re-introdurrebbe il vettore di escape #1 che `SandboxEscapeBench` testa (config-regression a 0). |
| `internal/sandbox/config.go` | ~50 | Env: `AURA_SANDBOX_URL` (default `http://127.0.0.1:18901`), `AURA_SANDBOX_TIMEOUT_SEC`. |
| `internal/sandbox/docker_test.go` | ~120 | Integration test sotto build-tag `sandbox_integration`. |
| `internal/agent/tools/execute.go` | ~140 | Tool `execute` con `Deferred: true`. Schema: `{lang: enum, code: string, timeout_sec?: int, session_id?: string (Slice 2b)}`. Delega a `sandbox.Runner`. |
| `sandbox/Dockerfile` | ~30 | `FROM python:3.12-slim` + apt: bash, coreutils. USER non-root. ENTRYPOINT sidecar. |
| `sandbox/sidecar.py` | ~150 | Server HTTP minimo (stdlib `http.server`) con endpoint `/exec/python`, `/exec/shell`, `/session/{id}/exec/{lang}` (2b). `subprocess.run` con timeout. Trunc stdout/stderr. Niente deps Python extra. |
| `sandbox/seccomp.json` | ~80 | Allowlist positiva (`SCMP_ACT_ERRNO(EPERM)` default + ~80 syscall enumerati), multi-arch `SCMP_ARCH_X86_64`+`SCMP_ARCH_AARCH64`; blocca network/mount/ptrace/unshare (amendment #32). |
| `compose.yaml` | ~25 | Service `aura-sandbox`: build sandbox/, security_opt seccomp, network none (override-able per session 2b), read_only, ulimits. |
| `cmd/aura/main.go` (diff) | ~+60 | Subcommand `aura exec [--session <id>] <lang> <code>` + registrazione del tool `execute` nel registry. |

**File targets — Slice 2b** (~350 LOC src + ~150 test + ~60 migration):
| Path | LOC | Note |
|---|---|---|
| `internal/sandbox/sessions.go` | ~150 | `SessionManager`: spawn/reuse/idle TTL reap. `Acquire(sessionID) (*ContainerHandle, error)`, `Release(handle)`. Container lock per session_id (sync.Map + mutex) per serializzare exec concurrent. Reaper goroutine controlla `last_used_at < now() - TTL` ogni 60s. Hard cap concurrent enforced. |
| `internal/sandbox/workspace.go` | ~80 | `WorkspaceManager`: `EnsureDir(conv_id) string` crea/restituisce `$AURA_RUN_DIR/conversations/<conv_id>/workspace/` con owner 65532:65532. Quota check pre-write via `walkSize`. Cleanup cascade integrato con `Conversations.Delete` (Slice 1.8). |
| `internal/sandbox/network.go` | ~80 | Network policy (amendment #40, D-08 — host-side proxy, NON iptables): parse `AURA_SANDBOX_NETWORK_ALLOW_HOSTS`, **forward proxy Go host-side** con hostname-CONNECT allowlist + resolve-then-pin (riusa la SSRF machinery di Slice 5, deny-wins, glob domains). NON inietta `iptables` rules (richiederebbe `CAP_NET_ADMIN`, vietato dal floor `cap_drop: ALL`). Risk-Based tier calc `ComputeSandboxTier(args)` per sezione governance cross-cutting. |
| `internal/sandbox/sessions_test.go` | ~100 | Integration test sotto build-tag `sandbox_integration`: round-trip session preserve state, TTL reap, hard cap enforce, workspace persist + quota, network allowlist enforcement. |
| `internal/db/queries/sandbox_sessions.sql` | ~30 | **4 query sqlc**: `InsertSession`, `TouchLastUsed`, `MarkTerminated`, `ListActive` (per boot recovery). |
| `internal/db/migrations/0008_sandbox_sessions.up.sql` | ~50 | (numero **0008** — prossimo libero quando Phase 8 esegue: floor shippato è 0007, vedi §Persistence "Migration numbering — fonte di verità"; il "0010" indicativo è superseduto dalla regola ordine-fase) `CREATE TABLE aura.sandbox_sessions (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), conversation_id uuid NOT NULL REFERENCES aura.conversations(id) ON DELETE CASCADE, container_id text NOT NULL, image_digest text NOT NULL, started_at timestamptz NOT NULL DEFAULT now(), last_used_at timestamptz NOT NULL DEFAULT now(), status text NOT NULL CHECK (status IN ('active','idle','terminated','evicted')) DEFAULT 'active'`). Indici su `(status, last_used_at)` per reaper + boot recovery. |
| `internal/db/migrations/0008_sandbox_sessions.down.sql` | ~3 | `DROP TABLE aura.sandbox_sessions;`. |
| `cmd/aura/main.go` (diff) | ~+20 | `aura exec --session <conv_id>` flag, `aura sandbox sessions {list|terminate <id>|prune}`. |

**Deferred-tool partition.** `execute` → **Deferred=true** (description lunga + schema enum + esempi safety). `tool_search` lo carica on-demand.

**Pattern di riferimento** (verifica audit 2026-05-28):

| Tool | Stateful | Network | Filesystem persist | Use case primario |
|---|---|---|---|---|
| OpenAI Code Interpreter | ✅ session | ❌ deny | ✅ `/mnt/data` | Chat data analysis |
| Anthropic Code Execution (beta) | ✅ container | ❌ | ✅ tmpfs+volume | Tool inside chat |
| E2B Sandbox | ✅ long-lived | ✅ configurable | ✅ full | Sandbox-as-a-service |
| Claude Code on the Web | ✅ session | ✅ policy-based | ✅ full | Agent workstation |
| **Aura Slice 2a** | ❌ stateless | ❌ deny | ❌ tmpfs effimero | Snippet untrusted isolated |
| **Aura Slice 2b** | ✅ session-bound | ⚠️ allowlist | ✅ workspace mount | Multi-turn agent + data persist |

**Open questions.**
1. **Sidecar implementation language.** Python (zero-build, leggi+rispondi) o Go (single binary, no Python runtime in container)? → *Proposto: Python stdlib. Il sidecar è 1 file, niente deps, niente compile step, container minimo.*
2. **State tra exec.** → *Decisione (audit 2026-05-28)*: 2a stateless default, 2b session-bound opt-in. Entrambi i mode supportati dal runner stesso (dispatch via presenza `session_id`).
3. **Filesystem out.** → *Decisione (audit 2026-05-28)*: 2a tmpfs only (effimero per call). 2b workspace mount RW persiste cross-call entro conversation, cleanup cascade su `Conversations.Delete`.
4. **Network policy.** Deny totale o allowlist? → *Decisione (audit 2026-05-28)*: 2a deny totale (network_mode: none). 2b allowlist granulare via `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` CSV. Risk-Based governance: `pypi.org`-only = SAFE-bump, arbitrary domains = RISKY/IRREVERSIBLE tier.
5. **Windows host.** Docker Desktop su Windows è OK ma seccomp è solo Linux-container. Sviluppo locale Windows + container Linux → OK. Sviluppo locale Windows nativo (no Docker)? → *Proposto: no-fallback. Aura runs in container or against a Docker sidecar. Punto.*
6. **Network allowlist DNS cache TTL.** *Default proposto*: 5 min. Pre-merge: validare con corpus di test (es. `pypi.org` ha multiple A records che ruotano? Cache invalida call legittimi?).
7. **Sandbox session vs `swarm.Coordinator` child.** Quando un agent_job o swarm worker spawn-a una sub-loop che usa `execute`, riusa il container session della parent conversation o ne crea uno nuovo? → *Default proposto*: stesso session della parent (forwarda `InvocationContext.SessionID`). Forza isolation per child solo se RISKY tier explicito.

**Commit message templates (sub-slice 2a + 2b):**

```
slice 2a: sandbox runner — Docker sidecar with seccomp + ulimit + no-net (stateless)

Implements sandbox.Runner against an isolated Python sidecar
(read-only rootfs, tmpfs /tmp, network_mode none, seccomp default-deny,
ulimit nofile=64, cpus=1.0, mem=512m). Stateless: every call =
subprocess fresh. Pattern OpenAI Code Interpreter MVP.

Exposes `execute` tool (deferred) and `aura exec <lang> <code>` CLI
for smoke.

Smoke:
  aura exec python "print(2+2)" -> 4
  aura exec python "import socket; socket.socket().connect(...)" -> EPERM

LOC: +XXX src / +YY test / +ZZ sidecar+infra.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

```
slice 2b: sandbox session-bound + workspace mount + network allowlist

Extends Slice 2a stateless runner with session-bound containers
(state preserved cross-call entro conversation), workspace mount RW
($AURA_RUN_DIR/conversations/<id>/workspace/ -> /workspace), and
granular network allowlist (default deny, opt-in
AURA_SANDBOX_NETWORK_ALLOW_HOSTS CSV).

Pattern di riferimento: Claude Code on the Web + E2B +
Anthropic Code Execution beta.

Migration 0008_sandbox_sessions (aura.sandbox_sessions: id, conv_id
uuid fk CASCADE, container_id, image_digest, started_at, last_used_at,
status enum active|idle|terminated|evicted). Boot recovery: status
'active' al boot -> 'terminated' (container ricreato lazy alla prima
call).

SessionManager: spawn/reuse/idle TTL reap (default 1800s,
AURA_SANDBOX_SESSION_TTL_SEC). Hard cap concurrent 5
(AURA_SANDBOX_MAX_CONCURRENT_SESSIONS). Container lock per session_id
(serializza exec concurrent intra-session).

Workspace quota 100 MiB per conversation
(AURA_SANDBOX_WORKSPACE_MAX_BYTES). Cleanup cascade su
Conversations.Delete (Slice 1.8).

Network policy: host-side Go forward proxy (hostname-CONNECT
allowlist + resolve-then-pin, NON iptables — CAP_NET_ADMIN vietato dal
floor cap_drop:ALL). Risk-Based tier RISKY auto se allowlist
non vuota; SAFE-bump per pypi.org-only use case legittimo install
deps.

CLI: aura exec --session <conv_id> per smoke. aura sandbox sessions
{list|terminate|prune} per admin.

Smoke:
  conv_id=$(aura chat new)
  aura exec --session $conv_id python "x = 42"
  aura exec --session $conv_id python "print(x)" -> 42 (state preserved)
  aura exec --session $conv_id python "open('/workspace/a.txt','w').write('hi')"
  cat $AURA_RUN_DIR/conversations/$conv_id/workspace/a.txt -> hi
  AURA_SANDBOX_NETWORK_ALLOW_HOSTS=pypi.org aura exec --session $conv_id \
    python "import urllib.request; urllib.request.urlopen('https://pypi.org')"
    -> success
  AURA_SANDBOX_NETWORK_ALLOW_HOSTS=pypi.org aura exec --session $conv_id \
    python "import urllib.request; urllib.request.urlopen('https://example.com')"
    -> connection refused

LOC: +XXX src / +YY test / +ZZ migration.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Slice 3 — Swarm coordinator (v1 minimal: ParallelAgent + 2-deep cap; full bus/DM-by-ID/tier-mapped → v2 SWARM-V2-01)

> **Slice 0.9 amendment**: `Coordinator.Spawn` produce `LlmAgent` workers che il Coordinator wrappa in `ParallelAgent` built-in (Slice 0.9) quando spawn-a multipli concorrenti. Lo "shared message bus" e DM-by-ID restano custom (Aura semantic), ma l'esecuzione parallela degli workers usa `errgroup` + ackChan tramite `ParallelAgent.Run`. `AURA_SWARM_MAX_DEPTH=3` resta enforced. Saving stimato: **−200 LOC** (no plumbing custom errgroup, no Event/chan custom per worker output).

> **Amendment #12 (v1 scope reduction, 2026-05-29):** v1 ship-scope is ParallelAgent reuse from Slice 0.9 + hard cap `AURA_SWARM_MAX_DEPTH=2` (NOT 3; coordinator-internal constant `MAX_SPAWN_DEPTH=2`). NO DM-by-ID, NO tier-mapped models in v1. Tier env vars `AURA_SWARM_MODEL_{CHAT,REASONING,WORKER}` documented as v1 no-ops (all resolve to `AURA_LLM_MODEL`); enforcement of distinct tier routing deferred to v2 SWARM-V2-01. Rationale: SUMMARY.md Features over-scope flag + STATE.md Deferred Items table. Saving stimato: −300 LOC vs full v1 design (no bus, no DM router, no tier dispatcher).

> **Amendment #34 (D14 ripple D00/D12, 2026-06-01) — RATIFY + 3 fix.** Il modello di coordinamento è ratificato (fan-out + supervisor = pattern corretto 2026; budget condiviso `*atomic.Int32` D-10/D-11 già shippato bounda il costo totale dell'albero). Tre aggiustamenti da D00/D12: **(A) cap larghezza fan-out** — `MAX_DEPTH` bounda la profondità, il budget gli step, ma NIENTE i child simultaneamente vivi → `AURA_SWARM_MAX_CONCURRENT` (per-target, vedi catalogo env): senza, N child paralleli saturano RAM/FD del 32GB (viola D00.6). **(B) sandbox session reuse sotto D12** — i worker che riusano il `SessionID` parent (OQ7 #7) condividono lo stesso container session-bound: N `execute` paralleli **serializzano** sul lock per-session (isolamento OK, ma il parallelismo del fan-out NON si estende alla sandbox; per sandbox parallela serve spawn RISKY-tier = container dedicato). Da documentare nello smoke file-read come timing-aware. **(C)** lo stub `internal/swarm/swarm.go` dichiara ancora `MaxSpawnDepth = 3`: l'impl Slice 3 usa **2** (amendment #12). Privacy-mode è ereditato by-construction (un solo `llm.Client` condiviso dal Coordinator → nessun child può acquisire un endpoint remoto).

> **Amendment #44 (v1 final shape — Phase 9 D-01..D-25, 2026-06-04) — SUPERSEDE la wording sotto.** La discuss-phase (3 researcher pass + 2 spike live + ruflo study) ha congelato la forma industriale MINIMA ("no atomic bombs" — lezione Phase 8). **L'intera acceptance list + file targets + open questions QUI SOTTO sono STALE** (predatano amendment #12 + la discuss-phase) e vanno lette come superseduti dai D-row che seguono. Decisioni in `.planning/phases/09-swarm-minimal/09-CONTEXT.md` (D-01..D-25) + `.planning/DECISIONS.md` §8.
>
> **Tool surface (cut machinery RIMOSSA):** UN solo tool deferred `swarm_spawn {goals:[...]}` — blocking; fan-out interno wrappa il `ParallelAgent` shippato (Slice 0.9); ritorna un array ordinato di per-child report. **TAGLIATI da v1** (research-unanimi, → v2 SWARM-V2-01): `swarm_talk`, `swarm_join` come tool separato, il message bus broadcast+DM-by-ID, il dispatcher tier→model `tier.go` (le env `AURA_SWARM_MODEL_*` restano no-op documentate), la macchina `SpawnInteractive`/`ResumeChild`/`Responder` con children-map+RWMutex, lo spawn annidato (**flat v1** — i worker NON ricevono `swarm_spawn` nel loro registry; `AURA_SWARM_MAX_DEPTH=2` resta come code-guard forward-compat unit-testato). I file target `bus.go`/`tier.go`/`swarm_talk.go`/`swarm_join.go` sono CANCELLATI; la forma è l'ephemeral-runner D-16: `internal/swarm/{swarm,report,brief}.go` + `internal/agent/tools/swarm_spawn.go` (runner costruito dentro `swarm_spawn.Execute`, GC al return, ZERO cross-call state).
>
> **Pause-as-report (D-04, supersede la OQ5 Responder design):** un child il cui `ask_user` scatta **termina**; la sua entry di report = `{child_id, tool_call_id, status:"needs_user_input", question, options}` (rilevata dallo shippato `Event{Actions.AwaitingInput}`). Il parent LLM decide se rilanciare via il proprio `ask_user` (pausa durevole shippata) + re-spawn del goal arricchito con la risposta. NESSUN child parcheggiato, nessun `ResumeChild`/`Responder`, nessun pending volatile. `ask_user` guadagna gli optional `proxied_from_child_id`/`proxied_tool_call_id` (D-05, model-discretionary).
>
> **Goal come primo USER message (D-07, supersede OQ1 "system prompt parametrizzato dal goal"):** il brief Anthropic 4-part (objective, output format, tool guidance, task boundaries) viaggia in `messages[1]`/UserTurns, MAI in `messages[0]` (byte-stable → DeepSeek implicit cache ~90% dal worker #2). Full tool inheritance MINUS `swarm_spawn` (D-08/D-10).
>
> **Sandbox session-reuse wording superseduta (#34(B), D-14):** sandbox-agent è process-based, NESSUN lock per-session; `MAX_CONCURRENT` è l'unico cap (no per-tool semaphores in v1). Lo stub `MaxSpawnDepth=3` (#34(C)) è già CANCELLATO (dir `internal/swarm/` verificata vuota nel rewrite tabula-rasa).
>
> **Env catalog (D-13/D-11):** ADD `AURA_SWARM_MAX_GOALS=8` (cap goals-array → tool error) + `AURA_SWARM_CHILD_TIMEOUT_SEC=120` (per-worker ctx deadline → `{status:"failed", error:"timeout"}`). NESSUNA env `AURA_MCP_{MAIL,WHATSAPP}_SERVER` (D-21 supersede la lista del D-23: managed config `~/.aura/mcp/servers.json` via `aura mcp` è il path di registrazione — vedi §Caps & Limits nota MCP). Risolve RESEARCH OQ1.

**Goal (v1 — supersede da #44).** Implementare il runner swarm minimale: un tool deferred `swarm_spawn {goals:[...]}` blocking che fa fan-out dei goal come worker `LlmAgent` reali via il `ParallelAgent` shippato (Slice 0.9), con **pause-as-report** (D-04) al posto della macchina Responder, **flat v1** (D-10) con `AURA_SWARM_MAX_DEPTH=2` retained come code-guard forward-compat, e ritorno di un array ordinato di per-child report (D-15). ~~spawn di agenti figli (tier `chat|reasoning|worker`), shared message bus con broadcast E DM-by-ID, `Join(id)` che blocca fino a final report del figlio, payload summarizer al return-to-parent~~ → **CUT v1, vedi #44**.

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

**Acceptance.** *(STALE — superseduta da #44; la v1 acceptance vive nei ROADMAP SC#1..SC#5 re-specced + i D-25 property test. Le voci `Talk`/broadcast/`Join`/bus/tier/Responder qui sotto NON sono criteri v1.)*
- [ ] ~~`coordinator.Spawn(req)` con `Depth >= MaxSpawnDepth` → errore `spawn depth exceeded`~~ → **#44 D-10**: flat v1, i worker NON ricevono `swarm_spawn` (tool-not-available); il code-guard `AURA_SWARM_MAX_DEPTH` è unit-testato a depth sintetica ≥ cap.
- [ ] **Swarm budget inheritance (amendment #19 cross-ref Slice 0.9)**: `Coordinator.Spawn` propagates parent's REMAINING budget to spawned children via `InvocationContext.RemainingSteps` + `RemainingWallclockDeadline`. Acceptance test: parent has 20 steps remaining, spawns 3 children — total step count across the spawn tree ≤ 20 (NOT 20*3=60). Cross-link Pitfall #9.
- [ ] ~~`coordinator.Talk(...)` broadcast/DM-by-ID~~ → **CUT v1 (#44)**, → v2 SWARM-V2-01. Nessun bus.
- [ ] ~~`coordinator.Join(id)` blocca fino al final report~~ → **#44 D-01/D-15**: il blocking è il tool stesso (`swarm_spawn` ritorna quando tutti i child finiscono); no tool `Join` separato. Report = array ordinato per goal_index con per-child summary cap (overflow via lo shippato `tools.NewResult` sidecar).
- [ ] ~~Payload summarizer custom~~ → **#44 D-15**: nessun secondo meccanismo di spillover; riusa `tools.NewResult` (preview + sidecar + `read_tool_output`).
- [ ] ~~Tier → model mapping in `tier.go`~~ → **CUT v1 (#44 D-03)**: schema `{goals}` only, nessun `tier` param. Le env `AURA_SWARM_MODEL_*` restano no-op documentate (v2 SWARM-V2-01).
- [ ] Goroutine leak test (`go test -race` + `goleak`): dopo il return di `swarm_spawn`, nessuna goroutine child residua (D-25, **resta criterio v1**).
- [ ] ~~Bus capacity bounded / backpressure~~ → **CUT v1 (#44)**: nessun bus. La concorrenza è bounded da `AURA_SWARM_MAX_CONCURRENT` (waves interne, D-12).

**File targets** *(STALE — superseduta da #44 D-16. `bus.go`/`tier.go`/`swarm_talk.go`/`swarm_join.go`/`coordinator.go`+children-map sono CANCELLATI.)* La forma v1 (ephemeral runner, ≤ 600 LOC ciascuno):
| Path | LOC | Note |
|---|---|---|
| `internal/swarm/swarm.go` | ~250 | Ephemeral per-call runner: fan-out + waves (`AURA_SWARM_MAX_CONCURRENT`) + per-child isolation (D-02, bypassa il cancel-siblings di `ParallelAgent` riusando i suoi leak-safety idiom) + budget `Child()` shared `*atomic.Int32` + collect. Costruito dentro `swarm_spawn.Execute`, GC al return, ZERO cross-call state. |
| `internal/swarm/report.go` | ~120 | `ChildReport{goal_index, child_id, status: ok\|failed\|needs_user_input, summary, error?, question?/options?/tool_call_id?}` + collezione ordinata + JSON marshal + per-child cap (D-15). |
| `internal/swarm/brief.go` | ~100 | Builder del brief 4-part D-07 + worker-overlay literal D-06 (load-bearing literals). |
| `internal/swarm/swarm_test.go` + `swarm_property_test.go` | ~300 | unit + race + goleak (SC#1/#3/#4) + rapid properties (D-25). |
| `internal/agent/tools/swarm_spawn.go` | ~80 | Deferred=true. Args: `{goals:[...]}` only. Description = il literal anti-over-spawn D-24. |
| `cmd/aura/main.go` (diff) | ~+30 | `aura swarm-demo` subcommand (mock-LLM fixture, pattern `aura agent dry-run` 02-07). |
| `cmd/aura/main.go` + `mcp.go` + `mcptools/{bridge,mount}.go` + `ask_user.go` + `askuser/store.go` + `runner_persist.go` (diff) | — | Edit chirurgici D-19/D-20/D-21/D-05 (MCP recipes + allowlist + Deferred flip + fail-soft boot + proxied_* plumb). |

**Deferred-tool partition.** L'unico tool swarm v1 `swarm_spawn` → **Deferred=true** (description = il literal D-24: usa SOLO per ≥2 subtask indipendenti self-contained; ogni goal è un brief completo; il worker non vede la conversazione). `swarm_talk`/`swarm_join` CUT.

**Open questions.** *(OQ1 + OQ5 RISOLTE da #44; OQ2/3/4 confermate sotto.)*
1. **Spawn = nuova istanza `agent.Loop`?** → **RISOLTA (#44 D-07)**: sì, child è un `LlmAgent` reale, ma il goal viaggia come **primo USER message** (brief 4-part), NON come system prompt parametrizzato — `messages[0]` resta byte-stable (KV cache). Supersede "system prompt parametrizzato dal goal".
2. **Children share tools registry?** → **RISOLTA (#44 D-08/D-10)**: full inherit MINUS `swarm_spawn` (flat v1). `ask_user` resta (D-04 lo converte in report). Filtro custom = slice futura.
3. **Persistent state.** I figli scrivono in un store comune (Neo4j MCP)? → *Proposto: NO in questo slice. Coordinator è in-memory. Persistenza è una concern ortogonale.*
4. **Cancellazione.** Se il parent termina (Loop.Turn returns), i figli devono ricevere ctx-cancel? → *Proposto: SÌ. `Coordinator` ha un `parentCtx`; quando si chiude tutti i child loops droppano.*
5. **Child pause propagation (audit round 1 P0).** Cosa succede se un child `LlmAgent` ritorna `*PausedState` (perché ha invocato `ask_user`)? Equivalente Slice 0.9: il child emette `Event{Actions.Escalate=true, StateDelta:{paused_state_token: ...}}` invece di ritornare il payload, e il `ParallelAgent`/Coordinator deve gestirlo.
   → *Decisione*: ogni child `LlmAgent` spawn-ato dal Coordinator ha un proprio `Responder` configurabile.
   - **Default per swarm spawn**: `RejectingResponder` — il child non può pausare. Se chiama `ask_user`, il runtime riceve immediatamente `answer="<auto-rejected: child loop has no human responder>"`. Il child decide se procedere comunque o terminare.
   - **Override esplicito**: il parent può chiamare `Coordinator.SpawnInteractive(req, parentResponder)` per propagare la pausa al parent. Il parent ottiene una nuova `PausedState` con `proxied_from_child_id=<child_id>` + `proxied_tool_call_id=<child_original_tool_call>`. La pending si **accoda** alle eventuali altre pending del parent (lista FIFO con priority, Area #4 closed 2026-05-28): N child con `SpawnInteractive` in pausa simultanea sono permessi e contribuiscono ciascuno una PausedState alla lista del parent.
   - **`Coordinator.ResumeChild(childID, answer)`**: nuovo metodo. Necessario quando `SpawnInteractive` è usato e il parent ha raccolto la risposta dell'utente. Quando il parent risolve una pending proxied (anche con `answer="reject"`), il sistema chiama `Coordinator.ResumeChild(child_id, answer)` — il child Loop riceve la risposta via il proprio `Responder`, decide se procedere o cancellarsi (**no forced cancellation**: rispetta autonomy del child, audit logged). **Children map mutex-protected (audit round 2 P0):** ResumeChild + Spawn + Join condividono `sync.RWMutex` su `children map[string]*childState` — race su N child paused simultaneously rifiutata strutturalmente. Test `go test -race` + assertion N=10 child interactive paralleli paused+resumed senza data race.
   - **Acceptance addizionale**: test che assert deadlock guard — child spawned senza `Interactive` flag + child chiama ask_user → child termina con `answer=auto-reject` entro 100ms, parent `Join` non blocca.
   - **Acceptance multi-pause**: test con 3 child `SpawnInteractive` che pausano simultaneamente → parent ottiene `len(pending)==3` durante `(*LlmAgent).Run`, sortate per priority+FIFO; `LlmAgent.ResumeBatch` con 3 risposte (incluso 1 `reject`) → 3 `Coordinator.ResumeChild` invocazioni, ciascun child decide procedere/cancellare, parent riprende solo dopo che tutti i RoleTool sono accodati.

**Commit message template.**
```
slice 3: swarm coordinator — spawn, bus broadcast+DM, tier model

Implements swarm.Coordinator with in-memory child registry, shared
bus (channel-buffered, backpressure-bounded), tier→model mapping,
payload summarizer at return-to-parent, AURA_SWARM_MAX_DEPTH=2 enforced (v1 amendment #12).
Exposes swarm.spawn / swarm.talk / swarm.join tools (deferred).

Smoke: aura swarm-demo spawns 3 workers, broadcasts, DMs, joins all.

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Slice 4 — KV cache builder (stable-prefix + provider-aware)

**Goal.** Estrarre la costruzione del prompt da `agent.Loop.Turn` in un
`PromptBuilder` dedicato (`internal/agent/prompt/`, RELOCATED da `internal/llm/prompt.go` per D-01a — import-cycle) che garantisca:
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
- [ ] Test invariante (Slice 4 specifico): `agent.Loop.Turn(ctx, "anything")` chiamato 5 volte → `messages[0]` identico, lunghezza monotona crescente di history (no in-place mutation di entries precedenti).
- [ ] Test invariante: il tool manifest renderizzato 5 volte di seguito è byte-identico (cache poisoning guard, link a [reference_aura_cache_poisoning_sites_2026-05-27](memory) — prefix `reference_` corretto post-audit round 1).
- [ ] **Cross-slice forward-compat (amendment #11)**: `PromptBuilder.Build(history, tools, provider)` accepts optional `messages[2]` payload from `:AgentInsight` cache (Slice 11e injects). When Slice 11e is shipped, the PromptBuilder MUST treat `messages[2]` as a stable-prefix continuation (same byte-identity test extended to indices 0, 1, 2). Until 11e ships, `messages[2]` is absent and the test runs on `[0]` only. Acceptance test re-runs in Phase 15: assert SHA-256 of `messages[0]`, `messages[1]` (if Agent.md set), AND `messages[2]` (if Insight cache hit) ALL constant across 5 consecutive turns.
- [ ] **Invariants distintamente testati (Area #12 closed 2026-05-28)**. Slice 1 e Slice 4 e Slice 1.8 misurano cose diverse e tutti i test restano validi:
  - **Slice 1** (riga 270): `client.Request(req)` non muta `req.Messages` (slice identica byte pre/post). Garantisce che il wire layer è read-only sul client input.
  - **Slice 4** (questa slice): `PromptBuilder.Build(history, tools)[0]` byte-identico turn-su-turn. Garantisce che il system message è cache-friendly stable-prefix.
  - **Slice 1.8** (riga 660): `LoadHistory(conv_id)` ritorna `[]Message` byte-identico tra due chiamate consecutive. Garantisce che la rehydration dalla persistence è deterministica.
  Sono tre invariants ortogonali, ognuna ha il suo test dedicato. Slice 4 NON deprecha né rimpiazza Slice 1: si aggiunge come layer sopra. Slice 1.8 entra come producer della history input di Slice 4.

**File targets** (≤ 400 LOC):

> **PRD-amendment (D-01a) — relocation del PromptBuilder.** Il target originale `internal/llm/prompt.go` NON è realizzabile: `internal/agent/tools/manifest.go` importa `internal/llm`, quindi `internal/llm` NON può importare `internal/agent/tools` (per `RenderToolDefs()`) senza creare un import-cycle confermato. Il PromptBuilder vive perciò nel nuovo subpackage **`internal/agent/prompt/`** (builder.go + hash.go + cache_anthropic.go), che può importare sia `internal/llm` sia `internal/agent/tools`. Il file-target `internal/llm/cache_deepseek.go` è **DROPPATO** (D-02a / OQ1): il parsing del `usage` DeepSeek/OpenRouter che descriveva è già shippato in `internal/llm/openai_compat/sse.go` + `usage.go` — non resta nulla da costruire lì.

| Path | LOC | Note |
|---|---|---|
| `internal/agent/prompt/builder.go` | ~140 | `PromptBuilder` con `Build(history, tools, provider) []Message`. Stable-prefix discipline. RELOCATED da `internal/llm/prompt.go` (D-01a: import-cycle — `internal/agent/tools/manifest.go` importa `internal/llm`). |
| `internal/agent/prompt/hash.go` | ~40 | SHA-256 dei segmenti cacheable (index-set `{0}` oggi, `{0,1,2}` forward-compat amendment #11). Alimenta il CI invariant gate. |
| `internal/agent/prompt/cache_anthropic.go` | ~70 | Inietta `cache_control: ephemeral` su system + tool manifest (no-op seam sotto OpenRouter, D-03). |
| `~~internal/llm/cache_deepseek.go~~` | — | **DROPPATO (D-02a).** Il parse `usage.prompt_cache_hit_tokens`/`cached_tokens` + `usage.cost` è GIÀ shippato in `internal/llm/openai_compat/sse.go` + `usage.go`; nessun file-target residuo. |
| `internal/agent/prompt/cache_metrics.go` | ~80 | `Tracker.Record(turn, promptTokens, cachedTokens)` + persistenza per-turn su `aura.cache_metrics` (D-02) + `Report()` consumato da `aura cache-stats`. |
| `internal/agent/prompt/builder_test.go` | ~100 | Invariant test su 5-turn (hash stability, monotonic growth, no-mutation, cache_control presence). RELOCATED (D-01a). |
| `internal/agent/llm_agent.go` (diff) | ~-15 / +10 | Sostituisce inline `llm.Request{Messages: l.Messages, ...}` con `l.Prompt.Build(...)`. `LlmAgent` ora prende un `*PromptBuilder` invece di costruire il request inline. |
| `internal/llm/openai_compat.go` (diff) | ~+25 | Parsa `usage.prompt_cache_hit_tokens` dal final chunk SSE e lo espone su un campo `Chunk.Usage`. |
| `cmd/aura/main.go` (diff) | ~+40 | `aura chat-loop` (REPL multi-turn) + `aura cache-stats`. |

**Deferred-tool partition.** Nessun tool nuovo. Pure plumbing interno + misurazione.

**Open questions.**
1. **~~Provider detection~~ → CHIUSA: provider primario = OpenRouter (vedi Slice 1 OQ1).**
   OpenRouter è OpenAI-compat ma fa pass-through verso il modello sottostante (DeepSeek v4 flash :exacto). Le ottimizzazioni cache:
   - **Anthropic `cache_control: ephemeral`** non si applica (DeepSeek non lo supporta).
   - **DeepSeek auto-cache nativo** è preservato anche via OpenRouter: la response include `usage.prompt_cache_hit_tokens` (formato OpenAI-compat esteso). Il parser deve gestirlo come optional field.
   - **OpenRouter aggiunge `usage.cost`** (USD totale della call) — utile da loggare in `cache.Tracker` accanto al hit-rate per misurare ROI reale del KV-discipline.
   *Decisione:* `Config.LLM.Provider="openrouter"` è una stringa documentale (per log/UI). Il client OpenAI-compat è invariante. Le routine cache_control restano in `cache_anthropic.go` (no-op in pratica) per non rompere se in futuro aggiungiamo un secondo provider Anthropic-diretto. **Seam portabilità D00 (amendment #30)**: l'endpoint (`AURA_LLM_BASE_URL`) + model sono già config-driven — è il contratto day-1 che tiene aperti i 3 target (remoto cloud/32GB, locale vLLM/llama.cpp su DGX). L'`LLMRouter` di Slice 13 estende questo seam (selezione runtime), ma nessun path deve mai hardcodare il provider/endpoint.
2. **Cache stats persistenza. → CHIUSA (D-02, override del precedente "in-memory only"): persist per-turn su Postgres.**
   Le metriche cache atterrano per-turn in una nuova tabella `aura.cache_metrics` (`conversation_id`, `seq`, `ts`, `prompt_tokens`, `cached_tokens`, `cost_usd`), così `aura cache-stats --since=<window>` è una vera query SQL time-windowed (es. `--since=1h`), non un dump in-memory volatile. Nuova migration **0007** nella sequenza esistente (0006 `conversation_turns_fts` è l'ultima shippata → 0007 è il prossimo slot libero). **Override esplicito del vecchio "Proposto: in-memory, stats sono debug"** — richiede questo PRD-amendment-commit PRIMA di qualsiasi codice (PRD-first). Nessun lavoro wire-layer: il dato sorgente è GIÀ shippato in `llm.Usage{PromptTokens, CompletionTokens, CachedTokens, Cost}` (D-02a — `prompt_tokens_details.cached_tokens` + OpenRouter `usage.cost`); il Tracker consuma il trailing `Usage` chunk già emesso da `internal/llm/openai_compat/sse.go` + `usage.go`.
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

## §Cross-cutting — Context rot mitigation policy (amendment #21, Pitfall #4 P0)

**Context rot è il primary failure mode degli agent loops** (Chroma research 2025-26: −30%+ accuracy in mid-window positions across 18 frontier models). Aura adotta una **policy 5-layer** anti-context-rot dichiarata top-level:

| Layer | Mitigation | Slice owner | Cosa fa |
|---|---|---|---|
| **L0** | KV cache discipline | Slice 4 (CAP-04, Phase 6) | `messages[0]` byte-identical cross-turn (system+manifest+tools). `messages[1]` mutable Agent.md profile (Slice 10). Cache hit ratio target ≥ 80% su DeepSeek-V4 / Anthropic. CI invariant audit `scripts/cache_invariant_audit.sh` cross-slice. |
| **L1** | Microcompact in-context eviction | Slice 1.8 (CORE-04, Phase 4) | Tool result age-based eviction: `RoleTool` messages older than `AURA_MICROCOMPACT_TTL_TURNS=10` → replaced with `[evicted, sidecar at <path>]` stub (reference Slice 1 ToolResult sidecar). Preserva structure, riduce token cost. NO LLM call. |
| **L2** | Budget trim history | Slice 1.8b (CORE-04, Phase 4) | Hard cap su history = `model_window - max(MaxOutputTokens, AURA_CONTEXT_MAX_OUTPUT_TOKENS) - AURA_CONTEXT_RESERVE_TOKENS` (subtract-reserves, §L4643; NON `pct × window` — fix doc-verify 2026-06-01) → on exceeded: drop oldest user/assistant pairs. Notify user + suggest `aura chat new` per fresh KV cache. |
| **L2.5** | **Hard rolling buffer safety net (amendment #22, picobot-inspired)** | Slice 1.8b (CORE-04, Phase 4) | **Fallback deterministico se L1+L2 falliscono** (summarization timeout/rate-limit, token estimation off-by-margin, eviction insufficient). Cap `AURA_HISTORY_HARD_CAP_TURNS=50` (picobot default). Post-L1+L2 attempt, se `estimated_tokens > 100% × model_window` ANCORA: drop oldest user/assistant **pair** (keep system L0 stable + preserve invariant `len(history) % 2 == 0` per pair-coherence) fino a fit. **Zero LLM call**, deterministic, fail-safe. Pattern reference: [picobot internal/session/manager.go `MaxHistorySize=50`](https://github.com/louisho5/picobot/blob/main/internal/session/manager.go). Aura aggiunge pair-aware drop (vs picobot string-flat) + invariant check (oldest pair = first non-system role=user message + immediately-following role=assistant message). Audit row `aura.context_rot_events {ts, conv_id, action='hard_drop_pairs', pairs_dropped, tokens_before, tokens_after}` per forensics. |
| **L3** | Tool result sidecar | Slice 1 (CORE-01, Phase 3) | Large tool outputs (`output_bytes > AURA_TOOLRESULT_PREVIEW_BYTES=4096`) → write `$AURA_RUN_DIR/conversations/<id>/<tool_call_id>.result` + ToolResult.Preview (first 4 KB + suffix `[truncated, full at <path>]`). LLM sees only preview, full retrievable on demand. |
| **L4** | Archival memory retrieval | Slice 11 (UX-06/07/08/09, Phase 15) | Long-term facts via Neo4j vector + fulltext: `:Chunk` (raw docs), `:Entity` (resolved nouns), `:AgentInsight` (extracted insights). Top-K relevant injected by `:AgentInsight` TTL cache (preserves L0 `messages[2..n]` stability per Slice 11e amendment #11). Tool `memory_search(query)` self-edit pattern (Letta-inspired). |

**Targets quantitative (Anthropic 2026 best practice)**:
- **Operative threshold**: 60% context utilization → trigger PROACTIVE compaction (L1+L2). Manual proactive produce summary migliore di reactive (model ha clear recall).
- **Hard cap**: 80% context utilization → AUTO trigger compaction + Notifier alert utente. Proceed o fail-loud (configurable via `AURA_CONTEXT_HARDFAIL_ON_OVERCAP=1`).
- **Time-based opportunistic**: at every "major milestone" → conversation archive boundary (CORE-04), agent.Run() completion (Slice 0.9 LoopAgent terminal Event), conversation_turn boundary post-LLM-response.
- **Formula budget (shipped — fonte autorevole §L4643)**: `hard_cap = model_context_window - max(MaxOutputTokens, AURA_CONTEXT_MAX_OUTPUT_TOKENS) - AURA_CONTEXT_RESERVE_TOKENS`. Default DeepSeek-V4: `1.048.576 - max(32.768, 20.000) - 13.000 ≈ 1.002.808` hard cap (finestra **1.048.576** token canonica — verificata su OpenRouter `deepseek-v4-flash:exacto`, amendment #28; NB **131.072 ≈ 128K è il max OUTPUT** del modello, non la finestra — probabile origine del refuso "128k"). Gli env `AURA_LOOP_BUDGET_PCT`/`AURA_OUTPUT_RESERVE_TOKENS`/`AURA_SYSTEM_PROMPT_TOKENS_BOUND` di questa formula **NON sono shippati** — superseded dai knob `AURA_CONTEXT_RESERVE_TOKENS`/`AURA_CONTEXT_MAX_OUTPUT_TOKENS` (fix doc-verify 2026-06-01).

**Industry pattern references (verified 2026-05-29)**:
- [Anthropic compaction docs](https://platform.claude.com/docs/en/build-with-claude/compaction): `compact_20260112` server-side, drop pre-summary blocks pattern. **Adopted in spirit (L1+L2 client-side)**, NOT direct API param (multi-provider compat: OpenRouter passa Anthropic-only params provider-specific).
- [LangGraph state+reducers 2026](https://callsphere.ai/blog/langgraph-state-management-typeddict-reducers-state-channels): TypedDict State + Reducers + PostgresSaver checkpointing. **NOT adopted as runtime pattern** — Aura `Actions.StateDelta` è forward-compat placeholder only; reducer logic concreto in Phase 4 (conversation state) o Phase 15 (memory). Rationale: low-parallelism single-user MVP, scope creep avoidance Phase 2 cornerstone.
- [Letta MemGPT 3-tier (Core/Recall/Archival)](https://sureprompts.com/blog/letta-memgpt-walkthrough): RAM/disk-cache/cold-storage analogy + agent self-edit tools. **Partially adopted**: Core = Agent.md (Slice 10 messages[1]), Recall = `conversation_search` FTS (Slice 1.8.5), Archival = Neo4j (Slice 11). Pattern self-edit tool dispatch via Slice 11 tool LLM-facing `memory_search`. NO Letta-style runtime adoption (Aura ha proprio Agent interface stolen-not-imported da adk-go).
- [mem0 passive fact extraction](https://vectorize.io/articles/mem0-vs-letta): LLM-driven extraction post-turn + vector store + 90% token saving claim. **Pattern adopted in Slice 11e (UX-09 agent journal)**: cross-conv pattern analyzer extract `:AgentInsight` from `:AgentEpisode` via LLM-driven extraction. NO mem0 library import (Python + commercial license terms).
- [Chroma context-rot paper via Morph](https://www.morphllm.com/context-rot): −30% mid-window degradation quantified, 18 frontier models. **Empirical baseline**: Aura mette L0+L1+L2+L3+L4 layer-defense per stay BELOW degradation cliff.

**Cross-slice acceptance criteria (cross-cutting CI gate)**:
- [ ] `scripts/cache_invariant_audit.sh` (Slice 4 amendment #16) asserts `messages[0]` byte-identical across 20-turn replay → L0 enforced.
- [ ] `scripts/microcompact_smoke.sh` (Slice 1.8) asserts tool result evicted after `AURA_MICROCOMPACT_TTL_TURNS` turns + sidecar accessible → L1 enforced.
- [ ] `scripts/budget_trim_smoke.sh` (Slice 1.8b) asserts hard cap 80% triggera summarization OR drop strategy → L2 enforced.
- [ ] `scripts/toolresult_sidecar_smoke.sh` (Slice 1) asserts >4 KB output → sidecar written + preview only in LLM context → L3 enforced.
- [ ] `scripts/archival_recall_smoke.sh` (Slice 11d) asserts `memory_search(query)` returns top-K from Neo4j + injected as `:AgentInsight` block in messages[2..n] → L4 enforced.
- [ ] Cross-slice integration test `make context_rot_drill` (CI job) replays 50-turn conversation, asserts target 60% utilization mid-conversation + final assistant accuracy ≥ baseline.

**Failure mode + observability**:
- Slog event `context_utilization` per LLM call con tag `{conversation_id, layer_active, tokens_used, tokens_limit, pct}` → dashboard PromQL `histogram_quantile(0.95, aura_context_utilization_pct)` < 0.6 target.
- Notifier alert quando `pct > 0.6` (proactive) o `pct > 0.8` (reactive auto-compact triggered).
- Audit row `context_rot_event` con `{ts, conv_id, trigger_layer, action_taken, before_tokens, after_tokens}` per forensics.

**Out of scope (context rot mitigation)**:
- Real-time fine-tuning on conversation patterns (out of scope: Aura non fine-tune local models)
- Cross-conversation deduplication (out of scope: privacy + complexity, deferred a future milestone)
- Provider-specific compaction params (out of scope: Aura multi-provider via OpenRouter, params Anthropic-only non passa-through reliable)

---

## §Cross-cutting — KV cache invariant CI (amendment #16, Pitfall #3 P0)

Cache poisoning is the highest-risk cross-slice failure mode in Aura. Slice 4 owns the invariant (`messages[0]` byte-identical turn-su-turn) but Slices 1.8, 5, 7e-core, 10, 11e all mutate the message-construction pipeline. Without a cross-slice gate, every capability silently risks planting its own poisoning site. Reference: `reference_aura_cache_poisoning_sites_2026-05-27` (6 sites mapped pre-rewrite — historical record kept as warning).

### Architectural rule: THREE-segment cacheable prefix (amendment #29)

- `messages[0]`: byte-identical turn-su-turn. Contains: system prompt + tool manifest (alphabetically sorted by name, per Slice 4). NO per-turn data, NO timestamps, NO conversation_id, NO identity_id strings.
- `messages[1]`: stable per-identity. Contiene SOLO il profilo `Agent.md` per-identity (Slice 10); cambia solo quando l'utente edita il profilo. NON contiene `:AgentInsight` (vedi `messages[2]`).
- `messages[2]`: cached-TTL prefix. Contiene il blocco top-K `:AgentInsight` (Slice 11e injection, cached per amendment #11 via `AURA_AGENT_INSIGHT_CACHE_TTL_SEC`). Byte-stable entro la finestra TTL → terzo segmento cacheable. **CHIUSA (amendment #29)**: index pinnato a `[2]`, NON appeso a `messages[1]` — Agent.md e Insight hanno domini di stabilità/invalidazione distinti e restano segmenti di prefisso separati.
- All conversation turns: `messages[3..N]`.

Any future slice that wants to inject context MUST land in the cacheable prefix at the appropriate stability tier (`messages[1]` per-identity, `messages[2]` cached-TTL) or in the dynamic tail (`messages[3..N]`) — NEVER mutate `messages[0]`, e NEVER co-locare payload con domini di stabilità diversi nello stesso indice. Violation = automatic Slice 4 invariant break.

### CI gate `scripts/cache_invariant_audit.sh`

- Authored in Phase 6 (Slice 4 implementation).
- 20-turn replay against a fixture conversation. Computes `SHA-256(json.Marshal(messages[0]))` per turn. Asserts all 20 hashes identical.
- On failure: explicit error `messages[0] mutated at turn N — diff: <previous-hash> vs <current-hash>; offending diff hint: <first 200 bytes of unified diff>`.
- Runs in CI on every PR from Phase 6 onward. PRs that break the script fail with the explicit error above.
- Exit codes: `0` (pass), `1` (mutation detected — gate failure), `2` (fixture corrupt — re-run).
- File targets: `scripts/cache_invariant_audit.sh` (~80 LOC bash) + `scripts/fixtures/cache_invariant/turn-{01..20}.json` (replay turns) + CI workflow integration `.github/workflows/ci.yml` step `name: cache invariant gate`.

### Cross-slice acceptance bullets (already present per-slice — this section enumerates them for audit)

- Slice 0.9 InvocationContext composition: confirm SessionStore / GraphStore / LLMClient injection is by-pointer (no per-call alloc).
- Slice 1 LLM client: `client.Request(req)` does NOT mutate `req.Messages` (read-only contract).
- Slice 1.8 LoadHistory: deterministic byte-identical reads.
- Slice 4 PromptBuilder: SHA-256(`messages[0]`) constant — the anchor invariant. Enforced by `scripts/cache_invariant_audit.sh` (amendment #16).
- Slice 5 web_fetch: no system-prompt mutation when results returned.
- Slice 7e-core skill snippet execution: skill body sourced from disk-cached SKILL.md, NOT inlined per call.
- Slice 10 Agent.md injection: lands at `messages[1]`, NOT `messages[0]`.
- Slice 11e :AgentInsight injection: cached per amendment #11, lands at `messages[2]` (third prefix segment). **CHIUSA (amendment #29)**: canonical builder order pinnato a `[0]`=system+tools, `[1]`=Agent.md, `[2]`=cached `:AgentInsight`, `[3..N]`=history+canvas tail. NON appeso a `messages[1]` (vedi §1588).

### Pitfall #3 mitigation summary

Pitfall #3 (Audit-table TRUNCATE bypass) is covered by amendment #17 — role separation (`aura_app` lacks TRUNCATE, migrations gated via `AURA_DB_MIGRATE_URL`) plus dual triggers (`BEFORE UPDATE/DELETE` + `BEFORE TRUNCATE`). Pitfall #2 (cross-slice KV cache poisoning) is covered by this section + the CI script. Living-doc gate amendment #20 (`docs/aura-quality-snapshot.md` + `scripts/quality_snapshot_gate.sh`) is the complementary forward-looking gate — Pitfall #3 catches structural mutation, amendment #20 catches measurement regression.

---

## §Test discipline (vale per tutte le slice)

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

### Test discipline rigorosa — niente asilo nido (estensione 2026-05-28)

Oltre alla regola "no nomi tool nel prompt E2E", ogni slice rispetta una **soglia minima di rigore** sui test che ne convalidano la `Definition of Done`. Il rigore è proporzionale al rischio: persistence + governance + sandbox = max rigore; CLI cosmetic = rigore standard.

**Hard requirements per ogni file `*_test.go` committato:**

1. **Naming**: `TestXxx_Behavior_When_Condition` (descrittivo, no `TestFoo1`, `TestFoo2`).
2. **Setup teardown**: niente shared state cross-test salvo build-tag `_integration` con DB transactionally rollback'd. Race detector verde (`go test -race ./...`).
3. **Goroutine leak check**: `goleak.VerifyNone(t)` in TestMain o `defer goleak.VerifyNone(t)` per test che spawn goroutine. **Per amendment #15 (2026-05-29) the mandate is extended to ALL packages che spawn goroutine** — non solo le slice 1/3/6/8/9/11/13 originali. Lista completa pre-Phase 0 closure: Slice 0.9 (workflow agents Parallel + Loop), 1 (HTTP client SSE reader), 1.5 (ask_user resume goroutine), 1.8 (microcompact background), 2a/2b (sandbox reaper goroutine), 3 (swarm ParallelAgent), 4 (no goroutine — skipped), 5 (web_fetch HTTP), 6 (scheduler dispatcher + heartbeat + cron tick), 7c (TTL sweeper if implemented), 7e-core (TTL archive sweeper), 8 (AG-UI SSE emitter), 9a (setup wizard HTTP + SSE pump), 9b (telegram bot polling), 9c (multimodal sidecar HTTP), 10 (interview LoopAgent — no extra goroutine), 11b/d/e (ingest pipeline + memify background workers; 11c community DEFERITA fuori MVP, amendment #27), 11f (Task Canvas — no goroutine extra), 13 (offline detector + cost tracker — v2). Every `*_test.go` for these packages MUST call `goleak.VerifyNone(t)` in TestMain OR `defer goleak.VerifyNone(t)` per test.
4. **Fixture realistic**: `testdata/*.{json,csv,md,sse,sql,html,pdf}` con contenuto realistic (estratto da casi reali pseudonimizzati), no `{"foo":"bar"}` placeholder.
5. **Property-based dove indicato**: PromptBuilder invariants, AG-UI translator coverage event types, swarm backpressure → `gopter` o `rapid` library. Vincolato a slice 4/8/3.
6. **Build tags integration**: `//go:build db_integration`, `//go:build sandbox_integration`, `//go:build multimodal_integration`, `//go:build onboarding_integration`. CI runner separato, no flaky-on-CI mainstream.
7. **No `time.Sleep` non-determinismo**: usare `synctest` (Go 1.24+) o channel sync. Wait condition con timeout esplicito 5s + fail-loud (no infinito).
8. **Coverage threshold per package**: ≥ 75% unit (`go test -cover ./internal/...`), ≥ 60% integration. Fail CI sotto threshold (no skip silenzioso).
9. **Mutation testing spot-check**: 1 invocation per slice di `go-mutesting` o equivalent su core file (`llm_agent.go`, `coordinator.go`, `pipeline.go`, ecc.) — score minimo 70% killed. Run manuale, non CI.
10. **Failure-driven test**: per ogni bug fixato durante implementation, un test che lo reproduce **prima** del fix (TDD reverse).

**Esempi concreti per slice:**

| Slice | Test "asilo" da rifiutare | Test rigoroso atteso |
|---|---|---|
| 1 (LLM client) | `assert reply == "4"` | Fixture SSE multi-chunk delta-merge + tool-call accumulator + ctx-cancel premature close + `goleak.VerifyNone` |
| 2a (Sandbox) | `aura exec python "print(2)" → 2` | Subprocess time + memory + stdout truncation 1 MiB + **seccomp allowlist-positiva load verify** + escape tests `ptrace(...)`/`open('/proc/self/root/etc/shadow')` → EPERM/ENOENT (non il contenuto) + `SandboxEscapeBench` escape-rate <5% (Gate-3, amendment #32) |
| 2b (Session sandbox) | Single session reuse | 3 session concurrent, hard cap enforce, TTL reap deterministico via `synctest`, workspace quota enforce, network allowlist via host-side forward proxy verify (allowed host CONNECT succeeds, non-allowed host connection refused; D-08), symlink-escape guard via `os.Root` (D-13) |
| 3 (Swarm) | `coordinator.Spawn(2) → 2 children` | Wall-clock parallelismo `<` 1.5x singolo (race detector enforced), 10 child interactive paused simultanei senza data race, multi-pause FIFO priority sort verify |
| 4 (KV cache) | `messages[0] == messages[0]` | `usage.prompt_cache_hit_tokens > 0` da turn 2, byte-exact comparison via hash, property-based su manifest ordering |
| 5 (Web tools) | `web_fetch("google.com") returns html` | SSRF protection (loopback denied, allowlist enforced), redirect chain max 5, content-type sniffing, robots.txt respect verify |
| 6 (Scheduler) | `task.fire after 10s` | `FOR UPDATE SKIP LOCKED` concurrency 5 workers, crash recovery `unknown_recovery` row, `ReschedulesOnRecovery` selective re-fire verify |
| 7c (Skill mutation) | `skill_create writes file` | Tx rollback su INSERT fail (FS-move reversed), audit row immutable (UPDATE/DELETE rejected via trigger), `approval_source` constraint coherence enforced |
| 8 (AG-UI) | `event.type == "TEXT_MESSAGE_CONTENT"` | AG-UI Dojo conformance suite full run, property-based on all ~25 event types, backpressure SSE channel cap 64 + drop with `RUN_ERROR` |
| 9b (Telegram) | `bot.send("hello")` | Throttle 1500ms/500ms/1000ms enforce con `synctest`, 429 backoff exponential up to 30s, golden fixture per ogni AG-UI event type → Telegram message |
| 10 (Onboarding) | Interview 1 question | LoopAgent max_iter=8 cap enforce, escalation event terminate, fact extraction recall on conv corpus (precision ≥ 0.7), audit profile_audit row con paused_state_token |
| 11b (Ingest) | `ingest.file(pdf) returns ok` | Content_hash idempotent, mem0 2-fase conflict dedup (95% recall on duplicate entities), entity type taxonomy coverage 100% |
| 11d (Retrieval) | `memory.search returns 5 chunks` | Weighted-RRF fusion correctness vs baseline (BM25-only / vector-only / graph-only) + scale-invariance (BM25 e vector con scale diverse → ranking stabile, no normalizzazione, amendment #26), re-ranker quality NDCG@5 ≥ 0.8 su corpus eval |
| 13b (vLLM+LMCache) | `vllm responds` | KV cache hit ratio > 30% turn 2-5 su long-context (>4K token prompt), failover offline detection switch entro 90s, cost tracker rolling 24h accuracy |

**Test che NON è "asilo nido" ma è LECITO** (smoke fast-feedback):
- 1-3 smoke test per slice che gira in < 5s, no rigor su edge case
- Compile + go vet + go build always green pre-commit
- Niente sostituisce i rigorous test sopra: smoke complementare, non alternativo

---

## §Slice Q&A discipline (gate qualità per slice, vale per tutte)

> **Regola assoluta (dichiarata 2026-05-28)**: **senza PRD completo non si scrive una riga di codice**. Ogni slice attraversa 3 gate Q&A formalizzati. Niente shortcut, niente "lo aggiusto dopo", niente "il PRD si capisce dal codice".

Ogni slice (e sub-slice) passa attraverso **3 gate sequenziali**: Definition of Ready (DoR), Implementation Q&A continuous, Definition of Done (DoD). Nessun commit di codice atterra senza tutti e 3 verdi.

### Gate 1 — Definition of Ready (DoR) — *PRE-implementazione*

La slice può essere implementata SE e SOLO SE tutti questi punti sono verdi nel PRD:

- [ ] **Pre-requisiti completati**: tutte le slice predecessor (sequencing rationale) implementate, mergiate, smoke verdi. Verifica `git log` su master, no slice "in progress" upstream.
- [ ] **Open questions chiuse**: ogni OQ della slice ha decisione esplicita (non solo "default proposto" — *deciso* dall'owner). Se OQ è "pre-merge benchmark", il benchmark è eseguito e risultato documentato nel PRD.
- [ ] **Acceptance criteria machine-checkable**: ogni `- [ ]` in §Acceptance è verificabile con un test concreto. No "il sistema deve essere robusto" (non testabile), sì "test `TestLoopRunCancel_NoGoroutineLeak` con `goleak.VerifyNone`" (testabile).
- [ ] **Smoke E2E definito e runnable**: la sezione §Smoke ha comandi shell eseguibili che producono output atteso. No "verifica manuale che funzioni".
- [ ] **File targets stimati**: ogni file ha LOC range stimato, no file > 600 LOC, refactor-on-touch documentato per file pre-esistenti.
- [ ] **Test plan documentato**: § Acceptance enumera quali test (unit, integration, smoke, property-based) coprono ogni acceptance bullet. No coverage 0% acceptance.
- [ ] **Risk-Based tier assegnato** (se la slice introduce tool LLM-facing): SAFE/NORMAL/RISKY/DESTRUCTIVE chiarito + giustificazione + gating policy.
- [ ] **Migration scriptato + down.sql** (se DB schema cambia): up + down + idempotente + index strategy + tx wrapping documentato.
- [ ] **Env vars catalogate**: nuove env aggiunte in Caps & Limits indice + default + tipo (cap/operative/path/secret).
- [ ] **Mini-PC RAM delta** stimato: se sidecar/process aggiunto, impatto su RAM table dichiarato.
- [ ] **Commit message template** scritto: imperative subject + body con "perché" + Co-Authored-By trailer.

**Gate 1 conclude con**: PRD section della slice firmata (esplicitamente "DoR ✅ <date>" in commit message PRD update). Solo allora si crea il branch implementation.

### Gate 2 — Implementation Q&A continuous — *DURANTE implementazione*

Ad ogni commit della slice (atomic, smoke verde):

- [ ] **`go vet ./...`** verde
- [ ] **`go build ./...`** verde
- [ ] **`go test ./internal/<package>/`** verde
- [ ] **`go test -race ./...`** verde su package toccati
- [ ] **Refactor-on-touch eseguito**: per ogni file editato, dead-code rimosso, dupl-folding applicato, LOC ≤ 600 verificato (`wc -l`), comments aggiornati allo stesso commit.
- [ ] **Niente test "asilo nido"**: rivedi i test scritti contro la tabella esempi §Test discipline rigorosa. Se un test non resiste alla rilettura (banale, scripted, no edge case), riscrivilo.
- [ ] **Niente `// TODO` lasciati**: tutti TODO menzionati nel commit message + issue tracker (anche se single-user). No TODO orphan.
- [ ] **No `panic`** salvo bug genuinamente unrecoverable: error wrapping sempre, contextual.
- [ ] **Niente env var hard-coded**: ogni magic number/path passa da `internal/config/` o env. Nessun `"localhost:8080"` letterale in business logic.
- [ ] **3-strike rule rispettata**: stesso failing approach max 3 volte. Strike 3 → fermarsi e chiedere all'owner (PRD-update o pivot).
- [ ] **No modify test per farli passare**: se test fallisce per bug nel code, fixare code. Se test è genuinamente sbagliato, riscriverlo + giustificarlo nel commit message.

**Gate 2 fail mode**: smoke red → revert commit, ripartire. Mai forzare push con smoke red.

### Gate 3 — Definition of Done (DoD) — *POST-implementazione (pre-merge)*

La slice è DONE SE e SOLO SE:

- [ ] **Tutti i `- [ ]` di §Acceptance ticked**: ogni bullet verificabile con test concreto, output documentato (snippet log, screenshot Telegram, JSON response). No "credo funzioni".
- [ ] **Smoke E2E end-to-end green**: tutti i comandi §Smoke girati su clean state, output matching atteso.
- [ ] **Integration tests passing**: build tag rispettati (`db_integration`, `sandbox_integration`, ecc.). Skip-on-no-container documentato ma test esiste.
- [ ] **Regression suite green**: slice precedenti non rotte. `go test ./...` full run (incluso build tags) verde.
- [ ] **Coverage threshold raggiunto**: ≥ 75% unit, ≥ 60% integration sul package nuovo. `go test -coverprofile=cover.out ./internal/<package>/` + `go tool cover -func=cover.out` output ≥ threshold.
- [ ] **Mutation testing spot-check** (slice critical): 1 file core sottoposto a `go-mutesting`, score killed ≥ 70%. Documentato in commit message o issue.
- [ ] **No goroutine leak**: `goleak.VerifyNone(t)` verde su test che spawn goroutine.
- [ ] **No data race**: `go test -race` verde su tutti i package toccati.
- [ ] **Documentation update**: PRD aggiornato se ha richiesto deviazioni dal piano (no "il PRD si capisce dal codice" — il PRD È la verità).
- [ ] **Commit message conforming**: imperative + perché + LOC final + Co-Authored-By trailer. Niente "fix" o "wip" mainstream.
- [ ] **Branch ready for merge**: rebase su master, conflict risolti, 1 commit atomic per slice (o N per sub-slice con atomicity nota).
- [ ] **Quality snapshot update (amendment #20)**: if the slice changes any code under a `docs/aura-quality-snapshot.md` CI gate path, the corresponding row's `Last measured` + `Last value` MUST be updated in the same commit. CI gate `scripts/quality_snapshot_gate.sh` runs in every PR; stale row blocks merge. Cross-reference user memory `feedback_aura_as_product` ("max 2 fasi staged avanti, ogni wave gate su numeri").

**Gate 3 conclude con**: PR merge-ready, owner approval esplicita "DoD ✅ <date>". Niente merge unilaterale.

### Q&A revision protocol (cosa fare quando una slice scopre buchi nel PRD)

Durante implementation può emergere che il PRD ha:
- Un buco architettonico
- Un'open question non chiusa
- Una dipendenza non vista
- Un edge case non considerato

**Protocollo standard:**

1. **Stop implementation** (no patch quick & dirty)
2. **Documenta il buco** in commit PRD-amendment: "Slice X: durante implementazione di Y scoperto Z, decisione W per ragione V"
3. **Aggiorna il PRD** con la nuova decisione (con date + reason)
4. **Ri-run Gate 1 DoR** per la slice affected
5. **Resume implementation**

**No silent decision**: ogni decisione architettonica che devia dal PRD passa per commit PRD-update con date e reason esplicite. Il PRD è la truth, non un suggerimento.

### Q&A escalation (quando l'owner non è disponibile)

3-strike rule + escalation chain:
1. Strike 1-2: ritenta con approccio diverso
2. Strike 3: stop, redigi report (cosa hai provato, cosa hai osservato, ipotesi per pivot), aspetta owner
3. Se owner non risponde in 24h: report PRD-update con default proposto + procedi con "tentative" flag. Owner conferma o pivota a review.

**Niente "ho deciso da solo perché mi sembrava giusto"**. Il PRD è la decisione collettiva, le deviazioni sono visibili.

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
`compose.yaml` di Slice 2). HTML→Markdown via `codeberg.org/readeck/go-readability/v2` (readeck fork — go-shiori upstream deprecated 2025-12-05 per amendment #3)
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
- [ ] Raw-body download ceiling: `AURA_WEB_FETCH_MAX_BODY_BYTES = 5_000_000` (5 MB; era `AURA_WEB_RESPONSE_CAP_BYTES = 24000`, rinominato + ri-defaultato 2026-06-02 dopo il Gate-3 di Slice 5 — il vecchio default 24KB, dimensionato come un numero markdown, rigettava ogni pagina reale con `response_too_large` PRIMA dell'estrazione readability). Hard limit anti-DOS del body HTTP grezzo applicato in `gateAndRead` prima di qualunque preview/sidecar logic. Distinto da `AURA_CONTEXT_PREVIEW_CAP_BYTES` (preview/sidecar threshold del markdown LLM-facing via `tools.NewResult`) per semantica.
- [ ] Timeout HTTP: SearXNG 20s, direct_fetch 30s, entrambi config-overrideable.
- [ ] Readability filter: pagine con <250 char di main content → ritornano `{warning: "low-content page"}` invece di noise.
- [ ] **Riusa il `ToolResult` pattern di Slice 1**: web_fetch su page grande (>2 KiB) → preview + sidecar file; modello può fare `read_tool_output(id, offset, limit)` per estrarre fette.

**File targets** (≤ 550 LOC src, no file >300):
| Path | LOC | Note |
|---|---|---|
| `internal/web/searxng.go` | ~130 | Client HTTP puro: `Query(ctx, params) ([]Result, error)`. Da pre-rewrite quasi 1:1. |
| `internal/web/fetcher.go` | ~120 | `Fetch(ctx, url) (Page, error)`. HTTP con `safeDialContext` SSRF defense. |
| `internal/web/html.go` | ~90 | `ExtractMarkdown(html []byte) (title, contentMD string, links []Link)`. Wrapper su `codeberg.org/readeck/go-readability/v2` (readeck fork, amendment #3) + `html-to-markdown/v2`. |
| `internal/web/config.go` | ~40 | `Config{SearXNGURL, FetchTimeoutSec, SearchTimeoutSec, MaxChars, AllowLoopback, AllowHosts []string}`. Lette da `internal/config` (esteso). |
| `internal/agent/tools/web_search.go` | ~70 | Deferred. Args→`web.SearXNG.Query`→`ToolResult`. No business logic qui, è un thin adapter. |
| `internal/agent/tools/web_fetch.go` | ~80 | Deferred. Args→`web.Fetcher.Fetch`→`ExtractMarkdown`→`ToolResult`. |
| `internal/web/searxng_test.go` | ~80 | Fixture JSON SearXNG response → test parser. |
| `internal/web/fetcher_test.go` | ~100 | SSRF tests (loopback/private IP rejected), readability filter. |
| `compose.yaml` (diff) | ~+15 | Aggiunge service `searxng` (image `searxng/searxng`), shared network col sandbox. |
| `cmd/aura/main.go` (diff) | ~+15 | Registra i due tool. |

**Cosa NON riportare dal pre-rewrite.**
- L'aggregazione `SearchTool` con 12 azioni (search/list/read/lessons/user_facts/god_nodes/subgraph/path/diff/gaps/surprises/suggest_questions). Quelle 10 azioni "memory/wiki" appartengono a una slice futura sulla wiki/graph layer (post-MCP-Neo4j), non a web tools. Slice 5 = SOLO web_search + web_fetch.
- Il pattern action-enum singolo-tool con `oneOf` schema. Per due tool indipendenti due tool separati sono più chiari.

**Commit message template.**

```
slice 5: web tools (web_search + web_fetch via SearXNG)

Two LLM-facing deferred tools backed by self-hosted SearXNG container
(extends compose.yaml). SSRF defense enumerated (IPv4 private/
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
2. **~~`go-readability` mantenuto?~~ → CHIUSA (amendment #3, 2026-05-29):** go-shiori upstream deprecated 2025-12-05. Migrazione a `codeberg.org/readeck/go-readability/v2` (readeck Go fork, actively maintained, same API surface for `Article` parsing). No-op refactor a livello chiamante; import path swap solo.

---

## Slice 6 — Scheduler (cron + agent jobs persistente)

> **Slice 0.9 amendment**: `Handler` interface per task type (reminder, agent_job, ecc.) = `Agent` interface (Slice 0.9). Dispatch uniforme: `task.Handler.Run(ctx) iter.Seq2[*Event, error]` invece di switch-per-kind con shape diverse. `Notifier` emette `Event` invece di struct custom. Saving stimato: **−200 LOC** (no dispatch switch ridondante, no shape custom per ogni handler kind).

> **Amendment #46 (Phase 10 reconciliation, 2026-06-04 — D-06..D-29 in `.planning/phases/10-scheduler/10-CONTEXT.md`).** La Slice 6 originale è STALE su sei assi e non può esprimere/consegnare le 4 query North-Star italiane (riassunto mail mattutino, notizie di Cuneo serali, "ricordami di chiamare Monica alle 17:30", riassunto borsa lunedì). Questo amendment riconcilia il PRD con le 29 decisioni CONTEXT ratificate prima di qualsiasi codice (precedente Wave-0 doc-only 05-01/08-01/09-01, per D-29):
> 1. **Grammar** `daily HH:MM`/`in=10m` → triade industriale `at | every | cron` con colonna `tz` IANA per-task (D-06/D-07).
> 2. **Parser** "nessuna libreria cron esterna" → cron lib parser-only `adhocore/gronx`, tick DIY 30s ritenuto (D-08).
> 3. **agent_job spawn** — il vecchio seam (coordinator-spawn + rejecting-responder + tier-config, machinery tagliata in Phase 9) → costruzione `LlmAgent` diretta che mirrora `swarm.runChild` (D-24/D-25).
> 4. **Tool surface** tabella 5-file `task_*` → UN solo tool `task` non-deferred con enum `action` via ActionRouter (D-09/D-10/D-11).
> 5. **Delivery** (OQ2) → composite via WhatsApp/mail MCP self-send già montato, stdout fallback (D-19..D-23).
> 6. **Migration** numero al landing = `0009` (floor 0008); env catalog esteso; `tier`/`toolsets` payload tagliati v1 (D-12/D-13/D-29).
> I riferimenti superati sono marcati inline `[superato amendment #46]`; i blocchi che seguono sono la verità corrente.

> **Atomicity note (audit round 1 P0):** ~1300 LOC distribuiti = troppo per 1 commit.
> Si committa in **2 sub-slice ordinati** (file-target per-tool superati da amendment #46 → un solo tool `task`):
> - **6a**: types + migration `0009_scheduler` (floor 0008 — vedi §Persistence numbering) + sqlc queries + store thin adapter + scheduler tick loop + Notifier interface + handler `reminder` + il tool `task` (azioni `list`/`cancel` operative). ~700 LOC. Funzionante end-to-end per reminder, base infra.
> - **6b**: handler `agent_job` (costruzione `LlmAgent` diretta mirroring `swarm.runChild`, NO import `internal/swarm` — D-24) + le azioni `schedule`/`run_now`/`approve` del tool `task` + `ActionRouter` helper (primo uso multi-action, vedi §Pattern condiviso). ~600 LOC.
> Ogni sub-slice atomic-commit, smoke green prima del successivo.

**Goal (amendment #46).** Portare UN solo tool `task` LLM-facing non-deferred con enum
`action ∈ {schedule, list, cancel, run_now, approve}` (D-09/D-11, supersede la tabella
5-file `task_*`), supportato da un cron-core in-process (tick loop DIY ogni 30s) il cui
**next-fire-time è calcolato dalla lib parser-only `github.com/adhocore/gronx`**
(`NextTickAfter`/`IsDue`/`IsValid`, zero dipendenze transitive — D-08; la vecchia dicitura
"nessuna libreria cron esterna" è superata: la lib calcola solo il prossimo tick, il loop
resta DIY) e un Repository persistente Postgres. La **schedule grammar è la triade
industriale `at | every | cron`** (D-06):
- `at` — one-shot, risolto e memorizzato come `timestamptz` UTC (copre "chiamare Monica alle 17:30");
- `every` — intervallo, floor `MinScheduleEveryMinutes` (default 5, configurabile);
- `cron` — espressione 5-field standard + **colonna `tz` IANA per-task** (default da config, `Europe/Rome` via `AURA_SCHEDULER_TZ`).
**Regola DST-safe (D-07):** un task ricorrente memorizza `(expr, tz)` e ricalcola
`next_run_at` IN-ZONA dopo ogni fire — MAI un offset UTC fisso (drift silenzioso ±1h
attraverso il cambio DST). Il DB resta UTC; solo il calcolo è tz-aware.
Supporta `TaskKind` estensibile: `reminder`, `agent_job` (+ `backup_postgres`/`backup_neo4j`,
vedi §Backup strategy).

**Pre-rewrite reference**:
- [internal/agent/tools/registry/scheduler.go](git:pre-rewrite-2026-05-27/internal/agent/tools/registry/scheduler.go) — 587 LOC tool wrapper (LARGE, da splittare per azione)
- [internal/cron/scheduler.go](git:pre-rewrite-2026-05-27/internal/cron/scheduler.go) — 255 LOC tick loop (OK)
- [internal/cron/store.go](git:pre-rewrite-2026-05-27/internal/cron/store.go) — **594 LOC GOD CLASS** (Upsert+Cancel+Delete+DueTasks+MarkFired+RecordManualRun+RecordAgentJobResult tutti in un file)
- [internal/cron/dispatch.go + dispatch_handlers.go](git:pre-rewrite-2026-05-27/internal/cron/dispatch.go) — 244+246 LOC con 5 `dispatchXxx` privati (`dispatchReminder`, `dispatchWikiMaintenance`, `dispatchLessonPromotion`, `dispatchProposalTTLSweep`, `dispatchMemoryDecay`) + 2 arm inline nel `Dispatch` switch (`BackupVerify`, `WALCheckpoint`). Tutto senza strategy pattern (verificato pre-rewrite tag, round 1 reality check)

**Smoke E2E (grammar triade, amendment #46).**
```bash
./aura chat "ricordami fra 10 minuti di controllare il forno"
# → modello chiama task(action=schedule, kind=reminder, every="10m", payload={text: ...})
# → 10 min dopo: il dispatcher chiama Notifier → composite delivery (WhatsApp/mail self-send, stdout fallback)

./aura chat "ricordami di chiamare Monica alle 17:30"
# → task(action=schedule, kind=reminder, at="2026-06-04T17:30:00+02:00"|risolto UTC) — one-shot North-Star Q3

./aura chat "ogni mattina alle 9:30 fammi il riassunto delle mail da evadere"
# → task(action=schedule, kind=agent_job, cron="30 9 * * *", tz="Europe/Rome", payload={goal: ...})
# → il dispatcher costruisce un LlmAgent diretto (registry full minus swarm_spawn) con goal serializzato

# CLI operator parity (D-14, nessun LLM): ogni path della triade è testabile a mano
./aura task schedule --kind reminder --at "2026-06-04T17:30" --args '{"text":"chiama Monica"}'
./aura task schedule --kind agent_job --cron "30 9 * * *" --tz Europe/Rome --args '{"goal":"..."}'
./aura task schedule --kind reminder --every 5m --args '{"text":"..."}'
```

**Acceptance.**
- [ ] `MinScheduleEveryMinutes` configurabile (default 5, era hardcoded pre-rewrite — fix).
- [ ] Validation: `daily HH:MM` accetta solo `00:00`–`23:59`; nomi task `^[a-zA-Z0-9_-]+$` (no path traversal).
- [ ] Persistence: i task sopravvivono al restart del processo. Tick loop riprende `DueTasks` allo startup e cattura up i missed run (con flag `MissedSince`).
- [ ] **`DueTasks` query usa `SELECT ... FOR UPDATE SKIP LOCKED` (audit round 1 P0)**: pattern atomico per multi-instance safety. Anche con 1 sola Aura attiva oggi, blocca double-dispatch se un manual `task.run_now` parte contemporaneo al tick. Costo zero, sblocca future multi-instance.
- [ ] Dispatcher: ogni `TaskKind` ha un `Handler` separato in `internal/cron/handlers/<kind>.go`. **Handler = `agent.Agent` (Slice 0.9)**: implementa `Run(ctx InvocationContext) iter.Seq2[*Event, error]` + metadata `MaxDuration() time.Duration` + `ReschedulesOnRecovery() bool` via embedding `BaseAgent` o struct fields. `MaxDuration` definisce la soglia oltre la quale un run senza `finished_at` è considerato in limbo al restart. `ReschedulesOnRecovery=true` (reminder, idempotenti) → il task viene riportato in `DueTasks` al boot; `=false` (agent_job, side-effecting) → limbo audit-only, no auto re-run. Aggiungere un nuovo kind = aggiungere 1 file con `Agent` impl, no dispatch switch nel tick loop (`tickLoop` itera `handler.Run(ctx)` yield events e li forwarda a `Notifier`/audit).
- [ ] **`agent_job` handler auto-rejecta child Loop pause (audit round 1 P0; spawn seam amendment #46 D-24/D-25)**: il sub-loop è un `LlmAgent` costruito **direttamente** dall'handler mirroring `swarm.runChild` (`internal/swarm/swarm.go:132-192` è il template provato) — registry `Without(reg, "swarm_spawn")`, goal come primo user message, budget da `agent_job_runs.step_budget`, sessione ephemeral `agent_job:<run_id>` (NO `internal/swarm` import; il vecchio seam coordinator-spawn + rejecting-responder è machinery tagliata in Phase 9, `[superato amendment #46]`). L'auto-reject (D-25) è **inject-and-continue**: su `Actions.AwaitingInput`, re-Run un fresh LlmAgent con i turn precedenti + una risposta sintetizzata `RoleTool` `"<auto-rejected: scheduled job has no human responder>"` (il resume seam del Runner senza DB, pattern `chat_repl.go` SubmitAnswer). `ask_user` RESTA nel registry del child (il modello deve VEDERE la rejection in-band e decidere come procedere, bounded dal budget residuo). Test asserisce: cron job che invoca ask_user non blocca, completa in <30s con il marker auto-rejected nell'audit.
- [ ] `LastError` persistito su failure, leggibile via `task.list()`.
- [ ] **Risk-Based Governance (Area #5 closed 2026-05-28)**: ogni `task(action=schedule)` calcola `tier = scoring.ComputeTaskTier(args)` via modulo condiviso `internal/scoring/` (vedi §Risk-Based Governance). Se `tier in {RISKY, DESTRUCTIVE}` il task viene creato con `status='pending_approval'` (non gira), il tool result include `{task_id, risk_tier, gate_recommended:true, status:'pending_approval'}`. L'agente decide autonomamente se ri-emettere `ask_user(kind=approval, ResumeContext={action:'approve', task_id})` o procedere silente. Se silente: Notifier IMMEDIATE alert all'utente (tier ≥ RISKY trigger threshold). Tutti i campi audit (`computed_risk_tier`, `gate_recommended`, `gate_taken`) registrati in `agent_job_runs`.
- [ ] **Azione `task(action=approve, task_id)`** (amendment #46: era tool `task.approve` deferred, ora azione dello stesso tool `task`): chiama `Queries.ApproveTask(id)` → `UPDATE scheduler_tasks SET status='active' WHERE status='pending_approval'`. Idempotente, no-op su task già attivi. Usato dal resume handler dell'ask_user approval e dalla CLI `aura task approve <id>` (D-27).
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
- [ ] **Scheduler budget contract (amendment #19 cross-ref Slice 0.9)**: scheduler-spawned `agent_job` runs read budget from `aura.agent_job_runs.step_budget` (NEW column, ALTER TABLE in same migration) — defaults to `AURA_LOOP_MAX_STEPS` if NULL. The scheduler sets `InvocationContext.RemainingSteps = step_budget` at spawn. Per-job override via `aura task schedule --max-steps N`. Acceptance test: schedule task with `--max-steps 10`, agent loops → terminates at step 10 (not fresh 25).
- [ ] **Ephemeral session per `agent_job` runs (amendment #23, picobot-inspired)**: a differenza di Slice 1.8 conversations (Telegram/CLI user-facing, persistono history multi-turn), gli `agent_job` runs **NON accumulano history cross-run**. Ogni run usa una `ephemeral_session = &session.Session{Key: "agent_job:<run_id>", History: []Message{}}` IN-MEMORY only, scartata a `Run() returns`. Cross-run state persiste SOLO in `aura.agent_job_runs` audit row (`summary text`, `outcome jsonb`, `tokens jsonb` field, `exit_status` enum). Pattern reference: [picobot internal/agent/loop.go system channels ephemeral](https://github.com/louisho5/picobot/blob/main/internal/agent/loop.go) — heartbeat/cron sessions non chiamano `sessions.Save(sess)`. Rationale: cron `daily_summary` runs daily × 365 giorni = 365 turn history se persistente, ognuno con large tool results, è storage waste + context-poisoning across-runs. `summary` field in audit row sufficient per forensics + replay debug. Acceptance test: spawn `agent_job` "echo task", verify `aura.agent_job_runs` row created with `summary` field populated AND `aura.conversation_turns` NOT touched (no row con `conversation_id` matching agent_job context).

**File targets** (≤ 750 LOC src — risparmio ~550 LOC vs pre-rewrite grazie a sqlc + ulteriori −200 LOC riapplicati da Slice 0.9: `Handler = Agent`, no dispatch switch ridondante, Notifier emette `*agent.Event` invece di struct custom):
| Path | LOC | Note |
|---|---|---|
| `internal/cron/types.go` | ~100 | `Task`, `TaskKind`, `ScheduleKind`, `Status` enums (Go domain types, distinti dai sqlc-generated). |
| `internal/cron/scheduler.go` | ~180 | Tick loop, lifecycle (Start/Stop), missed-run recovery, crash-recovery boot query (chiama `MarkRunsRecovered` prima del primo tick, itera RETURNING per reschedule selettivo, notifica utente). Usa `*cron.Queries` (sqlc client). Per ogni due task, recupera `Handler` (= `agent.Agent`) dal registry e itera `handler.Run(invocationCtx)` yield events → forward a Notifier + audit log. |
| `internal/cron/store.go` | ~80 | Thin adapter: domain `Task` ↔ sqlc rows. Trasforma enum string ↔ tipo Go. |
| `internal/db/queries/scheduler_tasks.sql` | ~120 | **8 query sqlc**: `UpsertTask`, `GetByName`, `ListTasks`, `DueTasks`, `MarkFired`, `CancelTask`, `DeleteTask`, `RecordRunResult`. Una query per concept, anti-god-class. |
| `internal/db/queries/agent_job_runs.sql` | ~80 | **4 query sqlc**: `RecordManualRun`, `RecordAgentJobResult`, `ListRuns`, `MarkRunsRecovered`. `MarkRunsRecovered` è la boot recovery query (UPDATE finished_at IS NULL AND started_at < threshold → exit_status='unknown_recovery'). RETURNING task_id per il reschedule loop. |
| `internal/db/migrations/0009_scheduler.up.sql` (numero al landing = **0009**, floor 0008=proxied_child_id_text; vedi §Persistence numbering — D-29) | ~90 | `CREATE TABLE aura.scheduler_tasks` (id, name unique, kind, schedule_kind, schedule_payload jsonb, next_run_at, `status text NOT NULL CHECK (status IN ('active','paused','cancelled','pending_approval'))`, last_error, created_at, updated_at). `CREATE TABLE aura.agent_job_runs` (id, task_id fk, started_at, finished_at, `exit_status text NOT NULL DEFAULT 'running' CHECK (exit_status IN ('running','completed','failed','cancelled','timeout','unknown_recovery'))`, `recovered_at timestamptz NULL`, `computed_risk_tier text NOT NULL DEFAULT 'normal' CHECK (computed_risk_tier IN ('safe','normal','risky','destructive'))`, `gate_recommended boolean NOT NULL DEFAULT false`, `gate_taken boolean NOT NULL DEFAULT false`, `approval_source text NULL CHECK (approval_source IS NULL OR approval_source IN ('ask_user','cli','auto'))`, `paused_state_token uuid NULL REFERENCES aura.paused_states(token) ON DELETE SET NULL`, summary text, tokens jsonb). Indici su `next_run_at WHERE status='active'` (scheduler_tasks) e su `(exit_status, started_at) WHERE finished_at IS NULL` (boot recovery scan). **`approval_source` enum + `paused_state_token` FK** (parity con `skill_audit`, fix audit Round 1 P1): se la run è triggered da `task.approve` post-ask_user → `approval_source='ask_user'` + `paused_state_token=<token>`. Se da `task.run_now` CLI → `'cli'` + NULL. Se da tick scheduler senza gate → `'auto'` + NULL. Forensics simmetrico cross-slice. |
| `internal/db/migrations/0009_scheduler.down.sql` | ~5 | DROP TABLEs. |
| `internal/cron/handlers/handler.go` | ~50 | `Handler` = type alias di `agent.Agent` (Slice 0.9) + metadata interface `HandlerMeta{ Kind() TaskKind; MaxDuration() time.Duration; ReschedulesOnRecovery() bool }`. Registry `map[TaskKind]Handler`. Helper `BaseHandler` struct embeddable per riusare metadata. |
| `internal/cron/handlers/reminder.go` | ~70 | `ReminderAgent` impl `agent.Agent`: `Run(ctx) iter.Seq2[*Event, error]` emette un `Event{Author:"reminder", LLMResponse:{Content: payload.Text}}` poi `Event{Actions.Escalate=true}`. Notifier consuma yield events. `MaxDuration=30s`, `ReschedulesOnRecovery=true` (idempotente: ri-notificare "controlla il forno" è safe). |
| `internal/cron/handlers/agent_job.go` | ~120 | **`AgentJobAgent` impl `agent.Agent` (amendment #46 D-24/D-25):** costruisce un child `LlmAgent` **direttamente** mirroring `swarm.runChild` (`internal/swarm/swarm.go:132-192`) — `agent.NewLlmAgent` con registry `Without(reg, "swarm_spawn")`, goal come primo user message, `agent.NewBudget(BudgetOptions{MaxSteps: &step_budget})` dalla riga `agent_job_runs.step_budget` (amendment #19), sessione ephemeral `agent_job:<run_id>` (amendment #23, mai via `runner.Turn` persistente). Forwarda i child events come yield del proprio `Run`. **NO import `internal/swarm` in `internal/cron`.** Auto-reject ask_user = inject-and-continue (D-25). `MaxDuration=600s` (default, override via payload), `ReschedulesOnRecovery=false` (side-effect committati non ricostruibili). `[superato amendment #46: il vecchio coordinator-spawn + il `tier ∈ {worker,chat,reasoning}` payload validato vs il tier-config catalog = machinery tagliata Phase 9, D-12; il child usa il modello unico configurato, il modifier reasoning di scoring diventa default piatto]`. |
| ~~`internal/cron/handlers/graph_maintenance.go`~~ | — | **RIMOSSO (audit round 1 P0)**: scope-creep esplicito (placeholder per future), va in slice dedicata post-MCP-Neo4j. Slice 6 supporta solo `reminder` e `agent_job` per ora. |

**Commit message templates (sub-slice 6a + 6b):**

```
slice 6a: scheduler infrastructure — types + sqlc + tick loop + reminder handler

PostgreSQL-backed scheduler base. Types (Task/TaskKind/ScheduleKind/Status),
migration 0009_scheduler (floor 0008, vedi §Persistence numbering) (tables scheduler_tasks + agent_job_runs with
exit_status check constraint (running|completed|failed|cancelled|timeout|
unknown_recovery), recovered_at nullable column, per-task tz column +
adhocore/gronx-computed next_run_at (DST-safe in-zone recompute), indices on
next_run_at WHERE status='active' and on (exit_status, started_at) WHERE
finished_at IS NULL, FOR UPDATE SKIP LOCKED on DueTasks). 12 sqlc queries
split across scheduler_tasks.sql + agent_job_runs.sql (incl. MarkRunsRecovered
boot query). Tick loop DIY 30s with missed-run recovery and crash recovery
(boot query marks finished_at IS NULL && started_at < MaxDuration as
unknown_recovery; reschedules only handlers with ReschedulesOnRecovery=true,
i.e. reminders; agent_job stays audit-only). Handler = agent.Agent (Slice
0.9): Run(ctx) iter.Seq2[*Event, error] + metadata MaxDuration() +
ReschedulesOnRecovery(). Notifier interface + stdout impl consume yield
events and emit boot summary if >=1 row recovered. Handlers registry +
reminder handler (MaxDuration=30s, reschedules=true). One `task` tool
(action enum), list/cancel actions operative.

Smoke: aura schedule reminder every=10m → tick loop fires → Notifier stdout.
Persistent across restart verified.

LOC: +XXX src / +YY test / +ZZ migration.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

```
slice 6b: scheduler agent_job + ActionRouter helper

Adds agent_job handler that constructs an LlmAgent directly, mirroring
swarm.runChild (registry Without swarm_spawn, budget from agent_job_runs row,
ephemeral session agent_job:<run_id>, NO internal/swarm import — D-24).
ask_user auto-reject = inject-and-continue (D-25): cron jobs never block on a
human responder. The `task` tool gains its schedule/run_now/approve actions.
ActionRouter helper introduced in internal/agent/tools/action.go (~90 LOC)
with sentinel passthrough contract, top-level required=["action"] only (no
root oneOf/anyOf/enum — OpenAI-wire-safe, D-10) — first multi-action consumer
is `task`. Slice 7 reuses.

Smoke: aura chat "ogni mattina alle 9:30 fammi il riassunto delle mail" →
task(action=schedule, cron="30 9 * * *", tz=Europe/Rome) → agent_job handler →
direct LlmAgent child → join.

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```
| `internal/agent/tools/task.go` | ~220 | **UN solo tool `task` non-deferred (amendment #46 D-09/D-10/D-11, supersede la tabella 5-file `task_*`):** enum `action ∈ {schedule, list, cancel, run_now, approve}` dispatchato via l'`ActionRouter` helper. **Schema discipline load-bearing (D-10, nanobot regression #3113):** top-level `required = ["action"]` SOLO; i requisiti per-azione vivono nelle field descriptions; NESSUN `oneOf`/`anyOf`/`enum` root-level (rompe i provider OpenAI-wire — DeepSeek è OpenAI-compat). Un test asserisce la shape dello schema. Comportamento per azione: `schedule` valida + `Queries.UpsertTask`, chiama `scoring.ComputeTaskTier(args)` (RISKY/DESTRUCTIVE → `status='pending_approval'` + Notifier IMMEDIATE alert, result porta `risk_tier`+`gate_recommended`); `list` mostra anche `pending_approval` con `[awaiting approval]`; `cancel`; `run_now` chiama `Handler.Run` diretto bypass tick (**rifiuta** task in pending_approval con errore strutturato che chiede `approve` prima); `approve` `UPDATE scheduler_tasks SET status='active' WHERE id=:id AND status='pending_approval' RETURNING id`, idempotente, audit `gate_taken=true`. `[superato amendment #46: il payload `tier ∈ {worker,chat,reasoning}` è tagliato v1 — D-12; il `toolsets:[wiki,web]` payload è tagliato — i child ricevono il registry full minus swarm_spawn, D-13]`. |
| `internal/agent/tools/action.go` | ~90 | **`ActionRouter` helper (amendment #46 D-09, primo consumer multi-action; Slice 7 lo riusa):** dispatcha sull'azione top-level con sentinel-passthrough contract; mantiene `required=["action"]` come unico requisito root-level. |
| `internal/scoring/scoring.go` | ~100 | Modulo condiviso. `RiskTier` enum (safe/normal/risky/destructive), `ComputeTaskTier(args)`, `ComputeSkillTier(action, body)`, `GateRecommended(tier)`, `RequiresImmediateAlert(tier)`. Mapping kind→tier hard-coded + modificatori (frequency, silent, payload regex destructive keywords). |
| `internal/scoring/scoring_test.go` | ~80 | Test esaustivo del mapping per ogni kind + tutti i modificatori. Niente DB. |
| `internal/cron/scheduler_test.go` | ~150 | Build tag `db_integration`. Round-trip + tick loop + missed-run recovery + crash recovery + risk_tier=RISKY → pending_approval. |
| `internal/cron/handlers/agent_job_test.go` | ~100 | Goal serialization + direct-LlmAgent child spawn (mirroring swarm.runChild; ask_user auto-reject inject-and-continue, D-25). |

**Open questions.**
1. **~~CONFLITTO con CLAUDE.md "Persistence: Neo4j via MCP"~~ → CHIUSA: Postgres come terzo pilastro.**
   Slice 0.5 ha introdotto Postgres come application-state store. Architettura persistence finale:
   - **Neo4j (via MCP)** = knowledge graph + vector index. **Unica fonte di knowledge E di vector embeddings.** Semantic memory, entity graph, conversational memory, derivati, embedding HNSW nativo (Apache Lucene). Embedder: **default `embeddinggemma-300m` via sidecar `aura-llama-embed`** (porto 8081, OpenAI-compat; nome modello NON hardcoded — qualsiasi 768-native-GGUF è drop-in, D11/#31), **768d** scritti direttamente su `:Chunk.embedding` (vector index Neo4j a 768d = il contratto; l'embedder emette 768 nativo o MRL-truncato-a-768 — D11/#31). NIENTE wiki markdown filesystem (deprecato 2026-05-27). NIENTE Qdrant: lo spike `D:/tmp/aura-neo4j-spike-2026-05-27/` Phase 6b ha misurato 22-30ms p95 + IT recall@5 5/5 su Neo4j vector index nativo con corpus reale Aura → Qdrant decommissionabile senza regressione. Container produzione `aura-neo4j` atterra in Slice 0.7 (rinominato da `neo4j-spike` dello spike).
   - **Postgres (via sqlc)** = application state. Scheduler tasks, paused states, audit log, identity, capability grants.
   - **Filesystem** = solo artefatti operativi (SKILL.md tree, tool result sidecar files). NESSUN knowledge in filesystem.
   Distinzione semantica chiara: **knowledge + vectors → solo Neo4j; infrastructure → Postgres; operational artifacts → filesystem**. CLAUDE.md aggiornata in coda al PRD.
2. **Notifier interface / delivery (RISOLTA composite — amendment #46 D-19..D-23).** Slice 6 ha bisogno di un sink per output reminder + agent_job. → *Decisione*: **composite delivery**. Interface `Notifier{ Notify(ctx, recipient, msg) error }` con impl multiple: l'output del job raggiunge l'utente via **WhatsApp/mail MCP self-send già montato** (`cmd/aura/main.go:150-158` allowlist include già `send_message`/`send_email`); `agent_job_runs.summary` tiene la forensics; **stdout Notifier è il fallback** + alert SC#3 24h missed-backup. Route per-task opzionale `notify: whatsapp|email|stdout` (D-20, il modello la setta dal phrasing utente), con `AURA_SCHEDULER_NOTIFY_DEFAULT` + `AURA_SCHEDULER_NOTIFY_RECIPIENT` come fallback globale. Notify anche su failure (D-21). Delivery failure → fallback chain + bounded retry `AURA_SCHEDULER_NOTIFY_RETRY_ATTEMPTS` (D-22, il risultato non si perde, audit summary sempre scritto). Quiet hours `AURA_SCHEDULER_QUIET_HOURS` differisce le notifiche non-DESTRUCTIVE (D-23). **NON in conflitto con amendment #23** (ephemeral session): #23 vieta la *history persistence*, qui è *output egress*. Telegram Notifier impl slotta in Phase 13. stdout-only respinto (nessuno fa il tail di un daemon).
3. **Agent-job sub-loop spawn seam (RISOLTA direct-LlmAgent — amendment #46 D-24/D-25).** → *Decisione*: l'`agent_job` handler costruisce un `LlmAgent` **direttamente**, mirroring `swarm.runChild` (`internal/swarm/swarm.go:132-192`): `agent.NewLlmAgent` con registry `Without(reg, "swarm_spawn")`, goal come primo user message, budget da `agent_job_runs.step_budget` (amendment #19), sessione ephemeral `agent_job:<run_id>` (amendment #23). **NO import `internal/swarm` in `internal/cron`.** `[superato: il vecchio coordinator-spawn + il `tier ∈ {worker,chat,reasoning}` payload validato vs il tier-config catalog di swarm = machinery tagliata in Phase 9 (D-12, grep-confermato dead); v1 usa il modello unico configurato, il modifier reasoning di scoring diventa default piatto. Re-add del `tier` payload → v2 SWARM-V2-01 quando il tier-config esisterà]`. Auto-reject ask_user = inject-and-continue (D-25). Evita di duplicare la logica spawn-loop.
4. **`agent_job_runs` retention (Area #13 closed 2026-05-28)** → *Decisione*: audit log forever, niente auto-purge per età. Pattern coerente con Area #7 paused_states e Area #9 RUN_DIR (delete-on-explicit-action). Per uso tipico (cron daily=365 rows/anno, weekly=52, every_hour=8760) la tabella resta MB-size per anni. Indici efficienti (`(exit_status, started_at) WHERE finished_at IS NULL` boot recovery, `(task_id, started_at DESC)` per `task list --runs`) reggono O(log n) anche a milioni di rows. CLI escape hatch `aura task runs purge --before <ISO date> --confirm` per casi estremi (es. `every_minute` cron lasciato attivo). Future slice "cold archive" se tabella supera 10 GB.

---

## Slice 7 — Skills (self-extension via ask_user governance)

> **Amendment #48 (D-32/D-33, Phase 11 Wave-0 doc-only) — Slice 7 truth-source realignment.** The original Slice 7 design was authored pre-rewrite and went STALE in 4 scout-confirmed classes (dead sandbox seam, wrong migration numbers, dotted tool names, skills-in-system-prompt). This amendment supersedes those items across §Slice 7 + §Slice 7e-core + §Caps & Limits. The locked decisions are D-01..D-38 (see `.planning/phases/11-skills/11-CONTEXT.md` + spikes 003-010). The actual model-facing surface is **ONE `skill` tool with an `action` enum** (D-01), NOT 7-11 dotted `skill.*` tools; execution is the shipped `tools.SandboxExec` → `internal/sandboxagent.Client` HTTP `:2468` by-path seam (D-15b/amendment #44), NOT `sandbox.Runner.Execute`; the manifest lives in the `skill` tool Description and `always:true` bodies render at `messages[1]` (D-06/D-07), NOT in the system prompt; migrations land at `0010+` (floor `0009`), NOT `0007`/`0012`.

> **Atomicity note (audit round 1 P0, amended #48):** ~2100 LOC totali = troppo per 1 commit.
> Si committa in **6 sub-slice ordinati** (Wave-0 amendment FIRST, then 7a-7e), ognuno atomic + smoke green (D-32):
> - **11-01 (Wave-0, this amendment)**: doc-only PRD-amendment package (D-33) — supersede the 4 staleness classes; no code.
> - **7a**: types + loader (filesystem + parser + cache 4-way split, parse-only validation per D-28) + `internal/skills/validator.go` (NFKC + literal blocklist single chokepoint, write-boundary only) + `internal/skills/paths.go` (`SanitizeName` single source) + the ONE `skill` tool with read actions `list`/`info` + the manifest-in-Description render (D-06). Read-only, no governance. ~500 LOC.
> - **7b**: catalog client — skills.sh **`/api/search` JSON lax-decode** (D-11, spike 003), NOT HTML scrape; the `skill` tool gains `action=catalog`. **Browse default-ON (D-12, amendment #14 FLIPPED)** — read-only JSON has no scrape attack surface; `aura skills disable-catalog` is the default-deny escape hatch. ~350 LOC.
> - **7c**: writer (atomic pending→active) + deleter + the `skill` tool mutation actions `create`/`update`/`delete` (ask_user governance). Schema `aura.skill_audit` (migration **`0010`**, floor 0009 — D-32) + sqlc queries + adapter + the **D-29 5-row audit-coherence matrix** CHECK. Tx wrapping esplicito attorno alla coppia (FS-move pending→active + INSERT skill_audit row): se INSERT fallisce, FS-move viene rollback-ato (rename inverso). ~600 LOC.
> - **7d**: installer — native Go **`git clone --depth 1 --single-branch -c core.autocrlf=false`** (D-15, spike 004b) + symlink-strip + Aura canonical hash, NOT `npx`; the `skill` tool gains `action=install`. ~450 LOC.
> - **7e-core (v1, amendment #13)**: **executable code snippets** (multi-lang Python/shell, eseguibili by-path via the shipped `sandbox_exec` → `sandboxagent.Client` :2468 seam, ro `/skills` mount per D-17) + **TTL 90gg archived state** (sweep via the **`skill_ttl_sweep` cron TaskKind**, D-16 — NOT a goroutine; archived skip da discovery default). Estende SKILL.md con `type: instruction|snippet` field. ~450 LOC. **Dipende dal sandbox-agent portable hardening floor** (token + seccomp + no-new-privileges + read-only rootfs + egress allowlist — D-38) per esecuzione sicura di codice model-authored. **NO cross-conv pattern analyzer in v1.**
> - **7f (v1.x, amendment #13 — deferred SKILL-V2-01)**: **pattern analysis multi-conversation auto-suggest** (cross-conv analyzer suggerisce save dopo 3+ pattern simili via Neo4j HNSW similarity clustering). ~250 LOC. **Dipende da Slice 0.7 Neo4j HNSW + Slice 7e-core + Slice 11 memory** (più downstream di v1 7-slice). Pattern reference: Voyager (Wang et al., NeurIPS 2023) skill library + mem0 procedural memory. Tracked in STATE.md Deferred Items `SKILL-V2-01`.
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
> Razionale: una skill scritta dall'agente, una volta attivata, rende il suo
> manifest entry visibile nella Description del tool `skill` e (se `always:true`)
> inietta il body al `messages[1]` del turn successivo (D-06/D-07) — è
> auto-modifica persistente. **NO `approve` action model-facing (D-03)**: il
> modello non può approvare le proprie mutation; l'attivazione passa SOLO per
> ask_user resume (umano) o `aura skills approve` CLI. Future skill read-only
> (es. `SAFE`) potranno essere auto-promosse senza gate via la stessa pipeline;
> oggi non esistono.

**Goal (amended #48, D-01).** **UNA** tool LLM-facing — `skill` — con un `action` enum, NON 7-11 tool dotted `skill.*` (i nomi dotted violano il constraint OpenAI-wire `^[a-zA-Z0-9_-]+$`; DeepSeek rifiuta sia i nomi dotted sia un `oneOf`/`enum` root). Copia il pattern `ActionRouter` di `internal/agent/tools/task.go` (Slice 6b, D-10 schema discipline: `required=["action"]` come unico requisito root, per-action fields nelle description, niente root `oneOf`). Il tool è **NON-deferred** (D-05, parity Claude Code — sempre visibile). Azioni:
- Read-only: `action=list` (BM25-ranked, D-09), `action=info` (body via ToolResult preview+sidecar), `action=catalog` (skills.sh `/api/search`, **default-ON** per D-12).
- Mutations (tier RISKY/DESTRUCTIVE da `internal/scoring/ComputeSkillTier`, atterrano in `pending/`, agente decide se gate via `ask_user`, Notifier alert se gate skipped): `action=create`, `action=update`, `action=delete`, `action=install`.
- Snippet lifecycle (7e): `action=use` (ritorna istruzioni + path in-sandbox, D-04 — il modello esegue via `sandbox_exec`, MAI un bespoke `run`), `action=restore`, `action=archive`.
- **NESSUNA `action=approve` model-facing (D-03)**: l'attivazione è solo umana (ask_user resume) o operatore (`aura skills approve` CLI).

Alimentati da:
- Loader filesystem multi-root (TTL cache 1s, pattern pre-rewrite ben tarato; parse-only validation per D-28 — il blocklist è enforced SOLO alle write boundary, mai nel load path).
- Catalog search via skills.sh — **client JSON `GET /api/search?q=` lax-decode** (D-11, spike 003; MAI `DisallowUnknownFields` — lo schema drifta `isDuplicate`/`count`), rank by `installs`. HTML scrape DEAD. Transport isolato per lo swap futuro alla public API (vercel-labs/skills#426).
- Install via **native Go `git clone --depth 1 --single-branch -c core.autocrlf=false`** (D-15, spike 004b — native WINS vs npx: bit-identical, 3× più veloce, droppa la dipendenza node/npx host) + symlink-strip a materialization (riusa il pattern `ScanOrphans` Lstat-no-follow di Slice 1.8) + **Aura canonical hash** (byte-sorted `(relPath,bytes)` sha256 — NON interop con l'upstream `skills-lock.json` `computedHash`, locale/platform-sensitive). Post-install `ParseSkill()` re-validation prima di `Invalidate()`. `--ignore-scripts` era un no-op (il CLI non gira mai postinstall, D-14) — moot. **Amendment #49 (install-leg routing, live-E2E-driven):** il `repo` accetta lo shorthand `owner/repo` del campo `source` di skills.sh (normalizzato a `https://github.com/owner/repo`), e `InstallOpts.SkillName` seleziona la skill dentro un repo multi-skill (layout `anthropics/skills/<name>/SKILL.md`) — senza selettore il primo-SKILL.md-trovato installava una skill arbitraria, rompendo la catalog→install loop del North Star D-35. Il catalog client applica un **format-boost merge**: query multi-parola contenente un token formato (xlsx/pdf/docx/…, alias excel→xlsx ecc.) esegue anche la query bare-token e fonde i risultati per installs desc — la search semantica di skills.sh seppelliva `anthropics/skills/xlsx` (102k installs) sotto skill di dominio.
- Writer in `~/.aura/skills/pending/<name>/` per le mutation in attesa di approval; al `Approva` move atomico in `~/.aura/skills/active/`.
- Audit log append-only in **Postgres** tabella `aura.skill_audit` di OGNI mutation con `{id, ts, actor_id (fk aura.identities), action, name, content_hash, source, approval_source (enum), paused_state_token fk, computed_risk_tier, gate_recommended, gate_taken, blocklist_override}`. Migrate **`0010_skill_audit.up.sql`** aggiunta in Slice 7c (floor `0009_scheduler` — D-32; NON `0007`, riservato da Slice 8/10 shipped). Query via sqlc `internal/db/queries/skill_audit.sql`. `approval_source` (Area #10 closed 2026-05-28) enum `{ask_user, cli, auto}` esplicito su COME la mutation è stata autorizzata, governato dalla matrice D-29 (vedi sotto). **Metadata consolidation (D-19)**: usage/archive state (status/last_used_at/use_count) NON è in colonne ALTER di `skill_audit` — vive in un sidecar JSON per-skill (atomic-write, la sola live-state source) + un'opzionale tabella `snippet_runs` (migration 0011) per le forensics per-run.

**Discovery + prompt injection design (amended #48, D-06/D-07).** Le skill NON entrano nel system prompt (`messages[0]` resta byte-stable, CAP-04 invariant). Tre livelli:
- **Manifest** (name + trigger-rich description per skill) vive nella **Description del tool `skill`**, generata a registry-build da `internal/skills/manifest.go` (precedente: `toolSearchDescription(reg)` in 08.1, D-06). Turn-stable: cambia solo su skill add/remove (busta la tools-prefix cache UNA volta, raro, accettato). Ordine alfabetico/byte-stable (cache-load-bearing). Oltre `AURA_SKILL_MANIFEST_CAP_BYTES` (~8k, Codex parity) il blocco termina con "N more — search with skill action=list {query}" (BM25 overflow, D-09 — riusa il ranker 08.1).
- **`always:true` bodies** rendono in UN solo blocco user-role a **`messages[1]`** (D-07, pattern convergente Claude Code CLAUDE.md / Codex AGENTS.md; il concat in system-prompt di nanobot è l'anti-pattern). Questo è IL seam che Phase 14 Agent.md condivide (profile first, always-skills after). **VINCOLO FLAGGATO**: l'evictor L2.5 di `conversations/context.go` deve proteggere il blocco `messages[1]` come protegge il system L0 — altrimenti una conversazione lunga sfratta silenziosamente la always-mode.
- **`messages[0]`** riceve la sezione frozen **"Skills — capability gaps"** del canonical system prompt (amendment #49, D-39 — supersede la "UNA sola frase" originale di D-06): check-installed-before-hand-building, artifact-family→catalog, install solo via ask_user approval, snippet execution BY-PATH. Resta una const byte-stable mechanism-not-enumeration (D-08/D-09 invarianti: zero timestamp, zero enumeration del tool set concreto).

> **Amendment #49 (D-39, Phase 11 gap-closure, live-E2E-driven, 2026-06-05).** Il live xlsx North-Star E2E (judge 0.40 vs gate ≥0.90, `docs/aura-skills-eval-2026-06-05.md`) ha provato che la policy D-06 "UNA sola frase" era insufficiente: il modello — istruito solo con "skills exist; the skill tool lists/applies them" + la doctrine `tool_search` — ha razionalmente brute-forzato web+sandbox (3× `tool_search` a vuoto, 15× `sandbox_exec` con Python ad-hoc) senza mai toccare la catalog→install path. Tre supersessioni discovery-routing, tutte cache-safe (const byte-stabili, nessun cambio schema, zero nuove dep), per il principio "se il modello non invoca il tool è bug del system prompt o del manifest, NON del test" (§Slice 1 + memoria `feedback_agent_must_know_tools_exist`):
> 1. `agent.SystemPrompt` è promosso al **canonical capability-gap system prompt** (`docs/system_prompt.txt`, adottato verbatim): tone, loop budget discipline, tool doctrine (tool_search 2-phrasing cap), sezione "Skills — capability gaps" (installed-check prima di build-by-hand; artifact families → `skill action=catalog`; install SEMPRE via ask_user, mai self-granted; snippet BY-PATH `python3 /skills/<name>/...`), sandbox/artifact verify-before-report, honesty, nota operator-pinned `messages[1]`. Nomina solo i verbi strutturali (`tool_search`/`text_response`/`skill`/`ask_user`) — mai il tool set concreto: D-08/D-09 reggono.
> 2. Il no-result di `tool_search` guadagna una coda fissa di orientamento verso `skill action=catalog` — il momento esatto del capability gap deve instradare DENTRO il sistema skills, non verso codice ad-hoc.
> 3. Il lead della Description del tool `skill` nomina il loop catalog/install+approval (prima documentava solo list/info/use, lasciando catalog/install sepolti nella schema enum description).

**SKILL.md format** (agentskills.io spec compat, D-30):
```
---
name: aura-implementation-loop      # ≤64, [a-z0-9-], must match dir name
description: Trigger-rich one-liner (≤1024) — incorporates when-to-use inline.
# Optional, tolerated-but-inert (agentskills.io interop): license, compatibility, metadata, allowed-tools
# Aura extensions: always: true (D-07/D-10) ; type: instruction|snippet (default instruction)
---

# Body markdown freeform
```
Parse con una **vera libreria YAML** (`goccy/go-yaml`, RESEARCH-verified — il
double-quoted scalar con escaped quotes del corpus reale richiede un parser vero;
+ CRLF normalization). NIENTE `when_to_use`/`tools` field (la description ricca
incorpora il when-to-use, confermato dalla spec). `always:true` è proposable dal
modello ma il gate lo flagga ("⚠ ALWAYS-ON: steers every future turn"); `install`
lo STRIPpa incondizionatamente dalle skill di terze parti (D-10).

**Pre-rewrite reference** (pattern-only, read at tag `pre-rewrite-2026-05-27`):
- [internal/agent/tools/registry/skill.go](git:pre-rewrite-2026-05-27/internal/agent/tools/registry/skill.go) — 347 LOC: l'**anti-pattern multi-tool che D-01 uccide** (UNA tool con `action` enum lo sostituisce).
- [internal/skills/loader.go](git:pre-rewrite-2026-05-27/internal/skills/loader.go) — 273 LOC (mixing 4 concerns: FS scan + YAML parse + cache + name validation — split target).
- [internal/skills/admin.go](git:pre-rewrite-2026-05-27/internal/skills/admin.go) — 326 LOC (installer + deleter + network — splittare; l'install npx è SUPERSEDED dal native clone D-15).
- [internal/skills/catalog.go](git:pre-rewrite-2026-05-27/internal/skills/catalog.go) — 133 LOC (HTML scrape — SUPERSEDED dal JSON `/api/search` D-11).

**Smoke E2E (prompt reali — vedi §Test discipline):**
```bash
./aura chat "fammi vedere quali competenze hai disponibili"
# → modello chiama skill(action=list) (read-only, no ask_user). Riassume.

./aura chat "ho bisogno di un'analisi statistica avanzata. cerca su skills.sh"
# === catalog browse default-ON (D-12) — nessuna pre-condizione opt-in ===
# → modello chiama skill(action=catalog, query=...) → /api/search JSON → 3 candidati
# → modello chiama ask_user(kind=choice, options=[3 skill candidati])
# → utente sceglie → skill(action=install, source=...) → ask_user(kind=approval, "Confermi install di X?")
# → approve → native git clone + symlink-strip + canonical hash, Loader.Invalidate(), next turn la usa

./aura chat "scrivi una skill che ti faccia rispondere sempre in haiku"
# → modello chiama skill(action=create, {name, description, body, always:true})
# → tool scrive ~/.aura/skills/pending/haiku-mode/SKILL.md (NON active); gate flagga always:true
# → tool ritorna ErrAwaitingUserInput(kind=approval, "Creare skill 'haiku-mode'? ...")
# → utente approva → move pending → active, Invalidate()
# → next turn: il body always:true rende a messages[1] → modalità haiku fino a skill(action=delete)

**Acceptance.**
- [ ] **SKILL.md format minimo**: frontmatter YAML obbligatorio (`name`, `description`), corpo non vuoto. `name` regex `^[a-z0-9-]+$`. **NO** `when_to_use:` o `tools:` field (rimossi dal design — la `description` ben scritta incorpora il when-to-use inline, pattern Anthropic-style confermato da `D:/tmp/assistant-ui/.claude/skills/tap/SKILL.md`). File invalidi loggati + skippati (no crash).
- [ ] Multi-root precedence: `~/.aura/skills/active/` override `internal/config/defaults/skills/`. Test asserisce override visibile in `list()`.
- [ ] TTL cache 1s preservato dal pre-rewrite.
- [ ] **Manifest packing (amended #48, D-09)**: il manifest (name + trigger-rich description, 1 riga/skill) vive nella **Description del tool `skill`** (D-06), cap a `AURA_SKILL_MANIFEST_CAP_BYTES` (~8k); oltre il cap il blocco termina con "N more — search with skill action=list {query}" (BM25 overflow — il `list` è BM25-ranked su name+description, riusa il ranker 08.1). Il body si carica on-demand via `skill(action=info)`. **NON** "tutte le skill listate anche 100+" (acceptance pre-rewrite SUPERSEDED — un manifest illimitato sfonda il context). Ordine alfabetico/byte-stable (cache-load-bearing).
- [ ] **Mutation flow (create/update/install/delete) Risk-Based (Area #5 closed 2026-05-28)**: il tool scrive in `pending/<name>/`, calcola `tier = scoring.ComputeSkillTier(action, body)` (oggi: create/update/install → RISKY, delete → DESTRUCTIVE). Ritorna tool result `{name, content_hash, risk_tier, gate_recommended:true, status:'pending'}`. **NON** ritorna automaticamente `ErrAwaitingUserInput`: l'agente decide se ri-emettere `ask_user(kind=approval, ResumeContext={action:'activate'|'install_active'|'delete_active', name})`. Resume handler: approve → `Writer.Activate()` (move atomico pending→active) + `Loader.Invalidate()` + audit `gate_taken=true`. Reject → delete pending + audit `gate_taken=true`. Se agente skippa gate: skill RESTA in `pending/` (non caricata), Notifier IMMEDIATE alert, audit `gate_taken=false`, l'utente attiva con `aura skills approve <name>` o ignora. **NO `approve` action model-facing (D-03)** — l'attivazione è solo umana/CLI. Edit ("approva con modifiche"): out of scope Slice 7.
- [ ] **Audit log + coherence matrix (amended #48, D-29)**: ogni mutation (anche le reject) scritte nella tabella `aura.skill_audit` di Postgres (sqlc-managed, migration **0010**). `aura skills audit --since=1h` query SQL. Postgres trigger `BEFORE UPDATE/DELETE` su `skill_audit` → `RAISE EXCEPTION` (audit append-only enforced a livello DB). La **5-row coherence matrix D-29** governa `(approval_source, paused_state_token, gate_recommended, gate_taken)` via CHECK SQL:

  | Event | approval_source | paused_state_token | gate_recommended | gate_taken |
  |---|---|---|---|---|
  | Mutation scritta in pending (gate non ancora esercitato) | NULL | NULL | true | false |
  | User approved via ask_user resume | `ask_user` | NOT NULL | true | true |
  | User rejected via ask_user resume | `ask_user` | NOT NULL | true | true |
  | Operatore CLI approve / `--allow-blocklisted` | `cli` (+`blocklist_override`) | NULL | true | true |
  | System (TTL `auto_archive`, `cleanup_pending_stale`) | `auto` | NULL | false | true |

- [ ] **BEFORE TRUNCATE trigger (amendment #17, Pitfall #6 P0)**: function `raise_audit_immutable_truncate()` + trigger `skill_audit_truncate_block BEFORE TRUNCATE ON aura.skill_audit FOR EACH STATEMENT`. Acceptance test (`db_integration`): connect as `aura_migrate`, `TRUNCATE aura.skill_audit;` → returns explicit error `audit table is append-only`. Cross-link role separation: connect as `aura_app`, same TRUNCATE → returns permission denied (role lacks TRUNCATE privilege). Both gates active = belt-and-suspenders defense per Pitfall #6 P0.
- [ ] **Catalog browse default-ON (amended #48, D-12 — amendment #14 FLIPPED)**: il `skill` tool advertise `action=catalog` di default (il razionale del #14 era la attack surface dello scrape HTML; il browse è ora JSON read-only via `/api/search`, niente scrape). Fresh install acceptance: `aura skills catalog list` ritorna risultati `/api/search` JSON out of the box. `aura skills disable-catalog` scrive `{catalog_enabled: false}` a `~/.aura/config.json` come **default-deny escape hatch**; quando disabilitato, `action=catalog` ritorna errore esplicito. Audit: `aura.skill_audit` INSERT row su enable/disable con `action='catalog_enable'|'catalog_disable'`, `approval_source='cli'`, `gate_recommended=false`... → corretto: usa la riga `auto`/`cli` coerente con D-29 (`gate_taken=true`).
- [ ] **`Validator.SanitizeName` chokepoint (audit round 1 P0)**: `internal/skills/paths.go` espone `SanitizeName(name) (clean string, err error)` — UNICA via per derivare path filesystem da user input. Regex `^[a-z0-9-]+$`, length 1-64, no reserved (`init`, `delete`, `.`, `..`). Writer + Deleter + Installer DEVONO chiamarlo prima di `filepath.Join(skillsDir, name)`. Test asserisce ogni file-touch site via static analysis (`grep -L 'SanitizeName' internal/skills/{writer,deleter,installer}.go` → empty).
- [ ] **skills.sh integration via native Go clone (amended #48, D-14/D-15 — npx SUPERSEDED)**:
  - **NESSUNA dipendenza `node`/`npm` host** (droppata: native clone WINS, spike 004b).
  - Catalog browse: **client JSON `GET /api/search?q=` lax-decode** (D-11, spike 003), rank by `installs`, mai `DisallowUnknownFields`. HTML scrape DEAD.
  - Install: `exec.LookPath("git")` fixed-argv `git clone --depth 1 --single-branch -c core.autocrlf=false <repoURL> <scratchDir>` (D-15) + copy skill subdir + **symlink-strip a materialization** (Lstat-no-follow — i symlink risolvono in-container, spike 005) + **Aura canonical hash** (byte-sorted `(relPath,bytes)` sha256, TOFU pin — NON l'upstream `skills-lock.json` `computedHash`, locale/platform-sensitive). Post-install `ParseSkill()` re-validation prima di `Invalidate()`.
  - **`AURA_SKILL_INSTALL_TIMEOUT_SEC` (90s)** via `context.WithTimeout`.
  - Install gate surfaces red flags (D-13): `metadata.*.install[]` blocks, tool wildcards, bundled-executable count; `always` stripped (D-10); body injection-blocklist (D-27).
  - **Acceptance**: install d'una skill che contiene un symlink fuori dal suo tree → lo strip lo rimuove a materialization, test asserisce (il `--ignore-scripts` pre-rewrite era un no-op — il clone non gira mai postinstall, D-14 — quindi quell'acceptance è moot e rimossa).
- [ ] **Capability boundary open-by-default per single-user**: nel tabula-rasa Aura locale, l'identity seed `'local'` (Slice 1.7) ha capability grant `'*'` (wildcard) — l'agente può self-extend liberamente (gate-ato comunque da `ask_user`). Capability lookup via `aura.capability_grants` (sqlc), non hard-coded: struttura estendibile per future multi-user senza toccare il codice. **Scaffolding intenzionale**: nessuna slice 1→10 ha consumer che chiama `HasCapability` (l'enforcement effettivo arriva con la slice multi-user/auth futura). Per oggi `HasCapability` è esportato e testabile, ma sempre `true` di fatto via grant `'*'` — non rimuoverlo, è il foundation pre-built per multi-user.
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
| `internal/skills/validator.go` | ~120 | Single source of truth (WRITE boundary only, D-28): regex name, size cap `AURA_SKILL_BODY_CAP_BYTES` (default 32 KiB), parse roundtrip, dup-name, **NFKC-normalize (`golang.org/x/text/unicode/norm`) THEN literal blocklist** (Pitfall: fullwidth/compat codepoints collapse to ASCII — normalize first). `FuzzSkillValidator` 10K mutations (SC#3). |
| `internal/skills/frontmatter.go` | ~80 | `goccy/go-yaml` parse + CRLF normalize + tolerate optional agentskills.io fields (D-30); the hand-roll is the anti-reference. |
| `internal/skills/paths.go` | ~40 | `SanitizeName(name) (string, error)` — **single chokepoint path-traversal guard** (audit round 1 P0). Riusato da writer/deleter/installer. Test static-analysis: ogni file-touch site DEVE chiamarlo prima di `filepath.Join`. |
| `internal/skills/writer.go` | ~120 | Atomic write a `pending/<name>/` + move pending→active. Path-traversal guard. Usato da create/update. |
| `internal/skills/deleter.go` | ~70 | FS remove da active + `Invalidate()`. Path-traversal guard. |
| `internal/skills/catalog.go` | ~140 | skills.sh `GET /api/search?q=` JSON lax-decode (D-11, spike 003), rank by `installs`, empty-query guard (server 400), transport-isolated for future public-API swap. NO HTML scrape, NO `DisallowUnknownFields`. |
| `internal/skills/installer.go` | ~140 | Native `git clone --depth 1 --single-branch -c core.autocrlf=false` (LookPath fixed-argv, D-15) + symlink-strip (Lstat-no-follow) + Aura canonical hash (byte-sorted sha256 TOFU pin). `AURA_SKILL_INSTALL_TIMEOUT_SEC` ctx. Post-install ParseSkill re-validation prima di Invalidate. Path-traversal guard. NO npx/node. |
| `internal/skills/materialize.go` | ~80 | Activation → host export dir (`AURA_SKILL_EXPORT_DIR`); de-materialize on archive/delete; symlink-strip at materialization (D-17, spike 005 — symlinks resolve in-container). |
| `internal/skills/audit.go` | ~70 | Thin adapter su sqlc `Queries.RecordSkillMutation` + `Queries.ListAuditSince`. Niente più file IO. |
| `internal/db/queries/skill_audit.sql` | ~45 | **4 query sqlc**: `RecordSkillMutation`, `ListAuditSince`, `GetByName`, `ListPendingApproval`. |
| `internal/db/migrations/0010_skill_audit.up.sql` | ~70 | **(amended #48, D-32: floor 0009, NOT 0007)** `CREATE TABLE aura.skill_audit` (id pk, ts timestamptz, actor_id uuid REFERENCES aura.identities(id), action text, name text, content_hash text, source text, `approval_source text NULL CHECK (approval_source IN ('ask_user','cli','auto'))`, `blocklist_override boolean NOT NULL DEFAULT false`, paused_state_token uuid REFERENCES aura.paused_states(token) ON DELETE SET NULL, `computed_risk_tier text NOT NULL DEFAULT 'risky' CHECK (computed_risk_tier IN ('safe','normal','risky','destructive'))`, `gate_recommended boolean NOT NULL DEFAULT true`, `gate_taken boolean NOT NULL DEFAULT true`). Indice su `ts DESC`, su `(gate_recommended, gate_taken) WHERE gate_recommended=true AND gate_taken=false`, su `(approval_source, ts DESC)`. **Triggers (Pitfall #6 P0): `skill_audit_append_only BEFORE UPDATE OR DELETE … FOR EACH ROW` + `skill_audit_truncate_block BEFORE TRUNCATE … FOR EACH STATEMENT` + role separation `aura_app` (INSERT/SELECT only, no TRUNCATE) vs `aura_migrate` (DDL).** **D-29 5-row coherence CHECK** (planner writes the exact SQL): `(approval_source IS NULL AND gate_recommended AND NOT gate_taken)` [pending] OR `(approval_source='ask_user' AND paused_state_token IS NOT NULL AND gate_taken)` OR `(approval_source='cli' AND paused_state_token IS NULL AND gate_taken)` OR `(approval_source='auto' AND paused_state_token IS NULL AND NOT gate_recommended AND gate_taken)`. (Optional `0011_snippet_runs` for per-run forensics, D-19.) |
| `internal/agent/tools/skill.go` | ~250 | **(amended #48, D-01: ONE tool, ActionRouter — NOT 7 dotted `skill_*.go`)** Copia `task.go` (Slice 6b). Non-deferred (D-05). `Execute` parses `action` discriminator → `NewActionRouter(map[string]ActionFunc{list,info,catalog,create,update,delete,install,use,restore,archive})`. Schema: `required=["action"]` solo root, per-action fields nelle description, `action` string-enum wire-safe, NO root `oneOf`/`enum` (400s DeepSeek, D-10). `Spec().Description` = `renderManifestDescription()` (turn-stable manifest, D-06). `create`/`update` → `Writer.WritePending` + `ComputeSkillTier` + gate; `delete` → DESTRUCTIVE; `install` → native clone; `use` → ritorna path in-sandbox (D-04, esecuzione via `sandbox_exec`); NO `approve` (D-03). |
| `internal/skills/loader/loader_test.go` | ~120 | Multi-root precedence + cache TTL + invalid SKILL.md skip. |
| `internal/skills/installer_test.go` | ~100 | Catalog fetch (fixture HTTP `/api/search`) + path traversal rejection + symlink-strip + ask_user flow. |
| `internal/skills/writer_test.go` | ~80 | Atomic write + pending→active move + concurrent write race. |
| `internal/skills/audit_integration_test.go` | ~80 | (`db_integration`) D-29 matrix INSERT coherence + immutability (UPDATE/DELETE/TRUNCATE rejected) + role-denied. |
| `cmd/aura/main.go` (diff) | ~+30 | Registra la UNA tool `skill` (non-deferred) in `buildBaseRegistry`. Sub-command `aura skills {list|info|create|update|delete|install|approve|always|audit|snippet …|restore|archive|disable-catalog}`. |

**Open questions.**
1. **~~`when_to_use` field~~ — ELIMINATO.** Confermato dall'esempio reale `D:/tmp/assistant-ui/.claude/skills/tap/SKILL.md` e dal pattern Claude Code: frontmatter minimo, description ricca incorpora il when-to-use.
2. **~~skills.sh endpoint~~ → CHIUSA (amended #48, D-11, spike 003): JSON `/api/search`, NON HTML scrape.** skills.sh espone `GET /api/search?q=` → `{skills:[{id,skillId,name,installs,source}]}` (probed live 2026-06-05). Lax-decode (mai `DisallowUnknownFields` — schema drifta `isDuplicate`/`count`), rank by `installs`, transport-isolato per lo swap futuro alla public API (vercel-labs/skills#426). La regex `catalogItemRE` HTML pre-rewrite è DEAD.
3. **~~Remote install source pattern~~ → CHIUSA (amended #48, D-15): native Go clone.** Il source è un repo Git clonato con `git clone --depth 1 --single-branch -c core.autocrlf=false` (LookPath fixed-argv), NON `npx skills add`. Validazione lato Aura = regex `^[A-Za-z0-9@:._/\-]{1,200}$` (no path traversal) + length cap + symlink-strip a materialization + Aura canonical hash TOFU pin.
4. **Skill versioning?** → *Default proposto: NO, fuori scope. Una skill = uno stato corrente nel filesystem. Audit log permette rollback manuale ("riapplica il content_hash X di ieri"). Versioning automatico è feature futura.*
5. **~~Cleanup pending stale~~ → CHIUSA (audit round 2 P0):** TTL 24h sui `pending/<name>/`, cleanup eseguito allo startup di Aura + ogni ora via tick. Implementato in Slice 7c con `internal/skills/cleanup.go` (~40 LOC). Logged via audit log (`action="cleanup_pending_stale"`, `name=<dir-name>`, `age_hours=<value>`).

**Commit message templates (sub-slice 7a + 7b + 7c + 7d).**

```
slice 7a: skills loader + validator + ONE skill tool (read actions)

Filesystem loader split 4-ways (filesystem/parser/cache/loader), parse-only
validation (D-28 — blocklist is write-boundary only). frontmatter.go via
goccy/go-yaml + CRLF normalize. Validator NFKC-normalize THEN literal blocklist
(ChatML/Anthropic/Llama/Llama-3/DeepSeek-Gemma-Qwen), FuzzSkillValidator. Single
chokepoint internal/skills/paths.go SanitizeName (static-analysis test). ONE
non-deferred skill tool (ActionRouter, copy of task.go) with read actions
list+info + manifest-in-Description render (D-06). TTL cache 1s.

Smoke: aura chat "fammi vedere quali competenze hai disponibili" →
skill(action=list) returns multi-root precedence resolved.

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
```

```
slice 7b: skills catalog (skills.sh /api/search JSON, default-ON)

catalog.go = GET /api/search?q= JSON lax-decode (D-11, spike 003), rank by
installs, empty-query guard, transport-isolated. NO HTML scrape, NO
DisallowUnknownFields. skill tool gains action=catalog, browse DEFAULT-ON
(D-12, amendment #14 flipped); aura skills disable-catalog escape hatch.

Smoke: aura chat "cerca su skills.sh una skill per analisi statistica" →
skill(action=catalog) returns candidate list, model presents via ask_user
kind=choice. Fresh install: aura skills catalog list works out of the box.

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
```

```
slice 7c: skills mutation (create/update/delete) + audit 0010 + governance C

Writer (atomic pending→active move via os.Rename), deleter (path-traversal
guarded), audit log on Postgres via sqlc (migration 0010_skill_audit, floor
0009 — D-32) with CREATE TABLE aura.skill_audit + D-29 5-row coherence CHECK +
index ts DESC + trigger skill_audit_truncate_block BEFORE TRUNCATE → RAISE
EXCEPTION + role-separation grants aura_app/aura_migrate (amendment #17 Pitfall
#6 P0) + trigger skill_audit_append_only BEFORE UPDATE/DELETE → RAISE EXCEPTION.
skill tool actions create/update/delete return ErrAwaitingUserInput(kind=
approval) — Loop pauses, user approves/rejects (NO model-facing approve, D-03),
resume handler activates or discards. Cleanup pending TTL 24h on startup +
hourly tick. ActionRouter from Slice 6b reused.

Smoke: aura chat "scrivi una skill che ti faccia rispondere sempre in haiku"
→ skill(action=create) writes pending → ask_user → approve → next turn the
always:true body renders at messages[1] → haiku mode.

LOC: +XXX src / +YY test / +ZZ migration.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
```

```
slice 7d: skills installer (native git clone, D-15)

Installer = native Go git clone --depth 1 --single-branch -c
core.autocrlf=false (LookPath fixed-argv, D-15 spike 004b — node/npx dep
DROPPED) + symlink-strip at materialization (Lstat-no-follow, spike 005) +
Aura canonical hash (byte-sorted (relPath,bytes) sha256 TOFU pin — NOT upstream
skills-lock.json computedHash). AURA_SKILL_INSTALL_TIMEOUT_SEC ctx, post-install
ParseSkill re-validation prior to Loader.Invalidate(). skill tool action=install
with ErrAwaitingUserInput approval gate; gate surfaces red flags (D-13:
metadata.*.install[], tool wildcards, bundled-exec count), strips always (D-10).

Smoke: malicious skill with a symlink escaping its tree → materialization
strips it, test asserts. (--ignore-scripts was a no-op — clone never runs
postinstall, D-14 — so that pre-rewrite assertion is moot/removed.)

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
```

### Slice 7e-core — Executable code snippets + TTL archived (v1; pattern analyzer → 7f v1.x per amendment #13)

> **Pattern reference**: rubato da Voyager (Wang et al., NeurIPS 2023) "skill library" pattern — agent costruisce libreria persistente di funzioni eseguibili discoverable via semantic embedding + lifelong learning. Plus mem0 procedural memory ADD-only pattern.
>
> **Idea (amended #48, D-04/D-15b/D-17/D-21)**: gli script Python/shell utili vengono **salvati** come `SKILL.md type: snippet`, **materializzati** come file su `AURA_SKILL_EXPORT_DIR` (host) montato ro su `/skills` nel container (D-17, spike 005), **scoperti** via BM25 `action=list` (D-21 — semantic Neo4j HNSW deferred a 7f/Phase 15), **eseguiti by-path** via il shipped `sandbox_exec` → `sandboxagent.Client` HTTP `:2468` (`python3 /skills/<name>.py`, interpreter+path MAI exec bit — spike 005). `action=use` ritorna le istruzioni + il path; il modello esegue via `sandbox_exec` (D-04 — il codice non ri-entra mai nel context, NESSUN bespoke `run`/`executor.go` con `sandbox.Runner.Execute`, che è DEAD). Token saving ~520/riuso.
>
> **Dipendenze (amended #48)**: il **sandbox-agent portable hardening floor** (token D-38 + seccomp + no-new-privileges + read-only rootfs + egress allowlist D-37) — il codice model-authored rende load-bearing l'isolation gap. Il gVisor `runsc` overlay + seccomp re-tightening sono una **regressione sandbox-wide di Phase 8** (D-38 scope note), tracciata lì, NON owned da Phase 11; Phase 11 dipende solo dal floor portabile. NIENTE Slice 2b bespoke session (killed, amendment #44).
>
> **Scope v1 (amendment #13)**: questa è la Slice 7e-core. `pattern_analyzer` cross-conv auto-suggest (sezione "Pattern auto-suggest" nella smoke, env vars `AURA_SKILL_PATTERN_ANALYSIS_*` + `AURA_SKILL_AUTOSUGGEST_*`) è SPLIT in Slice 7f (v1.x deferred SKILL-V2-01). 7e-core ship ~450 LOC; 7f ~250 LOC.

#### Estensione SKILL.md (backwards-compat con 7a)

Nuovo field `type: instruction|snippet` (default `instruction` per backwards-compat con Skills esistenti). Se `type: snippet`:

```yaml
---
name: parse_csv_groupby
type: snippet
language: python          # python|shell|js (multi-lang)
description: Parse CSV from URL or path, group by column, aggregate.
inputs_schema:
  source: { type: string, desc: "URL or local path" }
  by:     { type: string, desc: "Column to group by" }
  agg:    { type: string, enum: [sum, mean, count], default: sum }
outputs_desc: JSON array of {group_key, value}
deps: [pandas]            # PyPI per Python, npm per JS, none per shell
needs_network: false      # → influenza Risk-Based tier al run-time
needs_workspace: false    # → influenza sandbox mount (Slice 2b)
tags: [csv, data-analysis, groupby]
---

```python
import pandas as pd
import json, sys

args = json.load(sys.stdin)
df = pd.read_csv(args["source"])
result = df.groupby(args["by"]).agg(args.get("agg", "sum")).reset_index()
print(result.to_json(orient="records"))
```
```

Body = code block in fenced markdown con `language` matching. Parser estrae code, lo **materializza** come file su `AURA_SKILL_EXPORT_DIR` (ro `/skills` mount, D-17); `action=use` ritorna il path. L'esecuzione è **by-path via il shipped `sandbox_exec`** (`python3 /skills/<name>.py`, D-04) — NON `sandbox.Run(...)`/`sandbox.Runner.Execute(...)` (seam DEAD, amendment #44). Materializzazione solo su activation (pending/archived non materializzano; archive/delete de-materializza — mount in lockstep col loader state).

#### Pattern analysis multi-conversation (smart auto-suggest)

Background goroutine `internal/skills/pattern_analyzer.go`:

```
Loop ogni AURA_SKILL_PATTERN_ANALYSIS_INTERVAL_MIN=60 minuti:
  1. Query: tutti i tool_call con tool='execute' degli ultimi N giorni
     (default N=AURA_SKILL_PATTERN_ANALYSIS_WINDOW_DAYS=7)
     filtrato per identity_id corrente
  2. Per ogni execute call, estrai (code, args, exit_status):
     - Solo successful (exit_status=0)
     - Solo code >= AURA_SKILL_AUTOSUGGEST_MIN_LOC=20 righe
  3. Compute embedding del code (riusa aura-llama-embed Slice 0.7)
  4. Cluster via HNSW similarity threshold 0.85
  5. Per cluster size >= AURA_SKILL_AUTOSUGGEST_MIN_OCCURRENCES=3:
     - Synthesize snippet candidate (LLM call: "Generalizza questi 3
       script in una funzione parametrizzata")
     - Generate name + description + inputs_schema
     - Emit ask_user(kind=approval, ResumeContext:
       {action: 'save_snippet', candidate_yaml: ...},
       question: "Ho notato che hai eseguito 3 volte uno script per
       [X]. Vuoi salvarlo come `parse_csv_groupby`? [✅ Salva]
       [✏️ Modifica nome] [❌ No]")
  6. Risk-Based: tier RISKY (mutation) → gate_recommended=true
```

Trigger event-driven (non solo periodic): on `execute` exit_status=0, append a queue + lazy analyzer.

#### TTL 90gg + archived state

```
Metadata = sidecar JSON per-skill (D-19, the ONLY live-state source, atomic-write):
  last_used_at: timestamp ultimo successful run
  status: active|archived|deprecated
  use_count: int
  archived_at: timestamp (NULL se active)
(NON colonne ALTER su skill_audit — D-19 metadata consolidation.)

Sweep = cron TaskKind `skill_ttl_sweep` (D-16, NOT a goroutine), seeded daily
like the backup tasks (Phase-10 machinery: persistence/HA/missed-catch-up free):
  Per ogni skill type=snippet (scan sidecars, small N):
    Se last_used_at < now() - AURA_SKILL_SNIPPET_TTL_DAYS(90) AND status='active':
      status = 'archived' ; archived_at = now() ; de-materialize from /skills
      INSERT skill_audit (action='auto_archive', source='auto',
        approval_source='auto', gate_recommended=false, gate_taken=true)  # D-29 row 5
  NOTE: adding 'skill_ttl_sweep' requires ALTERing the 0009 scheduler_tasks.kind
  CHECK constraint in 0010 (the task is system-seeded, not model-schedulable).

Discovery default (skill action=list, BM25):
  WHERE status='active'  (archived skip)

Recovery:
  skill(action=restore, name) → status='active', archived_at=NULL ; re-materialize
  Audit log RESTORE entry
```

#### Componenti Slice 7e

```
internal/skills/
  snippet.go               # ~150  ParseSnippet (estende ParseSkill), validate type=snippet,
                           #       extract code block by language, optional inputs_schema (D-20
                           #       docs-only, tolerated not enforced; deps LOAD-BEARING only if
                           #       on-demand dep model — D-36)
  materialize.go           # (7a)  activation -> AURA_SKILL_EXPORT_DIR (ro /skills); de-materialize
                           #       on archive/delete; symlink-strip (D-17). NO bespoke executor.go:
                           #       exec is the shipped sandbox_exec by-path (D-04).
  metadata.go              # ~70   Sidecar JSON {last_used_at, status, use_count, archived_at}
                           #       atomic write (D-19, the ONLY live-state source)

internal/cron/handlers/skill_ttl.go  # ~80  skill_ttl_sweep cron.Handler (D-16, NOT a goroutine):
                           #       Meta{Kind:"skill_ttl_sweep"} + Run scans sidecars, archives
                           #       idle > AURA_SKILL_SNIPPET_TTL_DAYS, de-materializes, audit auto_archive

internal/agent/tools/skill.go (diff)  # ~+60  (the ONE tool — NOT new dotted tools)
  + action=use      - materializes + returns in-sandbox path + instructions (D-04;
                      model executes via sandbox_exec, NO bespoke run)
  + action=restore  - unarchives snippet, re-materializes
  + action=archive  - manual archive (tier SAFE, no gate), de-materializes

internal/db/migrations/0010_skill_audit.up.sql  # (7c) also ALTERs the 0009 scheduler_tasks.kind
                           #       CHECK to add 'skill_ttl_sweep' (system-seeded TaskKind)

internal/db/migrations/0011_snippet_runs.up.sql  # ~25  OPTIONAL per-run forensics (D-19):
    CREATE TABLE aura.snippet_runs (id uuid PK, ts, skill_name text,
      identity_id fk, conv_id fk conversations NULL,
      exit_status int, elapsed_ms int, stdout_bytes int,
      stderr_bytes int, workspace_files_generated int);
    Index (skill_name, ts DESC). (usage/archive live-state is the sidecar JSON, D-19 —
    NO last_used_at/use_count ALTER on skill_audit.)
```

Totale Slice 7e: **~830 LOC src + ~250 test + ~35 migration = ~1115 LOC**.

#### Smoke 7e

```bash
# Setup: Slice 7d completato, sandbox-agent up (make sandbox-up) + ro /skills mount

# 1. Save manuale snippet via la UNA tool skill
aura chat "Crea uno script Python che parsa un CSV e raggruppa per categoria,
salvalo come parse_csv_groupby"
# → LLM scrive code, chiama skill(action=create, type=snippet, language=python, ...)
# → ask_user("Confermi save snippet parse_csv_groupby (RISKY)?")
# → user approve → snippet salvato + materializzato in $AURA_SKILL_EXPORT_DIR (/skills ro)

# 2. Discovery BM25 (semantic Neo4j HNSW deferred a 7f/Phase 15, D-21)
aura chat "ho un CSV vendite per regione, raggruppami per regione"
# → LLM chiama skill(action=list, query="CSV groupby")  # BM25-ranked
# → top match: parse_csv_groupby
# → LLM chiama skill(action=use, name="parse_csv_groupby") → ritorna path /skills/parse_csv_groupby.py
# → LLM chiama sandbox_exec(command=python3, args=["/skills/parse_csv_groupby.py", ...])  # D-04 by-path
# → ToolResult JSON array → LLM presenta risultato

# === SECTION 3 BELOW DEFERRED TO SLICE 7f (v1.x) PER AMENDMENT #13 — kept here for forward-reference only ===
# 3. Pattern auto-suggest (background)
aura chat "esegui pandas read_csv su X, groupby Y"  # 1ª volta
aura chat "esegui pandas read_csv su A, groupby B"  # 2ª volta
aura chat "esegui pandas read_csv su C, groupby D"  # 3ª volta
# (60min dopo) pattern_analyzer detecta cluster size=3 similarity=0.92
# → ask_user inviato a Telegram:
#   "Ho notato 3 script simili recenti. Vuoi salvarli come
#    `pandas_csv_groupby_template`? [✅ Salva] [✏️ Modifica] [❌ No]"
# === END DEFERRED SECTION (Slice 7f / SKILL-V2-01) ===

# 4. TTL archived
# (90 giorni dopo, cron skill_ttl_sweep TaskKind — D-16, NOT a goroutine)
# → skill 'old_script_unused' status='archived', audit auto_archive, de-materialized
aura skills list                  # 'old_script_unused' nascosto (archived)
aura skills list --include-archived   # mostra
aura skills restore old_script_unused  # status='active' reseted, re-materialized
```

#### Acceptance 7e (amended #48)

- [ ] `internal/skills/snippet.go` parse `SKILL.md type: snippet` con language enum (python/shell/js), extract code block by language matching. `inputs_schema`/`outputs_desc`/`tags` = OPTIONAL docs-only (D-20, tolerated not enforced); `deps` LOAD-BEARING only under the on-demand dep model (D-36); `needs_network`/`needs_workspace` surfaced in the SAVE-time risk gate.
- [ ] **Execution is by-path via the shipped `sandbox_exec` → `sandboxagent.Client` :2468 (D-04/D-15b)** — `action=use` materializes the file under `/skills` (ro) and returns `python3 /skills/<name>.py` (interpreter+path, NEVER exec bit — spike 005). NO `internal/skills/executor.go`, NO `sandbox.Runner.Execute` (DEAD seam, amendment #44).
- [ ] **TTL sweep = cron `skill_ttl_sweep` TaskKind (D-16, NOT a goroutine)** — `internal/cron/handlers/skill_ttl.go` implements `cron.Handler`, seeded daily; scans sidecars, archives idle > `AURA_SKILL_SNIPPET_TTL_DAYS`, de-materializes, audit `auto_archive` (D-29 row 5). The 0010 migration ALTERs the 0009 `scheduler_tasks.kind` CHECK to admit `skill_ttl_sweep`.
- [ ] `skill(action=use, name)`: materializes + returns in-sandbox path + instructions; updates sidecar `last_used_at` + `use_count++` + optional INSERT `snippet_runs` row.
- [ ] `skill(action=restore, name)`: unarchives + re-materializes, audit RESTORE entry.
- [ ] `skill(action=archive, name)`: manual archive (tier SAFE, no gate) + de-materializes, audit MANUAL_ARCHIVE entry.
- [ ] Discovery filtro: `skill(action=list)` default `status='active'` (BM25-ranked, D-21). `--include-archived` flag/arg per mostrare tutti.
- [ ] **Migration 0010** (floor 0009, D-32) carries `skill_audit` + the `scheduler_tasks.kind` CHECK widening; usage/archive live-state is the sidecar JSON (D-19 — NO `last_used_at`/`use_count` ALTER on `skill_audit`). Optional **0011** for `aura.snippet_runs` per-run forensics.
- [ ] Test `skill_ttl_sweep` handler (`db_integration`): seeded task fires; snippet con `last_used_at = now()-91d` → archived next sweep.
- [ ] Test by-path exec (`sandbox_integration db_integration`): `action=use` returns the `/skills/<name>.py` path; `sandbox_exec` executes it; output captured (SC#4).
- [ ] Risk-Based: snippet `needs_network=true OR needs_workspace=true` → tier RISKY al save (gate_recommended=true). `needs_network:true` routes through the allowlisted host forward-proxy at RISKY tier (D-37; advisory on Docker Desktop, enforced on native-Linux). Default `needs_*=false` → tier SAFE.

#### Open questions Slice 7e

1. **Synth LLM call cost**: pattern_analyzer chiama LLM tier=reasoning per ogni cluster candidate (~2K token in + 500 out ≈ $0.01). 5 cluster/giorno × 30 = $1.5/mese. Acceptable. *Default proposto*: yes.
2. **Cross-identity sharing**: snippet privato per `identity_id` (Slice 1.7) o library globale Aura condivisa? *Default proposto*: privato per identity, future slice multi-user aggiunge `aura skill share <name> --to <identity>` con governance.
3. **Snippet versioning**: cosa succede su `skill.update` di un snippet già usato N volte? *Default proposto*: versioning implicito via `content_hash` in `skill_audit`. Snippet attivo = latest version. Past runs riferiscono content_hash storico per forensics.
4. **Workspace files cleanup**: snippet che produce files in `/workspace` → quanto persistono? *Default proposto*: workspace è scope conversation (Slice 1.8 cleanup cascade). Snippet generati files sopravvivono finché conversation viva, eliminati a `aura chat delete`.

#### Commit message template Slice 7e

```
slice 7e-core: executable code snippets + TTL archived (v1; pattern analyzer → 7f v1.x per amendment #13)

Estende SKILL.md formato con type=snippet (multi-lang python/shell/js),
eseguibili by-path via il shipped sandbox_exec -> sandboxagent.Client :2468
(D-04/D-15b — NO sandbox 2b bespoke, killed amendment #44). Pattern reference:
Voyager (Wang et al., NeurIPS 2023) skill library + mem0 procedural memory.

SKILL.md extended fields:
- type: instruction|snippet (default instruction, backwards-compat 7a)
- language: python|shell|js (multi-lang, all in the sandbox image)
- inputs_schema/outputs_desc/tags: OPTIONAL docs-only (D-20, tolerated)
- deps: array (LOAD-BEARING only under on-demand dep model, D-36)
- needs_network/needs_workspace: surfaced in the SAVE-time risk gate (D-37)

Snippet exec: action=use materializes the file under /skills (ro mount,
D-17) and returns python3 /skills/<name>.py (interpreter+path, NEVER exec
bit — spike 005); the model runs it via sandbox_exec (D-04). NO executor.go,
NO sandbox.Runner.Execute. Updates sidecar last_used_at + use_count + optional
snippet_runs row.

TTL sweep = cron skill_ttl_sweep TaskKind (D-16, NOT a goroutine — Phase-10
persistence/HA/catch-up free), seeded daily:
- snippet idle > AURA_SKILL_SNIPPET_TTL_DAYS -> status='archived' + audit
  auto_archive (D-29 row 5) + de-materialize from /skills
- Discovery default WHERE status='active' (BM25, archived skip — semantic
  Neo4j HNSW deferred 7f/Phase 15, D-21)
- skill(action=restore) unarchives + re-materializes + audit RESTORE

Migration 0010 (floor 0009, D-32): skill_audit + D-29 coherence CHECK +
ALTER 0009 scheduler_tasks.kind CHECK to admit skill_ttl_sweep.
Migration 0011 (optional): aura.snippet_runs per-run forensics (D-19 —
usage/archive live-state is the sidecar JSON, NO skill_audit ALTER).

Tool actions added to the ONE skill tool:
  action=use      - materializes + returns in-sandbox path (D-04)
  action=restore  - unarchives + re-materializes
  action=archive  - manual archive (tier SAFE)

Env nuove (Caps & Limits indice, D-34):
- AURA_SKILL_SNIPPET_TTL_DAYS=90
- AURA_SKILL_EXPORT_DIR=~/.aura/skills/export

Dipendenze: il sandbox-agent portable hardening floor (token D-38 + seccomp +
no-new-priv + ro-rootfs + egress allowlist D-37). gVisor overlay = Phase-8
regression (D-38 scope note), NOT owned here.

LOC: +XXX src / +YY test / +ZZ migration.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
```

---

## Slice 8 — AG-UI gateway (transport agnostico CLI / Telegram / web)

> **Slice 0.9 amendment**: l'AG-UI emitter consuma `iter.Seq2[*Event, error]` direttamente da `(*LlmAgent).Run()` (Slice 0.9). Mapping `*Event` Aura → AG-UI event types diventa una sola funzione `mapToAGUI(*Event) []agui.Event` invece di subscribere a 5 channel/callback diversi del Loop. Saving stimato: **−100 LOC** (no Subscribe/emitter plumbing custom, range-over-func nativo).

**Goal.** Esporre il Loop di Aura attraverso il protocollo **AG-UI** ([ag-ui-protocol/ag-ui](https://github.com/ag-ui-protocol/ag-ui)), MIT, ~17-25 event types standard, transport SSE/WS/webhook. Aura diventa un'agent compatibile con qualsiasi UI AG-UI-aware (CopilotKit, AG-UI Dojo, future custom frontend, Telegram bot adapter, browser SPA). Niente lock-in al transport; sostituisce/preempt qualsiasi channel-adapter custom.

**Razionale.** Out-of-scope ha sempre tenuto fuori Telegram, dashboard, web. Risolto via standard invece che codice custom: implementi una volta il protocollo, ogni transport ha il suo client (Telegram bot in-process via SSE, browser via fetch+SSE, CLI in-process). Pattern verificato production-grade: LangGraph + CrewAI (1st party), Microsoft Agent Framework + Google ADK + AWS Strands + Mastra + Pydantic AI + Agno + LlamaIndex + AG2 (1st party integrations), Claude Agent SDK + Langroid (community).

**Pre-requisiti.**
- Slice 1.8 conversation persistence (`threadId` = `conversation_id`).
- Slice 1.5 multi-pause FIFO (`outcome.interrupted` con `interrupts[]` mappato da `PausedState[]`).
- Slice 1 streaming SSE accumulator (event delta merging già pronto).
- Slice 1.7 identity (per future auth dell'endpoint `aura serve`, oggi local-only).

**Dipendenza Go.**
```go
// go.mod
require github.com/ag-ui-protocol/ag-ui/sdks/community/go <SHA-post-2026-05-14> // SHA-pinned per amendment #6 (pseudo-version repo; pin to commit ≥ 2026-05-14). At Phase 12 Slice 8 impl time, resolve via `go list -m -json github.com/ag-ui-protocol/ag-ui/sdks/community/go@HEAD`.
```

**Smoke.**
```bash
./aura serve --agui-port=9080 &      # background

# AG-UI Dojo client (npx) si connette
npx @ag-ui/dojo --backend http://127.0.0.1:9080
# → UI mostra chat, invio "ciao dimmi 2+2"
# → backend stream-a TEXT_MESSAGE_CONTENT delta token-per-token
# → UI rende "Risposta: 4"

# Test minimo via curl
curl -N -X POST http://127.0.0.1:9080/agent/run \
  -H 'Content-Type: application/json' \
  -d '{"threadId":"<conv_uuid>","runId":"<auto>","messages":[{"role":"user","content":"ciao"}]}'
# → SSE stream:
# event: RUN_STARTED
# data: {"threadId":"...","runId":"..."}
# event: TEXT_MESSAGE_START
# data: {"messageId":"msg-1","role":"assistant"}
# event: TEXT_MESSAGE_CONTENT
# data: {"messageId":"msg-1","delta":"Ciao"}
# ...
# event: RUN_FINISHED
# data: {"threadId":"...","runId":"...","outcome":{"type":"success"}}
```

**Acceptance.**
- [ ] **[8b — DEFERITO, amendment #35]** `aura serve --agui-port=<N>` (default `9080`) avvia HTTP server con endpoint `POST /agent/run` e `GET /threads/<id>/messages`. Lifecycle gestito (start/stop su SIGTERM), graceful drain delle connessioni SSE attive a shutdown. (8b atterra solo con un client HTTP reale + auth; non pre-req di Slice 9.)
- [ ] **Mapping Aura ↔ AG-UI**:
  - `threadId` = `aura.conversations.id` (Slice 1.8). Se thread non esiste in DB → 404. Se esiste ma `identity_id != local` → 403 (future auth).
  - `runId` = generated server-side (UUID v4 prefix `run-`) per ogni `POST /agent/run`. Persisted in `aura.conversation_turns.metadata.run_id` per audit.
  - `messageId` = UUID prefix `msg-`. Mapped 1:1 con `conversation_turns.seq` via metadata jsonb.
  - `toolCallId` = ID dal LLM (es. DeepSeek/OpenRouter `tool_call_id`). Persisted in `conversation_turns.tool_call_id`.
- [ ] **Eventi emessi dal Loop** (post-Slice 8 il Loop ha un `emitter` interface):
  - `RUN_STARTED` all'inizio di `Loop.Turn`.
  - `STEP_STARTED` / `STEP_FINISHED` per ogni iterazione (LLM call + tool dispatch).
  - `TEXT_MESSAGE_START` / `TEXT_MESSAGE_CONTENT` (delta token-per-token) / `TEXT_MESSAGE_END` per il contenuto assistant.
  - `TOOL_CALL_START` / `TOOL_CALL_ARGS` (delta JSON args) / `TOOL_CALL_END` per ogni tool call emesso dal LLM.
  - `TOOL_CALL_RESULT` (con `content` = preview + footer di `ToolResult`) dopo l'esecuzione.
  - `MESSAGES_SNAPSHOT` su `GET /threads/<id>/messages` (rehydration UI client).
  - `STATE_SNAPSHOT` opzionale al run start (current state della conv).
  - `STATE_DELTA` con `JSONPatchOperation[]` (RFC 6902) per update incrementali (es. cost USD running sum).
  - `RUN_FINISHED` con `outcome.type ∈ {success, interrupted, errored}`. Se `interrupted`, `outcome.interrupts[]` mappato da `pending []*PausedState` (Slice 1.5 multi-pause).
  - `RUN_ERROR` per failure non recuperabili (LLM 5xx finale, panic loop, etc.).
- [ ] **REASONING_* events (dual-field, amendment #33)**: se `reasoning_content` (stile DeepSeek-V4 remoto) **OPPURE `reasoning`** (vLLM ha rimosso il campo deprecato `reasoning_content` allineandosi alla guidance OpenAI — sul path local-LLM DGX il reasoning arriva qui) è presente in `usage.*`/`delta.*`, il client wire emette `REASONING_START` / `REASONING_MESSAGE_CONTENT` / `REASONING_END` parallelamente ai TEXT_MESSAGE_* (separate messageId). Accept-both, niente parse semantico, stream byte-per-byte. **Senza il dual-field il path DGX/`local-only` non emette mai reasoning** → la status-line "💭 Reasoning..." di Telegram resta vuota proprio sul deployment max-privacy. Acceptance: fixture SSE golden per entrambi i nomi campo → stessi eventi.
- [ ] **Resume contract**: per riprendere un Loop interrupted, il client invia un nuovo `POST /agent/run` con stesso `threadId` + nuovo `runId` + `messages` includono i RoleTool answers per gli interrupts precedenti (matching su `tool_call_id`). Server riconosce e ricostruisce lo stato Loop via `LoadHistory + ResumeBatch(answers)`.
- [ ] **Auth dell'endpoint (Out of scope esplicito)**: niente auth in Slice 8 (continuità con Out of scope "Auth dell'endpoint `aura serve`"). Endpoint bind di default a `127.0.0.1:9080` (local-only). Future slice auth aggiunge bearer token + identity FK alle conversations.

> **Amendment #35 (D15 split + reconciliation D00/D13, 2026-06-01) — RATIFY AG-UI + 3 fix.** AG-UI ratificato come transport standard 2026 (CopilotKit Series A 27M$ mag-2026; integrazioni 1st-party Google/Microsoft/AWS/Oracle; SSE copre il 99% dei casi; blast-radius confinato a `internal/agui/` ~440 LOC via translator puro → orphan-risk basso; WebSocket-rejected corretto). Tre aggiustamenti: **(1) Split 8a/8b.** Telegram (Slice 9b) consuma via **fanout in-process, NON via HTTP/SSE** (`agui_subscriber.go`/`fanout.go`) → solo `translator.go`+`fanout.go`+`client.go`+`types.go`+property-test sono sul critical-path = **Slice 8a** (pre-req di Slice 9). L'endpoint HTTP (`server.go`), `aura serve`, la Dojo conformance suite e `aura chat --via-agui` non hanno client in v1 (dashboard SPA out-of-scope) = **Slice 8b**, deferita finché non esiste un client HTTP reale **e** l'auth è progettata. **(2) Cloud-auth/D00 footgun (8b).** Il bind `127.0.0.1` + auth-out-of-scope è corretto su mini-PC/DGX (loopback) ma non copre il target **Hetzner cloud** (D00). `--bind` su indirizzo non-loopback DEVE richiedere auth (futura) e **fail-fast sotto `AURA_PRIVACY_MODE=local-only`** (coerente boot-fail #30/#33). Il check `identity_id != local → 403` è authorization SENZA authentication: non è un controllo di sicurezza finché l'auth non atterra. **(3) Reasoning dual-field** già risolto in Slice 1 (amendment #33): il renderer mappa REASONING_* da entrambi `reasoning`/`reasoning_content`.
- [ ] **CORS**: header permissivi per dev (`Access-Control-Allow-Origin: *` se `AURA_AGUI_CORS_PERMISSIVE=1`, default `*` per `127.0.0.1` origin). Future restrittivo via env.
- [ ] **Backpressure**: SSE writer buffered channel (capacity 64); su client lento, channel full → drop con WARN log + `RUN_ERROR` se persistente. Niente blocco del Loop.
- [ ] **[8b — DEFERITO, amendment #35] AG-UI Dojo compliance**: test integrazione che esegue il Dojo conformance suite (50-200 LOC per building block come da spec) contro `aura serve --agui-port`. Validation: text streaming, tool calls, state sync, HITL interrupts.
- [ ] Test unitario translator: input sequenza `*agent.Event` (Slice 0.9) → output `[]events.Event` AG-UI sequenza corretta. Property-based su tutti gli ~25 event types.
- [ ] **go.mod pin verification (amendment #6)**: `go.mod` for `github.com/ag-ui-protocol/ag-ui/sdks/community/go` MUST be a specific commit SHA (NOT `latest`, NOT a date-based pseudo-version `v0.0.0-YYYYMMDDHHMMSS-xxxxxxxxxxxx` resolved at install time). CI gate: `grep -E '^require github\.com/ag-ui-protocol/ag-ui/sdks/community/go [a-f0-9]{40}$' go.mod` must return exactly 1 match.

**File targets** (~600 LOC src + ~300 test — saving −100 LOC riapplicato da Slice 0.9: il translator consume `iter.Seq2[*Event, error]` direttamente, no `Emitter` interface custom né plumbing channel-based):
| Path | LOC | Note |
|---|---|---|
| `internal/agui/client.go` | ~80 | Wrapper sul Go SDK community `core/events`. Type aliases per evitare leak del package esterno nei call sites. |
| `internal/agui/translator.go` | ~180 | `Translate(seq iter.Seq2[*agent.Event, error]) iter.Seq2[events.Event, error]` — mapping deterministico 1:N da `*agent.Event` Slice 0.9 a AG-UI event types (text/tool/state/lifecycle/reasoning, ~25 types). ID generation via `IDGenerator` interface. Funzione pura, no mutable state cross-event eccetto IDs. Sostituisce `Emitter` interface pre-0.9 (drop ~180 LOC plumbing). |
| `internal/agui/fanout.go` | ~80 | Fanout helper per in-process subscribers (Slice 9b Telegram consumer): `Fanout` struct wrappa `iter.Seq2[*agent.Event, error]` e distribuisce a N subscriber channel concurrent. Aggiunto qui per centralizzare la dependency Slice 9b → Slice 8 (era retro-aggiunto in Slice 9, fix audit Round 1). |
| `internal/agui/server.go` **[8b deferito #35]** | ~200 | HTTP server: `POST /agent/run` (SSE response), `GET /threads/<id>/messages` (JSON `MESSAGES_SNAPSHOT`). Consume `(*LlmAgent).Run(ctx) iter.Seq2[*Event, error]` direttamente, passa a `Translate`, scrive SSE. Backpressure-bounded (buffered channel cap 64), CORS, graceful shutdown. `--bind` non-loopback richiede auth + fail-fast sotto `local-only` (#35). |
| `internal/agui/types.go` | ~80 | `RunAgentInput` parser, validation (`threadId` UUID, `messages[]` non vuoto, etc.), helpers. |
| `cmd/aura/main.go` (diff) | ~+80 | Subcommand `aura serve [--agui-port <N>] [--bind <addr>]`. |
| `cmd/aura/chat.go` (diff) **[8b deferito #35]** | ~+50 | `aura chat --via-agui` flag opzionale (passa per il server invece che in-process). Default in-process. **Note refactor-on-touch**: questo file viene rinominato/migrato a `internal/channels/cli/cli.go` in Slice 9a — la diff `+50` qui resta come delta intermedio, Slice 9a refactor la assorbe. |
| `internal/agui/server_test.go` **[8b deferito #35]** | ~180 | Integration test: spawn server, run AG-UI Dojo conformance suite, assert eventi. |
| `internal/agui/translator_test.go` | ~120 | Property-based translator coverage. Fixture sequenza `*agent.Event` → expected AG-UI events. |

**Deferred-tool partition.** Niente tool LLM-facing nuovo. AG-UI è transport infrastructure, non capability LLM-facing.

**Open questions.**
1. **Endpoint path canonico**: la spec ufficiale AG-UI suggerisce `POST /agent/run`? → *Default proposto*: `/agent/run` (allineato con SDK Go community sample). Configurabile via `AURA_AGUI_PATH_RUN` env.
2. **WebSocket transport?** → *Decisione*: Slice 8 implementa solo SSE (default AG-UI). WebSocket è transport opzionale (1 client supportato, non frontend tipico). Atterra in slice futura se serve telecom-grade bidi.
3. **Cost streaming via STATE_DELTA**: `running_cost_usd` aggiornato per turn via JSONPatch delta? → *Default proposto*: SÌ. Pattern: `STATE_DELTA { delta: [{op:"replace", path:"/cost_usd", value:0.0042}] }` dopo ogni LLM call.
4. **CLI default mode**: in-process o via-agui? → *Decisione differita post-benchmark*: Slice 8 implementa entrambe (in-process default), `aura chat --via-agui` flag opzionale. Scegliamo dopo aver misurato latency aggiunto su prompt tipico (~50-150 ms attesi per il roundtrip HTTP loopback).
5. **Telegram bot adapter slot**: dopo Slice 8 o in slice dedicata? → *Slice 9 dedicata*: `internal/channels/telegram/` come AG-UI client process (`aura telegram-bot` subcommand). Riusa eventi standard, traduzione one-way (events → Telegram messages). HITL interrupts → Telegram inline keyboard buttons. Scope separato.

**Mini-PC RAM budget.** AG-UI server è una goroutine HTTP minimal. Buffer 64 event/connection × N connessioni concurrent. Per Aura single-user local: 1-3 connessioni attese (CLI + opzionale Dojo o Telegram bot). RAM negligible (~5-10 MB heap aggiuntivi).

**Commit message template.**
```
slice 8: AG-UI gateway (SSE event protocol transport)

Aura speaks AG-UI (ag-ui-protocol/ag-ui, MIT). Endpoint
aura serve --agui-port=9080 expone POST /agent/run (SSE stream)
+ GET /threads/<id>/messages (snapshot). 25 event types emessi
dal Loop via internal/agui/emitter.

Mapping Aura -> AG-UI:
  conversations.id (1.8)      <-> threadId
  Loop.Turn                   <-> runId (uuid)
  PausedState[] (1.5 #4)      <-> outcome.interrupted.interrupts[]
  ToolResult (1)              <-> TOOL_CALL_RESULT.content
  RoleTool.ToolCallID         <-> tool_call_id matching su resume

Reasoning events (10 types) emessi se provider returns
reasoning_content (DeepSeek-V4) OPPURE reasoning (vLLM/local,
amendment #33 dual-field). Future-proof.

Translator pattern (Slice 0.9): translate(iter.Seq2[*agent.Event]) ->
iter.Seq2[events.Event] funzione pura, no Emitter interface custom.
Server.go consuma (*LlmAgent).Run(ctx) direttamente via range-over-func.
Fanout helper (~80 LOC) centralizza dependency Slice 9b in-process
subscriber, evita retro-aggiunta.

Out of scope mantenuto: auth dell'endpoint (slice dedicata).
Bind default 127.0.0.1, CORS env-driven.

CLI default in-process (zero overhead), --via-agui opt-in per
testing transport. Benchmark per scegliere mode finale post-1.9.

Dipendenza go.mod: github.com/ag-ui-protocol/ag-ui/sdks/community/go (SHA-pinned per amendment #6 — no pseudo-version `latest`).

Smoke: aura serve + curl /agent/run mostra SSE event stream
conforme. AG-UI Dojo conformance suite verde.

Telegram bot + web frontend = Slice 9 / future dedicate (Telegram
in-process subscriber via internal/agui/fanout, web SPA HTTP SSE
client esterno).

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Slice 9 — Channels framework + Telegram (main user-facing) + Setup wizard + multimodal

> **Atomicity note:** ~2330 LOC totali = troppo per 1 commit. Diviso in 3 sub-slice atomici:
> - **9a**: Channel framework + Setup wizard (QR/deep-link onboarding via web). ~1010 LOC + 300 test.
> - **9b**: Telegram impl (bot, renderer, status pane, markdown, HITL, commands). ~920 LOC + 300 test.
> - **9c**: Multimodal (voice + photo via Gemma 4 sidecar, documents via markitdown). ~400 LOC + 150 test.
> Ogni sub-slice atomic-commit, smoke green prima del successivo.

**Goal.** Aura passa da CLI-only a multi-channel agnostico, con **Telegram come main user-facing channel** (gli utenti finali non usano CLI). Architettura `internal/channels/<name>/` permette di aggiungere WhatsApp/Discord/Signal futuri come slice incrementali. CLI rimane debug-only per dev. Setup completo via web wizard (paste bot token + scan QR del deep link Telegram), niente codici testuali.

Slice 9 risolve Area #16 (transport agnostico) all'altezza prodotto: Slice 8 ha esposto AG-UI come standard, Slice 9 ne cabla il primo client production-grade (Telegram in-process subscriber).

### Pre-requisiti

- Slice 1 LLM client + ToolResult sidecar (riusato per content > cap)
- Slice 1.5 ask_user + multi-pause FIFO (HITL via inline keyboard)
- Slice 1.7 identities + capability_grants (telegram_accounts FK)
- Slice 1.8 conversation persistence (threadId mapping)
- Risk-Based Governance (sezione cross-cutting, rendering risk_tier in messaggi approval)
- Slice 8a AG-UI translator + fanout channel per in-process subscribers (NON l'HTTP server 8b, deferito — amendment #35)

### Dipendenze Go nuove

```go
// go.mod (Slice 9b)
require gopkg.in/telebot.v4 <SHA-post-2026-05-08>                 // bot library — SHA-pinned per amendment #5 (untagged repo; pin to specific commit ≥ 2026-05-08 to avoid silent master drift). At Phase 1 Slice 9b implementation time, resolve to the latest commit SHA via `go list -m -json gopkg.in/telebot.v4@HEAD` and replace the placeholder.
require github.com/skip2/go-qrcode latest                       // QR code generation
require github.com/mdp/qrterminal/v3 latest                     // ASCII QR per console
```

### Decisioni cumulate (chiusura Punti 1-8 discussione 2026-05-28)

| Punto | Aspetto | Decisione |
|---|---|---|
| 1 | Activation trigger | `aura serve` boot di tutti i channel configurati (Telegram main user-facing, env-driven, flag `--no-telegram` per debug). |
| 1 | Setup wizard | Sempre on `http://<bind>:9081/setup?token=<AURA_SETUP_TOKEN>`, one-time token `AURA_SETUP_TOKEN` (random UUIDv4) generato + stampato a stdout primo boot (`aura serve`), gated middleware su tutti i `/setup/*` endpoint. Paste bot token + valida via getMe + mostra QR del deep link `t.me/<bot>?start=<onboarding_token>`. Per amendment #10 — previene unauthorized access se `AURA_SETUP_BIND=0.0.0.0:9081`. |
| 1 | Onboarding flow | User scan QR → Telegram apre → `/start <token>` → bot match in `aura.telegram_setup_pending` → INSERT `aura.telegram_accounts`. No codici testuali, full QR/deep-link. |
| 2 | Rate limit | Adaptive con 429 `retry_after` parse + exponential backoff fino a 30s. |
| 3 | Status pane | Pattern master B (2 msg per turn: status pane edited + content reply). LOC ridotti a ~180 (vs master 563). |
| 3 | Throttle status | `AURA_TELEGRAM_STATUS_THROTTLE_MS=1500` (info di servizio, lento) |
| 3 | Throttle content | `AURA_TELEGRAM_CONTENT_THROTTLE_MS=500` (token streaming, veloce) |
| 3 | Chat rate hard | `AURA_TELEGRAM_CHAT_RATE_LIMIT_MS=1000` (queue drain serializzato per chat_id, sopra il debounce per-pane, rispetta 1/sec Telegram) |
| 3 | Markdown library | Custom ~80 LOC MarkdownV2 escaper in `internal/channels/telegram/mdv2.go` (per amendment #4 — promotes port to default. Rationale: `eekstunt/telegramify-markdown-go` is 4-star supply-chain risk per SUMMARY.md Stack P2; in-tree escaper covers MarkdownV2 reserved-char subset, zero external dep) |
| 4 | Approval UX | Renderer agnostico: legge `ask_user.options`. Assenti → 2 button hardcoded `✅ Approva / ❌ Rifiuta`. Presenti → render exact. Agente decide 2/3/N-way via options. |
| 4 | Reply testuale | Reply quote a un pending = nuovo turn user **parallelo** (multi-pause FIFO Slice 1.5 #4 coda i due). Tappare button = resume del pending originale. |
| 5 | Voice failure | 2 retry exponential (1s/2s), poi bot risponde `❌ Trascrizione non disponibile. Invia testo o riprova.` + reaction 😵 sul voice message. |
| 6 | Document size | Tiered: sync ≤5 MB inline / async background 5-50 MB con `📄 Convertendo nome.pdf...` follow-up / refuse >50 MB. |
| 6 | Convert lifecycle | Bot intermediario: niente INSERT in `conversation_turns` finché conversione ready. A done: 1 sendMessage real + AG-UI user message con contenuto convertito. |
| 7 | Vision model | Gemma 4 multimodal sidecar (`aura-llama-multimodal`, llama.cpp server). Variant TBD post-benchmark (E2B/E4B/26B), default baseline **E4B Q4_0** (~3 GB RAM, audio + image nativi). |
| 7 | Whisper | **Rimosso** da compose.yaml (Gemma 4 E4B audio nativo unifica STT). -300 MB RAM, 1 sidecar in meno. |
| 7 | Vision fallback | TBD post-benchmark: se Gemma quality basta → solo vision sidecar; altrimenti → markitdown OCR fallback se Gemma sidecar down. |
| 8 | Bot commands MVP | 10 commands: `/start /help /whoami /cancel /new /conversations /resume /reset /cost /search` (amendment #8 adds `/cost`; amendment #7 adds `/search`). |
| 8 | Command dispatch | Bot-intercept (no LLM call per commands). `/cancel` chiama direttamente `Loop.Cancel`, `/new` crea conv via Slice 1.8 `Create`, ecc. Solo `/start <token>` ha logica onboarding speciale. |

### Architettura componenti

```
internal/channels/
  channel.go              # ~70   Interface { Name(), Start(ctx, sub), Stop(), IsHealthy() }
  registry.go             # ~100  Lifecycle orchestration (StartAll/StopAll, error aggregation)
  cli/cli.go              # ~150  CLI as channel (refactor cmd/aura/chat.go Slice 1.8 → qui)

internal/setup/                # Slice 9a
  server.go               # ~150  HTTP server isolato porto 9081 (sempre on)
  handlers.go             # ~200  POST /setup/token (validate getMe), POST /setup/onboard-link, SSE /setup/events
  page.html               # ~250  Embedded HTML+CSS+JS, dark theme stile master
  qr.go                   # ~50   QR SVG generation (skip2/go-qrcode)
  types.go                # ~40

internal/channels/telegram/    # Slice 9b
  bot.go                  # ~100  tele.Bot wrapper, polling, lifecycle (Start/Stop come goroutine)
  agui_subscriber.go      # ~140  subscribe al fanout channel di internal/agui/emitter (in-process, no HTTP)
  renderer.go             # ~250  AG-UI events → Telegram messages
                          #       - Pattern master B: 2 msg per turn
                          #       - Throttle status 1500ms + content 500ms + chat queue 1000ms
                          #       - 429 backoff adaptive
                          #       - tool_call_result preview + sidecar pointer (Cursor pattern)
  status_pane.go          # ~180  Status pane manager (master pattern ridotto)
                          #       - sendMessage iniziale "⏳ thinking..."
                          #       - editMessageText cumulativo: tool calls list con glyph 🟡/✅/❌
                          #       - contentMode collapse a content start (footer compact)
                          #       - finalize a RUN_FINISHED
  hitl.go                 # ~150  ask_user pending → Telegram UX:
                          #       - kind=approval/choice + options → InlineKeyboardMarkup
                          #         (callback_data="resume:<token>:<idx>")
                          #       - kind=clarification senza options → ForceReply
                          #       - reply quote a pending msg → nuovo turn parallelo (FIFO)
                          #       - Callback handler chiama agui.ResumeViaSubscriber(token, answer)
  commands.go             # ~210  10 commands MVP (incl. /cost per amendment #8, /search per amendment #7) bot-intercept:
                          #       /start [token] - welcome + onboarding consume
                          #       /help - lista commands + breve guida
                          #       /whoami - tg_user_id + identity Aura + conv corrente
                          #       /cancel - Loop.Cancel(conv_id) per active turn
                          #       /new - Conversations.Create()
                          #       /conversations - list paginato 5/page con InlineKeyboard nav
                          #       /resume <id_prefix> - switch to conv by prefix match
                          #       /reset - conv.Delete + new
                          #       /cost - aura.cost_aggregator.TodayUSD() + per-conv breakdown (amendment #8)
                          #       /search <query> - SearchConversationTurns via pg_trgm (Slice 1.8.5, amendment #7)
  onboarding.go           # ~80   /start <onboarding_token> matcher → INSERT telegram_accounts
                          #       + delete pending row + emit SSE /setup/events "completed"
  config.go               # ~60   BotToken (env) + AllowedFallback (deprecated env) + bind addr
  store.go                # ~80   sqlc adapter aura.telegram_accounts + telegram_setup_pending

internal/channels/telegram/    # Slice 9c (multimodal)
  voice.go                # ~150  POST a aura-llama-multimodal /v1/audio/transcriptions
                          #       - 2 retry exp 1s/2s, hard fail con UX message + reaction 😵
                          #       - download voice via Telegram API getFile + downloadURL
                          #       - transcript text → user message AG-UI
  documents.go            # ~160  Tiered sync/async via markitdown sidecar /convert
                          #       - ≤5 MB sync (HTTP timeout 30s)
                          #       - 5-50 MB async: sendMessage placeholder + background goroutine + edit on done
                          #       - >50 MB refuse con messaggio
                          #       - output > AURA_CONVERSATION_TURN_CAP_BYTES → sidecar (Slice 1.8 pattern)
  photo.go                # ~90   POST a aura-llama-multimodal /v1/chat/completions con image_url
                          #       - base64 encode photo
                          #       - prompt "Describe this image briefly"
                          #       - description text → user message AG-UI
                          #       - Fallback strategy TBD post-benchmark (markitdown OCR if Gemma down)

internal/agui/emitter.go (diff)  # ~+50  fanout channel API per in-process subscribers
                                 #       (oltre all'HTTP SSE di Slice 8b, deferito #35)

internal/llm/openai_compat/models.go (diff)  # ~+30  SupportsVision/SupportsAudio capability flags
                                              #       per model

cmd/aura/main.go (diff)         # ~+120  subcommand routing:
                                 #         aura serve [--no-telegram] [--only=cli|telegram]
                                 #         aura telegram allow/list/revoke (admin CLI dev-only)

internal/db/queries/telegram_accounts.sql       # ~50   6 query sqlc
internal/db/queries/telegram_setup_pending.sql  # ~30   3 query sqlc

internal/db/migrations/0008_telegram.up.sql     # ~60
internal/db/migrations/0008_telegram.down.sql   # ~5
```

### Migration 0008 (Slice 9a)

```sql
CREATE TABLE aura.telegram_accounts (
  telegram_user_id bigint PRIMARY KEY,
  identity_id      uuid NOT NULL REFERENCES aura.identities(id),
  username         text,
  first_name       text,
  added_at         timestamptz NOT NULL DEFAULT now(),
  last_seen_at     timestamptz
);

CREATE TABLE aura.telegram_setup_pending (
  onboarding_token text PRIMARY KEY,
  identity_id      uuid NOT NULL REFERENCES aura.identities(id),
  generated_by     text,                     -- bot username when generated via /setup
  created_at       timestamptz NOT NULL DEFAULT now(),
  expires_at       timestamptz NOT NULL,     -- created_at + 1 hour
  consumed_at      timestamptz NULL
);
CREATE INDEX telegram_setup_pending_active
  ON aura.telegram_setup_pending (expires_at)
  WHERE consumed_at IS NULL;
```

### Sidecar compose.yaml changes (Slice 9c)

```yaml
# RIMOSSO: aura-whisper (sostituito da Gemma 4 E4B audio nativo)

# AGGIUNTO: aura-llama-multimodal
aura-llama-multimodal:
  image: ghcr.io/ggml-org/llama.cpp:${LLAMA_MULTIMODAL_IMAGE_TAG:-server}
  command:
    - -m /models/gemma-4-e4b-it-Q4_0.gguf       # variant default baseline
    - --mmproj /models/gemma-4-e4b-mmproj-Q4_0.gguf
    - --port 8082
    - --ctx-size 4096
    - --threads 4
  volumes:
    - aura-models:/models
  ports:
    - "127.0.0.1:${LLAMA_MULTIMODAL_HOST_PORT:-8082}:8082"
  healthcheck:
    test: ["CMD", "curl", "-f", "http://localhost:8082/health"]
```

Env propagati al container Aura:
```
MULTIMODAL_BASE_URL=http://aura-llama-multimodal:8082/v1
MULTIMODAL_MODEL=gemma-4-e4b
MULTIMODAL_API_KEY=no-key
```

### Smoke

**9a smoke:**
```bash
./aura serve --no-telegram                # niente bot, solo /setup endpoint attivo
curl http://127.0.0.1:9081/setup | grep "Aura Setup"
# admin apre browser, paste bot token → bot validato via getMe
# admin click "Add user" → genera onboarding token + QR
# QR scan → t.me/MyAuraBot?start=ONBOARD-XYZ
```

**9b smoke:**
```bash
TELEGRAM_BOT_TOKEN=xxx ./aura serve &
# admin/setup completato (vedi 9a)
# user invia "/start" al bot
# bot risponde "👋 Benvenuto in Aura. Usa /help per comandi."
# user invia "ciao dimmi 2+2"
# bot mostra status pane "⏳ thinking..." → "✅ done" e content "4"
```

**9c smoke:**
```bash
# user invia voice "ciao dimmi 2+2"
# bot reaction 🎤 → transcription via Gemma 4 → "ciao dimmi 2+2" → process come testo

# user invia photo (snapshot di un'equazione)
# bot reaction 🖼 → description via Gemma 4 → user message text → process

# user invia PDF di 3 MB
# bot reaction 📄 → markitdown convert sync → markdown text → process
```

### Acceptance

#### 9a (Channel framework + Setup wizard)
- [ ] `Channel` interface in `internal/channels/channel.go`. CLI implementata come `internal/channels/cli/cli.go` (refactor di Slice 1.8 `cmd/aura/chat.go` — file creato in 1.8 dedicato per i sub-command chat, ora migrato a channel abstraction).
- [ ] `Registry` orchestration: `StartAll` boot tutti i channel enabled (env `AURA_CHANNEL_<NAME>_ENABLED`, default true); `StopAll` graceful drain.
- [ ] Flag override per debug: `aura serve --no-telegram`, `aura serve --only=cli`.
- [ ] Setup wizard HTTP server bind `127.0.0.1:9081` (override `AURA_SETUP_BIND=0.0.0.0:9081` per setup remote con QR scan).
- [ ] `GET /setup` serve `page.html` embedded.
- [ ] **One-time token gate (amendment #10)**: `AURA_SETUP_TOKEN` env var. If unset at boot, `aura serve` generates a random UUIDv4 and prints `AURA_SETUP_TOKEN=<value>` to stdout (single line, parseable). Token persists in memory only (no disk). Middleware `requireSetupToken` on `/setup/*` returns 401 if `?token=` query param or `X-Aura-Setup-Token` header does not match. After successful Telegram onboarding (POST /setup/onboard-link completion event), the token is invalidated and a second navigation returns 401. Acceptance test: 401 without token, 200 with token, 401 after onboarding-complete.
- [ ] `POST /setup/token` body `{token}`: valida via Telegram `getMe`, persiste in secrets store, restart bot goroutine (se già attivo).
- [ ] `POST /setup/onboard-link`: genera UUID onboarding_token, INSERT `telegram_setup_pending` (TTL 1h), ritorna `{deep_link: "https://t.me/<bot_username>?start=<token>", qr_svg: "..."}`.
- [ ] `GET /setup/events` (SSE): emette `{type:"onboarding_completed", telegram_user_id, username}` quando `telegram_setup_pending.consumed_at` viene scritto. **Implementazione: poll DB ogni 2s** (default proposto chiuso per Slice 9a, vedi Open Questions sotto). LISTEN/NOTIFY è ottimizzazione futura, non bloccante.
- [ ] `GET /setup/status`: ritorna `{bot_configured, account_count, last_activity}`.
- [ ] Smoke: setup flow end-to-end senza CLI (paste token → QR → Telegram → completo).
- [ ] Test integrazione `db_integration`: round-trip onboarding token + cleanup expired.

#### 9b (Telegram impl)
- [ ] `internal/channels/telegram/bot.go` implementa `Channel` interface, polling `tele.Bot` via telebot.v4.
- [ ] `agui_subscriber.go` riceve eventi da `agui.Emitter.Subscribe()` (fanout channel, in-process).
- [ ] `renderer.go`: 2 msg per turn (status + content), throttle differenziato + chat queue serializzata + 429 backoff. Markdown via in-tree custom MarkdownV2 escaper `internal/channels/telegram/mdv2.go` (~80 LOC, per amendment #4 — promotes the port to default, eliminates `eekstunt/telegramify-markdown-go` dep). Acceptance: fuzz 10K random Unicode inputs → every output round-trips through Telegram `sendMessage` without `400 Bad Request: can't parse entities` (Pitfall #18 mitigation). Fallback plain text if escaping fails.
- [ ] `hitl.go`: `ask_user.options` → InlineKeyboardMarkup; assenti → ForceReply. Callback handler chiama `Resume(token, answer)`. Reply quote = new turn parallelo (multi-pause FIFO).
- [ ] `commands.go`: 10 commands MVP (incl. `/cost` + `/search`), bot-intercept, no LLM call per dispatching.
- [ ] **`/cost` command (amendment #8)**: returns today's cumulative USD spend (`aura.cost_aggregator.TodayUSD()`) + per-conversation breakdown top 5. Uses `aura chat cost` shared logic in `cmd/aura/cost.go` (~30 LOC, single source). Telegram-side: bot-intercept, no LLM call.
- [ ] **`/search` command (amendments #7+#8 cross-ref)**: bot-intercept calling `internal/conversations/search.Search(ctx, query)` from Slice 1.8.5. Result: top 5 turn excerpts with conversation links.
- [ ] `onboarding.go`: `/start <token>` matcher → consume `telegram_setup_pending` → INSERT `telegram_accounts` + send SSE event a /setup web.
- [ ] `renderer.go` mappa esplicitamente i seguenti AG-UI event types (definiti in Slice 8): `RUN_STARTED` (open status pane), `STEP_STARTED`/`STEP_FINISHED` (status pane update), `TEXT_MESSAGE_START`/`CONTENT`/`END` (content streaming), `TOOL_CALL_START`/`ARGS`/`END`/`RESULT` (status pane tool list con glyph 🟡→✅/❌), `REASONING_START`/`CONTENT`/`END` (status pane "💭 Reasoning..." line, collapsed se troppo lungo), `STATE_DELTA` (running cost USD su status pane footer), `STATE_SNAPSHOT` (no-op nel rendering, solo audit), `MESSAGES_SNAPSHOT` (no-op, solo HTTP responses), `RUN_FINISHED` (status pane finalize + content reply send), `RUN_ERROR` (status pane shows ❌ + error message).
- [ ] **Microcompact pointer handling**: il renderer riconosce `tool_call_result` content che inizia con `[tool_call_id=X: evicted from context...]` (formato L1 microcompact eviction Slice 1.8b) e lo rende come line "🗄️ Tool result evicted (X bytes archived)" invece di dump del puntatore raw nel Telegram message.
- [ ] Test renderer: golden fixture per ogni event type AG-UI → Telegram message expected. Almeno 1 fixture per type, incluso microcompact pointer case.
- [ ] Test commands: ogni command produce output atteso senza LLM call.

#### 9c (Multimodal)
- [ ] `voice.go`: POST a Gemma 4 multimodal sidecar `/v1/audio/transcriptions`. 2 retry + hard fail messaggio UX.
- [ ] `documents.go`: tiered sync/async via markitdown sidecar. ≤5 MB sync, 5-50 MB async background. Output > `AURA_CONVERSATION_TURN_CAP_BYTES` → sidecar.
- [ ] `photo.go`: POST a Gemma 4 multimodal sidecar `/v1/chat/completions` con base64 image_url. Description text → user message AG-UI.
- [ ] Test integration `multimodal_integration` build tag: requires sidecar up. Skipped in CI senza container.
- [ ] **Open question pre-merge**: benchmark Gemma 4 E2B vs E4B vs 26B MoE su corpus reale Aura per:
  - STT accuracy (WER su 20 audio sample IT/EN)
  - Image description quality (manual rating su 10 sample)
  - Latenza p50/p95
  - RAM steady state
  Default baseline E4B Q4 fino al benchmark.
- [ ] **Open question pre-merge**: vision fallback strategy. Se Gemma quality < threshold → markitdown OCR fallback path attivato. Altrimenti rimosso.

### File targets cumulativi

(Vedere sezione "Architettura componenti" sopra. Totale ~2330 LOC src + 750 test.)

### Mini-PC RAM budget — delta vs pre-Slice 9

- **Rimosso** `aura-whisper` (Whisper.cpp server): -300 MB
- **Aggiunto** `aura-llama-multimodal` (Gemma 4 E4B Q4 default): +3 GB
- Net: +2.7 GB. Su mini-PC 32 GB rimane abbondante headroom.

Slice 0.5 RAM table emendata per riflettere (-Whisper +Gemma 4 multimodal).

### Open questions

1. **Variant Gemma 4 finale**: E2B / E4B / 26B MoE / 31B. Decisione pre-merge Slice 9c dopo benchmark accuracy + latenza + RAM su corpus reale Aura. Default baseline E4B.
2. **Vision fallback markitdown OCR**: necessario o no. Pre-merge benchmark decide.
3. **Setup wizard PostgreSQL LISTEN/NOTIFY vs poll**: SSE `/setup/events` può usare LISTEN/NOTIFY (efficient, ~1 connection ws) o poll DB ogni 2s (semplice, no extra deps). → *Default proposto*: poll 2s per Slice 9a, LISTEN/NOTIFY come ottimizzazione futura.
4. **Channel framework: WhatsApp Business API / Discord / Signal future slice**: ogni new channel = nuova sub-slice in `internal/channels/<name>/`. Schema `<name>_accounts` table parallela. Non in scope Slice 9.

### Commit message templates (sub-slice 9a + 9b + 9c)

```
slice 9a: channels framework + setup wizard (QR/deep-link onboarding)

internal/channels/channel.go interface + registry orchestration.
CLI refactored from Slice 1.8 cmd/aura/chat.go to internal/channels/cli/.
Setup wizard HTTP server on 127.0.0.1:9081 (always on): paste Telegram
bot token (validated via getMe), POST /setup/onboard-link generates
UUID + QR SVG of t.me/<bot>?start=<token>. SSE /setup/events emits
'onboarding_completed' when telegram_setup_pending.consumed_at set.

Migration 0008_telegram: aura.telegram_accounts (FK identities) +
aura.telegram_setup_pending (1h TTL, partial index on active).

CLI `aura serve [--no-telegram] [--only=cli]` flag overrides.

LOC: +XXX src / +YY test / +ZZ migration.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

```
slice 9b: telegram bot (in-process AG-UI subscriber + commands)

internal/channels/telegram/* implements Channel interface as in-process
goroutine of aura serve. Subscribes to internal/agui/emitter fanout
channel (no HTTP overhead vs external SSE). Pattern master B: 2 msg
per turn (status pane + content reply), throttle differenziato (status
1500ms / content 500ms) + chat queue 1000ms serializzata + 429 backoff.
Markdown via in-tree custom MarkdownV2 escaper (per amendment #4, no external dep).

HITL: ask_user.options -> InlineKeyboardMarkup; assenti -> ForceReply.
Reply quote a pending = nuovo turn parallelo (multi-pause FIFO).

8 commands MVP bot-intercept (/start /help /whoami /cancel /new
/conversations /resume /reset). Onboarding /start <token> consume
telegram_setup_pending.

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

```
slice 9c: telegram multimodal (voice/photo via Gemma 4, docs via markitdown)

aura-whisper sidecar RIMOSSO da compose.yaml. aura-llama-multimodal
sidecar AGGIUNTO (Gemma 4 E4B Q4 baseline, variant TBD post-benchmark).
Unifica STT + vision (audio + image nativi in Gemma 4 E2B/E4B).
Net RAM: -300 MB + 3 GB = +2.7 GB su mini-PC 32 GB.

voice.go: POST sidecar /v1/audio/transcriptions, 2 retry + hard fail.
photo.go: POST sidecar /v1/chat/completions con image_url base64.
documents.go: tiered sync/async via markitdown sidecar
(<=5MB sync, 5-50MB async, >50MB refuse). Output > conversation_turn
cap -> sidecar (Slice 1.8 pattern).

Open questions pre-merge:
- Benchmark variant E2B/E4B/26B per STT accuracy + image description
  + latenza + RAM su corpus reale Aura.
- Vision fallback markitdown OCR: necessario o no post-benchmark.

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Slice 10 — User onboarding + `Agent.md` profile (per identity)

> **Slice 0.9 amendment**: l'interview state machine in `interview.go` (~180 LOC custom Go) viene rimpiazzata da `LoopAgent[InterviewStepAgent]` (Slice 0.9 built-in). Ogni step = un `InterviewStepAgent` (~40 LOC ciascuno) che genera la prossima domanda LLM-adattiva. Termination via `event.Actions.Escalate=true` emesso da `SummaryConfirmAgent` su "Conferma". `maxIterations=8` cap hard. Saving stimato: **−120 LOC** (no state machine custom, riusa LoopAgent runtime + escalation).

**Goal.** Aura conosce l'utente. Al primo Telegram message post-setup (Slice 9), il bot avvia un **LLM-driven interview free-form** (5-8 domande adattive) e genera un file `Agent.md` per quella identity. Il file viene iniettato come secondo system message nei prompt successivi, dando ad Aura context persistente su nome, lingua, tone, interessi, boundaries.

Pattern derivato da:
- **ChatGPT Custom Instructions** ("what to know about you" + "how to respond")
- **ChatGPT Memory** (estrazione fatti continua, ADD-only, mem0-style)
- **CLAUDE.md** (file markdown leggibile, iniettato come context)
- **AGENTS.md** standard (Linux Foundation, ma adattato a user preferences invece di coding project)

Per-identity (Slice 1.7): multi-user supportato strutturalmente. Filesystem-based per ispezionabilità + git-friendly + edit manuale possibile.

### Pre-requisiti

- Slice 1.5 ask_user (per approval gate degli updates)
- Slice 1.7 identities (per per-identity scoping)
- Slice 1.8 conversation persistence (per memory extraction da turn passati)
- Slice 4 PromptBuilder (per injection cache-friendly come secondo system message)
- Risk-Based Governance (sezione cross-cutting, per gating delle update uncertain)
- Slice 8 AG-UI emitter (per emit STATE_DELTA quando profile cambia)
- Slice 9 Telegram bot (transport principale dell'interview e degli updates)

### Decisioni cumulate (chiusura discussione 2026-05-28)

| Aspetto | Decisione |
|---|---|
| Filename | **`Agent.md`** (custom Aura, non AGENTS.md/CLAUDE.md). User-facing, non coding-oriented. |
| Trigger | **Auto al primo Telegram message** post-setup (no esistente `Agent.md` per quella identity). Skip option `[Salta, dopo]` disponibile, `/onboard` command per recovery. |
| Storage | **Filesystem** `~/.aura/agents/<identity_id>/Agent.md`. Git-friendly, editable manualmente, ispezionabile. Plus `preferences.json` (structured) + `metadata.json` (version, timestamps) + `changelog.md` (audit). |
| Interview style | **LLM Q&A free-form**. Bot chiede domande adattive (l'agente decide quale chiedere dopo in base alle risposte precedenti), utente risponde libero, LLM interpreta. |
| Auto-update | **Hybrid**: fatti **certi** (osservati `N≥AURA_PROFILE_CERTAINTY_N` volte consistenti, default 3) → auto-add silenzioso + changelog entry. Fatti **incerti** (1-2 osservazioni, conflicting, nuova categoria) → ask_user approval gate via Risk-Based pipeline (tier RISKY per modifiche preferences). |
| Auto-revert | `/forget <fact>` command rimuove specific fact + changelog REVERT entry. Idempotente. |
| Injection nel prompt | Agent.md content come **secondo system message** (dopo main Aura system prompt). Prefix cache-friendly (Slice 4): cambia raramente, sotto il main system. |

### File layout

```
~/.aura/agents/<identity_id>/
  Agent.md              # personalizzazione utente, markdown leggibile
  preferences.json      # structured: lang, timezone, voice_mode, can_proactive_message,
                        # tone_preference (formale/informale/tecnico), response_length
  metadata.json         # version (schema rev), generated_at, last_updated_at,
                        # onboarding_completed (bool), observation_counts (per-category)
  changelog.md          # append-only log delle modifiche (timestamp, fatto added/removed,
                        # source: onboarding|auto-extract|manual|forget)
```

### Esempio output `Agent.md` (post-onboarding)

```markdown
# Agent profile for Davide (local)

Generated 2026-05-28T14:32Z via onboarding interview.
Last updated 2026-05-28T16:08Z (auto-extract: "lingua=italiano" confirmed).

## About me

- Nome: Davide
- Occupazione: Software engineer, AI/agents
- Fuso orario: Europe/Rome (UTC+1)
- Lingua: italiano (auto-detect inglese se scrivo in EN)

## How I prefer responses

- Tono: informale, diretto
- Niente: "Spero questo aiuti!", "Certo!", preamboli
- Lunghezza: sintetica
- Code blocks: con linguaggio specificato
- Italiano: tu, non lei

## Areas of interest

- Coding agentic (Aura, Claude Code, agent frameworks)
- AI/ML systems design
- Backend Go + databases

## Boundaries

- NON proporre cron irreversibili senza approvazione esplicita
- NON salvare info personali in memoria senza chiedere
- OK proactive notification: news tech daily 09:00, reminder calendar

---
<!-- This file is auto-generated and updated by Aura. Edit freely;
     Aura preserves manual edits and only adds new facts via
     /onboard interview, /edit-profile, or auto-extract Risk-Based gate. -->
```

### Onboarding flow (LLM-driven, Telegram)

```
Trigger: primo Telegram message da nuovo telegram_account post-setup
         AND ~/.aura/agents/<identity_id>/Agent.md non esiste

Bot: 👋 Ciao! Sono Aura. Posso conoscerti meglio prima di iniziare?
     [✅ Sì, 5 minuti] [⏭️ Salta, dopo]

Se "Salta":
  → metadata.json onboarding_completed=false
  → Agent.md vuoto creato con placeholder
  → /onboard command disponibile per rifare interview
  → procedi a normal chat (no profile injection)

Se "Sì":
  → InterviewLoop = LoopAgent[InterviewStepAgent] (Slice 0.9 built-in)
  → maxIterations=8 cap hard, ogni iter = 1 step domanda+risposta
  → 5-8 domande adattive scelte dall'LLM in base alle risposte
  
  Domanda 1 (sempre): "Come ti chiami?"
  Domanda 2 (sempre): "Che lingua preferisci che parli? IT / EN / auto?"
  Domanda 3-6 (LLM-adaptive based on previous answers):
    es. "Qual è la tua occupazione?" → se "developer", chiede "che stack?"
    es. "Hai preferenze sul tono? (formale, informale, tecnico)"
    es. "Aree di interesse principali? (es. coding, news tech, finanza, salute...)"
    es. "C'è qualcosa di importante che dovrei sapere su di te?"
  Domanda 7 (sempre): "Posso scriverti spontaneamente con reminder/news?
                       [✅ Sì] [⏰ Solo reminder] [❌ No]"
  Domanda 8 (sempre, riassunto): bot mostra Agent.md generato:
    "Ecco cosa ho capito:
     [...content...]
     È corretto? Vuoi correzioni o aggiungere qualcosa?
     [✅ Conferma] [✏️ Modifica] [🔄 Rifai]"

  Se "Modifica" → bot chiede free-text "Cosa correggo?" → applica diff
  Se "Conferma" → Agent.md salvato + metadata.onboarding_completed=true
  Se "Rifai" → torna a domanda 1

  → bot risponde: "✅ Perfetto, Davide. Sono pronto. Cosa posso fare per te?"
  → procede a normal chat con Agent.md injected
```

### Auto-update flow (mem0-style hybrid)

Durante ogni turn user, Aura osserva il messaggio e tenta extraction di fatti rilevanti per il profile:

```
LoopTurn observer (post-LLM response):
  facts = extractor.Extract(user_message, agent_response)
    # LLM-driven extraction: returns []FactCandidate
    # FactCandidate{ category, key, value, confidence_0_1, evidence_quote }
  
  for each fact:
    counter = metadata.observation_counts[fact.category][fact.key]
    counter++

    if counter >= AURA_PROFILE_CERTAINTY_N (default 3):
      if fact already in Agent.md:
        # Already known, no action
      else:
        # CERTAIN new fact → auto-add
        Agent.md → append/update relevant section
        changelog.md → append "AUTO_ADD: <fact> (source: <evidence>, count: <N>)"
        metadata.last_updated_at = now()
        # No user prompt, silent
    
    elif counter < AURA_PROFILE_CERTAINTY_N AND fact.confidence > 0.7:
      # UNCERTAIN but high-confidence → approval gate
      compute risk_tier (Slice 5): typically RISKY for new preferences
      if tier == RISKY:
        emit ask_user(
          kind=approval,
          question="Ho notato che: {fact}. Devo salvarlo nel tuo profilo?",
          options=["✅ Sì", "❌ No, ignora", "✏️ Modifica"]
        )
        # User decides; ResumeContext applies update if approved
    
    else:
      # Low confidence or low count: keep tracking, no action
```

`/forget <fact>` command:
- User: `/forget` o `/forget italiano`
- `/forget` senza args: bot mostra lista facts recenti, user seleziona via inline keyboard
- `/forget <text>` con args: fuzzy match nei facts, mostra candidate + conferma
- On confirm: rimuove dal Agent.md + append "FORGET: <fact>" in changelog

### Injection nel prompt (cache-friendly)

```
Prompt structure (Slice 4 PromptBuilder + Slice 10):

System message 1 (stable across turns, all conversations):
  "You are Aura, a personal AI assistant..."
  [main Aura system prompt + tool manifest]

System message 2 (stable across turns, per-identity):
  [Agent.md content for current identity]

User/assistant/tool message history (variable)
```

`messages[0]` byte-identico turn-su-turn (Slice 4 invariant rispettato): main system non cambia.
`messages[1]` (Agent.md) byte-identico finché profile non viene updated. Cache hit drop solo on update (raro).

### Architettura componenti

```
internal/onboarding/
  interview.go      # ~80    InterviewLoop = LoopAgent[InterviewStepAgent]
                    #        (Slice 0.9 built-in, NO state machine custom)
                    #        - maxIterations=8 (cap hard, allineato a Slice 0.9)
                    #        - InterviewStepAgent.Run yield Event con domanda LLM
                    #          adattiva, attende risposta via ask_user, yield Event
                    #          con risposta accumulata in InvocationContext state
                    #        - SummaryConfirmAgent (sub-agent) emette
                    #          Event{Actions.Escalate=true} su "Conferma" → LoopAgent
                    #          termina naturalmente (saving -100 LOC vs state machine)
  steps.go          # ~120   InterviewStepAgent + SummaryConfirmAgent
                    #        impl di agent.Agent (Slice 0.9)
                    #        Domande adattive: l'LLM sceglie la prossima in base
                    #        al state accumulato (InvocationContext.SessionStore)
  store.go          # ~100   Filesystem read/write ~/.aura/agents/<id>/
                    #        - Agent.md, preferences.json, metadata.json, changelog.md
                    #        - atomic write (temp file + os.Rename)
  injector.go       # ~80    PromptBuilder hook: inject Agent.md come second
                    #        system message. Cache key invalidation su update.
  extractor.go      # ~150   LLM-driven fact extraction da conversation turn
                    #        - FactCandidate{category, key, value, confidence, evidence}
                    #        - prompt template per estrazione strutturata
  updater.go        # ~200   Auto-update logic:
                    #        - observation counter per category/key
                    #        - certainty threshold check (default N=3)
                    #        - certain → auto-add silenzioso + changelog + profile_audit row
                    #        - uncertain hi-conf → ask_user gate (Risk-Based) +
                    #          paused_state_token persisted, audit row scritto su resume
                    #        - low-conf → no action

internal/channels/telegram/commands.go (diff)  # ~+80
  + /onboard  - re-run interview (spawn nuovo LoopAgent[InterviewStepAgent])
  + /edit-profile - opens edit mode (textarea via ForceReply per section)
  + /forget [fact] - delete fact + changelog + profile_audit FORGET row
                    routing: bot-intercept con inline keyboard confirmation,
                    NON paused_states (è command, non Loop-emitted ask_user).
                    Idempotente: rimuovere fatto già rimosso = no-op + log.
  + /profile - mostra Agent.md current

internal/agent/llm_agent.go (diff)  # ~+50
  - hook onboarding.Updater.Observe(turn) post-LLM response (consume yield events)
  - hook onboarding.Injector.Inject() in PromptBuilder

internal/db/queries/profile_audit.sql            # ~40
  - 4 query: RecordProfileMutation, ListAuditSince, GetByIdentity, ListPendingApproval
  - per-identity, parity con skill_audit (Slice 7c) per simmetria governance

internal/db/migrations/0009_profile_audit.up.sql # ~50
  - CREATE TABLE aura.profile_audit (id, ts, identity_id fk, action,
    category, key, value_before, value_after, content_hash, source
    enum {onboarding,auto-extract,manual,forget},
    approval_source enum {ask_user,cli,auto},
    paused_state_token fk paused_states(token) NULL,
    computed_risk_tier, gate_recommended, gate_taken)
  - Forensics asimmetrico risolto (audit Round 1 P1)

internal/db/migrations/0009_profile_audit.down.sql # ~3

cmd/aura/main.go (diff)  # ~+40
  + aura profile show <identity_name>
  + aura profile edit <identity_name>  (opens $EDITOR su Agent.md)
  + aura profile reset <identity_name>  (--confirm required)
```

### Smoke

```bash
# Setup completato (Slice 9), telegram_accounts.user_id=12345 → identity=local

# Primo message da utente nuovo
# (Telegram client) "Ciao"
# (bot) "👋 Ciao! Sono Aura. Posso conoscerti meglio prima di iniziare?
#        [✅ Sì, 5 minuti] [⏭️ Salta, dopo]"

# user tap "Sì"
# (bot) "Come ti chiami?"
# (user) "Davide"
# (bot) "Piacere Davide. Che lingua preferisci? IT / EN / auto?"
# ... 5-8 domande ...
# (bot) "Ecco cosa ho capito: [...content Agent.md...]. È corretto?
#        [✅ Conferma] [✏️ Modifica] [🔄 Rifai]"

# user tap "Conferma"
# (bot) "✅ Perfetto, Davide. Sono pronto. Cosa posso fare per te?"

ls ~/.aura/agents/<identity_id>/
# Agent.md preferences.json metadata.json changelog.md

cat ~/.aura/agents/<identity_id>/Agent.md
# # Agent profile for Davide (local)
# ...
```

### Acceptance

- [ ] `internal/onboarding/interview.go` definisce `InterviewLoop = LoopAgent[InterviewStepAgent]` (Slice 0.9 built-in). `maxIterations=8` cap hard. Skip option gestita pre-spawn. `SummaryConfirmAgent` emette `Event{Actions.Escalate=true}` su "Conferma" → LoopAgent termina naturalmente. No state machine Go custom.
- [ ] `store.go` filesystem atomic write (temp + Rename), per-identity directory `~/.aura/agents/<identity_id>/` (UUID).
- [ ] `injector.go` aggiunge Agent.md come second system message in PromptBuilder. Cache invalidation hash basato su `metadata.last_updated_at`.
- [ ] `extractor.go` LLM-driven fact extraction post-turn. Prompt template strutturato che restituisce JSON `[{category, key, value, confidence, evidence}]`.
- [ ] `updater.go` hybrid auto-update: counter ≥ `AURA_PROFILE_CERTAINTY_N` (default 3) → silent add, altrimenti ask_user via Risk-Based pipeline.
- [ ] `/onboard` command rifa interview (skip already-completed con conferma "vuoi sovrascrivere?").
- [ ] `/edit-profile` apre edit mode (ForceReply per section: about-me, preferences, areas, boundaries).
- [ ] `/forget [fact]` rimuove fact + changelog REVERT entry. Senza args → lista facts paginata.
- [ ] `/profile` mostra Agent.md current con markdown rendering.
- [ ] Trigger auto: primo Telegram message senza Agent.md esistente per identity → start interview.
- [ ] CLI `aura profile show/edit/reset <identity_name>` per debug/admin.
- [ ] Test integration `onboarding_integration` build tag: interview round-trip end-to-end.
- [ ] Test fact extraction: 5 conversation turn esempio → fatti attesi extracted.
- [ ] Test hybrid update: 3 osservazioni consistenti → auto-add. 2 osservazioni + 1 conflicting → ask_user gate.

### File targets cumulativi

(Vedere "Architettura componenti" sopra. Totale ~580 LOC src + ~250 test + 50 LOC migration — saving −120 LOC riapplicato da Slice 0.9 amendment via `LoopAgent[InterviewStepAgent]` vs state machine Go custom.)

### Open questions

1. **AURA_PROFILE_CERTAINTY_N**: 3 è sweet spot? Pre-merge calibrare su corpus di test conversations. Default 3, env override.
2. **Conflicting facts handling**: se osservato 2 volte "lingua=IT" + 1 volta "lingua=EN" → cosa fare? *Default proposto*: keep current (no override automatic), log osservazione conflicting in changelog per analisi. Se 3+ conflict → ask_user explicit.
3. **Schema versioning Agent.md**: come gestire breaking change al template (es. nuova section)? *Default proposto*: `metadata.json.schema_version` int, migration script tra versions in `internal/onboarding/migrations/`.
4. **Privacy / forget compliance**: `/forget --all` per GDPR right-to-delete su single user → cancella `~/.aura/agents/<identity_id>/` + persists deletion log per audit. Out of scope qui, future slice multi-user/auth.

### Mini-PC RAM budget — delta vs pre-Slice 10

Negligibile. Slice 10 è puro filesystem + LLM call extraction (riusa LLM Slice 1). Nessun nuovo sidecar.

### Commit message template

```
slice 10: User onboarding + Agent.md profile (per identity)

Aura conosce l'utente. Primo Telegram message post-setup -> LLM-driven
free-form interview (5-8 domande adattive) -> Agent.md persisted per
identity in ~/.aura/agents/<identity_id>/Agent.md (filesystem,
git-friendly, manualmente editable).

Pattern derivato da ChatGPT Custom Instructions + Memory, CLAUDE.md
file injection, mem0-style ADD-only extraction.

Decisioni cumulate:
- Filename Agent.md (custom Aura, user-facing non coding-oriented)
- Trigger auto al primo Telegram message post-setup
- Storage filesystem per-identity (Slice 1.7 multi-user ready)
- Interview LLM Q&A free-form adattivo (no rigid form)
- Auto-update hybrid: fatti certi (N>=3 consistent) auto-add silenzioso
  + changelog; fatti incerti -> ask_user approval gate via Risk-Based
  (Slice 5 pipeline)
- Injection cache-friendly: secondo system message dopo main Aura
  (Slice 4 invariants preservati: messages[0] byte-identico)

File layout per identity:
  Agent.md          - markdown profile leggibile
  preferences.json  - structured (lang, tz, voice_mode, can_proactive)
  metadata.json     - version, timestamps, observation_counts
  changelog.md      - append-only audit log

Commands Telegram nuovi:
  /onboard       - re-run interview
  /edit-profile  - edit mode via ForceReply per section
  /forget [fact] - revoca fact + changelog REVERT
  /profile       - show current Agent.md

CLI debug:
  aura profile show/edit/reset <identity_name>

Hook al Loop:
- post-LLM response: Updater.Observe(turn) per fact extraction continuo
- pre-prompt: Injector.Inject() aggiunge Agent.md a system messages

Threshold env:
  AURA_PROFILE_CERTAINTY_N=3  (consecutive observations per auto-add)

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Slice 11 — Memory ingestion + taxonomy (Documents + Entities + Graph + Agent journal)

> **Pattern rubato** dai 4 sistemi memory production-grade 2026 (mix-and-match): **mem0** (48k stars, 91% latency / 90% token saving): 2-fase pipeline extract+conflict, hybrid retrieval. **Letta MemGPT**: 3-tier storage Core/Recall/Archival. **Microsoft GraphRAG**: entity+relationship extraction LLM + Leiden community clustering hierarchical. **Cognee**: Cognify pipeline 6-stage + Memify post-processing (prune/strengthen/derive). Sources: [mem0 state 2026](https://mem0.ai/blog/state-of-ai-agent-memory-2026), [Letta docs](https://docs.letta.com/concepts/letta/), [GraphRAG guide 2026](https://blog.premai.io/graphrag-implementation-guide-entity-extraction-query-routing-when-it-beats-vector-rag-2026/), [Cognee architecture](https://www.cognee.ai/blog/fundamentals/how-cognee-builds-ai-memory).

> **Atomicity: 4 sub-slice archival MVP (11a/11b/11d/11e) ~1850 LOC**, ognuno atomic + smoke green. **11c — Community detection è DEFERITA fuori dall'MVP Phase 15 (amendment #27)** — feature lazy/on-demand futura, non parte della sequenza atomica: GDS resta installato (Slice 0.7) per algoritmi ad-hoc (PageRank/WCC/Node-Similarity), ma il job schedulato Leiden + le community-summary LLM sono rimandati (vedi §11c sotto). **+ 11f Task Canvas (~400 LOC, amendment #25) è sequencing-indipendente** — non parte della sequenza atomica di Phase 15: può atterrare a ~Phase 9-10 (dipende solo da context management / Phase 6, non da ingestion/retrieval).

**Goal.** Aura **conosce** (ingest + index) e **ricorda** (retrieve) — sia il world dell'utente (documenti, entità, conversazioni passate) sia il proprio (agent journal, insight cross-conv). Pipeline MVP: file/URL/conversation → markitdown (Slice 9c) → chunker → embedder (Slice 0.7 sidecar) → Neo4j HNSW + entity LLM extraction → retrieval hybrid + LLM re-ranker. (Leiden community detection + summarization è la sub-slice 11c, DEFERITA fuori dall'MVP — amendment #27.)

### Pre-requisiti

- Slice 0.7 Neo4j infra + HNSW + `aura-llama-embed` sidecar
- Slice 0.9 Agent runtime abstraction (per background goroutine come `BackgroundAgent`)
- Slice 1.7 identity (scope, oggi single-user `local`)
- Slice 1.8 conversation persistence (rollup `:UserConversation`)
- Slice 5 web tools (per `ingest.url` riusa `web_fetch`)
- Slice 7e snippet (mirror in Neo4j `:UserSnippet` per semantic search)
- Slice 9c markitdown sidecar (document → markdown universal parser)
- Slice 10 Agent.md (Core tier già implementato)

### Decisioni cumulate (chiusura 2026-05-28)

| Aspetto | Decisione |
|---|---|
| Memory taxonomy cognitiva | **3 tipi**: Episodic (past events temporal) + Semantic (entities/relations) + Procedural (behaviors/scripts). Industry consensus 2026. |
| Storage tier (Letta-style) | **3 tier**: Core (in-context, Agent.md + top-K AgentInsight, ~2K token) + Recall (Postgres `conversation_turns` searchable) + Archival (Neo4j full graph, retrieved via tool). |
| Entity extraction | **Auto-extract sempre on** durante ingestion. LLM tier=reasoning batch ogni 10 chunk. Type taxonomy **POLE+O (amendment #29)**: `type` chiuso Person/Org/Location/Concept/Event/Topic/**Other** + proprietà `subtype` free-form. Cost ~$0.05/documento medio. |
| Retrieval | **Hybrid BM25 + HNSW + graph 1-hop expansion, fuse via Weighted-RRF Cypher-native (amendment #26, rank-based scale-free), poi LLM re-ranker tier=worker**. Re-rank top-20 → top-5. Max quality, +LLM cost ~$0.001/query. (Fusione lineare pesata = fallback deprecato.) |
| Privacy isolation | **No isolation single-user mode**: tutti node sotto identity `'local'` default. Future multi-user richiede refactor (accettato pre-merge). |
| Community detection | **DEFERITA fuori MVP (amendment #27, 11c)** — il job schedulato Leiden + community-summary LLM è over-engineering per il target single-user/self-hosted/cost-sensitive (le summary tematiche globali rispondono a una domanda di corpus-analytics che il single-user raramente pone; ogni community richiede una summary LLM rigenerata al drift del grafo; Microsoft stessa è passata a LazyGraphRAG). Diventa una feature lazy/on-demand futura. **GDS resta installato** (Slice 0.7) per PageRank/WCC/Node-Similarity ad-hoc; solo lo `gds.leiden.stream` schedulato + summary sono rimandati. Il valore cognitivo (local retrieval) è pienamente coperto da 11d (hybrid WRRF + graph 1-2 hop) senza community. |
| Memify post-processing | **Cognee pattern**: soft-archive stale entity (no mention 180gg → `archived_at`, NO hard delete — amendment #29) + strengthen RELATED_TO weight su co-occurrence + derive facts da multi-hop traversal. Background 24h. |
| Agent memory | **Episodic + Insight + cached injection (amendment #11)**: `:AgentEpisode` post-conv summary + `:AgentInsight` cross-conv pattern. Pre-conv inject top-K Insight relevant nel system prompt come `messages[2]`. **In-memory LRU cache TTL `AURA_AGENT_INSIGHT_CACHE_TTL_SEC=600` (10 min)** — preserva byte-identity di `messages[2]` turn-su-turn (Slice 4 invariant cross-slice). Senza cache: Slice 4 KV cache si rompe quando Slice 11e atterra (Pitfall #3, Risk #5). |

### Architettura (4 sub-slice archival MVP 11a/11b/11d/11e + 11c DEFERITA amendment #27 + 11f Task Canvas sequencing-indipendente)

**11a — Schema Neo4j + taxonomy doc (~200 LOC, niente impl)**:
```cypher
// === USER MEMORY (single-user 'local' scope) ===
CREATE CONSTRAINT document_id FOR (d:Document) REQUIRE d.id IS UNIQUE;
CREATE CONSTRAINT chunk_id    FOR (c:Chunk)    REQUIRE c.id IS UNIQUE;
CREATE CONSTRAINT entity_id   FOR (e:Entity)   REQUIRE e.id IS UNIQUE;
CREATE CONSTRAINT community_id FOR (cm:Community) REQUIRE cm.id IS UNIQUE;

// amendment #24: vector.dimensions HARDCODED a 768 (NON `${AURA_EMBED_DIMENSIONS}`):
// il runner migrate.go calcola il checksum sui byte grezzi del file PRIMA di ogni
// sostituzione → templating per-env romperebbe l'invariante checksum (vedi nota #24
// + ritiro acceptance #18 sotto). Contratto dimensione garantito a runtime da pingEmbed.
CREATE VECTOR INDEX chunk_embedding FOR (c:Chunk) ON (c.embedding)
  OPTIONS {indexConfig: {`vector.dimensions`: 768, `vector.similarity_function`: 'cosine', `vector.hnsw.m`: 32, `vector.hnsw.ef_construction`: 200}}; -- amendment #20: HNSW M=32 (NOT default 16) per recall@5 ≥ 0.8 @ 100K corpus
CREATE VECTOR INDEX entity_embedding FOR (e:Entity) ON (e.embedding)
  OPTIONS {indexConfig: {`vector.dimensions`: 768, `vector.similarity_function`: 'cosine', `vector.hnsw.m`: 32, `vector.hnsw.ef_construction`: 200}}; -- amendment #20: HNSW M=32 (NOT default 16) per recall@5 ≥ 0.8 @ 100K corpus
CREATE VECTOR INDEX community_embedding FOR (cm:Community) ON (cm.embedding)
  OPTIONS {indexConfig: {`vector.dimensions`: 768, `vector.similarity_function`: 'cosine', `vector.hnsw.m`: 32, `vector.hnsw.ef_construction`: 200}}; -- amendment #20: HNSW M=32 (NOT default 16) per recall@5 ≥ 0.8 @ 100K corpus
CREATE VECTOR INDEX agent_insight_embedding FOR (i:AgentInsight) ON (i.embedding)
  OPTIONS {indexConfig: {`vector.dimensions`: 768, `vector.similarity_function`: 'cosine', `vector.hnsw.m`: 32, `vector.hnsw.ef_construction`: 200}}; -- amendment #20: HNSW M=32 (NOT default 16) per recall@5 ≥ 0.8 @ 100K corpus
CREATE VECTOR INDEX reasoning_trace_embedding FOR (rt:ReasoningTrace) ON (rt.embedding)
  OPTIONS {indexConfig: {`vector.dimensions`: 768, `vector.similarity_function`: 'cosine', `vector.hnsw.m`: 32, `vector.hnsw.ef_construction`: 200}}; -- amendment #24: 5° index (reasoning traces, gated AURA_MEMORY_REASONING_TRACE_ENABLED)

CREATE FULLTEXT INDEX chunk_text FOR (c:Chunk) ON EACH [c.text];
CREATE FULLTEXT INDEX entity_name FOR (e:Entity) ON EACH [e.name];

CREATE INDEX entity_type   FOR (e:Entity)        ON (e.type);
CREATE INDEX entity_mentions FOR (e:Entity)      ON (e.mention_count);
CREATE INDEX episode_agent FOR (ep:AgentEpisode) ON (ep.agent_kind);
CREATE INDEX insight_agent FOR (i:AgentInsight)  ON (i.agent_kind);
// amendment #29: `identity_id` (OQ6) e `archived_at` (OQ4) sono scritti su :Entity ma
// NON indicizzati in MVP — in single-user lo scope è costante e l'archived-filter è
// query-time. L'index su `identity_id` è lo step additivo che atterra col landing
// multi-user (vedi OQ6); l'index su `archived_at` solo se il filtro diventa hot.

// Labels + properties:
// NB (amendment #29, OQ6): ogni nodo porta anche `identity_id` (costante 'local' in MVP single-user)
//     e ogni Cypher di lettura è scopato `WHERE n.identity_id = $identity_id` (refactor multi-user additivo).
// :Document   {id, source_uri, title, ingested_at, chunk_count, status, content_hash, identity_id}
// :Chunk      {id, document_id, sequence, text, embedding[768], tokens_count, identity_id}
// :Entity     {id, name, type, subtype, embedding[768], mention_count, first_seen_at, last_mentioned_at, archived_at, identity_id}
//             type ∈ {Person, Organization, Location, Concept, Event, Topic, Other}  // amendment #29 POLE+O: +Other catch-all
//             subtype = string free-form LLM-extracted (es. "Author","Family"); archived_at = soft-archive Memify (amendment #29 OQ4)
// :Community  {id, level (0=leaf, N=root), summary, embedding[768], member_count}  // amendment #27: schema provisioned, popolato solo quando 11c atterra (DEFERITA fuori MVP)
// :UserConversation {id, started_at, topic_summary, embedding[768], turn_count}
// :AgentEpisode  {id, agent_kind, started_at, ended_at, goal, outcome, summary}
//                agent_kind ∈ {chat, reasoning, worker}
// :AgentInsight  {id, agent_kind, pattern, confidence, observation_count, embedding[768], created_at}
// :ReasoningTrace {id, episode_id, step, decision, rationale, embedding[768], created_at}  // amendment #24, gated AURA_MEMORY_REASONING_TRACE_ENABLED

// Relations:
// (:Document)-[:HAS_CHUNK {seq}]->(:Chunk)
// (:Chunk)-[:MENTIONS {confidence}]->(:Entity)
// (:Entity)-[:RELATED_TO {weight, type}]->(:Entity)
// (:Entity)-[:IN_COMMUNITY]->(:Community)        // amendment #27: schema provisioned, popolato solo quando 11c atterra (DEFERITA fuori MVP)
// (:Community)-[:CONTAINS]->(:Community)   // hierarchical — amendment #27: idem, popolato solo da 11c (DEFERITA)
// (:UserConversation)-[:DISCUSSED]->(:Entity)
// (:UserConversation)-[:CITES]->(:Chunk)
// (:UserConversation)-[:USED_SNIPPET]->(:UserSnippet)   // Slice 7e mirror
// (:AgentEpisode)-[:LEARNED {strength}]->(:AgentInsight)
// (:AgentEpisode)-[:HANDLED]->(:UserConversation)
// (:AgentEpisode)-[:REASONED {seq}]->(:ReasoningTrace)   // amendment #24, gated
```

> **11a amendment #24 (2026-05-31) — Temporal validity model + NOOP + reasoning traces.**
> Convergenza dello studio comparativo 2026 (mem0 `ADD/UPDATE/DELETE/`**`NOOP`** + Zep/Graphiti valid-time + neo4j-agent-memory POLE+O reasoning nodes). Tre delta di **solo schema** in 11a (nessuna impl in 11a; plasmano il comportamento di 11b/11d/11e). Tutto sotto identity `'local'` (single-user) come il resto di Slice 11.
>
> **(1) Valid-time sulle fact-edge (Zep/Graphiti, semplificato — mono-temporale).** `:RELATED_TO` porta `valid_from` (timestamp, default = ingest time), `valid_to` (timestamp, `null` = vero adesso) e `confidence` (float 0-1). La conflict resolution è *invalidazione, non cancellazione*: un fatto contraddittorio imposta `valid_to = datetime()` sull'edge precedente e CREATE quello nuovo (la storia resta interrogabile). **Chi decide il conflitto:** 11a è solo-schema (fornisce le colonne); la *detection* di contraddizione fattuale (chiave = stesso `(startNode, type)` con `endNode` incompatibile — es. `(User)-[:LIVES_IN]->(città)` che cambia città — `:RELATED_TO` non ha proprietà `predicate`, il predicato È il `type` della relazione; distinta dal dedup di entità per fuzzy name+type) è responsabilità del **reconciler LLM di 11b** che emette `ADD | UPDATE(+invalidazione) | DELETE | NOOP` — non un constraint a write-time del grafo. Il retrieval (11d) DEVE preferire le edge con `valid_to IS NULL` e PUÒ esporre quelle superate solo su query esplicitamente storica (**11d acceptance da aggiornare di conseguenza** — vedi sotto). Schema aggiornato:
>   `// (:Entity)-[:RELATED_TO {weight, type, valid_from, valid_to, confidence}]->(:Entity)`
>   Il bi-temporale pieno (secondo asse `t_created`/`t_expired` di ingestion) è **rimandato** (Open question 7): un solo asse valid-time copre il failure mode dominante "il fatto è cambiato nel tempo" alla scala single-user.
>
> **(2) NOOP come esito di estrazione di prim'ordine (mem0).** Il reconciler entity/fact di 11b emette per ogni candidato uno di `ADD | UPDATE | DELETE | NOOP`. `NOOP` ("già noto, nulla cambia") è il guard anti-bloat e il caso comune sul re-ingest: non scrive nulla ed è loggato in `ingest_audit.action`. Questo **supera** la formulazione nuda "MERGE existing OR CREATE new" di 11b §entity_extractor: MERGE-senza-cambi == `NOOP`, MERGE-con-valore-cambiato == `UPDATE` (+ invalidazione valid-time del punto (1)).
>
> **(3) Reasoning traces come nodi di prim'ordine (neo4j-agent-memory POLE+O).** Nuova label `:ReasoningTrace {id, episode_id, step, decision, rationale, embedding[768], created_at}` + relazione `(:AgentEpisode)-[:REASONED {seq}]->(:ReasoningTrace)`. Predispone il journal (11e) a imparare dalle *proprie* decisioni passate, non solo dagli outcome. Vector index `reasoning_trace_embedding` (768d cosine, HNSW M=32, ef_construction 200) accanto ai quattro index di 11a. **Schema provisioned, NESSUN consumer in MVP**: con `AURA_MEMORY_REASONING_TRACE_ENABLED=false` (default) nulla scrive `:ReasoningTrace`, e l'insight clustering di 11e legge `:AgentEpisode` (outcome), non `:ReasoningTrace`. Lo schema atterra ORA solo per evitare una seconda migration; il read-path concreto in 11e è un *enhancement futuro*, non wiring MVP.
>
> **Nota path migration (auto-riconciliazione PRD).** La convenzione *implementata* è `internal/knowledge/migrations/000N_*.cypher` (vedi `internal/knowledge/migrate.go`, audit in `aura.knowledge_migrations`), **non** il path obsoleto `internal/db/migrations/neo4j/0002_memory_schema.cql` citato nell'acceptance 11a sotto. Lo schema temporale/reasoning atterra come `internal/knowledge/migrations/0002_memory_schema.cypher`. **Le migration Cypher sono forward-only** (il runner non ha sezione down; il marker `// -- migrate:up` è solo un commento, e un checksum-drift su versione applicata è errore fatto-corruzione): la riga di acceptance 11a "Migration Cypher … reversibile" va letta come *teardown via* `aura memory wipe-vectors --confirm` + re-apply, non come down-migration. Correzione propagata nell'acceptance 11a sotto e nel commit template 11a.
>
> **Conflitto risolto — dimensioni hardcoded vs templating (#18).** Il runner `migrate.go:loadMigrations` calcola il checksum sui **byte grezzi del file** *prima* di qualunque sostituzione; una sostituzione `${AURA_EMBED_DIMENSIONS}` per-env renderebbe il checksum env-dipendente (falso "history corruption" su host diverso) o desincronizzerebbe body-auditato vs body-applicato. Quindi: **il DDL di `0002_memory_schema.cypher` hardcoda `768`** (come già fa `0001_init.cypher`), e la riga di acceptance #18 sul templating `${...}` è da **ritirare** per le migration Cypher (il guard di dimensione resta `pingEmbed` al boot, `internal/knowledge/ping.go`). Non si possono avere insieme templating + checksum-su-byte-grezzi.
>
> **Limite splitter (D-08).** `splitCypherStatements` splitta su `;` naïve: `0002_memory_schema.cypher` NON deve contenere `;` dentro literal stringa (le `OPTIONS {...}` con `'cosine'`/`'standard'` sono safe; un futuro `CALL apoc`/`SET` con `;` in una stringa richiederebbe prima l'upgrade dello splitter).

**11b — Cognify ingestion pipeline (~500 LOC)**:
```
internal/memory/ingest/
  pipeline.go       # ~150  Cognify 6-stage orchestration:
                    #         1. Classify document (markdown vs code vs structured)
                    #         2. Check permissions (identity_id ownership)
                    #         3. Chunk (recursive semantic)
                    #         4. Extract entities (LLM batch)
                    #         5. Generate summary (LLM tier=worker)
                    #         6. Embed + commit to Neo4j
                    #       Idempotent: content_hash check pre-ingest
  chunker.go        # ~120  Recursive semantic chunker:
                    #         AURA_MEMORY_CHUNK_SIZE_TOKENS=512 (default)
                    #         AURA_MEMORY_CHUNK_OVERLAP_TOKENS=64
                    #         Respect markdown headers (split su ##/###)
                    #         Fallback sliding window se no struttura
  embedder.go       # ~100  Batch embedding via aura-llama-embed (Slice 0.7):
                    #         batch_size 32 per network roundtrip
                    #         retry exp backoff
                    #         token estimation pre-call per budget
  entity_extractor.go  # ~180  Mem0 2-fase pattern:
                       #         Fase 1: LLM extract candidates per batch 10 chunks
                       #                 (tier=reasoning, JSON output schema)
                       #         Fase 2: conflict detect via fuzzy match name+type
                       #                + embedding similarity > 0.92
                       #                MERGE existing OR CREATE new
                       #       Type taxonomy POLE+O (amendment #29): Person/Org/Location/
                       #       Concept/Event/Topic/Other (chiuso, queryable) + subtype free-form
  audit.go          # ~50   sqlc adapter aura.ingest_audit (parity skill_audit)
internal/agent/tools/ingest.go   # ~90   Tool LLM-facing Deferred=true:
                                   # ingest.file(path), ingest.url(url),
                                   # ingest.text(content, source_name)
                                   # ActionRouter dispatch
internal/channels/telegram/handlers.go (diff)  # ~+50
                                   # Document attach → auto ingest.file trigger
                                   # (riusa Slice 9c markitdown pipeline)
internal/db/queries/ingest_audit.sql           # ~40   4 query sqlc
internal/db/migrations/00NN_ingest_audit.up.sql  # ~50 (numero assegnato all'atterraggio; ≠ 0007/0008/0009/0010/0012/0013 già riservati)
```

**11c — Community detection + summarization (~250 LOC) — DEFERITA fuori MVP (amendment #27)**:

> Feature lazy/on-demand futura, NON parte dell'MVP Phase 15. GDS resta installato (Slice 0.7) per algoritmi ad-hoc (PageRank/WCC/Node-Similarity); solo il job schedulato Leiden + le community-summary LLM sono rimandati. Lo schema `:Community`/`:IN_COMMUNITY`/`:CONTAINS` + index `community_embedding` è già provisioned in 11a (evita una futura migration). Quando/se 11c atterrerà come feature on-demand, popolerà quello schema. Blocco architetturale conservato come riferimento:
```
internal/memory/graph/
  community.go      # ~200  Leiden detection via Neo4j GDS (DEFERITA):
                    #         CALL gds.leiden.stream('entity-graph',
                    #              {relationshipWeightProperty: 'weight'})
                    #       Hierarchical: level 0=leaf entities, level N=root
                    #       Trigger on-demand (NON background schedulato in MVP);
                    #       AURA_MEMORY_COMMUNITY_INTERVAL_HR resta env deferita
                    #       Per community: LLM tier=worker summarize members
                    #       Persist :Community node + relations
  community_test.go # ~80   Smoke test: 50 entity → cluster atteso N>=3 communities
```

**11d — Retrieval hybrid + LLM re-ranker (~400 LOC)**:
```
internal/memory/retrieval/
  search.go         # ~150  Hybrid retrieval:
                    #         BM25: CALL db.index.fulltext.queryNodes('chunk_text', $q)
                    #              YIELD node, score AS bm25_score
                    #         HNSW: CALL db.index.vector.queryNodes('chunk_embedding', 20, $q_embedding)
                    #              YIELD node, score AS vector_score
                    #         Graph 1-hop: MATCH (c)-[:MENTIONS]->(:Entity)<-[:MENTIONS]-(c2)
                    #         Fusione = Weighted-RRF Cypher-native (amendment #26): ogni
                    #           sorgente (vector HNSW, fulltext BM25, graph 1-hop per
                    #           co-mention count) ritorna i propri topK rankati; UNION;
                    #           per nodo contribution = sourceWeight / (rrfConstant + rank),
                    #           sommato cross-source; ORDER BY total DESC. Rank-based,
                    #           scale-free → nessuna normalizzazione dei punteggi grezzi.
                    #           Params: rrfConstant=AURA_MEMORY_RRF_CONSTANT (60),
                    #           sourceWeights {vector:1.0, fulltext:1.0, graph:1.0} tunable,
                    #           sourceK=20, finalK=5. (Fusione lineare pesata 0.4/0.4/0.2 =
                    #           fallback DEPRECATO, non default.)
                    #         Return top-20 candidates
  rerank.go         # ~100  LLM tier=worker re-ranker:
                    #         Prompt: "Score 0-10 relevance: query={q} chunk={text}"
                    #         Batch 4 chunks per call (cost optimization)
                    #         Return top-5 by re-ranked score
                    #       Cost stima: 5 calls × $0.0002 = $0.001/query
  recall.go         # ~80   Entity-based recall:
                    #         memory.recall(entity_id) → all chunks MENTIONING
                    #         (+ community summary SOLO se 11c landed — DEFERITA amendment #27)
                    #         + RELATED_TO entities up to 2-hop (valid_to IS NULL, amendment #24)
  global_search.go  # ~70   GraphRAG global pattern — DEFERITO con 11c (amendment #27):
                    #         Query → embed → top-K :Community by community.embedding
                    #         Return community summaries (not chunks)
                    #         Per query "general knowledge across dataset"
                    #       NON in MVP: dipende da :Community popolate da 11c (deferita).
                    #       In MVP memory.search espone solo scope=local.
internal/agent/tools/memory.go   # ~150  Tool LLM-facing Deferred=true:
                                  # memory.search(query, scope=local, k=5),
                                  #   (scope=global DEFERITO con 11c — amendment #27)
                                  # memory.recall(entity_name|entity_id),
                                  # memory.forget(doc_id|entity_id|chunk_id) (GDPR)
                                  # memory.summarize_conversation(conv_id) (manual rollup)
```

**11e — Agent journal + Memify post-processing (~300 LOC)**:
```
internal/memory/agent/
  journal.go        # ~150  Post-conversation goroutine:
                    #         Trigger on conversation.UpdateStatus('archived')
                    #         Summarize episode (LLM tier=worker):
                    #           goal, outcome, key entities discussed,
                    #           tools used, success/failure
                    #         Persist :AgentEpisode + :HANDLED relation
                    #       Parity con Slice 7e snippet pattern analyzer.
  insight.go        # ~200  Cross-conv pattern analyzer:
                    #         Background ogni AURA_MEMORY_INSIGHT_INTERVAL_MIN=60
                    #         Query :AgentEpisode ultimi 7gg
                    #         Cluster outcome+goal via embedding HNSW
                    #         Cluster size >= 3 → synthesize candidate (LLM tier=reasoning)
                    #         Persist :AgentInsight + :LEARNED {strength} relation
                    #       Inject hook (Slice 0.9 PromptBuilder):
                    #         Pre-conv: query top-K :AgentInsight relevant via embedding
                    #         Inject come third system message (post Agent.md Slice 10)
                    #         **Cache TTL (amendment #11 — Architecture spec gap):**
                    #         `AURA_AGENT_INSIGHT_CACHE_TTL_SEC=600` (default 10 min).
                    #         In-memory LRU keyed by (identity_id, query_embedding_hash).
                    #         During TTL window the SAME top-K subset is returned → messages[2]
                    #         is byte-identical turn-su-turn → KV cache hit on the third system
                    #         message (parity with messages[0] discipline of Slice 4).
                    #         Cache invalidation: explicit on :AgentInsight upsert (insight.go
                    #         analyzer goroutine triggers Invalidate(identity_id)); implicit on
                    #         TTL expiry. Without this cache, every turn re-queries Neo4j HNSW
                    #         and returns slightly different top-K (embedding similarity noise)
                    #         → messages[2] varies → Slice 4 invariant breaks → cache hit rate
                    #         collapses (see Pitfall #3 cross-slice, Risk #5 architectural spec
                    #         gap; reference_aura_cache_poisoning_sites_2026-05-27 site #7).
internal/memory/graph/memify.go  # ~250  Cognee Memify post-processing:
                                  #   Background ogni AURA_MEMORY_MEMIFY_INTERVAL_HR=24
                                  #   1. Soft-archive stale (amendment #29 — NO hard delete,
                                  #      riconciliato con #24 "invalidate don't delete"):
                                  #      :Entity con last_mentioned_at < AURA_MEMORY_PRUNE_IDLE_DAYS (180gg)
                                  #      AND mention_count < AURA_MEMORY_PRUNE_MIN_MENTIONS (3) →
                                  #      SET archived_at=datetime() + invalida edge correnti (valid_to=datetime()
                                  #      WHERE valid_to IS NULL). Escluse da retrieval/HNSW; subgrafo preservato
                                  #      (ri-attivabile su nuova mention). Hard DETACH DELETE solo per orphan
                                  #      da :Document cancellato (cleanup referenziale, §3807).
                                  #   ⚠️ valid-time aware (amendment #24): strengthen/derive
                                  #   operano SOLO su edge correnti (valid_to IS NULL); le edge
                                  #   invalidate non vengono riforzate né traversate (evita di
                                  #   resuscitare fatti superati).
                                  #   2. Strengthen frequent: :RELATED_TO.weight +=
                                  #      co-occurrence count ultimi 7gg  (WHERE r.valid_to IS NULL)
                                  #   3. Derive facts: multi-hop traversal
                                  #      (A)-[:RELATED_TO]->(B)-[:RELATED_TO]->(C)  (tutte valid_to IS NULL)
                                  #      con weight > 0.7 → derive (A)-[:DERIVED_FROM]->(C)
                                  #   Audit log INSERT memify_audit row per ogni op
```

**11f — Task Canvas: working-memory simbolica effimera (amendment #25, 2026-05-31) (~400 LOC)**:

> **Cos'è e cosa NON è.** Pattern rubato da TencentDB-Agent-Memory (TDAI) "Mermaid Symbolic Canvas" + score-cascade. **NON è memoria a lungo termine** — è *context-engineering* per task multi-step in volo (ricerche, run multi-tool da Telegram) dove gli output verbosi dei tool gonfiano la finestra e il conto OpenRouter. Comprime lo stato-transizioni del task in un grafo Mermaid denso tenuto in contesto, **offloadando i log grezzi su file**, recuperabili on-demand via `node_id`. Lo studio comparativo 2026 lo classifica "real win but narrow": adottare il *meccanismo* (non i numeri vendor −61%, self-reported), validando il risparmio reale sul carico Telegram.
>
> **Disaccoppiamento dalla memoria archival.** 11f è ortogonale a 11a-11e: non tocca Neo4j, non dipende da ingestion/retrieval/journal. Dipende solo dal context management (Phase 6) e dal loop. Consumatori naturali: **Swarm (Phase 9)** (stato coordinatore parent↔child) e **Agent journal 11e** (`:AgentEpisode.state_canvas` opzionale invece/oltre la prosa). Sequencing: **può atterrare a ~Phase 9-10**, molto prima di Phase 15. Vive in `internal/canvas/` (package standalone, importato da conversations e swarm — non sotto `internal/memory/` per non implicare che sia store archival).
>
> **Rispetto dell'invariante KV-cache (vincolo duro, cross-ref Slice 4 / Phase 6 / amendment #16).** Il canvas è iniettato nel **tail dinamico** del prompt, *dopo l'ultimo messaggio user* — quindi **dopo il prefisso cacheable pinnato `[0..2]`** (system+tools / Agent.md / cached `:AgentInsight` — confine CHIUSO da amendment #29, vedi §1588/§1613: il canvas sta sempre nel tail `[3..N]`, mai nel prefisso). Non altera la byte-identity del prefisso, quindi non pianta un cache-poisoning site. Punto d'inserzione mai dentro una coppia tool_use/tool_result (parità con la logica TDAI `adjustForToolCallPair`).

```
internal/canvas/
  canvas.go         # ~90   Modello Mermaid `flowchart TD`. Node format 1-riga:
                    #         NodeID["fase: azione<br/>status: done|doing|paused|blocked
                    #         <br/>summary: ≤150ch<br/>ts: ISO8601"]
                    #       NodeID schema `\d{3}-N\d+` (prefisso = canvas id, N = ordinale).
                    #       Header meta %%{taskGoal, progress(0-100), createdTime, updatedTime}%%.
                    #       Budget AURA_CANVAS_MAX_BYTES=4000 (soft-warn >2500).
  builder.go        # ~110  Genera/aggiorna il canvas da agent.Event del loop.
                    #       ⚠️ ONESTÀ vs TDAI: in TDAI TRE campi sono LLM-generati —
                    #       `summary`, lo `score` 0-10 (必填/mandatory, è ciò che PILOTA
                    #       il cascade) e `node_mapping` tool_call_id→NodeID (1:N).
                    #       MODALITÀ DETERMINISTICA (default, no LLM) produce solo la
                    #       TOPOLOGIA: 1 nodo per tool_call, edge da sequenza, node_id
                    #       assegnato SINCRONO all'offload (l'ordinale N è owned dal builder
                    #       → niente finestra NULL, niente backfill/wait di TDAI). summary =
                    #       head troncato del tool result; score = euristica (vedi offload.go).
                    #       È uno SKELETON DEGRADATO, non parità TDAI.
                    #       MODALITÀ LLM-REFINE (AURA_CANVAS_LLM_REFINE_ENABLED=false default) recupera
                    #       summary/score/node_mapping veri — e SOLO qui esiste il lifecycle
                    #       node_id NULL→backfill, che richiede di portare il "wait"/retry +
                    #       pickMmdDerivedFallbackNodeId di TDAI o la riga resta orfana.
                    #       Editing incrementale line-based (parità TDAI patchMmd): replace_blocks
                    #       su range di righe, no rewrite full.
  offload.go        # ~90   Score-cascade compression (TDAI). In TDAI lo score 0-10 di
                    #       "rimpiazzabilità" è LLM-mandatory e i tool-result sono sostituiti
                    #       col summary in ordine di score DESC (most-replaceable-first → TIENE
                    #       in contesto gli high-info). MODALITÀ DETERMINISTICA usa un'euristica
                    #       size/recency/re-access (formula amendment #29, vedi OQ8): score =
                    #       w_size·norm(bytes) + w_recency·age_rank − w_reaccess·was_read_again
                    #       (W_SIZE=5, W_RECENCY=3, W_REACCESS=4; SIZE_REF_BYTES=8192). NON ha
                    #       garanzia di preservazione informativa MA è SICURA (cascade non-lossy:
                    #       il raw resta su sidecar, recuperabile via canvas.read_offload — lo score
                    #       riordina solo l'ordine di eviction). Per la semantica TDAI vera serve
                    #       AURA_CANVAS_LLM_REFINE_ENABLED=true (on solo se offload-recovery-rate>0.15).
                    #       Risultato grezzo già su sidecar file (riusa ToolResult sidecar
                    #       Slice 3, $AURA_RUN_DIR); il messaggio in-context diventa:
                    #         [Offloaded Tool Result | node: 001-N3]
                    #         Summary: <...>
                    #         result_ref: <sidecar path> (canvas.read_offload(node_id) per il raw)
  trigger.go        # ~30   Gate da pressione di contesto vs finestra.
                    #       ⚠️ Le ratio TDAI (0.5/0.85/0.95) sono tarate su una finestra
                    #       200K (types.ts:230). Aura usa DeepSeek-V4 ~1M → 0.5 = 500K, troppo
                    #       presto (il modello regge comodo) = overhead per nulla. Quindi:
                    #       trigger = max(ratio·window, AURA_CANVAS_MIN_FLOOR_TOKENS=300000):
                    #         mild AURA_CANVAS_MILD_RATIO=0.5 (score-cascade),
                    #         aggressive AURA_CANVAS_AGGRESSIVE_RATIO=0.85 (head-delete + canvas
                    #         compensativo), emergency AURA_CANVAS_EMERGENCY_RATIO=0.95.
                    #       Il floor assoluto rende il gate robusto alla finestra effettiva
                    #       (finestra 1M canonica — vedi §1552/§1167, amendment #24; il floor
                    #       resta come robustezza alla finestra effettiva del modello attivo).
                    #       Sotto soglia: NO canvas (evita overhead su task brevi —
                    #       gimmick-risk). Gate anche su AURA_CANVAS_MIN_TASK_STEPS=4.
  store.go          # ~30   Persistenza canvas + offload index in Postgres (vedi sotto).
internal/agent/tools/canvas.go                   # ~50   Builtin (NON deferred) canvas.read_offload(
                                                  #        node_id, offset?, limit?): node_id→offload_entry
                                                  #        →tool_call_id; delega a sidecarPath(runDir,sessionID,
                                                  #        toolCallID) (result.go) RIUSANDO il guard validateID;
                                                  #        re-derive cross-process, NO path assoluto persistito
                                                  #        (read_tool_output è già durevole — amendment #28).
internal/db/queries/task_canvas.sql              # ~40   sqlc query
internal/db/migrations/00NN_task_canvas.up.sql   # ~45   (numero assegnato all'atterraggio;
                                                  #        NON 0007 — riservato a skill_audit Slice 7c)
internal/db/migrations/00NN_task_canvas.down.sql # ~5
```

**Schema Postgres (operational state → Postgres + sidecar file; coerente con lo split 3-store, NIENTE su Neo4j):**
```sql
CREATE TABLE aura.task_canvas (
    id              uuid        PRIMARY KEY,
    conversation_id uuid        NOT NULL REFERENCES aura.conversations (id) ON DELETE CASCADE,
    task_goal       text        NOT NULL,
    progress        smallint    NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 100),
    mermaid         text        NOT NULL,        -- il canvas corrente (≤ AURA_CANVAS_MAX_BYTES)
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE aura.offload_entry (
    canvas_id       uuid        NOT NULL REFERENCES aura.task_canvas (id) ON DELETE CASCADE,
    tool_call_id    text        NOT NULL,
    node_id         text,                        -- '001-N3'; in modalità deterministica assegnato SINCRONO (mai NULL); NULL solo in LLM-refine prima del backfill
    summary         text        NOT NULL,
    score           smallint    NOT NULL DEFAULT 0 CHECK (score BETWEEN 0 AND 10),
    result_ref      text,                        -- (opzionale, audit breadcrumb) path RELATIVO conversations/<conv_id>/<tool_call_id>.result; il recupero canonico RI-DERIVA da (conv_id, tool_call_id) via sidecarPath (result.go), NON da un path assoluto persistito (amendment #28)
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (canvas_id, tool_call_id)
);
```
> **Recupero cross-process (agent-driven) — amendment #28 (corretto vs codice shipped).** Il builtin `read_tool_output` (Slice 3 A4) **è GIÀ durevole cross-process**: ricostruisce il path in modo deterministico via `sidecarPath(runDir, sessionID, toolCallID)` (`internal/agent/tools/result.go:63`) e rilegge da disco — **nessuna `resultPaths` map in-memory** (quel design citato in §Slice 1 NON è stato implementato; `runDir` viene da config, `sessionID`=`conv_id`, `toolCallID` è persistito in `aura.conversation_turns`). Quindi il recupero di un offload di un turn/processo precedente **funziona già**. `canvas.read_offload(node_id, offset?, limit?)` è perciò un **thin wrapper**: (a) `node_id`→`offload_entry`→`tool_call_id`, (b) delega a `sidecarPath` + lettura byte-offset **riusando il guard path-traversal esistente `validateID`** (`result.go:45`, rifiuta `..`/separatori su entrambi gli id). Il path **NON va persistito come assoluto** (meno portabile se `AURA_RUN_DIR` cambia): si **ri-deriva** da (`conv_id`, `tool_call_id`), esattamente come `read_tool_output`. GC: `ON DELETE CASCADE` dalla conversazione + reclaim dei sidecar via lo sweep di `$AURA_RUN_DIR` già previsto.

### Smoke

```bash
# Setup: Slice 0.7 Neo4j + GDS attivo (ad-hoc PageRank/WCC; community detection 11c DEFERITA, amendment #27), Slice 9c markitdown attivo

# 1. Ingest documento
aura ingest /home/user/papers/voyager.pdf
# → markitdown convert pdf → markdown
# → chunker semantic 512 token → 47 chunks
# → embedder batch 2 round → 47 embeddings 768d
# → LLM entity extraction batch 5 round → 23 entities (Person/Org/Concept)
# → Neo4j upsert :Document + 47 :Chunk + 23 :Entity + 47 :HAS_CHUNK + 89 :MENTIONS
# → ingest_audit INSERT

aura memory search "Minecraft skill library lifelong learning"
# → BM25 top-20 + HNSW top-20 + graph 1-hop expansion (co-mention rank)
# → Weighted-RRF fusion (rrfConstant=60, scale-free)
# → LLM re-rank tier=worker → top-5
# → return chunks con source citation

# 2. Telegram ingest auto
# user invia PDF via Telegram document attach
# → bot reaction 📄 + handler chiama ingest.file auto
# → bot risponde "✅ Ingerito voyager.pdf: 47 chunks, 23 entities"

# 3. Community detection — DEFERITA fuori MVP (amendment #27, sub-slice 11c)
# (Quando/se 11c atterrerà come feature on-demand:)
#   Neo4j: 156 entities → Leiden detect 12 communities level 0, 3 level 1
#   Per community: LLM summary tier=worker
#   aura memory search "AI agents general topic" → global search top-3 community summaries
# In MVP: il global/thematic search non è disponibile; il local retrieval (11d) copre il bisogno.

# 4. Agent journal (post-conv)
# user "trova autore di voyager paper" → tool memory.search → response
# conversation archived → :AgentEpisode summary
# 7 giorni dopo (4 episode simili "find author paper"):
# pattern_analyzer cluster → :AgentInsight "When user asks paper author,
# memory.search 'authors' + memory.recall first author entity is best"
# Pre-conv next time: top-3 insight injected nel system prompt

# 5. GDPR forget
aura memory forget --document voyager.pdf
# → DETACH DELETE :Document + 47 :Chunk + dangling :Entity orphans cleanup
# → audit log FORGET entry
```

### Acceptance

#### 11a — Schema
- [ ] 5 vector index Neo4j (chunk/entity/community/agent_insight/reasoning_trace) creati con 768 dim cosine (il `community_embedding` è provisioned ma popolato solo quando 11c atterra — DEFERITA, amendment #27; `reasoning_trace_embedding` popolato solo se `AURA_MEMORY_REASONING_TRACE_ENABLED=true` — amendment #24)
- [ ] 2 fulltext index (chunk_text, entity_name) creati
- [ ] 4 constraint UNIQUE per id
- [ ] 4 index proprietà (entity_type, mention_count, agent_kind episode/insight)
- [ ] Migration Cypher in `internal/knowledge/migrations/0002_memory_schema.cypher` (path reale — vedi amendment #24; il `internal/db/migrations/neo4j/*.cql` qui sopra è obsoleto). **Forward-only** (il runner `internal/knowledge/migrate.go` non ha sezione down; teardown = `aura memory wipe-vectors --confirm` + re-apply). Re-apply idempotente; checksum-drift su versione applicata = errore fatale
- [ ] **Embedding dim consistency check (amendment #18, RICONCILIATO da #24)**: le `CREATE VECTOR INDEX` di `0002_memory_schema.cypher` **hardcodano `768`** (NON `${AURA_EMBED_DIMENSIONS}`) — il templating per-env è **ritirato** perché incompatibile col checksum-su-byte-grezzi di `migrate.go` (vedi nota #24). Il contratto dimensione è garantito a runtime dal boot self-test `pingEmbed` (`internal/knowledge/ping.go`): se il sidecar `aura-llama-embed` ritorna `output_dim ≠ 768`, Aura **fail-fast al boot** (no corruzione silenziosa dell'index HNSW, Pitfall #7 P0). Cambio dimensione = `aura memory wipe-vectors --confirm` + edit DDL + re-apply (DB forward-only). Acceptance test: mock sidecar dim=1024 → boot fallisce con errore esplicito prima di qualunque upsert.
- [ ] **HNSW M=32 verification (amendment #20)**: post-migration Cypher `SHOW INDEXES YIELD name, options WHERE name = 'chunk_embedding' RETURN options` MUST return `{vector.hnsw.m: 32, vector.hnsw.ef_construction: 200, vector.dimensions: 768, vector.similarity_function: 'cosine'}`. Same for entity_embedding, community_embedding, agent_insight_embedding, reasoning_trace_embedding (5 index totali — amendment #24; `community_embedding` e `reasoning_trace_embedding` sono creati ma popolati solo rispettivamente quando 11c atterra / se reasoning-trace abilitato).
- [ ] **Valid-time fact edges (amendment #24)**: `:RELATED_TO` definita con proprietà `valid_from`, `valid_to` (nullable), `confidence`. Acceptance: CREATE di due fatti contraddittori sulla stessa chiave `(startNode, type)` con `endNode` diverso → il primo edge ha `valid_to` impostato (non null), il secondo `valid_to IS NULL`; query `MATCH (s)-[r:RELATED_TO]->() WHERE r.valid_to IS NULL` ritorna solo il fatto corrente.
- [ ] **Reasoning trace schema opzionale (amendment #24)**: label `:ReasoningTrace` + relazione `:REASONED` + vector index `reasoning_trace_embedding` (768d cosine, M=32, ef_construction 200) presenti nello schema. Gated da `AURA_MEMORY_REASONING_TRACE_ENABLED` (default `false`): con flag off, 11e non scrive `:ReasoningTrace` ma lo schema esiste (no seconda migration).

#### 11b — Ingestion
- [ ] Tool `ingest.file(path)` chiama markitdown → chunker → embedder → entity extractor → Neo4j upsert
- [ ] `chunker.go` recursive semantic split (respect markdown headers), default 512 token, overlap 64
- [ ] `embedder.go` batch 32 chunks/call, retry exp backoff, ctx-cancel
- [ ] `entity_extractor.go` mem0 2-fase: LLM batch 10 chunks, JSON output schema, fuzzy dedup + embedding similarity > 0.92
- [ ] `ingest_audit` row INSERT per ogni ingest (parity skill_audit)
- [ ] Idempotent: content_hash check, ri-ingest stesso file = no-op + log
- [ ] Telegram document handler chiama `ingest.file` auto + risposta confirm
- [ ] **Embed batch dim consistency (amendment #18, Pitfall #7 P0)**: `embedder.go` Batch() asserts every returned vector has `len(v) == AURA_EMBED_DIMENSIONS`. Mismatch on any returned vector → abort ingest, log `embedding sidecar returned wrong dim — refusing to corrupt Neo4j vector index`. Acceptance test: mock sidecar returning 384d vector → ingest aborts before Neo4j upsert.

#### 11c — Community detection — DEFERITA fuori MVP (amendment #27)
> Nessun acceptance richiesto in Phase 15. Feature lazy/on-demand futura. GDS resta installato (Slice 0.7) per algoritmi ad-hoc. Quando 11c atterrerà, l'acceptance sarà (conservato come riferimento):
> - [ ] (deferito) Trigger on-demand chiama `CALL gds.leiden.stream(...)` su entity graph (NON background schedulato)
> - [ ] (deferito) Per community: LLM tier=worker summarize membri (max 10 entity per call)
> - [ ] (deferito) Persist `:Community` + `:IN_COMMUNITY` + hierarchical `:CONTAINS`

#### 11d — Retrieval
- [ ] Tool `memory.search(query, scope, k=5)` hybrid retrieval (BM25 + HNSW + graph 1-hop)
- [ ] **Fusione = Weighted-RRF Cypher-native (amendment #26)**: vector (HNSW) + fulltext (BM25) + graph 1-hop (rank by co-mention count) come 3 sorgenti rankate, UNION, `contribution = sourceWeight / (rrfConstant + rank)` sommato cross-source, ORDER BY total DESC. `rrfConstant=AURA_MEMORY_RRF_CONSTANT` (default 60), `sourceWeights {vector:1.0, fulltext:1.0, graph:1.0}` tunable, `sourceK=20`, `finalK=5`. Rank-based, scale-free: NESSUNA normalizzazione dei punteggi grezzi. La fusione lineare pesata `0.4*bm25 + 0.4*vector + 0.2*graph` è fallback DEPRECATO, non il default. Acceptance test: BM25 e vector con scale di punteggio molto diverse → ranking stabile senza normalizzazione; rimozione di una sorgente degrada ma non rompe l'ordinamento.
- [ ] LLM re-ranker top-20 → top-5 tier=worker, batch 4
- [ ] **HNSW M=32 baseline (amendment #20, UX-08)**: vector index creation MUST set `vector.hnsw.m: 32` (NOT Neo4j default 16) + `vector.hnsw.ef_construction: 200`. Acceptance test: post-migration, `SHOW INDEXES YIELD name, options` returns `m=32` for all 5 vector indexes (chunk/entity/community/agent_insight/reasoning_trace — amendment #24). Pre-merge benchmark recall@5 ≥ 0.8 @ 1K/10K/100K synthetic corpus (run `scripts/memory_recall_bench.sh` — file authored in Phase 15).
- [ ] **`docs/aura-quality-snapshot.md` CI gate (amendment #20)**: pre-merge for any PR touching `internal/memory/**` or `internal/db/migrations/neo4j/**`, `scripts/quality_snapshot_gate.sh` validates that the Phase 15 row in `docs/aura-quality-snapshot.md` has `Last measured` date ≥ PR base commit date. Stale → CI fail with `quality snapshot row 'GraphRAG retrieval recall@5 @ 100K corpus' stale — owner Phase 15 must re-measure and update before merge (amendment #20)`. Snapshot rows for Phase 5 (sandbox escape rate), Phase 6 (cache hit rate), Phase 11 (snippet success rate), Phase 13 (MarkdownV2 escape fuzz) gated by analogous path globs (see `docs/aura-quality-snapshot.md` CI gate contract section).
- [ ] **Valid-time aware retrieval (amendment #24)**: `memory.search`/`memory.recall` filtrano di default le `:RELATED_TO` con `valid_to IS NULL` (solo fatti correnti); le edge superate sono escluse salvo flag `include_historical`. Acceptance: dopo invalidazione (fatto X→Y), una query su quel predicate ritorna Y, non X.
- [ ] Tool `memory.recall(entity)` ritorna all chunks MENTIONING + 2-hop entities
- [ ] Tool `memory.forget(id)` GDPR-compliant cascade + audit FORGET row
- [ ] Tool `memory.summarize_conversation(conv_id)` manual rollup `:UserConversation`
- [ ] ~~Global search: scope=global → top-K `:Community` summaries~~ DEFERITO con 11c (amendment #27): dipende da `:Community` popolate da 11c; in MVP `memory.search` espone solo `scope=local`. L'acceptance MVP è: `memory.search` rifiuta/ignora `scope=global` con messaggio "global search not available (11c deferred)".

#### 11e — Agent journal + Memify
- [ ] Post-conv trigger crea `:AgentEpisode` con summary LLM-generated
- [ ] Cross-conv analyzer ogni 60min: cluster episode similarity → `:AgentInsight`
- [ ] PromptBuilder injection: top-3 `:AgentInsight` relevant come third system message
- [ ] **`:AgentInsight` retrieval cache (amendment #11)**: `internal/memory/agent/insight_cache.go` LRU (capacity 1024 entries, TTL `AURA_AGENT_INSIGHT_CACHE_TTL_SEC=600`). `Get(identity_id, query_embedding_hash) ([]Insight, bool)` + `Put(identity_id, query_embedding_hash, []Insight)` + `Invalidate(identity_id)`. Test invariant: 5 consecutive `PromptBuilder.Build(...)` calls within TTL → `messages[2]` SHA-256 identical (cross-link with `scripts/cache_invariant_audit.sh` from amendment #16). Test invalidation: insight.go analyzer Upsert → cache hit returns fresh data on next call within same TTL window.
- [ ] Memify background 24h: soft-archive stale (amendment #29: `archived_at` + invalida edge correnti, NO hard delete) + strengthen frequent + derive facts. Acceptance: un'`:Entity` oltre soglia (180gg/<3 mention) ottiene `archived_at` non-null, le sue edge `valid_to IS NULL` diventano invalidate, ed è esclusa dai candidate-set di retrieval — ma il nodo e il subgrafo restano interrogabili; una nuova mention la ri-attiva (`archived_at=NULL`).
- [ ] **Memify valid-time aware (amendment #24)**: strengthen/derive operano solo su `:RELATED_TO` con `valid_to IS NULL`. Acceptance: un'edge invalidata (`valid_to` non null) non viene riforzata né usata nel multi-hop derive.
- [ ] Audit log per ogni operazione Memify
- [ ] **Reasoning trace writer gated (amendment #24)**: `journal.go`/`insight.go` scrivono `:ReasoningTrace` + `:REASONED {seq}` SOLO se `AURA_MEMORY_REASONING_TRACE_ENABLED=true`. Acceptance: con flag off (default), nessun nodo `:ReasoningTrace` creato; con flag on, ogni step di reasoning dell'episodio persiste un nodo collegato all'`:AgentEpisode`.

#### 11f — Task Canvas (amendment #25)
- [ ] `canvas.go` produce Mermaid `flowchart TD` valido con NodeID schema `\d{3}-N\d+`, header meta `%%{...}%%`, nodi 1-riga con `status`/`summary`/`ts`; canvas ≤ `AURA_CANVAS_MAX_BYTES=4000`.
- [ ] `builder.go` genera il canvas in modalità **deterministica** dagli `agent.Event` (zero chiamate LLM) di default; `AURA_CANVAS_LLM_REFINE_ENABLED=false` di default; editing incrementale line-based (no rewrite full).
- [ ] `trigger.go` gating: sotto `AURA_CANVAS_MILD_RATIO=0.5` della finestra **nessun** canvas; mild→score-cascade, aggressive `0.85`→head-delete+canvas, emergency `0.95`. Gate anche su `AURA_CANVAS_MIN_TASK_STEPS=4`. Acceptance: task da 2 step → nessun canvas creato.
- [ ] `offload.go` score-cascade: tool-result sostituiti col summary in ordine di `score` decrescente; il messaggio in-context contiene `node`, `Summary`, riferimento al tool; raw recuperabile via `canvas.read_offload(node_id, ...)` che ri-deriva il path (read_tool_output è anch'esso già durevole — amendment #28).
- [ ] **Score deterministico + gate token misurabile (amendment #29, OQ8)**: `score = W_SIZE·norm(bytes/SIZE_REF) + W_RECENCY·age_rank − W_REACCESS·was_read_again` (default 5/3/4, SIZE_REF=8192B). Gate: (a) task ≤2 step → nessun canvas, zero overhead token; (b) task ≥8 step → riduzione token tool-output in-contesto **≥30%** con **100%** recuperabilità degli offload; (c) canvas attivo non aumenta mai i token totali su task breve. `AURA_CANVAS_LLM_REFINE_ENABLED` resta `false` salvo offload-recovery-rate >0.15 su carico reale.
- [ ] **Invariante KV-cache (cross-ref Slice 4 / amendment #16)**: il canvas è iniettato nel tail dinamico `[3..N]`, **dopo il prefisso cacheable pinnato `[0..2]`** (amendment #29; mai nel prefisso). Acceptance: `scripts/cache_invariant_audit.sh` resta verde (SHA-256 del prefisso `[0..2]` invariato su 20 turni) con canvas attivo.
- [ ] Persistenza: `aura.task_canvas` + `aura.offload_entry` (migration Postgres `00NN_task_canvas`, numero ≠ 0007); `ON DELETE CASCADE` da `aura.conversations`. Down migration presente (Postgres è reversibile, a differenza di Cypher).
- [ ] Recupero agent-driven verificato: `node_id` → `offload_entry` → `tool_call_id` → `canvas.read_offload` **ri-deriva** il path via `sidecarPath` e ritorna il raw **anche in un processo/turn diverso** (`read_tool_output` è già durevole — amendment #28). Acceptance: simulare restart processo → recupero ancora funzionante.
- [ ] **Path-traversal guard `canvas.read_offload`**: **riusa `validateID`/`sidecarPath` esistenti** (`internal/agent/tools/result.go:45-71`), NON reinventa il guard — rifiuta `node_id`/`tool_call_id` con `..` o separatori; il path risolto resta sotto `<AURA_RUN_DIR>/conversations/<conv_id>/`. Acceptance: `tool_call_id="../../etc/passwd"` → hard-fail.

### File targets cumulativi

(Vedere "Architettura" sopra. Archival MVP 11a/11b/11d/11e: **~1450 LOC src + ~370 test + ~90 migration = ~1910 LOC** — 11c (~200 src + ~80 test) DEFERITA fuori MVP, amendment #27. **11f Task Canvas (sequencing-indipendente): ~400 LOC src + ~130 test + ~50 migration**, contabilizzato nella fase in cui atterra (~Phase 9-10), non nel totale Phase 15.)

### Open questions

1. **Chunk size 512 vs 1024 tokens** — **CHIUSA (amendment #29)**: ratificato il default `AURA_MEMORY_CHUNK_SIZE_TOKENS=512` (overlap 64, header-aware). Razionale: il target single-user (papers/note/doc/conversazioni via WRRF + LLM re-ranker) è dominato da retrieval factoid/Q&A, dove il consenso 2026 colloca lo sweet-spot precision/recall a 256–512 token (512–1024 serve solo a summarization multi-hop); 512 massimizza context precision riducendo il noise per chunk. Vincolo embedder: `embeddinggemma-300m` ha context window 2048 token → 512 tiene ~4 chunk in una finestra e non rischia troncamento su split che rispettano gli header markdown. Follow-on documentati (NON MVP): (a) chunk-size adattivo per doc-type (1024 per doc lunghi da summarizzare) solo se un benchmark RAGAS su corpus reale lo giustifica; (b) spike late-chunking (encode-then-split, +nDCG a parità di storage), fattibile dato il context 2k di EmbeddingGemma.
2. **Entity type taxonomy fissa vs dynamic** — **CHIUSA (amendment #29)**: ratificata **ibrida POLE+O** — set chiuso e queryable di label `type` ∈ {Person, Organization, Location, Concept, Event, Topic, **Other**} + proprietà `subtype` free-form (string, LLM-extracted, es. "Author", "Family", "Conference"). Il `type` fisso tiene index-backed le query e stabile il candidate-set HNSW; `subtype` + il bucket `Other` catturano la coda lunga senza esplosione di label. Modello da neo4j-agent-memory (POLE+O: `+O` = Object/Other catch-all), allineato alla prassi GraphRAG 2026 (estrazione LLM con schema vincolato-ma-flessibile batte le tassonomie rigide). Nessuna gerarchia di subtype in MVP: `subtype` è il seam forward-compatible.
3. **Re-ranker cost optimization** — **CHIUSA (amendment #29)**: LLM re-ranker (tier=worker, top-20→top-5, batch 4) **always-on di default per query interattive** via `AURA_MEMORY_RERANK_ENABLED=true`. Razionale: il re-rank su candidate set RRF rumoroso dà ~+35% accuracy vs hybrid grezzo e riduce le allucinazioni (pattern standard "RRF top-N → re-rank top-k"); arXiv:2506.00049 dimostra che small embedder + LLM re-rank su hybrid tri-modale (dense+sparse+graph = stack Aura) batte embedder più grandi. A ~$0.001/query × ~100 q/giorno = ~$3/mese il costo è trascurabile in interattivo. Mitigazioni costo bulk: (a) gate `AURA_MEMORY_RERANK_ENABLED=false` per batch-eval/ingest massivi; (b) confidence-skip quando i top-5 WRRF sono già ben separati; (c) escape hatch documentato: cross-encoder locale (es. `ms-marco-MiniLM-L-6-v2`, ~50ms/100 pair CPU, zero costo API, ospitabile sul sidecar llama.cpp) come follow-on se il volume cresce oltre single-user — NON default MVP.
4. **Memify prune threshold + hard-delete vs invalidate** — **CHIUSA (amendment #29)**: riconciliata con #24 ("invalidate, don't delete", principio Graphiti/Zep). Memify **NON fa hard `DETACH DELETE`** su `:Entity`: fa **soft-archive** — set `archived_at = datetime()` + invalida le edge correnti pendenti (`valid_to = datetime()` WHERE `valid_to IS NULL`). Le entity archiviate sono escluse da retrieval e candidate-set HNSW ma il subgrafo è preservato (query "cosa sapevo" + ri-attivazione su nuova mention). Default conservativo **180gg** (`AURA_MEMORY_PRUNE_IDLE_DAYS=180`, alzato da 90 — per un assistant single-user un'entità non menzionata da 3 mesi è normale, non garbage) + `< 3 mention` (`AURA_MEMORY_PRUNE_MIN_MENTIONS=3`), entrambi configurabili. L'unico hard `DETACH DELETE` resta la cleanup degli orphan da `:Document` cancellato (cascade §3807) — integrità referenziale, distinta dal decay di memoria.
5. **Agent insight injection** → **CHIUSA (amendment #29)**: ratificati `AURA_MEMORY_INSIGHT_TOP_K=3` (~500 token/turn) + `AURA_MEMORY_INSIGHT_RELEVANCE_THRESHOLD=0.7` (skip junk). Il top-K set (similarity > threshold, cap K, ordering deterministico) è cachato dall'LRU `:AgentInsight` (`AURA_AGENT_INSIGHT_CACHE_TTL_SEC=600`) keyed by `(identity_id, query_embedding_hash)` → `messages[2]` byte-stable entro la TTL → KV-cache hit sul terzo segmento di prefisso. Adaptive-K basato su query similarity resta enhancement futuro fuori MVP (romperebbe la byte-identity rendendo il blocco query-dipendente).
6. **Multi-user refactor cost** — **CHIUSA (amendment #29)**: ratificato **single-user-only per il milestone**, multi-user deferito (~+800 LOC accettati post-Slice-11). **Hedge cheap obbligatorio ora**: ogni nodo (`:Entity`/`:Document`/`:Chunk`/`:AgentEpisode`/`:AgentInsight`/`:UserConversation`) scrive `identity_id` (costante `'local'` in MVP) e ogni Cypher di lettura è scopato con `WHERE n.identity_id = $identity_id` da subito. Così il landing multi-user è **additivo** (aggiungi index su `identity_id` + popola ID reali + FK) e non una riscrittura di ogni query né una migrazione full-graph. Parità con lo scaffolding capability_grants di Slice 1.7.
7. **Bi-temporale pieno vs mono valid-time (amendment #24)** — **CHIUSA (amendment #29)**: ratificato **mono valid-time** (`valid_from`/`valid_to`) per MVP. Il failure mode dominante single-user è "fatto stale contraddetto da fatto nuovo" (preferenze/status che cambiano), coperto interamente dalla valid-time. Il secondo asse (transaction-time `t_created`/`t_expired`, stile Graphiti) serve solo audit/correzione-retroattiva ("cosa credevo alla data D"), non un bisogno MVP, e raddoppierebbe le proprietà temporali su ogni edge. Forward-compatible: aggiungere i 2 property transaction-time + un index è puramente additivo — nessun rewrite di nodi/edge. Atterra come slice futura solo su caso d'uso audit concreto.
8. **Task Canvas: builder deterministico vs LLM-refine (amendment #25)** — **CHIUSA (amendment #29)**: lo **skeleton deterministico SHIPPA** a ~Phase 9-10. Argomento chiave: il cascade NON è lossy — il raw è sempre su sidecar e recuperabile via `canvas.read_offload` (lo score riordina solo *cosa* evictare per primo, non cosa si perde), quindi anche un'euristica imperfetta è sicura. **Formula score deterministica** (no LLM): `score = w_size·norm(bytes/AURA_CANVAS_SCORE_SIZE_REF_BYTES) + w_recency·age_rank − w_reaccess·was_read_again`, con default `AURA_CANVAS_SCORE_W_SIZE=5`, `_W_RECENCY=3`, `_W_REACCESS=4` (un risultato ri-letto via read_tool_output è "ancora utile" → score più basso → evictato più tardi); `AURA_CANVAS_SCORE_SIZE_REF_BYTES=8192`. **Gate di accettazione misurabile**: (a) su task ≤2 step il canvas NON deve essere creato (zero overhead — già `AURA_CANVAS_MIN_TASK_STEPS=4`); (b) su task ≥8 step deve ridurre i token di tool-output in-contesto di **≥30%** mantenendo **100% recuperabilità** degli offload; (c) canvas attivo non deve MAI aumentare i token totali su un task breve. `AURA_CANVAS_LLM_REFINE_ENABLED` resta **`false`** finché, su carico Telegram reale, l'offload-recovery-rate (frazione di offload poi ri-letti dall'agente) non supera **0.15** — soglia oltre la quale l'eviction euristica sta sbagliando troppo spesso e il cascade LLM-scored ripaga il suo costo. Swarm (Phase 9): beneficia già dello skeleton (lo stato coordinatore parent↔child è topologia pura, non richiede summary LLM).

### Mini-PC RAM budget — delta

- Background goroutines (insight + memify; community DEFERITA, amendment #27): ~50 MB heap
- Neo4j heap: già allocato Slice 0.7 (~1.5-2 GB)
- aura-llama-embed: già allocato Slice 0.7 (~600 MB)
- LLM call (re-ranker, entity extract, summarize): no RAM extra (remote)

Net delta: **negligible**.

### Commit message templates (4 sub-slice archival MVP 11a/11b/11d/11e; 11c DEFERITA fuori MVP amendment #27; 11f Task Canvas committato nella fase in cui atterra)

```
slice 11a: memory schema Neo4j (taxonomy 3-tier 3-type Letta+mem0+industry)

Definisce schema Neo4j per memory architecture: labels + relations +
vector indexes + fulltext indexes + constraints. Niente impl, solo
schema + Cypher migration.

Pattern reference mix-and-match:
- Industry consensus 2026: 3 tipi cognitivi (episodic/semantic/procedural)
- Letta MemGPT: 3-tier storage (Core/Recall/Archival)
- mem0: hybrid retrieval (BM25 + vector + entity)
- Microsoft GraphRAG: community detection Leiden hierarchical
- Cognee: Memify post-processing (prune/strengthen/derive)

Labels user-side: :Document, :Chunk, :Entity (taxonomy fissa),
:Community (Leiden hierarchical), :UserConversation
Labels agent-side: :AgentEpisode, :AgentInsight

Vector index (768d cosine): chunk, entity, community, agent_insight
Fulltext index: chunk_text, entity_name

Cypher migration in internal/knowledge/migrations/0002_memory_schema.cypher
(path reale — vedi amendment #24). Forward-only: teardown via
`aura memory wipe-vectors --confirm` + re-apply (no down.cql).
Schema include valid-time su :RELATED_TO + :ReasoningTrace gated
(amendment #24).

Privacy: single-user mode (no isolation), identity 'local' default
su tutti node. Refactor multi-user accettato (+800 LOC stima).

LOC: +XXX migration / +YY doc.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

```
slice 11b: cognify ingestion pipeline (markitdown -> chunker -> entity)

internal/memory/ingest/ pipeline 6-stage (Cognee pattern):
classify -> permissions -> chunk -> entity LLM -> summary -> embed+commit.

Chunker recursive semantic (512 token, overlap 64, respect markdown
headers). Embedder batch 32 chunks via aura-llama-embed Slice 0.7.

Entity extractor mem0 2-fase: LLM tier=reasoning extract candidates
batch 10 chunks, conflict detection via fuzzy match + embedding
similarity > 0.92, MERGE existing OR CREATE new.

Type taxonomy POLE+O (amendment #29): Person/Org/Location/Concept/Event/Topic/Other + subtype free-form.

Tool LLM-facing Deferred:
- ingest.file(path)
- ingest.url(url)  (riusa Slice 5 web_fetch)
- ingest.text(content, source_name)

Telegram document handler auto-trigger ingest.file post-receive.

Migration 00NN_ingest_audit (Postgres, parity skill_audit Slice 7c;
numero assegnato all'atterraggio, ≠ 0007/0008/0009/0010/0012/0013).

Idempotent: content_hash pre-check, re-ingest stesso file = no-op.

Cost stima: $0.05/documento medio per LLM entity extraction.

LOC: +XXX src / +YY test / +ZZ migration.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

```
# slice 11c — DEFERITA fuori MVP (amendment #27). Template conservato come
# riferimento per quando atterrerà come feature lazy/on-demand. NON committare in Phase 15.
slice 11c: community detection Leiden + summarization (GraphRAG pattern)

Trigger on-demand (NON background schedulato) chiama gds.leiden.stream()
su entity graph weighted by RELATED_TO.weight. Hierarchical clustering
produce N community level 0 (leaf), N/5 level 1, etc.

Per community: LLM tier=worker summarize membri (max 10 entity).
Persist :Community node + :IN_COMMUNITY + :CONTAINS relations
(schema già provisioned in 11a).

Community embedding via aura-llama-embed (Slice 0.7) per global
search retrieval (memory.search scope=global).

Cost stima: $0.10/run per 100 entities, on-demand (NON quotidiana).

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

```
slice 11d: retrieval hybrid (BM25 + HNSW + graph) + LLM re-ranker

internal/memory/retrieval/ hybrid search:
1. BM25 fulltext via Neo4j queryNodes('chunk_text', q)
2. HNSW vector queryNodes('chunk_embedding', 20, q_embedding)
3. Graph 1-hop expansion via :MENTIONS Entity (rank by co-mention count)
Fusione = Weighted-RRF Cypher-native (amendment #26): per sorgente
contribution = sourceWeight / (rrfConstant + rank), sommato cross-source,
ORDER BY total DESC. rrfConstant=60, sourceWeights vector/fulltext/graph=1.0,
sourceK=20, finalK=5. Rank-based scale-free, no normalizzazione.
(Fusione lineare 0.4/0.4/0.2 = fallback deprecato.)

LLM re-ranker tier=worker top-20 -> top-5, batch 4 chunks per call.
Cost ~$0.001/query.

Tool LLM-facing Deferred:
- memory.search(query, scope=local|global, k=5)
- memory.recall(entity_name|entity_id)
- memory.forget(doc_id|entity_id|chunk_id)  GDPR cascade + audit
- memory.summarize_conversation(conv_id)

Global search (GraphRAG): scope=global -> top-K :Community summaries
(invece di chunks). Per query "general knowledge across dataset".
DEFERITO con 11c (amendment #27): in MVP solo scope=local.

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

```
slice 11e: agent journal + Memify post-processing (Cognee pattern)

internal/memory/agent/:
- journal.go: post-conv trigger -> :AgentEpisode summary LLM-generated
- insight.go: cross-conv analyzer ogni 60min, cluster episode embedding
  -> :AgentInsight + :LEARNED relation. Inject top-3 nel system prompt
  pre-conv (cache-friendly come Agent.md Slice 10).

internal/memory/graph/memify.go: Cognee Memify pipeline background 24h:
- Soft-archive stale entities (amendment #29: last_mentioned_at < 180gg AND mention_count < 3 → archived_at + invalida edge correnti, NO hard DETACH DELETE; riconciliato con #24)
- Strengthen RELATED_TO weight via co-occurrence count
- Derive facts: multi-hop traversal weight > 0.7 -> :DERIVED_FROM

Audit log per ogni operazione Memify in memify_audit table.

Env nuove (Caps & Limits indice):
- AURA_MEMORY_CHUNK_SIZE_TOKENS=512
- AURA_MEMORY_CHUNK_OVERLAP_TOKENS=64
- AURA_MEMORY_INSIGHT_INTERVAL_MIN=60
- AURA_MEMORY_MEMIFY_INTERVAL_HR=24
- AURA_MEMORY_INSIGHT_TOP_K=3
- AURA_MEMORY_INSIGHT_RELEVANCE_THRESHOLD=0.7
- AURA_MEMORY_PRUNE_IDLE_DAYS=180
- AURA_MEMORY_PRUNE_MIN_MENTIONS=3

LOC: +XXX src / +YY test.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Slice 13 — Local LLM fallback (vLLM + LMCache disk-tier, doppio sidecar)

> **Pattern doppio sidecar** (decisione 2026-05-28):
> - `aura-llama-multimodal` (Slice 9c) **invariato**: llama.cpp + Gemma 4 E4B Q4 per vision/STT one-shot (lightweight, mature multimodal).
> - `aura-vllm-chat` **NUOVO**: vLLM serving + LMCache disk-tier per chat fallback offline/privacy. Modello chat-only (no multimodal duplicato).
>
> **Pattern reference**: LMCache ([github.com/LMCache/LMCache](https://github.com/LMCache/LMCache), 8.4k stars, Apache 2.0, prod in Google Cloud / GMI / CoreWeave). KV cache layer disk-tier per vLLM v1+, 3-10x TTFT reduction su long-context.

**Goal.** Aura può scegliere il backend LLM per ogni conversation:
1. **OpenRouter remote** (default, DeepSeek-V4 via Slice 1)
2. **vLLM local + LMCache** (chat fallback offline / privacy mode / cost cap)
3. **Gemma 4 multimodal local** (vision/STT one-shot via Slice 9c, invariato)

Trigger switching automatic e/o esplicito:
- **Conv flag** `prefer_local=true` (esplicito user/agent decision via `aura chat new --local` o `/local` Telegram)
- **Offline detection** (TCP probe verso `AURA_LLM_BASE_URL` ogni `AURA_LLM_OFFLINE_DETECTION_INTERVAL_SEC=30`, switch a local se fail consecutive)
- **Cost threshold** (cost cumulativo daily > `AURA_LLM_LOCAL_FALLBACK_COST_USD_DAY=1.0` → switch silent + Notifier alert)
- **Identity capability** `use_local_llm` (Slice 1.7 capability_grants, default off per single-user `local`, scaffolding pre-built per multi-user)

### Pre-requisiti

- Slice 1 LLM client OpenAI-compat (riusato drop-in per vLLM)
- Slice 0.5 Postgres (per `local_llm_sessions` table)
- Slice 1.7 identities (per capability `use_local_llm`)
- Slice 1.8 conversations (per `prefer_local` flag in metadata)
- Slice 9c multimodal sidecar (per coesistenza, no conflitto)

### ⚠️ Open question CRITICA pre-merge — vLLM CPU vs GPU

**vLLM è ottimizzato per CUDA GPU**. Su mini-PC senza GPU dedicata:
- **vLLM CPU mode**: latenza ~5-10x peggiore di llama.cpp CPU per stesso modello
- **vLLM GPU (RTX 4060 8GB+)**: latenza eccellente, RAM offload OK
- **llama.cpp CPU**: balance ottimale per CPU-only, supporto AVX2/AVX512 maturo

**Decisione pre-merge richiesta**: il mini-PC target ha GPU dedicata?
- **SE GPU**: Slice 13 vLLM+LMCache OK, procediamo
- **SE CPU-only**: opzione alternativa **13-bis** = riusare `aura-llama-multimodal` (llama.cpp E4B Q4) anche per chat fallback, no nuovo sidecar, no LMCache (incompatibile native). Saving RAM ~5 GB, perdita scalability KV cache cross-session. Pattern: 1 sidecar serve sia multimodal sia chat fallback.

### Decisioni cumulate (chiusura 2026-05-28)

| Aspetto | Decisione |
|---|---|
| Sidecar architecture | **Doppio sidecar**: `aura-llama-multimodal` (Slice 9c, vision/STT) + `aura-vllm-chat` (Slice 13, chat fallback). |
| Modello chat fallback | **Default proposto**: Gemma 3 12B Instruct Q5_K_M (~7 GB RAM, multilingual IT/EN/ES eccellente). Alternative: Llama 3.1 8B (~5 GB), Qwen 2.5 7B (~4.5 GB). Decisione finale pre-merge via benchmark. |
| KV cache layer | **LMCache disk-tier** (`/var/cache/lmcache/`, max 50 GB su NVMe). vLLM integration via `--kv-transfer-config`. |
| Switching policy | 4 trigger: conv flag explicit, offline detection 30s, cost threshold $1/day, identity capability. |
| Routing default | Sempre remote tranne se trigger attivo. Nessun auto-prefer-local senza signal. |
| Cost accounting | Local LLM cost = 0 in `aura.local_llm_cost`. Remote cost via OpenRouter usage API. Threshold check via aggregate. |
| Conversation persistence | `prefer_local` field in `aura.conversations.metadata jsonb` (Slice 1.8). Conversation continua su stesso backend (no switch mid-conversation). |

### Architettura componenti

```
internal/llm/
  router.go               # ~150  LLMRouter struct:
                          #         remoteClient *openai_compat.Client (Slice 1)
                          #         localClient  *openai_compat.Client (vLLM endpoint)
                          #         offlineDetector + costTracker
                          #       Route(ctx InvocationContext) *Client per call
                          #       Switching logic per priorita':
                          #         0. AURA_PRIVACY_MODE=local-only -> local (amendment #33,
                          #            forza locale; il branch remoto diventa irraggiungibile)
                          #         1. conv.metadata.prefer_local=true -> local
                          #         2. offline detection consecutive_fails>=3 -> local
                          #         3. costTracker.DailyTotal() > threshold -> local
                          #         4. capability check use_local_llm
                          #         (default) -> remote
  offline_detector.go     # ~80   Background goroutine: TCP dial AURA_LLM_BASE_URL
                          #       ogni 30s, exponential backoff, consecutive_fails counter
                          #       Emit STATE_DELTA event "online_status" via AG-UI (Slice 8)
  cost_tracker.go         # ~100  sqlc adapter su aura.local_llm_cost
                          #       OnCallStart: INSERT row con start_ts
                          #       OnCallEnd: UPDATE end_ts + total_cost_usd (da OpenRouter
                          #         usage response, NULL se local)
                          #       DailyTotal(): SUM WHERE ts >= now()-24h
internal/db/queries/local_llm_cost.sql          # ~30   3 query sqlc
internal/db/queries/local_llm_sessions.sql      # ~30   3 query sqlc (analoghe a sandbox_sessions)
internal/db/migrations/0013_local_llm.up.sql    # ~50
internal/db/migrations/0013_local_llm.down.sql  # ~3

internal/agent/llm_agent.go (diff)              # ~+30
  - Sostituisce field client *Client con router *LLMRouter
  - Per call: client := router.Route(ctx); client.Stream(...)

cmd/aura/main.go (diff)                          # ~+50
  + aura chat new --local                  # forza prefer_local=true su new conv
  + aura llm-router status                 # mostra current routing + offline state + cost
  + aura llm-router cost --today           # daily cost breakdown remote vs local

internal/channels/telegram/commands.go (diff)    # ~+40
  + /local             # switch corrente conv a local
  + /remote            # switch a remote (reset prefer_local)
  + /llm-status        # mostra routing + offline + cost

compose.yaml (diff)                              # ~+50
  Service aura-vllm-chat:
    image: vllm/vllm-openai:latest
    command:
      - --model gemma-3-12b-it
      - --port 8083
      - --host 127.0.0.1
      - --kv-transfer-config '{"kv_connector":"LMCacheConnectorV1",
                              "kv_role":"kv_both"}'
      - --max-model-len 8192
      - --gpu-memory-utilization 0.85   # se GPU
    environment:
      - LMCACHE_CONFIG_FILE=/etc/lmcache.yaml
    volumes:
      - lmcache-data:/var/cache/lmcache
      - ./lmcache.yaml:/etc/lmcache.yaml:ro
    ports:
      - "127.0.0.1:8083:8083"
    deploy:
      resources:
        reservations:
          devices:
            - capabilities: [gpu]        # se GPU available
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8083/v1/models"]

lmcache.yaml                                     # ~25
  local_storage:
    type: disk
    path: /var/cache/lmcache
    max_size_gb: 50
  chunk_size: 256                                # token granularity
  enable_blending: false                          # single-instance, no P2P
  enable_async_save: true                         # background flush to disk
```

### Migration 0013

```sql
CREATE TABLE aura.local_llm_sessions (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  conversation_id text NOT NULL REFERENCES aura.conversations(id) ON DELETE CASCADE,
  model           text NOT NULL,                  -- e.g. 'gemma-3-12b-it'
  backend         text NOT NULL CHECK (backend IN ('vllm', 'llama_cpp_fallback')),
  started_at      timestamptz NOT NULL DEFAULT now(),
  last_used_at    timestamptz NOT NULL DEFAULT now(),
  ended_at        timestamptz NULL,
  kv_cache_hits   bigint NOT NULL DEFAULT 0,
  kv_cache_misses bigint NOT NULL DEFAULT 0
);
CREATE INDEX local_llm_sessions_conv ON aura.local_llm_sessions(conversation_id, started_at DESC);

CREATE TABLE aura.local_llm_cost (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  conversation_id text NOT NULL REFERENCES aura.conversations(id) ON DELETE CASCADE,
  identity_id     uuid NOT NULL REFERENCES aura.identities(id),
  backend         text NOT NULL CHECK (backend IN ('remote_openrouter', 'local_vllm', 'local_llama_cpp')),
  model           text NOT NULL,
  started_at      timestamptz NOT NULL DEFAULT now(),
  ended_at        timestamptz NULL,
  total_cost_usd  numeric(10, 6) NOT NULL DEFAULT 0,
  tokens_in       int NOT NULL DEFAULT 0,
  tokens_out      int NOT NULL DEFAULT 0
);
CREATE INDEX local_llm_cost_daily ON aura.local_llm_cost(identity_id, started_at DESC)
  WHERE ended_at IS NOT NULL;

-- Slice 1.8 conversations metadata aggiunge field implicito (jsonb):
-- ALTER TABLE aura.conversations ALTER COLUMN metadata SET DEFAULT '{"prefer_local": false}';
-- prefer_local è una key dentro metadata jsonb esistente, no schema change.
```

### Smoke

```bash
# Setup: GPU mini-PC (NVIDIA RTX 4060 8GB+), Slice 9c attivo
docker compose up -d aura-vllm-chat
# attendi healthcheck verde (~30-60s warmup)

# Explicit local
conv=$(aura chat new --local)
aura chat resume $conv "ciao, dimmi 2+2 senza usare internet"
# → routing detecta prefer_local=true → call a vllm@8083
# → response da Gemma 3 12B local
# → INSERT aura.local_llm_cost (backend=local_vllm, cost_usd=0)

# Offline detection
sudo iptables -A OUTPUT -d 1.1.1.1 -j DROP   # simula offline
aura chat "domanda"
# → offline_detector consecutive_fails=3 → switch local
# → bot risponde via local LLM
# → STATE_DELTA event 'offline' broadcast su AG-UI / Telegram

# Cost threshold (su giornata di uso intenso remoto)
# costTracker.DailyTotal() > $1.0 → auto-switch silent
# → Notifier: "Limit cost giornaliero superato, switching a local LLM"

# Telegram commands
# user: /local
# → bot: "✅ Switch a local LLM Gemma 3 12B (no internet, no cost)"
# user: /llm-status
# → bot: "Current: local (vllm). Offline: NO. Cost today: $0.42 remote + $0 local."
```

### Acceptance

- [ ] `internal/llm/router.go` `LLMRouter` con 4 trigger di switching documented + priority order.
- [ ] `offline_detector.go` TCP dial ogni 30s + exponential backoff + consecutive_fails counter. Emette `STATE_DELTA` event `online_status` su AG-UI emitter (Slice 8 fanout).
- [ ] `cost_tracker.go` ON CALL START/END per ogni call LLM. `DailyTotal(identity_id)` per threshold check. Reset rolling 24h.
- [ ] Migration 0013: `aura.local_llm_sessions` + `aura.local_llm_cost`. Cascade su conversation delete (Slice 1.8).
- [ ] `compose.yaml` aggiunge `aura-vllm-chat` service con LMCache integration. Healthcheck verifica `/v1/models`.
- [ ] `lmcache.yaml` config disk-tier 50 GB, chunk_size 256, async_save.
- [ ] Telegram commands `/local`, `/remote`, `/llm-status` (bot-intercept come gli altri commands Slice 9b).
- [ ] CLI `aura chat new --local`, `aura llm-router status`, `aura llm-router cost --today`.
- [ ] Test routing priority: ogni trigger combinazione produce backend atteso (matrice).
- [ ] Test offline_detector: simula network drop con `iptables` → switch entro 90s (3x 30s).
- [ ] Test cost_tracker: 10 call con cost simulato → `DailyTotal` rolling 24h corretto.
- [ ] **Pre-merge benchmark CRITICO**: latency p50/p95 + tokens/sec vLLM Gemma 3 12B Q5 vs llama.cpp Gemma 4 E4B Q4 su prompt 1000-token. **SE vLLM CPU < 5 tokens/sec → switch a 13-bis (riusa llama.cpp E4B per chat fallback, no LMCache, save sidecar)**.

### Open questions

1. **CRITICA — GPU vs CPU**: vedi sezione sopra. Default vLLM assume GPU; CPU mode richiede benchmark + possibile alternative 13-bis.
2. **Modello fallback**: Gemma 3 12B vs Llama 3.1 8B vs Qwen 2.5 7B. Pre-merge benchmark su corpus IT (preferenza utente italiana) + EN code. Quality / size trade-off.
3. **LMCache config tuning**: `chunk_size` 256 default, ma per chat fallback (max_model_len 8192) potrebbe essere meglio 512. Test post-merge.
4. **Cost threshold default**: $1/day è ragionevole per single-user MVP? Configurable. Future: per-identity threshold.
5. **STATE_DELTA event reactive**: quando offline → switch, l'agente deve emettere messaggio proattivo all'utente Telegram? Default proposto: SÌ via Notifier alert (parity Slice 6 risk recovery).

### Alternative path: Slice 13-bis (CPU-only mini-PC)

Se benchmark pre-merge mostra vLLM CPU < 5 tokens/sec:

```
internal/llm/router.go  invariato (4 trigger logic uguale)
Backend local = riusa `aura-llama-multimodal` Slice 9c per chat
   (llama.cpp gestisce gia' /v1/chat/completions)
NESSUN nuovo sidecar, NESSUN LMCache (non native llama.cpp).
RAM saving: -7 GB (no Gemma 12B Q5)
KV cache: solo nativo llama.cpp (prefix caching + slot context)
Compose: solo flag passthrough a aura-llama-multimodal
Migration 0013 invariato (backend enum include 'local_llama_cpp_fallback')
```

LOC 13-bis: ~250 (vs 400 di 13 vLLM). Decisione pre-merge.

### Mini-PC RAM budget — delta

**Scenario A — Slice 13 con vLLM GPU**: vLLM Gemma 3 12B Q5 su GPU = ~7 GB VRAM, +1 GB RAM overhead. CPU+RAM totale invariato vs Slice 9c (sidecar separato carica solo VRAM). LMCache disk-tier: 50 GB NVMe.

**Scenario B — Slice 13 con vLLM CPU**: +7 GB RAM (modello in CPU). Tabella mini-PC `Peak realistic 7 GB → ~14 GB`. Su 32 GB OK, su 16 GB tight.

**Scenario C — Slice 13-bis (riusa llama.cpp E4B)**: invariato vs Slice 9c. Zero overhead nuovo.

### Commit message templates (13a + 13b)

```
slice 13a: LLM router + offline detection + cost tracking

internal/llm/router.go con 4 trigger priority order:
  1. conv.metadata.prefer_local=true (explicit)
  2. offline_detector consecutive_fails >= 3 (TCP probe 30s)
  3. cost_tracker.DailyTotal() > AURA_LLM_LOCAL_FALLBACK_COST_USD_DAY
  4. identity capability use_local_llm

Migration 0013_local_llm: aura.local_llm_sessions +
aura.local_llm_cost (CASCADE conv).

CLI: aura chat new --local, aura llm-router {status|cost --today}
Telegram: /local /remote /llm-status (bot-intercept)

offline_detector emette STATE_DELTA online_status su AG-UI Slice 8.
cost_tracker rolling 24h aggregate per identity.

No sidecar nuovo in 13a (vLLM atterra in 13b). Router puntando solo a
remote inizialmente, local backend = nil (skip routing local-path
finche' 13b non lo collega).

LOC: +XXX src / +YY test / +ZZ migration.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

```
slice 13b: vLLM chat sidecar + LMCache disk-tier

Aggiunge aura-vllm-chat sidecar con LMCache disk-tier 50 GB. Pattern
reference LMCache (Apache 2.0, 8.4k stars, prod GCP/GMI/CoreWeave).

compose.yaml service aura-vllm-chat:
  image vllm/vllm-openai:latest
  model: gemma-3-12b-it (default, override via env)
  --kv-transfer-config LMCacheConnectorV1
  port 127.0.0.1:8083
  GPU device passthrough se available

lmcache.yaml config:
  local_storage type=disk path=/var/cache/lmcache max_size_gb=50
  chunk_size=256 (tunable per long-context)
  enable_async_save=true

Router config: localClient ora punta a 127.0.0.1:8083, attivato
quando trigger 13a route a local.

PRE-MERGE BENCHMARK CRITICO:
  vLLM Gemma 3 12B Q5 latency p50/p95 + tokens/sec su prompt 1000-tok
  vs llama.cpp Gemma 4 E4B Q4 baseline.
  SE vLLM CPU < 5 tokens/sec -> SCRAP 13b, attiva 13-bis path
    (riusa aura-llama-multimodal Slice 9c come chat fallback,
     no nuovo sidecar, no LMCache).

Env nuove (Caps & Limits indice):
- AURA_LLM_LOCAL_BASE_URL=http://aura-vllm-chat:8083/v1
- AURA_LLM_LOCAL_MODEL=gemma-3-12b-it
- AURA_LLM_OFFLINE_DETECTION_INTERVAL_SEC=30
- AURA_LLM_LOCAL_FALLBACK_COST_USD_DAY=1.0
- LMCACHE_LOCAL_DISK_PATH=/var/cache/lmcache
- LMCACHE_MAX_LOCAL_DISK_GB=50

Mini-PC RAM delta:
- Scenario GPU: +1 GB RAM overhead + 7 GB VRAM (su GPU dedicata)
- Scenario CPU: +7 GB RAM (tight su 16 GB, OK su 32 GB)
- Scenario 13-bis: invariato (zero nuovo sidecar)

LOC: +XXX src / +YY test / +ZZ config.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Slice 14 — Packaging & Distribution (fat image + curl|sh installer + appliance)

> **Amendment #47 / OPS-01 (Phase 17, 2026-06-05): Aura packaging end-user.** Design discussion 2026-06-05 (4 decisioni utente convergute). Aura oggi si distribuisce come binario statico goreleaser + `compose.yaml` per i sidecar, ma il "single static binary" è una promessa rotta nel momento in cui serve MCP: `mcp-neo4j-cypher` è un subprocess Python che `internal/knowledge/client.go` spawna via PATH host (`pip install mcp-neo4j-cypher==0.6.0` — non documentato nel README), e le recipe MCP generiche (calculator/mail) vogliono `uvx`/`npx` host. Per l'end-user (appliance DGX Spark + self-host) il requisito host deve collassare a **solo Docker**.

**Decisione architettonica (utente, 2026-06-05):**
- **D-OPS-01 — Fat image unica.** Aura diventa un'immagine container (`docker/aura/Dockerfile`, multi-stage: builder `golang` → runtime `python:3.x-slim` + node + `uv`/`uvx` + `npx` + `pip install mcp-neo4j-cypher==0.6.0` pinned → COPY del binario `aura`). I subprocess MCP girano DENTRO il container dove i runtime esistono: `internal/knowledge/client.go` resta INVARIATO (stdio transport, nessun cambio al layer MCP — il lavoro è tutto packaging). L'host non richiede più Python/Node/pip/PATH surgery.
- **D-OPS-02 — Due porte, una source of truth.** (a) **Self-host**: `curl -fsSL <url>/install.sh | sh` — verifica/installa Docker, fetch `compose.yaml` + `.env`, genera i secret, `docker compose up`, stampa l'URL+token del wizard. (b) **Appliance**: immagine pre-pullata + `.env` pre-seeded + systemd `compose up` al boot (salta lo step curl). Stesso `compose.yaml`, stessa immagine.
- **D-OPS-03 — Secret generation first-run.** `install.sh` genera `POSTGRES_PASSWORD`/`NEO4J_PASSWORD` (`openssl rand`, `.env` chmod 600), idempotente (non rigenera se `.env` esiste). L'utente non edita mai `.env` a mano per i secret DB.
- **D-OPS-04 — API key both/either + rilassamento D-22.** `OPENROUTER_API_KEY` è l'unico secret non auto-generabile: l'installer lo accetta non-interattivo (env/flag) se fornito, altrimenti il **setup wizard (Slice 9a / Phase 13)** lo raccoglie più tardi scrivendo `~/.aura/llm.json`. Richiede il **rilassamento del fail-fast D-22**: `aura serve` deve bootare keyless (oggi `config.Load()` aborta su `AURA_LLM_API_KEY` vuoto, `internal/config`); senza key una chiamata agente ritorna un result strutturato `{"error":"llm_not_configured", "hint":...}` (fail-closed, stesso pattern di `SEARXNG_URL` vuoto in Slice 5), NON un boot abort. `aura db migrate` (`LoadDB()`) già salta LLM, invariato.
- **D-OPS-05 — Aura come servizio compose.** `compose.yaml` guadagna `aura` (`depends_on` healthcheck pg/neo4j/embed) + un one-shot `aura-migrate` (`db migrate && neo4j migrate`, exit 0, `aura` `depends_on: service_completed_successfully`). Volume persistente `aura-home → ~/.aura` (`llm.json`, `mcp/servers.json`, `agents/`, `pyscripts/`) sopravvive agli upgrade immagine.
- **D-OPS-06 — Distribuzione.** Immagine pubblicata su `ghcr.io` via goreleaser, pinnata per release tag; `install.sh` hostato in-repo (raw URL). Il binario host goreleaser RESTA per lo sviluppo (WSL `go run`); il container è SOLO l'artefatto di distribuzione end-user.

**Acceptance (high-level — `17-SPEC.md` li rende falsificabili via Socratic loop).**
- [ ] `curl -fsSL <url>/install.sh | sh` su host Docker pulito → stack su (postgres+neo4j+embed+searxng+sandbox-agent+aura), `.env` chmod-600 con secret auto-generati, migrazioni applicate, URL+token wizard stampato; ZERO Python/Node/pip sull'host.
- [ ] `mcp-neo4j-cypher` spawna DENTRO il container `aura` (python3 + package pinned presenti in-image); l'host NON ha `mcp-neo4j-cypher` su PATH.
- [ ] `docker compose up` senza `OPENROUTER_API_KEY` → `aura serve` bootta (no fail-fast); chiamata agente keyless → result `llm_not_configured` (fail-closed); dopo key (flag installer O wizard) → chat funziona.
- [ ] Re-run installer idempotente (secret `.env` preservati, non rigenerati); volume `aura-home` sopravvive a un upgrade immagine.
- [ ] Immagine su `ghcr.io` pinnata per tag; goreleaser produce ancora il binario host per dev; appliance = stesso compose + immagine con `.env` pre-seeded (no step curl).

**File targets** (≤ 600 LOC src, no file >300):
| Path | LOC | Note |
|---|---|---|
| `docker/aura/Dockerfile` | ~50 | Multi-stage: golang builder → python:3.x-slim + node + uv/uvx + npx + `pip install mcp-neo4j-cypher==0.6.0` + COPY aura. Layer-order cache-stable (rebuild cold 45-60min, preserva cache). |
| `compose.yaml` (diff) | ~+30 | service `aura` + one-shot `aura-migrate` + volume `aura-home`; depends_on healthcheck pg/neo4j/embed. |
| `scripts/install.sh` | ~120 | Docker check/install + fetch compose+.env + secret-gen (openssl, chmod 600, idempotente) + `OPENROUTER_API_KEY` opt-in (env/flag) + compose up + stampa wizard URL+token. |
| `internal/config/config.go` (diff) | ~+10 | D-OPS-04: rilassa il fail-fast empty-key in `Load()` (serve boots keyless); il path agente fail-closa al call con `llm_not_configured`. |
| `.goreleaser.yaml` (diff) | ~+15 | ghcr.io image build/push pinnato per tag (oltre ai binari host esistenti, che restano). |
| `README.md` (diff) | ~+20 | Quick start end-user: curl\|sh + appliance; rimuove lo step `pip install mcp-neo4j-cypher` host non documentato. |

**Boundaries.**
- **In scope**: fat image, compose service + one-shot migrate, installer self-host, secret-gen, D-22 relaxation, ghcr publish, README end-user.
- **Out of scope**: il **setup wizard web** (è Slice 9a / Phase 13 — questa slice ne *dipende* per la porta API-key-via-wizard, non lo costruisce); onboarding Telegram (Phase 14); marketplace / auto-update immagine in-band; orchestrazione k8s/helm; il binario host goreleaser (resta, NON sostituito — dev path).

**Dipende da**: Phase 13 (setup wizard per la porta deferred-key) per la storia completa; l'installer + fat image da soli richiedono solo il daemon `aura serve` (Phase 10 ✓).

**Env.** Nessuna nuova `AURA_*` runtime var: il volume `aura-home` riusa `AURA_CONFIG_DIR=~/.aura`. L'unico cambio comportamentale è il rilassamento del fail-fast empty-key (D-OPS-04, rilassa D-22) sul path `aura serve`.

**Commit message template.**

```
slice 14: packaging & distribution (fat image + curl|sh installer)

Single fat Aura container image (Go binary + python/uvx + node/npx +
pinned mcp-neo4j-cypher) so the host needs only Docker — MCP subprocesses
spawn inside the image, internal/knowledge/client.go unchanged. compose
gains aura + one-shot aura-migrate + aura-home volume. install.sh self-host
door (Docker check + secret-gen + compose up + wizard URL); appliance door
= same compose+image pre-seeded. Relaxes D-22 empty-key fail-fast (serve
boots keyless, agent call fail-closes llm_not_configured) so the wizard can
collect the key later. Image to ghcr.io pinned per tag; goreleaser host
binary retained for dev.

LOC: +XXX infra / +YY script / +ZZ doc.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
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

> **Home-slice del modulo `internal/scoring/` (amendment #41, D-11): Phase 8 / Slice 2b.** Il modulo condiviso `internal/scoring/` (puro, no DB: `RiskTier` enum + `ComputeTaskTier`/`ComputeSkillTier`/`ComputeSandboxTier` + `GateRecommended` + `RequiresImmediateAlert` + tabella modificatori + `AURA_RISK_ALERT_THRESHOLD`) viene **anticipato e shippato per intero in Phase 8** (era schedulato con Slice 6/Scheduler). Scheduler (P10) e Skills (P11) lo **consumano** senza ri-costruirlo. **Scope guard (D-12):** Phase 8 costruisce e unit-testa il MODULO, NON le pipeline applicative per-slice — wira SOLO il path advisory sandbox (`ComputeSandboxTier(args)` → `network_allow` empty=SAFE, `pypi.org`-only=SAFE-bump, domains arbitrari=RISKY/IRREVERSIBLE → tool result porta `{risk_tier, gate_recommended}`, gate **advisory** per "gate consigliato", nessuna persistenza pending-state per la sandbox in v1). Il `pending_approval` dello Scheduler, le colonne audit di `agent_job_runs` e la pending dir delle Skills restano P10/P11.

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
agent_job tier=reasoning (Area #14)      → +1 tier   # tier reasoning = costo LLM alto
agent_job tier=worker                    → 0  (no bump)
agent_job tier=chat                      → 0  (no bump)
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
       → resume handler chiama task approve / skill Writer.Activate (NO model-facing
         skill approve action, D-03) / cancellation
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

AURA_WEB_FETCH_MAX_BODY_BYTES = 5000000  (5 MB; era AURA_WEB_RESPONSE_CAP_BYTES = 24000)
  Raw HTTP response body download ceiling di web_fetch (Slice 5), DoS guard.
  Anti-DOS: protegge dal caricare HTML di N MiB in memoria; applicato in
  gateAndRead via io.LimitReader(cap+1) PRIMA dell'estrazione readability.
  Diversa semantica: è il ceiling del body HTTP grezzo, NON il preview-to-context.
  Il markdown LLM-facing (preview + spillover sidecar) è governato da
  AURA_CONTEXT_PREVIEW_CAP_BYTES via tools.NewResult, NON da questo campo.
  Rinominato + ri-defaultato a 5 MB 2026-06-02: il vecchio default 24KB
  (dimensionato come un numero markdown) rigettava ogni pagina reale.

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

AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS = 10  (Area #17 closed 2026-05-28)
  Tool result più vecchi di N turn nel context window vengono sostituiti
  con un puntatore "[evicted, re-fetch via read_tool_output(X)]" in
  LoadHistory(). Riusa sidecar di Slice 1, niente LLM call.
  Pattern Cursor "dynamic context discovery".

AURA_CONTEXT_RESERVE_TOKENS = 13000  (Area #17 closed 2026-05-28)
AURA_CONTEXT_MAX_OUTPUT_TOKENS = 20000
  Formula Claude Code per calcolare il context budget effettivo:
    hard_cap = Model.ContextWindow - max(MaxOutputTokens, AURA_CONTEXT_MAX_OUTPUT_TOKENS) - AURA_CONTEXT_RESERVE_TOKENS
    warn_cap = hard_cap * 0.75
  DeepSeek-V4 (1M context): ~967K hard / 725K warn.
  OpenAI/Anthropic (200K): ~167K hard / 125K warn.
  Sopra warn: log WARN. Sopra hard: errore esplicito Loop.Turn (use
  chat_compact tool o aura chat new).

AURA_TELEGRAM_STATUS_THROTTLE_MS = 1500  (Slice 9b, Punto 3 closed 2026-05-28)
AURA_TELEGRAM_CONTENT_THROTTLE_MS = 500
AURA_TELEGRAM_CHAT_RATE_LIMIT_MS = 1000
  Telegram bot rate limit handling. Throttle differenziato per pane:
  - Status pane (info di servizio, lento): edit ogni 1500ms accumulato.
  - Content streaming (token assistant): edit ogni 500ms accumulato.
  - Sopra entrambi: chat queue serializzata 1000ms (rispetta Telegram
    1 msg/sec per chat hard limit). Se 2 pane flush simultaneo, queue
    serializza al rate hard.
  429 backoff adaptive: parse retry_after header, exponential up to 30s.

AURA_TELEGRAM_DOC_SYNC_MAX_BYTES = 5242880   (5 MiB, Slice 9c, Punto 6)
AURA_TELEGRAM_DOC_ASYNC_MAX_BYTES = 52428800 (50 MiB = Telegram hard cap)
  Tiered document handling via markitdown sidecar:
  - <=SYNC: convert sync HTTP timeout 30s, no placeholder message.
  - SYNC..ASYNC: async background goroutine + "📄 Convertendo..."
    placeholder + edit a done.
  - >ASYNC: refuse con UX message (anche Telegram impone 50 MB).

AURA_SETUP_BIND = 127.0.0.1:9081  (Slice 9a, sempre on)
  Setup wizard HTTP server (paste bot token + onboard QR). Default
  loopback. Override AURA_SETUP_BIND=0.0.0.0:9081 per setup remoto con
  QR scan da phone su LAN (no auth, headless container scenario).

AURA_PROFILE_CERTAINTY_N = 3  (Slice 10, onboarding auto-update)
  Numero di osservazioni consistent richieste per auto-add silenzioso
  di un fatto a Agent.md. Sotto soglia (1-2 osservazioni) -> ask_user
  approval gate via Risk-Based pipeline (sezione cross-cutting). Hybrid pattern:
  fatti certi auto, incerti gate.

AURA_PROFILE_DIR = ~/.aura/agents  (Slice 10, default)
  Directory base per i profili per identity. Subdir <identity_id>/
  contiene Agent.md + preferences.json + metadata.json + changelog.md.
  Atomic write (temp + Rename) per evitare corruption su crash mid-update.
```

### Indice completo env vars (`AURA_*` + sidecar)

Tabella di tutte le environment variables citate nel PRD, slice di provenance, default, e se sono "caps & limits" (sopra) o "operative" (config/secret/path).

| Env var | Default | Tipo | Slice | Note |
|---|---|---|---|---|
| `AURA_DB_URL` | (richiesto) | operative | 0.5 | Postgres DSN. |
| `AURA_DB_MIGRATE_URL` | (optional, defaults to `AURA_DB_URL`) | secret | 0.5 | Migrations-only DSN connecting as `aura_migrate` role (amendment #17, Pitfall #6 P0). If unset, `aura db migrate` fails fast. Production runtime never uses this URL — only `aura db migrate` subcommand reads it. |
| `AURA_LLM_BASE_URL` | `https://openrouter.ai/api/v1` | operative | 1 | OpenAI-compat endpoint. |
| `AURA_LLM_API_KEY` | (richiesto via `.env` `OPENROUTER_API_KEY`) | secret | 1 | API key. |
| `AURA_LLM_MODEL` | `deepseek/deepseek-v4-flash:exacto` | operative | 1 | Model id. Endpoint+model config-driven = seam portabilità D00 (amendment #30); mai hardcodare un provider. |
| `AURA_PRIVACY_MODE` | `open` | operative | 0.5 (D00) | `open` (remoto permesso) \| `local-only`. **Cablato (amendment #33, era solo prosa)**: (a) **boot fail-fast in `internal/config`** — se `local-only` e `AURA_LLM_BASE_URL`/`AURA_EMBED_BASE_URL`/`aura serve --bind` puntano a host **non-loopback**, Aura aborta con errore esplicito. È un **check loopback standalone** (parse host → `IsLoopback`), **NON dipende** dal classifier SSRF di Slice 5 (Phase 7): è una garanzia D00 day-1; Slice 5 più tardi *riusa/estende* questo check per `web_fetch`, non il contrario. (b) **`LLMRouter.Route()` trigger priorità 0** (sopra `prefer_local`) → forza locale, branch remoto irraggiungibile. Rende "massima privacy DGX" verificabile, non promessa. |
| `AURA_LLM_TEMPERATURE` | `0.7` | operative | 1 | Sampling temperature (A5 amendment 2026-05-30). |
| `AURA_LLM_MAX_TOKENS` | `4096` | cap | 1 | Max output tokens per call (A5 amendment 2026-05-30). |
| `AURA_LLM_TOTAL_TIMEOUT_SEC` | `120` | cap | 1 | Global HTTP timeout (via `context.WithTimeout` sul request ctx, non `http.Client.Timeout`). |
| `AURA_LLM_CONNECT_TIMEOUT_SEC` | `10` | cap | 1 | TCP connect timeout via `Transport.DialContext` (A5 amendment 2026-05-30). |
| `AURA_OTEL_EXPORTER` | `otlp` | operative | 1 | OTel trace exporter ∈ `{stdout,otlp,none}` (A5 + A2 amendment 2026-05-30). `none` = no-op provider. |
| `AURA_OTEL_ENDPOINT` | `localhost:4317` | operative | 1 | OTLP/gRPC collector endpoint (A5 + A2 amendment 2026-05-30). Silent-drop senza collector. |
| `AURA_CONFIG_DIR` | `~/.aura/` | path | 1 | Config root (contiene `llm.json`). |
| `AURA_RUN_DIR` | `<os.UserCacheDir>/aura` | path | 1 | Runtime sidecar dir (default = cache dir, es. `~/.cache/aura`; fallback `<os.TempDir>/aura`. Corretto vs `~/.aura/run/` stale — amendment #28). |
| `AURA_RUN_DIR_WARN_THRESHOLD_BYTES` | `1073741824` (1 GiB) | cap | 1 | Boot warn threshold. |
| `AURA_CONTEXT_PREVIEW_CAP_BYTES` | `2048` | cap | 1 | ToolResult preview boundary. |
| `AURA_CONTEXT_RESERVE_TOKENS` | `13000` | cap | 1.8b | Context window reserve. |
| `AURA_CONTEXT_MAX_OUTPUT_TOKENS` | `20000` | cap | 1.8b | Max output tokens enforced. |
| `AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS` | `10` | cap | 1.8b | Microcompact L1 threshold. |
| `AURA_MODEL_CONTEXT_WINDOW` | (auto, per provider) | operative | 1.8b | Override context window calc. |
| `AURA_MODEL_MAX_OUTPUT_TOKENS` | (auto, per provider) | operative | 1.8b | Override max output calc. |
| `AURA_CONVERSATION_TURN_CAP_BYTES` | `65536` (64 KiB) | cap | 1.8 | DB cell spillover boundary. (Era erroneamente 262144 in tabella; codice + §L968/L4620 = 65536 — fix doc-verify 2026-06-01.) |
| `AURA_SANDBOX_URL` | `http://127.0.0.1:18901` | operative | 2a | Sandbox sidecar endpoint. |
| `AURA_SANDBOX_TIMEOUT_SEC` | `30` (cap `600`) | cap | 2a | Per-execute timeout. |
| `AURA_SANDBOX_SESSION_TTL_SEC` | `1800` (30 min) | cap | 2b | Session-bound container idle TTL prima del reap. |
| `AURA_SANDBOX_MAX_CONCURRENT_SESSIONS` | `5` | cap | 2b | Hard cap session concurrent per istanza Aura. |
| `AURA_SANDBOX_WORKSPACE_MAX_BYTES` | `104857600` (100 MiB) | cap | 2b | Quota per workspace mount per conversation. |
| `AURA_SANDBOX_NETWORK_ALLOW_HOSTS` | `` (empty = deny totale) | operative | 2b | CSV hosts allowlist (es. `pypi.org,files.pythonhosted.org`). Empty mantiene `network_mode: none` Slice 2a. Non-empty attiva tier Risk-Based RISKY. |
| `AURA_LOOP_MAX_STEPS` | `25` | cap | 0.9 | Tool-call iteration cap per `Run` (amendment #19, Pitfall #9 P0). Child agents inherit parent's REMAINING (not fresh). Trip → `Event.FinishReason='max_steps'`. |
| `AURA_LOOP_MAX_WALLCLOCK_SEC` | `300` | cap | 0.9 | Wallclock budget per `Run` in seconds (amendment #19, Pitfall #9 P0). Children inherit parent's absolute deadline. Trip → `Event.FinishReason='max_wallclock'`. |
| `AURA_LOOP_DEDUP_WINDOW` | `3` | cap | 0.9 | Sliding window size for tool-call dedup (amendment #19, Pitfall #9 P0). If last N tool calls have identical `tool_name + args_hash`, trip → `Event.FinishReason='dedup_loop'`. |
| `AURA_LOOP_DEDUP_EXEMPT_TOOLS` | `` (empty) | operative | 0.9 | CSV allowlist of tool names exempt from the two-phase dedup check (A7, D-19). Tools whose constant-result output is legitimately repeated (e.g. a poll) bypass the dedup veto and terminate only via the hard step/wallclock caps. |
| `AURA_LOOP_BRANCH_SOFT_FRACTION` | `1.0` | cap | 0.9 | Passive per-branch fair-share fraction in `(0,1]` (A7, D-12). A parallel branch's advisory soft cap is `ceil(remaining × frac / fanout)`; non-terminal — the shared hard `AURA_LOOP_MAX_STEPS` counter stays authoritative. |
| `AURA_LOOP_NODE_TIMEOUT_SEC` | `0` (disabled) | cap | 0.9 | Optional per-node soft timeout in seconds (A7, D-13). When >0 a single agent node is bounded via `context.WithDeadline`; `0` disables the per-node cap (only the run-wide `AURA_LOOP_MAX_WALLCLOCK_SEC` applies). |
| `AURA_LOOP_DEDUP_RESULT_CAP` | `2048` | cap | 0.9 | Byte cap on the tool-result preview the dedup ring retains as a progress veto (A7, D-18). Bounds memory; a changing preview within the cap suppresses the next dedup, a stable preview lets period-1/period-2 repeats terminate. |
| `AURA_SWARM_MAX_DEPTH` | `2` | cap | 3 | Spawn depth cap v1 (amendment #12 — was `3` pre-2026-05-29; full N-deep deferred to v2 SWARM-V2-01). |
| `AURA_SWARM_MAX_CONCURRENT` | `4` (risolto per-target) | cap | 3 (D00) | **Cap larghezza fan-out (amendment #34)**: worker concorrenti per `ParallelAgent` wrap. `MAX_DEPTH` bounda la profondità e il budget bounda gli *step*, ma NIENTE bounda i child *simultaneamente vivi* → su 32GB N child paralleli (ognuno = 1 conn LLM + goroutine + contesa sul container sandbox) satura RAM/FD (viola D00.6). Default risolto dal target (piccolo su 32GB, ampio su DGX, config-per-deployment). Eccedenza accodata, non lanciata concorrente. |
| `AURA_SWARM_MODEL_CHAT` | `=AURA_LLM_MODEL` | operative | 3 | Tier override (v1 NO-OP per amendment #12 — all tiers resolve to `AURA_LLM_MODEL`; enforcement deferred to v2 SWARM-V2-01). |
| `AURA_SWARM_MODEL_REASONING` | `=AURA_LLM_MODEL` | operative | 3 | Tier override (v1 NO-OP per amendment #12 — all tiers resolve to `AURA_LLM_MODEL`; enforcement deferred to v2 SWARM-V2-01). |
| `AURA_SWARM_MODEL_WORKER` | `=AURA_LLM_MODEL` | operative | 3 | Tier override (v1 NO-OP per amendment #12 — all tiers resolve to `AURA_LLM_MODEL`; enforcement deferred to v2 SWARM-V2-01). |
| `AURA_SWARM_MAX_GOALS` | `8` | cap | 9 | **Goals-array cap (amendment #44, D-13)**: `len(goals) > AURA_SWARM_MAX_GOALS` in a single `swarm_spawn` call → model-readable tool error (Anthropic over-spawn failure mode #1). Bounds the total fan-out per call; the per-child summary cap (reuses `AURA_CONTEXT_PREVIEW_CAP_BYTES`) bounds each report. |
| `AURA_SWARM_CHILD_TIMEOUT_SEC` | `120` | cap | 9 | **Per-worker ctx deadline (amendment #44, D-11)**: each swarm child runs under a `context.WithTimeout` of this many seconds; a timeout produces a `{status:"failed", error:"timeout"}` report entry, siblings unaffected. The shared `Budget` wallclock (`AURA_LOOP_MAX_WALLCLOCK_SEC`) remains the global ceiling. `_SEC` suffix per the naming convention below. |
| `AURA_WEB_FETCH_ALLOW_LOOPBACK` | `0` (false) | operative | 5 | Permit loopback URLs in web_fetch. |
| `AURA_WEB_FETCH_ALLOW_HOSTS` | `` (empty) | operative | 5 | CSV allowed hosts override. |
| `AURA_RISK_ALERT_THRESHOLD` | `risky` | cap | RBG | Notifier IMMEDIATE alert threshold (≥ tier). |
| `AURA_SCHEDULER_TZ` | `Europe/Rome` | operative | 6 | Default IANA timezone per i task `cron` privi di un `tz` esplicito (amendment #46, D-06/D-07). DST-safe: il DB resta UTC, solo il calcolo di `next_run_at` è tz-aware. |
| `AURA_SCHEDULER_NOTIFY_DEFAULT` | `stdout` | operative | 6 | Route di notifica fallback globale ∈ `{whatsapp, email, stdout}` (amendment #46, D-19/D-20). La route per-task `notify` nel payload la sovrascrive. |
| `AURA_SCHEDULER_NOTIFY_RECIPIENT` | `` (empty) | operative | 6 | Recipient JID/email di default per la route fallback globale (amendment #46, D-20). |
| `AURA_SCHEDULER_QUIET_HOURS` | `` (empty = off) | operative | 6 | Finestra quiet-hours `HH:MM-HH:MM` (es. `23:00-07:30`): le notifiche non-DESTRUCTIVE differiscono alla fine della finestra (amendment #46, D-23). I reminder schedulati esplicitamente dentro la finestra firano comunque al loro orario. |
| `AURA_SCHEDULER_TICK_SECONDS` | `30` | cap | 6 | Intervallo del tick loop DIY in secondi (amendment #46). |
| `AURA_SCHEDULER_MAX_CONCURRENT_RUNS` | `4` | cap | 6 | Cap concorrenza held-conn (advisory lock per-run su conn dedicata, D-03 Pitfall 2). Dimensionato **strettamente sotto** `pool MaxConns` (amendment #46). |
| `AURA_SCHEDULER_NOTIFY_RETRY_ATTEMPTS` | `3` | cap | 6 | Retry bounded della delivery prima del fallback definitivo a stdout + flag notification-undelivered (amendment #46, D-22). |
| `AURA_BACKUP_DIR` | `~/.aura/backups/` | path | 6 | Destinazione dei dump di backup (`pg_dump`, `neo4j-admin database dump`) via `docker exec` (amendment #46, D-26; riconcilia `$AURA_BACKUP_DIR` del ROADMAP con il path PRD). Retention 14d/7d rolling. |
| `AURA_SKILLS_DIR` | `~/.aura/skills` | path | 7 | Skills root (active/pending/archived dirs). Global FS; identity only in audit rows (amendment #48, D-34). |
| `AURA_SKILL_BODY_CAP_BYTES` | `32768` (32 KiB) | cap | 7 | Write-time refuse cap for a SKILL.md body (D-34; also in Caps prose). |
| `AURA_SKILL_INJECTION_BLOCKLIST` | (built-in list) | operative | 7 | Prompt-injection blocklist patterns (NFKC-normalize THEN match, write-boundary only — D-27/D-28). |
| `AURA_SKILL_MANIFEST_CAP_BYTES` | `8192` | cap | 7 | Manifest-in-Description byte cap; past it the block ends "N more — search with skill action=list {query}" BM25 overflow (amendment #48, D-09/D-34). |
| `AURA_SKILL_SNIPPET_TTL_DAYS` | `90` | cap | 7e | Idle threshold for the `skill_ttl_sweep` cron TaskKind to auto-archive a snippet (amendment #48, D-16/D-34). |
| `AURA_SKILL_EXPORT_DIR` | `~/.aura/skills/export` | path | 7e | Host dir materialized to the sandbox `/skills` ro mount on activation (amendment #48, D-17/D-34). |
| `AURA_SKILL_CATALOG_URL` | `https://www.skills.sh` | operative | 7 | skills.sh base for the `/api/search` JSON catalog client (amendment #48, D-11/D-34). |
| `AURA_SKILL_INSTALL_TIMEOUT_SEC` | `90` | cap | 7 | `context.WithTimeout` for the native `git clone` install (amendment #48, D-15/D-34). |
| `AURA_SANDBOX_AGENT_TOKEN` | (auto-gen at first boot, mirror `AURA_SETUP_TOKEN`) | secret | 2/7e | Bearer sent by `sandboxagent.Client` (`Authorization: Bearer`) so `:2468` enforces auth (today `--no-token`); the healthcheck must carry it too (amendment #48, D-38, spike 008). Model-authored snippet code makes this load-bearing. |
| `AURA_SKILL_PATTERN_ANALYSIS_INTERVAL_MIN` | `60` | cap | 7e | Background pattern_analyzer goroutine interval (v1.x deferred Slice 7f per amendment #13). |
| `AURA_SKILL_PATTERN_ANALYSIS_WINDOW_DAYS` | `7` | cap | 7e | Query execute logs window per cluster detection (v1.x deferred Slice 7f per amendment #13). |
| `AURA_SKILL_AUTOSUGGEST_MIN_LOC` | `20` | cap | 7e | Min code LOC per auto-suggest candidate (v1.x deferred Slice 7f per amendment #13). |
| `AURA_SKILL_AUTOSUGGEST_MIN_OCCURRENCES` | `3` | cap | 7e | Min cluster size per ask_user emit (v1.x deferred Slice 7f per amendment #13). |
| `AURA_SKILL_AUTOSUGGEST_SIMILARITY_THRESHOLD` | `0.85` | cap | 7e | HNSW similarity threshold per cluster grouping (v1.x deferred Slice 7f per amendment #13). |
| `AURA_SKILL_TTL_DAYS` | (superseded) | cap | 7e | **RENAMED → `AURA_SKILL_SNIPPET_TTL_DAYS` (amendment #48, D-34)**; same 90-day default. |
| `AURA_SKILL_TTL_SWEEP_INTERVAL_HR` | (removed) | cap | 7e | **REMOVED (amendment #48, D-16)**: the sweep is a cron `skill_ttl_sweep` TaskKind seeded daily, NOT a goroutine with an interval env. |
| `AURA_AGUI_CORS_PERMISSIVE` | `0` | operative | 8 | Dev mode CORS `*`. |
| `AURA_AGUI_PATH_RUN` | `/agent/run` | operative | 8 | AG-UI endpoint path. |
| `AURA_CHANNEL_<NAME>_ENABLED` | `1` (true) | operative | 9a | Per-channel enable (es. `AURA_CHANNEL_TELEGRAM_ENABLED`). |
| `AURA_SETUP_BIND` | `127.0.0.1:9081` | operative | 9a | Setup wizard bind. |
| `AURA_SETUP_TOKEN` | (auto-generated UUIDv4 if unset) | secret | 9a | One-time setup wizard auth token (amendment #10). Printed to stdout on first `aura serve` boot; invalidated after first successful Telegram onboarding. Required when `AURA_SETUP_BIND=0.0.0.0:9081` (remote setup). |
| `AURA_TELEGRAM_STATUS_THROTTLE_MS` | `1500` | cap | 9b | Status pane throttle. |
| `AURA_TELEGRAM_CONTENT_THROTTLE_MS` | `500` | cap | 9b | Content streaming throttle. |
| `AURA_TELEGRAM_CHAT_RATE_LIMIT_MS` | `1000` | cap | 9b | Chat queue hard rate (1/sec Telegram). |
| `AURA_TELEGRAM_DOC_SYNC_MAX_BYTES` | `5242880` (5 MiB) | cap | 9c | Sync convert boundary. |
| `AURA_TELEGRAM_DOC_ASYNC_MAX_BYTES` | `52428800` (50 MiB) | cap | 9c | Async convert hard cap. |
| `AURA_PROFILE_CERTAINTY_N` | `3` | cap | 10 | Auto-add certainty threshold. |
| `AURA_PROFILE_DIR` | `~/.aura/agents` | path | 10 | Per-identity profile dir. |
| `AURA_EMBED_DIMENSIONS` | `768` | cap | 0.7 | Embedding vector dimensionality (amendment #18, Pitfall #7 P0). Sidecar `aura-llama-embed` boot-asserts `model.output_dim == this`. Neo4j `CREATE VECTOR INDEX` **hardcoda 768** (templating ritirato — amendment #24, incompatibile col checksum di migrate.go); il contratto è garantito a boot da `pingEmbed`. **NEVER change in-place** on a populated DB — see Slice 0.7 runbook. |
| `AURA_MEMORY_CHUNK_SIZE_TOKENS` | `512` | cap | 11b | Recursive semantic chunker target size. |
| `AURA_MEMORY_CHUNK_OVERLAP_TOKENS` | `64` | cap | 11b | Sliding overlap fra chunks adiacenti. |
| `AURA_MEMORY_EMBED_BATCH_SIZE` | `32` | cap | 11b | Batch embedder requests per call sidecar. |
| `AURA_MEMORY_ENTITY_BATCH_SIZE` | `10` | cap | 11b | Batch chunks per LLM entity extraction call. |
| `AURA_MEMORY_COMMUNITY_INTERVAL_HR` | `24` | cap | 11c (DEFERITA, amendment #27) | Leiden community detection interval — env DEFERITA con 11c (feature lazy/on-demand futura). Non letta in MVP. |
| `AURA_AGENT_INSIGHT_CACHE_TTL_SEC` | `600` (10 min) | cap | 11e | LRU cache TTL per `:AgentInsight` retrieval (amendment #11). Preserva `messages[2]` byte-identity cross-turn → Slice 4 KV cache hit. **Cross-slice invariant** — modifica solo se Slice 4 `cache_invariant_audit.sh` resta verde. |
| `AURA_MEMORY_INSIGHT_INTERVAL_MIN` | `60` | cap | 11e | Cross-conv pattern analyzer interval. |
| `AURA_MEMORY_INSIGHT_TOP_K` | `3` | cap | 11e | Top-K AgentInsight inject in system prompt. |
| `AURA_MEMORY_INSIGHT_RELEVANCE_THRESHOLD` | `0.7` | cap | 11e | Min similarity per inject (skip junk). |
| `AURA_MEMORY_MEMIFY_INTERVAL_HR` | `24` | cap | 11e | Memify post-processing background interval. |
| `AURA_MEMORY_PRUNE_IDLE_DAYS` | `180` | cap | 11e | Entity soft-archive threshold giorni (last_mentioned_at); amendment #29 alzato 90→180, soft-archive non hard-delete. |
| `AURA_MEMORY_PRUNE_MIN_MENTIONS` | `3` | cap | 11e | Entity prune threshold (mention_count). |
| `AURA_MEMORY_RRF_CONSTANT` | `60` | cap | 11d | Weighted-RRF rank constant (amendment #26). Fusione Cypher-native rank-based scale-free. |
| `AURA_MEMORY_RERANK_TOP_K_IN` | `20` | cap | 11d | Hybrid retrieval candidates pre-rerank (RRF `sourceK`/finalK input). |
| `AURA_MEMORY_RERANK_TOP_K_OUT` | `5` | cap | 11d | LLM re-ranker output size (RRF `finalK`). |
| `AURA_MEMORY_RERANK_ENABLED` | `true` | cap | 11d | LLM re-ranker always-on per query interattive (amendment #29). `false` per batch-eval/bulk; confidence-skip + cross-encoder locale = follow-on. |
| `AURA_LLM_LOCAL_BASE_URL` | `http://aura-vllm-chat:8083/v1` | operative | 13 | Local LLM endpoint (vLLM o llama.cpp fallback). |
| `AURA_LLM_LOCAL_MODEL` | `gemma-3-12b-it` | operative | 13 | Local LLM model id. |
| `AURA_LLM_OFFLINE_DETECTION_INTERVAL_SEC` | `30` | cap | 13 | TCP probe interval verso remote per offline detection. |
| `AURA_LLM_LOCAL_FALLBACK_COST_USD_DAY` | `1.0` | cap | 13 | Threshold cost daily per auto-switch a local. |
| `LMCACHE_LOCAL_DISK_PATH` | `/var/cache/lmcache` | path | 13 | LMCache disk-tier path (no prefix AURA_, naming canonico LMCache). |
| `LMCACHE_MAX_LOCAL_DISK_GB` | `50` | cap | 13 | LMCache disk-tier max size GB. |
| `TELEGRAM_BOT_TOKEN` | (richiesto via setup) | secret | 9a | Bot token (no prefix `AURA_` per convenzione lib). |
| `OPENROUTER_API_KEY` | (richiesto via `.env`) | secret | 1 | Forwarded a `AURA_LLM_API_KEY`. |
| `MULTIMODAL_BASE_URL` | `http://aura-llama-multimodal:8082/v1` | operative | 9c | Sidecar URL (compose-only). |
| `MULTIMODAL_MODEL` | `gemma-4-e4b` | operative | 9c | Sidecar model name. |
| `MULTIMODAL_API_KEY` | `no-key` | operative | 9c | Sidecar dummy key. |
| `LLAMA_MULTIMODAL_IMAGE_TAG` | `server` | operative | 9c | Docker image tag. |
| `LLAMA_MULTIMODAL_HOST_PORT` | `8082` | operative | 9c | Host port mapping. |

**Convenzione naming**: env Aura-controlled usano prefix `AURA_<DOMAIN>_<UNIT>` (es. `AURA_SWARM_MAX_DEPTH`). Env per librerie/sidecar di terze parti (`TELEGRAM_BOT_TOKEN`, `OPENROUTER_API_KEY`, `MULTIMODAL_*`, `LLAMA_*`) mantengono il loro naming canonico per compatibilità con tooling esterno (compose, libreria bot, ecc.).

**Nota MCP server registration (amendment #44, D-21)**: NON esistono env vars `AURA_MCP_{MAIL,WHATSAPP}_SERVER`. La registrazione dei server MCP (mail, whatsapp, e ogni altro) avviene via la **managed config** `~/.aura/mcp/servers.json`, gestita dal CLI shippato `aura mcp {install,add,list,doctor,enable,disable,remove}`. Il boot li monta da lì (`buildRegistryWithMCP`). Gli unici env MCP sono gli override test-tier `AURA_MCP_*_SERVER_JSON` usati da `internal/mcp/*_integration_test.go`. Questo supersede la lista env del D-23 (artefatto pre-spike): lo spike 001/002 ha scoperto che il path managed-config esisteva già (Codex commit `ae11737a`).

**Amendment #45 / CAP-09 / MCP-V2-01 (Phase 16, 2026-06-04): Aura MCP Manager + Third-Party Trust.** The deferred generic MCP manager is promoted into v1 as Phase 16. Aura owns a local MCP control plane over `~/.aura/mcp/servers.json`: profiles, richer trusted recipes, Calendar fixture support, catalog metadata, redacted profile export, status/doctor/logs, Streamable HTTP, and mount-time risk-policy enforcement. Trust classes are `trusted_recipe`, `trusted_local`, `sandboxed_local`, `remote_http`, and `blocked`; arbitrary third-party local commands default to `blocked` until the operator explicitly trusts them or routes them through a sandboxed/Docker runtime. `aura chat` must fail soft and must not launch a blocked local MCP command. Doctor/status checks availability and auth; policy decides whether tools enter the runtime registry. OpenClaw plugin hosting, marketplace auto-install, typed plugin RPC, and a background restart supervisor stay out-of-scope for this amendment.

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

Niente cap senza unità nel nome (`AURA_TOOL_PREVIEW_CAP` → `AURA_CONTEXT_PREVIEW_CAP_BYTES`). Niente cap hardcoded nei file `.go` per valori >100 — devono essere env-overrideable. Suffissi unità sanciti: `_BYTES`, `_SEC`, `_MS`, `_HR`, `_DAYS`, `_TOKENS`, `_USD_DAY`; per i conteggi/limiti dimensionali `_MAX_STEPS`/`_MAX_DEPTH`/`_MAX_CONCURRENT` (es. `AURA_SWARM_MAX_CONCURRENT`, amendment #34); per i rapporti adimensionali in `(0,1]` **`_RATIO`** (es. `AURA_CANVAS_MILD_RATIO`); per i booleani **`_ENABLED`** (es. `AURA_MEMORY_REASONING_TRACE_ENABLED`, `AURA_CANVAS_LLM_REFINE_ENABLED`); pesi interi adimensionali di una formula **`_W_<TERM>`** (es. `AURA_CANVAS_SCORE_W_SIZE`, amendment #29 OQ8).

---

## Sequencing rationale (Postgres infra → Neo4j infra → Agent runtime → LLM client → AskUser → Identity → Conversations → Sandbox → Swarm → KV → Web → Scheduler → Skills → AG-UI gateway → Channels/Telegram → Onboarding/Agent.md)

0. **Slice 0.5 (Postgres infra)** è il prerequisito di Slice 1.5/6/7. Indipendente da Slice 1 (LLM client non tocca DB), quindi può essere committato in parallelo. Atterrarla per prima dà a tutte le altre slice un substrate persistence pronto.

0.9. **Slice 0.9 (Agent runtime abstraction)** atterra dopo le infra (Slice 0/0.5/0.7) e PRIMA di Slice 1: definisce l'interface `Agent` + `iter.Seq2[*Event, error]` streaming + workflow agents (Sequential/Loop/Parallel) che TUTTE le slice successive riusano. Pattern rubato da google/adk-go (non importato per evitare 35 deps GCP-heavy). Slice 1 ridefinisce `Loop` come `LlmAgent` (implementa `Agent`). Slice 3 Swarm riusa `ParallelAgent`. Slice 6 Scheduler.Handler implementa `Agent`. Slice 10 onboarding = `LoopAgent[InterviewStepAgent]`. Saving cumulativo stimato −400 LOC netti + 1 runtime unificato vs 4 special-case pre-0.9.
0.bis. **Slice 0.7 (Neo4j infra)** atterra subito dopo Slice 0.5. Le 8 slice 1→7 NON la consumano direttamente, ma la slice 0.7 sblocca: (a) il backup TaskKind `backup_neo4j` di Slice 6b che riferisce un container produzione, non lo spike fuori repo; (b) le slice knowledge-facing post-7 (in arrivo) che scrivono `:Chunk` / `:Entity` / `:Source`; (c) la disciplina CLAUDE.md "knowledge + vectors → solo Neo4j" diventa eseguibile dall'inizio del progetto invece che essere una promessa. Profile utente (Slice 10 Agent.md) resta su filesystem, NON su Neo4j.
1. **Slice 1 (LLM client + ToolResult)** è prerequisito di tutto il resto: senza un client reale e senza il pattern preview+persist sui tool result, le altre slice non hanno modo di osservare comportamento end-to-end senza avvelenare il context window.
2. **Slice 1.5 (ask_user)** subito dopo: tocca `loop.go` una seconda volta su una concern semanticamente diversa (state machine pause/resume + PausedState persistent in Postgres). Atterrarlo prima di Slice 2-4 evita di rifattorare `loop.go` un'altra volta in mezzo. È anche prerequisito di Slice 7 governance C.
2.bis. **Slice 1.7 (Identity minimal)** chiude la wave persistence applicativa. Atterra dopo 1.5 (paused_states) e prima di Slice 2 perché: (a) sblocca Slice 7c per usare `actor_id` FK su `aura.skill_audit` invece di campo `text` opaco; (b) i suoi consumer (Slice 7c) sono lontani, ma il pattern "seed identity + capability_grants" deve essere stabile prima che skill_audit lo referenzi. Indipendente da Slice 2/3/4: può essere committata in parallelo a Slice 2 se preferito.
2.ter. **Slice 1.8 (Conversation persistence)** chiude la wave persistence applicativa con multi-conversation Claude.ai-style. Atterra dopo 1.7 (identity FK su conversations) e prima di Slice 2. Sblocca: (a) `aura chat list/resume/new` cross-session; (b) paused_states ora FK a conversations con cascade; (c) auto-resolve di pending stale quando Loop.Stop() (Area #7 closed); (d) audit forensics per token cost + cache hit ratio per conversazione. Out of scope riga "Persistenza disk dello stato conversazionale" rimossa. Migrations: `0005_conversations` (NEW) + `0006_conversation_turns_fts` (Slice 1.8.5 FTS) sono il floor shippato; i numeri a valle (scheduler, skill_audit, …) sono indicativi e assegnati all'atterraggio per ordine-fase (vedi §Persistence "Migration numbering — fonte di verità").
3. **Slice 2 (Sandbox)** prima dello Swarm: lo swarm spawn-a agenti che — nella realtà — useranno `execute`. Avere `execute` funzionante prima rende lo smoke dello swarm meno artificiale. **Atomicity split**: 2a (base stateless + seccomp + ulimit + net deny, ~600 LOC) atterra qui prima di Slice 3; **2b** (session-bound + workspace mount + network allowlist, ~350 LOC + migration 0010) richiede `conversation_id` quindi atterra DOPO Slice 1.8 ma prima di Slice 5 (web tools, che potrebbero beneficiare di workspace shared con sandbox). Pattern di riferimento: 2a = OpenAI Code Interpreter MVP; 2b = Claude Code on the Web + E2B + Anthropic Code Execution beta.
4. **Slice 3 (Swarm)** prima della KV: la KV cache discipline deve coprire ANCHE i prompt dei figli swarm-spawn. Costruire il PromptBuilder dopo aver visto come il parent passa goal/tools al child evita un secondo refactor.
5. **Slice 4 (KV)** chiude le 4 fondamentali: ora la superficie del prompt (system + manifest + tool descriptions + parent/child contracts + ask_user) è stabile. Il builder ottimizza un bersaglio fermo.
6. **Slice 5 (Web tools)** apre il backlog post-tabula-rasa: indipendente dalle 4 fondamentali, riusa `ToolResult` per fetch di pagine grandi. NON introduce `ActionRouter` (web_search/web_fetch sono 2 tool indipendenti senza azioni multiple — YAGNI). Vedi §Pattern condiviso.
7. **Slice 6 (Scheduler)** prima di Skills: si committa in 2 sub-slice (6a infrastructure + reminder handler, 6b agent_job + ActionRouter helper). 6b introduce `ActionRouter` come primo consumer reale (tool `task` con 4 azioni). Slice 7 lo riusa.
8. **Slice 7 (Skills)** ultima del backlog interno: si committa in 5 sub-slice (7a loader+validator+read-only tools, 7b catalog, 7c mutation governance, 7d installer, **7e snippet executor + pattern analysis + TTL archived**). Richiede il maggior numero di primitive pre-esistenti (ask_user da 1.5, ToolResult da 1, ActionRouter da 6b, persistent state da 0.5 + 1.5). Atterrarla per ultima del dominio interno evita di riscrivere il flow governance ogni volta che una primitive cambia forma. **Sub-slice 7e** estende le Skills con **executable code snippets** (multi-lang via SKILL.md `type: snippet`) eseguibili tramite sandbox Slice 2b (deve essere atterrata) + **pattern analysis multi-conversation auto-suggest** (background analyzer cluster via Slice 0.7 HNSW, propone save dopo 3+ pattern simili) + **TTL 90gg + archived state** (background sweep). Pattern rubato da Voyager (Wang et al., NeurIPS 2023) skill library. Token saving stimato ~520/riuso → ~$75-100/mese su 50 snippet attivi. 7e atterra dopo 7d + 2b + 0.7 tutte completate.
9. **Slice 8a (AG-UI translator + fanout)** atterra dopo Slice 7: tutto il dominio interno (loop + ask_user + sandbox + swarm + kv + web + scheduler + skills) è stabile, gli eventi che il Loop deve emettere via AG-UI sono prevedibili (text/tool/state/lifecycle/reasoning). Atterrarla qui evita di rifare il mapping eventi ogni volta che una primitive interna cambia. Risolve Area #16 (transport agnostico): Slice 9 connette Telegram come consumer **in-process via fanout (8a)**, senza codice channel-adapter custom. **Slice 8b (HTTP server `aura serve` + Dojo conformance) è deferita** (amendment #35) finché non esiste un client HTTP reale (SPA browser / accesso remoto) + l'auth dell'endpoint — **NON è pre-req di Slice 9**.
10. **Slice 9 (Channels framework + Telegram + Setup wizard + Multimodal)** chiude il ciclo transport: Telegram diventa il **main user-facing channel** (gli utenti finali non usano CLI), il channel framework `internal/channels/<name>/` apre la porta a WhatsApp/Discord/Signal futuri come slice incrementali, Setup wizard QR/deep-link rende l'onboarding self-service zero-CLI, e il sidecar Gemma 4 multimodal unifica vision + STT (rimuovendo Whisper). Atomicity: 9a framework + setup, 9b Telegram impl, 9c multimodal.
11. **Slice 10 (User onboarding + Agent.md)** atterra dopo Slice 9 perché ha Telegram come transport principale dell'interview e usa tutto lo stack pre-esistente (ask_user 1.5, identities 1.7, conversations 1.8, PromptBuilder 4, Risk-Based 5, AG-UI 8). Agent.md per identity in filesystem `~/.aura/agents/<id>/` viene iniettato come secondo system message (cache-friendly, Slice 4 invariants preservati). Pattern hybrid ChatGPT Custom Instructions + Memory + mem0 ADD-only. Out of scope "Setup wizard" + "Telegram" rimossi dal PRD; restano out: dashboard SPA full, tray icon, OTA update, multi-user auth.

11.bis. **Slice 11 (Memory ingestion + taxonomy)** atterra dopo Slice 10 e prima di Slice 13. Dipende da: Slice 0.7 Neo4j+HNSW+embed sidecar, Slice 7e snippet (per `:UserSnippet` mirror semantic), Slice 9c markitdown sidecar (per document → markdown universal parser), Slice 10 Agent.md (Core tier già attivo). Pattern: **mix-and-match dei migliori sistemi memory 2026** — mem0 (2-fase pipeline + hybrid retrieval, 48k stars), Letta MemGPT (3-tier storage Core/Recall/Archival), Microsoft GraphRAG (Leiden community detection + global summarization), Cognee (Cognify pipeline 6-stage + Memify post-processing). 4 sub-slice atomic MVP: 11a schema (+ valid-time/NOOP/reasoning, amendment #24), 11b ingestion pipeline, 11d retrieval+rerank (Weighted-RRF Cypher-native adottato, amendment #26; fusione lineare deprecata), 11e agent journal+Memify. **11c community detection è DEFERITA fuori MVP (amendment #27)** — feature lazy/on-demand futura (lo studio comparativo 2026 l'ha giudicata over-engineering per il target single-user/cost-sensitive; Microsoft stessa è passata a LazyGraphRAG); GDS resta installato per algoritmi ad-hoc, solo lo schedulato Leiden + summary è rimandato. **11f Task Canvas (amendment #25) è separato e sequencing-indipendente** (~Phase 9-10, dipende solo dal context management). Privacy isolation: single-user mode (no isolation), refactor multi-user accettato. Cost stima MVP: $0.05/doc ingest + $0.001/query rerank ($0.10/community-detection-run quotidiana rimosso — 11c deferita). Decision pre-merge utente.

12. **Slice 13 (Local LLM fallback)** atterra dopo Slice 11 perché completa lo stack provider-agnostico: `LLMRouter` con 4 trigger (explicit `prefer_local`, offline detection, cost threshold, identity capability) sceglie tra remote OpenRouter e local vLLM+LMCache. **Pattern doppio sidecar**: `aura-llama-multimodal` Slice 9c invariato per vision/STT one-shot + `aura-vllm-chat` nuovo Slice 13 per chat fallback con LMCache disk-tier 50 GB. **Open question CRITICA pre-merge**: vLLM CPU mode è 5-10x più lento di llama.cpp CPU per stesso modello — se mini-PC senza GPU, attivare path **13-bis** (riusa Gemma 4 E4B Slice 9c per chat fallback, no LMCache, save 1 sidecar). Pattern reference LMCache (8.4k stars, Apache 2.0, prod GCP/GMI/CoreWeave). 2 sub-slice: 13a router+offline+cost, 13b vLLM+LMCache sidecar.

## Out of scope per tutte e 4 le slice (esplicito)

- Dashboard SPA full, wiki, Qdrant, FTS5 (vedi [README.md](README.md)). Telegram bot e setup wizard minimal ATTERRANO in Slice 9 (closes Area #16 production).
- Tray icon, OTA update.
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
- **Embedder dedicato**: `embeddinggemma` via sidecar `aura-llama-embed` (porto 8081, OpenAI-compat). **768d nativo**, scritti direttamente su nodi `:Chunk.embedding` in Neo4j. Vector index HNSW configurato a 768 dim (l'index è il contratto; l'embedder emette 768 nativo o MRL-truncato-a-768 — D11/amendment #31; nessun resize client-side sotto 768). NON in store separato.
- **PostgreSQL 17 via `sqlc` + `jackc/pgx/v5`** — application state. Scheduler tasks, paused states, audit log, identity, capability grants. Schema `aura`. Migrations versionate in `internal/db/migrations/`. Industrial-grade, type-safe queries generate da SQL files.

> **Migration numbering — fonte di verità (riconciliata 2026-06-01).** Floor **shippato** (Phase 1-4, presente nel codice): `0001_init`, `0002_knowledge_migrations`, `0003_paused_states`, `0004_identity`, `0005_conversations`, `0006_conversation_turns_fts`. **Regola**: ogni migration a valle prende il numero **all'atterraggio = prossimo intero libero quando la sua PHASE esegue** (ordine-fase, NON ordine-slice — es. `sandbox_sessions` di Slice 2b/Phase 8 atterra *prima* di `scheduler` di Slice 6/Phase 10). I numeri assoluti hardcodati nelle sezioni slice a valle (`scheduler`, `skill_audit`, `telegram`, `profile_audit`, `sandbox_sessions`, `skill_snippet_audit`, `ingest_audit`, `task_canvas`, `local_llm`) sono **indicativi**, superseduti da questa regola: l'implementatore usa il prossimo numero libero al landing, non il valore scritto nella sezione. (Cypher/Neo4j ha la sua sequenza separata in `internal/knowledge/migrations/*.cypher`, oggi `0001_init`.)
- **Filesystem** — solo artefatti operativi: SKILL.md tree (`~/.aura/skills/active/`), tool result sidecar files (`$AURA_RUN_DIR/conversations/<session>/<tool-call-id>.result`). **Mai knowledge.**

Distinzioni semantiche **non negoziabili**: knowledge + vectors → solo Neo4j (via MCP); application state → solo Postgres (via sqlc); operational artifacts → filesystem. Le slice future che hanno bisogno di persistenza scelgono lo store giusto in base a cosa stanno salvando, non a "che dep era già lì". Nessuna knowledge in Postgres; nessun task scheduler in Neo4j; nessun markdown wiki da nessuna parte; nessun vector index dedicato fuori da Neo4j (Qdrant deprecato 2026-05-27 dopo validazione spike Phase 6b).
```

Questa nota va committata nello stesso commit di Slice 0.5 (sono accoppiate: codice + contract documentale insieme).
