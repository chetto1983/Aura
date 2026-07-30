package documents

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identityctx"
)

// OperatorIdentity is the seeded `local` identity (migration 0004, UUID …001) that owns
// every pre-isolation document. Web ingest threads asset.IdentityID (this UUID for the
// operator) and retrieval threads identityctx.IdentityID(ctx) (the same UUID once the
// operator logs in), so the graph `:User.identifier` for the operator's docs is this UUID —
// NOT the literal string "local". The backfill therefore attaches the operator edge under
// this identifier so the post-flip scoped retrieval (which matches on the same UUID) still
// finds the operator's existing documents (D-12: the flip must not orphan operator docs).
// It is the shared no-principal fallback owner (see identityctx.LocalOperatorIdentity) —
// memory-MCP scoping falls back to the same UUID, so both :User-writing subsystems agree.
const OperatorIdentity = identityctx.LocalOperatorIdentity

// DocumentOwner pairs a graph document id with the identity that owns it in Postgres.
// Identity is the identity_id UUID text (the same value ingest/retrieval use as the
// `:User.identifier`); DocumentID is the graph `doc_…` id of the `:Document` node.
type DocumentOwner struct {
	Identity   string
	DocumentID string
}

// BackfillReport summarizes one idempotent backfill run.
type BackfillReport struct {
	// OwnersSourced is the number of (identity, document) pairs read from Postgres.
	OwnersSourced int `json:"owners_sourced"`
	// EdgesFromMap is the number of ownership edges MERGEd from the Postgres map (Op 1).
	// Re-running the backfill leaves this unchanged only in the sense that MERGE creates
	// no duplicate edge — the count reflects matched (existing) documents, not new edges.
	EdgesFromMap int `json:"edges_from_map"`
	// OrphansAttached is the number of `:Document` nodes that had NO owner edge and were
	// attached to the operator (Op 2 — the D-12 safety net for CLI/legacy documents that
	// carry no Postgres identity→document row).
	OrphansAttached int `json:"orphans_attached_to_operator"`
}

// Backfiller attaches the `(:User {identifier})-[:HAS_DOCUMENT]->(:Document)` ownership
// edge (Phase 36 MUSR-01, D-12) to every existing document BEFORE the AURA_MUSR_ISOLATION
// flip so the scoped fail-closed retrieval does not orphan pre-isolation documents. Every
// write is a `MERGE`, so a re-run creates no duplicate edges (idempotent, D-12).
type Backfiller struct {
	// Graph is the Neo4j knowledge client (the same seam the Indexer writes through).
	Graph KnowledgeClient
	// Operator overrides the fallback owner for orphan documents. Empty ⇒ OperatorIdentity.
	Operator string
}

// operator resolves the orphan-fallback owner identity.
func (b *Backfiller) operator() string {
	if b.Operator != "" {
		return b.Operator
	}
	return OperatorIdentity
}

// Run performs the two idempotent ownership passes and returns a summary:
//
//	Op 1 (Postgres map): for each (identity, document) pair, MATCH the existing :Document
//	  and MERGE the owner's :User -[:HAS_DOCUMENT]-> edge. MATCH (not MERGE) on the Document
//	  means a stale Postgres row for a purged document creates no phantom node.
//	Op 2 (orphan net): every :Document with NO owner edge at all (CLI-ingested or legacy
//	  pre-36-05 documents that never carried a Postgres identity row) is attached to the
//	  operator — the D-12 guarantee that NOTHING is left orphaned before the flip.
//
// Both passes MERGE, so re-running is safe (no duplicate edges).
func (b *Backfiller) Run(ctx context.Context, owners []DocumentOwner) (BackfillReport, error) {
	if b.Graph == nil {
		return BackfillReport{}, fmt.Errorf("documents backfill has no knowledge client")
	}
	report := BackfillReport{OwnersSourced: len(owners)}

	if len(owners) > 0 {
		pairs := make([]map[string]any, 0, len(owners))
		for _, o := range owners {
			if o.Identity == "" || o.DocumentID == "" {
				continue
			}
			pairs = append(pairs, map[string]any{"identity": o.Identity, "document_id": o.DocumentID})
		}
		if len(pairs) > 0 {
			rows, err := b.Graph.Write(ctx, backfillOwnerEdgeQuery, map[string]any{"pairs": pairs})
			if err != nil {
				return report, fmt.Errorf("backfill ownership edges from map: %w", err)
			}
			report.EdgesFromMap = countFromRows(rows, 0)
		}
	}

	rows, err := b.Graph.Write(ctx, backfillOrphanEdgeQuery, map[string]any{"operator": b.operator()})
	if err != nil {
		return report, fmt.Errorf("backfill orphan ownership edges: %w", err)
	}
	report.OrphansAttached = countFromRows(rows, 0)
	return report, nil
}

