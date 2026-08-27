package arcadedb

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// IndexedDocumentType is the vertex type services/ingest writes one record per object to.
// Its DDL lives in services/ingest/arcade.py, which mirrors document_schema.go.
const IndexedDocumentType = "IndexedDocument"

// DocumentCard is one document's own description, as the reconciler recorded it.
//
// It replaces the card the PostgreSQL control plane used to route, and carries no catalog
// id, tags or digest: an object has none of those, and the row that invented them is gone.
// SourceKey is the route back to the bytes; Card is what makes an aggregate answerable at
// all, because a question like "how many customers in Torino" is a property of the whole
// set and lives in no passage.
type DocumentCard struct {
	SearchDocumentID string
	SourceKind       string
	SourceKey        string
	FileName         string
	RawSHA256        string
	SizeBytes        int64
	PassageCount     int64
	Card             string
	Score            float64
}

// documentCardFields is every property the reader consumes, in the order decodeCard reads
// them. file_name_words is deliberately absent: it exists only to be indexed.
const documentCardFields = "search_document_id, source_kind, source_key, file_name, " +
	"raw_sha256, size_bytes, passage_count, card"

// DocumentCards ranks documents by their own description, and is the leg that survives.
//
// It runs even when the vector index is reachable, because the cascade needs a candidate
// set that outlives an embedding failure. Both legs of the OR matter: the card matches
// what a document CONTAINS ("Fatturato", "Torino"), the split file name matches what it is
// CALLED. The name needs its own property because Lucene's StandardAnalyzer keeps
// "clienti_complesso.xlsx" as one token -- measured -- so a search for "clienti" finds
// nothing against the raw name.
func (d *DocumentIndex) DocumentCards(
	ctx context.Context,
	identityID string,
	query string,
	limit int,
) ([]DocumentCard, error) {
	return d.DocumentCardsScoped(ctx, CandidateFilter{
		IdentityID: identityID, Limit: limit,
	}, query)
}

// DocumentCardsScoped is the degraded-capable card leg constrained by the same document
// ids and Garage source coordinates as the fused passage leg.
func (d *DocumentIndex) DocumentCardsScoped(
	ctx context.Context,
	filter CandidateFilter,
	query string,
) ([]DocumentCard, error) {
	filter, err := d.normalizeCandidateFilter(filter)
	if err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("arcadedb: document card query must be non-empty")
	}
	if utf8.RuneCountInString(query) > d.config.MaxQueryRunes {
		return nil, fmt.Errorf("arcadedb: document query exceeds %d characters", d.config.MaxQueryRunes)
	}
	client, err := d.tenantClient(ctx, filter.IdentityID)
	if err != nil {
		return nil, err
	}
	where, params := candidateWhere(filter)
	params["query"] = escapeLucene(query)
	// AND binds more tightly than OR, so parenthesize the two SEARCH_INDEX legs before
	// applying the scope; otherwise a card-text match could bypass every source filter.
	statement := "SELECT " + documentCardFields + ", $score AS card_score FROM " +
		IndexedDocumentType + " WHERE (SEARCH_INDEX('" + IndexedDocumentType +
		"[card]', :query) = true OR SEARCH_INDEX('" + IndexedDocumentType +
		"[file_name_words]', :query) = true) AND " + where +
		" ORDER BY card_score DESC, search_document_id ASC LIMIT " + strconv.Itoa(filter.Limit)
	rows, err := client.Query(ctx, statement, params)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: document cards: %w", err)
	}
	if len(rows) > filter.Limit {
		return nil, fmt.Errorf(
			"arcadedb: document card response has %d rows, limit is %d", len(rows), filter.Limit,
		)
	}
	cards := make([]DocumentCard, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for index, row := range rows {
		card, err := decodeDocumentCard(row)
		if err != nil {
			return nil, fmt.Errorf("arcadedb: document card %d: %w", index, err)
		}
		// The two SEARCH_INDEX legs are OR'd in one statement, so a document matching both
		// the card and its name comes back once -- but a duplicate would silently double
		// its weight in the ranking above, so it is refused rather than tolerated.
		if _, duplicate := seen[card.SearchDocumentID]; duplicate {
			return nil, fmt.Errorf("arcadedb: document card %s appears twice", card.SearchDocumentID)
		}
		seen[card.SearchDocumentID] = struct{}{}
		cards = append(cards, card)
	}
	return cards, nil
}

