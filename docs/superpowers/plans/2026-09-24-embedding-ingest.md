# Embedding model change — ingest (plan 3 of 5) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The ingest pipeline embeds with the route the cockpit chose and stamps every `Passage` and `IndexedDocument` vector with its space. A route change re-embeds every document once without re-extracting any. A document that keeps failing keeps its old, visibly old-stamped rows.

**Architecture:**
- **Supervisor.** `internal/ingestsupervisor` gains a `RouteResolver`. On every reconcile tick it reads the route from `aura.settings` through the plan 1 helper `settings.EmbedRoute`, attests the local sidecar, and reads a hosted model's input limit once per route change. The route becomes part of each child's `ProcessSpec`, so a change restarts every child within one poll.
- **Python child.** Embedding moves out of `services/ingest/app.py` into `services/ingest/embed.py`, where `deps` is the space and a refused input is split out with `RetryWithSmallerBatch`. It also gains the hosted request shape, width validation and a byte cut. Extraction becomes a function memoized by content, apart from embedding.
- **Scripts.** They start their child with the supervisor's own resolution (`-print-embed-env`).

**Tech Stack:** Go 1.26, Python 3.12, CocoIndex 1.0.24 (`coco.fn` memo/batching/`deps`, `RetryWithSmallerBatch`, neo4j target over Bolt), ArcadeDB 26.9.1, llama.cpp `/v1/models` and `/tokenize`, OpenRouter `/v1/embeddings` and `/v1/embeddings/models`.

**Spec:** `docs/superpowers/specs/2026-09-23-embedding-model-change-design.md`, as follows:
- §6 (all of it);
- §2 for the documents family's stamps;
- the documents half of §1 (width from `AURA_EMBED_DIMENSIONS`);
- the documents-family rebuild measurement that "What this design does not prove" assigns to plan 3.

Plan 1 (`docs/superpowers/plans/2026-09-23-embedding-route-and-identity.md`) delivered `settings.EmbedRoute`, `config.ResolveEmbedRoute`, `config.EmbedRouteKind`, `embeddings.RouteSpace`, `SpaceFor` and `AttestLocal`. Plan 2 (`…-embedding-memory-family.md`, last commit `cba7118ae`) delivered the memory family.

## Global Constraints

- **Where things run.**
  - Go tests run in WSL, never as a Windows `.exe`: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test <pkg> -run "<regex>" -count=1'` (single quotes, so Git Bash does not expand `$HOME`).
  - Python tests run inside the `aura-ingest:local` image, which carries Tika, LibreOffice, `aura-filecard` and CocoIndex. They use the runner written in Task 4 Step 1. `docker` is invoked from Git Bash; WSL has no Docker integration.
- **Test scope.** Each task runs only the targeted tests it names. `go vet ./...`, race, deadcode, coverage and the full `make quality-full` run at the end of plan 5 or in CI (operator, 2026-09-23).
- **Git.**
  - `--no-verify` is forbidden.
  - Another session shares the git index: `git add` only the paths the task names, and never stage or format someone else's file.
  - Run `gofmt -l` on the task's Go files before committing.
  - Work master-direct. No push until plans 1–5 are done and CI is green.
- **Files.** No file above 600 lines, and every touched file gets its dead code removed in the same commit. `services/ingest/app.py` (617 today) must end below 600.
- **The child's embedding environment is exactly seven variables**, set by the supervisor and never derived by Python:
  - `AURA_EMBED_BASE_URL`;
  - `AURA_EMBED_MODEL`: empty on the local route, and what makes a route hosted, like Go's `config.EmbedRouteKind`;
  - `AURA_EMBED_API_KEY`;
  - `AURA_EMBED_SPACE`: required;
  - `AURA_EMBED_DIMENSIONS`;
  - `AURA_EMBED_INPUT_LIMIT`: hosted only, else `0`;
  - `AURA_EMBED_TOKENIZER_URL`: always the local sidecar.
- **The stamp column** is `embed_space STRING`, indexed `NOTUNIQUE NULL_STRATEGY INDEX`, on both `Passage` and `IndexedDocument` (spec §2; the index form was measured on 26.9.1 in plan 2).
- **Only 400, 413 and 422 split a batch** (Go's `embeddings.RejectsInput`). Every other failure fails the batch as it does today.
- **The supervisor never calls `settings.OverlayEnv`.** Its `os.LookupEnv` is therefore the pre-overlay environment spec §6 requires for re-reads after boot.
- **CocoIndex 1.0.24 facts this plan rests on**, read in the installed wheel's source on 2026-09-24 (PyPI `cocoindex-1.0.24-cp311-abi3-manylinux_2_28_x86_64.whl`):
  - `_internal/function.py` (`_compute_logic_fingerprint`, `fn`/`fn.as_async` docstrings, ~625-680 and ~2060-2200):
    - `deps` is folded into the logic fingerprint and snapshotted at decoration;
    - the change propagates to callers under `logic_tracking="full"` (the default);
    - the fingerprint includes the module and qualname, so moving `_embed` to `embed.py` is itself one re-run;
    - `fn.as_async(memo=, batching=, max_batch_size=, deps=)` accepts all four together.
  - `_internal/batching.py`:
    - `RetryWithSmallerBatch` halves down to single items, and at size 1 the cause becomes that item's own failure;
    - sibling sub-batches keep their results;
    - "Transient errors (rate limits, network blips) should first be retried at the same batch size".
  - `_internal/api.py:430-540`: `mount_each(fn, items, *args)` takes `(key, value)` pairs and passes the value first.
  - `_internal/environment.py`: `coco.lifespan` overrides an earlier one with a warning, and the default environment reads `COCOINDEX_DB`.
- **Measured facts carried from the spec** (VM spike, audit F1): a component whose body raises keeps its previous target rows, `coco.map` fails a whole file on its first failing item, and `as_async(batching=True)` groups calls across files.
- **No change touches the VM.** The E2E through the updater runs after plan 5.

## Review Focus

1. **The sidecar restarts, or times out, for several ticks.**
   - Expected: the children keep running on their spec, with no restart storm and no children started without a route.
   - If it comes back with the same GGUF, nothing restarts; with a different GGUF, every child restarts once.
   - Pinned: Task 2 `TestRouteResolverAttestsTheSidecarOnEveryCall`, and Task 3 `TestReconcileKeepsChildrenOnTheirRouteWhileItCannotBeRead`, which also asserts that an identical route after the error restarts nothing.
2. **A hosted catalogue that is down when the route changes.**
   - Expected: the resolver errors instead of handing the child a limit of 0; the children keep the old route, the log names the failure once, and the next tick retries.
   - Pinned: Task 2 `TestRouteResolverFailsWhenTheHostedCatalogueIsDown`, and Task 3's log-once test.
3. **One refused chunk batched together with a healthy file's chunks.**
   - Expected: the healthy file is embedded and stamped with the new space, and only the refused file keeps its old rows.
   - Pinned: Task 4 `test_one_refused_input_fails_only_its_own_caller` (engine split), and Task 6 pass 3 of the re-run test (end to end on ArcadeDB).
4. **A key rotated in the cockpit.**
   - Expected: every child restarts, because the fingerprint carries the key, but nothing is re-embedded, because the space does not name the key and `deps` is the space alone.
   - Pinned: Task 3 `TestProcessSpecFingerprintMovesWithTheRouteAndOnlyWithIt` (the key moves the fingerprint), and plan 1's `SpaceFor` tests (the key is not in the space).
5. **A row whose stamp disagrees with its vector**, the one defect the design exists to end.
   - Expected: never.
   - Pinned: Task 6's re-run test recomputes every row's vector from its own stamp in every pass, the refused file included.

## File Structure

| file | change |
|---|---|
| `docs/superpowers/specs/2026-09-23-embedding-model-change-design.md` | §6 amendments (Task 1); rebuild measurement (Task 8) |
| `internal/embeddings/fit.go`, `client.go` | `inputLimit` exported as `InputLimit` |
| `internal/settings/embed_route.go` | `DefaultEmbedBaseURL` (moved from `arcadedb-mcp`) |
| `cmd/arcadedb-mcp/boot_settings.go` | uses `settings.DefaultEmbedBaseURL` |
| `internal/ingestsupervisor/route.go` | new: `EmbedRoute`, `EmbedRoute.Environment`, `RouteResolver` |
| `internal/ingestsupervisor/supervisor.go` | `RouteSource`, `New(…, routes, …)`, route in `ProcessSpec`, env, fingerprint, last-good route |
| `cmd/aura-ingest-supervisor/main.go` | settings store, resolver, `-print-embed-env` |
| `compose.yaml` | the `aura-ingest` embed env comment |
| `services/ingest/embed.py` | new: embedding out of `app.py`, hosted route, width, key, space, split |
| `services/ingest/chunk.py` | tokenizer from `AURA_EMBED_TOKENIZER_URL` |
| `services/ingest/arcade.py` | `embed_space` DDL + index on both types |
| `services/ingest/app.py` | stamps; `_extract`/`index_object`/`mount_targets`; below 600 |
| `services/ingest/tests/…` | `test_embed_failure.py` follows the move; new `test_embed_route.py`, `test_embed_space_stamp.py`, `fake_embedder.py`, `space_driver.py`, `test_embed_space_reruns.py`; `test_chunk.py`, `test_arcade_integration.py` gain one test each |
| `Makefile` | `ingest-test` passes the space and the tokenizer URL |
| `scripts/ingest_embed_env.sh` | new: sourced helper, the child's route for scripts |
| `scripts/ingest_reconcile_e2e.sh`, `scripts/ingest_media_e2e.sh` | children start with `--env-file` from the helper |

---

### Task 1: Record the rulings in the spec

The spec is the plan's authority, and four of its §6 lines are wrong or silent where this plan must act. The amendment lands before any code (CLAUDE.md, "PRD-amendment commit prima del code commit").

**Files:**
- Modify: `docs/superpowers/specs/2026-09-23-embedding-model-change-design.md` (§6 and the Files table)

- [ ] **Step 1: Amend the pre-overlay paragraph**

Replace:
```
Every re-read after boot passes the helper a lookup over the environment **as it was before
`OverlayEnv`**. Both processes overlay rows into their own environment at boot and never
unset, so a live `os.LookupEnv` would bring a deleted row's boot value back as the fallback
(review of plan 1).
```
with:
```
Every re-read after boot passes the helper a lookup over the environment **as it was before
`OverlayEnv`**. `arcadedb-mcp` overlays rows into its own environment at boot and never
unsets, so its live `os.LookupEnv` would bring a deleted row's boot value back as the fallback
(review of plan 1). The supervisor never overlays, so its `os.LookupEnv` is that environment.
```

- [ ] **Step 2: Amend the supervisor bullets**

Replace the four bullets from `- \`ProcessSpec\`, \`Environment()\` and \`fingerprint()\` gain` through `…the fail-closed behaviour \`arcadedb-mcp\` already has.` with:
```
- `ProcessSpec`, `Environment()` and `fingerprint()` gain `AURA_EMBED_BASE_URL`,
  `AURA_EMBED_MODEL`, `AURA_EMBED_API_KEY` (the sealed `OPENROUTER_API_KEY`: no embed-specific
  key exists, `bd31e157b`), `AURA_EMBED_SPACE`, `AURA_EMBED_DIMENSIONS` (the width the space
  was computed at, so the child cannot write another), `AURA_EMBED_INPUT_LIMIT`, and
  `AURA_EMBED_TOKENIZER_URL`, which is always the local sidecar.
- A route change restarts every child within one poll.
- The local attestation is read on every tick, as the daemon's route reads it on every call
  (§1, `embeddings.Route`): a GGUF swapped under unchanged settings changes the space, and a
  supervisor that attested only on a route change would stamp the new model's vectors with
  the old space. The hosted input limit is read once per route change, never per tick.
  (Amended 2026-09-24 in plan 3; the first text read both once per route change.)
- A failed settings read keeps the running children and their specs. At startup it starts
  nothing, the fail-closed behaviour `arcadedb-mcp` already has. A route that cannot embed --
  a hosted model without a credential, a local route without a base, a sidecar that does not
  answer its attestation -- is a failed read too, logged once per change. An identity that
  starts meanwhile starts on the last route that resolved.
- `aura-ingest-supervisor -print-embed-env` prints the environment a child would receive,
  credential included, and exits. The E2E scripts that run `python -m ingest.app` directly
  start their child with it, so no child ever stamps a space the resolver did not name.
```

- [ ] **Step 3: Amend the split rule**

Replace:
```
- raises `coco.RetryWithSmallerBatch() from err` on a failed request, so one bad input fails
  only its own caller (audit F1);
```
with:
```
- raises `coco.RetryWithSmallerBatch() from err` when the provider refuses the input (400,
  413, 422: `embeddings.RejectsInput`'s statuses), so one bad input fails only its own caller
  (audit F1). Any other failure fails the batch as before: a 401, 403, 429, 5xx or transport
  error fails the same way at any size, and CocoIndex's own contract says transient errors
  should not split (`cocoindex/_internal/batching.py`, 1.0.24). (Amended 2026-09-24, plan 3.)
```

- [ ] **Step 4: Amend the extraction split**

Replace:
```
**Extraction is memoized by content.** `process_file` splits:
- `_extract(content, file_name, content_type)`, memoized, returns text, card and anchors;
- the rest chunks and embeds.
```
with:
```
**Extraction is memoized by content.** `process_file` splits:
- `_extract(content, suffix, file_name, content_type)`, memoized, returns text, card and
  anchors. `suffix` is the object key's extension, which names the temporary file the
  extractors route on, as it does today. `_card` loses its own memo: it keyed on that
  temporary path and never hit. The `[extract]` log line moves into `_extract`, so it still
  counts real extractions (`scripts/ingest_reconcile_e2e.sh` asserts on the count);
- the rest (`index_object`) chunks and embeds.
```

- [ ] **Step 5: Extend the Files table**

Replace the row `| \`cmd/aura-ingest-supervisor/main.go\` | 72 | settings store wiring |` with these rows:
```
| `cmd/aura-ingest-supervisor/main.go` | 72 | settings store wiring; `-print-embed-env` |
| `internal/ingestsupervisor/route.go` | new | `EmbedRoute`, `RouteResolver` (resolved every tick) |
| `internal/settings/embed_route.go` | 57 | `DefaultEmbedBaseURL`, shared with `arcadedb-mcp` |
| `scripts/ingest_embed_env.sh` | new | a script's child gets the supervisor's route |
| `scripts/ingest_reconcile_e2e.sh`, `scripts/ingest_media_e2e.sh`, `Makefile` | — | children start with the resolved route; `ingest-test` env |
| `services/ingest/tests/space_driver.py`, `fake_embedder.py` | new | the §6 re-run test's driver |
```

- [ ] **Step 6: Commit**

```bash
git add docs/superpowers/specs/2026-09-23-embedding-model-change-design.md
git commit -m "docs(spec): attest per tick, split only refused inputs, extract by suffix

Plan 3 reading the code against §6 found four places the spec could not be implemented as
written. The supervisor attesting only on a route change would stamp a swapped GGUF's
vectors with the old space, which is the defect §1's run-time attestation exists to catch;
splitting on every failure would multiply requests to a rate-limited provider, which
CocoIndex's own batching contract advises against; the temporary file the extractors route
on takes its suffix from the object key, not the file name; and a route that cannot embed
needed a stated behaviour.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: The route resolver

**Files:**
- Modify: `internal/embeddings/fit.go:28-43`, `internal/embeddings/client.go:123`
- Modify: `internal/settings/embed_route.go`
- Modify: `cmd/arcadedb-mcp/boot_settings.go:14-17,79`
- Create: `internal/ingestsupervisor/route.go`
- Test: `internal/ingestsupervisor/route_test.go`

**Interfaces:**
- Consumes (plan 1):
  - `settings.EmbedRoute(ctx, settings.SecretLister, func(string) (string, bool), defaultLocalBase string) (config.EmbedConfig, string, error)`;
  - `config.ResolveEmbedRoute(config.EmbedConfig, apiKey string) (baseURL, credential, model string)`;
  - `config.EmbedRouteKind(config.EmbedConfig) config.EmbedKind`;
  - `embeddings.RouteSpace(ctx, *http.Client, config.EmbedConfig, dims int) (embeddings.Space, error)`;
  - `embeddings.ErrNoCredential`, `embeddings.ErrNoRoute`.
- Produces:
  - `func (c *embeddings.Client) InputLimit(ctx context.Context) (int, error)`;
  - `const settings.DefaultEmbedBaseURL = "http://aura-llama-embed:8081"`;
  - `type ingestsupervisor.EmbedRoute struct{ BaseURL, Model, APIKey, Space string; Dimensions, InputLimit int; TokenizerURL string }`;
  - `func (r EmbedRoute) Environment() []string`: the seven `KEY=VALUE` entries, in this order: BASE_URL, MODEL, API_KEY, SPACE, DIMENSIONS, INPUT_LIMIT, TOKENIZER_URL;
  - `type ingestsupervisor.RouteResolver struct{ Store settings.SecretLister; LookupEnv func(string) (string, bool); Dimensions int; HTTP *http.Client }` with `func (r *RouteResolver) Resolve(ctx context.Context) (EmbedRoute, error)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/ingestsupervisor/route_test.go`:
```go
package ingestsupervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/embeddings"
)

