# OOXML routing-card boundary

**Status:** retained by measurement, 2026-08-27
**Decision authority:** PRD amendment #162

## Outcome

Aura retains the small standard-library OOXML reader behind `filecard.Build`. It is a
bounded routing-card generator, not the spreadsheet engine used for exact answers. The
owned corpus now has a checked-in openpyxl oracle and an executable parity test.

The measurement also found and fixed one real defect: a blank physical row before the
selected header made invoice cards report six rows after the header instead of five.

## Runtime boundary

`aura-filecard` runs during ingest and emits at most 4 KiB of descriptive text. Its XLSX
scanner enforces these read limits before retaining workbook content:

- 8 sheets;
- 10,000 row elements per sheet;
- 64 columns;
- 512 tracked values per column;
- 8 MiB of shared strings.

The card is deliberately lossy. `document_open` gives the original workbook to the agent's
sandbox, whose declared toolchain includes LibreOffice, openpyxl and pandas. Exact filters,
counts, formulas and aggregates belong there; they must not use the card as authoritative
data.

## Dependency inventory

- `go.mod` contains no workbook library and no Excelize dependency.
- `docker/aura-ingest` contains LibreOffice for legacy Office normalization and invokes the
  Go `aura-filecard` binary; it does not use openpyxl to build cards.
- `docker/aura` and `docker/aura-sandbox` declare openpyxl for agent-side workbook work, but
  the pip requirement is not version-pinned.
- The parity manifest freezes the oracle that was actually measured: openpyxl 3.1.5.

This inventory is why replacing the Go scanner with “the installed workbook library” was
not an available refactor: no such Go dependency existed, and the Python reader serves a
different, sandboxed exact-computation boundary.

## Corpus measurement

The oracle manifest covers all 17 owned XLSX fixtures and all 18 of their sheets. It stores
the fixture SHA-256, sheet order and name, physical maximum row, maximum column, explicit
header row, header values and non-empty values per column.

Before the fix:

- sheet count/order/name, headers and column count: 18/18 exact;
- row span after the header: 4/18 exact;
- all 14 mismatches were invoice fixtures with the same +1 error.

After the fix, `TestOwnedWorkbookCorpusMatchesOpenpyxlOracle` requires every recorded field
to match on 18/18 sheets. `TestWorkbookRowCountUsesPhysicalHeaderPosition` isolates the blank
row regression without depending on the corpus.

The manifest is `scripts/fixtures/document_retrieval_eval/ooxml_parity.json`. When an owned
workbook changes, regenerate its values with exactly openpyxl 3.1.5, review the explicit
header row rather than inferring it from `filecard`, and update the workbook hash and oracle
values together. The Go test rejects an added, removed or renamed XLSX that is absent from
the manifest.

## Why Excelize was not adopted

Excelize's official `Rows`/`GetRows` surface is broad enough to replace much of the scanner,
but the current security advisory
[GHSA-q5j5-6p94-4gwc](https://github.com/qax-os/excelize/security/advisories/GHSA-q5j5-6p94-4gwc)
lists releases through 2.10.1 as affected by excessive allocation from attacker-controlled
row indexes and lists no patched release. Operator-uploaded workbooks cross an untrusted file
boundary even though Aura's own agent writes trusted memory. Adopting that release would
trade the current bounded row-element scan for a known memory-exhaustion surface.

Replacement should be reconsidered when a patched version can be pinned and can preserve
the same decompression/read caps behind `filecard.Build`. The existing oracle is the migration
gate for that future candidate.

## Residual scope

The corpus does not establish compatibility with every OOXML producer or feature. It contains
no macro behavior, charts, external links or formula-heavy workbooks. Unsupported detail is
acceptable only because the card states that it describes the file and the original remains
available to the authoritative sandbox reader. New routing failures require a fixture and an
oracle update; they are not grounds for silently broadening this parser into an Excel engine.
