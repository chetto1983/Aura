---
name: wordcraft-aura
description: "Edit Word documents (.docx, .doc, .odt, .rtf, .md) with the wordcraft MCP server in your box — find and replace text without losing its formatting, run any of its commands (styles, tables, lists, track changes, comments), save as docx, pdf, odt, rtf, html or md, and render a page to check it. Use it to fill a template, correct a contract or a letter, compile a bando or a verbale, convert a document to PDF. Italian triggers: «compila il modulo/bando», «modifica il contratto», «sostituisci nel documento», «salva in PDF», «converti il Word». Load it BEFORE the first wordcraft call."
---

# WordCraft in Aura

Your tools are `wordcraft__<tool>`. Files: `/workspace` only.

## The round trip

1. `wordcraft__open_document {path}` — returns paragraphs and words.
2. `wordcraft__get_text` to read; `wordcraft__inspect_document` for styles, tables and sections.
3. Replace text: `wordcraft__select_text {text, occurrence?}` with the EXACT text (copy it from `get_text`),
   then `wordcraft__type_text {text}`. `select_text` takes the FIRST match, and a template repeats its blanks
   (`n. ......`, `del ……`) in many places: select the whole blank together with the words just before it
   (`contratto n. ...... del ……`), so it matches only the place you mean (measured: a bare `n. ......`
   filled an attachment number 40 paragraphs before the one asked). Then find that sentence again in
   `get_text` and check it reads as it should. The new text takes the formatting where it lands; bold and italic
   elsewhere are kept (measured on a 821-paragraph bando: 0 italic characters lost, where python-docx lost
   108 in the one paragraph it touched).
4. Anything else is a command: `wordcraft__list_commands {query}`, then `wordcraft__execute {command, params}`.
   Several documents open: `wordcraft__list_documents` gives each one's `id`; pass it as `doc` to the other
   tools to work on one in the background.
5. `wordcraft__render_page {page}` and look at the image before you deliver.
6. `wordcraft__save_document {path}` — the extension picks the format: save both `.docx` and `.pdf` when the
   operator will send it on. New paths only; then `send_file`.

## Limits (measured 2026-10-18, WordCraft 0.8.0; the round trip re-run 2026-10-29 on 0.12.0)

- `screenshot`, `click`, `key` and `ui_inspect` need a desktop app and fail here.
- The box lacks some Microsoft fonts: page breaks can differ from Word by a page (measured 51 against
  Word's 50, where LibreOffice gave 66).
