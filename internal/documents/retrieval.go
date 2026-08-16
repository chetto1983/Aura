package documents

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/embeddings"
)

// ProductionRetrievalProfile is stamped on every response so a citation stored today can
// be read back tomorrow against the cascade that actually produced it. Bump it whenever the
// leg set, the admission thresholds or the ranking change — old citations then stay legible
// as products of the older profile instead of silently claiming the new one.
const ProductionRetrievalProfile = "arcadedb-fused-card-v2"

// The three sentinels separate blame. An empty query is the caller's input, an invalid scope
// means a named document is not visible to this identity or is not in a ready generation, and
// an invalid request means the envelope itself is out of bounds. Only the scope error carries
// tenancy meaning, so callers must not collapse them into one 400.
var (
	ErrEmptyDocumentQuery      = errors.New("documents: retrieval query is required")
	ErrInvalidDocumentScope    = errors.New("documents: document scope is invalid")
	ErrInvalidRetrievalRequest = errors.New("documents: retrieval request is invalid")
)

// RetrievalStatus reports which legs actually ran. A degraded retrieval is still a successful
// call — the cascade never fails a turn because ArcadeDB or the embedder is down — so this is
// the only signal that the answer is narrower than the profile name promises.
type RetrievalStatus string

// The status names the surviving legs; the degradation reason names the dependency that took
// the others out. They travel together because a thin result set caused by an offline embedder
// and one caused by an offline ArcadeDB need different operator responses.
const (
	RetrievalComplete    RetrievalStatus = "complete"
	RetrievalLexicalOnly RetrievalStatus = "degraded_lexical_only"
	RetrievalCardOnly    RetrievalStatus = "degraded_card_only"
	DegradationEmbedding                 = "query_embedding_unavailable"
	DegradationDense                     = "dense_retrieval_unavailable"
	DegradationArcade                    = "arcadedb_unavailable"
)

// RetrievalRequest is the query envelope. IdentityID is deliberately not decodable from JSON:
// the host stamps it from the authenticated turn, so a tool payload can never widen its own
// tenancy. An empty DocumentIDs means "every ready document", not "no document".
type RetrievalRequest struct {
	IdentityID  string   `json:"-"`
	Query       string   `json:"query"`
	Limit       int      `json:"limit,omitempty"`
	DocumentIDs []string `json:"document_ids,omitempty"`
}

// RetrievalResponse reports what ran. RejectedCandidates is retained in the wire shape and is
// now always zero: it counted candidates that failed control-plane revalidation, and there is
// no revalidation step to fail. It stays so a stored response from before the rewrite still
// decodes, and it is omitempty so a new one does not carry a meaningless field.
type RetrievalResponse struct {
	Query              string              `json:"query"`
	Profile            string              `json:"profile"`
	Status             RetrievalStatus     `json:"status"`
	DegradationReason  string              `json:"degradation_reason,omitempty"`
	RejectedCandidates int                 `json:"rejected_candidates,omitempty"`
	Documents          []RetrievalDocument `json:"documents"`
}

// RetrievalDocument is one document's share of the answer. RequiresOpen is set when the document
// carries no passage at all, which tells the agent to open the file rather than cite a snippet it
// does not hold; the card-only degradation forces it for every document.
type RetrievalDocument struct {
	DocumentID string  `json:"document_id"`
	Title      string  `json:"title"`
	Card       string  `json:"card,omitempty"`
	Score      float64 `json:"score"`
	// SourceKind and SourceKey are the route back to the bytes: "s3" plus the exact object
	// key. document_open takes the key and reads the object, which is the whole of what the
	// catalog's uuid -> version -> asset -> object chain used to do.
	SourceKind string `json:"source_kind,omitempty"`
	SourceKey  string `json:"source_key,omitempty"`
	// OriginalSHA256 is what a citation is pinned to now. VersionID, VersionNumber and
	// PipelineGeneration were here and are gone: nothing writes them, and a version number
	// only ever meant "which of this document's rows is current" -- a question that stops
	// existing once the bucket is the truth and an object simply has the bytes it has. The
	// digest answers the question that actually matters, which is whether the bytes a
	// citation quotes are still the bytes in the bucket.
	OriginalSHA256 string              `json:"original_sha256"`
	RequiresOpen   bool                `json:"requires_open"`
	Evidence       []RetrievalEvidence `json:"evidence"`
	Passages       []RetrievalPassage  `json:"passages"`
}

