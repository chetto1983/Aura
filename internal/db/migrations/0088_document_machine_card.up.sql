-- Split what the MACHINE knows about a document from what the AGENT wrote about it.
--
-- 0080 gave every document one digest, and document_describe was the only thing that
-- ever wrote it: the agent's note, made after it had opened the file. That leaves a file
-- nobody opened with an empty digest, and an empty digest is invisible to
-- document_search forever — the index only knew about documents somebody had already
-- found another way. At tens of files an operator absorbs that; production is thousands.
--
-- Ingest now writes a card from the file itself (internal/documents/filecard), and it
-- gets its own column rather than overwriting the digest. Two reasons, both concrete:
--
--   1. document_describe REPLACES the digest in full. With one column, the first note
--      the agent writes deletes the sheet names, the column narrations and the sampled
--      rows the machine had extracted — the model is not going to retype 3,000
--      characters to keep them. Separate columns make document_describe an editor of
--      what it knows on top of what the machine measured, which is the only division of
--      labour that survives contact with a model.
--   2. They deserve different weights. A person's tags and an agent's note are curated;
--      a card is bulk derived from structure. Weight D is where bulk belongs — Postgres'
--      default ts_rank weights are {D,C,B,A} = {0.1, 0.2, 0.4, 1.0}, so the card can
--      make a file findable without ever outranking what a human said about it.
ALTER TABLE aura.documents
    ADD COLUMN IF NOT EXISTS card text NOT NULL DEFAULT '';

COMMENT ON COLUMN aura.documents.card IS
    'Machine-written at ingest from the file itself, with no LLM: sheet names, column '
    'headers with their frequent values, sampled rows, or the metadata and page count of '
    'a format whose text cannot be read without opening it. A proxy for the file, never '
    'the file. aura.documents.digest holds the agent''s own note instead.';

-- A generated column's expression cannot be altered in place, and Postgres does not
-- recompute a STORED expression when a function it calls changes, so the column is
-- dropped and rebuilt — the same shape 0081 and 0082 used.
DROP INDEX IF EXISTS aura.documents_digest_tsv_idx;

ALTER TABLE aura.documents
    DROP COLUMN IF EXISTS digest_tsv;

ALTER TABLE aura.documents
    ADD COLUMN digest_tsv tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('simple'::regconfig, aura.searchable_text(title)), 'A') ||
        setweight(to_tsvector('simple'::regconfig,
            aura.searchable_text(aura.text_array_words(coalesce(tags, '{}'::text[])))), 'B') ||
        setweight(to_tsvector('simple'::regconfig, aura.searchable_text(digest)), 'C') ||
        setweight(to_tsvector('simple'::regconfig, aura.searchable_text(card)), 'D')
    ) STORED;

-- fastupdate = off is carried forward EXPLICITLY. 0085 turned it off with ALTER INDEX on
-- the index 0082 created; recreating the index here would silently take the default
-- (`ON`, per CREATE INDEX / GIN storage parameters) and quietly reintroduce the pending-
-- list scan tax that 0085 measured and removed. A storage parameter that lives in an
-- ALTER is a parameter the next rebuild loses.
CREATE INDEX documents_digest_tsv_idx
    ON aura.documents USING gin (digest_tsv) WITH (fastupdate = off);
