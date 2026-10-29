---
name: gridcraft-aura
description: "Open, recalculate, edit and save Excel workbooks (.xlsx, .xls, .xlsm, .xlsb, .ods, .csv) with the gridcraft MCP server in your box — a real formula engine, 513 Excel functions, cached values written on save. Use it for any spreadsheet task: change an input and read the new result, fill or fix cells, format, sort, filter, add a table or a chart, convert .xls to .xlsx. Italian triggers: «foglio Excel», «cambia la cella», «ricalcola», «aggiorna il budget», «salva in xlsx», «converti l'xls». Load it BEFORE the first gridcraft call."
---

# GridCraft in Aura

Your tools are `gridcraft__<tool>`, deferred: `tool_search` with "gridcraft" lists them.

## The round trip

1. A file attached to the conversation or in the library: `document_open {document_id}` with the id the
   attachment block or `document_search` gives (an attachment's asset id works at once) copies it to
   `/workspace/documents/<id>/<file>`. Work on THAT copy, not on a file of the same name you find elsewhere
   in `/workspace`. A file you made is already in `/workspace`.
2. `gridcraft__open_workbook {path}` — absolute path. It opens .xls too (measured: a 16-sheet, 906-formula
   valuation model).
3. FIND cells by their label, never by counting rows. The formula engine tells you the row exactly:
   `gridcraft__evaluate_formula {formula: "=MATCH(\"*tax*rate*\", 'Inputs'!A1:A300, 0)"}` answers the row of
   the first label in column A that matches (case-insensitive; `*` between words also matches "taxrate" or
   "tax-rate"), and `=INDEX('Inputs'!B1:B300, MATCH(\"*tax*rate*\", 'Inputs'!A1:A300, 0))` the value beside it.
   Which sheet and column hold a label: `gridcraft__execute_command {command: "edit.find", params: {what:
   "tax", all: true, within: "workbook"}}` — ONE short word, since it matches the text as written.
   `read_range` answers rows WITHOUT row numbers: counting rows in a big window wrote the neighbouring
   input, three times out of four on a 16-sheet model, and nothing warned.
   - Before you write, `gridcraft__get_cell {cell}` must show the value you mean to change; if it does not,
     you have the wrong cell — find it again, do not write.
4. Change: `gridcraft__set_cell {cell, input}` types exactly as a user would (a number, text, or `=FORMULA`).
   A cell on another sheet is quoted: `'Cost of capital'!B12`. Several cells:
   `gridcraft__write_range {range, values}`.
5. Every dependent formula recalculates at once. Find the output by its label the same way, read it
   before and after, and report both numbers. Its value is not always in the next column: read the whole
   row (`read_range {range: "'Valuation Model'!A71:J71"}`) BEFORE you change the input (measured: "Value of
   equity per share" in B71, its value in F71). Measured: the result matched LibreOffice's recalculation to
   the last digit.
6. `gridcraft__save_workbook {path}` — always a NEW path under `/workspace` (`<name>-modificato.xlsx`),
   never over the original. The saved file carries the cached values, so any reader sees the new numbers.
7. `send_file` delivers it; that copy also lands in the library.

`gridcraft__evaluate_formula {formula}` answers "what would X be" without touching the sheet.
`gridcraft__list_windows` lists the open workbooks (its `doc` id, title, unsaved changes); pass `doc` to the
other tools to work on one that is not active.

To LOOK at a sheet (its layout, a chart): select the sheet (`gridcraft__select {range: "'Sheet'!A1"}`), then
`gridcraft__execute_command {command: "file.exportPdf", params: {path: "/workspace/.../view.pdf",
range: "A1:H40", fitToPage: true}}`, then `pdfcraft__doc_open` and `pdfcraft__render_preview` give you the
image. The image comes back as a file under `/workspace/mcp-files/` (its path is in the result's `files`): `read_file` that path to see it. `##########` there is a number wider than its column, as in Excel: read the value with `read_range`.

## Limits (measured 2026-10-18, GridCraft 0.8.0; the round trip re-run 2026-10-29 on 0.13.0)

- The server writes only under `/workspace`: anywhere else it gets "Permission denied" (Aura's landrun
  fence; GridCraft has no root flags of its own).
- `screenshot` needs a desktop app (it is not offered); look through `file.exportPdf` as above.
- Error constants (`#DIV/0!`) are kept as values, not as empty formulas.
- An .xls keeps its values, formulas, number formats, merges, widths and names; its fonts, fills,
  borders, charts, pictures, comments and conditional formatting are not imported (the open warns). Say so
  when you send the .xlsx back.
- Alpha software: after a save, re-open the file and read the cells you changed before you report.
