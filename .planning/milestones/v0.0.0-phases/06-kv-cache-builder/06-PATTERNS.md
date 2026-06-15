# Phase 6: KV Cache Builder - Pattern Map

**Mapped:** 2026-06-02
**Files analyzed:** 10 (7 new, 3 modified)
**Analogs found:** 10 / 10 (every new/modified file has a real in-repo analog)

This phase is **reuse + one refactor**, not greenfield. Every new file copies an existing repo pattern at a known file:line. All excerpts below are verbatim from the live codebase (not invented). The planner should reference the analog file:line in each plan's action section.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/agent/prompt/builder.go` (new) | utility (prompt assembler) | transform (history → wire `llm.Request`) | `internal/agent/llm_agent.go:131-137` (inline `llm.Request{}`) + `internal/agent/prompt.go` (`systemMessage`) + `internal/agent/tools/manifest.go:51` (`RenderToolDefs`) | exact (extracts an existing inline construction) |
| `internal/agent/prompt/hash.go` (new) | utility (deterministic fingerprint) | transform (msgs → SHA-256 hex) | `internal/agent/llm_agent.go:318-328` (`canonicalArgs`) + `internal/canonicaljson/canonicaljson.go:32` (`Marshal`) | role-match (same canonicaljson→hash idiom) |
| `internal/agent/prompt/cache_anthropic.go` (new) | utility (provider branch / no-op seam) | transform (mutate `*llm.Request` by provider) | `internal/agent/tracing.go:129-138` (`setSpanAttrs` reading `provider`) + `internal/llm/config.go:19` (`defaultProvider`) | role-match (provider-keyed branch) |
| `internal/llm/request.go` → in `internal/llm/client.go:88-94` (modified) | model (wire struct field add) | request-response (serialized field) | `internal/llm/client.go:88-94` (`Request` struct) + `:43-53` (`ToolDef`) | exact (add a sibling field to the existing struct) |
| `internal/db/migrations/0007_cache_metrics.{up,down}.sql` (new) | migration | CRUD (DDL) | `internal/db/migrations/0005_conversations.up.sql` + `.down.sql` | exact (same schema/grant/comment conventions) |
| `internal/db/queries/cache_metrics.sql` (new) | query (sqlc source) | CRUD (INSERT + windowed SELECT) | `internal/db/queries/conversations.sql:1-48` | exact (`:exec`/`:many`/`:one` + `sqlc.arg`) |
| `internal/db/sqlc/cache_metrics.sql.go` (generated) | model/query client | CRUD | `internal/db/sqlc/conversations.sql.go` | exact (sqlc codegen — do not hand-edit) |
| `cmd/aura/cache.go` (new) | controller (CLI subcommand) | request-response (CLI args → stdout) | `cmd/aura/db.go:19-42` (dispatch) + `:84-114` (`dbStatus` tabwriter) | exact (same `case "<sub>"` + tabwriter) |
| `internal/runner/runner_persist.go` (modified) | service (persist seam) | event-driven (per-turn write) | `internal/runner/runner_persist.go:58-78` (`persistAssistantAnswer`) | exact (sibling INSERT in the same fn) |
| `internal/runner/interfaces.go` (modified) | model (narrow consumer interface) | — | `internal/runner/interfaces.go:50-57` (`PauseStore`) | exact (one more narrow Store) |
| `scripts/cache_invariant_audit.sh` (new) | test (CI smoke gate) | batch (run + diff + exit code) | `scripts/loop_budget_smoke.sh` | exact (same `set -euo pipefail` + grep-count + loud-fail discipline) |

> **PRD deviation (planner must amend PRD before code):** PRD §Slice 4 targets `internal/llm/prompt.go` + `internal/llm/cache_deepseek.go`. Confirmed import cycle (`internal/agent/tools/manifest.go:7` imports `internal/llm`, so `internal/llm` cannot import `tools`). `PromptBuilder` lands in **`internal/agent/prompt`**. `cache_deepseek.go` is **dropped** (parsing already shipped in `openai_compat`). Record both as a PRD-amendment + the D-02 OQ2 override (in-memory → Postgres) **before** any code commit (Research Pitfall 2).

