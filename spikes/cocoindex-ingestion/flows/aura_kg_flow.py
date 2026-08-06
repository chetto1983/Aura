"""Aura GraphRAG spike: PDFs -> MarkItDown -> {dense passages + knowledge graph} -> ArcadeDB.

Follows cocoindex's docs_to_knowledge_graph pattern (2 memoized LLM calls per DOCUMENT —
summary and triples — not per chunk, and not Microsoft GraphRAG's community-summarization
pass), extended with the Passage/embedding leg so dense retrieval and the graph share one
store. Target is CocoIndex's STOCK Neo4j connector over ArcadeDB's Bolt port.
"""

from __future__ import annotations

import asyncio
import base64
import dataclasses
import hashlib
import io
import json
import os
import pathlib
from collections.abc import AsyncIterator
from typing import Any

import cocoindex as coco
import instructor
import litellm
import pydantic
from cocoindex.connectors import localfs, neo4j
from cocoindex.resources.file import PatternFilePathMatcher

litellm.drop_params = True

ARCADE_HTTP = os.environ.get("ARCADE_HTTP", "http://aura-arcadedb:2480")
ARCADE_BOLT = os.environ.get("ARCADE_BOLT", "bolt://aura-arcadedb:7687")
ARCADE_DB = os.environ.get("ARCADE_DB", "aura_kg_spike")
ARCADE_PW = os.environ["ARCADEDB_PASSWORD"]
EMBED_URL = os.environ.get("EMBED_URL", "http://aura-llama-embed:8081/v1/embeddings")
OCR_URL = os.environ.get("OCR_BASE_URL", "http://aura-ocr-vl:8082/v1")
DIMS = int(os.environ.get("EMBED_DIMS", "768"))
CHUNK_CHARS = int(os.environ.get("CHUNK_CHARS", "1200"))
CHUNK_OVERLAP = int(os.environ.get("CHUNK_OVERLAP", "150"))
MIN_CHARS_PER_DOC = int(os.environ.get("MIN_CHARS_PER_DOC", "32"))
MAX_LLM_CHARS = int(os.environ.get("MAX_LLM_CHARS", "12000"))

_AUTH = "Basic " + base64.b64encode(f"root:{ARCADE_PW}".encode()).decode()

import urllib.request


def _arcade(path: str, payload: dict) -> tuple[bool, str]:
    req = urllib.request.Request(
        f"{ARCADE_HTTP}{path}", data=json.dumps(payload).encode(),
        headers={"Authorization": _AUTH, "Content-Type": "application/json"})
    try:
        return True, urllib.request.urlopen(req, timeout=60).read().decode()
    except Exception as e:  # noqa: BLE001
        return False, (e.read().decode() if hasattr(e, "read") else str(e))


def ensure_schema() -> None:
    """Native ArcadeDB DDL: vertex/edge types and the vector index Cypher cannot declare."""
    _arcade("/api/v1/server", {"command": f"create database {ARCADE_DB}"})
    ddl = [
        "CREATE VERTEX TYPE Passage IF NOT EXISTS",
        "CREATE PROPERTY Passage.id IF NOT EXISTS STRING",
        "CREATE PROPERTY Passage.document IF NOT EXISTS STRING",
        "CREATE PROPERTY Passage.ordinal IF NOT EXISTS INTEGER",
        "CREATE PROPERTY Passage.text IF NOT EXISTS STRING",
        "CREATE PROPERTY Passage.embedding IF NOT EXISTS ARRAY_OF_FLOATS",
        "CREATE INDEX IF NOT EXISTS ON Passage (id) UNIQUE",
        "CREATE INDEX IF NOT EXISTS ON Passage (text) FULL_TEXT "
        "METADATA {analyzer:'org.apache.lucene.analysis.standard.StandardAnalyzer'}",
        f'CREATE INDEX IF NOT EXISTS ON Passage (embedding) LSM_VECTOR METADATA '
        f'{{ "dimensions": {DIMS}, "similarity": "COSINE", "quantization": "NONE" }}',
        "CREATE VERTEX TYPE Document IF NOT EXISTS",
        "CREATE PROPERTY Document.filename IF NOT EXISTS STRING",
        "CREATE INDEX IF NOT EXISTS ON Document (filename) UNIQUE",
        "CREATE VERTEX TYPE Entity IF NOT EXISTS",
        "CREATE PROPERTY Entity.value IF NOT EXISTS STRING",
        "CREATE INDEX IF NOT EXISTS ON Entity (value) UNIQUE",
        "CREATE EDGE TYPE RELATIONSHIP IF NOT EXISTS",
        "CREATE EDGE TYPE MENTION IF NOT EXISTS",
    ]
    for stmt in ddl:
        ok, body = _arcade(f"/api/v1/command/{ARCADE_DB}", {"language": "sql", "command": stmt})
        if not ok:
            print(f"  schema FAIL {stmt[:58]}\n         {body[:140]}")
    print(f"  schema ready ({len(ddl)} statements)")


