# Aura document sidecar. It keeps /convert compatible with the original
# markitdown wrapper and adds /extract for structured document ingestion.
import os
import re
import shutil
import tempfile
from typing import Any

from fastapi import FastAPI, File, Form, HTTPException, UploadFile
from markitdown import MarkItDown

app = FastAPI(title="aura-markitdown", version="2")
_md = MarkItDown()
_ws_re = re.compile(r"\s+")
_max_chunk_chars = 12000


@app.get("/health")
def health() -> dict:
    return {"ok": True}


@app.post("/convert")
async def convert(file: UploadFile = File(...)) -> dict:
    path = _save_upload(file)
    try:
        result = _md.convert(path)
        return {"markdown": result.text_content}
    except Exception:
        raise HTTPException(status_code=422, detail="conversion failed")
    finally:
        _unlink(path)


@app.post("/extract")
async def extract(file: UploadFile = File(...), mime_type: str = Form("")) -> dict:
    path = _save_upload(file)
    suffix = os.path.splitext(file.filename or "")[1].lower()
    effective_mime = mime_type or file.content_type or ""
    try:
        if suffix == ".pdf" or effective_mime == "application/pdf":
            payload = _extract_pdf(path)
        elif suffix in (".xlsx", ".xlsm") or "spreadsheet" in effective_mime:
            payload = _extract_xlsx(path)
        elif suffix == ".docx" or effective_mime.endswith("wordprocessingml.document"):
            payload = _extract_docx(path)
        else:
            payload = _extract_markdown(path)
        payload["mime_type"] = effective_mime or payload.get("mime_type", "")
        return payload
    except HTTPException:
        raise
    except Exception:
        raise HTTPException(status_code=422, detail="extraction failed")
    finally:
        _unlink(path)


def _save_upload(file: UploadFile) -> str:
    suffix = os.path.splitext(file.filename or "")[1] or ".bin"
    fd, path = tempfile.mkstemp(suffix=suffix)
    try:
        with os.fdopen(fd, "wb") as fh:
            file.file.seek(0)
            shutil.copyfileobj(file.file, fh)
        return path
    except Exception:
        _unlink(path)
        raise


def _unlink(path: str) -> None:
    try:
        os.unlink(path)
    except FileNotFoundError:
        pass


def _normalize(text: str) -> str:
    return _ws_re.sub(" ", text or "").strip()


def _chunk_text(kind: str, text: str, locator: dict[str, Any], heading_path: list[str] | None = None) -> list[dict]:
    clean = _normalize(text)
    if not clean:
        return []
    heading_path = heading_path or []
    chunks = []
    while len(clean) > _max_chunk_chars:
        split_at = clean.rfind(" ", 0, _max_chunk_chars)
        if split_at < _max_chunk_chars // 2:
            split_at = _max_chunk_chars
        chunks.append(
            {
                "kind": kind,
                "text": clean[:split_at].strip(),
                "locator": locator,
                "heading_path": heading_path,
            }
        )
        clean = clean[split_at:].strip()
    if clean:
        chunks.append(
            {
                "kind": kind,
                "text": clean,
                "locator": locator,
                "heading_path": heading_path,
            }
        )
    return chunks


def _payload(title: str, mime_type: str, chunks: list[dict], pages: int = 0, sheets: int = 0, sections: int = 0) -> dict:
    return {
        "title": title,
        "mime_type": mime_type,
        "stats": {
            "pages": pages,
            "sheets": sheets,
            "sections": sections,
            "chunks": len(chunks),
            "characters": sum(len(chunk.get("text", "")) for chunk in chunks),
        },
        "chunks": chunks,
    }


def _extract_pdf(path: str) -> dict:
    try:
        import fitz
    except Exception:
        raise HTTPException(status_code=503, detail="pdf extractor unavailable")

    doc = fitz.open(path)
    try:
        title = (doc.metadata or {}).get("title") or os.path.basename(path)
        chunks: list[dict] = []
        for index, page in enumerate(doc, start=1):
            text = page.get_text("text") or ""
            chunks.extend(_chunk_text("page", text, {"page": index}))
        return _payload(title, "application/pdf", chunks, pages=doc.page_count)
    finally:
        doc.close()


def _extract_xlsx(path: str) -> dict:
    try:
        from openpyxl import load_workbook
    except Exception:
        raise HTTPException(status_code=503, detail="xlsx extractor unavailable")

    wb = load_workbook(path, read_only=True, data_only=False)
    try:
        chunks: list[dict] = []
        for ws in wb.worksheets:
            lines: list[str] = []
            row_start = 0
            row_end = 0
            for row_index, row in enumerate(ws.iter_rows(), start=1):
                cells = []
                for cell in row:
                    value = cell.value
                    if value is None:
                        continue
                    text = str(value).strip()
                    if not text:
                        continue
                    cells.append(f"{cell.coordinate}: {text}")
                if not cells:
                    continue
                if row_start == 0:
                    row_start = row_index
                row_end = row_index
                lines.append(f"row {row_index}: " + " | ".join(cells))
                joined = "\n".join(lines)
                if len(joined) >= _max_chunk_chars or len(lines) >= 50:
                    chunks.extend(
                        _chunk_text(
                            "rows",
                            joined,
                            {"sheet": ws.title, "row_start": row_start, "row_end": row_end},
                            [ws.title],
                        )
                    )
                    lines = []
                    row_start = 0
                    row_end = 0
            if lines:
                chunks.extend(
                    _chunk_text(
                        "rows",
                        "\n".join(lines),
                        {"sheet": ws.title, "row_start": row_start, "row_end": row_end},
                        [ws.title],
                    )
                )
        return _payload(os.path.basename(path), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", chunks, sheets=len(wb.worksheets))
    finally:
        wb.close()


def _extract_docx(path: str) -> dict:
    try:
        from docx import Document
    except Exception:
        raise HTTPException(status_code=503, detail="docx extractor unavailable")

    doc = Document(path)
    chunks: list[dict] = []
    heading_path: list[str] = []
    buffer: list[str] = []
    start_para = 0
    sections = 0

    def flush(end_para: int) -> None:
        nonlocal buffer, start_para
        if not buffer:
            return
        section = " > ".join(heading_path) if heading_path else "body"
        chunks.extend(
            _chunk_text(
                "section",
                "\n".join(buffer),
                {"section": section, "paragraph": start_para},
                heading_path[:],
            )
        )
        buffer = []
        start_para = end_para + 1

    for index, paragraph in enumerate(doc.paragraphs, start=1):
        text = _normalize(paragraph.text)
        if not text:
            continue
        style_name = (paragraph.style.name if paragraph.style else "").lower()
        if style_name.startswith("heading"):
            flush(index - 1)
            level = _heading_level(style_name)
            heading_path[:] = heading_path[: level - 1]
            heading_path.append(text)
            sections += 1
            start_para = index + 1
            continue
        if not buffer:
            start_para = index
        buffer.append(text)
    flush(len(doc.paragraphs))
    if sections == 0 and chunks:
        sections = 1
    return _payload(os.path.basename(path), "application/vnd.openxmlformats-officedocument.wordprocessingml.document", chunks, sections=sections)


def _heading_level(style_name: str) -> int:
    match = re.search(r"(\d+)", style_name)
    if not match:
        return 1
    return max(1, min(6, int(match.group(1))))


def _extract_markdown(path: str) -> dict:
    result = _md.convert(path)
    chunks = _chunk_text("markdown", result.text_content, {})
    return _payload(os.path.basename(path), "", chunks)
