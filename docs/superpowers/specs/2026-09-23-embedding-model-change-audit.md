# Adversarial audit — `2026-09-23-embedding-model-change-design.md` at `06deb3577`

Auditor: an independent Claude (Opus) subagent, 2026-09-23. It was briefed to reject the
design if the evidence said so, and had read-only access to the repo and the lab VM
`192.168.101.158`. Codex could not be used: its sandbox cannot initialise from a
non-interactive shell on this host.

**Outcome:** the design was rejected as implementation-ready. It was revised the same day; the
current spec answers every finding below. Where the design changed shape, the answer is
decision 5 (every vector carries its space) in place of the marker, clear and grace window.
The blocking findings F1, F2 and F3 were re-verified by the author before revising:
- F1: `IngestStatus` read `ready` with `errors=14` while `prompt.txt` failed every cycle.
- F2: the memory embedders use the 30 s `arcadedb` timeout, and `OverlayEnv` only calls
  `Setenv`.
- F3: the VM's `AURA_LLM_BASE_URL` points at Ollama, and `config_routes.go:31-34` is the fallback.

Legend: **[V]** verified by code, docs or measurement. **[S]** predicted from the mechanics
but not exercised.

**Scope and method.** HEAD was `8e640edbe`; the only commit after `06deb3577` touches
`mcp_live_mount*`, `serve_governance_write*` and `McpInstallPanel*`, which the spec does not
depend on. Every file:line citation was re-read.

Work on the VM, all read-only:
- SELECT queries on ArcadeDB and Postgres;
- container env, logs and inspect output;
- a copy of the installed cocoindex 1.0.24 source;
- a throwaway CocoIndex spike in `aura-ingest:/tmp`, with its state in `/tmp`, removed
  afterwards and verified removed.

`aura` restarted at 13:00:28Z during the audit; the auditor did not cause it.

Upstream sources: the OpenRouter catalogue (fetched live), ArcadeDB `full-text-index.adoc`
from `arcadedb-docs`, and cocoindex `rust/core/src/engine/component.rs` at tag v1.0.24.

## Findings, ordered by damage

### F1 (critical). A process-level ingest status cannot prove the space of individual document rows

**Breaks:** the §2 proof rule, §5 step 4, §6 `record_status`.

**Evidence:**
- [V] `component.rs` (v1.0.24) evaluates `let ret = ret?;` before `submit(...)`. A component
  whose body raises never submits, so its previous target rows stay in place.
- [V] `coco.map` (`api.py:553-566`): "No processing components are created… the first
  failure… is raised". One failing chunk therefore fails the whole file.
- [V] Spike: `mount_each(process_file, memo) → coco.map → _embed(memo, batching, deps=…)` with
  a `localfs` target. With `deps` A→C and one file's inputs failing, the run ended
  `STATUS ready`, `num_errors=2`, and **both files kept their deps-A rows**.
- [V] The healthy file failed too. Its `a0` shared a CocoIndex batch with the failing `b0`,
  because `as_async(batching=True)` groups calls across files. Production `_embed_batch`
  raises a plain `RuntimeError` (`app.py:281-286`), so one bad input fails up to 32 chunks
  from several files.
- [V] VM `IngestStatus`: `status=ready`, `errors=39`, `finished=471`. The logs show the
  zero-byte `Programma/prompt.txt` failing once per live cycle: 39 failures in 39 minutes,
  on a counter that accumulates per process. `ready` stays set in live mode.

**Predicted failure:** after a route change, any file that fails keeps its old-space rows.
Causes include a cloud 400 or 429, a bad input in the same batch, or an unreachable OCR/STT
route. `IngestStatus` still says `ready` with the new `embed_space`, so the marker promotes a
mixed corpus. The same gap lets the initializer self-certify a legacy corpus. If
`errors=0` were added to the rule instead, the pass could never complete on this VM.

**Smallest safe fix:**
- Stamp every Passage and IndexedDocument row with `embed_space`.
- Make completion `count(... WHERE embed_space IS NULL OR embed_space <> :target) = 0`.
- Filter dense reads by space.
- In `_embed`, raise `coco.RetryWithSmallerBatch() from err`, which is public in 1.0.24.

**Disposition:** decision 5 and §2/§3 stamp every row; §6 adopts `RetryWithSmallerBatch`; a
failing file keeps the documents family lexical and is named in the cockpit.

### F2 (critical). The grace window is not a fence

**Breaks:** §3, §5 step 1, and the claim that `embedding IS NULL` is an exact work queue.

**a) One guarded `Embed` call can span many request timeouts** [V]:
- `Embed` issues its batches in sequence, each with its own timeout
  (`embeddings/client.go:85-106,160`).
