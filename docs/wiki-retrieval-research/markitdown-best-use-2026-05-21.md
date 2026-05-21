# Markitdown — best-use research for Aura's source pipeline (2026-05-21)

Aura ingests user-uploaded files (xlsx, docx, pptx, epub, html, zip + 6 others) through a Docker sidecar (`aura-markitdown`) that exposes Microsoft's `markitdown-mcp`. Today the contract is one-shot: bytes in → single `extract.md` blob out, then an LLM-driven ingest writes a SUMMARY wiki page. The 2026-05-21 ruocci spreadsheet showed the pathology: a 180-row table gets summarized to entity classes (Ruocci Marco, Quadristi…) and the row-level facts (customer codes, emails) are dropped. The agent then has to re-read raw `extract.md` to answer "Delta Automazioni 615827".

This doc maps what markitdown actually offers today, what Aura's wrapper exposes, where the gaps are, and the highest-leverage lifts.

## Markitdown API surface today

Three layers stacked on top of each other:

1. **Core library** `MarkItDown` in `D:/tmp/markitdown/packages/markitdown/src/markitdown/_markitdown.py:93-300` — single class, multiple entrypoints (`convert`, `convert_local`, `convert_stream`, `convert_uri`, `convert_response`). All paths funnel into `_convert(file_stream, stream_info_guesses, **kwargs)` (line 538), which picks a converter from a priority-sorted registry and returns ONE `DocumentConverterResult`.
2. **Result type** `_base_converter.py:5-39` — fixed shape:
   ```python
   class DocumentConverterResult:
       def __init__(self, markdown: str, *, title: Optional[str] = None):
           self.markdown = markdown
           self.title = title
   ```
   That is literally the surface: `markdown` plus optional `title`. **No structured tree, no JSON, no per-section dict, no metadata bag.** Every converter must serialize whatever structure it found back into a flat Markdown string. Aura's pain (losing row-level facts) is the inevitable downstream consequence of this design.
3. **MCP wrapper** `markitdown-mcp/src/markitdown_mcp/__main__.py:20-23` — exposes exactly one tool:
   ```python
   @mcp.tool()
   async def convert_to_markdown(uri: str) -> str:
       """Convert a resource described by an http:, https:, file: or data: URI to markdown"""
       return MarkItDown(enable_plugins=check_plugins_enabled()).convert_uri(uri).markdown
   ```
   - Only `convert_to_markdown(uri)`. Drops `title`. No options forwarded. No streaming.
   - `MARKITDOWN_ENABLE_PLUGINS=true` env-var toggles 3rd-party converters at MCP startup (`__main__.py:26-31`).

Per-call options exist in the Python API (style_map, llm_client, llm_model, llm_prompt, exiftool_path, docintel_endpoint — see `_markitdown.py:148-152, 207-226`) and they DO flow into converters via `**kwargs`, but **none of them are surfaced through the MCP transport**. From Aura's vantage, the sidecar is a single black-box function.

## Per-format converters

All converters in `D:/tmp/markitdown/packages/markitdown/src/markitdown/converters/`. They share the `accepts/convert` shape from `_base_converter.py:42-105` but each has its own quirks.

