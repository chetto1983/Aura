-- Reverse 0088: take the machine card back out of the ranking vector and drop the
-- column. The digest keeps whatever the agent wrote; only the derived text is lost, and
-- ingest rebuilds it on the next upload.
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

-- Same reason as the up migration: 0085's fastupdate = off is a property of the index,
-- so a rebuild has to restate it.
CREATE INDEX documents_digest_tsv_idx
    ON aura.documents USING gin (digest_tsv) WITH (fastupdate = off);

ALTER TABLE aura.documents
    DROP COLUMN IF EXISTS card;
