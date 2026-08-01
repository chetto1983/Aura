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
	vectors, err := c.embedder.Embed(ctx, []string{statement})
	if err != nil || len(vectors) != 1 || len(vectors[0]) != vectorDimensions {
		// Fail SOFT and deliberately: an embedder that is down must degrade
		// retrieval, never refuse a write. A fact that was not stored is lost;
		// a fact stored without its vector is found lexically today and can be
		// embedded later by EmbedMissingFacts.
		return nil
	}
	return vectors[0]
}

// fuseRIDsStatement is the documented hybrid shape, and the function names carry
// backticks because the parser reads the dot as a path step otherwise.
//
// Each source is a RANKING, not a result set: the dense leg is
// `vector.neighbors`, the lexical leg is the `@rid, $score` projection the manual
// pairs with it, and `= true` on SEARCH_INDEX is the documented form rather than
// a bare predicate. The fusion returns the winning EDGES; the endpoint names it
// cannot carry — outV()/inV() are not on a fused record — are added by hydrating
// the rids in a second statement, which is one round trip for a set already
// bounded by the limit.
const fuseRIDsStatement = "SELECT @rid AS rid FROM (SELECT expand(`vector.fuse`(" +
	"`vector.neighbors`('" + factEdgeType + "[embedding]', :vector, :candidates), " +
	"(SELECT @rid, $score FROM " + factEdgeType +
	" WHERE SEARCH_INDEX('" + factEdgeType + "[statement]', :query) = true), " +
	"{ \"fusion\": \"RRF\" }" +
	"))) LIMIT :candidates"

// hydrateFactsStatement turns the fused rids back into whole facts, applying the
// validity window here rather than inside the fusion: a leg that filtered by
// as_of would rank against a different candidate set than the other.
const hydrateFactsStatement = "SELECT @rid, statement, predicate, valid_from, valid_to, " +
	"source_run_id, source_memory_ids, outV().name AS subject, inV().name AS object " +
	"FROM " + factEdgeType + " WHERE @rid IN :rids AND " + asOfCondition

// SearchFactsHybrid runs both legs and fuses them with ArcadeDB's own reciprocal
// rank fusion. With no embedder configured — or with the sidecar down — it is
// exactly SearchFacts, which is the point: the dense leg is an improvement, not a
// dependency.
func (c *Client) SearchFactsHybrid(
	ctx context.Context,
	query string,
	limit int,
	asOf time.Time,
) ([]FactHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("arcadedb: search query must be non-empty")
	}
	if limit <= 0 {
		limit = 5
	}
	if asOf.IsZero() {
		asOf = time.Now()
	}
	if c.embedder == nil {
		return c.SearchFacts(ctx, query, limit, asOf)
	}
	vectors, err := c.embedder.Embed(ctx, []string{query})
	if err != nil || len(vectors) != 1 || len(vectors[0]) != vectorDimensions {
		return c.SearchFacts(ctx, query, limit, asOf)
	}

	// Over-fetch each leg: fusion can only reorder what it is given, and a fact
	// ranked 8th lexically and 2nd densely is exactly the one the fusion exists
	// to promote.
	candidates := max(limit*4, 20)
	ranked, err := c.Query(ctx, fuseRIDsStatement, map[string]any{
		"query":      escapeLucene(query),
		"vector":     vectors[0],
		"candidates": candidates,
	})
	if err != nil || len(ranked) == 0 {
		// A fusion that fails must not lose the answer the lexical leg already had.
		return c.SearchFacts(ctx, query, limit, asOf)
	}
	rids := make([]string, 0, len(ranked))
	for _, row := range ranked {
		if rid := strings.TrimSpace(fmt.Sprintf("%v", row["rid"])); rid != "" && rid != "<nil>" {
			rids = append(rids, rid)
		}
	}
	if len(rids) == 0 {
		return c.SearchFacts(ctx, query, limit, asOf)
	}
	rows, err := c.Query(ctx, hydrateFactsStatement, map[string]any{
		"rids":  rids,
		"as_of": asOf.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return c.SearchFacts(ctx, query, limit, asOf)
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
	return hits, nil
}

// rankedFact keeps a hydrated fact next to its place in the fused ranking.
type rankedFact struct {
	hit  FactHit
	rank int
}

// EmbedMissingFacts backfills vectors for facts written before an embedder was
// configured, or while it was down. It returns how many it embedded.
func (c *Client) EmbedMissingFacts(ctx context.Context, batch int) (int, error) {
	if c == nil || c.embedder == nil {
		return 0, fmt.Errorf("arcadedb: no embedder configured")
	}
	if batch <= 0 {
		batch = 100
	}
	rows, err := c.Query(ctx,
		"SELECT @rid AS rid, statement FROM "+factEdgeType+
			" WHERE embedding IS NULL AND statement IS NOT NULL LIMIT "+strconv.Itoa(batch), nil)
	if err != nil {
		return 0, fmt.Errorf("arcadedb: find unembedded facts: %w", err)
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
	vectors, err := c.embedder.Embed(ctx, statements)
	if err != nil {
		return 0, fmt.Errorf("arcadedb: embed backfill: %w", err)
	}
	written := 0
	for i, rid := range rids {
		if len(vectors[i]) != vectorDimensions {
			continue
		}
		if _, err := c.Command(ctx,
			"UPDATE "+factEdgeType+" SET embedding = :vector WHERE @rid = :rid",
			map[string]any{"vector": vectors[i], "rid": rid}); err != nil {
			return written, fmt.Errorf("arcadedb: write embedding for %s: %w", rid, err)
		}
		written++
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
