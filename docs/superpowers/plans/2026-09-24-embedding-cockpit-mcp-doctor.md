# Embedding change, plan 5 of 5: cockpit, MCP watcher, doctor — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans (Native, chosen for
> plans 1-4). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** the operator changes the embedding route from the cockpit through a measured preview and
a confirmed apply, sees per tenant and family which rows are in which space, and every process
converges on the new route without a hand restart.

**Architecture:** the corpus side (counts, stuck documents, work to redo) lives in
`internal/arcadedb` on the tenant walker the pass already uses; the route side (target space,
native width, throughput, input limit, price) lives in `internal/embeddings`; `internal/agui`
turns both into the three §4 endpoints and refuses the three route keys on the generic writes;
`cmd/aura` wires them and gains a doctor check; `cmd/arcadedb-mcp` re-resolves its route every
60 s and exits when it moved; the web card previews before it applies.

**Tech Stack:** Go 1.26, ArcadeDB 26.9.1 SQL over HTTP, React + react-i18next + vitest.

**Spec:** `docs/superpowers/specs/2026-09-23-embedding-model-change-design.md` (§4, §7, §9, §10,
"Fix on touch", "Testing and acceptance"). Plans 1-4 are done; carry-over memories
`project_embedding_change_plan{2,3,4}_carryover`.

## Global Constraints

- No file above 600 lines; a file touched gets dead code removed in the same commit.
- Protected keys: `AURA_EMBED_MODEL`, `AURA_EMBED_BASE_URL`, `AURA_EMBED_CLOUD_BASE_URL` — generic
  `PUT`/`DELETE /api/settings/{key}` answer **409** and name `POST /api/settings/embedding-route`.
- The new endpoints use the capabilities the route keys already require: governance.write for the
  two POSTs (mounted like `PUT /api/settings/{key}`), governance.read for the GET; the handler
  also runs `authorizeSettingWrite` over the three keys.
- Preview refusals: native width narrower than `max(768, AURA_EMBED_DIMENSIONS)`; published input
  limit below **2048** tokens. Wider width is allowed with the Matryoshka warning.
- Token estimate = characters ÷ `CHARS_PER_TOKEN_FALLBACK` (= 3, `services/ingest/chunk.py:64`),
  an overshoot by design. Cost = tokens × `pricing.prompt`, "unknown" without a price.
- The probe embeds a fixed synthetic batch, never corpus text.
- `confirm_space` must equal the target the server recomputes from the same three values.
- Apply writes the three rows in one `ReplaceMany` and fires the existing restart trigger;
  nothing else.
- MCP exits through its existing graceful shutdown (10 s budget) and only when the route moved.
- en + it strings for every new cockpit string. Frontend gates: vitest ≥85% lines on new files;
  Stryker ≥70% runs in CI only.
- Tests: targeted `-run` per task, timed; whole-suite, race, deadcode, coverage at plan end or CI.
- Commits: explicit paths only (shared index), gofmt first, never `--no-verify`.

## Review Focus

1. A hosted route with no OpenRouter key: preview must answer a refusal the cockpit shows, never
   a 500 or a hang; apply must refuse too.
2. A tenant whose ingest types do not exist yet (no document ever): the report shows the
   documents family as empty and open, never an error for the whole page.
3. `confirm_space` from a preview made before the local GGUF changed: apply must refuse (the
   recomputed target differs), never write a route the operator did not see.
4. The MCP watcher on a settings read that fails (Postgres blip): keep running, never exit.
5. The web form with only non-embed backend keys dirty: the generic Save still saves them; the
   embed keys never reach the generic `PUT`.

---

### Task 1: the route probe (`internal/embeddings`)

**Files:**
- Create: `internal/embeddings/probe.go`, `internal/embeddings/probe_test.go`
- Modify: `internal/embeddings/fit.go` (catalogue entry read shared by `InputLimit`),
  `internal/embeddings/tasks.go` (`CharsPerTokenFallback`),
  `internal/embeddings/prefix_parity_test.go` (parity with chunk.py)

**Interfaces (Produces):**
```go
const CharsPerTokenFallback = 3 // tasks.go; parity-tested against chunk.py

type RouteProbe struct {
	Space          Space   // at the width the caller asked for
	NativeWidth    int     // what the model returns with no `dimensions` field
	CharsPerSecond float64 // the synthetic batch's characters / the request's wall time
	InputLimit     int     // tokens, from the route's catalogue
	PricePer1M     float64 // pricing.prompt per 1M tokens
	HasPrice       bool
}

// ProbeRoute resolves embed's space at dims and sends probeTexts once through the route.
func ProbeRoute(ctx context.Context, client *http.Client, embed config.EmbedConfig,
	credential string, dims int) (RouteProbe, error)

func (c *Client) catalogEntry(ctx context.Context) (llm.ModelCatalogEntry, error) // fit.go
```
- `probeTexts`: a fixed `[]string` of 8 synthetic sentences (~160 chars each), declared in probe.go.
- The raw request omits `dimensions` (`embeddingRequest.Dimensions` is `omitempty`), so a hosted
  model answers its native width; every returned vector must share one width.