// PassageLocator anchors a passage inside the document it was cut from. It moved here
// verbatim when pipeline_types.go was deleted; retrieval is its only remaining consumer.
//
// MOST OF IT IS DEAD UNDER THE CURRENT WRITER, and that is not a reason to read the
// fields as merely absent. services/ingest/app.py says so outright -- "the layout fields
// stay None: this writer only ever extracts plain text, never structured layout" -- so
// self_ref, captions, page_number, bbox_*, sheet_name, table_name, row/column and
// cell_reference are ALWAYS null since the layout-aware converter was withdrawn. Only HeadingPath and
// CharStart/CharEnd carry data, from cocoindex's own Chunk offsets.
//
// It also duplicates arcadedb.PassageCandidate, which carries the same fields and is the
// schema authority services/ingest/arcade.py mirrors. Collapsing the two into one model
// and dropping the dead fields changes the DDL, the sidecar and document_search's wire
// shape together, so it belongs to the retrieval sub-project, not to this deletion.
type PassageLocator struct {
	SelfRef      string    `json:"self_ref,omitempty"`
	HeadingPath  []string  `json:"heading_path,omitempty"`
	Captions     []string  `json:"captions,omitempty"`
	PageNumber   *int      `json:"page_number,omitempty"`
	BoundingBox  []float64 `json:"bounding_box,omitempty"`
	CharStart    *int      `json:"char_start,omitempty"`
	CharEnd      *int      `json:"char_end,omitempty"`
	SheetName    string    `json:"sheet_name,omitempty"`
	TableName    string    `json:"table_name,omitempty"`
	RowNumber    *int      `json:"row_number,omitempty"`
	ColumnNumber *int      `json:"column_number,omitempty"`
	CellRef      string    `json:"cell_ref,omitempty"`
}

// RetrievalPassage is the passage as ArcadeDB holds it. It used to be the control plane's copy,
// substituted for the projection's by a revalidation step that no longer exists. The two digests
// travel with it so a citation cannot outlive the bytes it quotes: OriginalSHA256 pins the object
// and NormalizedSHA256 pins this passage's text within it.
type RetrievalPassage struct {
	PassageID        string              `json:"passage_id"`
	Ordinal          int64               `json:"ordinal"`
	Text             string              `json:"text"`
	CitationToken    string              `json:"citation_token"`
	CitationLocator  string              `json:"citation_locator"`
	Locator          PassageLocator      `json:"locator"`
	OriginalSHA256   string              `json:"original_sha256"`
	NormalizedSHA256 string              `json:"normalized_text_sha256"`
	Evidence         []RetrievalEvidence `json:"evidence"`
}

// RetrievalEvidence records why a document or passage is in the answer. Score and Distance are
// mutually exclusive and sort in opposite directions — a lexical Score is relevance where higher
// wins, a dense Distance is cosine distance where lower wins — so neither may be read as the other.
type RetrievalEvidence struct {
	Leg      string   `json:"leg"`
	Rank     int      `json:"rank"`
	Score    *float64 `json:"score,omitempty"`
	Distance *float64 `json:"distance,omitempty"`
}

// RetrievalCard is the control-plane digest row for a ready document. It backs the one leg that
// runs entirely inside Postgres, and is therefore the only leg still standing when the projection
// is unreachable. Rank is the raw ts_rank, unbounded and not comparable to a passage score until
// normalizedScore squashes it.
// RetrievalCard is one document's own description, as the reconciler recorded it.
//
// CatalogID, Tags and Digest were here and are gone with the catalog that supplied them:
// an object in a bucket has no uuid, no tags and no separate digest, so the fields could
// only ever have been empty, and document_search marshals this whole shape into the
// model's context.
type RetrievalCard struct {
	DocumentID string
	Title      string
	// The object coordinates travel with the card too, so a card-only answer -- the
	// degraded path taken when ArcadeDB's vector leg is down -- is still openable.
	SourceKind     string
	SourceKey      string
	Card           string
	Rank           float64
	OriginalSHA256 string
}

