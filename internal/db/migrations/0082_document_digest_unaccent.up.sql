-- Accents made half the Italian vocabulary unsearchable.
--
-- Measured on the live catalog right after the agent wrote its first description:
-- the digest said `Località` and a search for `localita` returned nothing, while
-- `clienti` and `ragione sociale` ranked fine. Italian is typed without accents
-- far more often than with them — nobody reaches for à on a keyboard mid-query —
-- so the index has to fold them.
CREATE EXTENSION IF NOT EXISTS unaccent;

-- unaccent(text) is STABLE, not IMMUTABLE: it resolves a dictionary by name at
-- call time, so a generated column rejects it. The two-argument form takes the
-- dictionary explicitly and is deterministic for a fixed one, which is what the
-- wrapper declares. Same shape as aura.text_array_words in 0080.
CREATE OR REPLACE FUNCTION aura.searchable_text(source text)
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
AS $$
    SELECT coalesce(source, '') || ' ' ||
           regexp_replace(coalesce(source, ''), '[^[:alnum:]]+', ' ', 'g') || ' ' ||
           unaccent('unaccent'::regdictionary, coalesce(source, '')) || ' ' ||
           regexp_replace(
               unaccent('unaccent'::regdictionary, coalesce(source, '')),
               '[^[:alnum:]]+', ' ', 'g')
$$;

COMMENT ON FUNCTION aura.searchable_text(text) IS
    'Text as written, split on non-alphanumerics, and both again with accents folded — '
    'so Località is found by "localita" and Clienti.xlsx by "clienti".';

-- The generated column must be recomputed against the new definition; Postgres
-- does not re-run a STORED expression when a function it calls changes.
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
