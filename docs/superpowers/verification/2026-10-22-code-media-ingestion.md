# Code and media ingestion on the running stack

Date: 2026-10-22. Aura backend: `52cb5dbaf8433886ef9b4c427962874eafd0cf77`.
The ingestion container remains the existing GHCR deployment; extraction code was not changed.

Nine generated fixtures were uploaded through the existing file-manager multipart endpoint into
one new `uploads/ux-study-code-3dad072d-4f38-4599-8442-57cccd3132e2/` namespace. All nine writes
returned 200 and the actual source key. No pre-existing file was overwritten.

| Fixture | Indexed category | Measured extracted content |
|---|---|---|
| Python, Rust, C++, IEC Structured Text | Code | Original source, including the synthetic search marker |
| XML | Code | Text nodes, without attributes or the original markup |
| HTML | Code | Page text, without the original markup |
| SQLite bytes named `.db` | Code by suffix | Zero passages; binary source was not interpreted as text |
| MP4 | Video | Existing metadata-only ingestion |
| WAV, two-second synthetic tone | Audio | Indexed after STT recovered; no speech was expected |

The first eight files appeared after the next scan. Audio initially failed because the configured
`aura-stt` service was stopped (Docker reported it exited three days earlier). The indexer logged
the unavailable STT host, then connection refusal during startup. Starting that existing service
with `docker compose up -d --no-deps --no-build aura-stt` pulled its configured GHCR digest and
reused its model volume. Docker reported healthy; a direct transcription of the synthetic tone
returned `{"text":""}`. The same uploaded file then indexed automatically on a subsequent scan.
No second upload or extraction-code change was needed.

The actual status stream included `ready` with eight in-progress components before the files
appeared, and later retained two reported errors even after the audio became indexed. Therefore
the UI must show the recorded component counts without equating them to failed files or claiming
every upload is searchable from the aggregate status alone. Exact source-key lookup establishes
the readiness of each upload.

This run establishes that the retained pipeline handles these samples. It does not establish
all source encodings, every media codec, or transcript quality. Original code rendering is a
separate UI check: the XML/HTML results demonstrate why indexed passages cannot replace the
original-source viewer, and the `.db` result demonstrates why previewing must detect binary bytes.