// RetrievalControlPlane bounds the scope and routes the cards, both from ArcadeDB.
//
// It used to be PostgreSQL. Both of its jobs are full-text work over records the
// reconciler writes, and ArcadeDB already indexes those records FULL_TEXT with the same
// analyzer it uses for passages -- so a PostgreSQL control plane meant two search engines
// tokenising one query differently. It no longer re-confirms candidates either.
//
// RevalidateDocumentCandidates was here, and it re-read every candidate from Postgres so the
// control plane's copy could supersede the projection's before anything was quotable. It is
// gone because what it validated against no longer exists: there is no second copy of the
// text in Postgres, no version to confirm a passage belongs to, and no generation to check
// it against. Its requirements had also become unsatisfiable -- it demanded uuid passage,
// document and version ids and an integer generation, while the reconciler writes
// "doc_<hex>:0", "doc_<hex>", nothing, and "cocoindex-v1" -- so it rejected every real row.
//
// What replaces it is carried by the passage: source_key locates the bytes and raw_sha256
// proves them. That is the shape every comparable system uses -- cognee's retrievers return
// the vector payload directly, and the string "revalidat" does not appear anywhere in its
// codebase.
type RetrievalControlPlane interface {
	ResolveDocumentScope(context.Context, string, []string) ([]string, error)
	RouteDocumentCards(context.Context, string, string, []string, int) ([]RetrievalCard, error)
	DocumentNames(context.Context, string, []string) (map[string]string, error)
}

// RetrievalProjection is the search index side of the cascade. It is trusted for candidate
// ordering only, never for content or tenancy, and both of its legs are advisory: an error from
// either degrades the answer instead of failing the call.
type RetrievalProjection interface {
	FusedCandidates(context.Context, arcadedb.FusedCandidateQuery) ([]arcadedb.PassageCandidate, error)
}

// RetrievalConfig bounds the cascade. Every non-positive field is replaced by a production
// default during normalization, so the zero value is a working configuration rather than a
// broken one — which also means a deliberate "no limit" cannot be expressed here.
type RetrievalConfig struct {
	CandidateLimit int
	MaxLimit       int
	MaxDocumentIDs int
	MaxQueryRunes  int
	TopPassages    int
	// FusionStrategy picks the engine's combination rule. RRF by default: it ranks by
	// position, so it is indifferent to the dense leg scoring a distance and the lexical
	// leg a Lucene score. LINEAR measured better on the 2026-08-08 pilot (0.900 vs 0.850)
	// but the manual reserves it for tuned weights, which one pilot is not.
	FusionStrategy arcadedb.FusionStrategy
}

// HostRetriever runs the cascade in-process. Projection and Embedder are optional on purpose —
// a nil one degrades the answer along the documented status/reason pair — but a nil ControlPlane
// is a wiring bug and fails the call, because without it nothing can be revalidated.
type HostRetriever struct {
	ControlPlane RetrievalControlPlane
	Projection   RetrievalProjection
	Embedder     embeddings.Embedder
	Config       RetrievalConfig
}

