---
name: deckcraft-aura
description: "Build and edit PowerPoint presentations (.pptx) with the deckcraft MCP server in your box — change titles and text, add slides, shapes, pictures, tables and charts, render a slide to look at it, export to PDF or PNG, save as pptx. Use it for any slide deck task: update a presentation, make slides from data, turn a deck into a PDF. Italian triggers: «presentazione», «slide», «cambia il titolo della slide», «aggiungi una slide», «esporta in PDF». Load it BEFORE the first deckcraft call."
---

# DeckCraft in Aura

Your tools are `deckcraft__<tool>`. Files: `/workspace` only.

## The round trip

1. `deckcraft__open_presentation {path}` — reports slides, missing fonts and damaged media.
2. `deckcraft__inspect_document` (slides, layouts), `deckcraft__inspect_slide {index}` (every shape with its
   `id`, kind, box and text).
3. Change text: `deckcraft__go_to_slide {index}`, then `deckcraft__set_text {id, text}` with the shape id from
   `inspect_slide` (it keeps the first run's formatting).
4. Add: `add_slide {layout, title, body}` inserts AFTER THE CURRENT slide, which is the first after an open:
   for a slide at the end, `deckcraft__go_to_slide {index: <slides - 1>}` first (measured: without it the
   new "last" slide landed second). `layout` is a kind (`title`, `titleAndContent`, `sectionHeader`,
   `twoContent`, `comparison`, `titleOnly`, `blank`, `contentWithCaption`, `pictureWithCaption`) or a layout
   name from `inspect_document`; a deck whose only layout has no placeholders (one named `DEFAULT`) takes
   no kind, and its new slide drops `title` and `body` without an error: `inspect_slide` shows 0 shapes.
   Then put the text in with `add_text_box {x, y, w, text, size}` (points, as the boxes `inspect_slide`
   reports). `add_text_box`, `add_shape`, `insert_picture {path}`,
   `insert_table {rows, cols, data}`, `insert_chart {type, categories, series}`. Anything else:
   `list_commands {filter}` and `run_command {command, params}`.
5. `deckcraft__render_slide {slide}` and look at it; `inspect_document` confirms the slide count and order.
6. `deckcraft__export {path: "...pdf", format: "pdf"}` and/or `deckcraft__save_presentation {path}`; then
   `send_file`. Measured on a 15-slide, 23-picture deck: everything kept, PDF in 1.1 s.

## Limits (measured 2026-10-18, DeckCraft 0.8.0; the round trip re-run 2026-10-29 on 0.12.0)

- `open_presentation` lists fonts the box lacks (Arial, Wingdings): text draws in a metric-compatible
  stand-in (Liberation for Arial), so the layout holds; symbol fonts may show different glyphs.
- `pointer`, `key`, `select_tool`, `screenshot` and `call_app` need a desktop app and fail here.