| Converter | File | Tunable kwargs | Structure preserved | Notes |
|-----------|------|-----------------|---------------------|-------|
| `XlsxConverter` | `_xlsx_converter.py:36-95` | none (passes `**kwargs` to inner HTML converter — but xlsx doesn't read any) | one `## <sheet>` heading + one HTML→MD table per sheet | uses `pandas.read_excel(sheet_name=None)`, then `df.to_html(index=False)` (line 87). **Cell formulas are dropped — pandas resolves to values.** No row-grouping, no per-row formatting, no metadata about the workbook. |
| `XlsConverter` | `_xlsx_converter.py:98-157` | none | same as xlsx | xlrd backend |
| `DocxConverter` | `_docx_converter.py:31-95` | `style_map` (kwargs.get at line 78) | headings, lists, tables (via `mammoth`) | runs `pre_process_docx` first (deduped numbering), then mammoth → HTML → markdownify |
| `PptxConverter` | `_pptx_converter.py:34-` | `llm_client`, `llm_model`, `llm_prompt` for image captions | `<!-- Slide number: N -->` markers, per-slide flat text | calls `llm_caption()` per picture when llm_client provided (line 102-120) — this is the ONLY converter that talks to an LLM during conversion |
| `PdfConverter` | `_pdf_converter.py` | none (uses pdfminer) | minimal — flat text only | NB: Aura does NOT use this; Mistral OCR runs before markitdown for PDFs |
| `ImageConverter` | `_image_converter.py` | `llm_client`, `llm_model`, `exiftool_path` | EXIF block + optional LLM caption | |
| `AudioConverter` | `_audio_converter.py` | optional speech transcription deps | EXIF + transcript | |
| `HtmlConverter` | `_html_converter.py` | none | full structure via markdownify | inner converter that DOCX/XLSX delegate to |
| `EpubConverter` | `_epub_converter.py` | none | per-chapter sections | |
| `IpynbConverter` | `_ipynb_converter.py` | none | cells preserved | |
| `OutlookMsgConverter` | `_outlook_msg_converter.py` | none | headers + body | |
| `CsvConverter` | `_csv_converter.py` | none | markdown table | |
| `RssConverter`, `WikipediaConverter`, `YouTubeConverter`, `BingSerpConverter` | `_*_converter.py` | URL-shape-specific | provider-tailored | |
| `ZipConverter` | `_zip_converter.py:22-` | recurses into `_markitdown` registry | `## File: <path>` per entry, then each file's MD inline | this is the closest thing to "multi-output" in the codebase — but it still concatenates into ONE markdown string |
| `DocumentIntelligenceConverter` | `_doc_intel_converter.py` | `endpoint`, `credential`, `file_types`, `api_version` | Azure-side structure | registered only if `docintel_endpoint` provided |

**No converter produces multiple files. No converter returns a dict/JSON. No converter exposes a per-row or per-sheet callback.** The xlsx converter is the most relevant for Aura's pain and the most option-impoverished: it accepts zero kwargs.

Output structure: always single flat markdown. Section boundaries are conveyed by `##` headings (sheets/chapters), `<!-- Slide number: N -->` comments (pptx), or `## File: <path>` blocks (zip). There is no machine-readable side channel.

Metadata extraction: limited to `DocumentConverterResult.title` (often `None`). Sheet names appear as Markdown headings, not as structured fields. Author/created/modified workbook properties are NEVER extracted (openpyxl exposes them via `workbook.properties` but the converter doesn't touch it).

## Plugin system

`_markitdown.py:65-82, 232-250` + `markitdown-sample-plugin/src/markitdown_sample_plugin/_plugin.py`. Mechanism:

1. **Discovery**: Python entry-points group `"markitdown.plugin"` (see sample's `pyproject.toml`: `[project.entry-points."markitdown.plugin"]`). At `enable_plugins()` time, `_load_plugins()` (line 65-82) iterates `entry_points(group="markitdown.plugin")` and calls each module's `register_converters(markitdown, **kwargs)`.
2. **Registration**: the plugin module exposes `register_converters(markitdown, **kwargs)` (sample line 25-31):
   ```python
   def register_converters(markitdown, **kwargs):
       markitdown.register_converter(RtfConverter())
   ```
3. **Priority**: `register_converter(converter, *, priority=PRIORITY_SPECIFIC_FILE_FORMAT)` (`_markitdown.py:641-671`). Plugins can register BEFORE built-ins by using priority `< 0.0`, or after by using `> 10.0`. New registrations are inserted at index 0 — so a custom xlsx converter registered with priority 0.0 will be tried BEFORE the built-in XlsxConverter (stable sort + LIFO).
4. **Activation**: requires `enable_plugins=True` at construction OR `--use-plugins` on CLI. In the MCP container, env-var `MARKITDOWN_ENABLE_PLUGINS=true` flips the switch (`__main__.py:26-31`).
5. **Plugin interface version**: sample declares `__plugin_interface_version__ = 1`. Currently unenforced — the field is informational.

In Aura's setup the plugin would live as a small Python package inside the `aura-markitdown` Docker image, installed alongside `markitdown[all]`. The MCP server itself needs no patch — just `pip install <plugin>` + `MARKITDOWN_ENABLE_PLUGINS=true`.

## Aura's current wrapping — gaps

`D:/Aura/internal/storage/sources/markitdown/client.go` and `types.go`. The Go contract:

```go
type ConvertInput struct {
    Bytes    []byte
    MimeType string
    Filename string
}
type ConvertResult struct {
    Markdown string
    Warnings []string
}
type Converter interface {
    Convert(ctx context.Context, in ConvertInput) (ConvertResult, error)
}
```

Gaps mapped to markitdown features that EXIST but Aura doesn't surface:

| Markitdown feature | Surfaced today? | Notes |
|--------------------|-----------------|-------|
| `convert_to_markdown(uri)` single tool | ✓ (`client.go:103`) | calls with `data:<mime>;base64,...` URI |
| `MARKITDOWN_ENABLE_PLUGINS` env-var | partial — set at Docker level only, no Aura toggle | |
| `DocumentConverterResult.title` | ✗ — MCP server drops it (`__main__.py:23` returns `.markdown` not the result object) | |
| Per-converter kwargs (style_map, llm_client, llm_prompt) | ✗ — not in MCP signature | |
| `ConvertResult.Warnings` field | declared, never populated | placeholder |
| `Filename` hint passed to converter (extension nudge) | ✗ — Aura builds `data:` URI from mime alone (`client.go:98`); filename never travels | data URIs can't carry filename, but the MCP API takes generic URIs. Aura could use `file:` URIs into a tmpfs share to carry filename + extension hints |
| Plugin-provided custom converters | ✗ — no plugin installed in `aura-markitdown` image | this is the biggest open lever |
| Multi-output (one MD per sheet/slide/chapter) | ✗ at upstream too — would require either client-side post-split or a custom plugin that emits multi-section markdown with a known frontmatter delimiter |
| Workbook/document metadata (author, created, sheet count, modified) | ✗ — upstream doesn't extract it | trivial to add via a custom converter using `openpyxl.load_workbook(...).properties` |

The wrapper is intentionally minimal (3 small Go files, ~160 LOC). The CONTRACT is the bottleneck, not the wrapper code: `ConvertResult{Markdown, Warnings}` cannot represent structure. To exploit anything richer than "one blob", Aura needs (a) a richer upstream payload, and (b) a richer ConvertResult shape.

## Top 3 markitdown lifts for Aura

Prioritized by impact-per-LOC. All three are independent and can be shipped in any order.

### Lift 1 — Custom `xlsx` converter as a markitdown plugin (HIGH impact, ~150-250 LOC Python)

The 2026-05-21 ruocci failure is fully explained by `_xlsx_converter.py:83-93`: every sheet becomes one HTML table, and the ingest LLM is structurally incapable of preserving 180 rows in a summary. A custom converter can emit a far more retrieval-friendly shape WITHIN markitdown's flat-markdown contract:

```python
# packages/aura-markitdown-plugin/_aura_xlsx.py
def register_converters(md, **kwargs):
    md.register_converter(AuraXlsxConverter(), priority=-1.0)  # before built-in

class AuraXlsxConverter(DocumentConverter):
    def convert(self, file_stream, stream_info, **kwargs):
        wb = openpyxl.load_workbook(file_stream, data_only=True)
        out = []
        out.append(f"---\nworkbook_title: {wb.properties.title or ''}\n"
                   f"author: {wb.properties.creator or ''}\n"
                   f"sheets: {wb.sheetnames}\n---\n\n")
        for sn in wb.sheetnames:
            ws = wb[sn]
            header = [c.value for c in next(ws.iter_rows(max_row=1))]
            out.append(f"## Sheet: {sn} ({ws.max_row-1} rows)\n\n")
            for row in ws.iter_rows(min_row=2, values_only=True):
                # one-entity-per-paragraph format: heading + key:value list
                title = next((v for v in row if v), "(blank)")
                out.append(f"### {title}\n")
                for k, v in zip(header, row):
                    if v not in (None, "", title):
                        out.append(f"- **{k}**: {v}")
                out.append("")
        return DocumentConverterResult(markdown="\n".join(out), title=wb.properties.title)
```

Why this works for Aura specifically:
- One `###` per row = chunkable retrieval unit. The downstream ingest's "one wiki page per entity" pattern (or even just the chunker) now has natural seams.
- Headers become labels next to values → "Delta Automazioni 615827" survives as `**code**: 615827` on the `### Delta Automazioni` paragraph.
- YAML frontmatter surfaces sheet metadata that today is lost.
- Cost: one Python file + `Dockerfile` line `pip install /opt/aura-markitdown-plugin` + `MARKITDOWN_ENABLE_PLUGINS=true` (already toggleable). No core markitdown patch.

Risk: very large workbooks (10k+ rows) blow up the markdown. Mitigate with a row cap + "...(N more rows truncated)..." footer.

### Lift 2 — Per-sheet / per-slide multi-output via client-side splitter (MEDIUM impact, ~80 LOC Go)

Built-in xlsx and pptx use stable section delimiters (`## <sheet>` and `<!-- Slide number: N -->`). Aura already has the markdown; splitting it into `extract.sheet_<n>.md` files at write time is pure post-processing. Useful even without Lift 1 because it lets the ingest LLM see ONE sheet at a time (smaller context, less drift).

Where to wire it: `D:/Aura/internal/storage/sources/markitdown/client.go` after `Convert` returns, OR in the ingest layer where `extract.md` is consumed. A `splitter.go` next to `client.go` would keep ownership clean.

Risk: heading-name collisions across sheets, no formal schema. Lift 1 supersedes this (provides frontmatter + explicit sheet markers), so Lift 2 is a fallback if we don't want to ship a Python plugin.

### Lift 3 — Pass-through `Warnings` + `Title` (LOW impact, ~30 LOC Go + a markitdown-mcp fork OR a thin Aura-side HTTP wrapper)

`ConvertResult.Warnings` is declared in `types.go:29` but never populated. Markitdown DOES emit warnings (`warn(...)` calls in `_markitdown.py:65-82, 230, 247` for plugin load failures and double-registration; converters use `warn()` too). To carry them out:

- Cheapest: write a thin Aura-owned MCP shim (Python ~40 LOC) that wraps `MarkItDown().convert_uri(uri)` in `warnings.catch_warnings(record=True)` and returns a JSON object `{markdown, title, warnings, sheet_names}` instead of a raw string.
- Surface `title` (currently dropped by `__main__.py:23`).
- Same shim is the natural place for Lift 1's plugin install + custom xlsx-aware metadata return.

This is plumbing, not algorithmic — but it unblocks every future structured-extraction lift.

## xlsx-specific recommendation

**Ideal ingest path** given today's failure case:

1. **Upload**: xlsx arrives via `/api/sources/upload` → store at `wiki/raw/src_<sha>/source.xlsx` (already done).
2. **Convert via custom Aura plugin (Lift 1)**:
   - YAML frontmatter: `workbook_title`, `author`, `created`, `modified`, `sheets: [Sheet1, Sheet2…]`, `row_counts: {Sheet1: 180}`.
   - Per sheet: `## Sheet: <name> (<n> rows)` heading.
   - Per row: `### <heading-of-row>` (best-effort: first non-empty cell, or column-A value) + bullet list of `**<header>**: <value>` pairs.
3. **Persist** `extract.md` at `wiki/raw/src_<sha>/extract.md` (same path Aura uses today; no API change).
4. **Optional split (Lift 2)**: if sheet count > 1 OR rows > N (config knob), write `extract.sheet_<i>.md` siblings. The ingest pipeline can iterate them.
5. **Ingest**:
   - Pass 1 — summary page (today's behavior, keep as a navigation index): "Workbook X has sheets [a, b, c], owner Y, 180 rows total".
   - Pass 2 (NEW) — per-entity wiki pages OR per-sheet wiki pages with the entity list **preserved verbatim** as the body. Each `###` row block becomes either a wiki page or a chunk that the vector index can retrieve directly. No more "summary loses customer codes".
6. **Retrieval**: agent asks "Delta Automazioni codice?" → vector hit lands on the row chunk → tool returns the `**code**: 615827` line directly, no need to re-read raw `extract.md`.

Compare to today: pandas-table dump → summary → entity classes only. The row-level data exists on disk (`extract.md` is 180 rows long) but is invisible to retrieval because (a) it isn't chunked at row boundaries, (b) it isn't projected into the wiki/vector index, and (c) the summary actively masks it.

## Plugin vs post-process verdict

**Verdict: plugin (Lift 1).** Reasoning:

- **Cost**: a single Python file (~150-250 LOC) + one `RUN pip install` line in the markitdown sidecar's Dockerfile + flipping `MARKITDOWN_ENABLE_PLUGINS=true` (already plumbed). Aura's Go wrapper does NOT change. The Aura/markitdown contract stays `bytes → markdown` — the markdown is just shaped better.
- **Correctness**: structure is extracted ONCE, at the only place that has full fidelity (the openpyxl object model — formulas, properties, dimensions, merged cells, named ranges). Post-processing markdown means re-parsing markdown tables, which loses cell types, drops empty cells, and is brittle to header collisions across sheets.
- **Performance**: same workbook is parsed exactly once. Built-in xlsx converter is replaced (lower priority value wins), not run in addition.
- **Ownership**: the plugin lives in Aura's repo (e.g. `services/aura-markitdown-plugin/`), version-pinned to Aura. Nothing upstream changes; no fork of markitdown needed.
- **Generalizable**: the same plugin package can carry an `AuraDocxConverter`, `AuraPptxConverter`, etc. as future failure cases surface. Post-processing would have to be reinvented per format.

Post-processing (Lift 2) is still worth shipping AS WELL — it's free given the structure Lift 1 produces (well-known frontmatter + heading conventions) — but it should not be the primary fix. Doing post-process alone is rebuilding the workbook's structure from the markitdown lossy projection of it, which is exactly the bug that bit us today.
