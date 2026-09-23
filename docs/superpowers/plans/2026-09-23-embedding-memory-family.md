# Embedding model change — memory family (plan 2 of 5) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every `FACT`, `ConversationTurn` and `ReasoningTrace` vector carries the space that produced it. Dense memory retrieval runs only when the whole memory is in the reader's space. A daemon pass re-embeds whatever is not, in batches and with a cursor, setting aside only the records the model itself refuses.

**Architecture:** `internal/embeddings` gains a typed HTTP status error, a live credential and a `Route` that embeds *and* names its space (the local route attests the sidecar on every call). `internal/arcadedb` swaps its plain embedder for a `DenseEmbedder` (embed + space), and every write goes through one helper that reads the space before the request. That helper isolates input refusals by halving, checked against a control input. The helper's result is written as `embedding` plus `embed_space` in the same statement. A per-tenant gate (three indexed counts, cached 30 s) decides dense vs lexical. The `memory_embed_backfill` sweep becomes the §5 pass: RID cursor per type, rotation across tenants, and a clean stop at the run budget. The daemon builds one memory route whose key is read from the running LLM profile.

**Tech Stack:** Go 1.26, ArcadeDB 26.9.1 over HTTP (SQL + sqlscript), llama.cpp `/v1/models` and `/v1/embeddings`, OpenRouter embeddings.

**Spec:** `docs/superpowers/specs/2026-09-23-embedding-model-change-design.md` (amended `6de7e7da6`): §1 (width per family), §2, §3 (memory family), §5, §10 (memory tools' reason), Fix on touch (`SearchConversationTurnsHybrid`, `rankRecallKinds`). Plan 1 (`docs/superpowers/plans/2026-09-23-embedding-route-and-identity.md`) delivered `embeddings.Space`, `SpaceFor`, `RouteSpace`, `AttestLocal`, `config.ResolveEmbedRoute`, `config.EmbedRouteKind`, `settings.EmbedRoute`.

## Global Constraints

- Tests run in WSL, never as a Windows `.exe`: `wsl -e bash -lc "cd /mnt/d/Aura && go test <pkg> -run '<regex>' -count=1"`.
- Per task: the targeted tests named in the task only. `go vet ./...`, race, deadcode and coverage run at the end of plan 5 or in CI (operator, 2026-09-23).
- `--no-verify` is forbidden. Another session shares the git index: `git add` only the paths the task names, and never stage or format someone else's file without asking.
- Master-direct, no push until plans 1–5 are done and CI is green.
- No file above 600 lines; every touched file gets dead-code removal in the same commit.
- Memory vectors are 768 wide (`arcadedb.vectorDimensions`); the memory space is computed at that width (spec §1, "each family names its own width").
- The stamp column is `embed_space STRING`, indexed `NOTUNIQUE NULL_STRATEGY INDEX`.
- Every write that sets `embedding` sets `embed_space` in the same statement. Every path that removes `embedding` removes `embed_space`, except a refusal, which keeps the stamp of the space that refused (spec §2, §5).
- A record is set aside as refused only on 400, 413 or 422, and only after a control input has embedded successfully in the same call. 401, 403, 429, 5xx and network errors end the call, and nothing is marked (spec §5).
- ArcadeDB facts this plan rests on, read in the official docs (`ArcadeData/arcadedb-docs`, `src/main/asciidoc/…`, 2026-09-23) or measured on the lab VM (26.9.1, read-only, 2026-09-23):
  - `reference/sql/sql-indexes.adoc`: null strategy default `SKIP`, "queries against null values that use an index return no entries" → `NULL_STRATEGY INDEX`.
  - `reference/sql/sql-pagination.adoc`: RID-LIMIT paging `WHERE @rid > <lower-rid> … LIMIT n`, starting at `#-1:-1`.
    - VM, measured: a string bind parameter compares as a RID (`#44:9` is followed by `#44:10`, not by `#44:2048`), and `ORDER BY @rid` works (`reference/sql/sql-select.adoc` documents it).
  - `reference/sql/sql-update.adoc`: `SET a = …, b = …` takes several fields. A multi-field `REMOVE` is not documented, so every clear is `SET embedding = NULL, embed_space = …`. `SET embedding = NULL` on an `LSM_VECTOR`-indexed property already runs in production (`clearFactEmbeddingsStatement`).
  - `reference/sql/sql-script.adoc`: a script is one implicit transaction, all-or-nothing.
  - `reference/sql/sql-where.adoc` does not define `<>` against NULL.
    - VM, measured: `embed_space <> 'x'` counts rows whose stamp is NULL.
    - The code still writes `(embed_space IS NULL OR embed_space <> :space)` so as not to depend on it.
  - `concepts/vector-search.adoc`: before 26.10.1, updating a vector is a delete plus an insert and "forced a full graph rebuild on the first query after the next restart". A full re-embed on 26.9.1 therefore costs one graph rebuild per memory vector index after the next ArcadeDB restart. That is acceptable at memory scale (tens to thousands of vectors); it goes into the spec in Task 6.
- Integration tier (`arcadedb_integration`), locally:
  - Bring ArcadeDB up once from the repo root (Git Bash): `docker compose up -d arcadedb`.
  - The plan workspace (`.superpowers/sdd/2026-09-23-embedding-memory-family/`) holds `arcade-it.sh`, written in Task 2 Step 1.
  - Run: `wsl -e bash .superpowers/sdd/2026-09-23-embedding-memory-family/arcade-it.sh '<regex>'`.
  - Read the `-v` output: a test reported as `SKIP` did not run.

## Review Focus

1. **A stale writer during a pass.** `arcadedb-mcp`, still on the old route, writes a fact while the daemon's pass runs on the new one. The fact carries the old stamp, dense memory stays lexical, and the next run re-embeds it. Dense retrieval never opens over it. Task 6 pins this in its integration test.
2. **A GGUF replaced under a running sidecar.** Writes after the swap carry the new space. A vector computed across the swap may claim the older space, which gets re-embedded, and never the newer one. Task 2 pins this: `embedStored` reads the space before the request.
3. **The sidecar down for a whole run, or the cloud key revoked in the middle of one.** The run ends, nothing is refused, no stamp moves, and the next run resumes. Task 6 covers it with 401, 429, 503 and a transport error.
4. **A tenant with thousands of turns and a 5-minute budget.** The run stops at the budget without reporting a failure, the next run starts from whatever is still in another space, and the tenant order rotates. Task 6 covers it.
5. **Soft-deleted turns with old-space vectors.** They neither close the gate nor cost requests. If a replay revives one, the pass re-embeds it. Task 4 (gate statement) and Task 6 (selection statement) cover it.

## File Structure

| file | change |
|---|---|
| `internal/embeddings/client.go` | `StatusError`, `RejectsInput`, `ErrNoCredential`, `Client.Credential`, `key()` |
| `internal/embeddings/fit.go` | the catalogue read uses `key()` |
| `internal/embeddings/route.go` | new: `Route`, `NewRoute`, `Route.Space` |
| `internal/arcadedb/embedding.go` | `DenseEmbedder`, `NewMemoryEmbedder`; `NewSidecarEmbedder` and both aliases removed |
| `internal/arcadedb/embedding_space.go` | new: stamp DDL, `storedVector`, gate, `denseQueryVector` |
| `internal/arcadedb/memory_embed_stored.go` | new: `embedStored` (space first, halving, control input), `embedOne`, `embedDistinct` |
| `internal/arcadedb/memory_embed_pass.go` | new: the §5 pass, `storeVectors`, `EmbedMissingFacts`, `ReEmbedAllFacts` |
| `internal/arcadedb/client.go`, `tenant_clients.go` | `DenseEmbedder` field; gate field |
| `internal/arcadedb/memory.go` | `UpsertFact` stamps; `createFactEmbeddingClause` removed (−14 lines) |
| `internal/arcadedb/memory_batch.go`, `memory_batch_state.go`, `memory_batch_store.go` | `map[string]storedVector`; stamp carried, copied only within the space |
| `internal/arcadedb/memory_vector.go` | FACT stamp DDL; reasons; the gate on `SearchFactsHybrid`; the fill moves out |
| `internal/arcadedb/memory_conversation.go` | turn stamp DDL; batched projection embed; dead hybrid search removed |
| `internal/arcadedb/memory_reasoning.go`, `memory_reasoning_statements.go` | trace stamp DDL; stamped upsert; `ReasoningSearchResult` |
| `internal/arcadedb/memory_recall.go` | the gate; the one-sided ranking reason |
| `internal/arcadedb/memory_backfill.go` | the sweep drives the pass; space check first; rotation; budget |
| `internal/cron/handlers/memory_embed_backfill.go` | comments and log text |
| `cmd/arcadedb-mcp/main.go`, `boot_settings.go`, `tenant.go`, `tool_memory_recall.go` | `NewMemoryEmbedder`; boot space from the embedder; trace reason |
| `cmd/aura/memory_embedder.go` | new: the daemon's memory route with the live key |
| `cmd/aura/chat_boot.go`, `chat_boot_memory.go`, `chat_memory_projection.go`, `serve_memory_backfill.go`, `serve.go`, `serve_provisioning.go`, `document_index_wiring.go` (+3 callers) | one shared resolver; boot kick; dead parameter removed |

The spec's Files table put the pass in `memory_backfill.go` and the gate in `memory_vector.go`. This plan gives each its own file (`memory_embed_pass.go`, `embedding_space.go`), because `memory_vector.go` would otherwise cross 600 lines and both are one responsibility.

---

### Task 1: The route knows its credential, its failures and its space

**Files:**
- Modify: `internal/embeddings/client.go`
- Modify: `internal/embeddings/fit.go:55`
- Create: `internal/embeddings/route.go`
- Test: `internal/embeddings/client_test.go` (append), `internal/embeddings/route_test.go` (new)

**Interfaces:**
- Consumes: `config.ResolveEmbedRoute(embed, "")`, `config.EmbedRouteKind(embed)`, `RouteSpace(ctx, client, embed, dims)`, `SpaceFor(...)` (plan 1).
- Produces:
  - `var ErrNoCredential error`
  - `type StatusError struct{ Code int; Status string }`
  - `func RejectsInput(err error) bool`
  - `Client.Credential func() string`
  - `type Route struct{…}`
  - `func NewRoute(embed config.EmbedConfig, credential func() string, dims int, timeout time.Duration) *Route`
  - `func (r *Route) Embed(ctx, texts) ([][]float64, error)`
  - `func (r *Route) Space(ctx) (Space, error)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/embeddings/client_test.go` (add `"errors"` and `"strconv"` to its imports):

```go
// A key rotated in the cockpit must reach a running client: the daemon builds its memory
// route once, and a boot copy of the key would keep embedding with a revoked one.
func TestClientReadsTheCredentialOnEveryRequest(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	server := httptest.NewServer(withCatalogue(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"index": 0, "embedding": []float64{1, 0}},
		}})
	}))
	t.Cleanup(server.Close)
	key := "first"
	client := &Client{
		BaseURL: server.URL, Model: "model", Credential: func() string { return key },
		Client: server.Client(), Dimensions: 2,
	}
	if _, err := client.Embed(t.Context(), []string{"a"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	key = "rotated"
	if _, err := client.Embed(t.Context(), []string{"b"}); err != nil {
		t.Fatalf("Embed after rotation: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != "Bearer first" || seen[1] != "Bearer rotated" {
		t.Fatalf("authorization headers = %q, want the key current at each request", seen)
	}
}

// A hosted route whose key is empty must not send anything: OpenRouter would answer 401,
// and an unauthenticated request says nothing about the text it carried.
func TestClientRefusesAHostedRouteWithoutACredential(t *testing.T) {
	requests := 0
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
	}))
	t.Cleanup(server.Close)
	client := &Client{
		BaseURL: server.URL, Model: "model", Credential: func() string { return " " },
		Client: server.Client(), Dimensions: 2,
	}
	if _, err := client.Embed(t.Context(), []string{"a"}); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("err = %v, want ErrNoCredential", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 0 {
		t.Fatalf("%d request(s) reached the provider without a key", requests)
	}
}

// Only a refusal of the input itself says something about a text. The memory pass sets a
// record aside on RejectsInput alone; reading a 401 or a 429 as "bad text" would stamp a
// whole memory refused after one revoked key or one rate limit.
func TestRejectsInputOnlyForAnInputRefusal(t *testing.T) {
	for code, want := range map[int]bool{
		400: true, 413: true, 422: true,
		401: false, 403: false, 404: false, 429: false, 500: false, 503: false,
	} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			server := httptest.NewServer(withCatalogue(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
			}))
			t.Cleanup(server.Close)
			client := &Client{BaseURL: server.URL, Model: "model", APIKey: "key", Client: server.Client(), Dimensions: 2}
			_, err := client.Embed(t.Context(), []string{"a"})
			if err == nil {
				t.Fatalf("Embed succeeded against HTTP %d", code)
			}
			if got := RejectsInput(err); got != want {
				t.Fatalf("RejectsInput(HTTP %d) = %v, want %v (err %v)", code, got, want, err)
			}
		})
	}
	if RejectsInput(errors.New("request: connection refused")) {
		t.Fatal("a transport error was read as an input refusal")
	}
}
```

Create `internal/embeddings/route_test.go`:

```go
package embeddings

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
)

func TestNewRouteIsNilWhenDenseEmbeddingIsOff(t *testing.T) {
	if route := NewRoute(config.EmbedConfig{BaseURL: "  "}, nil, 768, 0); route != nil {
		t.Fatalf("an empty local base built %+v, want nil", route)
	}
}

// The local space is the sidecar's own answer, read at the route's width, on every call.
func TestRouteNamesTheLocalSpaceFromTheSidecar(t *testing.T) {
	server := modelsServer(t, measuredModels, http.StatusOK)
	route := NewRoute(config.EmbedConfig{BaseURL: server.URL}, nil, 768, time.Second)
	space, err := route.Space(t.Context())
	if err != nil {
		t.Fatalf("Space: %v", err)
	}
	want, err := RouteSpace(t.Context(), server.Client(), config.EmbedConfig{BaseURL: server.URL}, 768)
	if err != nil {
		t.Fatalf("RouteSpace: %v", err)
	}
	if space != want {
		t.Fatalf("space = %+v, want %+v", space, want)
	}
	if route.client.hosted() {
		t.Fatal("the local route was built as a hosted one")
	}
}

// A cloud route without its key produces no vector, so it names no space; with the key it
// is OpenRouter's, whatever the chat route is (spec §0).
func TestRouteWithoutAKeyHasNoSpace(t *testing.T) {
	cloud := config.EmbedConfig{CloudModel: "perplexity/pplx-embed-v1-0.6b"}
	if _, err := NewRoute(cloud, nil, 768, time.Second).Space(t.Context()); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("keyless cloud route: err = %v, want ErrNoCredential", err)
	}
	keyed := NewRoute(cloud, func() string { return "sk-or" }, 768, time.Second)
	space, err := keyed.Space(t.Context())
	if err != nil {
		t.Fatalf("Space: %v", err)
	}
	if want := SpaceFor(config.EmbedOpenRouter, "perplexity/pplx-embed-v1-0.6b", "", 768, ""); space != want {
		t.Fatalf("space = %+v, want %+v", space, want)
	}
	if keyed.client.BaseURL != "https://openrouter.ai/api" {
		t.Fatalf("base = %q, want OpenRouter", keyed.client.BaseURL)
	}
}
```

- [ ] **Step 2: Run the tests and watch them fail**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./internal/embeddings/ -run 'TestClientReadsTheCredential|TestClientRefusesAHosted|TestRejectsInput|TestNewRoute|TestRoute' -count=1"`
Expected: FAIL at build time: `unknown field Credential`, `undefined: ErrNoCredential`, `undefined: RejectsInput`, `undefined: NewRoute`.

- [ ] **Step 3: Implement**

In `internal/embeddings/client.go`:

1. Add `"errors"` to the imports.
2. Replace the `Client` doc comment and add the field:

```go
// Client calls an OpenAI-compatible /v1/embeddings endpoint. With neither APIKey nor
// Credential it is the local llama.cpp sidecar; either one selects a hosted route.
type Client struct {
	BaseURL string
	Model   string
	APIKey  string
	// Credential, when set, is read on every request in place of APIKey, so a key rotated
	// in the cockpit reaches a running client. Setting it marks the route hosted even while
	// it returns "": such a client refuses to embed (ErrNoCredential) rather than send an
	// unauthenticated request to a provider.
	Credential func() string
	Client     *http.Client
	Dimensions int
	BatchSize  int
	Timeout    time.Duration

	limitMu sync.Mutex
	limit   int
}
```

3. Add after the `Embedder` interface:

```go
// ErrNoCredential is a hosted route asked to embed while its credential is empty. It is the
// route's failure, never the text's, and nothing is sent.
var ErrNoCredential = errors.New("embeddings: the hosted route has no credential")

// StatusError is an answer outside 2xx from the embedding endpoint.
type StatusError struct {
	Code   int
	Status string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("endpoint returned HTTP %d (%s)", e.Code, e.Status)
}

// RejectsInput reports whether err is the endpoint refusing the input itself: 400, 413 or
// 422. Only those say something about a text; 401, 403 and 429 are about the route or the
// account, and a 5xx is about the server.
func RejectsInput(err error) bool {
	var status *StatusError
	if !errors.As(err, &status) {
		return false
	}
	switch status.Code {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return true
	}
	return false
}
```

4. In `Embed`, right after `if len(texts) == 0 { return nil, nil }`:

```go
	if c.hosted() && c.key() == "" {
		return nil, ErrNoCredential
	}
```

5. In `postJSON` replace the header line and the status check:

```go
	if c.hosted() {
		req.Header.Set("Authorization", "Bearer "+c.key())
	}
```
```go
	if resp.StatusCode/100 != 2 {
		return &StatusError{Code: resp.StatusCode, Status: resp.Status}
	}
```

6. Replace `hosted` and add `key`:

```go
func (c *Client) hosted() bool {
	return c.Credential != nil || strings.TrimSpace(c.APIKey) != ""
}

// key is this request's credential: Credential's current answer, else APIKey.
func (c *Client) key() string {
	if c.Credential != nil {
		return strings.TrimSpace(c.Credential())
	}
	return strings.TrimSpace(c.APIKey)
}
```

In `internal/embeddings/fit.go:55` replace `strings.TrimSpace(c.APIKey)` with `c.key()`. Remove the `strings` import if it becomes unused (it is still used by `strings.TrimSpace(c.Model)` on the next line, so it stays).

Create `internal/embeddings/route.go`:

```go
package embeddings

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
)

// Route is one resolved embedding route at one width: it embeds, and it names the space
// its vectors are in. The space is read, never remembered: the local route asks the
// sidecar on every call (AttestLocal), so a model swapped under a running process is named
// by the next write, not by a restart.
type Route struct {
	client *Client
	embed  config.EmbedConfig
}

// NewRoute builds the route embed resolves to, or nil when dense embedding is switched off
// (the local route with no base). credential is read on every request of a cloud route; a
// cloud route built without one refuses to embed instead of sending an unauthenticated
// request.
func NewRoute(embed config.EmbedConfig, credential func() string, dims int, timeout time.Duration) *Route {
	base, _, model := config.ResolveEmbedRoute(embed, "")
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	client := &Client{
		BaseURL: base, Model: model, Dimensions: dims,
		Client: &http.Client{Timeout: timeout}, Timeout: timeout,
	}
	if config.EmbedRouteKind(embed) != config.EmbedLocal {
		if credential == nil {
			credential = func() string { return "" }
		}
		client.Credential = credential
	}
	return &Route{client: client, embed: embed}
}

// Embed embeds texts through the route.
func (r *Route) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	return r.client.Embed(ctx, texts)
}

// Space names the space this route's vectors are in. A hosted route without a credential
// produces no vector, so it has no space either.
func (r *Route) Space(ctx context.Context) (Space, error) {
	if r.client.hosted() && r.client.key() == "" {
		return Space{}, ErrNoCredential
	}
	return RouteSpace(ctx, r.client.httpClient(), r.embed, r.client.Dimensions)
}
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./internal/embeddings/ -count=1"`
Expected: `ok  github.com/chetto1983/aura/internal/embeddings`. The whole package runs because the status error changed every non-2xx path.

- [ ] **Step 5: Commit**

```bash
git add internal/embeddings/client.go internal/embeddings/fit.go internal/embeddings/route.go internal/embeddings/client_test.go internal/embeddings/route_test.go
git commit -F - <<'EOF'
feat(embed): a route that names its space, reads its key live, types its refusals

The memory pass must tell a refused text from a refused route, and a key rotated
in the cockpit must reach a running daemon. The client now returns a typed
StatusError (RejectsInput is true for 400, 413 and 422 only), reads an optional
Credential on every request, and refuses to send a hosted request without a key
(ErrNoCredential). Route binds a client to the route it was resolved from and
names its space: the local route attests the sidecar on every call.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 2: Facts are stamped with the space that produced their vector

**Files:**
- Modify: `internal/arcadedb/embedding.go` (whole file), `client.go:186-190,411-420`, `tenant_clients.go:15,29`, `memory_backfill.go:63,73`
- Create: `internal/arcadedb/embedding_space.go`, `internal/arcadedb/memory_embed_stored.go`
- Modify: `internal/arcadedb/memory.go:288-301,407-414`, `memory_vector.go:49-61,81-126`, `memory_batch.go:237,295,371`, `memory_batch_state.go:26,48,68,117-118,212`, `memory_batch_store.go:35-74,155,195,312-315,411`
- Modify: `cmd/arcadedb-mcp/main.go:67`, `cmd/arcadedb-mcp/tenant.go:24`, `cmd/aura/chat_memory_projection.go:119-123`, `cmd/aura/serve_memory_backfill.go:62-63`, `cmd/aura/document_index_wiring.go:11-15` and its three callers
- Test: `internal/arcadedb/memory_embed_stored_test.go` (new), `internal/arcadedb/embedding_test.go` (rewrite), `internal/arcadedb/memory_space_live_integration_test.go` (new); Space methods on the existing fakes (listed in Step 5)

**Interfaces:**
- Consumes: Task 1's `embeddings.NewRoute`, `embeddings.StatusError`, `embeddings.RejectsInput`.
- Produces:
  - `type DenseEmbedder interface { Embed(ctx, []string) ([][]float64, error); Space(ctx) (embeddings.Space, error) }`
  - `func NewMemoryEmbedder(embed config.EmbedConfig, credential func() string) DenseEmbedder`
  - `func spaceStampStatements(typeName string) []string`
  - `type storedVector struct{ vector any; space string }`
  - `func (v storedVector) createClause(params map[string]any) string`
  - `func nullableString(string) any` (renamed from `nullableMemoryBatchString`)
  - `func (c *Client) embedStored(ctx, texts []string) ([]storedVector, error)`
  - `func (c *Client) embedOne(ctx, text string) storedVector`
  - `func (c *Client) embedDistinct(ctx, statements []string) map[string]storedVector`
  - `memoryBatchBackend.EmbedStatements(ctx, []string) map[string]storedVector`
  - `memoryBatchFact.EmbedSpace string`

- [ ] **Step 1: Write the integration helper and the failing tests**

Create the workspace script (the workspace comes from `sdd-workspace`):

```bash
W=.superpowers/sdd/2026-09-23-embedding-memory-family
cat > "$W/arcade-it.sh" <<'EOF'
#!/bin/bash
# $1 = go test -run regex; $2 = package (default ./internal/arcadedb/)
set -e
cd /mnt/d/Aura
set -a; . ./.env; set +a
unset AURA_MCP_SERVERS_JSON
export ARCADEDB_URL=http://127.0.0.1:2480
go test -tags arcadedb_integration "${2:-./internal/arcadedb/}" -run "$1" -count=1 -v 2>&1 | tail -60
EOF
docker compose up -d arcadedb
```

Create `internal/arcadedb/memory_embed_stored_test.go`:

```go
package arcadedb

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/embeddings"
)

