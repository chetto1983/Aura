"""ArcadeDB schema owned by the CocoIndex document reconciler.

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
mirroring internal/arcadedb/client.go's command/server endpoint split. A third,
GET /api/v1/ready, is what ensure_schema waits on before either: see
wait_until_ready() for why a server that is merely still booting must not be
read as a server that rejects us.
"""

from __future__ import annotations

import base64
import json
import time
import urllib.error
import urllib.parse
import urllib.request

PASSAGE_TYPE = "Passage"
DOCUMENT_TYPE = "IndexedDocument"

# These names were written by withdrawn projection/version/layout models. The current
# plain-text reconciler cannot populate them, and keeping them would advertise a contract
# that no production path implements.
RETIRED_PASSAGE_PROPERTIES = (
    "passage_id", "projection_key", "document_id", "version_id", "version_number",
    "pipeline_generation", "self_ref", "captions", "page_number", "bbox_left",
    "bbox_top", "bbox_right", "bbox_bottom", "sheet_name", "table_name", "row_number",
    "column_number", "cell_reference", "active", "created_at", "tombstoned_at",
)
RETIRED_TYPES = ("DocumentProjection", "HAS_PASSAGE")

# ArcadeDB's own liveness endpoint. GetReadyHandler.isRequireAuthentication() is false,
# which is why the compose healthcheck can probe it without baking the root password into
# the file -- and why this module sends no credential either.
READY_PATH = "/api/v1/ready"

# The two statuses GetReadyHandler produces: 204 once server status is ONLINE (and, under
# HA, once the node has joined the Raft group and caught up on the committed log), 503
# while it is not. The 503 is the one that matters here: the HTTP port binds before the
# engine is online, so "the socket answered" is NOT the same claim as "DDL will work".
_READY = 204
_NOT_ONLINE_YET = 503

# Long enough to outlast the compose healthcheck's own patience for this same server
# (start_period 40s + interval 10s x retries 12 = 160s). Failing sooner than the
# orchestrator gives up would mean declaring dead a server the stack still considers
# starting.
DEFAULT_READY_TIMEOUT_S = 180.0
_POLL_INTERVAL_S = 2.0


def schema_version(dimensions: int) -> str:
    """The schema stamp Aura's Go retriever compares every candidate against.

    Mirrors internal/arcadedb/document_schema.go's DocumentIndex.schemaVersion(). The Go
    reader REJECTS any candidate whose schema_version differs from its own, so this is a
    contract, not a label: a passage stamped with anything else is written successfully
    and then never returned. It encodes the properties that would invalidate an index if
    they changed -- analyzer, similarity, quantization and dimensions.
    """
    return f"document-v1:standard-analyzer:cosine:none:{dimensions}"


class ArcadeSchemaError(RuntimeError):
    """Raised when ArcadeDB rejects database creation or a DDL statement."""


def wait_until_ready(
    base_url: str,
    *,
    timeout_s: float = DEFAULT_READY_TIMEOUT_S,
    poll_interval_s: float = _POLL_INTERVAL_S,
) -> None:
    """Block until ArcadeDB reports itself ONLINE, or raise once the budget is spent.

    ArcadeDB not being up YET and ArcadeDB rejecting what we ask are different facts, and
    conflating them is what made this sidecar crash-loop. `depends_on: condition:
    service_healthy` covers only `docker compose up`: when the Docker daemon restarts it
    rebrings every `restart: unless-stopped` container in arbitrary order, ignoring both
    depends_on and healthchecks. Measured 2026-08-23 -- aura-ingest started 6s after
    ArcadeDB, `create database` hit a port nothing was listening on, and the URLError
    killed the process. 14 such deaths in five days, in bursts, one burst per restart.

    So a transport failure and a 503 are waited out, while any other answer is raised on
    the spot: waiting three minutes on a wrong port or a proxy in the way would hide the
    fault rather than report it.
    """
    base_url = base_url.rstrip("/")
    deadline = time.monotonic() + timeout_s
    reported = False
    while True:
        not_ready = _probe_ready(base_url, poll_interval_s)
        if not_ready is None:
            if reported:
                print(f"[arcade] {base_url} ready", flush=True)
            return
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise ArcadeSchemaError(
                f"GET {base_url}{READY_PATH}: not ready within {timeout_s:g}s: {not_ready}"
            )
        if not reported:
            # Once entering the wait and once leaving it. Every probe would be the same
            # log storm this replaces; silence for three minutes would read as a hang.
            print(f"[arcade] waiting for {base_url}{READY_PATH}: {not_ready}", flush=True)
            reported = True
        time.sleep(min(poll_interval_s, remaining))