type routeRows struct {
	rows   []sqlc.AuraSettings
	secret string
}

func (r routeRows) List(context.Context) ([]sqlc.AuraSettings, error) { return r.rows, nil }
func (r routeRows) Secret(context.Context, string) (string, error)    { return r.secret, nil }

func noEnv(string) (string, bool) { return "", false }

// localSidecar answers /v1/models as llama.cpp b10964 did on the lab VM (trimmed to what the
// attestation reads), with a GGUF size the test can change under it.
func localSidecar(t *testing.T, size *atomic.Int64) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprintf(w, `{"data":[{"id":"/models/embeddinggemma-300M-Q8_0.gguf",`+
			`"meta":{"n_embd":768,"n_params":307581696,"size":%d,"ftype":"Q8_0"}}]}`, size.Load())
	}))
	t.Cleanup(server.Close)
	return server
}

// hostedCatalogue serves an OpenRouter-shaped /v1/embeddings/models and counts its reads.
func hostedCatalogue(t *testing.T, reads *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings/models" {
			http.NotFound(w, r)
			return
		}
		reads.Add(1)
		_, _ = io.WriteString(w, `{"data":[{"id":"vendor/embed","context_length":8192},`+
			`{"id":"vendor/other","context_length":512}]}`)
	}))
	t.Cleanup(server.Close)
	return server
}

func hostedRows(base, model string) []sqlc.AuraSettings {
	return []sqlc.AuraSettings{
		{Key: "AURA_EMBED_BASE_URL", Value: "http://aura-llama-embed:8081"},
		{Key: "AURA_EMBED_MODEL", Value: model},
		{Key: "AURA_EMBED_CLOUD_BASE_URL", Value: base},
	}
}

func TestRouteResolverNamesTheLocalSidecarsSpace(t *testing.T) {
	var size atomic.Int64
	size.Store(327060480)
	sidecar := localSidecar(t, &size)
	resolver := &RouteResolver{
		Store:     routeRows{rows: []sqlc.AuraSettings{{Key: "AURA_EMBED_BASE_URL", Value: sidecar.URL}}},
		LookupEnv: noEnv, Dimensions: 768, HTTP: sidecar.Client(),
	}

	route, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := embeddings.SpaceFor(config.EmbedLocal, "", "", 768,
		"embeddinggemma-300M-Q8_0.gguf|size=327060480|params=307581696|embd=768|ftype=Q8_0")
	if route.Space != want.ID {
		t.Fatalf("space = %q, want %q: a child would stamp a space the daemon never names", route.Space, want.ID)
	}
	if route.BaseURL != sidecar.URL || route.TokenizerURL != sidecar.URL || route.Model != "" ||
		route.APIKey != "" || route.InputLimit != 0 || route.Dimensions != 768 {
		t.Fatalf("route = %+v, want the local sidecar with no model, key or limit", route)
	}
}

// A GGUF swapped under unchanged settings moves the space on the next tick, as it moves on the
// daemon's next call (embeddings.Route): attesting only on a route change would stamp the new
// model's vectors with the old space.
func TestRouteResolverAttestsTheSidecarOnEveryCall(t *testing.T) {
	var size atomic.Int64
	size.Store(327060480)
	sidecar := localSidecar(t, &size)
	resolver := &RouteResolver{
		Store:     routeRows{rows: []sqlc.AuraSettings{{Key: "AURA_EMBED_BASE_URL", Value: sidecar.URL}}},
		LookupEnv: noEnv, Dimensions: 768, HTTP: sidecar.Client(),
	}

	before, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	same, err := resolver.Resolve(context.Background())
	if err != nil || same.Space != before.Space {
		t.Fatalf("an unchanged sidecar moved the space (%q -> %q, %v): every tick would restart the children",
			before.Space, same.Space, err)
	}
	size.Store(999)
	after, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve after the swap: %v", err)
	}
	if after.Space == before.Space {
		t.Fatal("a swapped GGUF kept the space")
	}
}

func TestRouteResolverReadsAHostedModelsLimitOncePerRoute(t *testing.T) {
	var reads atomic.Int32
	catalogue := hostedCatalogue(t, &reads)
	resolver := &RouteResolver{
		Store:     routeRows{rows: hostedRows(catalogue.URL, "vendor/embed"), secret: "sk-test"},
		LookupEnv: noEnv, Dimensions: 768, HTTP: catalogue.Client(),
	}

	route, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := embeddings.SpaceFor(config.EmbedEndpoint, "vendor/embed", catalogue.URL, 768, "")
	if route.Space != want.ID || route.Model != "vendor/embed" || route.APIKey != "sk-test" ||
		route.BaseURL != catalogue.URL || route.InputLimit != 8192 ||
		route.TokenizerURL != "http://aura-llama-embed:8081" {
		t.Fatalf("route = %+v, want vendor/embed at %s, limit 8192, local tokenizer", route, catalogue.URL)
	}
	if _, err := resolver.Resolve(context.Background()); err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if got := reads.Load(); got != 1 {
		t.Fatalf("catalogue read %d times for one route, want once: it would be read on every tick", got)
	}

	resolver.Store = routeRows{rows: hostedRows(catalogue.URL, "vendor/other"), secret: "sk-test"}
	other, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve after the model change: %v", err)
	}
	if other.InputLimit != 512 || reads.Load() != 2 {
		t.Fatalf("limit %d after %d reads, want 512 after a second read: a new model kept the old limit",
			other.InputLimit, reads.Load())
	}
}

func TestRouteResolverFailsWhenTheHostedCatalogueIsDown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	resolver := &RouteResolver{
		Store:     routeRows{rows: hostedRows(server.URL, "vendor/embed"), secret: "sk-test"},
		LookupEnv: noEnv, Dimensions: 768, HTTP: server.Client(),
	}

	if route, err := resolver.Resolve(context.Background()); err == nil {
		t.Fatalf("resolved %+v with no input limit: the child would cut every input to nothing", route)
	}
}

func TestRouteResolverRefusesAHostedRouteWithoutACredential(t *testing.T) {
	var reads atomic.Int32
	catalogue := hostedCatalogue(t, &reads)
	resolver := &RouteResolver{
		Store:     routeRows{rows: hostedRows(catalogue.URL, "vendor/embed")},
		LookupEnv: noEnv, Dimensions: 768, HTTP: catalogue.Client(),
	}

	_, err := resolver.Resolve(context.Background())
	if !errors.Is(err, embeddings.ErrNoCredential) {
		t.Fatalf("Resolve error = %v, want ErrNoCredential", err)
	}
	if reads.Load() != 0 {
		t.Fatal("the catalogue was asked for a route that cannot embed")
	}
}