// refusingEmbedder refuses, with status, every request carrying one of refuse; the control
// input is refused too when refuseControl is set.
type refusingEmbedder struct {
	refuse        []string
	refuseControl bool
	status        int
	space         string
	calls         [][]string
}

func (e *refusingEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	e.calls = append(e.calls, append([]string(nil), texts...))
	for _, text := range texts {
		if strings.HasSuffix(text, controlInput) {
			if e.refuseControl {
				return nil, &embeddings.StatusError{Code: e.status}
			}
			continue
		}
		for _, refused := range e.refuse {
			if strings.HasSuffix(text, refused) {
				return nil, &embeddings.StatusError{Code: e.status}
			}
		}
	}
	vectors := make([][]float64, len(texts))
	for i := range texts {
		vectors[i] = vectorOf(float64(i + 1))
	}
	return vectors, nil
}

func (e *refusingEmbedder) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: e.space}, nil
}

// swappingEmbedder names one space until it has embedded, and another after: a model
// swapped while a request is in flight.
type swappingEmbedder struct{ embedded bool }

func (e *swappingEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	e.embedded = true
	vectors := make([][]float64, len(texts))
	for i := range texts {
		vectors[i] = vectorOf(1)
	}
	return vectors, nil
}

func (e *swappingEmbedder) Space(context.Context) (embeddings.Space, error) {
	if e.embedded {
		return embeddings.Space{ID: "es1-new"}, nil
	}
	return embeddings.Space{ID: "es1-old"}, nil
}

func storedClient(t *testing.T, embedder DenseEmbedder) *Client {
	t.Helper()
	client, _ := recordingClient(t, `{"result":[]}`)
	return client.WithEmbedder(embedder)
}

func TestEmbedStoredStampsEveryVectorWithItsSpace(t *testing.T) {
	vectors, err := storedClient(t, &refusingEmbedder{space: "es1-a"}).
		embedStored(context.Background(), []string{"one", "two"})
	if err != nil {
		t.Fatalf("embedStored: %v", err)
	}
	for i, vector := range vectors {
		if vector.vector == nil || vector.space != "es1-a" {
			t.Fatalf("text %d = %+v, want a vector stamped es1-a", i, vector)
		}
	}
}

func TestEmbedStoredIsolatesTheOneRefusedText(t *testing.T) {
	embedder := &refusingEmbedder{refuse: []string{"poison"}, status: http.StatusBadRequest, space: "es1-a"}
	texts := []string{"alpha", "beta", "poison", "gamma", "delta"}
	vectors, err := storedClient(t, embedder).embedStored(context.Background(), texts)
	if err != nil {
		t.Fatalf("embedStored: %v", err)
	}
	for i, text := range texts {
		got := vectors[i]
		if text == "poison" {
			if got.vector != nil || got.space != "es1-a" {
				t.Fatalf("refused text = %+v, want the refusing space and no vector", got)
			}
			continue
		}
		if got.vector == nil || got.space != "es1-a" {
			t.Fatalf("%q = %+v, want embedded despite a refused neighbour", text, got)
		}
	}
	controls := 0
	for _, call := range embedder.calls {
		if len(call) == 1 && strings.HasSuffix(call[0], controlInput) {
			controls++
		}
	}
	if controls != 1 {
		t.Fatalf("control input sent %d times, want once per call", controls)
	}
}

// An unknown model id answers 400 to every input. Marking the texts would stamp a whole
// memory refused over one typo in a route.
func TestEmbedStoredBlamesTheRouteWhenTheControlIsRefusedToo(t *testing.T) {
	embedder := &refusingEmbedder{
		refuse: []string{"alpha", "beta"}, refuseControl: true, status: http.StatusBadRequest, space: "es1-a",
	}
	if _, err := storedClient(t, embedder).embedStored(context.Background(), []string{"alpha", "beta"}); err == nil {
		t.Fatal("a route refusing the control input too was read as two bad texts")
	}
}

func TestEmbedStoredNeverSplitsARouteFailure(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500, 503} {
		embedder := &refusingEmbedder{refuse: []string{"alpha"}, status: status, space: "es1-a"}
		if _, err := storedClient(t, embedder).embedStored(context.Background(), []string{"alpha", "beta"}); err == nil {
			t.Fatalf("HTTP %d was absorbed", status)
		}
		if len(embedder.calls) != 1 {
			t.Fatalf("HTTP %d was split into %d requests; only an input refusal is split", status, len(embedder.calls))
		}
	}
}

// Review Focus 2: read after the request, a vector from the old model could claim the new
// space and open the gate over it.
func TestEmbedStoredReadsTheSpaceBeforeTheRequest(t *testing.T) {
	vectors, err := storedClient(t, &swappingEmbedder{}).embedStored(context.Background(), []string{"alpha"})
	if err != nil {
		t.Fatalf("embedStored: %v", err)
	}
	if vectors[0].space != "es1-old" {
		t.Fatalf("stamp = %q, want the space read before the request", vectors[0].space)
	}
}

func TestEmbedStoredRefusesAnUnnamedSpace(t *testing.T) {
	if _, err := storedClient(t, &refusingEmbedder{}).embedStored(context.Background(), []string{"alpha"}); err == nil {
		t.Fatal("vectors were produced in a space with no name")
	}
}

func TestEmbedOneIsFailSoft(t *testing.T) {
	// Compared by field: storedVector holds a slice, and == on one would panic, not fail.
	var none *Client
	if got := none.embedOne(context.Background(), "fact"); got.vector != nil || got.space != "" {
		t.Fatalf("nil client = %+v", got)
	}
	down := storedClient(t, &refusingEmbedder{refuse: []string{"fact"}, status: http.StatusServiceUnavailable, space: "es1-a"})
	if got := down.embedOne(context.Background(), "fact"); got.vector != nil || got.space != "" {
		t.Fatalf("a route failure = %+v, want nothing stored", got)
	}
}

func TestUpsertFactStampsTheSpaceThatRefusedIt(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	client.WithEmbedder(&refusingEmbedder{
		refuse: []string{validFact().Statement}, status: http.StatusBadRequest, space: "es1-a",
	})
	if _, err := client.UpsertFact(context.Background(), validFact(), now); err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	edge, params := rec.statements[len(rec.statements)-1], rec.params[len(rec.params)-1]
	if strings.Contains(edge, "embedding = :embedding") || !strings.Contains(edge, "embed_space = :embed_space") {
		t.Fatalf("a refused fact must carry the refusing space and no vector:\n%s", edge)
	}
	if params["embed_space"] != "es1-a" {
		t.Fatalf("embed_space = %v, want es1-a", params["embed_space"])
	}
}

func TestFactSchemaStampsTheSpaceWithIndexedNulls(t *testing.T) {
	joined := strings.Join(vectorSchemaStatements(), "\n")
	for _, want := range []string{
		"CREATE PROPERTY FACT.embed_space IF NOT EXISTS STRING",
		"CREATE INDEX IF NOT EXISTS ON FACT (embed_space) NOTUNIQUE NULL_STRATEGY INDEX",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("FACT schema lacks %q", want)
		}
	}
}
```

Rewrite `internal/arcadedb/embedding_test.go`. The transport tests it held duplicated `internal/embeddings/client_test.go` and go with `NewSidecarEmbedder`:

```go
package arcadedb

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
)

// A nil *embeddings.Route in a DenseEmbedder would be non-nil, and every "no embedder"
// branch would call through it.
func TestNewMemoryEmbedderIsABareNilWhenDenseRetrievalIsOff(t *testing.T) {
	if embedder := NewMemoryEmbedder(config.EmbedConfig{BaseURL: " "}, nil); embedder != nil {
		t.Fatalf("got %#v, want a bare nil", embedder)
	}
}

func TestNewMemoryEmbedderNamesItsSpaceAtTheIndexWidth(t *testing.T) {
	embedder := NewMemoryEmbedder(config.EmbedConfig{CloudModel: "vendor/embed"}, func() string { return "key" })
	space, err := embedder.Space(context.Background())
	if err != nil {
		t.Fatalf("Space: %v", err)
	}
	if want := embeddings.SpaceFor(config.EmbedOpenRouter, "vendor/embed", "", vectorDimensions, ""); space != want {
		t.Fatalf("space = %+v, want %+v", space, want)
	}
	keyless := NewMemoryEmbedder(config.EmbedConfig{CloudModel: "vendor/embed"}, nil)
	if _, err := keyless.Space(context.Background()); !errors.Is(err, embeddings.ErrNoCredential) {
		t.Fatalf("keyless cloud route: err = %v, want ErrNoCredential", err)
	}
}
```

Create `internal/arcadedb/memory_space_live_integration_test.go`:

```go
//go:build arcadedb_integration

// The stamp, against a live ArcadeDB. The unit tests prove what is written; this proves the
// index does not hide the rows the gate and the pass must find. With ArcadeDB's default null
// strategy an unstamped row answers no query that uses the index (arcadedb-docs
// reference/sql/sql-indexes.adoc), and the gate would open over it.
//
// Run: arcade-it.sh MemorySpace
package arcadedb

import (
	"context"
	"testing"
	"time"
)

func TestMemorySpaceStampsFactsAndFindsTheUnstampedThroughTheIndex(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	now := time.Now().UTC()
	write := func(embedder DenseEmbedder, subject string) {
		t.Helper()
		fact := mergeFact(subject, "knows", "SpaceObject", subject+" knows the space object.")
		if _, err := client.WithEmbedder(embedder).UpsertFact(ctx, fact, now); err != nil {
			t.Fatalf("UpsertFact(%s): %v", subject, err)
		}
	}
	write(constantEmbedder{value: 1, space: "es1-route-a"}, "SpaceA")
	write(constantEmbedder{value: 2, space: "es1-route-b"}, "SpaceB")
	write(nil, "SpaceNone")

	count := func(where string, params map[string]any) int {
		t.Helper()
		rows, err := client.Query(ctx, "SELECT count(*) AS n FROM "+factEdgeType+" WHERE "+where, params)
		if err != nil {
			t.Fatalf("count %q: %v", where, err)
		}
		return int(rowInt(rows[0], "n"))
	}
	if n := count("embed_space IS NULL", nil); n != 1 {
		t.Fatalf("unstamped facts = %d, want 1: the stamp index hides NULLs", n)
	}
	if n := count("embed_space = :s", map[string]any{"s": "es1-route-a"}); n != 1 {
		t.Fatalf("facts in es1-route-a = %d, want 1", n)
	}
	if n := count("embedding IS NOT NULL AND (embed_space IS NULL OR embed_space <> :space)",
		map[string]any{"space": "es1-route-a"}); n != 1 {
		t.Fatalf("vectors outside es1-route-a = %d, want 1 (the es1-route-b fact)", n)
	}
}
```

- [ ] **Step 2: Run the tests and watch them fail**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./internal/arcadedb/ -run 'TestEmbedStored|TestEmbedOne|TestUpsertFactStamps|TestFactSchemaStamps|TestNewMemoryEmbedder' -count=1"`
Expected: FAIL at build time: `undefined: controlInput`, `undefined: DenseEmbedder`, `undefined: NewMemoryEmbedder`, `c.embedStored undefined`.

- [ ] **Step 3: Implement the embedder, the stamp and the stored-embedding helper**

Replace `internal/arcadedb/embedding.go` entirely:

```go
package arcadedb

import (
	"context"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
)

// DenseEmbedder is the memory dense leg: it embeds, and it names the space its vectors are
// in, so every stored vector carries that space (spec §2) and every dense read can check
// the corpus is in it (spec §3). Optional: with none, memory retrieval is the lexical leg
// alone, which is the behaviour that shipped.
type DenseEmbedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
	Space(ctx context.Context) (embeddings.Space, error)
}

// EmbeddingGemma's query and stored-text prefixes are asymmetric. Memory facts
// are stored retrieval documents; natural-language searches are queries.
const (
	taskQueryPrefix    = embeddings.QueryPrefix
	taskDocumentPrefix = embeddings.UntitledDocumentPrefix
)

func withTask(prefix string, texts []string) []string {
	return embeddings.Prefix(prefix, texts)
}

// NewMemoryEmbedder resolves the memory family's route at the index width, or returns nil
// when dense embedding is switched off. The result is an interface on purpose: a nil
// *embeddings.Route stored in one is non-nil, and every "no embedder" branch would call
// through it. credential is read on every request of a cloud route.
func NewMemoryEmbedder(embed config.EmbedConfig, credential func() string) DenseEmbedder {
	route := embeddings.NewRoute(embed, credential, vectorDimensions, DefaultTimeout)
	if route == nil {
		return nil
	}
	return route
}
```

In `internal/arcadedb/client.go` change the field and `WithEmbedder`:

```go
	// embedder is optional: with none, memory retrieval is the lexical leg alone,
	// which is the behaviour that shipped and must not regress when it is absent.
	embedder DenseEmbedder
```
```go
// WithEmbedder attaches the dense leg. A nil embedder is legal and leaves
// retrieval lexical, so a caller can pass whatever configuration produced
// without branching.
func (c *Client) WithEmbedder(e DenseEmbedder) *Client {
```

In `internal/arcadedb/tenant_clients.go` and `internal/arcadedb/memory_backfill.go`, change every `embedder Embedder` / `embedder    Embedder` to `DenseEmbedder` (the struct fields and the `NewTenantClients` / `NewTenantBackfill` parameters).

Create `internal/arcadedb/embedding_space.go`:

