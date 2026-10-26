# The chosen model reads images, ChatGPT included — design

Date: 2026-10-25
Status: written 2026-10-25. The operator skipped the questions ("parti pure"); every call made
while writing is listed in §7 for the operator to check. Revised the same day after a read-only
builder check: the credentials mount, the store the indexer opens, refusals, a scan's time budget,
and the sidecar default (dropped from this change, §4.7). §4.4 amended the same day: the summary
reads the route per image. Implemented and accepted on the lab VM on 2026-10-26 (plan Status;
`docs/superpowers/verification/2026-10-25-media-vision-primary-lab-vm.md`). §4.7 stays open for the
operator.

## 1. Why

Measured on 2026-10-23 on the lab VM, build `d46cedab6`, with the chat on ChatGPT `gpt-6.1-sol`
(`docs/superpowers/verification/2026-10-23-cloud-service-credentials.md`, "Found during the run").
`verbale_scansionato_italiano.pdf`, a scan with no text layer, was not indexed. The ingest error:

`media indexer exited 1: describe scanned PDF page 1: Post "http://aura-ocr-vl:8082/v1/chat/completions": dial tcp: lookup aura-ocr-vl ... server misbehaving`

The cause, read in the code:
- `llm.NewContentCapabilitySource` has no case for the ChatGPT target, so it returns nil.
- `multimodal.PrimaryAcceptsImages` is then false, and `VisionClient.route` takes the local arm,
  `MULTIMODAL_BASE_URL`, the aura-ocr-vl sidecar. The VM does not run it.
- Underneath both: the vision client's primary arm speaks only OpenAI `/chat/completions` with an
  API key. ChatGPT plan usage is the Responses API with the identity's account OAuth token
  (`internal/llm/chatgpt`), so it could not use the arm even with the capability source fixed.

The operator's decision, 2026-10-23: "se il modello scelto ha la vision si sceglie quello",
ChatGPT included.

**Measured 2026-10-25 on the lab VM, at the operator's request.** The question was whether the
existing `/chat/completions` arm could take the plan token. A throwaway probe, never committed, ran
once inside the `aura` container:
- It took the operator's token through `chatgptplan.Service.AccessToken`, under the store's lock,
  and printed nothing of it.
- It sent page 1 of `verbale_scansionato_italiano.pdf`, rendered at 1024 px (PNG, 35 KB), with
  `gpt-6.1-sol`:

| Request | Answer |
|---|---|
| `GET /v1/models` | 200; `gpt-6.1-sol`, `visibility: list`, `input_modalities: [text, image]` |
| `POST /v1/chat/completions`, not streamed: today's vision request | **401** `rejected_by_access_enforcement`, `no_matching_rule` |
| `POST /v1/chat/completions`, streamed | **401**, the same |
| `POST /v1/responses`, streamed, `store: false`, `input_text` + `input_image` | 200, `response.completed`; the text: `VERBALE SCANSIONATO AURA  CODICE PRATICA: ITALIA-7391  IMPORTO APPROVATO: 48270 EURO  RESPONSABILE: GIULIA BIANCHI` |

The plan token is refused on Chat Completions and accepted on Responses, as OpenAI documents.
[Models and inference](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference)
names only `/v1/models` and `/v1/responses`;
[preview limitations](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations)
require `stream: true` and `store: false`.

What it does not show:
- **Other models and accounts.** One model, one account, one page.
- **Latency and cost.** Neither was recorded.
- **Ingest.** The text came from the probe, not from the media indexer.

## 2. Goals, non-goals, anti-goals

Goals:
- When the chat runs on ChatGPT and the selected model reads images, that model reads them: the
  media indexer's images and scanned PDF pages, and the image summary the daemon writes on upload.
  Each document is read with its owner's own ChatGPT account, as chat inference already is.
- When the ChatGPT route cannot read an image, the file falls back as it does today: the local
  sidecar if one is configured, otherwise card-only.
- A scan of up to the page limit has the time to be read (§4.6).

Non-goals:
- Other providers. OpenRouter, llama.cpp and Ollama keep their route unchanged.
- Audio. Speech-to-text keeps its own route.
- Re-reading files already indexed card-only. A file's extraction is memoized on its bytes and
  name (`services/ingest/app.py` `_extract`), and this change does not alter that key.
- A choice of vision model separate from the chat model.
- What happens under compose when nothing can read an image (§4.7).

Anti-goals, what would make this a failure even if it ran:
- Sending a document to an account other than its owner's.
- Indexing an empty or invented text because the model never saw the image, for example when the
  model does not read images and the request goes out as text only.
