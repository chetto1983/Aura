# Image and Video Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let Aura generate and edit images and generate videos from text or images using the operator's selected OpenRouter models, with identity-owned billing, durable video jobs, and native cockpit and Telegram delivery.

**Architecture:** Keep two thin native tools in `internal/agent/tools`; put provider requests, catalog normalization, parameter clamping, and video supervision in `internal/mediagen`. Reuse the installed OpenAI Go SDK, the identity LLM resolver, identity-scoped assets, sqlc transactions, artifact events, and the existing conversation completion dispatcher. Render media through the existing trusted `local_artifact` path and installed assistant-ui elements.

**Tech Stack:** Go 1.27.1; openai-go/v3 v3.61.0; pgx/v5, sqlc v1.31.1 and golang-migrate; Postgres and Garage; React 19, TypeScript, assistant-ui, shadcn, Vitest, Stryker, Playwright; telebot.v4.

**Spec:** `docs/superpowers/specs/2026-09-14-image-video-generation-design.md`.

## Global Constraints

The following values and requirements come from the spec; they apply to every task.

- "Generation is an Aura capability, served by OpenRouter only. No local sidecar."
- "Two native Go tools, `image_generate` and `video_generate`, on the installed openai-go client. No MCP server, no hand-written HTTP client."
- "The operator picks the image and video model the way they pick the LLM; the agent never picks a model."
- "Every generation spends on the identity's own key through the same credit decision as its LLM turns."
- "Every file stays under 600 lines."
- "Both specs are `Deferred: true`".
- "`AURA_IMAGE_MODEL` (default `microsoft/mai-image-2.6`)".
- "`AURA_VIDEO_MODEL` (default `minimax/hailuo-3-max`)".
- "`AURA_VIDEO_INLINE_WAIT_SEC` (default 45, always inside the tool's context)".
- "`AURA_ASSET_MAX_VIDEO_BYTES`, default 52428800".
- Catalog cache: "5 minutes"; polling: "every 5 s"; job ceiling: "30-minute".
- "Content is downloaded only from the configured base URL (`videos/{id}/content`), never from `unsigned_urls`".
- "SVG stays download-only."
- "New strings in English and Italian."
- "Regeneration uses the message action bar's existing Reload."
- "WhatsApp is out of v1."
- Errors are `{error, message}` tool results, never Go errors: `no_key`, `no_credit`, `unsupported`, `asset_not_found`, `content_blocked`, `model_rejected`, `job_failed`, `job_expired`, `too_large`, `already_delivered`.
- "coverage at or above 85% across the tag matrix"; mutation "at or above 70%" on clamp and watcher state machine; "race detector and goleak clean"; "vitest at or above 85% and Stryker at or above 70% on the new web components."
- "Paid runs are batched and started only after a go, with the estimated cost stated first."

Project rules additionally require PRD evidence before implementation, WSL for quality gates, dynamically allocated migration numbers, package-local coverage policy, and atomic commits with a rationale and the repository's co-author convention. Documentation, generated sqlc output, and an installed registry file must follow the repository's existing file-size exemptions; do not introduce new exemptions for handwritten production code.

---

## Planning evidence and decisions to carry into execution

Planning inspected the worktree on 2026-09-14. No paid generation was performed. The generation measurements in the spec are prior evidence, not tests rerun during planning.

| Finding | Consequence |
|---|---|
| `internal/db/migrations/0020_assets.up.sql` restricts modality to document/image/audio/unknown. | Task 2 must migrate the assets CHECK, not just add a Go constant. |
| `internal/assets/ingest_agent.go` explicitly bypasses modality byte limits. | Both generated outputs and reference reads need bounded reads in the media path. Preserve existing `send_file` behavior. |
| `AgentIngestRequest.SourceRef` and the partial unique index already deduplicate agent assets. | Use stable `media-job:<id>` source references for watcher ingest; never create another asset when collecting. |
| `SnapshotFor` returns a credit-refusal snapshot with an empty key and the credit sentinel client, usually without an error. Local exemption can return a deployment snapshot. | Recognize cmd's existing `creditExhaustedClient` value before missing-key classification; reject local routes even if a process credential is present. |
| `main.go` is 595 lines, `config.go` and `agui/server.go` are 598. | Use embedded media configuration/handles and companion wiring files; split the relevant existing concern when touching would cross 600. |
| `attachToolArtifacts` reconstructs a card from `assets.tool_call_id`. | Bind the video asset to the actual collecting call, not automatically to the original submitting call. |
| `ToolFallback` runs grouping after trusted inline display dispatch. | Generation placeholders and content-filter cards must break tool groups or they render twice/disappear. |
| `pulseChatAction` already refreshes every four seconds; a separate turn-wide typing pulse already exists. | One action controller must arbitrate typing/upload actions; two independent tickers would overwrite each other. |
| `openai.NewClient` has retries and environment defaults. The existing compatibility client disables retries. | Explicit base URL/key override ambient credential defaults; disable POST retries. Test an ambient OPENAI_API_KEY cannot replace the selected identity key. |

### Provider and registry references

Read these again if the installed versions or wire contracts change:

- [OpenRouter Image API](https://openrouter.ai/docs/guides/overview/multimodal/image-generation): typed parameter descriptors, image-reference objects, and per-endpoint pricing lines.
- [OpenRouter Video API](https://openrouter.ai/docs/guides/overview/multimodal/video-generation): JSON submission, relative polling/content paths, frame-reference fields.
- [Video model catalog](https://openrouter.ai/docs/api/api-reference/video-generation/list-videos-models): model capabilities and SKU map.
- [assistant-ui Image](https://www.assistant-ui.com/elements/image) and [ImageGeneration](https://www.assistant-ui.com/elements/image-generation): owned registry elements, standalone props and compound components.
- [Telegram Bot API](https://core.telegram.org/bots/api#sending-files): multipart upload ceilings and native media methods.
- [Postgres row security](https://www.postgresql.org/docs/current/ddl-rowsecurity.html): use the repository's already measured owner/restrictive-policy pattern.

Free catalog GETs during planning found:

~~~json
{
  "image": {
    "id": "microsoft/mai-image-2.6",
    "reference_max": 5,
    "output_pricing": {"billable": "output_image", "unit": "token", "cost_usd": 0.000038}
  },
  "video": {
    "id": "minimax/hailuo-3-max",
    "durations": [5,6,7,8,9,10,11,12,13,14,15],
    "resolutions": ["768p","480p"],
    "frame_images": ["first_frame","last_frame"],
    "generate_audio": false,
    "pricing_skus": {
      "duration_seconds": "0.08",
      "duration_seconds_480p": "0.05",
      "duration_seconds_768p": "0.08"
    }
  }
}
~~~

These are fixture observations, never permanent production model constants. MAI does not expose a fixed price per image: show its reference limit and omit a fabricated per-image price. A synthetic per-image-priced fixture must exercise that label. For Hailuo the observed display range is $0.05–$0.08/s; actual billed cost still comes from `usage.cost`.

Registry measurement: `https://r.assistant-ui.com/image.json` exists; `image-generation.json` returns 404; `elements-image-generation.json` exists. Therefore Task 11 uses:

~~~sh
npx shadcn@latest add @assistant-ui/image @assistant-ui/elements-image-generation
~~~

This corrects an installation identifier; it does not replace the specified components. The generation element also installs `elements-surfaces`.

### Recovery boundary requiring an explicit resolution

The spec's "A paid clip is never lost, including across a restart" is stronger than its prescribed "submit, then insert" sequence. Neither a Go mutex nor `delivered_at` makes the external POST and Postgres transaction atomic. The inspected Video API documentation does not establish a supported idempotency/reconciliation mechanism.

The video implementation depends on resolving this boundary before Task 6. Independently, all branches must:

1. Disable automatic submission retries.
2. Persist a provider ID as soon as it is received, using daemon-owned cancellation and a bounded persistence operation.
3. Never treat a submission timeout as permission to submit again.
4. Keep already-ingested bytes in identity-owned storage when delivery fails.
5. Distinguish a successfully claimed Aura artifact from Telegram receipt; the latter remains the channel's existing best-effort external side effect.

Task 6 explicitly records which crash interval the guarantee covers. A successful normal submit/restart test is not proof of recovery from an ambiguous provider acceptance.

## File structure and ownership

Paths in this table are planned exact locations. Migration filename prefixes are resolved by the allocation commands in Tasks 2 and 6 when those tasks execute.

| Area | Create | Modify / reuse |
|---|---|---|
| Configuration and billing | `internal/config/config_media.go`, `internal/config/config_media_test.go`, `internal/mediagen/errors.go`, `internal/mediagen/ports.go`, `cmd/aura/media_credentials.go`, `cmd/aura/media_credentials_test.go`, `cmd/aura/media_settings.go`, `cmd/aura/media_settings_test.go` | `internal/config/config.go`, `internal/config/config_knobs.go`, `internal/settings/settings.go`, `internal/agui/settings_api_authz.go`, `cmd/aura/serve.go`, `prd.md`, `.env.example` |
| Video assets | `internal/assets/video_test.go`, `internal/assets/video_integration_test.go`; next migration pair `*_asset_video.*.sql` | `internal/assets/types.go`, `internal/assets/limits.go`, `cmd/aura/document_processor_wiring.go` |
| Catalog and clamp | `internal/mediagen/catalog.go`, `catalog_cache.go`, `catalog_price.go`, `clamp.go`, `types.go`, matching `*_test.go` | SDK `Client.Get`; no new provider library |
| Requests | `internal/mediagen/client.go`, `client_image.go`, `client_video.go`, `references.go`, matching `*_test.go`, `testdata/image_models.json`, `testdata/image_endpoints.json`, `testdata/video_models.json` | `assets.Service.OpenForIdentity` through composition-root port |
| Image tool | `internal/agent/tools/image_generate.go`, `image_generate_test.go`, `media_delivery.go`, `media_delivery_test.go`, `cmd/aura/media_assets.go`, `media_assets_test.go`, `media_registry.go`, `serve_media.go` | `cmd/aura/main.go`, `cmd/aura/serve.go`, `internal/agent/tools/send_file_ingest.go`, tool manifest/gateway tests |
| Video store | `internal/mediagen/job.go`, `store.go`, `store_delivery.go`, `store_test.go`, `store_integration_test.go`, `internal/db/queries/media_jobs.sql`; next migration pair `*_media_job.*.sql` | `internal/db/sqlc/models.go`, `querier.go`, generated `media_jobs.sql.go` |
| Watcher | `internal/mediagen/watcher.go`, `watcher_state.go`, `watcher_resume.go`, `watcher_test.go`, `watcher_state_test.go`, `watcher_resume_test.go` | Durable store and existing assets adapter |
| Completion dispatcher | `cmd/aura/background_completion.go`, `background_completion_test.go`, `background_completion_format.go`, `internal/agent/llm_agent_steer_media_test.go` | Rename/generalize `cmd/aura/shell_completion.go` and its tests; `internal/steer/inbox.go`, `internal/agent/llm_agent_steer.go`, `cmd/aura/serve_env.go`, `serve_lifecycle.go`, `serve.go`, `serve_media.go` |
| Video tool | `internal/agent/tools/video_generate.go`, `video_generate_test.go`, `video_generate_collect_test.go` | `media_delivery.go`, `media_registry.go`, `serve_media.go`, tool/gateway contracts |
| Model settings | `internal/agui/settings_media_models.go`, `settings_media_models_test.go`, `web/src/settings/mediaModelCatalog.ts`, `mediaModelCatalogFormat.ts`, `useMediaModelCatalog.ts`, corresponding `__tests__` | `internal/agui/server.go`, route/options files, `cmd/aura/serve_webui.go`, `web/src/settings/ModelPicker.tsx`, `useModelCatalog.ts`, `SettingField.tsx`, `ModelSettingsPanel.tsx`, `modelSettingsDefs.ts` |
| Media UI | `web/src/components/assistant-ui/elements/image.tsx`, `image-generation.tsx`, `surfaces.tsx` (registry); `web/src/chat/generation/GenerationFrame.tsx`, `generationState.ts`, `GenerationToolDisplay.tsx`; `web/src/chat/artifacts/renderers/GeneratedImagePreview.tsx`, `VideoPreview.tsx`; corresponding tests | `ExternalStoreChat_messages.tsx`, `toolGrouping.ts`, `displays/LocalArtifactDisplay.tsx`, `artifacts/artifactMeta.ts`, `artifacts/useBlobPreview.ts`, `artifacts/PreviewModal.tsx`, `routes/SharePage.tsx`, `web/src/i18n/resources.ts`, new `resources.media.ts`, `resources.settings.ts`, package lock |
| Telegram | `internal/channels/telegram/media_action.go`, `media_action_test.go`, `artifact_media_test.go` | `artifact.go`, `status_pane.go`, `bot_dispatch_turn.go`, `bot_typing.go` |
| Evidence and gates | `web/e2e/media-generation-live.spec.ts`, `web/e2e/media-generation-live.helpers.ts`, `docs/testing/image-video-generation.md` | `scripts/coverage_package_policy.json`, `scripts/critical_mutation_gate.py` and contract tests, `web/stryker.config.json`, `web/vitest.stryker.config.ts`, `.github/workflows/ci.yml`, `docs/aura-quality-snapshot.md` only after measurement |

The feature is one integrated subsystem: catalog, credentials and assets serve both tools; video adds durable execution. Keep one plan with independently testable tasks. Dependencies: 1 → 2/3 → 4 → 5; 2/4 → 6 → 7 → 8 → 9; 1/3 → 10; 5/9 → 11/12; all → 13.

## Execution conventions

- Run shell snippets in a WSL checkout, with `~/.local/bin` and `~/go/bin` on PATH. Preserve unrelated edits. Create any isolated worktree using the git-worktrees skill at execution time.
- Before each migration: inspect the directory and assign the next integer. The observed planning maximum (0126) is deliberately not an implementation filename.
- Each code task follows red → green → review → atomic commit. Test snippets below define required behavior; expand the listed case matrices with real fixtures. New named APIs are defined in the producing task's Interfaces block.
- After each Go editing step run `go vet ./...`, `go build ./...`, and focused package tests; before its commit run focused race tests. Do not declare a runtime task complete using only those checks.
- At task close follow CLAUDE.md's merge-to-master rule with fresh post-merge checks. Never force, discard unrelated changes, or merge a partial task. Push and inspect CI at phase close.
- Commit examples show subjects and rationale; append the actual repository co-author trailer rather than inventing an identity.
- Each shell block starts at the repository root unless it explicitly begins with `cd web`. Before staging, inspect the diff and select only that task's files/hunks; directory shorthand in commit examples must never include unrelated work.

### Task 1: Live media settings and identity credentials

**Files:** Create the configuration/billing files in the map; modify settings allowlist/authz, config catalog, serve resolver binding, PRD and env example. Test `internal/config/config_media_test.go`, `cmd/aura/media_credentials_test.go`, `cmd/aura/media_settings_test.go`, and extend `internal/agui/settings_api_authz_test.go`.

**Interfaces:**
- Consumes: `(*runner.IdentityLLMResolver).SnapshotFor(context.Context, string) (llm.RuntimeSnapshot, error)`, `(*settings.Store).List(context.Context) ([]sqlc.AuraSettings, error)`.
- Produces in `mediagen`: `MediaCredentials.For(ctx context.Context, identityID string) (baseURL, apiKey string, err error)`; `ModelSettings.Model(ctx context.Context, kind Kind) (string, error)`; `type Kind string` with `KindImage Kind = "image"` and `KindVideo Kind = "video"`.
- Produces in `config`: `MediaConfig{ImageModel, VideoModel string; VideoInlineWaitSec int; AssetMaxVideoBytes int64}`, embedded as `Config.Media MediaConfig`.
- Produces: `*mediagen.Error{Code, Message string}`, implementing `error`; `ErrorCode(error) string` returns an empty string for nil, the contained code for a media Error, or `job_failed` for an infrastructure failure.

- [x] **Step 1: Pin credential classification with a unit test.**

Define a local resolver stub; do not instantiate or compare a real secret in output.

~~~go
type snapshotFunc func(context.Context, string) (llm.RuntimeSnapshot, error)
func (f snapshotFunc) SnapshotFor(ctx context.Context, id string) (llm.RuntimeSnapshot, error) {
    return f(ctx, id)
}

func TestMediaCredentialsRefusesCreditSentinel(t *testing.T) {
    exhausted := creditExhaustedClient{}
    port := mediaCredentials{
        resolver: snapshotFunc(func(context.Context, string) (llm.RuntimeSnapshot, error) {
            return llm.RuntimeSnapshot{
                Client: exhausted,
                Config: llm.Config{BaseURL: "https://openrouter.ai/api/v1"},
            }, nil
        }),
    }
    base, key, err := port.For(context.Background(), "owner")
    if mediagen.ErrorCode(err) != "no_credit" || base != "" || key != "" {
        t.Fatal("credit refusal must return no usable credential")
    }
}
~~~

Add cases for `runner.ErrNoIdentityLLMKey`, `identitykey.ErrNoCredit`, nil resolver, empty owner, local URLs with a deployment key, valid owner key, and unrelated store failure. A non-credit infrastructure error is not a fabricated `no_credit`.

- [x] **Step 2: Run red.**

~~~sh
go test ./cmd/aura -run TestMediaCredentials -count=1
~~~

Expected: missing media credential types. Config tests must assert all four exact defaults and reject negative wait / nonpositive byte ceiling; allow wait zero for immediate detachment.

- [x] **Step 3: Implement the port and settings precedence.**

~~~go
type snapshotResolver interface {
    SnapshotFor(context.Context, string) (llm.RuntimeSnapshot, error)
}
type mediaCredentials struct {
    resolver snapshotResolver
}
func (p mediaCredentials) For(ctx context.Context, owner string) (string, string, error) {
    if p.resolver == nil || strings.TrimSpace(owner) == "" {
        return "", "", &mediagen.Error{Code: "no_key", Message: "No identity credential is available."}
    }
    snap, err := p.resolver.SnapshotFor(ctx, owner)
    _, noCredit := snap.Client.(creditExhaustedClient)
    if errors.Is(err, identitykey.ErrNoCredit) ||
        (err == nil && noCredit) {
        return "", "", &mediagen.Error{Code: "no_credit", Message: "This identity has no generation credit."}
    }
    if errors.Is(err, runner.ErrNoIdentityLLMKey) {
        return "", "", &mediagen.Error{Code: "no_key", Message: "Connect this identity to OpenRouter."}
    }
    if err != nil { return "", "", err }
    if llm.ReasoningTarget(snap.Config.Provider, snap.Config.BaseURL) != llm.ReasoningTargetOpenRouter ||
        llm.IsKeylessLocalBaseURL(snap.Config.BaseURL) ||
        strings.TrimSpace(snap.Config.APIKey) == "" {
        return "", "", &mediagen.Error{Code: "no_key", Message: "Generation requires the OpenRouter route."}
    }
    return snap.Config.BaseURL, snap.Config.APIKey, nil
}
~~~

Use the existing singleton `identityLLMResolver(chat)` in serve wiring for both Runner and media, so invalidation affects both. `cmd/aura/identity_llm_resolver.go` already creates the sentinel as `creditExhaustedClient{}`; the composition adapter recognizes that existing concrete type without adding a second resolver or changing runner internals. A nil resolver remains a real nil interface and refuses generation.

`mediaModelSettings.Model` reads the store on each call: nonempty stored row → boot environment value → specified default. A store error must surface, not silently switch models. Add both models to `AllowedKeys`, `adminOnlySettingKeys`, `callTimeSettingKeys`; register four config knobs. Use `loadMediaConfig() MediaConfig` in `config_media.go` so Config/Load stay at or below 600 lines.

- [x] **Step 4: Record evidence and test hot changes.** (scope per implementer-notes.md ruling R3: no PRD/`.env.example` edit — settings hot-change/reset/authz tests only)

Update the current PRD sections for tools, assets, model selection and configuration; record the prior measured image/video costs and what those tests did not establish. Include this plan's free-catalog measurements without claiming a new paid probe. The four env example rows are:

~~~dotenv
AURA_IMAGE_MODEL=microsoft/mai-image-2.6
AURA_VIDEO_MODEL=minimax/hailuo-3-max
AURA_VIDEO_INLINE_WAIT_SEC=45
AURA_ASSET_MAX_VIDEO_BYTES=52428800
~~~

Test saved value changes on the same settings-port instance, reset to default, member PUT/DELETE refusal and admin success without restart. Preserve the existing no-key/no-credit LLM refusal behavior.

- [x] **Step 5: Verify and commit.** (file set per implementer-notes.md: `internal/mediagen`, `internal/settings/settings.go`, `internal/agui/settings_api_authz*.go`, `cmd/aura/media_*.go` — no `config`, `serve.go`, `prd.md`, `.env.example`)

~~~sh
go test ./internal/mediagen ./internal/settings ./internal/agui ./cmd/aura -count=1
go test -race ./internal/mediagen/ ./internal/settings/ ./internal/agui/ ./cmd/aura/ -count=1   # WSL
git add internal/mediagen internal/settings/settings.go internal/agui/settings_api_authz.go internal/agui/settings_api_authz_test.go cmd/aura/media_credentials.go cmd/aura/media_credentials_test.go cmd/aura/media_settings.go cmd/aura/media_settings_test.go docs/superpowers/plans/2026-09-14-image-video-generation.md
git commit -m "feat(media): resolve live models and identity credentials" -m "Reuse the existing credit decision and record measured provider constraints before generation."
~~~

### Task 2: Persist and validate video assets

**Files:** Modify `internal/assets/types.go`, `limits.go`, `cmd/aura/document_processor_wiring.go`; create video unit/integration tests and the dynamically allocated `asset_video` migration pair.

**Interfaces:**
- Consumes: `Config.Media.AssetMaxVideoBytes`, `assets.Service.IngestAgentFile`.
- Produces: `assets.ModalityVideo`, `assets.Limits.MaxVideoBytes int64`, accepted MP4/WebM asset rows. Media producers remain responsible for size checks before the limit-bypassing agent ingest.

- [ ] **Step 1: Write boundary tests.**

~~~go
func TestVideoModalityAndLimits(t *testing.T) {
    limits := Limits{MaxVideoBytes: 50 << 20}
    for _, sample := range []struct{ name, mime string }{
        {"clip.mp4", "video/mp4"}, {"clip.webm", "video/webm"},
        {"clip.MP4", ""}, {"clip.webm", "application/octet-stream"},
    } {
        if got := InferModality(sample.name, sample.mime); got != ModalityVideo {
            t.Fatalf("%s: modality %q", sample.name, got)
        }
    }
    if err := limits.Validate(ModalityVideo, "clip.mp4", 50<<20); err != nil { t.Fatal(err) }
    if !errors.Is(limits.Validate(ModalityVideo, "clip.mp4", (50<<20)+1), ErrAssetTooLarge) {
        t.Fatal("one excess byte must be refused")
    }
    if !errors.Is(limits.Validate(ModalityVideo, "clip.avi", 1), ErrAssetUnsupported) {
        t.Fatal("unsupported extension accepted")
    }
}
~~~

Add MIME/extension mismatch, negative size, and unknown `video/*` cases. The upload pipeline must validate the accepted video MIME allowlist; never treat every video subtype as playable MP4.

- [ ] **Step 2: Run red and allocate the migration.**

~~~sh
go test ./internal/assets -run TestVideo -count=1
ls internal/db/migrations/ | tail -1
~~~

Use the next integer printed by the latter command when creating the files, padded to four digits.

- [ ] **Step 3: Implement modality and database constraint.**

~~~sql
-- *_asset_video.up.sql
ALTER TABLE aura.assets DROP CONSTRAINT assets_modality_check;
ALTER TABLE aura.assets ADD CONSTRAINT assets_modality_check
  CHECK (modality IN ('document','image','audio','video','unknown'));
~~~

~~~sql
-- *_asset_video.down.sql
-- A populated video store cannot be downgraded without an explicit data migration.
ALTER TABLE aura.assets DROP CONSTRAINT assets_modality_check;
ALTER TABLE aura.assets ADD CONSTRAINT assets_modality_check
  CHECK (modality IN ('document','image','audio','unknown'));
~~~

The down migration intentionally fails transactionally while video rows exist; do not delete or relabel user media. Add `ModalityVideo = "video"`, infer only mp4/webm, and mirror the existing image/audio limit cases. Wire `MaxVideoBytes: cfg.Media.AssetMaxVideoBytes` into the existing service.

- [ ] **Step 4: Verify the real database path.**

In `video_integration_test.go` (`//go:build db_integration`) use the package's existing DB fixture to call `IngestAgentFile` with a small real MP4 fixture, `ModalityVideo`, and a unique SourceRef. Read it with `GetForIdentity`; assert `accepted`, `video`, actual MIME, bytes and ownership. Test both allowed formats and denied cross-owner access. Run only on the disposable integration database.

~~~sh
go test ./internal/assets -run 'TestVideo|TestIngestAgentFile' -count=1
go test -tags=db_integration ./internal/assets -run TestVideo -count=1
go test -race ./internal/assets
~~~

- [ ] **Step 5: Commit.**

~~~sh
git add internal/assets/types.go internal/assets/limits.go internal/assets/video_test.go internal/assets/video_integration_test.go internal/db/migrations cmd/aura/document_processor_wiring.go
git commit -m "feat(assets): support bounded MP4 and WebM assets" -m "The SQL modality constraint must admit video together with its runtime validation."
~~~

### Task 3: Cached catalog, prices and deterministic parameter clamp

**Files:** Create `internal/mediagen/types.go`, `catalog.go`, `catalog_cache.go`, `catalog_price.go`, `clamp.go`, their tests and the three catalog JSON fixtures.

**Interfaces:**
- Produces `ImageInput{Prompt, AspectRatio string; ReferenceAssetIDs []string}`.
- Produces `VideoInput{Prompt string; Duration int; Resolution, AspectRatio, FirstFrameAssetID string; ReferenceAssetIDs []string; Audio *bool}`.
- Produces `Parameter{Type string; Values []string; Min, Max *int}`, `Model{ID string; Kind Kind; Parameters map[string]Parameter; Durations []int; Resolutions, AspectRatios, FrameImages []string; GenerateAudio bool; PricingSKUs map[string]string; ImagePricing []PriceLine}`, and `PriceLine{Billable, Unit, Variant string; CostUSD float64}`.
- `ClampImage(ImageInput, *Model) (ImageInput, []string)`; `ClampVideo(VideoInput, *Model) (VideoInput, []string, error)`.
- `Catalog.List(ctx, baseURL string, kind Kind, refresh bool) ([]Model, error)`; `Catalog.Find(ctx, baseURL string, kind Kind, id string) (*Model, error)`. Missing ID returns nil, nil; unavailable catalog returns an error.
- `ImagePrice([]PriceLine) (min, max float64, ok bool)`; `VideoPrice(map[string]string) (min, max float64, ok bool)`.

- [ ] **Step 1: Write clamp and price tests.**

~~~go
func TestClampVideoUsesNearestDeclaredValues(t *testing.T) {
    muted := false
    in := VideoInput{Prompt: "moving sea", Duration: 4, Resolution: "720p", AspectRatio: "3:2", Audio: &muted}
    model := Model{
        Durations: []int{5, 10}, Resolutions: []string{"480p", "768p"},
        AspectRatios: []string{"16:9", "1:1"}, GenerateAudio: false,
    }
    used, changes, err := ClampVideo(in, &model)
    if err != nil { t.Fatal(err) }
    if used.Duration != 5 || used.Resolution != "768p" || used.AspectRatio != "16:9" ||
        used.Audio != nil || len(changes) != 4 {
        t.Fatalf("unexpected clamp: %#v, %v", used, changes)
    }
}
func TestImagePriceDoesNotCallTokenPricingPerImage(t *testing.T) {
    _, _, ok := ImagePrice([]PriceLine{{Billable: "output_image", Unit: "token", CostUSD: .000038}})
    if ok { t.Fatal("token pricing must not become a per-image label") }
}
~~~

Add a property test with `testing/quick`: for nonempty supported duration sets the selected duration belongs to the set, and every already-supported duration is unchanged. Table-test reference truncation, unsupported/dropped ratios, no frame support, audio nil versus false, empty catalog entries, nil model, and input slice immutability.

- [ ] **Step 2: Run red.**

~~~sh
go test ./internal/mediagen -run 'TestClamp|TestImagePrice|TestVideoPrice' -count=1
~~~

Expected: undefined catalog/clamp functions.

- [ ] **Step 3: Implement pure selection and notes.**

~~~go
func nearestInt(want int, supported []int) int {
    best := supported[0]
    distance := func(v int) uint64 {
        if v >= want { return uint64(v) - uint64(want) }
        return uint64(want) - uint64(v)
    }
    for _, candidate := range supported[1:] {
        if distance(candidate) < distance(best) ||
            (distance(candidate) == distance(best) && candidate < best) {
            best = candidate
        }
    }
    return best
}
~~~

Normalize resolution heights as `480p=480,720p=720,768p=768,1080p=1080,1K=1024,2K=2048,4K=4096`. Ignore unparseable catalog candidates; retain exact matches; choose smaller height on a tie. Parse ratios as positive finite numerator/denominator and choose minimum absolute difference; choose smaller ratio on ties. Do not parse `auto` as a number.

For a known image model, missing parameter descriptors mean unsupported: drop requested ratio/references and explain each change. Truncate only the consumed references before loading assets. For a known video model, reject a requested first frame without `first_frame` support, drop undeclared audio, and clamp nonempty supported sets. The video catalog may not declare a reference maximum: in that case retain references for provider validation; do not invent an image-model limit. Missing model passes inputs through with no invented capabilities. Every dropped/clamped parameter produces one plain-language note containing requested and used values, never base64 bytes.

- [ ] **Step 4: Implement the cache and conservative pricing.**

~~~go
type catalogCacheKey struct { BaseURL string; Kind Kind }
type catalogCacheEntry struct { Models []Model; ExpiresAt time.Time }
const catalogTTL = 5 * time.Minute
~~~

Use one daemon-owned cache for tool clamp and settings lists, keyed by normalized base URL and kind. Serialize same-key refreshes; copy returned slices/maps so callers cannot mutate the cache. Tests inject `now func() time.Time` and a fetch function. A refresh bypasses the TTL; failures do not become empty successful catalogs or erase a still-valid entry. Public catalog GETs use no services credential. Endpoint pricing GETs use constructed relative SDK paths, not untrusted `endpoints` URLs. Bound a refresh by the existing twenty-second catalog timeout and at most four simultaneous endpoint-detail reads. Failed optional pricing enrichment leaves that row's price unknown; it does not erase known capabilities or pretend a paid model is free.

Image pricing considers only `billable=output_image, unit=image` finite nonnegative lines. For video accept finite nonnegative numeric values on `duration_seconds`/`duration_seconds_*` as USD/s and `cents_per_second`/`cents_per_second_*` divided by 100. Omit unknown/token/megapixel SKU units. Calculate min/max across eligible entries without multiplying or dividing by clip duration. Preserve zero as a real known price.

- [ ] **Step 5: Verify and commit.**

Cache tests assert one fetch within five minutes, refresh on expiry, explicit refresh, separated origins/kinds, failed fetch retry and concurrency under race. Price tests include `0.05–0.08`, zero, malformed/negative/NaN and multiple endpoints. Include the real observed token-priced image fixture.

~~~sh
go test ./internal/mediagen -count=1
go test -race ./internal/mediagen
git add internal/mediagen
git commit -m "feat(media): normalize model catalogs and clamp supported options" -m "Use provider capability data and explicit pricing units for both tools and the picker."
~~~


### Task 4: SDK request execution, references and provider errors

**Files:** Create the client and reference files/tests listed in the file map.

**Interfaces:**
- Consumes: catalog types and `MediaCredentials`; no direct settings/global credential lookup inside the client.
- `ReferenceReader.Open(ctx, identityID, assetID string) (io.ReadCloser, ReferenceMeta, error)`; `ReferenceMeta{MIMEType, Modality string; SizeBytes int64}`.
- `LoadReferences(ctx, reader ReferenceReader, owner string, ids []string, maxBytes int64) ([]ImageReference, error)`.
- `ImageReference{Type string; ImageURL ImageURL}`, `ImageURL{URL string}`, `FrameReference{Type string; ImageURL ImageURL; FrameType string}`, with snake_case JSON tags.
- `ImageRequest{Model, Prompt, AspectRatio string; References []ImageReference}`; `ImageResult{Bytes []byte; MIMEType string; CostUSD *float64}`.
- `VideoRequest{Model, Prompt string; Duration int; Resolution, AspectRatio string; FrameImages []FrameReference; InputReferences []ImageReference; GenerateAudio *bool}`; JSON field names are `duration`, `frame_images`, `input_references`, `generate_audio`, with absent optional fields omitted.
- `RemoteVideo{ID string; Status Status; CostUSD *float64; Error *Error}`; define `type Status string` in types.go with `StatusPending="pending"`, `StatusInProgress="in_progress"`, `StatusCompleted="completed"`, `StatusFailed="failed"`, `StatusExpired="expired"`, `StatusCancelled="cancelled"`, all typed Status.
- `Client.GenerateImage(ctx, baseURL, apiKey string, req ImageRequest) (ImageResult, error)`.
- `Client.SubmitVideo(ctx, baseURL, apiKey string, req VideoRequest) (RemoteVideo, error)`.
- `Client.GetVideo(ctx, baseURL, apiKey, providerID string) (RemoteVideo, error)`.
- `Client.DownloadVideo(ctx, baseURL, apiKey, providerID string, maxBytes int64) ([]byte, error)`.
- `NewClient(httpClient *http.Client, maxImageBytes int64) *Client`; `Client` contains `http *http.Client` and `maxImageBytes int64`. This is a small SDK caller, not a reimplementation of HTTP serialization.

- [ ] **Step 1: Write wire-contract tests with httptest.**

~~~go
func TestSubmitVideoSendsJSONAndAcceptsWhitespace(t *testing.T) {
    calls := 0
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        calls++
        if r.Method != "POST" || r.URL.Path != "/videos" ||
            !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
            t.Errorf("unexpected video request: %s %s", r.Method, r.URL.Path)
        }
        var body map[string]any
        if err := json.NewDecoder(r.Body).Decode(&body); err != nil { t.Error(err) }
        if body["model"] != "minimax/hailuo-3-max" || body["duration"] != float64(5) {
            t.Error("model/duration wire fields differ")
        }
        if _, found := body["generate_audio"]; found { t.Error("unsupported audio was sent") }
        w.Header().Set("Content-Type", "application/json")
        _, _ = io.WriteString(w, "\n\n {\"id\":\"vid_1\",\"status\":\"in_progress\"}\n")
    }))
    defer srv.Close()
    client := NewClient(srv.Client(), 25<<20)
    got, err := client.SubmitVideo(context.Background(), srv.URL+"/", "test-key",
        VideoRequest{Model: "minimax/hailuo-3-max", Prompt: "moving sea", Duration: 5})
    if err != nil || got.ID != "vid_1" || calls != 1 { t.Fatalf("submit: %v %#v", err, got) }
}
~~~

Add image tests that return actual small encoded PNG/JPEG/WebP bytes, extra `media_type` and `usage.cost`; inspect `aspect_ratio` and both kinds of references. A test server returning 500 on POST must receive exactly one request.

- [ ] **Step 2: Run red.**

~~~sh
go test ./internal/mediagen -run 'TestSubmitVideo|TestGenerateImage|TestLoadReferences' -count=1
~~~

- [ ] **Step 3: Implement calls using the installed SDK.**

~~~go
func (c *Client) sdk(baseURL, apiKey string) openai.Client {
    return openai.NewClient(
        option.WithBaseURL(strings.TrimRight(baseURL, "/")+"/"),
        option.WithAPIKey(apiKey),
        option.WithMaxRetries(0),
        option.WithHTTPClient(c.http),
    )
}
~~~

For images use `sdk.Images.Generate(ctx, openai.ImageGenerateParams{Model: openai.ImageModel(req.Model), Prompt: req.Prompt}, opts...)`; append `option.WithJSONSet("aspect_ratio", req.AspectRatio)` and `option.WithJSONSet("input_references", req.References)` only when present. Capture raw bytes with `option.WithResponseBodyInto(&raw)` and decode `data[].b64_json`, `media_type` and optional `usage.cost`. Require one usable output; do not emit a successful asset from an empty response. Pick extension from validated/sniffed MIME, including SVG as a downloadable asset. A missing MIME may be sniffed; unknown/contradictory media is refused.

For videos use `sdk.Post(ctx, "videos", req, &raw)`, `sdk.Get(ctx, "videos/"+url.PathEscape(id), nil, &raw)`, and `sdk.Get(ctx, "videos/"+url.PathEscape(id)+"/content", nil, &response)` where `response` is `*http.Response`. Close content bodies on every path and read at most `maxBytes+1`. Use these generic calls from the outset, avoiding the deprecated VideoService methods.

Validate opaque provider IDs (nonempty, no slash, backslash, dot-segment, query or fragment) before path construction. Configure the injected HTTP client's CheckRedirect to reject any origin change and test it: same-host subdomain redirects must not receive Authorization. Return `model_rejected` with a useful redacted upstream message for provider validation, `no_credit` for 402, `content_blocked` only for an explicit provider content-policy code, `job_failed` for remote failure and `job_expired` for expiry. Do not classify every 400 as policy blocking. Errors must not contain the key, image data or whole request bodies.

- [ ] **Step 4: Implement bounded owned-asset reads.**

~~~go
func readCapped(r io.Reader, maxBytes int64) ([]byte, error) {
    if maxBytes <= 0 || maxBytes == math.MaxInt64 {
        return nil, &Error{Code: "too_large", Message: "Invalid media byte limit."}
    }
    data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
    if err != nil { return nil, err }
    if int64(len(data)) > maxBytes {
        return nil, &Error{Code: "too_large", Message: "Media exceeds the configured byte limit."}
    }
    return data, nil
}
~~~

`LoadReferences` opens each selected asset under its owner, requires image modality and image MIME, enforces both declared and streamed sizes, closes the reader immediately after each image, and produces:
~~~json
{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aXWQAAAAASUVORK5CYII="}}
~~~
The first-frame object additionally contains `"frame_type":"first_frame"`. Foreign/deleted/missing IDs map to the same `asset_not_found` result; a video passed as an image maps to `unsupported`. Byte limits apply before base64 allocation; reject oversized image output before decode when the encoded length already exceeds the maximum possible valid encoding, then verify the decoded size.

- [ ] **Step 5: Verify and commit.**

Include missing/foreign assets, lying lengths, nil reader, early cancellation, non-image reference, content truncation, malformed base64/JSON, absent cost, explicit zero cost, 400, 402, policy refusal, 5xx, terminal statuses, same-origin content and malicious `unsigned_urls` tests. Set an unrelated ambient OPENAI_API_KEY in a test and assert Authorization is still the explicit identity key.

~~~sh
go test ./internal/mediagen -count=1
go test -race ./internal/mediagen
git add internal/mediagen
git commit -m "feat(media): execute image and video requests through the SDK" -m "Preserve extra response fields and bound owned reference and content reads without resubmitting paid work."
~~~

### Task 5: Discoverable image generation and artifact delivery

**Files:** Create `image_generate.go`, its tests, `media_delivery.go` and tests, `cmd/aura/media_assets.go` and tests, `media_registry.go`, `serve_media.go`. Modify `main.go`, `serve.go` and the existing built-in spec golden test/fixture. Reuse `ingestForDelivery`.

**Interfaces:**
- Consumes: `MediaCredentials`, `ModelSettings`, `ReferenceReader`, `Catalog.Find`, `Client.GenerateImage`, `tools.AssetDeliverer`.
- Produces `tools.ImageGenerate` with dependency fields `Credentials mediagen.MediaCredentials`, `Models mediagen.ModelSettings`, `Catalog *mediagen.Catalog`, `Client *mediagen.Client`, `References mediagen.ReferenceReader`, `Assets AssetDeliverer`, `MaxImageBytes int64`.
- `stageMedia(ctx context.Context, data []byte, mimeType string) (path, filename string, err error)`.
- `mediaArtifactResult(ctx context.Context, path, filename, mimeType, assetID, prompt string, size int64, preview any) ToolResult`.
- `mediaAssetAdapter` in cmd implements reference reads over `OpenForIdentity`; image delivery reuses `sendFileAssetAdapter`.

- [ ] **Step 1: Write the deferred specification contract.**

~~~go
func TestImageGenerateSpec(t *testing.T) {
    spec := (&ImageGenerate{}).Spec()
    if spec.Name != "image_generate" || !spec.Deferred || !spec.Mutating {
        t.Fatal("image generation must be a deferred, admitted mutation")
    }
    var schema map[string]any
    if err := json.Unmarshal(spec.Parameters, &schema); err != nil { t.Fatal(err) }
    props := schema["properties"].(map[string]any)
    if _, found := props["model"]; found { t.Fatal("agent cannot choose model") }
    if _, found := props["reference_asset_ids"]; !found { t.Fatal("missing editing references") }
    if _, err := OperationFingerprint(spec, json.RawMessage("{\"prompt\":\"a picture\"}")); err != nil {
        t.Fatal(err)
    }
}
~~~

Create an Execute test using `WithToolCallContext(identityctx.WithIdentityID(ctx, owner), "thread", "call-image", t.TempDir(), 8192)`, a fixture catalog/SDK server, an owned reference reader, and a recording `AssetDeliverer`. Assert the full artifact descriptor and parsed preview; no metadata must contain base64.

- [ ] **Step 2: Run red.**

~~~sh
go test ./internal/agent/tools -run 'TestImageGenerate|TestMediaDelivery' -count=1
~~~

- [ ] **Step 3: Implement schema and Execute pipeline.**

~~~go
return Spec{
    Name: "image_generate",
    Summary: "Generate an image or picture, or edit an image using reference assets.",
    Description: "Create one image from a prompt or edit supplied image assets. Use reference_asset_ids from attachments or previous generation results. The operator chooses the model. Read adjustments and errors; do not invent a model or a download URL.",
    Parameters: json.RawMessage(imageGenerateParameters),
    Deferred: true, Mutating: true,
    OperationScope: OperationScopeAgent,
    OperationNormalizer: OperationNormalizerCanonical,
    ReplayPolicy: ReplayToolResult,
}
~~~

`imageGenerateParameters` is a file-local constant containing the exact prompt/ratio/reference schema from the spec, `required:["prompt"]`, `additionalProperties:false`, and the seven allowed ratios. Refuse blank prompt, missing identity/call/thread, or absent dependencies before calling the provider. Malformed invocation produces `unsupported` with actionable text; operational failures map through `ErrorCode` to `errorResult`, with a nil Go error.

Execution order: credentials → live image model → catalog clamp → bounded reference read → SDK generation → stage → `ingestForDelivery` → artifact. Preflight the required delivery dependency before charging. A failed ingest is `job_failed` with no success artifact; unlike `send_file`, a newly paid generation must not silently claim successful cockpit delivery using a path-only descriptor.

- [ ] **Step 4: Implement staging, preview and wiring.**

~~~go
descriptor := map[string]any{
    "path": path, "filename": filename, "mime_type": mimeType,
    "asset_id": assetID, "caption": prompt,
    "tool_call_id": ToolCallIDFromContext(ctx), "size_bytes": size,
}
meta := ToolResultMeta{"artifact": descriptor}
encoded, err := json.Marshal(preview)
if err != nil { return errorResult("job_failed", "Cannot encode the generation result.") }
return ToolResult{Preview: string(encoded), Bytes: len(encoded), Meta: &meta}
~~~

`preview` is a typed media result with `asset_id,mime_type,model,cost_usd,used,adjustments`. Keep the original prompt as caption; channel-specific sanitizing stays in the channel.

`stageMedia` gets the private tool context inside the tools package; use `os.MkdirTemp(tc.runDir, "media-")`, a fixed basename plus MIME-derived extension, directory mode 0700 and file mode 0600. No caller-controlled file path. Validate the run directory exists and stays under its configured root. Remove partial files on failure. Successful paths must remain until asynchronous channel delivery has consumed them; use the existing run-directory retention lifecycle, not `defer os.Remove` inside Execute.

Add `mediaToolHandles` as an embedded field in `runtimeToolHandles`, and one registration helper call in `buildBaseRegistryWithHandles`. All registry variants retain a discoverable tool with nil dependencies on static/manifest paths; execution then returns a useful error. Serve injects live dependencies after asset construction and before accepting turns.

- [ ] **Step 5: Verify discovery and replay contracts, then commit.**

Tests cover all descriptor keys, MIME extensions, UTF-8 prompt, clamping notes, nil dependencies, no-key/no-credit zero outbound requests, dropped references never opened, ingest failure, strict run-root staging, and standard operation metadata. Extend tool-search tests using "image", "picture", "edit photo". Add the new tool to `builtinTools()` in `builtin_spec_golden_test.go`; this repository's "golden" is a static well-formedness sweep, not a generated snapshot to update.

~~~sh
go test ./internal/agent/tools ./internal/gateway ./cmd/aura -count=1
go test -race ./internal/agent/tools ./cmd/aura
git add internal/agent/tools cmd/aura/main.go cmd/aura/media_assets.go cmd/aura/media_assets_test.go cmd/aura/media_registry.go cmd/aura/serve_media.go cmd/aura/serve.go
git commit -m "feat(tools): generate images as identity-owned artifacts" -m "Reuse delivery ingestion and deferred discovery while enforcing the operator-selected model."
~~~


### Task 6: Identity-scoped job store and atomic delivery claims

**Files:** Create `internal/mediagen/job.go`, `store.go`, `store_delivery.go`, `store_test.go`, `store_integration_test.go`, `internal/db/queries/media_jobs.sql`, and the allocated `media_job` migration pair. Regenerate sqlc. Add the assets-delivery binding query in `media_jobs.sql`, so claim and binding use one identity transaction.

**Interfaces:**
- `Job{ID, IdentityID, ConversationID, ToolCallID, ProviderJobID, Model string; Request json.RawMessage; Status Status; Error *Error; AssetID string; CostUSD *float64; CreatedAt, UpdatedAt time.Time; CompletedAt, DeliveredAt *time.Time}`.
- `NewStore(pool *pgxpool.Pool) *Store`.
- `Store.Insert(ctx context.Context, job Job) (Job, error)`; `Store.Get(ctx, ownerID, jobID string) (Job, error)`.
- `Store.Recoverable(ctx, ownerID string) ([]Job, error)`.
- `Store.Progress(ctx, ownerID, jobID string, status Status, cost *float64, failure *Error) (Job, error)`.
- `Store.Complete(ctx, ownerID, jobID, assetID string, cost *float64) (Job, error)`.
- `Store.ClaimDelivery(ctx, ownerID, jobID, conversationID, deliveryCallID string) (Job, bool, error)`: atomically checks completed/undelivered and binds the existing asset to that delivery call. True means the caller won the claim.
- `JobStore` is the consumer-side interface containing these exact methods; `Store` satisfies it.

- [ ] **Step 1: Resolve the recovery acceptance boundary before implementing the store.**

Run a deterministic fault-injection experiment against the Task 4 SDK test server, which records accepted POSTs. Crash/abort at these boundaries: before provider acceptance, after acceptance before the response, after the response before Insert, after Insert, after asset ingest before Complete, and after ClaimDelivery before emitting the tool result. Record accepted requests, provider IDs, job rows, assets and transcript cards.

~~~text
fault                                  expected evidence
before acceptance                      no remote job, no charge claim
after acceptance / response lost       outcome unknown; never POST again automatically
response received / before Insert      remote ID known only to the interrupted process
after Insert                           same job resumed; one content download/asset identity
after ingest / before Complete         stable SourceRef reuses the same asset
after claim / before event              asset survives; external receipt is not proven
~~~

Use the user's recovery-scope choice to record the exact guarantee in the spec/PRD before code:
- A guarantee starting after successful Insert permits the implementation below and must explicitly exclude the ambiguous submission interval.
- An absolute guarantee requires documented and measured provider idempotency or job reconciliation plus a durable artifact-delivery protocol. The currently inspected contract does not establish these. Keep this task's implementation gate open until that evidence and a concrete revised design exist; do not silently add a retry loop or claim that a local outbox fixes an unknowable remote ID.

The claim schema below implements one winner per completed job, not transactional receipt by an external channel. Include the claim-to-event crash interval in that decision. This step is a bounded falsification experiment, not a paid provider test.

- [ ] **Step 2: Write RLS and competing-claim tests.**

In tagged integration tests open the already migrated disposable database with `pgxpool.New(ctx, os.Getenv("AURA_DB_URL"))`. Fail under CI when the env is absent; allow a local skip only when explicitly running without integration setup. Seed two unique identities with `INSERT INTO aura.identities(id,name,kind) VALUES($1,$2,'user')`; create one accepted video asset and completed job in A's `db.WithIdentityTxRaw`.

~~~go
var won atomic.Int32
var workers sync.WaitGroup
for i := 0; i < 2; i++ {
    workers.Go(func() {
        _, claimed, err := store.ClaimDelivery(ctx, ownerA, jobID, "thread-a", uuid.NewString())
        if err != nil { t.Error(err); return }
        if claimed { won.Add(1) }
    })
}
workers.Wait()
if won.Load() != 1 { t.Fatalf("delivery winners = %d", won.Load()) }
if _, err := store.Get(ctx, ownerB, jobID); !errors.Is(err, pgx.ErrNoRows) {
    t.Fatal("foreign job must look missing")
}
~~~

This assertion belongs in `TestMediaJobClaimsOnceAndIsolatesOwners`; define its setup in that file using the SQL above and migration schema below. Add direct raw SELECT/UPDATE/INSERT tests for absent and empty identity settings, so the test proves database policies rather than merely WHERE clauses.

- [ ] **Step 3: Allocate and implement schema.**

~~~sh
ls internal/db/migrations/ | tail -1
~~~

Create the next `*_media_job.up.sql` with:

~~~sql
CREATE TABLE aura.media_job (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  identity_id uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
  conversation_id text NOT NULL,
  tool_call_id text NOT NULL,
  provider_job_id text NOT NULL UNIQUE,
  model text NOT NULL,
  request jsonb NOT NULL,
  status text NOT NULL CHECK (status IN
    ('pending','in_progress','completed','failed','expired','cancelled')),
  error jsonb,
  asset_id uuid REFERENCES aura.assets(id),
  cost_usd numeric,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz,
  delivered_at timestamptz,
  CHECK (cost_usd IS NULL OR cost_usd >= 0),
  CHECK (status <> 'completed' OR asset_id IS NOT NULL),
  CHECK (delivered_at IS NULL OR status = 'completed')
);
CREATE INDEX media_job_recovery_idx ON aura.media_job(identity_id, created_at)
  WHERE status IN ('pending','in_progress') OR
    (status = 'completed' AND delivered_at IS NULL);
ALTER TABLE aura.media_job ENABLE ROW LEVEL SECURITY;
CREATE POLICY media_job_owner_isolation ON aura.media_job
  USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid)
  WITH CHECK (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
CREATE POLICY media_job_requires_identity ON aura.media_job
  AS RESTRICTIVE FOR ALL TO aura_app
  USING (NULLIF(current_setting('app.current_identity', true), '') IS NOT NULL)
  WITH CHECK (NULLIF(current_setting('app.current_identity', true), '') IS NOT NULL);
~~~

Follow the adjacent migration's grants/default-privileges convention; use the nonowner runtime pool for tests. The down migration drops this feature's table/index/policies only; never run it on a production database with paid jobs as an automatic rollback.

- [ ] **Step 4: Implement sqlc queries and scoped methods.**

~~~sql
-- name: GetMediaJobForIdentity :one
SELECT * FROM aura.media_job WHERE id = $1 AND identity_id = $2;

-- name: ListRecoverableMediaJobs :many
SELECT * FROM aura.media_job WHERE identity_id = $1
AND (status IN ('pending','in_progress') OR
     (status = 'completed' AND delivered_at IS NULL))
ORDER BY created_at, id;

-- name: ClaimMediaJobDelivery :one
UPDATE aura.media_job SET delivered_at = now(), updated_at = now()
WHERE id = $1 AND identity_id = $2 AND conversation_id = $3
  AND status = 'completed' AND asset_id IS NOT NULL AND delivered_at IS NULL
RETURNING *;

-- name: BindMediaJobAssetDelivery :execrows
UPDATE aura.assets SET tool_call_id = $3, updated_at = now()
WHERE id = $1 AND identity_id = $2 AND thread_id = $4
  AND source_kind = 'agent' AND status = 'accepted' AND deleted_at IS NULL;
~~~

Add InsertMediaJob, UpdateMediaJobProgress and CompleteMediaJob queries with explicit owner arguments. `Progress` changes only active rows, records cost whenever present (including zero), and sets `completed_at` on failed/expired/cancelled. `Complete` requires an owned accepted video asset and only updates active rows; set completed status after ingest succeeds, never on remote status alone.

All store calls go through `db.WithIdentityTx`; ClaimDelivery runs the two queries in one transaction and rolls back unless exactly one asset row is bound. Map a failed claim to false after looking up the owned row, preserving missing/status distinction. Do not overwrite the job's original `tool_call_id`; `assets.tool_call_id` is the actual delivery call.

The persisted request contains the clamped JSON body with reference data removed. Include asset IDs and the submission origin in a reserved `_aura` audit object; the SDK body builder never forwards this object. This lets resume refuse an origin switch without storing a key. Redact URLs containing user data, bearer credentials and all data URLs.

- [ ] **Step 5: Verify and commit.**

~~~sh
sqlc generate
go test ./internal/mediagen -count=1
go test -tags=db_integration ./internal/mediagen -count=1
go test -race -tags=db_integration ./internal/mediagen
git add internal/db/migrations internal/db/queries/media_jobs.sql internal/db/sqlc internal/mediagen
git commit -m "feat(media): persist scoped video jobs and delivery claims" -m "Recover paid jobs from durable IDs and atomically bind a single owned asset delivery."
~~~

Test actual status CHECKs, absent cost versus zero, duplicate provider ID, foreign asset, wrong conversation, rollback on failed asset binding, missing/empty RLS context, recovery ordering and retained original creation time.

### Task 7: Detached watcher and race-free ownership transfer

**Files:** Create `watcher.go`, `watcher_state.go`, `watcher_resume.go` and matching tests; add lifecycle fields in `job.go` only when needed.

**Interfaces:**
- `VideoAssets.IngestVideo(ctx context.Context, job Job, data []byte) (assetID string, err error)`; cmd adapter uses `IngestAgentFile` with stable `SourceRef: "media-job:"+job.ID`, `ModalityVideo`, and initially empty ToolCallID.
- `Completion{IdentityID, ConversationID, JobID string; Status Status}`.
- `WatcherOptions{PollInterval, MaxAge time.Duration; MaxVideoBytes int64; Now func() time.Time}`; production passes exactly five seconds and thirty minutes.
- `NewWatcher(parent context.Context, store JobStore, client *Client, credentials MediaCredentials, assets VideoAssets, notify func(Completion), opts WatcherOptions) *Watcher`.
- `Watcher.Track(job Job, inlineWaiter bool) *Waiter`: registration and waiter ownership are installed under one mutex before a polling goroutine starts. One active goroutine per job.
- `Waiter.Wait(ctx context.Context, duration time.Duration) (Job, bool)`: true means this waiter owns inline terminal handling; false means ownership was handed to the wake path.
- `Waiter.Release()`: idempotent; if an unacknowledged terminal completion exists, enqueue a wake exactly once.
- `Watcher.Resume(ctx context.Context, ownerID string) error`; `Watcher.Stop(ctx context.Context) error`.

- [ ] **Step 1: Write pure handoff-state tests before asynchronous code.**

Define an unexported `jobHandoff` with `waiting, terminal, claimed, notified bool`. Its API is `finish() bool` (returns whether to notify), `detach() bool` (returns whether to notify), and `claim() bool` (whether waiter owns delivery).

~~~go
func TestCompletionAtWaitBoundaryHasOneOwner(t *testing.T) {
    for _, finishFirst := range []bool{false, true} {
        state := jobHandoff{waiting: true}
        notices := 0
        if finishFirst {
            if state.finish() { notices++ }
            if state.detach() { notices++ }
        } else {
            if state.detach() { notices++ }
            if state.finish() { notices++ }
        }
        if notices != 1 || state.claim() {
            t.Fatalf("handoff lost or duplicated: %#v notices=%d", state, notices)
        }
    }
}
func TestInlineClaimSuppressesWake(t *testing.T) {
    state := jobHandoff{waiting: true}
    if state.finish() || !state.claim() || state.detach() {
        t.Fatal("inline completion acquired twice")
    }
}
~~~

Add completion-before-registration, duplicate Track, cancellation-after-finish-before-claim, release-after-failed-delivery, and repeated finish/release tests. These are the mutation targets.

- [ ] **Step 2: Run red, then implement the state machine.**

~~~sh
go test ./internal/mediagen -run 'TestCompletionAtWaitBoundary|TestInlineClaim' -count=1
~~~

~~~go
func (s *jobHandoff) finish() bool {
    s.terminal = true
    return s.notifyIfDetached()
}
func (s *jobHandoff) detach() bool {
    s.waiting = false
    return s.notifyIfDetached()
}
func (s *jobHandoff) notifyIfDetached() bool {
    if !s.terminal || s.waiting || s.claimed || s.notified { return false }
    s.notified = true
    return true
}
func (s *jobHandoff) claim() bool {
    if !s.terminal || !s.waiting || s.claimed || s.notified { return false }
    s.claimed = true
    s.waiting = false
    return true
}
~~~

In the real watcher all state methods run under the same mutex; notify callbacks and network/database work run after unlocking. A failed inline delivery before database claim explicitly returns ownership (`claimed=false`) before detach/notification. A completed database claim is never reset blindly.

- [ ] **Step 3: Implement polling and ingestion.**

~~~text
track:
  create done channel and waiter state under mutex
  install entry before spawning
poll:
  resolve current owner's credential
  require current origin == recorded submission origin
  GET videos/{providerID}
  preserve known usage.cost
  pending/in_progress -> persist progress; wait for next tick
  completed -> GET configured content path -> bounded read -> stable asset ingest
               -> persist Complete -> publish terminal -> close done once
  failed/expired/cancelled -> persist terminal code and cost -> publish terminal
  created_at + 30m exceeded -> persist expired -> publish terminal
shutdown:
  cancel only watcher context; leave active job rows recoverable; join goroutines
~~~

Implement the loop with `select` on daemon context and timer; bound every poll/download/persistence call by daemon context and remaining job lifetime. Poll immediately once, then five seconds between attempts. Network/429/5xx/download/object-store failures retry reads on the same provider ID until the original job deadline; no submission call exists in the watcher. Retain remote completion/cost while download is retried. A too-large clip becomes failed with `too_large` and recorded cost, even when download is refused from Content-Length.

At resume, a completed/undelivered job produces one completion notification; active rows rejoin the same watcher; the deadline is computed from stored CreatedAt. Failed/expired jobs do not become paid submissions. If the owner's key is temporarily unavailable or the configured origin changed, retain the row and retry within the ceiling, without sending an old job ID to a new origin.

- [ ] **Step 4: Test asynchronous behavior and recovery.**

Use injected timers or short options in httptest; use deterministic barrier channels for races, not multi-second sleeps. The fake store is thread-safe, copies rows, enforces the same terminal guards and counts Complete/Progress/Claim calls; it implements the full Task 6 JobStore interface in `watcher_test.go`.

Assertions: cancellation of the tool context does not stop the watcher; daemon cancellation does; terminal channels close once; no notification while an inline waiter owns the job; exactly one notification on timeout; repeated download failure never increments POST count; ingestion recovery reuses SourceRef; remote failure/expiry and local timeout preserve cost; no reader/goroutine leaks. Add `goleak.VerifyTestMain` once for the mediagen package.

- [ ] **Step 5: Verify and commit.**

~~~sh
go test ./internal/mediagen -count=1
go test -race ./internal/mediagen -count=1
git add internal/mediagen cmd/aura/media_assets.go cmd/aura/media_assets_test.go
git commit -m "feat(media): supervise durable video completion" -m "Transfer inline ownership under one lock and keep downloads independent of the originating turn."
~~~

### Task 8: Shared background dispatcher and boot lifecycle

**Files:** Rename/generalize shell completion code/tests into `background_completion.go`, `background_completion_test.go`; create `background_completion_format.go`. Modify `internal/steer/inbox.go`, `internal/agent/llm_agent_steer.go`, `cmd/aura/serve_env.go`, `serve_lifecycle.go`, `serve.go`, `serve_media.go`; create `internal/agent/llm_agent_steer_media_test.go`.

**Interfaces:**
- Reuse the existing `WakeWithSteer(context.Context,string,runner.SteerPusher,string,string) iter.Seq2[*agent.Event,error]`.
- `backgroundCompletion{OwnerID, ConversationID, Source, Line string}`.
- `backgroundCompletionDispatcher.NotifyShell(tools.BackgroundShellCompletion)` and `.NotifyMedia(mediagen.Completion)`.
- `newBackgroundCompletionDispatcher(parent context.Context, run backgroundCompletionWakeRunner, pusher runner.SteerPusher) *backgroundCompletionDispatcher`.
- `steer.SourceMedia = "media"`; same provenance/envelope rules as SourceShell.

- [ ] **Step 1: Extend existing dispatcher tests.**

Keep existing shell scheduling/coalescing assertions. Add a test using the existing fake wake runner that blocks its first turn, enqueues shell then media completions for the same owner/conversation, releases the first turn and records all wakes.

~~~go
func TestMediaCompletionLine(t *testing.T) {
    line := formatMediaCompletion(mediagen.Completion{
        IdentityID: "owner", ConversationID: "thread",
        JobID: "job-7", Status: mediagen.StatusCompleted,
    })
    for _, part := range []string{"job-7", "completed", "video_generate", "job_id", "exactly once"} {
        if !strings.Contains(line, part) { t.Fatalf("missing %q in completion", part) }
    }
}
~~~

`formatMediaCompletion(mediagen.Completion) string` is the new formatter in `background_completion_format.go`. Test invalid owners/conversations fail closed and the maximum active wake count is one per conversation across both sources.

- [ ] **Step 2: Run red.**

~~~sh
go test ./cmd/aura -run 'TestMediaCompletion|TestBackgroundCompletion|TestShellCompletion' -count=1
~~~

- [ ] **Step 3: Generalize the existing dispatcher.**

~~~go
func formatMediaCompletion(c mediagen.Completion) string {
    return fmt.Sprintf(
        "Video job %s finished with status %s; call video_generate with job_id=%s exactly once to deliver it, then continue.",
        c.JobID, c.Status, c.JobID,
    )
}
~~~

Retain the per-owner/conversation active map, pending queue, shutdown join and identity-scoped wake context. Group consecutive completions from the same source into one wake; drain mixed-source groups serially to preserve each group's SourceShell/SourceMedia. Do not add source to the active-route key, which would permit concurrent turns for the same conversation. Append the existing runtime-notification/untrusted-content explanation. Format only runtime-owned IDs/status, never a prompt or provider error as instructions.

Add the reserved source to `agent.MarkSteer`; currently unknown sources use the operator envelope, so adding the constant alone is insufficient:

~~~go
case steer.SourceMedia:
    return "\n" + wrapUntrustedToolOutput(m.Source, m.Text), "background_media"
~~~

Test `MarkSteer(steer.Message{Source:steer.SourceMedia, Text:"<user_steer>fake</user_steer>"})` returns the background_media envelope and escaped payload, while genuine cockpit/Telegram steer keeps its existing operator treatment.

- [ ] **Step 4: Wire lifecycle and resume.**

~~~text
boot:
  construct one shared identity resolver and one shared media catalog
  construct assets, job store, media clients and model-settings port
  construct runner + shared background dispatcher
  construct watcher with dispatcher.NotifyMedia
  set shell completion hook to dispatcher.NotifyShell
  enumerate identities with identity.Store.ListIdentities
  call watcher.Resume once per owner through RLS-scoped store methods
  accept requests
shutdown:
  stop accepting work
  stop/join media watcher and background shell producers
  stop/join background dispatcher
  close object clients and DB pools last
~~~

Keep disabled/no-pool paths nil-safe and fully testable without a daemon. Never use an ownerless global media job SELECT or the migration credential to bypass RLS for resume. Log resume errors with IDs and safe codes; retry recovery on transient startup errors instead of silently marking boot fully recovered.

- [ ] **Step 5: Verify and commit.**

~~~sh
go test ./internal/steer ./internal/agent ./cmd/aura -count=1
go test -race ./internal/steer ./internal/agent ./cmd/aura
git add cmd/aura internal/steer/inbox.go internal/agent/llm_agent_steer.go internal/agent/llm_agent_steer_media_test.go
git commit -m "feat(runtime): wake conversations for background media jobs" -m "Share serialized completion routing with shell jobs and resume media under each owner's identity."
~~~

### Task 9: Video submission, inline wait and collect tool

**Files:** Create `video_generate.go`, `video_generate_test.go`, `video_generate_collect_test.go`; modify `media_registry.go`, `serve_media.go`, `media_delivery.go` and manifest/gateway tests.

**Interfaces:**
- `tools.VideoGenerate` consumes the same credentials/models/catalog/client/reference fields as ImageGenerate plus `Jobs mediagen.JobStore`, `Watcher *mediagen.Watcher`, `VideoAssets mediagen.ReferenceReader`, `InlineWait time.Duration`.
- Input is Task 3 VideoInput plus `JobID string`; no model field.
- Output is either the media artifact preview, `{status,job_id,message,model,cost_usd,used,adjustments}`, or a spec error result.
- `stageExistingVideo(ctx, reader mediagen.ReferenceReader, owner, assetID string, maxBytes int64) (path, filename, mimeType string, size int64, err error)` stages the existing owned asset, with no second ingest.

- [ ] **Step 1: Write submit/collect tests.**

~~~go
func TestVideoGenerateSchemaAllowsCollectWithoutPrompt(t *testing.T) {
    spec := (&VideoGenerate{}).Spec()
    if !spec.Deferred || !spec.Mutating { t.Fatal("missing deferred mutation metadata") }
    var schema map[string]any
    if err := json.Unmarshal(spec.Parameters, &schema); err != nil { t.Fatal(err) }
    if _, exists := schema["anyOf"]; !exists { t.Fatal("prompt or job_id is required") }
    if _, exists := schema["properties"].(map[string]any)["model"]; exists {
        t.Fatal("model cannot be an agent argument")
    }
}
~~~

Build an Execute fixture from Task 4's httptest provider, Task 7's fake store and the real watcher. With a barrier-controlled completion, assert: one POST, persisted row before wait, artifact on early completion, no wake; with timeout, assert in_progress then one wake then a fresh collect call with one artifact. Repeat collect with a new tool-call ID and assert `already_delivered` and no provider request.

- [ ] **Step 2: Run red.**

~~~sh
go test ./internal/agent/tools -run TestVideoGenerate -count=1
~~~

- [ ] **Step 3: Implement submission.**

~~~json
{
  "type": "object",
  "properties": {
    "prompt": {"type":"string"},
    "job_id": {"type":"string"},
    "duration": {"type":"integer","minimum":1},
    "resolution": {"enum":["480p","720p","768p","1080p","1K","2K","4K"]},
    "aspect_ratio": {"enum":["16:9","9:16","1:1","4:3","3:4","3:2","2:3","21:9","9:21"]},
    "first_frame_asset_id": {"type":"string"},
    "reference_asset_ids": {"type":"array","items":{"type":"string"}},
    "audio": {"type":"boolean"}
  },
  "anyOf": [{"required":["prompt"]},{"required":["job_id"]}],
  "additionalProperties": false
}
~~~

Use the image tool's mutation metadata; video is not an action-key multiplexed tool, so do not mark Multiplexed. Summary: "Generate a video or animate an image; collect a completed video job." Give the description both submit and collect examples, the asynchronous return contract and instruction to wait for the runtime notification.

When job_id is present, choose collect before credentials/model/clamp and do not submit even if prompt is also supplied. Submission requires nonempty identity/thread/call and wired store/watcher before billing. Resolve credential/model, clamp, load first frame and references, call SDK once, persist the sanitized Job, then `waiter := Watcher.Track(job, true)`. Wait for the smaller of configured inline window and tool context; if it expires, Release and return the static in_progress result. The watcher's lifecycle never inherits the tool context.

If the remote submit already reports completed, persist an active job and let the watcher perform the same download/ingest path. A provider status of completed does not prove a usable Aura asset exists.

- [ ] **Step 4: Implement collect and stable artifact correlation.**

~~~text
Get(owner, job_id)
  missing / foreign / wrong conversation -> asset_not_found
  delivered_at non-null -> already_delivered
  pending / in_progress -> status result
  failed -> stored error.code, else job_failed
  expired -> job_expired
  cancelled -> job_failed with cancellation message
  completed ->
    OpenForIdentity + bounded restage of job.asset_id
    ClaimDelivery(owner, job_id, current conversation, current call)
    winner -> mediaArtifactResult with the SAME asset_id
    loser -> already_delivered
~~~

Collecting a finished job does not need a fresh paid credential or the current model; use the job's stored model/cost. Stage before claiming so a staging error leaves the job collectible. A failed inline attempt before claiming releases waiter ownership and triggers the wake path. Preserve original prompt and adjustments in the sanitized request so the artifact/result remain accurate after restart.

Pin the replay distinction: replaying the same operation does not resubmit; an explicit second collect with a new operation returns already_delivered. Replayed cached tool results must not create a second visible media artifact in the same message. Verify the existing gateway replay marker cannot invalidate JSON parsing: use the established result parsing helpers or strip only the exact runtime marker in the UI parser.

- [ ] **Step 5: Verify and commit.**

Include foreign job, wrong conversation, no-credit submit with zero HTTP, collect after credit removal, first-frame unsupported before asset load, original model after settings change, origin change, tool cancellation at handoff, failed staging, duplicate calls, provider timeout without re-POST, and restart fixtures.

~~~sh
go test ./internal/agent/tools ./internal/mediagen ./internal/gateway ./cmd/aura -count=1
go test -race ./internal/agent/tools ./internal/mediagen ./cmd/aura
git add internal/agent/tools cmd/aura/media_registry.go cmd/aura/serve_media.go
git commit -m "feat(tools): generate and collect durable video artifacts" -m "Use one completion owner and preserve the same asset across inline and resumed delivery."
~~~


### Task 10: Media model endpoints and one reusable picker

**Files:** Create `internal/agui/settings_media_models.go`, `settings_media_models_test.go`, `web/src/settings/mediaModelCatalog.ts`, `mediaModelCatalogFormat.ts`, `useMediaModelCatalog.ts`, `__tests__/mediaModelCatalog.test.ts`, `__tests__/mediaModelCatalogFormat.test.ts`, `__tests__/useMediaModelCatalog.test.ts`. Modify `internal/agui/server.go`, `settings_api.go`, `cmd/aura/serve_webui.go`, `serve_media.go`, and the settings frontend files in the map.

**Interfaces:**
- `agui.MediaCatalogLister.List(ctx context.Context, kind mediagen.Kind, refresh bool) ([]mediagen.Model, error)`, injected via `(*Server).SetMediaCatalog(lister MediaCatalogLister)` defined in settings_media_models.go, following the existing setter pattern. The composition adapter supplies the configured OpenRouter base URL.
- Responses are `{models: MediaCatalogModel[]}` with common `id,kind`; images add `reference_max,image_min_usd,image_max_usd,has_price`; videos add `duration_min,duration_max,resolutions,image_to_video,second_min_usd,second_max_usd,has_price`.
- `fetchMediaModels(kind: 'image'|'video', refresh?: boolean): Promise<readonly MediaCatalogModel[]>`.
- `useMediaModelCatalog(kind: 'image'|'video', enabled: boolean): ModelCatalogState<MediaCatalogModel>`.
- Generalize `ModelCatalogState<M = LLMCatalogModel>`; `ModelPicker<M extends {readonly id:string}>` accepts required `formatRow: (model:M, freeLabel:string) => string`.
- `imageModelMeta(model: ImageCatalogModel, labels: MediaLabels): string`; `videoModelMeta(model: VideoCatalogModel, labels: MediaLabels): string`. `MediaLabels` contains `references:(max:number)=>string`, `duration:(min:number,max:number)=>string`, and `imageToVideo:string`; the React caller builds it from `t` and tests provide explicit labels.

- [ ] **Step 1: Write formatter and picker tests.**

~~~tsx
const labels = {
  references: (max: number) => String(max) + ' reference images',
  duration: (min: number, max: number) => String(min) + '–' + String(max) + ' s',
  imageToVideo: 'Image-to-video',
};
it('shows per-second prices and never token prices as per-image prices', () => {
  expect(videoModelMeta({
    kind: 'video', id: 'minimax/hailuo-3-max', has_price: true,
    second_min_usd: .05, second_max_usd: .08, duration_min: 5, duration_max: 15,
    resolutions: ['480p', '768p'], image_to_video: true,
  }, labels)).toContain('$0.05–$0.08/s');
  const label = imageModelMeta({
    kind: 'image', id: 'microsoft/mai-image-2.6', has_price: false, reference_max: 5,
  }, labels);
  expect(label).toContain('5');
  expect(label).not.toContain('/image');
});
~~~

Extend ModelPicker tests: image/video row formatting, vendor grouping, custom entry, missing saved model, search/count, error and Refresh. Existing LLM tests must still show their exact context/token-price labels.

- [ ] **Step 2: Run red.**

~~~sh
cd web
npx vitest run src/settings/__tests__/mediaModelCatalogFormat.test.ts src/settings/__tests__/ModelPicker.test.tsx
~~~

- [ ] **Step 3: Implement handlers on the shared catalog.**

~~~go
mux.HandleFunc("GET /api/settings/image-models", s.handleListImageModels)
mux.HandleFunc("GET /api/settings/video-models", s.handleListVideoModels)
~~~

Mount both through `RequireCapability(..., governanceWriteCapability)` in the outer serve mux, matching llm-models. The handlers pass `refresh=1` as a force refresh and the fixed image/video kind to the injected catalog. They do not accept a provider-controlled or arbitrary client base URL. Use the saved runtime route; reject a local route with an actionable response. Do not fetch or return generation credentials to the browser. Map catalog failures to 502; empty lists remain successful empty lists only when actually returned by the provider.

Tests must exercise both inner routes and outer capability enforcement. Verify member model-row writes/deletes are refused even when the member has governance.write; admin changes are visible to the next tool call on the same daemon instance.

- [ ] **Step 4: Generalize picker data without duplicating its UI.**

~~~tsx
interface ModelPickerProps<M extends { readonly id: string }> {
  readonly id: string;
  readonly value: string;
  readonly catalog: ModelCatalogState<M>;
  readonly onChange: (value: string) => void;
  readonly formatRow: (model: M, freeLabel: string) => string;
}
~~~

Replace the direct `modelMeta` call inside `toModelOptions` with the formatter parameter; pass `modelMeta` explicitly for LLM rows. Add the two media SettingsKey values and cloud-only rows to the existing Models pane. Extend SettingsFields with per-key picker bindings rather than making all fields use the LLM catalog.

Define `ModelRow = LLMCatalogModel | MediaCatalogModel` and `PickerBinding{catalog: ModelCatalogState<ModelRow>; formatRow:(row:ModelRow, freeLabel:string)=>string}` for that heterogeneous map. The binding's formatter narrows `'kind' in row` before calling image/video formatters with its localized MediaLabels. The LLM route retains its existing useModelCatalog hook.

The media hook fetches only while cloud mode is active, uses AbortController/sequence invalidation on disable/unmount, and sends `refresh=1` for the Refresh button. Catalog errors do not disable saving a custom model ID. Add English/Italian labels for image model, video model, references, seconds and image-to-video in `resources.settings.ts`.

- [ ] **Step 5: Verify and commit.**

~~~sh
go test ./internal/agui ./cmd/aura -count=1
cd web
npx vitest run src/settings
npm run typecheck
npm run lint
git add src/settings src/i18n/resources.settings.ts
git add ../internal/agui ../cmd/aura/serve_webui.go ../cmd/aura/serve_media.go
git commit -m "feat(settings): select image and video models with capability prices" -m "Share cached model data with the tools while retaining one picker and live admin-controlled settings."
~~~

### Task 11: Generation frames and image/video previews

**Files:** Install the three registry files from the map. Create `generation/GenerationFrame.tsx`, `generationState.ts`, `GenerationToolDisplay.tsx`, `artifacts/renderers/GeneratedImagePreview.tsx`, `VideoPreview.tsx`, their tests and `i18n/resources.media.ts`. Modify `artifacts/useBlobPreview.ts` and tests to reuse `useAssetContent`, `ExternalStoreChat_messages.tsx`, `toolGrouping.ts`, `LocalArtifactDisplay.tsx`, `artifactMeta.ts`, `PreviewModal.tsx`, `SharePage.tsx`, localization aggregation and the npm lock.

**Interfaces:**
- `generationState(toolName:string, statusType:string|undefined, result:unknown): 'running'|'deferred'|'blocked'|'fallback'`.
- `generationArgs(argsText:string|undefined): {prompt:string; aspectRatio:string}`: total parser, safe default ratio "1 / 1".
- `GenerationFrame({prompt,aspectRatio,generating}: {prompt:string;aspectRatio:string;generating:boolean})`.
- `GenerationToolDisplay({toolName,argsText,statusType,result}: {toolName:string;argsText:string|undefined;statusType:string|undefined;result:unknown})`: presentation only; callers dispatch fallback for ordinary errors.
- `GeneratedImagePreview` and `VideoPreview` consume existing `RendererProps` and are default exports.
- Reuse `useBlobPreview(assetId:string,mimeType?:string): BlobPreview` rather than introducing another object-URL lifecycle helper. Refactor its fetch leg onto `useAssetContent(assetId,'blob')`, preserving abort/cleanup/stale-asset guarantees.

- [ ] **Step 1: Write state, grouping and preview tests.**

~~~tsx
it('a replayed detached video has a static frame', () => {
  expect(generationState('video_generate', 'complete',
    '{"status":"in_progress","job_id":"job-1"}')).toBe('deferred');
  render(<GenerationFrame prompt="A moving sea" aspectRatio="16 / 9" generating={false} />);
  expect(screen.getByText('Arriving in this chat')).toBeVisible();
  expect(screen.getByTestId('generation-frame')).toHaveAttribute('data-generating', 'false');
});
it('rejects executable or malformed ratio text', () => {
  expect(generationArgs('{"prompt":"<img onerror=alert(1)>","aspect_ratio":"url(evil)"}'))
    .toEqual({ prompt: '<img onerror=alert(1)>', aspectRatio: '1 / 1' });
});
~~~

Tests also assert SVG creates no img/video and keeps a download link; video controls/playsInline; blob cleanup; content-filter card with no src; image fullscreen keyboard focus/Escape; download URL uses asset ID; raw `path` never reaches the DOM. Test a deferred media result between other completed tools does not join or duplicate their ToolGroup.

- [ ] **Step 2: Install actual registry elements and run red.**

~~~sh
cd web
npx shadcn@latest add @assistant-ui/image @assistant-ui/elements-image-generation
npx vitest run src/chat/generation src/chat/artifacts src/chat/displays
~~~

Inspect registry diffs and package changes. Reuse existing helper/dependency versions; do not install another assistant runtime, new backend SDK or demo application. Split installed source by concern if necessary to satisfy the existing size gate.

- [ ] **Step 3: Implement explicit rendering states.**

~~~ts
export function generationState(
  toolName: string, statusType: string | undefined, result: unknown,
): 'running' | 'deferred' | 'blocked' | 'fallback' {
  if (toolName !== 'image_generate' && toolName !== 'video_generate') return 'fallback';
  if (statusType === 'running') return 'running';
  let parsed: unknown = result;
  if (typeof result === 'string') {
    try { parsed = JSON.parse(result); } catch { return 'fallback'; }
  }
  if (typeof parsed !== 'object' || parsed === null) return 'fallback';
  if ('error' in parsed && parsed.error === 'content_blocked') return 'blocked';
  if (toolName === 'video_generate' && 'status' in parsed && parsed.status === 'in_progress') {
    return 'deferred';
  }
  return 'fallback';
}
~~~

Apply the existing runtime replay-marker normalization before this parser when a replay marker is present. Treat partial/malformed args as empty prompt plus square frame; never render them as HTML. Map only enumerated aspect ratios to CSS values. Rendering order in ToolFallback:
1. Existing trusted inline display, including local_artifact.
2. Running/deferred generation frame or content-filter card.
3. Existing grouping and ToolActivityCard.

Use the same pure predicate in `isGroupableToolPart` to exclude deferred/blocked generation parts. Ordinary generation failures retain the existing error card. Do not recognize arbitrary tool results as a trusted DisplayPayload.

Extend the installed ImageGeneration element with `aspectRatio`, `label` and localizable text props. Apply aspect ratio to its inner visual frame, replacing the hard-coded aspect-square and false "1024 × 1024" label. Render prompt visibly as text while running and settled; omit its inert regenerate button, since message Reload already owns regeneration. A deferred result uses `generating=false` and a static arriving label; it is never an endless loading animation on replay. Preserve reduced-motion behavior.

- [ ] **Step 4: Implement previews and object-URL lifecycle.**

~~~tsx
export default function VideoPreview({ assetId, mimeType, fileName }: RendererProps) {
  const safeMime = mimeType === 'video/webm' ? 'video/webm' : 'video/mp4';
  const { url, error } = useBlobPreview(assetId, safeMime);
  if (error !== undefined) return <PreviewError detail={error} />;
  if (url === undefined) return <PreviewLoading />;
  return (
    <video
      src={url}
      controls
      playsInline
      preload="metadata"
      aria-label={fileName}
      className="max-h-[70vh] w-full rounded-lg bg-surface object-contain"
    />
  );
}
~~~

Generated MP4 blobs are relabeled `video/mp4`; keep WebM bytes labeled WebM for send_file parity. No autoplay. Use Image's standalone/compound elements for image preview and fullscreen. Extend owned `Image.Actions` with `downloadHref` and labels so the download action navigates to `/api/assets/{id}/download`, while copy uses the loaded image and reports clipboard failure. Do not fetch another unauthenticated provider URL.

Add `video` to PreviewKind; gate recognized video MIME/extensions after SVG rejection. LocalArtifactDisplay lazy-loads image and video next to HTML; retain its fallback file card. Add exhaustive video branches to PreviewModal and SharePage so the new union does not break other asset consumers. Public-share previews use the existing AssetSource provider, preserving token-scoped URLs.

Refactor `useBlobPreview` to create a URL only for the current `useAssetContent` blob, revoke it on replacement/unmount, and return no stale URL when assetId/MIME changes. Preserve image/PDF/share existing tests as regression coverage.

- [ ] **Step 5: Localize, verify and commit.**

Add English/Italian keys for generating image/video, arriving in this chat, provider-blocked media, zoom/close, download/copy/copied/copy failed and preview failure. Match cockpit CSS tokens, 44px action targets, keyboard focus and dark/light themes.

~~~sh
cd web
npx vitest run src/chat/generation src/chat/artifacts src/chat/displays src/chat/__tests__/toolGrouping.test.ts src/i18n
npm run typecheck
npm run lint
npm run build
git add src/components/assistant-ui src/chat src/routes/SharePage.tsx src/i18n package.json package-lock.json
git commit -m "feat(cockpit): render generation progress and inline media" -m "Keep artifact identity and replay semantics while adding accessible image and video previews."
~~~

### Task 12: Native Telegram media and coordinated upload actions

**Files:** Create `internal/channels/telegram/media_action.go`, `media_action_test.go`, `artifact_media_test.go`; modify `artifact.go`, `status_pane.go`, `bot_dispatch_turn.go`, `bot_typing.go`.

**Interfaces:**
- Reuse `botSender.Send` and `botNotifier.Notify`, installed `tele.Photo`, `tele.Video{Streaming:true}` and `tele.Document`.
- `artifactPayload(desc map[string]any) (any, bool)` returns a telebot sendable or the oversized-video cockpit text.
- `mediaActionController` tracks active tool-call IDs and computes current `tele.ChatAction`; `Start(callID,toolName string)`, `Finish(callID string)`, `Stop()`.
- Status consumer owns the controller during a turn; its action pulse replaces that turn's independent Typing pulse.

- [ ] **Step 1: Write dispatch table tests.**

~~~go
func TestArtifactMediaRouting(t *testing.T) {
    cases := []struct{ mime string; size int64; want string }{
        {"image/png", 10_000_000, "photo"},
        {"image/png", 10_000_001, "document"},
        {"video/mp4", 50_000_000, "video"},
        {"video/mp4", 50_000_001, "cockpit"},
        {"application/pdf", 100, "document"},
    }
    for _, tc := range cases {
        payload, ok := artifactPayload(map[string]any{
            "path": "/tmp/fixture", "filename": "fixture", "mime_type": tc.mime,
            "size_bytes": tc.size, "caption": "a test",
        })
        if !ok { t.Fatal("valid descriptor ignored") }
        got := "cockpit"
        switch payload.(type) {
        case *tele.Photo: got = "photo"
        case *tele.Video: got = "video"
        case *tele.Document: got = "document"
        }
        if got != tc.want { t.Fatalf("%s %d: %s", tc.mime, tc.size, got) }
    }
}
~~~

Use conservative decimal 10 MB/50 MB for Telegram; Aura's 50 MiB asset cap remains exactly 52428800. Thus a clip between 50,000,000 and 52,428,800 bytes can exist in the cockpit and must use Telegram's fallback. Use a real fixture when testing the Send seam; missing/lying descriptor size must be checked against os.Stat before choosing a native upload.

- [ ] **Step 2: Run red.**

~~~sh
go test ./internal/channels/telegram -run 'TestArtifactMedia|TestMediaAction' -count=1
~~~

- [ ] **Step 3: Implement native payload selection.**

~~~go
switch {
case strings.HasPrefix(mimeType, "image/") && size <= 10_000_000:
    payload = &tele.Photo{File: tele.FromDisk(path), Caption: caption}
case strings.HasPrefix(mimeType, "video/") && size <= 50_000_000:
    payload = &tele.Video{File: tele.FromDisk(path), Caption: caption, Streaming: true}
case strings.HasPrefix(mimeType, "video/"):
    payload = "Il video è disponibile nel cockpit."
default:
    payload = &tele.Document{File: tele.FromDisk(path), FileName: filename, Caption: caption}
}
~~~

Keep the existing best-effort Send result handling. Bound caption to Telegram's 1024-character limit after existing sanitization. Unknown or missing MIME stays document; preserve generic send_file descriptors. A provider SVG or format Telegram rejects as a photo may fall back to Document on a definite Bot API validation rejection; never retry ambiguous network failures as another send, which could duplicate a received message.

- [ ] **Step 4: Reuse the four-second pulse with one action owner.**

~~~text
RUN_STARTED -> typing
image_generate start -> upload_photo
video_generate start -> upload_video
generation result/end -> remove that call; use remaining action or typing
RUN_FINISHED/RUN_ERROR/channel close/context cancel -> stop and join
~~~

Use a mutex-protected active-call map; choose video over image if both are active. Start/Finish notify immediately when the selected action changes. Make the existing pulse read the controller's selected action at each tick, rather than adding a second ticker. Keep `pulseChatAction` as a fixed-action adapter for non-turn callers such as voice handling. The status consumer calls controller methods; the old full-turn typing pulse in bot_dispatch_turn must not compete.

Tests use a controllable pulse interval/clock and recording notifier. Assert immediate action switch, four-second refresh, default typing restored, independent call tracking, no uploads after terminal result, and Stop joins without leaks. Confirm AG-UI ToolCallEnd is emitted after execution (do not stop merely on streamed argument completion).

- [ ] **Step 5: Verify and commit.**

~~~sh
go test ./internal/channels/telegram -count=1
go test -race ./internal/channels/telegram
git add internal/channels/telegram
git commit -m "feat(telegram): deliver native generation media" -m "Choose photo/video uploads by actual size and keep a single coordinated chat-action pulse."
~~~


### Task 13: Coverage/mutation integration and real-agent acceptance

**Files:** Create `web/e2e/media-generation-live.spec.ts`, `web/e2e/media-generation-live.helpers.ts`, `docs/testing/image-video-generation.md`. Modify `scripts/coverage_package_policy.json`, `scripts/critical_mutation_gate.py`, `scripts/critical_mutation_gate_test.py`, `web/stryker.config.json`, `web/vitest.stryker.config.ts`; update existing CI wiring only where it does not already run these gates. Record measured results in PRD and the quality snapshot.

**Interfaces:**
- Live tests reuse `gotoAuthenticated(page, path)`, normal conversation creation, the composer and real agent runtime.
- Test-only activation: `AURA_E2E_MEDIA_GENERATION=1`. Document it in the test guide; it is not a production config knob.
- Helper `createMediaConversation(page: Page, title: string): Promise<string>`.
- Evidence records contain acceptance ID, exact commit, conversation ID, tool-call IDs, job/asset IDs, measured model/cost, status, media inspection notes and artifact paths. Never include keys or base64/reference contents.

- [ ] **Step 1: Extend executable coverage and mutation scope before claiming completion.**

~~~json
{"github.com/chetto1983/aura/internal/mediagen": {"mode":"target"}}
~~~

Merge that entry into the existing package-inventory object, preserving every current entry. Existing touched packages retain their actual floor/baseline contract; if their denominator changes, measure and update the policy deliberately, never lower a threshold just to pass.

~~~python
GO_SCOPES.update({
    "media_clamp": "internal/mediagen/clamp.go",
    "media_watcher": "internal/mediagen/watcher_state.go",
})
~~~

Extend mutation contract tests to require both scopes and fail for missing/zero executed mutants. The existing critical-mutation CI invocation picks up these entries; do not duplicate the runner. Add all new media components and formatter/state logic to Stryker's mutate list, and add their actual tests to `vitest.stryker.config.ts` (its include list is explicit). Include the owned registry image/generation customizations, generation grouping/state, GeneratedImagePreview and VideoPreview. Measure media frontend mutations separately at >=70%, in addition to the existing aggregate floor, so unrelated files cannot mask survivors.

- [ ] **Step 2: Write a real composer-driven image acceptance test.**

~~~ts
async function createMediaConversation(page: Page, title: string): Promise<string> {
  return page.evaluate(async (value) => {
    const response = await fetch('/api/conversations', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: value }),
    });
    if (response.status !== 201) throw new Error('Conversation creation failed');
    const row = await response.json() as { ID: string };
    return row.ID;
  }, title);
}
~~~