def _probe_ready(base_url: str, timeout_s: float) -> str | None:
    """None when ArcadeDB is ONLINE, else why it is not yet. Raises on anything else."""
    request = urllib.request.Request(base_url + READY_PATH, method="GET")
    try:
        with urllib.request.urlopen(request, timeout=timeout_s) as response:
            status = response.status
    except urllib.error.HTTPError as exc:
        if exc.code == _NOT_ONLINE_YET:
            return f"HTTP {exc.code} (server not ONLINE yet)"
        raise ArcadeSchemaError(f"GET {READY_PATH}: HTTP {exc.code}") from exc
    except urllib.error.URLError as exc:
        return str(exc.reason)
    if status != _READY:
        raise ArcadeSchemaError(f"GET {READY_PATH}: unexpected HTTP {status}")
    return None


def ensure_schema(
    base_url: str,
    database: str,
    auth: tuple[str, str],
    dimensions: int,
    *,
    timeout_s: float = 60.0,
    ready_timeout_s: float = DEFAULT_READY_TIMEOUT_S,
) -> None:
    """Create `database` if absent, then run every Passage DDL statement.

    Safe to call on every process start -- and that promise is why the readiness wait
    lives HERE rather than at the call site: each DDL statement carries its own IF NOT
    EXISTS, "create database" tolerates an "already exists" error the same way
    internal/arcadedb/tenant_clients.go's provision() does, and the wait absorbs a server
    that has not finished booting. A caller cannot forget the part that was missing.

    ready_timeout_s is separate from timeout_s on purpose: the latter bounds a single DDL
    statement against a server already answering, the former bounds a cold JVM coming up.
    One number for both would either abandon a slow boot or hang on a wedged statement.
    """
    if dimensions <= 0:
        raise ValueError("dimensions must be positive")
    base_url = base_url.rstrip("/")
    wait_until_ready(base_url, timeout_s=ready_timeout_s)
    _create_database(base_url, database, auth, timeout_s)
    for statement in _document_ddl(dimensions):
        _command(base_url, database, auth, statement, timeout_s)
    _drop_retired_schema(base_url, database, auth, timeout_s)


def _document_ddl(dimensions: int) -> list[str]:
    """The complete declared schema for CocoIndex's two record targets."""
    t = PASSAGE_TYPE
    return [
        f"CREATE VERTEX TYPE {t} IF NOT EXISTS",
        f"CREATE PROPERTY {t}.passage_key IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.search_document_id IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.source_kind IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.source_key IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.raw_sha256 IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.schema_version IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.ordinal IF NOT EXISTS LONG",
        f"CREATE PROPERTY {t}.text IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.normalized_text_sha256 IF NOT EXISTS STRING",
        f"CREATE PROPERTY {t}.heading_path IF NOT EXISTS LIST OF STRING",
        f"CREATE PROPERTY {t}.char_start IF NOT EXISTS LONG",
        f"CREATE PROPERTY {t}.char_end IF NOT EXISTS LONG",
        # ARRAY_OF_FLOATS, not an untyped list or LIST OF FLOAT: LSM_VECTOR
        # indexes only this exact type, and a Cypher write to a LIST OF FLOAT
        # property fails outright ("declared as LIST of 'FLOAT' but a value
        # of type 'ARRAY_OF_FLOATS' is used" -- see memory_vector.go).
        f"CREATE PROPERTY {t}.embedding IF NOT EXISTS ARRAY_OF_FLOATS",
        f"CREATE INDEX IF NOT EXISTS ON {t} (passage_key) UNIQUE",
        f"CREATE INDEX IF NOT EXISTS ON {t} (search_document_id) NOTUNIQUE",
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
        # One record per object, carrying the card. It lives HERE and not in PostgreSQL
        # because the card leg is a full-text ranking, and ArcadeDB already indexes
        # Passage.text FULL_TEXT with this same analyzer: putting the card in Postgres
        # would mean two full-text engines ranking the same words differently. ArcadeDB is
        # multi-model -- documents, vertices and edges in one store -- so the record and
        # the passages that came from it sit in the same database and the same query
        # language.
        f"CREATE VERTEX TYPE {DOCUMENT_TYPE} IF NOT EXISTS",
        f"CREATE PROPERTY {DOCUMENT_TYPE}.search_document_id IF NOT EXISTS STRING",
        f"CREATE PROPERTY {DOCUMENT_TYPE}.source_kind IF NOT EXISTS STRING",
        f"CREATE PROPERTY {DOCUMENT_TYPE}.source_key IF NOT EXISTS STRING",
        f"CREATE PROPERTY {DOCUMENT_TYPE}.file_name IF NOT EXISTS STRING",
        f"CREATE PROPERTY {DOCUMENT_TYPE}.file_name_words IF NOT EXISTS STRING",
        f"CREATE PROPERTY {DOCUMENT_TYPE}.raw_sha256 IF NOT EXISTS STRING",
        f"CREATE PROPERTY {DOCUMENT_TYPE}.size_bytes IF NOT EXISTS LONG",
        f"CREATE PROPERTY {DOCUMENT_TYPE}.passage_count IF NOT EXISTS LONG",
        f"CREATE PROPERTY {DOCUMENT_TYPE}.card IF NOT EXISTS STRING",
        f"CREATE PROPERTY {DOCUMENT_TYPE}.indexed_at IF NOT EXISTS DATETIME",
        f"CREATE INDEX IF NOT EXISTS ON {DOCUMENT_TYPE} (search_document_id) UNIQUE",
        # Same analyzer as Passage.text on purpose: the card leg and the passage leg must
        # tokenise a query identically or the two rank the same words differently.
        f"CREATE INDEX IF NOT EXISTS ON {DOCUMENT_TYPE} (card) FULL_TEXT METADATA "
        "{analyzer:'org.apache.lucene.analysis.standard.StandardAnalyzer'}",
        # file_name_words, NOT file_name. MEASURED 2026-08-08: StandardAnalyzer keeps
        # "clienti_complesso.xlsx" as ONE token -- neither underscore nor dot splits it --
        # so indexing the raw name makes a file findable only by its exact full spelling
        # and a search for "clienti" returns nothing. This is the same problem migration
        # 0081 solved for PostgreSQL with aura.searchable_text ("text as written PLUS the
        # same text split on every non-alphanumeric run"), and the same fix.
        f"CREATE INDEX IF NOT EXISTS ON {DOCUMENT_TYPE} (file_name_words) FULL_TEXT METADATA "
        "{analyzer:'org.apache.lucene.analysis.standard.StandardAnalyzer'}",
    ]


