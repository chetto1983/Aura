"""ArcadeDB schema for the Passage vertex type the ingestion pipeline writes.

Passage's property names, its indexes, and its HAS_PASSAGE edge type are NOT
this module's to invent: they mirror internal/arcadedb/document_schema.go's
documentSchemaStatements exactly, because that is the schema Aura's Go
retriever reads in production -- a name invented here would write rows the
product cannot see. document_schema.go survives this rewrite; if its
statements move, this list must move with them.

DocumentProjection is GONE from both sides (2026-08-08). It was the other
vertex type declared there, this sidecar never populated it, and the Go
generation/tombstone writer that was its only author was deleted with the
in-process pipeline -- so declaring it was dead schema.

One deliberate exception: source_kind/source_key are declared here but not
in document_schema.go. Go's retriever doesn't read them yet; they exist so a
retrieval hit can be traced back to the Garage object it came from. ArcadeDB
property creation is additive and IF NOT EXISTS, so a Go-created Passage type
tolerates these extra properties without conflict.

ensure_schema() MUST run before the first passage write, every run,
idempotently. CocoIndex's Neo4j-dialect Cypher MERGE creates an untyped list
for a list-valued property, and ArcadeDB's LSM_VECTOR index then refuses to
index it -- measured on the live stack:
    "LSM_VECTOR index requires METADATA with dimensions, similarity,
     maxConnections, and beamWidth"
Declaring the typed ARRAY_OF_FLOATS property and its LSM_VECTOR index up
front, in ArcadeDB SQL, before any Bolt write reaches it, is what avoids
that -- see the @coco.lifespan hook in
spikes/cocoindex-ingestion/flows/aura_flow.py for the pattern this module
generalises into a reusable, tested function.

Transport is ArcadeDB's HTTP API, not Bolt: ArcadeDB's Bolt port is
Cypher-only and rejects SQL DDL with Neo.ClientError.Statement.SyntaxError
(measured). LSM_VECTOR/FULL_TEXT index METADATA has no Cypher equivalent, so
schema DDL goes over POST /api/v1/command/<db> as ArcadeDB SQL, and database
creation over POST /api/v1/server -- both ArcadeDB's own HTTP API surface,
mirroring internal/arcadedb/client.go's command/server endpoint split.
"""

from __future__ import annotations

import base64
import json
import urllib.error
import urllib.parse
import urllib.request

PASSAGE_TYPE = "Passage"
PASSAGE_EDGE_TYPE = "HAS_PASSAGE"


class ArcadeSchemaError(RuntimeError):
    """Raised when ArcadeDB rejects database creation or a DDL statement."""


def ensure_schema(
    base_url: str,
    database: str,
    auth: tuple[str, str],
    dimensions: int,
    *,
    timeout_s: float = 60.0,
) -> None:
    """Create `database` if absent, then run every Passage DDL statement.

    Safe to call on every process start: each DDL statement carries its own
    IF NOT EXISTS, and "create database" tolerates an "already exists" error
    the same way internal/arcadedb/tenant_clients.go's provision() does --
    ArcadeDB's server command has no IF NOT EXISTS form of its own.
    """
    if dimensions <= 0:
        raise ValueError("dimensions must be positive")
    base_url = base_url.rstrip("/")
    _create_database(base_url, database, auth, timeout_s)
    for statement in _passage_ddl(dimensions):
        _command(base_url, database, auth, statement, timeout_s)