- Caching card-only for a transient failure: a timeout, a refused stream, a plan limit, a
  catalogue that did not answer. These must stay failures that the next cycle retries.
- An OAuth token in a log line, an error message or the indexer's stderr.

## 3. What exists, and is reused

- **`internal/llm/chatgpt`.** The Responses client with the identity's OAuth token:
  - `accessToken` reads the token per request;
  - `fetchModelWire` reads the account catalogue, and `projectMedia` derives image support from
    an entry's `supports_image` and `input_modalities`;
  - `mediaPart` builds the `input_image` part;
  - `consumeStream` succeeds only after `response.completed`, and `sanitizeError` strips the token.
- **`internal/chatgptplan.Service`.** The sealed per-identity credentials under
  `<dir>/chatgpt-plan`. `AccessToken(ctx)` needs the identity in `ctx`, refreshes with rotation,
  and serializes on an flock over `<dir>/chatgpt-plan/.lock`, which holds across processes that
  share the directory. The daemon builds it as `chatgptplan.New(filepath.Dir(cfg.SkillsDir),
  cfg.AuthulaSecret)` (`cmd/aura/serve_chatgpt.go`).
- **`internal/multimodal.VisionClient`.** The vision route: the primary model over OpenAI
  `/chat/completions`, then the local sidecar, then `ErrNoVisionRoute`. Its two callers are
  `cmd/aura/document_processor_wiring.go` (the daemon's `ImageProcessor`) and
  `cmd/aura-media-index` (the ingest bridge). Both pass through
  `assets.DescribeImageForRetrieval`, which downscales first.
- **The ingest bridge.** `services/ingest/media.py` runs `aura-media-index` in the CocoIndex child.
  Exit 3 (`exitNoVisionRoute`) degrades the file to card-only; any other failure raises and is
  retried. The child's environment carries `AURA_INGEST_IDENTITY_ID`
  (`internal/ingestsupervisor`), and the subprocess inherits it.
- **The asset owner.** Processors run on a context with no principal; `storeForAsset` stamps
  `asset.IdentityID` (`internal/assets/object_resolver.go`).

## 4. Design

### 4.1 The ChatGPT image reader

The ChatGPT client gains one operation: read one image with the selected model and a prompt, and
return the model's text. It is built from what the client already has:
- the token from `accessToken`, for the identity in `ctx`;
- the account catalogue, read on each call as `projectMedia` already reads it. The derivation of
  an entry's image support moves into one helper that `projectMedia` and this operation share;
- one user message with an `input_text` part, the prompt, and the `input_image` part `mediaPart`
  builds; `store: false`; the model is the configured one;
- the existing streaming path, which succeeds only on `response.completed`.

It sorts every way it can end into three outcomes:

| Outcome | When |
|---|---|
| The text | `response.completed`, with output text and no refusal. |
| Cannot read here | The selected model's catalogue entry does not read images, or is missing from the listed models. The owner has no connected account or no plan grant: `chatgptplan.ErrAuthorizationRequired` or `ErrPlanDisabled`, from a store that opened (§4.3). The model refused: any `response.refusal.delta` in the stream. |
| Failure | Everything else: no identity in the context, no token source, a store that could not be read, the catalogue did not answer, a refresh failed on the network, the stream failed, went idle or ended without `response.completed`, a plan limit, a timeout. |

A refusal is never returned as the image's text. Today `consumeStream` emits refusal deltas as
`Text` (`stream.go`), which is right for the chat and wrong here: the refusal would be indexed and
memoized as what the page says. Asking again gets the same refusal on the owner's plan, so it is
"cannot read here", not a failure.

"Cannot read here" is one exported sentinel that `internal/multimodal` recognises with
`errors.Is`. It is `llm.ErrMandatoryModalityUnavailable`, wrapped with the reason. Both packages
already import `internal/llm`.

### 4.2 The vision route

`VisionConfig` gains an optional image reader: an interface with the same `Describe` signature
`VisionClient` has. When it is set, it is the primary arm, and the HTTP primary arm is not used.
`route()` stays a configuration branch that issues no I/O; the reader's own catalogue read
happens inside its call.

- **The reader returns text.** That is the answer.
- **The reader says "cannot read here".** The local arm takes the image if `LocalBaseURL` is set.
  Otherwise `Describe` returns `ErrNoVisionRoute`.
- **The reader fails.** `Describe` returns that error. The local arm is not tried: a transient
  outage of the chosen model is not a reason to read the document with another model.

`VisionModel()` names the configured model for the reader arm. The asset's `vision_model`
metadata names the model that actually read the image. When the reader declined and the sidecar
read it, that is the sidecar's model.

### 4.3 The media indexer

When the effective configuration's provider is ChatGPT, `aura-media-index` builds the reader:
- a ChatGPT client over the effective `cfg.LLM`;
- its tokens from the daemon's credentials store, opened, never created. The path comes from one
  derivation that the daemon and the indexer share, not from a copy of `filepath.Dir(cfg.SkillsDir)`;
- a context carrying `AURA_INGEST_IDENTITY_ID`.

**Opened, never created.** `chatgptplan.New` creates the directory and writes a fresh `host-id`
when there is none (`service.go`, `store.go` `loadHostID`). In the indexer, that would turn every
installation mistake into an empty store, then `ErrAuthorizationRequired`, then "cannot read
here", and finally a scan memoized card-only with no error anywhere:
- a missing mount;
- a path that does not agree with the daemon's;
- a new aura-ingest image running with an old `compose.yaml`, because the updater converges
  images and payload in separate steps.

So `chatgptplan` gains a way to open an existing store and nothing else. It shares `New`'s lock
and record code, and refuses a missing directory or `host-id` with an error. That error is a
failure. So are a missing `AURA_INGEST_IDENTITY_ID` and a store that cannot be read. The indexer
exits 1 on each, and the file is retried until the installation is right. Only
`ErrAuthorizationRequired` and `ErrPlanDisabled`, read from a store that opened, mean that the
owner has not connected ChatGPT.

Exit codes keep their meaning: 3 for "no route", 1 for a failure. A scan stops at its first page
that fails, as today, so a failure on page 4 of 6 indexes nothing and the next cycle retries the
file.

### 4.4 The daemon's image summary

`buildAssetService` gives the image processor the same reader while the chat route is ChatGPT. It
reuses the boot's `chatgptplan.Service` (`chatEnv.chatGPTPlan`), so one process holds one service.
Only the serving daemon's asset service (`serve.go`) gets it. The other two callers never read an
image: `docs.go` ingests with `ModalityDocument`, and `document_open_wiring.go` only gets an
upload by id.
The image processor stamps `asset.IdentityID` on the context it reads the image with, as
`storeForAsset` does for the object. The image summary stays fail-soft, as today.

**The route is read when the image arrives, not at boot** (amended 2026-10-25, during the lab VM
run). This section first kept the boot snapshot until the next restart. The VM showed what that
costs. The daemon booted on Ollama at 20:10:40Z, and the operator then switched the chat to
ChatGPT in the cockpit. The chat followed at once. The upload summary would have stayed on Ollama
(found by reading `visionConfigFrom`), and the cockpit offers no restart for a model switch, so
the summary moved only after a `docker restart aura` at 20:21:57Z. A screenshot attached in chat at
20:32:20Z was then summarised by `gpt-6.1-sol`. The same file, attached at 14:33 while the chat was
on OpenRouter, had been summarised by `deepseek/deepseek-v4.1-flash`.

The chat's route lives on `llm.Runtime`, which the cockpit replaces without a restart. The daemon's
image summary now reads it for each image. When the runtime publishes a new route
(`RuntimeSnapshot.Version` grows), the summary rebuilds its ChatGPT reader and its capability
source from that one snapshot. The reader, the capability and the key therefore never disagree,
which is what the boot snapshot was protecting. Between switches the capability source keeps its
own one-minute cache. As a side effect, a probe that failed at boot no longer leaves the HTTP
primary arm off until the next restart. Read from the code, not measured: an Ollama or llama.cpp
probe that failed is asked again a minute later, and OpenRouter's model-card client caches no
failure, so it asks again on the next image, waiting at most `visionCapabilityTimeout` (5 s)
before the image falls to the sidecar. The sidecar's URL, its
model and the timeout stay boot settings, as today. The other two asset services (`docs.go`,
`document_open_wiring.go`) get no image reader. They never read an image, and they no longer run a
capability probe they never use.

What the amendment does not show: an image already being read when the route changes finishes on
the route it started with.

### 4.5 The credentials in aura-ingest

The CocoIndex child runs in `aura-ingest`, which does not mount `aura-home`. It mounts the volume
at `/var/lib/aura`, read-write, as `aura` and `aura-migrate` do, and sets `AURA_SKILLS_DIR` to the
daemon's value, so that the shared derivation lands on the daemon's `chatgpt-plan`. Read-write,
because a refresh rewrites the sealed file and `lockStore` opens `.lock` read-write.

**Why the whole volume, not a `subpath` of it.** A volume `subpath` mount needs Docker Engine 26.
moby#47687 reports a Compose that mounted the volume root and ignored `subpath`. Docker refuses a
subpath that does not exist (measured 2026-10-25 on Engine 29.8.1). Neither `scripts/install.sh`
nor the updater checks the engine version, and two deployments are not measured: the mini-PC and
Andrea's appliance. On an engine below 26, the updater's `docker compose up -d` would fail on the
whole file. A plain volume mount works on every engine and needs no directory to exist first.

**What it widens.** Little. `aura-ingest` already holds `AURA_AUTHULA_SECRET`, the key that seals
these files. Its `aura_app` database role has `SELECT, INSERT, UPDATE, DELETE` on every `aura`
table, MCP registry and settings included. It also has every identity's Garage binding. Write access
to the skills and plugin trees on `aura-home` reaches no further than those rows already reach.

The payload manifest is regenerated with the compose change (`make payload-manifest`).

### 4.6 A scan's time budget

The bridge gives `aura-media-index` `3 × MULTIMODAL_TIMEOUT_SEC + 30` seconds for any kind
(`services/ingest/media.py` `derive`): 390 s by default. A scan is up to 20 pages, one vision call
each (`maxScannedPDFPages`). At about 30 s a page, a 15-page scan is killed every cycle, and every
cycle reads its first pages again on the owner's plan. For the `pdf` kind, the budget becomes
`(maxScannedPDFPages + 1) × MULTIMODAL_TIMEOUT_SEC + 30`. That is the page cap, plus the probe
page, at the per-call timeout each. `image` and `audio` keep their budget. Each call is still
bounded by `MULTIMODAL_TIMEOUT_SEC` in the Go client, so the larger budget lets the process finish
a scan, and nothing waits longer on any one call.

### 4.7 Not in this change: the sidecar default

`compose.yaml` sets `MULTIMODAL_BASE_URL: http://aura-ocr-vl:8082/v1` on `aura` and
`aura-ingest`, whether or not the `ocr` profile runs. So under compose the local arm always looks
configured, `ErrNoVisionRoute` never happens, and an image that nothing can read retries every
cycle: the 2026-10-23 symptom. The aura-ocr-vl comment in `compose.yaml` claims the opposite
("which ingest degrades on (card-only) instead of failing the file forever").

The default stays, because changing it trades one problem for two:
- **Card-only is memoized.** An unreadable file that retries heals itself on the first cycle
  after its owner connects ChatGPT or a sidecar starts. Once it is memoized card-only, it never
  does, until its bytes change.
- **Deployments may run the `ocr` profile.** Andrea's appliance is not measured. Emptying the
  default would silently turn its OCR into card-only.

This change corrects the comment so that it says what happens, and the gap is recorded as open
(§9).

## 5. Acceptance

Machine-checkable, in the package tests:
1. **The reader sends one message.** It carries the prompt as `input_text` and the image as an
   `input_image` data URL of its MIME type, `store: false` and the configured model. It returns
   the streamed text after `response.completed`.
2. **A model that does not read images** gives "cannot read here", and no Responses request is
   sent. A missing or unlisted entry gives the same.
3. **No account and no grant** each give "cannot read here". So does a stream that carries a
   refusal, even one that ends in `response.completed`. Its text is not returned.
4. **These are failures, not "cannot read here":** no identity, no token source, a failed stream,
   an idle stream, a stream without `response.completed`, a catalogue that did not answer. None of
   them puts the token in its error.
5. **The vision route:**
   - reader text is the answer;
   - "cannot read here" goes to the sidecar when one is set, else gives `ErrNoVisionRoute`;
   - a reader failure is returned, and the sidecar gets no request;
   - `vision_model` names the model that read the image.
6. **Opening the store** (`chatgptplan`):
   - it opens the daemon's existing store and returns its tokens;
   - it refuses a missing directory and a missing `host-id`, and creates neither.
7. **`aura-media-index` with a ChatGPT route:**
   - it reads with the identity named by `AURA_INGEST_IDENTITY_ID`;
   - it exits 1 without that variable, and exits 1 when the store does not open;
   - it exits 3 when the reader cannot read and there is no sidecar;
   - it exits 1 on a reader failure;
   - a scanned PDF's pages are read in order, as today.
8. **The image processor** reads with `asset.IdentityID` in its context.
9. **The bridge's budget** (`services/ingest/tests/test_media.py`): `pdf` gets
   `(maxScannedPDFPages + 1) × MULTIMODAL_TIMEOUT_SEC + 30` s; `image` and `audio` keep
   `3 × MULTIMODAL_TIMEOUT_SEC + 30` s.
10. **The compose contract** (`cmd/aura/container_artifacts_test.go`): `aura-ingest` mounts
   `aura-home` at `/var/lib/aura` and sets the daemon's `AURA_SKILLS_DIR`.

On the lab VM, after the operator approves the update in the cockpit:
- **A. The scan.** The operator uploads `scripts/fixtures/document_retrieval_eval/corpus/verbale_scansionato_italiano.pdf`
  from the cockpit, with the chat still on ChatGPT `gpt-6.1-sol`. Within a few ingest cycles:
  - the file is indexed with no component error;
  - its passages hold `ITALIA-7391`, `48270` and `GIULIA BIANCHI`;
  - the agent, asked "qual è l'importo approvato della pratica ITALIA-7391?", answers 48270 euro
    from the document.

  Claude reads the ingest log and the index back over ssh, read-only.
- **B. An image.** The operator uploads a picture with legible text from the cockpit. Its asset
  row records a summary, with `vision_model` set to the ChatGPT model.
- **C. The account boundary.** Read from the code path and the log: the indexer's ChatGPT request
  ran under the operator's identity, the owner of the document.

If the account catalogue says `gpt-6.1-sol` does not read images, A does not pass. Under compose
the image then goes to the absent sidecar and retries (§4.7). The run records what happened.

## 6. Constraints

- Go and the existing packages only. No new client, and no hand-written HTTP for the Responses API
  (`openai-go` is already the transport).
- Files ≤ 600 LOC; `go vet`, build, race, lint green; the coverage gate's package floors hold for
  `internal/llm/chatgpt`, `internal/chatgptplan`, `internal/multimodal` and `internal/assets`.
- No real ChatGPT call in CI. The tests fake the catalogue and the Responses stream with
  `httptest`, as `internal/llm/chatgpt`'s tests already do.

## 7. Calls made while writing, for the operator to check

1. **The owner's account, per document.** A member's document is read with the member's ChatGPT
   account, never the operator's. Chat inference already resolves each request's own identity
   (`serve_chatgpt_boot.go`). Without an account, the member's documents fall back.
2. **"Not connected" falls back, like a model that does not read images.** Under compose, the
   fallback is the sidecar URL, so the file retries until its owner connects ChatGPT, and the next
   cycle reads it. Without a sidecar URL it is card-only, memoized until the file changes.
3. **A refusal is "cannot read here",** never the page's text.
4. **No sidecar after a ChatGPT failure.** A transient ChatGPT failure retries. It does not hand
   the document to another model.
5. **The catalogue is read once per image,** as the chat already does per turn with media, rather
   than once at boot. It is the only way the daemon can answer per owner. A 6-page scan costs 6
   catalogue reads and 6 Responses calls on the owner's plan.
6. **The whole `aura-home` volume goes into `aura-ingest`** (§4.5). A `subpath` would need an engine
   version no install checks. A new internal endpoint on the daemon would be new API surface.
7. **An installation mistake is a failure that retries, not card-only** (§4.3).
8. **A scan's budget grows with the page cap** (§4.6).
9. **The sidecar default stays** (§4.7). Only its false comment is corrected.
10. **Scans and images go to OpenAI under the owner's ChatGPT account,** as the chat already
   sends attached images. The PII egress spec (`2026-10-13-pii-model-egress-design.md`) is
   proposed and not implemented; this change adds no egress policy.
11. **The upload summary follows a model switch without a restart** (§4.4, amended 2026-10-25 on
   the operator's question "si può evitare il riavvio?"). The media indexer already did, since it
   starts a new process for each file.

## 8. Left to the builder

- The names of the new method, interface and helpers, and which file in each package holds them.
- How the reader tells a refusal apart: its own pass over the stream events, or a refusal mark the
  shared `consumeStream` gains for it.
- How `vision_model` learns which arm answered.
- The reasoning effort: none is sent, so the provider's default applies.

## 9. What this design does not show, and what stays open

- **Other models reading images on the plan.** For `gpt-6.1-sol` the probe measured it (§1). Any
  other model's catalogue entry answers it at run time.
- **Latency and plan cost per page.** Not measured.
- **A page that always fails.** The scan stops there, and every cycle reads the pages before it
  again on the owner's plan. A per-page memo would fix that; none exists, for any route.
- **Re-reading old card-only files.** Out of scope (§2).
- **The sidecar default** (§4.7). Under compose, a file that nothing can read retries every
  cycle. Open, for the operator: card-only that memoizes, against retries that heal themselves.
- **A member identity.** The VM has one identity. The per-owner boundary is covered by the unit
  tests (§5.7, §5.8).