def _drop_retired_schema(
    base_url: str, database: str, auth: tuple[str, str], timeout_s: float
) -> None:
    """Remove retired values, properties and types from an existing tenant database."""
    path = f"/api/v1/query/{urllib.parse.quote(database, safe='')}"
    body = _post(
        base_url, path,
        {"language": "sql", "command": "SELECT name, properties FROM schema:types"},
        auth, timeout_s,
    )
    rows = {row.get("name"): row for row in body.get("result", []) if row.get("name")}
    passage_properties = _property_names(rows.get(PASSAGE_TYPE, {}).get("properties"))
    for property_name in RETIRED_PASSAGE_PROPERTIES:
        if property_name not in passage_properties:
            continue
        # DROP PROPERTY changes only the schema. Remove the record values first so no
        # schemaless remnants survive, then FORCE removes any index that depended on it.
        _command(base_url, database, auth, f"UPDATE {PASSAGE_TYPE} REMOVE {property_name}", timeout_s)
        _command(
            base_url, database, auth,
            f"DROP PROPERTY {PASSAGE_TYPE}.{property_name} FORCE", timeout_s,
        )
    for type_name in RETIRED_TYPES:
        if type_name in rows:
            _command(base_url, database, auth, f"DROP TYPE {type_name} UNSAFE", timeout_s)


def _property_names(value: object) -> set[str]:
    if not isinstance(value, list):
        return set()
    names: set[str] = set()
    for item in value:
        if isinstance(item, str):
            names.add(item)
        elif isinstance(item, dict) and isinstance(item.get("name"), str):
            names.add(item["name"])
    return names


def indexed_source_keys(
    base_url: str, database: str, auth: tuple[str, str], timeout_s: float
) -> set[str]:
    """The object keys that actually carry an IndexedDocument row.

    app.py declares that row AFTER mapping the chunks, so a document whose extraction or
    embedding raised leaves no row at all -- which makes the row the exact witness that a
    file made it through. A file with no extractable text still gets one, with
    passage_count 0, and that distinction is the point: silence about a scanned image is
    correct, silence about a statute is a lost document.
    """
    path = f"/api/v1/query/{urllib.parse.quote(database, safe='')}"
    body = _post(
        base_url, path,
        {"language": "sql", "command": f"SELECT source_key FROM {DOCUMENT_TYPE}"},
        auth, timeout_s,
    )
    return {row["source_key"] for row in body.get("result", []) if row.get("source_key")}


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
