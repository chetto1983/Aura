DROP INDEX IF EXISTS aura.documents_digest_tsv_idx;

ALTER TABLE aura.documents
    DROP COLUMN IF EXISTS digest_tsv;

-- Back to the 0080 shape: raw text, filenames as single tokens.
ALTER TABLE aura.documents
    ADD COLUMN digest_tsv tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('simple'::regconfig, coalesce(title, '')), 'A') ||
        setweight(to_tsvector('simple'::regconfig, aura.text_array_words(coalesce(tags, '{}'::text[]))), 'B') ||
        setweight(to_tsvector('simple'::regconfig, coalesce(digest, '')), 'C')
    ) STORED;

CREATE INDEX documents_digest_tsv_idx
    ON aura.documents USING gin (digest_tsv);

DROP FUNCTION IF EXISTS aura.searchable_text(text);
