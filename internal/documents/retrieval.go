package documents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
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
const ProductionRetrievalProfile = "lexical-dense-card-cascade-v1"

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

// RetrievalResponse reports what was thrown away as well as what was kept: RejectedCandidates
// counts projection candidates that failed control-plane revalidation. A non-zero count is a
// staleness signal about ArcadeDB, not a sign that the query was bad.
type RetrievalResponse struct {
	Query              string              `json:"query"`
	Profile            string              `json:"profile"`
	Status             RetrievalStatus     `json:"status"`
	DegradationReason  string              `json:"degradation_reason,omitempty"`
	RejectedCandidates int                 `json:"rejected_candidates,omitempty"`
	Documents          []RetrievalDocument `json:"documents"`
}

// RetrievalDocument is one document's share of the answer. RequiresOpen is set when no passage
// survived revalidation, which tells the agent to open the file rather than cite a snippet it
// does not actually hold; the card-only degradation forces it for every document.
type RetrievalDocument struct {
	DocumentID         string              `json:"document_id"`
	CatalogID          string              `json:"catalog_id"`
	Title              string              `json:"title"`
	Tags               []string            `json:"tags,omitempty"`
	Digest             string              `json:"digest,omitempty"`
	Card               string              `json:"card,omitempty"`
	Score              float64             `json:"score"`
	RequiresOpen       bool                `json:"requires_open"`
	VersionID          string              `json:"version_id"`
	VersionNumber      int64               `json:"version_number"`
	OriginalSHA256     string              `json:"original_sha256"`
	PipelineGeneration int64               `json:"pipeline_generation"`
	Evidence           []RetrievalEvidence `json:"evidence"`
	Passages           []RetrievalPassage  `json:"passages"`
}

// PassageLocator anchors a passage inside the document it was cut from. It moved here
// verbatim when pipeline_types.go was deleted; retrieval is its only remaining consumer.
//
// MOST OF IT IS DEAD UNDER THE CURRENT WRITER, and that is not a reason to read the
// fields as merely absent. services/ingest/app.py says so outright -- "the layout fields
// stay None: this writer only ever extracts plain text, never structured layout" -- so
// self_ref, captions, page_number, bbox_*, sheet_name, table_name, row/column and
// cell_reference are ALWAYS null since Docling was withdrawn. Only HeadingPath and
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

