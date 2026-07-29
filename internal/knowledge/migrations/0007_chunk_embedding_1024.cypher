// -- migrate:up
// Move the chunk vector index to the embedder's native width: 768 -> 1024.
//
// Why the width changes at all. The sidecar default is Qwen3-Embedding-0.6B, which is
// 1024 native. It was being narrowed to 768 by Matryoshka truncation only because the
// index was created at 768 back when a 768-native model was in use. Truncation is a
// supported, lossy operation — keeping it costs retrieval quality for nothing once the
// index can hold the full vector. Measured on the appliance CPU (Intel N95, -ngl 0),
// the model swap that motivated this costs no latency: ~34 ms per embedding against
// ~91 ms for the 768-native model it replaces.
//
// Why the vectors are DELETED and not left in place. A stored 768-d vector under a
// 1024-d index is the silent-corruption case this codebase already guards against at
// boot (internal/knowledge/ping.go): Neo4j accepts a mis-sized vector and drops the node
// from the index WITHOUT erroring, so search quietly stops returning it while the data
// still looks present. Removing the property makes the gap visible and forces the
// re-embed that a width change requires anyway.
//
// Operator consequence, stated plainly: after this migration every Chunk must be
// re-embedded before document search returns anything. On a large corpus the REMOVE
// below is one transaction — batch it with CALL {} IN TRANSACTIONS if the graph is big
// enough for that to matter.
//
// NOT covered here: the agent-memory MCP owns entity/fact/message/preference/step/task
// vector indexes and creates them itself from --embedding-dimensions
// ($AURA_EMBED_DIMENSIONS). Those are created IF NOT EXISTS, so an existing 768-d index
// survives an env change untouched — they must be dropped separately for the sidecar to
// rebuild them at the new width.
//
// Re-runnable: DROP IF EXISTS, a REMOVE that no-ops on an empty match, CREATE IF NOT EXISTS.

DROP INDEX chunk_embedding IF EXISTS;

MATCH (c:Chunk) WHERE c.embedding IS NOT NULL REMOVE c.embedding;

CREATE VECTOR INDEX chunk_embedding IF NOT EXISTS
  FOR (c:Chunk) ON c.embedding
  OPTIONS { indexConfig: {
    `vector.dimensions`: 1024,
    `vector.similarity_function`: 'cosine',
    `vector.hnsw.m`: 32,
    `vector.hnsw.ef_construction`: 200
  }};