func TestRouteResolverRefusesALocalRouteWithNoBase(t *testing.T) {
	resolver := &RouteResolver{
		Store:     routeRows{rows: []sqlc.AuraSettings{{Key: "AURA_EMBED_BASE_URL", Value: ""}}},
		LookupEnv: noEnv, Dimensions: 768,
	}

	if _, err := resolver.Resolve(context.Background()); !errors.Is(err, embeddings.ErrNoRoute) {
		t.Fatalf("Resolve error = %v, want ErrNoRoute", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test ./internal/ingestsupervisor/ -run "RouteResolver" -count=1'`
Expected: build failure, `undefined: RouteResolver`.

- [ ] **Step 3: Export the input-limit read**

In `internal/embeddings/fit.go`, replace:
```go
// inputLimit returns the model's input limit in tokens. It is read from the route's
// catalogue on first use and kept only once read, so a sidecar that was not up yet is
// asked again rather than guessed at.
func (c *Client) inputLimit(ctx context.Context) (int, error) {
```
with:
```go
// InputLimit returns the model's input limit in tokens. It is read from the route's
// catalogue on first use and kept only once read, so a sidecar that was not up yet is
// asked again rather than guessed at. The ingest supervisor reads it to hand a hosted
// model's limit to its Python children, which cut inputs to it in bytes.
func (c *Client) InputLimit(ctx context.Context) (int, error) {
```
In `internal/embeddings/client.go:123`, replace `limit, err := c.inputLimit(ctx)` with `limit, err := c.InputLimit(ctx)`.

- [ ] **Step 4: Share the local default**

In `internal/settings/embed_route.go`, add above `// EmbedRoute reads`:
```go
// DefaultEmbedBaseURL is the product's local sidecar, the base a route falls back to when
// neither a row nor the environment names one. An explicitly empty row still wins: that is
// how the operator switches dense retrieval off.
const DefaultEmbedBaseURL = "http://aura-llama-embed:8081"

```
In `cmd/arcadedb-mcp/boot_settings.go`, delete these lines:
```go
// This is the product default when neither a row nor the environment names a local base.
// An explicitly empty row still wins, so the operator can disable dense retrieval.
const defaultMemoryEmbedBaseURL = "http://aura-llama-embed:8081"

```
Then replace `settings.EmbedRoute(ctx, store, os.LookupEnv, defaultMemoryEmbedBaseURL)` with `settings.EmbedRoute(ctx, store, os.LookupEnv, settings.DefaultEmbedBaseURL)`.

- [ ] **Step 5: Write the resolver**

Create `internal/ingestsupervisor/route.go`:
```go
package ingestsupervisor

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
	"github.com/chetto1983/aura/internal/settings"
)

// EmbedRoute is the embedding binding every child receives (services/ingest/embed.py). A
// child derives none of it: the space in particular is named here, by the resolution the
// daemon runs, so a Python vector and a Go vector from one route carry one stamp (spec §1).
type EmbedRoute struct {
	BaseURL    string
	Model      string // empty on the local route
	APIKey     string // empty on the local route; never logged
	Space      string
	Dimensions int
	// InputLimit is a hosted model's published limit in tokens, which the child turns into a
	// byte cut; 0 on the local route, where the child asks the tokenizer instead.
	InputLimit int
	// TokenizerURL is the local sidecar whichever route embeds: chunk boundaries are counted
	// with its tokenizer, so a model change never re-chunks a document (audit F9).
	TokenizerURL string
}

// Environment is the route as the child reads it.
func (r EmbedRoute) Environment() []string {
	return []string{
		"AURA_EMBED_BASE_URL=" + r.BaseURL,
		"AURA_EMBED_MODEL=" + r.Model,
		"AURA_EMBED_API_KEY=" + r.APIKey,
		"AURA_EMBED_SPACE=" + r.Space,
		"AURA_EMBED_DIMENSIONS=" + strconv.Itoa(r.Dimensions),
		"AURA_EMBED_INPUT_LIMIT=" + strconv.Itoa(r.InputLimit),
		"AURA_EMBED_TOKENIZER_URL=" + r.TokenizerURL,
	}
}

// RouteResolver reads the route from aura.settings on every call. The supervisor never
// overlays the rows onto its own environment, so LookupEnv is the environment as it was
// before any row: the fallback a deleted row must fall back to (spec §6).
type RouteResolver struct {
	Store      settings.SecretLister
	LookupEnv  func(string) (string, bool)
	Dimensions int
	HTTP       *http.Client

	limit hostedLimit
}

// hostedLimit remembers the last hosted model's input limit: the catalogue is read once per
// route change, not on every tick.
type hostedLimit struct {
	base, model string
	tokens      int
}

// Resolve names the route and its space. The local sidecar is attested on every call, as the
// daemon's route attests it (embeddings.Route), so a GGUF swapped under unchanged settings
// moves the space on the next tick. A route that cannot embed -- a hosted model without a
// credential, a local route without a base -- is an error, and the caller keeps what runs.
func (r *RouteResolver) Resolve(ctx context.Context) (EmbedRoute, error) {
	embed, key, err := settings.EmbedRoute(ctx, r.Store, r.LookupEnv, settings.DefaultEmbedBaseURL)
	if err != nil {
		return EmbedRoute{}, err
	}
	base, credential, model := config.ResolveEmbedRoute(embed, key)
	hosted := config.EmbedRouteKind(embed) != config.EmbedLocal
	if hosted && credential == "" {
		return EmbedRoute{}, embeddings.ErrNoCredential
	}
	space, err := embeddings.RouteSpace(ctx, r.HTTP, embed, r.Dimensions)
	if err != nil {
		return EmbedRoute{}, fmt.Errorf("embedding space: %w", err)
	}
	route := EmbedRoute{
		BaseURL: base, Model: model, APIKey: credential, Space: space.ID,
		Dimensions: r.Dimensions, TokenizerURL: embed.BaseURL,
	}
	if hosted {
		if route.InputLimit, err = r.inputLimit(ctx, base, model, credential); err != nil {
			return EmbedRoute{}, err
		}
	}
	return route, nil
}

func (r *RouteResolver) inputLimit(ctx context.Context, base, model, key string) (int, error) {
	if r.limit.base == base && r.limit.model == model {
		return r.limit.tokens, nil
	}
	client := &embeddings.Client{BaseURL: base, Model: model, APIKey: key, Client: r.HTTP}
	tokens, err := client.InputLimit(ctx)
	if err != nil {
		return 0, fmt.Errorf("embedding input limit: %w", err)
	}
	r.limit = hostedLimit{base: base, model: model, tokens: tokens}
	return tokens, nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test ./internal/ingestsupervisor/ ./internal/embeddings/ ./internal/settings/ ./cmd/arcadedb-mcp/ -count=1'`
Expected: all four packages `ok`. The six `RouteResolver` tests pass.

- [ ] **Step 7: Commit**

```bash
gofmt -l internal/ingestsupervisor internal/embeddings internal/settings cmd/arcadedb-mcp
git add internal/ingestsupervisor/route.go internal/ingestsupervisor/route_test.go \
  internal/embeddings/fit.go internal/embeddings/client.go \
  internal/settings/embed_route.go cmd/arcadedb-mcp/boot_settings.go
git commit -m "feat(ingest): resolve the embedding route and its space for the ingest children

The ingest worker has always embedded with the local sidecar whatever the cockpit chose,
because nothing handed it a route. The resolver reads the same aura.settings rows the daemon
reads, attests the local sidecar on every call as the daemon's route does, and reads a
hosted model's input limit once per route change. A route that cannot embed is an error, so
the supervisor can keep what runs instead of starting children that fail every file.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: The supervisor hands every child the route

**Files:**
- Modify: `internal/ingestsupervisor/supervisor.go`
- Modify: `cmd/aura-ingest-supervisor/main.go`
- Modify: `compose.yaml` (the `aura-ingest` `AURA_EMBED_BASE_URL` lines, ~969)
- Test: `internal/ingestsupervisor/supervisor_test.go`

**Interfaces:**
- Consumes: `EmbedRoute`, `EmbedRoute.Environment()` and `RouteResolver` (Task 2).
- Produces:
  - `type RouteSource interface{ Resolve(context.Context) (EmbedRoute, error) }`;
  - `func New(lister IdentityLister, resolver CredentialResolver, routes RouteSource, launcher Launcher, options Options) *Supervisor`;
  - the `ProcessSpec.Embed EmbedRoute` field;
  - the flag `aura-ingest-supervisor -print-embed-env`, which prints `EmbedRoute.Environment()` one line each and exits 0.

- [ ] **Step 1: Update the tests to the new contract and add the route tests**

In `internal/ingestsupervisor/supervisor_test.go`:

(a) Add after the `fakeResolver` methods:
```go
type fakeRoutes struct {
	route EmbedRoute
	err   error
}

func (f *fakeRoutes) Resolve(context.Context) (EmbedRoute, error) { return f.route, f.err }

var testRoute = EmbedRoute{
	BaseURL: "http://aura-llama-embed:8081", Space: "es1-0123456789abcdef", Dimensions: 768,
	TokenizerURL: "http://aura-llama-embed:8081",
}
```

(b) Every existing `New(` call gains the route source as its third argument:
- `New(lister, resolver, launcher, Options{` becomes `New(lister, resolver, &fakeRoutes{route: testRoute}, launcher, Options{`;
- in `TestReconcileDoesNotRepeatUnprovisionedWarningEveryPoll` and `TestSupervisorRunStopsChildrenOnCancellation`, insert `&fakeRoutes{route: testRoute},` after the `&fakeResolver{…}` argument;
- `New(nil, nil, nil, Options{})` becomes `New(nil, nil, nil, nil, Options{})`.

(c) In `TestSupervisorRunRejectsMissingDependencies`, the expected text becomes `"requires identity, credential, embedding route, and process dependencies"`. The contract gained a dependency; the old message would name three of four.

(d) In `TestReconcileStartsOnlyProvisionedActiveUsersWithIsolatedState`, add after the S3 route check:
```go
	if spec.Embed != testRoute {
		t.Fatalf("Embed = %+v, want the resolved route %+v", spec.Embed, testRoute)
	}
```

(e) In `TestProcessSpecEnvironmentContainsOnlyItsIdentityBinding`, add an `Embed` field to the spec literal:
```go
		Embed: EmbedRoute{
			BaseURL: "https://openrouter.ai/api", Model: "vendor/embed", APIKey: "sk-user",
			Space: "es1-0123456789abcdef", Dimensions: 768, InputLimit: 8192,
			TokenizerURL: "http://aura-llama-embed:8081",
		},
```
Add `"AURA_EMBED_BASE_URL=http://compose-default:8081",` to the base environment slice, and add these entries to the `want` map:
```go
		"AURA_EMBED_BASE_URL":      "https://openrouter.ai/api",
		"AURA_EMBED_MODEL":         "vendor/embed",
		"AURA_EMBED_API_KEY":       "sk-user",
		"AURA_EMBED_SPACE":         "es1-0123456789abcdef",
		"AURA_EMBED_DIMENSIONS":    "768",
		"AURA_EMBED_INPUT_LIMIT":   "8192",
		"AURA_EMBED_TOKENIZER_URL": "http://aura-llama-embed:8081",
```
and after the secret count check:
```go
	if got := environmentCount(env, "AURA_EMBED_BASE_URL"); got != 1 {
		t.Errorf("embed base env count = %d, want the route to replace compose's fallback", got)
	}
```

(f) Append the new tests:
```go
func twoProvisionedUsers() (*fakeLister, *fakeResolver, []string) {
	ids := []string{"a696df2b-b7bc-4ee7-870b-15d2cced1839", "76db7481-0175-49f4-8c55-aab4d26e14ae"}
	return &fakeLister{items: []identity.Identity{{ID: ids[0], Kind: "user"}, {ID: ids[1], Kind: "user"}}},
		&fakeResolver{credentials: map[string]objectstore.Credentials{
			ids[0]: {Bucket: "aura-a", AccessKey: "key-a", SecretKey: "secret-a"},
			ids[1]: {Bucket: "aura-b", AccessKey: "key-b", SecretKey: "secret-b"},
		}}, ids
}

func TestReconcileRestartsEveryChildWhenTheRouteChanges(t *testing.T) {
	lister, resolver, _ := twoProvisionedUsers()
	routes := &fakeRoutes{route: testRoute}
	launcher := &fakeLauncher{}
	supervisor := New(lister, resolver, routes, launcher, Options{PollInterval: time.Second, StateRoot: "/state"})
	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	routes.route.Space = "es1-fedcba9876543210"
	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile after the route change: %v", err)
	}
	if !launcher.processes[0].stopped || !launcher.processes[1].stopped {
		t.Fatal("a child kept embedding with the old route after one poll")
	}
	if len(launcher.specs) != 4 {
		t.Fatalf("starts = %d, want both children restarted once", len(launcher.specs))
	}
	for _, spec := range launcher.specs[2:] {
		if spec.Embed.Space != "es1-fedcba9876543210" {
			t.Fatalf("restarted child space = %q, want the new one", spec.Embed.Space)
		}
	}
}

func TestReconcileKeepsChildrenOnTheirRouteWhileItCannotBeRead(t *testing.T) {
	lister, resolver, ids := twoProvisionedUsers()
	lister.items = lister.items[:1]
	routes := &fakeRoutes{route: testRoute}
	launcher := &fakeLauncher{}
	supervisor := New(lister, resolver, routes, launcher, Options{PollInterval: time.Second, StateRoot: "/state"})
	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}

	routes.route, routes.err = EmbedRoute{}, errors.New("attest local embedder: connection refused")
	lister.items = append(lister.items, identity.Identity{ID: ids[1], Kind: "user"})
	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile while the route is unreadable: %v", err)
	}
	if launcher.processes[0].stopped {
		t.Fatal("a running child was stopped because the route could not be read")
	}
	if len(launcher.specs) != 2 || launcher.specs[1].Embed != testRoute {
		t.Fatalf("specs = %+v, want the new identity started on the last route that resolved", launcher.specs)
	}

	routes.route, routes.err = testRoute, nil
	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile after the route came back: %v", err)
	}
	if len(launcher.specs) != 2 || launcher.processes[0].stopped || launcher.processes[1].stopped {
		t.Fatal("the same route coming back restarted children: a sidecar blip would be a restart storm")
	}

	lister.items = lister.items[1:]
	routes.route, routes.err = EmbedRoute{}, errors.New("settings database unreachable")
	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile removing an identity: %v", err)
	}
	if !launcher.processes[0].stopped {
		t.Fatal("a removed identity kept its child because the route could not be read")
	}
}

func TestReconcileStartsNothingBeforeARouteResolves(t *testing.T) {
	lister, resolver, _ := twoProvisionedUsers()
	routes := &fakeRoutes{err: embeddings.ErrNoCredential}
	launcher := &fakeLauncher{}
	supervisor := New(lister, resolver, routes, launcher, Options{PollInterval: time.Second, StateRoot: "/state"})

	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile without a route: %v", err)
	}
	if len(launcher.specs) != 0 {
		t.Fatalf("started %d children with no route: they would embed with nothing", len(launcher.specs))
	}
	routes.route, routes.err = testRoute, nil
	if err := supervisor.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile once the route resolves: %v", err)
	}
	if len(launcher.specs) != 2 {
		t.Fatalf("starts = %d, want both children once the route resolves", len(launcher.specs))
	}
}

func TestReconcileLogsAnUnresolvedRouteOncePerChange(t *testing.T) {
	lister, resolver, _ := twoProvisionedUsers()
	var logs bytes.Buffer
	supervisor := New(lister, resolver, &fakeRoutes{err: embeddings.ErrNoCredential}, &fakeLauncher{},
		Options{PollInterval: time.Second, Logger: slog.New(slog.NewTextHandler(&logs, nil))})

	for range 3 {
		if err := supervisor.Reconcile(t.Context()); err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
	}
	if got := strings.Count(logs.String(), "embedding route unresolved"); got != 1 {
		t.Fatalf("warning count = %d, want one per change of failure; logs=%q", got, logs.String())
	}
}

func TestProcessSpecFingerprintMovesWithTheRouteAndOnlyWithIt(t *testing.T) {
	base := ProcessSpec{
		IdentityID: "a696df2b-b7bc-4ee7-870b-15d2cced1839", Bucket: "aura-user", AccessKey: "k",
		SecretKey: "s", S3Endpoint: "http://garage:3900", S3Region: "garage", StateDB: "/state/u/coco.db",
		Embed: EmbedRoute{
			BaseURL: "http://embed:8081", Model: "vendor/embed", APIKey: "sk", Space: "es1-aaaaaaaaaaaaaaaa",
			Dimensions: 768, InputLimit: 8192, TokenizerURL: "http://tokenizer:8081",
		},
	}
	same := base
	if same.fingerprint() != base.fingerprint() {
		t.Fatal("an identical spec moved its fingerprint: every tick would restart the child")
	}
	for name, change := range map[string]func(*EmbedRoute){
		"base":       func(r *EmbedRoute) { r.BaseURL = "http://other:8081" },
		"model":      func(r *EmbedRoute) { r.Model = "vendor/other" },
		"key":        func(r *EmbedRoute) { r.APIKey = "sk-rotated" },
		"space":      func(r *EmbedRoute) { r.Space = "es1-bbbbbbbbbbbbbbbb" },
		"dimensions": func(r *EmbedRoute) { r.Dimensions = 1024 },
		"limit":      func(r *EmbedRoute) { r.InputLimit = 512 },
		"tokenizer":  func(r *EmbedRoute) { r.TokenizerURL = "http://elsewhere:8081" },
	} {
		changed := base
		change(&changed.Embed)
		if changed.fingerprint() == base.fingerprint() {
			t.Errorf("a %s change kept the fingerprint: the child would never learn it", name)
		}
	}
}
```
Add `"github.com/chetto1983/aura/internal/embeddings"` to the test imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test ./internal/ingestsupervisor/ -count=1'`
Expected: build failure: `too many arguments in call to New`, `unknown field Embed in struct literal of type ProcessSpec`.

- [ ] **Step 3: Give the supervisor the route**

In `internal/ingestsupervisor/supervisor.go`:

(a) After the `Launcher` interface, add:
```go
// RouteSource resolves the embedding route every child embeds with (RouteResolver).
type RouteSource interface {
	Resolve(context.Context) (EmbedRoute, error)
}
```

(b) In `ProcessSpec`, after `StateDB string`, add:
```go
	// Embed is the embedding route. It is part of the fingerprint, so a route change
	// restarts the child and CocoIndex re-embeds under the new space.
	Embed EmbedRoute
```

(c) In `Environment`, replace `overrides := []string{` … `}` so the identity entries are followed by the route:
```go
	overrides := append([]string{
		"AURA_INGEST_IDENTITY_ID=" + s.IdentityID,
		"AURA_INGEST_S3_ENDPOINT=" + s.S3Endpoint,
		"AURA_INGEST_S3_BUCKET=" + s.Bucket,
		"AURA_INGEST_S3_ACCESS_KEY_ID=" + s.AccessKey,
		"AURA_INGEST_S3_SECRET_ACCESS_KEY=" + s.SecretKey,
		"AURA_INGEST_S3_REGION=" + s.S3Region,
		"COCOINDEX_DB=" + s.StateDB,
	}, s.Embed.Environment()...)
```

(d) Replace `fingerprint` with:
```go
func (s ProcessSpec) fingerprint() [sha256.Size]byte {
	fields := []string{s.IdentityID, s.Bucket, s.AccessKey, s.SecretKey, s.S3Endpoint, s.S3Region, s.StateDB}
	return sha256.Sum256([]byte(strings.Join(append(fields, s.Embed.Environment()...), "\x00")))
}
```

(e) The `Supervisor` struct becomes:
```go
type Supervisor struct {
	lister   IdentityLister
	resolver CredentialResolver
	routes   RouteSource
	launcher Launcher
	options  Options
	active   map[string]managedProcess
	unbound  map[string]string
	// route is the last embedding route that resolved, routed whether one ever has, and
	// routeErr the failure last logged, so a failure that persists is logged once.
	route    EmbedRoute
	routed   bool
	routeErr string
}
```

(f) `New` becomes:
```go
// New constructs a supervisor over Aura's existing identity and object-store seams and the
// embedding route every child embeds with.
func New(
	lister IdentityLister, resolver CredentialResolver, routes RouteSource, launcher Launcher, options Options,
) *Supervisor {
```
The return line becomes:
```go
	return &Supervisor{
		lister: lister, resolver: resolver, routes: routes, launcher: launcher,
		options: options, active: make(map[string]managedProcess), unbound: make(map[string]string),
	}
```

(g) In `Run`, the dependency check becomes:
```go
	if s.lister == nil || s.resolver == nil || s.routes == nil || s.launcher == nil {
		return errors.New("ingest supervisor requires identity, credential, embedding route, and process dependencies")
	}
```

(h) In `Reconcile`, after `s.reapExited()`, insert:
```go
	route, routed := s.embedRoute(ctx)
	if !routed {
		return nil
	}
```
In the `desired[item.ID] = ProcessSpec{…}` literal, add `Embed: route,` after `StateDB: …,`.

(i) Add after `Reconcile`:
```go
// embedRoute is the route this tick's children embed with. A route that does not resolve
// keeps the last one that did: running children keep their specs, and an identity that
// starts meanwhile joins them. Before any route has resolved there is nothing to start.
// Each change of failure is logged once, as an unbound identity is.
func (s *Supervisor) embedRoute(ctx context.Context) (EmbedRoute, bool) {
	route, err := s.routes.Resolve(ctx)
	if err != nil {
		if s.routeErr != err.Error() {
			s.options.Logger.Warn("embedding route unresolved; running children keep theirs", "error", err)
			s.routeErr = err.Error()
		}
		return s.route, s.routed
	}
	if !s.routed || s.routeErr != "" || route.Space != s.route.Space {
		s.options.Logger.Info("embedding route resolved", "space", route.Space, "model", route.Model)
	}
	s.route, s.routed, s.routeErr = route, true, ""
	return route, true
}
```

- [ ] **Step 4: Wire the command**

Replace the `import` block and `run()` in `cmd/aura-ingest-supervisor/main.go` with:
```go
import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/envutil"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/ingestsupervisor"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/settings"
)

// routeTimeout bounds one route read's HTTP call -- the sidecar's /v1/models, a hosted
// catalogue -- well inside the supervisor's 15 s tick.
const routeTimeout = 10 * time.Second
```
```go
func run() error {
	printEnv := flag.Bool("print-embed-env", false,
		"print the embedding environment a child would receive, credential included, and exit")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, &db.Config{URL: strings.TrimSpace(os.Getenv("AURA_DB_URL"))})
	if err != nil {
		return fmt.Errorf("ingest supervisor database: %w", err)
	}
	defer pool.Close()
	store, err := settings.NewStore(pool, os.Getenv("AURA_AUTHULA_SECRET"))
	if err != nil {
		return fmt.Errorf("ingest supervisor settings: %w", err)
	}
	routes := &ingestsupervisor.RouteResolver{
		Store: store,
		// This process never overlays aura.settings onto its environment, so this is the
		// pre-overlay environment a deleted row must fall back to (spec §6).
		LookupEnv:  os.LookupEnv,
		Dimensions: envutil.IntDefault("AURA_EMBED_DIMENSIONS", config.DefaultEmbedDimensions),
		HTTP:       &http.Client{Timeout: routeTimeout},
	}
	if *printEnv {
		return printEmbedEnv(ctx, routes)
	}
	resolver, err := objectstore.NewIdentityStore(
		pool,
		os.Getenv("AURA_AUTHULA_SECRET"),
		objectstore.Credentials{
			Bucket:    os.Getenv("AURA_OBJECTSTORE_BUCKET"),
			AccessKey: os.Getenv("AURA_OBJECTSTORE_ACCESS_KEY"),
			SecretKey: os.Getenv("AURA_OBJECTSTORE_SECRET_KEY"),
		},
		identityctx.LocalOperatorIdentity,
	)
	if err != nil {
		return fmt.Errorf("ingest supervisor object store: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	supervisor := ingestsupervisor.New(
		identity.New(pool), resolver, routes, ingestsupervisor.NewExecLauncher(),
		ingestsupervisor.Options{
			PollInterval: pollInterval(os.Getenv("AURA_INGEST_SUPERVISOR_INTERVAL")),
			StateRoot:    os.Getenv("AURA_INGEST_STATE_ROOT"),
			S3Endpoint:   os.Getenv("AURA_OBJECTSTORE_ENDPOINT"),
			S3Region:     os.Getenv("AURA_OBJECTSTORE_REGION"),
			Logger:       logger,
		},
	)
	if err := supervisor.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// printEmbedEnv is how a script that runs `python -m ingest.app` directly hands its child the
// route a supervised child would get: this resolution, never a space the script made up.
func printEmbedEnv(ctx context.Context, routes *ingestsupervisor.RouteResolver) error {
	route, err := routes.Resolve(ctx)
	if err != nil {
		return err
	}
	for _, entry := range route.Environment() {
		fmt.Println(entry)
	}
	return nil
}
```

- [ ] **Step 5: Correct the compose comment**

In `compose.yaml`, in the `aura-ingest` service, replace:
```yaml
      AURA_EMBED_BASE_URL: http://aura-llama-embed:8081
      AURA_EMBED_DIMENSIONS: "768"
```
with:
```yaml
      # Fallbacks only: the supervisor resolves the embedding route from aura.settings on
      # every tick and overrides these in each child's environment
      # (internal/ingestsupervisor/route.go).
      AURA_EMBED_BASE_URL: http://aura-llama-embed:8081
      AURA_EMBED_DIMENSIONS: "768"
```

- [ ] **Step 6: Run the tests and build the command**

Run: `wsl -e bash -c 'export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && go test ./internal/ingestsupervisor/ -count=1 && go vet ./internal/ingestsupervisor/ ./cmd/aura-ingest-supervisor/ && go build -o /dev/null ./cmd/aura-ingest-supervisor/'`
Expected: `ok`, then no vet output and no build output.

- [ ] **Step 7: Commit**

```bash
gofmt -l internal/ingestsupervisor cmd/aura-ingest-supervisor
git add internal/ingestsupervisor/supervisor.go internal/ingestsupervisor/supervisor_test.go \
  cmd/aura-ingest-supervisor/main.go compose.yaml
git commit -m "feat(ingest): every child embeds with the resolved route and restarts when it moves

The route is part of each child's spec and fingerprint, so a route change -- a model, a
key, a swapped GGUF -- restarts every child within one poll. A route that cannot be read
keeps the children on the last one that resolved, and starts nothing before any has.
-print-embed-env lets scripts that run the Python app directly start it the same way.
The missing-dependency test now names the fourth dependency.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Embedding moves out of `app.py` and follows the route

**Files:**
- Create: `services/ingest/embed.py`
- Modify: `services/ingest/app.py` (imports, constants, lifespan, `process_chunk`, the card vector; lines 176-354 removed)
- Modify: `services/ingest/chunk.py:16-20,42`
- Modify: `Makefile` (`ingest-test`)
- Modify: `services/ingest/tests/test_embed_failure.py` (follows the move)
- Create: `services/ingest/tests/test_embed_route.py`
- Modify: `services/ingest/tests/test_chunk.py`
- Create (git-ignored workspace): `.superpowers/sdd/2026-09-24-embedding-ingest/ingest-py.sh`

**Interfaces:**
- Consumes: the seven-variable environment (Global Constraints).
- Produces (module `ingest.embed`):
  - constants `BASE_URL: str`, `DIMENSIONS: int`, `MODEL: str`, `SPACE: str`, `INPUT_LIMIT: int`, `EMBED_MAX_BATCH`, `EMBED_REQUEST_TOKEN_BUDGET`, `EMBED_SPECIAL_TOKENS`;
  - `class EmbedRequestError(RuntimeError)`, with `.status: int | None` and `.rejects_input: bool`;
  - `embed_text`, the CocoIndex async batched function (one text → one vector);
  - the private helpers the tests call: `_embed_texts(texts) -> list[list[float]]`, `_embed_batch(texts)`, `_fit(text) -> (str, int)`, `_fit_for_embedding`, `_head_within_ceiling`, `_request_end`, `_embed_failure_detail`.

- [ ] **Step 1: Write the Python test runner and build the image**

Create `.superpowers/sdd/2026-09-24-embedding-ingest/ingest-py.sh`:
```bash
#!/usr/bin/env bash
# The ingest suite in the ingest image, the way `make ingest-test` runs it, for one selector.
# Git Bash, from the repo root: bash .superpowers/sdd/2026-09-24-embedding-ingest/ingest-py.sh ingest/tests/test_x.py
set -euo pipefail
export MSYS_NO_PATHCONV=1
repo="$(cd "$(dirname "$0")/../../.." && pwd -W)"
pw="$(sed -n 's/^ARCADEDB_PASSWORD=//p' "$repo/.env" | tr -d '\r' | tr -d '"')"
docker run --rm --network aura_default --entrypoint sh \
  -v "$repo/services/ingest:/app/ingest:ro" \
  -v "$repo/scripts/fixtures/document_pipeline_e2e:/fx:ro" \
  -e ARCADEDB_PASSWORD="$pw" -e ARCADE_HTTP=http://arcadedb:2480 -e ARCADE_BOLT=bolt://arcadedb:7687 \
  -e AURA_EMBED_BASE_URL=http://aura-llama-embed:8081 \
  -e AURA_EMBED_TOKENIZER_URL=http://aura-llama-embed:8081 \
  -e AURA_EMBED_SPACE=es1-ingest-test \
  -e AURA_INGEST_IDENTITY_ID=00000000-0000-0000-0000-000000000001 \
  -e AURA_INGEST_S3_BUCKET=aura-ingest-test \
  -e AURA_INGEST_S3_ACCESS_KEY_ID=test -e AURA_INGEST_S3_SECRET_ACCESS_KEY=test \
  aura-ingest:local -c "pip install --quiet pytest && cd /app && python -m pytest ${1:-ingest/tests} -q"
```
Then, from Git Bash at the repo root:
```bash
docker compose up -d arcadedb aura-llama-embed
docker build -f docker/aura-ingest/Dockerfile -t aura-ingest:local .
bash .superpowers/sdd/2026-09-24-embedding-ingest/ingest-py.sh
```
Expected: the build succeeds, and the current suite passes on the current code before anything changes. This is the baseline; record its pass count in the ledger. If `aura-llama-embed` cannot start, the chunk tests fall back to the character estimate by design; record that in the ledger.

- [ ] **Step 2: Write the failing tests**

Create `services/ingest/tests/test_embed_route.py`:
```python
"""The route, the width and the space, as the supervisor hands them over.

internal/ingestsupervisor/route.go resolves the route and this process only obeys it. Each
test sets the environment the supervisor would and reloads the module, which reads it once at
import -- the lifetime a route has in a real child.
"""

import asyncio
import importlib
import io
import json
import os
import subprocess
import sys
import urllib.error

import cocoindex as coco
import pytest

from ingest import chunk, embed

HOSTED = {
    "AURA_EMBED_BASE_URL": "https://openrouter.ai/api",
    "AURA_EMBED_MODEL": "vendor/embed",
    "AURA_EMBED_API_KEY": "sk-test-key",
    "AURA_EMBED_INPUT_LIMIT": "64",
    "AURA_EMBED_DIMENSIONS": "4",
    "AURA_EMBED_SPACE": "es1-hosted0000000000",
}


@pytest.fixture(autouse=True)
def keep_the_tokenizer_state(monkeypatch):
    """A failure path asks the tokenizer, and an unreachable one is sticky for the process."""
    monkeypatch.setattr(chunk, "_server_reachable", chunk._server_reachable)


@pytest.fixture
def hosted(monkeypatch):
    for key, value in HOSTED.items():
        monkeypatch.setenv(key, value)
    yield importlib.reload(embed)
    monkeypatch.undo()
    importlib.reload(embed)


def _answer(vectors):
    return io.BytesIO(json.dumps(
        {"data": [{"index": i, "embedding": v} for i, v in enumerate(vectors)]}
    ).encode())


def _http_error(code):
    return urllib.error.HTTPError(
        url="http://embed/v1/embeddings", code=code, msg="refused", hdrs=None, fp=io.BytesIO(b"refused"),
    )


def _embeddings_only(answer):
    """Route /v1/embeddings to `answer`; the tokenizer is unreachable in these tests."""
    def urlopen(req, *args, **kwargs):
        if not req.full_url.endswith("/v1/embeddings"):
            raise urllib.error.URLError("no tokenizer in this test")
        return answer(req)
    return urlopen


def test_a_hosted_request_names_the_model_the_width_and_the_key(hosted, monkeypatch):
    sent = []

    def answer(req):
        sent.append(req)
        return _answer([[0.5, 0.5, 0.5, 0.5]])

    monkeypatch.setattr(hosted.urllib.request, "urlopen", _embeddings_only(answer))

    hosted._embed_batch(["ciao"])

    body = json.loads(sent[0].data)
    assert sent[0].full_url == "https://openrouter.ai/api/v1/embeddings"
    assert body["model"] == "vendor/embed" and body["dimensions"] == 4
    assert body["input"] == [chunk.EMBED_DOC_PREFIX + "ciao"]
    assert sent[0].get_header("Authorization") == "Bearer sk-test-key"


def test_the_local_route_sends_neither_a_key_nor_a_width(monkeypatch):
    sent = []

    def answer(req):
        sent.append(req)
        return _answer([[0.0] * embed.DIMENSIONS])

    monkeypatch.setattr(embed.urllib.request, "urlopen", _embeddings_only(answer))

    embed._embed_batch(["ciao"])

    assert "dimensions" not in json.loads(sent[0].data)
    assert sent[0].get_header("Authorization") is None


def test_a_wider_vector_is_cut_to_the_width_and_renormalised(hosted, monkeypatch):
    monkeypatch.setattr(hosted.urllib.request, "urlopen",
                        _embeddings_only(lambda _req: _answer([[3.0, 4.0, 0.0, 0.0, 9.0, 9.0]])))

    [vector] = hosted._embed_batch(["ciao"])

    assert vector == pytest.approx([0.6, 0.8, 0.0, 0.0])


def test_a_narrower_vector_is_refused_rather_than_stored(hosted, monkeypatch):
    monkeypatch.setattr(hosted.urllib.request, "urlopen", _embeddings_only(lambda _req: _answer([[1.0, 0.0]])))

    with pytest.raises(hosted.EmbedRequestError, match="dimension 2, want 4"):
        hosted._embed_batch(["ciao"])


def test_a_hosted_input_is_cut_to_the_published_limit_in_bytes(hosted):
    # Two bytes each, so a cut in the middle of one would not decode.
    fitted, _ = hosted._fit("è" * 100)

    sent = (chunk.EMBED_DOC_PREFIX + fitted).encode("utf-8")
    assert len(sent) + hosted.EMBED_SPECIAL_TOKENS <= int(HOSTED["AURA_EMBED_INPUT_LIMIT"])
    assert fitted and set(fitted) == {"è"}


def test_a_hosted_input_that_fits_is_sent_unchanged(hosted):
    assert hosted._fit("ciao")[0] == "ciao"


def test_the_key_never_reaches_a_child_process(hosted):
    assert "AURA_EMBED_API_KEY" not in os.environ
    child = subprocess.run(
        [sys.executable, "-c", "import os; print(os.environ.get('AURA_EMBED_API_KEY', ''))"],
        capture_output=True, text=True, check=True,
    )
    assert child.stdout.strip() == ""


def _import_embed(changes):
    env = dict(os.environ)
    for key, value in changes.items():
        if value is None:
            env.pop(key, None)
        else:
            env[key] = value
    return subprocess.run([sys.executable, "-c", "import ingest.embed"], env=env, capture_output=True, text=True)


def test_a_child_without_a_space_refuses_to_start():
    done = _import_embed({"AURA_EMBED_SPACE": None})

    assert done.returncode != 0 and "AURA_EMBED_SPACE is required" in done.stderr


def test_a_hosted_child_without_a_key_refuses_to_start():
    done = _import_embed({"AURA_EMBED_MODEL": "vendor/embed", "AURA_EMBED_API_KEY": None,
                          "AURA_EMBED_INPUT_LIMIT": "8192"})

    assert done.returncode != 0 and "AURA_EMBED_API_KEY is empty" in done.stderr


def test_a_refused_input_asks_cocoindex_to_split_the_batch(monkeypatch):
    def refuse(_req):
        raise _http_error(400)

    monkeypatch.setattr(embed.urllib.request, "urlopen", _embeddings_only(refuse))

    with pytest.raises(coco.RetryWithSmallerBatch) as caught:
        embed._embed_texts(["uno", "due"])

    assert isinstance(caught.value.__cause__, embed.EmbedRequestError)
    assert caught.value.__cause__.status == 400


@pytest.mark.parametrize("code", [401, 403, 429, 500, 503])
def test_a_route_failure_fails_the_batch_without_splitting_it(monkeypatch, code):
    def fail(_req):
        raise _http_error(code)

    monkeypatch.setattr(embed.urllib.request, "urlopen", _embeddings_only(fail))

    with pytest.raises(embed.EmbedRequestError) as caught:
        embed._embed_texts(["uno"])

    assert caught.value.status == code


def test_one_refused_input_fails_only_its_own_caller(monkeypatch):
    def answer(req):
        inputs = json.loads(req.data)["input"]
        if any("RIFIUTATO" in text for text in inputs):
            raise _http_error(400)
        return _answer([[0.0] * embed.DIMENSIONS for _ in inputs])

    monkeypatch.setattr(embed.urllib.request, "urlopen", _embeddings_only(answer))

    async def embed_all():
        texts = ["uno", "RIFIUTATO", "tre", "quattro"]
        return await asyncio.gather(*(embed.embed_text(text) for text in texts), return_exceptions=True)

    results = asyncio.run(embed_all())

    assert isinstance(results[1], embed.EmbedRequestError)
    assert all(isinstance(result, list) for index, result in enumerate(results) if index != 1)
```

Append to `services/ingest/tests/test_chunk.py`:
```python
def test_tokens_are_counted_by_the_local_sidecar_whatever_route_embeds(monkeypatch):
    """Chunk boundaries follow the tokenizer, so they must not follow the embedding route: a
    model change would otherwise re-chunk every document (audit F9)."""
    import io
    import json

    from ingest import chunk as chunk_module

    seen = []

    def urlopen(req, *_args, **_kwargs):
        seen.append(req.full_url)
        return io.BytesIO(json.dumps({"tokens": [1, 2, 3]}).encode())

    monkeypatch.setenv("AURA_EMBED_TOKENIZER_URL", "http://tokenizer.test:8081")
    monkeypatch.setenv("AURA_EMBED_BASE_URL", "https://openrouter.ai/api")
    monkeypatch.setattr(chunk_module, "_server_reachable", None)
    monkeypatch.setattr(chunk_module.urllib.request, "urlopen", urlopen)

    assert count_tokens("ciao") == 3
    assert seen == ["http://tokenizer.test:8081/tokenize"]
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `bash .superpowers/sdd/2026-09-24-embedding-ingest/ingest-py.sh "ingest/tests/test_embed_route.py ingest/tests/test_chunk.py"`
Expected: `test_embed_route.py` fails at collection with `ImportError: cannot import name 'embed' from 'ingest'`. The new `test_chunk.py` test fails with `seen == ['http://aura-llama-embed:8081/tokenize']`, because the base URL variable is read instead of the tokenizer one.

- [ ] **Step 4: Write `embed.py`**

Create `services/ingest/embed.py`. The helpers carried from `app.py` (`_estimated_tokens`, `_fit_for_embedding`, `_request_end`, `_head_within_ceiling`, `_embed_failure_detail`) keep their bodies and docstrings verbatim. `_embed_batch` changes only where marked.
```python
"""The documents family's embedding: one route and one space per process (spec §6).

The supervisor resolves the route from aura.settings on every tick and restarts this
process when it moves (internal/ingestsupervisor/route.go). Nothing here derives a route or
a space: the environment names both, and every vector this module returns was produced in
SPACE, which app.py stamps beside it.
"""

import json
import math
import os
import urllib.error
import urllib.request

import cocoindex as coco

from ingest import chunk

BASE_URL = os.environ.get("AURA_EMBED_BASE_URL", "http://aura-llama-embed:8081")
DIMENSIONS = int(os.environ.get("AURA_EMBED_DIMENSIONS", "768"))
# Set only on a hosted route, and that is what makes a route hosted: Go's
# config.EmbedRouteKind reads the same field.
MODEL = os.environ.get("AURA_EMBED_MODEL", "").strip()
SPACE = os.environ.get("AURA_EMBED_SPACE", "").strip()
# Popped, not read: Tika, LibreOffice, aura-filecard and aura-media-index are children of this
# process, and none of them has any business holding a provider's key.
_API_KEY = os.environ.pop("AURA_EMBED_API_KEY", "").strip()
INPUT_LIMIT = int(os.environ.get("AURA_EMBED_INPUT_LIMIT") or "0")

# One request carries several chunks, bounded by BOTH a count and a token budget -- the two
# bounds internal/embeddings/fit.go:22-25 already applies, for the reason measured there: a
# 2048-token input took 5.7 s on the appliance sidecar, so 32 of them behind one deadline is
# a timeout, not a speed-up. A single input over the budget still goes alone.
EMBED_MAX_BATCH = 32
EMBED_REQUEST_TOKEN_BUDGET = 4096
# What the server prepends and appends to every input. chunk.count_tokens(add_special=True)
# measures it properly; this is the same two tokens, added to an estimate that never asks.
EMBED_SPECIAL_TOKENS = 2
# The document prefix travels inside every input, so a hosted byte budget pays for it too.
_PREFIX_BYTES = len(chunk.EMBED_DOC_PREFIX.encode("utf-8"))

# A vector without the space that produced it is the defect the stamp exists to end, so a
# process started without one embeds nothing rather than write rows nobody can place.
if not SPACE:
    raise RuntimeError("AURA_EMBED_SPACE is required: every stored vector is stamped with its space")
if MODEL and not _API_KEY:
    raise RuntimeError(f"AURA_EMBED_MODEL={MODEL} is a hosted route, and AURA_EMBED_API_KEY is empty")
if MODEL and INPUT_LIMIT <= EMBED_SPECIAL_TOKENS + _PREFIX_BYTES:
    raise RuntimeError(f"AURA_EMBED_INPUT_LIMIT={INPUT_LIMIT} leaves no room for a hosted input")


class EmbedRequestError(RuntimeError):
    """An embedding request that failed, with the HTTP status when there was one."""

    def __init__(self, message: str, status: int | None = None):
        super().__init__(message)
        self.status = status

    @property
    def rejects_input(self) -> bool:
        # Only these say something about a text (Go's embeddings.RejectsInput): 401, 403 and
        # 429 are about the route or the account, and a 5xx is about the server.
        return self.status in (400, 413, 422)


# (_estimated_tokens, _fit_for_embedding and _request_end: moved verbatim from app.py.)


def _fit(text: str) -> tuple[str, int]:
    """What to send for one chunk, before its prefix, and what it is expected to cost."""
    return _fit_hosted(text) if MODEL else _fit_for_embedding(text)


def _fit_hosted(text: str) -> tuple[str, int]:
    """Cut to a hosted model's published limit in bytes, internal/embeddings fitInput's rule.

    There is no tokenizer for a hosted model here, and no token was ever shorter than one
    UTF-8 byte, so prefix + text within INPUT_LIMIT - 2 bytes always fits. It drops text a
    truncating provider would have kept; one that refuses instead (qwen3-embedding-8b answers
    400) would otherwise refuse the whole file.
    """
    budget = INPUT_LIMIT - EMBED_SPECIAL_TOKENS - _PREFIX_BYTES
    encoded = text.encode("utf-8")
    if len(encoded) > budget:
        text = encoded[:budget].decode("utf-8", "ignore")
    return text, _estimated_tokens(text)


def _embed_texts(texts: list[str]) -> list[list[float]]:
    """Embed a batch of chunks, one HTTP request per group rather than per chunk.

    CocoIndex groups concurrent calls itself (batching=True), so the call sites still pass
    ONE text and await ONE vector. Measured 2026-09-21 from the appliance: against the local
    sidecar this is 1.0x -- it is compute-bound at one slot -- but against a cloud embedder
    it is 25.9x (9.95 s -> 0.38 s for 32 chunks), because there each chunk was a network
    round trip. The token count, and therefore the bill, is identical either way.

    A request the provider refuses as input goes back to CocoIndex to split
    (RetryWithSmallerBatch). Batching groups chunks across files, so a plain raise failed
    every file sharing the request with one bad input (audit F1). Only 400, 413 and 422
    split: any other failure fails the same way at any size, and CocoIndex's own contract
    says not to split on it (cocoindex/_internal/batching.py).
    """
    fitted = [_fit(text) for text in texts]
    costs = [cost for _, cost in fitted]
    out: list[list[float]] = []
    start = 0
    try:
        while start < len(fitted):
            end = _request_end(costs, start)
            out.extend(_embed_batch([text for text, _ in fitted[start:end]]))
            start = end
    except EmbedRequestError as err:
        if err.rejects_input:
            raise coco.RetryWithSmallerBatch() from err
        raise
    return out


# deps is the space. A route change restarts this process with another one, the logic
# fingerprint moves, and CocoIndex re-runs every file that embedded under the old space.
# deps is snapshotted at import, which is exactly the lifetime of a space here.
embed_text = coco.fn.as_async(
    memo=True, batching=True, max_batch_size=EMBED_MAX_BATCH, deps=SPACE,
)(_embed_texts)


def _endpoint(base: str) -> str:
    """Where /v1/embeddings is, by internal/embeddings endpoint()'s rule."""
    base = base.strip().rstrip("/")
    if base.endswith("/embeddings"):
        return base
    if base.endswith("/v1"):
        return base + "/embeddings"
    return base + "/v1/embeddings"


def _to_width(vector: list[float]) -> list[float]:
    """The vector at DIMENSIONS, by internal/embeddings TruncateMRL's rule.

    Wider keeps the leading components and renormalises, which is valid only for a
    Matryoshka-trained model (the cockpit's preview warns about it). Narrower cannot fill the
    index at all, and nothing is stored rather than a vector ArcadeDB would refuse.
    """
    if len(vector) == DIMENSIONS:
        return vector
    if len(vector) < DIMENSIONS:
        raise EmbedRequestError(f"embedding has dimension {len(vector)}, want {DIMENSIONS}")
    head = vector[:DIMENSIONS]
    norm = math.sqrt(sum(value * value for value in head))
    if norm == 0 or not math.isfinite(norm):
        raise EmbedRequestError(f"embedding truncated to {DIMENSIONS} invalid components")
    return [value / norm for value in head]


def _embed_batch(texts: list[str]) -> list[list[float]]:
    """(docstring moved verbatim from app.py)"""
    # (the "Whether batching is actually happening" comment and print, verbatim)
    print(f"[embed] {len(texts)} chunk(s) in one request", flush=True)
    body = {"input": [chunk.EMBED_DOC_PREFIX + text for text in texts], "model": MODEL or "embeddinggemma"}
    headers = {"Content-Type": "application/json"}
    if MODEL:
        # Only a hosted route truncates server-side; llama.cpp ignores the field, and
        # _to_width narrows whatever comes back either way.
        body["dimensions"] = DIMENSIONS
        headers["Authorization"] = f"Bearer {_API_KEY}"
    req = urllib.request.Request(_endpoint(BASE_URL), data=json.dumps(body).encode(), headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            data = json.loads(resp.read())["data"]
    except urllib.error.HTTPError as exc:
        # The longest input is the one a size-related failure is about.
        raise EmbedRequestError(_embed_failure_detail(exc, max(texts, key=len)), exc.code) from exc
    if len(data) != len(texts):
        raise EmbedRequestError(f"embedding endpoint returned {len(data)} vectors for {len(texts)} inputs")
    out: list[list[float] | None] = [None] * len(texts)
    for item in data:
        index = item.get("index")
        if not isinstance(index, int) or not 0 <= index < len(out):
            raise EmbedRequestError(f"embedding response carries an unusable index {index!r}")
        if out[index] is not None:
            raise EmbedRequestError(f"embedding response repeats index {index}")
        out[index] = _to_width(item["embedding"])
    missing = [i for i, vector in enumerate(out) if vector is None]
    if missing:
        raise EmbedRequestError(f"embedding response is missing indexes {missing}")
    return out


# (_head_within_ceiling and _embed_failure_detail: moved verbatim from app.py.)
```
Replace each `(… moved verbatim …)` placeholder line with the real function copied from `app.py` (lines 187-239 and 305-354 there), unchanged.

- [ ] **Step 5: Point `app.py` at `embed.py`**

In `services/ingest/app.py`:
1. Change the imports:
   - delete `import json`, `import math`, `import urllib.error` and `import urllib.request`;
   - the ingest import line becomes `from ingest import arcade, chunk, embed, extract, identity, media, outline, source`.
2. Delete the two lines `EMBED_BASE_URL = …` and `EMBED_DIMENSIONS = …`, then:
   - `SCHEMA_VERSION = arcade.schema_version(EMBED_DIMENSIONS)` becomes `SCHEMA_VERSION = arcade.schema_version(embed.DIMENSIONS)`;
   - in `coco_lifespan`, `EMBED_DIMENSIONS` becomes `embed.DIMENSIONS`.
3. Delete everything from the comment `# One request carries several chunks, bounded by BOTH…` (line 176) through the end of `_embed_failure_detail` (line 354).
4. In `process_chunk`, `embedding=await _embed(piece.text),` becomes `embedding=await embed.embed_text(piece.text),`.
5. In `process_file`, `embedding=await _embed(card) if card.strip() else [0.0] * EMBED_DIMENSIONS,` becomes `embedding=await embed.embed_text(card) if card.strip() else [0.0] * embed.DIMENSIONS,`.

In `services/ingest/chunk.py`, line 42 `_EMBED_BASE_URL_ENV = "AURA_EMBED_BASE_URL"` becomes `_TOKENIZER_URL_ENV = "AURA_EMBED_TOKENIZER_URL"`, and in `_tokenize_remote`, `os.environ.get(_EMBED_BASE_URL_ENV, …)` becomes `os.environ.get(_TOKENIZER_URL_ENV, …)`. In the module docstring, replace:
```
count_tokens counts with the embedding server's OWN tokenizer (POST /tokenize,
base URL from AURA_EMBED_BASE_URL) whenever reachable -- that is the tokenizer
the 2048 ceiling actually belongs to.
```
with:
```
count_tokens counts with the local sidecar's OWN tokenizer (POST /tokenize, base
URL from AURA_EMBED_TOKENIZER_URL, which the supervisor points at the local sidecar
whichever route embeds) whenever reachable -- the tokenizer the 2048 ceiling belongs
to, and the one chunk boundaries must keep following when the embedding model moves.
```

In `services/ingest/tests/test_embed_failure.py`, which follows the moved code (its assertions are unchanged; only the module it reaches and the width of its fake vectors change):
- `from ingest import app, chunk` becomes `from ingest import chunk, embed`;
- `app._embed(text)` becomes `embed.embed_text(text)`;
- every other `app.` becomes `embed.`;
- in `test_an_oversized_chunk_does_not_take_its_batch_mates_down_with_it`, the fake's `[0.5, 0.5]` becomes `[0.5] * embed.DIMENSIONS`, and the assertion becomes `assert vectors == [[0.5] * embed.DIMENSIONS] * 2, "the healthy chunk was lost with the oversized one"`;
- in `test_vectors_are_placed_by_the_response_index_not_by_arrival`, `[float(i)]` becomes `[float(i)] * embed.DIMENSIONS`, and the assertion becomes `assert embed._embed_batch(["a", "b", "c"]) == [[float(i)] * embed.DIMENSIONS for i in range(3)]`.

The width change is the new contract: spec §6 requires every returned width to be validated, and a 1-wide vector is now correctly refused.

In `Makefile`, `ingest-test`, after `-e AURA_EMBED_BASE_URL=http://aura-llama-embed:8081 \` add:
```make
	  -e AURA_EMBED_TOKENIZER_URL=http://aura-llama-embed:8081 \
	  -e AURA_EMBED_SPACE=es1-ingest-test \
```

- [ ] **Step 6: Run the whole suite to verify it passes**

Run: `bash .superpowers/sdd/2026-09-24-embedding-ingest/ingest-py.sh`
Expected: every test passes, the Step 1 baseline plus 17 new cases (12 functions in `test_embed_route.py`, one of them parametrized five ways, and the chunk test). Then `wc -l services/ingest/app.py services/ingest/embed.py`: both are below 600.

- [ ] **Step 7: Commit**

```bash
git add services/ingest/embed.py services/ingest/app.py services/ingest/chunk.py Makefile \
  services/ingest/tests/test_embed_failure.py services/ingest/tests/test_embed_route.py \
  services/ingest/tests/test_chunk.py
git commit -m "feat(ingest): the worker embeds with the supervisor's route and names its space

Embedding moves out of app.py, which was over the 600-line cap, into embed.py. The module
takes the route the supervisor resolved: a hosted route sends the model, the key and the
width, inputs are cut to the published limit in bytes, every returned width is checked,
and the key is removed from the environment before any extractor runs. The space is the
batched function's dependency, so a route change re-runs every file, and a refused input
is split out instead of failing every file it shared a request with. Chunks are counted
with the local sidecar's tokenizer whichever route embeds. test_embed_failure.py follows
the moved code; its fake vectors are now full width, since a 1-wide vector is refused.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Every document row is stamped

**Files:**
- Modify: `services/ingest/arcade.py` (`_document_ddl`, new `_space_stamp_ddl`)
- Modify: `services/ingest/app.py` (`Passage`, `IndexedDocument`, `process_chunk`, the card row)
- Create: `services/ingest/tests/test_embed_space_stamp.py`
- Modify: `services/ingest/tests/test_arcade_integration.py`

**Interfaces:**
- Consumes: `embed.SPACE`, `embed.embed_text`, `embed.DIMENSIONS` (Task 4).
- Produces: the `embed_space: str` field on `app.Passage` and `app.IndexedDocument`; `arcade._space_stamp_ddl(type_name) -> list[str]`.

- [ ] **Step 1: Write the failing tests**

Create `services/ingest/tests/test_embed_space_stamp.py`:
```python
"""Every document vector carries the space that produced it (spec §2).

The documents gate compares these stamps with the reader's space, so a row without one, or
with an index that cannot see a missing one, would let dense retrieval open over vectors
nobody checked.
"""

import dataclasses

from ingest import app, arcade


def test_both_document_types_declare_the_stamp_and_index_its_nulls():
    ddl = arcade._document_ddl(768)

    for type_name in (arcade.PASSAGE_TYPE, arcade.DOCUMENT_TYPE):
        assert f"CREATE PROPERTY {type_name}.embed_space IF NOT EXISTS STRING" in ddl
        assert f"CREATE INDEX IF NOT EXISTS ON {type_name} (embed_space) NOTUNIQUE NULL_STRATEGY INDEX" in ddl


def test_every_declared_row_carries_a_stamp():
    for record in (app.Passage, app.IndexedDocument):
        assert "embed_space" in {field.name for field in dataclasses.fields(record)}
```

Append to `services/ingest/tests/test_arcade_integration.py`:
```python
def test_rows_written_before_the_stamp_are_counted_through_its_index(disposable_database):
    """The upgrade adds the stamp to a database whose rows have none, and the documents gate
    must count every one of them. With the index's default null strategy it would count none
    (arcadedb-docs reference/sql/sql-indexes.adoc), so this builds the index over rows that
    already exist, the way the upgrade does."""
    ensure_schema(ARCADE_HTTP, disposable_database, AUTH, DIMS)
    _command(disposable_database, "DROP PROPERTY Passage.embed_space FORCE")
    _command(disposable_database, "INSERT INTO Passage SET passage_key = 'before', text = 'x'")

    ensure_schema(ARCADE_HTTP, disposable_database, AUTH, DIMS)
    _command(disposable_database,
             "INSERT INTO Passage SET passage_key = 'after', text = 'y', embed_space = 'es1-aaaaaaaaaaaaaaaa'")

    unstamped = _query(disposable_database, "SELECT count(*) AS n FROM Passage WHERE embed_space IS NULL", {})
    stamped = _query(disposable_database, "SELECT count(*) AS n FROM Passage WHERE embed_space = :s",
                     {"s": "es1-aaaaaaaaaaaaaaaa"})
    assert unstamped[0]["n"] == 1, "the stamp index hides the rows written before it"
    assert stamped[0]["n"] == 1
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `bash .superpowers/sdd/2026-09-24-embedding-ingest/ingest-py.sh "ingest/tests/test_embed_space_stamp.py ingest/tests/test_arcade_integration.py"`
Expected: both stamp tests fail (the DDL line and the `embed_space` field are absent). The integration test fails at `DROP PROPERTY Passage.embed_space FORCE` with `ArcadeSchemaError` naming a missing property.

- [ ] **Step 3: Declare and write the stamp**

In `services/ingest/arcade.py`, add after `schema_version`:
```python
def _space_stamp_ddl(type_name: str) -> list[str]:
    """The space a row's vector was produced in (spec §2), declared beside that vector.

    NULL_STRATEGY INDEX is load-bearing, exactly as in internal/arcadedb/embedding_space.go:
    with ArcadeDB's default, SKIP, "queries against null values that use an index return no
    entries" (arcadedb-docs reference/sql/sql-indexes.adoc), so every row written before this
    column existed -- after the upgrade, all of them -- would be invisible to the gate that
    has to count it.
    """
    return [
        f"CREATE PROPERTY {type_name}.embed_space IF NOT EXISTS STRING",
        f"CREATE INDEX IF NOT EXISTS ON {type_name} (embed_space) NOTUNIQUE NULL_STRATEGY INDEX",
    ]
```
In `_document_ddl`:
- after the `Passage` `LSM_VECTOR` index statement (the one ending `"quantization": "NONE" }}',` before the `# One record per object` comment), add `*_space_stamp_ddl(t),`;
- after the last statement (the `IndexedDocument` `LSM_VECTOR` index), add `*_space_stamp_ddl(DOCUMENT_TYPE),`.