~~~tsx
test('the real agent generates a visible image', async ({ page }, info) => {
  test.setTimeout(300_000);
  await gotoAuthenticated(page, '/');
  const id = await createMediaConversation(page, 'Media generation acceptance');
  await gotoAuthenticated(page, '/c/' + encodeURIComponent(id));
  const input = page.getByRole('textbox', { name: 'Ask Aura', exact: true });
  await input.fill('Generate an image of a red wooden boat on a calm mountain lake, 16:9.');
  await input.press('Enter');
  await expect(page.getByTestId('generation-frame')).toBeVisible({ timeout: 90_000 });
  const image = page.locator('img[data-slot="image-preview"]').last();
  await expect(image).toBeVisible({ timeout: 180_000 });
  expect(await image.evaluate((el) => (el as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
  await info.attach('generated-image', { body: await page.screenshot(), contentType: 'image/png' });
});
~~~

Preserve the installed ImagePreview's `data-slot="image-preview"` on its img in Task 11. Save network/SSE trace evidence and assert `tool_search` actually promotes `image_generate`, then a corresponding call and artifact appear. A screenshot alone does not prove the real tool was called. Test helpers may read the trace/API and create/upload fixtures, but may not inject assistant messages, call the Go tool directly, or mock the generation response.

- [ ] **Step 3: Run all unpaid gates first.**

~~~sh
go vet ./...
go build ./...
go test ./internal/mediagen ./internal/agent/tools ./internal/assets ./internal/agui ./internal/channels/telegram ./internal/steer ./cmd/aura
go test -race ./internal/mediagen ./internal/agent/tools ./internal/assets ./internal/agui ./internal/channels/telegram ./internal/steer ./cmd/aura
bash scripts/coverage_docker.sh
make quality
make web-quality
make critical-mutation
~~~

Run the full tagged integration tier on the disposable DB, including media RLS, concurrent delivery and recovery. Prove tests executed: missing CI env is a failure, not green-by-skip. Verify sqlc sync and no new file over 600 LOC. Record combined Go coverage, package inventory and separate media mutation scores. Do not substitute a bare unit coverage percentage or average independent authorities.

The live suite must not run in ordinary unpaid CI jobs. When an explicitly approved paid acceptance run is configured, absence of its required runtime/auth settings must fail that run. A skipped paid suite is reported as not executed and does not close the feature.

- [ ] **Step 4: Prepare the paid batch and obtain the spec-required go.**

Prepare the running stack, test identities, browser login, Telegram test conversation, restart procedure and trace collection before asking to spend. Refresh free catalog prices. State the intended number of generations and estimated cost.

A batch satisfying five fresh conversations with both tools plus one edit and one extra animation uses approximately six images and six five-second 480p videos. Based solely on the spec's prior observations:
~~~text
6 × $0.038982 + 6 × $0.2475 = $1.718892 of generation spend
~~~
This excludes LLM turns and is not a price guarantee. State a concrete proposed budget at execution time using the current chosen models/options; image token billing and video parameters can change it. Reuse successful runs across acceptance cases; do not repeat paid work merely to rerun green checks.

The spec's explicit "started only after a go" governs this future paid batch. Writing this plan, fetching public catalogs and running mocked/unit tests does not authorize that spending or sending live Telegram messages.

- [ ] **Step 5: Execute and record the acceptance matrix.**

| Spec acceptance | Procedure | Required evidence |
|---|---|---|
| 1 | Open both catalogs; save/reset model as admin and attempt same as member. | Chosen models present; Hailuo $/s; no false MAI per-image rate; member denied; hot update. |
| 2 | Fresh cockpit prompt for image, without naming a tool. | tool_search trace, image_generate, running frame, image/png asset, cost_usd, zoom and human/agent visual inspection. |
| 3 | Upload a real photo through the normal upload flow; ask "make it night". | reference_asset_ids equals the uploaded owned asset ID; edited output visually follows it. |
| 4 | Ask for a five-second 480p video with default inline wait. | Same-turn video artifact and real playback/moving frames; actual latency recorded. If provider exceeds 45 seconds, document that observation and exercise inline deterministically as well; do not fake a same-turn pass. |
| 5 | Restart/configure with inline wait 1; submit video. | in_progress result; static historical frame; SourceMedia wake; one collect call; one visible video; explicit second collect returns already_delivered. |
| 6 | Restart Aura after the job row is durably inserted and before completion. | Same provider_job_id after boot, resumed poll/download, same asset identity, one delivery claim; separate results for the fault intervals from Task 6. |
| 7 | In a conversation with a generated image, ask "Animate this". | first_frame_asset_id points to that generated asset; wire first_frame; moving output. |
| 8 | In the approved Telegram test conversation ask for image and video. | Bot API photo/video message types and visible playback; upload action while tool runs. |
| 9 | Zero-credit identity tries generation. | Resolver/tool refusal and no outbound generation POST. If Runner prevents starting the turn, record that refusal plus the direct tool-port test for no_credit. |
| 10 | Five fresh conversations, natural prompts for both capabilities. | Both tools discovered via tool_search in all five, no tool-name hints/system prompt patch. |
| 11 | Run all configured gates on the final revision. | >=85% full-matrix Go and Vitest, >=70% clamp/watcher and media frontend mutations, clean race/goleak, green CI. |

For each artifact record MIME/size, job and asset IDs, cost, screenshot and inspection. For video use actual playback and sampled frames; a downloaded MP4 signature alone does not prove motion. Reopen successful conversations to assert image/video cards stay on the correct tool call. Inspect the 1-second historical placeholder remains static, with the later wake artifact visible once.

Record restart command and exact stopping point. Restart only the authorized test deployment; do not stop unrelated services. Restore temporary wait settings and test-only credit changes when the run ends.

- [ ] **Step 6: Close with measured evidence, commit and CI.**

Document all results in `docs/testing/image-video-generation.md` with failures/limitations. Update PRD's affected product requirements and the quality snapshot only for metrics actually measured. Keep Task 6's stronger recovery claim unaccepted if its evidence is absent. All eleven acceptance criteria plus the defined recovery scope must be assessed explicitly; never mark the phase done because unit tests alone are green.

~~~sh
git diff --check
git add web/e2e/media-generation-live.spec.ts web/e2e/media-generation-live.helpers.ts docs/testing/image-video-generation.md scripts/coverage_package_policy.json scripts/critical_mutation_gate.py scripts/critical_mutation_gate_test.py web/stryker.config.json web/vitest.stryker.config.ts prd.md docs/aura-quality-snapshot.md
git commit -m "test(media): verify generation delivery and recovery" -m "Record real-agent evidence and enforce coverage and mutation floors for the new runtime."
~~~

Include any actually changed CI file explicitly in the commit. Complete the required reviews, merge the finished task as directed by CLAUDE.md, run fresh post-merge checks, push the phase and inspect its CI results. A failed live criterion remains open with evidence; never replace it with a mock result.

## Spec coverage audit

| Requirement | Tasks |
|---|---|
| OpenRouter-only, installed SDK, no agent-chosen model | 1, 3, 4, 5, 9 |
| Identity key, local-route refusal, cost/no-credit | 1, 4, 5, 7, 9, 13 |
| Text-to-image, references, clamp, adjustments | 3, 4, 5, 13 |
| Text-to-video, first frame, references/audio | 3, 4, 9, 13 |
| Media job schema, RLS, restart, no repeat submit | 6, 7, 8, 9 |
| Inline/wake ownership and once-only claim | 6, 7, 8, 9, 13 |
| Assets video modality and exact byte ceiling | 2, 4, 7 |
| Catalog endpoints, prices, admin/hot settings | 1, 3, 10 |
| Registry elements, running/static/blocked/error states | 11 |
| Inline media, zoom/copy/download, SVG gate, send_file parity | 11 |
| Telegram photo/video/actions and size fallback | 12, 13 |
| English/Italian, existing Reload, v1 exclusions | 10, 11, 12 |
| Four config rows and PRD evidence | 1, 2, 13 |
| httptest/property/RLS/race/goleak/mutation/live tests | All code tasks; 13 aggregates gates |
| Absolute no-loss claim around ambiguous submit/claim | Explicit Task 6 decision gate; current spec sequence alone does not prove it |

## Planning self-review

- The implementation work is not executed by writing this document; no acceptance box is prechecked.
- Migration numbers are deliberately assigned at execution, twice: assets modality and media jobs.
- Shared catalog, SDK, identity resolver, asset SourceRef dedup, ingest helper, picker, blob lifecycle and chat-action pulse are reused.
- Tools have exact names/schemas, result fields and ownership rules; store/watcher/delivery consumers use the producing task's interfaces.
- The plan records the two measured spec corrections (registry identifier and unavailable per-image MAI price).
- The recovery guarantee has a visible preimplementation decision gate, not an unproven exactly-once assertion.
- Paid live work has a concrete batch and a separate future go requirement from the spec.
