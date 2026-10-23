# Cloud service credentials: one helper, never the chat profile

Proposed on 2026-10-23 at `64f24df80`.
- **Found by the operator's Task 13 step 7 run** of `docs/superpowers/plans/2026-10-18-document-fields.md`
  on the lab VM, and by the operator's reading of the embedding settings page.
- **Decided by the operator on 2026-10-23** in chat:
  - the shared services keep spending the deployment's services key, because the admin chooses the route
    ("la scelta la fa l'admin quindi basta la chiave principale");
  - one helper serves every one of them ("deve essere un helper comune per tutti", "fai uguale a
    immagini e video consolida gli altri");
  - a manual embedding endpoint gets a credential of its own, stored sealed, so that Mistral's endpoint can
    be tried ("aggiungila nel db così possiamo testare endpoint di Mistral");
  - approved: "procedi pure con la risoluzione dei bachi".

Risk tier: **medium-high.**
- It changes where every cloud embedding, speech and OCR call takes its credential from.
- It adds one sealed settings key.
- No schema, no migration, no change to any vector or to any space.

## The problem, measured

On the lab VM, 2026-10-23, build `f95a8fccb`:
- **The chat route is ChatGPT** (`gpt-6.1-sol`, `GET /api/settings/llm-routes`).
- **The embedding route is OpenRouter** (`AURA_EMBED_MODEL = perplexity/pplx-embed-v1-4b`, row written
  2026-10-20 10:03).
- **The services key is stored.** `OPENROUTER_API_KEY` is a 210-byte sealed row, written 2026-10-02.
- **The ingest embeds.** All 490 passages and 31 documents of the operator's library are stamped
  `es1-b604719dc5fc0ce4`, the 23 office files of the run included.
- **The daemon does not.**
  - The settings page reads "memory space: embeddings: the hosted route has no credential".
  - Every `document_search` of the run answered `status: lexical_only`,
    `degradation_reason: query_embedding_unavailable`.
  - With the filters the question implied, the lexical leg alone found no qualified passage, and the
    search abstained (`f01`, first call).

## The cause, read in the code

- At boot `applySecretSettings` puts the services key into `cfg.LLM.APIKey`
  (`cmd/aura/chat_boot_settings.go`).
- `cfg.LLM.ResolveModelProfile` then runs on `cfg.LLM` itself (`cmd/aura/chat_boot.go`). For the `chatgpt`
  provider, `ResolveChatGPTProfile` sets `APIKey = ""`, rightly, because the services key must never
  reach `api.openai.com`. A route switched live goes the same way (`primaryLLMRouteReloader.Prepare`).
- **Every reader of the chat profile's key loses the services key with it:**
  - the memory embedder and the document query embedder (`liveKey`, `cmd/aura/memory_embedder.go`, added
    2026-09-24 so that a rotated key reached running clients);
  - the embedding settings probes and the model compatibility check (`cmd/aura/serve_embedding_route.go`);
  - cloud speech-to-text and text-to-speech (`config.SpeechCloudRoute` returns `cfg.LLM.APIKey`), on the
    web and on Telegram;
  - the settings page's "has value" for `OPENROUTER_API_KEY` (`EffectiveValue`).
- **Unaffected:**
  - the ingest supervisor, `arcadedb-mcp` and `aura-media-index`, which read the sealed row themselves;
  - image and video generation, which spend the identity's own key (`mediaCredentials`).
- **Only the ChatGPT profile clears the key.** On an Ollama or llama.cpp chat route `cfg.LLM.APIKey` still
  holds the services key, so there the embedders work.
- **Two more defects, read in the same code:**
  - `config.ResolveEmbedRoute` hands the credential to any hosted route. On a manual endpoint the services
    key went to whatever URL the admin typed.
  - Vision has two arms: the primary chat route, with `cfg.LLM.APIKey`, or the local OCR sidecar
    (`internal/multimodal/vision.go`). `aura-media-index` puts the services key into `cfg.LLM.APIKey` for
    that call. On an Ollama chat route whose model reads images, the services key goes to the Ollama host.

The 2026-09-23 spec (`2026-09-23-embedding-model-change-design.md` §5, "the daemon's embedders take the
credential from the running LLM profile") is what this replaces. Its §6, "no embed-specific key exists"
(`bd31e157b`), still holds for OpenRouter: the manual endpoint's key serves a provider the services key
cannot reach, so it can never be a stale copy of it.

## Decisions

1. **Who pays (operator).**
   - The shared cloud services spend the deployment's services key, `OPENROUTER_API_KEY` (`aura-services`):
     embeddings, cloud speech-to-text and text-to-speech, and vision through the primary route when that
     route is OpenRouter. This is the rule of `prd.md` (2026-09-27), unchanged.
   - Image and video generation keep spending the identity's own key.
   - **The services key goes only to OpenRouter (my call, from the defects above).** Vision through a
     primary route on any other host sends no services key.
2. **One helper (operator).**
   - Every one of those services takes its credential from one helper in `internal/settings`. It reads the
     sealed row on every call, the way generation reads the identity's key, with the process environment
     as the fallback for an absent row (the fallback `settings.EmbedRoute` already has).
   - Nothing reads the chat profile's key for a service any more: `liveKey`, `SpeechCloudRoute`'s key and
     `EffectiveValue`'s `OPENROUTER_API_KEY` case go.
   - The ingest supervisor, `arcadedb-mcp`, `aura-media-index` and `aura doctor` move onto the same helper,
     so there is one path.
3. **A manual endpoint's own key (operator).**
   - New sealed setting `AURA_EMBED_CLOUD_API_KEY`, beside `AURA_EMBED_CLOUD_BASE_URL`.
   - The helper gives each embedding route its own credential: none on the local sidecar, the services
     key on OpenRouter, `AURA_EMBED_CLOUD_API_KEY` on a manual endpoint.
   - **A manual endpoint never receives the services key.**
   - **How it is written (my call).** Like `AURA_OPENROUTER_MANAGEMENT_KEY`: its own sealed row, saved
     through the generic settings write, never sent back to a browser. The cockpit's manual arm shows it
     with the existing secret field and its existing behaviour. It is not a route key, because it does not
     change the space, so it needs no preview. The preview and the apply read the stored key, so the admin
     saves the key before the preview.
   - **A manual endpoint needs its key (my call).** Without one, the preview refuses with `key_missing`, as
     today. A keyless self-hosted endpoint is a separate request. Today `embeddings.Client.hosted()` means
     both "is a cloud route" and "has a credential", and `services/ingest/embed.py` refuses a model with no
     key; a keyless endpoint would need both split.
4. **Rotation (my call).** Because the helper reads on every call, a services key the reconciler replaces
   reaches the daemon's embedders and speech clients on their next call, with no restart. `arcadedb-mcp`
   and the ingest children keep their restart-on-change, which already compares the credential by hash.
5. **A missing key stays visible (my call).**
   - `embeddings.Route.Space` keeps refusing a hosted route with no credential.
   - With the key taken from the right place, the settings page's error disappears when the key exists and
     stays when it does not, as the 2026-09-23 spec §5 wants ("the cockpit names the missing key").
## Acceptance

Machine-checkable, each by a test that fails before the change:
1. With the chat profile's key empty (the ChatGPT profile), the memory embedder, the document query
   embedder, the embedding route probe and compatibility check, cloud STT and cloud TTS each send the
   services key read from the settings store.
2. A services key changed in the store is the one the next call sends, with no rebuild of the client.
3. On a manual endpoint, the request carries `AURA_EMBED_CLOUD_API_KEY`, never `OPENROUTER_API_KEY`; with no
   endpoint key the route is refused with `key_missing`.
4. With no `OPENROUTER_API_KEY` row, the key in the environment and the chat on ChatGPT, `GET /api/settings`
   reports the key as set. (With the row present it already does.)
5. `AURA_EMBED_CLOUD_API_KEY` is stored sealed, and no settings or preview response carries its value.
6. Vision through the primary route sends the services key when the primary's base is OpenRouter, and no key
   on any other host.

Live, on the lab VM with the chat on ChatGPT, after the build reaches it through the updater:
7. The embedding space panel names the space with no error.
8. `document_search` answers with its dense legs (`status` not `lexical_only`).
9. Task 13 step 7 of the document fields plan runs on that build.

## Not in this change

- The chat route's own credential: what a chat request to an Ollama or llama.cpp host carries is the chat
  client's business, not a service's.
- A ChatGPT primary reading images: its vision call has no API key to send in either version.
- Trying Mistral's endpoint. This change makes it possible. The run reads Mistral's embeddings
  documentation first: its width parameter and its native width decide whether the 768-wide space can use it.
- The "Configurato" badge, which says a model is chosen, not that the route works.

## What this does not prove

- The ChatGPT case is the measured one. On an Ollama or llama.cpp chat route the embedders worked before
  this change; the services key reaching the Ollama host through vision is read in the code, not measured.
- A per-call read of a sealed row costs one Postgres read and one decryption per embedding or speech call.
  It is not measured here; the operator's rule is accuracy over latency on the lab VM.
