package documents

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRetrievalStore answers the routing and revalidation half of retrieval. It is
// deliberately separate from the projection store: PostgreSQL owns which version is
// active and openable, and that verdict overrides anything the vector index still holds.
type PostgresRetrievalStore struct {
	pool *pgxpool.Pool
}

// NewPostgresRetrievalStore tolerates a nil pool so wiring can build the retriever
// before the pool exists; each method then reports "not configured" rather than panicking.
func NewPostgresRetrievalStore(pool *pgxpool.Pool) *PostgresRetrievalStore {
	return &PostgresRetrievalStore{pool: pool}
}

const readyDocumentJoins = `
JOIN aura.document_versions version
  ON version.id = document.active_version_id
 AND version.identity_id = document.identity_id
 AND version.document_id = document.id
JOIN aura.storage_objects raw_object
  ON raw_object.id = version.storage_object_id
 AND raw_object.identity_id = document.identity_id
 AND raw_object.document_id = document.id
 AND raw_object.version_id = version.id
JOIN aura.assets asset
  ON asset.id = version.asset_id
 AND asset.identity_id = document.identity_id
`

const readyDocumentWhere = `
document.identity_id = $1::uuid
AND document.status = 'ready'
AND document.deleted_at IS NULL
AND document.pipeline_generation > 0
AND version.status = 'ready'
AND version.deleted_at IS NULL
AND version.pipeline_generation = document.pipeline_generation
AND raw_object.status = 'live'
AND raw_object.deleted_at IS NULL
AND raw_object.asset_id = asset.id
AND raw_object.sha256 = version.sha256
AND asset.status = 'searchable'
AND asset.deleted_at IS NULL
AND asset.catalog_document_id = document.id
AND asset.document_version_id = version.id
AND asset.pipeline_generation = document.pipeline_generation
`

const resolveDocumentScopeSQL = `
SELECT document.id::text, document.search_document_id
FROM aura.documents document
` + readyDocumentJoins + `
WHERE ` + readyDocumentWhere + `
  AND (document.id::text = ANY($2::text[]) OR document.search_document_id = ANY($2::text[]))
ORDER BY document.id
`

const routeDocumentCardsSQL = `
SELECT document.id::text, document.search_document_id, document.title, document.tags,
       document.digest, document.card, version.sha256,
       ts_rank(document.digest_tsv,
         websearch_to_tsquery('simple', public.unaccent('public.unaccent'::regdictionary, $2::text)))
FROM aura.documents document
` + readyDocumentJoins + `
WHERE ` + readyDocumentWhere + `
  -- An absent scope means "every ready document", and it arrives here as a NIL slice from
  -- ResolveDocumentScope, which pgx encodes as SQL NULL rather than an empty array. Under
  -- three-valued logic NULL = '{}' is NULL and id = ANY(NULL) is NULL, so the older
  -- "$3 = '{}' OR ..." form evaluated to NULL for every row and this leg returned NOTHING
  -- on the common unscoped path. cardinality() over a coalesced array is TRUE/FALSE for
  -- both spellings.
  AND (cardinality(coalesce($3::text[], '{}'::text[])) = 0
       OR document.id::text = ANY($3::text[]))
  AND document.digest_tsv @@ websearch_to_tsquery(
        'simple', public.unaccent('public.unaccent'::regdictionary, $2::text))
ORDER BY 8 DESC, document.id ASC
LIMIT $4
`

// ResolveDocumentScope narrows caller-supplied document ids to the ones this identity
// actually owns. Unknown ids are dropped rather than rejected, so a stale id in a client
// request cannot be used to probe whether a document exists in another tenant.
func (s *PostgresRetrievalStore) ResolveDocumentScope(
	ctx context.Context,
	identityID string,
	documentIDs []string,
) ([]string, error) {
	if len(documentIDs) == 0 {
		return nil, nil
	}
	if err := validateRetrievalStore(s, identityID); err != nil {
		return nil, err
	}
	requested := make(map[string]struct{}, len(documentIDs))
	for _, id := range documentIDs {
		requested[id] = struct{}{}
	}
	resolved := make(map[string]struct{}, len(documentIDs))
	matched := make(map[string]struct{}, len(documentIDs))
	err := db.WithIdentityTxRaw(ctx, s.pool, identityID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, resolveDocumentScopeSQL, identityID, documentIDs)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var catalogID, searchID string
			if err := rows.Scan(&catalogID, &searchID); err != nil {
				return err
			}
			resolved[catalogID] = struct{}{}
			if _, ok := requested[catalogID]; ok {
				matched[catalogID] = struct{}{}
			}
			if _, ok := requested[searchID]; ok {
				matched[searchID] = struct{}{}
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("documents: resolve retrieval scope: %w", err)
	}
	if len(matched) != len(requested) {
		return nil, ErrInvalidDocumentScope
	}
	out := make([]string, 0, len(resolved))
	for id := range resolved {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// RouteDocumentCards is the lexical leg over title, tags, digest and card. It runs even
// when the vector index is reachable: the cascade needs a candidate set that survives an
// embedding failure, and this is the leg that degrades to.
func (s *PostgresRetrievalStore) RouteDocumentCards(
	ctx context.Context,
	identityID string,
	query string,
	documentIDs []string,
	limit int,
) ([]RetrievalCard, error) {
	if err := validateRetrievalStore(s, identityID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" || limit <= 0 {
		return nil, fmt.Errorf("documents: card route requires a query and positive limit")
	}
	cards := make([]RetrievalCard, 0, min(limit, 32))
	err := db.WithIdentityTxRaw(ctx, s.pool, identityID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, routeDocumentCardsSQL, identityID, query, documentIDs, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			card, err := scanRetrievalCard(rows)
			if err != nil {
				return err
			}
			cards = append(cards, card)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return cards, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanRetrievalCard(row rowScanner) (RetrievalCard, error) {
	var card RetrievalCard
	var rank float32
	err := row.Scan(
		&card.CatalogID, &card.DocumentID, &card.Title, &card.Tags,
		&card.Digest, &card.Card, &card.OriginalSHA256, &rank,
	)
	card.Rank = float64(rank)
	return card, err
}

func validateRetrievalStore(store *PostgresRetrievalStore, identityID string) error {
	if store == nil || store.pool == nil {
		return fmt.Errorf("documents: retrieval store is not configured")
	}
	if _, err := uuid.Parse(strings.TrimSpace(identityID)); err != nil {
		return fmt.Errorf("documents: retrieval identity is invalid")
	}
	return nil
}