// ResolveDocumentScope narrows caller-supplied ids to the ones this identity actually has.
//
// An empty request means "every document" and returns an empty scope rather than an error:
// that is how the caller spells "do not filter", and turning it into a lookup of nothing
// would make the unscoped path -- the common one -- return no documents at all.
func (d *DocumentIndex) ResolveDocumentScope(
	ctx context.Context,
	identityID string,
	documentIDs []string,
) ([]string, error) {
	identityID = strings.TrimSpace(identityID)
	if err := validateIdentifier("document identity", identityID); err != nil {
		return nil, err
	}
	requested, err := d.normalizeDocumentIDs(documentIDs)
	if err != nil {
		return nil, err
	}
	if len(requested) == 0 {
		return nil, nil
	}
	client, err := d.tenantClient(ctx, identityID)
	if err != nil {
		return nil, err
	}
	rows, err := client.Query(ctx,
		"SELECT search_document_id FROM "+IndexedDocumentType+
			" WHERE search_document_id IN :ids ORDER BY search_document_id",
		map[string]any{"ids": requested})
	if err != nil {
		return nil, fmt.Errorf("arcadedb: resolve document scope: %w", err)
	}
	scope := make([]string, 0, len(rows))
	for _, row := range rows {
		id, err := requiredString(row, "search_document_id")
		if err != nil {
			return nil, err
		}
		scope = append(scope, id)
	}
	return scope, nil
}

// DocumentByID resolves one document's record from the id a retrieval hit carries.
//
// This is the whole of what document_open used to do in five hops -- doc_<hex> to a catalog
// uuid, to the document, to its active version, to an asset id, to an object. The record
// carries source_key, so the answer is one lookup and the bytes are one Get away.
func (d *DocumentIndex) DocumentByID(
	ctx context.Context,
	identityID string,
	searchDocumentID string,
) (DocumentCard, error) {
	identityID = strings.TrimSpace(identityID)
	if err := validateIdentifier("document identity", identityID); err != nil {
		return DocumentCard{}, err
	}
	searchDocumentID = strings.TrimSpace(searchDocumentID)
	if err := validateIdentifier("document id", searchDocumentID); err != nil {
		return DocumentCard{}, err
	}
	client, err := d.tenantClient(ctx, identityID)
	if err != nil {
		return DocumentCard{}, err
	}
	rows, err := client.Query(ctx,
		"SELECT "+documentCardFields+" FROM "+IndexedDocumentType+
			" WHERE search_document_id = :id LIMIT 2",
		map[string]any{"id": searchDocumentID})
	if err != nil {
		return DocumentCard{}, fmt.Errorf("arcadedb: document by id: %w", err)
	}
	if len(rows) == 0 {
		return DocumentCard{}, fmt.Errorf("arcadedb: document %s not found", searchDocumentID)
	}
	// search_document_id is UNIQUE-indexed, so two rows means the index is not doing its
	// job and picking either one would be arbitrary.
	if len(rows) > 1 {
		return DocumentCard{}, fmt.Errorf("arcadedb: document %s is not unique", searchDocumentID)
	}
	return decodeDocumentCard(rows[0])
}

