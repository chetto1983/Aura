# Aura markitdown sidecar (9c documents leg, UX-04). A thin HTTP wrapper over the
# OFFICIAL microsoft/markitdown library — the lib ships no HTTP server (lib + CLI +
# MCP only), so Aura vendors this ~30-line FastAPI app instead of depending on an
# unpinned community image (supply-chain discipline, cf. the 13-01 deps gate).
#
# Contract consumed by internal/channels/telegram/documents.go:
#   POST /convert   multipart/form-data, file field "file"  ->  {"markdown": "..."}
#   GET  /health    ->  {"ok": true}
import os
import tempfile

from fastapi import FastAPI, File, HTTPException, UploadFile
from markitdown import MarkItDown

app = FastAPI(title="aura-markitdown", version="1")
_md = MarkItDown()


@app.get("/health")
def health() -> dict:
    return {"ok": True}


@app.post("/convert")
async def convert(file: UploadFile = File(...)) -> dict:
    data = await file.read()
    # markitdown detects the format by file extension, so round-trip through a temp
    # file carrying the original suffix (a bare stream loses the type hint).
    suffix = os.path.splitext(file.filename or "")[1] or ".bin"
    fd, path = tempfile.mkstemp(suffix=suffix)
    try:
        with os.fdopen(fd, "wb") as fh:
            fh.write(data)
        result = _md.convert(path)
        return {"markdown": result.text_content}
    except Exception:
        # Do not leak the converter's internals to the client (a parse error can echo
        # file content); documents.go maps any non-2xx to a sidecarStatusError.
        raise HTTPException(status_code=422, detail="conversion failed")
    finally:
        os.unlink(path)
