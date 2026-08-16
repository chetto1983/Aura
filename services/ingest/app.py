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
import re
import subprocess
import sys
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
class IndexedDocument:
    """One record per object indexed, in the SAME database as its passages.

    Mirrors arcade.py's IndexedDocument DDL. It carries what reconciling a bucket actually
    knows -- no title, no status, no version, because the object has none and inventing
    them is what made the old catalog unable to answer its own document_open contract.

    It lived in PostgreSQL for one commit. That was wrong: the card leg is a full-text
    ranking and ArcadeDB already indexes Passage.text FULL_TEXT with the same analyzer, so
    a Postgres card meant two search engines tokenising the same query differently. No
    identity_id either -- the database IS the identity (mem_<uuid>), so a column repeating
    it would be a second place for tenancy to be wrong.
    """

    search_document_id: str
    source_kind: str
    source_key: str
    # The base name -- how a person refers to a file, and the title retrieval falls back to.
    file_name: str
    # The same name PLUS its non-alphanumeric runs turned into spaces. Mirrors
    # aura.searchable_text (migration 0081) because the analyzer has the same blind spot
    # here as there: "clienti_complesso.xlsx" is a single token, so without this a search
    # for "clienti" cannot find it.
    file_name_words: str
    raw_sha256: str
    size_bytes: int
    passage_count: int
    # What filecard measured about the file. Empty when it could not be described --
    # never an error, because a card is how a document is found, not whether it exists.
    card: str
    indexed_at: datetime.datetime


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


def _name_words(file_name: str) -> str:
    """The name as written plus its non-alphanumeric runs split, for the full-text index."""
    return file_name + " " + re.sub(r"[^0-9A-Za-z]+", " ", file_name)


def _card_name(file_name: str, converted_path: str) -> str:
    """The real stem under the extension filecard will actually parse.

    The name filecard is given does TWO jobs and both have to be served here.

    It ROUTES: Request.ext() prefers FileName over Path, so carding a converted .ods under
    the name "x.ods" falls through to "file, 12 KB" -- the conversion happens and is then
    thrown away. MEASURED: 12/12 fixture formats card structurally when the CONVERTED
    extension is used, against 4/12 before.

    And it is DISPLAYED: the card opens with this name, that first line is indexed, and the
    agent reads it back. Passing the converted temp file's whole basename served the routing
    and lost the name -- measured 2026-08-16, a chat attachment carded as "tmptq9teunw.pdf"
    while the file_name stored beside it was already correct.

    LibreOffice keeps the stem, so this only ever changes the extension, which is an honest
    statement of what was parsed. source_key on the row still carries the true original key.
    """
    return pathlib.PurePosixPath(file_name).stem + pathlib.PurePosixPath(converted_path).suffix


