-- The card the ingest sidecar builds for each reconciled object.
--
-- WHY IT MATTERS MORE THAN IT LOOKS. A card is how a document is FOUND, and for a
-- spreadsheet it is how one is ANSWERED. Measured 2026-08-08 on a 500-row, 3-sheet
-- workbook with a merged banner above the real header: filecard located the header on
-- row 4, typed every column, and reported value distributions -- "Citta: 7 distinct
-- values, most common Torino (98), Milano (94), Roma (89)". That is exactly the question
-- internal/documents/open.go records as scoring 0% at every k when asked of passages,
-- because an aggregate is a property of the whole set and lives in no chunk.
--
-- Before this column, a document reconciled from the bucket had no card at all: only the
-- upload path built one, so the same spreadsheet was answerable if uploaded and merely
-- searchable if it arrived in the bucket. The two paths now produce the same card from
-- the same implementation -- cmd/aura-filecard, shipped into the sidecar image and called
-- the way extract.py already calls soffice.
--
-- NOT NULL DEFAULT '' rather than nullable: an empty card means "could not be described",
-- which is a real and expected outcome (a truncated zip still gets a row), while NULL
-- would leave callers to invent the difference between absent and empty.
ALTER TABLE aura.ingested_documents
    ADD COLUMN card text NOT NULL DEFAULT '';

COMMENT ON COLUMN aura.ingested_documents.card IS
    'What filecard measured about the object: sheets, detected headers, column types, value distributions and sample rows. Empty when the file could not be read -- never an error, because a card is how a document is found, not whether it was stored.';
