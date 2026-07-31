DROP INDEX IF EXISTS aura.documents_digest_tsv_idx;

ALTER TABLE aura.documents
    DROP COLUMN IF EXISTS digest_tsv;

-- Back to the 0081 definition: split on separators, accents NOT folded.
CREATE OR REPLACE FUNCTION aura.searchable_text(source text)
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
AS $$
    SELECT coalesce(source, '') || ' ' ||
           regexp_replace(coalesce(source, ''), '[^[:alnum:]]+', ' ', 'g')
$$;

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
