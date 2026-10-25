# The chosen model reads images, ChatGPT included — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for
> tracking. Commits on master (the operator's standing rule).

**Goal:** with the chat on ChatGPT, the selected model reads the media indexer's images and scanned PDF
pages, and the daemon's upload summaries, with each document owner's own account. When it cannot,
the existing fallback applies.

**Architecture:**
- **`internal/llm/chatgpt` reads one image.** `(*Client).Describe` sends one Responses message with
  the prompt and the image. It sorts every ending into text, "cannot read here" (wrapped
  `llm.ErrMandatoryModalityUnavailable`) or a failure.
- **`internal/multimodal` takes it as the primary arm.** `VisionConfig.PrimaryReader` replaces the
  HTTP primary arm when it is set. On "cannot read here", the sidecar takes the image, or the result
  is `ErrNoVisionRoute`. A failure is returned unchanged.
- **The indexer opens the daemon's store.** `chatgptplan.Open` reads an existing store and creates
  nothing. `aura-media-index` builds the reader for `AURA_INGEST_IDENTITY_ID`, and `aura-ingest` mounts
  `aura-home`.
- **The daemon's serving asset service** gets the same reader, and the image processor reads under the
  asset's owner.
- **The ingest bridge** gives a scan a budget that grows with the page cap.

**Tech Stack:** Go (`internal/chatgptplan`, `internal/llm/chatgpt`, `internal/multimodal`,
`internal/assets`, `internal/config`, `cmd/aura-media-index`, `cmd/aura`), `openai-go/v3` Responses
(already the transport), Python (`services/ingest/media.py`), Docker Compose, the lab VM.

**Spec:** `docs/superpowers/specs/2026-10-25-media-vision-primary-design.md` (written 2026-10-25, revised
the same day after a read-only builder check; committed `aa321507c`).

## Global Constraints

- **"Cannot read here" is `llm.ErrMandatoryModalityUnavailable`,** wrapped with the reason
  (`fmt.Errorf("%w: ...", llm.ErrMandatoryModalityUnavailable)`). It is recognised only with
  `errors.Is`.
- **Cannot read here:**
  - the catalogue entry for `cfg.Model` with `visibility == "list"` has no image support, or there is
    no such entry;
  - `chatgptplan.ErrAuthorizationRequired`;
  - `chatgptplan.ErrPlanDisabled`;
  - any `response.refusal.delta` in the stream;
  - an image MIME type that is not `image/*`.
- **Failure:** everything else, including:
  - no identity in the context, or no token source;
  - a store error, or a catalogue error;
  - a stream error, idle stream or missing `response.completed`;
  - a plan limit, or a timeout.
- **Image support** is the existing rule from `projectMedia`: `supports_image`, or `"image"` in
  `input_modalities`.
- **The request:** one user message; `input_text` = the prompt; `input_image` from `mediaPart`
  (`Detail: "auto"`, data URL); `Store: false`; `Model: cfg.Model`; no reasoning parameter; no tools.
- **The token never appears** in an error, a log line or stderr (`sanitizeError`, `safeResponseError`).
- **The reader call is bounded** by `MULTIMODAL_TIMEOUT_SEC`, like the HTTP arms (`TimeoutContext`).
- **`chatgptplan.Open` creates nothing.** A missing `<dir>/chatgpt-plan` or `host-id` is
  `chatgptplan.ErrStoreMissing`. `Open` writes nothing but the `.lock` that `lockStore` already
  opens.
- **The store's parent is `config.(*Config).ChatGPTPlanRoot()`** = `filepath.Dir(c.SkillsDir)`. Both
  the daemon and the indexer use it; nothing else computes it.
- **Exit codes of `aura-media-index`:** 3 = `ErrNoVisionRoute`, 1 = any other failure. A ChatGPT route
  without `AURA_INGEST_IDENTITY_ID`, or with a store that does not open, exits 1.
- **The bridge's budget:**
  - `pdf`: `(_SCANNED_PDF_PAGE_LIMIT + 1) * request_timeout + 30`, with `_SCANNED_PDF_PAGE_LIMIT = 20`
    (= Go `maxScannedPDFPages`);
  - `image` and `audio`: `3 * request_timeout + 30` (unchanged).
- **Compose:**
  - `aura-ingest` gets `- aura-home:/var/lib/aura` and `AURA_SKILLS_DIR: /var/lib/aura/skills`;
  - no `subpath`;
  - `MULTIMODAL_BASE_URL` stays as it is; only the aura-ocr-vl comment changes;
  - `make payload-manifest` in the same commit.
- **Files ≤ 600 LOC. Go commands run in WSL** (never a Windows `.exe`), with
  `export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"`. Per touched package:
  - `go vet`;
  - `go test`;
  - `go test -race`.

## Review Focus

1. **An upload stored without an image MIME type** (`application/octet-stream` after a failed
   downscale) must be "cannot read here", with no request sent. Never a failure retried every cycle.
   Test: Task 2 `TestDescribeCannotReadANonImageType`.
2. **A reader that never answers** must be cut at `MULTIMODAL_TIMEOUT_SEC`, like the HTTP arms. Test:
   Task 3 `TestVisionBoundsTheReaderByTheTimeout`.
3. **The daemon and the indexer refreshing one owner's token.** The second process must read the
   rotated credential, not refresh again with a spent refresh token. Test: Task 1
   `TestOpenedStoreReadsTheCredentialTheDaemonRotated`.
4. **A refusal followed by output text** in the same stream must still be "cannot read here". Test:
   Task 2, a case of `TestDescribeTreatsARefusalAsCannotRead`.
5. **The indexer pointed at the wrong store path.** It must exit 1 and name the missing store, not
   degrade to card-only. Test: Task 4 `TestRunFailsWhenTheChatGPTStoreDoesNotOpen`.

## Decisions taken while writing this plan

- **`internal/llm/chatgpt` imports `internal/chatgptplan`** to recognise `ErrAuthorizationRequired`
  and `ErrPlanDisabled`. There is no cycle: `chatgptplan` imports only `identityctx` and `secret`. Both
  packages are the one product, ChatGPT plan usage.
- **`VisionClient.Describe` returns `(text, model string, err error)`,** so that `vision_model` names
  the arm that answered.
  - `VisionModel()` loses its only production caller and is deleted.
  - `assets.DescribeImageForRetrieval` returns the model the same way.
  - `ImageReader.Describe` keeps two results: its model is fixed.
- **The reader's stream loop is its own,** because it must see refusals that `consumeStream` turns
  into text. The two loops share three extracted helpers:
  - `openStream`: the idle context plus `NewStreaming`;
  - `terminalError`: the `response.failed` / `response.incomplete` / `error` event to a
    `*ResponseError`;
  - `streamEndError`: idle, `stream.Err()`, `ErrStreamIncomplete`.
- **`buildAssetService` gains an `imageReader multimodal.ImageReader` parameter.** `serve.go` passes
  `chatGPTImageReader(cfg.LLM, chat.chatGPTPlan)`; `docs.go` and `document_open_wiring.go` pass
  `nil`, since they never read an image (spec §4.4).

## File structure

- `internal/chatgptplan/service.go`: `Open`, with `New` and `Open` sharing one constructor.
- `internal/chatgptplan/store.go`: `loadHostID` gains a `create bool` parameter.
- `internal/chatgptplan/open_test.go` (new).
- `internal/config/config_paths.go`: `ChatGPTPlanRoot`.
- `cmd/aura/serve_chatgpt.go`: uses `ChatGPTPlanRoot`, and gains `chatGPTImageReader`.
- `internal/llm/chatgpt/describe.go` (new): `Describe`.
- `internal/llm/chatgpt/media.go`: `imageCapabilities`, now shared.
- `internal/llm/chatgpt/stream.go`: the extracted helpers.
- `internal/llm/chatgpt/client.go`: `openStream`.
- `internal/llm/chatgpt/describe_test.go` (new).
- `internal/multimodal/vision.go`: `ImageReader`, `PrimaryReader`, the new `Describe` results, and
  `VisionModel` removed.
- `internal/multimodal/vision_test.go`: covers the above.
- `internal/assets/image_processor.go`: the owner's identity and the answering model.
- `internal/assets/image_processor_test.go`: covers the above.
- `cmd/aura-media-index/main.go`: the ChatGPT route.
- `cmd/aura-media-index/chatgpt.go` (new): `chatGPTRoute`.
- `cmd/aura-media-index/main_test.go` and `chatgpt_test.go` (new).
- `cmd/aura/document_processor_wiring.go`, `serve.go`, `docs.go`, `document_open_wiring.go`: the
  `buildAssetService` parameter.
- `services/ingest/media.py` and `services/ingest/tests/test_media.py`: the budget.
- `compose.yaml`, `scripts/payload_manifest.txt` and `cmd/aura/container_artifacts_test.go`: the mount.

---

### Task 1: Open the daemon's credentials store

**Files:**
- Modify: `internal/chatgptplan/service.go` (`New`), `internal/chatgptplan/store.go` (`loadHostID`)
- Modify: `internal/config/config_paths.go`, `cmd/aura/serve_chatgpt.go:16-26`
- Create: `internal/chatgptplan/open_test.go`
- Test: `internal/config/config_paths_test.go` (create if absent)

**Interfaces:**
- Produces:
  - `chatgptplan.Open(dir, authulaSecret string) (*Service, error)`;
  - `chatgptplan.ErrStoreMissing`;
  - `func (c *Config) ChatGPTPlanRoot() string`.

- [ ] **Step 1: Write the failing tests** in `open_test.go`, with the `oidcFixture` from `fixture_test.go`:
  - `TestOpenReadsTheDaemonsStore`: `f.service(t, dir)` then `f.connect(t, s, owner)`. Then
    `Open(dir, testSecret)`, and `AccessToken(ownerContext(owner))` equals the connected access token.
  - `TestOpenRefusesAMissingStore`: on an empty `t.TempDir()`, `Open` returns an error with
    `errors.Is(err, ErrStoreMissing)`, and `filepath.Join(dir, "chatgpt-plan")` does not exist
    afterwards.
  - `TestOpenRefusesAStoreWithoutHostID`: with `os.Mkdir(filepath.Join(dir, "chatgpt-plan"), 0o700)`,
    `Open` returns `ErrStoreMissing`, and `host-id` does not exist afterwards.
  - `TestOpenedStoreReadsTheCredentialTheDaemonRotated` (Review Focus 3):
    - the daemon's service refreshes an expired credential (`changeCredential` sets `ExpiresAt` in
      the past; the fixture answers the refresh);
    - then the opened service, its `endpoint` set to the fixture's, returns the rotated access token;
    - the fixture counts exactly one refresh.

  In `config_paths_test.go`, `TestChatGPTPlanRootIsTheParentOfTheSkillsDir`:
  `(&Config{SkillsDir: "/var/lib/aura/skills"}).ChatGPTPlanRoot() == "/var/lib/aura"`.
