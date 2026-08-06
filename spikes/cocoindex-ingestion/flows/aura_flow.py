"""Aura ingestion spike: local PDFs -> MarkItDown -> chunk -> EmbeddingGemma -> ArcadeDB.

The ArcadeDB target is CocoIndex's STOCK Neo4j connector pointed at ArcadeDB's Bolt
port. No fork: ArcadeDB is wire- and Cypher-compatible ("connect by changing the
connection URL alone"), the one exception being vector-index DDL, which is native
(JVector/LSM_VECTOR) and therefore declared in ArcadeDB SQL by ensure_schema() below
BEFORE any Bolt write. Declaring the property type up front is not optional: a plain
MERGE creates an untyped list and LSM_VECTOR then refuses to index it.
"""

from __future__ import annotations

import base64
import dataclasses
import io
import json
import os
import pathlib
import urllib.request
from collections.abc import AsyncIterator

import cocoindex as coco
from cocoindex.connectors import localfs, neo4j
from cocoindex.resources.file import PatternFilePathMatcher

ARCADE_HTTP = os.environ.get("ARCADE_HTTP", "http://aura-arcadedb:2480")
ARCADE_BOLT = os.environ.get("ARCADE_BOLT", "bolt://aura-arcadedb:7687")
ARCADE_DB = os.environ.get("ARCADE_DB", "aura_ingest_spike")
ARCADE_PW = os.environ["ARCADEDB_PASSWORD"]
EMBED_URL = os.environ.get("EMBED_URL", "http://aura-llama-embed:8081/v1/embeddings")
OCR_URL = os.environ.get("OCR_BASE_URL", "http://aura-ocr-vl:8082/v1")
DIMS = int(os.environ.get("EMBED_DIMS", "768"))
CHUNK_CHARS = int(os.environ.get("CHUNK_CHARS", "1200"))
CHUNK_OVERLAP = int(os.environ.get("CHUNK_OVERLAP", "150"))

_AUTH = "Basic " + base64.b64encode(f"root:{ARCADE_PW}".encode()).decode()


def _arcade(path: str, payload: dict) -> tuple[bool, str]:
    req = urllib.request.Request(
        f"{ARCADE_HTTP}{path}",
        data=json.dumps(payload).encode(),
        headers={"Authorization": _AUTH, "Content-Type": "application/json"},
    )
    try:
        return True, urllib.request.urlopen(req, timeout=60).read().decode()
    except Exception as e:  # noqa: BLE001 - surfaced to the caller as text
        return False, (e.read().decode() if hasattr(e, "read") else str(e))


def ensure_schema() -> None:
    """Everything the Neo4j dialect cannot express, in ArcadeDB's own SQL."""
    _arcade("/api/v1/server", {"command": f"create database {ARCADE_DB}"})
    ddl = [
        "CREATE VERTEX TYPE Passage IF NOT EXISTS",
        "CREATE PROPERTY Passage.id IF NOT EXISTS STRING",
        "CREATE PROPERTY Passage.document IF NOT EXISTS STRING",
        "CREATE PROPERTY Passage.ordinal IF NOT EXISTS INTEGER",
        "CREATE PROPERTY Passage.text IF NOT EXISTS STRING",
        # ARRAY_OF_FLOATS, not an untyped list: LSM_VECTOR indexes only a typed property.
        "CREATE PROPERTY Passage.embedding IF NOT EXISTS ARRAY_OF_FLOATS",
        "CREATE INDEX IF NOT EXISTS ON Passage (id) UNIQUE",
        "CREATE INDEX IF NOT EXISTS ON Passage (text) FULL_TEXT "
        "METADATA {analyzer:'org.apache.lucene.analysis.standard.StandardAnalyzer'}",
        f'CREATE INDEX IF NOT EXISTS ON Passage (embedding) LSM_VECTOR METADATA '
        f'{{ "dimensions": {DIMS}, "similarity": "COSINE", "quantization": "NONE" }}',
    ]
    for stmt in ddl:
        ok, body = _arcade(f"/api/v1/command/{ARCADE_DB}", {"language": "sql", "command": stmt})
        print(f"  schema {'ok  ' if ok else 'FAIL'} {stmt[:62]}" + ("" if ok else f"\n         {body[:150]}"))


KG_DB = coco.ContextKey[neo4j.ConnectionFactory]("kg_db")


@coco.lifespan
async def coco_lifespan(builder: coco.EnvironmentBuilder) -> AsyncIterator[None]:
    ensure_schema()
    builder.provide(
        KG_DB,
        neo4j.ConnectionFactory(
            uri=ARCADE_BOLT, auth=("root", ARCADE_PW), database=ARCADE_DB
        ),
    )
    yield