## Pattern Assignments

### `internal/agent/prompt/builder.go` (utility, transform)

**Analog:** the inline construction this file extracts — `internal/agent/llm_agent.go:131-137`. The builder MUST reproduce this byte-for-byte (D-01: preserve the invariant, don't recreate it).

**Core pattern to copy** (`internal/agent/llm_agent.go:131-137`):
```go
req := llm.Request{
	Model:       a.cfg.Model,
	Messages:    a.history, // read-only — the client never mutates it (Req#13)
	Tools:       a.registry.RenderToolDefs(),
	Temperature: a.cfg.Temperature,
	MaxTokens:   a.cfg.MaxTokens,
}
```

**messages[0] source** — the byte-stable system message is prepended once at agent construction, NOT per-call (`internal/agent/llm_agent.go:68-69`):
```go
hist := make([]llm.Message, 0, len(cfg.UserTurns)+1)
hist = append(hist, llm.Message{Role: llm.RoleSystem, Content: systemMessage()})
```
and `systemMessage()` is a pure constant accessor (`internal/agent/prompt.go:25`):
```go
func systemMessage() string { return SystemPrompt }
```
`SystemPrompt` is a package `const` (`internal/agent/prompt.go:14-20`) — no clock, no per-turn value. **The builder must not re-prepend or re-template it.** It assembles the wire request around the already-stable `history[0]`.

**Tools half (cache-load-bearing ordering)** — `internal/agent/tools/manifest.go:51` `RenderToolDefs()` reuses the alphabetical sort of `Render()` (`manifest.go:39`):
```go
sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
```
The builder calls `reg.RenderToolDefs()` exactly as the inline code does — do NOT re-sort or re-order.

**Integration seam:** swap the inline `llm.Request{...}` in `llm_agent.go:131-137` for `PromptBuilder.Build(...)`. `a.cfg.Provider` is already on the struct (`llm_agent.go:41` field `cfg llm.Config`; read at `tracing.go:132`), so the provider branch needs no new plumbing.

---

### `internal/agent/prompt/hash.go` (utility, transform)

**Analog:** the existing canonicaljson→fingerprint idiom in `internal/agent/llm_agent.go:318-328` (`canonicalArgs`):
```go
func canonicalArgs(rawArgs string) []byte {
	var v any
	if err := json.Unmarshal([]byte(rawArgs), &v); err != nil {
		return []byte(rawArgs)
	}
	canon, err := canonicaljson.Marshal(v)
	if err != nil {
		return []byte(rawArgs)
	}
	return canon
}
```

**Deterministic serializer to reuse** (`internal/canonicaljson/canonicaljson.go:32`) — sorted keys, `json.Number` (1 ≠ 1.0), strict-reject. Do NOT hand-roll `json.Marshal` + key-sort:
```go
func Marshal(v any) ([]byte, error) {
```
The package doc (`canonicaljson.go:5-7`) confirms it is the project's reuse point: "feeds the dedup fingerprint sha256(name + canonical_json(args)) and is reused by Phase 4 (conversation hash) + Phase 11 (skill content_hash)."

**Index-set requirement (D-06a):** `PrefixHash(msgs []llm.Message, indices []int)` — iterate the index set, `canonicaljson.Marshal(msgs[i])` each present index, feed a single `sha256.New()`, return `hex.EncodeToString`. Skip indices `>= len(msgs)` so `{0,1,2}` works once Slices 10/11e ship while `{0}` is the only present index today.

---

### `internal/agent/prompt/cache_anthropic.go` (utility, provider branch — dormant no-op)

**Analog (provider-keyed read):** `internal/agent/tracing.go:129-138` already reads the provider string off the same `llm.Config`:
```go
func setSpanAttrs(span oteltrace.Span, model, provider, requestID string, usage llm.Usage) {
	span.SetAttributes(
		attribute.String("llm.model", model),
		attribute.String("llm.provider", provider),
		...
```

**Provider source of truth** (`internal/llm/config.go:19`) — never hardcode; the day-1 default is `openrouter`:
```go
defaultProvider          = "openrouter"
```

**Behavior:** `injectCacheControl(req *llm.Request, provider string)` is a **pure no-op unless `provider == "anthropic"`**. Under `openrouter` (day-1) it must leave `req` untouched — the SC#3 wire test asserts OpenRouter requests carry NO `cache_control`. Anthropic placement (CITED, dormant): `cache_control: {"type":"ephemeral"}` on the system block + the LAST tool def only, **never on history** (poisoning the gate prevents). The actual Anthropic-native wire translation is Slice 13's job; this phase only proves the branch exists.

---

### `internal/llm/client.go` (modified — the `Request` struct + design comment)

**Analog:** the existing `Request` struct (`internal/llm/client.go:88-94`) and its load-bearing comment that D-03a explicitly rewrites:
```go
// Request is one chat-completion call. Caching is a property of how the caller
// constructs Messages (stable prefix) — the wire layer is unaware.
type Request struct {
	Model       string
	Messages    []Message
	Tools       []ToolDef
	Temperature float64
	MaxTokens   int
}
```

**Change (D-03):** add `ToolsCacheControl string` as a sibling field. **Update the comment** (D-03a / DEEP REFACTOR ON TOUCH) — "the wire layer is unaware" is now false; the field is wire-shape but the injection *decision* lives in the `PromptBuilder`/provider layer. The package doc at `client.go:1-5` ("KV-cache discipline lives in the prompt builder (separate file)") already anticipates this — keep it consistent.

---

### `internal/db/migrations/0007_cache_metrics.{up,down}.sql` (migration, CRUD)

**Analog:** `internal/db/migrations/0005_conversations.up.sql`. Migration sequence verified — last shipped is `0006`, so **0007 is correct** (`internal/db/migrations/` glob).

**Schema conventions to copy** (`0005_conversations.up.sql:8-21, 60-65, 75-76`):
- `numeric(10, 4)` for USD (line 19: `total_cost_usd numeric(10, 4) NOT NULL DEFAULT 0`).
- `timestamptz NOT NULL DEFAULT now()` (line 13).
- `uuid ... REFERENCES aura.conversations (id) ON DELETE CASCADE` (line 11).
- Grants (lines 60-61): `GRANT SELECT, INSERT, UPDATE, DELETE ON ... TO aura_app;` / `GRANT ALL ON ... TO aura_migrate;` — for cache_metrics use **`SELECT, INSERT`** to `aura_app` (no UPDATE/DELETE; append-only metrics — ASVS V4/role-separation).
- `COMMENT ON TABLE ... IS '...(Slice 4 / Phase 6)...'` (lines 75-76).
- **Plain `CREATE INDEX`** (not `CONCURRENTLY`) — fresh table, multi-statement-tx-safe (Pitfall 4; the `CONCURRENTLY` note lives at `0005...up.sql:70-73`).

**Down-migration analog** (`0005_conversations.down.sql:10-12`) — `DROP TABLE IF EXISTS aura.cache_metrics;`.

---

### `internal/db/queries/cache_metrics.sql` (query, CRUD)

**Analog:** `internal/db/queries/conversations.sql`. Copy the directive + `sqlc.arg` style.

**`:exec` INSERT pattern** (`conversations.sql:1-3, 41-48`):
```sql
-- name: CreateConversation :one
INSERT INTO aura.conversations (id, identity_id, model, status, metadata)
VALUES ($1, $2, $3, 'active', $4)
```
**`sqlc.arg` named-arg pattern** for the `--since` window (`conversations.sql:41-48`):
```sql
-- name: UpdateConversationAggregates :exec
UPDATE aura.conversations
SET total_cost_usd = total_cost_usd + sqlc.arg(cost_usd)
WHERE id = sqlc.arg(id);
```
Queries: `InsertCacheMetric :exec`, `ListCacheMetricsSince :many` (per-turn rows, `WHERE ts >= sqlc.arg(since)::timestamptz ORDER BY ts ASC`), `AggregateCacheMetricsSince :one` (`count(*)` + `coalesce(sum(...),0)`). **SQL-side aggregation** (Research Pattern 3), compute the hit-rate ratio client-side guarding divide-by-zero.

---

### `internal/db/sqlc/cache_metrics.sql.go` (generated, CRUD)

**Analog:** `internal/db/sqlc/conversations.sql.go` (shape reference only — DO NOT hand-edit; run `sqlc generate`). The generated INSERT params + method shape mirrors `conversations.sql.go:22-52`:
```go
type CreateConversationParams struct {
	ID         pgtype.UUID `json:"id"`
	IdentityID pgtype.UUID `json:"identity_id"`
	Model      string      `json:"model"`
	Metadata   []byte      `json:"metadata"`
}

func (q *Queries) CreateConversation(ctx context.Context, arg CreateConversationParams) (AuraConversations, error) {
	row := q.db.QueryRow(ctx, createConversation, ...)
```
Config: `sql_package: pgx/v5`, `emit_interface: true`, `output_models_file_name: models.go` (`sqlc.yaml:4-27`). CI job "sqlc generate is in sync" fails if the generated file is not committed.

---

### `cmd/aura/cache.go` (controller, request-response)

**Analog:** `cmd/aura/db.go` (dispatch + tabwriter output) and `cmd/aura/main.go:35-61` (switch).

**Switch wiring** (`cmd/aura/main.go:42-45`) — add two cases (`cache-audit` hidden, NOT added to `usage()`):
```go
case "db":
	runDB(os.Args[2:])
case "neo4j":
	runNeo4j(os.Args[2:])
```

**Subcommand dispatch shape** (`cmd/aura/db.go:19-42`):
```go
func runDB(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: aura db {migrate|ping|status|reset}")
		os.Exit(1)
	}
	cfg := config.LoadDB()
	ctx := context.Background()
	switch args[0] {
	case "migrate":
		dbMigrate(ctx, cfg)
	...
	}
}
```

**tabwriter output** for `cache-stats` (`cmd/aura/db.go:108-113`):
```go
w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
_, _ = fmt.Fprintln(w, "VERSION\tDIRTY")
for _, r := range rows {
	_, _ = fmt.Fprintf(w, "%d\t%v\n", r.Version, r.Dirty)
}
_ = w.Flush()
```

**Registry build for the `cache-audit` replay** (`cmd/aura/main.go:68-77`) — `buildRegistry()` already constructs the real registry; the audit reuses it with `agenttest.FakeClient` as the `Client`:
```go
func buildRegistry() *tools.Registry {
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(&tools.ToolSearch{Registry: reg})
	...
}
```
`--since` parsing: `time.ParseDuration` (reject unparseable → exit non-zero, ASVS V5). Exit codes for `cache-audit`: 0 pass / 1 mutation / 2 fixture corrupt.

---

### `internal/runner/runner_persist.go` (modified — sibling INSERT)

**Analog:** `persistAssistantAnswer` already holds the per-turn `llm.Usage` and writes one aggregate (`internal/runner/runner_persist.go:58-78`):
```go
func (r *Runner) persistAssistantAnswer(ctx context.Context, convID string, ev *agent.Event) error {
	seq, err := r.nextSeq(ctx, convID)
	if err != nil {
		return err
	}
	u := usageFromStateDelta(ev.Actions.StateDelta)
	cost := 0.0
	if u.Cost != nil {
		cost = *u.Cost
	}
	return r.Conv.AppendTurn(ctx, conversations.AppendTurnParams{
		ConversationID: convID,
		Seq:            seq,
		Role:           llm.RoleAssistant,
		Content:        ev.LLMResponse.Content,
		InputTokens:    u.PromptTokens,
		OutputTokens:   u.CompletionTokens,
		CachedTokens:   u.CachedTokens,
		CostUSD:        cost,
	})
}
```
**Change (D-02a):** `u` and `cost` are ALREADY computed here — add ONE sibling `cacheMetrics.Insert(...)` call (no wire-path touch). `usageFromStateDelta` (`runner_persist.go:193-206`) is the source of `CachedTokens` (`d["cache_hit_tokens"]`). Mirror the `int32(...)` casts the conversations store uses (`internal/conversations/store.go:270-278`).

---

### `internal/runner/interfaces.go` (modified — new narrow Store)

**Analog:** `PauseStore` (`internal/runner/interfaces.go:50-57`) — a narrow consumer-side interface (D-A2-02 "accept interfaces, return structs"):
```go
type PauseStore interface {
	Insert(ctx context.Context, p askuser.InsertParams) error
	GetByToken(ctx context.Context, token string) (askuser.Pending, error)
	...
}
```
**Add `CacheMetricStore`** with just `Insert(ctx, ...) error`, wire a `cacheMetrics CacheMetricStore` field into `Deps` (`runner.go:37-49`) + `Runner` (`runner.go:58-74`) + `New` (`runner.go:78-100`), exactly mirroring how `Pause PauseStore` threads through. The `cache-audit` subcommand passes a no-op fake (it needs no Postgres); the prod path passes the real sqlc-backed store. Concrete stores satisfy the interface implicitly (see `internal/conversations/store.go:254` `AppendTurn` + tx pattern at `:288-298`).

---

### `scripts/cache_invariant_audit.sh` (test — CI smoke gate)

**Analog:** `scripts/loop_budget_smoke.sh` — the established runtime-faithful, NO-SKIP-AS-GREEN gate.

**Header + strict mode + toplevel cd** (`loop_budget_smoke.sh:17-19`):
```bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
```

**Run-the-binary + count-lines + loud-fail discipline** (`loop_budget_smoke.sh:22-35`) — the exact pattern the audit copies (drive `go run ./cmd/aura cache-audit`, capture stdout, count the 20 hash lines, fail loudly with the explicit message on mismatch):
```bash
OUT="$(go run ./cmd/aura agent dry-run --max-steps 25)"
LINES="$(printf '%s\n' "$OUT" | grep -c . || true)"
if [[ "${LINES:-0}" -ne 26 ]]; then
  echo "FAIL (SC#2): expected exactly 26 Event lines ..., got ${LINES:-0}" >&2
  printf '%s\n' "$OUT" >&2
  exit 1
fi
```
For the cache gate: assert exactly 20 `turn NN: <hex>` lines, diff consecutive hashes, and on mismatch emit the SC#5 wording `messages[0] mutated at <site>` to stderr + `exit 1`. The `|| true` guard on `grep -c` (`:30`) is required so an EMPTY output aborts with the diagnostic, not a bare pipefail.

## Shared Patterns

### Provider awareness (never hardcode)
**Source:** `internal/llm/config.go:19` (`defaultProvider = "openrouter"`), read via `a.cfg.Provider` (`internal/agent/tracing.go:132`).
**Apply to:** `builder.go`, `cache_anthropic.go`. The provider is config-driven (Seam D00 / amendment #30); the branch reads `cfg.Provider`, never a literal.

### Deterministic hashing
**Source:** `internal/canonicaljson/canonicaljson.go:32` (`Marshal`) — sorted keys, `json.Number`, strict-reject.
**Apply to:** `hash.go` (the `PrefixHash` invariant fingerprint). Reuse, do not hand-roll (Research "Don't Hand-Roll").

### Postgres role separation
**Source:** `internal/db/migrations/0005_conversations.up.sql:60-61` (`aura_app` = SELECT/INSERT/UPDATE/DELETE, `aura_migrate` = ALL).
**Apply to:** `0007_cache_metrics.up.sql` — `aura_app` gets **SELECT, INSERT only** (append-only metrics, no UPDATE/DELETE). DDL via `aura_migrate` + `AURA_DB_MIGRATE_URL` (amendment #17).

### Narrow consumer-side Store interface (D-A2-02)
**Source:** `internal/runner/interfaces.go:50-64` (`PauseStore`, `IdentityStore`).
**Apply to:** the new `CacheMetricStore` — declare only the method the Runner calls (`Insert`); concrete sqlc-backed store satisfies it implicitly. Lets the audit pass an in-memory no-op (no DB → supports the 85% floor).

### sqlc parameterized queries (no string concat — ASVS V5)
**Source:** `internal/db/queries/conversations.sql:41-48` (`sqlc.arg`), generated to `pgtype`-typed params (`conversations.sql.go:22-35`).
**Apply to:** `cache_metrics.sql` — the `--since` window arg is a bound `sqlc.arg(since)::timestamptz`, never interpolated.

### NO-SKIP-AS-GREEN runtime-faithful gate
**Source:** `scripts/loop_budget_smoke.sh:15-42` (runs the real binary, counts real output lines, fails loudly on empty).
**Apply to:** `cache_invariant_audit.sh` + the `cache-audit` subcommand. The replay drives the REAL `runner.Turn → LlmAgent.Run → Build` path against `agenttest.FakeClient` (D-04); a synthetic `Build()` hash is forbidden (trivially green).

### FakeClient request capture (the audit's read seam)
**Source:** `internal/agent/agenttest/fakeclient.go:53-59` — `Stream` records each request with a **cloned** Messages slice:
```go
snap := req
snap.Messages = append([]llm.Message(nil), req.Messages...)
f.Requests = append(f.Requests, snap)
```
**Apply to:** the `cache-audit` replay — read `fakeClient.Requests[n].Messages[0]` directly (D-05); the clone means a later in-place agent mutation cannot corrupt the snapshot. Build turns with `agenttest.ToolCallTurn` (`fakeclient.go:107`) + `TextChunks` (`:96`) + `MakeToolCall` (`:126`) — fixtures MUST include tool-call turns.

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| (none) | — | — | Every new/modified file maps to an existing repo analog. |

**Note on dropped PRD file-targets** (planner: fold into the PRD-amendment, do NOT create):
- `internal/llm/cache_deepseek.go` — DeepSeek/OpenRouter usage parsing (`cached_tokens`, `cost`) is ALREADY shipped in `internal/llm/openai_compat/sse.go` + `usage.go` (D-02a). Open Question 1 resolves: do not create it.
- `internal/llm/prompt.go` — import cycle; relocated to `internal/agent/prompt/`.

## Metadata

**Analog search scope:** `internal/agent`, `internal/agent/prompt` (target), `internal/agent/tools`, `internal/agent/agenttest`, `internal/llm`, `internal/db/{migrations,queries,sqlc}`, `internal/runner`, `internal/conversations`, `internal/canonicaljson`, `cmd/aura`, `scripts/`.
**Files scanned (read this session):** `internal/agent/prompt.go`, `internal/agent/tools/manifest.go`, `internal/agent/llm_agent.go`, `internal/agent/tracing.go` (setSpanAttrs), `internal/agent/agenttest/fakeclient.go`, `internal/llm/client.go`, `internal/llm/config.go`, `internal/canonicaljson/canonicaljson.go`, `internal/runner/runner.go`, `internal/runner/runner_persist.go`, `internal/runner/interfaces.go`, `internal/conversations/store.go`, `internal/db/migrations/0005_conversations.{up,down}.sql`, `internal/db/queries/conversations.sql`, `internal/db/sqlc/conversations.sql.go`, `cmd/aura/main.go`, `cmd/aura/db.go`, `scripts/loop_budget_smoke.sh`, `sqlc.yaml`, plus migration-sequence glob (confirms 0006 is latest → 0007 correct).
**Pattern extraction date:** 2026-06-02