```go
package arcadedb

// The embedding space every memory vector is stamped with (spec §2).
//
// Vectors from two models do not share a space, and nothing errors when they mix:
// vector.neighbors still returns its k nearest and the answers quietly get worse. So every
// stored vector carries the space that produced it (embeddings.Space.ID), and dense
// retrieval runs only over a corpus entirely in the reader's space (spec §3).

// spaceStampStatements declare the stamp beside a type's vector.
//
// NULL_STRATEGY INDEX is load-bearing. ArcadeDB's default, SKIP, keeps nulls out of the
// index, and "queries against null values that use an index return no entries"
// (arcadedb-docs reference/sql/sql-indexes.adoc): every unstamped row -- after an upgrade,
// all of them -- would vanish from the gate's count and the pass's selection, and the gate
// would open over vectors it never checked.
func spaceStampStatements(typeName string) []string {
	return []string{
		"CREATE PROPERTY " + typeName + ".embed_space IF NOT EXISTS STRING",
		"CREATE INDEX IF NOT EXISTS ON " + typeName + " (embed_space) NOTUNIQUE NULL_STRATEGY INDEX",
	}
}

// storedVector is what embedding one text contributes to the row that stores it: a vector
// and the space that produced it; the space alone, when that space refused the text; or
// neither, when no route answered and the row is left for the pass (facts, traces) or the
// conversation reconciler (turns) to fill.
type storedVector struct {
	vector any // []float64 from the embedder, or a stored value carried over unchanged
	space  string
}

// createClause extends a CREATE with what v stores, and with nothing when it stores
// nothing: a new row without a vector is already unstamped.
//
// The vector used to have a clause of its own for the same reason, and leaving it off was a
// real defect: the vector was computed, bound as a parameter, and dropped because no SET
// named it. ArcadeDB accepts unused parameters silently, so every fact was stored without
// its vector and nothing said so.
func (v storedVector) createClause(params map[string]any) string {
	switch {
	case v.vector != nil:
		params["embedding"], params["embed_space"] = v.vector, nullableString(v.space)
		return ", embedding = :embedding, embed_space = :embed_space"
	case v.space != "":
		params["embed_space"] = v.space
		return ", embed_space = :embed_space"
	}
	return ""
}
```

Create `internal/arcadedb/memory_embed_stored.go`:

```go
package arcadedb

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/embeddings"
)

// controlInput tells a refused text from a refused route. An endpoint answers 400 to every
// input when the model id is wrong; read as "these texts are bad", one typo in a route
// would stamp a whole memory refused. A route that embeds this and refuses a text has
// refused the text.
const controlInput = "Aura memory embedding route check."

// embedStored embeds texts as stored documents: one storedVector per text, in order.
//
// The space is read BEFORE the request, and that order is the safe one. A model swapped
// during the request can then only make a vector claim the older space, which the pass
// re-embeds; read afterwards, an old model's vector could claim the new space and open the
// gate over it.
//
// A request the endpoint refuses as input (embeddings.RejectsInput) is split in halves
// until each refused text stands alone, and only that text is marked refused -- its
// storedVector carries the space and no vector -- once controlInput has shown the route
// itself works. Any other failure belongs to the route and is returned; nothing is marked.
// A vector of the wrong width is neither, and that text gets an empty storedVector.
func (c *Client) embedStored(ctx context.Context, texts []string) ([]storedVector, error) {
	space, err := c.embedder.Space(ctx)
	if err != nil {
		return nil, err
	}
	if space.ID == "" {
		return nil, fmt.Errorf("arcadedb: the embedding route named no space")
	}
	out := make([]storedVector, len(texts))
	routeWorks := false
	var embed func(lo, hi int) error
	embed = func(lo, hi int) error {
		vectors, err := c.embedder.Embed(ctx, withTask(taskDocumentPrefix, texts[lo:hi]))
		switch {
		case err == nil:
			for i, vector := range vectors {
				if i < hi-lo && len(vector) == vectorDimensions {
					out[lo+i] = storedVector{vector: vector, space: space.ID}
				}
			}
			return nil
		case !embeddings.RejectsInput(err):
			return err
		case hi-lo > 1:
			mid := lo + (hi-lo)/2
			if err := embed(lo, mid); err != nil {
				return err
			}
			return embed(mid, hi)
		}
		if !routeWorks {
			if _, controlErr := c.embedder.Embed(ctx, withTask(taskDocumentPrefix, []string{controlInput})); controlErr != nil {
				return fmt.Errorf("arcadedb: the embedding route refuses a control input too (%v): %w", controlErr, err)
			}
			routeWorks = true
		}
		out[lo] = storedVector{space: space.ID}
		return nil
	}
	if len(texts) > 0 {
		if err := embed(0, len(texts)); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// embedOne is embedStored for a single-text write, and fail-soft: a route that cannot
// answer leaves the text with neither vector nor stamp, and the write goes ahead. A fact
// that was not stored is lost; one stored without a vector is found lexically today and
// embedded by the pass later.
func (c *Client) embedOne(ctx context.Context, text string) storedVector {
	if c == nil || c.embedder == nil || strings.TrimSpace(text) == "" {
		return storedVector{}
	}
	vectors, err := c.embedStored(ctx, []string{text})
	if err != nil {
		return storedVector{}
	}
	return vectors[0]
}

// embedDistinct embeds each distinct non-blank statement once, in one request, keyed by
// statement: a batch may restate a fact, and the vector is a pure function of the text.
// Fail-soft like embedOne: a statement the route did not answer has no entry.
func (c *Client) embedDistinct(ctx context.Context, statements []string) map[string]storedVector {
	out := make(map[string]storedVector, len(statements))
	seen := make(map[string]bool, len(statements))
	unique := make([]string, 0, len(statements))
	for _, statement := range statements {
		if seen[statement] || strings.TrimSpace(statement) == "" {
			continue
		}
		seen[statement] = true
		unique = append(unique, statement)
	}
	if len(unique) == 0 || c == nil || c.embedder == nil {
		return out
	}
	vectors, err := c.embedStored(ctx, unique)
	if err != nil {
		return out
	}
	for i, statement := range unique {
		if vectors[i].vector != nil || vectors[i].space != "" {
			out[statement] = vectors[i]
		}
	}
	return out
}
```

- [ ] **Step 4: Stamp the fact writes**

`internal/arcadedb/memory_vector.go`:
- `vectorSchemaStatements` returns the existing two statements `append`ed with `spaceStampStatements(factEdgeType)...`.
- Delete `embedStatements` (lines 81-126); `embedDistinct` replaces it.
- Leave `embedStatement` in place: turns and traces use it until Task 3.

`internal/arcadedb/memory.go`:
- Delete `createFactEmbeddingClause` and its comment (lines 288-301); its history now lives on `createClause`.
- In `UpsertFact` replace the block from `statement := createFactStatement` to the closing brace of `if vector := c.embedStatement(...)` with:

```go
	// Stored with the space that produced its vector, or with the space that refused it;
	// with neither when no route answered, for the memory_embed_backfill pass to fill.
	statement := createFactStatement + c.embedOne(ctx, fact.Statement).createClause(params)
```

`internal/arcadedb/memory_batch.go`:
- `Embedding any` gains a sibling field `EmbedSpace string` in `memoryBatchFact`.
- The `EmbedStatements` interface method returns `map[string]storedVector`, and the `embeddings map[string][]float64` parameter of `applyMemoryBatchAttempt` (line 371) becomes `map[string]storedVector`.
- Update the method's comment: "Fail-soft: a statement with no entry is written without a vector; an entry with a space and no vector is one the model refused."

`internal/arcadedb/memory_batch_state.go`:
- The three `embeddings map[string][]float64` parameters become `map[string]storedVector`.
- At line 117:

```go
	if vector, ok := embeddings[fact.Statement]; ok {
		stored.Embedding, stored.EmbedSpace = vector.vector, vector.space
	}
```
- At line 212: `fact.Embedding, fact.EmbedSpace = nil, ""`.

`internal/arcadedb/memory_batch_store.go`:
- Rename `nullableMemoryBatchString` to `nullableString` (definition and its callers in this file).
- Replace the lookup constant, `EmbedStatements` and `storedStatementVectors`:

```go
// storedStatementVectorsStatement reuses a stored vector only when it is in the space the
// batch embeds in: copying one from another space would stamp an old model's vector with
// the new space and open the gate over it (spec §2).
const storedStatementVectorsStatement = "SELECT statement, embedding FROM " + factEdgeType +
	" WHERE statement IN :statements AND embedding IS NOT NULL AND embed_space = :space"

// EmbedStatements reuses the vector of every statement the store already holds in the
// batch's space and sends only the rest to the embedder. A capture restates the fact its
// tool call has just written, so without the lookup every explicit fact was embedded twice.
func (backend clientMemoryBatchBackend) EmbedStatements(
	ctx context.Context,
	statements []string,
) map[string]storedVector {
	client := backend.client
	if client == nil || client.embedder == nil || len(statements) == 0 {
		return nil
	}
	space, err := client.embedder.Space(ctx)
	if err != nil {
		return nil
	}
	vectors := client.storedStatementVectors(ctx, statements, space.ID)
	missing := make([]string, 0, len(statements))
	for _, statement := range statements {
		if _, stored := vectors[statement]; !stored {
			missing = append(missing, statement)
		}
	}
	maps.Copy(vectors, client.embedDistinct(ctx, missing))
	return vectors
}

// storedStatementVectors is fail-soft like the embedder it saves a call to: a lookup that
// cannot be served only means every statement is embedded, never that the batch fails.
func (c *Client) storedStatementVectors(ctx context.Context, statements []string, space string) map[string]storedVector {
	vectors := make(map[string]storedVector, len(statements))
	rows, err := c.Query(ctx, storedStatementVectorsStatement, map[string]any{"statements": statements, "space": space})
	if err != nil {
		return vectors
	}
	for _, row := range rows {
		if vector := rowVector(row, "embedding"); vector != nil {
			vectors[rowString(row, "statement")] = storedVector{vector: vector, space: space}
		}
	}
	return vectors
}
```
- In the fact load (line 155), add `embed_space, ` after `embedding, `; in the `memoryBatchFact` literal (line 195) add `EmbedSpace: rowString(row, "embed_space"),`.
- In `createFact`, replace the `statement := createFactStatement` block with:

```go
	statement := createFactStatement +
		storedVector{vector: fact.Embedding, space: fact.EmbedSpace}.createClause(params)
```

- [ ] **Step 5: Move every caller and fake to the new seam**

Production callers:
- `cmd/arcadedb-mcp/main.go:67`:

```go
	embedder := arcadedb.NewMemoryEmbedder(embedRoute.embed, func() string { return embedRoute.apiKey })
```
- `cmd/arcadedb-mcp/tenant.go:24`: `embedder arcadedb.DenseEmbedder,`.
- `cmd/aura/chat_memory_projection.go:119-123`: delete `embedURL, apiKey, model := cfg.EmbedRoute()` and pass the embedder directly. Task 7 replaces the key closure with the live one.

```go
	clients := arcadedb.NewTenantClients(
		arcadedb.Config{BaseURL: cfg.ArcadeDB.BaseURL}, admin,
		arcadedb.NewMemoryEmbedder(cfg.Embed, func() string { return cfg.LLM.APIKey }), credentials,
	)
```
- `cmd/aura/serve_memory_backfill.go:62-63`:

```go
	embedder := arcadedb.NewMemoryEmbedder(chat.cfg.Embed, func() string { return chat.cfg.LLM.APIKey })
```
- `cmd/aura/document_index_wiring.go`: the `embedder arcadedb.Embedder` parameter is always nil. Remove it, pass `nil` to `NewTenantClients`, and drop the argument at `document_open_wiring.go:42`, `document_processor_wiring.go:72`, `document_retrieval_wiring.go:33` and `document_agent_live_test.go:135`.

Test fakes gain a `Space` method. Each file needs `"github.com/chetto1983/aura/internal/embeddings"` imported.
- `internal/arcadedb/memory_vector_test.go`: add `spaceErr error` to `stubEmbedder` and

```go
// stubSpace is the space every stubEmbedder vector is in.
const stubSpace = "es1-stub"

func (s *stubEmbedder) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: stubSpace}, s.spaceErr
}
```
- `internal/arcadedb/memory_backfill_test.go`:

```go
func (b *batchEmbedder) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: "es1-batch"}, nil
}
```
  and `testBackfill`'s parameter becomes `embedder DenseEmbedder`.
- `internal/arcadedb/memory_reembed_live_integration_test.go`: `constantEmbedder` gains `space string` and

```go
// Space is the embedder's own when set, else one derived from its value, so two constant
// embedders are two routes.
func (e constantEmbedder) Space(context.Context) (embeddings.Space, error) {
	if e.space != "" {
		return embeddings.Space{ID: e.space}, nil
	}
	return embeddings.Space{ID: fmt.Sprintf("es1-constant-%g", e.value)}, nil
}
```
- `internal/arcadedb/memory_conversation_live_test.go`:

```go
func (e *countingEmbedder) Space(ctx context.Context) (embeddings.Space, error) {
	return constantEmbedder{value: 1}.Space(ctx)
}
```
- `internal/arcadedb/memory_vector_live_test.go`: `liveEmbedder` returns `DenseEmbedder`, built as `NewMemoryEmbedder(config.EmbedConfig{BaseURL: embedURL}, nil)` (the local sidecar needs no model name). Drop the now-unused `time` import if it goes unused.
- `cmd/arcadedb-mcp/tool_memory_recall_test.go`:

```go
func (s recallStubEmbedder) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: "es1-recall-stub"}, nil
}
```
- `cmd/arcadedb-mcp/memory_live_integration_test.go`:

```go
func (agentMemoryLiveUnavailableEmbedder) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{}, errors.New("forced live embedding fallback")
}
```
- `cmd/arcadedb-mcp/memory_live_integration_helpers_test.go:97` and `memory_recall_live_integration_test.go:88`:

```go
	embedder := arcadedb.NewMemoryEmbedder(
		config.EmbedConfig{BaseURL: embedURL, CloudModel: os.Getenv("AURA_EMBED_MODEL")},
		func() string { return os.Getenv("OPENROUTER_API_KEY") })
```
  In the recall file the URL expression is `agentMemoryLiveEnv("AURA_EMBED_BASE_URL", "http://127.0.0.1:8081")`. Add the `config` import and drop `time` if it goes unused.
- `internal/documents/retrieval_fusion_bench_test.go:129` and `retrieval_recall_bench_test.go:58` (tag `retrieval_eval`): replace `arcadedb.NewSidecarEmbedder(<url>, "embeddinggemma", "", 2*time.Minute)` with `arcadedb.NewMemoryEmbedder(config.EmbedConfig{BaseURL: <url>}, nil)`.
- `internal/arcadedb/memory_batch_embedding_test.go`:
  - `memoryBatchFakeBackend.EmbedStatements` returns `map[string]storedVector` with `storedVector{vector: make([]float64, vectorDimensions), space: "es1-fake"}` per statement.
  - `TestMemoryBatch_EmbedsCreatedFacts` additionally asserts `fact.EmbedSpace == "es1-fake"`.
  - In `TestMemoryBatchEmbedReusesStoredStatementVectors`:
    - read `vectors[statement].vector.([]float64)`;
    - assert every entry's `space` is `stubSpace`;
    - record the lookup's params and assert `params["space"] == stubSpace`.
- `internal/arcadedb/memory_batch_live_test.go:215`: `memoryBatchLiveBackend.EmbedStatements` returns `map[string]storedVector`.
- `internal/arcadedb/memory_vector_test.go`, `TestUpsertFactStoresTheVectorItComputed`: add

```go
	if !strings.Contains(edge, "embed_space = :embed_space") || params["embed_space"] != stubSpace {
		t.Fatalf("the vector is stored without the space that produced it:\n%s\nparams=%v", edge, params)
	}
```

- [ ] **Step 6: Run the unit tests, then the integration test**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./internal/embeddings/ ./internal/arcadedb/ ./cmd/arcadedb-mcp/ -count=1 && go vet -tags 'arcadedb_integration retrieval_eval document_live_e2e' ./internal/arcadedb/ ./internal/documents/ ./cmd/arcadedb-mcp/ ./cmd/aura/"`
Expected: `ok` for the three packages and a silent vet (the tagged files compile).

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./cmd/aura/ -run 'Memory|Backfill|DocumentIndex|Doctor' -count=1"`
Expected: `ok`.

Run: `wsl -e bash .superpowers/sdd/2026-09-23-embedding-memory-family/arcade-it.sh 'MemorySpace|ReEmbedAllFacts|MemoryBatchLive|MemoryVectorAnswers'`
Expected: `--- PASS: TestMemorySpaceStampsFactsAndFindsTheUnstampedThroughTheIndex`, `--- PASS: TestReEmbedAllFactsReachesTheWholeCorpusNotTheFirstBatch`, and none `SKIP`ped for a missing `ARCADEDB_URL`. A test that needs the embedding sidecar may skip locally; say so in the task report.

- [ ] **Step 7: Record the stamp's two rules in the spec**

In `docs/superpowers/specs/2026-09-23-embedding-model-change-design.md` §2, after the first bullet, add:

```markdown
  The index is `NOTUNIQUE NULL_STRATEGY INDEX`: with ArcadeDB's default, `SKIP`, "queries
  against null values that use an index return no entries" (arcadedb-docs
  `reference/sql/sql-indexes.adoc`), so every unstamped row would be invisible to the gate.
- A writer reads its space **before** the request that produces the vector. A model swapped
  during the request can then only make a vector claim the older space, which the pass
  re-embeds, never the newer one.
```

- [ ] **Step 8: Commit**

