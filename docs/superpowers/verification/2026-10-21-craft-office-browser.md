# Craft Office browser import checks

Date: 2026-10-21. Running Aura GHCR revision: `9ef3a2e73128a4dba6016c719a7bcc931e04452d`.
These checks establish installed app capabilities before replacing the document preview dispatch.
They do not establish that the new Documents workspace has shipped.

Each app ran in a fresh same-origin iframe at `/apps/<app>/?host=parent`. After its existing
`<app>:ready` message, the probe sent the filename and bytes through `<app>:open`. The probe
captured bridge messages and screenshots and did not save anything to the user's object store.
All listed files emitted ready and dirty=false, with no failed message; every screenshot was
opened and visually inspected for the expected source content.

| File | App | Visible result |
|---|---|---|
| Public ANAC DOCX | WordCraft | Formatted cover, emblem and document text; 64 pages reported |
| Synthetic Word 97 DOC | WordCraft | Heading and two expected paragraphs, one page |
| Synthetic ODT | WordCraft | Heading and paragraphs, with visible paragraph-spacing differences |
| Synthetic RTF | WordCraft | Heading and two expected paragraphs, one page |
| Public FCFF XLS | GridCraft | Populated workbook, multiple sheets, partial-import banner |
| Synthetic ODS | GridCraft | Expected cells and explicit data-only-import dialog |
| Synthetic CSV | GridCraft | Expected three-column table and two data rows |
| Public EU-OSHA PPTX | DeckCraft | First slide with illustrations/text and slide thumbnails; 15 slides |

The XLS banner states that values, formulas and some layout metadata import, while fonts, fills,
borders, charts, pictures and other features do not. The imported workbook uses an XLSX name.
The ODS dialog states that formulas become saved values, formatting and other features do not
import, and saving requires a new XLSX file. These are the installed app's own notices and must
remain visible; opening a format is not a lossless round-trip promise.

Synthetic fixtures were generated in the ignored report fixture directory. A minimal RTF and CSV
were authored locally; the existing ingestion image's LibreOffice 7.4.7.2 converted copies to real
DOC, ODT and ODS fixtures. This was fixture generation only: no extraction code or production
preview path changed. Its unique temporary directory was removed afterward. Script:
`.superpowers/sdd/2026-10-21-paperless-documents/create-office-fixtures.py`.

Screenshots are under `output/playwright/paperless-comparison/`: `craft-word-browser-docx.png`,
`craft-word-browser-doc.png`, `craft-wordcraft-browser-odt.png`, `craft-wordcraft-browser-rtf.png`,
`craft-grid-browser-xls.png`, `craft-gridcraft-browser-ods.png`, `craft-gridcraft-browser-csv.png`,
and `craft-deck-browser-pptx.png`.

No console errors were reported. Existing browser/native warnings concerned ignored preload
integrity and GPU power preference, plus egui web's unimplemented SetTheme/Title commands.
WordCraft reported that the Italian Hunspell dictionary was not installed. The files still
rendered. WordCraft used Italian; GridCraft and DeckCraft used English, consistent with the
existing Craft language inventory. This check does not add translations to those apps.

Not verified here: complex legacy-format fidelity, macros, encrypted files, XLSB, Office
templates, saving through Aura's object-store bridge, mobile editing, or unsupported PPT import.
The installed DeckCraft inventory explicitly excludes old binary PPT. The new workspace should
list and download unsupported formats without claiming an available editor.