// RetrievalPassage carries the control-plane copy of the text, never the projection's: the
// revalidation step replaces the candidate wholesale. The version, original digest and
// normalized-text digest travel with it so a citation cannot outlive the bytes it quotes.
type RetrievalPassage struct {
	PassageID        string              `json:"passage_id"`
	Ordinal          int64               `json:"ordinal"`
	Text             string              `json:"text"`
	CitationToken    string              `json:"citation_token"`
	CitationLocator  string              `json:"citation_locator"`
	Locator          PassageLocator      `json:"locator"`
	VersionID        string              `json:"version_id"`
	VersionNumber    int64               `json:"version_number"`
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
type RetrievalCard struct {
	CatalogID          string
	DocumentID         string
	Title              string
	Tags               []string
	Digest             string
	Card               string
	Rank               float64
	VersionID          string
	VersionNumber      int64
	OriginalSHA256     string
	PipelineGeneration int64
}

// RetrievalControlPlane is the sole authority on what exists: it bounds the scope, routes the
// cards, and re-confirms every projection candidate against the live version before a word of it
// reaches the caller. Nothing the projection returns is quotable until it has passed through here.
type RetrievalControlPlane interface {
	ResolveDocumentScope(context.Context, string, []string) ([]string, error)
	RouteDocumentCards(context.Context, string, string, []string, int) ([]RetrievalCard, error)
	RevalidateDocumentCandidates(context.Context, string, []arcadedb.PassageCandidate) (map[string]RevalidatedCandidate, error)
}

// RevalidatedCandidate is the control plane's own card and passage for a candidate coordinate.
// It supersedes the projection's copy rather than annotating it; only the retrieval leg and its
// scores are carried back over from the candidate that proposed it.
type RevalidatedCandidate struct {
	Card    RetrievalCard
	Passage arcadedb.PassageCandidate
}

// RetrievalProjection is the search index side of the cascade. It is trusted for candidate
// ordering only, never for content or tenancy, and both of its legs are advisory: an error from
// either degrades the answer instead of failing the call.
type RetrievalProjection interface {
	LexicalCandidates(context.Context, arcadedb.LexicalCandidateQuery) ([]arcadedb.PassageCandidate, error)
	DenseCandidates(context.Context, arcadedb.DenseCandidateQuery) ([]arcadedb.PassageCandidate, error)
}

// RetrievalConfig bounds the cascade. Every non-positive field is replaced by a production
// default during normalization, so the zero value is a working configuration rather than a
// broken one — which also means a deliberate "no limit" cannot be expressed here.
type RetrievalConfig struct {
	CandidateLimit   int
	MaxLimit         int
	MaxDocumentIDs   int
	MaxQueryRunes    int
	TopPassages      int
	DenseMaxDistance float64
	LexicalMinScore  float64
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
		response.Documents = rankDocuments(cards, nil, request.Limit, cfg.TopPassages, true)
		return response, nil
	}
	filter := arcadedb.CandidateFilter{
		IdentityID: request.IdentityID, Limit: cfg.CandidateLimit, DocumentIDs: scope,
	}
	lexical, err := r.Projection.LexicalCandidates(ctx, arcadedb.LexicalCandidateQuery{
		CandidateFilter: filter, Query: request.Query,
	})
	if err != nil {
		response.Status, response.DegradationReason = RetrievalCardOnly, DegradationArcade
		response.Documents = rankDocuments(cards, nil, request.Limit, cfg.TopPassages, true)
		return response, nil
	}
	lexical = admittedLexical(lexical, request.Query, cfg.LexicalMinScore)
	candidates := lexical
	if r.Embedder == nil {
		response.Status, response.DegradationReason = RetrievalLexicalOnly, DegradationEmbedding
	} else {
		vectors, embedErr := r.Embedder.Embed(ctx, embeddings.RetrievalQueries([]string{request.Query}))
		if embedErr != nil || len(vectors) != 1 {
			response.Status, response.DegradationReason = RetrievalLexicalOnly, DegradationEmbedding
		} else {
			dense, denseErr := r.Projection.DenseCandidates(ctx, arcadedb.DenseCandidateQuery{
				CandidateFilter: filter, Embedding: vectors[0],
			})
			if denseErr != nil {
				response.Status, response.DegradationReason = RetrievalLexicalOnly, DegradationDense
			} else {
				candidates = append(candidates, admittedDense(dense, cfg.DenseMaxDistance)...)
			}
		}
	}
	validated, err := r.ControlPlane.RevalidateDocumentCandidates(ctx, request.IdentityID, candidates)
	if err != nil {
		return RetrievalResponse{}, fmt.Errorf("documents: revalidate retrieval candidates: %w", err)
	}
	accepted := make([]validatedPassage, 0, len(candidates))
	for _, candidate := range candidates {
		if authoritative, ok := validated[candidateCoordinateKey(candidate)]; ok {
			authoritative.Passage.Leg = candidate.Leg
			authoritative.Passage.LexicalScore = candidate.LexicalScore
			authoritative.Passage.DenseDistance = candidate.DenseDistance
			accepted = append(accepted, validatedPassage{
				candidate: authoritative.Passage, card: authoritative.Card,
			})
		} else {
			response.RejectedCandidates++
		}
	}
	response.Documents = rankDocuments(cards, accepted, request.Limit, cfg.TopPassages, false)
	return response, nil
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
	if cfg.DenseMaxDistance <= 0 {
		cfg.DenseMaxDistance = 0.55
	}
	if cfg.LexicalMinScore < 0 {
		cfg.LexicalMinScore = 0
	}
	return cfg
}

func admittedLexical(candidates []arcadedb.PassageCandidate, query string, minimum float64) []arcadedb.PassageCandidate {
	multiTerm := len(strings.Fields(query)) > 1
	return filterCandidates(candidates, func(candidate arcadedb.PassageCandidate) bool {
		return candidate.LexicalScore != nil && *candidate.LexicalScore > 0 && (!multiTerm || *candidate.LexicalScore >= minimum)
	})
}

func admittedDense(candidates []arcadedb.PassageCandidate, maximum float64) []arcadedb.PassageCandidate {
	return filterCandidates(candidates, func(candidate arcadedb.PassageCandidate) bool {
		return candidate.DenseDistance != nil && *candidate.DenseDistance <= maximum
	})
}

func filterCandidates(candidates []arcadedb.PassageCandidate, keep func(arcadedb.PassageCandidate) bool) []arcadedb.PassageCandidate {
	out := make([]arcadedb.PassageCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if keep(candidate) {
			out = append(out, candidate)
		}
	}
	return out
}

type validatedPassage struct {
	candidate arcadedb.PassageCandidate
	card      RetrievalCard
}

func candidateCoordinateKey(candidate arcadedb.PassageCandidate) string {
	hash := sha256.New()
	for _, part := range []string{candidate.PassageID, candidate.DocumentID, candidate.SearchDocumentID,
		candidate.VersionID, strconv.FormatInt(candidate.VersionNumber, 10), candidate.PipelineGeneration,
		strconv.FormatInt(candidate.Ordinal, 10), candidate.RawSHA256, candidate.NormalizedSHA256} {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
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

func normalizedScore(value float64) float64 {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value / (1 + value)
}