In `services/ingest/app.py`, add after `embedding: list[float]` in `IndexedDocument`:
```python
    # The space `embedding` was produced in (spec §2). The documents gate compares it with the
    # reader's, so a vector from another model is never ranked.
    embed_space: str
```
and after `embedding: list[float]` in `Passage`:
```python
    embed_space: str
```
In `process_chunk`, after `embedding=await embed.embed_text(piece.text),` add `embed_space=embed.SPACE,`. In the `IndexedDocument(...)` row, after the `embedding=…` line, add `embed_space=embed.SPACE,`.

The empty card's zero vector is stamped too: it is out of scope (spec, Out of scope), and an unstamped row would hold the gate closed forever.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `bash .superpowers/sdd/2026-09-24-embedding-ingest/ingest-py.sh`
Expected: the whole suite passes, with the three new tests included.

- [ ] **Step 5: Commit**

```bash
git add services/ingest/arcade.py services/ingest/app.py \
  services/ingest/tests/test_embed_space_stamp.py services/ingest/tests/test_arcade_integration.py
git commit -m "feat(ingest): stamp every passage and document vector with its space

Both document types gain embed_space, indexed so that a missing stamp is visible to a
count. The integration test builds the index over rows that already exist, which is the
upgrade's real shape: every document written before this commit is unstamped until
CocoIndex re-runs it.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Extraction is memoized by content, proven by a re-run test

**Files:**
- Modify: `services/ingest/app.py` (`_card`, `process_file` → `Extracted`, `_extract`, `process_file`, `index_object`; `app_main` → `mount_targets`)
- Create: `services/ingest/tests/fake_embedder.py`
- Create: `services/ingest/tests/space_driver.py`
- Create: `services/ingest/tests/test_embed_space_reruns.py`

**Interfaces:**
- Consumes: `embed.embed_text`, `embed.SPACE`, `embed.DIMENSIONS`, and the stamps (Tasks 4-5).
- Produces (module `ingest.app`):
  - `class Extracted(text: str, card: str, anchors: list[outline.Anchor])`;
  - `_extract(content: bytes, suffix: str, file_name: str, content_type: str | None) -> Extracted`, memoized;
  - `async index_object(identity_id, key, file_name, content, extracted, table, documents) -> None`;
  - `async mount_targets() -> tuple[TableTarget[Passage], TableTarget[IndexedDocument]]`.

- [ ] **Step 1: Write the fake embedder and the driver**

Create `services/ingest/tests/fake_embedder.py`:
```python
"""A stand-in embedding server for the re-run test: deterministic, countable, and able to
refuse one input in one space.

A vector depends on the space as well as on the input, so a row whose stamp and vector
disagree fails the test's comparison.
"""