- The catalogue fetch is retried on every call until it succeeds (`fit.go:31-43`).
- Each over-limit input costs one `/tokenize` request (`fit.go:99`).
- MCP `memory_reembed` embeds up to 100 statements in one call (`MaintenanceBatch=100`), then
  `writeVectors` updates by `@rid` unconditionally (`memory_vector.go:473-488`).
- The memory embedders use `arcadedb.DefaultTimeout` (30 s), not `embeddings.DefaultTimeout`.

**b) The batch paths wait between embedding and writing** [V]. `ApplyMemoryBatch` and
`ApplyAcceptedCapture` embed before taking the per-identity lock, then allow up to 21 conflict
attempts with backoff (`memory_batch.go:334-357`, `write_retry.go:31`). `UpsertFact` retries
the same way.

**c) The copy path never passes the guard** [V code, S interleaving].
`storedStatementVectors` reads stored vectors before the lock (`memory_batch_store.go:35-74`)
and writes them later (`memory_batch_state.go:117-118`).

**Predicted failure:** old vectors land after the clear, carry no mark, are never refilled,
and get promoted.

**Fix:** a write-time check inside ArcadeDB, copying only in `ready(own)`, or stamping the Go
rows, which removes the need for a clear or a grace window.

**Disposition:** stamps (decision 5). Writers never refuse; stale vectors carry their stale
stamp and are re-embedded; the copy path filters by stamp.

### F3 (high). The OpenRouter option actually targets the chat LLM's base, Ollama on the VM

**Evidence** [V]:
- `ResolveEmbedRoute` uses `sharedCloudBase(llmBaseURL)` when `AURA_EMBED_CLOUD_BASE_URL` is
  empty (`config_routes.go:26-35`), and the OpenRouter option clears that key
  (`embeddingBackendState.ts:14-21`).
- The VM's `AURA_LLM_BASE_URL` is `http://host.docker.internal:11434/v1`, with
  `AURA_LLM_PROVIDER=ollama`.
- `AURA_LLM_BASE_URL` is a hot LLM-profile key.

**Predicted failure:** the E2E would embed against Ollama and never complete. Changing the
chat LLM silently re-routes embeddings.

**Fix:** resolve to `llm.DefaultBaseURL`, never to the chat base.

**Disposition:** §0, as a separate first commit; it is a live defect.

### F4 (high). A new confirmation does not reset `cleared_at`

An A→B→C sequence would skip the clear for C and promote B vectors as C. There was no
"route change mid-pass" test.

**Disposition:** no clear exists any more. A→B→C is correct by stamps and is tested.

### F5 (high). The relevance floors are calibrated for EmbeddingGemma only

[V] Memory uses `DenseMaxDistance 0.72` and `MinRelevance 0.28` (`client.go:66-77`); documents
use `RelevanceFloor 0.32` ("one embedder, one day", `document_schema.go:55-79`). Another model
has another distance distribution, so abstention becomes arbitrary.

**Disposition:** §9. The floors are keyed by model, other models get the reason
`uncalibrated_floors`, the E2E records the first calibration datum, and the gap is disclosed.

### F6 (high). Width and `dimensions` support are never checked before confirming

[V] None of the 37 catalogue models declares its output width, and `supported_parameters` is
empty for all of them.
- Models narrower than the width (384-d MiniLM) fail `TruncateMRL` on every embed.
- Wider models that were not trained for truncation (`ada-002`, `mistral-embed`) are silently
  cut.
- One model id can be served by several providers.

**Disposition:** §4 adds a live probe on synthetic text, refuses narrower models and warns on
wider ones. Provider pinning is out of scope and disclosed.

### F7 (high/medium). A document re-embed re-runs full extraction, including billed vision and STT

[V] `media.derive` is not memoized (`media.py:64-72`), and `_card` and `extract_scanned_pdf`
memoize on a temporary path. On the VM that means 3 PNGs through vision and 5 identical MP4s
through CPU Whisper. The "three minutes" estimate counts embedding only.

**Disposition:** §6. `_extract` is memoized by content, so `deps` re-runs chunking and
embedding only. The first deploy re-extracts once, stated in the release note.

### F8 (medium). Lexical mode, as specified, returns bad results on this corpus (measured)

[V]:
- `$score` under `OR` depends on predicate order: 1.3798 vs 1.2880 for the same document.
- Nothing de-duplicates: five identical transcripts outrank the PID manual for a PID question.
- Nothing sets a floor: an out-of-corpus question scored 4.45.
- The merge rule is unspecified.

