# Native document preview API verification

Date: 2026-10-21. Task 2 of the Paperless Documents plan, following the user's instruction
to reuse Craft, include Office and video, and preserve extraction.

## Implementation boundary

`GET /api/documents/{id}/thumbnail` authorizes the indexed document before opening its
identity-scoped object. Current object attributes supply the byte count; stale index size
does not control the copy. Originals stream through the existing SandboxRouter into a
unique `/workspace/scratch/document-preview-<UUID>` directory. Separate native processes
and request-local FilmCraft projects/data directories preserve mounted editor sessions.

Native paths are PdfCraft batch rendering, WordCraft PDF conversion, GridCraft's first
sheet fitted to one page, DeckCraft's first-slide PDF export, PhotoCraft's native serve
preview and FilmCraft probe/import/sequence/frame rendering. No host renderer or external
source URL is accepted. FilmCraft uses the returned media id and refuses offline placeholders.

Bounds: 32 MiB original; one request admitted before source opening/materialization;
30-second operation context; 64 MiB per native output file; 64 KiB captured command logs;
2 MiB returned PNG; 480-pixel output edges. Raster headers use existing codecs with a
1 MiB header budget and 64MP/32768-edge checks before native pixel allocation. FilmCraft
metadata gets those dimension checks before import. Output PNG headers are checked before
the bounded pixel decode. Sources, archive streams and scratch files close or clean up on
success, error and cancellation; cleanup uses its own five-second context. Existing native
sandbox CPU/memory/process limits apply; these are not a per-document memory benchmark.

Thumbnail responses are `image/png`, `private, no-store`, and `nosniff`. Status codes:
200 success; 404 missing/foreign document or missing original; 413 byte/dimension cap;
415 unsupported type; 422 invalid native input/output; 503 unavailable native transport/tool;
504 deadline; 408 cancellation; 500 other failures. Error bodies expose codes, not native
logs, keys or credentials. Query version hints do not create a preview cache.

`GET /api/documents/ingestion` reuses `DocumentIndex.IngestState` and its tenant resolution.
It returns the original fields in `{indexing: state|null}`. Absent state is null, query
failure is 500, and unwired reads are 503. Counts remain work components, not files;
`ready` with nonzero errors remains visible. Extraction and publication are unchanged.

## Native SDK integration

The committed `craft_integration` test uses existing synthetic repository fixtures plus
generated PNG/Markdown/RTF/CSV. An explicit test-box variable selects an already running
Craft sandbox. Only its resolver is replaced: streamed copy-in, ExecStream cancellation,
copy-out and PNG validation use production transport. The test creates/reconfigures no box
and removes only its request scratch directories. Optional public fixtures expand coverage.

The expanded run passed 18 fixtures, six corrupt-format refusals and actual subprocess
cancellation. Every returned PNG decoded, fit 480px, stayed below 2 MiB and had nonuniform
pixels. Representative results, including transport:

| Source | Output | Time |
|---|---|---|
| Committed synthetic PDF | 340x480, 22,044 bytes | 0.244s |
| Public ANAC DOCX | 340x480, 24,903 bytes | 0.471s |
| Synthetic legacy DOC | 371x480, 5,841 bytes | 0.220s |
| Synthetic ODT | 371x480, 6,111 bytes | 0.232s |
| Committed XLSX | 371x480, 12,158 bytes | 0.220s |
| Public FCFF XLS | 371x480, 34,839 bytes | 0.325s |
| Synthetic ODS | 371x480, 4,270 bytes | 0.221s |
| Public EU-OSHA PPTX | 480x270, 47,905 bytes | 0.527s |
| Public 19MP JPEG | 480x367, 310,349 bytes | 0.736s |
| Public Sintel MP4 | 480x270, 70,076 bytes | 1.472s |

RTF, Markdown, CSV, synthetic PPTX and PNG also passed. Visual native/browser observations
are separately recorded in the Craft document, Office and video verification files.
These single runs establish the tested paths, not a latency SLA or all codec/import fidelity.

GridCraft intentionally accepts plain text named `.xlsx` as CSV. The initial corrupt-input
test wrongly assumed otherwise; the corruption fixture was corrected to a malformed ZIP
package, preserving the native detection behavior. Legacy XLS/ODS imports can lose layout;
Word exports all pages before previewing the first; font substitution can alter pagination.
Unsupported types, oversized sources and failures retain their existing full viewer.

The final default matrix also includes a committed synthetic MP4 containing eight gradient
frames and no third-party footage. The offline CI-parity race run passed all nine default
fixtures, six corruption refusals and cancellation in5.560s. Synthetic video output was
480x270/103,489bytes in0.515s. The existing sandbox CI job now runs this tier after its image
build, using an offline disposable2CPU/2GB box; missing configuration fails under CI.

## Go verification

- Whole `go vet ./...`, `go build ./...`, and `go test -p1 ./...`: PASS. The later tiny
  cancellation-start classification fix passed fresh package vet/build and focused tests.
- Serial final race: documents4.723s, agui37.559s, cmd/aura94.985s: PASS.
- Native offline CI-parity race with the existing image and unconditional video: PASS5.560s.
- Disposable Postgres cross-consumer db_integration coverage: all six consumer suites PASS;
  documents **1015/1159 statements =87.575496%**. Native `go tool cover -func` reports87.6%
  after merging the855 distinct blocks across5130 repeated profile rows. This uses one
  denominator and unioned positive counters, not averaged consumer percentages. AGUI unit
  coverage is86.1%. Existing target policies remain unchanged; full release covdata gating
  remains Task5.
- Seven valid isolated overlay mutants killed (100%): input cap, source dimension cap,
  native output size, returned FilmCraft id, PhotoCraft failure, object identity and archive
  identity. Production source was never mutated in place.
- CI YAML parsed. `/proc/self/limits` confirmed the native shell file cap is67,108,864bytes.

Full commands, RED/GREEN observations and logs are in the ignored task report. Browser
acceptance, publication and CI on the eventual pushed commit remain controller-owned work.
