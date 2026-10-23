# Cloud service credentials Implementation Plan

**Status (2026-10-23):** written; spec approved in chat ("procedi pure con la risoluzione dei bachi").

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for
> tracking. Commits on master (the operator's standing rule).

**Goal:** every cloud embedding, speech and vision call takes its credential from one helper that reads
`aura.settings` on every call, never from the chat profile; a manual embedding endpoint gets its own sealed
key and never the services key.

**Architecture:** `settings.Credentials` (new, `internal/settings/credentials.go`) answers the services key,
an embedding route's key by route kind, and the primary route's key for vision. The embedding client's
credential becomes a context-taking read that can fail. `cmd/aura` builds one `Credentials` per process from
its settings store and hands its methods to the memory and document embedders, the embedding settings
probes, speech and vision; `arcadedb-mcp`, the ingest supervisor and `aura-media-index` reach it through
`settings.EmbedRoute` or directly.

**Tech Stack:** Go 1.27, React 19 + vitest (cockpit), ArcadeDB 26.10.1, Postgres `aura.settings`.

**Spec:** `docs/superpowers/specs/2026-10-23-cloud-service-credentials-design.md`.

## Global Constraints

- The services key is `OPENROUTER_API_KEY`; the manual endpoint's key is the new sealed
  `AURA_EMBED_CLOUD_API_KEY`. A present non-empty row wins; an empty or absent row falls back to the process
  environment of the same name.
- The services key goes only to OpenRouter: an embedding route of kind `openrouter`, the speech cloud base
  `llm.DefaultBaseURL`, and vision when `llm.ReasoningTarget(provider, base) == llm.ReasoningTargetOpenRouter`.
- A credential read that fails returns its error. It is never reported as `embeddings.ErrNoCredential`.
- No secret value is logged, serialized into a response, or put on the environment of the daemon.
- No migration. No file over 600 lines. Test-first; `go vet`, build, tests and `-race` (WSL) per touched
  package; web: `npx vitest run` on the touched files, `npx tsc -b`, `npx oxlint`.
- Image and video generation are not touched (`cmd/aura/media_credentials.go`).

## Review Focus

1. **The settings store is unreachable at call time.** The embedding, speech or vision call fails with the
   store's error; the cockpit never says "key missing" for a database outage. Pinned in Task 1 and Task 2.
2. **The reconciler replaces the services key while Aura runs.** The next call sends the new key with no
   restart. Pinned in Task 1 and Task 3.
3. **A manual endpoint with no key of its own.** Refused with `key_missing` in the preview and
   `ErrNoCredential` at embed time; the services key is never sent instead. Pinned in Task 1 and Task 3.
4. **An Ollama chat route whose model reads images.** Vision sends no `Authorization` header to the Ollama
   host. Pinned in Task 4.
5. **The manual endpoint's key on the wire.** `GET /api/settings` reports it set with an empty value; an
   empty secret box in the cockpit keeps the stored key. Pinned in Task 5.

---

### Task 1: the helper, the sealed endpoint key, and the route's key by kind

**Files:**
- Create: `internal/settings/credentials.go`, `internal/settings/credentials_test.go`
- Modify: `internal/settings/settings.go` (`AllowedKeys`), `internal/settings/embed_route.go`
  (`SecretReader`, `EmbedRoute` key), `internal/settings/embed_route_test.go`,
  `internal/ingestsupervisor/route_test.go`

**Interfaces:**
- Produces:
  - `type SecretReader interface { Secret(ctx context.Context, key string) (string, error) }`;
    `SecretLister` becomes `interface { Lister; SecretReader }`.
  - `const ServicesKeySetting = "OPENROUTER_API_KEY"`, `const EmbedEndpointKeySetting = "AURA_EMBED_CLOUD_API_KEY"`.
  - `type Credentials struct { Store SecretReader; LookupEnv func(string) (string, bool) }`. A nil `Store`
    reads the environment only; a nil `LookupEnv` reads no environment.
  - `func (c Credentials) ServicesKey(ctx context.Context) (string, error)`
  - `func (c Credentials) EmbedKey(ctx context.Context, embed config.EmbedConfig) (string, error)`:
    `""` for `config.EmbedLocal`, `ServicesKey` for `config.EmbedOpenRouter`, the endpoint key for
    `config.EmbedEndpoint`.
  - `func (c Credentials) PrimaryRouteKey(ctx context.Context, provider, baseURL string) (string, error)`:
    `ServicesKey` when `llm.ReasoningTarget(provider, baseURL) == llm.ReasoningTargetOpenRouter`, else `""`.
  - `EmbedRoute` keeps its signature; its returned key is now `EmbedKey` of the route it returns.

- [ ] **Step 1: Write the failing tests** in `credentials_test.go`, over a fake `SecretReader` backed by a map
  the test can change and an optional error:
  - `TestServicesKeyReadsTheRowOnEveryCall`: row `"k1"`, read, set `"k2"`, read → `"k1"` then `"k2"`.
  - `TestServicesKeyFallsBackToTheEnvironmentForAnEmptyRow`: no row, `LookupEnv` gives `"env-key"` → `"env-key"`.
  - `TestServicesKeyReturnsTheStoreError`: the fake fails with `errBoom` → `errors.Is(err, errBoom)`, key `""`.
  - `TestEmbedKeyByRoute` (table): local `{BaseURL: "http://x"}` → `""`; OpenRouter `{CloudModel: "m"}` →
    the services key; endpoint `{CloudModel: "m", CloudBaseURL: "https://api.mistral.ai"}` with both rows set
    → the endpoint key; endpoint with only the services row → `""`.
  - `TestPrimaryRouteKeyGoesOnlyToOpenRouter` (table): `("openrouter", llm.DefaultBaseURL)` → services key;
    `("ollama", "http://ollama:11434/v1")` → `""`; `("chatgpt", "https://api.openai.com/v1")` → `""`.
  - `TestEmbedCloudAPIKeyIsSealed`: `AllowedKeys[EmbedEndpointKeySetting].Secret` is true.
  - In `embed_route_test.go`, `TestEmbedRouteGivesAManualEndpointItsOwnKey`: rows `AURA_EMBED_MODEL`,
    `AURA_EMBED_CLOUD_BASE_URL`, secrets `OPENROUTER_API_KEY="services"`, `AURA_EMBED_CLOUD_API_KEY="endpoint"`
    → returned key `"endpoint"`; with the endpoint secret absent → `""`.
  - In `internal/ingestsupervisor/route_test.go`, `TestRouteResolverRefusesAManualEndpointWithoutItsKey`: the
    same rows with only the services secret → `Resolve` returns `embeddings.ErrNoCredential`.
- [ ] **Step 2: Run them, expect FAIL** (undefined `Credentials`, `EmbedEndpointKeySetting`):
  `wsl -e bash -lc 'cd /mnt/d/Aura && go test ./internal/settings/ ./internal/ingestsupervisor/'`
- [ ] **Step 3: Implement.** `credentials.go` with the interfaces above; add
  `"AURA_EMBED_CLOUD_API_KEY": {Secret: true, Kind: KindString, Label: "Embedding endpoint API key"}` beside
  `AURA_EMBED_CLOUD_BASE_URL`; in `EmbedRoute` replace the direct `OPENROUTER_API_KEY` read with
  `Credentials{Store: store, LookupEnv: lookupEnv}.EmbedKey(ctx, embed)` and rewrite its doc comment ("There
  is no embedding-specific key" goes: the manual endpoint has one, OpenRouter does not).
- [ ] **Step 4: Run them, expect PASS**, then `go vet` and `go test -race` on both packages (WSL).
- [ ] **Step 5: Commit** `feat(settings): one reader for the cloud credentials, and a manual endpoint's own key`.

### Task 2: the embedding client reads its credential with the call's context

**Files:**
- Modify: `internal/embeddings/client.go`, `route.go`, `compat.go`, their tests;
  `internal/arcadedb/embedding.go`; `cmd/arcadedb-mcp/main.go`; `cmd/aura/memory_embedder.go`,
  `cmd/aura/document_retrieval_wiring.go`, `cmd/aura/serve_embedding_route.go` (signature adaptation only)

**Interfaces:**
- Produces:
  - `type CredentialFunc func(ctx context.Context) (string, error)` in `internal/embeddings`.
  - `Client.Credential CredentialFunc`; `func (c *Client) key(ctx context.Context) (string, error)`.
  - `func NewRoute(embed config.EmbedConfig, credential CredentialFunc, dims int, timeout time.Duration) *Route`.
  - `Compatibility.Key CredentialFunc`.
  - `func NewMemoryEmbedder(embed config.EmbedConfig, credential embeddings.CredentialFunc) DenseEmbedder`.
- Behaviour is unchanged in this task: `cmd/aura` and `arcadedb-mcp` wrap what they pass today.

- [ ] **Step 1: Write the failing tests** in `internal/embeddings`:
  - `TestRouteReturnsTheCredentialReadError`: a hosted route whose credential fails with `errBoom` →
    `Embed` and `Space` return an error that `errors.Is(err, errBoom)` and not `errors.Is(err, ErrNoCredential)`;
    the test server receives no request.
  - `TestRouteRefusesAnEmptyCredential`: credential returns `""` → `ErrNoCredential` from `Embed` and `Space`.
  - Existing tests that set `Credential: func() string {…}` or `Key: func() string {…}` move to the new type.
- [ ] **Step 2: Run, expect FAIL** (type mismatch): `wsl -e bash -lc 'cd /mnt/d/Aura && go test ./internal/embeddings/'`
- [ ] **Step 3: Implement.** `key(ctx)` returns the read's error unchanged; every caller of `key()`
  (`Embed`, the request build, `fit.go`'s catalogue read, `Route.Space`) passes its `ctx` and returns the
  error. `Compatibility.Check` reads `c.Key(ctx)` once. Adapt the three `cmd/aura` callers and
  `arcadedb-mcp`'s `func() string` closures to `CredentialFunc` with no change of source.
- [ ] **Step 4: Run, expect PASS**: `go build ./... && go vet ./...` and `-race` tests for `internal/embeddings`,
  `internal/arcadedb`, `cmd/arcadedb-mcp`, `cmd/aura` (WSL).
- [ ] **Step 5: Commit** `refactor(embeddings): read a route's credential with the call's context, and fail loudly`.

### Task 3: the daemon's embedders and the embedding page on the helper

**Files:**
- Create: `cmd/aura/service_credentials.go`, `cmd/aura/service_credentials_test.go`
- Modify: `cmd/aura/memory_embedder.go`, `memory_embedder_test.go`, `document_retrieval_wiring.go`,
  `document_retrieval_wiring_test.go`, `chat_boot.go`, `runtime_tool_handles.go` (comment),
  `serve_embedding_route.go`, `serve_settings.go`, `serve_settings_test.go`, `doctor_embed_probe.go` and its
  test; `internal/config/config_routes.go` (`EmbedRoute` method goes)

**Interfaces:**
- Consumes: `settings.Credentials` (Task 1), `embeddings.CredentialFunc` (Task 2).
- Produces:
  - `func serviceCredentials(cfg *config.Config, pool *pgxpool.Pool) settings.Credentials`: the settings store
    over `pool` and `cfg.AuthulaSecret`, `LookupEnv: os.LookupEnv`. A nil pool or a store that cannot be
    built yields env-only `Credentials` and one `slog.Warn` naming why.
  - `chatEnv.credentials settings.Credentials`, set in `bootChatEnv`.
  - `func memoryEmbedder(cfg *config.Config, creds settings.Credentials) arcadedb.DenseEmbedder`
  - `func newQueryEmbedder(cfg *config.Config, creds settings.Credentials) documents.QueryEmbedder`
  - Both pass `func(ctx) { return creds.EmbedKey(ctx, cfg.Embed) }`.
- Removed: `liveKey`, `wireDocumentQueryEmbedder` (the retriever is built with the helper from the start),
  `primaryLLMRouteReloader.EffectiveValue`'s `OPENROUTER_API_KEY` case, `config.(*Config).EmbedRoute`.

- [ ] **Step 1: Write the failing tests**, replacing `TestMemoryEmbedderReadsTheRotatedKey` and
  `TestDocumentQueryEmbedderReadsTheRotatedKey` (they pinned the chat profile as the source, which the spec
  retires; say so in the commit body):
  - `TestMemoryEmbedderReadsTheEndpointKeyOnEveryCall` and `TestDocumentQueryEmbedderReadsTheEndpointKeyOnEveryCall`:
    endpoint route at an `httptest` server, fake store with `OPENROUTER_API_KEY="services"` and
    `AURA_EMBED_CLOUD_API_KEY="k1"`; embed, change the endpoint key to `"k2"`, embed → `Authorization` headers
    `["Bearer k1", "Bearer k2"]`; `"services"` never seen.
  - `TestQueryEmbedderNamesTheOpenRouterSpaceWithTheStoredServicesKey`: `Embed{CloudModel: "perplexity/pplx-embed-v1-4b", Dimensions: 768}`,
    store with only the services key → `Space` succeeds; empty store and no env → `ErrNoCredential`. (A hosted
    space needs no network, so no server.)
  - `TestEmbeddingRouteProbeSendsTheRoutesOwnKey`: the `embeddingRoutes.Probe` of a manual endpoint with no
    endpoint key and a services key stored → `ErrNoCredential` (the page shows `key_missing`).
  - `TestSettingsReportAnEnvironmentServicesKeyOnAChatGPTRoute`: a `primaryLLMRouteReloader` whose runtime holds a
    ChatGPT profile → `EffectiveValue("OPENROUTER_API_KEY")` returns `ok == false`, so the list falls back to
    the environment.
  - `TestDoctorProbeNamesAManualEndpointWithoutItsKey`: endpoint route, services key only → the embed probe
    fails naming `AURA_EMBED_CLOUD_API_KEY`.
- [ ] **Step 2: Run, expect FAIL**: `wsl -e bash -lc 'cd /mnt/d/Aura && go test ./cmd/aura/ -run "Embedder|Probe|Settings|Doctor"'`
- [ ] **Step 3: Implement** the interfaces above. `bootChatEnv` builds `serviceCredentials(cfg, pool)` once and
  passes it to `memoryEmbedder`, `newQueryEmbedder` (classifier and `chatEnv.documentEmbedder`) and
  `chatEnv`; `newHostDocumentRetriever` builds its own from `(cfg, pool)`. `wireEmbeddingRoutes` gives the
  probe `creds.EmbedKey(ctx, embed)` for the route being probed and the compatibility check
  `creds.ServicesKey`. The doctor's embed probe opens `settingsListerForCLI`'s store as the `SecretReader`
  and reports the missing key by its setting name.
- [ ] **Step 4: Run, expect PASS**; `go vet ./... && go build ./...`; `go test -race ./cmd/aura/` (WSL).
- [ ] **Step 5: Commit** `fix(aura): the embedders take the services key from the settings, not the chat profile`.

### Task 4: speech and vision on the helper

**Files:**
- Modify: `internal/multimodal/stt.go`, `tts.go`, `vision.go`, `runtime_config.go` and their tests;
  `internal/channels/telegram/sidecar.go`, `tts.go` and tests; `internal/config/config_routes.go`
  (`SpeechCloudRoute` goes); `cmd/aura/serve_voice.go`, `serve_channels.go`, `document_processor_wiring.go`
  and the asset service that calls them; `cmd/aura-media-index/main.go`

**Interfaces:**
- Consumes: `settings.Credentials` (Task 1); `chatEnv.credentials` and `serviceCredentials` (Task 3).
- Produces:
  - `STTConfig.OpenRouterKey`, `TTSConfig.OpenRouterKey`, `telegram.MultimodalConfig.OpenRouterKey`,
    `VisionConfig.PrimaryKey`, each `func(context.Context) (string, error)`; nil sends no key.
  - `func STTConfigFrom(cfg *config.Config, servicesKey func(context.Context) (string, error)) STTConfig`
  - `func TTSConfigFrom(cfg *config.Config, format string, servicesKey func(context.Context) (string, error)) TTSConfig`
  - `func VisionConfigFrom(cfg *config.Config, primaryAcceptsImages bool, primaryKey func(context.Context) (string, error)) VisionConfig`
  - The cloud speech base is `llm.DefaultBaseURL`, set by `STTConfigFrom`/`TTSConfigFrom`.
- Callers pass `creds.ServicesKey` to speech and `func(ctx) { return creds.PrimaryRouteKey(ctx, cfg.LLM.Provider, cfg.LLM.BaseURL) }`
  to vision. `aura-media-index` builds `settings.Credentials{Store: store, LookupEnv: os.LookupEnv}` from the
  store it already opens and stops writing the services key into `cfg.LLM.APIKey`.

- [ ] **Step 1: Write the failing tests:**
  - `TestSTTCloudSendsTheKeyReadAtRequestTime` and `TestTTSCloudSendsTheKeyReadAtRequestTime`: the key func
    returns `"k1"` then `"k2"` → two requests carry `Bearer k1`, `Bearer k2`.
  - `TestSpeechKeyReadErrorFailsTheRequest`: the key func fails → `Transcribe`/`Synthesize` return that error and
    the server sees no request.
  - `TestVisionSendsNoKeyToANonOpenRouterPrimary`: `VisionConfigFrom` over `LLM{Provider: "ollama", BaseURL: server.URL}`
    with images accepted and a services key available → the request has no `Authorization` header.
  - `TestVisionSendsTheServicesKeyToAnOpenRouterPrimary`: the same over the OpenRouter provider and base →
    `PrimaryKey` returns the services key.
  - `TestTelegramTTSUsesTheServicesKeyFunc`: `newTTSClient` with `OpenRouterKey` returning `"k"` → `Bearer k`.
- [ ] **Step 2: Run, expect FAIL**: `wsl -e bash -lc 'cd /mnt/d/Aura && go test ./internal/multimodal/ ./internal/channels/telegram/'`
- [ ] **Step 3: Implement.** The request builders read the key with the request's `ctx`; `route()` methods take
  `ctx` and return the read error. Adapt the callers named above.
- [ ] **Step 4: Run, expect PASS**; `go vet ./... && go build ./...`; `-race` on `internal/multimodal`,
  `internal/channels/telegram`, `cmd/aura`, `cmd/aura-media-index` (WSL).
- [ ] **Step 5: Commit** `fix(multimodal): speech and vision take the services key from the settings, and only to OpenRouter`.

### Task 5: the manual endpoint's key in the cockpit

**Files:**
- Modify: `internal/agui/settings_api_authz.go` (`callTimeSettingKeys`), `internal/agui/settings_api_test.go`;
  `web/src/settings/modelSettingsDefs.ts`, `web/src/settings/EmbeddingBackendControl.tsx`,
  `web/src/i18n/resources.settings.ts` (IT and EN), `web/src/settings/__tests__/ModelSettingsPanel.voice.test.tsx`
  (or the embedding control's own test file)

**Interfaces:**
- Consumes: `AURA_EMBED_CLOUD_API_KEY` in `settings.AllowedKeys` (Task 1).
- Produces: a `SettingDef` `{ key: 'AURA_EMBED_CLOUD_API_KEY', kind: 'string', secret: true, labelKey:
  'settings.fields.embedCloudApiKey', helpKey: 'settings.help.embedCloudApiKey' }` in `EMBEDDING_SETTINGS`,
  rendered only in the manual arm, after the URL and the model.

- [ ] **Step 1: Write the failing tests:**
  - Go, `TestSettingsListNeverCarriesTheEndpointKey`: a stored `AURA_EMBED_CLOUD_API_KEY` row → the list item has
    `has_value: true`, `value: ""`, `applied: "live"`.
  - vitest: the manual arm shows the key field; Locale and OpenRouter do not; typing a key and saving sends it
    through the generic settings save (`PUT /api/settings/AURA_EMBED_CLOUD_API_KEY`), not the route apply;
    an untouched empty box sends nothing.
- [ ] **Step 2: Run, expect FAIL**: `go test ./internal/agui/ -run SettingsList` (WSL);
  `cd web && npx vitest run src/settings` (Windows).
- [ ] **Step 3: Implement.** Add the key to `callTimeSettingKeys` (the daemon reads it on every call); add the
  def; render it in the `choice === 'manual'` `SettingsFields` with the existing secret field; Italian label
  "Chiave API dell'endpoint", English "Endpoint API key"; help says it is sent only to the endpoint above.
- [ ] **Step 4: Run, expect PASS**; `npx tsc -b`; `npx oxlint`; the web gates the pre-push runs.
- [ ] **Step 5: Commit** `feat(cockpit): a manual embedding endpoint takes its own key`.

### Task 6: gates, the lab VM, and the record

**Files:**
- Create: `docs/superpowers/verification/2026-10-23-cloud-service-credentials.md`
- Modify: `prd.md` (the 2026-09-27 paragraph on cloud services: a dated note), 
  `docs/superpowers/specs/2026-09-23-embedding-model-change-design.md` (§5 "The key is read live": amended,
  pointing to this spec), this plan's Status line, `scripts/coverage_package_policy.json` if a pinned
  denominator moved.

- [ ] **Step 1:** `make quality` in WSL; the coverage gate `bash scripts/coverage_docker.sh` with the package
  policy (update an exact baseline only when the measured denominator moved, with its numbers in the commit).
- [ ] **Step 2:** Push from WSL with the recipe in `CLAUDE.md`; CI, Skills and CodeQL green on the pushed SHA;
  "Publish Aura edge image" green; the VM's updater applies it (`docker inspect aura` revision).
- [ ] **Step 3: Measure on the VM, chat on ChatGPT** (acceptance 7 and 8):
  - `GET /api/settings/embedding-space`: the space named, no error;
  - one `document_search` through `aura docs mcp` in the `aura` container: `status` is not `lexical_only`;
  - the aura log names no `retrieval degraded` with `query_embedding_unavailable`.
- [ ] **Step 4:** Write the verification file: what was measured, and what it does not show (the Ollama vision
  leak is read in the code, not measured; no Mistral run). Add the PRD note and the 2026-09-23 amendment.
  Set this plan's Status.
- [ ] **Step 5: Commit** `docs: the cloud service credentials, measured on the lab VM`. Task 13 step 7 of
  `2026-10-18-document-fields.md` then runs on this build.

## Decisions taken while writing this plan

- **`settings.Credentials` lives in `internal/settings`**, beside `EmbedRoute`, the one place every process
  already reads its route from. `internal/multimodal` and `internal/embeddings` take plain functions, so neither
  imports `settings`.
- **The retriever stops being re-wired at chat boot.** It was built before the LLM runtime existed and re-wired
  once the runtime could give the key live; with the key in the settings, it is built right the first time.
- **Speech keeps sending an unauthenticated request when no key is set**, as today; only embeddings refuse up
  front, because only they have a cockpit gate that names the missing key.

## File structure

| File | Responsibility |
|---|---|
| `internal/settings/credentials.go` | the one reader of the cloud credentials, by service and by route |
| `internal/embeddings/{client,route,compat}.go` | the credential read with the call's context |
| `cmd/aura/service_credentials.go` | the daemon's `Credentials` over its settings store |
| `internal/multimodal/{stt,tts,vision,runtime_config}.go` | speech and vision read the key at request time |
| `web/src/settings/EmbeddingBackendControl.tsx` | the manual arm's key field |
