# Document Pipeline Rewrite — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Aura's hand-built document ingestion (Docling + 9,023 LOC of stage machine, leases, retry backoff, orphan sweep, durable-delete workers and an unread tombstone scheme) with three specialised engines — iscc-tika extracts, CocoIndex reconciles, ArcadeDB retrieves — keeping PRD amendment #114's contract intact and closing only on amendment #115's production gate driven by the real agent.

**Architecture:** A Python sidecar runs a CocoIndex app whose incremental state lives in LMDB on a volume; `coco.auto_refresh` makes any source live. Extraction is iscc-tika, with LibreOffice normalising legacy Office formats first. Passages and their embeddings are written by CocoIndex's **stock `neo4j` target connector** over ArcadeDB's Bolt plugin — no writer of ours — into a schema created *before* any write. Retrieval is ArcadeDB's own vector kit, reached over HTTP by Aura's Go client. Aura keeps identity, tenancy, tool contracts and sandbox delivery. Two sidecars (`aura-docling`, `aura-rerank`) are deleted.

**Tech Stack:** Python 3.12 + cocoindex 1.0.19 + iscc-tika 0.6.0 · Go 1.26 · ArcadeDB 26.7.3 (`LSM_VECTOR` + `ARRAY_OF_FLOATS`) · PostgreSQL 18 (control plane, fail-closed RLS) · llama.cpp sidecars (EmbeddingGemma-300M embed, Qwen3.5-9B answer) · Garage S3 · Docker Compose.

**Two protocols on purpose, and the reason is not symmetry.** The **Bolt plugin is load-bearing**: `cocoindex.connectors.neo4j` is a full target (`mount_table_target`, `mount_relation_target`, `ConnectionFactory`) that speaks Bolt, so enlisting the plugin is what buys the ingestion writer for free. Removing it would force a bespoke writer — the exact thing amendment #118 exists to stop. Aura's **own** client keeps using the HTTP API at :2480, which `internal/arcadedb/client.go` already does and which is verified on 26.7.3 for DDL, parameterised 768-float writes, and `vector.neighbors` with a RID filter. Different callers, different surfaces; neither replaces the other.

## Global Constraints