```bash
git add internal/arcadedb/embedding.go internal/arcadedb/embedding_space.go internal/arcadedb/memory_embed_stored.go \
  internal/arcadedb/client.go internal/arcadedb/tenant_clients.go internal/arcadedb/memory_backfill.go \
  internal/arcadedb/memory.go internal/arcadedb/memory_vector.go internal/arcadedb/memory_batch.go \
  internal/arcadedb/memory_batch_state.go internal/arcadedb/memory_batch_store.go \
  internal/arcadedb/embedding_test.go internal/arcadedb/memory_embed_stored_test.go \
  internal/arcadedb/memory_space_live_integration_test.go internal/arcadedb/memory_vector_test.go \
  internal/arcadedb/memory_backfill_test.go internal/arcadedb/memory_reembed_live_integration_test.go \
  internal/arcadedb/memory_conversation_live_test.go internal/arcadedb/memory_vector_live_test.go \
  internal/arcadedb/memory_batch_embedding_test.go internal/arcadedb/memory_batch_live_test.go \
  internal/documents/retrieval_fusion_bench_test.go internal/documents/retrieval_recall_bench_test.go \
  cmd/arcadedb-mcp/main.go cmd/arcadedb-mcp/tenant.go cmd/arcadedb-mcp/tool_memory_recall_test.go \
  cmd/arcadedb-mcp/memory_live_integration_test.go cmd/arcadedb-mcp/memory_live_integration_helpers_test.go \
  cmd/arcadedb-mcp/memory_recall_live_integration_test.go \
  cmd/aura/chat_memory_projection.go cmd/aura/serve_memory_backfill.go cmd/aura/document_index_wiring.go \
  cmd/aura/document_open_wiring.go cmd/aura/document_processor_wiring.go cmd/aura/document_retrieval_wiring.go \
  cmd/aura/document_agent_live_test.go docs/superpowers/specs/2026-09-23-embedding-model-change-design.md
git commit -F - <<'EOF'
feat(memory): stamp every fact vector with the space that produced it

A fact vector is only comparable with vectors from the same model, and nothing
recorded which model that was. FACT gains embed_space, indexed with
NULL_STRATEGY INDEX because the default hides unstamped rows from any indexed
query. Every fact write goes through embedStored:
- it reads the space before the request;
- it splits an input refusal until the refused text stands alone, and marks it
  refused only once a control input embeds;
- it stores the vector and its stamp in the same statement.
The batch path copies a stored vector only within the writer's space.
NewMemoryEmbedder replaces NewSidecarEmbedder and returns a bare nil when dense
retrieval is off; the daemon's projection resolver had been handed a typed nil.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 3: Turns and traces are stamped; turn embeds are batched per projection

**Files:**
- Modify: `internal/arcadedb/embedding_space.go` (add `replaceClause`), `memory_conversation.go:66-98,126-127,138-226`, `memory_reasoning.go:22-44,188-219`, `memory_reasoning_statements.go:19-20`, `memory_vector.go:63-79`
- Test: `internal/arcadedb/memory_conversation_test.go:154-230`, `memory_reasoning_test.go:59-113`, `memory_conversation_live_test.go:89-92`, `internal/runner/runner_memory_projection_test.go:87-90`, `internal/arcadedb/memory_vector_test.go:201-230`

**Interfaces:**
- Consumes: `storedVector`, `embedStored`, `embedOne`, `spaceStampStatements`, `nullableString`, `refusingEmbedder` (test), `stubSpace` (test).
- Produces:
  - `func (v storedVector) replaceClause(params map[string]any) string`
  - `func (c *Client) embedChangedTurns(ctx, turns []ConversationTurnProjection, answered map[int]string) map[int]storedVector`
  - `storedTurnVectorsStatement` now selects turns with a vector or a stamp.

- [ ] **Step 1: Write the failing tests**

In `internal/arcadedb/memory_conversation_test.go`, rewrite the table and the detection of `TestConversationProjectionReplayEmbedsOnlyWhatChanged`:
- The table gets `wantCleared bool` in place of `wantRemoved`, plus a `refuse bool` column.
- The embedder becomes `var embedder DenseEmbedder` with `stub := &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}` (err set when `embedderDown`). When `refuse` is set, use `&refusingEmbedder{refuse: []string{content}, status: http.StatusBadRequest, space: stubSpace}` instead.
- `wantEmbeds` counts the calls of whichever fake is in use: add `embeds := func() int` returning `len(stub.calls)` or the refusing fake's non-control calls.

```go
	tests := []struct {
		name         string
		stored       string
		embedderDown bool
		refuse       bool
		wantEmbeds   int
		wantWritten  bool
		wantCleared  bool
		wantStamp    any
	}{
		{"unchanged turn keeps its stored vector", storedWithVector(conversationContentHash(content)), false, false, 0, false, false, nil},
		{"edited turn is embedded again", older, false, false, 1, true, false, stubSpace},
		{"turn stored without a vector is embedded", `{"result":[]}`, false, false, 1, true, false, stubSpace},
		{"embedder down keeps a vector that still matches", storedWithVector(conversationContentHash(content)), true, false, 0, false, false, nil},
		{"embedder down still clears a vector of older content", older, true, false, 1, false, true, nil},
		{"a refused turn keeps the space that refused it", `{"result":[]}`, false, true, 1, false, true, stubSpace},
	}
```
and the detection:

```go
			var wroteTurn, wroteVector, clearedVector bool
			var stamp any
			for _, request := range *requests {
				statement, _ := request.Payload["command"].(string)
				if !strings.Contains(statement, "content_hash = :content_hash") {
					continue
				}
				wroteTurn = true
				if !strings.Contains(statement, "embedding = :embedding") {
					continue
				}
				params, _ := request.Payload["params"].(map[string]any)
				stamp = params["embed_space"]
				if params["embedding"] != nil {
					wroteVector = true
				} else {
					clearedVector = true
				}
			}
			if !wroteTurn {
				t.Fatal("replay no longer writes the turn it exists to repair")
			}
			if wroteVector != tt.wantWritten || clearedVector != tt.wantCleared || stamp != tt.wantStamp {
				t.Fatalf("vector written=%v cleared=%v stamp=%v, want written=%v cleared=%v stamp=%v",
					wroteVector, clearedVector, stamp, tt.wantWritten, tt.wantCleared, tt.wantStamp)
			}
```
Add `"net/http"` to the imports. The refusing case counts one embed. The control input is a second call, and `embeds()` excludes it: `for _, call := range f.calls { if !strings.HasSuffix(call[0], controlInput) { n++ } }`.

Append a batching test to the same file:

```go
// The reconciler replays every conversation once a minute; a projection of several changed
// turns must reach the embedder as ONE request, not one per turn (spec §5, Turns).
func TestConversationProjectionEmbedsItsChangedTurnsInOneRequest(t *testing.T) {
	embedder := &stubEmbedder{vectors: [][][]float64{{vectorOf(1), vectorOf(2), vectorOf(3)}}}
	client, _ := routedClient(t, func(recordedRequest) testResponse { return testResponse{Body: `{"result":[]}`} })
	client.WithEmbedder(embedder)
	projection := ConversationProjection{IdentityID: "identity-a", ConversationID: "conversation-1"}
	for seq := 1; seq <= 3; seq++ {
		content := fmt.Sprintf("turn number %d", seq)
		projection.Turns = append(projection.Turns, ConversationTurnProjection{
			IdentityID: "identity-a", ConversationID: "conversation-1", Seq: seq,
			Role: "user", Content: content, ContentHash: conversationContentHash(content),
			OccurredAt: time.Date(2026, 9, 23, 6, 0, seq, 0, time.UTC),
			SourceRef:  fmt.Sprintf("postgres://conversation/conversation-1/turn/%d", seq),
		})
	}
	if err := client.ApplyConversationProjection(context.Background(), projection); err != nil {
		t.Fatalf("ApplyConversationProjection: %v", err)
	}
	if len(embedder.calls) != 1 || len(embedder.calls[0]) != 3 {
		t.Fatalf("embedder calls = %v, want one request carrying the three turns", embedder.calls)
	}
}
```
Add `"fmt"` if absent.

In `internal/arcadedb/memory_reasoning_test.go`, `expectedReasoningSchemaStatements` gains the two stamp statements:

```go
		"CREATE PROPERTY ReasoningTrace.embed_space IF NOT EXISTS STRING",
		"CREATE INDEX IF NOT EXISTS ON ReasoningTrace (embed_space) NOTUNIQUE NULL_STRATEGY INDEX",
```
and append:

```go
func TestUpsertReasoningTraceStoresItsVectorWithTheSpace(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(3)}}})
	if err := client.UpsertReasoningTrace(context.Background(), validReasoningTrace()); err != nil {
		t.Fatalf("UpsertReasoningTrace: %v", err)
	}
	statement, params, ok := findRecordedStatement(rec, "provider_summary = :provider_summary")
	if !ok {
		t.Fatal("no trace upsert recorded")
	}
	if !strings.Contains(statement, "embedding = :embedding, embed_space = :embed_space") ||
		params["embed_space"] != stubSpace {
		t.Fatalf("trace stored without its space:\n%s\nparams=%v", statement, params)
	}
	if _, _, cleared := findRecordedStatement(rec, "REMOVE embedding"); cleared {
		t.Fatal("a second statement clears the vector; the upsert sets both columns itself")
	}
}
```

In `TestConversationSchemaStatements` (`memory_conversation_test.go:25-28`) add `"embed_space"` and `"NULL_STRATEGY INDEX"` to `required`.

In `internal/arcadedb/memory_vector_test.go` delete `TestEmbedStatementIsFailSoft`; `TestEmbedOneIsFailSoft` (Task 2) replaces it.

- [ ] **Step 2: Run the tests and watch them fail**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./internal/arcadedb/ -run 'TestConversationProjection|TestUpsertReasoningTrace|TestReasoningSchema|TestConversationSchema' -count=1"`
Expected: FAIL. The refused case writes no stamp, and the batching test sees three calls of one text. The trace upsert has no `embed_space`, and the schema tests lack the stamp.

- [ ] **Step 3: Implement**

Append to `internal/arcadedb/embedding_space.go`:

```go
// replaceClause sets both columns on an upsert or an update, to NULL where v has nothing:
// the row may still hold a vector computed for older content, or in another space, and
// leaving it would keep a vector that no longer describes the row.
func (v storedVector) replaceClause(params map[string]any) string {
	params["embedding"], params["embed_space"] = v.vector, nullableString(v.space)
	return ", embedding = :embedding, embed_space = :embed_space"
}
```

`internal/arcadedb/memory_conversation.go`:
- `conversationSchemaStatements` returns `append([]string{…existing…}, spaceStampStatements(conversationTurnType)...)`.
- Replace `storedTurnVectorsStatement` and its lookup comment:

```go
// storedTurnVectorsStatement names the turns this projection must not embed again: those
// with a vector, and those a space refused. Turns with neither are the reconciler's to
// fill; turns answered in another space are the pass's (spec §5), so no turn is embedded
// twice.
const storedTurnVectorsStatement = "SELECT turn_seq, content_hash FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND conversation_id = :conversation_id" +
	" AND (embedding IS NOT NULL OR embed_space IS NOT NULL)"
```
- In `ApplyConversationProjection`:
  - rename `embedded` to `answered`;
  - replace the per-turn embedding block and delete the `REMOVE embedding` block (`if _, hasVector := …`):

```go
	answered, err := c.storedTurnVectorHashes(ctx, projection)
	if err != nil {
		return err
	}
	// The reconciler replays every turn once a minute: a turn whose content is already
	// answered keeps its answer, so the embedder sees only what changed, all in one request.
	vectors := c.embedChangedTurns(ctx, projection.Turns, answered)
	for index, turn := range projection.Turns {
		turnParams := map[string]any{
			"identity_id": turn.IdentityID, "conversation_id": turn.ConversationID,
			"turn_seq": turn.Seq, "role": turn.Role, "content": turn.Content,
			"content_hash": turn.ContentHash,
			"occurred_at":  turn.OccurredAt.UTC().Format(time.RFC3339Nano),
			"source_ref":   turn.SourceRef,
		}
		statement := upsertConversationTurnStatement
		if vector, changed := vectors[index]; changed {
			statement += vector.replaceClause(turnParams)
		}
		statement += upsertConversationTurnWhere
		if _, err := c.Command(ctx, statement, turnParams); err != nil {
			return fmt.Errorf("arcadedb: upsert conversation turn %d: %w", turn.Seq, err)
		}
```
  (the three edge statements and the reasoning link that follow stay as they are).
- Add below `storedTurnVectorHashes`:

```go
// embedChangedTurns answers every turn whose content has no stored answer, in one request.
// Each changed turn gets an entry -- its vector, its refusal, or nothing when no route
// answered -- and "nothing" clears a vector computed for older content. A route outage
// never clears a turn whose content still matches: that turn is not changed.
func (c *Client) embedChangedTurns(
	ctx context.Context,
	turns []ConversationTurnProjection,
	answered map[int]string,
) map[int]storedVector {
	changed := make(map[int]storedVector)
	texts := make([]string, 0, len(turns))
	indexes := make([]int, 0, len(turns))
	for index, turn := range turns {
		if answered[turn.Seq] == turn.ContentHash {
			continue
		}
		changed[index] = storedVector{}
		texts = append(texts, turn.Content)
		indexes = append(indexes, index)
	}
	if len(texts) == 0 || c.embedder == nil {
		return changed
	}
	vectors, err := c.embedStored(ctx, texts)
	if err != nil {
		return changed
	}
	for position, index := range indexes {
		changed[index] = vectors[position]
	}
	return changed
}
```

`internal/arcadedb/memory_reasoning.go`:
- `reasoningSchemaStatements` returns `append([]string{…existing…}, spaceStampStatements(reasoningTraceType)...)`.
- In `UpsertReasoningTrace`:
  - replace `vector := c.embedStatement(ctx, trace.ProviderSummary)` with `stored := c.embedOne(ctx, trace.ProviderSummary)`;
  - replace the statement assembly with the single line below, and delete the `if vector == nil { … clearReasoningEmbeddingStatement … }` block:

```go
	params := reasoningTraceParams(trace)
	statement := upsertReasoningTraceStatement + stored.replaceClause(params) + reasoningTraceWhere
```

`internal/arcadedb/memory_reasoning_statements.go`: delete `clearReasoningEmbeddingStatement`.

`internal/arcadedb/memory_vector.go`: delete `embedStatement` (lines 63-79). No caller remains.

Test updates that follow from the rule "removing a vector removes its stamp":
- `internal/arcadedb/memory_conversation_live_test.go:89-92`: the manual clear becomes

```go
	if _, err := client.Command(ctx, "UPDATE ConversationTurn SET embedding = NULL, embed_space = NULL"+
		" WHERE identity_id = :identity_id AND conversation_id = :conversation_id", scope); err != nil {
```
  Update its comment: a turn whose vector *and stamp* were lost is embedded again. A stamp without a vector is a refusal, which is kept.
- `internal/runner/runner_memory_projection_test.go:87-90`: the clear is now part of the turn upsert, so

```go
	if commandCount != 5 {
		t.Fatalf("ArcadeDB commands = %d, want conversation upsert, turn upsert (vector cleared in the same"+
			" statement), HAS_TURN, NEXT_TURN, INITIATED_BY", commandCount)
	}
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./internal/arcadedb/ ./internal/runner/ -run 'Conversation|Reasoning|EmbedOne|Projection' -count=1"`
Expected: `ok` for both packages.

Run: `wsl -e bash .superpowers/sdd/2026-09-23-embedding-memory-family/arcade-it.sh 'ConversationProjectionLive|ReasoningGraphLive'`
Expected: every listed test `PASS`, none `SKIP`.

- [ ] **Step 5: Commit**

```bash
git add internal/arcadedb/embedding_space.go internal/arcadedb/memory_conversation.go internal/arcadedb/memory_reasoning.go \
  internal/arcadedb/memory_reasoning_statements.go internal/arcadedb/memory_vector.go \
  internal/arcadedb/memory_conversation_test.go internal/arcadedb/memory_reasoning_test.go \
  internal/arcadedb/memory_conversation_live_test.go internal/arcadedb/memory_vector_test.go \
  internal/runner/runner_memory_projection_test.go
git commit -F - <<'EOF'
feat(memory): stamp turns and traces; embed a projection's turns in one request

ConversationTurn and ReasoningTrace gain embed_space. Their upserts set the
vector and its stamp in the statement that writes the row. The separate REMOVE
that followed a failed embed is gone, so a vector for older content is still
cleared, in the same write.

The reconciler's projection now embeds every changed turn in one request
instead of one per turn, and keeps a turn a space refused rather than asking
again every minute. Turns answered in another space are left to the pass.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 4: Dense memory runs only when the whole memory is in the reader's space

**Files:**
- Modify: `internal/arcadedb/embedding_space.go` (gate), `client.go` (gate field), `memory_vector.go:209-254` (reasons, `SearchFactsHybrid`), `memory_recall.go:225-275,321-333` (gate, one-sided reason), `memory_reasoning.go:315-430` (`ReasoningSearchResult`), `cmd/arcadedb-mcp/tool_memory_recall.go:244-282`
- Test: `internal/arcadedb/embedding_space_test.go` (new), `internal/arcadedb/memory_fakeserver_test.go` (helper), `internal/arcadedb/memory_identifiers_test.go:49`, `internal/arcadedb/memory_recall_test.go`, `cmd/arcadedb-mcp/tool_memory_test.go` (helper), `cmd/arcadedb-mcp/tool_memory_recall_test.go`, `internal/arcadedb/memory_gate_live_integration_test.go` (new)

**Interfaces:**
- Consumes: `DenseEmbedder.Space`, the stamp columns (Tasks 2-3).
- Produces:
  - `type memorySpaceType struct{ name, live string }`
  - vars `factSpace`, `turnSpace`, `traceSpace`, `memorySpaceTypes`
  - `const otherSpace`
  - `func (t memorySpaceType) mismatchCount() string`
  - `type spaceGate struct`
  - `const spaceGateTTL`
  - `func (c *Client) memoryDenseOpen(ctx, space string) (bool, error)`
  - `func (c *Client) denseQueryVector(ctx, query string) ([]float64, string)`
  - reasons `reasonEmbeddingSpaceMismatch = "embedding_space_mismatch"`, `reasonSpaceCheckFailed = "embedding_space_check_failed"`, `reasonFactRankingFailed = "fact_ranking_failed"`, `reasonTurnRankingFailed = "turn_ranking_failed"`
  - `type ReasoningSearchResult struct{ Traces []ReasoningTrace; RetrievalPath, Reason string }`
  - `func (c *Client) SearchReasoningTraces(ctx, identityID, query string, limit int) (ReasoningSearchResult, error)`

- [ ] **Step 1: Write the failing tests**

Add to `internal/arcadedb/memory_fakeserver_test.go`:

```go
// openMemoryGate prepends the three answers a dense read asks for first -- one count per
// memory type (memoryDenseOpen), each "no vector in another space" -- to a test whose fake
// answers statements in order.
func openMemoryGate(bodies ...string) []string {
	zero := `{"result":[{"n":0}]}`
	return append([]string{zero, zero, zero}, bodies...)
}
```

Create `internal/arcadedb/embedding_space_test.go`:

```go
package arcadedb

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// gateClient answers each memory type's mismatch count from counts (by type name), or with
// status when it is set, and every other statement with an empty result.
func gateClient(t *testing.T, counts map[string]int, status int) (*Client, *[]recordedRequest) {
	t.Helper()
	return routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		if !strings.HasPrefix(statement, "SELECT count(*) AS n FROM ") {
			return testResponse{Body: `{"result":[]}`}
		}
		if status != 0 {
			return testResponse{Status: status, Body: `{"detail":"down"}`}
		}
		typeName := strings.Fields(strings.TrimPrefix(statement, "SELECT count(*) AS n FROM "))[0]
		return testResponse{Body: `{"result":[{"n":` + strconv.Itoa(counts[typeName]) + `}]}`}
	})
}

func gateQueries(requests *[]recordedRequest) []recordedRequest {
	var out []recordedRequest
	for _, request := range *requests {
		if statement, _ := request.Payload["command"].(string); strings.HasPrefix(statement, "SELECT count(*) AS n FROM ") {
			out = append(out, request)
		}
	}
	return out
}

func TestMemoryGateOpensOnlyWhenNoVectorIsInAnotherSpace(t *testing.T) {
	open, _ := gateClient(t, map[string]int{}, 0)
	if ok, err := open.memoryDenseOpen(context.Background(), "es1-a"); err != nil || !ok {
		t.Fatalf("empty mismatch counts: open=%v err=%v, want open", ok, err)
	}
	closed, requests := gateClient(t, map[string]int{conversationTurnType: 2}, 0)
	if ok, err := closed.memoryDenseOpen(context.Background(), "es1-a"); err != nil || ok {
		t.Fatalf("two turns in another space: open=%v err=%v, want closed", ok, err)
	}
	for _, request := range gateQueries(requests) {
		statement, _ := request.Payload["command"].(string)
		params, _ := request.Payload["params"].(map[string]any)
		if !strings.Contains(statement, "embedding IS NOT NULL") || !strings.Contains(statement, otherSpace) ||
			params["space"] != "es1-a" {
			t.Fatalf("gate count does not ask for vectors outside the space:\n%s params=%v", statement, params)
		}
		if strings.Contains(statement, conversationTurnType) && !strings.Contains(statement, "deleted_at IS NULL") {
			t.Fatalf("soft-deleted turns close the gate (Review Focus 5):\n%s", statement)
		}
	}
}

