package arcadedb

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The dense leg of memory retrieval, and the reason it exists.
//
// Retrieval here was Lucene full-text plus a graph walk — exact, one round trip,
// no sidecar. Measured on 24 real facts, that has one failure it cannot be tuned
// out of: a question in one language cannot reach a fact written in another.
// `analyzer recall Italian English` returned the right fact first;
// `ricerca testuale italiano inglese` returned nothing, because the facts are
// written in English and the operator asks in Italian.
//
// EmbeddingGemma-300M closes exactly that gap — measured on the live sidecar,
// "cliente a Torino" against "customer in Turin" is 0.8698 while an unrelated
// sentence is 0.5113.
//
// Everything here is ArcadeDB's own: an LSM_VECTOR index (HNSW), vector.neighbors
// for the dense search, and vector.fuse for reciprocal rank fusion across the two
// legs. No fusion arithmetic in Go — the database ships it, and a hand-rolled one
// is a second thing to tune and get wrong.
const (
	// vectorDimensions is EmbeddingGemma-300M's native width. It is part of the
	// index definition, so changing the embedder means rebuilding the index; the
	// index refuses a query vector of any other length, which is the failure you
	// want rather than silent nonsense.
	vectorDimensions = 768

	// NONE, not INT8. The INT8 recommendation is for 10K-1M vectors; the manual
	// says to omit quantization "for very small datasets (< 10K vectors) where
	// maximum precision matters", and a personal memory is squarely there — it
	// holds tens to thousands of facts, not a document corpus. Copying the
	// production default at this scale trades the accuracy that matters most for
	// a memory saving that is measured in kilobytes.
	vectorQuantization = "NONE"
)

// vectorSchemaStatements extend the memory schema with the dense leg. They are
// separate from memorySchemaStatements only for reading: EnsureMemorySchema runs
// both, and an index on an edge type is legal (verified against 26.7.3, which
// indexed the existing 24 facts on creation).
func vectorSchemaStatements() []string {
	return append([]string{
		// ARRAY_OF_FLOATS, not LIST OF FLOAT. The manual shows both, but they are not
		// interchangeable at the binding: a JSON array sent through the SQL endpoint
		// arrives as ARRAY_OF_FLOATS, and a property declared LIST OF FLOAT rejects it
		// with "declared as LIST of 'FLOAT' but a value of type 'ARRAY_OF_FLOATS' is
		// used". The declaration follows the wire, not the prose.
		"CREATE PROPERTY " + factEdgeType + ".embedding IF NOT EXISTS ARRAY_OF_FLOATS",
		"CREATE INDEX IF NOT EXISTS ON " + factEdgeType + " (embedding) LSM_VECTOR METADATA " +
			"{ \"dimensions\": " + strconv.Itoa(vectorDimensions) +
			", \"similarity\": \"COSINE\", \"quantization\": \"" + vectorQuantization + "\" }",
	}, spaceStampStatements(factEdgeType)...)
}

// rerankOpen/rerankClose wrap a fused ranking in ArcadeDB's native
// `vector.rerank`, which re-scores the fused candidates against their
// full-precision vectors and emits a real cosine `score` (the RRF pseudo-score
// it replaces was 1/(60+rank), identical for every source's rank 1).
//
// This is the ORDERING half of retrieval; maxDistance above is the RECALL half.
// RRF ranks only, so one incidental lexical hit at rank 1 ties with the correct
// dense hit at rank 1 and wins the tie-break. Measured 2026-09-02 against a live
// 102-fact memory, asking in English for a fact written in Italian: the lexical
// leg returned exactly one row -- the wrong one, matched on stray English tokens
// -- and RRF put it first, while the dense leg alone ranked the right fact first
// and its next four all relevant. Wrapping the same fusion in vector.rerank
// reproduced the dense-only order exactly, which is what keeps the lexical leg
// useful for RECALL (it drags exact identifiers into the candidate set) without
// letting it decide the ORDER.
//
// Two literals rather than a builder so the three fused statements stay const.
const (
	// embeddingProperty is the vector property name on BOTH FACT and
	// ConversationTurn, which is why one rerank wrapper serves the mixed-type
	// fusion in memory_recall.go as well as the single-type ones.
	embeddingProperty = "embedding"
	rerankOpen        = "`vector.rerank`((SELECT expand("
	rerankClose       = ")), :vector, '" + embeddingProperty + "', :candidates)"
	// relevanceFloor is the ONE abstention gate, and it sits AFTER the rerank on
	// purpose: maxDistance bounds the dense leg only, so a lexical hit on an
	// incidental word used to enter the result set with nothing left to reject it.
	// Measured 2026-09-02 on a live 102-fact memory -- asking for a pizza-dough
	// recipe matched the literal word "Ricetta:" inside an unrelated fact and came
	// back as an answer. Reranked, that hit scores 0.2174, while the WORST true
	// match across three answerable questions scores 0.3326 (the others 0.4859 and
	// 0.4878) and two further unanswerable ones score 0.1674 and 0.0434. 0.28 is
	// the midpoint of 0.2174..0.3326.
	relevanceFloor = " WHERE score >= :min_relevance"
)