KG_DB = coco.ContextKey[neo4j.ConnectionFactory]("kg_db")
LLM_MODEL = coco.ContextKey[str]("llm_model", detect_change=True)


@coco.lifespan
async def coco_lifespan(builder: coco.EnvironmentBuilder) -> AsyncIterator[None]:
    ensure_schema()
    builder.provide(KG_DB, neo4j.ConnectionFactory(
        uri=ARCADE_BOLT, auth=("root", ARCADE_PW), database=ARCADE_DB))
    builder.provide(LLM_MODEL, os.environ.get(
        "KG_LLM_MODEL", "openrouter/deepseek/deepseek-v4-flash-0731:nitro"))
    yield


# --- graph/table row schemas -------------------------------------------------
@dataclasses.dataclass
class Passage:
    id: str
    document: str
    ordinal: int
    text: str
    embedding: list[float]


@dataclasses.dataclass
class Document:
    filename: str
    title: str
    summary: str


@dataclasses.dataclass
class Entity:
    value: str


@dataclasses.dataclass
class Relationship:
    id: str
    predicate: str


@dataclasses.dataclass
class Triple:
    subject: str
    predicate: str
    object: str


@dataclasses.dataclass
class DocTriples:
    filename: str
    triples: list[Triple]


# --- LLM extraction ----------------------------------------------------------
class DocumentSummary(pydantic.BaseModel):
    title: str
    summary: str


class ExtractedRelationship(pydantic.BaseModel):
    subject: str
    predicate: str
    object: str


class RelationshipList(pydantic.BaseModel):
    relationships: list[ExtractedRelationship]


SUMMARY_PROMPT = (
    "You are reading a business document that may be in Italian. "
    "Return a short title and a one-paragraph summary. "
    "Write BOTH in the document's own language."
)
RELATIONSHIP_PROMPT = (
    "Extract factual (subject, predicate, object) triples from this document. "
    "Subjects and objects must be concrete named entities — people, organisations, "
    "contracts, accounts, amounts, dates, places. Keep entity strings verbatim from "
    "the document so they deduplicate across documents. Predicates are short verb "
    "phrases. Return at most 25 triples."
)


@coco.fn(memo=True)
async def extract_summary(content: str) -> DocumentSummary:
    client = instructor.from_litellm(litellm.acompletion, mode=instructor.Mode.JSON)
    result = await client.chat.completions.create(
        model=coco.use_context(LLM_MODEL),
        response_model=DocumentSummary,
        messages=[{"role": "system", "content": SUMMARY_PROMPT},
                  {"role": "user", "content": content[:MAX_LLM_CHARS]}],
    )
    return DocumentSummary.model_validate(result.model_dump())


@coco.fn(memo=True)
async def extract_relationships(content: str) -> list[Triple]:
    client = instructor.from_litellm(litellm.acompletion, mode=instructor.Mode.JSON)
    result = await client.chat.completions.create(
        model=coco.use_context(LLM_MODEL),
        response_model=RelationshipList,
        messages=[{"role": "system", "content": RELATIONSHIP_PROMPT},
                  {"role": "user", "content": content[:MAX_LLM_CHARS]}],
    )
    validated = RelationshipList.model_validate(result.model_dump())
    return [Triple(r.subject, r.predicate, r.object) for r in validated.relationships]


# --- extraction / chunking / embedding ---------------------------------------
_MD_PLAIN = None
_MD_OCR = None


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
    # Plain first: markitdown-ocr registers at priority -1.0 and replaces the built-in
    # PDF converter for EVERY file, and its pdfplumber path double-reads bold glyphs
    # ("OGGETTO" -> "OOGGGGEETTTTOO"), which no search recovers. Only a document with no
    # usable text layer is worth that path.
    suffix = pathlib.Path(filename).suffix
    text = _plain().convert_stream(io.BytesIO(content), file_extension=suffix).text_content or ""
    if len(text.strip()) >= MIN_CHARS_PER_DOC:
        return text
    return _ocr().convert_stream(io.BytesIO(content), file_extension=suffix).text_content or ""