func TestMemoryGateReusesItsAnswerForThirtySeconds(t *testing.T) {
	client, requests := gateClient(t, map[string]int{}, 0)
	ctx := context.Background()
	for range 2 {
		if _, err := client.memoryDenseOpen(ctx, "es1-a"); err != nil {
			t.Fatalf("memoryDenseOpen: %v", err)
		}
	}
	if n := len(gateQueries(requests)); n != len(memorySpaceTypes) {
		t.Fatalf("two reads within the TTL counted %d times, want one round of %d", n, len(memorySpaceTypes))
	}
	client.memoryGate.checked = time.Now().Add(-spaceGateTTL - time.Second)
	if _, err := client.memoryDenseOpen(ctx, "es1-a"); err != nil {
		t.Fatalf("memoryDenseOpen: %v", err)
	}
	if _, err := client.memoryDenseOpen(ctx, "es1-b"); err != nil {
		t.Fatalf("memoryDenseOpen: %v", err)
	}
	if n := len(gateQueries(requests)); n != 3*len(memorySpaceTypes) {
		t.Fatalf("an expired answer and a new space counted %d times, want three rounds", n)
	}
}

func TestSearchFactsHybridServesLexicallyOutsideTheSpace(t *testing.T) {
	client, _ := gateClient(t, map[string]int{factEdgeType: 1}, 0)
	embedder := &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}
	client.WithEmbedder(embedder)
	result, err := client.SearchFactsHybrid(context.Background(), "blue notebook", 5, time.Time{})
	if err != nil {
		t.Fatalf("SearchFactsHybrid: %v", err)
	}
	if result.RetrievalPath != retrievalPathLexical || result.Reason != reasonEmbeddingSpaceMismatch {
		t.Fatalf("result = %+v, want lexical with %q", result, reasonEmbeddingSpaceMismatch)
	}
	if len(embedder.calls) != 0 {
		t.Fatal("the query was embedded although the gate was closed")
	}
}

func TestMemoryGateFailureIsNamed(t *testing.T) {
	client, _ := gateClient(t, nil, http.StatusInternalServerError)
	client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})
	_, reason := client.denseQueryVector(context.Background(), "blue notebook")
	if reason != reasonSpaceCheckFailed {
		t.Fatalf("reason = %q, want %q", reason, reasonSpaceCheckFailed)
	}
}

func TestMemoryRecallServesLexicallyOutsideTheSpace(t *testing.T) {
	client, _ := gateClient(t, map[string]int{reasoningTraceType: 1}, 0)
	client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})
	result, err := client.RecallMemory(context.Background(), RecallRequest{
		IdentityID: "identity-a", Mode: RecallModeSemantic, Query: "blue notebook",
	})
	if err != nil {
		t.Fatalf("RecallMemory: %v", err)
	}
	if result.Retrieval.Path != retrievalPathLexical {
		t.Fatalf("path = %q, want lexical", result.Retrieval.Path)
	}
}

func TestSearchReasoningTracesNamesItsPath(t *testing.T) {
	client, _ := gateClient(t, map[string]int{factEdgeType: 1}, 0)
	client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})
	result, err := client.SearchReasoningTraces(context.Background(), "identity-a", "deployment", 5)
	if err != nil {
		t.Fatalf("SearchReasoningTraces: %v", err)
	}
	if result.RetrievalPath != retrievalPathLexical || result.Reason != reasonEmbeddingSpaceMismatch {
		t.Fatalf("result = %+v, want lexical with %q", result, reasonEmbeddingSpaceMismatch)
	}
}

```

Append to `internal/arcadedb/memory_recall_test.go`. The fact side answers, so the evidence is not empty. An empty result would overwrite the reason with `no_qualified_candidates` (`hydrateRecallRanking`) and hide what the test is about.

```go
// Fix on touch (spec): an answer from half the memory used to pass for a whole one.
func TestMemoryRecallNamesTheSideThatFailed(t *testing.T) {
	client, _ := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		switch {
		case strings.HasPrefix(statement, "SELECT count(*) AS n FROM "):
			return testResponse{Body: `{"result":[{"n":0}]}`}
		case strings.Contains(statement, "vector.fuse") && strings.Contains(statement, conversationTurnType):
			return testResponse{Status: http.StatusInternalServerError, Body: `{"detail":"turn index down"}`}
		case strings.Contains(statement, "vector.fuse"):
			return testResponse{Body: `{"result":[{"rid":"#10:1","score":0.03}]}`}
		case strings.Contains(statement, "FROM FACT") && strings.Contains(statement, "@rid IN"):
			return testResponse{Body: recallFactRow}
		}
		return testResponse{Body: `{"result":[]}`}
	})
	client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})
	result, err := client.RecallMemory(context.Background(), RecallRequest{
		IdentityID: "identity-a", Mode: RecallModeSemantic, Query: "where is the blue notebook", Limit: 5,
	})
	if err != nil {
		t.Fatalf("RecallMemory: %v", err)
	}
	if len(result.Evidence) != 1 || result.Retrieval.Path != retrievalPathHybrid ||
		result.Reason != reasonTurnRankingFailed {
		t.Fatalf("result = %+v (reason %q), want the fact answered with %q named",
			result.Evidence, result.Reason, reasonTurnRankingFailed)
	}
}
```

The degraded side fills the reason only when nothing else has claimed it: a lexical read already names why it is lexical.

In `cmd/arcadedb-mcp/tool_memory_test.go` add:

```go
// openMemoryGate prepends the three "no vector in another space" answers a dense read asks
// for first (arcadedb memoryDenseOpen).
func openMemoryGate(responses ...string) []string {
	zero := `{"result":[{"n":0}]}`
	return append([]string{zero, zero, zero}, responses...)
}
```
and in `cmd/arcadedb-mcp/tool_memory_recall_test.go` append:

```go
func TestMemoryRecallReasoningCarriesTheRetrievalReason(t *testing.T) {
	client, _ := newRecordingDB(t, `{"result":[{"n":1}]}`)
	client.WithEmbedder(recallStubEmbedder{})
	_, output, err := memoryRecallHandler(singleTenant(t, client))(
		context.Background(), reqWithIdentity(testIdentity), MemoryRecallInput{
			Mode: "reasoning", Query: "deployment", Limit: 5,
		},
	)
	if err != nil {
		t.Fatalf("memory_recall: %v", err)
	}
	if output.Retrieval.Reason != "embedding_space_mismatch" || output.Retrieval.Path != "lexical" {
		t.Fatalf("retrieval = %+v, want lexical with embedding_space_mismatch", output.Retrieval)
	}
}
```

Create `internal/arcadedb/memory_gate_live_integration_test.go`:

```go
//go:build arcadedb_integration

// The gate against a live ArcadeDB: a memory written on one route is served densely to that
// route and lexically to any other, with the reason named.
//
// Run: arcade-it.sh MemoryGateLive
package arcadedb

import (
	"context"
	"testing"
	"time"
)

func TestMemoryGateLiveFollowsTheStamps(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	now := time.Now().UTC()
	routeA := constantEmbedder{value: 1, space: "es1-route-a"}
	fact := mergeFact("GateSubject", "keeps", "GateObject", "GateSubject keeps the gate object.")
	if _, err := client.WithEmbedder(routeA).UpsertFact(ctx, fact, now); err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	onA, err := client.WithEmbedder(routeA).SearchFactsHybrid(ctx, "gate object", 5, now)
	if err != nil {
		t.Fatalf("SearchFactsHybrid on route A: %v", err)
	}
	if onA.RetrievalPath != retrievalPathHybrid {
		t.Fatalf("route A over its own memory = %+v, want hybrid", onA)
	}
	onB, err := client.WithEmbedder(constantEmbedder{value: 2, space: "es1-route-b"}).
		SearchFactsHybrid(ctx, "gate object", 5, now)
	if err != nil {
		t.Fatalf("SearchFactsHybrid on route B: %v", err)
	}
	if onB.RetrievalPath != retrievalPathLexical || onB.Reason != reasonEmbeddingSpaceMismatch {
		t.Fatalf("route B over route A's memory = %+v, want lexical with the mismatch named", onB)
	}
	if len(onB.Facts) == 0 {
		t.Fatal("the lexical fallback lost the fact")
	}
}
```

- [ ] **Step 2: Run the tests and watch them fail**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./internal/arcadedb/ ./cmd/arcadedb-mcp/ -run 'MemoryGate|ServesLexically|NamesItsPath|NamesTheSide|CarriesTheRetrievalReason' -count=1"`
Expected: FAIL at build time: `undefined: otherSpace`, `client.memoryGate undefined`, `result.RetrievalPath undefined (type []ReasoningTrace)`.

- [ ] **Step 3: Implement**

Append to `internal/arcadedb/embedding_space.go` (add imports `"context"`, `"fmt"`, `"sync"`, `"time"`):

```go
// memorySpaceType is one memory type whose vectors must share a space. live narrows the
// rows retrieval can reach: rows outside it neither close the gate nor cost the pass a
// request.
type memorySpaceType struct {
	name string
	live string
}

var (
	factSpace  = memorySpaceType{name: factEdgeType}
	turnSpace  = memorySpaceType{name: conversationTurnType, live: " AND deleted_at IS NULL"}
	traceSpace = memorySpaceType{name: reasoningTraceType}

	// memorySpaceTypes is the memory family (spec §3); the documents family is gated apart,
	// so a document that will not re-index never turns dense memory off.
	memorySpaceTypes = []memorySpaceType{factSpace, turnSpace, traceSpace}
)

// otherSpace matches a row whose stamp is not :space, a missing stamp included. ArcadeDB
// happened to answer `NULL <> 'x'` as true (lab VM, 26.9.1, 2026-09-23), but its docs do not
// define it, and the explicit form does not depend on it.
const otherSpace = "(embed_space IS NULL OR embed_space <> :space)"

// mismatchCount counts t's vectors in another space. Rows without a vector are not counted:
// they cannot be ranked, so they cannot be ranked wrongly.
func (t memorySpaceType) mismatchCount() string {
	return "SELECT count(*) AS n FROM " + t.name + " WHERE embedding IS NOT NULL AND " + otherSpace + t.live
}

// spaceGateTTL bounds how stale one tenant's gate answer can be: a pass that finishes
// opens the gate, and a stale writer closes it, within this.
const spaceGateTTL = 30 * time.Second

// spaceGate caches one tenant's answer, keyed by the space it was asked for.
type spaceGate struct {
	mu      sync.Mutex
	space   string
	open    bool
	checked time.Time
}

// memoryDenseOpen reports whether every memory vector this tenant holds is in space. Dense
// retrieval ranks a query against the whole corpus, so one vector from another model makes
// every distance suspect: until the pass has moved all of them, memory is served lexically
// (spec §3, operator decision 2).
func (c *Client) memoryDenseOpen(ctx context.Context, space string) (bool, error) {
	gate := &c.memoryGate
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.space == space && time.Since(gate.checked) < spaceGateTTL {
		return gate.open, nil
	}
	open := true
	for _, t := range memorySpaceTypes {
		rows, err := c.Query(ctx, t.mismatchCount(), map[string]any{"space": space})
		if err != nil {
			return false, fmt.Errorf("arcadedb: count %s vectors outside the space: %w", t.name, err)
		}
		if len(rows) > 0 && rowInt(rows[0], "n") > 0 {
			open = false
			break
		}
	}
	gate.space, gate.open, gate.checked = space, open, time.Now()
	return open, nil
}

// denseQueryVector is the dense leg's entry for every memory read: the query's vector, or
// nil and the reason the read must be lexical. The gate is asked before the query is
// embedded, so a closed gate costs no embedding request.
func (c *Client) denseQueryVector(ctx context.Context, query string) ([]float64, string) {
	if c == nil || c.embedder == nil {
		return nil, reasonEmbedderNotConfigured
	}
	space, err := c.embedder.Space(ctx)
	if err != nil {
		return nil, reasonEmbeddingFailed
	}
	open, err := c.memoryDenseOpen(ctx, space.ID)
	if err != nil {
		return nil, reasonSpaceCheckFailed
	}
	if !open {
		return nil, reasonEmbeddingSpaceMismatch
	}
	vectors, err := c.embedder.Embed(ctx, withTask(taskQueryPrefix, []string{query}))
	if err != nil {
		return nil, reasonEmbeddingFailed
	}
	if len(vectors) != 1 || len(vectors[0]) != vectorDimensions {
		return nil, reasonEmbeddingInvalid
	}
	return vectors[0], ""
}
```

`internal/arcadedb/client.go`, beside `embedder`:

```go
	// memoryGate is this tenant's answer to "is every memory vector in my space"
	// (embedding_space.go), cached for spaceGateTTL.
	memoryGate spaceGate
```

`internal/arcadedb/memory_vector.go`:
- Add to the reason constants:

```go
	reasonEmbeddingSpaceMismatch = "embedding_space_mismatch"
	reasonSpaceCheckFailed       = "embedding_space_check_failed"
```
- In `SearchFactsHybrid` replace the lines from `if c.embedder == nil {` through the width check with:

```go
	vector, reason := c.denseQueryVector(ctx, query)
	if vector == nil {
		return c.searchFactsFallback(ctx, query, limit, asOf, reason)
	}
```
  and bind `"vector": vector` in the fusion params.

`internal/arcadedb/memory_recall.go`:
- Delete `recallQueryVector`. In `recallSemantic`, `vector, embeddingReason := c.denseQueryVector(ctx, query)`.
- `rankRecallKinds` returns the degraded side:

```go
const (
	reasonFactRankingFailed = "fact_ranking_failed"
	reasonTurnRankingFailed = "turn_ranking_failed"
)
```
```go
// rankRecallKinds runs the two per-kind rankings. Both are attempted even when the first
// fails: a memory with no conversations yet, or a fact index still building, must still
// answer from the half that works rather than reporting an empty memory. The error is
// returned only when NEITHER side produced a ranking; one failing side is named in the
// returned reason, so an answer from half the memory never passes for a whole one.
func (c *Client) rankRecallKinds(
	ctx context.Context,
	factStatement, turnStatement string,
	params map[string]any,
	excluded []string,
) ([]recallRankedRID, []recallRankedRID, string, error) {
	factRows, factErr := c.Query(ctx, applyRecallExclusions(factStatement, params, excluded), params)
	turnRows, turnErr := c.Query(ctx, applyRecallExclusions(turnStatement, params, excluded), params)
	switch {
	case factErr != nil && turnErr != nil:
		return nil, nil, "", fmt.Errorf("rank facts: %v; rank turns: %w", factErr, turnErr)
	case factErr != nil:
		return nil, decodeRecallRanking(turnRows), reasonFactRankingFailed, nil
	case turnErr != nil:
		return decodeRecallRanking(factRows), nil, reasonTurnRankingFailed, nil
	}
	return decodeRecallRanking(factRows), decodeRecallRanking(turnRows), "", nil
}
```
  In `recallSemantic`: `facts, turns, degraded, err := c.rankRecallKinds(...)` (both calls), and after the error check `if reason == "" { reason = degraded }`.

`internal/arcadedb/memory_reasoning.go`:
- Add:

```go
// ReasoningSearchResult names the path that answered, as FactSearchResult does, so a
// degraded search is told apart from a memory with nothing to say.
type ReasoningSearchResult struct {
	Traces        []ReasoningTrace
	RetrievalPath string
	Reason        string
}
```
- `SearchReasoningTraces` returns `(ReasoningSearchResult, error)`:
  - errors return `ReasoningSearchResult{}`;
  - replace the embedder block with `vector, reason := c.denseQueryVector(ctx, query); if vector == nil { return c.searchReasoningLexical(ctx, params, reason) }` and `params["vector"] = vector`;
  - a failed fusion: `return c.searchReasoningLexical(ctx, params, reasonFusionFailed)`;
  - no rids: `return ReasoningSearchResult{Traces: []ReasoningTrace{}, RetrievalPath: retrievalPathHybrid}, nil`;
  - the end: `traces, err = c.hydrateReasoningBodies(ctx, identityID, traces); if err != nil { return ReasoningSearchResult{}, err }; return ReasoningSearchResult{Traces: traces, RetrievalPath: retrievalPathHybrid}, nil`.
- `searchReasoningLexical(ctx, params, reason string) (ReasoningSearchResult, error)` wraps its traces as `ReasoningSearchResult{Traces: traces, RetrievalPath: retrievalPathLexical, Reason: reason}`.

`cmd/arcadedb-mcp/tool_memory_recall.go:244-282` (reasoning mode):

```go
	var traces []arcadedb.ReasoningTrace
	path, reason := "graph", ""
	if traceID != "" {
		trace, found, err := client.GetReasoningTrace(ctx, identity, traceID)
		if err != nil {
			return MemoryRecallOutput{}, err
		}
		if found {
			traces = []arcadedb.ReasoningTrace{trace}
		}
	} else {
		result, err := client.SearchReasoningTraces(ctx, identity, query, in.Limit)
		if err != nil {
			return MemoryRecallOutput{}, err
		}
		traces, path, reason = result.Traces, result.RetrievalPath, result.Reason
	}
	output := MemoryRecallOutput{
		Evidence: make([]MemoryRecallEvidence, 0, len(traces)), Facts: make([]MemorySearchHit, 0),
		Abstained: len(traces) == 0,
		Retrieval: MemoryRecallRetrievalMetadata{
			EffectivePath: "reasoning", Path: path, ReasoningCount: len(traces),
			Abstained: len(traces) == 0, Reason: reason,
		},
	}
	if output.Abstained {
		if reason == "" {
			reason = "no_reasoning_evidence"
		}
		output.Reason, output.Retrieval.Reason = reason, reason
	}
```
In the telemetry call below, use `Reason: output.Reason` and `Path: path`.

Update the tests that answer statements in sequence:
- `internal/arcadedb/memory_identifiers_test.go:49`: `recordingClient(t, openMemoryGate(ranks, rows)...)`.
- In `cmd/arcadedb-mcp/tool_memory_recall_test.go`, every `newRecordingDB(t, …)` whose client gets `WithEmbedder(recallStubEmbedder{})` passes `openMemoryGate(…)...`.
- Callers of `SearchReasoningTraces` in `internal/arcadedb/*_test.go` and `cmd/arcadedb-mcp/*_test.go`: read `.Traces` from the result.

Run the packages. Every remaining failure must be a sequence shift caused by the three gate counts; fix it by wrapping the bodies in `openMemoryGate`. Any other failure is a defect: debug it rather than adapting the test.

- [ ] **Step 4: Run the tests and watch them pass**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./internal/arcadedb/ ./cmd/arcadedb-mcp/ -count=1"`
Expected: `ok` for both.

Run: `wsl -e bash .superpowers/sdd/2026-09-23-embedding-memory-family/arcade-it.sh 'MemoryGateLive|MemoryRecallLive|MemoryVector'`
Expected: `--- PASS: TestMemoryGateLiveFollowsTheStamps` and no `SKIP` for a missing `ARCADEDB_URL`.

- [ ] **Step 5: Commit**

