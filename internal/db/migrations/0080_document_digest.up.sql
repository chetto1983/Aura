-- One digest per document, replacing per-chunk retrieval for the question
-- "WHICH file", which is the only question the index still has to answer once
-- the agent can open the file itself (document_open, 2026-07-31).
--
-- Measured on the corpus this replaces: a 5889-row spreadsheet became 118 chunks
-- and 118 vectors, and answered "how many customers in Torino" at 0% recall for
-- every k, because an aggregate is a property of the whole set and sits in no
-- passage. The same held for the 29 MB manual (616 distinct parameters, no k).
-- A digest is enough to pick the file; the file answers the question.
ALTER TABLE aura.documents
    ADD COLUMN IF NOT EXISTS digest text NOT NULL DEFAULT '';

COMMENT ON COLUMN aura.documents.digest IS
    'What this document IS, in enough words to pick it out of a library: sheets and their '
    'column headers with row counts for tabular content, the heading outline for prose. '
    'Not the content — document_open hands over the file for that.';

-- 'simple' does no stemming and no stop-word removal, DELIBERATELY. The corpus is
-- mixed Italian and English, and a single stemming configuration over a mixed
-- corpus was measured this week to halve recall (LOCOMO, same 1536 questions and
-- same turns, only the analyzer differing: Italian 18.6% R@10 vs English 43.8%).
-- Picking either language here would repeat that mistake in Postgres.
-- array_to_string(anyarray, text) is STABLE, not IMMUTABLE — it may invoke an
-- element type's output function — so a generated column cannot call it and
-- Postgres rejects the ALTER with "generation expression is not immutable".
-- For text[] specifically the flattening IS deterministic, so it is declared as
-- such here rather than dropping tags out of the ranking. Tags are the one part
-- of a document's description a PERSON wrote (the cockpit makes them editable),
-- and they belong in the vector above the generated digest.
CREATE OR REPLACE FUNCTION aura.text_array_words(words text[])
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
RETURNS NULL ON NULL INPUT
AS $$ SELECT array_to_string(words, ' ') $$;

COMMENT ON FUNCTION aura.text_array_words(text[]) IS
    'Immutable text[] -> space-joined text, so document tags can take part in the '
    'generated digest_tsv ranking vector.';

-- 'simple'::regconfig, cast explicitly: only the two-argument
-- to_tsvector(regconfig, text) is immutable; an untyped literal leaves the call
-- resolvable against the config-dependent form.
ALTER TABLE aura.documents
    ADD COLUMN IF NOT EXISTS digest_tsv tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('simple'::regconfig, coalesce(title, '')), 'A') ||
        setweight(to_tsvector('simple'::regconfig, aura.text_array_words(coalesce(tags, '{}'::text[]))), 'B') ||
        setweight(to_tsvector('simple'::regconfig, coalesce(digest, '')), 'C')
    ) STORED;

CREATE INDEX IF NOT EXISTS documents_digest_tsv_idx
    ON aura.documents USING gin (digest_tsv);
