package documents

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
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
	// DegradationUnconfigured is the deployment that never wired a passage index — a
	// different operator response from an index that is wired and refusing, which is
	// why it stopped sharing DegradationArcade's name (measured 2026-09-06: the two
	// were indistinguishable on the wire and neither was logged).
	DegradationUnconfigured = "passage_index_unconfigured"

	// AbstainedNoQualifiedPassage is the healthy cascade reporting that the corpus has
	// nothing to say. It is NOT a degradation: every leg ran, and the dense floor let
	// nothing through, which is the only way a vector index can ever answer "no" --
	// vector.neighbors returns its k nearest however far away they are.
	AbstainedNoQualifiedPassage = "no_qualified_passage"
)

// RetrievalRequest is the query envelope. IdentityID is deliberately not decodable from JSON:
// the host stamps it from the authenticated turn, so a tool payload can never widen its own
// tenancy. An empty DocumentIDs means "every ready document", not "no document".
type RetrievalRequest struct {
	IdentityID   string        `json:"-"`
	Query        string        `json:"query"`
	Limit        int           `json:"limit,omitempty"`
	DocumentIDs  []string      `json:"document_ids,omitempty"`
	SourceScopes []SourceScope `json:"-"`
	// Neighbours asks for the passages either side of every hit, by ordinal. A chunk
	// boundary can cut a table or a definition in half, and the half that answers the
	// question is then unreachable: the neighbour is by construction the passage that did
	// NOT match, so no rephrasing of the query reaches it and the only escape is opening
	// the whole file. Zero, the default, costs nothing.
	Neighbours int `json:"neighbours,omitempty"`
}

// RetrievalResponse reports which production legs ran and what they returned.
type RetrievalResponse struct {
	Query             string          `json:"query"`
	Profile           string          `json:"profile"`
	Status            RetrievalStatus `json:"status"`
	DegradationReason string          `json:"degradation_reason,omitempty"`
	// Abstained says the corpus could not answer, so the caller must not treat the empty
	// document list as a retrieval failure -- and must not answer from its own knowledge
	// as though the library had agreed. It mirrors memory_search's own contract.
	Abstained        bool   `json:"abstained"`
	AbstentionReason string `json:"abstention_reason,omitempty"`

	Documents []RetrievalDocument `json:"documents"`
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
	OriginalSHA256 string `json:"original_sha256"`
	// What the reconciler recorded about the OBJECT, as opposed to about the answer.
	// Present only when the card leg ranked this document: one found by its passages alone
	// has no card row behind it, and reporting passage_count 0 for a document that plainly
	// has passages would be worse than reporting nothing.
	//
	// SizeBytes and IndexedAt are what tell two documents with the SAME file name apart,
	// which this corpus holds: measured 2026-09-09, two meteo_caraglio_settimanale_verificato.docx
	// of 9028 and 7712 bytes disagreeing on the forecast, arriving with identical titles and
	// scores 0.014 apart. Retrieval ranks topical similarity and cannot know which is true;
	// what it CAN do is stop hiding the fields that let the reader decide.
	//
	// PassageCount is how many passages the whole document has, against the few Passages
	// carries, so "the answer may be elsewhere in this file" is visible rather than guessed.
	SizeBytes    *int64     `json:"size_bytes,omitempty"`
	PassageCount *int64     `json:"passage_count,omitempty"`
	IndexedAt    *time.Time `json:"indexed_at,omitempty"`

	RequiresOpen bool                `json:"requires_open"`
	Evidence     []RetrievalEvidence `json:"evidence"`
	Passages     []RetrievalPassage  `json:"passages"`
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
	// The passages either side of this one in the same document, nearest first, present
	// only when the caller asked for them. They were not ranked and carry no score: they
	// are here because they are ADJACENT, which is a different claim from being relevant.
	ContextBefore []PassageContext `json:"context_before,omitempty"`
	ContextAfter  []PassageContext `json:"context_after,omitempty"`
}