- [ ] **Step 2: Run them to see them fail.**
  Run: `go test ./internal/chatgptplan/ ./internal/config/ -run 'Open|ChatGPTPlanRoot'`.
  Expected: FAIL, `undefined: Open`, `ErrStoreMissing` and `ChatGPTPlanRoot`.
- [ ] **Step 3: Implement.**
  - `New` and `Open` call one unexported constructor with a `create bool`.
  - With `create == false`:
    - `os.Stat` must find the `chatgpt-plan` directory, or the result is `ErrStoreMissing`;
    - `privateDirectory` is not called;
    - `loadHostID(dir, false)` returns `ErrStoreMissing` for a missing file instead of writing one.
  - `ErrStoreMissing` reads: `chatgpt plan: no credential store here; the daemon creates it`.
  - `ChatGPTPlanRoot` goes into `config_paths.go`, with a one-line comment that the ChatGPT store
    lives beside the skills root.
  - `newChatGPTPlan` calls `chatgptplan.New(cfg.ChatGPTPlanRoot(), cfg.AuthulaSecret)`.
- [ ] **Step 4: Run them to see them pass,** then the packages with `-race`.
  Run: `go test -race ./internal/chatgptplan/ ./internal/config/ && go vet ./internal/chatgptplan/ ./internal/config/ ./cmd/aura/`.
  Expected: `ok` for both packages; vet silent.
