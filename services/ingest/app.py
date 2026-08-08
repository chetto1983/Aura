"""The CocoIndex app: the ONLY entry point, `python -m ingest.app`.

CocoIndex reconciles one identity's Garage bucket into ArcadeDB (FINDINGS.md's "S3
incrementale" already proved add/modify/delete with zero code of ours). This file wires
that proven behaviour to OUR extractor, chunker and passage identity.

Live is NOT `-L` alone: amazon_s3 has no live/watch anything, so `update_blocking(live=
True)` alone keeps the app alive but re-scans nothing. `coco.auto_refresh` is what
re-runs the scan on an interval, and it degrades to exactly one pass in catch-up mode --
so `reconcile` is always wrapped the same way and AURA_INGEST_LIVE only picks whether
`update_blocking` returns after that one pass or keeps the interval loop running.
"""

import dataclasses
import datetime
import hashlib
import json
import os
import pathlib
import tempfile
import urllib.request

import cocoindex as coco
from cocoindex.connectors import amazon_s3, neo4j

from ingest import arcade, chunk, extract, identity, source

ARCADE_HTTP = os.environ.get("ARCADE_HTTP", "http://aura-arcadedb:2480")
ARCADE_BOLT = os.environ.get("ARCADE_BOLT", "bolt://aura-arcadedb:7687")
ARCADE_PASSWORD = os.environ["ARCADEDB_PASSWORD"]
EMBED_BASE_URL = os.environ.get("AURA_EMBED_BASE_URL", "http://aura-llama-embed:8081")
EMBED_DIMENSIONS = int(os.environ.get("AURA_EMBED_DIMENSIONS", "768"))

_S3_CONFIG = source.config_from_env()

# DERIVED, never configured. Both of these are contracts with Aura's Go retriever, and
# both were wrong until 2026-08-08 in ways that fail silently: rows landed, and nothing
# could ever read them.
#
#   - the database was a single ARCADE_DB (default "aura_ingest"), while the retriever
#     opens mem_<identity_uuid>. It was writing to a database the reader never opens.
#   - schema_version was the string "1", while the reader rejects any candidate whose
#     stamp differs from its own "document-v1:standard-analyzer:cosine:none:<dims>".
#
# An env var for either would just be a way to reintroduce the same defect, so there
# isn't one.
ARCADE_DB = identity.database_for(_S3_CONFIG.identity_id)
SCHEMA_VERSION = arcade.schema_version(EMBED_DIMENSIONS)
# Free-text provenance marker, not read back by anything.
PIPELINE_GENERATION = "cocoindex-v1"
_LIVE = os.environ.get("AURA_INGEST_LIVE", "").strip().lower() in {"1", "true", "yes", "on"}
_INTERVAL_S = float(os.environ.get("AURA_INGEST_INTERVAL_SEC", "60"))

KG_DB = coco.ContextKey[neo4j.ConnectionFactory]("kg_db")
S3 = coco.ContextKey[object]("s3_client")


@coco.lifespan
async def coco_lifespan(builder: coco.EnvironmentBuilder):
    # Schema BEFORE any write: a Cypher MERGE against an untyped property makes
    # LSM_VECTOR refuse to index it (measured live in Task 5; arcade.py docstring).
    arcade.ensure_schema(ARCADE_HTTP, ARCADE_DB, ("root", ARCADE_PASSWORD), EMBED_DIMENSIONS)
    builder.provide(KG_DB, neo4j.ConnectionFactory(
        uri=ARCADE_BOLT, auth=("root", ARCADE_PASSWORD), database=ARCADE_DB))
    async with source.create_client(_S3_CONFIG) as client:
        builder.provide(S3, client)
        yield


@dataclasses.dataclass(frozen=True, slots=True)
class Passage:
    """Mirrors arcade.py's Passage DDL field-for-field -- that is the schema authority.

    document_id mirrors search_document_id and passage_id mirrors passage_key: this
    pipeline has no separate document-catalog identity to invent one from. The layout
    fields (page/bbox/sheet/table/row/column/cell/captions/self_ref/projection_key) stay
    None: this writer only ever extracts plain text, never structured layout.
    tombstoned_at stays None always -- CocoIndex deletes the row outright on source
    deletion, so nothing here ever soft-tombstones (FINDINGS.md §4).

    source_kind/source_key carry where the file actually lives: source_kind is
    always "s3" (this app has only one source connector) and source_key is the
    exact same raw object key process_file already resolves and feeds into
    search_document_id -- reused, not recomputed, so the two stay byte-identical.
    """

    passage_key: str
    passage_id: str
    projection_key: str | None
    document_id: str
    search_document_id: str
    source_kind: str
    source_key: str
    version_id: str | None
    version_number: int | None
    raw_sha256: str
    pipeline_generation: str
    schema_version: str
    ordinal: int
    text: str
    normalized_text_sha256: str
    self_ref: str | None
    heading_path: list[str]
    captions: list[str] | None
    page_number: int | None
    bbox_left: float | None
    bbox_top: float | None
    bbox_right: float | None
    bbox_bottom: float | None
    char_start: int
    char_end: int
    sheet_name: str | None
    table_name: str | None
    row_number: int | None
    column_number: int | None
    cell_reference: str | None
    embedding: list[float]
    active: bool
    created_at: datetime.datetime
    tombstoned_at: datetime.datetime | None