// PassageContext is a neighbouring passage. It carries its own citation token because
// quoting it under the token of the passage it neighbours would cite text that passage
// does not contain -- and its own Locator for the same reason. document_search tells its
// caller to cite the citation_token AND the locator's heading_path, so a neighbour that
// carried only the token asked for a citation half of which it had not been given;
// measured 2026-09-09, the answer to a question about a table split across chunks lives
// in the neighbour, which is exactly when the missing half is the one needed.
type PassageContext struct {
	PassageID     string         `json:"passage_id"`
	Ordinal       int64          `json:"ordinal"`
	Text          string         `json:"text"`
	CitationToken string         `json:"citation_token"`
	Locator       PassageLocator `json:"locator"`
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
	// NormalizedSHA256 hashes the extracted text, so two copies stored in different
	// containers collapse even when neither returned a passage.
	NormalizedSHA256 string
	SizeBytes        int64
	PassageCount     int64
	IndexedAt        time.Time
}

// RetrievalControlPlane bounds identity scope and routes IndexedDocument cards in ArcadeDB.
type RetrievalControlPlane interface {
	ResolveDocumentScope(context.Context, string, []string) ([]string, error)
	RouteDocumentCards(
		context.Context, string, string, []float64, []string, []SourceScope, int,
	) ([]RetrievalCard, error)
	DocumentNames(context.Context, string, []string) (map[string]string, error)
}

// PassageIndex reads the fused lexical/vector passage ranking from the identity database,
// and the unranked passages a caller names by position.
type PassageIndex interface {
	FusedCandidates(context.Context, arcadedb.FusedCandidateQuery) ([]arcadedb.PassageCandidate, error)
	PassagesAt(context.Context, string, []arcadedb.PassageRef) ([]arcadedb.PassageCandidate, error)
}