- A hosted route with an empty credential returns `ErrNoCredential` before any request.
- `InputLimit` keeps its behaviour and its cache; the limit arithmetic moves to a helper both use.

- [ ] Step 1: tests (httptest server): local route probe reads `/v1/models` (attest + n_ctx) and
  `/v1/embeddings`, returns NativeWidth 768, a positive CharsPerSecond, InputLimit 2048, no price;
  hosted probe sends `Authorization: Bearer`, no `dimensions` field, reads price
  `pricing.prompt` "0.00000002" → PricePer1M 0.02; mixed widths → error; empty credential →
  `ErrNoCredential` and zero requests; parity `CHARS_PER_TOKEN_FALLBACK = 3` in chunk.py.
- [ ] Step 2: run, watch fail (undefined `ProbeRoute`).
- [ ] Step 3: implement.
- [ ] Step 4: `go test ./internal/embeddings/ -run 'Probe|Parity|InputLimit' -count=1` → PASS.
- [ ] Step 5: commit `feat(embeddings): a route probe names the target space, width, speed, limit and price`.

### Task 2: the corpus report (`internal/arcadedb`)

**Files:**
- Create: `internal/arcadedb/embedding_space_report.go`, `internal/arcadedb/embedding_space_report_test.go`
- Modify: `internal/arcadedb/document_space_live_integration_test.go` (one live assertion that
  `sum(<text>.length())` and the four counts parse on 26.9.1)

**Interfaces (Produces):**
```go
type TypeTally struct {
	Type       string `json:"type"`
	InSpace    int    `json:"in_space"`
	OtherSpace int    `json:"other_space"`
	NoVector   int    `json:"no_vector"`
	Rejected   int    `json:"rejected"` // no vector, stamped with this space: the model refused it
}
type FamilyState struct {
	Family string      `json:"family"` // "memory" | "documents"
	Space  string      `json:"space"`
	Open   bool        `json:"open"`
	Types  []TypeTally `json:"types"`
}
type StuckDocument struct {
	FileName  string `json:"file_name"`
	SourceKey string `json:"source_key"`
	Space     string `json:"space"` // "" when unstamped
}
type TenantSpaceReport struct {
	IdentityID     string          `json:"identity_id"`
	Families       []FamilyState   `json:"families"`
	StuckDocuments []StuckDocument `json:"stuck_documents"`
	IngestStatus   string          `json:"ingest_status,omitempty"`
	IngestErrors   int             `json:"ingest_errors"`
}
type TypeWork struct {
	Type  string `json:"type"`
	Rows  int    `json:"rows"`
	Chars int    `json:"chars"`
}
type CorpusWork struct {
	Types []TypeWork `json:"types"`
	// PassagesOverLimit counts passages longer than the limit in characters: a lower bound on
	// the bytes the hosted cut measures (spec §6).
	PassagesOverLimit int `json:"passages_over_limit"`
}

func (b *TenantBackfill) SpaceReports(ctx context.Context, memorySpace, documentSpace string) ([]TenantSpaceReport, error)
func (b *TenantBackfill) CorpusWork(ctx context.Context, memorySpace, documentSpace string, limitChars int) (CorpusWork, error)
```
- Both walk identities through `sweepTenant` (unprovisioned tenants are skipped, not failed); a
  tenant that fails is logged and left out; an error only when no tenant answered and one failed.
- Per type four counts: vectors in space, `vectorsOutside`, `embedding IS NULL`, and
  `embedding IS NULL AND embed_space = :space`; the memory types keep their `live` filter (traces
  need `:now`). A missing ingest type (`missingIngestType`) is zero rows.
- `Open` = `OtherSpace == 0` over the family's types (the gate's own rule).
- Stuck documents: `SELECT file_name, source_key, embed_space FROM IndexedDocument WHERE
  embedding IS NOT NULL AND (embed_space IS NULL OR embed_space <> :space) ORDER BY file_name
  LIMIT 50`. The ingest row: `SELECT status, errors FROM IngestStatus LIMIT 1`.