import hashlib
import io
import json
import math
import os
import urllib.error
import urllib.request

REFUSED = "REFUSED-IN-THIS-SPACE"

_requests = 0
_real_urlopen = urllib.request.urlopen


def vector(space: str, sent: str, dimensions: int) -> list[float]:
    digest = hashlib.sha256(f"{space}\x00{sent}".encode("utf-8")).digest()
    raw = [float(digest[i % len(digest)]) + 1.0 for i in range(dimensions)]
    norm = math.sqrt(sum(value * value for value in raw))
    return [value / norm for value in raw]


def requests() -> int:
    return _requests


def install() -> None:
    """Answer /v1/embeddings in this process; the tokenizer still goes to the real sidecar."""
    from ingest import embed

    def urlopen(req, *args, **kwargs):
        global _requests
        if not req.full_url.endswith("/v1/embeddings"):
            return _real_urlopen(req, *args, **kwargs)
        _requests += 1
        inputs = json.loads(req.data)["input"]
        if os.environ.get("SPACE_THAT_REFUSES") == embed.SPACE and any(REFUSED in text for text in inputs):
            raise urllib.error.HTTPError(req.full_url, 400, "Bad Request", None, io.BytesIO(b"refused"))
        data = [{"index": i, "embedding": vector(embed.SPACE, text, embed.DIMENSIONS)}
                for i, text in enumerate(inputs)]
        return io.BytesIO(json.dumps({"data": data}).encode())

    urllib.request.urlopen = urlopen