@coco.fn(memo=True)
def _embed(text: str) -> list[float]:
    # EmbeddingGemma is asymmetric: documents carry "title: none | text: …" (queries
    # carry a different prefix) -- omitting it measured recall@1 0.25 -> 0.05.
    payload = json.dumps({"input": f"title: none | text: {text}", "model": "embeddinggemma"}).encode()
    req = urllib.request.Request(
        f"{EMBED_BASE_URL.rstrip('/')}/v1/embeddings", data=payload,
        headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=120) as resp:
        return json.loads(resp.read())["data"][0]["embedding"]


@coco.fn
async def process_chunk(
    item: tuple[int, chunk.Chunk], document_id: str, search_document_id: str,
    source_kind: str, source_key: str, raw_sha256: str, table: neo4j.TableTarget[Passage],
) -> None:
    ordinal, piece = item
    table.declare_record(row=Passage(
        passage_key=f"{search_document_id}:{ordinal}",
        passage_id=f"{search_document_id}:{ordinal}",
        projection_key=None,
        document_id=document_id,
        search_document_id=search_document_id,
        source_kind=source_kind,
        source_key=source_key,
        version_id=None,
        version_number=None,
        raw_sha256=raw_sha256,
        pipeline_generation=PIPELINE_GENERATION,
        schema_version=SCHEMA_VERSION,
        ordinal=ordinal,
        text=piece.text,
        normalized_text_sha256=hashlib.sha256(piece.text.encode("utf-8")).hexdigest(),
        self_ref=None,
        heading_path=list(piece.heading_path),
        captions=None,
        page_number=None,
        bbox_left=None, bbox_top=None, bbox_right=None, bbox_bottom=None,
        char_start=piece.start,
        char_end=piece.end,
        sheet_name=None, table_name=None, row_number=None, column_number=None, cell_reference=None,
        embedding=_embed(piece.text),
        active=True,
        created_at=datetime.datetime.now(datetime.timezone.utc),
        tombstoned_at=None,
    ))


@coco.fn(memo=True)
async def process_file(
    file: amazon_s3.S3File, identity_id: str, table: neo4j.TableTarget[Passage],
) -> None:
    # The walker's iteration key is the PREFIX-RELATIVE path (F0: the spike used exactly
    # that as the passage identity, breaking find->open). resolve() is the raw S3 object
    # key, stable regardless of any prefix scoping -- that is the source_key identity.py
    # hashes into search_document_id.
    key = file.file_path.resolve()
    # A distinctive, greppable line: only prints when this component actually RUNS its
    # body, so it is the observable proxy for "was this file re-extracted" -- memo=True
    # skips the whole function, print included, on an unchanged rerun.
    print(f"[extract] {key}", flush=True)
    content = await file.read()
    with tempfile.NamedTemporaryFile(suffix=pathlib.Path(key).suffix) as tmp:
        tmp.write(content)
        tmp.flush()
        text = extract.extract_text(tmp.name)
    source_kind = "s3"
    search_document_id = identity.search_document_id(identity_id, source_kind, key)
    pieces = chunk.chunk(text)
    raw_sha256 = hashlib.sha256(content).hexdigest()
    await coco.map(
        process_chunk, list(enumerate(pieces)),
        search_document_id, search_document_id, source_kind, key, raw_sha256, table,
    )


@coco.fn
async def reconcile(identity_id: str, table: neo4j.TableTarget[Passage]) -> None:
    walker = source.walk(coco.use_context(S3), _S3_CONFIG)
    await coco.mount_each(process_file, walker.items(), identity_id, table)


@coco.fn
async def app_main(identity_id: str, interval_s: float) -> None:
    table = await neo4j.mount_table_target(
        KG_DB, arcade.PASSAGE_TYPE,
        await neo4j.TableSchema.from_class(Passage, primary_key="passage_key"),
        primary_key="passage_key",
    )
    await coco.mount(
        coco.auto_refresh(reconcile, interval=datetime.timedelta(seconds=interval_s)),
        identity_id, table,
    )


app = coco.App(
    coco.AppConfig(name=f"aura-ingest/{_S3_CONFIG.identity_id}"),
    app_main, _S3_CONFIG.identity_id, _INTERVAL_S,
)

if __name__ == "__main__":
    app.update_blocking(live=_LIVE)
