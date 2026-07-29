// Package knowledge is the graph + vector substrate: an MCP-subprocess Cypher
// client (no native Go Neo4j driver per CLAUDE.md §Project scope discipline ban),
// a Cypher migration runner that audits applied versions in Postgres
// aura.knowledge_migrations, and the embedding-sidecar dim self-test that makes
// the AURA_EMBED_DIMENSIONS contract operational (Amendment #18 / Pitfall #5). The
// width is deliberately not restated in prose here — it moved 384 -> 768 -> 1024 while
// comments went on asserting the old number.
//
// Config is pure data, populated by internal/config from the environment. It
// carries everything the MCP client and embed self-test need; no methods.
package knowledge

// DefaultEmbedDimensions is the NATIVE width of the repo's default embedding sidecar
// (Qwen3-Embedding-0.6B) and the width encoded in the Neo4j HNSW indexes from migration
// 0007. It is no longer an MRL-truncated value: truncation is lossy and buys nothing
// once the index holds the full vector. A deployment may still set a smaller
// AURA_EMBED_DIMENSIONS, but only together with indexes created at that same width.
const DefaultEmbedDimensions = 1024

// Config holds the Neo4j + mcp-neo4j-cypher + embed-sidecar wiring. Populated by
// internal/config.Load from NEO4J_*, AURA_NEO4J_*, AURA_MCP_NEO4J_*, AURA_EMBED_*.
type Config struct {
	BoltURL           string // bolt://host:7687 — passed to mcp-neo4j-cypher --db-url
	User              string // Neo4j auth user (default neo4j)
	Password          string // Neo4j auth password (NEO4J_PASSWORD; never logged unredacted)
	Database          string // Community edition is single-DB; always "neo4j" (Pitfall #8)
	MCPBinary         string // mcp-neo4j-cypher executable on PATH
	ConnectTimeoutSec int    // first-call connect/retry budget
	EmbedURL          string // OpenAI-compat embeddings base URL (sidecar)
	EmbedDimensions   int    // contract dim; boot self-test refuses a mismatch
	EmbedModel        string // AURA_EMBED_MODEL — set to a hosted model to swap embeddings to OpenRouter; empty = local embedding sidecar
}