@coco.fn(memo=True)
def _card(path: str, file_name: str) -> str:
    """Describe the file, by calling Aura's own filecard rather than reimplementing it.

    A card is how a document is FOUND, and for a spreadsheet it is how one is ANSWERED:
    measured on a 500-row, 3-sheet workbook it locates the real header under a merged
    banner, types every column and reports value distributions -- "Citta: most common
    Torino (98), Milano (94)". That is precisely the aggregate that scores 0% at every k
    when asked of passages (internal/documents/open.go), so a document reconciled from the
    bucket without a card loses the only thing that could answer it.

    Subprocess for the same reason extract.py runs `soffice`: the logic exists, in Go, and
    a Python port would be a second implementation to keep in step. Failure is not fatal --
    Service.writeCard does not fail an ingest over a card either -- so a file that cannot
    be described simply has none, and the reason is printed.
    """
    try:
        done = subprocess.run(
            ["aura-filecard", "-name", file_name, path],
            check=False, capture_output=True, timeout=120,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        print(f"[card] {file_name}: {exc}", flush=True)
        return ""
    if done.stderr:
        print(f"[card] {file_name}: {done.stderr.decode('utf-8', 'replace').strip()}", flush=True)
    return done.stdout.decode("utf-8", "replace")


@coco.fn(memo=True)
def _embed(text: str) -> list[float]:
    # EmbeddingGemma is asymmetric: documents carry "title: none | text: …" (queries
    # carry a different prefix) -- omitting it measured recall@1 0.25 -> 0.05. The
    # prefix is a chunk.py constant because the chunk budget has to subtract it; see
    # chunk.document_budget().
    payload = json.dumps(
        {"input": chunk.EMBED_DOC_PREFIX + text, "model": "embeddinggemma"}
    ).encode()
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
    documents: neo4j.TableTarget[IndexedDocument],
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
    # The object's own name when it carries one, the key's tail when it does not.
    #
    # A chat attachment's key is `chat/<assetID>.pdf` on purpose -- a key travels into
    # presigned URLs and access logs, so the filename is deliberately kept out of it -- and
    # with nothing else carrying the name, every attachment reached this index as
    # "019f8a2b-....pdf". That name is not just displayed: it goes into file_name_words
    # below, so searching for a document by the name the operator gave it found nothing.
    #
    # The fallback is not a stopgap: a file the operator dropped into the bucket directly
    # has no metadata and its key IS its name.
    file_name = (
        await source.object_file_name(coco.use_context(S3), _S3_CONFIG, key)
        or pathlib.PurePosixPath(key).name
    )
    with tempfile.NamedTemporaryFile(suffix=pathlib.Path(key).suffix) as tmp:
        tmp.write(content)
        tmp.flush()
        # ONE conversion, both consumers. extract.prepared yields the OOXML form for the
        # formats LibreOffice has to normalise, so the extractor and the card read the same
        # converted file -- and, crucially, the card is built from the CONVERTED file: a
        # .xls or .ods carded raw yields "file, 6 KB" and nothing else, because filecard
        # routes .xlsx/.docx/.pptx only.
        with extract.prepared(tmp.name) as ready:
            # Routed by family, not attempted-and-caught. Tika raises on a format it
            # cannot parse, and the raise killed the whole component -- one photo in the
            # bucket and that file never got indexed. The library's own image and audio
            # examples do the same thing one level up, choosing the processor from the
            # path; this is that choice made where the extension is already known.
            #
            # A non-textual file still gets its row: name-searchable, card-described,
            # openable. Giving images their own embedding and audio its own transcript --
            # the two examples above -- is the natural next step, and this leaves the
            # seam for it rather than deciding it here.
            text = extract.extract_text(ready) if extract.extractable(ready) else ""
            card = _card(ready, _card_name(file_name, ready))
    source_kind = "s3"
    search_document_id = identity.search_document_id(identity_id, source_kind, key)
    # document_budget(), not the bare ceiling: _embed sends EMBED_DOC_PREFIX + text, so a
    # chunk sized to the full ceiling overflows by the prefix and the request 500s.
    pieces = chunk.chunk(text, max_tokens=chunk.document_budget())
    raw_sha256 = hashlib.sha256(content).hexdigest()
    await coco.map(
        process_chunk, list(enumerate(pieces)),
        search_document_id, search_document_id, source_kind, key, raw_sha256, table,
    )
    # One row per OBJECT, declared beside the passages so both targets are reconciled from
    # the same pass over the same source. That is the whole reason this row is not a
    # catalog: when the object leaves the bucket, CocoIndex removes the passages AND this
    # row together, so the two can never disagree about what exists.
    documents.declare_record(row=IndexedDocument(
        search_document_id=search_document_id,
        source_kind=source_kind,
        source_key=key,
        file_name=file_name,
        file_name_words=_name_words(file_name),
        raw_sha256=raw_sha256,
        size_bytes=len(content),
        passage_count=len(pieces),
        card=card,
        indexed_at=datetime.datetime.now(datetime.timezone.utc),
    ))


@coco.fn
async def reconcile(
    identity_id: str, table: neo4j.TableTarget[Passage],
    documents: neo4j.TableTarget[IndexedDocument],
) -> None:
    walker = source.walk(coco.use_context(S3), _S3_CONFIG)
    await coco.mount_each(process_file, walker.items(), identity_id, table, documents)


@coco.fn
async def app_main(identity_id: str, interval_s: float) -> None:
    table = await neo4j.mount_table_target(
        KG_DB, arcade.PASSAGE_TYPE,
        await neo4j.TableSchema.from_class(Passage, primary_key="passage_key"),
        primary_key="passage_key",
    )
    # managed_by=USER: arcade.ensure_schema owns the DDL (see _document_ddl) and this
    # target only upserts and deletes rows. SYSTEM would have CocoIndex create the type
    # itself, outside the one place that knows the schema version the Go reader checks.
    # (This comment used to credit "migration 0094" and a row-level security policy. Both
    # were wrong: no such migration exists, the type lives in ArcadeDB where a Postgres
    # RLS policy has no meaning, and the isolation here is the per-identity database.)
    # The SAME target connector that writes the passages, pointed at a second type. One
    # writer, one store, one query language -- and the record and its passages can never
    # end up in different databases.
    documents = await neo4j.mount_table_target(
        KG_DB, arcade.DOCUMENT_TYPE,
        await neo4j.TableSchema.from_class(IndexedDocument, primary_key="search_document_id"),
        primary_key="search_document_id",
    )
    await coco.mount(
        coco.auto_refresh(reconcile, interval=datetime.timedelta(seconds=interval_s)),
        identity_id, table, documents,
    )


app = coco.App(
    coco.AppConfig(name=f"aura-ingest/{_S3_CONFIG.identity_id}"),
    app_main, _S3_CONFIG.identity_id, _INTERVAL_S,
)

def audit_pass() -> int:
    """Compare the bucket against the index and name every object that produced no row.

    This exists because an ingest that loses documents and exits 0 is not a hypothetical:
    on 2026-08-09 two separate defects did exactly that on the same corpus, and both were
    found by running this comparison BY HAND. CocoIndex catches a component failure, prints
    "component build failed" and carries on, so a lost document looks identical to a
    document with nothing to say -- in catch-up mode as well as live, whatever compose.yaml
    claims. Reconciliation is the only thing that can tell them apart, so it ships.

    Returns the number of missing objects; the caller decides what that is worth.
    """
    expected = source.expected_keys(_S3_CONFIG)
    indexed = arcade.indexed_source_keys(
        ARCADE_HTTP, ARCADE_DB, ("root", ARCADE_PASSWORD), 60.0
    )
    missing = sorted(expected - indexed)
    print(f"[audit] {len(expected)} objects in {_S3_CONFIG.bucket}, "
          f"{len(indexed)} indexed, {len(missing)} missing", flush=True)
    for key in missing:
        print(f"[audit] MISSING {key}", flush=True)
    return len(missing)


if __name__ == "__main__":
    app.update_blocking(live=_LIVE)
    # Live never returns, so there is no pass to audit and no exit code to carry one.
    if not _LIVE:
        sys.exit(1 if audit_pass() else 0)
