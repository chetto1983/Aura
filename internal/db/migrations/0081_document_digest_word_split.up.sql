-- Filenames were indexing as ONE token, which made the library unsearchable by
-- the most likely query of all.
--
-- Postgres' text parser classifies `Clienti.xlsx` as a single `file` token, so
-- to_tsvector('simple','Clienti.xlsx') is {'clienti.xlsx'} and a search for
-- `clienti` misses it. Measured on the live catalog right after 0080 landed: the
-- document was listed by the library but returned zero hits for its own name.
--
-- searchable_text keeps BOTH forms — the original, so an exact `clienti.xlsx`
-- still matches, and a separator-split copy, so every word inside a filename,
-- a column header or a heading is reachable on its own.
CREATE OR REPLACE FUNCTION aura.searchable_text(source text)
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
AS $$
    SELECT coalesce(source, '') || ' ' ||
           regexp_replace(coalesce(source, ''), '[^[:alnum:]]+', ' ', 'g')
$$;

COMMENT ON FUNCTION aura.searchable_text(text) IS
    'Text as written PLUS the same text split on every non-alphanumeric run, so '
    'Clienti.xlsx is findable both as itself and as the word "clienti".';

-- A generated column''s expression cannot be altered in place; the column is
-- derived, so dropping and recomputing it costs only the rewrite.
DROP INDEX IF EXISTS aura.documents_digest_tsv_idx;

ALTER TABLE aura.documents
    DROP COLUMN IF EXISTS digest_tsv;

ALTER TABLE aura.documents
    ADD COLUMN digest_tsv tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('simple'::regconfig, aura.searchable_text(title)), 'A') ||
        setweight(to_tsvector('simple'::regconfig,
            aura.searchable_text(aura.text_array_words(coalesce(tags, '{}'::text[])))), 'B') ||
        setweight(to_tsvector('simple'::regconfig, aura.searchable_text(digest)), 'C')
    ) STORED;

CREATE INDEX documents_digest_tsv_idx
    ON aura.documents USING gin (digest_tsv);