- [ ] **Step 5: Commit.**
  ```bash
  git add internal/chatgptplan internal/config/config_paths.go internal/config/config_paths_test.go cmd/aura/serve_chatgpt.go
  git commit -m "feat(chatgptplan): open the daemon's credentials store without creating one"
  ```
  The body says why: an indexer that created a store would turn every installation mistake into
  card-only.

### Task 2: The ChatGPT client reads one image

**Files:**
- Create: `internal/llm/chatgpt/describe.go`, `internal/llm/chatgpt/describe_test.go`
- Modify: `internal/llm/chatgpt/media.go` (`projectMedia`), `stream.go`, `client.go` (`Stream`)

**Interfaces:**
- Consumes: `chatgptplan.ErrAuthorizationRequired`, `chatgptplan.ErrPlanDisabled` (existing).
- Produces:
  - `func (c *Client) Describe(ctx context.Context, image []byte, mimeType, prompt string) (string, error)`;
  - `func imageCapabilities(models []modelWire, slug string) llm.ProviderContentCapabilities`, used by
    `projectMedia` and `Describe`.

- [ ] **Step 1: Write the failing tests** in `describe_test.go`, on `fixtureClient` and `event`. The
  handler serves `GET /v1/models` with a catalogue and `POST /v1/responses` with SSE. The config gains
  `Model: "vision"`.
  - `TestDescribeSendsOneImageMessage`:
    - the catalogue is `{"models":[{"slug":"vision","visibility":"list","supports_image":true}]}`;
    - the stream sends `response.output_text.delta` `"VERBALE "` and `"ITALIA-7391"`, then `completed`;
    - `Describe(ctx, []byte("png-bytes"), "image/png", "Read this.")` returns `"VERBALE ITALIA-7391"`;
    - the decoded request has `model == "vision"` and `store == false`;
    - it has one input item with role `user`, whose content is `[input_text "Read this.",
      input_image "data:image/png;base64,cG5nLWJ5dGVz"]`;
    - it has no `tools` and no `reasoning`;
    - the request carries `Authorization: Bearer oauth-test-token`.
  - `TestDescribeCannotReadWithAModelThatSeesNoImages`. A table over three catalogues: text only
    (`input_modalities:["text"]`), the slug missing, the slug with `visibility:"hide"`. Each gives
    `errors.Is(err, llm.ErrMandatoryModalityUnavailable)`, and `/v1/responses` is never hit.
  - `TestDescribeCannotReadWithoutAConnectedAccount`. A table: the token source returns
    `chatgptplan.ErrAuthorizationRequired`, then `chatgptplan.ErrPlanDisabled`. Each gives the
    sentinel, and no HTTP request is made.
  - `TestDescribeTreatsARefusalAsCannotRead` (Review Focus 4). A table:
    - only `response.refusal.delta`, then `completed`;
    - `output_text.delta` `"partial"`, then `refusal.delta`, then `completed`.

    Each gives the sentinel, and the error text does not contain `"partial"`.
  - `TestDescribeCannotReadANonImageType` (Review Focus 1). With `"application/octet-stream"`, the
    result is the sentinel, and no request reaches `/v1/responses`.
  - `TestDescribeFailures`. A table, each `!errors.Is(err, llm.ErrMandatoryModalityUnavailable)`, and
    `!strings.Contains(err.Error(), "oauth-test-token")`:
    - the token source returns `errors.New("chatgpt plan: an Aura identity is required")`;
    - `/v1/models` answers 500;
    - `/v1/responses` answers 429 with a body naming the token;
    - the stream sends `response.failed` with code `usage_limit_reached`;
    - the stream ends after a text delta without `completed`.