// fuseRIDsStatement is the documented hybrid shape, and the function names carry
// backticks because the parser reads the dot as a path step otherwise.
//
// Each source is a RANKING, not a result set: the dense leg is
// `vector.neighbors`, the lexical leg is the `@rid, $score` projection the manual
// pairs with it, and `= true` on SEARCH_INDEX is the documented form rather than
// a bare predicate. Both legs apply valid-time before their candidate limits;
// vector.neighbors accepts the inline RID subquery as its documented filter.
// The fusion returns the winning EDGES; the endpoint names it cannot carry —
// outV()/inV() are not on a fused record — are added by hydrating the rids in a
// second statement, which is one round trip for a set already bounded by the limit.
const fuseRIDsStatement = "SELECT @rid AS rid FROM (SELECT expand(" + rerankOpen + "`vector.fuse`(" +
	"`vector.neighbors`('" + factEdgeType + "[embedding]', :vector, :candidates, " +
	"{ filter: (SELECT @rid FROM " + factEdgeType + " WHERE " + asOfCondition + denseSpaceFilter +
	").@rid, maxDistance: :max_distance }), " +
	"(SELECT @rid, $score FROM " + factEdgeType +
	" WHERE SEARCH_INDEX('" + factEdgeType + "[statement]', :query) = true AND " +
	"$score >= :min_lexical_score AND " +
	asOfCondition + " LIMIT :candidates), " +
	"{ \"fusion\": \"RRF\" }" +
	")" + rerankClose + "))" + relevanceFloor + " LIMIT :candidates"

// hydrateFactsStatement rechecks validity after fusion, closing the race where a
// fact is superseded between candidate ranking and hydration.
//
// fact_key is in the projection because the lexical path's own statement has always
// carried it, and a hit that arrives with an identity or without it depending on which
// retrieval path happened to run is not a contract a caller can use. The identity is
// what memory_upsert_fact's supersedes_fact_key asks for -- "the fact_key a prior recall
// returned" -- so dropping it here meant a fact found while the embedder was UP could not
// be corrected precisely, and the same fact found with the embedder down could.
const hydrateFactsStatement = "SELECT @rid, statement, predicate, valid_from, valid_to, " +
	"sources, fact_key, outV().name AS subject, outV().kind AS subject_kind, " +
	"inV().name AS object, inV().kind AS object_kind " +
	"FROM " + factEdgeType + " WHERE @rid IN :rids AND " + asOfCondition

// FactSearchResult carries the hits together with the path that produced them and,
// when nothing qualified, the reason it abstained rather than an approximate answer.
type FactSearchResult struct {
	Facts         []FactHit
	RetrievalPath string
	Abstained     bool
	Reason        string
	// FloorsReason is ReasonUncalibratedFloors when a hybrid answer was admitted by floors
	// never measured for its space (spec §9). Reason names why a read left the dense path;
	// this names how far to trust one that did not.
	FloorsReason string
}

const (
	retrievalPathHybrid  = "hybrid"
	retrievalPathLexical = "lexical"

	reasonEmbedderNotConfigured = "embedder_not_configured"
	reasonEmbeddingFailed       = "embedding_failed"
	reasonEmbeddingInvalid      = "embedding_invalid"
	reasonFusionFailed          = "fusion_failed"
	reasonFusionResultInvalid   = "fusion_result_invalid"
	reasonHydrationFailed       = "hydration_failed"
	reasonNoQualifiedCandidates = "no_qualified_candidates"
	reasonQueryIgnoredByRecent  = "query_ignored_by_recent_mode"

	reasonEmbeddingSpaceMismatch = "embedding_space_mismatch"
	reasonSpaceCheckFailed       = "embedding_space_check_failed"
)

