"""Fail-soft client for Aura's optional GPU rerank sidecar (aura-rerank /v1/rerank).

Mirrors ``internal/rerank.RerankClient`` (Go), stdlib only (urllib, no httpx/openai
SDK): on ANY failure mode -- no base_url, transport error, non-2xx, decode error,
result/doc length mismatch, or an out-of-range index -- it returns the input index
order unchanged (identity, which is the upstream embedding/RRF order) and logs ONCE
per process. Document text is truncated on the wire and never logged.
"""

from __future__ import annotations

import json
import logging
import os
import urllib.error
import urllib.request

logger = logging.getLogger(__name__)

# Mirror Go maxRerankDocChars: cap each document (and the query) on the wire so a
# query/doc pair stays inside the sidecar's context window. Character slicing matches
# the spike-070 ``str[:480]``.
_MAX_DOC_CHARS = 480
_RERANK_TIMEOUT = 30.0
_RERANK_ENV = "AURA_RERANK_BASE_URL"
_RERANK_MODEL = "aura-rerank"

_warned = False


def rerank_base_url() -> str:
    """Return the configured rerank base URL (env AURA_RERANK_BASE_URL), or ''."""
    return os.environ.get(_RERANK_ENV, "").strip()


def rerank(query: str, docs: list[str], base_url: str | None = None) -> list[int]:
    """Return docs' indices in descending-relevance order via ``{base_url}/v1/rerank``.

    FAIL-SOFT: every failure mode returns ``list(range(len(docs)))`` (the input order,
    which is the upstream embedding/RRF order) and logs once per process. Never raises.
    When ``base_url`` is None it is read from ``AURA_RERANK_BASE_URL``.
    """
    count = len(docs)
    identity = list(range(count))
    if count == 0:
        return identity
    resolved = (base_url if base_url is not None else rerank_base_url()).strip()
    if not resolved:
        return _degrade(identity, "base URL empty (rerank disabled)")

    payload = json.dumps(
        {
            "model": _RERANK_MODEL,
            "query": query[:_MAX_DOC_CHARS],
            "documents": [_truncate(doc) for doc in docs],
        }
    ).encode("utf-8")
    request = urllib.request.Request(
        resolved.rstrip("/") + "/v1/rerank",
        data=payload,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=_RERANK_TIMEOUT) as response:
            if response.status // 100 != 2:
                return _degrade(identity, "sidecar non-2xx status")
            raw = response.read()
    except urllib.error.HTTPError:
        return _degrade(identity, "sidecar non-2xx status")
    except (urllib.error.URLError, OSError, TimeoutError):
        return _degrade(identity, "sidecar transport error")

    try:
        results = json.loads(raw)["results"]
    except (ValueError, KeyError, TypeError):
        return _degrade(identity, "sidecar decode error")
    if not isinstance(results, list) or len(results) != count:
        return _degrade(identity, "result/doc length mismatch")

    scored: list[tuple[int, float]] = []
    for item in results:
        try:
            index = int(item["index"])
            score = float(item["relevance_score"])
        except (KeyError, TypeError, ValueError):
            return _degrade(identity, "sidecar decode error")
        if index < 0 or index >= count:
            return _degrade(identity, "out-of-range result index")
        scored.append((index, score))
    scored.sort(key=lambda pair: pair[1], reverse=True)
    return [index for index, _ in scored]


def _truncate(doc: str) -> str:
    """Cap a document for the wire body only; empty docs become 'n/a' (spike-070)."""
    doc = doc if doc.strip() else "n/a"
    return doc[:_MAX_DOC_CHARS]


def _degrade(identity: list[int], reason: str) -> list[int]:
    """Log the fail-soft reason once per process (no doc text/secrets); return identity."""
    global _warned
    if not _warned:
        _warned = True
        logger.warning(
            "rerank: degraded to identity order (further occurrences suppressed): %s",
            reason,
        )
    return identity