- [ ] **Step 2: Run them to see them fail.**
  Run: `go test ./internal/llm/chatgpt/ -run Describe`.
  Expected: FAIL, `c.Describe undefined`.
- [ ] **Step 3: Implement.**
  - `imageCapabilities` is the catalogue loop moved out of `projectMedia`; `projectMedia` calls it.
  - Extract `openStream(ctx, params, token)` from `Stream`: it returns the stream, `firedIdle` and
    `cleanup`, or a sanitized error. Extract `terminalError(event, token) *ResponseError` and
    `streamEndError(stream, firedIdle, token) error` from `consumeStream`. `Stream` and
    `consumeStream` keep their behaviour and use the helpers.
  - `Describe` checks, in this order:
    1. the MIME type;
    2. `accessToken`, with the two plan errors mapped to the sentinel;
    3. `fetchModelWire` and `imageCapabilities`;
    4. `mediaPart`.

    Then it opens the stream and runs its own loop:
    - `output_text.delta` appends;
    - `refusal.delta` marks a refusal;
    - a terminal event returns `terminalError`;
    - `completed` returns the text, or the sentinel when refused;
    - after the loop, `streamEndError`.
  - The sentinel's wraps read `chatgpt: <model> does not read images`,
    `chatgpt: <reason from the plan error>`, `chatgpt: the model refused to read the image` and
    `chatgpt: <mime> is not an image`.
