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
	return []string{
		// ARRAY_OF_FLOATS, not LIST OF FLOAT. The manual shows both, but they are not
		// interchangeable at the binding: a JSON array sent through the SQL endpoint
		// arrives as ARRAY_OF_FLOATS, and a property declared LIST OF FLOAT rejects it
		// with "declared as LIST of 'FLOAT' but a value of type 'ARRAY_OF_FLOATS' is
		// used". The declaration follows the wire, not the prose.
		"CREATE PROPERTY " + factEdgeType + ".embedding IF NOT EXISTS ARRAY_OF_FLOATS",
		"CREATE INDEX IF NOT EXISTS ON " + factEdgeType + " (embedding) LSM_VECTOR METADATA " +
			"{ \"dimensions\": " + strconv.Itoa(vectorDimensions) +
			", \"similarity\": \"COSINE\", \"quantization\": \"" + vectorQuantization + "\" }",
	}
}

// embedStatement returns the vector for one fact statement, or nil when no
// embedder is configured. A nil vector is not an error: the fact is still
// written and still reachable through the lexical leg.
func (c *Client) embedStatement(ctx context.Context, statement string) []float64 {
	if c == nil || c.embedder == nil || strings.TrimSpace(statement) == "" {
		return nil
	}
	vectors, err := c.embedder.Embed(ctx, withTask(taskDocumentPrefix, []string{statement}))
	if err != nil || len(vectors) != 1 || len(vectors[0]) != vectorDimensions {
		// Fail SOFT and deliberately: an embedder that is down must degrade
		// retrieval, never refuse a write. A fact that was not stored is lost;
		// a fact stored without its vector is found lexically today and can be
		// embedded later by EmbedMissingFacts.
		return nil
	}
	return vectors[0]
}

// embedStatements is embedStatement for a whole batch: ONE round trip for every
// statement a memory batch is about to create, keyed by statement so the caller can
// attach each vector to the fact it belongs to.
//
// It is called BEFORE the ArcadeDB transaction opens, deliberately. The embedder is
// an HTTP sidecar, and holding a write transaction open across a network call to a
// second service would put that service's latency inside the lock the batch holds on
// the identity. Statements are de-duplicated because a batch may restate the same
// fact more than once and the vector is a pure function of the text.
//
// Fail-soft matches embedStatement exactly: a down embedder yields no vectors, the
// facts are still written, and EmbedMissingFacts fills them in later. The caller
// distinguishes "no vector for this statement" by a missing map key, so a partial
// result embeds what it can rather than discarding the batch.
func (c *Client) embedStatements(ctx context.Context, statements []string) map[string][]float64 {
	if c == nil || c.embedder == nil || len(statements) == 0 {
		return nil
	}
	unique := make([]string, 0, len(statements))
	seen := make(map[string]struct{}, len(statements))
	for _, statement := range statements {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, done := seen[statement]; done {
			continue
		}
		seen[statement] = struct{}{}
		unique = append(unique, statement)
	}
	if len(unique) == 0 {
		return nil
	}
	vectors, err := c.embedder.Embed(ctx, withTask(taskDocumentPrefix, unique))
	if err != nil || len(vectors) != len(unique) {
		return nil
	}
	embedded := make(map[string][]float64, len(unique))
	for i, statement := range unique {
		if len(vectors[i]) != vectorDimensions {
			continue
		}
		embedded[statement] = vectors[i]
	}
	return embedded
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
	"{ filter: (SELECT @rid FROM " + factEdgeType + " WHERE " + asOfCondition +
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
)