// Retrieve runs the card, lexical and dense legs and returns only passages the control plane
// re-confirmed. A missing or failing projection dependency degrades the answer rather than
// failing it; only a malformed request or a control-plane error returns a non-nil error.
func (r *HostRetriever) Retrieve(ctx context.Context, request RetrievalRequest) (RetrievalResponse, error) {
	if r == nil {
		return RetrievalResponse{}, fmt.Errorf("documents: retriever is not configured")
	}
	request, cfg, err := normalizeRetrievalRequest(request, r.Config)
	if err != nil {
		return RetrievalResponse{}, err
	}
	if r.ControlPlane == nil {
		return RetrievalResponse{}, fmt.Errorf("documents: retrieval control plane is not configured")
	}
	scope, err := r.ControlPlane.ResolveDocumentScope(ctx, request.IdentityID, request.DocumentIDs)
	if err != nil {
		return RetrievalResponse{}, err
	}
	cards, err := r.ControlPlane.RouteDocumentCards(
		ctx, request.IdentityID, request.Query, scope, cfg.CandidateLimit,
	)
	if err != nil {
		return RetrievalResponse{}, fmt.Errorf("documents: route document cards: %w", err)
	}
	response := RetrievalResponse{
		Query: request.Query, Profile: ProductionRetrievalProfile,
		Status: RetrievalComplete, Documents: []RetrievalDocument{},
	}
	if r.Projection == nil {
		response.Status, response.DegradationReason = RetrievalCardOnly, DegradationArcade
		response.Documents = rankCardsOnly(cards, request.Limit, cfg.TopPassages)
		return response, nil
	}
	// The embedding comes before the index read because there is one read: the engine
	// needs both the vector and the terms to fuse them. Without an embedding there is
	// nothing to fuse, so the answer degrades to the cards rather than to a second,
	// hand-reconciled ranking -- that reconciliation is the thing being removed.
	vectors, embedErr := r.embedQuery(ctx, request.Query)
	if embedErr != nil {
		response.Status, response.DegradationReason = RetrievalCardOnly, DegradationEmbedding
		response.Documents = rankCardsOnly(cards, request.Limit, cfg.TopPassages)
		return response, nil
	}
	fused, err := r.Projection.FusedCandidates(ctx, arcadedb.FusedCandidateQuery{
		CandidateFilter: arcadedb.CandidateFilter{
			IdentityID: request.IdentityID, Limit: cfg.CandidateLimit, DocumentIDs: scope,
		},
		Query: request.Query, Embedding: vectors, Strategy: cfg.FusionStrategy,
	})
	if err != nil {
		response.Status, response.DegradationReason = RetrievalCardOnly, DegradationArcade
		response.Documents = rankCardsOnly(cards, request.Limit, cfg.TopPassages)
		return response, nil
	}
	response.Documents = rankDocuments(
		cards, fused, r.passageLegNames(ctx, request.IdentityID, cards, fused),
		request.Limit, cfg.TopPassages, false,
	)
	return response, nil
}

// passageLegNames resolves display names for the documents ONLY the passage leg produced.
//
// The card leg already carries the name of everything it ranked, so asking for those again
// would be a second answer to a question already answered. What is left are the hits with
// no card in the ranking, which is where the name is missing entirely.
//
// A failure here is silenced deliberately, and it is the one silence in this cascade that
// does not set a DegradationReason: the passages are intact and the answer is correct, only
// its titles fall back to the key -- which is the documented behaviour for an object that
// never carried a name. Degrading the whole response would be a worse answer, not a safer
// one.
func (r *HostRetriever) passageLegNames(
	ctx context.Context,
	identityID string,
	cards []RetrievalCard,
	passages []arcadedb.PassageCandidate,
) map[string]string {
	carded := make(map[string]struct{}, len(cards))
	for _, card := range cards {
		carded[card.DocumentID] = struct{}{}
	}
	missing := make([]string, 0, len(passages))
	seen := make(map[string]struct{}, len(passages))
	for _, passage := range passages {
		id := passage.SearchDocumentID
		if _, ranked := carded[id]; ranked {
			continue
		}
		if _, already := seen[id]; already {
			continue
		}
		seen[id] = struct{}{}
		missing = append(missing, id)
	}
	if len(missing) == 0 {
		return nil
	}
	names, err := r.ControlPlane.DocumentNames(ctx, identityID, missing)
	if err != nil {
		return nil
	}
	return names
}