```bash
git add internal/arcadedb/embedding_space.go internal/arcadedb/client.go internal/arcadedb/memory_vector.go \
  internal/arcadedb/memory_recall.go internal/arcadedb/memory_reasoning.go \
  internal/arcadedb/embedding_space_test.go internal/arcadedb/memory_fakeserver_test.go \
  internal/arcadedb/memory_identifiers_test.go internal/arcadedb/memory_gate_live_integration_test.go \
  cmd/arcadedb-mcp/tool_memory_recall.go cmd/arcadedb-mcp/tool_memory_test.go cmd/arcadedb-mcp/tool_memory_recall_test.go
git add $(git diff --name-only -- 'internal/arcadedb/*_test.go' 'cmd/arcadedb-mcp/*_test.go')
git commit -F - <<'EOF'
feat(memory): serve memory densely only when every vector is in the reader's space

Dense retrieval ranks a query against the whole corpus, so one vector from
another model makes every distance suspect. Each memory read now asks first,
with one count per type cached per tenant for 30 s, whether any vector lies
outside its space. If one does, the read is lexical with the reason
embedding_space_mismatch, and the query is never embedded. Soft-deleted turns
do not count.

SearchReasoningTraces now names its path and reason, as fact search does, and
memory_recall passes them on. Recall reports which side failed when only one
ranking answered.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

Before the second `git add`, check its list: it must contain only test files this task changed.

---

### Task 5: Remove the dead hybrid turn search (fix on touch)

**Files:**
- Modify: `internal/arcadedb/memory_conversation.go` (delete `ConversationSearchResult`, `searchConversationTurnsStatement`, `fuseConversationTurnRIDsStatement`, `hydrateConversationTurnsStatement`, `SearchConversationTurnsHybrid`, `searchConversationTurnsLexical`, `reasonIfEmpty`)
- Test: `internal/arcadedb/memory_conversation_test.go:80-105` (delete `TestConversationProjectionSearchFailsClosedAcrossIdentity`), `internal/arcadedb/memory_vector_test.go` (`TestEveryHybridStatementReranksItsFusion` loses the turn-fusion entry), `internal/arcadedb/memory_conversation_live_test.go:133-146`, `internal/runner/runner_memory_projection_test.go:91-97`, `cmd/arcadedb-mcp/memory_live_integration_test.go:330-341`

**Interfaces:**
- Consumes: `RecallMemory` (open and semantic modes).
- Produces: nothing new. `ConversationTurnHit` and `conversationTurnHitFromRow` stay, because recall uses them.

- [ ] **Step 1: Move each test off the dead function**

`internal/runner/runner_memory_projection_test.go:91-97` read the turn back from a fake that echoes a canned row to every query, so it proved only the fake's canned row. It now asserts what the projector sent. The handler records the turn upsert's params:

```go
	var turnParams map[string]any
```
declared beside `commandCount`, with the handler's payload decode gaining `Params map[string]any \`json:"params"\``, and, before `commandCount++`:

```go
		if strings.Contains(payload.Command, "content_hash = :content_hash") {
			turnParams = payload.Params
		}
```
The read-back becomes:

```go
	if turnParams["content"] != content || turnParams["source_ref"] != "postgres://conversation/conversation-1/turn/1" {
		t.Fatalf("turn upsert lost content/provenance: %v", turnParams)
	}
```

`internal/arcadedb/memory_conversation_live_test.go:133-146`:

```go
	recall := func(identityID, query string) RecallResult {
		t.Helper()
		result, err := client.RecallMemory(context.Background(), RecallRequest{
			IdentityID: identityID, Mode: RecallModeSemantic, Query: query,
		})
		if err != nil {
			t.Fatalf("recall %s %q: %v", identityID, query, err)
		}
		return result
	}
	if deleted := recall("identity-a", "deleteviolet"); len(deleted.Evidence) != 0 {
		t.Fatalf("deleted identity still sees projection: %+v", deleted.Evidence)
	}
	if foreign := recall("identity-b", "foreignsilver"); len(foreign.Evidence) != 1 {
		t.Fatalf("foreign identity was altered by another delete: %+v", foreign.Evidence)
	}
```

`cmd/arcadedb-mcp/memory_live_integration_test.go:330-341`:

```go
	seeded, err := seedClient.RecallMemory(ctx, arcadedb.RecallRequest{
		IdentityID: identityID, Mode: arcadedb.RecallModeSemantic, Query: "aurora notebook Turin", Limit: 10,
	})
	if err != nil {
		t.Fatalf("verify projected recall fixtures: %v", err)
	}
	seededConversations := make(map[string]bool)
	for _, evidence := range seeded.Evidence {
		if evidence.Conversation != nil {
			seededConversations[evidence.Conversation.ConversationID] = true
		}
	}
	if !seededConversations["conversation-active"] || !seededConversations["conversation-historical"] ||
		seededConversations["conversation-foreign"] {
		t.Fatalf("projected recall fixtures = %+v, want active and historical owner candidates only", seeded.Evidence)
	}
```

Delete `TestConversationProjectionSearchFailsClosedAcrossIdentity`. Recall's identity scoping is covered by `TestReasoningRecallIdentity` and the recall tests, and the live test above covers turns. Delete the `fuseConversationTurnRIDsStatement` entry in `TestEveryHybridStatementReranksItsFusion`.

- [ ] **Step 2: Delete the production code and run**

Delete the seven symbols listed under Files from `internal/arcadedb/memory_conversation.go`, and drop imports that become unused (`sort` if nothing else uses it).

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./internal/arcadedb/ ./internal/runner/ ./cmd/arcadedb-mcp/ -count=1 && go vet -tags arcadedb_integration ./internal/arcadedb/ ./cmd/arcadedb-mcp/"`
Expected: `ok` for the three packages and a silent vet.

Run: `wsl -e bash .superpowers/sdd/2026-09-23-embedding-memory-family/arcade-it.sh 'ConversationProjectionLive'`
Expected: every test `PASS`.

- [ ] **Step 3: Commit**

```bash
git add internal/arcadedb/memory_conversation.go internal/arcadedb/memory_conversation_test.go \
  internal/arcadedb/memory_vector_test.go internal/arcadedb/memory_conversation_live_test.go \
  internal/runner/runner_memory_projection_test.go cmd/arcadedb-mcp/memory_live_integration_test.go
git commit -F - <<'EOF'
refactor(memory): remove the hybrid turn search nothing called

SearchConversationTurnsHybrid had no production caller since recall merged
facts and turns (spec, fix on touch). Its tests read turns back through it;
they now read through RecallMemory, the path production uses.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 6: The pass moves every memory row into the daemon's space

**Files:**
- Create: `internal/arcadedb/memory_embed_pass.go`
- Modify: `internal/arcadedb/embedding_space.go` (the `source` and `fillsUnanswered` fields), `memory_vector.go:357-500` (`EmbedMissingFacts`, `ReEmbedAllFacts`, `clearFactEmbeddingsStatement`, `embedFacts`, `writeVectors` leave), `memory_backfill.go:26-39,59-94,132-167,209-232`, `internal/cron/handlers/memory_embed_backfill.go:14-41`, `cmd/aura/serve_provisioning.go`, `cmd/aura/serve.go:339`
- Test: `internal/arcadedb/memory_embed_pass_test.go` (new), `internal/arcadedb/memory_backfill_test.go` (fake and tests), `internal/arcadedb/memory_vector_test.go:270-390` (fill tests move to the pass file), `internal/arcadedb/memory_pass_live_integration_test.go` (new), `cmd/aura/serve_provisioning_test.go` (kick)

**Interfaces:**
- Consumes: `memorySpaceType`, `otherSpace`, `embedStored`, `storedVector`, `memoryDenseOpen` (Task 4).
- Produces:
  - `memorySpaceType.source string`, `memorySpaceType.fillsUnanswered bool`
  - `func (t memorySpaceType) passSelection() string`
  - `type passTally struct{ embedded, refused int }`
  - `func (c *Client) reembedMemory(ctx) (passTally, error)`
  - `func (c *Client) reembedType(ctx, t memorySpaceType, batch, rounds int) (passTally, error)`
  - `func (c *Client) storeVectors(ctx, typeName string, rids []string, vectors []storedVector) (passTally, error)`
  - `EmbedMissingFacts` and `ReEmbedAllFacts` keep their signatures
  - `func rotated(ids []string, turn uint64) []string`
  - `TenantBackfill.rotation atomic.Uint64`
  - `func kickMemoryEmbedBackfill(ctx, tasks memorySweepTasks) error`

- [ ] **Step 1: Write the failing tests**

Create `internal/arcadedb/memory_embed_pass_test.go`. `passServer` is an in-memory ArcadeDB for the three memory types that honours the pass's selection (type, stamp, cursor, batch) and its sqlscript/UPDATE writes:

```go
package arcadedb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/embeddings"
)

type passRow struct {
	position int
	text     string
	vector   bool
	space    string
	deleted  bool
}

// passServer holds rows per type and answers exactly the statements the pass sends.
type passServer struct {
	mu      sync.Mutex
	rows    map[string][]*passRow // by type
	selects int
}

var passUpdate = regexp.MustCompile(`UPDATE (\w+) SET embedding = :(v\d+), embed_space = :(s\d+) WHERE @rid = :(r\d+)`)

func (s *passServer) rid(typeName string, row *passRow) string {
	return fmt.Sprintf("#%d:%d", map[string]int{factEdgeType: 10, conversationTurnType: 20, reasoningTraceType: 30}[typeName], row.position)
}

func (s *passServer) serve(w http.ResponseWriter, r *http.Request) {
	if handleTransactionEndpoints(w, r) {
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var payload struct {
		Command string         `json:"command"`
		Params  map[string]any `json:"params"`
	}
	_ = json.Unmarshal(raw, &payload)
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if strings.HasPrefix(payload.Command, "SELECT @rid AS rid, ") {
		s.selects++
		_ = json.NewEncoder(w).Encode(map[string]any{"result": s.selectRows(payload.Command, payload.Params)})
		return
	}
	for _, match := range passUpdate.FindAllStringSubmatch(payload.Command, -1) {
		typeName, rid := match[1], payload.Params[match[4]]
		for _, row := range s.rows[typeName] {
			if s.rid(typeName, row) == rid {
				row.vector = payload.Params[match[2]] != nil
				row.space, _ = payload.Params[match[3]].(string)
			}
		}
	}
	_, _ = io.WriteString(w, `{"result":[{"count":1}]}`)
}

func (s *passServer) selectRows(statement string, params map[string]any) []map[string]any {
	typeName := strings.Fields(statement[strings.Index(statement, " FROM ")+6:])[0]
	space, _ := params["space"].(string)
	cursor, _ := params["cursor"].(string)
	batch := int(params["batch"].(float64))
	rows := append([]*passRow(nil), s.rows[typeName]...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].position < rows[j].position })
	out := []map[string]any{}
	for _, row := range rows {
		if len(out) == batch {
			break
		}
		if ridAfter(s.rid(typeName, row), cursor) && row.space != space &&
			(!strings.Contains(statement, "deleted_at IS NULL") || !row.deleted) &&
			(!strings.Contains(statement, "(embedding IS NOT NULL OR embed_space IS NOT NULL)") || row.vector || row.space != "") {
			out = append(out, map[string]any{"rid": s.rid(typeName, row), "text": row.text})
		}
	}
	return out
}

func ridAfter(rid, cursor string) bool {
	if cursor == "#-1:-1" {
		return true
	}
	position := func(value string) int { n, _ := strconv.Atoi(value[strings.Index(value, ":")+1:]); return n }
	return position(rid) > position(cursor)
}

func newPassClient(t *testing.T, rows map[string][]*passRow, embedder DenseEmbedder) (*Client, *passServer) {
	t.Helper()
	server := &passServer{rows: rows}
	srv := httptest.NewServer(http.HandlerFunc(server.serve))
	t.Cleanup(srv.Close)
	return mustClient(t, srv.URL).WithEmbedder(embedder), server
}

func stampedRows(n int, space string, vector bool) []*passRow {
	rows := make([]*passRow, n)
	for i := range rows {
		rows[i] = &passRow{position: i, text: fmt.Sprintf("text %d", i), vector: vector, space: space}
	}
	return rows
}

func TestPassMovesEveryTypeToTheNewSpace(t *testing.T) {
	rows := map[string][]*passRow{
		factEdgeType:         append(stampedRows(40, "es1-a", true), &passRow{position: 40, text: "never embedded"}),
		conversationTurnType: stampedRows(3, "", true),
		reasoningTraceType:   stampedRows(2, "es1-a", true),
	}
	client, _ := newPassClient(t, rows, &refusingEmbedder{space: "es1-b"})
	tally, err := client.reembedMemory(context.Background())
	if err != nil {
		t.Fatalf("reembedMemory: %v", err)
	}
	if tally.embedded != 46 || tally.refused != 0 {
		t.Fatalf("tally = %+v, want 46 embedded", tally)
	}
	for typeName, typed := range rows {
		for _, row := range typed {
			if !row.vector || row.space != "es1-b" {
				t.Fatalf("%s %+v was left outside es1-b", typeName, *row)
			}
		}
	}
}

// A turn with neither vector nor stamp is the reconciler's; a turn refused in another
// space, and a soft-deleted turn, are not the same case (Review Focus 5).
func TestPassLeavesTheReconcilersTurnsAlone(t *testing.T) {
	rows := map[string][]*passRow{
		conversationTurnType: {
			{position: 0, text: "never answered"},
			{position: 1, text: "refused elsewhere", space: "es1-a"},
			{position: 2, text: "deleted", vector: true, space: "es1-a", deleted: true},
		},
	}
	client, _ := newPassClient(t, rows, &refusingEmbedder{space: "es1-b"})
	if _, err := client.reembedType(context.Background(), turnSpace, backfillBatch, 0); err != nil {
		t.Fatalf("reembedType: %v", err)
	}
	turns := rows[conversationTurnType]
	if turns[0].space != "" || turns[1].space != "es1-b" || turns[2].space != "es1-a" {
		t.Fatalf("turns = %+v %+v %+v", *turns[0], *turns[1], *turns[2])
	}
}

// A cursor, not a re-selection: a row whose vector comes back unusable must not be selected
// again on every round.
func TestPassCursorPassesARowItCouldNotFix(t *testing.T) {
	rows := map[string][]*passRow{factEdgeType: stampedRows(70, "es1-a", true)}
	embedder := &widthEmbedder{short: "text 5", space: "es1-b"}
	client, server := newPassClient(t, rows, embedder)
	if _, err := client.reembedType(context.Background(), factSpace, backfillBatch, 0); err != nil {
		t.Fatalf("reembedType: %v", err)
	}
	if server.selects != 3 {
		t.Fatalf("selections = %d, want 3 for 70 rows in rounds of %d", server.selects, backfillBatch)
	}
	if rows[factEdgeType][5].space != "es1-a" {
		t.Fatal("a row with an unusable vector was stamped")
	}
}

func TestPassQuarantinesOnlyTheRefusedRecord(t *testing.T) {
	rows := map[string][]*passRow{factEdgeType: stampedRows(5, "es1-a", true)}
	embedder := &refusingEmbedder{refuse: []string{"text 3"}, status: http.StatusBadRequest, space: "es1-b"}
	client, _ := newPassClient(t, rows, embedder)
	tally, err := client.reembedType(context.Background(), factSpace, backfillBatch, 0)
	if err != nil {
		t.Fatalf("reembedType: %v", err)
	}
	if tally.embedded != 4 || tally.refused != 1 {
		t.Fatalf("tally = %+v, want 4 embedded, 1 refused", tally)
	}
	refused := rows[factEdgeType][3]
	if refused.vector || refused.space != "es1-b" {
		t.Fatalf("refused record = %+v, want no vector and the refusing space", *refused)
	}
}

// Review Focus 3: a route failure ends the run and moves nothing.
func TestPassEndsOnARouteFailureWithoutMarkingAnything(t *testing.T) {
	for _, failure := range []error{
		&embeddings.StatusError{Code: 401}, &embeddings.StatusError{Code: 429},
		&embeddings.StatusError{Code: 503}, errors.New("request: connection refused"),
	} {
		rows := map[string][]*passRow{factEdgeType: stampedRows(3, "es1-a", true)}
		client, _ := newPassClient(t, rows, &failingEmbedder{err: failure, space: "es1-b"})
		if _, err := client.reembedType(context.Background(), factSpace, backfillBatch, 0); err == nil {
			t.Fatalf("%v was absorbed", failure)
		}
		for _, row := range rows[factEdgeType] {
			if row.space != "es1-a" || !row.vector {
				t.Fatalf("%v moved %+v", failure, *row)
			}
		}
	}
}

// A second route change in mid-pass needs no protocol: B-stamped rows differ from C.
func TestPassFollowsASecondRouteChange(t *testing.T) {
	rows := map[string][]*passRow{factEdgeType: append(stampedRows(2, "es1-a", true), stampedRows(1, "es1-b", true)...)}
	rows[factEdgeType][2].position = 2
	client, _ := newPassClient(t, rows, &refusingEmbedder{space: "es1-c"})
	if _, err := client.reembedMemory(context.Background()); err != nil {
		t.Fatalf("reembedMemory: %v", err)
	}
	for _, row := range rows[factEdgeType] {
		if row.space != "es1-c" {
			t.Fatalf("row %+v left behind by A→B→C", *row)
		}
	}
}

type widthEmbedder struct {
	short string
	space string
}

func (e *widthEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	vectors := make([][]float64, len(texts))
	for i, text := range texts {
		vectors[i] = vectorOf(1)
		if strings.HasSuffix(text, e.short) {
			vectors[i] = []float64{1}
		}
	}
	return vectors, nil
}

func (e *widthEmbedder) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: e.space}, nil
}

type failingEmbedder struct {
	err   error
	space string
}

func (e *failingEmbedder) Embed(context.Context, []string) ([][]float64, error) { return nil, e.err }

func (e *failingEmbedder) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: e.space}, nil
}
```

In `internal/arcadedb/memory_backfill_test.go`, `tenantServer` holds only facts (`pending` is its fact count). In `serve`, replace the block from `if !strings.HasPrefix(payload.Command, "SELECT @rid AS rid")` to the end with:

```go
	if !strings.HasPrefix(payload.Command, "SELECT @rid AS rid") {
		_, _ = io.WriteString(w, `{"result":[{"count":1}]}`)
		return
	}
	if !strings.Contains(payload.Command, " FROM "+factEdgeType+" ") {
		_, _ = io.WriteString(w, `{"result":[]}`) // this fake holds no turns and no traces
		return
	}
	s.selects[database]++
	take := min(s.pending[database], backfillBatch)
	s.pending[database] -= take
	rows := make([]string, 0, take)
	for i := range take {
		rows = append(rows, fmt.Sprintf(`{"rid":"#3:%d","text":"fact %d"}`, i, i))
	}
	_, _ = io.WriteString(w, `{"result":[`+strings.Join(rows, ",")+`]}`)