- [ ] **Step 4: Run the package with `-race`.**
  Run: `go test -race ./internal/llm/chatgpt/ && go vet ./internal/llm/chatgpt/`.
  Expected: `ok`. The existing `TestStreamingRequestAndHistory`, `TestTerminalFailures`,
  `TestRequestImagesAndToolMedia` and the rest still pass unchanged.
- [ ] **Step 5: Commit.**
  ```bash
  git add internal/llm/chatgpt
  git commit -m "feat(chatgpt): read one image with the selected model on the account"
  ```

### Task 3: The vision route takes the reader

**Files:**
- Modify: `internal/multimodal/vision.go`, `internal/multimodal/vision_test.go`
- Modify: `internal/assets/image_processor.go`, `internal/assets/image_processor_test.go`

**Interfaces:**
- Consumes: the `Describe` signature from Task 2 (two results).
- Produces:
  - `type ImageReader interface { Describe(ctx context.Context, image []byte, mimeType, prompt string) (string, error) }`;
  - the field `VisionConfig.PrimaryReader ImageReader`;
  - `func (c *VisionClient) Describe(ctx, imageBytes []byte, mimeType, prompt string) (text, model string, err error)`;
  - `func DescribeImageForRetrieval(ctx, vision *multimodal.VisionClient, imageBytes []byte, mimeType string) (text, model string, err error)`.

- [ ] **Step 1: Write the failing tests** in `vision_test.go`, with a `fakeReader` that records its
  context's deadline and identity and returns a preset `(text, err)`:
  - `TestVisionReaderAnswersAsThePrimaryArm`:
    - with `PrimaryReader` set to answer `"ok"`, `Model: "gpt-x"` and a `PrimaryBaseURL` server that
      fails the test if hit;
    - `Describe` returns `("ok", "gpt-x", nil)`.
  - `TestVisionReaderThatCannotReadFallsToTheSidecar`:
    - the reader returns `fmt.Errorf("%w: no", llm.ErrMandatoryModalityUnavailable)`;
    - `LocalBaseURL` is a server answering `"sidecar"`, with `LocalModel: "glm-ocr"`;
    - the result is `("sidecar", "glm-ocr", nil)`.
  - `TestVisionReaderThatCannotReadWithoutSidecarIsNoRoute`: the same reader, no `LocalBaseURL`;
    `errors.Is(err, ErrNoVisionRoute)`.
  - `TestVisionReaderFailureIsReturnedWithoutTheSidecar`:
    - the reader returns `errors.New("chatgpt: usage_limit_reached")`;
    - the sidecar server fails the test if hit;
    - the error is returned, `!errors.Is(err, ErrNoVisionRoute)`.
  - `TestVisionBoundsTheReaderByTheTimeout` (Review Focus 2). With `TimeoutSec: 1`, the reader sees a
    context deadline no later than 1 s after the call.

  Update the existing tests to the three results. `TestVisionPrefersPrimaryModelWhenItAcceptsImages`
  asserts the returned model instead of `VisionModel()`.

  In `image_processor_test.go`, `TestImageProcessorReadsUnderTheAssetOwner`:
  - the reader records `identityctx.IdentityID(ctx)`;
  - `ProcessAsset` on `Asset{IdentityID: "owner-1", …}` gives `"owner-1"`;
  - `result.Metadata["vision_model"]` is the reader arm's model.
- [ ] **Step 2: Run them to see them fail.**
  Run: `go test ./internal/multimodal/ ./internal/assets/ -run 'Vision|ImageProcessor|DescribeImage'`.
  Expected: FAIL to compile on `PrimaryReader` and on the result count.
- [ ] **Step 3: Implement.**
  - `Describe` bounds the call with `TimeoutContext(ctx, c.cfg.TimeoutSec)` and runs:
    1. the reader arm when `PrimaryReader != nil`. On the sentinel it goes to the local arm, or
       returns `fmt.Errorf("%w: %w", ErrNoVisionRoute, err)`. Any other error is returned;
    2. otherwise the existing `route()` arms.
  - The HTTP POST moves into one helper that both HTTP arms and the fallback use.
  - Delete `VisionModel`. Update the `VisionConfig` doc comment to name the reader arm.
  - `ImageProcessor.ProcessAsset` passes `identityctx.WithIdentityID(ctx, asset.IdentityID)` to
    `DescribeImageForRetrieval`, and records its model as `vision_model`.
