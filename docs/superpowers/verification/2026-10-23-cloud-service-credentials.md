# Cloud service credentials, measured on the lab VM — 2026-10-23

Plan `docs/superpowers/plans/2026-10-23-cloud-service-credentials.md`, spec
`docs/superpowers/specs/2026-10-23-cloud-service-credentials-design.md`. The build is `d46cedab6`,
pushed from WSL with the pre-push gates. CI, Skills, CodeQL and "Publish Aura edge image" were
green on that SHA. The VM's updater started it at 10:32:23Z.

## Gates before the push

- `make quality` (WSL) passed on `d46cedab6`: deadcode, vet, build, file-size, capability
  declaration, embedding and LLM model contracts, lint, test-race and vuln.
- `bash scripts/coverage_docker.sh`: owned internal coverage **48473/55452 (87.4%)** ≥ 85%.
  The package-local policy passed and no pinned denominator moved, so
  `scripts/coverage_package_policy.json` is unchanged.

## The VM, chat on ChatGPT

Settings on both runs:
- `AURA_LLM_PROVIDER=chatgpt`, `AURA_LLM_MODEL=gpt-6.1-sol`;
- embedding route OpenRouter `perplexity/pplx-embed-v1-4b`;
- the services key stored as a sealed row.

| Measure | Before: image `5fb0a36b7`, about 08:50Z | After: image `d46cedab6`, 10:32Z onward |
|---|---|---|
| Boot `embedding space` lines, memory and documents | `space=""`, `space_error="embeddings: the hosted route has no credential"` | `space=es1-b604719dc5fc0ce4`, label `openrouter perplexity/pplx-embed-v1-4b, 768d, recipe 1`, `space_error=<nil>` |
| `GET /api/settings/embedding-space` | `space_error`, 0 tenants | space named, no error, 2 tenants |
| Space of the operator's tenant | not readable | memory: 25 facts, 56 turns, 16 traces in space, 4 traces without a vector. Documents: 490 passages and 31 cards in space |
| Memory embed backfill | failed at 08:24:33, 08:29:33, 08:35:03, 08:40:33 and 08:46:03 with "hosted route has no credential" | ran at 10:32:31 with `count 0`, no error |
| `retrieval degraded` / `query_embedding_unavailable` / credential lines in the aura log | present on every `document_search` | none since boot |
| One agent turn, `f01` of the document filter set, through `POST /agent/run` | 3 `document_search` calls, each `status: lexical_only`, `degradation_reason: query_embedding_unavailable`, 0 hits, 35.8 s | 1 call: `status: complete`, `abstained: false`, documents returned, no degradation reason, 19.8 s. The answer names the four gold invoices |

Acceptance 7 (the space named with no error) and 8 (`document_search` with its dense legs) hold.

The brief also lists `aura docs search` from the CLI in the container, and it answered `complete`
on both builds: before the change the CLI already put the stored services key into its own
`cfg.LLM.APIKey`. So that path does not tell the builds apart. The ledger records the ruling that
measures acceptance 8 on the daemon's embedders instead.

Acceptance 9, the document fields run of Task 13 step 7, ran on this build. Its record is
`2026-10-23-document-fields-lab-vm.md`.

## Found during the run, not caused by this change

After the cleanup and re-upload of step 7, the scanned PDF `verbale_scansionato_italiano.pdf` was
not indexed. The ingest reported one component error:

`media indexer exited 1: describe scanned PDF page 1: Post "http://aura-ocr-vl:8082/v1/chat/completions": dial tcp: lookup aura-ocr-vl ... server misbehaving`

- **The cause, read in the code.** With a ChatGPT primary, `llm.NewContentCapabilitySource`
  returns nil, because it has no case for the ChatGPT target. `PrimaryAcceptsImages` is then
  false, and the vision client falls back to `MULTIMODAL_BASE_URL`, the local OCR sidecar. The
  VM does not run one.
- **The same branch before this change.** The capability source never read the key, so the
  branch was the same on the old build.
- **Why the morning upload worked.** It indexed this PDF at 06:37:42Z. The chat moved to ChatGPT
  at 06:42:19Z (the `AURA_LLM_PROVIDER` row's time), after that upload.
- **Already in the spec.** Its "Not in this change" names this case: a ChatGPT primary reading
  images.
- **The operator's decision, the same day:** when the chosen model reads images, the media indexer
  uses it, ChatGPT included. Aura's ChatGPT client already sends `input_image` on the plan and
  reads image support from the plan's catalogue. This is a slice of its own.

## What this does not show

- **The Ollama vision path is not measured.** That the services key reached an Ollama host
  through vision is read in the code.
- **No manual endpoint was tried.** There was no Mistral run, so `AURA_EMBED_CLOUD_API_KEY` is
  covered only by unit, handler and web tests.
- **Rotation is not measured on the VM.** It is covered by the store-rotation tests.
- **The cost of the per-call read is not measured.** Each embedding or speech call now does one
  Postgres read and one decryption.
- **Cloud speech-to-text and text-to-speech were not called live** in this run.
