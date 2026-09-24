"""A stand-in embedding server for the re-run test: deterministic, countable, and able to
refuse one input in one space.

A vector depends on the space as well as on the input, so a row whose stamp and vector
disagree fails the test's comparison.
"""

import hashlib
import io
import json
import math
import os
import urllib.error
import urllib.request

REFUSED = "REFUSED-IN-THIS-SPACE"

_requests = 0
_real_urlopen = urllib.request.urlopen


def vector(space: str, sent: str, dimensions: int) -> list[float]:
    digest = hashlib.sha256(f"{space}\x00{sent}".encode("utf-8")).digest()
    raw = [float(digest[i % len(digest)]) + 1.0 for i in range(dimensions)]
    norm = math.sqrt(sum(value * value for value in raw))
    return [value / norm for value in raw]


def requests() -> int:
    return _requests


def install() -> None:
    """Answer /v1/embeddings in this process; the tokenizer still goes to the real sidecar."""
    from ingest import embed

    def urlopen(req, *args, **kwargs):
        global _requests
        if not req.full_url.endswith("/v1/embeddings"):
            return _real_urlopen(req, *args, **kwargs)
        _requests += 1
        inputs = json.loads(req.data)["input"]
        if os.environ.get("SPACE_THAT_REFUSES") == embed.SPACE and any(REFUSED in text for text in inputs):
            raise urllib.error.HTTPError(req.full_url, 400, "Bad Request", None, io.BytesIO(b"refused"))
        data = [{"index": i, "embedding": vector(embed.SPACE, text, embed.DIMENSIONS)}
                for i, text in enumerate(inputs)]
        return io.BytesIO(json.dumps({"data": data}).encode())

    urllib.request.urlopen = urlopen