- [ ] **Step 4: Run both packages with `-race`.** `cmd/aura-media-index` stops compiling until Task 4,
  so this run is the packages only.
  Run: `go test -race ./internal/multimodal/ ./internal/assets/ && go vet ./internal/multimodal/ ./internal/assets/`.
  Expected: `ok` ×2.
- [ ] **Step 5: Fix the media indexer's call sites** to the three-result `DescribeImageForRetrieval`,
  so the tree compiles (the model is unused there).
  Run: `go build ./cmd/... ./internal/...`.
  Expected: success.
- [ ] **Step 6: Commit.**
  ```bash
  git add internal/multimodal internal/assets cmd/aura-media-index/main.go
  git commit -m "feat(multimodal): the selected model's own client reads the image when it can"
  ```

### Task 4: The media indexer reads with the owner's ChatGPT account

**Files:**
- Create: `cmd/aura-media-index/chatgpt.go`, `cmd/aura-media-index/chatgpt_test.go`
- Modify: `cmd/aura-media-index/main.go` (`newMediaRuntime`, `run`), `cmd/aura-media-index/main_test.go`

**Interfaces:**
- Consumes:
  - `chatgptplan.Open` and `ErrStoreMissing`, from Task 1;
  - `cfg.ChatGPTPlanRoot()`;
  - `(*chatgpt.Client).Describe`, from Task 2;
  - `VisionConfig.PrimaryReader`, from Task 3.
- Produces:
  - `func chatGPTRoute(ctx context.Context, cfg *config.Config, getenv func(string) string) (context.Context, multimodal.ImageReader, error)`;
  - `newMediaRuntime(cfg, creds, client, primaryAcceptsImages, reader multimodal.ImageReader) mediaRuntime`.

- [ ] **Step 1: Write the failing tests.**

  In `chatgpt_test.go`:
  - `TestChatGPTRouteIsNilOffChatGPT`: provider `ollama` returns the same context, a nil reader and
    no error.
  - `TestChatGPTRouteNeedsTheIngestIdentity`: provider `chatgpt` with `getenv` returning `""` gives an
    error naming `AURA_INGEST_IDENTITY_ID`.
  - `TestChatGPTRouteOpensTheStoreForTheIdentity`:
    - `chatgptplan.New(root, secret)` makes a store in a temp `root`;
    - `cfg.SkillsDir` is `root+"/skills"`, and `AuthulaSecret` is the same 64-hex secret;
    - the identity is `"a696df2b-b7bc-4ee7-870b-15d2cced1839"`;
    - the result is a non-nil reader, and `identityctx.IdentityID(ctx)` is that identity.

  In `main_test.go`:
  - `TestRunFailsWhenTheChatGPTStoreDoesNotOpen` (Review Focus 5):
    - `t.Setenv` provider `chatgpt`, `AURA_LLM_BASE_URL=https://api.openai.com/v1`, a model,
      `AURA_AUTHULA_SECRET`, an empty temp `AURA_SKILLS_DIR` and `AURA_INGEST_IDENTITY_ID`, with
      `AURA_DB_URL=""` and `MULTIMODAL_BASE_URL=""`, as `TestRunReportsNoVisionRouteWithItsOwnExitCode`
      sets them;
    - `run` on an image exits 1;
    - stderr contains `no credential store here`.
  - `TestRunFailsOnAChatGPTRouteWithoutTheIngestIdentity`: the same, without the identity; exit 1.
  - `TestMediaRuntimeReadsWithTheReader`: `newMediaRuntime` with a fake reader answering
    `"Pilot workspace"` derives that text for an image.
  - `TestMediaRuntimeIsNoRouteWhenTheReaderCannotRead`: a fake reader returning the sentinel and no
    `MultimodalBaseURL` gives `errors.Is(err, multimodal.ErrNoVisionRoute)`.
  - `TestMediaRuntimeReadsScannedPagesInOrderWithTheReader`:
    - a `renderPDF` stub returns three page files;
    - the fake reader answers `page-1`, `page-2`, `page-3`;
    - the text is `"PDF page 1:\npage-1\n\nPDF page 2:\npage-2\n\nPDF page 3:\npage-3"`.

  Existing `newMediaRuntime` calls pass `nil` as the reader.
