// -- migrate:up
// The service-owned singleton brackets multi-query Agent Memory recall.

CREATE CONSTRAINT memory_corpus_revision_singleton IF NOT EXISTS
  FOR (r:MemoryCorpusRevision) REQUIRE r.singleton IS UNIQUE;

MERGE (r:MemoryCorpusRevision {singleton: 'corpus'})
ON CREATE SET
  r.epoch = 1,
  r.created_at = datetime(),
  r.updated_at = datetime();
