"""Same pipeline, source swapped from localfs to S3 (Garage).

The point of this spike is NOT the ingest — that is already proven — it is what happens
on the SECOND run: a modified object must be reprocessed, a deleted object must take its
passages with it, and everything untouched must cost nothing. CocoIndex detects both from
the S3 ETag it stores as the content fingerprint; no bookkeeping of ours.
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
from aiobotocore.session import get_session
from cocoindex.connectors import amazon_s3, neo4j
from cocoindex.resources.file import PatternFilePathMatcher

ARCADE_HTTP = os.environ.get("ARCADE_HTTP", "http://aura-arcadedb:2480")
ARCADE_BOLT = os.environ.get("ARCADE_BOLT", "bolt://aura-arcadedb:7687")
ARCADE_DB = os.environ.get("ARCADE_DB", "aura_s3_spike")
ARCADE_PW = os.environ["ARCADEDB_PASSWORD"]
EMBED_URL = os.environ.get("EMBED_URL", "http://aura-llama-embed:8081/v1/embeddings")
DIMS = int(os.environ.get("EMBED_DIMS", "768"))
CHUNK_CHARS, CHUNK_OVERLAP = 1200, 150

S3_ENDPOINT = os.environ.get("S3_ENDPOINT", "http://aura-garage:3900")
S3_BUCKET = os.environ.get("S3_BUCKET", "cocoindex-spike")
S3_KEY = os.environ["S3_ACCESS_KEY"]
S3_SECRET = os.environ["S3_SECRET_KEY"]

_AUTH = "Basic " + base64.b64encode(f"root:{ARCADE_PW}".encode()).decode()


def _arcade(path: str, payload: dict):
    req = urllib.request.Request(
        f"{ARCADE_HTTP}{path}", data=json.dumps(payload).encode(),
        headers={"Authorization": _AUTH, "Content-Type": "application/json"})
    try:
        return True, urllib.request.urlopen(req, timeout=60).read().decode()
    except Exception as e:  # noqa: BLE001
        return False, (e.read().decode() if hasattr(e, "read") else str(e))


def ensure_schema() -> None:
    _arcade("/api/v1/server", {"command": f"create database {ARCADE_DB}"})
    for stmt in [
        "CREATE VERTEX TYPE Passage IF NOT EXISTS",
        "CREATE PROPERTY Passage.id IF NOT EXISTS STRING",
        "CREATE PROPERTY Passage.document IF NOT EXISTS STRING",
        "CREATE PROPERTY Passage.ordinal IF NOT EXISTS INTEGER",
        "CREATE PROPERTY Passage.text IF NOT EXISTS STRING",
        "CREATE PROPERTY Passage.embedding IF NOT EXISTS ARRAY_OF_FLOATS",
        "CREATE INDEX IF NOT EXISTS ON Passage (id) UNIQUE",
        f'CREATE INDEX IF NOT EXISTS ON Passage (embedding) LSM_VECTOR METADATA '
        f'{{ "dimensions": {DIMS}, "similarity": "COSINE", "quantization": "NONE" }}',
    ]:
        _arcade(f"/api/v1/command/{ARCADE_DB}", {"language": "sql", "command": stmt})


KG_DB = coco.ContextKey[neo4j.ConnectionFactory]("kg_db")
S3 = coco.ContextKey[object]("s3_client")


@coco.lifespan
async def coco_lifespan(builder: coco.EnvironmentBuilder) -> AsyncIterator[None]:
    ensure_schema()
    builder.provide(KG_DB, neo4j.ConnectionFactory(
        uri=ARCADE_BOLT, auth=("root", ARCADE_PW), database=ARCADE_DB))
    session = get_session()
    # Garage is S3-compatible; the connector takes a client we build, so the custom
    # endpoint lives here and nothing in the connector needs to know about it.
    async with session.create_client(
        "s3", endpoint_url=S3_ENDPOINT,
        aws_access_key_id=S3_KEY, aws_secret_access_key=S3_SECRET,
        region_name=os.environ.get("S3_REGION", "garage"),
    ) as client:
        builder.provide(S3, client)
        yield


@dataclasses.dataclass
class Passage:
    id: str
    document: str
    ordinal: int
    text: str
    embedding: list[float]


_MD = None


def _md():
    global _MD
    if _MD is None:
        from markitdown import MarkItDown

        _MD = MarkItDown(enable_plugins=False)
    return _MD


@coco.fn(memo=True)
def to_markdown(content: bytes, filename: str) -> str:
    return _md().convert_stream(
        io.BytesIO(content), file_extension=pathlib.Path(filename).suffix).text_content or ""


@coco.fn(memo=True)
def split(text: str) -> list[str]:
    out, i = [], 0
    while i < len(text):
        out.append(text[i : i + CHUNK_CHARS])
        i += max(1, CHUNK_CHARS - CHUNK_OVERLAP)
    return [c for c in out if sum(ch.isalnum() for ch in c) >= 24]


@coco.fn(memo=True)
def embed(text: str) -> list[float]:
    req = urllib.request.Request(
        EMBED_URL,
        data=json.dumps({"input": f"title: none | text: {text}", "model": "embeddinggemma"}).encode(),
        headers={"Content-Type": "application/json"})
    return json.loads(urllib.request.urlopen(req, timeout=120).read())["data"][0]["embedding"]


@coco.fn(memo=True)
async def process_file(file, table: neo4j.TableTarget[Passage]) -> None:
    name = file.file_path.path.as_posix()
    print(f"    [process] {name}", flush=True)
    md = to_markdown(await file.read(), name)
    for ordinal, chunk in enumerate(split(md)):
        table.declare_record(row=Passage(
            id=f"{name}#{ordinal}", document=name, ordinal=ordinal,
            text=chunk, embedding=embed(chunk)))


@coco.fn
async def app_main() -> None:
    table = await neo4j.mount_table_target(
        KG_DB, "Passage",
        await neo4j.TableSchema.from_class(Passage, primary_key="id"), primary_key="id")
    walker = amazon_s3.list_objects(
        coco.use_context(S3), S3_BUCKET,
        path_matcher=PatternFilePathMatcher(included_patterns=["**/*.pdf"]))
    await coco.mount_each(process_file, walker.items(), table)


app = coco.App(coco.AppConfig(name="AuraS3Spike"), app_main)
