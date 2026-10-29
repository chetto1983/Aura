---
name: pdfcraft-aura
description: "Work on PDF files with the pdfcraft MCP server in your box — real redaction that removes the text under it and an audit that proves it, PDF to Word/PowerPoint/HTML export, text extraction and search, page render, split and combine, forms, signatures, PDF/A. Use it when the operator wants to black out names or data in a PDF, turn a PDF into an editable Word file, find something in a long PDF, split or merge PDFs. Italian triggers: «oscura», «anonimizza», «cancella i nomi dal PDF», «trasforma il PDF in Word», «dividi il PDF», «unisci i PDF». Load it BEFORE the first pdfcraft call."
---

# PdfCraft in Aura

The server runs compact: 17 core tools as `pdfcraft__<tool>`, and every other one of its ~170 tools through
`pdfcraft__tool_search {query|category|name}` then `pdfcraft__tool_call {name, arguments}`.
Files: `/workspace` only — paths outside are refused.

## Recipes

**Open and read.** `pdfcraft__doc_open {path}` returns `doc` (an id). `pdfcraft__text_extract {doc, pages?}`,
`pdfcraft__text_find {doc, query}` (every match with page and rectangle), `pdfcraft__render_preview {doc, page}`
to look at a page.

**Redact for good** (measured: 10 of 10 marks applied, 0 left in the text, the rest untouched):
1. `tool_call redact_mark {doc, find: "<text>"}` — or `words: [...]`, or `pattern: email|phone|date|...`.
2. `tool_call redact_apply {doc}` — removes text, images and vectors under the marks.
3. `tool_call doc_audit_redactions {doc}` — must answer `count: 0`; report it.
4. `pdfcraft__doc_save {doc, path: "/workspace/...-oscurato.pdf"}`, then `send_file`.
Never fake a redaction by drawing a box: the audit finds it.

**PDF to Word.** `tool_call doc_export_office {doc, path: "...docx"}` (also .pptx, .html, .rtf). Measured on a
42-page legal note: headings, tables and text kept in 0.13 s, where LibreOffice produced an empty file.

## Limits (measured 2026-10-18, PdfCraft 0.9.0, CLI package; the redaction re-run 2026-10-29 on 0.15.0)

- No OCR: the CLI package has no recognition models, and they read English only. You can read a scanned
  page yourself: render it with `render_preview` and look at the image.
- Save to a new path; the original stays as it was.
