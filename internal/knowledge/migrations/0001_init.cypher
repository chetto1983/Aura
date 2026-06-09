// -- migrate:up
// Source: Neo4j 5.26 Cypher manual (vector + fulltext index syntax) + PRD §Slice 0.7 amendment #20 (HNSW M=32)
// D-08 minimal scope: ONLY the :Chunk constraint + vector + fulltext indexes.
// No other node labels here — the richer memory schema lands in later phases.

CREATE CONSTRAINT chunk_id IF NOT EXISTS
  FOR (c:Chunk) REQUIRE c.id IS UNIQUE;

CREATE VECTOR INDEX chunk_embedding IF NOT EXISTS
  FOR (c:Chunk) ON c.embedding
  OPTIONS { indexConfig: {
    `vector.dimensions`: 384,
    `vector.similarity_function`: 'cosine',
    `vector.hnsw.m`: 32,
    `vector.hnsw.ef_construction`: 200
  }};

CREATE FULLTEXT INDEX chunk_text IF NOT EXISTS
  FOR (c:Chunk) ON EACH [c.text]
  OPTIONS { indexConfig: {
    `fulltext.analyzer`: 'standard',
    `fulltext.eventually_consistent`: false
  }};