- Work: per type `SELECT count(*) AS n, sum(<source>.length()) AS chars FROM <T> WHERE
  (embed_space IS NULL OR embed_space <> :space) AND <source> IS NOT NULL<live>` with the
  family's target; sources: fact `statement`, turn `content`, trace `provider_summary`, Passage
  `text`, IndexedDocument `card`. Over limit: `SELECT count(*) AS n FROM Passage WHERE
  text.length() > :limit` (skipped when `limitChars` is 0).

- [ ] Step 1: unit tests over the fake HTTP server (`testDocumentIndex`/client fixtures): the four
  counts per type become one tally; a family with an OtherSpace row is closed; missing Passage type
  → zero, open; stuck list and ingest row decoded; work sums across two tenants; unprovisioned
  tenant skipped.
- [ ] Step 2: watch fail.
- [ ] Step 3: implement (≤300 lines).
- [ ] Step 4: `go test ./internal/arcadedb/ -run 'SpaceReport|CorpusWork' -count=1` → PASS; live:
  `go test -tags arcadedb_integration ./internal/arcadedb/ -run DocumentSpaceLive -count=1` → PASS.
- [ ] Step 5: commit `feat(arcadedb): report each tenant's rows by embedding space, and the work a new space costs`.

### Task 3: the three endpoints and the refused keys (`internal/agui`)

**Files:**
- Create: `internal/agui/settings_embedding_route.go`, `internal/agui/settings_embedding_route_test.go`
- Modify: `internal/agui/settings_api.go` (409 on the three keys in PUT/DELETE; routes
  registered; header says "the embedding route" instead of "the embed dimension")

**Interfaces:**
- Consumes: Task 1 `embeddings.RouteProbe`, Task 2 `arcadedb.TenantSpaceReport`, `CorpusWork`.
- Produces:
```go
type EmbeddingRouteValues struct {
	BaseURL      string `json:"AURA_EMBED_BASE_URL"`
	Model        string `json:"AURA_EMBED_MODEL"`
	CloudBaseURL string `json:"AURA_EMBED_CLOUD_BASE_URL"`
}
// EmbeddingRoutes is the seam cmd/aura implements.
type EmbeddingRoutes interface {
	Current(ctx context.Context) (memory, documents embeddings.Space, err error)
	Reports(ctx context.Context, memory, documents string) ([]arcadedb.TenantSpaceReport, error)
	Probe(ctx context.Context, route EmbeddingRouteValues) (memory, documents embeddings.RouteProbe, err error)
	Work(ctx context.Context, memory, documents string, limitChars int) (arcadedb.CorpusWork, error)
	Dimensions() int // AURA_EMBED_DIMENSIONS
}
func (s *Server) SetEmbeddingRoutes(routes EmbeddingRoutes)
```
- `GET /api/settings/embedding-space` → `{space, space_label, documents_space, floors_calibrated, tenants}`.
- `POST /api/settings/embedding-route/preview` → `{space, space_label, native_width, dimensions,
  width_warning, chars_per_second, input_limit, passages_over_limit, work, tokens, cost_usd
  (null when unknown), cost_known, local, duration_seconds, floors_calibrated, refusals[]}`; a probe
  error is a refusal `probe_failed: <err>` with 200, never a 5xx.
- `POST /api/settings/embedding-route` body `{...route, confirm_space}`: recompute the preview;
  refusals → 422; mismatch → 409 `{"error":"space_changed","space":...}`; else `ReplaceMany`
  the three rows (empty value = empty row, the settings meaning), then restart trigger (absent →
  the rows are saved and `restart_required: true`, 200).
- Refusal rules and arithmetic are one pure function `previewRoute(probe, work, dims) routePreview`.

- [ ] Step 1: tests — PUT/DELETE of each protected key → 409 naming the endpoint; GET assembles
  current spaces + reports; preview arithmetic (tokens = chars/3 rounded up, cost from price,
  local → cost 0 & known, no price → unknown); refusals (width 512 < 768, limit 512 < 2048, probe
  error); apply: mismatch → 409, refusal → 422, match → ReplaceMany with exactly the three keys and
  the trigger fired once; unauthenticated → 401; no seam → 503.
- [ ] Step 2: watch fail. Step 3: implement. Step 4:
  `go test ./internal/agui/ -run 'EmbeddingRoute|EmbeddingSpace|ProtectedEmbed|PutSetting|DeleteSetting' -count=1` → PASS.
- [ ] Step 5: commit `feat(agui): the embedding route changes only through a preview and a confirmed apply`.

### Task 4: daemon wiring and `aura doctor` (`cmd/aura`)

**Files:**
- Create: `cmd/aura/serve_embedding_route.go` (+ `_test.go`), `cmd/aura/doctor_embedding_space.go` (+ `_test.go`)
- Modify: `cmd/aura/serve_webui.go` (three mounts), the file that calls `SetSettingsStore` (one
  `SetEmbeddingRoutes` line), `cmd/aura/doctor.go` (one check row)