- **PRD amendment #118 is the contract.** #114's guarantees (provenance-bearing passages, durable erasure, cross-tenant non-disclosure) are NOT reopened. Only the implementation changes.
- **The closing gate is amendment #115's production E2E, above 98%, driven by the real Aura agent.** A green unit suite explicitly does not close any phase. No mock, skip, fallback or degraded status may appear in the gate run.
- **The E2E answering model is LOCAL:** `unsloth/Qwen3.5-9B-GGUF` → `Qwen3.5-9B-UD-Q4_K_XL.gguf`, 5,966,095,584 bytes, repo commit `3885219b6810b007914f3a7950a8d1b469d598a5`, SHA-256 `6f5d30666c2d8ae16a306e616d95341dcf3cc46810df84d7e6f5a7d1e4c1b293`, native context 262144 (served at `-c 32768`, the conservative value load-tested stable on the 12GB RTX 3060, not the native ceiling). Served by `aura-llm` on :8084 with `AURA_LLM_BASE_URL=http://aura-llm:8084/v1`, from a pre-fetched LOCAL path (not `--hf-repo`). **No OpenRouter spend in any gate.** SUPERSEDES an earlier pin of `Qwen/Qwen3-8B-GGUF`'s `Qwen3-8B-Q4_K_M.gguf` (5,027,783,488 bytes, commit `7c41481f57cb95916b40956ab2f0b139b296d974`, declared context 40960): the human changed the served model during Task 7 execution (2026-08-07) before that artifact was fully downloaded. The 40960 figure belonged to Qwen3-8B and does NOT apply to the deployed Qwen3.5-9B.
- **The chunker's hard ceiling is 2048 TOKENS** — EmbeddingGemma-300M's GGUF declares `context_length = 2048`; a longer input is refused with HTTP 500. Never reason in bytes.
- **Embedding prefixes are asymmetric and mandatory:** documents `title: none | text: …`, queries `task: search result | query: …`. Omitting them measured recall@1 0.25 → 0.05.
- **Never pass `--pooling`.** The GGUF declares its own pooling and llama.cpp obeys it when the flag is absent.
- **Migration numbering:** run `ls internal/db/migrations/ | tail -1` and use the next integer. Never deduce, never copy a number from any document. Current tail is `0093_document_pipeline_convergence`.
- **File size ≤600 LOC**, enforced by a pre-commit hook. Refactor on touch.
- **Coverage floor 85%** across the tag matrix; verify locally with `bash scripts/coverage_docker.sh` BEFORE pushing.
- **Run `.sh` gates in WSL**, not Git Bash: `wsl -e bash -lc 'cd /mnt/d/Repo/Aura && export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH" && …'`.
- **`MSYS_NO_PATHCONV=1`** on every `docker run` that passes an absolute path, or Git Bash rewrites it silently.
- Commit messages: imperative subject, body explaining *why*, `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.

---

## File Structure

**Created**

| path | responsibility |
|---|---|
| `docker/aura-ingest/Dockerfile` | the CocoIndex sidecar image: python 3.12 + cocoindex + iscc-tika + LibreOffice |
| `services/ingest/app.py` | the CocoIndex App and its `auto_refresh` wrapper — the ONLY entry point |
| `services/ingest/extract.py` | iscc-tika extraction + LibreOffice normalisation for legacy formats |
| `services/ingest/chunk.py` | token-bounded chunking (≤2048) with heading-path prefixes |
| `services/ingest/identity.py` | the stable passage identity — the Python mirror of `SearchDocumentID` |
| `services/ingest/arcade.py` | schema-first DDL **only** — the rows are written by CocoIndex's stock `neo4j` target, not by us. `declare_vector_index` is never called: it emits Neo4j's `CREATE VECTOR INDEX` syntax while ArcadeDB wants `LSM_VECTOR METADATA`, so the index is created here first and the connector only inserts. |
| `services/ingest/source.py` | the source binding (bucket or catalog) incl. the per-connection RLS GUC |
| `internal/db/migrations/00NN_*.up.sql` / `.down.sql` | retire the hand-rolled stage ledger |
| `scripts/fetch_llm_model.sh` | fetch + verify Qwen3.5-9B-UD-Q4_K_XL against its published size and SHA-256 |

**Modified**

| path | change |
|---|---|
| `compose.yaml` | delete `aura-docling` + `aura-rerank` + their network/volume/env; add `aura-ingest`; point `aura-llm` at Qwen3.5-9B |
| `cmd/aura/document_pipeline_wiring.go` | stop constructing the Docling client and the in-process worker |
| `internal/documents/*` | delete the groups listed in HANDOFF §2 |
| `internal/config/config_document.go` | drop `AURA_DOCLING_*`; keep chunk knobs, cap at the served model's context |
| `scripts/document_pipeline_e2e*.{sh,go}` | re-point the component list; local LLM; no rerank |
| `.env.example`, `README.md`, `scripts/install.sh` | env catalogue follows |

**Deleted**

`internal/documents/{pipeline_worker,pipeline_store*,jobs_store,jobs_worker,delete_durable_*,events_store,orphans,retry_backoff,job_context,pipeline_types,pipeline_artifact_cache,delete,docling_client*,docling_passages}.go` + their tests · `internal/arcadedb/document_projection.go` + tests · `scripts/seed_docling_tokenizer.sh`.

---

## Task 0: Prove the ground before building on it

The whole plan rests on three claims measured in a spike but never on this machine at HEAD. Prove them first; a failure here changes the plan, not the implementation.

**Files:** none (probes only, under the scratchpad)

**Interfaces:**
- Consumes: the running stack (`aura-arcadedb`, `aura-llama-embed`, `aura-garage`, `aura-postgres` at migration 93)
- Produces: a go/no-go on Bolt writes, `auto_refresh` reconciliation, and iscc-tika coverage

- [x] **Step 1: Confirm ArcadeDB accepts a typed vector over HTTP — DONE 2026-08-06, PASS**

`docker run --rm -i` — **without `-i` the heredoc is silently discarded and the probe prints nothing**, which reads exactly like a hang. POST `/api/v1/command/<db>` with `{"language":"sql","command":…,"params":{…}}` and Basic auth.

The DDL must carry METADATA or the engine rejects it: `LSM_VECTOR index requires METADATA with dimensions, similarity, maxConnections, and beamWidth`. Both forms work — the JSON-quoted one `internal/arcadedb/document_schema.go:314` already builds (three keys, defaults for the rest) and the documented five-key form. Copy the Go one; it is the in-house pattern.

The ANN call is **not** `function('vector.neighbors', …)` — that fails with `Unknown function name 'function'`. The proven shape is `internal/arcadedb/document_retrieval.go:134`: a backticked name and type+property as ONE string.

```sql
SELECT expand(`vector.neighbors`('Passage[embedding]', :q, 3))
SELECT expand(`vector.neighbors`('Passage[embedding]', :q, 3,
  { filter: (SELECT @rid FROM Passage WHERE document = :doc).@rid }))
```

Both verified. Note the trailing `.@rid` on the filter subquery — without it the engine throws "must contain RIDs, got: ResultInternal", which is what makes the documentation's example look broken.

- [ ] **Step 2: Confirm `coco.auto_refresh` exists and is source-agnostic**

```bash
MSYS_NO_PATHCONV=1 docker run --rm aura-pipeline:probe python -c "
import cocoindex as coco, inspect
assert 'auto_refresh' in coco.__all__, coco.__all__
print(inspect.signature(coco.auto_refresh))
print([s for s in coco.__all__ if any(k in s.lower() for k in ('query','search','rank','retriev'))])
"
```

Expected: a signature, and an **empty** list — proving retrieval is not CocoIndex's job and must not be built there.

- [ ] **Step 3: Confirm iscc-tika covers the format matrix, in its own image**

Build `docker/aura-ingest/Dockerfile` (Task 1) is not ready yet, so use the existing `aura-tika:solo`. Extract each fixture and assert a known phrase survives verbatim.

```bash
MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W)/scripts/fixtures:/fx:ro" aura-tika:solo python - <<'PY'
from iscc_tika import extract_text
import pathlib, sys
fails = []
for p in sorted(pathlib.Path("/fx").rglob("*")):
    if p.suffix.lower() not in {".pdf",".docx",".xlsx",".pptx",".doc",".xls",".ppt",".odt",".ods",".odp",".rtf",".csv",".html"}:
        continue
    try:
        t = extract_text(str(p))
        print(f"ok   {p.name:38s} {len(t):7d} chars")
    except Exception as e:
        fails.append((p.name, type(e).__name__, str(e)[:90]))
for f in fails: print("FAIL", *f)
sys.exit(1 if fails else 0)
PY
```

Expected: exit 0. **Never import `extractous` and `iscc_tika` in the same process** — two GraalVM native images give `NoSuchMethodError … TesseractOCRConfig.setSkipOcr`.

- [ ] **Step 4: Record the outcome**

This historical recording step is superseded: keep the measured spike results in
`spikes/cocoindex-ingestion/FINDINGS.md` and record only current residual gaps in the canonical
`.planning/codebase/CONCERNS.md`. If any step failed, STOP and report — do not proceed.

- [ ] **Step 5: Commit**

```bash
git add spikes/cocoindex-ingestion/FINDINGS.md .planning/codebase/CONCERNS.md
git commit -m "Verify the three load-bearing spike claims at HEAD before building on them"
```

---

## Task 1: The ingest sidecar image

**Files:**
- Create: `docker/aura-ingest/Dockerfile`
- Create: `docker/aura-ingest/requirements.txt`
- Test: `scripts/ingest_image_contract_test.sh`

**Interfaces:**
- Produces: image `aura-ingest:local` with `python3.12`, `cocoindex==1.0.19`, `iscc-tika==0.6.0`, `neo4j`, `asyncpg==0.31.0`, and `soffice` on PATH.

- [ ] **Step 1: Write the failing contract test**

```bash
#!/usr/bin/env bash
# scripts/ingest_image_contract_test.sh
set -euo pipefail
img="${AURA_INGEST_IMAGE:-aura-ingest:local}"
docker run --rm "$img" python -c "
import cocoindex, iscc_tika, neo4j, asyncpg
assert cocoindex.__version__.startswith('1.0'), cocoindex.__version__
assert 'auto_refresh' in cocoindex.__all__
print('py ok', cocoindex.__version__)
"
docker run --rm --entrypoint sh "$img" -lc 'command -v soffice >/dev/null && echo soffice ok'
echo "ok: ingest image contract"
```

- [ ] **Step 2: Run it to verify it fails**

Run: `bash scripts/ingest_image_contract_test.sh`
Expected: FAIL — `Unable to find image 'aura-ingest:local'`.

- [ ] **Step 3: Write the Dockerfile**

```dockerfile
# docker/aura-ingest/Dockerfile
FROM python:3.12-slim-bookworm

# LibreOffice normalises the legacy Office formats iscc-tika reads but filecard cannot
# route (.doc/.xls/.ppt). SAL_USE_VCLPLUGIN=svp keeps it headless with no X server.
RUN apt-get update && apt-get install -y --no-install-recommends \
      libreoffice-writer libreoffice-calc libreoffice-impress \
    && rm -rf /var/lib/apt/lists/*
ENV SAL_USE_VCLPLUGIN=svp

COPY docker/aura-ingest/requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir -r /tmp/requirements.txt

COPY services/ingest /app/ingest
WORKDIR /app
# The LMDB state MUST live on a volume; an ephemeral path silently reprocesses everything.
ENV COCOINDEX_DB=/state/coco.db
ENTRYPOINT ["python", "-m", "ingest.app"]
```

```
cocoindex==1.0.19
iscc-tika==0.6.0
neo4j==5.28.1
asyncpg==0.31.0
```

- [ ] **Step 4: Build and run the test**

Run: `docker build -t aura-ingest:local -f docker/aura-ingest/Dockerfile . && bash scripts/ingest_image_contract_test.sh`
Expected: PASS, printing `py ok 1.0.19`, `soffice ok`, `ok: ingest image contract`.

- [ ] **Step 5: Commit**

```bash
git add docker/aura-ingest scripts/ingest_image_contract_test.sh
git commit -m "Add the ingest sidecar image, with LibreOffice for the formats filecard cannot route"
```

---

## Task 2: Stable passage identity (F0 — the blocker)

Today `Passage.document` carries the ingest walker's path, which differs by source (`/corpus/x.pdf` under localfs, `mutuo.pdf` under S3 — no bucket, no prefix). `document_search` returns it and `document_open` cannot resolve it, so find→open is broken and everything above it is invalidated.

**Files:**
- Create: `services/ingest/identity.py`
- Test: `services/ingest/tests/test_identity.py`
- Reference (do not modify): `internal/documents/ids.go:85`

**Interfaces:**
- Produces: `search_document_id(identity_id: str, source_kind: str, source_key: str) -> str` returning `doc_<32 hex>`, byte-compatible with Go's `SearchDocumentID`.

- [ ] **Step 1: Read the Go original and copy its exact framing**

Run: `sed -n '70,100p' internal/documents/ids.go`
The digest is SHA-256 over the null-separated sequence `["aura.document.search.v1", identityID, sourceKind, sourceKey]`, hex-encoded, truncated to 32 characters, prefixed `doc_`. **Confirm each element against the source before writing the Python** — a mismatch here is invisible until `document_open` fails.

- [ ] **Step 2: Write the failing test with a vector taken from Go**

```python
# services/ingest/tests/test_identity.py
from ingest.identity import search_document_id

def test_matches_the_go_implementation():
    # generated with: go run ./scripts/idprobe -identity 0000...0001 -kind s3 -key bucket/a.pdf
    got = search_document_id("00000000-0000-0000-0000-000000000001", "s3", "bucket/a.pdf")
    assert got.startswith("doc_")
    assert len(got) == 36
    assert got == GO_VECTOR  # paste the value the Go probe printed

def test_key_changes_the_identity():
    a = search_document_id("id", "s3", "bucket/a.pdf")
    b = search_document_id("id", "s3", "bucket/b.pdf")
    assert a != b

def test_identity_scopes_the_id():
    a = search_document_id("id-1", "s3", "bucket/a.pdf")
    b = search_document_id("id-2", "s3", "bucket/a.pdf")
    assert a != b
```

- [ ] **Step 3: Generate the Go vector**

Write a throwaway probe under the scratchpad that calls `documents.SearchDocumentID` with those exact arguments and prints the result; paste it into `GO_VECTOR`. Do not guess the value.

- [ ] **Step 4: Run the test to verify it fails**

Run: `docker run --rm -v "$(pwd -W)/services:/app/services:ro" aura-ingest:local python -m pytest services/ingest/tests/test_identity.py -v`
Expected: FAIL — `ModuleNotFoundError: ingest.identity`.

- [ ] **Step 5: Implement**

```python
# services/ingest/identity.py
"""The passage identity Aura can actually open.

The walker's path is not an identity: it changes with the source, and
`document_open` accepts only a catalog uuid or a `doc_<hex>`. This mirrors
`internal/documents/ids.go` byte for byte -- a divergence here is invisible
until an open fails at the far end of the chain.
"""

import hashlib

_DOMAIN = "aura.document.search.v1"


def search_document_id(identity_id: str, source_kind: str, source_key: str) -> str:
    h = hashlib.sha256()
    for part in (_DOMAIN, identity_id, source_kind, source_key):
        h.update(part.encode("utf-8"))
        h.update(b"\x00")
    return "doc_" + h.hexdigest()[:32]
```

- [ ] **Step 6: Run the test to verify it passes**

Run the pytest command from Step 4.
Expected: PASS, 3 tests. If `test_matches_the_go_implementation` fails, the framing differs — re-read the Go and fix the Python, never the vector.

- [ ] **Step 7: Commit**

```bash
git add services/ingest/identity.py services/ingest/tests/test_identity.py
git commit -m "Give the passage an identity document_open can resolve"
```

---

## Task 3: Extraction with iscc-tika, and the legacy-format door

**Files:**
- Create: `services/ingest/extract.py`
- Test: `services/ingest/tests/test_extract.py`
- Fixtures: `scripts/fixtures/document_pipeline_e2e/` (reuse; add `.xls`, `.doc`, `.odt` if absent)

**Interfaces:**
- Consumes: nothing from earlier tasks
- Produces: `extract_text(path: str) -> str` and `needs_normalisation(path: str) -> bool`

- [ ] **Step 1: Write the failing tests**

```python
# services/ingest/tests/test_extract.py
import pathlib
import pytest
from ingest.extract import extract_text, needs_normalisation

FX = pathlib.Path("/fx")

@pytest.mark.parametrize("name", [
    "sample.pdf", "sample.docx", "sample.xlsx", "sample.pptx",
    "sample.doc", "sample.xls", "sample.ppt",
    "sample.odt", "sample.ods", "sample.odp", "sample.rtf",
])
def test_every_office_format_yields_text(name):
    text = extract_text(str(FX / name))
    assert len(text) > 0
    assert "<?xml" not in text[:200] and "{\\rtf" not in text[:200], "returned markup, not text"

def test_exact_phrase_survives_on_a_justified_pdf():
    # MarkItDown emitted "La  sovranita  appartiene  al  popolo" here -- 49.8 double
    # spaces per 1k chars -- which silently breaks the lexical leg of hybrid retrieval.
    text = extract_text(str(FX / "costituzione.pdf"))
    assert "La sovranità appartiene al popolo" in text
    per_1k = text.count("  ") / (len(text) / 1000)
    assert per_1k < 5.0, f"{per_1k:.1f} double spaces per 1k chars"

def test_legacy_formats_are_flagged_for_normalisation():
    assert needs_normalisation("a.xls") and needs_normalisation("a.doc") and needs_normalisation("a.ppt")
    assert not needs_normalisation("a.xlsx") and not needs_normalisation("a.pdf")
```

- [ ] **Step 2: Run to verify it fails**

Run: `docker run --rm -v "$(pwd -W)/services:/app/services:ro" -v "$(pwd -W)/scripts/fixtures/document_pipeline_e2e:/fx:ro" aura-ingest:local python -m pytest services/ingest/tests/test_extract.py -v`
Expected: FAIL — `ModuleNotFoundError: ingest.extract`.

- [ ] **Step 3: Implement**

```python
# services/ingest/extract.py
"""Text extraction, and the door for the formats Aura could not open at all.

iscc-tika (Apache Tika via a GraalVM native image) is the extractor: measured on a
real corpus it handled 15 formats of 16 with zero failures, 18x faster than Docling
on the same PDF. It is the maintained fork of extractous, which has not been pushed
since 2024-12-21. NEVER import both in one process -- two native images collide with
NoSuchMethodError ... TesseractOCRConfig.setSkipOcr.

LibreOffice is here for a narrower reason: `filecard/build.go` routes only .xlsx and
.xlsm, so an ordinary 2010 .xls has no way into Aura's ETL path. `soffice --convert-to`
preserves what filecard needs -- banner rows, the real header below them, and numeric
cells that stay numeric.
"""

import pathlib
import subprocess
import tempfile

from iscc_tika import extract_text as _tika_extract

_LEGACY = {".doc": "docx", ".xls": "xlsx", ".ppt": "pptx"}


def needs_normalisation(path: str) -> bool:
    return pathlib.Path(path).suffix.lower() in _LEGACY


def normalise(path: str, outdir: str) -> str:
    """Convert a legacy Office file to its OOXML equivalent. Returns the new path."""
    target = _LEGACY[pathlib.Path(path).suffix.lower()]
    subprocess.run(
        ["soffice", "--headless", "--convert-to", target, "--outdir", outdir, path],
        check=True, capture_output=True, timeout=300,
    )
    out = pathlib.Path(outdir) / (pathlib.Path(path).stem + "." + target)
    if not out.exists():
        raise RuntimeError(f"soffice produced no {target} for {path}")
    return str(out)


def extract_text(path: str) -> str:
    if needs_normalisation(path):
        with tempfile.TemporaryDirectory() as tmp:
            return _tika_extract(normalise(path, tmp))
    return _tika_extract(path)
```

- [ ] **Step 4: Run to verify it passes**

Run the pytest command from Step 2.
Expected: PASS, 13 tests. If a legacy format fails with "source file could not be loaded", check the path is not under a tmpfs — in the `aura` container `/tmp` is tmpfs and `docker cp` into it silently does nothing.

- [ ] **Step 5: Commit**

```bash
git add services/ingest/extract.py services/ingest/tests/test_extract.py
git commit -m "Extract with Tika, and open the door legacy Office files never had"
```

---

## Task 4: Token-bounded chunking

**Files:**
- Create: `services/ingest/chunk.py`
- Test: `services/ingest/tests/test_chunk.py`

**Interfaces:**
- Consumes: nothing
- Produces: `chunk(text: str, max_tokens: int = 2048) -> list[Chunk]` where `Chunk` has `.text`, `.start`, `.end`, `.heading_path`

- [ ] **Step 1: Write the failing test**

```python
# services/ingest/tests/test_chunk.py
from ingest.chunk import chunk, count_tokens

def test_no_chunk_exceeds_the_model_ceiling():
    text = "parola " * 20000
    for c in chunk(text, max_tokens=2048):
        assert count_tokens(c.text) <= 2048

def test_offsets_reconstruct_the_source():
    text = "alpha beta gamma delta " * 500
    for c in chunk(text, max_tokens=64):
        assert text[c.start:c.end] == c.text

def test_a_single_oversized_paragraph_is_split_not_dropped():
    text = "x" * 200000
    out = chunk(text, max_tokens=2048)
    assert len(out) > 1
    assert sum(len(c.text) for c in out) >= len(text) * 0.99
```

- [ ] **Step 2: Run to verify it fails**

Run: `docker run --rm -v "$(pwd -W)/services:/app/services:ro" aura-ingest:local python -m pytest services/ingest/tests/test_chunk.py -v`
Expected: FAIL — `ModuleNotFoundError: ingest.chunk`.

- [ ] **Step 3: Implement using CocoIndex's own splitter**

Use `cocoindex.ops.text.RecursiveSplitter` rather than writing a splitter — inventory before invention. Wrap it so the ceiling is enforced in tokens, and count tokens with the tokenizer the embedder actually uses (`google/embeddinggemma-300m`), not by whitespace. Keep the module under 100 lines.

- [ ] **Step 4: Run to verify it passes**

Run the pytest command from Step 2.
Expected: PASS, 3 tests.

- [ ] **Step 5: Commit**

```bash
git add services/ingest/chunk.py services/ingest/tests/test_chunk.py
git commit -m "Bound chunks by the model's real 2048-token ceiling, not by bytes"
```

---

## Task 5: Schema-first ArcadeDB (F1)

A Cypher `MERGE` creates an untyped list and `LSM_VECTOR` then refuses to index it. The DDL must run before the first write, every run, idempotently.

**Files:**
- Create: `services/ingest/arcade.py`
- Test: `services/ingest/tests/test_arcade_integration.py` (marked `integration`)

**Interfaces:**
- Consumes: `identity.search_document_id`
- Produces: `ensure_schema(session)`, `write_passages(session, doc_id, passages)`, `delete_document(session, doc_id)`

- [ ] **Step 1: Write the failing integration test**

```python
def test_written_vector_is_retrievable_by_ann(bolt_session):
    ensure_schema(bolt_session)
    write_passages(bolt_session, "doc_" + "a"*32, [P(text="alpha", embedding=[0.1]*768, ordinal=0)])
    rows = bolt_session.run(
        "SELECT FROM function('vector.neighbors', 'Passage', 'embedding', $q, 5)", q=[0.1]*768
    ).data()
    assert rows, "the vector index returned nothing -- schema ran after the write?"

def test_delete_removes_every_passage_of_the_document(bolt_session):
    ensure_schema(bolt_session)
    doc = "doc_" + "b"*32
    write_passages(bolt_session, doc, [P(text=str(i), embedding=[0.1]*768, ordinal=i) for i in range(5)])
    delete_document(bolt_session, doc)
    n = bolt_session.run("SELECT count(*) AS n FROM Passage WHERE document = $d", d=doc).single()["n"]
    assert n == 0
```

- [ ] **Step 2: Run to verify it fails**

Run against a disposable database, never `aura_memory` or a `mem_<uuid>`.
Expected: FAIL — module missing.

- [ ] **Step 3: Implement, DDL first**

`ensure_schema` issues, in this order and each `IF NOT EXISTS`: `CREATE VERTEX TYPE Passage`, `CREATE PROPERTY Passage.embedding ARRAY_OF_FLOATS`, `CREATE PROPERTY Passage.text STRING`, `CREATE PROPERTY Passage.document STRING`, `CREATE INDEX ON Passage (embedding) LSM_VECTOR`, `CREATE INDEX ON Passage (text) FULL_TEXT`, `CREATE INDEX ON Passage (document) NOTUNIQUE`.

- [ ] **Step 4: Run to verify it passes**

Expected: PASS, 2 tests.

- [ ] **Step 5: Commit**

```bash
git add services/ingest/arcade.py services/ingest/tests/test_arcade_integration.py
git commit -m "Type the vector property before writing it, because LSM_VECTOR will not index a bare list"
```

---

## Task 6: The CocoIndex app and live reconciliation (F2)

**Files:**
- Create: `services/ingest/app.py`, `services/ingest/source.py`
- Test: `scripts/ingest_reconcile_e2e.sh`

**Interfaces:**
- Consumes: every module above
- Produces: an executable `python -m ingest.app` honouring `AURA_INGEST_LIVE` and `AURA_INGEST_INTERVAL_SEC`

- [ ] **Step 1: Write the failing reconciliation test**

A shell script that, against a disposable bucket and database: (1) puts three objects and runs one pass, asserting three documents; (2) runs again unchanged, asserting **zero** re-extractions (memoization); (3) modifies one object, asserting exactly one re-extraction; (4) deletes one, asserting its passages are gone without any delete worker running.

- [ ] **Step 2: Run to verify it fails**

Expected: FAIL at step 1 — no app.

- [ ] **Step 3: Implement**

Key points, each of which cost a day in the spike:

```python
# services/ingest/app.py  (skeleton -- keep under 200 LOC)
import cocoindex as coco

@coco.fn(memo=True)                       # memo -- or every run re-extracts everything
async def process_file(file, target): ...

@coco.fn
async def app_main(source_uri: str) -> None:
    items = build_source(source_uri)      # see source.py
    await coco.mount_each(process_file, items, target)

app = coco.App(coco.AppConfig(name="AuraIngest"), app_main, source_uri=...)
```

**Live is NOT `-L`.** On a non-live source `cocoindex update -L` prints `⏳ Ready | Watching for changes...` and exits 0 — every symptom of success while watching nothing. Wrap the sync function: `coco.auto_refresh(fn, interval=…)`, public API, no LiveComponent to write.

**The LMDB state goes on a volume** (`COCOINDEX_DB=/state/coco.db`). An ephemeral path reprocesses the corpus every run and still looks correct.

- [ ] **Step 4: Run to verify it passes**

Expected: all four assertions pass, and step 2 shows zero re-extraction.

- [ ] **Step 5: Commit**

```bash
git add services/ingest/app.py services/ingest/source.py scripts/ingest_reconcile_e2e.sh
git commit -m "Reconcile the corpus instead of orchestrating it"
```

---

## Task 7: Wire the sidecar into compose, and serve Qwen3.5-9B locally

> **SUPERSEDED 2026-08-07, mid-execution:** this task originally pinned `Qwen/Qwen3-8B-GGUF`'s
> `Qwen3-8B-Q4_K_M.gguf` (5,027,783,488 bytes, commit `7c41481f57cb95916b40956ab2f0b139b296d974`,
> SHA-256 `d98cdcbd03e17ce47681435b5150e34c1417f50b5c0019dd560e4882c5745785`, declared context
> 40960 — visible below only as the ORIGINAL instruction). The human changed the served model
> before that artifact finished downloading. The DEPLOYED artifact is
> `unsloth/Qwen3.5-9B-GGUF`'s `Qwen3.5-9B-UD-Q4_K_XL.gguf`, 5,966,095,584 bytes, commit
> `3885219b6810b007914f3a7950a8d1b469d598a5`, SHA-256
> `6f5d30666c2d8ae16a306e616d95341dcf3cc46810df84d7e6f5a7d1e4c1b293`, native context 262144,
> served at `-c 32768` (load-tested, not the native ceiling). The 40960 figure below is history,
> not a live ceiling — it does not apply to the deployed model.

**Files:**
- Modify: `compose.yaml`, `.env.example`, `scripts/install.sh`
- Create: `scripts/fetch_llm_model.sh`

- [ ] **Step 1: Write the failing model-fetch test**

ORIGINAL instruction (superseded, see banner above): mirror `scripts/fetch_embedding_model_test.sh`: assert the fetched file starts with `GGUF`, matches the published size **5027783488**, and its SHA-256 equals `d98cdcbd03e17ce47681435b5150e34c1417f50b5c0019dd560e4882c5745785`. AS BUILT: the deployed pins are 5,966,095,584 bytes / SHA-256 `6f5d30666c2d8ae16a306e616d95341dcf3cc46810df84d7e6f5a7d1e4c1b293` (see banner).

- [ ] **Step 2: Run to verify it fails**

Run in WSL: `wsl -e bash -lc 'cd /mnt/d/Repo/Aura && bash scripts/fetch_llm_model_test.sh'`
Expected: FAIL — script missing.

- [ ] **Step 3: Implement the fetch and add the compose service**

`aura-ingest` joins the default network, mounts the state volume, and depends on `arcadedb` + `aura-llama-embed` being healthy. `aura-llm` serves `Qwen3.5-9B-UD-Q4_K_XL.gguf` (AS BUILT; ORIGINAL instruction named `Qwen3-8B-Q4_K_M.gguf`, see banner). **Do not set `--pooling`** anywhere. Set `-c` no higher than the model's declared context — AS BUILT this is 262144 native, served at the load-tested 32768 (ORIGINAL instruction said "40960 for Qwen3-8B", which does not apply here); the embedder stays 2048.

- [ ] **Step 4: Run to verify it passes, and bring the stack up**

Run: `docker compose up -d aura-ingest aura-llm && docker compose ps`
Expected: both healthy. Verify the LLM answers: `curl -s localhost:8084/v1/chat/completions -d '{"model":"q","messages":[{"role":"user","content":"ciao"}]}'`.

- [ ] **Step 5: Commit**

```bash
git add compose.yaml .env.example scripts/install.sh scripts/fetch_llm_model.sh scripts/fetch_llm_model_test.sh
git commit -m "Serve the answering model locally and mount the ingest sidecar"
```

---

## Task 8: Reduce `document_open` and retire the in-process pipeline (F4)

**Files:**
- Modify: `cmd/aura/document_pipeline_wiring.go`, `internal/documents/open.go`
- Test: existing `internal/documents/open_test.go` must stay green

- [ ] **Step 1: Assert the current contract still holds**

Run: `go test ./internal/documents/ -run TestOpen -v`
Expected: PASS. This is the behaviour that must survive the change.

- [ ] **Step 2: Delete the Docling construction**

`buildRuntimeDocumentPipeline` currently fails without a Docling client and has no alternative branch. Remove the function and its call site rather than leaving a disabled path — dark code is forbidden.

- [ ] **Step 3: Run build and vet**

Run: `go build ./... && go vet ./...`
Expected: clean. Every compile error names a caller that must also go — follow them.

- [ ] **Step 4: Run the full unit suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git commit -am "Stop constructing an in-process pipeline the sidecar now owns"
```

---

## Task 9: Delete the replaced machinery (F5)

Only now. Deleting earlier leaves the tree unbuildable for several tasks.

**Files:** the deletion list in File Structure, plus `scripts/seed_docling_tokenizer.sh`, the `aura-docling` and `aura-rerank` compose services with their network/volume/env/depends_on, `AURA_DOCLING_*` and `AURA_RERANK_*` in every catalogue, and the CI steps that reference them.

- [ ] **Step 1: Delete, in one commit per group**

Groups: (a) docling client + tests, (b) pipeline store/worker + tests, (c) jobs + tests, (d) durable delete + tests, (e) `document_projection.go` + tests, (f) compose/env/CI/scripts.

- [ ] **Step 2: After each group, build and test**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: clean. Never leave a group half-deleted across a commit.

- [ ] **Step 3: Add the migration retiring the stage ledger**

Get the number from the directory, never from this document:

```bash
ls internal/db/migrations/ | tail -1
```

The `.down.sql` must recreate what the `.up.sql` drops — the tree is forward-only in practice but the pair must be honest.

- [ ] **Step 4: Prove nothing survives**

```bash
grep -ri "docling" --include="*.go" --include="*.yaml" --include="*.sh" --include="*.example" . | grep -v "^./.git" | grep -v "^./docs" | grep -v prd.md
grep -ri "AURA_RERANK" --include="*.go" --include="*.yaml" --include="*.sh" --include="*.example" . | grep -v "^./.git" | grep -v "^./docs"
```

Expected: no output from either.

- [ ] **Step 5: Verify coverage did not fall through the floor**

Run: `bash scripts/coverage_docker.sh`
Expected: ≥85%. Removing ~10k lines removes their tests too; if a surviving package drops below the floor, add daemon-free unit tests for its pure logic before proceeding.

- [ ] **Step 6: Commit each group as it lands**

---

## Task 10: The production gate (amendment #115, re-pointed)

**This is the only step that closes the work.** A green unit suite does not.

**Files:**
- Modify: `scripts/document_pipeline_e2e.sh`, `scripts/document_pipeline_e2e_probe.go`, `scripts/document_pipeline_e2e_support.go`

- [ ] **Step 1: Re-point the component list**

The gate must fail closed unless the configured **local** LLM (`aura-llm`, Qwen3.5-9B-UD-Q4_K_XL — AS BUILT; superseded the originally-pinned Qwen3-8B-Q4_K_M mid-Task-7, see the Task 7 banner above), the **ingest sidecar**, the embedding server, Garage, PostgreSQL and the per-identity ArcadeDB projection all execute without mock, skip, fallback or degraded status. Docling and the reranker are removed from that list. **No OpenRouter.**

- [ ] **Step 2: Add the format-coverage hard check**

The canary corpus gains one file per format — `.pdf .docx .xlsx .pptx .doc .xls .ppt .odt .ods .odp .rtf` — and the gate asserts each becomes searchable, plus that a known phrase from the justified PDF is found by **exact-phrase** search.

- [ ] **Step 3: Run the gate**

```bash
wsl -e bash -lc 'cd /mnt/d/Repo/Aura && export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH" && bash scripts/document_pipeline_e2e.sh'
```

Expected: score above 98%, every hard check passing: canonical ingest through ready activation, exact ground-truth answer and citation locator/version/SHA **through a natural agent prompt**, cross-tenant non-disclosure, retry/restart recovery, delete plus Garage absence, post-delete non-recall, and cleanup of both disposable identities.

- [ ] **Step 4: If it scores below 98%, fix the product, not the gate**

Record the failing check verbatim. Never relax an assertion to reach the number.

- [ ] **Step 5: Update the quality snapshot**

For every row whose CI-gate-path glob matches a file changed here, bump `Last measured` and prepend a re-attestation note. Verify locally first — it must print `ok: … checked N row(s)`:

```bash
AURA_QUALITY_CHANGED_FILES="$(git diff --name-only origin/master..HEAD)" \
AURA_QUALITY_BASE_DATE="$(git log -1 --format=%cs origin/master)" \
bash scripts/quality_snapshot_gate.sh
```

- [ ] **Step 6: Run the full local matrix before pushing**

Run in WSL: `make quality-full`
Expected: green. A green local full-matrix run is worth more than a push-and-wait CI cycle.

- [ ] **Step 7: Push and confirm CI is green**

```bash
git push
gh run watch
```

Expected: every job green. The work is not done while any job is red or while the gate has not run against the production surface.

---

## Self-Review

**Spec coverage.** HANDOFF F0→F5 map to Tasks 2, 5, 6, 3, 8, 9; the production gate to Task 10; the sidecar and local model to Tasks 1 and 7. **F6 (`table_query`), F7 (prefix routing end-to-end) and F8 (the file-manager UI) are deliberately NOT in this plan** — each is an independent capability that needs its own spec and would make this plan unshippable. They remain open in HANDOFF §8.

**The source decision, made 2026-08-06: the Garage bucket, one CocoIndex app per identity.**

Both candidates are Silo and both are server-enforced, so the tie is broken on how each one *fails*. The industry criterion is that isolation must be enforced by infrastructure rather than application code, and the two differ exactly there: the Postgres route's isolation depends on `app.current_identity` being set on every connection, which is a thing a future change can forget on a reused pool; the bucket route's isolation *is* the credential, and a wrong key cannot open a bucket at all. Fail-closed RLS softens the first failure to "zero rows", but a shared pool carrying the wrong GUC is not softened by anything.

Three practical reasons make the safer option also the simpler one: (1) Postgres holds metadata, not content — a Postgres source would still fetch bytes from Garage, so it is two systems instead of one; (2) `internal/assets/object_resolver.go` already implements bucket-per-identity with the owner's own credentials, so this follows an existing in-house pattern rather than inventing one; (3) the spike already proved add/modify/delete reconciliation live against Garage with `coco.auto_refresh`.

Shape: `coco.App(coco.AppConfig(name=f"aura-ingest/{identity_id}"), app_main, …)` — the app name scopes the incremental state, so one tenant's reconciliation cannot touch another's. `services/ingest/source.py` therefore binds `amazon_s3` with that identity's Garage key, and `search_document_id(identity, "s3", key)` derives the passage identity without any join.

**Accepted consequence, recorded because it is a behaviour change and not a cleanup:** the bucket becomes the truth about what exists and the catalog becomes its projection. Catalog rows with no asset — the ones `aura docs ingest` and `document_index` created, which amendment #114's own audit found unable to satisfy their own `document_open` contract — stop existing for ingestion.

**Postgres-as-source is not deleted from the design, it is deferred.** If a future need makes the catalog the right source, the mechanism is known: `PgTableSource` takes an `asyncpg.Pool` we construct, and `asyncpg.create_pool` exposes `setup=`/`init=` per-connection hooks where the GUC is set.

**Placeholder scan.** Tasks 4, 5 and 6 give implementation direction plus full tests rather than full implementation bodies, because each wraps a library API whose exact call shape must be read from the installed package at implementation time; the constraints that make them non-obvious (memo, auto_refresh, volume-backed LMDB, DDL ordering) are stated explicitly. Every other task carries literal code.

**Type consistency.** `search_document_id` is used with that exact name in Tasks 2 and 5. `extract_text`/`needs_normalisation`/`normalise` are consistent between Task 3's tests and implementation. `ensure_schema`/`write_passages`/`delete_document` are consistent between Task 5's test and Step 3.