- [ ] **Step 2: Run them to see them fail.**
  Run: `go test ./cmd/aura-media-index/`.
  Expected: FAIL, `undefined: chatGPTRoute`, and a wrong argument count to `newMediaRuntime`.
- [ ] **Step 3: Implement.**
  - `chatGPTRoute`:
    - off ChatGPT, it returns `(ctx, nil, nil)`;
    - it trims `getenv("AURA_INGEST_IDENTITY_ID")`; empty is an error;
    - it calls `chatgptplan.Open(cfg.ChatGPTPlanRoot(), cfg.AuthulaSecret)`, and its error is returned
      wrapped;
    - it returns `identityctx.WithIdentityID(ctx, id)` and `chatgpt.New(cfg.LLM, plan)`.
  - `newMediaRuntime` sets `visionCfg.PrimaryReader = reader` only for a non-nil reader, so that no
    typed nil lands in the interface.
  - `run` calls `chatGPTRoute(ctx, cfg, os.Getenv)` after `loadEffectiveConfig`. On error it prints and
    returns 1. It passes the returned context to `derive`.
- [ ] **Step 4: Run the package with `-race`.**
  Run: `go test -race ./cmd/aura-media-index/ && go vet ./cmd/aura-media-index/`.
  Expected: `ok`. `TestRunReportsNoVisionRouteWithItsOwnExitCode` still exits 3.
- [ ] **Step 5: Commit.**
  ```bash
  git add cmd/aura-media-index
  git commit -m "feat(media-index): read images with the document owner's ChatGPT account"
  ```

### Task 5: The daemon's upload summary reads with ChatGPT

**Files:**
- Modify: `cmd/aura/document_processor_wiring.go` (`buildAssetService`, `visionConfigFrom`),
  `cmd/aura/serve.go:275`, `cmd/aura/docs.go:299`, `cmd/aura/document_open_wiring.go:49`
- Modify: `cmd/aura/serve_chatgpt.go` (`chatGPTImageReader`)
- Test: `cmd/aura/serve_chatgpt_test.go` (or the existing file that tests `newChatGPTPlan`)

**Interfaces:**
- Consumes: `multimodal.ImageReader` and `PrimaryReader`, from Task 3; `chatgpt.New`.
- Produces:
  - `func chatGPTImageReader(cfg llm.Config, plan *chatgptplan.Service) multimodal.ImageReader`;
  - `buildAssetService(cfg, pool, objectStore, imageReader multimodal.ImageReader)`.

- [ ] **Step 1: Write the failing test** `TestChatGPTImageReaderOnlyOnTheChatGPTRoute`:
  - provider `chatgpt` with a non-nil plan (from `chatgptplan.New` on a temp dir) gives a non-nil
    reader;
  - provider `openrouter` gives `nil`;
  - provider `chatgpt` with a nil plan gives `nil`, an untyped nil: `reader == nil`.
- [ ] **Step 2: Run it to see it fail.**
  Run: `go test ./cmd/aura/ -run TestChatGPTImageReader`.
  Expected: FAIL, `undefined: chatGPTImageReader`.
- [ ] **Step 3: Implement.**
  - `chatGPTImageReader` returns `chatgpt.New(cfg, plan)` on the ChatGPT route with a plan, else nil.
  - `buildAssetService` puts a non-nil `imageReader` into the vision config it builds.
  - `serve.go` passes `chatGPTImageReader(chat.cfg.LLM, chat.chatGPTPlan)`; the other two callers pass
    `nil`.
- [ ] **Step 4: Run the touched tests and vet.**
  Run: `go test -race ./cmd/aura/ -run 'ChatGPT|Asset|Vision|DocumentProcessor' && go vet ./cmd/aura/`.
  Expected: `ok`.
- [ ] **Step 5: Commit.**
  ```bash
  git add cmd/aura
  git commit -m "feat(assets): the daemon's image summary reads with ChatGPT on that route"
  ```

### Task 6: aura-ingest reaches the store, and a scan has its time

**Files:**
- Modify: `services/ingest/media.py` (`derive`), `services/ingest/tests/test_media.py`
- Modify: `compose.yaml` (the `aura-ingest` block, the aura-ocr-vl comment), `scripts/payload_manifest.txt`
- Modify: `cmd/aura/container_artifacts_test.go`

