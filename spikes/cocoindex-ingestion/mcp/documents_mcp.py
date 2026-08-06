"""Aura documents MCP — the retrieval half of the document domain.

Sits in the same sidecar as the CocoIndex ingestion flow, so one process owns documents
end to end. Mirrors what `cmd/arcadedb-mcp` does for memory.

Three tools move here cleanly. `document_open` deliberately does NOT: it routes to the
caller's sandbox and copies bytes in (`document_open.go:111`, "a box that cannot be
reached DENIES — there is no host arm to fall back to"), which needs a sandbox handle no
MCP has. This server resolves WHICH file; the Go tool still does the copy-in.
"""

from __future__ import annotations

import base64
import json
import os
import re
import urllib.request
from typing import Any, Optional

import instructor
import litellm
import pydantic
from mcp.server.fastmcp import FastMCP
from pydantic import BaseModel, ConfigDict, Field

litellm.drop_params = True

ARCADE = os.environ.get("ARCADE_HTTP", "http://aura-arcadedb:2480")
DB = os.environ.get("ARCADE_DB", "aura_manual_spike")
PW = os.environ["ARCADEDB_PASSWORD"]
EMBED_URL = os.environ.get("EMBED_URL", "http://aura-llama-embed:8081/v1/embeddings")
MODEL = os.environ.get("KG_LLM_MODEL", "openrouter/deepseek/deepseek-v4-flash-0731:nitro")
AUTH = "Basic " + base64.b64encode(f"root:{PW}".encode()).decode()

K_DENSE = int(os.environ.get("K_DENSE", "10"))
K_LEX = int(os.environ.get("K_LEX", "10"))
CAND_CHARS = int(os.environ.get("CAND_CHARS", "1400"))

mcp = FastMCP(
    "documents_mcp",
    # Explicit, not via FASTMCP_* env: the settings were not picked up from the
    # environment and the server silently bound 127.0.0.1:8000, unreachable from
    # another container.
    host=os.environ.get("MCP_HOST", "0.0.0.0"),
    port=int(os.environ.get("MCP_PORT", "8099")),
)

STOP = set(
    """qual quale quali cosa chi come dove quando quanto quanta quante quanti devo deve
    essere stato stata sono il lo la i gli le un uno una di del della dei delle degli a al
    alla ai alle da dal dalla in nel nella con su per tra fra che non piu e o si no what
    which where when who how the of for and""".split()
)


def _sql(cmd: str) -> list[dict[str, Any]]:
    req = urllib.request.Request(
        f"{ARCADE}/api/v1/command/{DB}",
        data=json.dumps({"language": "sql", "command": cmd}).encode(),
        headers={"Authorization": AUTH, "Content-Type": "application/json"},
    )
    return json.loads(urllib.request.urlopen(req, timeout=60).read())["result"]


def _embed(text: str, kind: str = "query") -> list[float]:
    # EmbeddingGemma is asymmetric; the prefixes are not decoration.
    body = (
        f"task: search result | query: {text}" if kind == "query" else f"title: none | text: {text}"
    )
    req = urllib.request.Request(
        EMBED_URL,
        data=json.dumps({"input": body, "model": "embeddinggemma"}).encode(),
        headers={"Content-Type": "application/json"},
    )
    return json.loads(urllib.request.urlopen(req, timeout=120).read())["data"][0]["embedding"]


def _quote(v: str) -> str:
    return v.replace("'", "")


def _dense(query: str, where: str) -> dict[str, tuple[str, str]]:
    vec = _embed(query)
    rows = _sql(f'SELECT expand(vector.neighbors("Passage[embedding]", {json.dumps(vec)}, {K_DENSE}))')
    out = {}
    for r in rows:
        if r.get("id") and (not where or where in (r.get("document") or "")):
            out[r["id"]] = (r.get("document") or "", r.get("text") or "")
    return out


def _lexical(query: str, prefix: str) -> dict[str, tuple[str, str]]:
    """Term-overlap over the Lucene index. This is the leg dense cannot serve: rare
    identifiers (order numbers, codici fiscali, meter readings) carry no semantic signal,
    and were measured missing from dense@3 while CONTAINSTEXT found every one."""
    terms = [w for w in re.findall(r"[\w./-]{4,}", query.lower()) if w not in STOP]
    hits: dict[str, int] = {}
    meta: dict[str, tuple[str, str]] = {}
    for t in terms:
        clause = f"text CONTAINSTEXT '{_quote(t)}'"
        if prefix:
            clause += f" AND document LIKE '%{_quote(prefix)}%'"
        try:
            rows = _sql(f"SELECT id, document, text FROM Passage WHERE {clause} LIMIT 25")
        except Exception:
            continue
        for r in rows:
            if pid := r.get("id"):
                hits[pid] = hits.get(pid, 0) + 1
                meta[pid] = (r.get("document") or "", r.get("text") or "")
    return {p: meta[p] for p in sorted(hits, key=lambda p: -hits[p])[:K_LEX]}


class _Verdict(pydantic.BaseModel):
    indice: int = Field(description="1-based index of the passage that answers, or 0 if none does")
    motivo: str


