// -- migrate:up
// Document ingestion schema. The existing 0001 migration owns the Chunk unique
// id, vector index, and fulltext index. This migration adds document metadata
// and secondary lookup indexes used by the document ingestion pipeline.

CREATE CONSTRAINT document_id IF NOT EXISTS
  FOR (d:Document) REQUIRE d.id IS UNIQUE;

CREATE INDEX document_source_id IF NOT EXISTS
  FOR (d:Document) ON (d.source_id);

CREATE INDEX document_content_hash IF NOT EXISTS
  FOR (d:Document) ON (d.content_hash);

CREATE INDEX document_status IF NOT EXISTS
  FOR (d:Document) ON (d.status);

CREATE INDEX chunk_document_id IF NOT EXISTS
  FOR (c:Chunk) ON (c.document_id);

CREATE INDEX chunk_content_hash IF NOT EXISTS
  FOR (c:Chunk) ON (c.content_hash);

CREATE INDEX chunk_chunk_hash IF NOT EXISTS
  FOR (c:Chunk) ON (c.chunk_hash);
