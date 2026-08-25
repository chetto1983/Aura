package documents

import (
	"context"
	"errors"
	"fmt"
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
// means a named document is not visible to this identity, and an invalid request means the
// envelope itself is out of bounds. Only the scope error carries
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
	RetrievalCardOnly    RetrievalStatus = "degraded_card_only"
	DegradationEmbedding                 = "query_embedding_unavailable"
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

// RetrievalResponse reports which production legs ran and what they returned.
type RetrievalResponse struct {
	Query             string              `json:"query"`
	Profile           string              `json:"profile"`
	Status            RetrievalStatus     `json:"status"`
	DegradationReason string              `json:"degradation_reason,omitempty"`
	Documents         []RetrievalDocument `json:"documents"`
}

// RetrievalDocument is one document's share of the answer. RequiresOpen is set when the document
// carries no passage at all, which tells the agent to open the file rather than cite a snippet it
// does not hold; the card-only degradation forces it for every document.
type RetrievalDocument struct {
	DocumentID string  `json:"document_id"`
	Title      string  `json:"title"`
	Card       string  `json:"card,omitempty"`
	Score      float64 `json:"score"`
	// SourceKind and SourceKey route document_open back to the exact object bytes.
	SourceKind string `json:"source_kind,omitempty"`
	SourceKey  string `json:"source_key,omitempty"`
	// OriginalSHA256 pins citations to the object bytes they quote.
	OriginalSHA256 string              `json:"original_sha256"`
	RequiresOpen   bool                `json:"requires_open"`
	Evidence       []RetrievalEvidence `json:"evidence"`
	Passages       []RetrievalPassage  `json:"passages"`
}

// PassageLocator anchors a passage in the plain text produced by the current extractor.
type PassageLocator struct {
	HeadingPath []string `json:"heading_path,omitempty"`
	CharStart   *int     `json:"char_start,omitempty"`
	CharEnd     *int     `json:"char_end,omitempty"`
}

// RetrievalPassage is the passage as ArcadeDB holds it. OriginalSHA256 pins the object and
// NormalizedSHA256 pins this passage's text within it.
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

// RetrievalEvidence records which ranked production leg admitted the result.
type RetrievalEvidence struct {
	Leg   string   `json:"leg"`
	Rank  int      `json:"rank"`
	Score *float64 `json:"score,omitempty"`
}

// RetrievalCard is one document's own description, as the reconciler recorded it.
type RetrievalCard struct {
	DocumentID string
	Title      string
	// The object coordinates keep a card-only answer openable.
	SourceKind     string
	SourceKey      string
	Card           string
	Rank           float64
	OriginalSHA256 string
}

// RetrievalControlPlane bounds identity scope and routes IndexedDocument cards in ArcadeDB.
type RetrievalControlPlane interface {
	ResolveDocumentScope(context.Context, string, []string) ([]string, error)
	RouteDocumentCards(context.Context, string, string, []string, int) ([]RetrievalCard, error)
	DocumentNames(context.Context, string, []string) (map[string]string, error)
}

// PassageIndex reads the fused lexical/vector passage ranking from the identity database.
type PassageIndex interface {
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

// HostRetriever runs the cascade in-process. PassageIndex and Embedder may degrade to cards;
// ControlPlane is required because it owns identity scope and document metadata.
type HostRetriever struct {
	ControlPlane RetrievalControlPlane
	PassageIndex PassageIndex
	Embedder     embeddings.Embedder
	Config       RetrievalConfig
}

// Retrieve runs the document-card and fused-passage legs. A missing or failing passage index
// degrades the answer; malformed requests and control-plane failures remain hard errors.
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
	if r.PassageIndex == nil {
		response.Status, response.DegradationReason = RetrievalCardOnly, DegradationArcade
		response.Documents = rankCardsOnly(cards, request.Limit, cfg.TopPassages)
		return response, nil
	}
	// The embedding comes before the index read because there is one read: the engine
	// needs both the vector and the terms to fuse them. Without an embedding there is
	// nothing to fuse, so the answer degrades to the cards rather than to a second,
	// hand-reconciled ranking.
	vectors, embedErr := r.embedQuery(ctx, request.Query)
	if embedErr != nil {
		response.Status, response.DegradationReason = RetrievalCardOnly, DegradationEmbedding
		response.Documents = rankCardsOnly(cards, request.Limit, cfg.TopPassages)
		return response, nil
	}
	fused, err := r.PassageIndex.FusedCandidates(ctx, arcadedb.FusedCandidateQuery{
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
	if candidate.CharacterSpan != nil {
		return "chars=" + strconv.FormatInt(candidate.CharacterSpan.Start, 10) + "-" +
			strconv.FormatInt(candidate.CharacterSpan.End, 10)
	}
	return "ordinal=" + strconv.FormatInt(candidate.Ordinal, 10)
}