// LoadDocumentOwners reads the identity→graph-document map from Postgres. It unions the two
// authoritative sources: aura.assets.document_id (the graph id an uploaded asset was indexed
// under) and aura.documents.metadata->>'search_document_id' (the control-plane catalog's
// pointer to the same graph id), each paired with its identity_id. Soft-deleted rows are
// included — an ownership edge on a deleted document is harmless (retrieval independently
// filters inactive/deleted chunks) and keeps ownership consistent if the row is undeleted.
func LoadDocumentOwners(ctx context.Context, db sqlc.DBTX) ([]DocumentOwner, error) {
	if db == nil {
		return nil, fmt.Errorf("document owner source has no database handle")
	}
	rows, err := db.Query(ctx, documentOwnerMapQuery)
	if err != nil {
		return nil, fmt.Errorf("load document owners: %w", err)
	}
	defer rows.Close()
	owners := make([]DocumentOwner, 0)
	for rows.Next() {
		var identity, documentID string
		if err := rows.Scan(&identity, &documentID); err != nil {
			return nil, fmt.Errorf("scan document owner: %w", err)
		}
		owners = append(owners, DocumentOwner{Identity: identity, DocumentID: documentID})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate document owners: %w", err)
	}
	return owners, nil
}

// documentOwnerMapQuery unions the two Postgres sources of the identity→graph-document map.
// identity_id::text is the exact string ingest/retrieval use as the `:User.identifier`.
const documentOwnerMapQuery = `
SELECT DISTINCT identity_id::text AS identity, document_id
FROM aura.assets
WHERE document_id <> '' AND identity_id IS NOT NULL
UNION
SELECT DISTINCT identity_id::text AS identity, metadata->>'search_document_id' AS document_id
FROM aura.documents
WHERE metadata->>'search_document_id' IS NOT NULL
  AND metadata->>'search_document_id' <> ''
`

// backfillOwnerEdgeQuery MERGEs the ownership edge for every Postgres-sourced pair. It
// MATCHes the existing :Document (never MERGEs it — a stale row must not resurrect a purged
// document) and mirrors the Indexer's `:User` node shape verbatim so the edge is identical
// to one an in-line ingest would have written.
const backfillOwnerEdgeQuery = `
UNWIND $pairs AS pair
MATCH (d:Document {id: pair.document_id})
MERGE (u:User {identifier: pair.identity})
  ON CREATE SET u.id = pair.identity, u.created_at = datetime()
MERGE (u)-[:HAS_DOCUMENT]->(d)
WITH count(*) AS count
` + advanceDocumentCorpusRevisionQuery + `
RETURN count
`

// backfillOrphanEdgeQuery attaches every still-unowned :Document to the operator (D-12 net).
// A document with no `:User -[:HAS_DOCUMENT]->` edge is a CLI-ingested or pre-36-05 legacy
// document that carries no Postgres identity row; leaving it unowned would make it vanish
// from the operator's scoped search after the flip.
const backfillOrphanEdgeQuery = `
MATCH (d:Document)
WHERE NOT EXISTS { (:User)-[:HAS_DOCUMENT]->(d) }
MERGE (u:User {identifier: $operator})
  ON CREATE SET u.id = $operator, u.created_at = datetime()
MERGE (u)-[:HAS_DOCUMENT]->(d)
WITH count(d) AS count
` + advanceDocumentCorpusRevisionQuery + `
RETURN count
`