```
Update its comment: "which of its facts are outside the space". The existing `TestTenantBackfillBatchesUntilAShortRound` stays as it is: two full fact rounds and the short one that ends them.

Replace `TestTenantBackfillSelectsOnlyFactsWithoutAVector`, `TestTenantBackfillBoundsOneTenantsRounds` and `TestTenantBackfillStopsOnARoundThatEmbedsNothing`. The first two pinned the old selection and its round cap. The third pinned a stop-on-zero that the cursor (`TestPassCursorPassesARowItCouldNotFix`) replaces. Their replacements:

```go
// The selection is "not in the daemon's space", paged by RID: it selects the facts a route
// change left behind as well as the ones never embedded, and it moves past a row it could
// not fix instead of selecting it again.
func TestTenantBackfillSelectsRowsOutsideTheSpace(t *testing.T) {
	server := newTenantServer(t, map[string]bool{databaseA: true}, map[string]int{databaseA: 1})
	backfill := testBackfill(t, server, staticRoster{ids: []string{tenantA}}, &batchEmbedder{})

	if _, err := backfill.EmbedMissing(context.Background(), time.Time{}); err != nil {
		t.Fatalf("EmbedMissing: %v", err)
	}
	selected := ""
	for _, statement := range server.statements {
		if strings.Contains(statement, "SELECT @rid AS rid") && strings.Contains(statement, " FROM "+factEdgeType+" ") {
			selected = statement
		}
	}
	for _, want := range []string{otherSpace, "@rid > :cursor", "ORDER BY @rid"} {
		if !strings.Contains(selected, want) {
			t.Fatalf("selection = %q, want %q", selected, want)
		}
	}
	if strings.Contains(selected, "embedding IS NULL") {
		t.Fatalf("selection = %q still keys on a missing vector", selected)
	}
}

// A tenant is drained within the run's budget, not cut at a round count: a route change
// leaves the whole memory behind, and a cap of 20 rounds would take a large tenant many
// runs while its reads stay lexical.
func TestTenantBackfillDrainsATenantUntilNothingIsLeft(t *testing.T) {
	const backlog = (backfillRoundsPerTenant + 5) * backfillBatch
	server := newTenantServer(t, map[string]bool{databaseA: true}, map[string]int{databaseA: backlog})
	backfill := testBackfill(t, server, staticRoster{ids: []string{tenantA}}, &batchEmbedder{})

	embedded, err := backfill.EmbedMissing(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("EmbedMissing: %v", err)
	}
	if embedded != backlog {
		t.Fatalf("embedded = %d, want the whole backlog of %d", embedded, backlog)
	}
}
```

Add three tests:

```go
// Review Focus 4: the tenant a run starts from moves on, so a backlog that outlasts one
// run's budget cannot starve the tenants behind it.
func TestTenantBackfillRotatesTheTenantItStartsFrom(t *testing.T) {
	if got := rotated([]string{"a", "b", "c"}, 4); strings.Join(got, "") != "bca" {
		t.Fatalf("rotated = %v, want b c a", got)
	}
	if got := rotated(nil, 3); len(got) != 0 {
		t.Fatalf("rotated(nil) = %v", got)
	}
}

// Review Focus 4: the run budget ending is not a failure; the next run resumes.
func TestTenantBackfillStopsAtTheBudgetWithoutFailing(t *testing.T) {
	server := newTenantServer(t, map[string]bool{databaseA: true, databaseB: true},
		map[string]int{databaseA: 10, databaseB: 10})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := testBackfill(t, server, staticRoster{ids: []string{tenantA, tenantB}}, &batchEmbedder{}).
		EmbedMissing(ctx, time.Time{}); err != nil {
		t.Fatalf("a spent budget was reported as a failure: %v", err)
	}
}

// No space, no pass: a hosted route without its key, or a sidecar that cannot name its
// model, would fail every tenant the same way.
func TestTenantBackfillDoesNotRunWithoutASpace(t *testing.T) {
	server := newTenantServer(t, map[string]bool{databaseA: true}, map[string]int{databaseA: 3})
	embedder := &stubEmbedder{spaceErr: embeddings.ErrNoCredential}
	_, err := testBackfill(t, server, staticRoster{ids: []string{tenantA}}, embedder).EmbedMissing(context.Background(), time.Time{})
	if !errors.Is(err, embeddings.ErrNoCredential) {
		t.Fatalf("err = %v, want ErrNoCredential", err)
	}
	if server.sawDatabase(databaseA) {
		t.Fatal("the pass visited a tenant with no space to embed in")
	}
}
```
In the budget test, the pre-cancelled context makes the existence probe fail. `EmbedMissing` must treat `ctx.Err() != nil` as the budget, not as a failure. The space check comes first and uses the stub, which ignores `ctx`.

In `internal/arcadedb/memory_vector_test.go`, delete `TestEmbedMissingAndReembedFactsWriteOnlyValidVectors`, `TestEmbedFactsHandlesNoWorkAndFailures` and `TestEmbedFactsCapsMaintenanceBatch`. Their contracts (a width filter, failures and a batch cap) move to the pass tests above and to:

```go
func TestEmbedMissingFactsTakesOneBoundedRound(t *testing.T) {
	rows := map[string][]*passRow{factEdgeType: stampedRows(150, "", false)}
	client, server := newPassClient(t, rows, &refusingEmbedder{space: "es1-b"})
	embedded, err := client.EmbedMissingFacts(context.Background(), 1000)
	if err != nil {
		t.Fatalf("EmbedMissingFacts: %v", err)
	}
	if embedded != defaultMemoryLimits.MaintenanceBatch || server.selects != 1 {
		t.Fatalf("embedded %d in %d selections, want one round capped at %d",
			embedded, server.selects, defaultMemoryLimits.MaintenanceBatch)
	}
}
```
This test goes at the end of `memory_embed_pass_test.go`.

Create `internal/arcadedb/memory_pass_live_integration_test.go`:

```go
//go:build arcadedb_integration

// The pass against a live ArcadeDB: after a route change every memory row is outside the
// space, memory is lexical, and one pass brings it back dense.
//
// Run: arcade-it.sh MemoryPassLive
package arcadedb

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestMemoryPassLiveMovesTheMemoryAndReopensTheGate(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	now := time.Now().UTC()
	routeA := constantEmbedder{value: 1, space: "es1-route-a"}
	for _, subject := range []string{"PassOne", "PassTwo", "PassPoison"} {
		fact := mergeFact(subject, "keeps", "PassObject", subject+" keeps the pass object.")
		if _, err := client.WithEmbedder(routeA).UpsertFact(ctx, fact, now); err != nil {
			t.Fatalf("UpsertFact(%s): %v", subject, err)
		}
	}
	projection := liveConversationProjection("identity-a", "conversation-pass", 1, "passturnamber")
	if err := client.WithEmbedder(routeA).ApplyConversationProjection(ctx, projection); err != nil {
		t.Fatalf("ApplyConversationProjection: %v", err)
	}
	if err := client.WithEmbedder(routeA).UpsertReasoningTrace(ctx, validReasoningTrace()); err != nil {
		t.Fatalf("UpsertReasoningTrace: %v", err)
	}

	routeB := &refusingEmbedder{refuse: []string{"PassPoison keeps the pass object."}, status: http.StatusBadRequest, space: "es1-route-b"}
	onB := client.WithEmbedder(routeB)
	if open, err := onB.memoryDenseOpen(ctx, "es1-route-b"); err != nil || open {
		t.Fatalf("before the pass: open=%v err=%v, want closed", open, err)
	}
	tally, err := onB.reembedMemory(ctx)
	if err != nil {
		t.Fatalf("reembedMemory: %v", err)
	}
	if tally.embedded != 4 || tally.refused != 1 {
		t.Fatalf("tally = %+v, want 2 facts + 1 turn + 1 trace embedded and 1 fact refused", tally)
	}
	onB.memoryGate.checked = time.Time{}
	if open, err := onB.memoryDenseOpen(ctx, "es1-route-b"); err != nil || !open {
		t.Fatalf("after the pass: open=%v err=%v, want open (the refusal does not count)", open, err)
	}

	// Review Focus 1: a stale writer still on route A closes the gate again, and the next
	// run re-embeds what it wrote.
	stale := mergeFact("PassLate", "keeps", "PassObject", "PassLate keeps the pass object.")
	if _, err := client.WithEmbedder(routeA).UpsertFact(ctx, stale, now); err != nil {
		t.Fatalf("stale UpsertFact: %v", err)
	}
	onB = client.WithEmbedder(routeB)
	onB.memoryGate.checked = time.Time{}
	if open, _ := onB.memoryDenseOpen(ctx, "es1-route-b"); open {
		t.Fatal("a stale writer's vector did not close the gate")
	}
	if _, err := onB.reembedMemory(ctx); err != nil {
		t.Fatalf("second reembedMemory: %v", err)
	}
	onB.memoryGate.checked = time.Time{}
	if open, _ := onB.memoryDenseOpen(ctx, "es1-route-b"); !open {
		t.Fatal("the second run did not re-embed the stale writer's fact")
	}
}
```
`liveConversationProjection` lives in `memory_conversation_live_test.go`, `validReasoningTrace` in `memory_reasoning_test.go`, and `refusingEmbedder` in `memory_embed_stored_test.go`; all compile under the tag. The projected turn does not match the trace's (`conversation-a`, 7). That is harmless: ArcadeDB answers a `CREATE EDGE` whose TO side is empty by doing nothing and reporting success (measured 2026-09-03, see `TestConversationProjectionClosesReasoningInitiatorEdge`).

For the boot kick, append to `cmd/aura/serve_provisioning_test.go` (add `"context"` and `"github.com/chetto1983/aura/internal/cron"` to its imports if absent):

```go
type fakeSweepTasks struct {
	tasks  []cron.Task
	kicked []string
}

func (f *fakeSweepTasks) ListActiveTasks(context.Context) ([]cron.Task, error) { return f.tasks, nil }

func (f *fakeSweepTasks) RunTaskNow(_ context.Context, id string) error {
	f.kicked = append(f.kicked, id)
	return nil
}

// A route change restarts the daemon, and every memory read is lexical until the pass has
// run: the sweep must run on the first tick, not five minutes later (spec §5).
func TestKickMemoryEmbedBackfillRunsTheSweepNow(t *testing.T) {
	tasks := &fakeSweepTasks{tasks: []cron.Task{
		{ID: "other", Kind: cron.KindMemoryMentionLink},
		{ID: "sweep", Kind: cron.KindMemoryEmbedBackfill},
	}}
	if err := kickMemoryEmbedBackfill(context.Background(), tasks); err != nil {
		t.Fatalf("kickMemoryEmbedBackfill: %v", err)
	}
	if len(tasks.kicked) != 1 || tasks.kicked[0] != "sweep" {
		t.Fatalf("kicked = %v, want the memory embed sweep only", tasks.kicked)
	}
}
```

- [ ] **Step 2: Run the tests and watch them fail**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./internal/arcadedb/ -run 'TestPass|TestTenantBackfill|TestEmbedMissingFacts' -count=1; go test ./cmd/aura/ -run TestKickMemoryEmbedBackfill -count=1"`
Expected: FAIL at build time: `reembedMemory undefined`, `undefined: rotated`, `undefined: kickMemoryEmbedBackfill`.

- [ ] **Step 3: Implement the pass**

In `internal/arcadedb/embedding_space.go`, `memorySpaceType` gains the fields the pass needs:

```go
type memorySpaceType struct {
	name string
	live string
	// source is the text the type's vector embeds.
	source string
	// fillsUnanswered: rows with neither vector nor stamp are the pass's to embed. Turns are
	// not: the conversation reconciler fills them on its next replay (memory_conversation.go).
	fillsUnanswered bool
}

var (
	factSpace  = memorySpaceType{name: factEdgeType, source: "statement", fillsUnanswered: true}
	turnSpace  = memorySpaceType{name: conversationTurnType, source: "content", live: " AND deleted_at IS NULL"}
	traceSpace = memorySpaceType{name: reasoningTraceType, source: "provider_summary", fillsUnanswered: true}
	…
)
```

Create `internal/arcadedb/memory_embed_pass.go`:

```go
package arcadedb

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// The pass that keeps a tenant's memory in one embedding space (spec §5).
//
// It generalises the old fill of "facts without a vector". After a route change every row
// carries the old space, and "has no vector" selected none of them; "not stamped with the
// daemon's space" selects all of them, and shrinks as the work is done, which is what makes
// an interrupted run, or a second route change in mid-pass, need no protocol.

// passSelection is the rows of t the pass owns in :space, one cursor page at a time.
// ArcadeDB pages by RID natively: `@rid > :cursor` seeks, starting from #-1:-1
// (arcadedb-docs reference/sql/sql-pagination.adoc). A string parameter compares as a RID
// -- #44:9 is followed by #44:10, not #44:2048 (lab VM, 26.9.1, 2026-09-23). The cursor is
// what lets a run pass a row it could not fix instead of selecting it again every round.
func (t memorySpaceType) passSelection() string {
	answered := ""
	if !t.fillsUnanswered {
		answered = " AND (embedding IS NOT NULL OR embed_space IS NOT NULL)"
	}
	return "SELECT @rid AS rid, " + t.source + " AS text FROM " + t.name +
		" WHERE " + t.source + " IS NOT NULL AND " + otherSpace + answered + t.live +
		" AND @rid > :cursor ORDER BY @rid LIMIT :batch"
}

// passTally counts what a pass changed: vectors written, and records the model refused.
type passTally struct {
	embedded int
	refused  int
}

func (p *passTally) add(other passTally) {
	p.embedded += other.embedded
	p.refused += other.refused
}

// reembedMemory runs the pass over the whole memory family until nothing is left or ctx
// ends.
func (c *Client) reembedMemory(ctx context.Context) (passTally, error) {
	var tally passTally
	for _, t := range memorySpaceTypes {
		typed, err := c.reembedType(ctx, t, backfillBatch, 0)
		tally.add(typed)
		if err != nil {
			return tally, err
		}
	}
	return tally, nil
}

// reembedType pages t by RID and re-embeds each page in the space read for it. rounds
// bounds the pages; 0 means until nothing is left.
func (c *Client) reembedType(ctx context.Context, t memorySpaceType, batch, rounds int) (passTally, error) {
	var tally passTally
	cursor := "#-1:-1"
	for round := 0; rounds == 0 || round < rounds; round++ {
		space, err := c.embedder.Space(ctx)
		if err != nil {
			return tally, err
		}
		rows, err := c.Query(ctx, t.passSelection(), map[string]any{
			"space": space.ID, "cursor": cursor, "batch": batch,
		})
		if err != nil {
			return tally, fmt.Errorf("arcadedb: select %s to re-embed: %w", t.name, err)
		}
		rids, texts := make([]string, 0, len(rows)), make([]string, 0, len(rows))
		for _, row := range rows {
			rid := rowString(row, "rid")
			if rid == "" {
				continue
			}
			cursor = rid
			if text := rowString(row, "text"); strings.TrimSpace(text) != "" {
				rids, texts = append(rids, rid), append(texts, text)
			}
		}
		if len(texts) > 0 {
			vectors, err := c.embedStored(ctx, texts)
			if err != nil {
				return tally, err
			}
			written, err := c.storeVectors(ctx, t.name, rids, vectors)
			tally.add(written)
			if err != nil {
				return tally, err
			}
		}
		if len(rows) < batch {
			return tally, nil
		}
	}
	return tally, nil
}

// storeVectors writes a page in ONE round trip, and falls back to one statement per row
// only when that fails.
//
// The round trip is the entire cost. Measured on this host 2026-08-03: a vector UPDATE by
// @rid takes 55-78ms where `SELECT 1` takes 53-63ms, so a page of 32 written one at a time
// spent ~1.8s in handshakes to do ~0.3s of work. A sqlscript is one implicit transaction
// (arcadedb-docs reference/sql/sql-script.adoc), which is also why the per-row fallback
// exists: one poisoned row would roll back its 31 healthy companions on every run.
//
// A vector is written with its space. A refused text is set aside: its vector removed, its
// row stamped with the space that refused it, so neither the pass nor the gate counts it
// until the space changes. A row with neither is left as it was.
func (c *Client) storeVectors(ctx context.Context, typeName string, rids []string, vectors []storedVector) (passTally, error) {
	var tally passTally
	statements := make([]string, 0, len(rids))
	params := make(map[string]any, len(rids)*3)
	usable := make([]int, 0, len(rids))
	for i := range rids {
		if i >= len(vectors) || vectors[i].space == "" {
			continue
		}
		usable = append(usable, i)
		n := strconv.Itoa(len(usable) - 1)
		statements = append(statements,
			"UPDATE "+typeName+" SET embedding = :v"+n+", embed_space = :s"+n+" WHERE @rid = :r"+n)
		params["v"+n], params["s"+n], params["r"+n] = vectors[i].vector, vectors[i].space, rids[i]
	}
	count := func(i int) {
		if vectors[i].vector != nil {
			tally.embedded++
		} else {
			tally.refused++
		}
	}
	if len(statements) == 0 {
		return tally, nil
	}
	if _, err := c.Script(ctx, strings.Join(statements, ";\n"), params); err == nil {
		for _, i := range usable {
			count(i)
		}
		return tally, nil
	}
	var failures []string
	for _, i := range usable {
		if _, err := c.Command(ctx,
			"UPDATE "+typeName+" SET embedding = :vector, embed_space = :space WHERE @rid = :rid",
			map[string]any{"vector": vectors[i].vector, "space": vectors[i].space, "rid": rids[i]}); err != nil {
			failures = append(failures, rids[i])
			continue
		}
		count(i)
	}
	if len(failures) > 0 {
		return tally, fmt.Errorf("arcadedb: write %s embedding failed for %d of %d rows (first %s)",
			typeName, len(failures), len(usable), failures[0])
	}
	return tally, nil
}

// EmbedMissingFacts is one bounded round of the pass over facts: the memory_reembed tool's
// repair, without `all`. It returns how many vectors it wrote.
func (c *Client) EmbedMissingFacts(ctx context.Context, batch int) (int, error) {
	if c == nil || c.embedder == nil {
		return 0, fmt.Errorf("arcadedb: no embedder configured")
	}
	tally, err := c.reembedType(ctx, factSpace, boundedLimit(batch, 100, c.memoryLimits().MaintenanceBatch), 1)
	return tally.embedded, err
}

// ReEmbedAllFacts recomputes EVERY fact's vector in the current space: a same-space repair
// (spec, Out of scope). A route change needs no call: the pass re-embeds on its own.
//
// It clears every vector and stamp, then drains. Selecting "facts with a vector" directly
// is what shipped first, and it could not finish: that set does not shrink as the work is
// done, so every call returned the same first `batch` rows (measured 2026-09-03 through the
// MCP surface: two consecutive `all` calls on a 55-fact memory both reported 30).
func (c *Client) ReEmbedAllFacts(ctx context.Context, batch int) (int, error) {
	if c == nil || c.embedder == nil {
		return 0, fmt.Errorf("arcadedb: no embedder configured")
	}
	if _, err := c.Command(ctx, clearFactEmbeddingsStatement, nil); err != nil {
		return 0, fmt.Errorf("arcadedb: clear fact vectors: %w", err)
	}
	tally, err := c.reembedType(ctx, factSpace, boundedLimit(batch, 100, c.memoryLimits().MaintenanceBatch),
		backfillRoundsPerTenant)
	return tally.embedded, err
}

// clearFactEmbeddingsStatement drops every fact's vector and stamp, together: a stamp
// without a vector means "this space refused the text".
const clearFactEmbeddingsStatement = "UPDATE " + factEdgeType +
	" SET embedding = NULL, embed_space = NULL WHERE embedding IS NOT NULL OR embed_space IS NOT NULL"
```