**Disposition:** §8. Two card queries merged by max, grouping by `raw_sha256`, a floor,
explicit ranking, and IT/EN acceptance with abstention.

### F9 (medium). Byte-counted chunks shrink passages about 3.6x on every hosted route

**Disposition:** §6. Chunking always uses the local tokenizer (`AURA_EMBED_TOKENIZER_URL`), so
boundaries never move. On a hosted route inputs are cut to the limit in bytes, as the Go client
does, and the preview reports how many passages would be cut. The limit is read once per route
change.

### F10 (medium). Re-reading settings through `OverlayEnv` cannot see a deleted row

[V] `OverlayEnv` never unsets (`settings.go:360-373`). The exit code on the shutdown-timeout
path is 1, not 0. The design cannot crash-loop.

**Disposition:** a pure route helper that distinguishes an absent row from an empty one and
never touches the environment; the exit code is stated as 0 or 1.

### F11 (medium). The pass has a fixed throughput cap, and one bad record blocks promotion forever

[V]:
- The sweep does at most 20 rounds × 32 records per run, every 5 minutes.
- One rejected input fails the whole call, and the fill re-selects the same `LIMIT` set.

[S]:
- The turn reconciler would re-embed turns in parallel with the pass, billing them twice.
- Tenants late in the order starve.

**Disposition:** §5. A cursor, the round cap lifted while mismatches remain, a kick at boot,
rotating tenant order, batch halving, and 4xx quarantine. The reconciler fills only turns
without a vector.

### F12 (medium). Marker initialisation is under-specified

**Disposition:** no marker exists any more.

### F13 (Gate 1). The file table misses files, one already at 597 lines

`memory.go` (597), `retrieval_rank.go`, doctor wiring, `cmd/arcadedb-mcp/main.go`,
`tool_memory_recall.go` and the i18n files were missing.

**Disposition:** all are listed. `memory.go` is explicitly not touched.

### F14 (low). The colon-separated space string can collide

**Disposition:** a canonical JSON object, hashed.

### F15 (low). The local fingerprint is less trustworthy than stated

[V] The updater never touches `AURA_EMBED_FINGERPRINT`, and llama.cpp `/v1/models` reports
size, parameter count, width and quantization.

**Disposition:** §1 attests the local model at run time.

### F16 (low). Implementation traps

- `observed_at` is an ISO string.
- The vertex types use `REMOVE`, not `SET NULL`.
- Zero-vector cards need care.
- `AURA_EMBED_API_KEY` would be inherited by subprocesses.

**Disposition:** `IngestStatus` is no longer used, and no clear exists. Zero-vector cards carry
the current stamp and are never counted as mismatched. The key is removed from the environment
after it is read.

## The first spec's measured claims

| Claim | Verdict |
|---|---|
| Settings row, sidecar command line, container env, counts and characters | VERIFIED |
| "Three minutes" | IMPRECISE: embedding only (F7) |
| `IngestStatus` and `MemoryBatchReceipt` are per-database singletons | IMPRECISE: receipts are one per idempotency key |
| `deps` behaviour and docstring | VERIFIED; `function.py:629` is the internal helper, the docstring is at ~2060-2081 |
| "`deps` fires on nothing else" | WRONG: it triggered full re-extraction (F7) |
| Catalogue limits reach 32,768 | WRONG: the maximum is 131,072 |
| "Every Go vector write goes through `embedder`" | WRONG: the copy path does not (F2c) |
| Turns and traces have no null fill | IMPRECISE: the reconciler re-embeds turns that have no vector |
| Grace = 80 s | IMPRECISE: 30 s memory timeout; no per-call bound |
| "The updater converges `.env`", used as assurance for the fingerprint | IMPRECISE (F15) |
| Other citations, `434cdd539` figures, fix-on-touch items | VERIFIED |

## Findings of the 2026-09-21 review that the first spec left open

- #2 (identity is the effective transform): partly answered.
- #4 (fence): not answered.
- #5 (lexical merge): partly answered.
- #9 (test matrix): partly answered.
- #11 (correctness assumptions): partly answered.

The revision answers all five: runtime attestation and the OpenRouter base; stamps instead of
a fence; the §8 merge; the expanded test matrix; F1 and F5 disclosed.

## Verdict (at `06deb3577`)

Reject as implementation-ready. The direction was sound: `deps` re-embedding is confirmed,
including the removal of stale rows and the change back, and the supervisor seam and the MCP
drain-exit are right. Three things blocked it:
- document completion proven per process, while CocoIndex fails per file;
- a grace window that is not a fence;
- an OpenRouter option that targets Ollama.