```

Create `services/ingest/tests/space_driver.py`:
```python
"""One catch-up pass of the production indexing path, for test_embed_space_reruns.py.

    python -m ingest.tests.space_driver <database> <name>=<text> ...

Production reads each object from Garage; here the objects come from argv, and that is the
only part replaced. _extract, chunking, embed.embed_text with the space as its dependency,
and the stamped rows the ArcadeDB target writes are production code. The embedding server is
fake_embedder, so the test can count requests and refuse one input on purpose.
"""

import sys

import cocoindex as coco
from cocoindex.connectors import neo4j

from ingest import app, arcade, embed
from ingest.tests import fake_embedder

_database = ""


@coco.lifespan
async def _lifespan(builder: coco.EnvironmentBuilder):
    # Replaces app.py's lifespan (CocoIndex keeps the last one registered): same target, a
    # disposable database, and no S3 client, since nothing here reads the bucket.
    builder.provide(app.KG_DB, neo4j.ConnectionFactory(
        uri=app.ARCADE_BOLT, auth=("root", app.ARCADE_PASSWORD), database=_database))
    yield


@coco.fn(memo=True)
async def process_object(
    item: tuple[str, str], table: neo4j.TableTarget[app.Passage],
    documents: neo4j.TableTarget[app.IndexedDocument],
) -> None:
    name, text = item
    content = text.encode("utf-8")
    extracted = app._extract(content, ".txt", name, "text/plain")
    await app.index_object(app._S3_CONFIG.identity_id, name, name, content, extracted, table, documents)