func (r *HostRetriever) embedQuery(ctx context.Context, query string) ([]float64, error) {
	if r.Embedder == nil {
		return nil, fmt.Errorf("documents: retrieval embedder is not configured")
	}
	vectors, err := r.Embedder.Embed(ctx, embeddings.RetrievalQueries([]string{query}))
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("documents: embedder returned %d vectors, want 1", len(vectors))
	}
	return vectors[0], nil
}

func normalizeRetrievalRequest(request RetrievalRequest, cfg RetrievalConfig) (RetrievalRequest, RetrievalConfig, error) {
	cfg = normalizedRetrievalConfig(cfg)
	request.IdentityID = strings.TrimSpace(request.IdentityID)
	request.Query = strings.TrimSpace(request.Query)
	if request.IdentityID == "" {
		return RetrievalRequest{}, cfg, fmt.Errorf("%w: identity is required", ErrInvalidRetrievalRequest)
	}
	if request.Query == "" {
		return RetrievalRequest{}, cfg, ErrEmptyDocumentQuery
	}
	if utf8.RuneCountInString(request.Query) > cfg.MaxQueryRunes {
		return RetrievalRequest{}, cfg, fmt.Errorf(
			"%w: query exceeds %d characters", ErrInvalidRetrievalRequest, cfg.MaxQueryRunes,
		)
	}
	if request.Limit == 0 {
		request.Limit = 8
	}
	if request.Limit < 1 || request.Limit > cfg.MaxLimit {
		return RetrievalRequest{}, cfg, fmt.Errorf(
			"%w: limit must be between 1 and %d", ErrInvalidRetrievalRequest, cfg.MaxLimit,
		)
	}
	if len(request.DocumentIDs) > cfg.MaxDocumentIDs {
		return RetrievalRequest{}, cfg, fmt.Errorf(
			"%w: scope exceeds %d ids", ErrInvalidDocumentScope, cfg.MaxDocumentIDs,
		)
	}
	seen := make(map[string]struct{}, len(request.DocumentIDs))
	ids := make([]string, 0, len(request.DocumentIDs))
	for _, id := range request.DocumentIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return RetrievalRequest{}, cfg, fmt.Errorf("%w: blank document id", ErrInvalidDocumentScope)
		}
		if _, ok := seen[id]; !ok {
			seen[id], ids = struct{}{}, append(ids, id)
		}
	}
	sort.Strings(ids)
	request.DocumentIDs = ids
	return request, cfg, nil
}

func normalizedRetrievalConfig(cfg RetrievalConfig) RetrievalConfig {
	if cfg.CandidateLimit <= 0 {
		cfg.CandidateLimit = 200
	}
	if cfg.MaxLimit <= 0 {
		cfg.MaxLimit = 50
	}
	if cfg.MaxDocumentIDs <= 0 {
		cfg.MaxDocumentIDs = 100
	}
	if cfg.MaxQueryRunes <= 0 {
		cfg.MaxQueryRunes = 2048
	}
	if cfg.TopPassages <= 0 {
		cfg.TopPassages = 3
	}
	if cfg.FusionStrategy == "" {
		cfg.FusionStrategy = arcadedb.FusionRRF
	}
	return cfg
}

func citationLocator(candidate arcadedb.PassageCandidate) string {
	parts := make([]string, 0, 5)
	add := func(name, value string) {
		if value != "" {
			parts = append(parts, name+"="+url.QueryEscape(value))
		}
	}
	add("ref", candidate.SelfRef)
	if candidate.PageNumber != nil {
		parts = append(parts, "page="+strconv.FormatInt(*candidate.PageNumber, 10))
	}
	add("sheet", candidate.SheetName)
	add("table", candidate.TableName)
	add("cell", candidate.CellReference)
	if candidate.RowNumber != nil {
		parts = append(parts, "row="+strconv.FormatInt(*candidate.RowNumber, 10))
	}
	if candidate.ColumnNumber != nil {
		parts = append(parts, "column="+strconv.FormatInt(*candidate.ColumnNumber, 10))
	}
	if len(parts) == 0 {
		parts = append(parts, "ordinal="+strconv.FormatInt(candidate.Ordinal, 10))
	}
	return strings.Join(parts, ";")
}