// MaxRetrievalNeighbours bounds the context a caller can pull around every hit. Each one is
// a whole passage of text, so the ceiling is what keeps a limit of 20 from returning 20x7
// passages to a model that asked for the two lines a table header sat on.
const MaxRetrievalNeighbours = 3

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
	// degradations rate-limits the WARN that names why a leg was lost (see
	// retrieval_degradation.go). The zero value logs every distinct cause once per
	// degradationRestateAfter, so a retriever built by any caller is already vocal.
	degradations degradationLog
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
	// ResolveDocumentScope uses an empty slice for the intentionally unscoped path. If the
	// caller DID name ids and none survive owner resolution, treating that same empty slice
	// as unscoped would widen a failed filter to the owner's whole corpus.
	if len(request.DocumentIDs) > 0 && len(scope) == 0 {
		return RetrievalResponse{}, fmt.Errorf("%w: no named document is visible", ErrInvalidDocumentScope)
	}
	response := RetrievalResponse{
		Query: request.Query, Profile: ProductionRetrievalProfile,
		Status: RetrievalComplete, Documents: []RetrievalDocument{},
	}
	// The embedding now comes before BOTH legs, because both are scored against it: the
	// card leg ranks a description by the same reranked cosine the passage leg ranks text
	// by, which is what lets one be weighed against the other at all. Without a vector
	// there is no comparable score for either, so this degrades with no documents rather
	// than answering from an unranked card list.
	vectors, embedErr := r.embedQuery(ctx, request.Query)
	if embedErr != nil {
		response.Status, response.DegradationReason = RetrievalCardOnly, DegradationEmbedding
		r.degradations.warn(DegradationEmbedding, embedErr.Error(), request.IdentityID)
		return response, nil
	}
	cards, err := r.ControlPlane.RouteDocumentCards(
		ctx, request.IdentityID, request.Query, vectors, scope, request.SourceScopes,
		cfg.CandidateLimit,
	)
	if err != nil {
		return RetrievalResponse{}, fmt.Errorf("documents: route document cards: %w", err)
	}
	if r.PassageIndex == nil {
		response.Status, response.DegradationReason = RetrievalCardOnly, DegradationUnconfigured
		r.degradations.warn(DegradationUnconfigured, "no passage index is wired", request.IdentityID)
		response.Documents = rankCardsOnly(cards, request.Limit, cfg.TopPassages)
		return response, nil
	}
	sourceKeys, sourcePrefixes := ArcadeSourceFilters(request.SourceScopes)
	fused, err := r.PassageIndex.FusedCandidates(ctx, arcadedb.FusedCandidateQuery{
		CandidateFilter: arcadedb.CandidateFilter{
			IdentityID: request.IdentityID, Limit: cfg.CandidateLimit, DocumentIDs: scope,
			SourceKeys: sourceKeys, SourcePrefixes: sourcePrefixes,
		},
		Query: request.Query, Embedding: vectors, Strategy: cfg.FusionStrategy,
	})
	if err != nil {
		response.Status, response.DegradationReason = RetrievalCardOnly, DegradationArcade
		r.degradations.warn(DegradationArcade, err.Error(), request.IdentityID)
		response.Documents = rankCardsOnly(cards, request.Limit, cfg.TopPassages)
		return response, nil
	}
	// Abstain when NEITHER leg qualified, not when the passage leg alone came back empty.
	//
	// The rule used to be "no passage, no answer", written when a card was ranked by BM25
	// and a passage by a cosine, so a card could not be weighed against anything and
	// letting it answer by itself is how "ricetta della carbonara" came back with three
	// worker reports. Both legs now score the same reranked cosine and BOTH are cut by the
	// same RelevanceFloor, so a card that survived it is qualified evidence -- it says
	// which FILE knows the answer, and requires_open says how to read it.
	//
	// Keeping the old rule silently deleted the only kind of document that can never have
	// a passage: a spreadsheet is routed to document_open on purpose and carries none.
	// Measured 2026-09-09 on the live corpus, "elenco dei comuni italiani con CAP e codice
	// ISTAT" abstained outright while gi_comuni_cap.xlsx sat in the card leg -- and the
	// same question asked with a place name in it did return the file, because unrelated
	// weather documents happened to qualify and carried the cards along with them.
	if len(fused) == 0 {
		if len(cards) == 0 {
			response.Abstained, response.AbstentionReason = true, AbstainedNoQualifiedPassage
			return response, nil
		}
		// No passage was retrieved, so nothing here can be quoted: every answer must be
		// opened. That is rankCardsOnly's contract, and it is not a degradation -- both
		// legs ran and one of them found something.
		response.Documents = rankCardsOnly(cards, request.Limit, cfg.TopPassages)
		return response, nil
	}
	response.Documents = rankDocuments(
		cards, fused, r.passageLegNames(ctx, request.IdentityID, cards, fused),
		request.Limit, cfg.TopPassages, false,
	)
	r.attachNeighbours(ctx, request.IdentityID, response.Documents, request.Neighbours)
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
		// The answer survives without names — the passages and their ids still rank — so
		// this is not a degradation of the RESULT and does not get the status field. It is
		// still a dependency failing, and it goes through the same rate-limited log as the
		// leg degradations rather than vanishing.
		r.degradations.warn("passage_names_unavailable", err.Error(), identityID)
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
	if request.Neighbours < 0 || request.Neighbours > MaxRetrievalNeighbours {
		return RetrievalRequest{}, cfg, fmt.Errorf(
			"%w: neighbours must be between 0 and %d",
			ErrInvalidRetrievalRequest, MaxRetrievalNeighbours,
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
	sourceScopes, err := NormalizeSourceScopes(request.SourceScopes)
	if err != nil {
		return RetrievalRequest{}, cfg, fmt.Errorf("%w: %v", ErrInvalidDocumentScope, err)
	}
	request.SourceScopes = sourceScopes
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
