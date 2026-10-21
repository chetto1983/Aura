# Code preview inventory

Date: 2026-10-21. Trigger: the user asked to include code files and rendering.

Aura already has `TextPreview -> HighlightedSource -> highlightCode` using its lazy Shiki core,
theme hooks, escaped output and plain-source fallback. `HtmlPreview` already provides Source and
Rendered tabs, with an opaque-origin iframe. `CodeBlock` and the existing clipboard helpers are
available; no new syntax-highlighting engine, source editor or execution service is needed.

The current general artifact suffix list recognizes only a small subset of the server's source
registry. Document rows now have `file_type=code`, so document preview routing should use that
classification instead of falling back to download for recognized source formats.

A direct run against the installed helper produced:

| Suffix | Current output |
|---|---|
| py, ts, go, html, json | Highlighted escaped spans |
| rs, cpp, cs, java, xml, iecst | Plain-text fallback |

Command: `node .superpowers/sdd/2026-10-21-paperless-documents/probe-source-highlighting.mjs`.
It exited zero. This proves the current helper's grammar selection, not full document routing.

Installed Shiki 4.5.0 has more grammars. Its [bundle documentation](https://shiki.style/guide/bundles)
and installed exports show lazy loading and fine-grained grammar imports. Extend Aura's existing
fine-grained loader for common missing languages; do not introduce a second highlighter or eagerly
load every grammar. Plain escaped text remains useful for unsupported domain languages.

`useAssetContent(..., 'text')` currently reads the whole body and `HighlightedSource` passes it all
to Shiki. Code preview must bound reading/tokenization, identify truncation, and provide the original
download. A source-looking filename can contain binary bytes (the shared registry explicitly notes
SQLite versus Siemens `.db`); classification alone cannot establish that it is readable text.

Existing HTML behavior is a reusable supported renderer, not an instruction to execute arbitrary
programming languages. The new Code filter must preserve source escaping and the current iframe
sandbox boundary. No ingestion/extraction changes were made by this inventory.