In `internal/arcadedb/memory_vector.go`, delete `EmbedMissingFacts`, `ReEmbedAllFacts`, `clearFactEmbeddingsStatement`, `embedFacts` and `writeVectors` (lines 357-500).

In `internal/arcadedb/memory_backfill.go`:
- Rewrite the header comment: the sweep keeps every memory row in the daemon's space. That covers the facts the old key, "absence of a vector", selected, and it catches every writer without knowing any of them.
- Keep `backfillBatch`. Keep `backfillRoundsPerTenant` with its comment now naming `ReEmbedAllFacts` as its user: the sweep is bounded by its run budget and rotation, not by rounds.
- Delete `embedMissingInBatches`.
- Add `rotation atomic.Uint64` to `TenantBackfill` (import `"sync/atomic"`).
- Replace `EmbedMissing`:

```go
// EmbedMissing runs the pass (memory_embed_pass.go) over every identity's memory and
// returns how many vectors it wrote. Its name is the cron seam's (handlers.MemoryEmbedder).
func (b *TenantBackfill) EmbedMissing(ctx context.Context, _ time.Time) (int, error) {
	if b == nil || b.embedder == nil {
		return 0, fmt.Errorf("arcadedb: memory embed backfill is not configured")
	}
	// No space, no pass: a hosted route without its key, or a sidecar that cannot name its
	// model, would fail every tenant the same way (spec §5).
	if _, err := b.embedder.Space(ctx); err != nil {
		return 0, fmt.Errorf("arcadedb: memory re-embed cannot run: %w", err)
	}
	return b.sweepTenants(ctx, "embed backfill",
		func(ctx context.Context, client *Client, database string) (int, error) {
			tally, err := client.WithEmbedder(b.embedder).reembedMemory(ctx)
			if tally.refused > 0 {
				slog.Warn("memory re-embed: records refused by the embedding model",
					"database", database, "refused", tally.refused)
			}
			return tally.embedded, err
		})
}
```
- In `sweepTenants`:
  - replace the loop head with `for _, identityID := range rotated(identities, b.rotation.Add(1)-1) {`;
  - as its first statement add

```go
		if ctx.Err() != nil {
			slog.Info("memory "+sweep+": run budget reached; the rest resumes next run",
				"count", total, "tenants", swept)
			return total, nil
		}
```
  - in the `case err != nil:` branch, before recording, add `if ctx.Err() != nil { continue }`, so a tenant cut off by the budget is not a failure.
  - `LinkMentions` shares this walk and gains the same two behaviours. Both are right for it too: it runs under its own cron budget, and its hub cap is per tenant. Add one line to the `sweepTenants` doc comment: "The walk starts one identity later on each run, and a run whose budget ends mid-walk reports what it did rather than failing."
- Add:

```go
// rotated starts the walk one identity later on each run, so a tenant whose backlog outlasts
// one run's budget cannot starve the ones behind it.
func rotated(ids []string, turn uint64) []string {
	if len(ids) == 0 {
		return ids
	}
	start := int(turn % uint64(len(ids)))
	return append(append(make([]string, 0, len(ids)), ids[start:]...), ids[:start]...)
}
```

In `internal/cron/handlers/memory_embed_backfill.go`:
- The `memoryEmbedBackfillMaxDuration` comment reads: the run budget; a tenant's backlog continues next run, and the tenant order rotates.
- The success format becomes `"memory embed backfill ok: embedded %d record(s)"`.
- The `MemoryEmbedder` comment says: re-embeds every memory row not in the daemon's space.

In `cmd/aura/serve_provisioning.go`, below `seedMemoryEmbedBackfillSweep`:

```go
// memorySweepTasks is the slice of *cron.Store the boot kick needs.
type memorySweepTasks interface {
	ListActiveTasks(ctx context.Context) ([]cron.Task, error)
	RunTaskNow(ctx context.Context, id string) error
}

// kickMemoryEmbedBackfill runs the memory pass on the scheduler's first tick. A route change
// restarts the daemon, and every memory read is lexical until the pass has run, so waiting
// out the five-minute schedule would keep memory degraded for nothing (spec §5).
func kickMemoryEmbedBackfill(ctx context.Context, tasks memorySweepTasks) error {
	active, err := tasks.ListActiveTasks(ctx)
	if err != nil {
		return fmt.Errorf("list active tasks: %w", err)
	}
	for _, task := range active {
		if task.Kind == cron.KindMemoryEmbedBackfill {
			return tasks.RunTaskNow(ctx, task.ID)
		}
	}
	return nil
}
```
In `cmd/aura/serve.go`, after the `seedMemoryEmbedBackfillSweep` block:

```go
	if err := kickMemoryEmbedBackfill(ctx, store); err != nil {
		slog.Warn("aura serve: kick memory embed backfill", "err", err)
	}
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./internal/arcadedb/ ./internal/cron/handlers/ ./cmd/arcadedb-mcp/ -count=1 && go test ./cmd/aura/ -run 'Kick|Backfill|Memory' -count=1"`
Expected: `ok` for every package.

Run: `wsl -e bash .superpowers/sdd/2026-09-23-embedding-memory-family/arcade-it.sh 'MemoryPassLive|ReEmbedAllFacts|MemorySpace|MemoryGateLive'`
Expected: all `PASS`, none `SKIP`.

- [ ] **Step 5: Record the pass's rules and the rebuild cost in the spec**

In the spec §5 "Failures" bullet list, after the refusal bullet, add:

```markdown
  - A record is set aside only once a control input has embedded in the same call: an
    unknown model id answers 400 to every input, and without the control one typo in a
    route would stamp a whole memory refused.
```
After "Turns." in §5 add: "A turn a space refused is re-selected by the pass once the space changes; the reconciler never asks again for a turn that has a vector or a refusal."
In "What this design does not prove" add:

```markdown
- **A full re-embed on ArcadeDB 26.9.1 costs one graph rebuild per vector index** on the first
  query after the next ArcadeDB restart: before 26.10.1 an updated vector is a delete plus an
  insert, and a delete forced that rebuild (arcadedb-docs `concepts/vector-search.adoc`).
  Memory indexes hold tens to thousands of vectors; the documents family is plan 3's to measure.
- **The run budget stops a pass mid-tenant.** The next run resumes from what is still in
  another space, starting one tenant later.
```

- [ ] **Step 6: Commit**

```bash
git add internal/arcadedb/memory_embed_pass.go internal/arcadedb/embedding_space.go internal/arcadedb/memory_vector.go \
  internal/arcadedb/memory_backfill.go internal/arcadedb/memory_embed_pass_test.go internal/arcadedb/memory_backfill_test.go \
  internal/arcadedb/memory_vector_test.go internal/arcadedb/memory_pass_live_integration_test.go \
  internal/cron/handlers/memory_embed_backfill.go cmd/aura/serve_provisioning.go cmd/aura/serve.go \
  cmd/aura/serve_provisioning_test.go docs/superpowers/specs/2026-09-23-embedding-model-change-design.md
git commit -F - <<'EOF'
feat(memory): the pass re-embeds every memory row outside the daemon's space

After a route change every stored vector is in the old space, and the fill of
"facts without a vector" selected none of them. The memory_embed_backfill sweep
now pages each of the three types by RID:
- it selects what is not stamped with the daemon's space and re-embeds it in
  batches;
- it sets aside a record the model refuses, once a control input shows the
  route works, and ends the run on any other failure;
- it stops cleanly at the run budget, rotates the tenant it starts from, and
  does not run at all without a space.
The daemon kicks it on boot, since a route change is a restart.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 7: The daemon's memory route reads its key live; MCP names its space from the embedder

**Files:**
- Create: `cmd/aura/memory_embedder.go`
- Modify: `cmd/aura/chat_boot.go:57,431-433,509`, `cmd/aura/chat_memory_projection.go:85-124`, `cmd/aura/chat_boot_memory.go:36-44,93-107`, `cmd/aura/serve_memory_backfill.go:51-78`, `cmd/arcadedb-mcp/boot_settings.go:87-99`, `cmd/arcadedb-mcp/main.go:67-75`
- Test: `cmd/aura/memory_embedder_test.go` (new), `cmd/aura/chat_boot_memory_capture_test.go:33,50-57`, `cmd/aura/chat_boot_test.go:485,526`, `cmd/aura/serve_memory_backfill_test.go:14-22`, `cmd/aura/two_identity_memory_harness_test.go:95`, `cmd/arcadedb-mcp/boot_settings_test.go` (`TestBootSpaceGivesUpOnAStalledSidecar`)

**Interfaces:**
- Consumes: `arcadedb.NewMemoryEmbedder`, `llm.Runtime.Snapshot()`.
- Produces:
  - `func memoryEmbedder(cfg *config.Config, runtime *llm.Runtime) arcadedb.DenseEmbedder`
  - `func newChatTenantClients(cfg *config.Config, embedder arcadedb.DenseEmbedder) *arcadedb.TenantClients`
  - `newChatConversationProjector(clients *arcadedb.TenantClients, source runner.ConversationProjectionSource)`
  - `newChatReasoningMemory(cfg *config.Config, clients *arcadedb.TenantClients)`
  - `buildMemoryCaptureQueue(clients *arcadedb.TenantClients)`
  - `chatEnv.memoryEmbedder arcadedb.DenseEmbedder`
  - `bootSpace(embedder arcadedb.DenseEmbedder, timeout time.Duration) (embeddings.Space, error)`

- [ ] **Step 1: Write the failing tests**

Create `cmd/aura/memory_embedder_test.go`:

```go
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
)

// A key rotated in the cockpit replaces the LLM profile in place (primaryLLMRouteReloader);
// the daemon never restarts for it, so the memory route must read it from there on every
// request (spec §5, "The key is read live").
func TestMemoryEmbedderReadsTheRotatedKey(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = io.WriteString(w, `{"data":[{"id":"vendor/embed","context_length":2048}]}`)
			return
		}
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		vector := make([]float64, 768)
		vector[0] = 1
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"index": 0, "embedding": vector}}})
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{Embed: config.EmbedConfig{CloudModel: "vendor/embed", CloudBaseURL: server.URL}}
	runtime := llm.NewRuntime(nil, llm.Config{APIKey: "boot-key"})
	embedder := memoryEmbedder(cfg, runtime)
	if _, err := embedder.Embed(t.Context(), []string{"a"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	runtime.Replace(nil, llm.Config{APIKey: "rotated-key"})
	if _, err := embedder.Embed(t.Context(), []string{"b"}); err != nil {
		t.Fatalf("Embed after rotation: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != "Bearer boot-key" || seen[1] != "Bearer rotated-key" {
		t.Fatalf("authorization = %q, want the key live at each request", seen)
	}
}

func TestMemoryEmbedderIsNilWithoutARoute(t *testing.T) {
	if embedder := memoryEmbedder(&config.Config{}, llm.NewRuntime(nil, llm.Config{})); embedder != nil {
		t.Fatalf("got %#v, want nil with no embedding base", embedder)
	}
}
```
`llm.Client` is an interface, so `llm.NewRuntime(nil, …)` publishes a profile with no chat client; the test needs only its key.

Update the existing tests to the new signatures:
- `cmd/aura/chat_boot_memory_capture_test.go:33`: count `"buildMemoryCaptureQueue(memoryClients)"`.
- Lines 50 and 57: `buildMemoryCaptureQueue(newChatTenantClients(withoutMemory, nil))` and `buildMemoryCaptureQueue(newChatTenantClients(configured, nil))`.
- `cmd/aura/chat_boot_test.go:485`: `newChatConversationProjector(newChatTenantClients(cfg, nil), emptyProjectionSource{})`.
- `cmd/aura/chat_boot_test.go:526`: `newChatReasoningMemory(cfg, newChatTenantClients(cfg, nil))`.
- `cmd/aura/two_identity_memory_harness_test.go:95`: `newChatTenantClients(cfg, nil)`.
- `cmd/aura/serve_memory_backfill_test.go`: `backfillEnv` sets `memoryEmbedder: arcadedb.NewMemoryEmbedder(config.EmbedConfig{BaseURL: embedURL}, nil),`.
- `cmd/arcadedb-mcp/boot_settings_test.go`, `TestBootSpaceGivesUpOnAStalledSidecar`: the call becomes `bootSpace(arcadedb.NewMemoryEmbedder(config.EmbedConfig{BaseURL: srv.URL}, nil), 50*time.Millisecond)`. Add the `arcadedb` import.

- [ ] **Step 2: Run the tests and watch them fail**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./cmd/aura/ -run 'MemoryEmbedder|MemoryCapture|ChatConversationProjector|ChatReasoningMemory|Backfill' -count=1; go test ./cmd/arcadedb-mcp/ -run BootSpace -count=1"`
Expected: FAIL at build time: `undefined: memoryEmbedder`, `too many arguments in call to newChatTenantClients`, `cannot use … as config.EmbedConfig` in `bootSpace`.

- [ ] **Step 3: Implement**

Create `cmd/aura/memory_embedder.go`:

```go
package main

import (
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
)

// memoryEmbedder is the daemon's one memory route. Its key is read from the running LLM
// profile on every request: a key rotated in the cockpit replaces that profile in place
// (serve_settings.go primaryLLMRouteReloader) without a restart, and a boot copy would keep
// embedding with the revoked key (spec §5).
func memoryEmbedder(cfg *config.Config, runtime *llm.Runtime) arcadedb.DenseEmbedder {
	return arcadedb.NewMemoryEmbedder(cfg.Embed, func() string { return runtime.Snapshot().Config.APIKey })
}
```

`cmd/aura/chat_memory_projection.go`:

```go
func newChatConversationProjector(
	clients *arcadedb.TenantClients,
	source runner.ConversationProjectionSource,
) *runner.ConversationProjector {
	if source == nil || clients == nil {
		return nil
	}
	return runner.NewConversationProjector(source, tenantConversationProjectionSink{clients: clients}, 0)
}

// newChatTenantClients is the daemon's one memory resolver: the conversation projector,
// the reasoning writer and the capture queue share it, and with it one cached client and
// one schema check per tenant.
func newChatTenantClients(cfg *config.Config, embedder arcadedb.DenseEmbedder) *arcadedb.TenantClients {
```
with the body unchanged except for the last statement:

```go
	return arcadedb.NewTenantClients(
		arcadedb.Config{BaseURL: cfg.ArcadeDB.BaseURL}, admin, embedder, credentials,
	)
```

`cmd/aura/chat_boot_memory.go`:

```go
func buildMemoryCaptureQueue(clients *arcadedb.TenantClients) *runner.MemoryCaptureQueue {
	if clients == nil {
		return nil
	}
	return runner.NewMemoryCaptureQueue(
		tenantMemoryCaptureSink{clients: clients}, runner.MemoryCaptureQueueConfig{},
	)
}
```
```go
func newChatReasoningMemory(cfg *config.Config, clients *arcadedb.TenantClients) *chatReasoningMemory {
	if cfg == nil || clients == nil {
		return nil
	}
	store := &tenantReasoningMemory{
```
(the rest unchanged; the `strings` import goes if it becomes unused).

`cmd/aura/chat_boot.go`:
- add the field `memoryEmbedder arcadedb.DenseEmbedder` to `chatEnv` (beside `llmRuntime`);
- replace lines 431-433 (the projector, the reasoning memory and the capture queue) with:

```go
	memoryDense := memoryEmbedder(cfg, llmRuntime)
	memoryClients := newChatTenantClients(cfg, memoryDense)
	conversationProjector := newChatConversationProjector(memoryClients, convStore)
	reasoningMemory := newChatReasoningMemory(cfg, memoryClients)
	memoryCaptureQueue := buildMemoryCaptureQueue(memoryClients)
```
- add `memoryEmbedder: memoryDense,` to the `chatEnv` literal on line 509.

`cmd/aura/serve_memory_backfill.go`:
- replace the two lines that built the embedder with `embedder := chat.memoryEmbedder`;
- update the doc comment: "the daemon's memory route, the same one every memory write uses, so the pass cannot write vectors from another model than the writes do".

`cmd/arcadedb-mcp/boot_settings.go`:

```go
// bootSpace names the space this process embeds memory in, for the boot log. The listener
// starts after it, and the embeddings client's own timeout is a minute.
func bootSpace(embedder arcadedb.DenseEmbedder, timeout time.Duration) (embeddings.Space, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return embedder.Space(ctx)
}
```
Its imports gain `arcadedb` and drop anything unused. The width now comes from the embedder (768, `vectorDimensions`), so the `config.DefaultEmbedDimensions` duplicate is gone. The `errString` comment ends in "the space is informational until plan 2 stamps it", which is now stale; it becomes: "the local sidecar may still be loading, and every write reads the space again, so a failed boot read costs only this log line."

`cmd/arcadedb-mcp/main.go`: `space, spaceErr := bootSpace(embedder, bootAttestTimeout)`.

- [ ] **Step 4: Run the tests and watch them pass**

Run: `wsl -e bash -lc "cd /mnt/d/Aura && go test ./cmd/aura/ -run 'MemoryEmbedder|MemoryCapture|ChatConversationProjector|ChatReasoningMemory|Backfill|Kick' -count=1 && go test ./cmd/arcadedb-mcp/ -count=1 && go vet -tags 'db_integration garage_integration authula_integration musr_e2e arcadedb_integration' ./cmd/aura/ ./cmd/arcadedb-mcp/"`
Expected: `ok` for both packages and a silent vet. The tag set is the harness's build line (`cmd/aura/two_identity_memory_harness_test.go:1`), so vet compiles it.

- [ ] **Step 5: Commit**

```bash
git add cmd/aura/memory_embedder.go cmd/aura/memory_embedder_test.go cmd/aura/chat_boot.go cmd/aura/chat_memory_projection.go \
  cmd/aura/chat_boot_memory.go cmd/aura/serve_memory_backfill.go cmd/aura/chat_boot_memory_capture_test.go \
  cmd/aura/chat_boot_test.go cmd/aura/serve_memory_backfill_test.go cmd/aura/two_identity_memory_harness_test.go \
  cmd/arcadedb-mcp/boot_settings.go cmd/arcadedb-mcp/main.go cmd/arcadedb-mcp/boot_settings_test.go
git commit -F - <<'EOF'
feat(memory): one daemon memory route, its key read from the live LLM profile

A key rotated in the cockpit replaces the LLM profile in place, and the daemon's
memory embedders held a boot copy: embeddings kept the revoked key until a
restart. The daemon now builds one memory route whose credential is read from
the running profile on every request. The projector, the reasoning writer, the
capture queue and the pass share it through one tenant resolver. MCP names its
boot space from its embedder, so the width comes from one place.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

## Out of scope for this plan

- The documents family (`Passage`, `IndexedDocument`): stamps in plan 3; gate, lexical mode and floors in plan 4. The daemon's document query embedder (`cmd/aura/embedding_client.go`) gets its live key in plan 4.
- Every re-read of the route after boot (the ingest supervisor in plan 3, MCP's watcher in plan 5), which must use the pre-overlay environment (spec §6).
- MCP exiting when its space or credential changes (plan 5), and the counts the cockpit and `aura doctor` show per family (plan 5).