**Interfaces:**
- Consumes: `ChatGPTPlanRoot()` = parent of `AURA_SKILLS_DIR`, from Task 1.
- Produces: nothing new for later tasks.

- [ ] **Step 1: Write the failing tests.**
  - In `test_media.py`, `test_scan_budget_grows_with_the_page_cap`:
    - with `MULTIMODAL_TIMEOUT_SEC=120` and `media._run` monkeypatched to record its `timeout`;
    - `derive(p, "scan.pdf", "pdf")` records `21 * 120 + 30`;
    - `derive(p, "a.png", "image")` records `3 * 120 + 30`;
    - `derive(p, "a.ogg", "audio")` records `3 * 120 + 30`.
  - In `container_artifacts_test.go`, `TestIngestOpensTheDaemonsChatGPTStore`. The `aura-ingest`
    block:
    - contains `- aura-home:/var/lib/aura` and `AURA_SKILLS_DIR: /var/lib/aura/skills`;
    - does not contain `subpath`.
- [ ] **Step 2: Run them to see them fail.**
  - Run: `go test ./cmd/aura/ -run TestIngestOpensTheDaemonsChatGPTStore`.
    Expected: FAIL, the missing mount.
  - Run (WSL, the image built): `docker run --rm --entrypoint sh -v "$PWD/services/ingest:/app/ingest:ro" aura-ingest:local -c 'pip install --quiet pytest && cd /app && python -m pytest ingest/tests/test_media.py -q'`.
    Expected: `test_scan_budget_grows_with_the_page_cap` FAILS with `390 != 2550`.
- [ ] **Step 3: Implement.**
  - In `media.py`, `_SCANNED_PDF_PAGE_LIMIT = 20`, with a comment that it mirrors
    `maxScannedPDFPages` in `cmd/aura-media-index`. `derive` picks the budget by `media_kind`.
  - In `compose.yaml`, `aura-ingest` gains the volume line and the env line. A short comment says
    that the media indexer opens the daemon's ChatGPT store there, and why it is not a `subpath`:
    the engine version.
  - The aura-ocr-vl comment says what happens: with `MULTIMODAL_BASE_URL` set, an image no route can
    read fails every cycle; card-only needs it unset.
  - Then `make payload-manifest` (WSL).
- [ ] **Step 4: Run them to see them pass,** with the whole contract file and the bridge suite.
  - Run: `go test ./cmd/aura/ -run 'Container|Compose|Ingest'`.
    Expected: `ok`.
  - Run: the same pytest command on `ingest/tests/test_media.py ingest/tests/test_billed_media_memo.py`.
    Expected: all pass.
- [ ] **Step 5: Commit.**
  ```bash
  git add services/ingest/media.py services/ingest/tests/test_media.py compose.yaml scripts/payload_manifest.txt cmd/aura/container_artifacts_test.go
  git commit -m "feat(ingest): open the daemon's ChatGPT store and give a scan its time"
  ```

### Task 7: Gates, push, and the lab VM

**Files:**
- Create: `docs/superpowers/verification/2026-10-25-media-vision-primary-lab-vm.md` (dated on the
  run's day)
- Modify: `prd.md` (the media paragraph: the route and the measurement), the Status lines of this
  plan and of the spec

- [ ] **Step 1: The full gates in WSL:**
  - `make quality`;
  - the coverage gate for the touched packages (`bash scripts/coverage_docker.sh`);
  - `make ingest-test`.

  Expected: all green. The package floors hold for `internal/chatgptplan`, `internal/llm/chatgpt`,
  `internal/multimodal` and `internal/assets`.
- [ ] **Step 2: The final whole-branch review,** then one fix pass (executing-plans).
- [ ] **Step 3: Push with the WSL recipe** and check every CI job is green on the pushed SHA.
- [ ] **Step 4: The operator approves the update in the cockpit.** Before any check, every container's
  `StartedAt` is past the aura revision flip. `aura-ingest` shows the `aura-home` mount.
- [ ] **Step 5: Acceptance A–C** (spec §5), driven by the operator. Claude reads back over ssh,
  read-only:
  - the ingest log for the file;
  - the index's passages;
  - the asset row of B;
  - the agent's turn.
- [ ] **Step 6: Record the result.** The verification file, the PRD paragraph and the Status lines, in
  one docs commit and push.

  The file says what was measured, and what the run does not show (spec §9).