- `embeddingRoutes` implements `agui.EmbeddingRoutes` from the chat env: current spaces from the
  memory and document embedders' `Space`; reports and work from a `TenantBackfill` built like
  `buildMemoryMentionLink` (no embedder needed); probe through `embeddings.ProbeRoute` with the
  live key (`liveKey(runtime)`) at 768 and at `AURA_EMBED_DIMENSIONS` (one request when equal).
- Doctor check `embedding_space` (failureCode 0: a closed gate is a state, not an outage): PASS
  "all N tenants dense in <label>" or FAIL naming each `tenant family` closed with its
  other-space count.
- [ ] Steps: tests with fakes (closed gate named; all open; no ArcadeDB configured → PASS "not
  configured"); watch fail; implement; `go test ./cmd/aura/ -run 'EmbeddingRoute|DoctorEmbeddingSpace|Doctor' -count=1`;
  commit `feat(aura): wire the embedding route endpoints, and doctor names every closed gate`.

### Task 5: the MCP route watcher (`cmd/arcadedb-mcp`)

**Files:**
- Create: `cmd/arcadedb-mcp/space_watch.go`, `cmd/arcadedb-mcp/space_watch_test.go`
- Modify: `cmd/arcadedb-mcp/boot_settings.go` (keeps the store open, snapshots the environment
  before `OverlayEnv`), `cmd/arcadedb-mcp/main.go` (starts the watcher on the signal context)

```go
type routeIdentity struct{ base, model, credentialHash string }
func identityOf(route embeddingRoute) routeIdentity
func watchEmbeddingRoute(ctx context.Context, cancel func(), boot routeIdentity,
	resolve func(context.Context) (embeddingRoute, error), every time.Duration, logger *slog.Logger)
```
- Every `every` (60 s) resolve through `settings.EmbedRoute` with the pre-overlay environment;
  a failed read logs and keeps running; a different base, model or credential hash logs
  "embedding route changed; exiting to restart on it" and calls `cancel` once.
- [ ] Steps: tests (fake resolve + short interval): unchanged → never cancels; failed read →
  never cancels; model change → cancels once; credential change → cancels, the key never appears
  in the log buffer; watch fail; implement; `go test ./cmd/arcadedb-mcp/ -count=1` (whole
  package: goldens); commit `feat(arcadedb-mcp): exit and restart on a changed embedding route`.

### Task 6: the cockpit card (`web/`)

**Files:**
- Create: `web/src/settings/embeddingSpaceApi.ts`, `web/src/settings/EmbeddingSpacePanel.tsx`,
  `web/src/settings/EmbeddingRoutePreview.tsx`, tests under `web/src/settings/__tests__/`
- Modify: `web/src/settings/EmbeddingBackendControl.tsx` (Preview button; apply after confirm),
  `web/src/settings/modelSettingsState.ts` (the three keys never reach `putSetting`/`deleteSetting`),
  `web/src/settings/ModelSettingsPanel.tsx` (mounts the panel under the control),
  `web/src/i18n/*` (en + it)
- The control shows "Preview change" while the route differs from the saved one; the preview card
  lists target, width (+ warning), throughput and estimated duration, tokens and cost (or
  "unknown"/"local"), input limit and passages cut, the lexical-until-re-embedded and
  uncalibrated-floors statements, and refusals; "Apply and restart" is enabled only with no
  refusal and after the operator ticks the confirmation.
- The panel lists, per tenant and family, in space / other space / no vector / rejected, the
  gate state, the stuck documents by file name and the ingest error count.
- [ ] Steps: vitest (Save disabled until confirmed; refusal shown and apply disabled; unknown
  price; panel with stuck documents; the generic save skips the embed keys); watch fail;
  implement; run `npx vitest run src/settings` in WSL; `npm run lint` and read "Found N errors";
  commit `feat(web): preview and confirm an embedding route change, and show each family's space`.

### Task 7: records

**Files:** `prd.md` (upgrade note: after the upgrade every family is lexical until re-embedded;
documents answer `lexical_only`), spec §4/§7 amendments for the rulings below, ledger.
- [ ] commit `docs: record plan 5 — upgrade note and the rulings the cockpit and watcher took`.

### Close

`go vet ./...`, `go test -race` on touched packages, deadcode, lint; coverage via
`scripts/coverage_docker.sh`; headless opus final review; fix pass; push; CI green; VM E2E through
the updater (spec "Testing and acceptance", E2E 1-4, cockpit driven with the operator's own login).
