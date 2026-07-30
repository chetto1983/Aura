// -- migrate:up
// The singleton brackets versioned document retrieval and is advanced by every
// production mutation that can change document/chunk membership, scope, order, or score.

CREATE CONSTRAINT document_corpus_revision_singleton IF NOT EXISTS
  FOR (r:DocumentCorpusRevision) REQUIRE r.singleton IS UNIQUE;

MERGE (r:DocumentCorpusRevision {singleton: 'corpus'})
ON CREATE SET
  r.epoch = 1,
  r.created_at = datetime(),
  r.updated_at = datetime();
