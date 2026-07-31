DROP INDEX IF EXISTS aura.documents_digest_tsv_idx;

ALTER TABLE aura.documents
    DROP COLUMN IF EXISTS digest_tsv;

ALTER TABLE aura.documents
    DROP COLUMN IF EXISTS digest;

-- After the generated column that referenced it, never before.
DROP FUNCTION IF EXISTS aura.text_array_words(text[]);