// SearchFactsHybrid runs both legs and fuses them with ArcadeDB's own reciprocal
// rank fusion. With no embedder configured — or with the sidecar down — it is
// exactly SearchFacts, which is the point: the dense leg is an improvement, not a
// dependency. The result names the path it took, so a caller can tell a fused
// answer from a lexical fallback instead of inferring it.
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
	if c.embedder == nil {
		return c.searchFactsFallback(ctx, query, limit, asOf, reasonEmbedderNotConfigured)
	}
	vectors, err := c.embedder.Embed(ctx, withTask(taskQueryPrefix, []string{query}))
	if err != nil {
		return c.searchFactsFallback(ctx, query, limit, asOf, reasonEmbeddingFailed)
	}
	if len(vectors) != 1 || len(vectors[0]) != vectorDimensions {
		return c.searchFactsFallback(ctx, query, limit, asOf, reasonEmbeddingInvalid)
	}

	// Over-fetch each leg: fusion can only reorder what it is given, and a fact
	// ranked 8th lexically and 2nd densely is exactly the one the fusion exists
	// to promote.
	candidates := min(max(limit*4, 20), limits.HybridCandidates)
	ranked, err := c.Query(ctx, fuseRIDsStatement, map[string]any{
		"query":        escapeLucene(query),
		"vector":       vectors[0],
		"candidates":   candidates,
		"as_of":        asOf.UTC().Format(time.RFC3339),
		"max_distance": limits.DenseMaxDistance, "min_relevance": limits.MinRelevance,
		"min_lexical_score": lexicalScoreFloor(query, limits.LexicalMinScore),
	})
	if err != nil {
		// A fusion that fails must not lose the answer the lexical leg already had.
		return c.searchFactsFallback(ctx, query, limit, asOf, reasonFusionFailed)
	}
	if len(ranked) == 0 {
		return FactSearchResult{
			RetrievalPath: retrievalPathHybrid,
			Abstained:     true,
			Reason:        reasonNoQualifiedCandidates,
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
	for _, item := range ordered {
		if len(hits) == limit {
			break
		}
		hits = append(hits, item.hit)
	}
	result := FactSearchResult{Facts: hits, RetrievalPath: retrievalPathHybrid}
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

// EmbedMissingFacts backfills vectors for facts written before an embedder was
// configured, or while it was down. It returns how many it embedded.
func (c *Client) EmbedMissingFacts(ctx context.Context, batch int) (int, error) {
	return c.embedFacts(ctx, batch, "embedding IS NULL AND statement IS NOT NULL")
}

// ReEmbedAllFacts recomputes EVERY fact's vector, including the ones that already have
// one. It exists because a vector is only meaningful against the model that produced it:
// swap the embedder and the stored corpus is silently in the wrong geometry, still
// answering, just answering worse. That is not hypothetical — on 2026-08-02 the appliance
// was found running a GGUF missing EmbeddingGemma's two dense projections, so every vector
// written until then came from the backbone alone.
//
// Deliberately NOT idempotent-by-skipping: re-running it is the point.
//
// It CLEARS every vector and then drains the gap, rather than selecting the facts to
// re-embed directly. The direct selection is what shipped, and it could not finish:
// `WHERE statement IS NOT NULL LIMIT batch` describes a set that re-embedding does not
// shrink, so every call returned the same first `batch` rows and everything past them
// kept the old model's vector forever -- on the one operation whose entire purpose is
// that no vector is left in the old geometry. Measured 2026-09-03 through the MCP
// surface: two consecutive `all` calls on a 55-fact memory both reported 30.
//
// "Has no vector" is the only predicate that shrinks as the work is done, which is why
// the embed backfill was built on it, and reusing it here means the two paths cannot
// drift. The cost is a window where the cleared tail answers lexically only -- which the
// hybrid search already treats as normal, the dense leg being an improvement and never a
// dependency -- and anything this call's rounds do not reach is picked up by the
// scheduled memory_embed_backfill sweep, because it selects on exactly that predicate.
func (c *Client) ReEmbedAllFacts(ctx context.Context, batch int) (int, error) {
	if c == nil || c.embedder == nil {
		return 0, fmt.Errorf("arcadedb: no embedder configured")
	}
	if _, err := c.Command(ctx, clearFactEmbeddingsStatement, nil); err != nil {
		return 0, fmt.Errorf("arcadedb: clear fact vectors: %w", err)
	}
	return embedMissingInBatches(ctx, c, batch)
}

// clearFactEmbeddingsStatement drops the stored vectors. It is filtered on the property
// being present so the write touches only the rows that have one.
const clearFactEmbeddingsStatement = "UPDATE " + factEdgeType +
	" SET embedding = NULL WHERE embedding IS NOT NULL"

// embedFacts is the shared body. `where` decides which facts are in scope; everything
// after it — batching, the document-side task prefix, the width check — is identical,
// because a backfill and a re-embed differ only in what they select.
func (c *Client) embedFacts(ctx context.Context, batch int, where string) (int, error) {
	if c == nil || c.embedder == nil {
		return 0, fmt.Errorf("arcadedb: no embedder configured")
	}
	batch = boundedLimit(batch, 100, c.memoryLimits().MaintenanceBatch)
	rows, err := c.Query(ctx,
		"SELECT @rid AS rid, statement FROM "+factEdgeType+
			" WHERE "+where+" LIMIT "+strconv.Itoa(batch), nil)
	if err != nil {
		return 0, fmt.Errorf("arcadedb: select facts to embed: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	statements := make([]string, 0, len(rows))
	rids := make([]string, 0, len(rows))
	for _, row := range rows {
		statement, _ := row["statement"].(string)
		rid := fmt.Sprintf("%v", row["rid"])
		if strings.TrimSpace(statement) == "" || rid == "" {
			continue
		}
		statements = append(statements, statement)
		rids = append(rids, rid)
	}
	vectors, err := c.embedder.Embed(ctx, withTask(taskDocumentPrefix, statements))
	if err != nil {
		return 0, fmt.Errorf("arcadedb: embed backfill: %w", err)
	}
	return c.writeVectors(ctx, rids, vectors)
}

// writeVectors stores a whole batch in ONE round trip, and falls back to one
// statement per fact only when that fails.
//
// The round trip is the entire cost here. Measured on this host 2026-08-03: a
// vector UPDATE addressed by @rid takes 55-78ms while a `SELECT 1` on the same
// connection takes 53-63ms, so the write itself is ~10ms and the rest is HTTP.
// A batch of 32 written one at a time therefore spent ~1.8s in handshakes to do
// ~0.3s of work — more than half the sweep's wall clock.
//
// sqlscript is the server's own answer: "A sqlscript can consist of one or
// multiple SQL statements, which is collectively treated as a transaction ...
// begin and commit implicitly enclose any sqlscript command"
// (arcadedb-docs, reference/http-api/http.adoc).
//
// The per-fact fallback is not belt-and-braces, it is the poison-row cure. A
// script is atomic, so ONE malformed row would roll back its 31 healthy
// companions and do so again on every later sweep — the batch would never land
// and the tenant would stall behind it. Falling back isolates the bad row and
// lets the rest through, which is also why this path reports how many landed
// instead of abandoning the batch at the first error the way it used to.
func (c *Client) writeVectors(ctx context.Context, rids []string, vectors [][]float64) (int, error) {
	statements := make([]string, 0, len(rids))
	params := make(map[string]any, len(rids)*2)
	usable := make([]int, 0, len(rids))
	for i, rid := range rids {
		// A SHORT answer is skipped, not indexed into. The embedder is a sidecar over
		// HTTP and nothing makes it return one vector per text: it used to be indexed
		// positionally, so an answer with fewer vectors than statements panicked the
		// process with an index out of range rather than embedding what it could. A
		// missing vector is the same case as the wrong-width vector below -- unusable
		// for this fact, harmless to the rest of the batch -- and the drain loop stops
		// on a short round rather than spinning on it.
		if i >= len(vectors) || len(vectors[i]) != vectorDimensions {
			continue
		}
		usable = append(usable, i)
		n := strconv.Itoa(len(usable) - 1)
		statements = append(statements,
			"UPDATE "+factEdgeType+" SET embedding = :v"+n+" WHERE @rid = :r"+n)
		params["v"+n] = vectors[i]
		params["r"+n] = rid
	}
	if len(statements) == 0 {
		return 0, nil
	}
	if _, err := c.Script(ctx, strings.Join(statements, ";\n"), params); err == nil {
		return len(statements), nil
	}
	written := 0
	var failures []string
	for _, i := range usable {
		if _, err := c.Command(ctx,
			"UPDATE "+factEdgeType+" SET embedding = :vector WHERE @rid = :rid",
			map[string]any{"vector": vectors[i], "rid": rids[i]}); err != nil {
			failures = append(failures, rids[i])
			continue
		}
		written++
	}
	if len(failures) > 0 {
		return written, fmt.Errorf("arcadedb: write embedding failed for %d of %d facts (first %s)",
			len(failures), len(usable), failures[0])
	}
	return written, nil
}

// escapeLucene neutralises the query-syntax characters in user text before it
// reaches SEARCH_INDEX. Lucene's parser treats `?`, `*`, `"`, `~`, `:` and the
// rest as operators, so a question ending in `…impossible"?` was not a poor
// query, it was a PARSE ERROR — and the benchmark that hit it counted the
// resulting zero rows as a recall miss for weeks.
func escapeLucene(query string) string {
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