@coco.fn(memo=True)
def split(text: str) -> list[str]:
    out, i = [], 0
    while i < len(text):
        out.append(text[i : i + CHUNK_CHARS])
        i += max(1, CHUNK_CHARS - CHUNK_OVERLAP)
    return [c for c in out if c.strip()]


@coco.fn(memo=True)
def embed(text: str) -> list[float]:
    req = urllib.request.Request(
        EMBED_URL,
        data=json.dumps({"input": text, "model": "embeddinggemma"}).encode(),
        headers={"Content-Type": "application/json"})
    return json.loads(urllib.request.urlopen(req, timeout=120).read())["data"][0]["embedding"]


# --- phase 1: per document ---------------------------------------------------
@coco.fn(memo=True)
async def process_file(
    file: localfs.File,
    passage_table: neo4j.TableTarget[Passage],
    document_table: neo4j.TableTarget[Document],
) -> DocTriples:
    name = file.file_path.path.as_posix()
    md = to_markdown(await file.read(), name)

    for ordinal, chunk in enumerate(split(md)):
        passage_table.declare_record(row=Passage(
            id=f"{name}#{ordinal}", document=name, ordinal=ordinal,
            text=chunk, embedding=embed(chunk)))

    summary = await extract_summary(md)
    document_table.declare_record(row=Document(
        filename=name, title=summary.title, summary=summary.summary))

    return DocTriples(filename=name, triples=await extract_relationships(md))


# --- phase 2: dedup entities, then edges --------------------------------------
@coco.fn
async def build_graph(
    docs: list[DocTriples],
    entity_table: neo4j.TableTarget[Entity],
    relationship_rel: neo4j.RelationTarget[Relationship],
    mention_rel: neo4j.RelationTarget[Any],
) -> None:
    entities: set[str] = set()
    mentions: set[tuple[str, str]] = set()
    for doc in docs:
        for t in doc.triples:
            entities.add(t.subject)
            entities.add(t.object)
            mentions.add((doc.filename, t.subject))
            mentions.add((doc.filename, t.object))
            rel_id = hashlib.sha256(
                f"{t.subject}\x00{t.predicate}\x00{t.object}".encode()).hexdigest()[:32]
            relationship_rel.declare_relation(
                from_id=t.subject, to_id=t.object,
                record=Relationship(id=rel_id, predicate=t.predicate))
    for value in entities:
        entity_table.declare_record(row=Entity(value=value))
    for filename, entity in mentions:
        mention_rel.declare_relation(from_id=filename, to_id=entity)


@coco.fn
async def app_main(sourcedir: pathlib.Path) -> None:
    passage_table = await neo4j.mount_table_target(
        KG_DB, "Passage",
        await neo4j.TableSchema.from_class(Passage, primary_key="id"), primary_key="id")
    document_table = await neo4j.mount_table_target(
        KG_DB, "Document",
        await neo4j.TableSchema.from_class(Document, primary_key="filename"),
        primary_key="filename")
    entity_table = await neo4j.mount_table_target(
        KG_DB, "Entity",
        await neo4j.TableSchema.from_class(Entity, primary_key="value"), primary_key="value")
    relationship_rel = await neo4j.mount_relation_target(
        KG_DB, "RELATIONSHIP", entity_table, entity_table,
        await neo4j.TableSchema.from_class(Relationship, primary_key="id"), primary_key="id")
    mention_rel = await neo4j.mount_relation_target(
        KG_DB, "MENTION", document_table, entity_table)

    files = localfs.walk_dir(
        sourcedir, recursive=True,
        path_matcher=PatternFilePathMatcher(
            included_patterns=["**/*.pdf", "**/*.docx", "**/*.pptx", "**/*.md"],
            excluded_patterns=["**/.*"]))

    coros = []
    async for path_key, file in files.items():
        coros.append(coco.use_mount(
            coco.component_subpath("file", path_key),
            process_file, file, passage_table, document_table))
    docs: list[DocTriples] = list(await asyncio.gather(*coros))

    await coco.mount(
        coco.component_subpath("build_graph"),
        build_graph, docs, entity_table, relationship_rel, mention_rel)


app = coco.App(
    coco.AppConfig(name="AuraKgSpike"),
    app_main,
    sourcedir=pathlib.Path(os.environ.get("CORPUS", "/corpus")),
)