// normalizeDocumentIDs trims, validates and de-duplicates caller-supplied document ids.
//
// Shared by every by-id lookup so a malformed id is refused identically in each, and so
// the bound on how many ids one statement may carry lives in a single place.
func (d *DocumentIndex) normalizeDocumentIDs(documentIDs []string) ([]string, error) {
	requested := make([]string, 0, len(documentIDs))
	seen := make(map[string]struct{}, len(documentIDs))
	for _, id := range documentIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if err := validateIdentifier("document filter id", id); err != nil {
			return nil, err
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		requested = append(requested, id)
	}
	if len(requested) > d.config.MaxDocumentFilters {
		return nil, fmt.Errorf(
			"arcadedb: document filter count %d exceeds maximum %d",
			len(requested), d.config.MaxDocumentFilters)
	}
	return requested, nil
}

// DocumentNames resolves each id's display name, for hits the card leg never ranked.
//
// A Passage mirrors the CHUNK, not the object, so it carries no name: a document that
// reached the answer through the passage leg alone would be titled with the tail of its
// key, and a chat attachment's key is a uuid on purpose. The name is one record away in
// this same database, keyed by the id the hit already carries -- the same lookup
// document_open makes -- so it costs one statement rather than a field on every chunk.
func (d *DocumentIndex) DocumentNames(
	ctx context.Context,
	identityID string,
	documentIDs []string,
) (map[string]string, error) {
	identityID = strings.TrimSpace(identityID)
	if err := validateIdentifier("document identity", identityID); err != nil {
		return nil, err
	}
	requested, err := d.normalizeDocumentIDs(documentIDs)
	if err != nil {
		return nil, err
	}
	if len(requested) == 0 {
		return nil, nil
	}
	client, err := d.tenantClient(ctx, identityID)
	if err != nil {
		return nil, err
	}
	rows, err := client.Query(ctx,
		"SELECT search_document_id, file_name FROM "+IndexedDocumentType+
			" WHERE search_document_id IN :ids",
		map[string]any{"ids": requested})
	if err != nil {
		return nil, fmt.Errorf("arcadedb: document names: %w", err)
	}
	names := make(map[string]string, len(rows))
	for index, row := range rows {
		id, err := requiredString(row, "search_document_id")
		if err != nil {
			return nil, fmt.Errorf("arcadedb: document name %d: %w", index, err)
		}
		name, err := requiredString(row, "file_name")
		if err != nil {
			return nil, fmt.Errorf("arcadedb: document name %s: %w", id, err)
		}
		names[id] = name
	}
	return names, nil
}

func decodeDocumentCard(row map[string]any) (DocumentCard, error) {
	card := DocumentCard{}
	var err error
	for _, field := range []struct {
		key    string
		target *string
	}{
		{"search_document_id", &card.SearchDocumentID},
		{"source_kind", &card.SourceKind},
		{"source_key", &card.SourceKey},
		{"file_name", &card.FileName},
		{"raw_sha256", &card.RawSHA256},
	} {
		if *field.target, err = requiredString(row, field.key); err != nil {
			return DocumentCard{}, err
		}
	}
	if !validSHA256(card.RawSHA256) {
		return DocumentCard{}, fmt.Errorf("document card carries an invalid SHA-256")
	}
	// The card itself is OPTIONAL, and that is deliberate: filecard returns an empty card
	// for a file it could not read, and a document that cannot be described still exists,
	// is still searchable by name, and can still be opened.
	if card.Card, err = optionalString(row, "card"); err != nil {
		return DocumentCard{}, err
	}
	if card.SizeBytes, err = requiredInt64(row, "size_bytes", false); err != nil {
		return DocumentCard{}, err
	}
	if card.PassageCount, err = requiredInt64(row, "passage_count", false); err != nil {
		return DocumentCard{}, err
	}
	card.Score = optionalFloat(row, "card_score")
	return card, nil
}

func optionalFloat(row map[string]any, key string) float64 {
	switch value := row[key].(type) {
	case float64:
		return value
	case int64:
		return float64(value)
	case int:
		return float64(value)
	default:
		return 0
	}
}