_RERANK = (
    "Ti do una domanda e alcuni passaggi numerati estratti da documenti. "
    "Scegli l'UNICO passaggio che contiene la risposta esplicita alla domanda. "
    "Se NESSUN passaggio contiene la risposta, rispondi indice=0. "
    "Non dedurre, non inferire: se il dato non e' scritto, indice=0."
)


class SearchInput(BaseModel):
    """Find the passage that answers a question, or abstain."""

    model_config = ConfigDict(str_strip_whitespace=True, extra="forbid")

    query: str = Field(..., description="Natural-language question, fact, topic, or filename.", min_length=1)
    prefix: Optional[str] = Field(
        default=None,
        description=(
            "Restrict to documents whose path contains this S3 prefix, e.g. 'clienti/rossi/'. "
            "Use it whenever the question names an entity that is a folder — it is a "
            "deterministic filter and beats semantic matching on names and codes."
        ),
    )
    limit: int = Field(default=5, description="Maximum passages to return.", ge=1, le=25)


@mcp.tool(
    name="document_search",
    annotations={"readOnlyHint": True, "destructiveHint": False, "idempotentHint": True, "openWorldHint": False},
)
async def document_search(params: SearchInput) -> str:
    """Search indexed documents and return the passage that answers, or an explicit
    "no answer" when nothing does.

    Combines dense vector search with exact-token search, then reranks. The reranker is
    allowed to abstain: distance alone cannot separate answerable from unanswerable
    (measured, the worst true answer sits farther than the best false one), so a top-k
    result is never returned as an answer on its own.
    """
    prefix = params.prefix or ""
    merged = {**_lexical(params.query, prefix), **_dense(params.query, prefix)}
    if not merged:
        return json.dumps({"answered": False, "reason": "nessun candidato", "prefix": prefix})

    cands = list(merged.items())
    listing = "\n\n".join(f"[{i + 1}] {t[:CAND_CHARS]}" for i, (_, (_, t)) in enumerate(cands))
    client = instructor.from_litellm(litellm.acompletion, mode=instructor.Mode.JSON)
    v = await client.chat.completions.create(
        model=MODEL,
        response_model=_Verdict,
        temperature=0.0,
        messages=[
            {"role": "system", "content": _RERANK},
            {"role": "user", "content": f"DOMANDA: {params.query}\n\nPASSAGGI:\n{listing}"},
        ],
    )
    if not (1 <= v.indice <= len(cands)):
        return json.dumps(
            {"answered": False, "reason": v.motivo, "candidates_considered": len(cands), "prefix": prefix},
            ensure_ascii=False,
        )
    pid, (doc, text) = cands[v.indice - 1]
    return json.dumps(
        {
            "answered": True,
            "document": doc,
            "passage_id": pid,
            "text": text,
            "why": v.motivo,
            "candidates_considered": len(cands),
        },
        ensure_ascii=False,
    )


class DescribeInput(BaseModel):
    model_config = ConfigDict(str_strip_whitespace=True, extra="forbid")
    document: str = Field(..., description="Document path or S3 key, e.g. 'clienti/rossi/mutuo.pdf'.", min_length=1)


@mcp.tool(
    name="document_describe",
    annotations={"readOnlyHint": True, "destructiveHint": False, "idempotentHint": True, "openWorldHint": False},
)
async def document_describe(params: DescribeInput) -> str:
    """Structure of one indexed document: passage count and its opening text."""
    doc = _quote(params.document)
    rows = _sql(f"SELECT count(*) AS n FROM Passage WHERE document LIKE '%{doc}%'")
    n = rows[0]["n"] if rows else 0
    if not n:
        return json.dumps({"found": False, "document": params.document}, ensure_ascii=False)
    head = _sql(f"SELECT document, text FROM Passage WHERE document LIKE '%{doc}%' ORDER BY ordinal LIMIT 1")
    return json.dumps(
        {
            "found": True,
            "document": head[0]["document"] if head else params.document,
            "passages": n,
            "opening": (head[0]["text"] if head else "")[:600],
        },
        ensure_ascii=False,
    )


class ResolveInput(BaseModel):
    model_config = ConfigDict(str_strip_whitespace=True, extra="forbid")
    prefix: Optional[str] = Field(default=None, description="List only documents whose path contains this prefix.")


@mcp.tool(
    name="document_resolve",
    annotations={"readOnlyHint": True, "destructiveHint": False, "idempotentHint": True, "openWorldHint": False},
)
async def document_resolve(params: ResolveInput) -> str:
    """List indexed documents, optionally under a folder prefix.

    Use this to discover what exists before searching, and to hand a document path to the
    host's document_open, which stages the file into the caller's sandbox.
    """
    where = f" WHERE document LIKE '%{_quote(params.prefix)}%'" if params.prefix else ""
    rows = _sql(f"SELECT document AS d, count(*) AS n FROM Passage{where} GROUP BY document")
    return json.dumps(
        {"prefix": params.prefix or "", "documents": [{"path": r["d"], "passages": r["n"]} for r in rows]},
        ensure_ascii=False,
    )


if __name__ == "__main__":
    mcp.run(transport=os.environ.get("MCP_TRANSPORT", "streamable-http"))