@coco.fn
async def driver_main(objects: dict[str, str]) -> None:
    table, documents = await app.mount_targets()
    await coco.mount_each(
        process_object, [(name, (name, text)) for name, text in objects.items()], table, documents,
    )


if __name__ == "__main__":
    _database = sys.argv[1]
    objects = dict(arg.split("=", 1) for arg in sys.argv[2:])
    fake_embedder.install()
    arcade.ensure_schema(app.ARCADE_HTTP, _database, ("root", app.ARCADE_PASSWORD), embed.DIMENSIONS)
    coco.App(coco.AppConfig(name="space-driver"), driver_main, objects).update_blocking()
    print(f"REQUESTS {fake_embedder.requests()}", flush=True)
```

- [ ] **Step 2: Write the failing test**

Create `services/ingest/tests/test_embed_space_reruns.py`:
```python
"""A route change re-embeds every document once, re-extracts none, and never mislabels a row.

Spec §6 measured end to end on the production indexing path (space_driver.py) against a real
ArcadeDB: four catch-up passes over one CocoIndex state.

1. Space A: both files are extracted, embedded and stamped A.
2. Space A again: nothing runs, because the memo holds.
3. Space B, which refuses one file's text:
   - nothing is re-extracted, because _extract is memoized by content, apart from embedding;
   - the healthy file is re-embedded and stamped B;
   - the refused file keeps its A rows (CocoIndex's contract for a failing component), where
     the documents gate sees them and stays closed.
4. Space A again: the healthy file returns to A. Memo entries are keyed by the fingerprint
   they were recorded under, so a change back is a change.

In every pass, every row's vector is the one the fake embedder produces for that row's own
stamp.
"""

import os
import re
import subprocess
import sys

import pytest

from ingest import chunk, embed
from ingest.arcade import ArcadeSchemaError, _post
from ingest.tests import fake_embedder

ARCADE_HTTP = os.environ.get("ARCADE_HTTP", "http://arcadedb:2480")
AUTH = ("root", os.environ["ARCADEDB_PASSWORD"])
DATABASE = "aura_t_space_reruns"
SPACE_A, SPACE_B = "es1-aaaaaaaaaaaaaaaa", "es1-bbbbbbbbbbbbbbbb"
HEALTHY, REFUSED = "fornitori.txt", "rifiutato.txt"
OBJECTS = [
    f"{HEALTHY}=Il fornitore consegna i ricambi ogni martedì mattina.",
    f"{REFUSED}=Questa nota contiene {fake_embedder.REFUSED} a metà del testo.",
]


def _drop() -> None:
    try:
        _post(ARCADE_HTTP, "/api/v1/server", {"command": f"drop database {DATABASE}"}, AUTH, 30.0)
    except ArcadeSchemaError as exc:
        if "not exist" not in str(exc).lower():
            raise


@pytest.fixture
def database():
    _drop()
    try:
        yield DATABASE
    finally:
        _drop()


def _pass(state, space, refusing=""):
    env = dict(os.environ, AURA_EMBED_SPACE=space, COCOINDEX_DB=str(state), SPACE_THAT_REFUSES=refusing)
    done = subprocess.run(
        [sys.executable, "-m", "ingest.tests.space_driver", DATABASE, *OBJECTS],
        env=env, capture_output=True, text=True, timeout=600,
    )
    assert done.returncode == 0, done.stdout + done.stderr
    requests = int(re.search(r"^REQUESTS (\d+)$", done.stdout, re.MULTILINE).group(1))
    return done.stdout.count("[extract] "), requests


def _stamps() -> dict[str, set[str]]:
    """Each file's stamps, after checking that every row's vector is its own stamp's."""
    stamps: dict[str, set[str]] = {}
    for type_name, field in (("Passage", "text"), ("IndexedDocument", "card")):
        body = _post(ARCADE_HTTP, f"/api/v1/query/{DATABASE}", {
            "language": "sql",
            "command": f"SELECT source_key, embed_space, {field} AS sent, embedding FROM {type_name}",
        }, AUTH, 30.0)
        for row in body["result"]:
            stamps.setdefault(row["source_key"], set()).add(row["embed_space"])
            if not (row["sent"] or "").strip():
                continue  # an empty card stores a zero vector, out of scope for this design
            expected = fake_embedder.vector(row["embed_space"], chunk.EMBED_DOC_PREFIX + row["sent"],
                                            embed.DIMENSIONS)
            assert row["embedding"] == pytest.approx(expected, abs=1e-6), (
                f"{type_name} {row['source_key']}: the vector is not its stamp's"
            )
    return stamps


def test_a_route_change_re_embeds_once_re_extracts_nothing_and_never_mislabels(database, tmp_path):
    state = tmp_path / "coco.db"

    extracted, requests = _pass(state, SPACE_A)
    assert extracted == 2 and requests > 0
    assert _stamps() == {HEALTHY: {SPACE_A}, REFUSED: {SPACE_A}}

    assert _pass(state, SPACE_A) == (0, 0), "an unchanged pass re-ran: the memo does not hold"

    extracted, requests = _pass(state, SPACE_B, refusing=SPACE_B)
    assert extracted == 0, "a route change re-extracted: extraction is not memoized apart from embedding"
    assert requests > 0
    assert _stamps() == {HEALTHY: {SPACE_B}, REFUSED: {SPACE_A}}

    extracted, _ = _pass(state, SPACE_A)
    assert extracted == 0
    assert _stamps() == {HEALTHY: {SPACE_A}, REFUSED: {SPACE_A}}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `bash .superpowers/sdd/2026-09-24-embedding-ingest/ingest-py.sh ingest/tests/test_embed_space_reruns.py`
Expected: FAIL at the first pass's `returncode == 0`, with `AttributeError: module 'ingest.app' has no attribute 'mount_targets'` in the driver's stderr.

- [ ] **Step 4: Split extraction from indexing**

In `services/ingest/app.py`:

(a) `_card` drops its memo: its decorator line `@coco.fn(memo=True)` is deleted. It keyed on a temporary path and never hit; it now runs inside `_extract`'s memo.

(b) Replace the whole `process_file` function with the four definitions below. Every comment inside today's `process_file` moves with the line it explains:
- the walker/`resolve()` comment and the chat-attachment name comment go with `process_file`;
- the `ONE conversion`, `Routed by family` and `Read inside the block` comments go with `_extract`;
- the `document_budget()` comment and the `One row per OBJECT` comment go with `index_object`.

The old `[extract] {key}` comment is replaced by `_extract`'s docstring.
```python
@dataclasses.dataclass(frozen=True, slots=True)
class Extracted:
    """What one file says: its text, its card and where its sections start."""

    text: str
    card: str
    anchors: list[outline.Anchor]


@coco.fn(memo=True)
def _extract(content: bytes, suffix: str, file_name: str, content_type: str | None) -> Extracted:
    """Read a file once per content, apart from embedding it.

    embed.embed_text carries the embedding space as its dependency, so a route change re-runs
    process_file for every document. Before this split that re-ran every vision and
    speech-to-text call with it, the billed ones included (audit F7). This memo is keyed by
    the bytes, the key's suffix -- which names the temporary file the extractors route on --
    the name and the content type, and none of them moves with the route. A scanned PDF is
    still read again when the vision route changes, through media.extract_scanned_pdf's
    dependency on media.CONFIG_FINGERPRINT.

    The print runs only when this body does: "[extract]" in the log counts real extractions
    (scripts/ingest_reconcile_e2e.sh asserts on the count).
    """
    print(f"[extract] {file_name}", flush=True)
    with tempfile.NamedTemporaryFile(suffix=suffix) as tmp:
        tmp.write(content)
        tmp.flush()
        # (the "ONE conversion, both consumers" comment, verbatim)
        with extract.prepared(tmp.name) as ready:
            # (the "Routed by family" comment, verbatim)
            text = media.index_text(ready, file_name, content_type)
            # (the "Read inside the block" comment, verbatim)
            anchors = outline.anchors_in(text, outline.titles_of(ready))
            card = _card(ready, _card_name(file_name, ready))
    return Extracted(text=text, card=card, anchors=list(anchors))


@coco.fn(memo=True)
async def process_file(
    file: amazon_s3.S3File, identity_id: str, table: neo4j.TableTarget[Passage],
    documents: neo4j.TableTarget[IndexedDocument],
) -> None:
    # (the walker/resolve() comment, verbatim)
    key = file.file_path.resolve()
    content = await file.read()
    # (the "The object's own name when it carries one" comment, verbatim)
    facts = await source.object_facts(coco.use_context(S3), _S3_CONFIG, key)
    file_name = facts.file_name or pathlib.PurePosixPath(key).name
    extracted = _extract(content, pathlib.PurePosixPath(key).suffix, file_name, facts.content_type)
    await index_object(identity_id, key, file_name, content, extracted, table, documents)


async def index_object(
    identity_id: str, key: str, file_name: str, content: bytes, extracted: Extracted,
    table: neo4j.TableTarget[Passage], documents: neo4j.TableTarget[IndexedDocument],
) -> None:
    """Chunk, embed and declare one object's rows, every vector stamped with embed.SPACE."""
    source_kind = "s3"
    search_document_id = identity.search_document_id(identity_id, source_kind, key)
    # (the "document_budget(), not the bare ceiling" comment, verbatim)
    pieces = chunk.chunk(extracted.text, max_tokens=chunk.document_budget(), anchors=extracted.anchors)
    raw_sha256 = hashlib.sha256(content).hexdigest()
    await coco.map(
        process_chunk, list(enumerate(pieces)),
        search_document_id, source_kind, key, raw_sha256, table,
    )
    # (the "One row per OBJECT" comment, verbatim)
    documents.declare_record(row=IndexedDocument(
        search_document_id=search_document_id,
        source_kind=source_kind,
        source_key=key,
        file_name=file_name,
        file_name_words=_name_words(file_name),
        raw_sha256=raw_sha256,
        normalized_text_sha256=_text_fingerprint(extracted.text),
        size_bytes=len(content),
        passage_count=len(pieces),
        card=extracted.card,
        # (the "The card describes the file" comment, verbatim)
        embedding=(await embed.embed_text(extracted.card) if extracted.card.strip()
                   else [0.0] * embed.DIMENSIONS),
        embed_space=embed.SPACE,
        indexed_at=datetime.datetime.now(datetime.timezone.utc),
    ))
```

(c) Replace `app_main` with:
```python
async def mount_targets() -> tuple[neo4j.TableTarget[Passage], neo4j.TableTarget[IndexedDocument]]:
    """The two record targets; arcade.ensure_schema owns their DDL and they only reconcile rows.

    The SAME target connector writes the passages and, pointed at a second type, the
    documents. One writer, one store, one query language -- and a record and its passages
    can never end up in different databases.
    """
    table = await neo4j.mount_table_target(
        KG_DB, arcade.PASSAGE_TYPE,
        await neo4j.TableSchema.from_class(Passage, primary_key="passage_key"),
        primary_key="passage_key",
    )
    documents = await neo4j.mount_table_target(
        KG_DB, arcade.DOCUMENT_TYPE,
        await neo4j.TableSchema.from_class(IndexedDocument, primary_key="search_document_id"),
        primary_key="search_document_id",
    )
    return table, documents


@coco.fn
async def app_main(identity_id: str, interval_s: float) -> None:
    table, documents = await mount_targets()
    await coco.mount(
        coco.auto_refresh(reconcile, interval=datetime.timedelta(seconds=interval_s)),
        identity_id, table, documents,
    )
```
Replace every `(… verbatim)` placeholder with the named comment from today's `process_file`, unchanged.

- [ ] **Step 5: Run the test to verify it passes, then the whole suite**

Run: `bash .superpowers/sdd/2026-09-24-embedding-ingest/ingest-py.sh ingest/tests/test_embed_space_reruns.py`
Expected: PASS.

If an assertion fails, the failure is a measurement of CocoIndex, not a typo to adjust:
- `extracted == 0` failing in pass 3 means a child function's memo does not survive its parent's re-run;
- `REFUSED: {SPACE_A}` failing means a failed component lost its rows;
- a vector mismatch means a row was written with another space's vector.

Stop, record the output in the ledger, and rule from the spec. The test is not to be weakened.

Then run: `bash .superpowers/sdd/2026-09-24-embedding-ingest/ingest-py.sh`
Expected: the whole suite passes. `wc -l services/ingest/app.py` is below 600.

- [ ] **Step 6: Commit**

