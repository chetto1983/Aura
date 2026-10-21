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

## Source-size measurement, 2026-10-22

The installed helper was also run on synthetic Python source after warming its grammar:

| Source bytes | Lines | Highlighted HTML bytes | Elapsed | Heap change |
|---|---|---|---|---|
| 65,536 | 1,338 | 239,525 | 112 ms | 4 MiB |
| 262,144 | 5,351 | 957,753 | 182 ms | 21 MiB |
| 1,225,037 | 25,002 | 4,475,241 | 784 ms | 43 MiB |

Command: `node .superpowers/sdd/2026-10-21-paperless-documents/measure-source-preview.mjs`.
The fixture repeats a simple assignment and is not a worst-case grammar benchmark. Times are
one local Node run, excluding browser DOM layout, and heap changes are not peak memory. The
measurement demonstrates that source expansion and synchronous tokenization grow enough to
justify bounding preview reads and highlighting. Download must still provide the original file.

The running pre-rebuild file-manager preview was also exercised with the newly indexed
synthetic sources. Python displayed all 308 bytes and 35 styled syntax spans. Rust opened the
shared preview dialog but showed only its download fallback. This establishes the actual
routing gap in addition to the grammar-selection probe; both need to be addressed.