// SearchFactsHybrid runs both legs and fuses them with ArcadeDB's own reciprocal
// rank fusion. With no embedder configured, with the sidecar down, or with a memory
// not wholly in the reader's embedding space, it is exactly SearchFacts, which is the
// point: the dense leg is an improvement, not a dependency. The result names the path it
// took, so a caller can tell a fused answer from a lexical fallback instead of inferring it.
func (c *Client) SearchFactsHybrid(
	ctx context.Context,
	query string,
	limit int,
	asOf time.Time,
) (FactSearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return FactSearchResult{}, fmt.Errorf("arcadedb: search query must be non-empty")
	}
	limits := c.memoryLimits()
	if err := validateRuneLimit("search query", query, limits.QueryRunes); err != nil {
		return FactSearchResult{}, err
	}
	limit = boundedLimit(limit, 5, limits.Results)
	if asOf.IsZero() {
		asOf = time.Now()
	}
	dense, reason := c.denseQueryVector(ctx, query)
	if dense.vector == nil {
		return c.searchFactsFallback(ctx, query, limit, asOf, reason)
	}

	// Over-fetch each leg: fusion can only reorder what it is given, and a fact
	// ranked 8th lexically and 2nd densely is exactly the one the fusion exists
	// to promote.
	candidates := min(max(limit*4, 20), limits.HybridCandidates)
	params := map[string]any{
		"query":             escapeLucene(query),
		"candidates":        candidates,
		"as_of":             asOf.UTC().Format(time.RFC3339),
		"min_lexical_score": lexicalScoreFloor(query, limits.LexicalMinScore),
	}
	dense.bind(params)
	floorsReason := limits.bindDenseFloors(params, dense.space)
	ranked, err := c.Query(ctx, fuseRIDsStatement, params)
	if err != nil {
		// A fusion that fails must not lose the answer the lexical leg already had.
		return c.searchFactsFallback(ctx, query, limit, asOf, reasonFusionFailed)
	}
	if len(ranked) == 0 {
		return FactSearchResult{
			RetrievalPath: retrievalPathHybrid,
			Abstained:     true,
			Reason:        reasonNoQualifiedCandidates,
			FloorsReason:  floorsReason,
		}, nil
	}
	rids := make([]string, 0, len(ranked))
	for _, row := range ranked {
		if rid := strings.TrimSpace(fmt.Sprintf("%v", row["rid"])); rid != "" && rid != "<nil>" {
			rids = append(rids, rid)
		}
	}
	if len(rids) == 0 {
		return c.searchFactsFallback(ctx, query, limit, asOf, reasonFusionResultInvalid)
	}
	rows, err := c.Query(ctx, hydrateFactsStatement, map[string]any{
		"rids":  rids,
		"as_of": asOf.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return c.searchFactsFallback(ctx, query, limit, asOf, reasonHydrationFailed)
	}
	// The hydration is a set read, so it loses the fused order; restore it from
	// the ranking, which is the only thing that knew it. The rank travels WITH
	// each hit rather than being looked up by position: sorting the hits while
	// reading the rank out of the parallel rows slice reads a different row after
	// the first swap.
	order := make(map[string]int, len(rids))
	for i, rid := range rids {
		order[rid] = i
	}
	ordered := make([]rankedFact, 0, len(rows))
	for _, row := range rows {
		rank, ok := order[fmt.Sprintf("%v", row["@rid"])]
		if !ok {
			rank = len(order)
		}
		ordered = append(ordered, rankedFact{hit: factHitFromRow(row), rank: rank})
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].rank < ordered[j].rank })

	hits := make([]FactHit, 0, min(len(ordered), limit))
	identifiers := memoryQueryIdentifiers(query)
	for _, item := range ordered {
		if len(hits) == limit {
			break
		}
		if memoryIdentifiersMatch(identifiers, item.hit.Statement, item.hit.Subject, item.hit.Object) {
			hits = append(hits, item.hit)
		}
	}
	result := FactSearchResult{Facts: hits, RetrievalPath: retrievalPathHybrid, FloorsReason: floorsReason}
	if len(hits) == 0 {
		result.Abstained = true
		result.Reason = reasonNoQualifiedCandidates
	}
	return result, nil
}

func (c *Client) searchFactsFallback(
	ctx context.Context,
	query string,
	limit int,
	asOf time.Time,
	reason string,
) (FactSearchResult, error) {
	hits, err := c.SearchFacts(ctx, query, limit, asOf)
	if err != nil {
		return FactSearchResult{}, err
	}
	return FactSearchResult{
		Facts:         hits,
		RetrievalPath: retrievalPathLexical,
		Abstained:     len(hits) == 0,
		Reason:        reason,
	}, nil
}

// rankedFact keeps a hydrated fact next to its place in the fused ranking.
type rankedFact struct {
	hit  FactHit
	rank int
}

// luceneBooleanKeywords are read as operators by Lucene's classic parser, and ONLY in
// upper case. A backslash cannot disarm them the way it disarms punctuation, so the
// standalone token is lower-cased instead: the analyzer folds case before matching, so
// this changes what the parser does without changing what the query finds.
var luceneBooleanKeywords = map[string]string{"AND": "and", "OR": "or", "NOT": "not"}

// escapeLucene neutralises the query-syntax characters in user text before it
// reaches SEARCH_INDEX. Lucene's parser treats `?`, `*`, `"`, `~`, `:` and the
// rest as operators, so a question ending in `…impossible"?` was not a poor
// query, it was a PARSE ERROR — and the benchmark that hit it counted the
// resulting zero rows as a recall miss for weeks.
func escapeLucene(query string) string {
	fields := strings.Fields(query)
	for i, field := range fields {
		if lowered, ok := luceneBooleanKeywords[field]; ok {
			fields[i] = lowered
		}
	}
	query = strings.Join(fields, " ")
	var b strings.Builder
	b.Grow(len(query) + 8)
	for _, r := range query {
		switch r {
		case '+', '-', '&', '|', '!', '(', ')', '{', '}', '[', ']',
			'^', '"', '~', '*', '?', ':', '\\', '/':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