@dataclasses.dataclass
class Passage:
    id: str
    document: str
    ordinal: int
    text: str
    embedding: list[float]


# Two converters, deliberately. markitdown-ocr registers at priority -1.0, so enabling
# plugins replaces the built-in PDF converter for EVERY file, not just scans — and its
# pdfplumber path double-reads bold glyphs: measured on a 3-page Italian letter, 319
# adjacent duplicate letters vs 123, turning "OGGETTO" into "OOGGGGEETTTTOO" and "Egr."
# into "EEggrr..". Neither survives a search. So: plain extraction always, and the OCR
# build only for a document the text layer cannot serve at all.
_MD_PLAIN = None
_MD_OCR = None
MIN_CHARS_PER_DOC = int(os.environ.get("MIN_CHARS_PER_DOC", "32"))


def _plain():
    global _MD_PLAIN
    if _MD_PLAIN is None:
        from markitdown import MarkItDown

        _MD_PLAIN = MarkItDown(enable_plugins=False)
    return _MD_PLAIN


def _ocr():
    global _MD_OCR
    if _MD_OCR is None:
        from markitdown import MarkItDown
        from openai import OpenAI

        _MD_OCR = MarkItDown(
            enable_plugins=True,
            llm_client=OpenAI(base_url=OCR_URL, api_key="no-key"),
            llm_model=os.environ.get("OCR_MODEL", "glm-ocr"),
            llm_prompt="Text Recognition:",
        )
    return _MD_OCR


@coco.fn(memo=True)
def to_markdown(content: bytes, filename: str) -> str:
    suffix = pathlib.Path(filename).suffix
    text = _plain().convert_stream(io.BytesIO(content), file_extension=suffix).text_content or ""
    if len(text.strip()) >= MIN_CHARS_PER_DOC:
        return text
    # No usable text layer: this is a scan, and GLM-OCR is the only thing that reads it.
    return _ocr().convert_stream(io.BytesIO(content), file_extension=suffix).text_content or ""


_ALNUM_MIN = int(os.environ.get("CHUNK_MIN_ALNUM", "24"))


def _has_content(chunk: str) -> bool:
    # A chunk of markdown table rules ("--- | --- | ---") has no content but still
    # competes for top-1: measured, several of those WON the top slot for questions
    # whose answer is not in the corpus. Cheapest possible fix, applied before embedding.
    return sum(ch.isalnum() for ch in chunk) >= _ALNUM_MIN


@coco.fn(memo=True)
def split(text: str) -> list[str]:
    out, i = [], 0
    while i < len(text):
        out.append(text[i : i + CHUNK_CHARS])
        i += max(1, CHUNK_CHARS - CHUNK_OVERLAP)
    return [c for c in out if c.strip() and _has_content(c)]


@coco.fn(memo=True)
def embed(text: str) -> list[float]:
    # EmbeddingGemma is trained ASYMMETRICALLY: documents carry "title: … | text: …",
    # queries carry "task: search result | query: …". llama.cpp does not add either,
    # and neither does Aura today — indexing raw text on both sides is a silent recall
    # tax on the one component the retrieval study says sets the ceiling (D8).
    payload = {"input": f"title: none | text: {text}", "model": "embeddinggemma"}
    req = urllib.request.Request(
        EMBED_URL, data=json.dumps(payload).encode(),
        headers={"Content-Type": "application/json"},
    )
    body = json.loads(urllib.request.urlopen(req, timeout=120).read())
    return body["data"][0]["embedding"]


@coco.fn(memo=True)
async def process_file(
    file: localfs.File, table: neo4j.TableTarget[Passage]
) -> None:
    name = str(file.file_path.path)
    md = to_markdown(await file.read(), name)
    for ordinal, chunk in enumerate(split(md)):
        table.declare_record(
            row=Passage(
                id=f"{name}#{ordinal}",
                document=name,
                ordinal=ordinal,
                text=chunk,
                embedding=embed(chunk),
            )
        )


@coco.fn
async def app_main(sourcedir: pathlib.Path) -> None:
    table = await neo4j.mount_table_target(
        KG_DB,
        "Passage",
        await neo4j.TableSchema.from_class(Passage, primary_key="id"),
        primary_key="id",
    )
    files = localfs.walk_dir(
        sourcedir,
        recursive=True,
        path_matcher=PatternFilePathMatcher(
            included_patterns=["**/*.pdf"],
            excluded_patterns=["**/.*"],
        ),
    )
    await coco.mount_each(process_file, files.items(), table)


app = coco.App(
    coco.AppConfig(name="AuraIngestSpike"),
    app_main,
    sourcedir=pathlib.Path(os.environ.get("CORPUS", "/corpus")),
)