```bash
git add services/ingest/app.py services/ingest/tests/fake_embedder.py \
  services/ingest/tests/space_driver.py services/ingest/tests/test_embed_space_reruns.py
git commit -m "feat(ingest): a route change re-embeds every document without re-extracting any

Extraction becomes a function memoized by content, apart from embedding, so the space
dependency that re-runs every file after a route change no longer re-runs vision and
speech-to-text with it (audit F7). The re-run test drives the production indexing path
against ArcadeDB through four passes -- first, unchanged, a new space that refuses one
file, and back -- and checks every row's vector against its own stamp in each.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Scripts start their child with the supervisor's route

**Files:**
- Create: `scripts/ingest_embed_env.sh`
- Modify: `scripts/ingest_reconcile_e2e.sh` (the settings-DSN loop at ~72-78; the three `--entrypoint python "$img" -m ingest.app` runs at ~213, ~415, ~483)
- Modify: `scripts/ingest_media_e2e.sh` (the `run_ingest` docker run at ~152)

**Interfaces:**
- Consumes: `aura-ingest-supervisor -print-embed-env` (Task 3); `AURA_EMBED_SPACE` is now required (Task 4).
- Produces:
  - `ingest_container_env <KEY>`, which prints KEY's value from the running `aura-ingest` container;
  - `ingest_embed_env <image> <network> <out-file>`, which uses that container's own settings DSN and sealing secret, so the answer is the one the running supervisor gets. `ingest_media_e2e.sh` does not load `.env`, so neither value can come from the script's own environment.

- [ ] **Step 1: Watch the reconcile E2E fail on the current scripts**

Rebuild the image, because the supervisor binary changed. Then bring up what the script inspects and run it:
```bash
docker build -f docker/aura-ingest/Dockerfile -t aura-ingest:local .
docker compose up -d postgres arcadedb garage aura-llama-embed aura-ingest
AURA_INGEST_E2E_SCOPE=reconcile bash scripts/ingest_reconcile_e2e.sh
```
Expected: FAIL in the first `run_pass`, with `RuntimeError: AURA_EMBED_SPACE is required` in the child's log. A script that runs `python -m ingest.app` directly no longer starts a child without a space.

`aura-ingest` is the real supervisor on the local dev stack. With Task 3 in the image it resolves the route from the local `aura.settings`; its log line `embedding route resolved` names the space. Record that line in the ledger: it is the first live reading of the supervisor.

- [ ] **Step 2: Write the helper**

Create `scripts/ingest_embed_env.sh`:
```bash
# Sourced by the ingest E2E scripts that run `python -m ingest.app` directly.

# ingest_container_env <KEY> prints KEY's value in the running aura-ingest container.
ingest_container_env() {
  local entry
  while IFS= read -r entry; do
    case "$entry" in
      "$1="*) printf '%s\n' "${entry#"$1"=}"; return 0 ;;
    esac
  done < <(docker inspect aura-ingest --format '{{range .Config.Env}}{{println .}}{{end}}')
  echo "FAIL: aura-ingest has no $1" >&2
  return 1
}

# ingest_embed_env <image> <network> <out-file> writes the embedding environment a supervised
# child would receive, by asking the supervisor binary itself (-print-embed-env) with the
# running supervisor's own settings DSN and sealing secret. A child a script starts then
# stamps the space the daemon compares against, never one the script made up. The file
# carries the route's credential: keep it in the script's private scratch directory, which
# the script's cleanup removes.
ingest_embed_env() {
  local dsn secret
  dsn="$(ingest_container_env AURA_DB_URL)" || return 1
  secret="$(ingest_container_env AURA_AUTHULA_SECRET)" || return 1
  docker run --rm --network "$2" \
    -e AURA_DB_URL="$dsn" -e AURA_AUTHULA_SECRET="$secret" \
    -e AURA_EMBED_BASE_URL=http://aura-llama-embed:8081 \
    --entrypoint aura-ingest-supervisor "$1" -print-embed-env > "$3"
}
```

- [ ] **Step 3: Use it in both scripts**

In `scripts/ingest_reconcile_e2e.sh`:
1. After `repo_root`/`cd` near the top, add `source "$repo_root/scripts/ingest_embed_env.sh"` (use the script's existing repo-root variable, as `retrieval_fixture` does).
2. Replace the settings-DSN block with `settings_db_url="$(ingest_container_env AURA_DB_URL)"`. That block is `settings_db_url=""`, the `while … done < <(docker inspect aura-ingest …)` loop, and its `[ -n "$settings_db_url" ] || …` check.
3. After `scratch="$(mktemp -d)"`, add:
```bash
embed_env="$scratch/embed.env"
ingest_embed_env "$img" "$net" "$embed_env"
```
4. In each of the three `docker run … --entrypoint python "$img" -m ingest.app` invocations, add `--env-file "$embed_env" \` on the line before `-v "$volume…:/state"`.

In `scripts/ingest_media_e2e.sh`:
1. After `cd "$repo_root"`, add `source "$repo_root/scripts/ingest_embed_env.sh"`.
2. Add `aura-ingest` to the `for container in …` list of containers that must be running. Then, after that loop, add:
```bash
embed_env="$scratch/embed.env"
ingest_embed_env "$image" "$network" "$embed_env"
```
3. In `run_ingest`, add `--env-file "$embed_env" \` before `-v "$state_volume:/state"`, and delete the now-overridden `-e AURA_EMBED_BASE_URL=http://aura-llama-embed:8081 \` line.

The child's `-e AURA_DB_URL=` stays: it keeps `aura-media-index` on the environment's vision route, as the script intends.

- [ ] **Step 4: Run the reconcile E2E to verify it passes**

Run:
```bash
bash -n scripts/ingest_embed_env.sh scripts/ingest_reconcile_e2e.sh scripts/ingest_media_e2e.sh
AURA_INGEST_E2E_SCOPE=reconcile bash scripts/ingest_reconcile_e2e.sh
```
Expected: no syntax output, then the script's own `ok: reconciliation only -- add, rerun-unchanged, modify, delete, live second cycle and two-identity isolation all passed.`

Then check that the helper and the running supervisor name the same space. This prints only the space line, never the key line:
```bash
source scripts/ingest_embed_env.sh
ingest_embed_env aura-ingest:local aura_default /dev/stdout | grep '^AURA_EMBED_SPACE='
docker logs aura-ingest 2>&1 | grep 'embedding route resolved' | tail -1
```
Expected: the same `es1-…` in both lines. Record them in the ledger. The E2E's own databases are dropped by its cleanup, and Task 6's test already checks every row's stamp against its vector.

`scripts/ingest_media_e2e.sh` needs Ollama Cloud and TTS. It runs in the VM E2E after plan 5; only its syntax is checked here.

- [ ] **Step 5: Commit**

```bash
git add scripts/ingest_embed_env.sh scripts/ingest_reconcile_e2e.sh scripts/ingest_media_e2e.sh
git commit -m "test(ingest): scripts start their child with the supervisor's own route

A child now refuses to start without a space, so the E2E scripts that run the Python app
directly ask the supervisor binary for the route a supervised child would get and pass it
through an env file in their scratch directory. The settings DSN lookup both scripts need
moves into the shared helper.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Measure what a full documents re-embed costs ArcadeDB

The spec says (`concepts/vector-search.adoc`) that before 26.10.1 an updated vector forces a full graph rebuild on the first query after the next restart, and leaves the documents family's number to this plan. This task measures it on a disposable ArcadeDB, never on the dev stack's or the VM's.

**Files:**
- Create (git-ignored workspace): `.superpowers/sdd/2026-09-24-embedding-ingest/rebuild/measure.py`, `run.sh`
- Modify: `docs/superpowers/specs/2026-09-23-embedding-model-change-design.md` ("What this design does not prove")

- [ ] **Step 1: Write the measurement**

`.superpowers/sdd/2026-09-24-embedding-ingest/rebuild/measure.py`:
```python
"""Time the first vector query after an ArcadeDB restart, before and after re-embedding.

    python measure.py ready | load N | query | reembed N
"""
import base64
import json
import math
import random
import sys
import time
import urllib.error
import urllib.request

BASE = "http://arcadedb-measure:2480/api/v1"
AUTH = "Basic " + base64.b64encode(b"root:measure-only-pw").decode()
DB, DIMS = "rebuild", 768


def call(path, body=None):
    req = urllib.request.Request(BASE + path, data=json.dumps(body).encode() if body else None,
                                 headers={"Authorization": AUTH, "Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=900) as resp:
        raw = resp.read()
        return json.loads(raw) if raw else {}


def vec(seed):
    rnd = random.Random(seed)
    raw = [rnd.gauss(0, 1) for _ in range(DIMS)]
    norm = math.sqrt(sum(v * v for v in raw))
    return [round(v / norm, 6) for v in raw]


def write(n, generation, update):
    for start in range(0, n, 200):
        lines = []
        for i in range(start, min(n, start + 200)):
            v = json.dumps(vec(generation * 10_000_000 + i))
            lines.append(f"UPDATE Passage SET embedding = {v} WHERE k = 'p{i}'" if update
                         else f"INSERT INTO Passage SET k = 'p{i}', embedding = {v}")
        call(f"/command/{DB}", {"language": "sqlscript", "command": ";\n".join(lines)})


def main():
    phase = sys.argv[1]
    if phase == "ready":
        deadline = time.monotonic() + 180
        while time.monotonic() < deadline:
            try:
                urllib.request.urlopen(BASE + "/ready", timeout=5)
                return
            except (urllib.error.URLError, OSError):
                time.sleep(2)
        raise SystemExit("not ready")
    if phase == "load":
        call("/server", {"command": f"create database {DB}"})
        for stmt in ("CREATE VERTEX TYPE Passage", "CREATE PROPERTY Passage.k STRING",
                     "CREATE PROPERTY Passage.embedding ARRAY_OF_FLOATS",
                     "CREATE INDEX ON Passage (k) UNIQUE",
                     'CREATE INDEX ON Passage (embedding) LSM_VECTOR METADATA '
                     '{ "dimensions": 768, "similarity": "COSINE", "quantization": "NONE" }'):
            call(f"/command/{DB}", {"language": "sql", "command": stmt})
        write(int(sys.argv[2]), 0, update=False)
    elif phase == "reembed":
        write(int(sys.argv[2]), 1, update=True)
    elif phase == "query":
        q, times = vec(424242), []
        for _ in range(3):
            start = time.perf_counter()
            call(f"/query/{DB}", {"language": "sql", "params": {"q": q},
                                  "command": "SELECT expand(`vector.neighbors`('Passage[embedding]', :q, 10))"})
            times.append((time.perf_counter() - start) * 1000)
        print("query ms: " + " ".join(f"{t:.0f}" for t in times), flush=True)


main()
```
`.superpowers/sdd/2026-09-24-embedding-ingest/rebuild/run.sh`:
```bash
#!/usr/bin/env bash
# Git Bash: bash .superpowers/sdd/2026-09-24-embedding-ingest/rebuild/run.sh 5000
set -euo pipefail
export MSYS_NO_PATHCONV=1
n="$1"
here="$(cd "$(dirname "$0")" && pwd -W)"
image="arcadedata/arcadedb:26.9.1@sha256:02a1a74fcef3c9d91e680262ddb7fde05ff9cfb356b366e7faf8e41d80995a70"
cleanup() {
  docker rm -f arcadedb-measure >/dev/null 2>&1 || true
  docker volume rm rebuild-measure >/dev/null 2>&1 || true
  docker network rm rebuild-measure >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup
docker network create rebuild-measure >/dev/null
docker volume create rebuild-measure >/dev/null
docker run -d --name arcadedb-measure --network rebuild-measure \
  -v rebuild-measure:/home/arcadedb/databases \
  -e JAVA_OPTS="-Darcadedb.server.rootPassword=measure-only-pw" "$image" >/dev/null
py() { docker run --rm --network rebuild-measure -v "$here:/w:ro" --entrypoint python aura-ingest:local /w/measure.py "$@"; }
py ready
py load "$n"
echo "== $n vectors, loaded, warm"; py query
docker restart arcadedb-measure >/dev/null; py ready
echo "== after a restart, nothing re-embedded"; py query
py reembed "$n"
echo "== re-embedded, before a restart"; py query
docker restart arcadedb-measure >/dev/null; py ready
echo "== after a restart, every vector re-embedded"; py query
```

- [ ] **Step 2: Run it at 5,000 and at 20,000 vectors**

Run: `bash .superpowers/sdd/2026-09-24-embedding-ingest/rebuild/run.sh 5000`, then the same with `20000`.
Expected: four `query ms:` lines per run. Record all eight in the ledger with the wall-clock time of each run.

If 20,000 takes over 20 minutes to load, stop it, record the rate reached, and rule the upper size from it: the number records what was measured, not a target.

- [ ] **Step 3: Record the measurement in the spec**

In "What this design does not prove", replace the bullet that begins `- **A full re-embed on ArcadeDB 26.9.1 costs one graph rebuild per vector index**` with the same statement, followed by the measured numbers. Use this form, filled with the values from Step 2:
```
- **A full re-embed on ArcadeDB 26.9.1 costs one graph rebuild per vector index** on the first
  query after the next ArcadeDB restart: before 26.10.1 an updated vector is a delete plus an
  insert, and a delete forced that rebuild (arcadedb-docs `concepts/vector-search.adoc`).
  Measured 2026-09-24 on a disposable 26.9.1 (768-d, COSINE, no quantization, one query
  thread): at 5,000 vectors the first query after a restart took A ms after a full re-embed
  against B ms with nothing re-embedded (later queries C ms); at 20,000, D ms against E ms
  (F ms). Memory indexes hold tens to thousands of vectors. Not measured: concurrent queries
  during the rebuild, and INT8 quantization.
```
If the numbers show no rebuild (the first query after a re-embed is as fast as without one), write that instead, and say the documented cost did not reproduce on this build.

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/specs/2026-09-23-embedding-model-change-design.md
git commit -m "docs(spec): measure the vector rebuild a documents re-embed costs on 26.9.1

The spec took the rebuild from ArcadeDB's docs and left the documents family's number to
plan 3. Measured on a disposable 26.9.1 at 5,000 and 20,000 vectors, before and after a
full re-embed, across restarts.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## After the last task

- Final whole-branch review by a fresh opus agent: `superpowers:executing-plans`, "Final Review".
  - Launch it with the agent pipe (`reference_agent_pipe_claude_headless`), not Codex, which is broken.
  - Give it this plan's Review Focus verbatim and the ledger's `Ruling:` lines.
- Carried to plan 4 (documents family):
  - the documents gate, lexical mode and floors;
  - the daemon's document query embedder with its live key.

  Rows written by plan 3 carry the stamps the gate will read.
- Carried to plan 5: the cockpit counts, `aura doctor`, and MCP's watcher.
- Carried to the VM E2E: the first live post-upgrade pass on the documents family.
  - Every document re-extracts once, because `deps` and the new functions are new fingerprints (spec §6).
  - Measure what the pass does to document-query latency on the shared `-np 1` sidecar.
- No push until plans 1–5 are done and CI is green.