def _passage_ddl(dimensions: int) -> list[str]:
    """The exact Passage + HAS_PASSAGE statements from document_schema.go:276-319."""
    t = PASSAGE_TYPE
    return [
        f"CREATE VERTEX TYPE {t} IF NOT EXISTS",
        f"CREATE PROPERTY {t}.passage_key IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.passage_id IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.projection_key IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.document_id IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.search_document_id IF NOT EXISTS STRING",
        # Not in document_schema.go yet (Go retrieval doesn't consume these):
        # added here so a retrieval hit can resolve back to the object it came
        # from. Property creation is additive/idempotent, so this does not
        # conflict with the Go-created type -- see this module's docstring.
        f"CREATE PROPERTY {t}.source_kind IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.source_key IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.version_id IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.version_number IF NOT EXISTS LONG",
        f"CREATE PROPERTY {t}.raw_sha256 IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.pipeline_generation IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.schema_version IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.ordinal IF NOT EXISTS LONG",
        f"CREATE PROPERTY {t}.text IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.normalized_text_sha256 IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.self_ref IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.heading_path IF NOT EXISTS LIST OF STRING",
        f"CREATE PROPERTY {t}.captions IF NOT EXISTS LIST OF STRING",
        f"CREATE PROPERTY {t}.page_number IF NOT EXISTS LONG",
        f"CREATE PROPERTY {t}.bbox_left IF NOT EXISTS DOUBLE",
        f"CREATE PROPERTY {t}.bbox_top IF NOT EXISTS DOUBLE",
        f"CREATE PROPERTY {t}.bbox_right IF NOT EXISTS DOUBLE",
        f"CREATE PROPERTY {t}.bbox_bottom IF NOT EXISTS DOUBLE",
        f"CREATE PROPERTY {t}.char_start IF NOT EXISTS LONG",
        f"CREATE PROPERTY {t}.char_end IF NOT EXISTS LONG",
        f"CREATE PROPERTY {t}.sheet_name IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.table_name IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.row_number IF NOT EXISTS LONG",
        f"CREATE PROPERTY {t}.column_number IF NOT EXISTS LONG",
        f"CREATE PROPERTY {t}.cell_reference IF NOT EXISTS STRING",
        # ARRAY_OF_FLOATS, not an untyped list or LIST OF FLOAT: LSM_VECTOR
        # indexes only this exact type, and a Cypher write to a LIST OF FLOAT
        # property fails outright ("declared as LIST of 'FLOAT' but a value
        # of type 'ARRAY_OF_FLOATS' is used" -- see memory_vector.go).
        f"CREATE PROPERTY {t}.embedding IF NOT EXISTS ARRAY_OF_FLOATS",
        f"CREATE PROPERTY {t}.active IF NOT EXISTS BOOLEAN",
        f"CREATE PROPERTY {t}.created_at IF NOT EXISTS DATETIME",
        f"CREATE PROPERTY {t}.tombstoned_at IF NOT EXISTS DATETIME",
        f"CREATE INDEX IF NOT EXISTS ON {t} (passage_key) UNIQUE",
        f"CREATE INDEX IF NOT EXISTS ON {t} (projection_key) NOTUNIQUE",
        f"CREATE INDEX IF NOT EXISTS ON {t} (active, document_id) NOTUNIQUE",
        # The corpus is multilingual (Italian/English business documents), so
        # the analyzer is pinned explicitly rather than left at ArcadeDB's
        # per-language default.
        f"CREATE INDEX IF NOT EXISTS ON {t} (text) FULL_TEXT METADATA "
        "{analyzer:'org.apache.lucene.analysis.standard.StandardAnalyzer'}",
        # A bare LSM_VECTOR index (no METADATA) is rejected outright --
        # measured: "LSM_VECTOR index requires METADATA with dimensions,
        # similarity, maxConnections, and beamWidth". quantization: NONE
        # matches document_schema.go; ArcadeDB's manual recommends INT8 only
        # above 10K vectors, which does not apply at this corpus size.
        f"CREATE INDEX IF NOT EXISTS ON {t} (embedding) LSM_VECTOR METADATA "
        f'{{ "dimensions": {dimensions}, "similarity": "COSINE", "quantization": "NONE" }}',
        f"CREATE EDGE TYPE {PASSAGE_EDGE_TYPE} IF NOT EXISTS",
    ]


def _create_database(base_url: str, database: str, auth: tuple[str, str], timeout_s: float) -> None:
    try:
        _post(base_url, "/api/v1/server", {"command": f"create database {database}"}, auth, timeout_s)
    except ArcadeSchemaError as exc:
        if "already exists" not in str(exc).lower():
            raise


def _command(base_url: str, database: str, auth: tuple[str, str], statement: str, timeout_s: float) -> None:
    path = f"/api/v1/command/{urllib.parse.quote(database, safe='')}"
    _post(base_url, path, {"language": "sql", "command": statement}, auth, timeout_s)


def _post(base_url: str, path: str, payload: dict, auth: tuple[str, str], timeout_s: float) -> dict:
    user, password = auth
    token = base64.b64encode(f"{user}:{password}".encode()).decode()
    request = urllib.request.Request(
        base_url + path,
        data=json.dumps(payload).encode(),
        headers={"Authorization": f"Basic {token}", "Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout_s) as response:
            body = response.read().decode()
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode() if exc.fp else str(exc)
        raise ArcadeSchemaError(f"POST {path}: HTTP {exc.code}: {detail}") from exc
    except urllib.error.URLError as exc:
        raise ArcadeSchemaError(f"POST {path}: {exc.reason}") from exc
    return json.loads(body) if body else {}
